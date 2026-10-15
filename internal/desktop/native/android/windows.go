package android

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/qinyongliang/gosshd-bastion/internal/desktop/contract"
)

type windowInstance struct {
	Token, Package, Activity string
	Display                  int
	Bounds                   contract.Bounds
	Focused                  bool
	visible, surface         bool
}

var headerPattern = regexp.MustCompile(`^(?:Window #[0-9]+ )?Window\{([^\s}]+)\s+u[0-9]+\s+([^}]+)\}`)
var bracketFrame = regexp.MustCompile(`\[(-?[0-9]+),\s*(-?[0-9]+)\]\[(-?[0-9]+),\s*(-?[0-9]+)\]`)
var rectFrame = regexp.MustCompile(`Rect\((-?[0-9]+),\s*(-?[0-9]+)\s*-\s*(-?[0-9]+),\s*(-?[0-9]+)\)`)
var displayPattern = regexp.MustCompile(`\bcur=([0-9]+)x([0-9]+)\b`)

func parseWindows(dump string, size contract.Bounds) []windowInstance {
	var out []windowInstance
	var current *windowInstance
	flush := func() {
		if current != nil && current.surface && current.visible && current.Display == 0 && (current.Package == "android" || validPackageName(current.Package)) && insideDisplay(current.Bounds, size) {
			out = append(out, *current)
		}
		current = nil
	}
	focus := ""
	for _, line := range strings.Split(dump, "\n") {
		if strings.Contains(line, "mCurrentFocus=") {
			focus = focusToken(line)
			break
		}
	}
	for _, raw := range strings.Split(dump, "\n") {
		line := strings.TrimSpace(raw)
		if m := headerPattern.FindStringSubmatch(line); m != nil {
			flush()
			title := m[2]
			pkg := strings.SplitN(title, "/", 2)[0]
			current = &windowInstance{Token: m[1], Package: pkg, Activity: title, visible: true}
			continue
		}
		if current == nil {
			continue
		}
		if strings.Contains(line, "mDisplayId=") {
			current.Display, _ = strconv.Atoi(fieldValue(line, "mDisplayId="))
		}
		if strings.Contains(line, "package=") {
			current.Package = fieldValue(line, "package=")
		}
		if strings.Contains(line, "mHasSurface=") {
			current.surface = fieldValue(line, "mHasSurface=") == "true"
		}
		for _, key := range []string{"isOnScreen=", "isReadyForDisplay()="} {
			if strings.Contains(line, key) && fieldValue(line, key) == "false" {
				current.visible = false
			}
		}
		if strings.Contains(line, "mViewVisibility=0x") && fieldValue(line, "mViewVisibility=") != "0x0" {
			current.visible = false
		}
		if strings.HasPrefix(line, "Frames:") || strings.HasPrefix(line, "mFrame=") || strings.HasPrefix(line, "frame=") {
			if frame, ok := parseFrame(line); ok {
				current.Bounds = frame
			}
		}
	}
	flush()
	for i := range out {
		out[i].Focused = out[i].Token == focus
	}
	if len(out) > maxWindows {
		out = out[:maxWindows]
	}
	return out // WindowManager's order is retained; never sort away z-order.
}

