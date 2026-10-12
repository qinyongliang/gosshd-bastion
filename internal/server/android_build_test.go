package server

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAndroidBuildScriptUsesNDKAndPublishesBothABIs(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Android bash build script mock runs on Linux")
	}
	source, err := os.ReadFile(filepath.Join("..", "..", "scripts", "build-agent-android.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "both architectures", true: "fail fast"}[fail], func(t *testing.T) {
			root := t.TempDir()
			bin := filepath.Join(root, "mockbin")
			ndk := filepath.Join(root, "NDK with spaces")
			compiler := filepath.Join(ndk, "toolchains/llvm/prebuilt/linux-x86_64/bin/clang")
			for _, dir := range []string{bin, filepath.Join(root, "scripts"), filepath.Dir(compiler)} {
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			script := filepath.Join(root, "scripts/build-agent-android.sh")
			if err := os.WriteFile(script, source, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(compiler, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			mock := `#!/bin/sh
set -eu
printf '%s|%s|%s|%s|%s|%s\n' "$GOOS" "$GOARCH" "${GOARM:-}" "$CGO_ENABLED" "$CC" "$*" >> "$MOCK_BUILD_LOG"
if [ "${MOCK_BUILD_FAIL:-}" = "$GOARCH" ]; then exit 7; fi
while [ "$#" -gt 0 ]; do if [ "$1" = -o ]; then printf '%s' "$GOARCH" > "$2"; exit 0; fi; shift; done
exit 8
`
			if err := os.WriteFile(filepath.Join(bin, "go"), []byte(mock), 0o700); err != nil {
				t.Fatal(err)
			}
			log := filepath.Join(root, "build.log")
			cmd := exec.Command("bash", script, "test-version")
			cmd.Env = append(os.Environ(), "ANDROID_NDK_HOME="+ndk, "PATH="+bin+":"+os.Getenv("PATH"), "MOCK_BUILD_LOG="+log)
			if fail {
				cmd.Env = append(cmd.Env, "MOCK_BUILD_FAIL=arm64")
			}
			out, err := cmd.CombinedOutput()
			if fail {
				if err == nil {
					t.Fatalf("build accepted failed compiler: %s", out)
				}
			} else if err != nil {
				t.Fatalf("build: %v %s", err, out)
			}
			body, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			text := string(body)
			for _, want := range []string{"android|arm|7|1|", "--target=armv7a-linux-androideabi24", "android|arm64||1|", "--target=aarch64-linux-android24", "-buildmode=pie", "-checklinkname=0", "max-page-size=16384", "main.version=test-version"} {
				if !strings.Contains(text, want) {
					t.Errorf("missing Android build option %q: %s", want, text)
				}
			}
			for arch, want := range map[string]string{"armv7": "arm", "arm64": "arm64"} {
				asset := filepath.Join(root, "dist", "gosshd-agent-test-version-android-"+arch)
				body, err := os.ReadFile(asset)
				if fail && arch == "arm64" {
					if !os.IsNotExist(err) {
						t.Fatalf("failed build published asset: %s", asset)
					}
					continue
				}
				if err != nil || string(body) != want {
					t.Fatalf("asset %s: %q %v", asset, body, err)
				}
				versioned := filepath.Join(root, "build/agent/test-version/android", arch, "gosshd-agent")
				if _, err := os.Stat(versioned); err != nil {
					t.Fatalf("missing version-scoped local asset: %v", err)
				}
			}
		})
	}
}

func TestAndroidReleaseIntegration(t *testing.T) {
	for path, fragment := range map[string]string{
		"../../build-release.ps1":               "scripts/build-agent-android.ps1",
		"../../.github/workflows/release.yml":   "bash scripts/build-agent-android.sh",
		"../../scripts/build-agent-android.ps1": "armv7a-linux-androideabi24",
	} {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(body), fragment) {
			t.Errorf("Android release integration missing from %s", path)
		}
	}
}
