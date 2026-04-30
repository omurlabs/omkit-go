// scheduler.go — scheduler module.
//
// exports: NewScheduler
// used_by: none
// rules:   none
// agent:   codedna-cli (no-llm) | codedna-cli | 2026-04-30 | codedna-cli | initial CodeDNA annotation pass
// message: 

package jobqueue

import "github.com/hibiken/asynq"

// NewScheduler returns an asynq.Scheduler bound to cfg's Redis. Caller
// registers cron entries via Scheduler.Register and starts the scheduler
// alongside the asynq.Server.
//
// Asynq Scheduler uses Redis SETNX for cross-replica safety. Spec note
// (§ Error Handling): the lock TTL must be ≤ cron_interval/2 — Asynq's
// default is fine for the cron schedules in this codebase (smallest is
// 30 minutes for marrow:reindex). Re-evaluate if a sub-minute schedule
// is added.
func NewScheduler(cfg Config) (*asynq.Scheduler, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return asynq.NewScheduler(
		asynq.RedisClientOpt{
			Addr:     cfg.Addr,
			Password: cfg.Password,
		},
		nil,
	), nil
}
