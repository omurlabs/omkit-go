package privacy

import (
	"context"
	"strings"
)

// HeaderName is the canonical HTTP header carrying the per-request
// PrivacyClass across service boundaries. Track 4 of
// docs/superpowers/plans/2026-05-03-cloud-readiness-prep.md.
const HeaderName = "X-Omur-Privacy-Class"

// PrivacyClass is the per-request privacy classification. It lets routing
// decisions stay auditable across local-only, hybrid, and cloud-only
// deployments — the hostname-based "Ollama vs cloud-proxy" signal does not
// survive the cloud migration; this class does.
type PrivacyClass string

const (
	// ClassPublic — non-sensitive content; safe for any backend.
	ClassPublic PrivacyClass = "public"
	// ClassTenant — tenant-scoped content; default; safe for any backend
	// the tenant has authorised but not for shared / system endpoints.
	ClassTenant PrivacyClass = "tenant"
	// ClassSensitive — protected health information or equivalent; only
	// on-tenant or local backends; cloud routes must refuse.
	ClassSensitive PrivacyClass = "sensitive"
)

// DefaultPrivacyClass is the value applied when a request omits the header.
// It is the safe middle ground: callers that mean Public must say so
// explicitly.
func DefaultPrivacyClass() PrivacyClass { return ClassTenant }

// ParsePrivacyClass normalises an arbitrary header value into a
// PrivacyClass. It accepts case-insensitive matches with surrounding
// whitespace. Unknown / empty values fall back to DefaultPrivacyClass —
// the safe direction so unparseable headers do not silently downgrade
// sensitive content.
func ParsePrivacyClass(raw string) PrivacyClass {
	normalised := strings.ToLower(strings.TrimSpace(raw))
	switch normalised {
	case "public":
		return ClassPublic
	case "tenant":
		return ClassTenant
	case "sensitive":
		return ClassSensitive
	default:
		return DefaultPrivacyClass()
	}
}

// AllowsCloud reports whether a backend that may egress beyond the tenant
// boundary is permitted for this class. Sensitive is the only refusal.
func AllowsCloud(c PrivacyClass) bool {
	return c != ClassSensitive
}

// privacyClassKey is the unexported context key for the per-request
// PrivacyClass. Keeping the type private prevents collisions with
// other packages that store strings under the same key.
type privacyClassKey struct{}

// WithPrivacyClass returns a new context carrying the given PrivacyClass.
// Spine middleware sets this once per request after parsing the header;
// downstream services call FromContext to read it back.
func WithPrivacyClass(ctx context.Context, c PrivacyClass) context.Context {
	return context.WithValue(ctx, privacyClassKey{}, c)
}

// FromContext returns the PrivacyClass attached to the context, or
// DefaultPrivacyClass if none is present. Never returns the zero value
// of PrivacyClass — callers that branch on the result do not need to
// guard against an empty class.
func FromContext(ctx context.Context) PrivacyClass {
	v, ok := ctx.Value(privacyClassKey{}).(PrivacyClass)
	if !ok || v == "" {
		return DefaultPrivacyClass()
	}
	return v
}
