package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func newValidCityOpenWorldFreightSettlementPolicy(t *testing.T) CityOpenWorldFreightSettlementPolicy {
	t.Helper()
	hash, err := cityOpenWorldFreightSettlementPolicyHash()
	require.NoError(t, err)
	metadata, err := json.Marshal(map[string]any{
		"schema_version": cityOpenWorldFreightSettlementSchemaVersion,
		"scope":          "post_baseline_v17_v18_partial_freight_settlement",
	})
	require.NoError(t, err)
	return CityOpenWorldFreightSettlementPolicy{
		ProfileID:              cityOpenWorldFreightSettlementProfileID,
		ProfileVersion:         cityOpenWorldFreightSettlementProfileVersion,
		ContentHash:            hash,
		BaselineTick:           0,
		SourceContract:         cityOpenWorldFreightSettlementSourceContract,
		ReceiptContract:        cityOpenWorldFreightSettlementReceiptContract,
		ResourceContract:       cityOpenWorldFreightSettlementResourceContract,
		FinancialContract:      cityOpenWorldFreightSettlementFinancialContract,
		LiabilityContract:      cityOpenWorldFreightSettlementLiabilityContract,
		MaximumOrders:          cityOpenWorldFreightSettlementMaximumOrders,
		MaximumCasesPerOrder:   cityOpenWorldFreightSettlementMaximumCasesPerOrder,
		MaximumReceiptsPerCase: cityOpenWorldFreightSettlementMaximumReceiptsPerCase,
		MaximumReceiptsPerTick: cityOpenWorldFreightSettlementMaximumReceiptsPerTick,
		Revision:               1,
		Metadata:               metadata,
	}
}

func newValidCityOpenWorldFreightSettlementState(t *testing.T) *CityOpenWorldFreightSettlementState {
	t.Helper()
	const (
		shipmentCode = "enterprise.freight.shipment.settlement.test"
		supplyOrder  = "supply.order.settlement.test"
	)
	metadata := json.RawMessage(`{"schema_version":1}`)
	settlementOrder := cityOpenWorldFreightSettlementOrderCode(cityOpenWorldFreightSettlementSourceShipment, shipmentCode)
	settlementCase := cityOpenWorldFreightSettlementCaseCode(
		settlementOrder, cityOpenWorldFreightSettlementSourceShipment, shipmentCode,
	)
	receiptCode := cityOpenWorldFreightSettlementReceiptCode(1)
	claimCode := cityOpenWorldFreightSettlementClaimCode(receiptCode)
	operation := CityResourceOperationCursor{Tick: 2, Sequence: 1}
	journal := CityJournalCursor{Tick: 2, Sequence: 1}
	policy := newValidCityOpenWorldFreightSettlementPolicy(t)
	policy.OrderCount = 1
	policy.CaseCount = 1
	policy.ReceiptCount = 1
	policy.ClaimCount = 1
	policy.AcceptedUnits = 6
	policy.LostUnits = 2
	policy.RejectedUnits = 2
	policy.RefundedUnits = 20
	return &CityOpenWorldFreightSettlementState{
		Policy: policy,
		Orders: []CityOpenWorldFreightSettlementOrder{{
			Code: settlementOrder, SourceKind: cityOpenWorldFreightSettlementSourceShipment,
			SourceCode: shipmentCode, OrderCode: supplyOrder, SourceTick: 1,
			State: cityOpenWorldFreightSettlementOrderSettled, Version: 2, Metadata: metadata,
		}},
		Cases: []CityOpenWorldFreightSettlementCase{{
			Code: settlementCase, SettlementOrderCode: settlementOrder,
			SourceKind: cityOpenWorldFreightSettlementSourceShipment, SourceCode: shipmentCode,
			TransportState: cityOpenWorldEnterpriseFreightReceiptStateAwaitingReceipt,
			State:          cityOpenWorldFreightSettlementCaseSettled, SourceTick: 1, Version: 2, Metadata: metadata,
		}},
		Lines: []CityOpenWorldFreightSettlementCaseLine{{
			CaseCode: settlementCase, SourceLineNo: 1, ResourceCode: "basic_material",
			SourceFirmCode: "firm.seller", SourceDistrictCode: "district.seller",
			DestinationFirmCode: "firm.buyer", DestinationDistrictCode: "district.buyer",
			QuantityUnits: 10, UnitPriceUnits: 5, TotalPriceUnits: 50, Metadata: metadata,
		}},
		Receipts: []CityOpenWorldFreightSettlementReceipt{{
			Code: receiptCode, CaseCode: settlementCase, ReceiptTick: 2, SourceCommandSequence: 1,
			LiabilityParty: cityOpenWorldFreightSettlementLiabilityCarrier, RefundedUnits: 20,
			ResourceOperation: &operation, Journal: &journal, Metadata: metadata,
		}},
		ReceiptLines: []CityOpenWorldFreightSettlementReceiptLine{{
			ReceiptCode: receiptCode, CaseCode: settlementCase, SourceLineNo: 1,
			AcceptedUnits: 6, LostUnits: 2, RejectedUnits: 2,
		}},
		Claims: []CityOpenWorldFreightSettlementClaim{{
			Code: claimCode, ReceiptCode: receiptCode, CaseCode: settlementCase,
			LiabilityParty: cityOpenWorldFreightSettlementLiabilityCarrier,
			ClaimAmount:    20, State: cityOpenWorldFreightSettlementClaimOpen,
			CreatedTick: 2, Metadata: metadata,
		}},
	}
}

