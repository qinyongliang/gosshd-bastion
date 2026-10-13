package agent

import (
	"bufio"
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/pkg/sftp"
	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
)

func TestEmbeddedDefaultRootUsesUserHomeDirectory(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewEmbedded(Config{}, "test-home")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Clean(client.cfg.Root) != filepath.Clean(home) {
		t.Fatalf("root = %q, want home %q", client.cfg.Root, home)
	}
}

func TestAgentSFTPUsesConfiguredRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "灯.scad"), []byte("cube(10);"), 0600); err != nil {
		t.Fatal(err)
	}
	agentConn, peerConn := net.Pipe()
	t.Cleanup(func() { _ = peerConn.Close(); _ = agentConn.Close() })
	c := &Client{cfg: Config{Root: root}}
	go c.handleSFTP(agentConn, bufio.NewReader(agentConn))
	reader := bufio.NewReader(peerConn)
	response, err := protocol.ReadJSONLine[protocol.StreamResponse](reader)
	if err != nil || !response.OK {
		t.Fatalf("response = %+v, err = %v", response, err)
	}
	client, err := sftp.NewClientPipe(reader, peerConn)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	actual, err := client.RealPath(".")
	if err != nil {
		t.Fatal(err)
	}
	if actual != testSFTPPath(root) {
		t.Fatalf("SFTP cwd = %q, want %q", actual, testSFTPPath(root))
	}
	file, err := client.Open("灯.scad")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	contents, err := io.ReadAll(file)
	if err != nil || string(contents) != "cube(10);" {
		t.Fatalf("contents = %q, err = %v", contents, err)
	}
}

func testSFTPPath(native string) string {
	native = filepath.ToSlash(native)
	if runtime.GOOS == "windows" && !strings.HasPrefix(native, "/") {
		return "/" + native
	}
	return native
}

func TestLocalFileDownloadAcceptsSFTPPath(t *testing.T) {
	native := filepath.Join(t.TempDir(), "灯.scad")
	if err := os.WriteFile(native, []byte("cube(10);"), 0600); err != nil {
		t.Fatal(err)
	}
	src, err := openDownloadSource(context.Background(), protocol.StreamRequest{Download: &protocol.FileDownloadRequest{Path: testSFTPPath(native)}})
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	contents, err := io.ReadAll(src)
	if err != nil || string(contents) != "cube(10);" {
		t.Fatalf("contents = %q, err = %v", contents, err)
	}
}

func TestLocalFileUploadAcceptsSFTPPath(t *testing.T) {
	native := filepath.Join(t.TempDir(), "新灯.scad")
	dst, err := openUploadDestination(context.Background(), protocol.StreamRequest{Upload: &protocol.FileUploadRequest{Path: testSFTPPath(native), Size: 9}})
	if err != nil {
		t.Fatal(err)
	}
	defer dst.abort()
	if _, err := io.WriteString(dst, "cube(10);"); err != nil {
		t.Fatal(err)
	}
	if err := dst.Close(); err != nil {
		t.Fatal(err)
	}
	if err := dst.commit(); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(native)
	if err != nil || string(contents) != "cube(10);" {
		t.Fatalf("contents = %q, err = %v", contents, err)
	}
}

func TestLocalFileRequestUsesRootWithoutMutatingDelegatedPaths(t *testing.T) {
	root := t.TempDir()
	c := &Client{cfg: Config{Root: root}}
	download := &protocol.FileDownloadRequest{Path: "灯.scad"}
	upload := &protocol.FileUploadRequest{Path: "new.scad", Size: 3}
	req := protocol.StreamRequest{Download: download, Upload: upload}
	local := c.localFileRequest(req)
	if local.Download.Path != filepath.Join(root, "灯.scad") || local.Upload.Path != filepath.Join(root, "new.scad") {
		t.Fatalf("local paths = %q, %q", local.Download.Path, local.Upload.Path)
	}
	if download.Path != "灯.scad" || upload.Path != "new.scad" {
		t.Fatal("request payload was mutated")
	}
	req.TunnelHops = []protocol.TunnelSSHHop{{Address: "remote:22"}}
	delegated := c.localFileRequest(req)
	if delegated.Download != download || delegated.Upload != upload {
		t.Fatal("delegated request was rewritten")
	}
	empty := c.localFileRequest(protocol.StreamRequest{Download: &protocol.FileDownloadRequest{}})
	if empty.Download.Path != "" {
		t.Fatal("empty path must remain invalid")
	}
}

func TestLocalFileWindowsDrivePaths(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows native path conversion")
	}
	for remote, want := range map[string]string{
		"/D:/3D/灯.scad":                  `D:\3D\灯.scad`,
		"D:/3D/灯.scad":                   `D:\3D\灯.scad`,
		"/C:/Users/user/home with spaces": `C:\Users\user\home with spaces`,
		"/D:":                            `D:\`,
		"//server/share/file":            `\\server\share\file`,
	} {
		if got := localFilePath(remote); got != want {
			t.Errorf("localFilePath(%q) = %q, want %q", remote, got, want)
		}
	}
}
