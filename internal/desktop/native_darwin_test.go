//go:build darwin

package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"image/png"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestMacNativeFixture(t *testing.T) {
	if os.Getenv("GOSSHD_DESKTOP_NATIVE_TEST") != "1" {
		t.Skip("native macOS desktop test is opt-in")
	}
	helperPath, fixturePath := os.Getenv("GOSSHD_DESKTOP_HELPER_TEST_PATH"), os.Getenv("GOSSHD_DESKTOP_FIXTURE_TEST_PATH")
	if helperPath == "" || fixturePath == "" {
		t.Fatal("native helper/fixture paths required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	helper, err := startProcess(ctx, Config{HelperPath: helperPath}, platformCommand)
	if err != nil {
		t.Fatal(err)
	}
	defer helper.close()
	seq := 0
	call := func(method string, p Params) Response {
		t.Helper()
		seq++
		res, err := helper.call(Request{ID: strconv.Itoa(seq), Method: method, Params: p})
		if err != nil || !res.OK {
			t.Fatalf("native %s failed: %v %s", method, err, res.Error)
		}
		return res
	}
	statusResponse := call("status", Params{})
	var status struct {
		Permissions struct {
			Screen bool `json:"screen_capture"`
			AX     bool `json:"accessibility"`
		} `json:"permissions"`
	}
	_ = json.Unmarshal(statusResponse.Result, &status)
	if !status.Permissions.Screen || !status.Permissions.AX {
		t.Fatalf("required macOS permissions missing: screen_capture=%v accessibility=%v", status.Permissions.Screen, status.Permissions.AX)
	}
	fixture := exec.CommandContext(ctx, fixturePath)
	if err = fixture.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { fixture.Process.Kill(); fixture.Wait() }()
	var window Window
	for i := 0; i < 50; i++ {
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
		time.Sleep(100 * time.Millisecond)
	}
	if window.ID == "" {
		t.Fatal("owned fixture window unavailable")
	}
	observe := func(screenshot bool) State {
		res := call("get_window_state", Params{Window: &window, IncludeScreenshot: &screenshot, IncludeText: true})
		var state State
		if err = json.Unmarshal(res.Result, &state); err != nil {
			t.Fatal(err)
		}
		return state
	}
	state := observe(true)
	if state.Image == nil {
		t.Fatal("screenshot missing")
	}
	if _, err = png.DecodeConfig(bytes.NewReader(state.Image.Data)); err != nil {
		t.Fatal("invalid native PNG")
	}
	find := func(state State, name string) int {
		for _, e := range state.Elements {
			if e.Name == name {
				return e.Index
			}
		}
		t.Fatalf("fixture element %q missing", name)
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
		t.Fatal("native semantic invoke did not update fixture")
	}
	index = find(state, "Fixture text")
	call("click", Params{Window: &window, SnapshotID: state.SnapshotID, ElementIndex: &index})
	state = observe(false)
	call("press_key", Params{Window: &window, SnapshotID: state.SnapshotID, Key: "Command+a"})
	time.Sleep(100 * time.Millisecond)
	state = observe(false)
	call("type_text", Params{Window: &window, SnapshotID: state.SnapshotID, Text: "typed sample"})
	time.Sleep(150 * time.Millisecond)
	state = observe(false)
	typed := false
	for _, e := range state.Elements {
		if e.Value == "typed sample" {
			typed = true
		}
	}
	if !typed {
		t.Fatal("native keyboard input did not update fixture")
	}
}
