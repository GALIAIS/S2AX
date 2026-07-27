package securityaudit

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"
)

func insertUnifiedPromptDecision(
	ctx context.Context,
	queryer sqlQueryer,
	event *Event,
	snapshot PromptSnapshot,
	result *NormalizedResult,
) error {
	if event == nil || result == nil {
		return nil
	}
	risk, requestAction := unifiedRiskAndAction(result)
	policyKey := strings.TrimSpace(result.PolicyID)
	if policyKey == "" {
		policyKey = "prompt-audit-config"
	}
	policyVersion := int64(result.PolicyVersion)
	if policyVersion <= 0 {
		policyVersion = event.ConfigVersion
	}
	if policyVersion <= 0 {
		policyVersion = 1
	}
	candidateActions := defaultCandidateActions(result)
	selectedKey, selectedVersion, selectedConfig, selected, err := resolveActivePolicyForSnapshot(ctx, queryer, snapshot)
	if err != nil {
		return fmt.Errorf("resolve active security policy: %w", err)
	}
	if selected {
		policyKey = selectedKey
		policyVersion = selectedVersion
		candidateActions = effectiveCandidateActionsForRisk(selectedConfig, risk)
	}
	evaluation := evaluationStatusOrDefault(result.EvaluationStatus)
	detectors := detectorEvidenceFromPromptResult(result)
	candidateActions, err = applyActiveExceptionToActions(ctx, queryer, snapshot, detectors, candidateActions)
	if err != nil {
		return fmt.Errorf("resolve active security exception: %w", err)
	}
	if selected {
		detectors = evidenceForPolicyMode(detectors, selectedConfig.Evidence.Mode)
		if err := enforcePromptEvidencePolicy(
			ctx,
			queryer,
			event.ID,
			event.CreatedAt,
			selectedConfig.Evidence,
		); err != nil {
			return fmt.Errorf("apply prompt evidence policy: %w", err)
		}
	}
	detectorRaw, _ := json.Marshal(detectors)
	actionsRaw, _ := json.Marshal(candidateActions)
	sourceEventID := event.ID
	auditID := newSecurityID("aud")
	decisionID := newSecurityID("dec")
	digestInput, _ := json.Marshal(map[string]any{
		"audit_id": auditID, "source_event_id": sourceEventID, "request_id": snapshot.RequestID,
		"policy_key": policyKey, "policy_version": policyVersion, "evaluation_status": evaluation,
		"risk_level": risk, "request_action": requestAction, "prompt_hash": snapshot.PromptHash,
		"detectors": detectors, "candidate_actions": candidateActions,
	})
	digest := sha256.Sum256(digestInput)
	var decisionPK int64
	var persistedDecisionID string
	err = queryer.QueryRowContext(ctx, `
INSERT INTO security_audit_decisions(
    decision_id,audit_id,source_type,source_event_id,request_id,stage,
    user_id,user_snapshot,api_key_id,api_key_snapshot,group_id,group_snapshot,
    provider,endpoint,protocol,requested_model,policy_key,policy_version,
    canonicalizer_version,evaluation_status,risk_level,request_action,
    failure_mode,failure_reason,prompt_hash,redacted_preview,detector_summary,
    candidate_actions,decision_digest
) VALUES (
    $1,$2,'prompt_audit',$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,
    'prompt-v2',$18,$19,$20,$21,$22,$23,$24,$25::jsonb,$26::jsonb,$27
)
ON CONFLICT (source_type,source_event_id)
DO UPDATE SET
    policy_key=EXCLUDED.policy_key,
    policy_version=EXCLUDED.policy_version,
    evaluation_status=EXCLUDED.evaluation_status,
    risk_level=EXCLUDED.risk_level,
    request_action=EXCLUDED.request_action,
    failure_mode=EXCLUDED.failure_mode,
    failure_reason=EXCLUDED.failure_reason,
    detector_summary=EXCLUDED.detector_summary,
    candidate_actions=EXCLUDED.candidate_actions,
    decision_digest=EXCLUDED.decision_digest
RETURNING id,decision_id`,
		decisionID, auditID, sourceEventID, snapshot.RequestID, normalizeStage(snapshot.Stage),
		nullableID(snapshot.UserID), snapshot.UserEmailSnapshot, nullableID(snapshot.APIKeyID),
		snapshot.APIKeyNameSnapshot, snapshot.GroupID, snapshot.GroupName,
		snapshot.Provider, snapshot.Endpoint, snapshot.Protocol, snapshot.Model,
		policyKey, policyVersion, evaluation, risk, requestAction,
		string(result.FailureMode), TrimRunes(strings.TrimSpace(result.FailureReason), 96),
		snapshot.PromptHash, snapshot.RedactedPreview, detectorRaw, actionsRaw,
		hex.EncodeToString(digest[:]),
	).Scan(&decisionPK, &persistedDecisionID)
	if err != nil {
		return fmt.Errorf("insert unified decision: %w", err)
	}
	if err := insertUnifiedEvidence(ctx, queryer, decisionPK, detectors); err != nil {
		return fmt.Errorf("insert unified evidence: %w", err)
	}
	for _, action := range candidateActions {
		if action == "pause_api_key" && snapshot.APIKeyID <= 0 {
			continue
		}
		if (action == "pause_user" || action == "notify_user") && snapshot.UserID <= 0 {
			continue
		}
		if err := insertUnifiedAction(ctx, queryer, decisionPK, persistedDecisionID, action, snapshot, risk); err != nil {
			return fmt.Errorf("insert unified action %q: %w", action, err)
		}
	}
	return nil
}

