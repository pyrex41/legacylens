package rag

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const benchFortranFixture = `module m_blas_bench
contains
subroutine sgemv(trans, m, n, alpha, a, lda, x, incx, beta, y, incy)
  character, intent(in) :: trans
  integer, intent(in) :: m, n, lda, incx, incy
  real, intent(in) :: alpha, beta
  real, intent(in) :: a(lda,*), x(*)
  real, intent(inout) :: y(*)
  integer :: i, j
  real :: temp
  do j = 1, n
    temp = alpha * x((j-1)*incx + 1)
    do i = 1, m
      y((i-1)*incy + 1) = y((i-1)*incy + 1) + temp * a(i,j)
    end do
  end do
end subroutine sgemv

subroutine dgemm(transa, transb, m, n, k, alpha, a, lda, b, ldb, beta, c, ldc)
  character, intent(in) :: transa, transb
  integer, intent(in) :: m, n, k, lda, ldb, ldc
  double precision, intent(in) :: alpha, beta
  double precision, intent(in) :: a(lda,*), b(ldb,*)
  double precision, intent(inout) :: c(ldc,*)
  integer :: i, j, l
  double precision :: temp
  do j = 1, n
    do l = 1, k
      temp = alpha * b(l,j)
      do i = 1, m
        c(i,j) = c(i,j) + temp * a(i,l)
      end do
    end do
  end do
end subroutine dgemm

subroutine dtrsv(uplo, trans, diag, n, a, lda, x, incx)
  character, intent(in) :: uplo, trans, diag
  integer, intent(in) :: n, lda, incx
  double precision, intent(in) :: a(lda,*)
  double precision, intent(inout) :: x(*)
  integer :: i, j
  do j = n, 1, -1
    if (x(j) /= 0.0d0) then
      do i = j - 1, 1, -1
        x(i) = x(i) - a(i,j) * x(j)
      end do
    end if
  end do
end subroutine dtrsv
end module m_blas_bench
`

func setupBenchRepo(t testing.TB) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "blas.f90"), []byte(benchFortranFixture), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return root
}

