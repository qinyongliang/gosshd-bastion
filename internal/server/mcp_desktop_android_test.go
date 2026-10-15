package server

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/qinyongliang/gosshd-bastion/internal/store"
)

func TestDesktopAndroidPlatformAndMCPIsolation(t *testing.T) {
	for _, platform := range []string{"android", "linux"} {
		t.Run(platform, func(t *testing.T) {
			app, user, target := desktopTestApp(t)
			fakeDesktopAgentPlatform(t, app, target.AgentID, platform)
			ctx := context.Background()
			if app.authorizeDesktop(ctx, user.ID, target.ID, false) == nil {
				t.Fatal("platform bypassed desktop grant")
			}
			grant := store.DesktopAccess{UserID: user.ID, TargetID: target.ID, AllowRead: true, AllowControl: false, GrantedBy: user.ID}
			if err := app.store.Repository().SetDesktopAccess(ctx, grant); err != nil {
				t.Fatal(err)
			}
			err := app.authorizeDesktop(ctx, user.ID, target.ID, false)
			if platform == "linux" {
				if err == nil {
					t.Fatal("unsupported platform accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if app.authorizeDesktop(ctx, user.ID, target.ID, true) == nil {
				t.Fatal("Android read-only grant permitted control")
			}
			client := desktopMCPClient(t, app, user)
			opened, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "desktop_session_open", Arguments: map[string]any{"target_id": target.ID}})
			if err != nil || opened.IsError {
				t.Fatalf("Android session: %v %+v", err, opened)
			}
			id := stringField(t, opened.StructuredContent, "session_id")
			result, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "desktop_get_window_state", Arguments: map[string]any{"session_id": id, "params": map[string]any{"window": map[string]any{"id": "10", "app": "20"}}}})
			if err != nil || result.IsError {
				t.Fatalf("Android observation: %v %+v", err, result)
			}
			images := 0
			for _, c := range result.Content {
				if _, ok := c.(*mcp.ImageContent); ok {
					images++
				}
			}
			if images != 1 {
				t.Fatal("Android MCP image missing")
			}
			result, err = client.CallTool(ctx, &mcp.CallToolParams{Name: "desktop_type_text", Arguments: map[string]any{"session_id": id, "params": map[string]any{"window": map[string]any{"id": "10", "app": "20"}, "snapshot_id": "test-snapshot", "text": "example"}}})
			if err != nil || !result.IsError {
				t.Fatal("Android control bypassed grant")
			}
			if err = app.authorizeDesktop(ctx, user.ID, target.ID, false); err != nil {
				t.Fatal(err)
			}
		})
	}
}
