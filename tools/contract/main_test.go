package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func loadTestContract(t *testing.T) document {
	t.Helper()
	t.Chdir("../..")
	content, err := os.ReadFile("api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var contract document
	if err = json.Unmarshal(content, &contract); err != nil {
		t.Fatal(err)
	}
	return contract
}

func TestNullablePolicyGeneratesTypedOptionalFields(t *testing.T) {
	output, err := generateGo(loadTestContract(t), "synthetic_hash")
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range []string{"type QuotaPolicyInput struct", "*QuotaPolicyInput", "*Integer"} {
		if !strings.Contains(string(output), declaration) {
			t.Fatalf("missing typed nullable policy: %s", declaration)
		}
	}
}

func TestBackendGenerationExcludesFrontendArtifacts(t *testing.T) {
	contract := loadTestContract(t)
	artifacts, err := generate(contract, "synthetic_hash", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 4 {
		t.Fatalf("backend artifacts %d", len(artifacts))
	}
	for path := range artifacts {
		if strings.HasPrefix(path, "web/") {
			t.Fatalf("frontend generated under backend-only: %s", path)
		}
	}
}

func TestAdminResearchCannotLoseAuthorizationOrReplayContract(t *testing.T) {
	contract := loadTestContract(t)
	if err := validateInvariants(contract); err != nil {
		t.Fatal(err)
	}
	mutations := map[string]func(document){
		"origin-policy-removed": func(operation document) { delete(operation, "x-origin-policy") },
		"cookie-removed":        func(operation document) { operation["security"] = []any{} },
		"permission-weakened":   func(operation document) { operation["x-permission"] = "admin:read" },
		"identity-made-session-specific": func(operation document) {
			asMap(operation["x-idempotency"])["identity"] = "session_id"
		},
	}
	for _, header := range []string{"X-Exta-CSRF", "Idempotency-Key"} {
		mutations["missing-"+header] = func(operation document) {
			var parameters []any
			for _, parameter := range operation["parameters"].([]any) {
				if asMap(parameter)["name"] != header {
					parameters = append(parameters, parameter)
				}
			}
			operation["parameters"] = parameters
		}
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			candidate := clone(contract)
			operation := asMap(asMap(asMap(candidate["paths"])["/v1/admin/research"])["post"])
			mutate(operation)
			if err := validateInvariants(candidate); err == nil {
				t.Fatal("weakened admin creation contract was accepted")
			}
		})
	}
	t.Run("both-transports-cannot-change-replay-scope", func(t *testing.T) {
		candidate := clone(contract)
		for _, path := range []string{"/v1/admin/research", "/v1/research"} {
			operation := asMap(asMap(asMap(candidate["paths"])[path])["post"])
			asMap(operation["x-idempotency"])["identity"] = "session_id"
		}
		if err := validateInvariants(candidate); err == nil {
			t.Fatal("matching but session-specific replay scope was accepted")
		}
	})
}

func TestGeneratedMcpResearchUsesSameStrictIntentSchema(t *testing.T) {
	contract := loadTestContract(t)
	for _, tool := range generateTools(contract, "synthetic_hash") {
		compiler := jsonschema.NewCompiler()
		compiler.DefaultDraft(jsonschema.Draft2020)
		compiler.UseLoader(offlineLoader{})
		for _, direction := range []string{"inputSchema", "outputSchema"} {
			address := "https://exta.invalid/" + tool["name"].(string) + "/" + direction
			if err := compiler.AddResource(address, map[string]any(asMap(tool[direction]))); err != nil {
				t.Fatal(err)
			}
			schema, err := compiler.Compile(address)
			if err != nil {
				t.Fatalf("%s %s: %v", tool["name"], direction, err)
			}
			if tool["name"] != "research_create" || direction != "inputSchema" {
				continue
			}
			value := map[string]any{"input": "Synthetic research", "idempotency_key": "synthetic-key-1234", "budget": map[string]any{"mode": "hard", "unit": "micro_usd", "amount": 1000000}}
			if err = schema.Validate(value); err != nil {
				t.Fatal(err)
			}
			delete(value, "idempotency_key")
			if schema.Validate(value) == nil {
				t.Fatal("MCP accepted research without retry identity")
			}
			value["idempotency_key"] = "synthetic-key-1234"
			value["owner_principal_id"] = "forged"
			if schema.Validate(value) == nil {
				t.Fatal("MCP accepted caller-supplied ownership")
			}
		}
	}
}

func TestLoginCannotLoseBrowserOriginEnforcement(t *testing.T) {
	contract := loadTestContract(t)
	operation := asMap(asMap(asMap(contract["paths"])["/v1/admin/session"])["post"])
	delete(operation, "x-origin-policy")
	if err := validateInvariants(contract); err == nil {
		t.Fatal("login without exact Origin enforcement was accepted")
	}
}
