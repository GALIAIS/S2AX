package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/lib/pq"
)

// cityOpenWorldEnterpriseFreightDispatchCandidate is a frozen V15 dispatch
// observed by V16. The adapter reads the current order state only to reject a
// terminal contract; it never alters that V15 lifecycle.
type cityOpenWorldEnterpriseFreightDispatchCandidate struct {
	dispatchFactID     int64
	orderCode          string
	dispatchTick       int64
	sellerNodeCode     string
	buyerNodeCode      string
	sourceHubCode      string
	destinationHubCode string
	carrierActorID     int64
	carrierActorCode   string
}

type cityOpenWorldEnterpriseFreightLineCandidate struct {
	lineNo                  int
	resourceCode            string
	sourceFirmCode          string
	sourceDistrictCode      string
	destinationFirmCode     string
	destinationDistrictCode string
	quantityUnits           int64
	unitPriceUnits          int64
	totalPriceUnits         int64
}

type cityOpenWorldEnterpriseFreightSourceRecord struct {
	id                int64
	orderCode         string
	code              string
	state             string
	carrierActorID    int64
	carrierActorCode  string
	demandID          sql.NullInt64
	routeID           sql.NullInt64
	lastRuntimeFactID int64
	demandStatus      sql.NullString
	routeStatus       sql.NullString
	orderState        string
}

func loadCityOpenWorldEnterpriseFreightPolicyForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
) (*CityOpenWorldEnterpriseFreightPolicy, error) {
	policy := &CityOpenWorldEnterpriseFreightPolicy{}
	err := tx.QueryRowContext(ctx, `
SELECT profile_id, profile_version, content_hash, baseline_tick,
       source_contract, demand_contract, completion_contract, terminal_contract,
       carrier_actor_code, maximum_sources, maximum_generations_per_tick,
       source_count, pending_count, demand_count, scheduled_count, completed_count,
       expired_count, voided_count, orphaned_count, suppressed_count, fact_count,
       transition_count, revision, metadata
FROM city_open_world_enterprise_freight_profiles
WHERE world_id = $1
FOR UPDATE`, worldID).Scan(
		&policy.ProfileID, &policy.ProfileVersion, &policy.ContentHash, &policy.BaselineTick,
		&policy.SourceContract, &policy.DemandContract, &policy.CompletionContract,
		&policy.TerminalContract, &policy.CarrierActorCode, &policy.MaximumSources,
		&policy.MaximumGenerationsPerTick, &policy.SourceCount, &policy.PendingCount,
		&policy.DemandCount, &policy.ScheduledCount, &policy.CompletedCount,
		&policy.ExpiredCount, &policy.VoidedCount, &policy.OrphanedCount,
		&policy.SuppressedCount, &policy.FactCount, &policy.TransitionCount,
		&policy.Revision, &policy.Metadata,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v16_enterprise_freight_profile"})
	}
	if err != nil {
		return nil, fmt.Errorf("lock V16 enterprise-freight profile: %w", err)
	}
	policyHash, hashErr := cityOpenWorldEnterpriseFreightPolicyHash()
	if hashErr != nil || policy.ProfileID != cityOpenWorldEnterpriseFreightProfileID ||
		policy.ProfileVersion != cityOpenWorldEnterpriseFreightProfileVersion ||
		policy.ContentHash != policyHash || policy.BaselineTick < 0 ||
		policy.SourceContract != cityOpenWorldEnterpriseFreightSourceContract ||
		policy.DemandContract != cityOpenWorldEnterpriseFreightDemandContract ||
		policy.CompletionContract != cityOpenWorldEnterpriseFreightCompletionContract ||
		policy.TerminalContract != cityOpenWorldEnterpriseFreightTerminalContract ||
		policy.CarrierActorCode != cityOpenWorldEnterpriseFreightCarrierActorCode ||
		policy.MaximumSources != cityOpenWorldEnterpriseFreightMaximumSources ||
		policy.MaximumGenerationsPerTick != cityOpenWorldEnterpriseFreightMaximumGenerationsTick ||
		policy.Revision < 1 || !cityOpenWorldEnterpriseFreightPolicyMetadataValid(policy.Metadata) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v16_enterprise_freight_profile"})
	}
	return policy, nil
}

func updateCityOpenWorldEnterpriseFreightPolicy(
	ctx context.Context,
	tx *sql.Tx,
	worldID, sourceDelta, pendingDelta, demandDelta, scheduledDelta, completedDelta,
	expiredDelta, voidedDelta, orphanedDelta, suppressedDelta, factDelta, transitionDelta int64,
) error {
	result, err := tx.ExecContext(ctx, `
UPDATE city_open_world_enterprise_freight_profiles
SET source_count = source_count + $2,
    pending_count = pending_count + $3,
    demand_count = demand_count + $4,
    scheduled_count = scheduled_count + $5,
    completed_count = completed_count + $6,
    expired_count = expired_count + $7,
    voided_count = voided_count + $8,
    orphaned_count = orphaned_count + $9,
    suppressed_count = suppressed_count + $10,
    fact_count = fact_count + $11,
    transition_count = transition_count + $12,
    revision = revision + 1,
    updated_at = NOW()
WHERE world_id = $1`, worldID, sourceDelta, pendingDelta, demandDelta, scheduledDelta,
		completedDelta, expiredDelta, voidedDelta, orphanedDelta, suppressedDelta,
		factDelta, transitionDelta)
	if err != nil {
		return fmt.Errorf("update V16 enterprise-freight profile: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v16_enterprise_freight_profile"})
	}
	return nil
}

