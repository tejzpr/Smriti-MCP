/* SPDX-License-Identifier: MPL-2.0
 * Copyright 2026 Tejus Pratap <tejzpr@gmail.com>
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package config

import (
	"os"
	"os/user"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func clearEnv() {
	envVars := []string{
		"ACCESSING_USER", "STORAGE_LOCATION", "BACKUP_TYPE", "BACKUP_SYNC_INTERVAL",
		"CONSOLIDATION_INTERVAL", "GIT_BASE_URL", "S3_ENDPOINT", "S3_REGION",
		"S3_ACCESS_KEY", "S3_SECRET_KEY", "LLM_BASE_URL", "LLM_API_KEY", "LLM_MODEL",
		"EMBEDDING_BASE_URL", "EMBEDDING_API_KEY", "EMBEDDING_MODEL", "EMBEDDING_DIMS",
		"DB_TYPE", "NEO4J_URI", "NEO4J_USERNAME", "NEO4J_PASSWORD", "NEO4J_DATABASE",
		"NEO4J_ISOLATION",
	}
	for _, v := range envVars {
		os.Unsetenv(v)
	}
}

func TestLoadFromEnv_Defaults(t *testing.T) {
	clearEnv()
	defer clearEnv()

	cfg, err := LoadFromEnv()
	require.NoError(t, err)

	u, _ := user.Current()
	homeDir, _ := os.UserHomeDir()

	assert.Equal(t, u.Username, cfg.User)
	assert.Equal(t, filepath.Join(homeDir, ".smriti", u.Username), cfg.LocalPath)
	assert.Equal(t, filepath.Join(homeDir, ".smriti", u.Username, "memory.lbug"), cfg.DBPath)
	assert.Equal(t, "none", cfg.BackupType)
	assert.Equal(t, 60, cfg.BackupSyncInterval)
	assert.Equal(t, 3600, cfg.ConsolidationInterval)
	assert.Equal(t, "", cfg.GitBaseURL)
	assert.Equal(t, "https://api.openai.com/v1", cfg.LLMBaseURL)
	assert.Equal(t, "", cfg.LLMAPIKey)
	assert.Equal(t, "gpt-4o-mini", cfg.LLMModel)
	assert.Equal(t, "https://api.openai.com/v1", cfg.EmbeddingBaseURL)
	assert.Equal(t, "", cfg.EmbeddingAPIKey)
	assert.Equal(t, "text-embedding-3-small", cfg.EmbeddingModel)
	assert.Equal(t, 0, cfg.EmbeddingDims)
	assert.True(t, cfg.EmbeddingDimsAutoDetect)
}

func TestLoadFromEnv_CustomUser(t *testing.T) {
	clearEnv()
	defer clearEnv()

	os.Setenv("ACCESSING_USER", "alice")
	os.Setenv("STORAGE_LOCATION", "/tmp/smriti-test")

	cfg, err := LoadFromEnv()
	require.NoError(t, err)

	assert.Equal(t, "alice", cfg.User)
	assert.Equal(t, "/tmp/smriti-test/alice", cfg.LocalPath)
	assert.Equal(t, "/tmp/smriti-test/alice/memory.lbug", cfg.DBPath)
}

func TestLoadFromEnv_GitHubBackup(t *testing.T) {
	clearEnv()
	defer clearEnv()

	os.Setenv("BACKUP_TYPE", "github")
	os.Setenv("GIT_BASE_URL", "git@github.com:myorg")
	os.Setenv("ACCESSING_USER", "bob")

	cfg, err := LoadFromEnv()
	require.NoError(t, err)

	assert.Equal(t, "github", cfg.BackupType)
	assert.Equal(t, "git@github.com:myorg", cfg.GitBaseURL)
	assert.Equal(t, "git@github.com:myorg/bob_smriti", cfg.GitRepoURL())
}

func TestLoadFromEnv_GitHubMissingBaseURL(t *testing.T) {
	clearEnv()
	defer clearEnv()

	os.Setenv("BACKUP_TYPE", "github")

	_, err := LoadFromEnv()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "GIT_BASE_URL is required")
}

func TestLoadFromEnv_S3Backup(t *testing.T) {
	clearEnv()
	defer clearEnv()

	os.Setenv("BACKUP_TYPE", "s3")
	os.Setenv("S3_REGION", "us-east-1")
	os.Setenv("S3_ACCESS_KEY", "AKIAIOSFODNN7EXAMPLE")
	os.Setenv("S3_SECRET_KEY", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY")
	os.Setenv("S3_ENDPOINT", "https://minio.local:9000")
	os.Setenv("ACCESSING_USER", "carol")

	cfg, err := LoadFromEnv()
	require.NoError(t, err)

	assert.Equal(t, "s3", cfg.BackupType)
	assert.Equal(t, "us-east-1", cfg.S3Region)
	assert.Equal(t, "AKIAIOSFODNN7EXAMPLE", cfg.S3AccessKey)
	assert.Equal(t, "https://minio.local:9000", cfg.S3Endpoint)
	assert.Equal(t, "carol_smriti", cfg.S3Bucket())
}

func TestLoadFromEnv_S3MissingRegion(t *testing.T) {
	clearEnv()
	defer clearEnv()

	os.Setenv("BACKUP_TYPE", "s3")
	os.Setenv("S3_ACCESS_KEY", "key")
	os.Setenv("S3_SECRET_KEY", "secret")

	_, err := LoadFromEnv()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "S3_REGION is required")
}

func TestLoadFromEnv_S3MissingAccessKey(t *testing.T) {
	clearEnv()
	defer clearEnv()

	os.Setenv("BACKUP_TYPE", "s3")
	os.Setenv("S3_REGION", "us-east-1")
	os.Setenv("S3_SECRET_KEY", "secret")

	_, err := LoadFromEnv()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "S3_ACCESS_KEY is required")
}

func TestLoadFromEnv_S3MissingSecretKey(t *testing.T) {
	clearEnv()
	defer clearEnv()

	os.Setenv("BACKUP_TYPE", "s3")
	os.Setenv("S3_REGION", "us-east-1")
	os.Setenv("S3_ACCESS_KEY", "key")

	_, err := LoadFromEnv()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "S3_SECRET_KEY is required")
}

func TestLoadFromEnv_InvalidBackupType(t *testing.T) {
	clearEnv()
	defer clearEnv()

	os.Setenv("BACKUP_TYPE", "ftp")

	_, err := LoadFromEnv()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid BACKUP_TYPE")
}

func TestLoadFromEnv_CustomLLMConfig(t *testing.T) {
	clearEnv()
	defer clearEnv()

	os.Setenv("LLM_BASE_URL", "https://llm.local/v1")
	os.Setenv("LLM_API_KEY", "sk-test-key")
	os.Setenv("LLM_MODEL", "gpt-4")

	cfg, err := LoadFromEnv()
	require.NoError(t, err)

	assert.Equal(t, "https://llm.local/v1", cfg.LLMBaseURL)
	assert.Equal(t, "sk-test-key", cfg.LLMAPIKey)
	assert.Equal(t, "gpt-4", cfg.LLMModel)
}

func TestLoadFromEnv_CustomEmbeddingConfig(t *testing.T) {
	clearEnv()
	defer clearEnv()

	os.Setenv("EMBEDDING_BASE_URL", "https://embed.local/v1")
	os.Setenv("EMBEDDING_API_KEY", "sk-embed-key")
	os.Setenv("EMBEDDING_MODEL", "text-embedding-ada-002")
	os.Setenv("EMBEDDING_DIMS", "768")

	cfg, err := LoadFromEnv()
	require.NoError(t, err)

	assert.Equal(t, "https://embed.local/v1", cfg.EmbeddingBaseURL)
	assert.Equal(t, "sk-embed-key", cfg.EmbeddingAPIKey)
	assert.Equal(t, "text-embedding-ada-002", cfg.EmbeddingModel)
	assert.Equal(t, 768, cfg.EmbeddingDims)
	assert.False(t, cfg.EmbeddingDimsAutoDetect)
}

func TestLoadFromEnv_InvalidEmbeddingDims(t *testing.T) {
	clearEnv()
	defer clearEnv()

	os.Setenv("EMBEDDING_DIMS", "notanumber")
	_, err := LoadFromEnv()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid EMBEDDING_DIMS")
}

func TestLoadFromEnv_ZeroEmbeddingDims(t *testing.T) {
	clearEnv()
	defer clearEnv()

	os.Setenv("EMBEDDING_DIMS", "0")
	_, err := LoadFromEnv()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "EMBEDDING_DIMS must be > 0")
}

func TestLoadFromEnv_InvalidBackupSyncInterval(t *testing.T) {
	clearEnv()
	defer clearEnv()

	os.Setenv("BACKUP_SYNC_INTERVAL", "-1")
	_, err := LoadFromEnv()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "BACKUP_SYNC_INTERVAL must be >= 0")
}

func TestLoadFromEnv_InvalidConsolidationInterval(t *testing.T) {
	clearEnv()
	defer clearEnv()

	os.Setenv("CONSOLIDATION_INTERVAL", "abc")
	_, err := LoadFromEnv()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid CONSOLIDATION_INTERVAL")
}

func TestLoadFromEnv_CustomIntervals(t *testing.T) {
	clearEnv()
	defer clearEnv()

	os.Setenv("BACKUP_SYNC_INTERVAL", "120")
	os.Setenv("CONSOLIDATION_INTERVAL", "7200")

	cfg, err := LoadFromEnv()
	require.NoError(t, err)

	assert.Equal(t, 120, cfg.BackupSyncInterval)
	assert.Equal(t, 7200, cfg.ConsolidationInterval)
}

func TestLoadFromEnv_DisabledBackupSync(t *testing.T) {
	clearEnv()
	defer clearEnv()

	os.Setenv("BACKUP_SYNC_INTERVAL", "0")

	cfg, err := LoadFromEnv()
	require.NoError(t, err)

	assert.Equal(t, 0, cfg.BackupSyncInterval)
}

func TestLoadFromEnv_BackupTypeCaseInsensitive(t *testing.T) {
	clearEnv()
	defer clearEnv()

	os.Setenv("BACKUP_TYPE", "GitHub")
	os.Setenv("GIT_BASE_URL", "git@github.com:myorg")

	cfg, err := LoadFromEnv()
	require.NoError(t, err)
	assert.Equal(t, "github", cfg.BackupType)
}

func TestGitRepoURL_NotGitHub(t *testing.T) {
	cfg := &Config{BackupType: "none"}
	assert.Equal(t, "", cfg.GitRepoURL())
}

func TestS3Bucket_NotS3(t *testing.T) {
	cfg := &Config{BackupType: "none"}
	assert.Equal(t, "", cfg.S3Bucket())
}

func TestLoadFromEnv_DBTypeDefaults(t *testing.T) {
	clearEnv()
	defer clearEnv()

	cfg, err := LoadFromEnv()
	require.NoError(t, err)

	assert.Equal(t, "ladybug", cfg.DBType)
	assert.Equal(t, "tenant", cfg.Neo4jIsolation)
	assert.Equal(t, "neo4j", cfg.Neo4jDatabase)
}

func TestLoadFromEnv_InvalidDBType(t *testing.T) {
	clearEnv()
	defer clearEnv()

	os.Setenv("DB_TYPE", "postgres")

	_, err := LoadFromEnv()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid DB_TYPE")
}

func TestLoadFromEnv_Neo4jRequiresURI(t *testing.T) {
	clearEnv()
	defer clearEnv()

	os.Setenv("DB_TYPE", "neo4j")
	os.Setenv("NEO4J_USERNAME", "neo4j")
	os.Setenv("NEO4J_PASSWORD", "secret")

	_, err := LoadFromEnv()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "NEO4J_URI is required")
}

func TestLoadFromEnv_Neo4jRequiresUsername(t *testing.T) {
	clearEnv()
	defer clearEnv()

	os.Setenv("DB_TYPE", "neo4j")
	os.Setenv("NEO4J_URI", "bolt://localhost:7687")
	os.Setenv("NEO4J_PASSWORD", "secret")

	_, err := LoadFromEnv()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "NEO4J_USERNAME is required")
}

func TestLoadFromEnv_Neo4jRequiresPassword(t *testing.T) {
	clearEnv()
	defer clearEnv()

	os.Setenv("DB_TYPE", "neo4j")
	os.Setenv("NEO4J_URI", "bolt://localhost:7687")
	os.Setenv("NEO4J_USERNAME", "neo4j")

	_, err := LoadFromEnv()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "NEO4J_PASSWORD is required")
}

func TestLoadFromEnv_InvalidNeo4jIsolation(t *testing.T) {
	clearEnv()
	defer clearEnv()

	os.Setenv("NEO4J_ISOLATION", "magic")

	_, err := LoadFromEnv()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid NEO4J_ISOLATION")
}

func TestLoadFromEnv_Neo4jIsolationTenant(t *testing.T) {
	clearEnv()
	defer clearEnv()

	os.Setenv("DB_TYPE", "neo4j")
	os.Setenv("NEO4J_URI", "bolt://localhost:7687")
	os.Setenv("NEO4J_USERNAME", "neo4j")
	os.Setenv("NEO4J_PASSWORD", "secret")
	os.Setenv("NEO4J_ISOLATION", "tenant")
	os.Setenv("ACCESSING_USER", "alice")

	cfg, err := LoadFromEnv()
	require.NoError(t, err)

	assert.Equal(t, "tenant", cfg.Neo4jIsolation)
	assert.Equal(t, "neo4j", cfg.Neo4jDatabase) // unchanged
}

func TestLoadFromEnv_Neo4jIsolationDatabase(t *testing.T) {
	clearEnv()
	defer clearEnv()

	os.Setenv("DB_TYPE", "neo4j")
	os.Setenv("NEO4J_URI", "bolt://localhost:7687")
	os.Setenv("NEO4J_USERNAME", "neo4j")
	os.Setenv("NEO4J_PASSWORD", "secret")
	os.Setenv("NEO4J_ISOLATION", "database")
	os.Setenv("ACCESSING_USER", "alice")

	cfg, err := LoadFromEnv()
	require.NoError(t, err)

	assert.Equal(t, "database", cfg.Neo4jIsolation)
	assert.Equal(t, "alice", cfg.Neo4jDatabase) // auto-set from user
}

func TestLoadFromEnv_Neo4jIsolationDatabaseCustomDB(t *testing.T) {
	clearEnv()
	defer clearEnv()

	os.Setenv("DB_TYPE", "neo4j")
	os.Setenv("NEO4J_URI", "bolt://localhost:7687")
	os.Setenv("NEO4J_USERNAME", "neo4j")
	os.Setenv("NEO4J_PASSWORD", "secret")
	os.Setenv("NEO4J_ISOLATION", "database")
	os.Setenv("NEO4J_DATABASE", "custom_db")
	os.Setenv("ACCESSING_USER", "alice")

	cfg, err := LoadFromEnv()
	require.NoError(t, err)

	assert.Equal(t, "database", cfg.Neo4jIsolation)
	assert.Equal(t, "custom_db", cfg.Neo4jDatabase) // user-specified, not overridden
}
