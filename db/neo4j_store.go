/* SPDX-License-Identifier: MPL-2.0
 * Copyright 2026 Tejus Pratap <tejzpr@gmail.com>
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package db

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

type Neo4jStore struct {
	driver     neo4j.DriverWithContext
	dbName     string
	uri        string
	mu         sync.Mutex
	tenantUser string // non-empty when using tenant-property isolation
}

type Neo4jConfig struct {
	URI       string // e.g. "bolt://localhost:7687" or "neo4j://localhost:7687"
	Username  string
	Password  string
	Database  string // optional, defaults to "neo4j"
	Isolation string // "tenant" (default) or "database"
	User      string // the ACCESSING_USER; used for tenant-property isolation
}

func OpenNeo4j(cfg Neo4jConfig) (*Neo4jStore, error) {
	if cfg.Database == "" {
		cfg.Database = "neo4j"
	}

	driver, err := neo4j.NewDriverWithContext(cfg.URI, neo4j.BasicAuth(cfg.Username, cfg.Password, ""))
	if err != nil {
		return nil, fmt.Errorf("create neo4j driver: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := driver.VerifyConnectivity(ctx); err != nil {
		driver.Close(ctx)
		return nil, fmt.Errorf("neo4j connectivity check failed: %w", err)
	}

	tenantUser := ""
	if cfg.Isolation != "database" {
		tenantUser = cfg.User
	}

	return &Neo4jStore{
		driver:     driver,
		dbName:     cfg.Database,
		uri:        cfg.URI,
		tenantUser: tenantUser,
	}, nil
}

func (s *Neo4jStore) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.driver != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.driver.Close(ctx)
		s.driver = nil
	}
}

func (s *Neo4jStore) Execute(query string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := context.Background()
	session := s.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: s.dbName})
	defer session.Close(ctx)

	_, err := session.Run(ctx, query, nil)
	if err != nil {
		return fmt.Errorf("execute query: %w", err)
	}
	return nil
}

func (s *Neo4jStore) QueryRows(query string) ([]map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.queryRowsInternal(query, nil)
}

func (s *Neo4jStore) QuerySingleValue(query string) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.queryRowsInternal(query, nil)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("query returned no rows")
	}
	// Return the first value from the first row
	for _, v := range rows[0] {
		return v, nil
	}
	return nil, fmt.Errorf("row has no columns")
}

func (s *Neo4jStore) PreparedExecute(query string, params map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := context.Background()
	session := s.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: s.dbName})
	defer session.Close(ctx)

	_, err := session.Run(ctx, query, params)
	if err != nil {
		return fmt.Errorf("execute prepared query: %w", err)
	}
	return nil
}

func (s *Neo4jStore) PreparedQueryRows(query string, params map[string]any) ([]map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.queryRowsInternal(query, params)
}

func (s *Neo4jStore) Path() string {
	return s.uri + "/" + s.dbName
}

func (s *Neo4jStore) DBType() string {
	return "neo4j"
}

func (s *Neo4jStore) TenantUser() string {
	return s.tenantUser
}

// queryRowsInternal runs a query with optional params and collects all result
// rows as []map[string]any. Caller must hold s.mu.
func (s *Neo4jStore) queryRowsInternal(query string, params map[string]any) ([]map[string]any, error) {
	ctx := context.Background()
	session := s.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: s.dbName})
	defer session.Close(ctx)

	result, err := session.Run(ctx, query, params)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}

	var rows []map[string]any
	for result.Next(ctx) {
		record := result.Record()
		row := make(map[string]any, len(record.Keys))
		for i, key := range record.Keys {
			val := record.Values[i]
			row[key] = neo4jValueToGo(val)
		}
		rows = append(rows, row)
	}
	if err := result.Err(); err != nil {
		return nil, fmt.Errorf("iterate rows: %w", err)
	}
	return rows, nil
}

// neo4jValueToGo converts Neo4j driver types to standard Go types for
// compatibility with the rest of the codebase.
func neo4jValueToGo(val any) any {
	switch v := val.(type) {
	case neo4j.LocalDateTime:
		return v.Time()
	case neo4j.Date:
		return v.Time()
	case neo4j.Time:
		return v.Time()
	case neo4j.Duration:
		return v.String()
	case []any:
		// Convert nested slices (e.g. embeddings)
		result := make([]any, len(v))
		for i, item := range v {
			result[i] = neo4jValueToGo(item)
		}
		return result
	default:
		return val
	}
}