// advanceCityOpenWorldV16EnterpriseFreight is intentionally placed before V9
// scheduling. A dispatch visible at T creates a demand at T+1, and V9's own
// requested_tick guard means network allocation cannot occur before T+2.
func advanceCityOpenWorldV16EnterpriseFreight(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence int64,
) (cityOpenWorldRuntimeAutomaticExecution, error) {
	execution := cityOpenWorldRuntimeAutomaticExecution{
		facts: make([]CityOpenWorldRuntimeFact, 0), effects: make([]CityOpenWorldRuntimeEffect, 0),
		events: make([]worldRuntimeAutomaticEvent, 0), nextFactSeq: factSequence,
	}
	policy, err := loadCityOpenWorldEnterpriseFreightPolicyForUpdate(ctx, tx, worldID)
	if err != nil {
		return execution, err
	}
	if targetTick <= policy.BaselineTick {
		return execution, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v16_enterprise_freight_baseline"})
	}
	if err = activateCityOpenWorldRuntimeFactWrite(ctx, tx, worldID); err != nil {
		return execution, err
	}
	if err = activateCityOpenWorldEnterpriseFreightWrite(ctx, tx, worldID); err != nil {
		return execution, err
	}
	// Project V9 transport state first. A terminal V15 order may race with a
	// demand that V9 has already scheduled or completed; recording the transport
	// observation before terminal reconciliation keeps the append-only source
	// state machine causally complete.
	if err = observeCityOpenWorldEnterpriseFreightTransport(ctx, tx, worldID, targetTick, policy, &execution); err != nil {
		return execution, err
	}
	if err = reconcileCityOpenWorldEnterpriseFreightTerminalSources(ctx, tx, worldID, targetTick, policy, &execution); err != nil {
		return execution, err
	}
	if err = generateCityOpenWorldEnterpriseFreightSources(ctx, tx, worldID, targetTick, policy, &execution); err != nil {
		return execution, err
	}
	if len(execution.facts) > 0 {
		if err = updateCityOpenWorldRuntimeProfile(ctx, tx, worldID, 0, int64(len(execution.facts)), 0, 0); err != nil {
			return execution, err
		}
	}
	if _, err = tx.ExecContext(ctx, `SELECT assert_city_open_world_enterprise_freight_foundation($1)`, worldID); err != nil {
		return execution, fmt.Errorf("validate V16 enterprise-freight foundation after advancement: %w", err)
	}
	return execution, nil
}

func loadCityOpenWorldEnterpriseFreightDispatchCandidates(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	limit int,
) ([]cityOpenWorldEnterpriseFreightDispatchCandidate, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT dispatch.source_fact_id, dispatch.order_code, dispatch.dispatched_tick,
       header.seller_node_code, header.buyer_node_code,
       source_hub.code, destination_hub.code, carrier.id, carrier.code
FROM city_open_world_supply_chain_dispatches dispatch
JOIN city_open_world_supply_chain_orders header
  ON header.world_id = dispatch.world_id AND header.code = dispatch.order_code
JOIN LATERAL (
    SELECT transition.state
    FROM city_open_world_supply_chain_order_transitions transition
    WHERE transition.world_id = header.world_id AND transition.order_code = header.code
    ORDER BY transition.transition_tick DESC, transition.transition_sequence DESC
    LIMIT 1
) current_state ON TRUE
JOIN city_open_world_supply_chain_nodes seller
  ON seller.world_id = header.world_id AND seller.code = header.seller_node_code
JOIN city_open_world_supply_chain_nodes buyer
  ON buyer.world_id = header.world_id AND buyer.code = header.buyer_node_code
JOIN city_open_world_mobility_hubs source_hub
  ON source_hub.world_id = seller.world_id AND source_hub.facility_id = seller.facility_id
JOIN city_open_world_mobility_hubs destination_hub
  ON destination_hub.world_id = buyer.world_id AND destination_hub.facility_id = buyer.facility_id
JOIN city_open_world_actors carrier
  ON carrier.world_id = dispatch.world_id AND carrier.code = $3
WHERE dispatch.world_id = $1
  AND dispatch.dispatched_tick < $2
  AND current_state.state = 'dispatched'
  AND NOT EXISTS (
      SELECT 1 FROM city_open_world_enterprise_freight_sources source
      WHERE source.world_id = dispatch.world_id AND source.order_code = dispatch.order_code
  )
ORDER BY dispatch.dispatched_tick ASC, dispatch.order_code ASC
LIMIT $4
FOR UPDATE OF dispatch, header`, worldID, targetTick,
		cityOpenWorldEnterpriseFreightCarrierActorCode, limit)
	if err != nil {
		return nil, fmt.Errorf("load V16 enterprise-freight dispatch candidates: %w", err)
	}
	items := make([]cityOpenWorldEnterpriseFreightDispatchCandidate, 0)
	for rows.Next() {
		item := cityOpenWorldEnterpriseFreightDispatchCandidate{}
		if err = rows.Scan(&item.dispatchFactID, &item.orderCode, &item.dispatchTick,
			&item.sellerNodeCode, &item.buyerNodeCode, &item.sourceHubCode,
			&item.destinationHubCode, &item.carrierActorID, &item.carrierActorCode); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan V16 enterprise-freight dispatch candidate: %w", err)
		}
		items = append(items, item)
	}
	if err = closeCityRows(rows, "iterate V16 enterprise-freight dispatch candidates"); err != nil {
		return nil, err
	}
	return items, nil
}

func loadCityOpenWorldEnterpriseFreightLineCandidates(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	orderCode string,
) ([]cityOpenWorldEnterpriseFreightLineCandidate, int64, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT line.line_no, resource.code, source_firm.code, source_district.code,
       destination_firm.code, destination_district.code, line.quantity_units,
       line.unit_price_units, line.total_price_units
FROM city_open_world_supply_chain_order_lines line
JOIN city_resources resource
  ON resource.id = line.resource_id AND resource.world_id = line.world_id
JOIN city_inventory_balances source_balance
  ON source_balance.id = line.source_balance_id AND source_balance.world_id = line.world_id
JOIN city_inventory_balances destination_balance
  ON destination_balance.id = line.destination_balance_id AND destination_balance.world_id = line.world_id
JOIN city_economic_entities source_firm
  ON source_firm.id = source_balance.entity_id AND source_firm.world_id = line.world_id
JOIN city_districts source_district
  ON source_district.id = source_balance.district_id AND source_district.world_id = line.world_id
JOIN city_economic_entities destination_firm
  ON destination_firm.id = destination_balance.entity_id AND destination_firm.world_id = line.world_id
JOIN city_districts destination_district
  ON destination_district.id = destination_balance.district_id AND destination_district.world_id = line.world_id
WHERE line.world_id = $1 AND line.order_code = $2
ORDER BY line.line_no
FOR UPDATE OF line`, worldID, orderCode)
	if err != nil {
		return nil, 0, fmt.Errorf("load V16 enterprise-freight order lines: %w", err)
	}
	items := make([]cityOpenWorldEnterpriseFreightLineCandidate, 0)
	var requestedUnits int64
	for rows.Next() {
		item := cityOpenWorldEnterpriseFreightLineCandidate{}
		if err = rows.Scan(&item.lineNo, &item.resourceCode, &item.sourceFirmCode,
			&item.sourceDistrictCode, &item.destinationFirmCode, &item.destinationDistrictCode,
			&item.quantityUnits, &item.unitPriceUnits, &item.totalPriceUnits); err != nil {
			_ = rows.Close()
			return nil, 0, fmt.Errorf("scan V16 enterprise-freight order line: %w", err)
		}
		if item.quantityUnits <= 0 || item.quantityUnits > math.MaxInt64-requestedUnits {
			_ = rows.Close()
			return nil, 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v16_enterprise_freight_units"})
		}
		requestedUnits += item.quantityUnits
		items = append(items, item)
	}
	if err = closeCityRows(rows, "iterate V16 enterprise-freight order lines"); err != nil {
		return nil, 0, err
	}
	if len(items) == 0 || requestedUnits <= 0 {
		return nil, 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v16_enterprise_freight_lines"})
	}
	return items, requestedUnits, nil
}

