package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"github.com/qinyongliang/gosshd-bastion/internal/agent"
	"github.com/qinyongliang/gosshd-bastion/internal/bastion"
	"github.com/qinyongliang/gosshd-bastion/internal/store"

	gossh "golang.org/x/crypto/ssh"
)

func TestSSHRejectsUnknownPublicKey(t *testing.T) {
	app, _, sshAddr, stop := startBastionTestApp(t)
	defer stop()
	_ = app

	_, err := gossh.Dial("tcp", sshAddr, &gossh.ClientConfig{
		User:            "test2",
		Auth:            []gossh.AuthMethod{gossh.PublicKeys(testSSHSigner(t))},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	})
	if err == nil {
		t.Fatalf("expected unknown key to be rejected")
	}
}

func TestSSHExecRoutesAliasToDirectTarget(t *testing.T) {
	app, httpAddr, sshAddr, stop := startBastionTestApp(t)
	defer stop()
	_ = httpAddr
	ctx := context.Background()
	userSigner := testSSHSigner(t)
	user := seedBastionUserWithKey(t, app, userSigner)
	targetAddr, closeTarget := startTestSSHServer(t)
	defer closeTarget()
	host, portText, _ := net.SplitHostPort(targetAddr)
	port := mustAtoi(t, portText)
	personal, err := app.store.Repository().GetPersonalOrganizationForUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	target, err := app.store.Repository().CreateSSHTarget(ctx, store.CreateSSHTargetParams{
		OwnerType:      store.OwnerOrganization,
		OwnerID:        personal.ID,
		Alias:          "test2",
		TargetType:     store.TargetDirect,
		Host:           host,
		Port:           port,
		RemoteUsername: "remote",
		AuthType:       store.AuthPassword,
		CreatedBy:      user.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	attachAllowPolicyForTarget(t, app, personal.ID, target.ID, false)

	out, err := runBastionSSHCommand(sshAddr, "test2", userSigner, "whoami")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "remote" {
		t.Fatalf("unexpected output %q", out)
	}
}

func TestSSHExecRoutesAliasToPrivateKeyTarget(t *testing.T) {
	app, _, sshAddr, stop := startBastionTestApp(t)
	defer stop()
	ctx := context.Background()
	userSigner := testSSHSigner(t)
	targetSigner, targetPrivateKey := testSSHSignerWithPrivateKey(t)
	user := seedBastionUserWithKey(t, app, userSigner)
	targetAddr, closeTarget := startTestSSHServerWithAuthorizedKey(t, targetSigner.PublicKey())
	defer closeTarget()
	host, portText, _ := net.SplitHostPort(targetAddr)
	port := mustAtoi(t, portText)
	personal, err := app.store.Repository().GetPersonalOrganizationForUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	target, err := app.store.Repository().CreateSSHTarget(ctx, store.CreateSSHTargetParams{
		OwnerType:       store.OwnerOrganization,
		OwnerID:         personal.ID,
		Alias:           "private-box",
		TargetType:      store.TargetDirect,
		Host:            host,
		Port:            port,
		RemoteUsername:  "remote",
		AuthType:        store.AuthPrivateKey,
		EncryptedSecret: targetPrivateKey,
		CreatedBy:       user.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	attachAllowPolicyForTarget(t, app, personal.ID, target.ID, false)

	out, err := runBastionSSHCommand(sshAddr, "private-box", userSigner, "whoami")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "remote" {
		t.Fatalf("unexpected output %q", out)
	}
}

func TestPasswordTargetOffersKeyboardInteractiveFallback(t *testing.T) {
	auth, err := targetAuthMethods(store.SSHTarget{
		AuthType:        store.AuthPassword,
		EncryptedSecret: []byte("correct-password"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(auth) != 2 {
		t.Fatalf("password target should offer password and keyboard-interactive auth, got %d methods", len(auth))
	}
}

func TestSSHDeniesBlacklistedExecAndAudits(t *testing.T) {
	app, _, sshAddr, stop := startBastionTestApp(t)
	defer stop()
	ctx := context.Background()
	userSigner := testSSHSigner(t)
	user := seedBastionUserWithKey(t, app, userSigner)
	org, err := app.store.Repository().CreateOrganization(ctx, store.CreateOrganizationParams{Name: "Ops", Slug: "ops", OwnerUserID: user.ID})
	if err != nil {
		t.Fatal(err)
	}
	groups, err := app.store.Repository().ListOrganizationUserGroups(ctx, org.ID)
	if err != nil {
		t.Fatal(err)
	}
	targetAddr, closeTarget := startTestSSHServer(t)
	defer closeTarget()
	host, portText, _ := net.SplitHostPort(targetAddr)
	port := mustAtoi(t, portText)
	target, err := app.store.Repository().CreateSSHTarget(ctx, store.CreateSSHTargetParams{
		OwnerType:      store.OwnerOrganization,
		OwnerID:        org.ID,
		Alias:          "test2",
		TargetType:     store.TargetDirect,
		Host:           host,
		Port:           port,
		RemoteUsername: "remote",
		AuthType:       store.AuthPassword,
		CreatedBy:      user.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	policy, err := app.store.Repository().CreateCommandPolicy(ctx, store.CreateCommandPolicyParams{
		OwnerType:     store.OwnerOrganization,
		OwnerID:       org.ID,
		Name:          "deny dangerous",
		DefaultAction: store.DecisionAllow,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.store.Repository().CreatePolicyRule(ctx, store.CreatePolicyRuleParams{
		PolicyID: policy.ID, RuleType: store.RuleBlacklist, PatternType: store.PatternContains, Pattern: "rm -rf",
	}); err != nil {
		t.Fatal(err)
	}
	if err := app.store.Repository().AttachPolicyToTarget(ctx, policy.ID, target.ID); err != nil {
		t.Fatal(err)
	}
	if err := app.store.Repository().AttachPolicyToUserGroup(ctx, policy.ID, groups[0].ID); err != nil {
		t.Fatal(err)
	}

	_, err = runBastionSSHCommand(sshAddr, "test2", userSigner, "rm -rf /tmp/example")
	if err == nil {
		t.Fatalf("expected denied command to fail")
	}
	page, err := app.audit.Repository().ListCommandAuditLogs(ctx, store.AuditLogFilter{UserID: user.ID})
	if err != nil {
		t.Fatal(err)
	}
	logs := page.Logs
	if len(logs) != 1 || logs[0].PolicyDecision != store.DecisionDeny || logs[0].Command != "rm -rf /tmp/example" {
		t.Fatalf("deny audit mismatch: %+v", logs)
	}
	if logs[0].PublicKeyFingerprint != gossh.FingerprintSHA256(userSigner.PublicKey()) {
		t.Fatalf("deny audit public key mismatch: %+v", logs[0])
	}
}

func TestSSHExecRoutesAliasThroughAgentTarget(t *testing.T) {
	app, httpAddr, sshAddr, stop := startBastionTestApp(t)
	defer stop()
	ctx := context.Background()
	userSigner := testSSHSigner(t)
	user := seedBastionUserWithKey(t, app, userSigner)
	personal, err := app.store.Repository().GetPersonalOrganizationForUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}

	token := "agent-route-token"
	if _, err := app.store.Repository().CreateAgentEnrollment(ctx, store.CreateAgentEnrollmentParams{
		OwnerType:   store.OwnerOrganization,
		OwnerID:     personal.ID,
		TokenHash:   codeHash(token),
		Label:       "agentbox-initial",
		DefaultHost: "127.0.0.1",
		DefaultPort: 22,
		CreatedBy:   user.ID,
		ExpiresAt:   time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	agentClient, err := agent.New(agent.Config{
		Server:          "http://" + httpAddr,
		EnrollmentToken: token,
		IDFile:          filepath.Join(t.TempDir(), "agent.json"),
	})
	if err != nil {
		t.Fatal(err)
	}
	agentCtx, cancelAgent := context.WithCancel(ctx)
	defer cancelAgent()
	go func() {
		if err := agentClient.Run(agentCtx); err != nil {
			t.Logf("agent stopped: %v", err)
		}
	}()
	var target store.SSHTarget
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		targets, err := app.store.Repository().ListSSHTargets(ctx, store.OwnerOrganization, personal.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, candidate := range targets {
			if candidate.TargetType == store.TargetAgent {
				target = candidate
				break
			}
		}
		if target.ID != "" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if target.ID == "" {
		t.Fatalf("agent target was not created")
	}
	renamed, err := app.store.Repository().UpdateSSHTarget(ctx, target.ID, store.UpdateSSHTargetParams{Alias: "agentbox"})
	if err != nil {
		t.Fatal(err)
	}
	if renamed.Alias != "agentbox" {
		t.Fatalf("rename mismatch: %+v", renamed)
	}
	attachAllowPolicyForTarget(t, app, personal.ID, renamed.ID, false)
	if err := os.WriteFile(app.knownHostsPath(), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := runBastionSSHCommand(sshAddr, "agentbox", userSigner, "echo agent-ok")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "agent-ok" {
		t.Fatalf("unexpected agent output %q", out)
	}
}

func TestSSHDirectTargetRoutesThroughAgentProxyWithoutProxyCredentials(t *testing.T) {
	app, httpAddr, sshAddr, stop := startBastionTestApp(t)
	defer stop()
	ctx := context.Background()
	userSigner := testSSHSigner(t)
	user := seedBastionUserWithKey(t, app, userSigner)
	targetAddr, closeTarget := startTestSSHServerWithPassword(t, "secret")
	defer closeTarget()
	host, portText, _ := net.SplitHostPort(targetAddr)
	port := mustAtoi(t, portText)
	personal, err := app.store.Repository().GetPersonalOrganizationForUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}

	token := "agent-proxy-token"
	if _, err := app.store.Repository().CreateAgentEnrollment(ctx, store.CreateAgentEnrollmentParams{
		OwnerType:   store.OwnerOrganization,
		OwnerID:     personal.ID,
		TokenHash:   codeHash(token),
		Label:       "proxy-agent",
		DefaultHost: "127.0.0.1",
		DefaultPort: 22,
		CreatedBy:   user.ID,
		ExpiresAt:   time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	agentClient, err := agent.New(agent.Config{
		Server:          "http://" + httpAddr,
		EnrollmentToken: token,
		IDFile:          filepath.Join(t.TempDir(), "agent.json"),
	})
	if err != nil {
		t.Fatal(err)
	}
	agentCtx, cancelAgent := context.WithCancel(ctx)
	defer cancelAgent()
	go func() {
		if err := agentClient.Run(agentCtx); err != nil {
			t.Logf("agent stopped: %v", err)
		}
	}()
	proxyTarget := waitForAgentTarget(t, app, personal.ID)
	if len(proxyTarget.EncryptedSecret) != 0 {
		t.Fatalf("agent proxy target should not need credentials")
	}
	target, err := app.store.Repository().CreateSSHTarget(ctx, store.CreateSSHTargetParams{
		OwnerType:       store.OwnerOrganization,
		OwnerID:         personal.ID,
		Alias:           "proxiedbox",
		TargetType:      store.TargetDirect,
		Host:            host,
		Port:            port,
		RemoteUsername:  "remote",
		AuthType:        store.AuthPassword,
		EncryptedSecret: []byte("secret"),
		ProxyTargetID:   proxyTarget.ID,
		CreatedBy:       user.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	attachAllowPolicyForTarget(t, app, personal.ID, target.ID, false)

	out, err := runBastionSSHCommand(sshAddr, "proxiedbox", userSigner, "whoami")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "remote" {
		t.Fatalf("unexpected proxied output %q", out)
	}
}

func TestSSHSFTPRoutesAliasToDirectTarget(t *testing.T) {
	app, _, sshAddr, stop := startBastionTestApp(t)
	defer stop()
	ctx := context.Background()
	userSigner := testSSHSigner(t)
	user := seedBastionUserWithKey(t, app, userSigner)
	targetAddr, closeTarget := startTestSFTPServer(t, testSFTPModeSubsystem)
	defer closeTarget()
	host, portText, _ := net.SplitHostPort(targetAddr)
	port := mustAtoi(t, portText)
	personal, err := app.store.Repository().GetPersonalOrganizationForUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	target, err := app.store.Repository().CreateSSHTarget(ctx, store.CreateSSHTargetParams{
		OwnerType:      store.OwnerOrganization,
		OwnerID:        personal.ID,
		Alias:          "sftpbox",
		TargetType:     store.TargetDirect,
		Host:           host,
		Port:           port,
		RemoteUsername: "remote",
		AuthType:       store.AuthPassword,
		CreatedBy:      user.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	attachAllowSFTPPolicyForTarget(t, app, personal.ID, target.ID)
	client, err := gossh.Dial("tcp", sshAddr, &gossh.ClientConfig{
		User:            "sftpbox",
		Auth:            []gossh.AuthMethod{gossh.PublicKeys(userSigner)},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sftpClient.ReadDir("/"); err != nil {
		t.Fatal(err)
	}
	if err := sftpClient.Close(); err != nil {
		t.Fatal(err)
	}
	_ = client.Close()
	waitForSFTPAudit(t, app, target.ID)
}

func TestSSHSFTPFallsBackWhenSubsystemExitsBeforeHandshake(t *testing.T) {
	app, _, sshAddr, stop := startBastionTestApp(t)
	defer stop()
	ctx := context.Background()
	userSigner := testSSHSigner(t)
	user := seedBastionUserWithKey(t, app, userSigner)
	targetAddr, closeTarget := startTestSFTPServer(t, testSFTPModeRejectSubsystem)
	defer closeTarget()
	host, portText, _ := net.SplitHostPort(targetAddr)
	port := mustAtoi(t, portText)
	personal, err := app.store.Repository().GetPersonalOrganizationForUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	target, err := app.store.Repository().CreateSSHTarget(ctx, store.CreateSSHTargetParams{
		OwnerType:      store.OwnerOrganization,
		OwnerID:        personal.ID,
		Alias:          "fallback-sftpbox",
		TargetType:     store.TargetDirect,
		Host:           host,
		Port:           port,
		RemoteUsername: "remote",
		AuthType:       store.AuthPassword,
		CreatedBy:      user.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	attachAllowSFTPPolicyForTarget(t, app, personal.ID, target.ID)
	client, err := gossh.Dial("tcp", sshAddr, &gossh.ClientConfig{
		User:            "fallback-sftpbox",
		Auth:            []gossh.AuthMethod{gossh.PublicKeys(userSigner)},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sftpClient.ReadDir("/"); err != nil {
		t.Fatal(err)
	}
	if err := sftpClient.Close(); err != nil {
		t.Fatal(err)
	}
	_ = client.Close()
	waitForSFTPAudit(t, app, target.ID)
}

func TestSSHInteractiveShellReturnsAfterRemoteExitWithoutClientEOF(t *testing.T) {
	app, _, sshAddr, stop := startBastionTestApp(t)
	defer stop()
	ctx := context.Background()
	userSigner := testSSHSigner(t)
	user := seedBastionUserWithKey(t, app, userSigner)
	targetAddr, closeTarget := startTestShellExitServer(t)
	defer closeTarget()
	host, portText, _ := net.SplitHostPort(targetAddr)
	port := mustAtoi(t, portText)
	personal, err := app.store.Repository().GetPersonalOrganizationForUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	target, err := app.store.Repository().CreateSSHTarget(ctx, store.CreateSSHTargetParams{
		OwnerType:      store.OwnerOrganization,
		OwnerID:        personal.ID,
		Alias:          "shellbox",
		TargetType:     store.TargetDirect,
		Host:           host,
		Port:           port,
		RemoteUsername: "remote",
		AuthType:       store.AuthPassword,
		CreatedBy:      user.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	attachAllowPolicyForTarget(t, app, personal.ID, target.ID, true)
	client, err := gossh.Dial("tcp", sshAddr, &gossh.ClientConfig{
		User:            "shellbox",
		Auth:            []gossh.AuthMethod{gossh.PublicKeys(userSigner)},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	stdin, err := session.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	_ = stdin
	if err := session.RequestPty("xterm-256color", 24, 80, gossh.TerminalModes{}); err != nil {
		t.Fatal(err)
	}
	if err := session.Shell(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- session.Wait()
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(700 * time.Millisecond):
		t.Fatal("interactive shell did not return after remote exit")
	}
}

func startBastionTestApp(t *testing.T) (*App, string, string, func()) {
	t.Helper()
	httpLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	sshLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	app := NewApp(Config{
		DatabasePath: filepath.Join(t.TempDir(), "gosshd.db"),
		HostKeyPath:  filepath.Join(t.TempDir(), "host_key"),
	})
	if err := app.ensureServices(ctx); err != nil {
		t.Fatal(err)
	}
	go func() {
		if err := app.RunListeners(ctx, httpLn, sshLn); err != nil {
			t.Logf("server stopped: %v", err)
		}
	}()
	stop := func() {
		cancel()
		_ = app.Close()
	}
	return app, httpLn.Addr().String(), sshLn.Addr().String(), stop
}

func seedBastionUserWithKey(t *testing.T, app *App, signer gossh.Signer) store.User {
	t.Helper()
	ctx := context.Background()
	user, err := app.store.Repository().CreateUser(ctx, store.CreateUserParams{
		Email:        "ssh@example.com",
		DisplayName:  "SSH User",
		PasswordHash: []byte("hash"),
	})
	if err != nil {
		t.Fatal(err)
	}
	normalized, fingerprint, err := bastion.NormalizeAuthorizedKey(string(gossh.MarshalAuthorizedKey(signer.PublicKey())))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.store.Repository().CreatePublicKey(ctx, store.CreatePublicKeyParams{
		UserID:        user.ID,
		Name:          "test",
		AuthorizedKey: normalized,
		Fingerprint:   fingerprint,
	}); err != nil {
		t.Fatal(err)
	}
	return user
}

func attachAllowPolicyForTarget(t *testing.T, app *App, orgID, targetID string, allowInteractive bool) {
	t.Helper()
	ctx := context.Background()
	groups, err := app.store.Repository().ListOrganizationUserGroups(ctx, orgID)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) == 0 {
		t.Fatalf("organization %s has no user groups", orgID)
	}
	policy, err := app.store.Repository().CreateCommandPolicy(ctx, store.CreateCommandPolicyParams{
		OwnerType:        store.OwnerOrganization,
		OwnerID:          orgID,
		Name:             "allow test target",
		DefaultAction:    store.DecisionAllow,
		AllowInteractive: allowInteractive,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.store.Repository().AttachPolicyToTarget(ctx, policy.ID, targetID); err != nil {
		t.Fatal(err)
	}
	if err := app.store.Repository().AttachPolicyToUserGroup(ctx, policy.ID, groups[0].ID); err != nil {
		t.Fatal(err)
	}
}

func attachAllowSFTPPolicyForTarget(t *testing.T, app *App, orgID, targetID string) {
	t.Helper()
	ctx := context.Background()
	groups, err := app.store.Repository().ListOrganizationUserGroups(ctx, orgID)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) == 0 {
		t.Fatalf("organization %s has no user groups", orgID)
	}
	policy, err := app.store.Repository().CreateCommandPolicy(ctx, store.CreateCommandPolicyParams{
		OwnerType:     store.OwnerOrganization,
		OwnerID:       orgID,
		Name:          "allow test sftp",
		DefaultAction: store.DecisionAllow,
		AllowDownload: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.store.Repository().AttachPolicyToTarget(ctx, policy.ID, targetID); err != nil {
		t.Fatal(err)
	}
	if err := app.store.Repository().AttachPolicyToUserGroup(ctx, policy.ID, groups[0].ID); err != nil {
		t.Fatal(err)
	}
}

func waitForSFTPAudit(t *testing.T, app *App, targetID string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		page, err := app.audit.Repository().ListCommandAuditLogs(context.Background(), store.AuditLogFilter{
			TargetID:    targetID,
			RequestType: store.RequestSFTP,
			Limit:       1,
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Logs) > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for sftp audit log")
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func runBastionSSHCommand(addr, alias string, signer gossh.Signer, command string) (string, error) {
	client, err := gossh.Dial("tcp", addr, &gossh.ClientConfig{
		User:            alias,
		Auth:            []gossh.AuthMethod{gossh.PublicKeys(signer)},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	})
	if err != nil {
		return "", err
	}
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()
	var out bytes.Buffer
	session.Stdout = &out
	err = session.Run(command)
	return out.String(), err
}

func startTestSSHServer(t *testing.T) (string, func()) {
	t.Helper()
	return startTestSSHServerWithAuthorizedKey(t, nil)
}

func startTestSSHServerWithPassword(t *testing.T, password string) (string, func()) {
	t.Helper()
	hostSigner := testSSHSigner(t)
	cfg := &gossh.ServerConfig{
		PasswordCallback: func(meta gossh.ConnMetadata, supplied []byte) (*gossh.Permissions, error) {
			if meta.User() == "remote" && string(supplied) == password {
				return nil, nil
			}
			return nil, errors.New("unauthorized")
		},
	}
	cfg.AddHostKey(hostSigner)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			raw, err := ln.Accept()
			if err != nil {
				return
			}
			go handleTestSSHConn(raw, cfg)
		}
	}()
	return ln.Addr().String(), func() {
		_ = ln.Close()
		<-done
	}
}

func startTestShellExitServer(t *testing.T) (string, func()) {
	t.Helper()
	hostSigner := testSSHSigner(t)
	cfg := &gossh.ServerConfig{NoClientAuth: true}
	cfg.AddHostKey(hostSigner)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			raw, err := ln.Accept()
			if err != nil {
				return
			}
			go handleTestShellExitConn(raw, cfg)
		}
	}()
	return ln.Addr().String(), func() {
		_ = ln.Close()
		<-done
	}
}

type testSFTPMode int

const (
	testSFTPModeSubsystem testSFTPMode = iota
	testSFTPModeRejectSubsystem
	testSFTPModeExitSubsystem
)

func startTestSFTPServer(t *testing.T, mode testSFTPMode) (string, func()) {
	t.Helper()
	hostSigner := testSSHSigner(t)
	cfg := &gossh.ServerConfig{NoClientAuth: true}
	cfg.AddHostKey(hostSigner)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			raw, err := ln.Accept()
			if err != nil {
				return
			}
			go handleTestSFTPConn(raw, cfg, mode)
		}
	}()
	return ln.Addr().String(), func() {
		_ = ln.Close()
		<-done
	}
}

func startTestSSHServerWithAuthorizedKey(t *testing.T, authorizedKey gossh.PublicKey) (string, func()) {
	t.Helper()
	hostSigner := testSSHSigner(t)
	cfg := &gossh.ServerConfig{}
	if authorizedKey == nil {
		cfg.NoClientAuth = true
	} else {
		cfg.PublicKeyCallback = func(meta gossh.ConnMetadata, key gossh.PublicKey) (*gossh.Permissions, error) {
			if bytes.Equal(key.Marshal(), authorizedKey.Marshal()) {
				return nil, nil
			}
			return nil, errors.New("unauthorized")
		}
	}
	cfg.AddHostKey(hostSigner)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			raw, err := ln.Accept()
			if err != nil {
				return
			}
			go handleTestSSHConn(raw, cfg)
		}
	}()
	return ln.Addr().String(), func() {
		_ = ln.Close()
		<-done
	}
}

func handleTestSFTPConn(raw net.Conn, cfg *gossh.ServerConfig, mode testSFTPMode) {
	conn, chans, reqs, err := gossh.NewServerConn(raw, cfg)
	if err != nil {
		_ = raw.Close()
		return
	}
	defer conn.Close()
	go gossh.DiscardRequests(reqs)
	for ch := range chans {
		if ch.ChannelType() != "session" {
			_ = ch.Reject(gossh.UnknownChannelType, "unsupported")
			continue
		}
		channel, requests, err := ch.Accept()
		if err != nil {
			continue
		}
		go func() {
			defer channel.Close()
			for req := range requests {
				switch req.Type {
				case "subsystem":
					var payload struct{ Name string }
					_ = gossh.Unmarshal(req.Payload, &payload)
					if payload.Name != "sftp" {
						req.Reply(false, nil)
						continue
					}
					if mode == testSFTPModeRejectSubsystem {
						req.Reply(false, nil)
						continue
					}
					req.Reply(true, nil)
					if mode == testSFTPModeExitSubsystem {
						sendExit(channel, 1)
						return
					}
					serveTestSFTP(channel)
					sendExit(channel, 0)
					return
				case "exec":
					var payload struct{ Command string }
					_ = gossh.Unmarshal(req.Payload, &payload)
					if !strings.Contains(payload.Command, "sftp-server") {
						req.Reply(false, nil)
						continue
					}
					req.Reply(true, nil)
					serveTestSFTP(channel)
					sendExit(channel, 0)
					return
				default:
					req.Reply(false, nil)
				}
			}
		}()
	}
}

func serveTestSFTP(rwc io.ReadWriteCloser) {
	server, err := sftp.NewServer(rwc)
	if err != nil {
		return
	}
	_ = server.Serve()
	_ = server.Close()
}

func handleTestShellExitConn(raw net.Conn, cfg *gossh.ServerConfig) {
	conn, chans, reqs, err := gossh.NewServerConn(raw, cfg)
	if err != nil {
		_ = raw.Close()
		return
	}
	defer conn.Close()
	go gossh.DiscardRequests(reqs)
	for ch := range chans {
		if ch.ChannelType() != "session" {
			_ = ch.Reject(gossh.UnknownChannelType, "unsupported")
			continue
		}
		channel, requests, err := ch.Accept()
		if err != nil {
			continue
		}
		go func() {
			defer channel.Close()
			for req := range requests {
				switch req.Type {
				case "pty-req":
					req.Reply(true, nil)
				case "shell":
					req.Reply(true, nil)
					_, _ = channel.Write([]byte("bye\r\n"))
					sendExit(channel, 0)
					return
				default:
					req.Reply(false, nil)
				}
			}
		}()
	}
}

func handleTestSSHConn(raw net.Conn, cfg *gossh.ServerConfig) {
	conn, chans, reqs, err := gossh.NewServerConn(raw, cfg)
	if err != nil {
		_ = raw.Close()
		return
	}
	defer conn.Close()
	go gossh.DiscardRequests(reqs)
	for ch := range chans {
		if ch.ChannelType() != "session" {
			_ = ch.Reject(gossh.UnknownChannelType, "unsupported")
			continue
		}
		channel, requests, err := ch.Accept()
		if err != nil {
			continue
		}
		go func() {
			defer channel.Close()
			for req := range requests {
				if req.Type != "exec" {
					req.Reply(false, nil)
					continue
				}
				req.Reply(true, nil)
				var payload struct{ Command string }
				_ = gossh.Unmarshal(req.Payload, &payload)
				switch strings.TrimSpace(payload.Command) {
				case "whoami":
					_, _ = channel.Write([]byte("remote\n"))
					sendExit(channel, 0)
				default:
					_, _ = channel.Stderr().Write([]byte("unknown command\n"))
					sendExit(channel, 1)
				}
				return
			}
		}()
	}
}

func testSSHSigner(t *testing.T) gossh.Signer {
	t.Helper()
	signer, _ := testSSHSignerWithPrivateKey(t)
	return signer
}

func testSSHSignerWithPrivateKey(t *testing.T) (gossh.Signer, []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := gossh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	block, err := gossh.MarshalPrivateKey(key, "")
	if err != nil {
		t.Fatal(err)
	}
	return signer, pem.EncodeToMemory(block)
}

func mustAtoi(t *testing.T, value string) int {
	t.Helper()
	var out int
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			t.Fatalf("invalid int %q", value)
		}
		out = out*10 + int(ch-'0')
	}
	return out
}
