package server

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	pathpkg "path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/qinyongliang/gosshd-bastion/internal/bastion"
	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
	"github.com/qinyongliang/gosshd-bastion/internal/store"

	"github.com/gorilla/websocket"
	"github.com/pkg/sftp"
	gossh "golang.org/x/crypto/ssh"
)

const webConsolePublicKeyName = "Web console"

type terminalWSMessage struct {
	Type string `json:"type"`
	Data string `json:"data,omitempty"`
	Code int    `json:"code,omitempty"`
	Cols int    `json:"cols,omitempty"`
	Rows int    `json:"rows,omitempty"`
}

type terminalWSWriter struct {
	mu sync.Mutex
	ws *websocket.Conn
}

func (w *terminalWSWriter) write(msg terminalWSMessage) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.ws.WriteJSON(msg)
}

func (a *App) handleTargetTerminalWS(w http.ResponseWriter, r *http.Request, user store.User) {
	ctx := r.Context()
	target, err := a.targetForUser(ctx, r.PathValue("id"), user)
	if err != nil {
		writeOwnerError(w, err)
		return
	}
	sourceIP := sshSourceIPFromRequest(r)
	decision, err := a.bastion.EvaluateAccess(ctx, user.ID, target.ID, store.RequestShell, sourceIP)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if decision.Action == store.DecisionDeny {
		code := 126
		now := time.Now().UTC()
		_, _ = a.createAuditLog(context.Background(), store.CreateCommandAuditLogParams{
			UserID:         user.ID,
			TargetID:       target.ID,
			OrganizationID: organizationIDForTarget(target),
			PublicKeyName:  webConsolePublicKeyName,
			SessionID:      newAuditSessionID(),
			Command:        "web terminal",
			RequestType:    store.RequestShell,
			PolicyDecision: store.DecisionDeny,
			PolicyReason:   decision.Reason,
			ExitCode:       &code,
			StartedAt:      now,
			EndedAt:        &now,
			RemoteAddress:  sourceIP,
		})
		writeError(w, http.StatusForbidden, "interactive terminal denied: "+decision.Reason)
		return
	}

	cols, rows := terminalSizeFromQuery(r)
	sessionID := newAuditSessionID()
	startedAt := time.Now().UTC()
	recorder, err := newTerminalRecorder(a.auditRecordingsPath, sessionID, cols, rows, target)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "terminal recording unavailable: "+err.Error())
		return
	}
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		_, _ = recorder.Close()
		return
	}
	writer := &terminalWSWriter{ws: ws}
	exitCode := a.webTerminalOnTarget(context.Background(), target, ws, writer, cols, rows, recorder)
	endedAt := time.Now().UTC()
	a.recordShellAuditAsync(recorder, store.CreateCommandAuditLogParams{
		UserID:         user.ID,
		TargetID:       target.ID,
		OrganizationID: organizationIDForTarget(target),
		PublicKeyName:  webConsolePublicKeyName,
		SessionID:      sessionID,
		Command:        "web terminal",
		RequestType:    store.RequestShell,
		PolicyDecision: decision.Action,
		PolicyReason:   decision.Reason,
		ExitCode:       &exitCode,
		StartedAt:      startedAt,
		EndedAt:        &endedAt,
		RemoteAddress:  sourceIP,
	})
	_ = writer.write(terminalWSMessage{Type: "exit", Code: exitCode})
	_ = ws.Close()
}

func (a *App) webTerminalOnTarget(ctx context.Context, target store.SSHTarget, ws *websocket.Conn, writer *terminalWSWriter, cols, rows int, recorder *terminalRecorder) int {
	if target.TargetType == store.TargetAgent {
		return a.webAgentTerminal(target.AgentID, ws, writer, cols, rows, recorder)
	}
	return a.webDirectTerminal(ctx, target, ws, writer, cols, rows, recorder)
}

