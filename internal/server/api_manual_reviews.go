package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/qinyongliang/gosshd-bastion/internal/store"
)

type apiManualReview struct {
	ID                 string `json:"id"`
	OrganizationID     string `json:"organization_id"`
	SessionID          string `json:"session_id,omitempty"`
	TargetID           string `json:"target_id"`
	TargetName         string `json:"target_name"`
	TargetAlias        string `json:"target_alias"`
	UserID             string `json:"user_id"`
	UserEmail          string `json:"user_email"`
	UserDisplayName    string `json:"user_display_name"`
	Command            string `json:"command"`
	Reason             string `json:"reason"`
	CreatedAt          string `json:"created_at"`
	ExpiresAt          string `json:"expires_at"`
	DefaultAllow       bool   `json:"default_allow"`
	AutoAllowMinutes   int    `json:"auto_allow_minutes,omitempty"`
	AutoAllowExpiresAt string `json:"auto_allow_expires_at,omitempty"`
}

type apiManualReviewsResponse struct {
	Reviews []apiManualReview `json:"reviews"`
}

type apiManualReviewDecisionResponse struct {
	OK                 bool   `json:"ok"`
	AutoAllowMinutes   int    `json:"auto_allow_minutes,omitempty"`
	AutoAllowExpiresAt string `json:"auto_allow_expires_at,omitempty"`
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
		Allow            bool `json:"allow"`
		AutoAllowMinutes *int `json:"auto_allow_minutes"`
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
	}
	if err := a.manualReviews.DecideWithAutoAllow(review.ID, manualReviewDecision{
		Allow:      req.Allow,
		ReviewerID: user.ID,
		Reviewer:   user.DisplayName,
	}, req.AutoAllowMinutes); err != nil {
		if errors.Is(err, errManualReviewNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	response := apiManualReviewDecisionResponse{OK: true}
	if state, ok := a.manualReviews.AutoAllowState(review.OrganizationID, review.SessionID); ok {
		response.AutoAllowMinutes = state.Minutes
		response.AutoAllowExpiresAt = state.ExpiresAt.Format(time.RFC3339)
	}
	writeJSON(w, http.StatusOK, response)
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
	}
}

func formatOptionalTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format(time.RFC3339)
}
