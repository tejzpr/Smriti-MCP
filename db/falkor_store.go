/* SPDX-License-Identifier: MPL-2.0
 * Copyright 2026 Tejus Pratap <tejzpr@gmail.com>
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package db

import (
	"fmt"
	"sync"
	"time"

	"github.com/FalkorDB/falkordb-go/v2"
)

type FalkorStore struct {
	db         *falkordb.FalkorDB
	graph      *falkordb.Graph
	graphName  string
	addr       string
	mu         sync.Mutex
	tenantUser string // non-empty when using tenant-property isolation
}

type FalkorConfig struct {
	Addr      string // e.g. "localhost:6379"
	Password  string // optional
	GraphName string // graph key name, defaults to "smriti"
	Isolation string // "tenant" (default) or "graph" (per-graph isolation)
	User      string // ACCESSING_USER; used for tenant-property isolation
}

func OpenFalkor(cfg FalkorConfig) (*FalkorStore, error) {
	if cfg.GraphName == "" {
		cfg.GraphName = "smriti"
	}
	if cfg.Addr == "" {
		cfg.Addr = "localhost:6379"
	}

	opts := &falkordb.ConnectionOption{
		Addr:     cfg.Addr,
		Password: cfg.Password,
	}

	db, err := falkordb.FalkorDBNew(opts)
	if err != nil {
		return nil, fmt.Errorf("create falkordb connection: %w", err)
	}

	// Verify connectivity with a simple ping
	graph := db.SelectGraph(cfg.GraphName)
	_, err = graph.Query("RETURN 1", nil, nil)
	if err != nil {
		return nil, fmt.Errorf("falkordb connectivity check failed: %w", err)
	}

	tenantUser := ""
	if cfg.Isolation != "graph" {
		tenantUser = cfg.User
	}

	return &FalkorStore{
		db:         db,
		graph:      graph,
		graphName:  cfg.GraphName,
		addr:       cfg.Addr,
		tenantUser: tenantUser,
	}, nil
}

func (s *FalkorStore) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		s.db.Conn.Close()
		s.db = nil
	}
}

func (s *FalkorStore) Execute(query string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.graph.Query(query, nil, nil)
	if err != nil {
		return fmt.Errorf("execute query: %w", err)
	}
	return nil
}

func (s *FalkorStore) QueryRows(query string) ([]map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.queryRowsInternal(query, nil)
}

func (s *FalkorStore) QuerySingleValue(query string) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.queryRowsInternal(query, nil)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("query returned no rows")
	}
	for _, v := range rows[0] {
		return v, nil
	}
	return nil, fmt.Errorf("row has no columns")
}

func (s *FalkorStore) PreparedExecute(query string, params map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.graph.Query(query, params, nil)
	if err != nil {
		return fmt.Errorf("execute prepared query: %w", err)
	}
	return nil
}

func (s *FalkorStore) PreparedQueryRows(query string, params map[string]any) ([]map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.queryRowsInternal(query, params)
}

func (s *FalkorStore) Path() string {
	return "falkordb://" + s.addr + "/" + s.graphName
}

func (s *FalkorStore) DBType() string {
	return "falkordb"
}

func (s *FalkorStore) TenantUser() string {
	return s.tenantUser
}

// queryRowsInternal runs a query with optional params and collects all result
// rows as []map[string]any. Caller must hold s.mu.
func (s *FalkorStore) queryRowsInternal(query string, params map[string]any) ([]map[string]any, error) {
	result, err := s.graph.Query(query, params, nil)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}

	var rows []map[string]any
	for result.Next() {
		record := result.Record()
		row := make(map[string]any)
		for _, key := range record.Keys() {
			val, _ := record.Get(key)
			row[key] = falkorValueToGo(val)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// falkorValueToGo converts FalkorDB types to standard Go types.
func falkorValueToGo(val any) any {
	switch v := val.(type) {
	case time.Time:
		return v
	case *falkordb.Node:
		// Shouldn't happen in our queries, but handle gracefully
		props := make(map[string]any)
		for k, pv := range v.Properties {
			props[k] = falkorValueToGo(pv)
		}
		return props
	case *falkordb.Edge:
		props := make(map[string]any)
		for k, pv := range v.Properties {
			props[k] = falkorValueToGo(pv)
		}
		return props
	case []any:
		result := make([]any, len(v))
		for i, item := range v {
			result[i] = falkorValueToGo(item)
		}
		return result
	default:
		return val
	}
}
