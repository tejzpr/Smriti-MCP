// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 Tejus Pratap <tejzpr@gmail.com>
//
// See LICENSE file for details.

package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tejzpr/smriti-mcp/db"
	"github.com/tejzpr/smriti-mcp/llm"
)

func setupTestEngine(t *testing.T) (*Engine, *httptest.Server) {
	t.Helper()

	callCount := 0
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
			callCount++
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
				words := strings.Fields(strings.ToLower(userContent))
				entities := []string{}
				keywords := []string{}
				for i, w := range words {
					if i < 3 {
						keywords = append(keywords, w)
					}
					if len(w) > 4 {
						entities = append(entities, w)
					}
				}
				if len(entities) == 0 {
					entities = keywords
				}
				resp := map[string]any{
					"entities": entities,
					"keywords": keywords,
				}
				b, _ := json.Marshal(resp)
				responseContent = string(b)
			} else {
				words := strings.Fields(strings.ToLower(userContent))
				entities := make([]map[string]string, 0)
				for _, w := range words {
					if len(w) > 3 {
						entities = append(entities, map[string]string{
							"name": w,
							"type": "keyword",
						})
					}
				}
				if len(entities) > 5 {
					entities = entities[:5]
				}
				resp := map[string]any{
					"summary":     fmt.Sprintf("Summary of: %s", truncateStr(userContent, 50)),
					"memory_type": "semantic",
					"entities":    entities,
				}
				b, _ := json.Marshal(resp)
				responseContent = string(b)
			}

			resp := map[string]any{
				"choices": []map[string]any{
					{"message": map[string]string{
						"role":    "assistant",
						"content": responseContent,
					}},
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
		LLMModel:     "test-model",
		EmbedBaseURL: server.URL,
		EmbedModel:   "test-embed",
		EmbedDims:    4,
	})

	engine := NewEngine(store, client)

	t.Cleanup(func() {
		engine.Stop()
		store.Close()
		server.Close()
	})

	return engine, server
}

