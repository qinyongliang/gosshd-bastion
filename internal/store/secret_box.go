package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
)

const secretBoxPrefix = "gosshd:v1:"

type SecretBox struct {
	aead cipher.AEAD
}

func NewSecretBox(keyMaterial []byte) (*SecretBox, error) {
	if len(keyMaterial) == 0 {
		return nil, nil
	}
	hash := sha256.Sum256(keyMaterial)
	block, err := aes.NewCipher(hash[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &SecretBox{aead: aead}, nil
}

func (b *SecretBox) Seal(value []byte) ([]byte, error) {
	if b == nil || len(value) == 0 {
		return append([]byte(nil), value...), nil
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	sealed := b.aead.Seal(nonce, nonce, value, nil)
	out := secretBoxPrefix + base64.RawStdEncoding.EncodeToString(sealed)
	return []byte(out), nil
}

func (b *SecretBox) Open(value []byte) ([]byte, error) {
	if b == nil || len(value) == 0 || !strings.HasPrefix(string(value), secretBoxPrefix) {
		return append([]byte(nil), value...), nil
	}
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(string(value), secretBoxPrefix))
	if err != nil || len(raw) < b.aead.NonceSize() {
		return nil, errors.New("invalid encrypted secret")
	}
	nonce := raw[:b.aead.NonceSize()]
	return b.aead.Open(nil, nonce, raw[b.aead.NonceSize():], nil)
}
