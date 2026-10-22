package research

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"exta/internal/domain"
	"exta/internal/providers"
	"exta/internal/store"
)

// Cancel records local observation or upstream cancellation intent without claiming a refund.
func (engine *Engine) Cancel(ctx context.Context, owner, actor, id string, input domain.CancelRequest) (domain.Job, error) {
	var item record
	if domain.ValidateJSON("CancelRequest", []byte(store.JSON(input))) != nil {
		return item.Job, store.Reject("invalid_request", 400, "Invalid cancellation request")
	}
	err := engine.Store.Write(ctx, func(tx *sql.Tx) error {
		var err error
		item, err = readTx(ctx, tx, id, owner)
		if err != nil {
			return err
		}
		if err = versionMatches(item, input.ExpectedVersion); err != nil {
			return err
		}
		if terminal(item.Job.State) {
			return nil
		}
		from := item.Job.State
		if item.Job.State == "queued" {
			item.Job.State = "cancelled"
			item.Job.UpstreamMayContinue = false
			item.Job.CompletionReason = pointer("cancelled_before_submission")
			if err = store.ReleaseUnsentTx(ctx, tx, item.ReservationID); err != nil {
				return err
			}
		} else if input.Mode == "observation" {
			item.ObservationStopped = true
			item.Job.State = "observation_expired"
			item.Job.UpstreamMayContinue = true
			item.Job.Uncertainty = true
			item.Job.CompletionReason = pointer("observation_stopped")
		} else {
			if item.Job.SourceProvider != "exa" {
				return store.Reject("cancel_not_supported", 409, "Provider does not offer upstream cancellation; stopping observation leaves upstream work running")
			}
			item.CancelPending = true
			if item.RawID != "" {
				item.Job.State = "cancel_requested"
				item.ObservationStopped = false
			}
			item.NextPoll = engine.Store.Now().UnixMilli()
		}
		if err = engine.saveTx(ctx, tx, &item, from, "cancel_"+input.Mode, actor); err != nil {
			return err
		}
		return store.AuditTx(ctx, tx, actor, "research.cancel", id, map[string]any{"mode": input.Mode, "version": item.Job.Version}, engine.Store.Now())
	})
	if err != nil {
		return domain.Job{}, err
	}
	return engine.expose(item, true)
}

// Resume extends observation of the same identity and reuses existing holds.
func (engine *Engine) Resume(ctx context.Context, owner, actor, id string, input domain.ResumeRequest) (domain.Job, error) {
	var item record
	if domain.ValidateJSON("ResumeRequest", []byte(store.JSON(input))) != nil {
		return item.Job, store.Reject("invalid_request", 400, "Invalid observation window")
	}
	err := engine.Store.Write(ctx, func(tx *sql.Tx) error {
		var err error
		item, err = readTx(ctx, tx, id, owner)
		if err != nil {
			return err
		}
		if err = versionMatches(item, input.ExpectedVersion); err != nil {
			return err
		}
		if item.Job.State != "observation_expired" && item.Job.State != "submission_unknown" {
			return store.Reject("job_state_conflict", 409, "Only stopped or uncertain observation can be resumed")
		}
		if item.Lease != "" && item.LeaseUntil > engine.Store.Now().UnixMilli() {
			return store.Reject("job_state_conflict", 409, "An existing action is still completing")
		}
		if item.RawID == "" && item.Job.SourceProvider != "exa" {
			return store.Reject("job_state_conflict", 409, "Provider cannot discover an unknown task; reconciliation is required")
		}
		settings, err := store.SettingsTx(ctx, tx)
		if err != nil {
			return err
		}
		if input.ObservationSeconds > settings.Limits.ObservationSeconds {
			return store.Reject("invalid_request", 400, "Observation exceeds the configured limit")
		}
		from := item.Job.State
		item.ObservationStopped = false
		item.Job.ObservationDeadline = store.Stamp(engine.Store.Now().Add(time.Duration(input.ObservationSeconds) * time.Second))
		item.Job.PollingCount = 0
		item.MaxPolls = int(settings.Limits.MaxPolls)
		item.Job.CompletionReason = nil
		item.NextPoll = engine.Store.Now().UnixMilli()
		item.Action = ""
		item.Lease = ""
		item.LeaseUntil = 0
		item.Job.State = "accepted"
		if item.RawID == "" {
			item.Job.State = "submission_unknown"
		} else if item.CancelPending {
			item.Job.State = "cancel_requested"
		}
		if err = engine.saveTx(ctx, tx, &item, from, "observation_resumed", actor); err != nil {
			return err
		}
		return store.AuditTx(ctx, tx, actor, "research.resume", id, map[string]any{"seconds": input.ObservationSeconds}, engine.Store.Now())
	})
	if err != nil {
		return domain.Job{}, err
	}
	return engine.expose(item, true)
}

