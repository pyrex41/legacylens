package rag

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// HTTPEmbedder calls a remote embedding service that exposes an
// OpenAI-compatible /v1/embeddings endpoint. This is compatible with:
//   - OpenAI API
//   - Local sentence-transformers server (e.g. `python -m sentence_transformers.server`)
//   - TEI (Text Embeddings Inference by HuggingFace)
//   - Any OpenAI-compatible embedding proxy
type HTTPEmbedder struct {
	endpoint string
	model    string
	dim      int
	apiKey   string
	client   *http.Client
}

type HTTPEmbedderConfig struct {
	Endpoint string // e.g. "http://localhost:8080/v1/embeddings"
	Model    string // e.g. "all-MiniLM-L6-v2"
	Dim      int    // expected output dimension (384 for MiniLM-L6-v2)
	APIKey   string // optional bearer token
}

func NewHTTPEmbedder(cfg HTTPEmbedderConfig) *HTTPEmbedder {
	if cfg.Dim <= 0 {
		cfg.Dim = 384
	}
	if cfg.Model == "" {
		cfg.Model = "all-MiniLM-L6-v2"
	}
	return &HTTPEmbedder{
		endpoint: cfg.Endpoint,
		model:    cfg.Model,
		dim:      cfg.Dim,
		apiKey:   cfg.APIKey,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (e *HTTPEmbedder) Dimension() int {
	return e.dim
}

type embeddingRequest struct {
	Input string `json:"input"`
	Model string `json:"model"`
}

type embeddingResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
}

func (e *HTTPEmbedder) Embed(text string) []float32 {
	vec, err := e.embedRemote(text)
	if err != nil {
		// Return zero vector on error; callers should use CachedEmbedder
		// to avoid repeated failures and log the issue upstream.
		return make([]float32, e.dim)
	}
	return vec
}

func (e *HTTPEmbedder) embedRemote(text string) ([]float32, error) {
	reqBody, err := json.Marshal(embeddingRequest{
		Input: text,
		Model: e.model,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, e.endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if e.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.apiKey)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("embedding service returned %d: %s", resp.StatusCode, string(body))
	}

	var result embeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if len(result.Data) == 0 || len(result.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("empty embedding response")
	}

	raw := result.Data[0].Embedding
	if len(raw) != e.dim {
		return nil, fmt.Errorf("dimension mismatch: got %d, want %d", len(raw), e.dim)
	}

	vec := make([]float32, e.dim)
	for i, v := range raw {
		vec[i] = float32(v)
	}
	normalize(vec)
	return vec, nil
}
