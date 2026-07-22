package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// V15 supply-chain rows deliberately keep database IDs out of the canonical
// snapshot. Recovery therefore resolves every foreign key from stable world
// identities: entity/facility/district/resource codes, fact cursors, journal
// cursors, resource-operation cursors, and source command sequences.
type cityOpenWorldSupplyChainRecoveryFactKey struct {
	tick     int64
	sequence int64
}

type cityOpenWorldSupplyChainRecoveryLineKey struct {
	orderCode string
	lineNo    int
}

func requireCityOpenWorldSupplyChainRecoveryFactID(
	factIDs map[cityOpenWorldSupplyChainRecoveryFactKey]int64,
	reference CityOpenWorldRuntimeFactRef,
) (int64, error) {
	id, found := factIDs[cityOpenWorldSupplyChainRecoveryFactKey{tick: reference.Tick, sequence: reference.Sequence}]
	if !found || id <= 0 {
		return 0, fmt.Errorf("supply-chain fact %d/%d is unavailable", reference.Tick, reference.Sequence)
	}
	return id, nil
}

func resolveCityOpenWorldSupplyChainRecoveryCommandID(
	commandIDs map[int64]int64,
	sequence *int64,
) (any, error) {
	if sequence == nil {
		return nil, nil
	}
	id, found := commandIDs[*sequence]
	if !found || id <= 0 {
		return nil, fmt.Errorf("supply-chain source command %d is unavailable", *sequence)
	}
	return id, nil
}

func loadCityOpenWorldSupplyChainRecoveryNodeIDs(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	node CityOpenWorldSupplyChainNode,
) (firmID, facilityID, districtID int64, err error) {
	err = tx.QueryRowContext(ctx, `
SELECT firm.id, facility.id, district.id
FROM city_economic_entities firm
JOIN city_open_world_facilities facility
  ON facility.world_id = firm.world_id AND facility.code = $3 AND facility.state = 'active'
JOIN city_districts district
  ON district.world_id = firm.world_id AND district.code = $4
WHERE firm.world_id = $1 AND firm.entity_type = 'firm' AND firm.code = $2 AND firm.status = 'active'`,
		worldID, node.FirmCode, node.FacilityCode, node.DistrictCode,
	).Scan(&firmID, &facilityID, &districtID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, 0, fmt.Errorf("supply-chain node %s references an unavailable endpoint", node.Code)
	}
	if err != nil {
		return 0, 0, 0, fmt.Errorf("resolve supply-chain node %s endpoint IDs: %w", node.Code, err)
	}
	return firmID, facilityID, districtID, nil
}

func loadCityOpenWorldSupplyChainRecoveryInventoryID(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	entityCode, districtCode, resourceCode string,
) (balanceID, resourceID int64, err error) {
	err = tx.QueryRowContext(ctx, `
SELECT balance.id, resource.id
FROM city_inventory_balances balance
JOIN city_economic_entities entity
  ON entity.id = balance.entity_id AND entity.world_id = balance.world_id
JOIN city_districts district
  ON district.id = balance.district_id AND district.world_id = balance.world_id
JOIN city_resources resource
  ON resource.id = balance.resource_id AND resource.world_id = balance.world_id
WHERE balance.world_id = $1
  AND entity.code = $2 AND district.code = $3 AND resource.code = $4
  AND entity.status = 'active' AND resource.status = 'active' AND resource.storable
  AND balance.status = 'active'`, worldID, entityCode, districtCode, resourceCode,
	).Scan(&balanceID, &resourceID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, fmt.Errorf("supply-chain inventory %s/%s/%s is unavailable", entityCode, districtCode, resourceCode)
	}
	if err != nil {
		return 0, 0, fmt.Errorf("resolve supply-chain inventory %s/%s/%s: %w", entityCode, districtCode, resourceCode, err)
	}
	return balanceID, resourceID, nil
}