func generateCityOpenWorldEnterpriseFreightSources(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	policy *CityOpenWorldEnterpriseFreightPolicy,
	execution *cityOpenWorldRuntimeAutomaticExecution,
) error {
	if policy == nil || execution == nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v16_enterprise_freight_generation"})
	}
	remaining := policy.MaximumSources - int(policy.SourceCount)
	if remaining <= 0 {
		return nil
	}
	if remaining > policy.MaximumGenerationsPerTick {
		remaining = policy.MaximumGenerationsPerTick
	}
	candidates, err := loadCityOpenWorldEnterpriseFreightDispatchCandidates(ctx, tx, worldID, targetTick, remaining)
	if err != nil {
		return err
	}
	for _, candidate := range candidates {
		lines, requestedUnits, lineErr := loadCityOpenWorldEnterpriseFreightLineCandidates(ctx, tx, worldID, candidate.orderCode)
		if lineErr != nil {
			return lineErr
		}
		if err = createCityOpenWorldEnterpriseFreightSource(ctx, tx, worldID, targetTick, policy, candidate, lines, requestedUnits, execution); err != nil {
			return err
		}
		policy.SourceCount++
	}
	return nil
}

func createCityOpenWorldEnterpriseFreightSource(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	policy *CityOpenWorldEnterpriseFreightPolicy,
	candidate cityOpenWorldEnterpriseFreightDispatchCandidate,
	lines []cityOpenWorldEnterpriseFreightLineCandidate,
	requestedUnits int64,
	execution *cityOpenWorldRuntimeAutomaticExecution,
) error {
	if candidate.dispatchFactID <= 0 || candidate.carrierActorID <= 0 ||
		candidate.carrierActorCode != cityOpenWorldEnterpriseFreightCarrierActorCode ||
		candidate.dispatchTick <= 0 || candidate.dispatchTick >= targetTick ||
		requestedUnits <= 0 || len(lines) == 0 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v16_enterprise_freight_source"})
	}
	sourceCode := cityOpenWorldEnterpriseFreightSourceCode(candidate.orderCode)
	mobilityDeadline := targetTick + cityOpenWorldMobilityMaximumWaitTicks
	rootPayload, err := json.Marshal(map[string]any{
		"schema_version": cityOpenWorldEnterpriseFreightSchemaVersion,
		"source_code":    sourceCode, "order_code": candidate.orderCode,
		"dispatch_tick": candidate.dispatchTick, "source_tick": targetTick,
		"carrier_actor_code":   candidate.carrierActorCode,
		"source_hub_code":      candidate.sourceHubCode,
		"destination_hub_code": candidate.destinationHubCode,
		"requested_units":      requestedUnits,
		"source_contract":      cityOpenWorldEnterpriseFreightSourceContract,
	})
	if err != nil {
		return fmt.Errorf("marshal V16 enterprise-freight source fact: %w", err)
	}
	root, err := insertCityOpenWorldRuntimeFact(ctx, tx, cityOpenWorldRuntimeFactInsert{
		worldID: worldID, tick: targetTick, sequence: execution.nextFactSeq,
		factType: cityOpenWorldRuntimeFactEnterpriseFreightSourceCreated, payload: rootPayload,
	})
	if err != nil {
		return err
	}
	execution.facts = append(execution.facts, root.fact)
	execution.nextFactSeq++

	if requestedUnits > cityOpenWorldEnterpriseFreightMaximumRequestedUnits {
		return createCityOpenWorldEnterpriseFreightSuppressedSource(ctx, tx, worldID, targetTick, sourceCode,
			candidate, lines, requestedUnits, mobilityDeadline, root, execution)
	}
	return createCityOpenWorldEnterpriseFreightDemandSource(ctx, tx, worldID, targetTick, sourceCode,
		candidate, lines, requestedUnits, mobilityDeadline, root, execution)
}

func cityOpenWorldEnterpriseFreightSourceMetadata(candidate cityOpenWorldEnterpriseFreightDispatchCandidate, requestedUnits int64) ([]byte, error) {
	return json.Marshal(map[string]any{
		"schema_version":  cityOpenWorldEnterpriseFreightSchemaVersion,
		"source_contract": cityOpenWorldEnterpriseFreightSourceContract,
		"order_code":      candidate.orderCode,
		"dispatch_tick":   candidate.dispatchTick,
		"requested_units": requestedUnits,
	})
}

