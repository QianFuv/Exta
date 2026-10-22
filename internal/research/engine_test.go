package research

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptrace"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"exta/internal/domain"
	"exta/internal/providers"
	"exta/internal/security"
	"exta/internal/store"
)

const exaIntent = `{"input":"Private research question","budget":{"unit":"micro_usd","amount":1000000,"mode":"hard"}}`

type localTransport func(*http.Request) (*http.Response, error)

func (transport localTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func fixture(t *testing.T) (*Engine, string, *time.Time) {
	t.Helper()
	vault, err := security.NewVault(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	state, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "state.sqlite"), vault)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { state.Close() })
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	state.Now = func() time.Time { return now }
	owner, err := state.BootstrapAdmin(t.Context(), "synthetic-research-operator-secret")
	if err != nil {
		t.Fatal(err)
	}
	account, err := state.CreateAccount(t.Context(), owner, domain.AccountCreate{Provider: "exa", Label: "Synthetic", Enabled: true, QuotaPolicy: domain.QuotaPolicy{Unit: "micro_usd", Allocation: 100000000, PerOperationReservation: 1000000, ActiveJobs: 2}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = state.AddKey(t.Context(), owner, domain.KeyCreate{AccountId: account.Id, Label: "Synthetic", Enabled: true, Secret: "synthetic-original-research-secret"})
	if err != nil {
		t.Fatal(err)
	}
	client, err := providers.New("exa", localTransport(func(*http.Request) (*http.Response, error) {
		t.Error("unexpected network dispatch")
		return nil, errors.New("unexpected")
	}))
	if err != nil {
		t.Fatal(err)
	}
	return &Engine{Store: state, Clients: map[domain.Provider]*providers.Client{"exa": client}, PollDelay: func(int) time.Duration { return time.Second }}, owner, &now
}

func acceptJob(t *testing.T, engine *Engine, owner string) domain.Job {
	t.Helper()
	job, err := engine.Accept(t.Context(), owner, "synthetic-idempotency-key", []byte(exaIntent))
	if err != nil {
		t.Fatal(err)
	}
	return job
}

func claimJob(t *testing.T, engine *Engine) record {
	t.Helper()
	item, found, err := engine.claim(t.Context())
	if err != nil || !found {
		t.Fatalf("claim found=%v: %v", found, err)
	}
	return item
}

func localJob(t *testing.T, engine *Engine, id string) domain.Job {
	t.Helper()
	item, err := engine.Get(t.Context(), "", id, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	return item.Job
}

func expectFault(t *testing.T, err error, code string) {
	t.Helper()
	var fault *store.Fault
	if !errors.As(err, &fault) || fault.Code != code {
		t.Fatalf("want %s, got %v", code, err)
	}
}

func TestConcurrentBindingAndFrozenReplay(t *testing.T) {
	engine, owner, _ := fixture(t)
	var group sync.WaitGroup
	ids := make(chan string, 50)
	for index := 0; index < 50; index++ {
		group.Go(func() {
			job, err := engine.Accept(t.Context(), owner, "synthetic-idempotency-key", []byte(exaIntent))
			if err != nil {
				t.Error(err)
			} else {
				ids <- job.Id
			}
		})
	}
	group.Wait()
	close(ids)
	first := ""
	for id := range ids {
		if first == "" {
			first = id
		}
		if id != first {
			t.Fatal("duplicate accepted job")
		}
	}
	for _, table := range []string{"research_jobs", "idempotency_records", "budget_reservations"} {
		var count int
		if err := engine.Store.DB.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil || count != 1 {
			t.Fatalf("%s %d %v", table, count, err)
		}
	}
	if _, err := engine.Store.DB.Exec(`UPDATE settings SET data=json_set(data,'$.limits.observation_seconds',60); UPDATE route_policies SET data=json_set(data,'$.enabled',json('false'))`); err != nil {
		t.Fatal(err)
	}
	replay, err := engine.Accept(t.Context(), owner, "synthetic-idempotency-key", []byte(exaIntent))
	if err != nil || replay.Id != first {
		t.Fatalf("frozen replay: %v", err)
	}
	var intent map[string]any
	_ = json.Unmarshal([]byte(exaIntent), &intent)
	intent["input"] = "Changed intent"
	_, err = engine.Accept(t.Context(), owner, "synthetic-idempotency-key", []byte(store.JSON(intent)))
	expectFault(t, err, "idempotency_conflict")
	if _, err = engine.Store.DB.Exec(`UPDATE idempotency_records SET replay_context='{}'`); err != nil {
		t.Fatal(err)
	}
	_, err = engine.Accept(t.Context(), owner, "synthetic-idempotency-key", []byte(exaIntent))
	expectFault(t, err, "storage_unavailable")
}

func TestNewAcceptanceHonorsObservationLimit(t *testing.T) {
	engine, owner, _ := fixture(t)
	_, err := engine.Store.DB.Exec(`UPDATE settings SET data=json_set(data,'$.limits.observation_seconds',60)`)
	if err != nil {
		t.Fatal(err)
	}
	var input map[string]any
	_ = json.Unmarshal([]byte(exaIntent), &input)
	input["observation_seconds"] = 3600
	_, err = engine.Accept(t.Context(), owner, "synthetic-idempotency-key", []byte(store.JSON(input)))
	expectFault(t, err, "invalid_request")
}

func TestLockedQueuedJobReleasesOnlyProvenUnsentHolds(t *testing.T) {
	engine, owner, _ := fixture(t)
	job := acceptJob(t, engine, owner)
	engine.Store.Vault = nil
	if _, err := engine.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	job = localJob(t, engine, job.Id)
	if job.State != "failed" || job.ActiveSlotHeld || job.BudgetHoldState != "released_unspent" || job.Uncertainty || job.UpstreamMayContinue {
		t.Fatalf("stranded unsent: %+v", job)
	}
}

func TestReconciliationCannotFenceLiveCreateEvidence(t *testing.T) {
	engine, owner, _ := fixture(t)
	job := acceptJob(t, engine, owner)
	claimed := claimJob(t, engine)
	job, err := engine.Cancel(t.Context(), owner, owner, job.Id, domain.CancelRequest{Mode: "observation", ExpectedVersion: claimed.Job.Version})
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(store.JSON(map[string]any{"action": "budget_write_off", "expected_version": job.Version, "reason": "Synthetic uncertainty review", "evidence_ref": "synthetic-log", "acknowledge_upstream_may_bill": true}))
	_, err = engine.Reconcile(t.Context(), owner, job.Id, raw)
	expectFault(t, err, "job_state_conflict")
	if err = engine.complete(t.Context(), claimed, providers.ResearchOutcome{ID: "upstream_1", State: "running"}); err != nil {
		t.Fatal(err)
	}
	job = localJob(t, engine, job.Id)
	if job.UpstreamJobId == nil || *job.UpstreamJobId != "upstream_1" || job.State != "observation_expired" {
		t.Fatalf("lost evidence: %+v", job)
	}
}

func TestUpstreamCancellationSurvivesObservationLimit(t *testing.T) {
	engine, owner, now := fixture(t)
	job := acceptJob(t, engine, owner)
	claimed := claimJob(t, engine)
	if err := engine.complete(t.Context(), claimed, providers.ResearchOutcome{ID: "upstream_1", State: "running"}); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(25 * time.Hour)
	if _, _, err := engine.claim(t.Context()); err != nil {
		t.Fatal(err)
	}
	job = localJob(t, engine, job.Id)
	job, err := engine.Cancel(t.Context(), owner, owner, job.Id, domain.CancelRequest{Mode: "upstream", ExpectedVersion: job.Version})
	if err != nil {
		t.Fatal(err)
	}
	claimed = claimJob(t, engine)
	if claimed.Action != "cancel" {
		t.Fatalf("cancel action lost: %+v", claimed)
	}
	if err = engine.complete(t.Context(), claimed, providers.ResearchOutcome{ID: "upstream_1", State: "completed", StopReason: "budget_reached", Outcome: providers.Outcome{Cost: pointer(int64(200000))}}); err != nil {
		t.Fatal(err)
	}
	job = localJob(t, engine, job.Id)
	if job.State != "succeeded" || !job.IsPartial || job.ActiveSlotHeld || job.BudgetHoldState != "settled" {
		t.Fatalf("completion race: %+v", job)
	}
}

func TestFrozenRouteBoundsSafeRetryAndCallDeadline(t *testing.T) {
	engine, owner, _ := fixture(t)
	_, err := engine.Store.DB.Exec(`UPDATE route_policies SET data=json_set(data,'$.max_attempts',1,'$.deadline_seconds',5) WHERE capability='research_create'`)
	if err != nil {
		t.Fatal(err)
	}
	job := acceptJob(t, engine, owner)
	engine.Clients["exa"], err = providers.New("exa", localTransport(func(request *http.Request) (*http.Response, error) {
		deadline, ok := request.Context().Deadline()
		if !ok || time.Until(deadline) > 5*time.Second {
			t.Error("frozen request deadline ignored")
		}
		return nil, errors.New("synthetic dial failure before transmission")
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = engine.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	job = localJob(t, engine, job.Id)
	if job.State != "failed" || job.BudgetHoldState != "released_unspent" {
		t.Fatalf("frozen max attempts ignored: %+v", job)
	}
}

func TestResearchOutcomePersistsSharedRateGate(t *testing.T) {
	engine, owner, _ := fixture(t)
	acceptJob(t, engine, owner)
	claimed := claimJob(t, engine)
	outcome := providers.ResearchOutcome{Outcome: providers.Outcome{Status: 429, GateKind: "rate", GateScope: "account", SharedCooldown: true, Cooldown: 300 * time.Second, IsUnspent: true, CanRetry: true}}
	if err := engine.complete(t.Context(), claimed, outcome); err != nil {
		t.Fatal(err)
	}
	_, err := engine.Store.Reserve(t.Context(), store.Admission{ParentId: store.ID("request"), PrincipalId: owner, Provider: "exa", Operation: "search"})
	expectFault(t, err, "upstream_rate_limited")
}

func TestRestartNeverBlindlyResubmitsAndFencesOldWorker(t *testing.T) {
	engine, owner, _ := fixture(t)
	job := acceptJob(t, engine, owner)
	claimed := claimJob(t, engine)
	if err := engine.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := engine.complete(t.Context(), claimed, providers.ResearchOutcome{ID: "late_identity", State: "completed", Outcome: providers.Outcome{Cost: pointer(int64(0))}}); err != nil {
		t.Fatal(err)
	}
	job = localJob(t, engine, job.Id)
	if job.State != "submission_unknown" || !job.ActiveSlotHeld || job.BudgetHoldState != "held" || job.UpstreamJobId != nil {
		t.Fatalf("recovery lost uncertainty: %+v", job)
	}
	claimed = claimJob(t, engine)
	if claimed.Action != "reconcile" {
		t.Fatal("restart scheduled a duplicate create")
	}
}

func TestTerminalUnknownCostRetainsMoneyAndWriteOffDebitsOnce(t *testing.T) {
	engine, owner, _ := fixture(t)
	job := acceptJob(t, engine, owner)
	claimed := claimJob(t, engine)
	if err := engine.complete(t.Context(), claimed, providers.ResearchOutcome{ID: "upstream_1", State: "completed"}); err != nil {
		t.Fatal(err)
	}
	job = localJob(t, engine, job.Id)
	if job.ActiveSlotHeld || job.BudgetHoldState != "held" || !job.Uncertainty {
		t.Fatalf("unknown cost released: %+v", job)
	}
	raw := []byte(store.JSON(map[string]any{"action": "budget_write_off", "expected_version": job.Version, "reason": "Synthetic unknown cost review", "evidence_ref": "synthetic-log", "acknowledge_upstream_may_bill": true}))
	job, err := engine.Reconcile(t.Context(), owner, job.Id, raw)
	if err != nil || job.BudgetHoldState != "written_off" {
		t.Fatalf("writeoff %v %+v", err, job)
	}
	var total int64
	if err = engine.Store.DB.QueryRow("SELECT sum(amount) FROM usage_ledger").Scan(&total); err != nil || total != 1000000 {
		t.Fatalf("debit %d %v", total, err)
	}
	_, err = engine.Reconcile(t.Context(), owner, job.Id, raw)
	expectFault(t, err, "revision_conflict")
}

func syntheticResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(bytes.NewBufferString(body))}
}

func TestQueuedAdmissionRechecksBillingAndCooldown(t *testing.T) {
	for _, kind := range []string{"quota", "invalid", "rate"} {
		t.Run(kind, func(t *testing.T) {
			engine, owner, now := fixture(t)
			job := acceptJob(t, engine, owner)
			item, err := engine.read(t.Context(), job.Id, owner)
			if err != nil {
				t.Fatal(err)
			}
			var account, key string
			if err = engine.Store.DB.QueryRow("SELECT account_id,key_id FROM budget_reservations WHERE id=?", item.ReservationID).Scan(&account, &key); err != nil {
				t.Fatal(err)
			}
			scope, entity := "account", account
			if kind == "invalid" {
				scope, entity = "key", key
			}
			if err = engine.Store.GateFailure(t.Context(), scope, entity, item.Credential, kind, 5*time.Minute); err != nil {
				t.Fatal(err)
			}
			_, found, err := engine.claim(t.Context())
			if err != nil || found {
				t.Fatalf("blocked create claimed %v %v", found, err)
			}
			job = localJob(t, engine, job.Id)
			if kind == "rate" {
				if job.State != "queued" || !job.ActiveSlotHeld {
					t.Fatal("cooldown destroyed queued intent")
				}
				*now = now.Add(5 * time.Minute)
				if next := claimJob(t, engine); next.Action != "create" {
					t.Fatal("cooldown did not reopen")
				}
			} else if job.State != "failed" || job.BudgetHoldState != "released_unspent" {
				t.Fatalf("revoked admission held unsent cost: %+v", job)
			}
		})
	}
}

func TestPinnedCredentialSurvivesRotationAndRealRestart(t *testing.T) {
	engine, owner, now := fixture(t)
	job := acceptJob(t, engine, owner)
	calls := 0
	client, err := providers.New("exa", localTransport(func(request *http.Request) (*http.Response, error) {
		calls++
		if request.Header.Get("x-api-key") != "synthetic-original-research-secret" {
			t.Error("poll changed pinned credential")
		}
		if calls == 1 {
			return syntheticResponse(200, `{"id":"upstream_1","status":"running"}`), nil
		}
		if request.Method != "GET" || request.URL.Path != "/agent/runs/upstream_1" {
			t.Error("restart created duplicate work")
		}
		return syntheticResponse(200, `{"id":"upstream_1","status":"completed","output":{"text":"Private report"},"costDollars":{"total":0.1}}`), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	engine.Clients["exa"] = client
	if _, err = engine.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	keys, err := engine.Store.Keys(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	secret := "synthetic-rotated-research-secret"
	if _, err = engine.Store.ChangeKey(t.Context(), owner, keys[0].Id, keys[0].Version, nil, nil, &secret, false); err != nil {
		t.Fatal(err)
	}
	path, vault := engine.Store.Path, engine.Store.Vault
	engine.Store.Close()
	engine.Store, err = store.OpenExisting(t.Context(), path, vault)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Store.Close()
	engine.Store.Now = func() time.Time { return *now }
	*now = now.Add(2 * time.Second)
	if err = engine.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err = engine.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	job = localJob(t, engine, job.Id)
	if job.State != "succeeded" || job.Result == nil || calls != 2 {
		t.Fatalf("restart result: %+v calls %d", job, calls)
	}
	var data string
	var cipher []byte
	if err = engine.Store.DB.QueryRow("SELECT data,result_cipher FROM research_jobs WHERE id=?", job.Id).Scan(&data, &cipher); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains([]byte(data), []byte("Private")) || bytes.Contains(cipher, []byte("Private")) {
		t.Fatal("private research content stored in plaintext")
	}
}

func TestPossibleTransmissionNeverCreatesAgain(t *testing.T) {
	engine, owner, _ := fixture(t)
	job := acceptJob(t, engine, owner)
	client, err := providers.New("exa", localTransport(func(request *http.Request) (*http.Response, error) {
		if trace := httptrace.ContextClientTrace(request.Context()); trace != nil && trace.GotConn != nil {
			trace.GotConn(httptrace.GotConnInfo{})
		}
		return nil, errors.New("synthetic connection lost after dispatch")
	}))
	if err != nil {
		t.Fatal(err)
	}
	engine.Clients["exa"] = client
	if _, err = engine.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	job = localJob(t, engine, job.Id)
	if job.State != "submission_unknown" || !job.ActiveSlotHeld || job.BudgetHoldState != "held" {
		t.Fatalf("unknown dispatch retried or released: %+v", job)
	}
}

func TestUnacknowledgedCancelIsNotAutomaticallyResent(t *testing.T) {
	engine, owner, now := fixture(t)
	job := acceptJob(t, engine, owner)
	item := claimJob(t, engine)
	if err := engine.complete(t.Context(), item, providers.ResearchOutcome{ID: "upstream_1", State: "running"}); err != nil {
		t.Fatal(err)
	}
	job = localJob(t, engine, job.Id)
	job, err := engine.Cancel(t.Context(), owner, owner, job.Id, domain.CancelRequest{Mode: "upstream", ExpectedVersion: job.Version})
	if err != nil {
		t.Fatal(err)
	}
	item = claimJob(t, engine)
	if item.Action != "cancel" {
		t.Fatal("missing cancel")
	}
	if err = engine.complete(t.Context(), item, providers.ResearchOutcome{Outcome: providers.Outcome{Failure: store.Reject("upstream_failure", 502, "Synthetic uncertain cancel")}}); err != nil {
		t.Fatal(err)
	}
	job = localJob(t, engine, job.Id)
	if !job.ActiveSlotHeld || job.BudgetHoldState != "held" {
		t.Fatal("cancel uncertainty released holds")
	}
	_, err = engine.Cancel(t.Context(), owner, owner, job.Id, domain.CancelRequest{Mode: "upstream", ExpectedVersion: job.Version})
	if err != nil {
		t.Fatal(err)
	}
	*now = now.Add(2 * time.Second)
	if item = claimJob(t, engine); item.Action != "poll" {
		t.Fatal("ambiguous cancel was resent")
	}
}

func TestManualSettlementPreservesPartialAndProtocolEvidence(t *testing.T) {
	for _, output := range []string{`{"text":"Partial answer"}`, `{"structured":[1]}`} {
		t.Run(output, func(t *testing.T) {
			engine, owner, _ := fixture(t)
			job := acceptJob(t, engine, owner)
			claimed := claimJob(t, engine)
			if err := engine.complete(t.Context(), claimed, providers.ResearchOutcome{ID: "upstream_1", State: "running"}); err != nil {
				t.Fatal(err)
			}
			job = localJob(t, engine, job.Id)
			job, err := engine.Cancel(t.Context(), owner, owner, job.Id, domain.CancelRequest{Mode: "observation", ExpectedVersion: job.Version})
			if err != nil {
				t.Fatal(err)
			}
			engine.Clients["exa"], err = providers.New("exa", localTransport(func(*http.Request) (*http.Response, error) {
				return syntheticResponse(200, `{"id":"upstream_1","status":"completed","stopReason":"budget_reached","output":`+output+`}`), nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			raw := []byte(store.JSON(map[string]any{"action": "settle_terminal", "expected_version": job.Version, "reason": "Synthetic terminal review", "evidence_ref": "synthetic-log", "upstream_state": "completed", "cost": nil}))
			job, err = engine.Reconcile(t.Context(), owner, job.Id, raw)
			if err != nil {
				t.Fatal(err)
			}
			if !job.IsPartial || job.CompletionReason == nil || *job.CompletionReason != "budget_reached" {
				t.Fatal("partial completion evidence lost")
			}
			if output == `{"structured":[1]}` && job.Error == nil {
				t.Fatal("malformed report evidence hidden")
			}
		})
	}
}

func TestTavilyUndocumentedSchemaTypesAreRejected(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"flag":{"type":"boolean","description":"Synthetic flag"}}}`)
	if err := validateProviderSchema(domain.ResearchCreate{Profile: pointer("tavily_mini"), OutputSchema: &schema}); err == nil {
		t.Fatal("accepted undocumented Tavily boolean")
	}
}

func TestOfficialExaIdentifierAttachesWithoutTruncation(t *testing.T) {
	for _, upstream := range []string{".task:with-punctuation_1", strings.Repeat("a", 200)} {
		engine, owner, now := fixture(t)
		job := acceptJob(t, engine, owner)
		claimed := claimJob(t, engine)
		if err := engine.complete(t.Context(), claimed, providers.ResearchOutcome{}); err != nil {
			t.Fatal(err)
		}
		job = localJob(t, engine, job.Id)
		input, err := engine.input(claimed)
		if err != nil {
			t.Fatal(err)
		}
		payload, err := providers.ResearchPayload("exa", input, job.Id, claimed.IntentDigest)
		if err != nil {
			t.Fatal(err)
		}
		body := store.JSON(map[string]any{"id": upstream, "status": "running", "createdAt": store.Stamp(*now), "request": payload})
		engine.Clients["exa"], err = providers.New("exa", localTransport(func(*http.Request) (*http.Response, error) { return syntheticResponse(200, body), nil }))
		if err != nil {
			t.Fatal(err)
		}
		raw := []byte(store.JSON(map[string]any{"action": "attach_upstream_id", "expected_version": job.Version, "reason": "Synthetic verified correlation", "evidence_ref": "synthetic-provider-capture", "upstream_job_id": upstream}))
		job, err = engine.Reconcile(t.Context(), owner, job.Id, raw)
		if err != nil || job.UpstreamJobId == nil || *job.UpstreamJobId != upstream || !job.ActiveSlotHeld || job.BudgetHoldState != "held" {
			t.Fatalf("attach: %v %+v", err, job)
		}
	}
}

func TestMalformedStatusCannotPoisonWorker(t *testing.T) {
	engine, owner, _ := fixture(t)
	job := acceptJob(t, engine, owner)
	engine.Clients["exa"], _ = providers.New("exa", localTransport(func(*http.Request) (*http.Response, error) {
		return syntheticResponse(200, store.JSON(map[string]any{"id": "upstream_1", "status": strings.Repeat("x", 129)})), nil
	}))
	if _, err := engine.Tick(t.Context()); err != nil {
		t.Fatalf("worker killed by provider metadata: %v", err)
	}
	job = localJob(t, engine, job.Id)
	if job.State != "observation_expired" || job.Error == nil || job.UpstreamJobId == nil || !job.ActiveSlotHeld || job.BudgetHoldState != "held" {
		t.Fatalf("malformed identity/evidence lost: %+v", job)
	}
}

func TestManualReconciliationCannotBypassSharedCooldown(t *testing.T) {
	engine, owner, _ := fixture(t)
	job := acceptJob(t, engine, owner)
	claimed := claimJob(t, engine)
	if err := engine.complete(t.Context(), claimed, providers.ResearchOutcome{}); err != nil {
		t.Fatal(err)
	}
	job = localJob(t, engine, job.Id)
	calls := 0
	engine.Clients["exa"], _ = providers.New("exa", localTransport(func(*http.Request) (*http.Response, error) {
		calls++
		response := syntheticResponse(429, `{"error":{"code":"RATE_LIMIT_EXCEEDED"}}`)
		response.Header.Set("Retry-After", "300")
		return response, nil
	}))
	raw := []byte(store.JSON(map[string]any{"action": "attach_upstream_id", "expected_version": job.Version, "reason": "Synthetic verified correlation", "evidence_ref": "synthetic-provider-capture", "upstream_job_id": "upstream_1"}))
	_, err := engine.Reconcile(t.Context(), owner, job.Id, raw)
	expectFault(t, err, "job_state_conflict")
	_, err = engine.Reconcile(t.Context(), owner, job.Id, raw)
	expectFault(t, err, "upstream_rate_limited")
	if calls != 1 {
		t.Fatalf("manual verification bypassed cooldown: %d", calls)
	}
}

func TestSuccessfulResearchClosesItsCircuitProbe(t *testing.T) {
	engine, owner, now := fixture(t)
	accounts, err := engine.Store.Accounts(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 5; index++ {
		if err = engine.Store.GateFailure(t.Context(), "account", accounts[0].Id, "", "transient", 0); err != nil {
			t.Fatal(err)
		}
	}
	*now = now.Add(31 * time.Second)
	acceptJob(t, engine, owner)
	engine.Clients["exa"], _ = providers.New("exa", localTransport(func(*http.Request) (*http.Response, error) {
		return syntheticResponse(200, `{"id":"upstream_1","status":"running"}`), nil
	}))
	if _, err = engine.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err = engine.Store.Reserve(t.Context(), store.Admission{ParentId: store.ID("request"), PrincipalId: owner, Provider: "exa", Operation: "search"}); err != nil {
		t.Fatalf("successful probe remained blocked: %v", err)
	}
}

func TestQueuedResearchConsumesRatesAtDispatch(t *testing.T) {
	engine, owner, now := fixture(t)
	job := acceptJob(t, engine, owner)
	other := store.ID("principal")
	if _, err := engine.Store.DB.Exec("INSERT INTO principals VALUES(?,NULL,'[]',?)", other, store.Stamp(*now)); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 10; index++ {
		if _, err := engine.Store.Reserve(t.Context(), store.Admission{ParentId: store.ID("request"), PrincipalId: other, Provider: "exa", Operation: "search"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, found, err := engine.claim(t.Context()); err != nil || found {
		t.Fatalf("research bypassed shared dispatch rate %v %v", found, err)
	}
	job = localJob(t, engine, job.Id)
	if job.State != "queued" || !job.ActiveSlotHeld {
		t.Fatal("rate wait changed financial admission")
	}
	*now = now.Add(time.Second)
	if claimJob(t, engine).Action != "create" {
		t.Fatal("rate never reopened")
	}
}

func TestResearchAcceptanceHasBoundedDatabaseWait(t *testing.T) {
	if testing.Short() {
		t.Skip("exercises the 30-second acceptance contract")
	}
	engine, owner, _ := fixture(t)
	connection, err := engine.Store.DB.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	started := time.Now()
	_, err = engine.Accept(t.Context(), owner, "synthetic-idempotency-key", []byte(exaIntent))
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > 32*time.Second {
		t.Fatalf("acceptance deadline %s %v", time.Since(started), err)
	}
}

func TestExistingJobAcquiresExclusiveCurrentCircuitProbe(t *testing.T) {
	engine, owner, now := fixture(t)
	job := acceptJob(t, engine, owner)
	original := claimJob(t, engine)
	if err := engine.complete(t.Context(), original, providers.ResearchOutcome{ID: "upstream_1", State: "running", Outcome: providers.Outcome{Status: 200}}); err != nil {
		t.Fatal(err)
	}
	account := original.Snapshot.AccountId
	for index := 0; index < 5; index++ {
		if err := engine.Store.GateFailure(t.Context(), "account", account, "", "transient", 0); err != nil {
			t.Fatal(err)
		}
	}
	*now = now.Add(31 * time.Second)
	claimed := claimJob(t, engine)
	if claimed.Action != "poll" {
		t.Fatal("existing job did not resume observation")
	}
	_, err := engine.Store.ResearchDispatch(t.Context(), claimed.ReservationID, false, store.ID("other_probe"))
	expectFault(t, err, "upstream_rate_limited")
	if err = engine.complete(t.Context(), claimed, providers.ResearchOutcome{ID: "upstream_1", State: "running", Outcome: providers.Outcome{Status: 200}}); err != nil {
		t.Fatal(err)
	}
	if _, err = engine.Store.ResearchDispatch(t.Context(), claimed.ReservationID, false, store.ID("next_probe")); err != nil {
		t.Fatalf("old generation blocked successful probe: %v", err)
	}
	if localJob(t, engine, job.Id).State != "running" {
		t.Fatal("probe altered upstream identity")
	}
}

func TestRevokedQueueReleasesBeforeAnyWireDebit(t *testing.T) {
	engine, owner, _ := fixture(t)
	job := acceptJob(t, engine, owner)
	keys, err := engine.Store.Keys(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	isEnabled := false
	if _, err = engine.Store.ChangeKey(t.Context(), owner, keys[0].Id, keys[0].Version, nil, &isEnabled, nil, false); err != nil {
		t.Fatal(err)
	}
	var before, after int
	if err = engine.Store.DB.QueryRow("SELECT coalesce(sum(used),0) FROM rate_windows WHERE scope IN ('account','key')").Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, found, err := engine.claim(t.Context()); err != nil || found {
		t.Fatalf("revoked claim %v %v", found, err)
	}
	if err = engine.Store.DB.QueryRow("SELECT coalesce(sum(used),0) FROM rate_windows WHERE scope IN ('account','key')").Scan(&after); err != nil {
		t.Fatal(err)
	}
	job = localJob(t, engine, job.Id)
	if before != after || job.BudgetHoldState != "released_unspent" || job.ActiveSlotHeld {
		t.Fatalf("revoked queue consumed resources %d/%d %+v", before, after, job)
	}
}

func TestShutdownReportsFailedDurableFinalization(t *testing.T) {
	engine, owner, _ := fixture(t)
	acceptJob(t, engine, owner)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	engine.Clients["exa"], _ = providers.New("exa", localTransport(func(*http.Request) (*http.Response, error) {
		cancel()
		engine.Store.Close()
		return syntheticResponse(200, `{"id":"upstream_1","status":"running"}`), nil
	}))
	err := engine.Run(ctx)
	if !errors.Is(err, errFinalization) {
		t.Fatalf("shutdown concealed lost durable finalization: %v", err)
	}
}

func TestCancelledClaimRollbackIsAnOrdinaryShutdown(t *testing.T) {
	engine, _, _ := fixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	err := engine.Store.Write(ctx, func(*sql.Tx) error {
		cancel()
		wait, done := context.WithTimeout(t.Context(), 2*time.Second)
		defer done()
		connection, err := engine.Store.DB.Conn(wait)
		if err != nil {
			return err
		}
		return connection.Close()
	})
	if !errors.Is(err, sql.ErrTxDone) {
		t.Fatalf("automatic rollback scenario did not occur: %v", err)
	}
	if !errors.Is(normalizeClaimError(ctx, err), context.Canceled) {
		t.Fatal("normal claim cancellation reported as worker failure")
	}
	if normalizeClaimError(ctx, errFinalization) != errFinalization {
		t.Fatal("finalization failure was suppressed")
	}
}

func TestTavilyKnownIdentityPollsAndUnknownIdentityFailsClosed(t *testing.T) {
	for _, known := range []bool{true, false} {
		engine, owner, now := fixture(t)
		account, err := engine.Store.CreateAccount(t.Context(), owner, domain.AccountCreate{Provider: "tavily", Label: "Synthetic Tavily", Enabled: true, QuotaPolicy: domain.QuotaPolicy{Unit: "tavily_credit", Allocation: 1000, PerOperationReservation: 110, ActiveJobs: 2}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = engine.Store.AddKey(t.Context(), owner, domain.KeyCreate{AccountId: account.Id, Enabled: true, Label: "Synthetic", Secret: "synthetic-original-tavily-secret"}); err != nil {
			t.Fatal(err)
		}
		calls := 0
		engine.Clients["tavily"], _ = providers.New("tavily", localTransport(func(request *http.Request) (*http.Response, error) {
			calls++
			if request.Header.Get("Authorization") != "Bearer synthetic-original-tavily-secret" {
				t.Fatal("wrong pinned Tavily credential")
			}
			if calls == 1 && !known {
				if trace := httptrace.ContextClientTrace(request.Context()); trace != nil && trace.GotConn != nil {
					trace.GotConn(httptrace.GotConnInfo{})
				}
				return nil, errors.New("synthetic uncertain transmission")
			}
			if calls == 1 {
				return syntheticResponse(201, `{"request_id":"tavily_task","status":"pending"}`), nil
			}
			if request.Method != "GET" || request.URL.Query().Get("include_usage") != "true" {
				t.Fatal("known ID was resubmitted")
			}
			return syntheticResponse(200, `{"request_id":"tavily_task","status":"failed","usage":{"credits":3}}`), nil
		}))
		intent := []byte(`{"input":"Synthetic Tavily research","profile":"tavily_mini","budget":{"mode":"estimated","unit":"tavily_credit","amount":110,"acknowledge_estimated_budget":true}}`)
		job, err := engine.Accept(t.Context(), owner, "synthetic-idempotency-key", intent)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = engine.Tick(t.Context()); err != nil {
			t.Fatal(err)
		}
		job = localJob(t, engine, job.Id)
		if known {
			*now = now.Add(2 * time.Second)
			if _, err = engine.Tick(t.Context()); err != nil {
				t.Fatal(err)
			}
			job = localJob(t, engine, job.Id)
			if job.State != "failed" || job.ActiveSlotHeld || job.BudgetHoldState != "settled" || calls != 2 {
				t.Fatalf("known Tavily evidence lost: %+v calls=%d", job, calls)
			}
		} else {
			raw := []byte(store.JSON(map[string]any{"action": "attach_upstream_id", "expected_version": job.Version, "reason": "Synthetic recovered ID", "evidence_ref": "administrator-assertion-only", "upstream_job_id": "tavily_task"}))
			_, err = engine.Reconcile(t.Context(), owner, job.Id, raw)
			expectFault(t, err, "job_state_conflict")
			*now = now.Add(2 * time.Second)
			if _, err = engine.Tick(t.Context()); err != nil {
				t.Fatal(err)
			}
			*now = now.Add(2 * time.Hour)
			if _, err = engine.Tick(t.Context()); err != nil {
				t.Fatal(err)
			}
			job = localJob(t, engine, job.Id)
			if calls != 1 || job.State != "observation_expired" || !job.ActiveSlotHeld || job.BudgetHoldState != "held" {
				t.Fatalf("unknown Tavily evidence fabricated: %+v calls=%d", job, calls)
			}
		}
	}
}
