package mcptools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"net"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/qinyongliang/gosshd-bastion/internal/desktop"
)

func testService(t *testing.T) *desktop.Service {
	t.Helper()
	service := desktop.NewService(desktop.Adapter{Authorize: func(context.Context, string, string, bool) error { return nil }, Dial: func(context.Context, string) (io.ReadWriteCloser, error) {
		a, b := net.Pipe()
		go func() {
			defer b.Close()
			encoder := json.NewEncoder(b)
			_ = encoder.Encode(desktop.Response{ID: "init", OK: true})
			scan := bufio.NewScanner(b)
			for scan.Scan() {
				var req desktop.Request
				_ = json.Unmarshal(scan.Bytes(), &req)
				var result any = map[string]any{"ok": true, "background_supported": true}
				if req.Method == "get_window_state" {
					var data bytes.Buffer
					_ = png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 2, 2)))
					elements := []desktop.Element(nil)
					if req.Params.IncludeText {
						elements = []desktop.Element{
							{Index: 0, Parent: -1, Role: "ControlType.Window", Name: "Example", Enabled: true},
							{Index: 1, Parent: 0, Role: "ControlType.Edit", Name: "Address", Value: "sample", Enabled: true, Focused: true, Actions: []string{"set_value", "focus"}, Bounds: desktop.Bounds{X: 20, Y: 30, Width: 100, Height: 20}},
						}
					}
					var screenshot *desktop.Image
					if req.Params.IncludeScreenshot == nil || *req.Params.IncludeScreenshot {
						screenshot = &desktop.Image{Data: data.Bytes(), MIMEType: "image/png", Width: 2, Height: 2, CaptureMethod: "print_window"}
					}
					result = desktop.State{Elements: elements, Mode: req.Params.Mode, ObservationMethod: "uia.control_view", SnapshotID: "snapshot", Window: *req.Params.Window, Image: screenshot}
				}
				if req.Params.Mode == "auto" && req.Method == "click" {
					result = map[string]any{"ok": true, "mode": "auto", "operation_method": "foreground_input.send_input", "foreground_fallback": true}
				}
				if req.Method == "set_value" {
					_ = encoder.Encode(desktop.Response{ID: req.ID, ErrorCode: desktop.BackgroundNotSupported, Error: "background_not_supported: element is read-only"})
					continue
				}
				wire, _ := json.Marshal(result)
				if encoder.Encode(desktop.Response{ID: req.ID, OK: true, Result: wire}) != nil {
					return
				}
			}
		}()
		return a, nil
	}})
	t.Cleanup(service.Close)
	return service
}
func TestMCPImagesAreNotDuplicatedAsBase64Text(t *testing.T) {
	svc := testService(t)
	server := mcp.NewServer(&mcp.Implementation{Name: "desktop-test"}, nil)
	RegisterMCP(server, svc, "actor")
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	ctx := context.Background()
	ss, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "desktop-client"}, nil)
	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	opened, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "desktop_session_open", Arguments: map[string]any{"target_id": "target"}})
	if err != nil || opened.IsError {
		t.Fatalf("open: %v %+v", err, opened)
	}
	b := resultJSON(t, opened)
	var out Output
	_ = json.Unmarshal(b, &out)
	state, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "desktop_get_window_state", Arguments: map[string]any{"session_id": out.SessionID, "params": map[string]any{"window": map[string]any{"id": "10", "app": "20"}}}})
	if err != nil || state.IsError {
		t.Fatalf("observe: %v %+v", err, state)
	}
	images := 0
	for _, c := range state.Content {
		if _, ok := c.(*mcp.ImageContent); ok {
			images++
		}
		if text, ok := c.(*mcp.TextContent); ok && strings.Contains(text.Text, `"data"`) {
			t.Fatal("screenshot duplicated in text")
		}
	}
	if images != 1 {
		t.Fatalf("expected one image, got %d", images)
	}
}

func TestMCPBackgroundSchemaErrorsAndCaptureMetadata(t *testing.T) {
	svc := testService(t)
	server := mcp.NewServer(&mcp.Implementation{Name: "desktop-test"}, nil)
	RegisterMCP(server, svc, "actor")
	st, ct := mcp.NewInMemoryTransports()
	ctx := context.Background()
	ss, err := server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "desktop-client"}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	tools, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools.Tools {
		if tool.Name == "desktop_session_open" || tool.Name == "desktop_session_close" {
			continue
		}
		wire, _ := json.Marshal(tool.InputSchema)
		if !strings.Contains(string(wire), `"enum":["","default","auto","background"]`) {
			t.Fatalf("%s schema lacks mode enum: %s", tool.Name, wire)
		}
		if !strings.Contains(tool.Description, desktop.BackgroundNotSupported) {
			t.Fatalf("%s description lacks fail-closed contract", tool.Name)
		}
	}
	opened, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "desktop_session_open", Arguments: map[string]any{"target_id": "target"}})
	if err != nil || opened.IsError {
		t.Fatalf("open: %v %+v", err, opened)
	}
	wire := resultJSON(t, opened)
	var out Output
	_ = json.Unmarshal(wire, &out)
	call := func(method string, params map[string]any) *mcp.CallToolResult {
		t.Helper()
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "desktop_" + method, Arguments: map[string]any{"session_id": out.SessionID, "params": params}})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	window := map[string]any{"id": "10", "app": "20"}
	state := call("get_window_state", map[string]any{"mode": "background", "window": window})
	if state.IsError {
		t.Fatalf("observe: %+v", state)
	}
	wire = resultJSON(t, state)
	if !strings.Contains(string(wire), `"capture_method":"print_window"`) || !strings.Contains(string(wire), `"observation_method":"uia.control_view"`) || !strings.Contains(string(wire), `"mode":"background"`) {
		t.Fatalf("native method metadata lost: %s", wire)
	}
	for _, method := range []string{"drag", "set_value"} {
		params := map[string]any{"mode": "background", "window": window, "snapshot_id": "snapshot", "element_index": 0, "text": "sample"}
		if method == "drag" {
			params = map[string]any{"mode": "background", "window": window, "snapshot_id": "snapshot", "x": 0, "y": 0, "to_x": 1, "to_y": 1}
		}
		res := call(method, params)
		wire = resultJSON(t, res)
		var failure Output
		_ = json.Unmarshal(wire, &failure)
		if !res.IsError || failure.ErrorCode != desktop.BackgroundNotSupported || !strings.HasPrefix(failure.Error, desktop.BackgroundNotSupported+":") {
			t.Fatalf("%s coded refusal lost: %+v %s", method, res, wire)
		}
	}
	autoState := call("get_window_state", map[string]any{"mode": "auto", "window": window})
	if autoState.IsError {
		t.Fatal("auto observe failed")
	}
	autoResult := call("click", map[string]any{"mode": "auto", "window": window, "snapshot_id": "snapshot", "element_index": 0})
	wire = resultJSON(t, autoResult)
	if autoResult.IsError || !strings.Contains(string(wire), `"foreground_fallback":true`) || !strings.Contains(string(wire), `"operation_method":"foreground_input.send_input"`) {
		t.Fatalf("auto foreground fallback metadata lost: %s", wire)
	}
	for _, mode := range []any{"unknown", true, 5} {
		if !call("status", map[string]any{"mode": mode}).IsError {
			t.Fatalf("invalid mode %v accepted", mode)
		}
	}
}
