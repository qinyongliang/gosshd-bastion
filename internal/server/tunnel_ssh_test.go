package server

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/qinyongliang/gosshd-bastion/internal/store"
	ssh "golang.org/x/crypto/ssh"
)

type tunnelSSHAddress struct {
	Host string
	Port uint32
}
type tunnelSSHForward struct {
	Host       string
	Port       uint32
	OriginHost string
	OriginPort uint32
}

func tunnelTestSSH(t *testing.T) string {
	t.Helper()
	cfg := &ssh.ServerConfig{PasswordCallback: func(_ ssh.ConnMetadata, p []byte) (*ssh.Permissions, error) {
		if string(p) != "tunnel-password" {
			return nil, fmt.Errorf("wrong password")
		}
		return nil, nil
	}}
	cfg.AddHostKey(testSSHSigner(t))
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var connections []net.Conn
	t.Cleanup(func() {
		l.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, c := range connections {
			c.Close()
		}
	})
	go func() {
		for {
			raw, e := l.Accept()
			if e != nil {
				return
			}
			mu.Lock()
			connections = append(connections, raw)
			mu.Unlock()
			go func() {
				conn, chans, requests, e := ssh.NewServerConn(raw, cfg)
				if e != nil {
					raw.Close()
					return
				}
				defer conn.Close()
				var listenersMu sync.Mutex
				listeners := map[string]net.Listener{}
				go func() {
					defer func() {
						listenersMu.Lock()
						defer listenersMu.Unlock()
						for _, listener := range listeners {
							listener.Close()
						}
					}()
					for req := range requests {
						var addr tunnelSSHAddress
						_ = ssh.Unmarshal(req.Payload, &addr)
						key := net.JoinHostPort(addr.Host, strconv.Itoa(int(addr.Port)))
						switch req.Type {
						case "tcpip-forward":
							listener, e := net.Listen("tcp", key)
							if e != nil {
								req.Reply(false, nil)
								continue
							}
							listenersMu.Lock()
							listeners[key] = listener
							listenersMu.Unlock()
							req.Reply(true, ssh.Marshal(struct{ Port uint32 }{uint32(listener.Addr().(*net.TCPAddr).Port)}))
							go func() {
								for {
									source, e := listener.Accept()
									if e != nil {
										return
									}
									go func() {
										defer source.Close()
										remote := source.RemoteAddr().(*net.TCPAddr)
										payload := ssh.Marshal(tunnelSSHForward{addr.Host, addr.Port, remote.IP.String(), uint32(remote.Port)})
										ch, r, e := conn.OpenChannel("forwarded-tcpip", payload)
										if e != nil {
											return
										}
										defer ch.Close()
										go ssh.DiscardRequests(r)
										tunnelTestBridge(source, ch)
									}()
								}
							}()
						case "cancel-tcpip-forward":
							listenersMu.Lock()
							listener := listeners[key]
							delete(listeners, key)
							listenersMu.Unlock()
							if listener != nil {
								listener.Close()
							}
							req.Reply(true, nil)
						default:
							req.Reply(false, nil)
						}
					}
				}()
				for next := range chans {
					if next.ChannelType() != "direct-tcpip" {
						next.Reject(ssh.UnknownChannelType, "unsupported")
						continue
					}
					var addr tunnelSSHForward
					if ssh.Unmarshal(next.ExtraData(), &addr) != nil {
						next.Reject(ssh.ConnectionFailed, "bad address")
						continue
					}
					dest, e := net.DialTimeout("tcp", net.JoinHostPort(addr.Host, strconv.Itoa(int(addr.Port))), time.Second)
					if e != nil {
						next.Reject(ssh.ConnectionFailed, e.Error())
						continue
					}
					ch, r, e := next.Accept()
					if e != nil {
						dest.Close()
						continue
					}
					go ssh.DiscardRequests(r)
					go func() { defer dest.Close(); defer ch.Close(); tunnelTestBridge(dest, ch) }()
				}
			}()
		}
	}()
	return l.Addr().String()
}
func tunnelTestBridge(tcp net.Conn, ch ssh.Channel) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); io.Copy(ch, tcp); ch.CloseWrite() }()
	go func() {
		defer wg.Done()
		io.Copy(tcp, ch)
		if c, ok := tcp.(interface{ CloseWrite() error }); ok {
			c.CloseWrite()
		} else {
			tcp.Close()
		}
	}()
	wg.Wait()
}
func tunnelSSHTarget(t *testing.T, app *App, u store.User, o store.Organization, alias, address, proxy string) store.SSHTarget {
	t.Helper()
	host, p, _ := net.SplitHostPort(address)
	port, _ := strconv.Atoi(p)
	v, err := app.store.Repository().CreateSSHTarget(context.Background(), store.CreateSSHTargetParams{OwnerType: store.OwnerOrganization, OwnerID: o.ID, Name: alias, Alias: alias, TargetType: store.TargetDirect, Host: host, Port: port, RemoteUsername: "test", AuthType: store.AuthPassword, EncryptedSecret: []byte("tunnel-password"), ProxyTargetID: proxy, CreatedBy: u.ID})
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func TestTunnelSSHEntryAndJumpHosts(t *testing.T) {
	for _, jump := range []bool{false, true} {
		t.Run(fmt.Sprint("jump=", jump), func(t *testing.T) {
			app, u, o := tunnelFixture(t)
			echo := tunnelEcho(t)
			proxy := ""
			if jump {
				proxy = tunnelSSHTarget(t, app, u, o, "jump", tunnelTestSSH(t), "").ID
			}
			entry := tunnelSSHTarget(t, app, u, o, "entry", tunnelTestSSH(t), proxy)
			v := createTestTunnel(t, app, u, o, entry.ID, "", echo.Addr().(*net.TCPAddr).Port)
			c, err := net.Dial("tcp", net.JoinHostPort(v.ListenHost, strconv.Itoa(v.ListenPort)))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			roundtripTunnel(t, c)
		})
	}
}
func TestTunnelSSHBehindAgentsUsesP2P(t *testing.T) {
	app, u, o := tunnelFixture(t)
	echo := tunnelEcho(t)
	attachTunnelAgent(t, app, "entry-anchor")
	attachTunnelAgent(t, app, "exit-anchor")
	entryAgent := tunnelAgentTarget(t, app, u, o, "entry-anchor")
	exitAgent := tunnelAgentTarget(t, app, u, o, "exit-anchor")
	entry := tunnelSSHTarget(t, app, u, o, "entry-ssh", tunnelTestSSH(t), entryAgent.ID)
	exit := tunnelSSHTarget(t, app, u, o, "exit-ssh", tunnelTestSSH(t), exitAgent.ID)
	v := createTestTunnel(t, app, u, o, entry.ID, exit.ID, echo.Addr().(*net.TCPAddr).Port)
	c, err := net.Dial("tcp", net.JoinHostPort(v.ListenHost, strconv.Itoa(v.ListenPort)))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	roundtripTunnel(t, c)
	until := time.Now().Add(15 * time.Second)
	for time.Now().Before(until) {
		if app.tunnelAPIStatus(v).Transport == "direct" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if app.tunnelAPIStatus(v).Transport != "direct" {
		t.Fatal("SSH-through-Agent pair did not establish P2P")
	}
	roundtripTunnel(t, c)
}
