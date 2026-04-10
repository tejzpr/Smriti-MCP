/* SPDX-License-Identifier: MPL-2.0
 * Copyright 2026 Tejus Pratap <tejzpr@gmail.com>
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package config

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	// User
	User      string
	LocalPath string // {STORAGE_LOCATION}/{User}/
	DBPath    string // {LocalPath}/memory.lbug

	// Database backend
	DBType string // "ladybug" (default), "neo4j", or "falkordb"

	// Neo4j connection (only when DBType="neo4j")
	Neo4jURI       string
	Neo4jUsername  string
	Neo4jPassword  string
	Neo4jDatabase  string
	Neo4jIsolation string // "tenant" (default) or "database"

	// FalkorDB connection (only when DBType="falkordb")
	FalkorAddr      string
	FalkorPassword  string
	FalkorGraphName string
	FalkorIsolation string // "tenant" (default) or "graph"

	// Backup
	BackupType         string // "none", "github", "s3"
	BackupSyncInterval int    // seconds, 0 = disabled

	// Consolidation
	ConsolidationInterval int // seconds

	// GitHub backup
	GitBaseURL string

	// S3 backup
	S3Endpoint  string
	S3Region    string
	S3AccessKey string
	S3SecretKey string

	// LLM
	LLMBaseURL string
	LLMAPIKey  string
	LLMModel   string

	// Embedding
	EmbeddingBaseURL        string
	EmbeddingAPIKey         string
	EmbeddingModel          string
	EmbeddingDims           int
	EmbeddingDimsAutoDetect bool
}

func LoadFromEnv() (*Config, error) {
	cfg := &Config{}

	// User resolution
	cfg.User = os.Getenv("ACCESSING_USER")
	if cfg.User == "" {
		u, err := user.Current()
		if err != nil {
			return nil, fmt.Errorf("failed to get current user: %w", err)
		}
		cfg.User = u.Username
	}

	// Storage location
	storageLocation := os.Getenv("STORAGE_LOCATION")
	if storageLocation == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		storageLocation = filepath.Join(homeDir, ".smriti")
	}
	cfg.LocalPath = filepath.Join(storageLocation, cfg.User)
	cfg.DBPath = filepath.Join(cfg.LocalPath, "memory.lbug")

	// Database type
	cfg.DBType = strings.ToLower(os.Getenv("DB_TYPE"))
	if cfg.DBType == "" {
		cfg.DBType = "ladybug"
	}
	if cfg.DBType != "ladybug" && cfg.DBType != "neo4j" && cfg.DBType != "falkordb" {
		return nil, fmt.Errorf("invalid DB_TYPE: %q (must be ladybug, neo4j, or falkordb)", cfg.DBType)
	}

	// Neo4j connection config
	cfg.Neo4jURI = os.Getenv("NEO4J_URI")
	cfg.Neo4jUsername = os.Getenv("NEO4J_USERNAME")
	cfg.Neo4jPassword = os.Getenv("NEO4J_PASSWORD")
	cfg.Neo4jDatabase = os.Getenv("NEO4J_DATABASE")
	if cfg.Neo4jDatabase == "" {
		cfg.Neo4jDatabase = "neo4j"
	}
	// Neo4j isolation mode
	cfg.Neo4jIsolation = strings.ToLower(os.Getenv("NEO4J_ISOLATION"))
	if cfg.Neo4jIsolation == "" {
		cfg.Neo4jIsolation = "tenant"
	}
	if cfg.Neo4jIsolation != "tenant" && cfg.Neo4jIsolation != "database" {
		return nil, fmt.Errorf("invalid NEO4J_ISOLATION: %q (must be tenant or database)", cfg.Neo4jIsolation)
	}
	if cfg.DBType == "neo4j" {
		if cfg.Neo4jURI == "" {
			return nil, fmt.Errorf("NEO4J_URI is required when DB_TYPE=neo4j")
		}
		if cfg.Neo4jUsername == "" {
			return nil, fmt.Errorf("NEO4J_USERNAME is required when DB_TYPE=neo4j")
		}
		if cfg.Neo4jPassword == "" {
			return nil, fmt.Errorf("NEO4J_PASSWORD is required when DB_TYPE=neo4j")
		}
		// In database isolation mode, use user name as the database
		if cfg.Neo4jIsolation == "database" && cfg.Neo4jDatabase == "neo4j" {
			cfg.Neo4jDatabase = cfg.User
		}
	}

	// FalkorDB connection config
	cfg.FalkorAddr = os.Getenv("FALKOR_ADDR")
	if cfg.FalkorAddr == "" {
		cfg.FalkorAddr = "localhost:6379"
	}
	cfg.FalkorPassword = os.Getenv("FALKOR_PASSWORD")
	cfg.FalkorGraphName = os.Getenv("FALKOR_GRAPH")
	if cfg.FalkorGraphName == "" {
		cfg.FalkorGraphName = "smriti"
	}
	cfg.FalkorIsolation = strings.ToLower(os.Getenv("FALKOR_ISOLATION"))
	if cfg.FalkorIsolation == "" {
		cfg.FalkorIsolation = "tenant"
	}
	if cfg.FalkorIsolation != "tenant" && cfg.FalkorIsolation != "graph" {
		return nil, fmt.Errorf("invalid FALKOR_ISOLATION: %q (must be tenant or graph)", cfg.FalkorIsolation)
	}
	if cfg.DBType == "falkordb" {
		// In graph isolation mode, use user name as the graph name
		if cfg.FalkorIsolation == "graph" && cfg.FalkorGraphName == "smriti" {
			cfg.FalkorGraphName = cfg.User + "_smriti"
		}
	}

	// Backup type
	cfg.BackupType = strings.ToLower(os.Getenv("BACKUP_TYPE"))
	if cfg.BackupType == "" {
		cfg.BackupType = "none"
	}
	if cfg.BackupType != "none" && cfg.BackupType != "github" && cfg.BackupType != "s3" {
		return nil, fmt.Errorf("invalid BACKUP_TYPE: %q (must be none, github, or s3)", cfg.BackupType)
	}

	// Backup sync interval
	cfg.BackupSyncInterval = 60
	if v := os.Getenv("BACKUP_SYNC_INTERVAL"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("invalid BACKUP_SYNC_INTERVAL: %w", err)
		}
		if n < 0 {
			return nil, fmt.Errorf("BACKUP_SYNC_INTERVAL must be >= 0, got %d", n)
		}
		cfg.BackupSyncInterval = n
	}

	// Consolidation interval
	cfg.ConsolidationInterval = 3600
	if v := os.Getenv("CONSOLIDATION_INTERVAL"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("invalid CONSOLIDATION_INTERVAL: %w", err)
		}
		if n < 0 {
			return nil, fmt.Errorf("CONSOLIDATION_INTERVAL must be >= 0, got %d", n)
		}
		cfg.ConsolidationInterval = n
	}

	// GitHub backup config
	cfg.GitBaseURL = os.Getenv("GIT_BASE_URL")
	if cfg.BackupType == "github" && cfg.GitBaseURL == "" {
		return nil, fmt.Errorf("GIT_BASE_URL is required when BACKUP_TYPE=github")
	}

	// S3 backup config
	cfg.S3Endpoint = os.Getenv("S3_ENDPOINT")
	cfg.S3Region = os.Getenv("S3_REGION")
	cfg.S3AccessKey = os.Getenv("S3_ACCESS_KEY")
	cfg.S3SecretKey = os.Getenv("S3_SECRET_KEY")
	if cfg.BackupType == "s3" {
		if cfg.S3Region == "" {
			return nil, fmt.Errorf("S3_REGION is required when BACKUP_TYPE=s3")
		}
		if cfg.S3AccessKey == "" {
			return nil, fmt.Errorf("S3_ACCESS_KEY is required when BACKUP_TYPE=s3")
		}
		if cfg.S3SecretKey == "" {
			return nil, fmt.Errorf("S3_SECRET_KEY is required when BACKUP_TYPE=s3")
		}
	}

	// LLM config
	cfg.LLMBaseURL = os.Getenv("LLM_BASE_URL")
	if cfg.LLMBaseURL == "" {
		cfg.LLMBaseURL = "https://api.openai.com/v1"
	}
	cfg.LLMAPIKey = os.Getenv("LLM_API_KEY")
	cfg.LLMModel = os.Getenv("LLM_MODEL")
	if cfg.LLMModel == "" {
		cfg.LLMModel = "gpt-4o-mini"
	}

	// Embedding config
	cfg.EmbeddingBaseURL = os.Getenv("EMBEDDING_BASE_URL")
	if cfg.EmbeddingBaseURL == "" {
		cfg.EmbeddingBaseURL = "https://api.openai.com/v1"
	}
	cfg.EmbeddingAPIKey = os.Getenv("EMBEDDING_API_KEY")
	cfg.EmbeddingModel = os.Getenv("EMBEDDING_MODEL")
	if cfg.EmbeddingModel == "" {
		cfg.EmbeddingModel = "text-embedding-3-small"
	}
	cfg.EmbeddingDims = 0
	cfg.EmbeddingDimsAutoDetect = true
	if v := os.Getenv("EMBEDDING_DIMS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("invalid EMBEDDING_DIMS: %w", err)
		}
		if n <= 0 {
			return nil, fmt.Errorf("EMBEDDING_DIMS must be > 0, got %d", n)
		}
		cfg.EmbeddingDims = n
		cfg.EmbeddingDimsAutoDetect = false
	}

	return cfg, nil
}

func (c *Config) GitRepoURL() string {
	if c.BackupType != "github" {
		return ""
	}
	return c.GitBaseURL + "/" + c.User + "_smriti"
}

func (c *Config) S3Bucket() string {
	if c.BackupType != "s3" {
		return ""
	}
	return c.User + "_smriti"
}
