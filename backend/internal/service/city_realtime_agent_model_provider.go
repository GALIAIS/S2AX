package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
)

const (
	cityRealtimeAgentProviderErrorTimeout            = "provider_timeout"
	cityRealtimeAgentProviderErrorRateLimited        = "provider_rate_limited"
	cityRealtimeAgentProviderErrorUnavailable        = "provider_unavailable"
	cityRealtimeAgentProviderErrorTransport          = "provider_transport"
	cityRealtimeAgentProviderErrorServer             = "provider_5xx"
	cityRealtimeAgentProviderErrorInvalidResponse    = "provider_invalid_response"
	cityRealtimeAgentProviderErrorAdapterUnavailable = "provider_adapter_unavailable"
	cityRealtimeAgentProviderErrorConfiguration      = "provider_configuration"
	cityRealtimeAgentProviderErrorExecutionCanceled  = "provider_execution_cancelled"

	cityRealtimeAgentProviderMaximumDecisionEnvelopeBytes = 64 * 1024
)

// CityRealtimeAgentDecisionProvider is the only execution boundary between a
// realtime Agent request and a model transport.  Providers receive a sealed,
// scope-filtered Observation plus an immutable, secret-free profile snapshot;
// they never receive a database handle, a user API key, an upstream account,
// a mutation callback, or an unrestricted tool surface.
//
// Provider implementations are process-local trusted code registered by the
// server at bootstrap. They are not created from browser input or database
// rows. The returned JSON is only a candidate and still passes the existing
// schema, precondition, action-context and reducer checks before it can affect
// the world.
type CityRealtimeAgentDecisionProvider interface {
	ProviderCode() string
	Execute(ctx context.Context, request CityRealtimeAgentProviderRequest) (CityRealtimeAgentProviderResponse, error)
}

// CityRealtimeAgentProviderProfile is the narrow execution projection of an
// immutable model profile. RouteRef is an internal bounded reference such as
// group:<id>, never an endpoint URL. PlatformGroupID is available only to a
// future internal gateway adapter for scheduler selection; it is never exposed
// to player-facing projections.
type CityRealtimeAgentProviderProfile struct {
	Code            string
	Version         int
	ProfileHash     string
	BudgetHash      string
	ProviderCode    string
	ProviderClass   string
	RouteRef        string
	PlatformGroupID *int64
	ModelIdentifier string
	Temperature     float64
	MaxInputTokens  int
	MaxOutputTokens int
	TimeoutMS       int
	ResponseSchema  string
	PrivacyClass    string
	RetentionPolicy string
	FallbackPolicy  string
}

// CityRealtimeAgentProviderRequest contains only the sealed facts a provider
// needs to produce one candidate decision. The exported fields deliberately
// omit mutable world state, credentials, provider URLs and raw persona source.
// The unexported fields support the deterministic adapter without broadening
// the external provider contract.
type CityRealtimeAgentProviderRequest struct {
	WorldID          int64
	RequestCode      string
	AttemptCode      string
	AttemptNumber    int
	RequestHash      string
	ObservationCode  string
	ObservationHash  string
	PreconditionHash string
	Observation      json.RawMessage
	Profile          *CityRealtimeAgentProviderProfile

	decisionRequest     cityRealtimeAgentDecisionRequestRecord
	observationRecord   cityRealtimeAgentObservationRecord
	fakePreferredAction string
}

// CityRealtimeAgentProviderResponse intentionally keeps only the strict
// decision-envelope candidate. Raw provider transcript, reasoning, tool calls
// and transport metadata must not enter the city state or audit persistence.
type CityRealtimeAgentProviderResponse struct {
	DecisionEnvelope json.RawMessage
}

// CityRealtimeAgentDecisionProviderError classifies a provider boundary
// failure without retaining the provider's original text. Only Code is written
// to the append-only attempt audit row. Err is for in-process diagnostics and
// is never persisted or returned through player APIs.
type CityRealtimeAgentDecisionProviderError struct {
	Code string
	Err  error
}

func (e *CityRealtimeAgentDecisionProviderError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Err == nil {
		return e.Code
	}
	return e.Code + ": " + e.Err.Error()
}

