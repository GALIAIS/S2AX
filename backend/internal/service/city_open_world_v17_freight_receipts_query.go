package service

import (
	"context"
	"fmt"
)

// GetCityOpenWorldEnterpriseFreightReceiptState exposes only the custody and
// receipt projection. It never returns credentials, raw upstream account data,
// inventory balances, or resource-operation payloads.
func (s *CityEconomyService) GetCityOpenWorldEnterpriseFreightReceiptState(
	ctx context.Context,
	userID, worldID int64,
) (*CityOpenWorldEnterpriseFreightReceiptState, error) {
	if userID <= 0 || worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, userID, worldID); err != nil {
		return nil, err
	}
	var version string
	if err := s.db.QueryRowContext(ctx, `SELECT simulation_version FROM city_worlds WHERE id = $1`, worldID).Scan(&version); err != nil {
		return nil, fmt.Errorf("load V17 freight-receipt world version: %w", err)
	}
	if !cityEngineSupportsOpenWorldEnterpriseFreightReceipts(version) {
		return nil, ErrCitySimulationVersion.WithMetadata(map[string]string{"version": version})
	}
	state, err := loadCityOpenWorldEnterpriseFreightReceiptState(ctx, s.db, worldID)
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
	return s.projectCityOpenWorldEnterpriseFreightReceiptStateForOwnedFirms(ctx, worldID, state, ownedFirmCodes)
}

// projectCityOpenWorldEnterpriseFreightReceiptStateForOwnedFirms follows the
// V15/V16 bilateral contract visibility rule. A participant can see custody
// evidence only for a shipment whose buyer or seller firm it owns; all global
// counters are recomputed from that scoped view to prevent traffic-volume side
// channels.
func (s *CityEconomyService) projectCityOpenWorldEnterpriseFreightReceiptStateForOwnedFirms(
	ctx context.Context,
	worldID int64,
	state *CityOpenWorldEnterpriseFreightReceiptState,
	ownedFirmCodes map[string]struct{},
) (*CityOpenWorldEnterpriseFreightReceiptState, error) {
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
		return nil, fmt.Errorf("load V17 freight-receipt shipment ownership: %w", err)
	}
	nodeFirms := make(map[string]string)
	for rows.Next() {
		var nodeCode, firmCode string
		if err = rows.Scan(&nodeCode, &firmCode); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan V17 freight-receipt shipment ownership: %w", err)
		}
		nodeFirms[nodeCode] = firmCode
	}
	if err = closeCityRows(rows, "iterate V17 freight-receipt shipment ownership"); err != nil {
		return nil, err
	}

	view := &CityOpenWorldEnterpriseFreightReceiptState{
		Policy:      state.Policy,
		Shipments:   make([]CityOpenWorldEnterpriseFreightShipment, 0),
		Lines:       make([]CityOpenWorldEnterpriseFreightShipmentLine, 0),
		Facts:       make([]CityOpenWorldEnterpriseFreightReceiptFact, 0),
		Transitions: make([]CityOpenWorldEnterpriseFreightShipmentTransition, 0),
		Receipts:    make([]CityOpenWorldEnterpriseFreightReceipt, 0),
	}
	visible := make(map[string]struct{})
	for _, shipment := range state.Shipments {
		_, sellerOwned := ownedFirmCodes[nodeFirms[shipment.SellerNodeCode]]
		_, buyerOwned := ownedFirmCodes[nodeFirms[shipment.BuyerNodeCode]]
		if sellerOwned || buyerOwned {
			visible[shipment.Code] = struct{}{}
			view.Shipments = append(view.Shipments, shipment)
		}
	}
	for _, line := range state.Lines {
		if _, exists := visible[line.ShipmentCode]; exists {
			view.Lines = append(view.Lines, line)
		}
	}
	for _, fact := range state.Facts {
		if _, exists := visible[fact.ShipmentCode]; exists {
			view.Facts = append(view.Facts, fact)
		}
	}
	for _, transition := range state.Transitions {
		if _, exists := visible[transition.ShipmentCode]; exists {
			view.Transitions = append(view.Transitions, transition)
		}
	}
	for _, receipt := range state.Receipts {
		if _, exists := visible[receipt.ShipmentCode]; exists {
			view.Receipts = append(view.Receipts, receipt)
		}
	}
	view.Policy.ShipmentCount = int64(len(view.Shipments))
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
	for _, shipment := range view.Shipments {
		switch shipment.State {
		case cityOpenWorldEnterpriseFreightReceiptStateAwaitingRoute:
			view.Policy.AwaitingRouteCount++
		case cityOpenWorldEnterpriseFreightReceiptStateInTransit:
			view.Policy.InTransitCount++
		case cityOpenWorldEnterpriseFreightReceiptStateAwaitingReceipt:
			view.Policy.AwaitingReceiptCount++
		case cityOpenWorldEnterpriseFreightReceiptStateReceived:
			view.Policy.ReceivedCount++
		case cityOpenWorldEnterpriseFreightReceiptStateSettled:
			view.Policy.SettledCount++
		case cityOpenWorldEnterpriseFreightReceiptStateExpired:
			view.Policy.ExpiredCount++
		case cityOpenWorldEnterpriseFreightReceiptStateVoided:
			view.Policy.VoidedCount++
		case cityOpenWorldEnterpriseFreightReceiptStateOrphaned:
			view.Policy.OrphanedCount++
		}
	}
	// A scoped view is not the persisted projection, so its profile revision
	// is intentionally redacted rather than suggesting a global change count.
	view.Policy.Revision = 0
	return view, nil
}
