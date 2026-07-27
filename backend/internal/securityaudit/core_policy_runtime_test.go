package securityaudit

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestResolveActivePolicyForSnapshotDistinguishesNoMatchFromDatabaseFailure(t *testing.T) {
	t.Run("no active policy is an explicit fallback", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })
		mock.ExpectQuery(`SELECT p\.policy_key,p\.version,p\.config`).
			WillReturnRows(sqlmock.NewRows([]string{"policy_key", "version", "config"}))

		_, _, _, selected, resolveErr := resolveActivePolicyForSnapshot(
			context.Background(),
			db,
			PromptSnapshot{UserID: 7, APIKeyID: 9, Protocol: "openai", Endpoint: "/v1/chat/completions"},
		)

		require.NoError(t, resolveErr)
		require.False(t, selected)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("database failure cannot silently bypass governance", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })
		mock.ExpectQuery(`SELECT p\.policy_key,p\.version,p\.config`).
			WillReturnError(errors.New("database unavailable"))

		_, _, _, selected, resolveErr := resolveActivePolicyForSnapshot(
			context.Background(),
			db,
			PromptSnapshot{UserID: 7, APIKeyID: 9, Protocol: "openai", Endpoint: "/v1/chat/completions"},
		)

		require.ErrorContains(t, resolveErr, "database unavailable")
		require.False(t, selected)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("invalid active policy cannot silently bypass governance", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })
		mock.ExpectQuery(`SELECT p\.policy_key,p\.version,p\.config`).
			WillReturnRows(sqlmock.NewRows([]string{"policy_key", "version", "config"}).
				AddRow("broken-policy", int64(3), []byte(`{"scope":`)))

		_, _, _, selected, resolveErr := resolveActivePolicyForSnapshot(
			context.Background(),
			db,
			PromptSnapshot{UserID: 7, APIKeyID: 9, Protocol: "openai", Endpoint: "/v1/chat/completions"},
		)

		require.ErrorContains(t, resolveErr, "decode active policy broken-policy v3")
		require.False(t, selected)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestEvidenceForPolicyModeMinimizesStoredDetectorEvidence(t *testing.T) {
	input := []DetectorEvidence{{
		DetectorID: "remote_guard", Category: "cyber_abuse",
		SafeSummary: "redacted finding", EvidenceDigest: "digest",
	}}

	require.Empty(t, evidenceForPolicyMode(input, "none"))
	digestOnly := evidenceForPolicyMode(input, "digest_only")
	require.Len(t, digestOnly, 1)
	require.Empty(t, digestOnly[0].SafeSummary)
	require.Equal(t, "digest", digestOnly[0].EvidenceDigest)
	require.Equal(t, "redacted finding", input[0].SafeSummary, "policy projection must not mutate detector output")
	require.Equal(t, input, evidenceForPolicyMode(input, "findings_encrypted"))
	require.Equal(t, input, evidenceForPolicyMode(input, "full_encrypted"))
}

func TestEnforcePromptEvidencePolicyAppliesModeAndRetention(t *testing.T) {
	t.Run("non-full mode removes prompt ciphertext", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })
		mock.ExpectQuery(`(?s)UPDATE prompt_audit_events.*evidence_ciphertext=''.*evidence_status='not_stored'.*WHERE id=\$1.*RETURNING id`).
			WithArgs(int64(19)).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(19)))

		err = enforcePromptEvidencePolicy(
			context.Background(),
			db,
			19,
			time.Date(2026, time.July, 24, 4, 0, 0, 0, time.UTC),
			PolicyEvidence{Mode: "findings_encrypted", RetentionDays: 30},
		)

		require.NoError(t, err)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("full mode applies exact policy retention", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })
		createdAt := time.Date(2026, time.July, 24, 4, 0, 0, 0, time.UTC)
		expiresAt := createdAt.Add(7 * 24 * time.Hour)
		mock.ExpectQuery(`(?s)UPDATE prompt_audit_events.*evidence_expires_at=CASE.*WHERE id=\$1.*RETURNING id`).
			WithArgs(int64(19), expiresAt).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(19)))

		err = enforcePromptEvidencePolicy(
			context.Background(),
			db,
			19,
			createdAt,
			PolicyEvidence{Mode: "full_encrypted", RetentionDays: 7},
		)

		require.NoError(t, err)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}
