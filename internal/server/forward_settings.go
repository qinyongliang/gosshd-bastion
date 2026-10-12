package server

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/qinyongliang/gosshd-bastion/internal/store"
	"net/http"
)

const settingForward = "forward"

type forwardSettings struct {
	RetentionDays int `json:"retention_days"`
}

func (a *App) loadForwardSettings(ctx context.Context) (forwardSettings, error) {
	s, err := a.store.Repository().GetSystemSetting(ctx, settingForward)
	if isNotFound(err) {
		return forwardSettings{7}, nil
	}
	if err != nil {
		return forwardSettings{}, err
	}
	var out forwardSettings
	if err = json.Unmarshal([]byte(s.ValueJSON), &out); err != nil {
		return out, err
	}
	if out.RetentionDays < 1 || out.RetentionDays > 365 {
		return forwardSettings{}, errors.New("invalid stored forward log retention")
	}
	return out, nil
}
func (a *App) saveForwardSettings(ctx context.Context, actor store.User, in forwardSettings) error {
	if !actor.IsSystemAdmin {
		return errors.New("system administrator required")
	}
	if in.RetentionDays < 1 || in.RetentionDays > 365 {
		return errors.New("retention_days must be between 1 and 365")
	}
	data, _ := json.Marshal(in)
	if err := a.store.Repository().UpsertSystemSetting(ctx, settingForward, data, actor.ID); err != nil {
		return err
	}
	a.forwards.notify()
	return nil
}
func (a *App) handleForwardSettings(w http.ResponseWriter, r *http.Request, u store.User) {
	if r.Method == http.MethodPut {
		var in forwardSettings
		if err := readJSON(r, &in); err != nil {
			writeError(w, 400, err.Error())
			return
		}
		if err := a.saveForwardSettings(r.Context(), u, in); err != nil {
			writeError(w, 400, err.Error())
			return
		}
		writeJSON(w, 200, in)
		return
	}
	settings, err := a.loadForwardSettings(r.Context())
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, settings)
}
