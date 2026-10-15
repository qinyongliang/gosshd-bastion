package desktop

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	androidhelper "github.com/qinyongliang/gosshd-bastion/internal/desktop/native/android"
)

type embeddedRunner struct {
	started chan struct{}
	once    sync.Once
}

func (r *embeddedRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if name == "getprop" {
		r.once.Do(func() { close(r.started) })
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return nil, errors.New("denied")
}
func TestEmbeddedAndroidCancelBusyAndIdle(t *testing.T) {
	for _, busy := range []bool{false, true} {
		t.Run(map[bool]string{false: "idle", true: "busy"}[busy], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			r := &embeddedRunner{started: make(chan struct{})}
			helper := &androidhelper.Helper{Runner: r, UID: 10001, Lookup: func(n string) (string, error) { return n, nil }}
			p, err := startEmbeddedProcess(ctx, Config{}, helper)
			if err != nil {
				t.Fatal(err)
			}
			defer p.close()
			callsDone := make(chan struct{})
			if busy {
				go func() { defer close(callsDone); _, _ = p.call(Request{ID: "one", Method: "status"}) }()
				<-r.started
			}
			cancel()
			done := make(chan struct{})
			go func() { p.close(); close(done) }()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("Android helper survived cancellation")
			}
			if busy {
				select {
				case <-callsDone:
				case <-time.After(time.Second):
					t.Fatal("call survived cancellation")
				}
			}
		})
	}
}
func TestEmbeddedAndroidDisconnectCancelsCommand(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	runner := &embeddedRunner{started: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		done <- serveWithStarter(context.Background(), server, Config{Enabled: true}, func(ctx context.Context, cfg Config) (*process, error) {
			return startEmbeddedProcess(ctx, cfg, &androidhelper.Helper{Runner: runner, UID: 10001, Lookup: func(n string) (string, error) { return n, nil }})
		})
	}()
	scan := bufio.NewScanner(client)
	if !scan.Scan() {
		t.Fatal("no init")
	}
	var init Response
	if json.Unmarshal(scan.Bytes(), &init) != nil || !init.OK {
		t.Fatal("invalid init")
	}
	if err := json.NewEncoder(client).Encode(Request{ID: "busy", Method: "status"}); err != nil {
		t.Fatal(err)
	}
	<-runner.started
	client.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("disconnect left Android command running")
	}
}
func TestEmbeddedAndroidRejectsCustomHelper(t *testing.T) {
	if _, err := startEmbeddedProcess(context.Background(), Config{HelperPath: "/example/helper"}, nil); err == nil {
		t.Fatal("custom Android helper accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := startEmbeddedProcess(ctx, Config{}, nil); err == nil {
		t.Fatal("canceled helper started")
	}
}
