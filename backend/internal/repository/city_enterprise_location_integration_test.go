//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCityEnterpriseLocationCommandsRelocationConservationAndReplay(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("city-enterprise-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	outsider := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("city-enterprise-outsider-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(750041)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Enterprise Location Protocol City", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionF7V4,
	})
	require.NoError(t, err)
	worldID := foundation.World.ID

	initial, err := cityService.GetEnterpriseLocationState(ctx, service.CityEnterpriseLocationQueryInput{
		UserID: owner.ID, WorldID: worldID,
	})
	require.NoError(t, err)
	require.Equal(t, int64(2), initial.Profile.BaselineSiteCount)
	require.Equal(t, int64(2), initial.Profile.SiteCount)
	require.Len(t, initial.BaselineSites, 2)
	require.Len(t, initial.Sites, 2)
	require.Empty(t, initial.Facts)
	_, err = cityService.GetEnterpriseLocationState(ctx, service.CityEnterpriseLocationQueryInput{
		UserID: outsider.ID, WorldID: worldID,
	})
	require.ErrorIs(t, err, service.ErrCityWorldNotFound)

	var firmID, employees, productionCapacity int64
	var firmCode, sourceDistrict string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT entity.id, entity.code, district.code, firm.employee_units,
       firm.production_capacity_units
FROM city_firm_states firm
JOIN city_economic_entities entity
  ON entity.id = firm.entity_id AND entity.world_id = firm.world_id
JOIN city_districts district
  ON district.id = firm.district_id AND district.world_id = firm.world_id
WHERE firm.world_id = $1 AND entity.status = 'active'
ORDER BY entity.code ASC LIMIT 1`, worldID).Scan(
		&firmID, &firmCode, &sourceDistrict, &employees, &productionCapacity,
	))
	officeUnits := (employees + 3) / 4
	headquartersUnits := officeUnits
	productionUnits := (productionCapacity + 1) / 2

	var officePool string
	require.NoError(t, integrationDB.QueryRowContext(ctx, enterpriseAvailablePoolSQL+`
  AND pool_district.code = $2 AND pool.use_type = 'commercial'
  AND pool.unit_count + COALESCE(adjustment.added_capacity, 0) / pool.capacity_units_per_unit
      - COALESCE(occupied.units, 0) >= $3
