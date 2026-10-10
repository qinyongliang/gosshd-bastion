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
	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
	"github.com/qinyongliang/gosshd-bastion/internal/store"
	"github.com/qinyongliang/gosshd-bastion/internal/tunnel"
	"github.com/qinyongliang/gosshd-bastion/internal/upload"
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
	if err != nil || size < 0 || size > upload.MaxSize || name == "" || name == "." || name == ".." || path.Base(name) != name || strings.ContainsAny(name, "\\\x00") {
		writeError(w, http.StatusBadRequest, "invalid file name or size")
		return
	}
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
	dest := remoteJoin(dir, name)
	req := protocol.StreamRequest{Type: protocol.StreamFileUpload, Upload: &protocol.FileUploadRequest{Path: dest, Size: size}, TunnelHops: endpoint.hops, STUNServers: servers}
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
			_ = writeMessage(map[string]string{"type": "error", "error": err.Error()})
		}
		return
	}
	_ = stream.SetDeadline(time.Time{})
	if writeMessage(map[string]any{"type": "ready", "path": dest, "stun_servers": servers}) != nil {
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
			if relay.Send(packet) != nil {
				return
			}
		}
	}()
	result := "upload cancelled or connection lost"
	code := 255
	defer func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
		defer cancel()
		a.auditWebSFTP(ctx, user, target, decision, fmt.Sprintf("sftp upload %s (%d bytes)", dest, size), decision.Action, result, code, sshSourceIPFromRequest(r))
	}()
	for {
		packet, err := relay.Read()
		if err != nil {
			return
		}
		finished := false
		if packet.Kind == tunnel.UploadStatus {
			var status protocol.FileUploadStatus
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
