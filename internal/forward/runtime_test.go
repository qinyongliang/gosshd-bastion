package forward

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPProxyAndAccessLogs(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Forwarded-For") != "127.0.0.1" {
			t.Errorf("untrusted forwarded header: %s", r.Header.Get("X-Forwarded-For"))
		}
		w.WriteHeader(201)
		fmt.Fprint(w, r.URL.RequestURI())
	}))
	defer upstream.Close()
	host, port, _ := net.SplitHostPort(upstream.Listener.Addr().String())
	var p int
	fmt.Sscan(port, &p)
	logs, err := OpenLogs(t.TempDir(), uuid.NewString(), 7)
	if err != nil {
		t.Fatal(err)
	}
	defer logs.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	c := &HTTPConfig{Servers: []ServerBlock{{ID: "site", Hosts: []string{"test.local"}, Locations: []Location{{ID: "api", Path: "/api/", Scheme: "http", Host: host, Port: p, StripPrefix: true}}}}}
	runtime, err := Start(context.Background(), listener, c, nil, func(ctx context.Context, id string) (net.Conn, error) {
		if id != "api" {
			t.Fatal(id)
		}
		return (&net.Dialer{}).DialContext(ctx, "tcp", upstream.Listener.Addr().String())
	}, logs, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	req, _ := http.NewRequest("GET", "http://"+listener.Addr().String()+"/api/hello?q=a%2Fb", nil)
	req.Host = "test.local"
	req.Header.Set("X-Forwarded-For", "evil")
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 201 || string(body) != "/hello?q=a%2Fb" {
		t.Fatalf("%d %s", response.StatusCode, body)
	}
	runtime.Close()
	if err := logs.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	events, _ := logs.Query(context.Background(), LogFilter{})
	if len(events) != 1 || events[0].Status != 201 || events[0].RouteID != "api" || events[0].Path != "/api/hello?q=a%2Fb" {
		t.Fatalf("%+v", events)
	}
}
