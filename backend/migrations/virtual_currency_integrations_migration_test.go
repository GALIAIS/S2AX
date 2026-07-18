package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVirtualCurrencyIntegrationsMigrationCreatesEncryptedCredentialsAndScopes(t *testing.T) {
	content, err := FS.ReadFile("184_virtual_currency_integrations.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))
	require.Contains(t, sql, "virtual_currency_integrations")
	require.Contains(t, sql, "secret_ciphertext")
	require.Contains(t, sql, "virtual_currency_integration_scopes")
	require.Contains(t, sql, "can_settle")
	require.Contains(t, sql, "idx_virtual_currency_integration_scopes_lookup")
}
