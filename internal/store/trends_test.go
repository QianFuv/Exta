package store_test

import (
	"testing"
	"time"

	"exta/internal/domain"
	"exta/internal/store"
)

func TestCallTrendsCountLogicalRequestsAndZeroFillUtcBuckets(t *testing.T) {
	state, principal := openStore(t)
	now := time.Date(2026, 10, 24, 8, 30, 0, 0, time.FixedZone("Shanghai", 8*3600))
	state.Now = func() time.Time { return now }
	start := now.UTC().Truncate(time.Hour).Add(-23 * time.Hour)
	for index, status := range []domain.RequestStatus{"succeeded", "failed", "running", "uncertain"} {
		item := domain.RequestRecord{Id: store.ID("request"), Operation: "search", Status: status, CreatedAt: store.Stamp(start.Add(time.Duration(index) * time.Hour)), Usage: []domain.UsageAmount{}, RouteRevision: 1, BillingUncertain: true}
		if err := state.StartRequest(t.Context(), principal, item); err != nil {
			t.Fatal(err)
		}
		for attempt := 1; attempt <= 3; attempt++ {
			if err := state.StartAttempt(t.Context(), item.Id, domain.Attempt{Id: store.ID("attempt"), AttemptNumber: domain.Integer(attempt), Provider: "exa", CreatedAt: item.CreatedAt}); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, instant := range []time.Time{start.Add(-time.Nanosecond), now} {
		if err := state.StartRequest(t.Context(), principal, domain.RequestRecord{Id: store.ID("request"), Status: "failed", CreatedAt: store.Stamp(instant)}); err != nil {
			t.Fatal(err)
		}
	}
	result, err := state.CallTrends(t.Context(), 24)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Buckets) != 24 || result.From != store.Stamp(start) || result.Buckets[0].Succeeded != 1 || result.Buckets[1].Failed != 1 || result.Buckets[2].Pending != 1 || result.Buckets[3].Uncertain != 1 {
		t.Fatalf("incorrect buckets: %+v", result)
	}
	var total domain.Integer
	for _, bucket := range result.Buckets {
		total += bucket.Total
		if bucket.Total != bucket.Succeeded+bucket.Failed+bucket.Pending+bucket.Uncertain+bucket.Cancelled {
			t.Fatal("outcomes do not partition total")
		}
	}
	if total != 4 || result.Buckets[23].Total != 0 {
		t.Fatalf("attempts or out-of-range calls counted: %d", total)
	}
	if err := domain.ValidateJSON("CallTrends", []byte(store.JSON(result))); err != nil {
		t.Fatal(err)
	}
	for _, hours := range []int{168, 720} {
		result, err := state.CallTrends(t.Context(), hours)
		if err != nil || len(result.Buckets) != map[int]int{168: 168, 720: 30}[hours] {
			t.Fatalf("range %d: %+v %v", hours, result, err)
		}
	}
	if _, err := state.CallTrends(t.Context(), 25); err == nil {
		t.Fatal("unbounded range accepted")
	}
}
