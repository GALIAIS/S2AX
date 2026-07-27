package securityaudit

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

type SecurityAuditCoreService interface {
	SecurityOverview(context.Context, int64) (*SecurityAuditOverview, error)
	ListPolicies(context.Context) ([]PolicySummary, error)
	CreatePolicy(context.Context, CreatePolicyRequest, int64) (*PolicyVersion, error)
	ListPolicyVersions(context.Context, string) ([]*PolicyVersion, error)
	ListPolicyTransitions(context.Context, string, int) ([]PolicyTransition, error)
	ListPolicyShadowEvaluations(context.Context, string, int64, int64, int) (*PolicyShadowEvaluationSummary, error)
	ValidatePolicy(context.Context, string, int64, int64) (*PolicyVersion, error)
	SimulatePolicy(context.Context, string, int64, PolicySimulationRequest) (*PolicySimulationResult, error)
	ReplayPolicy(context.Context, string, int64, PolicyReplayRequest) (*PolicyReplayResult, error)
	ShadowPolicy(context.Context, string, int64, int64, string) (*PolicyVersion, error)
	ActivatePolicy(context.Context, string, int64, int64, string) (*PolicyVersion, error)
	RollbackPolicy(context.Context, string, int64, int64, string) (*PolicyVersion, error)
	ListUnifiedDecisions(context.Context, DecisionFilter, int, int) (*DecisionPage, error)
	GetUnifiedDecision(context.Context, int64) (*UnifiedDecision, error)
	RevealUnifiedEvidence(context.Context, int64, int64, string) (*EvidenceReveal, error)
	OpenDecisionCase(context.Context, int64, int64, string) (*AuditCase, error)
	AddDecisionFeedback(context.Context, int64, int64, FeedbackRequest) (map[string]any, error)
	ListActions(context.Context, ActionFilter, int, int) (*ActionPage, error)
	RetryAction(context.Context, int64, int64) (*EnforcementAction, error)
	CancelAction(context.Context, int64, int64) (*EnforcementAction, error)
	RevertAction(context.Context, int64, int64) (*EnforcementAction, error)
	ListCases(context.Context, CaseFilter, int, int) (*CasePage, error)
	GetCase(context.Context, int64) (*AuditCase, error)
	TransitionCase(context.Context, int64, int64, CaseTransitionRequest) (*AuditCase, error)
	ListExceptions(context.Context, bool) ([]*AuditException, error)
	CreateException(context.Context, CreateExceptionRequest, int64) (*AuditException, error)
	ExpireException(context.Context, int64, int64, ExpireExceptionRequest) (*AuditException, error)
	ListEndpointHealth(context.Context) ([]EndpointHealth, error)
	ResetEndpointBreaker(context.Context, string) (*EndpointHealth, error)
	ListBehaviorSignals(context.Context, BehaviorSignalFilter, int, int) (*BehaviorSignalPage, error)
	ListSecurityAuditNotifications(context.Context, string, string, int) ([]*SecurityAuditNotification, error)
	UpdateSecurityAuditNotificationStatus(context.Context, int64, string) (*SecurityAuditNotification, error)
	MarkAllSecurityAuditNotificationsRead(context.Context, string) (int64, error)
	ListUserSecurityAuditNotifications(context.Context, int64, string, int) ([]UserSecurityAuditNotification, error)
	UpdateUserSecurityAuditNotificationStatus(context.Context, int64, int64, string) (*UserSecurityAuditNotification, error)
	MarkAllUserSecurityAuditNotificationsRead(context.Context, int64) (int64, error)
}

func (s *PromptService) SecurityOverview(ctx context.Context, windowHours int64) (*SecurityAuditOverview, error) {
	return s.repo.SecurityAuditOverview(ctx, windowHours)
}

func (s *PromptService) ListPolicies(ctx context.Context) ([]PolicySummary, error) {
	return s.repo.ListPolicySummaries(ctx)
}

func (s *PromptService) CreatePolicy(ctx context.Context, request CreatePolicyRequest, actorID int64) (*PolicyVersion, error) {
	return s.repo.CreatePolicyVersion(ctx, request, actorID)
}

