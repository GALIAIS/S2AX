package securityaudit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

var policyKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{2,95}$`)
var signalRuleIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{2,95}$`)

var validPolicyActions = map[string]struct{}{
	"pause_api_key": {}, "pause_user": {}, "notify_user": {}, "notify_admin": {}, "open_case": {},
}

var validSignalMetrics = map[string]struct{}{
	"request_count": {}, "token_count": {}, "actual_cost": {},
	"error_count": {}, "error_rate": {}, "business_limited_rate": {},
	"average_duration_ms": {}, "maximum_duration_ms": {},
	"distinct_ip_count": {}, "distinct_model_count": {},
}

var validSignalSubjects = map[string]struct{}{"user": {}, "api_key": {}, "group": {}}
var validRiskSeverities = map[string]struct{}{"low": {}, "medium": {}, "high": {}, "critical": {}}

func defaultSecurityPolicyConfig() SecurityPolicyConfig {
	return SecurityPolicyConfig{
		Name:     "Default safety policy",
		Priority: 100,
		Scope:    PolicyScope{AllGroups: true},
		Mode:     ModeBlocking,
		Detectors: []PolicyDetector{
			{ID: "builtin_regex", Enabled: true, TimeoutMS: 20},
			{ID: "remote_guard", Enabled: true, TimeoutMS: 2500},
		},
		Failure: PolicyFailure{
			LocalError: FailureAllowAndRecord, RemoteTimeout: FailureFallbackLocal, RemoteInvalid: FailureBlockAndRecord,
		},
		Actions: PolicyActions{
			Medium:   []string{"notify_admin"},
			High:     []string{"open_case", "notify_admin"},
			Critical: []string{"open_case", "notify_admin"},
		},
		Evidence: PolicyEvidence{Mode: "findings_encrypted", RetentionDays: 30},
		Signals: PolicySignals{
			Enabled: false,
			Rules: []BehaviorSignalRule{
				{ID: "request_burst", Enabled: true, Metric: "request_count", SubjectType: "api_key", WindowMinutes: 1, Threshold: 600, MinimumSamples: 100, Severity: "medium"},
				{ID: "token_burst", Enabled: true, Metric: "token_count", SubjectType: "user", WindowMinutes: 5, Threshold: 5_000_000, MinimumSamples: 20, Severity: "high"},
				{ID: "cost_burst", Enabled: true, Metric: "actual_cost", SubjectType: "user", WindowMinutes: 60, Threshold: 100, MinimumSamples: 10, Severity: "high"},
				{ID: "error_ratio", Enabled: true, Metric: "error_rate", SubjectType: "api_key", WindowMinutes: 5, Threshold: 0.8, MinimumSamples: 20, Severity: "medium"},
				{ID: "ip_fanout", Enabled: true, Metric: "distinct_ip_count", SubjectType: "api_key", WindowMinutes: 1, Threshold: 20, MinimumSamples: 20, Severity: "high"},
			},
		},
	}
}

func canonicalSecurityPolicy(config SecurityPolicyConfig) SecurityPolicyConfig {
	config.Name = strings.TrimSpace(config.Name)
	if config.Name == "" {
		config.Name = "Security policy"
	}
	config.Scope.GroupIDs = canonicalPositiveInt64s(config.Scope.GroupIDs)
	config.Scope.UserIDs = canonicalPositiveInt64s(config.Scope.UserIDs)
	config.Scope.APIKeyIDs = canonicalPositiveInt64s(config.Scope.APIKeyIDs)
	config.Scope.Protocols = canonicalStrings(config.Scope.Protocols)
	config.Scope.Endpoints = canonicalStrings(config.Scope.Endpoints)
	config.Scope.Models = canonicalStrings(config.Scope.Models)
	for i := range config.Detectors {
		config.Detectors[i].ID = strings.ToLower(strings.TrimSpace(config.Detectors[i].ID))
	}
	sort.SliceStable(config.Detectors, func(i, j int) bool { return config.Detectors[i].ID < config.Detectors[j].ID })
	config.Actions.Low = canonicalActions(config.Actions.Low)
	config.Actions.Medium = canonicalActions(config.Actions.Medium)
	config.Actions.High = canonicalActions(config.Actions.High)
	config.Actions.Critical = canonicalActions(config.Actions.Critical)
	for i := range config.Signals.Rules {
		rule := &config.Signals.Rules[i]
		rule.ID = strings.ToLower(strings.TrimSpace(rule.ID))
		rule.Metric = strings.ToLower(strings.TrimSpace(rule.Metric))
		rule.SubjectType = strings.ToLower(strings.TrimSpace(rule.SubjectType))
		rule.Severity = strings.ToLower(strings.TrimSpace(rule.Severity))
	}
	sort.SliceStable(config.Signals.Rules, func(i, j int) bool {
		return config.Signals.Rules[i].ID < config.Signals.Rules[j].ID
	})
	config.Evidence.Mode = strings.ToLower(strings.TrimSpace(config.Evidence.Mode))
	config.Failure.LocalError = normalizeFailureMode(config.Failure.LocalError)
	config.Failure.RemoteTimeout = normalizeFailureMode(config.Failure.RemoteTimeout)
	config.Failure.RemoteInvalid = normalizeFailureMode(config.Failure.RemoteInvalid)
	return config
}

