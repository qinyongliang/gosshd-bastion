package desktop

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func TestBackgroundValidation(t *testing.T) {
	index := 1
	window := &Window{ID: "10", App: "20"}
	base := Params{Mode: "background", Window: window, SnapshotID: "observed", ElementIndex: &index}
	for _, tc := range []struct {
		method string
		change func(*Params)
		denied bool
	}{
		{"set_value", nil, false}, {"perform_action", func(p *Params) { p.Action = "invoke" }, false},
		{"perform_action", func(p *Params) { p.Action = "toggle" }, false}, {"perform_action", func(p *Params) { p.Action = "expand" }, false},
		{"perform_action", func(p *Params) { p.Action = "collapse" }, false}, {"perform_action", func(p *Params) { p.Action = "select" }, false},
		{"perform_action", func(p *Params) { p.Action = "secondary" }, false}, {"scroll", nil, false}, {"click", nil, false},
		{"get_window_state", nil, false}, {"screenshot", nil, false},
		{"activate_window", nil, true}, {"type_text", nil, true}, {"press_key", func(p *Params) { p.Key = "Ctrl+a" }, true},
		{"drag", nil, true}, {"launch_app", func(p *Params) { p.App = "/example.app" }, true}, {"request_permissions", nil, true},
		{"perform_action", func(p *Params) { p.Action = "focus" }, true},
		{"click", func(p *Params) { p.ElementIndex = nil }, true}, {"click", func(p *Params) { p.Button = "right" }, true},
		{"click", func(p *Params) { p.Button = "middle" }, true}, {"click", func(p *Params) { p.ClickCount = 2 }, true},
		{"scroll", func(p *Params) { p.ElementIndex = nil }, true}, {"screenshot", func(p *Params) { p.Window = nil }, true},
	} {
		p := base
		if tc.change != nil {
			tc.change(&p)
		}
		err := Validate(Request{Method: tc.method, Params: p})
		if tc.denied {
			var failure *ProtocolError
			if !errors.As(err, &failure) || failure.Code != BackgroundNotSupported || !strings.Contains(err.Error(), BackgroundNotSupported) {
				t.Errorf("%s: expected coded refusal, got %v", tc.method, err)
			}
		} else if err != nil {
			t.Errorf("%s: %v", tc.method, err)
		}
	}
	for _, method := range []string{"status", "list_apps", "list_windows", "list_displays"} {
		if err := Validate(Request{Method: method, Params: Params{Mode: "background"}}); err != nil {
			t.Error(err)
		}
	}
}

func TestModeValidationAndCompatibility(t *testing.T) {
	window := &Window{ID: "10", App: "20"}
	for _, mode := range []string{"", "default", "auto"} {
		for _, method := range []string{"click", "drag", "activate_window", "type_text", "press_key", "scroll"} {
			if err := Validate(Request{Method: method, Params: Params{Mode: mode, Window: window, SnapshotID: "s", Key: "a"}}); err != nil {
				t.Errorf("%s/%s: %v", mode, method, err)
			}
		}
	}
	for _, mode := range []string{"foreground", "Background", "strict", " default "} {
		if Validate(Request{Method: "status", Params: Params{Mode: mode}}) == nil {
			t.Errorf("invalid mode %q accepted", mode)
		}
	}
	var p Params
	if json.Unmarshal([]byte(`{"mode":true}`), &p) == nil {
		t.Fatal("non-string mode accepted")
	}
	if err := Validate(Request{Method: "set_value", Params: Params{Mode: "background", Window: window, SnapshotID: "s"}}); err == nil {
		t.Fatal("missing semantic index accepted")
	}
}

