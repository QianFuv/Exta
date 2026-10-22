package security

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"exta/internal/domain"
	"exta/internal/store"
)

func testAuth(t *testing.T) (*Auth, string, domain.Session, string) {
	t.Helper()
	vault, err := NewVault(bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatal(err)
	}
	state, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "state.sqlite"), vault)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { state.Close() })
	principal, err := state.BootstrapAdmin(t.Context(), "synthetic-bootstrap-credential-only")
	if err != nil {
		t.Fatal(err)
	}
	session, token, err := state.Login(t.Context(), "synthetic-bootstrap-credential-only", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	auth, err := NewAuth(state, "http://127.0.0.1:8789", nil)
	if err != nil {
		t.Fatal(err)
	}
	return auth, principal, session, token
}

func TestAdminMutationAuthorizationBeforeEffects(t *testing.T) {
	auth, principal, session, token := testAuth(t)
	effects := 0
	handler := auth.Guard(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		identity, _, err := auth.Admin(request, "admin:research:create")
		if err != nil {
			WriteError(writer, request, err)
			return
		}
		if identity.PrincipalId != principal {
			t.Error("unstable principal")
		}
		effects++
		WriteJSON(writer, 202, map[string]string{"ok": "true"})
	}))
	cases := []struct {
		name, origin, csrf, cookie string
		code                       int
	}{{"missing session", "http://127.0.0.1:8789", session.CsrfToken, "", 401}, {"missing origin", "", session.CsrfToken, token, 403}, {"wrong origin", "https://attacker.example", session.CsrfToken, token, 403}, {"missing csrf", "http://127.0.0.1:8789", "", token, 403}, {"wrong csrf", "http://127.0.0.1:8789", "wrong", token, 403}, {"valid", "http://127.0.0.1:8789", session.CsrfToken, token, 202}}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			request := httptest.NewRequest("POST", "http://127.0.0.1:8789/v1/admin/research", nil)
			request.Header.Set("Origin", item.origin)
			request.Header.Set("X-Exta-CSRF", item.csrf)
			if item.cookie != "" {
				request.AddCookie(&http.Cookie{Name: "exta_session", Value: item.cookie})
			}
			writer := httptest.NewRecorder()
			handler.ServeHTTP(writer, request)
			if writer.Code != item.code {
				t.Fatalf("status=%d body=%s", writer.Code, writer.Body.String())
			}
			if item.code != 202 {
				if err := domain.ValidateJSON("ApiError", writer.Body.Bytes()); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
	if effects != 1 {
		t.Fatalf("denied requests reached use case: %d", effects)
	}
	if _, err := auth.Store.DB.Exec("UPDATE principals SET permissions=? WHERE id=?", store.JSON([]string{"admin:read"}), principal); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("POST", "http://127.0.0.1:8789/v1/admin/research", nil)
	request.AddCookie(&http.Cookie{Name: "exta_session", Value: token})
	request.Header.Set("Origin", "http://127.0.0.1:8789")
	request.Header.Set("X-Exta-CSRF", session.CsrfToken)
	writer := httptest.NewRecorder()
	handler.ServeHTTP(writer, request)
	if writer.Code != 403 || effects != 1 {
		t.Fatal("permission change did not take effect")
	}
}

func TestBearerAndCookieAreSeparateAuthorities(t *testing.T) {
	auth, principal, _, cookie := testAuth(t)
	created, err := auth.Store.MintToken(t.Context(), principal, domain.TokenCreate{Label: "Test read", Scopes: []domain.ClientScope{"read"}, ExpiresAt: store.Stamp(time.Now().Add(time.Hour))})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", "http://127.0.0.1:8789/v1/ready?token="+created.TokenValue, nil)
	request.AddCookie(&http.Cookie{Name: "exta_session", Value: cookie})
	if _, err = auth.Client(request, "read"); err == nil {
		t.Fatal("cookie or query granted client scope")
	}
	request.Header.Set("Authorization", "Bearer "+created.TokenValue)
	if _, err = auth.Client(request, "read"); err != nil {
		t.Fatal(err)
	}
	if _, err = auth.Client(request, "research:create"); err == nil {
		t.Fatal("read token granted create scope")
	}
	request.Header.Del("Cookie")
	if _, _, err = auth.Admin(request, "admin:read"); err == nil {
		t.Fatal("bearer granted admin")
	}
	writer := httptest.NewRecorder()
	auth.SessionCookie(writer, cookie, time.Now().Add(time.Hour))
	saved := writer.Result().Cookies()[0]
	if !saved.HttpOnly || saved.SameSite != http.SameSiteStrictMode || saved.Path != "/v1/admin" {
		t.Fatalf("unsafe cookie: %+v", saved)
	}
}

func TestHostAndOriginExactMatch(t *testing.T) {
	auth, _, _, _ := testAuth(t)
	handler := auth.Guard(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(204) }))
	for _, origin := range []string{"null", "http://127.0.0.1:8789.evil.example", "http://127.0.0.1:8789/", "http://127.0.0.1:5173"} {
		request := httptest.NewRequest("GET", "http://127.0.0.1:8789/v1/live", nil)
		request.Header.Set("Origin", origin)
		writer := httptest.NewRecorder()
		handler.ServeHTTP(writer, request)
		if writer.Code != 403 {
			t.Fatalf("accepted %s", origin)
		}
	}
	request := httptest.NewRequest("GET", "http://attacker.example/v1/live", nil)
	writer := httptest.NewRecorder()
	handler.ServeHTTP(writer, request)
	if writer.Code != 403 {
		t.Fatal("host accepted")
	}
	if _, err := NewAuth(auth.Store, "http://0.0.0.0:8789", nil); err == nil {
		t.Fatal("clear nonloopback origin accepted")
	}
}

