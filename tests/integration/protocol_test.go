package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"exta/internal/domain"
	"exta/internal/httpapi"
	"exta/internal/security"
	"exta/internal/service"
	"exta/internal/store"
)

const fixtureOrigin = "http://127.0.0.1:8789"

func gateway(t *testing.T, state *store.Store, core *service.Service) http.Handler {
	t.Helper()
	auth, err := security.NewAuth(state, fixtureOrigin, nil)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	usage := &service.UsageService{Store: state, Clients: core.Clients}
	err = httpapi.Register(mux, auth, core, usage, httpapi.Options{Origin: fixtureOrigin, Version: "test", Readiness: func() domain.Readiness {
		return domain.Readiness{Database: "ready", SecretStore: "unlocked", Assets: "missing", BindScope: "loopback", Providers: []json.RawMessage{}}
	}})
	if err != nil {
		t.Fatal(err)
	}
	return auth.Guard(mux)
}

func scopedToken(t *testing.T, state *store.Store, principal string, scopes ...domain.ClientScope) string {
	t.Helper()
	token, err := state.MintToken(t.Context(), principal, domain.TokenCreate{Label: "Synthetic fixture", ExpiresAt: store.Stamp(time.Now().Add(time.Hour)), Scopes: scopes})
	if err != nil {
		t.Fatal(err)
	}
	return token.TokenValue
}

