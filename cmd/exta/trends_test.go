package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"exta/internal/domain"
)

func TestCallTrendsRequiresAdminAndBoundsTheRange(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "state")
	if err := initialize(t.Context(), directory, "synthetic-administrator-password", &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	state, auth, configuration, lock, err := openRuntime(t.Context(), directory)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	defer state.Close()
	_, token, err := state.Login(t.Context(), "synthetic-administrator-password", "fixture")
	if err != nil {
		t.Fatal(err)
	}
	handler, err := managementHandler(state, auth, configuration)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		query         string
		authenticated bool
		status        int
	}{{"", false, 401}, {"?hours=25", true, 400}, {"?hours=168", true, 200}} {
		request := httptest.NewRequest("GET", configuration.Origin+"/v1/admin/usage/trends"+item.query, nil)
		if item.authenticated {
			request.AddCookie(&http.Cookie{Name: "exta_session", Value: token})
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != item.status {
			t.Fatalf("%s: %d %s", item.query, response.Code, response.Body.String())
		}
		if response.Code == 200 {
			if err := domain.ValidateJSON("CallTrends", response.Body.Bytes()); err != nil {
				t.Fatal(err)
			}
		}
	}
}
