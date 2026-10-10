package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/qinyongliang/gosshd-bastion/internal/filetransfer"
	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
	"github.com/qinyongliang/gosshd-bastion/internal/store"
	"github.com/qinyongliang/gosshd-bastion/internal/tunnel"
)

func TestFileUploadDownloadSessionReuse(t *testing.T) {
	for _, action := range []string{"upload", "download"} {
		for _, ssh := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/ssh=%t", action, ssh), func(t *testing.T) {
				srv, client, app := newAPITestServer(t)
				defer srv.Close()
				postJSON(t, client, srv.URL+"/api/auth/login", map[string]string{"email": "admin", "password": "admin-pass"}, http.StatusOK, nil)
				user, _ := app.store.Repository().GetUserByEmail(context.Background(), "admin")
				org, _ := app.store.Repository().GetPersonalOrganizationForUser(context.Background(), user.ID)
				attachTunnelAgent(t, app, "session-agent")
				target := uploadAgentTarget(t, app, user, org, "session-agent")
				var connections atomic.Int64
				var initialConnections int64
				if ssh {
					address, closeServer := startTestSFTPServer(t, testSFTPModeSubsystem, func() { connections.Add(1) })
					defer closeServer()
					target = tunnelSSHTarget(t, app, user, org, "session-ssh", address, target.ID)
				}
				dir := t.TempDir()
				fileStarts := make(map[string]time.Time)
				var ws *websocket.Conn
				var sendSeq, receiveSeq uint64 = 1, 1
				send := func(p tunnel.Packet) {
					t.Helper()
					if err := ws.WriteMessage(websocket.BinaryMessage, p.Bytes()); err != nil {
						t.Fatal(err)
					}
				}
				for index, data := range [][]byte{[]byte("first file"), nil, bytes.Repeat([]byte("other contents"), 10000)} {
					name := fmt.Sprintf("file-%d", index)
					full := filepath.Join(dir, name)
					p := dir
					if action == "download" {
						p = full
						if err := os.WriteFile(full, data, 0600); err != nil {
							t.Fatal(err)
						}
					}
					auditPath := full
					if action == "upload" {
						auditPath = remoteJoin(dir, name)
					}
					fileStarts[fmt.Sprintf("sftp %s %s (%d bytes)", action, auditPath, len(data))] = time.Now().UTC()
					if index == 0 {
						params := url.Values{"reuse": {"1"}, "path": {p}, "name": {name}, "size": {fmt.Sprint(len(data))}}
						u, _ := url.Parse(srv.URL)
						headers := http.Header{"Origin": {srv.URL}}
						for _, c := range client.Jar.Cookies(u) {
							headers.Add("Cookie", c.String())
						}
						var err error
						ws, _, err = websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/api/targets/"+target.ID+"/files/"+action+"/ws?"+params.Encode(), headers)
						if err != nil {
							t.Fatal(err)
						}
						defer ws.Close()
					} else {
						if err := ws.WriteJSON(map[string]any{"path": p, "name": name, "size": len(data)}); err != nil {
							t.Fatal(err)
						}
					}
					_ = ws.SetReadDeadline(time.Now().Add(15 * time.Second))
					// ACKs for prior file control can arrive after its receipt.
					for {
						kind, body, err := ws.ReadMessage()
						if err != nil {
							t.Fatal(err)
						}
						if kind != websocket.TextMessage {
							continue
						}
						var ready map[string]any
						_ = json.Unmarshal(body, &ready)
						if ready["type"] != "ready" || ready["reuse"] != true {
							t.Fatalf("not reusable: %s", body)
						}
						break
					}
					if index == 0 {
						initialConnections = connections.Load()
					}
					time.Sleep(40 * time.Millisecond)
					if action == "upload" {
						for offset := 0; offset < len(data); offset += filetransfer.MaxChunkSize {
							send(tunnel.Packet{Kind: tunnel.Data, Seq: sendSeq, Body: filetransfer.ChunkRecord(data[offset:min(len(data), offset+filetransfer.MaxChunkSize)])})
							sendSeq++
						}
						send(tunnel.Packet{Kind: tunnel.Data, Seq: sendSeq, Body: filetransfer.FinishRecord()})
						sendSeq++
					} else {
						send(tunnel.Packet{Kind: tunnel.Data, Seq: sendSeq, Body: []byte{1}})
						sendSeq++
					}
					var records bytes.Buffer
					for {
						_, body, err := ws.ReadMessage()
						if err != nil {
							t.Fatal(err)
						}
						packet, err := tunnel.Parse(body)
						if err != nil {
							t.Fatal(err)
						}
						if packet.Kind == tunnel.Data {
							if packet.Seq != receiveSeq {
								t.Fatalf("sequence reset: %d want %d", packet.Seq, receiveSeq)
							}
							receiveSeq++
							records.Write(packet.Body)
							send(tunnel.Packet{Kind: tunnel.Ack, Seq: packet.Seq, Body: []byte{0}})
							if bytes.Equal(packet.Body, filetransfer.FinishRecord()) {
								var received bytes.Buffer
								if _, err := filetransfer.Receive(&records, &received, int64(len(data)), nil); err != nil || !bytes.Equal(data, received.Bytes()) {
									t.Fatalf("corrupt download: %v", err)
								}
								send(tunnel.Packet{Kind: tunnel.Data, Seq: sendSeq, Body: []byte{2}})
								sendSeq++
							}
						} else if packet.Kind == tunnel.TransferStatus {
							var status protocol.FileTransferStatus
							_ = json.Unmarshal(packet.Body, &status)
							if status.Type == "error" {
								t.Fatal(status.Error)
							}
							if status.Type == "complete" {
								hash := sha256.Sum256(data)
								if status.SHA256 != hex.EncodeToString(hash[:]) {
									t.Fatal("incorrect checksum")
								}
								break
							}
						}
					}
					actual, err := os.ReadFile(full)
					if err != nil || !bytes.Equal(actual, data) {
						t.Fatalf("corrupt file: %v", err)
					}
				}
				if ssh && connections.Load() != initialConnections {
					t.Fatalf("batch reconnected SSH: %d initially %d", connections.Load(), initialConnections)
				}
				// An established P2P/session never bypasses a newly changed policy.
				attachAllowSFTPPolicyForTargetAccess(t, app, org.ID, target.ID, false, false)
				if err := ws.WriteJSON(map[string]any{"path": dir, "name": "denied", "size": 0}); err != nil {
					t.Fatal(err)
				}
				for {
					kind, body, err := ws.ReadMessage()
					if err != nil {
						t.Fatal(err)
					}
					if kind != websocket.TextMessage {
						continue
					}
					var result map[string]string
					_ = json.Unmarshal(body, &result)
					if result["type"] != "error" || !strings.Contains(result["error"], "policy") {
						t.Fatalf("policy change ignored: %s", body)
					}
					break
				}
				logs, err := app.audit.Repository().ListCommandAuditLogs(context.Background(), store.AuditLogFilter{TargetID: target.ID, RequestType: store.RequestSFTP, Limit: 20})
				if err != nil || len(logs.Logs) != 4 {
					t.Fatalf("per-file audit missing: %d %v", len(logs.Logs), err)
				}
				for _, log := range logs.Logs {
					if log.ExitCode == nil || *log.ExitCode != 0 {
						continue
					}
					start, ok := fileStarts[log.Command]
					if !ok || log.StartedAt.Before(start) || log.EndedAt == nil || log.EndedAt.Sub(log.StartedAt) < 40*time.Millisecond {
						t.Fatalf("file audit did not measure its own transfer: %+v", log)
					}
				}
			})
		}
	}
}
