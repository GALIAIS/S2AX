package securityaudit

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	securityAuditSignalAggregationLock int64 = 579147893221901923
	securityAuditSignalEvaluationLock  int64 = 579147893221901924
	securityAuditSignalEvaluationBatch       = 500
)

type behaviorEvaluationRequest struct {
	AnchorID       int64   `json:"anchor_id"`
	PolicyKey      string  `json:"policy_key"`
	PolicyVersion  int64   `json:"policy_version"`
	RuleID         string  `json:"rule_id"`
	Metric         string  `json:"metric"`
	WindowMinutes  int     `json:"window_minutes"`
	Threshold      float64 `json:"threshold"`
	MinimumSamples int64   `json:"minimum_samples"`
	Severity       string  `json:"severity"`
}

type behaviorEvaluation struct {
	ID             int64
	AnchorID       int64
	PolicyKey      string
	PolicyVersion  int64
	RuleID         string
	Metric         string
	WindowMinutes  int
	ObservedValue  float64
	ThresholdValue float64
	SampleCount    int64
	Score          float64
	Severity       string
	Matched        bool
}

type sqlRowsQueryer interface {
	sqlQueryer
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// AggregateClosedBehaviorSignalWindows projects closed UTC minute buckets from
// usage and ops logs. It never retains raw payloads, user agents or IP addresses.
func (r *PostgreSQLRepository) AggregateClosedBehaviorSignalWindows(ctx context.Context, now time.Time) (int64, error) {
	if r == nil || r.db == nil {
		return 0, errors.New("security audit database unavailable")
	}
	if now.IsZero() {
		now = time.Now()
	}
	closedBoundary := now.UTC().Truncate(time.Minute)
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	var locked bool
	if err = tx.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock($1)`, securityAuditSignalAggregationLock).Scan(&locked); err != nil {
		return 0, err
	}
	if !locked {
		return 0, nil
	}
	var start time.Time
	if err = tx.QueryRowContext(ctx, `
SELECT last_aggregated_at
FROM security_audit_signal_watermark
WHERE id=1
FOR UPDATE`).Scan(&start); err != nil {
		return 0, err
	}
	start = start.UTC().Truncate(time.Minute)
	if !closedBoundary.After(start) {
		return 0, tx.Commit()
	}
	end := start.Add(10 * time.Minute)
	if end.After(closedBoundary) {
		end = closedBoundary
	}
	result, err := tx.ExecContext(ctx, `
WITH raw_events AS (
    SELECT
        date_trunc('minute', ul.created_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC' AS bucket_start,
        ul.user_id,
        ul.api_key_id,
        ul.group_id,
        COALESCE(NULLIF(ul.requested_model, ''), ul.model, '') AS model,
        NULLIF(BTRIM(COALESCE(ul.ip_address, '')), '') AS client_ip,
        1::bigint AS success_count,
        0::bigint AS error_count,
        0::bigint AS business_limited_count,
        GREATEST(
            ul.input_tokens + ul.output_tokens + ul.cache_creation_tokens + ul.cache_read_tokens,
            0
        )::bigint AS token_count,
        GREATEST(ul.actual_cost, 0)::numeric AS actual_cost,
        GREATEST(COALESCE(ul.duration_ms, 0), 0)::bigint AS duration_ms,
        CASE WHEN ul.duration_ms IS NULL THEN 0::bigint ELSE 1::bigint END AS duration_sample_count
    FROM usage_logs ul
    WHERE ul.created_at >= $1 AND ul.created_at < $2

    UNION ALL

    SELECT
        date_trunc('minute', oe.created_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC' AS bucket_start,
        oe.user_id,
        oe.api_key_id,
        oe.group_id,
        COALESCE(NULLIF(oe.requested_model, ''), oe.model, '') AS model,
        NULLIF(BTRIM(COALESCE(oe.client_ip::text, '')), '') AS client_ip,
        0::bigint AS success_count,
        1::bigint AS error_count,
        CASE WHEN oe.is_business_limited THEN 1::bigint ELSE 0::bigint END AS business_limited_count,
        0::bigint AS token_count,
        0::numeric AS actual_cost,
        GREATEST(COALESCE(oe.duration_ms, 0), 0)::bigint AS duration_ms,
        CASE WHEN oe.duration_ms IS NULL THEN 0::bigint ELSE 1::bigint END AS duration_sample_count
    FROM ops_error_logs oe
    WHERE oe.created_at >= $1 AND oe.created_at < $2
      AND COALESCE(oe.is_count_tokens, FALSE) = FALSE
),
expanded AS (
    SELECT
        raw.bucket_start,
        subject.subject_type,
        subject.subject_id,
        subject.user_id,
        subject.api_key_id,
        subject.group_id,
        raw.model,
        raw.client_ip,
        raw.success_count,
        raw.error_count,
        raw.business_limited_count,
        raw.token_count,
        raw.actual_cost,
        raw.duration_ms,
        raw.duration_sample_count
    FROM raw_events raw
    CROSS JOIN LATERAL (
        VALUES
            ('user'::text, raw.user_id, raw.user_id, NULL::bigint, NULL::bigint),
            ('api_key'::text, raw.api_key_id, raw.user_id, raw.api_key_id, NULL::bigint),
            ('group'::text, raw.group_id, NULL::bigint, NULL::bigint, raw.group_id)
    ) AS subject(subject_type, subject_id, user_id, api_key_id, group_id)
    WHERE subject.subject_id IS NOT NULL AND subject.subject_id > 0
),
aggregated AS (
    SELECT
        bucket_start,
        subject_type,
        subject_id,
        MAX(user_id) AS user_id,
        MAX(api_key_id) AS api_key_id,
        MAX(group_id) AS group_id,
        SUM(success_count) + SUM(error_count) AS request_count,
        SUM(success_count) AS success_count,
        SUM(error_count) AS error_count,
        SUM(business_limited_count) AS business_limited_count,
        SUM(token_count) AS token_count,
        SUM(actual_cost) AS actual_cost,
        SUM(duration_ms) AS duration_sum_ms,
        SUM(duration_sample_count) AS duration_sample_count,
        MAX(duration_ms)::integer AS duration_max_ms,
        COUNT(DISTINCT client_ip) FILTER (WHERE client_ip IS NOT NULL)::integer AS distinct_ip_count,
        COUNT(DISTINCT model) FILTER (WHERE model <> '')::integer AS distinct_model_count
    FROM expanded
    GROUP BY bucket_start, subject_type, subject_id
)
INSERT INTO security_audit_signal_windows(
    bucket_start,bucket_seconds,subject_type,subject_id,user_id,api_key_id,group_id,
    subject_snapshot,request_count,success_count,error_count,business_limited_count,
    token_count,actual_cost,duration_sum_ms,duration_sample_count,duration_max_ms,
    distinct_ip_count,distinct_model_count,computed_at
)
SELECT
    a.bucket_start,60,a.subject_type,a.subject_id,a.user_id,a.api_key_id,a.group_id,
    CASE a.subject_type
        WHEN 'user' THEN COALESCE((SELECT NULLIF(u.email, '') FROM users u WHERE u.id=a.subject_id), 'user#' || a.subject_id::text)
        WHEN 'api_key' THEN COALESCE((SELECT NULLIF(k.name, '') FROM api_keys k WHERE k.id=a.subject_id), 'api-key#' || a.subject_id::text)
        WHEN 'group' THEN COALESCE((SELECT NULLIF(g.name, '') FROM groups g WHERE g.id=a.subject_id), 'group#' || a.subject_id::text)
        ELSE a.subject_type || '#' || a.subject_id::text
    END,
    a.request_count,a.success_count,a.error_count,a.business_limited_count,
    a.token_count,a.actual_cost,a.duration_sum_ms,a.duration_sample_count,a.duration_max_ms,
    a.distinct_ip_count,a.distinct_model_count,NOW()
FROM aggregated a
ON CONFLICT (bucket_start,bucket_seconds,subject_type,subject_id)
DO UPDATE SET
    user_id=EXCLUDED.user_id,
    api_key_id=EXCLUDED.api_key_id,
    group_id=EXCLUDED.group_id,
    subject_snapshot=EXCLUDED.subject_snapshot,
    request_count=EXCLUDED.request_count,
    success_count=EXCLUDED.success_count,
    error_count=EXCLUDED.error_count,
    business_limited_count=EXCLUDED.business_limited_count,
    token_count=EXCLUDED.token_count,
    actual_cost=EXCLUDED.actual_cost,
    duration_sum_ms=EXCLUDED.duration_sum_ms,
    duration_sample_count=EXCLUDED.duration_sample_count,
    duration_max_ms=EXCLUDED.duration_max_ms,
    distinct_ip_count=EXCLUDED.distinct_ip_count,
    distinct_model_count=EXCLUDED.distinct_model_count,
    computed_at=NOW()`, start, end)
	if err != nil {
		_, _ = tx.ExecContext(ctx, `
UPDATE security_audit_signal_watermark
SET last_error=$1,updated_at=NOW()
WHERE id=1`, TrimRunes(err.Error(), 512))
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `
UPDATE security_audit_signal_watermark
SET last_aggregated_at=$1,last_error='',updated_at=NOW()
WHERE id=1`, end); err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	affected, _ := result.RowsAffected()
	return affected, nil
}

// EvaluateBehaviorSignals evaluates a bounded page of durable windows. The
// watermark is advanced in the same transaction as evaluations and decisions,
// so retries are idempotent and multi-instance deployments are safe.
func (r *PostgreSQLRepository) EvaluateBehaviorSignals(ctx context.Context) (int64, error) {
	if r == nil || r.db == nil {
		return 0, errors.New("security audit database unavailable")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	var locked bool
	if err = tx.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock($1)`, securityAuditSignalEvaluationLock).Scan(&locked); err != nil {
		return 0, err
	}
	if !locked {
		return 0, nil
	}
	var cursor int64
	var aggregatedUntil time.Time
	if err = tx.QueryRowContext(ctx, `
SELECT last_evaluated_window_id,last_aggregated_at
FROM security_audit_signal_watermark
WHERE id=1
FOR UPDATE`).Scan(&cursor, &aggregatedUntil); err != nil {
		return 0, err
	}
	policies, err := listActiveBehaviorPolicies(ctx, tx)
	if err != nil {
		return 0, err
	}
	anchors, err := listSignalAnchors(ctx, tx, cursor, aggregatedUntil, securityAuditSignalEvaluationBatch)
	if err != nil {
		return 0, err
	}
	if len(anchors) == 0 {
		if _, err = tx.ExecContext(ctx, `
UPDATE security_audit_signal_watermark
SET last_evaluated_at=last_aggregated_at,last_error='',updated_at=NOW()
WHERE id=1`); err != nil {
			return 0, err
		}
		return 0, tx.Commit()
	}
	requests := make([]behaviorEvaluationRequest, 0)
	policyByAnchor := make(map[int64]*PolicyVersion, len(anchors))
	for _, anchor := range anchors {
		policy := selectBehaviorPolicy(policies, anchor)
		if policy == nil {
			continue
		}
		policyByAnchor[anchor.ID] = policy
		for _, rule := range policy.Config.Signals.Rules {
			if !rule.Enabled || rule.SubjectType != anchor.SubjectType {
				continue
			}
			requests = append(requests, behaviorEvaluationRequest{
				AnchorID: anchor.ID, PolicyKey: policy.PolicyKey, PolicyVersion: policy.Version,
				RuleID: rule.ID, Metric: rule.Metric, WindowMinutes: rule.WindowMinutes,
				Threshold: rule.Threshold, MinimumSamples: rule.MinimumSamples, Severity: rule.Severity,
			})
		}
	}
	evaluations, err := insertBehaviorEvaluations(ctx, tx, requests)
	if err != nil {
		return 0, err
	}
	matchedByAnchor := make(map[int64][]behaviorEvaluation)
	for _, evaluation := range evaluations {
		if evaluation.Matched {
			matchedByAnchor[evaluation.AnchorID] = append(matchedByAnchor[evaluation.AnchorID], evaluation)
		}
	}
	for _, anchor := range anchors {
		matched := matchedByAnchor[anchor.ID]
		if len(matched) == 0 {
			continue
		}
		policy := policyByAnchor[anchor.ID]
		if policy == nil {
			continue
		}
		decisionPK, err := insertBehaviorDecision(ctx, tx, anchor, policy, matched)
		if err != nil {
			return 0, err
		}
		if _, err = tx.ExecContext(ctx, `
UPDATE security_audit_signal_evaluations
SET decision_pk=$2
WHERE anchor_window_id=$1 AND matched=TRUE AND decision_pk IS NULL`, anchor.ID, decisionPK); err != nil {
			return 0, err
		}
	}
	last := anchors[len(anchors)-1]
	if _, err = tx.ExecContext(ctx, `
UPDATE security_audit_signal_watermark
SET last_evaluated_window_id=$1,last_evaluated_at=$2,last_error='',updated_at=NOW()
WHERE id=1`, last.ID, last.BucketStart); err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	// Return consumed anchors rather than matches so maintenance can distinguish
	// an empty result set from a full page with no policy matches and drain
	// backlog without waiting for another 30-second tick.
	return int64(len(anchors)), nil
}

func (r *PostgreSQLRepository) RecordBehaviorSignalError(ctx context.Context, cause error) {
	if r == nil || r.db == nil || cause == nil {
		return
	}
	_, _ = r.db.ExecContext(ctx, `
UPDATE security_audit_signal_watermark
SET last_error=$1,updated_at=NOW()
WHERE id=1`, TrimRunes(cause.Error(), 512))
}

func listActiveBehaviorPolicies(ctx context.Context, queryer sqlRowsQueryer) ([]*PolicyVersion, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT `+policyColumns("p")+`
FROM security_audit_policy_versions p
WHERE p.status='active'
ORDER BY p.priority DESC,p.policy_key,p.version DESC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]*PolicyVersion, 0)
	for rows.Next() {
		item, err := scanPolicy(rows)
		if err != nil {
			return nil, err
		}
		item.Config = canonicalSecurityPolicy(item.Config)
		if item.Config.Signals.Enabled {
			items = append(items, item)
		}
	}
	return items, rows.Err()
}