func resolveActivePolicyForSnapshot(
	ctx context.Context,
	queryer sqlQueryer,
	snapshot PromptSnapshot,
) (string, int64, SecurityPolicyConfig, bool, error) {
	var policyKey string
	var version int64
	var raw []byte
	err := queryer.QueryRowContext(ctx, `
SELECT p.policy_key,p.version,p.config
FROM security_audit_policy_versions p
WHERE p.status='active'
  AND (
      COALESCE((p.config#>>'{scope,all_groups}')::boolean,FALSE)
      OR (
          jsonb_array_length(COALESCE(p.config#>'{scope,api_key_ids}','[]'::jsonb))=0
          AND jsonb_array_length(COALESCE(p.config#>'{scope,user_ids}','[]'::jsonb))=0
          AND jsonb_array_length(COALESCE(p.config#>'{scope,group_ids}','[]'::jsonb))=0
      )
      OR EXISTS (
          SELECT 1 FROM jsonb_array_elements_text(COALESCE(p.config#>'{scope,api_key_ids}','[]'::jsonb)) value
          WHERE value::bigint=$1 AND $1>0
      )
      OR EXISTS (
          SELECT 1 FROM jsonb_array_elements_text(COALESCE(p.config#>'{scope,user_ids}','[]'::jsonb)) value
          WHERE value::bigint=$2 AND $2>0
      )
      OR EXISTS (
          SELECT 1 FROM jsonb_array_elements_text(COALESCE(p.config#>'{scope,group_ids}','[]'::jsonb)) value
          WHERE value::bigint=$3 AND $3>0
      )
  )
  AND (
      jsonb_array_length(COALESCE(p.config#>'{scope,protocols}','[]'::jsonb))=0
      OR EXISTS (
          SELECT 1 FROM jsonb_array_elements_text(COALESCE(p.config#>'{scope,protocols}','[]'::jsonb)) value
          WHERE lower(value)=lower($4)
      )
  )
  AND (
      jsonb_array_length(COALESCE(p.config#>'{scope,endpoints}','[]'::jsonb))=0
      OR EXISTS (
          SELECT 1 FROM jsonb_array_elements_text(COALESCE(p.config#>'{scope,endpoints}','[]'::jsonb)) value
          WHERE lower(value)=lower($5)
      )
  )
  AND (
      jsonb_array_length(COALESCE(p.config#>'{scope,models}','[]'::jsonb))=0
      OR EXISTS (
          SELECT 1 FROM jsonb_array_elements_text(COALESCE(p.config#>'{scope,models}','[]'::jsonb)) value
          WHERE lower(value)=lower($6)
      )
  )
ORDER BY
  CASE
    WHEN EXISTS (
        SELECT 1 FROM jsonb_array_elements_text(COALESCE(p.config#>'{scope,api_key_ids}','[]'::jsonb)) value
        WHERE value::bigint=$1 AND $1>0
    ) THEN 5
    WHEN EXISTS (
        SELECT 1 FROM jsonb_array_elements_text(COALESCE(p.config#>'{scope,user_ids}','[]'::jsonb)) value
        WHERE value::bigint=$2 AND $2>0
    ) THEN 4
    WHEN EXISTS (
        SELECT 1 FROM jsonb_array_elements_text(COALESCE(p.config#>'{scope,group_ids}','[]'::jsonb)) value
        WHERE value::bigint=$3 AND $3>0
    ) THEN 3
    ELSE 1
  END DESC,
  (
      CASE WHEN jsonb_array_length(COALESCE(p.config#>'{scope,protocols}','[]'::jsonb))>0 THEN 1 ELSE 0 END
      + CASE WHEN jsonb_array_length(COALESCE(p.config#>'{scope,endpoints}','[]'::jsonb))>0 THEN 1 ELSE 0 END
      + CASE WHEN jsonb_array_length(COALESCE(p.config#>'{scope,models}','[]'::jsonb))>0 THEN 1 ELSE 0 END
  ) DESC,
  p.priority DESC,p.policy_key,p.version DESC
LIMIT 1`,
		snapshot.APIKeyID, snapshot.UserID, int64PointerValue(snapshot.GroupID),
		snapshot.Protocol, snapshot.Endpoint, snapshot.Model,
	).Scan(&policyKey, &version, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, SecurityPolicyConfig{}, false, nil
	}
	if err != nil {
		return "", 0, SecurityPolicyConfig{}, false, err
	}
	var config SecurityPolicyConfig
	if err := json.Unmarshal(raw, &config); err != nil {
		return "", 0, SecurityPolicyConfig{}, false, fmt.Errorf("decode active policy %s v%d: %w", policyKey, version, err)
	}
	return policyKey, version, canonicalSecurityPolicy(config), true, nil
}

