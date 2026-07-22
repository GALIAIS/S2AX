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
	"regexp"
	"sort"
	"strings"
)

const (
	cityOpenWorldSupplyChainSchemaVersion          = 1
	cityOpenWorldSupplyChainProfileID              = "sub2api-open-world-supply-chain"
	cityOpenWorldSupplyChainProfileVersion         = "1.0.0"
	cityOpenWorldSupplyChainNodeContract           = "firm_facility_district_node_v1"
	cityOpenWorldSupplyChainOrderContract          = "append_only_order_transition_v1"
	cityOpenWorldSupplyChainSettlementContract     = "acceptance_purchase_reversal_v1"
	cityOpenWorldSupplyChainDeliveryContract       = "atomic_inventory_transfer_v1"
	cityOpenWorldSupplyChainMaximumOrders          = 10000
	cityOpenWorldSupplyChainMaximumOrderLines      = 32
	cityOpenWorldSupplyChainMaximumTransitionsTick = 512
	cityOpenWorldSupplyChainAcceptTimeoutTicks     = 12
	cityOpenWorldSupplyChainDispatchTimeoutTicks   = 24
	cityOpenWorldSupplyChainSupplierFirmCode       = "municipal_services"
	cityOpenWorldSupplyChainBuyerFirmCode          = "openworld_trade_buyer"
	cityOpenWorldSupplyChainSupplierNodeCode       = "supply.node.municipal_services"
	cityOpenWorldSupplyChainBuyerNodeCode          = "supply.node.openworld_trade_buyer"
	cityOpenWorldSupplyChainSystemExpiryJournalTag = "open_world_supply_chain.auto_expiry.v1"
	cityOpenWorldSupplyChainStateProposed          = "proposed"
	cityOpenWorldSupplyChainStateAccepted          = "accepted"
	cityOpenWorldSupplyChainStateDispatched        = "dispatched"
	cityOpenWorldSupplyChainStateDelivered         = "delivered"
	// Settled is the V22 successor to V15's historic all-or-nothing
	// delivery. It is terminal like delivered, but its resource movement and
	// refund evidence live in the freight-settlement projection rather than a
	// city_open_world_supply_chain_deliveries row.
	cityOpenWorldSupplyChainStateSettled           = "settled"
	cityOpenWorldSupplyChainStateCancelled         = "cancelled"
	cityOpenWorldSupplyChainStateExpired           = "expired"
	cityOpenWorldSupplyChainStateFailed            = "failed"
	cityOpenWorldSupplyChainSettlementAcceptance   = "acceptance"
	cityOpenWorldSupplyChainSettlementReversal     = "reversal"
	cityOpenWorldSupplyChainReasonCreated          = "buyer_created"
	cityOpenWorldSupplyChainReasonAccepted         = "seller_accepted"
	cityOpenWorldSupplyChainReasonDispatched       = "seller_dispatched"
	cityOpenWorldSupplyChainReasonDelivered        = "buyer_delivered"
	cityOpenWorldSupplyChainReasonSettled          = "freight_settlement_completed"
	cityOpenWorldSupplyChainReasonCancelled        = "party_cancelled"
	cityOpenWorldSupplyChainReasonExpired          = "deadline_expired"
	cityOpenWorldSupplyChainReasonFailed           = "party_failed"
)

var cityOpenWorldSupplyChainCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{1,159}$`)

// CityOpenWorldSupplyChainPolicy pins the static F10.0 contract. Counters are
// derived from append-only projections and never serve as an independent
// source of business truth.
type CityOpenWorldSupplyChainPolicy struct {
	ProfileID                 string          `json:"profile_id"`
	ProfileVersion            string          `json:"profile_version"`
	ContentHash               string          `json:"content_hash"`
	BaselineTick              int64           `json:"baseline_tick"`
	NodeContract              string          `json:"node_contract"`
	OrderContract             string          `json:"order_contract"`
	SettlementContract        string          `json:"settlement_contract"`
	DeliveryContract          string          `json:"delivery_contract"`
	MaximumOrders             int             `json:"maximum_orders"`
	MaximumOrderLines         int             `json:"maximum_order_lines"`
	MaximumTransitionsPerTick int             `json:"maximum_transitions_per_tick"`
	AcceptTimeoutTicks        int64           `json:"accept_timeout_ticks"`
	DispatchTimeoutTicks      int64           `json:"dispatch_timeout_ticks"`
	NodeCount                 int64           `json:"node_count"`
	OrderCount                int64           `json:"order_count"`
	ActiveOrderCount          int64           `json:"active_order_count"`
	FactCount                 int64           `json:"fact_count"`
	ReservationCount          int64           `json:"reservation_count"`
	ReleaseCount              int64           `json:"release_count"`
	DispatchCount             int64           `json:"dispatch_count"`
	DeliveryCount             int64           `json:"delivery_count"`
	SettlementCount           int64           `json:"settlement_count"`
	Revision                  int64           `json:"revision"`
	Metadata                  json.RawMessage `json:"metadata"`
}

// CityOpenWorldSupplyChainNode is the only F10.0 endpoint identity. It binds
// an existing firm, active V5 facility and F3 district; F7 enterprise sites
// are intentionally not used by this open-world engine.
type CityOpenWorldSupplyChainNode struct {
	Code         string          `json:"code"`
	FirmCode     string          `json:"firm_code"`
	FacilityCode string          `json:"facility_code"`
	DistrictCode string          `json:"district_code"`
	State        string          `json:"state"`
	BaselineTick int64           `json:"baseline_tick"`
	Metadata     json.RawMessage `json:"metadata"`
}

type CityOpenWorldSupplyChainFact struct {
	Tick                  int64           `json:"tick"`
	Sequence              int64           `json:"sequence"`
	SourceCommandSequence *int64          `json:"source_command_sequence,omitempty"`
	OrderCode             *string         `json:"order_code,omitempty"`
	FactType              string          `json:"fact_type"`
	Payload               json.RawMessage `json:"payload"`
}

type CityOpenWorldSupplyChainOrder struct {
	Code                 string                      `json:"code"`
	BuyerNodeCode        string                      `json:"buyer_node_code"`
	SellerNodeCode       string                      `json:"seller_node_code"`
	CreatedTick          int64                       `json:"created_tick"`
	AcceptDeadlineTick   int64                       `json:"accept_deadline_tick"`
	DispatchDeadlineTick int64                       `json:"dispatch_deadline_tick"`
	CreatedFact          CityOpenWorldRuntimeFactRef `json:"created_fact"`
	Metadata             json.RawMessage             `json:"metadata"`
}

type CityOpenWorldSupplyChainOrderLine struct {
	OrderCode               string          `json:"order_code"`
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

type CityOpenWorldSupplyChainOrderTransition struct {
	OrderCode          string                      `json:"order_code"`
	TransitionTick     int64                       `json:"transition_tick"`
	TransitionSequence int64                       `json:"transition_sequence"`
	State              string                      `json:"state"`
	ReasonCode         string                      `json:"reason_code"`
	SourceFact         CityOpenWorldRuntimeFactRef `json:"source_fact"`
	Metadata           json.RawMessage             `json:"metadata"`
}

type CityOpenWorldSupplyChainReservation struct {
	OrderCode      string                      `json:"order_code"`
	LineNo         int                         `json:"line_no"`
	SourceFirmCode string                      `json:"source_firm_code"`
	DistrictCode   string                      `json:"district_code"`
	ResourceCode   string                      `json:"resource_code"`
	QuantityUnits  int64                       `json:"quantity_units"`
	ReservedTick   int64                       `json:"reserved_tick"`
	SourceFact     CityOpenWorldRuntimeFactRef `json:"source_fact"`
	Metadata       json.RawMessage             `json:"metadata"`
}

type CityOpenWorldSupplyChainReservationRelease struct {
	OrderCode    string                      `json:"order_code"`
	LineNo       int                         `json:"line_no"`
	ReleasedTick int64                       `json:"released_tick"`
	ReasonCode   string                      `json:"reason_code"`
	SourceFact   CityOpenWorldRuntimeFactRef `json:"source_fact"`
	Metadata     json.RawMessage             `json:"metadata"`
}

type CityOpenWorldSupplyChainDispatch struct {
	OrderCode      string                      `json:"order_code"`
	DispatchedTick int64                       `json:"dispatched_tick"`
	SourceFact     CityOpenWorldRuntimeFactRef `json:"source_fact"`
	Metadata       json.RawMessage             `json:"metadata"`
}

type CityOpenWorldSupplyChainDelivery struct {
	OrderCode         string                      `json:"order_code"`
	DeliveredTick     int64                       `json:"delivered_tick"`
	ResourceOperation CityResourceOperationCursor `json:"resource_operation"`
	SourceFact        CityOpenWorldRuntimeFactRef `json:"source_fact"`
	Metadata          json.RawMessage             `json:"metadata"`
}

type CityOpenWorldSupplyChainSettlement struct {
	OrderCode            string                      `json:"order_code"`
	SettlementKind       string                      `json:"settlement_kind"`
	Journal              CityJournalCursor           `json:"journal"`
	SourceFact           CityOpenWorldRuntimeFactRef `json:"source_fact"`
	ReversalOfSettlement *string                     `json:"reversal_of_settlement_kind,omitempty"`
	Metadata             json.RawMessage             `json:"metadata"`
}

type CityOpenWorldSupplyChainState struct {
	Policy       CityOpenWorldSupplyChainPolicy               `json:"policy"`
	Nodes        []CityOpenWorldSupplyChainNode               `json:"nodes"`
	Facts        []CityOpenWorldSupplyChainFact               `json:"facts"`
	Orders       []CityOpenWorldSupplyChainOrder              `json:"orders"`
	Lines        []CityOpenWorldSupplyChainOrderLine          `json:"lines"`
	Transitions  []CityOpenWorldSupplyChainOrderTransition    `json:"transitions"`
	Reservations []CityOpenWorldSupplyChainReservation        `json:"reservations"`
	Releases     []CityOpenWorldSupplyChainReservationRelease `json:"releases"`
	Dispatches   []CityOpenWorldSupplyChainDispatch           `json:"dispatches"`
	Deliveries   []CityOpenWorldSupplyChainDelivery           `json:"deliveries"`
	Settlements  []CityOpenWorldSupplyChainSettlement         `json:"settlements"`
}

type cityOpenWorldSupplyChainNodeSeed struct {
	code         string
	firmCode     string
	facilityID   int64
	facilityCode string
	districtID   int64
	districtCode string
}

func cityOpenWorldSupplyChainPolicyHash() (string, error) {
	raw, err := json.Marshal(struct {
		SchemaVersion        int    `json:"schema_version"`
		ProfileID            string `json:"profile_id"`
		ProfileVersion       string `json:"profile_version"`
		NodeContract         string `json:"node_contract"`
		OrderContract        string `json:"order_contract"`
		SettlementContract   string `json:"settlement_contract"`
		DeliveryContract     string `json:"delivery_contract"`
		MaximumOrders        int    `json:"maximum_orders"`
		MaximumOrderLines    int    `json:"maximum_order_lines"`
		MaximumTransitions   int    `json:"maximum_transitions_per_tick"`
		AcceptTimeoutTicks   int64  `json:"accept_timeout_ticks"`
		DispatchTimeoutTicks int64  `json:"dispatch_timeout_ticks"`
	}{
		SchemaVersion: cityOpenWorldSupplyChainSchemaVersion,
		ProfileID:     cityOpenWorldSupplyChainProfileID, ProfileVersion: cityOpenWorldSupplyChainProfileVersion,
		NodeContract: cityOpenWorldSupplyChainNodeContract, OrderContract: cityOpenWorldSupplyChainOrderContract,
		SettlementContract: cityOpenWorldSupplyChainSettlementContract, DeliveryContract: cityOpenWorldSupplyChainDeliveryContract,
		MaximumOrders: cityOpenWorldSupplyChainMaximumOrders, MaximumOrderLines: cityOpenWorldSupplyChainMaximumOrderLines,
		MaximumTransitions: cityOpenWorldSupplyChainMaximumTransitionsTick,
		AcceptTimeoutTicks: cityOpenWorldSupplyChainAcceptTimeoutTicks, DispatchTimeoutTicks: cityOpenWorldSupplyChainDispatchTimeoutTicks,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func cityOpenWorldSupplyChainOrderCode(commandSequence int64) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("supply.order.v1\x00%d", commandSequence)))
	return "supply.order." + hex.EncodeToString(sum[:20])
}

func activateCityOpenWorldSupplyChainBootstrapWrite(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if _, err := tx.ExecContext(ctx, `
SELECT set_config('sub2api.city_open_world_supply_chain_bootstrap_world_id', $1, TRUE)`, fmt.Sprintf("%d", worldID)); err != nil {
		return fmt.Errorf("enable open-world V15 supply-chain bootstrap: %w", err)
	}
	return nil
}

func activateCityOpenWorldSupplyChainWrite(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if _, err := tx.ExecContext(ctx, `
SELECT set_config('sub2api.city_open_world_supply_chain_world_id', $1, TRUE)`, fmt.Sprintf("%d", worldID)); err != nil {
		return fmt.Errorf("enable open-world V15 supply-chain write: %w", err)
	}
	return nil
}

func assertCityOpenWorldSupplyChainFoundation(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if _, err := tx.ExecContext(ctx, `SELECT assert_city_open_world_supply_chain_foundation($1)`, worldID); err != nil {
		return fmt.Errorf("validate V15 supply-chain foundation: %w", err)
	}
	return nil
}

// initializeCityOpenWorldV15SupplyChainFoundation adds only the frozen F10.0
// participants and nodes. It never creates mutable enterprise lifecycle state;
// future onboarding belongs to a versioned general-economy extension.
func initializeCityOpenWorldV15SupplyChainFoundation(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if worldID <= 0 {
		return ErrCityInvalidInput
	}
	var simulationVersion string
	var baselineTick, ownerUserID int64
	if err := tx.QueryRowContext(ctx, `
