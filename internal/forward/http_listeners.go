package forward

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// HTTPListeners validates the canonical listener list. Stored legacy rules are migrated.
func HTTPListeners(config *HTTPConfig) ([]HTTPListener, error) {
	if config == nil {
		return nil, errors.New("HTTP configuration required")
	}
	entries := config.Listeners
	if len(entries) == 0 || len(entries) > 64 {
		return nil, errors.New("HTTP requires 1–64 listeners")
	}
	normalized := make([]HTTPListener, len(entries))
	seen := map[string]bool{}
	for i, entry := range entries {
		entry.Host = strings.TrimSpace(entry.Host)
		if entry.Host == "" {
			entry.Host = "0.0.0.0"
		}
		ip := net.ParseIP(entry.Host)
		if ip == nil {
			return nil, errors.New("listen host must be an IP address")
		}
		if entry.Port < 1 || entry.Port > 65535 {
			return nil, errors.New("listen ports must be between 1 and 65535")
		}
		entry.Host = ip.String()
		address := net.JoinHostPort(entry.Host, strconv.Itoa(entry.Port))
		if seen[address] {
			return nil, errors.New("HTTP listeners must be unique")
		}
		for _, previous := range normalized[:i] {
			if previous.Port != entry.Port {
				continue
			}
			// Wildcard TCP listeners may be dual-stack; reserve the entire port.
			if net.ParseIP(previous.Host).IsUnspecified() || ip.IsUnspecified() {
				return nil, errors.New("HTTP listener IP/port ranges overlap")
			}
		}
		seen[address] = true
		normalized[i] = entry
	}
	return normalized, nil
}

// StartHTTP binds every endpoint before serving. A failed bind releases earlier
// endpoints. All endpoints share routing, upstream pools, logs and counters.
func StartHTTP(parent context.Context, config *HTTPConfig, listen func(string) (net.Listener, error), dial DialFunc, logs EventRecorder, direct func(string) bool) (*Runtime, error) {
	entries, err := HTTPListeners(config)
	if err != nil {
		return nil, err
	}
	listeners := make([]net.Listener, 0, len(entries))
	closeAll := func() {
		for _, l := range listeners {
			_ = l.Close()
		}
	}
	for _, entry := range entries {
		address := net.JoinHostPort(entry.Host, strconv.Itoa(entry.Port))
		l, err := listen(address)
		if err != nil {
			closeAll()
			return nil, fmt.Errorf("listen %s: %w", address, err)
		}
		listeners = append(listeners, l)
	}
	runtime, err := startListeners(parent, listeners, config, nil, dial, logs, direct, newRuntimeCounters())
	if err != nil {
		closeAll()
	}
	return runtime, err
}
