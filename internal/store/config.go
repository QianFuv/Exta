package store

import (
	"context"
	"database/sql"
	"time"

	"exta/internal/domain"
)

func timeFromMillis(value int64) time.Time { return time.UnixMilli(value) }

// Settings returns persisted limits; callers add fresh local readiness/build diagnostics.
func (state *Store) Settings(ctx context.Context) (domain.Settings, error) {
	var data string
	if err := state.DB.QueryRowContext(ctx, "SELECT data FROM settings WHERE id=1").Scan(&data); err != nil {
		return domain.Settings{}, err
	}
	return Decode[domain.Settings](data)
}

// SaveSettings cannot mutate protected network, data or encryption configuration.
func (state *Store) SaveSettings(ctx context.Context, actor string, input domain.SettingsPatch) (domain.Settings, error) {
	var item domain.Settings
	err := state.Write(ctx, func(tx *sql.Tx) error {
		var data string
		if err := tx.QueryRowContext(ctx, "SELECT data FROM settings WHERE id=1").Scan(&data); err != nil {
			return err
		}
		var err error
		item, err = Decode[domain.Settings](data)
		if err != nil {
			return err
		}
		if item.Version != input.ExpectedVersion {
			return Reject("revision_conflict", 409, "Settings changed; reload before saving")
		}
		if input.Limits != nil {
			item.Limits = *input.Limits
		}
		if input.Retention != nil {
			item.Retention = *input.Retention
		}
		item.Version++
		if _, err = tx.ExecContext(ctx, "UPDATE settings SET data=?,version=? WHERE id=1", JSON(item), item.Version); err != nil {
			return err
		}
		return AuditTx(ctx, tx, actor, "settings.update", "settings", map[string]any{"version": item.Version}, state.Now())
	})
	return item, err
}

// Route returns the latest immutable route revision.
func (state *Store) Route(ctx context.Context, operation string) (domain.RoutePolicy, error) {
	var data string
	if err := state.DB.QueryRowContext(ctx, "SELECT data FROM route_policies WHERE capability=? ORDER BY revision DESC LIMIT 1", operation).Scan(&data); err != nil {
		return domain.RoutePolicy{}, Missing(err)
	}
	return Decode[domain.RoutePolicy](data)
}

// Routes returns exactly the six seeded operation policies, even with no keys.
func (state *Store) Routes(ctx context.Context) ([]domain.RoutePolicy, error) {
	rows, err := state.DB.QueryContext(ctx, `SELECT data FROM route_policies r WHERE revision=(SELECT max(revision) FROM route_policies WHERE capability=r.capability) ORDER BY capability`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.RoutePolicy{}
	for rows.Next() {
		var data string
		if err = rows.Scan(&data); err != nil {
			return nil, err
		}
		item, err := Decode[domain.RoutePolicy](data)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// SaveRoute appends a revision and leaves already admitted work untouched.
func (state *Store) SaveRoute(ctx context.Context, actor, operation string, input domain.RouteReplace) (domain.RoutePolicy, error) {
	var item domain.RoutePolicy
	err := state.Write(ctx, func(tx *sql.Tx) error {
		var data string
		if err := tx.QueryRowContext(ctx, "SELECT data FROM route_policies WHERE capability=? ORDER BY revision DESC LIMIT 1", operation).Scan(&data); err != nil {
			return Missing(err)
		}
		var err error
		item, err = Decode[domain.RoutePolicy](data)
		if err != nil {
			return err
		}
		if item.Revision != input.ExpectedVersion {
			return Reject("revision_conflict", 409, "Route changed; reload before saving")
		}
		item.Revision++
		item.Enabled = input.Enabled
		item.Targets = input.Targets
		item.MaxAttempts = input.MaxAttempts
		item.DeadlineSeconds = input.DeadlineSeconds
		item.AllowDuplicateCost = input.AllowDuplicateCost
		item.CreatedAt = Stamp(state.Now())
		if _, err = tx.ExecContext(ctx, "INSERT INTO route_policies VALUES(?,?,?)", operation, item.Revision, JSON(item)); err != nil {
			return err
		}
		return AuditTx(ctx, tx, actor, "route.replace", operation, map[string]any{"revision": item.Revision}, state.Now())
	})
	return item, err
}
