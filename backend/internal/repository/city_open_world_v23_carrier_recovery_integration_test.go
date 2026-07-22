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

// TestCityOpenWorldV23CarrierRecoveryClosesOnlyFundedCarrierClaims proves the
// manual-reserve boundary end to end: V22 creates the carrier liability, V23
// rejects an unfunded recovery, admits only an audited government reserve
// funding, rejects generic ledger access to the reserve, then settles exactly
// one seller claim and survives replay/recovery without exposing reserve data
// to an unrelated viewer.
func TestCityOpenWorldV23CarrierRecoveryClosesOnlyFundedCarrierClaims(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("open-world-v23-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	viewer := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("open-world-v23-viewer-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(23_230_001)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Open World V23 Carrier Recovery", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionOpenWorldV23,
		StyleProfileID:    "jp.metropolitan", SpawnPolicy: "city_center",
	})
	require.NoError(t, err)
	worldID := foundation.World.ID
	engine, err := cityService.GetEngineInfo(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, service.CitySimulationVersionOpenWorldV23, engine.Version)
	require.NotNil(t, engine.VersionVector)
	contentCatalog := requireCityOpenWorldVersionBinding(t, engine.VersionVector, "content_catalog")
	require.Equal(t, "sub2api-open-world-carrier-recovery-catalog", contentCatalog.BundleID)
	_, err = cityService.AddWorldMember(ctx, service.CityMemberAddInput{
		UserID: owner.ID, WorldID: worldID, Identity: viewer.Email, Role: service.CityMemberRoleViewer,
	})
	require.NoError(t, err)

	initialRecovery, err := cityService.GetCityOpenWorldCarrierRecoveryState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Empty(t, initialRecovery.Fundings)
	require.Empty(t, initialRecovery.Recoveries)
	require.Equal(t, "system_freight_reserve", initialRecovery.Policy.CarrierFirmCode)

	var buyerEntityID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT id
