package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAndroidInstallerPersistsExplicitDesktopBackend(t *testing.T) {
	for _, backend := range []string{"shell", "accessibility"} {
		t.Run(backend, func(t *testing.T) {
			f := newAndroidInstallFixture(t, backend == "accessibility", "arm64-v8a")
			tokenFile := filepath.Join(f.root, "pairing 'quoted token")
			if err := os.WriteFile(tokenFile, []byte(strings.Repeat("a", 43)), 0600); err != nil {
				t.Fatal(err)
			}
			extra := []string{"GOSSHD_DESKTOP_ENABLE=1", "GOSSHD_ANDROID_BACKEND=" + backend, "GOSSHD_ANDROID_BRIDGE_TOKEN_FILE=" + tokenFile}
			if out, err := f.run("run", extra...); err != nil {
				t.Fatalf("desktop installer: %v %s", err, out)
			}
			body, err := os.ReadFile(f.record)
			if err != nil {
				t.Fatal(err)
			}
			var record struct{ Args []string }
			if err = json.Unmarshal(body, &record); err != nil {
				t.Fatal(err)
			}
			args := strings.Join(record.Args, "\n")
			if !strings.Contains(args, "--desktop-enable\n--android-backend\n"+backend) {
				t.Fatalf("selected desktop mode missing: %v", record.Args)
			}
			if backend == "accessibility" && !strings.Contains(args, "--android-bridge-token-file\n"+tokenFile) {
				t.Fatal("pairing path was not preserved literally")
			}
			if strings.Contains(args, strings.Repeat("a", 43)) {
				t.Fatal("pairing token value leaked into process arguments")
			}
		})
	}
}
func TestAndroidInstallerRejectsUnknownDesktopBackend(t *testing.T) {
	f := newAndroidInstallFixture(t, true, "aarch64")
	if _, err := f.run("run", "GOSSHD_DESKTOP_ENABLE=1", "GOSSHD_ANDROID_BACKEND=arbitrary"); err == nil {
		t.Fatal("unknown desktop backend accepted")
	}
	if _, err := os.Stat(f.record); !os.IsNotExist(err) {
		t.Fatal("Agent ran after invalid backend selection")
	}
}
