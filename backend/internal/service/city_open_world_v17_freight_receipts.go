package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
)

const (
	cityOpenWorldEnterpriseFreightReceiptSchemaVersion           = 1
	cityOpenWorldEnterpriseFreightReceiptProfileID               = "sub2api-open-world-enterprise-freight-receipts"
	cityOpenWorldEnterpriseFreightReceiptProfileVersion          = "1.0.0"
	cityOpenWorldEnterpriseFreightReceiptShipmentContract        = "v16_source_custody_snapshot_v1"
	cityOpenWorldEnterpriseFreightReceiptReceiptContract         = "v15_atomic_delivery_receipt_gate_v1"
	cityOpenWorldEnterpriseFreightReceiptLegacyContract          = "pre_v17_source_legacy_delivery_v1"
	cityOpenWorldEnterpriseFreightReceiptMaximumShipments        = 10000
	cityOpenWorldEnterpriseFreightReceiptMaximumObservationsTick = 128

	cityOpenWorldEnterpriseFreightReceiptStateAwaitingRoute   = "awaiting_route"
	cityOpenWorldEnterpriseFreightReceiptStateInTransit       = "in_transit"
	cityOpenWorldEnterpriseFreightReceiptStateAwaitingReceipt = "awaiting_receipt"
	cityOpenWorldEnterpriseFreightReceiptStateReceived        = "received"
	// Settled is V22's partial-outcome successor to the legacy atomic V15
	// delivery receipt. It has settlement evidence, not a V17 receipt row.
	cityOpenWorldEnterpriseFreightReceiptStateSettled         = "settled"
	cityOpenWorldEnterpriseFreightReceiptStateExpired         = "expired"
	cityOpenWorldEnterpriseFreightReceiptStateVoided          = "voided"
	cityOpenWorldEnterpriseFreightReceiptStateOrphaned        = "orphaned"

	cityOpenWorldEnterpriseFreightReceiptEvidenceFreight     = "enterprise_freight"
	cityOpenWorldEnterpriseFreightReceiptEvidenceSupplyChain = "supply_chain"

	cityOpenWorldEnterpriseFreightReceiptReasonDemandPending = "v16_demand_pending"
	cityOpenWorldEnterpriseFreightReceiptReasonScheduled     = "v16_route_scheduled"
	cityOpenWorldEnterpriseFreightReceiptReasonCompleted     = "v16_route_completed"
	cityOpenWorldEnterpriseFreightReceiptReasonExpired       = "v16_demand_expired"
	cityOpenWorldEnterpriseFreightReceiptReasonVoided        = "v16_demand_voided"
	cityOpenWorldEnterpriseFreightReceiptReasonOrphaned      = "v16_transport_orphaned"
	cityOpenWorldEnterpriseFreightReceiptReasonReceived      = "v15_delivery_received"
	cityOpenWorldEnterpriseFreightReceiptReasonSettled       = "v22_freight_settlement_completed"
)

// CityOpenWorldEnterpriseFreightReceiptPolicy locks the V17 observation and
// receipt-gate contract. It deliberately models physical custody evidence,
// not a second mutable inventory balance: V15 remains the sole owner of
// inventory reservation/release and the atomic resource transfer.
type CityOpenWorldEnterpriseFreightReceiptPolicy struct {
	ProfileID                  string          `json:"profile_id"`
	ProfileVersion             string          `json:"profile_version"`
	ContentHash                string          `json:"content_hash"`
	BaselineTick               int64           `json:"baseline_tick"`
	ShipmentContract           string          `json:"shipment_contract"`
	ReceiptContract            string          `json:"receipt_contract"`
	LegacyContract             string          `json:"legacy_contract"`
	MaximumShipments           int             `json:"maximum_shipments"`
	MaximumObservationsPerTick int             `json:"maximum_observations_per_tick"`
	ShipmentCount              int64           `json:"shipment_count"`
	AwaitingRouteCount         int64           `json:"awaiting_route_count"`
	InTransitCount             int64           `json:"in_transit_count"`
	AwaitingReceiptCount       int64           `json:"awaiting_receipt_count"`
	ReceivedCount              int64           `json:"received_count"`
	SettledCount               int64           `json:"settled_count"`
	ExpiredCount               int64           `json:"expired_count"`
	VoidedCount                int64           `json:"voided_count"`
	OrphanedCount              int64           `json:"orphaned_count"`
	FactCount                  int64           `json:"fact_count"`
	TransitionCount            int64           `json:"transition_count"`
	ReceiptCount               int64           `json:"receipt_count"`
	Revision                   int64           `json:"revision"`
	Metadata                   json.RawMessage `json:"metadata"`
}

// CityOpenWorldEnterpriseFreightReceiptFactRef identifies a V17 fact by the
// source evidence namespace and cursor. V15 command facts and V16 runtime
// facts have independent sequence spaces, so EvidenceKind is part of the
// canonical identity.
type CityOpenWorldEnterpriseFreightReceiptFactRef struct {
	EvidenceKind string `json:"evidence_kind"`
	Tick         int64  `json:"tick"`
	Sequence     int64  `json:"sequence"`
}

// CityOpenWorldEnterpriseFreightShipment is a custody projection over one
// V16 source. SourceEvidence must always be the immutable V16 source.created
// fact; LastFact points to the last V17 fact and can therefore be V16-derived
// or the V15 delivery confirmation evidence.
type CityOpenWorldEnterpriseFreightShipment struct {
	Code               string                                       `json:"code"`
	FreightSourceCode  string                                       `json:"freight_source_code"`
	OrderCode          string                                       `json:"order_code"`
	SellerNodeCode     string                                       `json:"seller_node_code"`
	BuyerNodeCode      string                                       `json:"buyer_node_code"`
	SourceHubCode      string                                       `json:"source_hub_code"`
	DestinationHubCode string                                       `json:"destination_hub_code"`
	SourceTick         int64                                        `json:"source_tick"`
	RequestedUnits     int64                                        `json:"requested_units"`
	State              string                                       `json:"state"`
	SourceEvidence     CityOpenWorldEnterpriseFreightReceiptFactRef `json:"source_evidence"`
	LastFact           CityOpenWorldEnterpriseFreightReceiptFactRef `json:"last_fact"`
	Version            int64                                        `json:"version"`
	Metadata           json.RawMessage                              `json:"metadata"`
}

