/* SPDX-License-Identifier: MPL-2.0
 * Copyright 2026 Tejus Pratap <tejzpr@gmail.com>
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tejzpr/smriti-mcp/backup"
	"github.com/tejzpr/smriti-mcp/db"
	"github.com/tejzpr/smriti-mcp/llm"
	"github.com/tejzpr/smriti-mcp/memory"
)

func setupToolsTestEngine(t *testing.T) (*memory.Engine, *httptest.Server) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/embeddings" {
			var req struct {
				Input string `json:"input"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			emb := make([]float32, 4)
			h := uint32(0)
			for _, c := range req.Input {
				h = h*31 + uint32(c)
			}
			for i := range emb {
				h = h*1103515245 + 12345
				emb[i] = float32(h%1000) / 1000.0
			}
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
	err = db.InitSchema(store, 4)
	require.NoError(t, err)

	client := llm.NewClient(llm.ClientConfig{
		LLMBaseURL:   server.URL,
		LLMModel:     "test",
		EmbedBaseURL: server.URL,
		EmbedModel:   "test",
		EmbedDims:    4,
	})

	engine := memory.NewEngine(store, client)

	t.Cleanup(func() {
		engine.Stop()
		store.Close()
		server.Close()
	})

	return engine, server
}

func makeCallToolRequest(name string, args map[string]any) mcp.CallToolRequest {
	return mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      name,
			Arguments: args,
		},
	}
}

// === Store Tool Tests ===

func TestSmritiStoreTool_Definition(t *testing.T) {
	tool := SmritiStoreTool()
	assert.Equal(t, "smriti_store", tool.Name)
	assert.Contains(t, tool.InputSchema.Required, "content")
}

func TestHandleSmritiStore_Success(t *testing.T) {
	engine, _ := setupToolsTestEngine(t)
	handler := HandleSmritiStore(engine)
	ctx := context.Background()

	req := makeCallToolRequest("smriti_store", map[string]any{
		"content":    "Go is a compiled language",
		"importance": 0.8,
		"tags":       "golang,programming",
		"source":     "test",
	})

	result, err := handler(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.IsError)

	var parsed map[string]any
	text := result.Content[0].(mcp.TextContent).Text
	require.NoError(t, json.Unmarshal([]byte(text), &parsed))
	assert.NotEmpty(t, parsed["id"])
	assert.NotEmpty(t, parsed["summary"])
}

func TestHandleSmritiStore_MissingContent(t *testing.T) {
	engine, _ := setupToolsTestEngine(t)
	handler := HandleSmritiStore(engine)
	ctx := context.Background()

	req := makeCallToolRequest("smriti_store", map[string]any{})
	result, err := handler(ctx, req)
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

// === Recall Tool Tests ===

func TestSmritiRecallTool_Definition(t *testing.T) {
	tool := SmritiRecallTool()
	assert.Equal(t, "smriti_recall", tool.Name)
}

func TestHandleSmritiRecall_ListMode(t *testing.T) {
	engine, _ := setupToolsTestEngine(t)
	ctx := context.Background()

	storeHandler := HandleSmritiStore(engine)
	storeReq := makeCallToolRequest("smriti_store", map[string]any{
		"content": "Test memory for listing",
	})
	_, err := storeHandler(ctx, storeReq)
	require.NoError(t, err)

	recallHandler := HandleSmritiRecall(engine)
	recallReq := makeCallToolRequest("smriti_recall", map[string]any{
		"mode":  "list",
		"limit": float64(10),
	})

	result, err := recallHandler(ctx, recallReq)
	require.NoError(t, err)
	assert.False(t, result.IsError)

	var parsed []map[string]any
	text := result.Content[0].(mcp.TextContent).Text
	require.NoError(t, json.Unmarshal([]byte(text), &parsed))
	assert.Greater(t, len(parsed), 0)
}

func TestHandleSmritiRecall_RecallMode(t *testing.T) {
	engine, _ := setupToolsTestEngine(t)
	ctx := context.Background()

	storeHandler := HandleSmritiStore(engine)
	storeReq := makeCallToolRequest("smriti_store", map[string]any{
		"content": "Kubernetes orchestrates containers",
	})
	_, err := storeHandler(ctx, storeReq)
	require.NoError(t, err)

	recallHandler := HandleSmritiRecall(engine)
	recallReq := makeCallToolRequest("smriti_recall", map[string]any{
		"query": "containers",
		"limit": float64(5),
	})

	result, err := recallHandler(ctx, recallReq)
	require.NoError(t, err)
	assert.False(t, result.IsError)
}

func TestHandleSmritiRecall_RecallRequiresQuery(t *testing.T) {
	engine, _ := setupToolsTestEngine(t)
	handler := HandleSmritiRecall(engine)
	ctx := context.Background()

	req := makeCallToolRequest("smriti_recall", map[string]any{
		"mode": "recall",
	})

	result, err := handler(ctx, req)
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

func TestHandleSmritiRecall_UnknownMode(t *testing.T) {
	engine, _ := setupToolsTestEngine(t)
	handler := HandleSmritiRecall(engine)
	ctx := context.Background()

	req := makeCallToolRequest("smriti_recall", map[string]any{
		"mode":  "invalid",
		"query": "test",
	})

	result, err := handler(ctx, req)
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

// === Manage Tool Tests ===

func TestSmritiManageTool_Definition(t *testing.T) {
	tool := SmritiManageTool()
	assert.Equal(t, "smriti_manage", tool.Name)
	assert.Contains(t, tool.InputSchema.Required, "action")
}

func TestHandleSmritiManage_Forget(t *testing.T) {
	engine, _ := setupToolsTestEngine(t)
	ctx := context.Background()

	storeHandler := HandleSmritiStore(engine)
	storeReq := makeCallToolRequest("smriti_store", map[string]any{
		"content": "Memory to forget",
	})
	storeResult, err := storeHandler(ctx, storeReq)
	require.NoError(t, err)

	var stored map[string]any
	text := storeResult.Content[0].(mcp.TextContent).Text
	require.NoError(t, json.Unmarshal([]byte(text), &stored))
	engramID := stored["id"].(string)

	bp := &backup.Noop{}
	manageHandler := HandleSmritiManage(engine, bp)
	manageReq := makeCallToolRequest("smriti_manage", map[string]any{
		"action":    "forget",
		"memory_id": engramID,
	})

	result, err := manageHandler(ctx, manageReq)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content[0].(mcp.TextContent).Text, engramID)
}

func TestHandleSmritiManage_ForgetMissingID(t *testing.T) {
	engine, _ := setupToolsTestEngine(t)
	bp := &backup.Noop{}
	handler := HandleSmritiManage(engine, bp)
	ctx := context.Background()

	req := makeCallToolRequest("smriti_manage", map[string]any{
		"action": "forget",
	})

	result, err := handler(ctx, req)
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

func TestHandleSmritiManage_Sync(t *testing.T) {
	engine, _ := setupToolsTestEngine(t)
	bp := &backup.Noop{}
	handler := HandleSmritiManage(engine, bp)
	ctx := context.Background()

	req := makeCallToolRequest("smriti_manage", map[string]any{
		"action": "sync",
	})

	result, err := handler(ctx, req)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content[0].(mcp.TextContent).Text, "sync")
}

func TestHandleSmritiManage_UnknownAction(t *testing.T) {
	engine, _ := setupToolsTestEngine(t)
	bp := &backup.Noop{}
	handler := HandleSmritiManage(engine, bp)
	ctx := context.Background()

	req := makeCallToolRequest("smriti_manage", map[string]any{
		"action": "invalid",
	})

	result, err := handler(ctx, req)
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

func TestHandleSmritiManage_MissingAction(t *testing.T) {
	engine, _ := setupToolsTestEngine(t)
	bp := &backup.Noop{}
	handler := HandleSmritiManage(engine, bp)
	ctx := context.Background()

	req := makeCallToolRequest("smriti_manage", map[string]any{})

	result, err := handler(ctx, req)
	require.NoError(t, err)
	assert.True(t, result.IsError)
}
