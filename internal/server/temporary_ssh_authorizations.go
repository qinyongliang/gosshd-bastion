package server

import (
	"context"
	"errors"
	"time"

	"github.com/qinyongliang/gosshd-bastion/internal/store"
	gossh "golang.org/x/crypto/ssh"
)

var errTemporarySSHAuthorization = errors.New("temporary SSH authorization is not valid")

func (a *App) temporarySSHAuthorizationTarget(ctx context.Context, id string) (store.TemporarySSHAuthorization, store.SSHTarget, error) {
	if err := a.ensureServices(ctx); err != nil {
		return store.TemporarySSHAuthorization{}, store.SSHTarget{}, err
	}
	grant, err := a.store.Repository().GetTemporarySSHAuthorization(ctx, id)
	if err != nil || !time.Now().Before(grant.ExpiresAt) {
		return store.TemporarySSHAuthorization{}, store.SSHTarget{}, errTemporarySSHAuthorization
	}
	user, err := a.store.Repository().GetUser(ctx, grant.CreatedBy)
	if err != nil || user.DisabledAt != nil {
		return store.TemporarySSHAuthorization{}, store.SSHTarget{}, errTemporarySSHAuthorization
	}
	target, err := a.targetForUser(ctx, grant.TargetID, user)
	if err != nil {
		return store.TemporarySSHAuthorization{}, store.SSHTarget{}, errTemporarySSHAuthorization
	}
	return grant, target, nil
}

func (a *App) watchTemporarySSHAuthorization(conn *gossh.ServerConn, id string, done <-chan struct{}) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			if _, _, err := a.temporarySSHAuthorizationTarget(context.Background(), id); err != nil {
				_ = conn.Close()
				return
			}
		}
	}
}
