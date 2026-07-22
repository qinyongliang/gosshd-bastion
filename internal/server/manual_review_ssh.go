package server

import (
	"context"
	"time"

	"github.com/qinyongliang/gosshd-bastion/internal/bastion"
	"github.com/qinyongliang/gosshd-bastion/internal/store"
)

func (a *App) reviewDeniedCommand(ctx context.Context, userID string, target store.SSHTarget, command string, decision bastion.Decision) bastion.Decision {
	return a.reviewDeniedCommandForSession(ctx, userID, target, command, decision, "")
}

func (a *App) reviewDeniedCommandForSession(ctx context.Context, userID string, target store.SSHTarget, command string, decision bastion.Decision, sessionID string) bastion.Decision {
	if !decision.AllowManualReview {
		return decision
	}
	organizationID := organizationIDForTarget(target)
	user, err := a.store.Repository().GetUser(ctx, userID)
	if err != nil {
		return decision
	}
	timeout := time.Duration(decision.ManualReviewTimeoutSeconds) * time.Second
	review, decided := a.manualReviews.Create(manualReviewRequest{
		OrganizationID:  organizationID,
		SessionID:       sessionID,
		TargetID:        target.ID,
		TargetName:      target.Name,
		TargetAlias:     target.Alias,
		UserID:          user.ID,
		UserEmail:       user.Email,
		UserDisplayName: user.DisplayName,
		Command:         command,
		Reason:          decision.Reason,
	}, timeout)
	select {
	case result, ok := <-decided:
		if ok && result.Allow {
			decision.Action = store.DecisionAllow
			decision.Reason = "manual approved by " + manualReviewerLabel(result) + ": " + decision.Reason
			decision.AllowManualReview = false
			return decision
		}
		if ok {
			decision.Reason = "manual rejected by " + manualReviewerLabel(result) + ": " + decision.Reason
			return decision
		}
		decision.Reason = "manual review expired: " + decision.Reason
		return decision
	case <-ctx.Done():
		a.manualReviews.Expire(review.ID)
		decision.Reason = "manual review cancelled: " + decision.Reason
		return decision
	}
}

func manualReviewerLabel(result manualReviewDecision) string {
	if result.Reviewer != "" {
		return result.Reviewer
	}
	return result.ReviewerID
}
