package service

import (
	"context"
	"fmt"
)

// GetCityOpenWorldFreightSettlementState exposes V22's receipt and liability
// evidence to world members. The projection never contains credentials,
// upstream account data, raw resource-operation payloads, journal lines, or
// mutable administration controls.
func (s *CityEconomyService) GetCityOpenWorldFreightSettlementState(
	ctx context.Context,
	userID, worldID int64,
) (*CityOpenWorldFreightSettlementState, error) {
	if userID <= 0 || worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, userID, worldID); err != nil {
		return nil, err
	}
	var version string
	if err := s.db.QueryRowContext(ctx, `SELECT simulation_version FROM city_worlds WHERE id = $1`, worldID).Scan(&version); err != nil {
		return nil, fmt.Errorf("load V22 freight-settlement world version: %w", err)
	}
	if !cityEngineSupportsOpenWorldFreightSettlements(version) {
		return nil, ErrCitySimulationVersion.WithMetadata(map[string]string{"version": version})
	}
	state, err := loadCityOpenWorldFreightSettlementState(ctx, s.db, worldID)
	if err != nil {
		return nil, err
	}
	fullRead, ownedFirmCodes, err := s.cityOpenWorldSupplyChainReadScope(ctx, userID, worldID)
	if err != nil {
		return nil, err
	}
	if fullRead {
		return state, nil
	}
	return projectCityOpenWorldFreightSettlementStateForOwnedFirms(state, ownedFirmCodes), nil
}

// projectCityOpenWorldFreightSettlementStateForOwnedFirms keeps V22 aligned
// with the bilateral visibility contract used by V15--V18. A regular member
// may inspect receipt outcomes only for a supply-chain line whose seller or
// buyer firm they own. Global counters and revision are rebuilt from this
// filtered view, preventing unrelated freight volume or refund activity from
// becoming an observation side channel.
func projectCityOpenWorldFreightSettlementStateForOwnedFirms(
	state *CityOpenWorldFreightSettlementState,
	ownedFirmCodes map[string]struct{},
) *CityOpenWorldFreightSettlementState {
	if state == nil {
		return nil
	}
	view := &CityOpenWorldFreightSettlementState{
		Policy:       state.Policy,
		Orders:       make([]CityOpenWorldFreightSettlementOrder, 0),
		Cases:        make([]CityOpenWorldFreightSettlementCase, 0),
		Lines:        make([]CityOpenWorldFreightSettlementCaseLine, 0),
		Receipts:     make([]CityOpenWorldFreightSettlementReceipt, 0),
		ReceiptLines: make([]CityOpenWorldFreightSettlementReceiptLine, 0),
		Claims:       make([]CityOpenWorldFreightSettlementClaim, 0),
	}

	visibleCases := make(map[string]struct{})
	for _, line := range state.Lines {
		_, sourceOwned := ownedFirmCodes[line.SourceFirmCode]
		_, destinationOwned := ownedFirmCodes[line.DestinationFirmCode]
		if sourceOwned || destinationOwned {
			visibleCases[line.CaseCode] = struct{}{}
		}
	}
	visibleOrders := make(map[string]struct{})
	for _, settlementCase := range state.Cases {
		if _, visible := visibleCases[settlementCase.Code]; visible {
			visibleOrders[settlementCase.SettlementOrderCode] = struct{}{}
			view.Cases = append(view.Cases, settlementCase)
		}
	}
	for _, order := range state.Orders {
		if _, visible := visibleOrders[order.Code]; visible {
			view.Orders = append(view.Orders, order)
		}
	}
	for _, line := range state.Lines {
		if _, visible := visibleCases[line.CaseCode]; visible {
			view.Lines = append(view.Lines, line)
		}
	}

	visibleReceipts := make(map[string]struct{})
	for _, receipt := range state.Receipts {
		if _, visible := visibleCases[receipt.CaseCode]; visible {
			visibleReceipts[receipt.Code] = struct{}{}
			view.Receipts = append(view.Receipts, receipt)
		}
	}
	for _, line := range state.ReceiptLines {
		if _, visible := visibleReceipts[line.ReceiptCode]; visible {
			view.ReceiptLines = append(view.ReceiptLines, line)
		}
	}
	for _, claim := range state.Claims {
		if _, visible := visibleReceipts[claim.ReceiptCode]; visible {
			view.Claims = append(view.Claims, claim)
		}
	}

	view.Policy.OrderCount = int64(len(view.Orders))
	view.Policy.CaseCount = int64(len(view.Cases))
	view.Policy.ReceiptCount = int64(len(view.Receipts))
	view.Policy.ClaimCount = int64(len(view.Claims))
	view.Policy.AcceptedUnits = 0
	view.Policy.LostUnits = 0
	view.Policy.RejectedUnits = 0
	view.Policy.RefundedUnits = 0
	for _, line := range view.ReceiptLines {
		view.Policy.AcceptedUnits += line.AcceptedUnits
		view.Policy.LostUnits += line.LostUnits
		view.Policy.RejectedUnits += line.RejectedUnits
	}
	for _, receipt := range view.Receipts {
		view.Policy.RefundedUnits += receipt.RefundedUnits
	}
	// This is a scoped API view instead of the persisted global projection.
	view.Policy.Revision = 0
	sortCityOpenWorldFreightSettlementState(view)
	return view
}
