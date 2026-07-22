package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

const (
	cityOpenWorldMobilitySchemaVersion           = 1
	cityOpenWorldMobilityProfileID               = "sub2api-open-world-mobility"
	cityOpenWorldMobilityProfileVersion          = "1.0.0"
	cityOpenWorldMobilityTopologyContractVersion = "facility-hub-zone-graph-v1"
	cityOpenWorldMobilitySchedulingContract      = "next_tick_capacity_v1"
	cityOpenWorldMobilityMaximumSchedulesPerTick = 256
	cityOpenWorldMobilityMaximumWaitTicks        = 32
	cityOpenWorldMobilityZoneSize                = int64(64)
	cityOpenWorldMobilityMaximumRequestUnits     = int64(1_000)
)

// CityOpenWorldMobilityPolicy records the immutable topology/scheduling
// contract and the append-only counters for the V9 aggregate transport layer.
// The policy intentionally represents a macro network, rather than claiming
// that this first layer is a lane-level traffic simulation.
type CityOpenWorldMobilityPolicy struct {
	ProfileID               string          `json:"profile_id"`
	ProfileVersion          string          `json:"profile_version"`
	ContentHash             string          `json:"content_hash"`
	BaselineTick            int64           `json:"baseline_tick"`
	TopologyContractVersion string          `json:"topology_contract_version"`
	SchedulingContract      string          `json:"scheduling_contract"`
	MaximumSchedulesPerTick int             `json:"maximum_schedules_per_tick"`
	MaximumWaitTicks        int64           `json:"maximum_wait_ticks"`
	ModeCount               int64           `json:"mode_count"`
	HubCount                int64           `json:"hub_count"`
	EdgeCount               int64           `json:"edge_count"`
	DemandCount             int64           `json:"demand_count"`
	RouteCount              int64           `json:"route_count"`
	AllocationCount         int64           `json:"allocation_count"`
	CompletedCount          int64           `json:"completed_count"`
	ExpiredCount            int64           `json:"expired_count"`
	ActorMetricCount        int64           `json:"actor_metric_count"`
	Revision                int64           `json:"revision"`
	Metadata                json.RawMessage `json:"metadata"`
}

// CityOpenWorldMobilityMode is a sealed generic transport mode. Units are
// deliberately not coupled to a particular game profession or economy: later
// freight, commute, emergency, and player-owned vehicle adapters can share it.
type CityOpenWorldMobilityMode struct {
	Code                     string          `json:"code"`
	UnitKind                 string          `json:"unit_kind"`
	SpeedUnitsPerTick        int64           `json:"speed_units_per_tick"`
	CapacityUnitsPerTick     int64           `json:"capacity_units_per_tick"`
	CongestionThresholdMilli int             `json:"congestion_threshold_milli"`
	MaximumDelayTicks        int64           `json:"maximum_delay_ticks"`
	Version                  string          `json:"version"`
	ContentHash              string          `json:"content_hash"`
	Metadata                 json.RawMessage `json:"metadata"`
}

// CityOpenWorldMobilityHub is a fixed V9 access point. Facility hubs retain a
// public facility code but hide database identifiers from snapshot/API users.
type CityOpenWorldMobilityHub struct {
	Code         string          `json:"code"`
	HubKind      string          `json:"hub_kind"`
	FacilityCode *string         `json:"facility_code,omitempty"`
	ZoneX        int64           `json:"zone_x"`
	ZoneY        int64           `json:"zone_y"`
	AnchorX      int64           `json:"anchor_x"`
	AnchorY      int64           `json:"anchor_y"`
	AnchorZ      int32           `json:"anchor_z"`
	Version      string          `json:"version"`
	ContentHash  string          `json:"content_hash"`
	Metadata     json.RawMessage `json:"metadata"`
}

// CityOpenWorldMobilityEdge is a directed edge in the sealed macro topology.
// BaseTravelTicks are immutable; actual route time receives a deterministic
// congestion delay from capacity already allocated for its departure tick.
type CityOpenWorldMobilityEdge struct {
	Code                 string          `json:"code"`
	ModeCode             string          `json:"mode_code"`
	FromHubCode          string          `json:"from_hub_code"`
	ToHubCode            string          `json:"to_hub_code"`
	Tier                 string          `json:"tier"`
	DistanceUnits        int64           `json:"distance_units"`
	BaseTravelTicks      int64           `json:"base_travel_ticks"`
	CapacityUnitsPerTick int64           `json:"capacity_units_per_tick"`
	Version              string          `json:"version"`
	ContentHash          string          `json:"content_hash"`
	Metadata             json.RawMessage `json:"metadata"`
}

// CityOpenWorldMobilityDemand is a fact-backed request. Origin is captured
// when the command is accepted, so a later local actor move cannot rewrite the
// route's causal starting point.
type CityOpenWorldMobilityDemand struct {
	Code                  string                       `json:"code"`
	ActorCode             string                       `json:"actor_code"`
	SourceHubCode         string                       `json:"source_hub_code"`
	DestinationHubCode    string                       `json:"destination_hub_code"`
	ModeCode              string                       `json:"mode_code"`
	PurposeCode           string                       `json:"purpose_code"`
	RequestedUnits        int64                        `json:"requested_units"`
	RequestedTick         int64                        `json:"requested_tick"`
	EarliestDepartureTick int64                        `json:"earliest_departure_tick"`
	DeadlineTick          int64                        `json:"deadline_tick"`
	Status                string                       `json:"status"`
	SourceFact            CityOpenWorldRuntimeFactRef  `json:"source_fact"`
	LastFact              *CityOpenWorldRuntimeFactRef `json:"last_fact,omitempty"`
	RouteCode             *string                      `json:"route_code,omitempty"`
	ScheduledTick         *int64                       `json:"scheduled_tick,omitempty"`
	CompletedTick         *int64                       `json:"completed_tick,omitempty"`
	ExpiredTick           *int64                       `json:"expired_tick,omitempty"`
	Version               int64                        `json:"version"`
	Metadata              json.RawMessage              `json:"metadata"`
}

// CityOpenWorldMobilityRoute records an accepted path. Completion does not
// mutate local actor position; V10's independent arrival bridge may later
// consume the sealed completion while keeping both reducers auditable.
type CityOpenWorldMobilityRoute struct {
	Code                 string                       `json:"code"`
	DemandCode           string                       `json:"demand_code"`
	ActorCode            string                       `json:"actor_code"`
	ModeCode             string                       `json:"mode_code"`
	SourceHubCode        string                       `json:"source_hub_code"`
	DestinationHubCode   string                       `json:"destination_hub_code"`
	DepartureTick        int64                        `json:"departure_tick"`
	ArrivalTick          int64                        `json:"arrival_tick"`
	BaseTravelTicks      int64                        `json:"base_travel_ticks"`
	CongestionDelayTicks int64                        `json:"congestion_delay_ticks"`
	Status               string                       `json:"status"`
	SourceFact           CityOpenWorldRuntimeFactRef  `json:"source_fact"`
	CompletionFact       *CityOpenWorldRuntimeFactRef `json:"completion_fact,omitempty"`
	CompletedTick        *int64                       `json:"completed_tick,omitempty"`
	Version              int64                        `json:"version"`
	Metadata             json.RawMessage              `json:"metadata"`
}

// CityOpenWorldMobilityAllocation is one route's reservation on an edge at a
// departure tick. It keeps capacity evidence separate from route narrative.
type CityOpenWorldMobilityAllocation struct {
	RouteCode            string          `json:"route_code"`
	EdgeCode             string          `json:"edge_code"`
	DepartureTick        int64           `json:"departure_tick"`
	AllocatedUnits       int64           `json:"allocated_units"`
	CapacityUnitsPerTick int64           `json:"capacity_units_per_tick"`
	OccupancyMilli       int             `json:"occupancy_milli"`
	DelayTicks           int64           `json:"delay_ticks"`
	Version              int64           `json:"version"`
	Metadata             json.RawMessage `json:"metadata"`
}