func insertCityOpenWorldEnterpriseFreightSourceLines(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	sourceCode string,
	lines []cityOpenWorldEnterpriseFreightLineCandidate,
) error {
	for _, line := range lines {
		metadata, err := json.Marshal(map[string]any{
			"schema_version":    cityOpenWorldEnterpriseFreightSchemaVersion,
			"snapshot_contract": "v15_order_line_snapshot_v1",
		})
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_enterprise_freight_source_lines
    (world_id, source_code, line_no, resource_code, source_firm_code,
     source_district_code, destination_firm_code, destination_district_code,
     quantity_units, unit_price_units, total_price_units, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb)`,
			worldID, sourceCode, line.lineNo, line.resourceCode, line.sourceFirmCode,
			line.sourceDistrictCode, line.destinationFirmCode, line.destinationDistrictCode,
			line.quantityUnits, line.unitPriceUnits, line.totalPriceUnits, []byte(metadata)); err != nil {
			return fmt.Errorf("insert V16 enterprise-freight source line: %w", err)
		}
	}
	return nil
}

func insertCityOpenWorldEnterpriseFreightFact(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	sourceCode, factType string,
	runtimeFact *cityOpenWorldRuntimeFactRecord,
	payload map[string]any,
) (int64, error) {
	if runtimeFact == nil || runtimeFact.id <= 0 || !cityOpenWorldEnterpriseFreightFactTypeValid(factType) {
		return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v16_enterprise_freight_fact"})
	}
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	var id int64
	if err = tx.QueryRowContext(ctx, `
INSERT INTO city_open_world_enterprise_freight_facts
    (world_id, tick, sequence, source_code, fact_type, runtime_fact_id, payload)
VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)
RETURNING id`, worldID, runtimeFact.fact.Tick, runtimeFact.fact.Sequence,
		sourceCode, factType, runtimeFact.id, []byte(rawPayload)).Scan(&id); err != nil {
		return 0, fmt.Errorf("insert V16 enterprise-freight fact %s: %w", factType, err)
	}
	return id, nil
}

func insertCityOpenWorldEnterpriseFreightTransition(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	sourceCode, state, reasonCode string,
	freightFactID int64,
	runtimeFact *cityOpenWorldRuntimeFactRecord,
	previousState string,
) error {
	if runtimeFact == nil || freightFactID <= 0 || !cityOpenWorldEnterpriseFreightTransitionAllowed(previousState, state) ||
		!cityOpenWorldSupplyChainReasonValid(reasonCode) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v16_enterprise_freight_transition"})
	}
	metadata, err := json.Marshal(map[string]any{
		"schema_version": cityOpenWorldEnterpriseFreightSchemaVersion,
		"previous_state": previousState,
		"source_fact":    map[string]any{"tick": runtimeFact.fact.Tick, "sequence": runtimeFact.fact.Sequence},
	})
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_enterprise_freight_transitions
    (world_id, source_code, transition_tick, transition_sequence, state,
     reason_code, source_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb)`,
		worldID, sourceCode, runtimeFact.fact.Tick, runtimeFact.fact.Sequence,
		state, reasonCode, freightFactID, []byte(metadata)); err != nil {
		return fmt.Errorf("insert V16 enterprise-freight transition: %w", err)
	}
	return nil
}

func insertCityOpenWorldEnterpriseFreightSource(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	sourceCode string,
	candidate cityOpenWorldEnterpriseFreightDispatchCandidate,
	requestedUnits, sourceTick, mobilityDeadline int64,
	state string,
	demandID, routeID *int64,
	rootFact, lastFact *cityOpenWorldRuntimeFactRecord,
) error {
	if rootFact == nil || lastFact == nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v16_enterprise_freight_source_fact"})
	}
	metadata, err := cityOpenWorldEnterpriseFreightSourceMetadata(candidate, requestedUnits)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_enterprise_freight_sources
    (world_id, code, order_code, seller_node_code, buyer_node_code,
     source_hub_code, destination_hub_code, carrier_actor_id, dispatch_fact_id,
     dispatch_tick, source_tick, mobility_deadline_tick, requested_units, state,
     mobility_demand_id, mobility_route_id, source_runtime_fact_id,
     last_runtime_fact_id, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14,
        $15, $16, $17, $18, 1, $19::jsonb)`,
		worldID, sourceCode, candidate.orderCode, candidate.sellerNodeCode, candidate.buyerNodeCode,
		candidate.sourceHubCode, candidate.destinationHubCode, candidate.carrierActorID,
		candidate.dispatchFactID, candidate.dispatchTick, sourceTick, mobilityDeadline,
		requestedUnits, state, cityOpenWorldNullableInt64(demandID), cityOpenWorldNullableInt64(routeID),
		rootFact.id, lastFact.id, []byte(metadata)); err != nil {
		return fmt.Errorf("insert V16 enterprise-freight source: %w", err)
	}
	return nil
}

func createCityOpenWorldEnterpriseFreightDemandSource(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	sourceCode string,
	candidate cityOpenWorldEnterpriseFreightDispatchCandidate,
	lines []cityOpenWorldEnterpriseFreightLineCandidate,
	requestedUnits, mobilityDeadline int64,
	root *cityOpenWorldRuntimeFactRecord,
	execution *cityOpenWorldRuntimeAutomaticExecution,
) error {
	demandCode := cityOpenWorldEnterpriseFreightDemandCode(sourceCode)
	demandPayload, err := json.Marshal(map[string]any{
		"schema_version": cityOpenWorldEnterpriseFreightSchemaVersion,
		"source_code":    sourceCode, "order_code": candidate.orderCode,
		"demand_code": demandCode, "actor_code": candidate.carrierActorCode,
		"source_hub_code": candidate.sourceHubCode, "destination_hub_code": candidate.destinationHubCode,
		"mode_code":       cityOpenWorldEnterpriseFreightModeCode,
		"purpose_code":    cityOpenWorldEnterpriseFreightPurposeCode,
		"requested_units": requestedUnits, "earliest_departure_tick": targetTick + 1,
		"deadline_tick": mobilityDeadline, "arrival_bridge": "excluded",
	})
	if err != nil {
		return fmt.Errorf("marshal V16 enterprise-freight demand fact: %w", err)
	}
	demandFact, err := insertCityOpenWorldRuntimeFact(ctx, tx, cityOpenWorldRuntimeFactInsert{
		worldID: worldID, tick: targetTick, sequence: execution.nextFactSeq,
		parentFactID: &root.id, actorID: &candidate.carrierActorID,
		factType: CityOpenWorldRuntimeFactMobilityRequested, payload: demandPayload,
	})
	if err != nil {
		return err
	}
	execution.facts = append(execution.facts, demandFact.fact)
	execution.nextFactSeq++
	metadata, err := json.Marshal(map[string]any{
		"schema_version": cityOpenWorldMobilitySchemaVersion,
		"origin":         "enterprise_freight_v16",
		"transport_adapter": map[string]any{
			"kind": "enterprise_freight_v1", "source_code": sourceCode,
			"order_code": candidate.orderCode, "arrival_bridge": "excluded",
			"source_contract":     cityOpenWorldEnterpriseFreightSourceContract,
			"completion_contract": cityOpenWorldEnterpriseFreightCompletionContract,
		},
	})
	if err != nil {
		return err
	}
	var demandID int64
	if err = tx.QueryRowContext(ctx, `
