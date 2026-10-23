package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"exta/internal/domain"
)

// Identity separates stable ownership from transport-specific authentication.
type Identity struct {
	PrincipalId string
	Permissions []domain.AdminPermission
	Scopes      []domain.ClientScope
}

// AdminPermissions is the closed permission set from the frozen contract.
var AdminPermissions = []domain.AdminPermission{"admin:read", "admin:keys:write", "admin:routes:write", "admin:usage:refresh", "admin:research:create", "admin:research:cancel", "admin:research:observe", "admin:research:reconcile", "admin:tokens:write", "admin:settings:write", "admin:audit:read"}

// RandomToken creates an opaque local authentication value for explicit flows.
func RandomToken() string {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(value)
}

// BootstrapAdmin creates the single administrator from an explicit persistent password.
func (state *Store) BootstrapAdmin(ctx context.Context, credential string) (string, error) {
	if !ValidPassword(credential) {
		return "", Reject("invalid_request", 400, "Password must contain 16 to 512 characters on one line")
	}
	verifier, err := hashPassword(credential)
	if err != nil {
		return "", err
	}
	principal := ID("principal")
	err = state.Write(ctx, func(tx *sql.Tx) error {
		var count int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM principals").Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			return Reject("revision_conflict", 409, "Administrator already initialized")
		}
		_, err := tx.ExecContext(ctx, "INSERT INTO principals VALUES(?,?,?,?)", principal, verifier, JSON(AdminPermissions), Stamp(state.Now()))
		return err
	})
	return principal, err
}

// Login rate-limits credential checks and issues a new session without changing ownership.
func (state *Store) Login(ctx context.Context, credential, address string) (domain.Session, string, error) {
	var result domain.Session
	var principal, verifier string
	var attempts int
	err := state.Write(ctx, func(tx *sql.Tx) error {
		window := state.Now().UnixMilli() / 60000 * 60000
		if _, err := tx.ExecContext(ctx, `INSERT INTO login_limits VALUES(?,?,1) ON CONFLICT(address) DO UPDATE SET attempts=CASE WHEN window_ms=excluded.window_ms THEN attempts+1 ELSE 1 END,window_ms=excluded.window_ms`, Digest([]byte(address)), window); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, "SELECT attempts FROM login_limits WHERE address=?", Digest([]byte(address))).Scan(&attempts); err != nil {
			return err
		}
		if attempts > 10 {
			return nil
		}
		var count int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM principals").Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return nil
		}
		return tx.QueryRowContext(ctx, "SELECT id,credential_hash FROM principals").Scan(&principal, &verifier)
	})
	if err != nil {
		return result, "", err
	}
	if attempts > 10 {
		fault := Reject("local_rate_limited", 429, "Too many login attempts")
		fault.RetryAfterMs = 60000
		return result, "", fault
	}
	if principal == "" || !verifyPassword(credential, verifier) {
		return result, "", Reject("invalid_token", 401, "Invalid administrator password")
	}
	token := RandomToken()
	err = state.Write(ctx, func(tx *sql.Tx) error {
		var permissions string
		if err := tx.QueryRowContext(ctx, "SELECT permissions FROM principals WHERE id=? AND credential_hash=?", principal, verifier).Scan(&permissions); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return Reject("invalid_token", 401, "Administrator password changed")
			}
			return err
		}
		if err := json.Unmarshal([]byte(permissions), &result.Permissions); err != nil {
			return err
		}
		result.PrincipalId = principal
		result.CsrfToken = RandomToken()
		result.ExpiresAt = Stamp(state.Now().Add(8 * time.Hour))
		_, err := tx.ExecContext(ctx, "INSERT INTO admin_sessions VALUES(?,?,?,?)", Digest([]byte(token)), principal, result.CsrfToken, result.ExpiresAt)
		return err
	})
	if err != nil {
		return domain.Session{}, "", err
	}
	return result, token, nil
}