ORDER BY pool.code ASC LIMIT 1`, worldID, sourceDistrict, officeUnits).Scan(&officePool))

	marshalPayload := func(value any) json.RawMessage {
		raw, marshalErr := json.Marshal(value)
		require.NoError(t, marshalErr)
		return raw
	}
	submitAndStep := func(expectedTick int64, key, commandType string, payload json.RawMessage) *service.CityStepResult {
		_, submitErr := cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: key + "-command",
			CommandType: commandType, Payload: payload, ExpectedWorldTick: &expectedTick,
		})
		require.NoError(t, submitErr)
		step, stepErr := cityService.StepWorld(ctx, service.CityStepInput{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: key + "-step",
			ExpectedWorldTick: &expectedTick,
		})
		require.NoError(t, stepErr)
		require.Len(t, step.Commands, 1)
		require.Equal(t, service.CityCommandStatusApplied, step.Commands[0].Status, "%+v", step.Commands[0])
		require.Len(t, step.EnterpriseLocationFacts, 1)
		return step
	}

	opened := submitAndStep(0, "enterprise-open", service.CityCommandTypeEnterpriseSiteOpen,
		marshalPayload(map[string]any{
			"firm_entity_id": firmID, "pool_code": officePool,
			"site_type": service.CityEnterpriseSiteOffice, "name": "Regional Operations Office",
			"target_occupied_units": officeUnits,
		}))
	require.Equal(t, service.CityEnterpriseLocationFactOpened, opened.EnterpriseLocationFacts[0].FactType)
	openedSiteCode := *opened.EnterpriseLocationFacts[0].SiteCode

	resized := submitAndStep(1, "enterprise-resize", service.CityCommandTypeEnterpriseSiteResize,
		marshalPayload(map[string]any{
			"site_code": openedSiteCode, "target_occupied_units": officeUnits + 1,
		}))
	require.Equal(t, service.CityEnterpriseLocationFactResized, resized.EnterpriseLocationFacts[0].FactType)
	closed := submitAndStep(2, "enterprise-close", service.CityCommandTypeEnterpriseSiteClose,
		marshalPayload(map[string]any{
			"site_code": openedSiteCode, "reason": "Consolidated before inter-district relocation",
		}))
	require.Equal(t, service.CityEnterpriseLocationFactClosed, closed.EnterpriseLocationFacts[0].FactType)

	var targetDistrict, headquartersPool, productionPool string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT district.code,
       (SELECT pool.code `+enterpriseAvailablePoolFromSQL+`
        WHERE pool.world_id = $1 AND pool.district_id = district.id
          AND pool.use_type = 'commercial'
          AND pool.unit_count + COALESCE(adjustment.added_capacity, 0) / pool.capacity_units_per_unit
              - COALESCE(occupied.units, 0) >= $2
        ORDER BY pool.code ASC LIMIT 1),
       (SELECT pool.code `+enterpriseAvailablePoolFromSQL+`
        WHERE pool.world_id = $1 AND pool.district_id = district.id
          AND pool.use_type = 'industrial'
          AND pool.unit_count + COALESCE(adjustment.added_capacity, 0) / pool.capacity_units_per_unit
              - COALESCE(occupied.units, 0) >= $3
        ORDER BY pool.code ASC LIMIT 1)
FROM city_districts district
WHERE district.world_id = $1 AND district.code <> $4
  AND EXISTS (SELECT 1 `+enterpriseAvailablePoolFromSQL+`
              WHERE pool.world_id = $1 AND pool.district_id = district.id
                AND pool.use_type = 'commercial'
                AND pool.unit_count + COALESCE(adjustment.added_capacity, 0) / pool.capacity_units_per_unit
                    - COALESCE(occupied.units, 0) >= $2)
  AND EXISTS (SELECT 1 `+enterpriseAvailablePoolFromSQL+`
              WHERE pool.world_id = $1 AND pool.district_id = district.id
                AND pool.use_type = 'industrial'
                AND pool.unit_count + COALESCE(adjustment.added_capacity, 0) / pool.capacity_units_per_unit
                    - COALESCE(occupied.units, 0) >= $3)
ORDER BY district.sort_order ASC LIMIT 1`,
		worldID, headquartersUnits, productionUnits, sourceDistrict).Scan(
		&targetDistrict, &headquartersPool, &productionPool,
	))
	var sourceInventoryBefore int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COALESCE(SUM(balance.opening_quantity_units), 0)::BIGINT
FROM city_inventory_balances balance
JOIN city_districts district ON district.id = balance.district_id
WHERE balance.world_id = $1 AND balance.entity_id = $2 AND district.code = $3`,
		worldID, firmID, sourceDistrict).Scan(&sourceInventoryBefore))
	require.Positive(t, sourceInventoryBefore)

	relocated := submitAndStep(3, "enterprise-relocate", service.CityCommandTypeEnterpriseRelocate,
		marshalPayload(map[string]any{
			"firm_entity_id": firmID, "headquarters_pool_code": headquartersPool,
			"production_pool_code": productionPool, "reason": "Move operating center",
		}))
	require.Equal(t, service.CityEnterpriseLocationFactRelocated, relocated.EnterpriseLocationFacts[0].FactType)
	var relocationOperation *service.CityResourceOperation
	for _, operation := range relocated.ResourceOperations {
		if operation.Metadata["enterprise_location_fact_id"] != nil {
			relocationOperation = operation
			break
		}
	}
	require.NotNil(t, relocationOperation)
	require.Equal(t, "transfer", relocationOperation.OperationType)
	require.GreaterOrEqual(t, relocationOperation.EntryCount, 2)
	require.Equal(t, relocationOperation.IncomingUnits, relocationOperation.OutgoingUnits)

	state, err := cityService.GetEnterpriseLocationState(ctx, service.CityEnterpriseLocationQueryInput{
		UserID: owner.ID, WorldID: worldID, FirmCode: firmCode,
	})
	require.NoError(t, err)
	require.Equal(t, int64(4), state.Profile.FactCount)
	require.Equal(t, int64(5), state.Profile.SiteCount)
	require.Len(t, state.Facts, 4)
	activeByType := make(map[string]int)
	for _, site := range state.Sites {
		if site.Status == service.CityEnterpriseSiteStatusActive {
			activeByType[site.SiteType]++
			if site.IsPrimary {
				require.Equal(t, targetDistrict, site.DistrictCode)
			}
		}
	}
	require.Equal(t, 1, activeByType[service.CityEnterpriseSiteHeadquarters])
	require.Equal(t, 1, activeByType[service.CityEnterpriseSiteProduction])

	var firmDistrict string
	var sourceInventoryAfter, targetInventoryAfter int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT district.code
FROM city_firm_states firm
JOIN city_districts district ON district.id = firm.district_id
WHERE firm.world_id = $1 AND firm.entity_id = $2`, worldID, firmID).Scan(&firmDistrict))
	require.Equal(t, targetDistrict, firmDistrict)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COALESCE(SUM(balance.quantity_units), 0)::BIGINT
