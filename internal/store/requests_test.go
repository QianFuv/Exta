package store_test

import (
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"exta/internal/domain"
	"exta/internal/store"
)

func TestRequestHistoryOwnershipPaginationAndNoRawContent(t *testing.T) {
	state, principal := openStore(t)
	addAccount(t, state, principal, 100)
	created := state.Now()
	for index := range 3 {
		item := domain.RequestRecord{Id: store.ID("request"), Operation: "search", Status: "running", CreatedAt: store.Stamp(created.Add(time.Duration(index) * time.Second)), RouteRevision: 1, Usage: []domain.UsageAmount{}}
		if err := state.StartRequest(t.Context(), principal, item); err != nil {
			t.Fatal(err)
		}
	}
	first, err := state.Requests(t.Context(), store.RequestFilter{PrincipalId: principal, Limit: 2})
	if err != nil || len(first.Items) != 2 || first.NextCursor == nil {
		t.Fatalf("first page: %+v %v", first, err)
	}
	last, err := state.Requests(t.Context(), store.RequestFilter{PrincipalId: principal, Limit: 2, Cursor: *first.NextCursor})
	if err != nil || len(last.Items) != 1 || last.NextCursor != nil || first.Items[1].Id == last.Items[0].Id {
		t.Fatalf("last page: %+v %v", last, err)
	}
	if _, err = state.Request(t.Context(), "different-principal", first.Items[0].Id); err == nil {
		t.Fatal("cross-owner request was visible")
	} else {
		faultCode(t, err, "not_found")
	}
	if err = domain.ValidateJSON("RequestRecordList", []byte(store.JSON(first))); err != nil {
		t.Fatal(err)
	}
	audit, err := state.Audit(t.Context(), "", 1)
	if err != nil || len(audit.Items) != 1 {
		t.Fatalf("audit: %+v %v", audit, err)
	}
	if err = domain.ValidateJSON("AuditEventList", []byte(store.JSON(audit))); err != nil {
		t.Fatal(err)
	}
	var changes map[string]any
	if err = json.Unmarshal(audit.Items[0].Changes, &changes); err != nil || changes["changes"] != nil {
		t.Fatal("audit changes were not decoded as their original metadata")
	}
}

func TestInterruptedRequestRetainsReservationAcrossRestart(t *testing.T) {
	state, principal := openStore(t)
	account, _ := addAccount(t, state, principal, 100)
	item := domain.RequestRecord{Id: store.ID("request"), Operation: "search", Status: "running", CreatedAt: store.Stamp(state.Now()), RouteRevision: 1, Usage: []domain.UsageAmount{}}
	if err := state.StartRequest(t.Context(), principal, item); err != nil {
		t.Fatal(err)
	}
	hold, err := state.Reserve(t.Context(), store.Admission{ParentId: item.Id, PrincipalId: principal, Provider: "exa", Operation: "search"})
	if err != nil {
		t.Fatal(err)
	}
	attempt := domain.Attempt{Id: store.ID("attempt"), AttemptNumber: 1, AccountId: account.Id, CredentialVersionId: hold.CredentialVersionId, Provider: "exa", TransmissionState: "possibly_sent", BillingUncertain: true, CreatedAt: item.CreatedAt}
	if err = state.StartAttempt(t.Context(), item.Id, attempt); err != nil {
		t.Fatal(err)
	}
	path, vault := state.Path, state.Vault
	state.Close()
	state, err = store.OpenExisting(t.Context(), path, vault)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	if err = state.RecoverRequests(t.Context()); err != nil {
		t.Fatal(err)
	}
	detail, err := state.Request(t.Context(), principal, item.Id)
	if err != nil || detail.Request.Status != "uncertain" || !detail.Request.BillingUncertain || len(detail.Attempts) != 1 {
		t.Fatalf("recovered: %+v %v", detail, err)
	}
	if err = domain.ValidateJSON("RequestDetail", []byte(store.JSON(detail))); err != nil {
		t.Fatal(err)
	}
	if err = state.Write(t.Context(), func(tx *sql.Tx) error {
		used, held, err := store.BalanceTx(t.Context(), tx, account.Id)
		if used != 0 || held != 10 {
			t.Errorf("interruption released budget: used=%d held=%d", used, held)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var activity int
	if err = state.DB.QueryRow("SELECT generation FROM activity_fence").Scan(&activity); err != nil || activity != 2 {
		t.Fatalf("request acceptance and transmission must both fence rollback: %d %v", activity, err)
	}
}

func TestAttemptEvidenceAndAccountingCommitAtomically(t *testing.T) {
	state, principal := openStore(t)
	account, _ := addAccount(t, state, principal, 100)
	for _, known := range []bool{false, true} {
		hold, err := state.Reserve(t.Context(), store.Admission{ParentId: store.ID("request"), PrincipalId: principal, Provider: "exa", Operation: "search"})
		if err != nil {
			t.Fatal(err)
		}
		attempt := domain.Attempt{Id: store.ID("attempt"), AttemptNumber: 1, Provider: "exa", AccountId: account.Id, CredentialVersionId: hold.CredentialVersionId, TransmissionState: "possibly_sent", BillingUncertain: true, CreatedAt: store.Stamp(state.Now())}
		if err = state.StartAttempt(t.Context(), hold.ParentId, attempt); err != nil {
			t.Fatal(err)
		}
		attempt.TransmissionState = "response_received"
		attempt.BillingUncertain = !known
		cost := int64(3)
		var amount *int64
		if known {
			amount = &cost
		}
		for range 2 {
			if err = state.FinishAttempt(t.Context(), attempt, hold.Id, false, amount, "reported"); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := state.Write(t.Context(), func(tx *sql.Tx) error {
		used, held, err := store.BalanceTx(t.Context(), tx, account.Id)
		if used != 3 || held != 10 {
			t.Errorf("unknown discarded or known double charged: used=%d held=%d", used, held)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	metrics, err := state.Overview(t.Context(), 2)
	if err != nil || metrics.WaitingRequests != 2 {
		t.Fatalf("metrics: %+v %v", metrics, err)
	}
	if err = domain.ValidateJSON("OverviewMetrics", []byte(store.JSON(metrics))); err != nil {
		t.Fatal(err)
	}
}
