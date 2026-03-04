package rag

import "testing"

func TestFuseRRFReturnsExpectedOrder(t *testing.T) {
	a := Chunk{ID: "a", Name: "a"}
	b := Chunk{ID: "b", Name: "b"}
	c := Chunk{ID: "c", Name: "c"}

	vec := []SearchResult{
		{Chunk: a, VectorScore: 0.9},
		{Chunk: b, VectorScore: 0.8},
	}
	key := []SearchResult{
		{Chunk: b, KeywordScore: 2},
		{Chunk: c, KeywordScore: 1},
	}

	out := FuseRRF(vec, key, 3)
	if len(out) != 3 {
		t.Fatalf("expected 3 results, got %d", len(out))
	}
	if out[0].Chunk.ID != "b" {
		t.Fatalf("expected top result b, got %s", out[0].Chunk.ID)
	}
}
