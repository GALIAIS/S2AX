package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

type cityRealtimeAgentGatewayTestScheduler struct {
	lease cityRealtimeAgentGatewayAccountLease
	err   error

	groupID int64
	session string
	model   string
}

func (s *cityRealtimeAgentGatewayTestScheduler) SelectSystemAccount(
	_ context.Context,
	groupID int64,
	sessionHash string,
	modelIdentifier string,
) (cityRealtimeAgentGatewayAccountLease, error) {
	s.groupID = groupID
	s.session = sessionHash
	s.model = modelIdentifier
	return s.lease, s.err
}

type cityRealtimeAgentGatewayTestUpstream struct {
	response *http.Response
	err      error
	request  *http.Request
	proxyURL string
}

func (u *cityRealtimeAgentGatewayTestUpstream) Do(
	_ *http.Request,
	_ string,
	_ int64,
	_ int,
) (*http.Response, error) {
	return nil, errors.New("unexpected non-TLS upstream request")
}

func (u *cityRealtimeAgentGatewayTestUpstream) DoWithTLS(
	request *http.Request,
	proxyURL string,
	_ int64,
	_ int,
	_ *tlsfingerprint.Profile,
) (*http.Response, error) {
	u.request = request
	u.proxyURL = proxyURL
	return u.response, u.err
}

func cityRealtimeAgentGatewayTestProviderRequest() CityRealtimeAgentProviderRequest {
	groupID := int64(71)
	return CityRealtimeAgentProviderRequest{
		WorldID:          9,
		RequestCode:      "adr.gateway.test",
		AttemptCode:      "aat.gateway.test",
		AttemptNumber:    1,
		RequestHash:      strings.Repeat("a", 64),
		ObservationCode:  "aob.gateway.test",
		ObservationHash:  strings.Repeat("b", 64),
		PreconditionHash: strings.Repeat("c", 64),
		Observation: json.RawMessage(`{
  "allowed_actions":["wait"],
  "character":{"action_context":{"schema_version":1}}
}`),
		Profile: &CityRealtimeAgentProviderProfile{
			Code:            "gateway.model",
			Version:         2,
			ProviderCode:    cityRealtimeAgentModelProviderGateway,
			ProviderClass:   "sub2api_group",
			RouteRef:        "group:71",
			PlatformGroupID: &groupID,
			ModelIdentifier: "gpt-5-mini",
			Temperature:     0.2,
			MaxInputTokens:  4096,
			MaxOutputTokens: 512,
			ResponseSchema:  cityRealtimeAgentDecisionEnvelopeVersion,
		},
	}
}

func cityRealtimeAgentGatewayTestEnvelope(request CityRealtimeAgentProviderRequest) string {
	return `{"schema_version":"agent-decision-v1","request_code":"` + request.RequestCode +
		`","observation_hash":"` + request.ObservationHash +
		`","precondition_hash":"` + request.PreconditionHash +
		`","intent":{"action_code":"wait","arguments":{}},"reason_code":"gateway_wait"}`
}

