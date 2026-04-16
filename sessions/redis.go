package sessions

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisConfig configures the Redis/Valkey-backed session store.
type RedisConfig struct {
	Addr      string
	Password  string
	DB        int
	KeyPrefix string // e.g. "omur:session:"
}

type redisStore struct {
	rdb    *redis.Client
	prefix string
}

// NewRedisStore returns a Store backed by Redis/Valkey. List is implemented via
// SCAN and a client-side tenant filter; prefer PostgresSessionStore when
// tenant-scoped listing matters.
func NewRedisStore(cfg RedisConfig) (Store, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		_ = rdb.Close()
		return nil, err
	}
	prefix := cfg.KeyPrefix
	if prefix == "" {
		prefix = "omur:session:"
	}
	return &redisStore{rdb: rdb, prefix: prefix}, nil
}

func (r *redisStore) key(token string) string { return r.prefix + token }

func (r *redisStore) Get(ctx context.Context, token string) (*Session, error) {
	b, err := r.rdb.Get(ctx, r.key(token)).Bytes()
	if err == redis.Nil {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var s Session
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *redisStore) Put(ctx context.Context, s *Session) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	ttl := time.Until(s.ExpiresAt)
	if ttl < 0 {
		ttl = time.Second
	}
	return r.rdb.Set(ctx, r.key(s.Token), b, ttl).Err()
}

func (r *redisStore) Delete(ctx context.Context, token string) error {
	return r.rdb.Del(ctx, r.key(token)).Err()
}

func (r *redisStore) List(ctx context.Context, tenantID string) ([]*Session, error) {
	iter := r.rdb.Scan(ctx, 0, r.prefix+"*", 100).Iterator()
	var out []*Session
	for iter.Next(ctx) {
		b, err := r.rdb.Get(ctx, iter.Val()).Bytes()
		if err != nil {
			continue
		}
		var s Session
		if err := json.Unmarshal(b, &s); err != nil {
			continue
		}
		if s.TenantID == tenantID {
			out = append(out, &s)
		}
	}
	return out, iter.Err()
}

func (r *redisStore) Close() error { return r.rdb.Close() }
