package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/qinyongliang/gosshd-bastion/internal/bastion"
	"github.com/qinyongliang/gosshd-bastion/internal/filetransfer"
	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
	"github.com/qinyongliang/gosshd-bastion/internal/store"
	"github.com/qinyongliang/gosshd-bastion/internal/tunnel"
)

func (a *App) handleTargetFileUploadWS(w http.ResponseWriter, r *http.Request, user store.User) {
	target, decision, allowed, _, ok := a.authorizeTargetSFTP(w, r, user, "upload")
	if !ok {
		return
	}
	dir := remotePathFromQuery(r)
	if !allowed {
		a.auditWebSFTP(r.Context(), user, target, decision, "sftp upload "+dir, store.DecisionDeny, "upload is not allowed", 126, sshSourceIPFromRequest(r))
		writeError(w, http.StatusForbidden, "SFTP upload is not allowed by policy")
		return
	}
	name := r.URL.Query().Get("name")
	size, err := strconv.ParseInt(r.URL.Query().Get("size"), 10, 64)
	if err != nil || size < 0 || size > filetransfer.MaxSize || name == "" || name == "." || name == ".." || path.Base(name) != name || strings.ContainsAny(name, "\\\x00") {
		writeError(w, http.StatusBadRequest, "invalid file name or size")
		return
	}
	dest := remoteJoin(dir, name)
	a.handleTargetFileTransferWS(w, r, user, target, decision, protocol.StreamRequest{Type: protocol.StreamFileUpload, Upload: &protocol.FileUploadRequest{Path: dest, Size: size}}, "upload", dest, size)
}

func (a *App) handleTargetFileDownloadWS(w http.ResponseWriter, r *http.Request, user store.User) {
	target, decision, _, allowed, ok := a.authorizeTargetSFTP(w, r, user, "download")
	if !ok {
		return
	}
	source := remotePathFromQuery(r)
	if !allowed {
		a.auditWebSFTP(r.Context(), user, target, decision, "sftp download "+source, store.DecisionDeny, "download is not allowed", 126, sshSourceIPFromRequest(r))
		writeError(w, http.StatusForbidden, "SFTP download is not allowed by policy")
		return
	}
	if strings.ContainsRune(source, 0) {
		writeError(w, http.StatusBadRequest, "invalid file path")
		return
	}
	a.handleTargetFileTransferWS(w, r, user, target, decision, protocol.StreamRequest{Type: protocol.StreamFileDownload, Download: &protocol.FileDownloadRequest{Path: source}}, "download", source, 0)
}

