// token_allowlist.go — token_allowlist module.
//
// exports: AllowedInterpolationTokens | ValidateInterpolationTokens
// rules:   none
// agent:   codedna-cli | 2026-05-02 | extract from cortex/personas/validation

package validation

import (
	"fmt"
	"regexp"
)

// AllowedInterpolationTokens — see spec §4.9.
// Numeric value tokens ({lab_value}, {dose_mg}) are deliberately forbidden:
// LLM hallucination risk on numeric values is unacceptable for safety templates.
var AllowedInterpolationTokens = map[string]struct{}{
	"persona_label": {},
	"lab_name":      {},
	"lab_unit":      {},
	"drug_name":     {},
	"age_band":      {},
	"trimester":     {},
}

var tokenRE = regexp.MustCompile(`\{([a-z_]+)\}`)

// ValidateInterpolationTokens returns an error if the template contains
// any token outside the allowlist.
func ValidateInterpolationTokens(template string) error {
	for _, m := range tokenRE.FindAllStringSubmatch(template, -1) {
		token := m[1]
		if _, ok := AllowedInterpolationTokens[token]; !ok {
			return fmt.Errorf("interpolation_token_disallowed: {%s} not in allowlist", token)
		}
	}
	return nil
}
