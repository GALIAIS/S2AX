package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func newValidCityOpenWorldCarrierRecoveryState(t *testing.T) *CityOpenWorldCarrierRecoveryState {
	t.Helper()
	hash, err := cityOpenWorldCarrierRecoveryPolicyHash()
	require.NoError(t, err)
	profileMetadata, err := json.Marshal(map[string]any{
		"schema_version":   cityOpenWorldCarrierRecoverySchemaVersion,
		"scope":            "manual_carrier_reserve_and_claim_recovery",
		"reserve_policy":   cityOpenWorldCarrierRecoveryReservePolicy,
		"claim_visibility": "seller_scoped_recovery_evidence",
	})
	require.NoError(t, err)
	funding := CityOpenWorldCarrierReserveFunding{
		Code:                  cityOpenWorldCarrierReserveFundingCode(1),
		FundingTick:           2,
		SourceCommandSequence: 1,
		AmountUnits:           50,
		Journal:               CityJournalCursor{Tick: 2, Sequence: 1},
	}
	funding.Metadata, err = json.Marshal(map[string]any{
		"schema_version": cityOpenWorldCarrierRecoverySchemaVersion,
		"contract":       cityOpenWorldCarrierRecoveryFundingContract,
		"carrier_firm":   cityOpenWorldCarrierRecoveryFirmCode,
		"amount_units":   funding.AmountUnits,
	})
	require.NoError(t, err)
	recovery := CityOpenWorldFreightClaimRecovery{
		Code:                  cityOpenWorldFreightClaimRecoveryCode(2),
		ClaimCode:             "freight.settlement.claim.v23.test",
		CaseCode:              "freight.settlement.case.v23.test",
		SellerFirmCode:        "firm.seller",
		RecoveryTick:          3,
		SourceCommandSequence: 2,
		AmountUnits:           20,
		Journal:               CityJournalCursor{Tick: 3, Sequence: 1},
	}
	recovery.Metadata, err = json.Marshal(map[string]any{
		"schema_version": cityOpenWorldCarrierRecoverySchemaVersion,
		"contract":       cityOpenWorldCarrierRecoveryRecoveryContract,
		"claim_code":     recovery.ClaimCode,
		"case_code":      recovery.CaseCode,
		"seller_firm":    recovery.SellerFirmCode,
		"carrier_firm":   cityOpenWorldCarrierRecoveryFirmCode,
		"amount_units":   recovery.AmountUnits,
	})
	require.NoError(t, err)
	return &CityOpenWorldCarrierRecoveryState{
		Policy: CityOpenWorldCarrierRecoveryPolicy{
			ProfileID:              cityOpenWorldCarrierRecoveryProfileID,
			ProfileVersion:         cityOpenWorldCarrierRecoveryProfileVersion,
			ContentHash:            hash,
			BaselineTick:           0,
			CarrierActorCode:       cityOpenWorldEnterpriseFreightCarrierActorCode,
			CarrierFirmCode:        cityOpenWorldCarrierRecoveryFirmCode,
			FundingContract:        cityOpenWorldCarrierRecoveryFundingContract,
			RecoveryContract:       cityOpenWorldCarrierRecoveryRecoveryContract,
			ReservePolicy:          cityOpenWorldCarrierRecoveryReservePolicy,
			MaximumFundingsPerTick: cityOpenWorldCarrierRecoveryMaximumFundingsPerTick,
			MaximumRecoveriesTick:  cityOpenWorldCarrierRecoveryMaximumRecoveriesTick,
			MaximumAmountUnits:     cityOpenWorldCarrierRecoveryMaximumAmountUnits,
			FundingCount:           1,
			RecoveryCount:          1,
			FundedUnits:            funding.AmountUnits,
			RecoveredUnits:         recovery.AmountUnits,
			Revision:               3,
			Metadata:               profileMetadata,
		},
		Fundings:   []CityOpenWorldCarrierReserveFunding{funding},
		Recoveries: []CityOpenWorldFreightClaimRecovery{recovery},
	}
}

