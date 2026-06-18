package agent

import (
	"bufio"
	"io"
	"log"
	"net"
	"sync"

	"github.com/qinyongliang/gosshd-bastion/internal/protocol"

	"github.com/pkg/sftp"
)

func (c *Client) handleStream(stream io.ReadWriteCloser) {
	defer stream.Close()
	reader := bufio.NewReader(stream)
	req, err := protocol.ReadJSONLine[protocol.StreamRequest](reader)
	if err != nil {
		return
	}
	switch req.Type {
	case protocol.StreamExec, protocol.StreamShell:
		c.handleCommand(stream, reader, req)
	case protocol.StreamSFTP:
		c.handleSFTP(stream, reader)
	case protocol.StreamTCP:
		c.handleTCP(stream, reader, req.Target)
	default:
		_ = protocol.WriteJSONLine(stream, protocol.StreamResponse{OK: false, Error: "unsupported stream type"})
	}
}

func (c *Client) handleSFTP(stream io.ReadWriteCloser, reader *bufio.Reader) {
	if err := protocol.WriteJSONLine(stream, protocol.StreamResponse{OK: true}); err != nil {
		return
	}
	server, err := sftp.NewServer(readWriteCloser{Reader: reader, Writer: stream, Closer: stream})
	if err != nil {
		log.Printf("sftp server init failed: %v", err)
		return
	}
	if err := server.Serve(); err != nil && err != io.EOF {
		log.Printf("sftp server failed: %v", err)
	}
	_ = server.Close()
}

type readWriteCloser struct {
	io.Reader
	io.Writer
	io.Closer
}

func (c *Client) handleTCP(stream io.ReadWriteCloser, reader *bufio.Reader, target string) {
	if target == "" {
		_ = protocol.WriteJSONLine(stream, protocol.StreamResponse{OK: false, Error: "missing target"})
		return
	}
	conn, err := net.Dial("tcp", target)
	if err != nil {
		_ = protocol.WriteJSONLine(stream, protocol.StreamResponse{OK: false, Error: err.Error()})
		return
	}
	defer conn.Close()
	if err := protocol.WriteJSONLine(stream, protocol.StreamResponse{OK: true}); err != nil {
		return
	}
	bridge(conn, struct {
		io.Reader
		io.Writer
		io.Closer
	}{Reader: reader, Writer: stream, Closer: stream})
}

func bridge(a io.ReadWriter, b io.ReadWriteCloser) {
	var closeOnce sync.Once
	closeBoth := func() {
		closeOnce.Do(func() {
			if c, ok := a.(io.Closer); ok {
				_ = c.Close()
			}
			_ = b.Close()
		})
	}
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(b, a)
		closeBoth()
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(a, b)
		closeBoth()
		done <- struct{}{}
	}()
	<-done
}

func closeWriter(v any) error {
	if c, ok := v.(interface{ CloseWrite() error }); ok {
		return c.CloseWrite()
	}
	return nil
}
