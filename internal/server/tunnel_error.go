package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"syscall"
	"time"

	ssh "golang.org/x/crypto/ssh"
)

type tunnelErrorDiagnostic struct {
	Code    string `json:"code"`
	Stage   string `json:"stage"`
	Machine string `json:"machine"`
	Address string `json:"address"`
}

func diagnoseTunnelListenError(ctx context.Context, err error, machine, host string, port int, client *ssh.Client) *tunnelErrorDiagnostic {
	detail := &tunnelErrorDiagnostic{Code: "listen_failed", Stage: "entry_listen", Machine: machine, Address: net.JoinHostPort(host, strconv.Itoa(port))}
	raw := strings.ToLower(err.Error())
	if errors.Is(err, syscall.EADDRINUSE) || strings.Contains(raw, "address already in use") || strings.Contains(raw, "only one usage of each socket address") {
		detail.Code = "port_in_use"
	} else if strings.Contains(raw, "tcpip-forward request denied by peer") {
		detail.Code = "ssh_forward_denied"
		if client != nil && probeTunnelListenPort(ctx, client, host, port) {
			detail.Code = "port_in_use"
		}
	}
	return detail
}

// Probe only the requested TCP port, using the same SSH account as the tunnel.
// Unsupported commands, restricted shells and timeouts retain the generic denial.
func probeTunnelListenPort(ctx context.Context, client *ssh.Client, host string, port int) bool {
	ip := net.ParseIP(host)
	if ip == nil || port < 1 || port > 65535 {
		return false
	}
	family := "6"
	if ip.To4() != nil {
		family = "4"
	}
	deadline, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	result := make(chan bool, 1)
	go func() {
		session, err := client.NewSession()
		if err != nil {
			result <- false
			return
		}
		defer session.Close()
		output, err := session.Output(fmt.Sprintf("ss -H -ltn%s 'sport = :%d' 2>/dev/null", family, port))
		result <- err == nil && tunnelListenPortOccupied(string(output), host, port)
	}()
	select {
	case occupied := <-result:
		return occupied
	case <-deadline.Done():
		return false
	}
}

func tunnelListenPortOccupied(output, host string, port int) bool {
	requested := net.ParseIP(host)
	if requested == nil {
		return false
	}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || fields[0] != "LISTEN" {
			continue
		}
		local := fields[3]
		colon := strings.LastIndex(local, ":")
		if colon < 0 || local[colon+1:] != strconv.Itoa(port) {
			continue
		}
		bound := strings.Trim(local[:colon], "[]")
		if bound == "*" {
			return true
		}
		ip := net.ParseIP(bound)
		if ip == nil || (ip.To4() != nil) != (requested.To4() != nil) {
			continue
		}
		if ip.IsUnspecified() || requested.IsUnspecified() || ip.Equal(requested) {
			return true
		}
	}
	return false
}
