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
	fakeDesktopAgentPlatform(t, app, id, "windows")
}
func fakeDesktopAgentPlatform(t *testing.T, app *App, id, platform string) {
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
	app.registry.RegisterWithInfo(id, server, AgentRegistryInfo{GOOS: platform, GOARCH: "arm64"})
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
func TestDesktopMCPInheritedReadControlRevocationAndAudit(t *testing.T) {
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
	if !mcpHasTool(tools, "desktop_get_window_state") || !mcpHasTool(tools, "desktop_perform_action") || mcpHasTool(tools, "desktop_access_set") || mcpHasTool(tools, "session_send_command") || mcpHasTool(tools, "auth_register") {
		t.Fatal("unexpected desktop tool surface")
	}
	access := call("desktop_access_get", map[string]any{"target_id": target.ID}, false)
	wire, _ := json.Marshal(access.StructuredContent)
	var view desktopAccessView
	if err = json.Unmarshal(wire, &view); err != nil || !view.AllowRead || !view.AllowControl || view.Authorization != "target_access" {
		t.Fatalf("unexpected inherited access %s: %v", wire, err)
	}
	opened := call("desktop_session_open", map[string]any{"target_id": target.ID}, false)
	sessionID := stringField(t, opened.StructuredContent, "session_id")
	params := map[string]any{"window": map[string]any{"id": "10", "app": "20"}, "include_text": true}
	state := call("desktop_get_window_state", map[string]any{"session_id": sessionID, "params": params}, false)
	imageCount := 0
	for _, content := range state.Content {
		if _, ok := content.(*mcp.ImageContent); ok {
			imageCount++
		}
	}
	if imageCount != 1 {
		t.Fatal("MCP screenshot image not returned")
	}
	params["snapshot_id"] = "test-snapshot"
	params["text"] = "private typed text must not be audited"
	call("desktop_type_text", map[string]any{"session_id": sessionID, "params": params}, false)
	outsider, err := app.store.Repository().CreateUser(ctx, store.CreateUserParams{Email: "session-outsider@example.test", DisplayName: "Outsider", PasswordHash: []byte("hash")})
	if err != nil {
		t.Fatal(err)
	}
	outsiderClient := desktopMCPClient(t, app, outsider)
	foreign, err := outsiderClient.CallTool(ctx, &mcp.CallToolParams{Name: "desktop_status", Arguments: map[string]any{"session_id": sessionID}})
	if err != nil || !foreign.IsError {
		t.Fatalf("foreign session was accessible: %+v %v", foreign, err)
	}
	audits := call("desktop_audit_list", map[string]any{}, false)
	wire, _ = json.Marshal(audits.StructuredContent)
	if strings.Contains(string(wire), "private typed text") || strings.Contains(string(wire), "data") {
		t.Fatal("desktop content leaked into audit")
	}
	if !strings.Contains(string(wire), "desktop_type_text") && !strings.Contains(string(wire), "type_text") {
		t.Fatal("control audit missing")
	}
	if _, err = app.store.DB().ExecContext(ctx, `UPDATE users SET disabled_at=? WHERE id=?`, time.Now().Format(time.RFC3339), user.ID); err != nil {
		t.Fatal(err)
	}
	call("desktop_status", map[string]any{"session_id": sessionID}, true)
}

func TestDesktopInheritedAccessIsolationAndRevocation(t *testing.T) {
	app, admin, target := desktopTestApp(t)
	ctx := context.Background()
	outsider, err := app.store.Repository().CreateUser(ctx, store.CreateUserParams{Email: "outsider@example.test", DisplayName: "Outsider", PasswordHash: []byte("hash")})
	if err != nil {
		t.Fatal(err)
	}
	// Legacy desktop records, including expired/revoked ones, no longer define access.
	expiry := time.Now().Add(time.Hour)
	if err = app.store.Repository().SetDesktopAccess(ctx, store.DesktopAccess{UserID: outsider.ID, TargetID: target.ID, ExpiresAt: &expiry, GrantedBy: admin.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err = app.store.DB().ExecContext(ctx, `UPDATE desktop_access SET expires_at=? WHERE user_id=? AND target_id=?`, time.Now().Add(-time.Hour).Format(time.RFC3339), outsider.ID, target.ID); err != nil {
		t.Fatal(err)
	}
	if app.authorizeDesktop(ctx, outsider.ID, target.ID, false) == nil {
		t.Fatal("organization boundary bypassed")
	}
	if err = app.store.Repository().AddOrganizationMember(ctx, target.OwnerID, outsider.ID, store.RoleMember); err != nil {
		t.Fatal(err)
	}
	for _, control := range []bool{false, true} {
		if err = app.authorizeDesktop(ctx, outsider.ID, target.ID, control); err != nil {
			t.Fatal(err)
		}
	}
	group, err := app.store.Repository().CreateOrganizationUserGroup(ctx, store.CreateOrganizationUserGroupParams{OrganizationID: target.OwnerID, Name: "Desktop operators", Slug: "desktop-operators"})
	if err != nil {
		t.Fatal(err)
	}
	if err = app.store.Repository().AttachUserGroupToTarget(ctx, group.ID, target.ID); err != nil {
		t.Fatal(err)
	}
	if app.authorizeDesktop(ctx, outsider.ID, target.ID, false) == nil {
		t.Fatal("target group boundary bypassed")
	}
	if err = app.store.Repository().AddUserToGroup(ctx, group.ID, outsider.ID); err != nil {
		t.Fatal(err)
	}
	if err = app.authorizeDesktop(ctx, outsider.ID, target.ID, true); err != nil {
		t.Fatal(err)
	}
	fakeDesktopAgent(t, app, target.AgentID)
	memberClient := desktopMCPClient(t, app, outsider)
	opened, err := memberClient.CallTool(ctx, &mcp.CallToolParams{Name: "desktop_session_open", Arguments: map[string]any{"target_id": target.ID}})
	if err != nil || opened.IsError {
		t.Fatalf("member could not open without desktop grant: %+v %v", opened, err)
	}
	sessionID := stringField(t, opened.StructuredContent, "session_id")
	if err = app.store.Repository().RemoveUserFromGroup(ctx, group.ID, outsider.ID); err != nil {
		t.Fatal(err)
	}
	revoked, err := memberClient.CallTool(ctx, &mcp.CallToolParams{Name: "desktop_status", Arguments: map[string]any{"session_id": sessionID}})
	if err != nil || !revoked.IsError {
		t.Fatalf("target revocation not enforced on existing session: %+v %v", revoked, err)
	}
	if err = app.store.Repository().AddUserToGroup(ctx, group.ID, outsider.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = app.store.DB().ExecContext(ctx, `UPDATE users SET disabled_at=? WHERE id=?`, time.Now().Format(time.RFC3339), outsider.ID); err != nil {
		t.Fatal(err)
	}
	if app.authorizeDesktop(ctx, outsider.ID, target.ID, false) == nil {
		t.Fatal("disabled account could read desktop")
	}
	if _, err = app.store.DB().ExecContext(ctx, `UPDATE users SET disabled_at=NULL WHERE id=?`, outsider.ID); err != nil {
		t.Fatal(err)
	}
	if err = app.store.Repository().RemoveOrganizationMember(ctx, target.OwnerID, outsider.ID); err != nil {
		t.Fatal(err)
	}
	if app.authorizeDesktop(ctx, outsider.ID, target.ID, true) == nil {
		t.Fatal("organization revocation bypassed")
	}
}

func TestDesktopInheritedAccessRejectsUnsupportedAgent(t *testing.T) {
	app, user, target := desktopTestApp(t)
	fakeDesktopAgent(t, app, target.AgentID)
	session, err := app.registry.Get(target.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	app.registry.RegisterWithInfo(target.AgentID, session, AgentRegistryInfo{GOOS: "linux", GOARCH: "amd64"})
	if app.authorizeDesktop(context.Background(), user.ID, target.ID, false) == nil {
		t.Fatal("unsupported Agent platform accepted")
	}
}
