package forward

import (
	"context"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPWebSocketUpgradeAndAccessLog(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		kind, payload, err := conn.ReadMessage()
		if err == nil {
			conn.WriteMessage(kind, payload)
		}
	}))
	defer up.Close()
	logs, err := OpenLogs(t.TempDir(), uuid.NewString(), 7)
	if err != nil {
		t.Fatal(err)
	}
	defer logs.Close()
	listener, _ := net.Listen("tcp", "127.0.0.1:0")
	config := &HTTPConfig{Servers: []ServerBlock{{ID: "site", Hosts: []string{"_"}, Locations: []Location{{ID: "socket", Path: "/socket", Scheme: "http", Host: "127.0.0.1", Port: up.Listener.Addr().(*net.TCPAddr).Port}}}}}
	runtime, err := Start(context.Background(), listener, config, nil, func(ctx context.Context, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", up.Listener.Addr().String())
	}, logs, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	conn, response, err := websocket.DefaultDialer.Dial("ws://"+listener.Addr().String()+"/socket?q=1", nil)
	if err != nil {
		t.Fatalf("upgrade %+v %v", response, err)
	}
	conn.SetReadDeadline(time.Now().Add(time.Second))
	if err := conn.WriteMessage(websocket.TextMessage, []byte("edge socket")); err != nil {
		t.Fatal(err)
	}
	_, payload, err := conn.ReadMessage()
	conn.Close()
	if err != nil || string(payload) != "edge socket" {
		t.Fatalf("%q %v", payload, err)
	}
	runtime.Close()
	logs.Flush(context.Background())
	events, _ := logs.Query(context.Background(), LogFilter{})
	if len(events) != 1 || events[0].Status != 101 || events[0].Path != "/socket?q=1" || events[0].RouteID != "socket" || events[0].Up == 0 || events[0].Down == 0 {
		t.Fatal(events)
	}
}
