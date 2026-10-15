package mcptools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/qinyongliang/gosshd-bastion/internal/desktop"
)

// This opt-in acceptance test operates only an owned emulator fixture. It never
// selects a physical device or installs/launches arbitrary application packages.
func TestAndroidEmulatorFixture(t *testing.T)              { runAndroidEmulatorFixture(t, false) }
func TestAndroidAccessibilityEmulatorFixture(t *testing.T) { runAndroidEmulatorFixture(t, true) }
func runAndroidEmulatorFixture(t *testing.T, auxiliary bool) {
	envName := "GOSSHD_ANDROID_EMULATOR_TEST"
	if auxiliary {
		envName = "GOSSHD_ANDROID_ACCESSIBILITY_TEST"
	}
	if os.Getenv(envName) != "1" {
		t.Skip("set GOSSHD_ANDROID_EMULATOR_TEST=1 for an explicitly selected emulator")
	}
	adb, serial := os.Getenv("GOSSHD_ANDROID_ADB"), os.Getenv("GOSSHD_ANDROID_EMULATOR_SERIAL")
	if adb == "" || !filepath.IsAbs(adb) || !strings.HasPrefix(serial, "emulator-") {
		t.Fatal("absolute adb path and explicit emulator serial required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	adbCommand := func(args ...string) []byte {
		t.Helper()
		out, err := exec.CommandContext(ctx, adb, append([]string{"-s", serial}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("emulator command %v: %v", args, err)
		}
		return out
	}
	if strings.TrimSpace(string(adbCommand("shell", "getprop", "ro.kernel.qemu"))) != "1" {
		t.Fatal("refusing non-emulator target")
	}
	if strings.TrimSpace(string(adbCommand("shell", "getprop", "sys.boot_completed"))) != "1" {
		t.Fatal("emulator is not booted")
	}
	// Reset only the owned fixture so acceptance can be repeated without reinstalling.
	adbCommand("shell", "wm", "user-rotation", "lock", "0")
	adbCommand("shell", "input", "keyevent", "KEYCODE_WAKEUP")
	adbCommand("shell", "wm", "dismiss-keyguard")
	adbCommand("shell", "am", "force-stop", "com.example.gosshd.fixture")
	adbCommand("shell", "am", "start", "-W", "-n", "com.example.gosshd.fixture/.MainActivity")
	uid := strings.TrimSpace(string(adbCommand("shell", "id", "-u")))
	command := []string{"-s", serial, "shell", "-T", "/data/local/tmp/gosshd-desktop-mcp", "--allow-read", "--allow-control", "--android-backend", "shell"}
	if auxiliary {
		binary := os.Getenv("GOSSHD_ANDROID_COMPANION_MCP")
		if !strings.HasPrefix(binary, "/data/app/") || !strings.HasSuffix(binary, "/lib/x86_64/libdesktop_mcp.so") {
			t.Fatal("explicit owned debug companion native-library path required")
		}
		uid = strings.TrimSpace(string(adbCommand("shell", "run-as", "com.gosshd.desktop", "id", "-u")))
		command = []string{"-s", serial, "shell", "-T", "run-as", "com.gosshd.desktop", binary, "--allow-read", "--allow-control", "--android-backend", "accessibility", "--android-bridge-token-file", "/data/user/0/com.gosshd.desktop/files/pair.token"}
	}
	cmd := exec.CommandContext(ctx, adb, command...)
	cmd.Stderr = io.Discard
	client := mcp.NewClient(&mcp.Implementation{Name: "android-emulator-fixture", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd, TerminateDuration: time.Second}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	call := func(name string, args map[string]any, wantError bool) *mcp.CallToolResult {
		t.Helper()
		res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if res.IsError != wantError {
			wire, _ := json.Marshal(res.Content)
			t.Fatalf("%s error=%v want=%v: %s", name, res.IsError, wantError, wire)
		}
		return res
	}
	decode := func(res *mcp.CallToolResult, out any) {
		t.Helper()
		wire, err := json.Marshal(res.StructuredContent)
		if err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(wire, out); err != nil {
			t.Fatal(err)
		}
	}
	type envelope struct {
		SessionID string          `json:"session_id"`
		Result    json.RawMessage `json:"result"`
	}
	open := call("desktop_session_open", map[string]any{"target_id": "local"}, false)
	var opened envelope
	decode(open, &opened)
	sid := opened.SessionID
	defer func() {
		_, _ = session.CallTool(context.Background(), &mcp.CallToolParams{Name: "desktop_session_close", Arguments: map[string]any{"session_id": sid}})
	}()
	var status struct {
		Status struct {
			Platform    string `json:"platform"`
			Interactive bool   `json:"interactive"`
			ShellUser   bool   `json:"shell_user"`
			Root        bool   `json:"root"`
			Backend     string `json:"backend"`
			UID         int    `json:"uid"`
		} `json:"status"`
	}
	if err = json.Unmarshal(opened.Result, &status); err != nil {
		t.Fatal(err)
	}
	if auxiliary {
		if status.Status.Backend != "accessibility" || status.Status.UID < 10000 || status.Status.Root {
			t.Fatalf("not an unrooted auxiliary session: %+v", status.Status)
		}
	}
	if status.Status.Platform != "android" || !status.Status.Interactive || !auxiliary && ((uid == "2000" && !status.Status.ShellUser) || (uid == "0" && !status.Status.Root) || (uid != "0" && uid != "2000")) {
		t.Fatalf("native status: %+v", status.Status)
	}
	t.Logf("native status: android, interactive, uid=%s capture/accessibility probes passed", uid)
	target := func() desktop.Window {
		t.Helper()
		for attempt := 0; attempt < 15; attempt++ {
			result := call("desktop_list_windows", map[string]any{"session_id": sid}, false)
			var e envelope
			decode(result, &e)
			var list struct {
				Windows []desktop.Window `json:"windows"`
			}
			if err := json.Unmarshal(e.Result, &list); err != nil {
				t.Fatal(err)
			}
			var candidates []desktop.Window
			for _, w := range list.Windows {
				if w.App == "com.example.gosshd.fixture" {
					candidates = append(candidates, w)
				}
				if w.App == "com.example.gosshd.fixture" && (!auxiliary || w.Title == "GOSSHD desktop fixture") {
					return w
				}
			}

			if len(candidates) == 1 {
				return candidates[0]
			}
			time.Sleep(150 * time.Millisecond)
		}
		t.Fatal("owned fixture window missing")
		return desktop.Window{}
	}
	observe := func(label string) desktop.State {
		t.Helper()
		var result *mcp.CallToolResult
		for attempt := 0; attempt < 10; attempt++ {
			var err error
			result, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "desktop_get_window_state", Arguments: map[string]any{"session_id": sid, "params": desktop.Params{Window: ptrWindow(target()), IncludeText: true, MaxDepth: 16}}})
			if err != nil {
				t.Fatal(err)
			}
			if !result.IsError {
				break
			}
			// Re-observation is safe after transient rotation/selection window changes;
			// this never retries a mutating operation.
			time.Sleep(150 * time.Millisecond)
		}
		if result.IsError {
			wire, _ := json.Marshal(result.Content)
			t.Fatalf("cannot obtain stable observation: %s", wire)
		}
		var e envelope
		decode(result, &e)
		var state desktop.State
		if err := json.Unmarshal(e.Result, &state); err != nil {
			t.Fatal(err)
		}
		images := 0
		for _, c := range result.Content {
			if img, ok := c.(*mcp.ImageContent); ok {
				images++
				cfg, err := png.DecodeConfig(bytes.NewReader(img.Data))
				if err != nil {
					t.Fatal(err)
				}
				if cfg.Width > 1600 || cfg.Height > 1600 || state.Screenshot == nil || cfg.Width != state.Screenshot.Width || cfg.Height != state.Screenshot.Height {
					t.Fatal("native PNG bounds mismatch")
				}
				if label != "" && os.Getenv("GOSSHD_ANDROID_EMULATOR_ARTIFACT_DIR") != "" {
					dir := os.Getenv("GOSSHD_ANDROID_EMULATOR_ARTIFACT_DIR")
					if err := os.MkdirAll(dir, 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(dir, label+".png"), img.Data, 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
		}
		if images != 1 {
			t.Fatal("MCP must return one native image")
		}
		if err := desktop.ValidateElements(state.Elements); err != nil {
			t.Fatal(err)
		}
		return state
	}
	element := func(state desktop.State, id string) desktop.Element {
		t.Helper()
		for _, e := range state.Elements {
			if e.AutomationID == "com.example.gosshd.fixture:id/"+id {
				return e
			}
		}
		t.Fatalf("fixture element %s missing", id)
		return desktop.Element{}
	}
	assertText := func(state desktop.State, id, want string) {
		t.Helper()
		got := element(state, id)
		if got.Value != want {
			t.Fatalf("%s value=%q want=%q", id, got.Value, want)
		}
	}
	action := func(name string, state desktop.State, p desktop.Params, wantError bool) {
		t.Helper()
		p.Window = &state.Window
		p.SnapshotID = state.SnapshotID
		call("desktop_"+name, map[string]any{"session_id": sid, "params": p}, wantError)
	}
	state := observe("before")
	t.Logf("observed native screen: %.0fx%.0f, image %dx%d, %d elements", state.Bounds.Width, state.Bounds.Height, state.Screenshot.Width, state.Screenshot.Height, len(state.Elements))
	tap := element(state, "tap")
	action("click", state, desktop.Params{ElementIndex: &tap.Index}, false)
	action("click", state, desktop.Params{ElementIndex: &tap.Index}, true)
	state = observe("")
	assertText(state, "result", "Tapped 1")
	t.Log("tap changed UI; reused snapshot rejected")
	editor := element(state, "editor")
	action("click", state, desktop.Params{ElementIndex: &editor.Index}, false)
	state = observe("")
	literal := "hello %s; $(demo)"
	action("type_text", state, desktop.Params{Text: literal}, false)
	state = observe("")
	assertText(state, "editor", literal)
	t.Log("literal printable text including percent-s and shell punctuation preserved")
	if auxiliary {
		editor = element(state, "editor")
		action("set_value", state, desktop.Params{ElementIndex: &editor.Index, Text: "辅助功能 中文 🐱"}, false)
		state = observe("")
		assertText(state, "editor", "辅助功能 中文 🐱")
		t.Log("ordinary app UID set_value preserved Unicode and emoji")
	} else {
		action("type_text", state, desktop.Params{Text: "valid prefix 中文"}, true)
		state = observe("")
		assertText(state, "editor", literal)
		t.Log("Unicode rejected without typing its ASCII prefix")
	}
	action("press_key", state, desktop.Params{Key: "Ctrl+a"}, false)
	state = observe("")
	action("type_text", state, desktop.Params{Text: "replacement"}, false)
	state = observe("")
	assertText(state, "editor", "replacement")
	if auxiliary {
		t.Log("native editable Ctrl+a selection verified")
	} else {
		t.Log("Ctrl+a sent as a real key combination")
	}
	tap = element(state, "tap")
	action("perform_action", state, desktop.Params{ElementIndex: &tap.Index, Action: "secondary"}, false)
	state = observe("")
	assertText(state, "result", "Long pressed")
	t.Log("observed long press changed UI")
	toggle := element(state, "toggle")
	action("perform_action", state, desktop.Params{ElementIndex: &toggle.Index, Action: "toggle"}, false)
	state = observe("")
	assertText(state, "result", "Checked")
	if element(state, "toggle").ToggleState != "on" {
		t.Fatal("toggle state missing")
	}
	t.Log("toggle gesture and checked state verified")
	slider := element(state, "slider")
	b := slider.Bounds
	action("drag", state, desktop.Params{X: b.X + b.Width*0.1, Y: b.Y + b.Height/2, ToX: b.X + b.Width*0.9, ToY: b.Y + b.Height/2}, false)
	state = observe("")
	if !strings.HasPrefix(element(state, "result").Value, "Slider ") {
		t.Fatal("drag did not move slider")
	}
	t.Log("drag changed slider UI")
	scroll := element(state, "scroll")
	beforeTree := state.Tree
	action("scroll", state, desktop.Params{ElementIndex: &scroll.Index, DeltaY: 120}, false)
	state = observe("after-input")
	if state.Tree == beforeTree {
		t.Fatal("scroll did not change visible rows")
	}
	t.Log("scroll changed visible row hierarchy")
	action("click", state, desktop.Params{X: state.Bounds.Width, Y: 1}, true)
	state = observe("")
	editor = element(state, "editor")
	action("set_value", state, desktop.Params{ElementIndex: &editor.Index, Text: "value"}, !auxiliary)
	state = observe("")
	call("desktop_screenshot", map[string]any{"session_id": sid}, false)
	action("click", state, desktop.Params{X: 100, Y: 100}, true)
	t.Log("bounds, backend-specific set_value and display-snapshot invalidation verified")
	// Rotate only this fresh emulator, and restore its fixed default orientation.
	state = observe("")
	adbCommand("shell", "wm", "user-rotation", "lock", "1")
	defer adbCommand("shell", "wm", "user-rotation", "lock", "0")
	for i := 0; i < 20; i++ {
		dump := string(adbCommand("shell", "dumpsys", "window", "-a"))
		if strings.Contains(dump, fmt.Sprintf("cur=%.0fx%.0f", state.Bounds.Height, state.Bounds.Width)) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	action("click", state, desktop.Params{X: 100, Y: 100}, true)
	var rotated desktop.State
	for attempt := 0; attempt < 5; attempt++ {
		rotated = observe("landscape")
		if rotated.Bounds.Width > rotated.Bounds.Height {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if rotated.Bounds.Width <= rotated.Bounds.Height {
		t.Fatal("rotation was not reflected in native geometry")
	}
	t.Log("rotation invalidated old observation and produced landscape PNG")
	// Sleeping display must reject input; wake the owned emulator during cleanup.
	adbCommand("shell", "input", "keyevent", "KEYCODE_SLEEP")
	for i := 0; i < 20; i++ {
		if strings.Contains(string(adbCommand("shell", "dumpsys", "power")), "mWakefulness=Asleep") {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	defer func() {
		adbCommand("shell", "input", "keyevent", "KEYCODE_WAKEUP")
		adbCommand("shell", "wm", "dismiss-keyguard")
	}()
	action("click", rotated, desktop.Params{X: 100, Y: 100}, true)
	res := call("desktop_status", map[string]any{"session_id": sid}, false)
	var sleep envelope
	decode(res, &sleep)
	if !bytes.Contains(sleep.Result, []byte(`"interactive":false`)) {
		t.Fatal("sleeping display reported interactive")
	}
	t.Log("sleep rejected input and status reported unavailable")
	call("desktop_session_close", map[string]any{"session_id": sid}, false)
	call("desktop_status", map[string]any{"session_id": sid}, true)
	t.Log("closed session rejected further operations")
}
func ptrWindow(value desktop.Window) *desktop.Window { return &value }
