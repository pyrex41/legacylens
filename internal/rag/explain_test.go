package rag

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestExplainReturnsStructuredAnswer(t *testing.T) {
	dim := 32
	dbPath := filepath.Join(t.TempDir(), "explain_test.db")
	store := NewProductionSQLiteStore(dim, dbPath)
	embedder := NewHashEmbedder(dim)

	ctx := context.Background()
	if err := store.Init(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}

	src := `      subroutine sgemv(trans, m, n, alpha, a, lda, x, incx, beta, y, incy)
      implicit none
      character :: trans
      integer, intent(in) :: m, n, lda, incx, incy
      real, intent(in) :: alpha, beta
      real, intent(in) :: a(lda,*), x(*)
      real, intent(inout) :: y(*)
      end subroutine sgemv`

	chunker := NewFortranChunker(180)
	chunks := chunker.ChunkFile("test/sgemv.f90", src)
	for _, c := range chunks {
		vec := embedder.Embed(c.EmbeddingText())
		if err := store.Upsert(ctx, c, vec); err != nil {
			t.Fatalf("upsert: %v", err)
		}
	}

	engine := NewQueryEngine(store, embedder)
	result, err := engine.Explain(ctx, "How does sgemv work?", 5)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}

	if result.Query != "How does sgemv work?" {
		t.Errorf("query mismatch: %s", result.Query)
	}
	if result.Answer == "" {
		t.Error("answer is empty")
	}
	if len(result.RawResults) == 0 {
		t.Error("no raw results")
	}
	if len(result.Citations) == 0 {
		t.Error("no citations")
	}
	if len(result.Symbols) == 0 {
		t.Error("no symbols")
	}

	sym := result.Symbols[0]
	if sym.Name != "sgemv" {
		t.Errorf("expected symbol sgemv, got %s", sym.Name)
	}
	if sym.Frontmatter == "" {
		t.Error("symbol frontmatter is empty")
	}
	if !strings.Contains(sym.Frontmatter, "sgemv") {
		t.Error("frontmatter should contain symbol name")
	}
	if sym.Explanation == "" {
		t.Error("symbol explanation is empty")
	}
}

func TestExplainNoResults(t *testing.T) {
	dim := 32
	dbPath := filepath.Join(t.TempDir(), "explain_empty.db")
	store := NewProductionSQLiteStore(dim, dbPath)
	embedder := NewHashEmbedder(dim)

	ctx := context.Background()
	if err := store.Init(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}

	engine := NewQueryEngine(store, embedder)
	result, err := engine.Explain(ctx, "nonexistent query", 5)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}

	if result.Query != "nonexistent query" {
		t.Errorf("query mismatch: %s", result.Query)
	}
	if !strings.Contains(result.Answer, "No relevant code") {
		t.Errorf("expected no-results message, got: %s", result.Answer)
	}
	if len(result.Citations) != 0 {
		t.Error("expected no citations for empty store")
	}
	if len(result.Symbols) != 0 {
		t.Error("expected no symbols for empty store")
	}
}

func TestExplainCitationsAreDeduplicated(t *testing.T) {
	c := Chunk{
		ID: "a", File: "test.f90", StartLine: 1, EndLine: 10,
		Name: "foo", Type: ChunkTypeSubroutine,
	}
	results := []SearchResult{
		{Chunk: c, VectorScore: 0.9, HybridScore: 0.5},
		{Chunk: c, VectorScore: 0.8, HybridScore: 0.4},
	}
	citations := buildCitations(results)
	if len(citations) != 1 {
		t.Errorf("expected 1 deduplicated citation, got %d", len(citations))
	}
}

func TestExplainSymbolsAreDeduplicated(t *testing.T) {
	c := Chunk{
		ID: "a", File: "test.f90", StartLine: 1, EndLine: 10,
		Name: "foo", Type: ChunkTypeSubroutine,
		Frontmatter: "---\ntitle: foo\n---",
	}
	results := []SearchResult{
		{Chunk: c, VectorScore: 0.9},
		{Chunk: c, VectorScore: 0.8},
	}
	symbols := buildSymbolExplanations(results)
	if len(symbols) != 1 {
		t.Errorf("expected 1 deduplicated symbol, got %d", len(symbols))
	}
}

func TestDescribeChunkWithParams(t *testing.T) {
	c := Chunk{
		Name: "sgemv",
		Type: ChunkTypeSubroutine,
		File: "sgemv.f90",
		StartLine: 1, EndLine: 50,
		Parameters: []Parameter{
			{Name: "m", Type: "integer", Intent: "in"},
			{Name: "alpha", Type: "real", Intent: "in"},
		},
		Skills: []string{"fortran", "blas", "subroutine"},
	}
	desc := describeChunk(c)
	if !strings.Contains(desc, "sgemv") {
		t.Error("description should contain symbol name")
	}
	if !strings.Contains(desc, "m (integer) [in]") {
		t.Errorf("description should contain typed param, got: %s", desc)
	}
	if !strings.Contains(desc, "alpha (real) [in]") {
		t.Errorf("description should contain alpha param, got: %s", desc)
	}
	if !strings.Contains(desc, "Skills:") {
		t.Error("description should contain skills")
	}
}

func TestSynthesizeAnswerContainsCitations(t *testing.T) {
	c := Chunk{
		ID: "a", File: "test.f90", StartLine: 1, EndLine: 10,
		Name: "foo", Type: ChunkTypeSubroutine,
	}
	results := []SearchResult{{Chunk: c, HybridScore: 0.5}}
	citations := buildCitations(results)
	answer := synthesizeAnswer("test query", results, citations)

	if !strings.Contains(answer, "test query") {
		t.Error("answer should contain query")
	}
	if !strings.Contains(answer, "Citations") {
		t.Error("answer should contain citations section")
	}
	if !strings.Contains(answer, "foo") {
		t.Error("answer should contain symbol name")
	}
}

func TestCapitalize(t *testing.T) {
	tests := []struct{ in, want string }{
		{"subroutine", "Subroutine"},
		{"function", "Function"},
		{"", ""},
		{"A", "A"},
	}
	for _, tt := range tests {
		if got := capitalize(tt.in); got != tt.want {
			t.Errorf("capitalize(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
