/* SPDX-License-Identifier: MPL-2.0
 * Copyright 2026 Tejus Pratap <tejzpr@gmail.com>
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package memory

import (
	"fmt"

	"github.com/tejzpr/smriti-mcp/db"
)

// tsFunc returns the Cypher function name for converting a datetime string
// to a temporal value.
//
//	LadybugDB:  timestamp('2006-01-02 15:04:05')
//	Neo4j:      localdatetime('2006-01-02T15:04:05')
func tsFunc(store db.Store) string {
	switch store.DBType() {
	case "neo4j", "falkordb":
		return "localdatetime"
	default:
		return "timestamp"
	}
}

// tsFormat returns the Go time.Format layout string appropriate for the
// database backend's temporal parsing.
//
//	LadybugDB: "2006-01-02 15:04:05"  (space separator)
//	Neo4j:     "2006-01-02T15:04:05"  (ISO 8601 with T)
func tsFormat(store db.Store) string {
	switch store.DBType() {
	case "neo4j", "falkordb":
		return "2006-01-02T15:04:05"
	default:
		return "2006-01-02 15:04:05"
	}
}

// vectorSearchQuery returns the Cypher query for a vector similarity search.
//
//	LadybugDB uses:  CALL QUERY_VECTOR_INDEX(label, index, embedding, k)
//	Neo4j uses:      CALL db.index.vector.queryNodes(index, k, embedding)
func vectorSearchQuery(store db.Store, indexName string, embStr string, limit int) string {
	switch store.DBType() {
	case "neo4j":
		return fmt.Sprintf(
			`CALL db.index.vector.queryNodes('%s', %d, %s)
			YIELD node, score
			RETURN node.id AS id, node.content AS content, node.summary AS summary,
				node.memory_type AS memory_type, node.importance AS importance,
				node.access_count AS access_count, node.decay_factor AS decay_factor,
				node.embedding AS embedding, node.source AS source, node.tags AS tags,
				node.created_at AS created_at, node.last_accessed_at AS last_accessed_at,
				node.cluster_id AS cluster_id,
				score AS distance
			ORDER BY score DESC`, indexName, limit, embStr)
	case "falkordb":
		return fmt.Sprintf(
			`CALL db.idx.vector.queryNodes('Engram', 'embedding', %d, vecf32(%s))
			YIELD node, score
			RETURN node.id AS id, node.content AS content, node.summary AS summary,
				node.memory_type AS memory_type, node.importance AS importance,
				node.access_count AS access_count, node.decay_factor AS decay_factor,
				node.embedding AS embedding, node.source AS source, node.tags AS tags,
				node.created_at AS created_at, node.last_accessed_at AS last_accessed_at,
				node.cluster_id AS cluster_id,
				score AS distance
			ORDER BY score DESC`, limit, embStr)
	default:
		// LadybugDB
		return fmt.Sprintf(
			`CALL QUERY_VECTOR_INDEX('Engram', '%s', %s, %d)
			RETURN node.id AS id, node.content AS content, node.summary AS summary,
				node.memory_type AS memory_type, node.importance AS importance,
				node.access_count AS access_count, node.decay_factor AS decay_factor,
				node.embedding AS embedding, node.source AS source, node.tags AS tags,
				node.created_at AS created_at, node.last_accessed_at AS last_accessed_at,
				node.cluster_id AS cluster_id,
				distance
			ORDER BY distance`, indexName, embStr, limit)
	}
}

// ftsSearchQuery returns the Cypher query for a full-text search.
//
//	LadybugDB uses:  CALL query_fts_index(label, index, query)
//	Neo4j uses:      CALL db.index.fulltext.queryNodes(index, query)
func ftsSearchQuery(store db.Store, indexName string, searchTerm string, limit int) string {
	switch store.DBType() {
	case "neo4j":
		return fmt.Sprintf(
			`CALL db.index.fulltext.queryNodes('%s', '%s')
			YIELD node, score
			RETURN node.id AS id, node.content AS content, node.summary AS summary,
				node.memory_type AS memory_type, node.importance AS importance,
				node.access_count AS access_count, node.decay_factor AS decay_factor,
				node.embedding AS embedding, node.source AS source, node.tags AS tags,
				node.created_at AS created_at, node.last_accessed_at AS last_accessed_at,
				node.cluster_id AS cluster_id,
				score
			ORDER BY score DESC
			LIMIT %d`, indexName, escapeCypher(searchTerm), limit)
	case "falkordb":
		return fmt.Sprintf(
			`CALL db.idx.fulltext.queryNodes('Engram', '%s')
			YIELD node, score
			RETURN node.id AS id, node.content AS content, node.summary AS summary,
				node.memory_type AS memory_type, node.importance AS importance,
				node.access_count AS access_count, node.decay_factor AS decay_factor,
				node.embedding AS embedding, node.source AS source, node.tags AS tags,
				node.created_at AS created_at, node.last_accessed_at AS last_accessed_at,
				node.cluster_id AS cluster_id,
				score
			ORDER BY score DESC
			LIMIT %d`, escapeCypher(searchTerm), limit)
	default:
		// LadybugDB
		return fmt.Sprintf(
			`CALL query_fts_index('Engram', '%s', '%s')
			RETURN node.id AS id, node.content AS content, node.summary AS summary,
				node.memory_type AS memory_type, node.importance AS importance,
				node.access_count AS access_count, node.decay_factor AS decay_factor,
				node.embedding AS embedding, node.source AS source, node.tags AS tags,
				node.created_at AS created_at, node.last_accessed_at AS last_accessed_at,
				node.cluster_id AS cluster_id,
				score
			ORDER BY score DESC
			LIMIT %d`, indexName, escapeCypher(searchTerm), limit)
	}
}

// lenFunc returns the Cypher function for array length.
//
//	LadybugDB:  len(x)
//	Neo4j:      size(x)
func lenFunc(store db.Store) string {
	switch store.DBType() {
	case "neo4j", "falkordb":
		return "size"
	default:
		return "len"
	}
}

// embeddingLiteral wraps an embedding array string for the target database.
//
//	LadybugDB / Neo4j: [0.1, 0.2, ...]
//	FalkorDB:          vecf32([0.1, 0.2, ...])
func embeddingLiteral(store db.Store, embStr string) string {
	if store.DBType() == "falkordb" {
		return "vecf32(" + embStr + ")"
	}
	return embStr
}

// escapeCypher escapes single quotes in a string for use in Cypher literals.
func escapeCypher(s string) string {
	result := ""
	for _, c := range s {
		if c == '\'' {
			result += "\\'"
		} else {
			result += string(c)
		}
	}
	return result
}

// isTenant returns true if the store is using tenant-property isolation.
func isTenant(store db.Store) bool {
	return store.TenantUser() != ""
}

// tenantFilter returns a WHERE clause fragment to filter by user.
// If not in tenant mode, returns "".
// Usage: "MATCH (e:Engram)" + tenantFilter(store, "e") + " RETURN ..."
func tenantFilter(store db.Store, alias string) string {
	if !isTenant(store) {
		return ""
	}
	return fmt.Sprintf(" WHERE %s.user = $__tenant_user", alias)
}

// tenantFilterAnd returns an AND clause fragment to append to an existing WHERE.
// If not in tenant mode, returns "".
func tenantFilterAnd(store db.Store, alias string) string {
	if !isTenant(store) {
		return ""
	}
	return fmt.Sprintf(" AND %s.user = $__tenant_user", alias)
}

// tenantProp returns the property assignment fragment for CREATE statements.
// If not in tenant mode, returns "".
// Usage: fmt.Sprintf("CREATE (e:Engram {id: $id, %s ...})", tenantProp(store))
func tenantProp(store db.Store) string {
	if !isTenant(store) {
		return ""
	}
	return "user: $__tenant_user,\n\t\t"
}

// tenantParam adds the __tenant_user parameter to a params map.
// If not in tenant mode, returns the map unchanged.
// If params is nil and tenant is needed, creates a new map.
func tenantParam(store db.Store, params map[string]any) map[string]any {
	if !isTenant(store) {
		return params
	}
	if params == nil {
		params = make(map[string]any)
	}
	params["__tenant_user"] = store.TenantUser()
	return params
}
