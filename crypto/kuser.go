package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
)

const (
	KUserSize       = 32
	selfTestPayload = "OMUR-SELFTEST-V1"
	selfTestAAD     = "omur-selftest"
)

type KUser [KUserSize]byte

func NewKUser() (KUser, error) {
	var k KUser
	if _, err := rand.Read(k[:]); err != nil {
		return k, fmt.Errorf("kuser rand: %w", err)
	}
	return k, nil
}

func (k *KUser) Zero() {
	for i := range k {
		k[i] = 0
	}
}

func (k *KUser) aead() (cipher.AEAD, error) {
	block, err := aes.NewCipher(k[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func (k *KUser) NewSelfTest() (nonce, ciphertext []byte, err error) {
	a, err := k.aead()
	if err != nil {
		return nil, nil, err
	}
	nonce = make([]byte, a.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, err
	}
	ct := a.Seal(nil, nonce, []byte(selfTestPayload), []byte(selfTestAAD))
	return nonce, ct, nil
}

func (k *KUser) VerifySelfTest(nonce, ciphertext []byte) error {
	a, err := k.aead()
	if err != nil {
		return err
	}
	pt, err := a.Open(nil, nonce, ciphertext, []byte(selfTestAAD))
	if err != nil {
		return fmt.Errorf("selftest open: %w", err)
	}
	if string(pt) != selfTestPayload {
		return errors.New("selftest payload mismatch")
	}
	return nil
}