func TestRunBenchmarkSQLite(t *testing.T) {
	repo := setupBenchRepo(t)
	embedder := NewHashEmbedder(64)
	dbPath := filepath.Join(t.TempDir(), "bench_sqlite.db")

	cfg := BenchmarkConfig{
		Runs:     3,
		TopK:     3,
		Queries:  []string{"sgemv", "matrix multiply", "triangular solve"},
		RepoPath: repo,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	report, err := RunBenchmark(ctx, cfg, embedder, func() (VectorStore, string) {
		return NewProductionSQLiteStore(embedder.Dimension(), dbPath), dbPath
	})
	if err != nil {
		t.Fatalf("benchmark failed: %v", err)
	}

	if report.Backend != "sqlite" {
		t.Errorf("expected backend sqlite, got %s", report.Backend)
	}
	if report.ChunksIngested == 0 {
		t.Error("expected chunks > 0")
	}
	if report.IngestDuration.P50 == 0 {
		t.Error("expected non-zero ingest p50")
	}
	if len(report.HybridQueries) != 3 {
		t.Errorf("expected 3 hybrid queries, got %d", len(report.HybridQueries))
	}
	if len(report.VectorOnlyQueries) != 3 {
		t.Errorf("expected 3 vector queries, got %d", len(report.VectorOnlyQueries))
	}
	for _, q := range report.HybridQueries {
		if q.ResultCount == 0 {
			t.Errorf("hybrid query %q returned 0 results", q.Query)
		}
	}

	reportPath := filepath.Join(t.TempDir(), "report_sqlite.json")
	if err := WriteReport(report, reportPath); err != nil {
		t.Fatalf("write report: %v", err)
	}
	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	if len(data) < 10 {
		t.Error("report file too small")
	}

	summary := FormatReportSummary(report)
	if summary == "" {
		t.Error("expected non-empty summary")
	}
}

func TestRunBenchmarkCozo(t *testing.T) {
	repo := setupBenchRepo(t)
	embedder := NewHashEmbedder(64)
	dbPath := filepath.Join(t.TempDir(), "bench_cozo.db")

	cfg := BenchmarkConfig{
		Runs:     3,
		TopK:     3,
		Queries:  []string{"sgemv", "matrix multiply"},
		RepoPath: repo,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	report, err := RunBenchmark(ctx, cfg, embedder, func() (VectorStore, string) {
		return NewCozoStore(embedder.Dimension(), dbPath), dbPath
	})
	if err != nil {
		t.Fatalf("benchmark failed: %v", err)
	}

	if report.Backend != "cozo" {
		t.Errorf("expected backend cozo, got %s", report.Backend)
	}
	if report.ChunksIngested == 0 {
		t.Error("expected chunks > 0")
	}
	if report.DBSizeBytes == 0 {
		t.Error("expected non-zero DB size")
	}
}

func TestComputeStats(t *testing.T) {
	durations := []time.Duration{
		10 * time.Millisecond,
		20 * time.Millisecond,
		30 * time.Millisecond,
		40 * time.Millisecond,
		50 * time.Millisecond,
	}
	stats := computeStats(durations)
	if stats.Min != 10*time.Millisecond {
		t.Errorf("min: got %v, want 10ms", stats.Min)
	}
	if stats.Max != 50*time.Millisecond {
		t.Errorf("max: got %v, want 50ms", stats.Max)
	}
	if stats.P50 != 30*time.Millisecond {
		t.Errorf("p50: got %v, want 30ms", stats.P50)
	}
	if stats.Mean != 30*time.Millisecond {
		t.Errorf("mean: got %v, want 30ms", stats.Mean)
	}
}

func TestComputeStatsEmpty(t *testing.T) {
	stats := computeStats(nil)
	if stats.Min != 0 || stats.Max != 0 || stats.P50 != 0 {
		t.Errorf("expected zero stats for empty input, got %+v", stats)
	}
}

func BenchmarkIngestSQLite(b *testing.B) {
	repo := setupBenchRepo(b)
	embedder := NewHashEmbedder(64)

	for i := 0; i < b.N; i++ {
		dbPath := filepath.Join(b.TempDir(), "bench.db")
		store := NewProductionSQLiteStore(embedder.Dimension(), dbPath)
		p := NewPipeline(DefaultPipelineConfig(), embedder, store)
		ctx := context.Background()
		if _, err := p.IngestRepo(ctx, repo); err != nil {
			b.Fatalf("ingest: %v", err)
		}
		store.Close()
	}
}

func BenchmarkIngestCozo(b *testing.B) {
	repo := setupBenchRepo(b)
	embedder := NewHashEmbedder(64)

	for i := 0; i < b.N; i++ {
		dbPath := filepath.Join(b.TempDir(), "bench.db")
		store := NewCozoStore(embedder.Dimension(), dbPath)
		p := NewPipeline(DefaultPipelineConfig(), embedder, store)
		ctx := context.Background()
		if _, err := p.IngestRepo(ctx, repo); err != nil {
			b.Fatalf("ingest: %v", err)
		}
		store.Close()
	}
}

func BenchmarkHybridSearchSQLite(b *testing.B) {
	repo := setupBenchRepo(b)
	embedder := NewHashEmbedder(64)
	store := NewProductionSQLiteStore(embedder.Dimension(), ":memory:")
	p := NewPipeline(DefaultPipelineConfig(), embedder, store)
	ctx := context.Background()
	if _, err := p.IngestRepo(ctx, repo); err != nil {
		b.Fatalf("ingest: %v", err)
	}
	engine := NewQueryEngine(store, embedder)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := engine.Search(ctx, "sgemv matrix vector", 5); err != nil {
			b.Fatalf("search: %v", err)
		}
	}
	store.Close()
}

func BenchmarkHybridSearchCozo(b *testing.B) {
	repo := setupBenchRepo(b)
	embedder := NewHashEmbedder(64)
	store := NewCozoStore(embedder.Dimension(), ":memory:")
	p := NewPipeline(DefaultPipelineConfig(), embedder, store)
	ctx := context.Background()
	if _, err := p.IngestRepo(ctx, repo); err != nil {
		b.Fatalf("ingest: %v", err)
	}
	engine := NewQueryEngine(store, embedder)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := engine.Search(ctx, "sgemv matrix vector", 5); err != nil {
			b.Fatalf("search: %v", err)
		}
	}
	store.Close()
}
