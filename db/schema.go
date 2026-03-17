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

func InitSchema(store *Store, embeddingDims int) error {
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
	return nil
}

func EnsureIndexes(store *Store) {
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
			tags STRING
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
