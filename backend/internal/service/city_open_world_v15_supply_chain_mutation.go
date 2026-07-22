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

	"github.com/lib/pq"
)

const (
	CityCommandTypeOpenWorldSupplyOrderCreate   = "open_world.supply_order.create"
	CityCommandTypeOpenWorldSupplyOrderAccept   = "open_world.supply_order.accept"
	CityCommandTypeOpenWorldSupplyOrderDispatch = "open_world.supply_order.dispatch"
	CityCommandTypeOpenWorldSupplyOrderDeliver  = "open_world.supply_order.deliver"
	CityCommandTypeOpenWorldSupplyOrderCancel   = "open_world.supply_order.cancel"
	CityCommandTypeOpenWorldSupplyOrderFail     = "open_world.supply_order.fail"

	cityOpenWorldSupplyChainRejectionNodeNotFound       = "CITY_SUPPLY_CHAIN_NODE_NOT_FOUND"
	cityOpenWorldSupplyChainRejectionOrderNotFound      = "CITY_SUPPLY_CHAIN_ORDER_NOT_FOUND"
	cityOpenWorldSupplyChainRejectionOrderLimit         = "CITY_SUPPLY_CHAIN_ORDER_LIMIT"
	cityOpenWorldSupplyChainRejectionTransitionLimit    = "CITY_SUPPLY_CHAIN_TRANSITION_LIMIT"
	cityOpenWorldSupplyChainRejectionState              = "CITY_SUPPLY_CHAIN_STATE_INVALID"
	cityOpenWorldSupplyChainRejectionInsufficientStock  = "CITY_SUPPLY_CHAIN_INSUFFICIENT_STOCK"
	cityOpenWorldSupplyChainRejectionOrderNotActionable = "CITY_SUPPLY_CHAIN_ORDER_NOT_ACTIONABLE"
	cityOpenWorldSupplyChainRejectionReceiptNotReady    = "CITY_SUPPLY_CHAIN_RECEIPT_NOT_READY"
)

type cityOpenWorldSupplyChainOrderLinePayload struct {
	ResourceCode   string `json:"resource_code"`
	QuantityUnits  int64  `json:"quantity_units"`
	UnitPriceUnits int64  `json:"unit_price_units"`
}

type cityOpenWorldSupplyChainOrderCreatePayload struct {
	BuyerNodeCode  string                                     `json:"buyer_node_code"`
	SellerNodeCode string                                     `json:"seller_node_code"`
	Lines          []cityOpenWorldSupplyChainOrderLinePayload `json:"lines"`
}

type cityOpenWorldSupplyChainOrderActionPayload struct {
	OrderCode string `json:"order_code"`
}

type cityOpenWorldSupplyChainBusinessError struct{ code string }

func (err *cityOpenWorldSupplyChainBusinessError) Error() string { return err.code }

func cityOpenWorldSupplyChainReject(code string) error {
	return &cityOpenWorldSupplyChainBusinessError{code: code}
}

func cityOpenWorldSupplyChainBusinessRejectionCode(err error) string {
	var businessErr *cityOpenWorldSupplyChainBusinessError
	if errors.As(err, &businessErr) {
		return businessErr.code
	}
	if code := cityLedgerBusinessRejectionCode(err); code != "" {
		return code
	}
	return cityResourceBusinessRejectionCode(err)
}

func isCityOpenWorldSupplyChainCommand(commandType string) bool {
	switch commandType {
	case CityCommandTypeOpenWorldSupplyOrderCreate,
		CityCommandTypeOpenWorldSupplyOrderAccept,
		CityCommandTypeOpenWorldSupplyOrderDispatch,
		CityCommandTypeOpenWorldSupplyOrderDeliver,
		CityCommandTypeOpenWorldSupplyOrderCancel,
		CityCommandTypeOpenWorldSupplyOrderFail:
		return true
	default:
		return false
	}
}

func normalizeCityOpenWorldSupplyChainCommand(commandType string, rawPayload json.RawMessage) (any, bool, error) {
	normalizeCode := func(value *string, field string) error {
		*value = strings.ToLower(strings.TrimSpace(*value))
		if !cityOpenWorldSupplyChainCodeValid(*value) {
			return ErrCityInvalidInput.WithMetadata(map[string]string{"field": field})
		}
		return nil
	}
	switch commandType {
	case CityCommandTypeOpenWorldSupplyOrderCreate:
		var value cityOpenWorldSupplyChainOrderCreatePayload
		if err := decodeStrictCityObject(rawPayload, &value); err != nil {
			return nil, true, ErrCityInvalidInput.WithCause(err)
		}
		if err := normalizeCode(&value.BuyerNodeCode, "buyer_node_code"); err != nil {
			return nil, true, err
		}
		if err := normalizeCode(&value.SellerNodeCode, "seller_node_code"); err != nil {
			return nil, true, err
		}
		if value.BuyerNodeCode == value.SellerNodeCode || len(value.Lines) == 0 || len(value.Lines) > cityOpenWorldSupplyChainMaximumOrderLines {
			return nil, true, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "supply_order"})
		}
		seen := make(map[string]struct{}, len(value.Lines))
		for index := range value.Lines {
			line := &value.Lines[index]
			line.ResourceCode = strings.ToLower(strings.TrimSpace(line.ResourceCode))
			if !cityPhysicalCodePattern.MatchString(line.ResourceCode) || line.QuantityUnits <= 0 ||
				line.QuantityUnits > cityMaximumResourceUnits || line.UnitPriceUnits <= 0 ||
				line.UnitPriceUnits > cityMaximumTransactionUnits || line.QuantityUnits > math.MaxInt64/line.UnitPriceUnits {
				return nil, true, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "lines"})
			}
			if _, exists := seen[line.ResourceCode]; exists {
				return nil, true, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "lines"})
			}
			seen[line.ResourceCode] = struct{}{}
		}
		sort.Slice(value.Lines, func(i, j int) bool { return value.Lines[i].ResourceCode < value.Lines[j].ResourceCode })
		return value, true, nil
	case CityCommandTypeOpenWorldSupplyOrderAccept,
		CityCommandTypeOpenWorldSupplyOrderDispatch,
		CityCommandTypeOpenWorldSupplyOrderDeliver,
		CityCommandTypeOpenWorldSupplyOrderCancel,
		CityCommandTypeOpenWorldSupplyOrderFail:
		var value cityOpenWorldSupplyChainOrderActionPayload
		if err := decodeStrictCityObject(rawPayload, &value); err != nil {
			return nil, true, ErrCityInvalidInput.WithCause(err)
		}
		if err := normalizeCode(&value.OrderCode, "order_code"); err != nil {
			return nil, true, err
		}
		return value, true, nil
	default:
		return nil, false, nil
	}
}

type cityOpenWorldSupplyChainExecution struct {
	pending                cityPendingEvent
	facts                  []CityOpenWorldSupplyChainFact
	nextFactSequence       int64
	nextJournalSequence    int64
	nextResourceOpSequence int64
}

type cityOpenWorldSupplyChainFactRecord struct {
	id   int64
	fact CityOpenWorldSupplyChainFact
}

type cityOpenWorldSupplyChainNodeRecord struct {
	code         string
	firmID       int64
	firmCode     string
	facilityID   int64
	facilityCode string
	districtID   int64
	districtCode string
}

type cityOpenWorldSupplyChainOrderRecord struct {
	code                 string
	buyerNodeCode        string
	sellerNodeCode       string
	createdTick          int64
	acceptDeadlineTick   int64
	dispatchDeadlineTick int64
	lastTransitionTick   int64
	state                string
}

type cityOpenWorldSupplyChainLineRecord struct {
	lineNo               int
	resourceID           int64
	resourceCode         string
	sourceBalanceID      int64
	destinationBalanceID int64
	quantityUnits        int64
	unitPriceUnits       int64
	totalPriceUnits      int64
}

type cityOpenWorldSupplyChainPolicyDelta struct {
	orders       int64
	activeOrders int64
	facts        int64
	reservations int64
	releases     int64
	dispatches   int64
	deliveries   int64
	settlements  int64
}

