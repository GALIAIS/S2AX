package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

type cityOpenWorldEnterpriseFreightReceiptPolicyDelta struct {
	shipments       int64
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

type cityOpenWorldEnterpriseFreightReceiptShipmentRecord struct {
	id                  int64
	code                string
	freightSourceCode   string
	orderCode           string
	sellerNodeCode      string
	buyerNodeCode       string
	sourceHubCode       string
	destinationHubCode  string
	sourceTick          int64
	requestedUnits      int64
	state               string
	sourceFreightFactID int64
	lastReceiptFactID   sql.NullInt64
	version             int64
}

type cityOpenWorldEnterpriseFreightReceiptCandidate struct {
	freightSourceCode   string
	orderCode           string
	sellerNodeCode      string
	buyerNodeCode       string
	sourceHubCode       string
	destinationHubCode  string
	sourceTick          int64
	requestedUnits      int64
	sourceFreightFactID int64
	sourceFactTick      int64
	sourceFactSequence  int64
}

type cityOpenWorldEnterpriseFreightReceiptLineCandidate struct {
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

type cityOpenWorldEnterpriseFreightReceiptFreightTransitionCandidate struct {
	freightFactID int64
	tick          int64
	sequence      int64
	factType      string
	state         string
	reasonCode    string
}

func loadCityOpenWorldEnterpriseFreightReceiptPolicyForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
) (*CityOpenWorldEnterpriseFreightReceiptPolicy, error) {
	policy := &CityOpenWorldEnterpriseFreightReceiptPolicy{}
	err := tx.QueryRowContext(ctx, `
SELECT profile_id, profile_version, content_hash, baseline_tick,
       shipment_contract, receipt_contract, legacy_contract,
       maximum_shipments, maximum_observations_per_tick,
       shipment_count, awaiting_route_count, in_transit_count, awaiting_receipt_count,
       received_count, settled_count, expired_count, voided_count, orphaned_count, fact_count,
       transition_count, receipt_count, revision, metadata
FROM city_open_world_enterprise_freight_receipt_profiles
WHERE world_id = $1
FOR UPDATE`, worldID).Scan(
		&policy.ProfileID, &policy.ProfileVersion, &policy.ContentHash, &policy.BaselineTick,
		&policy.ShipmentContract, &policy.ReceiptContract, &policy.LegacyContract,
		&policy.MaximumShipments, &policy.MaximumObservationsPerTick,
		&policy.ShipmentCount, &policy.AwaitingRouteCount, &policy.InTransitCount,
		&policy.AwaitingReceiptCount, &policy.ReceivedCount, &policy.SettledCount, &policy.ExpiredCount,
		&policy.VoidedCount, &policy.OrphanedCount, &policy.FactCount,
		&policy.TransitionCount, &policy.ReceiptCount, &policy.Revision, &policy.Metadata,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v17_freight_receipt_profile"})
	}
	if err != nil {
		return nil, fmt.Errorf("lock V17 freight-receipt profile: %w", err)
	}
	hash, hashErr := cityOpenWorldEnterpriseFreightReceiptPolicyHash()
	if hashErr != nil || policy.ProfileID != cityOpenWorldEnterpriseFreightReceiptProfileID ||
		policy.ProfileVersion != cityOpenWorldEnterpriseFreightReceiptProfileVersion ||
		policy.ContentHash != hash || policy.BaselineTick < 0 ||
		policy.ShipmentContract != cityOpenWorldEnterpriseFreightReceiptShipmentContract ||
		policy.ReceiptContract != cityOpenWorldEnterpriseFreightReceiptReceiptContract ||
		policy.LegacyContract != cityOpenWorldEnterpriseFreightReceiptLegacyContract ||
		policy.MaximumShipments != cityOpenWorldEnterpriseFreightReceiptMaximumShipments ||
		policy.MaximumObservationsPerTick != cityOpenWorldEnterpriseFreightReceiptMaximumObservationsTick ||
		policy.Revision < 1 || !cityOpenWorldEnterpriseFreightReceiptPolicyMetadataValid(policy.Metadata) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v17_freight_receipt_profile"})
	}
	return policy, nil
}

