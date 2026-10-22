package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"exta/internal/domain"
	"exta/internal/store"
)

func TestManagementTransportErrorsRemainJSON(t *testing.T) {
	directory, _ := operationFixture(t)
	state, auth, configuration, lock, err := openRuntime(t.Context(), directory)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	defer state.Close()
	var owner string
	if err = state.DB.QueryRow("SELECT id FROM principals LIMIT 1").Scan(&owner); err != nil {
		t.Fatal(err)
	}
	token, err := state.MintToken(t.Context(), owner, domain.TokenCreate{Label: "Transport regression", Scopes: []domain.ClientScope{"read"}, ExpiresAt: store.Stamp(time.Now().Add(time.Hour))})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := managementHandler(state, auth, configuration)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		method, path, contentType, accept, allow string
		status                                   int
	}{
		{"GET", "/mcp", "", "application/json", "POST", 405},
		{"POST", "/mcp", "text/plain", "application/json, text/event-stream", "", 415},
		{"POST", "/mcp", "application/json", "text/plain", "", 400},
		{"OPTIONS", "/v1/admin/settings", "", "application/json", "GET, HEAD, PATCH", 405},
	} {
		request := httptest.NewRequest(test.method, configuration.Origin+test.path, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
		request.Header.Set("Authorization", "Bearer "+token.TokenValue)
		request.Header.Set("Mcp-Protocol-Version", "2025-11-25")
		request.Header.Set("Content-Type", test.contentType)
		request.Header.Set("Accept", test.accept)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != test.status || !json.Valid(response.Body.Bytes()) || !strings.Contains(response.Header().Get("Content-Type"), "application/json") || response.Header().Get("Allow") != test.allow {
			t.Errorf("%s %s: %d %v %s", test.method, test.path, response.Code, response.Header(), response.Body.String())
		}
	}
}
