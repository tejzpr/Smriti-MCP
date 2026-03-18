/* SPDX-License-Identifier: MPL-2.0
 * Copyright 2026 Tejus Pratap <tejzpr@gmail.com>
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	llmBaseURL   string
	llmAPIKey    string
	llmModel     string
	embedBaseURL string
	embedAPIKey  string
	embedModel   string
	embedDims    int
	httpClient   *http.Client
}

type ClientConfig struct {
	LLMBaseURL   string
	LLMAPIKey    string
	LLMModel     string
	EmbedBaseURL string
	EmbedAPIKey  string
	EmbedModel   string
	EmbedDims    int
}

func NewClient(cfg ClientConfig) *Client {
	return &Client{
		llmBaseURL:   cfg.LLMBaseURL,
		llmAPIKey:    cfg.LLMAPIKey,
		llmModel:     cfg.LLMModel,
		embedBaseURL: cfg.EmbedBaseURL,
		embedAPIKey:  cfg.EmbedAPIKey,
		embedModel:   cfg.EmbedModel,
		embedDims:    cfg.EmbedDims,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *apiError `json:"error,omitempty"`
}

type apiError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

type embedRequest struct {
	Model      string `json:"model"`
	Input      string `json:"input"`
	Dimensions int    `json:"dimensions,omitempty"`
}

type embedResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Error *apiError `json:"error,omitempty"`
}

func (c *Client) Chat(ctx context.Context, messages []ChatMessage, temperature float64) (string, error) {
	reqBody := chatRequest{
		Model:       c.llmModel,
		Messages:    messages,
		Temperature: temperature,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal chat request: %w", err)
	}

	url := c.llmBaseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create chat request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.llmAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.llmAPIKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("chat request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read chat response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("chat API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var chatResp chatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return "", fmt.Errorf("unmarshal chat response: %w", err)
	}

	if chatResp.Error != nil {
		return "", fmt.Errorf("chat API error: %s", chatResp.Error.Message)
	}

	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("chat API returned no choices")
	}

	return chatResp.Choices[0].Message.Content, nil
}

func (c *Client) Embed(ctx context.Context, text string) ([]float32, error) {
	reqBody := embedRequest{
		Model:      c.embedModel,
		Input:      text,
		Dimensions: c.embedDims,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal embed request: %w", err)
	}

	url := c.embedBaseURL + "/embeddings"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create embed request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.embedAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.embedAPIKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embed request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read embed response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embed API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var embedResp embedResponse
	if err := json.Unmarshal(respBody, &embedResp); err != nil {
		return nil, fmt.Errorf("unmarshal embed response: %w", err)
	}

	if embedResp.Error != nil {
		return nil, fmt.Errorf("embed API error: %s", embedResp.Error.Message)
	}

	if len(embedResp.Data) == 0 {
		return nil, fmt.Errorf("embed API returned no data")
	}

	return embedResp.Data[0].Embedding, nil
}

func (c *Client) EmbedDims() int {
	return c.embedDims
}

func (c *Client) SetEmbedDims(dims int) {
	c.embedDims = dims
}

// ProbeEmbeddingDims makes a single embedding call with a short test string
// and returns the actual dimensionality of the returned vector.
func (c *Client) ProbeEmbeddingDims(ctx context.Context) (int, error) {
	// Send without dimensions hint so we get the model's native dims
	reqBody := embedRequest{
		Model: c.embedModel,
		Input: "test",
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return 0, fmt.Errorf("marshal probe request: %w", err)
	}

	url := c.embedBaseURL + "/embeddings"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("create probe request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.embedAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.embedAPIKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("probe request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("read probe response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("probe API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var embedResp embedResponse
	if err := json.Unmarshal(respBody, &embedResp); err != nil {
		return 0, fmt.Errorf("unmarshal probe response: %w", err)
	}

	if embedResp.Error != nil {
		return 0, fmt.Errorf("probe API error: %s", embedResp.Error.Message)
	}

	if len(embedResp.Data) == 0 || len(embedResp.Data[0].Embedding) == 0 {
		return 0, fmt.Errorf("probe API returned no embedding data")
	}

	return len(embedResp.Data[0].Embedding), nil
}

func (c *Client) ChatWithJSON(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	messages := []ChatMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}
	return c.Chat(ctx, messages, 0.0)
}
