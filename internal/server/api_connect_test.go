package server

import "testing"

func TestParseTargetSystemProbeFiltersHTMLPublicIP(t *testing.T) {
	snapshot, ok := parseTargetSystemProbe("__GOSSHD_SYSTEM_V1__\nos=windows\npublic_ip=<!DOCTYPE html>\npublic_ipv4=<!DOCTYPE html>\npublic_ipv6=<!DOCTYPE html>\nhostname=WIN\n")
	if !ok {
		t.Fatal("probe should parse")
	}
	if snapshot.PublicIP != "" || snapshot.PublicIPv4 != "" || snapshot.PublicIPv6 != "" {
		t.Fatalf("html public ip should be ignored, got public=%q ipv4=%q ipv6=%q", snapshot.PublicIP, snapshot.PublicIPv4, snapshot.PublicIPv6)
	}
}

func TestParseTargetSystemProbeAcceptsPublicIPLiteral(t *testing.T) {
	snapshot, ok := parseTargetSystemProbe("__GOSSHD_SYSTEM_V1__\nos=linux\npublic_ipv4=203.0.113.10\npublic_ipv6=240e:39f:3d2:81c0::9b\n")
	if !ok {
		t.Fatal("probe should parse")
	}
	if snapshot.PublicIP != "203.0.113.10" || snapshot.PublicIPv4 != "203.0.113.10" || snapshot.PublicIPv6 != "240e:39f:3d2:81c0::9b" {
		t.Fatalf("public ip mismatch: public=%q ipv4=%q ipv6=%q", snapshot.PublicIP, snapshot.PublicIPv4, snapshot.PublicIPv6)
	}
}

func TestParseTargetSystemProbeBackfillsPublicIPByFamily(t *testing.T) {
	snapshot, ok := parseTargetSystemProbe("__GOSSHD_SYSTEM_V1__\nos=linux\npublic_ip=240e:39f:3d2:81c0:81c4:9b\n")
	if !ok {
		t.Fatal("probe should parse")
	}
	if snapshot.PublicIPv6 != "240e:39f:3d2:81c0:81c4:9b" || snapshot.PublicIPv4 != "" {
		t.Fatalf("legacy public ip should backfill ipv6 only: ipv4=%q ipv6=%q", snapshot.PublicIPv4, snapshot.PublicIPv6)
	}
}
