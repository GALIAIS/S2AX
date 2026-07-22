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
	cityOpenWorldEnterpriseFreightSchemaVersion          = 1
	cityOpenWorldEnterpriseFreightProfileID              = "sub2api-open-world-enterprise-freight"
	cityOpenWorldEnterpriseFreightProfileVersion         = "1.0.0"
	cityOpenWorldEnterpriseFreightSourceContract         = "v15_dispatched_fact_snapshot_v1"
	cityOpenWorldEnterpriseFreightDemandContract         = "v9_system_carrier_demand_v1"
	cityOpenWorldEnterpriseFreightCompletionContract     = "v9_transport_observation_no_receipt_v1"
	cityOpenWorldEnterpriseFreightTerminalContract       = "v15_terminal_pending_demand_void_v1"
	cityOpenWorldEnterpriseFreightCarrierActorCode       = "system.freight.carrier"
	cityOpenWorldEnterpriseFreightCarrierActorType       = "system.freight_carrier"
	cityOpenWorldEnterpriseFreightModeCode               = "freight"
	cityOpenWorldEnterpriseFreightPurposeCode            = "enterprise.freight"
	cityOpenWorldEnterpriseFreightMaximumSources         = 10000
	cityOpenWorldEnterpriseFreightMaximumGenerationsTick = 128
	// V9 validates a request up to 1,000 units, but the frozen V9 freight
	// graph's narrowest local edge carries 32 cargo units per tick.  This first
	// adapter deliberately creates one atomic demand per dispatched order, so
	// it must use the network-safe limit rather than accepting a request that
	// can never be scheduled.  Splitting, convoying and in-transit inventory
	// belong to a later explicit logistics version.
	cityOpenWorldEnterpriseFreightMaximumRequestedUnits = int64(32)

	cityOpenWorldEnterpriseFreightStateDemandPending      = "demand_pending"
	cityOpenWorldEnterpriseFreightStateRouteScheduled     = "route_scheduled"
	cityOpenWorldEnterpriseFreightStateRouteCompleted     = "route_completed"
	cityOpenWorldEnterpriseFreightStateDemandExpired      = "demand_expired"
	cityOpenWorldEnterpriseFreightStateVoided             = "voided"
	cityOpenWorldEnterpriseFreightStateTransportOrphaned  = "transport_orphaned"
	cityOpenWorldEnterpriseFreightStateSuppressed         = "suppressed"
	cityOpenWorldEnterpriseFreightReasonDispatched        = "v15_dispatched"
	cityOpenWorldEnterpriseFreightReasonScheduled         = "v9_route_scheduled"
	cityOpenWorldEnterpriseFreightReasonCompleted         = "v9_route_completed"
	cityOpenWorldEnterpriseFreightReasonExpired           = "v9_demand_expired"
	cityOpenWorldEnterpriseFreightReasonTerminalPending   = "v15_terminal_pending_demand"
	cityOpenWorldEnterpriseFreightReasonTerminalInTransit = "v15_terminal_after_route_scheduled"
	cityOpenWorldEnterpriseFreightReasonUnitsExceeded     = "requested_units_exceed_v9_limit"

	cityOpenWorldRuntimeFactEnterpriseFreightSourceCreated     = "system.enterprise_freight.source.created"
	cityOpenWorldRuntimeFactEnterpriseFreightSourceSuppressed  = "system.enterprise_freight.source.suppressed"
	cityOpenWorldRuntimeFactEnterpriseFreightRouteScheduled    = "system.enterprise_freight.route.scheduled"
	cityOpenWorldRuntimeFactEnterpriseFreightRouteCompleted    = "system.enterprise_freight.route.completed"
	cityOpenWorldRuntimeFactEnterpriseFreightDemandExpired     = "system.enterprise_freight.demand.expired"
	cityOpenWorldRuntimeFactEnterpriseFreightTransportOrphaned = "system.enterprise_freight.transport.orphaned"
)

// CityOpenWorldEnterpriseFreightPolicy pins the narrow dispatch-to-demand
// adapter. It does not claim to own vehicles, in-transit inventory, receipts,
// or enterprise settlement; those stay in later explicitly-versioned layers.
type CityOpenWorldEnterpriseFreightPolicy struct {
	ProfileID                 string          `json:"profile_id"`
	ProfileVersion            string          `json:"profile_version"`
	ContentHash               string          `json:"content_hash"`
	BaselineTick              int64           `json:"baseline_tick"`
	SourceContract            string          `json:"source_contract"`
	DemandContract            string          `json:"demand_contract"`
	CompletionContract        string          `json:"completion_contract"`
	TerminalContract          string          `json:"terminal_contract"`
	CarrierActorCode          string          `json:"carrier_actor_code"`
	MaximumSources            int             `json:"maximum_sources"`
	MaximumGenerationsPerTick int             `json:"maximum_generations_per_tick"`
	SourceCount               int64           `json:"source_count"`
	PendingCount              int64           `json:"pending_count"`
	DemandCount               int64           `json:"demand_count"`
	ScheduledCount            int64           `json:"scheduled_count"`
	CompletedCount            int64           `json:"completed_count"`
	ExpiredCount              int64           `json:"expired_count"`
	VoidedCount               int64           `json:"voided_count"`
	OrphanedCount             int64           `json:"orphaned_count"`
	SuppressedCount           int64           `json:"suppressed_count"`
	FactCount                 int64           `json:"fact_count"`
	TransitionCount           int64           `json:"transition_count"`
	Revision                  int64           `json:"revision"`
	Metadata                  json.RawMessage `json:"metadata"`
}

