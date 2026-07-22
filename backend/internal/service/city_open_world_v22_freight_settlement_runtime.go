package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

type cityOpenWorldFreightSettlementPolicyDelta struct {
	orders   int64
	cases    int64
	receipts int64
	claims   int64
	accepted int64
	lost     int64
	rejected int64
	refunded int64
}

type cityOpenWorldFreightSettlementOrderRecord struct {
	code       string
	sourceKind string
	sourceCode string
	orderCode  string
	sourceTick int64
	state      string
	version    int64
}

type cityOpenWorldFreightSettlementCaseRecord struct {
	code                string
	settlementOrderCode string
	sourceKind          string
	sourceCode          string
	transportState      string
	state               string
	sourceTick          int64
	version             int64
}

type cityOpenWorldFreightSettlementLineRecord struct {
	sourceLineNo            int
	resourceCode            string
	sourceFirmCode          string
	sourceDistrictCode      string
	destinationFirmCode     string
	destinationDistrictCode string
	quantityUnits           int64
	unitPriceUnits          int64
	totalPriceUnits         int64
}

func ensureCityOpenWorldFreightSettlementEngine(ctx context.Context, tx *sql.Tx, worldID int64) error {
	var version string
	if err := tx.QueryRowContext(ctx, `SELECT simulation_version FROM city_worlds WHERE id = $1 FOR UPDATE`, worldID).Scan(&version); err != nil {
		return fmt.Errorf("lock V22 freight-settlement world: %w", err)
	}
	if !cityEngineSupportsOpenWorldFreightSettlements(version) {
		return ErrCityCommandVersion.WithMetadata(map[string]string{"version": version})
	}
	return nil
}

func loadCityOpenWorldFreightSettlementPolicyForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
) (*CityOpenWorldFreightSettlementPolicy, error) {
	policy := &CityOpenWorldFreightSettlementPolicy{}
	err := tx.QueryRowContext(ctx, `
SELECT profile_id, profile_version, content_hash, baseline_tick,
       source_contract, receipt_contract, resource_contract, financial_contract,
       liability_contract, maximum_orders, maximum_cases_per_order,
       maximum_receipts_per_case, maximum_receipts_per_tick, order_count,
       case_count, receipt_count, claim_count, accepted_units, lost_units,
       rejected_units, refunded_units, revision, metadata
FROM city_open_world_freight_settlement_profiles
WHERE world_id = $1
FOR UPDATE`, worldID).Scan(
		&policy.ProfileID, &policy.ProfileVersion, &policy.ContentHash, &policy.BaselineTick,
		&policy.SourceContract, &policy.ReceiptContract, &policy.ResourceContract, &policy.FinancialContract,
		&policy.LiabilityContract, &policy.MaximumOrders, &policy.MaximumCasesPerOrder,
		&policy.MaximumReceiptsPerCase, &policy.MaximumReceiptsPerTick, &policy.OrderCount,
		&policy.CaseCount, &policy.ReceiptCount, &policy.ClaimCount, &policy.AcceptedUnits,
		&policy.LostUnits, &policy.RejectedUnits, &policy.RefundedUnits, &policy.Revision, &policy.Metadata,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v22_freight_settlement_profile"})
	}
	if err != nil {
		return nil, fmt.Errorf("lock V22 freight-settlement profile: %w", err)
	}
	return policy, nil
}

