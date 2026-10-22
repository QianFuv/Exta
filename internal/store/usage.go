package store

import (
	"context"
	"database/sql"
	"time"

	"exta/internal/domain"
)

// UsageCredential pins the current credential for an explicit read-only usage query.
// A dedicated service key must be disabled for retrieval to prevent accidental pooling.
func (state *Store) UsageCredential(ctx context.Context, accountID, keyID string, dedicated bool) (string, error) {
	var version, data string
	if err := state.DB.QueryRowContext(ctx, "SELECT active_version,data FROM provider_keys WHERE id=? AND account_id=?", keyID, accountID).Scan(&version, &data); err != nil {
		return "", Missing(err)
	}
	key, err := Decode[domain.ProviderKey](data)
	if err != nil {
		return "", err
	}
	if key.RetirementState != "active" || dedicated && key.Enabled || !dedicated && !key.Enabled {
		return "", Reject("provider_unavailable", 503, "Usage credential is not configured for this purpose")
	}
	return state.Credential(ctx, version)
}

// ClaimUsageRefresh persists a minimum account cooldown before any upstream call.
func (state *Store) ClaimUsageRefresh(ctx context.Context, accountIDs []string) (time.Time, error) {
	now := state.Now()
	next := now.Add(2 * time.Minute)
	err := state.Write(ctx, func(tx *sql.Tx) error {
		for _, id := range accountIDs {
			var until int64
			err := tx.QueryRowContext(ctx, "SELECT coalesce(max(window_ms),0) FROM rate_windows WHERE (scope='usage' AND entity_id=?) OR (scope='usage_provider' AND entity_id=(SELECT provider FROM provider_accounts WHERE id=?))", id, id).Scan(&until)
			if err != nil {
				return err
			}
			if until > now.UnixMilli() {
				fault := Reject("local_rate_limited", 429, "Usage refresh is cooling down for this account")
				fault.RetryAfterMs = until - now.UnixMilli()
				return fault
			}
			if _, err = tx.ExecContext(ctx, "INSERT INTO rate_windows VALUES('usage',?,?,0) ON CONFLICT(scope,entity_id) DO UPDATE SET window_ms=excluded.window_ms", id, next.UnixMilli()); err != nil {
				return err
			}
		}
		return nil
	})
	return next, err
}

// ExtendUsageCooldown preserves the longest deadline from actual dispatch or upstream hints.
func (state *Store) ExtendUsageCooldown(ctx context.Context, accountID string, duration time.Duration) (time.Time, error) {
	return state.extendUsageCooldown(ctx, "usage", accountID, duration)
}

// ExtendProviderUsageCooldown keeps unknown-scope limits shared across usage accounts.
func (state *Store) ExtendProviderUsageCooldown(ctx context.Context, provider string, duration time.Duration) (time.Time, error) {
	return state.extendUsageCooldown(ctx, "usage_provider", provider, duration)
}

// ProviderUsageDeadline checks the shared endpoint before each account dispatch.
func (state *Store) ProviderUsageDeadline(ctx context.Context, provider string) (time.Time, error) {
	var until int64
	err := state.DB.QueryRowContext(ctx, "SELECT coalesce(max(window_ms),0) FROM rate_windows WHERE scope='usage_provider' AND entity_id=?", provider).Scan(&until)
	return time.UnixMilli(until), err
}

func (state *Store) extendUsageCooldown(ctx context.Context, scope, id string, duration time.Duration) (time.Time, error) {
	until := state.Now().Add(max(2*time.Minute, duration)).UnixMilli()
	err := state.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "INSERT INTO rate_windows VALUES(?,?,?,0) ON CONFLICT(scope,entity_id) DO UPDATE SET window_ms=max(window_ms,excluded.window_ms)", scope, id, until); err != nil {
			return err
		}
		return tx.QueryRowContext(ctx, "SELECT window_ms FROM rate_windows WHERE scope=? AND entity_id=?", scope, id).Scan(&until)
	})
	return time.UnixMilli(until), err
}

// SaveUsage stores a complete observation without merging upstream totals into the local ledger.
func (state *Store) SaveUsage(ctx context.Context, items []domain.AccountUsage) error {
	return state.Write(ctx, func(tx *sql.Tx) error {
		for _, item := range items {
			if err := domain.ValidateJSON("AccountUsage", []byte(JSON(item))); err != nil {
				return err
			}
			if item.KeyId == nil && item.Used == nil {
				var known int
				if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM usage_snapshots WHERE account_id=? AND key_id IS NULL AND json_extract(data,'$.used') IS NOT NULL", item.AccountId).Scan(&known); err != nil {
					return err
				}
				if known > 0 {
					continue
				}
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO usage_snapshots VALUES(?,?,?,?,?)", ID("usage"), item.AccountId, item.KeyId, Stamp(state.Now()), JSON(item)); err != nil {
				return err
			}
		}
		return nil
	})
}

// Usage reports local admission balances alongside the latest separate upstream observations.
func (state *Store) Usage(ctx context.Context) ([]domain.AccountUsage, error) {
	accounts, err := state.Accounts(ctx)
	if err != nil {
		return nil, err
	}
	items := []domain.AccountUsage{}
	err = state.Write(ctx, func(tx *sql.Tx) error {
		for _, account := range accounts {
			used, held, err := BalanceTx(ctx, tx, account.Id)
			if err != nil {
				return err
			}
			observed, spent := Stamp(state.Now()), domain.Integer(used)
			limit := account.QuotaPolicy.Allocation
			remaining := max(domain.Integer(0), limit-spent-domain.Integer(held))
			source := "local"
			var estimates int
			if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM usage_ledger WHERE account_id=? AND certainty='estimated'", account.Id).Scan(&estimates); err != nil {
				return err
			}
			if estimates > 0 {
				source = "estimate"
			}
			items = append(items, domain.AccountUsage{AccountId: account.Id, Provider: account.Provider, Source: source, ObservedAt: &observed, Freshness: "fresh", Unit: account.QuotaPolicy.Unit, Used: &spent, Reserved: domain.Integer(held), LimitKind: "finite", Limit: &limit, Remaining: &remaining})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	rows, err := state.DB.QueryContext(ctx, `SELECT data FROM usage_snapshots current WHERE id=(SELECT id FROM usage_snapshots WHERE account_id=current.account_id AND key_id IS current.key_id ORDER BY observed_at DESC,rowid DESC LIMIT 1) ORDER BY account_id,key_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seenAccounts := map[string]bool{}
	for rows.Next() {
		var data string
		if err = rows.Scan(&data); err != nil {
			return nil, err
		}
		item, err := Decode[domain.AccountUsage](data)
		if err != nil {
			return nil, err
		}
		if item.KeyId == nil {
			seenAccounts[item.AccountId] = true
		}
		if item.ObservedAt != nil && item.Freshness == "fresh" {
			observed, err := time.Parse(time.RFC3339Nano, *item.ObservedAt)
			if err != nil || state.Now().Sub(observed) > 5*time.Minute || observed.After(state.Now().Add(time.Minute)) {
				item.Freshness = "stale"
			}
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	for _, account := range accounts {
		if !seenAccounts[account.Id] {
			reason := "not_refreshed"
			if account.Provider == "exa" {
				reason = "account_total_unavailable"
			}
			items = append(items, domain.AccountUsage{AccountId: account.Id, Provider: account.Provider, Source: "upstream", Unit: account.QuotaPolicy.Unit, Freshness: "unknown", LimitKind: "unknown", UnavailableReason: &reason})
		}
	}
	return items, nil
}
