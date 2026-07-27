package securityaudit

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestEvidenceRevealFailsClosedWhenAccessAuditCannotBePersisted(t *testing.T) {
	t.Run("prompt event access log is mandatory", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })
		repo := NewPostgreSQLRepository(db)
		service := &PromptService{repo: repo, config: &fakeConfigStore{}, clock: realClock{}}

		mock.ExpectQuery(`SELECT evidence_ciphertext, evidence_status, evidence_expires_at`).
			WithArgs(int64(11)).
			WillReturnRows(sqlmock.NewRows([]string{
				"evidence_ciphertext", "evidence_status", "evidence_expires_at",
			}).AddRow("secret prompt", string(EvidenceEncrypted), nil))
		mock.ExpectExec(`INSERT INTO prompt_audit_evidence_access_logs`).
			WithArgs(int64(11), int64(7), "incident review", "revealed").
			WillReturnError(errors.New("audit log unavailable"))

		result, revealErr := service.RevealEventEvidence(
			context.Background(), 11, 7, "incident review",
		)

		require.Nil(t, result)
		require.ErrorContains(t, revealErr, "record prompt evidence access before reveal")
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("unified access log is mandatory", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })
		repo := NewPostgreSQLRepository(db)
		service := &PromptService{repo: repo, config: &fakeConfigStore{}, clock: realClock{}}

		mock.ExpectQuery(`SELECT source_event_id`).
			WithArgs(int64(23)).
			WillReturnRows(sqlmock.NewRows([]string{"source_event_id"}).AddRow(int64(11)))
		mock.ExpectQuery(`SELECT evidence_ciphertext, evidence_status, evidence_expires_at`).
			WithArgs(int64(11)).
			WillReturnRows(sqlmock.NewRows([]string{
				"evidence_ciphertext", "evidence_status", "evidence_expires_at",
			}).AddRow("secret prompt", string(EvidenceEncrypted), nil))
		mock.ExpectExec(`INSERT INTO prompt_audit_evidence_access_logs`).
			WithArgs(int64(11), int64(7), "incident review", "revealed").
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectExec(`INSERT INTO security_audit_evidence_access_logs`).
			WithArgs(int64(23), int64(7), "incident review", "revealed").
			WillReturnError(errors.New("unified audit log unavailable"))

		result, revealErr := service.RevealUnifiedEvidence(
			context.Background(), 23, 7, "incident review",
		)

		require.Nil(t, result)
		require.ErrorContains(t, revealErr, "record unified evidence access before reveal")
		require.NoError(t, mock.ExpectationsWereMet())
	})
}
