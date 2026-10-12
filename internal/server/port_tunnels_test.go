package server

import (
	"bytes"
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/qinyongliang/gosshd-bastion/internal/store"
)

func tunnelUDPEcho(t *testing.T, address string) net.PacketConn {
	t.Helper()
	c, err := net.ListenPacket("udp", address)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	go func() {
		buf := make([]byte, 65535)
		for {
			n, a, e := c.ReadFrom(buf)
			if e != nil {
				return
			}
			c.WriteTo(buf[:n], a)
		}
	}()
	return c
}
func roundtripUDP(t *testing.T, c net.Conn, payload []byte) {
	t.Helper()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.Write(payload); err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 65535)
	n, err := c.Read(b)
	if err != nil || !bytes.Equal(b[:n], payload) {
		t.Fatalf("UDP got %d/%d bytes: %v", n, len(payload), err)
	}
}
func TestPortTunnelTCPAndUDPAllEndpoints(t *testing.T) {
	for _, mode := range []string{"bastion", "same-agent", "agents", "agent-bastion", "bastion-agent"} {
		t.Run(mode, func(t *testing.T) {
			app, u, o := tunnelFixture(t)
			tcpEcho := tunnelEcho(t)
			udpEcho := tunnelUDPEcho(t, tcpEcho.Addr().String())
			entry, exit := "", ""
			if mode != "bastion" {
				attachTunnelAgent(t, app, "port-entry")
				attachTunnelAgent(t, app, "port-exit")
				a := tunnelAgentTarget(t, app, u, o, "port-entry")
				b := tunnelAgentTarget(t, app, u, o, "port-exit")
				switch mode {
				case "same-agent":
					entry, exit = a.ID, a.ID
				case "agents":
					entry, exit = a.ID, b.ID
				case "agent-bastion":
					entry = a.ID
				case "bastion-agent":
					exit = b.ID
				}
			}
			view, err := app.saveManagedTunnel(context.Background(), u, store.Tunnel{OrganizationID: o.ID, CreatedBy: u.ID, TunnelConfig: store.TunnelConfig{Protocol: "port", Transports: []string{"tcp", "udp"}, Name: "dual port", EntryTargetID: entry, ExitTargetID: exit, ListenHost: "127.0.0.1", ListenPort: freeTunnelPort(t), DestinationHost: "127.0.0.1", DestinationPort: udpEcho.LocalAddr().(*net.UDPAddr).Port}})
			if err != nil {
				t.Fatal(err)
			}
			enabled, err := app.setManagedTunnelEnabled(context.Background(), u, view.Tunnel, true)
			if err != nil {
				t.Fatal(err)
			}
			v := enabled.Tunnel
			waitForward(t, app, v, func(s tunnelStatus) bool { return s.Status == "running" && s.Transport != "negotiating" })
			address := net.JoinHostPort(v.ListenHost, strconv.Itoa(v.ListenPort))
			tcp, err := net.Dial("tcp", address)
			if err != nil {
				t.Fatal(err)
			}
			defer tcp.Close()
			roundtripTunnel(t, tcp)
			clients := make([]net.Conn, 2)
			for i := range clients {
				clients[i], err = net.Dial("udp", address)
				if err != nil {
					t.Fatal(err)
				}
				defer clients[i].Close()
			}
			for _, payload := range [][]byte{[]byte("port UDP"), {}, bytes.Repeat([]byte("packet"), 160)} {
				for _, c := range clients {
					roundtripUDP(t, c, payload)
				}
			}
			t.Run("large-datagram", func(t *testing.T) {
				payload := bytes.Repeat([]byte("packet"), 10000)
				probe, e := net.Dial("udp", udpEcho.LocalAddr().String())
				if e != nil {
					t.Fatal(e)
				}
				defer probe.Close()
				probe.SetDeadline(time.Now().Add(300 * time.Millisecond))
				probe.Write(payload)
				buf := make([]byte, 65535)
				if n, e := probe.Read(buf); e != nil || n != len(payload) {
					t.Skip("native loopback cannot deliver large UDP packets in this environment")
				}
				for _, c := range clients {
					roundtripUDP(t, c, payload)
				}
			})
			if mode == "agents" {
				waitForward(t, app, v, func(s tunnelStatus) bool { return s.Transport == "direct" })
				roundtripUDP(t, clients[0], []byte("after P2P migration"))
				roundtripTunnel(t, tcp)
			}
			if _, err := app.setManagedTunnelEnabled(context.Background(), u, v, false); err != nil {
				t.Fatal(err)
			}
			tcp.SetReadDeadline(time.Now().Add(3 * time.Second))
			if _, err := tcp.Read(make([]byte, 1)); err == nil {
				t.Fatal("TCP session survived stop")
			}
			clients[0].SetDeadline(time.Now().Add(250 * time.Millisecond))
			clients[0].Write([]byte("stopped"))
			if _, err := clients[0].Read(make([]byte, 64)); err == nil {
				t.Fatal("UDP session survived stop")
			}
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				l, e1 := net.Listen("tcp", address)
				p, e2 := net.ListenPacket("udp", address)
				if l != nil {
					l.Close()
				}
				if p != nil {
					p.Close()
				}
				if e1 == nil && e2 == nil {
					return
				}
				time.Sleep(50 * time.Millisecond)
			}
			t.Fatal("port listeners not released")
		})
	}
}
func TestPortTunnelValidation(t *testing.T) {
	app, u, o := tunnelFixture(t)
	base := store.TunnelConfig{Protocol: "port", Transports: []string{"tcp"}, Name: "port", ListenHost: "127.0.0.1", ListenPort: 1234, DestinationHost: "localhost", DestinationPort: 5678}
	for _, networks := range [][]string{nil, {}, {"tcp", "tcp"}, {"udp", "sctp"}} {
		c := base
		c.Transports = networks
		if err := app.validateTunnel(context.Background(), u, o.ID, c); err == nil {
			t.Fatalf("accepted transports %v", networks)
		}
	}
	for _, protocol := range []string{"", "tcp", "udp"} {
		c := base
		c.Protocol = protocol
		if err := app.validateTunnel(context.Background(), u, o.ID, c); err == nil {
			t.Fatalf("accepted obsolete protocol %q", protocol)
		}
	}
	ssh := tunnelSSHTarget(t, app, u, o, "udp-ssh", tunnelTestSSH(t), "")
	for _, entry := range []bool{false, true} {
		c := base
		c.Transports = []string{"tcp", "udp"}
		if entry {
			c.EntryTargetID = ssh.ID
		} else {
			c.ExitTargetID = ssh.ID
		}
		if err := app.validateTunnel(context.Background(), u, o.ID, c); err == nil {
			t.Fatal("accepted UDP SSH endpoint")
		}
	}
}