func applyActiveExceptionToActions(
	ctx context.Context,
	queryer sqlQueryer,
	snapshot PromptSnapshot,
	detectors []DetectorEvidence,
	actions []string,
) ([]string, error) {
	if len(actions) == 0 {
		return actions, nil
	}
	matchedDetectorIDs := make([]string, 0, len(detectors))
	matchedCategories := make([]string, 0, len(detectors))
	for _, detector := range detectors {
		if detector.Outcome != "matched" {
			continue
		}
		if value := strings.ToLower(strings.TrimSpace(detector.DetectorID)); value != "" {
			matchedDetectorIDs = append(matchedDetectorIDs, value)
		}
		if value := strings.ToLower(strings.TrimSpace(detector.Category)); value != "" {
			matchedCategories = append(matchedCategories, value)
		}
	}
	matchedDetectorIDs = canonicalStrings(matchedDetectorIDs)
	matchedCategories = canonicalStrings(matchedCategories)
	var effect string
	err := queryer.QueryRowContext(ctx, `
SELECT effect
FROM security_audit_exceptions
WHERE status='active'
  AND starts_at<=NOW()
  AND (permanent OR expires_at>NOW())
  AND (
      (scope_type='api_key' AND scope_id=$1)
      OR (scope_type='user' AND scope_id=$2)
      OR (scope_type='group' AND scope_id=$3)
      OR (scope_type='model' AND lower(scope_id)=lower($4))
      OR (scope_type='endpoint' AND lower(scope_id)=lower($5))
      OR (scope_type='detector' AND lower(scope_id)=ANY($6::text[]))
      OR (scope_type='category' AND lower(scope_id)=ANY($7::text[]))
  )
  AND (detector_id='' OR lower(detector_id)=ANY($6::text[]))
  AND (category='' OR lower(category)=ANY($7::text[]))
ORDER BY
  CASE scope_type
      WHEN 'api_key' THEN 7 WHEN 'user' THEN 6 WHEN 'group' THEN 5
      WHEN 'model' THEN 4 WHEN 'endpoint' THEN 4
      WHEN 'detector' THEN 3 WHEN 'category' THEN 2 ELSE 1
  END DESC,
  created_at DESC
LIMIT 1`,
		strconv.FormatInt(snapshot.APIKeyID, 10), strconv.FormatInt(snapshot.UserID, 10),
		strconv.FormatInt(int64PointerValue(snapshot.GroupID), 10), snapshot.Model, snapshot.Endpoint,
		pq.Array(matchedDetectorIDs), pq.Array(matchedCategories),
	).Scan(&effect)
	if errors.Is(err, sql.ErrNoRows) {
		return actions, nil
	}
	if err != nil {
		return nil, err
	}
	switch effect {
	case "allow_and_record":
		return []string{}, nil
	case "warn_only":
		out := make([]string, 0, len(actions))
		for _, action := range actions {
			if action == "notify_user" || action == "notify_admin" || action == "open_case" {
				out = append(out, action)
			}
		}
		return out, nil
	default:
		return actions, nil
	}
}

