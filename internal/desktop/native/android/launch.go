package android

import (
	"context"
	"errors"
	"strings"
)

func validPackageName(value string) bool {
	if len(value) < 3 || len(value) > 80 {
		return false
	}
	segments := strings.Split(value, ".")
	if len(segments) < 2 {
		return false
	}
	for _, segment := range segments {
		if segment == "" {
			return false
		}
		for i, r := range segment {
			letter := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
			digit := r >= '0' && r <= '9'
			if !letter && r != '_' && (!digit || i == 0) {
				return false
			}
		}
	}
	return true
}
func validComponent(value string) bool {
	parts := strings.Split(value, "/")
	if len(parts) != 2 || !validPackageName(parts[0]) || parts[1] == "" {
		return false
	}
	for _, r := range parts[1] {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '$') {
			return false
		}
	}
	return true
}
func (h *Helper) launchApp(ctx context.Context, app string) error {
	if !validPackageName(app) {
		return errors.New("launch_app requires an installed package identifier, such as com.example.app")
	}
	out, err := h.runTool(ctx, "monkey", "-p", app, "-c", "android.intent.category.LAUNCHER", "1")
	if err != nil {
		return err
	}
	if !strings.Contains(string(out), "Events injected: 1") {
		return errors.New("Android did not confirm an app launch; enumerate windows before retrying")
	}
	return nil
}