func TestCityOpenWorldCarrierRecoveryStatePinsManualReserveEvidence(t *testing.T) {
	state := newValidCityOpenWorldCarrierRecoveryState(t)
	require.NoError(t, validateCityOpenWorldCarrierRecoveryState(state))

	forgedFunding := newValidCityOpenWorldCarrierRecoveryState(t)
	forgedFunding.Fundings[0].Metadata = json.RawMessage(`{"schema_version":1,"contract":"forged","carrier_firm":"system_freight_reserve","amount_units":50}`)
	require.Error(t, validateCityOpenWorldCarrierRecoveryState(forgedFunding))

	insolvent := newValidCityOpenWorldCarrierRecoveryState(t)
	insolvent.Policy.FundedUnits = 10
	require.Error(t, validateCityOpenWorldCarrierRecoveryState(insolvent))

	forgedClaimCode := newValidCityOpenWorldCarrierRecoveryState(t)
	forgedClaimCode.Recoveries[0].Code = "carrier.claim.recovery.forged"
	require.Error(t, validateCityOpenWorldCarrierRecoveryState(forgedClaimCode))
}

func TestCityOpenWorldCarrierRecoveryProjectionHidesReserveFromMembers(t *testing.T) {
	state := newValidCityOpenWorldCarrierRecoveryState(t)
	view := projectCityOpenWorldCarrierRecoveryStateForOwnedFirms(state, map[string]struct{}{
		"firm.seller": {},
	})
	require.Empty(t, view.Fundings)
	require.Len(t, view.Recoveries, 1)
	require.Empty(t, view.Policy.CarrierFirmCode)
	require.Zero(t, view.Policy.FundingCount)
	require.Zero(t, view.Policy.RecoveryCount)
	require.Equal(t, int64(1), state.Policy.FundingCount, "read projection must not mutate canonical state")

	hidden := projectCityOpenWorldCarrierRecoveryStateForOwnedFirms(state, map[string]struct{}{
		"firm.other": {},
	})
	require.Empty(t, hidden.Recoveries)
}

func TestNormalizeCityOpenWorldCarrierRecoveryCommandRejectsUnsafePayloads(t *testing.T) {
	value, handled, err := normalizeCityOpenWorldCarrierRecoveryCommand(
		CityCommandTypeOpenWorldCarrierReserveFund,
		json.RawMessage(`{"amount_units":25,"memo":" reserve "}`),
	)
	require.NoError(t, err)
	require.True(t, handled)
	funding, ok := value.(cityOpenWorldCarrierReserveFundPayload)
	require.True(t, ok)
	require.Equal(t, "reserve", funding.Memo)

	_, handled, err = normalizeCityOpenWorldCarrierRecoveryCommand(
		CityCommandTypeOpenWorldFreightClaimResolve,
		json.RawMessage(`{"claim_code":"not valid","extra":true}`),
	)
	require.True(t, handled)
	require.Error(t, err)
}

func TestReplayCityOpenWorldCarrierRecoveryCheckpointRequiresResolvedClaim(t *testing.T) {
	settlements := newValidCityOpenWorldFreightSettlementState(t)
	settlements.Claims[0].State = cityOpenWorldFreightSettlementClaimResolved
	carrierRecovery := newValidCityOpenWorldCarrierRecoveryState(t)
	carrierRecovery.Recoveries[0].ClaimCode = settlements.Claims[0].Code
	carrierRecovery.Recoveries[0].CaseCode = settlements.Claims[0].CaseCode
	carrierRecovery.Recoveries[0].AmountUnits = settlements.Claims[0].ClaimAmount
	metadata, err := json.Marshal(map[string]any{
		"schema_version": cityOpenWorldCarrierRecoverySchemaVersion,
		"contract":       cityOpenWorldCarrierRecoveryRecoveryContract,
		"claim_code":     carrierRecovery.Recoveries[0].ClaimCode,
		"case_code":      carrierRecovery.Recoveries[0].CaseCode,
		"seller_firm":    carrierRecovery.Recoveries[0].SellerFirmCode,
		"carrier_firm":   cityOpenWorldCarrierRecoveryFirmCode,
		"amount_units":   carrierRecovery.Recoveries[0].AmountUnits,
	})
	require.NoError(t, err)
	carrierRecovery.Recoveries[0].Metadata = metadata
	carrierRecovery.Policy.RecoveredUnits = carrierRecovery.Recoveries[0].AmountUnits

	state := &cityHashState{
		SimulationVersion: CitySimulationVersionOpenWorldV23,
		OpenWorldRuntime: &cityOpenWorldRuntimeHashState{
			FreightSettlements: settlements,
			CarrierRecovery:    carrierRecovery,
		},
	}
	require.NoError(t, replayCityOpenWorldCarrierRecoveryCheckpoint(3, state))

	settlements.Claims[0].State = cityOpenWorldFreightSettlementClaimOpen
	require.Error(t, replayCityOpenWorldCarrierRecoveryCheckpoint(3, state))
}
