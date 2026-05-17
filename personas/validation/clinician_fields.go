// clinician_fields.go — clinician_fields module.
//
// exports: FieldDiff | ClinicianOnlyPaths | IsClinicianFieldChange
// rules:   none
// agent:   codedna-cli | 2026-05-02 | extract from cortex/personas/validation

// Package validation provides the content-validation pipeline for persona
// config writes. Used by both the YAML import path and the admin PATCH path.
// See spec §4.9 for the canonical clinician-only field set.
package validation

import (
	"regexp"
	"strings"
)

// FieldDiff represents a single jsonb path that changed between OLD and NEW.
type FieldDiff struct {
	Path     string `json:"path"`
	OldValue any    `json:"old"`
	NewValue any    `json:"new"`
}

// ClinicianOnlyPaths — canonical list per spec §4.9.
// Match by path prefix (handles array-element fields like guards[N].priority).
var ClinicianOnlyPaths = []string{
	"pregnancy_teratogen_block.entries",
	"critical_value_escalation.threshold_table",
	"trend_min_data.analyte_class_table",
	"kb.red_flags",
	"kb.urgency_keywords",
	"guards[].priority",
	"guards[].safety_axis",
	"guards[].requires_clinician_review",
	"guards[].conservative_default",
	"refusal_templates[].safety_class",
}

var arrayIndexRE = regexp.MustCompile(`\[\d+\]`)

// IsClinicianFieldChange returns true if the diff path matches any
// clinician-only path (with array-index wildcards normalised).
func IsClinicianFieldChange(diff FieldDiff) bool {
	norm := normaliseArrayIndices(diff.Path)
	norm = normaliseMapKeys(norm)
	for _, path := range ClinicianOnlyPaths {
		if strings.HasPrefix(norm, path) || norm == path {
			return true
		}
	}
	return false
}

func normaliseArrayIndices(path string) string {
	return arrayIndexRE.ReplaceAllString(path, "[]")
}

// normaliseMapKeys converts refusal_templates.P_CRITICAL.safety_class
// into refusal_templates[].safety_class so map-keyed paths match ClinicianOnlyPaths.
func normaliseMapKeys(path string) string {
	prefixMap := map[string]string{
		"refusal_templates.": "refusal_templates[].",
	}
	for prefix, norm := range prefixMap {
		if strings.HasPrefix(path, prefix) {
			rest := path[len(prefix):]
			dotIdx := strings.IndexByte(rest, '.')
			if dotIdx >= 0 {
				return norm + rest[dotIdx+1:]
			}
			return norm + rest
		}
	}
	return path
}
