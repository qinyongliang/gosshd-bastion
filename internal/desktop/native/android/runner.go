package android

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

const commandTimeout = 20 * time.Second
const maxOutputBytes = 24 << 20

// LocalRunner executes argv directly; it never invokes a shell or evaluates UI text.
type LocalRunner struct {
	Timeout   time.Duration
	MaxOutput int
}

func (r LocalRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = commandTimeout
	}
	limit := r.MaxOutput
	if limit <= 0 {
		limit = maxOutputBytes
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	prepareCommand(cmd)
	cmd.WaitDelay = time.Second
	stdout := &limitWriter{limit: limit}
	cmd.Stdout = stdout
	cmd.Stderr = &limitWriter{limit: 4096}
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// Device diagnostics can contain screen data or typed text. Return only the
		// fixed command name and exit status, never stdout/stderr.
		return nil, fmt.Errorf("Android tool %s failed: %w", name, err)
	}
	if stdout.overflow {
		return nil, errors.New("Android command output exceeds limit")
	}
	return stdout.buffer.Bytes(), nil
}

type limitWriter struct {
	buffer   bytes.Buffer
	limit    int
	overflow bool
}

func (w *limitWriter) Write(data []byte) (int, error) {
	n := min(len(data), max(0, w.limit-w.buffer.Len()))
	w.buffer.Write(data[:n])
	if n < len(data) {
		w.overflow = true
	}
	return len(data), nil
}
