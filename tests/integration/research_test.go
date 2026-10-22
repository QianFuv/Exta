package integration

import (
	"encoding/json"
	"strings"
	"testing"

	"exta/internal/domain"
	"exta/internal/store"
)

func TestResearchRESTAndMCPShareBindingAndOwnership(t *testing.T) {
	state, owner, _, _ := foundation(t)
	mock := newMockProvider(t)
	core, _ := retrievalService(t, state, mock, nil)
	handler := gateway(t, state, core)
	body := `{"input":"Synthetic research","budget":{"mode":"hard","unit":"micro_usd","amount":1000000}}`
	key := "synthetic-research-idempotency"
	token := scopedToken(t, state, owner, "research:create", "research:read")
	response := dispatch(handler, "POST", "/v1/research", body, token, map[string]string{"Idempotency-Key": key})
	var job domain.Job
	if response.Code != 202 || json.Unmarshal(response.Body.Bytes(), &job) != nil {
		t.Fatalf("REST accept %d %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Location") != "/v1/research/"+job.Id {
		t.Fatal("missing durable location")
	}
	var arguments map[string]any
	_ = json.Unmarshal([]byte(body), &arguments)
	arguments["idempotency_key"] = key
	call := store.JSON(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "research_create", "arguments": arguments, "_meta": map[string]any{"io.modelcontextprotocol/protocolVersion": "2026-07-28", "io.modelcontextprotocol/clientCapabilities": map[string]any{}}}})
	response = dispatch(handler, "POST", "/mcp", call, scopedToken(t, state, owner, "research:create"), map[string]string{"Mcp-Protocol-Version": "2026-07-28", "Mcp-Method": "tools/call", "Mcp-Name": "research_create"})
	var rpc struct {
		Result struct {
			Structured domain.Job `json:"structuredContent"`
		} `json:"result"`
	}
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &rpc) != nil || rpc.Result.Structured.Id != job.Id {
		t.Fatalf("MCP replay %d %s", response.Code, response.Body.String())
	}
	assertCount(t, state, "SELECT count(*) FROM research_jobs", 1)
	assertCount(t, state, "SELECT count(*) FROM budget_reservations", 1)
	session, cookie, err := state.Login(t.Context(), "synthetic-local-operator-for-foundation", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	adminHeaders := map[string]string{"Cookie": "exta_session=" + cookie, "Origin": fixtureOrigin, "Idempotency-Key": key}
	response = dispatch(handler, "POST", "/v1/admin/research", body, "", adminHeaders)
	if response.Code != 403 {
		t.Fatal("admin Research accepted missing CSRF")
	}
	adminHeaders["X-Exta-CSRF"] = session.CsrfToken
	response = dispatch(handler, "POST", "/v1/admin/research", body, "", adminHeaders)
	var adminJob domain.Job
	if response.Code != 202 || json.Unmarshal(response.Body.Bytes(), &adminJob) != nil || adminJob.Id != job.Id {
		t.Fatalf("admin lost stable binding: %d %s", response.Code, response.Body.String())
	}
	assertCount(t, state, "SELECT count(*) FROM research_jobs", 1)
	other := store.ID("principal")
	if _, err := state.DB.Exec("INSERT INTO principals VALUES(?,NULL,'[]',?)", other, store.Stamp(state.Now())); err != nil {
		t.Fatal(err)
	}
	response = dispatch(handler, "GET", "/v1/research/"+job.Id, "", scopedToken(t, state, other, "research:read"), nil)
	if response.Code != 404 {
		t.Fatal("cross-owner job exposed")
	}
	response = dispatch(handler, "GET", "/v1/research/"+job.Id, "", token, nil)
	if response.Code != 200 || !strings.Contains(response.Body.String(), job.Id) {
		t.Fatal("owner lost local job")
	}
	if len(mock.Calls()) != 0 {
		t.Fatal("acceptance or local reads dispatched upstream")
	}
	response = dispatch(handler, "GET", "/v1/research/"+job.Id+"?events_limit=0", "", token, nil)
	if response.Code != 400 {
		t.Fatal("REST accepted explicit zero event limit")
	}
}

func TestResearchAuthorizationPrecedesReservation(t *testing.T) {
	state, owner, _, _ := foundation(t)
	mock := newMockProvider(t)
	core, _ := retrievalService(t, state, mock, nil)
	handler := gateway(t, state, core)
	body := `{"input":"Synthetic research","budget":{"mode":"hard","unit":"micro_usd","amount":1000000}}`
	for _, entry := range []struct {
		token, origin string
		status        int
	}{{"", "", 401}, {scopedToken(t, state, owner, "read"), "", 403}, {scopedToken(t, state, owner, "research:create"), "https://invalid.example", 403}} {
		headers := map[string]string{"Idempotency-Key": "synthetic-research-idempotency"}
		if entry.origin != "" {
			headers["Origin"] = entry.origin
		}
		response := dispatch(handler, "POST", "/v1/research", body, entry.token, headers)
		if response.Code != entry.status {
			t.Fatalf("authorization %d %s", response.Code, response.Body.String())
		}
	}
	assertCount(t, state, "SELECT count(*) FROM research_jobs", 0)
	assertCount(t, state, "SELECT count(*) FROM budget_reservations", 0)
}
