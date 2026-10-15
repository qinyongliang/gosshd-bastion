package android

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

func (h *Helper) privileged() bool { return h.UID == 0 || h.UID == shellUID }
func (h *Helper) requirePrivileged() error {
	if !h.privileged() {
		return fmt.Errorf("Android desktop requires root or adb shell uid 2000; current uid %d is unsupported", h.UID)
	}
	return nil
}
func (h *Helper) sdk(ctx context.Context) int {
	if h.SDK > 0 {
		return h.SDK
	}
	out, err := h.runTool(ctx, "getprop", "ro.build.version.sdk")
	if err != nil {
		return 0
	}
	value, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return value
}
func (h *Helper) status(ctx context.Context) (any, error) {
	permissions := map[string]any{"screen_capture": false, "accessibility": false}
	result := map[string]any{"platform": "android", "background_supported": false, "supported_modes": []string{"default", "auto"}, "interactive": false, "protocol_version": protocolVersion, "root": h.UID == 0, "shell_user": h.UID == shellUID, "permissions": permissions, "capture_backend": "screencap", "input_backend": "input", "sdk": h.sdk(ctx), "primary_display_only": true, "semantic_actions": false, "unicode_text": false, "set_value": false}
	if !h.privileged() {
		result["unavailable"] = "Use a root or adb shell Agent; non-root Termux cannot control the screen"
		return result, nil
	}
	for _, tool := range []string{"dumpsys", "screencap", "uiautomator", "input"} {
		if _, err := h.tool(tool); err != nil {
			result["unavailable"] = err.Error()
			return result, nil
		}
	}
	// UID eligibility is not proof of a grant: probe each read path without input.
	data, err := h.runTool(ctx, "screencap", "-p")
	if err == nil {
		_, err = decodePNG(data)
	}
	permissions["screen_capture"] = err == nil
	if err != nil {
		result["unavailable"] = "screen capture probe failed"
	}
	windows, _, dump, err := h.windowSnapshot(ctx)
	if err != nil {
		result["unavailable"] = "WindowManager access or display geometry unavailable"
		return result, nil
	}
	awake, err := h.screenAwake(ctx)
	if err != nil {
		result["unavailable"] = err.Error()
		return result, nil
	}
	locked, known := lockState(dump)
	result["screen_on"], result["locked"], result["lock_state_known"] = awake, locked, known
	if !awake || !known || locked {
		return result, nil
	}
	for _, w := range windows {
		if w.Focused {
			_, _, _, err = h.dumpTree(ctx, w.Bounds, w.Package, 1, 1)
			permissions["accessibility"] = err == nil
			break
		}
	}
	result["interactive"] = permissions["screen_capture"] == true && permissions["accessibility"] == true
	return result, nil
}
func (h *Helper) screenAwake(ctx context.Context) (bool, error) {
	data, err := h.runTool(ctx, "dumpsys", "power")
	if err != nil {
		return false, err
	}
	switch fieldValue(string(data), "mWakefulness=") {
	case "Awake":
		return true, nil
	case "Asleep", "Dozing", "Dreaming":
		return false, nil
	}
	return false, errors.New("device power state is unknown")
}
func lockState(dump string) (bool, bool) {
	known := false
	locked := false
	for _, key := range []string{"mShowingLockscreen=", "mDreamingLockscreen=", "isStatusBarKeyguard=", "mKeyguardShowing="} {
		switch fieldValue(dump, key) {
		case "true":
			known = true
			locked = true
		case "false":
			known = true
		}
	}
	if i := strings.Index(dump, "KeyguardServiceDelegate"); i >= 0 {
		switch fieldValue(dump[i:], "showing=") {
		case "true":
			known = true
			locked = true
		case "false":
			known = true
		}
	}
	return locked, known
}
func (h *Helper) checkInteractive(ctx context.Context, dump string) error {
	awake, err := h.screenAwake(ctx)
	if err != nil {
		return err
	}
	if !awake {
		return errors.New("screen is off; wake and unlock the device locally")
	}
	locked, known := lockState(dump)
	if !known {
		return errors.New("lock state is unknown; desktop operations are unavailable")
	}
	if locked {
		return errors.New("device is locked; unlock it locally")
	}
	return nil
}
func fieldValue(text, key string) string {
	for _, line := range strings.Split(text, "\n") {
		i := strings.Index(line, key)
		if i < 0 {
			continue
		}
		value := strings.TrimSpace(line[i+len(key):])
		if j := strings.IndexAny(value, " \t,}"); j >= 0 {
			value = value[:j]
		}
		return value
	}
	return ""
}
