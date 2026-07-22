package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
)

const (
	cityOpenWorldEffectiveCapacitySchemaVersion      = 1
	cityOpenWorldEffectiveCapacityProfileID          = "sub2api-open-world-effective-capacity"
	cityOpenWorldEffectiveCapacityProfileVersion     = "1.0.0"
	cityOpenWorldEffectiveCapacityTopologyContract   = "v19_edge_corridor_mapping_v1"
	cityOpenWorldEffectiveCapacityAssetContract      = "v20_corridor_segment_ordinal_1_v1"
	cityOpenWorldEffectiveCapacityAdmissionContract  = "effective_infrastructure_capacity_v1"
	cityOpenWorldEffectiveCapacityVisibilityContract = "next_tick_after_command_v1"
	cityOpenWorldEffectiveCapacityMaximumAdmissions  = 1_000_000
	cityOpenWorldEffectiveCapacitySchedulerEffect    = "next_tick_effective_capacity_v1"
)

// CityOpenWorldEffectiveCapacityPolicy pins V21's only new routing bridge.
// It does not own topology or infrastructure state: those remain V19 and V20
// authority. Its mutable counter is a reconstruction/audit aid only.
type CityOpenWorldEffectiveCapacityPolicy struct {
	ProfileID          string          `json:"profile_id"`
	ProfileVersion     string          `json:"profile_version"`
	ContentHash        string          `json:"content_hash"`
	BaselineTick       int64           `json:"baseline_tick"`
	TopologyContract   string          `json:"topology_contract"`
	AssetContract      string          `json:"asset_contract"`
	AdmissionContract  string          `json:"admission_contract"`
	VisibilityContract string          `json:"visibility_contract"`
	MaximumAdmissions  int             `json:"maximum_admissions"`
	AdmissionCount     int64           `json:"admission_count"`
	Revision           int64           `json:"revision"`
	Metadata           json.RawMessage `json:"metadata"`
}

// CityOpenWorldEffectiveCapacityAdmission freezes the exact V20 corridor
// state consumed by one V9 route-edge reservation. It is deliberately not a
// second allocation: V9 remains the owner of the reservation itself.
type CityOpenWorldEffectiveCapacityAdmission struct {
	RouteCode                     string                       `json:"route_code"`
	EdgeCode                      string                       `json:"edge_code"`
	DepartureTick                 int64                        `json:"departure_tick"`
	CorridorCode                  string                       `json:"corridor_code"`
	AssetCode                     string                       `json:"asset_code"`
	AssetState                    string                       `json:"asset_state"`
	StateEffectiveTick            int64                        `json:"state_effective_tick"`
	StateSourceFact               *CityOpenWorldRuntimeFactRef `json:"state_source_fact,omitempty"`
	ScheduleFact                  CityOpenWorldRuntimeFactRef  `json:"schedule_fact"`
	BaselineCapacityUnitsPerTick  int64                        `json:"baseline_capacity_units_per_tick"`
	CapacityMilli                 int64                        `json:"capacity_milli"`
	EffectiveCapacityUnitsPerTick int64                        `json:"effective_capacity_units_per_tick"`
	AllocatedUnits                int64                        `json:"allocated_units"`
	OccupancyMilli                int                          `json:"occupancy_milli"`
	DelayTicks                    int64                        `json:"delay_ticks"`
	Metadata                      json.RawMessage              `json:"metadata"`
}

type CityOpenWorldEffectiveCapacityState struct {
	Policy     CityOpenWorldEffectiveCapacityPolicy      `json:"policy"`
	Admissions []CityOpenWorldEffectiveCapacityAdmission `json:"admissions"`
}

// CityOpenWorldEffectiveCapacityEdge is a read-model and scheduling input.
// It is derived from V9/V19/V20 and therefore intentionally not stored in
// canonical V21 state.
type CityOpenWorldEffectiveCapacityEdge struct {
	EdgeCode                      string                       `json:"edge_code"`
	CorridorCode                  string                       `json:"corridor_code"`
	AssetCode                     string                       `json:"asset_code"`
	AssetState                    string                       `json:"asset_state"`
	StateEffectiveTick            int64                        `json:"state_effective_tick"`
	StateSourceFact               *CityOpenWorldRuntimeFactRef `json:"state_source_fact,omitempty"`
	BaselineCapacityUnitsPerTick  int64                        `json:"baseline_capacity_units_per_tick"`
	CapacityMilli                 int64                        `json:"capacity_milli"`
	EffectiveCapacityUnitsPerTick int64                        `json:"effective_capacity_units_per_tick"`
}

// cityOpenWorldEffectiveCapacityAllocationMetadata is copied into a V9
// allocation only when that allocation was admitted through V21.  Keeping the
// marker in the V9 evidence makes a snapshot self-describing without turning
// V21 into a second reservation ledger.
type cityOpenWorldEffectiveCapacityAllocationMetadata struct {
	SchemaVersion                 int                          `json:"schema_version"`
	AllocationContract            string                       `json:"allocation_contract"`
	CapacityContract              string                       `json:"capacity_contract"`
	CorridorCode                  string                       `json:"corridor_code"`
	AssetCode                     string                       `json:"asset_code"`
	AssetState                    string                       `json:"asset_state"`
	StateEffectiveTick            int64                        `json:"state_effective_tick"`
	StateSourceFact               *CityOpenWorldRuntimeFactRef `json:"state_source_fact,omitempty"`
	BaselineCapacityUnitsPerTick  int64                        `json:"baseline_capacity_units_per_tick"`
	CapacityMilli                 int64                        `json:"capacity_milli"`
	EffectiveCapacityUnitsPerTick int64                        `json:"effective_capacity_units_per_tick"`
}

type cityOpenWorldEffectiveCapacitySchedulingState struct {
	policy            *CityOpenWorldEffectiveCapacityPolicy
	edges             map[string]CityOpenWorldEffectiveCapacityEdge
	allocatedByEdge   map[string]int64
	admissionsWritten int64
}

