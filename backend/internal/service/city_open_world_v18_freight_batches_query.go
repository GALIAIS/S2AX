package service

import (
	"context"
	"fmt"
)

// GetCityOpenWorldFreightBatchState exposes V18's overflow-consignment
// projection. It is intentionally evidence-only: callers never receive raw
// inventory balances, resource-operation payloads, or upstream account data.
func (s *CityEconomyService) GetCityOpenWorldFreightBatchState(
	ctx context.Context,
	userID, worldID int64,
) (*CityOpenWorldFreightBatchState, error) {
	if userID <= 0 || worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, userID, worldID); err != nil {
		return nil, err
	}
	var version string
	if err := s.db.QueryRowContext(ctx, `SELECT simulation_version FROM city_worlds WHERE id = $1`, worldID).Scan(&version); err != nil {
		return nil, fmt.Errorf("load V18 freight-batch world version: %w", err)
	}
	if !cityEngineSupportsOpenWorldFreightBatches(version) {
		return nil, ErrCitySimulationVersion.WithMetadata(map[string]string{"version": version})
	}
	state, err := loadCityOpenWorldFreightBatchState(ctx, s.db, worldID)
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
	return s.projectCityOpenWorldFreightBatchStateForOwnedFirms(ctx, worldID, state, ownedFirmCodes)
}

// projectCityOpenWorldFreightBatchStateForOwnedFirms preserves the bilateral
// V15 visibility contract. Dynamic counters are recomputed from the filtered
// projection so a participant cannot infer unrelated transport volume.
func (s *CityEconomyService) projectCityOpenWorldFreightBatchStateForOwnedFirms(
	ctx context.Context,
	worldID int64,
	state *CityOpenWorldFreightBatchState,
	ownedFirmCodes map[string]struct{},
) (*CityOpenWorldFreightBatchState, error) {
	if state == nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT node.code, firm.code
FROM city_open_world_supply_chain_nodes node
JOIN city_economic_entities firm
  ON firm.id = node.firm_entity_id AND firm.world_id = node.world_id
WHERE node.world_id = $1`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V18 freight-batch plan ownership: %w", err)
	}
	nodeFirms := make(map[string]string)
	for rows.Next() {
		var nodeCode, firmCode string
		if err = rows.Scan(&nodeCode, &firmCode); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan V18 freight-batch plan ownership: %w", err)
		}
		nodeFirms[nodeCode] = firmCode
	}
	if err = closeCityRows(rows, "iterate V18 freight-batch plan ownership"); err != nil {
		return nil, err
	}

	view := &CityOpenWorldFreightBatchState{
		Policy:       state.Policy,
		Plans:        make([]CityOpenWorldFreightBatchPlan, 0),
		Consignments: make([]CityOpenWorldFreightBatchConsignment, 0),
		Lines:        make([]CityOpenWorldFreightBatchLine, 0),
		Facts:        make([]CityOpenWorldFreightBatchFact, 0),
		Transitions:  make([]CityOpenWorldFreightBatchTransition, 0),
		Receipts:     make([]CityOpenWorldFreightBatchReceipt, 0),
	}
	visiblePlans := make(map[string]struct{})
	for _, plan := range state.Plans {
		_, sellerOwned := ownedFirmCodes[nodeFirms[plan.SellerNodeCode]]
		_, buyerOwned := ownedFirmCodes[nodeFirms[plan.BuyerNodeCode]]
		if sellerOwned || buyerOwned {
			visiblePlans[plan.Code] = struct{}{}
			view.Plans = append(view.Plans, plan)
		}
	}
	visibleConsignments := make(map[string]struct{})
	for _, consignment := range state.Consignments {
		if _, visible := visiblePlans[consignment.PlanCode]; visible {
			visibleConsignments[consignment.Code] = struct{}{}
			view.Consignments = append(view.Consignments, consignment)
		}
	}
	for _, line := range state.Lines {
		if _, visible := visibleConsignments[line.ConsignmentCode]; visible {
			view.Lines = append(view.Lines, line)
		}
	}
	for _, fact := range state.Facts {
		if _, visible := visibleConsignments[fact.ConsignmentCode]; visible {
			view.Facts = append(view.Facts, fact)
		}
	}
	for _, transition := range state.Transitions {
		if _, visible := visibleConsignments[transition.ConsignmentCode]; visible {
			view.Transitions = append(view.Transitions, transition)
		}
	}
	for _, receipt := range state.Receipts {
		if _, visible := visibleConsignments[receipt.ConsignmentCode]; visible {
			view.Receipts = append(view.Receipts, receipt)
		}
	}

	view.Policy.PlanCount = int64(len(view.Plans))
	view.Policy.ConsignmentCount = int64(len(view.Consignments))
	view.Policy.FactCount = int64(len(view.Facts))
	view.Policy.TransitionCount = int64(len(view.Transitions))
	view.Policy.ReceiptCount = int64(len(view.Receipts))
	view.Policy.AwaitingRouteCount = 0
	view.Policy.InTransitCount = 0
	view.Policy.AwaitingReceiptCount = 0
	view.Policy.ReceivedCount = 0
	view.Policy.SettledCount = 0
	view.Policy.ExpiredCount = 0
	view.Policy.VoidedCount = 0
	view.Policy.OrphanedCount = 0
	for _, consignment := range view.Consignments {
		switch consignment.State {
		case cityOpenWorldFreightBatchConsignmentStateAwaitingRoute:
			view.Policy.AwaitingRouteCount++
		case cityOpenWorldFreightBatchConsignmentStateInTransit:
			view.Policy.InTransitCount++
		case cityOpenWorldFreightBatchConsignmentStateAwaitingReceipt:
			view.Policy.AwaitingReceiptCount++
		case cityOpenWorldFreightBatchConsignmentStateReceived:
			view.Policy.ReceivedCount++
		case cityOpenWorldFreightBatchConsignmentStateSettled:
			view.Policy.SettledCount++
		case cityOpenWorldFreightBatchConsignmentStateExpired:
			view.Policy.ExpiredCount++
		case cityOpenWorldFreightBatchConsignmentStateVoided:
			view.Policy.VoidedCount++
		case cityOpenWorldFreightBatchConsignmentStateOrphaned:
			view.Policy.OrphanedCount++
		}
	}
	// This is a scoped view, not the persisted global projection.
	view.Policy.Revision = 0
	return view, nil
}
