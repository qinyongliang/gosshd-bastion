package server

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
	"github.com/qinyongliang/gosshd-bastion/internal/store"

	gossh "golang.org/x/crypto/ssh"
)

func (a *App) handleBastionSSHConn(conn *gossh.ServerConn, chans <-chan gossh.NewChannel, reqs <-chan *gossh.Request, userID, publicKeyFingerprint string) {
	alias := conn.User()
	target, err := a.resolveBastionTarget(context.Background(), userID, alias)
	if err != nil {
		go gossh.DiscardRequests(reqs)
		for ch := range chans {
			_ = ch.Reject(gossh.ConnectionFailed, err.Error())
		}
		return
	}
	sourceIP := sshSourceIP(conn.RemoteAddr())
	forwardManager := newForwardManager(conn)
	defer forwardManager.closeAll()
	go a.handleBastionGlobalRequests(forwardManager, reqs, userID, publicKeyFingerprint, target, sourceIP)
	for ch := range chans {
		switch ch.ChannelType() {
		case "session":
			go a.handleBastionSession(userID, publicKeyFingerprint, target, ch, sourceIP)
		case "direct-tcpip":
			go a.handleBastionDirectTCPIP(userID, publicKeyFingerprint, target, ch, sourceIP)
		default:
			_ = ch.Reject(gossh.UnknownChannelType, "unsupported channel type")
		}
	}
}

func (a *App) resolveBastionTarget(ctx context.Context, userID, alias string) (store.SSHTarget, error) {
	if err := a.ensureServices(ctx); err != nil {
		return store.SSHTarget{}, err
	}
	personal, err := a.store.Repository().GetPersonalOrganizationForUser(ctx, userID)
	if err != nil {
		return store.SSHTarget{}, err
	}
	for _, ownerID := range []string{personal.ID} {
		targets, err := a.store.Repository().ListSSHTargets(ctx, store.OwnerOrganization, ownerID)
		if err != nil {
			return store.SSHTarget{}, err
		}
		for _, target := range targets {
			if target.Alias == alias {
				return target, nil
			}
		}
	}
	orgs, err := a.store.Repository().ListOrganizationsForUser(ctx, userID)
	if err != nil {
		return store.SSHTarget{}, err
	}
	var matches []store.SSHTarget
	for _, org := range orgs {
		if org.ID == personal.ID {
			continue
		}
		targets, err := a.store.Repository().ListSSHTargets(ctx, store.OwnerOrganization, org.ID)
		if err != nil {
			return store.SSHTarget{}, err
		}
		for _, target := range targets {
			if target.Alias == alias {
				matches = append(matches, target)
			}
		}
	}
	if len(matches) == 0 {
		return store.SSHTarget{}, fmt.Errorf("target alias %q not found", alias)
	}
	if len(matches) > 1 {
		return store.SSHTarget{}, fmt.Errorf("target alias %q is ambiguous", alias)
	}
	return matches[0], nil
}

func (a *App) handleBastionSession(userID, publicKeyFingerprint string, target store.SSHTarget, newCh gossh.NewChannel, sourceIP string) {
	ch, reqs, err := newCh.Accept()
	if err != nil {
		return
	}
	defer ch.Close()
	ptyWidth, ptyHeight := 80, 24
	started := false
	for req := range reqs {
		switch req.Type {
		case "pty-req":
			ptyWidth, ptyHeight = parsePtySize(req.Payload)
			req.Reply(true, nil)
		case "window-change":
			ptyWidth, ptyHeight = parseWindowChange(req.Payload)
		case "exec":
			if started {
				req.Reply(false, nil)
				continue
			}
			var payload struct{ Command string }
			if err := gossh.Unmarshal(req.Payload, &payload); err != nil {
				req.Reply(false, nil)
				continue
			}
			started = true
			req.Reply(true, nil)
			a.handleBastionExec(userID, publicKeyFingerprint, target, ch, payload.Command, sourceIP)
			return
		case "shell":
			if started {
				req.Reply(false, nil)
				continue
			}
			started = true
			req.Reply(true, nil)
			a.handleBastionShell(userID, publicKeyFingerprint, target, ch, sourceIP, ptyWidth, ptyHeight)
			return
		case "subsystem":
			if started {
				req.Reply(false, nil)
				continue
			}
			var payload struct{ Name string }
			if err := gossh.Unmarshal(req.Payload, &payload); err != nil || payload.Name != "sftp" {
				req.Reply(false, nil)
				continue
			}
			started = true
			req.Reply(true, nil)
			a.handleBastionSFTP(userID, publicKeyFingerprint, target, ch, sourceIP)
			return
		default:
			req.Reply(false, nil)
		}
	}
}

