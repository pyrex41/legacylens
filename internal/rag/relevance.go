package rag

import (
	"context"
	"fmt"
	"strings"
)

// RelevanceJudgment defines an expected result for a query. MatchName is
// matched case-insensitively against chunk Name fields. MatchType optionally
// restricts to a specific ChunkType (empty means any type matches).
type RelevanceJudgment struct {
	MatchName string
	MatchType ChunkType
}

// EvalQuery pairs a natural-language query with its expected relevant results.
type EvalQuery struct {
	Query    string
	Expected []RelevanceJudgment
}

// RelevanceMetrics holds precision/recall/hit-rate for a single query.
type RelevanceMetrics struct {
	Query     string  `json:"query"`
	Precision float64 `json:"precision"`
	Recall    float64 `json:"recall"`
	HitRate   float64 `json:"hit_rate"`
	Hits      int     `json:"hits"`
	Expected  int     `json:"expected"`
	Retrieved int     `json:"retrieved"`
}

// RelevanceSummary aggregates metrics across all evaluation queries.
type RelevanceSummary struct {
	MeanPrecision float64            `json:"mean_precision"`
	MeanRecall    float64            `json:"mean_recall"`
	MeanHitRate   float64            `json:"mean_hit_rate"`
	QueryCount    int                `json:"query_count"`
	PassRate      float64            `json:"pass_rate"`
	Queries       []RelevanceMetrics `json:"queries"`
}

func judgeMatch(j RelevanceJudgment, ch Chunk) bool {
	if !strings.EqualFold(j.MatchName, ch.Name) {
		nameHit := false
		for _, part := range strings.Split(ch.Name, "_part_") {
			if strings.EqualFold(j.MatchName, part) {
				nameHit = true
				break
			}
		}
		if !nameHit && !strings.HasPrefix(strings.ToLower(ch.Name), strings.ToLower(j.MatchName)+"_part_") {
			return false
		}
	}
	if j.MatchType != "" && j.MatchType != ch.Type {
		return false
	}
	return true
}

func scoreQuery(results []SearchResult, expected []RelevanceJudgment) RelevanceMetrics {
	if len(expected) == 0 {
		return RelevanceMetrics{Expected: 0, Retrieved: len(results), HitRate: 1}
	}

	hits := 0
	matched := make([]bool, len(expected))
	for _, r := range results {
		for i, j := range expected {
			if !matched[i] && judgeMatch(j, r.Chunk) {
				matched[i] = true
				hits++
				break
			}
		}
	}

	precision := 0.0
	if len(results) > 0 {
		precision = float64(hits) / float64(len(results))
	}
	recall := float64(hits) / float64(len(expected))
	hitRate := 0.0
	if hits > 0 {
		hitRate = 1.0
	}

	return RelevanceMetrics{
		Precision: precision,
		Recall:    recall,
		HitRate:   hitRate,
		Hits:      hits,
		Expected:  len(expected),
		Retrieved: len(results),
	}
}

// EvalRelevance runs each evaluation query through the engine and scores
// the results against expected judgments.
func EvalRelevance(ctx context.Context, engine *QueryEngine, queries []EvalQuery, k int) (*RelevanceSummary, error) {
	if k <= 0 {
		k = 5
	}
	summary := &RelevanceSummary{
		QueryCount: len(queries),
		Queries:    make([]RelevanceMetrics, len(queries)),
	}

	var totalP, totalR, totalH float64
	passCount := 0

	for i, eq := range queries {
		results, err := engine.Search(ctx, eq.Query, k)
		if err != nil {
			return nil, fmt.Errorf("eval query %d %q: %w", i+1, eq.Query, err)
		}
		m := scoreQuery(results, eq.Expected)
		m.Query = eq.Query
		summary.Queries[i] = m

		totalP += m.Precision
		totalR += m.Recall
		totalH += m.HitRate
		if m.Recall >= 0.5 {
			passCount++
		}
	}

	n := float64(len(queries))
	summary.MeanPrecision = totalP / n
	summary.MeanRecall = totalR / n
	summary.MeanHitRate = totalH / n
	summary.PassRate = float64(passCount) / n

	return summary, nil
}