func parseFrame(line string) (contract.Bounds, bool) {
	for _, key := range []string{"mFrame=", "frame=", "mBounds="} {
		if i := strings.Index(line, key); i >= 0 {
			line = line[i+len(key):]
			break
		}
	}
	m := bracketFrame.FindStringSubmatch(line)
	if m == nil {
		m = rectFrame.FindStringSubmatch(line)
	}
	if m == nil {
		return contract.Bounds{}, false
	}
	var v [4]int
	for i := range v {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return contract.Bounds{}, false
		}
		v[i] = n
	}
	if v[2] <= v[0] || v[3] <= v[1] {
		return contract.Bounds{}, false
	}
	return contract.Bounds{X: float64(v[0]), Y: float64(v[1]), Width: float64(v[2] - v[0]), Height: float64(v[3] - v[1])}, true
}
func insideDisplay(b, size contract.Bounds) bool {
	return b.Width >= 1 && b.Height >= 1 && b.X >= 0 && b.Y >= 0 && b.X+b.Width <= size.Width && b.Y+b.Height <= size.Height
}
func focusToken(value string) string {
	i := strings.Index(value, "Window{")
	if i < 0 {
		return ""
	}
	fields := strings.Fields(strings.TrimSuffix(value[i+len("Window{"):], "}"))
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}
func displayBounds(dump string) (contract.Bounds, error) {
	// Logical current geometry accounts for rotation and wm size overrides.
	start := strings.Index(dump, "Display: mDisplayId=0")
	if start < 0 {
		return contract.Bounds{}, errors.New("primary display geometry is unavailable")
	}
	section := dump[start:]
	if end := strings.Index(section[1:], "Display: mDisplayId="); end >= 0 {
		section = section[:end+1]
	}
	m := displayPattern.FindStringSubmatch(section)
	if m == nil {
		return contract.Bounds{}, errors.New("primary logical display geometry is unavailable")
	}
	w, _ := strconv.Atoi(m[1])
	h, _ := strconv.Atoi(m[2])
	if w < 1 || h < 1 || w > 16384 || h > 16384 || int64(w)*int64(h) > 64<<20 {
		return contract.Bounds{}, errors.New("invalid display geometry")
	}
	return contract.Bounds{Width: float64(w), Height: float64(h)}, nil
}
func (h *Helper) windowSnapshot(ctx context.Context) ([]windowInstance, contract.Bounds, string, error) {
	data, err := h.runTool(ctx, "dumpsys", "window", "-a")
	if err != nil {
		return nil, contract.Bounds{}, "", err
	}
	dump := string(data)
	size, err := displayBounds(dump)
	if err != nil {
		return nil, contract.Bounds{}, "", err
	}
	return parseWindows(dump, size), size, dump, nil
}
func matchWindow(windows []windowInstance, id, app string) (*windowInstance, error) {
	for i := range windows {
		if windows[i].Token == id && windows[i].Package == app {
			return &windows[i], nil
		}
	}
	return nil, errors.New("window no longer exists or is not visible on the primary display; enumerate again")
}
func windowObject(w windowInstance) contract.Window {
	return contract.Window{ID: w.Token, App: w.Package, Title: limitText(w.Activity, 512)}
}
func (h *Helper) listWindows(ctx context.Context) (any, error) {
	if err := h.requirePrivileged(); err != nil {
		return nil, err
	}
	windows, _, _, err := h.windowSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	list := make([]contract.Window, 0, len(windows))
	for _, w := range windows {
		list = append(list, windowObject(w))
	}
	return map[string]any{"windows": list}, nil
}
func (h *Helper) listApps(ctx context.Context) (any, error) {
	if err := h.requirePrivileged(); err != nil {
		return nil, err
	}
	windows, _, _, err := h.windowSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	order := []string{}
	groups := map[string][]contract.Window{}
	for _, w := range windows {
		if _, ok := groups[w.Package]; !ok {
			order = append(order, w.Package)
		}
		groups[w.Package] = append(groups[w.Package], windowObject(w))
	}
	list := make([]any, 0, len(order))
	for _, pkg := range order {
		list = append(list, map[string]any{"id": pkg, "windows": groups[pkg]})
	}
	return map[string]any{"apps": list}, nil
}
func (h *Helper) listDisplays(ctx context.Context) (any, error) {
	if err := h.requirePrivileged(); err != nil {
		return nil, err
	}
	_, size, _, err := h.windowSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{"displays": []any{map[string]any{"id": "0", "bounds": size, "primary": true}}}, nil
}
func (h *Helper) foreground(ctx context.Context, target windowInstance) error {
	if target.Focused {
		return nil
	}
	// Restore only a real activity component printed by WindowManager.
	if !validComponent(target.Activity) || strings.SplitN(target.Activity, "/", 2)[0] != target.Package {
		return errors.New("this window has no launchable activity; activate it on the device")
	}
	out, err := h.runTool(ctx, "am", "start", "-W", "-n", target.Activity)
	if err != nil {
		return err
	}
	if strings.Contains(string(out), "Error:") {
		return errors.New("activity activation failed")
	}
	windows, _, _, err := h.windowSnapshot(ctx)
	if err != nil {
		return err
	}
	current, err := matchWindow(windows, target.Token, target.Package)
	if err != nil || !current.Focused {
		return fmt.Errorf("cannot confirm target window focus; enumerate and observe again")
	}
	return nil
}
