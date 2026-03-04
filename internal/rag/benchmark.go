package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"runtime"
	"sort"
	"time"
)

type BenchmarkConfig struct {
	Runs       int
	TopK       int
	Queries    []string
	RepoPath   string
	ReportPath string
	EvalQueries []EvalQuery
}

func DefaultBenchmarkConfig() BenchmarkConfig {
	return BenchmarkConfig{
		Runs: 5,
		TopK: 5,
		Queries: []string{
			"How does sgemv work?",
			"matrix vector multiplication",
			"What parameters does dgemm accept?",
			"triangular solve",
			"BLAS level 2 operations",
		},
	}
}

type LatencyStats struct {
	Min    time.Duration `json:"min_ms"`
	Max    time.Duration `json:"max_ms"`
	Mean   time.Duration `json:"mean_ms"`
	P50    time.Duration `json:"p50_ms"`
	P95    time.Duration `json:"p95_ms"`
	Stddev time.Duration `json:"stddev_ms"`
}

type QueryBenchResult struct {
	Query        string       `json:"query"`
	ResultCount  int          `json:"result_count"`
	Latency      LatencyStats `json:"latency"`
	TopResultIDs []string     `json:"top_result_ids"`
}

type BenchmarkReport struct {
	Backend           string             `json:"backend"`
	Timestamp         time.Time          `json:"timestamp"`
	Runs              int                `json:"runs"`
	ChunksIngested    int                `json:"chunks_ingested"`
	IngestDuration    LatencyStats       `json:"ingest_duration"`
	IngestMemoryMB    float64            `json:"ingest_memory_mb"`
	DBSizeBytes       int64              `json:"db_size_bytes"`
	HybridQueries     []QueryBenchResult `json:"hybrid_queries"`
	VectorOnlyQueries []QueryBenchResult `json:"vector_only_queries"`
	Relevance         *RelevanceSummary  `json:"relevance,omitempty"`
}

func RunBenchmark(ctx context.Context, cfg BenchmarkConfig, embedder Embedder, storeFactory func() (VectorStore, string)) (*BenchmarkReport, error) {
	if cfg.Runs <= 0 {
		cfg.Runs = 5
	}
	if cfg.TopK <= 0 {
		cfg.TopK = 5
	}
	if len(cfg.Queries) == 0 {
		cfg.Queries = DefaultBenchmarkConfig().Queries
	}

	store, dbPath := storeFactory()
	report := &BenchmarkReport{
		Backend:   store.Name(),
		Timestamp: time.Now().UTC(),
		Runs:      cfg.Runs,
	}

	pcfg := DefaultPipelineConfig()
	pipeline := NewPipeline(pcfg, embedder, store)

	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)

	ingestDurations := make([]time.Duration, cfg.Runs)
	var lastN int
	for i := 0; i < cfg.Runs; i++ {
		start := time.Now()
		n, err := pipeline.IngestRepo(ctx, cfg.RepoPath)
		ingestDurations[i] = time.Since(start)
		if err != nil {
			return nil, fmt.Errorf("ingest run %d: %w", i+1, err)
		}
		lastN = n
	}
	report.ChunksIngested = lastN
	report.IngestDuration = computeStats(ingestDurations)

	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)
	report.IngestMemoryMB = float64(memAfter.TotalAlloc-memBefore.TotalAlloc) / (1024 * 1024)

	if dbPath != "" && dbPath != ":memory:" {
		if info, err := os.Stat(dbPath); err == nil {
			report.DBSizeBytes = info.Size()
		}
	}

	engine := NewQueryEngine(store, embedder)

	report.HybridQueries = make([]QueryBenchResult, len(cfg.Queries))
	for qi, q := range cfg.Queries {
		durations := make([]time.Duration, cfg.Runs)
		var lastResults []SearchResult
		for i := 0; i < cfg.Runs; i++ {
			start := time.Now()
			res, err := engine.Search(ctx, q, cfg.TopK)
			durations[i] = time.Since(start)
			if err != nil {
				return nil, fmt.Errorf("hybrid query run %d/%d: %w", qi+1, i+1, err)
			}
			lastResults = res
		}
		ids := make([]string, len(lastResults))
		for j, r := range lastResults {
			ids[j] = r.Chunk.ID
		}
		report.HybridQueries[qi] = QueryBenchResult{
			Query:        q,
			ResultCount:  len(lastResults),
			Latency:      computeStats(durations),
			TopResultIDs: ids,
		}
	}

	report.VectorOnlyQueries = make([]QueryBenchResult, len(cfg.Queries))
	for qi, q := range cfg.Queries {
		durations := make([]time.Duration, cfg.Runs)
		var lastResults []SearchResult
		for i := 0; i < cfg.Runs; i++ {
			vec := embedder.Embed(q)
			start := time.Now()
			res, err := store.VectorSearch(ctx, vec, cfg.TopK)
			durations[i] = time.Since(start)
			if err != nil {
				return nil, fmt.Errorf("vector query run %d/%d: %w", qi+1, i+1, err)
			}
			lastResults = res
		}
		ids := make([]string, len(lastResults))
		for j, r := range lastResults {
			ids[j] = r.Chunk.ID
		}
		report.VectorOnlyQueries[qi] = QueryBenchResult{
			Query:        q,
			ResultCount:  len(lastResults),
			Latency:      computeStats(durations),
			TopResultIDs: ids,
		}
	}

	if len(cfg.EvalQueries) > 0 {
		relevance, err := EvalRelevance(ctx, engine, cfg.EvalQueries, cfg.TopK)
		if err != nil {
			return nil, fmt.Errorf("relevance eval: %w", err)
		}
		report.Relevance = relevance
	}

	store.Close()

	return report, nil
}