FROM city_inventory_balances balance
JOIN city_districts district ON district.id = balance.district_id
WHERE balance.world_id = $1 AND balance.entity_id = $2 AND district.code = $3`,
		worldID, firmID, sourceDistrict).Scan(&sourceInventoryAfter))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COALESCE(SUM(balance.quantity_units), 0)::BIGINT
FROM city_inventory_balances balance
JOIN city_districts district ON district.id = balance.district_id
WHERE balance.world_id = $1 AND balance.entity_id = $2 AND district.code = $3`,
		worldID, firmID, targetDistrict).Scan(&targetInventoryAfter))
	require.Zero(t, sourceInventoryAfter)
	require.Equal(t, sourceInventoryBefore, targetInventoryAfter)

	fromGenesis, targetTick := int64(0), int64(4)
	replay, err := cityService.StartReplay(ctx, service.CityReplayInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "enterprise-replay",
		FromTick: &fromGenesis, TargetTick: &targetTick,
	})
	require.NoError(t, err)
	if replay.ErrorDetail != nil {
		t.Logf("enterprise replay detail: %s", *replay.ErrorDetail)
	}
	require.Equal(t, service.CityReplayStatusVerified, replay.Status, "%+v", replay)
	recovery, err := cityService.StartRecovery(ctx, service.CityRecoveryInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "enterprise-recovery",
		ReplayRunID: replay.ID,
	})
	require.NoError(t, err)
	if recovery.ErrorDetail != nil {
		t.Logf("enterprise recovery detail: %s", *recovery.ErrorDetail)
	}
	require.Equal(t, service.CityRecoveryStatusApplied, recovery.Status, "%+v", recovery)
	restored, err := cityService.GetEnterpriseLocationState(ctx, service.CityEnterpriseLocationQueryInput{
		UserID: owner.ID, WorldID: worldID, FirmCode: firmCode,
	})
	require.NoError(t, err)
	require.Equal(t, state.Profile, restored.Profile)
	require.Equal(t, state.Sites, restored.Sites)
	require.Equal(t, state.Facts, restored.Facts)
}

