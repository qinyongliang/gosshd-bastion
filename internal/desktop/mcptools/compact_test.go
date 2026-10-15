package mcptools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/qinyongliang/gosshd-bastion/internal/desktop"
)

func resultJSON(t *testing.T, result *mcp.CallToolResult) []byte {
	t.Helper()
	for _, content := range result.Content {
		if text, ok := content.(*mcp.TextContent); ok {
			return []byte(text.Text)
		}
	}
	t.Fatal("missing JSON text content")
	return nil
}

func compactClient(t *testing.T) (*mcp.ClientSession, string) {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "compact-test"}, nil)
	RegisterMCP(server, testService(t), "actor")
	st, ct := mcp.NewInMemoryTransports()
	ss, err := server.Connect(context.Background(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ss.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "compact-client"}, nil)
	cs, err := client.Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	opened, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "desktop_session_open", Arguments: map[string]any{"target_id": "target"}})
	if err != nil || opened.IsError {
		t.Fatalf("open: %v %+v", err, opened)
	}
	if opened.StructuredContent != nil || strings.Count(string(resultJSON(t, opened)), `"session_id":`) != 1 {
		t.Fatal("session-open result duplicated")
	}
	var out Output
	if err := json.Unmarshal(resultJSON(t, opened), &out); err != nil {
		t.Fatal(err)
	}
	if out.SessionID == "" {
		t.Fatal("missing session ID")
	}
	return cs, out.SessionID
}

func TestCompactDesktopPayloads(t *testing.T) {
	cs, id := compactClient(t)
	for _, method := range []string{"status", "get_window_state"} {
		args := map[string]any{"session_id": id}
		if method == "get_window_state" {
			args["params"] = map[string]any{"window": map[string]any{"id": "10", "app": "20"}, "include_text": true, "include_screenshot": false}
		}
		res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "desktop_" + method, Arguments: args})
		if err != nil || res.IsError {
			t.Fatalf("%s: %v %+v", method, err, res)
		}
		if res.StructuredContent != nil {
			t.Fatalf("%s duplicated JSON in structuredContent", method)
		}
		text := string(resultJSON(t, res))
		if len(res.Content) != 1 {
			t.Fatal("text-only operation returned unnecessary content/image blocks")
		}
		if strings.Contains(text, `"elements":`) {
			t.Fatal("raw element geometry leaked into default output")
		}
		if method == "get_window_state" && strings.Index(text, `"snapshot_id":`) > strings.Index(text, `"accessibility":`) {
			t.Fatal("long accessibility text precedes snapshot identity")
		}
		if method == "get_window_state" && !strings.Contains(text, `"accessibility":`) {
			t.Fatalf("missing compact accessibility: %s", text)
		}
	}
}

func TestDesktopMethodSpecificSchemas(t *testing.T) {
	cs, _ := compactClient(t)
	list, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range list.Tools {
		if tool.Name != "desktop_launch_app" && tool.Name != "desktop_click" && tool.Name != "desktop_status" {
			continue
		}
		wire, _ := json.Marshal(tool.InputSchema)
		var schema struct {
			Required   []string
			Properties map[string]struct {
				Required   []string
				Properties map[string]any
			}
		}
		if err := json.Unmarshal(wire, &schema); err != nil {
			t.Fatal(err)
		}
		params := schema.Properties["params"]
		has := func(values []string, key string) bool {
			for _, value := range values {
				if value == key {
					return true
				}
			}
			return false
		}
		if tool.Name == "desktop_launch_app" && (!has(schema.Required, "params") || !has(params.Required, "app")) {
			t.Errorf("launch_app must require params.app: %s", wire)
		}
		if tool.Name == "desktop_click" && (!has(params.Required, "window") || !has(params.Required, "snapshot_id")) {
			t.Errorf("click must require observed window and snapshot: %s", wire)
		}
		if tool.Name == "desktop_status" && len(params.Properties) > 1 {
			t.Errorf("status exposes unrelated action parameters: %s", wire)
		}
	}
}

