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

func TestFileUploadFolderDirectoriesBatch(t *testing.T) {
	for _, ssh := range []bool{false, true} {
		t.Run(fmt.Sprintf("ssh=%t", ssh), func(t *testing.T) {
			srv, client, app := newAPITestServer(t)
			defer srv.Close()
			postJSON(t, client, srv.URL+"/api/auth/login", map[string]string{"email": "admin", "password": "admin-pass"}, http.StatusOK, nil)
			ctx := context.Background()
			user, _ := app.store.Repository().GetUserByEmail(ctx, "admin")
			org, _ := app.store.Repository().GetPersonalOrganizationForUser(ctx, user.ID)
			attachTunnelAgent(t, app, "folders-agent")
			target := uploadAgentTarget(t, app, user, org, "folders-agent")
			var connections atomic.Int64
			if ssh {
				address, closeServer := startTestSFTPServer(t, testSFTPModeSubsystem, func() { connections.Add(1) })
				defer closeServer()
				target = tunnelSSHTarget(t, app, user, org, "folders-ssh", address, target.ID)
			}
			root := t.TempDir()
			paths := []string{filepath.Join(root, "first", "nested"), filepath.Join(root, "first", "empty"), filepath.Join(root, "second")}
			for i := range paths {
				paths[i] = filepath.ToSlash(paths[i])
			}
			endpoint := srv.URL + "/api/targets/" + target.ID + "/files/mkdir"
			postJSON(t, client, endpoint, map[string]any{"paths": paths}, http.StatusCreated, nil)
			for _, path := range paths {
				info, err := os.Stat(path)
				if err != nil || !info.IsDir() {
					t.Fatalf("directory missing: %s %v", path, err)
				}
			}
			if ssh && connections.Load() != 1 {
				t.Fatalf("directory batch opened %d SSH connections", connections.Load())
			}
			logs, err := app.audit.Repository().ListCommandAuditLogs(ctx, store.AuditLogFilter{TargetID: target.ID, RequestType: store.RequestSFTP})
			if err != nil || len(logs.Logs) != len(paths) {
				t.Fatalf("missing directory audits: %+v %v", logs, err)
			}
			for _, log := range logs.Logs {
				if !strings.HasPrefix(log.Command, "sftp mkdir ") || log.ExitCode == nil || *log.ExitCode != 0 {
					t.Fatalf("incorrect audit: %+v", log)
				}
			}
			// Validate the entire batch before creating its first directory.
			invalid := filepath.Join(root, "must-not-exist")
			postJSON(t, client, endpoint, map[string]any{"paths": []string{invalid, "/"}}, http.StatusBadRequest, nil)
			if _, err := os.Stat(invalid); !os.IsNotExist(err) {
				t.Fatalf("invalid batch partially created paths: %v", err)
			}
			attachAllowSFTPPolicyForTargetAccess(t, app, org.ID, target.ID, false, true)
			postJSON(t, client, endpoint, map[string]any{"paths": []string{invalid}}, http.StatusForbidden, nil)
			if _, err := os.Stat(invalid); !os.IsNotExist(err) {
				t.Fatalf("download-only policy created a directory: %v", err)
			}
		})
	}
}
