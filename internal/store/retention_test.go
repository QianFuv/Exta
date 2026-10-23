package store_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"exta/internal/domain"
	"exta/internal/store"
)

func TestRetentionBoundsBatchesAndPreservesSpending(t *testing.T) {
	state, owner := openStore(t)
	account, _ := addAccount(t, state, owner, 100)
	old := time.Now().Add(-100 * 24 * time.Hour)
	state.Now = func() time.Time { return old }
	for index := range 4 {
		id := store.ID("request")
		hold, err := state.Reserve(t.Context(), store.Admission{ParentId: id, PrincipalId: owner, Provider: "exa", Operation: "search"})
		if err != nil {
			t.Fatal(err)
		}
		item := domain.RequestRecord{Id: id, Operation: "search", Status: "succeeded", CreatedAt: store.Stamp(old), CompletedAt: func() *string { value := store.Stamp(old); return &value }(), RouteRevision: 1, Usage: []domain.UsageAmount{}}
		if err = state.StartRequest(t.Context(), owner, item); err != nil {
			t.Fatal(err)
		}
		attempt := domain.Attempt{Id: store.ID("attempt"), AttemptNumber: 1, KeyId: &account.Id, CredentialVersionId: hold.CredentialVersionId, Provider: "exa", TransmissionState: "response_received", CreatedAt: item.CreatedAt}
		if err = state.StartAttempt(t.Context(), id, attempt); err != nil {
			t.Fatal(err)
		}
		if index != 3 {
			cost := int64(5)
			if err = state.Settle(t.Context(), hold.Id, &cost, "reported"); err != nil {
				t.Fatal(err)
			}
		}
	}
	state.Now = time.Now
	counts, err := state.Cleanup(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if counts["attempts"] != 1 || counts["requests"] != 1 {
		t.Fatalf("batch: %v", counts)
	}
	for _, count := range counts {
		if count > 1 {
			t.Fatal("unbounded cleanup")
		}
	}
	for range 4 {
		if _, err = state.Cleanup(t.Context(), 100); err != nil {
			t.Fatal(err)
		}
	}
	var remaining int
	if err = state.DB.QueryRow("SELECT count(*) FROM requests").Scan(&remaining); err != nil || remaining != 1 {
		t.Fatalf("unresolved request lost: %d %v", remaining, err)
	}
	if err = state.Write(t.Context(), func(tx *sql.Tx) error {
		spent, held, err := store.BalanceTx(t.Context(), tx, account.Id)
		if spent != 15 || held != 10 {
			t.Fatalf("cleanup refunded money: %d %d", spent, held)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = state.Cleanup(ctx, 100); err == nil {
		t.Fatal("ignored cancellation")
	}
}

func TestRetentionKeepsLatestUsageAndRemovesExpiredSessions(t *testing.T) {
	state, owner := openStore(t)
	account, _ := addAccount(t, state, owner, 100)
	old := store.Stamp(time.Now().Add(-100 * 24 * time.Hour))
	for index, data := range []string{`{"used":1}`, `{"used":2}`, `{"used":null}`} {
		if _, err := state.DB.Exec("INSERT INTO usage_snapshots VALUES(?,?,NULL,?,?)", store.ID("usage"), account.Id, old, data); err != nil {
			t.Fatal(index, err)
		}
	}
	if _, err := state.DB.Exec("INSERT INTO admin_sessions VALUES(?,?,?,?)", []byte("expired"), owner, "synthetic", old); err != nil {
		t.Fatal(err)
	}
	counts, err := state.Cleanup(t.Context(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if counts["usage_snapshots"] != 1 || counts["sessions"] != 1 {
		t.Fatalf("retention counts: %v", counts)
	}
	var count int
	if err = state.DB.QueryRow("SELECT count(*) FROM usage_snapshots").Scan(&count); err != nil || count != 2 {
		t.Fatalf("latest or known usage removed: %d %v", count, err)
	}
}
