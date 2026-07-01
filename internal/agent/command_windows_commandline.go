//go:build windows

package agent

import "strings"

func windowsCommandLine(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return "cmd.exe"
	}
	args := windowsInteractiveShellArgs(command)
	if len(args) == 0 {
		return quoteWindowsArg(command)
	}
	parts := []string{quoteWindowsArg(command)}
	for _, arg := range args {
		parts = append(parts, quoteWindowsArg(arg))
	}
	return strings.Join(parts, " ")
}

func quoteWindowsArg(arg string) string {
	if arg == "" {
		return `""`
	}
	if !strings.ContainsAny(arg, " \t\"") {
		return arg
	}
	return `"` + strings.ReplaceAll(arg, `"`, `\"`) + `"`
}