func validateSecurityPolicy(policyKey string, config SecurityPolicyConfig) []string {
	errs := make([]string, 0)
	if !policyKeyPattern.MatchString(strings.TrimSpace(policyKey)) {
		errs = append(errs, "policy_key 必须以小写字母开头且仅包含小写字母、数字、_、-，长度 3-96")
	}
	if length := len([]rune(strings.TrimSpace(config.Name))); length < 2 || length > 160 {
		errs = append(errs, "name 长度必须为 2-160")
	}
	if config.Priority < -100000 || config.Priority > 100000 {
		errs = append(errs, "priority 必须在 -100000 到 100000 之间")
	}
	switch config.Mode {
	case ModeOff, ModeAsync, ModeBlocking:
	default:
		errs = append(errs, "mode 无效")
	}
	if !config.Scope.AllGroups && len(config.Scope.GroupIDs)+len(config.Scope.UserIDs)+len(config.Scope.APIKeyIDs)+
		len(config.Scope.Protocols)+len(config.Scope.Endpoints)+len(config.Scope.Models) == 0 {
		errs = append(errs, "策略必须至少配置一个作用域")
	}
	if config.Scope.AllGroups &&
		len(config.Scope.GroupIDs)+len(config.Scope.UserIDs)+len(config.Scope.APIKeyIDs) > 0 {
		errs = append(errs, "scope.all_groups 与 group_ids/user_ids/api_key_ids 不能同时配置")
	}
	if len(config.Detectors) == 0 {
		errs = append(errs, "至少需要一个检测器")
	}
	seenDetectors := map[string]struct{}{}
	enabled := 0
	for _, detector := range config.Detectors {
		if detector.ID == "" {
			errs = append(errs, "detector.id 不能为空")
			continue
		}
		if _, exists := seenDetectors[detector.ID]; exists {
			errs = append(errs, "detector.id 重复: "+detector.ID)
		}
		seenDetectors[detector.ID] = struct{}{}
		if detector.Enabled {
			enabled++
		}
		if detector.TimeoutMS < 1 || detector.TimeoutMS > 30000 {
			errs = append(errs, fmt.Sprintf("检测器 %s timeout_ms 必须在 1-30000", detector.ID))
		}
	}
	if config.Mode != ModeOff && enabled == 0 {
		errs = append(errs, "启用策略至少需要一个启用的检测器")
	}
	for label, mode := range map[string]FailureMode{
		"failure.local_error": config.Failure.LocalError, "failure.remote_timeout": config.Failure.RemoteTimeout,
		"failure.remote_invalid": config.Failure.RemoteInvalid,
	} {
		if !validFailureMode(mode) {
			errs = append(errs, label+" 无效")
		}
	}
	for level, actions := range map[string][]string{
		"low": config.Actions.Low, "medium": config.Actions.Medium,
		"high": config.Actions.High, "critical": config.Actions.Critical,
	} {
		for _, action := range actions {
			if _, ok := validPolicyActions[action]; !ok {
				errs = append(errs, fmt.Sprintf("actions.%s 包含不支持的动作 %s", level, action))
			}
			if level == "low" && (action == "pause_user" || action == "pause_api_key") {
				errs = append(errs, "low 风险不得自动暂停主体")
			}
		}
	}
	switch config.Evidence.Mode {
	case "none", "digest_only", "findings_encrypted", "full_encrypted":
	default:
		errs = append(errs, "evidence.mode 无效")
	}
	if config.Evidence.RetentionDays < 0 || config.Evidence.RetentionDays > 3650 {
		errs = append(errs, "evidence.retention_days 必须在 0-3650")
	}
	if len(config.Signals.Rules) > 64 {
		errs = append(errs, "signals.rules 最多允许 64 条")
	}
	seenRules := map[string]struct{}{}
	enabledRules := 0
	for _, rule := range config.Signals.Rules {
		if !signalRuleIDPattern.MatchString(rule.ID) {
			errs = append(errs, "signal rule id 必须以小写字母开头且仅包含小写字母、数字、_、-，长度 3-96")
		}
		if _, exists := seenRules[rule.ID]; exists {
			errs = append(errs, "signal rule id 重复: "+rule.ID)
		}
		seenRules[rule.ID] = struct{}{}
		if rule.Enabled {
			enabledRules++
		}
		if _, ok := validSignalMetrics[rule.Metric]; !ok {
			errs = append(errs, "signal rule metric 无效: "+rule.Metric)
		}
		if _, ok := validSignalSubjects[rule.SubjectType]; !ok {
			errs = append(errs, "signal rule subject_type 无效: "+rule.SubjectType)
		}
		if rule.WindowMinutes < 1 || rule.WindowMinutes > 1440 {
			errs = append(errs, fmt.Sprintf("signal rule %s window_minutes 必须在 1-1440", rule.ID))
		}
		if rule.Threshold <= 0 || rule.Threshold > 1e15 {
			errs = append(errs, fmt.Sprintf("signal rule %s threshold 必须大于 0", rule.ID))
		}
		if rule.MinimumSamples < 1 || rule.MinimumSamples > 1_000_000_000 {
			errs = append(errs, fmt.Sprintf("signal rule %s minimum_samples 必须在 1-1000000000", rule.ID))
		}
		if _, ok := validRiskSeverities[rule.Severity]; !ok {
			errs = append(errs, "signal rule severity 无效: "+rule.Severity)
		}
		if (rule.Metric == "error_rate" || rule.Metric == "business_limited_rate") && rule.Threshold > 1 {
			errs = append(errs, fmt.Sprintf("signal rule %s 比率阈值必须在 (0,1]", rule.ID))
		}
	}
	if config.Signals.Enabled && enabledRules == 0 {
		errs = append(errs, "启用行为信号时至少需要一条启用规则")
	}
	if config.Signals.Enabled &&
		len(config.Scope.Protocols)+len(config.Scope.Endpoints)+len(config.Scope.Models) > 0 {
		errs = append(errs, "行为信号策略不能配置 protocols/endpoints/models；聚合窗口不包含请求维度")
	}
	return canonicalStrings(errs)
}

