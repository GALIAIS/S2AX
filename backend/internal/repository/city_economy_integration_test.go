//go:build integration

package repository

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCityEconomyFoundationCreatesIsolatedAuthorizedChart(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("city-owner-%s@example.com", suffix),
		PasswordHash: "integration-test-password",
	})
	outsider := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("city-outsider-%s@example.com", suffix),
		PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	_, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID,
		Name:        "Invalid City",
		Timezone:    "Not/A_Timezone",
	})
	require.ErrorIs(t, err, service.ErrCityInvalidInput)
	scale := 3
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID,
		Name:        "Integration City " + suffix,
		Timezone:    "Asia/Shanghai",
		MonetaryUnit: service.CityMonetaryUnitCreateInput{
			Code: "metro_credit", Name: "Metro Credit", Symbol: "MC", Scale: &scale,
		},
	})
	require.NoError(t, err)
	require.NotNil(t, foundation)
	worldID := foundation.World.ID

	t.Cleanup(func() {
		tx, beginErr := integrationDB.BeginTx(ctx, nil)
		if beginErr == nil {
			_, _ = tx.ExecContext(ctx, "DELETE FROM city_accounts WHERE world_id = $1", worldID)
			_, _ = tx.ExecContext(ctx, "DELETE FROM city_account_templates WHERE world_id = $1", worldID)
			_, _ = tx.ExecContext(ctx, "DELETE FROM city_economic_entities WHERE world_id = $1", worldID)
			_, _ = tx.ExecContext(ctx, "DELETE FROM city_monetary_units WHERE world_id = $1", worldID)
			_, _ = tx.ExecContext(ctx, "DELETE FROM city_members WHERE world_id = $1", worldID)
			_, _ = tx.ExecContext(ctx, "DELETE FROM city_worlds WHERE id = $1", worldID)
			_ = tx.Commit()
		}
		_ = client.User.DeleteOneID(outsider.ID).Exec(ctx)
		_ = client.User.DeleteOneID(owner.ID).Exec(ctx)
	})

	require.Equal(t, service.CityWorldStatusPaused, foundation.World.Status)
	require.Equal(t, service.CitySimulationVersionV1, foundation.World.SimulationVersion)
	require.Equal(t, "owner", foundation.World.MemberRole)
	require.Positive(t, foundation.World.Seed)
	require.Len(t, foundation.MonetaryUnits, 1)
	require.True(t, foundation.MonetaryUnits[0].IsBase)
	require.Equal(t, "metro_credit", foundation.MonetaryUnits[0].Code)
	require.Equal(t, 3, foundation.MonetaryUnits[0].Scale)
	require.Len(t, foundation.Entities, 4)
	require.NotEmpty(t, foundation.AccountTemplates)

	wantedAccounts := map[string]string{
		service.CityEntityTypeHousehold:  "cash",
		service.CityEntityTypeFirm:       "inventory",
		service.CityEntityTypeGovernment: "tax_revenue",
		service.CityEntityTypeClearing:   "rounding",
	}
	for _, entity := range foundation.Entities {
		require.NotEmpty(t, entity.Accounts, entity.EntityType)
		found := false
		for _, account := range entity.Accounts {
			if account.Code == wantedAccounts[entity.EntityType] {
				found = true
			}
			require.Zero(t, account.CurrentBalanceUnits)
		}
		require.True(t, found, "missing required %s account for %s", wantedAccounts[entity.EntityType], entity.EntityType)
	}

	worlds, err := cityService.ListWorlds(ctx, owner.ID)
	require.NoError(t, err)
	require.Len(t, worlds, 1)
	require.Equal(t, worldID, worlds[0].ID)

	_, err = cityService.GetWorld(ctx, outsider.ID, worldID)
	require.ErrorIs(t, err, service.ErrCityWorldNotFound)
	_, err = cityService.CreateWorld(ctx, service.CityWorldCreateInput{OwnerUserID: owner.ID, Name: "Duplicate"})
	require.ErrorIs(t, err, service.ErrCityWorldExists)

	baseUnitTx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = baseUnitTx.ExecContext(ctx, `
UPDATE city_monetary_units SET is_base = FALSE WHERE world_id = $1 AND is_base`, worldID)
	require.NoError(t, err)
	require.ErrorContains(t, baseUnitTx.Commit(), "exactly one base monetary unit")

	ownerTx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = ownerTx.ExecContext(ctx, `
UPDATE city_members SET status = 'left', left_at = NOW()
WHERE world_id = $1 AND user_id = $2`, worldID, owner.ID)
	require.NoError(t, err)
	require.ErrorContains(t, ownerTx.Commit(), "exactly one active owner membership")

	var protectedAccountID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT id FROM city_accounts
WHERE world_id = $1 AND allow_negative = FALSE
ORDER BY id ASC LIMIT 1`, worldID).Scan(&protectedAccountID))
	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_accounts SET current_balance_units = -1 WHERE id = $1`, protectedAccountID)
	require.ErrorContains(t, err, "only change through a draft journal")

	var firmID, unitID, householdTemplateID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT id FROM city_economic_entities
WHERE world_id = $1 AND entity_type = 'firm' LIMIT 1`, worldID).Scan(&firmID))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT id FROM city_monetary_units WHERE world_id = $1 AND is_base LIMIT 1`, worldID).Scan(&unitID))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT id FROM city_account_templates
WHERE world_id = $1 AND entity_type = 'household' LIMIT 1`, worldID).Scan(&householdTemplateID))
	_, err = integrationDB.ExecContext(ctx, `
INSERT INTO city_accounts
    (world_id, entity_id, entity_type, monetary_unit_id, template_id,
     allow_negative, current_balance_units, version, status, metadata)
VALUES ($1, $2, 'firm', $3, $4, FALSE, 0, 0, 'active', '{}'::jsonb)`,
		worldID, firmID, unitID, householdTemplateID)
	require.ErrorContains(t, err, "city_accounts_template_fk")
}
