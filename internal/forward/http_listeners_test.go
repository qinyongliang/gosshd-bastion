package forward

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

func TestHTTPListenerValidation(t *testing.T) {
	for _, test := range []struct {
		name    string
		entries []HTTPListener
		bad     bool
	}{
		{"missing listeners rejected", nil, true}, {"default host", []HTTPListener{{Port: 8080}}, false},
		{"multiple IPs", []HTTPListener{{Host: "127.0.0.1", Port: 8080}, {Host: "192.0.2.1", Port: 8080}, {Host: "::1", Port: 8080}}, false},
		{"duplicate", []HTTPListener{{Host: "127.0.0.1", Port: 8080}, {Host: "::ffff:127.0.0.1", Port: 8080}}, true},
		{"wildcard overlap", []HTTPListener{{Host: "0.0.0.0", Port: 8080}, {Host: "127.0.0.1", Port: 8080}}, true},
		{"IPv4 wildcard and IPv6 loopback", []HTTPListener{{Host: "0.0.0.0", Port: 8080}, {Host: "::1", Port: 8080}}, true},
		{"IPv6 wildcard overlap", []HTTPListener{{Host: "::", Port: 8080}, {Host: "127.0.0.1", Port: 8080}}, true},
		{"hostname", []HTTPListener{{Host: "listener.example", Port: 8080}}, true},
		{"zero port", []HTTPListener{{Port: 0}}, true}, {"high port", []HTTPListener{{Port: 65536}}, true},
		{"too many", make([]HTTPListener, 65), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := &HTTPConfig{Listeners: test.entries}
			got, err := HTTPListeners(config)
			if (err != nil) != test.bad {
				t.Fatalf("%+v %v", got, err)
			}
			if !test.bad && test.name == "default host" && (got[0].Host != "0.0.0.0" || config.Listeners[0].Host != "") {
				t.Fatal("defaulting must not mutate caller configuration")
			}
		})
	}
}

func listenerTestConfig(up *httptest.Server) *HTTPConfig {
	host, port, _ := net.SplitHostPort(up.Listener.Addr().String())
	n, _ := strconv.Atoi(port)
	return &HTTPConfig{Listeners: []HTTPListener{{Host: "127.0.0.1", Port: 8080}, {Host: "127.0.0.1", Port: 8081}}, Servers: []ServerBlock{{ID: "site", Hosts: []string{"_"}, Locations: []Location{{ID: "backend", Path: "/", Scheme: "http", Host: host, Port: n}}}}}
}

