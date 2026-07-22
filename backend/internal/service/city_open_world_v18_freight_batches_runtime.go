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

type cityOpenWorldFreightBatchPolicyDelta struct {
	plans           int64
	consignments    int64
	awaitingRoute   int64
	inTransit       int64
	awaitingReceipt int64
	received        int64
	settled         int64
	expired         int64
	voided          int64
	orphaned        int64
	facts           int64
	transitions     int64
	receipts        int64
}

type cityOpenWorldFreightBatchPlanRecord struct {
	id                  int64
	code                string
	overflowSourceCode  string
	orderCode           string
	sellerNodeCode      string
	buyerNodeCode       string
	sourceHubCode       string
	destinationHubCode  string
	carrierActorID      int64
	carrierActorCode    string
	sourceTick          int64
	requiredUnits       int64
	consignmentCount    int
	state               string
	sourceRuntimeFactID int64
	lastRuntimeFactID   int64
	version             int64
}

type cityOpenWorldFreightBatchConsignmentRecord struct {
	id                  int64
	code                string
	planCode            string
	batchNo             int
	requestedUnits      int64
	state               string
	mobilityDemandID    int64
	mobilityRouteID     sql.NullInt64
	sourceRuntimeFactID int64
	lastRuntimeFactID   int64
	version             int64
	carrierActorID      int64
	carrierActorCode    string
	orderCode           string
}

type cityOpenWorldFreightBatchCandidate struct {
	overflowSourceCode  string
	orderCode           string
	sellerNodeCode      string
	buyerNodeCode       string
	sourceHubCode       string
	destinationHubCode  string
	carrierActorID      int64
	carrierActorCode    string
	sourceTick          int64
	requestedUnits      int64
	sourceRuntimeFactID int64
}

func loadCityOpenWorldFreightBatchPolicyForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
) (*CityOpenWorldFreightBatchPolicy, error) {
	policy := &CityOpenWorldFreightBatchPolicy{}
	err := tx.QueryRowContext(ctx, `
SELECT profile_id, profile_version, content_hash, baseline_tick,
       source_contract, packing_contract, transport_contract, receipt_contract,
       maximum_units, maximum_consignments_per_plan, maximum_plans_per_tick,
       maximum_observations_per_tick, plan_count, consignment_count,
       awaiting_route_count, in_transit_count, awaiting_receipt_count,
       received_count, settled_count, expired_count, voided_count, orphaned_count,
       fact_count, transition_count, receipt_count, revision, metadata
FROM city_open_world_freight_batch_profiles
WHERE world_id = $1
FOR UPDATE`, worldID).Scan(
		&policy.ProfileID, &policy.ProfileVersion, &policy.ContentHash, &policy.BaselineTick,
		&policy.SourceContract, &policy.PackingContract, &policy.TransportContract, &policy.ReceiptContract,
		&policy.MaximumUnits, &policy.MaximumConsignmentsPerPlan, &policy.MaximumPlansPerTick,
		&policy.MaximumObservationsPerTick, &policy.PlanCount, &policy.ConsignmentCount,
		&policy.AwaitingRouteCount, &policy.InTransitCount, &policy.AwaitingReceiptCount,
		&policy.ReceivedCount, &policy.SettledCount, &policy.ExpiredCount, &policy.VoidedCount, &policy.OrphanedCount,
		&policy.FactCount, &policy.TransitionCount, &policy.ReceiptCount, &policy.Revision, &policy.Metadata,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v18_freight_batch_profile"})
	}
	if err != nil {
		return nil, fmt.Errorf("lock V18 freight-batch profile: %w", err)
	}
	hash, hashErr := cityOpenWorldFreightBatchPolicyHash()
	if hashErr != nil || policy.ProfileID != cityOpenWorldFreightBatchProfileID ||
		policy.ProfileVersion != cityOpenWorldFreightBatchProfileVersion || policy.ContentHash != hash ||
		policy.BaselineTick < 0 || policy.SourceContract != cityOpenWorldFreightBatchSourceContract ||
		policy.PackingContract != cityOpenWorldFreightBatchPackingContract ||
		policy.TransportContract != cityOpenWorldFreightBatchTransportContract ||
		policy.ReceiptContract != cityOpenWorldFreightBatchReceiptContract ||
		policy.MaximumUnits != cityOpenWorldFreightBatchMaximumUnits ||
		policy.MaximumConsignmentsPerPlan != cityOpenWorldFreightBatchMaximumConsignmentsPerPlan ||
		policy.MaximumPlansPerTick != cityOpenWorldFreightBatchMaximumPlansPerTick ||
		policy.MaximumObservationsPerTick != cityOpenWorldFreightBatchMaximumObservationsPerTick ||
		policy.Revision < 1 || !cityOpenWorldFreightBatchPolicyMetadataValid(policy.Metadata) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v18_freight_batch_profile"})
	}
	return policy, nil
}

func updateCityOpenWorldFreightBatchPolicy(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	delta cityOpenWorldFreightBatchPolicyDelta,
) error {
	result, err := tx.ExecContext(ctx, `
UPDATE city_open_world_freight_batch_profiles
SET plan_count = plan_count + $2,
    consignment_count = consignment_count + $3,
    awaiting_route_count = awaiting_route_count + $4,
    in_transit_count = in_transit_count + $5,
    awaiting_receipt_count = awaiting_receipt_count + $6,
    received_count = received_count + $7,
    settled_count = settled_count + $8,
    expired_count = expired_count + $9,
    voided_count = voided_count + $10,
    orphaned_count = orphaned_count + $11,
    fact_count = fact_count + $12,
    transition_count = transition_count + $13,
    receipt_count = receipt_count + $14,
    revision = revision + 1,
    updated_at = NOW()
WHERE world_id = $1`, worldID, delta.plans, delta.consignments, delta.awaitingRoute,
		delta.inTransit, delta.awaitingReceipt, delta.received, delta.settled, delta.expired,
		delta.voided, delta.orphaned, delta.facts, delta.transitions, delta.receipts)
	if err != nil {
		return fmt.Errorf("update V18 freight-batch profile: %w", err)
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr != nil || rows != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v18_freight_batch_profile"})
	}
	return nil
}

// advanceCityOpenWorldV18FreightBatches transforms only V16 overflow source
// evidence. It deliberately runs after V16/V17 and before V9: a newly split
// consignment receives a pending demand at T and cannot enter capacity
// scheduling until a later tick.
func advanceCityOpenWorldV18FreightBatches(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence int64,
) (cityOpenWorldRuntimeAutomaticExecution, error) {
	execution := cityOpenWorldRuntimeAutomaticExecution{
		facts: make([]CityOpenWorldRuntimeFact, 0), effects: make([]CityOpenWorldRuntimeEffect, 0),
		events: make([]worldRuntimeAutomaticEvent, 0), nextFactSeq: factSequence,
	}
	policy, err := loadCityOpenWorldFreightBatchPolicyForUpdate(ctx, tx, worldID)
	if err != nil {
		return execution, err
	}
	if targetTick <= policy.BaselineTick {
		return execution, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v18_freight_batch_baseline"})
	}
	if err = activateCityOpenWorldRuntimeFactWrite(ctx, tx, worldID); err != nil {
		return execution, err
	}
	if err = activateCityOpenWorldFreightBatchWrite(ctx, tx, worldID); err != nil {
		return execution, err
	}
	if err = observeCityOpenWorldFreightBatchTransport(ctx, tx, worldID, targetTick, policy, &execution); err != nil {
		return execution, err
	}
	if err = reconcileCityOpenWorldFreightBatchTerminalPlans(ctx, tx, worldID, targetTick, policy, &execution); err != nil {
		return execution, err
	}
	if err = createCityOpenWorldFreightBatchPlans(ctx, tx, worldID, targetTick, policy, &execution); err != nil {
		return execution, err
	}
	if len(execution.facts) > 0 {
		if err = updateCityOpenWorldRuntimeProfile(ctx, tx, worldID, 0, int64(len(execution.facts)), 0, 0); err != nil {
			return execution, err
		}
	}
	if err = assertCityOpenWorldFreightBatchFoundation(ctx, tx, worldID); err != nil {
		return execution, err
	}
	return execution, nil
}