func TestCityOpenWorldFreightSettlementStateEnforcesOutcomeEvidence(t *testing.T) {
	state := newValidCityOpenWorldFreightSettlementState(t)
	require.NoError(t, validateCityOpenWorldFreightSettlementState(state))

	overResolved := newValidCityOpenWorldFreightSettlementState(t)
	overResolved.ReceiptLines[0].AcceptedUnits = 11
	require.Error(t, validateCityOpenWorldFreightSettlementState(overResolved))

	missingResourceEvidence := newValidCityOpenWorldFreightSettlementState(t)
	missingResourceEvidence.Receipts[0].ResourceOperation = nil
	require.Error(t, validateCityOpenWorldFreightSettlementState(missingResourceEvidence))

	missingCarrierClaim := newValidCityOpenWorldFreightSettlementState(t)
	missingCarrierClaim.Claims = nil
	missingCarrierClaim.Policy.ClaimCount = 0
	require.Error(t, validateCityOpenWorldFreightSettlementState(missingCarrierClaim))

	invalidOrderState := newValidCityOpenWorldFreightSettlementState(t)
	invalidOrderState.Orders[0].State = cityOpenWorldFreightSettlementOrderReceiving
	require.Error(t, validateCityOpenWorldFreightSettlementState(invalidOrderState))
}

func TestCityOpenWorldFreightSettlementStateRejectsForgedReceiptIdentity(t *testing.T) {
	duplicateLine := newValidCityOpenWorldFreightSettlementState(t)
	duplicateLine.ReceiptLines = append(duplicateLine.ReceiptLines, duplicateLine.ReceiptLines[0])
	require.Error(t, validateCityOpenWorldFreightSettlementState(duplicateLine))

	forgedReceipt := newValidCityOpenWorldFreightSettlementState(t)
	forgedReceipt.Receipts[0].Code = "freight.settlement.receipt.forged"
	require.Error(t, validateCityOpenWorldFreightSettlementState(forgedReceipt))

	forgedClaim := newValidCityOpenWorldFreightSettlementState(t)
	forgedClaim.Claims[0].Code = "freight.settlement.claim.forged"
	require.Error(t, validateCityOpenWorldFreightSettlementState(forgedClaim))
}