func updateCityOpenWorldFreightSettlementPolicy(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	delta cityOpenWorldFreightSettlementPolicyDelta,
) error {
	if delta.orders < 0 || delta.cases < 0 || delta.receipts < 0 || delta.claims < 0 ||
		delta.accepted < 0 || delta.lost < 0 || delta.rejected < 0 || delta.refunded < 0 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v22_freight_settlement_policy_delta"})
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_open_world_freight_settlement_profiles
SET order_count = order_count + $2,
    case_count = case_count + $3,
    receipt_count = receipt_count + $4,
    claim_count = claim_count + $5,
    accepted_units = accepted_units + $6,
    lost_units = lost_units + $7,
    rejected_units = rejected_units + $8,
    refunded_units = refunded_units + $9,
    revision = revision + 1,
    updated_at = NOW()
WHERE world_id = $1
  AND order_count + $2 BETWEEN 0 AND maximum_orders
  AND case_count + $3 >= 0
  AND receipt_count + $4 >= 0
  AND claim_count + $5 >= 0
  AND accepted_units + $6 >= 0
  AND lost_units + $7 >= 0
  AND rejected_units + $8 >= 0
  AND refunded_units + $9 >= 0`,
		worldID, delta.orders, delta.cases, delta.receipts, delta.claims,
		delta.accepted, delta.lost, delta.rejected, delta.refunded,
	)
	if err != nil {
		return fmt.Errorf("update V22 freight-settlement policy: %w", err)
	}
	rows, rowsErr := result.RowsAffected()
	if rowsErr != nil || rows != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v22_freight_settlement_policy_update"})
	}
	return nil
}

// advanceCityOpenWorldV22FreightSettlements creates only durable tracking
// cases for post-baseline custody evidence that has reached an actionable
// terminal observation. It never settles inventory, releases a reservation,
// posts a refund, or mutates a V15/V17/V18 state; those effects belong to the
// explicit receipt command.
func advanceCityOpenWorldV22FreightSettlements(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
) error {
	if targetTick <= 0 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v22_freight_settlement_tick"})
	}
	if err := ensureCityOpenWorldFreightSettlementEngine(ctx, tx, worldID); err != nil {
		return err
	}
	if err := activateCityOpenWorldFreightSettlementWrite(ctx, tx, worldID); err != nil {
		return err
	}
	policy, err := loadCityOpenWorldFreightSettlementPolicyForUpdate(ctx, tx, worldID)
	if err != nil {
		return err
	}
	delta := cityOpenWorldFreightSettlementPolicyDelta{}
	if err = materializeCityOpenWorldFreightSettlementShipmentCases(ctx, tx, worldID, targetTick, policy, &delta); err != nil {
		return err
	}
	if err = materializeCityOpenWorldFreightSettlementBatchCases(ctx, tx, worldID, targetTick, policy, &delta); err != nil {
		return err
	}
	if delta.orders != 0 || delta.cases != 0 {
		if err = updateCityOpenWorldFreightSettlementPolicy(ctx, tx, worldID, delta); err != nil {
			return err
		}
	}
	return assertCityOpenWorldFreightSettlementFoundation(ctx, tx, worldID)
}

func cityOpenWorldFreightSettlementActionableTransportState(state string) bool {
	return state == cityOpenWorldEnterpriseFreightReceiptStateAwaitingReceipt ||
		state == cityOpenWorldEnterpriseFreightReceiptStateExpired ||
		state == cityOpenWorldEnterpriseFreightReceiptStateVoided ||
		state == cityOpenWorldEnterpriseFreightReceiptStateOrphaned
}

func materializeCityOpenWorldFreightSettlementShipmentCases(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	policy *CityOpenWorldFreightSettlementPolicy,
	delta *cityOpenWorldFreightSettlementPolicyDelta,
) error {
	if policy == nil || delta == nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v22_freight_settlement_shipment_input"})
	}
	rows, err := tx.QueryContext(ctx, `
SELECT shipment.code, shipment.order_code, shipment.source_tick, shipment.state
FROM city_open_world_enterprise_freight_shipments shipment
JOIN LATERAL (
    SELECT transition.state
    FROM city_open_world_supply_chain_order_transitions transition
    WHERE transition.world_id = shipment.world_id
      AND transition.order_code = shipment.order_code
    ORDER BY transition.transition_tick DESC, transition.transition_sequence DESC
    LIMIT 1
) supply_state ON TRUE
LEFT JOIN city_open_world_freight_batch_plans batch
  ON batch.world_id = shipment.world_id AND batch.order_code = shipment.order_code
LEFT JOIN city_open_world_freight_settlement_orders settlement_order
  ON settlement_order.world_id = shipment.world_id
 AND settlement_order.source_kind = 'shipment'
 AND settlement_order.source_code = shipment.code
WHERE shipment.world_id = $1
  AND shipment.source_tick > $2
  AND supply_state.state = 'dispatched'
  AND shipment.state IN ('awaiting_receipt','expired','voided','orphaned')
  AND batch.code IS NULL
  AND settlement_order.code IS NULL
ORDER BY shipment.source_tick, shipment.code
FOR UPDATE OF shipment`, worldID, policy.BaselineTick)
	if err != nil {
		return fmt.Errorf("load V22 freight-settlement shipment candidates: %w", err)
	}
	candidates := make([]struct {
		code, orderCode, state string
		sourceTick             int64
	}, 0)
	for rows.Next() {
		item := struct {
			code, orderCode, state string
			sourceTick             int64
		}{}
		if err = rows.Scan(&item.code, &item.orderCode, &item.sourceTick, &item.state); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan V22 freight-settlement shipment candidate: %w", err)
		}
		candidates = append(candidates, item)
	}
	if err = closeCityRows(rows, "iterate V22 freight-settlement shipment candidates"); err != nil {
		return err
	}
	for _, candidate := range candidates {
		if policy.OrderCount+delta.orders >= int64(policy.MaximumOrders) {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v22_freight_settlement_order_limit"})
		}
		orderCode := cityOpenWorldFreightSettlementOrderCode(cityOpenWorldFreightSettlementSourceShipment, candidate.code)
		caseCode := cityOpenWorldFreightSettlementCaseCode(orderCode, cityOpenWorldFreightSettlementSourceShipment, candidate.code)
		if err = insertCityOpenWorldFreightSettlementOrder(ctx, tx, worldID, orderCode,
			cityOpenWorldFreightSettlementSourceShipment, candidate.code, candidate.orderCode, candidate.sourceTick,
			cityOpenWorldFreightSettlementOrderReceiving, targetTick); err != nil {
			return err
		}
		if err = insertCityOpenWorldFreightSettlementCase(ctx, tx, worldID, caseCode, orderCode,
			cityOpenWorldFreightSettlementSourceShipment, candidate.code, candidate.state, candidate.sourceTick, targetTick); err != nil {
			return err
		}
		lines, linesErr := loadCityOpenWorldFreightSettlementShipmentLines(ctx, tx, worldID, candidate.code)
		if linesErr != nil {
			return linesErr
		}
		if err = insertCityOpenWorldFreightSettlementCaseLines(ctx, tx, worldID, caseCode, lines); err != nil {
			return err
		}
		delta.orders++
		delta.cases++
	}
	return nil
}

func materializeCityOpenWorldFreightSettlementBatchCases(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	policy *CityOpenWorldFreightSettlementPolicy,
	delta *cityOpenWorldFreightSettlementPolicyDelta,
) error {
	if policy == nil || delta == nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v22_freight_settlement_batch_input"})
	}
	// A plan may have consignments become actionable over several ticks. The
	// order is created on the first such observation; each remaining case is
	// appended only when its own V18 custody state is terminal/actionable.
	rows, err := tx.QueryContext(ctx, `
SELECT plan.code, plan.order_code, plan.source_tick,
       consignment.code, consignment.state
FROM city_open_world_freight_batch_plans plan
JOIN LATERAL (
    SELECT transition.state
    FROM city_open_world_supply_chain_order_transitions transition
    WHERE transition.world_id = plan.world_id
      AND transition.order_code = plan.order_code
    ORDER BY transition.transition_tick DESC, transition.transition_sequence DESC
    LIMIT 1
) supply_state ON TRUE
JOIN city_open_world_freight_batch_consignments consignment
  ON consignment.world_id = plan.world_id AND consignment.plan_code = plan.code
LEFT JOIN city_open_world_freight_settlement_orders settlement_order
  ON settlement_order.world_id = plan.world_id
 AND settlement_order.source_kind = 'consignment'
 AND settlement_order.source_code = plan.code
LEFT JOIN city_open_world_freight_settlement_cases settlement_case
  ON settlement_case.world_id = consignment.world_id
 AND settlement_case.source_kind = 'consignment'
 AND settlement_case.source_code = consignment.code
WHERE plan.world_id = $1
  AND plan.source_tick > $2
  AND supply_state.state = 'dispatched'
  AND consignment.state IN ('awaiting_receipt','expired','voided','orphaned')
  AND settlement_case.code IS NULL
  AND (settlement_order.code IS NULL OR settlement_order.state IN ('awaiting_transport','receiving'))
ORDER BY plan.source_tick, plan.code, consignment.batch_no
FOR UPDATE OF plan, consignment`, worldID, policy.BaselineTick)
	if err != nil {
		return fmt.Errorf("load V22 freight-settlement batch candidates: %w", err)
	}
	type candidate struct {
		planCode, orderCode, consignmentCode, state string
		sourceTick                                  int64
	}
	candidates := make([]candidate, 0)
	for rows.Next() {
		item := candidate{}
		if err = rows.Scan(&item.planCode, &item.orderCode, &item.sourceTick, &item.consignmentCode, &item.state); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan V22 freight-settlement batch candidate: %w", err)
		}
		candidates = append(candidates, item)
	}
	if err = closeCityRows(rows, "iterate V22 freight-settlement batch candidates"); err != nil {
		return err
	}
	for _, candidate := range candidates {
		settlementOrderCode := cityOpenWorldFreightSettlementOrderCode(cityOpenWorldFreightSettlementSourceConsignment, candidate.planCode)
		var exists bool
		if err = tx.QueryRowContext(ctx, `
SELECT EXISTS (
    SELECT 1 FROM city_open_world_freight_settlement_orders
    WHERE world_id = $1 AND code = $2
    FOR UPDATE
)`, worldID, settlementOrderCode).Scan(&exists); err != nil {
			return fmt.Errorf("lock V22 freight-settlement batch order: %w", err)
		}
		if !exists {
			if policy.OrderCount+delta.orders >= int64(policy.MaximumOrders) {
				return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v22_freight_settlement_order_limit"})
			}
			if err = insertCityOpenWorldFreightSettlementOrder(ctx, tx, worldID, settlementOrderCode,
				cityOpenWorldFreightSettlementSourceConsignment, candidate.planCode, candidate.orderCode, candidate.sourceTick,
				cityOpenWorldFreightSettlementOrderReceiving, targetTick); err != nil {
				return err
			}
			delta.orders++
		}
		if policy.CaseCount+delta.cases >= int64(policy.MaximumOrders*policy.MaximumCasesPerOrder) {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v22_freight_settlement_case_limit"})
		}
		caseCode := cityOpenWorldFreightSettlementCaseCode(settlementOrderCode, cityOpenWorldFreightSettlementSourceConsignment, candidate.consignmentCode)
		if err = insertCityOpenWorldFreightSettlementCase(ctx, tx, worldID, caseCode, settlementOrderCode,
			cityOpenWorldFreightSettlementSourceConsignment, candidate.consignmentCode, candidate.state, candidate.sourceTick, targetTick); err != nil {
			return err
		}
		lines, linesErr := loadCityOpenWorldFreightSettlementBatchLines(ctx, tx, worldID, candidate.consignmentCode)
		if linesErr != nil {
			return linesErr
		}
		if err = insertCityOpenWorldFreightSettlementCaseLines(ctx, tx, worldID, caseCode, lines); err != nil {
			return err
		}
		delta.cases++
	}
	return nil
}

func insertCityOpenWorldFreightSettlementOrder(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	code, sourceKind, sourceCode, orderCode string,
	sourceTick int64,
	state string,
	targetTick int64,
) error {
	metadata, err := json.Marshal(map[string]any{
		"schema_version": cityOpenWorldFreightSettlementSchemaVersion,
		"created_tick":   targetTick,
		"source_kind":    sourceKind,
		"source_code":    sourceCode,
	})
	if err != nil {
		return fmt.Errorf("marshal V22 freight-settlement order metadata: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_freight_settlement_orders
    (world_id, code, source_kind, source_code, order_code, source_tick, state, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb)`,
		worldID, code, sourceKind, sourceCode, orderCode, sourceTick, state, []byte(metadata)); err != nil {
		return fmt.Errorf("insert V22 freight-settlement order: %w", err)
	}
	return nil
}

