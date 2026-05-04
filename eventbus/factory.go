// factory.go — factory module.
//
// exports: Backend | BackendPostgres | BackendRedis | BackendFromEnv | New
// rules:   none
// agent:   codedna-cli (no-llm) | codedna-cli | 2026-04-30 | codedna-cli | initial CodeDNA annotation pass
// message: 

package eventbus

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Backend selects which Bus implementation New returns.
type Backend string

const (
	BackendPostgres Backend = "postgres"
	BackendRedis    Backend = "redis"
)

// BackendFromEnv returns the configured event bus backend, defaulting to postgres.
func BackendFromEnv() (Backend, error) {
	v := os.Getenv("OMUR_EVENTBUS_BACKEND")
	if v == "" {
		return BackendPostgres, nil
	}
	switch Backend(v) {
	case BackendPostgres, BackendRedis:
		return Backend(v), nil
	}
	return "", fmt.Errorf("eventbus: unknown backend %q", v)
}

// New constructs a Bus from the env-selected backend. Postgres requires a
// non-nil pool; Redis reads VALKEY_HOST / VALKEY_PORT / VALKEY_PASSWORD.
// consumerName is used for tracking offset (postgres) and for the consumer
// group (redis, defaulted to consumerName itself).
func New(ctx context.Context, pool *pgxpool.Pool, consumerName string) (Bus, error) {
	b, err := BackendFromEnv()
	if err != nil {
		return nil, err
	}
	switch b {
	case BackendPostgres:
		if pool == nil {
			return nil, fmt.Errorf("eventbus: postgres backend requires non-nil pool")
		}
		return NewPostgresBus(pool, PostgresConfig{ConsumerName: consumerName}), nil
	case BackendRedis:
		host := os.Getenv("VALKEY_HOST")
		port := os.Getenv("VALKEY_PORT")
		if port == "" {
			port = "6379"
		}
		return NewRedisBus(RedisConfig{
			Addr:         host + ":" + port,
			Password:     os.Getenv("VALKEY_PASSWORD"),
			ConsumerName: consumerName,
			Group:        consumerName,
		})
	}
	return nil, fmt.Errorf("eventbus: unreachable")
}