func updateCityOpenWorldEnterpriseFreightReceiptPolicy(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	delta cityOpenWorldEnterpriseFreightReceiptPolicyDelta,
) error {
	result, err := tx.ExecContext(ctx, `
UPDATE city_open_world_enterprise_freight_receipt_profiles
SET shipment_count = shipment_count + $2,
    awaiting_route_count = awaiting_route_count + $3,
    in_transit_count = in_transit_count + $4,
    awaiting_receipt_count = awaiting_receipt_count + $5,
    received_count = received_count + $6,
    settled_count = settled_count + $7,
    expired_count = expired_count + $8,
    voided_count = voided_count + $9,
    orphaned_count = orphaned_count + $10,
    fact_count = fact_count + $11,
    transition_count = transition_count + $12,
    receipt_count = receipt_count + $13,
    revision = revision + 1,
    updated_at = NOW()
WHERE world_id = $1`, worldID, delta.shipments, delta.awaitingRoute, delta.inTransit,
		delta.awaitingReceipt, delta.received, delta.settled, delta.expired, delta.voided, delta.orphaned,
		delta.facts, delta.transitions, delta.receipts)
	if err != nil {
		return fmt.Errorf("update V17 freight-receipt profile: %w", err)
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr != nil || rows != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v17_freight_receipt_profile"})
	}
	return nil
}

// advanceCityOpenWorldV17EnterpriseFreightReceipts only projects predecessor
// evidence. It runs after V16 and before V9 scheduling, so it can never turn a
// same-tick network decision into an inventory receipt.
func advanceCityOpenWorldV17EnterpriseFreightReceipts(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
) error {
	policy, err := loadCityOpenWorldEnterpriseFreightReceiptPolicyForUpdate(ctx, tx, worldID)
	if err != nil {
		return err
	}
	if targetTick <= policy.BaselineTick {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v17_freight_receipt_baseline"})
	}
	if err = activateCityOpenWorldRuntimeFactWrite(ctx, tx, worldID); err != nil {
		return err
	}
	if err = activateCityOpenWorldEnterpriseFreightReceiptWrite(ctx, tx, worldID); err != nil {
		return err
	}
	if err = syncCityOpenWorldEnterpriseFreightReceiptShipments(ctx, tx, worldID, policy, policy.MaximumObservationsPerTick); err != nil {
		return err
	}
	if err = createCityOpenWorldEnterpriseFreightReceiptShipments(ctx, tx, worldID, policy, policy.MaximumObservationsPerTick); err != nil {
		return err
	}
	return assertCityOpenWorldEnterpriseFreightReceiptFoundation(ctx, tx, worldID)
}

func syncCityOpenWorldEnterpriseFreightReceiptShipments(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	policy *CityOpenWorldEnterpriseFreightReceiptPolicy,
	limit int,
) error {
	if policy == nil || limit < 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v17_freight_receipt_sync"})
	}
	rows, err := tx.QueryContext(ctx, `
SELECT code
FROM city_open_world_enterprise_freight_shipments
WHERE world_id = $1
  AND state IN ('awaiting_route', 'in_transit', 'awaiting_receipt')
ORDER BY source_tick, code
LIMIT $2
FOR UPDATE`, worldID, limit)
	if err != nil {
		return fmt.Errorf("load V17 freight-receipt shipment sync candidates: %w", err)
	}
	codes := make([]string, 0)
	for rows.Next() {
		var code string
		if err = rows.Scan(&code); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan V17 freight-receipt shipment sync candidate: %w", err)
		}
		codes = append(codes, code)
	}
	if err = closeCityRows(rows, "iterate V17 freight-receipt shipment sync candidates"); err != nil {
		return err
	}
	for _, code := range codes {
		shipment, loadErr := loadCityOpenWorldEnterpriseFreightReceiptShipmentForUpdate(ctx, tx, worldID, code)
		if loadErr != nil {
			return loadErr
		}
		if err = syncCityOpenWorldEnterpriseFreightReceiptShipment(ctx, tx, worldID, policy, shipment); err != nil {
			return err
		}
	}
	return nil
}

func createCityOpenWorldEnterpriseFreightReceiptShipments(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	policy *CityOpenWorldEnterpriseFreightReceiptPolicy,
	limit int,
) error {
	if policy == nil || limit < 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v17_freight_receipt_create"})
	}
	remaining := policy.MaximumShipments - int(policy.ShipmentCount)
	if remaining <= 0 {
		return nil
	}
	if remaining > limit {
		remaining = limit
	}
	candidates, err := loadCityOpenWorldEnterpriseFreightReceiptCandidates(ctx, tx, worldID, policy.BaselineTick, remaining)
	if err != nil {
		return err
	}
	for _, candidate := range candidates {
		if err = createCityOpenWorldEnterpriseFreightReceiptShipment(ctx, tx, worldID, policy, candidate); err != nil {
			return err
		}
	}
	return nil
}

