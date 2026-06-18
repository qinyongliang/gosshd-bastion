package protocol

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	WebSocketPath = "/ws/agent"

	StreamExec          = "exec"
	StreamShell         = "shell"
	StreamSFTP          = "sftp"
	StreamTCP           = "tcp"
	StreamCancelForward = "cancel-forward"
)

const (
	FrameStdin byte = iota + 1
	FrameStdout
	FrameStderr
	FrameExit
	FrameResize
)

type AgentHello struct {
	ID              string `json:"id"`
	Token           string `json:"token,omitempty"`
	EnrollmentToken string `json:"enrollment_token,omitempty"`
	Version         string `json:"version,omitempty"`
	GOOS            string `json:"goos,omitempty"`
	GOARCH          string `json:"goarch,omitempty"`
}

type StreamRequest struct {
	Type    string `json:"type"`
	Command string `json:"command,omitempty"`
	Target  string `json:"target,omitempty"`
	Width   int    `json:"width,omitempty"`
	Height  int    `json:"height,omitempty"`
}

type StreamResponse struct {
	OK               bool   `json:"ok"`
	Error            string `json:"error,omitempty"`
	ExitCode         int    `json:"exit_code,omitempty"`
	ServerVersion    string `json:"server_version,omitempty"`
	AgentDownloadURL string `json:"agent_download_url,omitempty"`
}

type Frame struct {
	Type byte
	Data []byte
}

type ForwardSpec struct {
	BindHost string `json:"bind_host"`
	BindPort uint32 `json:"bind_port"`
	Target   string `json:"target"`
}

type ForwardResult struct {
	BindHost string `json:"bind_host"`
	BindPort uint32 `json:"bind_port"`
	Error    string `json:"error,omitempty"`
}

type AgentIDFile struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
}

func DefaultIDFile() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".gosshd", "agent.json"), nil
}

func LoadOrCreateID(path string) (string, error) {
	if path == "" {
		var err error
		path, err = DefaultIDFile()
		if err != nil {
			return "", err
		}
	}
	if data, err := os.ReadFile(path); err == nil {
		var stored AgentIDFile
		if json.Unmarshal(data, &stored) == nil && IsValidID(stored.ID) {
			return stored.ID, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	id := uuid.NewString()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(AgentIDFile{ID: id, CreatedAt: time.Now().UTC()}, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return id, nil
}

func IsValidID(id string) bool {
	_, err := uuid.Parse(id)
	return err == nil
}

func WriteJSONLine(w io.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = w.Write(data)
	return err
}

func ReadJSONLine[T any](r *bufio.Reader) (T, error) {
	var out T
	line, err := r.ReadBytes('\n')
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(line, &out); err != nil {
		return out, err
	}
	return out, nil
}

func WriteFrame(w io.Writer, frame Frame) error {
	var header [5]byte
	header[0] = frame.Type
	binary.BigEndian.PutUint32(header[1:], uint32(len(frame.Data)))
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	if len(frame.Data) == 0 {
		return nil
	}
	_, err := w.Write(frame.Data)
	return err
}

func ReadFrame(r io.Reader) (Frame, error) {
	var header [5]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return Frame{}, err
	}
	size := binary.BigEndian.Uint32(header[1:])
	if size > 32*1024*1024 {
		return Frame{}, fmt.Errorf("frame too large: %d", size)
	}
	data := make([]byte, size)
	if size > 0 {
		if _, err := io.ReadFull(r, data); err != nil {
			return Frame{}, err
		}
	}
	return Frame{Type: header[0], Data: data}, nil
}

func ExitFrame(code int) Frame {
	var data [4]byte
	binary.BigEndian.PutUint32(data[:], uint32(code))
	return Frame{Type: FrameExit, Data: data[:]}
}

func ExitCode(frame Frame) int {
	if len(frame.Data) != 4 {
		return 255
	}
	return int(binary.BigEndian.Uint32(frame.Data))
}

func NormalizeServerURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	return strings.TrimRight(raw, "/")
}

func JoinHostPort(host string, port uint32) string {
	return net.JoinHostPort(host, fmt.Sprintf("%d", port))
}
