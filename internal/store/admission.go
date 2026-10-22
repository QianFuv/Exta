package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"exta/internal/domain"
)

// Admission is already-validated intent; reserving it never sends a network request.
type Admission struct {
	ParentId, PrincipalId, Provider, Operation string
	Amount                                     int64
	MinimumAmount                              int64
	Research                                   bool
}

// Reservation pins identity and accounting across configuration edits and restarts.
type Reservation struct {
	Id, ParentId, PrincipalId, AccountId, KeyId, CredentialVersionId, Provider, Unit, State, CapacityBasis string
	Amount                                                                                                 int64
	ActiveSlot                                                                                             bool
	AccountGateGeneration, KeyGateGeneration                                                               int64
}

// BalanceTx counts both paid/estimated charges and unsettled reservations.
func BalanceTx(ctx context.Context, tx *sql.Tx, account string) (int64, int64, error) {
	var used, held int64
	if err := tx.QueryRowContext(ctx, "SELECT coalesce(sum(amount),0) FROM usage_ledger WHERE account_id=?", account).Scan(&used); err != nil {
		return 0, 0, err
	}
	err := tx.QueryRowContext(ctx, "SELECT coalesce(sum(amount),0) FROM budget_reservations WHERE account_id=? AND state='held'", account).Scan(&held)
	return used, held, err
}

// SettingsTx reads limits inside a larger atomic acceptance transaction.
func SettingsTx(ctx context.Context, tx *sql.Tx) (domain.Settings, error) {
	var data string
	if err := tx.QueryRowContext(ctx, "SELECT data FROM settings WHERE id=1").Scan(&data); err != nil {
		return domain.Settings{}, err
	}
	return Decode[domain.Settings](data)
}

// Reserve wraps the same transaction primitive used by durable research acceptance.
func (state *Store) Reserve(ctx context.Context, input Admission) (Reservation, error) {
	var result Reservation
	err := state.Write(ctx, func(tx *sql.Tx) error {
		settings, err := SettingsTx(ctx, tx)
		if err != nil {
			return err
		}
		result, err = state.ReserveTx(ctx, tx, input, settings.Limits)
		return err
	})
	return result, err
}

