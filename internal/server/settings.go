package server

import (
	"context"
	"encoding/json"
	"strings"
)

const (
	settingAuth     = "auth"
	settingBranding = "branding"
	settingDingTalk = "dingtalk"
	settingLDAP     = "ldap"
)

type authSettings struct {
	PublicRegistration bool `json:"public_registration"`
}

type brandingSettings struct {
	AppName        string `json:"app_name"`
	AppDescription string `json:"app_description"`
	AppIcon        string `json:"app_icon"`
}

func defaultBrandingSettings() brandingSettings {
	return brandingSettings{
		AppName:        "gosshd",
		AppDescription: "AI service bastion",
	}
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

func (a *App) loadBrandingSettings(ctx context.Context) (brandingSettings, error) {
	a.brandingCacheMu.RLock()
	if a.brandingCacheValid {
		out := a.brandingCache
		a.brandingCacheMu.RUnlock()
		return out, nil
	}
	a.brandingCacheMu.RUnlock()

	out := defaultBrandingSettings()
	if a.store == nil {
		return out, nil
	}
	setting, err := a.store.Repository().GetSystemSetting(ctx, settingBranding)
	if isNotFound(err) {
		return out, nil
	}
	if err != nil {
		return brandingSettings{}, err
	}
	if err := json.Unmarshal([]byte(setting.ValueJSON), &out); err != nil {
		return brandingSettings{}, err
	}
	out = normalizeBrandingSettings(out)
	a.brandingCacheMu.Lock()
	a.brandingCache = out
	a.brandingCacheValid = true
	a.brandingCacheMu.Unlock()
	return out, nil
}

func normalizeBrandingSettings(value brandingSettings) brandingSettings {
	defaults := defaultBrandingSettings()
	value.AppName = strings.TrimSpace(value.AppName)
	value.AppDescription = strings.TrimSpace(value.AppDescription)
	value.AppIcon = strings.TrimSpace(value.AppIcon)
	if value.AppName == "" {
		value.AppName = defaults.AppName
	}
	if value.AppDescription == "" {
		value.AppDescription = defaults.AppDescription
	}
	return value
}

func (a *App) clearBrandingCache() {
	a.brandingCacheMu.Lock()
	a.brandingCache = brandingSettings{}
	a.brandingCacheValid = false
	a.brandingCacheMu.Unlock()
}