func (a *App) webDirectTerminal(ctx context.Context, target store.SSHTarget, ws *websocket.Conn, writer *terminalWSWriter, cols, rows int, recorder *terminalRecorder) int {
	client, err := a.openTargetSSHClient(ctx, target)
	if err != nil {
		_ = writer.write(terminalWSMessage{Type: "error", Data: err.Error()})
		return 255
	}
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		_ = writer.write(terminalWSMessage{Type: "error", Data: err.Error()})
		return 255
	}
	defer session.Close()
	stdin, err := session.StdinPipe()
	if err != nil {
		_ = writer.write(terminalWSMessage{Type: "error", Data: err.Error()})
		return 255
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		_ = writer.write(terminalWSMessage{Type: "error", Data: err.Error()})
		return 255
	}
	stderr, err := session.StderrPipe()
	if err != nil {
		_ = writer.write(terminalWSMessage{Type: "error", Data: err.Error()})
		return 255
	}
	if err := session.RequestPty("xterm-256color", rows, cols, gossh.TerminalModes{}); err != nil {
		_ = writer.write(terminalWSMessage{Type: "error", Data: err.Error()})
		return 255
	}
	if err := session.Shell(); err != nil {
		_ = writer.write(terminalWSMessage{Type: "error", Data: err.Error()})
		return 255
	}

	go func() {
		for {
			var msg terminalWSMessage
			if err := ws.ReadJSON(&msg); err != nil {
				_ = closeWriter(stdin)
				_ = session.Close()
				return
			}
			switch msg.Type {
			case "input":
				_, _ = io.WriteString(stdin, msg.Data)
			case "resize":
				if msg.Cols > 0 && msg.Rows > 0 {
					_ = session.WindowChange(msg.Rows, msg.Cols)
				}
			}
		}
	}()

	var outputWG sync.WaitGroup
	outputWG.Add(2)
	go copyReaderToTerminalWS(&outputWG, writer, "output", stdout, recorder)
	go copyReaderToTerminalWS(&outputWG, writer, "error", stderr, recorder)
	err = session.Wait()
	_ = closeWriter(stdin)
	outputWG.Wait()
	if err == nil {
		return 0
	}
	if exit, ok := err.(*gossh.ExitError); ok {
		return exit.ExitStatus()
	}
	return 255
}

func (a *App) webAgentTerminal(agentID string, ws *websocket.Conn, writer *terminalWSWriter, cols, rows int, recorder *terminalRecorder) int {
	reader, stream, err := a.openAgentStream(agentID, protocol.StreamRequest{Type: protocol.StreamShell, Width: cols, Height: rows})
	if err != nil {
		_ = writer.write(terminalWSMessage{Type: "error", Data: err.Error()})
		return 255
	}
	defer stream.Close()
	go func() {
		for {
			var msg terminalWSMessage
			if err := ws.ReadJSON(&msg); err != nil {
				_ = stream.Close()
				return
			}
			switch msg.Type {
			case "input":
				_ = protocol.WriteFrame(stream, protocol.Frame{Type: protocol.FrameStdin, Data: []byte(msg.Data)})
			case "resize":
				if msg.Cols > 0 && msg.Rows > 0 {
					var data [8]byte
					binary.BigEndian.PutUint32(data[0:4], uint32(msg.Cols))
					binary.BigEndian.PutUint32(data[4:8], uint32(msg.Rows))
					_ = protocol.WriteFrame(stream, protocol.Frame{Type: protocol.FrameResize, Data: data[:]})
				}
			}
		}
	}()
	exitCode := 255
	for {
		frame, err := protocol.ReadFrame(reader)
		if err != nil {
			break
		}
		switch frame.Type {
		case protocol.FrameStdout:
			recorder.WriteOutput(frame.Data)
			_ = writer.write(terminalWSMessage{Type: "output", Data: string(frame.Data)})
		case protocol.FrameStderr:
			recorder.WriteOutput(frame.Data)
			_ = writer.write(terminalWSMessage{Type: "error", Data: string(frame.Data)})
		case protocol.FrameExit:
			exitCode = protocol.ExitCode(frame)
			return exitCode
		}
	}
	return exitCode
}

func copyReaderToTerminalWS(wg *sync.WaitGroup, writer *terminalWSWriter, typ string, reader io.Reader, recorder *terminalRecorder) {
	defer wg.Done()
	buf := make([]byte, 32*1024)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			recorder.WriteOutput(chunk)
			_ = writer.write(terminalWSMessage{Type: typ, Data: string(chunk)})
		}
		if err != nil {
			return
		}
	}
}

type apiTargetFileEntry struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	Type       string `json:"type"`
	Size       int64  `json:"size"`
	Mode       string `json:"mode"`
	ModifiedAt string `json:"modified_at,omitempty"`
}

type apiTargetFilesResponse struct {
	Path    string               `json:"path"`
	Entries []apiTargetFileEntry `json:"entries"`
}

