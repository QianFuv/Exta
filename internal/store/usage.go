package store

import (
	"context"
	"database/sql"
	"time"

	"exta/internal/domain"
)

// UsageCredential pins the current credential for an explicit read-only usage query.
// A dedicated service key must be disabled for retrieval to prevent accidental pooling.
func (state *Store) UsageCredential(ctx context.Context, keyID string, dedicated bool) (string, error) {
	var version, data string
	if err := state.DB.QueryRowContext(ctx, "SELECT active_version,data FROM provider_keys WHERE id=?", keyID).Scan(&version, &data); err != nil {
		return "", Missing(err)
	}
	key, err := Decode[domain.UpstreamKey](data)
	if err != nil {
		return "", err
	}
	if key.RetirementState != "active" || dedicated && (key.Enabled || key.Provider != "exa") || !dedicated && !key.Enabled {
		return "", Reject("provider_unavailable", 503, "Usage credential is not configured for this purpose")
	}
	return state.Credential(ctx, version)
}

// ClaimUsageRefresh persists a minimum key-association cooldown before any upstream call.
func (state *Store) ClaimUsageRefresh(ctx context.Context, keyIDs []string) (time.Time, error) {
	now := state.Now()
	next := now.Add(2 * time.Minute)
	err := state.Write(ctx, func(tx *sql.Tx) error {
		seen := map[string]bool{}
		for _, keyID := range keyIDs {
			var id, provider string
			if err := tx.QueryRowContext(ctx, "SELECT association_ref,provider FROM provider_keys WHERE id=?", keyID).Scan(&id, &provider); err != nil {
				return Missing(err)
			}
			if seen[id] {
				continue
			}
			seen[id] = true
			var until int64
			err := tx.QueryRowContext(ctx, "SELECT coalesce(max(window_ms),0) FROM rate_windows WHERE (scope='usage' AND entity_id=?) OR (scope='usage_provider' AND entity_id=?)", id, provider).Scan(&until)
			if err != nil {
				return err
			}
			if until > now.UnixMilli() {
				fault := Reject("local_rate_limited", 429, "Usage refresh is cooling down for this key association")
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
func (state *Store) ExtendUsageCooldown(ctx context.Context, associationID string, duration time.Duration) (time.Time, error) {
	return state.extendUsageCooldown(ctx, "usage", associationID, duration)
}

// ExtendProviderUsageCooldown keeps unknown-scope limits shared across provider keys.
func (state *Store) ExtendProviderUsageCooldown(ctx context.Context, provider string, duration time.Duration) (time.Time, error) {
	return state.extendUsageCooldown(ctx, "usage_provider", provider, duration)
}

// ProviderUsageDeadline checks the shared endpoint before each key dispatch.
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
func (state *Store) SaveUsage(ctx context.Context, items []domain.KeyUsage) error {
	return state.Write(ctx, func(tx *sql.Tx) error {
		for _, item := range items {
			if err := domain.ValidateJSON("KeyUsage", []byte(JSON(item))); err != nil {
				return err
			}
			if item.KeyId == nil && item.Used == nil {
				var known int
				if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM usage_snapshots WHERE association_ref=? AND key_id IS NULL AND json_extract(data,'$.used') IS NOT NULL", item.AssociationRef).Scan(&known); err != nil {
					return err
				}
				if known > 0 {
					continue
				}
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO usage_snapshots VALUES(?,?,?,?,?)", ID("usage"), item.AssociationRef, item.KeyId, Stamp(state.Now()), JSON(item)); err != nil {
				return err
			}
		}
		return nil
	})
}

// Usage keeps configured local safety budgets separate from upstream observations.
func (state *Store) Usage(ctx context.Context) ([]domain.KeyUsage, error) {
	keys, err := state.Keys(ctx)
	if err != nil {
		return nil, err
	}
	items := []domain.KeyUsage{}
	err = state.Write(ctx, func(tx *sql.Tx) error {
		seen := map[string]bool{}
		for _, key := range keys {
			used, held, err := KeyBalanceTx(ctx, tx, key.Id)
			if err != nil {
				return err
			}
			observed, spent, limit := Stamp(state.Now()), domain.Integer(used), key.QuotaPolicy.Allocation
			remaining := max(domain.Integer(0), limit-spent-domain.Integer(held))
			source := "local"
			var estimates int
			if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM usage_ledger WHERE key_id=? AND certainty='estimated'", key.Id).Scan(&estimates); err != nil {
				return err
			}
			if estimates > 0 {
				source = "estimate"
			}
			item := domain.KeyUsage{AssociationRef: key.AssociationRef, AssociationEvidence: key.AssociationEvidence, Scope: "key", KeyId: &key.Id, Provider: key.Provider, Source: source, ObservedAt: &observed, Freshness: "fresh", Unit: key.QuotaPolicy.Unit, Used: &spent, Reserved: domain.Integer(held), LimitKind: "finite", Limit: &limit, Remaining: &remaining}
			if limit == 0 {
				item.LimitKind, item.Limit, item.Remaining = "unlimited", nil, nil
			}
			items = append(items, item)
			if key.SharedLimit != nil && !seen[key.AssociationRef] {
				seen[key.AssociationRef] = true
				used, held, err = BalanceTx(ctx, tx, key.AssociationRef)
				if err != nil {
					return err
				}
				sharedSpent, sharedLimit := domain.Integer(used), key.SharedLimit.Allocation
				sharedRemaining := max(domain.Integer(0), sharedLimit-sharedSpent-domain.Integer(held))
				shared := domain.KeyUsage{AssociationRef: key.AssociationRef, AssociationEvidence: key.AssociationEvidence, Scope: "shared", Provider: key.Provider, Source: "local", ObservedAt: &observed, Freshness: "fresh", Unit: key.QuotaPolicy.Unit, Used: &sharedSpent, Reserved: domain.Integer(held), LimitKind: "finite", Limit: &sharedLimit, Remaining: &sharedRemaining}
				if sharedLimit == 0 {
					shared.LimitKind, shared.Limit, shared.Remaining = "unlimited", nil, nil
				}
				items = append(items, shared)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	rows, err := state.DB.QueryContext(ctx, `SELECT data FROM usage_snapshots current WHERE id=(SELECT id FROM usage_snapshots WHERE association_ref=current.association_ref AND key_id IS current.key_id ORDER BY observed_at DESC,rowid DESC LIMIT 1) ORDER BY association_ref,key_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seenKeys := map[string]bool{}
	for rows.Next() {
		var data string
		if err = rows.Scan(&data); err != nil {
			return nil, err
		}
		item, err := Decode[domain.KeyUsage](data)
		if err != nil {
			return nil, err
		}
		if item.KeyId != nil {
			seenKeys[*item.KeyId] = true
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
	for _, key := range keys {
		if !seenKeys[key.Id] {
			reason := "not_refreshed"
			items = append(items, domain.KeyUsage{AssociationRef: key.AssociationRef, AssociationEvidence: key.AssociationEvidence, Scope: "key", KeyId: &key.Id, Provider: key.Provider, Source: "upstream", Unit: key.QuotaPolicy.Unit, Freshness: "unknown", LimitKind: "unknown", UnavailableReason: &reason})
		}
	}
	return items, nil
}