func (engine *Engine) matches(item record, outcome providers.ResearchOutcome) bool {
	if item.Job.SourceProvider != "exa" || outcome.ID == "" || outcome.CreatedAt.IsZero() {
		return false
	}
	created, err := time.Parse(time.RFC3339Nano, item.Job.CreatedAt)
	if err != nil || outcome.CreatedAt.Before(created.Add(-5*time.Minute)) || outcome.CreatedAt.After(engine.Store.Now().Add(5*time.Minute)) {
		return false
	}
	var request struct {
		Query, Effort string
		Metadata      map[string]string
		Budget        struct {
			MaxCost json.RawMessage `json:"maxCostDollars"`
		}
		OutputSchema json.RawMessage `json:"outputSchema"`
	}
	if json.Unmarshal(outcome.Request, &request) != nil || request.Metadata["exta_job_id"] != item.Job.Id || request.Metadata["exta_intent_digest"] != item.IntentDigest {
		return false
	}
	input, err := engine.input(item)
	if err != nil || request.Query != input.Input || request.Effort != "auto" {
		return false
	}
	var budget domain.HardBudget
	_ = json.Unmarshal(input.Budget, &budget)
	cost := providers.Amount(request.Budget.MaxCost, 1000000)
	if cost == nil || *cost != int64(budget.Amount) {
		return false
	}
	if input.OutputSchema == nil {
		return len(request.OutputSchema) == 0 || string(request.OutputSchema) == "null"
	}
	var left, right any
	if json.Unmarshal(request.OutputSchema, &left) != nil || json.Unmarshal(*input.OutputSchema, &right) != nil {
		return false
	}
	return store.JSON(left) == store.JSON(right)
}

func (engine *Engine) discover(ctx context.Context, item record, credential string, limit int64) (providers.ResearchOutcome, error) {
	var found providers.ResearchOutcome
	cursor := ""
	seen := map[string]bool{}
	for page := 0; page < 5; page++ {
		if page > 0 {
			var err error
			item.DispatchReservation, err = engine.Store.ResearchDispatch(ctx, item.ReservationID, false, item.Lease)
			if err != nil {
				return providers.ResearchOutcome{}, err
			}
		}
		items, next, more, err := engine.Clients[item.Job.SourceProvider].ResearchList(ctx, credential, cursor, limit)
		if err != nil {
			var failure *providers.ResearchListFailure
			if errors.As(err, &failure) {
				return providers.ResearchOutcome{Outcome: failure.Outcome}, nil
			}
			return found, err
		}
		persist, done := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		err = engine.Store.Write(persist, func(tx *sql.Tx) error {
			return engine.applyGatesTx(persist, tx, item, providers.ResearchOutcome{Outcome: providers.Outcome{Status: 200}})
		})
		done()
		if err != nil {
			return providers.ResearchOutcome{}, err
		}
		for _, candidate := range items {
			if engine.matches(item, candidate) && !seen[candidate.ID] {
				if found.ID != "" {
					return providers.ResearchOutcome{}, store.Reject("job_state_conflict", 409, "Multiple upstream tasks match the original submission")
				}
				found = candidate
				seen[candidate.ID] = true
			}
		}
		if !more {
			return found, nil
		}
		if next == cursor {
			return providers.ResearchOutcome{}, store.Reject("upstream_protocol_error", 502, "Research listing cursor did not advance")
		}
		cursor = next
	}
	return providers.ResearchOutcome{}, store.Reject("job_state_conflict", 409, "Bounded listing is incomplete; manual reconciliation is required")
}