SELECT simulation_version, current_tick, owner_user_id
FROM city_worlds WHERE id = $1 FOR UPDATE`, worldID).Scan(&simulationVersion, &baselineTick, &ownerUserID); err != nil {
		return fmt.Errorf("load V15 supply-chain world: %w", err)
	}
	if !cityEngineSupportsOpenWorldSupplyChain(simulationVersion) {
		return ErrCitySimulationVersion.WithMetadata(map[string]string{"version": simulationVersion})
	}
	if _, err := tx.ExecContext(ctx, `SELECT assert_city_open_world_commute_lifecycle_foundation($1)`, worldID); err != nil {
		return fmt.Errorf("validate V15 supply-chain lifecycle prerequisite: %w", err)
	}
	if err := activateCityOpenWorldSupplyChainBootstrapWrite(ctx, tx, worldID); err != nil {
		return err
	}
	if _, err := ensureCityOpenWorldSupplyChainFirm(ctx, tx, worldID, cityOpenWorldSupplyChainSupplierFirmCode, "Municipal Services", nil); err != nil {
		return err
	}
	if _, err := ensureCityOpenWorldSupplyChainFirm(ctx, tx, worldID, cityOpenWorldSupplyChainBuyerFirmCode, "Open World Trade Buyer", &ownerUserID); err != nil {
		return err
	}
	nodes, err := loadCityOpenWorldSupplyChainNodeSeeds(ctx, tx, worldID)
	if err != nil {
		return err
	}
	if len(nodes) != 2 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v15_supply_chain_nodes"})
	}
	resourceCodes, err := loadCityOpenWorldSupplyChainStorableResourceCodes(ctx, tx, worldID)
	if err != nil {
		return err
	}
	if len(resourceCodes) == 0 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v15_supply_chain_resources"})
	}
	policyHash, err := cityOpenWorldSupplyChainPolicyHash()
	if err != nil {
		return fmt.Errorf("hash V15 supply-chain policy: %w", err)
	}
	metadata, err := json.Marshal(map[string]any{
		"schema_version":   cityOpenWorldSupplyChainSchemaVersion,
		"endpoint_model":   "firm_facility_district_node_v1",
		"participant_mode": "frozen_baseline_participants_v1",
	})
	if err != nil {
		return fmt.Errorf("marshal V15 supply-chain profile metadata: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_supply_chain_profiles
    (world_id, profile_id, profile_version, content_hash, baseline_tick,
     node_contract, order_contract, settlement_contract, delivery_contract,
     maximum_orders, maximum_order_lines, maximum_transitions_per_tick,
     accept_timeout_ticks, dispatch_timeout_ticks, node_count, order_count,
     active_order_count, fact_count, reservation_count, release_count,
     dispatch_count, delivery_count, settlement_count, revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14,
        $15, 0, 0, 0, 0, 0, 0, 0, 0, 1, $16::jsonb)`,
		worldID, cityOpenWorldSupplyChainProfileID, cityOpenWorldSupplyChainProfileVersion, policyHash, baselineTick,
		cityOpenWorldSupplyChainNodeContract, cityOpenWorldSupplyChainOrderContract,
		cityOpenWorldSupplyChainSettlementContract, cityOpenWorldSupplyChainDeliveryContract,
		cityOpenWorldSupplyChainMaximumOrders, cityOpenWorldSupplyChainMaximumOrderLines,
		cityOpenWorldSupplyChainMaximumTransitionsTick, cityOpenWorldSupplyChainAcceptTimeoutTicks,
		cityOpenWorldSupplyChainDispatchTimeoutTicks, len(nodes), metadata); err != nil {
		return fmt.Errorf("insert V15 supply-chain profile: %w", err)
	}
	for _, node := range nodes {
		var firmID int64
		if err = tx.QueryRowContext(ctx, `
SELECT id FROM city_economic_entities
WHERE world_id = $1 AND entity_type = 'firm' AND code = $2 AND status = 'active'`, worldID, node.firmCode).Scan(&firmID); err != nil {
			return fmt.Errorf("load V15 supply-chain firm %s: %w", node.firmCode, err)
		}
		nodeMetadata, marshalErr := json.Marshal(map[string]any{
			"schema_version": cityOpenWorldSupplyChainSchemaVersion,
			"firm_code":      node.firmCode, "facility_code": node.facilityCode, "district_code": node.districtCode,
		})
		if marshalErr != nil {
			return fmt.Errorf("marshal V15 supply-chain node metadata: %w", marshalErr)
		}
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_supply_chain_nodes
    (world_id, code, firm_entity_id, facility_id, district_id, state, baseline_tick, metadata)
VALUES ($1, $2, $3, $4, $5, 'active', $6, $7::jsonb)`,
			worldID, node.code, firmID, node.facilityID, node.districtID, baselineTick, nodeMetadata); err != nil {
			return fmt.Errorf("insert V15 supply-chain node %s: %w", node.code, err)
		}
		// Freeze a zero-capacity inventory topology in the V15 baseline instead
		// of allowing an otherwise invisible balance row to materialize when a
		// player places the first order.  It keeps new worlds' physical snapshot
		// topology deterministic; the command path still calls ensure* as an
		// idempotent compatibility guard for legacy worlds and later catalogs.
		for _, resourceCode := range resourceCodes {
			if _, ensureErr := ensureCityInventoryRef(ctx, tx, worldID, firmID, node.districtCode, resourceCode); ensureErr != nil {
				return fmt.Errorf("initialize V15 supply-chain inventory %s/%s: %w", node.code, resourceCode, ensureErr)
			}
		}
	}
	return assertCityOpenWorldSupplyChainFoundation(ctx, tx, worldID)
}

func loadCityOpenWorldSupplyChainStorableResourceCodes(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) ([]string, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT code
FROM city_resources
WHERE world_id = $1 AND status = 'active' AND storable
ORDER BY code`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V15 supply-chain storable resources: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]string, 0)
	for rows.Next() {
		var code string
		if err = rows.Scan(&code); err != nil {
			return nil, fmt.Errorf("scan V15 supply-chain storable resource: %w", err)
		}
		items = append(items, code)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate V15 supply-chain storable resources: %w", err)
	}
	return items, nil
}