func TestCityRealtimeAgentGatewayProviderUsesDedicatedSystemTransport(t *testing.T) {
	request := cityRealtimeAgentGatewayTestProviderRequest()
	decision := cityRealtimeAgentGatewayTestEnvelope(request)
	upstream := &cityRealtimeAgentGatewayTestUpstream{
		response: &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":` + strconvQuoteJSON(decision) + `}}]}`)),
			Header:     make(http.Header),
		},
	}
	released := false
	scheduler := &cityRealtimeAgentGatewayTestScheduler{lease: cityRealtimeAgentGatewayAccountLease{
		Account: &Account{
			ID:          17,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Concurrency: 3,
			Credentials: map[string]any{
				"api_key":       "sk-city-test",
				"base_url":      "https://models.example.test",
				"model_mapping": map[string]any{"gpt-5-mini": "upstream-city-model"},
			},
		},
		Release: func() { released = true },
	}}
	transport := &cityRealtimeAgentGatewaySystemHTTPTransport{
		scheduler:       scheduler,
		httpUpstream:    upstream,
		validateBaseURL: func(value string) (string, error) { return value, nil },
	}
	provider := newCityRealtimeAgentGatewayDecisionProvider(transport)

	response, err := provider.Execute(context.Background(), request)
	require.NoError(t, err)
	require.JSONEq(t, decision, string(response.DecisionEnvelope))
	require.True(t, released)
	require.Equal(t, int64(71), scheduler.groupID)
	require.Equal(t, "gpt-5-mini", scheduler.model)
	require.True(t, strings.HasPrefix(scheduler.session, "city-agent-"))
	require.NotNil(t, upstream.request)
	require.Equal(t, http.MethodPost, upstream.request.Method)
	require.Equal(t, "https://models.example.test/v1/chat/completions", upstream.request.URL.String())
	require.Equal(t, "Bearer sk-city-test", upstream.request.Header.Get("Authorization"))
	require.Equal(t, cityRealtimeAgentGatewaySystemIdentityUserAgent, upstream.request.Header.Get("User-Agent"))
	require.True(t, strings.HasPrefix(upstream.request.Header.Get("X-Client-Request-ID"), "city-agent-"))
	identity, ok := cityRealtimeAgentGatewaySystemIdentityFromContext(upstream.request.Context())
	require.True(t, ok)
	require.Equal(t, cityRealtimeAgentGatewaySystemIdentityName, identity.Workload)
	require.Equal(t, upstream.request.Header.Get("X-Client-Request-ID"), identity.RequestID)
	require.Equal(t, HTTPUpstreamProfileOpenAI, HTTPUpstreamProfileFromContext(upstream.request.Context()))

	body, readErr := io.ReadAll(upstream.request.Body)
	require.NoError(t, readErr)
	var payload cityRealtimeAgentGatewayChatRequest
	require.NoError(t, json.Unmarshal(body, &payload))
	require.Equal(t, "upstream-city-model", payload.Model)
	require.False(t, payload.Stream)
	require.Equal(t, 512, payload.MaxTokens)
	require.Len(t, payload.Messages, 2)
	require.Contains(t, payload.Messages[0].Content, request.RequestCode)
	require.Contains(t, payload.Messages[1].Content, string(request.Observation))
	require.NotContains(t, payload.Messages[0].Content, "sk-city-test")
}

func TestCityRealtimeAgentGatewayProviderFailsClosedForUnsupportedAccountAndBadOutput(t *testing.T) {
	request := cityRealtimeAgentGatewayTestProviderRequest()
	unsupported := &cityRealtimeAgentGatewaySystemHTTPTransport{
		scheduler: &cityRealtimeAgentGatewayTestScheduler{lease: cityRealtimeAgentGatewayAccountLease{Account: &Account{
			ID: 17, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		}}},
		httpUpstream:    &cityRealtimeAgentGatewayTestUpstream{},
		validateBaseURL: func(value string) (string, error) { return value, nil },
	}
	_, err := newCityRealtimeAgentGatewayDecisionProvider(unsupported).Execute(context.Background(), request)
	require.Error(t, err)
	require.Equal(t, cityRealtimeAgentProviderErrorConfiguration, cityRealtimeAgentProviderErrorCodeFrom(err))

	badOutput := &cityRealtimeAgentGatewaySystemHTTPTransport{
		scheduler: &cityRealtimeAgentGatewayTestScheduler{lease: cityRealtimeAgentGatewayAccountLease{Account: &Account{
			ID: 19, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
			Credentials: map[string]any{"api_key": "sk-city-test", "base_url": "https://models.example.test"},
		}}},
		httpUpstream: &cityRealtimeAgentGatewayTestUpstream{response: &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("{\"choices\":[{\"message\":{\"content\":\"```json\\n{}\\n```\"}}]}")),
		}},
		validateBaseURL: func(value string) (string, error) { return value, nil },
	}
	_, err = newCityRealtimeAgentGatewayDecisionProvider(badOutput).Execute(context.Background(), request)
	require.Error(t, err)
	require.Equal(t, cityRealtimeAgentProviderErrorInvalidResponse, cityRealtimeAgentProviderErrorCodeFrom(err))
}

func TestCityRealtimeAgentGatewayProviderClassifiesHTTPFailures(t *testing.T) {
	for _, testCase := range []struct {
		statusCode int
		want       string
	}{
		{http.StatusTooManyRequests, cityRealtimeAgentProviderErrorRateLimited},
		{http.StatusServiceUnavailable, cityRealtimeAgentProviderErrorUnavailable},
		{http.StatusInternalServerError, cityRealtimeAgentProviderErrorServer},
		{http.StatusUnauthorized, cityRealtimeAgentProviderErrorConfiguration},
	} {
		require.Equal(t, testCase.want, cityRealtimeAgentGatewayHTTPFailureCode(testCase.statusCode))
	}
}

