package android

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qinyongliang/gosshd-bastion/internal/desktop/contract"
)

// Synthesized fixtures only: never copy a live device dump into the repository.
const deviceDump = `WINDOW MANAGER POLICY STATE
  mShowingLockscreen=false
WINDOW MANAGER DISPLAY CONTENTS
  Display: mDisplayId=0 rootTasks=1
    init=120x200 320dpi cur=120x200 app=120x180
    mCurrentFocus=Window{cafe01 u0 com.example.app/.MainActivity}
WINDOW MANAGER WINDOWS
  Window #0 Window{cafe01 u0 com.example.app/.MainActivity}:
    mDisplayId=0 rootTaskId=1 mSession=Session{example}
    mAttrs={(0,0)(fillxfill) package=com.example.app ty=BASE_APPLICATION}
    mHasSurface=true isReadyForDisplay()=true mWindowRemovalAllowed=false
    Frames: parent=[0,0][120,200] display=[0,0][120,200] frame=[10,20][110,180] last=[0,0][0,0]
  Window #1 Window{dead01 u0 com.example.hidden/.Main}:
    mDisplayId=0
    mHasSurface=false isReadyForDisplay()=false
    mFrame=[0,0][120,200] last=[0,0][0,0]
  Window #2 Window{beef02 u0 com.example.external/.Main}:
    mDisplayId=1
    mHasSurface=true isReadyForDisplay()=true
    mFrame=[0,0][120,200]
`
const deviceTree = `<?xml version='1.0' encoding='UTF-8' standalone='yes' ?>
<hierarchy rotation="0"><node class="android.widget.FrameLayout" package="com.example.app" enabled="true" bounds="[10,20][110,180]">
  <node class="android.widget.Button" package="com.example.app" text="Example" resource-id="com.example.app:id/action" enabled="true" clickable="true" long-clickable="true" bounds="[30,40][70,60]"/>
  <node class="android.widget.EditText" package="com.example.app" text="PRIVATE_PASSWORD" password="true" bounds="[30,70][70,90]"/>
  <node class="android.widget.TextView" package="com.example.other" text="FOREIGN_WINDOW" bounds="[30,40][70,60]"/>
  <node class="android.widget.TextView" package="com.example.app" text="OFFSCREEN" bounds="[300,400][350,450]"/>
</node></hierarchy>`

type commandCall struct {
	name string
	args []string
}
type fakeRunner struct {
	calls                   []commandCall
	dump, tree, power, help string
	screenshot              []byte
	fail                    map[string]error
	hook                    func(string, []string) error
}

