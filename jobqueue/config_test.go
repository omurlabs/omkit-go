package jobqueue

import "testing"

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{"missing addr", Config{Password: "p", QueueName: "q"}, "Addr is required"},
		{"missing password", Config{Addr: "a", QueueName: "q"}, "Password is required"},
		{"missing queue", Config{Addr: "a", Password: "p"}, "QueueName is required"},
		{"ok", Config{Addr: "a", Password: "p", QueueName: "q"}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Errorf("validate: %v", err)
				}
				return
			}
			if err == nil || !contains(err.Error(), tc.wantErr) {
				t.Errorf("err = %v, want substring %q", err, tc.wantErr)
			}
		})
	}
}

func TestConcurrencyDefaults(t *testing.T) {
	if (Config{}).concurrency() != DefaultConcurrency {
		t.Error("zero concurrency must fall back to default")
	}
	if (Config{Concurrency: 9}).concurrency() != 9 {
		t.Error("non-zero concurrency must pass through")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
