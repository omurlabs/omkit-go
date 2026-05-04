// scheduler.go — scheduler module.
//
// exports: ProviderSource | PgxProviderSource | FetchProviders | DefaultPollInterval | Provider | CronDeriver | Asynq | Enqueuer | Scheduler | Option | WithPollInterval | WithImmediateOnRegister | New | Start | Stop | Entries
// rules:   none
// agent:   codedna-cli (no-llm) | codedna-cli | 2026-04-30 | codedna-cli | initial CodeDNA annotation pass
// message: 

// Package scheduler wraps Asynq Scheduler with a DB-driven reconcile loop.
//
// The Asynq Scheduler API requires callers to Register cron entries one-by-one
// and surrender Unregister responsibility on change. For services like Pulse
// that derive schedules from a database (one entry per (tenant, provider)
// row), this package owns the reconcile pattern: every PollInterval it queries
// the providers table, diffs the desired state against the registered entries,
// and Register/Unregisters to converge.
//
// Cross-replica caveat: Asynq Scheduler does NOT dedupe firings across
// replicas — running two pulse instances against the same Valkey will
// enqueue the same task twice per cronspec firing. The spec line claiming
// internal SETNX coordination was wrong; the asynq Scheduler tracks entries
// in Redis but each replica fires its own. Services that use this package
// must deploy as a single replica until either (a) leader election is added
// here, or (b) handler-side idempotency makes duplicate firings harmless.
package scheduler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/omurlabs/omur-core/packages/omur-go-sdk/jobqueue"
)

// ProviderSource is the data path the reconcile loop reads from. The
// production implementation queries the providers table; tests pass a stub.
type ProviderSource interface {
	FetchProviders(ctx context.Context, kind string) ([]Provider, error)
}

// PgxProviderSource queries the providers table via a pgxpool.Pool. Pulse
// passes a BYPASSRLS schema-owner pool because the read is intentionally
// cross-tenant.
type PgxProviderSource struct{ Pool *pgxpool.Pool }

func (p PgxProviderSource) FetchProviders(ctx context.Context, kind string) ([]Provider, error) {
	const q = "SELECT tenant_id::text, name, config FROM providers WHERE kind = $1 AND enabled = TRUE"
	rows, err := p.Pool.Query(ctx, q, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Provider
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
		out = append(out, Provider{TenantID: tid, Name: name, Config: cfg})
	}
	return out, rows.Err()
}

// DefaultPollInterval is how often the reconcile loop re-fetches the
// providers table. Matches the registry SDK's historical default.
const DefaultPollInterval = 10 * time.Second

// Provider is one row from the providers table after JSON-decoding config.
type Provider struct {
	TenantID string
	Name     string
	Config   map[string]any
}

// CronDeriver turns a Provider row into a cron spec. Returning ok=false skips
// the row (unknown provider, missing config, etc.).
type CronDeriver func(p Provider) (cronspec string, ok bool)

// Asynq is the narrow interface the reconciler needs from *asynq.Scheduler.
// Defined to allow mocking; production code passes a real *asynq.Scheduler.
type Asynq interface {
	Register(cronspec string, task *asynq.Task, opts ...asynq.Option) (string, error)
	Unregister(entryID string) error
	Start() error
	Shutdown()
}

// Enqueuer is the narrow interface for the immediate-poll-on-register hook.
// Pulse passes a *jobqueue.Client adapter; tests pass a stub.
type Enqueuer interface {
	Enqueue(ctx context.Context, taskType, tenantID string, payload any, opts ...asynq.Option) (*asynq.TaskInfo, error)
}

// Scheduler owns the reconcile loop and the registered entry table.
type Scheduler struct {
	asynq        Asynq
	source       ProviderSource
	kind         string // providers.kind value, e.g. "collector"
	queue        string // Asynq queue name, e.g. "pulse"
	taskType     string // task type, e.g. "pulse:provider-sync"
	deriveCron   CronDeriver
	pollInterval time.Duration
	enqueuer     Enqueuer // optional; immediate poll on first registration

	mu      sync.Mutex
	entries map[string]registeredEntry // key "<tenant>:<name>"

	wg     sync.WaitGroup
	cancel context.CancelFunc
}

type registeredEntry struct {
	entryID  string
	hash     string // sha256(cronspec || config_json) — re-register on change
	cronspec string
}

// Option configures Scheduler.
type Option func(*Scheduler)

// WithPollInterval overrides the default reconcile cadence.
func WithPollInterval(d time.Duration) Option {
	return func(s *Scheduler) { s.pollInterval = d }
}

// WithImmediateOnRegister enqueues one task as soon as a new (tenant,
// provider) row is registered. Restores the "first poll on start" behaviour
// from the legacy ticker loops.
func WithImmediateOnRegister(e Enqueuer) Option {
	return func(s *Scheduler) { s.enqueuer = e }
}