func listSignalAnchors(
	ctx context.Context,
	queryer sqlRowsQueryer,
	cursor int64,
	aggregatedUntil time.Time,
	limit int,
) ([]*BehaviorSignalWindow, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT `+signalWindowColumns("w")+`,0,''::text
FROM security_audit_signal_windows w
WHERE w.id>$1 AND w.bucket_start<$2
ORDER BY w.id
LIMIT $3`, cursor, aggregatedUntil, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]*BehaviorSignalWindow, 0, limit)
	for rows.Next() {
		item, err := scanSignalWindow(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func selectBehaviorPolicy(policies []*PolicyVersion, window *BehaviorSignalWindow) *PolicyVersion {
	var selected *PolicyVersion
	selectedRank := -1
	for _, policy := range policies {
		rank := behaviorPolicyScopeRank(policy.Config.Scope, window)
		if rank < 0 {
			continue
		}
		if selected == nil || rank > selectedRank ||
			(rank == selectedRank && policy.Priority > selected.Priority) {
			selected, selectedRank = policy, rank
		}
	}
	return selected
}

func behaviorPolicyScopeRank(scope PolicyScope, window *BehaviorSignalWindow) int {
	if window == nil {
		return -1
	}
	// Signal windows aggregate by subject and intentionally do not retain exact
	// request protocol, endpoint, or model. A request-filtered policy therefore
	// cannot be evaluated truthfully against a signal window.
	if len(scope.Protocols)+len(scope.Endpoints)+len(scope.Models) > 0 {
		return -1
	}
	if window.APIKeyID != nil && containsInt64(scope.APIKeyIDs, *window.APIKeyID) {
		return 5
	}
	if window.UserID != nil && containsInt64(scope.UserIDs, *window.UserID) {
		return 4
	}
	if window.GroupID != nil && containsInt64(scope.GroupIDs, *window.GroupID) {
		return 3
	}
	if scope.AllGroups {
		return 1
	}
	if len(scope.APIKeyIDs)+len(scope.UserIDs)+len(scope.GroupIDs) == 0 {
		return 1
	}
	return -1
}

func insertBehaviorEvaluations(
	ctx context.Context,
	queryer sqlRowsQueryer,
	requests []behaviorEvaluationRequest,
) ([]behaviorEvaluation, error) {
	if len(requests) == 0 {
		return []behaviorEvaluation{}, nil
	}
	raw, err := json.Marshal(requests)
	if err != nil {
		return nil, err
	}
	rows, err := queryer.QueryContext(ctx, `
WITH requested AS (
    SELECT *
    FROM jsonb_to_recordset($1::jsonb) AS r(
        anchor_id bigint,
        policy_key text,
        policy_version bigint,
        rule_id text,
        metric text,
        window_minutes integer,
        threshold double precision,
        minimum_samples bigint,
        severity text
    )
),
aggregated AS (
    SELECT
        r.*,
        a.subject_type,
        a.subject_id,
        COALESCE(SUM(w.request_count), 0)::bigint AS request_count,
        COALESCE(SUM(w.error_count), 0)::bigint AS error_count,
        COALESCE(SUM(w.business_limited_count), 0)::bigint AS business_limited_count,
        COALESCE(SUM(w.token_count), 0)::bigint AS token_count,
        COALESCE(SUM(w.actual_cost), 0)::double precision AS actual_cost,
        COALESCE(SUM(w.duration_sum_ms), 0)::bigint AS duration_sum_ms,
        COALESCE(SUM(w.duration_sample_count), 0)::bigint AS duration_sample_count,
        COALESCE(MAX(w.duration_max_ms), 0)::integer AS maximum_duration_ms,
        COALESCE(MAX(w.distinct_ip_count), 0)::integer AS distinct_ip_count,
        COALESCE(MAX(w.distinct_model_count), 0)::integer AS distinct_model_count
    FROM requested r
    JOIN security_audit_signal_windows a ON a.id=r.anchor_id
    JOIN security_audit_signal_windows w
      ON w.subject_type=a.subject_type
     AND w.subject_id=a.subject_id
     AND w.bucket_start<=a.bucket_start
     AND w.bucket_start>=a.bucket_start-((r.window_minutes-1)::text || ' minutes')::interval
    GROUP BY
        r.anchor_id,r.policy_key,r.policy_version,r.rule_id,r.metric,
        r.window_minutes,r.threshold,r.minimum_samples,r.severity,
        a.subject_type,a.subject_id
),
observed AS (
    SELECT
        a.*,
        CASE a.metric
            WHEN 'request_count' THEN a.request_count::double precision
            WHEN 'token_count' THEN a.token_count::double precision
            WHEN 'actual_cost' THEN a.actual_cost
            WHEN 'error_count' THEN a.error_count::double precision
            WHEN 'error_rate' THEN a.error_count::double precision/NULLIF(a.request_count,0)
            WHEN 'business_limited_rate' THEN a.business_limited_count::double precision/NULLIF(a.request_count,0)
            WHEN 'average_duration_ms' THEN a.duration_sum_ms::double precision/NULLIF(a.duration_sample_count,0)
            WHEN 'maximum_duration_ms' THEN a.maximum_duration_ms::double precision
            WHEN 'distinct_ip_count' THEN a.distinct_ip_count::double precision
            WHEN 'distinct_model_count' THEN a.distinct_model_count::double precision
            ELSE 0::double precision
        END AS observed_value
    FROM aggregated a
),
inserted AS (
    INSERT INTO security_audit_signal_evaluations(
        anchor_window_id,policy_key,policy_version,rule_id,metric,window_minutes,
        observed_value,threshold_value,sample_count,score,severity,matched
    )
    SELECT
        o.anchor_id,o.policy_key,o.policy_version,o.rule_id,o.metric,o.window_minutes,
        COALESCE(o.observed_value,0),o.threshold,o.request_count,
        LEAST(1,COALESCE(o.observed_value,0)/NULLIF(o.threshold*2,0)),
        o.severity,
        o.request_count>=o.minimum_samples AND COALESCE(o.observed_value,0)>=o.threshold
    FROM observed o
    ON CONFLICT (anchor_window_id,policy_key,policy_version,rule_id)
    DO UPDATE SET
        observed_value=EXCLUDED.observed_value,
        threshold_value=EXCLUDED.threshold_value,
        sample_count=EXCLUDED.sample_count,
        score=EXCLUDED.score,
        severity=EXCLUDED.severity,
        matched=EXCLUDED.matched,
        evaluated_at=NOW()
    RETURNING id,anchor_window_id,policy_key,policy_version,rule_id,metric,
              window_minutes,observed_value,threshold_value,sample_count,score,severity,matched
)
SELECT * FROM inserted`, raw)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]behaviorEvaluation, 0, len(requests))
	for rows.Next() {
		var item behaviorEvaluation
		if err := rows.Scan(
			&item.ID, &item.AnchorID, &item.PolicyKey, &item.PolicyVersion,
			&item.RuleID, &item.Metric, &item.WindowMinutes, &item.ObservedValue,
			&item.ThresholdValue, &item.SampleCount, &item.Score, &item.Severity,
			&item.Matched,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func insertBehaviorDecision(
	ctx context.Context,
	queryer sqlQueryer,
	window *BehaviorSignalWindow,
	policy *PolicyVersion,
	evaluations []behaviorEvaluation,
) (int64, error) {
	if window == nil || policy == nil || len(evaluations) == 0 {
		return 0, errors.New("behavior decision input unavailable")
	}
	sort.SliceStable(evaluations, func(i, j int) bool {
		return severityRank(evaluations[i].Severity) > severityRank(evaluations[j].Severity)
	})
	risk := evaluations[0].Severity
	candidateActions := effectiveCandidateActionsForRisk(policy.Config, risk)
	snapshot := PromptSnapshot{
		UserID: windowID(window.UserID), APIKeyID: windowID(window.APIKeyID), GroupID: window.GroupID,
		UserEmailSnapshot: subjectSnapshot(window, "user"), APIKeyNameSnapshot: subjectSnapshot(window, "api_key"),
		GroupName: subjectSnapshot(window, "group"), Stage: "post_request",
	}
	evidence := make([]DetectorEvidence, 0, len(evaluations))
	for _, evaluation := range evaluations {
		summary := fmt.Sprintf(
			"%d 分钟 %s=%.6g，阈值=%.6g，样本=%d",
			evaluation.WindowMinutes, evaluation.Metric, evaluation.ObservedValue,
			evaluation.ThresholdValue, evaluation.SampleCount,
		)
		sum := sha256.Sum256([]byte(summary))
		evidence = append(evidence, DetectorEvidence{
			DetectorID: "behavior_signal/" + evaluation.RuleID, DetectorVersion: "signal-v1",
			Outcome: "matched", Category: evaluation.Metric, Score: clampScore(evaluation.Score),
			Severity: evaluation.Severity, SafeSummary: summary,
			EvidenceDigest: hex.EncodeToString(sum[:]),
		})
	}
	candidateActions, err := applyActiveExceptionToActions(ctx, queryer, snapshot, evidence, candidateActions)
	if err != nil {
		return 0, fmt.Errorf("resolve active security exception: %w", err)
	}
	detectorsRaw, _ := json.Marshal(evidence)
	actionsRaw, _ := json.Marshal(candidateActions)
	decisionID := newSecurityID("dec")
	auditID := newSecurityID("aud")
	preview := fmt.Sprintf(
		"%s %s 在 %s 命中 %d 条行为安全规则",
		window.SubjectType, window.SubjectSnapshot,
		window.BucketStart.UTC().Format(time.RFC3339), len(evaluations),
	)
	digestRaw, _ := json.Marshal(map[string]any{
		"source_event_id": window.ID, "policy_key": policy.PolicyKey,
		"policy_version": policy.Version, "risk_level": risk,
		"evaluations": evaluations, "candidate_actions": candidateActions,
	})
	digest := sha256.Sum256(digestRaw)
	var decisionPK int64
	var persistedDecisionID string
	err = queryer.QueryRowContext(ctx, `
INSERT INTO security_audit_decisions(
    decision_id,audit_id,source_type,source_event_id,request_id,stage,
    user_id,user_snapshot,api_key_id,api_key_snapshot,group_id,group_snapshot,
    provider,endpoint,protocol,requested_model,policy_key,policy_version,
    canonicalizer_version,evaluation_status,risk_level,request_action,
    prompt_hash,redacted_preview,detector_summary,candidate_actions,decision_digest
) VALUES (
    $1,$2,'behavior',$3,'','post_request',
    $4,$5,$6,$7,$8,$9,
    '','','behavior_signal','',$10,$11,
    'signal-v1','complete',$12,'warn',
    '',$13,$14::jsonb,$15::jsonb,$16
)
ON CONFLICT (source_type,source_event_id)
DO UPDATE SET
    policy_key=EXCLUDED.policy_key,
    policy_version=EXCLUDED.policy_version,
    risk_level=EXCLUDED.risk_level,
    request_action=EXCLUDED.request_action,
    redacted_preview=EXCLUDED.redacted_preview,
    detector_summary=EXCLUDED.detector_summary,
    candidate_actions=EXCLUDED.candidate_actions,
    decision_digest=EXCLUDED.decision_digest
RETURNING id,decision_id`,
		decisionID, auditID, window.ID,
		window.UserID, snapshot.UserEmailSnapshot, window.APIKeyID, snapshot.APIKeyNameSnapshot,
		window.GroupID, snapshot.GroupName, policy.PolicyKey, policy.Version, risk,
		RedactPreview(preview, 512), detectorsRaw, actionsRaw, hex.EncodeToString(digest[:]),
	).Scan(&decisionPK, &persistedDecisionID)
	if err != nil {
		return 0, err
	}
	if err := insertUnifiedEvidence(ctx, queryer, decisionPK, evidence); err != nil {
		return 0, err
	}
	for _, action := range candidateActions {
		if (action == "pause_api_key") && snapshot.APIKeyID <= 0 {
			continue
		}
		if (action == "pause_user" || action == "notify_user") && snapshot.UserID <= 0 {
			continue
		}
		if err := insertUnifiedAction(ctx, queryer, decisionPK, persistedDecisionID, action, snapshot, risk); err != nil {
			return 0, err
		}
	}
	return decisionPK, nil
}

func (r *PostgreSQLRepository) ListBehaviorSignals(
	ctx context.Context,
	filter BehaviorSignalFilter,
	page, pageSize int,
) (*BehaviorSignalPage, error) {
	page, pageSize = normalizePage(page, pageSize)
	where, args := buildSignalWhere(filter)
	var total int64
	if err := r.db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM security_audit_signal_windows w`+where, args...).Scan(&total); err != nil {
		return nil, err
	}
	limitIndex := len(args) + 1
	queryArgs := append(append([]any(nil), args...), pageSize, (page-1)*pageSize)
	rows, err := r.db.QueryContext(ctx, `
SELECT `+signalWindowColumns("w")+`,
       COUNT(e.id) FILTER (WHERE e.matched=TRUE)::integer,
       COALESCE((
           array_agg(e.severity ORDER BY
               CASE e.severity WHEN 'critical' THEN 4 WHEN 'high' THEN 3 WHEN 'medium' THEN 2 ELSE 1 END DESC
           ) FILTER (WHERE e.matched=TRUE)
       )[1], '')
FROM security_audit_signal_windows w
LEFT JOIN security_audit_signal_evaluations e ON e.anchor_window_id=w.id
`+where+`
GROUP BY w.id
ORDER BY w.bucket_start DESC,w.id DESC
LIMIT $`+strconv.Itoa(limitIndex)+` OFFSET $`+strconv.Itoa(limitIndex+1), queryArgs...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]*BehaviorSignalWindow, 0, pageSize)
	for rows.Next() {
		item, err := scanSignalWindow(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	pages := 0
	if total > 0 {
		pages = int((total + int64(pageSize) - 1) / int64(pageSize))
	}
	return &BehaviorSignalPage{Items: items, Total: total, Page: page, PageSize: pageSize, Pages: pages}, rows.Err()
}

func signalWindowColumns(alias string) string {
	return alias + `.id,` + alias + `.bucket_start,` + alias + `.bucket_seconds,` +
		alias + `.subject_type,` + alias + `.subject_id,` + alias + `.user_id,` +
		alias + `.api_key_id,` + alias + `.group_id,` + alias + `.subject_snapshot,` +
		alias + `.request_count,` + alias + `.success_count,` + alias + `.error_count,` +
		alias + `.business_limited_count,` + alias + `.token_count,` + alias + `.actual_cost,` +
		alias + `.duration_sum_ms,` + alias + `.duration_sample_count,` + alias + `.duration_max_ms,` +
		alias + `.distinct_ip_count,` + alias + `.distinct_model_count,` + alias + `.computed_at`
}

func scanSignalWindow(row rowScanner) (*BehaviorSignalWindow, error) {
	item := &BehaviorSignalWindow{}
	var userID, apiKeyID, groupID sql.NullInt64
	if err := row.Scan(
		&item.ID, &item.BucketStart, &item.BucketSeconds, &item.SubjectType,
		&item.SubjectID, &userID, &apiKeyID, &groupID, &item.SubjectSnapshot,
		&item.RequestCount, &item.SuccessCount, &item.ErrorCount,
		&item.BusinessLimitedCount, &item.TokenCount, &item.ActualCost,
		&item.DurationSumMS, &item.DurationSampleCount, &item.DurationMaxMS,
		&item.DistinctIPCount, &item.DistinctModelCount, &item.ComputedAt,
		&item.MatchedRules, &item.HighestSeverity,
	); err != nil {
		return nil, err
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
	return item, nil
}

func buildSignalWhere(filter BehaviorSignalFilter) (string, []any) {
	clauses := []string{" WHERE 1=1"}
	args := make([]any, 0)
	add := func(template string, value any) {
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf(template, len(args)))
	}
	if value := strings.TrimSpace(filter.SubjectType); value != "" {
		add(" AND w.subject_type=$%d", value)
	}
	if filter.SubjectID != nil {
		add(" AND w.subject_id=$%d", *filter.SubjectID)
	}
	if filter.StartAt != nil {
		add(" AND w.bucket_start >= $%d", *filter.StartAt)
	}
	if filter.EndAt != nil {
		add(" AND w.bucket_start < $%d", *filter.EndAt)
	}
	if filter.MatchedOnly {
		clauses = append(clauses, `
 AND EXISTS (
     SELECT 1 FROM security_audit_signal_evaluations e
     WHERE e.anchor_window_id=w.id AND e.matched=TRUE
 )`)
	}
	return strings.Join(clauses, ""), args
}

func severityRank(value string) int {
	switch strings.TrimSpace(value) {
	case "critical":
		return 4
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}

func windowID(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func subjectSnapshot(window *BehaviorSignalWindow, subjectType string) string {
	if window != nil && window.SubjectType == subjectType {
		return window.SubjectSnapshot
	}
	return ""
}
