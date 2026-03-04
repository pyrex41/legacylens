package rag

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
)

type VectorStore interface {
	Name() string
	Init(ctx context.Context) error
	Upsert(ctx context.Context, chunk Chunk, vector []float32) error
	VectorSearch(ctx context.Context, query []float32, k int) ([]SearchResult, error)
	KeywordSearch(ctx context.Context, query string, k int) ([]SearchResult, error)
	HybridSearch(ctx context.Context, queryText string, queryVec []float32, k int) ([]SearchResult, error)
}

type InMemoryStore struct {
	name   string
	dim    int
	mu     sync.RWMutex
	chunks map[string]Chunk
	vecs   map[string][]float32
}

func NewSQLiteStore(dim int) *InMemoryStore {
	return &InMemoryStore{
		name:   "sqlite",
		dim:    dim,
		chunks: map[string]Chunk{},
		vecs:   map[string][]float32{},
	}
}

func NewCozoStore(dim int) *InMemoryStore {
	return &InMemoryStore{
		name:   "cozo",
		dim:    dim,
		chunks: map[string]Chunk{},
		vecs:   map[string][]float32{},
	}
}

func (s *InMemoryStore) Name() string { return s.name }

func (s *InMemoryStore) Init(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func (s *InMemoryStore) Upsert(ctx context.Context, chunk Chunk, vector []float32) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if len(vector) != s.dim {
		return errors.New("embedding dimension mismatch")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.chunks[chunk.ID] = chunk
	cp := make([]float32, len(vector))
	copy(cp, vector)
	s.vecs[chunk.ID] = cp
	return nil
}

func (s *InMemoryStore) VectorSearch(ctx context.Context, query []float32, k int) ([]SearchResult, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	if len(query) != s.dim {
		return nil, errors.New("query embedding dimension mismatch")
	}
	if k <= 0 {
		k = 5
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]SearchResult, 0, len(s.chunks))
	for id, ch := range s.chunks {
		vec := s.vecs[id]
		out = append(out, SearchResult{
			Chunk:       ch,
			VectorScore: cosine(query, vec),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].VectorScore > out[j].VectorScore })
	if len(out) > k {
		out = out[:k]
	}
	return out, nil
}

func (s *InMemoryStore) KeywordSearch(ctx context.Context, query string, k int) ([]SearchResult, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	if k <= 0 {
		k = 5
	}
	terms := tokenize(query)
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]SearchResult, 0, len(s.chunks))
	for _, ch := range s.chunks {
		text := strings.ToLower(ch.EmbeddingText())
		var score float64
		for _, t := range terms {
			if t == "" {
				continue
			}
			if strings.Contains(text, t) {
				score += 1.0
			}
		}
		if score > 0 {
			out = append(out, SearchResult{
				Chunk:        ch,
				KeywordScore: score,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].KeywordScore > out[j].KeywordScore })
	if len(out) > k {
		out = out[:k]
	}
	return out, nil
}

func (s *InMemoryStore) HybridSearch(ctx context.Context, queryText string, queryVec []float32, k int) ([]SearchResult, error) {
	vec, err := s.VectorSearch(ctx, queryVec, k*3)
	if err != nil {
		return nil, err
	}
	key, err := s.KeywordSearch(ctx, queryText, k*3)
	if err != nil {
		return nil, err
	}
	return FuseRRF(vec, key, k), nil
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