func loadCityOpenWorldFreightBatchCandidates(
	ctx context.Context,
	tx *sql.Tx,
	worldID, baselineTick int64,
	limit int,
) ([]cityOpenWorldFreightBatchCandidate, error) {
	if limit < 1 {
		return []cityOpenWorldFreightBatchCandidate{}, nil
	}
	rows, err := tx.QueryContext(ctx, `
SELECT source.code, source.order_code, source.seller_node_code, source.buyer_node_code,
       source.source_hub_code, source.destination_hub_code, source.carrier_actor_id,
       carrier.code, source.source_tick, source.requested_units, source.source_runtime_fact_id
FROM city_open_world_enterprise_freight_sources source
JOIN city_open_world_actors carrier
  ON carrier.id = source.carrier_actor_id AND carrier.world_id = source.world_id
LEFT JOIN city_open_world_freight_batch_plans plan
  ON plan.world_id = source.world_id AND plan.overflow_source_code = source.code
WHERE source.world_id = $1
  AND source.state = 'suppressed'
  AND source.source_tick > $2
  AND source.requested_units > $3
  AND plan.code IS NULL
ORDER BY source.source_tick, source.code
LIMIT $4
FOR UPDATE OF source`, worldID, baselineTick, cityOpenWorldFreightBatchMaximumUnits, limit)
	if err != nil {
		return nil, fmt.Errorf("load V18 freight-batch candidates: %w", err)
	}
	items := make([]cityOpenWorldFreightBatchCandidate, 0)
	for rows.Next() {
		item := cityOpenWorldFreightBatchCandidate{}
		if err = rows.Scan(&item.overflowSourceCode, &item.orderCode, &item.sellerNodeCode, &item.buyerNodeCode,
			&item.sourceHubCode, &item.destinationHubCode, &item.carrierActorID, &item.carrierActorCode,
			&item.sourceTick, &item.requestedUnits, &item.sourceRuntimeFactID); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan V18 freight-batch candidate: %w", err)
		}
		items = append(items, item)
	}
	if err = closeCityRows(rows, "iterate V18 freight-batch candidates"); err != nil {
		return nil, err
	}
	return items, nil
}

