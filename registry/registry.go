// Package registry manages provider lifecycle: DB-driven tasks with Valkey hot-reload.
package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// Backend selects the live-reload strategy used by Registry.Start.
type Backend string

const (
	// BackendPostgres polls the providers table for changes.
	BackendPostgres Backend = "postgres"
	// BackendRedis subscribes to omur:providers:updated:* on Valkey.
	BackendRedis Backend = "redis"
)

// backendFromEnv returns the configured provider-registry backend, defaulting
// to postgres.
func backendFromEnv() Backend {
	v := os.Getenv("OMUR_PROVIDERS_BACKEND")
	switch Backend(v) {
	case BackendRedis:
		return BackendRedis
	case BackendPostgres:
		return BackendPostgres
	default:
		return BackendPostgres
	}
}

// DefaultProvidersPollInterval is the fallback poll interval for the postgres
// backend when callers don't override via WithPollInterval.
const DefaultProvidersPollInterval = 10 * time.Second

// Provider is the interface each data provider must implement.
type Provider interface {
	// Run is the main loop. Must respect ctx cancellation for clean shutdown.
	Run(ctx context.Context) error
}

// ProviderFactory creates a Provider instance from tenant ID and config.
type ProviderFactory func(tenantID string, config map[string]any) Provider

// Registry manages one goroutine per active (tenant_id, provider_name) pair.
type Registry struct {
	kind      string
	factories map[string]ProviderFactory
	db        *pgxpool.Pool
	valkeyURL string

	backend      Backend
	pollInterval time.Duration

	mu     sync.Mutex
	tasks  map[string]context.CancelFunc // key: "tenant_id:name"
	wg     sync.WaitGroup
	cancel context.CancelFunc
}

// Option configures a Registry.
type Option func(*Registry)

// WithPollInterval overrides the default providers poll interval for the
// postgres backend.
func WithPollInterval(d time.Duration) Option {
	return func(r *Registry) {
		r.pollInterval = d
	}
}

// WithBackend forces a specific backend, overriding OMUR_PROVIDERS_BACKEND.
func WithBackend(b Backend) Option {
	return func(r *Registry) {
		r.backend = b
	}
}

// New creates a Registry for the given provider kind (e.g. "collector"). The
// backend is selected from OMUR_PROVIDERS_BACKEND (default postgres) unless
// WithBackend is supplied.
func New(kind string, db *pgxpool.Pool, valkeyURL string, factories map[string]ProviderFactory, opts ...Option) *Registry {
	r := &Registry{
		kind:         kind,
		factories:    factories,
		db:           db,
		valkeyURL:    valkeyURL,
		tasks:        make(map[string]context.CancelFunc),
		pollInterval: DefaultProvidersPollInterval,
		backend:      backendFromEnv(),
	}
	for _, o := range opts {
		o(r)
	}
	return r
}

type providerRow struct {
	TenantID string
	Name     string
	Config   map[string]any
}

// Start loads enabled providers from DB and subscribes to Valkey for hot-reload.
func (r *Registry) Start(ctx context.Context) {
	ctx, r.cancel = context.WithCancel(ctx)

	rows, err := r.fetchProviders(ctx, "")
	if err != nil {
		slog.Warn("registry.db_unavailable", "kind", r.kind, "error", err)
	}
	for _, row := range rows {
		r.startTask(ctx, row)
	}

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		switch r.backend {
		case BackendRedis:
			r.subscribeValkey(ctx)
		default:
			r.runPollingLoop(ctx)
		}
	}()
	slog.Info("registry.started", "kind", r.kind, "tasks", len(r.tasks), "backend", string(r.backend))
}

// runPollingLoop reconciles desired-vs-running tasks every pollInterval by
// re-fetching from the providers table. This replaces valkey pub/sub at the
// cost of pollInterval latency.
func (r *Registry) runPollingLoop(ctx context.Context) {
	ticker := time.NewTicker(r.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.reconcile(ctx)
		}
	}
}

// Stop cancels all running providers and waits for them to finish.
func (r *Registry) Stop() {
	if r.cancel != nil {
		r.cancel()
	}
	r.wg.Wait()
	slog.Info("registry.stopped", "kind", r.kind)
}

// RunningKeys returns keys of currently running provider tasks.
func (r *Registry) RunningKeys() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	keys := make([]string, 0, len(r.tasks))
	for k := range r.tasks {
		keys = append(keys, k)
	}
	return keys
}

