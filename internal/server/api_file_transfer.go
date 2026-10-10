package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path"
	"strconv"
	"strings"
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
	if writeMessage(map[string]any{"type": "ready", "path": filePath, "size": size, "stun_servers": servers}) != nil {
		return
	}
	relay := &tunnel.Relay{Reader: reader, Writer: stream, Closer: stream}
	go func() {
		defer stream.Close()
		for {
			_ = ws.SetReadDeadline(time.Now().Add(60 * time.Second))
			kind, body, err := ws.ReadMessage()
			if err != nil || kind != websocket.BinaryMessage {
				return
			}
			packet, err := tunnel.Parse(body)
			if err != nil {
				return
			}
			switch packet.Kind {
			case tunnel.Data, tunnel.Fin, tunnel.Ack, tunnel.Signal, tunnel.Probe, tunnel.ProbeAck:
			default:
				// Completion and audit metadata must originate from the Agent.
				return
			}
			if action == "download" && packet.Kind == tunnel.Data {
				// Download clients can only start and acknowledge receipt. They cannot
				// inject a new path, file contents or completion metadata.
				if len(packet.Body) != 1 || (packet.Seq != 1 || packet.Body[0] != 1) && (packet.Seq != 2 || packet.Body[0] != 2) {
					return
				}
			}
			if relay.Send(packet) != nil {
				return
			}
		}
	}()
	result := action + " cancelled or connection lost"
	code := 255
	defer func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
		defer cancel()
		a.auditWebSFTP(ctx, user, target, decision, fmt.Sprintf("sftp %s %s (%d bytes)", action, filePath, size), decision.Action, result, code, sshSourceIPFromRequest(r))
	}()
	for {
		packet, err := relay.Read()
		if err != nil {
			return
		}
		finished := false
		if packet.Kind == tunnel.TransferStatus {
			var status protocol.FileTransferStatus
			if json.Unmarshal(packet.Body, &status) != nil {
				return
			}
			if status.Type == "complete" {
				if status.Loaded != size || len(status.SHA256) != 64 {
					return
				}
				code = 0
				result = fmt.Sprintf("%s; sha256=%s; direct transport bytes=%d", decision.Reason, status.SHA256, status.DirectBytes)
				finished = true
			} else if status.Type == "error" {
				result = status.Error
				finished = true
			}
		}
		_ = ws.SetWriteDeadline(time.Now().Add(15 * time.Second))
		if ws.WriteMessage(websocket.BinaryMessage, packet.Bytes()) != nil || finished {
			return
		}
	}
}
