package desktop

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func bridgeToken(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(p, []byte(strings.Repeat("a", 43)), 0600); err != nil {
		t.Fatal(err)
	}
	return p
}
func TestAndroidBackendSelection(t *testing.T) {
	for _, tc := range []struct {
		cfg  Config
		want string
		bad  bool
	}{
		{Config{}, "shell", false}, {Config{AndroidBackend: "shell", AndroidBridgeTokenFile: "file"}, "shell", false}, {Config{AndroidBridgeTokenFile: "file"}, "accessibility", false}, {Config{AndroidBackend: "accessibility"}, "", true}, {Config{AndroidBackend: "other"}, "", true}} {
		got, err := androidBackend(tc.cfg)
		if got != tc.want || (err != nil) != tc.bad {
			t.Fatalf("selection: %q %v", got, err)
		}
	}
}
func TestAndroidCompanionAuthenticationAndCancellation(t *testing.T) {
	for _, accepted := range []bool{false, true} {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan struct{})
		go func() {
			defer close(done)
			defer l.Close()
			c, err := l.Accept()
			if err != nil {
				return
			}
			defer c.Close()
			reader := bufio.NewReader(c)
			nonce := strings.Repeat("n", 43)
			json.NewEncoder(c).Encode(map[string]any{"protocol": 1, "nonce": nonce})
			line, _ := reader.ReadBytes('\n')
			var auth map[string]any
			json.Unmarshal(line, &auth)
			if auth["proof"] != androidBridgeProof(strings.Repeat("a", 43), "client:"+nonce) {
				return
			}
			json.NewEncoder(c).Encode(map[string]any{"ok": accepted, "proof": androidBridgeProof(strings.Repeat("a", 43), "server:"+nonce)})
			if accepted {
				line, _ = reader.ReadBytes('\n')
				var req Request
				json.Unmarshal(line, &req)
				json.NewEncoder(c).Encode(Response{ID: req.ID, OK: true, Result: json.RawMessage(`{"platform":"android","backend":"accessibility"}`)})
				reader.ReadByte()
			}
		}()
		ctx, cancel := context.WithCancel(context.Background())
		p, err := startAndroidBridgeAddress(ctx, Config{AndroidBridgeTokenFile: bridgeToken(t)}, l.Addr().String())
		if !accepted {
			if err == nil {
				t.Fatal("pairing rejection ignored")
			}
		} else {
			if err != nil {
				t.Fatal(err)
			}
			res, err := p.call(Request{ID: "status", Method: "status"})
			if err != nil || !res.OK {
				t.Fatalf("call: %+v %v", res, err)
			}
			cancel()
			p.close()
		}
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("bridge connection survived cancel")
		}
	}
}
func TestAndroidPairingTokenValidation(t *testing.T) {
	for _, value := range []string{"", strings.Repeat("a", 42), strings.Repeat("a", 42) + ";", strings.Repeat("a", 200)} {
		p := filepath.Join(t.TempDir(), "token")
		os.WriteFile(p, []byte(value), 0600)
		if _, err := readAndroidBridgeToken(p); err == nil {
			t.Fatal("invalid token accepted")
		}
	}
}

func TestAndroidCompanionRejectsFakeServerProof(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		c, err := l.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		json.NewEncoder(c).Encode(map[string]any{"protocol": 1, "nonce": strings.Repeat("n", 43)})
		bufio.NewReader(c).ReadBytes('\n')
		json.NewEncoder(c).Encode(map[string]any{"ok": true, "proof": strings.Repeat("x", 43)})
	}()
	if _, err := startAndroidBridgeAddress(context.Background(), Config{AndroidBridgeTokenFile: bridgeToken(t)}, l.Addr().String()); err == nil {
		t.Fatal("fake companion proof accepted")
	}
	<-done
}
