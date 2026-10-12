package server

import (
	"bufio"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
)

func TestDownloadAgentAndroidArchitectureAliases(t *testing.T) {
	for _, tc := range []struct{ arch, assetArch string }{
		{"arm", "armv7"}, {"armeabi-v7a", "armv7"}, {"armv7", "armv7"},
		{"arm64-v8a", "arm64"}, {"aarch64", "arm64"}, {"arm64", "arm64"},
	} {
		t.Run(tc.arch, func(t *testing.T) {
			root := t.TempDir()
			body := []byte("android-" + tc.assetArch)
			path := filepath.Join(root, "dev", "android", tc.assetArch, "gosshd-agent")
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, body, 0o700); err != nil {
				t.Fatal(err)
			}
			app := NewApp(Config{Version: "dev", AgentPath: root})
			for _, checksum := range []bool{false, true} {
				url := "/download/agent/android/" + tc.arch
				want := string(body)
				if checksum {
					url += ".sha256"
					want = fmt.Sprintf("%x\n", sha256.Sum256(body))
				}
				rec := httptest.NewRecorder()
				app.downloadAgent(rec, httptest.NewRequest(http.MethodGet, url, nil))
				if rec.Code != http.StatusOK || rec.Body.String() != want {
					t.Fatalf("download %s: status=%d body=%q want=%q", url, rec.Code, rec.Body.String(), want)
				}
			}
		})
	}
}

func TestAgentWSAndroidUpdateUsesPublishedArchitecture(t *testing.T) {
	for _, tc := range []struct{ goarch, assetArch string }{{"arm", "armv7"}, {"arm64", "arm64"}} {
		t.Run(tc.goarch, func(t *testing.T) {
			root := t.TempDir()
			body := []byte("android-update-" + tc.assetArch)
			path := filepath.Join(root, "v1.2.3", "android", tc.assetArch, "gosshd-agent")
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, body, 0o700); err != nil {
				t.Fatal(err)
			}
			app := NewApp(Config{Version: "v1.2.3", PublicHost: "relay.example.com", AgentPath: root, DatabasePath: filepath.Join(root, "db")})
			token := createAgentEnrollmentForTest(t, context.Background(), app, "android")
			t.Cleanup(func() { _ = app.Close() })
			mux := http.NewServeMux()
			app.routes(mux)
			srv := httptest.NewServer(mux)
			t.Cleanup(srv.Close)
			conn := dialAgentWS(t, srv.URL)
			defer conn.Close()
			if err := protocol.WriteJSONLine(conn, protocol.AgentHello{
				ID: "11111111-1111-4111-8111-111111111111", EnrollmentToken: token,
				Version: "v1.2.2", GOOS: "android", GOARCH: tc.goarch,
			}); err != nil {
				t.Fatal(err)
			}
			resp, err := protocol.ReadJSONLine[protocol.StreamResponse](bufio.NewReader(conn))
			if err != nil {
				t.Fatal(err)
			}
			wantURL := "http://relay.example.com/download/agent/android/" + tc.assetArch
			wantSHA := fmt.Sprintf("%x", sha256.Sum256(body))
			if !resp.OK || resp.AgentDownloadURL != wantURL || resp.AgentDownloadSHA256 != wantSHA {
				t.Fatalf("update offer = %+v; want %s sha=%s", resp, wantURL, wantSHA)
			}
		})
	}
}

func TestAgentReleaseURLAndroidABIAliases(t *testing.T) {
	app := NewApp(Config{Version: "v1.2.3"})
	for _, tc := range []struct{ goos, arch, want string }{
		{"android", "arm", "android-armv7"}, {"android", "armeabi-v7a", "android-armv7"},
		{"android", "arm64-v8a", "android-arm64"}, {"linux", "arm", "linux-arm"},
	} {
		if got := app.agentReleaseURL(tc.goos, tc.arch, "gosshd-agent"); !strings.HasSuffix(got, "gosshd-agent-v1.2.3-"+tc.want) {
			t.Errorf("release URL for %s/%s: %s", tc.goos, tc.arch, got)
		}
	}
}
