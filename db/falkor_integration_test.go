//go:build integration

/* SPDX-License-Identifier: MPL-2.0
 * Copyright 2026 Tejus Pratap <tejzpr@gmail.com>
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package db

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func falkorTestStore(t *testing.T, user string) *FalkorStore {
	t.Helper()
	store, err := OpenFalkor(FalkorConfig{
		Addr:      "localhost:6379",
		GraphName: "smriti_test",
		Isolation: "tenant",
		User:      user,
	})
	require.NoError(t, err, "failed to connect to FalkorDB at localhost:6379")
	t.Cleanup(func() {
		// Clean up test data
		_ = store.Execute("MATCH (n) DETACH DELETE n")
		store.Close()
	})
	return store
}

func TestFalkor_Connect(t *testing.T) {
	store := falkorTestStore(t, "test-user")
	assert.Equal(t, "falkordb", store.DBType())
	assert.Contains(t, store.Path(), "falkordb://")
	assert.Equal(t, "test-user", store.TenantUser())
}

func TestFalkor_ConnectGraphMode(t *testing.T) {
	store, err := OpenFalkor(FalkorConfig{
		Addr:      "localhost:6379",
		GraphName: "smriti_test_graph",
		Isolation: "graph",
		User:      "alice",
	})
	require.NoError(t, err)
	defer store.Close()

	assert.Equal(t, "", store.TenantUser(), "graph isolation should not set tenant user")
	assert.Contains(t, store.Path(), "smriti_test_graph")

	// Clean up
	_ = store.Execute("MATCH (n) DETACH DELETE n")
}

func TestFalkor_SchemaInit(t *testing.T) {
	store := falkorTestStore(t, "test-user")

	err := InitSchemaFalkor(store, 3)
	require.NoError(t, err, "schema init should succeed")

	// Run again — should be idempotent
	err = InitSchemaFalkor(store, 3)
	require.NoError(t, err, "schema init should be idempotent")
}

func TestFalkor_ExecuteAndQuery(t *testing.T) {
	store := falkorTestStore(t, "test-user")

	err := store.Execute("CREATE (e:Engram {id: 'test-1', content: 'hello world', created_at: localdatetime('2026-01-01T10:00:00')})")
	require.NoError(t, err)

	rows, err := store.QueryRows("MATCH (e:Engram {id: 'test-1'}) RETURN e.id AS id, e.content AS content")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "test-1", rows[0]["id"])
	assert.Equal(t, "hello world", rows[0]["content"])

	val, err := store.QuerySingleValue("MATCH (e:Engram {id: 'test-1'}) RETURN e.content")
	require.NoError(t, err)
	assert.Equal(t, "hello world", val)
}

func TestFalkor_PreparedExecuteAndQuery(t *testing.T) {
	store := falkorTestStore(t, "test-user")

	err := store.PreparedExecute(
		"CREATE (e:Engram {id: $id, content: $content})",
		map[string]any{"id": "param-1", "content": "parameterized content"},
	)
	require.NoError(t, err)

	rows, err := store.PreparedQueryRows(
		"MATCH (e:Engram {id: $id}) RETURN e.content AS content",
		map[string]any{"id": "param-1"},
	)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "parameterized content", rows[0]["content"])
}

func TestFalkor_TenantIsolation(t *testing.T) {
	storeAlice := falkorTestStore(t, "alice")
	storeBob, err := OpenFalkor(FalkorConfig{
		Addr:      "localhost:6379",
		GraphName: "smriti_test",
		Isolation: "tenant",
		User:      "bob",
	})
	require.NoError(t, err)
	defer storeBob.Close()

	// Alice creates a node
	err = storeAlice.PreparedExecute(
		"CREATE (e:Engram {id: $id, content: $content, user: $user})",
		map[string]any{"id": "alice-1", "content": "alice's secret", "user": "alice"},
	)
	require.NoError(t, err)

	// Bob creates a node
	err = storeBob.PreparedExecute(
		"CREATE (e:Engram {id: $id, content: $content, user: $user})",
		map[string]any{"id": "bob-1", "content": "bob's data", "user": "bob"},
	)
	require.NoError(t, err)

	// Alice should only see her data when filtering by user
	rows, err := storeAlice.PreparedQueryRows(
		"MATCH (e:Engram) WHERE e.user = $user RETURN e.id AS id",
		map[string]any{"user": "alice"},
	)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "alice-1", rows[0]["id"])

	// Bob should only see his data when filtering by user
	rows, err = storeBob.PreparedQueryRows(
		"MATCH (e:Engram) WHERE e.user = $user RETURN e.id AS id",
		map[string]any{"user": "bob"},
	)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "bob-1", rows[0]["id"])
}

func TestFalkor_Relationships(t *testing.T) {
	store := falkorTestStore(t, "test-user")

	err := store.Execute("CREATE (e:Engram {id: 'rel-e1', content: 'engram1'})")
	require.NoError(t, err)
	err = store.Execute("CREATE (c:Cue {id: 'rel-c1', name: 'cue1'})")
	require.NoError(t, err)

	err = store.Execute(`
		MATCH (e:Engram {id: 'rel-e1'}), (c:Cue {id: 'rel-c1'})
		CREATE (e)-[:EncodedBy {strength: 1.0}]->(c)
	`)
	require.NoError(t, err)

	rows, err := store.QueryRows(`
		MATCH (e:Engram {id: 'rel-e1'})-[r:EncodedBy]->(c:Cue {id: 'rel-c1'})
		RETURN e.id AS eid, c.id AS cid, r.strength AS strength
	`)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "rel-e1", rows[0]["eid"])
	assert.Equal(t, "rel-c1", rows[0]["cid"])
}

func TestFalkor_VectorIndex(t *testing.T) {
	store := falkorTestStore(t, "test-user")

	// Create vector index
	err := store.Execute("CREATE VECTOR INDEX FOR (e:Engram) ON (e.embedding) OPTIONS {dimension: 3, similarityFunction: 'cosine'}")
	if err != nil {
		t.Skipf("Vector index creation failed (may not be supported in this FalkorDB version): %v", err)
	}

	// Insert a node with a vector embedding
	err = store.Execute("CREATE (e:Engram {id: 'vec-1', content: 'test vec', embedding: vecf32([0.1, 0.2, 0.3])})")
	require.NoError(t, err)

	err = store.Execute("CREATE (e:Engram {id: 'vec-2', content: 'other vec', embedding: vecf32([0.9, 0.8, 0.7])})")
	require.NoError(t, err)

	// Query the vector index
	rows, err := store.QueryRows("CALL db.idx.vector.queryNodes('Engram', 'embedding', 2, vecf32([0.1, 0.2, 0.3])) YIELD node, score RETURN node.id AS id, score")
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(rows), 1)
	// Verify both results are returned and vec-1 is among them
	ids := make([]string, len(rows))
	for i, r := range rows {
		ids[i] = r["id"].(string)
		t.Logf("  result[%d] id=%s score=%v", i, r["id"], r["score"])
	}
	assert.Contains(t, ids, "vec-1")
	assert.Contains(t, ids, "vec-2")
}
