package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"exta/internal/domain"
	"exta/internal/security"
	"exta/internal/service"
	"exta/internal/store"
)

func TestBrowserFixture(t *testing.T) {
	directory := os.Getenv("EXTA_BROWSER_STATE")
	if directory == "" {
		t.Skip("browser harness supplies isolated synthetic state")
	}
	directory, err := filepath.Abs(directory)
	if err != nil || filepath.Base(directory) != "state" || !strings.HasPrefix(filepath.Base(filepath.Dir(directory)), "exta-browser-") {
		t.Fatal("refusing non-browser fixture directory")
	}
	key, err := os.ReadFile(filepath.Join(directory, "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	vault, err := security.NewVault(key)
	if err != nil {
		t.Fatal(err)
	}
	state, err := store.OpenExisting(t.Context(), filepath.Join(directory, "exta.sqlite"), vault)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	if os.Getenv("EXTA_BROWSER_PHASE") == "verify" {
		var attempts, events int
		if err := state.DB.QueryRow(`SELECT coalesce(sum(json_extract(data,'$.create_attempts')+json_extract(data,'$.job.polling_count')),0) FROM research_jobs`).Scan(&attempts); err != nil {
			t.Fatal(err)
		}
		if err := state.DB.QueryRow(`SELECT count(*) FROM job_events WHERE json_extract(data,'$.reason') LIKE 'worker_%' AND json_extract(data,'$.reason') != 'worker_recovered'`).Scan(&events); err != nil {
			t.Fatal(err)
		}
		if attempts != 0 || events != 0 {
			t.Fatalf("browser fixture dispatched upstream: attempts=%d events=%d", attempts, events)
		}
		var jobs, reservations int
		if err := state.DB.QueryRow("SELECT count(*) FROM research_jobs").Scan(&jobs); err != nil {
			t.Fatal(err)
		}
		if err := state.DB.QueryRow("SELECT count(*) FROM budget_reservations").Scan(&reservations); err != nil {
			t.Fatal(err)
		}
		if jobs != 3 || reservations != 3 {
			t.Fatalf("retry duplicated acceptance: jobs=%d reservations=%d", jobs, reservations)
		}
		return
	}
	var count int
	if err := state.DB.QueryRow("SELECT count(*) FROM research_jobs").Scan(&count); err != nil || count != 0 {
		t.Fatal("fixture requires fresh state")
	}
	var owner string
	if err := state.DB.QueryRow("SELECT id FROM principals LIMIT 1").Scan(&owner); err != nil {
		t.Fatal(err)
	}
	upstreamKey, err := state.AddKey(t.Context(), owner, domain.UpstreamKeyCreate{Provider: "exa", QuotaPolicy: &domain.QuotaPolicy{Unit: "micro_usd", Allocation: 10000000, PerOperationReservation: 1000000, ActiveJobs: 2}, Label: "Invalid offline fixture key", Enabled: true, Secret: "synthetic-invalid-browser-fixture-key"})
	if err != nil {
		t.Fatal(err)
	}
	core, err := service.New(state, nil, security.UrlPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	body := json.RawMessage(`{"input":"Offline browser retry fixture","profile":"exa_auto","budget":{"mode":"hard","unit":"micro_usd","amount":1000000}}`)
	ids := map[string]string{}
	for _, kind := range []string{"report", "unknown", "queued"} {
		principal := owner
		if kind == "unknown" {
			principal = store.ID("principal")
			if _, err := state.DB.Exec("INSERT INTO principals VALUES(?,NULL,'[]',?)", principal, store.Stamp(state.Now())); err != nil {
				t.Fatal(err)
			}
		}
		job, err := core.Research.Accept(t.Context(), principal, "browser-fixture-"+kind, body)
		if err != nil {
			t.Fatal(err)
		}
		ids[kind] = job.Id
		err = state.Write(t.Context(), func(tx *sql.Tx) error {
			var raw, reservation string
			if err := tx.QueryRowContext(t.Context(), "SELECT data,json_extract(data,'$.reservation_id') FROM research_jobs WHERE id=?", job.Id).Scan(&raw, &reservation); err != nil {
				return err
			}
			var record map[string]json.RawMessage
			if err := json.Unmarshal([]byte(raw), &record); err != nil {
				return err
			}
			var cipher []byte
			if kind == "report" {
				cost := int64(1)
				if err := store.SettleTx(t.Context(), tx, reservation, &cost, "reported", state.Now()); err != nil {
					return err
				}
				job.State = "succeeded"
				job.ActiveSlotHeld = false
				job.BudgetHoldState = "settled"
				job.CapacityReleaseBasis = "verified_terminal"
				job.IsPartial = true
				reason := "budget_reached"
				job.CompletionReason = &reason
				markdown := "# Synthetic report\n\n<script>window.extaUnsafe=true</script>\n\n![remote](https://invalid.example/tracker.png)\n\n[Unsafe](javascript:alert(1))\n\n| Item | Value |\n| --- | --- |\n| Safe | Rendered |"
				cipher, err = vault.Seal(job.Id+"/result", []byte(store.JSON(domain.ResearchReport{Markdown: &markdown, Citations: []domain.Citation{}})))
				if err != nil {
					return err
				}
			} else if kind == "unknown" {
				job.State = "observation_expired"
				job.Uncertainty = true
				job.UpstreamMayContinue = true
				reason := "observation_limit"
				job.CompletionReason = &reason
			}
			record["job"] = json.RawMessage(store.JSON(job))
			_, err = tx.ExecContext(t.Context(), "UPDATE research_jobs SET state=?,data=?,result_cipher=?,next_poll_ms=? WHERE id=?", job.State, store.JSON(record), cipher, time.Now().Add(time.Hour).UnixMilli(), job.Id)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, input := range []domain.UpstreamKeyCreate{
		{Provider: "exa", Label: "Exa secondary · offline fixture", Enabled: false, Secret: "synthetic-invalid-secondary-key", QuotaPolicy: &domain.QuotaPolicy{Unit: "micro_usd", Allocation: 5000000, PerOperationReservation: 500000, ActiveJobs: 1}},
		{Provider: "tavily", Label: "Tavily search · offline fixture", Enabled: false, Secret: "synthetic-invalid-tavily-key", QuotaPolicy: &domain.QuotaPolicy{Unit: "tavily_credit", Allocation: 10000, PerOperationReservation: 100, ActiveJobs: 1}},
	} {
		if _, err := state.AddKey(t.Context(), owner, input); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = state.DB.Exec("UPDATE credential_versions SET validity='invalid'"); err != nil {
		t.Fatal(err)
	}
	if _, err = state.DB.Exec("UPDATE runtime_gates SET blocked=1"); err != nil {
		t.Fatal(err)
	}
	requestId := seedBrowserRequests(t, state, owner, upstreamKey.Id)
	metadata := map[string]any{"owner": owner, "jobs": ids, "body": body, "key": "browser-fixture-queued", "upstreamKey": upstreamKey.Id, "request": requestId}
	if err := os.WriteFile(filepath.Join(filepath.Dir(directory), "fixture.json"), []byte(store.JSON(metadata)), 0600); err != nil {
		t.Fatal(err)
	}
}

// seedBrowserRequests supplies sanitized local history without contacting a provider.
func seedBrowserRequests(t *testing.T, state *store.Store, owner, keyId string) string {
	t.Helper()
	var credentialId string
	if err := state.DB.QueryRow("SELECT id FROM credential_versions LIMIT 1").Scan(&credentialId); err != nil {
		t.Fatal(err)
	}
	operations := []domain.Operation{"search", "fetch", "advanced_search", "search", "fetch", "search", "advanced_search", "search"}
	for index, operation := range operations {
		created := store.Stamp(time.Now().Add(-time.Duration(index) * 4 * time.Minute))
		duration, count, failures, status := domain.Integer(236+index*147), domain.Integer(3+index), domain.Integer(0), domain.Integer(200)
		partial, provider := false, domain.Provider("exa")
		id := fmt.Sprintf("request_browser_%02d", index)
		record := domain.RequestRecord{Id: id, Operation: operation, Status: "succeeded", SourceProvider: &provider, RouteRevision: 1, CreatedAt: created, CompletedAt: &created, DurationMs: &duration, ResultCount: &count, FailureCount: &failures, Partial: &partial, Usage: []domain.UsageAmount{}}
		if index == 3 {
			record.Status = "failed"
			count, failures, status = 0, 1, 429
			record.Error = &domain.ErrorDetail{Code: "upstream_rate_limited", Message: "Synthetic provider rate limit", RequestId: id, Retryable: true, Details: json.RawMessage(`{}`)}
		}
		attempt := domain.Attempt{Id: fmt.Sprintf("attempt_browser_%02d", index), KeyId: &keyId, CredentialVersionId: credentialId, Provider: provider, AttemptNumber: 1, CreatedAt: created, DurationMs: duration, HttpStatus: &status, TransmissionState: "response_received"}
		for schema, item := range map[string]any{"RequestRecord": record, "Attempt": attempt} {
			if err := domain.ValidateJSON(schema, []byte(store.JSON(item))); err != nil {
				t.Fatal(err)
			}
		}
		if err := state.StartRequest(t.Context(), owner, record); err != nil {
			t.Fatal(err)
		}
		if err := state.StartAttempt(t.Context(), id, attempt); err != nil {
			t.Fatal(err)
		}
	}
	return "request_browser_00"
}
