package server

import (
	"context"
	"encoding/json"
)

const (
	settingAuth     = "auth"
	settingDingTalk = "dingtalk"
	settingLDAP     = "ldap"
)

type authSettings struct {
	PublicRegistration bool `json:"public_registration"`
}

func (a *App) loadAuthSettings(ctx context.Context) (authSettings, error) {
	setting, err := a.store.Repository().GetSystemSetting(ctx, settingAuth)
	if isNotFound(err) {
		return authSettings{}, nil
	}
	if err != nil {
		return authSettings{}, err
	}
	var out authSettings
	if err := json.Unmarshal([]byte(setting.ValueJSON), &out); err != nil {
		return authSettings{}, err
	}
	return out, nil
}
