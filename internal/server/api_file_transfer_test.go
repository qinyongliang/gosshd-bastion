package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/qinyongliang/gosshd-bastion/internal/store"
)

func TestFileTransferBatchCopyMove(t *testing.T) {
	for _, ssh := range []bool{false, true} {
		t.Run(fmt.Sprintf("ssh=%t", ssh), func(t *testing.T) {
			srv, client, app := newAPITestServer(t)
			defer srv.Close()
			postJSON(t, client, srv.URL+"/api/auth/login", map[string]string{"email": "admin", "password": "admin-pass"}, http.StatusOK, nil)
			ctx := context.Background()
			user, _ := app.store.Repository().GetUserByEmail(ctx, "admin")
			org, _ := app.store.Repository().GetPersonalOrganizationForUser(ctx, user.ID)
			attachTunnelAgent(t, app, "transfer-agent")
			target := uploadAgentTarget(t, app, user, org, "transfer-agent")
			var connections atomic.Int64
			if ssh {
				address, closeServer := startTestSFTPServer(t, testSFTPModeSubsystem, func() { connections.Add(1) })
				defer closeServer()
				target = tunnelSSHTarget(t, app, user, org, "transfer-ssh", address, target.ID)
			}
			root := filepath.ToSlash(t.TempDir())
			file := root + "/file "
			folder := root + "/folder"
			copied := root + "/copied "
			moved := root + "/moved"
			for _, dir := range []string{folder + "/nested/empty", copied, moved} {
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
			}
			for _, name := range []string{file, folder + "/nested/content.txt"} {
				if err := os.WriteFile(name, []byte("contents"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			endpoint := srv.URL + "/api/targets/" + target.ID + "/files/"
			var result struct {
				Transfers []targetFileTransfer `json:"transfers"`
			}
			postJSON(t, client, endpoint+"copy", map[string]any{"sources": []string{file, folder}, "destination": copied}, http.StatusOK, &result)
			if len(result.Transfers) != 2 || result.Transfers[0].Destination != copied+"/file " {
				t.Fatalf("incorrect destinations: %+v", result)
			}
			if ssh && connections.Load() != 1 {
				t.Fatalf("copy opened %d connections", connections.Load())
			}
			postJSON(t, client, endpoint+"move", map[string]any{"sources": []string{copied + "/file ", copied + "/folder"}, "destination": moved}, http.StatusOK, nil)
			if ssh && connections.Load() != 2 {
				t.Fatalf("move batch did not reuse a connection: %d", connections.Load())
			}
			for _, name := range []string{file, folder + "/nested/content.txt", moved + "/file ", moved + "/folder/nested/content.txt"} {
				data, err := os.ReadFile(name)
				if err != nil || string(data) != "contents" {
					t.Fatalf("content missing at %s: %q %v", name, data, err)
				}
			}
			if info, err := os.Stat(moved + "/folder/nested/empty"); err != nil || !info.IsDir() {
				t.Fatalf("empty directory missing: %v", err)
			}
			if _, err := os.Stat(copied + "/folder"); !os.IsNotExist(err) {
				t.Fatalf("move left its source: %v", err)
			}
			logs, err := app.audit.Repository().ListCommandAuditLogs(ctx, store.AuditLogFilter{TargetID: target.ID, RequestType: store.RequestSFTP})
			if err != nil || len(logs.Logs) != 4 {
				t.Fatalf("missing per-item audits: %+v %v", logs, err)
			}
			for _, log := range logs.Logs {
				if log.ExitCode == nil || *log.ExitCode != 0 {
					t.Fatalf("failed transfer audit: %+v", log)
				}
			}
			// Reject the entire invalid batch before copying its first item.
			for _, body := range []map[string]any{
				{"sources": []string{file, "/"}, "destination": copied},
				{"sources": []string{file, file}, "destination": copied},
				{"sources": []string{}, "destination": copied},
				{"sources": []string{file, folder}, "destination": file},
				{"sources": []string{file, folder}, "destination": folder},
				{"source": file, "destination": file},
			} {
				postJSON(t, client, endpoint+"copy", body, http.StatusBadRequest, nil)
			}
			if _, err := os.Stat(copied + "/file "); !os.IsNotExist(err) {
				t.Fatalf("invalid batch partially copied: %v", err)
			}
			// Legacy single-source requests still support a renamed destination.
			var single targetFileTransfer
			postJSON(t, client, endpoint+"copy", map[string]string{"source": file, "destination": copied + "/renamed"}, http.StatusOK, &single)
			if single.Source != file || single.Destination != copied+"/renamed" {
				t.Fatalf("incorrect single response: %+v", single)
			}
			postJSON(t, client, endpoint+"move", map[string]string{"source": copied + "/renamed", "destination": moved + "/renamed"}, http.StatusOK, nil)
			// Copy needs read and write; move needs write. Denial opens no connection.
			for _, policy := range []struct {
				action           string
				upload, download bool
			}{{"copy", true, false}, {"move", false, true}} {
				attachAllowSFTPPolicyForTargetAccess(t, app, org.ID, target.ID, policy.upload, policy.download)
				before := connections.Load()
				postJSON(t, client, endpoint+policy.action, map[string]any{"sources": []string{file, folder}, "destination": copied}, http.StatusForbidden, nil)
				if connections.Load() != before {
					t.Fatal("denied operation opened a connection")
				}
			}
		})
	}
}

func TestFileTransferPartialFailure(t *testing.T) {
	srv, client, app := newAPITestServer(t)
	defer srv.Close()
	postJSON(t, client, srv.URL+"/api/auth/login", map[string]string{"email": "admin", "password": "admin-pass"}, http.StatusOK, nil)
	ctx := context.Background()
	user, _ := app.store.Repository().GetUserByEmail(ctx, "admin")
	org, _ := app.store.Repository().GetPersonalOrganizationForUser(ctx, user.ID)
	attachTunnelAgent(t, app, "partial-transfer")
	target := uploadAgentTarget(t, app, user, org, "partial-transfer")
	root := filepath.ToSlash(t.TempDir())
	if err := os.Mkdir(root+"/out", 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"first", "last"} {
		if err := os.WriteFile(root+"/"+name, []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var response map[string]string
	postJSON(t, client, srv.URL+"/api/targets/"+target.ID+"/files/move", map[string]any{"sources": []string{root + "/first", root + "/missing", root + "/last"}, "destination": root + "/out"}, http.StatusBadGateway, &response)
	if !strings.Contains(response["error"], root+"/missing") {
		t.Fatalf("failed source not reported: %+v", response)
	}
	for _, name := range []string{"out/first", "last"} {
		if _, err := os.Stat(root + "/" + name); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(root + "/out/last"); !os.IsNotExist(err) {
		t.Fatalf("batch continued after failure: %v", err)
	}
	logs, err := app.audit.Repository().ListCommandAuditLogs(ctx, store.AuditLogFilter{TargetID: target.ID, RequestType: store.RequestSFTP})
	if err != nil || len(logs.Logs) != 2 {
		t.Fatalf("missing partial-operation audits: %+v %v", logs, err)
	}
	for _, log := range logs.Logs {
		want := 0
		if strings.Contains(log.Command, root+"/missing") {
			want = 255
		}
		if log.ExitCode == nil || *log.ExitCode != want {
			t.Fatalf("incorrect partial-operation audit: %+v", log)
		}
	}
}