func TestHTTPMultipleListenersWebSocketAndSharedAccounting(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !websocket.IsWebSocketUpgrade(r) {
			fmt.Fprint(w, r.URL.RequestURI())
			return
		}
		c, err := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		for {
			kind, data, err := c.ReadMessage()
			if err != nil {
				return
			}
			if c.WriteMessage(kind, data) != nil {
				return
			}
		}
	}))
	defer up.Close()
	logs, err := OpenLogs(t.TempDir(), uuid.NewString(), 7)
	if err != nil {
		t.Fatal(err)
	}
	defer logs.Close()
	runtime, err := StartHTTP(context.Background(), listenerTestConfig(up), func(string) (net.Listener, error) { return net.Listen("tcp", "127.0.0.1:0") }, func(ctx context.Context, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", up.Listener.Addr().String())
	}, logs, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	if len(runtime.transports) != 1 {
		t.Fatal("listeners duplicated upstream pools")
	}
	client := &http.Client{Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()
	var sockets []*websocket.Conn
	for i, l := range runtime.listeners {
		address := l.Addr().String()
		if !strings.Contains(runtime.State().ListenAddress, address) {
			t.Fatal(runtime.State())
		}
		response, err := client.Get("http://" + address + fmt.Sprintf("/http?listener=%d", i))
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if string(body) != fmt.Sprintf("/http?listener=%d", i) {
			t.Fatal(string(body))
		}
		c, res, err := websocket.DefaultDialer.Dial("ws://"+address+fmt.Sprintf("/socket?listener=%d", i), nil)
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != 101 {
			t.Fatal(res.StatusCode)
		}
		defer c.Close()
		sockets = append(sockets, c)
		for _, kind := range []int{websocket.TextMessage, websocket.BinaryMessage} {
			payload := []byte{0, 1, 2, byte(kind)}
			if kind == websocket.TextMessage {
				payload = []byte("edge websocket")
			}
			if err = c.WriteMessage(kind, payload); err != nil {
				t.Fatal(err)
			}
			c.SetReadDeadline(time.Now().Add(5 * time.Second))
			gotKind, got, err := c.ReadMessage()
			if err != nil || gotKind != kind || string(got) != string(payload) {
				t.Fatalf("kind=%d payload=%q err=%v", gotKind, got, err)
			}
		}
	}
	if runtime.State().Requests != 4 || runtime.State().Active != 2 {
		t.Fatal(runtime.State())
	}
	// Stopping must also close hijacked WebSocket connections on every listener.
	if err = runtime.Close(); err != nil {
		t.Fatal(err)
	}
	for _, c := range sockets {
		c.SetReadDeadline(time.Now().Add(time.Second))
		if _, _, err = c.ReadMessage(); err == nil {
			t.Fatal("stopped websocket still open")
		}
	}
	if err = logs.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	events, err := logs.Query(context.Background(), LogFilter{})
	if err != nil || len(events) != 4 {
		t.Fatalf("%+v %v", events, err)
	}
	upgraded := 0
	for _, event := range events {
		if event.Status == 101 {
			upgraded++
			if event.RouteID != "backend" || event.Up == 0 || event.Down == 0 || !strings.HasPrefix(event.Path, "/socket?") {
				t.Fatal(event)
			}
		}
	}
	if upgraded != 2 {
		t.Fatal(events)
	}
}

func TestHTTPListenerBindFailureRollsBack(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer up.Close()
	var first net.Listener
	calls := 0
	runtime, err := StartHTTP(context.Background(), listenerTestConfig(up), func(string) (net.Listener, error) {
		calls++
		if calls == 2 {
			return nil, errors.New("address already in use")
		}
		var err error
		first, err = net.Listen("tcp", "127.0.0.1:0")
		return first, err
	}, nil, nil, nil)
	if err == nil || runtime != nil || first == nil {
		t.Fatalf("%v %v", runtime, err)
	}
	retry, err := net.Listen("tcp", first.Addr().String())
	if err != nil {
		t.Fatal("first listener leaked", err)
	}
	retry.Close()
}

func TestHTTPListenerFailureStopsOtherEndpoints(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer up.Close()
	runtime, err := StartHTTP(context.Background(), listenerTestConfig(up), func(string) (net.Listener, error) { return net.Listen("tcp", "127.0.0.1:0") }, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	runtime.listeners[0].Close()
	deadline := time.Now().Add(3 * time.Second)
	for runtime.Alive() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if runtime.Alive() {
		t.Fatal("partially closed listener set still marked healthy")
	}
	runtime.Close()
	rebound, err := net.Listen("tcp", runtime.listeners[1].Addr().String())
	if err != nil {
		t.Fatal("remaining listener leaked", err)
	}
	rebound.Close()
}

func TestHTTPMultipleListenersWebSocketTLSUpstream(t *testing.T) {
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		kind, payload, err := c.ReadMessage()
		if err == nil {
			c.WriteMessage(kind, payload)
		}
	}))
	defer up.Close()
	config := listenerTestConfig(up)
	config.Servers[0].Locations[0].Scheme = "https"
	logs, err := OpenLogs(t.TempDir(), uuid.NewString(), 7)
	if err != nil {
		t.Fatal(err)
	}
	defer logs.Close()
	runtime, err := StartHTTP(context.Background(), config, func(string) (net.Listener, error) { return net.Listen("tcp", "127.0.0.1:0") }, func(ctx context.Context, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", up.Listener.Addr().String())
	}, logs, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	roots := x509.NewCertPool()
	roots.AddCert(up.Certificate())
	// Trust only this fixture certificate; never disable certificate verification.
	runtime.transports[0].TLSClientConfig = &tls.Config{RootCAs: roots}
	for _, listener := range runtime.listeners {
		c, res, err := websocket.DefaultDialer.Dial("ws://"+listener.Addr().String()+"/socket", nil)
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != 101 {
			t.Fatal(res.StatusCode)
		}
		c.SetReadDeadline(time.Now().Add(5 * time.Second))
		c.WriteMessage(websocket.TextMessage, []byte("TLS socket"))
		_, got, err := c.ReadMessage()
		c.Close()
		if err != nil || string(got) != "TLS socket" {
			t.Fatalf("%q %v", got, err)
		}
	}
}
