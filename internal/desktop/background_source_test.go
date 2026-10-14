package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Structural regression checks are intentionally not labeled native GUI tests.
// They catch removal of the native guard and accidental unconditional activation
// or capture fallback on hosts that cannot execute the other platform's helper.
func TestNativeBackgroundSourceContract(t *testing.T) {
	read := func(platform, file string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join("native", platform, file))
		if err != nil {
			t.Fatal(err)
		}
		return strings.ReplaceAll(string(b), "\r\n", "\n")
	}
	section := func(source, start, end string) string {
		t.Helper()
		i := strings.Index(source, start)
		if i < 0 {
			t.Fatalf("missing %q", start)
		}
		source = source[i+len(start):]
		i = strings.Index(source, end)
		if i < 0 {
			t.Fatalf("missing %q", end)
		}
		return source[:i]
	}
	windows := read("windows", "DesktopHelper.cs")
	mac := read("macos", "DesktopHelper.swift")
	for _, source := range []string{windows, mac} {
		for _, name := range []string{"background_not_supported", "background_supported", "capture_method", "operation_method"} {
			if !strings.Contains(source, name) {
				t.Fatalf("native contract missing %s", name)
			}
		}
	}
	if !strings.Contains(windows, "{ValidateMode(method,p);switch(method)") {
		t.Fatal("Windows native mode guard must precede dispatch")
	}
	if !strings.Contains(mac, "try validateMode(method,p)\n switch method") {
		t.Fatal("macOS native mode guard must precede dispatch")
	}
	screenshot := section(windows, "static object Screenshot(", "static object CaptureScreenshot(")
	if !strings.Contains(windows, `RequireScreenFallback(background);method="visible_screen";return screenCapture()`) || !strings.Contains(screenshot, "ChooseBitmap(background,") {
		t.Fatal("Windows visible-screen fallback lacks strict refusal")
	}
	for _, unsafe := range []string{"Activate(", "ShowWindow(", "SetFocus(", "SetCursorPos(", "Send("} {
		if strings.Contains(screenshot, unsafe) {
			t.Fatalf("window capture introduced %s", unsafe)
		}
	}
	capture := section(mac, "func screenshot(", "func observe(")
	if !strings.Contains(capture, "background && info.isEmpty") || !strings.Contains(capture, "SCContentFilter(desktopIndependentWindow:window)") {
		t.Fatal("macOS capture is not window-only in strict mode")
	}
	for _, unsafe := range []string{"activate(", "CGEvent(", "CGDisplayCreateImage", "sourceRect"} {
		if strings.Contains(capture, unsafe) {
			t.Fatalf("macOS capture introduced %s", unsafe)
		}
	}
	operations := section(mac, "func operate(", "func dispatch(")
	preSwitch := section(operations, "let info=", "switch method")
	if strings.Contains(preSwitch, "activate(info)") {
		t.Fatal("macOS activates before semantic dispatch")
	}
	for _, pair := range [][2]string{{"case \"set_value\":", "case \"perform_action\":"}, {"case \"perform_action\":", "case \"press_key\":"}} {
		semantic := section(operations, pair[0], pair[1])
		if strings.Contains(semantic, "CGEvent") || strings.Contains(semantic, ".post(") {
			t.Fatal("semantic AX path introduced global input")
		}
		if pair[0] == "case \"set_value\":" && strings.Contains(semantic, "activate(") {
			t.Fatal("AX value write activates")
		}
	}
	if !strings.Contains(operations, "validateScrollActions(requested,available,isBackground)") {
		t.Fatal("macOS scrolling must preflight all capabilities")
	}
	for _, native := range []string{"AXUIElementIsAttributeSettable", "AXUIElementCopyActionNames", "AXUIElementSetAttributeValue", "AXUIElementPerformAction"} {
		if !strings.Contains(mac, native) {
			t.Fatalf("AX capability missing %s", native)
		}
	}
}
