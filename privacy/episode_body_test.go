package privacy

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

// fakeKMS is a deterministic in-memory KMS stub satisfying kms.KMS.
//
// Wraps DEKs by XOR-ing against a per-(user, purpose) static key and binds
// AAD as a literal suffix so AAD mismatches surface at unwrap time. Mirrors
// the production invariant that cross-user / cross-purpose / cross-AAD unwrap
// fails closed; suffices for round-trip + tamper tests.
type fakeKMS struct {
	wrappingKeys map[string][]byte
}

func newFakeKMS() *fakeKMS { return &fakeKMS{wrappingKeys: map[string][]byte{}} }

func (f *fakeKMS) keyFor(userID, purpose string) []byte {
	k := userID + "|" + purpose
	if v, ok := f.wrappingKeys[k]; ok {
		return v
	}
	v := make([]byte, 32)
	if _, err := rand.Read(v); err != nil {
		panic(err)
	}
	f.wrappingKeys[k] = v
	return v
}

func (f *fakeKMS) Wrap(_ context.Context, _ string, _, _ []byte) ([]byte, error) {
	return nil, errors.New("fakeKMS.Wrap not implemented")
}

func (f *fakeKMS) Unwrap(_ context.Context, _ string, _, _ []byte) ([]byte, error) {
	return nil, errors.New("fakeKMS.Unwrap not implemented")
}

func (f *fakeKMS) CurrentVersion(_ context.Context, _ string) (string, error) {
	return "v1", nil
}

func (f *fakeKMS) WrapDEK(_ context.Context, userID, purpose string, plainDEK, aad []byte) ([]byte, string, error) {
	kek := f.keyFor(userID, purpose)
	w := make([]byte, len(plainDEK))
	for i, b := range plainDEK {
		w[i] = b ^ kek[i%len(kek)]
	}
	out := append(w, []byte("|aad=")...)
	out = append(out, aad...)
	return out, "v1", nil
}

func (f *fakeKMS) UnwrapDEK(_ context.Context, userID, purpose string, wrapped, aad []byte) ([]byte, error) {
	sep := []byte("|aad=")
	idx := bytes.Index(wrapped, sep)
	if idx < 0 {
		return nil, errors.New("fakeKMS: malformed wrapped dek")
	}
	w, boundAAD := wrapped[:idx], wrapped[idx+len(sep):]
	if !bytes.Equal(boundAAD, aad) {
		return nil, errors.New("fakeKMS: aad mismatch at kms layer")
	}
	kek := f.keyFor(userID, purpose)
	out := make([]byte, len(w))
	for i, b := range w {
		out[i] = b ^ kek[i%len(kek)]
	}
	return out, nil
}

func (f *fakeKMS) DeleteUserKeys(_ context.Context, userID string) error {
	for k := range f.wrappingKeys {
		if strings.HasPrefix(k, userID+"|") {
			delete(f.wrappingKeys, k)
		}
	}
	return nil
}

func ctxArgs() (string, string, string) {
	return "tenant-alice", "ep-2026-05-08-001", "lab_result"
}

func TestBuildAAD_Format(t *testing.T) {
	got, err := BuildAAD("ep1", "t1", "lab_result")
	if err != nil {
		t.Fatalf("BuildAAD: %v", err)
	}
	want := "omur:gnokee:episode:ep1:t1:lab_result"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestBuildAAD_RejectsColonInParts(t *testing.T) {
	cases := [][3]string{
		{"ep:1", "t1", "lab_result"},
		{"ep1", "t:1", "lab_result"},
		{"ep1", "t1", "lab:result"},
	}
	for _, c := range cases {
		if _, err := BuildAAD(c[0], c[1], c[2]); err == nil {
			t.Fatalf("expected error for %v", c)
		}
	}
}

func TestBuildAAD_RejectsEmpty(t *testing.T) {
	if _, err := BuildAAD("", "t1", "lab_result"); err == nil {
		t.Fatal("expected error for empty episodeID")
	}
}

func TestRoundTrip_PlaintextRecovered(t *testing.T) {
	k := newFakeKMS()
	tenant, ep, schema := ctxArgs()
	plaintext := []byte("specimen note: hemolysed")

	env, err := EncryptEpisodeBody(context.Background(), k, plaintext, tenant, ep, schema)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	got, err := DecryptEpisodeBody(context.Background(), k, env, tenant, ep, schema)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("recovered %q want %q", got, plaintext)
	}
}

