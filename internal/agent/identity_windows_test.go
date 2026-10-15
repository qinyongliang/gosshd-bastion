//go:build windows

package agent

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sys/windows/registry"
)

func TestWindowsIdentityChild(t *testing.T) {
	mode := os.Getenv("GOSSHD_IDENTITY_TEST_MODE")
	if mode == "" {
		return
	}
	if mode == "boot" {
		raw, err := readSystemBootID()
		if err != nil {
			t.Fatal(err)
		}
		fmt.Println("BOOT:" + agentBootID("synthetic-agent", raw))
		return
	}
	release, err := acquireAgentInstanceGuard(os.Getenv("GOSSHD_IDENTITY_TEST_KEY"), "")
	if mode == "contender" {
		if !errors.Is(err, ErrAgentAlreadyRunning) {
			if release != nil {
				release()
			}
			t.Fatalf("duplicate guard: %v", err)
		}
		fmt.Println("BUSY")
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	fmt.Println("READY")
	_, _ = bufio.NewReader(os.Stdin).ReadByte()
}

func identityChildCommand(t *testing.T, mode, key string) *exec.Cmd {
	t.Helper()
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, path, "-test.run=^TestWindowsIdentityChild$")
	cmd.Env = append(os.Environ(), "GOSSHD_IDENTITY_TEST_MODE="+mode, "GOSSHD_IDENTITY_TEST_KEY="+key)
	cmd.SysProcAttr = hiddenWindowSysProcAttr()
	return cmd
}

func TestWindowsBootIdentitySurvivesProcessRestart(t *testing.T) {
	var boot string
	for n := 0; n < 2; n++ {
		output, err := identityChildCommand(t, "boot", "").CombinedOutput()
		if err != nil {
			t.Fatalf("boot child: %v %s", err, output)
		}
		var got string
		for _, line := range strings.Split(string(output), "\n") {
			if strings.HasPrefix(line, "BOOT:") {
				got = strings.TrimSpace(strings.TrimPrefix(line, "BOOT:"))
			}
		}
		if got == "" {
			t.Fatalf("missing child identity: %s", output)
		}
		if n == 0 {
			boot = got
		} else if got != boot {
			t.Fatal("OS boot identity changed between separate Agent processes")
		}
	}
}

func TestWindowsInstanceGuardAcrossProcessesAndCrash(t *testing.T) {
	key := uuid.NewString()
	release, err := acquireAgentInstanceGuard(key, "")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	out, err := identityChildCommand(t, "contender", key).CombinedOutput()
	if err != nil || !strings.Contains(string(out), "BUSY") {
		t.Fatalf("cross-process duplicate guard: %v %s", err, out)
	}
	release()
	holder := identityChildCommand(t, "holder", key)
	input, err := holder.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	output, err := holder.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := holder.Start(); err != nil {
		t.Fatal(err)
	}
	defer holder.Process.Kill()
	line, err := bufio.NewReader(output).ReadString('\n')
	if err != nil || strings.TrimSpace(line) != "READY" {
		t.Fatalf("holder did not acquire guard: %q %v", line, err)
	}
	if err := holder.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = holder.Wait()
	afterCrash, err := acquireAgentInstanceGuard(key, "")
	if err != nil {
		t.Fatalf("kernel did not release crashed process handle: %v", err)
	}
	afterCrash()
}

func TestWindowsVolatileBootCacheSurvivesHandlesAndInvalidatesOldBoot(t *testing.T) {
	path := `SOFTWARE\GosshdBootTest-` + uuid.NewString()
	t.Cleanup(func() { registry.DeleteKey(registry.CURRENT_USER, path) })
	first, err := windowsBootSessionID(registry.CURRENT_USER, path, 100)
	if err != nil {
		t.Fatal(err)
	}
	again, err := windowsBootSessionID(registry.CURRENT_USER, path, 100)
	if err != nil || again != first {
		t.Fatalf("closing all Agent handles lost the boot ID: %v", err)
	}
	next, err := windowsBootSessionID(registry.CURRENT_USER, path, 200)
	if err != nil || next == first {
		t.Fatalf("old boot cache was reused after boot changed: %v", err)
	}
	if _, err := windowsBootSessionID(registry.CURRENT_USER, path, 0); err == nil {
		t.Fatal("missing kernel stamp must not initialize constant identity")
	}
}

func TestWindowsVolatileBootCacheConcurrentInitialization(t *testing.T) {
	path := `SOFTWARE\GosshdBootTest-` + uuid.NewString()
	t.Cleanup(func() { registry.DeleteKey(registry.CURRENT_USER, path) })
	var wg sync.WaitGroup
	results := make(chan string, 16)
	for n := 0; n < 16; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, err := windowsBootSessionID(registry.CURRENT_USER, path, 123)
			if err != nil {
				t.Errorf("initialize: %v", err)
				return
			}
			results <- id
		}()
	}
	wg.Wait()
	close(results)
	var first string
	for id := range results {
		if first == "" {
			first = id
		} else if first != id {
			t.Fatal("concurrent processes would receive different boot IDs")
		}
	}
	if first == "" {
		t.Fatal("no boot identity created")
	}
}
