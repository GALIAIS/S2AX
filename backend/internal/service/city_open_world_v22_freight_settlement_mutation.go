package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
)

const (
	cityOpenWorldFreightSettlementRejectionCaseNotFound       = "CITY_FREIGHT_SETTLEMENT_CASE_NOT_FOUND"
	cityOpenWorldFreightSettlementRejectionCaseNotActionable  = "CITY_FREIGHT_SETTLEMENT_CASE_NOT_ACTIONABLE"
	cityOpenWorldFreightSettlementRejectionReceiptLimit       = "CITY_FREIGHT_SETTLEMENT_RECEIPT_LIMIT"
	cityOpenWorldFreightSettlementRejectionOutcomeExceeded    = "CITY_FREIGHT_SETTLEMENT_OUTCOME_EXCEEDED"
	cityOpenWorldFreightSettlementRejectionTransportMismatch  = "CITY_FREIGHT_SETTLEMENT_TRANSPORT_MISMATCH"
	cityOpenWorldFreightSettlementRejectionSourceState        = "CITY_FREIGHT_SETTLEMENT_SOURCE_STATE_INVALID"
	cityOpenWorldFreightSettlementRejectionSettlementRequired = "CITY_SUPPLY_CHAIN_SETTLEMENT_REQUIRED"
)

// cityOpenWorldFreightSettlementReceiptLinePayload records only a delta. The
// case projection remains append-only and derives a line's final outcome from
// every accepted/lost/rejected delta rather than allowing a mutable overwrite.
type cityOpenWorldFreightSettlementReceiptLinePayload struct {
	SourceLineNo  int   `json:"source_line_no"`
	AcceptedUnits int64 `json:"accepted_units"`
	LostUnits     int64 `json:"lost_units"`
	RejectedUnits int64 `json:"rejected_units"`
}

type cityOpenWorldFreightSettlementReceiptPayload struct {
	CaseCode       string                                             `json:"case_code"`
	LiabilityParty string                                             `json:"liability_party"`
	Lines          []cityOpenWorldFreightSettlementReceiptLinePayload `json:"lines"`
}

type cityOpenWorldFreightSettlementBusinessError struct{ code string }

func (err *cityOpenWorldFreightSettlementBusinessError) Error() string { return err.code }

func cityOpenWorldFreightSettlementReject(code string) error {
	return &cityOpenWorldFreightSettlementBusinessError{code: code}
}

func cityOpenWorldFreightSettlementBusinessRejectionCode(err error) string {
	var businessErr *cityOpenWorldFreightSettlementBusinessError
	if errors.As(err, &businessErr) {
		return businessErr.code
	}
	if code := cityLedgerBusinessRejectionCode(err); code != "" {
		return code
	}
	return cityResourceBusinessRejectionCode(err)
}

// Receipt acknowledgement is intentionally buyer-scoped for ordinary world
// members. A seller can inspect its own order through existing views, but may
// not decide another firm's accepted/lost/rejected outcome; a world owner and
// system administrator retain their normal management authority.
func authorizeCityOpenWorldFreightSettlementCommandSubmission(
	ctx context.Context,
	queryer citySQLQueryer,
	world *lockedCityWorld,
	userID, worldID int64,
	commandType string,
	payload json.RawMessage,
) error {
	if world == nil || userID <= 0 || worldID <= 0 ||
		commandType != CityCommandTypeOpenWorldFreightSettlementReceipt {
		return ErrCityPermissionDenied
	}
	if world.memberRole == CityMemberRoleOwner {
		return nil
	}
	value := cityOpenWorldFreightSettlementReceiptPayload{}
	if err := json.Unmarshal(payload, &value); err != nil {
		return ErrCityInvalidInput.WithCause(err)
	}
	value.CaseCode = strings.ToLower(strings.TrimSpace(value.CaseCode))
	if !cityOpenWorldSupplyChainCodeValid(value.CaseCode) {
		return ErrCityInvalidInput.WithMetadata(map[string]string{"field": "case_code"})
	}
	var allowed bool
	err := queryer.QueryRowContext(ctx, `
SELECT EXISTS (
    SELECT 1
    FROM city_open_world_freight_settlement_cases settlement_case
    JOIN city_open_world_freight_settlement_orders settlement_order
      ON settlement_order.world_id = settlement_case.world_id
     AND settlement_order.code = settlement_case.settlement_order_code
    JOIN city_open_world_supply_chain_orders supply_order
      ON supply_order.world_id = settlement_order.world_id
     AND supply_order.code = settlement_order.order_code
    JOIN city_open_world_supply_chain_nodes buyer_node
      ON buyer_node.world_id = supply_order.world_id
     AND buyer_node.code = supply_order.buyer_node_code
    JOIN city_economic_entities buyer_firm
      ON buyer_firm.id = buyer_node.firm_entity_id
     AND buyer_firm.world_id = buyer_node.world_id
    WHERE settlement_case.world_id = $1
      AND settlement_case.code = $2
      AND buyer_node.state = 'active'
      AND buyer_firm.entity_type = 'firm'
      AND buyer_firm.status = 'active'
      AND buyer_firm.owner_user_id = $3
)`, worldID, value.CaseCode, userID).Scan(&allowed)
	if err != nil {
		return fmt.Errorf("authorize V22 freight-settlement receipt ownership: %w", err)
	}
	if !allowed {
		return ErrCityPermissionDenied
	}
	return nil
}

func normalizeCityOpenWorldFreightSettlementCommand(commandType string, rawPayload json.RawMessage) (any, bool, error) {
	if commandType != CityCommandTypeOpenWorldFreightSettlementReceipt {
		return nil, false, nil
	}
	value := cityOpenWorldFreightSettlementReceiptPayload{}
	if err := decodeStrictCityObject(rawPayload, &value); err != nil {
		return nil, true, ErrCityInvalidInput.WithCause(err)
	}
	value.CaseCode = strings.ToLower(strings.TrimSpace(value.CaseCode))
	value.LiabilityParty = strings.ToLower(strings.TrimSpace(value.LiabilityParty))
	if !cityOpenWorldSupplyChainCodeValid(value.CaseCode) ||
		(value.LiabilityParty != cityOpenWorldFreightSettlementLiabilitySeller &&
			value.LiabilityParty != cityOpenWorldFreightSettlementLiabilityCarrier) ||
		len(value.Lines) == 0 || len(value.Lines) > cityOpenWorldSupplyChainMaximumOrderLines {
		return nil, true, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "freight_settlement_receipt"})
	}
	seen := make(map[int]struct{}, len(value.Lines))
	for index := range value.Lines {
		line := &value.Lines[index]
		if line.SourceLineNo <= 0 || line.AcceptedUnits < 0 || line.LostUnits < 0 || line.RejectedUnits < 0 ||
			line.AcceptedUnits > cityMaximumResourceUnits || line.LostUnits > cityMaximumResourceUnits ||
			line.RejectedUnits > cityMaximumResourceUnits ||
			line.AcceptedUnits > math.MaxInt64-line.LostUnits ||
			line.AcceptedUnits+line.LostUnits > math.MaxInt64-line.RejectedUnits ||
			line.AcceptedUnits+line.LostUnits+line.RejectedUnits == 0 {
			return nil, true, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "lines"})
		}
		if _, exists := seen[line.SourceLineNo]; exists {
			return nil, true, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "lines"})
		}
		seen[line.SourceLineNo] = struct{}{}
	}
	sort.Slice(value.Lines, func(i, j int) bool { return value.Lines[i].SourceLineNo < value.Lines[j].SourceLineNo })
	return value, true, nil
}

