package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/qinyongliang/gosshd-bastion/internal/store"
)

type apiManualReview struct {
	ID                 string   `json:"id"`
	OrganizationID     string   `json:"organization_id"`
	SessionID          string   `json:"session_id,omitempty"`
	TargetID           string   `json:"target_id"`
	TargetName         string   `json:"target_name"`
	TargetAlias        string   `json:"target_alias"`
	UserID             string   `json:"user_id"`
	UserEmail          string   `json:"user_email"`
	UserDisplayName    string   `json:"user_display_name"`
	Command            string   `json:"command"`
	Reason             string   `json:"reason"`
	CreatedAt          string   `json:"created_at"`
	ExpiresAt          string   `json:"expires_at"`
	DefaultAllow       bool     `json:"default_allow"`
	AutoAllowMinutes   int      `json:"auto_allow_minutes,omitempty"`
	AutoAllowExpiresAt string   `json:"auto_allow_expires_at,omitempty"`
	AutoAllowTargetIDs []string `json:"auto_allow_target_ids,omitempty"`
}

type apiManualReviewsResponse struct {
	Reviews []apiManualReview `json:"reviews"`
}

type apiManualReviewDecisionResponse struct {
	OK                 bool     `json:"ok"`
	AutoAllowMinutes   int      `json:"auto_allow_minutes,omitempty"`
	AutoAllowExpiresAt string   `json:"auto_allow_expires_at,omitempty"`
	AutoAllowTargetIDs []string `json:"auto_allow_target_ids,omitempty"`
}

type apiManualReviewChoiceResponse struct {
	Active             bool     `json:"active"`
	Allow              bool     `json:"allow"`
	AutoAllowMinutes   int      `json:"auto_allow_minutes,omitempty"`
	AutoAllowExpiresAt string   `json:"auto_allow_expires_at,omitempty"`
	AutoAllowTargetIDs []string `json:"auto_allow_target_ids,omitempty"`
}