// Session uses current permissions and a stored stable principal on every request.
func (state *Store) Session(ctx context.Context, token string) (domain.Session, error) {
	var result domain.Session
	var permissions string
	err := state.DB.QueryRowContext(ctx, `SELECT s.principal_id,s.csrf,s.expires_at,p.permissions FROM admin_sessions s JOIN principals p ON p.id=s.principal_id WHERE s.token_hash=? AND s.expires_at>?`, Digest([]byte(token)), Stamp(state.Now())).Scan(&result.PrincipalId, &result.CsrfToken, &result.ExpiresAt, &permissions)
	if errors.Is(err, sql.ErrNoRows) {
		return result, Reject("authentication_required", 401, "Administrator session is missing or expired")
	}
	if err != nil {
		return result, err
	}
	err = json.Unmarshal([]byte(permissions), &result.Permissions)
	return result, err
}

// Logout invalidates only the current session.
func (state *Store) Logout(ctx context.Context, token string) error {
	_, err := state.DB.ExecContext(ctx, "DELETE FROM admin_sessions WHERE token_hash=?", Digest([]byte(token)))
	return err
}

// MintToken reveals a new scoped token once, retaining only its hash.
func (state *Store) MintToken(ctx context.Context, actor string, input domain.TokenCreate) (domain.TokenCreated, error) {
	expires, err := time.Parse(time.RFC3339Nano, input.ExpiresAt)
	if err != nil || !expires.After(state.Now()) {
		return domain.TokenCreated{}, Reject("invalid_request", 400, "Token expiry must be in the future")
	}
	value := "exta_" + RandomToken()
	item := domain.ClientToken{Id: ID("token"), PrincipalId: actor, Label: input.Label, Scopes: input.Scopes, CreatedAt: Stamp(state.Now()), ExpiresAt: Stamp(expires), Version: 1}
	err = state.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "INSERT INTO access_tokens VALUES(?,?,?,?,?,0)", item.Id, actor, Digest([]byte(value)), JSON(item), item.ExpiresAt); err != nil {
			return err
		}
		return AuditTx(ctx, tx, actor, "token.create", item.Id, map[string]any{"scopes": item.Scopes}, state.Now())
	})
	return domain.TokenCreated{Token: item, TokenValue: value}, err
}

// AuthenticateToken rejects revoked/expired tokens and never grants admin permissions.
func (state *Store) AuthenticateToken(ctx context.Context, token string) (Identity, error) {
	var data string
	err := state.DB.QueryRowContext(ctx, "SELECT data FROM access_tokens WHERE token_hash=? AND revoked=0 AND expires_at>?", Digest([]byte(token)), Stamp(state.Now())).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return Identity{}, Reject("invalid_token", 401, "Invalid or expired API token")
	}
	if err != nil {
		return Identity{}, err
	}
	item, err := Decode[domain.ClientToken](data)
	return Identity{PrincipalId: item.PrincipalId, Scopes: item.Scopes}, err
}

// Tokens lists masked token metadata owned by the current administrator.
func (state *Store) Tokens(ctx context.Context, owner string) ([]domain.ClientToken, error) {
	rows, err := state.DB.QueryContext(ctx, "SELECT data FROM access_tokens WHERE principal_id=? ORDER BY json_extract(data,'$.created_at') DESC,id DESC", owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.ClientToken{}
	for rows.Next() {
		var data string
		if err = rows.Scan(&data); err != nil {
			return nil, err
		}
		item, err := Decode[domain.ClientToken](data)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// RevokeToken is a versioned owner-filtered mutation.
func (state *Store) RevokeToken(ctx context.Context, owner, id string, version domain.Integer) (domain.ClientToken, error) {
	var item domain.ClientToken
	err := state.Write(ctx, func(tx *sql.Tx) error {
		var data string
		if err := tx.QueryRowContext(ctx, "SELECT data FROM access_tokens WHERE id=? AND principal_id=?", id, owner).Scan(&data); err != nil {
			return Missing(err)
		}
		var err error
		item, err = Decode[domain.ClientToken](data)
		if err != nil {
			return err
		}
		if item.Version != version {
			return Reject("revision_conflict", 409, "Token changed; reload before revoking")
		}
		stamp := Stamp(state.Now())
		item.RevokedAt = &stamp
		item.Version++
		if _, err = tx.ExecContext(ctx, "UPDATE access_tokens SET revoked=1,data=? WHERE id=?", JSON(item), id); err != nil {
			return err
		}
		return AuditTx(ctx, tx, owner, "token.revoke", id, map[string]any{"version": item.Version}, state.Now())
	})
	return item, err
}
