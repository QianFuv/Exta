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
	Id, ParentId, PrincipalId, AssociationRef, KeyId, CredentialVersionId, Provider, Unit, State, CapacityBasis string
	Amount                                                                                                      int64
	ActiveSlot                                                                                                  bool
	AssociationGateGeneration, KeyGateGeneration                                                                int64
}

// BalanceTx counts both paid/estimated charges and unsettled reservations.
func BalanceTx(ctx context.Context, tx *sql.Tx, association string) (int64, int64, error) {
	var used, held int64
	if err := tx.QueryRowContext(ctx, "SELECT coalesce(sum(amount),0) FROM usage_ledger WHERE association_ref=?", association).Scan(&used); err != nil {
		return 0, 0, err
	}
	err := tx.QueryRowContext(ctx, "SELECT coalesce(sum(amount),0) FROM budget_reservations WHERE association_ref=? AND state='held'", association).Scan(&held)
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

// ReserveTx atomically selects a key, spends rate tokens and reserves budget/slots.
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
	candidates, err := eligibleKeysTx(ctx, tx, input.Provider, now)
	if err != nil {
		return result, err
	}
	var selectionErr error = Reject("provider_unavailable", 503, "No eligible upstream key")
	if err = consumeRate(ctx, tx, "principal", input.PrincipalId, now, 1, int64(limits.PrincipalRequestsPerMinute), 10); err != nil {
		return result, err
	}
	for _, candidate := range candidates {
		key, keyId, credential := candidate.Key, candidate.Key.Id, candidate.Credential
		amount := input.Amount
		if amount == 0 {
			amount = int64(key.QuotaPolicy.PerOperationReservation)
		}
		if amount <= 0 {
			selectionErr = Reject("quota_unavailable", 503, "Per-operation safety reservation is not configured")
			continue
		}
		amount = max(amount, input.MinimumAmount)
		if err = keyBudgetTx(ctx, tx, key, amount, input.Research); err != nil {
			var fault *Fault
			if !errors.As(err, &fault) {
				return result, err
			}
			selectionErr = err
			continue
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
		if _, err = tx.ExecContext(ctx, "SAVEPOINT key_admission"); err != nil {
			return result, err
		}
		if !input.Research {
			if err = consumeRate(ctx, tx, "association", key.AssociationRef, now, units, rate, burst); err == nil {
				err = consumeRate(ctx, tx, "key", keyId, now, units, rate, burst)
			}
		}
		if err != nil {
			var fault *Fault
			if !errors.As(err, &fault) || fault.Code != "local_rate_limited" {
				return result, err
			}
			selectionErr = err
			if _, err = tx.ExecContext(ctx, "ROLLBACK TO key_admission"); err != nil {
				return result, err
			}
			if _, err = tx.ExecContext(ctx, "RELEASE key_admission"); err != nil {
				return result, err
			}
			continue
		}
		if _, err = tx.ExecContext(ctx, "RELEASE key_admission"); err != nil {
			return result, err
		}
		result = Reservation{Id: ID("hold"), ParentId: input.ParentId, PrincipalId: input.PrincipalId, AssociationRef: key.AssociationRef, KeyId: keyId, CredentialVersionId: credential, Provider: input.Provider, Unit: string(key.QuotaPolicy.Unit), Amount: amount, State: "held", ActiveSlot: input.Research, CapacityBasis: "none"}
		if err = tx.QueryRowContext(ctx, "SELECT generation FROM runtime_gates WHERE scope='association' AND entity_id=?", key.AssociationRef).Scan(&result.AssociationGateGeneration); err != nil {
			return result, err
		}
		if err = tx.QueryRowContext(ctx, "SELECT generation FROM runtime_gates WHERE scope='key' AND entity_id=?", keyId).Scan(&result.KeyGateGeneration); err != nil {
			return result, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO budget_reservations VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, result.Id, result.ParentId, result.PrincipalId, result.AssociationRef, result.KeyId, result.CredentialVersionId, result.Unit, result.Amount, result.State, result.ActiveSlot, result.CapacityBasis, Stamp(state.Now()), result.AssociationGateGeneration, result.KeyGateGeneration); err != nil {
			return Reservation{}, err
		}
		for _, gate := range [][2]string{{"association", key.AssociationRef}, {"key", keyId}} {
			if input.Research {
				if _, err = tx.ExecContext(ctx, "UPDATE runtime_gates SET next_order=(SELECT coalesce(max(next_order),0)+1 FROM runtime_gates WHERE scope=?) WHERE scope=? AND entity_id=?", gate[0], gate[0], gate[1]); err != nil {
					return Reservation{}, err
				}
				continue
			}
			if _, err = tx.ExecContext(ctx, `UPDATE runtime_gates SET next_order=(SELECT coalesce(max(next_order),0)+1 FROM runtime_gates WHERE scope=?),half_open=CASE WHEN breaker_ms>0 THEN ? ELSE 0 END,probe_owner=CASE WHEN breaker_ms>0 THEN ? ELSE '' END WHERE scope=? AND entity_id=?`, gate[0], now+180000, result.Id, gate[0], gate[1]); err != nil {
				return Reservation{}, err
			}
		}
		return result, nil
	}
	return result, selectionErr
}

// ProviderCooldown prevents unknown/network rate limits being bypassed across keys.
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
	err := tx.QueryRowContext(ctx, `SELECT r.id,r.parent_id,r.principal_id,r.association_ref,r.key_id,r.credential_version_id,k.provider,r.unit,r.amount,r.state,r.active_slot,r.capacity_basis,r.association_gate_generation,r.key_gate_generation FROM budget_reservations r JOIN provider_keys k ON k.id=r.key_id WHERE r.id=?`, id).Scan(&item.Id, &item.ParentId, &item.PrincipalId, &item.AssociationRef, &item.KeyId, &item.CredentialVersionId, &item.Provider, &item.Unit, &item.Amount, &item.State, &item.ActiveSlot, &item.CapacityBasis, &item.AssociationGateGeneration, &item.KeyGateGeneration)
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
	if _, err = tx.ExecContext(ctx, "INSERT INTO usage_ledger VALUES(?,?,'charge',1,?,?,?,?,?,?,?) ON CONFLICT(source,event_kind,revision) DO NOTHING", ID("usage"), id, item.AssociationRef, item.PrincipalId, item.Unit, *cost, certainty, Stamp(now), item.KeyId); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE budget_reservations SET state='settled' WHERE id=? AND state='held'", id)
	return err
}

