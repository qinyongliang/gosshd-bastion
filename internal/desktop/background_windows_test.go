//go:build windows

package desktop

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Compiles and talks to an owned console helper, but never enumerates, captures,
// activates, observes or manipulates real windows. Native guards run first.
func TestWindowsBackgroundNativeRefusals(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	helper, err := startProcess(ctx, Config{}, platformCommand)
	if err != nil {
		t.Fatal(err)
	}
	defer helper.close()
	testNativeBackgroundRefusals(t, helper)
}

func TestWindowsBackgroundNativeCapturePolicy(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd, cleanup, err := platformCommand(ctx, Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	cmd.Args = append(cmd.Args, "--self-test")
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "background capture policy self-test passed") {
		t.Fatalf("native capture policy: %v %s", err, out)
	}
}
