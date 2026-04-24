//go:build integration

// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 Tejus Pratap <tejzpr@gmail.com>
//
// See LICENSE file for details.

package db

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testNeo4jURI      = "bolt://localhost:7687"
	testNeo4jUsername = "neo4j"
	testNeo4jPassword = "testpass123"
	testNeo4jDatabase = "neo4j"
)

func openTestNeo4j(t *testing.T, user string) *Neo4jStore {
	t.Helper()
	isolation := "tenant"
	if user == "" {
		isolation = "database"
	}
	store, err := OpenNeo4j(Neo4jConfig{
		URI:       testNeo4jURI,
		Username:  testNeo4jUsername,
		Password:  testNeo4jPassword,
		Database:  testNeo4jDatabase,
		Isolation: isolation,
		User:      user,
	})
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	return store
}

func cleanupNeo4j(t *testing.T, store Store) {
	t.Helper()
	// Remove all nodes and relationships
	err := store.Execute("MATCH (n) DETACH DELETE n")
	require.NoError(t, err)
}

func TestNeo4j_Connect(t *testing.T) {
	store := openTestNeo4j(t, "testuser")
	assert.Equal(t, "neo4j", store.DBType())
	assert.Equal(t, "testuser", store.TenantUser())
	assert.Contains(t, store.Path(), "bolt://localhost:7687")
}

func TestNeo4j_ConnectDatabaseMode(t *testing.T) {
	store := openTestNeo4j(t, "")
	assert.Equal(t, "neo4j", store.DBType())
	assert.Equal(t, "", store.TenantUser())
}

func TestNeo4j_SchemaInit(t *testing.T) {
	store := openTestNeo4j(t, "testuser")
	cleanupNeo4j(t, store)

	err := InitSchemaNeo4j(store, 4)
	require.NoError(t, err)

	// Idempotent — run again
	err = InitSchemaNeo4j(store, 4)
	require.NoError(t, err)
}

func TestNeo4j_ExecuteAndQuery(t *testing.T) {
	store := openTestNeo4j(t, "testuser")
	cleanupNeo4j(t, store)

	err := InitSchemaNeo4j(store, 4)
	require.NoError(t, err)

	// Create an Engram
	err = store.PreparedExecute(`CREATE (e:Engram {
		user: $user,
		id: $id,
		content: $content,
		summary: $summary,
		memory_type: 'semantic',
		importance: 0.8,
		access_count: 0,
		created_at: localdatetime('2024-01-01T00:00:00'),
		last_accessed_at: localdatetime('2024-01-01T00:00:00'),
		decay_factor: 1.0,
		embedding: [0.1, 0.2, 0.3, 0.4],
		source: 'test',
		tags: 'integration',
		cluster_id: -1
	})`, map[string]any{
		"user":    "testuser",
		"id":      "engram-int-1",
		"content": "Neo4j integration test content",
		"summary": "integration test",
	})
	require.NoError(t, err)

	// Query it back
	rows, err := store.PreparedQueryRows(
		`MATCH (e:Engram {id: $id}) WHERE e.user = $user RETURN e.id AS id, e.content AS content`,
		map[string]any{"id": "engram-int-1", "user": "testuser"},
	)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "engram-int-1", rows[0]["id"])
	assert.Equal(t, "Neo4j integration test content", rows[0]["content"])

	// Clean up
	cleanupNeo4j(t, store)
}