func loadCityOpenWorldEnterpriseFreightReceiptCandidates(
	ctx context.Context,
	tx *sql.Tx,
	worldID, baselineTick int64,
	limit int,
) ([]cityOpenWorldEnterpriseFreightReceiptCandidate, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT source.code, source.order_code, source.seller_node_code, source.buyer_node_code,
       source.source_hub_code, source.destination_hub_code, source.source_tick,
       source.requested_units, source_fact.id, source_fact.tick, source_fact.sequence
FROM city_open_world_enterprise_freight_sources source
JOIN city_open_world_enterprise_freight_facts source_fact
  ON source_fact.world_id = source.world_id AND source_fact.source_code = source.code
 AND source_fact.fact_type = 'source.created'
WHERE source.world_id = $1
  AND source.source_tick > $2
  AND source.state <> 'suppressed'
  AND NOT EXISTS (
      SELECT 1 FROM city_open_world_enterprise_freight_shipments shipment
      WHERE shipment.world_id = source.world_id AND shipment.freight_source_code = source.code
  )
ORDER BY source.source_tick, source.code
LIMIT $3
FOR UPDATE OF source`, worldID, baselineTick, limit)
	if err != nil {
		return nil, fmt.Errorf("load V17 freight-receipt source candidates: %w", err)
	}
	items := make([]cityOpenWorldEnterpriseFreightReceiptCandidate, 0)
	for rows.Next() {
		item := cityOpenWorldEnterpriseFreightReceiptCandidate{}
		if err = rows.Scan(&item.freightSourceCode, &item.orderCode, &item.sellerNodeCode,
			&item.buyerNodeCode, &item.sourceHubCode, &item.destinationHubCode,
			&item.sourceTick, &item.requestedUnits, &item.sourceFreightFactID,
			&item.sourceFactTick, &item.sourceFactSequence); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan V17 freight-receipt source candidate: %w", err)
		}
		items = append(items, item)
	}
	if err = closeCityRows(rows, "iterate V17 freight-receipt source candidates"); err != nil {
		return nil, err
	}
	return items, nil
}

func loadCityOpenWorldEnterpriseFreightReceiptLineCandidates(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	freightSourceCode string,
) ([]cityOpenWorldEnterpriseFreightReceiptLineCandidate, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT line_no, resource_code, source_firm_code, source_district_code,
       destination_firm_code, destination_district_code, quantity_units,
       unit_price_units, total_price_units
FROM city_open_world_enterprise_freight_source_lines
WHERE world_id = $1 AND source_code = $2
ORDER BY line_no
FOR UPDATE`, worldID, freightSourceCode)
	if err != nil {
		return nil, fmt.Errorf("load V17 freight-receipt source lines: %w", err)
	}
	items := make([]cityOpenWorldEnterpriseFreightReceiptLineCandidate, 0)
	for rows.Next() {
		item := cityOpenWorldEnterpriseFreightReceiptLineCandidate{}
		if err = rows.Scan(&item.lineNo, &item.resourceCode, &item.sourceFirmCode,
			&item.sourceDistrictCode, &item.destinationFirmCode, &item.destinationDistrictCode,
			&item.quantityUnits, &item.unitPriceUnits, &item.totalPriceUnits); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan V17 freight-receipt source line: %w", err)
		}
		items = append(items, item)
	}
	if err = closeCityRows(rows, "iterate V17 freight-receipt source lines"); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v17_freight_receipt_lines"})
	}
	return items, nil
}