func int64PointerValue(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func evidenceForPolicyMode(evidence []DetectorEvidence, mode string) []DetectorEvidence {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "none":
		return []DetectorEvidence{}
	case "digest_only":
		out := make([]DetectorEvidence, len(evidence))
		copy(out, evidence)
		for i := range out {
			out[i].SafeSummary = ""
		}
		return out
	default:
		return evidence
	}
}

func enforcePromptEvidencePolicy(
	ctx context.Context,
	queryer sqlQueryer,
	eventID int64,
	createdAt time.Time,
	policy PolicyEvidence,
) error {
	if eventID <= 0 {
		return nil
	}
	mode := strings.ToLower(strings.TrimSpace(policy.Mode))
	var persistedID int64
	if mode != "full_encrypted" || policy.RetentionDays <= 0 {
		return queryer.QueryRowContext(ctx, `
UPDATE prompt_audit_events
SET evidence_ciphertext='',evidence_status='not_stored',evidence_expires_at=NULL
WHERE id=$1
RETURNING id`, eventID).Scan(&persistedID)
	}
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	expiresAt := createdAt.UTC().Add(time.Duration(policy.RetentionDays) * 24 * time.Hour)
	return queryer.QueryRowContext(ctx, `
UPDATE prompt_audit_events
SET evidence_expires_at=CASE
        WHEN evidence_ciphertext<>'' THEN $2
        ELSE NULL
    END
WHERE id=$1
RETURNING id`, eventID, expiresAt).Scan(&persistedID)
}

func insertUnifiedEvidence(ctx context.Context, queryer sqlQueryer, decisionPK int64, evidence []DetectorEvidence) error {
	for _, item := range evidence {
		var ignored int64
		err := queryer.QueryRowContext(ctx, `
INSERT INTO security_audit_evidence(
    decision_pk,detector_id,detector_version,outcome,category,score,severity,
    safe_summary,evidence_digest,latency_ms,error_code
) VALUES (
    $1::bigint,$2::varchar(96),$3::varchar(64),$4::varchar(24),$5::varchar(96),
    $6::double precision,$7::varchar(24),$8::text,$9::varchar(64),$10::integer,$11::varchar(96)
)
ON CONFLICT (decision_pk,detector_id,category) DO NOTHING
RETURNING id`,
			decisionPK, item.DetectorID, item.DetectorVersion, item.Outcome,
			item.Category, item.Score, item.Severity, item.SafeSummary,
			item.EvidenceDigest, item.LatencyMS, item.ErrorCode,
		).Scan(&ignored)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func insertUnifiedAction(
	ctx context.Context,
	queryer sqlQueryer,
	decisionPK int64,
	decisionID, actionType string,
	snapshot PromptSnapshot,
	risk string,
) error {
	subjectType := "request"
	subjectID := int64(0)
	if actionType == "pause_api_key" {
		subjectType, subjectID = "api_key", snapshot.APIKeyID
	} else if actionType == "pause_user" || actionType == "notify_user" {
		subjectType, subjectID = "user", snapshot.UserID
	}
	idempotencyKey := fmt.Sprintf("%s:%s:%s:%d:1", decisionID, actionType, subjectType, subjectID)
	actionID := newSecurityID("act")
	var actionPK int64
	var persistedActionID string
	err := queryer.QueryRowContext(ctx, `
INSERT INTO security_audit_actions(
    action_id,decision_pk,action_type,subject_type,subject_id,status,idempotency_key
) VALUES ($1,$2,$3,$4,$5,'pending',$6)
ON CONFLICT (idempotency_key) DO UPDATE SET idempotency_key=EXCLUDED.idempotency_key
RETURNING id,action_id`,
		actionID, decisionPK, actionType, subjectType, subjectID, idempotencyKey,
	).Scan(&actionPK, &persistedActionID)
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]any{
		"action_id": persistedActionID, "action_type": actionType, "decision_id": decisionID,
		"subject_type": subjectType, "subject_id": subjectID, "risk_level": risk,
	})
	var outboxID int64
	return queryer.QueryRowContext(ctx, `
INSERT INTO security_audit_outbox(event_id,action_id,topic,payload)
VALUES ($1,$2,'security_audit.enforcement',$3::jsonb)
ON CONFLICT (event_id) DO UPDATE SET event_id=EXCLUDED.event_id
RETURNING id`, "out_"+persistedActionID, actionPK, payload).Scan(&outboxID)
}

