//go:build windows

package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWindowsNativeFixture(t *testing.T) {
	if os.Getenv("GOSSHD_DESKTOP_NATIVE_TEST") != "1" {
		t.Skip("set GOSSHD_DESKTOP_NATIVE_TEST=1 in an interactive user session")
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "Fixture.cs")
	exe := filepath.Join(dir, "Fixture.exe")
	fixtureSource, err := os.ReadFile(filepath.Join("native", "windows", "Fixture.cs"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, fixtureSource, 0600); err != nil {
		t.Fatal(err)
	}
	compiler := filepath.Join(os.Getenv("SystemRoot"), "Microsoft.NET", "Framework64", "v4.0.30319", "csc.exe")
	if _, err := os.Stat(compiler); err != nil {
		compiler = filepath.Join(os.Getenv("SystemRoot"), "Microsoft.NET", "Framework", "v4.0.30319", "csc.exe")
	}
	cmd := exec.Command(compiler, "/nologo", "/target:winexe", "/r:System.Windows.Forms.dll", "/r:System.Drawing.dll", "/out:"+exe, source)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture compile: %v %s", err, output)
	}
	fixture := exec.Command(exe)
	if err := fixture.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = fixture.Process.Kill(); _ = fixture.Wait() }()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	helper, err := startProcess(ctx, Config{}, platformCommand)
	if err != nil {
		t.Fatal(err)
	}
	defer helper.close()
	seq := 0
	call := func(method string, p Params) Response {
		t.Helper()
		seq++
		started := time.Now()
		res, err := helper.call(Request{ID: strings.Repeat("x", seq), Method: method, Params: p})
		t.Logf("native %s elapsed %s", method, time.Since(started))
		if err != nil || !res.OK {
			t.Fatalf("%s failed: %v %s", method, err, res.Error)
		}
		return res
	}
	var window Window
	for attempt := 0; attempt < 30; attempt++ {
		res := call("list_windows", Params{})
		var list struct {
			Windows []Window `json:"windows"`
		}
		_ = json.Unmarshal(res.Result, &list)
		for _, w := range list.Windows {
			if w.Title == "GOSSHD desktop test fixture" {
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
		t.Fatal("fixture window unavailable")
	}
	observe := func(screenshot bool) State {
		t.Helper()
		res := call("get_window_state", Params{Window: &window, IncludeScreenshot: &screenshot, IncludeText: true})
		var state State
		if err = json.Unmarshal(res.Result, &state); err != nil {
			t.Fatal(err)
		}
		return state
	}
	state := observe(true)
	if state.Image == nil {
		t.Fatal("missing native screenshot")
	}
	if _, err = png.DecodeConfig(bytes.NewReader(state.Image.Data)); err != nil {
		t.Fatalf("invalid PNG: %v", err)
	}
	find := func(state State, name string) int {
		for _, e := range state.Elements {
			if e.Name == name {
				return e.Index
			}
		}
		t.Fatalf("fixture element %q unavailable", name)
		return -1
	}
	index := find(state, "Fixture text")
	call("set_value", Params{Window: &window, SnapshotID: state.SnapshotID, ElementIndex: &index, Text: "MCP sample"})
	state = observe(false)
	index = find(state, "Fixture button")
	call("perform_action", Params{Window: &window, SnapshotID: state.SnapshotID, ElementIndex: &index, Action: "invoke"})
	time.Sleep(150 * time.Millisecond)
	state = observe(false)
	applied := false
	for _, e := range state.Elements {
		if strings.Contains(e.Name, "Applied: MCP sample") || strings.Contains(e.Value, "Applied: MCP sample") {
			applied = true
		}
	}
	if !applied {
		t.Fatal("native invoke did not update fixture")
	}
	// Coordinate input and keyboard operations are confined to the fixture.
	index = find(state, "Fixture text")
	call("click", Params{Window: &window, SnapshotID: state.SnapshotID, ElementIndex: &index})
	state = observe(false)
	call("press_key", Params{Window: &window, SnapshotID: state.SnapshotID, Key: "Ctrl+a"})
	time.Sleep(100 * time.Millisecond)
	state = observe(false)
	call("type_text", Params{Window: &window, SnapshotID: state.SnapshotID, Text: "typed sample"})
	time.Sleep(150 * time.Millisecond)
	state = observe(false)
	found := false
	for _, e := range state.Elements {
		if e.Value == "typed sample" {
			found = true
		}
	}
	if !found {
		t.Fatalf("native text input did not update fixture: %+v", state.Elements)
	}

	state = observe(false)
	index = find(state, "Fixture toggle")
	call("perform_action", Params{Window: &window, SnapshotID: state.SnapshotID, ElementIndex: &index, Action: "toggle"})
	state = observe(false)
	toggled := false
	for _, e := range state.Elements {
		if e.Name == "Fixture toggle" && e.ToggleState == "on" {
			toggled = true
		}
	}
	if !toggled {
		t.Fatal("native toggle did not update fixture")
	}
	index = find(state, "Fixture slider")
	call("set_value", Params{Window: &window, SnapshotID: state.SnapshotID, ElementIndex: &index, Text: "9"})
	state = observe(false)
	ranged := false
	for _, e := range state.Elements {
		if e.Name == "Fixture slider" && e.Value == "9" {
			ranged = true
		}
	}
	if !ranged {
		t.Fatal("native range value did not update fixture")
	}
	index = find(state, "Second")
	call("perform_action", Params{Window: &window, SnapshotID: state.SnapshotID, ElementIndex: &index, Action: "select"})
	state = observe(false)
	selected := false
	for _, e := range state.Elements {
		if e.Name == "Second" && e.Selected != nil && *e.Selected {
			selected = true
		}
	}
	if !selected {
		t.Fatal("native selection did not update fixture")
	}
	var dragBounds Bounds
	for _, e := range state.Elements {
		if e.Name == "Fixture drag" {
			dragBounds = e.Bounds
		}
	}
	call("drag", Params{Window: &window, SnapshotID: state.SnapshotID, X: dragBounds.X + 15, Y: dragBounds.Y + 15, ToX: dragBounds.X + 90, ToY: dragBounds.Y + 60})
	time.Sleep(100 * time.Millisecond)
	state = observe(false)
	dragged := false
	for _, e := range state.Elements {
		if e.Name == "Dragged" {
			dragged = true
		}
	}
	if !dragged {
		t.Fatal("native drag did not reach fixture")
	}
	var scrollBounds Bounds
	for _, e := range state.Elements {
		if e.Name == "Fixture scroll" && e.Role == "ControlType.Pane" {
			scrollBounds = e.Bounds
		}
	}
	if scrollBounds.Width == 0 {
		t.Fatalf("scroll region not in UIA observation: %+v", state.Elements)
	}
	call("scroll", Params{Window: &window, SnapshotID: state.SnapshotID, X: scrollBounds.X + 30, Y: scrollBounds.Y + 30, DeltaY: 120})
	time.Sleep(100 * time.Millisecond)
	state = observe(false)
	// WinForms routes wheel events through the focused/hovered child, so verify
	// the scroll bar's value or explicit panel event rather than assuming a role.
	scrolled := false
	for _, e := range state.Elements {
		if strings.HasPrefix(e.Name, "Scrolled:") || e.Name == "Scrollable sample" && e.Bounds.Y < scrollBounds.Y {
			scrolled = true
		}
	}
	if !scrolled {
		t.Fatalf("native scroll did not move fixture content; requested region %+v; elements %+v", scrollBounds, state.Elements)
	}
	screenshot := call("screenshot", Params{Window: &window})
	var shot State
	_ = json.Unmarshal(screenshot.Result, &shot)
	if shot.Image == nil {
		t.Fatal("dedicated screenshot tool returned no image")
	}
	stale, err := helper.call(Request{ID: "stale", Method: "click", Params: Params{Window: &window, SnapshotID: "invalid", X: 10, Y: 10}})
	if err != nil {
		t.Fatal(err)
	}
	if stale.OK {
		t.Fatal("native backend accepted a stale snapshot")
	}
}

// Compile coverage is safe on headless CI; no helper/window is started.
func TestWindowsHelperCompiles(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd, cleanup, err := platformCommand(ctx, Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if _, err = os.Stat(cmd.Path); err != nil {
		t.Fatal(err)
	}
}