type cityOpenWorldFreightSettlementExecution struct {
	pending                cityPendingEvent
	facts                  []CityOpenWorldSupplyChainFact
	nextFactSequence       int64
	nextJournalSequence    int64
	nextResourceOpSequence int64
}

type cityOpenWorldFreightSettlementReceiptTotals struct {
	accepted int64
	lost     int64
	rejected int64
}

func (s *CityEconomyService) applyCityOpenWorldFreightSettlementCommand(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, journalSequence, resourceOperationSequence int64,
	ledgerUnit *cityLedgerBaseUnit,
	command *CityCommand,
) (cityOpenWorldFreightSettlementExecution, error) {
	const savepoint = "city_open_world_freight_settlement_command"
	if _, err := tx.ExecContext(ctx, `SAVEPOINT `+savepoint); err != nil {
		return cityOpenWorldFreightSettlementExecution{}, fmt.Errorf("create V22 freight-settlement command savepoint: %w", err)
	}
	execution, err := s.postCityOpenWorldFreightSettlementCommand(
		ctx, tx, worldID, targetTick, factSequence, journalSequence, resourceOperationSequence, ledgerUnit, command,
	)
	if err != nil {
		if _, rollbackErr := tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT `+savepoint); rollbackErr != nil {
			return cityOpenWorldFreightSettlementExecution{}, fmt.Errorf("rollback V22 freight-settlement command after %v: %w", err, rollbackErr)
		}
		if _, releaseErr := tx.ExecContext(ctx, `RELEASE SAVEPOINT `+savepoint); releaseErr != nil {
			return cityOpenWorldFreightSettlementExecution{}, fmt.Errorf("release rejected V22 freight-settlement command: %w", releaseErr)
		}
		if code := cityOpenWorldFreightSettlementBusinessRejectionCode(err); code != "" {
			return cityOpenWorldFreightSettlementExecution{
				pending: rejectedCityCommand(command, code), nextFactSequence: factSequence,
				nextJournalSequence: journalSequence, nextResourceOpSequence: resourceOperationSequence,
			}, nil
		}
		return cityOpenWorldFreightSettlementExecution{}, err
	}
	if _, err = tx.ExecContext(ctx, `RELEASE SAVEPOINT `+savepoint); err != nil {
		return cityOpenWorldFreightSettlementExecution{}, fmt.Errorf("release V22 freight-settlement command savepoint: %w", err)
	}
	return execution, nil
}

func (s *CityEconomyService) postCityOpenWorldFreightSettlementCommand(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, journalSequence, resourceOperationSequence int64,
	ledgerUnit *cityLedgerBaseUnit,
	command *CityCommand,
) (cityOpenWorldFreightSettlementExecution, error) {
	if command == nil || command.ID <= 0 || command.Sequence <= 0 || ledgerUnit == nil || targetTick <= 0 ||
		factSequence <= 0 || journalSequence <= 0 || resourceOperationSequence <= 0 {
		return cityOpenWorldFreightSettlementExecution{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v22_freight_settlement_command"})
	}
	if err := ensureCityOpenWorldFreightSettlementEngine(ctx, tx, worldID); err != nil {
		return cityOpenWorldFreightSettlementExecution{}, err
	}
	if err := activateCityOpenWorldFreightSettlementWrite(ctx, tx, worldID); err != nil {
		return cityOpenWorldFreightSettlementExecution{}, err
	}
	if err := activateCityOpenWorldSupplyChainWrite(ctx, tx, worldID); err != nil {
		return cityOpenWorldFreightSettlementExecution{}, err
	}
	if err := activateCityOpenWorldEnterpriseFreightReceiptWrite(ctx, tx, worldID); err != nil {
		return cityOpenWorldFreightSettlementExecution{}, err
	}
	if err := activateCityOpenWorldFreightBatchWrite(ctx, tx, worldID); err != nil {
		return cityOpenWorldFreightSettlementExecution{}, err
	}
	policy, err := loadCityOpenWorldFreightSettlementPolicyForUpdate(ctx, tx, worldID)
	if err != nil {
		return cityOpenWorldFreightSettlementExecution{}, err
	}
	if command.CommandType != CityCommandTypeOpenWorldFreightSettlementReceipt {
		return cityOpenWorldFreightSettlementExecution{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"command_type": command.CommandType})
	}
	payload, err := decodeStoredCityCommandPayload[cityOpenWorldFreightSettlementReceiptPayload](command)
	if err != nil {
		return cityOpenWorldFreightSettlementExecution{}, err
	}
	return s.recordCityOpenWorldFreightSettlementReceipt(
		ctx, tx, worldID, targetTick, factSequence, journalSequence, resourceOperationSequence,
		ledgerUnit, policy, command, payload,
	)
}

func loadCityOpenWorldFreightSettlementOrderForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	code string,
) (*cityOpenWorldFreightSettlementOrderRecord, error) {
	record := &cityOpenWorldFreightSettlementOrderRecord{}
	err := tx.QueryRowContext(ctx, `
SELECT code, source_kind, source_code, order_code, source_tick, state, version
FROM city_open_world_freight_settlement_orders
WHERE world_id = $1 AND code = $2
FOR UPDATE`, worldID, code).Scan(
		&record.code, &record.sourceKind, &record.sourceCode, &record.orderCode,
		&record.sourceTick, &record.state, &record.version,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, cityOpenWorldFreightSettlementReject(cityOpenWorldFreightSettlementRejectionCaseNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("lock V22 freight-settlement order: %w", err)
	}
	return record, nil
}

func loadCityOpenWorldFreightSettlementCaseForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	code string,
) (*cityOpenWorldFreightSettlementCaseRecord, error) {
	record := &cityOpenWorldFreightSettlementCaseRecord{}
	err := tx.QueryRowContext(ctx, `
SELECT code, settlement_order_code, source_kind, source_code, transport_state,
       state, source_tick, version
FROM city_open_world_freight_settlement_cases
WHERE world_id = $1 AND code = $2
FOR UPDATE`, worldID, code).Scan(
		&record.code, &record.settlementOrderCode, &record.sourceKind, &record.sourceCode,
		&record.transportState, &record.state, &record.sourceTick, &record.version,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, cityOpenWorldFreightSettlementReject(cityOpenWorldFreightSettlementRejectionCaseNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("lock V22 freight-settlement case: %w", err)
	}
	return record, nil
}

func loadCityOpenWorldFreightSettlementCaseLinesForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	caseCode string,
) ([]cityOpenWorldFreightSettlementLineRecord, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT source_line_no, resource_code, source_firm_code, source_district_code,
       destination_firm_code, destination_district_code, quantity_units,
       unit_price_units, total_price_units
FROM city_open_world_freight_settlement_case_lines
WHERE world_id = $1 AND case_code = $2
ORDER BY source_line_no
FOR UPDATE`, worldID, caseCode)
	if err != nil {
		return nil, fmt.Errorf("lock V22 freight-settlement case lines: %w", err)
	}
	lines := make([]cityOpenWorldFreightSettlementLineRecord, 0)
	for rows.Next() {
		line := cityOpenWorldFreightSettlementLineRecord{}
		if err = rows.Scan(&line.sourceLineNo, &line.resourceCode, &line.sourceFirmCode,
			&line.sourceDistrictCode, &line.destinationFirmCode, &line.destinationDistrictCode,
			&line.quantityUnits, &line.unitPriceUnits, &line.totalPriceUnits); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan V22 freight-settlement case line: %w", err)
		}
		lines = append(lines, line)
	}
	if err = closeCityRows(rows, "iterate V22 freight-settlement case lines"); err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v22_freight_settlement_case_lines"})
	}
	return lines, nil
}

