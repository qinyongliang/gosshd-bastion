//go:build darwin

package desktop

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Uses an explicitly selected test build; it does not rebuild, re-sign, replace
// an authorized app, request permissions or interact with target windows.
func TestMacBackgroundNativeRefusals(t *testing.T) {
	path := os.Getenv("GOSSHD_DESKTOP_HELPER_TEST_PATH")
	if path == "" {
		t.Skip("explicit macOS helper test build required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	helper, err := startProcess(ctx, Config{HelperPath: path}, platformCommand)
	if err != nil {
		t.Fatal(err)
	}
	defer helper.close()
	testNativeBackgroundRefusals(t, helper)
}
func TestMacBackgroundNativePolicySelfTest(t *testing.T) {
	path := os.Getenv("GOSSHD_DESKTOP_HELPER_TEST_PATH")
	if path == "" {
		t.Skip("explicit macOS helper test build required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--self-test").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "macOS desktop helper self-test passed") {
		t.Fatalf("native policy self-test: %v %s", err, out)
	}
}
