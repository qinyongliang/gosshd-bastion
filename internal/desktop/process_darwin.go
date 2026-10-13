//go:build darwin

package desktop

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func helperPath(cfg Config) (string, error) {
	path := cfg.HelperPath
	if path == "" {
		exe, err := os.Executable()
		if err != nil {
			return "", err
		}
		path = filepath.Join(filepath.Dir(exe), "GOSSHD Desktop Helper.app", "Contents", "MacOS", "gosshd-desktop-helper")
	}
	if !filepath.IsAbs(path) {
		return "", errors.New("desktop helper path must be absolute")
	}
	return path, nil
}
func platformCommand(ctx context.Context, cfg Config) (*exec.Cmd, func(), error) {
	path, err := helperPath(cfg)
	if err != nil {
		return nil, nil, err
	}
	return exec.CommandContext(ctx, path), func() {}, nil
}

// Launch a bundle through LaunchServices so TCC attributes access to the app the
// user approved, rather than the SSH/Agent parent. Private FIFOs preserve the
// same persistent JSON-lines protocol without inherited-fd assumptions.
func startNativeProcess(ctx context.Context, cfg Config) (*process, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := helperPath(cfg)
	if err != nil {
		return nil, err
	}
	bundle := ""
	for dir := filepath.Dir(path); dir != "/" && dir != "."; dir = filepath.Dir(dir) {
		if strings.HasSuffix(dir, ".app") {
			bundle = dir
			break
		}
	}
	if bundle == "" {
		return startProcess(ctx, cfg, platformCommand)
	}
	dir, err := os.MkdirTemp("", "gosshd-desktop-ipc-")
	if err != nil {
		return nil, err
	}
	// lsof reports canonical names; match only this session's private FIFO.
	canonical, err := filepath.EvalSymlinks(dir)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	dir = canonical
	cleanup := func() { _ = os.RemoveAll(dir) }
	inputPath, outputPath := filepath.Join(dir, "input"), filepath.Join(dir, "output")
	for _, fifo := range []string{inputPath, outputPath} {
		if err = syscall.Mkfifo(fifo, 0600); err != nil {
			cleanup()
			return nil, err
		}
	}
	input, err := os.OpenFile(inputPath, os.O_RDWR|syscall.O_NONBLOCK, 0600)
	if err != nil {
		cleanup()
		return nil, err
	}
	output, err := os.OpenFile(outputPath, os.O_RDONLY|syscall.O_NONBLOCK, 0600)
	if err != nil {
		input.Close()
		cleanup()
		return nil, err
	}
	command := exec.Command("/usr/bin/open", "-n", "-g", "-W", "-a", bundle, "--stdin", inputPath, "--stdout", outputPath, "--stderr", "/dev/null")
	if err = command.Start(); err != nil {
		input.Close()
		output.Close()
		cleanup()
		return nil, err
	}
	ipcContext, cancelIPC := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- command.Wait(); cancelIPC(); input.Close(); output.Close() }()
	scanner := bufio.NewScanner(&nativeFIFO{file: output, ctx: ipcContext})
	scanner.Buffer(make([]byte, 65536), MaxMessage)
	p := &process{cmd: command, in: &nativeFIFO{file: input, ctx: ipcContext}, out: scanner}
	finished := make(chan struct{})
	p.closeNative = func() {
		defer close(finished)
		cancelIPC()
		// Closing the input writer causes a normally idle helper to exit. If a native
		// provider is stuck, find only the process holding this session's private FIFO.
		probeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		ids := nativeFIFOReaders(probeCtx, inputPath)
		cancel()
		input.Close()
		output.Close()
		for _, pid := range ids {
			if pid <= 1 || pid == os.Getpid() || pid == command.Process.Pid {
				continue
			}
			if child, e := os.FindProcess(pid); e == nil {
				_ = child.Kill()
			}
		}
		if command.Process != nil {
			_ = command.Process.Kill()
		}
		select {
		case <-done:
		case <-time.After(3 * time.Second):
		}
		cleanup()
	}
	go func() {
		select {
		case <-ctx.Done():
			p.close()
		case <-finished:
		}
	}()
	return p, nil
}

// macOS FIFO entries have no device identity, so lsof's filename selector can
// miss them. Read only same-user stdin fields, retain exact matches, and never
// expose the other processes' paths or diagnostics to callers or audit logs.
func nativeFIFOReaders(ctx context.Context, path string) []int {
	cmd := exec.CommandContext(ctx, "/usr/sbin/lsof", "-n", "-Fpn", "-a", "-u", strconv.Itoa(os.Getuid()), "-d", "0")
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil
	}
	if err = cmd.Start(); err != nil {
		out.Close()
		return nil
	}
	var pids []int
	pid := 0
	scanner := bufio.NewScanner(out)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "p") {
			pid, _ = strconv.Atoi(line[1:])
		}
		if line == "n"+path && pid > 1 {
			pids = append(pids, pid)
		}
	}
	_ = cmd.Wait()
	return pids
}

// Go deliberately does not poll Darwin FIFOs through kqueue. Blocking file
// reads cannot be reliably interrupted by Close, so use nonblocking I/O with
// bounded, context-aware retries. The output FIFO never holds our own writer.
type nativeFIFO struct {
	file *os.File
	ctx  context.Context
}

func (f *nativeFIFO) Name() string { return f.file.Name() }
func (f *nativeFIFO) Close() error { return f.file.Close() }
func (f *nativeFIFO) wait() error {
	timer := time.NewTimer(5 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-f.ctx.Done():
		return f.ctx.Err()
	case <-timer.C:
		return nil
	}
}
func (f *nativeFIFO) Read(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}
	for {
		if err := f.ctx.Err(); err != nil {
			return 0, err
		}
		n, err := f.file.Read(data)
		if n > 0 {
			return n, nil
		}
		// EOF before LaunchServices opens stdout is not a completed response.
		if err != nil && !errors.Is(err, syscall.EAGAIN) && !errors.Is(err, io.EOF) {
			return 0, err
		}
		if err = f.wait(); err != nil {
			return 0, err
		}
	}
}
func (f *nativeFIFO) Write(data []byte) (int, error) {
	total := 0
	for total < len(data) {
		if err := f.ctx.Err(); err != nil {
			return total, err
		}
		n, err := f.file.Write(data[total:])
		total += n
		if err != nil && !errors.Is(err, syscall.EAGAIN) {
			return total, err
		}
		if total < len(data) {
			if err = f.wait(); err != nil {
				return total, err
			}
		}
	}
	return total, nil
}