func createCityOpenWorldEnterpriseFreightReceiptShipment(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	policy *CityOpenWorldEnterpriseFreightReceiptPolicy,
	candidate cityOpenWorldEnterpriseFreightReceiptCandidate,
) error {
	if candidate.freightSourceCode == "" || candidate.orderCode == "" || candidate.sourceFreightFactID <= 0 ||
		candidate.sourceTick <= policy.BaselineTick || candidate.requestedUnits <= 0 ||
		candidate.sourceFactTick <= 0 || candidate.sourceFactSequence <= 0 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v17_freight_receipt_shipment"})
	}
	lines, err := loadCityOpenWorldEnterpriseFreightReceiptLineCandidates(ctx, tx, worldID, candidate.freightSourceCode)
	if err != nil {
		return err
	}
	var total int64
	for _, line := range lines {
		if line.quantityUnits <= 0 || line.quantityUnits > candidate.requestedUnits-total {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v17_freight_receipt_shipment_lines"})
		}
		total += line.quantityUnits
	}
	if total != candidate.requestedUnits {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v17_freight_receipt_shipment_lines"})
	}
	shipmentCode := cityOpenWorldEnterpriseFreightShipmentCode(candidate.freightSourceCode)
	metadata, err := json.Marshal(map[string]any{
		"schema_version":      cityOpenWorldEnterpriseFreightReceiptSchemaVersion,
		"source_contract":     cityOpenWorldEnterpriseFreightReceiptShipmentContract,
		"freight_source_code": candidate.freightSourceCode,
		"order_code":          candidate.orderCode,
		"inventory":           "v15_only_until_delivery",
	})
	if err != nil {
		return fmt.Errorf("marshal V17 freight-receipt shipment metadata: %w", err)
	}
	var shipmentID int64
	if err = tx.QueryRowContext(ctx, `
INSERT INTO city_open_world_enterprise_freight_shipments
    (world_id, code, freight_source_code, order_code, seller_node_code,
     buyer_node_code, source_hub_code, destination_hub_code, source_tick,
     requested_units, state, source_freight_fact_id, last_receipt_fact_id,
     version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 'awaiting_route', $11,
        NULL, 0, $12::jsonb)
RETURNING id`, worldID, shipmentCode, candidate.freightSourceCode, candidate.orderCode,
		candidate.sellerNodeCode, candidate.buyerNodeCode, candidate.sourceHubCode,
		candidate.destinationHubCode, candidate.sourceTick, candidate.requestedUnits,
		candidate.sourceFreightFactID, []byte(metadata)).Scan(&shipmentID); err != nil {
		return fmt.Errorf("insert V17 freight-receipt shipment: %w", err)
	}
	_ = shipmentID
	for _, line := range lines {
		lineMetadata, metadataErr := json.Marshal(map[string]any{
			"schema_version":    cityOpenWorldEnterpriseFreightReceiptSchemaVersion,
			"snapshot_contract": "v16_source_line_snapshot_v1",
		})
		if metadataErr != nil {
			return metadataErr
		}
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_enterprise_freight_shipment_lines
    (world_id, shipment_code, line_no, resource_code, source_firm_code,
     source_district_code, destination_firm_code, destination_district_code,
     quantity_units, unit_price_units, total_price_units, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb)`,
			worldID, shipmentCode, line.lineNo, line.resourceCode, line.sourceFirmCode,
			line.sourceDistrictCode, line.destinationFirmCode, line.destinationDistrictCode,
			line.quantityUnits, line.unitPriceUnits, line.totalPriceUnits, []byte(lineMetadata)); err != nil {
			return fmt.Errorf("insert V17 freight-receipt shipment line: %w", err)
		}
	}
	rootFactID, err := insertCityOpenWorldEnterpriseFreightReceiptFreightFact(ctx, tx, worldID,
		shipmentCode, candidate.sourceFreightFactID, candidate.sourceFactTick, candidate.sourceFactSequence,
		candidate.freightSourceCode, "shipment.created", map[string]any{
			"schema_version":      cityOpenWorldEnterpriseFreightReceiptSchemaVersion,
			"freight_source_code": candidate.freightSourceCode,
			"order_code":          candidate.orderCode,
		})
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `
UPDATE city_open_world_enterprise_freight_shipments
SET last_receipt_fact_id = $3, updated_at = NOW()
WHERE world_id = $1 AND code = $2 AND last_receipt_fact_id IS NULL`, worldID, shipmentCode, rootFactID); err != nil {
		return fmt.Errorf("set V17 freight-receipt shipment root: %w", err)
	}
	if err = updateCityOpenWorldEnterpriseFreightReceiptPolicy(ctx, tx, worldID,
		cityOpenWorldEnterpriseFreightReceiptPolicyDelta{
			shipments: 1, awaitingRoute: 1, facts: 1,
		}); err != nil {
		return err
	}
	shipment, err := loadCityOpenWorldEnterpriseFreightReceiptShipmentForUpdate(ctx, tx, worldID, shipmentCode)
	if err != nil {
		return err
	}
	return syncCityOpenWorldEnterpriseFreightReceiptShipment(ctx, tx, worldID, policy, shipment)
}

func loadCityOpenWorldEnterpriseFreightReceiptShipmentForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	shipmentCode string,
) (*cityOpenWorldEnterpriseFreightReceiptShipmentRecord, error) {
	record := &cityOpenWorldEnterpriseFreightReceiptShipmentRecord{}
	err := tx.QueryRowContext(ctx, `
SELECT id, code, freight_source_code, order_code, seller_node_code, buyer_node_code,
       source_hub_code, destination_hub_code, source_tick, requested_units,
       state, source_freight_fact_id, last_receipt_fact_id, version
FROM city_open_world_enterprise_freight_shipments
WHERE world_id = $1 AND code = $2
FOR UPDATE`, worldID, shipmentCode).Scan(
		&record.id, &record.code, &record.freightSourceCode, &record.orderCode,
		&record.sellerNodeCode, &record.buyerNodeCode, &record.sourceHubCode,
		&record.destinationHubCode, &record.sourceTick, &record.requestedUnits,
		&record.state, &record.sourceFreightFactID, &record.lastReceiptFactID, &record.version,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v17_freight_receipt_shipment"})
	}
	if err != nil {
		return nil, fmt.Errorf("lock V17 freight-receipt shipment %s: %w", shipmentCode, err)
	}
	return record, nil
}

func loadCityOpenWorldEnterpriseFreightReceiptFreightTransitions(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	freightSourceCode string,
) ([]cityOpenWorldEnterpriseFreightReceiptFreightTransitionCandidate, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT fact.id, fact.tick, fact.sequence, fact.fact_type, transition.state, transition.reason_code
FROM city_open_world_enterprise_freight_transitions transition
JOIN city_open_world_enterprise_freight_facts fact
  ON fact.id = transition.source_fact_id AND fact.world_id = transition.world_id
WHERE transition.world_id = $1 AND transition.source_code = $2
ORDER BY transition.transition_tick, transition.transition_sequence`, worldID, freightSourceCode)
	if err != nil {
		return nil, fmt.Errorf("load V17 freight-receipt freight transitions: %w", err)
	}
	items := make([]cityOpenWorldEnterpriseFreightReceiptFreightTransitionCandidate, 0)
	for rows.Next() {
		item := cityOpenWorldEnterpriseFreightReceiptFreightTransitionCandidate{}
		if err = rows.Scan(&item.freightFactID, &item.tick, &item.sequence,
			&item.factType, &item.state, &item.reasonCode); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan V17 freight-receipt freight transition: %w", err)
		}
		items = append(items, item)
	}
	if err = closeCityRows(rows, "iterate V17 freight-receipt freight transitions"); err != nil {
		return nil, err
	}
	return items, nil
}

