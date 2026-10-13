//go:build windows

package server

import (
	"context"
	"encoding/json"
	"github.com/hashicorp/yamux"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/qinyongliang/gosshd-bastion/internal/agent"
	"github.com/qinyongliang/gosshd-bastion/internal/desktop"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestDesktopMCPRealAgentNativeFixture(t *testing.T) {
	if os.Getenv("GOSSHD_DESKTOP_NATIVE_TEST") != "1" {
		t.Skip("interactive native end-to-end test is opt-in")
	}
	app, actor, target := desktopTestApp(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	a, b := net.Pipe()
	server, err := yamux.Server(a, nil)
	if err != nil {
		t.Fatal(err)
	}
	peer, err := yamux.Client(b, nil)
	if err != nil {
		t.Fatal(err)
	}
	app.registry.RegisterWithInfo(target.AgentID, server, AgentRegistryInfo{GOOS: "windows", GOARCH: "amd64"})
	realAgent, err := agent.NewEmbedded(agent.Config{Desktop: desktop.Config{Enabled: true}}, target.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	agentDone := make(chan struct{})
	go func() { defer close(agentDone); _ = realAgent.ServeSession(ctx, peer) }()
	t.Cleanup(func() {
		app.desktopSessions.Close()
		cancel()
		server.Close()
		peer.Close()
		<-agentDone
		realAgent.Close()
	})
	dir := t.TempDir()
	source := filepath.Join(dir, "Fixture.cs")
	binary := filepath.Join(dir, "Fixture.exe")
	data, err := os.ReadFile(filepath.Join("..", "desktop", "native", "windows", "Fixture.cs"))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(source, data, 0600); err != nil {
		t.Fatal(err)
	}
	framework := filepath.Join(os.Getenv("SystemRoot"), "Microsoft.NET", "Framework64", "v4.0.30319")
	if _, err = os.Stat(filepath.Join(framework, "csc.exe")); err != nil {
		framework = filepath.Join(os.Getenv("SystemRoot"), "Microsoft.NET", "Framework", "v4.0.30319")
	}
	if output, err := exec.Command(filepath.Join(framework, "csc.exe"), "/nologo", "/target:winexe", "/r:System.Windows.Forms.dll", "/r:System.Drawing.dll", "/out:"+binary, source).CombinedOutput(); err != nil {
		t.Fatalf("fixture compiler %v %s", err, output)
	}
	fixture := exec.CommandContext(ctx, binary)
	if err = fixture.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { fixture.Process.Kill(); fixture.Wait() }()
	client := desktopMCPClient(t, app, actor)
	call := func(name string, args map[string]any) *mcp.CallToolResult {
		t.Helper()
		result, err := client.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil || result.IsError {
			t.Fatalf("%s: %v %+v", name, err, result)
		}
		return result
	}
	call("desktop_access_set", map[string]any{"target_id": target.ID, "user_id": actor.ID, "allow_read": true, "allow_control": true})
	opened := call("desktop_session_open", map[string]any{"target_id": target.ID})
	id := stringField(t, opened.StructuredContent, "session_id")
	var window desktop.Window
	for i := 0; i < 30; i++ {
		result := call("desktop_list_windows", map[string]any{"session_id": id})
		wire, _ := json.Marshal(result.StructuredContent)
		var out struct {
			Result struct {
				Windows []desktop.Window `json:"windows"`
			} `json:"result"`
		}
		_ = json.Unmarshal(wire, &out)
		for _, w := range out.Result.Windows {
			if w.App == strconv.Itoa(fixture.Process.Pid) {
				window = w
				break
			}
		}
		if window.ID != "" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if window.ID == "" {
		t.Fatal("owned fixture was not returned through real Agent")
	}
	observe := func() desktop.State {
		result := call("desktop_get_window_state", map[string]any{"session_id": id, "params": map[string]any{"window": window, "include_text": true, "include_screenshot": true}})
		image := false
		for _, content := range result.Content {
			if _, ok := content.(*mcp.ImageContent); ok {
				image = true
			}
		}
		if !image {
			t.Fatal("real Agent screenshot not returned as MCP image")
		}
		wire, _ := json.Marshal(result.StructuredContent)
		var out struct {
			Result desktop.State `json:"result"`
		}
		_ = json.Unmarshal(wire, &out)
		return out.Result
	}
	state := observe()
	index := -1
	for _, e := range state.Elements {
		if e.Name == "Fixture text" {
			index = e.Index
			break
		}
	}
	if index < 0 {
		t.Fatal("real Agent accessibility node unavailable")
	}
	call("desktop_set_value", map[string]any{"session_id": id, "params": map[string]any{"window": window, "snapshot_id": state.SnapshotID, "element_index": index, "text": "Agent MCP sample"}})
	state = observe()
	updated := false
	for _, e := range state.Elements {
		if e.Name == "Fixture text" && e.Value == "Agent MCP sample" {
			updated = true
		}
	}
	if !updated {
		t.Fatal("MCP -> real Agent -> native accessibility control did not take effect")
	}
	call("desktop_session_close", map[string]any{"session_id": id})
}
