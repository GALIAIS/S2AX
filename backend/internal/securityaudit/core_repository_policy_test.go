package securityaudit

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestListPolicyTransitionsReturnsNativeLifecycleHistory(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := NewPostgreSQLRepository(db)
	now := time.Now().UTC()
	mock.ExpectQuery(`SELECT id,policy_version_id,policy_key,version,from_status,to_status,actor_id,reason,created_at`).
		WithArgs("default_security", 50).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "policy_version_id", "policy_key", "version", "from_status",
			"to_status", "actor_id", "reason", "created_at",
		}).
			AddRow(2, 10, "default_security", 3, "shadow", "active", 7, "reviewed rollout", now).
			AddRow(1, 10, "default_security", 3, "validated", "shadow", nil, "automated validation", now.Add(-time.Minute)))

	items, err := repo.ListPolicyTransitions(context.Background(), " default_security ", 50)

	require.NoError(t, err)
	require.Len(t, items, 2)
	require.Equal(t, int64(7), *items[0].ActorID)
	require.Nil(t, items[1].ActorID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestTransitionPolicyPersistsActorAndReasonInSameTransaction(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := NewPostgreSQLRepository(db)
	config := canonicalSecurityPolicy(defaultSecurityPolicyConfig())

	mock.ExpectBegin()
	mock.ExpectExec(`SELECT pg_advisory_xact_lock`).
		WithArgs("security-audit-policy:default_security").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`(?s)SELECT .*FROM security_audit_policy_versions p.*FOR UPDATE`).
		WithArgs("default_security", int64(3)).
		WillReturnRows(policyVersionRows(config, "draft"))
	mock.ExpectQuery(`(?s)UPDATE security_audit_policy_versions.*RETURNING`).
		WithArgs("default_security", int64(3), PolicyStatusValidated, "validated after review").
		WillReturnRows(policyVersionRows(config, PolicyStatusValidated))
	mock.ExpectExec(`INSERT INTO security_audit_policy_transitions`).
		WithArgs(
			int64(10), "default_security", int64(3), PolicyStatusDraft,
			PolicyStatusValidated, int64(7), "validated after review",
		).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	updated, err := repo.TransitionPolicy(
		context.Background(), "default_security", 3,
		PolicyStatusValidated, 7, "validated after review",
	)

	require.NoError(t, err)
	require.Equal(t, PolicyStatusValidated, updated.Status)
	require.NoError(t, mock.ExpectationsWereMet())
}

func policyVersionRows(config SecurityPolicyConfig, status string) *sqlmock.Rows {
	raw, _ := json.Marshal(config)
	now := time.Now().UTC()
	var validatedAt any
	if status == PolicyStatusValidated {
		validatedAt = now
	}
	return sqlmock.NewRows([]string{
		"id", "policy_key", "version", "name", "status", "priority", "config",
		"config_digest", "validation_errors", "change_reason", "created_by",
		"created_at", "validated_at", "shadowed_at", "activated_at", "retired_at",
	}).AddRow(
		10, "default_security", 3, config.Name, status, config.Priority, raw,
		"digest", []byte(`[]`), "reviewed", 7,
		now, validatedAt, nil, nil, nil,
	)
}