func (s *PromptService) ListPolicyVersions(ctx context.Context, policyKey string) ([]*PolicyVersion, error) {
	return s.repo.ListPolicyVersions(ctx, policyKey)
}

func (s *PromptService) ListPolicyTransitions(
	ctx context.Context,
	policyKey string,
	limit int,
) ([]PolicyTransition, error) {
	return s.repo.ListPolicyTransitions(ctx, policyKey, limit)
}

func (s *PromptService) ListPolicyShadowEvaluations(
	ctx context.Context,
	policyKey string,
	version, windowHours int64,
	limit int,
) (*PolicyShadowEvaluationSummary, error) {
	return s.repo.ListPolicyShadowEvaluations(ctx, policyKey, version, windowHours, limit)
}

func (s *PromptService) ValidatePolicy(ctx context.Context, policyKey string, version, actorID int64) (*PolicyVersion, error) {
	policy, err := s.repo.GetPolicyVersion(ctx, policyKey, version)
	if err != nil {
		return nil, err
	}
	policy.ValidationErrors = validateSecurityPolicy(policy.PolicyKey, policy.Config)
	if len(policy.ValidationErrors) > 0 {
		return policy, errors.New("策略校验失败: " + strings.Join(policy.ValidationErrors, "; "))
	}
	if policy.Status == PolicyStatusDraft {
		return s.repo.TransitionPolicy(ctx, policyKey, version, PolicyStatusValidated, actorID, "validated")
	}
	return policy, nil
}

func (s *PromptService) SimulatePolicy(ctx context.Context, policyKey string, version int64, request PolicySimulationRequest) (*PolicySimulationResult, error) {
	policy, err := s.repo.GetPolicyVersion(ctx, policyKey, version)
	if err != nil {
		return nil, err
	}
	result := simulatePolicy(policy, request)
	return &result, nil
}

func (s *PromptService) ReplayPolicy(ctx context.Context, policyKey string, version int64, request PolicyReplayRequest) (*PolicyReplayResult, error) {
	policy, err := s.repo.GetPolicyVersion(ctx, policyKey, version)
	if err != nil {
		return nil, err
	}
	if validationErrors := validateSecurityPolicy(policy.PolicyKey, policy.Config); len(validationErrors) > 0 {
		return nil, errors.New("策略校验失败: " + strings.Join(validationErrors, "; "))
	}
	return s.repo.ReplayPolicy(ctx, policy, request)
}

func (s *PromptService) ShadowPolicy(ctx context.Context, policyKey string, version, actorID int64, reason string) (*PolicyVersion, error) {
	if !validPolicyTransitionReason(reason) {
		return nil, ErrPolicyReasonInvalid
	}
	policy, err := s.ensureValidatedPolicy(ctx, policyKey, version, actorID)
	if err != nil {
		return nil, err
	}
	if policy.Status == PolicyStatusShadow {
		return policy, nil
	}
	return s.repo.TransitionPolicy(ctx, policyKey, version, PolicyStatusShadow, actorID, reason)
}

func (s *PromptService) ActivatePolicy(ctx context.Context, policyKey string, version, actorID int64, reason string) (*PolicyVersion, error) {
	if !validPolicyTransitionReason(reason) {
		return nil, ErrPolicyReasonInvalid
	}
	policy, err := s.ensureValidatedPolicy(ctx, policyKey, version, actorID)
	if err != nil {
		return nil, err
	}
	if policy.Status == PolicyStatusActive {
		return policy, nil
	}
	return s.repo.TransitionPolicy(ctx, policyKey, version, PolicyStatusActive, actorID, reason)
}

func (s *PromptService) RollbackPolicy(ctx context.Context, policyKey string, version, actorID int64, reason string) (*PolicyVersion, error) {
	if !validPolicyTransitionReason(reason) {
		return nil, ErrPolicyReasonInvalid
	}
	policy, err := s.repo.GetPolicyVersion(ctx, policyKey, version)
	if err != nil {
		return nil, err
	}
	if len(validateSecurityPolicy(policy.PolicyKey, policy.Config)) > 0 {
		return nil, errors.New("目标历史版本未通过当前校验，不能回滚")
	}
	if policy.Status != PolicyStatusRetired && policy.Status != PolicyStatusValidated && policy.Status != PolicyStatusShadow {
		return nil, ErrInvalidTransition
	}
	return s.repo.TransitionPolicy(ctx, policyKey, version, PolicyStatusActive, actorID, reason)
}

