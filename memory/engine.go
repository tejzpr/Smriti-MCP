/* SPDX-License-Identifier: MPL-2.0
 * Copyright 2026 Tejus Pratap <tejzpr@gmail.com>
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package memory

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/tejzpr/smriti-mcp/db"
	"github.com/tejzpr/smriti-mcp/llm"
)

type leidenState struct {
	cachedResolution float64
	cachedNodeCount  int64
}

type Engine struct {
	store         db.Store
	llm           *llm.Client
	mu            sync.RWMutex
	stopCh        chan struct{}
	leiden        leidenState
	embeddingDims int
}

func NewEngine(store db.Store, llmClient *llm.Client, opts ...EngineOption) *Engine {
	e := &Engine{
		store:  store,
		llm:    llmClient,
		stopCh: make(chan struct{}),
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

type EngineOption func(*Engine)

func WithEmbeddingDims(dims int) EngineOption {
	return func(e *Engine) {
		e.embeddingDims = dims
	}
}

func (e *Engine) Store() db.Store {
	return e.store
}

func (e *Engine) LLM() *llm.Client {
	return e.llm
}

func (e *Engine) StartConsolidation(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := e.Consolidate(ctx); err != nil {
					log.Printf("consolidation error: %v", err)
				}
			case <-e.stopCh:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (e *Engine) Stop() {
	close(e.stopCh)
}
