// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 Tejus Pratap <tejzpr@gmail.com>
//
// See LICENSE file for details.

package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/tejzpr/smriti-mcp/memory"
)

func SmritiRecallTool() mcp.Tool {
	return mcp.NewTool("smriti_recall",
		mcp.WithDescription("Retrieve memories. Provide a query for search/recall, or omit for listing."),
		mcp.WithString("query", mcp.Description("Natural language query. If omitted, returns recent memories.")),
		mcp.WithNumber("limit", mcp.Description("Max results (default: 5)")),
		mcp.WithString("mode", mcp.Description("'recall' (deep multi-hop, default), 'search' (fast single-pass), or 'list'")),
		mcp.WithString("memory_type", mcp.Description("Filter by memory type: episodic, semantic, procedural")),
	)
}

func HandleSmritiRecall(engine *memory.Engine) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		query := request.GetString("query", "")
		mode := request.GetString("mode", "")
		limit := int(request.GetFloat("limit", 5))

		if mode == "" {
			if query == "" {
				mode = "list"
			} else {
				mode = "recall"
			}
		}

		var results []memory.SearchResult
		var err error

		switch mode {
		case "recall":
			if query == "" {
				return mcp.NewToolResultError("query is required for recall mode"), nil
			}
			results, err = engine.Recall(ctx, memory.RecallRequest{
				Query: query,
				Limit: limit,
			})
		case "search":
			if query == "" {
				return mcp.NewToolResultError("query is required for search mode"), nil
			}
			results, err = engine.Search(ctx, memory.RecallRequest{
				Query: query,
				Mode:  "vector",
				Limit: limit,
			})
		case "list":
			results, err = engine.Search(ctx, memory.RecallRequest{
				Mode:  "list",
				Limit: limit,
			})
		default:
			return mcp.NewToolResultError(fmt.Sprintf("unknown mode: %s", mode)), nil
		}

		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("recall failed: %v", err)), nil
		}

		memoryType := request.GetString("memory_type", "")
		if memoryType != "" {
			filtered := make([]memory.SearchResult, 0)
			for _, r := range results {
				if r.Engram.MemoryType == memoryType {
					filtered = append(filtered, r)
				}
			}
			results = filtered
		}

		output := make([]map[string]any, 0, len(results))
		for _, r := range results {
			output = append(output, map[string]any{
				"id":          r.Engram.ID,
				"content":     r.Engram.Content,
				"summary":     r.Engram.Summary,
				"memory_type": r.Engram.MemoryType,
				"importance":  r.Engram.Importance,
				"score":       r.Score,
				"match_type":  r.MatchType,
				"tags":        r.Engram.Tags,
				"source":      r.Engram.Source,
				"created_at":  r.Engram.CreatedAt,
			})
		}

		b, _ := json.Marshal(output)
		return mcp.NewToolResultText(string(b)), nil
	}
}
