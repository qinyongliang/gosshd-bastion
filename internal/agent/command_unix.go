//go:build !windows

package agent

import (
	"bufio"
	"encoding/binary"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/qinyongliang/gosshd-bastion/internal/protocol"

	"github.com/creack/pty"
)

func (c *Client) handleCommand(stream io.ReadWriteCloser, reader *bufio.Reader, req protocol.StreamRequest) {
	cmd := agentShellCommand(c.cfg.Shell, req.Type)
	if req.Type == protocol.StreamExec {
		cmd = exec.Command(c.cfg.Shell, "-lc", req.Command)
	}
	cmd.Dir = c.cfg.Root
	cmd.Env = commandEnvironment(os.Environ(), c.cfg.Root)
	if err := protocol.WriteJSONLine(stream, protocol.StreamResponse{OK: true}); err != nil {
		return
	}
	if req.Type == protocol.StreamShell && shellBaseName(c.cfg.Shell) == "bash" {
		if err := ensureAgentBashRC(); err != nil {
			_ = protocol.WriteFrame(stream, protocol.Frame{Type: protocol.FrameStderr, Data: []byte(err.Error())})
			_ = protocol.WriteFrame(stream, protocol.ExitFrame(255))
			return
		}
	}
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(req.Width), Rows: uint16(req.Height)})
	if err != nil {
		_ = protocol.WriteFrame(stream, protocol.Frame{Type: protocol.FrameStderr, Data: []byte(err.Error())})
		_ = protocol.WriteFrame(stream, protocol.ExitFrame(255))
		return
	}
	defer ptmx.Close()
	go copyFramesToWriter(ptmx, reader)
	buf := make([]byte, 32*1024)
	for {
		n, err := ptmx.Read(buf)
		if n > 0 {
			if writeErr := protocol.WriteFrame(stream, protocol.Frame{Type: protocol.FrameStdout, Data: append([]byte(nil), buf[:n]...)}); writeErr != nil {
				return
			}
		}
		if err != nil {
			break
		}
	}
	code := waitExitCode(cmd)
	_ = protocol.WriteFrame(stream, protocol.ExitFrame(code))
}

func agentShellCommand(shell string, streamType string) *exec.Cmd {
	if streamType != protocol.StreamShell {
		return exec.Command(shell)
	}
	if shellBaseName(shell) == "bash" {
		return exec.Command(shell, "--rcfile", agentBashRCPath(), "-i")
	}
	return exec.Command(shell, "-l")
}

func agentBashRCPath() string {
	return filepath.Join(os.TempDir(), "gosshd-agent-bashrc")
}

func ensureAgentBashRC() error {
	path := agentBashRCPath()
	return os.WriteFile(path, []byte(agentBashRC()), 0600)
}

func agentBashRC() string {
	return `if [ -r /etc/motd ]; then
  cat /etc/motd
fi
if [ -r /etc/profile ]; then
  . /etc/profile
fi
if [ -r "$HOME/.bash_profile" ]; then
  . "$HOME/.bash_profile"
elif [ -r "$HOME/.bash_login" ]; then
  . "$HOME/.bash_login"
elif [ -r "$HOME/.profile" ]; then
  . "$HOME/.profile"
fi
if [ -r "$HOME/.bashrc" ]; then
  . "$HOME/.bashrc"
fi
__gosshd_command_running=0
__gosshd_original_prompt_command=${PROMPT_COMMAND-}
__gosshd_preexec() {
  if [ "$__gosshd_command_running" = 0 ]; then
    __gosshd_command_running=1
    printf '\033]633;C\007'
  fi
}
__gosshd_precmd() {
  local __gosshd_rc=$?
  trap - DEBUG
  if [ "$__gosshd_command_running" = 1 ]; then
    printf '\033]633;D;%s\007' "$__gosshd_rc"
    __gosshd_command_running=0
  fi
  printf '\033]633;A\007'
  if [ -n "$__gosshd_original_prompt_command" ]; then
    eval "$__gosshd_original_prompt_command"
  fi
  trap '__gosshd_preexec' DEBUG
}
PROMPT_COMMAND='__gosshd_precmd'
trap '__gosshd_preexec' DEBUG
`
}

func shellBaseName(shell string) string {
	base := filepath.Base(strings.TrimSpace(shell))
	return strings.TrimPrefix(base, "-")
}

func copyFramesToWriter(w io.Writer, reader *bufio.Reader) {
	for {
		frame, err := protocol.ReadFrame(reader)
		if err != nil {
			return
		}
		switch frame.Type {
		case protocol.FrameStdin:
			_, _ = w.Write(frame.Data)
		case protocol.FrameResize:
			if len(frame.Data) == 8 {
				_ = pty.Setsize(w.(*os.File), &pty.Winsize{
					Cols: uint16(binary.BigEndian.Uint32(frame.Data[0:4])),
					Rows: uint16(binary.BigEndian.Uint32(frame.Data[4:8])),
				})
			}
		}
	}
}

func waitExitCode(cmd *exec.Cmd) int {
	if err := cmd.Wait(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			if status, ok := exit.Sys().(syscall.WaitStatus); ok {
				return status.ExitStatus()
			}
		}
		return 255
	}
	return 0
}