func (s *CityEconomyService) applyCityOpenWorldSupplyChainCommand(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, journalSequence, resourceOperationSequence int64,
	ledgerUnit *cityLedgerBaseUnit,
	command *CityCommand,
) (cityOpenWorldSupplyChainExecution, error) {
	const savepoint = "city_open_world_supply_chain_command"
	if _, err := tx.ExecContext(ctx, `SAVEPOINT `+savepoint); err != nil {
		return cityOpenWorldSupplyChainExecution{}, fmt.Errorf("create V15 supply-chain command savepoint: %w", err)
	}
	execution, err := s.postCityOpenWorldSupplyChainCommand(
		ctx, tx, worldID, targetTick, factSequence, journalSequence, resourceOperationSequence, ledgerUnit, command,
	)
	if err != nil {
		if _, rollbackErr := tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT `+savepoint); rollbackErr != nil {
			return cityOpenWorldSupplyChainExecution{}, fmt.Errorf("rollback V15 supply-chain command after %v: %w", err, rollbackErr)
		}
		if _, releaseErr := tx.ExecContext(ctx, `RELEASE SAVEPOINT `+savepoint); releaseErr != nil {
			return cityOpenWorldSupplyChainExecution{}, fmt.Errorf("release rejected V15 supply-chain command: %w", releaseErr)
		}
		if code := cityOpenWorldSupplyChainBusinessRejectionCode(err); code != "" {
			return cityOpenWorldSupplyChainExecution{
				pending: rejectedCityCommand(command, code), nextFactSequence: factSequence,
				nextJournalSequence: journalSequence, nextResourceOpSequence: resourceOperationSequence,
			}, nil
		}
		return cityOpenWorldSupplyChainExecution{}, err
	}
	if _, err = tx.ExecContext(ctx, `RELEASE SAVEPOINT `+savepoint); err != nil {
		return cityOpenWorldSupplyChainExecution{}, fmt.Errorf("release V15 supply-chain command savepoint: %w", err)
	}
	return execution, nil
}

func ensureCityOpenWorldSupplyChainEngine(ctx context.Context, tx *sql.Tx, worldID int64) error {
	var version string
	if err := tx.QueryRowContext(ctx, `SELECT simulation_version FROM city_worlds WHERE id = $1 FOR UPDATE`, worldID).Scan(&version); err != nil {
		return fmt.Errorf("lock V15 supply-chain world: %w", err)
	}
	if !cityEngineSupportsOpenWorldSupplyChain(version) {
		return ErrCitySimulationVersion.WithMetadata(map[string]string{"version": version})
	}
	return nil
}

func loadCityOpenWorldSupplyChainPolicyForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
) (*CityOpenWorldSupplyChainPolicy, error) {
	policy := &CityOpenWorldSupplyChainPolicy{}
	err := tx.QueryRowContext(ctx, `
SELECT profile_id, profile_version, content_hash, baseline_tick,
       node_contract, order_contract, settlement_contract, delivery_contract,
       maximum_orders, maximum_order_lines, maximum_transitions_per_tick,
       accept_timeout_ticks, dispatch_timeout_ticks, node_count, order_count,
       active_order_count, fact_count, reservation_count, release_count,
       dispatch_count, delivery_count, settlement_count, revision, metadata
FROM city_open_world_supply_chain_profiles
WHERE world_id = $1
FOR UPDATE`, worldID).Scan(
		&policy.ProfileID, &policy.ProfileVersion, &policy.ContentHash, &policy.BaselineTick,
		&policy.NodeContract, &policy.OrderContract, &policy.SettlementContract, &policy.DeliveryContract,
		&policy.MaximumOrders, &policy.MaximumOrderLines, &policy.MaximumTransitionsPerTick,
		&policy.AcceptTimeoutTicks, &policy.DispatchTimeoutTicks, &policy.NodeCount, &policy.OrderCount,
		&policy.ActiveOrderCount, &policy.FactCount, &policy.ReservationCount, &policy.ReleaseCount,
		&policy.DispatchCount, &policy.DeliveryCount, &policy.SettlementCount, &policy.Revision, &policy.Metadata,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v15_supply_chain_profile"})
	}
	if err != nil {
		return nil, fmt.Errorf("lock V15 supply-chain profile: %w", err)
	}
	contentHash, hashErr := cityOpenWorldSupplyChainPolicyHash()
	if hashErr != nil {
		return nil, hashErr
	}
	if policy.ProfileID != cityOpenWorldSupplyChainProfileID || policy.ProfileVersion != cityOpenWorldSupplyChainProfileVersion ||
		policy.ContentHash != contentHash || policy.NodeContract != cityOpenWorldSupplyChainNodeContract ||
		policy.OrderContract != cityOpenWorldSupplyChainOrderContract || policy.SettlementContract != cityOpenWorldSupplyChainSettlementContract ||
		policy.DeliveryContract != cityOpenWorldSupplyChainDeliveryContract || policy.MaximumOrders != cityOpenWorldSupplyChainMaximumOrders ||
		policy.MaximumOrderLines != cityOpenWorldSupplyChainMaximumOrderLines || policy.MaximumTransitionsPerTick != cityOpenWorldSupplyChainMaximumTransitionsTick ||
		policy.AcceptTimeoutTicks != cityOpenWorldSupplyChainAcceptTimeoutTicks || policy.DispatchTimeoutTicks != cityOpenWorldSupplyChainDispatchTimeoutTicks ||
		policy.Revision < 1 || !cityOpenWorldSupplyChainJSONObject(policy.Metadata) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v15_supply_chain_policy"})
	}
	return policy, nil
}

func updateCityOpenWorldSupplyChainPolicy(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	delta cityOpenWorldSupplyChainPolicyDelta,
) error {
	result, err := tx.ExecContext(ctx, `
UPDATE city_open_world_supply_chain_profiles
SET order_count = order_count + $2,
    active_order_count = active_order_count + $3,
    fact_count = fact_count + $4,
    reservation_count = reservation_count + $5,
    release_count = release_count + $6,
    dispatch_count = dispatch_count + $7,
    delivery_count = delivery_count + $8,
    settlement_count = settlement_count + $9,
    revision = revision + 1,
    updated_at = NOW()
WHERE world_id = $1`, worldID, delta.orders, delta.activeOrders, delta.facts,
		delta.reservations, delta.releases, delta.dispatches, delta.deliveries, delta.settlements)
	if err != nil {
		return fmt.Errorf("update V15 supply-chain profile: %w", err)
	}
	if count, countErr := result.RowsAffected(); countErr != nil || count != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v15_supply_chain_profile"})
	}
	return nil
}

func ensureCityOpenWorldSupplyChainTransitionBudget(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, wanted int64,
	policy *CityOpenWorldSupplyChainPolicy,
) error {
	if policy == nil || wanted < 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v15_supply_chain_transition_budget"})
	}
	var existing int64
	if err := tx.QueryRowContext(ctx, `
SELECT COUNT(*) FROM city_open_world_supply_chain_order_transitions
WHERE world_id = $1 AND transition_tick = $2`, worldID, targetTick).Scan(&existing); err != nil {
		return fmt.Errorf("count V15 supply-chain transitions: %w", err)
	}
	if existing+wanted > int64(policy.MaximumTransitionsPerTick) {
		return cityOpenWorldSupplyChainReject(cityOpenWorldSupplyChainRejectionTransitionLimit)
	}
	return nil
}

func loadCityOpenWorldSupplyChainNodeForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	code string,
) (*cityOpenWorldSupplyChainNodeRecord, error) {
	record := &cityOpenWorldSupplyChainNodeRecord{}
	err := tx.QueryRowContext(ctx, `
SELECT node.code, firm.id, firm.code, facility.id, facility.code, district.id, district.code
FROM city_open_world_supply_chain_nodes node
JOIN city_economic_entities firm
  ON firm.id = node.firm_entity_id AND firm.world_id = node.world_id AND firm.status = 'active'
JOIN city_open_world_facilities facility
  ON facility.id = node.facility_id AND facility.world_id = node.world_id AND facility.state = 'active'
JOIN city_districts district
  ON district.id = node.district_id AND district.world_id = node.world_id
WHERE node.world_id = $1 AND node.code = $2 AND node.state = 'active'
FOR UPDATE OF node, firm, facility, district`, worldID, code).Scan(
		&record.code, &record.firmID, &record.firmCode, &record.facilityID, &record.facilityCode,
		&record.districtID, &record.districtCode,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, cityOpenWorldSupplyChainReject(cityOpenWorldSupplyChainRejectionNodeNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("lock V15 supply-chain node %s: %w", code, err)
	}
	return record, nil
}

func insertCityOpenWorldSupplyChainFact(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, sequence int64,
	sourceCommand *CityCommand,
	orderCode *string,
	factType string,
	payload map[string]any,
) (*cityOpenWorldSupplyChainFactRecord, error) {
	if worldID <= 0 || targetTick <= 0 || sequence <= 0 ||
		!cityOpenWorldSupplyChainFactTypeValid(factType) || payload == nil ||
		(orderCode != nil && !cityOpenWorldSupplyChainCodeValid(*orderCode)) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v15_supply_chain_fact"})
	}
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal V15 supply-chain fact payload: %w", err)
	}
	var sourceCommandID any
	var sourceCommandSequence *int64
	if sourceCommand != nil {
		if sourceCommand.ID <= 0 || sourceCommand.Sequence <= 0 {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v15_supply_chain_fact_command"})
		}
		sourceCommandID = sourceCommand.ID
		sequenceValue := sourceCommand.Sequence
		sourceCommandSequence = &sequenceValue
	}
	var order any
	if orderCode != nil {
		order = *orderCode
	}
	record := &cityOpenWorldSupplyChainFactRecord{fact: CityOpenWorldSupplyChainFact{
		Tick: targetTick, Sequence: sequence, SourceCommandSequence: sourceCommandSequence,
		OrderCode: orderCode, FactType: factType, Payload: rawPayload,
	}}
	if err = tx.QueryRowContext(ctx, `
INSERT INTO city_open_world_supply_chain_facts
    (world_id, tick, sequence, source_command_id, order_code, fact_type, payload)
VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)
RETURNING id`, worldID, targetTick, sequence, sourceCommandID, order, factType, []byte(rawPayload)).Scan(&record.id); err != nil {
		return nil, fmt.Errorf("insert V15 supply-chain fact: %w", err)
	}
	return record, nil
}

func insertCityOpenWorldSupplyChainTransition(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, transitionSequence int64,
	orderCode, previousState, nextState, reasonCode string,
	fact *cityOpenWorldSupplyChainFactRecord,
	automatic bool,
) error {
	if fact == nil || fact.id <= 0 || fact.fact.Tick != targetTick || fact.fact.Sequence != transitionSequence ||
		!cityOpenWorldSupplyChainTransitionAllowed(previousState, nextState) ||
		!cityOpenWorldSupplyChainReasonValid(reasonCode) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v15_supply_chain_transition"})
	}
	metadata, err := json.Marshal(map[string]any{
		"schema_version": cityOpenWorldSupplyChainSchemaVersion,
		"previous_state": previousState,
		"automatic":      automatic,
		"source_fact": map[string]any{
			"tick": fact.fact.Tick, "sequence": fact.fact.Sequence,
		},
	})
	if err != nil {
		return fmt.Errorf("marshal V15 supply-chain transition metadata: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_supply_chain_order_transitions
    (world_id, order_code, transition_tick, transition_sequence, state,
     reason_code, source_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb)`,
		worldID, orderCode, targetTick, transitionSequence, nextState, reasonCode,
		fact.id, []byte(metadata)); err != nil {
		return fmt.Errorf("insert V15 supply-chain transition: %w", err)
	}
	return nil
}

func loadCityOpenWorldSupplyChainOrderForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	orderCode string,
) (*cityOpenWorldSupplyChainOrderRecord, error) {
	record := &cityOpenWorldSupplyChainOrderRecord{}
	err := tx.QueryRowContext(ctx, `
SELECT header.code, header.buyer_node_code, header.seller_node_code,
       header.created_tick, header.accept_deadline_tick, header.dispatch_deadline_tick,
       current_state.transition_tick, current_state.state
FROM city_open_world_supply_chain_orders header
JOIN LATERAL (
    SELECT transition.transition_tick, transition.state
    FROM city_open_world_supply_chain_order_transitions transition
    WHERE transition.world_id = header.world_id AND transition.order_code = header.code
    ORDER BY transition.transition_tick DESC, transition.transition_sequence DESC
    LIMIT 1
) current_state ON TRUE
WHERE header.world_id = $1 AND header.code = $2
FOR UPDATE OF header`, worldID, orderCode).Scan(
		&record.code, &record.buyerNodeCode, &record.sellerNodeCode,
		&record.createdTick, &record.acceptDeadlineTick, &record.dispatchDeadlineTick,
		&record.lastTransitionTick, &record.state,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, cityOpenWorldSupplyChainReject(cityOpenWorldSupplyChainRejectionOrderNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("lock V15 supply-chain order %s: %w", orderCode, err)
	}
	if !cityOpenWorldSupplyChainTransitionStateValid(record.state) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v15_supply_chain_order_state"})
	}
	return record, nil
}

func loadCityOpenWorldSupplyChainLinesForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	orderCode string,
) ([]cityOpenWorldSupplyChainLineRecord, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT line.line_no, line.resource_id, resource.code, line.source_balance_id,
       line.destination_balance_id, line.quantity_units, line.unit_price_units,
       line.total_price_units
FROM city_open_world_supply_chain_order_lines line
JOIN city_resources resource
  ON resource.id = line.resource_id AND resource.world_id = line.world_id
WHERE line.world_id = $1 AND line.order_code = $2
ORDER BY line.line_no
FOR UPDATE OF line`, worldID, orderCode)
	if err != nil {
		return nil, fmt.Errorf("lock V15 supply-chain order lines: %w", err)
	}
	items := make([]cityOpenWorldSupplyChainLineRecord, 0)
	for rows.Next() {
		item := cityOpenWorldSupplyChainLineRecord{}
		if err = rows.Scan(&item.lineNo, &item.resourceID, &item.resourceCode,
			&item.sourceBalanceID, &item.destinationBalanceID, &item.quantityUnits,
			&item.unitPriceUnits, &item.totalPriceUnits); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan V15 supply-chain order line: %w", err)
		}
		items = append(items, item)
	}
	if err = closeCityRows(rows, "iterate V15 supply-chain order lines"); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v15_supply_chain_order_lines"})
	}
	return items, nil
}

func cityOpenWorldSupplyChainTotalPrice(lines []cityOpenWorldSupplyChainLineRecord) (int64, error) {
	if len(lines) == 0 {
		return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v15_supply_chain_total"})
	}
	var total int64
	for _, line := range lines {
		if line.totalPriceUnits <= 0 || line.totalPriceUnits > math.MaxInt64-total {
			return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v15_supply_chain_total"})
		}
		total += line.totalPriceUnits
	}
	return total, nil
}

func cityOpenWorldSupplyChainPending(
	command *CityCommand,
	orderCode, state string,
	journal *CityJournal,
	operation *CityResourceOperation,
) cityPendingEvent {
	eventType := "city.open_world.supply_order." + state
	payload := map[string]any{"order_code": orderCode, "state": state}
	result := map[string]any{"applied": true, "order_code": orderCode, "state": state}
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
	return cityPendingEvent{command: command, status: CityCommandStatusApplied, eventType: eventType, payload: payload, result: result}
}

func cityOpenWorldSupplyChainActionableAtTick(order *cityOpenWorldSupplyChainOrderRecord, targetTick int64, acceptedState string) error {
	if order == nil || targetTick <= order.lastTransitionTick ||
		!cityOpenWorldSupplyChainTransitionAllowed(order.state, acceptedState) {
		return cityOpenWorldSupplyChainReject(cityOpenWorldSupplyChainRejectionOrderNotActionable)
	}
	return nil
}

func (s *CityEconomyService) postCityOpenWorldSupplyChainCommand(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, journalSequence, resourceOperationSequence int64,
	ledgerUnit *cityLedgerBaseUnit,
	command *CityCommand,
) (cityOpenWorldSupplyChainExecution, error) {
	if command == nil || command.ID <= 0 || command.Sequence <= 0 || ledgerUnit == nil {
		return cityOpenWorldSupplyChainExecution{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v15_supply_chain_command"})
	}
	if err := ensureCityOpenWorldSupplyChainEngine(ctx, tx, worldID); err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	if err := activateCityOpenWorldSupplyChainWrite(ctx, tx, worldID); err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	policy, err := loadCityOpenWorldSupplyChainPolicyForUpdate(ctx, tx, worldID)
	if err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	switch command.CommandType {
	case CityCommandTypeOpenWorldSupplyOrderCreate:
		payload, decodeErr := decodeStoredCityCommandPayload[cityOpenWorldSupplyChainOrderCreatePayload](command)
		if decodeErr != nil {
			return cityOpenWorldSupplyChainExecution{}, decodeErr
		}
		return s.createCityOpenWorldSupplyChainOrder(
			ctx, tx, worldID, targetTick, factSequence, journalSequence, resourceOperationSequence,
			policy, command, payload,
		)
	case CityCommandTypeOpenWorldSupplyOrderAccept,
		CityCommandTypeOpenWorldSupplyOrderDispatch,
		CityCommandTypeOpenWorldSupplyOrderDeliver,
		CityCommandTypeOpenWorldSupplyOrderCancel,
		CityCommandTypeOpenWorldSupplyOrderFail:
		payload, decodeErr := decodeStoredCityCommandPayload[cityOpenWorldSupplyChainOrderActionPayload](command)
		if decodeErr != nil {
			return cityOpenWorldSupplyChainExecution{}, decodeErr
		}
		switch command.CommandType {
		case CityCommandTypeOpenWorldSupplyOrderAccept:
			return s.acceptCityOpenWorldSupplyChainOrder(
				ctx, tx, worldID, targetTick, factSequence, journalSequence, resourceOperationSequence,
				ledgerUnit, policy, command, payload.OrderCode,
			)
		case CityCommandTypeOpenWorldSupplyOrderDispatch:
			return s.dispatchCityOpenWorldSupplyChainOrder(
				ctx, tx, worldID, targetTick, factSequence, journalSequence, resourceOperationSequence,
				policy, command, payload.OrderCode,
			)
		case CityCommandTypeOpenWorldSupplyOrderDeliver:
			return s.deliverCityOpenWorldSupplyChainOrder(
				ctx, tx, worldID, targetTick, factSequence, journalSequence, resourceOperationSequence,
				policy, command, payload.OrderCode,
			)
		case CityCommandTypeOpenWorldSupplyOrderCancel:
			return s.cancelCityOpenWorldSupplyChainOrder(
				ctx, tx, worldID, targetTick, factSequence, journalSequence, resourceOperationSequence,
				ledgerUnit, policy, command, payload.OrderCode,
			)
		default:
			return s.failCityOpenWorldSupplyChainOrder(
				ctx, tx, worldID, targetTick, factSequence, journalSequence, resourceOperationSequence,
				ledgerUnit, policy, command, payload.OrderCode,
			)
		}
	default:
		return cityOpenWorldSupplyChainExecution{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"command_type": command.CommandType})
	}
}

func (s *CityEconomyService) createCityOpenWorldSupplyChainOrder(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, journalSequence, resourceOperationSequence int64,
	policy *CityOpenWorldSupplyChainPolicy,
	command *CityCommand,
	payload cityOpenWorldSupplyChainOrderCreatePayload,
) (cityOpenWorldSupplyChainExecution, error) {
	if policy == nil || policy.OrderCount >= int64(policy.MaximumOrders) {
		return cityOpenWorldSupplyChainExecution{}, cityOpenWorldSupplyChainReject(cityOpenWorldSupplyChainRejectionOrderLimit)
	}
	if err := ensureCityOpenWorldSupplyChainTransitionBudget(ctx, tx, worldID, targetTick, 1, policy); err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	buyer, err := loadCityOpenWorldSupplyChainNodeForUpdate(ctx, tx, worldID, payload.BuyerNodeCode)
	if err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	seller, err := loadCityOpenWorldSupplyChainNodeForUpdate(ctx, tx, worldID, payload.SellerNodeCode)
	if err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	if buyer.firmID == seller.firmID {
		return cityOpenWorldSupplyChainExecution{}, cityOpenWorldSupplyChainReject(cityOpenWorldSupplyChainRejectionNodeNotFound)
	}
	orderCode := cityOpenWorldSupplyChainOrderCode(command.Sequence)
	lines := make([]cityOpenWorldSupplyChainLineRecord, 0, len(payload.Lines))
	for index, requested := range payload.Lines {
		source, sourceErr := ensureCityInventoryRef(ctx, tx, worldID, seller.firmID, seller.districtCode, requested.ResourceCode)
		if sourceErr != nil {
			return cityOpenWorldSupplyChainExecution{}, sourceErr
		}
		destination, destinationErr := ensureCityInventoryRef(ctx, tx, worldID, buyer.firmID, buyer.districtCode, requested.ResourceCode)
		if destinationErr != nil {
			return cityOpenWorldSupplyChainExecution{}, destinationErr
		}
		if source.id == destination.id || source.resourceID != destination.resourceID ||
			source.entityID != seller.firmID || destination.entityID != buyer.firmID ||
			source.districtID != seller.districtID || destination.districtID != buyer.districtID {
			return cityOpenWorldSupplyChainExecution{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v15_supply_chain_inventory_scope"})
		}
		if requested.QuantityUnits > math.MaxInt64/requested.UnitPriceUnits {
			return cityOpenWorldSupplyChainExecution{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v15_supply_chain_line_total"})
		}
		lines = append(lines, cityOpenWorldSupplyChainLineRecord{
			lineNo: index + 1, resourceID: source.resourceID, resourceCode: source.resourceCode,
			sourceBalanceID: source.id, destinationBalanceID: destination.id,
			quantityUnits: requested.QuantityUnits, unitPriceUnits: requested.UnitPriceUnits,
			totalPriceUnits: requested.QuantityUnits * requested.UnitPriceUnits,
		})
	}
	totalPrice, err := cityOpenWorldSupplyChainTotalPrice(lines)
	if err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	factLines := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		factLines = append(factLines, map[string]any{
			"resource_code": line.resourceCode, "quantity_units": line.quantityUnits,
			"unit_price_units": line.unitPriceUnits, "total_price_units": line.totalPriceUnits,
		})
	}
	fact, err := insertCityOpenWorldSupplyChainFact(ctx, tx, worldID, targetTick, factSequence, command, &orderCode, "order.proposed", map[string]any{
		"schema_version":  cityOpenWorldSupplyChainSchemaVersion,
		"buyer_node_code": buyer.code, "seller_node_code": seller.code,
		"total_price_units": totalPrice, "lines": factLines,
	})
	if err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	orderMetadata, err := json.Marshal(map[string]any{
		"schema_version":   cityOpenWorldSupplyChainSchemaVersion,
		"command_sequence": command.Sequence, "total_price_units": totalPrice,
	})
	if err != nil {
		return cityOpenWorldSupplyChainExecution{}, fmt.Errorf("marshal V15 supply-chain order metadata: %w", err)
	}
	acceptDeadline := targetTick + policy.AcceptTimeoutTicks
	dispatchDeadline := targetTick + policy.DispatchTimeoutTicks
	if acceptDeadline <= targetTick || dispatchDeadline <= acceptDeadline {
		return cityOpenWorldSupplyChainExecution{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v15_supply_chain_deadline"})
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_supply_chain_orders
    (world_id, code, buyer_node_code, seller_node_code, created_tick,
     accept_deadline_tick, dispatch_deadline_tick, created_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb)`,
		worldID, orderCode, buyer.code, seller.code, targetTick, acceptDeadline,
		dispatchDeadline, fact.id, []byte(orderMetadata)); err != nil {
		return cityOpenWorldSupplyChainExecution{}, fmt.Errorf("insert V15 supply-chain order: %w", err)
	}
	for _, line := range lines {
		lineMetadata, marshalErr := json.Marshal(map[string]any{
			"schema_version":   cityOpenWorldSupplyChainSchemaVersion,
			"source_node_code": seller.code, "destination_node_code": buyer.code,
		})
		if marshalErr != nil {
			return cityOpenWorldSupplyChainExecution{}, fmt.Errorf("marshal V15 supply-chain line metadata: %w", marshalErr)
		}
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_supply_chain_order_lines
    (world_id, order_code, line_no, resource_id, source_balance_id,
     destination_balance_id, quantity_units, unit_price_units, total_price_units, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb)`,
			worldID, orderCode, line.lineNo, line.resourceID, line.sourceBalanceID,
			line.destinationBalanceID, line.quantityUnits, line.unitPriceUnits,
			line.totalPriceUnits, []byte(lineMetadata)); err != nil {
			return cityOpenWorldSupplyChainExecution{}, fmt.Errorf("insert V15 supply-chain order line: %w", err)
		}
	}
	if err = insertCityOpenWorldSupplyChainTransition(ctx, tx, worldID, targetTick, factSequence,
		orderCode, "", cityOpenWorldSupplyChainStateProposed, cityOpenWorldSupplyChainReasonCreated, fact, false); err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	if err = updateCityOpenWorldSupplyChainPolicy(ctx, tx, worldID, cityOpenWorldSupplyChainPolicyDelta{
		orders: 1, activeOrders: 1, facts: 1,
	}); err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	if err = assertCityOpenWorldSupplyChainFoundation(ctx, tx, worldID); err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	return cityOpenWorldSupplyChainExecution{
		pending: cityOpenWorldSupplyChainPending(command, orderCode, cityOpenWorldSupplyChainStateProposed, nil, nil),
		facts:   []CityOpenWorldSupplyChainFact{fact.fact}, nextFactSequence: factSequence + 1,
		nextJournalSequence: journalSequence, nextResourceOpSequence: resourceOperationSequence,
	}, nil
}

func lockCityOpenWorldSupplyChainSourceBalances(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	lines []cityOpenWorldSupplyChainLineRecord,
) (map[int64]int64, error) {
	ids := make([]int64, 0, len(lines))
	seen := make(map[int64]struct{}, len(lines))
	for _, line := range lines {
		if line.sourceBalanceID <= 0 {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v15_supply_chain_source_balance"})
		}
		if _, exists := seen[line.sourceBalanceID]; !exists {
			seen[line.sourceBalanceID] = struct{}{}
			ids = append(ids, line.sourceBalanceID)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	rows, err := tx.QueryContext(ctx, `
SELECT id, quantity_units
FROM city_inventory_balances
WHERE world_id = $1 AND id = ANY($2) AND status = 'active'
ORDER BY id
FOR UPDATE`, worldID, pq.Array(ids))
	if err != nil {
		return nil, fmt.Errorf("lock V15 supply-chain source balances: %w", err)
	}
	balances := make(map[int64]int64, len(ids))
	for rows.Next() {
		var balanceID, quantity int64
		if err = rows.Scan(&balanceID, &quantity); err != nil {
			_ = rows.Close()
			return nil, err
		}
		balances[balanceID] = quantity
	}
	if err = closeCityRows(rows, "iterate V15 supply-chain source balances"); err != nil {
		return nil, err
	}
	if len(balances) != len(ids) {
		return nil, cityOpenWorldSupplyChainReject(cityOpenWorldSupplyChainRejectionInsufficientStock)
	}
	return balances, nil
}

func loadCityInventoryRefByID(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, balanceID int64,
) (*cityInventoryRef, error) {
	item := &cityInventoryRef{}
	err := queryer.QueryRowContext(ctx, `
SELECT balance.id, balance.entity_id, balance.entity_type, entity.code,
       balance.district_id, district.code, balance.resource_id, resource.code,
       balance.quantity_units, balance.version
FROM city_inventory_balances balance
JOIN city_economic_entities entity
  ON entity.id = balance.entity_id AND entity.world_id = balance.world_id AND entity.status = 'active'
JOIN city_districts district
  ON district.id = balance.district_id AND district.world_id = balance.world_id
JOIN city_resources resource
  ON resource.id = balance.resource_id AND resource.world_id = balance.world_id
 AND resource.status = 'active' AND resource.storable
WHERE balance.world_id = $1 AND balance.id = $2 AND balance.status = 'active'`,
		worldID, balanceID).Scan(
		&item.id, &item.entityID, &item.entityType, &item.entityCode,
		&item.districtID, &item.districtCode, &item.resourceID, &item.resourceCode,
		&item.quantityUnits, &item.version,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, cityResourceReject(cityResourceRejectionScopeNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("load V15 supply-chain inventory balance: %w", err)
	}
	return item, nil
}

func activeCityOpenWorldSupplyChainReservationUnits(
	ctx context.Context,
	tx *sql.Tx,
	worldID, balanceID int64,
) (int64, error) {
	var reserved int64
	if err := tx.QueryRowContext(ctx, `
SELECT COALESCE(SUM(GREATEST(reservation.quantity_units - COALESCE(outcome.outbound_units, 0), 0)), 0)::BIGINT
FROM city_open_world_supply_chain_reservations reservation
LEFT JOIN city_open_world_supply_chain_reservation_releases release
  ON release.world_id = reservation.world_id AND release.reservation_id = reservation.id
LEFT JOIN LATERAL (
    SELECT COALESCE(SUM(receipt_line.accepted_units + receipt_line.lost_units), 0)::BIGINT AS outbound_units
    FROM city_open_world_freight_settlement_orders settlement_order
    JOIN city_open_world_freight_settlement_cases settlement_case
      ON settlement_case.world_id = settlement_order.world_id
     AND settlement_case.settlement_order_code = settlement_order.code
    JOIN city_open_world_freight_settlement_receipt_lines receipt_line
      ON receipt_line.world_id = settlement_case.world_id
     AND receipt_line.case_code = settlement_case.code
    WHERE settlement_order.world_id = reservation.world_id
      AND settlement_order.order_code = reservation.order_code
      AND receipt_line.source_line_no = reservation.line_no
) outcome ON TRUE
WHERE reservation.world_id = $1 AND reservation.source_balance_id = $2
  AND release.id IS NULL`, worldID, balanceID).Scan(&reserved); err != nil {
		return 0, fmt.Errorf("sum V15 supply-chain active reservations: %w", err)
	}
	return reserved, nil
}

// A V22 case is the successor evidence for a dispatched V15 order. Once it
// exists, the historical atomic delivery/failure actions would either bypass
// partial inventory movement or reverse already-refunded quantities. Keep the
// original commands available for pre-V22 evidence, but route tracked orders
// through the explicit settlement receipt protocol.
func cityOpenWorldSupplyChainSettlementTracked(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	orderCode string,
) (bool, error) {
	var tracked bool
	if err := tx.QueryRowContext(ctx, `
SELECT EXISTS (
    SELECT 1
    FROM city_open_world_freight_settlement_orders
    WHERE world_id = $1 AND order_code = $2
)`, worldID, orderCode).Scan(&tracked); err != nil {
		return false, fmt.Errorf("check V22 freight-settlement tracking: %w", err)
	}
	return tracked, nil
}

func insertCityOpenWorldSupplyChainReservations(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	orderCode string,
	lines []cityOpenWorldSupplyChainLineRecord,
	fact *cityOpenWorldSupplyChainFactRecord,
) error {
	if fact == nil || fact.id <= 0 || fact.fact.FactType != "order.accepted" {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v15_supply_chain_reservation_fact"})
	}
	for _, line := range lines {
		metadata, err := json.Marshal(map[string]any{
			"schema_version": cityOpenWorldSupplyChainSchemaVersion,
			"resource_code":  line.resourceCode,
		})
		if err != nil {
			return fmt.Errorf("marshal V15 supply-chain reservation metadata: %w", err)
		}
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_supply_chain_reservations
    (world_id, order_code, line_no, source_balance_id, quantity_units,
     reserved_tick, source_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb)`,
			worldID, orderCode, line.lineNo, line.sourceBalanceID, line.quantityUnits,
			targetTick, fact.id, []byte(metadata)); err != nil {
			return fmt.Errorf("insert V15 supply-chain reservation: %w", err)
		}
	}
	return nil
}