func (a *App) handleBastionExec(userID, publicKeyFingerprint string, target store.SSHTarget, ch gossh.Channel, command, sourceIP string) {
	ctx := context.Background()
	sessionID := newAuditSessionID()
	startedAt := time.Now().UTC()
	decision, err := a.bastion.EvaluateCommandForSource(ctx, userID, target.ID, command, sourceIP)
	if err != nil {
		_, _ = ch.Stderr().Write([]byte(err.Error() + "\n"))
		sendExit(ch, 255)
		return
	}
	if decision.Action == store.DecisionDeny {
		_, _ = ch.Stderr().Write([]byte("command denied: " + decision.Reason + "\n"))
		code := 126
		endedAt := time.Now().UTC()
		_, _ = a.createAuditLog(ctx, store.CreateCommandAuditLogParams{
			UserID:               userID,
			TargetID:             target.ID,
			OrganizationID:       organizationIDForTarget(target),
			PublicKeyFingerprint: publicKeyFingerprint,
			SessionID:            sessionID,
			Command:              command,
			RequestType:          store.RequestExec,
			PolicyDecision:       store.DecisionDeny,
			PolicyReason:         decision.Reason,
			ExitCode:             &code,
			StartedAt:            startedAt,
			EndedAt:              &endedAt,
			RemoteAddress:        sourceIP,
		})
		sendExit(ch, code)
		return
	}
	exitCode := a.execOnTarget(ctx, target, ch, command)
	endedAt := time.Now().UTC()
	_, _ = a.createAuditLog(ctx, store.CreateCommandAuditLogParams{
		UserID:               userID,
		TargetID:             target.ID,
		OrganizationID:       organizationIDForTarget(target),
		PublicKeyFingerprint: publicKeyFingerprint,
		SessionID:            sessionID,
		Command:              command,
		RequestType:          store.RequestExec,
		PolicyDecision:       decision.Action,
		PolicyReason:         decision.Reason,
		ExitCode:             &exitCode,
		StartedAt:            startedAt,
		EndedAt:              &endedAt,
		RemoteAddress:        sourceIP,
	})
	sendExit(ch, exitCode)
}

func (a *App) handleBastionShell(userID, publicKeyFingerprint string, target store.SSHTarget, ch gossh.Channel, sourceIP string, width, height int) {
	ctx := context.Background()
	sessionID := newAuditSessionID()
	startedAt := time.Now().UTC()
	decision, err := a.bastion.EvaluateAccess(ctx, userID, target.ID, store.RequestShell, sourceIP)
	if err != nil {
		_, _ = ch.Stderr().Write([]byte(err.Error() + "\n"))
		sendExit(ch, 255)
		return
	}
	if decision.Action == store.DecisionDeny {
		_, _ = ch.Stderr().Write([]byte("interactive terminal denied: " + decision.Reason + "\n"))
		code := 126
		endedAt := time.Now().UTC()
		_, _ = a.createAuditLog(ctx, store.CreateCommandAuditLogParams{
			UserID:               userID,
			TargetID:             target.ID,
			OrganizationID:       organizationIDForTarget(target),
			PublicKeyFingerprint: publicKeyFingerprint,
			SessionID:            sessionID,
			Command:              "interactive shell",
			RequestType:          store.RequestShell,
			PolicyDecision:       store.DecisionDeny,
			PolicyReason:         decision.Reason,
			ExitCode:             &code,
			StartedAt:            startedAt,
			EndedAt:              &endedAt,
			RemoteAddress:        sourceIP,
		})
		sendExit(ch, code)
		return
	}
	recorder, err := newTerminalRecorder(a.auditRecordingsPath, sessionID, width, height, target)
	if err != nil {
		_, _ = ch.Stderr().Write([]byte("terminal recording unavailable: " + err.Error() + "\n"))
		sendExit(ch, 255)
		return
	}
	exitCode := a.shellOnTarget(ctx, target, ch, width, height, recorder)
	endedAt := time.Now().UTC()
	a.recordShellAuditAsync(recorder, store.CreateCommandAuditLogParams{
		UserID:               userID,
		TargetID:             target.ID,
		OrganizationID:       organizationIDForTarget(target),
		PublicKeyFingerprint: publicKeyFingerprint,
		SessionID:            sessionID,
		Command:              "interactive shell",
		RequestType:          store.RequestShell,
		PolicyDecision:       decision.Action,
		PolicyReason:         decision.Reason,
		ExitCode:             &exitCode,
		StartedAt:            startedAt,
		EndedAt:              &endedAt,
		RemoteAddress:        sourceIP,
	})
	sendExit(ch, exitCode)
}