INSERT INTO city_open_world_mobility_demands
    (world_id, code, actor_id, source_hub_code, destination_hub_code, mode_code,
     purpose_code, requested_units, requested_tick, earliest_departure_tick,
     deadline_tick, status, source_fact_id, last_fact_id, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11,
        'pending', $12, $12, 1, $13::jsonb)
RETURNING id`, worldID, demandCode, candidate.carrierActorID, candidate.sourceHubCode,
		candidate.destinationHubCode, cityOpenWorldEnterpriseFreightModeCode,
		cityOpenWorldEnterpriseFreightPurposeCode, requestedUnits, targetTick,
		targetTick+1, mobilityDeadline, demandFact.id, []byte(metadata)).Scan(&demandID); err != nil {
		return fmt.Errorf("insert V16 enterprise-freight mobility demand: %w", err)
	}
	metricCreated, err := updateCityOpenWorldMobilityActorMetric(ctx, tx, worldID,
		candidate.carrierActorID, candidate.carrierActorCode, 1, 0, 0, 0, nil, targetTick)
	if err != nil {
		return err
	}
	if err = updateCityOpenWorldMobilityPolicy(ctx, tx, worldID, 1, 0, 0, 0, 0, metricCreated); err != nil {
		return err
	}
	if err = insertCityOpenWorldEnterpriseFreightSource(ctx, tx, worldID, sourceCode,
		candidate, requestedUnits, targetTick, mobilityDeadline, cityOpenWorldEnterpriseFreightStateDemandPending,
		&demandID, nil, root, demandFact); err != nil {
		return err
	}
	if err = insertCityOpenWorldEnterpriseFreightSourceLines(ctx, tx, worldID, sourceCode, lines); err != nil {
		return err
	}
	if _, err = insertCityOpenWorldEnterpriseFreightFact(ctx, tx, worldID, sourceCode, "source.created", root, map[string]any{
		"schema_version": cityOpenWorldEnterpriseFreightSchemaVersion, "order_code": candidate.orderCode,
	}); err != nil {
		return err
	}
	demandFreightFactID, err := insertCityOpenWorldEnterpriseFreightFact(ctx, tx, worldID, sourceCode, "demand.requested", demandFact, map[string]any{
		"schema_version": cityOpenWorldEnterpriseFreightSchemaVersion, "demand_code": demandCode,
	})
	if err != nil {
		return err
	}
	if err = insertCityOpenWorldEnterpriseFreightTransition(ctx, tx, worldID, sourceCode,
		cityOpenWorldEnterpriseFreightStateDemandPending, cityOpenWorldEnterpriseFreightReasonDispatched,
		demandFreightFactID, demandFact, ""); err != nil {
		return err
	}
	if err = updateCityOpenWorldEnterpriseFreightPolicy(ctx, tx, worldID,
		1, 1, 1, 0, 0, 0, 0, 0, 0, 2, 1); err != nil {
		return err
	}
	if err = postCityOpenWorldRuntimeFact(ctx, tx, demandFact.id); err != nil {
		return err
	}
	if err = postCityOpenWorldRuntimeFact(ctx, tx, root.id); err != nil {
		return err
	}
	execution.events = append(execution.events, worldRuntimeAutomaticEvent{eventType: "city.open_world.enterprise_freight_demand_requested", payload: map[string]any{
		"source_code": sourceCode, "order_code": candidate.orderCode, "demand_code": demandCode,
	}})
	return nil
}

func createCityOpenWorldEnterpriseFreightSuppressedSource(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	sourceCode string,
	candidate cityOpenWorldEnterpriseFreightDispatchCandidate,
	lines []cityOpenWorldEnterpriseFreightLineCandidate,
	requestedUnits, mobilityDeadline int64,
	root *cityOpenWorldRuntimeFactRecord,
	execution *cityOpenWorldRuntimeAutomaticExecution,
) error {
	payload, err := json.Marshal(map[string]any{
		"schema_version": cityOpenWorldEnterpriseFreightSchemaVersion,
		"source_code":    sourceCode, "order_code": candidate.orderCode,
		"requested_units": requestedUnits, "maximum_requested_units": cityOpenWorldEnterpriseFreightMaximumRequestedUnits,
		"reason_code": cityOpenWorldEnterpriseFreightReasonUnitsExceeded,
	})
	if err != nil {
		return err
	}
	fact, err := insertCityOpenWorldRuntimeFact(ctx, tx, cityOpenWorldRuntimeFactInsert{
		worldID: worldID, tick: targetTick, sequence: execution.nextFactSeq,
		parentFactID: &root.id, factType: cityOpenWorldRuntimeFactEnterpriseFreightSourceSuppressed, payload: payload,
	})
	if err != nil {
		return err
	}
	execution.facts = append(execution.facts, fact.fact)
	execution.nextFactSeq++
	if err = insertCityOpenWorldEnterpriseFreightSource(ctx, tx, worldID, sourceCode,
		candidate, requestedUnits, targetTick, mobilityDeadline, cityOpenWorldEnterpriseFreightStateSuppressed,
		nil, nil, root, fact); err != nil {
		return err
	}
	if err = insertCityOpenWorldEnterpriseFreightSourceLines(ctx, tx, worldID, sourceCode, lines); err != nil {
		return err
	}
	if _, err = insertCityOpenWorldEnterpriseFreightFact(ctx, tx, worldID, sourceCode, "source.created", root, map[string]any{
		"schema_version": cityOpenWorldEnterpriseFreightSchemaVersion, "order_code": candidate.orderCode,
	}); err != nil {
		return err
	}
	freightFactID, err := insertCityOpenWorldEnterpriseFreightFact(ctx, tx, worldID, sourceCode, "source.suppressed", fact, map[string]any{
		"schema_version": cityOpenWorldEnterpriseFreightSchemaVersion,
		"reason_code":    cityOpenWorldEnterpriseFreightReasonUnitsExceeded,
	})
	if err != nil {
		return err
	}
	if err = insertCityOpenWorldEnterpriseFreightTransition(ctx, tx, worldID, sourceCode,
		cityOpenWorldEnterpriseFreightStateSuppressed, cityOpenWorldEnterpriseFreightReasonUnitsExceeded,
		freightFactID, fact, ""); err != nil {
		return err
	}
	if err = updateCityOpenWorldEnterpriseFreightPolicy(ctx, tx, worldID,
		1, 0, 0, 0, 0, 0, 0, 0, 1, 2, 1); err != nil {
		return err
	}
	if err = postCityOpenWorldRuntimeFact(ctx, tx, fact.id); err != nil {
		return err
	}
	if err = postCityOpenWorldRuntimeFact(ctx, tx, root.id); err != nil {
		return err
	}
	execution.events = append(execution.events, worldRuntimeAutomaticEvent{eventType: "city.open_world.enterprise_freight_suppressed", payload: map[string]any{
		"source_code": sourceCode, "order_code": candidate.orderCode,
	}})
	return nil
}

func loadCityOpenWorldEnterpriseFreightSourceRecords(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	states []string,
	limit int,
) ([]cityOpenWorldEnterpriseFreightSourceRecord, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT source.id, source.order_code, source.code, source.state,
       source.carrier_actor_id, carrier.code, source.mobility_demand_id,
       COALESCE(source.mobility_route_id, demand.route_id), source.last_runtime_fact_id, demand.status,
       route.status, current_state.state
FROM city_open_world_enterprise_freight_sources source
JOIN city_open_world_actors carrier
  ON carrier.id = source.carrier_actor_id AND carrier.world_id = source.world_id
JOIN LATERAL (
    SELECT transition.state
    FROM city_open_world_supply_chain_order_transitions transition
    WHERE transition.world_id = source.world_id AND transition.order_code = source.order_code
    ORDER BY transition.transition_tick DESC, transition.transition_sequence DESC
    LIMIT 1
) current_state ON TRUE
LEFT JOIN city_open_world_mobility_demands demand
  ON demand.id = source.mobility_demand_id AND demand.world_id = source.world_id
LEFT JOIN city_open_world_mobility_routes route
  ON route.id = COALESCE(source.mobility_route_id, demand.route_id) AND route.world_id = source.world_id
WHERE source.world_id = $1 AND source.state = ANY($2)
ORDER BY source.source_tick ASC, source.code ASC
LIMIT $3
FOR UPDATE OF source`, worldID, pq.Array(states), limit)
	if err != nil {
		return nil, fmt.Errorf("load V16 enterprise-freight sources: %w", err)
	}
	items := make([]cityOpenWorldEnterpriseFreightSourceRecord, 0)
	for rows.Next() {
		item := cityOpenWorldEnterpriseFreightSourceRecord{}
		if err = rows.Scan(&item.id, &item.orderCode, &item.code, &item.state,
			&item.carrierActorID, &item.carrierActorCode, &item.demandID, &item.routeID,
			&item.lastRuntimeFactID, &item.demandStatus, &item.routeStatus, &item.orderState); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan V16 enterprise-freight source: %w", err)
		}
		items = append(items, item)
	}
	if err = closeCityRows(rows, "iterate V16 enterprise-freight sources"); err != nil {
		return nil, err
	}
	return items, nil
}

