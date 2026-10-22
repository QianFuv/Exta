package store

import (
	"context"
	"database/sql"
	"time"

	"exta/internal/domain"
)

// Cleanup removes one bounded batch per category while retaining financial evidence.
// Ledger entries, reservations, job identities and credential versions are never purged.
func (state *Store) Cleanup(ctx context.Context, batch int) (map[string]int64, error) {
	counts := map[string]int64{}
	if batch < 1 || batch > 100 {
		return counts, Reject("invalid_request", 400, "Cleanup batch must be between 1 and 100")
	}
	err := state.Write(ctx, func(tx *sql.Tx) error {
		var data string
		if err := tx.QueryRowContext(ctx, "SELECT data FROM settings WHERE id=1").Scan(&data); err != nil {
			return err
		}
		settings, err := Decode[domain.Settings](data)
		if err != nil {
			return err
		}
		if err = domain.ValidateJSON("Retention", []byte(JSON(settings.Retention))); err != nil {
			return err
		}
		now := state.Now()
		cutoff := func(days domain.Integer) string { return Stamp(now.Add(-time.Duration(days) * 24 * time.Hour)) }
		resolvedJob := resolvedResearch
		resolvedRequest := `created_at<? AND json_extract(data,'$.status') NOT IN ('accepted','running') AND json_extract(data,'$.completed_at') IS NOT NULL AND NOT EXISTS(SELECT 1 FROM budget_reservations b WHERE b.parent_id=requests.id AND (b.state='held' OR b.active_slot<>0))`
		steps := []struct {
			name, query string
			arguments   []any
		}{
			{"sessions", "DELETE FROM admin_sessions WHERE token_hash IN (SELECT token_hash FROM admin_sessions WHERE expires_at<? LIMIT ?)", []any{Stamp(now), batch}},
			{"login_limits", "DELETE FROM login_limits WHERE address IN (SELECT address FROM login_limits WHERE window_ms<? LIMIT ?)", []any{now.Add(-24 * time.Hour).UnixMilli(), batch}},
			{"attempts", "DELETE FROM attempts WHERE id IN (SELECT id FROM attempts WHERE parent_id IN (SELECT id FROM requests WHERE " + resolvedRequest + ") LIMIT ?)", []any{cutoff(settings.Retention.RequestDays), batch}},
			{"requests", "DELETE FROM requests WHERE id IN (SELECT id FROM requests WHERE " + resolvedRequest + " AND NOT EXISTS(SELECT 1 FROM attempts WHERE parent_id=requests.id) LIMIT ?)", []any{cutoff(settings.Retention.RequestDays), batch}},
			{"research_content", "UPDATE research_jobs SET input_cipher=x'',result_cipher=NULL,data=json_set(data,'$.content_purged',json('true')) WHERE id IN (SELECT id FROM research_jobs WHERE " + resolvedJob + " AND updated_at<? AND (length(input_cipher)>0 OR result_cipher IS NOT NULL) LIMIT ?)", []any{cutoff(settings.Retention.ResearchContentDays), batch}},
			{"job_events", "DELETE FROM job_events WHERE id IN (SELECT id FROM job_events WHERE created_at<? AND job_id IN (SELECT id FROM research_jobs WHERE " + resolvedJob + ") LIMIT ?)", []any{cutoff(settings.Retention.IdempotencyDays), batch}},
			{"idempotency", "DELETE FROM idempotency_records WHERE rowid IN (SELECT rowid FROM idempotency_records WHERE expires_at<? AND job_id IN (SELECT id FROM research_jobs WHERE " + resolvedJob + " AND created_at<?) LIMIT ?)", []any{Stamp(now), cutoff(settings.Retention.IdempotencyDays), batch}},
			{"usage_snapshots", `DELETE FROM usage_snapshots WHERE id IN (SELECT id FROM usage_snapshots current WHERE observed_at<? AND id<>(SELECT id FROM usage_snapshots WHERE account_id=current.account_id AND key_id IS current.key_id ORDER BY observed_at DESC,rowid DESC LIMIT 1) AND id IS NOT (SELECT id FROM usage_snapshots WHERE account_id=current.account_id AND key_id IS current.key_id AND json_extract(data,'$.used') IS NOT NULL ORDER BY observed_at DESC,rowid DESC LIMIT 1) LIMIT ?)`, []any{cutoff(90), batch}},
			{"audit", "DELETE FROM audit_events WHERE id IN (SELECT id FROM audit_events WHERE created_at<? AND NOT EXISTS(SELECT 1 FROM research_jobs WHERE research_jobs.id=audit_events.entity_id AND NOT (" + resolvedJob + ")) LIMIT ?)", []any{cutoff(settings.Retention.AuditDays), batch}},
		}
		total := int64(0)
		for _, step := range steps {
			result, err := tx.ExecContext(ctx, step.query, step.arguments...)
			if err != nil {
				return err
			}
			count, err := result.RowsAffected()
			if err != nil {
				return err
			}
			counts[step.name], total = count, total+count
		}
		if total == 0 {
			return nil
		}
		if err = AdvanceActivityTx(ctx, tx); err != nil {
			return err
		}
		return AuditTx(ctx, tx, "system", "retention.cleanup", "retention", counts, now)
	})
	if err != nil {
		return nil, err
	}
	return counts, nil
}
