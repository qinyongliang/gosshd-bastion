package server

import (
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/qinyongliang/gosshd/internal/store"

	gossh "golang.org/x/crypto/ssh"
)

func (a *App) handleBastionSSHConn(conn *gossh.ServerConn, chans <-chan gossh.NewChannel, reqs <-chan *gossh.Request, userID string) {
	go gossh.DiscardRequests(reqs)
	alias := conn.User()
	target, err := a.resolveBastionTarget(context.Background(), userID, alias)
	if err != nil {
		for ch := range chans {
			_ = ch.Reject(gossh.ConnectionFailed, err.Error())
		}
		return
	}
	for ch := range chans {
		if ch.ChannelType() != "session" {
			_ = ch.Reject(gossh.UnknownChannelType, "unsupported channel type")
			continue
		}
		go a.handleBastionSession(userID, target, ch)
	}
}

func (a *App) resolveBastionTarget(ctx context.Context, userID, alias string) (store.SSHTarget, error) {
	if err := a.ensureServices(ctx); err != nil {
		return store.SSHTarget{}, err
	}
	if target, err := a.store.Repository().ResolveUserTarget(ctx, userID, alias); err == nil {
		return target, nil
	} else if !isNotFound(err) {
		return store.SSHTarget{}, err
	}
	orgs, err := a.store.Repository().ListOrganizationsForUser(ctx, userID)
	if err != nil {
		return store.SSHTarget{}, err
	}
	var matches []store.SSHTarget
	for _, org := range orgs {
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

func (a *App) handleBastionSession(userID string, target store.SSHTarget, newCh gossh.NewChannel) {
	ch, reqs, err := newCh.Accept()
	if err != nil {
		return
	}
	defer ch.Close()
	started := false
	for req := range reqs {
		switch req.Type {
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
			a.handleBastionExec(userID, target, ch, payload.Command)
			return
		default:
			req.Reply(false, nil)
		}
	}
}

func (a *App) handleBastionExec(userID string, target store.SSHTarget, ch gossh.Channel, command string) {
	ctx := context.Background()
	decision, err := a.bastion.EvaluateCommand(ctx, userID, target.ID, command)
	if err != nil {
		_, _ = ch.Stderr().Write([]byte(err.Error() + "\n"))
		sendExit(ch, 255)
		return
	}
	if decision.Action == store.DecisionDeny {
		_, _ = ch.Stderr().Write([]byte("command denied: " + decision.Reason + "\n"))
		code := 126
		_, _ = a.store.Repository().CreateCommandAuditLog(ctx, store.CreateCommandAuditLogParams{
			UserID:         userID,
			TargetID:       target.ID,
			OrganizationID: organizationIDForTarget(target),
			SessionID:      newAuditSessionID(),
			Command:        command,
			RequestType:    store.RequestExec,
			PolicyDecision: store.DecisionDeny,
			PolicyReason:   decision.Reason,
			ExitCode:       &code,
			RemoteAddress:  "",
		})
		sendExit(ch, code)
		return
	}
	exitCode := a.execOnTarget(ctx, target, ch, command)
	_, _ = a.store.Repository().CreateCommandAuditLog(ctx, store.CreateCommandAuditLogParams{
		UserID:         userID,
		TargetID:       target.ID,
		OrganizationID: organizationIDForTarget(target),
		SessionID:      newAuditSessionID(),
		Command:        command,
		RequestType:    store.RequestExec,
		PolicyDecision: decision.Action,
		PolicyReason:   decision.Reason,
		ExitCode:       &exitCode,
		RemoteAddress:  "",
	})
	sendExit(ch, exitCode)
}

func (a *App) execOnTarget(ctx context.Context, target store.SSHTarget, ch gossh.Channel, command string) int {
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

func (a *App) openTargetSSHClient(ctx context.Context, target store.SSHTarget) (*gossh.Client, error) {
	if target.TargetType != store.TargetDirect {
		return nil, fmt.Errorf("unsupported target type %q", target.TargetType)
	}
	auth := gossh.Password(string(target.EncryptedSecret))
	if len(target.EncryptedSecret) == 0 {
		auth = gossh.Password("")
	}
	cfg := &gossh.ClientConfig{
		User:            target.RemoteUsername,
		Auth:            []gossh.AuthMethod{auth},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}
	addr := net.JoinHostPort(target.Host, fmt.Sprintf("%d", target.Port))
	return gossh.Dial("tcp", addr, cfg)
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
