package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

func loadCityOpenWorldSupplyChainFactsForTick(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, tick int64,
) ([]CityOpenWorldSupplyChainFact, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT fact.tick, fact.sequence, command.sequence, fact.order_code, fact.fact_type, fact.payload
FROM city_open_world_supply_chain_facts fact
LEFT JOIN city_commands command
  ON command.id = fact.source_command_id AND command.world_id = fact.world_id
WHERE fact.world_id = $1 AND fact.tick = $2
ORDER BY fact.sequence`, worldID, tick)
	if err != nil {
		return nil, fmt.Errorf("load V15 supply-chain facts for tick: %w", err)
	}
	items := make([]CityOpenWorldSupplyChainFact, 0)
	for rows.Next() {
		item := CityOpenWorldSupplyChainFact{}
		var commandSequence sql.NullInt64
		var orderCode sql.NullString
		if err = rows.Scan(&item.Tick, &item.Sequence, &commandSequence, &orderCode, &item.FactType, &item.Payload); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan V15 supply-chain fact for tick: %w", err)
		}
		item.SourceCommandSequence = nullInt64Pointer(commandSequence)
		if orderCode.Valid {
			item.OrderCode = cityOpenWorldStringPointer(orderCode.String)
		}
		items = append(items, item)
	}
	if err = closeCityRows(rows, "iterate V15 supply-chain facts for tick"); err != nil {
		return nil, err
	}
	return items, nil
}

// loadCityOpenWorldSupplyChainState reads the V15 projection through its
// immutable evidence links. The canonical hash never trusts a denormalized
// counter without rebuilding the matching ordered state first.
func loadCityOpenWorldSupplyChainState(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*CityOpenWorldSupplyChainState, error) {
	if worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	state := &CityOpenWorldSupplyChainState{
		Nodes: make([]CityOpenWorldSupplyChainNode, 0), Facts: make([]CityOpenWorldSupplyChainFact, 0),
		Orders: make([]CityOpenWorldSupplyChainOrder, 0), Lines: make([]CityOpenWorldSupplyChainOrderLine, 0),
		Transitions: make([]CityOpenWorldSupplyChainOrderTransition, 0), Reservations: make([]CityOpenWorldSupplyChainReservation, 0),
		Releases: make([]CityOpenWorldSupplyChainReservationRelease, 0), Dispatches: make([]CityOpenWorldSupplyChainDispatch, 0),
		Deliveries: make([]CityOpenWorldSupplyChainDelivery, 0), Settlements: make([]CityOpenWorldSupplyChainSettlement, 0),
	}
	if err := queryer.QueryRowContext(ctx, `
SELECT profile_id, profile_version, content_hash, baseline_tick,
       node_contract, order_contract, settlement_contract, delivery_contract,
       maximum_orders, maximum_order_lines, maximum_transitions_per_tick,
       accept_timeout_ticks, dispatch_timeout_ticks, node_count, order_count,
       active_order_count, fact_count, reservation_count, release_count,
       dispatch_count, delivery_count, settlement_count, revision, metadata
FROM city_open_world_supply_chain_profiles
WHERE world_id = $1`, worldID).Scan(
		&state.Policy.ProfileID, &state.Policy.ProfileVersion, &state.Policy.ContentHash,
		&state.Policy.BaselineTick, &state.Policy.NodeContract, &state.Policy.OrderContract,
		&state.Policy.SettlementContract, &state.Policy.DeliveryContract, &state.Policy.MaximumOrders,
		&state.Policy.MaximumOrderLines, &state.Policy.MaximumTransitionsPerTick,
		&state.Policy.AcceptTimeoutTicks, &state.Policy.DispatchTimeoutTicks, &state.Policy.NodeCount,
		&state.Policy.OrderCount, &state.Policy.ActiveOrderCount, &state.Policy.FactCount,
		&state.Policy.ReservationCount, &state.Policy.ReleaseCount, &state.Policy.DispatchCount,
		&state.Policy.DeliveryCount, &state.Policy.SettlementCount, &state.Policy.Revision, &state.Policy.Metadata,
	); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v15_supply_chain_profile"})
	} else if err != nil {
		return nil, fmt.Errorf("load V15 supply-chain profile: %w", err)
	}

	nodeRows, err := queryer.QueryContext(ctx, `
SELECT node.code, firm.code, facility.code, district.code, node.state,
       node.baseline_tick, node.metadata
FROM city_open_world_supply_chain_nodes node
JOIN city_economic_entities firm
  ON firm.id = node.firm_entity_id AND firm.world_id = node.world_id
JOIN city_open_world_facilities facility
  ON facility.id = node.facility_id AND facility.world_id = node.world_id
JOIN city_districts district
  ON district.id = node.district_id AND district.world_id = node.world_id
WHERE node.world_id = $1
ORDER BY node.code`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V15 supply-chain nodes: %w", err)
	}
	for nodeRows.Next() {
		item := CityOpenWorldSupplyChainNode{}
		if err = nodeRows.Scan(&item.Code, &item.FirmCode, &item.FacilityCode, &item.DistrictCode,
			&item.State, &item.BaselineTick, &item.Metadata); err != nil {
			_ = nodeRows.Close()
			return nil, fmt.Errorf("scan V15 supply-chain node: %w", err)
		}
		state.Nodes = append(state.Nodes, item)
	}
	if err = closeCityRows(nodeRows, "iterate V15 supply-chain nodes"); err != nil {
		return nil, err
	}

	factRows, err := queryer.QueryContext(ctx, `
SELECT fact.tick, fact.sequence, command.sequence, fact.order_code, fact.fact_type, fact.payload
FROM city_open_world_supply_chain_facts fact
LEFT JOIN city_commands command
  ON command.id = fact.source_command_id AND command.world_id = fact.world_id
WHERE fact.world_id = $1
ORDER BY fact.tick, fact.sequence`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V15 supply-chain facts: %w", err)
	}
	for factRows.Next() {
		item := CityOpenWorldSupplyChainFact{}
		var commandSequence sql.NullInt64
		var orderCode sql.NullString
		if err = factRows.Scan(&item.Tick, &item.Sequence, &commandSequence, &orderCode, &item.FactType, &item.Payload); err != nil {
			_ = factRows.Close()
			return nil, fmt.Errorf("scan V15 supply-chain fact: %w", err)
		}
		item.SourceCommandSequence = nullInt64Pointer(commandSequence)
		if orderCode.Valid {
			item.OrderCode = cityOpenWorldStringPointer(orderCode.String)
		}
		state.Facts = append(state.Facts, item)
	}
	if err = closeCityRows(factRows, "iterate V15 supply-chain facts"); err != nil {
		return nil, err
	}

	orderRows, err := queryer.QueryContext(ctx, `
SELECT header.code, header.buyer_node_code, header.seller_node_code,
       header.created_tick, header.accept_deadline_tick, header.dispatch_deadline_tick,
       fact.tick, fact.sequence, header.metadata
FROM city_open_world_supply_chain_orders header
JOIN city_open_world_supply_chain_facts fact
  ON fact.id = header.created_fact_id AND fact.world_id = header.world_id
WHERE header.world_id = $1
ORDER BY header.code`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V15 supply-chain orders: %w", err)
	}
	for orderRows.Next() {
		item := CityOpenWorldSupplyChainOrder{}
		if err = orderRows.Scan(&item.Code, &item.BuyerNodeCode, &item.SellerNodeCode,
			&item.CreatedTick, &item.AcceptDeadlineTick, &item.DispatchDeadlineTick,
			&item.CreatedFact.Tick, &item.CreatedFact.Sequence, &item.Metadata); err != nil {
			_ = orderRows.Close()
			return nil, fmt.Errorf("scan V15 supply-chain order: %w", err)
		}
		state.Orders = append(state.Orders, item)
	}
	if err = closeCityRows(orderRows, "iterate V15 supply-chain orders"); err != nil {
		return nil, err
	}

	lineRows, err := queryer.QueryContext(ctx, `
SELECT line.order_code, line.line_no, resource.code,
       source_entity.code, source_district.code,
       destination_entity.code, destination_district.code,
       line.quantity_units, line.unit_price_units, line.total_price_units, line.metadata
FROM city_open_world_supply_chain_order_lines line
JOIN city_resources resource
  ON resource.id = line.resource_id AND resource.world_id = line.world_id
JOIN city_inventory_balances source_balance
  ON source_balance.id = line.source_balance_id AND source_balance.world_id = line.world_id
JOIN city_economic_entities source_entity
  ON source_entity.id = source_balance.entity_id AND source_entity.world_id = source_balance.world_id
JOIN city_districts source_district
  ON source_district.id = source_balance.district_id AND source_district.world_id = source_balance.world_id
JOIN city_inventory_balances destination_balance
  ON destination_balance.id = line.destination_balance_id AND destination_balance.world_id = line.world_id
JOIN city_economic_entities destination_entity
  ON destination_entity.id = destination_balance.entity_id AND destination_entity.world_id = destination_balance.world_id
JOIN city_districts destination_district
  ON destination_district.id = destination_balance.district_id AND destination_district.world_id = destination_balance.world_id
WHERE line.world_id = $1
ORDER BY line.order_code, line.line_no`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V15 supply-chain order lines: %w", err)
	}
	for lineRows.Next() {
		item := CityOpenWorldSupplyChainOrderLine{}
		if err = lineRows.Scan(&item.OrderCode, &item.LineNo, &item.ResourceCode,
			&item.SourceFirmCode, &item.SourceDistrictCode, &item.DestinationFirmCode,
			&item.DestinationDistrictCode, &item.QuantityUnits, &item.UnitPriceUnits,
			&item.TotalPriceUnits, &item.Metadata); err != nil {
			_ = lineRows.Close()
			return nil, fmt.Errorf("scan V15 supply-chain order line: %w", err)
		}
		state.Lines = append(state.Lines, item)
	}
	if err = closeCityRows(lineRows, "iterate V15 supply-chain order lines"); err != nil {
		return nil, err
	}

	transitionRows, err := queryer.QueryContext(ctx, `
SELECT transition.order_code, transition.transition_tick, transition.transition_sequence,
       transition.state, transition.reason_code, fact.tick, fact.sequence, transition.metadata
FROM city_open_world_supply_chain_order_transitions transition
JOIN city_open_world_supply_chain_facts fact
  ON fact.id = transition.source_fact_id AND fact.world_id = transition.world_id
WHERE transition.world_id = $1
ORDER BY transition.order_code, transition.transition_tick, transition.transition_sequence`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V15 supply-chain transitions: %w", err)
	}
	for transitionRows.Next() {
		item := CityOpenWorldSupplyChainOrderTransition{}
		if err = transitionRows.Scan(&item.OrderCode, &item.TransitionTick, &item.TransitionSequence,
			&item.State, &item.ReasonCode, &item.SourceFact.Tick, &item.SourceFact.Sequence,
			&item.Metadata); err != nil {
			_ = transitionRows.Close()
			return nil, fmt.Errorf("scan V15 supply-chain transition: %w", err)
		}
		state.Transitions = append(state.Transitions, item)
	}
	if err = closeCityRows(transitionRows, "iterate V15 supply-chain transitions"); err != nil {
		return nil, err
	}

	reservationRows, err := queryer.QueryContext(ctx, `
SELECT reservation.order_code, reservation.line_no, entity.code, district.code,
       resource.code, reservation.quantity_units, reservation.reserved_tick,
       fact.tick, fact.sequence, reservation.metadata
FROM city_open_world_supply_chain_reservations reservation
JOIN city_inventory_balances balance
  ON balance.id = reservation.source_balance_id AND balance.world_id = reservation.world_id
JOIN city_economic_entities entity
  ON entity.id = balance.entity_id AND entity.world_id = balance.world_id
JOIN city_districts district
  ON district.id = balance.district_id AND district.world_id = balance.world_id
JOIN city_resources resource
  ON resource.id = balance.resource_id AND resource.world_id = balance.world_id
JOIN city_open_world_supply_chain_facts fact
  ON fact.id = reservation.source_fact_id AND fact.world_id = reservation.world_id
WHERE reservation.world_id = $1
ORDER BY reservation.order_code, reservation.line_no`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V15 supply-chain reservations: %w", err)
	}
	for reservationRows.Next() {
		item := CityOpenWorldSupplyChainReservation{}
		if err = reservationRows.Scan(&item.OrderCode, &item.LineNo, &item.SourceFirmCode,
			&item.DistrictCode, &item.ResourceCode, &item.QuantityUnits, &item.ReservedTick,
			&item.SourceFact.Tick, &item.SourceFact.Sequence, &item.Metadata); err != nil {
			_ = reservationRows.Close()
			return nil, fmt.Errorf("scan V15 supply-chain reservation: %w", err)
		}
		state.Reservations = append(state.Reservations, item)
	}
	if err = closeCityRows(reservationRows, "iterate V15 supply-chain reservations"); err != nil {
		return nil, err
	}

	releaseRows, err := queryer.QueryContext(ctx, `
SELECT reservation.order_code, reservation.line_no, release.released_tick, release.reason_code,
       fact.tick, fact.sequence, release.metadata
FROM city_open_world_supply_chain_reservation_releases release
JOIN city_open_world_supply_chain_reservations reservation
  ON reservation.id = release.reservation_id AND reservation.world_id = release.world_id
JOIN city_open_world_supply_chain_facts fact
  ON fact.id = release.source_fact_id AND fact.world_id = release.world_id
WHERE release.world_id = $1
ORDER BY reservation.order_code, reservation.line_no`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V15 supply-chain reservation releases: %w", err)
	}
	for releaseRows.Next() {
		item := CityOpenWorldSupplyChainReservationRelease{}
		if err = releaseRows.Scan(&item.OrderCode, &item.LineNo, &item.ReleasedTick, &item.ReasonCode,
			&item.SourceFact.Tick, &item.SourceFact.Sequence, &item.Metadata); err != nil {
			_ = releaseRows.Close()
			return nil, fmt.Errorf("scan V15 supply-chain reservation release: %w", err)
		}
		state.Releases = append(state.Releases, item)
	}
	if err = closeCityRows(releaseRows, "iterate V15 supply-chain reservation releases"); err != nil {
		return nil, err
	}

	dispatchRows, err := queryer.QueryContext(ctx, `
SELECT dispatch.order_code, dispatch.dispatched_tick, fact.tick, fact.sequence, dispatch.metadata
FROM city_open_world_supply_chain_dispatches dispatch
JOIN city_open_world_supply_chain_facts fact
  ON fact.id = dispatch.source_fact_id AND fact.world_id = dispatch.world_id
WHERE dispatch.world_id = $1
ORDER BY dispatch.order_code`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V15 supply-chain dispatches: %w", err)
	}
	for dispatchRows.Next() {
		item := CityOpenWorldSupplyChainDispatch{}
		if err = dispatchRows.Scan(&item.OrderCode, &item.DispatchedTick, &item.SourceFact.Tick,
			&item.SourceFact.Sequence, &item.Metadata); err != nil {
			_ = dispatchRows.Close()
			return nil, fmt.Errorf("scan V15 supply-chain dispatch: %w", err)
		}
		state.Dispatches = append(state.Dispatches, item)
	}
	if err = closeCityRows(dispatchRows, "iterate V15 supply-chain dispatches"); err != nil {
		return nil, err
	}

	deliveryRows, err := queryer.QueryContext(ctx, `
SELECT delivery.order_code, delivery.delivered_tick, operation.tick, operation.sequence,
       fact.tick, fact.sequence, delivery.metadata
FROM city_open_world_supply_chain_deliveries delivery
JOIN city_resource_operations operation
  ON operation.id = delivery.resource_operation_id AND operation.world_id = delivery.world_id
JOIN city_open_world_supply_chain_facts fact
  ON fact.id = delivery.source_fact_id AND fact.world_id = delivery.world_id
WHERE delivery.world_id = $1
ORDER BY delivery.order_code`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V15 supply-chain deliveries: %w", err)
	}
	for deliveryRows.Next() {
		item := CityOpenWorldSupplyChainDelivery{}
		if err = deliveryRows.Scan(&item.OrderCode, &item.DeliveredTick, &item.ResourceOperation.Tick,
			&item.ResourceOperation.Sequence, &item.SourceFact.Tick, &item.SourceFact.Sequence,
			&item.Metadata); err != nil {
			_ = deliveryRows.Close()
			return nil, fmt.Errorf("scan V15 supply-chain delivery: %w", err)
		}
		state.Deliveries = append(state.Deliveries, item)
	}
	if err = closeCityRows(deliveryRows, "iterate V15 supply-chain deliveries"); err != nil {
		return nil, err
	}

	settlementRows, err := queryer.QueryContext(ctx, `
SELECT settlement.order_code, settlement.settlement_kind, journal.tick, journal.sequence,
       fact.tick, fact.sequence, reversal.settlement_kind, settlement.metadata
FROM city_open_world_supply_chain_settlements settlement
JOIN city_journals journal
  ON journal.id = settlement.journal_id AND journal.world_id = settlement.world_id
JOIN city_open_world_supply_chain_facts fact
  ON fact.id = settlement.source_fact_id AND fact.world_id = settlement.world_id
LEFT JOIN city_open_world_supply_chain_settlements reversal
  ON reversal.id = settlement.reversal_of_settlement_id AND reversal.world_id = settlement.world_id
WHERE settlement.world_id = $1
ORDER BY settlement.order_code, settlement.settlement_kind`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V15 supply-chain settlements: %w", err)
	}
	for settlementRows.Next() {
		item := CityOpenWorldSupplyChainSettlement{}
		var reversalKind sql.NullString
		if err = settlementRows.Scan(&item.OrderCode, &item.SettlementKind, &item.Journal.Tick,
			&item.Journal.Sequence, &item.SourceFact.Tick, &item.SourceFact.Sequence,
			&reversalKind, &item.Metadata); err != nil {
			_ = settlementRows.Close()
			return nil, fmt.Errorf("scan V15 supply-chain settlement: %w", err)
		}
		if reversalKind.Valid {
			item.ReversalOfSettlement = cityOpenWorldStringPointer(reversalKind.String)
		}
		state.Settlements = append(state.Settlements, item)
	}
	if err = closeCityRows(settlementRows, "iterate V15 supply-chain settlements"); err != nil {
		return nil, err
	}

	sortCityOpenWorldSupplyChainState(state)
	if err = validateCityOpenWorldSupplyChainState(state); err != nil {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v15_supply_chain"}).WithCause(err)
	}
	return state, nil
}

// GetCityOpenWorldSupplyChainState is deliberately world-read scoped. V15
// publishes only codes, aggregate prices, quantities, and lifecycle evidence;
// credentials and account internals remain outside this projection.
func (s *CityEconomyService) GetCityOpenWorldSupplyChainState(
	ctx context.Context,
	userID, worldID int64,
) (*CityOpenWorldSupplyChainState, error) {
	if userID <= 0 || worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, userID, worldID); err != nil {
		return nil, err
	}
	var version string
	if err := s.db.QueryRowContext(ctx, `SELECT simulation_version FROM city_worlds WHERE id = $1`, worldID).Scan(&version); err != nil {
		return nil, fmt.Errorf("load V15 supply-chain world version: %w", err)
	}
	if !cityEngineSupportsOpenWorldSupplyChain(version) {
		return nil, ErrCitySimulationVersion.WithMetadata(map[string]string{"version": version})
	}
	state, err := loadCityOpenWorldSupplyChainState(ctx, s.db, worldID)
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
	return projectCityOpenWorldSupplyChainStateForOwnedFirms(state, ownedFirmCodes), nil
}

// cityOpenWorldSupplyChainReadScope intentionally separates ordinary world
// membership from economic ownership.  Membership grants access to the game,
// while order histories, prices, quantities, and lifecycle cursors remain
// visible to a regular member only when one of the two contract firms is
// theirs.  World owners and system administrators keep the management view.
func (s *CityEconomyService) cityOpenWorldSupplyChainReadScope(
	ctx context.Context,
	userID, worldID int64,
) (bool, map[string]struct{}, error) {
	if IsCitySystemAdministrator(ctx) {
		return true, nil, nil
	}
	var role string
	err := s.db.QueryRowContext(ctx, `
SELECT role
FROM city_members
WHERE world_id = $1 AND user_id = $2 AND status = 'active'`, worldID, userID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil, ErrCityPermissionDenied
	}
	if err != nil {
		return false, nil, fmt.Errorf("load V15 supply-chain read role: %w", err)
	}
	if role == CityMemberRoleOwner {
		return true, nil, nil
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT DISTINCT firm.code
FROM city_open_world_supply_chain_nodes node
JOIN city_economic_entities firm
  ON firm.id = node.firm_entity_id AND firm.world_id = node.world_id
WHERE node.world_id = $1 AND node.state = 'active'
  AND firm.entity_type = 'firm' AND firm.status = 'active'
  AND firm.owner_user_id = $2
ORDER BY firm.code`, worldID, userID)
	if err != nil {
		return false, nil, fmt.Errorf("load V15 supply-chain owned firms: %w", err)
	}
	ownedFirmCodes := make(map[string]struct{})
	for rows.Next() {
		var firmCode string
		if err = rows.Scan(&firmCode); err != nil {
			_ = rows.Close()
			return false, nil, fmt.Errorf("scan V15 supply-chain owned firm: %w", err)
		}
		ownedFirmCodes[firmCode] = struct{}{}
	}
	if err = closeCityRows(rows, "iterate V15 supply-chain owned firms"); err != nil {
		return false, nil, err
	}
	return false, ownedFirmCodes, nil
}

