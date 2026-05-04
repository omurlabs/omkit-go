// openbao.go — openbao module.
//
// exports: ErrKMSUnavailable | Error | ErrKMSAuth | OpenBaoKMS | NewOpenBaoKMS | Wrap | Unwrap | CurrentVersion | WrapDEK | UnwrapDEK | DeleteUserKeys
// rules:   none
// agent:   codedna-cli (no-llm) | codedna-cli | 2026-04-30 | codedna-cli | initial CodeDNA annotation pass
// message: 

package kms

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"
)

// ErrKMSUnavailable is returned when the KMS backend responds with a 5xx
// status code or is unreachable (network error, timeout). Callers should treat
// this as a transient error and may retry with backoff.
type ErrKMSUnavailable struct {
	Status  int
	Message string
}

func (e *ErrKMSUnavailable) Error() string {
	return fmt.Sprintf("kms: unavailable (status %d): %s", e.Status, e.Message)
}

// ErrKMSAuth is returned when the KMS backend responds with a 4xx status code
// (401, 403, 400 with an invalid ciphertext context). Callers must treat this
// as a permanent error — retrying will not help.
type ErrKMSAuth struct {
	Status  int
	Message string
}

func (e *ErrKMSAuth) Error() string {
	return fmt.Sprintf("kms: auth error (status %d): %s", e.Status, e.Message)
}

// OpenBaoKMS implements the KMS interface using OpenBao (or Vault) Transit
// secrets engine with derived keys. A single master Transit key
// (default: "omur-user-content") is configured with derived=true so that
// per-(userID, purpose) separation is enforced via the context field without
// requiring one Transit key per user.
//
// Authentication: AppRole (role_id + secret_id → client_token). The token is
// cached in memory and refreshed when it is near expiry or when a 403 is
// received.
//
// DeleteUserKeys: Transit derived-key mode does not support per-context
// revocation — the context is a derivation parameter, not a stored key.
// Per-user crypto-shred therefore writes a tombstone to the kv-v2 path
// omur/shredded/{userID} so future operations can detect and refuse access.
// True per-user key material deletion requires one Transit key per user
// (higher operational cost) and is deferred to Stage D if the stronger
// guarantee is required.
//
// Env vars (read in NewOpenBaoKMS):
//   - OMUR_KMS_OPENBAO_ADDR      — e.g. http://openbao:8200 (required)
//   - OMUR_KMS_OPENBAO_ROLE_ID   — AppRole role_id (required)
//   - OMUR_KMS_OPENBAO_SECRET_ID — AppRole secret_id (required)
//   - OMUR_KMS_OPENBAO_KEY_NAME  — Transit key name (default: omur-user-content)
type OpenBaoKMS struct {
	addr    string
	roleID  string
	secretID string
	keyName  string
	client   *http.Client

	mu          sync.Mutex
	token       string
	tokenExpiry time.Time
}

// NewOpenBaoKMS constructs an OpenBaoKMS adapter from environment variables.
// Returns an error when required env vars are missing.
func NewOpenBaoKMS() (*OpenBaoKMS, error) {
	addr := os.Getenv("OMUR_KMS_OPENBAO_ADDR")
	if addr == "" {
		return nil, fmt.Errorf("openbao kms: OMUR_KMS_OPENBAO_ADDR must be set")
	}
	roleID := os.Getenv("OMUR_KMS_OPENBAO_ROLE_ID")
	if roleID == "" {
		return nil, fmt.Errorf("openbao kms: OMUR_KMS_OPENBAO_ROLE_ID must be set")
	}
	secretID := os.Getenv("OMUR_KMS_OPENBAO_SECRET_ID")
	if secretID == "" {
		return nil, fmt.Errorf("openbao kms: OMUR_KMS_OPENBAO_SECRET_ID must be set")
	}
	keyName := os.Getenv("OMUR_KMS_OPENBAO_KEY_NAME")
	if keyName == "" {
		keyName = "omur-user-content"
	}
	return &OpenBaoKMS{
		addr:     addr,
		roleID:   roleID,
		secretID: secretID,
		keyName:  keyName,
		client:   &http.Client{Timeout: 10 * time.Second},
	}, nil
}

// --- Token management -------------------------------------------------------

// token returns the cached token, refreshing it when expired or near expiry
// (within 30 s of expiry). Thread-safe.
func (o *OpenBaoKMS) getToken(ctx context.Context) (string, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.token != "" && time.Until(o.tokenExpiry) > 30*time.Second {
		return o.token, nil
	}
	return o.login(ctx)
}

