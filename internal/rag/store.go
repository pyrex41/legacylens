package rag

import (
	"context"
	"strings"
)

type VectorStore interface {
	Name() string
	Init(ctx context.Context) error
	Count(ctx context.Context) (int, error)
	Upsert(ctx context.Context, chunk Chunk, vector []float32) error
	VectorSearch(ctx context.Context, query []float32, k int) ([]SearchResult, error)
	KeywordSearch(ctx context.Context, query string, k int) ([]SearchResult, error)
	HybridSearch(ctx context.Context, queryText string, queryVec []float32, k int) ([]SearchResult, error)
	Close() error
}

// EdgeStore is an optional interface for stores that support graph edges.
// Pipeline type-asserts store.(EdgeStore) after ingestion.
type EdgeStore interface {
	UpsertEdges(ctx context.Context, edges []Edge) error
	BuildEdges(ctx context.Context) error
}

func tokenize(v string) []string {
	v = strings.ToLower(v)
	repl := strings.NewReplacer(",", " ", ".", " ", "(", " ", ")", " ", "\n", " ", "\t", " ")
	v = repl.Replace(v)
	return strings.Fields(v)
}

func cosine(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot float64
	for i := range a {
		dot += float64(a[i] * b[i])
	}
	return dot
}
