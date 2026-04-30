// encryption.go — encryption module.
//
// exports: ErrInvalidToken | ErrInvalidKey | GenerateKey | Encrypt | Decrypt | MaskSecret
// used_by: none
// rules:   none
// agent:   codedna-cli (no-llm) | codedna-cli | 2026-04-30 | codedna-cli | initial CodeDNA annotation pass
// message: 

// Package encryption provides Fernet-compatible encryption for Omur settings secrets.
package encryption

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"time"
)

// Fernet token format: Version (1) | Timestamp (8) | IV (16) | Ciphertext (n) | HMAC (32)

var (
	ErrInvalidToken = errors.New("invalid or corrupted token")
	ErrInvalidKey   = errors.New("invalid key: must be 32 bytes URL-safe base64")
)

// GenerateKey creates a new Fernet-compatible 32-byte key, URL-safe base64 encoded.
func GenerateKey() (string, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(key), nil
}

// Encrypt encrypts plaintext using a Fernet key. Returns URL-safe base64 token.
func Encrypt(plaintext, key string) (string, error) {
	k, err := decodeKey(key)
	if err != nil {
		return "", err
	}
	signingKey := k[:16]
	encryptionKey := k[16:]

	iv := make([]byte, aes.BlockSize)
	if _, err := rand.Read(iv); err != nil {
		return "", err
	}

	// PKCS7 pad
	padded := pkcs7Pad([]byte(plaintext), aes.BlockSize)

	block, err := aes.NewCipher(encryptionKey)
	if err != nil {
		return "", err
	}
	ciphertext := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ciphertext, padded)

	// Build token: version(1) + timestamp(8) + iv(16) + ciphertext
	now := uint64(time.Now().Unix())
	token := make([]byte, 0, 1+8+16+len(ciphertext)+32)
	token = append(token, 0x80) // version
	ts := make([]byte, 8)
	binary.BigEndian.PutUint64(ts, now)
	token = append(token, ts...)
	token = append(token, iv...)
	token = append(token, ciphertext...)

	// HMAC-SHA256
	mac := hmac.New(sha256.New, signingKey)
	mac.Write(token)
	token = append(token, mac.Sum(nil)...)

	return base64.URLEncoding.EncodeToString(token), nil
}

// Decrypt decrypts a Fernet token. Returns ErrInvalidToken on failure.
func Decrypt(token, key string) (string, error) {
	k, err := decodeKey(key)
	if err != nil {
		return "", err
	}
	signingKey := k[:16]
	encryptionKey := k[16:]

	raw, err := base64.URLEncoding.DecodeString(token)
	if err != nil {
		return "", ErrInvalidToken
	}
	if len(raw) < 1+8+16+16+32 { // min: version + ts + iv + 1 block + hmac
		return "", ErrInvalidToken
	}
	if raw[0] != 0x80 {
		return "", ErrInvalidToken
	}

	payload := raw[:len(raw)-32]
	expectedMAC := raw[len(raw)-32:]

	mac := hmac.New(sha256.New, signingKey)
	mac.Write(payload)
	if !hmac.Equal(mac.Sum(nil), expectedMAC) {
		return "", ErrInvalidToken
	}

	iv := raw[9:25]
	ciphertext := raw[25 : len(raw)-32]

	block, err := aes.NewCipher(encryptionKey)
	if err != nil {
		return "", ErrInvalidToken
	}
	plain := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plain, ciphertext)

	unpadded, err := pkcs7Unpad(plain)
	if err != nil {
		return "", ErrInvalidToken
	}
	return string(unpadded), nil
}

// MaskSecret masks a secret for display.
func MaskSecret(value string) string {
	n := len(value)
	if n == 0 {
		return ""
	}
	if n >= 10 {
		return value[:4] + "****" + value[n-4:]
	}
	if n >= 4 {
		return value[:2] + "****" + value[n-2:]
	}
	return "****"
}

func decodeKey(key string) ([]byte, error) {
	k, err := base64.URLEncoding.DecodeString(key)
	if err != nil {
		return nil, ErrInvalidKey
	}
	if len(k) != 32 {
		return nil, ErrInvalidKey
	}
	return k, nil
}

func pkcs7Pad(data []byte, blockSize int) []byte {
	padding := blockSize - len(data)%blockSize
	pad := make([]byte, padding)
	for i := range pad {
		pad[i] = byte(padding)
	}
	return append(data, pad...)
}

func pkcs7Unpad(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, ErrInvalidToken
	}
	padding := int(data[len(data)-1])
	if padding == 0 || padding > len(data) {
		return nil, ErrInvalidToken
	}
	for _, b := range data[len(data)-padding:] {
		if int(b) != padding {
			return nil, ErrInvalidToken
		}
	}
	return data[:len(data)-padding], nil
}
