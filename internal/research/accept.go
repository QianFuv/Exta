package research

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math/big"
	"sort"
	"strings"
	"time"

	"exta/internal/domain"
	"exta/internal/providers"
	"exta/internal/store"
)

func canonical(value any, output *strings.Builder) error {
	switch item := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(item))
		for key := range item {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		output.WriteString("{")
		for _, key := range keys {
			output.WriteString(store.JSON(key))
			output.WriteByte(':')
			if err := canonical(item[key], output); err != nil {
				return err
			}
			output.WriteByte(',')
		}
		output.WriteString("}")
	case []any:
		output.WriteString("[")
		for _, element := range item {
			if err := canonical(element, output); err != nil {
				return err
			}
			output.WriteByte(',')
		}
		output.WriteString("]")
	case json.Number:
		number, ok := new(big.Rat).SetString(string(item))
		if !ok {
			return store.Reject("invalid_request", 400, "Invalid numeric intent")
		}
		output.WriteByte('n')
		output.WriteString(number.RatString())
		output.WriteByte(';')
	default:
		output.WriteString(store.JSON(item))
	}
	return nil
}

func resolve(raw []byte, replay domain.ResearchReplayContext) (domain.ResearchCreate, string, error) {
	var input domain.ResearchCreate
	if json.Unmarshal(raw, &input) != nil {
		return input, "", store.Reject("invalid_json", 400, "Invalid research request")
	}
	if input.Profile == nil {
		input.Profile = pointer(replay.Defaults.Profile)
	}
	if input.ObservationSeconds == nil {
		input.ObservationSeconds = pointer(replay.Defaults.ObservationSeconds)
	}
	encoded := []byte(store.JSON(input))
	if domain.ValidateJSON("ResearchCreate", encoded) != nil {
		return input, "", store.Reject("budget_guarantee_unavailable", 422, "Resolved profile and budget are incompatible")
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return input, "", err
	}
	var output strings.Builder
	if err := canonical(value, &output); err != nil {
		return input, "", err
	}
	return input, store.Digest([]byte(output.String())), nil
}

func validateProviderSchema(input domain.ResearchCreate) error {
	if input.OutputSchema == nil {
		return nil
	}
	var schema map[string]any
	if json.Unmarshal(*input.OutputSchema, &schema) != nil || schema["type"] != "object" {
		return store.Reject("unsupported_parameter", 422, "Research structured output requires an object root")
	}
	if *input.Profile == "exa_auto" {
		return nil
	}
	var check func(map[string]any) bool
	check = func(node map[string]any) bool {
		for key := range node {
			if key != "type" && key != "description" && key != "properties" && key != "required" && key != "items" {
				return false
			}
		}
		kind, ok := node["type"].(string)
		if !ok {
			return false
		}
		switch kind {
		case "object", "array", "string", "number", "integer":
		default:
			return false
		}
		if properties, ok := node["properties"].(map[string]any); ok {
			for _, value := range properties {
				child, ok := value.(map[string]any)
				if !ok || !check(child) {
					return false
				}
				if description, ok := child["description"].(string); !ok || description == "" {
					return false
				}
			}
		}
		if value, exists := node["items"]; exists {
			child, ok := value.(map[string]any)
			if !ok || !check(child) {
				return false
			}
		}
		return true
	}
	if _, ok := schema["properties"].(map[string]any); !ok || !check(schema) {
		return store.Reject("unsupported_parameter", 422, "Tavily schema requires supported typed, described properties")
	}
	return nil
}