func loadCityOpenWorldFreightBatchSourceLines(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	sourceCode string,
) ([]cityOpenWorldFreightBatchSourceLine, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT line_no, resource_code, quantity_units, unit_price_units, total_price_units
FROM city_open_world_enterprise_freight_source_lines
WHERE world_id = $1 AND source_code = $2
ORDER BY line_no
FOR UPDATE`, worldID, sourceCode)
	if err != nil {
		return nil, fmt.Errorf("load V18 freight-batch source lines: %w", err)
	}
	items := make([]cityOpenWorldFreightBatchSourceLine, 0)
	for rows.Next() {
		item := cityOpenWorldFreightBatchSourceLine{}
		if err = rows.Scan(&item.LineNo, &item.ResourceCode, &item.QuantityUnits, &item.UnitPriceUnits, &item.TotalPriceUnits); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan V18 freight-batch source line: %w", err)
		}
		items = append(items, item)
	}
	if err = closeCityRows(rows, "iterate V18 freight-batch source lines"); err != nil {
		return nil, err
	}
	return items, nil
}

func createCityOpenWorldFreightBatchPlans(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	policy *CityOpenWorldFreightBatchPolicy,
	execution *cityOpenWorldRuntimeAutomaticExecution,
) error {
	if policy == nil || execution == nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v18_freight_batch_creation"})
	}
	remaining := policy.MaximumPlansPerTick
	if remaining < 1 {
		return nil
	}
	candidates, err := loadCityOpenWorldFreightBatchCandidates(ctx, tx, worldID, policy.BaselineTick, remaining)
	if err != nil {
		return err
	}
	for _, candidate := range candidates {
		lines, lineErr := loadCityOpenWorldFreightBatchSourceLines(ctx, tx, worldID, candidate.overflowSourceCode)
		if lineErr != nil {
			return lineErr
		}
		packed, packErr := cityOpenWorldFreightBatchPackLines(lines)
		if packErr != nil {
			return packErr
		}
		if len(packed) > policy.MaximumConsignmentsPerPlan || candidate.requestedUnits <= cityOpenWorldFreightBatchMaximumUnits {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v18_freight_batch_candidate"})
		}
		var total int64
		for _, batch := range packed {
			if batch.RequestedUnits <= 0 || batch.RequestedUnits > cityOpenWorldFreightBatchMaximumUnits || batch.RequestedUnits > math.MaxInt64-total {
				return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v18_freight_batch_quantity"})
			}
			total += batch.RequestedUnits
		}
		if total != candidate.requestedUnits {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v18_freight_batch_conservation"})
		}
		if err = createCityOpenWorldFreightBatchPlan(ctx, tx, worldID, targetTick, policy, candidate, packed, execution); err != nil {
			return err
		}
	}
	return nil
}

func createCityOpenWorldFreightBatchPlan(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	policy *CityOpenWorldFreightBatchPolicy,
	candidate cityOpenWorldFreightBatchCandidate,
	packed []cityOpenWorldFreightBatchPackedConsignment,
	execution *cityOpenWorldRuntimeAutomaticExecution,
) error {
	if policy == nil || execution == nil || candidate.overflowSourceCode == "" || candidate.orderCode == "" ||
		candidate.carrierActorID <= 0 || candidate.carrierActorCode != cityOpenWorldEnterpriseFreightCarrierActorCode ||
		candidate.sourceTick <= policy.BaselineTick || candidate.sourceTick > targetTick || candidate.requestedUnits <= cityOpenWorldFreightBatchMaximumUnits ||
		candidate.sourceRuntimeFactID <= 0 || len(packed) < 2 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v18_freight_batch_plan"})
	}
	planCode := cityOpenWorldFreightBatchPlanCode(candidate.overflowSourceCode)
	metadata, err := json.Marshal(map[string]any{
		"schema_version":       cityOpenWorldFreightBatchSchemaVersion,
		"source_contract":      cityOpenWorldFreightBatchSourceContract,
		"packing_contract":     cityOpenWorldFreightBatchPackingContract,
		"order_code":           candidate.orderCode,
		"overflow_source_code": candidate.overflowSourceCode,
	})
	if err != nil {
		return err
	}
	var planID int64
	if err = tx.QueryRowContext(ctx, `
INSERT INTO city_open_world_freight_batch_plans
    (world_id, code, overflow_source_code, order_code, seller_node_code, buyer_node_code,
     source_hub_code, destination_hub_code, carrier_actor_id, source_tick, required_units,
     consignment_count, state, source_runtime_fact_id, last_runtime_fact_id, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12,
        'active', $13, $13, 1, $14::jsonb)
RETURNING id`, worldID, planCode, candidate.overflowSourceCode, candidate.orderCode,
		candidate.sellerNodeCode, candidate.buyerNodeCode, candidate.sourceHubCode, candidate.destinationHubCode,
		candidate.carrierActorID, candidate.sourceTick, candidate.requestedUnits, len(packed), candidate.sourceRuntimeFactID,
		[]byte(metadata)).Scan(&planID); err != nil {
		return fmt.Errorf("insert V18 freight-batch plan: %w", err)
	}
	if planID <= 0 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v18_freight_batch_plan_id"})
	}
	if err = updateCityOpenWorldFreightBatchPolicy(ctx, tx, worldID, cityOpenWorldFreightBatchPolicyDelta{plans: 1}); err != nil {
		return err
	}
	for _, batch := range packed {
		if err = createCityOpenWorldFreightBatchConsignment(ctx, tx, worldID, targetTick, planCode, candidate, batch, execution); err != nil {
			return err
		}
	}
	return nil
}

func createCityOpenWorldFreightBatchConsignment(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	planCode string,
	candidate cityOpenWorldFreightBatchCandidate,
	batch cityOpenWorldFreightBatchPackedConsignment,
	execution *cityOpenWorldRuntimeAutomaticExecution,
) error {
	if execution == nil || planCode == "" || batch.BatchNo < 1 || batch.RequestedUnits <= 0 ||
		batch.RequestedUnits > cityOpenWorldFreightBatchMaximumUnits || len(batch.Lines) == 0 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v18_freight_batch_consignment"})
	}
	consignmentCode := cityOpenWorldFreightBatchConsignmentCode(planCode, batch.BatchNo)
	demandCode := cityOpenWorldFreightBatchDemandCode(consignmentCode)
	rootPayload, err := json.Marshal(map[string]any{
		"schema_version":       cityOpenWorldFreightBatchSchemaVersion,
		"plan_code":            planCode,
		"consignment_code":     consignmentCode,
		"batch_no":             batch.BatchNo,
		"order_code":           candidate.orderCode,
		"overflow_source_code": candidate.overflowSourceCode,
		"requested_units":      batch.RequestedUnits,
		"source_contract":      cityOpenWorldFreightBatchSourceContract,
	})
	if err != nil {
		return fmt.Errorf("marshal V18 freight-batch root fact: %w", err)
	}
	root, err := insertCityOpenWorldRuntimeFact(ctx, tx, cityOpenWorldRuntimeFactInsert{
		worldID: worldID, tick: targetTick, sequence: execution.nextFactSeq,
		parentFactID: &candidate.sourceRuntimeFactID, actorID: &candidate.carrierActorID,
		factType: cityOpenWorldRuntimeFactFreightBatchConsignmentCreated, payload: rootPayload,
	})
	if err != nil {
		return err
	}
	execution.facts = append(execution.facts, root.fact)
	execution.nextFactSeq++
	demandPayload, err := json.Marshal(map[string]any{
		"schema_version":   cityOpenWorldFreightBatchSchemaVersion,
		"plan_code":        planCode,
		"consignment_code": consignmentCode,
		"demand_code":      demandCode, "actor_code": candidate.carrierActorCode,
		"source_hub_code": candidate.sourceHubCode, "destination_hub_code": candidate.destinationHubCode,
		"mode_code": cityOpenWorldEnterpriseFreightModeCode, "purpose_code": "enterprise.freight_batch",
		"requested_units": batch.RequestedUnits, "earliest_departure_tick": targetTick + 1,
		"deadline_tick": targetTick + cityOpenWorldMobilityMaximumWaitTicks, "arrival_bridge": "excluded",
	})
	if err != nil {
		return fmt.Errorf("marshal V18 freight-batch demand fact: %w", err)
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
	demandMetadata, err := json.Marshal(map[string]any{
		"schema_version": cityOpenWorldMobilitySchemaVersion,
		"origin":         "enterprise_freight_batch_v18",
		"transport_adapter": map[string]any{
			"kind": "enterprise_freight_batch_v1", "plan_code": planCode,
			"consignment_code": consignmentCode, "order_code": candidate.orderCode,
			"arrival_bridge": "excluded", "transport_contract": cityOpenWorldFreightBatchTransportContract,
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
		candidate.destinationHubCode, cityOpenWorldEnterpriseFreightModeCode, "enterprise.freight_batch",
		batch.RequestedUnits, targetTick, targetTick+1, targetTick+cityOpenWorldMobilityMaximumWaitTicks,
		demandFact.id, []byte(demandMetadata)).Scan(&demandID); err != nil {
		return fmt.Errorf("insert V18 freight-batch mobility demand: %w", err)
	}
	metricCreated, err := updateCityOpenWorldMobilityActorMetric(ctx, tx, worldID,
		candidate.carrierActorID, candidate.carrierActorCode, 1, 0, 0, 0, nil, targetTick)
	if err != nil {
		return err
	}
	if err = updateCityOpenWorldMobilityPolicy(ctx, tx, worldID, 1, 0, 0, 0, 0, metricCreated); err != nil {
		return err
	}
	metadata, err := json.Marshal(map[string]any{
		"schema_version": cityOpenWorldFreightBatchSchemaVersion,
		"plan_code":      planCode, "batch_no": batch.BatchNo, "demand_code": demandCode,
	})
	if err != nil {
		return err
	}
	var consignmentID int64
	if err = tx.QueryRowContext(ctx, `
INSERT INTO city_open_world_freight_batch_consignments
    (world_id, code, plan_code, batch_no, requested_units, state,
     mobility_demand_id, mobility_route_id, source_runtime_fact_id,
     last_runtime_fact_id, version, metadata)
VALUES ($1, $2, $3, $4, $5, 'awaiting_route', $6, NULL, $7, $8, 1, $9::jsonb)
RETURNING id`, worldID, consignmentCode, planCode, batch.BatchNo, batch.RequestedUnits,
		demandID, root.id, demandFact.id, []byte(metadata)).Scan(&consignmentID); err != nil {
		return fmt.Errorf("insert V18 freight-batch consignment: %w", err)
	}
	if consignmentID <= 0 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v18_freight_batch_consignment_id"})
	}
	for _, line := range batch.Lines {
		lineMetadata, metadataErr := json.Marshal(map[string]any{
			"schema_version":   cityOpenWorldFreightBatchSchemaVersion,
			"packing_contract": cityOpenWorldFreightBatchPackingContract,
		})
		if metadataErr != nil {
			return metadataErr
		}
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_freight_batch_lines
    (world_id, consignment_code, source_line_no, resource_code, quantity_units,
     unit_price_units, total_price_units, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb)`, worldID, consignmentCode,
			line.SourceLineNo, line.ResourceCode, line.QuantityUnits, line.UnitPriceUnits,
			line.TotalPriceUnits, []byte(lineMetadata)); err != nil {
			return fmt.Errorf("insert V18 freight-batch line %s/%d: %w", consignmentCode, line.SourceLineNo, err)
		}
	}
	if _, err = insertCityOpenWorldFreightBatchRuntimeFact(ctx, tx, worldID, consignmentCode, root,
		"consignment.created", map[string]any{"schema_version": cityOpenWorldFreightBatchSchemaVersion, "plan_code": planCode}); err != nil {
		return err
	}
	factID, err := insertCityOpenWorldFreightBatchRuntimeFact(ctx, tx, worldID, consignmentCode, demandFact,
		"demand.requested", map[string]any{"schema_version": cityOpenWorldFreightBatchSchemaVersion, "demand_code": demandCode})
	if err != nil {
		return err
	}
	if err = insertCityOpenWorldFreightBatchTransition(ctx, tx, worldID, consignmentCode,
		cityOpenWorldFreightBatchConsignmentStateAwaitingRoute, cityOpenWorldFreightBatchReasonDispatched,
		factID, CityOpenWorldFreightBatchFactRef{EvidenceKind: cityOpenWorldFreightBatchEvidenceRuntime, Tick: demandFact.fact.Tick, Sequence: demandFact.fact.Sequence}); err != nil {
		return err
	}
	if err = updateCityOpenWorldFreightBatchPolicy(ctx, tx, worldID, cityOpenWorldFreightBatchPolicyDelta{
		consignments: 1, awaitingRoute: 1, facts: 2, transitions: 1,
	}); err != nil {
		return err
	}
	if err = postCityOpenWorldRuntimeFact(ctx, tx, demandFact.id); err != nil {
		return err
	}
	if err = postCityOpenWorldRuntimeFact(ctx, tx, root.id); err != nil {
		return err
	}
	execution.events = append(execution.events, worldRuntimeAutomaticEvent{eventType: "city.open_world.freight_batch_demand_requested", payload: map[string]any{
		"plan_code": planCode, "consignment_code": consignmentCode, "demand_code": demandCode,
	}})
	return nil
}

