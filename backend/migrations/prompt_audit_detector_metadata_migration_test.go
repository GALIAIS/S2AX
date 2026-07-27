package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPromptAuditDetectorMetadataMigrationIsBoundedAndTraceable(t *testing.T) {
	raw, err := FS.ReadFile("301_prompt_audit_detector_metadata.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(raw))
	for _, fragment := range []string{
		"detector_adapter varchar(64)",
		"provider_request_id varchar(160)",
		"finish_reason varchar(32)",
		"model_digest varchar(64)",
		"'qwen3guard_chat', 'openai_moderations', 'strict_json_chat'",
		"model_digest ~ '^[0-9a-f]{64}$'",
		"idx_prompt_audit_events_provider_request",
	} {
		require.Contains(t, sql, fragment)
	}
}