func syncCityOpenWorldEnterpriseFreightReceiptShipment(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	policy *CityOpenWorldEnterpriseFreightReceiptPolicy,
	shipment *cityOpenWorldEnterpriseFreightReceiptShipmentRecord,
) error {
	if policy == nil || shipment == nil || shipment.code == "" || shipment.sourceTick <= policy.BaselineTick {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v17_freight_receipt_sync_shipment"})
	}
	transitions, err := loadCityOpenWorldEnterpriseFreightReceiptFreightTransitions(ctx, tx, worldID, shipment.freightSourceCode)
	if err != nil {
		return err
	}
	for _, transition := range transitions {
		nextState, reasonCode, factType, ok := cityOpenWorldEnterpriseFreightReceiptTransitionFromFreight(transition.state)
		if !ok {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v17_freight_receipt_freight_state"})
		}
		var alreadyObserved bool
		if err = tx.QueryRowContext(ctx, `
SELECT EXISTS(
    SELECT 1
    FROM city_open_world_enterprise_freight_receipt_facts
    WHERE world_id = $1 AND freight_fact_id = $2
)`, worldID, transition.freightFactID).Scan(&alreadyObserved); err != nil {
			return fmt.Errorf("check V17 freight-receipt observation: %w", err)
		}
		if alreadyObserved {
			continue
		}
		if !cityOpenWorldEnterpriseFreightReceiptTransitionAllowed(shipmentStateBeforeFirstTransition(shipment), nextState) {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v17_freight_receipt_transition"})
		}
		factID, insertErr := insertCityOpenWorldEnterpriseFreightReceiptFreightFact(ctx, tx, worldID,
			shipment.code, transition.freightFactID, transition.tick, transition.sequence,
			shipment.freightSourceCode, factType, map[string]any{
				"schema_version":      cityOpenWorldEnterpriseFreightReceiptSchemaVersion,
				"freight_state":       transition.state,
				"freight_reason_code": transition.reasonCode,
			})
		if insertErr != nil {
			return insertErr
		}
		if err = transitionCityOpenWorldEnterpriseFreightReceiptShipment(ctx, tx, worldID, shipment,
			nextState, reasonCode, factID,
			CityOpenWorldEnterpriseFreightReceiptFactRef{EvidenceKind: cityOpenWorldEnterpriseFreightReceiptEvidenceFreight, Tick: transition.tick, Sequence: transition.sequence}); err != nil {
			return err
		}
		if err = updateCityOpenWorldEnterpriseFreightReceiptPolicy(ctx, tx, worldID,
			cityOpenWorldEnterpriseFreightReceiptStateTransitionDelta(shipment.state, nextState, 1, 1, 0)); err != nil {
			return err
		}
		shipment.state = nextState
		shipment.version++
		shipment.lastReceiptFactID = sql.NullInt64{Int64: factID, Valid: true}
	}
	return nil
}