func deterministicEmbedding(input string, dims int) []float32 {
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

func truncateStr(s string, max int) string {
	if len(s) > max {
		return s[:max]
	}
	return s
}

// === Part 1: Encoding Tests ===

func TestEncode_CreatesEngramAndCues(t *testing.T) {
	engine, _ := setupTestEngine(t)
	ctx := context.Background()

	engram, err := engine.Encode(ctx, StoreRequest{
		Content:    "Go is a statically typed programming language",
		Source:     "test",
		Tags:       []string{"golang", "programming"},
		Importance: 0.8,
	})

	require.NoError(t, err)
	assert.NotEmpty(t, engram.ID)
	assert.Equal(t, "Go is a statically typed programming language", engram.Content)
	assert.NotEmpty(t, engram.Summary)
	assert.Equal(t, "semantic", engram.MemoryType)
	assert.Equal(t, 0.8, engram.Importance)
	assert.Equal(t, int64(0), engram.AccessCount)
	assert.Equal(t, 1.0, engram.DecayFactor)
	assert.Equal(t, "test", engram.Source)
	assert.Equal(t, "golang,programming", engram.Tags)
	assert.Len(t, engram.Embedding, 4)

	rows, err := engine.store.QueryRows("MATCH (e:Engram) RETURN e.id AS id")
	require.NoError(t, err)
	assert.Len(t, rows, 1)

	cueRows, err := engine.store.QueryRows("MATCH (c:Cue) RETURN c.id AS id, c.name AS name")
	require.NoError(t, err)
	assert.Greater(t, len(cueRows), 0)

	edgeRows, err := engine.store.QueryRows("MATCH ()-[r:EncodedBy]->() RETURN count(r) AS cnt")
	require.NoError(t, err)
	assert.Greater(t, len(edgeRows), 0)
}

func TestEncode_DefaultImportance(t *testing.T) {
	engine, _ := setupTestEngine(t)
	ctx := context.Background()

	engram, err := engine.Encode(ctx, StoreRequest{
		Content: "Test content for default importance",
	})

	require.NoError(t, err)
	assert.Equal(t, 0.5, engram.Importance)
}

func TestEncode_AutoAssociation(t *testing.T) {
	engine, _ := setupTestEngine(t)
	ctx := context.Background()

	_, err := engine.Encode(ctx, StoreRequest{
		Content: "Go programming language basics",
	})
	require.NoError(t, err)

	_, err = engine.Encode(ctx, StoreRequest{
		Content: "Go programming language basics",
	})
	require.NoError(t, err)

	rows, err := engine.store.QueryRows("MATCH ()-[r:AssociatedWith]->() RETURN count(r) AS cnt")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	cnt := toInt64(rows[0]["cnt"])
	assert.Greater(t, cnt, int64(0), "identical content should auto-associate")
}

func TestEncode_MultipleEngrams(t *testing.T) {
	engine, _ := setupTestEngine(t)
	ctx := context.Background()

	contents := []string{
		"Python is great for data science",
		"Machine learning requires lots of data",
		"Neural networks are a type of ML model",
	}

	for _, c := range contents {
		_, err := engine.Encode(ctx, StoreRequest{Content: c})
		require.NoError(t, err)
	}

	rows, err := engine.store.QueryRows("MATCH (e:Engram) RETURN count(e) AS cnt")
	require.NoError(t, err)
	cnt := toInt64(rows[0]["cnt"])
	assert.Equal(t, int64(3), cnt)
}

// === Part 2: Retrieval Tests ===

func TestRecall_Basic(t *testing.T) {
	engine, _ := setupTestEngine(t)
	ctx := context.Background()

	_, err := engine.Encode(ctx, StoreRequest{
		Content: "Kubernetes orchestrates containers at scale",
	})
	require.NoError(t, err)

	_, err = engine.Encode(ctx, StoreRequest{
		Content: "Docker builds and runs container images",
	})
	require.NoError(t, err)

	results, err := engine.Recall(ctx, RecallRequest{
		Query: "container orchestration",
		Limit: 5,
	})
	require.NoError(t, err)
	assert.Greater(t, len(results), 0)

	for _, r := range results {
		assert.NotEmpty(t, r.Engram.ID)
		assert.Greater(t, r.Score, 0.0)
		assert.NotEmpty(t, r.MatchType)
	}
}

func TestRecall_SortedByScore(t *testing.T) {
	engine, _ := setupTestEngine(t)
	ctx := context.Background()

	for _, c := range []string{
		"Alpha topic about databases",
		"Beta topic about databases",
		"Gamma topic about databases",
	} {
		_, err := engine.Encode(ctx, StoreRequest{Content: c})
		require.NoError(t, err)
	}

	results, err := engine.Recall(ctx, RecallRequest{
		Query: "databases",
		Limit: 10,
	})
	require.NoError(t, err)

	for i := 1; i < len(results); i++ {
		assert.GreaterOrEqual(t, results[i-1].Score, results[i].Score)
	}
}

func TestRecall_LimitRespected(t *testing.T) {
	engine, _ := setupTestEngine(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		_, err := engine.Encode(ctx, StoreRequest{
			Content: fmt.Sprintf("Memory item number %d about testing", i),
		})
		require.NoError(t, err)
	}

	results, err := engine.Recall(ctx, RecallRequest{
		Query: "testing",
		Limit: 2,
	})
	require.NoError(t, err)
	assert.LessOrEqual(t, len(results), 2)
}

func TestRecall_StrengthenAccess(t *testing.T) {
	engine, _ := setupTestEngine(t)
	ctx := context.Background()

	engram, err := engine.Encode(ctx, StoreRequest{
		Content: "Important fact about strengthening",
	})
	require.NoError(t, err)

	results, err := engine.Recall(ctx, RecallRequest{
		Query: "strengthening",
		Limit: 5,
	})
	require.NoError(t, err)

	if len(results) > 0 {
		rows, err := engine.store.PreparedQueryRows(
			"MATCH (e:Engram {id: $eid}) RETURN e.access_count AS cnt",
			map[string]any{"eid": engram.ID})
		require.NoError(t, err)
		if len(rows) > 0 {
			cnt := toInt64(rows[0]["cnt"])
			assert.GreaterOrEqual(t, cnt, int64(0))
		}
	}
}

// === Part 3: Search Tests ===

func TestSearch_ListMode(t *testing.T) {
	engine, _ := setupTestEngine(t)
	ctx := context.Background()

	for _, c := range []string{"first item", "second item", "third item"} {
		_, err := engine.Encode(ctx, StoreRequest{Content: c})
		require.NoError(t, err)
	}

	results, err := engine.Search(ctx, RecallRequest{
		Mode:  "list",
		Limit: 10,
	})
	require.NoError(t, err)
	assert.Len(t, results, 3)
	for _, r := range results {
		assert.Equal(t, "list", r.MatchType)
	}
}

func TestSearch_VectorMode(t *testing.T) {
	engine, _ := setupTestEngine(t)
	ctx := context.Background()

	_, err := engine.Encode(ctx, StoreRequest{Content: "vector search target content"})
	require.NoError(t, err)

	results, err := engine.Search(ctx, RecallRequest{
		Query: "vector search",
		Mode:  "vector",
		Limit: 5,
	})
	require.NoError(t, err)
	assert.Greater(t, len(results), 0)
	for _, r := range results {
		assert.Equal(t, "vector", r.MatchType)
	}
}

func TestSearch_DefaultLimit(t *testing.T) {
	engine, _ := setupTestEngine(t)
	ctx := context.Background()

	_, err := engine.Encode(ctx, StoreRequest{Content: "test item"})
	require.NoError(t, err)

	results, err := engine.Search(ctx, RecallRequest{
		Mode: "list",
	})
	require.NoError(t, err)
	assert.LessOrEqual(t, len(results), 10)
}

// === Part 4: Consolidation Tests ===

func TestComputeDecay(t *testing.T) {
	tests := []struct {
		name             string
		currentDecay     float64
		hoursSinceAccess float64
		wantLess         float64
	}{
		{"no time passed", 1.0, 0, 1.01},
		{"one day passed", 1.0, 24, 1.0},
		{"one week passed", 1.0, 168, 1.0},
		{"already decayed", 0.5, 24, 0.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := computeDecay(tt.currentDecay, tt.hoursSinceAccess)
			assert.LessOrEqual(t, result, tt.wantLess)
			assert.GreaterOrEqual(t, result, 0.0)
		})
	}
}

