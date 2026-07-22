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

	"github.com/Wei-Shaw/sub2api/internal/cityspatial"
)

const (
	cityOpenWorldSpatialNetworkSchemaVersion    = 1
	cityOpenWorldSpatialNetworkProfileID        = "sub2api-open-world-spatial-network"
	cityOpenWorldSpatialNetworkProfileVersion   = "1.0.0"
	cityOpenWorldSpatialNetworkTopologyContract = "v9_hub_edge_spatial_corridor_v1"
	cityOpenWorldSpatialNetworkStyleContract    = "worldgen_transport_style_catalog_v1"
	cityOpenWorldSpatialNetworkMaximumNodes     = 4096
	cityOpenWorldSpatialNetworkMaximumCorridors = 32768
)

// CityOpenWorldSpatialNetworkPolicy pins V19's static infrastructure bridge.
// It owns neither route scheduling nor mutable capacity: those remain V9 and
// later F9.3 revisions respectively.
type CityOpenWorldSpatialNetworkPolicy struct {
	ProfileID                    string          `json:"profile_id"`
	ProfileVersion               string          `json:"profile_version"`
	ContentHash                  string          `json:"content_hash"`
	BaselineTick                 int64           `json:"baseline_tick"`
	TopologyContract             string          `json:"topology_contract"`
	StyleContract                string          `json:"style_contract"`
	TransportStyleID             string          `json:"transport_style_id"`
	TransportStyleVersion        string          `json:"transport_style_version"`
	TransportStyleHash           string          `json:"transport_style_hash"`
	SourceWorldgenProfileID      string          `json:"source_worldgen_profile_id"`
	SourceWorldgenProfileVersion string          `json:"source_worldgen_profile_version"`
	SourceWorldgenProfileHash    string          `json:"source_worldgen_profile_hash"`
	MaximumNodes                 int             `json:"maximum_nodes"`
	MaximumCorridors             int             `json:"maximum_corridors"`
	NodeCount                    int64           `json:"node_count"`
	CorridorCount                int64           `json:"corridor_count"`
	Revision                     int64           `json:"revision"`
	Metadata                     json.RawMessage `json:"metadata"`
}

// CityOpenWorldSpatialNetworkNode is a public, stable spatial identity for a
// single V9 hub. It is an immutable descriptor, not a live station state.
type CityOpenWorldSpatialNetworkNode struct {
	Code        string          `json:"code"`
	HubCode     string          `json:"hub_code"`
	HubKind     string          `json:"hub_kind"`
	NodeClass   string          `json:"node_class"`
	AnchorX     int64           `json:"anchor_x"`
	AnchorY     int64           `json:"anchor_y"`
	AnchorZ     int32           `json:"anchor_z"`
	Version     string          `json:"version"`
	ContentHash string          `json:"content_hash"`
	Metadata    json.RawMessage `json:"metadata"`
}

// CityOpenWorldSpatialNetworkCorridor is a public, stable spatial identity
// for one directed V9 edge. It preserves the source edge's capacity/travel
// values but does not mutate or replace V9 routing behavior.
type CityOpenWorldSpatialNetworkCorridor struct {
	Code                 string          `json:"code"`
	EdgeCode             string          `json:"edge_code"`
	ModeCode             string          `json:"mode_code"`
	FromNodeCode         string          `json:"from_node_code"`
	ToNodeCode           string          `json:"to_node_code"`
	CorridorClass        string          `json:"corridor_class"`
	Tier                 string          `json:"tier"`
	DistanceUnits        int64           `json:"distance_units"`
	BaseTravelTicks      int64           `json:"base_travel_ticks"`
	CapacityUnitsPerTick int64           `json:"capacity_units_per_tick"`
	Version              string          `json:"version"`
	ContentHash          string          `json:"content_hash"`
	Metadata             json.RawMessage `json:"metadata"`
}

// CityOpenWorldSpatialNetworkState participates in canonical snapshots from
// V19 onward. The state is deliberately static; mutable segment status is a
// later append-only F9.3 layer.
type CityOpenWorldSpatialNetworkState struct {
	Policy    CityOpenWorldSpatialNetworkPolicy     `json:"policy"`
	Nodes     []CityOpenWorldSpatialNetworkNode     `json:"nodes"`
	Corridors []CityOpenWorldSpatialNetworkCorridor `json:"corridors"`
}

