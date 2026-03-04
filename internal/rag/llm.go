package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// LLMClient provides LLM completion capabilities.
type LLMClient interface {
	Complete(ctx context.Context, prompt string) (string, error)
}

// GrokClient is an LLM client for XAI's Grok API (OpenAI-compatible).
type GrokClient struct {
	endpoint   string
	apiKey     string
	model      string
	httpClient *http.Client
}

// NewGrokClient creates a new Grok LLM client.
func NewGrokClient(endpoint, apiKey, model string) *GrokClient {
	if endpoint == "" {
		endpoint = "https://api.x.ai/v1/chat/completions"
	}
	if model == "" {
		model = "grok-4-1-fast-non-reasoning"
	}
	return &GrokClient{
		endpoint: endpoint,
		apiKey:   apiKey,
		model:    model,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (c *GrokClient) Complete(ctx context.Context, prompt string) (string, error) {
	reqBody := chatRequest{
		Model: c.model,
		Messages: []chatMessage{
			{
				Role:    "system",
				Content: "You are a Fortran code expert analyzing legacy BLAS libraries. Provide clear, technical answers grounded in the code. Cite code locations as file:startLine-endLine.",
			},
			{
				Role:    "user",
				Content: prompt,
			},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("llm request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("llm error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var chatResp chatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return "", fmt.Errorf("unmarshal response: %w", err)
	}

	if chatResp.Error != nil {
		return "", fmt.Errorf("llm api error: %s", chatResp.Error.Message)
	}

	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("llm returned no choices")
	}

	return chatResp.Choices[0].Message.Content, nil
}
