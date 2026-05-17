// record.go — record module.
//
// exports: RecordUsage
// rules:   none
// agent:   codedna-cli (no-llm) | codedna-cli | 2026-04-30 | codedna-cli | initial CodeDNA annotation pass
// message: 

package quota

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/omurlabs/omkit-go/dbpool"
)

// RecordUsage inserts a row into usage_log so queries_per_month quota counters
// advance. It is the write side of the quota contract — quota enforcement at
// Spine's quotaMiddleware reads usage_log; without this write, the enforcement
// is silently a no-op. Callers should invoke this after any successful LLM
// completion (chat, embed, tool call) on behalf of a tenant.
//
// tenantID must be a valid UUID string; empty tenantID is a no-op rather than
// an error, so fire-and-forget callers (e.g. goroutines on the response path)
// don't need to branch. pool may be nil in dev/test — also a no-op.
//
// costUSD may be zero when the provider does not return a price (e.g. local
// Ollama). Writes happen under omur_app + SET LOCAL app.tenant_id so RLS is
// enforced; a compromised tenant context cannot poison another tenant's counter.
func RecordUsage(
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID, provider, model string,
	inputTokens, outputTokens int,
	costUSD float64,
) error {
	if pool == nil || tenantID == "" {
		return nil
	}
	err := dbpool.WithTenant(ctx, pool, tenantID, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `
			INSERT INTO usage_log
				(tenant_id, provider, model, input_tokens, output_tokens, cost_usd)
			VALUES
				($1::uuid, $2, $3, $4, $5, $6)
		`, tenantID, provider, model, inputTokens, outputTokens, costUSD)
		return e
	})
	if err != nil {
		return fmt.Errorf("quota.RecordUsage: %w", err)
	}
	return nil
}