func (a *App) handleTargetFiles(w http.ResponseWriter, r *http.Request, user store.User) {
	target, decision, _, allowDownload, ok := a.authorizeTargetSFTP(w, r, user, "list")
	if !ok {
		return
	}
	if !allowDownload {
		a.auditWebSFTP(r.Context(), user, target, decision, "sftp list "+remotePathFromQuery(r), store.DecisionDeny, "download/list is not allowed", 126, sshSourceIPFromRequest(r))
		writeError(w, http.StatusForbidden, "SFTP list is not allowed by policy")
		return
	}
	client, closeClient, err := a.openSFTPClient(r.Context(), target)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer closeClient()
	dir := remotePathFromQuery(r)
	infos, err := client.ReadDir(dir)
	if err != nil {
		a.auditWebSFTP(r.Context(), user, target, decision, "sftp list "+dir, decision.Action, err.Error(), 255, sshSourceIPFromRequest(r))
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	out := apiTargetFilesResponse{Path: dir}
	for _, info := range infos {
		out.Entries = append(out.Entries, apiFileEntry(dir, info))
	}
	a.auditWebSFTP(r.Context(), user, target, decision, "sftp list "+dir, decision.Action, decision.Reason, 0, sshSourceIPFromRequest(r))
	writeJSON(w, http.StatusOK, out)
}

func (a *App) handleTargetFileDownload(w http.ResponseWriter, r *http.Request, user store.User) {
	target, decision, _, allowDownload, ok := a.authorizeTargetSFTP(w, r, user, "download")
	if !ok {
		return
	}
	filePath := remotePathFromQuery(r)
	if !allowDownload {
		a.auditWebSFTP(r.Context(), user, target, decision, "sftp download "+filePath, store.DecisionDeny, "download is not allowed", 126, sshSourceIPFromRequest(r))
		writeError(w, http.StatusForbidden, "SFTP download is not allowed by policy")
		return
	}
	client, closeClient, err := a.openSFTPClient(r.Context(), target)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer closeClient()
	file, err := client.Open(filePath)
	if err != nil {
		a.auditWebSFTP(r.Context(), user, target, decision, "sftp download "+filePath, decision.Action, err.Error(), 255, sshSourceIPFromRequest(r))
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+downloadName(filePath)+`"`)
	if _, err := io.Copy(w, file); err != nil {
		a.auditWebSFTP(context.Background(), user, target, decision, "sftp download "+filePath, decision.Action, err.Error(), 255, sshSourceIPFromRequest(r))
		return
	}
	a.auditWebSFTP(context.Background(), user, target, decision, "sftp download "+filePath, decision.Action, decision.Reason, 0, sshSourceIPFromRequest(r))
}

func (a *App) handleTargetFileUpload(w http.ResponseWriter, r *http.Request, user store.User) {
	target, decision, allowUpload, _, ok := a.authorizeTargetSFTP(w, r, user, "upload")
	if !ok {
		return
	}
	dir := remotePathFromQuery(r)
	if !allowUpload {
		a.auditWebSFTP(r.Context(), user, target, decision, "sftp upload "+dir, store.DecisionDeny, "upload is not allowed", 126, sshSourceIPFromRequest(r))
		writeError(w, http.StatusForbidden, "SFTP upload is not allowed by policy")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024<<20)
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file is required")
		return
	}
	defer file.Close()
	name := pathpkg.Base(header.Filename)
	if name == "." || name == "/" || name == "" {
		writeError(w, http.StatusBadRequest, "invalid file name")
		return
	}
	client, closeClient, err := a.openSFTPClient(r.Context(), target)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer closeClient()
	destPath := remoteJoin(dir, name)
	dst, err := client.Create(destPath)
	if err != nil {
		a.auditWebSFTP(r.Context(), user, target, decision, "sftp upload "+destPath, decision.Action, err.Error(), 255, sshSourceIPFromRequest(r))
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	written, copyErr := io.Copy(dst, file)
	closeErr := dst.Close()
	if copyErr != nil {
		a.auditWebSFTP(r.Context(), user, target, decision, "sftp upload "+destPath, decision.Action, copyErr.Error(), 255, sshSourceIPFromRequest(r))
		writeError(w, http.StatusBadGateway, copyErr.Error())
		return
	}
	if closeErr != nil {
		a.auditWebSFTP(r.Context(), user, target, decision, "sftp upload "+destPath, decision.Action, closeErr.Error(), 255, sshSourceIPFromRequest(r))
		writeError(w, http.StatusBadGateway, closeErr.Error())
		return
	}
	a.auditWebSFTP(r.Context(), user, target, decision, fmt.Sprintf("sftp upload %s (%d bytes)", destPath, written), decision.Action, decision.Reason, 0, sshSourceIPFromRequest(r))
	writeJSON(w, http.StatusCreated, map[string]any{"path": destPath, "size": written})
}

func (a *App) authorizeTargetSFTP(w http.ResponseWriter, r *http.Request, user store.User, action string) (store.SSHTarget, bastion.Decision, bool, bool, bool) {
	target, err := a.targetForUser(r.Context(), r.PathValue("id"), user)
	if err != nil {
		writeOwnerError(w, err)
		return store.SSHTarget{}, bastion.Decision{}, false, false, false
	}
	decision, allowUpload, allowDownload, err := a.bastion.EvaluateSFTPAccess(r.Context(), user.ID, target.ID, sshSourceIPFromRequest(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return store.SSHTarget{}, bastion.Decision{}, false, false, false
	}
	if decision.Action == store.DecisionDeny {
		a.auditWebSFTP(r.Context(), user, target, decision, "sftp "+action+" "+remotePathFromQuery(r), store.DecisionDeny, decision.Reason, 126, sshSourceIPFromRequest(r))
		writeError(w, http.StatusForbidden, "SFTP denied: "+decision.Reason)
		return store.SSHTarget{}, bastion.Decision{}, false, false, false
	}
	return target, decision, allowUpload, allowDownload, true
}

func (a *App) openSFTPClient(ctx context.Context, target store.SSHTarget) (*sftp.Client, func(), error) {
	if target.TargetType == store.TargetAgent {
		reader, stream, err := a.openAgentStream(target.AgentID, protocol.StreamRequest{Type: protocol.StreamSFTP})
		if err != nil {
			return nil, nil, err
		}
		client, err := sftp.NewClientPipe(reader, stream)
		if err != nil {
			_ = stream.Close()
			return nil, nil, err
		}
		return client, func() {
			_ = client.Close()
			_ = stream.Close()
		}, nil
	}
	sshClient, err := a.openTargetSSHClient(ctx, target)
	if err != nil {
		return nil, nil, err
	}
	client, err := sftp.NewClient(sshClient)
	if err != nil {
		_ = sshClient.Close()
		return nil, nil, err
	}
	return client, func() {
		_ = client.Close()
		_ = sshClient.Close()
	}, nil
}

func (a *App) auditWebSFTP(ctx context.Context, user store.User, target store.SSHTarget, decision bastion.Decision, command, action, reason string, code int, sourceIP string) {
	now := time.Now().UTC()
	_, _ = a.createAuditLog(ctx, store.CreateCommandAuditLogParams{
		UserID:         user.ID,
		TargetID:       target.ID,
		OrganizationID: organizationIDForTarget(target),
		PublicKeyName:  webConsolePublicKeyName,
		SessionID:      newAuditSessionID(),
		Command:        command,
		RequestType:    store.RequestSFTP,
		PolicyDecision: action,
		PolicyReason:   firstNonEmpty(reason, decision.Reason),
		ExitCode:       &code,
		StartedAt:      now,
		EndedAt:        &now,
		RemoteAddress:  sourceIP,
	})
}

func (a *App) targetForUser(ctx context.Context, targetID string, user store.User) (store.SSHTarget, error) {
	if err := a.ensureServices(ctx); err != nil {
		return store.SSHTarget{}, err
	}
	target, err := a.store.Repository().GetSSHTarget(ctx, strings.TrimSpace(targetID))
	if err != nil {
		return store.SSHTarget{}, err
	}
	if _, _, err := a.resolveOwner(ctx, target.OwnerType, target.OwnerID, user.ID); err != nil {
		return store.SSHTarget{}, err
	}
	return target, nil
}

func apiFileEntry(dir string, info fs.FileInfo) apiTargetFileEntry {
	typ := "file"
	if info.IsDir() {
		typ = "dir"
	} else if info.Mode()&fs.ModeSymlink != 0 {
		typ = "symlink"
	}
	return apiTargetFileEntry{
		Name:       info.Name(),
		Path:       remoteJoin(dir, info.Name()),
		Type:       typ,
		Size:       info.Size(),
		Mode:       info.Mode().String(),
		ModifiedAt: info.ModTime().UTC().Format(time.RFC3339),
	}
}

func terminalSizeFromQuery(r *http.Request) (int, int) {
	cols, _ := strconv.Atoi(r.URL.Query().Get("cols"))
	rows, _ := strconv.Atoi(r.URL.Query().Get("rows"))
	if cols <= 0 {
		cols = 120
	}
	if rows <= 0 {
		rows = 32
	}
	return cols, rows
}

func remotePathFromQuery(r *http.Request) string {
	raw := strings.TrimSpace(r.URL.Query().Get("path"))
	if raw == "" {
		return "."
	}
	return pathpkg.Clean(raw)
}

func remoteJoin(dir, name string) string {
	if strings.TrimSpace(dir) == "" || dir == "." {
		return pathpkg.Clean(name)
	}
	return pathpkg.Join(dir, name)
}

func downloadName(remotePath string) string {
	name := pathpkg.Base(remotePath)
	if name == "." || name == "/" || name == "" {
		return "download"
	}
	return strings.ReplaceAll(name, `"`, "")
}

func sshSourceIPFromRequest(r *http.Request) string {
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
		return strings.TrimSpace(strings.Split(forwarded, ",")[0])
	}
	return sshSourceIP(dummyRemoteAddr(r.RemoteAddr))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

type dummyRemoteAddr string

func (a dummyRemoteAddr) Network() string { return "tcp" }
func (a dummyRemoteAddr) String() string  { return string(a) }
