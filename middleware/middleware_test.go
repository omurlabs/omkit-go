package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
}

func TestBearerAuth_AcceptsBearerToken(t *testing.T) {
	h := BearerAuth("secret", okHandler())
	req := httptest.NewRequest("GET", "/api/x", nil)
	req.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("valid bearer: got %d, want 200", w.Code)
	}
}

func TestBearerAuth_AcceptsServiceToken(t *testing.T) {
	h := BearerAuth("secret", okHandler())
	req := httptest.NewRequest("GET", "/api/x", nil)
	req.Header.Set("X-Service-Token", "secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("valid service token: got %d, want 200", w.Code)
	}
}

func TestBearerAuth_RejectsAuthentikUIDAlone(t *testing.T) {
	// Regression guard: the old BearerAuth let any non-empty X-Authentik-Uid
	// pass. A peer on the backend network could forge it to bypass auth. Now
	// Caddy must inject X-Service-Token alongside forward_auth.
	h := BearerAuth("secret", okHandler())
	req := httptest.NewRequest("GET", "/api/x", nil)
	req.Header.Set("X-Authentik-Uid", "attacker-forged-uid")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("X-Authentik-Uid alone: got %d, want 401", w.Code)
	}
}

func TestBearerAuth_RejectsWrongToken(t *testing.T) {
	h := BearerAuth("secret", okHandler())
	req := httptest.NewRequest("GET", "/api/x", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token: got %d, want 401", w.Code)
	}
}

func TestBearerAuth_SkipsHealthEndpoints(t *testing.T) {
	h := BearerAuth("secret", okHandler())
	for _, path := range []string{"/health", "/healthz", "/ready"} {
		req := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("%s: got %d, want 200 (health bypass)", path, w.Code)
		}
	}
}

func TestBearerAuth_EmptyTokenIsNoop(t *testing.T) {
	h := BearerAuth("", okHandler())
	req := httptest.NewRequest("GET", "/api/x", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("empty token disables BearerAuth: got %d, want 200", w.Code)
	}
}

func TestMustBearerAuth_EmptyTokenPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("MustBearerAuth with empty token: expected panic, got nil")
		}
	}()
	MustBearerAuth("", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
}

func TestMustBearerAuth_SetTokenDelegates(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
	})
	h := MustBearerAuth("secret", next)

	req := httptest.NewRequest("GET", "/protected", nil)
	req.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
}
