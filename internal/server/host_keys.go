package server

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"

	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func (a *App) targetHostKeyCallback() gossh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key gossh.PublicKey) error {
		path := a.knownHostsPath()
		a.knownHostsMu.Lock()
		defer a.knownHostsMu.Unlock()

		callback, err := knownhosts.New(path)
		if err == nil {
			if err := callback(hostname, remote, key); err == nil {
				return nil
			} else {
				var keyErr *knownhosts.KeyError
				if !errors.As(err, &keyErr) || len(keyErr.Want) > 0 {
					return err
				}
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}

		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return err
		}
		defer f.Close()
		if _, err := fmt.Fprintln(f, knownhosts.Line([]string{hostname}, key)); err != nil {
			return err
		}
		return nil
	}
}