func (s *PromptService) ensureValidatedPolicy(ctx context.Context, policyKey string, version, actorID int64) (*PolicyVersion, error) {
	policy, err := s.repo.GetPolicyVersion(ctx, policyKey, version)
	if err != nil {
		return nil, err
	}
	if errorsFound := validateSecurityPolicy(policy.PolicyKey, policy.Config); len(errorsFound) > 0 {
		return nil, errors.New("策略校验失败: " + strings.Join(errorsFound, "; "))
	}
	if policy.Status == PolicyStatusDraft {
		return s.repo.TransitionPolicy(ctx, policyKey, version, PolicyStatusValidated, actorID, "validated before transition")
	}
	return policy, nil
}

func validPolicyTransitionReason(reason string) bool {
	length := len([]rune(strings.TrimSpace(reason)))
	return length >= 3 && length <= 512
}

func (s *PromptService) ListUnifiedDecisions(ctx context.Context, filter DecisionFilter, page, pageSize int) (*DecisionPage, error) {
	return s.repo.ListUnifiedDecisions(ctx, filter, page, pageSize)
}

func (s *PromptService) GetUnifiedDecision(ctx context.Context, id int64) (*UnifiedDecision, error) {
	return s.repo.GetUnifiedDecision(ctx, id)
}

func (s *PromptService) RevealUnifiedEvidence(ctx context.Context, decisionPK, adminID int64, reason string) (*EvidenceReveal, error) {
	eventID, err := s.repo.FindPromptEventIDForDecision(ctx, decisionPK)
	if err != nil {
		return nil, errors.Join(err, s.recordUnifiedEvidenceAccess(ctx, decisionPK, adminID, reason, evidenceOutcome(err)))
	}
	result, err := s.RevealEventEvidence(ctx, eventID, adminID, reason)
	if auditErr := s.recordUnifiedEvidenceAccess(ctx, decisionPK, adminID, reason, evidenceOutcome(err)); auditErr != nil {
		if err != nil {
			return nil, errors.Join(err, auditErr)
		}
		return nil, fmt.Errorf("record unified evidence access before reveal: %w", auditErr)
	}
	return result, err
}

func (s *PromptService) recordUnifiedEvidenceAccess(
	ctx context.Context,
	decisionPK, adminID int64,
	reason, outcome string,
) error {
	auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	return s.repo.RecordUnifiedEvidenceAccess(auditCtx, decisionPK, adminID, reason, outcome)
}

func evidenceOutcome(err error) string {
	switch {
	case err == nil:
		return "revealed"
	case errors.Is(err, ErrEvidenceExpired):
		return "expired"
	case errors.Is(err, ErrEvidenceUnavailable), errors.Is(err, ErrDecisionNotFound):
		return "unavailable"
	default:
		return "decrypt_failed"
	}
}

func (s *PromptService) OpenDecisionCase(ctx context.Context, decisionPK, actorID int64, reason string) (*AuditCase, error) {
	return s.repo.OpenCaseForDecision(ctx, decisionPK, actorID, reason)
}

func (s *PromptService) AddDecisionFeedback(ctx context.Context, decisionPK, actorID int64, request FeedbackRequest) (map[string]any, error) {
	return s.repo.CreateFeedback(ctx, decisionPK, actorID, request)
}

func (s *PromptService) ListActions(ctx context.Context, filter ActionFilter, page, pageSize int) (*ActionPage, error) {
	return s.repo.ListActions(ctx, filter, page, pageSize)
}

func (s *PromptService) RetryAction(ctx context.Context, id, actorID int64) (*EnforcementAction, error) {
	return s.repo.RetryAction(ctx, id, actorID)
}

func (s *PromptService) CancelAction(ctx context.Context, id, actorID int64) (*EnforcementAction, error) {
	return s.repo.CancelAction(ctx, id, actorID)
}

