package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"

	"exta/internal/domain"
	"exta/internal/store"
)

func TestManagementProviderFiltersBeforePagination(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "state")
	var output bytes.Buffer
	if err := initialize(t.Context(), directory, "synthetic-administrator-password", &output); err != nil {
		t.Fatal(err)
	}
	credential := "synthetic-administrator-password"
	state, auth, configuration, lock, err := openRuntime(t.Context(), directory)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	defer state.Close()
	session, token, err := state.Login(t.Context(), credential, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	for _, provider := range []domain.Provider{"exa", "tavily"} {
		unit := domain.UsageUnit("micro_usd")
		if provider == "tavily" {
			unit = "tavily_credit"
		}
		for range 2 {
			if _, err := state.AddKey(t.Context(), session.PrincipalId, domain.UpstreamKeyCreate{Provider: provider, Label: "Filter key", Secret: "synthetic-filter-" + store.ID("key"), QuotaPolicy: (domain.QuotaPolicy{Unit: unit, ActiveJobs: 1}).Input()}); err != nil {
				t.Fatal(err)
			}
		}
	}
	handler, err := managementHandler(state, auth, configuration)
	if err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"keys"} {
		for _, provider := range []string{"exa", "tavily"} {
			cursor := ""
			seen := map[string]bool{}
			for count := 0; count < 3; count++ {
				request := httptest.NewRequest("GET", configuration.Origin+"/v1/admin/"+endpoint+"?provider="+provider+"&limit=1&cursor="+url.QueryEscape(cursor), nil)
				request.AddCookie(&http.Cookie{Name: "exta_session", Value: token})
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				var result struct {
					Items      []struct{ Id, Provider string }
					NextCursor *string `json:"next_cursor"`
				}
				if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &result) != nil {
					t.Fatalf("list: %d %s", response.Code, response.Body.String())
				}
				for _, item := range result.Items {
					if item.Provider != provider || seen[item.Id] {
						t.Fatalf("%s filter leaked or duplicated %+v", endpoint, item)
					}
					seen[item.Id] = true
				}
				if result.NextCursor == nil {
					break
				}
				cursor = *result.NextCursor
			}
			if len(seen) != 2 {
				t.Fatalf("%s %s pagination returned %d", endpoint, provider, len(seen))
			}
		}
		request := httptest.NewRequest("GET", configuration.Origin+"/v1/admin/"+endpoint+"?provider=unknown", nil)
		request.AddCookie(&http.Cookie{Name: "exta_session", Value: token})
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != 400 {
			t.Fatalf("invalid provider: %d", response.Code)
		}
	}
}
