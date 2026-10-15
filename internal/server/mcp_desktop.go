package server

import (
	"context"
	"errors"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/qinyongliang/gosshd-bastion/internal/desktop"
	desktopmcp "github.com/qinyongliang/gosshd-bastion/internal/desktop/mcptools"
	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
	"github.com/qinyongliang/gosshd-bastion/internal/store"
	"io"
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
func (a *App) authorizeDesktop(ctx context.Context, actorID, targetID string, _ bool) error {
	_, target, err := a.desktopTarget(ctx, actorID, targetID)
	if err != nil {
		return err
	}
	if info, online := a.registry.Info(target.AgentID); online && info.GOOS != "windows" && info.GOOS != "darwin" && info.GOOS != "android" {
		return errors.New("desktop requires a Windows, macOS or Android Agent")
	}
	return nil
}

type desktopAccessInput struct {
	TargetID string `json:"target_id"`
	UserID   string `json:"user_id,omitempty"`
}
type desktopAccessView struct {
	TargetID      string `json:"target_id"`
	UserID        string `json:"user_id"`
	AllowRead     bool   `json:"allow_read"`
	AllowControl  bool   `json:"allow_control"`
	Authorization string `json:"authorization"`
}

func (a *App) canManageDesktopAccess(ctx context.Context, actor store.User, target store.SSHTarget) error {
	if actor.IsSystemAdmin {
		return nil
	}
	return a.requireOrganizationAdmin(ctx, target.OwnerID, actor)
}
func (a *App) addMCPDesktopTools(s *mcp.Server, actor store.User) {
	desktopmcp.RegisterMCP(s, a.desktopSessions, actor.ID)
	mcp.AddTool(s, &mcp.Tool{Name: "desktop_access_get", Description: "Inspect desktop eligibility inherited from existing account, organization and target access. No separate desktop grant is needed. Organization/system administrators may inspect another member. Native OS permissions and the Agent desktop opt-out are separate.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, func(ctx context.Context, _ *mcp.CallToolRequest, in desktopAccessInput) (*mcp.CallToolResult, desktopAccessView, error) {
		current, target, err := a.desktopTarget(ctx, actor.ID, in.TargetID)
		if err != nil {
			return nil, desktopAccessView{}, err
		}
		if in.UserID == "" {
			in.UserID = actor.ID
		}
		if in.UserID != actor.ID {
			if err = a.canManageDesktopAccess(ctx, current, target); err != nil {
				return nil, desktopAccessView{}, err
			}
		}
		if err = a.authorizeDesktop(ctx, in.UserID, target.ID, false); err != nil {
			return nil, desktopAccessView{}, err
		}
		return nil, desktopAccessView{TargetID: target.ID, UserID: in.UserID, AllowRead: true, AllowControl: true, Authorization: "target_access"}, nil
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

func desktopMCPInstructions() string {
	return "Desktop read/control inherits existing account, organization and target access. No separate desktop grant or desktop_access_set call is required. When the user requests desktop work, start directly with desktop_session_open rather than asking them to authorize desktop access again. Windows/macOS Agents enable the module by default unless explicitly disabled. Check the returned native OS permission state; missing macOS permissions can be requested with desktop_request_permissions, but the local user must approve the OS prompt. " + desktopmcp.OperatorInstructions
}
