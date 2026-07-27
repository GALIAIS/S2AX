package securityaudit

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestExpireExceptionRejectsMissingAuditReasonBeforeRepositoryMutation(t *testing.T) {
	service := &PromptService{}

	_, err := service.ExpireException(context.Background(), 7, 11, ExpireExceptionRequest{Reason: "  "})

	require.ErrorIs(t, err, ErrExceptionReasonInvalid)
}

func TestSecurityPolicyBehaviorSignalsAreCanonicalAndValidated(t *testing.T) {
	config := defaultSecurityPolicyConfig()
	config.Signals.Enabled = true
	config.Signals.Rules = []BehaviorSignalRule{
		{
			ID: " TOKEN_BURST ", Enabled: true, Metric: " TOKEN_COUNT ",
			SubjectType: " USER ", WindowMinutes: 5, Threshold: 1000,
			MinimumSamples: 2, Severity: " HIGH ",
		},
		{
			ID: "request_burst", Enabled: true, Metric: "request_count",
			SubjectType: "api_key", WindowMinutes: 1, Threshold: 100,
			MinimumSamples: 10, Severity: "medium",
		},
	}

	canonical := canonicalSecurityPolicy(config)
	require.Equal(t, "request_burst", canonical.Signals.Rules[0].ID)
	require.Equal(t, "token_burst", canonical.Signals.Rules[1].ID)
	require.Empty(t, validateSecurityPolicy("default_security", canonical))
}

func TestSecurityPolicyRejectsUnsafeBehaviorSignalRules(t *testing.T) {
	config := defaultSecurityPolicyConfig()
	config.Signals.Enabled = true
	config.Signals.Rules = []BehaviorSignalRule{
		{
			ID: "bad", Enabled: true, Metric: "error_rate", SubjectType: "api_key",
			WindowMinutes: 0, Threshold: 2, MinimumSamples: 0, Severity: "extreme",
		},
	}

	errs := validateSecurityPolicy("default_security", canonicalSecurityPolicy(config))
	require.NotEmpty(t, errs)
	require.Contains(t, errs, "signal rule bad window_minutes 必须在 1-1440")
	require.Contains(t, errs, "signal rule bad minimum_samples 必须在 1-1000000000")
	require.Contains(t, errs, "signal rule severity 无效: extreme")
	require.Contains(t, errs, "signal rule bad 比率阈值必须在 (0,1]")
}

func TestSecurityPolicyRejectsActionsWithoutARealExecutor(t *testing.T) {
	config := defaultSecurityPolicyConfig()
	config.Actions.High = []string{"record_hash"}

	errs := validateSecurityPolicy("default_security", canonicalSecurityPolicy(config))

	require.Contains(t, errs, "actions.high 包含不支持的动作 record_hash")
}

func TestPolicyModeCannotLeakConfiguredActionsIntoEnforcement(t *testing.T) {
	for _, mode := range []Mode{ModeOff, ModeAsync} {
		t.Run(string(mode), func(t *testing.T) {
			config := defaultSecurityPolicyConfig()
			config.Mode = mode
			config.Actions.High = []string{"pause_user", "open_case", "notify_admin"}

			require.Equal(
				t,
				[]string{"notify_admin", "open_case", "pause_user"},
				candidateActionsForRisk(canonicalSecurityPolicy(config), "high"),
				"configured actions remain available for immutable policy review",
			)
			require.Empty(
				t,
				effectiveCandidateActionsForRisk(canonicalSecurityPolicy(config), "high"),
				"observation modes must never publish action/outbox records",
			)
		})
	}

	blocking := defaultSecurityPolicyConfig()
	blocking.Actions.High = []string{"pause_user", "open_case"}
	require.Equal(
		t,
		[]string{"open_case", "pause_user"},
		effectiveCandidateActionsForRisk(canonicalSecurityPolicy(blocking), "high"),
	)
}

func TestSimulatePolicyMakesObservationModeSideEffectsExplicit(t *testing.T) {
	config := defaultSecurityPolicyConfig()
	config.Mode = ModeAsync
	config.Actions.Critical = []string{"pause_user", "open_case"}
	policy := &PolicyVersion{PolicyKey: "audit_only", Config: config}

	result := simulatePolicy(policy, PolicySimulationRequest{UserID: 7, RiskLevel: "critical"})

	require.True(t, result.Matched)
	require.Equal(t, "allow", result.RequestAction)
	require.Empty(t, result.CandidateActions)
	require.Contains(t, result.Warnings, "策略模式为 async_audit：只记录审计结果，不阻断请求且不生成持久化处置动作")
	require.Contains(t, result.Trace, SimulationTrace{
		Step: "mode", Outcome: string(ModeAsync),
		Detail: "request_action=allow,candidate_actions=0",
	})
}

