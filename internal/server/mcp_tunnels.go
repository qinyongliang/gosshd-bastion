package server

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/qinyongliang/gosshd-bastion/internal/store"
)

type mcpTunnelIDInput struct {
	TunnelID string `json:"tunnel_id"`
}
type mcpTunnelListInput struct {
	OrganizationID string `json:"organization_id"`
}
type mcpTunnelSaveInput struct {
	OrganizationID  string `json:"organization_id"`
	Name            string `json:"name"`
	EntryTargetID   string `json:"entry_target_id,omitempty" jsonschema:"Entry machine ID; empty selects the bastion (system administrators only)."`
	ListenHost      string `json:"listen_host" jsonschema:"IP address to bind on the entry machine; use 127.0.0.1 for local access."`
	ListenPort      int    `json:"listen_port"`
	ExitTargetID    string `json:"exit_target_id,omitempty" jsonschema:"Exit machine ID; empty selects the bastion (system administrators only). SSH jump chains are supported."`
	DestinationHost string `json:"destination_host" jsonschema:"Destination resolved and accessed from the exit machine."`
	DestinationPort int    `json:"destination_port"`
	DurationSeconds int64  `json:"duration_seconds,omitempty" jsonschema:"Duration per enable; 0 is permanent; maximum 365 days. Reconnecting does not reset expiration."`
}
type mcpTunnelUpdateInput struct {
	TunnelID        string  `json:"tunnel_id"`
	Name            *string `json:"name,omitempty"`
	EntryTargetID   *string `json:"entry_target_id,omitempty"`
	ListenHost      *string `json:"listen_host,omitempty"`
	ListenPort      *int    `json:"listen_port,omitempty"`
	ExitTargetID    *string `json:"exit_target_id,omitempty"`
	DestinationHost *string `json:"destination_host,omitempty"`
	DestinationPort *int    `json:"destination_port,omitempty"`
	DurationSeconds *int64  `json:"duration_seconds,omitempty"`
}
type mcpTunnelOutput struct {
	Tunnel apiTunnel `json:"tunnel"`
}
type mcpTunnelsOutput struct {
	Tunnels []apiTunnel `json:"tunnels"`
}
type mcpTunnelTrafficInput struct {
	SourceIP string `json:"source_ip,omitempty" jsonschema:"Filter by IPv4 or IPv6 source address; unknown selects unavailable source addresses."`
	TunnelID string `json:"tunnel_id"`
	From     *int64 `json:"from,omitempty" jsonschema:"Inclusive Unix seconds; defaults to last 24 hours."`
	To       *int64 `json:"to,omitempty" jsonschema:"Exclusive Unix seconds; maximum range 31 days."`
}
type mcpTunnelTrafficOutput = tunnelTrafficStatistics