func dispatch(handler http.Handler, method, path, body, token string, headers map[string]string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, fixtureOrigin+path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestRESTAndMCPUseSameRetrievalAuthorizationAndAccounting(t *testing.T) {
	state, principal, _, _ := foundation(t)
	mock := newMockProvider(t, mockReply{Status: 200, Body: `{"results":[],"costDollars":{"total":0}}`}, mockReply{Status: 200, Body: `{"results":[],"costDollars":{"total":0}}`})
	core, _ := retrievalService(t, state, mock, nil)
	handler := gateway(t, state, core)
	execute := scopedToken(t, state, principal, "execute")
	read := scopedToken(t, state, principal, "read")
	for _, fixture := range []struct {
		token  string
		status int
	}{{"", 401}, {read, 403}} {
		response := dispatch(handler, "POST", "/v1/search", `{"query":"q"}`, fixture.token, nil)
		if response.Code != fixture.status {
			t.Fatalf("REST denial %d %s", response.Code, response.Body.String())
		}
	}
	assertCount(t, state, "SELECT count(*) FROM requests", 0)
	assertCount(t, state, "SELECT count(*) FROM budget_reservations", 0)
	response := dispatch(handler, "POST", "/v1/search", `{"query":"q"}`, execute, nil)
	if response.Code != 200 {
		t.Fatalf("REST retrieval: %d %s", response.Code, response.Body.String())
	}
	if err := domain.ValidateJSON("RetrievalResponse", response.Body.Bytes()); err != nil {
		t.Fatal(err)
	}
	var rest domain.RetrievalResponse
	json.Unmarshal(response.Body.Bytes(), &rest)
	response = dispatch(handler, "GET", "/v1/requests/"+rest.Meta.RequestId, "", read, nil)
	if response.Code != 200 {
		t.Fatalf("request details: %s", response.Body.String())
	}
	modern := map[string]string{"Mcp-Protocol-Version": "2026-07-28", "Mcp-Method": "tools/call", "Mcp-Name": "search"}
	call := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search","arguments":{"query":"q"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}`
	denied := dispatch(handler, "POST", "/mcp", call, read, modern)
	if denied.Code != 200 || !strings.Contains(denied.Body.String(), `"isError":true`) || !strings.Contains(denied.Body.String(), "forbidden") {
		t.Fatalf("MCP scope denial %d %s", denied.Code, denied.Body.String())
	}
	assertCount(t, state, "SELECT count(*) FROM requests", 1)
	response = dispatch(handler, "POST", "/mcp", call, execute, modern)
	if response.Code != 200 || strings.Contains(response.Body.String(), `"isError":true`) {
		t.Fatalf("MCP retrieval %d %s", response.Code, response.Body.String())
	}
	var rpc struct {
		Result struct {
			Structured json.RawMessage `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &rpc); err != nil {
		t.Fatal(err)
	}
	if err := domain.ValidateJSON("RetrievalResponse", rpc.Result.Structured); err != nil {
		t.Fatal(err)
	}
	assertCount(t, state, "SELECT count(*) FROM requests", 2)
	assertCount(t, state, "SELECT count(*) FROM usage_ledger", 2)
	assertCount(t, state, "SELECT count(*) FROM budget_reservations WHERE state='held'", 0)
	if len(mock.Calls()) != 2 {
		t.Fatal("transport bypass or retry")
	}
}

func TestMCPModernAndLegacyProtocolBoundaries(t *testing.T) {
	state, principal, _, _ := foundation(t)
	mock := newMockProvider(t)
	core, _ := retrievalService(t, state, mock, nil)
	handler := gateway(t, state, core)
	token := scopedToken(t, state, principal, "read")
	meta := map[string]any{"io.modelcontextprotocol/protocolVersion": "2026-07-28", "io.modelcontextprotocol/clientCapabilities": map[string]any{}}
	for _, method := range []string{"server/discover", "tools/list", "tools/call"} {
		parameters := map[string]any{"_meta": meta}
		headers := map[string]string{"Mcp-Protocol-Version": "2026-07-28", "Mcp-Method": method}
		if method == "tools/call" {
			parameters["name"] = "capabilities"
			parameters["arguments"] = map[string]any{}
			headers["Mcp-Name"] = "capabilities"
		}
		response := dispatch(handler, "POST", "/mcp", store.JSON(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": parameters}), token, headers)
		var envelope struct {
			Error json.RawMessage `json:"error"`
		}
		decodeErr := json.Unmarshal(response.Body.Bytes(), &envelope)
		if response.Code != 200 || decodeErr != nil || len(envelope.Error) > 0 {
			t.Fatalf("modern %s %d %s", method, response.Code, response.Body.String())
		}
		if response.Header().Get("Mcp-Session-Id") != "" {
			t.Fatal("stateless server created a session")
		}
		if !strings.Contains(response.Header().Get("Cache-Control"), "no-store") {
			t.Fatal("MCP permits caching authenticated results")
		}
	}
	legacy := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"fixture","version":"1"}}}`
	response := dispatch(handler, "POST", "/mcp", legacy, token, nil)
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"protocolVersion":"2025-11-25"`) {
		t.Fatalf("legacy initialize %d %s", response.Code, response.Body.String())
	}
	response = dispatch(handler, "POST", "/mcp", `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`, token, map[string]string{"Mcp-Protocol-Version": "2025-11-25"})
	if response.Code != 200 {
		t.Fatalf("legacy tools %d %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Header().Get("Cache-Control"), "no-store") {
		t.Fatal("legacy MCP permits caching authenticated results")
	}
	for _, headers := range []map[string]string{nil, {"Mcp-Protocol-Version": "2099-01-01"}, {"Mcp-Protocol-Version": "2026-07-28"}, {"Mcp-Protocol-Version": "2026-07-28", "Mcp-Method": "tools/call", "Mcp-Name": "wrong"}} {
		call := store.JSON(map[string]any{"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": map[string]any{"name": "capabilities", "arguments": map[string]any{}, "_meta": meta}})
		response = dispatch(handler, "POST", "/mcp", call, token, headers)
		if response.Code != 400 {
			t.Fatalf("invalid headers accepted %v %d %s", headers, response.Code, response.Body.String())
		}
		var envelope struct {
			JSONRPC string `json:"jsonrpc"`
			ID      int    `json:"id"`
			Error   struct {
				Code int `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil || envelope.JSONRPC != "2.0" || envelope.ID != 3 || envelope.Error.Code == 0 {
			t.Fatalf("uncorrelated protocol error: %s", response.Body.String())
		}
	}
	for _, method := range []string{"GET", "DELETE"} {
		response = dispatch(handler, method, "/mcp", "", token, nil)
		if response.Code != 405 {
			t.Fatalf("stateless %s %d", method, response.Code)
		}
	}
	response = dispatch(handler, "POST", "/mcp", `[{"jsonrpc":"2.0","id":1,"method":"tools/list"}]`, token, map[string]string{"Mcp-Protocol-Version": "2025-11-25"})
	if response.Code != 400 {
		t.Fatalf("batch accepted %d %s", response.Code, response.Body.String())
	}
	assertCount(t, state, "SELECT count(*) FROM requests", 0)
	if len(mock.Calls()) != 0 {
		t.Fatal("metadata made a provider call")
	}
}

func TestAdminMetadataAndMutationsFollowFrozenContract(t *testing.T) {
	state, _, _, _ := foundation(t)
	core, _ := retrievalService(t, state, newMockProvider(t), nil)
	handler := gateway(t, state, core)
	session, cookie, err := state.Login(t.Context(), "synthetic-local-operator-for-foundation", "fixture")
	if err != nil {
		t.Fatal(err)
	}
	headers := map[string]string{"Cookie": "exta_session=" + cookie, "Origin": fixtureOrigin, "X-Exta-CSRF": session.CsrfToken}
	for path, schema := range map[string]string{"/v1/admin/connection-info": "ConnectionInfo", "/v1/admin/requests": "RequestRecordList", "/v1/admin/usage": "KeyUsageView", "/v1/admin/audit": "AuditEventList"} {
		response := dispatch(handler, "GET", path, "", "", headers)
		if response.Code != 200 {
			t.Fatalf("metadata %s: %d %s", path, response.Code, response.Body.String())
		}
		if err = domain.ValidateJSON(schema, response.Body.Bytes()); err != nil {
			t.Fatalf("%s: %v\n%s", schema, err, response.Body.String())
		}
	}
	route, _ := state.Route(t.Context(), "search")
	input := domain.RouteReplace{ExpectedVersion: route.Revision, Enabled: true, Targets: route.Targets, DeadlineSeconds: 30, MaxAttempts: 3}
	for name, value := range map[string]string{"Origin": "https://wrong.example", "X-Exta-CSRF": "wrong"} {
		bad := map[string]string{}
		for key, item := range headers {
			bad[key] = item
		}
		bad[name] = value
		response := dispatch(handler, "PUT", "/v1/admin/routes/search", store.JSON(input), "", bad)
		if response.Code != 403 {
			t.Fatalf("mutation denial %d %s", response.Code, response.Body.String())
		}
	}
	assertCount(t, state, "SELECT count(*) FROM route_policies WHERE capability='search'", 1)
	response := dispatch(handler, "PUT", "/v1/admin/routes/search", store.JSON(input), "", headers)
	if response.Code != 200 {
		t.Fatalf("route save %d %s", response.Code, response.Body.String())
	}
	response = dispatch(handler, "PUT", "/v1/admin/routes/search", store.JSON(input), "", headers)
	if response.Code != 409 {
		t.Fatal("stale version accepted")
	}
	response = dispatch(handler, "POST", "/v1/admin/routes/preview", `{"operation":"search","request":{"query":"q"}}`, "", headers)
	if response.Code != 200 {
		t.Fatalf("preview %d %s", response.Code, response.Body.String())
	}
	assertCount(t, state, "SELECT count(*) FROM budget_reservations", 0)
}
