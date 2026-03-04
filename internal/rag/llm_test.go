package rag

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGrokClientComplete(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-key" {
			t.Errorf("expected Bearer test-key, got %s", auth)
		}

		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Model != "test-model" {
			t.Errorf("expected model test-model, got %s", req.Model)
		}
		if len(req.Messages) != 2 {
			t.Fatalf("expected 2 messages, got %d", len(req.Messages))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(chatResponse{
			Choices: []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			}{
				{Message: struct {
					Content string `json:"content"`
				}{Content: "SGEMV performs matrix-vector multiplication."}},
			},
		})
	}))
	defer ts.Close()

	client := NewGrokClient(ts.URL, "test-key", "test-model")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	answer, err := client.Complete(ctx, "How does sgemv work?")
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if answer != "SGEMV performs matrix-vector multiplication." {
		t.Errorf("unexpected answer: %s", answer)
	}
}

func TestGrokClientErrorResponse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
	}))
	defer ts.Close()

	client := NewGrokClient(ts.URL, "bad-key", "test-model")
	ctx := context.Background()

	_, err := client.Complete(ctx, "test")
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
}

func TestExplainWithLLM(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(chatResponse{
			Choices: []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			}{
				{Message: struct {
					Content string `json:"content"`
				}{Content: "LLM-generated answer about sgemv."}},
			},
		})
	}))
	defer ts.Close()

	dim := 32
	store := NewProductionSQLiteStore(dim, ":memory:")
	embedder := NewHashEmbedder(dim)
	ctx := context.Background()
	if err := store.Init(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}
	defer store.Close()

	c := Chunk{
		ID: "sgemv", File: "blas.f90", StartLine: 1, EndLine: 20,
		Name: "sgemv", Type: ChunkTypeSubroutine,
		Code: "subroutine sgemv()\nend subroutine sgemv",
	}
	c.Frontmatter = BuildFrontmatter(c)
	vec := embedder.Embed(c.EmbeddingText())
	if err := store.Upsert(ctx, c, vec); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	llm := NewGrokClient(ts.URL, "test-key", "test-model")
	engine := NewQueryEngine(store, embedder).WithLLM(llm)

	result, err := engine.Explain(ctx, "How does sgemv work?", 5)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	if result.Answer != "LLM-generated answer about sgemv." {
		t.Errorf("expected LLM answer, got: %s", result.Answer)
	}
}