func cityOpenWorldSpatialNetworkPolicyHash(
	style cityspatial.OpenWorldTransportStyleProfile,
	sourceWorldgenProfileID, sourceWorldgenProfileVersion, sourceWorldgenProfileHash string,
) (string, error) {
	raw, err := json.Marshal(struct {
		SchemaVersion                int    `json:"schema_version"`
		ProfileID                    string `json:"profile_id"`
		ProfileVersion               string `json:"profile_version"`
		TopologyContract             string `json:"topology_contract"`
		StyleContract                string `json:"style_contract"`
		TransportStyleID             string `json:"transport_style_id"`
		TransportStyleVersion        string `json:"transport_style_version"`
		TransportStyleHash           string `json:"transport_style_hash"`
		SourceWorldgenProfileID      string `json:"source_worldgen_profile_id"`
		SourceWorldgenProfileVersion string `json:"source_worldgen_profile_version"`
		SourceWorldgenProfileHash    string `json:"source_worldgen_profile_hash"`
		MaximumNodes                 int    `json:"maximum_nodes"`
		MaximumCorridors             int    `json:"maximum_corridors"`
	}{
		SchemaVersion:                cityOpenWorldSpatialNetworkSchemaVersion,
		ProfileID:                    cityOpenWorldSpatialNetworkProfileID,
		ProfileVersion:               cityOpenWorldSpatialNetworkProfileVersion,
		TopologyContract:             cityOpenWorldSpatialNetworkTopologyContract,
		StyleContract:                cityOpenWorldSpatialNetworkStyleContract,
		TransportStyleID:             style.ID,
		TransportStyleVersion:        style.Version,
		TransportStyleHash:           style.ContentHash,
		SourceWorldgenProfileID:      sourceWorldgenProfileID,
		SourceWorldgenProfileVersion: sourceWorldgenProfileVersion,
		SourceWorldgenProfileHash:    sourceWorldgenProfileHash,
		MaximumNodes:                 cityOpenWorldSpatialNetworkMaximumNodes,
		MaximumCorridors:             cityOpenWorldSpatialNetworkMaximumCorridors,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func cityOpenWorldSpatialNetworkNodeCode(hubCode string) string {
	sum := sha256.Sum256([]byte("v19.node\x00" + hubCode))
	return "spatial.network.node." + hex.EncodeToString(sum[:20])
}

func cityOpenWorldSpatialNetworkCorridorCode(edgeCode string) string {
	sum := sha256.Sum256([]byte("v19.corridor\x00" + edgeCode))
	return "spatial.network.corridor." + hex.EncodeToString(sum[:20])
}

func cityOpenWorldSpatialNetworkNodeContentHash(node CityOpenWorldSpatialNetworkNode) (string, error) {
	raw, err := json.Marshal(struct {
		Code      string `json:"code"`
		HubCode   string `json:"hub_code"`
		HubKind   string `json:"hub_kind"`
		NodeClass string `json:"node_class"`
		AnchorX   int64  `json:"anchor_x"`
		AnchorY   int64  `json:"anchor_y"`
		AnchorZ   int32  `json:"anchor_z"`
		Version   string `json:"version"`
	}{node.Code, node.HubCode, node.HubKind, node.NodeClass, node.AnchorX, node.AnchorY, node.AnchorZ, node.Version})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func cityOpenWorldSpatialNetworkCorridorContentHash(corridor CityOpenWorldSpatialNetworkCorridor) (string, error) {
	raw, err := json.Marshal(struct {
		Code                 string `json:"code"`
		EdgeCode             string `json:"edge_code"`
		ModeCode             string `json:"mode_code"`
		FromNodeCode         string `json:"from_node_code"`
		ToNodeCode           string `json:"to_node_code"`
		CorridorClass        string `json:"corridor_class"`
		Tier                 string `json:"tier"`
		DistanceUnits        int64  `json:"distance_units"`
		BaseTravelTicks      int64  `json:"base_travel_ticks"`
		CapacityUnitsPerTick int64  `json:"capacity_units_per_tick"`
		Version              string `json:"version"`
	}{
		corridor.Code, corridor.EdgeCode, corridor.ModeCode, corridor.FromNodeCode, corridor.ToNodeCode,
		corridor.CorridorClass, corridor.Tier, corridor.DistanceUnits, corridor.BaseTravelTicks,
		corridor.CapacityUnitsPerTick, corridor.Version,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func cityOpenWorldSpatialNetworkNodeMetadata(style cityspatial.OpenWorldTransportStyleProfile) (json.RawMessage, error) {
	return json.Marshal(map[string]any{
		"schema_version":          cityOpenWorldSpatialNetworkSchemaVersion,
		"source":                  "v9_hub",
		"transport_style_id":      style.ID,
		"transport_style_version": style.Version,
		"transport_style_hash":    style.ContentHash,
	})
}

func cityOpenWorldSpatialNetworkCorridorMetadata(style cityspatial.OpenWorldTransportStyleProfile) (json.RawMessage, error) {
	return json.Marshal(map[string]any{
		"schema_version":          cityOpenWorldSpatialNetworkSchemaVersion,
		"source":                  "v9_edge",
		"routing":                 "v9_path_backed",
		"transport_style_id":      style.ID,
		"transport_style_version": style.Version,
		"transport_style_hash":    style.ContentHash,
	})
}

func activateCityOpenWorldSpatialNetworkBootstrapWrite(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if _, err := tx.ExecContext(ctx, `
SELECT set_config('sub2api.city_open_world_spatial_network_bootstrap_world_id', $1, TRUE)`, fmt.Sprintf("%d", worldID)); err != nil {
		return fmt.Errorf("enable open-world V19 spatial-network bootstrap: %w", err)
	}
	return nil
}

func activateCityOpenWorldSpatialNetworkRecoveryWrite(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if _, err := tx.ExecContext(ctx, `
SELECT set_config('sub2api.city_open_world_spatial_network_recovery_world_id', $1, TRUE)`, fmt.Sprintf("%d", worldID)); err != nil {
		return fmt.Errorf("enable open-world V19 spatial-network recovery: %w", err)
	}
	return nil
}

func assertCityOpenWorldSpatialNetworkFoundation(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if _, err := tx.ExecContext(ctx, `SELECT assert_city_open_world_spatial_network_foundation($1)`, worldID); err != nil {
		return fmt.Errorf("validate V19 spatial-network foundation: %w", err)
	}
	return nil
}

// initializeCityOpenWorldV19SpatialNetworkFoundation freezes the profile's
// static mapping of V9 hubs/edges. It never creates a demand, route,
// allocation, shipment, receipt, inventory move, or journal entry.
func initializeCityOpenWorldV19SpatialNetworkFoundation(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if worldID <= 0 {
		return ErrCityInvalidInput
	}
	var version string
	var baselineTick int64
	if err := tx.QueryRowContext(ctx, `
SELECT simulation_version, current_tick
FROM city_worlds
WHERE id = $1
FOR UPDATE`, worldID).Scan(&version, &baselineTick); err != nil {
		return fmt.Errorf("lock V19 spatial-network world: %w", err)
	}
	if !cityEngineSupportsOpenWorldSpatialNetwork(version) || baselineTick < 0 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v19_spatial_network_world"})
	}
	if err := assertCityOpenWorldFreightBatchFoundation(ctx, tx, worldID); err != nil {
		return fmt.Errorf("validate V19 spatial-network V18 prerequisite: %w", err)
	}
	binding, err := loadCityOpenWorldBinding(ctx, tx, worldID)
	if err != nil {
		return fmt.Errorf("load V19 spatial-network binding: %w", err)
	}
	style, err := cityspatial.OpenWorldTransportStyleProfileForWorldgenProfile(binding.ProfileID)
	if err != nil {
		return fmt.Errorf("resolve V19 spatial-network transport style: %w", err)
	}
	if style.SourceWorldgenProfileID != binding.ProfileID || !cityWorldVersionHashValid(binding.ProfileHash) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v19_transport_style_binding"})
	}
	hubs, edges, err := loadCityOpenWorldSpatialNetworkMobilityTopology(ctx, tx, worldID)
	if err != nil {
		return err
	}
	nodes, corridors, err := buildCityOpenWorldSpatialNetworkTopology(style, hubs, edges)
	if err != nil {
		return err
	}
	contentHash, err := cityOpenWorldSpatialNetworkPolicyHash(*style, binding.ProfileID, binding.ProfileVersion, binding.ProfileHash)
	if err != nil {
		return fmt.Errorf("hash V19 spatial-network profile: %w", err)
	}
	metadata, err := json.Marshal(map[string]any{
		"schema_version": cityOpenWorldSpatialNetworkSchemaVersion,
		"scope":          "v9_hub_edge_spatial_identity_only",
		"mutability":     "static_until_future_f9_3_revision",
		"legacy":         "v18_topology_mapped_at_baseline",
	})
	if err != nil {
		return fmt.Errorf("marshal V19 spatial-network profile metadata: %w", err)
	}
	if err = activateCityOpenWorldSpatialNetworkBootstrapWrite(ctx, tx, worldID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_spatial_network_profiles
    (world_id, profile_id, profile_version, content_hash, baseline_tick,
     topology_contract, style_contract, transport_style_id, transport_style_version,
     transport_style_hash, source_worldgen_profile_id, source_worldgen_profile_version,
     source_worldgen_profile_hash, maximum_nodes, maximum_corridors,
     node_count, corridor_count, revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, 1, $18::jsonb)`,
		worldID, cityOpenWorldSpatialNetworkProfileID, cityOpenWorldSpatialNetworkProfileVersion,
		contentHash, baselineTick, cityOpenWorldSpatialNetworkTopologyContract,
		cityOpenWorldSpatialNetworkStyleContract, style.ID, style.Version, style.ContentHash,
		binding.ProfileID, binding.ProfileVersion, binding.ProfileHash,
		cityOpenWorldSpatialNetworkMaximumNodes, cityOpenWorldSpatialNetworkMaximumCorridors,
		len(nodes), len(corridors), []byte(metadata)); err != nil {
		return fmt.Errorf("insert V19 spatial-network profile: %w", err)
	}
	for _, node := range nodes {
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_spatial_network_nodes
    (world_id, code, hub_code, hub_kind, node_class, anchor_x, anchor_y, anchor_z,
     definition_version, content_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb)`,
			worldID, node.Code, node.HubCode, node.HubKind, node.NodeClass, node.AnchorX, node.AnchorY,
			node.AnchorZ, node.Version, node.ContentHash, []byte(node.Metadata)); err != nil {
			return fmt.Errorf("insert V19 spatial-network node %s: %w", node.Code, err)
		}
	}
	for _, corridor := range corridors {
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_spatial_network_corridors
    (world_id, code, edge_code, mode_code, from_node_code, to_node_code, corridor_class,
     tier, distance_units, base_travel_ticks, capacity_units_per_tick, definition_version,
     content_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14::jsonb)`,
			worldID, corridor.Code, corridor.EdgeCode, corridor.ModeCode, corridor.FromNodeCode,
			corridor.ToNodeCode, corridor.CorridorClass, corridor.Tier, corridor.DistanceUnits,
			corridor.BaseTravelTicks, corridor.CapacityUnitsPerTick, corridor.Version,
			corridor.ContentHash, []byte(corridor.Metadata)); err != nil {
			return fmt.Errorf("insert V19 spatial-network corridor %s: %w", corridor.Code, err)
		}
	}
	return assertCityOpenWorldSpatialNetworkFoundation(ctx, tx, worldID)
}

func loadCityOpenWorldSpatialNetworkMobilityTopology(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) ([]CityOpenWorldMobilityHub, []CityOpenWorldMobilityEdge, error) {
	hubRows, err := queryer.QueryContext(ctx, `
SELECT code, hub_kind, anchor_x, anchor_y, anchor_z
FROM city_open_world_mobility_hubs
WHERE world_id = $1
ORDER BY code ASC`, worldID)
	if err != nil {
		return nil, nil, fmt.Errorf("load V19 source mobility hubs: %w", err)
	}
	hubs := make([]CityOpenWorldMobilityHub, 0)
	for hubRows.Next() {
		item := CityOpenWorldMobilityHub{}
		if err = hubRows.Scan(&item.Code, &item.HubKind, &item.AnchorX, &item.AnchorY, &item.AnchorZ); err != nil {
			_ = hubRows.Close()
			return nil, nil, fmt.Errorf("scan V19 source mobility hub: %w", err)
		}
		hubs = append(hubs, item)
	}
	if err = closeCityRows(hubRows, "iterate V19 source mobility hubs"); err != nil {
		return nil, nil, err
	}
	edgeRows, err := queryer.QueryContext(ctx, `
SELECT code, mode_code, from_hub_code, to_hub_code, tier, distance_units,
       base_travel_ticks, capacity_units_per_tick
FROM city_open_world_mobility_edges
WHERE world_id = $1
ORDER BY code ASC`, worldID)
	if err != nil {
		return nil, nil, fmt.Errorf("load V19 source mobility edges: %w", err)
	}
	edges := make([]CityOpenWorldMobilityEdge, 0)
	for edgeRows.Next() {
		item := CityOpenWorldMobilityEdge{}
		if err = edgeRows.Scan(&item.Code, &item.ModeCode, &item.FromHubCode, &item.ToHubCode,
			&item.Tier, &item.DistanceUnits, &item.BaseTravelTicks, &item.CapacityUnitsPerTick); err != nil {
			_ = edgeRows.Close()
			return nil, nil, fmt.Errorf("scan V19 source mobility edge: %w", err)
		}
		edges = append(edges, item)
	}
	if err = closeCityRows(edgeRows, "iterate V19 source mobility edges"); err != nil {
		return nil, nil, err
	}
	if len(hubs) == 0 || len(edges) == 0 {
		return nil, nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v19_source_mobility_topology"})
	}
	return hubs, edges, nil
}

func buildCityOpenWorldSpatialNetworkTopology(
	style *cityspatial.OpenWorldTransportStyleProfile,
	hubs []CityOpenWorldMobilityHub,
	edges []CityOpenWorldMobilityEdge,
) ([]CityOpenWorldSpatialNetworkNode, []CityOpenWorldSpatialNetworkCorridor, error) {
	if style == nil || len(hubs) == 0 || len(edges) == 0 || len(hubs) > cityOpenWorldSpatialNetworkMaximumNodes ||
		len(edges) > cityOpenWorldSpatialNetworkMaximumCorridors {
		return nil, nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v19_spatial_network_topology"})
	}
	sortedHubs := append([]CityOpenWorldMobilityHub(nil), hubs...)
	sort.Slice(sortedHubs, func(i, j int) bool { return sortedHubs[i].Code < sortedHubs[j].Code })
	nodeMetadata, err := cityOpenWorldSpatialNetworkNodeMetadata(*style)
	if err != nil {
		return nil, nil, err
	}
	nodes := make([]CityOpenWorldSpatialNetworkNode, 0, len(sortedHubs))
	nodesByHub := make(map[string]CityOpenWorldSpatialNetworkNode, len(sortedHubs))
	for _, hub := range sortedHubs {
		if _, duplicate := nodesByHub[hub.Code]; duplicate || !cityOpenWorldSupplyChainCodeValid(hub.Code) ||
			hub.AnchorZ < 0 {
			return nil, nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v19_source_hub"})
		}
		nodeClass, found := cityspatial.OpenWorldTransportStyleNodeClass(*style, hub.HubKind)
		if !found {
			return nil, nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v19_node_style"})
		}
		node := CityOpenWorldSpatialNetworkNode{
			Code: cityOpenWorldSpatialNetworkNodeCode(hub.Code), HubCode: hub.Code, HubKind: hub.HubKind,
			NodeClass: nodeClass, AnchorX: hub.AnchorX, AnchorY: hub.AnchorY, AnchorZ: hub.AnchorZ,
			Version: cityOpenWorldSpatialNetworkProfileVersion, Metadata: append(json.RawMessage(nil), nodeMetadata...),
		}
		node.ContentHash, err = cityOpenWorldSpatialNetworkNodeContentHash(node)
		if err != nil {
			return nil, nil, err
		}
		nodes = append(nodes, node)
		nodesByHub[hub.Code] = node
	}
	sortedEdges := append([]CityOpenWorldMobilityEdge(nil), edges...)
	sort.Slice(sortedEdges, func(i, j int) bool { return sortedEdges[i].Code < sortedEdges[j].Code })
	corridorMetadata, err := cityOpenWorldSpatialNetworkCorridorMetadata(*style)
	if err != nil {
		return nil, nil, err
	}
	corridors := make([]CityOpenWorldSpatialNetworkCorridor, 0, len(sortedEdges))
	seenEdges := make(map[string]struct{}, len(sortedEdges))
	for _, edge := range sortedEdges {
		if _, duplicate := seenEdges[edge.Code]; duplicate || !cityOpenWorldSupplyChainCodeValid(edge.Code) ||
			edge.DistanceUnits < 1 || edge.BaseTravelTicks < 1 || edge.CapacityUnitsPerTick < 1 {
			return nil, nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v19_source_edge"})
		}
		seenEdges[edge.Code] = struct{}{}
		fromNode, foundFrom := nodesByHub[edge.FromHubCode]
		toNode, foundTo := nodesByHub[edge.ToHubCode]
		corridorClass, foundClass := cityspatial.OpenWorldTransportStyleCorridorClass(*style, edge.ModeCode, edge.Tier)
		if !foundFrom || !foundTo || !foundClass || fromNode.Code == toNode.Code {
			return nil, nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v19_corridor_endpoint"})
		}
		corridor := CityOpenWorldSpatialNetworkCorridor{
			Code: cityOpenWorldSpatialNetworkCorridorCode(edge.Code), EdgeCode: edge.Code, ModeCode: edge.ModeCode,
			FromNodeCode: fromNode.Code, ToNodeCode: toNode.Code, CorridorClass: corridorClass, Tier: edge.Tier,
			DistanceUnits: edge.DistanceUnits, BaseTravelTicks: edge.BaseTravelTicks,
			CapacityUnitsPerTick: edge.CapacityUnitsPerTick, Version: cityOpenWorldSpatialNetworkProfileVersion,
			Metadata: append(json.RawMessage(nil), corridorMetadata...),
		}
		corridor.ContentHash, err = cityOpenWorldSpatialNetworkCorridorContentHash(corridor)
		if err != nil {
			return nil, nil, err
		}
		corridors = append(corridors, corridor)
	}
	return nodes, corridors, nil
}

func loadCityOpenWorldSpatialNetworkState(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*CityOpenWorldSpatialNetworkState, error) {
	state := &CityOpenWorldSpatialNetworkState{
		Nodes: make([]CityOpenWorldSpatialNetworkNode, 0), Corridors: make([]CityOpenWorldSpatialNetworkCorridor, 0),
	}
	err := queryer.QueryRowContext(ctx, `
SELECT profile_id, profile_version, content_hash, baseline_tick, topology_contract,
       style_contract, transport_style_id, transport_style_version, transport_style_hash,
       source_worldgen_profile_id, source_worldgen_profile_version, source_worldgen_profile_hash,
       maximum_nodes, maximum_corridors, node_count, corridor_count, revision, metadata
FROM city_open_world_spatial_network_profiles
WHERE world_id = $1`, worldID).Scan(
		&state.Policy.ProfileID, &state.Policy.ProfileVersion, &state.Policy.ContentHash, &state.Policy.BaselineTick,
		&state.Policy.TopologyContract, &state.Policy.StyleContract, &state.Policy.TransportStyleID,
		&state.Policy.TransportStyleVersion, &state.Policy.TransportStyleHash, &state.Policy.SourceWorldgenProfileID,
		&state.Policy.SourceWorldgenProfileVersion, &state.Policy.SourceWorldgenProfileHash,
		&state.Policy.MaximumNodes, &state.Policy.MaximumCorridors, &state.Policy.NodeCount,
		&state.Policy.CorridorCount, &state.Policy.Revision, &state.Policy.Metadata,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v19_spatial_network_profile"})
	}
	if err != nil {
		return nil, fmt.Errorf("load V19 spatial-network profile: %w", err)
	}
	nodeRows, err := queryer.QueryContext(ctx, `
SELECT code, hub_code, hub_kind, node_class, anchor_x, anchor_y, anchor_z,
       definition_version, content_hash, metadata
FROM city_open_world_spatial_network_nodes
WHERE world_id = $1
ORDER BY code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V19 spatial-network nodes: %w", err)
	}
	for nodeRows.Next() {
		item := CityOpenWorldSpatialNetworkNode{}
		if err = nodeRows.Scan(&item.Code, &item.HubCode, &item.HubKind, &item.NodeClass,
			&item.AnchorX, &item.AnchorY, &item.AnchorZ, &item.Version, &item.ContentHash, &item.Metadata); err != nil {
			_ = nodeRows.Close()
			return nil, fmt.Errorf("scan V19 spatial-network node: %w", err)
		}
		state.Nodes = append(state.Nodes, item)
	}
	if err = closeCityRows(nodeRows, "iterate V19 spatial-network nodes"); err != nil {
		return nil, err
	}
	corridorRows, err := queryer.QueryContext(ctx, `
SELECT code, edge_code, mode_code, from_node_code, to_node_code, corridor_class,
       tier, distance_units, base_travel_ticks, capacity_units_per_tick,
       definition_version, content_hash, metadata
FROM city_open_world_spatial_network_corridors
WHERE world_id = $1
ORDER BY code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V19 spatial-network corridors: %w", err)
	}
	for corridorRows.Next() {
		item := CityOpenWorldSpatialNetworkCorridor{}
		if err = corridorRows.Scan(&item.Code, &item.EdgeCode, &item.ModeCode, &item.FromNodeCode,
			&item.ToNodeCode, &item.CorridorClass, &item.Tier, &item.DistanceUnits,
			&item.BaseTravelTicks, &item.CapacityUnitsPerTick, &item.Version, &item.ContentHash, &item.Metadata); err != nil {
			_ = corridorRows.Close()
			return nil, fmt.Errorf("scan V19 spatial-network corridor: %w", err)
		}
		state.Corridors = append(state.Corridors, item)
	}
	if err = closeCityRows(corridorRows, "iterate V19 spatial-network corridors"); err != nil {
		return nil, err
	}
	sortCityOpenWorldSpatialNetworkState(state)
	if err = validateCityOpenWorldSpatialNetworkState(state); err != nil {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v19_spatial_network_state"}).WithCause(err)
	}
	return state, nil
}

func validateCityOpenWorldSpatialNetworkState(state *CityOpenWorldSpatialNetworkState) error {
	if state == nil {
		return errors.New("spatial network state is required")
	}
	policy := state.Policy
	if policy.ProfileID != cityOpenWorldSpatialNetworkProfileID || policy.ProfileVersion != cityOpenWorldSpatialNetworkProfileVersion ||
		policy.BaselineTick < 0 || policy.TopologyContract != cityOpenWorldSpatialNetworkTopologyContract ||
		policy.StyleContract != cityOpenWorldSpatialNetworkStyleContract || policy.MaximumNodes != cityOpenWorldSpatialNetworkMaximumNodes ||
		policy.MaximumCorridors != cityOpenWorldSpatialNetworkMaximumCorridors || policy.Revision != 1 ||
		!cityWorldVersionHashValid(policy.SourceWorldgenProfileHash) || !cityOpenWorldSpatialNetworkPolicyMetadataValid(policy.Metadata) {
		return errors.New("invalid spatial network policy")
	}
	style, err := cityspatial.OpenWorldTransportStyleProfileForWorldgenProfile(policy.SourceWorldgenProfileID)
	if err != nil || style.ID != policy.TransportStyleID || style.Version != policy.TransportStyleVersion ||
		style.ContentHash != policy.TransportStyleHash {
		return errors.New("invalid spatial network transport style")
	}
	expectedPolicyHash, err := cityOpenWorldSpatialNetworkPolicyHash(*style, policy.SourceWorldgenProfileID,
		policy.SourceWorldgenProfileVersion, policy.SourceWorldgenProfileHash)
	if err != nil || expectedPolicyHash != policy.ContentHash || len(state.Nodes) == 0 || len(state.Corridors) == 0 ||
		len(state.Nodes) > policy.MaximumNodes || len(state.Corridors) > policy.MaximumCorridors ||
		policy.NodeCount != int64(len(state.Nodes)) || policy.CorridorCount != int64(len(state.Corridors)) {
		return errors.New("spatial network policy counters are inconsistent")
	}
	nodesByCode := make(map[string]CityOpenWorldSpatialNetworkNode, len(state.Nodes))
	nodesByHub := make(map[string]struct{}, len(state.Nodes))
	for _, node := range state.Nodes {
		if !cityOpenWorldSupplyChainCodeValid(node.Code) || !cityOpenWorldSupplyChainCodeValid(node.HubCode) ||
			node.AnchorZ < 0 || node.Version != cityOpenWorldSpatialNetworkProfileVersion ||
			cityOpenWorldSpatialNetworkNodeCode(node.HubCode) != node.Code || !cityOpenWorldSpatialNetworkNodeMetadataValid(node.Metadata, *style) {
			return errors.New("invalid spatial network node")
		}
		if _, exists := nodesByCode[node.Code]; exists {
			return errors.New("duplicate spatial network node code")
		}
		if _, exists := nodesByHub[node.HubCode]; exists {
			return errors.New("duplicate spatial network node hub")
		}
		expectedClass, found := cityspatial.OpenWorldTransportStyleNodeClass(*style, node.HubKind)
		expectedHash, hashErr := cityOpenWorldSpatialNetworkNodeContentHash(node)
		if !found || expectedClass != node.NodeClass || hashErr != nil || expectedHash != node.ContentHash {
			return errors.New("spatial network node contract is inconsistent")
		}
		nodesByCode[node.Code] = node
		nodesByHub[node.HubCode] = struct{}{}
	}
	seenEdges := make(map[string]struct{}, len(state.Corridors))
	seenCorridors := make(map[string]struct{}, len(state.Corridors))
	for _, corridor := range state.Corridors {
		if !cityOpenWorldSupplyChainCodeValid(corridor.Code) || !cityOpenWorldSupplyChainCodeValid(corridor.EdgeCode) ||
			!cityOpenWorldSupplyChainCodeValid(corridor.ModeCode) || corridor.DistanceUnits < 1 ||
			corridor.BaseTravelTicks < 1 || corridor.CapacityUnitsPerTick < 1 ||
			corridor.Version != cityOpenWorldSpatialNetworkProfileVersion ||
			cityOpenWorldSpatialNetworkCorridorCode(corridor.EdgeCode) != corridor.Code ||
			!cityOpenWorldSpatialNetworkCorridorMetadataValid(corridor.Metadata, *style) {
			return errors.New("invalid spatial network corridor")
		}
		if _, exists := seenCorridors[corridor.Code]; exists {
			return errors.New("duplicate spatial network corridor code")
		}
		if _, exists := seenEdges[corridor.EdgeCode]; exists {
			return errors.New("duplicate spatial network corridor edge")
		}
		if _, found := nodesByCode[corridor.FromNodeCode]; !found {
			return errors.New("spatial network corridor source node is missing")
		}
		if _, found := nodesByCode[corridor.ToNodeCode]; !found || corridor.FromNodeCode == corridor.ToNodeCode {
			return errors.New("spatial network corridor destination node is invalid")
		}
		expectedClass, found := cityspatial.OpenWorldTransportStyleCorridorClass(*style, corridor.ModeCode, corridor.Tier)
		expectedHash, hashErr := cityOpenWorldSpatialNetworkCorridorContentHash(corridor)
		if !found || expectedClass != corridor.CorridorClass || hashErr != nil || expectedHash != corridor.ContentHash {
			return errors.New("spatial network corridor contract is inconsistent")
		}
		seenCorridors[corridor.Code] = struct{}{}
		seenEdges[corridor.EdgeCode] = struct{}{}
	}
	return nil
}

func cityOpenWorldSpatialNetworkPolicyMetadataValid(raw json.RawMessage) bool {
	var metadata struct {
		SchemaVersion int    `json:"schema_version"`
		Scope         string `json:"scope"`
		Mutability    string `json:"mutability"`
		Legacy        string `json:"legacy"`
	}
	return json.Unmarshal(raw, &metadata) == nil && metadata.SchemaVersion == cityOpenWorldSpatialNetworkSchemaVersion &&
		metadata.Scope == "v9_hub_edge_spatial_identity_only" && metadata.Mutability == "static_until_future_f9_3_revision" &&
		metadata.Legacy == "v18_topology_mapped_at_baseline"
}

func cityOpenWorldSpatialNetworkNodeMetadataValid(raw json.RawMessage, style cityspatial.OpenWorldTransportStyleProfile) bool {
	var metadata struct {
		SchemaVersion         int    `json:"schema_version"`
		Source                string `json:"source"`
		TransportStyleID      string `json:"transport_style_id"`
		TransportStyleVersion string `json:"transport_style_version"`
		TransportStyleHash    string `json:"transport_style_hash"`
	}
	return json.Unmarshal(raw, &metadata) == nil && metadata.SchemaVersion == cityOpenWorldSpatialNetworkSchemaVersion &&
		metadata.Source == "v9_hub" && metadata.TransportStyleID == style.ID &&
		metadata.TransportStyleVersion == style.Version && metadata.TransportStyleHash == style.ContentHash
}

func cityOpenWorldSpatialNetworkCorridorMetadataValid(raw json.RawMessage, style cityspatial.OpenWorldTransportStyleProfile) bool {
	var metadata struct {
		SchemaVersion         int    `json:"schema_version"`
		Source                string `json:"source"`
		Routing               string `json:"routing"`
		TransportStyleID      string `json:"transport_style_id"`
		TransportStyleVersion string `json:"transport_style_version"`
		TransportStyleHash    string `json:"transport_style_hash"`
	}
	return json.Unmarshal(raw, &metadata) == nil && metadata.SchemaVersion == cityOpenWorldSpatialNetworkSchemaVersion &&
		metadata.Source == "v9_edge" && metadata.Routing == "v9_path_backed" &&
		metadata.TransportStyleID == style.ID && metadata.TransportStyleVersion == style.Version &&
		metadata.TransportStyleHash == style.ContentHash
}

func sortCityOpenWorldSpatialNetworkState(state *CityOpenWorldSpatialNetworkState) {
	if state == nil {
		return
	}
	sort.Slice(state.Nodes, func(i, j int) bool { return state.Nodes[i].Code < state.Nodes[j].Code })
	sort.Slice(state.Corridors, func(i, j int) bool { return state.Corridors[i].Code < state.Corridors[j].Code })
}
