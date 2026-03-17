/* SPDX-License-Identifier: MPL-2.0
 * Copyright 2026 Tejus Pratap <tejzpr@gmail.com>
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(handler)
}

func TestChat_Success(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/chat/completions", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))

		var req chatRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)
		assert.Equal(t, "gpt-4o-mini", req.Model)
		assert.Len(t, req.Messages, 1)
		assert.Equal(t, "user", req.Messages[0].Role)

		resp := chatResponse{
			Choices: []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			}{
				{Message: struct {
					Content string `json:"content"`
				}{Content: "Hello, world!"}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	defer server.Close()

	client := NewClient(ClientConfig{
		LLMBaseURL: server.URL,
		LLMAPIKey:  "test-key",
		LLMModel:   "gpt-4o-mini",
	})

	result, err := client.Chat(context.Background(), []ChatMessage{
		{Role: "user", Content: "Hi"},
	}, 0.7)

	require.NoError(t, err)
	assert.Equal(t, "Hello, world!", result)
}

func TestChat_NoAPIKey(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "", r.Header.Get("Authorization"))
		resp := chatResponse{
			Choices: []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			}{
				{Message: struct {
					Content string `json:"content"`
				}{Content: "ok"}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	})
	defer server.Close()

	client := NewClient(ClientConfig{LLMBaseURL: server.URL, LLMModel: "test-model"})
	result, err := client.Chat(context.Background(), []ChatMessage{{Role: "user", Content: "Hi"}}, 0.0)

	require.NoError(t, err)
	assert.Equal(t, "ok", result)
}

func TestChat_APIError(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"message":"invalid api key","type":"auth_error"}}`))
	})
	defer server.Close()

	client := NewClient(ClientConfig{LLMBaseURL: server.URL, LLMModel: "test-model"})
	_, err := client.Chat(context.Background(), []ChatMessage{{Role: "user", Content: "Hi"}}, 0.0)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "status 401")
}

func TestChat_EmptyChoices(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(chatResponse{Choices: nil})
	})
	defer server.Close()

	client := NewClient(ClientConfig{LLMBaseURL: server.URL, LLMModel: "m"})
	_, err := client.Chat(context.Background(), []ChatMessage{{Role: "user", Content: "Hi"}}, 0.0)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no choices")
}

func TestChat_ResponseError(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		resp := chatResponse{
			Error: &apiError{Message: "rate limit exceeded", Type: "rate_limit"},
		}
		json.NewEncoder(w).Encode(resp)
	})
	defer server.Close()

	client := NewClient(ClientConfig{LLMBaseURL: server.URL, LLMModel: "m"})
	_, err := client.Chat(context.Background(), []ChatMessage{{Role: "user", Content: "Hi"}}, 0.0)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "rate limit exceeded")
}

func TestEmbed_Success(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/embeddings", r.URL.Path)
		assert.Equal(t, "Bearer embed-key", r.Header.Get("Authorization"))

		var req embedRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)
		assert.Equal(t, "text-embedding-3-small", req.Model)
		assert.Equal(t, "hello world", req.Input)
		assert.Equal(t, 384, req.Dimensions)

		resp := embedResponse{
			Data: []struct {
				Embedding []float32 `json:"embedding"`
				Index     int       `json:"index"`
			}{
				{Embedding: []float32{0.1, 0.2, 0.3}, Index: 0},
			},
		}
		json.NewEncoder(w).Encode(resp)
	})
	defer server.Close()

	client := NewClient(ClientConfig{
		EmbedBaseURL: server.URL,
		EmbedAPIKey:  "embed-key",
		EmbedModel:   "text-embedding-3-small",
		EmbedDims:    384,
	})

	result, err := client.Embed(context.Background(), "hello world")
	require.NoError(t, err)
	assert.Equal(t, []float32{0.1, 0.2, 0.3}, result)
}

func TestEmbed_APIError(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":{"message":"invalid model"}}`))
	})
	defer server.Close()

	client := NewClient(ClientConfig{EmbedBaseURL: server.URL, EmbedModel: "m", EmbedDims: 384})
	_, err := client.Embed(context.Background(), "hello")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "status 400")
}

func TestEmbed_EmptyData(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(embedResponse{Data: nil})
	})
	defer server.Close()

	client := NewClient(ClientConfig{EmbedBaseURL: server.URL, EmbedModel: "m", EmbedDims: 384})
	_, err := client.Embed(context.Background(), "hello")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no data")
}

func TestEmbedDims(t *testing.T) {
	client := NewClient(ClientConfig{EmbedDims: 768})
	assert.Equal(t, 768, client.EmbedDims())
}

func TestChatWithJSON(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		json.NewDecoder(r.Body).Decode(&req)
		assert.Len(t, req.Messages, 2)
		assert.Equal(t, "system", req.Messages[0].Role)
		assert.Equal(t, "sys prompt", req.Messages[0].Content)
		assert.Equal(t, "user", req.Messages[1].Role)
		assert.Equal(t, "user prompt", req.Messages[1].Content)
		assert.Equal(t, 0.0, req.Temperature)

		resp := chatResponse{
			Choices: []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			}{
				{Message: struct {
					Content string `json:"content"`
				}{Content: `{"result":"ok"}`}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	})
	defer server.Close()

	client := NewClient(ClientConfig{LLMBaseURL: server.URL, LLMModel: "m"})
	result, err := client.ChatWithJSON(context.Background(), "sys prompt", "user prompt")

	require.NoError(t, err)
	assert.Equal(t, `{"result":"ok"}`, result)
}

func TestExtractMemoryInfo(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		resp := chatResponse{
			Choices: []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			}{
				{Message: struct {
					Content string `json:"content"`
				}{Content: `{"summary":"Meeting about project X","memory_type":"episodic","entities":[{"name":"Project X","type":"entity"},{"name":"meeting","type":"keyword"}]}`}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	})
	defer server.Close()

	client := NewClient(ClientConfig{LLMBaseURL: server.URL, LLMModel: "m"})
	result, err := client.ExtractMemoryInfo(context.Background(), "We had a meeting about Project X today")

	require.NoError(t, err)
	assert.Equal(t, "Meeting about project X", result.Summary)
	assert.Equal(t, "episodic", result.MemoryType)
	assert.Len(t, result.Entities, 2)
	assert.Equal(t, "project x", result.Entities[0].Name)
	assert.Equal(t, "entity", result.Entities[0].Type)
	assert.Equal(t, "meeting", result.Entities[1].Name)
}

func TestExtractMemoryInfo_InvalidMemoryType(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		resp := chatResponse{
			Choices: []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			}{
				{Message: struct {
					Content string `json:"content"`
				}{Content: `{"summary":"test","memory_type":"invalid","entities":[]}`}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	})
	defer server.Close()

	client := NewClient(ClientConfig{LLMBaseURL: server.URL, LLMModel: "m"})
	result, err := client.ExtractMemoryInfo(context.Background(), "test")

	require.NoError(t, err)
	assert.Equal(t, "semantic", result.MemoryType)
}

func TestExtractMemoryInfo_MarkdownWrapped(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		resp := chatResponse{
			Choices: []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			}{
				{Message: struct {
					Content string `json:"content"`
				}{Content: "```json\n{\"summary\":\"test\",\"memory_type\":\"semantic\",\"entities\":[]}\n```"}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	})
	defer server.Close()

	client := NewClient(ClientConfig{LLMBaseURL: server.URL, LLMModel: "m"})
	result, err := client.ExtractMemoryInfo(context.Background(), "test")

	require.NoError(t, err)
	assert.Equal(t, "test", result.Summary)
}

func TestExtractCues(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		resp := chatResponse{
			Choices: []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			}{
				{Message: struct {
					Content string `json:"content"`
				}{Content: `{"entities":["Project X","Alice"],"keywords":["architecture","decision"]}`}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	})
	defer server.Close()

	client := NewClient(ClientConfig{LLMBaseURL: server.URL, LLMModel: "m"})
	result, err := client.ExtractCues(context.Background(), "What did Alice decide about the Project X architecture?")

	require.NoError(t, err)
	assert.Equal(t, []string{"project x", "alice"}, result.Entities)
	assert.Equal(t, []string{"architecture", "decision"}, result.Keywords)
}

func TestCleanJSONResponse(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{`{"key":"value"}`, `{"key":"value"}`},
		{"```json\n{\"key\":\"value\"}\n```", `{"key":"value"}`},
		{"```\n{\"key\":\"value\"}\n```", `{"key":"value"}`},
		{"  {\"key\":\"value\"}  ", `{"key":"value"}`},
	}

	for _, tt := range tests {
		result := cleanJSONResponse(tt.input)
		assert.Equal(t, tt.expected, result)
	}
}
