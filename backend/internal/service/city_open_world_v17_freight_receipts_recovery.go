package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// V17 recovery reconnects custody evidence to durable V16 and V15 facts. It
// never recreates freight, inventory, delivery, or resource-operation events:
// all of those are restored by their owning projections before this adapter.
type cityOpenWorldEnterpriseFreightReceiptRecoveryFactKey struct {
	shipmentCode string
	evidenceKind string
	tick         int64
	sequence     int64
}

func cityOpenWorldEnterpriseFreightReceiptRecoveryKey(
	shipmentCode string,
	reference CityOpenWorldEnterpriseFreightReceiptFactRef,
) cityOpenWorldEnterpriseFreightReceiptRecoveryFactKey {
	return cityOpenWorldEnterpriseFreightReceiptRecoveryFactKey{
		shipmentCode: shipmentCode,
		evidenceKind: reference.EvidenceKind,
		tick:         reference.Tick,
		sequence:     reference.Sequence,
	}
}

func loadCityOpenWorldEnterpriseFreightReceiptRecoveryFreightFactID(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	sourceCode string,
	reference CityOpenWorldEnterpriseFreightReceiptFactRef,
) (int64, error) {
	var id int64
	err := tx.QueryRowContext(ctx, `
SELECT id
FROM city_open_world_enterprise_freight_facts
WHERE world_id = $1 AND source_code = $2 AND tick = $3 AND sequence = $4`,
		worldID, sourceCode, reference.Tick, reference.Sequence,
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("V17 freight evidence %s/%d/%d is unavailable", sourceCode, reference.Tick, reference.Sequence)
	}
	if err != nil {
		return 0, fmt.Errorf("resolve V17 freight evidence %s/%d/%d: %w", sourceCode, reference.Tick, reference.Sequence, err)
	}
	return id, nil
}

func loadCityOpenWorldEnterpriseFreightReceiptRecoverySupplyFactID(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	orderCode string,
	reference CityOpenWorldEnterpriseFreightReceiptFactRef,
	expectedFactType string,
) (int64, error) {
	if expectedFactType != "order.delivered" && expectedFactType != "order.settled" {
		return 0, fmt.Errorf("invalid V17 supply evidence type %q", expectedFactType)
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
		return 0, fmt.Errorf("V17 supply receipt evidence %s/%d/%d is unavailable", orderCode, reference.Tick, reference.Sequence)
	}
	if err != nil {
		return 0, fmt.Errorf("resolve V17 supply receipt evidence %s/%d/%d: %w", orderCode, reference.Tick, reference.Sequence, err)
	}
	return id, nil
}

func requireCityOpenWorldEnterpriseFreightReceiptRecoveryFactID(
	factIDs map[cityOpenWorldEnterpriseFreightReceiptRecoveryFactKey]int64,
	shipmentCode string,
	reference CityOpenWorldEnterpriseFreightReceiptFactRef,
) (int64, error) {
	id, found := factIDs[cityOpenWorldEnterpriseFreightReceiptRecoveryKey(shipmentCode, reference)]
	if !found || id <= 0 {
		return 0, fmt.Errorf("V17 receipt fact %s/%s/%d/%d is unavailable", shipmentCode, reference.EvidenceKind, reference.Tick, reference.Sequence)
	}
	return id, nil
}

func loadCityOpenWorldEnterpriseFreightReceiptRecoveryDeliveryID(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	orderCode string,
	deliveryFact CityOpenWorldRuntimeFactRef,
) (int64, error) {
	var id int64
	err := tx.QueryRowContext(ctx, `
SELECT delivery.id
FROM city_open_world_supply_chain_deliveries delivery
JOIN city_open_world_supply_chain_facts fact
  ON fact.id = delivery.source_fact_id AND fact.world_id = delivery.world_id
WHERE delivery.world_id = $1 AND delivery.order_code = $2
  AND delivery.delivered_tick = $3 AND fact.tick = $3 AND fact.sequence = $4
  AND fact.fact_type = 'order.delivered'`,
		worldID, orderCode, deliveryFact.Tick, deliveryFact.Sequence,
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("V17 supply delivery %s/%d/%d is unavailable", orderCode, deliveryFact.Tick, deliveryFact.Sequence)
	}
	if err != nil {
		return 0, fmt.Errorf("resolve V17 supply delivery %s/%d/%d: %w", orderCode, deliveryFact.Tick, deliveryFact.Sequence, err)
	}
	return id, nil
}

