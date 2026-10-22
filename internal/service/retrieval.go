package service

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"time"

	"exta/internal/domain"
	"exta/internal/providers"
	"exta/internal/scheduler"
	"exta/internal/security"
	"exta/internal/store"
)

// Service is shared by REST, MCP and administrator operations.
type Service struct {
	Store           *store.Store
	Clients         map[domain.Provider]*providers.Client
	Queue           scheduler.Queue
	URLs            security.UrlPolicy
	Wait            func(context.Context, time.Duration) error
	ResearchEnabled bool
}

// New creates local orchestration; constructing clients never contacts an upstream.
func New(state *store.Store, clients map[domain.Provider]*providers.Client, urls security.UrlPolicy) (*Service, error) {
	if clients == nil {
		clients = map[domain.Provider]*providers.Client{}
		for _, provider := range []domain.Provider{"exa", "tavily"} {
			client, err := providers.New(provider, nil)
			if err != nil {
				return nil, err
			}
			clients[provider] = client
		}
	}
	return &Service{Store: state, Clients: clients, URLs: urls, Wait: waitContext}, nil
}

func waitContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func pointer[T any](value T) *T { return &value }

func publicError(err error, id string) domain.ErrorDetail {
	fault := store.Reject("storage_unavailable", 503, "Local service is unavailable")
	var known *store.Fault
	if errors.As(err, &known) {
		fault = known
	} else if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		fault = store.Reject("deadline_exceeded", 504, "Operation deadline or caller cancellation ended the exchange")
	}
	result := domain.ErrorDetail{Code: domain.ErrorCode(fault.Code), Message: fault.Message, RequestId: id, Details: json.RawMessage(store.JSON(fault.Details)), Retryable: fault.Status == 429 || fault.Status == 503 || fault.Status == 504}
	if fault.RetryAfterMs > 0 {
		result.RetryAfterMs = pointer(domain.Integer(fault.RetryAfterMs))
	}
	return result
}