func loadCityOpenWorldSupplyChainRecoveryJournalID(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	cursor CityJournalCursor,
) (int64, error) {
	var journalID int64
	err := tx.QueryRowContext(ctx, `
SELECT id
FROM city_journals
WHERE world_id = $1 AND tick = $2 AND sequence = $3 AND posted_at IS NOT NULL`,
		worldID, cursor.Tick, cursor.Sequence,
	).Scan(&journalID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("supply-chain journal %d/%d is unavailable", cursor.Tick, cursor.Sequence)
	}
	if err != nil {
		return 0, fmt.Errorf("resolve supply-chain journal %d/%d: %w", cursor.Tick, cursor.Sequence, err)
	}
	return journalID, nil
}

func loadCityOpenWorldSupplyChainRecoveryResourceOperationID(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	cursor CityResourceOperationCursor,
) (int64, error) {
	var operationID int64
	err := tx.QueryRowContext(ctx, `
SELECT id
FROM city_resource_operations
WHERE world_id = $1 AND tick = $2 AND sequence = $3 AND posted_at IS NOT NULL`,
		worldID, cursor.Tick, cursor.Sequence,
	).Scan(&operationID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("supply-chain resource operation %d/%d is unavailable", cursor.Tick, cursor.Sequence)
	}
	if err != nil {
		return 0, fmt.Errorf("resolve supply-chain resource operation %d/%d: %w", cursor.Tick, cursor.Sequence, err)
	}
	return operationID, nil
}

