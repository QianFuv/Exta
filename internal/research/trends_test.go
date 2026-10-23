package research

import (
	"testing"
	"time"
)

func TestCallTrendsResearchReplayAndOutcomeChangesRemainOneCall(t *testing.T) {
	engine, owner, now := fixture(t)
	job := acceptJob(t, engine, owner)
	if replay := acceptJob(t, engine, owner); replay.Id != job.Id {
		t.Fatal("replay changed identity")
	}
	*now = now.Add(time.Second)
	for _, status := range []string{"queued", "submission_unknown", "observation_expired", "cancelled", "succeeded", "failed"} {
		if _, err := engine.Store.DB.Exec("UPDATE research_jobs SET state=?,data=json_set(data,'$.job.state',?) WHERE id=?", status, status, job.Id); err != nil {
			t.Fatal(err)
		}
		result, err := engine.Store.CallTrends(t.Context(), 24)
		if err != nil {
			t.Fatal(err)
		}
		bucket := result.Buckets[len(result.Buckets)-1]
		if bucket.Total != 1 {
			t.Fatalf("%s added a call: %+v", status, bucket)
		}
		if status == "succeeded" && bucket.Succeeded != 1 || status == "cancelled" && bucket.Cancelled != 1 || status == "failed" && bucket.Failed != 1 || status == "queued" && bucket.Pending != 1 || (status == "submission_unknown" || status == "observation_expired") && bucket.Uncertain != 1 {
			t.Fatalf("wrong outcome %s: %+v", status, bucket)
		}
	}
	*now = now.Add(time.Hour)
	result, err := engine.Store.CallTrends(t.Context(), 24)
	if err != nil || result.Buckets[len(result.Buckets)-2].Total != 1 || result.Buckets[len(result.Buckets)-1].Total != 0 {
		t.Fatalf("completion moved creation bucket: %+v %v", result, err)
	}
}
