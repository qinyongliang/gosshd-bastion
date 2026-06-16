package server

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"github.com/qinyongliang/gosshd/internal/protocol"
	"github.com/qinyongliang/gosshd/internal/relay"

	"github.com/gorilla/websocket"
)

func TestAgentWSReturnsVersionAndAgentDownloadURL(t *testing.T) {
	app := NewApp(Config{Version: "v1.2.3", PublicHost: "relay.example.com"})
	mux := http.NewServeMux()
	app.routes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + protocol.WebSocketPath
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	conn := relay.NewWSConn(ws)

	if err := protocol.WriteJSONLine(conn, protocol.AgentHello{
		ID:      "11111111-1111-4111-8111-111111111111",
		Version: "v1.2.2",
		GOOS:    "linux",
		GOARCH:  "amd64",
	}); err != nil {
		t.Fatal(err)
	}
	resp, err := protocol.ReadJSONLine[protocol.StreamResponse](bufio.NewReader(conn))
	if err != nil {
		t.Fatal(err)
	}
	if !resp.OK || resp.ServerVersion != "v1.2.3" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if got, want := resp.AgentDownloadURL, "http://relay.example.com/download/agent/linux/amd64"; got != want {
		t.Fatalf("download URL mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestAgentWSDefaultsMissingPlatformToServerPlatform(t *testing.T) {
	app := NewApp(Config{Version: "v1.2.3"})
	mux := http.NewServeMux()
	app.routes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + protocol.WebSocketPath
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	conn := relay.NewWSConn(ws)

	if err := protocol.WriteJSONLine(conn, protocol.AgentHello{
		ID:      "11111111-1111-4111-8111-111111111111",
		Version: "v1.2.2",
	}); err != nil {
		t.Fatal(err)
	}
	resp, err := protocol.ReadJSONLine[protocol.StreamResponse](bufio.NewReader(conn))
	if err != nil {
		t.Fatal(err)
	}
	wantSuffix := "/download/agent/" + runtime.GOOS + "/" + runtime.GOARCH
	if !strings.HasSuffix(resp.AgentDownloadURL, wantSuffix) {
		t.Fatalf("download URL %q does not end with %q", resp.AgentDownloadURL, wantSuffix)
	}
}