func TestCityOpenWorldFreightSettlementStateAcceptsOnlyZeroReceiptVoidedClosure(t *testing.T) {
	voided := newValidCityOpenWorldFreightSettlementState(t)
	voided.Orders[0].State = cityOpenWorldFreightSettlementOrderVoided
	voided.Cases[0].State = cityOpenWorldFreightSettlementCaseVoided
	voided.Receipts = nil
	voided.ReceiptLines = nil
	voided.Claims = nil
	voided.Policy.ReceiptCount = 0
	voided.Policy.ClaimCount = 0
	voided.Policy.AcceptedUnits = 0
	voided.Policy.LostUnits = 0
	voided.Policy.RejectedUnits = 0
	voided.Policy.RefundedUnits = 0
	require.NoError(t, validateCityOpenWorldFreightSettlementState(voided))

	voidedCaseMissing := newValidCityOpenWorldFreightSettlementState(t)
	voidedCaseMissing.Orders[0].State = cityOpenWorldFreightSettlementOrderVoided
	voidedCaseMissing.Receipts = nil
	voidedCaseMissing.ReceiptLines = nil
	voidedCaseMissing.Claims = nil
	voidedCaseMissing.Policy.ReceiptCount = 0
	voidedCaseMissing.Policy.ClaimCount = 0
	voidedCaseMissing.Policy.AcceptedUnits = 0
	voidedCaseMissing.Policy.LostUnits = 0
	voidedCaseMissing.Policy.RejectedUnits = 0
	voidedCaseMissing.Policy.RefundedUnits = 0
	require.Error(t, validateCityOpenWorldFreightSettlementState(voidedCaseMissing))

	resolvedVoid := newValidCityOpenWorldFreightSettlementState(t)
	resolvedVoid.Orders[0].State = cityOpenWorldFreightSettlementOrderVoided
	resolvedVoid.Cases[0].State = cityOpenWorldFreightSettlementCaseVoided
	require.Error(t, validateCityOpenWorldFreightSettlementState(resolvedVoid))
}

func TestCityOpenWorldFreightSettlementProjectionScopesPolicyAndEvidence(t *testing.T) {
	state := newValidCityOpenWorldFreightSettlementState(t)
	view := projectCityOpenWorldFreightSettlementStateForOwnedFirms(state, map[string]struct{}{
		"firm.buyer": {},
	})
	require.Len(t, view.Orders, 1)
	require.Len(t, view.Cases, 1)
	require.Len(t, view.Lines, 1)
	require.Len(t, view.Receipts, 1)
	require.Len(t, view.ReceiptLines, 1)
	require.Len(t, view.Claims, 1)
	require.Equal(t, int64(6), view.Policy.AcceptedUnits)
	require.Equal(t, int64(2), view.Policy.LostUnits)
	require.Equal(t, int64(2), view.Policy.RejectedUnits)
	require.Equal(t, int64(20), view.Policy.RefundedUnits)
	require.Zero(t, view.Policy.Revision)
	require.Equal(t, int64(1), state.Policy.Revision, "scoped reads must not mutate persisted state")

	hidden := projectCityOpenWorldFreightSettlementStateForOwnedFirms(state, map[string]struct{}{
		"firm.unrelated": {},
	})
	require.Empty(t, hidden.Orders)
	require.Empty(t, hidden.Cases)
	require.Empty(t, hidden.Lines)
	require.Empty(t, hidden.Receipts)
	require.Empty(t, hidden.ReceiptLines)
	require.Empty(t, hidden.Claims)
	require.Zero(t, hidden.Policy.OrderCount)
	require.Zero(t, hidden.Policy.CaseCount)
	require.Zero(t, hidden.Policy.ReceiptCount)
	require.Zero(t, hidden.Policy.ClaimCount)
	require.Zero(t, hidden.Policy.Revision)
}

func TestNormalizeCityOpenWorldFreightSettlementCommandSortsAndRejectsDuplicates(t *testing.T) {
	value, handled, err := normalizeCityOpenWorldFreightSettlementCommand(
		CityCommandTypeOpenWorldFreightSettlementReceipt,
		json.RawMessage(`{"case_code":"freight.settlement.case.test","liability_party":"carrier","lines":[{"source_line_no":2,"accepted_units":0,"lost_units":1,"rejected_units":0},{"source_line_no":1,"accepted_units":1,"lost_units":0,"rejected_units":0}]}`),
	)
	require.NoError(t, err)
	require.True(t, handled)
	payload, ok := value.(cityOpenWorldFreightSettlementReceiptPayload)
	require.True(t, ok)
	require.Equal(t, 1, payload.Lines[0].SourceLineNo)
	require.Equal(t, 2, payload.Lines[1].SourceLineNo)

	_, handled, err = normalizeCityOpenWorldFreightSettlementCommand(
		CityCommandTypeOpenWorldFreightSettlementReceipt,
		json.RawMessage(`{"case_code":"freight.settlement.case.test","liability_party":"seller","lines":[{"source_line_no":1,"accepted_units":1,"lost_units":0,"rejected_units":0},{"source_line_no":1,"accepted_units":0,"lost_units":1,"rejected_units":0}]}`),
	)
	require.True(t, handled)
	require.Error(t, err)
}