func (a *App) recordShellAuditAsync(recorder *terminalRecorder, params store.CreateCommandAuditLogParams) {
	a.backgroundWG.Add(1)
	go func() {
		defer a.backgroundWG.Done()
		meta, err := recorder.Close()
		if err == nil {
			params.RecordingPath = meta.RelativePath
			params.RecordingSize = meta.Size
			params.RecordingSHA256 = meta.SHA256
			params.RecordingDurationMS = meta.DurationMS
			params.RecordingWidth = meta.Width
			params.RecordingHeight = meta.Height
		}
		_, _ = a.createAuditLog(context.Background(), params)
	}()
}

func (a *App) handleBastionSFTP(userID, publicKeyFingerprint string, target store.SSHTarget, ch gossh.Channel, sourceIP string) {
	ctx := context.Background()
	sessionID := newAuditSessionID()
	startedAt := time.Now().UTC()
	decision, allowUpload, allowDownload, err := a.bastion.EvaluateSFTPAccess(ctx, userID, target.ID, sourceIP)
	if err != nil {
		_, _ = ch.Stderr().Write([]byte(err.Error() + "\n"))
		sendExit(ch, 255)
		return
	}
	if decision.Action == store.DecisionDeny {
		code := 126
		endedAt := time.Now().UTC()
		_, _ = a.createAuditLog(ctx, store.CreateCommandAuditLogParams{
			UserID:               userID,
			TargetID:             target.ID,
			OrganizationID:       organizationIDForTarget(target),
			PublicKeyFingerprint: publicKeyFingerprint,
			SessionID:            sessionID,
			Command:              "sftp subsystem",
			RequestType:          store.RequestSFTP,
			PolicyDecision:       store.DecisionDeny,
			PolicyReason:         decision.Reason,
			ExitCode:             &code,
			StartedAt:            startedAt,
			EndedAt:              &endedAt,
			RemoteAddress:        sourceIP,
		})
		sendExit(ch, code)
		return
	}
	exitCode := a.sftpOnTarget(ctx, target, ch, allowUpload, allowDownload)
	endedAt := time.Now().UTC()
	_, _ = a.createAuditLog(ctx, store.CreateCommandAuditLogParams{
		UserID:               userID,
		TargetID:             target.ID,
		OrganizationID:       organizationIDForTarget(target),
		PublicKeyFingerprint: publicKeyFingerprint,
		SessionID:            sessionID,
		Command:              "sftp subsystem",
		RequestType:          store.RequestSFTP,
		PolicyDecision:       decision.Action,
		PolicyReason:         decision.Reason,
		ExitCode:             &exitCode,
		StartedAt:            startedAt,
		EndedAt:              &endedAt,
		RemoteAddress:        sourceIP,
	})
	sendExit(ch, exitCode)
}

func (a *App) execOnTarget(ctx context.Context, target store.SSHTarget, ch gossh.Channel, command string) int {
	if target.TargetType == store.TargetAgent {
		return a.agentFramedSession(target.AgentID, ch, protocol.StreamRequest{
			Type:    protocol.StreamExec,
			Command: command,
			Width:   80,
			Height:  24,
		}, nil)
	}
	client, err := a.openTargetSSHClient(ctx, target)
	if err != nil {
		_, _ = ch.Stderr().Write([]byte(err.Error() + "\n"))
		return 255
	}
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		_, _ = ch.Stderr().Write([]byte(err.Error() + "\n"))
		return 255
	}
	defer session.Close()
	stdout, err := session.StdoutPipe()
	if err != nil {
		_, _ = ch.Stderr().Write([]byte(err.Error() + "\n"))
		return 255
	}
	stderr, err := session.StderrPipe()
	if err != nil {
		_, _ = ch.Stderr().Write([]byte(err.Error() + "\n"))
		return 255
	}
	if err := session.Start(command); err != nil {
		_, _ = ch.Stderr().Write([]byte(err.Error() + "\n"))
		return 255
	}
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(ch, stdout)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(ch.Stderr(), stderr)
		done <- struct{}{}
	}()
	err = session.Wait()
	<-done
	<-done
	if err == nil {
		return 0
	}
	if exit, ok := err.(*gossh.ExitError); ok {
		return exit.ExitStatus()
	}
	return 255
}

