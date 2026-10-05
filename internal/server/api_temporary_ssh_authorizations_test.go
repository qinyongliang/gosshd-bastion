package server

import (
	"context"
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/qinyongliang/gosshd-bastion/internal/store"
)

func TestAPITemporarySSHAuthorizationLifecycleAndIsolation(t *testing.T) {
	srv, client, app := newAPITestServer(t)
	defer srv.Close()
	ctx := context.Background()
	user, err := app.store.Repository().CreateUser(ctx, store.CreateUserParams{Email: "temporary@example.com", PasswordHash: []byte("hash")})
	if err != nil {
		t.Fatal(err)
	}
	attachTestSession(t, client, srv.URL, app, user.ID)
	org, err := app.store.Repository().GetPersonalOrganizationForUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	var targets []store.SSHTarget
	for _, alias := range []string{"shared-box", "other-box"} {
		target, err := app.store.Repository().CreateSSHTarget(ctx, store.CreateSSHTargetParams{
			OwnerType: store.OwnerOrganization, OwnerID: org.ID, Alias: alias, TargetType: store.TargetDirect,
			Host: "127.0.0.1", Port: 22, RemoteUsername: "root", AuthType: store.AuthPassword, CreatedBy: user.ID,
		})
		if err != nil {
			t.Fatal(err)
		}
		targets = append(targets, target)
	}
	base := srv.URL + "/api/targets/" + targets[0].ID + "/temporary-authorizations"
	otherBase := srv.URL + "/api/targets/" + targets[1].ID + "/temporary-authorizations"
	var created apiTemporarySSHAuthorizationResponse
	before := time.Now()
	postJSON(t, client, base, map[string]any{"name": " Handoff "}, http.StatusCreated, &created)
	grant := created.Authorization
	if _, err := uuid.Parse(grant.Token); err != nil || grant.Token == grant.ID || grant.Name != "Handoff" || grant.TargetID != targets[0].ID || grant.CreatedBy != user.ID {
		t.Fatal("created authorization does not contain the scoped UUID credential")
	}
	expires, err := time.Parse(time.RFC3339Nano, grant.ExpiresAt)
	if err != nil || expires.Before(before.Add(24*time.Hour)) || expires.After(time.Now().Add(24*time.Hour)) {
		t.Fatalf("default expiry: %s %v", grant.ExpiresAt, err)
	}
	var listed apiTemporarySSHAuthorizationsResponse
	getJSON(t, client, base, http.StatusOK, &listed)
	if len(listed.Authorizations) != 1 || listed.Authorizations[0].Token != grant.Token {
		t.Fatal("authorization missing from its target list")
	}
	getJSON(t, client, otherBase, http.StatusOK, &listed)
	if len(listed.Authorizations) != 0 {
		t.Fatal("authorization leaked into another target")
	}
	var renewed apiTemporarySSHAuthorizationResponse
	postJSON(t, client, base+"/"+grant.ID+"/renew", map[string]any{"duration_seconds": 7200}, http.StatusOK, &renewed)
	renewedExpiry, err := time.Parse(time.RFC3339Nano, renewed.Authorization.ExpiresAt)
	if err != nil || renewed.Authorization.Token != grant.Token || renewedExpiry.Sub(expires) != 2*time.Hour {
		t.Fatal("renewal must extend expiry without rotating the UUID")
	}
	postJSON(t, client, otherBase+"/"+grant.ID+"/renew", map[string]any{}, http.StatusNotFound, nil)
	deleteJSON(t, client, otherBase+"/"+grant.ID, http.StatusNotFound)
	for _, duration := range []any{0, -1, 1.5, int64(math.MaxInt64)} {
		postJSON(t, client, base, map[string]any{"duration_seconds": duration}, http.StatusBadRequest, nil)
		postJSON(t, client, base+"/"+grant.ID+"/renew", map[string]any{"duration_seconds": duration}, http.StatusBadRequest, nil)
	}
	outsider, err := app.store.Repository().CreateUser(ctx, store.CreateUserParams{Email: "outsider@example.com", PasswordHash: []byte("hash")})
	if err != nil {
		t.Fatal(err)
	}
	outsiderClient := apiClient(t)
	attachTestSession(t, outsiderClient, srv.URL, app, outsider.ID)
	getJSON(t, outsiderClient, base, http.StatusForbidden, nil)
	postJSON(t, outsiderClient, base, map[string]any{}, http.StatusForbidden, nil)
	postJSON(t, outsiderClient, base+"/"+grant.ID+"/renew", map[string]any{}, http.StatusForbidden, nil)
	deleteJSON(t, outsiderClient, base+"/"+grant.ID, http.StatusForbidden)
	getJSON(t, apiClient(t), base, http.StatusUnauthorized, nil)
	postJSON(t, apiClient(t), base, map[string]any{}, http.StatusUnauthorized, nil)
	deleteJSON(t, client, base+"/"+grant.ID, http.StatusNoContent)
	postJSON(t, client, base+"/"+grant.ID+"/renew", map[string]any{}, http.StatusNotFound, nil)
	getJSON(t, client, base, http.StatusOK, &listed)
	if len(listed.Authorizations) != 0 {
		t.Fatal("deleted grant remained in list")
	}
	postJSON(t, client, base, map[string]any{"duration_seconds": 1800}, http.StatusCreated, &created)
	customExpiry, _ := time.Parse(time.RFC3339Nano, created.Authorization.ExpiresAt)
	if remaining := time.Until(customExpiry); remaining > 30*time.Minute || remaining < 29*time.Minute {
		t.Fatal("custom expiry was ignored")
	}
	expired, err := app.store.Repository().CreateTemporarySSHAuthorization(ctx, store.CreateTemporarySSHAuthorizationParams{TargetID: targets[0].ID, CreatedBy: user.ID, ExpiresAt: time.Now().Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	postJSON(t, client, base+"/"+expired.ID+"/renew", map[string]any{}, http.StatusOK, &renewed)
	refreshed, _ := time.Parse(time.RFC3339Nano, renewed.Authorization.ExpiresAt)
	if renewed.Authorization.Token != expired.Token || time.Until(refreshed) < 23*time.Hour {
		t.Fatal("expired grant renewal did not start from now")
	}
}
