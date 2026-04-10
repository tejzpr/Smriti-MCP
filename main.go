/* SPDX-License-Identifier: MPL-2.0
 * Copyright 2026 Tejus Pratap <tejzpr@gmail.com>
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/mark3labs/mcp-go/server"
	"github.com/tejzpr/smriti-mcp/backup"
	"github.com/tejzpr/smriti-mcp/config"
	"github.com/tejzpr/smriti-mcp/db"
	"github.com/tejzpr/smriti-mcp/llm"
	"github.com/tejzpr/smriti-mcp/memory"
	"github.com/tejzpr/smriti-mcp/tools"
)

func main() {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	// Auto-detect embedding dimensions if not explicitly set
	if cfg.EmbeddingDimsAutoDetect {
		log.Println("EMBEDDING_DIMS not set, probing embedding API for dimensions...")
		probeClient := llm.NewClient(llm.ClientConfig{
			EmbedBaseURL: cfg.EmbeddingBaseURL,
			EmbedAPIKey:  cfg.EmbeddingAPIKey,
			EmbedModel:   cfg.EmbeddingModel,
		})
		probeCtx, probeCancel := context.WithTimeout(context.Background(), 30*time.Second)
		dims, probeErr := probeClient.ProbeEmbeddingDims(probeCtx)
		probeCancel()
		if probeErr != nil {
			log.Fatalf("failed to auto-detect embedding dimensions: %v (set EMBEDDING_DIMS env var to skip)", probeErr)
		}
		cfg.EmbeddingDims = dims
		log.Printf("auto-detected embedding dimensions: %d", dims)
	}

	var store db.Store
	switch cfg.DBType {
	case "neo4j":
		neo4jStore, err := db.OpenNeo4j(db.Neo4jConfig{
			URI:       cfg.Neo4jURI,
			Username:  cfg.Neo4jUsername,
			Password:  cfg.Neo4jPassword,
			Database:  cfg.Neo4jDatabase,
			Isolation: cfg.Neo4jIsolation,
			User:      cfg.User,
		})
		if err != nil {
			log.Fatalf("open neo4j: %v", err)
		}
		store = neo4jStore
	case "falkordb":
		falkorStore, err := db.OpenFalkor(db.FalkorConfig{
			Addr:      cfg.FalkorAddr,
			Password:  cfg.FalkorPassword,
			GraphName: cfg.FalkorGraphName,
			Isolation: cfg.FalkorIsolation,
			User:      cfg.User,
		})
		if err != nil {
			log.Fatalf("open falkordb: %v", err)
		}
		store = falkorStore
	default:
		if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755); err != nil {
			log.Fatalf("create db dir: %v", err)
		}
		lbugStore, err := db.Open(cfg.DBPath)
		if err != nil {
			log.Fatalf("open db: %v", err)
		}
		store = lbugStore
	}
	defer store.Close()

	switch cfg.DBType {
	case "neo4j":
		if err := db.InitSchemaNeo4j(store, cfg.EmbeddingDims); err != nil {
			log.Fatalf("init neo4j schema: %v", err)
		}
		db.MigrateSchemaNeo4j(store)
	case "falkordb":
		if err := db.InitSchemaFalkor(store, cfg.EmbeddingDims); err != nil {
			log.Fatalf("init falkordb schema: %v", err)
		}
		db.MigrateSchemaFalkor(store)
	default:
		if err := db.InitSchema(store, cfg.EmbeddingDims); err != nil {
			log.Fatalf("init schema: %v", err)
		}
		db.MigrateSchema(store)
	}

	if err := db.CheckEmbeddingDims(store, cfg.EmbeddingDims); err != nil {
		log.Fatalf("schema check: %v", err)
	}

	llmClient := llm.NewClient(llm.ClientConfig{
		LLMBaseURL:   cfg.LLMBaseURL,
		LLMModel:     cfg.LLMModel,
		LLMAPIKey:    cfg.LLMAPIKey,
		EmbedBaseURL: cfg.EmbeddingBaseURL,
		EmbedAPIKey:  cfg.EmbeddingAPIKey,
		EmbedModel:   cfg.EmbeddingModel,
		EmbedDims:    cfg.EmbeddingDims,
	})

	engine := memory.NewEngine(store, llmClient, memory.WithEmbeddingDims(cfg.EmbeddingDims))
	defer engine.Stop()

	bp := backup.New(cfg.BackupType, cfg.LocalPath, cfg.User, map[string]string{
		"git_base_url": cfg.GitBaseURL,
		"s3_endpoint":  cfg.S3Endpoint,
		"s3_region":    cfg.S3Region,
		"s3_bucket":    cfg.S3Bucket(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := bp.Init(ctx); err != nil {
		log.Printf("backup init warning: %v", err)
	}
	if err := bp.Pull(ctx); err != nil {
		log.Printf("backup pull warning: %v", err)
	}

	if cfg.ConsolidationInterval > 0 {
		engine.StartConsolidation(ctx, time.Duration(cfg.ConsolidationInterval)*time.Second)
	}

	if cfg.BackupSyncInterval > 0 && cfg.BackupType != "none" {
		go func() {
			ticker := time.NewTicker(time.Duration(cfg.BackupSyncInterval) * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					if err := bp.Push(ctx); err != nil {
						log.Printf("backup sync error: %v", err)
					}
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	s := server.NewMCPServer(
		"SmritiMCP",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	s.AddTool(tools.SmritiStoreTool(), tools.HandleSmritiStore(engine))
	s.AddTool(tools.SmritiRecallTool(), tools.HandleSmritiRecall(engine))
	s.AddTool(tools.SmritiManageTool(), tools.HandleSmritiManage(engine, bp))

	var shutdownOnce sync.Once
	shutdown := func() {
		shutdownOnce.Do(func() {
			log.Println("shutting down...")
			cancel()
			engine.Stop()
			if err := bp.Push(context.Background()); err != nil {
				log.Printf("final backup push error: %v", err)
			}
			bp.Close()
			store.Close()
			log.Println("shutdown complete")
		})
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		shutdown()
		os.Exit(0)
	}()

	isolationInfo := ""
	switch cfg.DBType {
	case "neo4j":
		isolationInfo = fmt.Sprintf(" isolation=%s", cfg.Neo4jIsolation)
	case "falkordb":
		isolationInfo = fmt.Sprintf(" isolation=%s", cfg.FalkorIsolation)
	}
	fmt.Fprintf(os.Stderr, "SmritiMCP server started for user=%s db_type=%s db=%s%s backup=%s\n", cfg.User, cfg.DBType, store.Path(), isolationInfo, cfg.BackupType)

	if err := server.ServeStdio(s); err != nil {
		log.Printf("server error: %v", err)
	}
	shutdown()
}
