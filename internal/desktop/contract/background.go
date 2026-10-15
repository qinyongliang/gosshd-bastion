package contract

import "strings"

const BackgroundNotSupported = "background_not_supported"

// ProtocolError preserves a native error code across independent RPC and MCP.
type ProtocolError struct{ Code, Message string }

func (e *ProtocolError) Error() string {
	if e.Code == "" || strings.HasPrefix(e.Message, e.Code+":") {
		return e.Message
	}
	return e.Code + ": " + e.Message
}
func backgroundUnsupported(reason string) error {
	return &ProtocolError{Code: BackgroundNotSupported, Message: reason}
}
func ValidateBackground(method string, p Params) error {
	if p.Mode != "background" {
		return nil
	}
	switch method {
	case "activate_window", "launch_app", "type_text", "press_key", "drag", "request_permissions":
		return backgroundUnsupported(method + " requires foreground input or user interaction")
	case "perform_action":
		if p.Action == "focus" {
			return backgroundUnsupported("focus changes the shared desktop focus")
		}
	case "click":
		if p.ElementIndex == nil || (p.Button != "" && p.Button != "left") || p.ClickCount == 2 {
			return backgroundUnsupported("click requires an element supporting a single semantic invoke")
		}
	case "scroll":
		if p.ElementIndex == nil {
			return backgroundUnsupported("scroll requires an element supporting native semantic scrolling")
		}
	case "screenshot":
		if p.Window == nil || p.Window.ID == "" || p.Window.App == "" {
			return backgroundUnsupported("capture requires a specific window; display capture is not a background window capture")
		}
	}
	return nil
}
