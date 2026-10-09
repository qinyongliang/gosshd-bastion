package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"syscall"
	"testing"
	"time"

	ssh "golang.org/x/crypto/ssh"
)

func TestTunnelErrorDiagnosis(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		host    string
		code    string
		address string
	}{
		{"wrapped bind conflict", fmt.Errorf("listen: %w", syscall.EADDRINUSE), "0.0.0.0", "port_in_use", "0.0.0.0:8081"},
		{"agent bind conflict", errors.New("listen tcp: bind: address already in use"), "127.0.0.1", "port_in_use", "127.0.0.1:8081"},
		{"windows bind conflict", errors.New("Only one usage of each socket address is normally permitted"), "127.0.0.1", "port_in_use", "127.0.0.1:8081"},
		{"unconfirmed SSH rejection", errors.New("ssh: tcpip-forward request denied by peer"), "0.0.0.0", "ssh_forward_denied", "0.0.0.0:8081"},
		{"IPv6 generic failure", errors.New("permission denied"), "::1", "listen_failed", "[::1]:8081"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := diagnoseTunnelListenError(context.Background(), tc.err, "prod-monitor", tc.host, 8081, nil)
			if got.Code != tc.code || got.Address != tc.address || got.Machine != "prod-monitor" || got.Stage != "entry_listen" {
				t.Fatalf("unexpected diagnostic: %+v", got)
			}
		})
	}
}

func TestTunnelErrorPortOccupancy(t *testing.T) {
	cases := []struct {
		name  string
		local string
		host  string
		want  bool
	}{
		{"IPv4 wildcard", "0.0.0.0:8081", "127.0.0.1", true},
		{"requested wildcard", "10.0.0.10:8081", "0.0.0.0", true},
		{"matching interface", "10.0.0.10:8081", "10.0.0.10", true},
		{"other interface", "10.0.0.11:8081", "10.0.0.10", false},
		{"other port", "0.0.0.0:18081", "0.0.0.0", false},
		{"IPv6 wildcard", "[::]:8081", "::1", true},
		{"IPv6 exact", "[::1]:8081", "::1", true},
		{"ss wildcard", "*:8081", "::", true},
		{"other family", "[::1]:8081", "127.0.0.1", false},
		{"invalid local", "invalid:8081", "0.0.0.0", false},
		{"invalid host", "0.0.0.0:8081", "localhost", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			output := "LISTEN 0 128 " + tc.local + " *:*\n"
			if got := tunnelListenPortOccupied(output, tc.host, 8081); got != tc.want {
				t.Fatalf("occupied = %v, want %v", got, tc.want)
			}
		})
	}
	if tunnelListenPortOccupied("ESTAB 0 0 0.0.0.0:8081 *:*\nLISTEN truncated", "0.0.0.0", 8081) {
		t.Fatal("malformed/non-listening rows counted as occupied")
	}
	if !tunnelListenPortOccupied("LISTEN 0 128 0.0.0.0:80 *:*\nLISTEN 0 128 0.0.0.0:8081 *:*", "0.0.0.0", 8081) {
		t.Fatal("matching listener after unrelated row was missed")
	}
}

func TestTunnelErrorSSHProbe(t *testing.T) {
	for _, mode := range []string{"occupied", "empty", "restricted", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			config := &ssh.ServerConfig{NoClientAuth: true}
			config.AddHostKey(testSSHSigner(t))
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			commands := make(chan string, 1)
			go func() {
				raw, err := listener.Accept()
				if err != nil {
					return
				}
				defer raw.Close()
				conn, channels, requests, err := ssh.NewServerConn(raw, config)
				if err != nil {
					return
				}
				defer conn.Close()
				go ssh.DiscardRequests(requests)
				for incoming := range channels {
					if mode == "restricted" || incoming.ChannelType() != "session" {
						incoming.Reject(ssh.Prohibited, "restricted")
						continue
					}
					channel, requests, err := incoming.Accept()
					if err != nil {
						return
					}
					for req := range requests {
						if req.Type != "exec" {
							req.Reply(false, nil)
							continue
						}
						var payload struct{ Command string }
						if ssh.Unmarshal(req.Payload, &payload) != nil {
							req.Reply(false, nil)
							continue
						}
						commands <- payload.Command
						req.Reply(true, nil)
						if mode == "timeout" {
							io.Copy(io.Discard, channel)
							break
						}
						if mode == "occupied" {
							io.WriteString(channel, "LISTEN 0 128 0.0.0.0:8081 *:*\n")
						}
						channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
						channel.Close()
						break
					}
				}
			}()
			client, err := ssh.Dial("tcp", listener.Addr().String(), &ssh.ClientConfig{User: "test", HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: time.Second})
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			timeout := 3 * time.Second
			if mode == "timeout" {
				timeout = 100 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			got := diagnoseTunnelListenError(ctx, errors.New("ssh: tcpip-forward request denied by peer"), "prod-monitor", "0.0.0.0", 8081, client)
			want := "ssh_forward_denied"
			if mode == "occupied" {
				want = "port_in_use"
			}
			if got.Code != want {
				t.Fatalf("code = %s, want %s", got.Code, want)
			}
			if mode != "restricted" {
				select {
				case command := <-commands:
					if command != "ss -H -ltn4 'sport = :8081' 2>/dev/null" {
						t.Fatalf("unexpected probe command: %s", command)
					}
				case <-time.After(time.Second):
					t.Fatal("port check was not executed")
				}
			}
		})
	}
}

func TestTunnelErrorClearStaleDiagnostic(t *testing.T) {
	run := &tunnelRun{state: tunnelStatus{ErrorDiagnostic: &tunnelErrorDiagnostic{Code: "port_in_use"}}}
	run.setError(errors.New("destination unavailable"))
	state := run.snapshot()
	if state.ErrorDiagnostic != nil || !strings.Contains(state.Error, "destination") {
		t.Fatalf("stale entry diagnostic: %+v", state)
	}
}