// CityOpenWorldMobilityActorMetric is a compact, derived audit projection of
// one actor's demand lifecycle. It is not a wallet or reward mechanism.
type CityOpenWorldMobilityActorMetric struct {
	ActorCode      string          `json:"actor_code"`
	RequestedCount int64           `json:"requested_count"`
	ScheduledCount int64           `json:"scheduled_count"`
	CompletedCount int64           `json:"completed_count"`
	ExpiredCount   int64           `json:"expired_count"`
	LastRouteCode  *string         `json:"last_route_code,omitempty"`
	LastEventTick  int64           `json:"last_event_tick"`
	Version        int64           `json:"version"`
	Metadata       json.RawMessage `json:"metadata"`
}

// CityOpenWorldMobilityState is included in the V9 canonical state. Static
// topology and mutable demand evidence are kept together so replay/recovery
// cannot silently swap a graph under a route history.
type CityOpenWorldMobilityState struct {
	Policy       CityOpenWorldMobilityPolicy        `json:"policy"`
	Modes        []CityOpenWorldMobilityMode        `json:"modes"`
	Hubs         []CityOpenWorldMobilityHub         `json:"hubs"`
	Edges        []CityOpenWorldMobilityEdge        `json:"edges"`
	Demands      []CityOpenWorldMobilityDemand      `json:"demands"`
	Routes       []CityOpenWorldMobilityRoute       `json:"routes"`
	Allocations  []CityOpenWorldMobilityAllocation  `json:"allocations"`
	ActorMetrics []CityOpenWorldMobilityActorMetric `json:"actor_metrics"`
}

type cityOpenWorldMobilityModeSeed struct {
	Code                     string `json:"code"`
	UnitKind                 string `json:"unit_kind"`
	SpeedUnitsPerTick        int64  `json:"speed_units_per_tick"`
	CapacityUnitsPerTick     int64  `json:"capacity_units_per_tick"`
	CongestionThresholdMilli int    `json:"congestion_threshold_milli"`
	MaximumDelayTicks        int64  `json:"maximum_delay_ticks"`
}

type cityOpenWorldMobilityFacilitySeed struct {
	ID   int64
	Code string
	X    int64
	Y    int64
	Z    int32
}

