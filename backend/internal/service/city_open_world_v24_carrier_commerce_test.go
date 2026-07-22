package service

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func newValidCityOpenWorldCarrierCommerceState(t *testing.T) *CityOpenWorldCarrierCommerceState {
	t.Helper()
	hash, err := cityOpenWorldCarrierCommercePolicyHash()
	require.NoError(t, err)
	profileMetadata, err := json.Marshal(cityOpenWorldCarrierCommerceProfileMetadata{
		SchemaVersion: cityOpenWorldCarrierCommerceSchemaVersion,
		Scope:         "post_baseline_v22_case_carrier_service_fee",
		Pricing:       "fixed_per_cargo_unit_v1",
		Settlement:    "cash_only_retry_without_credit_v1",
	})
	require.NoError(t, err)
	contract := CityOpenWorldCarrierServiceContract{
		CaseCode:         "freight.settlement.case.v24.test",
		SourceKind:       cityOpenWorldFreightSettlementSourceShipment,
		SourceCode:       "enterprise.freight.shipment.v24.test",
		SellerFirmCode:   "firm.seller",
		CarrierFirmCode:  cityOpenWorldCarrierRecoveryFirmCode,
		CarrierActorCode: cityOpenWorldEnterpriseFreightCarrierActorCode,
		SourceTick:       2,
		ContractTick:     3,
		CargoUnits:       10,
		FeePerCargoUnit:  cityOpenWorldCarrierCommerceFeePerCargoUnit,
		QuotedFeeUnits:   10,
	}
	contract.Code = cityOpenWorldCarrierServiceContractCode(contract.CaseCode)
	contract.Metadata = mustMarshalCityOpenWorldCarrierCommerceContractMetadata(t, contract)
	payment := CityOpenWorldCarrierFeePayment{
		ContractCode:    contract.Code,
		CaseCode:        contract.CaseCode,
		SellerFirmCode:  contract.SellerFirmCode,
		CarrierFirmCode: contract.CarrierFirmCode,
		PaymentTick:     4,
		CargoUnits:      contract.CargoUnits,
		AmountUnits:     contract.QuotedFeeUnits,
		Journal:         CityJournalCursor{Tick: 4, Sequence: 1},
	}
	payment.Code = cityOpenWorldCarrierFeePaymentCode(payment.ContractCode)
	payment.Metadata = mustMarshalCityOpenWorldCarrierCommercePaymentMetadata(t, payment)
	return &CityOpenWorldCarrierCommerceState{
		Policy: CityOpenWorldCarrierCommercePolicy{
			ProfileID:               cityOpenWorldCarrierCommerceProfileID,
			ProfileVersion:          cityOpenWorldCarrierCommerceProfileVersion,
			ContentHash:             hash,
			BaselineTick:            1,
			CarrierActorCode:        cityOpenWorldEnterpriseFreightCarrierActorCode,
			CarrierFirmCode:         cityOpenWorldCarrierRecoveryFirmCode,
			ServiceContract:         cityOpenWorldCarrierCommerceContractContract,
			PaymentContract:         cityOpenWorldCarrierCommercePaymentContract,
			FeePerCargoUnit:         cityOpenWorldCarrierCommerceFeePerCargoUnit,
			MaximumContractsPerTick: cityOpenWorldCarrierCommerceMaximumContractsPerTick,
			MaximumPaymentsPerTick:  cityOpenWorldCarrierCommerceMaximumPaymentsPerTick,
			ContractCount:           1,
			PaymentCount:            1,
			QuotedCargoUnits:        contract.CargoUnits,
			PaidCargoUnits:          payment.CargoUnits,
			PaidAmountUnits:         payment.AmountUnits,
			Revision:                3,
			Metadata:                profileMetadata,
		},
		Contracts: []CityOpenWorldCarrierServiceContract{contract},
		Payments:  []CityOpenWorldCarrierFeePayment{payment},
	}
}

