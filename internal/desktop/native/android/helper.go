// Package android implements the primary-display backend using fixed Android
// platform commands. It depends only on the leaf RPC contract, not on Agent,
// server, store or MCP packages. Runner is injected for host-side regressions.
package android

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/qinyongliang/gosshd-bastion/internal/desktop/contract"
)

const (
	protocolVersion = 2
	observationTTL  = 60 * time.Second
	shellUID        = 2000
	maxWindows      = 512
	maxDepthLimit   = 16
	maxElementLimit = 1024
)

var errPrimaryDisplay = errors.New("Android desktop supports only primary display 0")

type Runner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}
type observation struct {
	snapshot             string
	window               contract.Window
	frame, displayBounds contract.Bounds
	at                   time.Time
	refs                 []elementRef
}

func (o *observation) ref(index int) (elementRef, error) {
	if index < 0 || index >= len(o.refs) {
		return elementRef{}, errors.New("element_index is not in the latest observation")
	}
	return o.refs[index], nil
}

type Helper struct {
	Runner   Runner
	Lookup   func(string) (string, error)
	UID, SDK int
	TempDir  string
	Now      func() time.Time
	gate     sync.Mutex
	observed *observation
}

func NewHelper() *Helper { return &Helper{Runner: LocalRunner{}, UID: os.Geteuid()} }
func (h *Helper) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}
func (h *Helper) tool(name string) (string, error) {
	if h.Lookup != nil {
		return h.Lookup(name)
	}
	// Never fall back to a caller-controlled PATH for privileged device tools.
	p := "/system/bin/" + name
	if st, err := os.Stat(p); err != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0111 == 0 {
		return "", fmt.Errorf("Android tool %s is unavailable", name)
	}
	return p, nil
}
func (h *Helper) runTool(ctx context.Context, name string, args ...string) ([]byte, error) {
	if h.Runner == nil {
		return nil, errors.New("Android runner is unavailable")
	}
	p, err := h.tool(name)
	if err != nil {
		return nil, err
	}
	return h.Runner.Run(ctx, p, args...)
}
func (h *Helper) Dispatch(ctx context.Context, method string, p contract.Params) (any, error) {
	h.gate.Lock()
	defer h.gate.Unlock()
	// All attempted controls/observations consume the snapshot even on invalid input.
	saved := h.observed
	if contract.IsControl(method) || method == "screenshot" || method == "get_window_state" {
		h.observed = nil
	}
	if err := contract.Validate(contract.Request{Method: method, Params: p}); err != nil {
		return nil, err
	}
	if p.Mode == "background" && (contract.IsControl(method) || method == "screenshot" || method == "get_window_state") {
		return nil, &contract.ProtocolError{Code: contract.BackgroundNotSupported, Message: "Android shell backend has no independent background input/window capture; use auto or default"}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch method {
	case "status":
		return h.status(ctx)
	case "request_permissions":
		return map[string]any{"ok": false, "message": "Enable root or adb shell access locally; Android has no blanket desktop permission prompt and non-root Termux is unsupported"}, nil
	}
	if err := h.requirePrivileged(); err != nil {
		return nil, err
	}
	switch method {
	case "list_windows":
		return h.listWindows(ctx)
	case "list_apps":
		return h.listApps(ctx)
	case "list_displays":
		return h.listDisplays(ctx)
	case "get_window_state":
		return h.observe(ctx, p)
	case "screenshot":
		return h.screenshot(ctx, p)
	case "launch_app":
		_, _, dump, err := h.windowSnapshot(ctx)
		if err != nil {
			return nil, err
		}
		if err = h.checkInteractive(ctx, dump); err != nil {
			return nil, err
		}
		if err = h.launchApp(ctx, p.App); err != nil {
			return nil, err
		}
		return map[string]bool{"ok": true}, nil
	}
	if saved == nil || p.SnapshotID != saved.snapshot || p.Window.ID != saved.window.ID || p.Window.App != saved.window.App || h.now().Sub(saved.at) > observationTTL {
		return nil, errors.New("stale or consumed snapshot; observe this window again")
	}
	windows, size, dump, err := h.windowSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	if err = h.checkInteractive(ctx, dump); err != nil {
		return nil, err
	}
	target, err := matchWindow(windows, saved.window.ID, saved.window.App)
	if err != nil {
		return nil, err
	}
	if target.Bounds != saved.frame || size != saved.displayBounds {
		return nil, errors.New("window or display geometry changed; observe again")
	}
	if method == "activate_window" {
		err = h.foreground(ctx, *target)
	} else {
		// Coordinate input is global on Android. Restrict it to the focused visible
		// target; an inactive/occluded window must be activated then observed again.
		if !target.Focused {
			return nil, errors.New("target window is not focused; activate and observe again")
		}
		switch method {
		case "click":
			err = h.click(ctx, saved, *target, p)
		case "drag":
			err = h.drag(ctx, saved, *target, p)
		case "scroll":
			err = h.scroll(ctx, saved, *target, p)
		case "press_key":
			err = h.pressKey(ctx, saved, *target, p.Key)
		case "type_text":
			err = h.typeText(ctx, saved, *target, p.Text)
		case "perform_action":
			err = h.performAction(ctx, saved, *target, p)
		case "set_value":
			err = errors.New("Android shell backend has no semantic set_value; focus an editable field then type_text")
		default:
			err = errors.New("unsupported desktop operation")
		}
	}
	if err != nil {
		return nil, err
	}
	return map[string]bool{"ok": true}, nil
}
