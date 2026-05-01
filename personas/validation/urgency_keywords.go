// urgency_keywords.go — urgency_keywords module.
//
// exports: UrgencyKeywords | HasUrgencyKeyword
// used_by: spine
// rules:   none
// agent:   codedna-cli | 2026-05-02 | extract from cortex/personas/validation

package validation

import "strings"

// UrgencyKeywords — clinician-maintained allowlist (spec §4.9).
// Sourced via Cortex registry from kb.urgency_keywords (clinician-only field).
// Slice 2 ships these as in-code defaults until the kb table lands.
var UrgencyKeywords = []string{
	"urgent",
	"emergency",
	"seek care",
	"999",
	"911",
	"112",
	"119",
	"call your doctor now",
	"call emergency services",
	"go to a&e",
	"go to the er",
	"ring 999",
	"immediately",
}

// HasUrgencyKeyword returns true if the template contains at least one
// urgency keyword (case-insensitive). Required for refusal templates with
// safety_class: critical_escalation per §4.9.
func HasUrgencyKeyword(template string) bool {
	if template == "" {
		return false
	}
	lower := strings.ToLower(template)
	for _, kw := range UrgencyKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}