func mustMarshalCityOpenWorldCarrierCommerceContractMetadata(
	t *testing.T,
	contract CityOpenWorldCarrierServiceContract,
) json.RawMessage {
	t.Helper()
	metadata, err := json.Marshal(cityOpenWorldCarrierCommerceContractMetadata{
		SchemaVersion: cityOpenWorldCarrierCommerceSchemaVersion,
		Contract:      cityOpenWorldCarrierCommerceContractContract,
		CaseCode:      contract.CaseCode,
		SourceKind:    contract.SourceKind,
		SourceCode:    contract.SourceCode,
		SellerFirm:    contract.SellerFirmCode,
		CarrierFirm:   contract.CarrierFirmCode,
		CarrierActor:  contract.CarrierActorCode,
		CargoUnits:    contract.CargoUnits,
		FeePerUnit:    contract.FeePerCargoUnit,
		QuotedFee:     contract.QuotedFeeUnits,
	})
	require.NoError(t, err)
	return metadata
}

func mustMarshalCityOpenWorldCarrierCommercePaymentMetadata(
	t *testing.T,
	payment CityOpenWorldCarrierFeePayment,
) json.RawMessage {
	t.Helper()
	metadata, err := json.Marshal(cityOpenWorldCarrierCommercePaymentMetadata{
		SchemaVersion: cityOpenWorldCarrierCommerceSchemaVersion,
		Contract:      cityOpenWorldCarrierCommercePaymentContract,
		ContractCode:  payment.ContractCode,
		CaseCode:      payment.CaseCode,
		SellerFirm:    payment.SellerFirmCode,
		CarrierFirm:   payment.CarrierFirmCode,
		CargoUnits:    payment.CargoUnits,
		AmountUnits:   payment.AmountUnits,
	})
	require.NoError(t, err)
	return metadata
}

func TestCityOpenWorldCarrierCommerceStatePinsCausalCashEvidence(t *testing.T) {
	state := newValidCityOpenWorldCarrierCommerceState(t)
	require.NoError(t, validateCityOpenWorldCarrierCommerceState(state))

	empty := newValidCityOpenWorldCarrierCommerceState(t)
	empty.Contracts = nil
	empty.Payments = nil
	empty.Policy.ContractCount = 0
	empty.Policy.PaymentCount = 0
	empty.Policy.QuotedCargoUnits = 0
	empty.Policy.PaidCargoUnits = 0
	empty.Policy.PaidAmountUnits = 0
	empty.Policy.Revision = 1
	require.NoError(t, validateCityOpenWorldCarrierCommerceState(empty))

	sameTickPayment := newValidCityOpenWorldCarrierCommerceState(t)
	sameTickPayment.Payments[0].PaymentTick = sameTickPayment.Contracts[0].ContractTick
	sameTickPayment.Payments[0].Journal.Tick = sameTickPayment.Payments[0].PaymentTick
	sameTickPayment.Payments[0].Metadata = mustMarshalCityOpenWorldCarrierCommercePaymentMetadata(t, sameTickPayment.Payments[0])
	require.Error(t, validateCityOpenWorldCarrierCommerceState(sameTickPayment))

	preSourceContract := newValidCityOpenWorldCarrierCommerceState(t)
	preSourceContract.Contracts[0].ContractTick = preSourceContract.Contracts[0].SourceTick - 1
	preSourceContract.Contracts[0].Metadata = mustMarshalCityOpenWorldCarrierCommerceContractMetadata(t, preSourceContract.Contracts[0])
	require.Error(t, validateCityOpenWorldCarrierCommerceState(preSourceContract))

	forgedQuote := newValidCityOpenWorldCarrierCommerceState(t)
	forgedQuote.Contracts[0].QuotedFeeUnits++
	forgedQuote.Contracts[0].Metadata = mustMarshalCityOpenWorldCarrierCommerceContractMetadata(t, forgedQuote.Contracts[0])
	require.Error(t, validateCityOpenWorldCarrierCommerceState(forgedQuote))

	_, err := cityOpenWorldCarrierCommerceQuotedFee(math.MaxInt64, 2)
	require.Error(t, err)
}

