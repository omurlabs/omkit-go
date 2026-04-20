package tenant

import (
	"context"
	"net/http/httptest"
	"testing"
)

func TestWithID_Sets(t *testing.T) {
	ctx := WithID(context.Background(), "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	if got := FromContext(ctx); got != "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee" {
		t.Errorf("FromContext: got %q, want the override", got)
	}
}

func TestWithID_Overrides_ExistingValue(t *testing.T) {
	base := WithID(context.Background(), "original-tid")
	override := WithID(base, "system-tid")
	if got := FromContext(override); got != "system-tid" {
		t.Errorf("override not applied: got %q", got)
	}
	if got := FromContext(base); got != "original-tid" {
		t.Errorf("base mutated: got %q, want original-tid", got)
	}
}

func TestWithIDRequest_RewritesRequestContext(t *testing.T) {
	req := httptest.NewRequest("GET", "/x", nil)
	out := WithIDRequest(req, "system-tid")
	if got := FromRequest(out); got != "system-tid" {
		t.Errorf("WithIDRequest: got %q", got)
	}
	if got := FromRequest(req); got != "" {
		t.Errorf("WithIDRequest mutated original: got %q", got)
	}
}
