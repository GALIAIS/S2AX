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

// TestCityOpenWorldV24CarrierCommerceQuotesAndSettlesAfterTheNextTick proves
// the V24 causal boundary end to end. A settled V22 case is first quoted on a
// later automatic tick, then its seller pays exactly once on the following
// tick. The resulting cash journal survives verified replay and recovery.
func TestCityOpenWorldV24CarrierCommerceQuotesAndSettlesAfterTheNextTick(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("open-world-v24-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	viewer := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("open-world-v24-viewer-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(24_240_001)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Open World V24 Carrier Commerce", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionOpenWorldV24,
		StyleProfileID:    "jp.metropolitan", SpawnPolicy: "city_center",
	})
	require.NoError(t, err)
	worldID := foundation.World.ID
	engine, err := cityService.GetEngineInfo(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, service.CitySimulationVersionOpenWorldV24, engine.Version)
	require.NotNil(t, engine.VersionVector)
	contentCatalog := requireCityOpenWorldVersionBinding(t, engine.VersionVector, "content_catalog")
	require.Equal(t, "sub2api-open-world-carrier-commerce-catalog", contentCatalog.BundleID)
	_, err = cityService.AddWorldMember(ctx, service.CityMemberAddInput{
		UserID: owner.ID, WorldID: worldID, Identity: viewer.Email, Role: service.CityMemberRoleViewer,
	})
	require.NoError(t, err)

	commerce, err := cityService.GetCityOpenWorldCarrierCommerceState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Empty(t, commerce.Contracts)
	require.Empty(t, commerce.Payments)
	require.Equal(t, "system_freight_reserve", commerce.Policy.CarrierFirmCode)
	require.Equal(t, int64(0), commerce.Policy.BaselineTick)

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

	submitAndStep("v24-fund-buyer", service.CityCommandTypeLedgerSubsidy, map[string]any{
		"recipient_entity_id": buyerEntityID, "amount_units": int64(1_000), "memo": "fund V24 freight buyer",
	})
	submitAndStep("v24-create-order", service.CityCommandTypeOpenWorldSupplyOrderCreate, map[string]any{
		"buyer_node_code": "supply.node.openworld_trade_buyer", "seller_node_code": "supply.node.municipal_services",
		"lines": []map[string]any{{"resource_code": "basic_material", "quantity_units": int64(12), "unit_price_units": int64(5)}},
	})
	supply, err := cityService.GetCityOpenWorldSupplyChainState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, supply.Orders, 1)
	orderCode := supply.Orders[0].Code
	submitAndStep("v24-accept-order", service.CityCommandTypeOpenWorldSupplyOrderAccept, map[string]any{"order_code": orderCode})
	submitAndStep("v24-dispatch-order", service.CityCommandTypeOpenWorldSupplyOrderDispatch, map[string]any{"order_code": orderCode})

	var actionableCase service.CityOpenWorldFreightSettlementCase
	for attempt := 0; attempt < 64; attempt++ {
		step(fmt.Sprintf("v24-progress-%d", currentTick+1))
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
	require.NotEmpty(t, actionableCase.Code, "V24 predecessor did not create a receipt-ready V22 case")

	receiptResult := submitAndStep("v24-record-full-receipt", service.CityCommandTypeOpenWorldFreightSettlementReceipt, map[string]any{
		"case_code": actionableCase.Code, "liability_party": "seller",
		"lines": []map[string]any{{"source_line_no": 1, "accepted_units": int64(12), "lost_units": int64(0), "rejected_units": int64(0)}},
	})
	require.Len(t, receiptResult.ResourceOperations, 1)
	// A fully accepted seller-liability receipt only settles custody. V22 emits
	// a journal for a refund, so this path must not create one before V24
	// separately charges the carrier fee on its causal payment tick.
	require.Empty(t, receiptResult.Journals)

	settlements, err := cityService.GetCityOpenWorldFreightSettlementState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, settlements.Cases, 1)
	require.Equal(t, "settled", settlements.Cases[0].State)
	require.Empty(t, settlements.Claims)

	// The receipt command ran after this tick's automatic V24 stage, so no
	// quote exists yet. The next automatic tick creates a contract only.
	commerce, err = cityService.GetCityOpenWorldCarrierCommerceState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Empty(t, commerce.Contracts)
	require.Empty(t, commerce.Payments)
	step("v24-contract-boundary")
	commerce, err = cityService.GetCityOpenWorldCarrierCommerceState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, commerce.Contracts, 1)
	require.Empty(t, commerce.Payments)
	contract := commerce.Contracts[0]
	require.Equal(t, actionableCase.Code, contract.CaseCode)
	require.Equal(t, int64(12), contract.CargoUnits)
	require.Equal(t, contract.CargoUnits, contract.QuotedFeeUnits)

	// The V24 payment query requires contract_tick < target_tick. It therefore
	// can only debit seller cash on this later automatic pass.
	step("v24-payment-boundary")
	commerce, err = cityService.GetCityOpenWorldCarrierCommerceState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, commerce.Contracts, 1)
	require.Len(t, commerce.Payments, 1)
	payment := commerce.Payments[0]
	require.Equal(t, contract.Code, payment.ContractCode)
	require.Equal(t, contract.QuotedFeeUnits, payment.AmountUnits)
	require.Greater(t, payment.PaymentTick, contract.ContractTick)
	require.Equal(t, int64(1), commerce.Policy.ContractCount)
	require.Equal(t, int64(1), commerce.Policy.PaymentCount)
	require.Equal(t, payment.AmountUnits, commerce.Policy.PaidAmountUnits)

	var journalLines int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM city_journal_entries entry
