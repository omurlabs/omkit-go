package settings_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/omurlabs/omur-core/packages/omur-go-sdk/settings"
)

func TestCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "settings.json")

	m := settings.New(nil, settings.WithCache(cachePath))

	// Manually apply changes to populate cache
	m.ApplyChange("key1", "val1")
	m.ApplyChange("key2", "val2")

	// Write cache to disk
	if err := m.WriteCache(); err != nil {
		t.Fatalf("WriteCache: %v", err)
	}

	// New manager reads from disk cache
	m2 := settings.New(nil, settings.WithCache(cachePath))
	if err := m2.LoadCache(); err != nil {
		t.Fatalf("LoadCache: %v", err)
	}

	if got := m2.Get("key1"); got != "val1" {
		t.Errorf("key1: got %q, want %q", got, "val1")
	}
	if got := m2.Get("key2"); got != "val2" {
		t.Errorf("key2: got %q, want %q", got, "val2")
	}
}

func TestGet(t *testing.T) {
	m := settings.New(nil)
	m.ApplyChange("exists", "hello")

	if got := m.Get("exists"); got != "hello" {
		t.Errorf("exists: got %q, want %q", got, "hello")
	}
	if got := m.Get("missing"); got != "" {
		t.Errorf("missing: got %q, want empty", got)
	}
}

func TestOnChange(t *testing.T) {
	m := settings.New(nil)

	var gotKey, gotVal string
	m.OnChange("mykey", func(key, value string) {
		gotKey = key
		gotVal = value
	})

	m.ApplyChange("mykey", "myval")

	if gotKey != "mykey" {
		t.Errorf("key: got %q, want %q", gotKey, "mykey")
	}
	if gotVal != "myval" {
		t.Errorf("val: got %q, want %q", gotVal, "myval")
	}
}

func TestCacheFilePermissions(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "settings.json")

	m := settings.New(nil, settings.WithCache(cachePath))
	m.ApplyChange("k", "v")

	if err := m.WriteCache(); err != nil {
		t.Fatalf("WriteCache: %v", err)
	}

	info, err := os.Stat(cachePath)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("permissions: got %o, want 0600", perm)
	}
}
