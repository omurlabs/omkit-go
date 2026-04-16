package config_test

import (
	"os"
	"testing"

	"github.com/omurlabs/omur-core/packages/omur-go-sdk/config"
)

func TestEnvInt64_FallsBackOnEmpty(t *testing.T) {
	os.Unsetenv("OMUR_TEST_INT64")
	if got := config.EnvInt64("OMUR_TEST_INT64", 12345); got != 12345 {
		t.Errorf("got %d, want 12345", got)
	}
}

func TestEnvInt64_ParsesValue(t *testing.T) {
	t.Setenv("OMUR_TEST_INT64", "104857600")
	if got := config.EnvInt64("OMUR_TEST_INT64", 0); got != 104857600 {
		t.Errorf("got %d, want 104857600", got)
	}
}

func TestEnvInt64_FallsBackOnInvalid(t *testing.T) {
	t.Setenv("OMUR_TEST_INT64", "not-a-number")
	if got := config.EnvInt64("OMUR_TEST_INT64", 42); got != 42 {
		t.Errorf("got %d, want 42 (fallback)", got)
	}
}