// ReserveTx atomically selects account/key, spends rate tokens and reserves budget/slots.
func (state *Store) ReserveTx(ctx context.Context, tx *sql.Tx, input Admission, limits domain.Limits) (Reservation, error) {
	result := Reservation{}
	now := state.Now().UnixMilli()
	var providerUntil int64
	if err := tx.QueryRowContext(ctx, "SELECT coalesce(max(cooldown_ms),0) FROM runtime_gates WHERE scope='provider' AND entity_id=?", input.Provider).Scan(&providerUntil); err != nil {
		return result, err
	}
	if providerUntil > now {
		fault := Reject("upstream_rate_limited", 429, "Shared provider endpoint is cooling down")
		fault.RetryAfterMs = providerUntil - now
		return result, fault
	}
	if input.Research {
		var active, personal, queued int
		if err := tx.QueryRowContext(ctx, "SELECT count(*),coalesce(sum(CASE WHEN principal_id=? THEN 1 ELSE 0 END),0) FROM budget_reservations WHERE active_slot=1", input.PrincipalId).Scan(&active, &personal); err != nil {
			return result, err
		}
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM research_jobs WHERE state='queued'").Scan(&queued); err != nil {
			return result, err
		}
		if active >= int(limits.ResearchActive) || personal >= int(limits.PrincipalResearchActive) || queued >= int(limits.ResearchQueued) {
			return result, Reject("capacity_unavailable", 503, "Research capacity remains occupied by active or unsettled work")
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT a.data FROM provider_accounts a JOIN runtime_gates g ON g.scope='account' AND g.entity_id=a.id WHERE a.provider=? AND json_extract(a.data,'$.enabled')=1 AND g.blocked=0 AND g.cooldown_ms<=? AND g.breaker_ms<=? AND g.half_open<=? ORDER BY g.next_order,a.id`, input.Provider, now, now, now)
	if err != nil {
		return result, err
	}
	accounts := []domain.Account{}
	for rows.Next() {
		var data string
		if err = rows.Scan(&data); err != nil {
			rows.Close()
			return result, err
		}
		account, err := Decode[domain.Account](data)
		if err != nil {
			rows.Close()
			return result, err
		}
		accounts = append(accounts, account)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	var selectionErr error = Reject("provider_unavailable", 503, "No eligible provider account and credential")
	if err = consumeRate(ctx, tx, "principal", input.PrincipalId, now, 1, int64(limits.PrincipalRequestsPerMinute), 10); err != nil {
		return result, err
	}
	for _, account := range accounts {
		amount := input.Amount
		if amount == 0 {
			amount = int64(account.QuotaPolicy.PerOperationReservation)
		}
		if amount <= 0 {
			selectionErr = Reject("quota_unavailable", 503, "Per-operation reservation must be configured before dispatch")
			continue
		}
		amount = max(amount, input.MinimumAmount)
		used, held, err := BalanceTx(ctx, tx, account.Id)
		if err != nil {
			return result, err
		}
		if amount <= 0 || int64(account.QuotaPolicy.Allocation)-used-held < amount {
			selectionErr = Reject("quota_unavailable", 503, "No configured spendable provider allocation")
			continue
		}
		if input.Research {
			var active int
			if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM budget_reservations WHERE account_id=? AND active_slot=1", account.Id).Scan(&active); err != nil {
				return result, err
			}
			if active >= int(account.QuotaPolicy.ActiveJobs) {
				selectionErr = Reject("capacity_unavailable", 503, "Account active slots remain held")
				continue
			}
		}
		var keyId, credential string
		err = tx.QueryRowContext(ctx, `SELECT k.id,k.active_version FROM provider_keys k JOIN runtime_gates g ON g.scope='key' AND g.entity_id=k.id JOIN credential_versions c ON c.id=k.active_version WHERE k.account_id=? AND json_extract(k.data,'$.enabled')=1 AND json_extract(k.data,'$.retirement_state')='active' AND c.validity<>'invalid' AND g.validity<>'invalid' AND g.blocked=0 AND g.cooldown_ms<=? AND g.breaker_ms<=? AND g.half_open<=? ORDER BY g.next_order,k.id LIMIT 1`, account.Id, now, now, now).Scan(&keyId, &credential)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return result, err
		}
		units := int64(1)
		if input.Research && input.Provider == "exa" {
			units = 2
		}
		rate := int64(60)
		burst := int64(5)
		if input.Provider == "exa" {
			rate = 600
			burst = 10
		}
		if _, err = tx.ExecContext(ctx, "SAVEPOINT account_admission"); err != nil {
			return result, err
		}
		if err = consumeRate(ctx, tx, "account", account.Id, now, units, rate, burst); err == nil {
			err = consumeRate(ctx, tx, "key", keyId, now, units, rate, burst)
		}
		if err != nil {
			var fault *Fault
			if !errors.As(err, &fault) || fault.Code != "local_rate_limited" {
				return result, err
			}
			selectionErr = err
			if _, err = tx.ExecContext(ctx, "ROLLBACK TO account_admission"); err != nil {
				return result, err
			}
			if _, err = tx.ExecContext(ctx, "RELEASE account_admission"); err != nil {
				return result, err
			}
			continue
		}
		if _, err = tx.ExecContext(ctx, "RELEASE account_admission"); err != nil {
			return result, err
		}
		result = Reservation{Id: ID("hold"), ParentId: input.ParentId, PrincipalId: input.PrincipalId, AccountId: account.Id, KeyId: keyId, CredentialVersionId: credential, Provider: input.Provider, Unit: string(account.QuotaPolicy.Unit), Amount: amount, State: "held", ActiveSlot: input.Research, CapacityBasis: "none"}
		if err = tx.QueryRowContext(ctx, "SELECT generation FROM runtime_gates WHERE scope='account' AND entity_id=?", account.Id).Scan(&result.AccountGateGeneration); err != nil {
			return result, err
		}
		if err = tx.QueryRowContext(ctx, "SELECT generation FROM runtime_gates WHERE scope='key' AND entity_id=?", keyId).Scan(&result.KeyGateGeneration); err != nil {
			return result, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO budget_reservations VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, result.Id, result.ParentId, result.PrincipalId, result.AccountId, result.KeyId, result.CredentialVersionId, result.Unit, result.Amount, result.State, result.ActiveSlot, result.CapacityBasis, Stamp(state.Now()), result.AccountGateGeneration, result.KeyGateGeneration); err != nil {
			return Reservation{}, err
		}
		for _, gate := range [][2]string{{"account", account.Id}, {"key", keyId}} {
			if _, err = tx.ExecContext(ctx, `UPDATE runtime_gates SET next_order=(SELECT coalesce(max(next_order),0)+1 FROM runtime_gates WHERE scope=?),half_open=CASE WHEN breaker_ms>0 THEN ? ELSE 0 END,probe_owner=CASE WHEN breaker_ms>0 THEN ? ELSE '' END WHERE scope=? AND entity_id=?`, gate[0], now+180000, result.Id, gate[0], gate[1]); err != nil {
				return Reservation{}, err
			}
		}
		return result, nil
	}
	return result, selectionErr
}

// ProviderCooldown prevents unknown/network rate limits being bypassed across accounts.
func (state *Store) ProviderCooldown(ctx context.Context, provider string, duration time.Duration) error {
	return state.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO runtime_gates(scope,entity_id,cooldown_ms) VALUES('provider',?,?) ON CONFLICT(scope,entity_id) DO UPDATE SET cooldown_ms=max(cooldown_ms,excluded.cooldown_ms)", provider, state.Now().Add(duration).UnixMilli())
		return err
	})
}

func consumeRate(ctx context.Context, tx *sql.Tx, scope, id string, now, units, rate, burst int64) error {
	var last, used int64
	err := tx.QueryRowContext(ctx, "SELECT window_ms,used FROM rate_windows WHERE scope=? AND entity_id=?", scope, id).Scan(&last, &used)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	elapsed := max(int64(0), min(now-last, (used*60000+rate*1000-1)/(rate*1000)))
	used = max(int64(0), used-elapsed*rate*1000/60000)
	if used+units*1000 > burst*1000 {
		fault := Reject("local_rate_limited", 429, "Local rate allowance is exhausted")
		fault.RetryAfterMs = max(int64(1), (used+units*1000-burst*1000)*60000/(rate*1000))
		fault.Details["scope"] = scope
		return fault
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO rate_windows VALUES(?,?,?,?) ON CONFLICT(scope,entity_id) DO UPDATE SET window_ms=excluded.window_ms,used=excluded.used`, scope, id, now, used+units*1000)
	return err
}