// Settle is the synchronous operation settlement wrapper.
func (state *Store) Settle(ctx context.Context, id string, cost *int64, certainty string) error {
	return state.Write(ctx, func(tx *sql.Tx) error { return SettleTx(ctx, tx, id, cost, certainty, state.Now()) })
}

// GateFailure applies provider-reported shared or key scope without revoking a rotated credential by mistake.
func (state *Store) GateFailure(ctx context.Context, scope, id, credential, kind string, cooldown time.Duration) error {
	return state.Write(ctx, func(tx *sql.Tx) error {
		return state.GateFailureTx(ctx, tx, scope, id, credential, kind, cooldown)
	})
}

// GateFailureTx persists an upstream gate together with durable research evidence.
func (state *Store) GateFailureTx(ctx context.Context, tx *sql.Tx, scope, id, credential, kind string, cooldown time.Duration) error {
	if scope == "account" {
		scope = "association"
	}
	if scope != "association" && scope != "key" {
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
		_, err := tx.ExecContext(ctx, "UPDATE runtime_gates SET blocked=1,half_open=0 WHERE scope=? AND entity_id=? AND (scope='association' OR EXISTS(SELECT 1 FROM provider_keys WHERE id=? AND active_version=?))", scope, id, id, credential)
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
}

// GateSuccess closes a circuit but does not erase billing blocks or rate cooldowns.
func (state *Store) GateSuccess(ctx context.Context, reservation Reservation) error {
	return state.Write(ctx, func(tx *sql.Tx) error {
		return state.GateSuccessTx(ctx, tx, reservation)
	})
}

// GateSuccessTx closes a matching probe with the same transaction as job evidence.
func (state *Store) GateSuccessTx(ctx context.Context, tx *sql.Tx, reservation Reservation) error {
	for _, gate := range [][2]string{{"association", reservation.AssociationRef}, {"key", reservation.KeyId}} {
		generation := reservation.AssociationGateGeneration
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
}

// ResearchDispatch charges shared wire-attempt rates without reserving money again.
func (state *Store) ResearchDispatch(ctx context.Context, reservationID string, isCreate bool, probeOwner string) (Reservation, error) {
	var hold Reservation
	err := state.Write(ctx, func(tx *sql.Tx) error {
		var err error
		hold, err = ReservationTx(ctx, tx, reservationID)
		if err != nil {
			return err
		}
		return state.ResearchDispatchTx(ctx, tx, &hold, isCreate, probeOwner)
	})
	return hold, err
}

// ResearchDispatchTx enforces pinned identity and provider/association/key wire gates.
func (state *Store) ResearchDispatchTx(ctx context.Context, tx *sql.Tx, hold *Reservation, isCreate bool, probeOwner string) error {
	now := state.Now().UnixMilli()
	var until int64
	err := tx.QueryRowContext(ctx, `SELECT coalesce(max(max(cooldown_ms,breaker_ms,CASE WHEN probe_owner<>? THEN half_open ELSE 0 END)),0) FROM runtime_gates WHERE (scope='provider' AND entity_id=?) OR (scope='association' AND entity_id=?) OR (scope='key' AND entity_id=?)`, probeOwner, hold.Provider, hold.AssociationRef, hold.KeyId).Scan(&until)
	if err != nil {
		return err
	}
	if until > now {
		fault := Reject("upstream_rate_limited", 429, "Shared provider dispatch is cooling down")
		fault.RetryAfterMs = until - now
		return fault
	}
	var validity string
	if err = tx.QueryRowContext(ctx, "SELECT validity FROM credential_versions WHERE id=?", hold.CredentialVersionId).Scan(&validity); err != nil {
		return err
	}
	if validity == "invalid" {
		return Reject("provider_unavailable", 503, "Original credential is invalid")
	}
	if isCreate {
		var blocked int
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM runtime_gates WHERE blocked=1 AND ((scope='association' AND entity_id=?) OR (scope='key' AND entity_id=?))", hold.AssociationRef, hold.KeyId).Scan(&blocked); err != nil {
			return err
		}
		if blocked > 0 {
			return Reject("quota_unavailable", 503, "Original provider allocation is blocked")
		}
	}
	rate, burst, units := int64(60), int64(5), int64(1)
	if hold.Provider == "exa" {
		rate, burst = 600, 10
		if isCreate {
			units = 2
		}
	}
	if _, err = tx.ExecContext(ctx, "SAVEPOINT research_wire_rate"); err != nil {
		return err
	}
	rateErr := consumeRate(ctx, tx, "association", hold.AssociationRef, now, units, rate, burst)
	if rateErr == nil {
		rateErr = consumeRate(ctx, tx, "key", hold.KeyId, now, units, rate, burst)
	}
	if rateErr != nil {
		if _, err = tx.ExecContext(ctx, "ROLLBACK TO research_wire_rate"); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, "RELEASE research_wire_rate"); err != nil {
		return err
	}
	if rateErr != nil {
		return rateErr
	}
	for _, gate := range [][2]string{{"association", hold.AssociationRef}, {"key", hold.KeyId}} {
		var generation int64
		if err = tx.QueryRowContext(ctx, "SELECT generation FROM runtime_gates WHERE scope=? AND entity_id=?", gate[0], gate[1]).Scan(&generation); err != nil {
			return err
		}
		if gate[0] == "association" {
			hold.AssociationGateGeneration = generation
		} else {
			hold.KeyGateGeneration = generation
		}
		if _, err = tx.ExecContext(ctx, "UPDATE runtime_gates SET half_open=?,probe_owner=? WHERE scope=? AND entity_id=? AND breaker_ms>0", now+180000, probeOwner, gate[0], gate[1]); err != nil {
			return err
		}
	}
	hold.Id = probeOwner
	return nil
}