func shipmentStateBeforeFirstTransition(shipment *cityOpenWorldEnterpriseFreightReceiptShipmentRecord) string {
	if shipment == nil || shipment.version == 0 {
		return ""
	}
	return shipment.state
}

func cityOpenWorldEnterpriseFreightReceiptTransitionFromFreight(freightState string) (string, string, string, bool) {
	switch freightState {
	case cityOpenWorldEnterpriseFreightStateDemandPending:
		return cityOpenWorldEnterpriseFreightReceiptStateAwaitingRoute, cityOpenWorldEnterpriseFreightReceiptReasonDemandPending, "route.awaiting", true
	case cityOpenWorldEnterpriseFreightStateRouteScheduled:
		return cityOpenWorldEnterpriseFreightReceiptStateInTransit, cityOpenWorldEnterpriseFreightReceiptReasonScheduled, "transport.in_transit", true
	case cityOpenWorldEnterpriseFreightStateRouteCompleted:
		return cityOpenWorldEnterpriseFreightReceiptStateAwaitingReceipt, cityOpenWorldEnterpriseFreightReceiptReasonCompleted, "transport.arrived", true
	case cityOpenWorldEnterpriseFreightStateDemandExpired:
		return cityOpenWorldEnterpriseFreightReceiptStateExpired, cityOpenWorldEnterpriseFreightReceiptReasonExpired, "transport.expired", true
	case cityOpenWorldEnterpriseFreightStateVoided:
		return cityOpenWorldEnterpriseFreightReceiptStateVoided, cityOpenWorldEnterpriseFreightReceiptReasonVoided, "transport.voided", true
	case cityOpenWorldEnterpriseFreightStateTransportOrphaned:
		return cityOpenWorldEnterpriseFreightReceiptStateOrphaned, cityOpenWorldEnterpriseFreightReceiptReasonOrphaned, "transport.orphaned", true
	default:
		return "", "", "", false
	}
}

func insertCityOpenWorldEnterpriseFreightReceiptFreightFact(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	shipmentCode string,
	freightFactID, tick, sequence int64,
	freightSourceCode, factType string,
	payload map[string]any,
) (int64, error) {
	if freightFactID <= 0 || tick <= 0 || sequence <= 0 || !cityOpenWorldEnterpriseFreightReceiptFactTypeValid(factType) || payload == nil {
		return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v17_freight_receipt_fact"})
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	var id int64
	if err = tx.QueryRowContext(ctx, `
INSERT INTO city_open_world_enterprise_freight_receipt_facts
    (world_id, shipment_code, tick, sequence, fact_type, evidence_kind,
     freight_fact_id, supply_chain_fact_id, payload)
VALUES ($1, $2, $3, $4, $5, 'enterprise_freight', $6, NULL, $7::jsonb)
RETURNING id`, worldID, shipmentCode, tick, sequence, factType, freightFactID, []byte(raw)).Scan(&id); err != nil {
		return 0, fmt.Errorf("insert V17 freight-receipt freight fact %s/%s: %w", shipmentCode, freightSourceCode, err)
	}
	return id, nil
}

func insertCityOpenWorldEnterpriseFreightReceiptSupplyFact(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	shipmentCode string,
	supplyFact *cityOpenWorldSupplyChainFactRecord,
	factType string,
	payload map[string]any,
) (int64, error) {
	if supplyFact == nil || supplyFact.id <= 0 || supplyFact.fact.Tick <= 0 || supplyFact.fact.Sequence <= 0 ||
		!cityOpenWorldEnterpriseFreightReceiptFactTypeValid(factType) || payload == nil {
		return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v17_freight_receipt_supply_fact"})
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	var id int64
	if err = tx.QueryRowContext(ctx, `
INSERT INTO city_open_world_enterprise_freight_receipt_facts
    (world_id, shipment_code, tick, sequence, fact_type, evidence_kind,
     freight_fact_id, supply_chain_fact_id, payload)
VALUES ($1, $2, $3, $4, $5, 'supply_chain', NULL, $6, $7::jsonb)
RETURNING id`, worldID, shipmentCode, supplyFact.fact.Tick, supplyFact.fact.Sequence,
		factType, supplyFact.id, []byte(raw)).Scan(&id); err != nil {
		return 0, fmt.Errorf("insert V17 freight-receipt supply fact %s: %w", shipmentCode, err)
	}
	return id, nil
}

func transitionCityOpenWorldEnterpriseFreightReceiptShipment(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	shipment *cityOpenWorldEnterpriseFreightReceiptShipmentRecord,
	nextState, reasonCode string,
	factID int64,
	reference CityOpenWorldEnterpriseFreightReceiptFactRef,
) error {
	if shipment == nil || factID <= 0 || !cityOpenWorldEnterpriseFreightReceiptTransitionAllowed(shipmentStateBeforeFirstTransition(shipment), nextState) ||
		!cityOpenWorldEnterpriseFreightReceiptTransitionReasonMatchesState(nextState, reasonCode) ||
		reference.Tick <= 0 || reference.Sequence <= 0 || !cityOpenWorldEnterpriseFreightReceiptEvidenceKindValid(reference.EvidenceKind) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v17_freight_receipt_transition"})
	}
	metadata, err := json.Marshal(map[string]any{
		"schema_version": cityOpenWorldEnterpriseFreightReceiptSchemaVersion,
		"previous_state": shipmentStateBeforeFirstTransition(shipment),
		"source_fact": map[string]any{
			"evidence_kind": reference.EvidenceKind,
			"tick":          reference.Tick,
			"sequence":      reference.Sequence,
		},
	})
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_enterprise_freight_shipment_transitions
    (world_id, shipment_code, transition_tick, transition_sequence, state,
     reason_code, source_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb)`, worldID, shipment.code,
		reference.Tick, reference.Sequence, nextState, reasonCode, factID, []byte(metadata)); err != nil {
		return fmt.Errorf("insert V17 freight-receipt shipment transition: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
UPDATE city_open_world_enterprise_freight_shipments
SET state = $3, last_receipt_fact_id = $4, version = version + 1, updated_at = NOW()
WHERE world_id = $1 AND code = $2`, worldID, shipment.code, nextState, factID); err != nil {
		return fmt.Errorf("update V17 freight-receipt shipment state: %w", err)
	}
	return nil
}

