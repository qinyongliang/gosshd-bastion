package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
)

func temporarySSHFixture(t *testing.T, st *Store) (User, []SSHTarget) {
	t.Helper()
	ctx := context.Background()
	repo := st.Repository()
	user, err := repo.CreateUser(ctx, CreateUserParams{Email: "temporary@example.com", DisplayName: "Temporary", PasswordHash: []byte("hash")})
	if err != nil {
		t.Fatal(err)
	}
	org, err := repo.GetPersonalOrganizationForUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	var targets []SSHTarget
	for _, alias := range []string{"first", "second"} {
		target, err := repo.CreateSSHTarget(ctx, CreateSSHTargetParams{
			OwnerType: OwnerOrganization, OwnerID: org.ID, Alias: alias, TargetType: TargetDirect,
			Host: "127.0.0.1", Port: 22, RemoteUsername: "root", AuthType: AuthPassword, CreatedBy: user.ID,
		})
		if err != nil {
			t.Fatal(err)
		}
		targets = append(targets, target)
	}
	return user, targets
}

func TestTemporarySSHAuthorizationEncryptedPersistence(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "temporary.db")
	key := []byte("temporary-authorization-test-key")
	st, err := Open(ctx, path, key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	user, targets := temporarySSHFixture(t, st)
	grant, err := st.Repository().CreateTemporarySSHAuthorization(ctx, CreateTemporarySSHAuthorizationParams{
		TargetID: targets[0].ID, Name: "  emergency  ", CreatedBy: user.ID, ExpiresAt: time.Now().Add(24 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{grant.ID, grant.Token} {
		parsed, err := uuid.Parse(value)
		if err != nil || parsed.Version() != 4 {
			t.Fatalf("expected random UUID: %q err=%v", value, err)
		}
	}
	if grant.ID == grant.Token || grant.Name != "emergency" {
		t.Fatalf("invalid grant identity or name: %#v", grant)
	}
	var encrypted, hash []byte
	if err := st.DB().QueryRowContext(ctx, `SELECT encrypted_token, token_hash FROM temporary_ssh_authorizations WHERE id = ?`, grant.ID).Scan(&encrypted, &hash); err != nil {
		t.Fatal(err)
	}
	wantHash := sha256.Sum256([]byte(grant.Token))
	if !bytes.HasPrefix(encrypted, []byte(secretBoxPrefix)) || bytes.Contains(encrypted, []byte(grant.Token)) || !bytes.Equal(hash, wantHash[:]) {
		t.Fatal("bearer token is not encrypted or lookup hash is incorrect")
	}
	if _, err := st.DB().ExecContext(ctx, `INSERT INTO temporary_ssh_authorizations
		(id, target_id, name, encrypted_token, token_hash, created_by, expires_at, created_at, updated_at)
		SELECT 'duplicate', target_id, name, encrypted_token, token_hash, created_by, expires_at, created_at, updated_at
		FROM temporary_ssh_authorizations WHERE id = ?`, grant.ID); err == nil {
		t.Fatal("duplicate token hash accepted")
	}
	for _, lookup := range []func() (TemporarySSHAuthorization, error){
		func() (TemporarySSHAuthorization, error) {
			return st.Repository().GetTemporarySSHAuthorization(ctx, grant.ID)
		},
		func() (TemporarySSHAuthorization, error) {
			return st.Repository().GetTemporarySSHAuthorizationByToken(ctx, grant.Token)
		},
	} {
		loaded, err := lookup()
		if err != nil || !reflect.DeepEqual(loaded, grant) {
			t.Fatalf("grant round trip: got %#v want %#v err=%v", loaded, grant, err)
		}
	}
	if _, err := st.Repository().GetTemporarySSHAuthorizationByToken(ctx, "invalid-token"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("invalid token lookup: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(grant.Token)) {
		t.Fatal("database file contains plaintext bearer token")
	}
	st, err = Open(ctx, path, key)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := st.Repository().GetTemporarySSHAuthorizationByToken(ctx, grant.Token)
	if err != nil || !reflect.DeepEqual(loaded, grant) {
		t.Fatalf("grant persistence: got %#v want %#v err=%v", loaded, grant, err)
	}
}

func TestTemporarySSHAuthorizationTargetIsolationAndRenewal(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, filepath.Join(t.TempDir(), "temporary.db"), []byte("test-key"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	user, targets := temporarySSHFixture(t, st)
	repo := st.Repository()
	now := time.Now().UTC()
	var grants []TemporarySSHAuthorization
	for i, target := range targets {
		expiry := now.Add(time.Hour)
		if i == 1 {
			expiry = now.Add(-time.Hour)
		}
		grant, err := repo.CreateTemporarySSHAuthorization(ctx, CreateTemporarySSHAuthorizationParams{TargetID: target.ID, Name: target.Alias, CreatedBy: user.ID, ExpiresAt: expiry})
		if err != nil {
			t.Fatal(err)
		}
		grants = append(grants, grant)
	}
	for i, target := range targets {
		listed, err := repo.ListTemporarySSHAuthorizations(ctx, target.ID)
		if err != nil || len(listed) != 1 || listed[0].ID != grants[i].ID || listed[0].Token != grants[i].Token {
			t.Fatalf("list target %s: got %#v err=%v", target.ID, listed, err)
		}
	}
	if _, err := repo.RenewTemporarySSHAuthorization(ctx, grants[0].ID, targets[1].ID, time.Hour, now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-target renewal: %v", err)
	}
	if err := repo.DeleteTemporarySSHAuthorization(ctx, grants[0].ID, targets[1].ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-target deletion: %v", err)
	}
	for _, duration := range []time.Duration{0, -time.Hour} {
		if _, err := repo.RenewTemporarySSHAuthorization(ctx, grants[0].ID, targets[0].ID, duration, now); err == nil {
			t.Fatalf("non-positive renewal accepted: %s", duration)
		}
	}
	for i, grant := range grants {
		before, err := repo.GetTemporarySSHAuthorization(ctx, grant.ID)
		if err != nil || !reflect.DeepEqual(before, grant) {
			t.Fatalf("failed operations changed grant: got %#v err=%v", before, err)
		}
		renewed, err := repo.RenewTemporarySSHAuthorization(ctx, grant.ID, grant.TargetID, 2*time.Hour, now)
		wantExpiry := now.Add(2 * time.Hour)
		if i == 0 {
			wantExpiry = grant.ExpiresAt.Add(2 * time.Hour)
		}
		if err != nil || !renewed.ExpiresAt.Equal(wantExpiry) || renewed.Token != grant.Token || !renewed.UpdatedAt.Equal(now) || !renewed.CreatedAt.Equal(grant.CreatedAt) {
			t.Fatalf("renewal: got %#v want expiry %s err=%v", renewed, wantExpiry, err)
		}
		loaded, err := repo.GetTemporarySSHAuthorizationByToken(ctx, grant.Token)
		if err != nil || !reflect.DeepEqual(loaded, renewed) {
			t.Fatalf("renewal persistence: got %#v err=%v", loaded, err)
		}
		if err := repo.DeleteTemporarySSHAuthorization(ctx, grant.ID, grant.TargetID); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.GetTemporarySSHAuthorizationByToken(ctx, grant.Token); !errors.Is(err, ErrNotFound) {
			t.Fatalf("deleted token lookup: %v", err)
		}
		if err := repo.DeleteTemporarySSHAuthorization(ctx, grant.ID, grant.TargetID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("repeat deletion: %v", err)
		}
	}
	if _, err := repo.RenewTemporarySSHAuthorization(ctx, "missing", targets[0].ID, time.Hour, now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing renewal: %v", err)
	}
}

func TestTemporarySSHAuthorizationForeignKeysAndCascades(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, filepath.Join(t.TempDir(), "temporary.db"), []byte("test-key"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	owner, targets := temporarySSHFixture(t, st)
	repo := st.Repository()
	creator, err := repo.CreateUser(ctx, CreateUserParams{Email: "creator@example.com", PasswordHash: []byte("hash")})
	if err != nil {
		t.Fatal(err)
	}
	for _, params := range []CreateTemporarySSHAuthorizationParams{
		{TargetID: "missing", CreatedBy: owner.ID, ExpiresAt: time.Now().Add(time.Hour)},
		{TargetID: targets[0].ID, CreatedBy: "missing", ExpiresAt: time.Now().Add(time.Hour)},
	} {
		if _, err := repo.CreateTemporarySSHAuthorization(ctx, params); err == nil {
			t.Fatal("authorization with missing foreign key accepted")
		}
	}
	var grants []TemporarySSHAuthorization
	for _, target := range targets {
		grant, err := repo.CreateTemporarySSHAuthorization(ctx, CreateTemporarySSHAuthorizationParams{TargetID: target.ID, CreatedBy: creator.ID, ExpiresAt: time.Now().Add(time.Hour)})
		if err != nil {
			t.Fatal(err)
		}
		grants = append(grants, grant)
	}
	if err := repo.DeleteSSHTarget(ctx, targets[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetTemporarySSHAuthorization(ctx, grants[0].ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("target cascade: %v", err)
	}
	if _, err := repo.GetTemporarySSHAuthorization(ctx, grants[1].ID); err != nil {
		t.Fatalf("target cascade removed unrelated grant: %v", err)
	}
	if err := repo.DeleteUser(ctx, creator.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetTemporarySSHAuthorization(ctx, grants[1].ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("creator cascade: %v", err)
	}
	if _, err := repo.GetSSHTarget(ctx, targets[1].ID); err != nil {
		t.Fatalf("creator cascade removed another user's target: %v", err)
	}
}

func TestTemporarySSHAuthorizationRequiresEncryptionKey(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, filepath.Join(t.TempDir(), "temporary.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	user, targets := temporarySSHFixture(t, st)
	if _, err := st.Repository().CreateTemporarySSHAuthorization(ctx, CreateTemporarySSHAuthorizationParams{TargetID: targets[0].ID, CreatedBy: user.ID, ExpiresAt: time.Now().Add(time.Hour)}); err == nil {
		t.Fatal("authorization accepted without encryption key")
	}
	listed, err := st.Repository().ListTemporarySSHAuthorizations(ctx, targets[0].ID)
	if err != nil || len(listed) != 0 {
		t.Fatalf("authorization persisted without encryption: %#v err=%v", listed, err)
	}
}

func TestTemporarySSHAuthorizationCascadesOnEveryConnection(t *testing.T) {
	for _, query := range []string{"", "?_pragma=cache_size(321)"} {
		t.Run(query, func(t *testing.T) {
			ctx := context.Background()
			st, err := Open(ctx, filepath.Join(t.TempDir(), "temporary.db")+query, []byte("test-key"))
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			_, targets := temporarySSHFixture(t, st)
			creator, err := st.Repository().CreateUser(ctx, CreateUserParams{Email: "creator@example.com", PasswordHash: []byte("hash")})
			if err != nil {
				t.Fatal(err)
			}
			var grants []TemporarySSHAuthorization
			for _, target := range targets {
				grant, err := st.Repository().CreateTemporarySSHAuthorization(ctx, CreateTemporarySSHAuthorizationParams{
					TargetID: target.ID, CreatedBy: creator.ID, ExpiresAt: time.Now().Add(time.Hour),
				})
				if err != nil {
					t.Fatal(err)
				}
				grants = append(grants, grant)
			}
			first, err := st.DB().Conn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer first.Close()
			second, err := st.DB().Conn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer second.Close()
			for i, conn := range []*sql.Conn{first, second} {
				var enabled int
				if err := conn.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&enabled); err != nil || enabled != 1 {
					t.Fatalf("connection %d foreign_keys=%d err=%v", i+1, enabled, err)
				}
				if query != "" {
					var cacheSize int
					if err := conn.QueryRowContext(ctx, "PRAGMA cache_size").Scan(&cacheSize); err != nil || cacheSize != 321 {
						t.Fatalf("connection %d lost existing DSN pragma: cache_size=%d err=%v", i+1, cacheSize, err)
					}
				}
			}
			if _, err := second.ExecContext(ctx, "DELETE FROM ssh_targets WHERE id = ?", targets[0].ID); err != nil {
				t.Fatal(err)
			}
			var count int
			if err := second.QueryRowContext(ctx, "SELECT COUNT(*) FROM temporary_ssh_authorizations WHERE id = ?", grants[0].ID).Scan(&count); err != nil || count != 0 {
				t.Fatalf("second-connection target cascade: count=%d err=%v", count, err)
			}
			if err := second.QueryRowContext(ctx, "SELECT COUNT(*) FROM temporary_ssh_authorizations WHERE id = ?", grants[1].ID).Scan(&count); err != nil || count != 1 {
				t.Fatalf("target cascade removed unrelated grant: count=%d err=%v", count, err)
			}
			if _, err := second.ExecContext(ctx, "DELETE FROM users WHERE id = ?", creator.ID); err != nil {
				t.Fatal(err)
			}
			if err := second.QueryRowContext(ctx, "SELECT COUNT(*) FROM temporary_ssh_authorizations WHERE id = ?", grants[1].ID).Scan(&count); err != nil || count != 0 {
				t.Fatalf("second-connection creator cascade: count=%d err=%v", count, err)
			}
			if err := second.QueryRowContext(ctx, "SELECT COUNT(*) FROM ssh_targets WHERE id = ?", targets[1].ID).Scan(&count); err != nil || count != 1 {
				t.Fatalf("creator cascade removed another user's target: count=%d err=%v", count, err)
			}
		})
	}
}
