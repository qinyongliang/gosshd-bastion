//go:build darwin

package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
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
	helper, err := startNativeProcess(ctx, Config{HelperPath: helperPath})
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
	if !status.Permissions.AX || !status.Permissions.Screen {
		t.Fatalf("required macOS permissions missing: screen_capture=%v accessibility=%v", status.Permissions.Screen, status.Permissions.AX)
	}

	pidFile := filepath.Join(t.TempDir(), "fixture.pid")
	fixture := exec.Command("/usr/bin/open", "-n", "-g", "-W", "-a", fixturePath, "--env", "GOSSHD_FIXTURE_PID_FILE="+pidFile)
	if err = fixture.Start(); err != nil {
		t.Fatal(err)
	}
	fixturePID := 0
	defer func() {
		if fixturePID > 1 {
			if process, e := os.FindProcess(fixturePID); e == nil {
				process.Kill()
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
		t.Fatal("fixture app did not report its own PID")
	}
	var window Window
	for i := 0; i < 50; i++ {
		res := call("list_windows", Params{})
		var list struct {
			Windows []Window `json:"windows"`
		}
		_ = json.Unmarshal(res.Result, &list)
		for _, w := range list.Windows {
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
	await := func(description string, match func(State) bool) State {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for {
			current := observe(false)
			if match(current) {
				return current
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s: owned fixture elements=%+v", description, current.Elements)
			}
			time.Sleep(25 * time.Millisecond)
		}
	}
	has := func(name, value string) func(State) bool {
		return func(current State) bool {
			for _, e := range current.Elements {
				if e.Name == name && e.Value == value {
					return true
				}
			}
			return false
		}
	}
	index := find(state, "Fixture text")
	call("set_value", Params{Window: &window, SnapshotID: state.SnapshotID, ElementIndex: &index, Text: "MCP sample"})
	state = await("semantic text write", has("Fixture text", "MCP sample"))
	index = find(state, "Apply")
	call("perform_action", Params{Window: &window, SnapshotID: state.SnapshotID, ElementIndex: &index, Action: "invoke"})
	state = await("semantic button invoke", func(current State) bool {
		for _, e := range current.Elements {
			if e.Name == "Applied: MCP sample" || e.Value == "Applied: MCP sample" {
				return true
			}
		}
		return false
	})
	index = find(state, "Fixture text")
	call("click", Params{Window: &window, SnapshotID: state.SnapshotID, ElementIndex: &index})
	state = observe(false)
	call("press_key", Params{Window: &window, SnapshotID: state.SnapshotID, Key: "Command+a"})
	state = observe(false)
	// Literal Unicode must not inherit Command from the preceding shortcut.
	typed := "typed sample 中文🙂"
	call("type_text", Params{Window: &window, SnapshotID: state.SnapshotID, Text: typed})
	state = await("keyboard input after Command chord", has("Fixture text", typed))
	call("press_key", Params{Window: &window, SnapshotID: state.SnapshotID, Key: "Command+a"})
	state = observe(false)
	longText := strings.Repeat("x", 19) + "🙂 中文" + strings.Repeat("y", 127)
	call("type_text", Params{Window: &window, SnapshotID: state.SnapshotID, Text: longText})
	state = await("Unicode chunk boundary", has("Fixture text", longText))
	index = find(state, "Fixture toggle")
	call("perform_action", Params{Window: &window, SnapshotID: state.SnapshotID, ElementIndex: &index, Action: "toggle"})
	state = await("native checkbox toggle", has("Fixture toggle", "1"))
	index = find(state, "Fixture slider")
	call("set_value", Params{Window: &window, SnapshotID: state.SnapshotID, ElementIndex: &index, Text: "9"})
	state = await("native range write", has("Fixture slider", "9"))
	index = find(state, "Fixture pointer")
	r := state.Elements[index].Bounds
	call("drag", Params{Window: &window, SnapshotID: state.SnapshotID, X: r.X + 20, Y: r.Y + 20, ToX: r.X + 100, ToY: r.Y + 65})
	state = await("native pointer drag", func(current State) bool {
		for _, e := range current.Elements {
			if e.Name == "Dragged" || e.Value == "Dragged" {
				return true
			}
		}
		return false
	})
	index = find(state, "Fixture pointer")
	r = state.Elements[index].Bounds
	call("scroll", Params{Window: &window, SnapshotID: state.SnapshotID, X: r.X + 30, Y: r.Y + 30, DeltaY: 120})
	await("native wheel input", func(current State) bool {
		for _, e := range current.Elements {
			if strings.HasPrefix(e.Name, "Scrolled:") || strings.HasPrefix(e.Value, "Scrolled:") {
				return true
			}
		}
		return false
	})
	call("screenshot", Params{Window: &window})
}
