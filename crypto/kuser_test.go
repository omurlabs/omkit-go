package crypto

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func TestKUserSelfTestRoundTrip(t *testing.T) {
	var k KUser
	if _, err := rand.Read(k[:]); err != nil {
		t.Fatal(err)
	}
	nonce, ct, err := k.NewSelfTest()
	if err != nil {
		t.Fatal(err)
	}
	if err := k.VerifySelfTest(nonce, ct); err != nil {
		t.Fatalf("verify: %v", err)
	}
	// Mutate ciphertext → must fail.
	ct[0] ^= 0xff
	if err := k.VerifySelfTest(nonce, ct); err == nil {
		t.Fatal("expected tamper to fail")
	}
}

func TestKUserZero(t *testing.T) {
	var k KUser
	copy(k[:], []byte("0123456789abcdef0123456789abcdef"))
	k.Zero()
	if !bytes.Equal(k[:], make([]byte, 32)) {
		t.Fatalf("zero failed: %x", k[:])
	}
}
