package securityaudit

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActiveExceptionCanScopeFollowUpActionsByMatchedDetectorAndCategory(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	groupID := int64(3)
	snapshot := PromptSnapshot{
		UserID: 42, APIKeyID: 9, GroupID: &groupID,
		Model: "gpt-test", Endpoint: "/v1/chat/completions",
	}
	evidence := []DetectorEvidence{{
		DetectorID: "remote_guard",
		Outcome:    "matched",
		Category:   "cyber_abuse",
	}}

	mock.ExpectQuery(`(?s)scope_type='detector'.*scope_type='category'.*detector_id=''`).
		WithArgs("9", "42", "3", "gpt-test", "/v1/chat/completions", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"effect"}).AddRow("warn_only"))

	actions, applyErr := applyActiveExceptionToActions(
		context.Background(),
		db,
		snapshot,
		evidence,
		[]string{"pause_user", "notify_user", "open_case"},
	)
	require.NoError(t, applyErr)
	require.Equal(t, []string{"notify_user", "open_case"}, actions)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestActiveExceptionLookupFailureCannotSilentlyScheduleIrreversibleActions(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectQuery(`SELECT effect`).
		WillReturnError(assert.AnError)

	actions, applyErr := applyActiveExceptionToActions(
		context.Background(),
		db,
		PromptSnapshot{UserID: 42, APIKeyID: 9},
		[]DetectorEvidence{{DetectorID: "remote_guard", Outcome: "matched"}},
		[]string{"pause_user"},
	)

	require.ErrorIs(t, applyErr, assert.AnError)
	require.Nil(t, actions)
	require.NoError(t, mock.ExpectationsWereMet())
}
