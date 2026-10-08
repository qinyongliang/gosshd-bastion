package server

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/qinyongliang/gosshd-bastion/internal/store"
	gossh "golang.org/x/crypto/ssh"
)

func temporarySSHTestFixture(t *testing.T) (*App, string, store.User, store.SSHTarget) {
	t.Helper()
	app, _, addr, stop := startBastionTestApp(t)
	t.Cleanup(stop)
	ctx := context.Background()
	user := seedBastionUserWithKey(t, app, testSSHSigner(t))
	targetAddr, closeTarget := startTestSSHServer(t)
	t.Cleanup(closeTarget)
	host, port, _ := net.SplitHostPort(targetAddr)
	org, err := app.store.Repository().GetPersonalOrganizationForUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	target, err := app.store.Repository().CreateSSHTarget(ctx, store.CreateSSHTargetParams{
		OwnerType: store.OwnerOrganization, OwnerID: org.ID, Alias: "temporary-box", TargetType: store.TargetDirect,
		Host: host, Port: mustAtoi(t, port), RemoteUsername: "remote", AuthType: store.AuthPassword, CreatedBy: user.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	return app, addr, user, target
}

func createTemporarySSHTestGrant(t *testing.T, app *App, user store.User, target store.SSHTarget, expires time.Time) store.TemporarySSHAuthorization {
	t.Helper()
	grant, err := app.store.Repository().CreateTemporarySSHAuthorization(context.Background(), store.CreateTemporarySSHAuthorizationParams{TargetID: target.ID, CreatedBy: user.ID, ExpiresAt: expires})
	if err != nil {
		t.Fatal(err)
	}
	return grant
}

func dialWithoutClientKey(addr, username string) (*gossh.Client, error) {
	return gossh.Dial("tcp", addr, &gossh.ClientConfig{User: username, HostKeyCallback: gossh.InsecureIgnoreHostKey(), Timeout: 3 * time.Second})
}

func TestTemporarySSHAuthorizationExecWithoutKeyAndAudits(t *testing.T) {
	app, addr, user, target := temporarySSHTestFixture(t)
	grant := createTemporarySSHTestGrant(t, app, user, target, time.Now().Add(time.Hour))
	if _, err := app.store.DB().ExecContext(context.Background(), "UPDATE temporary_ssh_authorizations SET name = ? WHERE id = ?", "临时维护授权", grant.ID); err != nil {
		t.Fatal(err)
	}
	client, err := dialWithoutClientKey(addr, grant.Token)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	out, err := session.Output("whoami")
	if err != nil || strings.TrimSpace(string(out)) != "remote" {
		t.Fatalf("keyless exec failed: %v %q", err, out)
	}
	page, err := app.audit.Repository().ListCommandAuditLogs(context.Background(), store.AuditLogFilter{UserID: user.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Logs) != 1 || page.Logs[0].TargetID != target.ID || page.Logs[0].UserID != user.ID || page.Logs[0].PublicKeyFingerprint != "" {
		t.Fatal("temporary SSH command did not retain scoped audit identity")
	}
	if page.Logs[0].PublicKeyName != "临时维护授权" {
		t.Fatal("temporary authorization name missing from key column")
	}
}

func TestTemporarySSHAuthorizationRejectsInvalidExpiredDeletedAndAlias(t *testing.T) {
	app, addr, user, target := temporarySSHTestFixture(t)
	expired := createTemporarySSHTestGrant(t, app, user, target, time.Now().Add(-time.Hour))
	deleted := createTemporarySSHTestGrant(t, app, user, target, time.Now().Add(time.Hour))
	if err := app.store.Repository().DeleteTemporarySSHAuthorization(context.Background(), deleted.ID, target.ID); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{uuid.NewString(), expired.Token, deleted.Token, target.Alias, target.ID, expired.ID} {
		if client, err := dialWithoutClientKey(addr, name); err == nil {
			client.Close()
			t.Fatal("invalid or unkeyed alias login was accepted")
		}
	}
}

func TestTemporarySSHAuthorizationKeepsCommandPolicy(t *testing.T) {
	app, addr, user, target := temporarySSHTestFixture(t)
	ctx := context.Background()
	policy, err := app.store.Repository().CreateCommandPolicy(ctx, store.CreateCommandPolicyParams{OwnerType: target.OwnerType, OwnerID: target.OwnerID, Name: "deny temporary commands", DefaultAction: store.DecisionDeny})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.store.Repository().AttachPolicyToTarget(ctx, policy.ID, target.ID); err != nil {
		t.Fatal(err)
	}
	groups, err := app.store.Repository().ListOrganizationUserGroups(ctx, target.OwnerID)
	if err != nil || len(groups) == 0 {
		t.Fatal("test creator group missing")
	}
	if err := app.store.Repository().AttachPolicyToUserGroup(ctx, policy.ID, groups[0].ID); err != nil {
		t.Fatal(err)
	}
	grant := createTemporarySSHTestGrant(t, app, user, target, time.Now().Add(time.Hour))
	client, err := dialWithoutClientKey(addr, grant.Token)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if _, err := session.CombinedOutput("whoami"); err == nil {
		t.Fatal("temporary authorization bypassed command denial")
	}
}

func TestTemporarySSHAuthorizationClosesExistingConnection(t *testing.T) {
	for _, reason := range []string{"delete", "expire", "disable", "target-delete"} {
		t.Run(reason, func(t *testing.T) {
			app, addr, user, target := temporarySSHTestFixture(t)
			grant := createTemporarySSHTestGrant(t, app, user, target, time.Now().Add(time.Hour))
			client, err := dialWithoutClientKey(addr, grant.Token)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			closed := make(chan error, 1)
			go func() { closed <- client.Wait() }()
			ctx := context.Background()
			switch reason {
			case "delete":
				err = app.store.Repository().DeleteTemporarySSHAuthorization(ctx, grant.ID, target.ID)
			case "expire":
				_, err = app.store.DB().ExecContext(ctx, "UPDATE temporary_ssh_authorizations SET expires_at = ? WHERE id = ?", time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano), grant.ID)
			case "disable":
				err = app.store.Repository().UpdateUserDisabled(ctx, user.ID, true)
			case "target-delete":
				err = app.store.Repository().DeleteSSHTarget(ctx, target.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-closed:
			case <-time.After(4 * time.Second):
				t.Fatal("revoked temporary connection remained open")
			}
			if another, err := dialWithoutClientKey(addr, grant.Token); err == nil {
				another.Close()
				t.Fatal("revoked credential was accepted again")
			}
		})
	}
}

func TestTemporarySSHAuthorizationRenewalKeepsExistingConnection(t *testing.T) {
	app, addr, user, target := temporarySSHTestFixture(t)
	grant := createTemporarySSHTestGrant(t, app, user, target, time.Now().Add(2*time.Second))
	client, err := dialWithoutClientKey(addr, grant.Token)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if _, err := app.store.Repository().RenewTemporarySSHAuthorization(context.Background(), grant.ID, target.ID, time.Minute, time.Now()); err != nil {
		t.Fatal(err)
	}
	closed := make(chan error, 1)
	go func() { closed <- client.Wait() }()
	select {
	case <-closed:
		t.Fatal("renewed connection was closed at its old expiry")
	case <-time.After(2500 * time.Millisecond):
	}
	session, err := client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if out, err := session.Output("whoami"); err != nil || strings.TrimSpace(string(out)) != "remote" {
		t.Fatal("renewed connection no longer routes to the service")
	}
}

func TestTemporarySSHAuthorizationRevokesRemovedCreatorAccess(t *testing.T) {
	for _, reason := range []string{"membership", "group"} {
		t.Run(reason, func(t *testing.T) {
			app, addr, user, original := temporarySSHTestFixture(t)
			ctx := context.Background()
			owner, err := app.store.Repository().CreateUser(ctx, store.CreateUserParams{Email: "owner@example.com", PasswordHash: []byte("hash")})
			if err != nil {
				t.Fatal(err)
			}
			org, err := app.store.Repository().CreateOrganization(ctx, store.CreateOrganizationParams{Name: "Shared", Slug: "shared", OwnerUserID: owner.ID})
			if err != nil {
				t.Fatal(err)
			}
			if err := app.store.Repository().AddOrganizationMember(ctx, org.ID, user.ID, store.RoleMember); err != nil {
				t.Fatal(err)
			}
			target, err := app.store.Repository().CreateSSHTarget(ctx, store.CreateSSHTargetParams{OwnerType: store.OwnerOrganization, OwnerID: org.ID, Alias: "shared-box", TargetType: store.TargetDirect, Host: original.Host, Port: original.Port, RemoteUsername: original.RemoteUsername, AuthType: store.AuthPassword, CreatedBy: owner.ID})
			if err != nil {
				t.Fatal(err)
			}
			grant := createTemporarySSHTestGrant(t, app, user, target, time.Now().Add(time.Hour))
			client, err := dialWithoutClientKey(addr, grant.Token)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			closed := make(chan error, 1)
			go func() { closed <- client.Wait() }()
			if reason == "membership" {
				err = app.store.Repository().RemoveOrganizationMember(ctx, org.ID, user.ID)
			} else {
				var group store.OrganizationUserGroup
				group, err = app.store.Repository().CreateOrganizationUserGroup(ctx, store.CreateOrganizationUserGroupParams{OrganizationID: org.ID, Name: "Restricted", Slug: "restricted"})
				if err == nil {
					err = app.store.Repository().AttachUserGroupToTarget(ctx, group.ID, target.ID)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-closed:
			case <-time.After(4 * time.Second):
				t.Fatal("creator lost access but its temporary connection remained open")
			}
			if another, err := dialWithoutClientKey(addr, grant.Token); err == nil {
				another.Close()
				t.Fatal("grant bypassed removed creator access")
			}
		})
	}
}