func (r *Registry) startTask(ctx context.Context, row providerRow) {
	factory, ok := r.factories[row.Name]
	if !ok {
		slog.Warn("registry.unknown_provider", "name", row.Name)
		return
	}
	key := fmt.Sprintf("%s:%s", row.TenantID, row.Name)

	r.mu.Lock()
	if _, exists := r.tasks[key]; exists {
		r.mu.Unlock()
		return
	}
	taskCtx, cancel := context.WithCancel(ctx)
	r.tasks[key] = cancel
	r.mu.Unlock()

	provider := factory(row.TenantID, row.Config)
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		defer func() {
			r.mu.Lock()
			delete(r.tasks, key)
			r.mu.Unlock()
		}()
		if err := provider.Run(taskCtx); err != nil && taskCtx.Err() == nil {
			slog.Error("registry.task_crashed", "key", key, "error", err)
		}
	}()
	slog.Info("registry.task_started", "key", key)
}

func (r *Registry) cancelTenant(tenantID string) {
	r.mu.Lock()
	var toCancel []string
	for key, cancel := range r.tasks {
		if len(key) > len(tenantID) && key[:len(tenantID)+1] == tenantID+":" {
			cancel()
			toCancel = append(toCancel, key)
		}
	}
	for _, k := range toCancel {
		delete(r.tasks, k)
	}
	r.mu.Unlock()
}

func (r *Registry) reloadTenant(ctx context.Context, tenantID string) {
	r.cancelTenant(tenantID)
	// Brief pause for goroutines to exit
	time.Sleep(50 * time.Millisecond)

	rows, err := r.fetchProviders(ctx, tenantID)
	if err != nil {
		slog.Warn("registry.reload_failed", "tenant", tenantID, "error", err)
		return
	}
	for _, row := range rows {
		r.startTask(ctx, row)
	}
	slog.Info("registry.tenant_reloaded", "tenant", tenantID, "tasks", len(rows))
}

func (r *Registry) fetchProviders(ctx context.Context, tenantID string) ([]providerRow, error) {
	var query string
	var args []any
	if tenantID != "" {
		query = "SELECT tenant_id::text, name, config FROM providers WHERE kind = $1 AND enabled = TRUE AND tenant_id = $2::uuid"
		args = []any{r.kind, tenantID}
	} else {
		query = "SELECT tenant_id::text, name, config FROM providers WHERE kind = $1 AND enabled = TRUE"
		args = []any{r.kind}
	}

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []providerRow
	for rows.Next() {
		var tid, name string
		var configJSON []byte
		if err := rows.Scan(&tid, &name, &configJSON); err != nil {
			continue
		}
		var cfg map[string]any
		if err := json.Unmarshal(configJSON, &cfg); err != nil {
			cfg = make(map[string]any)
		}
		result = append(result, providerRow{TenantID: tid, Name: name, Config: cfg})
	}
	return result, rows.Err()
}

func (r *Registry) subscribeValkey(ctx context.Context) {
	if r.valkeyURL == "" {
		return
	}
	backoff := time.Second
	for {
		if err := r.runValkeySub(ctx); err == nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
			backoff = min(backoff*2, 60*time.Second)
		}
	}
}

func (r *Registry) runValkeySub(ctx context.Context) error {
	opts, err := redis.ParseURL(r.valkeyURL)
	if err != nil {
		return err
	}
	client := redis.NewClient(opts)
	defer client.Close()

	sub := client.PSubscribe(ctx, "omur:providers:updated:*")
	defer sub.Close()

	// Reconcile on connect
	r.reconcile(ctx)

	ch := sub.Channel()
	for {
		select {
		case <-ctx.Done():
			return nil
		case msg, ok := <-ch:
			if !ok {
				return fmt.Errorf("channel closed")
			}
			// Channel format: omur:providers:updated:<tenant_id>
			parts := splitChannel(msg.Channel)
			if len(parts) >= 4 {
				r.reloadTenant(ctx, parts[3])
			}
		}
	}
}

func (r *Registry) reconcile(ctx context.Context) {
	rows, err := r.fetchProviders(ctx, "")
	if err != nil {
		return
	}
	desired := make(map[string]providerRow)
	for _, row := range rows {
		desired[fmt.Sprintf("%s:%s", row.TenantID, row.Name)] = row
	}

	r.mu.Lock()
	// Cancel tasks not in DB
	for key, cancel := range r.tasks {
		if _, ok := desired[key]; !ok {
			cancel()
			delete(r.tasks, key)
		}
	}
	// Snapshot current keys
	current := make(map[string]bool)
	for k := range r.tasks {
		current[k] = true
	}
	r.mu.Unlock()

	// Start missing tasks
	for key, row := range desired {
		if !current[key] {
			r.startTask(ctx, row)
		}
	}
}

func splitChannel(ch string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(ch); i++ {
		if ch[i] == ':' {
			parts = append(parts, ch[start:i])
			start = i + 1
		}
	}
	parts = append(parts, ch[start:])
	return parts
}
