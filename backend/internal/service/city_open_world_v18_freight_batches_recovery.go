package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// V18 recovery restores only the batch adapter that joins V16 overflow
// evidence to V9 transport and V15's atomic delivery. It deliberately does
// not recreate a delivery, inventory transfer, or a mobility route: each is
// restored by its owning projection before this adapter is reattached.
type cityOpenWorldFreightBatchRecoveryFactKey struct {
	consignmentCode string
	evidenceKind    string
	tick            int64
	sequence        int64
}

func cityOpenWorldFreightBatchRecoveryKey(
	consignmentCode string,
	reference CityOpenWorldFreightBatchFactRef,
) cityOpenWorldFreightBatchRecoveryFactKey {
	return cityOpenWorldFreightBatchRecoveryFactKey{
		consignmentCode: consignmentCode,
		evidenceKind:    reference.EvidenceKind,
		tick:            reference.Tick,
		sequence:        reference.Sequence,
	}
}

func cityOpenWorldFreightBatchRuntimeReference(reference CityOpenWorldFreightBatchFactRef) CityOpenWorldRuntimeFactRef {
	return CityOpenWorldRuntimeFactRef{Tick: reference.Tick, Sequence: reference.Sequence}
}

func requireCityOpenWorldFreightBatchRecoveryFactID(
	factIDs map[cityOpenWorldFreightBatchRecoveryFactKey]int64,
	consignmentCode string,
	reference CityOpenWorldFreightBatchFactRef,
) (int64, error) {
	id, found := factIDs[cityOpenWorldFreightBatchRecoveryKey(consignmentCode, reference)]
	if !found || id <= 0 {
		return 0, fmt.Errorf("V18 freight-batch fact %s/%s/%d/%d is unavailable", consignmentCode, reference.EvidenceKind, reference.Tick, reference.Sequence)
	}
	return id, nil
}

func loadCityOpenWorldFreightBatchRecoverySupplyFactID(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	orderCode string,
	reference CityOpenWorldFreightBatchFactRef,
	expectedFactType string,
) (int64, error) {
	if expectedFactType != "order.delivered" && expectedFactType != "order.settled" {
		return 0, fmt.Errorf("invalid V18 supply evidence type %q", expectedFactType)
	}
	var id int64
	err := tx.QueryRowContext(ctx, `
SELECT id
FROM city_open_world_supply_chain_facts
WHERE world_id = $1 AND order_code = $2 AND tick = $3 AND sequence = $4
	  AND fact_type = $5`,
		worldID, orderCode, reference.Tick, reference.Sequence, expectedFactType,
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("V18 freight-batch supply evidence %s/%d/%d is unavailable", orderCode, reference.Tick, reference.Sequence)
	}
	if err != nil {
		return 0, fmt.Errorf("resolve V18 freight-batch supply evidence %s/%d/%d: %w", orderCode, reference.Tick, reference.Sequence, err)
	}
	return id, nil
}

