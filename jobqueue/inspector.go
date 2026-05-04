// inspector.go — inspector module.
//
// exports: NewInspector
// rules:   none
// agent:   codedna-cli (no-llm) | codedna-cli | 2026-04-30 | codedna-cli | initial CodeDNA annotation pass
// message: 

package jobqueue

import "github.com/hibiken/asynq"

// NewInspector returns an asynq.Inspector for queue-admin operations
// (list/run/delete archived tasks). Service DLQ HTTP handlers wrap this.
//
// The inspector owns its own Redis connection pool — close on shutdown.
func NewInspector(cfg Config) (*asynq.Inspector, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return asynq.NewInspector(asynq.RedisClientOpt{
		Addr:     cfg.Addr,
		Password: cfg.Password,
	}), nil
}
