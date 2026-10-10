package server

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/qinyongliang/gosshd-bastion/internal/store"
)

type delayedAuditReader struct {
	io.Reader
	waited bool
}

func (r *delayedAuditReader) Read(p []byte) (int, error) {
	if !r.waited {
		r.waited = true
		time.Sleep(40 * time.Millisecond)
	}
	return r.Reader.Read(p)
}

func TestHTTPUploadAuditIncludesRequestBodyTransfer(t *testing.T) {
	srv, _, app := newAPITestServer(t)
	defer srv.Close()
	ctx := context.Background()
	user, err := app.store.Repository().GetUserByEmail(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	org, err := app.store.Repository().GetPersonalOrganizationForUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	attachTunnelAgent(t, app, "audit-upload-agent")
	target := uploadAgentTarget(t, app, user, org, "audit-upload-agent")
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, err := writer.CreateFormFile("file", "file.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("upload contents")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/targets/"+target.ID+"/files/upload?path="+url.QueryEscape(t.TempDir()), &delayedAuditReader{Reader: &body})
	r.SetPathValue("id", target.ID)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()
	app.handleTargetFileUpload(w, r, user)
	if w.Code != http.StatusCreated {
		t.Fatalf("upload failed: %d %s", w.Code, w.Body.String())
	}
	page, err := app.audit.Repository().ListCommandAuditLogs(ctx, store.AuditLogFilter{TargetID: target.ID, RequestType: store.RequestSFTP})
	if err != nil || len(page.Logs) != 1 {
		t.Fatalf("upload audit missing: %+v %v", page, err)
	}
	log := page.Logs[0]
	if log.EndedAt == nil || log.EndedAt.Sub(log.StartedAt) < 40*time.Millisecond {
		t.Fatalf("HTTP upload audit omitted request body transfer: %+v", log)
	}
}

func TestAuditAPIPreservesSubsecondTimestamps(t *testing.T) {
	for _, offset := range []time.Duration{123 * time.Millisecond, 950 * time.Millisecond} {
		started := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC).Add(offset)
		ended := started.Add(125 * time.Millisecond)
		for source, log := range map[string]apiAuditLog{
			"stored":  apiAuditLogFromStore(store.CommandAuditLog{StartedAt: started, EndedAt: &ended}),
			"running": apiAuditLogFromRunning(runningAuditSnapshot{StartedAt: started, EndedAt: ended}),
		} {
			t.Run(source+"/"+offset.String(), func(t *testing.T) {
				start, err := time.Parse(time.RFC3339Nano, log.StartedAt)
				if err != nil || !start.Equal(started) {
					t.Fatalf("start precision lost: %q, want %s", log.StartedAt, started)
				}
				end, err := time.Parse(time.RFC3339Nano, log.EndedAt)
				if err != nil || !end.Equal(ended) || end.Sub(start) != 125*time.Millisecond {
					t.Fatalf("duration precision lost: %q - %q", log.EndedAt, log.StartedAt)
				}
			})
		}
	}
}