func cityOpenWorldEffectiveCapacityPolicyHash() (string, error) {
	raw, err := json.Marshal(struct {
		SchemaVersion      int    `json:"schema_version"`
		ProfileID          string `json:"profile_id"`
		ProfileVersion     string `json:"profile_version"`
		TopologyContract   string `json:"topology_contract"`
		AssetContract      string `json:"asset_contract"`
		AdmissionContract  string `json:"admission_contract"`
		VisibilityContract string `json:"visibility_contract"`
		MaximumAdmissions  int    `json:"maximum_admissions"`
	}{
		SchemaVersion: cityOpenWorldEffectiveCapacitySchemaVersion,
		ProfileID:     cityOpenWorldEffectiveCapacityProfileID, ProfileVersion: cityOpenWorldEffectiveCapacityProfileVersion,
		TopologyContract:   cityOpenWorldEffectiveCapacityTopologyContract,
		AssetContract:      cityOpenWorldEffectiveCapacityAssetContract,
		AdmissionContract:  cityOpenWorldEffectiveCapacityAdmissionContract,
		VisibilityContract: cityOpenWorldEffectiveCapacityVisibilityContract,
		MaximumAdmissions:  cityOpenWorldEffectiveCapacityMaximumAdmissions,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func cityOpenWorldEffectiveCapacityPolicyMetadata() (json.RawMessage, error) {
	return json.Marshal(map[string]any{
		"schema_version":      cityOpenWorldEffectiveCapacitySchemaVersion,
		"scope":               "v20_corridor_assets_to_v9_future_admission_only",
		"topology_contract":   cityOpenWorldEffectiveCapacityTopologyContract,
		"asset_contract":      cityOpenWorldEffectiveCapacityAssetContract,
		"admission_contract":  cityOpenWorldEffectiveCapacityAdmissionContract,
		"visibility_contract": cityOpenWorldEffectiveCapacityVisibilityContract,
	})
}

func cityOpenWorldEffectiveCapacityAdmissionMetadata() (json.RawMessage, error) {
	return json.Marshal(map[string]any{
		"schema_version":      cityOpenWorldEffectiveCapacitySchemaVersion,
		"admission_contract":  cityOpenWorldEffectiveCapacityAdmissionContract,
		"topology_contract":   cityOpenWorldEffectiveCapacityTopologyContract,
		"asset_contract":      cityOpenWorldEffectiveCapacityAssetContract,
		"visibility_contract": cityOpenWorldEffectiveCapacityVisibilityContract,
	})
}

func cityOpenWorldEffectiveCapacityPolicyMetadataValid(raw json.RawMessage) bool {
	var metadata struct {
		SchemaVersion      int    `json:"schema_version"`
		Scope              string `json:"scope"`
		TopologyContract   string `json:"topology_contract"`
		AssetContract      string `json:"asset_contract"`
		AdmissionContract  string `json:"admission_contract"`
		VisibilityContract string `json:"visibility_contract"`
	}
	return json.Unmarshal(raw, &metadata) == nil &&
		metadata.SchemaVersion == cityOpenWorldEffectiveCapacitySchemaVersion &&
		metadata.Scope == "v20_corridor_assets_to_v9_future_admission_only" &&
		metadata.TopologyContract == cityOpenWorldEffectiveCapacityTopologyContract &&
		metadata.AssetContract == cityOpenWorldEffectiveCapacityAssetContract &&
		metadata.AdmissionContract == cityOpenWorldEffectiveCapacityAdmissionContract &&
		metadata.VisibilityContract == cityOpenWorldEffectiveCapacityVisibilityContract
}

func cityOpenWorldEffectiveCapacityAdmissionMetadataValid(raw json.RawMessage) bool {
	var metadata struct {
		SchemaVersion      int    `json:"schema_version"`
		AdmissionContract  string `json:"admission_contract"`
		TopologyContract   string `json:"topology_contract"`
		AssetContract      string `json:"asset_contract"`
		VisibilityContract string `json:"visibility_contract"`
	}
	return json.Unmarshal(raw, &metadata) == nil &&
		metadata.SchemaVersion == cityOpenWorldEffectiveCapacitySchemaVersion &&
		metadata.AdmissionContract == cityOpenWorldEffectiveCapacityAdmissionContract &&
		metadata.TopologyContract == cityOpenWorldEffectiveCapacityTopologyContract &&
		metadata.AssetContract == cityOpenWorldEffectiveCapacityAssetContract &&
		metadata.VisibilityContract == cityOpenWorldEffectiveCapacityVisibilityContract
}

func cityOpenWorldEffectiveCapacityAllocationMetadataFor(
	edge CityOpenWorldEffectiveCapacityEdge,
) (json.RawMessage, error) {
	return json.Marshal(cityOpenWorldEffectiveCapacityAllocationMetadata{
		SchemaVersion:                 cityOpenWorldEffectiveCapacitySchemaVersion,
		AllocationContract:            "edge_departure_tick",
		CapacityContract:              cityOpenWorldEffectiveCapacityAdmissionContract,
		CorridorCode:                  edge.CorridorCode,
		AssetCode:                     edge.AssetCode,
		AssetState:                    edge.AssetState,
		StateEffectiveTick:            edge.StateEffectiveTick,
		StateSourceFact:               edge.StateSourceFact,
		BaselineCapacityUnitsPerTick:  edge.BaselineCapacityUnitsPerTick,
		CapacityMilli:                 edge.CapacityMilli,
		EffectiveCapacityUnitsPerTick: edge.EffectiveCapacityUnitsPerTick,
	})
}

// cityOpenWorldEffectiveCapacityAllocationMetadataFromRaw distinguishes an
// original V9 allocation from a V21-admitted allocation.  A malformed marker
// is never silently treated as a legacy allocation.
func cityOpenWorldEffectiveCapacityAllocationMetadataFromRaw(
	raw json.RawMessage,
) (cityOpenWorldEffectiveCapacityAllocationMetadata, bool, error) {
	marker := struct {
		CapacityContract *string `json:"capacity_contract"`
	}{}
	if err := json.Unmarshal(raw, &marker); err != nil {
		return cityOpenWorldEffectiveCapacityAllocationMetadata{}, false, err
	}
	if marker.CapacityContract == nil {
		return cityOpenWorldEffectiveCapacityAllocationMetadata{}, false, nil
	}
	metadata := cityOpenWorldEffectiveCapacityAllocationMetadata{}
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return metadata, true, err
	}
	if metadata.SchemaVersion != cityOpenWorldEffectiveCapacitySchemaVersion ||
		metadata.AllocationContract != "edge_departure_tick" ||
		metadata.CapacityContract != cityOpenWorldEffectiveCapacityAdmissionContract ||
		!worldRuntimeCodeValid(metadata.CorridorCode, 160) ||
		!worldRuntimeCodeValid(metadata.AssetCode, 160) ||
		!cityOpenWorldInfrastructureStateCapacityValid(metadata.AssetState, metadata.CapacityMilli) ||
		metadata.StateEffectiveTick < 0 || metadata.BaselineCapacityUnitsPerTick < 1 ||
		metadata.EffectiveCapacityUnitsPerTick < 1 ||
		metadata.EffectiveCapacityUnitsPerTick > metadata.BaselineCapacityUnitsPerTick {
		return metadata, true, fmt.Errorf("invalid V21 allocation capacity metadata")
	}
	if metadata.StateSourceFact != nil &&
		(metadata.StateSourceFact.Tick != metadata.StateEffectiveTick || metadata.StateSourceFact.Sequence < 1) {
		return metadata, true, fmt.Errorf("invalid V21 allocation state source metadata")
	}
	expected, err := cityOpenWorldEffectiveCapacityUnits(metadata.BaselineCapacityUnitsPerTick, metadata.CapacityMilli)
	if err != nil || expected != metadata.EffectiveCapacityUnitsPerTick {
		return metadata, true, fmt.Errorf("invalid V21 allocation capacity formula")
	}
	return metadata, true, nil
}

func cityOpenWorldEffectiveCapacityUnits(baseCapacity, capacityMilli int64) (int64, error) {
	if baseCapacity < 1 || capacityMilli < 0 || capacityMilli > 1_000 {
		return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v21_effective_capacity_formula"})
	}
	if capacityMilli == 0 {
		return 0, nil
	}
	whole := baseCapacity / 1_000
	remainder := baseCapacity % 1_000
	if whole > 0 && whole > math.MaxInt64/capacityMilli {
		return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v21_effective_capacity_overflow"})
	}
	result := whole * capacityMilli
	if remainder > 0 && remainder > math.MaxInt64/capacityMilli {
		return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v21_effective_capacity_overflow"})
	}
	partial := remainder * capacityMilli / 1_000
	if result > math.MaxInt64-partial {
		return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v21_effective_capacity_overflow"})
	}
	return result + partial, nil
}

func buildCityOpenWorldEffectiveCapacityEdges(
	mobility *CityOpenWorldMobilityState,
	network *CityOpenWorldSpatialNetworkState,
	infrastructure *CityOpenWorldInfrastructureState,
) (map[string]CityOpenWorldEffectiveCapacityEdge, error) {
	if err := validateCityOpenWorldMobilityState(mobility); err != nil {
		return nil, fmt.Errorf("validate V21 mobility prerequisite: %w", err)
	}
	if err := validateCityOpenWorldSpatialNetworkState(network); err != nil {
		return nil, fmt.Errorf("validate V21 spatial-network prerequisite: %w", err)
	}
	if err := validateCityOpenWorldInfrastructureState(infrastructure); err != nil {
		return nil, fmt.Errorf("validate V21 infrastructure prerequisite: %w", err)
	}
	corridorByEdge := make(map[string]CityOpenWorldSpatialNetworkCorridor, len(network.Corridors))
	for _, corridor := range network.Corridors {
		corridorByEdge[corridor.EdgeCode] = corridor
	}
	assetByCorridor := make(map[string]CityOpenWorldInfrastructureAsset, len(infrastructure.Assets))
	for _, asset := range infrastructure.Assets {
		if asset.AssetKind == cityOpenWorldInfrastructureAssetKindSegment && asset.SpatialCorridorCode != nil && asset.SegmentOrdinal == 1 {
			assetByCorridor[*asset.SpatialCorridorCode] = asset
		}
	}
	stateByAsset := make(map[string]CityOpenWorldInfrastructureAssetState, len(infrastructure.States))
	for _, state := range infrastructure.States {
		stateByAsset[state.AssetCode] = state
	}
	result := make(map[string]CityOpenWorldEffectiveCapacityEdge, len(mobility.Edges))
	for _, edge := range mobility.Edges {
		corridor, found := corridorByEdge[edge.Code]
		if !found || corridor.CapacityUnitsPerTick != edge.CapacityUnitsPerTick {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v21_corridor_mapping"})
		}
		asset, found := assetByCorridor[corridor.Code]
		if !found {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v21_asset_mapping"})
		}
		assetState, found := stateByAsset[asset.Code]
		if !found || !cityOpenWorldInfrastructureStateCapacityValid(assetState.State, assetState.CapacityMilli) {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v21_asset_state"})
		}
		effectiveCapacity, capacityErr := cityOpenWorldEffectiveCapacityUnits(edge.CapacityUnitsPerTick, assetState.CapacityMilli)
		if capacityErr != nil {
			return nil, capacityErr
		}
		result[edge.Code] = CityOpenWorldEffectiveCapacityEdge{
			EdgeCode: edge.Code, CorridorCode: corridor.Code, AssetCode: asset.Code,
			AssetState: assetState.State, StateEffectiveTick: assetState.EffectiveTick,
			StateSourceFact:               assetState.SourceFact,
			BaselineCapacityUnitsPerTick:  edge.CapacityUnitsPerTick,
			CapacityMilli:                 assetState.CapacityMilli,
			EffectiveCapacityUnitsPerTick: effectiveCapacity,
		}
	}
	if len(result) != len(mobility.Edges) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v21_edge_coverage"})
	}
	return result, nil
}

func cityOpenWorldEffectiveCapacityStateAtSchedule(
	infrastructure *CityOpenWorldInfrastructureState,
	assetCode string,
	scheduleFact CityOpenWorldRuntimeFactRef,
) (CityOpenWorldInfrastructureAssetState, error) {
	if infrastructure == nil || scheduleFact.Tick < 0 || scheduleFact.Sequence < 1 {
		return CityOpenWorldInfrastructureAssetState{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v21_state_timeline"})
	}
	var chosen *CityOpenWorldInfrastructureAssetTransition
	for index := range infrastructure.Transitions {
		transition := &infrastructure.Transitions[index]
		if transition.AssetCode != assetCode {
			continue
		}
		visible := transition.TransitionTick < scheduleFact.Tick ||
			(transition.TransitionTick == scheduleFact.Tick && transition.TransitionSeq < scheduleFact.Sequence)
		if !visible {
			continue
		}
		if chosen == nil || transition.TransitionTick > chosen.TransitionTick ||
			(transition.TransitionTick == chosen.TransitionTick && transition.TransitionSeq > chosen.TransitionSeq) {
			chosen = transition
		}
	}
	if chosen == nil {
		return CityOpenWorldInfrastructureAssetState{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v21_state_timeline_missing"})
	}
	return CityOpenWorldInfrastructureAssetState{
		AssetCode: assetCode, State: chosen.ToState, CapacityMilli: chosen.CapacityMilli,
		EffectiveTick: chosen.TransitionTick, SourceFact: chosen.SourceFact,
		Version: 0, Metadata: append(json.RawMessage(nil), chosen.Metadata...),
	}, nil
}

func cityOpenWorldEffectiveCapacityFactRefsEqual(
	left, right *CityOpenWorldRuntimeFactRef,
) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func loadCityOpenWorldEffectiveCapacitySchedulingState(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	mobility *CityOpenWorldMobilityState,
) (*cityOpenWorldEffectiveCapacitySchedulingState, error) {
	if mobility == nil || targetTick < 1 {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v21_scheduler_input"})
	}
	effectiveCapacity, err := loadCityOpenWorldEffectiveCapacityState(ctx, tx, worldID)
	if err != nil {
		return nil, err
	}
	policy := &effectiveCapacity.Policy
	if targetTick <= policy.BaselineTick {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v21_scheduler_tick"})
	}
	network, err := loadCityOpenWorldSpatialNetworkState(ctx, tx, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V21 scheduler spatial-network state: %w", err)
	}
	infrastructure, err := loadCityOpenWorldInfrastructureState(ctx, tx, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V21 scheduler infrastructure state: %w", err)
	}
	edges, err := buildCityOpenWorldEffectiveCapacityEdges(mobility, network, infrastructure)
	if err != nil {
		return nil, err
	}
	allocated := make(map[string]int64, len(edges))
	for _, allocation := range mobility.Allocations {
		if allocation.DepartureTick != targetTick {
			continue
		}
		if _, exists := edges[allocation.EdgeCode]; !exists {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v21_scheduler_allocation_edge"})
		}
		if allocation.AllocatedUnits > math.MaxInt64-allocated[allocation.EdgeCode] {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v21_scheduler_allocation_overflow"})
		}
		allocated[allocation.EdgeCode] += allocation.AllocatedUnits
	}
	return &cityOpenWorldEffectiveCapacitySchedulingState{
		policy: policy, edges: edges, allocatedByEdge: allocated,
	}, nil
}

func (state *cityOpenWorldEffectiveCapacitySchedulingState) edgeAvailable(
	edge CityOpenWorldMobilityEdge,
	requestedUnits int64,
) bool {
	if state == nil || requestedUnits < 1 {
		return false
	}
	capacity, found := state.edges[edge.Code]
	if !found || capacity.EffectiveCapacityUnitsPerTick < requestedUnits {
		return false
	}
	used := state.allocatedByEdge[edge.Code]
	return used >= 0 && used <= capacity.EffectiveCapacityUnitsPerTick-requestedUnits
}

func (state *cityOpenWorldEffectiveCapacitySchedulingState) reserve(
	edge CityOpenWorldMobilityEdge,
	requestedUnits int64,
) (CityOpenWorldEffectiveCapacityEdge, int64, error) {
	if state == nil || requestedUnits < 1 {
		return CityOpenWorldEffectiveCapacityEdge{}, 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v21_scheduler_reservation"})
	}
	capacity, found := state.edges[edge.Code]
	if !found || capacity.EffectiveCapacityUnitsPerTick < requestedUnits {
		return CityOpenWorldEffectiveCapacityEdge{}, 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v21_scheduler_edge"})
	}
	used := state.allocatedByEdge[edge.Code]
	if used < 0 || used > capacity.EffectiveCapacityUnitsPerTick-requestedUnits {
		return CityOpenWorldEffectiveCapacityEdge{}, 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v21_scheduler_capacity"})
	}
	next := used + requestedUnits
	state.allocatedByEdge[edge.Code] = next
	return capacity, next, nil
}

func (state *cityOpenWorldEffectiveCapacitySchedulingState) release(
	edgeCode string,
	requestedUnits int64,
) error {
	if state == nil || requestedUnits < 1 || state.allocatedByEdge[edgeCode] < requestedUnits {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v21_scheduler_release"})
	}
	state.allocatedByEdge[edgeCode] -= requestedUnits
	return nil
}

func activateCityOpenWorldEffectiveCapacityBootstrapWrite(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if _, err := tx.ExecContext(ctx, `
SELECT set_config('sub2api.city_open_world_effective_capacity_bootstrap_world_id', $1, TRUE)`, fmt.Sprintf("%d", worldID)); err != nil {
		return fmt.Errorf("enable open-world V21 effective-capacity bootstrap: %w", err)
	}
	return nil
}

func activateCityOpenWorldEffectiveCapacityWrite(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if _, err := tx.ExecContext(ctx, `
SELECT set_config('sub2api.city_open_world_effective_capacity_world_id', $1, TRUE)`, fmt.Sprintf("%d", worldID)); err != nil {
		return fmt.Errorf("enable open-world V21 effective-capacity write: %w", err)
	}
	return nil
}

func activateCityOpenWorldEffectiveCapacityRecoveryWrite(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if _, err := tx.ExecContext(ctx, `
SELECT set_config('sub2api.city_open_world_effective_capacity_recovery_world_id', $1, TRUE)`, fmt.Sprintf("%d", worldID)); err != nil {
		return fmt.Errorf("enable open-world V21 effective-capacity recovery: %w", err)
	}
	return nil
}

func assertCityOpenWorldEffectiveCapacityFoundation(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if _, err := tx.ExecContext(ctx, `SELECT assert_city_open_world_effective_capacity_foundation($1)`, worldID); err != nil {
		return fmt.Errorf("validate V21 effective-capacity foundation: %w", err)
	}
	return nil
}

func initializeCityOpenWorldV21EffectiveCapacityFoundation(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if worldID <= 0 {
		return ErrCityInvalidInput
	}
	var simulationVersion string
	var baselineTick int64
	if err := tx.QueryRowContext(ctx, `
SELECT simulation_version, current_tick
FROM city_worlds
WHERE id = $1
FOR UPDATE`, worldID).Scan(&simulationVersion, &baselineTick); err != nil {
		return fmt.Errorf("lock V21 effective-capacity world: %w", err)
	}
	if !cityEngineSupportsOpenWorldEffectiveCapacity(simulationVersion) || baselineTick < 0 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v21_effective_capacity_world"})
	}
	if err := assertCityOpenWorldInfrastructureFoundation(ctx, tx, worldID); err != nil {
		return fmt.Errorf("validate V21 V20 infrastructure prerequisite: %w", err)
	}
	mobility, err := loadCityOpenWorldMobilityState(ctx, tx, worldID)
	if err != nil {
		return fmt.Errorf("load V21 mobility prerequisite: %w", err)
	}
	network, err := loadCityOpenWorldSpatialNetworkState(ctx, tx, worldID)
	if err != nil {
		return fmt.Errorf("load V21 spatial-network prerequisite: %w", err)
	}
	infrastructure, err := loadCityOpenWorldInfrastructureState(ctx, tx, worldID)
	if err != nil {
		return fmt.Errorf("load V21 infrastructure prerequisite: %w", err)
	}
	if _, err = buildCityOpenWorldEffectiveCapacityEdges(mobility, network, infrastructure); err != nil {
		return err
	}
	contentHash, err := cityOpenWorldEffectiveCapacityPolicyHash()
	if err != nil {
		return fmt.Errorf("hash V21 effective-capacity policy: %w", err)
	}
	metadata, err := cityOpenWorldEffectiveCapacityPolicyMetadata()
	if err != nil {
		return fmt.Errorf("marshal V21 effective-capacity policy metadata: %w", err)
	}
	if err = activateCityOpenWorldEffectiveCapacityBootstrapWrite(ctx, tx, worldID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_effective_capacity_profiles
    (world_id, profile_id, profile_version, content_hash, baseline_tick,
     topology_contract, asset_contract, admission_contract, visibility_contract,
     maximum_admissions, admission_count, revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 0, 1, $11::jsonb)`,
		worldID, cityOpenWorldEffectiveCapacityProfileID, cityOpenWorldEffectiveCapacityProfileVersion,
		contentHash, baselineTick, cityOpenWorldEffectiveCapacityTopologyContract,
		cityOpenWorldEffectiveCapacityAssetContract, cityOpenWorldEffectiveCapacityAdmissionContract,
		cityOpenWorldEffectiveCapacityVisibilityContract, cityOpenWorldEffectiveCapacityMaximumAdmissions,
		[]byte(metadata)); err != nil {
		return fmt.Errorf("insert V21 effective-capacity profile: %w", err)
	}
	return assertCityOpenWorldEffectiveCapacityFoundation(ctx, tx, worldID)
}

func loadCityOpenWorldEffectiveCapacityPolicy(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*CityOpenWorldEffectiveCapacityPolicy, error) {
	item := &CityOpenWorldEffectiveCapacityPolicy{}
	err := queryer.QueryRowContext(ctx, `
SELECT profile_id, profile_version, content_hash, baseline_tick, topology_contract,
       asset_contract, admission_contract, visibility_contract, maximum_admissions,
       admission_count, revision, metadata
FROM city_open_world_effective_capacity_profiles
WHERE world_id = $1`, worldID).Scan(
		&item.ProfileID, &item.ProfileVersion, &item.ContentHash, &item.BaselineTick,
		&item.TopologyContract, &item.AssetContract, &item.AdmissionContract,
		&item.VisibilityContract, &item.MaximumAdmissions, &item.AdmissionCount,
		&item.Revision, &item.Metadata,
	)
	if err == sql.ErrNoRows {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v21_effective_capacity_profile"})
	}
	if err != nil {
		return nil, fmt.Errorf("load V21 effective-capacity profile: %w", err)
	}
	return item, nil
}

func loadCityOpenWorldEffectiveCapacityState(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*CityOpenWorldEffectiveCapacityState, error) {
	policy, err := loadCityOpenWorldEffectiveCapacityPolicy(ctx, queryer, worldID)
	if err != nil {
		return nil, err
	}
	state := &CityOpenWorldEffectiveCapacityState{Policy: *policy, Admissions: make([]CityOpenWorldEffectiveCapacityAdmission, 0)}
	rows, err := queryer.QueryContext(ctx, `
SELECT route.code, admission.edge_code, admission.departure_tick,
       admission.corridor_code, admission.asset_code, admission.asset_state,
       admission.state_effective_tick, state_fact.tick, state_fact.sequence,
       schedule_fact.tick, schedule_fact.sequence,
       admission.baseline_capacity_units_per_tick, admission.capacity_milli,
       admission.effective_capacity_units_per_tick, admission.allocated_units,
       admission.occupancy_milli, admission.delay_ticks, admission.metadata
FROM city_open_world_effective_capacity_admissions admission
JOIN city_open_world_mobility_routes route
  ON route.id = admission.route_id AND route.world_id = admission.world_id
LEFT JOIN city_open_world_runtime_facts state_fact
  ON state_fact.id = admission.state_source_fact_id AND state_fact.world_id = admission.world_id
JOIN city_open_world_runtime_facts schedule_fact
  ON schedule_fact.id = admission.schedule_fact_id AND schedule_fact.world_id = admission.world_id
WHERE admission.world_id = $1
ORDER BY admission.departure_tick ASC, schedule_fact.sequence ASC, route.code ASC, admission.edge_code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V21 effective-capacity admissions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		item := CityOpenWorldEffectiveCapacityAdmission{}
		var stateFactTick, stateFactSequence sql.NullInt64
		if err = rows.Scan(
			&item.RouteCode, &item.EdgeCode, &item.DepartureTick, &item.CorridorCode,
			&item.AssetCode, &item.AssetState, &item.StateEffectiveTick,
			&stateFactTick, &stateFactSequence, &item.ScheduleFact.Tick, &item.ScheduleFact.Sequence,
			&item.BaselineCapacityUnitsPerTick, &item.CapacityMilli,
			&item.EffectiveCapacityUnitsPerTick, &item.AllocatedUnits,
			&item.OccupancyMilli, &item.DelayTicks, &item.Metadata,
		); err != nil {
			return nil, fmt.Errorf("scan V21 effective-capacity admission: %w", err)
		}
		if stateFactTick.Valid != stateFactSequence.Valid {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v21_state_source_fact"})
		}
		if stateFactTick.Valid {
			item.StateSourceFact = &CityOpenWorldRuntimeFactRef{Tick: stateFactTick.Int64, Sequence: stateFactSequence.Int64}
		}
		state.Admissions = append(state.Admissions, item)
	}
	if err = closeCityRows(rows, "iterate V21 effective-capacity admissions"); err != nil {
		return nil, err
	}
	if err = validateCityOpenWorldEffectiveCapacityState(state); err != nil {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v21_effective_capacity_state"}).WithCause(err)
	}
	return state, nil
}

func validateCityOpenWorldEffectiveCapacityState(state *CityOpenWorldEffectiveCapacityState) error {
	if state == nil {
		return fmt.Errorf("V21 effective-capacity state is unavailable")
	}
	policy := state.Policy
	expectedHash, err := cityOpenWorldEffectiveCapacityPolicyHash()
	if err != nil || policy.ProfileID != cityOpenWorldEffectiveCapacityProfileID ||
		policy.ProfileVersion != cityOpenWorldEffectiveCapacityProfileVersion ||
		policy.ContentHash != expectedHash || policy.BaselineTick < 0 ||
		policy.TopologyContract != cityOpenWorldEffectiveCapacityTopologyContract ||
		policy.AssetContract != cityOpenWorldEffectiveCapacityAssetContract ||
		policy.AdmissionContract != cityOpenWorldEffectiveCapacityAdmissionContract ||
		policy.VisibilityContract != cityOpenWorldEffectiveCapacityVisibilityContract ||
		policy.MaximumAdmissions != cityOpenWorldEffectiveCapacityMaximumAdmissions ||
		policy.AdmissionCount != int64(len(state.Admissions)) || policy.AdmissionCount < 0 ||
		policy.AdmissionCount > int64(policy.MaximumAdmissions) || policy.Revision != policy.AdmissionCount+1 ||
		!cityOpenWorldEffectiveCapacityPolicyMetadataValid(policy.Metadata) {
		return fmt.Errorf("invalid V21 effective-capacity policy")
	}
	seen := make(map[string]struct{}, len(state.Admissions))
	for _, admission := range state.Admissions {
		key := admission.RouteCode + "\x00" + admission.EdgeCode
		if _, duplicate := seen[key]; duplicate || !worldRuntimeCodeValid(admission.RouteCode, 160) ||
			!worldRuntimeCodeValid(admission.EdgeCode, 160) || !worldRuntimeCodeValid(admission.CorridorCode, 160) ||
			!worldRuntimeCodeValid(admission.AssetCode, 160) || admission.DepartureTick <= policy.BaselineTick ||
			admission.StateEffectiveTick < 0 || admission.ScheduleFact.Tick != admission.DepartureTick ||
			admission.ScheduleFact.Sequence < 1 || admission.BaselineCapacityUnitsPerTick < 1 ||
			admission.CapacityMilli < 0 || admission.CapacityMilli > 1_000 ||
			admission.EffectiveCapacityUnitsPerTick < 1 ||
			admission.EffectiveCapacityUnitsPerTick > admission.BaselineCapacityUnitsPerTick ||
			admission.AllocatedUnits < 1 || admission.AllocatedUnits > admission.EffectiveCapacityUnitsPerTick ||
			admission.OccupancyMilli < 1 || admission.OccupancyMilli > 1_000 || admission.DelayTicks < 0 ||
			!cityOpenWorldInfrastructureStateCapacityValid(admission.AssetState, admission.CapacityMilli) ||
			!cityOpenWorldEffectiveCapacityAdmissionMetadataValid(admission.Metadata) {
			return fmt.Errorf("invalid V21 effective-capacity admission %s/%s", admission.RouteCode, admission.EdgeCode)
		}
		if admission.StateSourceFact != nil && (admission.StateSourceFact.Tick != admission.StateEffectiveTick || admission.StateSourceFact.Sequence < 1) {
			return fmt.Errorf("invalid V21 effective-capacity state fact %s/%s", admission.RouteCode, admission.EdgeCode)
		}
		expected, capacityErr := cityOpenWorldEffectiveCapacityUnits(admission.BaselineCapacityUnitsPerTick, admission.CapacityMilli)
		if capacityErr != nil || expected != admission.EffectiveCapacityUnitsPerTick {
			return fmt.Errorf("invalid V21 effective-capacity formula %s/%s", admission.RouteCode, admission.EdgeCode)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func sortCityOpenWorldEffectiveCapacityState(state *CityOpenWorldEffectiveCapacityState) {
	if state == nil {
		return
	}
	sort.Slice(state.Admissions, func(i, j int) bool {
		if state.Admissions[i].DepartureTick != state.Admissions[j].DepartureTick {
			return state.Admissions[i].DepartureTick < state.Admissions[j].DepartureTick
		}
		if state.Admissions[i].ScheduleFact.Sequence != state.Admissions[j].ScheduleFact.Sequence {
			return state.Admissions[i].ScheduleFact.Sequence < state.Admissions[j].ScheduleFact.Sequence
		}
		if state.Admissions[i].RouteCode != state.Admissions[j].RouteCode {
			return state.Admissions[i].RouteCode < state.Admissions[j].RouteCode
		}
		return state.Admissions[i].EdgeCode < state.Admissions[j].EdgeCode
	})
}

func cityOpenWorldEffectiveCapacityStateFactID(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	assetCode string,
	expected *CityOpenWorldRuntimeFactRef,
) (any, error) {
	var sourceFactID sql.NullInt64
	var factTick, factSequence sql.NullInt64
	if err := tx.QueryRowContext(ctx, `
SELECT state.source_fact_id, fact.tick, fact.sequence
FROM city_open_world_infrastructure_asset_states state
LEFT JOIN city_open_world_runtime_facts fact
  ON fact.id = state.source_fact_id AND fact.world_id = state.world_id
WHERE state.world_id = $1 AND state.asset_code = $2`, worldID, assetCode).Scan(
		&sourceFactID, &factTick, &factSequence,
	); err != nil {
		return nil, fmt.Errorf("load V21 effective-capacity asset source fact %s: %w", assetCode, err)
	}
	if sourceFactID.Valid != factTick.Valid || factTick.Valid != factSequence.Valid {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v21_current_asset_source_fact"})
	}
	if expected == nil {
		if sourceFactID.Valid {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v21_current_asset_source_unexpected"})
		}
		return nil, nil
	}
	if !sourceFactID.Valid || factTick.Int64 != expected.Tick || factSequence.Int64 != expected.Sequence {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v21_current_asset_source_mismatch"})
	}
	return sourceFactID.Int64, nil
}

func recordCityOpenWorldEffectiveCapacityAdmissions(
	ctx context.Context,
	tx *sql.Tx,
	worldID, routeID, scheduleFactID int64,
	scheduleFact CityOpenWorldRuntimeFactRef,
	allocations []CityOpenWorldMobilityAllocation,
	scheduling *cityOpenWorldEffectiveCapacitySchedulingState,
) error {
	if scheduling == nil || scheduling.policy == nil || routeID <= 0 || scheduleFactID <= 0 ||
		scheduleFact.Tick <= scheduling.policy.BaselineTick || scheduleFact.Sequence < 1 || len(allocations) == 0 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v21_admission_input"})
	}
	if scheduling.policy.AdmissionCount > int64(scheduling.policy.MaximumAdmissions-len(allocations)) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v21_admission_limit"})
	}
	if err := activateCityOpenWorldEffectiveCapacityWrite(ctx, tx, worldID); err != nil {
		return err
	}
	admissionMetadata, err := cityOpenWorldEffectiveCapacityAdmissionMetadata()
	if err != nil {
		return fmt.Errorf("marshal V21 effective-capacity admission metadata: %w", err)
	}
	for _, allocation := range allocations {
		capacity, found := scheduling.edges[allocation.EdgeCode]
		if !found || allocation.DepartureTick != scheduleFact.Tick ||
			allocation.AllocatedUnits < 1 || allocation.CapacityUnitsPerTick != capacity.EffectiveCapacityUnitsPerTick ||
			allocation.AllocatedUnits > capacity.EffectiveCapacityUnitsPerTick {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v21_admission_allocation"})
		}
		sourceFactID, sourceErr := cityOpenWorldEffectiveCapacityStateFactID(
			ctx, tx, worldID, capacity.AssetCode, capacity.StateSourceFact,
		)
		if sourceErr != nil {
			return sourceErr
		}
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_effective_capacity_admissions
    (world_id, route_id, edge_code, departure_tick, corridor_code, asset_code,
     asset_state, state_effective_tick, state_source_fact_id, schedule_fact_id,
     baseline_capacity_units_per_tick, capacity_milli,
     effective_capacity_units_per_tick, allocated_units, occupancy_milli,
     delay_ticks, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17::jsonb)`,
			worldID, routeID, allocation.EdgeCode, allocation.DepartureTick,
			capacity.CorridorCode, capacity.AssetCode, capacity.AssetState,
			capacity.StateEffectiveTick, sourceFactID, scheduleFactID,
			capacity.BaselineCapacityUnitsPerTick, capacity.CapacityMilli,
			capacity.EffectiveCapacityUnitsPerTick, allocation.AllocatedUnits,
			allocation.OccupancyMilli, allocation.DelayTicks, []byte(admissionMetadata)); err != nil {
			return fmt.Errorf("insert V21 effective-capacity admission %s: %w", allocation.EdgeCode, err)
		}
		if _, err = tx.ExecContext(ctx, `
UPDATE city_open_world_effective_capacity_profiles
SET admission_count = admission_count + 1,
    revision = revision + 1,
    updated_at = NOW()
WHERE world_id = $1`, worldID); err != nil {
			return fmt.Errorf("update V21 effective-capacity profile: %w", err)
		}
		scheduling.policy.AdmissionCount++
		scheduling.policy.Revision++
		scheduling.admissionsWritten++
	}
	return nil
}

func cityOpenWorldEffectiveCapacityStaticCheckpointEqual(
	previous, checkpoint *CityOpenWorldEffectiveCapacityState,
) bool {
	if previous == nil || checkpoint == nil ||
		validateCityOpenWorldEffectiveCapacityState(previous) != nil ||
		validateCityOpenWorldEffectiveCapacityState(checkpoint) != nil {
		return false
	}
	left, right := previous.Policy, checkpoint.Policy
	return left.ProfileID == right.ProfileID && left.ProfileVersion == right.ProfileVersion &&
		left.ContentHash == right.ContentHash && left.BaselineTick == right.BaselineTick &&
		left.TopologyContract == right.TopologyContract && left.AssetContract == right.AssetContract &&
		left.AdmissionContract == right.AdmissionContract && left.VisibilityContract == right.VisibilityContract &&
		left.MaximumAdmissions == right.MaximumAdmissions && string(left.Metadata) == string(right.Metadata)
}

func validateCityOpenWorldEffectiveCapacityRuntimeState(runtime *cityOpenWorldRuntimeHashState) error {
	if runtime == nil || runtime.Mobility == nil || runtime.SpatialNetwork == nil ||
		runtime.Infrastructure == nil || runtime.EffectiveCapacity == nil {
		return fmt.Errorf("V21 effective-capacity runtime prerequisite is unavailable")
	}
	if err := validateCityOpenWorldMobilityState(runtime.Mobility); err != nil {
		return fmt.Errorf("validate V21 mobility state: %w", err)
	}
	if err := validateCityOpenWorldSpatialNetworkState(runtime.SpatialNetwork); err != nil {
		return fmt.Errorf("validate V21 spatial-network state: %w", err)
	}
	if err := validateCityOpenWorldInfrastructureState(runtime.Infrastructure); err != nil {
		return fmt.Errorf("validate V21 infrastructure state: %w", err)
	}
	if err := validateCityOpenWorldEffectiveCapacityState(runtime.EffectiveCapacity); err != nil {
		return fmt.Errorf("validate V21 effective-capacity state: %w", err)
	}
	if _, err := buildCityOpenWorldEffectiveCapacityEdges(
		runtime.Mobility, runtime.SpatialNetwork, runtime.Infrastructure,
	); err != nil {
		return fmt.Errorf("validate V21 current capacity mapping: %w", err)
	}

	policy := runtime.EffectiveCapacity.Policy
	edges := make(map[string]CityOpenWorldMobilityEdge, len(runtime.Mobility.Edges))
	for _, edge := range runtime.Mobility.Edges {
		edges[edge.Code] = edge
	}
	corridorByEdge := make(map[string]CityOpenWorldSpatialNetworkCorridor, len(runtime.SpatialNetwork.Corridors))
	for _, corridor := range runtime.SpatialNetwork.Corridors {
		corridorByEdge[corridor.EdgeCode] = corridor
	}
	assetByCorridor := make(map[string]CityOpenWorldInfrastructureAsset, len(runtime.Infrastructure.Assets))
	for _, asset := range runtime.Infrastructure.Assets {
		if asset.AssetKind == cityOpenWorldInfrastructureAssetKindSegment &&
			asset.SpatialCorridorCode != nil && asset.SegmentOrdinal == 1 {
			assetByCorridor[*asset.SpatialCorridorCode] = asset
		}
	}
	modes := cityOpenWorldMobilityModeByCode(runtime.Mobility.Modes)
	routes := make(map[string]CityOpenWorldMobilityRoute, len(runtime.Mobility.Routes))
	for _, route := range runtime.Mobility.Routes {
		routes[route.Code] = route
	}
	allocations := make(map[string]CityOpenWorldMobilityAllocation, len(runtime.Mobility.Allocations))
	for _, allocation := range runtime.Mobility.Allocations {
		key := allocation.RouteCode + "\x00" + allocation.EdgeCode
		if _, duplicate := allocations[key]; duplicate {
			return fmt.Errorf("V21 allocation evidence is duplicated for %s", key)
		}
		allocations[key] = allocation
	}
	facts := make(map[CityOpenWorldRuntimeFactRef]CityOpenWorldRuntimeFact, len(runtime.Facts))
	for _, fact := range runtime.Facts {
		identity := CityOpenWorldRuntimeFactRef{Tick: fact.Tick, Sequence: fact.Sequence}
		if _, duplicate := facts[identity]; duplicate {
			return fmt.Errorf("V21 runtime fact identity is duplicated")
		}
		facts[identity] = fact
	}
	admissions := make(map[string]CityOpenWorldEffectiveCapacityAdmission, len(runtime.EffectiveCapacity.Admissions))
	for _, admission := range runtime.EffectiveCapacity.Admissions {
		key := admission.RouteCode + "\x00" + admission.EdgeCode
		if _, duplicate := admissions[key]; duplicate {
			return fmt.Errorf("V21 admission evidence is duplicated for %s", key)
		}
		admissions[key] = admission
	}

	for key, allocation := range allocations {
		metadata, marked, metadataErr := cityOpenWorldEffectiveCapacityAllocationMetadataFromRaw(allocation.Metadata)
		if metadataErr != nil {
			return fmt.Errorf("V21 allocation metadata %s is invalid: %w", key, metadataErr)
		}
		admission, admitted := admissions[key]
		if allocation.DepartureTick <= policy.BaselineTick {
			if marked || admitted {
				return fmt.Errorf("pre-V21 allocation %s carries V21 admission evidence", key)
			}
			continue
		}
		if !marked || !admitted {
			return fmt.Errorf("post-V21 allocation %s lacks effective-capacity evidence", key)
		}
		if metadata.BaselineCapacityUnitsPerTick != admission.BaselineCapacityUnitsPerTick ||
			metadata.CapacityMilli != admission.CapacityMilli ||
			metadata.EffectiveCapacityUnitsPerTick != admission.EffectiveCapacityUnitsPerTick ||
			metadata.CorridorCode != admission.CorridorCode || metadata.AssetCode != admission.AssetCode ||
			metadata.AssetState != admission.AssetState || metadata.StateEffectiveTick != admission.StateEffectiveTick ||
			!cityOpenWorldEffectiveCapacityFactRefsEqual(metadata.StateSourceFact, admission.StateSourceFact) {
			return fmt.Errorf("V21 allocation metadata %s does not match admission", key)
		}
	}

	orderedAdmissions := append([]CityOpenWorldEffectiveCapacityAdmission(nil), runtime.EffectiveCapacity.Admissions...)
	sort.Slice(orderedAdmissions, func(i, j int) bool {
		left, right := orderedAdmissions[i], orderedAdmissions[j]
		if left.DepartureTick != right.DepartureTick {
			return left.DepartureTick < right.DepartureTick
		}
		if left.ScheduleFact.Sequence != right.ScheduleFact.Sequence {
			return left.ScheduleFact.Sequence < right.ScheduleFact.Sequence
		}
		if left.EdgeCode != right.EdgeCode {
			return left.EdgeCode < right.EdgeCode
		}
		return left.RouteCode < right.RouteCode
	})
	usedByDepartureEdge := make(map[string]int64, len(orderedAdmissions))
	for _, admission := range orderedAdmissions {
		key := admission.RouteCode + "\x00" + admission.EdgeCode
		allocation, allocationFound := allocations[key]
		route, routeFound := routes[admission.RouteCode]
		edge, edgeFound := edges[admission.EdgeCode]
		corridor, corridorFound := corridorByEdge[admission.EdgeCode]
		asset, assetFound := assetByCorridor[admission.CorridorCode]
		if !allocationFound || !routeFound || !edgeFound || !corridorFound || !assetFound ||
			admission.DepartureTick != route.DepartureTick || admission.DepartureTick != allocation.DepartureTick ||
			corridor.Code != admission.CorridorCode || asset.Code != admission.AssetCode ||
			edge.CapacityUnitsPerTick != admission.BaselineCapacityUnitsPerTick ||
			allocation.AllocatedUnits != admission.AllocatedUnits ||
			allocation.CapacityUnitsPerTick != admission.EffectiveCapacityUnitsPerTick ||
			allocation.OccupancyMilli != admission.OccupancyMilli || allocation.DelayTicks != admission.DelayTicks {
			return fmt.Errorf("V21 admission %s is not linked to its route allocation", key)
		}
		mode, modeFound := modes[route.ModeCode]
		if !modeFound {
			return fmt.Errorf("V21 admission %s has unknown mobility mode", key)
		}
		scheduleFact, scheduleFound := facts[admission.ScheduleFact]
		if !scheduleFound || scheduleFact.FactType != CityOpenWorldRuntimeFactMobilityScheduled ||
			admission.ScheduleFact != route.SourceFact || scheduleFact.Tick != admission.DepartureTick {
			return fmt.Errorf("V21 admission %s has invalid mobility schedule fact", key)
		}
		var schedulePayload struct {
			RouteCode     string   `json:"route_code"`
			DepartureTick int64    `json:"departure_tick"`
			EdgeCodes     []string `json:"edge_codes"`
		}
		if err := json.Unmarshal(scheduleFact.Payload, &schedulePayload); err != nil ||
			schedulePayload.RouteCode != route.Code || schedulePayload.DepartureTick != admission.DepartureTick ||
			!cityOpenWorldEffectiveCapacityPathContains(schedulePayload.EdgeCodes, admission.EdgeCode) {
			return fmt.Errorf("V21 admission %s schedule payload is invalid", key)
		}
		historicalState, stateErr := cityOpenWorldEffectiveCapacityStateAtSchedule(
			runtime.Infrastructure, asset.Code, admission.ScheduleFact,
		)
		if stateErr != nil || historicalState.State != admission.AssetState ||
			historicalState.CapacityMilli != admission.CapacityMilli ||
			historicalState.EffectiveTick != admission.StateEffectiveTick ||
			!cityOpenWorldEffectiveCapacityFactRefsEqual(historicalState.SourceFact, admission.StateSourceFact) {
			return fmt.Errorf("V21 admission %s does not match infrastructure history", key)
		}
		if admission.StateSourceFact != nil {
			stateFact, found := facts[*admission.StateSourceFact]
			if !found || stateFact.FactType != cityOpenWorldInfrastructureFactAssetTransition {
				return fmt.Errorf("V21 admission %s has invalid infrastructure source fact", key)
			}
		}
		expectedCapacity, capacityErr := cityOpenWorldEffectiveCapacityUnits(
			admission.BaselineCapacityUnitsPerTick, admission.CapacityMilli,
		)
		if capacityErr != nil || expectedCapacity != admission.EffectiveCapacityUnitsPerTick || expectedCapacity < admission.AllocatedUnits {
			return fmt.Errorf("V21 admission %s has invalid effective capacity", key)
		}
		usageKey := fmt.Sprintf("%d\x00%s", admission.DepartureTick, admission.EdgeCode)
		used := usedByDepartureEdge[usageKey]
		if used < 0 || used > admission.EffectiveCapacityUnitsPerTick-admission.AllocatedUnits {
			return fmt.Errorf("V21 admission %s overbooks its corridor capacity", key)
		}
		occupancy, delay, delayErr := cityOpenWorldMobilityCongestionDelay(
			mode, used+admission.AllocatedUnits, admission.EffectiveCapacityUnitsPerTick,
		)
		if delayErr != nil || occupancy != admission.OccupancyMilli || delay != admission.DelayTicks {
			return fmt.Errorf("V21 admission %s has invalid congestion evidence", key)
		}
		usedByDepartureEdge[usageKey] = used + admission.AllocatedUnits
	}
	return nil
}

func cityOpenWorldEffectiveCapacityPathContains(path []string, edgeCode string) bool {
	for _, value := range path {
		if value == edgeCode {
			return true
		}
	}
	return false
}