// Retrieve enforces one total deadline and HTTP-attempt allowance across all targets.
// It only retries failed fetch URLs; uncertain full calls require explicit policy consent.
func (service *Service) Retrieve(ctx context.Context, principal, id, operation string, raw []byte) (response domain.RetrievalResponse, returnedErr error) {
	started := time.Now()
	maximum := time.Minute
	if operation == "search" {
		maximum = 30 * time.Second
	}
	if operation == "map" || operation == "crawl" {
		maximum = 165 * time.Second
	}
	ctx, cancel := context.WithDeadline(ctx, started.Add(maximum))
	defer cancel()
	response = domain.RetrievalResponse{Results: []domain.RetrievalResult{}, Failures: []domain.ItemFailure{}, Meta: domain.RequestMeta{RequestId: id, Operation: domain.Operation(operation), Warnings: []string{}, Usage: []domain.UsageAmount{}}}
	route, err := service.Store.Route(ctx, operation)
	if err != nil {
		return response, err
	}
	if !route.Enabled {
		return response, store.Reject("provider_unavailable", 503, "Capability route is disabled")
	}
	ctx, routeCancel := context.WithDeadline(ctx, started.Add(time.Duration(route.DeadlineSeconds)*time.Second))
	defer routeCancel()
	input, err := Prepare(ctx, service.URLs, operation, raw, false)
	if err != nil {
		return response, err
	}
	targets := []domain.RouteTarget{}
	for _, target := range route.Targets {
		if Compatible(input, target) {
			targets = append(targets, target)
		}
	}
	if len(targets) == 0 {
		return response, parameterError("profile/content_mode/format/urls", "No configured route preserves the requested profile, format and wire limits")
	}
	settings, err := service.Store.Settings(ctx)
	if err != nil {
		return response, err
	}
	if service.Store.Vault == nil {
		return response, store.Reject("secret_store_locked", 503, "Secret store is locked")
	}
	response.Meta.RouteRevision = route.Revision
	record := domain.RequestRecord{Id: id, Operation: domain.Operation(operation), RouteRevision: route.Revision, Status: "running", CreatedAt: store.Stamp(service.Store.Now()), Usage: []domain.UsageAmount{}}
	if err = service.Store.StartRequest(ctx, principal, record); err != nil {
		return response, err
	}
	defer func() {
		response.Meta.DurationMs = domain.Integer(time.Since(started).Milliseconds())
		record.DurationMs = &response.Meta.DurationMs
		record.CompletedAt = pointer(store.Stamp(service.Store.Now()))
		record.Usage = response.Meta.Usage
		record.SourceProvider = response.Meta.SourceProvider
		if returnedErr == nil {
			record.Status = "succeeded"
			record.ResultCount = pointer(domain.Integer(len(response.Results)))
			record.FailureCount = pointer(domain.Integer(len(response.Failures)))
			record.Partial = &response.Partial
		} else {
			record.Status = "failed"
			detail := publicError(returnedErr, id)
			record.Error = &detail
			if record.BillingUncertain && detail.Code == "deadline_exceeded" {
				record.Status = "uncertain"
			}
		}
		persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := service.Store.FinishRequest(persist, record); err != nil {
			returnedErr = err
		}
	}()
	release, err := service.Queue.Acquire(ctx, int(settings.Limits.SynchronousConcurrency), int(settings.Limits.SynchronousWaiting))
	if err != nil {
		return response, err
	}
	defer release()
	originalURLs := append([]string{}, input.Urls...)
	failures := map[string]domain.ItemFailure{}
	seenResults := map[string]bool{}
	cursor := 0
	var lastFailure error
	for attemptNumber := 1; attemptNumber <= int(route.MaxAttempts) && attemptNumber <= 3; attemptNumber++ {
		if ctx.Err() != nil {
			lastFailure = ctx.Err()
			break
		}
		var reservation store.Reservation
		var selected domain.RouteTarget
		var admissionErr error
		isAdmitted := false
		for offset := 0; offset < len(targets); offset++ {
			index := (cursor + offset) % len(targets)
			target := targets[index]
			if service.Clients[target.Provider] == nil {
				continue
			}
			reservation, admissionErr = service.Store.Reserve(ctx, store.Admission{ParentId: id, PrincipalId: principal, Provider: string(target.Provider), Operation: operation, MinimumAmount: providers.ReservationAmount(target.Provider, input)})
			if admissionErr == nil {
				selected = target
				cursor = (index + 1) % len(targets)
				isAdmitted = true
				break
			}
			var fault *store.Fault
			if !errors.As(admissionErr, &fault) {
				return response, admissionErr
			}
		}
		if !isAdmitted {
			if lastFailure == nil {
				lastFailure = admissionErr
			}
			if lastFailure == nil {
				lastFailure = store.Reject("provider_unavailable", 503, "No provider is available")
			}
			break
		}
		credential, err := service.Store.Credential(ctx, reservation.CredentialVersionId)
		if err != nil {
			persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			releaseErr := service.Store.ReleaseUnsent(persist, reservation.Id)
			cancel()
			if releaseErr != nil {
				return response, releaseErr
			}
			return response, err
		}
		attempt := domain.Attempt{Id: store.ID("attempt"), AttemptNumber: domain.Integer(attemptNumber), Provider: selected.Provider, AccountId: reservation.AccountId, CredentialVersionId: reservation.CredentialVersionId, TransmissionState: "possibly_sent", BillingUncertain: true, CreatedAt: store.Stamp(service.Store.Now())}
		if err = service.Store.StartAttempt(ctx, id, attempt); err != nil {
			persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			_ = service.Store.ReleaseUnsent(persist, reservation.Id)
			cancel()
			return response, err
		}
		outcome := service.Clients[selected.Provider].Retrieve(ctx, credential, input, int64(settings.Limits.ResponseBytes))
		response.Meta.AttemptCount = domain.Integer(attemptNumber)
		attempt.TransmissionState = outcome.Transmission
		attempt.BillingUncertain = !outcome.IsUnspent && outcome.Cost == nil
		attempt.DurationMs = domain.Integer(outcome.Duration.Milliseconds())
		attempt.UpstreamRequestId = outcome.RequestId
		if outcome.Status > 0 {
			attempt.HttpStatus = pointer(domain.Integer(outcome.Status))
		}
		if outcome.Failure != nil {
			attempt.RetryReason = pointer(outcome.Failure.Code)
		}
		record.BillingUncertain = record.BillingUncertain || attempt.BillingUncertain
		persist, persistCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		err = service.Store.FinishAttempt(persist, attempt, reservation.Id, outcome.IsUnspent, outcome.Cost, outcome.Certainty)
		if err == nil && outcome.GateKind != "" {
			entity := reservation.AccountId
			if outcome.GateScope == "key" {
				entity = reservation.KeyId
			}
			err = service.Store.GateFailure(persist, outcome.GateScope, entity, reservation.CredentialVersionId, outcome.GateKind, outcome.Cooldown)
		}
		if err == nil && outcome.SharedCooldown {
			err = service.Store.ProviderCooldown(persist, string(selected.Provider), outcome.Cooldown)
		}
		if err == nil && outcome.Failure == nil {
			err = service.Store.GateSuccess(persist, reservation)
		}
		persistCancel()
		if err != nil {
			return response, err
		}
		usage := domain.UsageAmount{Unit: domain.UsageUnit(reservation.Unit), Certainty: domain.UsageCertainty(outcome.Certainty), Source: string(selected.Provider) + "/attempt", ObservedAt: store.Stamp(service.Store.Now())}
		if outcome.Cost != nil {
			usage.Amount = pointer(domain.Integer(*outcome.Cost))
		}
		if outcome.IsUnspent {
			usage.Amount = pointer(domain.Integer(0))
			usage.Certainty = "reported"
			usage.Source = "proven_non_submission"
		}
		response.Meta.Usage = append(response.Meta.Usage, usage)
		if outcome.Failure != nil {
			lastFailure = outcome.Failure
			if operation == "fetch" {
				for _, link := range input.Urls {
					failures[link] = domain.ItemFailure{Url: link, Code: outcome.Failure.Code, Message: outcome.Failure.Message, Provider: selected.Provider, Retryable: outcome.CanRetry && !outcome.RetryProhibited}
				}
			}
			if outcome.RetryProhibited || !outcome.CanRetry && !(route.AllowDuplicateCost && !outcome.IsUnspent) {
				break
			}
			if attemptNumber == int(route.MaxAttempts) || attemptNumber == 3 {
				break
			}
			if outcome.Cooldown == 0 {
				delay := time.Duration(250*(1<<(attemptNumber-1)))*time.Millisecond + time.Duration(rand.Int64N(126))*time.Millisecond
				if err = service.Wait(ctx, min(delay, 5*time.Second)); err != nil {
					lastFailure = err
					break
				}
			}
			continue
		}
		lastFailure = nil
		response.Meta.SourceProvider = pointer(selected.Provider)
		response.ProviderOutput = outcome.Output
		response.Meta.Warnings = append(response.Meta.Warnings, outcome.Warnings...)
		for _, result := range outcome.Results {
			if operation != "fetch" || !seenResults[result.Url] {
				response.Results = append(response.Results, result)
				seenResults[result.Url] = true
			}
			delete(failures, result.Url)
		}
		pending := []string{}
		for _, link := range input.Urls {
			delete(failures, link)
		}
		for _, failure := range outcome.Failures {
			failures[failure.Url] = failure
			if failure.Retryable {
				pending = append(pending, failure.Url)
			}
		}
		if operation != "fetch" || len(pending) == 0 || attemptNumber == int(route.MaxAttempts) || attemptNumber == 3 {
			break
		}
		input.Urls = pending
	}
	if operation == "fetch" {
		for index, link := range originalURLs {
			if failure, exists := failures[link]; exists {
				failure.InputIndex = domain.Integer(index)
				response.Failures = append(response.Failures, failure)
			}
		}
		response.Partial = len(response.Failures) > 0
		if len(response.Results) == 0 && len(response.Failures) > 0 && lastFailure == nil {
			lastFailure = store.Reject("upstream_failure", 502, "Provider could not retrieve any requested URL")
		}
	}
	if lastFailure != nil && len(response.Results) == 0 {
		return response, lastFailure
	}
	if lastFailure != nil {
		response.Partial = true
		response.Meta.Warnings = append(response.Meta.Warnings, "Some failed URLs could not be retried within the remaining allowance")
	}
	response.Meta.DurationMs = domain.Integer(time.Since(started).Milliseconds())
	encoded, err := json.Marshal(response)
	if err != nil {
		return response, err
	}
	if int64(len(encoded)) > int64(settings.Limits.ResponseBytes) {
		return response, store.Reject("response_too_large", 502, "Normalized response exceeds the configured limit")
	}
	if err = domain.ValidateJSON("RetrievalResponse", encoded); err != nil {
		return response, store.Reject("upstream_protocol_error", 502, "Normalized response does not match the shared contract")
	}
	return response, nil
}
