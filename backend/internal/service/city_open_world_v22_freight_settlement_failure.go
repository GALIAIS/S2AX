package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

const cityOpenWorldFreightSettlementReasonV15FailedWithoutReceipt = "v15_order_failed_without_v22_receipt"

// cityOpenWorldFreightSettlementVoidedCustodySource identifies the one V22
// no-receipt failure closure that intentionally freezes predecessor custody.
// V16 supplies its freight-source code for shipment checks; V18 supplies its
// consignment code. Later automatic passes must not convert that preserved
// V17/V18 observation into a synthetic orphan because V15 was reversed.
func cityOpenWorldFreightSettlementVoidedCustodySource(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	sourceKind, sourceCode string,
) (bool, error) {
	if worldID <= 0 || tx == nil || sourceCode == "" || !cityOpenWorldFreightSettlementSourceKindValid(sourceKind) {
		return false, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v22_freight_settlement_voided_custody"})
	}
	var preserved bool
	err := tx.QueryRowContext(ctx, `
SELECT EXISTS (
    SELECT 1
    FROM city_open_world_freight_settlement_cases settlement_case
    JOIN city_open_world_freight_settlement_orders settlement_order
      ON settlement_order.world_id = settlement_case.world_id
     AND settlement_order.code = settlement_case.settlement_order_code
    WHERE settlement_case.world_id = $1
      AND settlement_case.source_kind = $2
      AND (
          (settlement_case.source_kind = 'shipment' AND EXISTS (
              SELECT 1
              FROM city_open_world_enterprise_freight_shipments shipment
              WHERE shipment.world_id = settlement_case.world_id
                AND shipment.code = settlement_case.source_code
                AND shipment.freight_source_code = $3
          ))
          OR (settlement_case.source_kind = 'consignment' AND settlement_case.source_code = $3)
      )
      AND settlement_case.state = 'voided'
      AND settlement_order.state = 'voided'
      AND NOT EXISTS (
          SELECT 1
          FROM city_open_world_freight_settlement_receipts receipt
          WHERE receipt.world_id = settlement_case.world_id
            AND receipt.case_code = settlement_case.code
      )
)`, worldID, sourceKind, sourceCode).Scan(&preserved)
	if err != nil {
		return false, fmt.Errorf("check V22 no-receipt custody preservation: %w", err)
	}
	return preserved, nil
}

// cityOpenWorldFreightSettlementFailureClosure is deliberately small: V15 is
// still the owner of the failed-order fact, reservation releases and full
// acceptance reversal. V22 only records that its receipt contract ended
// before it transferred, consumed, rejected, refunded, or claimed any cargo.
//
// Keeping the V17/V18 source untouched is important. A failed commercial
// order does not rewrite already-observed transport provenance into a fake
// delivery or V22 settlement.
type cityOpenWorldFreightSettlementFailureClosure struct {
	settlementOrderCode string
	supplyOrderCode     string
	caseCodes           []string
}

// prepareCityOpenWorldFreightSettlementFailureClosure locks a V22 overlay for
// a V15 failure command. A legacy failure remains valid while the overlay has
// no receipts at all. Once any V22 receipt exists, even a partial one, the
// old whole-order reversal would corrupt the quantity/refund ledger and must
// remain rejected.
func prepareCityOpenWorldFreightSettlementFailureClosure(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	supplyOrderCode string,
) (*cityOpenWorldFreightSettlementFailureClosure, error) {
	if worldID <= 0 || supplyOrderCode == "" {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v22_freight_settlement_failure_input"})
	}

	order := &cityOpenWorldFreightSettlementOrderRecord{}
	err := tx.QueryRowContext(ctx, `
SELECT code, source_kind, source_code, order_code, source_tick, state, version
FROM city_open_world_freight_settlement_orders
WHERE world_id = $1 AND order_code = $2
FOR UPDATE`, worldID, supplyOrderCode).Scan(
		&order.code, &order.sourceKind, &order.sourceCode, &order.orderCode,
		&order.sourceTick, &order.state, &order.version,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lock V22 freight-settlement order for V15 failure: %w", err)
	}
	if order.orderCode != supplyOrderCode ||
		(order.state != cityOpenWorldFreightSettlementOrderAwaiting && order.state != cityOpenWorldFreightSettlementOrderReceiving) {
		return nil, cityOpenWorldSupplyChainReject(cityOpenWorldFreightSettlementRejectionSettlementRequired)
	}

	rows, err := tx.QueryContext(ctx, `
SELECT code, state
FROM city_open_world_freight_settlement_cases
WHERE world_id = $1 AND settlement_order_code = $2
ORDER BY code
FOR UPDATE`, worldID, order.code)
	if err != nil {
		return nil, fmt.Errorf("lock V22 freight-settlement cases for V15 failure: %w", err)
	}
	caseCodes := make([]string, 0)
	for rows.Next() {
		var code, state string
		if err = rows.Scan(&code, &state); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan V22 freight-settlement case for V15 failure: %w", err)
		}
		if state != cityOpenWorldFreightSettlementCaseAwaiting {
			_ = rows.Close()
			return nil, cityOpenWorldSupplyChainReject(cityOpenWorldFreightSettlementRejectionSettlementRequired)
		}
		caseCodes = append(caseCodes, code)
	}
	if err = closeCityRows(rows, "iterate V22 freight-settlement cases for V15 failure"); err != nil {
		return nil, err
	}
	if len(caseCodes) == 0 {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v22_freight_settlement_failure_cases"})
	}

	var hasReceipt bool
	if err = tx.QueryRowContext(ctx, `
SELECT EXISTS (
    SELECT 1
    FROM city_open_world_freight_settlement_receipts receipt
    JOIN city_open_world_freight_settlement_cases settlement_case
      ON settlement_case.world_id = receipt.world_id
     AND settlement_case.code = receipt.case_code
    WHERE receipt.world_id = $1
      AND settlement_case.settlement_order_code = $2
)`, worldID, order.code).Scan(&hasReceipt); err != nil {
		return nil, fmt.Errorf("check V22 freight-settlement receipts for V15 failure: %w", err)
	}
	if hasReceipt {
		return nil, cityOpenWorldSupplyChainReject(cityOpenWorldFreightSettlementRejectionSettlementRequired)
	}

	return &cityOpenWorldFreightSettlementFailureClosure{
		settlementOrderCode: order.code,
		supplyOrderCode:     order.orderCode,
		caseCodes:           caseCodes,
	}, nil
}