func (r *fakeRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, commandCall{name, append([]string(nil), args...)})
	if r.hook != nil {
		if err := r.hook(name, args); err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := r.fail[name]; err != nil {
		return nil, err
	}
	switch name {
	case "getprop":
		return []byte("34\n"), nil
	case "dumpsys":
		if args[0] == "window" {
			return []byte(r.dump), nil
		}
		if args[0] == "power" {
			return []byte(r.power), nil
		}
	case "uiautomator":
		return []byte("UI hierarchy dumped"), nil
	case "cat":
		return []byte(r.tree), nil
	case "screencap":
		return r.screenshot, nil
	case "input":
		if len(args) == 0 {
			return []byte(r.help), nil
		}
		return nil, nil
	case "monkey":
		return []byte("Events injected: 1"), nil
	case "am":
		return []byte("Status: ok"), nil
	}
	return nil, errors.New("unexpected command: " + name)
}
func (r *fakeRunner) inputs() []commandCall {
	var out []commandCall
	for _, c := range r.calls {
		if c.name == "input" && len(c.args) > 0 {
			out = append(out, c)
		}
	}
	return out
}
func fixture(t *testing.T) (*Helper, *fakeRunner) {
	t.Helper()
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 120, 200))
	for y := 0; y < 200; y++ {
		for x := 0; x < 120; x++ {
			img.SetRGBA(x, y, color.RGBA{uint8(x), uint8(y), 0, 255})
		}
	}
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	r := &fakeRunner{dump: deviceDump, tree: deviceTree, power: "mWakefulness=Awake\n", help: "keycombination <key codes>", screenshot: buf.Bytes(), fail: map[string]error{}}
	return &Helper{Runner: r, UID: 2000, TempDir: t.TempDir(), Lookup: func(n string) (string, error) { return n, nil }}, r
}
func observed(t *testing.T, h *Helper) contract.State {
	t.Helper()
	result, err := h.Dispatch(context.Background(), "get_window_state", contract.Params{Window: &contract.Window{ID: "cafe01", App: "com.example.app"}, IncludeText: true})
	if err != nil {
		t.Fatal(err)
	}
	return result.(contract.State)
}
func actionParams(s contract.State) contract.Params {
	return contract.Params{Window: &s.Window, SnapshotID: s.SnapshotID, X: 20, Y: 30}
}
func TestAndroidWindowFormats(t *testing.T) {
	windows := parseWindows(deviceDump, contract.Bounds{Width: 120, Height: 200})
	if len(windows) != 1 || windows[0].Token != "cafe01" || !windows[0].Focused || windows[0].Bounds != (contract.Bounds{X: 10, Y: 20, Width: 100, Height: 160}) {
		t.Fatalf("bad inventory: %+v", windows)
	}
	for _, v := range []string{"mFrame=[10,20][110,180] last=[0,0][0,0]", "Frames: parent=[0,0][120,200] frame=[10,20][110,180]", "mFrame=Rect(10, 20 - 110, 180)"} {
		b, ok := parseFrame(v)
		if !ok || b != windows[0].Bounds {
			t.Fatalf("frame %q: %+v %v", v, b, ok)
		}
	}
}
func TestAndroidObservationCropTreeAndIndexes(t *testing.T) {
	h, _ := fixture(t)
	s := observed(t, h)
	if s.Image.Width != 100 || s.Image.Height != 160 || s.Bounds != (contract.Bounds{X: 10, Y: 20, Width: 100, Height: 160}) {
		t.Fatalf("geometry: %+v", s)
	}
	img, err := png.Decode(bytes.NewReader(s.Image.Data))
	if err != nil {
		t.Fatal(err)
	}
	red, green, _, _ := img.At(0, 0).RGBA()
	if red>>8 != 10 || green>>8 != 20 {
		t.Fatalf("wrong crop: %d %d", red, green)
	}
	if len(s.Elements) != 2 || s.Elements[0].Parent != -1 || s.Elements[1].Index != 1 || s.Elements[1].Parent != 0 || s.Elements[1].Bounds.X != 20 {
		t.Fatalf("bad tree: %+v", s.Elements)
	}
	b, _ := json.Marshal(s)
	if strings.Contains(string(b), "PRIVATE_PASSWORD") || strings.Contains(string(b), "FOREIGN_WINDOW") {
		t.Fatal("unscoped text exposed")
	}
	entries, _ := os.ReadDir(h.TempDir)
	if len(entries) != 0 {
		t.Fatal("dump directories survived observation")
	}
}
func TestAndroidFreshnessAndFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Helper, *fakeRunner)
		params func(contract.Params) contract.Params
	}{
		{"stale", nil, func(p contract.Params) contract.Params { p.SnapshotID = "wrong"; return p }},
		{"moved", func(_ *Helper, r *fakeRunner) {
			r.dump = strings.ReplaceAll(r.dump, "frame=[10,20][110,180]", "frame=[11,20][111,180]")
		}, nil},
		{"focus", func(_ *Helper, r *fakeRunner) {
			r.dump = strings.ReplaceAll(r.dump, "mCurrentFocus=Window{cafe01 u0 com.example.app/.MainActivity}", "mCurrentFocus=null")
		}, nil},
		{"off", func(_ *Helper, r *fakeRunner) { r.power = "mWakefulness=Asleep" }, nil},
		{"locked", func(_ *Helper, r *fakeRunner) {
			r.dump = strings.ReplaceAll(r.dump, "mShowingLockscreen=false", "mShowingLockscreen=true")
		}, nil},
		{"unknown-lock", func(_ *Helper, r *fakeRunner) { r.dump = strings.ReplaceAll(r.dump, "mShowingLockscreen=false", "") }, nil},
		{"outside", nil, func(p contract.Params) contract.Params { p.X = 100; return p }},
		{"expired", func(h *Helper, _ *fakeRunner) { h.Now = func() time.Time { return time.Now().Add(2 * time.Minute) } }, nil},
		{"dispatch-error", func(_ *Helper, r *fakeRunner) { r.fail["input"] = errors.New("denied") }, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, r := fixture(t)
			s := observed(t, h)
			p := actionParams(s)
			if tc.change != nil {
				tc.change(h, r)
			}
			if tc.params != nil {
				p = tc.params(p)
			}
			if _, err := h.Dispatch(context.Background(), "click", p); err == nil {
				t.Fatal("unsafe action accepted")
			}
			before := len(r.inputs())
			if tc.name != "dispatch-error" && before != 0 {
				t.Fatal("unsafe input sent")
			}
			if _, err := h.Dispatch(context.Background(), "click", actionParams(s)); err == nil {
				t.Fatal("failed action did not consume snapshot")
			}
			if len(r.inputs()) != before {
				t.Fatal("consumed action sent input")
			}
		})
	}
}
func TestAndroidLiteralTextAndChord(t *testing.T) {
	h, r := fixture(t)
	s := observed(t, h)
	p := actionParams(s)
	p.Text = "abc %s;$(echo)"
	if _, err := h.Dispatch(context.Background(), "type_text", p); err != nil {
		t.Fatal(err)
	}
	var typed strings.Builder
	for _, c := range r.inputs() {
		if c.args[0] == "text" {
			if strings.Contains(c.args[1], "%s") {
				t.Fatal("literal percent-s would be rewritten by input")
			}
			typed.WriteString(c.args[1])
		} else if c.args[0] == "keyevent" && c.args[1] == "62" {
			typed.WriteByte(' ')
		}
	}
	if typed.String() != p.Text {
		t.Fatalf("not literal: %q", typed.String())
	}
	h, r = fixture(t)
	s = observed(t, h)
	p = actionParams(s)
	p.Text = "valid prefix 中文"
	if _, err := h.Dispatch(context.Background(), "type_text", p); err == nil || len(r.inputs()) != 0 {
		t.Fatal("invalid text partially typed")
	}
	h, r = fixture(t)
	s = observed(t, h)
	p = actionParams(s)
	p.Key = "Ctrl+a"
	if _, err := h.Dispatch(context.Background(), "press_key", p); err != nil {
		t.Fatal(err)
	}
	calls := r.inputs()
	if len(calls) != 1 || strings.Join(calls[0].args, " ") != "keycombination 113 29" {
		t.Fatalf("not a chord: %+v", calls)
	}
	h, r = fixture(t)
	r.help = "keyevent <codes>"
	s = observed(t, h)
	p = actionParams(s)
	p.Key = "Ctrl+a"
	if _, err := h.Dispatch(context.Background(), "press_key", p); err == nil || len(r.inputs()) != 0 {
		t.Fatal("unsupported chord was sent sequentially")
	}
}
func TestAndroidPermissionStatus(t *testing.T) {
	h, r := fixture(t)
	h.UID = 10001
	result, err := h.Dispatch(context.Background(), "status", contract.Params{})
	if err != nil {
		t.Fatal(err)
	}
	status := result.(map[string]any)
	if status["interactive"] != false {
		t.Fatal("Termux reported controllable")
	}
	if _, err := h.Dispatch(context.Background(), "list_windows", contract.Params{}); err == nil {
		t.Fatal("Termux read allowed")
	}
	if len(r.inputs()) != 0 {
		t.Fatal("permission probe injected input")
	}
	h, r = fixture(t)
	r.fail["dumpsys"] = errors.New("SELinux denied")
	result, err = h.Dispatch(context.Background(), "status", contract.Params{})
	if err != nil {
		t.Fatal(err)
	}
	if result.(map[string]any)["interactive"] != false {
		t.Fatal("denial reported interactive")
	}
}
func TestAndroidDumpCleanupOnFailure(t *testing.T) {
	h, r := fixture(t)
	r.fail["cat"] = errors.New("read failure")
	if _, err := h.Dispatch(context.Background(), "get_window_state", contract.Params{Window: &contract.Window{ID: "cafe01", App: "com.example.app"}, IncludeText: true}); err == nil {
		t.Fatal("dump failure ignored")
	}
	entries, _ := os.ReadDir(h.TempDir)
	if len(entries) != 0 {
		t.Fatal("private dump path leaked")
	}
	for _, c := range r.calls {
		if c.name == "uiautomator" {
			dir := filepath.Dir(c.args[len(c.args)-1])
			if dir == h.TempDir {
				t.Fatal("dump not in private directory")
			}
		}
	}
}

