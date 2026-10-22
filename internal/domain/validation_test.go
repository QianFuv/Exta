package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUpstreamIdentifiersPreserveOfficialExaDomain(t *testing.T) {
	for _, id := range []string{".task:with-punctuation_1", strings.Repeat("a", 200), ".", ".."} {
		input, _ := json.Marshal(map[string]any{"action": "attach_upstream_id", "expected_version": 1, "reason": "Synthetic recovered task", "evidence_ref": "synthetic-provider-capture", "upstream_job_id": id})
		if err := ValidateJSON("AttachUpstreamId", input); err != nil {
			t.Fatalf("official ID %q: %v", id, err)
		}
	}
	for _, id := range []string{"", strings.Repeat("a", 201), "task/other", "task%2Fother", "task?query", "task#fragment"} {
		input, _ := json.Marshal(map[string]any{"action": "attach_upstream_id", "expected_version": 1, "reason": "Synthetic recovered task", "evidence_ref": "synthetic-provider-capture", "upstream_job_id": id})
		if ValidateJSON("AttachUpstreamId", input) == nil {
			t.Fatalf("unsafe ID accepted: %q", id)
		}
	}
}

func TestResearchInputCannotOverrideIdentityOrDowngradeBudget(t *testing.T) {
	cases := []struct {
		name  string
		input string
		valid bool
	}{
		{"default-exa", `{"input":"synthetic report","budget":{"mode":"hard","unit":"micro_usd","amount":1000000}}`, true},
		{"unresolved-profile-preserves-original-context", `{"input":"synthetic report","budget":{"mode":"estimated","unit":"tavily_credit","amount":110,"acknowledge_estimated_budget":true}}`, true},
		{"owner-forgery", `{"input":"synthetic report","owner_principal_id":"forged","budget":{"mode":"hard","unit":"micro_usd","amount":1000000}}`, false},
		{"hard-tavily", `{"input":"synthetic report","profile":"tavily_mini","budget":{"mode":"hard","unit":"micro_usd","amount":1000000}}`, false},
		{"unacknowledged-estimate", `{"input":"synthetic report","profile":"tavily_mini","budget":{"mode":"estimated","unit":"tavily_credit","amount":110}}`, false},
		{"pro-underfunded", `{"input":"synthetic report","profile":"tavily_pro","budget":{"mode":"estimated","unit":"tavily_credit","amount":110,"acknowledge_estimated_budget":true}}`, false},
		{"null-is-not-omission", `{"input":"synthetic report","profile":null,"budget":{"mode":"hard","unit":"micro_usd","amount":1000000}}`, false},
		{"missing-required-intent", `{"budget":{"mode":"hard","unit":"micro_usd","amount":1000000}}`, false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateJSON("ResearchCreate", []byte(test.input))
			if (err == nil) != test.valid {
				t.Fatalf("valid=%t, error=%v", test.valid, err)
			}
		})
	}
}

func TestOptionalNullablePatchPreservesClearVersusNoChange(t *testing.T) {
	for _, input := range []string{`{"expected_version":1}`, `{"expected_version":1,"external_ref":null}`, `{"expected_version":1,"external_ref":"account-reference"}`} {
		if err := ValidateJSON("AccountPatch", []byte(input)); err != nil {
			t.Fatal(err)
		}
		var patch AccountPatch
		if err := json.Unmarshal([]byte(input), &patch); err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(patch)
		if err != nil || string(encoded) != input {
			t.Fatalf("lost patch intent: input=%s output=%s error=%v", input, encoded, err)
		}
	}
}

func TestGeneratedResearchBudgetRemainsJsonObject(t *testing.T) {
	input := []byte(`{"input":"synthetic report","budget":{"mode":"hard","unit":"micro_usd","amount":1000000}}`)
	var request ResearchCreate
	if err := json.Unmarshal(input, &request); err != nil {
		t.Fatalf("generated boundary cannot decode budget: %v", err)
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if err = ValidateJSON("ResearchCreate", encoded); err != nil {
		t.Fatalf("generated boundary changed budget representation: %s: %v", encoded, err)
	}
}

func TestGeneratedIntegersAcceptExactJsonNumberForms(t *testing.T) {
	for _, pair := range [][2]string{{"60.0", "1000000.0"}, {"6e1", "1e6"}, {"600e-1", "100000000e-2"}} {
		t.Run(pair[0], func(t *testing.T) {
			input := []byte(`{"input":"synthetic report","observation_seconds":` + pair[0] + `,"budget":{"mode":"hard","unit":"micro_usd","amount":` + pair[1] + `}}`)
			if err := ValidateJSON("ResearchCreate", input); err != nil {
				t.Fatalf("integral JSON number rejected by schema: %v", err)
			}
			var request ResearchCreate
			if err := json.Unmarshal(input, &request); err != nil {
				t.Fatalf("schema-valid integer rejected by generated Go boundary: %v", err)
			}
			var budget HardBudget
			if err := json.Unmarshal(request.Budget, &budget); err != nil {
				t.Fatalf("schema-valid nested amount rejected by generated Go boundary: %v", err)
			}
			if request.ObservationSeconds == nil || *request.ObservationSeconds != 60 || budget.Amount != 1000000 {
				t.Fatal("exact integer value was changed")
			}
			request.Budget, _ = json.Marshal(budget)
			encoded, err := json.Marshal(request)
			if err != nil || ValidateJSON("ResearchCreate", encoded) != nil {
				t.Fatalf("typed roundtrip broke the wire contract: %s: %v", encoded, err)
			}
		})
	}
	input := []byte(`{"expected_version":9007199254740991.0}`)
	if err := ValidateJSON("AccountPatch", input); err != nil {
		t.Fatal(err)
	}
	var patch AccountPatch
	if err := json.Unmarshal(input, &patch); err != nil || patch.ExpectedVersion != 9007199254740991 {
		t.Fatalf("maximum safe integer lost precision: %v", err)
	}
}

func TestOutputSchemaCannotLoadExternalResources(t *testing.T) {
	for _, schema := range []string{`{"$ref":"https://example.com/schema"}`, `{"$ref":"file:///synthetic/schema"}`, `{"type":"invalid"}`, `{"description":"` + strings.Repeat("x", 16384) + `"}`} {
		input := `{"input":"synthetic report","budget":{"mode":"hard","unit":"micro_usd","amount":1000000},"output_schema":` + schema + `}`
		if err := ValidateJSON("ResearchCreate", []byte(input)); err == nil {
			t.Fatal("unsafe/invalid output schema accepted")
		}
	}
	valid := `{"input":"synthetic report","budget":{"mode":"hard","unit":"micro_usd","amount":1000000},"output_schema":{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"]}}`
	if err := ValidateJSON("ResearchCreate", []byte(valid)); err != nil {
		t.Fatal(err)
	}
}
