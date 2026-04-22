package tenant_test

import (
	"testing"

	"github.com/omurlabs/omur-core/packages/omur-go-sdk/tenant"
)

func TestBrowserUIDHeader(t *testing.T) {
	cases := map[string]string{
		"authentik": "X-Authentik-Uid",
		"zitadel":   "X-Auth-Request-User",
		"":          "X-Authentik-Uid", // empty defaults to authentik
	}
	for mode, want := range cases {
		if got := tenant.BrowserUIDHeader(mode); got != want {
			t.Errorf("BrowserUIDHeader(%q) = %q; want %q", mode, got, want)
		}
	}
}

func TestBrowserEmailHeader(t *testing.T) {
	cases := map[string]string{
		"authentik": "X-Authentik-Email",
		"zitadel":   "X-Auth-Request-Email",
	}
	for mode, want := range cases {
		if got := tenant.BrowserEmailHeader(mode); got != want {
			t.Errorf("BrowserEmailHeader(%q) = %q; want %q", mode, got, want)
		}
	}
}

func TestBrowserGroupsHeader(t *testing.T) {
	cases := map[string]string{
		"authentik": "X-Authentik-Groups",
		"zitadel":   "X-Auth-Request-Groups",
	}
	for mode, want := range cases {
		if got := tenant.BrowserGroupsHeader(mode); got != want {
			t.Errorf("BrowserGroupsHeader(%q) = %q; want %q", mode, got, want)
		}
	}
}
