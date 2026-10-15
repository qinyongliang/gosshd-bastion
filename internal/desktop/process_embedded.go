package desktop

import (
	"bufio"
	"context"
	"errors"
	"net"

	androidhelper "github.com/qinyongliang/gosshd-bastion/internal/desktop/native/android"
)

// startEmbeddedProcess adapts the isolated Android backend to the same private
// RPC stream used by subprocess helpers. It is host-testable without a device.
func startEmbeddedProcess(ctx context.Context, cfg Config, helper *androidhelper.Helper) (*process, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if cfg.HelperPath != "" {
		return nil, errors.New("Android uses the embedded backend; desktop-helper must be empty")
	}
	client, server := net.Pipe()
	helperCtx, cancel := context.WithCancel(ctx)
	finished := make(chan struct{})
	stop := context.AfterFunc(helperCtx, func() { client.Close(); server.Close() })
	go func() {
		defer close(finished)
		defer server.Close()
		_ = androidhelper.Serve(helperCtx, server, server, helper)
	}()
	scan := bufio.NewScanner(client)
	scan.Buffer(make([]byte, 65536), MaxMessage)
	p := &process{in: client, out: scan}
	p.closeNative = func() { cancel(); client.Close(); server.Close(); <-finished; stop() }
	return p, nil
}
