// Package tunnel provides a stable byte stream across relay and direct paths.
// Sequence numbers belong to the stream, so changing paths never replaces TCP sockets.
package tunnel

import (
	"encoding/binary"
	"errors"
	"io"
	"sync"
)

const (
	Data byte = iota + 1
	Ack
	Fin
	Signal
	Stats
	Probe
	ProbeAck
	PathInfo
)
const MaxPayload = 8192
const maxPacket = 64 * 1024
const window = 64

type Packet struct {
	Kind byte
	Seq  uint64
	Body []byte
}

func (p Packet) Bytes() []byte {
	b := make([]byte, 9+len(p.Body))
	b[0] = p.Kind
	binary.BigEndian.PutUint64(b[1:9], p.Seq)
	copy(b[9:], p.Body)
	return b
}
func Parse(b []byte) (Packet, error) {
	if len(b) < 9 || len(b) > maxPacket {
		return Packet{}, errors.New("invalid tunnel packet")
	}
	return Packet{b[0], binary.BigEndian.Uint64(b[1:9]), b[9:]}, nil
}

type Relay struct {
	Reader io.Reader
	Writer io.Writer
	Closer io.Closer
	mu     sync.Mutex
}

func (r *Relay) Read() (Packet, error) {
	var size [4]byte
	if _, err := io.ReadFull(r.Reader, size[:]); err != nil {
		return Packet{}, err
	}
	n := binary.BigEndian.Uint32(size[:])
	if n < 9 || n > maxPacket {
		return Packet{}, errors.New("invalid tunnel packet size")
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r.Reader, b); err != nil {
		return Packet{}, err
	}
	return Parse(b)
}
func (r *Relay) Send(p Packet) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	b := p.Bytes()
	if len(b) > maxPacket {
		return errors.New("tunnel packet too large")
	}
	buf := make([]byte, 4+len(b))
	binary.BigEndian.PutUint32(buf, uint32(len(b)))
	copy(buf[4:], b)
	_, err := io.Copy(r.Writer, bytesReader(buf))
	return err
}
func (r *Relay) Close() error { return r.Closer.Close() }

// io.Copy handles writers returning partial writes.
type sliceReader struct{ b []byte }

func (s *sliceReader) Read(p []byte) (int, error) {
	if len(s.b) == 0 {
		return 0, io.EOF
	}
	n := copy(p, s.b)
	s.b = s.b[n:]
	return n, nil
}
func bytesReader(b []byte) io.Reader { return &sliceReader{b: b} }
