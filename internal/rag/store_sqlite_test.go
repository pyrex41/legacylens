package rag

import (
	"context"
	"testing"
	"time"
)

func newTestSQLiteStore(t *testing.T, dim int) *SQLiteStore {
	t.Helper()
	s := NewProductionSQLiteStore(dim, ":memory:")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Init(ctx); err != nil {
		t.Fatalf("init sqlite store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestSQLiteStoreUpsertAndVectorSearch(t *testing.T) {
	dim := 4
	s := newTestSQLiteStore(t, dim)
	ctx := context.Background()

	embed := NewHashEmbedder(dim)
	chunks := []Chunk{
		{ID: "a", File: "a.f90", StartLine: 1, EndLine: 10, Name: "alpha", Type: ChunkTypeSubroutine,
			Parameters: []Parameter{{Name: "x", Type: "real", Intent: "in", Description: "input value"}},
			Skills:     []string{"fortran"}, Code: "subroutine alpha(x)\nend subroutine alpha"},
		{ID: "b", File: "b.f90", StartLine: 1, EndLine: 5, Name: "beta", Type: ChunkTypeFunction,
			Skills: []string{"fortran"}, Code: "function beta(y)\nend function beta"},
	}
	for _, c := range chunks {
		c.Frontmatter = BuildFrontmatter(c)
		vec := embed.Embed(c.EmbeddingText())
		if err := s.Upsert(ctx, c, vec); err != nil {
			t.Fatalf("upsert %s: %v", c.ID, err)
		}
	}

	qvec := embed.Embed("alpha subroutine")
	results, err := s.VectorSearch(ctx, qvec, 2)
	if err != nil {
		t.Fatalf("vector search: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].VectorScore <= 0 {
		t.Fatalf("expected positive vector score, got %f", results[0].VectorScore)
	}
}

func TestSQLiteStoreKeywordSearch(t *testing.T) {
	dim := 4
	s := newTestSQLiteStore(t, dim)
	ctx := context.Background()

	embed := NewHashEmbedder(dim)
	c := Chunk{
		ID: "sgemv", File: "blas.f90", StartLine: 1, EndLine: 20,
		Name: "sgemv", Type: ChunkTypeSubroutine,
		Skills:      []string{"blas", "fortran"},
		Description: "Single precision general matrix-vector multiply",
		Code:        "subroutine sgemv(trans, m, n, alpha, a, lda, x, incx, beta, y, incy)\nend subroutine sgemv",
	}
	c.Frontmatter = BuildFrontmatter(c)
	vec := embed.Embed(c.EmbeddingText())
	if err := s.Upsert(ctx, c, vec); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	results, err := s.KeywordSearch(ctx, "sgemv matrix", 5)
	if err != nil {
		t.Fatalf("keyword search: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("expected keyword results for 'sgemv matrix'")
	}
	if results[0].Chunk.ID != "sgemv" {
		t.Fatalf("expected sgemv chunk, got %s", results[0].Chunk.ID)
	}
	if results[0].KeywordScore <= 0 {
		t.Fatalf("expected positive keyword score, got %f", results[0].KeywordScore)
	}
}

func TestSQLiteStoreHybridSearch(t *testing.T) {
	dim := 4
	s := newTestSQLiteStore(t, dim)
	ctx := context.Background()

	embed := NewHashEmbedder(dim)
	chunks := []Chunk{
		{ID: "c1", File: "f1.f90", StartLine: 1, EndLine: 10, Name: "saxpy",
			Type: ChunkTypeSubroutine, Skills: []string{"blas", "fortran"},
			Description: "scalar alpha x plus y",
			Code:        "subroutine saxpy(n, sa, sx, incx, sy, incy)\nend subroutine saxpy"},
		{ID: "c2", File: "f2.f90", StartLine: 1, EndLine: 10, Name: "dgemm",
			Type: ChunkTypeSubroutine, Skills: []string{"blas", "fortran"},
			Description: "double precision general matrix multiply",
			Code:        "subroutine dgemm(transa, transb, m, n, k, alpha, a, lda, b, ldb, beta, c, ldc)\nend subroutine dgemm"},
	}
	for _, c := range chunks {
		c.Frontmatter = BuildFrontmatter(c)
		vec := embed.Embed(c.EmbeddingText())
		if err := s.Upsert(ctx, c, vec); err != nil {
			t.Fatalf("upsert %s: %v", c.ID, err)
		}
	}

	qvec := embed.Embed("saxpy scalar multiply")
	results, err := s.HybridSearch(ctx, "saxpy scalar multiply", qvec, 5)
	if err != nil {
		t.Fatalf("hybrid search: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("expected hybrid results")
	}
	if results[0].HybridScore <= 0 {
		t.Fatalf("expected positive hybrid score")
	}
}

func TestSQLiteStoreUpsertDimensionMismatch(t *testing.T) {
	s := newTestSQLiteStore(t, 4)
	ctx := context.Background()
	c := Chunk{ID: "x", Name: "x", File: "x.f90", StartLine: 1, EndLine: 1, Type: ChunkTypeUnknown}
	err := s.Upsert(ctx, c, []float32{1, 2})
	if err == nil {
		t.Fatal("expected dimension mismatch error")
	}
}

func TestSQLiteStoreUpsertIdempotent(t *testing.T) {
	dim := 4
	s := newTestSQLiteStore(t, dim)
	ctx := context.Background()

	embed := NewHashEmbedder(dim)
	c := Chunk{
		ID: "dup", File: "d.f90", StartLine: 1, EndLine: 5, Name: "dup",
		Type:   ChunkTypeFunction,
		Skills: []string{"fortran"},
		Parameters: []Parameter{
			{Name: "a", Type: "real", Intent: "in"},
		},
		Code: "function dup()\nend function dup",
	}
	c.Frontmatter = BuildFrontmatter(c)
	vec := embed.Embed(c.EmbeddingText())

	if err := s.Upsert(ctx, c, vec); err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	c.Description = "updated description"
	c.Parameters = []Parameter{
		{Name: "a", Type: "real", Intent: "in"},
		{Name: "b", Type: "integer", Intent: "out"},
	}
	c.Frontmatter = BuildFrontmatter(c)
	if err := s.Upsert(ctx, c, vec); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	results, err := s.VectorSearch(ctx, vec, 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result after idempotent upsert, got %d", len(results))
	}
	if results[0].Chunk.Description != "updated description" {
		t.Fatalf("expected updated description, got %q", results[0].Chunk.Description)
	}
	if len(results[0].Chunk.Parameters) != 2 {
		t.Fatalf("expected 2 params after update, got %d", len(results[0].Chunk.Parameters))
	}
}

func TestSQLiteStoreFTSQueryBuilder(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"sgemv matrix", "sgemv* OR matrix*"},
		{"", ""},
		{"hello, world!", "hello* OR world*"},
	}
	for _, tt := range tests {
		got := buildFTSQuery(tt.in)
		if got != tt.want {
			t.Errorf("buildFTSQuery(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSQLiteStoreInitIdempotent(t *testing.T) {
	s := NewProductionSQLiteStore(4, ":memory:")
	ctx := context.Background()
	if err := s.Init(ctx); err != nil {
		t.Fatalf("first init: %v", err)
	}
	if err := s.Init(ctx); err != nil {
		t.Fatalf("second init: %v", err)
	}
	s.Close()
}
