package security

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"exta/internal/domain"
	"exta/internal/store"
)

// Auth keeps browser sessions separate from client Bearer scopes.
type Auth struct {
	Store   *store.Store
	origins map[string]bool
	hosts   map[string]bool
	secure  bool
}

// NewAuth permits clear HTTP only for explicitly configured loopback origins.
func NewAuth(state *store.Store, origin string, developmentOrigins []string) (*Auth, error) {
	result := &Auth{Store: state, origins: map[string]bool{}, hosts: map[string]bool{}}
	for index, value := range append([]string{origin}, developmentOrigins...) {
		address, err := url.Parse(value)
		if err != nil || address.User != nil || address.RawQuery != "" || address.Fragment != "" || address.Path != "" || (address.Scheme != "http" && address.Scheme != "https") || address.Host == "" {
			return nil, errors.New("trusted origin must be an exact HTTP(S) origin")
		}
		ip := net.ParseIP(address.Hostname())
		isLoopback := ip != nil && ip.IsLoopback()
		if address.Scheme != "https" && !isLoopback {
			return nil, errors.New("HTTP management requires an explicit loopback address")
		}
		if index > 0 && !isLoopback {
			return nil, errors.New("development origins must be loopback")
		}
		if index == 0 {
			result.secure = address.Scheme == "https"
		}
		result.origins[value] = true
		result.hosts[strings.ToLower(address.Host)] = true
	}
	return result, nil
}

type requestIDKey struct{}

// Guard rejects hostile Host/Origin values and adds local response hardening.
func (auth *Auth) Guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		id := store.ID("request")
		request = request.WithContext(context.WithValue(request.Context(), requestIDKey{}, id))
		writer.Header().Set("X-Request-ID", id)
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		writer.Header().Set("Referrer-Policy", "no-referrer")
		writer.Header().Set("Cache-Control", "no-store")
		if !auth.hosts[strings.ToLower(request.Host)] {
			WriteError(writer, request, store.Reject("origin_rejected", 403, "Request host is not trusted"))
			return
		}
		if origin := request.Header.Get("Origin"); origin != "" && !auth.origins[origin] {
			WriteError(writer, request, store.Reject("origin_rejected", 403, "Request origin is not trusted"))
			return
		}
		next.ServeHTTP(writer, request)
	})
}

// CheckOrigin applies to every admin mutation, including unauthenticated login.
func (auth *Auth) CheckOrigin(request *http.Request) error {
	if !auth.hosts[strings.ToLower(request.Host)] || !auth.origins[request.Header.Get("Origin")] {
		return store.Reject("origin_rejected", 403, "Exact trusted Origin is required")
	}
	return nil
}

// Admin authorizes the current stable principal before any use-case side effect.
func (auth *Auth) Admin(request *http.Request, permission string) (store.Identity, domain.Session, error) {
	cookie, err := request.Cookie("exta_session")
	if err != nil {
		return store.Identity{}, domain.Session{}, store.Reject("authentication_required", 401, "Administrator session is required")
	}
	session, err := auth.Store.Session(request.Context(), cookie.Value)
	if err != nil {
		return store.Identity{}, session, err
	}
	identity := store.Identity{PrincipalId: session.PrincipalId, Permissions: session.Permissions}
	allowed := permission == "authenticated_admin"
	for _, candidate := range session.Permissions {
		if string(candidate) == permission {
			allowed = true
		}
	}
	if !allowed {
		return identity, session, store.Reject("forbidden", 403, "Administrator permission is required")
	}
	if request.Method != "GET" && request.Method != "HEAD" {
		if err = auth.CheckOrigin(request); err != nil {
			return identity, session, err
		}
		if subtle.ConstantTimeCompare([]byte(request.Header.Get("X-Exta-CSRF")), []byte(session.CsrfToken)) != 1 {
			return identity, session, store.Reject("csrf_invalid", 403, "CSRF token is missing or invalid")
		}
	}
	return identity, session, nil
}