func insertCityOpenWorldSupplyChainAcceptanceSettlement(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	orderCode string,
	journal *CityJournal,
	fact *cityOpenWorldSupplyChainFactRecord,
) error {
	if journal == nil || journal.ID <= 0 || fact == nil || fact.id <= 0 ||
		fact.fact.FactType != "order.accepted" {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v15_supply_chain_acceptance_settlement"})
	}
	metadata, err := json.Marshal(map[string]any{
		"schema_version": cityOpenWorldSupplyChainSchemaVersion,
		"journal_tick":   journal.Tick, "journal_sequence": journal.Sequence,
	})
	if err != nil {
		return fmt.Errorf("marshal V15 supply-chain acceptance settlement metadata: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_supply_chain_settlements
    (world_id, order_code, settlement_kind, journal_id, source_fact_id,
     reversal_of_settlement_id, metadata)
VALUES ($1, $2, 'acceptance', $3, $4, NULL, $5::jsonb)`,
		worldID, orderCode, journal.ID, fact.id, []byte(metadata)); err != nil {
		return fmt.Errorf("insert V15 supply-chain acceptance settlement: %w", err)
	}
	return nil
}

func (s *CityEconomyService) acceptCityOpenWorldSupplyChainOrder(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, journalSequence, resourceOperationSequence int64,
	ledgerUnit *cityLedgerBaseUnit,
	policy *CityOpenWorldSupplyChainPolicy,
	command *CityCommand,
	orderCode string,
) (cityOpenWorldSupplyChainExecution, error) {
	order, err := loadCityOpenWorldSupplyChainOrderForUpdate(ctx, tx, worldID, orderCode)
	if err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	if err = cityOpenWorldSupplyChainActionableAtTick(order, targetTick, cityOpenWorldSupplyChainStateAccepted); err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	if targetTick > order.acceptDeadlineTick {
		return cityOpenWorldSupplyChainExecution{}, cityOpenWorldSupplyChainReject(cityOpenWorldSupplyChainRejectionOrderNotActionable)
	}
	if err = ensureCityOpenWorldSupplyChainTransitionBudget(ctx, tx, worldID, targetTick, 1, policy); err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	buyer, err := loadCityOpenWorldSupplyChainNodeForUpdate(ctx, tx, worldID, order.buyerNodeCode)
	if err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	seller, err := loadCityOpenWorldSupplyChainNodeForUpdate(ctx, tx, worldID, order.sellerNodeCode)
	if err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	lines, err := loadCityOpenWorldSupplyChainLinesForUpdate(ctx, tx, worldID, order.code)
	if err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	balances, err := lockCityOpenWorldSupplyChainSourceBalances(ctx, tx, worldID, lines)
	if err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	for _, line := range lines {
		reserved, reservationErr := activeCityOpenWorldSupplyChainReservationUnits(ctx, tx, worldID, line.sourceBalanceID)
		if reservationErr != nil {
			return cityOpenWorldSupplyChainExecution{}, reservationErr
		}
		available := balances[line.sourceBalanceID] - reserved
		if reserved < 0 || available < line.quantityUnits {
			return cityOpenWorldSupplyChainExecution{}, cityOpenWorldSupplyChainReject(cityOpenWorldSupplyChainRejectionInsufficientStock)
		}
	}
	totalPrice, err := cityOpenWorldSupplyChainTotalPrice(lines)
	if err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	fact, err := insertCityOpenWorldSupplyChainFact(ctx, tx, worldID, targetTick, factSequence, command, &order.code, "order.accepted", map[string]any{
		"schema_version":  cityOpenWorldSupplyChainSchemaVersion,
		"buyer_node_code": buyer.code, "seller_node_code": seller.code,
		"total_price_units": totalPrice,
	})
	if err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	if err = insertCityOpenWorldSupplyChainTransition(ctx, tx, worldID, targetTick, factSequence,
		order.code, order.state, cityOpenWorldSupplyChainStateAccepted, cityOpenWorldSupplyChainReasonAccepted, fact, false); err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	if err = insertCityOpenWorldSupplyChainReservations(ctx, tx, worldID, targetTick, order.code, lines, fact); err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	buyerInventory, err := loadCityLedgerAccount(ctx, tx, worldID, ledgerUnit.id, buyer.firmID, CityEntityTypeFirm, "inventory")
	if err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	buyerCash, err := loadCityLedgerAccount(ctx, tx, worldID, ledgerUnit.id, buyer.firmID, CityEntityTypeFirm, "cash")
	if err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	sellerCash, err := loadCityLedgerAccount(ctx, tx, worldID, ledgerUnit.id, seller.firmID, CityEntityTypeFirm, "cash")
	if err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	sellerRevenue, err := loadCityLedgerAccount(ctx, tx, worldID, ledgerUnit.id, seller.firmID, CityEntityTypeFirm, "revenue")
	if err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	journal, err := postCityJournal(ctx, tx, cityLedgerJournalSpec{
		worldID: worldID, unit: ledgerUnit, tick: targetTick, sequence: journalSequence,
		operationKey: "supply:accept:" + order.code, journalType: "purchase", sourceCommandID: &command.ID,
		description: "Supply order acceptance " + order.code,
		metadata: map[string]any{
			"schema_version":    cityOpenWorldSupplyChainSchemaVersion,
			"supply_order_code": order.code, "supply_chain_fact_tick": fact.fact.Tick,
			"supply_chain_fact_sequence": fact.fact.Sequence, "total_price_units": totalPrice,
		},
		lines: []cityLedgerPostingLine{
			{account: buyerInventory, debitUnits: totalPrice, memo: "Supply order inventory " + order.code},
			{account: sellerCash, debitUnits: totalPrice, memo: "Supply order receipt " + order.code},
			{account: buyerCash, creditUnits: totalPrice, memo: "Supply order payment " + order.code},
			{account: sellerRevenue, creditUnits: totalPrice, memo: "Supply order revenue " + order.code},
		},
	})
	if err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	if err = insertCityOpenWorldSupplyChainAcceptanceSettlement(ctx, tx, worldID, order.code, journal, fact); err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	if err = updateCityOpenWorldSupplyChainPolicy(ctx, tx, worldID, cityOpenWorldSupplyChainPolicyDelta{
		facts: 1, reservations: int64(len(lines)), settlements: 1,
	}); err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	if err = assertCityOpenWorldSupplyChainFoundation(ctx, tx, worldID); err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	return cityOpenWorldSupplyChainExecution{
		pending: cityOpenWorldSupplyChainPending(command, order.code, cityOpenWorldSupplyChainStateAccepted, journal, nil),
		facts:   []CityOpenWorldSupplyChainFact{fact.fact}, nextFactSequence: factSequence + 1,
		nextJournalSequence: journalSequence + 1, nextResourceOpSequence: resourceOperationSequence,
	}, nil
}

func (s *CityEconomyService) dispatchCityOpenWorldSupplyChainOrder(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, journalSequence, resourceOperationSequence int64,
	policy *CityOpenWorldSupplyChainPolicy,
	command *CityCommand,
	orderCode string,
) (cityOpenWorldSupplyChainExecution, error) {
	order, err := loadCityOpenWorldSupplyChainOrderForUpdate(ctx, tx, worldID, orderCode)
	if err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	if err = cityOpenWorldSupplyChainActionableAtTick(order, targetTick, cityOpenWorldSupplyChainStateDispatched); err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	if targetTick > order.dispatchDeadlineTick {
		return cityOpenWorldSupplyChainExecution{}, cityOpenWorldSupplyChainReject(cityOpenWorldSupplyChainRejectionOrderNotActionable)
	}
	if err = ensureCityOpenWorldSupplyChainTransitionBudget(ctx, tx, worldID, targetTick, 1, policy); err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	fact, err := insertCityOpenWorldSupplyChainFact(ctx, tx, worldID, targetTick, factSequence, command, &order.code, "order.dispatched", map[string]any{
		"schema_version":   cityOpenWorldSupplyChainSchemaVersion,
		"seller_node_code": order.sellerNodeCode,
	})
	if err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	if err = insertCityOpenWorldSupplyChainTransition(ctx, tx, worldID, targetTick, factSequence,
		order.code, order.state, cityOpenWorldSupplyChainStateDispatched, cityOpenWorldSupplyChainReasonDispatched, fact, false); err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	metadata, err := json.Marshal(map[string]any{
		"schema_version": cityOpenWorldSupplyChainSchemaVersion,
		"source_fact":    map[string]any{"tick": fact.fact.Tick, "sequence": fact.fact.Sequence},
	})
	if err != nil {
		return cityOpenWorldSupplyChainExecution{}, fmt.Errorf("marshal V15 supply-chain dispatch metadata: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_supply_chain_dispatches
    (world_id, order_code, dispatched_tick, source_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5::jsonb)`,
		worldID, order.code, targetTick, fact.id, []byte(metadata)); err != nil {
		return cityOpenWorldSupplyChainExecution{}, fmt.Errorf("insert V15 supply-chain dispatch: %w", err)
	}
	if err = updateCityOpenWorldSupplyChainPolicy(ctx, tx, worldID, cityOpenWorldSupplyChainPolicyDelta{
		facts: 1, dispatches: 1,
	}); err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	if err = assertCityOpenWorldSupplyChainFoundation(ctx, tx, worldID); err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	return cityOpenWorldSupplyChainExecution{
		pending: cityOpenWorldSupplyChainPending(command, order.code, cityOpenWorldSupplyChainStateDispatched, nil, nil),
		facts:   []CityOpenWorldSupplyChainFact{fact.fact}, nextFactSequence: factSequence + 1,
		nextJournalSequence: journalSequence, nextResourceOpSequence: resourceOperationSequence,
	}, nil
}

func insertCityOpenWorldSupplyChainReservationReleases(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	orderCode, reasonCode string,
	fact *cityOpenWorldSupplyChainFactRecord,
) (int64, error) {
	if fact == nil || fact.id <= 0 || !cityOpenWorldSupplyChainReasonValid(reasonCode) {
		return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v15_supply_chain_release"})
	}
	rows, err := tx.QueryContext(ctx, `
SELECT reservation.id, reservation.line_no
FROM city_open_world_supply_chain_reservations reservation
LEFT JOIN city_open_world_supply_chain_reservation_releases release
  ON release.world_id = reservation.world_id AND release.reservation_id = reservation.id
WHERE reservation.world_id = $1 AND reservation.order_code = $2
  AND release.id IS NULL
ORDER BY reservation.line_no
FOR UPDATE OF reservation`, worldID, orderCode)
	if err != nil {
		return 0, fmt.Errorf("lock V15 supply-chain reservations for release: %w", err)
	}
	items := make([]struct {
		id     int64
		lineNo int
	}, 0)
	for rows.Next() {
		item := struct {
			id     int64
			lineNo int
		}{}
		if err = rows.Scan(&item.id, &item.lineNo); err != nil {
			_ = rows.Close()
			return 0, err
		}
		items = append(items, item)
	}
	if err = closeCityRows(rows, "iterate V15 supply-chain reservations for release"); err != nil {
		return 0, err
	}
	if len(items) == 0 {
		return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v15_supply_chain_active_reservations"})
	}
	for _, item := range items {
		metadata, marshalErr := json.Marshal(map[string]any{
			"schema_version": cityOpenWorldSupplyChainSchemaVersion,
			"line_no":        item.lineNo,
		})
		if marshalErr != nil {
			return 0, fmt.Errorf("marshal V15 supply-chain release metadata: %w", marshalErr)
		}
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_supply_chain_reservation_releases
    (world_id, reservation_id, released_tick, reason_code, source_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6::jsonb)`,
			worldID, item.id, targetTick, reasonCode, fact.id, []byte(metadata)); err != nil {
			return 0, fmt.Errorf("insert V15 supply-chain reservation release: %w", err)
		}
	}
	return int64(len(items)), nil
}

func (s *CityEconomyService) deliverCityOpenWorldSupplyChainOrder(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, journalSequence, resourceOperationSequence int64,
	policy *CityOpenWorldSupplyChainPolicy,
	command *CityCommand,
	orderCode string,
) (cityOpenWorldSupplyChainExecution, error) {
	order, err := loadCityOpenWorldSupplyChainOrderForUpdate(ctx, tx, worldID, orderCode)
	if err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	if err = cityOpenWorldSupplyChainActionableAtTick(order, targetTick, cityOpenWorldSupplyChainStateDelivered); err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	if tracked, trackedErr := cityOpenWorldSupplyChainSettlementTracked(ctx, tx, worldID, order.code); trackedErr != nil {
		return cityOpenWorldSupplyChainExecution{}, trackedErr
	} else if tracked {
		return cityOpenWorldSupplyChainExecution{}, cityOpenWorldSupplyChainReject(cityOpenWorldFreightSettlementRejectionSettlementRequired)
	}
	freightReceiptShipment, err := assertCityOpenWorldEnterpriseFreightReceiptReady(ctx, tx, worldID, order.code)
	if err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	freightBatchPlan, err := assertCityOpenWorldFreightBatchReady(ctx, tx, worldID, order.code)
	if err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	if err = ensureCityOpenWorldSupplyChainTransitionBudget(ctx, tx, worldID, targetTick, 1, policy); err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	seller, err := loadCityOpenWorldSupplyChainNodeForUpdate(ctx, tx, worldID, order.sellerNodeCode)
	if err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	lines, err := loadCityOpenWorldSupplyChainLinesForUpdate(ctx, tx, worldID, order.code)
	if err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	fact, err := insertCityOpenWorldSupplyChainFact(ctx, tx, worldID, targetTick, factSequence, command, &order.code, "order.delivered", map[string]any{
		"schema_version":   cityOpenWorldSupplyChainSchemaVersion,
		"seller_node_code": order.sellerNodeCode, "buyer_node_code": order.buyerNodeCode,
	})
	if err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	if err = insertCityOpenWorldSupplyChainTransition(ctx, tx, worldID, targetTick, factSequence,
		order.code, order.state, cityOpenWorldSupplyChainStateDelivered, cityOpenWorldSupplyChainReasonDelivered, fact, false); err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	operationLines := make([]cityResourcePostingLine, 0, len(lines)*2)
	for _, line := range lines {
		source, sourceErr := loadCityInventoryRefByID(ctx, tx, worldID, line.sourceBalanceID)
		if sourceErr != nil {
			return cityOpenWorldSupplyChainExecution{}, sourceErr
		}
		destination, destinationErr := loadCityInventoryRefByID(ctx, tx, worldID, line.destinationBalanceID)
		if destinationErr != nil {
			return cityOpenWorldSupplyChainExecution{}, destinationErr
		}
		if source.resourceID != line.resourceID || destination.resourceID != line.resourceID {
			return cityOpenWorldSupplyChainExecution{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v15_supply_chain_delivery_scope"})
		}
		operationLines = append(operationLines,
			cityResourcePostingLine{balance: source, direction: "out", quantityUnits: line.quantityUnits, memo: "Supply order delivery " + order.code},
			cityResourcePostingLine{balance: destination, direction: "in", quantityUnits: line.quantityUnits, memo: "Supply order delivery " + order.code},
		)
	}
	operation, err := postCityResourceOperation(ctx, tx, cityResourceOperationSpec{
		worldID: worldID, tick: targetTick, sequence: resourceOperationSequence,
		operationKey: "supply:deliver:" + order.code, operationType: "transfer", sourceCommandID: &command.ID,
		actorEntityID: seller.firmID, districtID: seller.districtID,
		description: "Supply order delivery " + order.code,
		metadata: map[string]any{
			"schema_version":    cityOpenWorldSupplyChainSchemaVersion,
			"supply_order_code": order.code, "supply_chain_fact_tick": fact.fact.Tick,
			"supply_chain_fact_sequence": fact.fact.Sequence,
		},
		lines: operationLines,
	})
	if err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	releases, err := insertCityOpenWorldSupplyChainReservationReleases(
		ctx, tx, worldID, targetTick, order.code, cityOpenWorldSupplyChainStateDelivered, fact,
	)
	if err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	metadata, err := json.Marshal(map[string]any{
		"schema_version":          cityOpenWorldSupplyChainSchemaVersion,
		"resource_operation_tick": operation.Tick, "resource_operation_sequence": operation.Sequence,
	})
	if err != nil {
		return cityOpenWorldSupplyChainExecution{}, fmt.Errorf("marshal V15 supply-chain delivery metadata: %w", err)
	}
	var deliveryID int64
	if err = tx.QueryRowContext(ctx, `
INSERT INTO city_open_world_supply_chain_deliveries
    (world_id, order_code, delivered_tick, resource_operation_id, source_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6::jsonb)
RETURNING id`, worldID, order.code, targetTick, operation.ID, fact.id, []byte(metadata)).Scan(&deliveryID); err != nil {
		return cityOpenWorldSupplyChainExecution{}, fmt.Errorf("insert V15 supply-chain delivery: %w", err)
	}
	if err = updateCityOpenWorldSupplyChainPolicy(ctx, tx, worldID, cityOpenWorldSupplyChainPolicyDelta{
		facts: 1, releases: releases, deliveries: 1, activeOrders: -1,
	}); err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	if err = recordCityOpenWorldEnterpriseFreightReceipt(ctx, tx, worldID, freightReceiptShipment, deliveryID, fact, operation); err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	if err = recordCityOpenWorldFreightBatchReceipts(ctx, tx, worldID, freightBatchPlan, deliveryID, fact, operation); err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	if err = assertCityOpenWorldSupplyChainFoundation(ctx, tx, worldID); err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	return cityOpenWorldSupplyChainExecution{
		pending: cityOpenWorldSupplyChainPending(command, order.code, cityOpenWorldSupplyChainStateDelivered, nil, operation),
		facts:   []CityOpenWorldSupplyChainFact{fact.fact}, nextFactSequence: factSequence + 1,
		nextJournalSequence: journalSequence, nextResourceOpSequence: resourceOperationSequence + 1,
	}, nil
}

type cityOpenWorldSupplyChainAcceptanceSettlementRecord struct {
	id        int64
	journalID int64
}

func loadCityOpenWorldSupplyChainAcceptanceSettlementForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	orderCode string,
) (*cityOpenWorldSupplyChainAcceptanceSettlementRecord, error) {
	record := &cityOpenWorldSupplyChainAcceptanceSettlementRecord{}
	err := tx.QueryRowContext(ctx, `
SELECT id, journal_id
FROM city_open_world_supply_chain_settlements
WHERE world_id = $1 AND order_code = $2 AND settlement_kind = 'acceptance'
FOR UPDATE`, worldID, orderCode).Scan(&record.id, &record.journalID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v15_supply_chain_acceptance_settlement"})
	}
	if err != nil {
		return nil, fmt.Errorf("lock V15 supply-chain acceptance settlement: %w", err)
	}
	return record, nil
}

func reverseCityOpenWorldSupplyChainAcceptanceSettlement(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, journalSequence int64,
	ledgerUnit *cityLedgerBaseUnit,
	orderCode, terminalState string,
	command *CityCommand,
	fact *cityOpenWorldSupplyChainFactRecord,
	automatic bool,
) (*CityJournal, error) {
	if ledgerUnit == nil || fact == nil || fact.id <= 0 || !cityOpenWorldSupplyChainStateTerminal(terminalState) ||
		terminalState == cityOpenWorldSupplyChainStateDelivered {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v15_supply_chain_reversal"})
	}
	acceptance, err := loadCityOpenWorldSupplyChainAcceptanceSettlementForUpdate(ctx, tx, worldID, orderCode)
	if err != nil {
		return nil, err
	}
	original, err := loadCityJournalByID(ctx, tx, worldID, acceptance.journalID, true)
	if err != nil {
		return nil, fmt.Errorf("load V15 supply-chain acceptance journal: %w", err)
	}
	if original.JournalType != "purchase" || original.ReversalOfJournalID != nil || len(original.Entries) < 2 {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v15_supply_chain_acceptance_journal"})
	}
	lines := make([]cityLedgerPostingLine, 0, len(original.Entries))
	for _, entry := range original.Entries {
		account, accountErr := loadCityLedgerAccountByID(ctx, tx, worldID, ledgerUnit.id, entry.AccountID)
		if accountErr != nil {
			return nil, accountErr
		}
		lines = append(lines, cityLedgerPostingLine{
			account: account, debitUnits: entry.CreditUnits, creditUnits: entry.DebitUnits,
			memo: "Supply order " + terminalState + " " + orderCode,
		})
	}
	metadata := map[string]any{
		"schema_version":    cityOpenWorldSupplyChainSchemaVersion,
		"supply_order_code": orderCode, "terminal_state": terminalState,
		"acceptance_journal_tick": original.Tick, "acceptance_journal_sequence": original.Sequence,
		"supply_chain_fact_tick": fact.fact.Tick, "supply_chain_fact_sequence": fact.fact.Sequence,
	}
	var sourceCommandID *int64
	if command != nil {
		sourceCommandID = &command.ID
	} else if automatic {
		metadata["system_origin"] = cityOpenWorldSupplyChainSystemExpiryJournalTag
	} else {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v15_supply_chain_reversal_origin"})
	}
	return postCityJournal(ctx, tx, cityLedgerJournalSpec{
		worldID: worldID, unit: ledgerUnit, tick: targetTick, sequence: journalSequence,
		operationKey: "supply:reverse:" + terminalState + ":" + orderCode,
		journalType:  "reversal", sourceCommandID: sourceCommandID, reversalOfJournalID: &original.ID,
		description: "Supply order " + terminalState + " " + orderCode,
		metadata:    metadata, lines: lines,
	})
}

func insertCityOpenWorldSupplyChainReversalSettlement(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	orderCode, terminalState string,
	journal *CityJournal,
	fact *cityOpenWorldSupplyChainFactRecord,
) error {
	if journal == nil || journal.ID <= 0 || fact == nil || fact.id <= 0 ||
		terminalState == cityOpenWorldSupplyChainStateDelivered || !cityOpenWorldSupplyChainStateTerminal(terminalState) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v15_supply_chain_reversal_settlement"})
	}
	acceptance, err := loadCityOpenWorldSupplyChainAcceptanceSettlementForUpdate(ctx, tx, worldID, orderCode)
	if err != nil {
		return err
	}
	metadata, err := json.Marshal(map[string]any{
		"schema_version": cityOpenWorldSupplyChainSchemaVersion,
		"terminal_state": terminalState, "journal_tick": journal.Tick,
		"journal_sequence": journal.Sequence,
	})
	if err != nil {
		return fmt.Errorf("marshal V15 supply-chain reversal settlement metadata: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_supply_chain_settlements
    (world_id, order_code, settlement_kind, journal_id, source_fact_id,
     reversal_of_settlement_id, metadata)
VALUES ($1, $2, 'reversal', $3, $4, $5, $6::jsonb)`,
		worldID, orderCode, journal.ID, fact.id, acceptance.id, []byte(metadata)); err != nil {
		return fmt.Errorf("insert V15 supply-chain reversal settlement: %w", err)
	}
	return nil
}

func (s *CityEconomyService) terminateCityOpenWorldSupplyChainOrder(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, journalSequence, resourceOperationSequence int64,
	ledgerUnit *cityLedgerBaseUnit,
	policy *CityOpenWorldSupplyChainPolicy,
	command *CityCommand,
	order *cityOpenWorldSupplyChainOrderRecord,
	nextState, reasonCode string,
	automatic bool,
) (cityOpenWorldSupplyChainExecution, error) {
	if order == nil || !cityOpenWorldSupplyChainStateTerminal(nextState) || nextState == cityOpenWorldSupplyChainStateDelivered ||
		!cityOpenWorldSupplyChainReasonValid(reasonCode) || policy == nil {
		return cityOpenWorldSupplyChainExecution{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v15_supply_chain_terminal"})
	}
	if !automatic {
		if err := cityOpenWorldSupplyChainActionableAtTick(order, targetTick, nextState); err != nil {
			return cityOpenWorldSupplyChainExecution{}, err
		}
	} else if targetTick <= order.lastTransitionTick ||
		!cityOpenWorldSupplyChainTransitionAllowed(order.state, nextState) {
		return cityOpenWorldSupplyChainExecution{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v15_supply_chain_automatic_terminal"})
	}
	if err := ensureCityOpenWorldSupplyChainTransitionBudget(ctx, tx, worldID, targetTick, 1, policy); err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	factType := "order." + nextState
	fact, err := insertCityOpenWorldSupplyChainFact(ctx, tx, worldID, targetTick, factSequence, command, &order.code, factType, map[string]any{
		"schema_version": cityOpenWorldSupplyChainSchemaVersion,
		"previous_state": order.state, "reason_code": reasonCode, "automatic": automatic,
	})
	if err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	if err = insertCityOpenWorldSupplyChainTransition(ctx, tx, worldID, targetTick, factSequence,
		order.code, order.state, nextState, reasonCode, fact, automatic); err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	delta := cityOpenWorldSupplyChainPolicyDelta{facts: 1, activeOrders: -1}
	var journal *CityJournal
	// A dispatched order has necessarily passed through accepted under the
	// append-only state machine.  It therefore still owns the acceptance
	// reservation and purchase journal even though its *current* state is no
	// longer "accepted".  Treating only the immediate accepted state as
	// reversible would leave failed freight orders with locked stock and an
	// irreversible payment.
	acceptedPreviously := order.state == cityOpenWorldSupplyChainStateAccepted ||
		order.state == cityOpenWorldSupplyChainStateDispatched
	if acceptedPreviously {
		releases, releaseErr := insertCityOpenWorldSupplyChainReservationReleases(
			ctx, tx, worldID, targetTick, order.code, nextState, fact,
		)
		if releaseErr != nil {
			return cityOpenWorldSupplyChainExecution{}, releaseErr
		}
		delta.releases = releases
		journal, err = reverseCityOpenWorldSupplyChainAcceptanceSettlement(
			ctx, tx, worldID, targetTick, journalSequence, ledgerUnit, order.code,
			nextState, command, fact, automatic,
		)
		if err != nil {
			return cityOpenWorldSupplyChainExecution{}, err
		}
		if err = insertCityOpenWorldSupplyChainReversalSettlement(ctx, tx, worldID, order.code, nextState, journal, fact); err != nil {
			return cityOpenWorldSupplyChainExecution{}, err
		}
		delta.settlements = 1
	}
	if err = updateCityOpenWorldSupplyChainPolicy(ctx, tx, worldID, delta); err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	if err = assertCityOpenWorldSupplyChainFoundation(ctx, tx, worldID); err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	execution := cityOpenWorldSupplyChainExecution{
		facts: []CityOpenWorldSupplyChainFact{fact.fact}, nextFactSequence: factSequence + 1,
		nextJournalSequence: journalSequence, nextResourceOpSequence: resourceOperationSequence,
	}
	if journal != nil {
		execution.nextJournalSequence++
	}
	if command != nil {
		execution.pending = cityOpenWorldSupplyChainPending(command, order.code, nextState, journal, nil)
	}
	return execution, nil
}

func (s *CityEconomyService) cancelCityOpenWorldSupplyChainOrder(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, journalSequence, resourceOperationSequence int64,
	ledgerUnit *cityLedgerBaseUnit,
	policy *CityOpenWorldSupplyChainPolicy,
	command *CityCommand,
	orderCode string,
) (cityOpenWorldSupplyChainExecution, error) {
	order, err := loadCityOpenWorldSupplyChainOrderForUpdate(ctx, tx, worldID, orderCode)
	if err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	if order.state != cityOpenWorldSupplyChainStateProposed && order.state != cityOpenWorldSupplyChainStateAccepted {
		return cityOpenWorldSupplyChainExecution{}, cityOpenWorldSupplyChainReject(cityOpenWorldSupplyChainRejectionOrderNotActionable)
	}
	return s.terminateCityOpenWorldSupplyChainOrder(
		ctx, tx, worldID, targetTick, factSequence, journalSequence, resourceOperationSequence,
		ledgerUnit, policy, command, order, cityOpenWorldSupplyChainStateCancelled,
		cityOpenWorldSupplyChainReasonCancelled, false,
	)
}

func (s *CityEconomyService) failCityOpenWorldSupplyChainOrder(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, journalSequence, resourceOperationSequence int64,
	ledgerUnit *cityLedgerBaseUnit,
	policy *CityOpenWorldSupplyChainPolicy,
	command *CityCommand,
	orderCode string,
) (cityOpenWorldSupplyChainExecution, error) {
	order, err := loadCityOpenWorldSupplyChainOrderForUpdate(ctx, tx, worldID, orderCode)
	if err != nil {
		return cityOpenWorldSupplyChainExecution{}, err
	}
	if order.state != cityOpenWorldSupplyChainStateDispatched {
		return cityOpenWorldSupplyChainExecution{}, cityOpenWorldSupplyChainReject(cityOpenWorldSupplyChainRejectionOrderNotActionable)
	}
	// V22 changes a dispatched order from an atomic delivery contract into an
	// append-only quantity settlement contract. A legacy whole-order failure is
	// safe only until the first V22 receipt has resolved cargo. In that narrow
	// no-receipt window V15 remains responsible for its established reversal,
	// then V22 records an explicit voided overlay without falsifying custody.
	closure, closureErr := prepareCityOpenWorldFreightSettlementFailureClosure(ctx, tx, worldID, order.code)
	if closureErr != nil {
		return cityOpenWorldSupplyChainExecution{}, closureErr
	}
	execution, terminateErr := s.terminateCityOpenWorldSupplyChainOrder(
		ctx, tx, worldID, targetTick, factSequence, journalSequence, resourceOperationSequence,
		ledgerUnit, policy, command, order, cityOpenWorldSupplyChainStateFailed,
		cityOpenWorldSupplyChainReasonFailed, false,
	)
	if terminateErr != nil {
		return cityOpenWorldSupplyChainExecution{}, terminateErr
	}
	if closure != nil {
		if err = closeCityOpenWorldFreightSettlementForFailedSupplyOrder(ctx, tx, worldID, targetTick, command, closure); err != nil {
			return cityOpenWorldSupplyChainExecution{}, err
		}
		annotateCityOpenWorldSupplyChainFailureWithFreightSettlementClosure(&execution.pending, closure)
	}
	return execution, nil
}

type cityOpenWorldSupplyChainAutomaticExecution struct {
	facts                  []CityOpenWorldSupplyChainFact
	events                 []worldRuntimeAutomaticEvent
	nextFactSequence       int64
	nextJournalSequence    int64
	nextResourceOpSequence int64
}

// advanceCityOpenWorldV15SupplyChain expires only pre-dispatch orders whose
// contractual response window has elapsed.  It intentionally runs before
// command execution for the tick: an expired order cannot be revived by a
// same-tick command, and a command submitted at the deadline remains valid.
func (s *CityEconomyService) advanceCityOpenWorldV15SupplyChain(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, journalSequence, resourceOperationSequence int64,
	ledgerUnit *cityLedgerBaseUnit,
) (cityOpenWorldSupplyChainAutomaticExecution, error) {
	execution := cityOpenWorldSupplyChainAutomaticExecution{
		facts: make([]CityOpenWorldSupplyChainFact, 0), events: make([]worldRuntimeAutomaticEvent, 0),
		nextFactSequence: factSequence, nextJournalSequence: journalSequence,
		nextResourceOpSequence: resourceOperationSequence,
	}
	if ledgerUnit == nil {
		return execution, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v15_supply_chain_ledger_unit"})
	}
	if err := ensureCityOpenWorldSupplyChainEngine(ctx, tx, worldID); err != nil {
		return execution, err
	}
	if err := activateCityOpenWorldSupplyChainWrite(ctx, tx, worldID); err != nil {
		return execution, err
	}
	policy, err := loadCityOpenWorldSupplyChainPolicyForUpdate(ctx, tx, worldID)
	if err != nil {
		return execution, err
	}
	var usedTransitions int64
	if err = tx.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM city_open_world_supply_chain_order_transitions
WHERE world_id = $1 AND transition_tick = $2`, worldID, targetTick).Scan(&usedTransitions); err != nil {
		return execution, fmt.Errorf("count V15 supply-chain automatic transitions: %w", err)
	}
	remaining := int64(policy.MaximumTransitionsPerTick) - usedTransitions
	if remaining < 0 {
		return execution, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v15_supply_chain_transition_budget"})
	}
	if remaining == 0 {
		return execution, nil
	}
	rows, err := tx.QueryContext(ctx, `
SELECT header.code
FROM city_open_world_supply_chain_orders header
JOIN LATERAL (
    SELECT transition.state
    FROM city_open_world_supply_chain_order_transitions transition
    WHERE transition.world_id = header.world_id AND transition.order_code = header.code
    ORDER BY transition.transition_tick DESC, transition.transition_sequence DESC
    LIMIT 1
) current_state ON TRUE
WHERE header.world_id = $1
  AND ((current_state.state = 'proposed' AND $2 > header.accept_deadline_tick)
    OR (current_state.state = 'accepted' AND $2 > header.dispatch_deadline_tick))
ORDER BY header.created_tick, header.code
LIMIT $3
FOR UPDATE OF header`, worldID, targetTick, remaining)
	if err != nil {
		return execution, fmt.Errorf("load V15 supply-chain expiry candidates: %w", err)
	}
	orderCodes := make([]string, 0)
	for rows.Next() {
		var code string
		if err = rows.Scan(&code); err != nil {
			_ = rows.Close()
			return execution, err
		}
		orderCodes = append(orderCodes, code)
	}
	if err = closeCityRows(rows, "iterate V15 supply-chain expiry candidates"); err != nil {
		return execution, err
	}
	for _, orderCode := range orderCodes {
		order, loadErr := loadCityOpenWorldSupplyChainOrderForUpdate(ctx, tx, worldID, orderCode)
		if loadErr != nil {
			return execution, loadErr
		}
		if (order.state == cityOpenWorldSupplyChainStateProposed && targetTick <= order.acceptDeadlineTick) ||
			(order.state == cityOpenWorldSupplyChainStateAccepted && targetTick <= order.dispatchDeadlineTick) {
			continue
		}
		if order.state != cityOpenWorldSupplyChainStateProposed && order.state != cityOpenWorldSupplyChainStateAccepted {
			continue
		}
		terminal, terminalErr := s.terminateCityOpenWorldSupplyChainOrder(
			ctx, tx, worldID, targetTick, execution.nextFactSequence, execution.nextJournalSequence,
			execution.nextResourceOpSequence, ledgerUnit, policy, nil, order,
			cityOpenWorldSupplyChainStateExpired, cityOpenWorldSupplyChainReasonExpired, true,
		)
		if terminalErr != nil {
			return execution, terminalErr
		}
		execution.facts = append(execution.facts, terminal.facts...)
		execution.nextFactSequence = terminal.nextFactSequence
		execution.nextJournalSequence = terminal.nextJournalSequence
		execution.nextResourceOpSequence = terminal.nextResourceOpSequence
		execution.events = append(execution.events, worldRuntimeAutomaticEvent{
			eventType: "city.open_world.supply_order.expired",
			payload: map[string]any{
				"order_code": order.code, "previous_state": order.state,
				"tick": targetTick,
			},
		})
	}
	if err = assertCityOpenWorldSupplyChainFoundation(ctx, tx, worldID); err != nil {
		return execution, err
	}
	return execution, nil
}