func (a *App) shellOnTarget(ctx context.Context, target store.SSHTarget, ch gossh.Channel, width, height int, recorder *terminalRecorder) int {
	if target.TargetType == store.TargetAgent {
		return a.agentFramedSession(target.AgentID, ch, protocol.StreamRequest{
			Type:   protocol.StreamShell,
			Width:  width,
			Height: height,
		}, recorder)
	}
	client, err := a.openTargetSSHClient(ctx, target)
	if err != nil {
		_, _ = ch.Stderr().Write([]byte(err.Error() + "\n"))
		return 255
	}
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		_, _ = ch.Stderr().Write([]byte(err.Error() + "\n"))
		return 255
	}
	defer session.Close()
	stdin, err := session.StdinPipe()
	if err != nil {
		_, _ = ch.Stderr().Write([]byte(err.Error() + "\n"))
		return 255
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		_, _ = ch.Stderr().Write([]byte(err.Error() + "\n"))
		return 255
	}
	stderr, err := session.StderrPipe()
	if err != nil {
		_, _ = ch.Stderr().Write([]byte(err.Error() + "\n"))
		return 255
	}
	if err := session.RequestPty("xterm-256color", height, width, gossh.TerminalModes{}); err != nil {
		_, _ = ch.Stderr().Write([]byte(err.Error() + "\n"))
		return 255
	}
	if err := session.Shell(); err != nil {
		_, _ = ch.Stderr().Write([]byte(err.Error() + "\n"))
		return 255
	}
	var outputWG sync.WaitGroup
	outputWG.Add(2)
	go func() {
		_, _ = io.Copy(stdin, ch)
		_ = closeWriter(stdin)
	}()
	go func() {
		defer outputWG.Done()
		_, _ = copyAndRecord(ch, stdout, recorder)
	}()
	go func() {
		defer outputWG.Done()
		_, _ = copyAndRecord(ch.Stderr(), stderr, recorder)
	}()
	err = session.Wait()
	_ = closeWriter(stdin)
	_ = ch.CloseWrite()
	outputWG.Wait()
	if err == nil {
		return 0
	}
	if exit, ok := err.(*gossh.ExitError); ok {
		return exit.ExitStatus()
	}
	return 255
}

func (a *App) sftpOnTarget(ctx context.Context, target store.SSHTarget, ch gossh.Channel, allowUpload, allowDownload bool) int {
	if target.TargetType == store.TargetAgent {
		return a.agentSFTP(target.AgentID, ch, allowUpload, allowDownload)
	}
	client, err := a.openTargetSSHClient(ctx, target)
	if err != nil {
		_, _ = ch.Stderr().Write([]byte(err.Error() + "\n"))
		return 255
	}
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		_, _ = ch.Stderr().Write([]byte(err.Error() + "\n"))
		return 255
	}
	defer session.Close()
	stdin, err := session.StdinPipe()
	if err != nil {
		_, _ = ch.Stderr().Write([]byte(err.Error() + "\n"))
		return 255
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		_, _ = ch.Stderr().Write([]byte(err.Error() + "\n"))
		return 255
	}
	if err := session.RequestSubsystem("sftp"); err != nil {
		_, _ = ch.Stderr().Write([]byte(err.Error() + "\n"))
		return 255
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = proxySFTPPackets(ch, stdin, allowUpload, allowDownload)
		_ = closeWriter(stdin)
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(ch, stdout)
	}()
	err = session.Wait()
	_ = ch.CloseWrite()
	wg.Wait()
	if err == nil {
		return 0
	}
	if exit, ok := err.(*gossh.ExitError); ok {
		return exit.ExitStatus()
	}
	return 255
}