// ReservationTx loads an unsettled or historical reservation without changing its state.
func ReservationTx(ctx context.Context, tx *sql.Tx, id string) (Reservation, error) {
	var item Reservation
	err := tx.QueryRowContext(ctx, `SELECT r.id,r.parent_id,r.principal_id,r.account_id,r.key_id,r.credential_version_id,a.provider,r.unit,r.amount,r.state,r.active_slot,r.capacity_basis,r.account_gate_generation,r.key_gate_generation FROM budget_reservations r JOIN provider_accounts a ON a.id=r.account_id WHERE r.id=?`, id).Scan(&item.Id, &item.ParentId, &item.PrincipalId, &item.AccountId, &item.KeyId, &item.CredentialVersionId, &item.Provider, &item.Unit, &item.Amount, &item.State, &item.ActiveSlot, &item.CapacityBasis, &item.AccountGateGeneration, &item.KeyGateGeneration)
	return item, Missing(err)
}

// ReleaseUnsent restores capacity only after affirmative evidence of non-submission.
func (state *Store) ReleaseUnsent(ctx context.Context, id string) error {
	return state.Write(ctx, func(tx *sql.Tx) error { return ReleaseUnsentTx(ctx, tx, id) })
}

// ReleaseUnsentTx is also used by queued cancellation in an atomic job transition.
func ReleaseUnsentTx(ctx context.Context, tx *sql.Tx, id string) error {
	_, err := tx.ExecContext(ctx, "UPDATE budget_reservations SET state='released_unspent',active_slot=0,capacity_basis='proven_non_submission' WHERE id=? AND state='held'", id)
	return err
}

