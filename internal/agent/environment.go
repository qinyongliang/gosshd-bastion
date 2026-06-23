package agent

import "strings"

func commandEnvironment(base []string) []string {
	for _, item := range base {
		if strings.HasPrefix(item, "TERM=") {
			return base
		}
	}
	return append(base, "TERM=xterm-256color")
}