func TestCityOpenWorldCarrierCommerceProjectionScopesCarrierAndGlobalCounters(t *testing.T) {
	state := newValidCityOpenWorldCarrierCommerceState(t)
	view := projectCityOpenWorldCarrierCommerceStateForOwnedFirms(state, map[string]struct{}{
		"firm.seller": {},
	})
	require.Len(t, view.Contracts, 1)
	require.Len(t, view.Payments, 1)
	require.Empty(t, view.Policy.CarrierActorCode)
	require.Empty(t, view.Policy.CarrierFirmCode)
	require.Zero(t, view.Policy.ContractCount)
	require.Zero(t, view.Policy.PaymentCount)
	require.Zero(t, view.Policy.QuotedCargoUnits)
	require.Zero(t, view.Policy.PaidCargoUnits)
	require.Zero(t, view.Policy.PaidAmountUnits)
	require.Zero(t, view.Policy.Revision)
	require.Empty(t, view.Contracts[0].CarrierActorCode)
	require.Empty(t, view.Contracts[0].CarrierFirmCode)
	require.Empty(t, view.Payments[0].CarrierFirmCode)
	require.Equal(t, cityOpenWorldCarrierRecoveryFirmCode, state.Policy.CarrierFirmCode, "scoped reads must not mutate canonical state")
	require.Equal(t, int64(1), state.Policy.ContractCount, "scoped reads must not mutate counters")

	hidden := projectCityOpenWorldCarrierCommerceStateForOwnedFirms(state, map[string]struct{}{
		"firm.other": {},
	})
	require.Empty(t, hidden.Contracts)
	require.Empty(t, hidden.Payments)
}

func TestReplayCityOpenWorldCarrierCommerceCheckpointRequiresLaterSettledCase(t *testing.T) {
	settlements := newValidCityOpenWorldFreightSettlementState(t)
	settlements.Cases[0].SourceTick = 2
	commerce := newValidCityOpenWorldCarrierCommerceState(t)
	contract := &commerce.Contracts[0]
	contract.CaseCode = settlements.Cases[0].Code
	contract.SourceKind = settlements.Cases[0].SourceKind
	contract.SourceCode = settlements.Cases[0].SourceCode
	contract.SourceTick = settlements.Cases[0].SourceTick
	contract.Code = cityOpenWorldCarrierServiceContractCode(contract.CaseCode)
	contract.Metadata = mustMarshalCityOpenWorldCarrierCommerceContractMetadata(t, *contract)
	payment := &commerce.Payments[0]
	payment.ContractCode = contract.Code
	payment.CaseCode = contract.CaseCode
	payment.Code = cityOpenWorldCarrierFeePaymentCode(payment.ContractCode)
	payment.Metadata = mustMarshalCityOpenWorldCarrierCommercePaymentMetadata(t, *payment)
	commerce.Policy.BaselineTick = 1
	commerce.Policy.QuotedCargoUnits = contract.CargoUnits
	commerce.Policy.PaidCargoUnits = payment.CargoUnits
	commerce.Policy.PaidAmountUnits = payment.AmountUnits

	state := &cityHashState{
		SimulationVersion: CitySimulationVersionOpenWorldV24,
		OpenWorldRuntime: &cityOpenWorldRuntimeHashState{
			FreightSettlements: settlements,
			CarrierRecovery:    newValidCityOpenWorldCarrierRecoveryState(t),
			CarrierCommerce:    commerce,
		},
	}
	require.NoError(t, replayCityOpenWorldCarrierCommerceCheckpoint(4, state))

	settlements.Cases[0].State = cityOpenWorldFreightSettlementCaseReceiving
	require.Error(t, replayCityOpenWorldCarrierCommerceCheckpoint(4, state))
}
