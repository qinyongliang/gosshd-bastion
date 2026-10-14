package agent

import (
	"bufio"
	"encoding/json"
	"github.com/qinyongliang/gosshd-bastion/internal/desktop"
	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
	"net"
	"testing"
)

func TestDesktopStreamExplicitOptOut(t *testing.T) {
	client, err := NewEmbedded(Config{DesktopDisabled: true}, "desktop-test")
	if err != nil {
		t.Fatal(err)
	}
	a, b := net.Pipe()
	defer a.Close()
	done := make(chan struct{})
	go func() { client.handleStream(b); close(done) }()
	if err = protocol.WriteJSONLine(a, protocol.StreamRequest{Type: protocol.StreamDesktop}); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(a).ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var response desktop.Response
	if err = json.Unmarshal(line, &response); err != nil || response.OK || response.ID != "init" {
		t.Fatalf("unexpected desktop init %s %v", line, err)
	}
	<-done
}