// New builds a Scheduler. asynqClient is typically jobqueue.NewScheduler(cfg).
// source is the providers-table reader; pulse passes a PgxProviderSource
// backed by a BYPASSRLS schema-owner pool because the read is intentionally
// cross-tenant.
func New(asynqClient Asynq, source ProviderSource, kind, queue, taskType string, deriveCron CronDeriver, opts ...Option) *Scheduler {
	s := &Scheduler{
		asynq:        asynqClient,
		source:       source,
		kind:         kind,
		queue:        queue,
		taskType:     taskType,
		deriveCron:   deriveCron,
		pollInterval: DefaultPollInterval,
		entries:      make(map[string]registeredEntry),
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Start does an initial reconcile, starts the underlying Asynq Scheduler, and
// kicks off the reconcile loop. Returns the asynq Scheduler's start error if
// any; the reconcile loop never errors back (failures are logged and retried
// next tick).
func (s *Scheduler) Start(ctx context.Context) error {
	ctx, s.cancel = context.WithCancel(ctx)

	if err := s.asynq.Start(); err != nil {
		return fmt.Errorf("asynq scheduler start: %w", err)
	}

	// Initial reconcile so providers are scheduled before the first tick.
	s.reconcile(ctx)

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.runLoop(ctx)
	}()
	slog.Info("scheduler.started", "kind", s.kind, "entries", len(s.entries))
	return nil
}

// Stop cancels the reconcile loop and shuts down the underlying scheduler.
func (s *Scheduler) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
	s.asynq.Shutdown()
	slog.Info("scheduler.stopped", "kind", s.kind)
}

// Entries returns the keys ("<tenant>:<name>") of currently registered
// schedules. Used by service /providers HTTP endpoints.
func (s *Scheduler) Entries() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := make([]string, 0, len(s.entries))
	for k := range s.entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (s *Scheduler) runLoop(ctx context.Context) {
	t := time.NewTicker(s.pollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.reconcile(ctx)
		}
	}
}

// reconcile is the diff engine: fetch desired rows, compare to s.entries,
// register/unregister to converge.
func (s *Scheduler) reconcile(ctx context.Context) {
	rows, err := s.source.FetchProviders(ctx, s.kind)
	if err != nil {
		slog.Warn("scheduler.fetch_failed", "kind", s.kind, "error", err)
		return
	}

	desired := make(map[string]Provider, len(rows))
	for _, r := range rows {
		desired[key(r.TenantID, r.Name)] = r
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Unregister entries no longer in DB.
	for k, ent := range s.entries {
		if _, ok := desired[k]; !ok {
			if err := s.asynq.Unregister(ent.entryID); err != nil {
				slog.Warn("scheduler.unregister_failed", "key", k, "error", err)
				continue
			}
			delete(s.entries, k)
			slog.Info("scheduler.unregistered", "key", k)
		}
	}

	// Register new or changed entries.
	for k, p := range desired {
		cronspec, ok := s.deriveCron(p)
		if !ok {
			continue
		}
		h := configHash(cronspec, p.Config)
		if existing, registered := s.entries[k]; registered {
			if existing.hash == h {
				continue // unchanged
			}
			// Config or cronspec changed — unregister + re-register.
			if err := s.asynq.Unregister(existing.entryID); err != nil {
				slog.Warn("scheduler.reregister_unregister_failed", "key", k, "error", err)
				continue
			}
			delete(s.entries, k)
		}

		entryID, err := s.registerOne(p, cronspec)
		if err != nil {
			slog.Warn("scheduler.register_failed", "key", k, "error", err)
			continue
		}
		s.entries[k] = registeredEntry{entryID: entryID, hash: h, cronspec: cronspec}
		slog.Info("scheduler.registered", "key", k, "cronspec", cronspec)

		// Optional immediate poll: enqueue one task right now so callers don't
		// wait for the first cron firing (which can be up to `cronspec` away).
		if s.enqueuer != nil {
			payload := buildPayload(p)
			if _, err := s.enqueuer.Enqueue(ctx, s.taskType, p.TenantID, payload, asynq.Queue(s.queue)); err != nil {
				slog.Warn("scheduler.immediate_enqueue_failed", "key", k, "error", err)
			}
		}
	}
}

func (s *Scheduler) registerOne(p Provider, cronspec string) (string, error) {
	payload := buildPayload(p)
	body, err := jobqueue.Wrap(p.TenantID, payload)
	if err != nil {
		return "", fmt.Errorf("envelope wrap: %w", err)
	}
	task := asynq.NewTask(s.taskType, body)
	return s.asynq.Register(cronspec, task, asynq.Queue(s.queue))
}

// providerSyncPayload mirrors what handlers receive after envelope unwrap.
// Defined here so the SDK + service stay in sync; the service's handler
// re-defines an identical struct rather than importing this package's type
// (loose coupling).
type providerSyncPayload struct {
	ProviderName string          `json:"provider_name"`
	Config       json.RawMessage `json:"config"`
}

func buildPayload(p Provider) providerSyncPayload {
	cfg, err := json.Marshal(p.Config)
	if err != nil {
		// Fallback to empty object — handler will see no config but still
		// know the provider name. Marshalling map[string]any rarely fails;
		// this is defensive only.
		cfg = []byte("{}")
	}
	return providerSyncPayload{ProviderName: p.Name, Config: cfg}
}

func key(tenant, name string) string { return tenant + ":" + name }

func configHash(cronspec string, cfg map[string]any) string {
	// Deterministic JSON hash so map-key ordering doesn't churn the hash.
	body, _ := json.Marshal(cfg)
	h := sha256.New()
	h.Write([]byte(cronspec))
	h.Write([]byte{0})
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}
