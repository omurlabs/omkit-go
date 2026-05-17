package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"testing"

	"github.com/hibiken/asynq"

	"github.com/omurlabs/omkit-go/jobqueue"
)

const tenantA = "11111111-1111-1111-1111-111111111111"
const tenantB = "22222222-2222-2222-2222-222222222222"

// fakeAsynq records Register/Unregister calls. Returns deterministic entry IDs.
type fakeAsynq struct {
	mu     sync.Mutex
	nextID int
	live   map[string]registered // entryID → record
	regErr error
	unregErr error
}

type registered struct {
	cronspec string
	taskType string
	payload  []byte
	queue    string
}

func newFakeAsynq() *fakeAsynq { return &fakeAsynq{live: map[string]registered{}} }

func (f *fakeAsynq) Register(cronspec string, task *asynq.Task, opts ...asynq.Option) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.regErr != nil {
		return "", f.regErr
	}
	f.nextID++
	id := "entry-" + itoa(f.nextID)
	queue := ""
	for _, o := range opts {
		// asynq.Queue is exported as a function returning an Option; we can't
		// inspect without internal access. Skip for simplicity — tests assert
		// at the Register-call level instead.
		_ = o
	}
	f.live[id] = registered{cronspec: cronspec, taskType: task.Type(), payload: task.Payload(), queue: queue}
	return id, nil
}

func (f *fakeAsynq) Unregister(entryID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.unregErr != nil {
		return f.unregErr
	}
	if _, ok := f.live[entryID]; !ok {
		return errors.New("not found")
	}
	delete(f.live, entryID)
	return nil
}

func (f *fakeAsynq) Start() error  { return nil }
func (f *fakeAsynq) Shutdown()     {}

func (f *fakeAsynq) liveIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.live))
	for k := range f.live {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	out := ""
	for n > 0 {
		out = string(rune('0'+n%10)) + out
		n /= 10
	}
	return out
}

// stubSource serves a fixed list of providers per call. Tests mutate Rows
// between reconcile calls to simulate DB changes.
type stubSource struct {
	mu   sync.Mutex
	Rows []Provider
}

func (s *stubSource) FetchProviders(_ context.Context, _ string) ([]Provider, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]Provider, len(s.Rows))
	copy(cp, s.Rows)
	return cp, nil
}

// stubEnqueuer records immediate enqueue calls.
type stubEnqueuer struct {
	mu    sync.Mutex
	calls []immediateCall
}

type immediateCall struct {
	taskType string
	tenantID string
	payload  any
}

func (s *stubEnqueuer) Enqueue(_ context.Context, taskType, tenantID string, payload any, _ ...asynq.Option) (*asynq.TaskInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, immediateCall{taskType, tenantID, payload})
	return nil, nil
}

func deriveTrivial(p Provider) (string, bool) {
	return "@every 5m", true
}

func TestReconcile_RegistersDesired(t *testing.T) {
	fa := newFakeAsynq()
	src := &stubSource{Rows: []Provider{
		{TenantID: tenantA, Name: "nightscout", Config: map[string]any{"u": "x"}},
		{TenantID: tenantB, Name: "open_wearables", Config: map[string]any{"u": "y"}},
	}}
	s := New(fa, src, "collector", "pulse", "pulse:provider-sync", deriveTrivial)

	s.reconcile(context.Background())

	if got := len(fa.liveIDs()); got != 2 {
		t.Fatalf("registered = %d, want 2", got)
	}
	gotKeys := s.Entries()
	wantKeys := []string{tenantA + ":nightscout", tenantB + ":open_wearables"}
	if !equalStrings(gotKeys, wantKeys) {
		t.Errorf("Entries() = %v, want %v", gotKeys, wantKeys)
	}
}

func TestReconcile_UnregistersRemoved(t *testing.T) {
	fa := newFakeAsynq()
	src := &stubSource{Rows: []Provider{
		{TenantID: tenantA, Name: "nightscout"},
		{TenantID: tenantB, Name: "nightscout"},
	}}
	s := New(fa, src, "collector", "pulse", "pulse:provider-sync", deriveTrivial)
	s.reconcile(context.Background())
	if len(fa.liveIDs()) != 2 {
		t.Fatal("setup expected 2 live")
	}

	src.Rows = []Provider{{TenantID: tenantA, Name: "nightscout"}}
	s.reconcile(context.Background())

	if got := len(fa.liveIDs()); got != 1 {
		t.Errorf("after removal: live = %d, want 1", got)
	}
}

