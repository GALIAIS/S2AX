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
	cityOpenWorldFreightBatchSchemaVersion                   = 1
	cityOpenWorldFreightBatchProfileID                       = "sub2api-open-world-freight-batches"
	cityOpenWorldFreightBatchProfileVersion                  = "1.0.0"
	cityOpenWorldFreightBatchSourceContract                  = "v16_suppressed_overflow_source_v1"
	cityOpenWorldFreightBatchPackingContract                 = "stable_line_capacity_packing_v1"
	cityOpenWorldFreightBatchTransportContract               = "v9_freight_consignment_demand_v1"
	cityOpenWorldFreightBatchReceiptContract                 = "all_consignment_arrivals_then_v15_atomic_delivery_v1"
	cityOpenWorldFreightBatchMaximumUnits                    = int64(32)
	cityOpenWorldFreightBatchMaximumConsignmentsPerPlan      = 128
	cityOpenWorldFreightBatchMaximumPlansPerTick             = 64
	cityOpenWorldFreightBatchMaximumObservationsPerTick      = 128
	cityOpenWorldFreightBatchPlanStateActive                 = "active"
	cityOpenWorldFreightBatchPlanStateReady                  = "ready"
	cityOpenWorldFreightBatchPlanStateReceived               = "received"
	cityOpenWorldFreightBatchPlanStateSettled                = "settled"
	cityOpenWorldFreightBatchPlanStateBlocked                = "blocked"
	cityOpenWorldFreightBatchConsignmentStateAwaitingRoute   = "awaiting_route"
	cityOpenWorldFreightBatchConsignmentStateInTransit       = "in_transit"
	cityOpenWorldFreightBatchConsignmentStateAwaitingReceipt = "awaiting_receipt"
	cityOpenWorldFreightBatchConsignmentStateReceived        = "received"
	cityOpenWorldFreightBatchConsignmentStateSettled         = "settled"
	cityOpenWorldFreightBatchConsignmentStateExpired         = "expired"
	cityOpenWorldFreightBatchConsignmentStateVoided          = "voided"
	cityOpenWorldFreightBatchConsignmentStateOrphaned        = "orphaned"
	cityOpenWorldFreightBatchEvidenceRuntime                 = "runtime"
	cityOpenWorldFreightBatchEvidenceSupplyChain             = "supply_chain"
	cityOpenWorldFreightBatchReasonDispatched                = "v16_overflow_suppressed"
	cityOpenWorldFreightBatchReasonScheduled                 = "v9_route_scheduled"
	cityOpenWorldFreightBatchReasonCompleted                 = "v9_route_completed"
	cityOpenWorldFreightBatchReasonExpired                   = "v9_demand_expired"
	cityOpenWorldFreightBatchReasonVoided                    = "v15_terminal_pending"
	cityOpenWorldFreightBatchReasonOrphaned                  = "v15_terminal_in_transit"
	cityOpenWorldFreightBatchReasonReceived                  = "v15_atomic_delivery_received"
	cityOpenWorldFreightBatchReasonSettled                   = "v22_freight_settlement_completed"
	cityOpenWorldRuntimeFactFreightBatchConsignmentCreated   = "system.enterprise_freight_batch.consignment.created"
	cityOpenWorldRuntimeFactFreightBatchRouteScheduled       = "system.enterprise_freight_batch.route.scheduled"
	cityOpenWorldRuntimeFactFreightBatchRouteCompleted       = "system.enterprise_freight_batch.route.completed"
	cityOpenWorldRuntimeFactFreightBatchDemandExpired        = "system.enterprise_freight_batch.demand.expired"
	cityOpenWorldRuntimeFactFreightBatchTransportOrphaned    = "system.enterprise_freight_batch.transport.orphaned"
)