// CityOpenWorldEnterpriseFreightSource is the immutable contract-to-network
// mapping. DispatchFact points at the V15 supply-chain fact namespace; the
// SourceFact and LastFact point at generic open-world runtime facts.
type CityOpenWorldEnterpriseFreightSource struct {
	Code                 string                      `json:"code"`
	OrderCode            string                      `json:"order_code"`
	SellerNodeCode       string                      `json:"seller_node_code"`
	BuyerNodeCode        string                      `json:"buyer_node_code"`
	SourceHubCode        string                      `json:"source_hub_code"`
	DestinationHubCode   string                      `json:"destination_hub_code"`
	CarrierActorCode     string                      `json:"carrier_actor_code"`
	DispatchFact         CityOpenWorldRuntimeFactRef `json:"dispatch_fact"`
	DispatchTick         int64                       `json:"dispatch_tick"`
	SourceTick           int64                       `json:"source_tick"`
	MobilityDeadlineTick int64                       `json:"mobility_deadline_tick"`
	RequestedUnits       int64                       `json:"requested_units"`
	State                string                      `json:"state"`
	DemandCode           *string                     `json:"demand_code,omitempty"`
	RouteCode            *string                     `json:"route_code,omitempty"`
	SourceFact           CityOpenWorldRuntimeFactRef `json:"source_fact"`
	LastFact             CityOpenWorldRuntimeFactRef `json:"last_fact"`
	Version              int64                       `json:"version"`
	Metadata             json.RawMessage             `json:"metadata"`
}

