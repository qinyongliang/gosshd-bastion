package agent

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"time"

	"github.com/pkg/sftp"
	"github.com/qinyongliang/gosshd-bastion/internal/filetransfer"
	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
)

type downloadSource struct {
	io.ReadCloser
	size int64
}

type downloadReadCloser struct {
	io.Reader
	close func() error
}

func (s downloadReadCloser) Close() error { return s.close() }

func openDownloadSource(ctx context.Context, req protocol.StreamRequest) (*downloadSource, error) {
	if req.Download == nil || req.Download.Path == "" {
		return nil, errors.New("invalid download request")
	}
	source := req.Download.Path
	sshClient, err := openTunnelSSH(ctx, req.TunnelHops)
	if err != nil {
		return nil, err
	}
	if sshClient != nil {
		stop := context.AfterFunc(ctx, func() { _ = sshClient.Close() })
		client, err := sftp.NewClient(sshClient)
		cleanup := func() { stop(); _ = sshClient.Close() }
		if err != nil {
			cleanup()
			return nil, err
		}
		info, err := client.Stat(source)
		if err == nil && !info.Mode().IsRegular() {
			err = errors.New("download source is not a regular file")
		}
		if err != nil {
			_ = client.Close()
			cleanup()
			return nil, err
		}
		file, err := client.Open(source)
		if err != nil {
			_ = client.Close()
			cleanup()
			return nil, err
		}
		return &downloadSource{ReadCloser: downloadReadCloser{Reader: file, close: func() error {
			err := file.Close()
			_ = client.Close()
			cleanup()
			return err
		}}, size: info.Size()}, nil
	}
	info, err := os.Stat(source)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("download source is not a regular file")
	}
	file, err := os.Open(source)
	if err != nil {
		return nil, err
	}
	// Stat the opened file so replacement between lookup and open cannot make
	// the advertised length refer to a different inode.
	info, err = file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = file.Close()
		if err == nil {
			err = errors.New("download source is not a regular file")
		}
		return nil, err
	}
	stop := context.AfterFunc(ctx, func() { _ = file.Close() })
	return &downloadSource{ReadCloser: downloadReadCloser{Reader: file, close: func() error { stop(); return file.Close() }}, size: info.Size()}, nil
}

func (c *Client) handleFileDownload(stream io.ReadWriteCloser, reader *bufio.Reader, req protocol.StreamRequest) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	setupTimeout := time.AfterFunc(20*time.Second, cancel)
	defer setupTimeout.Stop()
	src, err := openDownloadSource(ctx, req)
	if err != nil {
		_ = protocol.WriteJSONLine(stream, protocol.StreamResponse{Error: err.Error()})
		return
	}
	defer src.Close()
	setupTimeout.Stop()
	if protocol.WriteJSONLine(stream, protocol.StreamResponse{OK: true, Peer: true, Size: src.size}) != nil {
		return
	}
	stable, relay, cleanup := startFileTransfer(ctx, cancel, stream, reader, req)
	defer cleanup()
	// Waiting for start lets the browser finish ICE and prepare its writable sink
	// before even a small file is sent. Receipt follows checksum/length checks.
	var control [1]byte
	if _, err := io.ReadFull(stable, control[:]); err != nil || control[0] != 1 {
		return
	}
	checksum, err := filetransfer.Send(stable, src, src.size)
	if err == nil {
		if _, err = io.ReadFull(stable, control[:]); err == nil && control[0] != 2 {
			err = errors.New("invalid download receipt")
		}
	}
	status := protocol.FileTransferStatus{Type: "complete", Loaded: src.size, SHA256: checksum}
	if err != nil {
		status.Type, status.Error, status.SHA256 = "error", err.Error(), ""
	}
	_ = sendFileStatus(stable, relay, status, true)
}
