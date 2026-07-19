package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityPhysicalNetworkPolicyCatalogIsDeterministicAndExtensible(t *testing.T) {
	services, _, _, err := cityPublicServiceCatalog()
	require.NoError(t, err)
	policies, policyHash, err := cityPhysicalNetworkPolicyCatalog(services)
	require.NoError(t, err)
	require.Len(t, policies, len(services))
	require.Len(t, policyHash, 64)

	required := make(map[string]CityPhysicalNetworkPolicy)
	for _, policy := range policies {
		if policy.NetworkRequired {
			required[policy.ServiceCode] = policy
		}
		require.Len(t, policy.PolicyHash, 64)
		require.Equal(t, cityPhysicalNetworkAlgorithmVersion, policy.AlgorithmVersion)
	}
	require.ElementsMatch(t,
		[]string{"electric_power", "potable_water", "solid_waste", "wastewater"},
		mapKeys(required),
	)
	require.Equal(t, CityNetworkRouteDemandToFacility, required["wastewater"].RouteDirection)
	require.Equal(t, CityNetworkRouteDemandToFacility, required["solid_waste"].RouteDirection)
	require.Equal(t, CityNetworkRouteSupplyToDemand, required["electric_power"].RouteDirection)

	again, againHash, err := cityPhysicalNetworkPolicyCatalog(services)
	require.NoError(t, err)
	require.Equal(t, policies, again)
	require.Equal(t, policyHash, againHash)

	extended := append(append([]CityServiceDefinition(nil), services...), CityServiceDefinition{
		Code: "telecommunications", FlowKind: "delivery",
	})
	extendedPolicies, extendedHash, err := cityPhysicalNetworkPolicyCatalog(extended)
	require.NoError(t, err)
	require.Len(t, extendedPolicies, len(policies)+1)
	require.NotEqual(t, policyHash, extendedHash)
	var telecommunications CityPhysicalNetworkPolicy
	for _, policy := range extendedPolicies {
		if policy.ServiceCode == "telecommunications" {
			telecommunications = policy
			break
		}
	}
	require.Equal(t, "telecommunications", telecommunications.ServiceCode)
	require.False(t, telecommunications.NetworkRequired)
}

func TestCityPhysicalNetworkBaselineCodeIsStableAndBounded(t *testing.T) {
	short, err := cityPhysicalNetworkBaselineCode("edge", "electric_power", "connection_alpha")
	require.NoError(t, err)
	require.Equal(t, "edge.electric_power.connection_alpha", short)

	longComponent := "connection_" + strings.Repeat("a", 160)
	first, err := cityPhysicalNetworkBaselineCode("edge", "electric_power", longComponent)
	require.NoError(t, err)
	second, err := cityPhysicalNetworkBaselineCode("edge", "electric_power", longComponent)
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.LessOrEqual(t, len(first), 96)
	require.True(t, cityServiceCodePattern.MatchString(first))
}

func mapKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}
