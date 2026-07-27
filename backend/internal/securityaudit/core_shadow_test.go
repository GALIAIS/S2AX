package securityaudit

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestEvaluateShadowPoliciesPersistsComparisonWithoutEnforcement(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := NewPostgreSQLRepository(db)

	now := time.Date(2026, time.July, 24, 8, 0, 0, 0, time.UTC)
	config := canonicalSecurityPolicy(defaultSecurityPolicyConfig())
	config.Scope.AllGroups = true
	config.Actions.High = []string{"notify_admin"}
	configRaw, err := json.Marshal(config)
	require.NoError(t, err)

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT pg_try_advisory_xact_lock`).
		WithArgs(securityAuditShadowEvaluationLock).
		WillReturnRows(sqlmock.NewRows([]string{"locked"}).AddRow(true))
	mock.ExpectQuery(`(?s)SELECT last_decision_pk.*security_audit_shadow_watermark.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"last_decision_pk"}).AddRow(int64(0)))
	mock.ExpectQuery(`(?s)FROM security_audit_policy_versions p.*p.status='shadow'`).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "policy_key", "version", "name", "status", "priority", "config",
			"config_digest", "validation_errors", "change_reason", "created_by",
			"created_at", "validated_at", "shadowed_at", "activated_at", "retired_at",
		}).AddRow(
			int64(10), "default_security", int64(3), config.Name, PolicyStatusShadow,
			config.Priority, configRaw, "digest", []byte(`[]`), "shadow rollout",
			int64(7), now.Add(-2*time.Hour), now.Add(-90*time.Minute),
			now.Add(-time.Hour), nil, nil,
		))
	mock.ExpectQuery(`(?s)FROM security_audit_decisions.*WHERE id>\$1.*ORDER BY id`).
		WithArgs(int64(0), 500).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "decision_id", "source_type", "user_id", "api_key_id", "group_id",
			"protocol", "endpoint", "requested_model", "risk_level", "request_action",
			"candidate_actions", "created_at", "detector_summary",
		}).AddRow(
			int64(41), "dec_41", "legacy_moderation", int64(9), nil, int64(4),
			"openai", "/v1/chat/completions", "gpt-test", "high", "allow",
			[]byte(`[]`), now, []byte(`[{"detector_id":"legacy","outcome":"matched","category":"cyber"}]`),
		))
	mock.ExpectQuery(`(?s)SELECT effect.*FROM security_audit_exceptions`).
		WillReturnRows(sqlmock.NewRows([]string{"effect"}))
	mock.ExpectExec(`INSERT INTO security_audit_shadow_evaluations`).
		WithArgs(
			int64(41), int64(10), "default_security", int64(3), "high",
			"allow", "block", sqlmock.AnyArg(), sqlmock.AnyArg(), true, true,
		).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`(?s)UPDATE security_audit_shadow_watermark.*last_decision_pk=\$1`).
		WithArgs(int64(41)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	processed, err := repo.EvaluateShadowPolicies(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(1), processed)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestListPolicyShadowEvaluationsReturnsWatermarkAndSamples(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := NewPostgreSQLRepository(db)
	now := time.Date(2026, time.July, 24, 8, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`(?s)FROM security_audit_policy_versions p.*security_audit_shadow_watermark`).
		WithArgs("default_security", int64(3), int64(168)).
		WillReturnRows(sqlmock.NewRows([]string{
			"total", "request_changes", "action_changes", "stricter", "looser",
			"unchanged", "last_decision_pk", "last_evaluated_at", "last_error",
		}).AddRow(3, 1, 2, 1, 0, 1, 44, now, ""))
	mock.ExpectQuery(`(?s)FROM security_audit_shadow_evaluations e.*ORDER BY e.created_at DESC`).
		WithArgs("default_security", int64(3), int64(168), 20).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "decision_pk", "decision_id", "source_type", "policy_version_id",
			"policy_key", "policy_version", "risk_level", "baseline_request_action",
			"proposed_request_action", "baseline_actions", "proposed_actions",
			"request_action_changed", "actions_changed", "created_at",
			"decision_created_at", "detector_summary",
		}).AddRow(
			1, 41, "dec_41", "prompt_audit", 10, "default_security", 3,
			"high", "allow", "block", []byte(`[]`), []byte(`["notify_admin"]`),
			true, true, now, now.Add(-time.Second), []byte(`[]`),
		))

	result, err := repo.ListPolicyShadowEvaluations(
		context.Background(),
		"default_security",
		3,
		168,
		20,
	)
	require.NoError(t, err)
	require.Equal(t, int64(3), result.Total)
	require.Equal(t, int64(44), result.LastEvaluatedDecisionPK)
	require.Len(t, result.Items, 1)
	require.Equal(t, []string{"notify_admin"}, result.Items[0].ProposedActions)
	require.NoError(t, mock.ExpectationsWereMet())
}
