package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
	"github.com/qinyongliang/gosshd-bastion/internal/tunnel"
)

func (c *Client) handleFileSession(stream io.ReadWriteCloser, reader *bufio.Reader, req protocol.StreamRequest) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	setup := time.AfterFunc(20*time.Second, cancel)
	defer setup.Stop()
	client, closeClient, err := openFileSFTP(ctx, req.TunnelHops)
	if err != nil {
		_ = protocol.WriteJSONLine(stream, protocol.StreamResponse{Error: err.Error()})
		return
	}
	defer closeClient()
	// The server pins credentials for the lifetime of this session. Subsequent
	// operations contain only an authorized path/size and retain the direction.
	action, hops := req.FileAction, req.TunnelHops
	open := func(op protocol.StreamRequest) (*uploadDestination, *downloadSource, error) {
		op.TunnelHops = hops
		if action == "upload" && op.Upload != nil && op.Download == nil {
			dst, err := openUploadDestination(ctx, op, client)
			return dst, nil, err
		}
		if action == "download" && op.Download != nil && op.Upload == nil {
			src, err := openDownloadSource(ctx, op, client)
			return nil, src, err
		}
		return nil, nil, errors.New("invalid file operation")
	}
	dst, src, err := open(req)
	if err != nil {
		_ = protocol.WriteJSONLine(stream, protocol.StreamResponse{Error: err.Error()})
		return
	}
	closeFile := func() {
		if dst != nil {
			dst.abort()
		}
		if src != nil {
			_ = src.Close()
		}
	}
	defer func() { closeFile() }()
	size := func() int64 {
		if src != nil {
			return src.size
		}
		return req.Upload.Size
	}
	setup.Stop()
	if protocol.WriteJSONLine(stream, protocol.StreamResponse{OK: true, Peer: true, Reuse: true, Size: size()}) != nil {
		return
	}
	operations := make(chan protocol.StreamRequest, 1)
	stable, relay, cleanup := startFileTransfer(ctx, cancel, stream, reader, req, func(body []byte) {
		var op protocol.StreamRequest
		if json.Unmarshal(body, &op) != nil {
			_ = stream.Close()
			return
		}
		select {
		case operations <- op:
		default:
			_ = stream.Close()
		}
	})
	defer cleanup()
	for {
		before := stable.Counters()
		send := func(status protocol.FileTransferStatus) error {
			status.Direct = stable.Direct()
			n := stable.Counters()
			status.DirectBytes = n.ReceivedDirect - before.ReceivedDirect
			if action == "download" {
				status.DirectBytes = n.SentDirect - before.SentDirect
			}
			body, _ := json.Marshal(status)
			return relay.Send(tunnel.Packet{Kind: tunnel.TransferStatus, Body: body})
		}
		var status protocol.FileTransferStatus
		if dst != nil {
			status = receiveFileUpload(stable, dst, req.Upload.Size, send)
		} else {
			status = sendFileDownload(stable, src)
		}
		closeFile()
		dst, src = nil, nil
		if send(status) != nil || status.Type == "error" {
			return
		}
		select {
		case req = <-operations:
		case <-ctx.Done():
			return
		}
		// Restore the pinned hops for cancellation cleanup; they are never supplied
		// by the browser or the direct peer.
		dst, src, err = open(req)
		if err != nil {
			_ = send(protocol.FileTransferStatus{Type: "error", Error: err.Error()})
			return
		}
		if send(protocol.FileTransferStatus{Type: "ready", Loaded: size()}) != nil {
			return
		}
	}
}
