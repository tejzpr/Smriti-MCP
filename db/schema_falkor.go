// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 Tejus Pratap <tejzpr@gmail.com>
//
// See LICENSE file for details.

package db

import (
	"fmt"
	"strings"
)

// InitSchemaFalkor creates the necessary indexes for FalkorDB.
// FalkorDB is schemaless — nodes and relationships are created dynamically.
// We create range indexes for fast lookups and vector/fulltext indexes.
func InitSchemaFalkor(store Store, embeddingDims int) error {
	statements := falkorSchemaStatements(store, embeddingDims)
	for _, stmt := range statements {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if err := store.Execute(stmt); err != nil {
			if isFalkorAlreadyExistsError(err) {
				continue
			}
			return fmt.Errorf("falkordb schema init failed on %q: %w", truncate(stmt, 80), err)
		}
	}
	return nil
}

// MigrateSchemaFalkor runs FalkorDB-specific migrations.
// FalkorDB is schema-free for node properties — no ALTER needed.
func MigrateSchemaFalkor(store Store) {
	// Schema-free: new properties are added automatically when first SET.
}

// EnsureIndexesFalkor creates vector and full-text indexes for FalkorDB
// once the data size warrants them.
func EnsureIndexesFalkor(store Store, embeddingDims int) {
	count, err := store.QuerySingleValue("MATCH (e:Engram) RETURN count(e)")
	if err != nil {
		return
	}
	n, ok := count.(int64)
	if !ok || n < int64(IndexThreshold) {
		return
	}
	for _, stmt := range falkorIndexStatements(embeddingDims) {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if err := store.Execute(stmt); err != nil {
			if isFalkorAlreadyExistsError(err) {
				continue
			}
		}
	}
}

func falkorSchemaStatements(store Store, _ int) []string {
	stmts := []string{
		// Range indexes for fast lookups on id property
		`CREATE INDEX FOR (e:Engram) ON (e.id)`,
		`CREATE INDEX FOR (c:Cue) ON (c.id)`,
	}
	// Tenant-mode indexes for filtering by user
	if store.TenantUser() != "" {
		stmts = append(stmts,
			`CREATE INDEX FOR (e:Engram) ON (e.user)`,
			`CREATE INDEX FOR (c:Cue) ON (c.user)`,
		)
	}
	return stmts
}

func falkorIndexStatements(dims int) []string {
	return []string{
		// Vector indexes for similarity search
		fmt.Sprintf(`CREATE VECTOR INDEX FOR (e:Engram) ON (e.embedding) OPTIONS {dimension: %d, similarityFunction: 'cosine'}`, dims),
		fmt.Sprintf(`CREATE VECTOR INDEX FOR (c:Cue) ON (c.embedding) OPTIONS {dimension: %d, similarityFunction: 'cosine'}`, dims),

		// Full-text search indexes
		`CALL db.idx.fulltext.createNodeIndex('Engram', 'content', 'summary', 'tags')`,
		`CALL db.idx.fulltext.createNodeIndex('Cue', 'name')`,
	}
}

func isFalkorAlreadyExistsError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already exist") ||
		strings.Contains(msg, "already indexed") ||
		strings.Contains(msg, "index already")
}
