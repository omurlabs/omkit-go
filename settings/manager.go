// Package settings reads app_settings from Postgres, caches to disk, and
// keeps the cache up-to-date via either a Postgres polling loop (default) or
// a Valkey pub/sub subscriber (opt-in via OMUR_SETTINGS_BACKEND=redis).
package settings

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

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

	pollInterval time.Duration
	pollLastSeen time.Time
	backend      Backend
	stop         chan struct{}
	stopped      bool
}

// Backend selects the live-update strategy used by Manager.Start.
type Backend string

const (
	// BackendPostgres polls the app_settings table for new/updated rows.
	BackendPostgres Backend = "postgres"
	// BackendRedis subscribes to omur:settings:<tenant> on Valkey.
	BackendRedis Backend = "redis"
)

// Config configures a Manager via NewFromConfig. Prefer this over New for
// new code because it selects backend from OMUR_SETTINGS_BACKEND and supports
// polling.
type Config struct {
	Pool         *pgxpool.Pool
	TenantID     string
	Service      string
	CachePath    string
	ValkeyAddr   string        // only used when backend=redis
	PollInterval time.Duration // default 5s; only used when backend=postgres
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

// WithPollInterval overrides the default 5s Postgres poll interval.
func WithPollInterval(d time.Duration) Option {
	return func(m *Manager) {
		m.pollInterval = d
	}
}

// New creates a Manager. db may be nil (tests skip DB operations).
// For new code, prefer NewFromConfig which honours OMUR_SETTINGS_BACKEND.
func New(db *pgxpool.Pool, opts ...Option) *Manager {
	m := &Manager{
		cache:     make(map[string]string),
		listeners: make(map[string][]func(string, string)),
		db:        db,
		stop:      make(chan struct{}),
	}
	for _, o := range opts {
		o(m)
	}
	if m.backend == "" {
		// If WithValkey was supplied, preserve legacy redis-subscriber behaviour.
		if m.valkey != nil {
			m.backend = BackendRedis
		} else {
			m.backend = BackendPostgres
		}
	}
	if m.pollInterval == 0 {
		m.pollInterval = 5 * time.Second
	}
	return m
}

// NewFromConfig constructs a Manager from a Config struct, selecting the backend
// via OMUR_SETTINGS_BACKEND (default "postgres").
func NewFromConfig(cfg Config) *Manager {
	backend, err := backendFromEnv()
	if err != nil {
		// Invalid env falls back to postgres rather than panicking at construction.
		slog.Warn("settings: invalid OMUR_SETTINGS_BACKEND, defaulting to postgres", "err", err)
		backend = BackendPostgres
	}
	m := &Manager{
		cache:        make(map[string]string),
		listeners:    make(map[string][]func(string, string)),
		db:           cfg.Pool,
		tenantID:     cfg.TenantID,
		service:      cfg.Service,
		cachePath:    cfg.CachePath,
		pollInterval: cfg.PollInterval,
		backend:      backend,
		stop:         make(chan struct{}),
	}
	if m.pollInterval == 0 {
		m.pollInterval = 5 * time.Second
	}
	if backend == BackendRedis && cfg.ValkeyAddr != "" {
		m.valkey = valkeysub.New(cfg.ValkeyAddr)
	}
	return m
}

// backendFromEnv returns the configured backend, defaulting to postgres.
func backendFromEnv() (Backend, error) {
	v := os.Getenv("OMUR_SETTINGS_BACKEND")
	if v == "" {
		return BackendPostgres, nil
	}
	switch Backend(v) {
	case BackendPostgres, BackendRedis:
		return Backend(v), nil
	}
	return "", fmt.Errorf("unknown backend %q", v)
}

// Start loads settings from DB (falling back to disk cache) then starts the
// live-update mechanism for the configured backend.
func (m *Manager) Start(ctx context.Context) error {
	if err := m.loadFromDB(ctx); err != nil {
		_ = m.LoadCache()
	}
	switch m.backend {
	case BackendRedis:
		if m.valkey != nil {
			channel := fmt.Sprintf("omur:settings:%s", m.tenantID)
			go m.valkey.Subscribe(ctx, channel, m.handleMessage)
		}
	case BackendPostgres:
		if m.db != nil {
			go m.pollLoop(ctx)
		}
	}
	return nil
}

// Stop halts background workers started by Start. Safe to call multiple times.
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopped {
		return
	}
	m.stopped = true
	close(m.stop)
}

// pollLoop periodically re-reads rows from app_settings with updated_at > last_seen.
func (m *Manager) pollLoop(ctx context.Context) {
	ticker := time.NewTicker(m.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stop:
			return
		case <-ticker.C:
			if err := m.pollOnce(ctx); err != nil {
				slog.Warn("settings: poll failed", "err", err)
			}
		}
	}
}

func (m *Manager) pollOnce(ctx context.Context) error {
	since := m.pollLastSeen
	rows, err := m.db.Query(ctx,
		`SELECT key, value_json, is_secret, updated_at
		 FROM app_settings
		 WHERE updated_at > $1
		 ORDER BY updated_at ASC`, since)
	if err != nil {
		return err
	}
	defer rows.Close()
	var latest time.Time
	for rows.Next() {
		var key, valueJSON string
		var isSecret bool
		var updatedAt time.Time
		if err := rows.Scan(&key, &valueJSON, &isSecret, &updatedAt); err != nil {
			continue
		}
		if updatedAt.After(latest) {
			latest = updatedAt
		}
		if isSecret {
			continue
		}
		var s string
		if json.Unmarshal([]byte(valueJSON), &s) == nil {
			m.ApplyChange(key, s)
		} else {
			m.ApplyChange(key, valueJSON)
		}
	}
	if !latest.IsZero() {
		m.pollLastSeen = latest
	}
	return rows.Err()
}

// loadFromDB queries app_settings and stores non-secret values.
func (m *Manager) loadFromDB(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("no db pool")
	}
	rows, err := m.db.Query(ctx,
		"SELECT key, value_json, is_secret, updated_at FROM app_settings")
	if err != nil {
		return err
	}
	defer rows.Close()

	m.mu.Lock()
	defer m.mu.Unlock()
	for rows.Next() {
		var key, valueJSON string
		var isSecret bool
		var updatedAt time.Time
		if err := rows.Scan(&key, &valueJSON, &isSecret, &updatedAt); err != nil {
			continue
		}
		if updatedAt.After(m.pollLastSeen) {
			m.pollLastSeen = updatedAt
		}
		if isSecret {
			continue
		}
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
