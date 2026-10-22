package research

import (
	"net/http"
	"testing"
	"time"

	"exta/internal/providers"
	"exta/internal/store"
)

func TestResearchRetentionPreservesUnknownEvidenceAndFrozenReplay(t *testing.T) {
	for _, knownCost := range []bool{false, true} {
		t.Run(store.JSON(knownCost), func(t *testing.T) {
			engine, owner, now := fixture(t)
			job := acceptJob(t, engine, owner)
			claimed := claimJob(t, engine)
			outcome := providers.ResearchOutcome{ID: "retained_upstream", State: "completed"}
			if knownCost {
				outcome.Cost = pointer(int64(123))
			}
			if err := engine.complete(t.Context(), claimed, outcome); err != nil {
				t.Fatal(err)
			}
			*now = now.Add(31 * 24 * time.Hour)
			counts, err := engine.Store.Cleanup(t.Context(), 100)
			if err != nil {
				t.Fatal(err)
			}
			if (counts["research_content"] == 1) != knownCost {
				t.Fatalf("content cleanup %v", counts)
			}
			replay, err := engine.Accept(t.Context(), owner, "synthetic-idempotency-key", []byte(exaIntent))
			if err != nil || replay.Id != job.Id {
				t.Fatalf("frozen replay lost after content retention: %v", err)
			}
			inspection, err := store.InspectExisting(t.Context(), engine.Store.Path, engine.Store.Vault)
			if err != nil || !inspection.Unlocked {
				t.Fatalf("retained state inspection: %+v %v", inspection, err)
			}
			*now = now.Add(70 * 24 * time.Hour)
			counts, err = engine.Store.Cleanup(t.Context(), 100)
			if err != nil {
				t.Fatal(err)
			}
			if (counts["idempotency"] == 1) != knownCost {
				t.Fatalf("binding cleanup: %v", counts)
			}
			item, err := engine.read(t.Context(), job.Id, owner)
			if err != nil || item.RawID != "retained_upstream" {
				t.Fatalf("upstream identity lost: %v", err)
			}
			if !knownCost && (len(item.InputCipher) == 0 || counts["job_events"] != 0) {
				t.Fatal("unresolved recovery evidence purged")
			}
		})
	}
}

func TestInspectionRejectsEmptyRequiredResearchCiphertext(t *testing.T) {
	for _, field := range []string{"input_cipher", "result_cipher"} {
		t.Run(field, func(t *testing.T) {
			engine, owner, _ := fixture(t)
			job := acceptJob(t, engine, owner)
			if _, err := engine.Store.DB.Exec("UPDATE research_jobs SET "+field+"=x'' WHERE id=?", job.Id); err != nil {
				t.Fatal(err)
			}
			if _, err := store.InspectExisting(t.Context(), engine.Store.Path, engine.Store.Vault); err == nil {
				t.Fatal("empty required ciphertext authenticated")
			}
		})
	}
}

func TestReconciliationDoesNotResurrectPurgedResearchContent(t *testing.T) {
	for _, duringCall := range []bool{false, true} {
		t.Run(store.JSON(duringCall), func(t *testing.T) {
			engine, owner, now := fixture(t)
			job := acceptJob(t, engine, owner)
			claimed := claimJob(t, engine)
			if err := engine.complete(t.Context(), claimed, providers.ResearchOutcome{ID: "retention_1", State: "completed", Outcome: providers.Outcome{Cost: pointer(int64(123))}}); err != nil {
				t.Fatal(err)
			}
			*now = now.Add(31 * 24 * time.Hour)
			purge := func() {
				if _, err := engine.Store.Cleanup(t.Context(), 100); err != nil {
					t.Fatal(err)
				}
			}
			if !duringCall {
				purge()
			}
			engine.Clients["exa"], _ = providers.New("exa", localTransport(func(*http.Request) (*http.Response, error) {
				if duringCall {
					purge()
				}
				return syntheticResponse(200, `{"id":"retention_1","status":"completed","output":{"text":"Must remain deleted"},"costDollars":{"total":0.000123}}`), nil
			}))
			job = localJob(t, engine, job.Id)
			raw := []byte(store.JSON(map[string]any{"action": "settle_terminal", "expected_version": job.Version, "reason": "Verify retained settlement", "evidence_ref": "synthetic-local-proof", "upstream_state": "completed", "cost": map[string]any{"unit": "micro_usd", "amount": 123, "certainty": "reported", "source": "upstream", "observed_at": store.Stamp(*now), "pricing_revision": nil}}))
			if _, err := engine.Reconcile(t.Context(), owner, job.Id, raw); err != nil {
				t.Fatal(err)
			}
			item, err := engine.read(t.Context(), job.Id, owner)
			if err != nil || !item.ContentPurged || len(item.InputCipher) != 0 || len(item.ResultCipher) != 0 {
				t.Fatalf("content resurrected: %v", err)
			}
			if _, err = store.InspectExisting(t.Context(), engine.Store.Path, engine.Store.Vault); err != nil {
				t.Fatal(err)
			}
		})
	}
}
