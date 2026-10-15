//go:build !windows && !linux && !android && !darwin

package agent

import "errors"

func readSystemBootID() (string, error) {
	return "", errors.New("OS boot identity is not supported on this platform")
}

func acquireAgentInstanceGuard(key, idFile string) (func(), error) { return func() {}, nil }