// login performs AppRole authentication and caches the resulting token.
// Caller must hold o.mu.
func (o *OpenBaoKMS) login(ctx context.Context) (string, error) {
	body, _ := json.Marshal(map[string]string{
		"role_id":   o.roleID,
		"secret_id": o.secretID,
	})
	resp, err := o.doRaw(ctx, http.MethodPost, "/v1/auth/approle/login", "", body)
	if err != nil {
		return "", err
	}

	var out struct {
		Auth struct {
			ClientToken   string `json:"client_token"`
			LeaseDuration int    `json:"lease_duration"`
		} `json:"auth"`
	}
	if err := json.Unmarshal(resp, &out); err != nil || out.Auth.ClientToken == "" {
		return "", fmt.Errorf("openbao kms: unexpected login response: %s", truncate(resp, 200))
	}
	o.token = out.Auth.ClientToken
	lease := time.Duration(out.Auth.LeaseDuration) * time.Second
	if lease <= 0 {
		lease = time.Hour
	}
	o.tokenExpiry = time.Now().Add(lease)
	return o.token, nil
}

// --- HTTP helpers -----------------------------------------------------------

// doRaw performs a raw HTTP call against the OpenBao API. token may be empty
// (for /v1/auth/approle/login). Returns response body on success.
// Maps 5xx + network errors to ErrKMSUnavailable; 4xx to ErrKMSAuth.
func (o *OpenBaoKMS) doRaw(ctx context.Context, method, path, token string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, o.addr+path, bytes.NewReader(body))
	if err != nil {
		return nil, &ErrKMSUnavailable{Status: 0, Message: err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("X-Vault-Token", token)
	}
	resp, err := o.client.Do(req)
	if err != nil {
		return nil, &ErrKMSUnavailable{Status: 0, Message: err.Error()}
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode >= 500 {
		return nil, &ErrKMSUnavailable{Status: resp.StatusCode, Message: string(truncate(respBody, 200))}
	}
	if resp.StatusCode >= 400 {
		return nil, &ErrKMSAuth{Status: resp.StatusCode, Message: string(truncate(respBody, 200))}
	}
	return respBody, nil
}

// do performs an authenticated HTTP call, refreshing the token on 403.
func (o *OpenBaoKMS) do(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	tok, err := o.getToken(ctx)
	if err != nil {
		return nil, err
	}
	respBody, err := o.doRaw(ctx, method, path, tok, body)
	if err != nil {
		// Force token refresh on auth error.
		if authErr, ok := err.(*ErrKMSAuth); ok && authErr.Status == 403 {
			o.mu.Lock()
			o.token = ""
			o.mu.Unlock()
			tok2, loginErr := o.getToken(ctx)
			if loginErr != nil {
				return nil, loginErr
			}
			return o.doRaw(ctx, method, path, tok2, body)
		}
		return nil, err
	}
	return respBody, nil
}

// --- derivedContext builds the Transit context field ------------------------

// derivedContext returns the base64-encoded context value that OpenBao Transit
// uses as the per-key derivation input. It encodes userID + ":" + purpose +
// ":" + base64(aad) so each (userID, purpose, aad) triple produces a unique
// derived key, matching the semantics of LocalDevKMS.bindAAD.
func derivedContext(userID, purpose string, aad []byte) string {
	inner := userID + ":" + purpose + ":" + base64.StdEncoding.EncodeToString(aad)
	return base64.StdEncoding.EncodeToString([]byte(inner))
}

// --- KMS interface implementation -------------------------------------------

// Wrap encrypts plaintext using the named Transit key without derivation
// (used for tenant-level / system api_key wrapping).
func (o *OpenBaoKMS) Wrap(ctx context.Context, keyID string, plaintext, aad []byte) ([]byte, error) {
	body, _ := json.Marshal(map[string]string{
		"plaintext": base64.StdEncoding.EncodeToString(plaintext),
		"context":   base64.StdEncoding.EncodeToString(aad),
	})
	resp, err := o.do(ctx, http.MethodPost, "/v1/transit/encrypt/"+keyID, body)
	if err != nil {
		return nil, err
	}
	var out struct {
		Data struct {
			Ciphertext string `json:"ciphertext"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp, &out); err != nil || out.Data.Ciphertext == "" {
		return nil, &ErrKMSUnavailable{Status: 0, Message: "unexpected encrypt response"}
	}
	return []byte(out.Data.Ciphertext), nil
}

// Unwrap decrypts a Transit ciphertext.
func (o *OpenBaoKMS) Unwrap(ctx context.Context, keyID string, blob, aad []byte) ([]byte, error) {
	body, _ := json.Marshal(map[string]string{
		"ciphertext": string(blob),
		"context":    base64.StdEncoding.EncodeToString(aad),
	})
	resp, err := o.do(ctx, http.MethodPost, "/v1/transit/decrypt/"+keyID, body)
	if err != nil {
		return nil, err
	}
	var out struct {
		Data struct {
			Plaintext string `json:"plaintext"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp, &out); err != nil || out.Data.Plaintext == "" {
		return nil, &ErrKMSUnavailable{Status: 0, Message: "unexpected decrypt response"}
	}
	decoded, err := base64.StdEncoding.DecodeString(out.Data.Plaintext)
	if err != nil {
		return nil, fmt.Errorf("openbao kms: base64 decode plaintext: %w", err)
	}
	return decoded, nil
}

// CurrentVersion returns the latest key version for keyID.
func (o *OpenBaoKMS) CurrentVersion(ctx context.Context, keyID string) (string, error) {
	resp, err := o.do(ctx, http.MethodGet, "/v1/transit/keys/"+keyID, nil)
	if err != nil {
		return "", err
	}
	var out struct {
		Data struct {
			LatestVersion int `json:"latest_version"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp, &out); err != nil {
		return "", &ErrKMSUnavailable{Status: 0, Message: "unexpected keys response"}
	}
	return fmt.Sprintf("v%d", out.Data.LatestVersion), nil
}

// WrapDEK encrypts plainDEK using the master Transit key with per-user derived
// context. The context encodes userID + purpose + aad so cross-user and
// cross-purpose unwrap attempts fail at the Transit layer.
func (o *OpenBaoKMS) WrapDEK(ctx context.Context, userID, purpose string, plainDEK, aad []byte) (wrapped []byte, version string, err error) {
	ctx64 := derivedContext(userID, purpose, aad)
	body, _ := json.Marshal(map[string]string{
		"plaintext": base64.StdEncoding.EncodeToString(plainDEK),
		"context":   ctx64,
	})
	resp, err := o.do(ctx, http.MethodPost, "/v1/transit/encrypt/"+o.keyName, body)
	if err != nil {
		return nil, "", err
	}
	var out struct {
		Data struct {
			Ciphertext string `json:"ciphertext"`
			KeyVersion int    `json:"key_version"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp, &out); err != nil || out.Data.Ciphertext == "" {
		return nil, "", &ErrKMSUnavailable{Status: 0, Message: "unexpected wrap dek response"}
	}
	return []byte(out.Data.Ciphertext), fmt.Sprintf("v%d", out.Data.KeyVersion), nil
}

// UnwrapDEK decrypts a Transit ciphertext using the same derived context. Any
// mismatch in (userID, purpose, aad) causes Transit to return a 400 error
// which maps to ErrKMSAuth.
func (o *OpenBaoKMS) UnwrapDEK(ctx context.Context, userID, purpose string, wrapped, aad []byte) (plainDEK []byte, err error) {
	ctx64 := derivedContext(userID, purpose, aad)
	body, _ := json.Marshal(map[string]string{
		"ciphertext": string(wrapped),
		"context":    ctx64,
	})
	resp, err := o.do(ctx, http.MethodPost, "/v1/transit/decrypt/"+o.keyName, body)
	if err != nil {
		return nil, err
	}
	var out struct {
		Data struct {
			Plaintext string `json:"plaintext"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp, &out); err != nil || out.Data.Plaintext == "" {
		return nil, &ErrKMSUnavailable{Status: 0, Message: "unexpected unwrap dek response"}
	}
	decoded, err := base64.StdEncoding.DecodeString(out.Data.Plaintext)
	if err != nil {
		return nil, fmt.Errorf("openbao kms: base64 decode dek: %w", err)
	}
	return decoded, nil
}

// DeleteUserKeys writes a tombstone to kv-v2 path omur/shredded/{userID}.
// This is a best-effort signal: Transit derived-key mode does NOT support
// per-context key revocation. True crypto-shred (GDPR Art. 17) via this
// adapter requires per-user Transit keys (deferred to Stage D). The tombstone
// allows future WrapDEK/UnwrapDEK callers to detect and refuse access by
// checking the tombstone before each operation (application-level check).
//
// If the kv-v2 secret engine is not enabled at path "secret", this call
// returns an error. The bootstrap script (C2) enables it.
func (o *OpenBaoKMS) DeleteUserKeys(ctx context.Context, userID string) error {
	body, _ := json.Marshal(map[string]any{
		"data": map[string]string{
			"shredded_at": time.Now().UTC().Format(time.RFC3339),
			"reason":      "user_erasure_request",
		},
	})
	_, err := o.do(ctx, http.MethodPost, "/v1/secret/data/omur/shredded/"+userID, body)
	return err
}

// --- helpers ----------------------------------------------------------------

func truncate(b []byte, n int) []byte {
	if len(b) <= n {
		return b
	}
	return b[:n]
}
