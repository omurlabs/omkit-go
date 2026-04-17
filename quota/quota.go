// Package quota enforces per-tenant resource limits (plan 1.7).
// Limits are read from public.tenant_quotas; absence of a row means
// "use the defaults below". Usage is counted from the canonical sources:
//
//	docs    -> COUNT(*) FROM document_files
//	bytes   -> COALESCE(SUM(size_bytes), 0) FROM document_files
//	queries -> COUNT(*) FROM usage_log WHERE created_at >= date_trunc('month', now())
//
// All reads go through dbpool.WithTenant so RLS stays enforced.
package quota

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/omurlabs/omur-core/packages/omur-go-sdk/dbpool"
)

// Resource names the quota dimension being checked.
type Resource string

const (
	ResourceDocs    Resource = "docs"
	ResourceStorage Resource = "storage_bytes"
	ResourceQueries Resource = "queries_per_month"
)

// Defaults mirror stage-1.md §1.7.
const (
	DefaultDocs            = 100
	DefaultStorageBytes    = int64(500 * 1024 * 1024) // 500 MiB
	DefaultQueriesPerMonth = 1000
)

// Limits holds the effective hard caps for a tenant.
type Limits struct {
	Docs            int
	StorageBytes    int64
	QueriesPerMonth int
}

// Usage holds current consumption counters for a tenant.
type Usage struct {
	Docs             int
	StorageBytes     int64
	QueriesThisMonth int
}

// Decision is what the middleware / upload handler acts on.
type Decision struct {
	Allowed    bool
	Resource   Resource
	Limit      int64
	Used       int64
	RetryAfter int // seconds; 0 means "no retry will help" (e.g. storage full)
}

// Load returns the tenant's effective limits, falling back to defaults when
// the tenant has no row in tenant_quotas (the common case for new tenants).
func Load(ctx context.Context, pool *pgxpool.Pool, tenantID string) (Limits, error) {
	lim := Limits{
		Docs:            DefaultDocs,
		StorageBytes:    DefaultStorageBytes,
		QueriesPerMonth: DefaultQueriesPerMonth,
	}
	if pool == nil {
		return lim, nil
	}
	err := dbpool.WithTenant(ctx, pool, tenantID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT docs_limit, storage_bytes_limit, queries_per_month_limit
			  FROM tenant_quotas
			 WHERE tenant_id = $1::uuid
		`, tenantID).Scan(&lim.Docs, &lim.StorageBytes, &lim.QueriesPerMonth)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return lim, nil
	}
	if err != nil {
		return lim, fmt.Errorf("quota.Load: %w", err)
	}
	return lim, nil
}

// GetUsage reads the three counters under RLS for the request tenant.
// The size_bytes column on document_files is added in Task 4; until that
// migration runs, this call will return an error at runtime — callers must
// treat that gracefully.
func GetUsage(ctx context.Context, pool *pgxpool.Pool, tenantID string) (Usage, error) {
	var u Usage
	if pool == nil {
		return u, nil
	}
	err := dbpool.WithTenant(ctx, pool, tenantID, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			SELECT COUNT(*)::int, COALESCE(SUM(size_bytes), 0)::bigint
			  FROM document_files
		`).Scan(&u.Docs, &u.StorageBytes); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `
			SELECT COUNT(*)::int FROM usage_log
			 WHERE created_at >= date_trunc('month', now())
		`).Scan(&u.QueriesThisMonth)
	})
	if err != nil {
		return u, fmt.Errorf("quota.GetUsage: %w", err)
	}
	return u, nil
}

// CheckUpload pre-flights a single document upload of incomingBytes.
// Docs limit is checked first; storage limit is checked second.
// Returns Decision{Allowed: true} when the upload can proceed.
func CheckUpload(lim Limits, usage Usage, incomingBytes int64) (Decision, error) {
	if usage.Docs+1 > lim.Docs {
		return Decision{
			Allowed:    false,
			Resource:   ResourceDocs,
			Limit:      int64(lim.Docs),
			Used:       int64(usage.Docs),
			RetryAfter: 0,
		}, nil
	}
	if usage.StorageBytes+incomingBytes > lim.StorageBytes {
		return Decision{
			Allowed:    false,
			Resource:   ResourceStorage,
			Limit:      lim.StorageBytes,
			Used:       usage.StorageBytes,
			RetryAfter: 0,
		}, nil
	}
	return Decision{Allowed: true}, nil
}

// CheckQuery pre-flights a single query against the monthly budget.
// Monthly resets happen implicitly because the counter is date_trunc('month').
// RetryAfter is set to seconds until the first day of next month UTC.
func CheckQuery(lim Limits, usage Usage) (Decision, error) {
	if usage.QueriesThisMonth+1 > lim.QueriesPerMonth {
		return Decision{
			Allowed:    false,
			Resource:   ResourceQueries,
			Limit:      int64(lim.QueriesPerMonth),
			Used:       int64(usage.QueriesThisMonth),
			RetryAfter: secondsUntilNextMonth(),
		}, nil
	}
	return Decision{Allowed: true}, nil
}

func secondsUntilNextMonth() int {
	return capAt32Days(int(firstOfNextMonth().Sub(nowUTC()).Seconds()))
}
