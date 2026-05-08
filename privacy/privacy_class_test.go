package privacy

import (
	"context"
	"testing"
)

func TestHeaderNameIsCanonicalCase(t *testing.T) {
	if HeaderName != "X-Omur-Privacy-Class" {
		t.Fatalf("HeaderName = %q, want X-Omur-Privacy-Class", HeaderName)
	}
}

func TestDefaultIsTenant(t *testing.T) {
	if got := DefaultPrivacyClass(); got != ClassTenant {
		t.Fatalf("DefaultPrivacyClass() = %q, want tenant", got)
	}
}

func TestParseRecognisesKnownValues(t *testing.T) {
	cases := map[string]PrivacyClass{
		"public":      ClassPublic,
		"tenant":      ClassTenant,
		"sensitive":   ClassSensitive,
		"PUBLIC":      ClassPublic,
		"Sensitive":   ClassSensitive,
		"  tenant  ":  ClassTenant,
	}
	for raw, want := range cases {
		if got := ParsePrivacyClass(raw); got != want {
			t.Errorf("ParsePrivacyClass(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestParseFallsBackToDefault(t *testing.T) {
	for _, raw := range []string{"", "   ", "internal", "bogus", "p-u-b-l-i-c"} {
		if got := ParsePrivacyClass(raw); got != ClassTenant {
			t.Errorf("ParsePrivacyClass(%q) = %q, want tenant fallback", raw, got)
		}
	}
}

func TestAllowsCloudOnlyBlocksSensitive(t *testing.T) {
	if !AllowsCloud(ClassPublic) {
		t.Error("AllowsCloud(public) = false, want true")
	}
	if !AllowsCloud(ClassTenant) {
		t.Error("AllowsCloud(tenant) = false, want true")
	}
	if AllowsCloud(ClassSensitive) {
		t.Error("AllowsCloud(sensitive) = true, want false")
	}
}

func TestStringConversionRoundTrips(t *testing.T) {
	if string(ClassSensitive) != "sensitive" {
		t.Fatalf("string(ClassSensitive) = %q, want sensitive", string(ClassSensitive))
	}
}

func TestFromContextDefaultsToTenant(t *testing.T) {
	if got := FromContext(context.Background()); got != ClassTenant {
		t.Fatalf("FromContext(empty) = %q, want tenant", got)
	}
}

func TestWithPrivacyClassRoundTrips(t *testing.T) {
	for _, c := range []PrivacyClass{ClassPublic, ClassTenant, ClassSensitive} {
		ctx := WithPrivacyClass(context.Background(), c)
		if got := FromContext(ctx); got != c {
			t.Errorf("FromContext after WithPrivacyClass(%q) = %q", c, got)
		}
	}
}

func TestFromContextEmptyValueFallsBackToDefault(t *testing.T) {
	ctx := WithPrivacyClass(context.Background(), PrivacyClass(""))
	if got := FromContext(ctx); got != ClassTenant {
		t.Fatalf("FromContext(empty class) = %q, want tenant", got)
	}
}