func policyDigest(config SecurityPolicyConfig) (string, []byte, error) {
	config = canonicalSecurityPolicy(config)
	raw, err := json.Marshal(config)
	if err != nil {
		return "", nil, err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), raw, nil
}

func candidateActionsForRisk(config SecurityPolicyConfig, risk string) []string {
	switch strings.ToLower(strings.TrimSpace(risk)) {
	case "low":
		return append([]string(nil), config.Actions.Low...)
	case "medium":
		return append([]string(nil), config.Actions.Medium...)
	case "high":
		return append([]string(nil), config.Actions.High...)
	case "critical":
		return append([]string(nil), config.Actions.Critical...)
	default:
		return nil
	}
}

// effectiveCandidateActionsForRisk returns only actions that an active policy is
// allowed to publish. ModeOff and ModeAsync are observation modes: keeping the
// configured action lists lets an administrator switch the immutable draft to
// blocking later, but those lists must never leak into the action/outbox path.
func effectiveCandidateActionsForRisk(config SecurityPolicyConfig, risk string) []string {
	if config.Mode != ModeBlocking {
		return nil
	}
	return candidateActionsForRisk(config, risk)
}

func requestActionForRisk(config SecurityPolicyConfig, risk string) string {
	if config.Mode == ModeOff || config.Mode == ModeAsync {
		return "allow"
	}
	switch strings.ToLower(strings.TrimSpace(risk)) {
	case "critical", "high":
		return "block"
	case "medium":
		return "warn"
	default:
		return "allow"
	}
}

func simulatePolicy(policy *PolicyVersion, request PolicySimulationRequest) PolicySimulationResult {
	result := PolicySimulationResult{Policy: policy, Trace: []SimulationTrace{}, Warnings: []string{}}
	if policy == nil {
		result.Trace = append(result.Trace, SimulationTrace{Step: "policy", Outcome: "no_match", Detail: "没有可用策略"})
		return result
	}
	config := canonicalSecurityPolicy(policy.Config)
	result.Matched = policyScopeMatches(config.Scope, request)
	outcome := "matched"
	if !result.Matched {
		outcome = "no_match"
	}
	result.Trace = append(result.Trace, SimulationTrace{Step: "scope", Outcome: outcome, Detail: policy.PolicyKey})
	if !result.Matched {
		return result
	}
	risk := strings.ToLower(strings.TrimSpace(request.RiskLevel))
	if risk == "" {
		risk = "none"
	}
	result.RequestAction = requestActionForRisk(config, risk)
	result.CandidateActions = effectiveCandidateActionsForRisk(config, risk)
	result.Trace = append(result.Trace, SimulationTrace{Step: "risk", Outcome: risk, Detail: result.RequestAction})
	switch config.Mode {
	case ModeOff:
		result.Warnings = append(
			result.Warnings,
			"策略模式为 off：仅保留策略版本，不生成准入或持久化处置动作",
		)
	case ModeAsync:
		result.Warnings = append(
			result.Warnings,
			"策略模式为 async_audit：只记录审计结果，不阻断请求且不生成持久化处置动作",
		)
	}
	result.Trace = append(result.Trace, SimulationTrace{
		Step: "mode", Outcome: string(config.Mode),
		Detail: fmt.Sprintf("request_action=%s,candidate_actions=%d", result.RequestAction, len(result.CandidateActions)),
	})
	switch strings.TrimSpace(request.Failure) {
	case "local_error":
		result.FailureMode = config.Failure.LocalError
	case "remote_timeout":
		result.FailureMode = config.Failure.RemoteTimeout
	case "remote_invalid":
		result.FailureMode = config.Failure.RemoteInvalid
	}
	return result
}