// Refresh the actor for every operation, including long-lived in-process MCP sessions.
func (a *App) tunnelActor(ctx context.Context, actor store.User) (store.User, error) {
	if err := a.ensureServices(ctx); err != nil {
		return store.User{}, err
	}
	user, err := a.store.Repository().GetUser(ctx, actor.ID)
	if err != nil {
		return store.User{}, err
	}
	if user.DisabledAt != nil {
		return store.User{}, errors.New("account disabled")
	}
	return user, nil
}
func (a *App) addMCPTunnelTools(s *mcp.Server, initialActor store.User) {
	mcp.AddTool(s, &mcp.Tool{Name: "tunnel_list", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}, Description: "List persistent tunnel configurations and live status for an organization. Requires organization administrator access. Results identify the creator and latest enabling user."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in mcpTunnelListInput) (*mcp.CallToolResult, mcpTunnelsOutput, error) {
			out := mcpTunnelsOutput{Tunnels: []apiTunnel{}}
			actor, err := a.tunnelActor(ctx, initialActor)
			if err != nil {
				return nil, out, err
			}
			if strings.TrimSpace(in.OrganizationID) == "" {
				return nil, out, errors.New("organization_id required")
			}
			if err := a.requireOrganizationAdmin(ctx, in.OrganizationID, actor); err != nil {
				return nil, out, err
			}
			out.Tunnels, err = a.listTunnelViews(ctx, actor, in.OrganizationID)
			if err != nil {
				return nil, out, err
			}
			return nil, out, nil
		})
	mcp.AddTool(s, &mcp.Tool{Name: "tunnel_get", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}, Description: "Get a tunnel configuration, creator, live connection counts, cumulative traffic, relay/direct state and actual ICE network interface/path diagnostics."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in mcpTunnelIDInput) (*mcp.CallToolResult, mcpTunnelOutput, error) {
			actor, err := a.tunnelActor(ctx, initialActor)
			if err != nil {
				return nil, mcpTunnelOutput{}, err
			}
			t, err := a.lookupTunnel(ctx, actor, in.TunnelID)
			if err != nil {
				return nil, mcpTunnelOutput{}, err
			}
			return nil, mcpTunnelOutput{a.tunnelView(t)}, nil
		})
	mcp.AddTool(s, &mcp.Tool{Name: "tunnel_create", Description: "Create a persistent TCP tunnel, initially stopped. Flow: client -> entry machine listening address -> exit machine -> destination. Entry/exit accept Agent or ordinary SSH machine IDs and SSH jump chains. Call tunnel_enable separately to start. Creator is always the authenticated user."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in mcpTunnelSaveInput) (*mcp.CallToolResult, mcpTunnelOutput, error) {
			actor, err := a.tunnelActor(ctx, initialActor)
			if err != nil {
				return nil, mcpTunnelOutput{}, err
			}
			t := store.Tunnel{OrganizationID: in.OrganizationID, CreatedBy: actor.ID, TunnelConfig: store.TunnelConfig{Name: in.Name, EntryTargetID: in.EntryTargetID, ListenHost: in.ListenHost, ListenPort: in.ListenPort, ExitTargetID: in.ExitTargetID, DestinationHost: in.DestinationHost, DestinationPort: in.DestinationPort, DurationSeconds: in.DurationSeconds}}
			saved, err := a.saveManagedTunnel(ctx, actor, t)
			return nil, mcpTunnelOutput{saved}, err
		})
	mcp.AddTool(s, &mcp.Tool{Name: "tunnel_update", Description: "Patch a tunnel configuration; omitted fields are retained. Organization and creator cannot be changed. Editing an enabled tunnel restarts it and closes existing connections; expiration is retained, and duration changes apply on the next enable."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in mcpTunnelUpdateInput) (*mcp.CallToolResult, mcpTunnelOutput, error) {
			actor, err := a.tunnelActor(ctx, initialActor)
			if err != nil {
				return nil, mcpTunnelOutput{}, err
			}
			t, err := a.managedTunnel(ctx, actor, in.TunnelID)
			if err != nil {
				return nil, mcpTunnelOutput{}, err
			}
			if in.Name != nil {
				t.Name = *in.Name
			}
			if in.EntryTargetID != nil {
				t.EntryTargetID = *in.EntryTargetID
			}
			if in.ListenHost != nil {
				t.ListenHost = *in.ListenHost
			}
			if in.ListenPort != nil {
				t.ListenPort = *in.ListenPort
			}
			if in.ExitTargetID != nil {
				t.ExitTargetID = *in.ExitTargetID
			}
			if in.DestinationHost != nil {
				t.DestinationHost = *in.DestinationHost
			}
			if in.DestinationPort != nil {
				t.DestinationPort = *in.DestinationPort
			}
			if in.DurationSeconds != nil {
				t.DurationSeconds = *in.DurationSeconds
			}
			saved, err := a.saveManagedTunnel(ctx, actor, t)
			return nil, mcpTunnelOutput{saved}, err
		})
	for _, operation := range []struct {
		name, description string
		enabled           bool
	}{
		{"tunnel_enable", "Enable or renew a tunnel using its configured duration. Renews expiration and closes existing connections when already enabled. Records the authenticated enabling user.", true},
		{"tunnel_stop", "Stop a tunnel immediately and close existing connections. Retains its persistent configuration and traffic history.", false},
	} {
		mcp.AddTool(s, &mcp.Tool{Name: operation.name, Description: operation.description},
			func(ctx context.Context, _ *mcp.CallToolRequest, in mcpTunnelIDInput) (*mcp.CallToolResult, mcpTunnelOutput, error) {
				actor, err := a.tunnelActor(ctx, initialActor)
				if err != nil {
					return nil, mcpTunnelOutput{}, err
				}
				if strings.HasPrefix(in.TunnelID, "ssh-") {
					if operation.enabled {
						return nil, mcpTunnelOutput{}, errors.New("temporary SSH tunnels cannot be enabled; recreate them from the SSH client")
					}
					temporary, err := a.temporaryTunnel(ctx, actor, in.TunnelID)
					if err != nil {
						return nil, mcpTunnelOutput{}, err
					}
					view := a.tunnelView(temporary.config)
					temporary.stop()
					view.Enabled = false
					view.Status = "stopped"
					return nil, mcpTunnelOutput{view}, nil
				}
				t, err := a.managedTunnel(ctx, actor, in.TunnelID)
				if err != nil {
					return nil, mcpTunnelOutput{}, err
				}
				saved, err := a.setManagedTunnelEnabled(ctx, actor, t, operation.enabled)
				return nil, mcpTunnelOutput{saved}, err
			})
	}
	mcp.AddTool(s, &mcp.Tool{Name: "tunnel_delete", Description: "Delete a persistent tunnel configuration and stop its active connections. Historical audit traffic remains retained."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in mcpTunnelIDInput) (*mcp.CallToolResult, mcpOK, error) {
			actor, err := a.tunnelActor(ctx, initialActor)
			if err != nil {
				return nil, mcpOK{}, err
			}
			t, err := a.managedTunnel(ctx, actor, in.TunnelID)
			if err != nil {
				return nil, mcpOK{}, err
			}
			if err := a.store.Repository().DeleteTunnel(ctx, t.ID); err != nil {
				return nil, mcpOK{}, err
			}
			a.tunnels.stop(t.ID)
			return nil, mcpOK{OK: true}, nil
		})
	mcp.AddTool(s, &mcp.Tool{Name: "tunnel_traffic", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}, Description: "Get five-minute traffic buckets and per-source-IP statistics including relay/direct upload/download bytes, opened connections, peak concurrency and current active connections. Optionally filter by source_ip. Includes unflushed live counters; defaults to last 24 hours, maximum 31 days."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in mcpTunnelTrafficInput) (*mcp.CallToolResult, mcpTunnelTrafficOutput, error) {
			actor, err := a.tunnelActor(ctx, initialActor)
			if err != nil {
				return nil, mcpTunnelTrafficOutput{}, err
			}
			t, err := a.lookupTunnel(ctx, actor, in.TunnelID)
			if err != nil {
				return nil, mcpTunnelTrafficOutput{}, err
			}
			now := time.Now().Unix()
			to := now - now%300 + 300
			from := to - 24*3600
			if in.From != nil {
				from = *in.From
			}
			if in.To != nil {
				to = *in.To
			}
			if from < 0 || to <= from || to-from > 31*24*3600+300 {
				return nil, mcpTunnelTrafficOutput{}, errors.New("time range must be between 0 and 31 days")
			}
			from -= from % 300
			sourceIP, err := parseTunnelSourceFilter(in.SourceIP)
			if err != nil {
				return nil, mcpTunnelTrafficOutput{}, err
			}
			statistics, err := a.tunnelTrafficStatistics(ctx, t, from, to, sourceIP)
			return nil, statistics, err
		})
}
