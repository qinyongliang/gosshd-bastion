package forward

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"time"
)

const maxDatagramSize = 65535

// DialUDP exposes a connected UDP socket as a length-framed byte stream, so the
// existing relay/P2P multiplexers preserve packet boundaries without changes.
func DialUDP(ctx context.Context, address string) (net.Conn, error) {
	c, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "udp", address)
	if err != nil {
		return nil, err
	}
	return &datagramStream{Conn: c}, nil
}

type datagramStream struct {
	net.Conn
	readMu, writeMu   sync.Mutex
	readBuf, writeBuf []byte
	frame             []byte
}

func (c *datagramStream) Read(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}
	c.readMu.Lock()
	defer c.readMu.Unlock()
	if len(c.readBuf) == 0 {
		if c.frame == nil {
			c.frame = make([]byte, maxDatagramSize+2)
		}
		buf := c.frame
		n, err := c.Conn.Read(buf[2:])
		if err != nil {
			return 0, err
		}
		binary.BigEndian.PutUint16(buf, uint16(n))
		c.readBuf = buf[:n+2]
	}
	n := copy(b, c.readBuf)
	c.readBuf = c.readBuf[n:]
	return n, nil
}
func (c *datagramStream) Write(b []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.writeBuf = append(c.writeBuf, b...)
	for len(c.writeBuf) >= 2 {
		n := int(binary.BigEndian.Uint16(c.writeBuf))
		if len(c.writeBuf) < n+2 {
			break
		}
		written, err := c.Conn.Write(c.writeBuf[2 : n+2])
		if err != nil {
			return 0, err
		}
		if written != n {
			return 0, io.ErrShortWrite
		}
		c.writeBuf = c.writeBuf[n+2:]
	}
	return len(b), nil
}
func writeDatagram(w io.Writer, b []byte) error {
	if len(b) > maxDatagramSize {
		return errors.New("UDP datagram too large")
	}
	frame := make([]byte, len(b)+2)
	binary.BigEndian.PutUint16(frame, uint16(len(b)))
	copy(frame[2:], b)
	for len(frame) > 0 {
		n, e := w.Write(frame)
		if e != nil {
			return e
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		frame = frame[n:]
	}
	return nil
}
func readDatagram(r io.Reader, b []byte) (int, error) {
	var header [2]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return 0, err
	}
	n := int(binary.BigEndian.Uint16(header[:]))
	if n > len(b) {
		return 0, io.ErrShortBuffer
	}
	_, err := io.ReadFull(r, b[:n])
	return n, err
}

// DialNetwork only accepts the networks present in immutable route grants.
func DialNetwork(ctx context.Context, network, address string) (net.Conn, error) {
	if network == "udp" {
		return DialUDP(ctx, address)
	}
	if network != "tcp" {
		return nil, errors.New("unsupported forward network")
	}
	return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", address)
}
