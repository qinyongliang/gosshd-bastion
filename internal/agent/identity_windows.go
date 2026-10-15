//go:build windows

package agent

import (
	"errors"
	"fmt"
	"sync"

	"golang.org/x/sys/windows"
)

func acquireAgentInstanceGuard(key, idFile string) (func(), error) {
	name, err := windows.UTF16PtrFromString(`Global\gosshd-agent-` + key)
	if err != nil {
		return nil, err
	}
	// Use object existence, not thread ownership: Go goroutines can move between
	// OS threads. The kernel releases our handle even after a process crash.
	handle, err := windows.CreateMutexEx(nil, name, 0, windows.SYNCHRONIZE)
	if err != nil {
		if handle != 0 {
			windows.CloseHandle(handle)
		}
		if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
			return nil, ErrAgentAlreadyRunning
		}
		return nil, fmt.Errorf("create global Agent instance guard: %w", err)
	}
	var once sync.Once
	return func() { once.Do(func() { windows.CloseHandle(handle) }) }, nil
}
