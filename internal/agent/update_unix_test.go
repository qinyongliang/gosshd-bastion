//go:build !windows

package agent

import "testing"

func TestStableExecutablePathStripsUpdateTempSuffix(t *testing.T) {
	got := stableExecutablePath("/usr/local/bin/gosshd-agent.new-3110354209")
	if want := "/usr/local/bin/gosshd-agent"; got != want {
		t.Fatalf("stable executable path mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestStableExecutablePathPreservesRegularPath(t *testing.T) {
	got := stableExecutablePath("/usr/local/bin/gosshd-agent")
	if want := "/usr/local/bin/gosshd-agent"; got != want {
		t.Fatalf("regular executable path mismatch:\n got: %s\nwant: %s", got, want)
	}
}
