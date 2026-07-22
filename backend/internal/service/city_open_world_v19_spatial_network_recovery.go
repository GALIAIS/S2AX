package service

import (
	"context"
	"database/sql"
	"fmt"
)

// restoreCityOpenWorldSpatialNetworkProjection restores only V19's static
// profile/node/corridor descriptors. Its V9 source topology is restored first
// by the normal open-world recovery order, so every foreign-key and foundation
// check remains active during recovery.
func restoreCityOpenWorldSpatialNetworkProjection(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	networkState CityOpenWorldSpatialNetworkState,
) (int, error) {
	if err := validateCityOpenWorldSpatialNetworkState(&networkState); err != nil {
		return 0, fmt.Errorf("validate V19 spatial-network recovery input: %w", err)
	}
	if err := activateCityOpenWorldSpatialNetworkRecoveryWrite(ctx, tx, worldID); err != nil {
		return 0, err
	}
	count := 0
	policy := networkState.Policy
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_spatial_network_profiles
    (world_id, profile_id, profile_version, content_hash, baseline_tick,
     topology_contract, style_contract, transport_style_id, transport_style_version,
     transport_style_hash, source_worldgen_profile_id, source_worldgen_profile_version,
     source_worldgen_profile_hash, maximum_nodes, maximum_corridors,
     node_count, corridor_count, revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19::jsonb)`,
		worldID, policy.ProfileID, policy.ProfileVersion, policy.ContentHash, policy.BaselineTick,
		policy.TopologyContract, policy.StyleContract, policy.TransportStyleID, policy.TransportStyleVersion,
		policy.TransportStyleHash, policy.SourceWorldgenProfileID, policy.SourceWorldgenProfileVersion,
		policy.SourceWorldgenProfileHash, policy.MaximumNodes, policy.MaximumCorridors,
		policy.NodeCount, policy.CorridorCount, policy.Revision, []byte(policy.Metadata)); err != nil {
		return count, fmt.Errorf("restore V19 spatial-network profile: %w", err)
	}
	count++
	for _, node := range networkState.Nodes {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_spatial_network_nodes
    (world_id, code, hub_code, hub_kind, node_class, anchor_x, anchor_y, anchor_z,
     definition_version, content_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb)`,
			worldID, node.Code, node.HubCode, node.HubKind, node.NodeClass, node.AnchorX, node.AnchorY,
			node.AnchorZ, node.Version, node.ContentHash, []byte(node.Metadata)); err != nil {
			return count, fmt.Errorf("restore V19 spatial-network node %s: %w", node.Code, err)
		}
		count++
	}
	for _, corridor := range networkState.Corridors {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_spatial_network_corridors
    (world_id, code, edge_code, mode_code, from_node_code, to_node_code, corridor_class,
     tier, distance_units, base_travel_ticks, capacity_units_per_tick, definition_version,
     content_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14::jsonb)`,
			worldID, corridor.Code, corridor.EdgeCode, corridor.ModeCode, corridor.FromNodeCode,
			corridor.ToNodeCode, corridor.CorridorClass, corridor.Tier, corridor.DistanceUnits,
			corridor.BaseTravelTicks, corridor.CapacityUnitsPerTick, corridor.Version,
			corridor.ContentHash, []byte(corridor.Metadata)); err != nil {
			return count, fmt.Errorf("restore V19 spatial-network corridor %s: %w", corridor.Code, err)
		}
		count++
	}
	if err := assertCityOpenWorldSpatialNetworkFoundation(ctx, tx, worldID); err != nil {
		return count, fmt.Errorf("validate restored V19 spatial-network foundation: %w", err)
	}
	return count, nil
}