FROM city_economic_entities
WHERE world_id = $1 AND entity_type = 'firm' AND code = 'openworld_trade_buyer'`, worldID).Scan(&buyerEntityID))

	currentTick := int64(0)
	submit := func(key, commandType string, payload map[string]any) *service.CityCommand {
		t.Helper()
		raw, marshalErr := json.Marshal(payload)
		require.NoError(t, marshalErr)
		command, submitErr := cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: key,
			CommandType: commandType, Payload: raw, ExpectedWorldTick: &currentTick,
		})
		require.NoError(t, submitErr)
		return command
	}
	step := func(key string) *service.CityStepResult {
		t.Helper()
		result, stepErr := cityService.StepWorld(ctx, service.CityStepInput{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: key, ExpectedWorldTick: &currentTick,
		})
		require.NoError(t, stepErr)
		require.NotNil(t, result.Tick)
		require.Equal(t, currentTick+1, result.Tick.Tick)
		currentTick = result.Tick.Tick
		return result
	}
	submitAndStep := func(key, commandType string, payload map[string]any) *service.CityStepResult {
		t.Helper()
		command := submit(key, commandType, payload)
		result := step(key + "-step")
		require.Len(t, result.Commands, 1)
		require.Equal(t, command.ID, result.Commands[0].ID)
		require.Equal(t, service.CityCommandStatusApplied, result.Commands[0].Status)
		return result
	}

	submitAndStep("v23-fund-buyer", service.CityCommandTypeLedgerSubsidy, map[string]any{
		"recipient_entity_id": buyerEntityID, "amount_units": int64(1_000), "memo": "fund V23 settlement buyer",
	})
	submitAndStep("v23-create-order", service.CityCommandTypeOpenWorldSupplyOrderCreate, map[string]any{
		"buyer_node_code": "supply.node.openworld_trade_buyer", "seller_node_code": "supply.node.municipal_services",
		"lines": []map[string]any{{"resource_code": "basic_material", "quantity_units": int64(12), "unit_price_units": int64(5)}},
	})
	supply, err := cityService.GetCityOpenWorldSupplyChainState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, supply.Orders, 1)
	orderCode := supply.Orders[0].Code
	submitAndStep("v23-accept-order", service.CityCommandTypeOpenWorldSupplyOrderAccept, map[string]any{"order_code": orderCode})
	submitAndStep("v23-dispatch-order", service.CityCommandTypeOpenWorldSupplyOrderDispatch, map[string]any{"order_code": orderCode})

	var actionableCase service.CityOpenWorldFreightSettlementCase
	for attempt := 0; attempt < 64; attempt++ {
		step(fmt.Sprintf("v23-progress-%d", currentTick+1))
		settlements, settlementErr := cityService.GetCityOpenWorldFreightSettlementState(ctx, owner.ID, worldID)
		require.NoError(t, settlementErr)
		for _, settlementCase := range settlements.Cases {
			if settlementCase.State == "awaiting_outcome" && settlementCase.TransportState == "awaiting_receipt" {
				actionableCase = settlementCase
				break
			}
		}
		if actionableCase.Code != "" {
			break
		}
	}
	require.NotEmpty(t, actionableCase.Code, "V23 predecessor did not create a receipt-ready V22 case")

	settlementResult := submitAndStep("v23-record-carrier-loss", service.CityCommandTypeOpenWorldFreightSettlementReceipt, map[string]any{
		"case_code": actionableCase.Code, "liability_party": "carrier",
		"lines": []map[string]any{{"source_line_no": 1, "accepted_units": int64(10), "lost_units": int64(2), "rejected_units": int64(0)}},
	})
	require.Len(t, settlementResult.ResourceOperations, 1)
	require.Len(t, settlementResult.Journals, 1)
	settlements, err := cityService.GetCityOpenWorldFreightSettlementState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, settlements.Claims, 1)
	claim := settlements.Claims[0]
	require.Equal(t, "carrier", claim.LiabilityParty)
	require.Equal(t, "open", claim.State)
	require.Positive(t, claim.ClaimAmount)

	insufficientCommand := submit("v23-unfunded-claim-resolution", service.CityCommandTypeOpenWorldFreightClaimResolve, map[string]any{
		"claim_code": claim.Code, "memo": "must require funded reserve",
	})
	insufficientStep := step("v23-unfunded-claim-resolution-step")
	require.Len(t, insufficientStep.Commands, 1)
	require.Equal(t, insufficientCommand.ID, insufficientStep.Commands[0].ID)
	require.Equal(t, service.CityCommandStatusRejected, insufficientStep.Commands[0].Status)
	require.NotNil(t, insufficientStep.Commands[0].ErrorCode)
	require.Equal(t, "CITY_LEDGER_INSUFFICIENT_BALANCE", *insufficientStep.Commands[0].ErrorCode)

	fundAmount := claim.ClaimAmount + 7
	fundingResult := submitAndStep("v23-fund-carrier-reserve", service.CityCommandTypeOpenWorldCarrierReserveFund, map[string]any{
		"amount_units": fundAmount, "memo": "manually fund audited carrier reserve",
	})
	require.Len(t, fundingResult.Journals, 1)
	require.Equal(t, "subsidy", fundingResult.Journals[0].JournalType)

	var reserveEntityID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT id FROM city_economic_entities
WHERE world_id = $1 AND code = 'system_freight_reserve'`, worldID).Scan(&reserveEntityID))
	genericSubsidy := submit("v23-generic-reserve-subsidy-denied", service.CityCommandTypeLedgerSubsidy, map[string]any{
		"recipient_entity_id": reserveEntityID, "amount_units": int64(1), "memo": "must use V23 reserve funding",
	})
	genericSubsidyStep := step("v23-generic-reserve-subsidy-denied-step")
	require.Len(t, genericSubsidyStep.Commands, 1)
	require.Equal(t, genericSubsidy.ID, genericSubsidyStep.Commands[0].ID)
	require.Equal(t, service.CityCommandStatusRejected, genericSubsidyStep.Commands[0].Status)
	require.NotNil(t, genericSubsidyStep.Commands[0].ErrorCode)
	require.Equal(t, "CITY_LEDGER_RESERVED_ENTITY_CONTROLLED", *genericSubsidyStep.Commands[0].ErrorCode)

	recoveryResult := submitAndStep("v23-resolve-carrier-claim", service.CityCommandTypeOpenWorldFreightClaimResolve, map[string]any{
		"claim_code": claim.Code, "memo": "settle audited carrier liability",
	})
	require.Len(t, recoveryResult.Journals, 1)
	require.Equal(t, "cash_transfer", recoveryResult.Journals[0].JournalType)

	recoveryState, err := cityService.GetCityOpenWorldCarrierRecoveryState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, recoveryState.Fundings, 1)
	require.Len(t, recoveryState.Recoveries, 1)
	require.Equal(t, fundAmount, recoveryState.Policy.FundedUnits)
	require.Equal(t, claim.ClaimAmount, recoveryState.Policy.RecoveredUnits)
	require.Equal(t, claim.Code, recoveryState.Recoveries[0].ClaimCode)
	require.Equal(t, claim.ClaimAmount, recoveryState.Recoveries[0].AmountUnits)

	settlements, err = cityService.GetCityOpenWorldFreightSettlementState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, settlements.Claims, 1)
	require.Equal(t, "resolved", settlements.Claims[0].State)

	var reserveCash int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT account.current_balance_units