func TestAndroidTreeLimitsAndMalformedData(t *testing.T) {
	h, _ := fixture(t)
	elements, refs, truncated, err := parseTree([]byte(deviceTree), contract.Bounds{X: 10, Y: 20, Width: 100, Height: 160}, "com.example.app", 8, 1)
	if err != nil || !truncated || len(elements) != 1 || len(refs) != 1 {
		t.Fatalf("limit: %v %v %v", err, truncated, elements)
	}
	elements, _, truncated, err = parseTree([]byte(deviceTree), contract.Bounds{X: 10, Y: 20, Width: 100, Height: 160}, "com.example.app", 0, 512)
	if err != nil || !truncated || len(elements) != 1 {
		t.Fatalf("depth: %v %v %v", err, truncated, elements)
	}
	if _, _, _, err = parseTree([]byte("<hierarchy><node>"), contract.Bounds{}, "com.example.app", 8, 512); err == nil {
		t.Fatal("malformed XML accepted")
	}
	h.TempDir = "relative"
	if _, _, _, err = h.dumpTree(context.Background(), contract.Bounds{}, "com.example.app", 1, 1); err == nil {
		t.Fatal("relative temp directory accepted")
	}
}
func TestAndroidDisplayRotationAndScreenshotMismatch(t *testing.T) {
	size, err := displayBounds(strings.ReplaceAll(deviceDump, "cur=120x200", "cur=200x120"))
	if err != nil || size.Width != 200 || size.Height != 120 {
		t.Fatalf("rotation: %+v %v", size, err)
	}
	h, r := fixture(t)
	r.dump = strings.ReplaceAll(r.dump, "cur=120x200", "cur=121x200")
	if _, err = h.Dispatch(context.Background(), "screenshot", contract.Params{}); err == nil {
		t.Fatal("screenshot/display mismatch accepted")
	}
	h, r = fixture(t)
	s := observed(t, h)
	if _, err = h.Dispatch(context.Background(), "screenshot", contract.Params{}); err != nil {
		t.Fatal(err)
	}
	if _, err = h.Dispatch(context.Background(), "click", actionParams(s)); err == nil || len(r.inputs()) != 0 {
		t.Fatal("display screenshot preserved input snapshot")
	}
}
func TestAndroidGestureTranslation(t *testing.T) {
	h, r := fixture(t)
	s := observed(t, h)
	p := actionParams(s)
	p.ElementIndex = new(int)
	*p.ElementIndex = 1
	result, err := h.Dispatch(context.Background(), "click", p)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("successful action returned null")
	}
	calls := r.inputs()
	if len(calls) != 1 || strings.Join(calls[0].args, " ") != "tap 50 50" {
		t.Fatalf("center: %+v", calls)
	}
	h, r = fixture(t)
	s = observed(t, h)
	p = actionParams(s)
	p.ElementIndex = new(int)
	*p.ElementIndex = 1
	p.Action = "secondary"
	if _, err = h.Dispatch(context.Background(), "perform_action", p); err != nil {
		t.Fatal(err)
	}
	calls = r.inputs()
	if strings.Join(calls[0].args, " ") != "swipe 50 50 50 50 700" {
		t.Fatalf("long press: %+v", calls)
	}
	h, r = fixture(t)
	s = observed(t, h)
	p = actionParams(s)
	p.X = 99
	p.Y = 159
	p.DeltaX = -120
	p.DeltaY = -120
	if _, err = h.Dispatch(context.Background(), "scroll", p); err == nil {
		t.Fatal("zero-movement edge scroll was injected")
	}
}
func TestAndroidSessionIsolationAndInvalidation(t *testing.T) {
	h, r := fixture(t)
	other, _ := fixture(t)
	s := observed(t, h)
	if _, err := other.Dispatch(context.Background(), "click", actionParams(s)); err == nil {
		t.Fatal("snapshot crossed helpers")
	}
	p := actionParams(s)
	p.ClickCount = 3
	if _, err := h.Dispatch(context.Background(), "click", p); err == nil {
		t.Fatal("invalid action accepted")
	}
	if _, err := h.Dispatch(context.Background(), "click", actionParams(s)); err == nil || len(r.inputs()) != 0 {
		t.Fatal("invalid action preserved snapshot")
	}
}
func TestAndroidServeValidatesAndHonorsCancellation(t *testing.T) {
	h, _ := fixture(t)
	h.UID = 10001
	input := strings.NewReader("not-json\n{\"id\":\"a\",\"method\":\"exec\",\"params\":{}}\n{\"id\":\"b\",\"method\":\"status\",\"params\":{}}\n")
	var output bytes.Buffer
	if err := Serve(context.Background(), input, &output, h); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(&output)
	for i := 0; i < 3; i++ {
		var res contract.Response
		if err := decoder.Decode(&res); err != nil {
			t.Fatal(err)
		}
		if res.OK != (i == 2) {
			t.Fatalf("validation: %+v", res)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Serve(ctx, strings.NewReader("{}\n"), &output, h); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}
func TestAndroidLaunchValidationAndConfirmation(t *testing.T) {
	for _, app := range []string{"com.example.app;input tap 0 0", "/system/bin/sh", "com..app", "com.1app"} {
		if validPackageName(app) {
			t.Fatal("invalid app accepted: " + app)
		}
	}
	h, r := fixture(t)
	if _, err := h.Dispatch(context.Background(), "launch_app", contract.Params{App: "com.example.app"}); err != nil {
		t.Fatal(err)
	}
	r.hook = func(name string, _ []string) error {
		if name == "monkey" {
			return errors.New("launch denied")
		}
		return nil
	}
	if _, err := h.Dispatch(context.Background(), "launch_app", contract.Params{App: "com.example.app"}); err == nil {
		t.Fatal("launch error ignored")
	}
}

func TestAndroidOcclusionPreventsTouch(t *testing.T) {
	h, r := fixture(t)
	// Non-focusable overlay is above the app and still receives touch.
	r.dump = strings.Replace(r.dump, "  Window #0 Window{cafe01", `  Window #0 Window{overlay01 u0 com.example.overlay/.Dialog}:
    mDisplayId=0
    mHasSurface=true isReadyForDisplay()=true
    mFrame=[20,30][80,80]
  Window #1 Window{cafe01`, 1)
	s := observed(t, h)
	if _, err := h.Dispatch(context.Background(), "click", actionParams(s)); err == nil || len(r.inputs()) != 0 {
		t.Fatal("touch sent through overlay")
	}
}

func TestAndroidInactiveWindowMetadataThenActivation(t *testing.T) {
	h, r := fixture(t)
	r.dump = strings.ReplaceAll(r.dump, "mCurrentFocus=Window{cafe01 u0 com.example.app/.MainActivity}", "mCurrentFocus=null")
	disabled := false
	p := contract.Params{Window: &contract.Window{ID: "cafe01", App: "com.example.app"}, IncludeScreenshot: &disabled}
	result, err := h.Dispatch(context.Background(), "get_window_state", p)
	if err != nil {
		t.Fatal(err)
	}
	state := result.(contract.State)
	if state.Image != nil || len(state.Elements) != 0 {
		t.Fatal("inactive content returned")
	}
	r.hook = func(name string, _ []string) error {
		if name == "am" {
			r.dump = strings.ReplaceAll(r.dump, "mCurrentFocus=null", "mCurrentFocus=Window{cafe01 u0 com.example.app/.MainActivity}")
		}
		return nil
	}
	if _, err = h.Dispatch(context.Background(), "activate_window", actionParams(state)); err != nil {
		t.Fatal(err)
	}
	if _, err = h.Dispatch(context.Background(), "click", actionParams(state)); err == nil {
		t.Fatal("activation snapshot reused")
	}
}

func TestAndroidDetailedWindowDumpIgnoresInsetsAndKeepsSystemDialogs(t *testing.T) {
	dump := strings.Replace(deviceDump, "  Window #0", `  Window #0 Window{system01 u0 Example system dialog}:
    mDisplayId=0
    mOwnerUid=1000 package=android appop=NONE
    mHasSurface=true isReadyForDisplay()=true
    Frames: parent=[0,0][120,200] display=[0,0][120,200] frame=[20,30][100,80]
  Window #1`, 1)
	dump = strings.Replace(dump, "  Window #1 Window{dead01", `    InsetsSource id=1 type=statusBars frame=[0,0][120,15] visible=true
    InsetsSource id=2 type=navigationBars frame=[0,180][120,200] visible=true
  Window #2 Window{dead01`, 1)
	windows := parseWindows(dump, contract.Bounds{Width: 120, Height: 200})
	if len(windows) != 2 || windows[0].Package != "android" || windows[1].Bounds != (contract.Bounds{X: 10, Y: 20, Width: 100, Height: 160}) {
		t.Fatalf("bad detailed inventory: %+v", windows)
	}
	h, r := fixture(t)
	if _, _, _, err := h.windowSnapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 1 || strings.Join(r.calls[0].args, " ") != "window -a" {
		t.Fatalf("window geometry requires detailed dump: %+v", r.calls)
	}
}
