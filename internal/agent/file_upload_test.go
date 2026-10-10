package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
)

func TestFileUploadDestinationCommitAndCancel(t *testing.T) {
	for _, commit := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel", true: "commit"}[commit], func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "existing")
			if err := os.WriteFile(path, []byte("original"), 0640); err != nil {
				t.Fatal(err)
			}
			dst, err := openUploadDestination(context.Background(), protocol.StreamRequest{Upload: &protocol.FileUploadRequest{Path: path, Size: 3}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = dst.Write([]byte("new")); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(path)
			if string(before) != "original" {
				t.Fatal("original modified before commit")
			}
			if commit {
				if err := dst.Close(); err != nil {
					t.Fatal(err)
				}
				if err := dst.commit(); err != nil {
					t.Fatal(err)
				}
			}
			dst.abort()
			after, _ := os.ReadFile(path)
			want := "original"
			if commit {
				want = "new"
			}
			if string(after) != want {
				t.Fatalf("wanted %s, got %s", want, after)
			}
			entries, _ := os.ReadDir(dir)
			if len(entries) != 1 {
				t.Fatal("temporary upload file leaked")
			}
		})
	}
}