func insertCityOpenWorldFreightSettlementCase(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	code, settlementOrderCode, sourceKind, sourceCode, transportState string,
	sourceTick, targetTick int64,
) error {
	metadata, err := json.Marshal(map[string]any{
		"schema_version":  cityOpenWorldFreightSettlementSchemaVersion,
		"created_tick":    targetTick,
		"transport_state": transportState,
	})
	if err != nil {
		return fmt.Errorf("marshal V22 freight-settlement case metadata: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_freight_settlement_cases
    (world_id, code, settlement_order_code, source_kind, source_code,
     transport_state, state, source_tick, metadata)
VALUES ($1, $2, $3, $4, $5, $6, 'awaiting_outcome', $7, $8::jsonb)`,
		worldID, code, settlementOrderCode, sourceKind, sourceCode, transportState, sourceTick, []byte(metadata)); err != nil {
		return fmt.Errorf("insert V22 freight-settlement case: %w", err)
	}
	return nil
}

func insertCityOpenWorldFreightSettlementCaseLines(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	caseCode string,
	lines []cityOpenWorldFreightSettlementLineRecord,
) error {
	if len(lines) == 0 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v22_freight_settlement_case_lines"})
	}
	for _, line := range lines {
		metadata, err := json.Marshal(map[string]any{
			"schema_version": cityOpenWorldFreightSettlementSchemaVersion,
			"source_line_no": line.sourceLineNo,
		})
		if err != nil {
			return fmt.Errorf("marshal V22 freight-settlement case line metadata: %w", err)
		}
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_freight_settlement_case_lines
    (world_id, case_code, source_line_no, resource_code, source_firm_code,
     source_district_code, destination_firm_code, destination_district_code,
     quantity_units, unit_price_units, total_price_units, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb)`,
			worldID, caseCode, line.sourceLineNo, line.resourceCode, line.sourceFirmCode,
			line.sourceDistrictCode, line.destinationFirmCode, line.destinationDistrictCode,
			line.quantityUnits, line.unitPriceUnits, line.totalPriceUnits, []byte(metadata)); err != nil {
			return fmt.Errorf("insert V22 freight-settlement case line: %w", err)
		}
	}
	return nil
}

