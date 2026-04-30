// task.go — task module.
//
// exports: NewTask
// used_by: none
// rules:   none
// agent:   codedna-cli (no-llm) | codedna-cli | 2026-04-30 | codedna-cli | initial CodeDNA annotation pass
// message: 

package jobqueue

import (
	"github.com/hibiken/asynq"
)

// NewTask wraps payload in an Envelope (tenant_id + version) and returns an
// asynq.Task plus the SDK's default options. Callers append their own options
// — Asynq's option list is processed left-to-right with later values winning,
// so SDK defaults come first.
//
// taskType is the canonical task name, e.g. "solid-sync:fhir-push".
// tenantID must be a valid UUID; NewTask returns ErrInvalidEnvelope (wrapped)
// otherwise.
func NewTask(taskType, tenantID string, payload any, opts ...asynq.Option) (*asynq.Task, []asynq.Option, error) {
	body, err := Wrap(tenantID, payload)
	if err != nil {
		return nil, nil, err
	}
	merged := append(defaultOptions(), opts...)
	return asynq.NewTask(taskType, body), merged, nil
}

// defaultOptions returns the SDK-injected option set. Order matters: caller-
// supplied options must be appended after these so they override.
func defaultOptions() []asynq.Option {
	return []asynq.Option{
		asynq.MaxRetry(DefaultMaxRetry),
		asynq.Timeout(DefaultTaskTimeout),
		asynq.Retention(DefaultRetention),
	}
}
