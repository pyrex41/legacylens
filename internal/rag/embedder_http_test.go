package rag

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPEmbedderSuccess(t *testing.T) {
	dim := 4
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("expected application/json, got %s", ct)
		}

		var req embeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if req.Input == "" {
			t.Error("expected non-empty input")
		}

		resp := embeddingResponse{
			Data: []struct {
				Embedding []float64 `json:"embedding"`
			}{
				{Embedding: []float64{0.1, 0.2, 0.3, 0.4}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	e := NewHTTPEmbedder(HTTPEmbedderConfig{
		Endpoint: server.URL,
		Model:    "test-model",
		Dim:      dim,
	})

	if e.Dimension() != dim {
		t.Fatalf("expected dim %d, got %d", dim, e.Dimension())
	}

	vec := e.Embed("hello world")
	if len(vec) != dim {
		t.Fatalf("expected %d dims, got %d", dim, len(vec))
	}

	var nonZero bool
	for _, v := range vec {
		if v != 0 {
			nonZero = true
			break
		}
	}
	if !nonZero {
		t.Fatal("expected non-zero embedding")
	}
}

func TestHTTPEmbedderServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer server.Close()

	e := NewHTTPEmbedder(HTTPEmbedderConfig{
		Endpoint: server.URL,
		Dim:      4,
	})

	vec := e.Embed("test")
	if len(vec) != 4 {
		t.Fatalf("expected 4 dims, got %d", len(vec))
	}
	for _, v := range vec {
		if v != 0 {
			t.Fatalf("expected zero vector on error, got non-zero")
		}
	}
}

func TestHTTPEmbedderDimensionMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := embeddingResponse{
			Data: []struct {
				Embedding []float64 `json:"embedding"`
			}{
				{Embedding: []float64{0.1, 0.2}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	e := NewHTTPEmbedder(HTTPEmbedderConfig{
		Endpoint: server.URL,
		Dim:      4,
	})

	vec := e.Embed("test")
	for _, v := range vec {
		if v != 0 {
			t.Fatalf("expected zero vector on dim mismatch, got non-zero")
		}
	}
}

func TestHTTPEmbedderWithAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-key-123" {
			t.Errorf("expected Bearer test-key-123, got %s", auth)
		}
		resp := embeddingResponse{
			Data: []struct {
				Embedding []float64 `json:"embedding"`
			}{
				{Embedding: []float64{0.5, 0.5, 0.5, 0.5}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	e := NewHTTPEmbedder(HTTPEmbedderConfig{
		Endpoint: server.URL,
		Model:    "test",
		Dim:      4,
		APIKey:   "test-key-123",
	})

	vec := e.Embed("test")
	if len(vec) != 4 {
		t.Fatalf("expected 4 dims, got %d", len(vec))
	}
}