// projectCityOpenWorldSupplyChainStateForOwnedFirms produces the API view for
// a regular member.  The canonical state is loaded and verified first; this
// projection deliberately does not re-run canonical validation because its
// counters and relation sets are scoped to the caller rather than the world.
// Keeping the profile revision at zero also avoids using unrelated contracts
// as a side channel for observing another firm's activity.
func projectCityOpenWorldSupplyChainStateForOwnedFirms(
	state *CityOpenWorldSupplyChainState,
	ownedFirmCodes map[string]struct{},
) *CityOpenWorldSupplyChainState {
	if state == nil {
		return nil
	}
	nodesByCode := make(map[string]CityOpenWorldSupplyChainNode, len(state.Nodes))
	visibleNodeCodes := make(map[string]struct{})
	for _, node := range state.Nodes {
		nodesByCode[node.Code] = node
		if _, owned := ownedFirmCodes[node.FirmCode]; owned {
			visibleNodeCodes[node.Code] = struct{}{}
		}
	}
	visibleOrderCodes := make(map[string]struct{})
	for _, order := range state.Orders {
		buyer, buyerFound := nodesByCode[order.BuyerNodeCode]
		seller, sellerFound := nodesByCode[order.SellerNodeCode]
		if (buyerFound && cityOpenWorldSupplyChainFirmOwned(buyer.FirmCode, ownedFirmCodes)) ||
			(sellerFound && cityOpenWorldSupplyChainFirmOwned(seller.FirmCode, ownedFirmCodes)) {
			visibleOrderCodes[order.Code] = struct{}{}
			visibleNodeCodes[order.BuyerNodeCode] = struct{}{}
			visibleNodeCodes[order.SellerNodeCode] = struct{}{}
		}
	}

	view := &CityOpenWorldSupplyChainState{
		Policy:       state.Policy,
		Nodes:        make([]CityOpenWorldSupplyChainNode, 0, len(visibleNodeCodes)),
		Facts:        make([]CityOpenWorldSupplyChainFact, 0),
		Orders:       make([]CityOpenWorldSupplyChainOrder, 0, len(visibleOrderCodes)),
		Lines:        make([]CityOpenWorldSupplyChainOrderLine, 0),
		Transitions:  make([]CityOpenWorldSupplyChainOrderTransition, 0),
		Reservations: make([]CityOpenWorldSupplyChainReservation, 0),
		Releases:     make([]CityOpenWorldSupplyChainReservationRelease, 0),
		Dispatches:   make([]CityOpenWorldSupplyChainDispatch, 0),
		Deliveries:   make([]CityOpenWorldSupplyChainDelivery, 0),
		Settlements:  make([]CityOpenWorldSupplyChainSettlement, 0),
	}
	for _, node := range state.Nodes {
		if _, visible := visibleNodeCodes[node.Code]; visible {
			view.Nodes = append(view.Nodes, node)
		}
	}
	for _, fact := range state.Facts {
		if fact.OrderCode != nil {
			if _, visible := visibleOrderCodes[*fact.OrderCode]; visible {
				view.Facts = append(view.Facts, fact)
			}
		}
	}
	for _, order := range state.Orders {
		if _, visible := visibleOrderCodes[order.Code]; visible {
			view.Orders = append(view.Orders, order)
		}
	}
	for _, line := range state.Lines {
		if _, visible := visibleOrderCodes[line.OrderCode]; visible {
			view.Lines = append(view.Lines, line)
		}
	}
	for _, transition := range state.Transitions {
		if _, visible := visibleOrderCodes[transition.OrderCode]; visible {
			view.Transitions = append(view.Transitions, transition)
		}
	}
	for _, reservation := range state.Reservations {
		if _, visible := visibleOrderCodes[reservation.OrderCode]; visible {
			view.Reservations = append(view.Reservations, reservation)
		}
	}
	for _, release := range state.Releases {
		if _, visible := visibleOrderCodes[release.OrderCode]; visible {
			view.Releases = append(view.Releases, release)
		}
	}
	for _, dispatch := range state.Dispatches {
		if _, visible := visibleOrderCodes[dispatch.OrderCode]; visible {
			view.Dispatches = append(view.Dispatches, dispatch)
		}
	}
	for _, delivery := range state.Deliveries {
		if _, visible := visibleOrderCodes[delivery.OrderCode]; visible {
			view.Deliveries = append(view.Deliveries, delivery)
		}
	}
	for _, settlement := range state.Settlements {
		if _, visible := visibleOrderCodes[settlement.OrderCode]; visible {
			view.Settlements = append(view.Settlements, settlement)
		}
	}

	view.Policy.NodeCount = int64(len(view.Nodes))
	view.Policy.OrderCount = int64(len(view.Orders))
	view.Policy.FactCount = int64(len(view.Facts))
	view.Policy.ReservationCount = int64(len(view.Reservations))
	view.Policy.ReleaseCount = int64(len(view.Releases))
	view.Policy.DispatchCount = int64(len(view.Dispatches))
	view.Policy.DeliveryCount = int64(len(view.Deliveries))
	view.Policy.SettlementCount = int64(len(view.Settlements))
	view.Policy.ActiveOrderCount = 0
	for _, order := range view.Orders {
		if !cityOpenWorldSupplyChainStateTerminal(cityOpenWorldSupplyChainCurrentState(view.Transitions, order.Code)) {
			view.Policy.ActiveOrderCount++
		}
	}
	view.Policy.Revision = 0
	return view
}

func cityOpenWorldSupplyChainFirmOwned(firmCode string, ownedFirmCodes map[string]struct{}) bool {
	_, owned := ownedFirmCodes[firmCode]
	return owned
}
