// Package contract holds the desktop RPC wire types, bounds and the method
// table shared by the core module and every platform helper.
//
// It is a leaf package on purpose: the Android helper is Go code that the
// Agent embeds in-process, so it must be able to share these types without
// importing the core module that hosts it.
package contract

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
)

const MaxMessage = 16 << 20
const MaxImage = 8 << 20

type Window struct {
	ID    string `json:"id"`
	App   string `json:"app"`
	Title string `json:"title,omitempty"`
}
type Bounds struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}
type Element struct {
	Index                   int      `json:"index"`
	Parent                  int      `json:"parent"`
	Role                    string   `json:"role"`
	Name                    string   `json:"name,omitempty"`
	Value                   string   `json:"value,omitempty"`
	AutomationID            string   `json:"automation_id,omitempty"`
	Help                    string   `json:"help,omitempty"`
	Enabled                 bool     `json:"enabled"`
	Focused                 bool     `json:"focused,omitempty"`
	Selected                *bool    `json:"selected,omitempty"`
	Expanded                *bool    `json:"expanded,omitempty"`
	ToggleState             string   `json:"toggle_state,omitempty"`
	Minimum                 *float64 `json:"minimum,omitempty"`
	Maximum                 *float64 `json:"maximum,omitempty"`
	VerticalScrollable      *bool    `json:"vertical_scrollable,omitempty"`
	HorizontalScrollable    *bool    `json:"horizontal_scrollable,omitempty"`
	VerticalScrollPercent   *float64 `json:"vertical_scroll_percent,omitempty"`
	HorizontalScrollPercent *float64 `json:"horizontal_scroll_percent,omitempty"`
	Bounds                  Bounds   `json:"bounds"`
	Actions                 []string `json:"actions,omitempty"`
}
type Image struct {
	CaptureMethod string `json:"capture_method,omitempty"`
	MIMEType      string `json:"mime_type"`
	Data          []byte `json:"data"`
	Width         int    `json:"width"`
	Height        int    `json:"height"`
}
type ImageInfo struct {
	CaptureMethod string `json:"capture_method,omitempty"`
	MIMEType      string `json:"mime_type"`
	Width         int    `json:"width"`
	Height        int    `json:"height"`
}
type State struct {
	Mode              string     `json:"mode,omitempty"`
	ObservationMethod string     `json:"observation_method,omitempty"`
	Screenshot        *ImageInfo `json:"screenshot,omitempty"`
	CoordinateSpace   string     `json:"coordinate_space,omitempty"`
	Tree              string     `json:"accessibility_tree,omitempty"`
	SnapshotID        string     `json:"snapshot_id"`
	Window            Window     `json:"window"`
	Bounds            Bounds     `json:"bounds"`
	Elements          []Element  `json:"elements,omitempty"`
	Image             *Image     `json:"image,omitempty"`
	Truncated         bool       `json:"truncated,omitempty"`
}
type Params struct {
	// Empty/default preserves semantic-first operations with foreground fallback.
	Mode              string  `json:"mode,omitempty" jsonschema:"Execution mode: auto explicitly prefers semantics with allowed foreground fallback; default (or omitted) preserves compatibility; background strictly forbids focus, activation, global input and screen-crop fallback."`
	DisplayID         string  `json:"display_id,omitempty"`
	Window            *Window `json:"window,omitempty"`
	SnapshotID        string  `json:"snapshot_id,omitempty"`
	ElementIndex      *int    `json:"element_index,omitempty"`
	IncludeScreenshot *bool   `json:"include_screenshot,omitempty"`
	IncludeText       bool    `json:"include_text,omitempty"`
	MaxDepth          int     `json:"max_depth,omitempty"`
	MaxElements       int     `json:"max_elements,omitempty"`
	X                 float64 `json:"x,omitempty"`
	Y                 float64 `json:"y,omitempty"`
	ToX               float64 `json:"to_x,omitempty"`
	ToY               float64 `json:"to_y,omitempty"`
	DeltaX            float64 `json:"delta_x,omitempty"`
	DeltaY            float64 `json:"delta_y,omitempty"`
	Text              string  `json:"text,omitempty"`
	Key               string  `json:"key,omitempty"`
	Action            string  `json:"action,omitempty"`
	App               string  `json:"app,omitempty"`
	Button            string  `json:"button,omitempty"`
	ClickCount        int     `json:"click_count,omitempty"`
}
type Request struct {
	ID     string `json:"id"`
	Method string `json:"method"`
	Params Params `json:"params"`
}
type Response struct {
	ID        string          `json:"id"`
	OK        bool            `json:"ok"`
	Error     string          `json:"error,omitempty"`
	ErrorCode string          `json:"error_code,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
}

var readMethods = map[string]bool{"status": true, "list_windows": true, "list_apps": true, "get_window_state": true, "list_displays": true, "screenshot": true}
var controlMethods = map[string]bool{"activate_window": true, "launch_app": true, "click": true, "press_key": true, "type_text": true, "scroll": true, "drag": true, "set_value": true, "perform_action": true, "request_permissions": true}

func IsRead(method string) bool    { return readMethods[method] }
func IsControl(method string) bool { return controlMethods[method] }
func NeedsSnapshot(method string) bool {
	return IsControl(method) && method != "launch_app" && method != "request_permissions"
}
func Validate(req Request) error {
	if !readMethods[req.Method] && !controlMethods[req.Method] {
		return errors.New("unsupported desktop method")
	}
	p := req.Params
	if p.Mode != "" && p.Mode != "default" && p.Mode != "auto" && p.Mode != "background" {
		return errors.New("mode must be default, auto or background")
	}
	if req.Method == "get_window_state" || NeedsSnapshot(req.Method) {
		if p.Window == nil || p.Window.ID == "" || p.Window.App == "" {
			return errors.New("window id and app are required")
		}
	}
	if NeedsSnapshot(req.Method) && p.SnapshotID == "" {
		return errors.New("observe the window first; snapshot_id is required")
	}
	if len(p.DisplayID) > 80 {
		return errors.New("display identity too large")
	}
	if p.Window != nil && (len(p.Window.ID) > 80 || len(p.Window.App) > 80 || len(p.Window.Title) > 2048) {
		return errors.New("window identity too large")
	}
	if p.ElementIndex != nil && (*p.ElementIndex < 0 || *p.ElementIndex >= 1024) {
		return errors.New("invalid element index")
	}
	if (req.Method == "set_value" || req.Method == "perform_action") && p.ElementIndex == nil {
		return errors.New("element_index is required")
	}
	if p.MaxDepth < 0 || p.MaxDepth > 16 || p.MaxElements < 0 || p.MaxElements > 1024 {
		return errors.New("accessibility limits exceeded")
	}
	if len(p.Text) > 16384 || len(p.Key) > 80 || len(p.App) > 4096 || len(p.Action) > 80 || len(req.ID) > 80 {
		return errors.New("desktop argument too large")
	}
	for _, v := range []float64{p.X, p.Y, p.ToX, p.ToY, p.DeltaX, p.DeltaY} {
		if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > 100000 {
			return errors.New("invalid coordinate")
		}
	}
	if p.Button != "" && p.Button != "left" && p.Button != "right" && p.Button != "middle" {
		return errors.New("invalid mouse button")
	}
	if p.ClickCount < 0 || p.ClickCount > 2 {
		return errors.New("click_count must be 1 or 2")
	}
	if req.Method == "press_key" && p.Key == "" {
		return errors.New("key is required")
	}
	if req.Method == "launch_app" && p.App == "" {
		return errors.New("app is required")
	}
	if req.Method == "perform_action" {
		switch p.Action {
		case "invoke", "toggle", "expand", "collapse", "select", "focus", "secondary":
		default:
			return fmt.Errorf("unsupported accessibility action %q", p.Action)
		}
	}
	return ValidateBackground(req.Method, p)
}
