package kms

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Test server helpers
// ---------------------------------------------------------------------------

// loginOK returns a minimal AppRole login response.
func loginBody(token string, duration int) []byte {
	b, _ := json.Marshal(map[string]any{
		"auth": map[string]any{
			"client_token":   token,
			"lease_duration": duration,
		},
	})
	return b
}

// encryptResp returns a Transit encrypt response.
func encryptResp(ciphertext string) []byte {
	b, _ := json.Marshal(map[string]any{
		"data": map[string]any{
			"ciphertext":  ciphertext,
			"key_version": 1,
		},
	})
	return b
}

// decryptResp returns a Transit decrypt response with base64-encoded plaintext.
func decryptResp(plaintext []byte) []byte {
	b, _ := json.Marshal(map[string]any{
		"data": map[string]any{
			"plaintext": base64.StdEncoding.EncodeToString(plaintext),
		},
	})
	return b
}

// keysResp returns a transit/keys response with latest_version.
func keysResp(version int) []byte {
	b, _ := json.Marshal(map[string]any{
		"data": map[string]any{
			"latest_version": version,
		},
	})
	return b
}

// kvPutResp returns a kv-v2 create/update response.
func kvPutResp() []byte {
	b, _ := json.Marshal(map[string]any{"data": map[string]any{"version": 1}})
	return b
}

// newBaoKMS creates an OpenBaoKMS wired against a test server, bypassing env vars.
func newBaoKMSWithServer(srv *httptest.Server) *OpenBaoKMS {
	return &OpenBaoKMS{
		addr:     srv.URL,
		roleID:   "test-role-id",
		secretID: "test-secret-id",
		keyName:  "omur-user-content",
		client:   &http.Client{Timeout: 5 * time.Second},
	}
}

// ---------------------------------------------------------------------------
// Test 1: WrapDEK round-trip (bytes preserved)
// ---------------------------------------------------------------------------

func TestOpenBaoKMS_WrapDEKRoundTrip(t *testing.T) {
	originalDEK := []byte("this-is-a-32-byte-test-dek-value")
	ciphertext := "vault:v1:dGVzdC1jaXBoZXJ0ZXh0"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/auth/approle/login":
			w.Write(loginBody("test-token", 3600))
		case "/v1/transit/encrypt/omur-user-content":
			w.Write(encryptResp(ciphertext))
		case "/v1/transit/decrypt/omur-user-content":
			w.Write(decryptResp(originalDEK))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	k := newBaoKMSWithServer(srv)
	ctx := context.Background()

	wrapped, version, err := k.WrapDEK(ctx, "user-1", "content", originalDEK, []byte("doc-123"))
	if err != nil {
		t.Fatalf("WrapDEK failed: %v", err)
	}
	if string(wrapped) != ciphertext {
		t.Errorf("wrapped = %q, want %q", wrapped, ciphertext)
	}
	if version == "" {
		t.Error("version should not be empty")
	}

	unwrapped, err := k.UnwrapDEK(ctx, "user-1", "content", wrapped, []byte("doc-123"))
	if err != nil {
		t.Fatalf("UnwrapDEK failed: %v", err)
	}
	if string(unwrapped) != string(originalDEK) {
		t.Errorf("unwrapped = %q, want %q", unwrapped, originalDEK)
	}
}

// ---------------------------------------------------------------------------
// Test 2: AAD tamper → ErrKMSAuth (mocked 400)
// ---------------------------------------------------------------------------

func TestOpenBaoKMS_AADTamperReturnsAuthError(t *testing.T) {
	ciphertext := "vault:v1:dGVzdC1jaXBoZXJ0ZXh0"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/auth/approle/login":
			w.Write(loginBody("test-token", 3600))
		case "/v1/transit/encrypt/omur-user-content":
			w.Write(encryptResp(ciphertext))
		case "/v1/transit/decrypt/omur-user-content":
			// Simulate Transit rejecting a tampered context (wrong AAD).
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"errors":["invalid ciphertext: aad mismatch"]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	k := newBaoKMSWithServer(srv)
	ctx := context.Background()

	// Wrap with correct AAD.
	wrapped, _, err := k.WrapDEK(ctx, "user-1", "content", []byte("dek"), []byte("doc-123"))
	if err != nil {
		t.Fatalf("WrapDEK failed: %v", err)
	}

	// Unwrap with tampered AAD — should return ErrKMSAuth.
	_, err = k.UnwrapDEK(ctx, "user-1", "content", wrapped, []byte("TAMPERED"))
	if err == nil {
		t.Fatal("UnwrapDEK with tampered AAD should fail")
	}
	if _, ok := err.(*ErrKMSAuth); !ok {
		t.Errorf("expected *ErrKMSAuth, got %T: %v", err, err)
	}
}