func detectorEvidenceFromPromptResult(result *NormalizedResult) []DetectorEvidence {
	if result == nil {
		return []DetectorEvidence{}
	}
	detectorIDs := append([]string(nil), result.MatchedScanners...)
	if len(detectorIDs) == 0 {
		detectorIDs = []string{strings.TrimSpace(result.ScannerBackend)}
	}
	detectorIDs = canonicalStrings(detectorIDs)
	if len(detectorIDs) == 0 {
		detectorIDs = []string{"prompt_guard"}
	}
	categories := canonicalStrings(result.Categories)
	if len(categories) == 0 {
		categories = []string{""}
	}
	out := make([]DetectorEvidence, 0, len(detectorIDs)*len(categories))
	for _, detectorID := range detectorIDs {
		for _, category := range categories {
			score := result.ScannerScores[category]
			summary := RedactPreview(result.ScannerEvidence[category], 240)
			digest := ""
			if summary != "" {
				sum := sha256.Sum256([]byte(summary))
				digest = hex.EncodeToString(sum[:])
			}
			outcome := "clear"
			if result.Decision != EventPass && result.Decision != EventDegraded {
				outcome = "matched"
			} else if result.Decision == EventDegraded {
				outcome = "error"
			}
			out = append(out, DetectorEvidence{
				DetectorID: detectorID, DetectorVersion: result.ScannerVersion, Outcome: outcome,
				Category: category, Score: clampScore(score), Severity: unifiedRisk(result.RiskLevel),
				SafeSummary: summary, EvidenceDigest: digest, LatencyMS: result.LatencyMS,
				ErrorCode: strings.TrimSpace(result.FailureReason),
			})
		}
	}
	return out
}

func defaultCandidateActions(result *NormalizedResult) []string {
	if result == nil || result.Decision == EventPass || result.Decision == EventDegraded {
		return []string{}
	}
	if result.Decision == EventCritical || result.RiskLevel == RiskCritical || result.RiskLevel == RiskHigh {
		return []string{"open_case"}
	}
	return []string{}
}

func unifiedRiskAndAction(result *NormalizedResult) (string, string) {
	if result == nil {
		return "low", "warn"
	}
	risk := unifiedRisk(result.RiskLevel)
	switch result.Decision {
	case EventCritical:
		return risk, "block"
	case EventFlag:
		return risk, "warn"
	case EventDegraded:
		if result.FailureMode == FailureBlockAndRecord {
			return risk, "block"
		}
		return risk, "allow"
	default:
		return "none", "allow"
	}
}

func unifiedRisk(risk RiskLevel) string {
	switch risk {
	case RiskLow, RiskMedium, RiskHigh, RiskCritical:
		return string(risk)
	default:
		return "none"
	}
}

func clampScore(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func (r *PostgreSQLRepository) ListUnifiedDecisions(ctx context.Context, filter DecisionFilter, page, pageSize int) (*DecisionPage, error) {
	page, pageSize = normalizePage(page, pageSize)
	where, args := buildDecisionWhere(filter)
	var total int64
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM security_audit_decisions d`+where, args...).Scan(&total); err != nil {
		return nil, err
	}
	limitIndex := len(args) + 1
	queryArgs := append(append([]any(nil), args...), pageSize, (page-1)*pageSize)
	rows, err := r.db.QueryContext(ctx, `
SELECT `+decisionColumns("d")+`
FROM security_audit_decisions d`+where+
		fmt.Sprintf(` ORDER BY d.created_at DESC,d.id DESC LIMIT $%d OFFSET $%d`, limitIndex, limitIndex+1),
		queryArgs...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]*UnifiedDecision, 0, pageSize)
	for rows.Next() {
		item, err := scanUnifiedDecision(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	pages := 0
	if total > 0 {
		pages = int((total + int64(pageSize) - 1) / int64(pageSize))
	}
	return &DecisionPage{Items: items, Total: total, Page: page, PageSize: pageSize, Pages: pages}, rows.Err()
}

func (r *PostgreSQLRepository) GetUnifiedDecision(ctx context.Context, id int64) (*UnifiedDecision, error) {
	item, err := scanUnifiedDecision(r.db.QueryRowContext(ctx, `
SELECT `+decisionColumns("d")+`
FROM security_audit_decisions d WHERE d.id=$1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrDecisionNotFound
	}
	if err != nil {
		return nil, err
	}
	evidence, err := r.listDecisionEvidence(ctx, id)
	if err != nil {
		return nil, err
	}
	actions, err := r.listDecisionActions(ctx, id)
	if err != nil {
		return nil, err
	}
	item.Evidence = evidence
	item.Actions = actions
	return item, nil
}