func TestPolicyScopeUsesIdentityUnionAndRequestFilterIntersection(t *testing.T) {
	groupID := int64(9)
	scope := PolicyScope{
		GroupIDs:  []int64{9},
		Protocols: []string{"OpenAI"},
		Endpoints: []string{"/v1/chat/completions"},
		Models:    []string{"gpt-5"},
	}

	require.True(t, policyScopeMatches(scope, PolicySimulationRequest{
		GroupID: &groupID, Protocol: "openai", Endpoint: "/V1/CHAT/COMPLETIONS", Model: "GPT-5",
	}))
	require.False(t, policyScopeMatches(scope, PolicySimulationRequest{
		GroupID: &groupID, Protocol: "anthropic", Endpoint: "/v1/chat/completions", Model: "gpt-5",
	}), "matching identity must not bypass request filters")
	require.False(t, policyScopeMatches(scope, PolicySimulationRequest{
		UserID: 22, Protocol: "openai", Endpoint: "/v1/chat/completions", Model: "gpt-5",
	}), "matching request filters must not bypass identity selectors")
}

func TestPolicyScopeAllowsRequestOnlyAndGlobalFilteredPolicies(t *testing.T) {
	require.True(t, policyScopeMatches(
		PolicyScope{Endpoints: []string{"/v1/messages"}},
		PolicySimulationRequest{UserID: 22, Endpoint: "/V1/MESSAGES"},
	))
	require.False(t, policyScopeMatches(
		PolicyScope{AllGroups: true, Protocols: []string{"anthropic"}},
		PolicySimulationRequest{UserID: 22, Protocol: "openai"},
	))
}

func TestSecurityPolicyRejectsAmbiguousAndUnevaluableScopes(t *testing.T) {
	t.Run("all groups cannot coexist with identity selectors", func(t *testing.T) {
		config := defaultSecurityPolicyConfig()
		config.Scope.UserIDs = []int64{22}

		errs := validateSecurityPolicy("default_security", canonicalSecurityPolicy(config))

		require.Contains(t, errs, "scope.all_groups 与 group_ids/user_ids/api_key_ids 不能同时配置")
	})

	t.Run("behavior signals cannot claim unavailable request dimensions", func(t *testing.T) {
		config := defaultSecurityPolicyConfig()
		config.Scope = PolicyScope{AllGroups: true, Models: []string{"gpt-5"}}
		config.Signals.Enabled = true
		config.Signals.Rules = []BehaviorSignalRule{{
			ID: "request_burst", Enabled: true, Metric: "request_count",
			SubjectType: "api_key", WindowMinutes: 1, Threshold: 100,
			MinimumSamples: 10, Severity: "medium",
		}}

		errs := validateSecurityPolicy("default_security", canonicalSecurityPolicy(config))

		require.Contains(t, errs, "行为信号策略不能配置 protocols/endpoints/models；聚合窗口不包含请求维度")
	})
}

func TestPolicyTransitionReasonRequiresAuditableContext(t *testing.T) {
	require.False(t, validPolicyTransitionReason(""))
	require.False(t, validPolicyTransitionReason("ok"))
	require.True(t, validPolicyTransitionReason("reviewed rollout"))
	require.False(t, validPolicyTransitionReason(strings.Repeat("x", 513)))
}

func TestSelectBehaviorPolicyUsesMostSpecificScopeBeforePriority(t *testing.T) {
	groupID := int64(9)
	window := &BehaviorSignalWindow{
		ID: 1, BucketStart: time.Now(), SubjectType: "api_key", SubjectID: 33,
		UserID: int64Ptr(22), APIKeyID: int64Ptr(33), GroupID: &groupID,
	}
	global := &PolicyVersion{
		PolicyKey: "global", Priority: 1000,
		Config: SecurityPolicyConfig{Scope: PolicyScope{AllGroups: true}, Signals: PolicySignals{Enabled: true}},
	}
	user := &PolicyVersion{
		PolicyKey: "user", Priority: 1,
		Config: SecurityPolicyConfig{Scope: PolicyScope{UserIDs: []int64{22}}, Signals: PolicySignals{Enabled: true}},
	}
	apiKey := &PolicyVersion{
		PolicyKey: "api-key", Priority: -100,
		Config: SecurityPolicyConfig{Scope: PolicyScope{APIKeyIDs: []int64{33}}, Signals: PolicySignals{Enabled: true}},
	}

	require.Equal(t, apiKey, selectBehaviorPolicy([]*PolicyVersion{global, user, apiKey}, window))
}

func TestSelectBehaviorPolicySkipsRequestFilteredPolicy(t *testing.T) {
	window := &BehaviorSignalWindow{
		ID: 1, BucketStart: time.Now(), SubjectType: "api_key", SubjectID: 33,
		UserID: int64Ptr(22), APIKeyID: int64Ptr(33),
	}
	filtered := &PolicyVersion{
		PolicyKey: "request-filtered", Priority: 1000,
		Config: SecurityPolicyConfig{
			Scope:   PolicyScope{APIKeyIDs: []int64{33}, Models: []string{"gpt-5"}},
			Signals: PolicySignals{Enabled: true},
		},
	}
	global := &PolicyVersion{
		PolicyKey: "global", Priority: 1,
		Config: SecurityPolicyConfig{Scope: PolicyScope{AllGroups: true}, Signals: PolicySignals{Enabled: true}},
	}

	require.Equal(t, global, selectBehaviorPolicy([]*PolicyVersion{filtered, global}, window))
}

func int64Ptr(value int64) *int64 { return &value }
