package rag

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// CachedEmbedder wraps any Embedder with a SHA256-keyed file-based cache.
// Cache entries are stored as raw little-endian float32 BLOBs keyed by the
// SHA256 hash of the input text. This guarantees deterministic, reproducible
// embeddings across runs.
type CachedEmbedder struct {
	inner    Embedder
	cacheDir string
	mu       sync.Mutex
}

func NewCachedEmbedder(inner Embedder, cacheDir string) (*CachedEmbedder, error) {
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, fmt.Errorf("create cache dir: %w", err)
	}
	return &CachedEmbedder{inner: inner, cacheDir: cacheDir}, nil
}

func (c *CachedEmbedder) Dimension() int {
	return c.inner.Dimension()
}

func (c *CachedEmbedder) Embed(text string) []float32 {
	key := cacheKey(text)
	path := filepath.Join(c.cacheDir, key+".bin")

	if vec, err := c.loadCached(path); err == nil {
		return vec
	}

	vec := c.inner.Embed(text)

	c.mu.Lock()
	defer c.mu.Unlock()
	_ = os.WriteFile(path, encodeVector(vec), 0o644)
	return vec
}

func (c *CachedEmbedder) loadCached(path string) ([]float32, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	dim := c.inner.Dimension()
	if len(data) != dim*4 {
		return nil, fmt.Errorf("cache entry size mismatch: got %d bytes, want %d", len(data), dim*4)
	}
	return decodeVector(data), nil
}

func cacheKey(text string) string {
	h := sha256.Sum256([]byte(text))
	return hex.EncodeToString(h[:])
}
