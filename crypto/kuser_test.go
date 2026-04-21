package crypto

import (
	"bytes"
	"crypto/rand"
	"runtime"
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

// TestKUserZero_LeavesNoResidualBytes fills every byte with a distinct
// non-zero value and verifies Zero() truly clears them all, then uses
// runtime.KeepAlive to force the compiler to treat k as observed and NOT
// elide the zero loop under dead-code elimination.
func TestKUserZero_LeavesNoResidualBytes(t *testing.T) {
	var k KUser
	for i := range k {
		k[i] = byte(i + 1)
	}
	k.Zero()
	for i, b := range k {
		if b != 0 {
			t.Fatalf("byte %d non-zero after Zero(): 0x%02x", i, b)
		}
	}
	runtime.KeepAlive(&k)
}

// BenchmarkKUserZero exists so the compiler's dead-code elimination can be
// inspected via `go test -gcflags='-m' -bench=BenchmarkKUserZero ...`.
// The loop body reads + writes k, so the zero stores must be emitted for
// the benchmark to produce valid output.
func BenchmarkKUserZero(b *testing.B) {
	var k KUser
	for i := 0; i < b.N; i++ {
		for j := range k {
			k[j] = byte(j + 1)
		}
		k.Zero()
	}
	runtime.KeepAlive(&k)
}