// SettleTx books one charge. Unknown terminal cost releases only capacity, never budget.
func SettleTx(ctx context.Context, tx *sql.Tx, id string, cost *int64, certainty string, now time.Time) error {
	item, err := ReservationTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE budget_reservations SET active_slot=0,capacity_basis='verified_terminal' WHERE id=?", id); err != nil {
		return err
	}
	if cost == nil || item.State != "held" {
		return nil
	}
	if *cost < 0 {
		return Reject("invalid_request", 400, "Reported cost cannot be negative")
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO usage_ledger VALUES(?,?,'charge',1,?,?,?,?,?,?) ON CONFLICT(source,event_kind,revision) DO NOTHING", ID("usage"), id, item.AccountId, item.PrincipalId, item.Unit, *cost, certainty, Stamp(now)); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE budget_reservations SET state='settled' WHERE id=? AND state='held'", id)
	return err
}

// Settle is the synchronous operation settlement wrapper.
func (state *Store) Settle(ctx context.Context, id string, cost *int64, certainty string) error {
	return state.Write(ctx, func(tx *sql.Tx) error { return SettleTx(ctx, tx, id, cost, certainty, state.Now()) })
}

// GateFailure applies account/key scope without revoking a rotated credential by mistake.
func (state *Store) GateFailure(ctx context.Context, scope, id, credential, kind string, cooldown time.Duration) error {
	return state.Write(ctx, func(tx *sql.Tx) error {
		if scope != "account" && scope != "key" {
			return Reject("invalid_request", 400, "Unknown gate scope")
		}
		now := state.Now().UnixMilli()
		if kind == "invalid" {
			if _, err := tx.ExecContext(ctx, "UPDATE credential_versions SET validity='invalid' WHERE id=?", credential); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, "UPDATE runtime_gates SET validity='invalid',half_open=0 WHERE scope='key' AND entity_id=? AND EXISTS(SELECT 1 FROM provider_keys WHERE id=? AND active_version=?)", id, id, credential)
			return err
		}
		if kind == "quota" {
			_, err := tx.ExecContext(ctx, "UPDATE runtime_gates SET blocked=1,half_open=0 WHERE scope=? AND entity_id=? AND (scope='account' OR EXISTS(SELECT 1 FROM provider_keys WHERE id=? AND active_version=?))", scope, id, id, credential)
			return err
		}
		if kind == "rate" {
			_, err := tx.ExecContext(ctx, "UPDATE runtime_gates SET cooldown_ms=max(cooldown_ms,?),half_open=0 WHERE scope=? AND entity_id=?", now+cooldown.Milliseconds(), scope, id)
			return err
		}
		if kind == "transient" {
			_, err := tx.ExecContext(ctx, `UPDATE runtime_gates SET failures=CASE WHEN window_ms>? THEN failures+1 ELSE 1 END,window_ms=CASE WHEN window_ms>? THEN window_ms ELSE ? END,breaker_ms=CASE WHEN (window_ms>? AND failures>=4) OR half_open>0 THEN ? ELSE breaker_ms END,generation=generation+CASE WHEN (window_ms>? AND failures>=4) OR half_open>0 THEN 1 ELSE 0 END,half_open=0,probe_owner='' WHERE scope=? AND entity_id=?`, now-60000, now-60000, now, now-60000, now+30000, now-60000, scope, id)
			return err
		}
		return Reject("invalid_request", 400, "Unknown gate failure")
	})
}

// GateSuccess closes a circuit but does not erase billing blocks or rate cooldowns.
func (state *Store) GateSuccess(ctx context.Context, reservation Reservation) error {
	return state.Write(ctx, func(tx *sql.Tx) error {
		for _, gate := range [][2]string{{"account", reservation.AccountId}, {"key", reservation.KeyId}} {
			generation := reservation.AccountGateGeneration
			if gate[0] == "key" {
				generation = reservation.KeyGateGeneration
			}
			if _, err := tx.ExecContext(ctx, "UPDATE runtime_gates SET failures=0,breaker_ms=0,half_open=0,probe_owner='' WHERE scope=? AND entity_id=? AND generation=? AND (breaker_ms=0 OR probe_owner=?)", gate[0], gate[1], generation, reservation.Id); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, "UPDATE credential_versions SET validity='valid' WHERE id=? AND validity<>'invalid'", reservation.CredentialVersionId); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "UPDATE runtime_gates SET validity='valid' WHERE scope='key' AND validity<>'invalid' AND entity_id=? AND EXISTS(SELECT 1 FROM provider_keys WHERE id=? AND active_version=?)", reservation.KeyId, reservation.KeyId, reservation.CredentialVersionId)
		return err
	})
}