// Accept performs binding-first replay and durable acceptance in one SQLite transaction.
func (engine *Engine) Accept(ctx context.Context, owner, key string, raw []byte) (domain.Job, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var accepted record
	if domain.ValidateJSON("ResearchCreate", raw) != nil || domain.ValidateJSON("IdempotencyKey", []byte(store.JSON(key))) != nil {
		return accepted.Job, store.Reject("invalid_request", 400, "Invalid research request or idempotency key")
	}
	err := engine.Store.Write(ctx, func(tx *sql.Tx) error {
		var savedDigest, contextJSON, jobID string
		err := tx.QueryRowContext(ctx, "SELECT intent_digest,replay_context,job_id FROM idempotency_records WHERE principal_id=? AND operation='research_create' AND key_digest=?", owner, store.Digest([]byte(key))).Scan(&savedDigest, &contextJSON, &jobID)
		if err == nil {
			if domain.ValidateJSON("ResearchReplayContext", []byte(contextJSON)) != nil {
				return store.Reject("storage_unavailable", 503, "Original replay context is unavailable")
			}
			var replay domain.ResearchReplayContext
			_ = json.Unmarshal([]byte(contextJSON), &replay)
			_, digest, err := resolve(raw, replay)
			if err != nil {
				return err
			}
			if digest != savedDigest {
				return store.Reject("idempotency_conflict", 409, "Idempotency key is bound to a different research intent")
			}
			accepted, err = readTx(ctx, tx, jobID, owner)
			return err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if engine.Store.Vault == nil {
			return store.Reject("secret_store_locked", 503, "Secret store is locked")
		}
		settings, err := store.SettingsTx(ctx, tx)
		if err != nil {
			return err
		}
		var routeJSON string
		if err = tx.QueryRowContext(ctx, "SELECT data FROM route_policies WHERE capability='research_create' ORDER BY revision DESC LIMIT 1").Scan(&routeJSON); err != nil {
			return err
		}
		route, err := store.Decode[domain.RoutePolicy](routeJSON)
		if err != nil {
			return err
		}
		if !route.Enabled || len(route.Targets) == 0 {
			return store.Reject("provider_unavailable", 503, "Research route is disabled")
		}
		replay := domain.ResearchReplayContext{CanonicalizationVersion: 1, SchemaVersion: 1, SemanticConfigVersion: settings.Version, Defaults: domain.ResearchReplayDefaults{Profile: string(route.Targets[0].Profile), ObservationSeconds: settings.Limits.ObservationSeconds}}
		input, digest, err := resolve(raw, replay)
		if err != nil {
			return err
		}
		if *input.ObservationSeconds > settings.Limits.ObservationSeconds {
			return store.Reject("invalid_request", 400, "Observation exceeds the configured limit")
		}
		if err = validateProviderSchema(input); err != nil {
			return err
		}
		if input.SupersedesJobId != nil {
			if _, err = readTx(ctx, tx, *input.SupersedesJobId, owner); err != nil {
				return err
			}
		}
		var budget domain.HardBudget
		_ = json.Unmarshal(input.Budget, &budget)
		id := store.ID("job")
		var hold store.Reservation
		var selected domain.Provider
		for _, target := range route.Targets {
			if string(target.Profile) != *input.Profile || engine.Clients[target.Provider] == nil {
				continue
			}
			if _, err = providers.ResearchPayload(target.Provider, input, id, digest); err != nil {
				continue
			}
			hold, err = engine.Store.ReserveTx(ctx, tx, store.Admission{ParentId: id, PrincipalId: owner, Provider: string(target.Provider), Operation: "research_create", Amount: int64(budget.Amount), Research: true}, settings.Limits)
			if err != nil {
				return err
			}
			selected = target.Provider
			break
		}
		if selected == "" {
			return store.Reject("unsupported_parameter", 422, "No research route preserves the requested profile and budget")
		}
		now := engine.Store.Now()
		stamp := store.Stamp(now)
		job := domain.Job{Id: id, OwnerPrincipalId: owner, CreatedAt: stamp, UpdatedAt: stamp, State: "queued", SourceProvider: selected, Profile: *input.Profile, RouteRevision: route.Revision, Budget: input.Budget, Reservation: domain.UsageAmount{Unit: domain.UsageUnit(hold.Unit), Amount: pointer(domain.Integer(hold.Amount)), Certainty: "estimated", Source: "local_reservation", ObservedAt: stamp, PricingRevision: pointer("2026-10-22")}, ActiveSlotHeld: true, BudgetHoldState: "held", CapacityReleaseBasis: "none", ObservationDeadline: store.Stamp(now.Add(time.Duration(*input.ObservationSeconds) * time.Second)), CancelCapability: domain.CancelCapability{Observation: true, Upstream: selected == "exa"}, Version: 1}
		if selected == "tavily" {
			job.CancelCapability.UnavailableReason = pointer("Provider does not offer upstream cancellation")
		}
		if err = domain.ValidateJSON("Job", []byte(store.JSON(job))); err != nil {
			return err
		}
		snapshot := domain.ResearchExecutionSnapshot{AccountId: hold.AccountId, CredentialVersionId: hold.CredentialVersionId, PricingRevision: "2026-10-22", ReservedAmount: domain.Integer(hold.Amount), ReservedUnit: domain.UsageUnit(hold.Unit), RoutePolicy: route, SourceProvider: selected}
		cipher, err := engine.Store.Vault.Seal(id+"/input", []byte(store.JSON(input)))
		if err != nil {
			return err
		}
		accepted = record{Job: job, ReservationID: hold.Id, IntentDigest: digest, MaxPolls: int(settings.Limits.MaxPolls), Credential: hold.CredentialVersionId, InputCipher: cipher, Snapshot: snapshot}
		_, err = tx.ExecContext(ctx, "INSERT INTO research_jobs(id,principal_id,provider,account_id,key_id,credential_version_id,state,data,input_cipher,execution_snapshot,version,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)", id, owner, selected, hold.AccountId, hold.KeyId, hold.CredentialVersionId, job.State, store.JSON(accepted), cipher, store.JSON(snapshot), job.Version, stamp, stamp)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO idempotency_records VALUES(?,'research_create',?,?,?,?,NULL)", owner, store.Digest([]byte(key)), digest, store.JSON(replay), id)
		if err != nil {
			return err
		}
		if err = store.AdvanceActivityTx(ctx, tx); err != nil {
			return err
		}
		return engine.eventTx(ctx, tx, job, "", "accepted_locally", owner)
	})
	if err != nil {
		return domain.Job{}, err
	}
	return engine.expose(accepted, true)
}