func cityOpenWorldEnterpriseFreightReceiptStateTransitionDelta(previous, next string, facts, transitions, receipts int64) cityOpenWorldEnterpriseFreightReceiptPolicyDelta {
	delta := cityOpenWorldEnterpriseFreightReceiptPolicyDelta{facts: facts, transitions: transitions, receipts: receipts}
	apply := func(state string, change int64) {
		switch state {
		case cityOpenWorldEnterpriseFreightReceiptStateAwaitingRoute:
			delta.awaitingRoute += change
		case cityOpenWorldEnterpriseFreightReceiptStateInTransit:
			delta.inTransit += change
		case cityOpenWorldEnterpriseFreightReceiptStateAwaitingReceipt:
			delta.awaitingReceipt += change
		case cityOpenWorldEnterpriseFreightReceiptStateReceived:
			delta.received += change
		case cityOpenWorldEnterpriseFreightReceiptStateSettled:
			delta.settled += change
		case cityOpenWorldEnterpriseFreightReceiptStateExpired:
			delta.expired += change
		case cityOpenWorldEnterpriseFreightReceiptStateVoided:
			delta.voided += change
		case cityOpenWorldEnterpriseFreightReceiptStateOrphaned:
			delta.orphaned += change
		}
	}
	apply(previous, -1)
	apply(next, 1)
	return delta
}