func cityOpenWorldEnterpriseFreightOrderTerminal(state string) bool {
	switch state {
	case cityOpenWorldSupplyChainStateDelivered, cityOpenWorldSupplyChainStateCancelled,
		cityOpenWorldSupplyChainStateExpired, cityOpenWorldSupplyChainStateFailed:
		return true
	default:
		return false
	}
}

func reconcileCityOpenWorldEnterpriseFreightTerminalSources(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	policy *CityOpenWorldEnterpriseFreightPolicy,
	execution *cityOpenWorldRuntimeAutomaticExecution,
) error {
	items, err := loadCityOpenWorldEnterpriseFreightSourceRecords(ctx, tx, worldID,
		[]string{cityOpenWorldEnterpriseFreightStateDemandPending, cityOpenWorldEnterpriseFreightStateRouteScheduled, cityOpenWorldEnterpriseFreightStateRouteCompleted},
		policy.MaximumGenerationsPerTick)
	if err != nil {
		return err
	}
	for _, source := range items {
		if !cityOpenWorldEnterpriseFreightOrderTerminal(source.orderState) {
			continue
		}
		preserveCustody, preserveErr := cityOpenWorldFreightSettlementVoidedCustodySource(
			ctx, tx, worldID, cityOpenWorldFreightSettlementSourceShipment, source.code,
		)
		if preserveErr != nil {
			return preserveErr
		}
		if preserveCustody {
			continue
		}
		if source.demandStatus.Valid && source.demandStatus.String == "pending" &&
			(source.state == cityOpenWorldEnterpriseFreightStateDemandPending) {
			if err = voidCityOpenWorldEnterpriseFreightPendingDemand(ctx, tx, worldID, targetTick, source, execution); err != nil {
				return err
			}
			continue
		}
		if source.demandStatus.Valid && source.demandStatus.String == "expired" &&
			source.state == cityOpenWorldEnterpriseFreightStateDemandPending {
			if err = transitionCityOpenWorldEnterpriseFreightSource(ctx, tx, worldID, targetTick, source,
				cityOpenWorldEnterpriseFreightStateDemandExpired, cityOpenWorldEnterpriseFreightReasonExpired,
				cityOpenWorldRuntimeFactEnterpriseFreightDemandExpired, execution); err != nil {
				return err
			}
			continue
		}
		if err = transitionCityOpenWorldEnterpriseFreightSource(ctx, tx, worldID, targetTick, source,
			cityOpenWorldEnterpriseFreightStateTransportOrphaned, cityOpenWorldEnterpriseFreightReasonTerminalInTransit,
			cityOpenWorldRuntimeFactEnterpriseFreightTransportOrphaned, execution); err != nil {
			return err
		}
	}
	return nil
}

