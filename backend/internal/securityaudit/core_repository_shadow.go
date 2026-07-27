package securityaudit

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

const (
	securityAuditShadowEvaluationLock int64 = 579147893221901925
	securityAuditShadowDecisionBatch        = 500
)

type shadowDecision struct {
	replayDecision
	DetectorSummary []DetectorEvidence
}

// EvaluateShadowPolicies evaluates newly ingested unified decisions against all
// currently shadowed policy versions. It only writes comparison records and
// never creates enforcement actions.
func (r *PostgreSQLRepository) EvaluateShadowPolicies(ctx context.Context) (int64, error) {
	if r == nil || r.db == nil {
		return 0, errors.New("security audit database unavailable")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	var locked bool
	if err = tx.QueryRowContext(
		ctx,
		`SELECT pg_try_advisory_xact_lock($1)`,
		securityAuditShadowEvaluationLock,
	).Scan(&locked); err != nil {
		return 0, err
	}
	if !locked {
		return 0, nil
	}

	var cursor int64
	if err = tx.QueryRowContext(ctx, `
SELECT last_decision_pk
FROM security_audit_shadow_watermark
WHERE id=1
FOR UPDATE`).Scan(&cursor); err != nil {
		return 0, err
	}
	policies, err := listShadowPolicies(ctx, tx)
	if err != nil {
		return 0, err
	}
	decisions, err := listShadowDecisions(ctx, tx, cursor, securityAuditShadowDecisionBatch)
	if err != nil {
		return 0, err
	}
	if len(decisions) == 0 {
		if _, err = tx.ExecContext(ctx, `
UPDATE security_audit_shadow_watermark
SET last_evaluated_at=NOW(),last_error='',updated_at=NOW()
WHERE id=1`); err != nil {
			return 0, err
		}
		return 0, tx.Commit()
	}

	for _, decision := range decisions {
		for _, policy := range policies {
			if policy.ShadowedAt == nil || decision.CreatedAt.Before(*policy.ShadowedAt) {
				continue
			}
			request := PolicySimulationRequest{
				UserID: decision.UserID.Int64, APIKeyID: decision.APIKeyID.Int64,
				GroupID: nullableReplayID(decision.GroupID), Protocol: decision.Protocol,
				Endpoint: decision.Endpoint, Model: decision.RequestedModel,
				RiskLevel: decision.RiskLevel,
			}
			config := canonicalSecurityPolicy(policy.Config)
			if !policyScopeMatches(config.Scope, request) {
				continue
			}
			proposedActions := effectiveCandidateActionsForRisk(config, decision.RiskLevel)
			proposedActions, err = applyActiveExceptionToActions(
				ctx,
				tx,
				promptSnapshotFromShadowDecision(decision),
				decision.DetectorSummary,
				proposedActions,
			)
			if err != nil {
				return 0, err
			}
			baselineActions := canonicalActions(decision.CandidateActions)
			proposedActions = canonicalActions(proposedActions)
			baselineRaw, marshalErr := json.Marshal(baselineActions)
			if marshalErr != nil {
				return 0, marshalErr
			}
			proposedRaw, marshalErr := json.Marshal(proposedActions)
			if marshalErr != nil {
				return 0, marshalErr
			}
			baselineRequestAction := normalizedRequestAction(decision.RequestAction)
			proposedRequestAction := normalizedRequestAction(requestActionForRisk(config, decision.RiskLevel))
			_, execErr := tx.ExecContext(ctx, `
INSERT INTO security_audit_shadow_evaluations(
    decision_pk,policy_version_id,policy_key,policy_version,risk_level,
    baseline_request_action,proposed_request_action,baseline_actions,proposed_actions,
    request_action_changed,actions_changed
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9::jsonb,$10,$11)
ON CONFLICT (decision_pk,policy_version_id) DO NOTHING`,
				decision.ID, policy.ID, policy.PolicyKey, policy.Version,
				normalizeRiskLevel(decision.RiskLevel),
				baselineRequestAction, proposedRequestAction, baselineRaw, proposedRaw,
				baselineRequestAction != proposedRequestAction,
				!sameStringSet(baselineActions, proposedActions),
			)
			if execErr != nil {
				return 0, execErr
			}
		}
	}

	lastDecision := decisions[len(decisions)-1]
	if _, err = tx.ExecContext(ctx, `
UPDATE security_audit_shadow_watermark
SET last_decision_pk=$1,last_evaluated_at=NOW(),last_error='',updated_at=NOW()
WHERE id=1`, lastDecision.ID); err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	// The maintenance scheduler needs consumed decisions, not comparison count:
	// a full page may legitimately have no matching shadow policy.
	return int64(len(decisions)), nil
}

func (r *PostgreSQLRepository) RecordShadowEvaluationError(ctx context.Context, cause error) {
	if r == nil || r.db == nil || cause == nil {
		return
	}
	if _, err := r.db.ExecContext(ctx, `
UPDATE security_audit_shadow_watermark
SET last_error=$1,updated_at=NOW()
WHERE id=1`, TrimRunes(cause.Error(), 512)); err != nil {
		LogWarn("security_audit.shadow_error_record_failed", map[string]any{
			"status": "failed", "error_code": "shadow_watermark_update_failed",
		})
	}
}

func (r *PostgreSQLRepository) ListPolicyShadowEvaluations(
	ctx context.Context,
	policyKey string,
	version, windowHours int64,
	limit int,
) (*PolicyShadowEvaluationSummary, error) {
	policyKey = strings.TrimSpace(policyKey)
	if windowHours < 1 {
		windowHours = 24 * 7
	}
	if windowHours > 24*90 {
		windowHours = 24 * 90
	}
	if limit < 1 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	summary := &PolicyShadowEvaluationSummary{
		PolicyKey: policyKey, PolicyVersion: version, WindowHours: windowHours,
		Items: []PolicyShadowEvaluation{},
	}
	var lastEvaluatedAt sql.NullTime
	err := r.db.QueryRowContext(ctx, `
SELECT
    COUNT(e.id),
    COUNT(e.id) FILTER (WHERE e.request_action_changed),
    COUNT(e.id) FILTER (WHERE e.actions_changed),
    COUNT(e.id) FILTER (
        WHERE e.request_action_changed
          AND CASE e.proposed_request_action WHEN 'block' THEN 3 WHEN 'warn' THEN 2 ELSE 1 END
              > CASE e.baseline_request_action WHEN 'block' THEN 3 WHEN 'warn' THEN 2 ELSE 1 END
    ),
    COUNT(e.id) FILTER (
        WHERE e.request_action_changed
          AND CASE e.proposed_request_action WHEN 'block' THEN 3 WHEN 'warn' THEN 2 ELSE 1 END
              < CASE e.baseline_request_action WHEN 'block' THEN 3 WHEN 'warn' THEN 2 ELSE 1 END
    ),
    COUNT(e.id) FILTER (WHERE NOT e.request_action_changed AND NOT e.actions_changed),
    w.last_decision_pk,w.last_evaluated_at,w.last_error
FROM security_audit_policy_versions p
CROSS JOIN security_audit_shadow_watermark w
LEFT JOIN security_audit_shadow_evaluations e
  ON e.policy_version_id=p.id
 AND e.created_at>=NOW()-($3 * INTERVAL '1 hour')
WHERE p.policy_key=$1 AND p.version=$2
GROUP BY w.last_decision_pk,w.last_evaluated_at,w.last_error`,
		policyKey, version, windowHours,
	).Scan(
		&summary.Total, &summary.RequestActionChanges, &summary.CandidateActionChanges,
		&summary.StricterChanges, &summary.LooserChanges, &summary.Unchanged,
		&summary.LastEvaluatedDecisionPK, &lastEvaluatedAt, &summary.LastError,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrPolicyNotFound
	}
	if err != nil {
		return nil, err
	}
	summary.LastEvaluatedAt = nullableTime(lastEvaluatedAt)

	rows, err := r.db.QueryContext(ctx, `
SELECT
    e.id,e.decision_pk,d.decision_id,d.source_type,e.policy_version_id,
    e.policy_key,e.policy_version,e.risk_level,e.baseline_request_action,
    e.proposed_request_action,e.baseline_actions,e.proposed_actions,
    e.request_action_changed,e.actions_changed,e.created_at,d.created_at,d.detector_summary
FROM security_audit_shadow_evaluations e
JOIN security_audit_decisions d ON d.id=e.decision_pk
JOIN security_audit_policy_versions p ON p.id=e.policy_version_id
WHERE p.policy_key=$1 AND p.version=$2
  AND e.created_at>=NOW()-($3 * INTERVAL '1 hour')
ORDER BY e.created_at DESC,e.id DESC
LIMIT $4`, policyKey, version, windowHours, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var item PolicyShadowEvaluation
		var baselineRaw, proposedRaw, detectorRaw []byte
		if err := rows.Scan(
			&item.ID, &item.DecisionPK, &item.DecisionID, &item.SourceType,
			&item.PolicyVersionID, &item.PolicyKey, &item.PolicyVersion,
			&item.RiskLevel, &item.BaselineRequestAction, &item.ProposedRequestAction,
			&baselineRaw, &proposedRaw, &item.RequestActionChanged,
			&item.ActionsChanged, &item.CreatedAt, &item.DecisionCreatedAt, &detectorRaw,
		); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(baselineRaw, &item.BaselineActions); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(proposedRaw, &item.ProposedActions); err != nil {
			return nil, err
		}
		item.DetectorSummary = append(json.RawMessage(nil), detectorRaw...)
		summary.Items = append(summary.Items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return summary, nil
}

func listShadowPolicies(ctx context.Context, queryer sqlRowsQueryer) ([]*PolicyVersion, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT `+policyColumns("p")+`
FROM security_audit_policy_versions p
WHERE p.status='shadow'
ORDER BY p.priority DESC,p.policy_key,p.version DESC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]*PolicyVersion, 0)
	for rows.Next() {
		item, scanErr := scanPolicy(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		item.Config = canonicalSecurityPolicy(item.Config)
		items = append(items, item)
	}
	return items, rows.Err()
}

func listShadowDecisions(
	ctx context.Context,
	queryer sqlRowsQueryer,
	cursor int64,
	limit int,
) ([]shadowDecision, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT id,decision_id,source_type,user_id,api_key_id,group_id,protocol,endpoint,
       requested_model,risk_level,request_action,candidate_actions,created_at,detector_summary
FROM security_audit_decisions
WHERE id>$1
ORDER BY id
LIMIT $2`, cursor, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]shadowDecision, 0, limit)
	for rows.Next() {
		var item shadowDecision
		var candidatesRaw, detectorsRaw []byte
		if err := rows.Scan(
			&item.ID, &item.DecisionID, &item.SourceType, &item.UserID, &item.APIKeyID,
			&item.GroupID, &item.Protocol, &item.Endpoint, &item.RequestedModel,
			&item.RiskLevel, &item.RequestAction, &candidatesRaw, &item.CreatedAt,
			&detectorsRaw,
		); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(candidatesRaw, &item.CandidateActions); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(detectorsRaw, &item.DetectorSummary); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func promptSnapshotFromShadowDecision(decision shadowDecision) PromptSnapshot {
	return PromptSnapshot{
		UserID: decision.UserID.Int64, APIKeyID: decision.APIKeyID.Int64,
		GroupID: nullableReplayID(decision.GroupID), Protocol: decision.Protocol,
		Endpoint: decision.Endpoint, Model: decision.RequestedModel,
	}
}

func normalizedRequestAction(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "warn":
		return "warn"
	case "block":
		return "block"
	default:
		return "allow"
	}
}

func normalizeRiskLevel(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "low", "medium", "high", "critical":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "none"
	}
}
