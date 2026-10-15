//go:build linux || android || darwin

package agent

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
	"golang.org/x/sys/unix"
)

func acquireAgentInstanceGuard(key, idFile string) (func(), error) {
	directory := "/tmp"
	if runtime.GOOS == "android" {
		resolved, err := protocol.ResolveIDFilePath(idFile)
		if err != nil {
			return nil, err
		}
		directory = filepath.Dir(resolved)
	}
	path := filepath.Join(directory, "gosshd-agent-"+key+".lock")
	fd, err := unix.Open(path, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, fmt.Errorf("open Agent instance guard: %w", err)
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG {
		unix.Close(fd)
		return nil, errors.New("Agent instance guard is not a regular file")
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		unix.Close(fd)
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, ErrAgentAlreadyRunning
		}
		return nil, fmt.Errorf("lock Agent instance guard: %w", err)
	}
	var once sync.Once
	// Never unlink the pathname: replacing a locked inode would let a duplicate
	// lock a different file. This empty file contains no boot identity or secret.
	return func() { once.Do(func() { unix.Flock(fd, unix.LOCK_UN); unix.Close(fd) }) }, nil
}