JOIN city_journals journal ON journal.id = entry.journal_id
WHERE journal.world_id = $1
  AND journal.journal_type = 'freight_fee'
  AND journal.operation_key = $2`, worldID, "open_world.carrier_fee.payment."+contract.Code).Scan(&journalLines))
	require.Equal(t, 4, journalLines)

	viewerState, err := cityService.GetCityOpenWorldCarrierCommerceState(ctx, viewer.ID, worldID)
	require.NoError(t, err)
	require.Empty(t, viewerState.Contracts)
	require.Empty(t, viewerState.Payments)
	require.Empty(t, viewerState.Policy.CarrierFirmCode)
	require.Zero(t, viewerState.Policy.ContractCount)
	require.Zero(t, viewerState.Policy.PaymentCount)
	require.Zero(t, viewerState.Policy.PaidAmountUnits)

	fromGenesis, targetTick := int64(0), currentTick
	replay, err := cityService.StartReplay(ctx, service.CityReplayInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v24-carrier-commerce-replay",
		FromTick: &fromGenesis, TargetTick: &targetTick,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityReplayStatusVerified, replay.Status, "%+v", replay)
	recovery, err := cityService.StartRecovery(ctx, service.CityRecoveryInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v24-carrier-commerce-restore", ReplayRunID: replay.ID,
	})
	require.NoError(t, err)
	require.Equal(t, service.CityRecoveryStatusApplied, recovery.Status, "%+v", recovery)
	restored, err := cityService.GetCityOpenWorldCarrierCommerceState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, commerce, restored)
}

func TestCityOpenWorldV24UpgradeCreatesFutureOnlyCarrierCommerceBaseline(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("open-world-v24-upgrade-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(24_240_024)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Open World V24 Carrier Commerce Upgrade", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionOpenWorldV23,
		StyleProfileID:    "jp.metropolitan", SpawnPolicy: "city_center",
	})
	require.NoError(t, err)
	worldID := foundation.World.ID

	currentTick := int64(0)
	step, err := cityService.StepWorld(ctx, service.CityStepInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v24-upgrade-baseline-step", ExpectedWorldTick: &currentTick,
	})
	require.NoError(t, err)
	require.NotNil(t, step.Tick)
	currentTick = step.Tick.Tick

	upgrade, err := cityService.StartUpgrade(ctx, service.CityUpgradeInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "v23-v24-carrier-commerce-upgrade",
		TargetVersion: service.CitySimulationVersionOpenWorldV24,
	})
	require.NoError(t, err)
	if upgrade.ErrorDetail != nil {
		t.Logf("V23 to V24 carrier-commerce upgrade detail: %s", *upgrade.ErrorDetail)
	}
	require.Equal(t, service.CityUpgradeStatusApplied, upgrade.Status, "%+v", upgrade)
	engine, err := cityService.GetEngineInfo(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, service.CitySimulationVersionOpenWorldV24, engine.Version)
	require.NotNil(t, engine.VersionVector)
	require.Equal(t, currentTick, engine.VersionVector.BaselineTick)
	contentCatalog := requireCityOpenWorldVersionBinding(t, engine.VersionVector, "content_catalog")
	require.Equal(t, "sub2api-open-world-carrier-commerce-catalog", contentCatalog.BundleID)

	commerce, err := cityService.GetCityOpenWorldCarrierCommerceState(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, currentTick, commerce.Policy.BaselineTick)
	require.Empty(t, commerce.Contracts)
	require.Empty(t, commerce.Payments)
}