func TestCityRealtimeAgentGatewayProviderUsesAgentIdentityCodexResponsesTransport(t *testing.T) {
	request := cityRealtimeAgentGatewayTestProviderRequest()
	request.Profile.Temperature = 0
	decision := cityRealtimeAgentGatewayTestEnvelope(request)
	key, privateKey := newTestAgentIdentityKey(t)
	upstream := &cityRealtimeAgentGatewayTestUpstream{
		response: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body: io.NopCloser(strings.NewReader(
				`data: {"type":"response.completed","response":{"id":"resp-city-agent","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":` + strconvQuoteJSON(decision) + `}]}]}}` + "\n\n",
			)),
		},
	}
	released := false
	scheduler := &cityRealtimeAgentGatewayTestScheduler{lease: cityRealtimeAgentGatewayAccountLease{
		Account: &Account{
			ID:          23,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeOAuth,
			Concurrency: 2,
			Credentials: map[string]any{
				"auth_mode":                  OpenAIAuthModeAgentIdentity,
				"agent_runtime_id":           key.runtimeID,
				"agent_private_key":          privateKey,
				"task_id":                    key.taskID,
				"chatgpt_account_id":         "acct-city-agent",
				"chatgpt_account_is_fedramp": true,
			},
		},
		Release: func() { released = true },
	}}
	provider := newCityRealtimeAgentGatewayDecisionProvider(&cityRealtimeAgentGatewaySystemHTTPTransport{
		scheduler:    scheduler,
		httpUpstream: upstream,
	})

	response, err := provider.Execute(context.Background(), request)
	require.NoError(t, err)
	require.JSONEq(t, decision, string(response.DecisionEnvelope))
	require.True(t, released)
	require.NotNil(t, upstream.request)
	require.Equal(t, http.MethodPost, upstream.request.Method)
	require.Equal(t, chatgptCodexURL, upstream.request.URL.String())
	require.Equal(t, "chatgpt.com", upstream.request.Host)
	require.True(t, strings.HasPrefix(upstream.request.Header.Get("Authorization"), "AgentAssertion "))
	require.NotContains(t, upstream.request.Header.Get("Authorization"), privateKey)
	require.NotContains(t, upstream.request.Header.Get("Authorization"), "Bearer ")
	require.Equal(t, "acct-city-agent", upstream.request.Header.Get("chatgpt-account-id"))
	require.Equal(t, "true", upstream.request.Header.Get("x-openai-fedramp"))
	require.Equal(t, "text/event-stream", upstream.request.Header.Get("Accept"))
	require.Equal(t, "responses=experimental", upstream.request.Header.Get("OpenAI-Beta"))
	require.Equal(t, "codex_cli_rs", upstream.request.Header.Get("Originator"))
	require.Equal(t, codexCLIUserAgent, upstream.request.Header.Get("User-Agent"))
	require.Equal(t, codexCLIVersion, upstream.request.Header.Get("Version"))
	require.True(t, strings.HasPrefix(upstream.request.Header.Get("X-Client-Request-ID"), "city-agent-"))
	identity, ok := cityRealtimeAgentGatewaySystemIdentityFromContext(upstream.request.Context())
	require.True(t, ok)
	require.Equal(t, cityRealtimeAgentGatewaySystemIdentityName, identity.Workload)
	require.Equal(t, upstream.request.Header.Get("X-Client-Request-ID"), identity.RequestID)

	body, readErr := io.ReadAll(upstream.request.Body)
	require.NoError(t, readErr)
	var payload cityRealtimeAgentGatewayResponsesRequest
	require.NoError(t, json.Unmarshal(body, &payload))
	require.Equal(t, "gpt-5.4", payload.Model)
	require.True(t, payload.Stream)
	require.False(t, payload.Store)
	require.Contains(t, payload.Instructions, request.RequestCode)
	require.Len(t, payload.Input, 1)
	require.Equal(t, "user", payload.Input[0].Role)
	require.Contains(t, payload.Input[0].Content, string(request.Observation))
	require.NotContains(t, string(body), privateKey)
	require.NotContains(t, string(body), `"temperature"`)
	require.NotContains(t, string(body), `"max_output_tokens"`)
}

