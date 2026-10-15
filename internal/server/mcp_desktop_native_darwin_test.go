//go:build darwin

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
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestDesktopMCPRealAgentNativeFixture(t *testing.T) {
	if os.Getenv("GOSSHD_DESKTOP_NATIVE_TEST") != "1" {
		t.Skip("interactive native end-to-end test is opt-in")
	}
	helperPath, fixturePath := os.Getenv("GOSSHD_DESKTOP_HELPER_TEST_PATH"), os.Getenv("GOSSHD_DESKTOP_FIXTURE_TEST_PATH")
	if helperPath == "" || fixturePath == "" {
		t.Fatal("native helper/fixture app paths required")
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
	app.registry.RegisterWithInfo(target.AgentID, server, AgentRegistryInfo{GOOS: "darwin", GOARCH: runtime.GOARCH})
	realAgent, err := agent.NewEmbedded(agent.Config{Desktop: desktop.Config{Enabled: true, HelperPath: helperPath}}, target.AgentID)
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
	pidFile := filepath.Join(t.TempDir(), "fixture.pid")
	fixture := exec.Command("/usr/bin/open", "-n", "-g", "-W", "-a", fixturePath, "--env", "GOSSHD_FIXTURE_PID_FILE="+pidFile)
	if err = fixture.Start(); err != nil {
		t.Fatal(err)
	}
	fixturePID := 0
	defer func() {
		if fixturePID > 1 {
			if p, e := os.FindProcess(fixturePID); e == nil {
				p.Kill()
			}
		}
		fixture.Process.Kill()
		fixture.Wait()
	}()
	for i := 0; i < 50; i++ {
		if data, e := os.ReadFile(pidFile); e == nil {
			fixturePID, _ = strconv.Atoi(strings.TrimSpace(string(data)))
			if fixturePID > 1 {
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if fixturePID <= 1 {
		t.Fatal("fixture did not report its own PID")
	}
	client := desktopMCPClient(t, app, actor)
	call := func(name string, args map[string]any) *mcp.CallToolResult {
		t.Helper()
		result, err := client.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil || result.IsError {
			t.Fatalf("%s: %v %+v", name, err, result)
		}
		return result
	}
	opened := call("desktop_session_open", map[string]any{"target_id": target.ID})
	id := stringField(t, desktopResult(t, opened), "session_id")
	var window desktop.Window
	for i := 0; i < 30; i++ {
		result := call("desktop_list_windows", map[string]any{"session_id": id})
		wire, _ := json.Marshal(desktopResult(t, result))
		var out struct {
			Result struct {
				Windows []desktop.Window `json:"windows"`
			} `json:"result"`
		}
		_ = json.Unmarshal(wire, &out)
		for _, w := range out.Result.Windows {
			if w.App == strconv.Itoa(fixturePID) {
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
		result := call("desktop_get_window_state", map[string]any{"session_id": id, "params": map[string]any{"window": window, "include_text": true, "include_screenshot": true, "include_element_details": true}})
		image := false
		for _, content := range result.Content {
			if _, ok := content.(*mcp.ImageContent); ok {
				image = true
			}
		}
		if !image {
			t.Fatal("real Agent screenshot not returned as MCP image")
		}
		wire, _ := json.Marshal(desktopResult(t, result))
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
	index = -1
	for _, e := range state.Elements {
		if e.Name == "Fixture text" {
			index = e.Index
			break
		}
	}
	call("desktop_click", map[string]any{"session_id": id, "params": map[string]any{"window": window, "snapshot_id": state.SnapshotID, "element_index": index}})
	state = observe()
	call("desktop_press_key", map[string]any{"session_id": id, "params": map[string]any{"window": window, "snapshot_id": state.SnapshotID, "key": "Command+a"}})
	state = observe()
	call("desktop_type_text", map[string]any{"session_id": id, "params": map[string]any{"window": window, "snapshot_id": state.SnapshotID, "text": "MCP Unicode 中文🙂"}})
	updated = false
	for i := 0; i < 20; i++ {
		state = observe()
		for _, e := range state.Elements {
			if e.Name == "Fixture text" && e.Value == "MCP Unicode 中文🙂" {
				updated = true
			}
		}
		if updated {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !updated {
		t.Fatal("real Agent keyboard round trip did not update owned fixture")
	}
	call("desktop_session_close", map[string]any{"session_id": id})
}