func (e *CityRealtimeAgentDecisionProviderError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func cityRealtimeAgentProviderErrorCodeValid(code string) bool {
	switch strings.TrimSpace(code) {
	case cityRealtimeAgentProviderErrorTimeout,
		cityRealtimeAgentProviderErrorRateLimited,
		cityRealtimeAgentProviderErrorUnavailable,
		cityRealtimeAgentProviderErrorTransport,
		cityRealtimeAgentProviderErrorServer,
		cityRealtimeAgentProviderErrorInvalidResponse,
		cityRealtimeAgentProviderErrorAdapterUnavailable,
		cityRealtimeAgentProviderErrorConfiguration,
		cityRealtimeAgentProviderErrorExecutionCanceled:
		return true
	default:
		return false
	}
}

func cityRealtimeAgentProviderErrorCodeFrom(err error) string {
	if err == nil {
		return ""
	}
	var providerErr *CityRealtimeAgentDecisionProviderError
	if errors.As(err, &providerErr) && providerErr != nil && cityRealtimeAgentProviderErrorCodeValid(providerErr.Code) {
		return providerErr.Code
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return cityRealtimeAgentProviderErrorTimeout
	}
	if errors.Is(err, context.Canceled) {
		return cityRealtimeAgentProviderErrorExecutionCanceled
	}
	return cityRealtimeAgentProviderErrorTransport
}

func cityRealtimeAgentProviderFailureRetryable(code string) bool {
	switch strings.TrimSpace(code) {
	case cityRealtimeAgentProviderErrorTimeout,
		cityRealtimeAgentProviderErrorRateLimited,
		cityRealtimeAgentProviderErrorUnavailable,
		cityRealtimeAgentProviderErrorTransport,
		cityRealtimeAgentProviderErrorServer:
		return true
	default:
		return false
	}
}

func cityRealtimeAgentProviderProfileSnapshot(profile *CityRealtimeAgentModelProfile) *CityRealtimeAgentProviderProfile {
	if profile == nil {
		return nil
	}
	item := &CityRealtimeAgentProviderProfile{
		Code:            profile.Code,
		Version:         profile.Version,
		ProfileHash:     profile.ProfileHash,
		BudgetHash:      profile.BudgetHash,
		ProviderCode:    profile.ProviderCode,
		ProviderClass:   profile.ProviderClass,
		RouteRef:        profile.RouteRef,
		ModelIdentifier: profile.ModelIdentifier,
		Temperature:     profile.Temperature,
		MaxInputTokens:  profile.MaxInputTokens,
		MaxOutputTokens: profile.MaxOutputTokens,
		TimeoutMS:       profile.TimeoutMS,
		ResponseSchema:  profile.ResponseSchemaVersion,
		PrivacyClass:    profile.PrivacyClass,
		RetentionPolicy: profile.RetentionPolicy,
		FallbackPolicy:  profile.FallbackPolicy,
	}
	if profile.PlatformGroupID != nil {
		value := *profile.PlatformGroupID
		item.PlatformGroupID = &value
	}
	return item
}

func cityRealtimeAgentDecisionProviderRequest(
	worldID int64,
	request cityRealtimeAgentDecisionRequestRecord,
	observation cityRealtimeAgentObservationRecord,
	attempt cityRealtimeAgentDecisionAttemptRecord,
	profile *CityRealtimeAgentModelProfile,
	preferredFakeAction string,
) CityRealtimeAgentProviderRequest {
	payload := append(json.RawMessage(nil), observation.Payload...)
	return CityRealtimeAgentProviderRequest{
		WorldID:             worldID,
		RequestCode:         request.RequestCode,
		AttemptCode:         attempt.AttemptCode,
		AttemptNumber:       attempt.AttemptNumber,
		RequestHash:         attempt.RequestHash,
		ObservationCode:     observation.ObservationCode,
		ObservationHash:     observation.PayloadHash,
		PreconditionHash:    observation.PreconditionHash,
		Observation:         payload,
		Profile:             cityRealtimeAgentProviderProfileSnapshot(profile),
		decisionRequest:     request,
		observationRecord:   observation,
		fakePreferredAction: preferredFakeAction,
	}
}

func decodeCityRealtimeAgentProviderDecisionEnvelope(raw json.RawMessage) (cityRealtimeAgentDecisionEnvelope, error) {
	if len(raw) == 0 || len(raw) > cityRealtimeAgentProviderMaximumDecisionEnvelopeBytes {
		return cityRealtimeAgentDecisionEnvelope{}, &CityRealtimeAgentDecisionProviderError{
			Code: cityRealtimeAgentProviderErrorInvalidResponse,
		}
	}
	trimmed := bytes.TrimSpace(raw)
	// Decode into a struct alone accepts JSON null. A provider response must be
	// exactly one object-shaped decision envelope, never null, an array, a
	// scalar, or a stream of multiple values.
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return cityRealtimeAgentDecisionEnvelope{}, &CityRealtimeAgentDecisionProviderError{
			Code: cityRealtimeAgentProviderErrorInvalidResponse,
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	var envelope cityRealtimeAgentDecisionEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		return cityRealtimeAgentDecisionEnvelope{}, &CityRealtimeAgentDecisionProviderError{
			Code: cityRealtimeAgentProviderErrorInvalidResponse,
			Err:  err,
		}
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			err = fmt.Errorf("provider response contains multiple JSON values")
		}
		return cityRealtimeAgentDecisionEnvelope{}, &CityRealtimeAgentDecisionProviderError{
			Code: cityRealtimeAgentProviderErrorInvalidResponse,
			Err:  err,
		}
	}
	return envelope, nil
}

