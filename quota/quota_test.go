package quota_test

import (
	"testing"
	"time"

	"github.com/omurlabs/omur-core/packages/omur-go-sdk/quota"
)

func TestCheckUploadRejectsWhenOverDocs(t *testing.T) {
	lim := quota.Limits{Docs: 10, StorageBytes: 1 << 30, QueriesPerMonth: 1000}
	usage := quota.Usage{Docs: 10, StorageBytes: 0, QueriesThisMonth: 0}
	d, _ := quota.CheckUpload(lim, usage, 1)
	if d.Allowed {
		t.Fatal("docs at limit must be rejected")
	}
	if d.Resource != quota.ResourceDocs {
		t.Fatalf("resource = %s, want %s", d.Resource, quota.ResourceDocs)
	}
}

func TestCheckUploadRejectsWhenOverBytes(t *testing.T) {
	lim := quota.Limits{Docs: 100, StorageBytes: 1000, QueriesPerMonth: 1000}
	usage := quota.Usage{Docs: 0, StorageBytes: 500, QueriesThisMonth: 0}
	d, _ := quota.CheckUpload(lim, usage, 600)
	if d.Allowed {
		t.Fatal("upload that would push over byte limit must be rejected")
	}
	if d.Resource != quota.ResourceStorage {
		t.Fatalf("resource = %s, want %s", d.Resource, quota.ResourceStorage)
	}
}

func TestCheckUploadAllowsWhenUnder(t *testing.T) {
	lim := quota.Limits{Docs: 100, StorageBytes: 1 << 30, QueriesPerMonth: 1000}
	usage := quota.Usage{Docs: 50, StorageBytes: 1024, QueriesThisMonth: 0}
	d, _ := quota.CheckUpload(lim, usage, 512)
	if !d.Allowed {
		t.Fatal("under-limit upload must be allowed")
	}
}

func TestCheckQueryRejectsAtLimit(t *testing.T) {
	lim := quota.Limits{Docs: 100, StorageBytes: 1 << 30, QueriesPerMonth: 1000}
	usage := quota.Usage{Docs: 0, StorageBytes: 0, QueriesThisMonth: 1000}
	d, _ := quota.CheckQuery(lim, usage)
	if d.Allowed {
		t.Fatal("queries at limit must be rejected")
	}
	if d.RetryAfter <= 0 {
		t.Fatalf("RetryAfter must be > 0, got %d", d.RetryAfter)
	}
}

func TestCheckQueryAllowsWhenUnder(t *testing.T) {
	lim := quota.Limits{Docs: 100, StorageBytes: 1 << 30, QueriesPerMonth: 1000}
	usage := quota.Usage{Docs: 0, StorageBytes: 0, QueriesThisMonth: 999}
	d, _ := quota.CheckQuery(lim, usage)
	if !d.Allowed {
		t.Fatal("one-under-limit query must be allowed")
	}
}

func TestDefaultsMatchSpec(t *testing.T) {
	if quota.DefaultDocs != 100 {
		t.Fatalf("DefaultDocs = %d, want 100", quota.DefaultDocs)
	}
	if quota.DefaultStorageBytes != int64(500*1024*1024) {
		t.Fatalf("DefaultStorageBytes = %d, want %d", quota.DefaultStorageBytes, int64(500*1024*1024))
	}
	if quota.DefaultQueriesPerMonth != 1000 {
		t.Fatalf("DefaultQueriesPerMonth = %d, want 1000", quota.DefaultQueriesPerMonth)
	}
}

func TestCapAt32DaysBoundaries(t *testing.T) {
	// Inject a "now" that is 2026-04-30 23:59 UTC.
	// firstOfNextMonth() => 2026-05-01 00:00 UTC => diff = 60 seconds.
	fake := time.Date(2026, 4, 30, 23, 59, 0, 0, time.UTC)
	quota.SetNowUTC(func() time.Time { return fake })
	defer quota.SetNowUTC(nil) // restore

	lim := quota.Limits{Docs: 100, StorageBytes: 1 << 30, QueriesPerMonth: 1}
	usage := quota.Usage{QueriesThisMonth: 1}
	d, _ := quota.CheckQuery(lim, usage)
	if d.RetryAfter <= 0 || d.RetryAfter >= 86400 {
		t.Fatalf("RetryAfter = %d, want 0 < n < 86400", d.RetryAfter)
	}
}
