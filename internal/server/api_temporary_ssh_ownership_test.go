package server

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/qinyongliang/gosshd-bastion/internal/store"
)

func TestAPITemporarySSHAuthorizationOwnership(t *testing.T) {
	srv, ownerClient, app := newAPITestServer(t)
	defer srv.Close()
	ctx := context.Background()
	owner, err := app.store.Repository().CreateUser(ctx, store.CreateUserParams{Email: "owner@example.com", PasswordHash: []byte("hash")})
	if err != nil {
		t.Fatal(err)
	}
	member, err := app.store.Repository().CreateUser(ctx, store.CreateUserParams{Email: "member@example.com", PasswordHash: []byte("hash")})
	if err != nil {
		t.Fatal(err)
	}
	org, err := app.store.Repository().CreateOrganization(ctx, store.CreateOrganizationParams{Name: "Shared", Slug: "shared", OwnerUserID: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.store.Repository().AddOrganizationMember(ctx, org.ID, member.ID, store.RoleMember); err != nil {
		t.Fatal(err)
	}
	target, err := app.store.Repository().CreateSSHTarget(ctx, store.CreateSSHTargetParams{OwnerType: store.OwnerOrganization, OwnerID: org.ID, Alias: "shared", TargetType: store.TargetDirect, Host: "127.0.0.1", Port: 22, RemoteUsername: "root", AuthType: store.AuthPassword, CreatedBy: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	memberClient := apiClient(t)
	attachTestSession(t, ownerClient, srv.URL, app, owner.ID)
	attachTestSession(t, memberClient, srv.URL, app, member.ID)
	base := srv.URL + "/api/targets/" + target.ID + "/temporary-authorizations"
	var ownerGrant, memberGrant apiTemporarySSHAuthorizationResponse
	postJSON(t, ownerClient, base, map[string]any{}, http.StatusCreated, &ownerGrant)
	postJSON(t, memberClient, base, map[string]any{}, http.StatusCreated, &memberGrant)
	var list apiTemporarySSHAuthorizationsResponse
	getJSON(t, memberClient, base, http.StatusOK, &list)
	if len(list.Authorizations) != 1 || list.Authorizations[0].ID != memberGrant.Authorization.ID {
		t.Fatal("member can read a more privileged creator's bearer credential")
	}
	getJSON(t, ownerClient, base, http.StatusOK, &list)
	if len(list.Authorizations) != 2 {
		t.Fatal("organization owner cannot manage service authorizations")
	}
	postJSON(t, memberClient, base+"/"+ownerGrant.Authorization.ID+"/renew", map[string]any{}, http.StatusForbidden, nil)
	deleteJSON(t, memberClient, base+"/"+ownerGrant.Authorization.ID, http.StatusForbidden)
	postJSON(t, ownerClient, base+"/"+memberGrant.Authorization.ID+"/renew", map[string]any{}, http.StatusOK, &memberGrant)
	deleteJSON(t, ownerClient, base+"/"+memberGrant.Authorization.ID, http.StatusNoContent)
	group, err := app.store.Repository().CreateOrganizationUserGroup(ctx, store.CreateOrganizationUserGroupParams{OrganizationID: org.ID, Name: "Restricted", Slug: "restricted"})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.store.Repository().AttachUserGroupToTarget(ctx, group.ID, target.ID); err != nil {
		t.Fatal(err)
	}
	getJSON(t, memberClient, base, http.StatusForbidden, nil)
	postJSON(t, memberClient, base, map[string]any{}, http.StatusForbidden, nil)
	getJSON(t, ownerClient, base, http.StatusOK, &list)
	if len(list.Authorizations) != 1 {
		t.Fatal("ownership boundaries altered the remaining grant")
	}
	if expiry, err := time.Parse(time.RFC3339Nano, list.Authorizations[0].ExpiresAt); err != nil || time.Until(expiry) < 23*time.Hour {
		t.Fatal("failed renewal changed owner grant")
	}
}
