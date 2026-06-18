package server

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
	"github.com/qinyongliang/gosshd-bastion/internal/store"

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
	auth, err := targetAuthMethod(target)
	if err != nil {
		return nil, err
	}
	cfg := &gossh.ClientConfig{
		User:            target.RemoteUsername,
		Auth:            []gossh.AuthMethod{auth},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}
	addr := net.JoinHostPort(target.Host, fmt.Sprintf("%d", target.Port))
	if target.TargetType == store.TargetDirect {
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