func (a *App) agentFramedSession(agentID string, ch gossh.Channel, req protocol.StreamRequest, recorder *terminalRecorder) int {
	reader, stream, err := a.openAgentStream(agentID, req)
	if err != nil {
		_, _ = ch.Stderr().Write([]byte(err.Error() + "\n"))
		return 255
	}
	defer stream.Close()

	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := ch.Read(buf)
			if n > 0 {
				if writeErr := protocol.WriteFrame(stream, protocol.Frame{Type: protocol.FrameStdin, Data: append([]byte(nil), buf[:n]...)}); writeErr != nil {
					return
				}
			}
			if err != nil {
				return
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
			if recorder != nil {
				recorder.WriteOutput(frame.Data)
			}
			_, _ = ch.Write(frame.Data)
		case protocol.FrameStderr:
			if recorder != nil {
				recorder.WriteOutput(frame.Data)
			}
			_, _ = ch.Stderr().Write(frame.Data)
		case protocol.FrameExit:
			exitCode = protocol.ExitCode(frame)
			_ = ch.CloseWrite()
			return exitCode
		}
	}
	_ = ch.CloseWrite()
	return exitCode
}

func (a *App) agentSFTP(agentID string, ch gossh.Channel, allowUpload, allowDownload bool) int {
	reader, stream, err := a.openAgentStream(agentID, protocol.StreamRequest{Type: protocol.StreamSFTP})
	if err != nil {
		_, _ = ch.Stderr().Write([]byte(err.Error() + "\n"))
		return 255
	}
	defer stream.Close()

	var wg sync.WaitGroup
	var closeOnce sync.Once
	closeBoth := func() {
		closeOnce.Do(func() {
			_ = ch.Close()
			_ = stream.Close()
		})
	}
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = proxySFTPPackets(ch, stream, allowUpload, allowDownload)
		closeBoth()
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(ch, reader)
		closeBoth()
	}()
	wg.Wait()
	return 0
}

func (a *App) handleBastionDirectTCPIP(userID, publicKeyFingerprint string, target store.SSHTarget, newCh gossh.NewChannel, sourceIP string) {
	var payload directTCPIPPayload
	if err := gossh.Unmarshal(newCh.ExtraData(), &payload); err != nil {
		_ = newCh.Reject(gossh.ConnectionFailed, "invalid direct-tcpip payload")
		return
	}
	ctx := context.Background()
	sessionID := newAuditSessionID()
	startedAt := time.Now().UTC()
	decision, err := a.bastion.EvaluateAccess(ctx, userID, target.ID, store.RequestForward, sourceIP)
	destination := net.JoinHostPort(payload.HostToConnect, strconv.Itoa(int(payload.PortToConnect)))
	if err != nil {
		_ = newCh.Reject(gossh.ConnectionFailed, err.Error())
		return
	}
	if decision.Action == store.DecisionDeny {
		code := 126
		endedAt := time.Now().UTC()
		_, _ = a.createAuditLog(ctx, store.CreateCommandAuditLogParams{
			UserID:               userID,
			TargetID:             target.ID,
			OrganizationID:       organizationIDForTarget(target),
			PublicKeyFingerprint: publicKeyFingerprint,
			SessionID:            sessionID,
			Command:              "direct-tcpip " + destination,
			RequestType:          store.RequestForward,
			PolicyDecision:       store.DecisionDeny,
			PolicyReason:         decision.Reason,
			ExitCode:             &code,
			StartedAt:            startedAt,
			EndedAt:              &endedAt,
			RemoteAddress:        sourceIP,
		})
		_ = newCh.Reject(gossh.Prohibited, decision.Reason)
		return
	}
	ch, reqs, err := newCh.Accept()
	if err != nil {
		return
	}
	go gossh.DiscardRequests(reqs)
	exitCode := 0
	if target.TargetType == store.TargetAgent {
		reader, stream, err := a.openAgentStream(target.AgentID, protocol.StreamRequest{Type: protocol.StreamTCP, Target: destination})
		if err != nil {
			exitCode = 255
			_, _ = ch.Stderr().Write([]byte(err.Error() + "\n"))
			_ = ch.Close()
		} else {
			bridge(ch, struct {
				io.Reader
				io.Writer
				io.Closer
			}{Reader: reader, Writer: stream, Closer: stream})
		}
	} else {
		client, err := a.openTargetSSHClient(ctx, target)
		if err != nil {
			exitCode = 255
			_ = ch.Close()
		} else {
			defer client.Close()
			conn, err := client.Dial("tcp", destination)
			if err != nil {
				exitCode = 255
				_ = ch.Close()
			} else {
				bridge(ch, conn)
			}
		}
	}
	endedAt := time.Now().UTC()
	_, _ = a.createAuditLog(ctx, store.CreateCommandAuditLogParams{
		UserID:               userID,
		TargetID:             target.ID,
		OrganizationID:       organizationIDForTarget(target),
		PublicKeyFingerprint: publicKeyFingerprint,
		SessionID:            sessionID,
		Command:              "direct-tcpip " + destination,
		RequestType:          store.RequestForward,
		PolicyDecision:       decision.Action,
		PolicyReason:         decision.Reason,
		ExitCode:             &exitCode,
		StartedAt:            startedAt,
		EndedAt:              &endedAt,
		RemoteAddress:        sourceIP,
	})
}

