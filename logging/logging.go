// logging.go — logging module.
//
// exports: Init | FromContext
// used_by: none
// rules:   none
// agent:   batch-log-structuring | claude | 2026-05-02 | claude | env-driven level/format + ctx helper
// message: structured slog with LOG_LEVEL/LOG_FORMAT env + request_id-from-ctx helper

// Package logging provides structured logging for Omur Go services using slog.
//
// Default output is JSON, suitable for production log aggregation. Set
// LOG_FORMAT=text (or =console) to switch to the human-readable handler
// during local development. LOG_LEVEL accepts debug/info/warn/error.
package logging

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"github.com/omurlabs/omur-core/packages/omur-go-sdk/requestid"
)

// Init configures the default slog logger and returns it. Reads LOG_LEVEL
// and LOG_FORMAT from the environment so operators can tune verbosity and
// renderer without a redeploy.
func Init(service string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: levelFromEnv()}

	var handler slog.Handler
	if formatFromEnv() == "text" {
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}

	logger := slog.New(handler).With("service", service)
	slog.SetDefault(logger)
	return logger
}

// FromContext returns the default logger augmented with the request_id
// stored in ctx (if any). Hot-path callers should prefer
// `logging.FromContext(ctx).Info(...)` over `slog.Default().Info(...)` so
// every log record on the request goroutine carries the correlation id.
//
// Cheap when no id is present (single map lookup, no allocation in
// the empty-string fast path of slog.With).
func FromContext(ctx context.Context) *slog.Logger {
	logger := slog.Default()
	if ctx == nil {
		return logger
	}
	if id := requestid.FromContext(ctx); id != "" {
		return logger.With("request_id", id)
	}
	return logger
}

// levelFromEnv parses LOG_LEVEL into a slog.Level. Defaults to Info on
// missing or malformed input — services start chatty enough by default
// that silent debug-mode regressions are worse than ignoring junk.
func levelFromEnv() slog.Level {
	switch strings.ToLower(os.Getenv("LOG_LEVEL")) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// formatFromEnv normalises LOG_FORMAT. Returns "json" or "text"; any other
// value (including empty) maps to "json" so production stays structured.
func formatFromEnv() string {
	switch strings.ToLower(os.Getenv("LOG_FORMAT")) {
	case "text", "console":
		return "text"
	default:
		return "json"
	}
}
