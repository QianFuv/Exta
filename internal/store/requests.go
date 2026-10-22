package store

import (
	"context"
	"database/sql"
	"encoding/base64"
	"strings"
	"time"

	"exta/internal/domain"
)

// RequestFilter bounds metadata queries without retaining search input or content.
type RequestFilter struct {
	PrincipalId, Operation, Provider, Status, From, To, Cursor string
	Limit                                                      int
}

// StartRequest records admission provenance before any provider transmission.
func (state *Store) StartRequest(ctx context.Context, principal string, item domain.RequestRecord) error {
	return state.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO requests VALUES(?,?,?,?)", item.Id, principal, JSON(item), item.CreatedAt)
		if err != nil {
			return err
		}
		return AdvanceActivityTx(ctx, tx)
	})
}

// StartAttempt durably marks possible transmission before returning control to the network.
func (state *Store) StartAttempt(ctx context.Context, requestId string, item domain.Attempt) error {
	return state.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "INSERT INTO attempts VALUES(?,?,?,?)", item.Id, requestId, item.AttemptNumber, JSON(item)); err != nil {
			return err
		}
		return AdvanceActivityTx(ctx, tx)
	})
}

// FinishAttempt saves evidence and its accounting effect in one transaction.
// Unknown cost retains the reservation even after a synchronous response completes.
func (state *Store) FinishAttempt(ctx context.Context, item domain.Attempt, hold string, isUnspent bool, cost *int64, certainty string) error {
	return state.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "UPDATE attempts SET data=? WHERE id=?", JSON(item), item.Id); err != nil {
			return err
		}
		if isUnspent {
			return ReleaseUnsentTx(ctx, tx, hold)
		}
		return SettleTx(ctx, tx, hold, cost, certainty, state.Now())
	})
}

// FinishRequest persists only the sanitized summary; result bodies are never stored.
func (state *Store) FinishRequest(ctx context.Context, item domain.RequestRecord) error {
	_, err := state.DB.ExecContext(ctx, "UPDATE requests SET data=? WHERE id=?", JSON(item), item.Id)
	return err
}

// CursorBoundary validates the opaque keyset cursor used by metadata lists.
func CursorBoundary(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(decoded) == 0 || len(decoded) > 256 || !strings.Contains(string(decoded), "/") {
		return "", Reject("invalid_request", 400, "Invalid cursor")
	}
	return string(decoded), nil
}

// Requests returns a bounded owner-filtered page with stable descending order.
func (state *Store) Requests(ctx context.Context, filter RequestFilter) (domain.RequestRecordList, error) {
	result := domain.RequestRecordList{Items: []domain.RequestRecord{}}
	boundary, err := CursorBoundary(filter.Cursor)
	if err != nil {
		return result, err
	}
	if filter.Limit == 0 {
		filter.Limit = 25
	}
	if filter.Limit < 1 || filter.Limit > 100 {
		return result, Reject("invalid_request", 400, "Invalid pagination limit")
	}
	query := "SELECT data,created_at || '/' || id FROM requests WHERE 1=1"
	arguments := []any{}
	for _, field := range [][3]string{{"principal_id", filter.PrincipalId, "="}, {"json_extract(data,'$.operation')", filter.Operation, "="}, {"json_extract(data,'$.source_provider')", filter.Provider, "="}, {"json_extract(data,'$.status')", filter.Status, "="}, {"created_at", filter.From, ">="}, {"created_at", filter.To, "<="}, {"created_at || '/' || id", boundary, "<"}} {
		if field[1] != "" {
			query += " AND " + field[0] + field[2] + "?"
			arguments = append(arguments, field[1])
		}
	}
	query += " ORDER BY created_at DESC,id DESC LIMIT ?"
	arguments = append(arguments, filter.Limit+1)
	rows, err := state.DB.QueryContext(ctx, query, arguments...)
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
		if len(result.Items) == filter.Limit {
			cursor := base64.RawURLEncoding.EncodeToString([]byte(last))
			result.NextCursor = &cursor
			break
		}
		item, err := Decode[domain.RequestRecord](data)
		if err != nil {
			return result, err
		}
		result.Items = append(result.Items, item)
		last = order
	}
	return result, rows.Err()
}