func (a *App) handleBastionGlobalRequests(manager *forwardManager, reqs <-chan *gossh.Request, userID, publicKeyFingerprint string, target store.SSHTarget, sourceIP string) {
	for req := range reqs {
		switch req.Type {
		case "tcpip-forward":
			ctx := context.Background()
			sessionID := newAuditSessionID()
			startedAt := time.Now().UTC()
			decision, err := a.bastion.EvaluateAccess(ctx, userID, target.ID, store.RequestForward, sourceIP)
			if err != nil {
				req.Reply(false, nil)
				continue
			}
			if decision.Action == store.DecisionDeny {
				code := 126
				endedAt := time.Now().UTC()
				_, _ = a.createAuditLog(ctx, store.CreateCommandAuditLogParams{
					UserID:               userID,
					TargetID:             target.ID,
					OrganizationID:       organizationIDForTarget(target),
					PublicKeyFingerprint: publicKeyFingerprint,
					SessionID:            sessionID,
					Command:              "tcpip-forward",
					RequestType:          store.RequestForward,
					PolicyDecision:       store.DecisionDeny,
					PolicyReason:         decision.Reason,
					ExitCode:             &code,
					StartedAt:            startedAt,
					EndedAt:              &endedAt,
					RemoteAddress:        sourceIP,
				})
				req.Reply(false, nil)
				continue
			}
			manager.handleTCPIPForward(req)
			endedAt := time.Now().UTC()
			_, _ = a.createAuditLog(ctx, store.CreateCommandAuditLogParams{
				UserID:               userID,
				TargetID:             target.ID,
				OrganizationID:       organizationIDForTarget(target),
				PublicKeyFingerprint: publicKeyFingerprint,
				SessionID:            sessionID,
				Command:              "tcpip-forward",
				RequestType:          store.RequestForward,
				PolicyDecision:       decision.Action,
				PolicyReason:         decision.Reason,
				StartedAt:            startedAt,
				EndedAt:              &endedAt,
				RemoteAddress:        sourceIP,
			})
		case "cancel-tcpip-forward":
			req.Reply(true, nil)
		default:
			req.Reply(false, nil)
		}
	}
}

func (a *App) openTargetSSHClient(ctx context.Context, target store.SSHTarget) (*gossh.Client, error) {
	return a.openTargetSSHClientWithDepth(ctx, target, 0)
}

