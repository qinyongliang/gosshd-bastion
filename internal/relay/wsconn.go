package relay

import (
	"io"
	"net"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type WSConn struct {
	ws        *websocket.Conn
	readerMu  sync.Mutex
	writerMu  sync.Mutex
	reader    io.Reader
	closed    chan struct{}
	closeOnce sync.Once
}

func NewWSConn(ws *websocket.Conn) *WSConn {
	return &WSConn{ws: ws, closed: make(chan struct{})}
}

func (c *WSConn) Read(p []byte) (int, error) {
	c.readerMu.Lock()
	defer c.readerMu.Unlock()
	for {
		if c.reader == nil {
			messageType, reader, err := c.ws.NextReader()
			if err != nil {
				c.Close()
				return 0, err
			}
			if messageType != websocket.BinaryMessage {
				continue
			}
			c.reader = reader
		}
		n, err := c.reader.Read(p)
		if err == io.EOF {
			c.reader = nil
			if n > 0 {
				return n, nil
			}
			continue
		}
		if err != nil {
			c.Close()
		}
		return n, err
	}
}

func (c *WSConn) Write(p []byte) (int, error) {
	c.writerMu.Lock()
	defer c.writerMu.Unlock()
	w, err := c.ws.NextWriter(websocket.BinaryMessage)
	if err != nil {
		c.Close()
		return 0, err
	}
	n, err := w.Write(p)
	closeErr := w.Close()
	if err != nil {
		c.Close()
		return n, err
	}
	if closeErr != nil {
		c.Close()
		return n, closeErr
	}
	return n, nil
}

func (c *WSConn) Close() error {
	var err error
	c.closeOnce.Do(func() {
		close(c.closed)
		err = c.ws.Close()
	})
	return err
}

func (c *WSConn) LocalAddr() net.Addr {
	return c.ws.LocalAddr()
}

func (c *WSConn) RemoteAddr() net.Addr {
	return c.ws.RemoteAddr()
}

func (c *WSConn) SetDeadline(t time.Time) error {
	if err := c.ws.SetReadDeadline(t); err != nil {
		return err
	}
	return c.ws.SetWriteDeadline(t)
}

func (c *WSConn) SetReadDeadline(t time.Time) error {
	return c.ws.SetReadDeadline(t)
}

func (c *WSConn) SetWriteDeadline(t time.Time) error {
	return c.ws.SetWriteDeadline(t)
}
