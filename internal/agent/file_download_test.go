package agent

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
)

func TestFileDownloadSourceValidation(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "source")
	if err := os.WriteFile(name, []byte("contents"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"", dir, filepath.Join(dir, "missing"), name} {
		src, err := openDownloadSource(context.Background(), protocol.StreamRequest{Download: &protocol.FileDownloadRequest{Path: source}})
		if source != name {
			if err == nil {
				src.Close()
				t.Fatalf("invalid source accepted: %s", source)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(src)
		src.Close()
		if err != nil || src.size != 8 || string(data) != "contents" {
			t.Fatalf("invalid source: %s %v", data, err)
		}
	}
}
