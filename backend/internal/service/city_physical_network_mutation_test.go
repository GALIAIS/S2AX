package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeCityPhysicalNetworkCommandsAcceptsStrictVersionedPayloads(t *testing.T) {
	tests := []struct {
		name        string
		commandType string
		payload     string
	}{
		{
			name: "network", commandType: CityCommandTypePhysicalNetworkConfigure,
			payload: `{"code":"grid_core","name":"Core Grid","service_code":"electric_power","status":"active","expected_version":0,"metadata":{"operator":"city"}}`,
		},
		{
			name: "supply node", commandType: CityCommandTypePhysicalNodeConfigure,
			payload: `{"code":"supply_core","network_code":"grid_core","role":"supply","capacity_code":"facility_core.electric_power","district_code":"core","building_code":"plant_core","world_x":2,"world_y":3,"world_z":0,"status":"active","expected_version":0,"metadata":{}}`,
		},
		{
			name: "junction node", commandType: CityCommandTypePhysicalNodeConfigure,
			payload: `{"code":"junction_core","network_code":"grid_core","role":"junction","status":"offline","expected_version":1,"metadata":{}}`,
		},
		{
			name: "edge", commandType: CityCommandTypePhysicalEdgeConfigure,
			payload: `{"code":"line_core","network_code":"grid_core","from_node_code":"supply_core","to_node_code":"junction_core","direction":"bidirectional","installed_capacity_units":1000,"availability_milli":950,"loss_milli":25,"base_cost_units":4,"status":"active","expected_version":0,"metadata":{}}`,
		},
		{
			name: "edge transition", commandType: CityCommandTypePhysicalEdgeTransition,
			payload: `{"edge_code":"line_core","to_status":"isolated","expected_version":3,"metadata":{"reason":"maintenance"}}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value, handled, err := normalizeCityPhysicalNetworkCommand(
				test.commandType, json.RawMessage(test.payload),
			)
			require.True(t, handled)
			require.NoError(t, err)
			require.NotNil(t, value)
		})
	}
}

func TestNormalizeCityPhysicalNetworkCommandsRejectsAmbiguousTopology(t *testing.T) {
	tests := []struct {
		name        string
		commandType string
		payload     string
	}{
		{
			name: "unknown field", commandType: CityCommandTypePhysicalNetworkConfigure,
			payload: `{"code":"grid_core","name":"Core Grid","service_code":"electric_power","status":"active","expected_version":0,"metadata":{},"force":true}`,
		},
		{
			name: "partial coordinate", commandType: CityCommandTypePhysicalNodeConfigure,
			payload: `{"code":"junction_core","network_code":"grid_core","role":"junction","world_x":2,"world_y":3,"status":"active","expected_version":0,"metadata":{}}`,
		},
		{
			name: "junction with demand binding", commandType: CityCommandTypePhysicalNodeConfigure,
			payload: `{"code":"junction_core","network_code":"grid_core","role":"junction","demand_code":"demand_core","status":"active","expected_version":0,"metadata":{}}`,
		},
		{
			name: "edge self loop", commandType: CityCommandTypePhysicalEdgeConfigure,
			payload: `{"code":"line_core","network_code":"grid_core","from_node_code":"node_core","to_node_code":"node_core","direction":"directed","installed_capacity_units":1000,"availability_milli":950,"loss_milli":25,"base_cost_units":4,"status":"active","expected_version":0,"metadata":{}}`,
		},
		{
			name: "unversioned transition", commandType: CityCommandTypePhysicalEdgeTransition,
			payload: `{"edge_code":"line_core","to_status":"isolated","expected_version":0,"metadata":{}}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value, handled, err := normalizeCityPhysicalNetworkCommand(
				test.commandType, json.RawMessage(test.payload),
			)
			require.True(t, handled)
			require.Error(t, err)
			require.Nil(t, value)
		})
	}
}

func TestNormalizeCityPhysicalNetworkCommandLeavesOtherDomainsUntouched(t *testing.T) {
	value, handled, err := normalizeCityPhysicalNetworkCommand(
		CityCommandTypeFacilityRegister, json.RawMessage(`{"code":"facility"}`),
	)
	require.NoError(t, err)
	require.False(t, handled)
	require.Nil(t, value)
}
