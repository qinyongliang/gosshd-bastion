// Package filetransfer defines bounded, checksummed records inside a reliable tunnel stream.
package filetransfer

import (
	"bufio"
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

// Send emits exactly size bytes and a finish record. Growth or truncation of the
// source fails the transfer rather than silently returning a different file.
func Send(dst io.Writer, src io.Reader, size int64) (string, error) {
	if size < 0 {
		return "", errors.New("invalid file transfer size")
	}
	hash := sha256.New()
	// Batch source reads so a delegated SFTP source does not require an SSH
	// round trip for every small tunnel record. Buffering stays bounded.
	reader := bufio.NewReaderSize(src, MaxChunkSize*32)
	buffer := make([]byte, MaxChunkSize)
	for remaining := size; remaining > 0; {
		n, err := io.ReadFull(reader, buffer[:min(remaining, int64(len(buffer)))])
		if err != nil {
			return "", err
		}
		data := buffer[:n]
		if err := writeRecord(dst, ChunkRecord(data)); err != nil {
			return "", err
		}
		_, _ = hash.Write(data)
		remaining -= int64(n)
	}
	var extra [1]byte
	if n, err := reader.Read(extra[:]); n != 0 || err != io.EOF {
		return "", errors.New("file transfer size changed")
	}
	if err := writeRecord(dst, FinishRecord()); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func writeRecord(dst io.Writer, record []byte) error {
	n, err := dst.Write(record)
	if err == nil && n != len(record) {
		return io.ErrShortWrite
	}
	return err
}

// Receive acknowledges progress only after the destination accepts bytes.
// The caller commits the temporary file after this returns successfully.
func Receive(r io.Reader, dst io.Writer, size int64, progress func(int64)) (string, error) {
	if size < 0 || size > MaxSize {
		return "", errors.New("invalid file transfer size")
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
				return "", errors.New("file transfer size mismatch")
			}
			return hex.EncodeToString(hash.Sum(nil)), nil
		}
		if header[0] != Chunk || n <= 4 || n > MaxChunkSize+4 || written+int64(n-4) > size {
			return "", errors.New("invalid file transfer chunk")
		}
		data := make([]byte, n)
		if _, err := io.ReadFull(r, data); err != nil {
			return "", err
		}
		if crc32.ChecksumIEEE(data[4:]) != binary.BigEndian.Uint32(data[:4]) {
			return "", errors.New("file transfer checksum mismatch")
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
