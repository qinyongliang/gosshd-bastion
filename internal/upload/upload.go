// Package upload defines bounded, checksummed records inside a reliable tunnel stream.
package upload

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"hash/crc32"
	"io"

	"github.com/qinyongliang/gosshd-bastion/internal/tunnel"
)

const (
	Chunk        byte  = 1
	Finish       byte  = 2
	MaxChunkSize       = tunnel.MaxPayload - 9
	MaxSize      int64 = 1 << 30
)

// ChunkRecord fits in one tunnel packet, avoiding additional buffering.
func ChunkRecord(data []byte) []byte {
	b := make([]byte, 9+len(data))
	b[0] = Chunk
	binary.BigEndian.PutUint32(b[1:5], uint32(4+len(data)))
	binary.BigEndian.PutUint32(b[5:9], crc32.ChecksumIEEE(data))
	copy(b[9:], data)
	return b
}

func FinishRecord() []byte { return []byte{Finish, 0, 0, 0, 0} }

// Receive acknowledges progress only after the destination accepts bytes.
// The caller commits the temporary file after this returns successfully.
func Receive(r io.Reader, dst io.Writer, size int64, progress func(int64)) (string, error) {
	if size < 0 || size > MaxSize {
		return "", errors.New("invalid upload size")
	}
	hash := sha256.New()
	var written int64
	for {
		var header [5]byte
		if _, err := io.ReadFull(r, header[:]); err != nil {
			return "", err
		}
		n := binary.BigEndian.Uint32(header[1:])
		if header[0] == Finish && n == 0 {
			if written != size {
				return "", errors.New("upload size mismatch")
			}
			return hex.EncodeToString(hash.Sum(nil)), nil
		}
		if header[0] != Chunk || n <= 4 || n > MaxChunkSize+4 || written+int64(n-4) > size {
			return "", errors.New("invalid upload chunk")
		}
		data := make([]byte, n)
		if _, err := io.ReadFull(r, data); err != nil {
			return "", err
		}
		if crc32.ChecksumIEEE(data[4:]) != binary.BigEndian.Uint32(data[:4]) {
			return "", errors.New("upload checksum mismatch")
		}
		count, err := dst.Write(data[4:])
		if err != nil {
			return "", err
		}
		if count != len(data)-4 {
			return "", io.ErrShortWrite
		}
		_, _ = hash.Write(data[4:])
		written += int64(count)
		if progress != nil {
			progress(written)
		}
	}
}
