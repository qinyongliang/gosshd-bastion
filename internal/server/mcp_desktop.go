package server

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/qinyongliang/gosshd-bastion/internal/desktop"
	desktopmcp "github.com/qinyongliang/gosshd-bastion/internal/desktop/mcptools"
	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
	"github.com/qinyongliang/gosshd-bastion/internal/store"
	"io"
	"time"
)

// This adapter is the only bridge from the independent desktop module into
// GOSSHD identities, Agent transport and persistence. SSH policy is unchanged.
func (a *App) newDesktopService() *desktop.Service {
	return desktop.NewService(desktop.Adapter{
		Authorize: a.authorizeDesktop,
		Dial: func(ctx context.Context, targetID string) (io.ReadWriteCloser, error) {
			target, err := a.store.Repository().GetSSHTarget(ctx, targetID)
			if err != nil {
				return nil, err
			}
			agent, err := a.registry.Get(target.AgentID)
			if err != nil {
				return nil, err
			}

			type opened struct {
				stream io.ReadWriteCloser
				err    error
			}
			ready := make(chan opened)
			go func() {
				stream, err := agent.OpenStream()
				select {
				case ready <- opened{stream, err}:
				case <-ctx.Done():
					if stream != nil {
						stream.Close()
					}
				}
			}()
			var result opened
			select {
			case result = <-ready:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			if result.err != nil {
				return nil, result.err
			}
			stream := result.stream
			if err = protocol.WriteJSONLine(stream, protocol.StreamRequest{Type: protocol.StreamDesktop}); err != nil {
				stream.Close()
				return nil, err
			}
			return stream, nil

		},
		Audit: func(ctx context.Context, e desktop.Event) error {
			if err := a.ensureServices(ctx); err != nil {
				return err
			}
			return a.audit.Repository().SaveDesktopAudit(ctx, store.DesktopAudit{ID: e.ID, UserID: e.ActorID, TargetID: e.TargetID, SessionID: e.SessionID, Method: e.Method, Outcome: e.Outcome, StartedAt: e.At})
		},
	})
}
func (a *App) desktopTarget(ctx context.Context, actorID, targetID string) (store.User, store.SSHTarget, error) {
	if err := a.ensureServices(ctx); err != nil {
		return store.User{}, store.SSHTarget{}, err
	}
	user, err := a.store.Repository().GetUser(ctx, actorID)
	if err != nil || user.DisabledAt != nil {
		return store.User{}, store.SSHTarget{}, errors.New("desktop account is unavailable")
	}
	target, err := a.store.Repository().GetSSHTarget(ctx, targetID)
	if err != nil {
		return user, target, err
	}
	if target.TargetType != store.TargetAgent || target.AgentID == "" {
		return user, target, errors.New("desktop requires an Agent target")
	}
	if !user.IsSystemAdmin {
		if _, err = a.store.Repository().GetOrganizationMember(ctx, target.OwnerID, user.ID); err != nil {
			return user, target, errors.New("desktop target organization access required")
		}
		visible, err := a.targetVisibleToUser(ctx, target, user)
		if err != nil || !visible {
			return user, target, errors.New("desktop target access denied")
		}
	}
	return user, target, nil
}
func (a *App) authorizeDesktop(ctx context.Context, actorID, targetID string, control bool) error {
	_, target, err := a.desktopTarget(ctx, actorID, targetID)
	if err != nil {
		return err
	}
	grant, err := a.store.Repository().GetDesktopAccess(ctx, actorID, target.ID)
	if err != nil {
		return err
	}
	if !grant.AllowRead || (control && !grant.AllowControl) || (grant.ExpiresAt != nil && !time.Now().Before(*grant.ExpiresAt)) {
		return errors.New("explicit desktop read/control authorization required")
	}
	if info, online := a.registry.Info(target.AgentID); online && info.GOOS != "windows" && info.GOOS != "darwin" {
		return errors.New("desktop requires a Windows or macOS Agent")
	}
	return nil
}

type desktopAccessInput struct {
	TargetID string `json:"target_id"`
	UserID   string `json:"user_id,omitempty"`
}
type desktopAccessSetInput struct {
	TargetID     string `json:"target_id"`
	UserID       string `json:"user_id"`
	AllowRead    bool   `json:"allow_read"`
	AllowControl bool   `json:"allow_control"`
	ExpiresAt    string `json:"expires_at,omitempty"`
}

func (a *App) canManageDesktopAccess(ctx context.Context, actor store.User, target store.SSHTarget) error {
	if actor.IsSystemAdmin {
		return nil
	}
	return a.requireOrganizationAdmin(ctx, target.OwnerID, actor)
}
func (a *App) addMCPDesktopTools(s *mcp.Server, actor store.User) {
	desktopmcp.RegisterMCP(s, a.desktopSessions, actor.ID)
	mcp.AddTool(s, &mcp.Tool{Name: "desktop_access_get", Description: "Read your target-specific desktop grant. Organization/system administrators may inspect another member's grant. Desktop read and control are independent of SSH command policies.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, func(ctx context.Context, _ *mcp.CallToolRequest, in desktopAccessInput) (*mcp.CallToolResult, store.DesktopAccess, error) {
		current, target, err := a.desktopTarget(ctx, actor.ID, in.TargetID)
		if err != nil {
			return nil, store.DesktopAccess{}, err
		}
		if in.UserID == "" {
			in.UserID = actor.ID
		}
		if in.UserID != actor.ID {
			if err = a.canManageDesktopAccess(ctx, current, target); err != nil {
				return nil, store.DesktopAccess{}, err
			}
		}
		access, err := a.store.Repository().GetDesktopAccess(ctx, in.UserID, target.ID)
		return nil, access, err
	})
	mcp.AddTool(s, &mcp.Tool{Name: "desktop_access_set", Description: "Explicitly grant or revoke a member's target-scoped desktop read/control access. Requires organization/system administrator authority; control requires read. expires_at is RFC3339, at most one year; omitted expiry defaults to 24 hours. Existing sessions are closed on any change."}, func(ctx context.Context, _ *mcp.CallToolRequest, in desktopAccessSetInput) (*mcp.CallToolResult, store.DesktopAccess, error) {
		current, target, err := a.desktopTarget(ctx, actor.ID, in.TargetID)
		if err != nil {
			return nil, store.DesktopAccess{}, err
		}
		if err = a.canManageDesktopAccess(ctx, current, target); err != nil {
			return nil, store.DesktopAccess{}, err
		}
		member, err := a.store.Repository().GetUser(ctx, in.UserID)
		if err != nil {
			return nil, store.DesktopAccess{}, err
		}
		// Revocation must still work for disabled users or former members.
		if in.AllowRead || in.AllowControl {
			member, _, err = a.desktopTarget(ctx, in.UserID, in.TargetID)
			if err != nil {
				return nil, store.DesktopAccess{}, err
			}
		}
		expiry := time.Now().UTC().Add(24 * time.Hour)
		if in.ExpiresAt != "" {
			expiry, err = time.Parse(time.RFC3339, in.ExpiresAt)
			if err != nil {
				return nil, store.DesktopAccess{}, errors.New("expires_at must be RFC3339")
			}
		}
		grant := store.DesktopAccess{UserID: member.ID, TargetID: target.ID, AllowRead: in.AllowRead, AllowControl: in.AllowControl, ExpiresAt: &expiry, GrantedBy: current.ID}
		event := store.DesktopAudit{ID: uuid.NewString(), UserID: current.ID, TargetID: target.ID, Method: "access_set", Outcome: "started", StartedAt: time.Now().UTC()}
		if err = a.audit.Repository().SaveDesktopAudit(ctx, event); err != nil {
			return nil, store.DesktopAccess{}, err
		}
		if err = a.store.Repository().SetDesktopAccess(ctx, grant); err != nil {
			return nil, store.DesktopAccess{}, err
		}
		a.desktopSessions.Revoke(member.ID, target.ID)
		event.Outcome = "ok"
		if err = a.audit.Repository().SaveDesktopAudit(ctx, event); err != nil {
			return nil, store.DesktopAccess{}, errors.New("grant changed but audit completion failed; inspect access before retrying")
		}
		saved, err := a.store.Repository().GetDesktopAccess(ctx, member.ID, target.ID)
		return nil, saved, err
	})
	mcp.AddTool(s, &mcp.Tool{Name: "desktop_audit_list", Description: "List your desktop audit metadata only. Screenshots, accessibility text and typed input are never stored here.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		TargetID string `json:"target_id,omitempty"`
		Limit    int    `json:"limit,omitempty"`
	}) (*mcp.CallToolResult, struct {
		Events []store.DesktopAudit `json:"events"`
	}, error) {
		out := struct {
			Events []store.DesktopAudit `json:"events"`
		}{}
		if err := a.ensureServices(ctx); err != nil {
			return nil, out, err
		}
		var err error
		out.Events, err = a.audit.Repository().ListDesktopAudit(ctx, actor.ID, in.TargetID, in.Limit)
		return nil, out, err
	})
}

func desktopMCPInstructions() string { return desktopmcp.OperatorInstructions }
