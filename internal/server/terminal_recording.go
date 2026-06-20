package server

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/qinyongliang/gosshd-bastion/internal/store"
)

type terminalRecordingMeta struct {
	RelativePath string
	Size         int64
	SHA256       string
	DurationMS   int64
	Width        int
	Height       int
}

type terminalRecorder struct {
	mu      sync.Mutex
	root    string
	rel     string
	file    *os.File
	gzip    *gzip.Writer
	hash    hash.Hash
	started time.Time
	width   int
	height  int
	closed  bool
}

func newTerminalRecorder(root, sessionID string, width, height int, target store.SSHTarget) (*terminalRecorder, error) {
	now := time.Now().UTC()
	rel := filepath.Join(now.Format("2006"), now.Format("01"), sessionID+".cast.gz")
	abs := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(abs, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, err
	}
	h := sha256.New()
	gz := gzip.NewWriter(io.MultiWriter(file, h))
	rec := &terminalRecorder{
		root:    root,
		rel:     filepath.ToSlash(rel),
		file:    file,
		gzip:    gz,
		hash:    h,
		started: now,
		width:   width,
		height:  height,
	}
	header := map[string]any{
		"version":   2,
		"width":     width,
		"height":    height,
		"timestamp": now.Unix(),
		"target": map[string]any{
			"id":       target.ID,
			"name":     target.Name,
			"alias":    target.Alias,
			"endpoint": fmt.Sprintf("%s@%s:%d", target.RemoteUsername, target.Host, target.Port),
		},
	}
	if err := rec.writeJSONLine(header); err != nil {
		_, _ = rec.Close()
		return nil, err
	}
	return rec, nil
}

func (r *terminalRecorder) WriteOutput(data []byte) {
	if len(data) == 0 || r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	event := []any{time.Since(r.started).Seconds(), "o", string(data)}
	_ = r.writeJSONLineLocked(event)
}

func (r *terminalRecorder) Close() (terminalRecordingMeta, error) {
	if r == nil {
		return terminalRecordingMeta{}, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return r.metaLocked()
	}
	r.closed = true
	err := r.gzip.Close()
	if closeErr := r.file.Close(); err == nil {
		err = closeErr
	}
	meta, metaErr := r.metaLocked()
	if err == nil {
		err = metaErr
	}
	return meta, err
}

func (r *terminalRecorder) metaLocked() (terminalRecordingMeta, error) {
	abs := filepath.Join(r.root, filepath.FromSlash(r.rel))
	stat, err := os.Stat(abs)
	if err != nil {
		return terminalRecordingMeta{}, err
	}
	return terminalRecordingMeta{
		RelativePath: r.rel,
		Size:         stat.Size(),
		SHA256:       hex.EncodeToString(r.hash.Sum(nil)),
		DurationMS:   time.Since(r.started).Milliseconds(),
		Width:        r.width,
		Height:       r.height,
	}, nil
}

func (r *terminalRecorder) writeJSONLine(v any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.writeJSONLineLocked(v)
}

func (r *terminalRecorder) writeJSONLineLocked(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, err := r.gzip.Write(data); err != nil {
		return err
	}
	_, err = r.gzip.Write([]byte("\n"))
	return err
}

func loadTerminalRecording(path string) ([]json.RawMessage, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	decoder := json.NewDecoder(gz)
	var lines []json.RawMessage
	for {
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		lines = append(lines, raw)
	}
	return lines, nil
}