func (r *PostgreSQLRepository) listDecisionEvidence(ctx context.Context, decisionPK int64) ([]DetectorEvidence, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id,detector_id,detector_version,outcome,category,score,severity,safe_summary,
       evidence_digest,latency_ms,error_code,expires_at,hold_until,created_at
FROM security_audit_evidence
WHERE decision_pk=$1
ORDER BY id`, decisionPK)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]DetectorEvidence, 0)
	for rows.Next() {
		var item DetectorEvidence
		var expiresAt, holdUntil sql.NullTime
		if err := rows.Scan(
			&item.ID, &item.DetectorID, &item.DetectorVersion, &item.Outcome,
			&item.Category, &item.Score, &item.Severity, &item.SafeSummary,
			&item.EvidenceDigest, &item.LatencyMS, &item.ErrorCode, &expiresAt,
			&holdUntil, &item.CreatedAt,
		); err != nil {
			return nil, err
		}
		item.ExpiresAt = nullableTime(expiresAt)
		item.HoldUntil = nullableTime(holdUntil)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgreSQLRepository) listDecisionActions(ctx context.Context, decisionPK int64) ([]EnforcementAction, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT `+actionColumns("a")+`
FROM security_audit_actions a
WHERE a.decision_pk=$1 ORDER BY a.created_at,a.id`, decisionPK)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]EnforcementAction, 0)
	for rows.Next() {
		item, err := scanAction(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func decisionColumns(alias string) string {
	return alias + `.id,` + alias + `.decision_id,` + alias + `.audit_id,` + alias + `.source_type,` +
		alias + `.source_event_id,` + alias + `.request_id,` + alias + `.stage,` + alias + `.user_id,` +
		alias + `.user_snapshot,` + alias + `.api_key_id,` + alias + `.api_key_snapshot,` + alias + `.group_id,` +
		alias + `.group_snapshot,` + alias + `.provider,` + alias + `.endpoint,` + alias + `.protocol,` +
		alias + `.requested_model,` + alias + `.policy_key,` + alias + `.policy_version,` +
		alias + `.canonicalizer_version,` + alias + `.evaluation_status,` + alias + `.risk_level,` +
		alias + `.request_action,` + alias + `.failure_mode,` + alias + `.failure_reason,` +
		alias + `.prompt_hash,` + alias + `.redacted_preview,` + alias + `.detector_summary,` +
		alias + `.candidate_actions,` + alias + `.decision_digest,` + alias + `.created_at`
}

func scanUnifiedDecision(row rowScanner) (*UnifiedDecision, error) {
	item := &UnifiedDecision{}
	var sourceEventID, userID, apiKeyID, groupID sql.NullInt64
	var detectorsRaw, actionsRaw []byte
	if err := row.Scan(
		&item.ID, &item.DecisionID, &item.AuditID, &item.SourceType, &sourceEventID,
		&item.RequestID, &item.Stage, &userID, &item.UserSnapshot, &apiKeyID,
		&item.APIKeySnapshot, &groupID, &item.GroupSnapshot, &item.Provider, &item.Endpoint,
		&item.Protocol, &item.RequestedModel, &item.PolicyKey, &item.PolicyVersion,
		&item.CanonicalizerVersion, &item.EvaluationStatus, &item.RiskLevel,
		&item.RequestAction, &item.FailureMode, &item.FailureReason, &item.PromptHash,
		&item.RedactedPreview, &detectorsRaw, &actionsRaw, &item.DecisionDigest, &item.CreatedAt,
	); err != nil {
		return nil, err
	}
	if sourceEventID.Valid {
		item.SourceEventID = &sourceEventID.Int64
	}
	if userID.Valid {
		item.UserID = &userID.Int64
	}
	if apiKeyID.Valid {
		item.APIKeyID = &apiKeyID.Int64
	}
	if groupID.Valid {
		item.GroupID = &groupID.Int64
	}
	_ = json.Unmarshal(detectorsRaw, &item.DetectorSummary)
	_ = json.Unmarshal(actionsRaw, &item.CandidateActions)
	if item.DetectorSummary == nil {
		item.DetectorSummary = []DetectorEvidence{}
	}
	if item.CandidateActions == nil {
		item.CandidateActions = []string{}
	}
	return item, nil
}

func buildDecisionWhere(filter DecisionFilter) (string, []any) {
	clauses := []string{" WHERE 1=1"}
	args := make([]any, 0)
	add := func(template string, value any) {
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf(template, len(args)))
	}
	if value := strings.TrimSpace(filter.RiskLevel); value != "" {
		add(" AND d.risk_level=$%d", value)
	}
	if value := strings.TrimSpace(filter.RequestAction); value != "" {
		add(" AND d.request_action=$%d", value)
	}
	if value := strings.TrimSpace(filter.EvaluationStatus); value != "" {
		add(" AND d.evaluation_status=$%d", value)
	}
	if value := strings.TrimSpace(filter.SourceType); value != "" {
		add(" AND d.source_type=$%d", value)
	}
	if filter.UserID != nil {
		add(" AND d.user_id=$%d", *filter.UserID)
	}
	if filter.APIKeyID != nil {
		add(" AND d.api_key_id=$%d", *filter.APIKeyID)
	}
	if filter.GroupID != nil {
		add(" AND d.group_id=$%d", *filter.GroupID)
	}
	if value := strings.TrimSpace(filter.PolicyKey); value != "" {
		add(" AND d.policy_key=$%d", value)
	}
	if value := strings.TrimSpace(filter.Keyword); value != "" {
		add(" AND (d.request_id ILIKE $%d OR d.user_snapshot ILIKE $%d OR d.api_key_snapshot ILIKE $%d OR d.redacted_preview ILIKE $%d)", "%"+value+"%")
		index := len(args)
		clauses[len(clauses)-1] = fmt.Sprintf(
			" AND (d.request_id ILIKE $%d OR d.user_snapshot ILIKE $%d OR d.api_key_snapshot ILIKE $%d OR d.redacted_preview ILIKE $%d)",
			index, index, index, index,
		)
	}
	if filter.StartAt != nil {
		add(" AND d.created_at >= $%d", *filter.StartAt)
	}
	if filter.EndAt != nil {
		add(" AND d.created_at < $%d", *filter.EndAt)
	}
	return strings.Join(clauses, ""), args
}

func normalizePage(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

func (r *PostgreSQLRepository) FindPromptEventIDForDecision(ctx context.Context, decisionPK int64) (int64, error) {
	var eventID sql.NullInt64
	err := r.db.QueryRowContext(ctx, `
SELECT source_event_id
FROM security_audit_decisions
WHERE id=$1 AND source_type='prompt_audit'`, decisionPK).Scan(&eventID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrDecisionNotFound
	}
	if err != nil {
		return 0, err
	}
	if !eventID.Valid || eventID.Int64 <= 0 {
		return 0, ErrEvidenceUnavailable
	}
	return eventID.Int64, nil
}

func (r *PostgreSQLRepository) RecordUnifiedEvidenceAccess(ctx context.Context, decisionPK, adminID int64, reason, outcome string) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO security_audit_evidence_access_logs(decision_pk,admin_id,reason,outcome)
VALUES ($1,$2,$3,$4)`,
		decisionPK, nullableID(adminID), TrimRunes(strings.TrimSpace(reason), 256), outcome)
	return err
}

func (r *PostgreSQLRepository) SecurityAuditOverview(ctx context.Context, windowHours int64) (*SecurityAuditOverview, error) {
	if windowHours < 1 || windowHours > 24*90 {
		windowHours = 24
	}
	since := time.Now().Add(-time.Duration(windowHours) * time.Hour)
	result := &SecurityAuditOverview{
		WindowHours: windowHours, ByRisk: map[string]int64{}, BySource: map[string]int64{}, GeneratedAt: time.Now(),
	}
	err := r.db.QueryRowContext(ctx, `
SELECT
    COUNT(*),
    COUNT(*) FILTER (WHERE request_action='allow'),
    COUNT(*) FILTER (WHERE request_action='warn'),
    COUNT(*) FILTER (WHERE request_action='block'),
    COUNT(*) FILTER (WHERE evaluation_status<>'complete')
FROM security_audit_decisions
WHERE created_at >= $1`, since).Scan(
		&result.TotalDecisions, &result.Allowed, &result.Warned, &result.Blocked, &result.Degraded,
	)
	if err != nil {
		return nil, err
	}
	if err = r.db.QueryRowContext(ctx, `
SELECT
    (SELECT COUNT(*) FROM security_audit_cases WHERE status IN ('open','reviewing')),
    (SELECT COUNT(*) FROM security_audit_actions WHERE status IN ('pending','processing','retry')),
    (SELECT COUNT(*) FROM security_audit_actions WHERE status='failed')`).Scan(
		&result.OpenCases, &result.PendingActions, &result.FailedActions,
	); err != nil {
		return nil, err
	}
	if err = r.db.QueryRowContext(ctx, `
SELECT
    (SELECT COUNT(*) FROM security_audit_policy_versions WHERE status='active'),
    (SELECT COUNT(*) FROM security_audit_exceptions
     WHERE status='active' AND starts_at<=NOW() AND (permanent OR expires_at>NOW()))`).Scan(
		&result.ActivePolicies, &result.ActiveExceptions,
	); err != nil {
		return nil, err
	}
	if err = r.db.QueryRowContext(ctx, `
SELECT
    COALESCE((
        SELECT percentile_cont(0.95) WITHIN GROUP (ORDER BY e.latency_ms)::bigint
        FROM security_audit_evidence e
        JOIN security_audit_decisions d ON d.id=e.decision_pk
        WHERE d.created_at >= $1
    ),0),
    COALESCE((
        SELECT EXTRACT(EPOCH FROM (NOW()-MIN(created_at)))::bigint
        FROM security_audit_actions
        WHERE status IN ('pending','processing','retry')
    ),0),
    (SELECT COUNT(*) FROM security_audit_feedback WHERE conclusion='false_positive' AND created_at >= $1),
    (SELECT COUNT(*) FROM security_audit_feedback WHERE conclusion='false_negative' AND created_at >= $1),
    (SELECT COUNT(*) FROM security_audit_evidence_access_logs WHERE outcome='revealed' AND created_at >= $1)`,
		since,
	).Scan(
		&result.DetectorP95MS, &result.OldestPendingActionSec,
		&result.FalsePositiveCount, &result.FalseNegativeCount, &result.EvidenceRevealCount,
	); err != nil {
		return nil, err
	}
	var signalLastAggregated sql.NullTime
	if err = r.db.QueryRowContext(ctx, `
SELECT
    (SELECT COUNT(*) FROM security_audit_signal_evaluations
     WHERE matched=TRUE AND evaluated_at >= $1),
    (SELECT COUNT(*) FROM security_audit_notifications
     WHERE audience='admin' AND status='unread'),
    w.last_aggregated_at,
    w.last_error
FROM security_audit_signal_watermark w
WHERE w.id=1`, since).Scan(
		&result.BehaviorMatches, &result.UnreadNotifications,
		&signalLastAggregated, &result.SignalLastError,
	); err != nil {
		return nil, err
	}
	if signalLastAggregated.Valid {
		value := signalLastAggregated.Time
		result.SignalLastAggregatedAt = &value
		lag := time.Since(value).Seconds()
		if lag > 0 {
			result.SignalLagSeconds = int64(lag)
		}
	}
	if err := scanGroupedCounts(ctx, r.db, `
SELECT risk_level,COUNT(*) FROM security_audit_decisions
WHERE created_at >= $1 GROUP BY risk_level`, since, result.ByRisk); err != nil {
		return nil, err
	}
	if err := scanGroupedCounts(ctx, r.db, `
SELECT source_type,COUNT(*) FROM security_audit_decisions
WHERE created_at >= $1 GROUP BY source_type`, since, result.BySource); err != nil {
		return nil, err
	}
	return result, nil
}

func scanGroupedCounts(ctx context.Context, db *sql.DB, query string, since time.Time, target map[string]int64) error {
	rows, err := db.QueryContext(ctx, query, since)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var key string
		var count int64
		if err := rows.Scan(&key, &count); err != nil {
			return err
		}
		target[key] = count
	}
	return rows.Err()
}
