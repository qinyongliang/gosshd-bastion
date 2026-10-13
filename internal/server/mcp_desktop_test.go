package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"github.com/hashicorp/yamux"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/qinyongliang/gosshd-bastion/internal/desktop"
	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
	"github.com/qinyongliang/gosshd-bastion/internal/store"
	"image"
	"image/png"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func desktopTestApp(t *testing.T) (*App, store.User, store.SSHTarget) {
	t.Helper()
	app := NewApp(Config{DatabasePath: filepath.Join(t.TempDir(), "main.db"), BootstrapAdminPassword: "test-password"})
	if err := app.ensureServices(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	user, err := app.store.Repository().GetUserByEmail(context.Background(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	org, err := app.store.Repository().GetPersonalOrganizationForUser(context.Background(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	target, err := app.store.Repository().CreateSSHTarget(context.Background(), store.CreateSSHTargetParams{OwnerType: store.OwnerOrganization, OwnerID: org.ID, Alias: "desktop-test", TargetType: store.TargetAgent, AgentID: "desktop-test-agent", CreatedBy: user.ID})
	if err != nil {
		t.Fatal(err)
	}
	return app, user, target
}
func fakeDesktopAgent(t *testing.T, app *App, id string) {
	t.Helper()
	a, b := net.Pipe()
	server, err := yamux.Server(a, nil)
	if err != nil {
		t.Fatal(err)
	}
	client, err := yamux.Client(b, nil)
	if err != nil {
		t.Fatal(err)
	}
	app.registry.RegisterWithInfo(id, server, AgentRegistryInfo{GOOS: "windows", GOARCH: "amd64"})
	var workers sync.WaitGroup
	workers.Add(1)
	go func() {
		defer workers.Done()
		for {
			stream, err := client.AcceptStream()
			if err != nil {
				return
			}
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer stream.Close()
				reader := bufio.NewReader(stream)
				req, err := protocol.ReadJSONLine[protocol.StreamRequest](reader)
				if err != nil || req.Type != protocol.StreamDesktop {
					return
				}
				encoder := json.NewEncoder(stream)
				_ = encoder.Encode(desktop.Response{ID: "init", OK: true})
				scan := bufio.NewScanner(reader)
				for scan.Scan() {
					var req desktop.Request
					if json.Unmarshal(scan.Bytes(), &req) != nil {
						return
					}
					var result any = map[string]any{"ok": true}
					if req.Method == "status" {
						result = map[string]any{"platform": "windows"}
					}
					if req.Method == "get_window_state" {
						var data bytes.Buffer
						_ = png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 2, 2)))
						result = desktop.State{SnapshotID: "test-snapshot", Window: *req.Params.Window, Image: &desktop.Image{MIMEType: "image/png", Data: data.Bytes(), Width: 2, Height: 2}}
					}
					wire, _ := json.Marshal(result)
					if encoder.Encode(desktop.Response{ID: req.ID, OK: true, Result: wire}) != nil {
						return
					}
				}
			}()
		}
	}()
	t.Cleanup(func() { app.registry.Unregister(id, server); server.Close(); client.Close(); workers.Wait() })
}
func desktopMCPClient(t *testing.T, app *App, actor store.User) *mcp.ClientSession {
	t.Helper()
	server := app.newMCPServer(mcpActor{User: actor, ToolGroups: map[string]bool{"desktop": true}})
	st, ct := mcp.NewInMemoryTransports()
	ss, err := server.Connect(context.Background(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "desktop-integration"}, nil)
	cs, err := client.Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close(); ss.Close() })
	return cs
}
func TestDesktopMCPGrantControlRevocationAndAudit(t *testing.T) {
	app, user, target := desktopTestApp(t)
	fakeDesktopAgent(t, app, target.AgentID)
	client := desktopMCPClient(t, app, user)
	ctx := context.Background()
	call := func(name string, args map[string]any, wantError bool) *mcp.CallToolResult {
		t.Helper()
		res, err := client.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		if res.IsError != wantError {
			t.Fatalf("%s unexpected result %+v", name, res)
		}
		return res
	}
	tools, err := client.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !mcpHasTool(tools, "desktop_get_window_state") || !mcpHasTool(tools, "desktop_perform_action") || mcpHasTool(tools, "session_send_command") || mcpHasTool(tools, "auth_register") {
		t.Fatal("desktop tool group is not independent")
	}
	call("desktop_session_open", map[string]any{"target_id": target.ID}, true)
	grant := map[string]any{"target_id": target.ID, "user_id": user.ID, "allow_read": true, "allow_control": false}
	call("desktop_access_set", grant, false)
	opened := call("desktop_session_open", map[string]any{"target_id": target.ID}, false)
	sessionID := stringField(t, opened.StructuredContent, "session_id")
	params := map[string]any{"window": map[string]any{"id": "10", "app": "20"}, "include_text": true}
	state := call("desktop_get_window_state", map[string]any{"session_id": sessionID, "params": params}, false)
	imageCount := 0
	for _, c := range state.Content {
		if _, ok := c.(*mcp.ImageContent); ok {
			imageCount++
		}
	}
	if imageCount != 1 {
		t.Fatal("MCP screenshot image not returned")
	}
	params["snapshot_id"] = "test-snapshot"
	params["text"] = "private typed text must not be audited"
	call("desktop_type_text", map[string]any{"session_id": sessionID, "params": params}, true)
	grant["allow_control"] = true
	call("desktop_access_set", grant, false)
	opened = call("desktop_session_open", map[string]any{"target_id": target.ID}, false)
	sessionID = stringField(t, opened.StructuredContent, "session_id")
	call("desktop_get_window_state", map[string]any{"session_id": sessionID, "params": params}, false)
	call("desktop_type_text", map[string]any{"session_id": sessionID, "params": params}, false)
	grant["allow_read"] = false
	grant["allow_control"] = false
	call("desktop_access_set", grant, false)
	call("desktop_status", map[string]any{"session_id": sessionID, "params": map[string]any{}}, true)
	audits := call("desktop_audit_list", map[string]any{}, false)
	wire, _ := json.Marshal(audits.StructuredContent)
	if strings.Contains(string(wire), "private typed text") || strings.Contains(string(wire), "data") {
		t.Fatal("desktop content leaked into audit")
	}
	if !strings.Contains(string(wire), "desktop_type_text") && !strings.Contains(string(wire), "type_text") {
		t.Fatal("control audit missing")
	}
}
func TestDesktopAccessIsolationDisabledUsersAndExpiry(t *testing.T) {
	app, admin, target := desktopTestApp(t)
	ctx := context.Background()
	outsider, err := app.store.Repository().CreateUser(ctx, store.CreateUserParams{Email: "outsider@example.test", DisplayName: "Outsider", PasswordHash: []byte("hash")})
	if err != nil {
		t.Fatal(err)
	}
	grant := store.DesktopAccess{UserID: outsider.ID, TargetID: target.ID, AllowRead: true, GrantedBy: admin.ID}
	if err = app.store.Repository().SetDesktopAccess(ctx, grant); err != nil {
		t.Fatal(err)
	}
	if app.authorizeDesktop(ctx, outsider.ID, target.ID, false) == nil {
		t.Fatal("grant bypassed organization boundary")
	}
	if err = app.store.Repository().AddOrganizationMember(ctx, target.OwnerID, outsider.ID, store.RoleMember); err != nil {
		t.Fatal(err)
	}
	if err = app.authorizeDesktop(ctx, outsider.ID, target.ID, false); err != nil {
		t.Fatal(err)
	}
	if app.authorizeDesktop(ctx, outsider.ID, target.ID, true) == nil {
		t.Fatal("read granted control")
	}
	_, err = app.store.DB().ExecContext(ctx, `UPDATE users SET disabled_at=? WHERE id=?`, time.Now().Format(time.RFC3339), outsider.ID)
	if err != nil {
		t.Fatal(err)
	}
	if app.authorizeDesktop(ctx, outsider.ID, target.ID, false) == nil {
		t.Fatal("disabled user could read desktop")
	}
	_, _ = app.store.DB().ExecContext(ctx, `UPDATE users SET disabled_at=NULL WHERE id=?`, outsider.ID)
	_, _ = app.store.DB().ExecContext(ctx, `UPDATE desktop_access SET expires_at=? WHERE user_id=?`, time.Now().Add(-time.Hour).Format(time.RFC3339), outsider.ID)
	if app.authorizeDesktop(ctx, outsider.ID, target.ID, false) == nil {
		t.Fatal("expired grant accepted")
	}
	adminClient := desktopMCPClient(t, app, admin)
	revoked, callError := adminClient.CallTool(ctx, &mcp.CallToolParams{Name: "desktop_access_set", Arguments: map[string]any{"target_id": target.ID, "user_id": outsider.ID, "allow_read": false, "allow_control": false}})
	if callError != nil || revoked.IsError {
		t.Fatal("administrator could not revoke an expired/former-member grant")
	}

	client := desktopMCPClient(t, app, outsider)
	res, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "desktop_access_set", Arguments: map[string]any{"user_id": outsider.ID, "target_id": target.ID, "allow_read": true, "allow_control": true}})
	if err != nil || !res.IsError {
		t.Fatal("ordinary member could grant desktop access")
	}
}