func TestBoundedJSONKeepsExactNumbersAndOmissions(t *testing.T) {
	body := `{"input":"test","budget":{"unit":"micro_usd","amount":1e6,"mode":"hard"},"observation_seconds":60.0}`
	request := httptest.NewRequest("POST", "/", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	value, _, err := ReadJSON[domain.ResearchCreate](httptest.NewRecorder(), request, "ResearchCreate", 1024)
	if err != nil {
		t.Fatal(err)
	}
	if value.Profile != nil || value.ObservationSeconds == nil || *value.ObservationSeconds != 60 {
		t.Fatalf("input changed: %+v", value)
	}
	request = httptest.NewRequest("POST", "/", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	_, _, err = ReadJSON[domain.ResearchCreate](httptest.NewRecorder(), request, "ResearchCreate", 5)
	var fault *store.Fault
	if !errors.As(err, &fault) || fault.Status != 413 {
		t.Fatalf("body limit: %v", err)
	}
}

func TestVaultAuthenticatesIdentityAndKey(t *testing.T) {
	vault, err := NewVault(bytes.Repeat([]byte{2}, 32))
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := vault.Seal("version1", []byte("private-content-marker"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = vault.Open("version2", encrypted); err == nil {
		t.Fatal("ciphertext moved between identities")
	}
	wrong, _ := NewVault(bytes.Repeat([]byte{3}, 32))
	if _, err = wrong.Open("version1", encrypted); err == nil {
		t.Fatal("wrong key accepted")
	}
	encrypted[len(encrypted)-1] ^= 1
	if _, err = vault.Open("version1", encrypted); err == nil {
		t.Fatal("tamper accepted")
	}
	locked, _ := NewVault(nil)
	if _, err = locked.Open("version1", nil); err == nil {
		t.Fatal("locked vault read")
	}
}

func TestProcessLockAndProtectedDirectory(t *testing.T) {
	directory := t.TempDir()
	if err := ProtectDirectory(directory); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "exta.lock")
	first, err := LockDirectory(path)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := LockDirectory(path); err == nil {
		second.Close()
		t.Fatal("second writer acquired lock")
	}
	first.Close()
	third, err := LockDirectory(path)
	if err != nil {
		t.Fatal(err)
	}
	third.Close()
}

type fixtureResolver struct {
	addresses []netip.Addr
	calls     int
}

func (resolver *fixtureResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	resolver.calls++
	return resolver.addresses, nil
}

func TestURLPolicyBlocksPrivateAndAmbiguousTargets(t *testing.T) {
	resolver := &fixtureResolver{addresses: []netip.Addr{netip.MustParseAddr("8.8.8.8")}}
	policy := UrlPolicy{Resolver: resolver}
	for _, target := range []string{"file:///etc/passwd", "http://127.0.0.1", "http://127.1", "http://2130706433", "http://0177.0.0.1", "http://0x7f000001", "http://[::1]", "http://[::ffff:8.8.8.8]", "http://169.254.169.254/latest", "http://100.100.100.200", "http://192.168.0.1", "http://10.0.0.1", "http://metadata.google.internal", "http://public.local", "https://user:pass@example.com", "https://example.com:8080", "https://example.com\\@127.0.0.1", "http://[fe80::1%25eth0]", "http://%31%32%37.0.0.1"} {
		if _, err := policy.Validate(t.Context(), target); err == nil {
			t.Errorf("accepted %s", target)
		}
	}
	if resolver.calls != 0 {
		t.Fatalf("invalid targets caused DNS: %d", resolver.calls)
	}
	value, err := policy.Validate(t.Context(), "https://BÜCHER.de:443/path#fragment")
	if err != nil || value != "https://xn--bcher-kva.de/path" {
		t.Fatalf("IDN canonicalization: %s %v", value, err)
	}
	resolver.addresses = append(resolver.addresses, netip.MustParseAddr("10.0.0.1"))
	if _, err = policy.Validate(t.Context(), "https://example.com"); err == nil {
		t.Fatal("mixed public/private DNS accepted")
	}
	resolver.addresses = nil
	if _, err = policy.Validate(t.Context(), "https://example.com"); err == nil {
		t.Fatal("empty DNS accepted")
	}
}

func TestProviderClientHasNoProxyOrRedirectCredentials(t *testing.T) {
	client, err := ProviderClient("https://api.exa.ai")
	if err != nil {
		t.Fatal(err)
	}
	transport := client.Transport.(*http.Transport)
	if transport.Proxy != nil {
		t.Fatal("ambient proxy enabled")
	}
	if err = client.CheckRedirect(&http.Request{}, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatal("provider redirects enabled")
	}
	if _, err = transport.DialContext(t.Context(), "tcp", "127.0.0.1:443"); err == nil {
		t.Fatal("local egress accepted")
	}
	for _, origin := range []string{"http://api.exa.ai", "https://attacker.example", "https://api.exa.ai/", "https://api.exa.ai:443"} {
		if _, err = ProviderClient(origin); err == nil {
			t.Fatalf("provider config accepted %s", origin)
		}
	}
}

func TestErrorRedaction(t *testing.T) {
	writer := httptest.NewRecorder()
	request := httptest.NewRequest("GET", "/", nil)
	WriteError(writer, request, errors.New("secret-marker SQL /private/path"))
	if strings.Contains(writer.Body.String(), "secret-marker") {
		t.Fatal("raw error exposed")
	}
	var value domain.ApiError
	if err := json.Unmarshal(writer.Body.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
}
