package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/cityspatial"
	"github.com/stretchr/testify/require"
)

func cityOpenWorldV19TestTopology() ([]CityOpenWorldMobilityHub, []CityOpenWorldMobilityEdge) {
	return []CityOpenWorldMobilityHub{
			{Code: "hub.zone.west", HubKind: "zone", AnchorX: -9, AnchorY: 3, AnchorZ: 0},
			{Code: "hub.interchange.central", HubKind: "interchange", AnchorX: 0, AnchorY: 0, AnchorZ: 0},
			{Code: "hub.facility.east", HubKind: "facility", AnchorX: 13, AnchorY: 5, AnchorZ: 1},
		}, []CityOpenWorldMobilityEdge{
			{Code: "edge.transit.central.east", ModeCode: "transit", FromHubCode: "hub.interchange.central", ToHubCode: "hub.facility.east", Tier: "trunk", DistanceUnits: 13, BaseTravelTicks: 2, CapacityUnitsPerTick: 9},
			{Code: "edge.walk.west.central", ModeCode: "walk", FromHubCode: "hub.zone.west", ToHubCode: "hub.interchange.central", Tier: "local", DistanceUnits: 9, BaseTravelTicks: 3, CapacityUnitsPerTick: 15},
		}
}

func newValidCityOpenWorldSpatialNetworkState(t *testing.T) *CityOpenWorldSpatialNetworkState {
	t.Helper()
	style, err := cityspatial.OpenWorldTransportStyleProfileForWorldgenProfile(cityspatial.DefaultWorldgenProfileID)
	require.NoError(t, err)
	hubs, edges := cityOpenWorldV19TestTopology()
	nodes, corridors, err := buildCityOpenWorldSpatialNetworkTopology(style, hubs, edges)
	require.NoError(t, err)
	sourceHash := strings.Repeat("a", 64)
	policyHash, err := cityOpenWorldSpatialNetworkPolicyHash(*style, cityspatial.DefaultWorldgenProfileID, "1.0.0", sourceHash)
	require.NoError(t, err)
	metadata, err := json.Marshal(map[string]any{
		"schema_version": cityOpenWorldSpatialNetworkSchemaVersion,
		"scope":          "v9_hub_edge_spatial_identity_only",
		"mutability":     "static_until_future_f9_3_revision",
		"legacy":         "v18_topology_mapped_at_baseline",
	})
	require.NoError(t, err)
	return &CityOpenWorldSpatialNetworkState{
		Policy: CityOpenWorldSpatialNetworkPolicy{
			ProfileID:                    cityOpenWorldSpatialNetworkProfileID,
			ProfileVersion:               cityOpenWorldSpatialNetworkProfileVersion,
			ContentHash:                  policyHash,
			BaselineTick:                 0,
			TopologyContract:             cityOpenWorldSpatialNetworkTopologyContract,
			StyleContract:                cityOpenWorldSpatialNetworkStyleContract,
			TransportStyleID:             style.ID,
			TransportStyleVersion:        style.Version,
			TransportStyleHash:           style.ContentHash,
			SourceWorldgenProfileID:      cityspatial.DefaultWorldgenProfileID,
			SourceWorldgenProfileVersion: "1.0.0",
			SourceWorldgenProfileHash:    sourceHash,
			MaximumNodes:                 cityOpenWorldSpatialNetworkMaximumNodes,
			MaximumCorridors:             cityOpenWorldSpatialNetworkMaximumCorridors,
			NodeCount:                    int64(len(nodes)),
			CorridorCount:                int64(len(corridors)),
			Revision:                     1,
			Metadata:                     metadata,
		},
		Nodes:     nodes,
		Corridors: corridors,
	}
}

func TestCityOpenWorldSpatialNetworkTopologyIsDeterministicAndProfileDriven(t *testing.T) {
	hubs, edges := cityOpenWorldV19TestTopology()
	defaultStyle, err := cityspatial.OpenWorldTransportStyleProfileForWorldgenProfile(cityspatial.DefaultWorldgenProfileID)
	require.NoError(t, err)
	firstNodes, firstCorridors, err := buildCityOpenWorldSpatialNetworkTopology(defaultStyle, hubs, edges)
	require.NoError(t, err)

	reversedHubs := []CityOpenWorldMobilityHub{hubs[2], hubs[1], hubs[0]}
	reversedEdges := []CityOpenWorldMobilityEdge{edges[1], edges[0]}
	secondNodes, secondCorridors, err := buildCityOpenWorldSpatialNetworkTopology(defaultStyle, reversedHubs, reversedEdges)
	require.NoError(t, err)
	require.Equal(t, firstNodes, secondNodes)
	require.Equal(t, firstCorridors, secondCorridors)
	require.Len(t, firstNodes, len(hubs))
	require.Len(t, firstCorridors, len(edges))
	require.Equal(t, "city_interchange", firstNodes[1].NodeClass)
	require.Equal(t, "rapid_transit_spine", firstCorridors[0].CorridorClass)

	jpStyle, err := cityspatial.OpenWorldTransportStyleProfileForWorldgenProfile(cityspatial.WorldgenProfileJapanMetropolitan)
	require.NoError(t, err)
	jpNodes, jpCorridors, err := buildCityOpenWorldSpatialNetworkTopology(jpStyle, hubs, edges)
	require.NoError(t, err)
	require.NotEqual(t, firstNodes[1].NodeClass, jpNodes[1].NodeClass)
	require.NotEqual(t, firstCorridors[0].CorridorClass, jpCorridors[0].CorridorClass)
	require.Equal(t, "station_concourse", jpNodes[1].NodeClass)
	require.Equal(t, "rail_trunk", jpCorridors[0].CorridorClass)
}

func TestCityOpenWorldSpatialNetworkStateRejectsTopologyDrift(t *testing.T) {
	state := newValidCityOpenWorldSpatialNetworkState(t)
	require.NoError(t, validateCityOpenWorldSpatialNetworkState(state))

	badNodeClass := newValidCityOpenWorldSpatialNetworkState(t)
	badNodeClass.Nodes[0].NodeClass = "wrong_node_class"
	require.Error(t, validateCityOpenWorldSpatialNetworkState(badNodeClass))

	badEndpoint := newValidCityOpenWorldSpatialNetworkState(t)
	badEndpoint.Corridors[0].ToNodeCode = badEndpoint.Corridors[0].FromNodeCode
	require.Error(t, validateCityOpenWorldSpatialNetworkState(badEndpoint))

	badCounter := newValidCityOpenWorldSpatialNetworkState(t)
	badCounter.Policy.CorridorCount++
	require.Error(t, validateCityOpenWorldSpatialNetworkState(badCounter))

	badContentHash := newValidCityOpenWorldSpatialNetworkState(t)
	badContentHash.Nodes[0].ContentHash = strings.Repeat("0", 64)
	require.Error(t, validateCityOpenWorldSpatialNetworkState(badContentHash))
}