func loadCityOpenWorldFreightSettlementReceiptTotals(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	caseCode string,
) (map[int]cityOpenWorldFreightSettlementReceiptTotals, int64, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT line.source_line_no,
       COALESCE(SUM(line.accepted_units), 0)::BIGINT,
       COALESCE(SUM(line.lost_units), 0)::BIGINT,
       COALESCE(SUM(line.rejected_units), 0)::BIGINT
FROM city_open_world_freight_settlement_receipt_lines line
JOIN city_open_world_freight_settlement_receipts receipt
  ON receipt.world_id = line.world_id AND receipt.code = line.receipt_code
WHERE line.world_id = $1 AND line.case_code = $2
GROUP BY line.source_line_no
ORDER BY line.source_line_no`, worldID, caseCode)
	if err != nil {
		return nil, 0, fmt.Errorf("load V22 freight-settlement receipt totals: %w", err)
	}
	totals := make(map[int]cityOpenWorldFreightSettlementReceiptTotals)
	for rows.Next() {
		lineNo := 0
		item := cityOpenWorldFreightSettlementReceiptTotals{}
		if err = rows.Scan(&lineNo, &item.accepted, &item.lost, &item.rejected); err != nil {
			_ = rows.Close()
			return nil, 0, fmt.Errorf("scan V22 freight-settlement receipt total: %w", err)
		}
		totals[lineNo] = item
	}
	if err = closeCityRows(rows, "iterate V22 freight-settlement receipt totals"); err != nil {
		return nil, 0, err
	}
	var receipts int64
	if err = tx.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM city_open_world_freight_settlement_receipts
WHERE world_id = $1 AND case_code = $2`, worldID, caseCode).Scan(&receipts); err != nil {
		return nil, 0, fmt.Errorf("count V22 freight-settlement case receipts: %w", err)
	}
	return totals, receipts, nil
}

func assertCityOpenWorldFreightSettlementCaseTransport(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	settlementOrder *cityOpenWorldFreightSettlementOrderRecord,
	settlementCase *cityOpenWorldFreightSettlementCaseRecord,
) error {
	if settlementOrder == nil || settlementCase == nil ||
		settlementOrder.code != settlementCase.settlementOrderCode ||
		settlementOrder.orderCode == "" || settlementCase.sourceKind != settlementOrder.sourceKind {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v22_freight_settlement_source"})
	}
	var sourceState, sourceOrderCode string
	switch settlementCase.sourceKind {
	case cityOpenWorldFreightSettlementSourceShipment:
		if settlementOrder.sourceCode != settlementCase.sourceCode {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v22_freight_settlement_shipment_source"})
		}
		err := tx.QueryRowContext(ctx, `
SELECT state, order_code
FROM city_open_world_enterprise_freight_shipments
WHERE world_id = $1 AND code = $2
FOR UPDATE`, worldID, settlementCase.sourceCode).Scan(&sourceState, &sourceOrderCode)
		if errors.Is(err, sql.ErrNoRows) {
			return cityOpenWorldFreightSettlementReject(cityOpenWorldFreightSettlementRejectionTransportMismatch)
		}
		if err != nil {
			return fmt.Errorf("lock V22 freight-settlement shipment source: %w", err)
		}
	case cityOpenWorldFreightSettlementSourceConsignment:
		err := tx.QueryRowContext(ctx, `
SELECT consignment.state, plan.order_code
FROM city_open_world_freight_batch_consignments consignment
JOIN city_open_world_freight_batch_plans plan
  ON plan.world_id = consignment.world_id AND plan.code = consignment.plan_code
WHERE consignment.world_id = $1 AND consignment.code = $2
FOR UPDATE OF consignment, plan`, worldID, settlementCase.sourceCode).Scan(&sourceState, &sourceOrderCode)
		if errors.Is(err, sql.ErrNoRows) {
			return cityOpenWorldFreightSettlementReject(cityOpenWorldFreightSettlementRejectionTransportMismatch)
		}
		if err != nil {
			return fmt.Errorf("lock V22 freight-settlement consignment source: %w", err)
		}
	default:
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v22_freight_settlement_source_kind"})
	}
	if sourceOrderCode != settlementOrder.orderCode || sourceState != settlementCase.transportState ||
		!cityOpenWorldFreightSettlementActionableTransportState(sourceState) {
		return cityOpenWorldFreightSettlementReject(cityOpenWorldFreightSettlementRejectionTransportMismatch)
	}
	return nil
}

func cityOpenWorldFreightSettlementLineOutcomeTotal(value cityOpenWorldFreightSettlementReceiptTotals) (int64, error) {
	if value.accepted < 0 || value.lost < 0 || value.rejected < 0 ||
		value.accepted > math.MaxInt64-value.lost || value.accepted+value.lost > math.MaxInt64-value.rejected {
		return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v22_freight_settlement_line_total"})
	}
	return value.accepted + value.lost + value.rejected, nil
}

func cityOpenWorldFreightSettlementPending(
	command *CityCommand,
	caseCode, orderCode string,
	settled bool,
	journal *CityJournal,
	operation *CityResourceOperation,
) cityPendingEvent {
	payload := map[string]any{"case_code": caseCode, "settlement_order_code": orderCode, "settled": settled}
	result := map[string]any{"applied": true, "case_code": caseCode, "settlement_order_code": orderCode, "settled": settled}
	if journal != nil {
		payload["journal_tick"] = journal.Tick
		payload["journal_sequence"] = journal.Sequence
		result["journal_tick"] = journal.Tick
		result["journal_sequence"] = journal.Sequence
	}
	if operation != nil {
		payload["resource_operation_tick"] = operation.Tick
		payload["resource_operation_sequence"] = operation.Sequence
		result["resource_operation_tick"] = operation.Tick
		result["resource_operation_sequence"] = operation.Sequence
	}
	return cityPendingEvent{
		command: command, status: CityCommandStatusApplied,
		eventType: "city.open_world.freight_settlement.receipt",
		payload:   payload, result: result,
	}
}

