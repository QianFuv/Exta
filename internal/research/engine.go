// Package research owns durable submission, observation and financial uncertainty.
package research

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"exta/internal/domain"
	"exta/internal/providers"
	"exta/internal/store"
)

// Engine shares persisted ownership and admission across every transport and worker.
type Engine struct {
	Store     *store.Store
	Clients   map[domain.Provider]*providers.Client
	PollDelay func(int) time.Duration
}

type record struct {
	Job                       domain.Job                       `json:"job"`
	ReservationID             string                           `json:"reservation_id"`
	IntentDigest              string                           `json:"intent_digest"`
	Attempts                  int                              `json:"create_attempts"`
	MaxPolls                  int                              `json:"max_polls"`
	TotalPolls                int                              `json:"total_polls"`
	CancelPending             bool                             `json:"cancel_pending"`
	CancelSent                bool                             `json:"cancel_sent"`
	ObservationStopped        bool                             `json:"observation_stopped"`
	Action                    string                           `json:"action"`
	RawID                     string                           `json:"-"`
	Lease                     string                           `json:"-"`
	NextPoll                  int64                            `json:"-"`
	LeaseUntil                int64                            `json:"-"`
	Credential                string                           `json:"-"`
	InputCipher, ResultCipher []byte                           `json:"-"`
	Snapshot                  domain.ResearchExecutionSnapshot `json:"-"`
	DispatchReservation       store.Reservation                `json:"-"`
}

const recordColumns = "data,upstream_id,lease_token,next_poll_ms,lease_until_ms,credential_version_id,input_cipher,result_cipher,execution_snapshot"

type scanner interface{ Scan(...any) error }

func decodeRecord(row scanner) (record, error) {
	var item record
	var data, snapshot string
	var upstream, lease sql.NullString
	err := row.Scan(&data, &upstream, &lease, &item.NextPoll, &item.LeaseUntil, &item.Credential, &item.InputCipher, &item.ResultCipher, &snapshot)
	if err != nil {
		return item, store.Missing(err)
	}
	if json.Unmarshal([]byte(data), &item) != nil || json.Unmarshal([]byte(snapshot), &item.Snapshot) != nil {
		return item, store.Reject("storage_unavailable", 503, "Research record is unreadable")
	}
	item.RawID = upstream.String
	item.Lease = lease.String
	return item, nil
}

func readTx(ctx context.Context, tx *sql.Tx, id, owner string) (record, error) {
	return decodeRecord(tx.QueryRowContext(ctx, "SELECT "+recordColumns+" FROM research_jobs WHERE id=? AND (?='' OR principal_id=?)", id, owner, owner))
}

func (engine *Engine) read(ctx context.Context, id, owner string) (record, error) {
	return decodeRecord(engine.Store.DB.QueryRowContext(ctx, "SELECT "+recordColumns+" FROM research_jobs WHERE id=? AND (?='' OR principal_id=?)", id, owner, owner))
}

func pointer[T any](value T) *T { return &value }

func terminal(state domain.JobState) bool {
	return state == "succeeded" || state == "failed" || state == "cancelled"
}

func publicFault(code, message, id string) *domain.ErrorDetail {
	return &domain.ErrorDetail{Code: domain.ErrorCode(code), Message: message, RequestId: id, Details: json.RawMessage(`{}`)}
}

func (engine *Engine) expose(item record, content bool) (domain.Job, error) {
	job := item.Job
	if content && len(item.ResultCipher) > 0 {
		if engine.Store.Vault == nil {
			return job, store.Reject("secret_store_locked", 503, "Secret store is locked")
		}
		data, err := engine.Store.Vault.Open(job.Id+"/result", item.ResultCipher)
		if err != nil {
			return job, store.Reject("secret_store_locked", 503, "Research result cannot be decrypted")
		}
		var report domain.ResearchReport
		if json.Unmarshal(data, &report) != nil {
			return job, store.Reject("storage_unavailable", 503, "Research result is unreadable")
		}
		job.Result = &report
	}
	return job, nil
}