func builtInCityOpenWorldMobilityModes() ([]CityOpenWorldMobilityMode, error) {
	seeds := []cityOpenWorldMobilityModeSeed{
		{Code: "walk", UnitKind: "person", SpeedUnitsPerTick: 256, CapacityUnitsPerTick: 4_096, CongestionThresholdMilli: 900, MaximumDelayTicks: 1},
		{Code: "transit", UnitKind: "person", SpeedUnitsPerTick: 1_024, CapacityUnitsPerTick: 64, CongestionThresholdMilli: 700, MaximumDelayTicks: 4},
		{Code: "freight", UnitKind: "cargo", SpeedUnitsPerTick: 768, CapacityUnitsPerTick: 32, CongestionThresholdMilli: 650, MaximumDelayTicks: 6},
	}
	items := make([]CityOpenWorldMobilityMode, 0, len(seeds))
	for _, seed := range seeds {
		raw, err := json.Marshal(struct {
			SchemaVersion int                           `json:"schema_version"`
			Definition    cityOpenWorldMobilityModeSeed `json:"definition"`
		}{SchemaVersion: cityOpenWorldMobilitySchemaVersion, Definition: seed})
		if err != nil {
			return nil, fmt.Errorf("marshal V9 mobility mode %s: %w", seed.Code, err)
		}
		sum := sha256.Sum256(raw)
		metadata, err := cityOpenWorldMobilityModeMetadata()
		if err != nil {
			return nil, fmt.Errorf("marshal V9 mobility mode metadata %s: %w", seed.Code, err)
		}
		items = append(items, CityOpenWorldMobilityMode{
			Code: seed.Code, UnitKind: seed.UnitKind, SpeedUnitsPerTick: seed.SpeedUnitsPerTick,
			CapacityUnitsPerTick: seed.CapacityUnitsPerTick, CongestionThresholdMilli: seed.CongestionThresholdMilli,
			MaximumDelayTicks: seed.MaximumDelayTicks, Version: cityOpenWorldMobilityProfileVersion,
			ContentHash: hex.EncodeToString(sum[:]), Metadata: metadata,
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Code < items[j].Code })
	return items, nil
}

func cityOpenWorldMobilityHubCode(kind, value string) string {
	sum := sha256.Sum256([]byte(kind + "\x00" + value))
	return "hub." + kind + "." + hex.EncodeToString(sum[:16])
}

func cityOpenWorldMobilityZoneHubCode(zoneX, zoneY int64) string {
	return fmt.Sprintf("hub.zone.%d.%d", zoneX, zoneY)
}

func cityOpenWorldMobilityEdgeCode(modeCode, fromHubCode, toHubCode string) string {
	sum := sha256.Sum256([]byte(modeCode + "\x00" + fromHubCode + "\x00" + toHubCode))
	return "edge." + hex.EncodeToString(sum[:20])
}

func cityOpenWorldMobilityDistance(leftX, leftY, rightX, rightY int64) int64 {
	deltaX := leftX - rightX
	if deltaX < 0 {
		deltaX = -deltaX
	}
	deltaY := leftY - rightY
	if deltaY < 0 {
		deltaY = -deltaY
	}
	return deltaX + deltaY
}

func cityOpenWorldMobilityCeilDiv(value, divisor int64) int64 {
	if value <= 0 || divisor <= 0 {
		return 0
	}
	return 1 + (value-1)/divisor
}

func cityOpenWorldMobilityContentHash(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func cityOpenWorldMobilityModeByCode(modes []CityOpenWorldMobilityMode) map[string]CityOpenWorldMobilityMode {
	items := make(map[string]CityOpenWorldMobilityMode, len(modes))
	for _, item := range modes {
		items[item.Code] = item
	}
	return items
}

func cityOpenWorldMobilityHubContentHash(hub CityOpenWorldMobilityHub) (string, error) {
	return cityOpenWorldMobilityContentHash(struct {
		SchemaVersion int     `json:"schema_version"`
		Code          string  `json:"code"`
		HubKind       string  `json:"hub_kind"`
		FacilityCode  *string `json:"facility_code,omitempty"`
		ZoneX         int64   `json:"zone_x"`
		ZoneY         int64   `json:"zone_y"`
		AnchorX       int64   `json:"anchor_x"`
		AnchorY       int64   `json:"anchor_y"`
		AnchorZ       int32   `json:"anchor_z"`
	}{cityOpenWorldMobilitySchemaVersion, hub.Code, hub.HubKind, hub.FacilityCode, hub.ZoneX, hub.ZoneY, hub.AnchorX, hub.AnchorY, hub.AnchorZ})
}

func cityOpenWorldMobilityEdgeContentHash(edge CityOpenWorldMobilityEdge) (string, error) {
	return cityOpenWorldMobilityContentHash(struct {
		SchemaVersion        int    `json:"schema_version"`
		Code                 string `json:"code"`
		ModeCode             string `json:"mode_code"`
		FromHubCode          string `json:"from_hub_code"`
		ToHubCode            string `json:"to_hub_code"`
		Tier                 string `json:"tier"`
		DistanceUnits        int64  `json:"distance_units"`
		BaseTravelTicks      int64  `json:"base_travel_ticks"`
		CapacityUnitsPerTick int64  `json:"capacity_units_per_tick"`
	}{cityOpenWorldMobilitySchemaVersion, edge.Code, edge.ModeCode, edge.FromHubCode, edge.ToHubCode, edge.Tier, edge.DistanceUnits, edge.BaseTravelTicks, edge.CapacityUnitsPerTick})
}

// The topology checksum intentionally contains only canonical simulation
// fields. PostgreSQL JSONB is allowed to reorder diagnostic metadata keys on
// write/read; hashing raw json.RawMessage values would therefore make a newly
// created world fail validation immediately after its first database round
// trip. Each static item still has its own sealed content hash, while metadata
// remains a non-semantic explanation surface.
type cityOpenWorldMobilityTopologyMode struct {
	Code                     string `json:"code"`
	UnitKind                 string `json:"unit_kind"`
	SpeedUnitsPerTick        int64  `json:"speed_units_per_tick"`
	CapacityUnitsPerTick     int64  `json:"capacity_units_per_tick"`
	CongestionThresholdMilli int    `json:"congestion_threshold_milli"`
	MaximumDelayTicks        int64  `json:"maximum_delay_ticks"`
	Version                  string `json:"version"`
	ContentHash              string `json:"content_hash"`
}

type cityOpenWorldMobilityTopologyHub struct {
	Code         string  `json:"code"`
	HubKind      string  `json:"hub_kind"`
	FacilityCode *string `json:"facility_code,omitempty"`
	ZoneX        int64   `json:"zone_x"`
	ZoneY        int64   `json:"zone_y"`
	AnchorX      int64   `json:"anchor_x"`
	AnchorY      int64   `json:"anchor_y"`
	AnchorZ      int32   `json:"anchor_z"`
	Version      string  `json:"version"`
	ContentHash  string  `json:"content_hash"`
}

type cityOpenWorldMobilityTopologyEdge struct {
	Code                 string `json:"code"`
	ModeCode             string `json:"mode_code"`
	FromHubCode          string `json:"from_hub_code"`
	ToHubCode            string `json:"to_hub_code"`
	Tier                 string `json:"tier"`
	DistanceUnits        int64  `json:"distance_units"`
	BaseTravelTicks      int64  `json:"base_travel_ticks"`
	CapacityUnitsPerTick int64  `json:"capacity_units_per_tick"`
	Version              string `json:"version"`
	ContentHash          string `json:"content_hash"`
}

func cityOpenWorldMobilityTopologyHash(
	modes []CityOpenWorldMobilityMode,
	hubs []CityOpenWorldMobilityHub,
	edges []CityOpenWorldMobilityEdge,
) (string, error) {
	canonicalModes := make([]cityOpenWorldMobilityTopologyMode, 0, len(modes))
	for _, mode := range modes {
		canonicalModes = append(canonicalModes, cityOpenWorldMobilityTopologyMode{
			Code: mode.Code, UnitKind: mode.UnitKind, SpeedUnitsPerTick: mode.SpeedUnitsPerTick,
			CapacityUnitsPerTick: mode.CapacityUnitsPerTick, CongestionThresholdMilli: mode.CongestionThresholdMilli,
			MaximumDelayTicks: mode.MaximumDelayTicks, Version: mode.Version, ContentHash: mode.ContentHash,
		})
	}
	canonicalHubs := make([]cityOpenWorldMobilityTopologyHub, 0, len(hubs))
	for _, hub := range hubs {
		canonicalHubs = append(canonicalHubs, cityOpenWorldMobilityTopologyHub{
			Code: hub.Code, HubKind: hub.HubKind, FacilityCode: hub.FacilityCode, ZoneX: hub.ZoneX, ZoneY: hub.ZoneY,
			AnchorX: hub.AnchorX, AnchorY: hub.AnchorY, AnchorZ: hub.AnchorZ, Version: hub.Version, ContentHash: hub.ContentHash,
		})
	}
	canonicalEdges := make([]cityOpenWorldMobilityTopologyEdge, 0, len(edges))
	for _, edge := range edges {
		canonicalEdges = append(canonicalEdges, cityOpenWorldMobilityTopologyEdge{
			Code: edge.Code, ModeCode: edge.ModeCode, FromHubCode: edge.FromHubCode, ToHubCode: edge.ToHubCode,
			Tier: edge.Tier, DistanceUnits: edge.DistanceUnits, BaseTravelTicks: edge.BaseTravelTicks,
			CapacityUnitsPerTick: edge.CapacityUnitsPerTick, Version: edge.Version, ContentHash: edge.ContentHash,
		})
	}
	sort.Slice(canonicalModes, func(i, j int) bool { return canonicalModes[i].Code < canonicalModes[j].Code })
	sort.Slice(canonicalHubs, func(i, j int) bool { return canonicalHubs[i].Code < canonicalHubs[j].Code })
	sort.Slice(canonicalEdges, func(i, j int) bool { return canonicalEdges[i].Code < canonicalEdges[j].Code })
	return cityOpenWorldMobilityContentHash(struct {
		SchemaVersion      int                                 `json:"schema_version"`
		ProfileID          string                              `json:"profile_id"`
		ProfileVersion     string                              `json:"profile_version"`
		TopologyContract   string                              `json:"topology_contract"`
		SchedulingContract string                              `json:"scheduling_contract"`
		Modes              []cityOpenWorldMobilityTopologyMode `json:"modes"`
		Hubs               []cityOpenWorldMobilityTopologyHub  `json:"hubs"`
		Edges              []cityOpenWorldMobilityTopologyEdge `json:"edges"`
	}{
		SchemaVersion: cityOpenWorldMobilitySchemaVersion,
		ProfileID:     cityOpenWorldMobilityProfileID, ProfileVersion: cityOpenWorldMobilityProfileVersion,
		TopologyContract:   cityOpenWorldMobilityTopologyContractVersion,
		SchedulingContract: cityOpenWorldMobilitySchedulingContract,
		Modes:              canonicalModes, Hubs: canonicalHubs, Edges: canonicalEdges,
	})
}

// cityOpenWorldMobilityLegacyTopologyHash accepts worlds created while the
// first V9 implementation hashed raw metadata. It reconstructs the original
// bootstrap metadata instead of trusting JSONB display order, so an already
// created V9 world stays readable and recoverable while new worlds use the
// canonical semantic checksum above.
func cityOpenWorldMobilityLegacyTopologyHash(
	modes []CityOpenWorldMobilityMode,
	hubs []CityOpenWorldMobilityHub,
	edges []CityOpenWorldMobilityEdge,
) (string, error) {
	legacyModes := append([]CityOpenWorldMobilityMode(nil), modes...)
	for index := range legacyModes {
		metadata, err := cityOpenWorldMobilityModeMetadata()
		if err != nil {
			return "", err
		}
		legacyModes[index].Metadata = metadata
	}
	legacyHubs := append([]CityOpenWorldMobilityHub(nil), hubs...)
	for index := range legacyHubs {
		tier := "city"
		switch legacyHubs[index].HubKind {
		case "zone":
			tier = "district"
		case "facility":
			tier = "local"
		}
		metadata, err := cityOpenWorldMobilityHubMetadata(legacyHubs[index].HubKind, tier)
		if err != nil {
			return "", err
		}
		legacyHubs[index].Metadata = metadata
	}
	legacyEdges := append([]CityOpenWorldMobilityEdge(nil), edges...)
	for index := range legacyEdges {
		metadata, err := cityOpenWorldMobilityEdgeMetadata(legacyEdges[index].Tier)
		if err != nil {
			return "", err
		}
		legacyEdges[index].Metadata = metadata
	}
	sort.Slice(legacyModes, func(i, j int) bool { return legacyModes[i].Code < legacyModes[j].Code })
	sort.Slice(legacyHubs, func(i, j int) bool { return legacyHubs[i].Code < legacyHubs[j].Code })
	sort.Slice(legacyEdges, func(i, j int) bool { return legacyEdges[i].Code < legacyEdges[j].Code })
	return cityOpenWorldMobilityContentHash(struct {
		SchemaVersion      int                         `json:"schema_version"`
		ProfileID          string                      `json:"profile_id"`
		ProfileVersion     string                      `json:"profile_version"`
		TopologyContract   string                      `json:"topology_contract"`
		SchedulingContract string                      `json:"scheduling_contract"`
		Modes              []CityOpenWorldMobilityMode `json:"modes"`
		Hubs               []CityOpenWorldMobilityHub  `json:"hubs"`
		Edges              []CityOpenWorldMobilityEdge `json:"edges"`
	}{
		SchemaVersion: cityOpenWorldMobilitySchemaVersion,
		ProfileID:     cityOpenWorldMobilityProfileID, ProfileVersion: cityOpenWorldMobilityProfileVersion,
		TopologyContract:   cityOpenWorldMobilityTopologyContractVersion,
		SchedulingContract: cityOpenWorldMobilitySchedulingContract,
		Modes:              legacyModes, Hubs: legacyHubs, Edges: legacyEdges,
	})
}

func cityOpenWorldMobilityModeMetadata() (json.RawMessage, error) {
	return json.Marshal(map[string]any{
		"schema_version": cityOpenWorldMobilitySchemaVersion,
		"capacity_scope": "edge_departure_tick",
		"delay_contract": "post_allocation_occupancy_v1",
	})
}

func cityOpenWorldMobilityHubMetadata(hubKind, tier string) (json.RawMessage, error) {
	return json.Marshal(map[string]any{
		"schema_version": cityOpenWorldMobilitySchemaVersion,
		"hub_kind":       hubKind,
		"topology_tier":  tier,
	})
}

func cityOpenWorldMobilityEdgeMetadata(tier string) (json.RawMessage, error) {
	return json.Marshal(map[string]any{
		"schema_version":   cityOpenWorldMobilitySchemaVersion,
		"topology_tier":    tier,
		"routing_contract": "directed_weighted_path_v1",
	})
}

func buildCityOpenWorldMobilityTopology(
	binding *CityOpenWorldBinding,
	facilities []cityOpenWorldMobilityFacilitySeed,
) ([]CityOpenWorldMobilityMode, []CityOpenWorldMobilityHub, []CityOpenWorldMobilityEdge, error) {
	if binding == nil || len(facilities) == 0 {
		return nil, nil, nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v9_mobility_topology"})
	}
	modes, err := builtInCityOpenWorldMobilityModes()
	if err != nil {
		return nil, nil, nil, err
	}
	centralMetadata, err := cityOpenWorldMobilityHubMetadata("interchange", "city")
	if err != nil {
		return nil, nil, nil, err
	}
	central := CityOpenWorldMobilityHub{
		Code: "hub.interchange.central", HubKind: "interchange", ZoneX: cityOpenWorldFloorDiv(binding.SpawnX, cityOpenWorldMobilityZoneSize),
		ZoneY: cityOpenWorldFloorDiv(binding.SpawnY, cityOpenWorldMobilityZoneSize), AnchorX: binding.SpawnX, AnchorY: binding.SpawnY,
		AnchorZ: binding.SpawnZ, Version: cityOpenWorldMobilityProfileVersion, Metadata: centralMetadata,
	}
	central.ContentHash, err = cityOpenWorldMobilityHubContentHash(central)
	if err != nil {
		return nil, nil, nil, err
	}
	hubs := []CityOpenWorldMobilityHub{central}
	zoneHubs := make(map[string]CityOpenWorldMobilityHub)
	for _, facility := range facilities {
		zoneX := cityOpenWorldFloorDiv(facility.X, cityOpenWorldMobilityZoneSize)
		zoneY := cityOpenWorldFloorDiv(facility.Y, cityOpenWorldMobilityZoneSize)
		zoneCode := cityOpenWorldMobilityZoneHubCode(zoneX, zoneY)
		if _, found := zoneHubs[zoneCode]; !found {
			metadata, metadataErr := cityOpenWorldMobilityHubMetadata("zone", "district")
			if metadataErr != nil {
				return nil, nil, nil, metadataErr
			}
			zone := CityOpenWorldMobilityHub{
				Code: zoneCode, HubKind: "zone", ZoneX: zoneX, ZoneY: zoneY,
				AnchorX: zoneX*cityOpenWorldMobilityZoneSize + cityOpenWorldMobilityZoneSize/2,
				AnchorY: zoneY*cityOpenWorldMobilityZoneSize + cityOpenWorldMobilityZoneSize/2,
				AnchorZ: 0, Version: cityOpenWorldMobilityProfileVersion, Metadata: metadata,
			}
			zone.ContentHash, metadataErr = cityOpenWorldMobilityHubContentHash(zone)
			if metadataErr != nil {
				return nil, nil, nil, metadataErr
			}
			zoneHubs[zoneCode] = zone
		}
	}
	zoneCodes := make([]string, 0, len(zoneHubs))
	for code := range zoneHubs {
		zoneCodes = append(zoneCodes, code)
	}
	sort.Strings(zoneCodes)
	for _, code := range zoneCodes {
		hubs = append(hubs, zoneHubs[code])
	}
	sortedFacilities := append([]cityOpenWorldMobilityFacilitySeed(nil), facilities...)
	sort.Slice(sortedFacilities, func(i, j int) bool { return sortedFacilities[i].Code < sortedFacilities[j].Code })
	for _, facility := range sortedFacilities {
		metadata, metadataErr := cityOpenWorldMobilityHubMetadata("facility", "local")
		if metadataErr != nil {
			return nil, nil, nil, metadataErr
		}
		facilityCode := facility.Code
		hub := CityOpenWorldMobilityHub{
			Code: cityOpenWorldMobilityHubCode("facility", facility.Code), HubKind: "facility", FacilityCode: &facilityCode,
			ZoneX: cityOpenWorldFloorDiv(facility.X, cityOpenWorldMobilityZoneSize), ZoneY: cityOpenWorldFloorDiv(facility.Y, cityOpenWorldMobilityZoneSize),
			AnchorX: facility.X, AnchorY: facility.Y, AnchorZ: facility.Z,
			Version: cityOpenWorldMobilityProfileVersion, Metadata: metadata,
		}
		hub.ContentHash, metadataErr = cityOpenWorldMobilityHubContentHash(hub)
		if metadataErr != nil {
			return nil, nil, nil, metadataErr
		}
		hubs = append(hubs, hub)
	}
	sort.Slice(hubs, func(i, j int) bool { return hubs[i].Code < hubs[j].Code })
	hubByCode := make(map[string]CityOpenWorldMobilityHub, len(hubs))
	for _, hub := range hubs {
		hubByCode[hub.Code] = hub
	}
	edges := make([]CityOpenWorldMobilityEdge, 0, len(modes)*(len(sortedFacilities)*2+len(zoneCodes)*2))
	appendEdge := func(mode CityOpenWorldMobilityMode, from, to CityOpenWorldMobilityHub, tier string, capacityMultiplier int64) error {
		distance := cityOpenWorldMobilityDistance(from.AnchorX, from.AnchorY, to.AnchorX, to.AnchorY)
		baseTicks := cityOpenWorldMobilityCeilDiv(distance, mode.SpeedUnitsPerTick)
		if baseTicks < 1 {
			baseTicks = 1
		}
		metadata, metadataErr := cityOpenWorldMobilityEdgeMetadata(tier)
		if metadataErr != nil {
			return metadataErr
		}
		edge := CityOpenWorldMobilityEdge{
			Code: cityOpenWorldMobilityEdgeCode(mode.Code, from.Code, to.Code), ModeCode: mode.Code,
			FromHubCode: from.Code, ToHubCode: to.Code, Tier: tier, DistanceUnits: distance,
			BaseTravelTicks: baseTicks, CapacityUnitsPerTick: mode.CapacityUnitsPerTick * capacityMultiplier,
			Version: cityOpenWorldMobilityProfileVersion, Metadata: metadata,
		}
		edge.ContentHash, metadataErr = cityOpenWorldMobilityEdgeContentHash(edge)
		if metadataErr != nil {
			return metadataErr
		}
		edges = append(edges, edge)
		return nil
	}
	for _, mode := range modes {
		for _, facility := range sortedFacilities {
			facilityHub, found := hubByCode[cityOpenWorldMobilityHubCode("facility", facility.Code)]
			if !found {
				return nil, nil, nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v9_mobility_facility_hub"})
			}
			zoneHub, found := hubByCode[cityOpenWorldMobilityZoneHubCode(
				cityOpenWorldFloorDiv(facility.X, cityOpenWorldMobilityZoneSize), cityOpenWorldFloorDiv(facility.Y, cityOpenWorldMobilityZoneSize),
			)]
			if !found {
				return nil, nil, nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v9_mobility_zone_hub"})
			}
			if err = appendEdge(mode, facilityHub, zoneHub, "local", 1); err != nil {
				return nil, nil, nil, err
			}
			if err = appendEdge(mode, zoneHub, facilityHub, "local", 1); err != nil {
				return nil, nil, nil, err
			}
		}
		for _, zoneCode := range zoneCodes {
			zoneHub := hubByCode[zoneCode]
			if err = appendEdge(mode, zoneHub, central, "trunk", 2); err != nil {
				return nil, nil, nil, err
			}
			if err = appendEdge(mode, central, zoneHub, "trunk", 2); err != nil {
				return nil, nil, nil, err
			}
		}
	}
	sort.Slice(edges, func(i, j int) bool { return edges[i].Code < edges[j].Code })
	return modes, hubs, edges, nil
}

func activateCityOpenWorldMobilityBootstrapWrite(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if _, err := tx.ExecContext(ctx, `
SELECT set_config('sub2api.city_open_world_mobility_bootstrap_world_id', $1, TRUE)`, fmt.Sprintf("%d", worldID)); err != nil {
		return fmt.Errorf("enable open-world mobility bootstrap: %w", err)
	}
	return nil
}

// initializeCityOpenWorldV9MobilityFoundation freezes a macro topology at
// genesis or a paused V8 -> V9 upgrade. Existing actors, services, and impact
// evidence remain untouched; the baseline tick excludes invented retroactive
// trips exactly as V8 excludes retroactive impacts.
func initializeCityOpenWorldV9MobilityFoundation(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if worldID <= 0 {
		return ErrCityInvalidInput
	}
	var simulationVersion string
	var baselineTick int64
	if err := tx.QueryRowContext(ctx, `
SELECT simulation_version, current_tick
FROM city_worlds WHERE id = $1 FOR UPDATE`, worldID).Scan(&simulationVersion, &baselineTick); err != nil {
		return fmt.Errorf("load V9 mobility world: %w", err)
	}
	if !cityEngineSupportsOpenWorldMobility(simulationVersion) {
		return ErrCitySimulationVersion.WithMetadata(map[string]string{"version": simulationVersion})
	}
	if _, err := tx.ExecContext(ctx, `SELECT assert_city_open_world_impact_foundation($1)`, worldID); err != nil {
		return fmt.Errorf("validate V9 impact prerequisite: %w", err)
	}
	if err := activateCityOpenWorldMobilityBootstrapWrite(ctx, tx, worldID); err != nil {
		return err
	}
	binding, err := loadCityOpenWorldBinding(ctx, tx, worldID)
	if err != nil {
		return fmt.Errorf("load V9 mobility binding: %w", err)
	}
	rows, err := tx.QueryContext(ctx, `
SELECT id, code, anchor_x, anchor_y, anchor_z
FROM city_open_world_facilities
WHERE world_id = $1 AND state = 'active'
ORDER BY code ASC`, worldID)
	if err != nil {
		return fmt.Errorf("load V9 mobility facilities: %w", err)
	}
	facilities := make([]cityOpenWorldMobilityFacilitySeed, 0)
	for rows.Next() {
		item := cityOpenWorldMobilityFacilitySeed{}
		if err = rows.Scan(&item.ID, &item.Code, &item.X, &item.Y, &item.Z); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan V9 mobility facility: %w", err)
		}
		facilities = append(facilities, item)
	}
	if err = closeCityRows(rows, "iterate V9 mobility facilities"); err != nil {
		return err
	}
	modes, hubs, edges, err := buildCityOpenWorldMobilityTopology(binding, facilities)
	if err != nil {
		return err
	}
	contentHash, err := cityOpenWorldMobilityTopologyHash(modes, hubs, edges)
	if err != nil {
		return fmt.Errorf("hash V9 mobility topology: %w", err)
	}
	metadata, err := json.Marshal(map[string]any{
		"schema_version":    cityOpenWorldMobilitySchemaVersion,
		"topology_source":   "sealed_facility_hub_zone_graph",
		"baseline_scope":    "no_retroactive_trip_demands",
		"location_contract": "completion_does_not_move_actor_v1",
	})
	if err != nil {
		return fmt.Errorf("marshal V9 mobility profile metadata: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_mobility_profiles
    (world_id, profile_id, profile_version, content_hash, baseline_tick,
     topology_contract_version, scheduling_contract, maximum_schedules_per_tick,
     maximum_wait_ticks, mode_count, hub_count, edge_count, demand_count,
     route_count, allocation_count, completed_count, expired_count,
     actor_metric_count, revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12,
        0, 0, 0, 0, 0, 0, 1, $13::jsonb)`,
		worldID, cityOpenWorldMobilityProfileID, cityOpenWorldMobilityProfileVersion, contentHash, baselineTick,
		cityOpenWorldMobilityTopologyContractVersion, cityOpenWorldMobilitySchedulingContract,
		cityOpenWorldMobilityMaximumSchedulesPerTick, cityOpenWorldMobilityMaximumWaitTicks,
		len(modes), len(hubs), len(edges), []byte(metadata)); err != nil {
		return fmt.Errorf("insert V9 mobility profile: %w", err)
	}
	for _, mode := range modes {
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_mobility_modes
    (world_id, code, unit_kind, speed_units_per_tick, capacity_units_per_tick,
     congestion_threshold_milli, maximum_delay_ticks, definition_version,
     content_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb)`,
			worldID, mode.Code, mode.UnitKind, mode.SpeedUnitsPerTick, mode.CapacityUnitsPerTick,
			mode.CongestionThresholdMilli, mode.MaximumDelayTicks, mode.Version, mode.ContentHash,
			[]byte(mode.Metadata)); err != nil {
			return fmt.Errorf("insert V9 mobility mode %s: %w", mode.Code, err)
		}
	}
	facilityIDs := make(map[string]int64, len(facilities))
	for _, facility := range facilities {
		facilityIDs[facility.Code] = facility.ID
	}
	for _, hub := range hubs {
		var facilityID any
		if hub.FacilityCode != nil {
			id, found := facilityIDs[*hub.FacilityCode]
			if !found {
				return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v9_mobility_facility_identity"})
			}
			facilityID = id
		}
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_mobility_hubs
    (world_id, code, hub_kind, facility_id, facility_code, zone_x, zone_y,
     anchor_x, anchor_y, anchor_z, definition_version, content_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13::jsonb)`,
			worldID, hub.Code, hub.HubKind, facilityID, cityOpenWorldNullableString(hub.FacilityCode),
			hub.ZoneX, hub.ZoneY, hub.AnchorX, hub.AnchorY, hub.AnchorZ, hub.Version,
			hub.ContentHash, []byte(hub.Metadata)); err != nil {
			return fmt.Errorf("insert V9 mobility hub %s: %w", hub.Code, err)
		}
	}
	for _, edge := range edges {
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_mobility_edges
    (world_id, code, mode_code, from_hub_code, to_hub_code, tier,
     distance_units, base_travel_ticks, capacity_units_per_tick,
     definition_version, content_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb)`,
			worldID, edge.Code, edge.ModeCode, edge.FromHubCode, edge.ToHubCode, edge.Tier,
			edge.DistanceUnits, edge.BaseTravelTicks, edge.CapacityUnitsPerTick,
			edge.Version, edge.ContentHash, []byte(edge.Metadata)); err != nil {
			return fmt.Errorf("insert V9 mobility edge %s: %w", edge.Code, err)
		}
	}
	if _, err = tx.ExecContext(ctx, `SELECT assert_city_open_world_mobility_foundation($1)`, worldID); err != nil {
		return fmt.Errorf("validate V9 mobility foundation: %w", err)
	}
	return nil
}