func observeCityOpenWorldEnterpriseFreightTransport(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	policy *CityOpenWorldEnterpriseFreightPolicy,
	execution *cityOpenWorldRuntimeAutomaticExecution,
) error {
	items, err := loadCityOpenWorldEnterpriseFreightSourceRecords(ctx, tx, worldID,
		[]string{cityOpenWorldEnterpriseFreightStateDemandPending, cityOpenWorldEnterpriseFreightStateRouteScheduled},
		policy.MaximumGenerationsPerTick)
	if err != nil {
		return err
	}
	for _, source := range items {
		var nextState, reasonCode, runtimeFactType string
		switch {
		case source.demandStatus.Valid && source.demandStatus.String == "expired" && source.state == cityOpenWorldEnterpriseFreightStateDemandPending:
			nextState, reasonCode, runtimeFactType = cityOpenWorldEnterpriseFreightStateDemandExpired, cityOpenWorldEnterpriseFreightReasonExpired, cityOpenWorldRuntimeFactEnterpriseFreightDemandExpired
		case source.demandStatus.Valid && source.demandStatus.String == "scheduled" && source.state == cityOpenWorldEnterpriseFreightStateDemandPending:
			nextState, reasonCode, runtimeFactType = cityOpenWorldEnterpriseFreightStateRouteScheduled, cityOpenWorldEnterpriseFreightReasonScheduled, cityOpenWorldRuntimeFactEnterpriseFreightRouteScheduled
		case source.demandStatus.Valid && source.demandStatus.String == "completed" && source.state == cityOpenWorldEnterpriseFreightStateDemandPending:
			nextState, reasonCode, runtimeFactType = cityOpenWorldEnterpriseFreightStateRouteCompleted, cityOpenWorldEnterpriseFreightReasonCompleted, cityOpenWorldRuntimeFactEnterpriseFreightRouteCompleted
		case source.demandStatus.Valid && source.demandStatus.String == "completed" && source.state == cityOpenWorldEnterpriseFreightStateRouteScheduled:
			nextState, reasonCode, runtimeFactType = cityOpenWorldEnterpriseFreightStateRouteCompleted, cityOpenWorldEnterpriseFreightReasonCompleted, cityOpenWorldRuntimeFactEnterpriseFreightRouteCompleted
		default:
			continue
		}
		if err = transitionCityOpenWorldEnterpriseFreightSource(ctx, tx, worldID, targetTick, source,
			nextState, reasonCode, runtimeFactType, execution); err != nil {
			return err
		}
	}
	return nil
}

func voidCityOpenWorldEnterpriseFreightPendingDemand(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	source cityOpenWorldEnterpriseFreightSourceRecord,
	execution *cityOpenWorldRuntimeAutomaticExecution,
) error {
	if !source.demandID.Valid || source.demandID.Int64 <= 0 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v16_enterprise_freight_pending_demand"})
	}
	payload, err := json.Marshal(map[string]any{
		"schema_version": cityOpenWorldEnterpriseFreightSchemaVersion,
		"source_code":    source.code, "demand_id": source.demandID.Int64,
		"reason_code":         cityOpenWorldEnterpriseFreightReasonTerminalPending,
		"location_projection": "unchanged",
	})
	if err != nil {
		return err
	}
	fact, err := insertCityOpenWorldRuntimeFact(ctx, tx, cityOpenWorldRuntimeFactInsert{
		worldID: worldID, tick: targetTick, sequence: execution.nextFactSeq,
		parentFactID: &source.lastRuntimeFactID, actorID: &source.carrierActorID,
		factType: CityOpenWorldRuntimeFactMobilityExpired, payload: payload,
	})
	if err != nil {
		return err
	}
	execution.facts = append(execution.facts, fact.fact)
	execution.nextFactSeq++
	if _, err = tx.ExecContext(ctx, `
UPDATE city_open_world_mobility_demands
SET status = 'expired', expired_tick = $3, last_fact_id = $4,
    version = version + 1, updated_at = NOW()
WHERE world_id = $1 AND id = $2 AND status = 'pending'`, worldID, source.demandID.Int64, targetTick, fact.id); err != nil {
		return fmt.Errorf("void V16 enterprise-freight demand: %w", err)
	}
	if _, err = updateCityOpenWorldMobilityActorMetric(ctx, tx, worldID, source.carrierActorID,
		source.carrierActorCode, 0, 0, 0, 1, nil, targetTick); err != nil {
		return err
	}
	if err = updateCityOpenWorldMobilityPolicy(ctx, tx, worldID, 0, 0, 0, 0, 1, 0); err != nil {
		return err
	}
	freightFactID, err := insertCityOpenWorldEnterpriseFreightFact(ctx, tx, worldID, source.code, "demand.voided", fact, map[string]any{
		"schema_version": cityOpenWorldEnterpriseFreightSchemaVersion,
		"reason_code":    cityOpenWorldEnterpriseFreightReasonTerminalPending,
	})
	if err != nil {
		return err
	}
	if err = updateCityOpenWorldEnterpriseFreightSourceState(ctx, tx, worldID, source,
		cityOpenWorldEnterpriseFreightStateVoided, fact.id, nil, freightFactID, fact,
		cityOpenWorldEnterpriseFreightReasonTerminalPending); err != nil {
		return err
	}
	if err = updateCityOpenWorldEnterpriseFreightPolicy(ctx, tx, worldID,
		0, -1, 0, 0, 0, 0, 1, 0, 0, 1, 1); err != nil {
		return err
	}
	if err = postCityOpenWorldRuntimeFact(ctx, tx, fact.id); err != nil {
		return err
	}
	execution.events = append(execution.events, worldRuntimeAutomaticEvent{eventType: "city.open_world.enterprise_freight_demand_voided", payload: map[string]any{
		"source_code": source.code, "order_code": source.orderCode,
	}})
	return nil
}

