package desktop

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestHelperProcess(t *testing.T) {
	if os.Getenv("GOSSHD_DESKTOP_TEST_HELPER") != "1" {
		return
	}
	scan := bufio.NewScanner(os.Stdin)
	scan.Buffer(make([]byte, 65536), MaxMessage)
	for scan.Scan() {
		var req Request
		_ = json.Unmarshal(scan.Bytes(), &req)
		if req.Params.Text == "hang" {
			time.Sleep(time.Hour)
		}
		var result any = map[string]any{"ok": true}
		switch req.Method {
		case "status":
			result = map[string]any{"platform": "test"}
		case "get_window_state":
			var imageData bytes.Buffer
			_ = png.Encode(&imageData, image.NewRGBA(image.Rect(0, 0, 10, 10)))
			result = State{SnapshotID: "snapshot-" + req.ID, Window: *req.Params.Window, Bounds: Bounds{Width: 100, Height: 100}, Image: &Image{MIMEType: "image/png", Data: imageData.Bytes(), Width: 10, Height: 10}}
		}
		b, _ := json.Marshal(result)
		_ = writeResponse(os.Stdout, Response{ID: req.ID, OK: true, Result: b})
	}
	os.Exit(0)
}
func helperFactory(ctx context.Context, _ Config) (*exec.Cmd, func(), error) {
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestHelperProcess$")
	cmd.Env = append(os.Environ(), "GOSSHD_DESKTOP_TEST_HELPER=1")
	return cmd, func() {}, nil
}
func testService(t *testing.T) (*Service, *bool, *[]Event) {
	t.Helper()
	allowed := true
	events := []Event{}
	var mu sync.Mutex
	var workers sync.WaitGroup
	svc := NewService(Adapter{Authorize: func(_ context.Context, actor, target string, control bool) error {
		if !allowed {
			return errors.New("revoked")
		}
		if actor != "actor" || target != "target" {
			return errors.New("denied")
		}
		return nil
	}, Dial: func(ctx context.Context, _ string) (io.ReadWriteCloser, error) {
		a, b := net.Pipe()
		go func() { _ = ServeWithFactory(context.Background(), b, Config{Enabled: true}, helperFactory) }()
		return a, nil
	}, Audit: func(_ context.Context, e Event) error {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, e)
		return nil
	}})
	t.Cleanup(func() { svc.Close(); workers.Wait() })
	return svc, &allowed, &events
}
func TestServiceIsolationFreshnessRevocation(t *testing.T) {
	svc, allowed, events := testService(t)
	ctx := context.Background()
	info, _, err := svc.Open(ctx, "actor", "target")
	if err != nil {
		t.Fatal(err)
	}
	w := Window{ID: "10", App: "20"}
	res, err := svc.Call(ctx, "actor", info.ID, Request{Method: "get_window_state", Params: Params{Window: &w}})
	if err != nil {
		t.Fatal(err)
	}
	var state State
	_ = json.Unmarshal(res.Result, &state)
	action := Request{Method: "click", Params: Params{Window: &w, SnapshotID: state.SnapshotID}}
	if _, err = svc.Call(ctx, "other", info.ID, action); err == nil {
		t.Fatal("another actor used the session")
	}
	if _, err = svc.Call(ctx, "actor", info.ID, action); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Call(ctx, "actor", info.ID, action); err == nil {
		t.Fatal("consumed snapshot reused")
	}
	*allowed = false
	if _, err = svc.Call(ctx, "actor", info.ID, Request{Method: "status"}); err == nil {
		t.Fatal("revoked access used")
	}
	svc.mu.Lock()
	n := len(svc.sessions)
	svc.mu.Unlock()
	if n != 0 {
		t.Fatal("revoked session retained")
	}
	if len(*events) < 3 {
		t.Fatal("missing audit metadata")
	}
}
func TestServiceTimeoutClosesSession(t *testing.T) {
	svc, _, _ := testService(t)
	info, _, err := svc.Open(context.Background(), "actor", "target")
	if err != nil {
		t.Fatal(err)
	}
	svc.Timeout = 50 * time.Millisecond
	start := time.Now()
	_, err = svc.Call(context.Background(), "actor", info.ID, Request{Method: "status", Params: Params{Text: "hang"}})
	if err == nil || time.Since(start) > time.Second {
		t.Fatalf("timeout not enforced: %v", err)
	}
	svc.mu.Lock()
	n := len(svc.sessions)
	svc.mu.Unlock()
	if n != 0 {
		t.Fatal("timed-out session retained")
	}
}
func TestServiceLimitsAndIdleCleanup(t *testing.T) {
	svc, _, _ := testService(t)
	svc.IdleTimeout = 5 * time.Second
	for i := 0; i < 4; i++ {
		if _, _, err := svc.Open(context.Background(), "actor", "target"); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := svc.Open(context.Background(), "actor", "target"); err == nil {
		t.Fatal("session limit ignored")
	}
	svc.mu.Lock()
	for _, item := range svc.sessions {
		item.timer.Reset(50 * time.Millisecond)
	}
	svc.mu.Unlock()
	time.Sleep(250 * time.Millisecond)
	svc.mu.Lock()
	n := len(svc.sessions)
	svc.mu.Unlock()
	if n != 0 {
		t.Fatalf("idle sessions not cleaned: %d", n)
	}
}
func TestDisabledAgentDoesNotStartHelper(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	called := false
	go func() {
		_ = ServeWithFactory(context.Background(), b, Config{}, func(context.Context, Config) (*exec.Cmd, func(), error) { called = true; return nil, nil, nil })
	}()
	scan := bufio.NewScanner(a)
	if !scan.Scan() || !strings.Contains(scan.Text(), "disabled") {
		t.Fatal("expected disabled handshake")
	}
	if called {
		t.Fatal("helper started when disabled")
	}
}
func TestValidateDesktopRequest(t *testing.T) {
	w := Window{ID: "1", App: "2"}
	for _, req := range []Request{{Method: "exec"}, {Method: "click", Params: Params{Window: &w}}, {Method: "set_value", Params: Params{Window: &w, SnapshotID: "s"}}, {Method: "get_window_state", Params: Params{Window: &w, MaxElements: 1025}}, {Method: "perform_action", Params: Params{Window: &w, SnapshotID: "s", ElementIndex: new(int), Action: "run_shell"}}} {
		if Validate(req) == nil {
			t.Fatalf("invalid request accepted: %+v", req)
		}
	}
}