// CityOpenWorldEnterpriseFreightShipmentLine copies immutable V16 line
// evidence. It is intentionally not a balance and cannot be used to infer
// available inventory independently from V15 reservations.
type CityOpenWorldEnterpriseFreightShipmentLine struct {
	ShipmentCode            string          `json:"shipment_code"`
	LineNo                  int             `json:"line_no"`
	ResourceCode            string          `json:"resource_code"`
	SourceFirmCode          string          `json:"source_firm_code"`
	SourceDistrictCode      string          `json:"source_district_code"`
	DestinationFirmCode     string          `json:"destination_firm_code"`
	DestinationDistrictCode string          `json:"destination_district_code"`
	QuantityUnits           int64           `json:"quantity_units"`
	UnitPriceUnits          int64           `json:"unit_price_units"`
	TotalPriceUnits         int64           `json:"total_price_units"`
	Metadata                json.RawMessage `json:"metadata"`
}

// CityOpenWorldEnterpriseFreightReceiptFact stores the observed predecessor
// fact instead of creating a synthetic transport/receipt event. Freight facts
// point at V16; receipt.confirmed points at the V15 order.delivered fact.
type CityOpenWorldEnterpriseFreightReceiptFact struct {
	ShipmentCode      string          `json:"shipment_code"`
	Tick              int64           `json:"tick"`
	Sequence          int64           `json:"sequence"`
	FactType          string          `json:"fact_type"`
	EvidenceKind      string          `json:"evidence_kind"`
	FreightSourceCode *string         `json:"freight_source_code,omitempty"`
	SupplyOrderCode   *string         `json:"supply_order_code,omitempty"`
	Payload           json.RawMessage `json:"payload"`
}

type CityOpenWorldEnterpriseFreightShipmentTransition struct {
	ShipmentCode       string                                       `json:"shipment_code"`
	TransitionTick     int64                                        `json:"transition_tick"`
	TransitionSequence int64                                        `json:"transition_sequence"`
	State              string                                       `json:"state"`
	ReasonCode         string                                       `json:"reason_code"`
	SourceFact         CityOpenWorldEnterpriseFreightReceiptFactRef `json:"source_fact"`
	Metadata           json.RawMessage                              `json:"metadata"`
}

// CityOpenWorldEnterpriseFreightReceipt is proof that the existing V15
// delivery transfer happened after a V16-completed shipment was explicitly
// ready for receipt. It never duplicates the resource operation itself.
type CityOpenWorldEnterpriseFreightReceipt struct {
	ShipmentCode      string                                       `json:"shipment_code"`
	OrderCode         string                                       `json:"order_code"`
	ReceivedTick      int64                                        `json:"received_tick"`
	DeliveryFact      CityOpenWorldRuntimeFactRef                  `json:"delivery_fact"`
	ResourceOperation CityResourceOperationCursor                  `json:"resource_operation"`
	SourceFact        CityOpenWorldEnterpriseFreightReceiptFactRef `json:"source_fact"`
	Metadata          json.RawMessage                              `json:"metadata"`
}

type CityOpenWorldEnterpriseFreightReceiptState struct {
	Policy      CityOpenWorldEnterpriseFreightReceiptPolicy        `json:"policy"`
	Shipments   []CityOpenWorldEnterpriseFreightShipment           `json:"shipments"`
	Lines       []CityOpenWorldEnterpriseFreightShipmentLine       `json:"lines"`
	Facts       []CityOpenWorldEnterpriseFreightReceiptFact        `json:"facts"`
	Transitions []CityOpenWorldEnterpriseFreightShipmentTransition `json:"transitions"`
	Receipts    []CityOpenWorldEnterpriseFreightReceipt            `json:"receipts"`
}

