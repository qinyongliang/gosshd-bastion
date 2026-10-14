package main

import (
	"flag"
	"io"
	"runtime"
	"testing"

	"github.com/qinyongliang/gosshd-bastion/internal/agent"
)

func TestDesktopAgentFlags(t *testing.T) {
	for _, tt := range []struct {
		name              string
		args              []string
		enabled, disabled bool
	}{
		{name: "default", enabled: agent.DesktopEnabledByDefault(runtime.GOOS)},
		{name: "opt out", args: []string{"--desktop-disable"}, enabled: agent.DesktopEnabledByDefault(runtime.GOOS), disabled: true},
		{name: "legacy opt out", args: []string{"--desktop-enable=false"}, disabled: true},
		{name: "explicit on", args: []string{"--desktop-enable"}, enabled: true},
		{name: "disable wins", args: []string{"--desktop-disable", "--desktop-enable"}, enabled: true, disabled: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			flags := flag.NewFlagSet("agent-test", flag.ContinueOnError)
			flags.SetOutput(io.Discard)
			cfg, err := parseAgentFlags(flags, tt.args)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Desktop.Enabled != tt.enabled || cfg.DesktopDisabled != tt.disabled {
				t.Fatalf("unexpected flags: %+v", cfg)
			}
		})
	}
}