func (s *CityEconomyService) recordCityOpenWorldFreightSettlementReceipt(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, journalSequence, resourceOperationSequence int64,
	ledgerUnit *cityLedgerBaseUnit,
	policy *CityOpenWorldFreightSettlementPolicy,
	command *CityCommand,
	payload cityOpenWorldFreightSettlementReceiptPayload,
) (cityOpenWorldFreightSettlementExecution, error) {
	if policy == nil || command == nil || ledgerUnit == nil {
		return cityOpenWorldFreightSettlementExecution{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v22_freight_settlement_receipt"})
	}
	var receiptsThisTick int64
	if err := tx.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM city_open_world_freight_settlement_receipts
WHERE world_id = $1 AND receipt_tick = $2`, worldID, targetTick).Scan(&receiptsThisTick); err != nil {
		return cityOpenWorldFreightSettlementExecution{}, fmt.Errorf("count V22 freight-settlement tick receipts: %w", err)
	}
	if receiptsThisTick >= int64(policy.MaximumReceiptsPerTick) {
		return cityOpenWorldFreightSettlementExecution{}, cityOpenWorldFreightSettlementReject(cityOpenWorldFreightSettlementRejectionReceiptLimit)
	}
	settlementCase, err := loadCityOpenWorldFreightSettlementCaseForUpdate(ctx, tx, worldID, payload.CaseCode)
	if err != nil {
		return cityOpenWorldFreightSettlementExecution{}, err
	}
	if settlementCase.state == cityOpenWorldFreightSettlementCaseSettled ||
		(settlementCase.state != cityOpenWorldFreightSettlementCaseAwaiting && settlementCase.state != cityOpenWorldFreightSettlementCaseReceiving) {
		return cityOpenWorldFreightSettlementExecution{}, cityOpenWorldFreightSettlementReject(cityOpenWorldFreightSettlementRejectionCaseNotActionable)
	}
	settlementOrder, err := loadCityOpenWorldFreightSettlementOrderForUpdate(ctx, tx, worldID, settlementCase.settlementOrderCode)
	if err != nil {
		return cityOpenWorldFreightSettlementExecution{}, err
	}
	if settlementOrder.state == cityOpenWorldFreightSettlementOrderSettled ||
		(settlementOrder.state != cityOpenWorldFreightSettlementOrderAwaiting && settlementOrder.state != cityOpenWorldFreightSettlementOrderReceiving) {
		return cityOpenWorldFreightSettlementExecution{}, cityOpenWorldFreightSettlementReject(cityOpenWorldFreightSettlementRejectionCaseNotActionable)
	}
	if err = assertCityOpenWorldFreightSettlementCaseTransport(ctx, tx, worldID, settlementOrder, settlementCase); err != nil {
		return cityOpenWorldFreightSettlementExecution{}, err
	}
	caseLines, err := loadCityOpenWorldFreightSettlementCaseLinesForUpdate(ctx, tx, worldID, settlementCase.code)
	if err != nil {
		return cityOpenWorldFreightSettlementExecution{}, err
	}
	history, receiptCount, err := loadCityOpenWorldFreightSettlementReceiptTotals(ctx, tx, worldID, settlementCase.code)
	if err != nil {
		return cityOpenWorldFreightSettlementExecution{}, err
	}
	if receiptCount >= int64(policy.MaximumReceiptsPerCase) {
		return cityOpenWorldFreightSettlementExecution{}, cityOpenWorldFreightSettlementReject(cityOpenWorldFreightSettlementRejectionReceiptLimit)
	}
	lineByNo := make(map[int]cityOpenWorldFreightSettlementLineRecord, len(caseLines))
	for _, line := range caseLines {
		lineByNo[line.sourceLineNo] = line
	}
	var acceptedTotal, lostTotal, rejectedTotal, refundAmount int64
	for _, delta := range payload.Lines {
		line, exists := lineByNo[delta.SourceLineNo]
		if !exists {
			return cityOpenWorldFreightSettlementExecution{}, cityOpenWorldFreightSettlementReject(cityOpenWorldFreightSettlementRejectionOutcomeExceeded)
		}
		if settlementCase.transportState != cityOpenWorldEnterpriseFreightReceiptStateAwaitingReceipt && delta.AcceptedUnits != 0 {
			return cityOpenWorldFreightSettlementExecution{}, cityOpenWorldFreightSettlementReject(cityOpenWorldFreightSettlementRejectionSourceState)
		}
		current := history[delta.SourceLineNo]
		if current.accepted > math.MaxInt64-delta.AcceptedUnits || current.lost > math.MaxInt64-delta.LostUnits ||
			current.rejected > math.MaxInt64-delta.RejectedUnits {
			return cityOpenWorldFreightSettlementExecution{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v22_freight_settlement_outcome_overflow"})
		}
		next := cityOpenWorldFreightSettlementReceiptTotals{
			accepted: current.accepted + delta.AcceptedUnits,
			lost:     current.lost + delta.LostUnits,
			rejected: current.rejected + delta.RejectedUnits,
		}
		outcomeTotal, totalErr := cityOpenWorldFreightSettlementLineOutcomeTotal(next)
		if totalErr != nil {
			return cityOpenWorldFreightSettlementExecution{}, totalErr
		}
		if outcomeTotal > line.quantityUnits {
			return cityOpenWorldFreightSettlementExecution{}, cityOpenWorldFreightSettlementReject(cityOpenWorldFreightSettlementRejectionOutcomeExceeded)
		}
		history[delta.SourceLineNo] = next
		if acceptedTotal > math.MaxInt64-delta.AcceptedUnits || lostTotal > math.MaxInt64-delta.LostUnits ||
			rejectedTotal > math.MaxInt64-delta.RejectedUnits {
			return cityOpenWorldFreightSettlementExecution{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v22_freight_settlement_total_overflow"})
		}
		acceptedTotal += delta.AcceptedUnits
		lostTotal += delta.LostUnits
		rejectedTotal += delta.RejectedUnits
		unfulfilled := delta.LostUnits + delta.RejectedUnits
		if unfulfilled > 0 {
			if unfulfilled > math.MaxInt64/line.unitPriceUnits || refundAmount > math.MaxInt64-unfulfilled*line.unitPriceUnits {
				return cityOpenWorldFreightSettlementExecution{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v22_freight_settlement_refund_overflow"})
			}
			refundAmount += unfulfilled * line.unitPriceUnits
		}
	}

	allLinesResolved := true
	for _, line := range caseLines {
		total, totalErr := cityOpenWorldFreightSettlementLineOutcomeTotal(history[line.sourceLineNo])
		if totalErr != nil {
			return cityOpenWorldFreightSettlementExecution{}, totalErr
		}
		if total != line.quantityUnits {
			allLinesResolved = false
			break
		}
	}

	supplyOrder, err := loadCityOpenWorldSupplyChainOrderForUpdate(ctx, tx, worldID, settlementOrder.orderCode)
	if err != nil {
		return cityOpenWorldFreightSettlementExecution{}, err
	}
	if supplyOrder.state != cityOpenWorldSupplyChainStateDispatched {
		return cityOpenWorldFreightSettlementExecution{}, cityOpenWorldFreightSettlementReject(cityOpenWorldFreightSettlementRejectionSourceState)
	}
	supplyLines, err := loadCityOpenWorldSupplyChainLinesForUpdate(ctx, tx, worldID, supplyOrder.code)
	if err != nil {
		return cityOpenWorldFreightSettlementExecution{}, err
	}
	supplyLineByNo := make(map[int]cityOpenWorldSupplyChainLineRecord, len(supplyLines))
	for _, line := range supplyLines {
		supplyLineByNo[line.lineNo] = line
	}
	for _, line := range caseLines {
		supplyLine, exists := supplyLineByNo[line.sourceLineNo]
		if !exists || supplyLine.resourceCode != line.resourceCode || supplyLine.unitPriceUnits != line.unitPriceUnits ||
			line.quantityUnits > supplyLine.quantityUnits {
			return cityOpenWorldFreightSettlementExecution{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v22_freight_settlement_supply_line"})
		}
	}

	seller, err := loadCityOpenWorldSupplyChainNodeForUpdate(ctx, tx, worldID, supplyOrder.sellerNodeCode)
	if err != nil {
		return cityOpenWorldFreightSettlementExecution{}, err
	}
	buyer, err := loadCityOpenWorldSupplyChainNodeForUpdate(ctx, tx, worldID, supplyOrder.buyerNodeCode)
	if err != nil {
		return cityOpenWorldFreightSettlementExecution{}, err
	}

	var operation *CityResourceOperation
	if acceptedTotal > 0 || lostTotal > 0 {
		operationLines := make([]cityResourcePostingLine, 0, len(payload.Lines)*2)
		for _, delta := range payload.Lines {
			if delta.AcceptedUnits == 0 && delta.LostUnits == 0 {
				continue
			}
			supplyLine := supplyLineByNo[delta.SourceLineNo]
			source, sourceErr := loadCityInventoryRefByID(ctx, tx, worldID, supplyLine.sourceBalanceID)
			if sourceErr != nil {
				return cityOpenWorldFreightSettlementExecution{}, sourceErr
			}
			if source.resourceID != supplyLine.resourceID || source.entityID != seller.firmID || source.districtID != seller.districtID {
				return cityOpenWorldFreightSettlementExecution{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v22_freight_settlement_source_balance"})
			}
			outbound := delta.AcceptedUnits + delta.LostUnits
			operationLines = append(operationLines, cityResourcePostingLine{
				balance: source, direction: "out", quantityUnits: outbound,
				memo: "Freight settlement outbound " + settlementCase.code,
			})
			if delta.AcceptedUnits > 0 {
				destination, destinationErr := loadCityInventoryRefByID(ctx, tx, worldID, supplyLine.destinationBalanceID)
				if destinationErr != nil {
					return cityOpenWorldFreightSettlementExecution{}, destinationErr
				}
				if destination.resourceID != supplyLine.resourceID || destination.entityID != buyer.firmID || destination.districtID != buyer.districtID {
					return cityOpenWorldFreightSettlementExecution{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v22_freight_settlement_destination_balance"})
				}
				operationLines = append(operationLines, cityResourcePostingLine{
					balance: destination, direction: "in", quantityUnits: delta.AcceptedUnits,
					memo: "Freight settlement accepted " + settlementCase.code,
				})
			}
		}
		operation, err = postCityResourceOperation(ctx, tx, cityResourceOperationSpec{
			worldID: worldID, tick: targetTick, sequence: resourceOperationSequence,
			operationKey:  "freight:settlement:" + settlementCase.code + ":" + fmt.Sprintf("%d", command.Sequence),
			operationType: "freight_settlement", sourceCommandID: &command.ID,
			actorEntityID: seller.firmID, districtID: seller.districtID,
			description: "Freight settlement receipt " + settlementCase.code,
			metadata: map[string]any{
				"schema_version": cityOpenWorldFreightSettlementSchemaVersion,
				"case_code":      settlementCase.code, "settlement_order_code": settlementOrder.code,
				"accepted_units": acceptedTotal, "lost_units": lostTotal, "rejected_units": rejectedTotal,
			},
			lines: operationLines,
		})
		if err != nil {
			return cityOpenWorldFreightSettlementExecution{}, err
		}
	}

	var journal *CityJournal
	if refundAmount > 0 {
		buyerInventory, accountErr := loadCityLedgerAccount(ctx, tx, worldID, ledgerUnit.id, buyer.firmID, CityEntityTypeFirm, "inventory")
		if accountErr != nil {
			return cityOpenWorldFreightSettlementExecution{}, accountErr
		}
		buyerCash, accountErr := loadCityLedgerAccount(ctx, tx, worldID, ledgerUnit.id, buyer.firmID, CityEntityTypeFirm, "cash")
		if accountErr != nil {
			return cityOpenWorldFreightSettlementExecution{}, accountErr
		}
		sellerCash, accountErr := loadCityLedgerAccount(ctx, tx, worldID, ledgerUnit.id, seller.firmID, CityEntityTypeFirm, "cash")
		if accountErr != nil {
			return cityOpenWorldFreightSettlementExecution{}, accountErr
		}
		sellerRevenue, accountErr := loadCityLedgerAccount(ctx, tx, worldID, ledgerUnit.id, seller.firmID, CityEntityTypeFirm, "revenue")
		if accountErr != nil {
			return cityOpenWorldFreightSettlementExecution{}, accountErr
		}
		journal, err = postCityJournal(ctx, tx, cityLedgerJournalSpec{
			worldID: worldID, unit: ledgerUnit, tick: targetTick, sequence: journalSequence,
			operationKey: "freight:refund:" + settlementCase.code + ":" + fmt.Sprintf("%d", command.Sequence),
			journalType:  "freight_refund", sourceCommandID: &command.ID,
			description: "Freight outcome refund " + settlementCase.code,
			metadata: map[string]any{
				"schema_version": cityOpenWorldFreightSettlementSchemaVersion,
				"case_code":      settlementCase.code, "settlement_order_code": settlementOrder.code,
				"liability_party": payload.LiabilityParty, "refund_amount_units": refundAmount,
			},
			lines: []cityLedgerPostingLine{
				{account: buyerCash, debitUnits: refundAmount, memo: "Freight refund " + settlementCase.code},
				{account: sellerRevenue, debitUnits: refundAmount, memo: "Freight revenue correction " + settlementCase.code},
				{account: sellerCash, creditUnits: refundAmount, memo: "Freight refund paid " + settlementCase.code},
				{account: buyerInventory, creditUnits: refundAmount, memo: "Freight inventory correction " + settlementCase.code},
			},
		})
		if err != nil {
			return cityOpenWorldFreightSettlementExecution{}, err
		}
	}

	receiptCode := cityOpenWorldFreightSettlementReceiptCode(command.Sequence)
	metadata, err := json.Marshal(map[string]any{
		"schema_version": cityOpenWorldFreightSettlementSchemaVersion,
		"accepted_units": acceptedTotal, "lost_units": lostTotal, "rejected_units": rejectedTotal,
		"refund_amount_units": refundAmount,
	})
	if err != nil {
		return cityOpenWorldFreightSettlementExecution{}, fmt.Errorf("marshal V22 freight-settlement receipt metadata: %w", err)
	}
	var resourceOperationID, journalID any
	if operation != nil {
		resourceOperationID = operation.ID
	}
	if journal != nil {
		journalID = journal.ID
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_freight_settlement_receipts
    (world_id, code, case_code, receipt_tick, source_command_id, liability_party,
     refunded_units, resource_operation_id, journal_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb)`,
		worldID, receiptCode, settlementCase.code, targetTick, command.ID, payload.LiabilityParty,
		refundAmount, resourceOperationID, journalID, []byte(metadata)); err != nil {
		return cityOpenWorldFreightSettlementExecution{}, fmt.Errorf("insert V22 freight-settlement receipt: %w", err)
	}
	for _, delta := range payload.Lines {
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_freight_settlement_receipt_lines
    (world_id, receipt_code, case_code, source_line_no, accepted_units, lost_units, rejected_units)
VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			worldID, receiptCode, settlementCase.code, delta.SourceLineNo,
			delta.AcceptedUnits, delta.LostUnits, delta.RejectedUnits); err != nil {
			return cityOpenWorldFreightSettlementExecution{}, fmt.Errorf("insert V22 freight-settlement receipt line: %w", err)
		}
	}
	claimCount := int64(0)
	if payload.LiabilityParty == cityOpenWorldFreightSettlementLiabilityCarrier && refundAmount > 0 {
		claimMetadata, claimErr := json.Marshal(map[string]any{
			"schema_version": cityOpenWorldFreightSettlementSchemaVersion,
			"origin":         "carrier_liability_after_seller_refund",
		})
		if claimErr != nil {
			return cityOpenWorldFreightSettlementExecution{}, fmt.Errorf("marshal V22 freight-settlement claim metadata: %w", claimErr)
		}
		if _, claimErr = tx.ExecContext(ctx, `
INSERT INTO city_open_world_freight_settlement_claims
    (world_id, code, receipt_code, case_code, liability_party, claim_amount,
     state, created_tick, metadata)
VALUES ($1, $2, $3, $4, 'carrier', $5, 'open', $6, $7::jsonb)`,
			worldID, cityOpenWorldFreightSettlementClaimCode(receiptCode), receiptCode,
			settlementCase.code, refundAmount, targetTick, []byte(claimMetadata)); claimErr != nil {
			return cityOpenWorldFreightSettlementExecution{}, fmt.Errorf("insert V22 freight-settlement carrier claim: %w", claimErr)
		}
		claimCount = 1
	}

	nextCaseState := cityOpenWorldFreightSettlementCaseReceiving
	if allLinesResolved {
		nextCaseState = cityOpenWorldFreightSettlementCaseSettled
	}
	if nextCaseState != settlementCase.state {
		result, updateErr := tx.ExecContext(ctx, `
UPDATE city_open_world_freight_settlement_cases
SET state = $3, version = version + 1, updated_at = NOW()
WHERE world_id = $1 AND code = $2 AND state = $4`, worldID, settlementCase.code, nextCaseState, settlementCase.state)
		if updateErr != nil {
			return cityOpenWorldFreightSettlementExecution{}, fmt.Errorf("update V22 freight-settlement case state: %w", updateErr)
		}
		if rows, rowsErr := result.RowsAffected(); rowsErr != nil || rows != 1 {
			return cityOpenWorldFreightSettlementExecution{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v22_freight_settlement_case_state"})
		}
		settlementCase.state = nextCaseState
		settlementCase.version++
	}
	if settlementOrder.state == cityOpenWorldFreightSettlementOrderAwaiting {
		result, updateErr := tx.ExecContext(ctx, `
UPDATE city_open_world_freight_settlement_orders
SET state = 'receiving', version = version + 1, updated_at = NOW()
WHERE world_id = $1 AND code = $2 AND state = 'awaiting_transport'`, worldID, settlementOrder.code)
		if updateErr != nil {
			return cityOpenWorldFreightSettlementExecution{}, fmt.Errorf("advance V22 freight-settlement order state: %w", updateErr)
		}
		if rows, rowsErr := result.RowsAffected(); rowsErr != nil || rows != 1 {
			return cityOpenWorldFreightSettlementExecution{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v22_freight_settlement_order_state"})
		}
		settlementOrder.state = cityOpenWorldFreightSettlementOrderReceiving
		settlementOrder.version++
	}
	if err = updateCityOpenWorldFreightSettlementPolicy(ctx, tx, worldID, cityOpenWorldFreightSettlementPolicyDelta{
		receipts: 1, claims: claimCount, accepted: acceptedTotal, lost: lostTotal,
		rejected: rejectedTotal, refunded: refundAmount,
	}); err != nil {
		return cityOpenWorldFreightSettlementExecution{}, err
	}

	settledOrder, err := cityOpenWorldFreightSettlementOrderReadyToSettle(ctx, tx, worldID, settlementOrder)
	if err != nil {
		return cityOpenWorldFreightSettlementExecution{}, err
	}
	execution := cityOpenWorldFreightSettlementExecution{
		pending:          cityOpenWorldFreightSettlementPending(command, settlementCase.code, settlementOrder.code, settledOrder, journal, operation),
		nextFactSequence: factSequence, nextJournalSequence: journalSequence, nextResourceOpSequence: resourceOperationSequence,
	}
	if journal != nil {
		execution.nextJournalSequence++
	}
	if operation != nil {
		execution.nextResourceOpSequence++
	}
	if settledOrder {
		finalExecution, finalErr := s.finalizeCityOpenWorldFreightSettlementOrder(
			ctx, tx, worldID, targetTick, factSequence, settlementOrder, ledgerUnit, command,
		)
		if finalErr != nil {
			return cityOpenWorldFreightSettlementExecution{}, finalErr
		}
		execution.facts = finalExecution.facts
		execution.nextFactSequence = finalExecution.nextFactSequence
	}
	if err = assertCityOpenWorldFreightSettlementFoundation(ctx, tx, worldID); err != nil {
		return cityOpenWorldFreightSettlementExecution{}, err
	}
	return execution, nil
}

func cityOpenWorldFreightSettlementOrderReadyToSettle(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	settlementOrder *cityOpenWorldFreightSettlementOrderRecord,
) (bool, error) {
	if settlementOrder == nil || settlementOrder.state == cityOpenWorldFreightSettlementOrderSettled {
		return false, nil
	}
	var caseCount, settledCount int64
	if err := tx.QueryRowContext(ctx, `
SELECT COUNT(*), COUNT(*) FILTER (WHERE state = 'settled')
FROM city_open_world_freight_settlement_cases
WHERE world_id = $1 AND settlement_order_code = $2`, worldID, settlementOrder.code).Scan(&caseCount, &settledCount); err != nil {
		return false, fmt.Errorf("count V22 freight-settlement order cases: %w", err)
	}
	if caseCount == 0 || caseCount != settledCount {
		return false, nil
	}
	if settlementOrder.sourceKind == cityOpenWorldFreightSettlementSourceShipment {
		return caseCount == 1, nil
	}
	if settlementOrder.sourceKind != cityOpenWorldFreightSettlementSourceConsignment {
		return false, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v22_freight_settlement_order_kind"})
	}
	var sourceCaseCount int64
	if err := tx.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM city_open_world_freight_batch_consignments
WHERE world_id = $1 AND plan_code = $2`, worldID, settlementOrder.sourceCode).Scan(&sourceCaseCount); err != nil {
		return false, fmt.Errorf("count V22 freight-settlement source consignments: %w", err)
	}
	return sourceCaseCount > 0 && sourceCaseCount == caseCount, nil
}

func (s *CityEconomyService) finalizeCityOpenWorldFreightSettlementOrder(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence int64,
	settlementOrder *cityOpenWorldFreightSettlementOrderRecord,
	ledgerUnit *cityLedgerBaseUnit,
	command *CityCommand,
) (cityOpenWorldFreightSettlementExecution, error) {
	if settlementOrder == nil || command == nil || ledgerUnit == nil || factSequence <= 0 {
		return cityOpenWorldFreightSettlementExecution{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v22_freight_settlement_finalize"})
	}
	supplyOrder, err := loadCityOpenWorldSupplyChainOrderForUpdate(ctx, tx, worldID, settlementOrder.orderCode)
	if err != nil {
		return cityOpenWorldFreightSettlementExecution{}, err
	}
	if supplyOrder.state != cityOpenWorldSupplyChainStateDispatched ||
		!cityOpenWorldSupplyChainTransitionAllowed(supplyOrder.state, cityOpenWorldSupplyChainStateSettled) {
		return cityOpenWorldFreightSettlementExecution{}, cityOpenWorldFreightSettlementReject(cityOpenWorldFreightSettlementRejectionSourceState)
	}
	supplyPolicy, err := loadCityOpenWorldSupplyChainPolicyForUpdate(ctx, tx, worldID)
	if err != nil {
		return cityOpenWorldFreightSettlementExecution{}, err
	}
	if err = ensureCityOpenWorldSupplyChainTransitionBudget(ctx, tx, worldID, targetTick, 1, supplyPolicy); err != nil {
		return cityOpenWorldFreightSettlementExecution{}, err
	}
	fact, err := insertCityOpenWorldSupplyChainFact(ctx, tx, worldID, targetTick, factSequence, command, &supplyOrder.code, "order.settled", map[string]any{
		"schema_version":        cityOpenWorldFreightSettlementSchemaVersion,
		"settlement_order_code": settlementOrder.code,
		"previous_state":        supplyOrder.state,
		"reason_code":           cityOpenWorldSupplyChainReasonSettled,
	})
	if err != nil {
		return cityOpenWorldFreightSettlementExecution{}, err
	}
	if err = insertCityOpenWorldSupplyChainTransition(ctx, tx, worldID, targetTick, factSequence,
		supplyOrder.code, supplyOrder.state, cityOpenWorldSupplyChainStateSettled,
		cityOpenWorldSupplyChainReasonSettled, fact, false); err != nil {
		return cityOpenWorldFreightSettlementExecution{}, err
	}
	releases, err := insertCityOpenWorldSupplyChainReservationReleases(
		ctx, tx, worldID, targetTick, supplyOrder.code, cityOpenWorldSupplyChainStateSettled, fact,
	)
	if err != nil {
		return cityOpenWorldFreightSettlementExecution{}, err
	}
	if err = updateCityOpenWorldSupplyChainPolicy(ctx, tx, worldID, cityOpenWorldSupplyChainPolicyDelta{
		facts: 1, releases: releases, activeOrders: -1,
	}); err != nil {
		return cityOpenWorldFreightSettlementExecution{}, err
	}
	result, updateErr := tx.ExecContext(ctx, `
UPDATE city_open_world_freight_settlement_orders
SET state = 'settled', version = version + 1, updated_at = NOW()
WHERE world_id = $1 AND code = $2 AND state IN ('awaiting_transport','receiving')`, worldID, settlementOrder.code)
	if updateErr != nil {
		return cityOpenWorldFreightSettlementExecution{}, fmt.Errorf("complete V22 freight-settlement order: %w", updateErr)
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr != nil || rows != 1 {
		return cityOpenWorldFreightSettlementExecution{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v22_freight_settlement_order_complete"})
	}
	if err = s.bridgeCityOpenWorldFreightSettlementCustody(ctx, tx, worldID, settlementOrder, fact); err != nil {
		return cityOpenWorldFreightSettlementExecution{}, err
	}
	return cityOpenWorldFreightSettlementExecution{
		facts: []CityOpenWorldSupplyChainFact{fact.fact}, nextFactSequence: factSequence + 1,
	}, nil
}

func (s *CityEconomyService) bridgeCityOpenWorldFreightSettlementCustody(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	settlementOrder *cityOpenWorldFreightSettlementOrderRecord,
	supplyFact *cityOpenWorldSupplyChainFactRecord,
) error {
	rows, err := tx.QueryContext(ctx, `
SELECT code, source_kind, source_code, transport_state, state
FROM city_open_world_freight_settlement_cases
WHERE world_id = $1 AND settlement_order_code = $2
ORDER BY code
FOR UPDATE`, worldID, settlementOrder.code)
	if err != nil {
		return fmt.Errorf("lock V22 freight-settlement custody cases: %w", err)
	}
	cases := make([]cityOpenWorldFreightSettlementCaseRecord, 0)
	for rows.Next() {
		item := cityOpenWorldFreightSettlementCaseRecord{}
		if err = rows.Scan(&item.code, &item.sourceKind, &item.sourceCode, &item.transportState, &item.state); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan V22 freight-settlement custody case: %w", err)
		}
		item.settlementOrderCode = settlementOrder.code
		cases = append(cases, item)
	}
	if err = closeCityRows(rows, "iterate V22 freight-settlement custody cases"); err != nil {
		return err
	}
	if len(cases) == 0 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v22_freight_settlement_custody_cases"})
	}
	for index := range cases {
		settlementCase := &cases[index]
		if settlementCase.state != cityOpenWorldFreightSettlementCaseSettled {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v22_freight_settlement_case_pending"})
		}
		switch settlementCase.sourceKind {
		case cityOpenWorldFreightSettlementSourceShipment:
			if err = bridgeCityOpenWorldFreightSettlementShipment(ctx, tx, worldID, settlementOrder, settlementCase, supplyFact); err != nil {
				return err
			}
		case cityOpenWorldFreightSettlementSourceConsignment:
			if err = bridgeCityOpenWorldFreightSettlementConsignment(ctx, tx, worldID, settlementOrder, settlementCase, supplyFact); err != nil {
				return err
			}
		default:
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v22_freight_settlement_bridge_kind"})
		}
	}
	return nil
}

func bridgeCityOpenWorldFreightSettlementShipment(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	settlementOrder *cityOpenWorldFreightSettlementOrderRecord,
	settlementCase *cityOpenWorldFreightSettlementCaseRecord,
	supplyFact *cityOpenWorldSupplyChainFactRecord,
) error {
	shipment, err := loadCityOpenWorldEnterpriseFreightReceiptShipmentForUpdate(ctx, tx, worldID, settlementCase.sourceCode)
	if err != nil {
		return err
	}
	if shipment.orderCode != settlementOrder.orderCode || shipment.state != settlementCase.transportState ||
		!cityOpenWorldEnterpriseFreightReceiptTransitionAllowed(shipment.state, cityOpenWorldEnterpriseFreightReceiptStateSettled) {
		return cityOpenWorldFreightSettlementReject(cityOpenWorldFreightSettlementRejectionTransportMismatch)
	}
	factID, err := insertCityOpenWorldEnterpriseFreightReceiptSupplyFact(ctx, tx, worldID, shipment.code, supplyFact,
		"settlement.confirmed", map[string]any{
			"schema_version":        cityOpenWorldFreightSettlementSchemaVersion,
			"settlement_order_code": settlementOrder.code, "case_code": settlementCase.code,
		})
	if err != nil {
		return err
	}
	if err = transitionCityOpenWorldEnterpriseFreightReceiptShipment(ctx, tx, worldID, shipment,
		cityOpenWorldEnterpriseFreightReceiptStateSettled, cityOpenWorldEnterpriseFreightReceiptReasonSettled, factID,
		CityOpenWorldEnterpriseFreightReceiptFactRef{
			EvidenceKind: cityOpenWorldEnterpriseFreightReceiptEvidenceSupplyChain,
			Tick:         supplyFact.fact.Tick, Sequence: supplyFact.fact.Sequence,
		}); err != nil {
		return err
	}
	if err = updateCityOpenWorldEnterpriseFreightReceiptPolicy(ctx, tx, worldID,
		cityOpenWorldEnterpriseFreightReceiptStateTransitionDelta(shipment.state,
			cityOpenWorldEnterpriseFreightReceiptStateSettled, 1, 1, 0)); err != nil {
		return err
	}
	return nil
}

func bridgeCityOpenWorldFreightSettlementConsignment(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	settlementOrder *cityOpenWorldFreightSettlementOrderRecord,
	settlementCase *cityOpenWorldFreightSettlementCaseRecord,
	supplyFact *cityOpenWorldSupplyChainFactRecord,
) error {
	plan, err := loadCityOpenWorldFreightBatchPlanForUpdate(ctx, tx, worldID, settlementOrder.sourceCode)
	if err != nil {
		return err
	}
	if plan == nil || plan.orderCode != settlementOrder.orderCode {
		return cityOpenWorldFreightSettlementReject(cityOpenWorldFreightSettlementRejectionTransportMismatch)
	}
	consignments, err := loadCityOpenWorldFreightBatchConsignmentsForUpdate(ctx, tx, worldID, plan.code, nil, 0)
	if err != nil {
		return err
	}
	var consignment *cityOpenWorldFreightBatchConsignmentRecord
	for index := range consignments {
		if consignments[index].code == settlementCase.sourceCode {
			consignment = &consignments[index]
			break
		}
	}
	if consignment == nil || consignment.state != settlementCase.transportState ||
		!cityOpenWorldFreightBatchTransitionAllowed(consignment.state, cityOpenWorldFreightBatchConsignmentStateSettled) {
		return cityOpenWorldFreightSettlementReject(cityOpenWorldFreightSettlementRejectionTransportMismatch)
	}
	factID, err := insertCityOpenWorldFreightBatchSupplyFact(ctx, tx, worldID, consignment.code, supplyFact,
		"settlement.confirmed", map[string]any{
			"schema_version":        cityOpenWorldFreightSettlementSchemaVersion,
			"settlement_order_code": settlementOrder.code, "case_code": settlementCase.code,
		})
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_open_world_freight_batch_consignments
SET state = 'settled', version = version + 1, updated_at = NOW()
WHERE world_id = $1 AND id = $2 AND state = $3`, worldID, consignment.id, consignment.state)
	if err != nil {
		return fmt.Errorf("settle V18 freight-batch consignment: %w", err)
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr != nil || rows != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v22_freight_settlement_batch_consignment_state"})
	}
	if err = insertCityOpenWorldFreightBatchTransition(ctx, tx, worldID, consignment.code,
		cityOpenWorldFreightBatchConsignmentStateSettled, cityOpenWorldFreightBatchReasonSettled, factID,
		CityOpenWorldFreightBatchFactRef{
			EvidenceKind: cityOpenWorldFreightBatchEvidenceSupplyChain,
			Tick:         supplyFact.fact.Tick, Sequence: supplyFact.fact.Sequence,
		}); err != nil {
		return err
	}
	if err = updateCityOpenWorldFreightBatchPolicy(ctx, tx, worldID,
		cityOpenWorldFreightBatchStateTransitionDelta(consignment.state,
			cityOpenWorldFreightBatchConsignmentStateSettled, 1, 1, 0)); err != nil {
		return err
	}
	for index := range consignments {
		if consignments[index].id == consignment.id {
			consignments[index].state = cityOpenWorldFreightBatchConsignmentStateSettled
			break
		}
	}
	planState := cityOpenWorldFreightBatchPlanStateFromRecords(consignments)
	if planState != plan.state {
		result, updateErr := tx.ExecContext(ctx, `
UPDATE city_open_world_freight_batch_plans
SET state = $3, version = version + 1, updated_at = NOW()
WHERE world_id = $1 AND id = $2`, worldID, plan.id, planState)
		if updateErr != nil {
			return fmt.Errorf("settle V18 freight-batch plan: %w", updateErr)
		}
		if rows, rowsErr := result.RowsAffected(); rowsErr != nil || rows != 1 {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v22_freight_settlement_batch_plan_state"})
		}
	}
	return nil
}