// ---------------------------------------------------------------------------
// Test 3: 5xx response → ErrKMSUnavailable
// ---------------------------------------------------------------------------

func TestOpenBaoKMS_5xxReturnsUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/auth/approle/login":
			w.Write(loginBody("test-token", 3600))
		default:
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"errors":["internal server error"]}`))
		}
	}))
	defer srv.Close()

	k := newBaoKMSWithServer(srv)
	ctx := context.Background()

	_, _, err := k.WrapDEK(ctx, "user-1", "content", []byte("dek"), nil)
	if err == nil {
		t.Fatal("expected error on 500, got nil")
	}
	if _, ok := err.(*ErrKMSUnavailable); !ok {
		t.Errorf("expected *ErrKMSUnavailable, got %T: %v", err, err)
	}
}

// ---------------------------------------------------------------------------
// Test 4: 403 response → ErrKMSAuth
// ---------------------------------------------------------------------------

func TestOpenBaoKMS_403ReturnsAuthError(t *testing.T) {
	// Login succeeds; encrypt returns 403.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/auth/approle/login":
			w.Write(loginBody("test-token", 3600))
		default:
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"errors":["permission denied"]}`))
		}
	}))
	defer srv.Close()

	k := newBaoKMSWithServer(srv)
	ctx := context.Background()

	_, _, err := k.WrapDEK(ctx, "user-1", "content", []byte("dek"), nil)
	if err == nil {
		t.Fatal("expected error on 403, got nil")
	}
	if _, ok := err.(*ErrKMSAuth); !ok {
		t.Errorf("expected *ErrKMSAuth, got %T: %v", err, err)
	}
}

// ---------------------------------------------------------------------------
// Test 5: Token refresh on near-expiry (token set as already expired)
// ---------------------------------------------------------------------------

func TestOpenBaoKMS_TokenRefreshOnNearExpiry(t *testing.T) {
	loginCount := int32(0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/auth/approle/login":
			atomic.AddInt32(&loginCount, 1)
			w.Write(loginBody(fmt.Sprintf("token-%d", atomic.LoadInt32(&loginCount)), 3600))
		case "/v1/transit/encrypt/omur-user-content":
			w.Write(encryptResp("vault:v1:abc"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	k := newBaoKMSWithServer(srv)
	// Pre-set an expired token so the first call must refresh.
	k.token = "stale-token"
	k.tokenExpiry = time.Now().Add(-1 * time.Second)

	ctx := context.Background()
	_, _, err := k.WrapDEK(ctx, "user-1", "content", []byte("dek"), nil)
	if err != nil {
		t.Fatalf("WrapDEK after expired token: %v", err)
	}
	if count := atomic.LoadInt32(&loginCount); count < 1 {
		t.Errorf("expected at least 1 login call, got %d", count)
	}
}

// ---------------------------------------------------------------------------
// Test 6: CurrentVersion returns latest_version
// ---------------------------------------------------------------------------

func TestOpenBaoKMS_CurrentVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/auth/approle/login":
			w.Write(loginBody("test-token", 3600))
		case "/v1/transit/keys/omur-user-content":
			w.Write(keysResp(3))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	k := newBaoKMSWithServer(srv)
	ctx := context.Background()

	v, err := k.CurrentVersion(ctx, "omur-user-content")
	if err != nil {
		t.Fatalf("CurrentVersion failed: %v", err)
	}
	if v != "v3" {
		t.Errorf("CurrentVersion = %q, want %q", v, "v3")
	}
}

// ---------------------------------------------------------------------------
// Test 7: DeleteUserKeys writes tombstone
// ---------------------------------------------------------------------------

func TestOpenBaoKMS_DeleteUserKeysWritesTombstone(t *testing.T) {
	tombstonePath := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/auth/approle/login":
			w.Write(loginBody("test-token", 3600))
		default:
			if r.Method == http.MethodPost {
				tombstonePath = r.URL.Path
				w.Write(kvPutResp())
			} else {
				http.NotFound(w, r)
			}
		}
	}))
	defer srv.Close()

	k := newBaoKMSWithServer(srv)
	ctx := context.Background()

	err := k.DeleteUserKeys(ctx, "user-abc-123")
	if err != nil {
		t.Fatalf("DeleteUserKeys failed: %v", err)
	}
	if tombstonePath != "/v1/secret/data/omur/shredded/user-abc-123" {
		t.Errorf("tombstone path = %q, want %q",
			tombstonePath, "/v1/secret/data/omur/shredded/user-abc-123")
	}
}
