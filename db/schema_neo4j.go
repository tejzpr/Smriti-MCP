// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 Tejus Pratap <tejzpr@gmail.com>
//
// See LICENSE file for details.

package db

import (
	"fmt"
	"strings"
)

// InitSchemaNeo4j creates the necessary constraints and indexes for Neo4j.
// Neo4j does not require explicit table creation — nodes and relationships
// are created dynamically. We only need uniqueness constraints and indexes.
func InitSchemaNeo4j(store Store, embeddingDims int) error {
	statements := neo4jSchemaStatements(store, embeddingDims)
	for _, stmt := range statements {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if err := store.Execute(stmt); err != nil {
			if isAlreadyExistsError(err) || isNeo4jEquivalentError(err) {
				continue
			}
			return fmt.Errorf("neo4j schema init failed on %q: %w", truncate(stmt, 80), err)
		}
	}
	return nil
}

// MigrateSchemaNeo4j runs Neo4j-specific migrations.
// Neo4j properties are schema-free, so adding new properties doesn't
// require ALTER statements. This is kept for any future index migrations.
func MigrateSchemaNeo4j(store Store) {
	// Neo4j is schema-free for node properties — no ALTER needed.
	// cluster_id is added automatically when first SET on a node.
}

// EnsureIndexesNeo4j creates vector and full-text indexes for Neo4j
// once the data size warrants them.
func EnsureIndexesNeo4j(store Store, embeddingDims int) {
	count, err := store.QuerySingleValue("MATCH (e:Engram) RETURN count(e)")
	if err != nil {
		return
	}
	n, ok := count.(int64)
	if !ok || n < int64(IndexThreshold) {
		return
	}
	for _, stmt := range neo4jIndexStatements(embeddingDims) {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if err := store.Execute(stmt); err != nil {
			if isAlreadyExistsError(err) || isNeo4jEquivalentError(err) {
				continue
			}
		}
	}
}

func neo4jSchemaStatements(store Store, _ int) []string {
	stmts := []string{
		// Uniqueness constraints (also create indexes)
		`CREATE CONSTRAINT engram_id IF NOT EXISTS FOR (e:Engram) REQUIRE e.id IS UNIQUE`,
		`CREATE CONSTRAINT cue_id IF NOT EXISTS FOR (c:Cue) REQUIRE c.id IS UNIQUE`,
	}
	// Tenant-mode indexes for filtering by user
	if store.TenantUser() != "" {
		stmts = append(stmts,
			`CREATE INDEX engram_user_idx IF NOT EXISTS FOR (e:Engram) ON (e.user)`,
			`CREATE INDEX cue_user_idx IF NOT EXISTS FOR (c:Cue) ON (c.user)`,
		)
	}
	return stmts
}

func neo4jIndexStatements(dims int) []string {
	return []string{
		// Vector indexes for similarity search
		fmt.Sprintf(`CREATE VECTOR INDEX engram_embedding_idx IF NOT EXISTS
			FOR (e:Engram) ON (e.embedding)
			OPTIONS {indexConfig: {
				`+"`vector.dimensions`"+`: %d,
				`+"`vector.similarity_function`"+`: 'cosine'
			}}`, dims),
		fmt.Sprintf(`CREATE VECTOR INDEX cue_embedding_idx IF NOT EXISTS
			FOR (c:Cue) ON (c.embedding)
			OPTIONS {indexConfig: {
				`+"`vector.dimensions`"+`: %d,
				`+"`vector.similarity_function`"+`: 'cosine'
			}}`, dims),

		// Full-text search indexes
		`CREATE FULLTEXT INDEX engram_fts_idx IF NOT EXISTS
			FOR (e:Engram) ON EACH [e.content, e.summary, e.tags]`,
		`CREATE FULLTEXT INDEX cue_fts_idx IF NOT EXISTS
			FOR (c:Cue) ON EACH [c.name]`,
	}
}

func isNeo4jEquivalentError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "equivalent") ||
		strings.Contains(msg, "already exist") ||
		strings.Contains(msg, "constraint already") ||
		strings.Contains(msg, "index already")
}
