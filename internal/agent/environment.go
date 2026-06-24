package agent

import (
	"path"
	"strings"
)

func commandEnvironment(base []string, root string) []string {
	env := append([]string(nil), base...)
	hasTERM := false
	hasHOME := false
	hasXDGConfig := false
	for _, item := range base {
		if strings.HasPrefix(item, "TERM=") {
			hasTERM = true
		}
		if strings.HasPrefix(item, "HOME=") {
			hasHOME = true
		}
		if strings.HasPrefix(item, "XDG_CONFIG_HOME=") {
			hasXDGConfig = true
		}
	}
	if !hasTERM {
		env = append(env, "TERM=xterm-256color")
	}
	if !hasHOME && strings.TrimSpace(root) != "" {
		env = append(env, "HOME="+root)
		hasHOME = true
	}
	if !hasXDGConfig && hasHOME {
		home := envValue(env, "HOME")
		if strings.TrimSpace(home) != "" {
			env = append(env, "XDG_CONFIG_HOME="+path.Join(home, ".config"))
		}
	}
	return env
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			return strings.TrimPrefix(item, prefix)
		}
	}
	return ""
}
