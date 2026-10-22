package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"unicode/utf8"

	"exta/internal/domain"
)

// Accounts returns redacted account metadata in stable order.
func (state *Store) Accounts(ctx context.Context) ([]domain.Account, error) {
	rows, err := state.DB.QueryContext(ctx, "SELECT a.data,g.blocked FROM provider_accounts a JOIN runtime_gates g ON g.scope='account' AND g.entity_id=a.id ORDER BY a.created_at DESC,a.id DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.Account{}
	for rows.Next() {
		var data string
		var blocked bool
		if err = rows.Scan(&data, &blocked); err != nil {
			return nil, err
		}
		item, err := Decode[domain.Account](data)
		if err != nil {
			return nil, err
		}
		if blocked {
			item.BillingState = "blocked"
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func accountTx(ctx context.Context, tx *sql.Tx, id string) (domain.Account, error) {
	var data string
	if err := tx.QueryRowContext(ctx, "SELECT data FROM provider_accounts WHERE id=?", id).Scan(&data); err != nil {
		return domain.Account{}, Missing(err)
	}
	return Decode[domain.Account](data)
}

func validateQuota(provider domain.Provider, quota domain.QuotaPolicy) error {
	if (provider == "exa" && quota.Unit != "micro_usd") || (provider == "tavily" && quota.Unit != "tavily_credit") {
		return Reject("invalid_request", 400, "Quota unit must match provider")
	}
	return nil
}

// CreateAccount records an operator-declared billing group without contacting upstream.
func (state *Store) CreateAccount(ctx context.Context, actor string, input domain.AccountCreate) (domain.Account, error) {
	if err := validateQuota(input.Provider, input.QuotaPolicy); err != nil {
		return domain.Account{}, err
	}
	item := domain.Account{Id: ID("account"), Provider: input.Provider, Label: input.Label, Enabled: input.Enabled, QuotaPolicy: input.QuotaPolicy, VerificationState: "unverified", BillingState: "unknown", Version: 1, CreatedAt: Stamp(state.Now())}
	if input.VerificationState != nil {
		item.VerificationState = *input.VerificationState
	}
	if input.ExternalRef != nil {
		if err := json.Unmarshal(input.ExternalRef, &item.ExternalRef); err != nil {
			return item, err
		}
	}
	err := state.Write(ctx, func(tx *sql.Tx) error {
		if item.VerificationState == "unverified" {
			var count int
			if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM provider_accounts WHERE provider=? AND json_extract(data,'$.verification_state')='unverified'", item.Provider).Scan(&count); err != nil {
				return err
			}
			if count > 0 {
				return Reject("invalid_request", 400, "Use the existing unverified provider group or explicitly verify a separate account")
			}
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO provider_accounts VALUES(?,?,?,?,?)", item.Id, item.Provider, JSON(item), item.Version, item.CreatedAt); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO runtime_gates(scope,entity_id) VALUES('account',?)", item.Id); err != nil {
			return err
		}
		return AuditTx(ctx, tx, actor, "account.create", item.Id, map[string]any{"provider": item.Provider}, state.Now())
	})
	return item, err
}

// PatchAccount checks the version and prevents spending limits from erasing holds.
func (state *Store) PatchAccount(ctx context.Context, actor, id string, input domain.AccountPatch) (domain.Account, error) {
	var item domain.Account
	err := state.Write(ctx, func(tx *sql.Tx) error {
		var err error
		item, err = accountTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if item.Version != input.ExpectedVersion {
			return Reject("revision_conflict", 409, "Account changed; reload before saving")
		}
		if input.Enabled != nil {
			item.Enabled = *input.Enabled
		}
		if input.Label != nil {
			item.Label = *input.Label
		}
		if input.VerificationState != nil {
			item.VerificationState = *input.VerificationState
			if item.VerificationState == "unverified" {
				var count int
				if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM provider_accounts WHERE provider=? AND id<>? AND json_extract(data,'$.verification_state')='unverified'", item.Provider, id).Scan(&count); err != nil {
					return err
				}
				if count > 0 {
					return Reject("invalid_request", 400, "Only one unverified group is allowed per provider")
				}
			}
		}
		if input.ExternalRef != nil {
			if err = json.Unmarshal(input.ExternalRef, &item.ExternalRef); err != nil {
				return err
			}
		}
		if input.QuotaPolicy != nil {
			if err = validateQuota(item.Provider, *input.QuotaPolicy); err != nil {
				return err
			}
			used, held, err := BalanceTx(ctx, tx, id)
			if err != nil {
				return err
			}
			if int64(input.QuotaPolicy.Allocation) < used+held {
				return Reject("invalid_request", 400, "Allocation cannot be less than charged usage and unsettled holds")
			}
			item.QuotaPolicy = *input.QuotaPolicy
		}
		item.Version++
		if _, err = tx.ExecContext(ctx, "UPDATE provider_accounts SET data=?,version=? WHERE id=?", JSON(item), item.Version, id); err != nil {
			return err
		}
		return AuditTx(ctx, tx, actor, "account.update", id, map[string]any{"version": item.Version}, state.Now())
	})
	return item, err
}

// Keys never exposes encrypted or clear secret values.
func (state *Store) Keys(ctx context.Context) ([]domain.ProviderKey, error) {
	rows, err := state.DB.QueryContext(ctx, `SELECT k.data,g.validity,g.cooldown_ms,g.breaker_ms,g.blocked,(SELECT count(*) FROM credential_versions c WHERE c.key_id=k.id AND (c.id<>k.active_version OR json_extract(k.data,'$.retirement_state')<>'active') AND EXISTS(SELECT 1 FROM budget_reservations r WHERE r.credential_version_id=c.id AND (r.state='held' OR r.active_slot=1))) FROM provider_keys k JOIN runtime_gates g ON g.scope='key' AND g.entity_id=k.id ORDER BY k.created_at DESC,k.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.ProviderKey{}
	for rows.Next() {
		var data, validity string
		var cooldown, breaker int64
		var blocked bool
		var retained int64
		if err = rows.Scan(&data, &validity, &cooldown, &breaker, &blocked, &retained); err != nil {
			return nil, err
		}
		item, err := Decode[domain.ProviderKey](data)
		if err != nil {
			return nil, err
		}
		item.Validity = validity
		item.RetainedCredentialCount = domain.Integer(retained)
		if cooldown > state.Now().UnixMilli() {
			value := Stamp(timeFromMillis(cooldown))
			item.CooldownUntil = &value
		}
		if breaker > state.Now().UnixMilli() {
			item.Breaker = "open"
		} else if breaker > 0 {
			item.Breaker = "half_open"
		}
		if blocked {
			item.BillingState = "blocked"
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func keyTx(ctx context.Context, tx *sql.Tx, id string) (domain.ProviderKey, string, error) {
	var data, version string
	err := tx.QueryRowContext(ctx, "SELECT data,active_version FROM provider_keys WHERE id=?", id).Scan(&data, &version)
	if err != nil {
		return domain.ProviderKey{}, "", Missing(err)
	}
	item, err := Decode[domain.ProviderKey](data)
	return item, version, err
}

// AddKey encrypts a new credential atomically with its masked metadata.
func (state *Store) AddKey(ctx context.Context, actor string, input domain.KeyCreate) (domain.ProviderKey, error) {
	var item domain.ProviderKey
	err := state.Write(ctx, func(tx *sql.Tx) error {
		account, err := accountTx(ctx, tx, input.AccountId)
		if err != nil {
			return err
		}
		fingerprint := Digest([]byte(input.Secret))
		var count int
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM provider_keys WHERE fingerprint=?", fingerprint).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			return Reject("invalid_request", 400, "Credential already exists")
		}
		credential := ID("credential")
		if state.Vault == nil {
			return Reject("secret_store_locked", 503, "Secret store is locked")
		}
		encrypted, err := state.Vault.Seal(credential, []byte(input.Secret))
		if err != nil {
			return Reject("secret_store_locked", 503, "Secret store is locked")
		}
		last := lastFour(input.Secret)
		item = domain.ProviderKey{Id: ID("key"), AccountId: account.Id, Provider: account.Provider, Label: input.Label, Enabled: input.Enabled, Fingerprint: fingerprint, LastFour: last, Validity: "unknown", BillingState: "unknown", Breaker: "closed", RetirementState: "active", Version: 1, CreatedAt: Stamp(state.Now())}
		if _, err = tx.ExecContext(ctx, "INSERT INTO provider_keys VALUES(?,?,?,?,?,?,?)", item.Id, item.AccountId, fingerprint, credential, JSON(item), item.Version, item.CreatedAt); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO credential_versions(id,key_id,ciphertext,created_at) VALUES(?,?,?,?)", credential, item.Id, encrypted, item.CreatedAt); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO runtime_gates(scope,entity_id) VALUES('key',?)", item.Id); err != nil {
			return err
		}
		return AuditTx(ctx, tx, actor, "key.create", item.Id, map[string]any{"account_id": item.AccountId}, state.Now())
	})
	return item, err
}

// ChangeKey applies one versioned metadata, rotation or retirement mutation.
func (state *Store) ChangeKey(ctx context.Context, actor, id string, version domain.Integer, label *string, enabled *bool, secret *string, retire bool) (domain.ProviderKey, error) {
	var item domain.ProviderKey
	err := state.Write(ctx, func(tx *sql.Tx) error {
		var active string
		var err error
		item, active, err = keyTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if item.Version != version {
			return Reject("revision_conflict", 409, "Key changed; reload before saving")
		}
		if label != nil {
			item.Label = *label
		}
		if enabled != nil {
			if item.RetirementState != "active" && *enabled {
				return Reject("invalid_request", 400, "Retired credentials cannot be enabled")
			}
			item.Enabled = *enabled
		}
		action := "key.update"
		if secret != nil {
			if utf8.RuneCountInString(*secret) < 8 || utf8.RuneCountInString(*secret) > 4096 {
				return Reject("invalid_request", 400, "Credential is too short")
			}
			if Digest([]byte(*secret)) == item.Fingerprint {
				return Reject("invalid_request", 400, "New credential must differ from the current credential")
			}
			var duplicates int
			if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM provider_keys WHERE fingerprint=? AND id<>?", Digest([]byte(*secret)), id).Scan(&duplicates); err != nil {
				return err
			}
			if duplicates > 0 {
				return Reject("invalid_request", 400, "Credential already exists")
			}
			if item.RetirementState != "active" {
				return Reject("invalid_request", 400, "Retired key cannot rotate")
			}
			active = ID("credential")
			if state.Vault == nil {
				return Reject("secret_store_locked", 503, "Secret store is locked")
			}
			encrypted, err := state.Vault.Seal(active, []byte(*secret))
			if err != nil {
				return Reject("secret_store_locked", 503, "Secret store is locked")
			}
			item.Fingerprint = Digest([]byte(*secret))
			item.LastFour = lastFour(*secret)
			item.Validity = "unknown"
			if _, err = tx.ExecContext(ctx, "INSERT INTO credential_versions(id,key_id,ciphertext,created_at) VALUES(?,?,?,?)", active, id, encrypted, Stamp(state.Now())); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, "UPDATE runtime_gates SET validity='unknown',blocked=0 WHERE scope='key' AND entity_id=?", id); err != nil {
				return err
			}
			action = "key.rotate"
		}
		if retire {
			item.Enabled = false
			item.RetirementState = "retiring"
			action = "key.retire"
		}
		item.Version++
		if _, err = tx.ExecContext(ctx, "UPDATE provider_keys SET fingerprint=?,active_version=?,data=?,version=? WHERE id=?", item.Fingerprint, active, JSON(item), item.Version, id); err != nil {
			return err
		}
		return AuditTx(ctx, tx, actor, action, id, map[string]any{"version": item.Version}, state.Now())
	})
	return item, err
}

func lastFour(secret string) string {
	characters := []rune(secret)
	if len(characters) > 4 {
		characters = characters[len(characters)-4:]
	}
	return string(characters)
}

// Credential retrieves the pinned version even when its key has been retired.
func (state *Store) Credential(ctx context.Context, id string) (string, error) {
	var ciphertext []byte
	if err := state.DB.QueryRowContext(ctx, "SELECT ciphertext FROM credential_versions WHERE id=?", id).Scan(&ciphertext); err != nil {
		return "", Missing(err)
	}
	if state.Vault == nil {
		return "", Reject("secret_store_locked", 503, "Secret store is locked")
	}
	plain, err := state.Vault.Open(id, ciphertext)
	if err != nil {
		return "", Reject("secret_store_locked", 503, "Secret store is locked or unreadable")
	}
	return string(plain), nil
}

// IsConflict recognizes a version conflict without parsing driver error messages.
func IsConflict(err error) bool {
	var fault *Fault
	return errors.As(err, &fault) && fault.Code == "revision_conflict"
}
