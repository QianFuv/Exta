package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"exta/internal/domain"
	"exta/internal/security"
	"exta/internal/store"
)

func TestTrustedProxyHTTPConfiguration(t *testing.T) {
	directory := t.TempDir()
	data := []byte(`{"listen":"0.0.0.0:8789","origin":"https://exta.example","trusted_proxy":{"ips":["172.30.0.2"]}}`)
	if err := os.WriteFile(filepath.Join(directory, "config.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(directory); err != nil {
		t.Fatalf("explicit trusted proxy must allow internal HTTP with a fixed HTTPS origin: %v", err)
	}
}

func TestProxyConfigurationKeepsDirectDefaultsAndRejectsMixedTrust(t *testing.T) {
	for _, data := range []string{
		`{"listen":"0.0.0.0:8789","origin":"https://exta.example"}`,
		`{"listen":"0.0.0.0:8789","origin":"http://exta.example"}`,
		`{"listen":"0.0.0.0:8789","origin":"http://exta.example","trusted_proxy":{"ips":["172.30.0.2"]}}`,
		`{"listen":"0.0.0.0:8789","origin":"https://exta.example","trusted_proxy":{}}`,
		`{"listen":"0.0.0.0:8789","origin":"https://exta.example","trusted_proxy":{"ips":["0.0.0.0/0"]}}`,
		`{"listen":"0.0.0.0:8789","origin":"https://exta.example","trusted_proxy":{"ips":["172.30.0.2"]},"tls_cert_file":"cert","tls_key_file":"key"}`,
		`{"listen":"0.0.0.0:8789","origin":"https://exta.example","trusted_proxy":{"ips":["172.30.0.2"]},"development_origins":["http://127.0.0.1:5173"]}`,
	} {
		directory := t.TempDir()
		if err := os.WriteFile(filepath.Join(directory, "config.json"), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadConfig(directory); err == nil {
			t.Errorf("unsafe or ambiguous configuration accepted: %s", data)
		}
	}
}

func proxyFixture(t *testing.T, origin, peer string) (*store.Store, http.Handler, string) {
	t.Helper()
	directory := filepath.Join(t.TempDir(), "state")
	var output bytes.Buffer
	if err := initialize(t.Context(), directory, "synthetic-administrator-password", &output); err != nil {
		t.Fatal(err)
	}
	credential := "synthetic-administrator-password"
	configuration := config{Listen: "0.0.0.0:8789", Origin: origin, TrustedProxy: &security.ProxyConfig{IPs: []string{peer}}}
	data, err := json.Marshal(configuration)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "config.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	state, auth, configuration, lock, err := openRuntime(t.Context(), directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { state.Close(); lock.Close() })
	handler, err := managementHandler(state, auth, configuration)
	if err != nil {
		t.Fatal(err)
	}
	if !readiness(state, configuration).NonloopbackSafetyConfigured {
		t.Fatal("explicit reverse proxy protection is not reported")
	}
	return state, handler, credential
}

func TestHTTPSProxyToHTTPBackendPreservesSecurityAndProtocols(t *testing.T) {
	front := httptest.NewUnstartedServer(nil)
	origin := "https://" + front.Listener.Addr().String()
	state, handler, credential := proxyFixture(t, origin, "127.0.0.1")
	backend := httptest.NewServer(handler)
	t.Cleanup(backend.Close)
	upstream, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	director := proxy.Director
	proxy.Director = func(request *http.Request) {
		director(request)
		request.Header.Set("X-Forwarded-Proto", "https")
		request.Header.Set("X-Real-IP", "198.51.100.9")
	}
	front.Config.Handler = proxy
	front.StartTLS()
	t.Cleanup(front.Close)
	client := front.Client()
	client.Jar, err = cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path, body, requestOrigin, csrf, token string) (*http.Response, []byte) {
		t.Helper()
		call, err := http.NewRequest(method, origin+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		call.Header.Set("Content-Type", "application/json")
		call.Header.Set("Accept", "application/json, text/event-stream")
		call.Header.Set("Origin", requestOrigin)
		call.Header.Set("X-Exta-CSRF", csrf)
		call.Header.Set("X-Forwarded-Proto", "http")
		call.Header.Set("X-Forwarded-Host", "attacker.example")
		call.Header.Set("X-Forwarded-For", "127.0.0.1")
		call.Header.Set("X-Real-IP", "127.0.0.1")
		if token != "" {
			call.Header.Set("Authorization", "Bearer "+token)
		}
		response, err := client.Do(call)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		return response, data
	}
	login := store.JSON(map[string]string{"credential": credential})
	for _, requestOrigin := range []string{"", "https://attacker.example"} {
		response, _ := request("POST", "/v1/admin/session", login, requestOrigin, "", "")
		if response.StatusCode != 403 {
			t.Fatal("proxy bypassed exact login Origin")
		}
	}
	response, data := request("POST", "/v1/admin/session", login, origin, "", "")
	if response.StatusCode != 200 {
		t.Fatalf("proxied HTTPS login failed: %d", response.StatusCode)
	}
	cookies := response.Cookies()
	if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode || cookies[0].Path != "/v1/admin" {
		t.Fatal("internal HTTP weakened external session cookie")
	}
	var session domain.Session
	if err := json.Unmarshal(data, &session); err != nil {
		t.Fatal(err)
	}
	response, data = request("GET", "/v1/admin/connection-info", "", "", "", "")
	var connection domain.ConnectionInfo
	if err := json.Unmarshal(data, &connection); err != nil || response.StatusCode != 200 || connection.McpUrl != origin+"/mcp" || connection.RestBaseUrl != origin+"/v1" {
		t.Fatal("proxy headers changed configured external URLs or session was not sent")
	}
	response, _ = request("DELETE", "/v1/admin/session", "", origin, "", "")
	if response.StatusCode != 403 {
		t.Fatal("proxy bypassed CSRF")
	}
	response, _ = request("GET", "/v1/capabilities", "", "", "", "")
	if response.StatusCode != 401 {
		t.Fatal("proxy granted anonymous API access")
	}
	token, err := state.MintToken(t.Context(), session.PrincipalId, domain.TokenCreate{Label: "Synthetic proxy test", Scopes: []domain.ClientScope{"read"}, ExpiresAt: store.Stamp(time.Now().Add(time.Hour))})
	if err != nil {
		t.Fatal(err)
	}
	response, _ = request("GET", "/v1/capabilities", "", "", "", token.TokenValue)
	if response.StatusCode != 200 {
		t.Fatal("REST bearer operation failed through HTTP backend")
	}
	response, _ = request("POST", "/mcp", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"proxy-test","version":"test"}}}`, "", "", token.TokenValue)
	if response.StatusCode != 200 {
		t.Fatalf("MCP initialize failed through HTTP backend: %d", response.StatusCode)
	}
	response, _ = request("DELETE", "/v1/admin/session", "", origin, session.CsrfToken, "")
	if response.StatusCode != 200 || len(response.Cookies()) != 1 || !response.Cookies()[0].Secure || response.Cookies()[0].MaxAge != -1 {
		t.Fatal("secure logout failed")
	}
}

func TestProxyLoginLimitsUseVerifiedClientWithoutForwardingBypass(t *testing.T) {
	_, handler, credential := proxyFixture(t, "https://exta.example", "172.30.0.2")
	login := func(peer, client, forwarded string) int {
		request := httptest.NewRequest("POST", "http://exta.example/v1/admin/session", strings.NewReader(store.JSON(map[string]string{"credential": credential})))
		request.RemoteAddr = peer
		request.Header.Set("Origin", "https://exta.example")
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Forwarded-Proto", "https")
		request.Header.Set("X-Real-IP", client)
		request.Header.Set("X-Forwarded-For", forwarded)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response.Code
	}
	if login("172.30.0.3:1234", "198.51.100.1", "172.30.0.2") != 403 {
		t.Fatal("untrusted direct caller reached login")
	}
	for attempt := 0; attempt < 10; attempt++ {
		if login("172.30.0.2:1234", "198.51.100.1", "127.0.0.1") != 200 {
			t.Fatal("login allowance changed")
		}
	}
	if login("172.30.0.2:1234", "198.51.100.1", "198.51.100.99") != 429 {
		t.Fatal("spoofed forwarding chain bypassed client limit")
	}
	if login("172.30.0.2:1234", "198.51.100.2", "198.51.100.1") != 200 {
		t.Fatal("different clients incorrectly share the proxy IP limit")
	}
	if login("172.30.0.2:1234", "::ffff:198.51.100.1", "198.51.100.99") != 429 {
		t.Fatal("mapped IP spelling bypassed rate limit")
	}
}