// restoreCityOpenWorldSupplyChainProjection runs only after V14's runtime,
// social facilities, facts, and immutable economic projections have been
// restored. Monetary journals and resource operations are intentionally not
// regenerated here: they are durable F2/F3 evidence and this layer merely
// reconnects its fact-backed references to them.
func restoreCityOpenWorldSupplyChainProjection(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	supplyChain CityOpenWorldSupplyChainState,
	commandIDs map[int64]int64,
) (int, error) {
	if err := validateCityOpenWorldSupplyChainState(&supplyChain); err != nil {
		return 0, fmt.Errorf("validate V15 supply-chain recovery input: %w", err)
	}
	count := 0
	policy := supplyChain.Policy
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_supply_chain_profiles
    (world_id, profile_id, profile_version, content_hash, baseline_tick,
     node_contract, order_contract, settlement_contract, delivery_contract,
     maximum_orders, maximum_order_lines, maximum_transitions_per_tick,
     accept_timeout_ticks, dispatch_timeout_ticks, node_count, order_count,
     active_order_count, fact_count, reservation_count, release_count,
     dispatch_count, delivery_count, settlement_count, revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14,
        $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25::jsonb)`,
		worldID, policy.ProfileID, policy.ProfileVersion, policy.ContentHash, policy.BaselineTick,
		policy.NodeContract, policy.OrderContract, policy.SettlementContract, policy.DeliveryContract,
		policy.MaximumOrders, policy.MaximumOrderLines, policy.MaximumTransitionsPerTick,
		policy.AcceptTimeoutTicks, policy.DispatchTimeoutTicks, policy.NodeCount, policy.OrderCount,
		policy.ActiveOrderCount, policy.FactCount, policy.ReservationCount, policy.ReleaseCount,
		policy.DispatchCount, policy.DeliveryCount, policy.SettlementCount, policy.Revision, []byte(policy.Metadata)); err != nil {
		return count, fmt.Errorf("restore V15 supply-chain profile: %w", err)
	}
	count++

	for _, node := range supplyChain.Nodes {
		firmID, facilityID, districtID, resolveErr := loadCityOpenWorldSupplyChainRecoveryNodeIDs(ctx, tx, worldID, node)
		if resolveErr != nil {
			return count, resolveErr
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_supply_chain_nodes
    (world_id, code, firm_entity_id, facility_id, district_id, state, baseline_tick, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb)`,
			worldID, node.Code, firmID, facilityID, districtID, node.State, node.BaselineTick, []byte(node.Metadata)); err != nil {
			return count, fmt.Errorf("restore V15 supply-chain node %s: %w", node.Code, err)
		}
		count++
	}

	factIDs := make(map[cityOpenWorldSupplyChainRecoveryFactKey]int64, len(supplyChain.Facts))
	for _, fact := range supplyChain.Facts {
		sourceCommandID, resolveErr := resolveCityOpenWorldSupplyChainRecoveryCommandID(commandIDs, fact.SourceCommandSequence)
		if resolveErr != nil {
			return count, fmt.Errorf("restore V15 supply-chain fact %d/%d: %w", fact.Tick, fact.Sequence, resolveErr)
		}
		var factID int64
		if err := tx.QueryRowContext(ctx, `
INSERT INTO city_open_world_supply_chain_facts
    (world_id, tick, sequence, source_command_id, order_code, fact_type, payload)
VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)
RETURNING id`,
			worldID, fact.Tick, fact.Sequence, sourceCommandID, cityNullableString(fact.OrderCode), fact.FactType, []byte(fact.Payload)).Scan(&factID); err != nil {
			return count, fmt.Errorf("restore V15 supply-chain fact %d/%d: %w", fact.Tick, fact.Sequence, err)
		}
		factIDs[cityOpenWorldSupplyChainRecoveryFactKey{tick: fact.Tick, sequence: fact.Sequence}] = factID
		count++
	}

	for _, order := range supplyChain.Orders {
		createdFactID, resolveErr := requireCityOpenWorldSupplyChainRecoveryFactID(factIDs, order.CreatedFact)
		if resolveErr != nil {
			return count, fmt.Errorf("restore V15 supply-chain order %s: %w", order.Code, resolveErr)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_supply_chain_orders
    (world_id, code, buyer_node_code, seller_node_code, created_tick,
     accept_deadline_tick, dispatch_deadline_tick, created_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb)`,
			worldID, order.Code, order.BuyerNodeCode, order.SellerNodeCode, order.CreatedTick,
			order.AcceptDeadlineTick, order.DispatchDeadlineTick, createdFactID, []byte(order.Metadata)); err != nil {
			return count, fmt.Errorf("restore V15 supply-chain order %s: %w", order.Code, err)
		}
		count++
	}

	for _, line := range supplyChain.Lines {
		sourceBalanceID, resourceID, sourceErr := loadCityOpenWorldSupplyChainRecoveryInventoryID(
			ctx, tx, worldID, line.SourceFirmCode, line.SourceDistrictCode, line.ResourceCode,
		)
		if sourceErr != nil {
			return count, fmt.Errorf("restore V15 supply-chain line %s/%d: %w", line.OrderCode, line.LineNo, sourceErr)
		}
		destinationBalanceID, destinationResourceID, destinationErr := loadCityOpenWorldSupplyChainRecoveryInventoryID(
			ctx, tx, worldID, line.DestinationFirmCode, line.DestinationDistrictCode, line.ResourceCode,
		)
		if destinationErr != nil {
			return count, fmt.Errorf("restore V15 supply-chain line %s/%d: %w", line.OrderCode, line.LineNo, destinationErr)
		}
		if resourceID != destinationResourceID {
			return count, fmt.Errorf("restore V15 supply-chain line %s/%d references mismatched resources", line.OrderCode, line.LineNo)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_supply_chain_order_lines
    (world_id, order_code, line_no, resource_id, source_balance_id, destination_balance_id,
     quantity_units, unit_price_units, total_price_units, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb)`,
			worldID, line.OrderCode, line.LineNo, resourceID, sourceBalanceID, destinationBalanceID,
			line.QuantityUnits, line.UnitPriceUnits, line.TotalPriceUnits, []byte(line.Metadata)); err != nil {
			return count, fmt.Errorf("restore V15 supply-chain line %s/%d: %w", line.OrderCode, line.LineNo, err)
		}
		count++
	}

	for _, transition := range supplyChain.Transitions {
		sourceFactID, resolveErr := requireCityOpenWorldSupplyChainRecoveryFactID(factIDs, transition.SourceFact)
		if resolveErr != nil {
			return count, fmt.Errorf("restore V15 supply-chain transition %s: %w", transition.OrderCode, resolveErr)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_supply_chain_order_transitions
    (world_id, order_code, transition_tick, transition_sequence, state,
     reason_code, source_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb)`,
			worldID, transition.OrderCode, transition.TransitionTick, transition.TransitionSequence,
			transition.State, transition.ReasonCode, sourceFactID, []byte(transition.Metadata)); err != nil {
			return count, fmt.Errorf("restore V15 supply-chain transition %s: %w", transition.OrderCode, err)
		}
		count++
	}

	reservationIDs := make(map[cityOpenWorldSupplyChainRecoveryLineKey]int64, len(supplyChain.Reservations))
	for _, reservation := range supplyChain.Reservations {
		balanceID, _, resolveErr := loadCityOpenWorldSupplyChainRecoveryInventoryID(
			ctx, tx, worldID, reservation.SourceFirmCode, reservation.DistrictCode, reservation.ResourceCode,
		)
		if resolveErr != nil {
			return count, fmt.Errorf("restore V15 supply-chain reservation %s/%d: %w", reservation.OrderCode, reservation.LineNo, resolveErr)
		}
		sourceFactID, factErr := requireCityOpenWorldSupplyChainRecoveryFactID(factIDs, reservation.SourceFact)
		if factErr != nil {
			return count, fmt.Errorf("restore V15 supply-chain reservation %s/%d: %w", reservation.OrderCode, reservation.LineNo, factErr)
		}
		var reservationID int64
		if err := tx.QueryRowContext(ctx, `
INSERT INTO city_open_world_supply_chain_reservations
    (world_id, order_code, line_no, source_balance_id, quantity_units,
     reserved_tick, source_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb)
RETURNING id`,
			worldID, reservation.OrderCode, reservation.LineNo, balanceID, reservation.QuantityUnits,
			reservation.ReservedTick, sourceFactID, []byte(reservation.Metadata)).Scan(&reservationID); err != nil {
			return count, fmt.Errorf("restore V15 supply-chain reservation %s/%d: %w", reservation.OrderCode, reservation.LineNo, err)
		}
		reservationIDs[cityOpenWorldSupplyChainRecoveryLineKey{orderCode: reservation.OrderCode, lineNo: reservation.LineNo}] = reservationID
		count++
	}

	for _, release := range supplyChain.Releases {
		reservationID, found := reservationIDs[cityOpenWorldSupplyChainRecoveryLineKey{orderCode: release.OrderCode, lineNo: release.LineNo}]
		if !found || reservationID <= 0 {
			return count, fmt.Errorf("restore V15 supply-chain release %s/%d references an unavailable reservation", release.OrderCode, release.LineNo)
		}
		sourceFactID, factErr := requireCityOpenWorldSupplyChainRecoveryFactID(factIDs, release.SourceFact)
		if factErr != nil {
			return count, fmt.Errorf("restore V15 supply-chain release %s/%d: %w", release.OrderCode, release.LineNo, factErr)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_supply_chain_reservation_releases
    (world_id, reservation_id, released_tick, reason_code, source_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6::jsonb)`,
			worldID, reservationID, release.ReleasedTick, release.ReasonCode, sourceFactID, []byte(release.Metadata)); err != nil {
			return count, fmt.Errorf("restore V15 supply-chain release %s/%d: %w", release.OrderCode, release.LineNo, err)
		}
		count++
	}

	for _, dispatch := range supplyChain.Dispatches {
		sourceFactID, resolveErr := requireCityOpenWorldSupplyChainRecoveryFactID(factIDs, dispatch.SourceFact)
		if resolveErr != nil {
			return count, fmt.Errorf("restore V15 supply-chain dispatch %s: %w", dispatch.OrderCode, resolveErr)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_supply_chain_dispatches
    (world_id, order_code, dispatched_tick, source_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5::jsonb)`,
			worldID, dispatch.OrderCode, dispatch.DispatchedTick, sourceFactID, []byte(dispatch.Metadata)); err != nil {
			return count, fmt.Errorf("restore V15 supply-chain dispatch %s: %w", dispatch.OrderCode, err)
		}
		count++
	}

	for _, delivery := range supplyChain.Deliveries {
		operationID, operationErr := loadCityOpenWorldSupplyChainRecoveryResourceOperationID(ctx, tx, worldID, delivery.ResourceOperation)
		if operationErr != nil {
			return count, fmt.Errorf("restore V15 supply-chain delivery %s: %w", delivery.OrderCode, operationErr)
		}
		sourceFactID, factErr := requireCityOpenWorldSupplyChainRecoveryFactID(factIDs, delivery.SourceFact)
		if factErr != nil {
			return count, fmt.Errorf("restore V15 supply-chain delivery %s: %w", delivery.OrderCode, factErr)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_supply_chain_deliveries
    (world_id, order_code, delivered_tick, resource_operation_id, source_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6::jsonb)`,
			worldID, delivery.OrderCode, delivery.DeliveredTick, operationID, sourceFactID, []byte(delivery.Metadata)); err != nil {
			return count, fmt.Errorf("restore V15 supply-chain delivery %s: %w", delivery.OrderCode, err)
		}
		count++
	}

	settlementIDs := make(map[string]int64, len(supplyChain.Settlements))
	for _, settlement := range supplyChain.Settlements {
		journalID, journalErr := loadCityOpenWorldSupplyChainRecoveryJournalID(ctx, tx, worldID, settlement.Journal)
		if journalErr != nil {
			return count, fmt.Errorf("restore V15 supply-chain settlement %s/%s: %w", settlement.OrderCode, settlement.SettlementKind, journalErr)
		}
		sourceFactID, factErr := requireCityOpenWorldSupplyChainRecoveryFactID(factIDs, settlement.SourceFact)
		if factErr != nil {
			return count, fmt.Errorf("restore V15 supply-chain settlement %s/%s: %w", settlement.OrderCode, settlement.SettlementKind, factErr)
		}
		var reversalSettlementID any
		if settlement.ReversalOfSettlement != nil {
			key := settlement.OrderCode + "\x00" + *settlement.ReversalOfSettlement
			id, found := settlementIDs[key]
			if !found || id <= 0 {
				return count, fmt.Errorf("restore V15 supply-chain settlement %s/%s references an unavailable reversal origin", settlement.OrderCode, settlement.SettlementKind)
			}
			reversalSettlementID = id
		}
		var settlementID int64
		if err := tx.QueryRowContext(ctx, `
INSERT INTO city_open_world_supply_chain_settlements
    (world_id, order_code, settlement_kind, journal_id, source_fact_id,
     reversal_of_settlement_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)
RETURNING id`,
			worldID, settlement.OrderCode, settlement.SettlementKind, journalID, sourceFactID,
			reversalSettlementID, []byte(settlement.Metadata)).Scan(&settlementID); err != nil {
			return count, fmt.Errorf("restore V15 supply-chain settlement %s/%s: %w", settlement.OrderCode, settlement.SettlementKind, err)
		}
		settlementIDs[settlement.OrderCode+"\x00"+settlement.SettlementKind] = settlementID
		count++
	}
	if err := assertCityOpenWorldSupplyChainFoundation(ctx, tx, worldID); err != nil {
		return count, fmt.Errorf("validate restored V15 supply-chain foundation: %w", err)
	}
	return count, nil
}
