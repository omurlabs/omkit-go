// pipeline.go — pipeline module.
//
// exports: ErrPromptOverlaySafetyScreenFailed | ErrInterpolationTokenDisallowed | ErrUrgencyKeywordMissing | ErrSafetyClassDowngrade | ErrClinicianFieldImmutable | SafetyScreen | Pipeline | New | ValidateOverlay | ValidateRefusalTemplate | ValidateAdminDiff
// rules:   none
// agent:   codedna-cli | 2026-05-02 | extract from cortex/personas/validation

package validation

import "errors"

var (
	// ErrPromptOverlaySafetyScreenFailed is returned when the safety screen
	// rejects a prompt overlay.
	ErrPromptOverlaySafetyScreenFailed = errors.New("prompt_overlay_safety_screen_failed")
	// ErrInterpolationTokenDisallowed is returned when a template contains a
	// disallowed interpolation token.
	ErrInterpolationTokenDisallowed = errors.New("interpolation_token_disallowed")
	// ErrUrgencyKeywordMissing is returned when a critical_escalation template
	// lacks a required urgency keyword.
	ErrUrgencyKeywordMissing = errors.New("urgency_keyword_missing")
	// ErrSafetyClassDowngrade is returned when an admin attempts to downgrade
	// a safety_class (e.g. critical_escalation → data_gap).
	ErrSafetyClassDowngrade = errors.New("safety_class_admin_denied")
	// ErrClinicianFieldImmutable is returned when an admin attempts to mutate
	// a clinician-only field without the clinician role.
	ErrClinicianFieldImmutable = errors.New("clinician_field_immutable")
)

// SafetyScreen wraps the existing /v1/safety/screen endpoint.
type SafetyScreen interface {
	Screen(text string) error
}

// Pipeline orchestrates the content-validation steps for persona config writes.
type Pipeline struct {
	screen SafetyScreen
}

// New creates a Pipeline with the given safety screen implementation.
func New(s SafetyScreen) *Pipeline {
	return &Pipeline{screen: s}
}

// ValidateOverlay validates prompt_overlay.identity and prompt_overlay.rules
// by running them through the safety screen.
func (p *Pipeline) ValidateOverlay(identity, rules string) error {
	if err := p.screen.Screen(identity); err != nil {
		return ErrPromptOverlaySafetyScreenFailed
	}
	if err := p.screen.Screen(rules); err != nil {
		return ErrPromptOverlaySafetyScreenFailed
	}
	return nil
}

// ValidateRefusalTemplate validates interpolation tokens + urgency keywords
// for critical_escalation templates.
func (p *Pipeline) ValidateRefusalTemplate(templateID, safetyClass, body string) error {
	if err := ValidateInterpolationTokens(body); err != nil {
		return err
	}
	if safetyClass == "critical_escalation" && !HasUrgencyKeyword(body) {
		return ErrUrgencyKeywordMissing
	}
	return nil
}

// ValidateAdminDiff rejects diffs touching clinician-only fields when called
// with a non-clinician role.
// role: "admin" | "clinician" | "system"
func (p *Pipeline) ValidateAdminDiff(diffs []FieldDiff, role string) error {
	if role == "clinician" || role == "system" {
		return nil
	}
	for _, d := range diffs {
		if IsClinicianFieldChange(d) {
			return ErrClinicianFieldImmutable
		}
	}
	return nil
}
