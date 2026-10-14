//go:build windows

package desktop

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// This owned fixture is shown without activation, never topmost. Only its PID
// and observed elements are used; no pre-existing window/session is controlled.
func TestWindowsNativeBackgroundFixture(t *testing.T) {
	if os.Getenv("GOSSHD_DESKTOP_BACKGROUND_NATIVE_TEST") != "1" {
		t.Skip("owned non-activating GUI fixture is opt-in")
	}
	user32 := windows.NewLazySystemDLL("user32.dll")
	foreground := user32.NewProc("GetForegroundWindow")
	cursor := user32.NewProc("GetCursorPos")
	type desktopPosition struct {
		Foreground uintptr
		X, Y       int32
	}
	probe := func() desktopPosition {
		t.Helper()
		var p struct{ X, Y int32 }
		ok, _, err := cursor.Call(uintptr(unsafe.Pointer(&p)))
		if ok == 0 {
			t.Fatalf("cursor probe: %v", err)
		}
		h, _, _ := foreground.Call()
		return desktopPosition{h, p.X, p.Y}
	}
	before := probe()
	reportSharedState := func() {
		t.Helper()
		if now := probe(); now != before {
			t.Logf("application/user changed shared state: foreground=%t cursor=%t; no focus restoration attempted", now.Foreground != before.Foreground, now.X != before.X || now.Y != before.Y)
			before = now
		}
	}
	dir := t.TempDir()
	source := filepath.Join("native", "windows", "Fixture.cs")
	fixturePath := filepath.Join(dir, "BackgroundFixture.exe")
	fixtureSource, err := os.ReadFile(source)
	if err != nil || !strings.Contains(string(fixtureSource), "ShowWithoutActivation") || !strings.Contains(string(fixtureSource), "form.TopMost=!background") {
		t.Fatal("non-activating fixture source contract missing; refusing to launch GUI")
	}

	compiler := filepath.Join(os.Getenv("SystemRoot"), "Microsoft.NET", "Framework64", "v4.0.30319", "csc.exe")
	if _, err := os.Stat(compiler); err != nil {
		compiler = filepath.Join(os.Getenv("SystemRoot"), "Microsoft.NET", "Framework", "v4.0.30319", "csc.exe")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, compiler, "/nologo", "/target:winexe", "/r:System.Windows.Forms.dll", "/r:System.Drawing.dll", "/out:"+fixturePath, source)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture compile: %v %s", err, out)
	}
	fixture := exec.CommandContext(ctx, fixturePath, "--background")
	if err := fixture.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = fixture.Process.Kill(); _ = fixture.Wait() }()
	helper, err := startProcess(ctx, Config{}, platformCommand)
	if err != nil {
		t.Fatal(err)
	}
	defer helper.close()
	seq := 0
	call := func(method string, p Params) Response {
		t.Helper()
		seq++
		p.Mode = "background"
		res, err := helper.call(Request{ID: strconv.Itoa(seq), Method: method, Params: p})
		if err != nil || !res.OK {
			t.Fatalf("%s: %v %s", method, err, res.Error)
		}
		reportSharedState()
		return res
	}
	var window Window
	for i := 0; i < 40; i++ {
		res := call("list_windows", Params{})
		var list struct {
			Windows []Window `json:"windows"`
		}
		_ = json.Unmarshal(res.Result, &list)
		for _, w := range list.Windows {
			if w.App == strconv.Itoa(fixture.Process.Pid) {
				window = w
				break
			}
		}
		if window.ID != "" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if window.ID == "" {
		t.Fatal("owned background fixture window unavailable")
	}
	observe := func(screenshot bool) State {
		t.Helper()
		res := call("get_window_state", Params{Window: &window, IncludeText: true, IncludeScreenshot: &screenshot})
		var state State
		if err := json.Unmarshal(res.Result, &state); err != nil {
			t.Fatal(err)
		}
		return state
	}
	state := observe(true)
	if state.Image == nil || state.Image.CaptureMethod != "print_window" {
		t.Fatal("strict capture did not report PrintWindow")
	}
	find := func(state State, name string) int {
		t.Helper()
		for _, e := range state.Elements {
			if e.Name == name {
				return e.Index
			}
		}
		t.Fatalf("owned fixture element %q missing", name)
		return -1
	}
	operate := func(method, name, text, action string) {
		t.Helper()
		index := find(state, name)
		res := call(method, Params{Window: &window, SnapshotID: state.SnapshotID, ElementIndex: &index, Text: text, Action: action})
		var result struct {
			Method   string `json:"operation_method"`
			Fallback bool   `json:"foreground_fallback"`
		}
		_ = json.Unmarshal(res.Result, &result)
		if result.Fallback || len(result.Method) < 4 || result.Method[:4] != "uia." {
			t.Fatalf("semantic operation method lost: %s", result.Method)
		}
		state = observe(false)
	}
	operate("set_value", "Fixture text", "background sample", "")
	if state.Elements[find(state, "Fixture text")].Value != "background sample" {
		t.Fatal("native background text write failed")
	}
	operate("click", "Fixture button", "", "")
	applied := false
	for _, e := range state.Elements {
		if e.Name == "Applied: background sample" {
			applied = true
		}
	}
	if !applied {
		t.Fatal("native background invoke failed")
	}
	operate("perform_action", "Fixture toggle", "", "toggle")
	if state.Elements[find(state, "Fixture toggle")].ToggleState != "on" {
		t.Fatal("native background toggle failed")
	}
	operate("set_value", "Fixture slider", "8", "")
	if state.Elements[find(state, "Fixture slider")].Value != "8" {
		t.Fatal("native background range write failed")
	}
	reportSharedState()
}
