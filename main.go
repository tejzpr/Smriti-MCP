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

	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755); err != nil {
		log.Fatalf("create db dir: %v", err)
	}

	store, err := db.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer store.Close()

	if err := db.InitSchema(store, cfg.EmbeddingDims); err != nil {
		log.Fatalf("init schema: %v", err)
	}

	llmClient := llm.NewClient(llm.ClientConfig{
		LLMBaseURL:   cfg.LLMBaseURL,
		LLMModel:     cfg.LLMModel,
		LLMAPIKey:    cfg.LLMAPIKey,
		EmbedBaseURL: cfg.EmbeddingBaseURL,
		EmbedModel:   cfg.EmbeddingModel,
		EmbedDims:    cfg.EmbeddingDims,
	})

	engine := memory.NewEngine(store, llmClient)
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

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("shutting down...")
		if err := bp.Push(context.Background()); err != nil {
			log.Printf("final backup push error: %v", err)
		}
		bp.Close()
		cancel()
		os.Exit(0)
	}()

	fmt.Fprintf(os.Stderr, "SmritiMCP server started for user=%s db=%s backup=%s\n", cfg.User, cfg.DBPath, cfg.BackupType)

	if err := server.ServeStdio(s); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