func insertCityOpenWorldFreightBatchRuntimeFact(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	consignmentCode string,
	runtimeFact *cityOpenWorldRuntimeFactRecord,
	factType string,
	payload map[string]any,
) (int64, error) {
	if runtimeFact == nil || runtimeFact.id <= 0 || runtimeFact.fact.Tick <= 0 || runtimeFact.fact.Sequence <= 0 ||
		!cityOpenWorldFreightBatchFactTypeValid(factType) || payload == nil {
		return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v18_freight_batch_runtime_fact"})
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	var id int64
	if err = tx.QueryRowContext(ctx, `
INSERT INTO city_open_world_freight_batch_facts
    (world_id, consignment_code, tick, sequence, fact_type, evidence_kind,
     runtime_fact_id, supply_chain_fact_id, payload)
VALUES ($1, $2, $3, $4, $5, 'runtime', $6, NULL, $7::jsonb)
RETURNING id`, worldID, consignmentCode, runtimeFact.fact.Tick, runtimeFact.fact.Sequence,
		factType, runtimeFact.id, []byte(raw)).Scan(&id); err != nil {
		return 0, fmt.Errorf("insert V18 freight-batch runtime fact %s/%s: %w", consignmentCode, factType, err)
	}
	return id, nil
}

func insertCityOpenWorldFreightBatchSupplyFact(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	consignmentCode string,
	supplyFact *cityOpenWorldSupplyChainFactRecord,
	factType string,
	payload map[string]any,
) (int64, error) {
	if supplyFact == nil || supplyFact.id <= 0 || supplyFact.fact.Tick <= 0 || supplyFact.fact.Sequence <= 0 ||
		!cityOpenWorldFreightBatchFactTypeValid(factType) || payload == nil {
		return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v18_freight_batch_supply_fact"})
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	var id int64
	if err = tx.QueryRowContext(ctx, `
INSERT INTO city_open_world_freight_batch_facts
    (world_id, consignment_code, tick, sequence, fact_type, evidence_kind,
     runtime_fact_id, supply_chain_fact_id, payload)
VALUES ($1, $2, $3, $4, $5, 'supply_chain', NULL, $6, $7::jsonb)
RETURNING id`, worldID, consignmentCode, supplyFact.fact.Tick, supplyFact.fact.Sequence,
		factType, supplyFact.id, []byte(raw)).Scan(&id); err != nil {
		return 0, fmt.Errorf("insert V18 freight-batch supply fact %s/%s: %w", consignmentCode, factType, err)
	}
	return id, nil
}

func insertCityOpenWorldFreightBatchTransition(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	consignmentCode, state, reasonCode string,
	factID int64,
	reference CityOpenWorldFreightBatchFactRef,
) error {
	if factID <= 0 || !cityOpenWorldFreightBatchConsignmentStateValid(state) ||
		!cityOpenWorldFreightBatchReasonMatchesState(state, reasonCode) || reference.Tick <= 0 || reference.Sequence <= 0 ||
		!cityOpenWorldFreightBatchEvidenceKindValid(reference.EvidenceKind) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v18_freight_batch_transition"})
	}
	metadata, err := json.Marshal(map[string]any{"schema_version": cityOpenWorldFreightBatchSchemaVersion})
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_freight_batch_transitions
    (world_id, consignment_code, transition_tick, transition_sequence, state,
     reason_code, source_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb)`, worldID, consignmentCode,
		reference.Tick, reference.Sequence, state, reasonCode, factID, []byte(metadata)); err != nil {
		return fmt.Errorf("insert V18 freight-batch transition %s/%s: %w", consignmentCode, state, err)
	}
	return nil
}

func loadCityOpenWorldFreightBatchPlanForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	planCode string,
) (*cityOpenWorldFreightBatchPlanRecord, error) {
	item := &cityOpenWorldFreightBatchPlanRecord{}
	err := tx.QueryRowContext(ctx, `
SELECT plan.id, plan.code, plan.overflow_source_code, plan.order_code,
       plan.seller_node_code, plan.buyer_node_code, plan.source_hub_code,
       plan.destination_hub_code, plan.carrier_actor_id, carrier.code,
       plan.source_tick, plan.required_units, plan.consignment_count, plan.state,
       plan.source_runtime_fact_id, plan.last_runtime_fact_id, plan.version
FROM city_open_world_freight_batch_plans plan
JOIN city_open_world_actors carrier
  ON carrier.id = plan.carrier_actor_id AND carrier.world_id = plan.world_id
WHERE plan.world_id = $1 AND plan.code = $2
FOR UPDATE`, worldID, planCode).Scan(
		&item.id, &item.code, &item.overflowSourceCode, &item.orderCode, &item.sellerNodeCode,
		&item.buyerNodeCode, &item.sourceHubCode, &item.destinationHubCode, &item.carrierActorID,
		&item.carrierActorCode, &item.sourceTick, &item.requiredUnits, &item.consignmentCount,
		&item.state, &item.sourceRuntimeFactID, &item.lastRuntimeFactID, &item.version,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lock V18 freight-batch plan: %w", err)
	}
	return item, nil
}

func loadCityOpenWorldFreightBatchConsignmentsForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	planCode string,
	states []string,
	limit int,
) ([]cityOpenWorldFreightBatchConsignmentRecord, error) {
	query := `
SELECT consignment.id, consignment.code, consignment.plan_code, consignment.batch_no,
       consignment.requested_units, consignment.state, consignment.mobility_demand_id,
       consignment.mobility_route_id, consignment.source_runtime_fact_id,
       consignment.last_runtime_fact_id, consignment.version, plan.carrier_actor_id,
       carrier.code, plan.order_code
FROM city_open_world_freight_batch_consignments consignment
JOIN city_open_world_freight_batch_plans plan
  ON plan.world_id = consignment.world_id AND plan.code = consignment.plan_code
JOIN city_open_world_actors carrier
  ON carrier.id = plan.carrier_actor_id AND carrier.world_id = plan.world_id
WHERE consignment.world_id = $1 AND consignment.plan_code = $2`
	args := []any{worldID, planCode}
	if len(states) > 0 {
		query += ` AND consignment.state = ANY($3)`
		args = append(args, pq.Array(states))
	}
	query += ` ORDER BY consignment.batch_no`
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}
	query += ` FOR UPDATE OF consignment`
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("load V18 freight-batch consignments: %w", err)
	}
	items := make([]cityOpenWorldFreightBatchConsignmentRecord, 0)
	for rows.Next() {
		item := cityOpenWorldFreightBatchConsignmentRecord{}
		if err = rows.Scan(&item.id, &item.code, &item.planCode, &item.batchNo, &item.requestedUnits,
			&item.state, &item.mobilityDemandID, &item.mobilityRouteID, &item.sourceRuntimeFactID,
			&item.lastRuntimeFactID, &item.version, &item.carrierActorID, &item.carrierActorCode, &item.orderCode); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan V18 freight-batch consignment: %w", err)
		}
		items = append(items, item)
	}
	if err = closeCityRows(rows, "iterate V18 freight-batch consignments"); err != nil {
		return nil, err
	}
	return items, nil
}

func loadCityOpenWorldFreightBatchActiveConsignmentIDs(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	states []string,
	limit int,
) ([]int64, error) {
	if len(states) == 0 || limit < 1 {
		return []int64{}, nil
	}
	rows, err := tx.QueryContext(ctx, `
SELECT id
FROM city_open_world_freight_batch_consignments
WHERE world_id = $1 AND state = ANY($2)
ORDER BY plan_code, batch_no
LIMIT $3
FOR UPDATE`, worldID, pq.Array(states), limit)
	if err != nil {
		return nil, fmt.Errorf("load V18 active freight-batch consignment IDs: %w", err)
	}
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err = closeCityRows(rows, "iterate V18 active freight-batch consignment IDs"); err != nil {
		return nil, err
	}
	return ids, nil
}

func loadCityOpenWorldFreightBatchConsignmentByIDForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	worldID, consignmentID int64,
) (*cityOpenWorldFreightBatchConsignmentRecord, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT consignment.id, consignment.code, consignment.plan_code, consignment.batch_no,
       consignment.requested_units, consignment.state, consignment.mobility_demand_id,
       consignment.mobility_route_id, consignment.source_runtime_fact_id,
       consignment.last_runtime_fact_id, consignment.version, plan.carrier_actor_id,
       carrier.code, plan.order_code
FROM city_open_world_freight_batch_consignments consignment
JOIN city_open_world_freight_batch_plans plan
  ON plan.world_id = consignment.world_id AND plan.code = consignment.plan_code
JOIN city_open_world_actors carrier
  ON carrier.id = plan.carrier_actor_id AND carrier.world_id = plan.world_id
WHERE consignment.world_id = $1 AND consignment.id = $2
FOR UPDATE OF consignment`, worldID, consignmentID)
	if err != nil {
		return nil, fmt.Errorf("lock V18 freight-batch consignment: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err = rows.Err(); err != nil {
			return nil, err
		}
		return nil, nil
	}
	item := &cityOpenWorldFreightBatchConsignmentRecord{}
	if err = rows.Scan(&item.id, &item.code, &item.planCode, &item.batchNo, &item.requestedUnits,
		&item.state, &item.mobilityDemandID, &item.mobilityRouteID, &item.sourceRuntimeFactID,
		&item.lastRuntimeFactID, &item.version, &item.carrierActorID, &item.carrierActorCode, &item.orderCode); err != nil {
		return nil, err
	}
	return item, nil
}

