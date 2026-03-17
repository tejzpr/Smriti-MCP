/* SPDX-License-Identifier: MPL-2.0
 * Copyright 2026 Tejus Pratap <tejzpr@gmail.com>
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package db

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenInMemory(t *testing.T) {
	store, err := OpenInMemory()
	require.NoError(t, err)
	defer store.Close()

	assert.Equal(t, ":memory:", store.Path())
	assert.NotNil(t, store.DB())
}

func TestOpenAndClose(t *testing.T) {
	dir := t.TempDir()
	dbPath := dir + "/test.lbug"

	store, err := Open(dbPath)
	require.NoError(t, err)

	assert.Equal(t, dbPath, store.Path())
	store.Close()

	// Double close should be safe
	store.Close()
}

func TestExecute(t *testing.T) {
	store, err := OpenInMemory()
	require.NoError(t, err)
	defer store.Close()

	err = store.Execute("CREATE NODE TABLE TestNode(id STRING PRIMARY KEY, val INT64)")
	require.NoError(t, err)

	err = store.Execute("CREATE (:TestNode {id: 'a', val: 1})")
	require.NoError(t, err)
}

func TestExecute_InvalidQuery(t *testing.T) {
	store, err := OpenInMemory()
	require.NoError(t, err)
	defer store.Close()

	err = store.Execute("THIS IS NOT VALID CYPHER")
	require.Error(t, err)
}

func TestQueryRows(t *testing.T) {
	store, err := OpenInMemory()
	require.NoError(t, err)
	defer store.Close()

	err = store.Execute("CREATE NODE TABLE TestNode(id STRING PRIMARY KEY, val INT64)")
	require.NoError(t, err)
	err = store.Execute("CREATE (:TestNode {id: 'a', val: 10})")
	require.NoError(t, err)
	err = store.Execute("CREATE (:TestNode {id: 'b', val: 20})")
	require.NoError(t, err)

	rows, err := store.QueryRows("MATCH (n:TestNode) RETURN n.id AS id, n.val AS val ORDER BY n.id")
	require.NoError(t, err)
	require.Len(t, rows, 2)

	assert.Equal(t, "a", rows[0]["id"])
	assert.Equal(t, "b", rows[1]["id"])
}

func TestQuerySingleValue(t *testing.T) {
	store, err := OpenInMemory()
	require.NoError(t, err)
	defer store.Close()

	err = store.Execute("CREATE NODE TABLE TestNode(id STRING PRIMARY KEY, val INT64)")
	require.NoError(t, err)
	err = store.Execute("CREATE (:TestNode {id: 'a', val: 42})")
	require.NoError(t, err)

	val, err := store.QuerySingleValue("MATCH (n:TestNode) RETURN n.val")
	require.NoError(t, err)
	assert.Equal(t, int64(42), val)
}

func TestQuerySingleValue_NoRows(t *testing.T) {
	store, err := OpenInMemory()
	require.NoError(t, err)
	defer store.Close()

	err = store.Execute("CREATE NODE TABLE TestNode(id STRING PRIMARY KEY)")
	require.NoError(t, err)

	_, err = store.QuerySingleValue("MATCH (n:TestNode) RETURN n.id")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no rows")
}

func TestInitSchema_Idempotent(t *testing.T) {
	store, err := OpenInMemory()
	require.NoError(t, err)
	defer store.Close()

	// First init
	err = InitSchema(store, 4)
	require.NoError(t, err)

	// Second init should not fail (idempotent)
	err = InitSchema(store, 4)
	require.NoError(t, err)

	// Verify tables exist by inserting data
	err = store.Execute(`CREATE (e:Engram {
		id: 'test-1',
		content: 'hello',
		summary: 'test',
		memory_type: 'semantic',
		importance: 0.5,
		access_count: 0,
		created_at: timestamp('2024-01-01 00:00:00'),
		last_accessed_at: timestamp('2024-01-01 00:00:00'),
		decay_factor: 1.0,
		embedding: [0.1, 0.2, 0.3, 0.4],
		source: 'test',
		tags: 'tag1,tag2'
	})`)
	require.NoError(t, err)

	err = store.Execute(`CREATE (c:Cue {
		id: 'cue-1',
		name: 'hello',
		cue_type: 'keyword',
		embedding: [0.1, 0.2, 0.3, 0.4]
	})`)
	require.NoError(t, err)

	// Verify engram inserted
	rows, err := store.QueryRows("MATCH (e:Engram) RETURN e.id AS id, e.content AS content")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "test-1", rows[0]["id"])
	assert.Equal(t, "hello", rows[0]["content"])

	// Verify cue inserted
	rows, err = store.QueryRows("MATCH (c:Cue) RETURN c.id AS id, c.name AS name")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "cue-1", rows[0]["id"])
}

func TestInitSchema_RelationshipTables(t *testing.T) {
	store, err := OpenInMemory()
	require.NoError(t, err)
	defer store.Close()

	err = InitSchema(store, 4)
	require.NoError(t, err)

	// Create two engrams and a cue
	err = store.Execute(`CREATE (e:Engram {
		id: 'e1', content: 'a', summary: 's', memory_type: 'semantic',
		importance: 0.5, access_count: 0,
		created_at: timestamp('2024-01-01 00:00:00'),
		last_accessed_at: timestamp('2024-01-01 00:00:00'),
		decay_factor: 1.0, embedding: [0.1, 0.2, 0.3, 0.4],
		source: '', tags: ''
	})`)
	require.NoError(t, err)

	err = store.Execute(`CREATE (e:Engram {
		id: 'e2', content: 'b', summary: 's2', memory_type: 'semantic',
		importance: 0.5, access_count: 0,
		created_at: timestamp('2024-01-01 00:00:00'),
		last_accessed_at: timestamp('2024-01-01 00:00:00'),
		decay_factor: 1.0, embedding: [0.5, 0.6, 0.7, 0.8],
		source: '', tags: ''
	})`)
	require.NoError(t, err)

	err = store.Execute(`CREATE (c:Cue {
		id: 'c1', name: 'test', cue_type: 'keyword', embedding: [0.1, 0.2, 0.3, 0.4]
	})`)
	require.NoError(t, err)

	err = store.Execute(`CREATE (c:Cue {
		id: 'c2', name: 'test2', cue_type: 'keyword', embedding: [0.5, 0.6, 0.7, 0.8]
	})`)
	require.NoError(t, err)

	// Create EncodedBy relationship
	err = store.Execute(`MATCH (e:Engram {id: 'e1'}), (c:Cue {id: 'c1'})
		CREATE (e)-[:EncodedBy {strength: 0.9, created_at: timestamp('2024-01-01 00:00:00')}]->(c)`)
	require.NoError(t, err)

	// Create AssociatedWith relationship
	err = store.Execute(`MATCH (e1:Engram {id: 'e1'}), (e2:Engram {id: 'e2'})
		CREATE (e1)-[:AssociatedWith {relation_type: 'semantic', strength: 0.8, created_at: timestamp('2024-01-01 00:00:00')}]->(e2)`)
	require.NoError(t, err)

	// Create CoOccurs relationship
	err = store.Execute(`MATCH (c1:Cue {id: 'c1'}), (c2:Cue {id: 'c2'})
		CREATE (c1)-[:CoOccurs {weight: 0.7}]->(c2)`)
	require.NoError(t, err)

	// Verify relationships
	rows, err := store.QueryRows(`MATCH (e:Engram)-[r:EncodedBy]->(c:Cue) RETURN e.id AS eid, c.id AS cid, r.strength AS strength`)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "e1", rows[0]["eid"])
	assert.Equal(t, "c1", rows[0]["cid"])
}

func TestTruncate(t *testing.T) {
	assert.Equal(t, "hello", truncate("hello", 10))
	assert.Equal(t, "hel...", truncate("hello world", 3))
	assert.Equal(t, "a b c", truncate("a\nb\tc", 10))
}

func TestIsAlreadyExistsError(t *testing.T) {
	assert.False(t, isAlreadyExistsError(nil))
	assert.True(t, isAlreadyExistsError(fmt.Errorf("table Already Exists")))
	assert.True(t, isAlreadyExistsError(fmt.Errorf("extension already loaded")))
	assert.False(t, isAlreadyExistsError(fmt.Errorf("some other error")))
}