func ensureCityOpenWorldSupplyChainFirm(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	code, name string,
	ownerUserID *int64,
) (int64, error) {
	var entityID int64
	err := tx.QueryRowContext(ctx, `
SELECT id FROM city_economic_entities
WHERE world_id = $1 AND entity_type = 'firm' AND code = $2`, worldID, code).Scan(&entityID)
	if errors.Is(err, sql.ErrNoRows) {
		metadata, marshalErr := json.Marshal(map[string]any{
			"schema_version": cityOpenWorldSupplyChainSchemaVersion,
			"foundation":     "open_world_supply_chain_v15",
		})
		if marshalErr != nil {
			return 0, marshalErr
		}
		var owner any
		if ownerUserID != nil {
			owner = *ownerUserID
		}
		if err = tx.QueryRowContext(ctx, `
INSERT INTO city_economic_entities
    (world_id, entity_type, code, name, owner_user_id, status, metadata)
VALUES ($1, 'firm', $2, $3, $4, 'active', $5::jsonb)
RETURNING id`, worldID, code, name, owner, metadata).Scan(&entityID); err != nil {
			return 0, fmt.Errorf("insert V15 supply-chain firm %s: %w", code, err)
		}
	} else if err != nil {
		return 0, fmt.Errorf("load V15 supply-chain firm %s: %w", code, err)
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_accounts
    (world_id, entity_id, entity_type, monetary_unit_id, template_id,
     allow_negative, current_balance_units, version, status, metadata)
SELECT $1, $2, 'firm', unit.id, template.id, template.allow_negative,
       0, 0, 'active', '{}'::jsonb
FROM city_monetary_units unit
JOIN city_account_templates template
  ON template.world_id = unit.world_id AND template.entity_type = 'firm'
WHERE unit.world_id = $1 AND unit.is_base AND unit.status = 'active'
ON CONFLICT DO NOTHING`, worldID, entityID); err != nil {
		return 0, fmt.Errorf("ensure V15 supply-chain firm accounts %s: %w", code, err)
	}
	return entityID, nil
}

func loadCityOpenWorldSupplyChainNodeSeeds(ctx context.Context, tx *sql.Tx, worldID int64) ([]cityOpenWorldSupplyChainNodeSeed, error) {
	var districtID int64
	var districtCode string
	if err := tx.QueryRowContext(ctx, `
SELECT id, code FROM city_districts
WHERE world_id = $1
ORDER BY CASE WHEN code = 'central' THEN 0 ELSE 1 END, sort_order, code
LIMIT 1`, worldID).Scan(&districtID, &districtCode); err != nil {
		return nil, fmt.Errorf("load V15 supply-chain district: %w", err)
	}
	rows, err := tx.QueryContext(ctx, `
SELECT id, code
FROM city_open_world_facilities
WHERE world_id = $1 AND state = 'active'
ORDER BY CASE facility_type_code WHEN 'industry' THEN 0 WHEN 'commerce' THEN 1 ELSE 2 END, code
LIMIT 2`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V15 supply-chain facilities: %w", err)
	}
	defer func() { _ = rows.Close() }()
	facilities := make([]struct {
		id   int64
		code string
	}, 0, 2)
	for rows.Next() {
		item := struct {
			id   int64
			code string
		}{}
		if err = rows.Scan(&item.id, &item.code); err != nil {
			return nil, fmt.Errorf("scan V15 supply-chain facility: %w", err)
		}
		facilities = append(facilities, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate V15 supply-chain facilities: %w", err)
	}
	if len(facilities) < 2 {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v15_supply_chain_facilities"})
	}
	return []cityOpenWorldSupplyChainNodeSeed{
		{code: cityOpenWorldSupplyChainSupplierNodeCode, firmCode: cityOpenWorldSupplyChainSupplierFirmCode,
			facilityID: facilities[0].id, facilityCode: facilities[0].code, districtID: districtID, districtCode: districtCode},
		{code: cityOpenWorldSupplyChainBuyerNodeCode, firmCode: cityOpenWorldSupplyChainBuyerFirmCode,
			facilityID: facilities[1].id, facilityCode: facilities[1].code, districtID: districtID, districtCode: districtCode},
	}, nil
}

func cityOpenWorldSupplyChainCurrentState(transitions []CityOpenWorldSupplyChainOrderTransition, orderCode string) string {
	for index := len(transitions) - 1; index >= 0; index-- {
		if transitions[index].OrderCode == orderCode {
			return transitions[index].State
		}
	}
	return ""
}

func cityOpenWorldSupplyChainStateTerminal(state string) bool {
	return state == cityOpenWorldSupplyChainStateDelivered || state == cityOpenWorldSupplyChainStateSettled || state == cityOpenWorldSupplyChainStateCancelled ||
		state == cityOpenWorldSupplyChainStateExpired || state == cityOpenWorldSupplyChainStateFailed
}

func cityOpenWorldSupplyChainTransitionAllowed(previous, next string) bool {
	switch previous {
	case "":
		return next == cityOpenWorldSupplyChainStateProposed
	case cityOpenWorldSupplyChainStateProposed:
		return next == cityOpenWorldSupplyChainStateAccepted || next == cityOpenWorldSupplyChainStateCancelled || next == cityOpenWorldSupplyChainStateExpired
	case cityOpenWorldSupplyChainStateAccepted:
		return next == cityOpenWorldSupplyChainStateDispatched || next == cityOpenWorldSupplyChainStateCancelled || next == cityOpenWorldSupplyChainStateExpired
	case cityOpenWorldSupplyChainStateDispatched:
		return next == cityOpenWorldSupplyChainStateDelivered || next == cityOpenWorldSupplyChainStateSettled || next == cityOpenWorldSupplyChainStateFailed
	default:
		return false
	}
}

func validateCityOpenWorldSupplyChainState(state *CityOpenWorldSupplyChainState) error {
	if state == nil {
		return errors.New("supply-chain state is required")
	}
	policyHash, err := cityOpenWorldSupplyChainPolicyHash()
	if err != nil {
		return err
	}
	p := state.Policy
	if p.ProfileID != cityOpenWorldSupplyChainProfileID || p.ProfileVersion != cityOpenWorldSupplyChainProfileVersion ||
		p.ContentHash != policyHash || p.BaselineTick < 0 || p.NodeContract != cityOpenWorldSupplyChainNodeContract ||
		p.OrderContract != cityOpenWorldSupplyChainOrderContract || p.SettlementContract != cityOpenWorldSupplyChainSettlementContract ||
		p.DeliveryContract != cityOpenWorldSupplyChainDeliveryContract || p.MaximumOrders != cityOpenWorldSupplyChainMaximumOrders ||
		p.MaximumOrderLines != cityOpenWorldSupplyChainMaximumOrderLines || p.MaximumTransitionsPerTick != cityOpenWorldSupplyChainMaximumTransitionsTick ||
		p.AcceptTimeoutTicks != cityOpenWorldSupplyChainAcceptTimeoutTicks || p.DispatchTimeoutTicks != cityOpenWorldSupplyChainDispatchTimeoutTicks ||
		p.DispatchTimeoutTicks <= p.AcceptTimeoutTicks || p.Revision < 1 || !cityOpenWorldSupplyChainJSONObject(state.Policy.Metadata) {
		return errors.New("invalid supply-chain policy")
	}
	if p.NodeCount != int64(len(state.Nodes)) || p.OrderCount != int64(len(state.Orders)) || p.FactCount != int64(len(state.Facts)) ||
		p.ReservationCount != int64(len(state.Reservations)) || p.ReleaseCount != int64(len(state.Releases)) ||
		p.DispatchCount != int64(len(state.Dispatches)) || p.DeliveryCount != int64(len(state.Deliveries)) ||
		p.SettlementCount != int64(len(state.Settlements)) || len(state.Nodes) < 2 {
		return errors.New("supply-chain counters are inconsistent")
	}
	nodes := make(map[string]CityOpenWorldSupplyChainNode, len(state.Nodes))
	firmNodes := make(map[string]struct{}, len(state.Nodes))
	for _, node := range state.Nodes {
		if !cityOpenWorldSupplyChainCodeValid(node.Code) || !cityPhysicalCodePattern.MatchString(node.FirmCode) ||
			!cityOpenWorldSupplyChainCodeValid(node.FacilityCode) || !cityPhysicalCodePattern.MatchString(node.DistrictCode) ||
			node.State != "active" || node.BaselineTick != p.BaselineTick || !cityOpenWorldSupplyChainJSONObject(node.Metadata) {
			return errors.New("invalid supply-chain node")
		}
		if _, exists := nodes[node.Code]; exists {
			return errors.New("duplicate supply-chain node")
		}
		if _, exists := firmNodes[node.FirmCode+"\x00"+node.FacilityCode]; exists {
			return errors.New("duplicate supply-chain firm/facility node")
		}
		nodes[node.Code] = node
		firmNodes[node.FirmCode+"\x00"+node.FacilityCode] = struct{}{}
	}
	facts := make(map[CityOpenWorldRuntimeFactRef]CityOpenWorldSupplyChainFact, len(state.Facts))
	var lastFact CityOpenWorldRuntimeFactRef
	for index, fact := range state.Facts {
		key := CityOpenWorldRuntimeFactRef{Tick: fact.Tick, Sequence: fact.Sequence}
		if fact.Tick <= 0 || fact.Sequence <= 0 || !cityOpenWorldSupplyChainFactTypeValid(fact.FactType) || !cityOpenWorldSupplyChainJSONObject(fact.Payload) ||
			(fact.OrderCode != nil && !cityOpenWorldSupplyChainCodeValid(*fact.OrderCode)) ||
			(index > 0 && (key.Tick < lastFact.Tick || key.Tick == lastFact.Tick && key.Sequence <= lastFact.Sequence)) {
			return errors.New("invalid supply-chain fact")
		}
		if _, exists := facts[key]; exists {
			return errors.New("duplicate supply-chain fact")
		}
		facts[key] = fact
		lastFact = key
	}
	orders := make(map[string]CityOpenWorldSupplyChainOrder, len(state.Orders))
	for _, order := range state.Orders {
		if !cityOpenWorldSupplyChainCodeValid(order.Code) || order.BuyerNodeCode == order.SellerNodeCode ||
			order.CreatedTick <= 0 || order.AcceptDeadlineTick <= order.CreatedTick || order.DispatchDeadlineTick <= order.AcceptDeadlineTick ||
			!cityOpenWorldSupplyChainJSONObject(order.Metadata) {
			return errors.New("invalid supply-chain order")
		}
		if _, exists := nodes[order.BuyerNodeCode]; !exists {
			return errors.New("supply-chain buyer node is missing")
		}
		if _, exists := nodes[order.SellerNodeCode]; !exists {
			return errors.New("supply-chain seller node is missing")
		}
		if nodes[order.BuyerNodeCode].FirmCode == nodes[order.SellerNodeCode].FirmCode {
			return errors.New("supply-chain order self trade")
		}
		if _, exists := facts[order.CreatedFact]; !exists {
			return errors.New("supply-chain order created fact missing")
		}
		if _, exists := orders[order.Code]; exists {
			return errors.New("duplicate supply-chain order")
		}
		orders[order.Code] = order
	}
	linesByOrder := make(map[string][]CityOpenWorldSupplyChainOrderLine, len(state.Orders))
	seenLineResource := make(map[string]struct{}, len(state.Lines))
	for _, line := range state.Lines {
		order, exists := orders[line.OrderCode]
		if !exists || line.LineNo <= 0 || !cityPhysicalCodePattern.MatchString(line.ResourceCode) ||
			!cityPhysicalCodePattern.MatchString(line.SourceFirmCode) || !cityPhysicalCodePattern.MatchString(line.SourceDistrictCode) ||
			!cityPhysicalCodePattern.MatchString(line.DestinationFirmCode) || !cityPhysicalCodePattern.MatchString(line.DestinationDistrictCode) ||
			line.QuantityUnits <= 0 || line.UnitPriceUnits <= 0 || line.TotalPriceUnits <= 0 ||
			line.QuantityUnits > cityMaximumResourceUnits || line.UnitPriceUnits > cityMaximumResourceUnits ||
			line.QuantityUnits > math.MaxInt64/line.UnitPriceUnits ||
			line.QuantityUnits*line.UnitPriceUnits != line.TotalPriceUnits || !cityOpenWorldSupplyChainJSONObject(line.Metadata) {
			return errors.New("invalid supply-chain order line")
		}
		if line.SourceFirmCode != nodes[order.SellerNodeCode].FirmCode || line.SourceDistrictCode != nodes[order.SellerNodeCode].DistrictCode ||
			line.DestinationFirmCode != nodes[order.BuyerNodeCode].FirmCode || line.DestinationDistrictCode != nodes[order.BuyerNodeCode].DistrictCode {
			return errors.New("supply-chain order line does not match nodes")
		}
		lineKey := line.OrderCode + "\x00" + line.ResourceCode
		if _, exists := seenLineResource[lineKey]; exists {
			return errors.New("duplicate supply-chain line resource")
		}
		seenLineResource[lineKey] = struct{}{}
		linesByOrder[line.OrderCode] = append(linesByOrder[line.OrderCode], line)
	}
	for code, orderLines := range linesByOrder {
		if len(orderLines) > p.MaximumOrderLines {
			return errors.New("supply-chain order exceeds line limit")
		}
		sort.Slice(orderLines, func(i, j int) bool { return orderLines[i].LineNo < orderLines[j].LineNo })
		for index, line := range orderLines {
			if line.LineNo != index+1 {
				return errors.New("supply-chain line numbers are not contiguous")
			}
		}
		linesByOrder[code] = orderLines
	}
	for code := range orders {
		if len(linesByOrder[code]) == 0 {
			return errors.New("supply-chain order has no lines")
		}
	}
	transitionsByOrder := make(map[string][]CityOpenWorldSupplyChainOrderTransition, len(state.Orders))
	for _, transition := range state.Transitions {
		if _, exists := orders[transition.OrderCode]; !exists || transition.TransitionTick <= 0 || transition.TransitionSequence <= 0 ||
			!cityOpenWorldSupplyChainTransitionStateValid(transition.State) || !cityOpenWorldSupplyChainReasonValid(transition.ReasonCode) ||
			!cityOpenWorldSupplyChainJSONObject(transition.Metadata) {
			return errors.New("invalid supply-chain transition")
		}
		fact, exists := facts[transition.SourceFact]
		if !exists || fact.OrderCode == nil || *fact.OrderCode != transition.OrderCode || !cityOpenWorldSupplyChainFactMatchesState(fact.FactType, transition.State) {
			return errors.New("supply-chain transition fact mismatch")
		}
		transitionsByOrder[transition.OrderCode] = append(transitionsByOrder[transition.OrderCode], transition)
	}
	activeOrders := int64(0)
	for code := range orders {
		transitions := transitionsByOrder[code]
		if len(transitions) == 0 {
			return errors.New("supply-chain order missing transitions")
		}
		sort.Slice(transitions, func(i, j int) bool {
			return transitions[i].TransitionTick < transitions[j].TransitionTick ||
				transitions[i].TransitionTick == transitions[j].TransitionTick && transitions[i].TransitionSequence < transitions[j].TransitionSequence
		})
		previous := ""
		var last CityOpenWorldRuntimeFactRef
		for index, transition := range transitions {
			cursor := CityOpenWorldRuntimeFactRef{Tick: transition.TransitionTick, Sequence: transition.TransitionSequence}
			if index > 0 && (cursor.Tick < last.Tick || cursor.Tick == last.Tick && cursor.Sequence <= last.Sequence) ||
				!cityOpenWorldSupplyChainTransitionAllowed(previous, transition.State) {
				return errors.New("invalid supply-chain transition sequence")
			}
			previous, last = transition.State, cursor
		}
		if !cityOpenWorldSupplyChainStateTerminal(previous) {
			activeOrders++
		}
		transitionsByOrder[code] = transitions
	}
	if p.ActiveOrderCount != activeOrders || p.OrderCount > int64(p.MaximumOrders) {
		return errors.New("invalid supply-chain active counters")
	}
	return validateCityOpenWorldSupplyChainEvidence(state, orders, linesByOrder, transitionsByOrder, facts)
}

func validateCityOpenWorldSupplyChainEvidence(
	state *CityOpenWorldSupplyChainState,
	orders map[string]CityOpenWorldSupplyChainOrder,
	linesByOrder map[string][]CityOpenWorldSupplyChainOrderLine,
	transitionsByOrder map[string][]CityOpenWorldSupplyChainOrderTransition,
	facts map[CityOpenWorldRuntimeFactRef]CityOpenWorldSupplyChainFact,
) error {
	reservations := make(map[string]CityOpenWorldSupplyChainReservation, len(state.Reservations))
	for _, reservation := range state.Reservations {
		lineKey := fmt.Sprintf("%s\x00%d", reservation.OrderCode, reservation.LineNo)
		line, found := cityOpenWorldSupplyChainLineForKey(linesByOrder, lineKey)
		if !found || reservation.SourceFirmCode != line.SourceFirmCode || reservation.DistrictCode != line.SourceDistrictCode ||
			reservation.ResourceCode != line.ResourceCode || reservation.QuantityUnits != line.QuantityUnits || reservation.ReservedTick <= 0 ||
			!cityOpenWorldSupplyChainJSONObject(reservation.Metadata) || facts[reservation.SourceFact].FactType != "order.accepted" {
			return errors.New("invalid supply-chain reservation")
		}
		if _, exists := reservations[lineKey]; exists {
			return errors.New("duplicate supply-chain reservation")
		}
		reservations[lineKey] = reservation
	}
	releases := make(map[string]CityOpenWorldSupplyChainReservationRelease, len(state.Releases))
	for _, release := range state.Releases {
		key := fmt.Sprintf("%s\x00%d", release.OrderCode, release.LineNo)
		if _, exists := reservations[key]; !exists || release.ReleasedTick <= 0 ||
			(release.ReasonCode != "delivered" && release.ReasonCode != "settled" && release.ReasonCode != "cancelled" && release.ReasonCode != "expired" && release.ReasonCode != "failed") ||
			!cityOpenWorldSupplyChainJSONObject(release.Metadata) || !cityOpenWorldSupplyChainFactMatchesState(facts[release.SourceFact].FactType, release.ReasonCode) {
			return errors.New("invalid supply-chain reservation release")
		}
		if _, exists := releases[key]; exists {
			return errors.New("duplicate supply-chain reservation release")
		}
		releases[key] = release
	}
	dispatches := make(map[string]CityOpenWorldSupplyChainDispatch, len(state.Dispatches))
	for _, dispatch := range state.Dispatches {
		if _, exists := orders[dispatch.OrderCode]; !exists || dispatch.DispatchedTick <= 0 || !cityOpenWorldSupplyChainJSONObject(dispatch.Metadata) ||
			facts[dispatch.SourceFact].FactType != "order.dispatched" {
			return errors.New("invalid supply-chain dispatch")
		}
		if _, exists := dispatches[dispatch.OrderCode]; exists {
			return errors.New("duplicate supply-chain dispatch")
		}
		dispatches[dispatch.OrderCode] = dispatch
	}
	deliveries := make(map[string]CityOpenWorldSupplyChainDelivery, len(state.Deliveries))
	for _, delivery := range state.Deliveries {
		if _, exists := orders[delivery.OrderCode]; !exists || delivery.DeliveredTick <= 0 || delivery.ResourceOperation.Tick <= 0 ||
			delivery.ResourceOperation.Sequence <= 0 || !cityOpenWorldSupplyChainJSONObject(delivery.Metadata) || facts[delivery.SourceFact].FactType != "order.delivered" {
			return errors.New("invalid supply-chain delivery")
		}
		if _, exists := deliveries[delivery.OrderCode]; exists {
			return errors.New("duplicate supply-chain delivery")
		}
		deliveries[delivery.OrderCode] = delivery
	}
	settlements := make(map[string]map[string]CityOpenWorldSupplyChainSettlement, len(state.Orders))
	for _, settlement := range state.Settlements {
		if _, exists := orders[settlement.OrderCode]; !exists ||
			(settlement.SettlementKind != cityOpenWorldSupplyChainSettlementAcceptance && settlement.SettlementKind != cityOpenWorldSupplyChainSettlementReversal) ||
			settlement.Journal.Tick <= 0 || settlement.Journal.Sequence <= 0 || !cityOpenWorldSupplyChainJSONObject(settlement.Metadata) {
			return errors.New("invalid supply-chain settlement")
		}
		if settlements[settlement.OrderCode] == nil {
			settlements[settlement.OrderCode] = make(map[string]CityOpenWorldSupplyChainSettlement, 2)
		}
		if _, exists := settlements[settlement.OrderCode][settlement.SettlementKind]; exists {
			return errors.New("duplicate supply-chain settlement")
		}
		if settlement.SettlementKind == cityOpenWorldSupplyChainSettlementAcceptance && facts[settlement.SourceFact].FactType != "order.accepted" {
			return errors.New("invalid supply-chain acceptance settlement fact")
		}
		if settlement.SettlementKind == cityOpenWorldSupplyChainSettlementReversal &&
			!cityOpenWorldSupplyChainTerminalFact(facts[settlement.SourceFact].FactType) {
			return errors.New("invalid supply-chain reversal settlement fact")
		}
		settlements[settlement.OrderCode][settlement.SettlementKind] = settlement
	}
	for code := range orders {
		stateName := transitionsByOrder[code][len(transitionsByOrder[code])-1].State
		accepted := false
		for _, transition := range transitionsByOrder[code] {
			if transition.State == cityOpenWorldSupplyChainStateAccepted {
				accepted = true
				break
			}
		}
		if accepted {
			if len(reservationsForCityOpenWorldSupplyChainOrder(reservations, code)) != len(linesByOrder[code]) ||
				settlements[code][cityOpenWorldSupplyChainSettlementAcceptance].OrderCode == "" {
				return errors.New("accepted supply-chain order evidence missing")
			}
		}
		if stateName == cityOpenWorldSupplyChainStateDispatched && dispatches[code].OrderCode == "" {
			return errors.New("dispatched supply-chain order evidence missing")
		}
		if stateName == cityOpenWorldSupplyChainStateDelivered && deliveries[code].OrderCode == "" {
			return errors.New("delivered supply-chain order evidence missing")
		}
		if cityOpenWorldSupplyChainStateTerminal(stateName) && accepted {
			if len(releasesForCityOpenWorldSupplyChainOrder(releases, code)) != len(linesByOrder[code]) {
				return errors.New("terminal supply-chain order releases missing")
			}
			if stateName != cityOpenWorldSupplyChainStateDelivered && stateName != cityOpenWorldSupplyChainStateSettled && settlements[code][cityOpenWorldSupplyChainSettlementReversal].OrderCode == "" {
				return errors.New("terminal supply-chain order reversal missing")
			}
		}
	}
	return nil
}

func cityOpenWorldSupplyChainLineForKey(linesByOrder map[string][]CityOpenWorldSupplyChainOrderLine, key string) (CityOpenWorldSupplyChainOrderLine, bool) {
	parts := strings.Split(key, "\x00")
	if len(parts) != 2 {
		return CityOpenWorldSupplyChainOrderLine{}, false
	}
	for _, line := range linesByOrder[parts[0]] {
		if fmt.Sprintf("%d", line.LineNo) == parts[1] {
			return line, true
		}
	}
	return CityOpenWorldSupplyChainOrderLine{}, false
}

func reservationsForCityOpenWorldSupplyChainOrder(values map[string]CityOpenWorldSupplyChainReservation, orderCode string) []CityOpenWorldSupplyChainReservation {
	items := make([]CityOpenWorldSupplyChainReservation, 0)
	for _, value := range values {
		if value.OrderCode == orderCode {
			items = append(items, value)
		}
	}
	return items
}

func releasesForCityOpenWorldSupplyChainOrder(values map[string]CityOpenWorldSupplyChainReservationRelease, orderCode string) []CityOpenWorldSupplyChainReservationRelease {
	items := make([]CityOpenWorldSupplyChainReservationRelease, 0)
	for _, value := range values {
		if value.OrderCode == orderCode {
			items = append(items, value)
		}
	}
	return items
}

func cityOpenWorldSupplyChainFactTypeValid(value string) bool {
	switch value {
	case "order.proposed", "order.accepted", "order.dispatched", "order.delivered", "order.settled", "order.cancelled", "order.expired", "order.failed":
		return true
	default:
		return false
	}
}

func cityOpenWorldSupplyChainCodeValid(value string) bool {
	return cityOpenWorldSupplyChainCodePattern.MatchString(value)
}

func cityOpenWorldSupplyChainReasonValid(value string) bool {
	return len(value) <= 96 && cityOpenWorldSupplyChainCodePattern.MatchString(value)
}

func cityOpenWorldSupplyChainJSONObject(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var object map[string]json.RawMessage
	return json.Unmarshal(raw, &object) == nil && object != nil
}

func cityOpenWorldSupplyChainTransitionStateValid(value string) bool {
	return value == cityOpenWorldSupplyChainStateProposed || value == cityOpenWorldSupplyChainStateAccepted ||
		value == cityOpenWorldSupplyChainStateDispatched || value == cityOpenWorldSupplyChainStateDelivered || value == cityOpenWorldSupplyChainStateSettled ||
		value == cityOpenWorldSupplyChainStateCancelled || value == cityOpenWorldSupplyChainStateExpired || value == cityOpenWorldSupplyChainStateFailed
}

func cityOpenWorldSupplyChainFactMatchesState(factType, state string) bool {
	return factType == "order."+state
}

func cityOpenWorldSupplyChainTerminalFact(factType string) bool {
	// A V22 settlement is terminal but not an accounting reversal: accepted
	// units remain paid and lost/rejected units receive a proportional refund.
	// Keep this helper intentionally restricted to the legacy full reversal
	// terminal facts used by the V15 settlement evidence validator.
	return factType == "order.cancelled" || factType == "order.expired" || factType == "order.failed"
}

func sortCityOpenWorldSupplyChainState(state *CityOpenWorldSupplyChainState) {
	if state == nil {
		return
	}
	sort.Slice(state.Nodes, func(i, j int) bool { return state.Nodes[i].Code < state.Nodes[j].Code })
	sort.Slice(state.Facts, func(i, j int) bool {
		return state.Facts[i].Tick < state.Facts[j].Tick || state.Facts[i].Tick == state.Facts[j].Tick && state.Facts[i].Sequence < state.Facts[j].Sequence
	})
	sort.Slice(state.Orders, func(i, j int) bool { return state.Orders[i].Code < state.Orders[j].Code })
	sort.Slice(state.Lines, func(i, j int) bool {
		return state.Lines[i].OrderCode < state.Lines[j].OrderCode || state.Lines[i].OrderCode == state.Lines[j].OrderCode && state.Lines[i].LineNo < state.Lines[j].LineNo
	})
	sort.Slice(state.Transitions, func(i, j int) bool {
		left, right := state.Transitions[i], state.Transitions[j]
		return left.OrderCode < right.OrderCode || left.OrderCode == right.OrderCode && (left.TransitionTick < right.TransitionTick || left.TransitionTick == right.TransitionTick && left.TransitionSequence < right.TransitionSequence)
	})
	sort.Slice(state.Reservations, func(i, j int) bool {
		return state.Reservations[i].OrderCode < state.Reservations[j].OrderCode || state.Reservations[i].OrderCode == state.Reservations[j].OrderCode && state.Reservations[i].LineNo < state.Reservations[j].LineNo
	})
	sort.Slice(state.Releases, func(i, j int) bool {
		return state.Releases[i].OrderCode < state.Releases[j].OrderCode || state.Releases[i].OrderCode == state.Releases[j].OrderCode && state.Releases[i].LineNo < state.Releases[j].LineNo
	})
	sort.Slice(state.Dispatches, func(i, j int) bool { return state.Dispatches[i].OrderCode < state.Dispatches[j].OrderCode })
	sort.Slice(state.Deliveries, func(i, j int) bool { return state.Deliveries[i].OrderCode < state.Deliveries[j].OrderCode })
	sort.Slice(state.Settlements, func(i, j int) bool {
		return state.Settlements[i].OrderCode < state.Settlements[j].OrderCode || state.Settlements[i].OrderCode == state.Settlements[j].OrderCode && state.Settlements[i].SettlementKind < state.Settlements[j].SettlementKind
	})
}