// Client accepts only a scoped Bearer token, never a browser cookie or URL token.
func (auth *Auth) Client(request *http.Request, scope string) (store.Identity, error) {
	parts := strings.Fields(request.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return store.Identity{}, store.Reject("authentication_required", 401, "Bearer token is required")
	}
	identity, err := auth.Store.AuthenticateToken(request.Context(), parts[1])
	if err != nil {
		return identity, err
	}
	for _, candidate := range identity.Scopes {
		if string(candidate) == scope {
			return identity, nil
		}
	}
	return identity, store.Reject("forbidden", 403, "API token scope is required")
}

// SessionCookie never exposes the local session through JavaScript or public API paths.
func (auth *Auth) SessionCookie(writer http.ResponseWriter, token string, expires time.Time) {
	http.SetCookie(writer, &http.Cookie{Name: "exta_session", Value: token, Path: "/v1/admin", Expires: expires, HttpOnly: true, Secure: auth.secure, SameSite: http.SameSiteStrictMode})
}

// ClearSession removes the browser cookie with matching security attributes.
func (auth *Auth) ClearSession(writer http.ResponseWriter) {
	http.SetCookie(writer, &http.Cookie{Name: "exta_session", Value: "", Path: "/v1/admin", MaxAge: -1, HttpOnly: true, Secure: auth.secure, SameSite: http.SameSiteStrictMode})
}

// RequestID is the transport's opaque request identifier, not user-controlled input.
func RequestID(request *http.Request) string {
	if id, ok := request.Context().Value(requestIDKey{}).(string); ok {
		return id
	}
	return store.ID("request")
}

// ReadJSON enforces a byte bound and the shared strict schema without applying defaults.
func ReadJSON[T any](writer http.ResponseWriter, request *http.Request, schema string, limit int64) (T, []byte, error) {
	var result T
	if media := strings.Split(request.Header.Get("Content-Type"), ";")[0]; media != "application/json" {
		return result, nil, store.Reject("invalid_request", 400, "Content-Type must be application/json")
	}
	data, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, limit))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return result, nil, store.Reject("request_too_large", 413, "Request body exceeds the configured limit")
		}
		return result, nil, store.Reject("invalid_json", 400, "Request body could not be read")
	}
	if !json.Valid(data) {
		return result, nil, store.Reject("invalid_json", 400, "Request body must contain one valid JSON value")
	}
	if err = domain.ValidateJSON(schema, data); err != nil {
		fault := store.Reject("invalid_request", 400, "Request does not match the operation schema")
		fault.Details["schema"] = schema
		return result, nil, fault
	}
	if err = json.Unmarshal(data, &result); err != nil {
		return result, nil, store.Reject("invalid_request", 400, "Request cannot be decoded")
	}
	return result, data, nil
}

// WriteJSON encodes only caller-selected public DTOs.
func WriteJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

// WriteError removes underlying database, network and credential detail.
func WriteError(writer http.ResponseWriter, request *http.Request, err error) {
	fault := store.Reject("storage_unavailable", 503, "Local service is unavailable")
	var known *store.Fault
	if errors.As(err, &known) {
		fault = known
	} else if errors.Is(err, context.DeadlineExceeded) {
		fault = store.Reject("deadline_exceeded", 504, "Operation deadline exceeded")
	}
	details := fault.Details
	if details == nil {
		details = map[string]any{}
	}
	var retry *domain.Integer
	if fault.RetryAfterMs > 0 {
		value := domain.Integer(fault.RetryAfterMs)
		retry = &value
		writer.Header().Set("Retry-After", strconv.FormatInt((fault.RetryAfterMs+999)/1000, 10))
	}
	WriteJSON(writer, fault.Status, domain.ApiError{Error: domain.ErrorDetail{Code: domain.ErrorCode(fault.Code), Message: fault.Message, RequestId: RequestID(request), Retryable: fault.Status == 429 || fault.Status == 503 || fault.Status == 504, RetryAfterMs: retry, Details: json.RawMessage(store.JSON(details))}})
}
