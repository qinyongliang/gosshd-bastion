package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
	"github.com/qinyongliang/gosshd-bastion/internal/store"
	"github.com/qinyongliang/gosshd-bastion/internal/tunnel"
	"github.com/qinyongliang/gosshd-bastion/internal/upload"
)

func uploadWS(t *testing.T, base string, client *http.Client, target, dir, name string, size int) *websocket.Conn {
	t.Helper()
	params := url.Values{"path": {dir}, "name": {name}, "size": {strconv.Itoa(size)}}
	u, _ := url.Parse(base)
	headers := http.Header{"Origin": {base}}
	for _, c := range client.Jar.Cookies(u) {
		headers.Add("Cookie", c.String())
	}
	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(base, "http")+"/api/targets/"+target+"/files/upload/ws?"+params.Encode(), headers)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	_ = ws.SetReadDeadline(time.Now().Add(15 * time.Second))
	var response map[string]any
	if err := ws.ReadJSON(&response); err != nil || response["type"] != "ready" {
		t.Fatalf("upload not ready: %+v %v", response, err)
	}
	return ws
}

func TestFileUploadRelayAndFailurePreserveOriginal(t *testing.T) {
	for _, mode := range []string{"complete", "checksum", "cancel", "forged completion", "empty", "duplicate"} {
		t.Run(mode, func(t *testing.T) {
			srv, client, app := newAPITestServer(t)
			defer srv.Close()
			postJSON(t, client, srv.URL+"/api/auth/login", map[string]string{"email": "admin", "password": "admin-pass"}, http.StatusOK, nil)
			user, _ := app.store.Repository().GetUserByEmail(context.Background(), "admin")
			org, _ := app.store.Repository().GetPersonalOrganizationForUser(context.Background(), user.ID)
			attachTunnelAgent(t, app, "upload-agent")
			target := tunnelAgentTarget(t, app, user, org, "upload-agent")
			dir := t.TempDir()
			dest := filepath.Join(dir, "file.bin")
			if err := os.WriteFile(dest, []byte("original"), 0644); err != nil {
				t.Fatal(err)
			}
			data := []byte("new\x00\xffcontents")
			if mode == "empty" {
				data = nil
			}
			ws := uploadWS(t, srv.URL, client, target.ID, dir, "file.bin", len(data))
			var seq uint64 = 1
			send := func(body []byte) {
				if err := ws.WriteMessage(websocket.BinaryMessage, (tunnel.Packet{Kind: tunnel.Data, Seq: seq, Body: body}).Bytes()); err != nil {
					t.Fatal(err)
				}
				seq++
			}
			if len(data) > 0 {
				record := upload.ChunkRecord(data)
				if mode == "checksum" {
					record[9] ^= 1
				}
				send(record)
				if mode == "duplicate" {
					if err := ws.WriteMessage(websocket.BinaryMessage, (tunnel.Packet{Kind: tunnel.Data, Seq: 1, Body: record}).Bytes()); err != nil {
						t.Fatal(err)
					}
				}
			}
			if mode == "cancel" {
				ws.Close()
			} else if mode == "forged completion" {
				_ = ws.WriteMessage(websocket.BinaryMessage, (tunnel.Packet{Kind: tunnel.UploadStatus, Body: []byte(`{"type":"complete","loaded":13}`)}).Bytes())
			} else {
				send(upload.FinishRecord())
			}
			if mode != "cancel" && mode != "forged completion" {
				for {
					_, body, err := ws.ReadMessage()
					if err != nil {
						t.Fatal(err)
					}
					packet, err := tunnel.Parse(body)
					if err != nil {
						t.Fatal(err)
					}
					if packet.Kind != tunnel.UploadStatus {
						continue
					}
					var status protocol.FileUploadStatus
					if err := json.Unmarshal(packet.Body, &status); err != nil {
						t.Fatal(err)
					}
					if status.Type == "progress" {
						continue
					}
					want := "complete"
					if mode == "checksum" {
						want = "error"
					}
					if status.Type != want {
						t.Fatalf("unexpected status: %+v", status)
					}
					break
				}
			}
			deadline := time.Now().Add(5 * time.Second)
			for {
				entries, _ := os.ReadDir(dir)
				page, err := app.audit.Repository().ListCommandAuditLogs(context.Background(), store.AuditLogFilter{TargetID: target.ID, RequestType: store.RequestSFTP, Limit: 10})
				if err == nil && len(entries) == 1 && len(page.Logs) > 0 {
					logs := page.Logs
					success := mode == "complete" || mode == "empty" || mode == "duplicate"
					if logs[0].ExitCode == nil || (*logs[0].ExitCode == 0) != success {
						t.Fatalf("incorrect trusted audit: %+v", logs[0])
					}
					got, _ := os.ReadFile(dest)
					want := []byte("original")
					if success {
						want = data
					}
					if !bytes.Equal(got, want) {
						t.Fatalf("corrupted destination: %q", got)
					}
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("cleanup or audit missing")
				}
				time.Sleep(20 * time.Millisecond)
			}
		})
	}
}

func TestFileUploadValidationAndPermissions(t *testing.T) {
	srv, client, app := newAPITestServer(t)
	defer srv.Close()
	postJSON(t, client, srv.URL+"/api/auth/login", map[string]string{"email": "admin", "password": "admin-pass"}, http.StatusOK, nil)
	user, _ := app.store.Repository().GetUserByEmail(context.Background(), "admin")
	org, _ := app.store.Repository().GetPersonalOrganizationForUser(context.Background(), user.ID)
	attachTunnelAgent(t, app, "upload-agent")
	target := tunnelAgentTarget(t, app, user, org, "upload-agent")
	for _, name := range []string{"../escape", `a\b`, ".", "..", ""} {
		response, err := client.Get(srv.URL + "/api/targets/" + target.ID + "/files/upload/ws?size=1&name=" + url.QueryEscape(name))
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, response.Body)
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("filename accepted: %q %d", name, response.StatusCode)
		}
	}
	attachAllowSFTPPolicyForTargetAccess(t, app, org.ID, target.ID, false, true)
	response, err := client.Get(srv.URL + "/api/targets/" + target.ID + "/files/upload/ws?size=1&name=file")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("download-only policy permitted upload: %d", response.StatusCode)
	}
}
