/* SPDX-License-Identifier: MPL-2.0
 * Copyright 2026 Tejus Pratap <tejzpr@gmail.com>
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package memory

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tejzpr/smriti-mcp/db"
)

func setupLeidenEngine(t *testing.T) *Engine {
	t.Helper()
	store, err := db.OpenInMemory()
	require.NoError(t, err)
	err = db.InitSchema(store, 4)
	require.NoError(t, err)

	engine := NewEngine(store, nil)
	t.Cleanup(func() {
		engine.Stop()
		store.Close()
	})
	return engine
}

func createEngram(t *testing.T, engine *Engine, id string, embedding []float32) {
	t.Helper()
	embStr := float32SliceToString(embedding)
	now := time.Now().Format("2006-01-02 15:04:05")
	err := engine.store.PreparedExecute(
		`CREATE (e:Engram {
			id: $id, content: $content, summary: $summary,
			memory_type: 'semantic', importance: 0.5,
			access_count: 0, created_at: timestamp($ts),
			last_accessed_at: timestamp($ts), decay_factor: 1.0,
			embedding: `+embStr+`,
			source: 'test', tags: '', cluster_id: -1
		})`,
		map[string]any{
			"id":      id,
			"content": "content-" + id,
			"summary": "summary-" + id,
			"ts":      now,
		},
	)
	require.NoError(t, err)
}

func createAssociation(t *testing.T, engine *Engine, fromID, toID string, strength float64) {
	t.Helper()
	now := time.Now().Format("2006-01-02 15:04:05")
	err := engine.store.PreparedExecute(
		`MATCH (e1:Engram {id: $eid1}), (e2:Engram {id: $eid2})
		CREATE (e1)-[:AssociatedWith {relation_type: 'semantic', strength: $str, created_at: timestamp($ts)}]->(e2)`,
		map[string]any{
			"eid1": fromID,
			"eid2": toID,
			"str":  strength,
			"ts":   now,
		},
	)
	require.NoError(t, err)
}

func getClusterID(t *testing.T, engine *Engine, engramID string) int64 {
	t.Helper()
	rows, err := engine.store.PreparedQueryRows(
		`MATCH (e:Engram {id: $eid}) RETURN e.cluster_id AS cluster_id`,
		map[string]any{"eid": engramID},
	)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	return toInt64(rows[0]["cluster_id"])
}

func TestRunLeiden_SkipSmallGraph(t *testing.T) {
	engine := setupLeidenEngine(t)

	// 0 nodes: should be a no-op
	err := engine.runLeiden()
	assert.NoError(t, err)

	// 2 nodes: still below threshold
	createEngram(t, engine, "a", []float32{1, 0, 0, 0})
	createEngram(t, engine, "b", []float32{0, 1, 0, 0})
	err = engine.runLeiden()
	assert.NoError(t, err)

	// cluster_ids should remain -1
	assert.Equal(t, int64(-1), getClusterID(t, engine, "a"))
	assert.Equal(t, int64(-1), getClusterID(t, engine, "b"))
}

func TestRunLeiden_SkipNoEdges(t *testing.T) {
	engine := setupLeidenEngine(t)

	// 4 nodes, no edges
	createEngram(t, engine, "a", []float32{1, 0, 0, 0})
	createEngram(t, engine, "b", []float32{0, 1, 0, 0})
	createEngram(t, engine, "c", []float32{0, 0, 1, 0})
	createEngram(t, engine, "d", []float32{0, 0, 0, 1})

	err := engine.runLeiden()
	assert.NoError(t, err)

	// No edges means Leiden is skipped, cluster_ids remain -1
	assert.Equal(t, int64(-1), getClusterID(t, engine, "a"))
	assert.Equal(t, int64(-1), getClusterID(t, engine, "b"))
}

func TestRunLeiden_TwoCommunities(t *testing.T) {
	engine := setupLeidenEngine(t)

	// Community 1: a, b, c (strongly connected)
	createEngram(t, engine, "a1", []float32{1, 0, 0, 0})
	createEngram(t, engine, "a2", []float32{1, 0, 0, 0})
	createEngram(t, engine, "a3", []float32{1, 0, 0, 0})
	createAssociation(t, engine, "a1", "a2", 0.95)
	createAssociation(t, engine, "a2", "a3", 0.90)
	createAssociation(t, engine, "a1", "a3", 0.92)

	// Community 2: b, c, d (strongly connected)
	createEngram(t, engine, "b1", []float32{0, 1, 0, 0})
	createEngram(t, engine, "b2", []float32{0, 1, 0, 0})
	createEngram(t, engine, "b3", []float32{0, 1, 0, 0})
	createAssociation(t, engine, "b1", "b2", 0.93)
	createAssociation(t, engine, "b2", "b3", 0.88)
	createAssociation(t, engine, "b1", "b3", 0.91)

	// Weak cross-cluster link
	createAssociation(t, engine, "a1", "b1", 0.1)

	err := engine.runLeiden()
	require.NoError(t, err)

	// All nodes in community 1 should share a cluster_id
	cidA1 := getClusterID(t, engine, "a1")
	cidA2 := getClusterID(t, engine, "a2")
	cidA3 := getClusterID(t, engine, "a3")
	assert.Equal(t, cidA1, cidA2, "a1 and a2 should be in the same cluster")
	assert.Equal(t, cidA1, cidA3, "a1 and a3 should be in the same cluster")

	// All nodes in community 2 should share a cluster_id
	cidB1 := getClusterID(t, engine, "b1")
	cidB2 := getClusterID(t, engine, "b2")
	cidB3 := getClusterID(t, engine, "b3")
	assert.Equal(t, cidB1, cidB2, "b1 and b2 should be in the same cluster")
	assert.Equal(t, cidB1, cidB3, "b1 and b3 should be in the same cluster")

	// The two communities should have different cluster_ids
	assert.NotEqual(t, cidA1, cidB1, "communities should have different cluster_ids")

	// All cluster_ids should be >= 0 (assigned)
	assert.GreaterOrEqual(t, cidA1, int64(0))
	assert.GreaterOrEqual(t, cidB1, int64(0))
}

func TestRunLeiden_SingleComponent(t *testing.T) {
	engine := setupLeidenEngine(t)

	// All tightly connected
	createEngram(t, engine, "x1", []float32{1, 0, 0, 0})
	createEngram(t, engine, "x2", []float32{0, 1, 0, 0})
	createEngram(t, engine, "x3", []float32{0, 0, 1, 0})
	createAssociation(t, engine, "x1", "x2", 0.95)
	createAssociation(t, engine, "x2", "x3", 0.95)
	createAssociation(t, engine, "x1", "x3", 0.95)

	err := engine.runLeiden()
	require.NoError(t, err)

	// All should have the same cluster_id
	cid1 := getClusterID(t, engine, "x1")
	cid2 := getClusterID(t, engine, "x2")
	cid3 := getClusterID(t, engine, "x3")
	assert.Equal(t, cid1, cid2)
	assert.Equal(t, cid1, cid3)
	assert.GreaterOrEqual(t, cid1, int64(0))
}

func TestRunLeiden_SmartCacheSkipsRetune(t *testing.T) {
	engine := setupLeidenEngine(t)

	// Create a small connected graph
	createEngram(t, engine, "c1", []float32{1, 0, 0, 0})
	createEngram(t, engine, "c2", []float32{0, 1, 0, 0})
	createEngram(t, engine, "c3", []float32{0, 0, 1, 0})
	createAssociation(t, engine, "c1", "c2", 0.9)
	createAssociation(t, engine, "c2", "c3", 0.9)

	// First run: should auto-tune
	err := engine.runLeiden()
	require.NoError(t, err)
	assert.Greater(t, engine.leiden.cachedResolution, 0.0)
	assert.Equal(t, int64(3), engine.leiden.cachedNodeCount)

	cachedRes := engine.leiden.cachedResolution

	// Second run with same graph: should reuse cached resolution
	err = engine.runLeiden()
	require.NoError(t, err)
	assert.Equal(t, cachedRes, engine.leiden.cachedResolution, "resolution should be reused")
	assert.Equal(t, int64(3), engine.leiden.cachedNodeCount, "node count should be unchanged")
}

func TestRunLeiden_RetunesOnGrowth(t *testing.T) {
	engine := setupLeidenEngine(t)

	// Start with 3 nodes
	createEngram(t, engine, "d1", []float32{1, 0, 0, 0})
	createEngram(t, engine, "d2", []float32{0, 1, 0, 0})
	createEngram(t, engine, "d3", []float32{0, 0, 1, 0})
	createAssociation(t, engine, "d1", "d2", 0.9)
	createAssociation(t, engine, "d2", "d3", 0.9)

	err := engine.runLeiden()
	require.NoError(t, err)
	assert.Equal(t, int64(3), engine.leiden.cachedNodeCount)

	// Simulate >10% growth by manually setting cachedNodeCount low
	engine.leiden.cachedNodeCount = 2

	// Add another node with an edge
	createEngram(t, engine, "d4", []float32{0, 0, 0, 1})
	createAssociation(t, engine, "d3", "d4", 0.85)

	err = engine.runLeiden()
	require.NoError(t, err)
	assert.Equal(t, int64(4), engine.leiden.cachedNodeCount, "should re-tune and update node count")
}

func TestNeedsRetune(t *testing.T) {
	assert.True(t, needsRetune(0, 10), "zero cached should always retune")
	assert.True(t, needsRetune(100, 115), "15% growth should retune")
	assert.False(t, needsRetune(100, 105), "5% growth should NOT retune")
	assert.True(t, needsRetune(100, 85), "15% shrink should retune")
	assert.False(t, needsRetune(100, 95), "5% shrink should NOT retune")
}

func TestDetermineSeedCluster(t *testing.T) {
	seen := map[string]*SearchResult{
		"a": {Engram: Engram{ClusterID: 1}, HopDepth: 0},
		"b": {Engram: Engram{ClusterID: 1}, HopDepth: 0},
		"c": {Engram: Engram{ClusterID: 2}, HopDepth: 0},
		"d": {Engram: Engram{ClusterID: 2}, HopDepth: 1},
	}
	assert.Equal(t, int64(1), determineSeedCluster(seen))

	// All hop results: should return -1
	seenHops := map[string]*SearchResult{
		"x": {Engram: Engram{ClusterID: 1}, HopDepth: 1},
	}
	assert.Equal(t, int64(-1), determineSeedCluster(seenHops))

	// Empty: should return -1
	assert.Equal(t, int64(-1), determineSeedCluster(map[string]*SearchResult{}))

	// All unassigned: should return -1
	seenUnassigned := map[string]*SearchResult{
		"u": {Engram: Engram{ClusterID: -1}, HopDepth: 0},
	}
	assert.Equal(t, int64(-1), determineSeedCluster(seenUnassigned))
}

func TestConsolidate_RunsLeiden(t *testing.T) {
	engine, _ := setupTestEngine(t)
	ctx := context.Background()

	// Encode enough engrams to have associations
	_, err := engine.Encode(ctx, StoreRequest{Content: "Go programming language basics"})
	require.NoError(t, err)
	_, err = engine.Encode(ctx, StoreRequest{Content: "Go programming language basics"})
	require.NoError(t, err)

	// Consolidate should run without error (Leiden may or may not assign clusters
	// depending on whether enough nodes/edges exist)
	err = engine.Consolidate(ctx)
	assert.NoError(t, err)
}
