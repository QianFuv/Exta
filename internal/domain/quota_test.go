package domain

import (
	"encoding/json"
	"testing"
)

func TestOptionalPolicyResetAndPreservation(t *testing.T) {
	for _, fixture := range []struct {
		data  string
		reset bool
	}{
		{`{"expected_version":1}`, false},
		{`{"expected_version":1,"quota_policy":null}`, true},
		{`{"expected_version":1,"quota_policy":{}}`, true},
		{`{"expected_version":1,"quota_policy":{"allocation":null,"active_jobs":null}}`, true},
	} {
		if err := ValidateJSON("UpstreamKeyPatch", []byte(fixture.data)); err != nil {
			t.Fatal(err)
		}
		var patch UpstreamKeyPatch
		if err := json.Unmarshal([]byte(fixture.data), &patch); err != nil {
			t.Fatal(err)
		}
		if (patch.QuotaPolicy != nil) != fixture.reset {
			t.Fatalf("reset semantics: %s", fixture.data)
		}
	}
	if ValidateJSON("UpstreamKeyPatch", []byte(`{"expected_version":1,"quota_policy":{"allocation":-1}}`)) == nil {
		t.Fatal("negative limit accepted")
	}
}
