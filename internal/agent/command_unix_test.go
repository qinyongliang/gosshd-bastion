//go:build !windows

package agent

import (
	"strings"
	"testing"
)

func TestAgentBashRCDoesNotExportPromptHook(t *testing.T) {
	if script := agentBashRC(); !strings.Contains(script, "export -n PROMPT_COMMAND") {
		t.Fatalf("agent bashrc should not export the prompt hook to tmux child shells: %q", script)
	}
}
