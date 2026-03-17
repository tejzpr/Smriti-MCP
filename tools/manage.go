/* SPDX-License-Identifier: MPL-2.0
 * Copyright 2026 Tejus Pratap <tejzpr@gmail.com>
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package tools

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/tejzpr/smriti-mcp/backup"
	"github.com/tejzpr/smriti-mcp/memory"
)

func SmritiManageTool() mcp.Tool {
	return mcp.NewTool("smriti_manage",
		mcp.WithDescription("Administrative operations: forget a memory or sync to remote backup."),
		mcp.WithString("action", mcp.Required(), mcp.Description("'forget' or 'sync'")),
		mcp.WithString("memory_id", mcp.Description("Engram ID to delete (required for forget)")),
	)
}

func HandleSmritiManage(engine *memory.Engine, bp backup.Provider) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		action, err := request.RequireString("action")
		if err != nil {
			return mcp.NewToolResultError("action is required"), nil
		}

		switch action {
		case "forget":
			memoryID := request.GetString("memory_id", "")
			if memoryID == "" {
				return mcp.NewToolResultError("memory_id is required for forget action"), nil
			}
			if err := engine.Forget(memoryID); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("forget failed: %v", err)), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf(`{"status":"ok","deleted":"%s"}`, memoryID)), nil

		case "sync":
			if err := bp.Push(ctx); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("sync failed: %v", err)), nil
			}
			return mcp.NewToolResultText(`{"status":"ok","action":"sync"}`), nil

		default:
			return mcp.NewToolResultError(fmt.Sprintf("unknown action: %s", action)), nil
		}
	}
}
