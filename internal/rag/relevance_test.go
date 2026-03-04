package rag

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestScoreQueryPerfectRecall(t *testing.T) {
	results := []SearchResult{
		{Chunk: Chunk{Name: "sgemv", Type: ChunkTypeSubroutine}},
		{Chunk: Chunk{Name: "dgemv", Type: ChunkTypeSubroutine}},
		{Chunk: Chunk{Name: "other", Type: ChunkTypeFunction}},
	}
	expected := []RelevanceJudgment{
		{MatchName: "sgemv", MatchType: ChunkTypeSubroutine},
		{MatchName: "dgemv"},
	}
	m := scoreQuery(results, expected)
	if m.Recall != 1.0 {
		t.Errorf("recall: got %.2f, want 1.0", m.Recall)
	}
	if m.Hits != 2 {
		t.Errorf("hits: got %d, want 2", m.Hits)
	}
	wantPrecision := 2.0 / 3.0
	if m.Precision < wantPrecision-0.01 || m.Precision > wantPrecision+0.01 {
		t.Errorf("precision: got %.4f, want ~%.4f", m.Precision, wantPrecision)
	}
	if m.HitRate != 1.0 {
		t.Errorf("hit rate: got %.2f, want 1.0", m.HitRate)
	}
}

func TestScoreQueryNoHits(t *testing.T) {
	results := []SearchResult{
		{Chunk: Chunk{Name: "unrelated", Type: ChunkTypeFunction}},
	}
	expected := []RelevanceJudgment{
		{MatchName: "sgemv"},
	}
	m := scoreQuery(results, expected)
	if m.Recall != 0 {
		t.Errorf("recall: got %.2f, want 0", m.Recall)
	}
	if m.HitRate != 0 {
		t.Errorf("hit rate: got %.2f, want 0", m.HitRate)
	}
}

func TestScoreQueryPartialMatch(t *testing.T) {
	results := []SearchResult{
		{Chunk: Chunk{Name: "sgemv", Type: ChunkTypeSubroutine}},
		{Chunk: Chunk{Name: "other", Type: ChunkTypeFunction}},
	}
	expected := []RelevanceJudgment{
		{MatchName: "sgemv"},
		{MatchName: "dgemv"},
	}
	m := scoreQuery(results, expected)
	if m.Recall != 0.5 {
		t.Errorf("recall: got %.2f, want 0.5", m.Recall)
	}
	if m.Hits != 1 {
		t.Errorf("hits: got %d, want 1", m.Hits)
	}
}

func TestScoreQueryEmptyExpected(t *testing.T) {
	results := []SearchResult{
		{Chunk: Chunk{Name: "sgemv"}},
	}
	m := scoreQuery(results, nil)
	if m.HitRate != 1.0 {
		t.Errorf("hit rate: got %.2f, want 1.0 (no expectations)", m.HitRate)
	}
}

func TestJudgeMatchCaseInsensitive(t *testing.T) {
	j := RelevanceJudgment{MatchName: "SGEMV"}
	ch := Chunk{Name: "sgemv", Type: ChunkTypeSubroutine}
	if !judgeMatch(j, ch) {
		t.Error("expected case-insensitive match")
	}
}

func TestJudgeMatchTypeMismatch(t *testing.T) {
	j := RelevanceJudgment{MatchName: "sgemv", MatchType: ChunkTypeFunction}
	ch := Chunk{Name: "sgemv", Type: ChunkTypeSubroutine}
	if judgeMatch(j, ch) {
		t.Error("expected type mismatch to reject")
	}
}

func TestJudgeMatchPartSuffix(t *testing.T) {
	j := RelevanceJudgment{MatchName: "sgemv"}
	ch := Chunk{Name: "sgemv_part_1", Type: ChunkTypeSubroutine}
	if !judgeMatch(j, ch) {
		t.Error("expected _part_ suffix to match base name")
	}
}

func TestEvalRelevanceIntegration(t *testing.T) {
	root := t.TempDir()
	src := `module m_test
contains
subroutine sgemv(trans, m, n, alpha, a, lda, x, incx, beta, y, incy)
  character, intent(in) :: trans
  integer, intent(in) :: m, n, lda, incx, incy
  real, intent(in) :: alpha, beta
  real, intent(in) :: a(lda,*), x(*)
  real, intent(inout) :: y(*)
end subroutine sgemv

subroutine saxpy(n, a, x, incx, y, incy)
  integer, intent(in) :: n, incx, incy
  real, intent(in) :: a
  real, intent(in) :: x(*)
  real, intent(inout) :: y(*)
end subroutine saxpy
end module m_test`
	if err := os.WriteFile(filepath.Join(root, "test.f90"), []byte(src), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	embedder := NewHashEmbedder(64)
	store := NewProductionSQLiteStore(embedder.Dimension(), ":memory:")
	if err := store.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}
	defer store.Close()

	pipeline := NewPipeline(DefaultPipelineConfig(), embedder, store)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := pipeline.IngestRepo(ctx, root); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	engine := NewQueryEngine(store, embedder)
	queries := []EvalQuery{
		{
			Query:    "sgemv matrix vector",
			Expected: []RelevanceJudgment{{MatchName: "sgemv"}},
		},
		{
			Query:    "saxpy vector add",
			Expected: []RelevanceJudgment{{MatchName: "saxpy"}},
		},
	}

	summary, err := EvalRelevance(ctx, engine, queries, 5)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if summary.QueryCount != 2 {
		t.Errorf("query count: got %d, want 2", summary.QueryCount)
	}
	if summary.MeanHitRate < 0.5 {
		t.Errorf("mean hit rate too low: %.2f", summary.MeanHitRate)
	}

	formatted := FormatRelevanceSummary(summary)
	if formatted == "" {
		t.Error("expected non-empty formatted summary")
	}
	if !strings.Contains(formatted, "Relevance Evaluation") {
		t.Error("expected summary to contain 'Relevance Evaluation'")
	}
}

func TestCuratedEvalQueriesCount(t *testing.T) {
	queries := CuratedEvalQueries()
	if len(queries) != 20 {
		t.Errorf("expected 20 curated queries, got %d", len(queries))
	}
	for i, q := range queries {
		if q.Query == "" {
			t.Errorf("query %d has empty query string", i)
		}
		if len(q.Expected) == 0 {
			t.Errorf("query %d %q has no expected results", i, q.Query)
		}
	}
}
