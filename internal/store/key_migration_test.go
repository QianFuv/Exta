package store_test

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"exta/internal/domain"
	"exta/internal/store"
)

func TestKeyMigrationPreservesSharingHistoryAndPendingWork(t *testing.T) {
	state, owner := openStore(t)
	account, first := addAccount(t, state, owner, 1000000)
	second, err := state.AddKey(t.Context(), owner, domain.KeyCreate{AccountId: account.Id, Label: "Second synthetic key", Enabled: true, Secret: "synthetic-second-migration-key"})
	if err != nil {
		t.Fatal(err)
	}
	empty, err := state.CreateAccount(t.Context(), owner, domain.AccountCreate{Provider: "tavily", Label: "Empty archived configuration", Enabled: false, QuotaPolicy: domain.QuotaPolicy{Unit: "tavily_credit", Allocation: 123, PerOperationReservation: 1, ActiveJobs: 1}})
	if err != nil {
		t.Fatal(err)
	}
	var credential string
	if err = state.DB.QueryRow("SELECT active_version FROM provider_keys WHERE id=?", first.Id).Scan(&credential); err != nil {
		t.Fatal(err)
	}
	states := []string{"queued", "submitting", "submission_unknown", "running", "cancel_requested", "succeeded"}
	expectedJobs := map[string]string{}
	for index, status := range states {
		job := store.ID("job")
		hold := store.ID("hold")
		cipher, err := state.Vault.Seal(job+"/input", []byte(`{"query":"synthetic only"}`))
		if err != nil {
			t.Fatal(err)
		}
		stamp := store.Stamp(time.Now())
		snapshot := store.JSON(map[string]any{"account_id": account.Id, "credential_version_id": credential, "reserved_amount": 10, "route_policy": map[string]any{"revision": 7}})
		expectedJobs[job] = store.JSON(map[string]any{"state": status, "upstream_id": "upstream-" + status, "snapshot": store.JSON(map[string]any{"association_ref": account.Id, "credential_version_id": credential, "reserved_amount": 10, "route_policy": map[string]any{"revision": 7}}), "cipher": cipher})
		err = state.Write(t.Context(), func(tx *sql.Tx) error {
			if _, err := tx.Exec("INSERT INTO budget_reservations(id,parent_id,principal_id,account_id,key_id,credential_version_id,unit,amount,state,active_slot,created_at,account_gate_generation,key_gate_generation) VALUES(?,?,?,?,?,?,'micro_usd',10,'held',1,?,4,5)", hold, job, owner, account.Id, first.Id, credential, stamp); err != nil {
				return err
			}
			if _, err := tx.Exec("INSERT INTO research_jobs(id,principal_id,provider,account_id,key_id,credential_version_id,state,upstream_id,data,input_cipher,execution_snapshot,next_poll_ms,lease_token,lease_until_ms,version,created_at,updated_at) VALUES(?,?,'exa',?,?,?,?,?,'{}',?,?,123,'original-lease',456,7,?,?)", job, owner, account.Id, first.Id, credential, status, "upstream-"+status, cipher, snapshot, stamp, stamp); err != nil {
				return err
			}
			if _, err := tx.Exec("INSERT INTO idempotency_records VALUES(?,'research_create',?,?,'frozen-replay',?,NULL)", owner, job, "intent-"+job, job); err != nil {
				return err
			}
			if index == 0 {
				_, err = tx.Exec("INSERT INTO usage_ledger VALUES('charge-known',?,'charge',1,?,?,'micro_usd',7,'verified',?)", hold, account.Id, owner, stamp)
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err = state.DB.Exec("INSERT INTO usage_ledger VALUES('charge-unattributed','legacy-source','charge',1,?,?,'micro_usd',13,'estimated',?)", account.Id, owner, store.Stamp(time.Now())); err != nil {
		t.Fatal(err)
	}
	if _, err = state.DB.Exec("UPDATE runtime_gates SET blocked=1,generation=4,cooldown_ms=987,probe_owner='original-probe' WHERE scope='account' AND entity_id=?", account.Id); err != nil {
		t.Fatal(err)
	}
	if _, err = state.DB.Exec("INSERT INTO rate_windows VALUES('account',?,321,4321)", account.Id); err != nil {
		t.Fatal(err)
	}
	usage := store.JSON(map[string]any{"account_id": account.Id, "key_id": nil, "used": 71, "limit": 900, "source": "upstream", "freshness": "stale"})
	if _, err = state.DB.Exec("INSERT INTO usage_snapshots VALUES('old-observation',?,NULL,?,?)", account.Id, store.Stamp(time.Now()), usage); err != nil {
		t.Fatal(err)
	}
	disabled := false
	account, err = state.PatchAccount(t.Context(), owner, account.Id, domain.AccountPatch{ExpectedVersion: account.Version, Enabled: &disabled})
	if err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(t.TempDir(), "verified-backup.sqlite")
	if err = store.Snapshot(t.Context(), state.Path, backup); err != nil {
		t.Fatal(err)
	}
	if inspection, err := store.InspectExisting(t.Context(), backup, state.Vault); err != nil || !inspection.Unlocked {
		t.Fatalf("backup verification: %+v %v", inspection, err)
	}
	path, vault := state.Path, state.Vault
	state.Close()
	if err = store.MigrateKeySchema(t.Context(), path, vault); err != nil {
		t.Fatal(err)
	}
	inspection, err := store.InspectExisting(t.Context(), path, vault)
	if err != nil || inspection.SchemaVersion != 2 || !inspection.Unlocked || inspection.Generation != 1 {
		t.Fatalf("migrated inspection: %+v %v", inspection, err)
	}
	if err = store.MigrateKeySchema(t.Context(), path, vault); err != nil {
		t.Fatal("repeat migration", err)
	}
	afterRepeat, err := store.InspectExisting(t.Context(), path, vault)
	if err != nil || afterRepeat != inspection {
		t.Fatalf("repeat migration changed durable identity: before=%+v after=%+v err=%v", inspection, afterRepeat, err)
	}
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	checks := map[string]int64{
		"SELECT count(*) FROM provider_keys WHERE json_extract(data,'$.enabled')=0": 2,
		"SELECT count(*) FROM schema_migrations":                                    2,
		"SELECT count(*) FROM sqlite_schema WHERE name='provider_accounts'":         0,
		"SELECT count(*) FROM legacy_provider_accounts":                             2,
		"SELECT count(*) FROM provider_keys WHERE json_extract(data,'$.association_evidence')='legacy_operator_declared' AND json_extract(data,'$.inherited_shared_limit.allocation')=1000000": 2,
		"SELECT count(DISTINCT association_ref) FROM provider_keys":                                                                                        1,
		"SELECT sum(amount) FROM budget_reservations WHERE state='held'":                                                                                   60,
		"SELECT sum(active_slot) FROM budget_reservations":                                                                                                 6,
		"SELECT sum(amount) FROM usage_ledger":                                                                                                             20,
		"SELECT count(*) FROM usage_ledger WHERE key_id IS NULL AND id='charge-unattributed'":                                                              1,
		"SELECT count(*) FROM usage_ledger WHERE key_id IS NOT NULL AND id='charge-known'":                                                                 1,
		"SELECT count(*) FROM research_jobs WHERE lease_token='original-lease' AND next_poll_ms=123 AND lease_until_ms=456 AND version=7":                  6,
		"SELECT count(*) FROM idempotency_records WHERE replay_context='frozen-replay'":                                                                    6,
		"SELECT count(*) FROM legacy_record_payloads WHERE kind='research_job'":                                                                            6,
		"SELECT count(*) FROM runtime_gates WHERE scope='association' AND blocked=1 AND generation=4 AND cooldown_ms=987 AND probe_owner='original-probe'": 1,
		"SELECT used FROM rate_windows WHERE scope='association'":                                                                                          4321,
	}
	for query, wanted := range checks {
		var actual int64
		if err = database.QueryRow(query).Scan(&actual); err != nil || actual != wanted {
			t.Errorf("%s: want %d got %d (%v)", query, wanted, actual, err)
		}
	}
	for job, expected := range expectedJobs {
		var status, upstream, snapshot string
		var cipher []byte
		if err = database.QueryRow("SELECT state,upstream_id,execution_snapshot,input_cipher FROM research_jobs WHERE id=?", job).Scan(&status, &upstream, &snapshot, &cipher); err != nil {
			t.Fatal(err)
		}
		var parsed map[string]any
		if err = json.Unmarshal([]byte(snapshot), &parsed); err != nil {
			t.Fatal(err)
		}
		actual := store.JSON(map[string]any{"state": status, "upstream_id": upstream, "snapshot": store.JSON(parsed), "cipher": cipher})
		if actual != expected {
			t.Errorf("frozen execution evidence changed for %s: %s", job, actual)
		}
	}
	var attributed, originalUsage, migratedUsage string
	if err = database.QueryRow("SELECT key_id FROM usage_ledger WHERE id='charge-known'").Scan(&attributed); err != nil || attributed != first.Id {
		t.Fatal("known charge attribution changed", err)
	}
	if err = database.QueryRow("SELECT data FROM legacy_record_payloads WHERE kind='usage_snapshot' AND id='old-observation'").Scan(&originalUsage); err != nil || originalUsage != usage {
		t.Fatal("original usage evidence changed", err)
	}
	if err = database.QueryRow("SELECT data FROM usage_snapshots WHERE id='old-observation'").Scan(&migratedUsage); err != nil {
		t.Fatal(err)
	}
	var observation map[string]any
	if err = json.Unmarshal([]byte(migratedUsage), &observation); err != nil || observation["association_ref"] != account.Id || observation["used"] != float64(71) || observation["key_id"] != nil || observation["freshness"] != "stale" {
		t.Fatal("shared usage observation changed", err)
	}
	var archived string
	if err = database.QueryRow("SELECT data FROM legacy_provider_accounts WHERE id=?", empty.Id).Scan(&archived); err != nil || archived != store.JSON(empty) {
		t.Fatal("empty account archive changed", err)
	}
	for _, key := range []domain.ProviderKey{first, second} {
		var cipher []byte
		var version string
		if err = database.QueryRow("SELECT c.id,c.ciphertext FROM credential_versions c JOIN provider_keys k ON k.active_version=c.id WHERE k.id=?", key.Id).Scan(&version, &cipher); err != nil {
			t.Fatal(err)
		}
		if _, err = vault.Open(version, cipher); err != nil {
			t.Fatal("retained credential authentication", err)
		}
	}
}