// CityOpenWorldFreightBatchPolicy pins V18's overflow-only batching contract.
// It does not own inventory or cash: its receipts prove that all independent
// transport consignments preceded one V15 atomic delivery.
type CityOpenWorldFreightBatchPolicy struct {
	ProfileID                  string          `json:"profile_id"`
	ProfileVersion             string          `json:"profile_version"`
	ContentHash                string          `json:"content_hash"`
	BaselineTick               int64           `json:"baseline_tick"`
	SourceContract             string          `json:"source_contract"`
	PackingContract            string          `json:"packing_contract"`
	TransportContract          string          `json:"transport_contract"`
	ReceiptContract            string          `json:"receipt_contract"`
	MaximumUnits               int64           `json:"maximum_units"`
	MaximumConsignmentsPerPlan int             `json:"maximum_consignments_per_plan"`
	MaximumPlansPerTick        int             `json:"maximum_plans_per_tick"`
	MaximumObservationsPerTick int             `json:"maximum_observations_per_tick"`
	PlanCount                  int64           `json:"plan_count"`
	ConsignmentCount           int64           `json:"consignment_count"`
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

type CityOpenWorldFreightBatchFactRef struct {
	EvidenceKind string `json:"evidence_kind"`
	Tick         int64  `json:"tick"`
	Sequence     int64  `json:"sequence"`
}

type CityOpenWorldFreightBatchPlan struct {
	Code               string                           `json:"code"`
	OverflowSourceCode string                           `json:"overflow_source_code"`
	OrderCode          string                           `json:"order_code"`
	SellerNodeCode     string                           `json:"seller_node_code"`
	BuyerNodeCode      string                           `json:"buyer_node_code"`
	SourceHubCode      string                           `json:"source_hub_code"`
	DestinationHubCode string                           `json:"destination_hub_code"`
	CarrierActorCode   string                           `json:"carrier_actor_code"`
	SourceTick         int64                            `json:"source_tick"`
	RequiredUnits      int64                            `json:"required_units"`
	ConsignmentCount   int                              `json:"consignment_count"`
	State              string                           `json:"state"`
	SourceFact         CityOpenWorldFreightBatchFactRef `json:"source_fact"`
	LastFact           CityOpenWorldFreightBatchFactRef `json:"last_fact"`
	Version            int64                            `json:"version"`
	Metadata           json.RawMessage                  `json:"metadata"`
}

type CityOpenWorldFreightBatchConsignment struct {
	Code           string                           `json:"code"`
	PlanCode       string                           `json:"plan_code"`
	BatchNo        int                              `json:"batch_no"`
	RequestedUnits int64                            `json:"requested_units"`
	State          string                           `json:"state"`
	DemandCode     string                           `json:"demand_code"`
	RouteCode      *string                          `json:"route_code,omitempty"`
	SourceFact     CityOpenWorldFreightBatchFactRef `json:"source_fact"`
	LastFact       CityOpenWorldFreightBatchFactRef `json:"last_fact"`
	Version        int64                            `json:"version"`
	Metadata       json.RawMessage                  `json:"metadata"`
}

type CityOpenWorldFreightBatchLine struct {
	ConsignmentCode string          `json:"consignment_code"`
	SourceLineNo    int             `json:"source_line_no"`
	ResourceCode    string          `json:"resource_code"`
	QuantityUnits   int64           `json:"quantity_units"`
	UnitPriceUnits  int64           `json:"unit_price_units"`
	TotalPriceUnits int64           `json:"total_price_units"`
	Metadata        json.RawMessage `json:"metadata"`
}

type CityOpenWorldFreightBatchFact struct {
	ConsignmentCode string          `json:"consignment_code"`
	Tick            int64           `json:"tick"`
	Sequence        int64           `json:"sequence"`
	FactType        string          `json:"fact_type"`
	EvidenceKind    string          `json:"evidence_kind"`
	Payload         json.RawMessage `json:"payload"`
}

type CityOpenWorldFreightBatchTransition struct {
	ConsignmentCode    string                           `json:"consignment_code"`
	TransitionTick     int64                            `json:"transition_tick"`
	TransitionSequence int64                            `json:"transition_sequence"`
	State              string                           `json:"state"`
	ReasonCode         string                           `json:"reason_code"`
	SourceFact         CityOpenWorldFreightBatchFactRef `json:"source_fact"`
	Metadata           json.RawMessage                  `json:"metadata"`
}

type CityOpenWorldFreightBatchReceipt struct {
	ConsignmentCode   string                           `json:"consignment_code"`
	PlanCode          string                           `json:"plan_code"`
	OrderCode         string                           `json:"order_code"`
	ReceivedTick      int64                            `json:"received_tick"`
	DeliveryFact      CityOpenWorldRuntimeFactRef      `json:"delivery_fact"`
	ResourceOperation CityResourceOperationCursor      `json:"resource_operation"`
	SourceFact        CityOpenWorldFreightBatchFactRef `json:"source_fact"`
	Metadata          json.RawMessage                  `json:"metadata"`
}

type CityOpenWorldFreightBatchState struct {
	Policy       CityOpenWorldFreightBatchPolicy        `json:"policy"`
	Plans        []CityOpenWorldFreightBatchPlan        `json:"plans"`
	Consignments []CityOpenWorldFreightBatchConsignment `json:"consignments"`
	Lines        []CityOpenWorldFreightBatchLine        `json:"lines"`
	Facts        []CityOpenWorldFreightBatchFact        `json:"facts"`
	Transitions  []CityOpenWorldFreightBatchTransition  `json:"transitions"`
	Receipts     []CityOpenWorldFreightBatchReceipt     `json:"receipts"`
}

type cityOpenWorldFreightBatchSourceLine struct {
	LineNo          int
	ResourceCode    string
	QuantityUnits   int64
	UnitPriceUnits  int64
	TotalPriceUnits int64
}

type cityOpenWorldFreightBatchPackedLine struct {
	SourceLineNo    int
	ResourceCode    string
	QuantityUnits   int64
	UnitPriceUnits  int64
	TotalPriceUnits int64
}

type cityOpenWorldFreightBatchPackedConsignment struct {
	BatchNo        int
	RequestedUnits int64
	Lines          []cityOpenWorldFreightBatchPackedLine
}

func cityOpenWorldFreightBatchPolicyHash() (string, error) {
	raw, err := json.Marshal(struct {
		SchemaVersion              int    `json:"schema_version"`
		ProfileID                  string `json:"profile_id"`
		ProfileVersion             string `json:"profile_version"`
		SourceContract             string `json:"source_contract"`
		PackingContract            string `json:"packing_contract"`
		TransportContract          string `json:"transport_contract"`
		ReceiptContract            string `json:"receipt_contract"`
		MaximumUnits               int64  `json:"maximum_units"`
		MaximumConsignmentsPerPlan int    `json:"maximum_consignments_per_plan"`
		MaximumPlansPerTick        int    `json:"maximum_plans_per_tick"`
		MaximumObservationsPerTick int    `json:"maximum_observations_per_tick"`
	}{
		SchemaVersion:              cityOpenWorldFreightBatchSchemaVersion,
		ProfileID:                  cityOpenWorldFreightBatchProfileID,
		ProfileVersion:             cityOpenWorldFreightBatchProfileVersion,
		SourceContract:             cityOpenWorldFreightBatchSourceContract,
		PackingContract:            cityOpenWorldFreightBatchPackingContract,
		TransportContract:          cityOpenWorldFreightBatchTransportContract,
		ReceiptContract:            cityOpenWorldFreightBatchReceiptContract,
		MaximumUnits:               cityOpenWorldFreightBatchMaximumUnits,
		MaximumConsignmentsPerPlan: cityOpenWorldFreightBatchMaximumConsignmentsPerPlan,
		MaximumPlansPerTick:        cityOpenWorldFreightBatchMaximumPlansPerTick,
		MaximumObservationsPerTick: cityOpenWorldFreightBatchMaximumObservationsPerTick,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func cityOpenWorldFreightBatchPlanCode(overflowSourceCode string) string {
	sum := sha256.Sum256([]byte("v18\x00" + overflowSourceCode))
	return "freight.batch.plan." + hex.EncodeToString(sum[:20])
}

func cityOpenWorldFreightBatchConsignmentCode(planCode string, batchNo int) string {
	sum := sha256.Sum256([]byte(planCode + "\x00" + fmt.Sprintf("%d", batchNo)))
	return "freight.batch.consignment." + hex.EncodeToString(sum[:20])
}

func cityOpenWorldFreightBatchDemandCode(consignmentCode string) string {
	sum := sha256.Sum256([]byte("demand\x00" + consignmentCode))
	return "mobility.freight.batch." + hex.EncodeToString(sum[:20])
}

func activateCityOpenWorldFreightBatchBootstrapWrite(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if _, err := tx.ExecContext(ctx, `
SELECT set_config('sub2api.city_open_world_freight_batch_bootstrap_world_id', $1, TRUE)`, fmt.Sprintf("%d", worldID)); err != nil {
		return fmt.Errorf("enable open-world V18 freight-batch bootstrap: %w", err)
	}
	return nil
}

func activateCityOpenWorldFreightBatchWrite(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if _, err := tx.ExecContext(ctx, `
SELECT set_config('sub2api.city_open_world_freight_batch_world_id', $1, TRUE)`, fmt.Sprintf("%d", worldID)); err != nil {
		return fmt.Errorf("enable open-world V18 freight-batch write: %w", err)
	}
	return nil
}

func assertCityOpenWorldFreightBatchFoundation(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if _, err := tx.ExecContext(ctx, `SELECT assert_city_open_world_freight_batch_foundation($1)`, worldID); err != nil {
		return fmt.Errorf("validate V18 freight-batch foundation: %w", err)
	}
	return nil
}

func initializeCityOpenWorldV18FreightBatchFoundation(ctx context.Context, tx *sql.Tx, worldID int64) error {
	var version string
	var baselineTick int64
	if err := tx.QueryRowContext(ctx, `
SELECT simulation_version, current_tick
FROM city_worlds
WHERE id = $1
FOR UPDATE`, worldID).Scan(&version, &baselineTick); err != nil {
		return fmt.Errorf("lock V18 freight-batch world: %w", err)
	}
	if !cityEngineSupportsOpenWorldFreightBatches(version) || baselineTick < 0 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v18_freight_batch_world"})
	}
	if _, err := tx.ExecContext(ctx, `SELECT assert_city_open_world_enterprise_freight_foundation($1)`, worldID); err != nil {
		return fmt.Errorf("validate V18 freight-batch V16 prerequisite: %w", err)
	}
	if err := assertCityOpenWorldEnterpriseFreightReceiptFoundation(ctx, tx, worldID); err != nil {
		return fmt.Errorf("validate V18 freight-batch V17 prerequisite: %w", err)
	}
	hash, err := cityOpenWorldFreightBatchPolicyHash()
	if err != nil {
		return fmt.Errorf("hash V18 freight-batch profile: %w", err)
	}
	metadata, err := json.Marshal(map[string]any{
		"schema_version": cityOpenWorldFreightBatchSchemaVersion,
		"scope":          "v16_suppressed_overflow_to_v9_multi_consignment",
		"inventory":      "v15_atomic_delivery_only",
		"legacy":         "pre_v18_overflow_sources_untracked",
	})
	if err != nil {
		return fmt.Errorf("marshal V18 freight-batch profile metadata: %w", err)
	}
	if err = activateCityOpenWorldFreightBatchBootstrapWrite(ctx, tx, worldID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_freight_batch_profiles
    (world_id, profile_id, profile_version, content_hash, baseline_tick,
     source_contract, packing_contract, transport_contract, receipt_contract,
     maximum_units, maximum_consignments_per_plan, maximum_plans_per_tick,
     maximum_observations_per_tick, revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, 1, $14::jsonb)`,
		worldID, cityOpenWorldFreightBatchProfileID, cityOpenWorldFreightBatchProfileVersion,
		hash, baselineTick, cityOpenWorldFreightBatchSourceContract,
		cityOpenWorldFreightBatchPackingContract, cityOpenWorldFreightBatchTransportContract,
		cityOpenWorldFreightBatchReceiptContract, cityOpenWorldFreightBatchMaximumUnits,
		cityOpenWorldFreightBatchMaximumConsignmentsPerPlan, cityOpenWorldFreightBatchMaximumPlansPerTick,
		cityOpenWorldFreightBatchMaximumObservationsPerTick, []byte(metadata)); err != nil {
		return fmt.Errorf("insert V18 freight-batch profile: %w", err)
	}
	return assertCityOpenWorldFreightBatchFoundation(ctx, tx, worldID)
}

func loadCityOpenWorldFreightBatchState(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*CityOpenWorldFreightBatchState, error) {
	state := &CityOpenWorldFreightBatchState{
		Plans: make([]CityOpenWorldFreightBatchPlan, 0), Consignments: make([]CityOpenWorldFreightBatchConsignment, 0),
		Lines: make([]CityOpenWorldFreightBatchLine, 0), Facts: make([]CityOpenWorldFreightBatchFact, 0),
		Transitions: make([]CityOpenWorldFreightBatchTransition, 0), Receipts: make([]CityOpenWorldFreightBatchReceipt, 0),
	}
	err := queryer.QueryRowContext(ctx, `
SELECT profile_id, profile_version, content_hash, baseline_tick,
       source_contract, packing_contract, transport_contract, receipt_contract,
       maximum_units, maximum_consignments_per_plan, maximum_plans_per_tick,
       maximum_observations_per_tick, plan_count, consignment_count,
       awaiting_route_count, in_transit_count, awaiting_receipt_count,
       received_count, settled_count, expired_count, voided_count, orphaned_count,
       fact_count, transition_count, receipt_count, revision, metadata
FROM city_open_world_freight_batch_profiles
WHERE world_id = $1`, worldID).Scan(
		&state.Policy.ProfileID, &state.Policy.ProfileVersion, &state.Policy.ContentHash, &state.Policy.BaselineTick,
		&state.Policy.SourceContract, &state.Policy.PackingContract, &state.Policy.TransportContract,
		&state.Policy.ReceiptContract, &state.Policy.MaximumUnits, &state.Policy.MaximumConsignmentsPerPlan,
		&state.Policy.MaximumPlansPerTick, &state.Policy.MaximumObservationsPerTick,
		&state.Policy.PlanCount, &state.Policy.ConsignmentCount, &state.Policy.AwaitingRouteCount,
		&state.Policy.InTransitCount, &state.Policy.AwaitingReceiptCount, &state.Policy.ReceivedCount, &state.Policy.SettledCount,
		&state.Policy.ExpiredCount, &state.Policy.VoidedCount, &state.Policy.OrphanedCount,
		&state.Policy.FactCount, &state.Policy.TransitionCount, &state.Policy.ReceiptCount,
		&state.Policy.Revision, &state.Policy.Metadata,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v18_freight_batch_profile"})
	}
	if err != nil {
		return nil, fmt.Errorf("load V18 freight-batch profile: %w", err)
	}
	planRows, err := queryer.QueryContext(ctx, `
SELECT plan.code, plan.overflow_source_code, plan.order_code,
       plan.seller_node_code, plan.buyer_node_code, plan.source_hub_code,
       plan.destination_hub_code, carrier.code, plan.source_tick,
       plan.required_units, plan.consignment_count, plan.state,
       source_fact.tick, source_fact.sequence, last_fact.tick, last_fact.sequence,
       plan.version, plan.metadata
FROM city_open_world_freight_batch_plans plan
JOIN city_open_world_actors carrier
  ON carrier.id = plan.carrier_actor_id AND carrier.world_id = plan.world_id
JOIN city_open_world_runtime_facts source_fact
  ON source_fact.id = plan.source_runtime_fact_id AND source_fact.world_id = plan.world_id
JOIN city_open_world_runtime_facts last_fact
  ON last_fact.id = plan.last_runtime_fact_id AND last_fact.world_id = plan.world_id
WHERE plan.world_id = $1
ORDER BY plan.source_tick, plan.code`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V18 freight-batch plans: %w", err)
	}
	for planRows.Next() {
		item := CityOpenWorldFreightBatchPlan{SourceFact: CityOpenWorldFreightBatchFactRef{EvidenceKind: cityOpenWorldFreightBatchEvidenceRuntime}, LastFact: CityOpenWorldFreightBatchFactRef{EvidenceKind: cityOpenWorldFreightBatchEvidenceRuntime}}
		if err = planRows.Scan(&item.Code, &item.OverflowSourceCode, &item.OrderCode,
			&item.SellerNodeCode, &item.BuyerNodeCode, &item.SourceHubCode, &item.DestinationHubCode,
			&item.CarrierActorCode, &item.SourceTick, &item.RequiredUnits, &item.ConsignmentCount,
			&item.State, &item.SourceFact.Tick, &item.SourceFact.Sequence,
			&item.LastFact.Tick, &item.LastFact.Sequence, &item.Version, &item.Metadata); err != nil {
			_ = planRows.Close()
			return nil, fmt.Errorf("scan V18 freight-batch plan: %w", err)
		}
		state.Plans = append(state.Plans, item)
	}
	if err = closeCityRows(planRows, "iterate V18 freight-batch plans"); err != nil {
		return nil, err
	}
	consignmentRows, err := queryer.QueryContext(ctx, `
SELECT consignment.code, consignment.plan_code, consignment.batch_no,
       consignment.requested_units, consignment.state, demand.code, route.code,
       source_fact.tick, source_fact.sequence, last_fact.tick, last_fact.sequence,
       consignment.version, consignment.metadata
FROM city_open_world_freight_batch_consignments consignment
JOIN city_open_world_mobility_demands demand
  ON demand.id = consignment.mobility_demand_id AND demand.world_id = consignment.world_id
LEFT JOIN city_open_world_mobility_routes route
  ON route.id = consignment.mobility_route_id AND route.world_id = consignment.world_id
JOIN city_open_world_runtime_facts source_fact
  ON source_fact.id = consignment.source_runtime_fact_id AND source_fact.world_id = consignment.world_id
JOIN city_open_world_runtime_facts last_fact
  ON last_fact.id = consignment.last_runtime_fact_id AND last_fact.world_id = consignment.world_id
WHERE consignment.world_id = $1
ORDER BY consignment.plan_code, consignment.batch_no`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V18 freight-batch consignments: %w", err)
	}
	for consignmentRows.Next() {
		item := CityOpenWorldFreightBatchConsignment{SourceFact: CityOpenWorldFreightBatchFactRef{EvidenceKind: cityOpenWorldFreightBatchEvidenceRuntime}, LastFact: CityOpenWorldFreightBatchFactRef{EvidenceKind: cityOpenWorldFreightBatchEvidenceRuntime}}
		var routeCode sql.NullString
		if err = consignmentRows.Scan(&item.Code, &item.PlanCode, &item.BatchNo, &item.RequestedUnits,
			&item.State, &item.DemandCode, &routeCode, &item.SourceFact.Tick, &item.SourceFact.Sequence,
			&item.LastFact.Tick, &item.LastFact.Sequence, &item.Version, &item.Metadata); err != nil {
			_ = consignmentRows.Close()
			return nil, fmt.Errorf("scan V18 freight-batch consignment: %w", err)
		}
		item.RouteCode = nullStringPointer(routeCode)
		state.Consignments = append(state.Consignments, item)
	}
	if err = closeCityRows(consignmentRows, "iterate V18 freight-batch consignments"); err != nil {
		return nil, err
	}
	lineRows, err := queryer.QueryContext(ctx, `
SELECT consignment_code, source_line_no, resource_code, quantity_units,
       unit_price_units, total_price_units, metadata
FROM city_open_world_freight_batch_lines
WHERE world_id = $1
ORDER BY consignment_code, source_line_no`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V18 freight-batch lines: %w", err)
	}
	for lineRows.Next() {
		item := CityOpenWorldFreightBatchLine{}
		if err = lineRows.Scan(&item.ConsignmentCode, &item.SourceLineNo, &item.ResourceCode,
			&item.QuantityUnits, &item.UnitPriceUnits, &item.TotalPriceUnits, &item.Metadata); err != nil {
			_ = lineRows.Close()
			return nil, fmt.Errorf("scan V18 freight-batch line: %w", err)
		}
		state.Lines = append(state.Lines, item)
	}
	if err = closeCityRows(lineRows, "iterate V18 freight-batch lines"); err != nil {
		return nil, err
	}
	factRows, err := queryer.QueryContext(ctx, `
SELECT consignment_code, tick, sequence, fact_type, evidence_kind, payload
FROM city_open_world_freight_batch_facts
WHERE world_id = $1
ORDER BY consignment_code, tick, sequence`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V18 freight-batch facts: %w", err)
	}
	for factRows.Next() {
		item := CityOpenWorldFreightBatchFact{}
		if err = factRows.Scan(&item.ConsignmentCode, &item.Tick, &item.Sequence,
			&item.FactType, &item.EvidenceKind, &item.Payload); err != nil {
			_ = factRows.Close()
			return nil, fmt.Errorf("scan V18 freight-batch fact: %w", err)
		}
		state.Facts = append(state.Facts, item)
	}
	if err = closeCityRows(factRows, "iterate V18 freight-batch facts"); err != nil {
		return nil, err
	}
	transitionRows, err := queryer.QueryContext(ctx, `
SELECT transition.consignment_code, transition.transition_tick, transition.transition_sequence,
       transition.state, transition.reason_code, fact.evidence_kind, fact.tick, fact.sequence,
       transition.metadata
FROM city_open_world_freight_batch_transitions transition
JOIN city_open_world_freight_batch_facts fact
  ON fact.id = transition.source_fact_id AND fact.world_id = transition.world_id
WHERE transition.world_id = $1
ORDER BY transition.consignment_code, transition.transition_tick, transition.transition_sequence`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V18 freight-batch transitions: %w", err)
	}
	for transitionRows.Next() {
		item := CityOpenWorldFreightBatchTransition{}
		if err = transitionRows.Scan(&item.ConsignmentCode, &item.TransitionTick, &item.TransitionSequence,
			&item.State, &item.ReasonCode, &item.SourceFact.EvidenceKind, &item.SourceFact.Tick,
			&item.SourceFact.Sequence, &item.Metadata); err != nil {
			_ = transitionRows.Close()
			return nil, fmt.Errorf("scan V18 freight-batch transition: %w", err)
		}
		state.Transitions = append(state.Transitions, item)
	}
	if err = closeCityRows(transitionRows, "iterate V18 freight-batch transitions"); err != nil {
		return nil, err
	}
	receiptRows, err := queryer.QueryContext(ctx, `
SELECT receipt.consignment_code, receipt.plan_code, receipt.order_code, receipt.received_tick,
       supply_fact.tick, supply_fact.sequence, operation.tick, operation.sequence,
       batch_fact.evidence_kind, batch_fact.tick, batch_fact.sequence, receipt.metadata
FROM city_open_world_freight_batch_receipts receipt
JOIN city_open_world_supply_chain_deliveries delivery
  ON delivery.id = receipt.supply_chain_delivery_id AND delivery.world_id = receipt.world_id
JOIN city_open_world_supply_chain_facts supply_fact
  ON supply_fact.id = delivery.source_fact_id AND supply_fact.world_id = delivery.world_id
JOIN city_resource_operations operation
  ON operation.id = receipt.resource_operation_id AND operation.world_id = receipt.world_id
JOIN city_open_world_freight_batch_facts batch_fact
  ON batch_fact.id = receipt.source_fact_id AND batch_fact.world_id = receipt.world_id
WHERE receipt.world_id = $1
ORDER BY receipt.plan_code, receipt.consignment_code`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V18 freight-batch receipts: %w", err)
	}
	for receiptRows.Next() {
		item := CityOpenWorldFreightBatchReceipt{}
		if err = receiptRows.Scan(&item.ConsignmentCode, &item.PlanCode, &item.OrderCode, &item.ReceivedTick,
			&item.DeliveryFact.Tick, &item.DeliveryFact.Sequence,
			&item.ResourceOperation.Tick, &item.ResourceOperation.Sequence,
			&item.SourceFact.EvidenceKind, &item.SourceFact.Tick, &item.SourceFact.Sequence, &item.Metadata); err != nil {
			_ = receiptRows.Close()
			return nil, fmt.Errorf("scan V18 freight-batch receipt: %w", err)
		}
		state.Receipts = append(state.Receipts, item)
	}
	if err = closeCityRows(receiptRows, "iterate V18 freight-batch receipts"); err != nil {
		return nil, err
	}
	if err = validateCityOpenWorldFreightBatchState(state); err != nil {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v18_freight_batch_state"}).WithCause(err)
	}
	return state, nil
}

func validateCityOpenWorldFreightBatchState(state *CityOpenWorldFreightBatchState) error {
	if state == nil {
		return errors.New("freight-batch state is required")
	}
	hash, err := cityOpenWorldFreightBatchPolicyHash()
	if err != nil {
		return err
	}
	policy := state.Policy
	if policy.ProfileID != cityOpenWorldFreightBatchProfileID || policy.ProfileVersion != cityOpenWorldFreightBatchProfileVersion ||
		policy.ContentHash != hash || policy.BaselineTick < 0 || policy.SourceContract != cityOpenWorldFreightBatchSourceContract ||
		policy.PackingContract != cityOpenWorldFreightBatchPackingContract || policy.TransportContract != cityOpenWorldFreightBatchTransportContract ||
		policy.ReceiptContract != cityOpenWorldFreightBatchReceiptContract || policy.MaximumUnits != cityOpenWorldFreightBatchMaximumUnits ||
		policy.MaximumConsignmentsPerPlan != cityOpenWorldFreightBatchMaximumConsignmentsPerPlan ||
		policy.MaximumPlansPerTick != cityOpenWorldFreightBatchMaximumPlansPerTick ||
		policy.MaximumObservationsPerTick != cityOpenWorldFreightBatchMaximumObservationsPerTick || policy.Revision < 1 ||
		!cityOpenWorldFreightBatchPolicyMetadataValid(policy.Metadata) {
		return errors.New("invalid freight-batch policy")
	}
	if policy.PlanCount != int64(len(state.Plans)) || policy.ConsignmentCount != int64(len(state.Consignments)) ||
		policy.FactCount != int64(len(state.Facts)) || policy.TransitionCount != int64(len(state.Transitions)) ||
		policy.ReceiptCount != int64(len(state.Receipts)) {
		return errors.New("freight-batch policy counters are inconsistent")
	}

	plans := make(map[string]CityOpenWorldFreightBatchPlan, len(state.Plans))
	planCounts := make(map[string]int, len(state.Plans))
	planSources := make(map[string]struct{}, len(state.Plans))
	planOrders := make(map[string]struct{}, len(state.Plans))
	stateCounts := map[string]int64{}
	for _, plan := range state.Plans {
		if !cityOpenWorldSupplyChainCodeValid(plan.Code) || !cityOpenWorldSupplyChainCodeValid(plan.OverflowSourceCode) ||
			!cityOpenWorldSupplyChainCodeValid(plan.OrderCode) || !cityOpenWorldSupplyChainCodeValid(plan.SellerNodeCode) ||
			!cityOpenWorldSupplyChainCodeValid(plan.BuyerNodeCode) || !cityOpenWorldSupplyChainCodeValid(plan.SourceHubCode) ||
			!cityOpenWorldSupplyChainCodeValid(plan.DestinationHubCode) || !cityOpenWorldSupplyChainCodeValid(plan.CarrierActorCode) ||
			plan.SellerNodeCode == plan.BuyerNodeCode || plan.SourceHubCode == plan.DestinationHubCode ||
			plan.SourceTick <= policy.BaselineTick || plan.RequiredUnits <= cityOpenWorldFreightBatchMaximumUnits ||
			plan.ConsignmentCount < 2 || plan.ConsignmentCount > policy.MaximumConsignmentsPerPlan ||
			!cityOpenWorldFreightBatchPlanStateValid(plan.State) || plan.SourceFact.EvidenceKind != cityOpenWorldFreightBatchEvidenceRuntime ||
			plan.LastFact.EvidenceKind != cityOpenWorldFreightBatchEvidenceRuntime || plan.SourceFact.Tick <= 0 ||
			plan.SourceFact.Sequence <= 0 || plan.LastFact.Tick < plan.SourceFact.Tick || plan.LastFact.Sequence <= 0 ||
			plan.SourceFact.Tick != plan.SourceTick || plan.Version < 1 || !cityOpenWorldFreightBatchJSONObject(plan.Metadata) ||
			plan.Code != cityOpenWorldFreightBatchPlanCode(plan.OverflowSourceCode) ||
			plan.CarrierActorCode != cityOpenWorldEnterpriseFreightCarrierActorCode {
			return errors.New("invalid freight-batch plan")
		}
		if _, exists := plans[plan.Code]; exists {
			return errors.New("duplicate freight-batch plan")
		}
		if _, exists := planSources[plan.OverflowSourceCode]; exists {
			return errors.New("duplicate freight-batch plan source")
		}
		if _, exists := planOrders[plan.OrderCode]; exists {
			return errors.New("duplicate freight-batch plan order")
		}
		plans[plan.Code] = plan
		planSources[plan.OverflowSourceCode] = struct{}{}
		planOrders[plan.OrderCode] = struct{}{}
	}
	consignments := make(map[string]CityOpenWorldFreightBatchConsignment, len(state.Consignments))
	consignmentLines := make(map[string][]CityOpenWorldFreightBatchLine, len(state.Consignments))
	batchNumbers := make(map[string]map[int]struct{}, len(state.Plans))
	for _, consignment := range state.Consignments {
		plan, exists := plans[consignment.PlanCode]
		if !exists || !cityOpenWorldSupplyChainCodeValid(consignment.Code) || !cityOpenWorldSupplyChainCodeValid(consignment.PlanCode) ||
			!cityOpenWorldSupplyChainCodeValid(consignment.DemandCode) || consignment.BatchNo < 1 ||
			consignment.RequestedUnits <= 0 || consignment.RequestedUnits > policy.MaximumUnits ||
			!cityOpenWorldFreightBatchConsignmentStateValid(consignment.State) || consignment.DemandCode == "" ||
			consignment.SourceFact.EvidenceKind != cityOpenWorldFreightBatchEvidenceRuntime ||
			consignment.LastFact.EvidenceKind != cityOpenWorldFreightBatchEvidenceRuntime ||
			consignment.SourceFact.Tick <= 0 || consignment.SourceFact.Sequence <= 0 || consignment.LastFact.Tick <= 0 ||
			consignment.LastFact.Sequence <= 0 || consignment.SourceFact.Tick < plan.SourceTick ||
			consignment.LastFact.Tick < consignment.SourceFact.Tick || consignment.Version < 1 ||
			!cityOpenWorldFreightBatchJSONObject(consignment.Metadata) ||
			consignment.Code != cityOpenWorldFreightBatchConsignmentCode(consignment.PlanCode, consignment.BatchNo) ||
			consignment.DemandCode != cityOpenWorldFreightBatchDemandCode(consignment.Code) {
			return errors.New("invalid freight-batch consignment")
		}
		requiresRoute := consignment.State == cityOpenWorldFreightBatchConsignmentStateInTransit ||
			consignment.State == cityOpenWorldFreightBatchConsignmentStateAwaitingReceipt ||
			consignment.State == cityOpenWorldFreightBatchConsignmentStateReceived || consignment.State == cityOpenWorldFreightBatchConsignmentStateSettled ||
			consignment.State == cityOpenWorldFreightBatchConsignmentStateOrphaned
		if requiresRoute {
			if consignment.RouteCode == nil || !cityOpenWorldSupplyChainCodeValid(*consignment.RouteCode) {
				return errors.New("freight-batch consignment route is invalid")
			}
		} else if consignment.RouteCode != nil {
			return errors.New("freight-batch consignment route is unexpected")
		}
		if _, exists := consignments[consignment.Code]; exists {
			return errors.New("duplicate freight-batch consignment")
		}
		if batchNumbers[consignment.PlanCode] == nil {
			batchNumbers[consignment.PlanCode] = make(map[int]struct{})
		}
		if _, exists := batchNumbers[consignment.PlanCode][consignment.BatchNo]; exists {
			return errors.New("duplicate freight-batch consignment batch number")
		}
		batchNumbers[consignment.PlanCode][consignment.BatchNo] = struct{}{}
		consignments[consignment.Code] = consignment
		planCounts[consignment.PlanCode]++
		stateCounts[consignment.State]++
	}
	lineKeys := make(map[string]struct{}, len(state.Lines))
	for _, line := range state.Lines {
		if _, exists := consignments[line.ConsignmentCode]; !exists || line.SourceLineNo < 1 ||
			!cityPhysicalCodePattern.MatchString(line.ResourceCode) ||
			line.QuantityUnits <= 0 || line.UnitPriceUnits <= 0 || line.TotalPriceUnits <= 0 ||
			line.QuantityUnits > math.MaxInt64/line.UnitPriceUnits || line.TotalPriceUnits != line.QuantityUnits*line.UnitPriceUnits ||
			!cityOpenWorldFreightBatchJSONObject(line.Metadata) {
			return errors.New("invalid freight-batch line")
		}
		key := fmt.Sprintf("%s\x00%d", line.ConsignmentCode, line.SourceLineNo)
		if _, exists := lineKeys[key]; exists {
			return errors.New("duplicate freight-batch line")
		}
		lineKeys[key] = struct{}{}
		consignmentLines[line.ConsignmentCode] = append(consignmentLines[line.ConsignmentCode], line)
	}
	for code, consignment := range consignments {
		lines := consignmentLines[code]
		var total int64
		for _, line := range lines {
			if line.QuantityUnits > math.MaxInt64-total {
				return errors.New("invalid freight-batch line packing")
			}
			total += line.QuantityUnits
		}
		if len(lines) == 0 || total != consignment.RequestedUnits {
			return errors.New("freight-batch consignment quantity mismatch")
		}
	}
	for code, plan := range plans {
		if planCounts[code] != plan.ConsignmentCount {
			return errors.New("freight-batch plan count mismatch")
		}
		var total int64
		for _, consignment := range state.Consignments {
			if consignment.PlanCode == code {
				if consignment.RequestedUnits > math.MaxInt64-total {
					return errors.New("freight-batch plan quantity overflow")
				}
				total += consignment.RequestedUnits
			}
		}
		if total != plan.RequiredUnits || cityOpenWorldFreightBatchDerivedPlanState(state.Consignments, code) != plan.State {
			return errors.New("freight-batch plan state mismatch")
		}
	}
	facts := make(map[string]CityOpenWorldFreightBatchFact, len(state.Facts))
	for _, fact := range state.Facts {
		if _, exists := consignments[fact.ConsignmentCode]; !exists || fact.Tick <= 0 || fact.Sequence <= 0 ||
			!cityOpenWorldFreightBatchFactTypeValid(fact.FactType) || !cityOpenWorldFreightBatchEvidenceKindValid(fact.EvidenceKind) ||
			!cityOpenWorldFreightBatchJSONObject(fact.Payload) {
			return errors.New("invalid freight-batch fact")
		}
		if (fact.FactType == "receipt.confirmed" || fact.FactType == "settlement.confirmed") !=
			(fact.EvidenceKind == cityOpenWorldFreightBatchEvidenceSupplyChain) {
			return errors.New("freight-batch fact evidence is inconsistent")
		}
		if fact.Tick < plans[consignments[fact.ConsignmentCode].PlanCode].SourceTick {
			return errors.New("freight-batch fact predates its plan")
		}
		key := cityOpenWorldFreightBatchFactKey(fact.ConsignmentCode, CityOpenWorldFreightBatchFactRef{EvidenceKind: fact.EvidenceKind, Tick: fact.Tick, Sequence: fact.Sequence})
		if _, exists := facts[key]; exists {
			return errors.New("duplicate freight-batch fact")
		}
		facts[key] = fact
	}
	for code, consignment := range consignments {
		root, exists := facts[cityOpenWorldFreightBatchFactKey(code, consignment.SourceFact)]
		if !exists || root.FactType != "consignment.created" || root.EvidenceKind != cityOpenWorldFreightBatchEvidenceRuntime {
			return errors.New("freight-batch consignment root fact is inconsistent")
		}
		last, exists := facts[cityOpenWorldFreightBatchFactKey(code, consignment.LastFact)]
		if !exists || last.EvidenceKind != cityOpenWorldFreightBatchEvidenceRuntime ||
			(consignment.State == cityOpenWorldFreightBatchConsignmentStateSettled &&
				!cityOpenWorldFreightBatchSettlementRuntimeFactTypeValid(last.FactType)) ||
			(consignment.State != cityOpenWorldFreightBatchConsignmentStateSettled &&
				last.FactType != cityOpenWorldFreightBatchLastRuntimeFactType(consignment.State)) {
			return errors.New("freight-batch consignment last fact is inconsistent")
		}
	}

	transitionsByConsignment := make(map[string][]CityOpenWorldFreightBatchTransition, len(consignments))
	lastStates := make(map[string]string, len(consignments))
	lastFacts := make(map[string]CityOpenWorldFreightBatchFactRef, len(consignments))
	transitionKeys := make(map[string]struct{}, len(state.Transitions))
	for _, transition := range state.Transitions {
		if _, exists := consignments[transition.ConsignmentCode]; !exists || transition.TransitionTick <= 0 || transition.TransitionSequence <= 0 ||
			!cityOpenWorldFreightBatchConsignmentStateValid(transition.State) || transition.ReasonCode == "" ||
			!cityOpenWorldFreightBatchJSONObject(transition.Metadata) {
			return errors.New("invalid freight-batch transition")
		}
		fact, exists := facts[cityOpenWorldFreightBatchFactKey(transition.ConsignmentCode, transition.SourceFact)]
		if !exists || fact.Tick != transition.TransitionTick || fact.Sequence != transition.TransitionSequence ||
			!cityOpenWorldFreightBatchTransitionMatchesFact(transition.State, fact.FactType) ||
			!cityOpenWorldFreightBatchReasonMatchesState(transition.State, transition.ReasonCode) ||
			!cityOpenWorldFreightBatchTransitionAllowed(lastStates[transition.ConsignmentCode], transition.State) {
			return errors.New("freight-batch transition proof mismatch")
		}
		key := fmt.Sprintf("%s\x00%d\x00%d", transition.ConsignmentCode, transition.TransitionTick, transition.TransitionSequence)
		if _, exists := transitionKeys[key]; exists {
			return errors.New("duplicate freight-batch transition")
		}
		transitionKeys[key] = struct{}{}
		lastStates[transition.ConsignmentCode] = transition.State
		lastFacts[transition.ConsignmentCode] = transition.SourceFact
		transitionsByConsignment[transition.ConsignmentCode] = append(transitionsByConsignment[transition.ConsignmentCode], transition)
	}
	receipts := make(map[string]CityOpenWorldFreightBatchReceipt, len(state.Receipts))
	for _, receipt := range state.Receipts {
		consignment, exists := consignments[receipt.ConsignmentCode]
		if !exists || receipt.PlanCode != consignment.PlanCode || receipt.ReceivedTick <= 0 ||
			receipt.DeliveryFact.Tick <= 0 || receipt.DeliveryFact.Sequence <= 0 || receipt.ResourceOperation.Tick <= 0 ||
			receipt.ResourceOperation.Sequence <= 0 || receipt.SourceFact.EvidenceKind != cityOpenWorldFreightBatchEvidenceSupplyChain ||
			!cityOpenWorldFreightBatchJSONObject(receipt.Metadata) {
			return errors.New("invalid freight-batch receipt")
		}
		plan := plans[receipt.PlanCode]
		if receipt.OrderCode != plan.OrderCode {
			return errors.New("freight-batch receipt order mismatch")
		}
		fact, exists := facts[cityOpenWorldFreightBatchFactKey(receipt.ConsignmentCode, receipt.SourceFact)]
		if !exists || fact.FactType != "receipt.confirmed" || fact.Tick != receipt.DeliveryFact.Tick || fact.Sequence != receipt.DeliveryFact.Sequence ||
			receipt.ReceivedTick != receipt.DeliveryFact.Tick || receipt.ResourceOperation.Tick != receipt.DeliveryFact.Tick ||
			consignment.State != cityOpenWorldFreightBatchConsignmentStateReceived {
			return errors.New("freight-batch receipt proof mismatch")
		}
		if _, duplicate := receipts[receipt.ConsignmentCode]; duplicate {
			return errors.New("duplicate freight-batch receipt")
		}
		receipts[receipt.ConsignmentCode] = receipt
	}
	receiptCounts := make(map[string]int, len(plans))
	type receiptProof struct {
		tick, deliverySequence, operationTick, operationSequence int64
	}
	planProofs := make(map[string]receiptProof, len(plans))
	for _, receipt := range receipts {
		receiptCounts[receipt.PlanCode]++
		proof := receiptProof{tick: receipt.ReceivedTick, deliverySequence: receipt.DeliveryFact.Sequence, operationTick: receipt.ResourceOperation.Tick, operationSequence: receipt.ResourceOperation.Sequence}
		if existing, found := planProofs[receipt.PlanCode]; found && existing != proof {
			return errors.New("freight-batch plan receipts are not atomic")
		}
		planProofs[receipt.PlanCode] = proof
	}
	for code, consignment := range consignments {
		transitions := transitionsByConsignment[code]
		if len(transitions) == 0 || transitions[len(transitions)-1].State != consignment.State ||
			consignment.Version != int64(len(transitions)) {
			return errors.New("freight-batch consignment transition mismatch")
		}
		if consignment.State == cityOpenWorldFreightBatchConsignmentStateReceived || consignment.State == cityOpenWorldFreightBatchConsignmentStateSettled {
			if len(transitions) < 2 || transitions[len(transitions)-2].SourceFact != consignment.LastFact {
				return errors.New("freight-batch receipt runtime boundary mismatch")
			}
		} else if lastFacts[code] != consignment.LastFact {
			return errors.New("freight-batch consignment last transition mismatch")
		}
		_, hasReceipt := receipts[code]
		if (consignment.State == cityOpenWorldFreightBatchConsignmentStateReceived) != hasReceipt {
			return errors.New("freight-batch receipt terminal mismatch")
		}
	}
	for code, plan := range plans {
		if receiptCounts[code] > 0 && receiptCounts[code] != plan.ConsignmentCount {
			return errors.New("freight-batch plan receipt count mismatch")
		}
		if plan.State == cityOpenWorldFreightBatchPlanStateReceived {
			if receiptCounts[code] != plan.ConsignmentCount {
				return errors.New("freight-batch received plan lacks receipts")
			}
		} else if receiptCounts[code] != 0 {
			return errors.New("freight-batch nonterminal plan has receipts")
		}
		planLastFactValid := plan.LastFact == plan.SourceFact
		for _, consignment := range state.Consignments {
			if consignment.PlanCode == code && consignment.LastFact == plan.LastFact {
				planLastFactValid = true
				break
			}
		}
		if !planLastFactValid {
			return errors.New("freight-batch plan last fact is inconsistent")
		}
	}
	if policy.PlanCount != int64(len(plans)) || policy.ConsignmentCount != int64(len(consignments)) ||
		policy.AwaitingRouteCount != stateCounts[cityOpenWorldFreightBatchConsignmentStateAwaitingRoute] ||
		policy.InTransitCount != stateCounts[cityOpenWorldFreightBatchConsignmentStateInTransit] ||
		policy.AwaitingReceiptCount != stateCounts[cityOpenWorldFreightBatchConsignmentStateAwaitingReceipt] ||
		policy.ReceivedCount != stateCounts[cityOpenWorldFreightBatchConsignmentStateReceived] ||
		policy.SettledCount != stateCounts[cityOpenWorldFreightBatchConsignmentStateSettled] ||
		policy.ExpiredCount != stateCounts[cityOpenWorldFreightBatchConsignmentStateExpired] ||
		policy.VoidedCount != stateCounts[cityOpenWorldFreightBatchConsignmentStateVoided] ||
		policy.OrphanedCount != stateCounts[cityOpenWorldFreightBatchConsignmentStateOrphaned] ||
		policy.FactCount != int64(len(facts)) || policy.TransitionCount != int64(len(state.Transitions)) ||
		policy.ReceiptCount != int64(len(receipts)) {
		return errors.New("freight-batch policy counters are inconsistent")
	}
	return nil
}

func cityOpenWorldFreightBatchLastRuntimeFactType(state string) string {
	switch state {
	case cityOpenWorldFreightBatchConsignmentStateAwaitingRoute:
		return "demand.requested"
	case cityOpenWorldFreightBatchConsignmentStateInTransit:
		return "route.scheduled"
	case cityOpenWorldFreightBatchConsignmentStateAwaitingReceipt, cityOpenWorldFreightBatchConsignmentStateReceived, cityOpenWorldFreightBatchConsignmentStateSettled:
		return "route.completed"
	case cityOpenWorldFreightBatchConsignmentStateExpired:
		return "demand.expired"
	case cityOpenWorldFreightBatchConsignmentStateVoided:
		return "demand.voided"
	case cityOpenWorldFreightBatchConsignmentStateOrphaned:
		return "transport.orphaned"
	default:
		return ""
	}
}

func cityOpenWorldFreightBatchPackLines(lines []cityOpenWorldFreightBatchSourceLine) ([]cityOpenWorldFreightBatchPackedConsignment, error) {
	if len(lines) == 0 {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v18_freight_batch_lines"})
	}
	items := append([]cityOpenWorldFreightBatchSourceLine(nil), lines...)
	sort.Slice(items, func(i, j int) bool { return items[i].LineNo < items[j].LineNo })
	packed := make([]cityOpenWorldFreightBatchPackedConsignment, 0)
	current := cityOpenWorldFreightBatchPackedConsignment{BatchNo: 1, Lines: make([]cityOpenWorldFreightBatchPackedLine, 0)}
	seenLineNos := make(map[int]struct{}, len(items))
	for _, source := range items {
		if source.LineNo < 1 || source.ResourceCode == "" || source.QuantityUnits <= 0 || source.UnitPriceUnits <= 0 ||
			source.QuantityUnits > math.MaxInt64/source.UnitPriceUnits || source.TotalPriceUnits != source.QuantityUnits*source.UnitPriceUnits {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v18_freight_batch_line"})
		}
		if _, exists := seenLineNos[source.LineNo]; exists {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v18_freight_batch_line"})
		}
		seenLineNos[source.LineNo] = struct{}{}
		remaining := source.QuantityUnits
		for remaining > 0 {
			if current.RequestedUnits == cityOpenWorldFreightBatchMaximumUnits {
				packed = append(packed, current)
				current = cityOpenWorldFreightBatchPackedConsignment{BatchNo: len(packed) + 1, Lines: make([]cityOpenWorldFreightBatchPackedLine, 0)}
			}
			available := cityOpenWorldFreightBatchMaximumUnits - current.RequestedUnits
			quantity := remaining
			if quantity > available {
				quantity = available
			}
			if quantity <= 0 || quantity > math.MaxInt64/source.UnitPriceUnits {
				return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v18_freight_batch_packing"})
			}
			current.Lines = append(current.Lines, cityOpenWorldFreightBatchPackedLine{
				SourceLineNo: source.LineNo, ResourceCode: source.ResourceCode, QuantityUnits: quantity,
				UnitPriceUnits: source.UnitPriceUnits, TotalPriceUnits: quantity * source.UnitPriceUnits,
			})
			current.RequestedUnits += quantity
			remaining -= quantity
		}
	}
	if current.RequestedUnits > 0 {
		packed = append(packed, current)
	}
	if len(packed) < 2 || len(packed) > cityOpenWorldFreightBatchMaximumConsignmentsPerPlan {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v18_freight_batch_count"})
	}
	return packed, nil
}

func cityOpenWorldFreightBatchPlanStateValid(value string) bool {
	switch value {
	case cityOpenWorldFreightBatchPlanStateActive, cityOpenWorldFreightBatchPlanStateReady,
		cityOpenWorldFreightBatchPlanStateReceived, cityOpenWorldFreightBatchPlanStateSettled, cityOpenWorldFreightBatchPlanStateBlocked:
		return true
	default:
		return false
	}
}

func cityOpenWorldFreightBatchConsignmentStateValid(value string) bool {
	switch value {
	case cityOpenWorldFreightBatchConsignmentStateAwaitingRoute, cityOpenWorldFreightBatchConsignmentStateInTransit,
		cityOpenWorldFreightBatchConsignmentStateAwaitingReceipt, cityOpenWorldFreightBatchConsignmentStateReceived, cityOpenWorldFreightBatchConsignmentStateSettled,
		cityOpenWorldFreightBatchConsignmentStateExpired, cityOpenWorldFreightBatchConsignmentStateVoided,
		cityOpenWorldFreightBatchConsignmentStateOrphaned:
		return true
	default:
		return false
	}
}

func cityOpenWorldFreightBatchEvidenceKindValid(value string) bool {
	return value == cityOpenWorldFreightBatchEvidenceRuntime || value == cityOpenWorldFreightBatchEvidenceSupplyChain
}

func cityOpenWorldFreightBatchFactTypeValid(value string) bool {
	switch value {
	case "consignment.created", "demand.requested", "route.scheduled", "route.completed",
		"demand.expired", "demand.voided", "transport.orphaned", "receipt.confirmed", "settlement.confirmed":
		return true
	default:
		return false
	}
}

func cityOpenWorldFreightBatchTransitionMatchesFact(state, factType string) bool {
	switch state {
	case cityOpenWorldFreightBatchConsignmentStateAwaitingRoute:
		return factType == "demand.requested"
	case cityOpenWorldFreightBatchConsignmentStateInTransit:
		return factType == "route.scheduled"
	case cityOpenWorldFreightBatchConsignmentStateAwaitingReceipt:
		return factType == "route.completed"
	case cityOpenWorldFreightBatchConsignmentStateReceived:
		return factType == "receipt.confirmed"
	case cityOpenWorldFreightBatchConsignmentStateSettled:
		return factType == "settlement.confirmed"
	case cityOpenWorldFreightBatchConsignmentStateExpired:
		return factType == "demand.expired"
	case cityOpenWorldFreightBatchConsignmentStateVoided:
		return factType == "demand.voided"
	case cityOpenWorldFreightBatchConsignmentStateOrphaned:
		return factType == "transport.orphaned"
	default:
		return false
	}
}

func cityOpenWorldFreightBatchReasonMatchesState(state, reason string) bool {
	switch state {
	case cityOpenWorldFreightBatchConsignmentStateAwaitingRoute:
		return reason == cityOpenWorldFreightBatchReasonDispatched
	case cityOpenWorldFreightBatchConsignmentStateInTransit:
		return reason == cityOpenWorldFreightBatchReasonScheduled
	case cityOpenWorldFreightBatchConsignmentStateAwaitingReceipt:
		return reason == cityOpenWorldFreightBatchReasonCompleted
	case cityOpenWorldFreightBatchConsignmentStateReceived:
		return reason == cityOpenWorldFreightBatchReasonReceived
	case cityOpenWorldFreightBatchConsignmentStateSettled:
		return reason == cityOpenWorldFreightBatchReasonSettled
	case cityOpenWorldFreightBatchConsignmentStateExpired:
		return reason == cityOpenWorldFreightBatchReasonExpired
	case cityOpenWorldFreightBatchConsignmentStateVoided:
		return reason == cityOpenWorldFreightBatchReasonVoided
	case cityOpenWorldFreightBatchConsignmentStateOrphaned:
		return reason == cityOpenWorldFreightBatchReasonOrphaned
	default:
		return false
	}
}

func cityOpenWorldFreightBatchTransitionAllowed(previous, next string) bool {
	switch previous {
	case "":
		return next == cityOpenWorldFreightBatchConsignmentStateAwaitingRoute
	case cityOpenWorldFreightBatchConsignmentStateAwaitingRoute:
		return next == cityOpenWorldFreightBatchConsignmentStateInTransit || next == cityOpenWorldFreightBatchConsignmentStateExpired || next == cityOpenWorldFreightBatchConsignmentStateVoided
	case cityOpenWorldFreightBatchConsignmentStateInTransit:
		return next == cityOpenWorldFreightBatchConsignmentStateAwaitingReceipt || next == cityOpenWorldFreightBatchConsignmentStateOrphaned
	case cityOpenWorldFreightBatchConsignmentStateAwaitingReceipt:
		return next == cityOpenWorldFreightBatchConsignmentStateReceived || next == cityOpenWorldFreightBatchConsignmentStateSettled ||
			next == cityOpenWorldFreightBatchConsignmentStateOrphaned
	case cityOpenWorldFreightBatchConsignmentStateExpired,
		cityOpenWorldFreightBatchConsignmentStateVoided,
		cityOpenWorldFreightBatchConsignmentStateOrphaned:
		return next == cityOpenWorldFreightBatchConsignmentStateSettled
	default:
		return false
	}
}

func cityOpenWorldFreightBatchFactKey(consignmentCode string, reference CityOpenWorldFreightBatchFactRef) string {
	return consignmentCode + "\x00" + reference.EvidenceKind + "\x00" + fmt.Sprintf("%d\x00%d", reference.Tick, reference.Sequence)
}

// V22 settlement is deliberately proved by a supply-chain fact, while V18's
// durable last_fact column is a runtime-fact foreign key.  A settled V18
// consignment therefore retains the final transport fact that put the cargo
// into a resolvable state; its final transition carries the external
// settlement.confirmed proof.  Keeping those two clocks separate prevents a
// later settlement from masquerading as a mobility event.
func cityOpenWorldFreightBatchSettlementRuntimeFactTypeValid(factType string) bool {
	switch factType {
	case "route.completed", "demand.expired", "demand.voided", "transport.orphaned":
		return true
	default:
		return false
	}
}

func cityOpenWorldFreightBatchJSONObject(raw json.RawMessage) bool {
	var value any
	return len(raw) > 0 && json.Unmarshal(raw, &value) == nil && func() bool { _, ok := value.(map[string]any); return ok }()
}

func cityOpenWorldFreightBatchPolicyMetadataValid(raw json.RawMessage) bool {
	var metadata struct {
		SchemaVersion int    `json:"schema_version"`
		Scope         string `json:"scope"`
		Inventory     string `json:"inventory"`
		Legacy        string `json:"legacy"`
	}
	return json.Unmarshal(raw, &metadata) == nil && metadata.SchemaVersion == cityOpenWorldFreightBatchSchemaVersion &&
		metadata.Scope == "v16_suppressed_overflow_to_v9_multi_consignment" && metadata.Inventory == "v15_atomic_delivery_only" &&
		metadata.Legacy == "pre_v18_overflow_sources_untracked"
}

func cityOpenWorldFreightBatchDerivedPlanState(consignments []CityOpenWorldFreightBatchConsignment, planCode string) string {
	if len(consignments) == 0 {
		return cityOpenWorldFreightBatchPlanStateBlocked
	}
	matching := make([]CityOpenWorldFreightBatchConsignment, 0)
	for _, consignment := range consignments {
		if consignment.PlanCode == planCode {
			matching = append(matching, consignment)
		}
	}
	if len(matching) == 0 {
		return cityOpenWorldFreightBatchPlanStateBlocked
	}
	all := func(state string) bool {
		for _, consignment := range matching {
			if consignment.State != state {
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
	for _, consignment := range matching {
		if consignment.State == cityOpenWorldFreightBatchConsignmentStateExpired || consignment.State == cityOpenWorldFreightBatchConsignmentStateVoided || consignment.State == cityOpenWorldFreightBatchConsignmentStateOrphaned {
			return cityOpenWorldFreightBatchPlanStateBlocked
		}
	}
	return cityOpenWorldFreightBatchPlanStateActive
}