func transitionCityOpenWorldEnterpriseFreightSource(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	source cityOpenWorldEnterpriseFreightSourceRecord,
	nextState, reasonCode, runtimeFactType string,
	execution *cityOpenWorldRuntimeAutomaticExecution,
) error {
	payload, err := json.Marshal(map[string]any{
		"schema_version": cityOpenWorldEnterpriseFreightSchemaVersion,
		"source_code":    source.code, "order_code": source.orderCode,
		"previous_state": source.state, "state": nextState, "reason_code": reasonCode,
		"transport_contract": cityOpenWorldEnterpriseFreightCompletionContract,
	})
	if err != nil {
		return err
	}
	fact, err := insertCityOpenWorldRuntimeFact(ctx, tx, cityOpenWorldRuntimeFactInsert{
		worldID: worldID, tick: targetTick, sequence: execution.nextFactSeq,
		parentFactID: &source.lastRuntimeFactID, actorID: &source.carrierActorID,
		factType: runtimeFactType, payload: payload,
	})
	if err != nil {
		return err
	}
	execution.facts = append(execution.facts, fact.fact)
	execution.nextFactSeq++
	freightFactType := map[string]string{
		cityOpenWorldEnterpriseFreightStateRouteScheduled:    "route.scheduled",
		cityOpenWorldEnterpriseFreightStateRouteCompleted:    "route.completed",
		cityOpenWorldEnterpriseFreightStateDemandExpired:     "demand.expired",
		cityOpenWorldEnterpriseFreightStateTransportOrphaned: "transport.orphaned",
	}[nextState]
	if freightFactType == "" {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v16_enterprise_freight_transition_state"})
	}
	freightFactID, err := insertCityOpenWorldEnterpriseFreightFact(ctx, tx, worldID, source.code, freightFactType, fact, map[string]any{
		"schema_version": cityOpenWorldEnterpriseFreightSchemaVersion, "reason_code": reasonCode,
	})
	if err != nil {
		return err
	}
	var routeID *int64
	if nextState == cityOpenWorldEnterpriseFreightStateRouteScheduled || nextState == cityOpenWorldEnterpriseFreightStateRouteCompleted || nextState == cityOpenWorldEnterpriseFreightStateTransportOrphaned {
		if !source.routeID.Valid || source.routeID.Int64 <= 0 {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v16_enterprise_freight_route"})
		}
		routeID = &source.routeID.Int64
	}
	if err = updateCityOpenWorldEnterpriseFreightSourceState(ctx, tx, worldID, source, nextState,
		fact.id, routeID, freightFactID, fact, reasonCode); err != nil {
		return err
	}
	var pendingDelta, scheduledDelta, completedDelta, expiredDelta, orphanedDelta int64
	switch source.state {
	case cityOpenWorldEnterpriseFreightStateDemandPending:
		pendingDelta = -1
	case cityOpenWorldEnterpriseFreightStateRouteScheduled:
		scheduledDelta = -1
	case cityOpenWorldEnterpriseFreightStateRouteCompleted:
		completedDelta = -1
	}
	switch nextState {
	case cityOpenWorldEnterpriseFreightStateRouteScheduled:
		scheduledDelta++
	case cityOpenWorldEnterpriseFreightStateRouteCompleted:
		completedDelta++
	case cityOpenWorldEnterpriseFreightStateDemandExpired:
		expiredDelta++
	case cityOpenWorldEnterpriseFreightStateTransportOrphaned:
		orphanedDelta++
	}
	if err = updateCityOpenWorldEnterpriseFreightPolicy(ctx, tx, worldID,
		0, pendingDelta, 0, scheduledDelta, completedDelta, expiredDelta, 0, orphanedDelta, 0, 1, 1); err != nil {
		return err
	}
	if err = postCityOpenWorldRuntimeFact(ctx, tx, fact.id); err != nil {
		return err
	}
	eventType := "city.open_world.enterprise_freight." + nextState
	execution.events = append(execution.events, worldRuntimeAutomaticEvent{eventType: eventType, payload: map[string]any{
		"source_code": source.code, "order_code": source.orderCode, "reason_code": reasonCode,
	}})
	return nil
}

func updateCityOpenWorldEnterpriseFreightSourceState(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	source cityOpenWorldEnterpriseFreightSourceRecord,
	nextState string,
	lastFactID int64,
	routeID *int64,
	freightFactID int64,
	runtimeFact *cityOpenWorldRuntimeFactRecord,
	reasonCode string,
) error {
	if runtimeFact == nil || lastFactID != runtimeFact.id || !cityOpenWorldEnterpriseFreightTransitionAllowed(source.state, nextState) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v16_enterprise_freight_state"})
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_open_world_enterprise_freight_sources
SET state = $3,
    mobility_route_id = COALESCE($4, mobility_route_id),
    last_runtime_fact_id = $5,
    version = version + 1,
    updated_at = NOW()
WHERE world_id = $1 AND id = $2 AND state = $6`,
		worldID, source.id, nextState, cityOpenWorldNullableInt64(routeID), lastFactID, source.state)
	if err != nil {
		return fmt.Errorf("update V16 enterprise-freight source state: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v16_enterprise_freight_state"})
	}
	return insertCityOpenWorldEnterpriseFreightTransition(ctx, tx, worldID, source.code,
		nextState, reasonCode, freightFactID, runtimeFact, source.state)
}
