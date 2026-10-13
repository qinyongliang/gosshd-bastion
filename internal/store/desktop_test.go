package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestDesktopGrantPersistenceAndDefaults(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, filepath.Join(t.TempDir(), "main.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	user, err := st.Repository().CreateUser(ctx, CreateUserParams{Email: "desktop@example.test", DisplayName: "Desktop", PasswordHash: []byte("hash")})
	if err != nil {
		t.Fatal(err)
	}
	org, err := st.Repository().CreateOrganization(ctx, CreateOrganizationParams{Name: "Desktop", Slug: "desktop", OwnerUserID: user.ID})
	if err != nil {
		t.Fatal(err)
	}
	target, err := st.Repository().CreateSSHTarget(ctx, CreateSSHTargetParams{OwnerType: OwnerOrganization, OwnerID: org.ID, Alias: "desktop", TargetType: TargetAgent, AgentID: "test-agent", CreatedBy: user.ID})
	if err != nil {
		t.Fatal(err)
	}
	grant, err := st.Repository().GetDesktopAccess(ctx, user.ID, target.ID)
	if err != nil || grant.AllowRead || grant.AllowControl {
		t.Fatal("desktop must default denied")
	}
	grant.AllowControl = true
	grant.GrantedBy = user.ID
	if st.Repository().SetDesktopAccess(ctx, grant) == nil {
		t.Fatal("control without read accepted")
	}
	grant.AllowRead = true
	expiry := time.Now().Add(time.Hour)
	grant.ExpiresAt = &expiry
	if err = st.Repository().SetDesktopAccess(ctx, grant); err != nil {
		t.Fatal(err)
	}
	saved, err := st.Repository().GetDesktopAccess(ctx, user.ID, target.ID)
	if err != nil || !saved.AllowRead || !saved.AllowControl || saved.ExpiresAt == nil {
		t.Fatalf("grant not persisted %+v %v", saved, err)
	}
	if got := normalizeMCPToolGroups([]string{"desktop"}); len(got) != 1 || got[0] != "desktop" {
		t.Fatal("desktop MCP group rejected")
	}
	if got := normalizeMCPToolGroups(nil); len(got) != 1 || got[0] != "session" {
		t.Fatal("existing token defaults expanded")
	}
}
