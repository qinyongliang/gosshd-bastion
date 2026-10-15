package android

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestAndroidRunnerProcess(t *testing.T) {
	mode := os.Getenv("GOSSHD_ANDROID_RUNNER_FIXTURE")
	if mode == "" {
		return
	}
	switch mode {
	case "overflow":
		fmt.Print(strings.Repeat("x", 1024))
	case "hang":
		time.Sleep(time.Hour)
	case "error":
		fmt.Fprintln(os.Stderr, "private UI text must not leave stderr")
		os.Exit(2)
	}
	os.Exit(0)
}
func TestAndroidLocalRunnerBoundsCancellationAndDiagnostics(t *testing.T) {
	for _, mode := range []string{"overflow", "hang", "error"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("GOSSHD_ANDROID_RUNNER_FIXTURE", mode)
			timeout := 2 * time.Second
			if mode == "hang" {
				timeout = 100 * time.Millisecond
			}
			started := time.Now()
			_, err := (LocalRunner{Timeout: timeout, MaxOutput: 32}).Run(context.Background(), os.Args[0], "-test.run=^TestAndroidRunnerProcess$")
			if err == nil {
				t.Fatal("runner failure ignored")
			}
			if strings.Contains(err.Error(), "private UI text") {
				t.Fatal("native diagnostic data escaped")
			}
			if mode == "hang" && time.Since(started) > time.Second {
				t.Fatal("runner did not cancel")
			}
		})
	}
}
