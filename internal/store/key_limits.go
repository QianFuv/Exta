package store

import (
	"context"
	"database/sql"

	"exta/internal/domain"
)

type keyCandidate struct {
	Key        domain.UpstreamKey
	Credential string
}

func eligibleKeysTx(ctx context.Context, tx *sql.Tx, provider string, now int64) ([]keyCandidate, error) {
	rows, err := tx.QueryContext(ctx, `SELECT k.data,k.active_version FROM provider_keys k
JOIN credential_versions c ON c.id=k.active_version
JOIN runtime_gates kg ON kg.scope='key' AND kg.entity_id=k.id
JOIN runtime_gates sg ON sg.scope='association' AND sg.entity_id=k.association_ref
WHERE k.provider=? AND json_extract(k.data,'$.enabled')=1 AND json_extract(k.data,'$.retirement_state')='active'
AND c.validity<>'invalid' AND kg.validity<>'invalid' AND kg.blocked=0 AND sg.blocked=0
AND kg.cooldown_ms<=? AND sg.cooldown_ms<=? AND kg.breaker_ms<=? AND sg.breaker_ms<=? AND kg.half_open<=? AND sg.half_open<=?
ORDER BY sg.next_order,kg.next_order,k.id`, provider, now, now, now, now, now, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []keyCandidate{}
	for rows.Next() {
		var data, credential string
		if err = rows.Scan(&data, &credential); err != nil {
			return nil, err
		}
		key, err := Decode[domain.UpstreamKey](data)
		if err != nil {
			return nil, err
		}
		items = append(items, keyCandidate{Key: key, Credential: credential})
	}
	return items, rows.Err()
}

// KeyBalanceTx counts attributable local costs and all unsettled key reservations.
func KeyBalanceTx(ctx context.Context, tx *sql.Tx, key string) (int64, int64, error) {
	var used, held int64
	if err := tx.QueryRowContext(ctx, "SELECT coalesce(sum(amount),0) FROM usage_ledger WHERE key_id=?", key).Scan(&used); err != nil {
		return 0, 0, err
	}
	err := tx.QueryRowContext(ctx, "SELECT coalesce(sum(amount),0) FROM budget_reservations WHERE key_id=? AND state='held'", key).Scan(&held)
	return used, held, err
}

func keyBudgetTx(ctx context.Context, tx *sql.Tx, key domain.UpstreamKey, amount int64, research bool) error {
	if amount < 0 {
		return Reject("invalid_request", 400, "Reservation cannot be negative")
	}
	used, held, err := KeyBalanceTx(ctx, tx, key.Id)
	if err != nil {
		return err
	}
	if key.QuotaPolicy.Allocation > 0 && int64(key.QuotaPolicy.Allocation)-used-held < amount {
		return Reject("quota_unavailable", 503, "Configured key safety limit is exhausted")
	}
	if research {
		var active int64
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM budget_reservations WHERE key_id=? AND active_slot=1", key.Id).Scan(&active); err != nil {
			return err
		}
		if key.QuotaPolicy.ActiveJobs > 0 && active >= int64(key.QuotaPolicy.ActiveJobs) {
			return Reject("capacity_unavailable", 503, "Key active slots remain held")
		}
	}
	if shared := key.SharedLimit; shared != nil {
		used, held, err = BalanceTx(ctx, tx, key.AssociationRef)
		if err != nil {
			return err
		}
		if shared.Allocation > 0 && int64(shared.Allocation)-used-held < amount {
			return Reject("quota_unavailable", 503, "Shared safety limit is exhausted")
		}
		if research {
			var active int64
			if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM budget_reservations WHERE association_ref=? AND active_slot=1", key.AssociationRef).Scan(&active); err != nil {
				return err
			}
			if shared.ActiveJobs > 0 && active >= int64(shared.ActiveJobs) {
				return Reject("capacity_unavailable", 503, "Shared active slots remain held")
			}
		}
	}
	return nil
}
