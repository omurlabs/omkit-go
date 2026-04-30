// kuser.go — kuser module.
//
// exports: KUserSize | KUser | NewKUser | Zero | NewSelfTest | VerifySelfTest
// used_by: none
// rules:   none
// agent:   codedna-cli (no-llm) | codedna-cli | 2026-04-30 | codedna-cli | initial CodeDNA annotation pass
// message: 

package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"runtime"
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

// Zero overwrites every byte of k with 0. runtime.KeepAlive keeps k
// observed across the zero loop so the compiler can't treat k as dead
// and elide the writes — critical for credential material where "we
// cleared this" must hold even after the caller's last logical use.
func (k *KUser) Zero() {
	for i := range k {
		k[i] = 0
	}
	runtime.KeepAlive(k)
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