func restoreCityOpenWorldEnterpriseFreightReceiptProjection(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	receiptState CityOpenWorldEnterpriseFreightReceiptState,
) (int, error) {
	if err := validateCityOpenWorldEnterpriseFreightReceiptState(&receiptState); err != nil {
		return 0, fmt.Errorf("validate V17 freight-receipt recovery input: %w", err)
	}
	count := 0
	policy := receiptState.Policy
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_enterprise_freight_receipt_profiles
    (world_id, profile_id, profile_version, content_hash, baseline_tick,
     shipment_contract, receipt_contract, legacy_contract,
     maximum_shipments, maximum_observations_per_tick,
     shipment_count, awaiting_route_count, in_transit_count, awaiting_receipt_count,
     received_count, settled_count, expired_count, voided_count, orphaned_count, fact_count,
     transition_count, receipt_count, revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
        $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24::jsonb)`,
		worldID, policy.ProfileID, policy.ProfileVersion, policy.ContentHash, policy.BaselineTick,
		policy.ShipmentContract, policy.ReceiptContract, policy.LegacyContract,
		policy.MaximumShipments, policy.MaximumObservationsPerTick,
		policy.ShipmentCount, policy.AwaitingRouteCount, policy.InTransitCount,
		policy.AwaitingReceiptCount, policy.ReceivedCount, policy.SettledCount, policy.ExpiredCount,
		policy.VoidedCount, policy.OrphanedCount, policy.FactCount, policy.TransitionCount,
		policy.ReceiptCount, policy.Revision, []byte(policy.Metadata)); err != nil {
		return count, fmt.Errorf("restore V17 freight-receipt profile: %w", err)
	}
	count++

	for _, shipment := range receiptState.Shipments {
		rootFactID, rootErr := loadCityOpenWorldEnterpriseFreightReceiptRecoveryFreightFactID(
			ctx, tx, worldID, shipment.FreightSourceCode, shipment.SourceEvidence,
		)
		if rootErr != nil {
			return count, rootErr
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_enterprise_freight_shipments
    (world_id, code, freight_source_code, order_code, seller_node_code,
     buyer_node_code, source_hub_code, destination_hub_code, source_tick,
     requested_units, state, source_freight_fact_id, last_receipt_fact_id,
     version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, NULL, $13, $14::jsonb)`,
			worldID, shipment.Code, shipment.FreightSourceCode, shipment.OrderCode,
			shipment.SellerNodeCode, shipment.BuyerNodeCode, shipment.SourceHubCode,
			shipment.DestinationHubCode, shipment.SourceTick, shipment.RequestedUnits,
			shipment.State, rootFactID, shipment.Version, []byte(shipment.Metadata)); err != nil {
			return count, fmt.Errorf("restore V17 freight-receipt shipment %s: %w", shipment.Code, err)
		}
		count++
	}

	for _, line := range receiptState.Lines {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_enterprise_freight_shipment_lines
    (world_id, shipment_code, line_no, resource_code, source_firm_code,
     source_district_code, destination_firm_code, destination_district_code,
     quantity_units, unit_price_units, total_price_units, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb)`,
			worldID, line.ShipmentCode, line.LineNo, line.ResourceCode, line.SourceFirmCode,
			line.SourceDistrictCode, line.DestinationFirmCode, line.DestinationDistrictCode,
			line.QuantityUnits, line.UnitPriceUnits, line.TotalPriceUnits, []byte(line.Metadata)); err != nil {
			return count, fmt.Errorf("restore V17 freight-receipt shipment line %s/%d: %w", line.ShipmentCode, line.LineNo, err)
		}
		count++
	}

	factIDs := make(map[cityOpenWorldEnterpriseFreightReceiptRecoveryFactKey]int64, len(receiptState.Facts))
	for _, fact := range receiptState.Facts {
		var freightFactID, supplyFactID any
		var evidenceErr error
		if fact.EvidenceKind == cityOpenWorldEnterpriseFreightReceiptEvidenceFreight {
			if fact.FreightSourceCode == nil {
				return count, fmt.Errorf("V17 freight receipt fact %s is missing freight source evidence", fact.ShipmentCode)
			}
			freightFactID, evidenceErr = loadCityOpenWorldEnterpriseFreightReceiptRecoveryFreightFactID(
				ctx, tx, worldID, *fact.FreightSourceCode,
				CityOpenWorldEnterpriseFreightReceiptFactRef{EvidenceKind: fact.EvidenceKind, Tick: fact.Tick, Sequence: fact.Sequence},
			)
		} else {
			if fact.SupplyOrderCode == nil {
				return count, fmt.Errorf("V17 freight receipt fact %s is missing supply evidence", fact.ShipmentCode)
			}
			expectedFactType := ""
			switch fact.FactType {
			case "receipt.confirmed":
				expectedFactType = "order.delivered"
			case "settlement.confirmed":
				expectedFactType = "order.settled"
			default:
				return count, fmt.Errorf("V17 freight receipt fact %s has invalid supply fact type %q", fact.ShipmentCode, fact.FactType)
			}
			supplyFactID, evidenceErr = loadCityOpenWorldEnterpriseFreightReceiptRecoverySupplyFactID(
				ctx, tx, worldID, *fact.SupplyOrderCode,
				CityOpenWorldEnterpriseFreightReceiptFactRef{EvidenceKind: fact.EvidenceKind, Tick: fact.Tick, Sequence: fact.Sequence}, expectedFactType,
			)
		}
		if evidenceErr != nil {
			return count, evidenceErr
		}
		var factID int64
		if err := tx.QueryRowContext(ctx, `
INSERT INTO city_open_world_enterprise_freight_receipt_facts
    (world_id, shipment_code, tick, sequence, fact_type, evidence_kind,
     freight_fact_id, supply_chain_fact_id, payload)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb)