func policyScopeMatches(scope PolicyScope, request PolicySimulationRequest) bool {
	hasIdentitySelectors := len(scope.APIKeyIDs)+len(scope.UserIDs)+len(scope.GroupIDs) > 0
	identityMatches := scope.AllGroups || !hasIdentitySelectors ||
		containsInt64(scope.APIKeyIDs, request.APIKeyID) ||
		containsInt64(scope.UserIDs, request.UserID) ||
		(request.GroupID != nil && containsInt64(scope.GroupIDs, *request.GroupID))
	if !identityMatches {
		return false
	}
	if len(scope.Protocols) > 0 && !containsFold(scope.Protocols, request.Protocol) {
		return false
	}
	if len(scope.Endpoints) > 0 && !containsFold(scope.Endpoints, request.Endpoint) {
		return false
	}
	if len(scope.Models) > 0 && !containsFold(scope.Models, request.Model) {
		return false
	}
	return true
}

func validatePolicyTransition(current, target string) error {
	allowed := map[string]map[string]bool{
		PolicyStatusDraft:     {PolicyStatusValidated: true},
		PolicyStatusValidated: {PolicyStatusShadow: true, PolicyStatusActive: true, PolicyStatusRetired: true},
		PolicyStatusShadow:    {PolicyStatusActive: true, PolicyStatusRetired: true},
		PolicyStatusActive:    {PolicyStatusRetired: true},
		PolicyStatusRetired:   {PolicyStatusActive: true},
	}
	if !allowed[current][target] {
		return fmt.Errorf("不允许从 %s 转换为 %s", current, target)
	}
	return nil
}

func validateException(request CreateExceptionRequest, now time.Time) error {
	request.Name = strings.TrimSpace(request.Name)
	request.ScopeType = strings.TrimSpace(request.ScopeType)
	request.ScopeID = strings.TrimSpace(request.ScopeID)
	request.Reason = strings.TrimSpace(request.Reason)
	if len([]rune(request.Name)) < 2 || len([]rune(request.Name)) > 160 {
		return errors.New("例外名称长度必须为 2-160")
	}
	validScope := map[string]bool{"user": true, "api_key": true, "group": true, "model": true, "endpoint": true, "detector": true, "category": true}
	if !validScope[request.ScopeType] || request.ScopeID == "" {
		return errors.New("例外作用域无效")
	}
	if len([]rune(request.Reason)) < 3 || len([]rune(request.Reason)) > 512 {
		return errors.New("例外原因长度必须为 3-512")
	}
	if request.Effect != "allow_and_record" && request.Effect != "warn_only" {
		return errors.New("例外效果无效")
	}
	start := now
	if request.StartsAt != nil {
		start = *request.StartsAt
	}
	if request.Permanent {
		if request.ExpiresAt != nil {
			return errors.New("永久例外不能设置到期时间")
		}
		return nil
	}
	if request.ExpiresAt == nil || !request.ExpiresAt.After(start) {
		return errors.New("临时例外必须设置有效到期时间")
	}
	if request.ExpiresAt.Sub(start) > 30*24*time.Hour {
		return errors.New("临时例外最长 30 天；更长时间请显式选择永久例外")
	}
	return nil
}

func canonicalPositiveInt64s(values []int64) []int64 {
	seen := make(map[int64]struct{}, len(values))
	out := make([]int64, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func canonicalStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func canonicalActions(values []string) []string {
	for i := range values {
		values[i] = strings.ToLower(strings.TrimSpace(values[i]))
	}
	return canonicalStrings(values)
}

func containsInt64(values []int64, target int64) bool {
	if target <= 0 {
		return false
	}
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func containsFold(values []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, value := range values {
		if strings.EqualFold(value, target) {
			return true
		}
	}
	return false
}