func loadCityOpenWorldFreightSettlementShipmentLines(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	shipmentCode string,
) ([]cityOpenWorldFreightSettlementLineRecord, error) {
	return loadCityOpenWorldFreightSettlementLines(ctx, tx, worldID, `
SELECT line.line_no, line.resource_code, line.source_firm_code,
       line.source_district_code, line.destination_firm_code,
       line.destination_district_code, line.quantity_units,
       line.unit_price_units, line.total_price_units
FROM city_open_world_enterprise_freight_shipment_lines line
WHERE line.world_id = $1 AND line.shipment_code = $2
ORDER BY line.line_no`, shipmentCode, "V17 shipment")
}

func loadCityOpenWorldFreightSettlementBatchLines(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	consignmentCode string,
) ([]cityOpenWorldFreightSettlementLineRecord, error) {
	return loadCityOpenWorldFreightSettlementLines(ctx, tx, worldID, `
	SELECT line.source_line_no, line.resource_code, source_firm.code,
	       source_district.code, destination_firm.code, destination_district.code, line.quantity_units,
	       line.unit_price_units, line.total_price_units
FROM city_open_world_freight_batch_lines line
JOIN city_open_world_freight_batch_consignments consignment
  ON consignment.world_id = line.world_id AND consignment.code = line.consignment_code
JOIN city_open_world_freight_batch_plans plan
  ON plan.world_id = consignment.world_id AND plan.code = consignment.plan_code
JOIN city_open_world_supply_chain_order_lines supply_line
  ON supply_line.world_id = plan.world_id AND supply_line.order_code = plan.order_code
 AND supply_line.line_no = line.source_line_no
JOIN city_inventory_balances source_balance
  ON source_balance.id = supply_line.source_balance_id AND source_balance.world_id = supply_line.world_id
JOIN city_economic_entities source_firm
  ON source_firm.id = source_balance.entity_id AND source_firm.world_id = supply_line.world_id
JOIN city_districts source_district
  ON source_district.id = source_balance.district_id AND source_district.world_id = supply_line.world_id
JOIN city_inventory_balances destination_balance
  ON destination_balance.id = supply_line.destination_balance_id AND destination_balance.world_id = supply_line.world_id
JOIN city_economic_entities destination_firm
  ON destination_firm.id = destination_balance.entity_id AND destination_firm.world_id = supply_line.world_id
JOIN city_districts destination_district
  ON destination_district.id = destination_balance.district_id AND destination_district.world_id = supply_line.world_id
WHERE line.world_id = $1 AND line.consignment_code = $2
ORDER BY line.source_line_no`, consignmentCode, "V18 consignment")
}

func loadCityOpenWorldFreightSettlementLines(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	query, code, label string,
) ([]cityOpenWorldFreightSettlementLineRecord, error) {
	rows, err := tx.QueryContext(ctx, query, worldID, code)
	if err != nil {
		return nil, fmt.Errorf("load V22 freight-settlement %s lines: %w", label, err)
	}
	items := make([]cityOpenWorldFreightSettlementLineRecord, 0)
	for rows.Next() {
		item := cityOpenWorldFreightSettlementLineRecord{}
		if err = rows.Scan(&item.sourceLineNo, &item.resourceCode, &item.sourceFirmCode,
			&item.sourceDistrictCode, &item.destinationFirmCode, &item.destinationDistrictCode,
			&item.quantityUnits, &item.unitPriceUnits, &item.totalPriceUnits); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan V22 freight-settlement %s line: %w", label, err)
		}
		items = append(items, item)
	}
	if err = closeCityRows(rows, "iterate V22 freight-settlement "+label+" lines"); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v22_freight_settlement_source_lines"})
	}
	return items, nil
}