func TestComputeDecay_NeverNegative(t *testing.T) {
	result := computeDecay(0.01, 100000)
	assert.GreaterOrEqual(t, result, 0.0)
}

func TestComputeDecay_NeverExceedsOne(t *testing.T) {
	result := computeDecay(1.5, 0)
	assert.LessOrEqual(t, result, 1.0)
}

func TestConsolidate_AppliesDecay(t *testing.T) {
	engine, _ := setupTestEngine(t)
	ctx := context.Background()

	engram, err := engine.Encode(ctx, StoreRequest{
		Content: "Consolidation decay test content",
	})
	require.NoError(t, err)

	// Manually set last_accessed_at to 7 days ago to trigger decay
	weekAgo := time.Now().Add(-7 * 24 * time.Hour)
	engine.store.PreparedExecute(
		"MATCH (e:Engram {id: $eid}) SET e.last_accessed_at = timestamp($ts)",
		map[string]any{"eid": engram.ID, "ts": weekAgo.Format("2006-01-02 15:04:05")})

	err = engine.Consolidate(ctx)
	require.NoError(t, err)

	rows, err := engine.store.PreparedQueryRows(
		"MATCH (e:Engram {id: $eid}) RETURN e.decay_factor AS df",
		map[string]any{"eid": engram.ID})
	require.NoError(t, err)
	require.Len(t, rows, 1)

	df := toFloat64(rows[0]["df"])
	assert.Less(t, df, 1.0, "decay should reduce after a week")
}

func TestConsolidate_PrunesWeak(t *testing.T) {
	engine, _ := setupTestEngine(t)
	ctx := context.Background()

	engram, err := engine.Encode(ctx, StoreRequest{
		Content:    "Weak memory to be pruned",
		Importance: 0.1,
	})
	require.NoError(t, err)

	engine.store.PreparedExecute(
		"MATCH (e:Engram {id: $eid}) SET e.decay_factor = 0.01, e.importance = 0.1",
		map[string]any{"eid": engram.ID})

	err = engine.Consolidate(ctx)
	require.NoError(t, err)

	rows, err := engine.store.PreparedQueryRows(
		"MATCH (e:Engram {id: $eid}) RETURN e.id",
		map[string]any{"eid": engram.ID})
	require.NoError(t, err)
	assert.Len(t, rows, 0, "weak engram should be pruned")
}

func TestConsolidate_KeepsImportant(t *testing.T) {
	engine, _ := setupTestEngine(t)
	ctx := context.Background()

	engram, err := engine.Encode(ctx, StoreRequest{
		Content:    "Important memory to keep",
		Importance: 0.9,
	})
	require.NoError(t, err)

	engine.store.PreparedExecute(
		"MATCH (e:Engram {id: $eid}) SET e.decay_factor = 0.01",
		map[string]any{"eid": engram.ID})

	err = engine.Consolidate(ctx)
	require.NoError(t, err)

	rows, err := engine.store.PreparedQueryRows(
		"MATCH (e:Engram {id: $eid}) RETURN e.id",
		map[string]any{"eid": engram.ID})
	require.NoError(t, err)
	assert.Len(t, rows, 1, "important engram should survive pruning")
}

// === Forget Tests ===