func TestDesktopMalformedCallsDoNotConsumeObservation(t *testing.T) {
	cs, id := compactClient(t)
	window := map[string]any{"id": "10", "app": "20"}
	call := func(method string, params map[string]any) *mcp.CallToolResult {
		t.Helper()
		res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "desktop_" + method, Arguments: map[string]any{"session_id": id, "params": params}})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	if call("get_window_state", map[string]any{"window": window, "include_text": true}).IsError {
		t.Fatal("observe failed")
	}
	invalid := []struct {
		method string
		params map[string]any
	}{
		{"launch_app", map[string]any{"mode": "auto"}},
		{"launch_app", map[string]any{"path": "example.exe"}},
		{"click", map[string]any{"snapshot_id": "snapshot", "element_index": 1}},
		{"click", map[string]any{"window": window, "snapshot_id": "snapshot"}},
		{"click", map[string]any{"window": window, "snapshot_id": "snapshot", "element_index": nil}},
		{"click", map[string]any{"window": window, "snapshot_id": "snapshot", "element_index": 1, "x": 0, "y": 0}},
		{"press_key", map[string]any{"window": window, "snapshot_id": "snapshot"}},
		{"set_value", map[string]any{"window": window, "snapshot_id": "snapshot", "element_index": 1}},
		{"perform_action", map[string]any{"window": window, "snapshot_id": "snapshot", "element_index": 1, "action": "click"}},
		{"scroll", map[string]any{"window": window, "snapshot_id": "snapshot", "element_index": 1}},
		{"drag", map[string]any{"window": window, "snapshot_id": "snapshot", "to_x": 1, "to_y": 1}},
	}
	for _, tc := range invalid {
		if !call(tc.method, tc.params).IsError {
			t.Errorf("invalid %s accepted: %v", tc.method, tc.params)
		}
	}
	valid := map[string]any{"window": window, "snapshot_id": "snapshot", "element_index": 1, "mode": "auto"}
	first := call("click", valid)
	if first.IsError {
		t.Fatalf("schema errors consumed the observation: %s", resultJSON(t, first))
	}
	reused := call("click", valid)
	if !reused.IsError {
		t.Fatal("consumed snapshot accepted")
	}
	if reused.StructuredContent != nil {
		t.Fatal("error payload duplicated")
	}
	var failure Output
	if err := json.Unmarshal(resultJSON(t, reused), &failure); err != nil {
		t.Fatal(err)
	}
	if failure.NextAction != "get_window_state" || !strings.Contains(failure.Recovery, "Do not blindly retry") {
		t.Fatalf("missing safe recovery: %+v", failure)
	}
	if call("get_window_state", map[string]any{"window": window}).IsError {
		t.Fatal("reobserve failed")
	}
	if call("click", map[string]any{"window": window, "snapshot_id": "snapshot", "x": 0, "y": 0}).IsError {
		t.Fatal("explicit origin coordinates rejected")
	}
	if call("get_window_state", map[string]any{"window": window}).IsError {
		t.Fatal("reobserve before native failure failed")
	}
	nativeFailure := call("set_value", map[string]any{"window": window, "snapshot_id": "snapshot", "element_index": 1, "text": "sample", "mode": "background"})
	if !nativeFailure.IsError {
		t.Fatal("native refusal unexpectedly succeeded")
	}
	if !call("click", valid).IsError {
		t.Fatal("failed native action did not consume snapshot")
	}

}

func TestDesktopPayloadAndSchemaSizeReduction(t *testing.T) {
	cs, id := compactClient(t)
	call := func(details bool) []byte {
		t.Helper()
		res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "desktop_get_window_state", Arguments: map[string]any{"session_id": id, "params": map[string]any{"window": map[string]any{"id": "10", "app": "20"}, "include_text": true, "include_element_details": details}}})
		if err != nil || res.IsError {
			t.Fatalf("observe: %v %+v", err, res)
		}
		return resultJSON(t, res)
	}
	compact := call(false)
	raw := call(true)
	var out struct {
		Result desktop.State `json:"result"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Result.Elements) != 2 {
		t.Fatal("diagnostic opt-in lost raw records")
	}
	if out.Result.Tree != "" {
		t.Fatal("diagnostic records redundantly returned as a tree")
	}
	out.Result.Tree = desktop.RenderTree(out.Result.Elements, false)
	legacy, _ := json.Marshal(Output{SessionID: id, Result: out.Result})
	if len(compact)*100 >= len(legacy)*75 {
		t.Fatalf("insufficient payload reduction: compact=%d legacy=%d", len(compact), len(legacy))
	}
	// Reconstruct the old shared schema: every method advertised every Params field.
	old, err := jsonschema.For[struct {
		SessionID string         `json:"session_id"`
		Params    desktop.Params `json:"params,omitempty"`
	}](nil)
	if err != nil {
		t.Fatal(err)
	}
	old.Properties["params"].Properties["mode"].Enum = []any{"", "default", "auto", "background"}
	oldWire, _ := json.Marshal(old)
	tools, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	total, count := 0, 0
	for _, tool := range tools.Tools {
		if tool.Name == "desktop_session_open" || tool.Name == "desktop_session_close" {
			continue
		}
		wire, _ := json.Marshal(tool.InputSchema)
		total += len(wire)
		count++
	}
	baseline := len(oldWire) * count
	if total*100 >= baseline*75 {
		t.Fatalf("insufficient schema reduction: new=%d legacy=%d", total, baseline)
	}
	t.Logf("observation JSON: %d -> %d bytes (%d%% reduction); input schemas: %d -> %d bytes (%d%% reduction)", len(legacy), len(compact), 100-len(compact)*100/len(legacy), baseline, total, 100-total*100/baseline)
}
