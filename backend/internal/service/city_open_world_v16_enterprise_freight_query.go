package service

import (
	"context"
	"fmt"
)

// GetCityOpenWorldEnterpriseFreightState exposes the V16 transport-adapter
// projection. A route is deliberately presented as logistics evidence only:
// callers never receive an account, credential, inventory balance, or any
// implied delivery/settlement result from this endpoint.
func (s *CityEconomyService) GetCityOpenWorldEnterpriseFreightState(
	ctx context.Context,
	userID, worldID int64,
) (*CityOpenWorldEnterpriseFreightState, error) {
	if userID <= 0 || worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, userID, worldID); err != nil {
		return nil, err
	}
	var version string
	if err := s.db.QueryRowContext(ctx, `SELECT simulation_version FROM city_worlds WHERE id = $1`, worldID).Scan(&version); err != nil {
		return nil, fmt.Errorf("load V16 enterprise-freight world version: %w", err)
	}
	if !cityEngineSupportsOpenWorldEnterpriseFreight(version) {
		return nil, ErrCitySimulationVersion.WithMetadata(map[string]string{"version": version})
	}
	state, err := loadCityOpenWorldEnterpriseFreightState(ctx, s.db, worldID)
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
	return s.projectCityOpenWorldEnterpriseFreightStateForOwnedFirms(ctx, worldID, state, ownedFirmCodes)
}

// projectCityOpenWorldEnterpriseFreightStateForOwnedFirms follows V15's
// contract-side visibility rule. A regular participant sees a transport source
// only when it owns the buyer or seller firm of that dispatch. The profile's
// dynamic counters are recomputed from that scoped view, preventing global
// traffic volume from becoming an activity side channel.
func (s *CityEconomyService) projectCityOpenWorldEnterpriseFreightStateForOwnedFirms(
	ctx context.Context,
	worldID int64,
	state *CityOpenWorldEnterpriseFreightState,
	ownedFirmCodes map[string]struct{},
) (*CityOpenWorldEnterpriseFreightState, error) {
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
		return nil, fmt.Errorf("load V16 enterprise-freight source ownership: %w", err)
	}
	nodeFirms := make(map[string]string)
	for rows.Next() {
		var nodeCode, firmCode string
		if err = rows.Scan(&nodeCode, &firmCode); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan V16 enterprise-freight source ownership: %w", err)
		}
		nodeFirms[nodeCode] = firmCode
	}
	if err = closeCityRows(rows, "iterate V16 enterprise-freight source ownership"); err != nil {
		return nil, err
	}
	visibleSources := make(map[string]struct{})
	view := &CityOpenWorldEnterpriseFreightState{
		Policy:      state.Policy,
		Sources:     make([]CityOpenWorldEnterpriseFreightSource, 0),
		Lines:       make([]CityOpenWorldEnterpriseFreightSourceLine, 0),
		Facts:       make([]CityOpenWorldEnterpriseFreightFact, 0),
		Transitions: make([]CityOpenWorldEnterpriseFreightTransition, 0),
	}
	for _, source := range state.Sources {
		_, sellerOwned := ownedFirmCodes[nodeFirms[source.SellerNodeCode]]
		_, buyerOwned := ownedFirmCodes[nodeFirms[source.BuyerNodeCode]]
		if sellerOwned || buyerOwned {
			visibleSources[source.Code] = struct{}{}
			view.Sources = append(view.Sources, source)
		}
	}
	for _, line := range state.Lines {
		if _, visible := visibleSources[line.SourceCode]; visible {
			view.Lines = append(view.Lines, line)
		}
	}
	for _, fact := range state.Facts {
		if _, visible := visibleSources[fact.SourceCode]; visible {
			view.Facts = append(view.Facts, fact)
		}
	}
	for _, transition := range state.Transitions {
		if _, visible := visibleSources[transition.SourceCode]; visible {
			view.Transitions = append(view.Transitions, transition)
		}
	}
	view.Policy.SourceCount = int64(len(view.Sources))
	view.Policy.FactCount = int64(len(view.Facts))
	view.Policy.TransitionCount = int64(len(view.Transitions))
	view.Policy.PendingCount = 0
	view.Policy.DemandCount = 0
	view.Policy.ScheduledCount = 0
	view.Policy.CompletedCount = 0
	view.Policy.ExpiredCount = 0
	view.Policy.VoidedCount = 0
	view.Policy.OrphanedCount = 0
	view.Policy.SuppressedCount = 0
	for _, source := range view.Sources {
		if source.DemandCode != nil {
			view.Policy.DemandCount++
		}
		switch source.State {
		case cityOpenWorldEnterpriseFreightStateDemandPending:
			view.Policy.PendingCount++
		case cityOpenWorldEnterpriseFreightStateRouteScheduled:
			view.Policy.ScheduledCount++
		case cityOpenWorldEnterpriseFreightStateRouteCompleted:
			view.Policy.CompletedCount++
		case cityOpenWorldEnterpriseFreightStateDemandExpired:
			view.Policy.ExpiredCount++
		case cityOpenWorldEnterpriseFreightStateVoided:
			view.Policy.VoidedCount++
		case cityOpenWorldEnterpriseFreightStateTransportOrphaned:
			view.Policy.OrphanedCount++
		case cityOpenWorldEnterpriseFreightStateSuppressed:
			view.Policy.SuppressedCount++
		}
	}
	view.Policy.Revision = 0
	return view, nil
}
