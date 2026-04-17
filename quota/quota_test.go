package quota

import (
	"testing"
)

func TestCheckUploadRejectsWhenOverDocs(t *testing.T) {
	lim := Limits{Docs: 10, StorageBytes: 1 << 30, QueriesPerMonth: 1000}
	usage := Usage{Docs: 10, StorageBytes: 0, QueriesThisMonth: 0}
	d, _ := CheckUpload(lim, usage, 1)
	if d.Allowed {
		t.Fatal("docs at limit must be rejected")
	}
	if d.Resource != ResourceDocs {
		t.Fatalf("resource = %s, want %s", d.Resource, ResourceDocs)
	}
}

func TestCheckUploadRejectsWhenOverBytes(t *testing.T) {
	lim := Limits{Docs: 100, StorageBytes: 1000, QueriesPerMonth: 1000}
	usage := Usage{Docs: 0, StorageBytes: 500, QueriesThisMonth: 0}
	d, _ := CheckUpload(lim, usage, 600)
	if d.Allowed {
		t.Fatal("upload that would push over byte limit must be rejected")
	}
	if d.Resource != ResourceStorage {
		t.Fatalf("resource = %s, want %s", d.Resource, ResourceStorage)
	}
}

func TestCheckUploadAllowsWhenUnder(t *testing.T) {
	lim := Limits{Docs: 100, StorageBytes: 1 << 30, QueriesPerMonth: 1000}
	usage := Usage{Docs: 50, StorageBytes: 1024, QueriesThisMonth: 0}
	d, _ := CheckUpload(lim, usage, 512)
	if !d.Allowed {
		t.Fatal("under-limit upload must be allowed")
	}
}

func TestCheckQueryRejectsAtLimit(t *testing.T) {
	lim := Limits{Docs: 100, StorageBytes: 1 << 30, QueriesPerMonth: 1000}
	usage := Usage{Docs: 0, StorageBytes: 0, QueriesThisMonth: 1000}
	d, _ := CheckQuery(lim, usage)
	if d.Allowed {
		t.Fatal("queries at limit must be rejected")
	}
	if d.RetryAfter <= 0 {
		t.Fatalf("RetryAfter must be > 0, got %d", d.RetryAfter)
	}
}

func TestCheckQueryAllowsWhenUnder(t *testing.T) {
	lim := Limits{Docs: 100, StorageBytes: 1 << 30, QueriesPerMonth: 1000}
	usage := Usage{Docs: 0, StorageBytes: 0, QueriesThisMonth: 999}
	d, _ := CheckQuery(lim, usage)
	if !d.Allowed {
		t.Fatal("one-under-limit query must be allowed")
	}
}

func TestDefaultsMatchSpec(t *testing.T) {
	if DefaultDocs != 100 {
		t.Fatalf("DefaultDocs = %d, want 100", DefaultDocs)
	}
	if DefaultStorageBytes != int64(500*1024*1024) {
		t.Fatalf("DefaultStorageBytes = %d, want %d", DefaultStorageBytes, int64(500*1024*1024))
	}
	if DefaultQueriesPerMonth != 1000 {
		t.Fatalf("DefaultQueriesPerMonth = %d, want 1000", DefaultQueriesPerMonth)
	}
}

func TestCapAt32DaysBehavior(t *testing.T) {
	const day = 24 * 3600
	cases := []struct {
		in   int
		want int
	}{
		{-5, 60},
		{0, 0},
		{1000, 1000},
		{32 * day, 32 * day},
		{99 * day, 32 * day},
	}
	for _, tc := range cases {
		got := capAt32Days(tc.in)
		if got != tc.want {
			t.Errorf("capAt32Days(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}