func (s *PromptService) RevertAction(ctx context.Context, id, actorID int64) (*EnforcementAction, error) {
	return s.repo.RevertAction(ctx, id, actorID)
}

func (s *PromptService) ListCases(ctx context.Context, filter CaseFilter, page, pageSize int) (*CasePage, error) {
	return s.repo.ListCases(ctx, filter, page, pageSize)
}

func (s *PromptService) GetCase(ctx context.Context, id int64) (*AuditCase, error) {
	return s.repo.GetCase(ctx, id)
}

func (s *PromptService) TransitionCase(ctx context.Context, id, actorID int64, request CaseTransitionRequest) (*AuditCase, error) {
	return s.repo.TransitionCase(ctx, id, actorID, request)
}

func (s *PromptService) ListExceptions(ctx context.Context, includeInactive bool) ([]*AuditException, error) {
	return s.repo.ListExceptions(ctx, includeInactive)
}

func (s *PromptService) CreateException(ctx context.Context, request CreateExceptionRequest, actorID int64) (*AuditException, error) {
	return s.repo.CreateException(ctx, request, actorID)
}

func (s *PromptService) ExpireException(ctx context.Context, id, actorID int64, request ExpireExceptionRequest) (*AuditException, error) {
	reason := strings.TrimSpace(request.Reason)
	if !validPolicyTransitionReason(reason) {
		return nil, ErrExceptionReasonInvalid
	}
	return s.repo.ExpireException(ctx, id, actorID, reason)
}

func (s *PromptService) ListEndpointHealth(ctx context.Context) ([]EndpointHealth, error) {
	return s.repo.ListEndpointHealth(ctx)
}

func (s *PromptService) ResetEndpointBreaker(ctx context.Context, endpointID string) (*EndpointHealth, error) {
	return s.repo.ResetEndpointBreaker(ctx, endpointID)
}

func (s *PromptService) ListBehaviorSignals(
	ctx context.Context,
	filter BehaviorSignalFilter,
	page, pageSize int,
) (*BehaviorSignalPage, error) {
	return s.repo.ListBehaviorSignals(ctx, filter, page, pageSize)
}

func (s *PromptService) ListSecurityAuditNotifications(
	ctx context.Context,
	status, audience string,
	limit int,
) ([]*SecurityAuditNotification, error) {
	return s.repo.ListSecurityAuditNotifications(ctx, status, audience, limit)
}

func (s *PromptService) UpdateSecurityAuditNotificationStatus(
	ctx context.Context,
	id int64,
	status string,
) (*SecurityAuditNotification, error) {
	return s.repo.UpdateSecurityAuditNotificationStatus(ctx, id, status)
}

func (s *PromptService) MarkAllSecurityAuditNotificationsRead(
	ctx context.Context,
	audience string,
) (int64, error) {
	return s.repo.MarkAllSecurityAuditNotificationsRead(ctx, audience)
}

func (s *PromptService) ListUserSecurityAuditNotifications(
	ctx context.Context,
	userID int64,
	status string,
	limit int,
) ([]UserSecurityAuditNotification, error) {
	notifications, err := s.repo.ListUserSecurityAuditNotifications(ctx, userID, status, limit)
	if err != nil {
		return nil, err
	}
	result := make([]UserSecurityAuditNotification, 0, len(notifications))
	for _, notification := range notifications {
		result = append(result, notification.UserView())
	}
	return result, nil
}

func (s *PromptService) UpdateUserSecurityAuditNotificationStatus(
	ctx context.Context,
	userID, id int64,
	status string,
) (*UserSecurityAuditNotification, error) {
	notification, err := s.repo.UpdateUserSecurityAuditNotificationStatus(ctx, userID, id, status)
	if err != nil {
		return nil, err
	}
	result := notification.UserView()
	return &result, nil
}

func (s *PromptService) MarkAllUserSecurityAuditNotificationsRead(ctx context.Context, userID int64) (int64, error) {
	return s.repo.MarkAllUserSecurityAuditNotificationsRead(ctx, userID)
}
