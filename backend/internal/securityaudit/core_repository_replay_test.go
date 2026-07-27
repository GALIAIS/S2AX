package securityaudit

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestReplayPolicyComparesStoredDecisionsWithoutRawEvidence(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := NewPostgreSQLRepository(db)
	now := time.Now().UTC()
	emptyActions, _ := json.Marshal([]string{})
	rows := sqlmock.NewRows([]string{
		"id", "decision_id", "source_type", "user_id", "api_key_id", "group_id",
		"protocol", "endpoint", "requested_model", "risk_level", "request_action",
		"candidate_actions", "created_at",
	}).
		AddRow(10, "dec-high", "prompt_audit", 7, 11, 3, "openai", "/v1/chat/completions", "gpt", "high", "allow", emptyActions, now).
		AddRow(9, "dec-low", "legacy_moderation", 7, 11, 3, "openai", "/v1/chat/completions", "gpt", "low", "allow", emptyActions, now.Add(-time.Minute))
	mock.ExpectQuery("SELECT id,decision_id,source_type").WithArgs(168, 1000).WillReturnRows(rows)

	result, err := repo.ReplayPolicy(context.Background(), &PolicyVersion{
		PolicyKey: "default_security", Version: 2, ConfigDigest: "digest",
		Config: SecurityPolicyConfig{
			Name: "Default", Mode: ModeBlocking, Scope: PolicyScope{AllGroups: true},
			Actions: PolicyActions{High: []string{"open_case"}},
		},
	}, PolicyReplayRequest{WindowHours: 168, Limit: 1000})

	require.NoError(t, err)
	require.Equal(t, 2, result.Analyzed)
	require.Equal(t, 2, result.Matched)
	require.Equal(t, 1, result.ActionChanges)
	require.Equal(t, 1, result.StricterChanges)
	require.Zero(t, result.LooserChanges)
	require.Equal(t, 1, result.CandidateActionChanges)
	require.Equal(t, map[string]int{"allow": 1, "block": 1}, result.ByProposedAction)
	require.Len(t, result.Examples, 1)
	require.Equal(t, int64(10), result.Examples[0].DecisionPK)
	require.NoError(t, mock.ExpectationsWereMet())
}