// The independent service must reject legacy peers before forwarding a strict request.
func TestBackgroundServiceCapabilityAndNativeErrors(t *testing.T) {
	for _, supported := range []bool{false, true} {
		t.Run(fmt.Sprint(supported), func(t *testing.T) {
			svc, forwarded := backgroundTestService(t, supported)
			info, _, err := svc.Open(context.Background(), "actor", "target")
			if err != nil {
				t.Fatal(err)
			}
			<-forwarded // opening status
			window := &Window{ID: "10", App: "20"}
			if _, err = svc.Call(context.Background(), "actor", info.ID, Request{Method: "get_window_state", Params: Params{Window: window}}); err != nil {
				t.Fatal(err)
			}
			<-forwarded
			index := 0
			res, err := svc.Call(context.Background(), "actor", info.ID, Request{Method: "set_value", Params: Params{Mode: "background", Window: window, SnapshotID: "observed", ElementIndex: &index, Text: "sample"}})
			var failure *ProtocolError
			if !errors.As(err, &failure) || failure.Code != BackgroundNotSupported {
				t.Fatalf("coded error missing: %+v %v", res, err)
			}
			if supported {
				if (<-forwarded).Params.Mode != "background" || res.ErrorCode != BackgroundNotSupported {
					t.Fatal("native error/mode lost")
				}
				// The failed native action consumed the snapshot.
				if _, err = svc.Call(context.Background(), "actor", info.ID, Request{Method: "click", Params: Params{Window: window, SnapshotID: "observed"}}); err == nil || !strings.Contains(err.Error(), "stale") {
					t.Fatal("failed native action did not consume snapshot")
				}
			} else {
				select {
				case req := <-forwarded:
					t.Fatalf("legacy peer received strict request: %+v", req)
				default:
				}
			}
			// Auto does not hard-block a peer lacking strict capability.
			if _, err = svc.Call(context.Background(), "actor", info.ID, Request{Method: "status", Params: Params{Mode: "auto"}}); err != nil {
				t.Fatal(err)
			}
			if (<-forwarded).Params.Mode != "auto" {
				t.Fatal("auto mode did not reach peer")
			}
			// Coded refusals do not break the session.
			if _, err = svc.Call(context.Background(), "actor", info.ID, Request{Method: "status"}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func backgroundTestService(t *testing.T, supported bool) (*Service, chan Request) {
	t.Helper()
	forwarded := make(chan Request, 8)
	svc := NewService(Adapter{Authorize: func(context.Context, string, string, bool) error { return nil }, Dial: func(context.Context, string) (io.ReadWriteCloser, error) {
		a, b := net.Pipe()
		go func() {
			defer b.Close()
			encoder := json.NewEncoder(b)
			_ = encoder.Encode(Response{ID: "init", OK: true})
			scan := bufio.NewScanner(b)
			for scan.Scan() {
				var req Request
				if json.Unmarshal(scan.Bytes(), &req) != nil {
					return
				}
				forwarded <- req
				var result any = map[string]any{"background_supported": supported}
				if req.Method == "set_value" {
					_ = encoder.Encode(errorResponse(req.ID, backgroundUnsupported("element is read-only")))
					continue
				}
				if req.Method == "get_window_state" {
					result = State{SnapshotID: "observed", Window: *req.Params.Window}
				}
				wire, _ := json.Marshal(result)
				if encoder.Encode(Response{ID: req.ID, OK: true, Result: wire}) != nil {
					return
				}
			}
		}()
		return a, nil
	}})
	t.Cleanup(svc.Close)
	return svc, forwarded
}

func TestLegacyNativeHelperNeverReceivesBackground(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	helper, err := startProcess(ctx, Config{}, helperFactory)
	if err != nil {
		t.Fatal(err)
	}
	defer helper.close()
	res, err := helper.call(Request{ID: "strict", Method: "status", Params: Params{Mode: "background"}})
	if err != nil || res.OK || res.ErrorCode != BackgroundNotSupported {
		t.Fatalf("legacy helper accepted background: %+v %v", res, err)
	}
	res, err = helper.call(Request{ID: "auto", Method: "status", Params: Params{Mode: "auto"}})
	if err != nil || !res.OK {
		t.Fatal("auto mode hard-blocked a legacy helper")
	}
	res, err = helper.call(Request{ID: "compatible", Method: "status"})
	if err != nil || !res.OK {
		t.Fatal("legacy default path broken")
	}
}

// Shared native cases exercise each platform boundary without touching GUI apps.
func testNativeBackgroundRefusals(t *testing.T, helper *process) {
	t.Helper()
	var err error
	for _, tc := range []struct {
		method string
		params map[string]any
		code   string
	}{
		{"activate_window", nil, BackgroundNotSupported}, {"launch_app", nil, BackgroundNotSupported},
		{"type_text", nil, BackgroundNotSupported}, {"press_key", nil, BackgroundNotSupported}, {"drag", nil, BackgroundNotSupported}, {"request_permissions", nil, BackgroundNotSupported},
		{"perform_action", map[string]any{"action": "focus"}, BackgroundNotSupported},
		{"click", nil, BackgroundNotSupported}, {"click", map[string]any{"element_index": 1, "button": "right"}, BackgroundNotSupported},
		{"click", map[string]any{"element_index": 1, "button": "middle"}, BackgroundNotSupported}, {"click", map[string]any{"element_index": 1, "click_count": 2}, BackgroundNotSupported},
		{"scroll", nil, BackgroundNotSupported}, {"screenshot", nil, BackgroundNotSupported},
		{"status", map[string]any{"mode": "invalid"}, ""}, {"status", map[string]any{"mode": true}, ""},
	} {
		params := map[string]any{"mode": "background"}
		for k, v := range tc.params {
			params[k] = v
		}
		// Bypass Go validation/negotiation to test the native trust boundary itself.
		wire, _ := json.Marshal(map[string]any{"id": "native-refusal", "method": tc.method, "params": params})
		if _, err = helper.in.Write(append(wire, '\n')); err != nil {
			t.Fatal(err)
		}
		if !helper.out.Scan() {
			t.Fatal("native helper exited")
		}
		var res Response
		if err = json.Unmarshal(helper.out.Bytes(), &res); err != nil {
			t.Fatal(err)
		}
		if res.OK || res.ID != "native-refusal" || res.ErrorCode != tc.code {
			t.Fatalf("%s: %+v", tc.method, res)
		}
		if tc.code != "" && !strings.HasPrefix(res.Error, tc.code+":") {
			t.Fatalf("%s: error prefix missing", tc.method)
		}
	}
	for _, mode := range []string{"", "default", "auto", "background"} {
		res, err := helper.call(Request{ID: "status", Method: "status", Params: Params{Mode: mode}})
		if err != nil || !res.OK || !supportsBackground(res.Result) {
			t.Fatalf("mode %s: %+v %v", mode, res, err)
		}
	}
}

func TestBridgeBackgroundValidationErrorCodes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a, b := net.Pipe()
	done := make(chan error, 1)
	go func() { done <- ServeWithFactory(ctx, b, Config{Enabled: true}, helperFactory) }()
	defer func() { a.Close(); cancel(); <-done }()
	scan := bufio.NewScanner(a)
	if !scan.Scan() {
		t.Fatal("bridge did not initialize")
	}
	encoder := json.NewEncoder(a)
	if err := encoder.Encode(Request{ID: "denied", Method: "click", Params: Params{Mode: "background", Window: &Window{ID: "10", App: "20"}, SnapshotID: "s"}}); err != nil {
		t.Fatal(err)
	}
	if !scan.Scan() {
		t.Fatal("bridge exited on validation error")
	}
	var res Response
	if err := json.Unmarshal(scan.Bytes(), &res); err != nil || res.OK || res.ErrorCode != BackgroundNotSupported {
		t.Fatalf("bridge lost error code: %+v %v", res, err)
	}
	if err := encoder.Encode(Request{ID: "compatible", Method: "status", Params: Params{Mode: "auto"}}); err != nil {
		t.Fatal(err)
	}
	if !scan.Scan() {
		t.Fatal("bridge did not preserve legacy auto compatibility")
	}
	if err := json.Unmarshal(scan.Bytes(), &res); err != nil || !res.OK {
		t.Fatalf("bridge default/auto path broken: %+v %v", res, err)
	}
}
