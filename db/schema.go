/* SPDX-License-Identifier: MPL-2.0
 * Copyright 2026 Tejus Pratap <tejzpr@gmail.com>
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package db

import (
	"fmt"
	"strings"
)

const IndexThreshold = 50

func InitSchema(store Store, embeddingDims int) error {
	statements := schemaStatements(embeddingDims)
	for _, stmt := range statements {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if err := store.Execute(stmt); err != nil {
			if isAlreadyExistsError(err) {
				continue
			}
			return fmt.Errorf("schema init failed on %q: %w", truncate(stmt, 80), err)
		}
	}
	// Add user column for tenant-property isolation
	if store.TenantUser() != "" {
		for _, stmt := range []string{
			`ALTER TABLE Engram ADD user STRING DEFAULT ''`,
			`ALTER TABLE Cue ADD user STRING DEFAULT ''`,
		} {
			if err := store.Execute(stmt); err != nil {
				if isAlreadyExistsError(err) || isPropertyError(err) {
					continue
				}
			}
		}
	}
	return nil
}

func MigrateSchema(store Store) {
	migrations := []string{
		`ALTER TABLE Engram ADD cluster_id INT64 DEFAULT -1`,
	}
	// Add user columns if tenant isolation is active
	if store.TenantUser() != "" {
		migrations = append(migrations,
			`ALTER TABLE Engram ADD user STRING DEFAULT ''`,
			`ALTER TABLE Cue ADD user STRING DEFAULT ''`,
		)
	}
	for _, stmt := range migrations {
		if err := store.Execute(stmt); err != nil {
			if isAlreadyExistsError(err) || isPropertyError(err) {
				continue
			}
		}
	}
}

func isPropertyError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "property") || strings.Contains(msg, "column") || strings.Contains(msg, "exist")
}

// CheckEmbeddingDims verifies that the configured embedding dimensions match
// the existing database schema. Returns nil if the DB is empty or dims match.
// Returns an error with a clear message if there's a mismatch.
func CheckEmbeddingDims(store Store, expectedDims int) error {
	lenFn := "len"
	if store.DBType() == "neo4j" {
		lenFn = "size"
	}
	rows, err := store.QueryRows(fmt.Sprintf(`MATCH (e:Engram) WHERE e.embedding IS NOT NULL RETURN %s(e.embedding) AS dims LIMIT 1`, lenFn))
	if err != nil {
		// Table might not exist yet or query not supported — skip check
		return nil
	}
	if len(rows) == 0 {
		// No engrams yet — nothing to check
		return nil
	}
	actualDims := rows[0]["dims"]
	var actual int64
	switch v := actualDims.(type) {
	case int64:
		actual = v
	case int:
		actual = int64(v)
	case float64:
		actual = int64(v)
	default:
		return nil
	}
	if actual > 0 && actual != int64(expectedDims) {
		return fmt.Errorf(
			"embedding dimension mismatch: database has %d-dim embeddings but EMBEDDING_DIMS=%d. "+
				"Delete the database at the configured path to recreate with the correct dimensions, "+
				"or set EMBEDDING_DIMS=%d to match the existing database",
			actual, expectedDims, actual)
	}
	return nil
}

func EnsureIndexes(store Store) {
	count, err := store.QuerySingleValue("MATCH (e:Engram) RETURN count(e)")
	if err != nil {
		return
	}
	n, ok := count.(int64)
	if !ok || n < int64(IndexThreshold) {
		return
	}
	for _, stmt := range indexStatements() {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if err := store.Execute(stmt); err != nil {
			if isAlreadyExistsError(err) || isIndexError(err) {
				continue
			}
		}
	}
}

func schemaStatements(dims int) []string {
	dimStr := fmt.Sprintf("%d", dims)
	return []string{
		// Node tables
		`CREATE NODE TABLE IF NOT EXISTS Engram(
			id STRING PRIMARY KEY,
			content STRING,
			summary STRING,
			memory_type STRING,
			importance DOUBLE,
			access_count INT64,
			created_at TIMESTAMP,
			last_accessed_at TIMESTAMP,
			decay_factor DOUBLE,
			embedding FLOAT[` + dimStr + `],
			source STRING,
			tags STRING,
			cluster_id INT64
		)`,
		`CREATE NODE TABLE IF NOT EXISTS Cue(
			id STRING PRIMARY KEY,
			name STRING,
			cue_type STRING,
			embedding FLOAT[` + dimStr + `]
		)`,

		// Relationship tables
		`CREATE REL TABLE IF NOT EXISTS EncodedBy(FROM Engram TO Cue, strength DOUBLE, created_at TIMESTAMP)`,
		`CREATE REL TABLE IF NOT EXISTS AssociatedWith(FROM Engram TO Engram, relation_type STRING, strength DOUBLE, created_at TIMESTAMP)`,
		`CREATE REL TABLE IF NOT EXISTS CoOccurs(FROM Cue TO Cue, weight DOUBLE)`,

		// Extensions
		`INSTALL vector`,
		`LOAD EXTENSION vector`,
		`INSTALL fts`,
		`LOAD EXTENSION fts`,
	}
}

func indexStatements() []string {
	return []string{
		`CALL CREATE_VECTOR_INDEX('Engram', 'engram_embedding_idx', 'embedding')`,
		`CALL CREATE_VECTOR_INDEX('Cue', 'cue_embedding_idx', 'embedding')`,
		`CALL CREATE_FTS_INDEX('Engram', 'engram_fts_idx', ['content', 'summary', 'tags'])`,
		`CALL CREATE_FTS_INDEX('Cue', 'cue_fts_idx', ['name'])`,
	}
}

func isIndexError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "index") ||
		strings.Contains(msg, "empty") ||
		strings.Contains(msg, "no data")
}

func isAlreadyExistsError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already exists") ||
		strings.Contains(msg, "duplicate") ||
		strings.Contains(msg, "already loaded") ||
		strings.Contains(msg, "already installed")
}

func truncate(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}