func TestCityF7V3ToV4UpgradeCreatesAuditedEnterpriseBaseline(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("city-enterprise-upgrade-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(750042)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Enterprise Upgrade City", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionF7V3,
	})
	require.NoError(t, err)
	worldID := foundation.World.ID
	landBefore, err := cityService.GetLandState(ctx, service.CityLandQueryInput{
		UserID: owner.ID, WorldID: worldID,
		MinimumX: -4, MaximumX: 4, MinimumY: -4, MaximumY: 4, Z: 0,
	})
	require.NoError(t, err)
	_, err = cityService.GetEnterpriseLocationState(ctx, service.CityEnterpriseLocationQueryInput{
		UserID: owner.ID, WorldID: worldID,
	})
	require.ErrorIs(t, err, service.ErrCityEnterpriseLocationStateNotFound)
	engineBefore, err := cityService.GetEngineInfo(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, []string{service.CitySimulationVersionF7V4}, engineBefore.UpgradeTargets)

	planned, err := cityService.StartUpgrade(ctx, service.CityUpgradeInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "enterprise-upgrade-plan",
		TargetVersion: service.CitySimulationVersionF7V4, DryRun: true,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityUpgradeStatusPlanned, planned.Status, "%+v", planned)
	var profileCount int64
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM city_enterprise_location_profiles WHERE world_id = $1`, worldID,
	).Scan(&profileCount))
	require.Zero(t, profileCount)

	applied, err := cityService.StartUpgrade(ctx, service.CityUpgradeInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "enterprise-upgrade-apply",
		TargetVersion: service.CitySimulationVersionF7V4,
	})
	require.NoError(t, err)
	if applied.ErrorDetail != nil {
		t.Logf("enterprise upgrade detail: %s", *applied.ErrorDetail)
	}
	require.Equal(t, service.CityUpgradeStatusApplied, applied.Status, "%+v", applied)
	require.NotNil(t, applied.TargetSnapshotID)
	engineAfter, err := cityService.GetEngineInfo(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, service.CitySimulationVersionF7V4, engineAfter.Version)
	require.Equal(t, []string{service.CitySimulationVersionF7V5}, engineAfter.UpgradeTargets)
	require.Contains(t, engineAfter.Stages, "enterprise_location")
	state, err := cityService.GetEnterpriseLocationState(ctx, service.CityEnterpriseLocationQueryInput{
		UserID: owner.ID, WorldID: worldID,
	})
	require.NoError(t, err)
	require.Equal(t, int64(2), state.Profile.BaselineSiteCount)
	require.Equal(t, state.Profile.BaselineSiteCount, state.Profile.SiteCount)
	require.Zero(t, state.Profile.FactCount)
	landAfter, err := cityService.GetLandState(ctx, service.CityLandQueryInput{
		UserID: owner.ID, WorldID: worldID,
		MinimumX: -4, MaximumX: 4, MinimumY: -4, MaximumY: 4, Z: 0,
	})
	require.NoError(t, err)
	require.Equal(t, landBefore.Profile.BaselineHash, landAfter.Profile.BaselineHash)
	require.Equal(t, landBefore.Buildings, landAfter.Buildings)

	expectedZero := int64(0)
	step, err := cityService.StepWorld(ctx, service.CityStepInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "enterprise-upgraded-step",
		ExpectedWorldTick: &expectedZero,
	})
	require.NoError(t, err)
	require.Equal(t, service.CitySimulationVersionF7V4, step.Tick.SimulationVersion)
	fromUpgrade, targetTick := int64(0), int64(1)
	replay, err := cityService.StartReplay(ctx, service.CityReplayInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "enterprise-upgraded-replay",
		FromTick: &fromUpgrade, TargetTick: &targetTick,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityReplayStatusVerified, replay.Status, "%+v", replay)
}

const enterpriseAvailablePoolSQL = `
SELECT pool.code ` + enterpriseAvailablePoolFromSQL + `
WHERE pool.world_id = $1 AND building.status = 'active' AND parcel.status = 'active'`

const enterpriseAvailablePoolFromSQL = `
FROM city_building_unit_pools pool
JOIN city_buildings building
  ON building.id = pool.building_id AND building.world_id = pool.world_id
JOIN city_parcels parcel
  ON parcel.id = building.parcel_id AND parcel.world_id = building.world_id
JOIN city_districts pool_district
  ON pool_district.id = pool.district_id AND pool_district.world_id = pool.world_id
LEFT JOIN LATERAL (
    SELECT COALESCE(SUM(value.added_capacity_units), 0)::BIGINT AS added_capacity
    FROM city_building_adjustments value
    WHERE value.world_id = pool.world_id AND value.building_id = pool.building_id
) adjustment ON TRUE
LEFT JOIN LATERAL (
    SELECT COALESCE(SUM(site.occupied_units), 0)::BIGINT AS units
    FROM city_enterprise_sites site
    WHERE site.world_id = pool.world_id AND site.pool_id = pool.id AND site.status = 'active'
) occupied ON TRUE`