func (engine *Engine) saveTx(ctx context.Context, tx *sql.Tx, item *record, from domain.JobState, reason, actor string) error {
	hold, err := store.ReservationTx(ctx, tx, item.ReservationID)
	if err != nil {
		return err
	}
	item.Job.ActiveSlotHeld = hold.ActiveSlot
	item.Job.BudgetHoldState = domain.BudgetHoldState(hold.State)
	item.Job.CapacityReleaseBasis = domain.CapacityReleaseBasis(hold.CapacityBasis)
	item.Job.Version++
	item.Job.UpdatedAt = store.Stamp(engine.Store.Now())
	item.Job.Result = nil
	if terminal(item.Job.State) && !hold.ActiveSlot && hold.State != "held" {
		created, err := time.Parse(time.RFC3339Nano, item.Job.CreatedAt)
		if err != nil {
			return err
		}
		expires := store.Stamp(created.Add(90 * 24 * time.Hour))
		item.Job.IdempotencyExpiresAt = &expires
		if _, err = tx.ExecContext(ctx, "UPDATE idempotency_records SET expires_at=? WHERE job_id=?", expires, item.Job.Id); err != nil {
			return err
		}
	}
	if err = domain.ValidateJSON("Job", []byte(store.JSON(item.Job))); err != nil {
		return store.Reject("storage_unavailable", 503, "Research transition violates the public contract")
	}
	_, err = tx.ExecContext(ctx, "UPDATE research_jobs SET state=?,upstream_id=?,data=?,result_cipher=?,next_poll_ms=?,lease_token=?,lease_until_ms=?,version=?,updated_at=? WHERE id=?", item.Job.State, nullable(item.RawID), store.JSON(item), item.ResultCipher, item.NextPoll, nullable(item.Lease), item.LeaseUntil, item.Job.Version, item.Job.UpdatedAt, item.Job.Id)
	if err != nil {
		return err
	}
	return engine.eventTx(ctx, tx, item.Job, from, reason, actor)
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func (engine *Engine) eventTx(ctx context.Context, tx *sql.Tx, job domain.Job, from domain.JobState, reason, actor string) error {
	event := domain.JobEvent{Id: store.ID("event"), CreatedAt: store.Stamp(engine.Store.Now()), ToState: job.State, Reason: reason}
	if from != "" {
		event.FromState = &from
	}
	if actor != "" {
		event.ActorPrincipalId = &actor
	}
	_, err := tx.ExecContext(ctx, "INSERT INTO job_events VALUES(?,?,?,?)", event.Id, job.Id, event.CreatedAt, store.JSON(event))
	return err
}

// Get returns local evidence only; it never polls a provider while serving metadata.
func (engine *Engine) Get(ctx context.Context, owner, id, cursor string, limit int) (domain.JobDetail, error) {
	result := domain.JobDetail{Events: []domain.JobEvent{}}
	item, err := engine.read(ctx, id, owner)
	if err != nil {
		return result, err
	}
	result.Job, err = engine.expose(item, true)
	if err != nil {
		return result, err
	}
	if limit == 0 {
		limit = 25
	}
	if limit < 1 || limit > 100 {
		return result, store.Reject("invalid_request", 400, "Invalid event page limit")
	}
	boundary, err := store.CursorBoundary(cursor)
	if err != nil {
		return result, err
	}
	rows, err := engine.Store.DB.QueryContext(ctx, "SELECT data,created_at || '/' || id FROM job_events WHERE job_id=? AND (?='' OR created_at || '/' || id < ?) ORDER BY created_at DESC,id DESC LIMIT ?", id, boundary, boundary, limit+1)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	last := ""
	for rows.Next() {
		var data, order string
		if err = rows.Scan(&data, &order); err != nil {
			return result, err
		}
		if len(result.Events) == limit {
			result.EventsNextCursor = pointer(base64.RawURLEncoding.EncodeToString([]byte(last)))
			break
		}
		event, err := store.Decode[domain.JobEvent](data)
		if err != nil {
			return result, err
		}
		result.Events = append(result.Events, event)
		last = order
	}
	return result, rows.Err()
}

// List applies owner and state filters before bounded local pagination.
func (engine *Engine) List(ctx context.Context, owner, state, cursor string, limit int) (domain.JobList, error) {
	result := domain.JobList{Items: []domain.Job{}}
	if limit == 0 {
		limit = 25
	}
	if limit < 1 || limit > 100 {
		return result, store.Reject("invalid_request", 400, "Invalid job page limit")
	}
	if state != "" && domain.ValidateJSON("JobState", []byte(store.JSON(state))) != nil {
		return result, store.Reject("invalid_request", 400, "Invalid job state")
	}
	boundary, err := store.CursorBoundary(cursor)
	if err != nil {
		return result, err
	}
	rows, err := engine.Store.DB.QueryContext(ctx, "SELECT data,created_at || '/' || id FROM research_jobs WHERE (?='' OR principal_id=?) AND (?='' OR state=?) AND (?='' OR created_at || '/' || id < ?) ORDER BY created_at DESC,id DESC LIMIT ?", owner, owner, state, state, boundary, boundary, limit+1)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	last := ""
	for rows.Next() {
		var data, order string
		if err = rows.Scan(&data, &order); err != nil {
			return result, err
		}
		if len(result.Items) == limit {
			result.NextCursor = pointer(base64.RawURLEncoding.EncodeToString([]byte(last)))
			break
		}
		var item record
		if json.Unmarshal([]byte(data), &item) != nil {
			return result, store.Reject("storage_unavailable", 503, "Research metadata is unreadable")
		}
		result.Items = append(result.Items, item.Job)
		last = order
	}
	return result, rows.Err()
}

func versionMatches(item record, expected domain.Integer) error {
	if item.Job.Version != expected {
		return store.Reject("revision_conflict", 409, "Research job changed; reload before acting")
	}
	return nil
}

func isMissing(err error) bool {
	var fault *store.Fault
	return errors.As(err, &fault) && fault.Code == "not_found"
}
