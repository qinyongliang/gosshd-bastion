package upload

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"strings"
	"testing"
)

func TestReceiveValidatesUploadRecords(t *testing.T) {
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
		{"oversized", valid, 1, "invalid upload chunk"},
		{"unbounded allocation", huge, MaxSize, "invalid upload chunk"},
		{"negative size", FinishRecord(), -1, "invalid upload size"},
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
