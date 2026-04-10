/* SPDX-License-Identifier: MPL-2.0
 * Copyright 2026 Tejus Pratap <tejzpr@gmail.com>
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package testutil

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tejzpr/smriti-mcp/db"
	"github.com/tejzpr/smriti-mcp/llm"
	"github.com/tejzpr/smriti-mcp/memory"
)

type TestEnv struct {
	Engine *memory.Engine
	Store  db.Store
	Server *httptest.Server
}

func SetupTestEnv(t *testing.T, dims int) *TestEnv {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/embeddings" {
			var req struct {
				Input string `json:"input"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			emb := DeterministicEmbedding(req.Input, dims)
			resp := map[string]any{
				"data": []map[string]any{
					{"embedding": emb, "index": 0},
				},
			}
			json.NewEncoder(w).Encode(resp)
			return
		}

		if r.URL.Path == "/chat/completions" {
			var req struct {
				Messages []struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"messages"`
			}
			json.NewDecoder(r.Body).Decode(&req)

			userContent := ""
			systemContent := ""
			for _, m := range req.Messages {
				if m.Role == "user" {
					userContent = m.Content
				}
				if m.Role == "system" {
					systemContent = m.Content
				}
			}

			var responseContent string
			if strings.Contains(systemContent, "retrieval cue extractor") {
				resp := map[string]any{
					"entities": []string{"test"},
					"keywords": []string{"test"},
				}
				b, _ := json.Marshal(resp)
				responseContent = string(b)
			} else {
				words := strings.Fields(strings.ToLower(userContent))
				entities := make([]map[string]string, 0)
				for _, w := range words {
					if len(w) > 3 {
						entities = append(entities, map[string]string{"name": w, "type": "keyword"})
					}
				}
				if len(entities) > 3 {
					entities = entities[:3]
				}
				resp := map[string]any{
					"summary":     "Test summary",
					"memory_type": "semantic",
					"entities":    entities,
				}
				b, _ := json.Marshal(resp)
				responseContent = string(b)
			}

			resp := map[string]any{
				"choices": []map[string]any{
					{"message": map[string]string{"role": "assistant", "content": responseContent}},
				},
			}
			json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))

	store, err := db.OpenInMemory()
	require.NoError(t, err)
	err = db.InitSchema(store, dims)
	require.NoError(t, err)

	client := llm.NewClient(llm.ClientConfig{
		LLMBaseURL:   server.URL,
		LLMModel:     "test",
		EmbedBaseURL: server.URL,
		EmbedModel:   "test",
		EmbedDims:    dims,
	})

	engine := memory.NewEngine(store, client)

	env := &TestEnv{
		Engine: engine,
		Store:  store,
		Server: server,
	}

	t.Cleanup(func() {
		engine.Stop()
		store.Close()
		server.Close()
	})

	return env
}

func DeterministicEmbedding(input string, dims int) []float32 {
	emb := make([]float32, dims)
	h := uint32(0)
	for _, c := range input {
		h = h*31 + uint32(c)
	}
	for i := range emb {
		h = h*1103515245 + 12345
		emb[i] = float32(h%1000) / 1000.0
	}
	norm := float32(0)
	for _, v := range emb {
		norm += v * v
	}
	if norm > 0 {
		norm = float32(math.Sqrt(float64(norm)))
		for i := range emb {
			emb[i] /= norm
		}
	}
	return emb
}