func (a *App) openTargetSSHClientWithDepth(ctx context.Context, target store.SSHTarget, depth int) (*gossh.Client, error) {
	if depth > 3 {
		return nil, errors.New("ssh proxy chain is too deep")
	}
	auth, err := targetAuthMethod(target)
	if err != nil {
		return nil, err
	}
	cfg := &gossh.ClientConfig{
		User:            target.RemoteUsername,
		Auth:            []gossh.AuthMethod{auth},
		HostKeyCallback: a.targetHostKeyCallback(),
		Timeout:         5 * time.Second,
	}
	addr := net.JoinHostPort(target.Host, fmt.Sprintf("%d", target.Port))
	if target.TargetType == store.TargetDirect {
		if strings.TrimSpace(target.ProxyTargetID) != "" {
			if target.ProxyTargetID == target.ID {
				return nil, errors.New("target cannot use itself as proxy")
			}
			proxyTarget, err := a.store.Repository().GetSSHTarget(ctx, target.ProxyTargetID)
			if err != nil {
				return nil, fmt.Errorf("load proxy target: %w", err)
			}
			proxyClient, err := a.openTargetSSHClientWithDepth(ctx, proxyTarget, depth+1)
			if err != nil {
				return nil, fmt.Errorf("connect proxy target: %w", err)
			}
			conn, err := proxyClient.Dial("tcp", addr)
			if err != nil {
				_ = proxyClient.Close()
				return nil, fmt.Errorf("dial target through proxy: %w", err)
			}
			chained := closeChainConn{Conn: conn, closer: proxyClient}
			clientConn, chans, reqs, err := gossh.NewClientConn(chained, addr, cfg)
			if err != nil {
				_ = chained.Close()
				return nil, err
			}
			return gossh.NewClient(clientConn, chans, reqs), nil
		}
		return gossh.Dial("tcp", addr, cfg)
	}
	if target.TargetType == store.TargetAgent {
		session, err := a.registry.Get(target.AgentID)
		if err != nil {
			return nil, err
		}
		stream, err := session.Open()
		if err != nil {
			return nil, err
		}
		if err := protocol.WriteJSONLine(stream, protocol.StreamRequest{Type: protocol.StreamTCP, Target: addr}); err != nil {
			_ = stream.Close()
			return nil, err
		}
		reader := bufio.NewReader(stream)
		resp, err := protocol.ReadJSONLine[protocol.StreamResponse](reader)
		if err != nil {
			_ = stream.Close()
			return nil, err
		}
		if !resp.OK {
			_ = stream.Close()
			return nil, errors.New(resp.Error)
		}
		conn, chans, reqs, err := gossh.NewClientConn(readWriteConn{Reader: reader, Writer: stream, Closer: stream}, addr, cfg)
		if err != nil {
			_ = stream.Close()
			return nil, err
		}
		return gossh.NewClient(conn, chans, reqs), nil
	}
	return nil, fmt.Errorf("unsupported target type %q", target.TargetType)
}

type closeChainConn struct {
	net.Conn
	closer io.Closer
}

func (c closeChainConn) Close() error {
	err := c.Conn.Close()
	if c.closer != nil {
		if closeErr := c.closer.Close(); err == nil {
			err = closeErr
		}
	}
	return err
}

func targetAuthMethod(target store.SSHTarget) (gossh.AuthMethod, error) {
	switch target.AuthType {
	case store.AuthPrivateKey:
		signer, err := gossh.ParsePrivateKey(target.EncryptedSecret)
		if err != nil {
			return nil, fmt.Errorf("parse target private key: %w", err)
		}
		return gossh.PublicKeys(signer), nil
	case store.AuthPassword, "":
		return gossh.Password(string(target.EncryptedSecret)), nil
	default:
		return nil, fmt.Errorf("unsupported target auth type %q", target.AuthType)
	}
}

func organizationIDForTarget(target store.SSHTarget) string {
	if target.OwnerType == store.OwnerOrganization {
		return target.OwnerID
	}
	return ""
}

func newAuditSessionID() string {
	return strings.ReplaceAll(time.Now().UTC().Format("20060102150405.000000000"), ".", "")
}

func copyAndRecord(dst io.Writer, src io.Reader, recorder *terminalRecorder) (int64, error) {
	buf := make([]byte, 32*1024)
	var written int64
	for {
		n, readErr := src.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			recorder.WriteOutput(chunk)
			m, writeErr := dst.Write(chunk)
			written += int64(m)
			if writeErr != nil {
				return written, writeErr
			}
			if m != n {
				return written, io.ErrShortWrite
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				return written, nil
			}
			return written, readErr
		}
	}
}

const (
	sftpPacketInit       = 1
	sftpPacketOpen       = 3
	sftpPacketRead       = 5
	sftpPacketWrite      = 6
	sftpPacketSetstat    = 9
	sftpPacketFsetstat   = 10
	sftpPacketOpendir    = 11
	sftpPacketReaddir    = 12
	sftpPacketRemove     = 13
	sftpPacketMkdir      = 14
	sftpPacketRmdir      = 15
	sftpPacketRename     = 18
	sftpPacketSymlink    = 20
	sftpPacketExtended   = 200
	sftpStatus           = 101
	sftpPermissionDenied = 3
	sftpOpenRead         = 0x00000001
	sftpOpenWrite        = 0x00000002
	sftpOpenAppend       = 0x00000004
	sftpOpenCreat        = 0x00000008
	sftpOpenTrunc        = 0x00000010
	sftpOpenExcl         = 0x00000020
)

