package store

import (
	"context"
)

// Available reports current configured eligibility without consuming admission resources.
func (state *Store) Available(ctx context.Context, provider string, minimumAmount int64) (bool, error) {
	if state.Vault == nil {
		return false, nil
	}
	var count int
	now := state.Now().UnixMilli()
	err := state.DB.QueryRowContext(ctx, `SELECT count(*) FROM provider_keys k
JOIN provider_accounts a ON a.id=k.account_id
JOIN credential_versions c ON c.id=k.active_version
JOIN runtime_gates ag ON ag.scope='account' AND ag.entity_id=a.id
JOIN runtime_gates kg ON kg.scope='key' AND kg.entity_id=k.id
WHERE a.provider=? AND json_extract(a.data,'$.enabled')=1 AND json_extract(k.data,'$.enabled')=1
AND json_extract(k.data,'$.retirement_state')='active' AND c.validity<>'invalid' AND kg.validity<>'invalid'
AND ag.blocked=0 AND kg.blocked=0 AND ag.cooldown_ms<=? AND kg.cooldown_ms<=?
AND ag.breaker_ms<=? AND kg.breaker_ms<=? AND ag.half_open<=? AND kg.half_open<=?
AND NOT EXISTS(SELECT 1 FROM runtime_gates WHERE scope='provider' AND entity_id=? AND cooldown_ms>?)
AND json_extract(a.data,'$.quota_policy.per_operation_reservation') > 0
AND json_extract(a.data,'$.quota_policy.allocation') >=
coalesce((SELECT sum(amount) FROM usage_ledger WHERE account_id=a.id),0)+coalesce((SELECT sum(amount) FROM budget_reservations WHERE account_id=a.id AND state='held'),0)+max(json_extract(a.data,'$.quota_policy.per_operation_reservation'),?)`, provider, now, now, now, now, now, now, provider, now, minimumAmount).Scan(&count)
	return count > 0, err
}
