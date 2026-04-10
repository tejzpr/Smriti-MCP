/* SPDX-License-Identifier: MPL-2.0
 * Copyright 2026 Tejus Pratap <tejzpr@gmail.com>
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package db

// Store is the interface that all database backends must implement.
// Queries use Cypher syntax. Implementations must handle dialect
// differences (e.g. schema DDL, parameter binding, vector indexes)
// internally or via the schema helpers.
type Store interface {
	// Execute runs a Cypher statement that returns no rows.
	Execute(query string) error

	// QueryRows runs a Cypher query and returns all result rows as maps.
	QueryRows(query string) ([]map[string]any, error)

	// QuerySingleValue runs a Cypher query and returns the first column
	// of the first row.
	QuerySingleValue(query string) (any, error)

	// PreparedExecute runs a parameterised Cypher statement with no rows.
	PreparedExecute(query string, params map[string]any) error

	// PreparedQueryRows runs a parameterised Cypher query and returns
	// all result rows as maps.
	PreparedQueryRows(query string, params map[string]any) ([]map[string]any, error)

	// Close releases all resources held by the store.
	Close()

	// Path returns a human-readable connection string or path.
	Path() string

	// DBType returns the backend type identifier ("ladybug" or "neo4j").
	DBType() string

	// TenantUser returns the tenant user identifier for multi-tenant
	// isolation. Returns "" when tenant filtering is not needed
	// (e.g. LadybugDB uses per-file isolation, Neo4j in "database" mode
	// uses per-database isolation).
	TenantUser() string
}