func observeCityOpenWorldFreightBatchTransport(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	policy *CityOpenWorldFreightBatchPolicy,
	execution *cityOpenWorldRuntimeAutomaticExecution,
) error {
	if policy == nil || execution == nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v18_freight_batch_observe"})
	}
	ids, err := loadCityOpenWorldFreightBatchActiveConsignmentIDs(ctx, tx, worldID,
		[]string{cityOpenWorldFreightBatchConsignmentStateAwaitingRoute, cityOpenWorldFreightBatchConsignmentStateInTransit},
		policy.MaximumObservationsPerTick)
	if err != nil {
		return err
	}
	for _, id := range ids {
		consignment, loadErr := loadCityOpenWorldFreightBatchConsignmentByIDForUpdate(ctx, tx, worldID, id)
		if loadErr != nil {
			return loadErr
		}
		if consignment == nil {
			continue
		}
		var demandStatus string
		var routeID sql.NullInt64
		if err = tx.QueryRowContext(ctx, `
SELECT demand.status, demand.route_id
FROM city_open_world_mobility_demands demand
WHERE demand.world_id = $1 AND demand.id = $2`, worldID, consignment.mobilityDemandID).Scan(&demandStatus, &routeID); err != nil {
			return fmt.Errorf("load V18 freight-batch mobility demand: %w", err)
		}
		var nextState, reasonCode, runtimeFactType string
		switch {
		case demandStatus == "expired" && consignment.state == cityOpenWorldFreightBatchConsignmentStateAwaitingRoute:
			nextState, reasonCode, runtimeFactType = cityOpenWorldFreightBatchConsignmentStateExpired,
				cityOpenWorldFreightBatchReasonExpired, cityOpenWorldRuntimeFactFreightBatchDemandExpired
		case demandStatus == "scheduled" && consignment.state == cityOpenWorldFreightBatchConsignmentStateAwaitingRoute:
			nextState, reasonCode, runtimeFactType = cityOpenWorldFreightBatchConsignmentStateInTransit,
				cityOpenWorldFreightBatchReasonScheduled, cityOpenWorldRuntimeFactFreightBatchRouteScheduled
		case demandStatus == "completed" && (consignment.state == cityOpenWorldFreightBatchConsignmentStateAwaitingRoute || consignment.state == cityOpenWorldFreightBatchConsignmentStateInTransit):
			nextState, reasonCode, runtimeFactType = cityOpenWorldFreightBatchConsignmentStateAwaitingReceipt,
				cityOpenWorldFreightBatchReasonCompleted, cityOpenWorldRuntimeFactFreightBatchRouteCompleted
		default:
			continue
		}
		var route *int64
		if routeID.Valid {
			value := routeID.Int64
			route = &value
		}
		if nextState == cityOpenWorldFreightBatchConsignmentStateInTransit || nextState == cityOpenWorldFreightBatchConsignmentStateAwaitingReceipt {
			if route == nil || *route <= 0 {
				return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v18_freight_batch_route"})
			}
		}
		if err = transitionCityOpenWorldFreightBatchConsignment(ctx, tx, worldID, targetTick, consignment,
			nextState, reasonCode, runtimeFactType, route, execution); err != nil {
			return err
		}
	}
	return nil
}

func cityOpenWorldFreightBatchOrderTerminal(state string) bool {
	return cityOpenWorldEnterpriseFreightOrderTerminal(state)
}

