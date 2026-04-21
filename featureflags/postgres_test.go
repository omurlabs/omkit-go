package featureflags

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/omurlabs/omur-core/packages/omur-go-sdk/auth"
)

// fakeRefresher counts refresh calls and lets us control whether the refresh
// blocks, to simulate a slow Postgres.
type fakeRefresher struct {
	calls   atomic.Int32
	gate    chan struct{}
	flags   map[string]Flag
	refresh func(ctx context.Context) (map[string]Flag, error)
}

func newFakeRefresher(initial map[string]Flag) *fakeRefresher {
	fr := &fakeRefresher{
		flags: initial,
		gate:  make(chan struct{}, 1),
	}
	fr.refresh = func(ctx context.Context) (map[string]Flag, error) {
		fr.calls.Add(1)
		select {
		case <-fr.gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return fr.flags, nil
	}
	return fr
}

func TestPostgresStore_ThunderingHerd_CollapsesToSingleRefresh(t *testing.T) {
	fr := newFakeRefresher(map[string]Flag{"flag.x": {Enabled: true}})
	store := NewStoreWithRefresher(func(ctx context.Context) (map[string]Flag, error) {
		return fr.refresh(ctx)
	}, 1*time.Millisecond) // tiny TTL
	// prime once so the next reads see stale
	close(fr.gate)
	if err := store.Refresh(context.Background()); err != nil {
		t.Fatalf("prime: %v", err)
	}
	// reset gate to block next refresh
	fr.gate = make(chan struct{}, 1)
	// wait past TTL
	time.Sleep(5 * time.Millisecond)
	// fire 50 concurrent Gets; each will find stale-and-promote
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = store.Get("flag.x")
		}()
	}
	// give goroutines time to dispatch their singleflight calls
	time.Sleep(10 * time.Millisecond)
	// release the single in-flight refresh
	fr.gate <- struct{}{}
	wg.Wait()
	// give the collapsed refresh a moment to finish.
	time.Sleep(5 * time.Millisecond)
	// Exactly one refresh should have happened beyond the prime.
	if got := fr.calls.Load(); got != 2 { // 1 prime + 1 singleflight collapse
		t.Fatalf("want 2 refresh calls (1 prime + 1 collapse), got %d", got)
	}
}

func TestPostgresStore_RefreshError_KeepsStaleCache(t *testing.T) {
	wantFlag := Flag{Enabled: true}
	fr := newFakeRefresher(map[string]Flag{"flag.x": wantFlag})
	store := NewStoreWithRefresher(func(ctx context.Context) (map[string]Flag, error) {
		return fr.refresh(ctx)
	}, 1*time.Millisecond)
	close(fr.gate)
	if err := store.Refresh(context.Background()); err != nil {
		t.Fatalf("prime: %v", err)
	}
	// next refresh should fail; stale must remain
	fr.refresh = func(ctx context.Context) (map[string]Flag, error) {
		fr.calls.Add(1)
		return nil, context.DeadlineExceeded
	}
	time.Sleep(5 * time.Millisecond) // past TTL
	// read should still serve the stale value
	got, ok := store.Get("flag.x")
	if !ok {
		t.Fatalf("stale should survive error; got %v,%v", got, ok)
	}
	if got.Enabled != wantFlag.Enabled {
		t.Fatalf("stale flag should retain enabled=%v; got %+v", wantFlag.Enabled, got)
	}
}

func TestPostgresStore_Invalidate_ForcesReload(t *testing.T) {
	fr := newFakeRefresher(map[string]Flag{"flag.x": {Enabled: true}})
	store := NewStoreWithRefresher(func(ctx context.Context) (map[string]Flag, error) {
		return fr.refresh(ctx)
	}, 1*time.Hour)
	close(fr.gate)
	if err := store.Refresh(context.Background()); err != nil {
		t.Fatalf("prime: %v", err)
	}
	before := fr.calls.Load()
	store.Invalidate("flag.x")
	// Get now; should NOT have triggered a refresh (Invalidate is local map mutation only)
	if _, ok := store.Get("flag.x"); ok {
		t.Fatal("after Invalidate, key should be missing until next refresh")
	}
	// allow any Get-triggered background refresh to settle.
	time.Sleep(10 * time.Millisecond)
	if after := fr.calls.Load(); after != before {
		t.Fatalf("Invalidate must not trigger a refresh itself; got %d refresh calls", after-before)
	}
}

func TestParseFlagJSON_BareBool_GrantsAllRoles(t *testing.T) {
	f := ParseFromJSON([]byte("true"))
	if !f.Enabled || len(f.Roles) != 3 {
		t.Fatalf("want Enabled=true, 3 roles; got %+v", f)
	}
}

func TestParseFlagJSON_Object(t *testing.T) {
	f := ParseFromJSON([]byte(`{"enabled":true,"roles":["admin","user"]}`))
	if !f.Enabled || len(f.Roles) != 2 || f.Roles[0] != auth.RoleAdmin || f.Roles[1] != auth.RoleUser {
		t.Fatalf("unexpected parse: %+v", f)
	}
}

func TestParseFlagJSON_Malformed_Disabled(t *testing.T) {
	f := ParseFromJSON([]byte("{not-json}"))
	if f.Enabled {
		t.Fatal("malformed JSON must disable the flag")
	}
}