// Both directions share authorization-before-upgrade, Agent setup, ICE signaling,
// relay continuity and Agent-only status/audit handling.
func (a *App) handleTargetFileTransferWS(w http.ResponseWriter, r *http.Request, user store.User, target store.SSHTarget, decision bastion.Decision, req protocol.StreamRequest, action, filePath string, size int64) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer ws.Close()
	ws.SetReadLimit(64 * 1024)
	writeMessage := func(value any) error {
		_ = ws.SetWriteDeadline(time.Now().Add(15 * time.Second))
		return ws.WriteJSON(value)
	}
	endpoint, err := a.tunnelAgentEndpoint(r.Context(), target.ID)
	if err != nil {
		_ = writeMessage(map[string]string{"type": "error", "error": err.Error()})
		return
	}
	if endpoint == nil {
		_ = writeMessage(map[string]string{"type": "unavailable"})
		return
	}
	stream, err := endpoint.session.Open()
	if err != nil {
		_ = writeMessage(map[string]string{"type": "error", "error": err.Error()})
		return
	}
	defer stream.Close()
	_ = stream.SetDeadline(time.Now().Add(20 * time.Second))
	var servers []string
	for _, s := range strings.Split(a.cfg.TunnelSTUNServers, ",") {
		if s = strings.TrimSpace(s); s != "" {
			servers = append(servers, s)
		}
	}
	reuse := r.URL.Query().Get("reuse") == "1"
	if reuse {
		req.Type, req.FileAction = protocol.StreamFileSession, action
	}
	req.TunnelHops, req.STUNServers = endpoint.hops, servers
	if err = protocol.WriteJSONLine(stream, req); err != nil {
		_ = writeMessage(map[string]string{"type": "error", "error": err.Error()})
		return
	}
	reader := bufio.NewReader(stream)
	response, err := protocol.ReadJSONLine[protocol.StreamResponse](reader)
	if err != nil || !response.OK {
		if err == nil && response.Error == "unsupported stream type" {
			_ = writeMessage(map[string]string{"type": "unavailable"})
		} else {
			if err == nil {
				err = errors.New(response.Error)
			}
			a.auditWebSFTP(r.Context(), user, target, decision, "sftp "+action+" "+filePath, decision.Action, err.Error(), 255, sshSourceIPFromRequest(r))
			_ = writeMessage(map[string]string{"type": "error", "error": err.Error()})
		}
		return
	}
	_ = stream.SetDeadline(time.Time{})
	if action == "download" {
		size = response.Size
		if size < 0 {
			_ = writeMessage(map[string]string{"type": "error", "error": "invalid download size"})
			return
		}
	}
	if writeMessage(map[string]any{"type": "ready", "path": filePath, "size": size, "stun_servers": servers, "reuse": response.Reuse}) != nil {
		return
	}
	relay := &tunnel.Relay{Reader: reader, Writer: stream, Closer: stream}
	type operation struct {
		target   store.SSHTarget
		decision bastion.Decision
		path     string
		size     int64
	}
	current := &operation{target, decision, filePath, size}
	var opMu sync.Mutex
	rejected := make(chan string, 1)
	go func() {
		defer stream.Close()
		for {
			_ = ws.SetReadDeadline(time.Now().Add(60 * time.Second))
			kind, body, err := ws.ReadMessage()
			if err != nil {
				return
			}
			if kind == websocket.TextMessage && reuse {
				var op struct {
					Path string `json:"path"`
					Name string `json:"name"`
					Size int64  `json:"size"`
				}
				if json.Unmarshal(body, &op) != nil {
					return
				}
				query := url.Values{"path": {op.Path}}
				nextRequest := r.Clone(r.Context())
				u := *r.URL
				u.RawQuery = query.Encode()
				nextRequest.URL = &u
				t, d, upload, download, err := a.targetSFTPAccess(r.Context(), target.ID, user, sshSourceIPFromRequest(r))
				allowed := upload
				if action == "download" {
					allowed = download
				}
				if err != nil || d.Action == store.DecisionDeny || !allowed {
					if err == nil {
						a.auditWebSFTP(r.Context(), user, t, d, "sftp "+action+" "+op.Path, store.DecisionDeny, "operation is not allowed", 126, sshSourceIPFromRequest(r))
					}
					select {
					case rejected <- "SFTP operation denied by policy":
					case <-r.Context().Done():
					}
					return
				}
				e, err := a.tunnelAgentEndpoint(r.Context(), t.ID, endpoint)
				if err != nil || e == nil || e.session != endpoint.session || !reflect.DeepEqual(e.hops, endpoint.hops) {
					select {
					case rejected <- "target route changed; reconnect required":
					case <-r.Context().Done():
					}
					return
				}
				next := protocol.StreamRequest{}
				p := remotePathFromQuery(nextRequest)
				if action == "upload" {
					if op.Size < 0 || op.Size > filetransfer.MaxSize || op.Name == "" || op.Name == "." || op.Name == ".." || path.Base(op.Name) != op.Name || strings.ContainsAny(op.Name, "\\\x00") {
						return
					}
					p = remoteJoin(p, op.Name)
					next.Upload = &protocol.FileUploadRequest{Path: p, Size: op.Size}
				} else {
					if strings.ContainsRune(p, 0) {
						return
					}
					next.Download = &protocol.FileDownloadRequest{Path: p}
				}
				opMu.Lock()
				if current != nil {
					opMu.Unlock()
					return
				}
				current = &operation{t, d, p, op.Size}
				opMu.Unlock()
				next.TunnelHops = endpoint.hops
				encoded, _ := json.Marshal(next)
				if relay.Send(tunnel.Packet{Kind: tunnel.FileOperation, Body: encoded}) != nil {
					return
				}
				continue
			}
			if kind != websocket.BinaryMessage {
				return
			}
			packet, err := tunnel.Parse(body)
			if err != nil {
				return
			}
			switch packet.Kind {
			case tunnel.Data, tunnel.Ack, tunnel.Signal, tunnel.Probe, tunnel.ProbeAck:
			default:
				return
			}
			if action == "download" && packet.Kind == tunnel.Data && (len(packet.Body) != 1 || (packet.Body[0] != 1 && packet.Body[0] != 2)) {
				return
			}
			if relay.Send(packet) != nil {
				return
			}
		}
	}()
	audit := func(op *operation, result string, code int) {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
		defer cancel()
		a.auditWebSFTP(ctx, user, op.target, op.decision, fmt.Sprintf("sftp %s %s (%d bytes)", action, op.path, op.size), op.decision.Action, result, code, sshSourceIPFromRequest(r))
	}
	defer func() {
		opMu.Lock()
		op := current
		opMu.Unlock()
		if op != nil {
			audit(op, action+" cancelled or connection lost", 255)
		}
	}()
	packets := make(chan tunnel.Packet)
	done := make(chan struct{})
	defer close(done)
	go func() {
		defer close(packets)
		for {
			p, err := relay.Read()
			if err != nil {
				return
			}
			select {
			case packets <- p:
			case <-done:
				return
			}
		}
	}()
	for {
		var packet tunnel.Packet
		select {
		case reason := <-rejected:
			_ = writeMessage(map[string]string{"type": "error", "error": reason})
			return
		case p, ok := <-packets:
			if !ok {
				select {
				case reason := <-rejected:
					_ = writeMessage(map[string]string{"type": "error", "error": reason})
				default:
				}
				return
			}
			packet = p
		}
		finished, failed := false, false
		if packet.Kind == tunnel.TransferStatus {
			var status protocol.FileTransferStatus
			if json.Unmarshal(packet.Body, &status) != nil {
				return
			}
			opMu.Lock()
			op := current
			opMu.Unlock()
			if op == nil {
				return
			}
			if status.Type == "ready" {
				if action == "download" {
					opMu.Lock()
					op.size = status.Loaded
					opMu.Unlock()
				}
				if writeMessage(map[string]any{"type": "ready", "path": op.path, "size": op.size, "reuse": true}) != nil {
					return
				}
				continue
			}
			if status.Type == "complete" {
				if status.Loaded != op.size || len(status.SHA256) != 64 {
					return
				}
				audit(op, fmt.Sprintf("%s; sha256=%s; direct transport bytes=%d", op.decision.Reason, status.SHA256, status.DirectBytes), 0)
				finished = true
			} else if status.Type == "error" {
				audit(op, status.Error, 255)
				finished, failed = true, true
			}
			if finished {
				opMu.Lock()
				current = nil
				opMu.Unlock()
			}
		}
		_ = ws.SetWriteDeadline(time.Now().Add(15 * time.Second))
		if ws.WriteMessage(websocket.BinaryMessage, packet.Bytes()) != nil || failed || (finished && !reuse) {
			return
		}
	}
}