func cityRealtimeAgentExecuteDecisionProvider(
	ctx context.Context,
	provider CityRealtimeAgentDecisionProvider,
	request CityRealtimeAgentProviderRequest,
) (response CityRealtimeAgentProviderResponse, err error) {
	if provider == nil {
		return CityRealtimeAgentProviderResponse{}, &CityRealtimeAgentDecisionProviderError{
			Code: cityRealtimeAgentProviderErrorAdapterUnavailable,
		}
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			response = CityRealtimeAgentProviderResponse{}
			err = &CityRealtimeAgentDecisionProviderError{
				Code: cityRealtimeAgentProviderErrorTransport,
				Err:  fmt.Errorf("provider execution panic: %v", recovered),
			}
		}
	}()
	return provider.Execute(ctx, request)
}

type cityRealtimeAgentFakeDecisionProvider struct{}

func (cityRealtimeAgentFakeDecisionProvider) ProviderCode() string {
	return cityRealtimeAgentFakeProviderCode
}

func (cityRealtimeAgentFakeDecisionProvider) Execute(ctx context.Context, request CityRealtimeAgentProviderRequest) (CityRealtimeAgentProviderResponse, error) {
	if err := ctx.Err(); err != nil {
		return CityRealtimeAgentProviderResponse{}, err
	}
	envelope, err := cityRealtimeAgentFakeDecisionEnvelope(
		request.decisionRequest,
		request.observationRecord,
		request.fakePreferredAction,
	)
	if err != nil {
		return CityRealtimeAgentProviderResponse{}, &CityRealtimeAgentDecisionProviderError{
			Code: cityRealtimeAgentProviderErrorInvalidResponse,
			Err:  err,
		}
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		return CityRealtimeAgentProviderResponse{}, &CityRealtimeAgentDecisionProviderError{
			Code: cityRealtimeAgentProviderErrorInvalidResponse,
			Err:  err,
		}
	}
	return CityRealtimeAgentProviderResponse{DecisionEnvelope: raw}, nil
}

type cityRealtimeAgentDecisionProviderRegistry struct {
	mu        sync.RWMutex
	providers map[string]CityRealtimeAgentDecisionProvider
}

func newCityRealtimeAgentDecisionProviderRegistry() *cityRealtimeAgentDecisionProviderRegistry {
	return &cityRealtimeAgentDecisionProviderRegistry{
		providers: map[string]CityRealtimeAgentDecisionProvider{
			cityRealtimeAgentFakeProviderCode: cityRealtimeAgentFakeDecisionProvider{},
		},
	}
}

func (r *cityRealtimeAgentDecisionProviderRegistry) get(code string) (CityRealtimeAgentDecisionProvider, bool) {
	if r == nil {
		return nil, false
	}
	code = strings.ToLower(strings.TrimSpace(code))
	r.mu.RLock()
	defer r.mu.RUnlock()
	provider, found := r.providers[code]
	return provider, found
}

func (r *cityRealtimeAgentDecisionProviderRegistry) register(provider CityRealtimeAgentDecisionProvider) error {
	if r == nil || provider == nil {
		return ErrCityInvalidInput
	}
	code := strings.ToLower(strings.TrimSpace(provider.ProviderCode()))
	// Model Profile validation intentionally has a closed provider vocabulary.
	// A real adapter can currently only back the gateway profile; fake remains a
	// built-in deterministic verifier and cannot be replaced at runtime.
	if code != cityRealtimeAgentModelProviderGateway {
		return ErrCityInvalidInput.WithMetadata(map[string]string{"field": "provider_code"})
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.providers[code]; exists {
		return ErrCityRealtimeAgentProviderUnavailable.WithMetadata(map[string]string{"provider_code": code})
	}
	r.providers[code] = provider
	return nil
}

func (s *CityEconomyService) cityRealtimeAgentProviderRegistry() *cityRealtimeAgentDecisionProviderRegistry {
	s.agentDecisionProviderMu.Lock()
	defer s.agentDecisionProviderMu.Unlock()
	if s.agentDecisionProviders == nil {
		s.agentDecisionProviders = newCityRealtimeAgentDecisionProviderRegistry()
	}
	return s.agentDecisionProviders
}

// RegisterRealtimeAgentDecisionProvider installs one trusted process-local
// adapter for a bounded model-provider kind. It is intended for application
// bootstrap, not HTTP handlers. Registration is immutable for the service
// lifetime so a running worker cannot silently switch transports mid-world.
func (s *CityEconomyService) RegisterRealtimeAgentDecisionProvider(provider CityRealtimeAgentDecisionProvider) error {
	if s == nil {
		return ErrCityInvalidInput
	}
	return s.cityRealtimeAgentProviderRegistry().register(provider)
}

func (s *CityEconomyService) cityRealtimeAgentDecisionProvider(code string) (CityRealtimeAgentDecisionProvider, bool) {
	if s == nil {
		return nil, false
	}
	return s.cityRealtimeAgentProviderRegistry().get(code)
}