// closeCityOpenWorldFreightSettlementForFailedSupplyOrder runs only after V15
// has appended its order.failed transition. It makes the no-receipt closure
// explicit and leaves all V22 counters at zero; receipts, resource operations,
// refund journals, and carrier claims never exist for this path.
func closeCityOpenWorldFreightSettlementForFailedSupplyOrder(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	command *CityCommand,
	closure *cityOpenWorldFreightSettlementFailureClosure,
) error {
	if closure == nil {
		return nil
	}
	if worldID <= 0 || targetTick <= 0 || command == nil || command.ID <= 0 || command.Sequence <= 0 ||
		closure.settlementOrderCode == "" || closure.supplyOrderCode == "" || len(closure.caseCodes) == 0 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v22_freight_settlement_failure_closure"})
	}
	if err := ensureCityOpenWorldFreightSettlementEngine(ctx, tx, worldID); err != nil {
		return err
	}
	if err := activateCityOpenWorldFreightSettlementWrite(ctx, tx, worldID); err != nil {
		return err
	}

	metadata, err := json.Marshal(map[string]any{
		"v15_failure_closure": map[string]any{
			"reason_code":              cityOpenWorldFreightSettlementReasonV15FailedWithoutReceipt,
			"failed_tick":              targetTick,
			"failure_command_sequence": command.Sequence,
		},
	})
	if err != nil {
		return fmt.Errorf("marshal V22 no-receipt failure closure metadata: %w", err)
	}

	caseResult, err := tx.ExecContext(ctx, `
UPDATE city_open_world_freight_settlement_cases
SET state = 'voided',
    version = version + 1,
    metadata = metadata || $3::jsonb,
    updated_at = NOW()
WHERE world_id = $1
  AND settlement_order_code = $2
  AND state = 'awaiting_outcome'`, worldID, closure.settlementOrderCode, []byte(metadata))
	if err != nil {
		return fmt.Errorf("void V22 freight-settlement cases after V15 failure: %w", err)
	}
	if rows, rowsErr := caseResult.RowsAffected(); rowsErr != nil || rows != int64(len(closure.caseCodes)) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v22_freight_settlement_failure_case_state"})
	}

	orderResult, err := tx.ExecContext(ctx, `
UPDATE city_open_world_freight_settlement_orders
SET state = 'voided',
    version = version + 1,
    metadata = metadata || $3::jsonb,
    updated_at = NOW()
WHERE world_id = $1
  AND code = $2
  AND state IN ('awaiting_transport', 'receiving')`, worldID, closure.settlementOrderCode, []byte(metadata))
	if err != nil {
		return fmt.Errorf("void V22 freight-settlement order after V15 failure: %w", err)
	}
	if rows, rowsErr := orderResult.RowsAffected(); rowsErr != nil || rows != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v22_freight_settlement_failure_order_state"})
	}
	if err = assertCityOpenWorldFreightSettlementFoundation(ctx, tx, worldID); err != nil {
		return err
	}
	return nil
}

func annotateCityOpenWorldSupplyChainFailureWithFreightSettlementClosure(
	pending *cityPendingEvent,
	closure *cityOpenWorldFreightSettlementFailureClosure,
) {
	if pending == nil || closure == nil {
		return
	}
	if pending.payload == nil {
		pending.payload = make(map[string]any)
	}
	if pending.result == nil {
		pending.result = make(map[string]any)
	}
	pending.payload["freight_settlement_order_code"] = closure.settlementOrderCode
	pending.payload["freight_settlement_state"] = cityOpenWorldFreightSettlementOrderVoided
	pending.result["freight_settlement_order_code"] = closure.settlementOrderCode
	pending.result["freight_settlement_state"] = cityOpenWorldFreightSettlementOrderVoided
}
