package service

import (
	"context"
	"database/sql"
	"fmt"
)

// restoreCityOpenWorldFreightSettlementProjection reconnects V22's immutable
// receipt evidence after the V15 order lifecycle, V17 custody projection, V18
// batch projection, and durable command/resource/journal records have been
// restored. Snapshot state contains only stable public identities and cursor
// pairs; database IDs are deliberately resolved inside the recovery
// transaction rather than preserved as incidental storage details.
func restoreCityOpenWorldFreightSettlementProjection(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	settlementState CityOpenWorldFreightSettlementState,
	commandIDs map[int64]int64,
) (int, error) {
	if err := validateCityOpenWorldFreightSettlementState(&settlementState); err != nil {
		return 0, fmt.Errorf("validate V22 freight-settlement recovery input: %w", err)
	}
	if err := activateCityOpenWorldFreightSettlementRecoveryWrite(ctx, tx, worldID); err != nil {
		return 0, err
	}

	count := 0
	policy := settlementState.Policy
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_freight_settlement_profiles
    (world_id, profile_id, profile_version, content_hash, baseline_tick,
     source_contract, receipt_contract, resource_contract, financial_contract,
     liability_contract, maximum_orders, maximum_cases_per_order,
     maximum_receipts_per_case, maximum_receipts_per_tick, order_count,
     case_count, receipt_count, claim_count, accepted_units, lost_units,
     rejected_units, refunded_units, revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14,
        $15, $16, $17, $18, $19, $20, $21, $22, $23, $24::jsonb)`,
		worldID, policy.ProfileID, policy.ProfileVersion, policy.ContentHash,
		policy.BaselineTick, policy.SourceContract, policy.ReceiptContract,
		policy.ResourceContract, policy.FinancialContract, policy.LiabilityContract,
		policy.MaximumOrders, policy.MaximumCasesPerOrder, policy.MaximumReceiptsPerCase,
		policy.MaximumReceiptsPerTick, policy.OrderCount, policy.CaseCount,
		policy.ReceiptCount, policy.ClaimCount, policy.AcceptedUnits, policy.LostUnits,
		policy.RejectedUnits, policy.RefundedUnits, policy.Revision, []byte(policy.Metadata)); err != nil {
		return count, fmt.Errorf("restore V22 freight-settlement profile: %w", err)
	}
	count++

	for _, order := range settlementState.Orders {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_freight_settlement_orders
    (world_id, code, source_kind, source_code, order_code, source_tick,
     state, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb)`,
			worldID, order.Code, order.SourceKind, order.SourceCode, order.OrderCode,
			order.SourceTick, order.State, order.Version, []byte(order.Metadata)); err != nil {
			return count, fmt.Errorf("restore V22 freight-settlement order %s: %w", order.Code, err)
		}
		count++
	}

	for _, settlementCase := range settlementState.Cases {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_freight_settlement_cases
    (world_id, code, settlement_order_code, source_kind, source_code,
     transport_state, state, source_tick, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb)`,
			worldID, settlementCase.Code, settlementCase.SettlementOrderCode,
			settlementCase.SourceKind, settlementCase.SourceCode,
			settlementCase.TransportState, settlementCase.State,
			settlementCase.SourceTick, settlementCase.Version, []byte(settlementCase.Metadata)); err != nil {
			return count, fmt.Errorf("restore V22 freight-settlement case %s: %w", settlementCase.Code, err)
		}
		count++
	}

	for _, line := range settlementState.Lines {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_freight_settlement_case_lines
    (world_id, case_code, source_line_no, resource_code, source_firm_code,
     source_district_code, destination_firm_code, destination_district_code,
     quantity_units, unit_price_units, total_price_units, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb)`,
			worldID, line.CaseCode, line.SourceLineNo, line.ResourceCode,
			line.SourceFirmCode, line.SourceDistrictCode, line.DestinationFirmCode,
			line.DestinationDistrictCode, line.QuantityUnits, line.UnitPriceUnits,
			line.TotalPriceUnits, []byte(line.Metadata)); err != nil {
			return count, fmt.Errorf("restore V22 freight-settlement case line %s/%d: %w", line.CaseCode, line.SourceLineNo, err)
		}
		count++
	}

	var err error
	for _, receipt := range settlementState.Receipts {
		sourceCommandID, found := commandIDs[receipt.SourceCommandSequence]
		if !found || sourceCommandID <= 0 {
			return count, fmt.Errorf("V22 freight-settlement receipt %s source command %d is unavailable", receipt.Code, receipt.SourceCommandSequence)
		}
		var resourceOperationID, journalID any
		if receipt.ResourceOperation != nil {
			resourceOperationID, err = loadCityOpenWorldSupplyChainRecoveryResourceOperationID(
				ctx, tx, worldID, *receipt.ResourceOperation,
			)
			if err != nil {
				return count, fmt.Errorf("restore V22 freight-settlement receipt %s resource operation: %w", receipt.Code, err)
			}
		}
		if receipt.Journal != nil {
			journalID, err = loadCityOpenWorldSupplyChainRecoveryJournalID(ctx, tx, worldID, *receipt.Journal)
			if err != nil {
				return count, fmt.Errorf("restore V22 freight-settlement receipt %s journal: %w", receipt.Code, err)
			}
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_freight_settlement_receipts
    (world_id, code, case_code, receipt_tick, source_command_id,
     liability_party, refunded_units, resource_operation_id, journal_id,
     metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb)`,
			worldID, receipt.Code, receipt.CaseCode, receipt.ReceiptTick,
			sourceCommandID, receipt.LiabilityParty, receipt.RefundedUnits,
			resourceOperationID, journalID, []byte(receipt.Metadata)); err != nil {
			return count, fmt.Errorf("restore V22 freight-settlement receipt %s: %w", receipt.Code, err)
		}
		count++
	}

	for _, line := range settlementState.ReceiptLines {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_freight_settlement_receipt_lines
    (world_id, receipt_code, case_code, source_line_no, accepted_units,
     lost_units, rejected_units)
VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			worldID, line.ReceiptCode, line.CaseCode, line.SourceLineNo,
			line.AcceptedUnits, line.LostUnits, line.RejectedUnits); err != nil {
			return count, fmt.Errorf("restore V22 freight-settlement receipt line %s/%d: %w", line.ReceiptCode, line.SourceLineNo, err)
		}
		count++
	}

	for _, claim := range settlementState.Claims {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_freight_settlement_claims
    (world_id, code, receipt_code, case_code, liability_party, claim_amount,
     state, created_tick, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb)`,
			worldID, claim.Code, claim.ReceiptCode, claim.CaseCode,
			claim.LiabilityParty, claim.ClaimAmount, claim.State, claim.CreatedTick,
			[]byte(claim.Metadata)); err != nil {
			return count, fmt.Errorf("restore V22 freight-settlement claim %s: %w", claim.Code, err)
		}
		count++
	}

	if err := assertCityOpenWorldFreightSettlementFoundation(ctx, tx, worldID); err != nil {
		return count, fmt.Errorf("validate restored V22 freight-settlement foundation: %w", err)
	}
	return count, nil
}