func WriteReport(report *BenchmarkReport, path string) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
}

func FormatReportSummary(r *BenchmarkReport) string {
	s := fmt.Sprintf("=== Benchmark Report: %s ===\n", r.Backend)
	s += fmt.Sprintf("Timestamp:        %s\n", r.Timestamp.Format(time.RFC3339))
	s += fmt.Sprintf("Runs:             %d\n", r.Runs)
	s += fmt.Sprintf("Chunks ingested:  %d\n", r.ChunksIngested)
	s += fmt.Sprintf("Ingest p50:       %s\n", r.IngestDuration.P50)
	s += fmt.Sprintf("Ingest p95:       %s\n", r.IngestDuration.P95)
	s += fmt.Sprintf("Memory (alloc):   %.2f MB\n", r.IngestMemoryMB)
	s += fmt.Sprintf("DB size:          %d bytes\n", r.DBSizeBytes)
	s += "\n--- Hybrid Search ---\n"
	for _, q := range r.HybridQueries {
		s += fmt.Sprintf("  %q: p50=%s p95=%s results=%d\n", q.Query, q.Latency.P50, q.Latency.P95, q.ResultCount)
	}
	s += "\n--- Vector-Only Search ---\n"
	for _, q := range r.VectorOnlyQueries {
		s += fmt.Sprintf("  %q: p50=%s p95=%s results=%d\n", q.Query, q.Latency.P50, q.Latency.P95, q.ResultCount)
	}
	if r.Relevance != nil {
		s += FormatRelevanceSummary(r.Relevance)
	}
	return s
}

func computeStats(durations []time.Duration) LatencyStats {
	if len(durations) == 0 {
		return LatencyStats{}
	}
	sorted := make([]time.Duration, len(durations))
	copy(sorted, durations)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	var total time.Duration
	for _, d := range sorted {
		total += d
	}
	mean := total / time.Duration(len(sorted))

	var varianceSum float64
	for _, d := range sorted {
		diff := float64(d - mean)
		varianceSum += diff * diff
	}
	stddev := time.Duration(0)
	if len(sorted) > 1 {
		stddev = time.Duration(math.Sqrt(varianceSum / float64(len(sorted)-1)))
	}

	return LatencyStats{
		Min:    sorted[0],
		Max:    sorted[len(sorted)-1],
		Mean:   mean,
		P50:    percentile(sorted, 0.50),
		P95:    percentile(sorted, 0.95),
		Stddev: stddev,
	}
}

func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := p * float64(len(sorted)-1)
	lower := int(idx)
	if lower >= len(sorted)-1 {
		return sorted[len(sorted)-1]
	}
	frac := idx - float64(lower)
	return sorted[lower] + time.Duration(frac*float64(sorted[lower+1]-sorted[lower]))
}
