package server

import (
	"strings"
	"testing"
)

func TestAndroidAPKEnrollmentHasPublicDownloadAndPrivateSetup(t *testing.T) {
	app := NewApp(Config{Version: "v1.2.3", ReleaseBaseURL: "https://releases.example.com/download"})
	response := app.agentEnrollmentResponse("example-id", "example-enrollment-token", "https://bastion.example.com")
	if response.ServerURL != "https://bastion.example.com" || response.Token != "example-enrollment-token" {
		t.Fatal("manual setup fields missing")
	}
	if response.AndroidAPKURL != "https://releases.example.com/download/v1.2.3/gosshd-agent-android.apk" {
		t.Fatalf("unexpected package URL: %s", response.AndroidAPKURL)
	}
	if strings.Contains(response.AndroidAPKURL, response.Token) {
		t.Fatal("enrollment credential leaked into public APK URL")
	}
}