// Reconcile independently verifies provider evidence or applies one explicit audited risk action.
// Evidence references are recorded as data and are never fetched as arbitrary URLs.
func (engine *Engine) Reconcile(ctx context.Context, actor, id string, raw []byte) (domain.Job, error) {
	if domain.ValidateJSON("ReconcileRequest", raw) != nil {
		return domain.Job{}, store.Reject("invalid_request", 400, "Invalid reconciliation action")
	}
	var input struct {
		Action, Reason  string
		EvidenceRef     string              `json:"evidence_ref"`
		ExpectedVersion domain.Integer      `json:"expected_version"`
		UpstreamID      string              `json:"upstream_job_id"`
		UpstreamState   string              `json:"upstream_state"`
		Cost            *domain.UsageAmount `json:"cost"`
	}
	_ = json.Unmarshal(raw, &input)
	item, err := engine.read(ctx, id, "")
	if err != nil {
		return item.Job, err
	}
	if err = versionMatches(item, input.ExpectedVersion); err != nil {
		return item.Job, err
	}
	if item.Lease != "" && item.LeaseUntil > engine.Store.Now().UnixMilli() {
		return item.Job, store.Reject("job_state_conflict", 409, "An existing action is still completing")
	}
	if input.Action == "attach_upstream_id" && terminal(item.Job.State) {
		return item.Job, store.Reject("job_state_conflict", 409, "A terminal job cannot attach another task")
	}
	if input.Action == "attach_upstream_id" && item.Job.SourceProvider == "tavily" {
		return item.Job, store.Reject("job_state_conflict", 409, "Tavily lookup cannot independently verify the original submission correlation")
	}
	if item.Job.State == "queued" || item.Job.State == "submitting" {
		return item.Job, store.Reject("job_state_conflict", 409, "Submission must stop or become known before reconciliation")
	}
	var observation providers.ResearchOutcome
	if input.Action == "attach_upstream_id" || input.Action == "settle_terminal" {
		upstream := item.RawID
		if input.Action == "attach_upstream_id" {
			upstream = input.UpstreamID
			if item.RawID != "" && item.RawID != upstream {
				return item.Job, store.Reject("job_state_conflict", 409, "A different provider task is already attached")
			}
		}
		if upstream == "" {
			return item.Job, store.Reject("job_state_conflict", 409, "Independent verification requires a known provider identifier")
		}
		client := engine.Clients[item.Job.SourceProvider]
		if client == nil {
			return item.Job, store.Reject("provider_unavailable", 503, "Provider adapter is unavailable")
		}
		credential, err := engine.Store.Credential(ctx, item.Credential)
		if err != nil {
			return item.Job, err
		}
		settings, err := engine.Store.Settings(ctx)
		if err != nil {
			return item.Job, err
		}
		bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
		if item.DispatchReservation, err = engine.Store.ResearchDispatch(bounded, item.ReservationID, false, store.ID("probe")); err != nil {
			cancel()
			return item.Job, err
		}
		observation = client.ResearchGet(bounded, credential, upstream, int64(settings.Limits.ResponseBytes))
		cancel()
		persist, done := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		err = engine.Store.Write(persist, func(tx *sql.Tx) error { return engine.applyGatesTx(persist, tx, item, observation) })
		done()
		if err != nil {
			return item.Job, err
		}
		if observation.ID != upstream {
			return item.Job, store.Reject("job_state_conflict", 409, "Provider task could not be independently verified")
		}
		if input.Action == "attach_upstream_id" && !engine.matches(item, observation) {
			return item.Job, store.Reject("job_state_conflict", 409, "Provider response cannot verify the original submission correlation")
		}
		if input.Action == "settle_terminal" {
			if observation.State != input.UpstreamState {
				return item.Job, store.Reject("job_state_conflict", 409, "Provider does not confirm the requested terminal state")
			}
			if input.Cost != nil && (input.Cost.Unit != item.Job.Reservation.Unit || input.Cost.Amount == nil || observation.Cost == nil || int64(*input.Cost.Amount) != *observation.Cost) {
				return item.Job, store.Reject("job_state_conflict", 409, "Provider does not confirm the reported cost")
			}
		}
	}
	err = engine.Store.Write(ctx, func(tx *sql.Tx) error {
		var err error
		item, err = readTx(ctx, tx, id, "")
		if err != nil {
			return err
		}
		if err = versionMatches(item, input.ExpectedVersion); err != nil {
			return err
		}
		if item.Lease != "" && item.LeaseUntil > engine.Store.Now().UnixMilli() {
			return store.Reject("job_state_conflict", 409, "An existing action is still completing")
		}
		from := item.Job.State
		switch input.Action {
		case "attach_upstream_id":
			item.RawID = input.UpstreamID
			item.Job.UpstreamJobId = &input.UpstreamID
			item.Job.UpstreamState = &observation.State
			item.Job.State = "accepted"
			if item.ObservationStopped {
				item.Job.State = "observation_expired"
			}
			item.Job.Uncertainty = false
			item.Job.UpstreamMayContinue = true
			item.NextPoll = engine.Store.Now().UnixMilli()
		case "settle_terminal":
			item.Job.State = map[string]domain.JobState{"completed": "succeeded", "failed": "failed", "cancelled": "cancelled"}[observation.State]
			item.Job.UpstreamState = &observation.State
			item.Job.UpstreamMayContinue = false
			item.Job.Uncertainty = input.Cost == nil
			item.Job.CompletionReason = nil
			if observation.StopReason != "" {
				item.Job.CompletionReason = &observation.StopReason
			}
			item.Job.IsPartial = observation.StopReason == "budget_reached" || observation.State == "completed" && observation.Failure != nil
			item.Job.Error = nil
			if observation.Failure != nil {
				item.Job.Error = publicFault(observation.Failure.Code, observation.Failure.Message, item.Job.Id)
			}
			var cost *int64
			if input.Cost != nil {
				cost = pointer(int64(*input.Cost.Amount))
			}
			if observation.Report != nil && !item.ContentPurged {
				item.ResultCipher, err = engine.Store.Vault.Seal(id+"/result", []byte(store.JSON(observation.Report)))
				if err != nil {
					return err
				}
			}
			if err = store.SettleTx(ctx, tx, item.ReservationID, cost, "reported", engine.Store.Now()); err != nil {
				return err
			}
		case "budget_write_off":
			hold, err := store.ReservationTx(ctx, tx, item.ReservationID)
			if err != nil {
				return err
			}
			if hold.State != "held" {
				return store.Reject("job_state_conflict", 409, "Budget hold is already resolved")
			}
			if _, err = tx.ExecContext(ctx, "INSERT INTO usage_ledger VALUES(?,?,'write_off',1,?,?,?,?,?,?)", store.ID("usage"), hold.Id, hold.AccountId, hold.PrincipalId, hold.Unit, hold.Amount, "unknown", store.Stamp(engine.Store.Now())); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, "UPDATE budget_reservations SET state='written_off' WHERE id=?", hold.Id); err != nil {
				return err
			}
			item.Job.Uncertainty = true
		case "capacity_release_override":
			hold, err := store.ReservationTx(ctx, tx, item.ReservationID)
			if err != nil {
				return err
			}
			if !hold.ActiveSlot {
				return store.Reject("job_state_conflict", 409, "Capacity is already resolved")
			}
			if _, err = tx.ExecContext(ctx, "UPDATE budget_reservations SET active_slot=0,capacity_basis='manual_override' WHERE id=?", hold.Id); err != nil {
				return err
			}
			item.Job.UpstreamState = pointer("unknown")
			item.Job.UpstreamMayContinue = true
			item.Job.Uncertainty = true
			item.Job.State = "observation_expired"
			item.ObservationStopped = true
		}
		item.Lease = ""
		item.LeaseUntil = 0
		item.Action = ""
		if err = engine.saveTx(ctx, tx, &item, from, input.Action, actor); err != nil {
			return err
		}
		return store.AuditTx(ctx, tx, actor, "research.reconcile", id, map[string]any{"action": input.Action, "reason": input.Reason, "evidence_ref": input.EvidenceRef, "version": item.Job.Version}, engine.Store.Now())
	})
	if err != nil {
		return domain.Job{}, err
	}
	return engine.expose(item, true)
}