func TestNeo4j_TenantIsolation(t *testing.T) {
	store := openTestNeo4j(t, "")
	cleanupNeo4j(t, store)
	err := InitSchemaNeo4j(store, 4)
	require.NoError(t, err)

	// Create engram for alice
	err = store.PreparedExecute(`CREATE (e:Engram {
		user: 'alice',
		id: 'engram-alice-1',
		content: 'Alice secret data',
		summary: 'alice secret',
		memory_type: 'semantic',
		importance: 0.8,
		access_count: 0,
		created_at: localdatetime('2024-01-01T00:00:00'),
		last_accessed_at: localdatetime('2024-01-01T00:00:00'),
		decay_factor: 1.0,
		embedding: [0.1, 0.2, 0.3, 0.4],
		source: 'test',
		tags: '',
		cluster_id: -1
	})`, nil)
	require.NoError(t, err)

	// Create engram for bob
	err = store.PreparedExecute(`CREATE (e:Engram {
		user: 'bob',
		id: 'engram-bob-1',
		content: 'Bob private data',
		summary: 'bob private',
		memory_type: 'semantic',
		importance: 0.9,
		access_count: 0,
		created_at: localdatetime('2024-01-01T00:00:00'),
		last_accessed_at: localdatetime('2024-01-01T00:00:00'),
		decay_factor: 1.0,
		embedding: [0.5, 0.6, 0.7, 0.8],
		source: 'test',
		tags: '',
		cluster_id: -1
	})`, nil)
	require.NoError(t, err)

	// Query as alice — should only see alice's data
	aliceRows, err := store.PreparedQueryRows(
		`MATCH (e:Engram) WHERE e.user = $user RETURN e.id AS id, e.content AS content`,
		map[string]any{"user": "alice"},
	)
	require.NoError(t, err)
	require.Len(t, aliceRows, 1)
	assert.Equal(t, "engram-alice-1", aliceRows[0]["id"])

	// Query as bob — should only see bob's data
	bobRows, err := store.PreparedQueryRows(
		`MATCH (e:Engram) WHERE e.user = $user RETURN e.id AS id, e.content AS content`,
		map[string]any{"user": "bob"},
	)
	require.NoError(t, err)
	require.Len(t, bobRows, 1)
	assert.Equal(t, "engram-bob-1", bobRows[0]["id"])

	// Query without user filter — should see both
	allRows, err := store.QueryRows(`MATCH (e:Engram) RETURN e.id AS id ORDER BY e.id`)
	require.NoError(t, err)
	require.Len(t, allRows, 2)

	// Clean up
	cleanupNeo4j(t, store)
}

func TestNeo4j_Relationships(t *testing.T) {
	store := openTestNeo4j(t, "testuser")
	cleanupNeo4j(t, store)
	err := InitSchemaNeo4j(store, 4)
	require.NoError(t, err)

	// Create two engrams
	for _, id := range []string{"e1", "e2"} {
		err = store.PreparedExecute(`CREATE (e:Engram {
			user: $user, id: $id, content: $id, summary: $id,
			memory_type: 'semantic', importance: 0.5, access_count: 0,
			created_at: localdatetime('2024-01-01T00:00:00'),
			last_accessed_at: localdatetime('2024-01-01T00:00:00'),
			decay_factor: 1.0, embedding: [0.1, 0.2, 0.3, 0.4],
			source: 'test', tags: '', cluster_id: -1
		})`, map[string]any{"user": "testuser", "id": id})
		require.NoError(t, err)
	}

	// Create a Cue
	err = store.PreparedExecute(`CREATE (c:Cue {
		user: $user, id: $id, name: 'test-cue', cue_type: 'keyword',
		embedding: [0.1, 0.2, 0.3, 0.4]
	})`, map[string]any{"user": "testuser", "id": "cue-1"})
	require.NoError(t, err)

	// EncodedBy relationship
	err = store.PreparedExecute(
		`MATCH (e:Engram {id: $eid}), (c:Cue {id: $cid})
		CREATE (e)-[:EncodedBy {strength: 0.9, created_at: localdatetime('2024-01-01T00:00:00')}]->(c)`,
		map[string]any{"eid": "e1", "cid": "cue-1"},
	)
	require.NoError(t, err)

	// AssociatedWith relationship
	err = store.PreparedExecute(
		`MATCH (e1:Engram {id: $from}), (e2:Engram {id: $to})
		CREATE (e1)-[:AssociatedWith {relation_type: 'semantic', strength: 0.8, created_at: localdatetime('2024-01-01T00:00:00')}]->(e2)`,
		map[string]any{"from": "e1", "to": "e2"},
	)
	require.NoError(t, err)

	// Verify relationships
	rows, err := store.QueryRows(`MATCH (e:Engram)-[r:EncodedBy]->(c:Cue) RETURN e.id AS eid, c.id AS cid, r.strength AS strength`)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "e1", rows[0]["eid"])
	assert.Equal(t, "cue-1", rows[0]["cid"])

	rows, err = store.QueryRows(`MATCH (e1:Engram)-[r:AssociatedWith]->(e2:Engram) RETURN e1.id AS from_id, e2.id AS to_id`)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "e1", rows[0]["from_id"])
	assert.Equal(t, "e2", rows[0]["to_id"])

	// Clean up
	cleanupNeo4j(t, store)
}
