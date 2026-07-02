package server

import "testing"

func TestParseTargetSystemProbeFiltersHTMLPublicIP(t *testing.T) {
	snapshot, ok := parseTargetSystemProbe("__GOSSHD_SYSTEM_V1__\nos=windows\npublic_ip=<!DOCTYPE html>\nhostname=WIN\n")
	if !ok {
		t.Fatal("probe should parse")
	}
	if snapshot.PublicIP != "" {
		t.Fatalf("html public ip should be ignored, got %q", snapshot.PublicIP)
	}
}

func TestParseTargetSystemProbeAcceptsPublicIPLiteral(t *testing.T) {
	snapshot, ok := parseTargetSystemProbe("__GOSSHD_SYSTEM_V1__\nos=linux\npublic_ip=203.0.113.10\n")
	if !ok {
		t.Fatal("probe should parse")
	}
	if snapshot.PublicIP != "203.0.113.10" {
		t.Fatalf("public ip mismatch: %q", snapshot.PublicIP)
	}
}
