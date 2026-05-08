package privacy

import "strings"

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
