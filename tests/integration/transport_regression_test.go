package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strings"
	"testing"
	"time"

	"exta/internal/domain"
	"exta/internal/providers"
	"exta/internal/security"
	"exta/internal/service"
	"exta/internal/store"
)

func TestProtocolChunkedBodyLimitPrecedesAdmission(t *testing.T) {
	state, principal, _, _ := foundation(t)
	mock := newMockProvider(t)
	core, _ := retrievalService(t, state, mock, nil)
	settings, _ := state.Settings(t.Context())
	settings.Limits.RequestBytes = 65536
	if _, err := state.SaveSettings(t.Context(), principal, domain.SettingsPatch{ExpectedVersion: settings.Version, Limits: &settings.Limits}); err != nil {
		t.Fatal(err)
	}
	handler := gateway(t, state, core)
	token := scopedToken(t, state, principal, "execute")
	for _, path := range []string{"/v1/search", "/mcp"} {
		request := httptest.NewRequest("POST", fixtureOrigin+path, strings.NewReader(strings.Repeat(" ", 65537)))
		request.ContentLength = -1
		request.TransferEncoding = []string{"chunked"}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/json, text/event-stream")
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != 413 {
			t.Fatalf("unbounded %s: %d", path, response.Code)
		}
	}
	assertCount(t, state, "SELECT count(*) FROM budget_reservations", 0)
	assertCount(t, state, "SELECT count(*) FROM requests", 0)
	if len(mock.Calls()) != 0 {
		t.Fatal("oversized request sent upstream")
	}
}

func TestRequestOwnershipUsesPrincipalAcrossTokenRotation(t *testing.T) {
	state, principal, _, _ := foundation(t)
	other := store.ID("principal")
	if _, err := state.DB.ExecContext(t.Context(), "INSERT INTO principals VALUES(?,NULL,'[]',?)", other, store.Stamp(state.Now())); err != nil {
		t.Fatal(err)
	}
	mock := newMockProvider(t, mockReply{Status: 200, Body: `{"results":[],"costDollars":{"total":0}}`})
	core, _ := retrievalService(t, state, mock, nil)
	handler := gateway(t, state, core)
	first := scopedToken(t, state, principal, "execute")
	response := dispatch(handler, "POST", "/v1/search", `{"query":"q"}`, first, nil)
	var result domain.RetrievalResponse
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &result) != nil {
		t.Fatal(response.Body.String())
	}
	for _, fixture := range []struct {
		owner  string
		status int
	}{{principal, 200}, {other, 404}} {
		response = dispatch(handler, "GET", "/v1/requests/"+result.Meta.RequestId, "", scopedToken(t, state, fixture.owner, "read"), nil)
		if response.Code != fixture.status {
			t.Fatalf("ownership: %d %s", response.Code, response.Body.String())
		}
	}
}

func TestProtocolDisconnectCancelsRetrievalAndRetainsUnknownCost(t *testing.T) {
	for _, protocol := range []string{"REST", "MCP"} {
		t.Run(protocol, func(t *testing.T) {
			state, principal, _, _ := foundation(t)
			entered := make(chan struct{})
			client, err := providers.New("exa", localTransport(func(request *http.Request) (*http.Response, error) {
				if trace := httptrace.ContextClientTrace(request.Context()); trace != nil && trace.GotConn != nil {
					trace.GotConn(httptrace.GotConnInfo{})
				}
				close(entered)
				<-request.Context().Done()
				return nil, request.Context().Err()
			}))
			if err != nil {
				t.Fatal(err)
			}
			core, err := service.New(state, map[domain.Provider]*providers.Client{"exa": client}, security.UrlPolicy{})
			if err != nil {
				t.Fatal(err)
			}
			handler := gateway(t, state, core)
			path, body := "/v1/search", `{"query":"q"}`
			if protocol == "MCP" {
				path, body = "/mcp", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search","arguments":{"query":"q"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}`
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			request := httptest.NewRequest("POST", fixtureOrigin+path, strings.NewReader(body)).WithContext(ctx)
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Accept", "application/json, text/event-stream")
			request.Header.Set("Authorization", "Bearer "+scopedToken(t, state, principal, "execute"))
			if protocol == "MCP" {
				request.Header.Set("Mcp-Protocol-Version", "2026-07-28")
				request.Header.Set("Mcp-Method", "tools/call")
				request.Header.Set("Mcp-Name", "search")
			}
			done := make(chan struct{})
			go func() { defer close(done); handler.ServeHTTP(httptest.NewRecorder(), request) }()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("request did not dispatch")
			}
			cancel()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("disconnect did not drain")
			}
			assertCount(t, state, "SELECT count(*) FROM attempts", 1)
			assertCount(t, state, "SELECT count(*) FROM budget_reservations WHERE state='held'", 1)
			assertCount(t, state, "SELECT count(*) FROM requests WHERE json_extract(data,'$.billing_uncertain')=1", 1)
		})
	}
}
