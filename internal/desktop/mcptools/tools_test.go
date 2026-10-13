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
				var result any = map[string]bool{"ok": true}
				if req.Method == "get_window_state" {
					var data bytes.Buffer
					_ = png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 2, 2)))
					result = desktop.State{SnapshotID: "snapshot", Window: *req.Params.Window, Image: &desktop.Image{Data: data.Bytes(), MIMEType: "image/png", Width: 2, Height: 2}}
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
	b, _ := json.Marshal(opened.StructuredContent)
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
