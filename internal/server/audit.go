package server

import (
	"context"
	"errors"
	"log"
	"path/filepath"
	"strings"

	"github.com/qinyongliang/gosshd-bastion/internal/store"
)

const temporaryAuthorizationAuditPrefix = "temporary-authorization:"

func normalizeAuditIdentity(params *store.CreateCommandAuditLogParams) {
	if strings.HasPrefix(params.PublicKeyFingerprint, temporaryAuthorizationAuditPrefix) {
		params.PublicKeyName = strings.TrimPrefix(params.PublicKeyFingerprint, temporaryAuthorizationAuditPrefix)
		if params.PublicKeyName == "" {
			params.PublicKeyName = "Temporary SSH authorization"
		}
		params.PublicKeyFingerprint = ""
	}
}

func (a *App) createAuditLog(ctx context.Context, params store.CreateCommandAuditLogParams) (store.CommandAuditLog, error) {
	normalizeAuditIdentity(&params)
	defer func() {
		if a.runningAudits != nil && params.ID != "" {
			a.runningAudits.remove(a.runningAudits.get(params.ID))
		}
	}()
	if err := a.ensureServices(ctx); err != nil {
		return store.CommandAuditLog{}, err
	}
	if strings.TrimSpace(params.UserID) != "" && (params.UserEmail == "" || params.UserDisplayName == "") {
		if user, err := a.store.Repository().GetUser(ctx, params.UserID); err == nil {
			params.UserEmail = user.Email
			params.UserDisplayName = user.DisplayName
		} else if !errors.Is(err, store.ErrNotFound) {
			return store.CommandAuditLog{}, err
		}
	}
	if strings.TrimSpace(params.TargetID) != "" && (params.TargetAlias == "" || params.TargetHost == "") {
		if target, err := a.store.Repository().GetSSHTarget(ctx, params.TargetID); err == nil {
			params.TargetName = target.Name
			params.TargetAlias = target.Alias
			params.TargetHost = target.Host
			params.TargetPort = target.Port
			params.TargetUsername = target.RemoteUsername
			if params.OrganizationID == "" {
				params.OrganizationID = organizationIDForTarget(target)
			}
		} else if !errors.Is(err, store.ErrNotFound) {
			return store.CommandAuditLog{}, err
		}
	}
	if strings.TrimSpace(params.PublicKeyFingerprint) != "" && params.PublicKeyName == "" {
		if key, err := a.store.Repository().GetPublicKeyByFingerprint(ctx, params.PublicKeyFingerprint); err == nil {
			params.PublicKeyName = key.Name
		} else if !errors.Is(err, store.ErrNotFound) {
			return store.CommandAuditLog{}, err
		}
	}
	entry, err := a.audit.Repository().CreateCommandAuditLog(ctx, params)
	if err != nil {
		log.Printf("persist command audit failed: %v", err)
	}
	return entry, err
}

func (a *App) absoluteRecordingPath(rel string) string {
	rel = filepath.Clean(strings.TrimSpace(rel))
	if rel == "." || rel == "" || filepath.IsAbs(rel) {
		return ""
	}
	return filepath.Join(a.auditRecordingsPath, rel)
}