RETURNING id`, worldID, fact.ShipmentCode, fact.Tick, fact.Sequence, fact.FactType,
			fact.EvidenceKind, freightFactID, supplyFactID, []byte(fact.Payload)).Scan(&factID); err != nil {
			return count, fmt.Errorf("restore V17 freight-receipt fact %s/%s/%d/%d: %w", fact.ShipmentCode, fact.EvidenceKind, fact.Tick, fact.Sequence, err)
		}
		factIDs[cityOpenWorldEnterpriseFreightReceiptRecoveryKey(fact.ShipmentCode,
			CityOpenWorldEnterpriseFreightReceiptFactRef{EvidenceKind: fact.EvidenceKind, Tick: fact.Tick, Sequence: fact.Sequence})] = factID
		count++
	}

	for _, shipment := range receiptState.Shipments {
		lastFactID, resolveErr := requireCityOpenWorldEnterpriseFreightReceiptRecoveryFactID(
			factIDs, shipment.Code, shipment.LastFact,
		)
		if resolveErr != nil {
			return count, resolveErr
		}
		if _, err := tx.ExecContext(ctx, `
UPDATE city_open_world_enterprise_freight_shipments
SET last_receipt_fact_id = $3, updated_at = NOW()
WHERE world_id = $1 AND code = $2`, worldID, shipment.Code, lastFactID); err != nil {
			return count, fmt.Errorf("restore V17 freight-receipt shipment last fact %s: %w", shipment.Code, err)
		}
	}

	for _, transition := range receiptState.Transitions {
		sourceFactID, resolveErr := requireCityOpenWorldEnterpriseFreightReceiptRecoveryFactID(
			factIDs, transition.ShipmentCode, transition.SourceFact,
		)
		if resolveErr != nil {
			return count, resolveErr
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_enterprise_freight_shipment_transitions
    (world_id, shipment_code, transition_tick, transition_sequence, state,
     reason_code, source_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb)`,
			worldID, transition.ShipmentCode, transition.TransitionTick,
			transition.TransitionSequence, transition.State, transition.ReasonCode,
			sourceFactID, []byte(transition.Metadata)); err != nil {
			return count, fmt.Errorf("restore V17 freight-receipt transition %s/%d/%d: %w", transition.ShipmentCode, transition.TransitionTick, transition.TransitionSequence, err)
		}
		count++
	}

	for _, receipt := range receiptState.Receipts {
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
		sourceFactID, sourceErr := requireCityOpenWorldEnterpriseFreightReceiptRecoveryFactID(
			factIDs, receipt.ShipmentCode, receipt.SourceFact,
		)
		if sourceErr != nil {
			return count, sourceErr
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_enterprise_freight_receipts
    (world_id, shipment_code, order_code, received_tick,
     supply_chain_delivery_id, resource_operation_id, source_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb)`,
			worldID, receipt.ShipmentCode, receipt.OrderCode, receipt.ReceivedTick,
			deliveryID, operationID, sourceFactID, []byte(receipt.Metadata)); err != nil {
			return count, fmt.Errorf("restore V17 freight receipt %s: %w", receipt.ShipmentCode, err)
		}
		count++
	}
	if err := assertCityOpenWorldEnterpriseFreightReceiptFoundation(ctx, tx, worldID); err != nil {
		return count, fmt.Errorf("validate restored V17 freight-receipt foundation: %w", err)
	}
	return count, nil
}