func TestCityRealtimeAgentGatewayProviderAgentIdentityFailsClosedForIncompleteOrInvalidTask(t *testing.T) {
	request := cityRealtimeAgentGatewayTestProviderRequest()
	request.Profile.Temperature = 0
	key, privateKey := newTestAgentIdentityKey(t)
	baseAccount := func() *Account {
		return &Account{
			ID:       29,
			Platform: PlatformOpenAI,
			Type:     AccountTypeOAuth,
			Credentials: map[string]any{
				"auth_mode":          OpenAIAuthModeAgentIdentity,
				"agent_runtime_id":   key.runtimeID,
				"agent_private_key":  privateKey,
				"chatgpt_account_id": "acct-city-agent-fail-closed",
			},
		}
	}

	missingTaskUpstream := &cityRealtimeAgentGatewayTestUpstream{}
	missingTaskProvider := newCityRealtimeAgentGatewayDecisionProvider(&cityRealtimeAgentGatewaySystemHTTPTransport{
		scheduler:    &cityRealtimeAgentGatewayTestScheduler{lease: cityRealtimeAgentGatewayAccountLease{Account: baseAccount()}},
		httpUpstream: missingTaskUpstream,
	})
	_, err := missingTaskProvider.Execute(context.Background(), request)
	require.Error(t, err)
	require.Equal(t, cityRealtimeAgentProviderErrorConfiguration, cityRealtimeAgentProviderErrorCodeFrom(err))
	require.Nil(t, missingTaskUpstream.request, "incomplete identity must not attempt registration or an upstream model call")

	invalidTaskAccount := baseAccount()
	invalidTaskAccount.Credentials["task_id"] = key.taskID
	invalidTaskUpstream := &cityRealtimeAgentGatewayTestUpstream{response: &http.Response{
		StatusCode: http.StatusUnauthorized,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":{"code":"invalid_task_id"}}`)),
	}}
	invalidTaskProvider := newCityRealtimeAgentGatewayDecisionProvider(&cityRealtimeAgentGatewaySystemHTTPTransport{
		scheduler:    &cityRealtimeAgentGatewayTestScheduler{lease: cityRealtimeAgentGatewayAccountLease{Account: invalidTaskAccount}},
		httpUpstream: invalidTaskUpstream,
	})
	_, err = invalidTaskProvider.Execute(context.Background(), request)
	require.Error(t, err)
	require.Equal(t, cityRealtimeAgentProviderErrorConfiguration, cityRealtimeAgentProviderErrorCodeFrom(err))
	require.NotNil(t, invalidTaskUpstream.request)
	require.Equal(t, key.taskID, invalidTaskAccount.GetCredential("task_id"), "system transport must not auto-register or mutate task state")

	nonzeroTemperatureRequest := cityRealtimeAgentGatewayTestProviderRequest()
	readyAccount := baseAccount()
	readyAccount.Credentials["task_id"] = key.taskID
	nonzeroTemperatureUpstream := &cityRealtimeAgentGatewayTestUpstream{}
	nonzeroTemperatureProvider := newCityRealtimeAgentGatewayDecisionProvider(&cityRealtimeAgentGatewaySystemHTTPTransport{
		scheduler:    &cityRealtimeAgentGatewayTestScheduler{lease: cityRealtimeAgentGatewayAccountLease{Account: readyAccount}},
		httpUpstream: nonzeroTemperatureUpstream,
	})
	_, err = nonzeroTemperatureProvider.Execute(context.Background(), nonzeroTemperatureRequest)
	require.Error(t, err)
	require.Equal(t, cityRealtimeAgentProviderErrorConfiguration, cityRealtimeAgentProviderErrorCodeFrom(err))
	require.Nil(t, nonzeroTemperatureUpstream.request, "unsupported Agent Identity temperature must fail before an upstream call")
}

func TestCityRealtimeAgentGatewayAccountTransportReadyAllowsOnlyExplicitSystemTransports(t *testing.T) {
	key, privateKey := newTestAgentIdentityKey(t)
	require.False(t, cityRealtimeAgentGatewayAccountTransportReady(&Account{
		ID: 31, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Credentials: map[string]any{"access_token": "oauth-user-token", "chatgpt_account_id": "acct-normal"},
	}))
	require.True(t, cityRealtimeAgentGatewayAccountTransportReady(&Account{
		ID: 32, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-system-key"},
	}))
	require.True(t, cityRealtimeAgentGatewayAccountTransportReady(&Account{
		ID: 33, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Credentials: map[string]any{
			"auth_mode":          OpenAIAuthModeAgentIdentity,
			"agent_runtime_id":   key.runtimeID,
			"agent_private_key":  privateKey,
			"task_id":            key.taskID,
			"chatgpt_account_id": "acct-agent-ready",
		},
	}))
}

func strconvQuoteJSON(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