// FormatRelevanceSummary returns a human-readable relevance report.
func FormatRelevanceSummary(s *RelevanceSummary) string {
	var b strings.Builder
	b.WriteString("\n--- Relevance Evaluation ---\n")
	fmt.Fprintf(&b, "Queries:        %d\n", s.QueryCount)
	fmt.Fprintf(&b, "Mean Precision: %.2f%%\n", s.MeanPrecision*100)
	fmt.Fprintf(&b, "Mean Recall:    %.2f%%\n", s.MeanRecall*100)
	fmt.Fprintf(&b, "Mean Hit Rate:  %.2f%%\n", s.MeanHitRate*100)
	fmt.Fprintf(&b, "Pass Rate:      %.2f%% (recall >= 50%%)\n", s.PassRate*100)
	b.WriteString("\nPer-query breakdown:\n")
	for _, q := range s.Queries {
		status := "PASS"
		if q.Recall < 0.5 {
			status = "FAIL"
		}
		fmt.Fprintf(&b, "  [%s] %q: P=%.0f%% R=%.0f%% hits=%d/%d\n",
			status, q.Query, q.Precision*100, q.Recall*100, q.Hits, q.Expected)
	}
	return b.String()
}

// CuratedEvalQueries returns a 20-query evaluation set targeting M_blas
// BLAS routines. Each query has expected result names that should appear
// in top-K retrieval. These cover all BLAS levels and common query patterns.
func CuratedEvalQueries() []EvalQuery {
	return []EvalQuery{
		{
			Query:    "How does sgemv work?",
			Expected: []RelevanceJudgment{{MatchName: "sgemv", MatchType: ChunkTypeSubroutine}},
		},
		{
			Query:    "matrix vector multiplication single precision",
			Expected: []RelevanceJudgment{{MatchName: "sgemv"}, {MatchName: "dgemv"}},
		},
		{
			Query:    "What parameters does dgemm accept?",
			Expected: []RelevanceJudgment{{MatchName: "dgemm", MatchType: ChunkTypeSubroutine}},
		},
		{
			Query:    "double precision matrix multiply",
			Expected: []RelevanceJudgment{{MatchName: "dgemm"}},
		},
		{
			Query:    "triangular solve",
			Expected: []RelevanceJudgment{{MatchName: "dtrsv"}, {MatchName: "strsv"}},
		},
		{
			Query:    "BLAS level 2 operations",
			Expected: []RelevanceJudgment{{MatchName: "sgemv"}, {MatchName: "dgemv"}},
		},
		{
			Query:    "symmetric matrix operations",
			Expected: []RelevanceJudgment{{MatchName: "dsymv"}, {MatchName: "ssymv"}, {MatchName: "dsymm"}, {MatchName: "ssymm"}},
		},
		{
			Query:    "vector scaling routine",
			Expected: []RelevanceJudgment{{MatchName: "sscal"}, {MatchName: "dscal"}},
		},
		{
			Query:    "copy vector elements",
			Expected: []RelevanceJudgment{{MatchName: "scopy"}, {MatchName: "dcopy"}},
		},
		{
			Query:    "dot product computation",
			Expected: []RelevanceJudgment{{MatchName: "sdot"}, {MatchName: "ddot"}},
		},
		{
			Query:    "axpy operation y = alpha*x + y",
			Expected: []RelevanceJudgment{{MatchName: "saxpy"}, {MatchName: "daxpy"}},
		},
		{
			Query:    "Euclidean norm of a vector",
			Expected: []RelevanceJudgment{{MatchName: "snrm2"}, {MatchName: "dnrm2"}},
		},
		{
			Query:    "rank-1 update of a matrix",
			Expected: []RelevanceJudgment{{MatchName: "sger"}, {MatchName: "dger"}},
		},
		{
			Query:    "triangular matrix multiply",
			Expected: []RelevanceJudgment{{MatchName: "strmm"}, {MatchName: "dtrmm"}, {MatchName: "strmv"}, {MatchName: "dtrmv"}},
		},
		{
			Query:    "swap two vectors",
			Expected: []RelevanceJudgment{{MatchName: "sswap"}, {MatchName: "dswap"}},
		},
		{
			Query:    "find index of maximum absolute value",
			Expected: []RelevanceJudgment{{MatchName: "isamax"}, {MatchName: "idamax"}},
		},
		{
			Query:    "sum of absolute values",
			Expected: []RelevanceJudgment{{MatchName: "sasum"}, {MatchName: "dasum"}},
		},
		{
			Query:    "Givens rotation",
			Expected: []RelevanceJudgment{{MatchName: "srot"}, {MatchName: "drot"}, {MatchName: "srotg"}, {MatchName: "drotg"}},
		},
		{
			Query:    "band matrix vector multiply",
			Expected: []RelevanceJudgment{{MatchName: "sgbmv"}, {MatchName: "dgbmv"}},
		},
		{
			Query:    "symmetric rank-k update",
			Expected: []RelevanceJudgment{{MatchName: "ssyrk"}, {MatchName: "dsyrk"}},
		},
	}
}
