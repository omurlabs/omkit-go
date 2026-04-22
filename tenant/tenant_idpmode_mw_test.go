package tenant_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/omurlabs/omur-core/packages/omur-go-sdk/tenant"
)

func TestMiddlewareFactoryRejectsIllegalFlagCombos(t *testing.T) {
	cases := []struct {
		name    string
		cfg     tenant.MiddlewareConfig
		wantErr bool
	}{
		{"authentik default", tenant.MiddlewareConfig{IDPMode: "authentik"}, false},
		{"zitadel default", tenant.MiddlewareConfig{IDPMode: "zitadel"}, false},
		{"empty defaults to authentik", tenant.MiddlewareConfig{}, false},
		{
			"authentik + retire-authentik is contradictory",
			tenant.MiddlewareConfig{IDPMode: "authentik", RetireAuthentik: true},
			true,
		},
		{
			"retire-authentik requires privacy retirement first",
			tenant.MiddlewareConfig{IDPMode: "zitadel", RetireAuthentik: true, PrivacyRetireAuthentik: false},
			true,
		},
		{"unknown IDPMode is rejected", tenant.MiddlewareConfig{IDPMode: "okta"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tenant.Middleware(tc.cfg)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error for %+v, got nil", tc.cfg)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected no error for %+v, got %v", tc.cfg, err)
			}
		})
	}
}

func TestMiddlewareZitadelHeaderFamily(t *testing.T) {
	const wantTenant = "tenant-for-zitadel-user"
	const zitadelSub = "198261369861120001"

	cfg := tenant.MiddlewareConfig{
		IDPMode: tenant.IDPModeZitadel,
		ResolveV2: func(ctx context.Context, source, authUID string) (string, error) {
			if source != "zitadel" {
				t.Errorf("expected source=zitadel, got %q", source)
			}
			if authUID != zitadelSub {
				t.Errorf("expected authUID=%q, got %q", zitadelSub, authUID)
			}
			return wantTenant, nil
		},
	}
	mw, err := tenant.Middleware(cfg)
	if err != nil {
		t.Fatalf("Middleware: %v", err)
	}

	var gotTenant string
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTenant = tenant.FromRequest(r)
	}))

	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Set("X-Auth-Request-User", zitadelSub)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if gotTenant != wantTenant {
		t.Errorf("tenant = %q; want %q", gotTenant, wantTenant)
	}

	// Sanity: the old Authentik header must be ignored under IDPMode=zitadel.
	req2 := httptest.NewRequest("GET", "/x", nil)
	req2.Header.Set("X-Authentik-Uid", "12345")
	gotTenant = ""
	handler.ServeHTTP(httptest.NewRecorder(), req2)
	if gotTenant != "" {
		t.Errorf("Authentik header leaked under IDPMode=zitadel: got %q", gotTenant)
	}
}