// Request hides objects owned by another principal behind the same 404 as missing IDs.
func (state *Store) Request(ctx context.Context, principal, id string) (domain.RequestDetail, error) {
	result := domain.RequestDetail{Attempts: []domain.Attempt{}}
	query := "SELECT data FROM requests WHERE id=?"
	arguments := []any{id}
	if principal != "" {
		query += " AND principal_id=?"
		arguments = append(arguments, principal)
	}
	var data string
	if err := state.DB.QueryRowContext(ctx, query, arguments...).Scan(&data); err != nil {
		return result, Missing(err)
	}
	var err error
	result.Request, err = Decode[domain.RequestRecord](data)
	if err != nil {
		return result, err
	}
	rows, err := state.DB.QueryContext(ctx, "SELECT data FROM attempts WHERE parent_id=? ORDER BY attempt_number", id)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		if err = rows.Scan(&data); err != nil {
			return result, err
		}
		item, err := Decode[domain.Attempt](data)
		if err != nil {
			return result, err
		}
		result.Attempts = append(result.Attempts, item)
	}
	return result, rows.Err()
}

// RecoverRequests closes interrupted synchronous observations without refunding possible work.
func (state *Store) RecoverRequests(ctx context.Context) error {
	return state.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE requests SET data=json_set(data,'$.status','uncertain','$.billing_uncertain',json('true'),'$.completed_at',?) WHERE json_extract(data,'$.status') IN ('accepted','running')`, Stamp(state.Now()))
		return err
	})
}

// Audit returns bounded sanitized audit metadata, never provider secrets or raw input.
func (state *Store) Audit(ctx context.Context, cursor string, limit int) (domain.AuditEventList, error) {
	return state.FilteredAudit(ctx, cursor, limit, "", "")
}

// FilteredAudit applies filters before bounded keyset pagination.
func (state *Store) FilteredAudit(ctx context.Context, cursor string, limit int, action, entity string) (domain.AuditEventList, error) {
	result := domain.AuditEventList{Items: []domain.AuditEvent{}}
	boundary, err := CursorBoundary(cursor)
	if err != nil {
		return result, err
	}
	if limit < 1 || limit > 100 {
		return result, Reject("invalid_request", 400, "Invalid pagination limit")
	}
	if len(action) > 128 || len(entity) > 128 {
		return result, Reject("invalid_request", 400, "Audit filter exceeds the allowed length")
	}
	rows, err := state.DB.QueryContext(ctx, "SELECT id,actor,action,entity_id,created_at,data FROM audit_events WHERE (?='' OR created_at || '/' || id < ?) AND (?='' OR action=?) AND (?='' OR entity_id=?) ORDER BY created_at DESC,id DESC LIMIT ?", boundary, boundary, action, action, entity, entity, limit+1)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	last := ""
	for rows.Next() {
		var item domain.AuditEvent
		var data string
		if err = rows.Scan(&item.Id, &item.ActorPrincipalId, &item.Action, &item.EntityId, &item.CreatedAt, &data); err != nil {
			return result, err
		}
		if len(result.Items) == limit {
			next := base64.RawURLEncoding.EncodeToString([]byte(last))
			result.NextCursor = &next
			break
		}
		item, err = Decode[domain.AuditEvent](data)
		if err != nil {
			return result, err
		}
		result.Items = append(result.Items, item)
		last = item.CreatedAt + "/" + item.Id
	}
	return result, rows.Err()
}

// Overview reports timestamped local counters independently of upstream usage snapshots.
func (state *Store) Overview(ctx context.Context, waiting int) (domain.OverviewMetrics, error) {
	result := domain.OverviewMetrics{ObservedAt: Stamp(state.Now()), WaitingRequests: domain.Integer(waiting)}
	for _, query := range []struct {
		sql         string
		destination *domain.Integer
		arguments   []any
	}{
		{"SELECT count(*) FROM budget_reservations WHERE active_slot=1", &result.ActiveSlotsHeld, nil},
		{"SELECT count(*) FROM research_jobs WHERE state='queued'", &result.QueuedJobs, nil},
		{"SELECT count(*) FROM research_jobs WHERE state='submission_unknown'", &result.UncertainJobs, nil},
		{"SELECT count(*) FROM requests WHERE created_at>=?", &result.RequestsLastMinute, []any{Stamp(state.Now().Add(-time.Minute))}},
		{"SELECT count(*) FROM requests WHERE created_at>=? AND json_extract(data,'$.status') IN ('failed','uncertain')", &result.ErrorsLastMinute, []any{Stamp(state.Now().Add(-time.Minute))}},
	} {
		if err := state.DB.QueryRowContext(ctx, query.sql, query.arguments...).Scan(query.destination); err != nil {
			return result, err
		}
	}
	return result, nil
}
