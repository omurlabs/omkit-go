// logging_test.go — tests for logging module.
//
// exports: TestInit_DefaultsToJSON | TestInit_LogFormatText | TestInit_LogLevel | TestFromContext_AddsRequestID
// used_by: none
// rules:   none
// agent:   batch-log-structuring | claude | 2026-05-02 | claude | tests for env parsing + ctx helper
// message:

package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/omurlabs/omur-core/packages/omur-go-sdk/requestid"
)

func captureWith(t *testing.T, env map[string]string, fn func(*slog.Logger)) string {
	t.Helper()
	for k, v := range env {
		t.Setenv(k, v)
	}
	var buf bytes.Buffer
	opts := &slog.HandlerOptions{Level: levelFromEnv()}
	var handler slog.Handler
	if formatFromEnv() == "text" {
		handler = slog.NewTextHandler(&buf, opts)
	} else {
		handler = slog.NewJSONHandler(&buf, opts)
	}
	logger := slog.New(handler).With("service", "test-svc")
	prevDefault := slog.Default()
	slog.SetDefault(logger)
	t.Cleanup(func() { slog.SetDefault(prevDefault) })
	fn(logger)
	return buf.String()
}

func TestInit_DefaultsToJSON(t *testing.T) {
	out := captureWith(t, map[string]string{"LOG_LEVEL": "", "LOG_FORMAT": ""}, func(l *slog.Logger) {
		l.Info("hello", "user", "vadim")
	})
	var payload map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &payload); err != nil {
		t.Fatalf("default output must be JSON: %v\nraw=%q", err, out)
	}
	if payload["service"] != "test-svc" {
		t.Fatalf("service field missing: %v", payload)
	}
	if payload["msg"] != "hello" {
		t.Fatalf("msg field missing: %v", payload)
	}
}

func TestInit_LogFormatText(t *testing.T) {
	out := captureWith(t, map[string]string{"LOG_FORMAT": "text"}, func(l *slog.Logger) {
		l.Info("hello")
	})
	if json.Valid([]byte(strings.TrimSpace(out))) {
		t.Fatalf("LOG_FORMAT=text must produce non-JSON; got %q", out)
	}
	if !strings.Contains(out, "hello") {
		t.Fatalf("expected msg in text output, got %q", out)
	}
}

func TestInit_LogLevel(t *testing.T) {
	cases := []struct {
		env     string
		emit    func(*slog.Logger)
		seeMsg  bool
		message string
	}{
		{"debug", func(l *slog.Logger) { l.Debug("dbg") }, true, "dbg"},
		{"info", func(l *slog.Logger) { l.Debug("dbg") }, false, "dbg"},
		{"warn", func(l *slog.Logger) { l.Info("inf") }, false, "inf"},
		{"error", func(l *slog.Logger) { l.Warn("wrn") }, false, "wrn"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.env, func(t *testing.T) {
			out := captureWith(t, map[string]string{"LOG_LEVEL": tc.env}, tc.emit)
			gotMsg := strings.Contains(out, tc.message)
			if gotMsg != tc.seeMsg {
				t.Fatalf("LOG_LEVEL=%s see %q want=%v got=%v out=%q", tc.env, tc.message, tc.seeMsg, gotMsg, out)
			}
		})
	}
}

func TestFromContext_AddsRequestID(t *testing.T) {
	out := captureWith(t, nil, func(_ *slog.Logger) {
		ctx := requestid.NewContext(context.Background(), "req-123")
		FromContext(ctx).Info("hello")
	})
	var payload map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &payload); err != nil {
		t.Fatalf("output not JSON: %v\nraw=%q", err, out)
	}
	if payload["request_id"] != "req-123" {
		t.Fatalf("request_id missing/wrong: %v", payload)
	}
}

func TestFromContext_NilCtxIsSafe(t *testing.T) {
	out := captureWith(t, nil, func(_ *slog.Logger) {
		//nolint:staticcheck // intentionally testing nil ctx tolerance
		FromContext(nil).Info("hello")
	})
	if !strings.Contains(out, "hello") {
		t.Fatalf("expected log line, got %q", out)
	}
}

func TestFromContext_NoRequestID(t *testing.T) {
	out := captureWith(t, nil, func(_ *slog.Logger) {
		FromContext(context.Background()).Info("hello")
	})
	if strings.Contains(out, "request_id") {
		t.Fatalf("request_id should not appear when ctx empty, got %q", out)
	}
}