func reconcileCityOpenWorldFreightBatchTerminalPlans(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	policy *CityOpenWorldFreightBatchPolicy,
	execution *cityOpenWorldRuntimeAutomaticExecution,
) error {
	if policy == nil || execution == nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v18_freight_batch_terminal"})
	}
	ids, err := loadCityOpenWorldFreightBatchActiveConsignmentIDs(ctx, tx, worldID,
		[]string{cityOpenWorldFreightBatchConsignmentStateAwaitingRoute, cityOpenWorldFreightBatchConsignmentStateInTransit, cityOpenWorldFreightBatchConsignmentStateAwaitingReceipt},
		policy.MaximumObservationsPerTick)
	if err != nil {
		return err
	}
	for _, id := range ids {
		consignment, loadErr := loadCityOpenWorldFreightBatchConsignmentByIDForUpdate(ctx, tx, worldID, id)
		if loadErr != nil {
			return loadErr
		}
		if consignment == nil {
			continue
		}
		var orderState string
		if err = tx.QueryRowContext(ctx, `
SELECT transition.state
FROM city_open_world_supply_chain_order_transitions transition
WHERE transition.world_id = $1 AND transition.order_code = $2
ORDER BY transition.transition_tick DESC, transition.transition_sequence DESC
LIMIT 1`, worldID, consignment.orderCode).Scan(&orderState); err != nil {
			return fmt.Errorf("load V18 freight-batch order state: %w", err)
		}
		if !cityOpenWorldFreightBatchOrderTerminal(orderState) {
			continue
		}
		preserveCustody, preserveErr := cityOpenWorldFreightSettlementVoidedCustodySource(
			ctx, tx, worldID, cityOpenWorldFreightSettlementSourceConsignment, consignment.code,
		)
		if preserveErr != nil {
			return preserveErr
		}
		if preserveCustody {
			continue
		}
		var demandStatus string
		if err = tx.QueryRowContext(ctx, `
SELECT status FROM city_open_world_mobility_demands
WHERE world_id = $1 AND id = $2`, worldID, consignment.mobilityDemandID).Scan(&demandStatus); err != nil {
			return err
		}
		if consignment.state == cityOpenWorldFreightBatchConsignmentStateAwaitingRoute && demandStatus == "pending" {
			if err = voidCityOpenWorldFreightBatchPendingDemand(ctx, tx, worldID, targetTick, consignment, execution); err != nil {
				return err
			}
			continue
		}
		if consignment.state == cityOpenWorldFreightBatchConsignmentStateAwaitingRoute && demandStatus == "expired" {
			if err = transitionCityOpenWorldFreightBatchConsignment(ctx, tx, worldID, targetTick, consignment,
				cityOpenWorldFreightBatchConsignmentStateExpired, cityOpenWorldFreightBatchReasonExpired,
				cityOpenWorldRuntimeFactFreightBatchDemandExpired, nil, execution); err != nil {
				return err
			}
			continue
		}
		if err = transitionCityOpenWorldFreightBatchConsignment(ctx, tx, worldID, targetTick, consignment,
			cityOpenWorldFreightBatchConsignmentStateOrphaned, cityOpenWorldFreightBatchReasonOrphaned,
			cityOpenWorldRuntimeFactFreightBatchTransportOrphaned, nullInt64Pointer(consignment.mobilityRouteID), execution); err != nil {
			return err
		}
	}
	return nil
}

func voidCityOpenWorldFreightBatchPendingDemand(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	consignment *cityOpenWorldFreightBatchConsignmentRecord,
	execution *cityOpenWorldRuntimeAutomaticExecution,
) error {
	if consignment == nil || consignment.mobilityDemandID <= 0 || execution == nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v18_freight_batch_pending"})
	}
	payload, err := json.Marshal(map[string]any{
		"schema_version":   cityOpenWorldFreightBatchSchemaVersion,
		"consignment_code": consignment.code, "demand_id": consignment.mobilityDemandID,
		"reason_code": cityOpenWorldFreightBatchReasonVoided, "location_projection": "unchanged",
	})
	if err != nil {
		return err
	}
	fact, err := insertCityOpenWorldRuntimeFact(ctx, tx, cityOpenWorldRuntimeFactInsert{
		worldID: worldID, tick: targetTick, sequence: execution.nextFactSeq,
		parentFactID: &consignment.lastRuntimeFactID, actorID: &consignment.carrierActorID,
		factType: CityOpenWorldRuntimeFactMobilityExpired, payload: payload,
	})
	if err != nil {
		return err
	}
	execution.facts = append(execution.facts, fact.fact)
	execution.nextFactSeq++
	result, err := tx.ExecContext(ctx, `
UPDATE city_open_world_mobility_demands
SET status = 'expired', expired_tick = $3, last_fact_id = $4, version = version + 1, updated_at = NOW()
WHERE world_id = $1 AND id = $2 AND status = 'pending'`, worldID, consignment.mobilityDemandID, targetTick, fact.id)
	if err != nil {
		return fmt.Errorf("void V18 freight-batch mobility demand: %w", err)
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr != nil || rows != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v18_freight_batch_pending_demand"})
	}
	if _, err = updateCityOpenWorldMobilityActorMetric(ctx, tx, worldID, consignment.carrierActorID,
		consignment.carrierActorCode, 0, 0, 0, 1, nil, targetTick); err != nil {
		return err
	}
	if err = updateCityOpenWorldMobilityPolicy(ctx, tx, worldID, 0, 0, 0, 0, 1, 0); err != nil {
		return err
	}
	batchFactID, err := insertCityOpenWorldFreightBatchRuntimeFact(ctx, tx, worldID, consignment.code, fact,
		"demand.voided", map[string]any{"schema_version": cityOpenWorldFreightBatchSchemaVersion, "reason_code": cityOpenWorldFreightBatchReasonVoided})
	if err != nil {
		return err
	}
	if err = updateCityOpenWorldFreightBatchConsignmentState(ctx, tx, worldID, consignment,
		cityOpenWorldFreightBatchConsignmentStateVoided, fact.id, nil, batchFactID, fact,
		cityOpenWorldFreightBatchReasonVoided); err != nil {
		return err
	}
	if err = updateCityOpenWorldFreightBatchPolicy(ctx, tx, worldID, cityOpenWorldFreightBatchStateTransitionDelta(
		cityOpenWorldFreightBatchConsignmentStateAwaitingRoute, cityOpenWorldFreightBatchConsignmentStateVoided, 1, 1, 0)); err != nil {
		return err
	}
	if err = refreshCityOpenWorldFreightBatchPlan(ctx, tx, worldID, consignment.planCode, fact.id); err != nil {
		return err
	}
	if err = postCityOpenWorldRuntimeFact(ctx, tx, fact.id); err != nil {
		return err
	}
	execution.events = append(execution.events, worldRuntimeAutomaticEvent{eventType: "city.open_world.freight_batch_demand_voided", payload: map[string]any{
		"consignment_code": consignment.code, "plan_code": consignment.planCode,
	}})
	return nil
}