func cityOpenWorldEnterpriseFreightReceiptPolicyHash() (string, error) {
	raw, err := json.Marshal(struct {
		SchemaVersion              int    `json:"schema_version"`
		ProfileID                  string `json:"profile_id"`
		ProfileVersion             string `json:"profile_version"`
		ShipmentContract           string `json:"shipment_contract"`
		ReceiptContract            string `json:"receipt_contract"`
		LegacyContract             string `json:"legacy_contract"`
		MaximumShipments           int    `json:"maximum_shipments"`
		MaximumObservationsPerTick int    `json:"maximum_observations_per_tick"`
	}{
		SchemaVersion:              cityOpenWorldEnterpriseFreightReceiptSchemaVersion,
		ProfileID:                  cityOpenWorldEnterpriseFreightReceiptProfileID,
		ProfileVersion:             cityOpenWorldEnterpriseFreightReceiptProfileVersion,
		ShipmentContract:           cityOpenWorldEnterpriseFreightReceiptShipmentContract,
		ReceiptContract:            cityOpenWorldEnterpriseFreightReceiptReceiptContract,
		LegacyContract:             cityOpenWorldEnterpriseFreightReceiptLegacyContract,
		MaximumShipments:           cityOpenWorldEnterpriseFreightReceiptMaximumShipments,
		MaximumObservationsPerTick: cityOpenWorldEnterpriseFreightReceiptMaximumObservationsTick,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func cityOpenWorldEnterpriseFreightShipmentCode(sourceCode string) string {
	sum := sha256.Sum256([]byte("enterprise.freight.shipment.v1\x00" + sourceCode))
	return "enterprise.freight.shipment." + hex.EncodeToString(sum[:20])
}

func activateCityOpenWorldEnterpriseFreightReceiptBootstrapWrite(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if _, err := tx.ExecContext(ctx, `
SELECT set_config('sub2api.city_open_world_enterprise_freight_receipt_bootstrap_world_id', $1, TRUE)`, fmt.Sprintf("%d", worldID)); err != nil {
		return fmt.Errorf("enable open-world V17 freight-receipt bootstrap: %w", err)
	}
	return nil
}

func activateCityOpenWorldEnterpriseFreightReceiptWrite(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if _, err := tx.ExecContext(ctx, `
SELECT set_config('sub2api.city_open_world_enterprise_freight_receipt_world_id', $1, TRUE)`, fmt.Sprintf("%d", worldID)); err != nil {
		return fmt.Errorf("enable open-world V17 freight-receipt write: %w", err)
	}
	return nil
}

func assertCityOpenWorldEnterpriseFreightReceiptFoundation(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if _, err := tx.ExecContext(ctx, `SELECT assert_city_open_world_enterprise_freight_receipt_foundation($1)`, worldID); err != nil {
		return fmt.Errorf("validate V17 freight-receipt foundation: %w", err)
	}
	return nil
}

// initializeCityOpenWorldV17EnterpriseFreightReceiptFoundation creates only
// the sealed profile. It deliberately does not backfill pre-upgrade V16
// sources, because doing so would fabricate V17 custody/receipt history.
func initializeCityOpenWorldV17EnterpriseFreightReceiptFoundation(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if worldID <= 0 {
		return ErrCityInvalidInput
	}
	var version string
	var baselineTick int64
	if err := tx.QueryRowContext(ctx, `
SELECT simulation_version, current_tick
FROM city_worlds WHERE id = $1 FOR UPDATE`, worldID).Scan(&version, &baselineTick); err != nil {
		return fmt.Errorf("load V17 freight-receipt world: %w", err)
	}
	if !cityEngineSupportsOpenWorldEnterpriseFreightReceipts(version) {
		return ErrCitySimulationVersion.WithMetadata(map[string]string{"version": version})
	}
	if err := assertCityOpenWorldEnterpriseFreightFoundation(ctx, tx, worldID); err != nil {
		return fmt.Errorf("validate V17 freight-receipt freight prerequisite: %w", err)
	}
	if err := activateCityOpenWorldEnterpriseFreightReceiptBootstrapWrite(ctx, tx, worldID); err != nil {
		return err
	}
	hash, err := cityOpenWorldEnterpriseFreightReceiptPolicyHash()
	if err != nil {
		return fmt.Errorf("hash V17 freight-receipt policy: %w", err)
	}
	metadata, err := json.Marshal(map[string]any{
		"schema_version": cityOpenWorldEnterpriseFreightReceiptSchemaVersion,
		"scope":          "v16_transport_custody_and_v15_receipt_gate",
		"inventory":      "v15_only_until_delivery",
		"legacy":         "pre_v17_sources_untracked",
	})
	if err != nil {
		return fmt.Errorf("marshal V17 freight-receipt profile metadata: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_enterprise_freight_receipt_profiles
    (world_id, profile_id, profile_version, content_hash, baseline_tick,
     shipment_contract, receipt_contract, legacy_contract,
     maximum_shipments, maximum_observations_per_tick,
     shipment_count, awaiting_route_count, in_transit_count, awaiting_receipt_count,
     received_count, expired_count, voided_count, orphaned_count, fact_count,
     transition_count, receipt_count, revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
        0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, $11::jsonb)`,
		worldID, cityOpenWorldEnterpriseFreightReceiptProfileID,
		cityOpenWorldEnterpriseFreightReceiptProfileVersion, hash, baselineTick,
		cityOpenWorldEnterpriseFreightReceiptShipmentContract,
		cityOpenWorldEnterpriseFreightReceiptReceiptContract,
		cityOpenWorldEnterpriseFreightReceiptLegacyContract,
		cityOpenWorldEnterpriseFreightReceiptMaximumShipments,
		cityOpenWorldEnterpriseFreightReceiptMaximumObservationsTick,
		[]byte(metadata)); err != nil {
		return fmt.Errorf("insert V17 freight-receipt profile: %w", err)
	}
	return assertCityOpenWorldEnterpriseFreightReceiptFoundation(ctx, tx, worldID)
}

func loadCityOpenWorldEnterpriseFreightReceiptState(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*CityOpenWorldEnterpriseFreightReceiptState, error) {
	if worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	state := &CityOpenWorldEnterpriseFreightReceiptState{
		Shipments:   make([]CityOpenWorldEnterpriseFreightShipment, 0),
		Lines:       make([]CityOpenWorldEnterpriseFreightShipmentLine, 0),
		Facts:       make([]CityOpenWorldEnterpriseFreightReceiptFact, 0),
		Transitions: make([]CityOpenWorldEnterpriseFreightShipmentTransition, 0),
		Receipts:    make([]CityOpenWorldEnterpriseFreightReceipt, 0),
	}
	if err := queryer.QueryRowContext(ctx, `
SELECT profile_id, profile_version, content_hash, baseline_tick,
       shipment_contract, receipt_contract, legacy_contract,
       maximum_shipments, maximum_observations_per_tick,
       shipment_count, awaiting_route_count, in_transit_count, awaiting_receipt_count,
       received_count, settled_count, expired_count, voided_count, orphaned_count, fact_count,
       transition_count, receipt_count, revision, metadata
FROM city_open_world_enterprise_freight_receipt_profiles
WHERE world_id = $1`, worldID).Scan(
		&state.Policy.ProfileID, &state.Policy.ProfileVersion, &state.Policy.ContentHash,
		&state.Policy.BaselineTick, &state.Policy.ShipmentContract, &state.Policy.ReceiptContract,
		&state.Policy.LegacyContract, &state.Policy.MaximumShipments,
		&state.Policy.MaximumObservationsPerTick, &state.Policy.ShipmentCount,
		&state.Policy.AwaitingRouteCount, &state.Policy.InTransitCount,
		&state.Policy.AwaitingReceiptCount, &state.Policy.ReceivedCount, &state.Policy.SettledCount,
		&state.Policy.ExpiredCount, &state.Policy.VoidedCount, &state.Policy.OrphanedCount,
		&state.Policy.FactCount, &state.Policy.TransitionCount, &state.Policy.ReceiptCount,
		&state.Policy.Revision, &state.Policy.Metadata,
	); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v17_freight_receipt_profile"})
	} else if err != nil {
		return nil, fmt.Errorf("load V17 freight-receipt profile: %w", err)
	}

	shipmentRows, err := queryer.QueryContext(ctx, `
SELECT shipment.code, shipment.freight_source_code, shipment.order_code,
       shipment.seller_node_code, shipment.buyer_node_code, shipment.source_hub_code,
       shipment.destination_hub_code, shipment.source_tick, shipment.requested_units,
       shipment.state, source_fact.tick, source_fact.sequence,
       last_fact.evidence_kind, last_fact.tick, last_fact.sequence,
       shipment.version, shipment.metadata
FROM city_open_world_enterprise_freight_shipments shipment
JOIN city_open_world_enterprise_freight_facts source_fact
  ON source_fact.id = shipment.source_freight_fact_id
 AND source_fact.world_id = shipment.world_id
JOIN city_open_world_enterprise_freight_receipt_facts last_fact
  ON last_fact.id = shipment.last_receipt_fact_id
 AND last_fact.world_id = shipment.world_id
WHERE shipment.world_id = $1
ORDER BY shipment.code`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V17 freight-receipt shipments: %w", err)
	}
	for shipmentRows.Next() {
		item := CityOpenWorldEnterpriseFreightShipment{}
		if err = shipmentRows.Scan(
			&item.Code, &item.FreightSourceCode, &item.OrderCode,
			&item.SellerNodeCode, &item.BuyerNodeCode, &item.SourceHubCode,
			&item.DestinationHubCode, &item.SourceTick, &item.RequestedUnits,
			&item.State, &item.SourceEvidence.Tick, &item.SourceEvidence.Sequence,
			&item.LastFact.EvidenceKind, &item.LastFact.Tick, &item.LastFact.Sequence,
			&item.Version, &item.Metadata,
		); err != nil {
			_ = shipmentRows.Close()
			return nil, fmt.Errorf("scan V17 freight-receipt shipment: %w", err)
		}
		item.SourceEvidence.EvidenceKind = cityOpenWorldEnterpriseFreightReceiptEvidenceFreight
		state.Shipments = append(state.Shipments, item)
	}
	if err = closeCityRows(shipmentRows, "iterate V17 freight-receipt shipments"); err != nil {
		return nil, err
	}

	lineRows, err := queryer.QueryContext(ctx, `
SELECT shipment_code, line_no, resource_code, source_firm_code, source_district_code,
       destination_firm_code, destination_district_code, quantity_units,
       unit_price_units, total_price_units, metadata
FROM city_open_world_enterprise_freight_shipment_lines
WHERE world_id = $1
ORDER BY shipment_code, line_no`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V17 freight-receipt lines: %w", err)
	}
	for lineRows.Next() {
		item := CityOpenWorldEnterpriseFreightShipmentLine{}
		if err = lineRows.Scan(&item.ShipmentCode, &item.LineNo, &item.ResourceCode,
			&item.SourceFirmCode, &item.SourceDistrictCode, &item.DestinationFirmCode,
			&item.DestinationDistrictCode, &item.QuantityUnits, &item.UnitPriceUnits,
			&item.TotalPriceUnits, &item.Metadata); err != nil {
			_ = lineRows.Close()
			return nil, fmt.Errorf("scan V17 freight-receipt line: %w", err)
		}
		state.Lines = append(state.Lines, item)
	}
	if err = closeCityRows(lineRows, "iterate V17 freight-receipt lines"); err != nil {
		return nil, err
	}

	factRows, err := queryer.QueryContext(ctx, `
SELECT fact.shipment_code, fact.tick, fact.sequence, fact.fact_type, fact.evidence_kind,
       freight.source_code, supply.order_code, fact.payload
FROM city_open_world_enterprise_freight_receipt_facts fact
LEFT JOIN city_open_world_enterprise_freight_facts freight
  ON freight.id = fact.freight_fact_id AND freight.world_id = fact.world_id
LEFT JOIN city_open_world_supply_chain_facts supply
  ON supply.id = fact.supply_chain_fact_id AND supply.world_id = fact.world_id
WHERE fact.world_id = $1
ORDER BY fact.shipment_code, fact.evidence_kind, fact.tick, fact.sequence`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V17 freight-receipt facts: %w", err)
	}
	for factRows.Next() {
		item := CityOpenWorldEnterpriseFreightReceiptFact{}
		var freightSourceCode, supplyOrderCode sql.NullString
		if err = factRows.Scan(&item.ShipmentCode, &item.Tick, &item.Sequence,
			&item.FactType, &item.EvidenceKind, &freightSourceCode, &supplyOrderCode,
			&item.Payload); err != nil {
			_ = factRows.Close()
			return nil, fmt.Errorf("scan V17 freight-receipt fact: %w", err)
		}
		item.FreightSourceCode = nullStringPointer(freightSourceCode)
		item.SupplyOrderCode = nullStringPointer(supplyOrderCode)
		state.Facts = append(state.Facts, item)
	}
	if err = closeCityRows(factRows, "iterate V17 freight-receipt facts"); err != nil {
		return nil, err
	}

	transitionRows, err := queryer.QueryContext(ctx, `
SELECT transition.shipment_code, transition.transition_tick, transition.transition_sequence,
       transition.state, transition.reason_code, fact.evidence_kind,
       fact.tick, fact.sequence, transition.metadata
FROM city_open_world_enterprise_freight_shipment_transitions transition
JOIN city_open_world_enterprise_freight_receipt_facts fact
  ON fact.id = transition.source_fact_id AND fact.world_id = transition.world_id
WHERE transition.world_id = $1
ORDER BY transition.shipment_code, transition.transition_tick, transition.transition_sequence, fact.evidence_kind`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V17 freight-receipt transitions: %w", err)
	}
	for transitionRows.Next() {
		item := CityOpenWorldEnterpriseFreightShipmentTransition{}
		if err = transitionRows.Scan(&item.ShipmentCode, &item.TransitionTick,
			&item.TransitionSequence, &item.State, &item.ReasonCode,
			&item.SourceFact.EvidenceKind, &item.SourceFact.Tick, &item.SourceFact.Sequence,
			&item.Metadata); err != nil {
			_ = transitionRows.Close()
			return nil, fmt.Errorf("scan V17 freight-receipt transition: %w", err)
		}
		state.Transitions = append(state.Transitions, item)
	}
	if err = closeCityRows(transitionRows, "iterate V17 freight-receipt transitions"); err != nil {
		return nil, err
	}

	receiptRows, err := queryer.QueryContext(ctx, `
SELECT receipt.shipment_code, receipt.order_code, receipt.received_tick,
       supply_fact.tick, supply_fact.sequence,
       operation.tick, operation.sequence,
       receipt_fact.evidence_kind, receipt_fact.tick, receipt_fact.sequence,
       receipt.metadata
FROM city_open_world_enterprise_freight_receipts receipt
JOIN city_open_world_supply_chain_deliveries delivery
  ON delivery.id = receipt.supply_chain_delivery_id AND delivery.world_id = receipt.world_id
JOIN city_open_world_supply_chain_facts supply_fact
  ON supply_fact.id = delivery.source_fact_id AND supply_fact.world_id = receipt.world_id
JOIN city_resource_operations operation
  ON operation.id = receipt.resource_operation_id AND operation.world_id = receipt.world_id
JOIN city_open_world_enterprise_freight_receipt_facts receipt_fact
  ON receipt_fact.id = receipt.source_fact_id AND receipt_fact.world_id = receipt.world_id
WHERE receipt.world_id = $1
ORDER BY receipt.shipment_code`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V17 freight-receipts: %w", err)
	}
	for receiptRows.Next() {
		item := CityOpenWorldEnterpriseFreightReceipt{}
		if err = receiptRows.Scan(&item.ShipmentCode, &item.OrderCode, &item.ReceivedTick,
			&item.DeliveryFact.Tick, &item.DeliveryFact.Sequence,
			&item.ResourceOperation.Tick, &item.ResourceOperation.Sequence,
			&item.SourceFact.EvidenceKind, &item.SourceFact.Tick, &item.SourceFact.Sequence,
			&item.Metadata); err != nil {
			_ = receiptRows.Close()
			return nil, fmt.Errorf("scan V17 freight-receipt: %w", err)
		}
		state.Receipts = append(state.Receipts, item)
	}
	if err = closeCityRows(receiptRows, "iterate V17 freight-receipts"); err != nil {
		return nil, err
	}
	sortCityOpenWorldEnterpriseFreightReceiptState(state)
	if err = validateCityOpenWorldEnterpriseFreightReceiptState(state); err != nil {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v17_freight_receipt_state"}).WithCause(err)
	}
	return state, nil
}

func validateCityOpenWorldEnterpriseFreightReceiptState(state *CityOpenWorldEnterpriseFreightReceiptState) error {
	if state == nil {
		return errors.New("enterprise-freight receipt state is required")
	}
	hash, err := cityOpenWorldEnterpriseFreightReceiptPolicyHash()
	if err != nil {
		return err
	}
	p := state.Policy
	if p.ProfileID != cityOpenWorldEnterpriseFreightReceiptProfileID ||
		p.ProfileVersion != cityOpenWorldEnterpriseFreightReceiptProfileVersion || p.ContentHash != hash ||
		p.BaselineTick < 0 || p.ShipmentContract != cityOpenWorldEnterpriseFreightReceiptShipmentContract ||
		p.ReceiptContract != cityOpenWorldEnterpriseFreightReceiptReceiptContract ||
		p.LegacyContract != cityOpenWorldEnterpriseFreightReceiptLegacyContract ||
		p.MaximumShipments != cityOpenWorldEnterpriseFreightReceiptMaximumShipments ||
		p.MaximumObservationsPerTick != cityOpenWorldEnterpriseFreightReceiptMaximumObservationsTick ||
		p.Revision < 1 || !cityOpenWorldEnterpriseFreightReceiptPolicyMetadataValid(p.Metadata) {
		return errors.New("invalid enterprise-freight receipt policy")
	}
	if p.ShipmentCount != int64(len(state.Shipments)) || p.FactCount != int64(len(state.Facts)) ||
		p.TransitionCount != int64(len(state.Transitions)) || p.ReceiptCount != int64(len(state.Receipts)) {
		return errors.New("enterprise-freight receipt policy counters are inconsistent")
	}

	shipments := make(map[string]CityOpenWorldEnterpriseFreightShipment, len(state.Shipments))
	freightSources := make(map[string]struct{}, len(state.Shipments))
	orders := make(map[string]struct{}, len(state.Shipments))
	awaitingRoute, inTransit, awaitingReceipt := int64(0), int64(0), int64(0)
	received, settled, expired, voided, orphaned := int64(0), int64(0), int64(0), int64(0), int64(0)
	for _, shipment := range state.Shipments {
		if !cityOpenWorldSupplyChainCodeValid(shipment.Code) ||
			!cityOpenWorldSupplyChainCodeValid(shipment.FreightSourceCode) ||
			!cityOpenWorldSupplyChainCodeValid(shipment.OrderCode) ||
			!cityOpenWorldSupplyChainCodeValid(shipment.SellerNodeCode) ||
			!cityOpenWorldSupplyChainCodeValid(shipment.BuyerNodeCode) ||
			!cityOpenWorldSupplyChainCodeValid(shipment.SourceHubCode) ||
			!cityOpenWorldSupplyChainCodeValid(shipment.DestinationHubCode) ||
			shipment.SellerNodeCode == shipment.BuyerNodeCode || shipment.SourceHubCode == shipment.DestinationHubCode ||
			shipment.SourceTick <= p.BaselineTick || shipment.RequestedUnits <= 0 || shipment.Version < 1 ||
			shipment.SourceEvidence.EvidenceKind != cityOpenWorldEnterpriseFreightReceiptEvidenceFreight ||
			shipment.SourceEvidence.Tick <= 0 || shipment.SourceEvidence.Sequence <= 0 ||
			shipment.LastFact.Tick <= 0 || shipment.LastFact.Sequence <= 0 ||
			!cityOpenWorldEnterpriseFreightReceiptEvidenceKindValid(shipment.LastFact.EvidenceKind) ||
			!cityOpenWorldEnterpriseFreightReceiptStateValid(shipment.State) ||
			!cityOpenWorldEnterpriseFreightReceiptJSONObject(shipment.Metadata) ||
			shipment.Code != cityOpenWorldEnterpriseFreightShipmentCode(shipment.FreightSourceCode) {
			return errors.New("invalid enterprise-freight shipment")
		}
		if _, exists := shipments[shipment.Code]; exists {
			return errors.New("duplicate enterprise-freight shipment")
		}
		if _, exists := freightSources[shipment.FreightSourceCode]; exists {
			return errors.New("duplicate enterprise-freight shipment source")
		}
		if _, exists := orders[shipment.OrderCode]; exists {
			return errors.New("duplicate enterprise-freight shipment order")
		}
		shipments[shipment.Code] = shipment
		freightSources[shipment.FreightSourceCode] = struct{}{}
		orders[shipment.OrderCode] = struct{}{}
		switch shipment.State {
		case cityOpenWorldEnterpriseFreightReceiptStateAwaitingRoute:
			awaitingRoute++
		case cityOpenWorldEnterpriseFreightReceiptStateInTransit:
			inTransit++
		case cityOpenWorldEnterpriseFreightReceiptStateAwaitingReceipt:
			awaitingReceipt++
		case cityOpenWorldEnterpriseFreightReceiptStateReceived:
			received++
		case cityOpenWorldEnterpriseFreightReceiptStateSettled:
			settled++
		case cityOpenWorldEnterpriseFreightReceiptStateExpired:
			expired++
		case cityOpenWorldEnterpriseFreightReceiptStateVoided:
			voided++
		case cityOpenWorldEnterpriseFreightReceiptStateOrphaned:
			orphaned++
		}
	}
	if p.AwaitingRouteCount != awaitingRoute || p.InTransitCount != inTransit ||
		p.AwaitingReceiptCount != awaitingReceipt || p.ReceivedCount != received || p.SettledCount != settled ||
		p.ExpiredCount != expired || p.VoidedCount != voided || p.OrphanedCount != orphaned {
		return errors.New("enterprise-freight shipment state counters are inconsistent")
	}

	lineCounts := make(map[string]int, len(shipments))
	lineTotals := make(map[string]int64, len(shipments))
	lineKeys := make(map[string]struct{}, len(state.Lines))
	for _, line := range state.Lines {
		if _, exists := shipments[line.ShipmentCode]; !exists || line.LineNo < 1 ||
			!cityPhysicalCodePattern.MatchString(line.ResourceCode) ||
			!cityPhysicalCodePattern.MatchString(line.SourceFirmCode) ||
			!cityPhysicalCodePattern.MatchString(line.SourceDistrictCode) ||
			!cityPhysicalCodePattern.MatchString(line.DestinationFirmCode) ||
			!cityPhysicalCodePattern.MatchString(line.DestinationDistrictCode) ||
			line.QuantityUnits <= 0 || line.UnitPriceUnits <= 0 || line.TotalPriceUnits <= 0 ||
			line.QuantityUnits > math.MaxInt64/line.UnitPriceUnits ||
			line.QuantityUnits*line.UnitPriceUnits != line.TotalPriceUnits ||
			!cityOpenWorldEnterpriseFreightReceiptJSONObject(line.Metadata) {
			return errors.New("invalid enterprise-freight shipment line")
		}
		key := fmt.Sprintf("%s\x00%d", line.ShipmentCode, line.LineNo)
		if _, exists := lineKeys[key]; exists {
			return errors.New("duplicate enterprise-freight shipment line")
		}
		lineKeys[key] = struct{}{}
		if line.QuantityUnits > math.MaxInt64-lineTotals[line.ShipmentCode] {
			return errors.New("enterprise-freight shipment line quantity overflows")
		}
		lineTotals[line.ShipmentCode] += line.QuantityUnits
		lineCounts[line.ShipmentCode]++
	}
	for code, shipment := range shipments {
		if lineCounts[code] < 1 || lineTotals[code] != shipment.RequestedUnits {
			return errors.New("enterprise-freight shipment lines are inconsistent")
		}
	}

	facts := make(map[string]CityOpenWorldEnterpriseFreightReceiptFact, len(state.Facts))
	for _, fact := range state.Facts {
		if _, exists := shipments[fact.ShipmentCode]; !exists || fact.Tick <= 0 || fact.Sequence <= 0 ||
			!cityOpenWorldEnterpriseFreightReceiptFactTypeValid(fact.FactType) ||
			!cityOpenWorldEnterpriseFreightReceiptEvidenceKindValid(fact.EvidenceKind) ||
			!cityOpenWorldEnterpriseFreightReceiptJSONObject(fact.Payload) {
			return errors.New("invalid enterprise-freight receipt fact")
		}
		if fact.EvidenceKind == cityOpenWorldEnterpriseFreightReceiptEvidenceFreight {
			if fact.FreightSourceCode == nil || *fact.FreightSourceCode != shipments[fact.ShipmentCode].FreightSourceCode || fact.SupplyOrderCode != nil {
				return errors.New("invalid enterprise-freight receipt freight evidence")
			}
		} else if fact.SupplyOrderCode == nil || *fact.SupplyOrderCode != shipments[fact.ShipmentCode].OrderCode || fact.FreightSourceCode != nil {
			return errors.New("invalid enterprise-freight receipt supply evidence")
		}
		key := cityOpenWorldEnterpriseFreightReceiptFactKey(fact.ShipmentCode, CityOpenWorldEnterpriseFreightReceiptFactRef{
			EvidenceKind: fact.EvidenceKind, Tick: fact.Tick, Sequence: fact.Sequence,
		})
		if _, exists := facts[key]; exists {
			return errors.New("duplicate enterprise-freight receipt fact")
		}
		facts[key] = fact
	}
	for code, shipment := range shipments {
		root, exists := facts[cityOpenWorldEnterpriseFreightReceiptFactKey(code, shipment.SourceEvidence)]
		if !exists || root.FactType != "shipment.created" ||
			root.EvidenceKind != cityOpenWorldEnterpriseFreightReceiptEvidenceFreight {
			return errors.New("enterprise-freight shipment root fact is inconsistent")
		}
		last, exists := facts[cityOpenWorldEnterpriseFreightReceiptFactKey(code, shipment.LastFact)]
		if !exists || last.FactType != cityOpenWorldEnterpriseFreightReceiptLastFactType(shipment.State) {
			return errors.New("enterprise-freight shipment last fact is inconsistent")
		}
	}

	transitionCounts := make(map[string]int, len(shipments))
	lastStates := make(map[string]string, len(shipments))
	lastFacts := make(map[string]CityOpenWorldEnterpriseFreightReceiptFactRef, len(shipments))
	for _, transition := range state.Transitions {
		shipment, exists := shipments[transition.ShipmentCode]
		fact, factExists := facts[cityOpenWorldEnterpriseFreightReceiptFactKey(transition.ShipmentCode, transition.SourceFact)]
		if !exists || !factExists || transition.TransitionTick != transition.SourceFact.Tick ||
			transition.TransitionSequence != transition.SourceFact.Sequence || transition.TransitionTick <= 0 ||
			transition.TransitionSequence <= 0 || !cityOpenWorldEnterpriseFreightReceiptStateValid(transition.State) ||
			!cityOpenWorldSupplyChainReasonValid(transition.ReasonCode) ||
			!cityOpenWorldEnterpriseFreightReceiptTransitionFactMatchesState(fact.FactType, transition.State) ||
			!cityOpenWorldEnterpriseFreightReceiptTransitionReasonMatchesState(transition.State, transition.ReasonCode) ||
			!cityOpenWorldEnterpriseFreightReceiptTransitionAllowed(lastStates[transition.ShipmentCode], transition.State) ||
			!cityOpenWorldEnterpriseFreightReceiptJSONObject(transition.Metadata) {
			return errors.New("invalid enterprise-freight shipment transition")
		}
		_ = shipment
		lastStates[transition.ShipmentCode] = transition.State
		lastFacts[transition.ShipmentCode] = transition.SourceFact
		transitionCounts[transition.ShipmentCode]++
	}
	for code, shipment := range shipments {
		if transitionCounts[code] < 1 || lastStates[code] != shipment.State ||
			lastFacts[code] != shipment.LastFact || shipment.Version != int64(transitionCounts[code]) {
			return errors.New("enterprise-freight shipment transition projection is inconsistent")
		}
	}

	receipts := make(map[string]CityOpenWorldEnterpriseFreightReceipt, len(state.Receipts))
	for _, receipt := range state.Receipts {
		shipment, exists := shipments[receipt.ShipmentCode]
		if !exists || receipt.OrderCode != shipment.OrderCode || receipt.ReceivedTick <= 0 ||
			receipt.DeliveryFact.Tick <= 0 || receipt.DeliveryFact.Sequence <= 0 ||
			receipt.ResourceOperation.Tick <= 0 || receipt.ResourceOperation.Sequence <= 0 ||
			receipt.SourceFact.EvidenceKind != cityOpenWorldEnterpriseFreightReceiptEvidenceSupplyChain ||
			!cityOpenWorldEnterpriseFreightReceiptJSONObject(receipt.Metadata) {
			return errors.New("invalid enterprise-freight receipt")
		}
		fact, factExists := facts[cityOpenWorldEnterpriseFreightReceiptFactKey(receipt.ShipmentCode, receipt.SourceFact)]
		if !factExists || fact.FactType != "receipt.confirmed" ||
			fact.EvidenceKind != cityOpenWorldEnterpriseFreightReceiptEvidenceSupplyChain ||
			fact.Tick != receipt.DeliveryFact.Tick || fact.Sequence != receipt.DeliveryFact.Sequence ||
			shipment.State != cityOpenWorldEnterpriseFreightReceiptStateReceived {
			return errors.New("enterprise-freight receipt proof is inconsistent")
		}
		if _, duplicate := receipts[receipt.ShipmentCode]; duplicate {
			return errors.New("duplicate enterprise-freight receipt")
		}
		receipts[receipt.ShipmentCode] = receipt
	}
	for code, shipment := range shipments {
		_, hasReceipt := receipts[code]
		if (shipment.State == cityOpenWorldEnterpriseFreightReceiptStateReceived) != hasReceipt {
			return errors.New("enterprise-freight shipment receipt terminal state is inconsistent")
		}
	}
	return nil
}

func cityOpenWorldEnterpriseFreightReceiptStateValid(value string) bool {
	switch value {
	case cityOpenWorldEnterpriseFreightReceiptStateAwaitingRoute,
		cityOpenWorldEnterpriseFreightReceiptStateInTransit,
		cityOpenWorldEnterpriseFreightReceiptStateAwaitingReceipt,
		cityOpenWorldEnterpriseFreightReceiptStateReceived,
		cityOpenWorldEnterpriseFreightReceiptStateSettled,
		cityOpenWorldEnterpriseFreightReceiptStateExpired,
		cityOpenWorldEnterpriseFreightReceiptStateVoided,
		cityOpenWorldEnterpriseFreightReceiptStateOrphaned:
		return true
	default:
		return false
	}
}

func cityOpenWorldEnterpriseFreightReceiptEvidenceKindValid(value string) bool {
	return value == cityOpenWorldEnterpriseFreightReceiptEvidenceFreight ||
		value == cityOpenWorldEnterpriseFreightReceiptEvidenceSupplyChain
}

func cityOpenWorldEnterpriseFreightReceiptFactTypeValid(value string) bool {
	switch value {
	case "shipment.created", "route.awaiting", "transport.in_transit", "transport.arrived",
		"transport.expired", "transport.voided", "transport.orphaned", "receipt.confirmed", "settlement.confirmed":
		return true
	default:
		return false
	}
}

func cityOpenWorldEnterpriseFreightReceiptTransitionFactMatchesState(factType, state string) bool {
	switch state {
	case cityOpenWorldEnterpriseFreightReceiptStateAwaitingRoute:
		return factType == "route.awaiting"
	case cityOpenWorldEnterpriseFreightReceiptStateInTransit:
		return factType == "transport.in_transit"
	case cityOpenWorldEnterpriseFreightReceiptStateAwaitingReceipt:
		return factType == "transport.arrived"
	case cityOpenWorldEnterpriseFreightReceiptStateReceived:
		return factType == "receipt.confirmed"
	case cityOpenWorldEnterpriseFreightReceiptStateSettled:
		return factType == "settlement.confirmed"
	case cityOpenWorldEnterpriseFreightReceiptStateExpired:
		return factType == "transport.expired"
	case cityOpenWorldEnterpriseFreightReceiptStateVoided:
		return factType == "transport.voided"
	case cityOpenWorldEnterpriseFreightReceiptStateOrphaned:
		return factType == "transport.orphaned"
	default:
		return false
	}
}

func cityOpenWorldEnterpriseFreightReceiptLastFactType(state string) string {
	switch state {
	case cityOpenWorldEnterpriseFreightReceiptStateAwaitingRoute:
		return "route.awaiting"
	case cityOpenWorldEnterpriseFreightReceiptStateInTransit:
		return "transport.in_transit"
	case cityOpenWorldEnterpriseFreightReceiptStateAwaitingReceipt:
		return "transport.arrived"
	case cityOpenWorldEnterpriseFreightReceiptStateReceived:
		return "receipt.confirmed"
	case cityOpenWorldEnterpriseFreightReceiptStateSettled:
		return "settlement.confirmed"
	case cityOpenWorldEnterpriseFreightReceiptStateExpired:
		return "transport.expired"
	case cityOpenWorldEnterpriseFreightReceiptStateVoided:
		return "transport.voided"
	case cityOpenWorldEnterpriseFreightReceiptStateOrphaned:
		return "transport.orphaned"
	default:
		return ""
	}
}

func cityOpenWorldEnterpriseFreightReceiptTransitionReasonMatchesState(state, reason string) bool {
	switch state {
	case cityOpenWorldEnterpriseFreightReceiptStateAwaitingRoute:
		return reason == cityOpenWorldEnterpriseFreightReceiptReasonDemandPending
	case cityOpenWorldEnterpriseFreightReceiptStateInTransit:
		return reason == cityOpenWorldEnterpriseFreightReceiptReasonScheduled
	case cityOpenWorldEnterpriseFreightReceiptStateAwaitingReceipt:
		return reason == cityOpenWorldEnterpriseFreightReceiptReasonCompleted
	case cityOpenWorldEnterpriseFreightReceiptStateReceived:
		return reason == cityOpenWorldEnterpriseFreightReceiptReasonReceived
	case cityOpenWorldEnterpriseFreightReceiptStateSettled:
		return reason == cityOpenWorldEnterpriseFreightReceiptReasonSettled
	case cityOpenWorldEnterpriseFreightReceiptStateExpired:
		return reason == cityOpenWorldEnterpriseFreightReceiptReasonExpired
	case cityOpenWorldEnterpriseFreightReceiptStateVoided:
		return reason == cityOpenWorldEnterpriseFreightReceiptReasonVoided
	case cityOpenWorldEnterpriseFreightReceiptStateOrphaned:
		return reason == cityOpenWorldEnterpriseFreightReceiptReasonOrphaned
	default:
		return false
	}
}

func cityOpenWorldEnterpriseFreightReceiptTransitionAllowed(previous, next string) bool {
	switch previous {
	case "":
		return next == cityOpenWorldEnterpriseFreightReceiptStateAwaitingRoute
	case cityOpenWorldEnterpriseFreightReceiptStateAwaitingRoute:
		return next == cityOpenWorldEnterpriseFreightReceiptStateInTransit ||
			next == cityOpenWorldEnterpriseFreightReceiptStateExpired ||
			next == cityOpenWorldEnterpriseFreightReceiptStateVoided
	case cityOpenWorldEnterpriseFreightReceiptStateInTransit:
		return next == cityOpenWorldEnterpriseFreightReceiptStateAwaitingReceipt ||
			next == cityOpenWorldEnterpriseFreightReceiptStateOrphaned
	case cityOpenWorldEnterpriseFreightReceiptStateAwaitingReceipt:
		return next == cityOpenWorldEnterpriseFreightReceiptStateReceived ||
			next == cityOpenWorldEnterpriseFreightReceiptStateSettled ||
			next == cityOpenWorldEnterpriseFreightReceiptStateOrphaned
	case cityOpenWorldEnterpriseFreightReceiptStateExpired,
		cityOpenWorldEnterpriseFreightReceiptStateVoided,
		cityOpenWorldEnterpriseFreightReceiptStateOrphaned:
		return next == cityOpenWorldEnterpriseFreightReceiptStateSettled
	default:
		return false
	}
}

func cityOpenWorldEnterpriseFreightReceiptFactKey(shipmentCode string, reference CityOpenWorldEnterpriseFreightReceiptFactRef) string {
	return fmt.Sprintf("%s\x00%s\x00%d\x00%d", shipmentCode, reference.EvidenceKind, reference.Tick, reference.Sequence)
}

func cityOpenWorldEnterpriseFreightReceiptJSONObject(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var object map[string]json.RawMessage
	return json.Unmarshal(raw, &object) == nil && object != nil
}

func cityOpenWorldEnterpriseFreightReceiptPolicyMetadataValid(raw json.RawMessage) bool {
	var metadata struct {
		SchemaVersion int    `json:"schema_version"`
		Scope         string `json:"scope"`
		Inventory     string `json:"inventory"`
		Legacy        string `json:"legacy"`
	}
	return json.Unmarshal(raw, &metadata) == nil &&
		metadata.SchemaVersion == cityOpenWorldEnterpriseFreightReceiptSchemaVersion &&
		metadata.Scope == "v16_transport_custody_and_v15_receipt_gate" &&
		metadata.Inventory == "v15_only_until_delivery" &&
		metadata.Legacy == "pre_v17_sources_untracked"
}

func sortCityOpenWorldEnterpriseFreightReceiptState(state *CityOpenWorldEnterpriseFreightReceiptState) {
	if state == nil {
		return
	}
	sort.Slice(state.Shipments, func(i, j int) bool { return state.Shipments[i].Code < state.Shipments[j].Code })
	sort.Slice(state.Lines, func(i, j int) bool {
		return state.Lines[i].ShipmentCode < state.Lines[j].ShipmentCode ||
			state.Lines[i].ShipmentCode == state.Lines[j].ShipmentCode && state.Lines[i].LineNo < state.Lines[j].LineNo
	})
	sort.Slice(state.Facts, func(i, j int) bool {
		left, right := state.Facts[i], state.Facts[j]
		return left.ShipmentCode < right.ShipmentCode ||
			left.ShipmentCode == right.ShipmentCode && (left.Tick < right.Tick ||
				left.Tick == right.Tick && (left.Sequence < right.Sequence ||
					left.Sequence == right.Sequence && left.EvidenceKind < right.EvidenceKind))
	})
	sort.Slice(state.Transitions, func(i, j int) bool {
		left, right := state.Transitions[i], state.Transitions[j]
		return left.ShipmentCode < right.ShipmentCode ||
			left.ShipmentCode == right.ShipmentCode && (left.TransitionTick < right.TransitionTick ||
				left.TransitionTick == right.TransitionTick && (left.TransitionSequence < right.TransitionSequence ||
					left.TransitionSequence == right.TransitionSequence && left.SourceFact.EvidenceKind < right.SourceFact.EvidenceKind))
	})
	sort.Slice(state.Receipts, func(i, j int) bool { return state.Receipts[i].ShipmentCode < state.Receipts[j].ShipmentCode })
}
