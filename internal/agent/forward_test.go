package agent

import (
	"bytes"
	"context"
	"github.com/google/uuid"
	"github.com/qinyongliang/gosshd-bastion/internal/forward"
	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestForwardEncryptedSnapshotRestoresLocalTCPWithoutServer(t *testing.T) {
	echo, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echo.Close()
	go func() {
		for {
			conn, err := echo.Accept()
			if err != nil {
				return
			}
			go func() { defer conn.Close(); io.Copy(conn, conn) }()
		}
	}()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	dir := t.TempDir()
	cfg := Config{Root: dir, IDFile: filepath.Join(dir, "agent.json")}
	client, err := NewEmbedded(cfg, "edge-cache")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	m, err := client.forwarder()
	if err != nil {
		t.Fatal(err)
	}
	route := forward.Location{ID: "tcp", Host: "127.0.0.1", Port: echo.Addr().(*net.TCPAddr).Port}
	d := protocol.ForwardDeployment{ID: uuid.NewString(), Revision: 1, Protocol: "port", Transports: []string{"tcp"}, ListenAddress: address, Destination: &route, Routes: []protocol.ForwardRoute{{ID: "tcp", Network: "tcp", Address: echo.Addr().String()}}}
	m.mu.Lock()
	m.apply(d)
	if err := m.persist([]protocol.ForwardDeployment{d}); err != nil {
		m.mu.Unlock()
		t.Fatal(err)
	}
	m.mu.Unlock()
	roundtrip := func() {
		conn, err := net.DialTimeout("tcp", address, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(time.Second))
		payload := []byte("cached edge TCP")
		conn.Write(payload)
		got := make([]byte, len(payload))
		if _, err := io.ReadFull(conn, got); err != nil || !bytes.Equal(got, payload) {
			t.Fatalf("%q %v", got, err)
		}
	}
	roundtrip()
	client.Close()
	ciphertext, err := os.ReadFile(filepath.Join(m.dir, "rules.enc"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ciphertext, []byte(d.ID)) || bytes.Contains(ciphertext, []byte(address)) {
		t.Fatal("plaintext rule cache")
	}
	restarted, err := NewEmbedded(cfg, "edge-cache")
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	restored, err := restarted.forwarder()
	if err != nil {
		t.Fatal(err)
	}
	if len(restored.runs) != 1 {
		t.Fatal("rule not restored")
	}
	roundtrip()
	restarted.Close()
	logs, err := forward.OpenLogs(filepath.Join(m.dir, "logs"), d.ID, 7)
	if err != nil {
		t.Fatal(err)
	}
	defer logs.Close()
	stats, err := logs.Stats(context.Background())
	if err != nil || stats.Requests != 2 || stats.DirectUp != 30 || stats.DirectDown != 30 {
		t.Fatalf("restored totals: %+v %v", stats, err)
	}
}
func TestForwardAgentAbsoluteExpirationWithoutServer(t *testing.T) {
	dir := t.TempDir()
	client, _ := NewEmbedded(Config{Root: dir, IDFile: filepath.Join(dir, "agent.json")}, "edge-expiry")
	defer client.Close()
	m, err := client.forwarder()
	if err != nil {
		t.Fatal(err)
	}
	listener, _ := net.Listen("tcp", "127.0.0.1:0")
	address := listener.Addr().String()
	listener.Close()
	route := forward.Location{ID: "tcp", Host: "127.0.0.1", Port: 9}
	d := protocol.ForwardDeployment{ID: uuid.NewString(), Revision: 1, Protocol: "port", Transports: []string{"tcp"}, ListenAddress: address, Destination: &route, Routes: []protocol.ForwardRoute{{ID: "tcp", Network: "tcp", Address: "127.0.0.1:9"}}, ExpiresAt: time.Now().Add(200 * time.Millisecond).UnixMilli()}
	m.mu.Lock()
	m.apply(d)
	m.mu.Unlock()
	time.Sleep(300 * time.Millisecond)
	conn, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
	if err == nil {
		conn.Close()
		t.Fatal("expired Agent rule still listens without server")
	}
}

func TestForwardEncryptedSnapshotRestoresUDPWithoutServer(t *testing.T) {
	echo, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echo.Close()
	go func() {
		buf := make([]byte, 65535)
		for {
			n, a, e := echo.ReadFrom(buf)
			if e != nil {
				return
			}
			echo.WriteTo(buf[:n], a)
		}
	}()
	available, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := available.LocalAddr().String()
	available.Close()
	dir := t.TempDir()
	cfg := Config{Root: dir, IDFile: filepath.Join(dir, "agent.json")}
	client, err := NewEmbedded(cfg, "udp-cache")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	m, err := client.forwarder()
	if err != nil {
		t.Fatal(err)
	}
	route := forward.Location{ID: "port", Host: "127.0.0.1", Port: echo.LocalAddr().(*net.UDPAddr).Port}
	d := protocol.ForwardDeployment{ID: uuid.NewString(), Revision: 1, Protocol: "port", Transports: []string{"udp"}, ListenAddress: address, Destination: &route, Routes: []protocol.ForwardRoute{{ID: "udp", Network: "udp", Address: echo.LocalAddr().String()}}}
	m.mu.Lock()
	m.apply(d)
	err = m.persist([]protocol.ForwardDeployment{d})
	m.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	check := func() {
		c, e := net.Dial("udp", address)
		if e != nil {
			t.Fatal(e)
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(time.Second))
		c.Write([]byte("cached UDP"))
		buf := make([]byte, 32)
		n, e := c.Read(buf)
		if e != nil || string(buf[:n]) != "cached UDP" {
			t.Fatalf("restored UDP %q %v", buf[:n], e)
		}
	}
	check()
	m.mu.Lock()
	m.runs[d.ID].close()
	m.mu.Unlock()
	if err := m.logs[d.ID].Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	access, err := m.logs[d.ID].Query(context.Background(), forward.LogFilter{})
	if err != nil || len(access) != 0 {
		t.Fatalf("Agent port access details %+v %v", access, err)
	}
	totals, err := m.logs[d.ID].Stats(context.Background())
	if err != nil || totals.Requests != 1 || totals.Up != 10 || totals.Down != 10 {
		t.Fatalf("Agent aggregate totals %+v %v", totals, err)
	}
	client.Close()
	restarted, err := NewEmbedded(cfg, "udp-cache")
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	restored, err := restarted.forwarder()
	if err != nil {
		t.Fatal(err)
	}
	if restored.runs[d.ID] == nil || restored.runs[d.ID].err != "" {
		t.Fatal("UDP snapshot not restored")
	}
	check()
}
