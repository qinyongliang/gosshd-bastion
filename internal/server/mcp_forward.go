package server

import (
	"context"
	"errors"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/qinyongliang/gosshd-bastion/internal/forward"
	"github.com/qinyongliang/gosshd-bastion/internal/store"
)

type mcpForwardLogsInput struct {
	TunnelID string `json:"tunnel_id"`
	forward.LogFilter
}

func (a *App) addMCPForwardTools(s *mcp.Server, initial store.User) {
	mcp.AddTool(s, &mcp.Tool{Name: "forward_logs", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}, Description: "Query independent per-configuration port (TCP/UDP) and HTTP access logs and lifetime totals. Includes source IP, request host/path/query, matched server/location, upstream, response status, actual payload bytes, duration and concurrency; filter by source_ip/path/route_id/status and Unix-millisecond from/to. before is an exclusive sequence cursor; limit defaults to 100, maximum 500. Agent data is periodically reported; last_report and edge_stats expose staleness/dropped writes. Same organization-admin permissions as tunnel tools."}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpForwardLogsInput) (*mcp.CallToolResult, forwardLogsResponse, error) {
		actor, err := a.tunnelActor(ctx, initial)
		if err != nil {
			return nil, forwardLogsResponse{}, err
		}
		out, err := a.forwardLogs(ctx, actor, in.TunnelID, in.LogFilter)
		return nil, out, err
	})
	mcp.AddTool(s, &mcp.Tool{Name: "forward_log_settings_get", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}, Description: "Read global forwarding access-log retention, default seven days. Does not prune lifetime traffic/request counters. System administrators only."}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, forwardSettings, error) {
		actor, err := a.tunnelActor(ctx, initial)
		if err != nil {
			return nil, forwardSettings{}, err
		}
		if !actor.IsSystemAdmin {
			return nil, forwardSettings{}, errors.New("system administrator required")
		}
		out, err := a.loadForwardSettings(ctx)
		return nil, out, err
	})
	mcp.AddTool(s, &mcp.Tool{Name: "forward_log_settings_update", Description: "Set global access-log retention_days (1–365, default 7), propagated to Agents. Does not remove lifetime bandwidth/request totals. System administrators only."}, func(ctx context.Context, _ *mcp.CallToolRequest, in forwardSettings) (*mcp.CallToolResult, forwardSettings, error) {
		actor, err := a.tunnelActor(ctx, initial)
		if err != nil {
			return nil, forwardSettings{}, err
		}
		err = a.saveForwardSettings(ctx, actor, in)
		return nil, in, err
	})
}