func transitionCityOpenWorldFreightBatchConsignment(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	consignment *cityOpenWorldFreightBatchConsignmentRecord,
	nextState, reasonCode, runtimeFactType string,
	routeID *int64,
	execution *cityOpenWorldRuntimeAutomaticExecution,
) error {
	if consignment == nil || execution == nil || !cityOpenWorldFreightBatchTransitionAllowed(consignment.state, nextState) ||
		!cityOpenWorldFreightBatchReasonMatchesState(nextState, reasonCode) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v18_freight_batch_transition"})
	}
	payload, err := json.Marshal(map[string]any{
		"schema_version":   cityOpenWorldFreightBatchSchemaVersion,
		"consignment_code": consignment.code, "plan_code": consignment.planCode,
		"order_code": consignment.orderCode, "next_state": nextState, "reason_code": reasonCode,
		"transport_contract": cityOpenWorldFreightBatchTransportContract,
	})
	if err != nil {
		return err
	}
	fact, err := insertCityOpenWorldRuntimeFact(ctx, tx, cityOpenWorldRuntimeFactInsert{
		worldID: worldID, tick: targetTick, sequence: execution.nextFactSeq,
		parentFactID: &consignment.lastRuntimeFactID, actorID: &consignment.carrierActorID,
		factType: runtimeFactType, payload: payload,
	})
	if err != nil {
		return err
	}
	execution.facts = append(execution.facts, fact.fact)
	execution.nextFactSeq++
	factType := map[string]string{
		cityOpenWorldFreightBatchConsignmentStateInTransit:       "route.scheduled",
		cityOpenWorldFreightBatchConsignmentStateAwaitingReceipt: "route.completed",
		cityOpenWorldFreightBatchConsignmentStateExpired:         "demand.expired",
		cityOpenWorldFreightBatchConsignmentStateOrphaned:        "transport.orphaned",
	}[nextState]
	if factType == "" {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v18_freight_batch_transition_state"})
	}
	previousState := consignment.state
	batchFactID, err := insertCityOpenWorldFreightBatchRuntimeFact(ctx, tx, worldID, consignment.code, fact,
		factType, map[string]any{"schema_version": cityOpenWorldFreightBatchSchemaVersion, "reason_code": reasonCode})
	if err != nil {
		return err
	}
	if err = updateCityOpenWorldFreightBatchConsignmentState(ctx, tx, worldID, consignment,
		nextState, fact.id, routeID, batchFactID, fact, reasonCode); err != nil {
		return err
	}
	if err = updateCityOpenWorldFreightBatchPolicy(ctx, tx, worldID,
		cityOpenWorldFreightBatchStateTransitionDelta(previousState, nextState, 1, 1, 0)); err != nil {
		return err
	}
	if err = refreshCityOpenWorldFreightBatchPlan(ctx, tx, worldID, consignment.planCode, fact.id); err != nil {
		return err
	}
	if err = postCityOpenWorldRuntimeFact(ctx, tx, fact.id); err != nil {
		return err
	}
	execution.events = append(execution.events, worldRuntimeAutomaticEvent{eventType: "city.open_world.freight_batch." + nextState, payload: map[string]any{
		"consignment_code": consignment.code, "plan_code": consignment.planCode,
	}})
	return nil
}

