/* SPDX-License-Identifier: MPL-2.0
 * Copyright 2026 Tejus Pratap <tejzpr@gmail.com>
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package db

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	lbug "github.com/LadybugDB/go-ladybug"
)

type Store struct {
	db   *lbug.Database
	conn *lbug.Connection
	path string
	mu   sync.Mutex
}

func Open(dbPath string) (*Store, error) {
	dir := filepath.Dir(dbPath)
	if dir != ":memory:" && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("create db directory: %w", err)
		}
	}

	config := lbug.DefaultSystemConfig()
	database, err := lbug.OpenDatabase(dbPath, config)
	if err != nil {
		return nil, fmt.Errorf("open database at %s: %w", dbPath, err)
	}

	conn, err := lbug.OpenConnection(database)
	if err != nil {
		database.Close()
		return nil, fmt.Errorf("open connection: %w", err)
	}

	return &Store{
		db:   database,
		conn: conn,
		path: dbPath,
	}, nil
}

func OpenInMemory() (*Store, error) {
	config := lbug.DefaultSystemConfig()
	database, err := lbug.OpenInMemoryDatabase(config)
	if err != nil {
		return nil, fmt.Errorf("open in-memory database: %w", err)
	}

	conn, err := lbug.OpenConnection(database)
	if err != nil {
		database.Close()
		return nil, fmt.Errorf("open connection: %w", err)
	}

	return &Store{
		db:   database,
		conn: conn,
		path: ":memory:",
	}, nil
}

func (s *Store) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != nil {
		s.conn.Close()
		s.conn = nil
	}
	if s.db != nil {
		s.db.Close()
		s.db = nil
	}
}

func (s *Store) Execute(query string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	result, err := s.conn.Query(query)
	if err != nil {
		return fmt.Errorf("execute query: %w", err)
	}
	defer result.Close()
	return nil
}

func (s *Store) Query(query string) (*lbug.QueryResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result, err := s.conn.Query(query)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	return result, nil
}

func (s *Store) QueryRows(query string) ([]map[string]any, error) {
	result, err := s.Query(query)
	if err != nil {
		return nil, err
	}
	defer result.Close()

	var rows []map[string]any
	for result.HasNext() {
		row, err := result.Next()
		if err != nil {
			return nil, fmt.Errorf("iterate rows: %w", err)
		}
		rowMap, err := row.GetAsMap()
		if err != nil {
			return nil, fmt.Errorf("convert row to map: %w", err)
		}
		rows = append(rows, rowMap)
	}
	return rows, nil
}

func (s *Store) PreparedExecute(query string, params map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	stmt, err := s.conn.Prepare(query)
	if err != nil {
		return fmt.Errorf("prepare query: %w", err)
	}
	defer stmt.Close()
	result, err := s.conn.Execute(stmt, params)
	if err != nil {
		return fmt.Errorf("execute prepared query: %w", err)
	}
	defer result.Close()
	return nil
}

func (s *Store) PreparedQueryRows(query string, params map[string]any) ([]map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stmt, err := s.conn.Prepare(query)
	if err != nil {
		return nil, fmt.Errorf("prepare query: %w", err)
	}
	defer stmt.Close()
	result, err := s.conn.Execute(stmt, params)
	if err != nil {
		return nil, fmt.Errorf("execute prepared query: %w", err)
	}
	defer result.Close()

	var rows []map[string]any
	for result.HasNext() {
		row, err := result.Next()
		if err != nil {
			return nil, fmt.Errorf("iterate rows: %w", err)
		}
		rowMap, err := row.GetAsMap()
		if err != nil {
			return nil, fmt.Errorf("convert row to map: %w", err)
		}
		rows = append(rows, rowMap)
	}
	return rows, nil
}

func (s *Store) QuerySingleValue(query string) (any, error) {
	result, err := s.Query(query)
	if err != nil {
		return nil, err
	}
	defer result.Close()

	if !result.HasNext() {
		return nil, fmt.Errorf("query returned no rows")
	}
	row, err := result.Next()
	if err != nil {
		return nil, fmt.Errorf("get row: %w", err)
	}
	val, err := row.GetAsSlice()
	if err != nil {
		return nil, fmt.Errorf("convert row to slice: %w", err)
	}
	if len(val) == 0 {
		return nil, fmt.Errorf("row has no columns")
	}
	return val[0], nil
}

func (s *Store) Path() string {
	return s.path
}

func (s *Store) DB() *lbug.Database {
	return s.db
}