// CityOpenWorldEnterpriseFreightSourceLine is a copied V15 order-line
// snapshot. It is deliberately not another inventory balance and carries no
// mutable amount.
type CityOpenWorldEnterpriseFreightSourceLine struct {
	SourceCode              string          `json:"source_code"`
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

type CityOpenWorldEnterpriseFreightFact struct {
	Tick        int64                       `json:"tick"`
	Sequence    int64                       `json:"sequence"`
	SourceCode  string                      `json:"source_code"`
	FactType    string                      `json:"fact_type"`
	RuntimeFact CityOpenWorldRuntimeFactRef `json:"runtime_fact"`
	Payload     json.RawMessage             `json:"payload"`
}

type CityOpenWorldEnterpriseFreightTransition struct {
	SourceCode         string                      `json:"source_code"`
	TransitionTick     int64                       `json:"transition_tick"`
	TransitionSequence int64                       `json:"transition_sequence"`
	State              string                      `json:"state"`
	ReasonCode         string                      `json:"reason_code"`
	SourceFact         CityOpenWorldRuntimeFactRef `json:"source_fact"`
	Metadata           json.RawMessage             `json:"metadata"`
}

// CityOpenWorldEnterpriseFreightState is a V16 sibling of V9 mobility and
// V15 supply-chain state. It contains linkage evidence only; it must never
// become a parallel transportation or inventory truth source.
type CityOpenWorldEnterpriseFreightState struct {
	Policy      CityOpenWorldEnterpriseFreightPolicy       `json:"policy"`
	Sources     []CityOpenWorldEnterpriseFreightSource     `json:"sources"`
	Lines       []CityOpenWorldEnterpriseFreightSourceLine `json:"lines"`
	Facts       []CityOpenWorldEnterpriseFreightFact       `json:"facts"`
	Transitions []CityOpenWorldEnterpriseFreightTransition `json:"transitions"`
}

func cityOpenWorldEnterpriseFreightPolicyHash() (string, error) {
	raw, err := json.Marshal(struct {
		SchemaVersion             int    `json:"schema_version"`
		ProfileID                 string `json:"profile_id"`
		ProfileVersion            string `json:"profile_version"`
		SourceContract            string `json:"source_contract"`
		DemandContract            string `json:"demand_contract"`
		CompletionContract        string `json:"completion_contract"`
		TerminalContract          string `json:"terminal_contract"`
		CarrierActorCode          string `json:"carrier_actor_code"`
		MaximumSources            int    `json:"maximum_sources"`
		MaximumGenerationsPerTick int    `json:"maximum_generations_per_tick"`
		MaximumRequestedUnits     int64  `json:"maximum_requested_units"`
		ModeCode                  string `json:"mode_code"`
		PurposeCode               string `json:"purpose_code"`
	}{
		SchemaVersion:             cityOpenWorldEnterpriseFreightSchemaVersion,
		ProfileID:                 cityOpenWorldEnterpriseFreightProfileID,
		ProfileVersion:            cityOpenWorldEnterpriseFreightProfileVersion,
		SourceContract:            cityOpenWorldEnterpriseFreightSourceContract,
		DemandContract:            cityOpenWorldEnterpriseFreightDemandContract,
		CompletionContract:        cityOpenWorldEnterpriseFreightCompletionContract,
		TerminalContract:          cityOpenWorldEnterpriseFreightTerminalContract,
		CarrierActorCode:          cityOpenWorldEnterpriseFreightCarrierActorCode,
		MaximumSources:            cityOpenWorldEnterpriseFreightMaximumSources,
		MaximumGenerationsPerTick: cityOpenWorldEnterpriseFreightMaximumGenerationsTick,
		MaximumRequestedUnits:     cityOpenWorldEnterpriseFreightMaximumRequestedUnits,
		ModeCode:                  cityOpenWorldEnterpriseFreightModeCode,
		PurposeCode:               cityOpenWorldEnterpriseFreightPurposeCode,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func cityOpenWorldEnterpriseFreightSourceCode(orderCode string) string {
	sum := sha256.Sum256([]byte("enterprise.freight.source.v1\x00" + orderCode))
	return "enterprise.freight.source." + hex.EncodeToString(sum[:20])
}

func cityOpenWorldEnterpriseFreightDemandCode(sourceCode string) string {
	sum := sha256.Sum256([]byte("enterprise.freight.demand.v1\x00" + sourceCode))
	return "mobility.demand.freight." + hex.EncodeToString(sum[:20])
}

func activateCityOpenWorldEnterpriseFreightBootstrapWrite(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if _, err := tx.ExecContext(ctx, `
SELECT set_config('sub2api.city_open_world_enterprise_freight_bootstrap_world_id', $1, TRUE)`, fmt.Sprintf("%d", worldID)); err != nil {
		return fmt.Errorf("enable open-world V16 enterprise-freight bootstrap: %w", err)
	}
	return nil
}

func activateCityOpenWorldEnterpriseFreightWrite(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if _, err := tx.ExecContext(ctx, `
SELECT set_config('sub2api.city_open_world_enterprise_freight_world_id', $1, TRUE)`, fmt.Sprintf("%d", worldID)); err != nil {
		return fmt.Errorf("enable open-world V16 enterprise-freight write: %w", err)
	}
	return nil
}

func assertCityOpenWorldEnterpriseFreightFoundation(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if _, err := tx.ExecContext(ctx, `SELECT assert_city_open_world_enterprise_freight_foundation($1)`, worldID); err != nil {
		return fmt.Errorf("validate V16 enterprise-freight foundation: %w", err)
	}
	return nil
}

// initializeCityOpenWorldV16EnterpriseFreightFoundation creates only the
// narrow adapter profile and its schema-carrier actor. Existing V15 orders,
// V9 routes, V10 arrival rows, inventories and journals are never rewritten.
func initializeCityOpenWorldV16EnterpriseFreightFoundation(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if worldID <= 0 {
		return ErrCityInvalidInput
	}
	var simulationVersion string
	var baselineTick int64
	if err := tx.QueryRowContext(ctx, `
SELECT simulation_version, current_tick
FROM city_worlds WHERE id = $1 FOR UPDATE`, worldID).Scan(&simulationVersion, &baselineTick); err != nil {
		return fmt.Errorf("load V16 enterprise-freight world: %w", err)
	}
	if !cityEngineSupportsOpenWorldEnterpriseFreight(simulationVersion) {
		return ErrCitySimulationVersion.WithMetadata(map[string]string{"version": simulationVersion})
	}
	if _, err := tx.ExecContext(ctx, `SELECT assert_city_open_world_supply_chain_foundation($1)`, worldID); err != nil {
		return fmt.Errorf("validate V16 enterprise-freight supply-chain prerequisite: %w", err)
	}
	if err := activateCityOpenWorldEnterpriseFreightBootstrapWrite(ctx, tx, worldID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
SELECT set_config('sub2api.city_open_world_runtime_bootstrap_world_id', $1, TRUE)`, fmt.Sprintf("%d", worldID)); err != nil {
		return fmt.Errorf("enable V16 enterprise-freight carrier bootstrap: %w", err)
	}
	if err := ensureCityOpenWorldEnterpriseFreightCarrier(ctx, tx, worldID, baselineTick); err != nil {
		return err
	}
	policyHash, err := cityOpenWorldEnterpriseFreightPolicyHash()
	if err != nil {
		return fmt.Errorf("hash V16 enterprise-freight policy: %w", err)
	}
	metadata, err := json.Marshal(map[string]any{
		"schema_version":          cityOpenWorldEnterpriseFreightSchemaVersion,
		"scope":                   "dispatch_to_v9_freight_demand_only",
		"receipt":                 "not_implemented",
		"maximum_requested_units": cityOpenWorldEnterpriseFreightMaximumRequestedUnits,
		"mode_code":               cityOpenWorldEnterpriseFreightModeCode,
		"purpose_code":            cityOpenWorldEnterpriseFreightPurposeCode,
	})
	if err != nil {
		return fmt.Errorf("marshal V16 enterprise-freight profile metadata: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_enterprise_freight_profiles
    (world_id, profile_id, profile_version, content_hash, baseline_tick,
     source_contract, demand_contract, completion_contract, terminal_contract,
     carrier_actor_code, maximum_sources, maximum_generations_per_tick,
     source_count, pending_count, demand_count, scheduled_count, completed_count,
     expired_count, voided_count, orphaned_count, suppressed_count, fact_count,
     transition_count, revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12,
        0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, $13::jsonb)`,
		worldID, cityOpenWorldEnterpriseFreightProfileID, cityOpenWorldEnterpriseFreightProfileVersion,
		policyHash, baselineTick, cityOpenWorldEnterpriseFreightSourceContract,
		cityOpenWorldEnterpriseFreightDemandContract, cityOpenWorldEnterpriseFreightCompletionContract,
		cityOpenWorldEnterpriseFreightTerminalContract, cityOpenWorldEnterpriseFreightCarrierActorCode,
		cityOpenWorldEnterpriseFreightMaximumSources, cityOpenWorldEnterpriseFreightMaximumGenerationsTick,
		[]byte(metadata)); err != nil {
		return fmt.Errorf("insert V16 enterprise-freight profile: %w", err)
	}
	return assertCityOpenWorldEnterpriseFreightFoundation(ctx, tx, worldID)
}

func ensureCityOpenWorldEnterpriseFreightCarrier(ctx context.Context, tx *sql.Tx, worldID, baselineTick int64) error {
	var actorID int64
	var actorType, status string
	var ownerUserID sql.NullInt64
	err := tx.QueryRowContext(ctx, `
SELECT id, actor_type_code, status, owner_user_id
FROM city_open_world_actors
WHERE world_id = $1 AND code = $2
FOR UPDATE`, worldID, cityOpenWorldEnterpriseFreightCarrierActorCode).Scan(&actorID, &actorType, &status, &ownerUserID)
	if errors.Is(err, sql.ErrNoRows) {
		metadata, marshalErr := json.Marshal(map[string]any{
			"schema_version":  cityOpenWorldEnterpriseFreightSchemaVersion,
			"origin":          "v16_enterprise_freight_foundation",
			"coordinate_mode": "none",
		})
		if marshalErr != nil {
			return marshalErr
		}
		if err = tx.QueryRowContext(ctx, `
INSERT INTO city_open_world_actors
    (world_id, code, owner_user_id, actor_type_code, name, status,
     archetype_code, archetype_version, created_tick, updated_tick, version, metadata)
VALUES ($1, $2, NULL, $3, 'Enterprise Freight Carrier', 'active',
        NULL, NULL, $4, $4, 1, $5::jsonb)
RETURNING id`, worldID, cityOpenWorldEnterpriseFreightCarrierActorCode,
			cityOpenWorldEnterpriseFreightCarrierActorType, baselineTick, []byte(metadata)).Scan(&actorID); err != nil {
			return fmt.Errorf("insert V16 enterprise-freight carrier: %w", err)
		}
		if _, err = tx.ExecContext(ctx, `
UPDATE city_open_world_runtime_profiles
-- Runtime-profile revision is deliberately fact-backed
-- (revision = fact_count + 1).  A bootstrap-only system carrier changes the
-- actor projection but does not append a runtime fact, so advancing revision
-- here would violate the frozen profile contract.
SET actor_count = actor_count + 1, updated_at = NOW()
WHERE world_id = $1`, worldID); err != nil {
			return fmt.Errorf("update runtime profile for V16 carrier: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("load V16 enterprise-freight carrier: %w", err)
	}
	if actorID <= 0 || actorType != cityOpenWorldEnterpriseFreightCarrierActorType || status != "active" || ownerUserID.Valid {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v16_freight_carrier"})
	}
	return nil
}

func loadCityOpenWorldEnterpriseFreightState(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*CityOpenWorldEnterpriseFreightState, error) {
	if worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	state := &CityOpenWorldEnterpriseFreightState{
		Sources:     make([]CityOpenWorldEnterpriseFreightSource, 0),
		Lines:       make([]CityOpenWorldEnterpriseFreightSourceLine, 0),
		Facts:       make([]CityOpenWorldEnterpriseFreightFact, 0),
		Transitions: make([]CityOpenWorldEnterpriseFreightTransition, 0),
	}
	if err := queryer.QueryRowContext(ctx, `
SELECT profile_id, profile_version, content_hash, baseline_tick,
       source_contract, demand_contract, completion_contract, terminal_contract,
       carrier_actor_code, maximum_sources, maximum_generations_per_tick,
       source_count, pending_count, demand_count, scheduled_count, completed_count,
       expired_count, voided_count, orphaned_count, suppressed_count, fact_count,
       transition_count, revision, metadata
FROM city_open_world_enterprise_freight_profiles
WHERE world_id = $1`, worldID).Scan(
		&state.Policy.ProfileID, &state.Policy.ProfileVersion, &state.Policy.ContentHash,
		&state.Policy.BaselineTick, &state.Policy.SourceContract, &state.Policy.DemandContract,
		&state.Policy.CompletionContract, &state.Policy.TerminalContract,
		&state.Policy.CarrierActorCode, &state.Policy.MaximumSources,
		&state.Policy.MaximumGenerationsPerTick, &state.Policy.SourceCount,
		&state.Policy.PendingCount, &state.Policy.DemandCount, &state.Policy.ScheduledCount,
		&state.Policy.CompletedCount, &state.Policy.ExpiredCount, &state.Policy.VoidedCount,
		&state.Policy.OrphanedCount, &state.Policy.SuppressedCount, &state.Policy.FactCount,
		&state.Policy.TransitionCount, &state.Policy.Revision, &state.Policy.Metadata,
	); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v16_enterprise_freight_profile"})
	} else if err != nil {
		return nil, fmt.Errorf("load V16 enterprise-freight profile: %w", err)
	}

	sourceRows, err := queryer.QueryContext(ctx, `
SELECT source.code, source.order_code, source.seller_node_code, source.buyer_node_code,
       source.source_hub_code, source.destination_hub_code, carrier.code,
       dispatch_fact.tick, dispatch_fact.sequence, source.dispatch_tick, source.source_tick,
       source.mobility_deadline_tick, source.requested_units, source.state,
       demand.code, route.code, source_fact.tick, source_fact.sequence,
       last_fact.tick, last_fact.sequence, source.version, source.metadata
FROM city_open_world_enterprise_freight_sources source
JOIN city_open_world_actors carrier
  ON carrier.id = source.carrier_actor_id AND carrier.world_id = source.world_id
JOIN city_open_world_supply_chain_facts dispatch_fact
  ON dispatch_fact.id = source.dispatch_fact_id AND dispatch_fact.world_id = source.world_id
JOIN city_open_world_runtime_facts source_fact
  ON source_fact.id = source.source_runtime_fact_id AND source_fact.world_id = source.world_id
JOIN city_open_world_runtime_facts last_fact
  ON last_fact.id = source.last_runtime_fact_id AND last_fact.world_id = source.world_id
LEFT JOIN city_open_world_mobility_demands demand
  ON demand.id = source.mobility_demand_id AND demand.world_id = source.world_id
LEFT JOIN city_open_world_mobility_routes route
  ON route.id = source.mobility_route_id AND route.world_id = source.world_id
WHERE source.world_id = $1
ORDER BY source.code`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V16 enterprise-freight sources: %w", err)
	}
	for sourceRows.Next() {
		item := CityOpenWorldEnterpriseFreightSource{}
		var demandCode, routeCode sql.NullString
		if err = sourceRows.Scan(
			&item.Code, &item.OrderCode, &item.SellerNodeCode, &item.BuyerNodeCode,
			&item.SourceHubCode, &item.DestinationHubCode, &item.CarrierActorCode,
			&item.DispatchFact.Tick, &item.DispatchFact.Sequence, &item.DispatchTick, &item.SourceTick,
			&item.MobilityDeadlineTick, &item.RequestedUnits, &item.State, &demandCode, &routeCode,
			&item.SourceFact.Tick, &item.SourceFact.Sequence, &item.LastFact.Tick, &item.LastFact.Sequence,
			&item.Version, &item.Metadata,
		); err != nil {
			_ = sourceRows.Close()
			return nil, fmt.Errorf("scan V16 enterprise-freight source: %w", err)
		}
		item.DemandCode = nullStringPointer(demandCode)
		item.RouteCode = nullStringPointer(routeCode)
		state.Sources = append(state.Sources, item)
	}
	if err = closeCityRows(sourceRows, "iterate V16 enterprise-freight sources"); err != nil {
		return nil, err
	}

	lineRows, err := queryer.QueryContext(ctx, `
SELECT source_code, line_no, resource_code, source_firm_code, source_district_code,
       destination_firm_code, destination_district_code, quantity_units,
       unit_price_units, total_price_units, metadata
FROM city_open_world_enterprise_freight_source_lines
WHERE world_id = $1
ORDER BY source_code, line_no`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V16 enterprise-freight lines: %w", err)
	}
	for lineRows.Next() {
		item := CityOpenWorldEnterpriseFreightSourceLine{}
		if err = lineRows.Scan(&item.SourceCode, &item.LineNo, &item.ResourceCode,
			&item.SourceFirmCode, &item.SourceDistrictCode, &item.DestinationFirmCode,
			&item.DestinationDistrictCode, &item.QuantityUnits, &item.UnitPriceUnits,
			&item.TotalPriceUnits, &item.Metadata); err != nil {
			_ = lineRows.Close()
			return nil, fmt.Errorf("scan V16 enterprise-freight line: %w", err)
		}
		state.Lines = append(state.Lines, item)
	}
	if err = closeCityRows(lineRows, "iterate V16 enterprise-freight lines"); err != nil {
		return nil, err
	}

	factRows, err := queryer.QueryContext(ctx, `
SELECT fact.tick, fact.sequence, fact.source_code, fact.fact_type,
       runtime_fact.tick, runtime_fact.sequence, fact.payload
FROM city_open_world_enterprise_freight_facts fact
JOIN city_open_world_runtime_facts runtime_fact
  ON runtime_fact.id = fact.runtime_fact_id AND runtime_fact.world_id = fact.world_id
WHERE fact.world_id = $1
ORDER BY fact.tick, fact.sequence`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V16 enterprise-freight facts: %w", err)
	}
	for factRows.Next() {
		item := CityOpenWorldEnterpriseFreightFact{}
		if err = factRows.Scan(&item.Tick, &item.Sequence, &item.SourceCode, &item.FactType,
			&item.RuntimeFact.Tick, &item.RuntimeFact.Sequence, &item.Payload); err != nil {
			_ = factRows.Close()
			return nil, fmt.Errorf("scan V16 enterprise-freight fact: %w", err)
		}
		state.Facts = append(state.Facts, item)
	}
	if err = closeCityRows(factRows, "iterate V16 enterprise-freight facts"); err != nil {
		return nil, err
	}

	transitionRows, err := queryer.QueryContext(ctx, `
SELECT transition.source_code, transition.transition_tick, transition.transition_sequence,
       transition.state, transition.reason_code, runtime_fact.tick, runtime_fact.sequence,
       transition.metadata
FROM city_open_world_enterprise_freight_transitions transition
JOIN city_open_world_enterprise_freight_facts freight_fact
  ON freight_fact.id = transition.source_fact_id AND freight_fact.world_id = transition.world_id
JOIN city_open_world_runtime_facts runtime_fact
  ON runtime_fact.id = freight_fact.runtime_fact_id AND runtime_fact.world_id = transition.world_id
WHERE transition.world_id = $1
ORDER BY transition.source_code, transition.transition_tick, transition.transition_sequence`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V16 enterprise-freight transitions: %w", err)
	}
	for transitionRows.Next() {
		item := CityOpenWorldEnterpriseFreightTransition{}
		if err = transitionRows.Scan(&item.SourceCode, &item.TransitionTick, &item.TransitionSequence,
			&item.State, &item.ReasonCode, &item.SourceFact.Tick, &item.SourceFact.Sequence,
			&item.Metadata); err != nil {
			_ = transitionRows.Close()
			return nil, fmt.Errorf("scan V16 enterprise-freight transition: %w", err)
		}
		state.Transitions = append(state.Transitions, item)
	}
	if err = closeCityRows(transitionRows, "iterate V16 enterprise-freight transitions"); err != nil {
		return nil, err
	}
	sortCityOpenWorldEnterpriseFreightState(state)
	if err = validateCityOpenWorldEnterpriseFreightState(state); err != nil {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v16_enterprise_freight_state"}).WithCause(err)
	}
	return state, nil
}

func validateCityOpenWorldEnterpriseFreightState(state *CityOpenWorldEnterpriseFreightState) error {
	if state == nil {
		return errors.New("enterprise-freight state is required")
	}
	policyHash, err := cityOpenWorldEnterpriseFreightPolicyHash()
	if err != nil {
		return err
	}
	p := state.Policy
	if p.ProfileID != cityOpenWorldEnterpriseFreightProfileID ||
		p.ProfileVersion != cityOpenWorldEnterpriseFreightProfileVersion || p.ContentHash != policyHash ||
		p.BaselineTick < 0 || p.SourceContract != cityOpenWorldEnterpriseFreightSourceContract ||
		p.DemandContract != cityOpenWorldEnterpriseFreightDemandContract ||
		p.CompletionContract != cityOpenWorldEnterpriseFreightCompletionContract ||
		p.TerminalContract != cityOpenWorldEnterpriseFreightTerminalContract ||
		p.CarrierActorCode != cityOpenWorldEnterpriseFreightCarrierActorCode ||
		p.MaximumSources != cityOpenWorldEnterpriseFreightMaximumSources ||
		p.MaximumGenerationsPerTick != cityOpenWorldEnterpriseFreightMaximumGenerationsTick ||
		p.Revision < 1 || !cityOpenWorldEnterpriseFreightPolicyMetadataValid(p.Metadata) {
		return errors.New("invalid enterprise-freight policy")
	}
	if p.SourceCount != int64(len(state.Sources)) || p.FactCount != int64(len(state.Facts)) ||
		p.TransitionCount != int64(len(state.Transitions)) {
		return errors.New("enterprise-freight profile counters are inconsistent")
	}

	sources := make(map[string]CityOpenWorldEnterpriseFreightSource, len(state.Sources))
	orders := make(map[string]struct{}, len(state.Sources))
	pending, demands, scheduled, completed := int64(0), int64(0), int64(0), int64(0)
	expired, voided, orphaned, suppressed := int64(0), int64(0), int64(0), int64(0)
	for _, source := range state.Sources {
		if !cityOpenWorldSupplyChainCodeValid(source.Code) || !cityOpenWorldSupplyChainCodeValid(source.OrderCode) ||
			!cityOpenWorldSupplyChainCodeValid(source.SellerNodeCode) || !cityOpenWorldSupplyChainCodeValid(source.BuyerNodeCode) ||
			!cityOpenWorldSupplyChainCodeValid(source.SourceHubCode) || !cityOpenWorldSupplyChainCodeValid(source.DestinationHubCode) ||
			source.SellerNodeCode == source.BuyerNodeCode || source.SourceHubCode == source.DestinationHubCode ||
			source.CarrierActorCode != cityOpenWorldEnterpriseFreightCarrierActorCode ||
			source.DispatchFact.Tick <= 0 || source.DispatchFact.Sequence <= 0 || source.DispatchTick <= 0 ||
			source.SourceTick <= source.DispatchTick || source.MobilityDeadlineTick <= source.SourceTick ||
			source.RequestedUnits <= 0 ||
			(source.State != cityOpenWorldEnterpriseFreightStateSuppressed && source.RequestedUnits > cityOpenWorldEnterpriseFreightMaximumRequestedUnits) ||
			source.SourceFact.Tick != source.SourceTick || source.SourceFact.Sequence <= 0 ||
			source.LastFact.Tick < source.SourceFact.Tick || source.LastFact.Sequence <= 0 || source.Version < 1 ||
			!cityOpenWorldEnterpriseFreightJSONObject(source.Metadata) {
			return errors.New("invalid enterprise-freight source")
		}
		if _, exists := sources[source.Code]; exists {
			return errors.New("duplicate enterprise-freight source")
		}
		if _, exists := orders[source.OrderCode]; exists {
			return errors.New("duplicate enterprise-freight order source")
		}
		if !cityOpenWorldEnterpriseFreightStateValid(source.State) {
			return errors.New("invalid enterprise-freight source state")
		}
		if source.State == cityOpenWorldEnterpriseFreightStateSuppressed {
			if source.RequestedUnits <= cityOpenWorldEnterpriseFreightMaximumRequestedUnits ||
				source.DemandCode != nil || source.RouteCode != nil {
				return errors.New("suppressed enterprise-freight source has mobility evidence")
			}
			suppressed++
		} else {
			if source.DemandCode == nil || !cityOpenWorldSupplyChainCodeValid(*source.DemandCode) {
				return errors.New("enterprise-freight source demand is unavailable")
			}
			demands++
			switch source.State {
			case cityOpenWorldEnterpriseFreightStateDemandPending:
				pending++
			case cityOpenWorldEnterpriseFreightStateRouteScheduled:
				if source.RouteCode == nil || !cityOpenWorldSupplyChainCodeValid(*source.RouteCode) {
					return errors.New("scheduled enterprise-freight source route is unavailable")
				}
				scheduled++
			case cityOpenWorldEnterpriseFreightStateRouteCompleted:
				if source.RouteCode == nil || !cityOpenWorldSupplyChainCodeValid(*source.RouteCode) {
					return errors.New("completed enterprise-freight source route is unavailable")
				}
				completed++
			case cityOpenWorldEnterpriseFreightStateDemandExpired:
				expired++
			case cityOpenWorldEnterpriseFreightStateVoided:
				voided++
			case cityOpenWorldEnterpriseFreightStateTransportOrphaned:
				if source.RouteCode == nil || !cityOpenWorldSupplyChainCodeValid(*source.RouteCode) {
					return errors.New("orphaned enterprise-freight source route is unavailable")
				}
				orphaned++
			}
		}
		sources[source.Code] = source
		orders[source.OrderCode] = struct{}{}
	}
	if p.PendingCount != pending || p.DemandCount != demands || p.ScheduledCount != scheduled ||
		p.CompletedCount != completed || p.ExpiredCount != expired || p.VoidedCount != voided ||
		p.OrphanedCount != orphaned || p.SuppressedCount != suppressed {
		return errors.New("enterprise-freight dynamic counters are inconsistent")
	}

	lineCounts := make(map[string]int, len(state.Sources))
	lineTotals := make(map[string]int64, len(state.Sources))
	lineKeys := make(map[string]struct{}, len(state.Lines))
	for _, line := range state.Lines {
		if _, exists := sources[line.SourceCode]; !exists || line.LineNo < 1 ||
			!cityPhysicalCodePattern.MatchString(line.ResourceCode) || !cityPhysicalCodePattern.MatchString(line.SourceFirmCode) ||
			!cityPhysicalCodePattern.MatchString(line.SourceDistrictCode) || !cityPhysicalCodePattern.MatchString(line.DestinationFirmCode) ||
			!cityPhysicalCodePattern.MatchString(line.DestinationDistrictCode) || line.QuantityUnits <= 0 ||
			line.UnitPriceUnits <= 0 || line.TotalPriceUnits <= 0 ||
			line.QuantityUnits > math.MaxInt64/line.UnitPriceUnits ||
			line.QuantityUnits*line.UnitPriceUnits != line.TotalPriceUnits || !cityOpenWorldEnterpriseFreightJSONObject(line.Metadata) {
			return errors.New("invalid enterprise-freight source line")
		}
		key := fmt.Sprintf("%s\x00%d", line.SourceCode, line.LineNo)
		if _, exists := lineKeys[key]; exists {
			return errors.New("duplicate enterprise-freight source line")
		}
		lineKeys[key] = struct{}{}
		if line.QuantityUnits > math.MaxInt64-lineTotals[line.SourceCode] {
			return errors.New("enterprise-freight source line quantity overflows")
		}
		lineTotals[line.SourceCode] += line.QuantityUnits
		lineCounts[line.SourceCode]++
	}
	for sourceCode, source := range sources {
		if lineCounts[sourceCode] < 1 || lineTotals[sourceCode] != source.RequestedUnits {
			return errors.New("enterprise-freight source has no copied lines")
		}
	}

	facts := make(map[CityOpenWorldRuntimeFactRef]CityOpenWorldEnterpriseFreightFact, len(state.Facts))
	for _, fact := range state.Facts {
		cursor := CityOpenWorldRuntimeFactRef{Tick: fact.Tick, Sequence: fact.Sequence}
		if _, exists := sources[fact.SourceCode]; !exists || fact.Tick <= 0 || fact.Sequence <= 0 ||
			fact.RuntimeFact.Tick != fact.Tick || fact.RuntimeFact.Sequence != fact.Sequence ||
			!cityOpenWorldEnterpriseFreightFactTypeValid(fact.FactType) || !cityOpenWorldEnterpriseFreightJSONObject(fact.Payload) {
			return errors.New("invalid enterprise-freight fact")
		}
		if _, exists := facts[cursor]; exists {
			return errors.New("duplicate enterprise-freight fact")
		}
		facts[cursor] = fact
	}
	for code, source := range sources {
		root, rootExists := facts[source.SourceFact]
		if !rootExists || root.SourceCode != code || root.FactType != "source.created" {
			return errors.New("enterprise-freight source root projection is inconsistent")
		}
		last, lastExists := facts[source.LastFact]
		if !lastExists || last.SourceCode != code ||
			last.FactType != cityOpenWorldEnterpriseFreightStateLastFactType(source.State) {
			return errors.New("enterprise-freight source last-fact projection is inconsistent")
		}
	}

	transitionCounts := make(map[string]int, len(state.Sources))
	lastStates := make(map[string]string, len(state.Sources))
	lastFacts := make(map[string]CityOpenWorldRuntimeFactRef, len(state.Sources))
	for _, transition := range state.Transitions {
		cursor := transition.SourceFact
		fact, exists := facts[cursor]
		if _, sourceExists := sources[transition.SourceCode]; !sourceExists || !exists ||
			fact.SourceCode != transition.SourceCode ||
			transition.TransitionTick != cursor.Tick || transition.TransitionSequence != cursor.Sequence ||
			transition.TransitionTick <= 0 || transition.TransitionSequence <= 0 ||
			!cityOpenWorldEnterpriseFreightStateValid(transition.State) ||
			!cityOpenWorldSupplyChainReasonValid(transition.ReasonCode) ||
			!cityOpenWorldEnterpriseFreightTransitionReasonMatchesState(transition.State, transition.ReasonCode) ||
			!cityOpenWorldEnterpriseFreightJSONObject(transition.Metadata) ||
			!cityOpenWorldEnterpriseFreightTransitionFactMatchesState(fact.FactType, transition.State) ||
			!cityOpenWorldEnterpriseFreightTransitionAllowed(lastStates[transition.SourceCode], transition.State) {
			return errors.New("invalid enterprise-freight transition")
		}
		lastStates[transition.SourceCode] = transition.State
		lastFacts[transition.SourceCode] = cursor
		transitionCounts[transition.SourceCode]++
	}
	for code, source := range sources {
		if transitionCounts[code] < 1 || lastStates[code] != source.State ||
			lastFacts[code] != source.LastFact || source.Version != int64(transitionCounts[code]) {
			return errors.New("enterprise-freight source transition projection is inconsistent")
		}
	}
	return nil
}

func cityOpenWorldEnterpriseFreightStateValid(value string) bool {
	switch value {
	case cityOpenWorldEnterpriseFreightStateDemandPending,
		cityOpenWorldEnterpriseFreightStateRouteScheduled,
		cityOpenWorldEnterpriseFreightStateRouteCompleted,
		cityOpenWorldEnterpriseFreightStateDemandExpired,
		cityOpenWorldEnterpriseFreightStateVoided,
		cityOpenWorldEnterpriseFreightStateTransportOrphaned,
		cityOpenWorldEnterpriseFreightStateSuppressed:
		return true
	default:
		return false
	}
}

func cityOpenWorldEnterpriseFreightFactTypeValid(value string) bool {
	switch value {
	case "source.created", "source.suppressed", "demand.requested", "route.scheduled",
		"route.completed", "demand.expired", "demand.voided", "transport.orphaned":
		return true
	default:
		return false
	}
}

func cityOpenWorldEnterpriseFreightTransitionFactMatchesState(factType, state string) bool {
	switch state {
	case cityOpenWorldEnterpriseFreightStateDemandPending:
		return factType == "demand.requested"
	case cityOpenWorldEnterpriseFreightStateRouteScheduled:
		return factType == "route.scheduled"
	case cityOpenWorldEnterpriseFreightStateRouteCompleted:
		return factType == "route.completed"
	case cityOpenWorldEnterpriseFreightStateDemandExpired:
		return factType == "demand.expired"
	case cityOpenWorldEnterpriseFreightStateVoided:
		return factType == "demand.voided"
	case cityOpenWorldEnterpriseFreightStateTransportOrphaned:
		return factType == "transport.orphaned"
	case cityOpenWorldEnterpriseFreightStateSuppressed:
		return factType == "source.suppressed"
	default:
		return false
	}
}

func cityOpenWorldEnterpriseFreightStateLastFactType(state string) string {
	switch state {
	case cityOpenWorldEnterpriseFreightStateDemandPending:
		return "demand.requested"
	case cityOpenWorldEnterpriseFreightStateRouteScheduled:
		return "route.scheduled"
	case cityOpenWorldEnterpriseFreightStateRouteCompleted:
		return "route.completed"
	case cityOpenWorldEnterpriseFreightStateDemandExpired:
		return "demand.expired"
	case cityOpenWorldEnterpriseFreightStateVoided:
		return "demand.voided"
	case cityOpenWorldEnterpriseFreightStateTransportOrphaned:
		return "transport.orphaned"
	case cityOpenWorldEnterpriseFreightStateSuppressed:
		return "source.suppressed"
	default:
		return ""
	}
}

func cityOpenWorldEnterpriseFreightTransitionReasonMatchesState(state, reasonCode string) bool {
	switch state {
	case cityOpenWorldEnterpriseFreightStateDemandPending:
		return reasonCode == cityOpenWorldEnterpriseFreightReasonDispatched
	case cityOpenWorldEnterpriseFreightStateRouteScheduled:
		return reasonCode == cityOpenWorldEnterpriseFreightReasonScheduled
	case cityOpenWorldEnterpriseFreightStateRouteCompleted:
		return reasonCode == cityOpenWorldEnterpriseFreightReasonCompleted
	case cityOpenWorldEnterpriseFreightStateDemandExpired:
		return reasonCode == cityOpenWorldEnterpriseFreightReasonExpired
	case cityOpenWorldEnterpriseFreightStateVoided:
		return reasonCode == cityOpenWorldEnterpriseFreightReasonTerminalPending
	case cityOpenWorldEnterpriseFreightStateTransportOrphaned:
		return reasonCode == cityOpenWorldEnterpriseFreightReasonTerminalInTransit
	case cityOpenWorldEnterpriseFreightStateSuppressed:
		return reasonCode == cityOpenWorldEnterpriseFreightReasonUnitsExceeded
	default:
		return false
	}
}

func cityOpenWorldEnterpriseFreightTransitionAllowed(previous, next string) bool {
	switch previous {
	case "":
		return next == cityOpenWorldEnterpriseFreightStateDemandPending || next == cityOpenWorldEnterpriseFreightStateSuppressed
	case cityOpenWorldEnterpriseFreightStateDemandPending:
		return next == cityOpenWorldEnterpriseFreightStateRouteScheduled ||
			next == cityOpenWorldEnterpriseFreightStateRouteCompleted ||
			next == cityOpenWorldEnterpriseFreightStateDemandExpired ||
			next == cityOpenWorldEnterpriseFreightStateVoided
	case cityOpenWorldEnterpriseFreightStateRouteScheduled:
		return next == cityOpenWorldEnterpriseFreightStateRouteCompleted ||
			next == cityOpenWorldEnterpriseFreightStateTransportOrphaned
	case cityOpenWorldEnterpriseFreightStateRouteCompleted:
		return next == cityOpenWorldEnterpriseFreightStateTransportOrphaned
	default:
		return false
	}
}

func cityOpenWorldEnterpriseFreightJSONObject(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var object map[string]json.RawMessage
	return json.Unmarshal(raw, &object) == nil && object != nil
}

func cityOpenWorldEnterpriseFreightPolicyMetadataValid(raw json.RawMessage) bool {
	var metadata struct {
		SchemaVersion         int    `json:"schema_version"`
		Scope                 string `json:"scope"`
		Receipt               string `json:"receipt"`
		MaximumRequestedUnits int64  `json:"maximum_requested_units"`
		ModeCode              string `json:"mode_code"`
		PurposeCode           string `json:"purpose_code"`
	}
	return json.Unmarshal(raw, &metadata) == nil &&
		metadata.SchemaVersion == cityOpenWorldEnterpriseFreightSchemaVersion &&
		metadata.Scope == "dispatch_to_v9_freight_demand_only" &&
		metadata.Receipt == "not_implemented" &&
		metadata.MaximumRequestedUnits == cityOpenWorldEnterpriseFreightMaximumRequestedUnits &&
		metadata.ModeCode == cityOpenWorldEnterpriseFreightModeCode &&
		metadata.PurposeCode == cityOpenWorldEnterpriseFreightPurposeCode
}

func sortCityOpenWorldEnterpriseFreightState(state *CityOpenWorldEnterpriseFreightState) {
	if state == nil {
		return
	}
	sort.Slice(state.Sources, func(i, j int) bool { return state.Sources[i].Code < state.Sources[j].Code })
	sort.Slice(state.Lines, func(i, j int) bool {
		return state.Lines[i].SourceCode < state.Lines[j].SourceCode ||
			state.Lines[i].SourceCode == state.Lines[j].SourceCode && state.Lines[i].LineNo < state.Lines[j].LineNo
	})
	sort.Slice(state.Facts, func(i, j int) bool {
		return state.Facts[i].Tick < state.Facts[j].Tick ||
			state.Facts[i].Tick == state.Facts[j].Tick && state.Facts[i].Sequence < state.Facts[j].Sequence
	})
	sort.Slice(state.Transitions, func(i, j int) bool {
		left, right := state.Transitions[i], state.Transitions[j]
		return left.SourceCode < right.SourceCode ||
			left.SourceCode == right.SourceCode && (left.TransitionTick < right.TransitionTick ||
				left.TransitionTick == right.TransitionTick && left.TransitionSequence < right.TransitionSequence)
	})
}
