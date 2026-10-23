package store

import (
	"context"
	"database/sql"
	"errors"
	"unicode/utf8"

	"exta/internal/domain"
)

func validateQuota(provider domain.Provider, quota domain.QuotaPolicy) error {
	if err := domain.ValidateJSON("QuotaPolicy", []byte(JSON(quota))); err != nil {
		return Reject("invalid_request", 400, "Invalid key safety limits")
	}
	if (provider == "exa" && quota.Unit != "micro_usd") || (provider == "tavily" && quota.Unit != "tavily_credit") {
		return Reject("invalid_request", 400, "Quota unit must match provider")
	}
	return nil
}

// Keys never exposes encrypted or clear secret values.
func (state *Store) Keys(ctx context.Context) ([]domain.UpstreamKey, error) {
	rows, err := state.DB.QueryContext(ctx, `SELECT k.data,g.validity,max(g.cooldown_ms,s.cooldown_ms),max(g.breaker_ms,s.breaker_ms),max(g.blocked,s.blocked),(SELECT count(*) FROM credential_versions c WHERE c.key_id=k.id AND (c.id<>k.active_version OR json_extract(k.data,'$.retirement_state')<>'active') AND EXISTS(SELECT 1 FROM budget_reservations r WHERE r.credential_version_id=c.id AND (r.state='held' OR r.active_slot=1))) FROM provider_keys k JOIN runtime_gates g ON g.scope='key' AND g.entity_id=k.id JOIN runtime_gates s ON s.scope='association' AND s.entity_id=k.association_ref ORDER BY k.created_at DESC,k.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.UpstreamKey{}
	for rows.Next() {
		var data, validity string
		var cooldown, breaker int64
		var blocked bool
		var retained int64
		if err = rows.Scan(&data, &validity, &cooldown, &breaker, &blocked, &retained); err != nil {
			return nil, err
		}
		item, err := Decode[domain.UpstreamKey](data)
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

func keyTx(ctx context.Context, tx *sql.Tx, id string) (domain.UpstreamKey, string, error) {
	var data, version string
	err := tx.QueryRowContext(ctx, "SELECT data,active_version FROM provider_keys WHERE id=?", id).Scan(&data, &version)
	if err != nil {
		return domain.UpstreamKey{}, "", Missing(err)
	}
	item, err := Decode[domain.UpstreamKey](data)
	return item, version, err
}

// AddKey encrypts a new credential atomically with its masked metadata.
func (state *Store) AddKey(ctx context.Context, actor string, input domain.UpstreamKeyCreate) (domain.UpstreamKey, error) {
	var item domain.UpstreamKey
	if err := domain.ValidateJSON("UpstreamKeyCreate", []byte(JSON(input))); err != nil {
		return item, Reject("invalid_request", 400, "Invalid upstream key")
	}
	err := state.Write(ctx, func(tx *sql.Tx) error {
		quota := normalizeQuota(input.Provider, input.QuotaPolicy)
		if err := validateQuota(input.Provider, quota); err != nil {
			return err
		}
		fingerprint := Digest([]byte(input.Secret))
		var count int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM provider_keys WHERE fingerprint=?", fingerprint).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			return duplicateCredential()
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
		keyId := ID("key")
		item = domain.UpstreamKey{Id: keyId, AssociationRef: keyId, AssociationEvidence: "unknown", QuotaPolicy: quota, Provider: input.Provider, Label: input.Label, Enabled: input.Enabled, Fingerprint: fingerprint, LastFour: last, Validity: "unknown", BillingState: "unknown", Breaker: "closed", RetirementState: "active", Version: 1, CreatedAt: Stamp(state.Now())}
		if _, err = tx.ExecContext(ctx, "INSERT INTO provider_keys VALUES(?,?,?,?,?,?,?,?)", item.Id, item.Provider, item.AssociationRef, fingerprint, credential, JSON(item), item.Version, item.CreatedAt); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO credential_versions(id,key_id,ciphertext,created_at) VALUES(?,?,?,?)", credential, item.Id, encrypted, item.CreatedAt); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO runtime_gates(scope,entity_id) VALUES('key',?),('association',?)", item.Id, item.AssociationRef); err != nil {
			return err
		}
		return AuditTx(ctx, tx, actor, "key.create", item.Id, map[string]any{"provider": item.Provider}, state.Now())
	})
	return item, err
}

// PatchKey applies a versioned key configuration without changing shared constraints.
func (state *Store) PatchKey(ctx context.Context, actor, id string, input domain.UpstreamKeyPatch) (domain.UpstreamKey, error) {
	return state.changeKey(ctx, actor, id, input.ExpectedVersion, input.Label, input.Enabled, nil, false, input.QuotaPolicy)
}

// ChangeKey rotates or retires a key while preserving its accounting identity.
func (state *Store) ChangeKey(ctx context.Context, actor, id string, version domain.Integer, label *string, enabled *bool, secret *string, retire bool) (domain.UpstreamKey, error) {
	return state.changeKey(ctx, actor, id, version, label, enabled, secret, retire, nil)
}

// ChangeKey applies one versioned metadata, rotation or retirement mutation.
func (state *Store) changeKey(ctx context.Context, actor, id string, version domain.Integer, label *string, enabled *bool, secret *string, retire bool, policy *domain.QuotaPolicyInput) (domain.UpstreamKey, error) {
	var item domain.UpstreamKey
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
		if policy != nil {
			quota := normalizeQuota(item.Provider, policy)
			if err = validateQuota(item.Provider, quota); err != nil {
				return err
			}
			used, held, err := KeyBalanceTx(ctx, tx, item.Id)
			if err != nil {
				return err
			}
			if quota.Allocation > 0 && int64(quota.Allocation) < used+held {
				return Reject("invalid_request", 400, "Key limit cannot erase existing charges or unsettled holds")
			}
			item.QuotaPolicy = quota
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
				return duplicateCredential()
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

// duplicateCredential identifies a rejected secret without exposing its value or hash.
func duplicateCredential() *Fault {
	fault := Reject("invalid_request", 400, "Credential already exists")
	fault.Details["reason"] = "duplicate_credential"
	return fault
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

// normalizeQuota resolves missing local controls without inventing upstream funds.
func normalizeQuota(provider domain.Provider, input *domain.QuotaPolicyInput) domain.QuotaPolicy {
	policy := domain.QuotaPolicy{Unit: "micro_usd"}
	if provider == "tavily" {
		policy.Unit = "tavily_credit"
	}
	if input == nil {
		return policy
	}
	if input.Unit != nil {
		policy.Unit = *input.Unit
	}
	if input.Allocation != nil {
		policy.Allocation = *input.Allocation
	}
	if input.PerOperationReservation != nil {
		policy.PerOperationReservation = *input.PerOperationReservation
	}
	if input.ActiveJobs != nil {
		policy.ActiveJobs = *input.ActiveJobs
	}
	return policy
}