// assertCityOpenWorldEnterpriseFreightReceiptReady is called from V15's
// existing delivery command. Pre-V17 sources are intentionally left on their
// sealed legacy path; newly created V16 sources need a completed shipment.
func assertCityOpenWorldEnterpriseFreightReceiptReady(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	orderCode string,
) (*cityOpenWorldEnterpriseFreightReceiptShipmentRecord, error) {
	var simulationVersion string
	if err := tx.QueryRowContext(ctx, `
SELECT simulation_version
FROM city_worlds
WHERE id = $1`, worldID).Scan(&simulationVersion); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v17_freight_receipt_world"})
		}
		return nil, fmt.Errorf("load V17 freight-receipt world version: %w", err)
	}
	if !cityEngineSupportsOpenWorldEnterpriseFreightReceipts(simulationVersion) {
		return nil, nil
	}
	policy, err := loadCityOpenWorldEnterpriseFreightReceiptPolicyForUpdate(ctx, tx, worldID)
	if err != nil {
		return nil, err
	}
	var sourceCode string
	var sourceTick int64
	err = tx.QueryRowContext(ctx, `
SELECT code, source_tick
FROM city_open_world_enterprise_freight_sources
WHERE world_id = $1 AND order_code = $2 AND state <> 'suppressed'
ORDER BY source_tick DESC
LIMIT 1
FOR UPDATE`, worldID, orderCode).Scan(&sourceCode, &sourceTick)
	if errors.Is(err, sql.ErrNoRows) || sourceTick <= policy.BaselineTick {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load V17 delivery freight source: %w", err)
	}
	shipment, err := loadCityOpenWorldEnterpriseFreightReceiptShipmentForUpdate(ctx, tx, worldID,
		cityOpenWorldEnterpriseFreightShipmentCode(sourceCode))
	if err != nil {
		return nil, err
	}
	if shipment.freightSourceCode != sourceCode || shipment.orderCode != orderCode {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v17_freight_receipt_identity"})
	}
	if shipment.state != cityOpenWorldEnterpriseFreightReceiptStateAwaitingReceipt {
		return nil, cityOpenWorldSupplyChainReject(cityOpenWorldSupplyChainRejectionReceiptNotReady)
	}
	return shipment, nil
}

func recordCityOpenWorldEnterpriseFreightReceipt(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	shipment *cityOpenWorldEnterpriseFreightReceiptShipmentRecord,
	deliveryID int64,
	supplyFact *cityOpenWorldSupplyChainFactRecord,
	operation *CityResourceOperation,
) error {
	if shipment == nil {
		return nil
	}
	if deliveryID <= 0 || supplyFact == nil || supplyFact.id <= 0 || operation == nil || operation.ID <= 0 ||
		shipment.state != cityOpenWorldEnterpriseFreightReceiptStateAwaitingReceipt {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v17_freight_receipt_record"})
	}
	if err := activateCityOpenWorldEnterpriseFreightReceiptWrite(ctx, tx, worldID); err != nil {
		return err
	}
	factID, err := insertCityOpenWorldEnterpriseFreightReceiptSupplyFact(ctx, tx, worldID,
		shipment.code, supplyFact, "receipt.confirmed", map[string]any{
			"schema_version":              cityOpenWorldEnterpriseFreightReceiptSchemaVersion,
			"order_code":                  shipment.orderCode,
			"resource_operation_tick":     operation.Tick,
			"resource_operation_sequence": operation.Sequence,
		})
	if err != nil {
		return err
	}
	reference := CityOpenWorldEnterpriseFreightReceiptFactRef{
		EvidenceKind: cityOpenWorldEnterpriseFreightReceiptEvidenceSupplyChain,
		Tick:         supplyFact.fact.Tick, Sequence: supplyFact.fact.Sequence,
	}
	if err = transitionCityOpenWorldEnterpriseFreightReceiptShipment(ctx, tx, worldID, shipment,
		cityOpenWorldEnterpriseFreightReceiptStateReceived,
		cityOpenWorldEnterpriseFreightReceiptReasonReceived, factID, reference); err != nil {
		return err
	}
	metadata, err := json.Marshal(map[string]any{
		"schema_version":   cityOpenWorldEnterpriseFreightReceiptSchemaVersion,
		"receipt_contract": cityOpenWorldEnterpriseFreightReceiptReceiptContract,
		"delivery_fact":    map[string]any{"tick": supplyFact.fact.Tick, "sequence": supplyFact.fact.Sequence},
	})
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_enterprise_freight_receipts
    (world_id, shipment_code, order_code, received_tick,
     supply_chain_delivery_id, resource_operation_id, source_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb)`, worldID, shipment.code,
		shipment.orderCode, supplyFact.fact.Tick, deliveryID, operation.ID, factID, []byte(metadata)); err != nil {
		return fmt.Errorf("insert V17 freight-receipt: %w", err)
	}
	if err = updateCityOpenWorldEnterpriseFreightReceiptPolicy(ctx, tx, worldID,
		cityOpenWorldEnterpriseFreightReceiptStateTransitionDelta(
			cityOpenWorldEnterpriseFreightReceiptStateAwaitingReceipt,
			cityOpenWorldEnterpriseFreightReceiptStateReceived, 1, 1, 1,
		)); err != nil {
		return err
	}
	shipment.state = cityOpenWorldEnterpriseFreightReceiptStateReceived
	shipment.version++
	shipment.lastReceiptFactID = sql.NullInt64{Int64: factID, Valid: true}
	return assertCityOpenWorldEnterpriseFreightReceiptFoundation(ctx, tx, worldID)
}
