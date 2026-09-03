package server

import (
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/pkg/sftp"
)

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

func TestSFTPMovePathMovesDirectoryContents(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "source", "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "source", "nested", "file.txt"), []byte("moved"), 0o644); err != nil {
		t.Fatal(err)
	}

	clientConn, serverConn := net.Pipe()
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		server, err := sftp.NewServer(serverConn, sftp.WithServerWorkingDirectory(root))
		if err != nil {
			return
		}
		_ = server.Serve()
		_ = server.Close()
	}()

	client, err := sftp.NewClientPipe(clientConn, clientConn)
	if err != nil {
		_ = clientConn.Close()
		<-serverDone
		t.Fatal(err)
	}
	defer func() {
		_ = client.Close()
		_ = clientConn.Close()
		<-serverDone
	}()

	if err := sftpMovePath(client, "source", "destination"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Stat("source"); !os.IsNotExist(err) {
		t.Fatalf("source directory still exists: %v", err)
	}
	file, err := client.Open("destination/nested/file.txt")
	if err != nil {
		t.Fatal(err)
	}
	content, err := io.ReadAll(file)
	_ = file.Close()
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "moved" {
		t.Fatalf("moved file content = %q", content)
	}
}
