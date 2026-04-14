// Package settings reads app_settings from Postgres, caches to disk, and
// subscribes to Valkey for live updates.
package settings

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/omurlabs/omur-core/packages/omur-go-sdk/valkeysub"
)

// Manager holds a local in-memory copy of non-secret app settings.
type Manager struct {
	mu        sync.RWMutex
	cache     map[string]string
	listeners map[string][]func(string, string)
	cachePath string
	db        *pgxpool.Pool
	valkey    *valkeysub.Subscriber
	tenantID  string
	service   string
}

// Option configures a Manager.
type Option func(*Manager)

// WithValkey subscribes to live settings updates from the given Valkey address.
func WithValkey(addr string) Option {
	return func(m *Manager) {
		m.valkey = valkeysub.New(addr)
	}
}

// WithCache sets the disk-cache file path for fallback persistence.
func WithCache(path string) Option {
	return func(m *Manager) {
		m.cachePath = path
	}
}

// WithService sets an informational service name (unused at runtime, reserved).
func WithService(name string) Option {
	return func(m *Manager) {
		m.service = name
	}
}

// WithTenantID sets the tenant ID used to build the Valkey channel name.
func WithTenantID(id string) Option {
	return func(m *Manager) {
		m.tenantID = id
	}
}

// New creates a Manager. db may be nil (tests skip DB operations).
func New(db *pgxpool.Pool, opts ...Option) *Manager {
	m := &Manager{
		cache:     make(map[string]string),
		listeners: make(map[string][]func(string, string)),
		db:        db,
	}
	for _, o := range opts {
		o(m)
	}
	return m
}

// Start loads settings from DB (falling back to disk cache) then starts the
// Valkey subscriber in a background goroutine.
func (m *Manager) Start(ctx context.Context) error {
	if err := m.loadFromDB(ctx); err != nil {
		// Fallback to disk cache on DB failure.
		_ = m.LoadCache()
	}

	if m.valkey != nil {
		channel := fmt.Sprintf("omur:settings:%s", m.tenantID)
		go m.valkey.Subscribe(ctx, channel, m.handleMessage)
	}
	return nil
}

// loadFromDB queries app_settings and stores non-secret values.
func (m *Manager) loadFromDB(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("no db pool")
	}
	rows, err := m.db.Query(ctx,
		"SELECT key, value_json, is_secret FROM app_settings")
	if err != nil {
		return err
	}
	defer rows.Close()

	m.mu.Lock()
	defer m.mu.Unlock()
	for rows.Next() {
		var key, valueJSON string
		var isSecret bool
		if err := rows.Scan(&key, &valueJSON, &isSecret); err != nil {
			continue
		}
		if isSecret {
			continue
		}
		// Unwrap JSON string value if quoted, else store raw.
		var s string
		if json.Unmarshal([]byte(valueJSON), &s) == nil {
			m.cache[key] = s
		} else {
			m.cache[key] = valueJSON
		}
	}
	return rows.Err()
}

// handleMessage is called by the Valkey subscriber for each pub/sub payload.
func (m *Manager) handleMessage(payload string) {
	var msg struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal([]byte(payload), &msg); err != nil {
		return
	}
	m.ApplyChange(msg.Key, msg.Value)
}

// ApplyChange updates the in-memory cache and fires registered listeners.
// Exported so tests can drive it directly; production code calls it via
// handleMessage.
func (m *Manager) ApplyChange(key, value string) {
	m.mu.Lock()
	m.cache[key] = value
	fns := make([]func(string, string), len(m.listeners[key]))
	copy(fns, m.listeners[key])
	m.mu.Unlock()

	for _, fn := range fns {
		fn(key, value)
	}
}

// Get returns the cached value for key, or "" if absent.
func (m *Manager) Get(key string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cache[key]
}

// GetAll returns a deep copy of the current cache.
func (m *Manager) GetAll() map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]string, len(m.cache))
	for k, v := range m.cache {
		out[k] = v
	}
	return out
}

// OnChange registers fn to be called whenever key changes.
func (m *Manager) OnChange(key string, fn func(key, value string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listeners[key] = append(m.listeners[key], fn)
}

// WriteCache atomically writes the current in-memory cache to cachePath.
func (m *Manager) WriteCache() error {
	if m.cachePath == "" {
		return nil
	}
	m.mu.RLock()
	data, err := json.Marshal(m.cache)
	m.mu.RUnlock()
	if err != nil {
		return err
	}

	tmp := m.cachePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, m.cachePath)
}

// LoadCache reads and merges the disk cache into the in-memory cache.
func (m *Manager) LoadCache() error {
	if m.cachePath == "" {
		return nil
	}
	data, err := os.ReadFile(m.cachePath)
	if err != nil {
		return err
	}
	var loaded map[string]string
	if err := json.Unmarshal(data, &loaded); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, v := range loaded {
		m.cache[k] = v
	}
	return nil
}
