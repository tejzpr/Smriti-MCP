/* SPDX-License-Identifier: MPL-2.0
 * Copyright 2026 Tejus Pratap <tejzpr@gmail.com>
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package memory

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tejzpr/smriti-mcp/db"
	"github.com/tejzpr/smriti-mcp/llm"
)

// setupTenantEngine creates an engine backed by LadybugDB but wrapped
// with TenantStoreWrapper to simulate Neo4j tenant-property isolation.
// Two engines sharing the same underlying store with different tenant
// users should not see each other's data.
func setupTenantEngine(t *testing.T, innerStore db.Store, user string) (*Engine, *httptest.Server) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/embeddings" {
			var req struct {
				Input string `json:"input"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			emb := deterministicEmbedding(req.Input, 4)
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
					"entities": []string{},
					"keywords": strings.Fields(strings.ToLower(userContent)),
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
					"summary":     "Summary: " + userContent,
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

	wrapped := &db.TenantStoreWrapper{Store: innerStore, User: user}

	client := llm.NewClient(llm.ClientConfig{
		LLMBaseURL:   server.URL,
		LLMModel:     "test",
		EmbedBaseURL: server.URL,
		EmbedModel:   "test",
		EmbedDims:    4,
	})

	engine := NewEngine(wrapped, client)

	t.Cleanup(func() {
		engine.Stop()
		server.Close()
	})

	return engine, server
}

func TestTenantIsolation_StoreAndList(t *testing.T) {
	// Both engines share the same underlying LadybugDB
	innerStore, err := db.OpenInMemory()
	require.NoError(t, err)
	defer innerStore.Close()

	// Init schema with tenant wrapper so user column is created
	schemaStore := &db.TenantStoreWrapper{Store: innerStore, User: "_schema"}
	err = db.InitSchema(schemaStore, 4)
	require.NoError(t, err)

	engineAlice, _ := setupTenantEngine(t, innerStore, "alice")
	engineBob, _ := setupTenantEngine(t, innerStore, "bob")

	ctx := context.Background()

	// Alice stores a memory
	_, err = engineAlice.Encode(ctx, StoreRequest{
		Content:    "Alice secret project alpha",
		Importance: 0.8,
		Source:     "test",
	})
	require.NoError(t, err)

	// Bob stores a memory
	_, err = engineBob.Encode(ctx, StoreRequest{
		Content:    "Bob secret project beta",
		Importance: 0.9,
		Source:     "test",
	})
	require.NoError(t, err)

	// Alice lists — should only see her own memory
	aliceResults, err := engineAlice.Search(ctx, RecallRequest{
		Query: "secret",
		Limit: 10,
		Mode:  "list",
	})
	require.NoError(t, err)
	assert.Len(t, aliceResults, 1, "Alice should see exactly 1 memory")
	assert.Contains(t, aliceResults[0].Engram.Content, "Alice")

	// Bob lists — should only see his own memory
	bobResults, err := engineBob.Search(ctx, RecallRequest{
		Query: "secret",
		Limit: 10,
		Mode:  "list",
	})
	require.NoError(t, err)
	assert.Len(t, bobResults, 1, "Bob should see exactly 1 memory")
	assert.Contains(t, bobResults[0].Engram.Content, "Bob")
}

func TestTenantIsolation_ForgetDoesNotAffectOtherUser(t *testing.T) {
	innerStore, err := db.OpenInMemory()
	require.NoError(t, err)
	defer innerStore.Close()

	schemaStore := &db.TenantStoreWrapper{Store: innerStore, User: "_schema"}
	err = db.InitSchema(schemaStore, 4)
	require.NoError(t, err)

	engineAlice, _ := setupTenantEngine(t, innerStore, "alice")
	engineBob, _ := setupTenantEngine(t, innerStore, "bob")

	ctx := context.Background()

	// Alice stores
	aliceEngram, err := engineAlice.Encode(ctx, StoreRequest{
		Content:    "Alice important data",
		Importance: 0.9,
		Source:     "test",
	})
	require.NoError(t, err)

	// Bob stores
	_, err = engineBob.Encode(ctx, StoreRequest{
		Content:    "Bob important data",
		Importance: 0.9,
		Source:     "test",
	})
	require.NoError(t, err)

	// Alice forgets her own memory
	err = engineAlice.Forget(aliceEngram.ID)
	require.NoError(t, err)

	// Alice should have no memories
	aliceResults, err := engineAlice.Search(ctx, RecallRequest{
		Query: "data",
		Limit: 10,
		Mode:  "list",
	})
	require.NoError(t, err)
	assert.Len(t, aliceResults, 0, "Alice should have no memories after forget")

	// Bob's memory should be unaffected
	bobResults, err := engineBob.Search(ctx, RecallRequest{
		Query: "data",
		Limit: 10,
		Mode:  "list",
	})
	require.NoError(t, err)
	assert.Len(t, bobResults, 1, "Bob should still have his memory")
}

func TestTenantIsolation_NoTenantSeesAll(t *testing.T) {
	// When TenantUser is "", all data is visible (LadybugDB behavior)
	innerStore, err := db.OpenInMemory()
	require.NoError(t, err)
	defer innerStore.Close()

	// Init with tenant wrapper so user column exists for the Alice engine
	schemaStore := &db.TenantStoreWrapper{Store: innerStore, User: "_schema"}
	err = db.InitSchema(schemaStore, 4)
	require.NoError(t, err)

	// One tenant engine, one non-tenant engine
	engineAlice, _ := setupTenantEngine(t, innerStore, "alice")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/embeddings" {
			var req struct {
				Input string `json:"input"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			emb := deterministicEmbedding(req.Input, 4)
			resp := map[string]any{"data": []map[string]any{{"embedding": emb, "index": 0}}}
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
				b, _ := json.Marshal(map[string]any{"entities": []string{}, "keywords": strings.Fields(strings.ToLower(userContent))})
				responseContent = string(b)
			} else {
				b, _ := json.Marshal(map[string]any{"summary": "Summary", "memory_type": "semantic", "entities": []any{}})
				responseContent = string(b)
			}
			resp := map[string]any{"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": responseContent}}}}
			json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := llm.NewClient(llm.ClientConfig{
		LLMBaseURL: server.URL, LLMModel: "test",
		EmbedBaseURL: server.URL, EmbedModel: "test", EmbedDims: 4,
	})
	engineNoTenant := NewEngine(innerStore, client)
	defer engineNoTenant.Stop()

	ctx := context.Background()

	// Alice stores a memory (with tenant property)
	_, err = engineAlice.Encode(ctx, StoreRequest{
		Content:    "Alice tenant data",
		Importance: 0.8,
		Source:     "test",
	})
	require.NoError(t, err)

	// Non-tenant engine stores a memory (without user property)
	_, err = engineNoTenant.Encode(ctx, StoreRequest{
		Content:    "Shared data no tenant",
		Importance: 0.7,
		Source:     "test",
	})
	require.NoError(t, err)

	// Non-tenant engine should see both memories (no filter)
	allResults, err := engineNoTenant.Search(ctx, RecallRequest{
		Query: "data",
		Limit: 10,
		Mode:  "list",
	})
	require.NoError(t, err)
	assert.Len(t, allResults, 2, "Non-tenant engine should see all memories")
}
