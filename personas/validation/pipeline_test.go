package validation

import "testing"

// ── token allowlist ──────────────────────────────────────────────────────────

func TestTokenAllowlist_AllowedTokensPass(t *testing.T) {
	cases := []string{
		"Your {lab_name} is high.",
		"Hello {persona_label}.",
		"Updated {drug_name} dosing for {age_band}.",
		"You are in {trimester} trimester.",
		"Units: {lab_unit}",
	}
	for _, in := range cases {
		if err := ValidateInterpolationTokens(in); err != nil {
			t.Errorf("expected pass: %q got %v", in, err)
		}
	}
}

func TestTokenAllowlist_ForbiddenTokensRejected(t *testing.T) {
	cases := []string{
		"Your potassium is {lab_value} mmol/L.", // {lab_value} forbidden
		"Take {dose_mg} mg.",                    // {dose_mg} forbidden
		"Hi {user_input}.",                      // free-form forbidden
	}
	for _, in := range cases {
		if err := ValidateInterpolationTokens(in); err == nil {
			t.Errorf("expected reject: %q", in)
		}
	}
}

func TestTokenAllowlist_NoTokensPass(t *testing.T) {
	if err := ValidateInterpolationTokens("No tokens here."); err != nil {
		t.Errorf("no tokens should pass: %v", err)
	}
}

// ── urgency keywords ─────────────────────────────────────────────────────────

func TestUrgencyKeywords_RequiresOneOnCriticalEscalation(t *testing.T) {
	cases := map[string]bool{
		"Please seek urgent medical care now.": true,
		"Call your doctor now if you feel ill.": true,
		"Try to be careful.":                    false,
		"Ring 999 immediately.":                 true,
		"Call 911 right away.":                  true,
		"":                                      false,
	}
	for in, want := range cases {
		got := HasUrgencyKeyword(in)
		if got != want {
			t.Errorf("HasUrgencyKeyword(%q) = %v; want %v", in, got, want)
		}
	}
}

// ── clinician fields ─────────────────────────────────────────────────────────

func TestClinicianFields_DowngradeDetected(t *testing.T) {
	diff := FieldDiff{
		Path:     "refusal_templates.P_CRITICAL.safety_class",
		OldValue: "critical_escalation",
		NewValue: "data_gap",
	}
	if !IsClinicianFieldChange(diff) {
		t.Error("safety_class downgrade must be detected as clinician-only field change")
	}
}

func TestClinicianFields_GuardPriorityDetected(t *testing.T) {
	diff := FieldDiff{
		Path:     "guards[0].priority",
		OldValue: 5,
		NewValue: 1,
	}
	if !IsClinicianFieldChange(diff) {
		t.Error("guards[N].priority must be detected as clinician-only field change")
	}
}

func TestClinicianFields_RequiresClinicianReviewDetected(t *testing.T) {
	diff := FieldDiff{
		Path:     "guards[1].requires_clinician_review",
		OldValue: true,
		NewValue: false,
	}
	if !IsClinicianFieldChange(diff) {
		t.Error("guards[N].requires_clinician_review must be detected as clinician-only field change")
	}
}

func TestClinicianFields_AllowedFieldNotDetected(t *testing.T) {
	diff := FieldDiff{
		Path:     "prompt_overlay.identity",
		OldValue: "old",
		NewValue: "new",
	}
	if IsClinicianFieldChange(diff) {
		t.Error("prompt_overlay.identity is admin-allowed; should not be detected as clinician-only")
	}
}

// ── pipeline ─────────────────────────────────────────────────────────────────

type stubScreen struct{ refuse bool }

func (s *stubScreen) Screen(_ string) error {
	if s.refuse {
		return ErrPromptOverlaySafetyScreenFailed
	}
	return nil
}

func TestPipeline_RejectsUnsafePromptOverlay(t *testing.T) {
	p := New(&stubScreen{refuse: true})
	err := p.ValidateOverlay("dangerous prompt", "rules")
	if err == nil {
		t.Error("expected rejection")
	}
}

func TestPipeline_AcceptsSafePromptOverlay(t *testing.T) {
	p := New(&stubScreen{})
	if err := p.ValidateOverlay("You are a helpful assistant.", "rules"); err != nil {
		t.Errorf("expected pass; got %v", err)
	}
}

func TestPipeline_RejectsCriticalTemplateMissingUrgency(t *testing.T) {
	p := New(&stubScreen{})
	err := p.ValidateRefusalTemplate("P_CRITICAL", "critical_escalation",
		"Please be careful with your meds.")
	if err != ErrUrgencyKeywordMissing {
		t.Errorf("expected ErrUrgencyKeywordMissing; got %v", err)
	}
}

func TestPipeline_AcceptsCriticalTemplateWithUrgency(t *testing.T) {
	p := New(&stubScreen{})
	err := p.ValidateRefusalTemplate("P_CRITICAL", "critical_escalation",
		"Your {lab_name} is outside the safe range. Seek urgent medical care now.")
	if err != nil {
		t.Errorf("expected pass; got %v", err)
	}
}

func TestPipeline_AcceptsDataGapTemplateWithoutUrgency(t *testing.T) {
	p := New(&stubScreen{})
	err := p.ValidateRefusalTemplate("P_GAP", "data_gap",
		"I do not have enough information to answer.")
	if err != nil {
		t.Errorf("data_gap template should not require urgency keyword; got %v", err)
	}
}

func TestPipeline_RejectsClinicianFieldAdminWrite(t *testing.T) {
	p := New(&stubScreen{})
	diffs := []FieldDiff{
		{Path: "guards[0].priority", OldValue: 5, NewValue: 1},
	}
	err := p.ValidateAdminDiff(diffs, "admin")
	if err != ErrClinicianFieldImmutable {
		t.Errorf("expected ErrClinicianFieldImmutable; got %v", err)
	}
}

func TestPipeline_AllowsClinicianFieldClinicianWrite(t *testing.T) {
	p := New(&stubScreen{})
	diffs := []FieldDiff{
		{Path: "guards[0].priority", OldValue: 5, NewValue: 1},
	}
	err := p.ValidateAdminDiff(diffs, "clinician")
	if err != nil {
		t.Errorf("clinician role must pass; got %v", err)
	}
}

func TestPipeline_AllowsNonClinicianFieldAdminWrite(t *testing.T) {
	p := New(&stubScreen{})
	diffs := []FieldDiff{
		{Path: "prompt_overlay.identity", OldValue: "old", NewValue: "new"},
	}
	err := p.ValidateAdminDiff(diffs, "admin")
	if err != nil {
		t.Errorf("admin can change prompt_overlay; got %v", err)
	}
}
