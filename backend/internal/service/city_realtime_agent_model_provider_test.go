package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type cityRealtimeAgentTestGatewayProvider struct{}

func (cityRealtimeAgentTestGatewayProvider) ProviderCode() string {
	return cityRealtimeAgentModelProviderGateway
}

func (cityRealtimeAgentTestGatewayProvider) Execute(context.Context, CityRealtimeAgentProviderRequest) (CityRealtimeAgentProviderResponse, error) {
	return CityRealtimeAgentProviderResponse{}, nil
}

type cityRealtimeAgentPanickingProvider struct{}

func (cityRealtimeAgentPanickingProvider) ProviderCode() string {
	return cityRealtimeAgentModelProviderGateway
}

func (cityRealtimeAgentPanickingProvider) Execute(context.Context, CityRealtimeAgentProviderRequest) (CityRealtimeAgentProviderResponse, error) {
	panic("test provider panic")
}

func TestCityRealtimeAgentProviderResponseDecoderIsStrict(t *testing.T) {
	valid := map[string]any{
		"schema_version":    cityRealtimeAgentDecisionEnvelopeVersion,
		"request_code":      "adr.provider.test",
		"observation_hash":  strings.Repeat("a", 64),
		"precondition_hash": strings.Repeat("b", 64),
		"intent": map[string]any{
			"action_code": cityRealtimeAgentIntentActionWait,
			"arguments":   map[string]any{},
		},
		"reason_code": "provider_test_wait",
	}
	raw, err := json.Marshal(valid)
	require.NoError(t, err)
	envelope, err := decodeCityRealtimeAgentProviderDecisionEnvelope(raw)
	require.NoError(t, err)
	require.Equal(t, "adr.provider.test", envelope.RequestCode)

	for _, malformed := range []json.RawMessage{
		json.RawMessage(`{"unexpected":true}`),
		append(append(json.RawMessage(nil), raw...), []byte(` {}`)...),
		json.RawMessage(`null`),
		json.RawMessage(strings.Repeat("x", cityRealtimeAgentProviderMaximumDecisionEnvelopeBytes+1)),
	} {
		_, decodeErr := decodeCityRealtimeAgentProviderDecisionEnvelope(malformed)
		require.Error(t, decodeErr)
		require.Equal(t, cityRealtimeAgentProviderErrorInvalidResponse, cityRealtimeAgentProviderErrorCodeFrom(decodeErr))
	}
}

func TestCityRealtimeAgentProviderRegistryIsClosedAndImmutable(t *testing.T) {
	registry := newCityRealtimeAgentDecisionProviderRegistry()
	fake, found := registry.get(cityRealtimeAgentFakeProviderCode)
	require.True(t, found)
	require.NotNil(t, fake)

	require.Error(t, registry.register(cityRealtimeAgentFakeDecisionProvider{}), "the built-in deterministic verifier cannot be replaced")
	require.NoError(t, registry.register(cityRealtimeAgentTestGatewayProvider{}))
	registered, found := registry.get(cityRealtimeAgentModelProviderGateway)
	require.True(t, found)
	require.NotNil(t, registered)
	require.Error(t, registry.register(cityRealtimeAgentTestGatewayProvider{}), "gateway adapter registration is immutable")
}

func TestCityRealtimeAgentProviderFailureClassificationAndRetryDelay(t *testing.T) {
	for _, testCase := range []struct {
		err       error
		code      string
		retryable bool
	}{
		{context.DeadlineExceeded, cityRealtimeAgentProviderErrorTimeout, true},
		{context.Canceled, cityRealtimeAgentProviderErrorExecutionCanceled, false},
		{&CityRealtimeAgentDecisionProviderError{Code: cityRealtimeAgentProviderErrorRateLimited}, cityRealtimeAgentProviderErrorRateLimited, true},
		{&CityRealtimeAgentDecisionProviderError{Code: cityRealtimeAgentProviderErrorInvalidResponse}, cityRealtimeAgentProviderErrorInvalidResponse, false},
		{errors.New("transport failed"), cityRealtimeAgentProviderErrorTransport, true},
	} {
		require.Equal(t, testCase.code, cityRealtimeAgentProviderErrorCodeFrom(testCase.err))
		require.Equal(t, testCase.retryable, cityRealtimeAgentProviderFailureRetryable(testCase.code))
	}
	require.Equal(t, 5*time.Second, cityRealtimeAgentDecisionProviderRetryDelay(1))
	require.Equal(t, 10*time.Second, cityRealtimeAgentDecisionProviderRetryDelay(2))
	require.Equal(t, 20*time.Second, cityRealtimeAgentDecisionProviderRetryDelay(3))
	require.Equal(t, 2*time.Minute, cityRealtimeAgentDecisionProviderRetryDelay(8))
	require.Equal(t, 2*time.Minute, cityRealtimeAgentDecisionProviderRetryDelay(100))
}

func TestCityRealtimeAgentProviderPanicIsContained(t *testing.T) {
	_, err := cityRealtimeAgentExecuteDecisionProvider(context.Background(), cityRealtimeAgentPanickingProvider{}, CityRealtimeAgentProviderRequest{})
	require.Error(t, err)
	require.Equal(t, cityRealtimeAgentProviderErrorTransport, cityRealtimeAgentProviderErrorCodeFrom(err))
}
