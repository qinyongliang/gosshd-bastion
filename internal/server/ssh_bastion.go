package server

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/qinyongliang/gosshd-bastion/internal/bastion"
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
		log.Printf("bastion session request: target=%s alias=%s type=%s started=%t", target.ID, target.Alias, req.Type, started)
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
	if run, routedSessionID, routedThroughTerminal := a.tryExecInOpenTerminalSession(ctx, userID, target, command, sourceIP, startedAt); routedThroughTerminal {
		if routedSessionID != "" {
			sessionID = routedSessionID
		}
		if run.Err != nil {
			_, _ = ch.Stderr().Write([]byte(run.Err.Error() + "\n"))
		}
		if run.Output != "" {
			_, _ = ch.Write([]byte(run.Output))
		}
		endedAt := run.EndedAt
		if endedAt.IsZero() {
			endedAt = time.Now().UTC()
		}
		_, _ = a.createAuditLog(ctx, store.CreateCommandAuditLogParams{
			UserID:               userID,
			TargetID:             target.ID,
			OrganizationID:       organizationIDForTarget(target),
			PublicKeyFingerprint: publicKeyFingerprint,
			SessionID:            sessionID,
			Command:              command,
			RequestType:          store.RequestExec,
			PolicyDecision:       run.Decision.Action,
			PolicyReason:         run.Decision.Reason,
			ExitCode:             &run.ExitCode,
			StartedAt:            startedAt,
			EndedAt:              &endedAt,
			RemoteAddress:        sourceIP,
		})
		sendExit(ch, run.ExitCode)
		return
	}
	decision, err := a.bastion.EvaluateCommandForSource(ctx, userID, target.ID, command, sourceIP)
	if err != nil {
		_, _ = ch.Stderr().Write([]byte(err.Error() + "\n"))
		sendExit(ch, 255)
		return
	}
	if decision.Action == store.DecisionDeny && decision.AllowManualReview {
		_, _ = ch.Stderr().Write([]byte("command pending manual review: " + decision.Reason + "\n"))
		decision = a.reviewDeniedCommand(ctx, userID, target, command, decision)
		if decision.Action == store.DecisionAllow {
			_, _ = ch.Stderr().Write([]byte("manual review approved\n"))
		}
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

func (a *App) tryExecInOpenTerminalSession(ctx context.Context, userID string, target store.SSHTarget, command, sourceIP string, startedAt time.Time) (terminalSessionCommandRun, string, bool) {
	lookup := a.terminalSessions.earliestOnlineForUserTargetWithDiagnostics(userID, target.ID)
	session := lookup.Session
	if session == nil {
		log.Printf("ssh exec terminal route miss: user=%s target=%s alias=%s command=%q sessions=%s", userID, target.ID, target.Alias, command, summarizeTerminalRouteSnapshots(lookup.Snapshots))
		return terminalSessionCommandRun{}, "", false
	}
	run := a.runCommandInTerminalSession(ctx, session, command, terminalSessionCommandOptions{
		UserID:      userID,
		StartedAt:   startedAt,
		SourceIP:    sourceIP,
		NonBlocking: true,
	})
	if !run.Routed {
		log.Printf("ssh exec terminal route fallback: user=%s target=%s alias=%s session=%s command=%q err=%v", userID, target.ID, target.Alias, session.id, command, run.Err)
		return terminalSessionCommandRun{}, "", false
	}
	log.Printf("ssh exec terminal route hit: user=%s target=%s alias=%s session=%s command=%q exit=%d err=%v", userID, target.ID, target.Alias, session.id, command, run.ExitCode, run.Err)
	return run, session.id, true
}

func summarizeTerminalRouteSnapshots(snapshots []terminalSessionRouteSnapshot) string {
	if len(snapshots) == 0 {
		return "none"
	}
	const maxSnapshots = 8
	capacity := len(snapshots)
	if capacity > maxSnapshots {
		capacity = maxSnapshots
	}
	parts := make([]string, 0, capacity)
	for i, snapshot := range snapshots {
		if i >= maxSnapshots {
			parts = append(parts, "more="+strconv.Itoa(len(snapshots)-maxSnapshots))
			break
		}
		age := time.Since(snapshot.LastHeartbeat).Round(time.Second)
		parts = append(parts, fmt.Sprintf("%s:%s target=%s alias=%s clients=%d input=%t closed=%t busy=%t hb=%s",
			snapshot.ID,
			snapshot.Reason,
			snapshot.TargetID,
			snapshot.TargetAlias,
			snapshot.ClientCount,
			snapshot.InputReady,
			snapshot.Closed,
			snapshot.ShellBusy,
			age,
		))
	}
	return strings.Join(parts, "; ")
}

type terminalSessionCommandOptions struct {
	UserID           string
	Decision         bastion.Decision
	StartedAt        time.Time
	SourceIP         string
	NonBlocking      bool
	SkipPolicyReview bool
	WaitTimeout      time.Duration
}

type terminalSessionCommandRun struct {
	Routed    bool
	Allowed   bool
	Decision  bastion.Decision
	Output    string
	ExitCode  int
	StartedAt time.Time
	EndedAt   time.Time
	Err       error
}

func (a *App) runCommandInTerminalSession(ctx context.Context, session *terminalSession, command string, opts terminalSessionCommandOptions) terminalSessionCommandRun {
	startedAt := opts.StartedAt
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}
	run := terminalSessionCommandRun{
		Routed:    true,
		Decision:  opts.Decision,
		StartedAt: startedAt,
	}
	if run.Decision.Action == "" {
		run.Decision.Action = store.DecisionAllow
	}
	normalizedCommand, err := normalizeTerminalCommand(command)
	if err != nil {
		run.ExitCode = 255
		run.EndedAt = time.Now().UTC()
		run.Err = err
		return run
	}
	unlockCommand, acquired := session.lockCommand(opts.NonBlocking)
	if !acquired {
		run.Routed = false
		return run
	}
	defer unlockCommand()
	if err := session.commandReadinessError(); err != nil {
		run.EndedAt = time.Now().UTC()
		if opts.NonBlocking && terminalCommandUnavailableForRouting(err) {
			run.Routed = false
			return run
		}
		run.ExitCode = 255
		run.Err = err
		return run
	}
	if !opts.SkipPolicyReview {
		sourceIP := opts.SourceIP
		if strings.TrimSpace(sourceIP) == "" {
			sourceIP = session.sourceIP
		}
		decision, err := a.bastion.EvaluateCommandForSource(ctx, opts.UserID, session.target.ID, normalizedCommand, sourceIP)
		if err != nil {
			run.ExitCode = 255
			run.EndedAt = time.Now().UTC()
			run.Err = err
			return run
		}
		if decision.Action == store.DecisionDeny && decision.AllowManualReview {
			decision = a.reviewDeniedCommandForSession(ctx, opts.UserID, session.target, normalizedCommand, decision, session.id)
		}
		run.Decision = decision
	}
	if run.Decision.Action == store.DecisionDeny {
		run.Allowed = false
		run.Output = "command denied: " + run.Decision.Reason + "\r\n"
		run.ExitCode = 126
		run.EndedAt = time.Now().UTC()
		session.writeOutput("error", []byte(run.Output))
		return run
	}

	waitCtx := ctx
	if timeout := terminalSessionCommandWaitTimeout(opts); timeout > 0 {
		var cancel context.CancelFunc
		waitCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	result, sent, err := session.trySendCommandLocked(waitCtx, normalizedCommand)
	run.Allowed = true
	run.Output = result.Output
	run.ExitCode = result.ExitCode
	run.EndedAt = time.Now().UTC()
	if err != nil {
		if !sent && opts.NonBlocking && terminalCommandUnavailableForRouting(err) {
			run.Routed = false
			return run
		}
		run.Err = err
	}
	return run
}

func terminalSessionCommandWaitTimeout(opts terminalSessionCommandOptions) time.Duration {
	if opts.WaitTimeout > 0 {
		return opts.WaitTimeout
	}
	return 30 * time.Minute
}

func terminalCommandUnavailableForRouting(err error) bool {
	return errors.Is(err, errTerminalSessionBusy) || errors.Is(err, errTerminalSessionInputWait) || errors.Is(err, errTerminalSessionClosed)
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

func (a *App) completeShellAuditAsync(recorder *terminalRecorder, auditLogID string, exitCode int, endedAt time.Time) {
	a.backgroundWG.Add(1)
	go func() {
		defer a.backgroundWG.Done()
		params := store.CompleteCommandAuditLogParams{
			ID:       auditLogID,
			ExitCode: &exitCode,
			EndedAt:  endedAt,
		}
		meta, err := recorder.Close()
		if err == nil {
			params.RecordingPath = meta.RelativePath
			params.RecordingSize = meta.Size
			params.RecordingSHA256 = meta.SHA256
			params.RecordingDurationMS = meta.DurationMS
			params.RecordingWidth = meta.Width
			params.RecordingHeight = meta.Height
		}
		_ = a.audit.Repository().CompleteCommandAuditLog(context.Background(), params)
	}()
}

func (a *App) handleBastionSFTP(userID, publicKeyFingerprint string, target store.SSHTarget, ch gossh.Channel, sourceIP string) {
	ctx := context.Background()
	sessionID := newAuditSessionID()
	startedAt := time.Now().UTC()
	log.Printf("sftp request started: target=%s alias=%s type=%s source=%s", target.ID, target.Alias, target.TargetType, sourceIP)
	decision, allowUpload, allowDownload, err := a.bastion.EvaluateSFTPAccess(ctx, userID, target.ID, sourceIP)
	if err != nil {
		log.Printf("sftp access evaluation failed: target=%s alias=%s err=%v", target.ID, target.Alias, err)
		_, _ = ch.Stderr().Write([]byte(err.Error() + "\n"))
		sendExit(ch, 255)
		return
	}
	log.Printf("sftp access decision: target=%s alias=%s decision=%s upload=%t download=%t reason=%q", target.ID, target.Alias, decision.Action, allowUpload, allowDownload, decision.Reason)
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
	log.Printf("sftp request finished: target=%s alias=%s exit=%d", target.ID, target.Alias, exitCode)
	endedAt := time.Now().UTC()
	if _, err := a.createAuditLog(ctx, store.CreateCommandAuditLogParams{
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
	}); err != nil {
		log.Printf("sftp audit create failed: target=%s alias=%s exit=%d err=%v", target.ID, target.Alias, exitCode, err)
	}
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

	initialPacket, err := readSFTPPacket(ch)
	if err != nil {
		log.Printf("sftp initial packet failed: target=%s alias=%s err=%v", target.ID, target.Alias, err)
		return 255
	}
	if ok, id := sftpPacketAllowed(initialPacket, allowUpload, allowDownload); !ok {
		if id != 0 {
			_, _ = ch.Write(sftpStatusPacket(id, sftpPermissionDenied, "blocked by bastion policy"))
		}
		return 126
	}

	startSubsystem := func() (*targetSFTPRunner, error) {
		return newTargetSFTPSubsystemRunner(client)
	}
	startFallback := func() (*targetSFTPRunner, error) {
		return newTargetSFTPExecRunner(client, directSFTPServerCommand)
	}

	runner, err := startSubsystem()
	usingSubsystem := true
	if err != nil {
		log.Printf("sftp subsystem start failed, falling back: target=%s alias=%s err=%v", target.ID, target.Alias, err)
		usingSubsystem = false
		runner, err = startFallback()
		if err != nil {
			log.Printf("sftp fallback start failed: target=%s alias=%s err=%v", target.ID, target.Alias, err)
			_, _ = ch.Stderr().Write([]byte(err.Error() + "\n"))
			return 255
		}
	}
	firstResponse, err := runner.exchangeInitialSFTPPacket(initialPacket, 5*time.Second)
	if err != nil && usingSubsystem {
		log.Printf("sftp subsystem handshake failed, falling back: target=%s alias=%s err=%v", target.ID, target.Alias, err)
		_ = runner.close()
		runner, err = startFallback()
		if err != nil {
			log.Printf("sftp fallback start failed after handshake: target=%s alias=%s err=%v", target.ID, target.Alias, err)
			_, _ = ch.Stderr().Write([]byte(err.Error() + "\n"))
			return 255
		}
		firstResponse, err = runner.exchangeInitialSFTPPacket(initialPacket, 5*time.Second)
	}
	if err != nil {
		_ = runner.close()
		log.Printf("sftp handshake failed: target=%s alias=%s err=%v", target.ID, target.Alias, err)
		_, _ = ch.Stderr().Write([]byte(err.Error() + "\n"))
		return 255
	}
	if _, err := ch.Write(firstResponse); err != nil {
		_ = runner.close()
		log.Printf("sftp initial response write failed: target=%s alias=%s err=%v", target.ID, target.Alias, err)
		return 255
	}
	defer runner.close()

	var closeOnce sync.Once
	closeRunner := func() {
		closeOnce.Do(func() {
			_ = runner.close()
		})
	}
	go func() {
		_ = proxySFTPPackets(ch, runner.stdin, allowUpload, allowDownload)
		closeRunner()
	}()

	var outputWG sync.WaitGroup
	outputWG.Add(2)
	go func() {
		defer outputWG.Done()
		_, _ = io.Copy(ch, runner.stdout)
		closeRunner()
	}()
	go func() {
		defer outputWG.Done()
		_, _ = io.Copy(ch.Stderr(), runner.stderr)
	}()
	result := <-runner.wait
	closeRunner()
	outputDone := make(chan struct{})
	go func() {
		outputWG.Wait()
		close(outputDone)
	}()
	select {
	case <-outputDone:
	case <-time.After(5 * time.Second):
		log.Printf("sftp output drain timed out: target=%s alias=%s", target.ID, target.Alias)
	}
	if result.err != nil {
		log.Printf("sftp remote failed: target=%s alias=%s exit=%d err=%v", target.ID, target.Alias, result.exitCode, result.err)
	}
	return result.exitCode
}

type targetSFTPRunner struct {
	channel gossh.Channel
	stdin   io.WriteCloser
	stdout  io.Reader
	stderr  io.Reader
	wait    chan targetSFTPResult
}

type targetSFTPResult struct {
	exitCode int
	err      error
}

func newTargetSFTPSubsystemRunner(client *gossh.Client) (*targetSFTPRunner, error) {
	var payload struct {
		Subsystem string
	}
	payload.Subsystem = "sftp"
	return newTargetSFTPRunner(client, "subsystem", gossh.Marshal(&payload))
}

func newTargetSFTPExecRunner(client *gossh.Client, command string) (*targetSFTPRunner, error) {
	var payload struct {
		Command string
	}
	payload.Command = command
	return newTargetSFTPRunner(client, "exec", gossh.Marshal(&payload))
}

func newTargetSFTPRunner(client *gossh.Client, requestType string, payload []byte) (*targetSFTPRunner, error) {
	channel, reqs, err := client.OpenChannel("session", nil)
	if err != nil {
		return nil, err
	}
	ok, err := channel.SendRequest(requestType, true, payload)
	if err == nil && !ok {
		err = fmt.Errorf("ssh: %s request failed", requestType)
	}
	if err != nil {
		_ = channel.Close()
		return nil, err
	}
	runner := &targetSFTPRunner{
		channel: channel,
		stdin:   channel,
		stdout:  channel,
		stderr:  channel.Stderr(),
		wait:    make(chan targetSFTPResult, 1),
	}
	go runner.waitForExit(reqs)
	return runner, nil
}

func (r *targetSFTPRunner) waitForExit(reqs <-chan *gossh.Request) {
	result := targetSFTPResult{exitCode: 0}
	for req := range reqs {
		switch req.Type {
		case "exit-status":
			if len(req.Payload) < 4 {
				result.exitCode = 255
				result.err = errors.New("malformed sftp exit status")
				continue
			}
			result.exitCode = int(binary.BigEndian.Uint32(req.Payload))
		case "exit-signal":
			result.exitCode = 255
			result.err = fmt.Errorf("sftp exited with signal")
		}
	}
	r.wait <- result
}

func (r *targetSFTPRunner) exchangeInitialSFTPPacket(packet []byte, timeout time.Duration) ([]byte, error) {
	errCh := make(chan error, 1)
	go func() {
		_, err := r.stdin.Write(packet)
		errCh <- err
	}()
	select {
	case err := <-errCh:
		if err != nil {
			return nil, err
		}
	case result := <-r.wait:
		r.wait <- result
		if result.err != nil {
			return nil, result.err
		}
		if result.exitCode != 0 {
			return nil, fmt.Errorf("sftp exited before handshake with status %d", result.exitCode)
		}
		return nil, io.ErrUnexpectedEOF
	case <-time.After(timeout):
		return nil, fmt.Errorf("sftp handshake timed out")
	}

	packetCh := make(chan []byte, 1)
	go func() {
		packet, err := readSFTPPacket(r.stdout)
		if err != nil {
			errCh <- err
			return
		}
		packetCh <- packet
	}()
	select {
	case packet := <-packetCh:
		return packet, nil
	case err := <-errCh:
		return nil, err
	case result := <-r.wait:
		r.wait <- result
		if result.err != nil {
			return nil, result.err
		}
		if result.exitCode != 0 {
			return nil, fmt.Errorf("sftp exited before handshake with status %d", result.exitCode)
		}
		return nil, io.ErrUnexpectedEOF
	case <-time.After(timeout):
		return nil, fmt.Errorf("sftp handshake timed out")
	}
}

func (r *targetSFTPRunner) close() error {
	_ = closeWriter(r.stdin)
	return r.channel.Close()
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

	var closeOnce sync.Once
	closeStream := func() {
		closeOnce.Do(func() {
			_ = stream.Close()
		})
	}
	go func() {
		_ = proxySFTPPackets(ch, stream, allowUpload, allowDownload)
		closeStream()
	}()
	outputDone := make(chan struct{})
	go func() {
		defer close(outputDone)
		_, _ = io.Copy(ch, reader)
		closeStream()
	}()
	<-outputDone
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
	auth, err := targetAuthMethods(target)
	if err != nil {
		return nil, err
	}
	cfg := &gossh.ClientConfig{
		User:            target.RemoteUsername,
		Auth:            auth,
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
			if proxyTarget.TargetType == store.TargetAgent {
				conn, err := a.openAgentTCPConn(proxyTarget.AgentID, addr)
				if err != nil {
					return nil, fmt.Errorf("connect proxy target: %w", err)
				}
				clientConn, chans, reqs, err := gossh.NewClientConn(conn, addr, cfg)
				if err != nil {
					_ = conn.Close()
					return nil, err
				}
				return gossh.NewClient(clientConn, chans, reqs), nil
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
		tcpConn, err := a.openAgentTCPConn(target.AgentID, addr)
		if err != nil {
			return nil, err
		}
		conn, chans, reqs, err := gossh.NewClientConn(tcpConn, addr, cfg)
		if err != nil {
			_ = tcpConn.Close()
			return nil, err
		}
		return gossh.NewClient(conn, chans, reqs), nil
	}
	return nil, fmt.Errorf("unsupported target type %q", target.TargetType)
}

func (a *App) openAgentTCPConn(agentID, addr string) (net.Conn, error) {
	session, err := a.registry.Get(agentID)
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
	return readWriteConn{Reader: reader, Writer: stream, Closer: stream, remoteAddr: dummyAddr(addr)}, nil
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

func targetAuthMethods(target store.SSHTarget) ([]gossh.AuthMethod, error) {
	switch target.AuthType {
	case store.AuthPrivateKey:
		signer, err := gossh.ParsePrivateKey(target.EncryptedSecret)
		if err != nil {
			return nil, fmt.Errorf("parse target private key: %w", err)
		}
		return []gossh.AuthMethod{gossh.PublicKeys(signer)}, nil
	case store.AuthPassword, "":
		password := string(target.EncryptedSecret)
		return []gossh.AuthMethod{
			gossh.Password(password),
			gossh.KeyboardInteractive(func(user, instruction string, questions []string, echos []bool) ([]string, error) {
				answers := make([]string, len(questions))
				for i := range answers {
					answers[i] = password
				}
				return answers, nil
			}),
		}, nil
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

const directSFTPServerCommand = `sh -c 'for p in /usr/lib/openssh/sftp-server /usr/libexec/sftp-server /usr/lib/ssh/sftp-server; do if [ -x "$p" ]; then exec "$p"; fi; done; exec sftp-server'`

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
		sftpPacketMkdir, sftpPacketRmdir, sftpPacketRename, sftpPacketSymlink:
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
	remoteAddr net.Addr
}

func (c readWriteConn) LocalAddr() net.Addr { return dummyAddr("local:0") }
func (c readWriteConn) RemoteAddr() net.Addr {
	if c.remoteAddr != nil {
		return c.remoteAddr
	}
	return dummyAddr("remote:0")
}
func (c readWriteConn) SetDeadline(time.Time) error      { return nil }
func (c readWriteConn) SetReadDeadline(time.Time) error  { return nil }
func (c readWriteConn) SetWriteDeadline(time.Time) error { return nil }

type dummyAddr string

func (a dummyAddr) Network() string { return string(a) }
func (a dummyAddr) String() string  { return string(a) }