func updateCityOpenWorldFreightBatchConsignmentState(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	consignment *cityOpenWorldFreightBatchConsignmentRecord,
	nextState string,
	lastRuntimeFactID int64,
	routeID *int64,
	batchFactID int64,
	runtimeFact *cityOpenWorldRuntimeFactRecord,
	reasonCode string,
) error {
	if consignment == nil || runtimeFact == nil || batchFactID <= 0 || lastRuntimeFactID != runtimeFact.id ||
		!cityOpenWorldFreightBatchTransitionAllowed(consignment.state, nextState) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v18_freight_batch_state"})
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_open_world_freight_batch_consignments
SET state = $3, mobility_route_id = COALESCE($4, mobility_route_id),
    last_runtime_fact_id = $5, version = version + 1, updated_at = NOW()
WHERE world_id = $1 AND id = $2 AND state = $6`, worldID, consignment.id, nextState,
		cityOpenWorldNullableInt64(routeID), lastRuntimeFactID, consignment.state)
	if err != nil {
		return fmt.Errorf("update V18 freight-batch consignment state: %w", err)
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr != nil || rows != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v18_freight_batch_state"})
	}
	if err = insertCityOpenWorldFreightBatchTransition(ctx, tx, worldID, consignment.code, nextState, reasonCode,
		batchFactID, CityOpenWorldFreightBatchFactRef{EvidenceKind: cityOpenWorldFreightBatchEvidenceRuntime, Tick: runtimeFact.fact.Tick, Sequence: runtimeFact.fact.Sequence}); err != nil {
		return err
	}
	consignment.state = nextState
	consignment.lastRuntimeFactID = lastRuntimeFactID
	if routeID != nil {
		consignment.mobilityRouteID = sql.NullInt64{Int64: *routeID, Valid: true}
	}
	consignment.version++
	return nil
}

func cityOpenWorldFreightBatchStateTransitionDelta(previous, next string, facts, transitions, receipts int64) cityOpenWorldFreightBatchPolicyDelta {
	delta := cityOpenWorldFreightBatchPolicyDelta{facts: facts, transitions: transitions, receipts: receipts}
	apply := func(state string, change int64) {
		switch state {
		case cityOpenWorldFreightBatchConsignmentStateAwaitingRoute:
			delta.awaitingRoute += change
		case cityOpenWorldFreightBatchConsignmentStateInTransit:
			delta.inTransit += change
		case cityOpenWorldFreightBatchConsignmentStateAwaitingReceipt:
			delta.awaitingReceipt += change
		case cityOpenWorldFreightBatchConsignmentStateReceived:
			delta.received += change
		case cityOpenWorldFreightBatchConsignmentStateSettled:
			delta.settled += change
		case cityOpenWorldFreightBatchConsignmentStateExpired:
			delta.expired += change
		case cityOpenWorldFreightBatchConsignmentStateVoided:
			delta.voided += change
		case cityOpenWorldFreightBatchConsignmentStateOrphaned:
			delta.orphaned += change
		}
	}
	apply(previous, -1)
	apply(next, 1)
	return delta
}

func refreshCityOpenWorldFreightBatchPlan(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	planCode string,
	lastRuntimeFactID int64,
) error {
	plan, err := loadCityOpenWorldFreightBatchPlanForUpdate(ctx, tx, worldID, planCode)
	if err != nil {
		return err
	}
	if plan == nil || lastRuntimeFactID <= 0 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v18_freight_batch_plan_refresh"})
	}
	items, err := loadCityOpenWorldFreightBatchConsignmentsForUpdate(ctx, tx, worldID, planCode, nil, 0)
	if err != nil {
		return err
	}
	if len(items) != plan.consignmentCount {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v18_freight_batch_plan_count"})
	}
	state := cityOpenWorldFreightBatchPlanStateFromRecords(items)
	if _, err = tx.ExecContext(ctx, `
UPDATE city_open_world_freight_batch_plans
SET state = $3, last_runtime_fact_id = $4, version = version + 1, updated_at = NOW()
WHERE world_id = $1 AND id = $2`, worldID, plan.id, state, lastRuntimeFactID); err != nil {
		return fmt.Errorf("refresh V18 freight-batch plan: %w", err)
	}
	return nil
}

func cityOpenWorldFreightBatchPlanStateFromRecords(items []cityOpenWorldFreightBatchConsignmentRecord) string {
	if len(items) == 0 {
		return cityOpenWorldFreightBatchPlanStateBlocked
	}
	all := func(state string) bool {
		for _, item := range items {
			if item.state != state {
				return false
			}
		}
		return true
	}
	if all(cityOpenWorldFreightBatchConsignmentStateReceived) {
		return cityOpenWorldFreightBatchPlanStateReceived
	}
	if all(cityOpenWorldFreightBatchConsignmentStateSettled) {
		return cityOpenWorldFreightBatchPlanStateSettled
	}
	if all(cityOpenWorldFreightBatchConsignmentStateAwaitingReceipt) {
		return cityOpenWorldFreightBatchPlanStateReady
	}
	for _, item := range items {
		if item.state == cityOpenWorldFreightBatchConsignmentStateExpired || item.state == cityOpenWorldFreightBatchConsignmentStateVoided || item.state == cityOpenWorldFreightBatchConsignmentStateOrphaned {
			return cityOpenWorldFreightBatchPlanStateBlocked
		}
	}
	return cityOpenWorldFreightBatchPlanStateActive
}

// assertCityOpenWorldFreightBatchReady is a companion to the V17 receipt
// gate. Only a V18 post-baseline overflow source owns this gate; legacy V17
// orders and ordinary V16 sources intentionally return nil here.
func assertCityOpenWorldFreightBatchReady(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	orderCode string,
) (*cityOpenWorldFreightBatchPlanRecord, error) {
	var simulationVersion string
	if err := tx.QueryRowContext(ctx, `SELECT simulation_version FROM city_worlds WHERE id = $1`, worldID).Scan(&simulationVersion); err != nil {
		return nil, fmt.Errorf("load V18 freight-batch world version: %w", err)
	}
	if !cityEngineSupportsOpenWorldFreightBatches(simulationVersion) {
		return nil, nil
	}
	policy, err := loadCityOpenWorldFreightBatchPolicyForUpdate(ctx, tx, worldID)
	if err != nil {
		return nil, err
	}
	var sourceCode string
	var sourceTick int64
	err = tx.QueryRowContext(ctx, `
SELECT code, source_tick
FROM city_open_world_enterprise_freight_sources
WHERE world_id = $1 AND order_code = $2 AND state = 'suppressed'
ORDER BY source_tick DESC
LIMIT 1
FOR UPDATE`, worldID, orderCode).Scan(&sourceCode, &sourceTick)
	if errors.Is(err, sql.ErrNoRows) || sourceTick <= policy.BaselineTick {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load V18 freight-batch overflow source: %w", err)
	}
	plan, err := loadCityOpenWorldFreightBatchPlanForUpdate(ctx, tx, worldID, cityOpenWorldFreightBatchPlanCode(sourceCode))
	if err != nil {
		return nil, err
	}
	if plan == nil || plan.overflowSourceCode != sourceCode || plan.orderCode != orderCode || plan.state != cityOpenWorldFreightBatchPlanStateReady {
		return nil, cityOpenWorldSupplyChainReject(cityOpenWorldSupplyChainRejectionReceiptNotReady)
	}
	consignments, err := loadCityOpenWorldFreightBatchConsignmentsForUpdate(ctx, tx, worldID, plan.code, nil, 0)
	if err != nil {
		return nil, err
	}
	if len(consignments) != plan.consignmentCount || len(consignments) == 0 {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v18_freight_batch_ready_count"})
	}
	for _, consignment := range consignments {
		if consignment.state != cityOpenWorldFreightBatchConsignmentStateAwaitingReceipt {
			return nil, cityOpenWorldSupplyChainReject(cityOpenWorldSupplyChainRejectionReceiptNotReady)
		}
	}
	return plan, nil
}

func recordCityOpenWorldFreightBatchReceipts(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	plan *cityOpenWorldFreightBatchPlanRecord,
	deliveryID int64,
	supplyFact *cityOpenWorldSupplyChainFactRecord,
	operation *CityResourceOperation,
) error {
	if plan == nil {
		return nil
	}
	if deliveryID <= 0 || supplyFact == nil || supplyFact.id <= 0 || operation == nil || operation.ID <= 0 ||
		plan.state != cityOpenWorldFreightBatchPlanStateReady {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v18_freight_batch_receipt"})
	}
	if err := activateCityOpenWorldFreightBatchWrite(ctx, tx, worldID); err != nil {
		return err
	}
	consignments, err := loadCityOpenWorldFreightBatchConsignmentsForUpdate(ctx, tx, worldID, plan.code, nil, 0)
	if err != nil {
		return err
	}
	if len(consignments) != plan.consignmentCount || len(consignments) == 0 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v18_freight_batch_receipt_count"})
	}
	for index := range consignments {
		consignment := &consignments[index]
		if consignment.state != cityOpenWorldFreightBatchConsignmentStateAwaitingReceipt {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v18_freight_batch_receipt_state"})
		}
		factID, factErr := insertCityOpenWorldFreightBatchSupplyFact(ctx, tx, worldID, consignment.code, supplyFact,
			"receipt.confirmed", map[string]any{
				"schema_version": cityOpenWorldFreightBatchSchemaVersion,
				"plan_code":      plan.code, "order_code": plan.orderCode,
				"resource_operation_tick": operation.Tick, "resource_operation_sequence": operation.Sequence,
			})
		if factErr != nil {
			return factErr
		}
		reference := CityOpenWorldFreightBatchFactRef{EvidenceKind: cityOpenWorldFreightBatchEvidenceSupplyChain, Tick: supplyFact.fact.Tick, Sequence: supplyFact.fact.Sequence}
		if err = insertCityOpenWorldFreightBatchTransition(ctx, tx, worldID, consignment.code,
			cityOpenWorldFreightBatchConsignmentStateReceived, cityOpenWorldFreightBatchReasonReceived, factID, reference); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `
UPDATE city_open_world_freight_batch_consignments
SET state = 'received', version = version + 1, updated_at = NOW()
WHERE world_id = $1 AND id = $2 AND state = 'awaiting_receipt'`, worldID, consignment.id); err != nil {
			return fmt.Errorf("update V18 freight-batch receipt state: %w", err)
		}
		metadata, metadataErr := json.Marshal(map[string]any{
			"schema_version":   cityOpenWorldFreightBatchSchemaVersion,
			"receipt_contract": cityOpenWorldFreightBatchReceiptContract,
			"delivery_fact":    map[string]any{"tick": supplyFact.fact.Tick, "sequence": supplyFact.fact.Sequence},
		})
		if metadataErr != nil {
			return metadataErr
		}
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_freight_batch_receipts
    (world_id, consignment_code, plan_code, order_code, received_tick,
     supply_chain_delivery_id, resource_operation_id, source_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb)`, worldID, consignment.code,
			plan.code, plan.orderCode, supplyFact.fact.Tick, deliveryID, operation.ID, factID, []byte(metadata)); err != nil {
			return fmt.Errorf("insert V18 freight-batch receipt %s: %w", consignment.code, err)
		}
		if err = updateCityOpenWorldFreightBatchPolicy(ctx, tx, worldID,
			cityOpenWorldFreightBatchStateTransitionDelta(cityOpenWorldFreightBatchConsignmentStateAwaitingReceipt,
				cityOpenWorldFreightBatchConsignmentStateReceived, 1, 1, 1)); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `
UPDATE city_open_world_freight_batch_plans
SET state = 'received', version = version + 1, updated_at = NOW()
WHERE world_id = $1 AND id = $2 AND state = 'ready'`, worldID, plan.id); err != nil {
		return fmt.Errorf("update V18 freight-batch received plan: %w", err)
	}
	return assertCityOpenWorldFreightBatchFoundation(ctx, tx, worldID)
}