func loadCityOpenWorldMobilityState(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*CityOpenWorldMobilityState, error) {
	state := &CityOpenWorldMobilityState{
		Modes: make([]CityOpenWorldMobilityMode, 0), Hubs: make([]CityOpenWorldMobilityHub, 0),
		Edges: make([]CityOpenWorldMobilityEdge, 0), Demands: make([]CityOpenWorldMobilityDemand, 0),
		Routes: make([]CityOpenWorldMobilityRoute, 0), Allocations: make([]CityOpenWorldMobilityAllocation, 0),
		ActorMetrics: make([]CityOpenWorldMobilityActorMetric, 0),
	}
	if err := queryer.QueryRowContext(ctx, `
SELECT profile_id, profile_version, content_hash, baseline_tick,
       topology_contract_version, scheduling_contract, maximum_schedules_per_tick,
       maximum_wait_ticks, mode_count, hub_count, edge_count, demand_count,
       route_count, allocation_count, completed_count, expired_count,
       actor_metric_count, revision, metadata
FROM city_open_world_mobility_profiles
WHERE world_id = $1`, worldID).Scan(
		&state.Policy.ProfileID, &state.Policy.ProfileVersion, &state.Policy.ContentHash, &state.Policy.BaselineTick,
		&state.Policy.TopologyContractVersion, &state.Policy.SchedulingContract, &state.Policy.MaximumSchedulesPerTick,
		&state.Policy.MaximumWaitTicks, &state.Policy.ModeCount, &state.Policy.HubCount, &state.Policy.EdgeCount,
		&state.Policy.DemandCount, &state.Policy.RouteCount, &state.Policy.AllocationCount, &state.Policy.CompletedCount,
		&state.Policy.ExpiredCount, &state.Policy.ActorMetricCount, &state.Policy.Revision, &state.Policy.Metadata,
	); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v9_mobility_profile"})
	} else if err != nil {
		return nil, fmt.Errorf("load V9 mobility profile: %w", err)
	}
	modeRows, err := queryer.QueryContext(ctx, `
SELECT code, unit_kind, speed_units_per_tick, capacity_units_per_tick,
       congestion_threshold_milli, maximum_delay_ticks, definition_version,
       content_hash, metadata
FROM city_open_world_mobility_modes
WHERE world_id = $1
ORDER BY code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V9 mobility modes: %w", err)
	}
	for modeRows.Next() {
		item := CityOpenWorldMobilityMode{}
		if err = modeRows.Scan(&item.Code, &item.UnitKind, &item.SpeedUnitsPerTick, &item.CapacityUnitsPerTick,
			&item.CongestionThresholdMilli, &item.MaximumDelayTicks, &item.Version, &item.ContentHash, &item.Metadata); err != nil {
			_ = modeRows.Close()
			return nil, fmt.Errorf("scan V9 mobility mode: %w", err)
		}
		state.Modes = append(state.Modes, item)
	}
	if err = closeCityRows(modeRows, "iterate V9 mobility modes"); err != nil {
		return nil, err
	}
	hubRows, err := queryer.QueryContext(ctx, `
SELECT code, hub_kind, facility_code, zone_x, zone_y, anchor_x, anchor_y,
       anchor_z, definition_version, content_hash, metadata
FROM city_open_world_mobility_hubs
WHERE world_id = $1
ORDER BY code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V9 mobility hubs: %w", err)
	}
	for hubRows.Next() {
		item := CityOpenWorldMobilityHub{}
		var facilityCode sql.NullString
		if err = hubRows.Scan(&item.Code, &item.HubKind, &facilityCode, &item.ZoneX, &item.ZoneY,
			&item.AnchorX, &item.AnchorY, &item.AnchorZ, &item.Version, &item.ContentHash, &item.Metadata); err != nil {
			_ = hubRows.Close()
			return nil, fmt.Errorf("scan V9 mobility hub: %w", err)
		}
		item.FacilityCode = nullStringPointer(facilityCode)
		state.Hubs = append(state.Hubs, item)
	}
	if err = closeCityRows(hubRows, "iterate V9 mobility hubs"); err != nil {
		return nil, err
	}
	edgeRows, err := queryer.QueryContext(ctx, `
SELECT code, mode_code, from_hub_code, to_hub_code, tier, distance_units,
       base_travel_ticks, capacity_units_per_tick, definition_version,
       content_hash, metadata
FROM city_open_world_mobility_edges
WHERE world_id = $1
ORDER BY code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V9 mobility edges: %w", err)
	}
	for edgeRows.Next() {
		item := CityOpenWorldMobilityEdge{}
		if err = edgeRows.Scan(&item.Code, &item.ModeCode, &item.FromHubCode, &item.ToHubCode,
			&item.Tier, &item.DistanceUnits, &item.BaseTravelTicks, &item.CapacityUnitsPerTick,
			&item.Version, &item.ContentHash, &item.Metadata); err != nil {
			_ = edgeRows.Close()
			return nil, fmt.Errorf("scan V9 mobility edge: %w", err)
		}
		state.Edges = append(state.Edges, item)
	}
	if err = closeCityRows(edgeRows, "iterate V9 mobility edges"); err != nil {
		return nil, err
	}
	if err = loadCityOpenWorldMobilityDynamicState(ctx, queryer, worldID, state); err != nil {
		return nil, err
	}
	if err = validateCityOpenWorldMobilityState(state); err != nil {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v9_mobility_state"}).WithCause(err)
	}
	return state, nil
}

func validateCityOpenWorldMobilityState(state *CityOpenWorldMobilityState) error {
	if state == nil || state.Policy.ProfileID != cityOpenWorldMobilityProfileID ||
		state.Policy.ProfileVersion != cityOpenWorldMobilityProfileVersion || state.Policy.BaselineTick < 0 ||
		state.Policy.TopologyContractVersion != cityOpenWorldMobilityTopologyContractVersion ||
		state.Policy.SchedulingContract != cityOpenWorldMobilitySchedulingContract ||
		state.Policy.MaximumSchedulesPerTick != cityOpenWorldMobilityMaximumSchedulesPerTick ||
		state.Policy.MaximumWaitTicks != cityOpenWorldMobilityMaximumWaitTicks || state.Policy.Revision < 1 ||
		!cityWorldVersionHashValid(state.Policy.ContentHash) || !json.Valid(state.Policy.Metadata) {
		return fmt.Errorf("invalid V9 mobility policy")
	}
	if state.Policy.ModeCount != int64(len(state.Modes)) || state.Policy.HubCount != int64(len(state.Hubs)) ||
		state.Policy.EdgeCount != int64(len(state.Edges)) || state.Policy.DemandCount != int64(len(state.Demands)) ||
		state.Policy.RouteCount != int64(len(state.Routes)) || state.Policy.AllocationCount != int64(len(state.Allocations)) ||
		state.Policy.ActorMetricCount != int64(len(state.ActorMetrics)) {
		return fmt.Errorf("V9 mobility policy counters are inconsistent")
	}
	modes := make(map[string]CityOpenWorldMobilityMode, len(state.Modes))
	for _, mode := range state.Modes {
		if _, duplicate := modes[mode.Code]; duplicate || !worldRuntimeCodeValid(mode.Code, 64) ||
			(mode.UnitKind != "person" && mode.UnitKind != "cargo") || mode.SpeedUnitsPerTick < 1 ||
			mode.CapacityUnitsPerTick < 1 || mode.CongestionThresholdMilli < 0 || mode.CongestionThresholdMilli >= 1000 ||
			mode.MaximumDelayTicks < 0 || mode.Version != cityOpenWorldMobilityProfileVersion ||
			!cityWorldVersionHashValid(mode.ContentHash) || !json.Valid(mode.Metadata) {
			return fmt.Errorf("invalid V9 mobility mode %s", mode.Code)
		}
		modes[mode.Code] = mode
	}
	if len(modes) != 3 {
		return fmt.Errorf("V9 mobility mode catalog is incomplete")
	}
	hubs := make(map[string]CityOpenWorldMobilityHub, len(state.Hubs))
	for _, hub := range state.Hubs {
		if _, duplicate := hubs[hub.Code]; duplicate || !worldRuntimeCodeValid(hub.Code, 160) ||
			(hub.HubKind != "interchange" && hub.HubKind != "zone" && hub.HubKind != "facility") ||
			hub.AnchorZ < 0 || hub.Version != cityOpenWorldMobilityProfileVersion ||
			!cityWorldVersionHashValid(hub.ContentHash) || !json.Valid(hub.Metadata) {
			return fmt.Errorf("invalid V9 mobility hub %s", hub.Code)
		}
		if hub.HubKind == "facility" {
			if hub.FacilityCode == nil || !worldRuntimeCodeValid(*hub.FacilityCode, 160) {
				return fmt.Errorf("invalid V9 mobility facility hub %s", hub.Code)
			}
		} else if hub.FacilityCode != nil {
			return fmt.Errorf("non-facility V9 mobility hub carries a facility binding")
		}
		expectedHash, err := cityOpenWorldMobilityHubContentHash(hub)
		if err != nil || hub.ContentHash != expectedHash {
			return fmt.Errorf("invalid V9 mobility hub content hash %s", hub.Code)
		}
		hubs[hub.Code] = hub
	}
	if len(hubs) < 3 {
		return fmt.Errorf("V9 mobility hub catalog is incomplete")
	}
	edges := make(map[string]CityOpenWorldMobilityEdge, len(state.Edges))
	for _, edge := range state.Edges {
		if _, duplicate := edges[edge.Code]; duplicate || !worldRuntimeCodeValid(edge.Code, 160) ||
			(edge.Tier != "local" && edge.Tier != "trunk") || edge.DistanceUnits < 0 || edge.BaseTravelTicks < 1 ||
			edge.CapacityUnitsPerTick < 1 || edge.Version != cityOpenWorldMobilityProfileVersion ||
			!cityWorldVersionHashValid(edge.ContentHash) || !json.Valid(edge.Metadata) {
			return fmt.Errorf("invalid V9 mobility edge %s", edge.Code)
		}
		if _, found := modes[edge.ModeCode]; !found {
			return fmt.Errorf("V9 mobility edge %s has unknown mode", edge.Code)
		}
		if _, found := hubs[edge.FromHubCode]; !found {
			return fmt.Errorf("V9 mobility edge %s has unknown source hub", edge.Code)
		}
		if _, found := hubs[edge.ToHubCode]; !found || edge.ToHubCode == edge.FromHubCode {
			return fmt.Errorf("V9 mobility edge %s has invalid destination hub", edge.Code)
		}
		expectedHash, err := cityOpenWorldMobilityEdgeContentHash(edge)
		if err != nil || edge.ContentHash != expectedHash || edge.Code != cityOpenWorldMobilityEdgeCode(edge.ModeCode, edge.FromHubCode, edge.ToHubCode) {
			return fmt.Errorf("invalid V9 mobility edge content hash %s", edge.Code)
		}
		edges[edge.Code] = edge
	}
	if len(edges) == 0 {
		return fmt.Errorf("V9 mobility edge catalog is empty")
	}
	contentHash, err := cityOpenWorldMobilityTopologyHash(state.Modes, state.Hubs, state.Edges)
	if err != nil || contentHash != state.Policy.ContentHash {
		legacyHash, legacyErr := cityOpenWorldMobilityLegacyTopologyHash(state.Modes, state.Hubs, state.Edges)
		if legacyErr != nil || legacyHash != state.Policy.ContentHash {
			return fmt.Errorf("V9 mobility topology content hash is invalid")
		}
	}
	demands := make(map[string]CityOpenWorldMobilityDemand, len(state.Demands))
	requestedByActor := make(map[string]int64)
	expiredByActor := make(map[string]int64)
	for _, demand := range state.Demands {
		if _, duplicate := demands[demand.Code]; duplicate || !worldRuntimeCodeValid(demand.Code, 160) ||
			!worldRuntimeCodeValid(demand.ActorCode, 128) || !worldRuntimeCodeValid(demand.PurposeCode, 96) ||
			demand.RequestedUnits < 1 || demand.RequestedUnits > cityOpenWorldMobilityMaximumRequestUnits ||
			demand.RequestedTick <= state.Policy.BaselineTick || demand.EarliestDepartureTick != demand.RequestedTick+1 ||
			demand.DeadlineTick < demand.EarliestDepartureTick || demand.SourceFact.Tick != demand.RequestedTick ||
			demand.SourceFact.Sequence < 1 || demand.Version < 1 || !json.Valid(demand.Metadata) {
			return fmt.Errorf("invalid V9 mobility demand %s", demand.Code)
		}
		if _, found := modes[demand.ModeCode]; !found {
			return fmt.Errorf("V9 mobility demand %s has unknown mode", demand.Code)
		}
		if _, found := hubs[demand.SourceHubCode]; !found {
			return fmt.Errorf("V9 mobility demand %s has unknown source hub", demand.Code)
		}
		if _, found := hubs[demand.DestinationHubCode]; !found || demand.SourceHubCode == demand.DestinationHubCode {
			return fmt.Errorf("V9 mobility demand %s has invalid destination hub", demand.Code)
		}
		if demand.LastFact == nil || demand.LastFact.Tick < demand.SourceFact.Tick || demand.LastFact.Sequence < 1 {
			return fmt.Errorf("V9 mobility demand %s has invalid last fact", demand.Code)
		}
		switch demand.Status {
		case "pending":
			if demand.RouteCode != nil || demand.ScheduledTick != nil || demand.CompletedTick != nil || demand.ExpiredTick != nil || *demand.LastFact != demand.SourceFact {
				return fmt.Errorf("pending V9 mobility demand %s carries terminal state", demand.Code)
			}
		case "scheduled":
			if demand.RouteCode == nil || demand.ScheduledTick == nil || demand.CompletedTick != nil || demand.ExpiredTick != nil || demand.LastFact.Tick != *demand.ScheduledTick {
				return fmt.Errorf("scheduled V9 mobility demand %s is inconsistent", demand.Code)
			}
		case "completed":
			if demand.RouteCode == nil || demand.ScheduledTick == nil || demand.CompletedTick == nil || demand.ExpiredTick != nil || demand.LastFact.Tick != *demand.CompletedTick {
				return fmt.Errorf("completed V9 mobility demand %s is inconsistent", demand.Code)
			}
		case "expired":
			if demand.RouteCode != nil || demand.ScheduledTick != nil || demand.CompletedTick != nil || demand.ExpiredTick == nil || demand.LastFact.Tick != *demand.ExpiredTick {
				return fmt.Errorf("expired V9 mobility demand %s is inconsistent", demand.Code)
			}
			expiredByActor[demand.ActorCode]++
		default:
			return fmt.Errorf("unknown V9 mobility demand status %s", demand.Status)
		}
		demands[demand.Code] = demand
		requestedByActor[demand.ActorCode]++
	}
	routes := make(map[string]CityOpenWorldMobilityRoute, len(state.Routes))
	scheduledByActor := make(map[string]int64)
	completedByActor := make(map[string]int64)
	for _, route := range state.Routes {
		demand, found := demands[route.DemandCode]
		if _, duplicate := routes[route.Code]; duplicate || !found || !worldRuntimeCodeValid(route.Code, 160) ||
			route.ActorCode != demand.ActorCode || route.ModeCode != demand.ModeCode || route.SourceHubCode != demand.SourceHubCode ||
			route.DestinationHubCode != demand.DestinationHubCode || route.DepartureTick < demand.EarliestDepartureTick ||
			route.ArrivalTick <= route.DepartureTick || route.BaseTravelTicks < 1 || route.CongestionDelayTicks < 0 ||
			route.ArrivalTick != route.DepartureTick+route.BaseTravelTicks+route.CongestionDelayTicks ||
			route.SourceFact.Tick != route.DepartureTick || route.SourceFact.Sequence < 1 || route.Version < 1 || !json.Valid(route.Metadata) {
			return fmt.Errorf("invalid V9 mobility route %s", route.Code)
		}
		if demand.RouteCode == nil || *demand.RouteCode != route.Code || demand.ScheduledTick == nil || *demand.ScheduledTick != route.DepartureTick {
			return fmt.Errorf("V9 mobility route %s demand linkage is invalid", route.Code)
		}
		switch route.Status {
		case "scheduled":
			if route.CompletionFact != nil || route.CompletedTick != nil || demand.Status != "scheduled" {
				return fmt.Errorf("scheduled V9 mobility route %s is inconsistent", route.Code)
			}
		case "completed":
			if route.CompletionFact == nil || route.CompletedTick == nil || *route.CompletedTick < route.ArrivalTick ||
				route.CompletionFact.Tick != *route.CompletedTick || route.CompletionFact.Sequence < 1 || demand.Status != "completed" ||
				demand.CompletedTick == nil || *demand.CompletedTick != *route.CompletedTick {
				return fmt.Errorf("completed V9 mobility route %s is inconsistent", route.Code)
			}
			completedByActor[route.ActorCode]++
		default:
			return fmt.Errorf("unknown V9 mobility route status %s", route.Status)
		}
		scheduledByActor[route.ActorCode]++
		routes[route.Code] = route
	}
	allocationsByRoute := make(map[string]int)
	for _, allocation := range state.Allocations {
		route, routeFound := routes[allocation.RouteCode]
		edge, edgeFound := edges[allocation.EdgeCode]
		effectiveMetadata, effectiveMarked, metadataErr := cityOpenWorldEffectiveCapacityAllocationMetadataFromRaw(allocation.Metadata)
		if !routeFound || !edgeFound || allocation.DepartureTick != route.DepartureTick ||
			allocation.AllocatedUnits < 1 || allocation.AllocatedUnits > cityOpenWorldMobilityMaximumRequestUnits ||
			allocation.AllocatedUnits > allocation.CapacityUnitsPerTick ||
			allocation.OccupancyMilli < 1 || allocation.OccupancyMilli > 1000 || allocation.DelayTicks < 0 ||
			allocation.Version < 1 || !json.Valid(allocation.Metadata) || metadataErr != nil {
			return fmt.Errorf("invalid V9 mobility allocation %s/%s", allocation.RouteCode, allocation.EdgeCode)
		}
		if !effectiveMarked && allocation.CapacityUnitsPerTick != edge.CapacityUnitsPerTick {
			return fmt.Errorf("legacy V9 mobility allocation %s/%s has non-base capacity", allocation.RouteCode, allocation.EdgeCode)
		}
		if effectiveMarked && (effectiveMetadata.BaselineCapacityUnitsPerTick != edge.CapacityUnitsPerTick ||
			effectiveMetadata.EffectiveCapacityUnitsPerTick != allocation.CapacityUnitsPerTick) {
			return fmt.Errorf("V21 mobility allocation %s/%s has inconsistent capacity metadata", allocation.RouteCode, allocation.EdgeCode)
		}
		allocationsByRoute[allocation.RouteCode]++
	}
	for code := range routes {
		if allocationsByRoute[code] == 0 {
			return fmt.Errorf("V9 mobility route %s has no capacity evidence", code)
		}
	}
	completedCount := int64(0)
	for _, value := range completedByActor {
		completedCount += value
	}
	expiredCount := int64(0)
	for _, value := range expiredByActor {
		expiredCount += value
	}
	if state.Policy.CompletedCount != completedCount || state.Policy.ExpiredCount != expiredCount {
		return fmt.Errorf("V9 mobility lifecycle counters are inconsistent")
	}
	metrics := make(map[string]CityOpenWorldMobilityActorMetric, len(state.ActorMetrics))
	for _, metric := range state.ActorMetrics {
		if _, duplicate := metrics[metric.ActorCode]; duplicate || !worldRuntimeCodeValid(metric.ActorCode, 128) ||
			metric.RequestedCount != requestedByActor[metric.ActorCode] || metric.ScheduledCount != scheduledByActor[metric.ActorCode] ||
			metric.CompletedCount != completedByActor[metric.ActorCode] || metric.ExpiredCount != expiredByActor[metric.ActorCode] ||
			metric.LastEventTick < 1 || metric.Version < 1 || !json.Valid(metric.Metadata) {
			return fmt.Errorf("invalid V9 mobility actor metric %s", metric.ActorCode)
		}
		if metric.LastRouteCode != nil {
			if route, found := routes[*metric.LastRouteCode]; !found || route.ActorCode != metric.ActorCode {
				return fmt.Errorf("invalid V9 mobility actor metric route %s", metric.ActorCode)
			}
		}
		metrics[metric.ActorCode] = metric
	}
	if len(metrics) != len(requestedByActor) {
		return fmt.Errorf("V9 mobility actor metrics are incomplete")
	}
	return nil
}

// GetCityOpenWorldMobilityState exposes topology to all world readers while
// scoping personal demand/route/allocation history to actors they may inspect.
func (s *CityEconomyService) GetCityOpenWorldMobilityState(
	ctx context.Context,
	userID, worldID int64,
) (*CityOpenWorldMobilityState, error) {
	if userID <= 0 || worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, userID, worldID); err != nil {
		return nil, err
	}
	var version string
	if err := s.db.QueryRowContext(ctx, `SELECT simulation_version FROM city_worlds WHERE id = $1`, worldID).Scan(&version); err != nil {
		return nil, fmt.Errorf("load V9 mobility world version: %w", err)
	}
	if !cityEngineSupportsOpenWorldMobility(version) {
		return nil, ErrCitySimulationVersion.WithMetadata(map[string]string{"version": version})
	}
	state, err := loadCityOpenWorldMobilityState(ctx, s.db, worldID)
	if err != nil {
		return nil, err
	}
	all, err := cityOpenWorldServiceMayReadAll(ctx, s.db, userID, worldID)
	if err != nil || all {
		return state, err
	}
	visible, err := cityOpenWorldServiceVisibleActorCodes(ctx, s.db, userID, worldID)
	if err != nil {
		return nil, err
	}
	demands := make([]CityOpenWorldMobilityDemand, 0, len(state.Demands))
	for _, demand := range state.Demands {
		if _, found := visible[demand.ActorCode]; found {
			demands = append(demands, demand)
		}
	}
	routes := make([]CityOpenWorldMobilityRoute, 0, len(state.Routes))
	visibleRoutes := make(map[string]struct{})
	for _, route := range state.Routes {
		if _, found := visible[route.ActorCode]; found {
			routes = append(routes, route)
			visibleRoutes[route.Code] = struct{}{}
		}
	}
	allocations := make([]CityOpenWorldMobilityAllocation, 0, len(state.Allocations))
	for _, allocation := range state.Allocations {
		if _, found := visibleRoutes[allocation.RouteCode]; found {
			allocations = append(allocations, allocation)
		}
	}
	metrics := make([]CityOpenWorldMobilityActorMetric, 0, len(state.ActorMetrics))
	for _, metric := range state.ActorMetrics {
		if _, found := visible[metric.ActorCode]; found {
			metrics = append(metrics, metric)
		}
	}
	state.Demands, state.Routes, state.Allocations, state.ActorMetrics = demands, routes, allocations, metrics
	return state, nil
}
