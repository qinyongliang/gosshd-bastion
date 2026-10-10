package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"io"

	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
	"github.com/qinyongliang/gosshd-bastion/internal/tunnel"
)

func startFileTransfer(ctx context.Context, cancel context.CancelFunc, stream io.ReadWriteCloser, reader *bufio.Reader, req protocol.StreamRequest) (*tunnel.Conn, *tunnel.Relay, func()) {
	relay := &tunnel.Relay{Reader: reader, Writer: stream, Closer: stream}
	stable := tunnel.NewConn(relay)
	negotiator := tunnel.NewNegotiator(stable, false, req.STUNServers)
	go stable.Run()
	go negotiator.Run()
	go func() {
		select {
		case <-stable.Done():
			cancel()
		case <-ctx.Done():
		}
	}()
	return stable, relay, func() {
		cancel()
		_ = stable.Close()
		negotiator.Close()
	}
}

func sendFileStatus(stable *tunnel.Conn, relay *tunnel.Relay, status protocol.FileTransferStatus, download bool) error {
	status.Direct = stable.Direct()
	status.DirectBytes = stable.Counters().ReceivedDirect
	if download {
		status.DirectBytes = stable.Counters().SentDirect
	}
	body, _ := json.Marshal(status)
	return relay.Send(tunnel.Packet{Kind: tunnel.TransferStatus, Body: body})
}
