package filetransfer

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"strings"
	"testing"
)

func TestReceiveValidatesTransferRecords(t *testing.T) {
	data := []byte("binary\x00\xfffile")
	valid := append(ChunkRecord(data), FinishRecord()...)
	corrupt := append([]byte(nil), valid...)
	corrupt[9] ^= 1
	huge := []byte{Chunk, 0, 0, 0, 0}
	binary.BigEndian.PutUint32(huge[1:], 0xffffffff)
	for _, tc := range []struct {
		name    string
		data    []byte
		size    int64
		failure string
	}{
		{"valid", valid, int64(len(data)), ""},
		{"empty", FinishRecord(), 0, ""},
		{"checksum", corrupt, int64(len(data)), "checksum"},
		{"short", valid, int64(len(data) + 1), "size mismatch"},
		{"oversized", valid, 1, "invalid file transfer chunk"},
		{"unbounded allocation", huge, MaxSize, "invalid file transfer chunk"},
		{"negative size", FinishRecord(), -1, "invalid file transfer size"},
		{"no finish", ChunkRecord(data), int64(len(data)), "EOF"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var dst bytes.Buffer
			var progress int64
			checksum, err := Receive(bytes.NewReader(tc.data), &dst, tc.size, func(n int64) { progress = n })
			if tc.failure != "" {
				if err == nil || !strings.Contains(err.Error(), tc.failure) {
					t.Fatalf("expected %s: %v", tc.failure, err)
				}
				return
			}
			hash := sha256.Sum256(dst.Bytes())
			if err != nil || progress != tc.size || checksum != hex.EncodeToString(hash[:]) {
				t.Fatalf("invalid result: %s %d %v", checksum, progress, err)
			}
		})
	}
}

func TestSendExactLengthAndChecksums(t *testing.T) {
	data := bytes.Repeat([]byte("binary\x00\xff"), MaxChunkSize)
	for _, tc := range []struct {
		name string
		data []byte
		size int64
		fail bool
	}{
		{"multiple chunks", data, int64(len(data)), false},
		{"empty", nil, 0, false},
		{"truncated", data[:10], 11, true},
		{"grown", data[:10], 9, true},
		{"negative", nil, -1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var records bytes.Buffer
			checksum, err := Send(&records, bytes.NewReader(tc.data), tc.size)
			if tc.fail {
				if err == nil {
					t.Fatal("invalid source accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var result bytes.Buffer
			received, err := Receive(&records, &result, tc.size, nil)
			if err != nil || received != checksum || !bytes.Equal(result.Bytes(), tc.data) {
				t.Fatalf("corrupt transfer: %s %s %v", checksum, received, err)
			}
		})
	}
}
