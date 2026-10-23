package research

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"regexp"
	"time"

	"exta/internal/domain"
	"exta/internal/providers"
	"exta/internal/store"
)

var publicIdentifier = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,200}$`)

var errFinalization = errors.New("research finalization failed")

// Recover fences the previous process and preserves every possibly submitted hold.
// The caller must exclusively own the data directory before invoking this method.
func (engine *Engine) Recover(ctx context.Context) error {
	return engine.Store.Write(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, "SELECT "+recordColumns+" FROM research_jobs WHERE lease_token IS NOT NULL OR state='submitting'")
		if err != nil {
			return err
		}
		items := []record{}
		for rows.Next() {
			item, err := decodeRecord(rows)
			if err != nil {
				rows.Close()
				return err
			}
			items = append(items, item)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, item := range items {
			from := item.Job.State
			if item.Action == "create" || item.Job.State == "submitting" {
				item.Job.Uncertainty = true
				item.Job.UpstreamMayContinue = true
				if !item.ObservationStopped {
					item.Job.State = "submission_unknown"
				}
				item.Job.Error = publicFault("submission_outcome_unknown", "Submission was interrupted; automatic create is disabled", item.Job.Id)
			}
			item.Lease = ""
			item.LeaseUntil = 0
			item.Action = ""
			item.NextPoll = engine.Store.Now().UnixMilli()
			if err = engine.saveTx(ctx, tx, &item, from, "worker_recovered", ""); err != nil {
				return err
			}
		}
		return nil
	})
}

func (engine *Engine) claim(ctx context.Context) (record, bool, error) {
	var claimed record
	hasWork := false
	err := engine.Store.Write(ctx, func(tx *sql.Tx) error {
		now := engine.Store.Now()
		item, err := decodeRecord(tx.QueryRowContext(ctx, "SELECT "+recordColumns+" FROM research_jobs WHERE state IN ('queued','submitting','accepted','running','cancel_requested','submission_unknown') AND next_poll_ms<=? AND lease_until_ms<=? ORDER BY next_poll_ms,created_at,id LIMIT 1", now.UnixMilli(), now.UnixMilli()))
		if isMissing(err) {
			return nil
		}
		if err != nil {
			return err
		}
		from := item.Job.State
		deadline, err := time.Parse(time.RFC3339Nano, item.Job.ObservationDeadline)
		if err != nil {
			return err
		}
		if item.Job.State == "submitting" || item.Action == "create" {
			item.Job.State = "submission_unknown"
			item.Job.Uncertainty = true
			item.Job.UpstreamMayContinue = true
			item.Action = ""
			item.Lease = ""
			item.LeaseUntil = 0
			item.Job.Error = publicFault("submission_outcome_unknown", "Expired submission lease requires reconciliation", item.Job.Id)
			return engine.saveTx(ctx, tx, &item, from, "submission_lease_expired", "")
		}
		hasCancel := item.RawID != "" && item.CancelPending && !item.CancelSent
		if !hasCancel && (!now.Before(deadline) || int(item.Job.PollingCount) >= item.MaxPolls) {
			if item.Job.State == "queued" {
				item.Job.State = "failed"
				item.Job.UpstreamMayContinue = false
				if err = store.ReleaseUnsentTx(ctx, tx, item.ReservationID); err != nil {
					return err
				}
			} else {
				item.Job.State = "observation_expired"
				item.Job.UpstreamMayContinue = true
				item.Job.Uncertainty = true
			}
			item.Job.CompletionReason = pointer("observation_limit")
			return engine.saveTx(ctx, tx, &item, from, "observation_limit", "")
		}
		if engine.Store.Vault == nil {
			item.Job.State = "observation_expired"
			item.Job.Uncertainty = from != "queued"
			item.Job.UpstreamMayContinue = from != "queued"
			if from == "queued" {
				item.Job.State = "failed"
				if err = store.ReleaseUnsentTx(ctx, tx, item.ReservationID); err != nil {
					return err
				}
			}
			item.Job.Error = publicFault("secret_store_locked", "Research observation requires the original credential", item.Job.Id)
			return engine.saveTx(ctx, tx, &item, from, "credential_unavailable", "")
		}
		if item.RawID == "" && item.Job.State != "queued" && item.Job.SourceProvider != "exa" {
			item.NextPoll = deadline.UnixMilli()
			return engine.saveTx(ctx, tx, &item, from, "manual_correlation_required", "")
		}
		item.Action = "poll"
		hold, err := store.ReservationTx(ctx, tx, item.ReservationID)
		if err != nil {
			return err
		}
		if item.Job.State == "queued" {
			var enabled int
			if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM provider_keys k WHERE k.id=? AND json_extract(k.data,'$.enabled')=1 AND json_extract(k.data,'$.retirement_state')='active'", hold.KeyId).Scan(&enabled); err != nil {
				return err
			}
			if enabled == 0 {
				item.Job.State = "failed"
				item.Job.Error = publicFault("provider_unavailable", "Queued research credential is no longer eligible", item.Job.Id)
				if err = store.ReleaseUnsentTx(ctx, tx, item.ReservationID); err != nil {
					return err
				}
				return engine.saveTx(ctx, tx, &item, from, "queued_admission_revoked", "")
			}
		}
		item.Lease = store.ID("lease")
		if err = engine.Store.ResearchDispatchTx(ctx, tx, &hold, item.Job.State == "queued", item.Lease); err != nil {
			item.Lease = ""
			var fault *store.Fault
			if !errors.As(err, &fault) {
				return err
			}
			if fault.RetryAfterMs > 0 {
				item.NextPoll = now.UnixMilli() + fault.RetryAfterMs
				if !hasCancel {
					item.NextPoll = min(item.NextPoll, deadline.UnixMilli())
				}
			} else {
				item.Job.State = "observation_expired"
				item.Job.Uncertainty = true
				if from == "queued" {
					item.Job.State = "failed"
					item.Job.Uncertainty = false
					if err = store.ReleaseUnsentTx(ctx, tx, item.ReservationID); err != nil {
						return err
					}
				}
			}
			item.Job.Error = publicFault(fault.Code, fault.Message, item.Job.Id)
			return engine.saveTx(ctx, tx, &item, from, "dispatch_gate", "")
		}
		item.DispatchReservation = hold
		if item.Job.State == "queued" {
			item.Action = "create"
			item.Attempts++
			item.Job.State = "submitting"
		} else if item.RawID == "" {
			item.Action = "reconcile"
		} else if item.CancelPending && !item.CancelSent {
			item.Action = "cancel"
			item.CancelSent = true
			item.Job.State = "cancel_requested"
		}
		if item.Action != "create" {
			item.Job.PollingCount++
			item.TotalPolls++
		}
		item.LeaseUntil = now.Add(90 * time.Second).UnixMilli()
		if err = engine.saveTx(ctx, tx, &item, from, "worker_"+item.Action, ""); err != nil {
			return err
		}
		claimed = item
		hasWork = true
		return nil
	})
	return claimed, hasWork, err
}

func (engine *Engine) input(item record) (domain.ResearchCreate, error) {
	var input domain.ResearchCreate
	if engine.Store.Vault == nil {
		return input, store.Reject("secret_store_locked", 503, "Secret store is locked")
	}
	data, err := engine.Store.Vault.Open(item.Job.Id+"/input", item.InputCipher)
	if err != nil {
		return input, store.Reject("secret_store_locked", 503, "Research input cannot be decrypted")
	}
	if json.Unmarshal(data, &input) != nil {
		return input, store.Reject("storage_unavailable", 503, "Research input is unreadable")
	}
	return input, nil
}

// Tick claims and processes at most one persisted action, outside database transactions.
func (engine *Engine) Tick(ctx context.Context) (bool, error) {
	item, found, err := engine.claim(ctx)
	if err != nil || !found {
		return found, normalizeClaimError(ctx, err)
	}
	call, cancel := context.WithTimeout(ctx, time.Duration(max(1, min(30, item.Snapshot.RoutePolicy.DeadlineSeconds)))*time.Second)
	defer cancel()
	settings, err := engine.Store.Settings(call)
	var outcome providers.ResearchOutcome
	if err == nil {
		var credential string
		credential, err = engine.Store.Credential(call, item.Credential)
		if err == nil {
			client := engine.Clients[item.Job.SourceProvider]
			if client == nil {
				err = store.Reject("provider_unavailable", 503, "Research adapter is unavailable")
			} else {
				switch item.Action {
				case "create":
					var input domain.ResearchCreate
					input, err = engine.input(item)
					if err == nil {
						outcome = client.ResearchCreate(call, credential, input, item.Job.Id, item.IntentDigest, int64(settings.Limits.ResponseBytes))
					}
				case "poll":
					outcome = client.ResearchGet(call, credential, item.RawID, int64(settings.Limits.ResponseBytes))
				case "cancel":
					outcome = client.ResearchCancel(call, credential, item.RawID, int64(settings.Limits.ResponseBytes))
				case "reconcile":
					outcome, err = engine.discover(call, item, credential, int64(settings.Limits.ResponseBytes))
				}
			}
		}
	}
	if err != nil {
		fault := store.Reject("storage_unavailable", 503, "Research action cannot proceed")
		var known *store.Fault
		if errors.As(err, &known) {
			fault = known
		}
		outcome = providers.ResearchOutcome{Outcome: providers.Outcome{Failure: fault, IsUnspent: item.Action == "create", Transmission: "not_sent"}}
	}
	persist, done := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer done()
	if err = engine.complete(persist, item, outcome); err != nil {
		return true, fmt.Errorf("%w: %w", errFinalization, err)
	}
	return true, nil
}

func normalizeClaimError(ctx context.Context, err error) error {
	if ctx.Err() != nil && errors.Is(err, sql.ErrTxDone) {
		return ctx.Err()
	}
	return err
}

func (engine *Engine) nextDelay(count int) time.Duration {
	if engine.PollDelay != nil {
		return engine.PollDelay(count)
	}
	delay := min(60*time.Second, 5*time.Second*time.Duration(1<<max(0, min(count-1, 4))))
	return delay - time.Duration(rand.Int64N(int64(delay/5)+1))
}

func (engine *Engine) complete(ctx context.Context, claimed record, outcome providers.ResearchOutcome) error {
	return engine.Store.Write(ctx, func(tx *sql.Tx) error {
		item, err := readTx(ctx, tx, claimed.Job.Id, "")
		if err != nil {
			return err
		}
		if item.Lease != claimed.Lease {
			return nil
		}
		item.DispatchReservation = claimed.DispatchReservation
		if err = engine.applyGatesTx(ctx, tx, item, outcome); err != nil {
			return err
		}
		from := item.Job.State
		item.Lease = ""
		item.LeaseUntil = 0
		item.Action = ""
		delay := max(engine.nextDelay(int(item.Job.PollingCount)), outcome.Cooldown)
		next := engine.Store.Now().Add(delay)
		deadline, _ := time.Parse(time.RFC3339Nano, item.Job.ObservationDeadline)
		if next.After(deadline) {
			next = deadline
		}
		item.NextPoll = next.UnixMilli()
		if terminal(item.Job.State) {
			return engine.saveTx(ctx, tx, &item, from, "stale_action_ignored", "")
		}
		if outcome.ID != "" {
			if item.RawID != "" && item.RawID != outcome.ID {
				return store.Reject("job_state_conflict", 409, "Provider identity changed")
			}
			item.RawID = outcome.ID
			if publicIdentifier.MatchString(outcome.ID) {
				item.Job.UpstreamJobId = &outcome.ID
			}
		}
		if outcome.Failure != nil {
			item.Job.Error = publicFault(outcome.Failure.Code, outcome.Failure.Message, item.Job.Id)
		} else {
			item.Job.Error = nil
		}
		if outcome.State != "" {
			item.Job.UpstreamState = &outcome.State
		}
		isTerminal := outcome.State == "completed" || outcome.State == "failed" || outcome.State == "cancelled"
		if item.RawID != "" && isTerminal {
			item.Job.State = map[string]domain.JobState{"completed": "succeeded", "failed": "failed", "cancelled": "cancelled"}[outcome.State]
			item.Job.UpstreamMayContinue = false
			item.Job.Uncertainty = outcome.Cost == nil
			item.Job.CompletionReason = nil
			if outcome.StopReason != "" {
				item.Job.CompletionReason = &outcome.StopReason
			}
			item.Job.IsPartial = outcome.StopReason == "budget_reached" || outcome.State == "completed" && outcome.Failure != nil
			if outcome.Report != nil {
				item.ResultCipher, err = engine.Store.Vault.Seal(item.Job.Id+"/result", []byte(store.JSON(outcome.Report)))
				if err != nil {
					return err
				}
			}
			if err = store.SettleTx(ctx, tx, item.ReservationID, outcome.Cost, "reported", engine.Store.Now()); err != nil {
				return err
			}
		} else if item.RawID != "" {
			item.Job.UpstreamMayContinue = true
			item.Job.Uncertainty = false
			if !item.ObservationStopped {
				switch outcome.State {
				case "queued", "pending":
					item.Job.State = "accepted"
				case "running", "in_progress":
					item.Job.State = "running"
				case "":
					if claimed.Action == "create" {
						item.Job.State = "accepted"
					}
				default:
					item.Job.State = "observation_expired"
					item.Job.Uncertainty = true
					item.Job.CompletionReason = pointer("unsupported_upstream_state")
				}
				if item.CancelPending && item.Job.State != "observation_expired" {
					item.Job.State = "cancel_requested"
				}
			}
			if outcome.Status == 401 || outcome.Status == 403 {
				item.Job.State = "observation_expired"
				item.Job.Uncertainty = true
			}
			if outcome.Failure != nil && outcome.Failure.Code == "upstream_protocol_error" && outcome.State == "" {
				item.Job.State = "observation_expired"
				item.Job.Uncertainty = true
				item.Job.CompletionReason = pointer("invalid_upstream_state")
			}
		} else if claimed.Action == "create" && outcome.IsUnspent {
			if outcome.CanRetry && !outcome.RetryProhibited && outcome.Status != 401 && item.Attempts < int(min(2, item.Snapshot.RoutePolicy.MaxAttempts)) && !item.ObservationStopped && !item.CancelPending {
				item.Job.State = "queued"
				item.Job.Uncertainty = false
				item.Job.UpstreamMayContinue = false
			} else {
				item.Job.State = "failed"
				if item.CancelPending || item.ObservationStopped {
					item.Job.State = "cancelled"
				}
				item.Job.Uncertainty = false
				item.Job.UpstreamMayContinue = false
				if err = store.ReleaseUnsentTx(ctx, tx, item.ReservationID); err != nil {
					return err
				}
			}
		} else {
			if !item.ObservationStopped {
				item.Job.State = "submission_unknown"
			}
			item.Job.Uncertainty = true
			item.Job.UpstreamMayContinue = true
			if item.Job.Error == nil {
				item.Job.Error = publicFault("submission_outcome_unknown", "Submission outcome is unknown; no automatic create will follow", item.Job.Id)
			}
		}
		return engine.saveTx(ctx, tx, &item, from, "worker_observation", "")
	})
}

func (engine *Engine) applyGatesTx(ctx context.Context, tx *sql.Tx, item record, outcome providers.ResearchOutcome) error {
	hold, err := store.ReservationTx(ctx, tx, item.ReservationID)
	if err != nil {
		return err
	}
	if outcome.GateKind != "" {
		entity := hold.AssociationRef
		if outcome.GateScope == "key" {
			entity = hold.KeyId
		}
		if err = engine.Store.GateFailureTx(ctx, tx, outcome.GateScope, entity, item.Credential, outcome.GateKind, outcome.Cooldown); err != nil {
			return err
		}
	}
	if outcome.SharedCooldown {
		_, err = tx.ExecContext(ctx, "INSERT INTO runtime_gates(scope,entity_id,cooldown_ms) VALUES('provider',?,?) ON CONFLICT(scope,entity_id) DO UPDATE SET cooldown_ms=max(cooldown_ms,excluded.cooldown_ms)", item.Job.SourceProvider, engine.Store.Now().Add(outcome.Cooldown).UnixMilli())
	}
	if err == nil && outcome.Failure == nil && outcome.Status >= 200 && outcome.Status < 300 && item.DispatchReservation.Id != "" {
		err = engine.Store.GateSuccessTx(ctx, tx, item.DispatchReservation)
	}
	return err
}

// Run drives a bounded single worker until cancellation; no timer or goroutine is created per job.
func (engine *Engine) Run(ctx context.Context) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return nil
		}
		worked, err := engine.Tick(ctx)
		if err != nil {
			if ctx.Err() != nil && errors.Is(err, ctx.Err()) && !errors.Is(err, errFinalization) {
				return nil
			}
			return err
		}
		if worked {
			continue
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
