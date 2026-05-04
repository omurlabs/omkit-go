// postgres.go — postgres module.
//
// exports: PostgresStore | NewStoreWithRefresher | NewPostgresStore | Get | Refresh | Invalidate | AllFlags | LoadForService | ParseFromJSON
// rules:   none
// agent:   codedna-cli (no-llm) | codedna-cli | 2026-04-30 | codedna-cli | initial CodeDNA annotation pass
// message: 

package featureflags

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/singleflight"

	"github.com/omurlabs/omur-core/packages/omur-go-sdk/auth"
)

// refresherFunc fetches the current flag map from a backing store.
type refresherFunc func(ctx context.Context) (map[string]Flag, error)

// PostgresStore is a TTL-cached flag store backed by app_settings rows where
// key LIKE 'flag.%'. Concurrency model:
//
//   - Current map held as atomic.Pointer[map[string]Flag]; reads are lock-free.
//   - Refresh triggers collapsed by singleflight (one DB roundtrip per TTL window).
//   - On refresh error, the cache is NOT zeroed — stale served, logger warns.
//   - Invalidate(key) publishes a new map with the key removed. LOCAL to this
//     process; other Spine replicas pick up changes within TTL (typically 30s).
//
// The TTL bounds FLAG staleness, not USER-ROLE staleness — roles come from
// forward-auth headers via auth.RolesFromContext every request.
type PostgresStore struct {
	refresh       refresherFunc
	ttl           time.Duration
	ptr           atomic.Pointer[map[string]Flag]
	lastRefreshed atomic.Int64 // unix nanos
	group         singleflight.Group
	mu            sync.Mutex // serializes Invalidate writes
}

// NewStoreWithRefresher is the test-friendly constructor.
func NewStoreWithRefresher(refresh refresherFunc, ttl time.Duration) *PostgresStore {
	s := &PostgresStore{refresh: refresh, ttl: ttl}
	empty := map[string]Flag{}
	s.ptr.Store(&empty)
	return s
}

// NewPostgresStore returns a PostgresStore that refreshes from app_settings.
func NewPostgresStore(pool *pgxpool.Pool, ttl time.Duration) *PostgresStore {
	return NewStoreWithRefresher(func(ctx context.Context) (map[string]Flag, error) {
		return loadFromPool(ctx, pool)
	}, ttl)
}

// Get returns the flag for key, or ok=false if unknown. If the cache is older
// than TTL, a background refresh is triggered via singleflight and the
// CURRENT (stale) snapshot is returned to the caller with no blocking.
func (s *PostgresStore) Get(key string) (Flag, bool) {
	if s.stale() {
		go s.tryRefresh()
	}
	m := s.ptr.Load()
	if m == nil {
		return Flag{}, false
	}
	f, ok := (*m)[key]
	return f, ok
}

// Refresh synchronously reloads the flag map. Any refresh error leaves the
// existing snapshot in place and is logged.
func (s *PostgresStore) Refresh(ctx context.Context) error {
	_, err, _ := s.group.Do("refresh", func() (any, error) {
		m, err := s.refresh(ctx)
		if err != nil {
			return nil, err
		}
		s.ptr.Store(&m)
		s.lastRefreshed.Store(time.Now().UnixNano())
		return nil, nil
	})
	if err != nil {
		slog.Warn("featureflags.refresh_failed", "err", err)
	}
	return err
}

// Invalidate deletes the entry for key from the local cache. Does not trigger
// a DB read. Local to this process only.
func (s *PostgresStore) Invalidate(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.ptr.Load()
	if old == nil {
		return
	}
	cp := make(map[string]Flag, len(*old))
	for k, v := range *old {
		if k == key {
			continue
		}
		cp[k] = v
	}
	s.ptr.Store(&cp)
}

// AllFlags returns the current cached map. Used by endpoints that need to
// enumerate all known flags (e.g. /feature-flags/me). Returns a snapshot
// that callers must not mutate. Triggers a background refresh when the
// cache is stale so Invalidate+TTL still converges here — without this
// kick, /feature-flags/me (which never calls Get) would serve a map with
// Invalidated keys missing until something else refreshed.
func (s *PostgresStore) AllFlags() map[string]Flag {
	if s.stale() {
		go s.tryRefresh()
	}
	m := s.ptr.Load()
	if m == nil {
		return nil
	}
	return *m
}

func (s *PostgresStore) stale() bool {
	last := s.lastRefreshed.Load()
	if last == 0 {
		return true
	}
	return time.Since(time.Unix(0, last)) > s.ttl
}

func (s *PostgresStore) tryRefresh() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.Refresh(ctx)
}

// LoadForService is an escape hatch for callers that want a one-shot read
// without running the cache (e.g. tests or handlers with no store wired).
func LoadForService(ctx context.Context, pool *pgxpool.Pool) (map[string]Flag, error) {
	return loadFromPool(ctx, pool)
}

// loadFromPool is the production refresher.
func loadFromPool(ctx context.Context, pool *pgxpool.Pool) (map[string]Flag, error) {
	rows, err := pool.Query(ctx, `SELECT key, value_json FROM app_settings WHERE key LIKE 'flag.%'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Flag{}
	for rows.Next() {
		var key string
		var raw []byte
		if err := rows.Scan(&key, &raw); err != nil {
			return nil, err
		}
		out[key] = ParseFromJSON(raw)
	}
	return out, rows.Err()
}

// ParseFromJSON accepts BOTH the legacy bare-bool shape and the new object
// shape so Spine can deploy before the rewrap migration runs. Exported so
// Spine's GET /admin/feature-flags can share the exact same parsing logic
// the Store uses internally.
//
// TODO(remove after migration 010 confirmed applied on every deployed stack):
// delete the bare-bool branch below.
func ParseFromJSON(raw []byte) Flag {
	// bare bool
	var b bool
	if err := json.Unmarshal(raw, &b); err == nil {
		return Flag{Enabled: b, Roles: []auth.Role{auth.RoleAdmin, auth.RoleSupport, auth.RoleUser}}
	}
	// object shape
	var obj struct {
		Enabled bool     `json:"enabled"`
		Roles   []string `json:"roles"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return Flag{} // malformed → disabled
	}
	roles := make([]auth.Role, 0, len(obj.Roles))
	for _, r := range obj.Roles {
		roles = append(roles, auth.Role(r))
	}
	return Flag{Enabled: obj.Enabled, Roles: roles}
}
