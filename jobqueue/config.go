package jobqueue

import (
	"fmt"
	"time"
)

// Defaults applied by the SDK to every enqueued task unless overridden by the
// caller. These mirror the per-language values in the job-queue spec
// (docs/superpowers/specs/2026-04-29-job-queue-design.md § Error Handling).
const (
	DefaultMaxRetry     = 2
	DefaultTaskTimeout  = 5 * time.Minute
	DefaultRetention    = 48 * time.Hour
	DefaultConcurrency  = 4
	DefaultArchiveLimit = 10000
)

// Config configures the Asynq client, server, scheduler, and inspector
// constructors. One Config per service — the QueueName must match the queue
// the service's worker reads from (Asynq routes by queue name, not task type).
type Config struct {
	// Addr is the Valkey/Redis address (host:port).
	Addr string

	// Password is the Valkey/Redis password. Empty string is rejected by
	// validate() — prod and dev both require a password.
	Password string

	// QueueName is the Asynq queue this service reads/writes. Spec § Per-Service
	// Workers fixes one queue per service: "solid-sync", "reflex", "pulse",
	// "marrow", "frontal".
	QueueName string

	// Concurrency is the number of in-flight tasks per worker process.
	// Defaults to DefaultConcurrency when zero.
	Concurrency int

	// ShutdownTimeout caps how long Server.Shutdown waits for in-flight tasks
	// to finish. Zero falls back to Asynq's internal default (8s).
	ShutdownTimeout time.Duration
}

func (c Config) validate() error {
	if c.Addr == "" {
		return fmt.Errorf("jobqueue: Addr is required")
	}
	if c.Password == "" {
		return fmt.Errorf("jobqueue: Password is required (no empty-password dev path)")
	}
	if c.QueueName == "" {
		return fmt.Errorf("jobqueue: QueueName is required")
	}
	return nil
}

func (c Config) concurrency() int {
	if c.Concurrency <= 0 {
		return DefaultConcurrency
	}
	return c.Concurrency
}
