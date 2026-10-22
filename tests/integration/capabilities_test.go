package integration

import (
	"encoding/json"
	"testing"

	"exta/internal/domain"
)

func TestCapabilitiesRequireConfiguredAndSufficientReservation(t *testing.T) {
	for _, fixture := range []struct {
		name                    string
		allocation, reservation domain.Integer
		enabled                 bool
	}{{"unset", 1000000, 0, false}, {"configured exceeds allocation", 5, 10, false}, {"profile floor exceeds allocation", 6000, 1, false}, {"eligible", 7000, 1, true}} {
		t.Run(fixture.name, func(t *testing.T) {
			state, principal, account, _ := foundation(t)
			quota := account.QuotaPolicy
			quota.Allocation = fixture.allocation
			quota.PerOperationReservation = fixture.reservation
			if _, err := state.PatchAccount(t.Context(), principal, account.Id, domain.AccountPatch{ExpectedVersion: account.Version, QuotaPolicy: &quota}); err != nil {
				t.Fatal(err)
			}
			mock := newMockProvider(t)
			core, _ := retrievalService(t, state, mock, nil)
			result, err := core.Capabilities(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, capability := range result.Capabilities {
				if capability.Operation == "search" {
					found = true
					if capability.Enabled != fixture.enabled {
						t.Fatalf("eligibility: %+v", capability)
					}
				}
			}
			if !found {
				t.Fatal("search missing")
			}
			assertCount(t, state, "SELECT count(*) FROM budget_reservations", 0)
			assertCount(t, state, "SELECT count(*) FROM rate_windows", 0)
			if len(mock.Calls()) != 0 {
				t.Fatal("discovery dispatches upstream")
			}
		})
	}
}

func TestCapabilitiesExposePersistedLimitsThroughBothProtocols(t *testing.T) {
	state, principal, _, _ := foundation(t)
	core, _ := retrievalService(t, state, newMockProvider(t), nil)
	settings, err := state.Settings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	settings.Limits.RequestBytes = 65536
	settings.Limits.ResponseBytes = 262144
	settings.Limits.SynchronousConcurrency = 2
	if _, err = state.SaveSettings(t.Context(), principal, domain.SettingsPatch{ExpectedVersion: settings.Version, Limits: &settings.Limits}); err != nil {
		t.Fatal(err)
	}
	handler := gateway(t, state, core)
	token := scopedToken(t, state, principal, "read")
	rest := dispatch(handler, "GET", "/v1/capabilities", "", token, nil)
	var direct domain.Capabilities
	if err = json.Unmarshal(rest.Body.Bytes(), &direct); err != nil || rest.Code != 200 {
		t.Fatalf("REST %s", rest.Body.String())
	}
	rpc := dispatch(handler, "POST", "/mcp", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"capabilities","arguments":{},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}`, token, map[string]string{"Mcp-Protocol-Version": "2026-07-28", "Mcp-Method": "tools/call", "Mcp-Name": "capabilities"})
	var envelope struct {
		Result struct {
			Structured domain.Capabilities `json:"structuredContent"`
		} `json:"result"`
	}
	if err = json.Unmarshal(rpc.Body.Bytes(), &envelope); err != nil || rpc.Code != 200 {
		t.Fatalf("MCP %s", rpc.Body.String())
	}
	for _, result := range []domain.Capabilities{direct, envelope.Result.Structured} {
		for _, capability := range result.Capabilities {
			var defaults map[string]any
			if err = json.Unmarshal(capability.DefaultLimits, &defaults); err != nil {
				t.Fatal(err)
			}
			for key, value := range map[string]float64{"request_bytes": 65536, "response_bytes": 262144, "synchronous_concurrency": 2} {
				if defaults[key] != value {
					t.Fatalf("%s effective %s=%v", capability.Operation, key, defaults[key])
				}
			}
			if capability.Operation == "map" || capability.Operation == "crawl" {
				if defaults["max_depth"] != float64(2) || defaults["max_breadth"] != float64(20) || defaults["allow_external"] != false {
					t.Fatal("missing traversal defaults")
				}
			}
		}
	}
	assertCount(t, state, "SELECT count(*) FROM budget_reservations", 0)
}

func TestResearchCapabilitiesDescribeFrozenPolicyBounds(t *testing.T) {
	state, owner, _, _ := foundation(t)
	addTavily(t, state, owner)
	route, err := state.Route(t.Context(), "research_create")
	if err != nil {
		t.Fatal(err)
	}
	_, err = state.SaveRoute(t.Context(), owner, "research_create", domain.RouteReplace{ExpectedVersion: route.Revision, Enabled: true, Targets: []domain.RouteTarget{{Provider: "tavily", Profile: "tavily_mini"}}, MaxAttempts: 3, DeadlineSeconds: 60})
	if err != nil {
		t.Fatal(err)
	}
	core, _ := retrievalService(t, state, nil, newMockProvider(t))
	result, err := core.Capabilities(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, capability := range result.Capabilities {
		if capability.Operation == "research_create" {
			var defaults, hard map[string]any
			_ = json.Unmarshal(capability.DefaultLimits, &defaults)
			_ = json.Unmarshal(capability.HardLimits, &hard)
			if defaults["profile"] != "tavily_mini" || defaults["max_attempts"] != float64(2) || defaults["deadline_seconds"] != float64(30) || hard["max_attempts"] != float64(2) {
				t.Fatalf("false Research policy: %v %v", defaults, hard)
			}
		}
	}
}