func TestUDPForwardEdgeMultiplexerSurvivesServerLoss(t *testing.T) {
	app, u, o := tunnelFixture(t)
	echo := tunnelUDPEcho(t, "127.0.0.1:0")
	entryAgent := attachForwardTestAgent(t, app, "udp-edge-entry")
	exitAgent := attachForwardTestAgent(t, app, "udp-edge-exit")
	entry := tunnelAgentTarget(t, app, u, o, entryAgent.id)
	exit := tunnelAgentTarget(t, app, u, o, exitAgent.id)
	view, err := app.saveManagedTunnel(context.Background(), u, store.Tunnel{OrganizationID: o.ID, CreatedBy: u.ID, TunnelConfig: store.TunnelConfig{Protocol: "port", Transports: []string{"udp"}, Name: "UDP edge", EntryTargetID: entry.ID, ExitTargetID: exit.ID, ListenHost: "127.0.0.1", ListenPort: freeTunnelPort(t), DestinationHost: "127.0.0.1", DestinationPort: echo.LocalAddr().(*net.UDPAddr).Port}})
	if err != nil {
		t.Fatal(err)
	}
	view, err = app.setManagedTunnelEnabled(context.Background(), u, view.Tunnel, true)
	if err != nil {
		t.Fatal(err)
	}
	waitForward(t, app, view.Tunnel, func(s tunnelStatus) bool { return s.Status == "running" && s.Transport == "direct" })
	c, err := net.Dial("udp", net.JoinHostPort(view.ListenHost, strconv.Itoa(view.ListenPort)))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	roundtripUDP(t, c, []byte("before signaling loss"))
	entryAgent.disconnect()
	exitAgent.disconnect()
	roundtripUDP(t, c, []byte("existing UDP session after loss"))
	fresh, err := net.Dial("udp", net.JoinHostPort(view.ListenHost, strconv.Itoa(view.ListenPort)))
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	roundtripUDP(t, fresh, []byte("new UDP session after loss"))
}