func proxySFTPPackets(client io.ReadWriter, target io.Writer, allowUpload, allowDownload bool) error {
	for {
		packet, err := readSFTPPacket(client)
		if err != nil {
			return err
		}
		if ok, id := sftpPacketAllowed(packet, allowUpload, allowDownload); !ok {
			if id != 0 {
				_, _ = client.Write(sftpStatusPacket(id, sftpPermissionDenied, "blocked by bastion policy"))
			}
			continue
		}
		if _, err := target.Write(packet); err != nil {
			return err
		}
	}
}

func readSFTPPacket(r io.Reader) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	length := binary.BigEndian.Uint32(header[:])
	if length == 0 || length > 32*1024*1024 {
		return nil, fmt.Errorf("invalid sftp packet length %d", length)
	}
	packet := make([]byte, 4+length)
	copy(packet[:4], header[:])
	_, err := io.ReadFull(r, packet[4:])
	return packet, err
}

func sftpPacketAllowed(packet []byte, allowUpload, allowDownload bool) (bool, uint32) {
	if len(packet) < 5 {
		return true, 0
	}
	packetType := packet[4]
	if packetType == sftpPacketInit {
		return true, 0
	}
	id := sftpPacketID(packet)
	switch packetType {
	case sftpPacketOpen:
		read, write := sftpOpenDirections(packet)
		if read && !allowDownload {
			return false, id
		}
		if write && !allowUpload {
			return false, id
		}
	case sftpPacketRead, sftpPacketOpendir, sftpPacketReaddir:
		if !allowDownload {
			return false, id
		}
	case sftpPacketWrite, sftpPacketSetstat, sftpPacketFsetstat, sftpPacketRemove,
		sftpPacketMkdir, sftpPacketRmdir, sftpPacketRename, sftpPacketSymlink, sftpPacketExtended:
		if !allowUpload {
			return false, id
		}
	}
	return true, id
}

func sftpPacketID(packet []byte) uint32 {
	if len(packet) < 9 {
		return 0
	}
	return binary.BigEndian.Uint32(packet[5:9])
}

func sftpOpenDirections(packet []byte) (bool, bool) {
	if len(packet) < 13 {
		return true, true
	}
	rest := packet[9:]
	if len(rest) < 4 {
		return true, true
	}
	pathLen := int(binary.BigEndian.Uint32(rest[:4]))
	if pathLen < 0 || len(rest) < 4+pathLen+4 {
		return true, true
	}
	flags := binary.BigEndian.Uint32(rest[4+pathLen : 4+pathLen+4])
	read := flags&sftpOpenRead != 0
	write := flags&(sftpOpenWrite|sftpOpenAppend|sftpOpenCreat|sftpOpenTrunc|sftpOpenExcl) != 0
	if !read && !write {
		read = true
	}
	return read, write
}

func sftpStatusPacket(id uint32, code uint32, message string) []byte {
	messageBytes := []byte(message)
	payloadLen := 1 + 4 + 4 + 4 + len(messageBytes) + 4
	packet := make([]byte, 4+payloadLen)
	binary.BigEndian.PutUint32(packet[:4], uint32(payloadLen))
	packet[4] = sftpStatus
	binary.BigEndian.PutUint32(packet[5:9], id)
	binary.BigEndian.PutUint32(packet[9:13], code)
	binary.BigEndian.PutUint32(packet[13:17], uint32(len(messageBytes)))
	copy(packet[17:17+len(messageBytes)], messageBytes)
	binary.BigEndian.PutUint32(packet[17+len(messageBytes):], 0)
	return packet
}

func sshSourceIP(addr net.Addr) string {
	if addr == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err == nil {
		return strings.Trim(host, "[]")
	}
	return strings.Trim(addr.String(), "[]")
}

type readWriteConn struct {
	io.Reader
	io.Writer
	io.Closer
}

func (c readWriteConn) LocalAddr() net.Addr              { return dummyAddr("local") }
func (c readWriteConn) RemoteAddr() net.Addr             { return dummyAddr("remote") }
func (c readWriteConn) SetDeadline(time.Time) error      { return nil }
func (c readWriteConn) SetReadDeadline(time.Time) error  { return nil }
func (c readWriteConn) SetWriteDeadline(time.Time) error { return nil }

type dummyAddr string

func (a dummyAddr) Network() string { return string(a) }
func (a dummyAddr) String() string  { return string(a) }