func TestRoundTrip_EnvelopeShape(t *testing.T) {
	k := newFakeKMS()
	tenant, ep, schema := ctxArgs()
	env, err := EncryptEpisodeBody(context.Background(), k, []byte("x"), tenant, ep, schema)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if env.Schema != SchemaV1 {
		t.Fatalf("schema=%q", env.Schema)
	}
	if env.Alg != AlgAES256GCM {
		t.Fatalf("alg=%q", env.Alg)
	}
	if !strings.HasSuffix(env.KeyID, ":v1") {
		t.Fatalf("key_id=%q does not end in :v1", env.KeyID)
	}
	wantAAD := "omur:gnokee:episode:" + ep + ":" + tenant + ":" + schema
	if env.AAD != wantAAD {
		t.Fatalf("aad=%q want %q", env.AAD, wantAAD)
	}
}

func TestTamper_AADMismatchEpisodeID(t *testing.T) {
	k := newFakeKMS()
	tenant, ep, schema := ctxArgs()
	env, _ := EncryptEpisodeBody(context.Background(), k, []byte("x"), tenant, ep, schema)
	if _, err := DecryptEpisodeBody(context.Background(), k, env, tenant, "ep-OTHER", schema); !errors.Is(err, ErrAADMismatch) {
		t.Fatalf("want ErrAADMismatch, got %v", err)
	}
}

func TestTamper_AADMismatchTenant(t *testing.T) {
	k := newFakeKMS()
	tenant, ep, schema := ctxArgs()
	env, _ := EncryptEpisodeBody(context.Background(), k, []byte("x"), tenant, ep, schema)
	if _, err := DecryptEpisodeBody(context.Background(), k, env, "tenant-bob", ep, schema); !errors.Is(err, ErrAADMismatch) {
		t.Fatalf("want ErrAADMismatch, got %v", err)
	}
}

func TestTamper_AADMismatchSchemaLabel(t *testing.T) {
	k := newFakeKMS()
	tenant, ep, schema := ctxArgs()
	env, _ := EncryptEpisodeBody(context.Background(), k, []byte("x"), tenant, ep, schema)
	if _, err := DecryptEpisodeBody(context.Background(), k, env, tenant, ep, "medication"); !errors.Is(err, ErrAADMismatch) {
		t.Fatalf("want ErrAADMismatch, got %v", err)
	}
}

func TestTamper_CiphertextFlipFailsGCM(t *testing.T) {
	k := newFakeKMS()
	tenant, ep, schema := ctxArgs()
	env, _ := EncryptEpisodeBody(context.Background(), k, []byte("real plaintext goes here"), tenant, ep, schema)

	raw, err := base64.RawURLEncoding.DecodeString(env.Ciphertext)
	if err != nil {
		t.Fatalf("decode ciphertext: %v", err)
	}
	raw[0] ^= 0x01
	env.Ciphertext = base64.RawURLEncoding.EncodeToString(raw)

	if _, err := DecryptEpisodeBody(context.Background(), k, env, tenant, ep, schema); err == nil {
		t.Fatal("expected GCM authentication failure on tampered ciphertext")
	}
}

func TestSchemaGuard_UnknownSchema(t *testing.T) {
	k := newFakeKMS()
	tenant, ep, schema := ctxArgs()
	env, _ := EncryptEpisodeBody(context.Background(), k, []byte("x"), tenant, ep, schema)
	env.Schema = "omur.encrypted_body.v999"
	if _, err := DecryptEpisodeBody(context.Background(), k, env, tenant, ep, schema); !errors.Is(err, ErrUnsupportedSchema) {
		t.Fatalf("want ErrUnsupportedSchema, got %v", err)
	}
}

func TestSchemaGuard_UnknownAlg(t *testing.T) {
	k := newFakeKMS()
	tenant, ep, schema := ctxArgs()
	env, _ := EncryptEpisodeBody(context.Background(), k, []byte("x"), tenant, ep, schema)
	env.Alg = "ChaCha20-Poly1305"
	if _, err := DecryptEpisodeBody(context.Background(), k, env, tenant, ep, schema); !errors.Is(err, ErrUnsupportedSchema) {
		t.Fatalf("want ErrUnsupportedSchema, got %v", err)
	}
}

func TestNoPlaintextLeak_InMetadata(t *testing.T) {
	k := newFakeKMS()
	tenant, ep, schema := ctxArgs()
	plaintext := []byte("the patient reports left-sided chest pain")

	env, _ := EncryptEpisodeBody(context.Background(), k, plaintext, tenant, ep, schema)
	for label, field := range map[string]string{
		"schema":      env.Schema,
		"key_id":      env.KeyID,
		"nonce":       env.Nonce,
		"aad":         env.AAD,
		"alg":         env.Alg,
		"wrapped_dek": env.WrappedDEK,
	} {
		if strings.Contains(field, "chest pain") || strings.Contains(field, "patient reports") {
			t.Fatalf("plaintext leaked into envelope.%s = %q", label, field)
		}
	}
}
