/* SPDX-License-Identifier: MPL-2.0
 * Copyright 2026 Tejus Pratap <tejzpr@gmail.com>
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/tejzpr/smriti-mcp/memory"
)

func SmritiStoreTool() mcp.Tool {
	return mcp.NewTool("smriti_store",
		mcp.WithDescription("Store a new memory. The content is analyzed, embedded, and linked to related memories automatically."),
		mcp.WithString("content", mcp.Required(), mcp.Description("The memory content to store")),
		mcp.WithNumber("importance", mcp.Description("Priority weight 0.0-1.0 (default: 0.5)")),
		mcp.WithString("tags", mcp.Description("Comma-separated tags")),
		mcp.WithString("source", mcp.Description("Source/origin of this memory")),
	)
}

func HandleSmritiStore(engine *memory.Engine) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		content, err := request.RequireString("content")
		if err != nil || content == "" {
			return mcp.NewToolResultError("content is required"), nil
		}

		importance := request.GetFloat("importance", 0.5)

		var tags []string
		tagsStr := request.GetString("tags", "")
		if tagsStr != "" {
			for _, t := range strings.Split(tagsStr, ",") {
				t = strings.TrimSpace(t)
				if t != "" {
					tags = append(tags, t)
				}
			}
		}

		source := request.GetString("source", "")

		engram, err := engine.Encode(ctx, memory.StoreRequest{
			Content:    content,
			Source:     source,
			Tags:       tags,
			Importance: importance,
		})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to store memory: %v", err)), nil
		}

		result := map[string]any{
			"id":          engram.ID,
			"summary":     engram.Summary,
			"memory_type": engram.MemoryType,
			"importance":  engram.Importance,
			"tags":        engram.Tags,
		}
		b, _ := json.Marshal(result)
		return mcp.NewToolResultText(string(b)), nil
	}
}
