package rag

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCachedEmbedderDeterministic(t *testing.T) {
	dir := t.TempDir()
	inner := NewHashEmbedder(8)
	cached, err := NewCachedEmbedder(inner, dir)
	if err != nil {
		t.Fatalf("new cached embedder: %v", err)
	}

	v1 := cached.Embed("hello world")
	v2 := cached.Embed("hello world")

	if len(v1) != 8 || len(v2) != 8 {
		t.Fatalf("expected dim 8, got %d and %d", len(v1), len(v2))
	}
	for i := range v1 {
		if v1[i] != v2[i] {
			t.Fatalf("non-deterministic at index %d: %f != %f", i, v1[i], v2[i])
		}
	}
}

func TestCachedEmbedderPersistence(t *testing.T) {
	dir := t.TempDir()
	inner := NewHashEmbedder(8)

	cached1, err := NewCachedEmbedder(inner, dir)
	if err != nil {
		t.Fatalf("new cached embedder: %v", err)
	}
	v1 := cached1.Embed("test persistence")

	key := cacheKey("test persistence")
	cachePath := filepath.Join(dir, key+".bin")
	if _, err := os.Stat(cachePath); os.IsNotExist(err) {
		t.Fatalf("cache file not created at %s", cachePath)
	}

	cached2, err := NewCachedEmbedder(inner, dir)
	if err != nil {
		t.Fatalf("new cached embedder 2: %v", err)
	}
	v2 := cached2.Embed("test persistence")

	for i := range v1 {
		if v1[i] != v2[i] {
			t.Fatalf("cache miss at index %d: %f != %f", i, v1[i], v2[i])
		}
	}
}

func TestCachedEmbedderCorruptCache(t *testing.T) {
	dir := t.TempDir()
	inner := NewHashEmbedder(8)
	cached, err := NewCachedEmbedder(inner, dir)
	if err != nil {
		t.Fatalf("new cached embedder: %v", err)
	}

	key := cacheKey("corrupt test")
	cachePath := filepath.Join(dir, key+".bin")
	if err := os.WriteFile(cachePath, []byte("bad"), 0o644); err != nil {
		t.Fatalf("write corrupt cache: %v", err)
	}

	vec := cached.Embed("corrupt test")
	if len(vec) != 8 {
		t.Fatalf("expected dim 8, got %d", len(vec))
	}
	expected := inner.Embed("corrupt test")
	for i := range vec {
		if vec[i] != expected[i] {
			t.Fatalf("corrupt cache not bypassed at index %d", i)
		}
	}
}

func TestCachedEmbedderDimension(t *testing.T) {
	inner := NewHashEmbedder(16)
	cached, err := NewCachedEmbedder(inner, t.TempDir())
	if err != nil {
		t.Fatalf("new cached embedder: %v", err)
	}
	if cached.Dimension() != 16 {
		t.Fatalf("expected dim 16, got %d", cached.Dimension())
	}
}
