// Package logging provides structured logging for Omur Go services using slog.
package logging

import (
	"log/slog"
	"os"
)

// Init configures the default slog logger with JSON output and service name.
func Init(service string) *slog.Logger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	logger := slog.New(handler).With("service", service)
	slog.SetDefault(logger)
	return logger
}
