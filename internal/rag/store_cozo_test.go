package rag

import (
	"context"
	"testing"
	"time"
)

func newTestCozoStore(t *testing.T, dim int) *CozoStore {
	t.Helper()
	s := NewCozoStore(dim, "")  // in-memory
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Init(ctx); err != nil {
		t.Fatalf("init cozo store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestCozoStoreUpsertAndVectorSearch(t *testing.T) {
	dim := 4
	s := newTestCozoStore(t, dim)
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

func TestCozoStoreKeywordSearch(t *testing.T) {
	dim := 4
	s := newTestCozoStore(t, dim)
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

func TestCozoStoreHybridSearch(t *testing.T) {
	dim := 4
	s := newTestCozoStore(t, dim)
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

func TestCozoStoreUpsertDimensionMismatch(t *testing.T) {
	s := newTestCozoStore(t, 4)
	ctx := context.Background()
	c := Chunk{ID: "x", Name: "x", File: "x.f90", StartLine: 1, EndLine: 1, Type: ChunkTypeUnknown}
	err := s.Upsert(ctx, c, []float32{1, 2})
	if err == nil {
		t.Fatal("expected dimension mismatch error")
	}
}

func TestCozoStoreUpsertIdempotent(t *testing.T) {
	dim := 4
	s := newTestCozoStore(t, dim)
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

func TestCozoStoreNormalizedMetadata(t *testing.T) {
	dim := 4
	s := newTestCozoStore(t, dim)
	ctx := context.Background()

	embed := NewHashEmbedder(dim)
	c := Chunk{
		ID: "meta", File: "m.f90", StartLine: 1, EndLine: 10, Name: "meta_sub",
		Type: ChunkTypeSubroutine,
		Parameters: []Parameter{
			{Name: "x", Type: "real", Intent: "in", Description: "input"},
			{Name: "y", Type: "real", Intent: "out", Description: "output"},
		},
		Skills:      []string{"blas", "fortran", "linear-algebra"},
		Description: "test normalized metadata",
		Code:        "subroutine meta_sub(x, y)\nend subroutine meta_sub",
	}
	c.Frontmatter = BuildFrontmatter(c)
	vec := embed.Embed(c.EmbeddingText())
	if err := s.Upsert(ctx, c, vec); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	results, err := s.VectorSearch(ctx, vec, 1)
	if err != nil {
		t.Fatalf("vector search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	got := results[0].Chunk
	if len(got.Parameters) != 2 {
		t.Fatalf("expected 2 params, got %d", len(got.Parameters))
	}
	if got.Parameters[0].Name != "x" || got.Parameters[1].Name != "y" {
		t.Fatalf("param names mismatch: %v", got.Parameters)
	}
	if got.Parameters[0].Intent != "in" || got.Parameters[1].Intent != "out" {
		t.Fatalf("param intents mismatch: %v", got.Parameters)
	}
	if len(got.Skills) != 3 {
		t.Fatalf("expected 3 skills, got %d: %v", len(got.Skills), got.Skills)
	}
}

func TestCozoStoreEdgeBuild(t *testing.T) {
	dim := 4
	s := newTestCozoStore(t, dim)
	ctx := context.Background()
	embed := NewHashEmbedder(dim)

	// Module containing a subroutine, and the subroutine calls xerbla
	chunks := []Chunk{
		{ID: "mod1", File: "a.f90", StartLine: 1, EndLine: 50, Name: "blas_mod",
			Type: ChunkTypeModule, Code: "module blas_mod\ncontains\nend module blas_mod"},
		{ID: "sub1", File: "a.f90", StartLine: 5, EndLine: 30, Name: "sgemv",
			Type: ChunkTypeSubroutine, Code: "subroutine sgemv()\ncall xerbla('SGEMV', info)\nend subroutine"},
		{ID: "sub2", File: "b.f90", StartLine: 1, EndLine: 20, Name: "xerbla",
			Type: ChunkTypeSubroutine, Code: "subroutine xerbla(srname, info)\nend subroutine xerbla"},
	}
	for _, c := range chunks {
		c.Frontmatter = BuildFrontmatter(c)
		vec := embed.Embed(c.EmbeddingText())
		if err := s.Upsert(ctx, c, vec); err != nil {
			t.Fatalf("upsert %s: %v", c.ID, err)
		}
	}

	if err := s.BuildEdges(ctx); err != nil {
		t.Fatalf("build edges: %v", err)
	}

	// Verify edges were created
	res, err := s.db.Run(`?[src, dst, rel] := *edges[src, dst, rel]`, nil)
	if err != nil {
		t.Fatalf("query edges: %v", err)
	}
	if len(res.Rows) == 0 {
		t.Fatal("expected edges to be created, got none")
	}

	// Check for containment edge: mod1 → sub1
	foundContains := false
	foundCalls := false
	for _, row := range res.Rows {
		src := toString(row[0])
		dst := toString(row[1])
		rel := toString(row[2])
		if src == "mod1" && dst == "sub1" && rel == "contains" {
			foundContains = true
		}
		if src == "sub1" && dst == "sub2" && rel == "calls" {
			foundCalls = true
		}
	}
	if !foundContains {
		t.Error("expected containment edge mod1→sub1")
	}
	if !foundCalls {
		t.Error("expected call edge sub1→sub2 (sgemv calls xerbla)")
	}
}

func TestCozoStoreHybridSearchWithEdges(t *testing.T) {
	dim := 4
	s := newTestCozoStore(t, dim)
	ctx := context.Background()
	embed := NewHashEmbedder(dim)

	// sgemv calls xerbla; searching for "sgemv" should also surface xerbla via graph
	chunks := []Chunk{
		{ID: "sgemv", File: "a.f90", StartLine: 1, EndLine: 30, Name: "sgemv",
			Type: ChunkTypeSubroutine, Skills: []string{"blas", "fortran"},
			Description: "single precision matrix vector multiply",
			Code:        "subroutine sgemv()\ncall xerbla('SGEMV', info)\nend subroutine"},
		{ID: "xerbla", File: "b.f90", StartLine: 1, EndLine: 20, Name: "xerbla",
			Type: ChunkTypeSubroutine, Skills: []string{"blas", "error-handling"},
			Description: "error handler for BLAS routines",
			Code:        "subroutine xerbla(srname, info)\nend subroutine xerbla"},
		{ID: "dgemm", File: "c.f90", StartLine: 1, EndLine: 40, Name: "dgemm",
			Type: ChunkTypeSubroutine, Skills: []string{"blas", "fortran"},
			Description: "double precision matrix multiply",
			Code:        "subroutine dgemm()\nend subroutine dgemm"},
	}
	for _, c := range chunks {
		c.Frontmatter = BuildFrontmatter(c)
		vec := embed.Embed(c.EmbeddingText())
		if err := s.Upsert(ctx, c, vec); err != nil {
			t.Fatalf("upsert %s: %v", c.ID, err)
		}
	}

	if err := s.BuildEdges(ctx); err != nil {
		t.Fatalf("build edges: %v", err)
	}

	qvec := embed.Embed("sgemv matrix vector")
	results, err := s.HybridSearch(ctx, "sgemv matrix vector", qvec, 5)
	if err != nil {
		t.Fatalf("hybrid search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected hybrid results")
	}
	if results[0].HybridScore <= 0 {
		t.Fatal("expected positive hybrid score")
	}

	// Verify xerbla appears in results (graph expansion from sgemv→xerbla edge)
	foundXerbla := false
	for _, r := range results {
		if r.Chunk.ID == "xerbla" {
			foundXerbla = true
			break
		}
	}
	if !foundXerbla {
		ids := make([]string, len(results))
		for i, r := range results {
			ids[i] = r.Chunk.ID
		}
		t.Errorf("expected xerbla in results via graph expansion, got: %v", ids)
	}
}

func TestCozoStoreInitIdempotent(t *testing.T) {
	s := NewCozoStore(4, "")
	ctx := context.Background()
	if err := s.Init(ctx); err != nil {
		t.Fatalf("first init: %v", err)
	}
	if err := s.Init(ctx); err != nil {
		t.Fatalf("second init: %v", err)
	}
	s.Close()
}