func restoreCityOpenWorldFreightBatchProjection(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	batchState CityOpenWorldFreightBatchState,
) (int, error) {
	if err := validateCityOpenWorldFreightBatchState(&batchState); err != nil {
		return 0, fmt.Errorf("validate V18 freight-batch recovery input: %w", err)
	}
	count := 0
	policy := batchState.Policy
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_freight_batch_profiles
    (world_id, profile_id, profile_version, content_hash, baseline_tick,
     source_contract, packing_contract, transport_contract, receipt_contract,
     maximum_units, maximum_consignments_per_plan, maximum_plans_per_tick,
     maximum_observations_per_tick, plan_count, consignment_count,
     awaiting_route_count, in_transit_count, awaiting_receipt_count,
     received_count, settled_count, expired_count, voided_count, orphaned_count,
     fact_count, transition_count, receipt_count, revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
        $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28::jsonb)`,
		worldID, policy.ProfileID, policy.ProfileVersion, policy.ContentHash, policy.BaselineTick,
		policy.SourceContract, policy.PackingContract, policy.TransportContract, policy.ReceiptContract,
		policy.MaximumUnits, policy.MaximumConsignmentsPerPlan, policy.MaximumPlansPerTick,
		policy.MaximumObservationsPerTick, policy.PlanCount, policy.ConsignmentCount,
		policy.AwaitingRouteCount, policy.InTransitCount, policy.AwaitingReceiptCount,
		policy.ReceivedCount, policy.SettledCount, policy.ExpiredCount, policy.VoidedCount, policy.OrphanedCount,
		policy.FactCount, policy.TransitionCount, policy.ReceiptCount, policy.Revision, []byte(policy.Metadata)); err != nil {
		return count, fmt.Errorf("restore V18 freight-batch profile: %w", err)
	}
	count++

	plans := make(map[string]CityOpenWorldFreightBatchPlan, len(batchState.Plans))
	for _, plan := range batchState.Plans {
		carrierID, carrierErr := loadCityOpenWorldEnterpriseFreightRecoveryCarrierID(ctx, tx, worldID, plan.CarrierActorCode)
		if carrierErr != nil {
			return count, fmt.Errorf("restore V18 freight-batch plan %s carrier: %w", plan.Code, carrierErr)
		}
		sourceFactID, sourceErr := loadCityOpenWorldEnterpriseFreightRecoveryRuntimeFactID(
			ctx, tx, worldID, cityOpenWorldFreightBatchRuntimeReference(plan.SourceFact),
		)
		if sourceErr != nil {
			return count, fmt.Errorf("restore V18 freight-batch plan %s source fact: %w", plan.Code, sourceErr)
		}
		lastFactID, lastErr := loadCityOpenWorldEnterpriseFreightRecoveryRuntimeFactID(
			ctx, tx, worldID, cityOpenWorldFreightBatchRuntimeReference(plan.LastFact),
		)
		if lastErr != nil {
			return count, fmt.Errorf("restore V18 freight-batch plan %s last fact: %w", plan.Code, lastErr)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_freight_batch_plans
    (world_id, code, overflow_source_code, order_code, seller_node_code,
     buyer_node_code, source_hub_code, destination_hub_code, carrier_actor_id,
     source_tick, required_units, consignment_count, state,
     source_runtime_fact_id, last_runtime_fact_id, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
        $14, $15, $16, $17::jsonb)`,
			worldID, plan.Code, plan.OverflowSourceCode, plan.OrderCode, plan.SellerNodeCode,
			plan.BuyerNodeCode, plan.SourceHubCode, plan.DestinationHubCode, carrierID,
			plan.SourceTick, plan.RequiredUnits, plan.ConsignmentCount, plan.State,
			sourceFactID, lastFactID, plan.Version, []byte(plan.Metadata)); err != nil {
			return count, fmt.Errorf("restore V18 freight-batch plan %s: %w", plan.Code, err)
		}
		plans[plan.Code] = plan
		count++
	}

	for _, consignment := range batchState.Consignments {
		if _, found := plans[consignment.PlanCode]; !found {
			return count, fmt.Errorf("V18 freight-batch consignment %s references an unavailable plan", consignment.Code)
		}
		demandCode := consignment.DemandCode
		demandID, demandErr := loadCityOpenWorldEnterpriseFreightRecoveryDemandID(ctx, tx, worldID, &demandCode)
		if demandErr != nil || demandID == nil {
			if demandErr == nil {
				demandErr = errors.New("missing mobility demand")
			}
			return count, fmt.Errorf("restore V18 freight-batch consignment %s demand: %w", consignment.Code, demandErr)
		}
		routeID, routeErr := loadCityOpenWorldEnterpriseFreightRecoveryRouteID(ctx, tx, worldID, consignment.RouteCode)
		if routeErr != nil {
			return count, fmt.Errorf("restore V18 freight-batch consignment %s route: %w", consignment.Code, routeErr)
		}
		sourceFactID, sourceErr := loadCityOpenWorldEnterpriseFreightRecoveryRuntimeFactID(
			ctx, tx, worldID, cityOpenWorldFreightBatchRuntimeReference(consignment.SourceFact),
		)
		if sourceErr != nil {
			return count, fmt.Errorf("restore V18 freight-batch consignment %s source fact: %w", consignment.Code, sourceErr)
		}
		lastFactID, lastErr := loadCityOpenWorldEnterpriseFreightRecoveryRuntimeFactID(
			ctx, tx, worldID, cityOpenWorldFreightBatchRuntimeReference(consignment.LastFact),
		)
		if lastErr != nil {
			return count, fmt.Errorf("restore V18 freight-batch consignment %s last fact: %w", consignment.Code, lastErr)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_freight_batch_consignments
    (world_id, code, plan_code, batch_no, requested_units, state,
     mobility_demand_id, mobility_route_id, source_runtime_fact_id,
     last_runtime_fact_id, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb)`,
			worldID, consignment.Code, consignment.PlanCode, consignment.BatchNo,
			consignment.RequestedUnits, consignment.State, demandID, routeID,
			sourceFactID, lastFactID, consignment.Version, []byte(consignment.Metadata)); err != nil {
			return count, fmt.Errorf("restore V18 freight-batch consignment %s: %w", consignment.Code, err)
		}
		count++
	}

	for _, line := range batchState.Lines {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_freight_batch_lines
    (world_id, consignment_code, source_line_no, resource_code,
     quantity_units, unit_price_units, total_price_units, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb)`,
			worldID, line.ConsignmentCode, line.SourceLineNo, line.ResourceCode,
			line.QuantityUnits, line.UnitPriceUnits, line.TotalPriceUnits, []byte(line.Metadata)); err != nil {
			return count, fmt.Errorf("restore V18 freight-batch line %s/%d: %w", line.ConsignmentCode, line.SourceLineNo, err)
		}
		count++
	}

	factIDs := make(map[cityOpenWorldFreightBatchRecoveryFactKey]int64, len(batchState.Facts))
	for _, fact := range batchState.Facts {
		var runtimeFactID, supplyFactID any
		var evidenceErr error
		if fact.EvidenceKind == cityOpenWorldFreightBatchEvidenceRuntime {
			runtimeFactID, evidenceErr = loadCityOpenWorldEnterpriseFreightRecoveryRuntimeFactID(
				ctx, tx, worldID, CityOpenWorldRuntimeFactRef{Tick: fact.Tick, Sequence: fact.Sequence},
			)
		} else {
			consignment, found := findCityOpenWorldFreightBatchConsignment(batchState.Consignments, fact.ConsignmentCode)
			if !found {
				return count, fmt.Errorf("V18 freight-batch fact %s has no consignment", fact.ConsignmentCode)
			}
			plan, found := plans[consignment.PlanCode]
			if !found {
				return count, fmt.Errorf("V18 freight-batch fact %s has no plan", fact.ConsignmentCode)
			}
			expectedFactType := ""
			switch fact.FactType {
			case "receipt.confirmed":
				expectedFactType = "order.delivered"
			case "settlement.confirmed":
				expectedFactType = "order.settled"
			default:
				return count, fmt.Errorf("V18 freight-batch fact %s has invalid supply fact type %q", fact.ConsignmentCode, fact.FactType)
			}
			supplyFactID, evidenceErr = loadCityOpenWorldFreightBatchRecoverySupplyFactID(
				ctx, tx, worldID, plan.OrderCode,
				CityOpenWorldFreightBatchFactRef{EvidenceKind: fact.EvidenceKind, Tick: fact.Tick, Sequence: fact.Sequence}, expectedFactType,
			)
		}
		if evidenceErr != nil {
			return count, fmt.Errorf("restore V18 freight-batch fact %s/%s/%d/%d: %w", fact.ConsignmentCode, fact.EvidenceKind, fact.Tick, fact.Sequence, evidenceErr)
		}
		var factID int64
		if err := tx.QueryRowContext(ctx, `
INSERT INTO city_open_world_freight_batch_facts
    (world_id, consignment_code, tick, sequence, fact_type, evidence_kind,
     runtime_fact_id, supply_chain_fact_id, payload)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb)
RETURNING id`,
			worldID, fact.ConsignmentCode, fact.Tick, fact.Sequence, fact.FactType,
			fact.EvidenceKind, runtimeFactID, supplyFactID, []byte(fact.Payload)).Scan(&factID); err != nil {
			return count, fmt.Errorf("restore V18 freight-batch fact %s/%s/%d/%d: %w", fact.ConsignmentCode, fact.EvidenceKind, fact.Tick, fact.Sequence, err)
		}
		factIDs[cityOpenWorldFreightBatchRecoveryKey(fact.ConsignmentCode,
			CityOpenWorldFreightBatchFactRef{EvidenceKind: fact.EvidenceKind, Tick: fact.Tick, Sequence: fact.Sequence})] = factID
		count++
	}

	for _, transition := range batchState.Transitions {
		sourceFactID, sourceErr := requireCityOpenWorldFreightBatchRecoveryFactID(
			factIDs, transition.ConsignmentCode, transition.SourceFact,
		)
		if sourceErr != nil {
			return count, sourceErr
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_freight_batch_transitions
    (world_id, consignment_code, transition_tick, transition_sequence,
     state, reason_code, source_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb)`,
			worldID, transition.ConsignmentCode, transition.TransitionTick, transition.TransitionSequence,
			transition.State, transition.ReasonCode, sourceFactID, []byte(transition.Metadata)); err != nil {
			return count, fmt.Errorf("restore V18 freight-batch transition %s/%d/%d: %w", transition.ConsignmentCode, transition.TransitionTick, transition.TransitionSequence, err)
		}
		count++
	}

	for _, receipt := range batchState.Receipts {
		deliveryID, deliveryErr := loadCityOpenWorldEnterpriseFreightReceiptRecoveryDeliveryID(
			ctx, tx, worldID, receipt.OrderCode, receipt.DeliveryFact,
		)
		if deliveryErr != nil {
			return count, deliveryErr
		}
		operationID, operationErr := loadCityOpenWorldSupplyChainRecoveryResourceOperationID(
			ctx, tx, worldID, receipt.ResourceOperation,
		)
		if operationErr != nil {
			return count, operationErr
		}
		sourceFactID, sourceErr := requireCityOpenWorldFreightBatchRecoveryFactID(
			factIDs, receipt.ConsignmentCode, receipt.SourceFact,
		)
		if sourceErr != nil {
			return count, sourceErr
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_freight_batch_receipts
    (world_id, consignment_code, plan_code, order_code, received_tick,
     supply_chain_delivery_id, resource_operation_id, source_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb)`,
			worldID, receipt.ConsignmentCode, receipt.PlanCode, receipt.OrderCode, receipt.ReceivedTick,
			deliveryID, operationID, sourceFactID, []byte(receipt.Metadata)); err != nil {
			return count, fmt.Errorf("restore V18 freight-batch receipt %s: %w", receipt.ConsignmentCode, err)
		}
		count++
	}
	if err := assertCityOpenWorldFreightBatchFoundation(ctx, tx, worldID); err != nil {
		return count, fmt.Errorf("validate restored V18 freight-batch foundation: %w", err)
	}
	return count, nil
}

func findCityOpenWorldFreightBatchConsignment(
	consignments []CityOpenWorldFreightBatchConsignment,
	code string,
) (CityOpenWorldFreightBatchConsignment, bool) {
	for _, consignment := range consignments {
		if consignment.Code == code {
			return consignment, true
		}
	}
	return CityOpenWorldFreightBatchConsignment{}, false
}
