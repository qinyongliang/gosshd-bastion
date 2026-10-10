package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/qinyongliang/gosshd-bastion/internal/filetransfer"
	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
	"github.com/qinyongliang/gosshd-bastion/internal/store"
	"github.com/qinyongliang/gosshd-bastion/internal/tunnel"
)

func downloadWS(t *testing.T, base string, client *http.Client, target, source string) (*websocket.Conn, map[string]any) {
	t.Helper()
	u, _ := url.Parse(base)
	headers := http.Header{"Origin": {base}}
	for _, c := range client.Jar.Cookies(u) {
		headers.Add("Cookie", c.String())
	}
	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(base, "http")+"/api/targets/"+target+"/files/download/ws?path="+url.QueryEscape(source), headers)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	_ = ws.SetReadDeadline(time.Now().Add(15 * time.Second))
	var ready map[string]any
	if err := ws.ReadJSON(&ready); err != nil {
		t.Fatal(err)
	}
	return ws, ready
}

func TestFileDownloadLocalAndSSH(t *testing.T) {
	for _, mode := range []string{"local", "ssh", "empty", "duplicate start", "cancel", "forged completion", "missing", "directory"} {
		t.Run(mode, func(t *testing.T) {
			srv, client, app := newAPITestServer(t)
			defer srv.Close()
			postJSON(t, client, srv.URL+"/api/auth/login", map[string]string{"email": "admin", "password": "admin-pass"}, http.StatusOK, nil)
			user, _ := app.store.Repository().GetUserByEmail(context.Background(), "admin")
			org, _ := app.store.Repository().GetPersonalOrganizationForUser(context.Background(), user.ID)
			attachTunnelAgent(t, app, "download-agent")
			target := uploadAgentTarget(t, app, user, org, "download-agent")
			if mode == "ssh" {
				address, closeServer := startTestSFTPServer(t, testSFTPModeSubsystem)
				defer closeServer()
				target = tunnelSSHTarget(t, app, user, org, "ssh-download", address, target.ID)
			}
			data := bytes.Repeat([]byte("download\x00\xff"), 100000)
			if mode == "empty" {
				data = nil
			}
			dir := t.TempDir()
			source := filepath.Join(dir, "file.bin")
			if err := os.WriteFile(source, data, 0600); err != nil {
				t.Fatal(err)
			}
			if mode == "missing" {
				source += ".missing"
			}
			if mode == "directory" {
				source = dir
			}
			ws, ready := downloadWS(t, srv.URL, client, target.ID, source)
			if mode == "missing" || mode == "directory" {
				if ready["type"] != "error" {
					t.Fatalf("invalid source accepted: %+v", ready)
				}
				return
			}
			if ready["type"] != "ready" || ready["size"] != float64(len(data)) {
				t.Fatalf("not ready: %+v", ready)
			}
			time.Sleep(40 * time.Millisecond)
			send := func(p tunnel.Packet) {
				if err := ws.WriteMessage(websocket.BinaryMessage, p.Bytes()); err != nil {
					t.Fatal(err)
				}
			}
			start := tunnel.Packet{Kind: tunnel.Data, Seq: 1, Body: []byte{1}}
			send(start)
			if mode == "duplicate start" {
				send(start)
			}
			if mode == "forged completion" {
				send(tunnel.Packet{Kind: tunnel.TransferStatus, Body: []byte(`{"type":"complete"}`)})
			}
			var records bytes.Buffer
			var next uint64 = 1
			if mode != "forged completion" {
				for {
					_, body, err := ws.ReadMessage()
					if err != nil {
						t.Fatal(err)
					}
					p, err := tunnel.Parse(body)
					if err != nil {
						t.Fatal(err)
					}
					if p.Kind == tunnel.Data {
						if mode == "cancel" {
							ws.Close()
							break
						}
						if p.Seq != next {
							t.Fatalf("out of order packet: %d want %d", p.Seq, next)
						}
						next++
						records.Write(p.Body)
						send(tunnel.Packet{Kind: tunnel.Ack, Seq: p.Seq, Body: []byte{0}})
						if bytes.Equal(p.Body, filetransfer.FinishRecord()) {
							var received bytes.Buffer
							if _, err := filetransfer.Receive(&records, &received, int64(len(data)), nil); err != nil || !bytes.Equal(received.Bytes(), data) {
								t.Fatalf("corrupt download: %v", err)
							}
							send(tunnel.Packet{Kind: tunnel.Data, Seq: 2, Body: []byte{2}})
						}
					} else if p.Kind == tunnel.TransferStatus {
						var status protocol.FileTransferStatus
						if err := json.Unmarshal(p.Body, &status); err != nil {
							t.Fatal(err)
						}
						hash := sha256.Sum256(data)
						if status.Type != "complete" || status.SHA256 != hex.EncodeToString(hash[:]) {
							t.Fatalf("invalid receipt: %+v", status)
						}
						break
					}
				}
			}
			ws.Close()
			deadline := time.Now().Add(5 * time.Second)
			for {
				page, err := app.audit.Repository().ListCommandAuditLogs(context.Background(), store.AuditLogFilter{TargetID: target.ID, RequestType: store.RequestSFTP, Limit: 10})
				if err == nil && len(page.Logs) > 0 {
					log := page.Logs[0]
					if log.EndedAt == nil || log.EndedAt.Sub(log.StartedAt) < 40*time.Millisecond {
						t.Fatalf("download duration omitted transfer time: %+v", log)
					}
					success := mode != "cancel" && mode != "forged completion"
					if log.ExitCode == nil || (*log.ExitCode == 0) != success {
						t.Fatalf("incorrect audit: %+v", log)
					}
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("download audit missing")
				}
				time.Sleep(20 * time.Millisecond)
			}
		})
	}
}

func TestFileDownloadPermissionsAndOrigin(t *testing.T) {
	srv, client, app := newAPITestServer(t)
	defer srv.Close()
	postJSON(t, client, srv.URL+"/api/auth/login", map[string]string{"email": "admin", "password": "admin-pass"}, http.StatusOK, nil)
	user, _ := app.store.Repository().GetUserByEmail(context.Background(), "admin")
	org, _ := app.store.Repository().GetPersonalOrganizationForUser(context.Background(), user.ID)
	attachTunnelAgent(t, app, "download-agent")
	target := uploadAgentTarget(t, app, user, org, "download-agent")
	endpoint := srv.URL + "/api/targets/" + target.ID + "/files/download/ws?path=file"
	resp, err := http.Get(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous download allowed: %d", resp.StatusCode)
	}
	u, _ := url.Parse(srv.URL)
	headers := http.Header{"Origin": {"https://other.example"}}
	for _, c := range client.Jar.Cookies(u) {
		headers.Add("Cookie", c.String())
	}
	_, resp, err = websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(endpoint, "http"), headers)
	if err == nil || resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin download allowed: %v %+v", err, resp)
	}
	resp.Body.Close()
	attachAllowSFTPPolicyForTargetAccess(t, app, org.ID, target.ID, true, false)
	resp, err = client.Get(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("upload-only policy allowed download: %d", resp.StatusCode)
	}
}
