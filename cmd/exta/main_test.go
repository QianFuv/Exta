package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"exta/internal/domain"
	"exta/internal/security"
	"exta/internal/store"
)

func TestServeNeverSilentlyInitializes(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "absent")
	var output bytes.Buffer
	if err := run(t.Context(), []string{"serve", "--data-dir", directory}, &output, &output); err == nil {
		t.Fatal("serve initialized absent state")
	}
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatal("serve changed absent directory")
	}
}

func TestSyntheticRuntimeInitializationAndSafeStartup(t *testing.T) {
	directory := t.TempDir()
	var output bytes.Buffer
	if err := run(t.Context(), []string{"init", "--data-dir", directory}, &output, &output); err != nil {
		t.Fatal(err)
	}
	credential := strings.TrimSpace(strings.Split(output.String(), "\n")[1])
	output.Reset()
	if err := run(t.Context(), []string{"init", "--data-dir", directory}, &output, &output); err == nil {
		t.Fatal("second init overwrote state")
	}
	state, auth, configuration, lock, err := openRuntime(t.Context(), directory)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	defer state.Close()
	handler, err := managementHandler(state, auth, configuration)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("POST", configuration.Origin+"/v1/admin/session", strings.NewReader(`{"credential":"`+credential+`"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", configuration.Origin)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatalf("login %d %s", response.Code, response.Body.String())
	}
	if err = domain.ValidateJSON("Session", response.Body.Bytes()); err != nil {
		t.Fatal(err)
	}
	var session domain.Session
	json.Unmarshal(response.Body.Bytes(), &session)
	cookie := response.Result().Cookies()[0]
	request = httptest.NewRequest("GET", configuration.Origin+"/v1/admin/settings", nil)
	request.AddCookie(cookie)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatalf("settings %d %s", response.Code, response.Body.String())
	}
	if err = domain.ValidateJSON("Settings", response.Body.Bytes()); err != nil {
		t.Fatal(err)
	}
	var settings domain.Settings
	json.Unmarshal(response.Body.Bytes(), &settings)
	if settings.Readiness.Ready || settings.Readiness.SecretStore != "unlocked" {
		t.Fatalf("false readiness: %+v", settings.Readiness)
	}
	request = httptest.NewRequest("POST", configuration.Origin+"/v1/admin/accounts", strings.NewReader(`{"provider":"exa","label":"Synthetic","enabled":true,"quota_policy":{"unit":"micro_usd","allocation":0,"per_operation_reservation":1,"active_jobs":1}}`))
	request.AddCookie(cookie)
	request.Header.Set("Origin", configuration.Origin)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Exta-CSRF", session.CsrfToken)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 201 {
		t.Fatalf("account create %d %s", response.Code, response.Body.String())
	}
	if err = domain.ValidateJSON("Account", response.Body.Bytes()); err != nil {
		t.Fatal(err)
	}
	if _, _, _, second, err := openRuntime(t.Context(), directory); err == nil {
		second.Close()
		t.Fatal("two processes may share database")
	}
	request = httptest.NewRequest("GET", configuration.Origin+"/v1/ready", nil)
	request.AddCookie(cookie)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatal("admin cookie granted read scope")
	}
}

func TestWrongMasterKeyLeavesReadOnlyMetadataAvailable(t *testing.T) {
	directory := t.TempDir()
	var output bytes.Buffer
	if err := initialize(t.Context(), directory, &output); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "master.key"), bytes.Repeat([]byte{42}, 32), 0600); err != nil {
		t.Fatal(err)
	}
	state, _, configuration, lock, err := openRuntime(t.Context(), directory)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	defer lock.Close()
	if state.Vault != nil || readiness(state, configuration).SecretStore != "locked" {
		t.Fatal("wrong master key reported unlocked")
	}
	if _, err = state.Accounts(t.Context()); err != nil {
		t.Fatal("locked store lost readable metadata")
	}
}

func TestUnsafeNetworkConfigurationFailsBeforeOpen(t *testing.T) {
	directory := t.TempDir()
	data := []byte(`{"listen":"0.0.0.0:8789","origin":"http://example.com:8789"}`)
	if err := os.WriteFile(filepath.Join(directory, "config.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(directory); err == nil {
		t.Fatal("unsafe nonloopback accepted")
	}
}

func TestServeRejectsEmptyAndIncompleteBootstrap(t *testing.T) {
	for _, empty := range []bool{true, false} {
		t.Run(map[bool]string{true: "empty", false: "missing_admin"}[empty], func(t *testing.T) {
			directory := t.TempDir()
			if err := security.ProtectDirectory(directory); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(directory, "config.json"), []byte(`{"listen":"127.0.0.1:8789","origin":"http://127.0.0.1:8789"}`), 0600); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(directory, "exta.sqlite")
			if empty {
				if err := os.WriteFile(path, nil, 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				state, err := store.Open(t.Context(), path, nil)
				if err != nil {
					t.Fatal(err)
				}
				state.Close()
			}
			state, _, _, lock, err := openRuntime(t.Context(), directory)
			if err == nil {
				state.Close()
				lock.Close()
				t.Fatal("serve accepted incomplete initialization")
			}
			if empty {
				data, err := os.ReadFile(path)
				if err != nil || len(data) != 0 {
					t.Fatal("serve altered empty database")
				}
			}
		})
	}
}

func TestEightCharacterCredentialCreationAndRotation(t *testing.T) {
	directory := t.TempDir()
	var output bytes.Buffer
	if err := initialize(t.Context(), directory, &output); err != nil {
		t.Fatal(err)
	}
	credential := strings.TrimSpace(strings.Split(output.String(), "\n")[1])
	state, auth, configuration, lock, err := openRuntime(t.Context(), directory)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	defer lock.Close()
	session, token, err := state.Login(t.Context(), credential, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	account, err := state.CreateAccount(t.Context(), session.PrincipalId, domain.AccountCreate{Provider: "exa", Label: "Synthetic boundary", Enabled: true, QuotaPolicy: domain.QuotaPolicy{Unit: "micro_usd", Allocation: 100, PerOperationReservation: 1, ActiveJobs: 1}})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := managementHandler(state, auth, configuration)
	if err != nil {
		t.Fatal(err)
	}
	call := func(path, body string, status int) domain.ProviderKey {
		t.Helper()
		request := httptest.NewRequest("POST", configuration.Origin+path, strings.NewReader(body))
		request.AddCookie(&http.Cookie{Name: "exta_session", Value: token})
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", configuration.Origin)
		request.Header.Set("X-Exta-CSRF", session.CsrfToken)
		writer := httptest.NewRecorder()
		handler.ServeHTTP(writer, request)
		if writer.Code != status {
			t.Fatalf("status=%d body=%s", writer.Code, writer.Body.String())
		}
		if err := domain.ValidateJSON("ProviderKey", writer.Body.Bytes()); err != nil {
			t.Fatal(err)
		}
		var key domain.ProviderKey
		json.Unmarshal(writer.Body.Bytes(), &key)
		if strings.Contains(writer.Body.String(), "abcdefgh") || strings.Contains(writer.Body.String(), "ijklmnop") {
			t.Fatal("full secret in response")
		}
		return key
	}
	key := call("/v1/admin/keys", store.JSON(domain.KeyCreate{AccountId: account.Id, Label: "Eight", Enabled: true, Secret: "abcdefgh"}), 201)
	rotated := call("/v1/admin/keys/"+key.Id+"/rotate", `{"expected_version":1,"secret":"ijklmnop"}`, 200)
	if rotated.LastFour != "mnop" {
		t.Fatal("masked response changed")
	}
	var active string
	var ciphertext []byte
	if err = state.DB.QueryRow("SELECT c.id,c.ciphertext FROM provider_keys k JOIN credential_versions c ON c.id=k.active_version WHERE k.id=?", key.Id).Scan(&active, &ciphertext); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ciphertext, []byte("ijklmnop")) {
		t.Fatal("rotation persisted plaintext")
	}
	secret, err := state.Credential(t.Context(), active)
	if err != nil || secret != "ijklmnop" {
		t.Fatal("rotation did not retain encrypted secret", err)
	}
}