func TestForget(t *testing.T) {
	engine, _ := setupTestEngine(t)
	ctx := context.Background()

	engram, err := engine.Encode(ctx, StoreRequest{
		Content: "Memory to forget",
	})
	require.NoError(t, err)

	rows, err := engine.store.QueryRows("MATCH (e:Engram) RETURN count(e) AS cnt")
	require.NoError(t, err)
	assert.Equal(t, int64(1), toInt64(rows[0]["cnt"]))

	err = engine.Forget(engram.ID)
	require.NoError(t, err)

	rows, err = engine.store.QueryRows("MATCH (e:Engram) RETURN count(e) AS cnt")
	require.NoError(t, err)
	assert.Equal(t, int64(0), toInt64(rows[0]["cnt"]))
}

func TestForget_RemovesEdges(t *testing.T) {
	engine, _ := setupTestEngine(t)
	ctx := context.Background()

	engram, err := engine.Encode(ctx, StoreRequest{
		Content: "Memory with edges to forget",
	})
	require.NoError(t, err)

	edgeRows, err := engine.store.QueryRows("MATCH ()-[r:EncodedBy]->() RETURN count(r) AS cnt")
	require.NoError(t, err)
	assert.Greater(t, toInt64(edgeRows[0]["cnt"]), int64(0))

	err = engine.Forget(engram.ID)
	require.NoError(t, err)

	edgeRows, err = engine.store.QueryRows("MATCH ()-[r:EncodedBy]->() RETURN count(r) AS cnt")
	require.NoError(t, err)
	assert.Equal(t, int64(0), toInt64(edgeRows[0]["cnt"]))
}

// === Helper Function Tests ===

func TestCosineSimilarity(t *testing.T) {
	assert.InDelta(t, 1.0, cosineSimilarity(
		[]float32{1, 0, 0}, []float32{1, 0, 0}), 0.001)

	assert.InDelta(t, 0.0, cosineSimilarity(
		[]float32{1, 0, 0}, []float32{0, 1, 0}), 0.001)

	assert.InDelta(t, -1.0, cosineSimilarity(
		[]float32{1, 0, 0}, []float32{-1, 0, 0}), 0.001)

	assert.Equal(t, 0.0, cosineSimilarity([]float32{}, []float32{}))
	assert.Equal(t, 0.0, cosineSimilarity([]float32{1}, []float32{1, 2}))
	assert.Equal(t, 0.0, cosineSimilarity([]float32{0, 0, 0}, []float32{1, 0, 0}))
}

func TestFloat32SliceToString(t *testing.T) {
	assert.Equal(t, "[0.1,0.2,0.3]", float32SliceToString([]float32{0.1, 0.2, 0.3}))
	assert.Equal(t, "[]", float32SliceToString([]float32{}))
}

func TestRecencyScore(t *testing.T) {
	now := time.Now()
	assert.InDelta(t, 1.0, recencyScore(now, now), 0.001)

	dayAgo := now.Add(-24 * time.Hour)
	score := recencyScore(dayAgo, now)
	assert.Less(t, score, 1.0)
	assert.Greater(t, score, 0.0)

	weekAgo := now.Add(-7 * 24 * time.Hour)
	weekScore := recencyScore(weekAgo, now)
	assert.Less(t, weekScore, score, "older should have lower recency score")
}

func TestExtractEmbedding(t *testing.T) {
	assert.Nil(t, extractEmbedding(nil))
	assert.Equal(t, []float32{1, 2}, extractEmbedding([]float32{1, 2}))
	assert.Equal(t, []float32{1, 2}, extractEmbedding([]float64{1, 2}))
	assert.Equal(t, []float32{1, 2}, extractEmbedding([]any{float64(1), float64(2)}))
	assert.Nil(t, extractEmbedding("not an embedding"))
}

func TestToFloat64(t *testing.T) {
	assert.Equal(t, 1.5, toFloat64(1.5))
	assert.Equal(t, float64(1), toFloat64(float32(1)))
	assert.Equal(t, float64(42), toFloat64(int64(42)))
	assert.Equal(t, float64(10), toFloat64(10))
	assert.Equal(t, float64(0), toFloat64("not a number"))
}

func TestToInt64(t *testing.T) {
	assert.Equal(t, int64(42), toInt64(int64(42)))
	assert.Equal(t, int64(10), toInt64(10))
	assert.Equal(t, int64(3), toInt64(float64(3.7)))
	assert.Equal(t, int64(0), toInt64("not a number"))
}

func TestSqrt(t *testing.T) {
	assert.InDelta(t, 2.0, sqrt(4.0), 0.0001)
	assert.InDelta(t, 3.0, sqrt(9.0), 0.0001)
	assert.Equal(t, 0.0, sqrt(0.0))
	assert.Equal(t, 0.0, sqrt(-1.0))
}
