package store

import (
	"context"
	"database/sql"
	"errors"
)

// Available uses admission eligibility and budget checks without consuming resources.
func (state *Store) Available(ctx context.Context, provider string, minimumAmount int64) (bool, error) {
	if state.Vault == nil {
		return false, nil
	}
	available := false
	err := state.Write(ctx, func(tx *sql.Tx) error {
		now := state.Now().UnixMilli()
		var until int64
		if err := tx.QueryRowContext(ctx, "SELECT coalesce(max(cooldown_ms),0) FROM runtime_gates WHERE scope='provider' AND entity_id=?", provider).Scan(&until); err != nil {
			return err
		}
		if until > now {
			return nil
		}
		keys, err := eligibleKeysTx(ctx, tx, provider, now)
		if err != nil {
			return err
		}
		for _, candidate := range keys {
			err = keyBudgetTx(ctx, tx, candidate.Key, max(minimumAmount, int64(candidate.Key.QuotaPolicy.PerOperationReservation)), false)
			if err == nil {
				available = true
				return nil
			}
			var fault *Fault
			if !errors.As(err, &fault) {
				return err
			}
		}
		return nil
	})
	return available, err
}