FROM city_accounts account
JOIN city_economic_entities entity ON entity.id = account.entity_id
JOIN city_account_templates template ON template.id = account.template_id
WHERE account.world_id = $1 AND entity.code = 'system_freight_reserve' AND template.code = 'cash'`, worldID).Scan(&reserveCash))
	require.Equal(t, fundAmount-claim.ClaimAmount, reserveCash)

	viewerState, err := cityService.GetCityOpenWorldCarrierRecoveryState(ctx, viewer.ID, worldID)
	require.NoError(t, err)
	require.Empty(t, viewerState.Fundings)
	require.Empty(t, viewerState.Recoveries)
	require.Empty(t, viewerState.Policy.CarrierFirmCode)
	require.Zero(t, viewerState.Policy.FundedUnits)
	require.Zero(t, viewerState.Policy.RecoveredUnits)

	fromGenesis, targetTick := int64(0), currentTick
	replay, err := cityService.StartReplay(ctx, service.CityReplayInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v23-carrier-recovery-replay",
		FromTick: &fromGenesis, TargetTick: &targetTick,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityReplayStatusVerified, replay.Status, "%+v", replay)
	recovery, err := cityService.StartRecovery(ctx, service.CityRecoveryInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v23-carrier-recovery-restore", ReplayRunID: replay.ID,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityRecoveryStatusApplied, recovery.Status, "%+v", recovery)
	restored, err := cityService.GetCityOpenWorldCarrierRecoveryState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, recoveryState, restored)
}

// TestCityOpenWorldV23UpgradePreservesOpenCarrierClaimForFundedRecovery
// proves that V23 can close a real claim created before the upgrade. The
// upgrade itself must remain purely additive: it creates an empty reserve,
// preserves the V22 claim unchanged, and requires a later audited funding
// before the owner can resolve it.
func TestCityOpenWorldV23UpgradePreservesOpenCarrierClaimForFundedRecovery(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("open-world-v23-upgrade-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(23_230_024)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Open World V23 Carrier Recovery Upgrade", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionOpenWorldV22,
		StyleProfileID:    "jp.metropolitan", SpawnPolicy: "city_center",
	})
	require.NoError(t, err)
	worldID := foundation.World.ID

	var buyerEntityID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT id
FROM city_economic_entities
WHERE world_id = $1 AND entity_type = 'firm' AND code = 'openworld_trade_buyer'`, worldID).Scan(&buyerEntityID))

	currentTick := int64(0)
	submit := func(key, commandType string, payload map[string]any) *service.CityCommand {
		t.Helper()
		raw, marshalErr := json.Marshal(payload)
		require.NoError(t, marshalErr)
		command, submitErr := cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: key,
			CommandType: commandType, Payload: raw, ExpectedWorldTick: &currentTick,
		})
		require.NoError(t, submitErr)
		return command
	}
	step := func(key string) *service.CityStepResult {
		t.Helper()
		result, stepErr := cityService.StepWorld(ctx, service.CityStepInput{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: key, ExpectedWorldTick: &currentTick,
		})
		require.NoError(t, stepErr)
		require.NotNil(t, result.Tick)
		require.Equal(t, currentTick+1, result.Tick.Tick)
		currentTick = result.Tick.Tick
		return result
	}
	submitAndStep := func(key, commandType string, payload map[string]any) *service.CityStepResult {
		t.Helper()
		command := submit(key, commandType, payload)
		result := step(key + "-step")
		require.Len(t, result.Commands, 1)
		require.Equal(t, command.ID, result.Commands[0].ID)
		require.Equal(t, service.CityCommandStatusApplied, result.Commands[0].Status)
		return result
	}

	submitAndStep("v23-upgrade-fund-buyer", service.CityCommandTypeLedgerSubsidy, map[string]any{
		"recipient_entity_id": buyerEntityID, "amount_units": int64(1_000), "memo": "fund upgrade settlement buyer",
	})
	submitAndStep("v23-upgrade-create-order", service.CityCommandTypeOpenWorldSupplyOrderCreate, map[string]any{
		"buyer_node_code": "supply.node.openworld_trade_buyer", "seller_node_code": "supply.node.municipal_services",
		"lines": []map[string]any{{"resource_code": "basic_material", "quantity_units": int64(12), "unit_price_units": int64(5)}},
	})
	supply, err := cityService.GetCityOpenWorldSupplyChainState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, supply.Orders, 1)
	orderCode := supply.Orders[0].Code
	submitAndStep("v23-upgrade-accept-order", service.CityCommandTypeOpenWorldSupplyOrderAccept, map[string]any{"order_code": orderCode})
	submitAndStep("v23-upgrade-dispatch-order", service.CityCommandTypeOpenWorldSupplyOrderDispatch, map[string]any{"order_code": orderCode})

	var actionableCase service.CityOpenWorldFreightSettlementCase
	for attempt := 0; attempt < 64; attempt++ {
		step(fmt.Sprintf("v23-upgrade-progress-%d", currentTick+1))
		settlements, settlementErr := cityService.GetCityOpenWorldFreightSettlementState(ctx, owner.ID, worldID)
		require.NoError(t, settlementErr)
		for _, settlementCase := range settlements.Cases {
			if settlementCase.State == "awaiting_outcome" && settlementCase.TransportState == "awaiting_receipt" {
				actionableCase = settlementCase
				break
			}
		}
		if actionableCase.Code != "" {
			break
		}
	}
	require.NotEmpty(t, actionableCase.Code, "V22 predecessor did not create a receipt-ready claim case")
	submitAndStep("v23-upgrade-record-carrier-loss", service.CityCommandTypeOpenWorldFreightSettlementReceipt, map[string]any{
		"case_code": actionableCase.Code, "liability_party": "carrier",
		"lines": []map[string]any{{"source_line_no": 1, "accepted_units": int64(10), "lost_units": int64(2), "rejected_units": int64(0)}},
	})
	settlements, err := cityService.GetCityOpenWorldFreightSettlementState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, settlements.Claims, 1)
	claim := settlements.Claims[0]
	require.Equal(t, "carrier", claim.LiabilityParty)
	require.Equal(t, "open", claim.State)
	require.Positive(t, claim.ClaimAmount)

	upgradeTick := currentTick
	upgrade, err := cityService.StartUpgrade(ctx, service.CityUpgradeInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v22-v23-carrier-recovery-upgrade",
		TargetVersion: service.CitySimulationVersionOpenWorldV23,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityUpgradeStatusApplied, upgrade.Status, "%+v", upgrade)
	engine, err := cityService.GetEngineInfo(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, service.CitySimulationVersionOpenWorldV23, engine.Version)
	require.NotNil(t, engine.VersionVector)
	require.Equal(t, upgradeTick, engine.VersionVector.BaselineTick)
	contentCatalog := requireCityOpenWorldVersionBinding(t, engine.VersionVector, "content_catalog")
	require.Equal(t, "sub2api-open-world-carrier-recovery-catalog", contentCatalog.BundleID)

	carrierRecovery, err := cityService.GetCityOpenWorldCarrierRecoveryState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, upgradeTick, carrierRecovery.Policy.BaselineTick)
	require.Empty(t, carrierRecovery.Fundings)
	require.Empty(t, carrierRecovery.Recoveries)
	settlements, err = cityService.GetCityOpenWorldFreightSettlementState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, settlements.Claims, 1)
	require.Equal(t, "open", settlements.Claims[0].State)
	require.Equal(t, claim, settlements.Claims[0])

	submitAndStep("v23-upgrade-fund-carrier-reserve", service.CityCommandTypeOpenWorldCarrierReserveFund, map[string]any{
		"amount_units": claim.ClaimAmount, "memo": "fund pre-upgrade carrier claim",
	})
	submitAndStep("v23-upgrade-resolve-carrier-claim", service.CityCommandTypeOpenWorldFreightClaimResolve, map[string]any{
		"claim_code": claim.Code, "memo": "resolve pre-upgrade carrier claim",
	})
	carrierRecovery, err = cityService.GetCityOpenWorldCarrierRecoveryState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, carrierRecovery.Fundings, 1)
	require.Len(t, carrierRecovery.Recoveries, 1)
	require.Equal(t, claim.Code, carrierRecovery.Recoveries[0].ClaimCode)
	settlements, err = cityService.GetCityOpenWorldFreightSettlementState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, settlements.Claims, 1)
	require.Equal(t, "resolved", settlements.Claims[0].State)
}