func TestReconcile_ReregistersOnConfigChange(t *testing.T) {
	fa := newFakeAsynq()
	src := &stubSource{Rows: []Provider{
		{TenantID: tenantA, Name: "open_wearables", Config: map[string]any{"interval": float64(60)}},
	}}
	s := New(fa, src, "collector", "pulse", "pulse:provider-sync", deriveTrivial)
	s.reconcile(context.Background())

	originalIDs := fa.liveIDs()
	if len(originalIDs) != 1 {
		t.Fatalf("setup: live = %d, want 1", len(originalIDs))
	}

	// Change config — same key, different config value.
	src.Rows[0].Config = map[string]any{"interval": float64(120)}
	s.reconcile(context.Background())

	newIDs := fa.liveIDs()
	if len(newIDs) != 1 {
		t.Fatalf("after change: live = %d, want 1", len(newIDs))
	}
	if newIDs[0] == originalIDs[0] {
		t.Error("entry must be re-registered (new entryID) when config changes")
	}
}

func TestReconcile_NoOpOnUnchanged(t *testing.T) {
	fa := newFakeAsynq()
	src := &stubSource{Rows: []Provider{
		{TenantID: tenantA, Name: "nightscout", Config: map[string]any{"u": "x"}},
	}}
	s := New(fa, src, "collector", "pulse", "pulse:provider-sync", deriveTrivial)
	s.reconcile(context.Background())
	first := fa.liveIDs()

	s.reconcile(context.Background())
	second := fa.liveIDs()

	if !equalStrings(first, second) {
		t.Errorf("idempotent reconcile expected: first=%v second=%v", first, second)
	}
}

func TestReconcile_SkipsRowsDeriveSaysNo(t *testing.T) {
	fa := newFakeAsynq()
	src := &stubSource{Rows: []Provider{
		{TenantID: tenantA, Name: "nightscout"},
		{TenantID: tenantB, Name: "unknown"},
	}}
	derive := func(p Provider) (string, bool) {
		if p.Name == "unknown" {
			return "", false
		}
		return "@every 5m", true
	}
	s := New(fa, src, "collector", "pulse", "pulse:provider-sync", derive)
	s.reconcile(context.Background())

	if len(fa.liveIDs()) != 1 {
		t.Errorf("live = %d, want 1 (unknown filtered)", len(fa.liveIDs()))
	}
}

func TestReconcile_PayloadWrapsEnvelope(t *testing.T) {
	fa := newFakeAsynq()
	src := &stubSource{Rows: []Provider{
		{TenantID: tenantA, Name: "nightscout", Config: map[string]any{"u": "x"}},
	}}
	s := New(fa, src, "collector", "pulse", "pulse:provider-sync", deriveTrivial)
	s.reconcile(context.Background())

	if len(fa.live) != 1 {
		t.Fatal("expected 1 live entry")
	}
	var rec registered
	for _, r := range fa.live {
		rec = r
	}
	env, err := jobqueue.Unwrap(rec.payload)
	if err != nil {
		t.Fatalf("unwrap: %v", err)
	}
	if env.TenantID != tenantA {
		t.Errorf("tenant in envelope = %q", env.TenantID)
	}
	var inner providerSyncPayload
	if err := json.Unmarshal(env.Payload, &inner); err != nil {
		t.Fatal(err)
	}
	if inner.ProviderName != "nightscout" {
		t.Errorf("provider name = %q", inner.ProviderName)
	}
	var cfg map[string]any
	if err := json.Unmarshal(inner.Config, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg["u"] != "x" {
		t.Errorf("config = %v", cfg)
	}
}

func TestReconcile_ImmediateEnqueueOnRegister(t *testing.T) {
	fa := newFakeAsynq()
	eq := &stubEnqueuer{}
	src := &stubSource{Rows: []Provider{
		{TenantID: tenantA, Name: "nightscout"},
	}}
	s := New(fa, src, "collector", "pulse", "pulse:provider-sync", deriveTrivial,
		WithImmediateOnRegister(eq))
	s.reconcile(context.Background())

	if len(eq.calls) != 1 {
		t.Fatalf("immediate calls = %d, want 1", len(eq.calls))
	}
	if eq.calls[0].tenantID != tenantA {
		t.Errorf("tenant = %q", eq.calls[0].tenantID)
	}

	// Idempotent re-reconcile must NOT re-enqueue.
	s.reconcile(context.Background())
	if len(eq.calls) != 1 {
		t.Errorf("re-reconcile triggered immediate enqueue: calls = %d", len(eq.calls))
	}
}

func TestEntriesSorted(t *testing.T) {
	fa := newFakeAsynq()
	src := &stubSource{Rows: []Provider{
		{TenantID: tenantB, Name: "z"},
		{TenantID: tenantA, Name: "a"},
	}}
	s := New(fa, src, "collector", "pulse", "pulse:provider-sync", deriveTrivial)
	s.reconcile(context.Background())

	got := s.Entries()
	if !sort.StringsAreSorted(got) {
		t.Errorf("Entries not sorted: %v", got)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	ac := append([]string(nil), a...)
	bc := append([]string(nil), b...)
	sort.Strings(ac)
	sort.Strings(bc)
	for i := range ac {
		if ac[i] != bc[i] {
			return false
		}
	}
	return true
}
