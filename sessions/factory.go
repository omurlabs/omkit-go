// factory.go — factory module.
//
// exports: Backend | BackendPostgres | BackendRedis | BackendFromEnv | New
// rules:   none
// agent:   codedna-cli (no-llm) | codedna-cli | 2026-04-30 | codedna-cli | initial CodeDNA annotation pass
// message: 

package sessions

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Backend selects which Store implementation New returns.
type Backend string

const (
	BackendPostgres Backend = "postgres"
	BackendRedis    Backend = "redis"
)

// BackendFromEnv returns the configured session backend, defaulting to postgres.
func BackendFromEnv() (Backend, error) {
	v := os.Getenv("OMUR_SESSION_BACKEND")
	if v == "" {
		return BackendPostgres, nil
	}
	switch Backend(v) {
	case BackendPostgres, BackendRedis:
		return Backend(v), nil
	}
	return "", fmt.Errorf("sessions: unknown backend %q", v)
}

// New constructs a Store from the env-selected backend. For postgres the caller
// must pass a non-nil pool; for redis, connection details are read from
// VALKEY_HOST / VALKEY_PORT / VALKEY_PASSWORD (port defaults to 6379).
func New(ctx context.Context, pool *pgxpool.Pool) (Store, error) {
	b, err := BackendFromEnv()
	if err != nil {
		return nil, err
	}
	switch b {
	case BackendPostgres:
		if pool == nil {
			return nil, fmt.Errorf("sessions: postgres backend requires non-nil pool")
		}
		return NewPostgresStore(pool), nil
	case BackendRedis:
		return NewRedisStore(RedisConfig{
			Addr:     os.Getenv("VALKEY_HOST") + ":" + envOr("VALKEY_PORT", "6379"),
			Password: os.Getenv("VALKEY_PASSWORD"),
		})
	}
	return nil, fmt.Errorf("sessions: unreachable")
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
