package featureflags

import "testing"

func TestValidateRoles_AllKnown(t *testing.T) {
	if bad := ValidateRoles([]string{"admin", "support", "user"}); bad != "" {
		t.Fatalf("expected \"\", got %q", bad)
	}
}

func TestValidateRoles_UnknownReturnsFirst(t *testing.T) {
	if bad := ValidateRoles([]string{"admin", "superuser", "user"}); bad != "superuser" {
		t.Fatalf("expected \"superuser\", got %q", bad)
	}
}

func TestValidateRoles_Empty_Valid(t *testing.T) {
	if bad := ValidateRoles(nil); bad != "" {
		t.Fatalf("empty input should be valid, got %q", bad)
	}
	if bad := ValidateRoles([]string{}); bad != "" {
		t.Fatalf("empty slice should be valid, got %q", bad)
	}
}