func (a *App) handleListManualReviews(w http.ResponseWriter, r *http.Request, user store.User) {
	orgID := strings.TrimSpace(r.URL.Query().Get("organization_id"))
	if orgID == "" {
		writeError(w, http.StatusBadRequest, "organization_id is required")
		return
	}
	if err := a.requireOrganizationAdmin(r.Context(), orgID, user); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	sessionID := strings.TrimSpace(r.URL.Query().Get("session_id"))
	reviews, err := a.manualReviews.List(r.Context(), orgID, sessionID, manualReviewPollTimeout(r), manualReviewKnownIDs(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := apiManualReviewsResponse{Reviews: []apiManualReview{}}
	for _, review := range reviews {
		out.Reviews = append(out.Reviews, apiManualReviewFromSnapshot(review))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *App) handleDecideManualReview(w http.ResponseWriter, r *http.Request, user store.User) {
	review, ok := a.manualReviews.Get(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, errManualReviewNotFound.Error())
		return
	}
	if err := a.requireOrganizationAdmin(r.Context(), review.OrganizationID, user); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	var req struct {
		Allow              bool     `json:"allow"`
		AutoAllowMinutes   *int     `json:"auto_allow_minutes"`
		AutoAllowTargetIDs []string `json:"auto_allow_target_ids"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.AutoAllowMinutes != nil {
		if *req.AutoAllowMinutes < 0 || *req.AutoAllowMinutes > 1440 {
			writeError(w, http.StatusBadRequest, "auto_allow_minutes must be between 0 and 1440")
			return
		}
		if *req.AutoAllowMinutes > 0 {
			var err error
			req.AutoAllowTargetIDs, err = a.validateManualReviewTargets(r.Context(), review.OrganizationID, review.UserID, req.AutoAllowTargetIDs)
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
		}
	}
	if err := a.manualReviews.DecideWithAutoAllow(review.ID, manualReviewDecision{
		Allow:      req.Allow,
		ReviewerID: user.ID,
		Reviewer:   user.DisplayName,
	}, req.AutoAllowMinutes, req.AutoAllowTargetIDs); err != nil {
		if errors.Is(err, errManualReviewNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	response := apiManualReviewDecisionResponse{OK: true}
	if state, ok := a.manualReviews.AutoAllowState(review.OrganizationID, review.UserID); ok {
		response.AutoAllowMinutes = state.Minutes
		response.AutoAllowExpiresAt = state.ExpiresAt.Format(time.RFC3339)
		response.AutoAllowTargetIDs = state.TargetIDs
	}
	writeJSON(w, http.StatusOK, response)
}

func (a *App) handleGetManualReviewChoice(w http.ResponseWriter, r *http.Request, user store.User) {
	organizationID := strings.TrimSpace(r.URL.Query().Get("organization_id"))
	userID := strings.TrimSpace(r.URL.Query().Get("user_id"))
	if organizationID == "" || userID == "" {
		writeError(w, http.StatusBadRequest, "organization_id and user_id are required")
		return
	}
	if err := a.requireOrganizationAdmin(r.Context(), organizationID, user); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	if _, err := a.store.Repository().GetOrganizationMember(r.Context(), organizationID, userID); err != nil {
		writeError(w, http.StatusBadRequest, "SSH user is not an organization member")
		return
	}
	writeJSON(w, http.StatusOK, apiManualReviewChoiceFromState(a.manualReviews.AutoAllowState(organizationID, userID)))
}

func (a *App) handlePutManualReviewChoice(w http.ResponseWriter, r *http.Request, user store.User) {
	var req struct {
		OrganizationID     string   `json:"organization_id"`
		UserID             string   `json:"user_id"`
		AutoAllowMinutes   int      `json:"auto_allow_minutes"`
		AutoAllowTargetIDs []string `json:"auto_allow_target_ids"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	req.OrganizationID = strings.TrimSpace(req.OrganizationID)
	req.UserID = strings.TrimSpace(req.UserID)
	if req.OrganizationID == "" || req.UserID == "" {
		writeError(w, http.StatusBadRequest, "organization_id and user_id are required")
		return
	}
	if err := a.requireOrganizationAdmin(r.Context(), req.OrganizationID, user); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	if req.AutoAllowMinutes < 1 || req.AutoAllowMinutes > 1440 {
		writeError(w, http.StatusBadRequest, "auto_allow_minutes must be between 1 and 1440")
		return
	}
	targetIDs, err := a.validateManualReviewTargets(r.Context(), req.OrganizationID, req.UserID, req.AutoAllowTargetIDs)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	state := a.manualReviews.SetAutoAllow(req.OrganizationID, req.UserID, req.AutoAllowMinutes, true, targetIDs)
	writeJSON(w, http.StatusOK, apiManualReviewChoiceResponse{
		Active:             true,
		Allow:              state.Allow,
		AutoAllowMinutes:   state.Minutes,
		AutoAllowExpiresAt: state.ExpiresAt.Format(time.RFC3339),
		AutoAllowTargetIDs: state.TargetIDs,
	})
}

func (a *App) validateManualReviewTargets(ctx context.Context, organizationID, userID string, targetIDs []string) ([]string, error) {
	if _, err := a.store.Repository().GetOrganizationMember(ctx, organizationID, userID); err != nil {
		return nil, errors.New("SSH user is not an organization member")
	}
	unique := make([]string, 0, len(targetIDs))
	seen := make(map[string]struct{}, len(targetIDs))
	for _, rawID := range targetIDs {
		targetID := strings.TrimSpace(rawID)
		if targetID == "" {
			continue
		}
		if _, ok := seen[targetID]; ok {
			continue
		}
		target, err := a.store.Repository().GetSSHTarget(ctx, targetID)
		if err != nil || organizationIDForTarget(target) != organizationID {
			return nil, fmt.Errorf("target %q is not in the organization", targetID)
		}
		seen[targetID] = struct{}{}
		unique = append(unique, targetID)
	}
	if len(unique) == 0 {
		return nil, errors.New("auto_allow_target_ids must contain at least one target")
	}
	return unique, nil
}

func apiManualReviewChoiceFromState(state manualReviewAutoAllow, ok bool) apiManualReviewChoiceResponse {
	if !ok {
		return apiManualReviewChoiceResponse{Active: false}
	}
	return apiManualReviewChoiceResponse{
		Active:             true,
		Allow:              state.Allow,
		AutoAllowMinutes:   state.Minutes,
		AutoAllowExpiresAt: state.ExpiresAt.Format(time.RFC3339),
		AutoAllowTargetIDs: state.TargetIDs,
	}
}

func manualReviewPollTimeout(r *http.Request) time.Duration {
	seconds, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("timeout_seconds")))
	if err != nil || seconds < 0 {
		seconds = 25
	}
	if seconds > 60 {
		seconds = 60
	}
	return time.Duration(seconds) * time.Second
}

func manualReviewKnownIDs(r *http.Request) map[string]struct{} {
	raw := strings.TrimSpace(r.URL.Query().Get("known_ids"))
	if raw == "" {
		return nil
	}
	out := map[string]struct{}{}
	for _, item := range strings.Split(raw, ",") {
		id := strings.TrimSpace(item)
		if id != "" {
			out[id] = struct{}{}
		}
	}
	return out
}

func apiManualReviewFromSnapshot(review manualReviewSnapshot) apiManualReview {
	return apiManualReview{
		ID:                 review.ID,
		OrganizationID:     review.OrganizationID,
		SessionID:          review.SessionID,
		TargetID:           review.TargetID,
		TargetName:         review.TargetName,
		TargetAlias:        review.TargetAlias,
		UserID:             review.UserID,
		UserEmail:          review.UserEmail,
		UserDisplayName:    review.UserDisplayName,
		Command:            review.Command,
		Reason:             review.Reason,
		CreatedAt:          review.CreatedAt.Format(time.RFC3339),
		ExpiresAt:          review.ExpiresAt.Format(time.RFC3339),
		DefaultAllow:       review.DefaultAllow,
		AutoAllowMinutes:   review.AutoAllowMinutes,
		AutoAllowExpiresAt: formatOptionalTime(review.AutoAllowExpiresAt),
		AutoAllowTargetIDs: review.AutoAllowTargetIDs,
	}
}

func formatOptionalTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format(time.RFC3339)
}
