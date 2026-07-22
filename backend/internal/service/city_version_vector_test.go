package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/cityspatial"
	"github.com/stretchr/testify/require"
)

func TestNormalizeCityWorldCreateInputDefaultsToOpenWorldV24(t *testing.T) {
	normalized, err := normalizeCityWorldCreateInput(CityWorldCreateInput{
		OwnerUserID: 17,
		Name:        "Version vector city",
		Timezone:    "Asia/Shanghai",
	})
	require.NoError(t, err)
	require.Equal(t, CitySimulationVersionOpenWorldV24, normalized.simulationVersion)
	require.Equal(t, cityspatial.DefaultWorldgenProfileID, normalized.styleProfileID)
	require.Equal(t, cityOpenWorldSpawnPolicy, normalized.spawnPolicy)
	require.Equal(t, CityWorldStatusRunning, normalized.initialStatus)
	require.Equal(t, cityOpenWorldInitialSpeedMilli, normalized.initialSpeedMilli)

	legacy, err := normalizeCityWorldCreateInput(CityWorldCreateInput{
		OwnerUserID:       17,
		Name:              "Legacy compatibility city",
		SimulationVersion: CitySimulationVersionF8V3,
	})
	require.NoError(t, err)
	require.Equal(t, CitySimulationVersionF8V3, legacy.simulationVersion)
	require.Empty(t, legacy.styleProfileID)
	require.Empty(t, legacy.spawnPolicy)
	require.Equal(t, CityWorldStatusPaused, legacy.initialStatus)
	require.Equal(t, cityDefaultInitialSpeedMilli, legacy.initialSpeedMilli)

	historicalOpenWorld, err := normalizeCityWorldCreateInput(CityWorldCreateInput{
		OwnerUserID:       17,
		Name:              "Historical open-world compatibility city",
		SimulationVersion: CitySimulationVersionOpenWorldV23,
	})
	require.NoError(t, err)
	require.Equal(t, CityWorldStatusPaused, historicalOpenWorld.initialStatus)
	require.Equal(t, cityDefaultInitialSpeedMilli, historicalOpenWorld.initialSpeedMilli)

	realtime, err := normalizeCityWorldCreateInput(CityWorldCreateInput{
		OwnerUserID:       17,
		Name:              "Realtime diagnostic city",
		SimulationVersion: CitySimulationVersionRealtimeV1,
	})
	require.NoError(t, err)
	require.Equal(t, CitySimulationVersionRealtimeV1, realtime.simulationVersion)
	require.Equal(t, cityRealtimeDiagnosticClockProfileID, realtime.clockProfileID)
	require.Empty(t, realtime.styleProfileID)
	require.Empty(t, realtime.spawnPolicy)
	require.Equal(t, CityWorldStatusRunning, realtime.initialStatus)
	require.Equal(t, cityDefaultInitialSpeedMilli, realtime.initialSpeedMilli)

	customRealtime, err := normalizeCityWorldCreateInput(CityWorldCreateInput{
		OwnerUserID:       17,
		Name:              "Realtime production city",
		SimulationVersion: CitySimulationVersionRealtimeV1,
		ClockProfileID:    "production-ntp-v1",
	})
	require.NoError(t, err)
	require.Equal(t, "production-ntp-v1", customRealtime.clockProfileID)

	_, err = normalizeCityWorldCreateInput(CityWorldCreateInput{
		OwnerUserID:    17,
		Name:           "Non realtime profile misuse",
		ClockProfileID: "production-ntp-v1",
	})
	require.ErrorIs(t, err, ErrCityInvalidInput)
}

func TestCityWorldVersionVectorRejectsIncompleteV11ODMetadata(t *testing.T) {
	metadata, err := cityWorldVersionBindingMetadataWithServiceImpactMobilityArrivalAndOD(
		nil, "", "", strings.Repeat("a", 64), "1.0.0", strings.Repeat("b", 64), "1.0.0",
		strings.Repeat("c", 64), "1.0.0", strings.Repeat("d", 64), "1.0.0",
		strings.Repeat("e", 64), "1.0.0",
	)
	require.NoError(t, err)
	vector := CityWorldVersionVector{
		SchemaVersion: cityWorldVersionVectorSchemaVersion,
		Generation:    5,
		Bindings:      make([]CityWorldVersionBinding, 0, len(cityWorldVersionVectorComponentOrder)),
	}
	for _, component := range cityWorldVersionVectorComponentOrder {
		bindingMetadata := json.RawMessage(`{"schema_version":1}`)
		bundleID := "sub2api-" + strings.ReplaceAll(component, "_", "-")
		if component == cityWorldVersionComponentEconomicPolicy {
			bindingMetadata = json.RawMessage(`{"schema_version":1,"projection_version":0}`)
		}
		if component == cityWorldVersionComponentWorldgenPlan {
			bindingMetadata = json.RawMessage(`{"schema_version":1,"context_hash":"` + strings.Repeat("b", 64) + `","bootstrap_plan_hash":"` + strings.Repeat("a", 64) + `"}`)
		}
		if component == cityWorldVersionComponentContentCatalog {
			bindingMetadata = metadata
			bundleID = cityWorldVersionContentCatalogV11BundleID
		}
		vector.Bindings = append(vector.Bindings, CityWorldVersionBinding{
			ComponentCode: component, BundleID: bundleID, BundleVersion: "1.0.0",
			ContentHash: strings.Repeat("a", 64), Metadata: bindingMetadata,
		})
	}
	require.NoError(t, validateCityWorldVersionVector(vector))
	for index := range vector.Bindings {
		if vector.Bindings[index].ComponentCode == cityWorldVersionComponentContentCatalog {
			vector.Bindings[index].Metadata = json.RawMessage(`{"schema_version":1,"service_profile_hash":"` + strings.Repeat("a", 64) + `","service_profile_version":"1.0.0","impact_profile_hash":"` + strings.Repeat("b", 64) + `","impact_profile_version":"1.0.0","mobility_profile_hash":"` + strings.Repeat("c", 64) + `","mobility_profile_version":"1.0.0","arrival_profile_hash":"` + strings.Repeat("d", 64) + `","arrival_profile_version":"1.0.0"}`)
		}
	}
	require.Error(t, validateCityWorldVersionVector(vector))
}

func TestCityWorldVersionVectorRejectsIncompleteV12CommuteMetadata(t *testing.T) {
	metadata, err := cityWorldVersionBindingMetadataWithServiceImpactMobilityArrivalODAndCommute(
		nil, "", "", strings.Repeat("a", 64), "1.0.0", strings.Repeat("b", 64), "1.0.0",
		strings.Repeat("c", 64), "1.0.0", strings.Repeat("d", 64), "1.0.0",
		strings.Repeat("e", 64), "1.0.0", strings.Repeat("f", 64), "1.0.0",
	)
	require.NoError(t, err)
	vector := CityWorldVersionVector{
		SchemaVersion: cityWorldVersionVectorSchemaVersion,
		Generation:    6,
		Bindings:      make([]CityWorldVersionBinding, 0, len(cityWorldVersionVectorComponentOrder)),
	}
	for _, component := range cityWorldVersionVectorComponentOrder {
		bindingMetadata := json.RawMessage(`{"schema_version":1}`)
		bundleID := "sub2api-" + strings.ReplaceAll(component, "_", "-")
		if component == cityWorldVersionComponentEconomicPolicy {
			bindingMetadata = json.RawMessage(`{"schema_version":1,"projection_version":0}`)
		}
		if component == cityWorldVersionComponentWorldgenPlan {
			bindingMetadata = json.RawMessage(`{"schema_version":1,"context_hash":"` + strings.Repeat("b", 64) + `","bootstrap_plan_hash":"` + strings.Repeat("a", 64) + `"}`)
		}
		if component == cityWorldVersionComponentContentCatalog {
			bindingMetadata = metadata
			bundleID = cityWorldVersionContentCatalogV12BundleID
		}
		vector.Bindings = append(vector.Bindings, CityWorldVersionBinding{
			ComponentCode: component, BundleID: bundleID, BundleVersion: "1.0.0",
			ContentHash: strings.Repeat("a", 64), Metadata: bindingMetadata,
		})
	}
	require.NoError(t, validateCityWorldVersionVector(vector))
	for index := range vector.Bindings {
		if vector.Bindings[index].ComponentCode == cityWorldVersionComponentContentCatalog {
			vector.Bindings[index].Metadata = json.RawMessage(`{"schema_version":1,"service_profile_hash":"` + strings.Repeat("a", 64) + `","service_profile_version":"1.0.0","impact_profile_hash":"` + strings.Repeat("b", 64) + `","impact_profile_version":"1.0.0","mobility_profile_hash":"` + strings.Repeat("c", 64) + `","mobility_profile_version":"1.0.0","arrival_profile_hash":"` + strings.Repeat("d", 64) + `","arrival_profile_version":"1.0.0","od_profile_hash":"` + strings.Repeat("e", 64) + `","od_profile_version":"1.0.0"}`)
		}
	}
	require.Error(t, validateCityWorldVersionVector(vector))
}

func TestCityWorldVersionVectorRejectsIncompleteV15SupplyChainMetadata(t *testing.T) {
	metadata, err := cityWorldVersionBindingMetadataWithSupplyChain(
		nil, "", "", strings.Repeat("a", 64), "1.0.0", strings.Repeat("b", 64), "1.0.0",
		strings.Repeat("c", 64), "1.0.0", strings.Repeat("d", 64), "1.0.0",
		strings.Repeat("e", 64), "1.0.0", strings.Repeat("f", 64), "1.0.0",
		strings.Repeat("1", 64), "1.0.0", strings.Repeat("2", 64), "1.0.0",
		strings.Repeat("3", 64), "1.0.0",
		cityOpenWorldSupplyChainNodeContract, cityOpenWorldSupplyChainOrderContract,
		cityOpenWorldSupplyChainSettlementContract, cityOpenWorldSupplyChainDeliveryContract,
	)
	require.NoError(t, err)
	vector := CityWorldVersionVector{
		SchemaVersion: cityWorldVersionVectorSchemaVersion,
		Generation:    9,
		Bindings:      make([]CityWorldVersionBinding, 0, len(cityWorldVersionVectorComponentOrder)),
	}
	for _, component := range cityWorldVersionVectorComponentOrder {
		bindingMetadata := json.RawMessage(`{"schema_version":1}`)
		bundleID := "sub2api-" + strings.ReplaceAll(component, "_", "-")
		if component == cityWorldVersionComponentEconomicPolicy {
			bindingMetadata = json.RawMessage(`{"schema_version":1,"projection_version":0}`)
		}
		if component == cityWorldVersionComponentWorldgenPlan {
			bindingMetadata = json.RawMessage(`{"schema_version":1,"context_hash":"` + strings.Repeat("b", 64) + `","bootstrap_plan_hash":"` + strings.Repeat("a", 64) + `"}`)
		}
		if component == cityWorldVersionComponentContentCatalog {
			bindingMetadata = metadata
			bundleID = cityWorldVersionContentCatalogV15BundleID
		}
		vector.Bindings = append(vector.Bindings, CityWorldVersionBinding{
			ComponentCode: component, BundleID: bundleID, BundleVersion: "1.0.0",
			ContentHash: strings.Repeat("a", 64), Metadata: bindingMetadata,
		})
	}
	require.NoError(t, validateCityWorldVersionVector(vector))
	for index := range vector.Bindings {
		if vector.Bindings[index].ComponentCode == cityWorldVersionComponentContentCatalog {
			vector.Bindings[index].Metadata = json.RawMessage(`{"schema_version":1,"service_profile_hash":"` + strings.Repeat("a", 64) + `","service_profile_version":"1.0.0","impact_profile_hash":"` + strings.Repeat("b", 64) + `","impact_profile_version":"1.0.0","mobility_profile_hash":"` + strings.Repeat("c", 64) + `","mobility_profile_version":"1.0.0","arrival_profile_hash":"` + strings.Repeat("d", 64) + `","arrival_profile_version":"1.0.0","od_profile_hash":"` + strings.Repeat("e", 64) + `","od_profile_version":"1.0.0","commute_profile_hash":"` + strings.Repeat("f", 64) + `","commute_profile_version":"1.0.0","commute_source_profile_hash":"` + strings.Repeat("1", 64) + `","commute_source_profile_version":"1.0.0","commute_lifecycle_profile_hash":"` + strings.Repeat("2", 64) + `","commute_lifecycle_profile_version":"1.0.0","supply_chain_profile_hash":"` + strings.Repeat("3", 64) + `","supply_chain_profile_version":"1.0.0","supply_chain_node_contract":"wrong","supply_chain_order_contract":"` + cityOpenWorldSupplyChainOrderContract + `","supply_chain_settlement_contract":"` + cityOpenWorldSupplyChainSettlementContract + `","supply_chain_delivery_contract":"` + cityOpenWorldSupplyChainDeliveryContract + `"}`)
		}
	}
	require.Error(t, validateCityWorldVersionVector(vector))
}

func TestCityWorldVersionVectorRejectsIncompleteV16EnterpriseFreightMetadata(t *testing.T) {
	metadata, err := cityWorldVersionBindingMetadataWithEnterpriseFreight(
		nil, "", "", strings.Repeat("a", 64), "1.0.0", strings.Repeat("b", 64), "1.0.0",
		strings.Repeat("c", 64), "1.0.0", strings.Repeat("d", 64), "1.0.0",
		strings.Repeat("e", 64), "1.0.0", strings.Repeat("f", 64), "1.0.0",
		strings.Repeat("1", 64), "1.0.0", strings.Repeat("2", 64), "1.0.0",
		strings.Repeat("3", 64), "1.0.0",
		cityOpenWorldSupplyChainNodeContract, cityOpenWorldSupplyChainOrderContract,
		cityOpenWorldSupplyChainSettlementContract, cityOpenWorldSupplyChainDeliveryContract,
		strings.Repeat("4", 64), cityOpenWorldEnterpriseFreightProfileVersion,
		cityOpenWorldEnterpriseFreightSourceContract, cityOpenWorldEnterpriseFreightDemandContract,
		cityOpenWorldEnterpriseFreightCompletionContract, cityOpenWorldEnterpriseFreightTerminalContract,
		cityOpenWorldEnterpriseFreightCarrierActorCode,
	)
	require.NoError(t, err)
	vector := CityWorldVersionVector{
		SchemaVersion: cityWorldVersionVectorSchemaVersion,
		Generation:    10,
		Bindings:      make([]CityWorldVersionBinding, 0, len(cityWorldVersionVectorComponentOrder)),
	}
	for _, component := range cityWorldVersionVectorComponentOrder {
		bindingMetadata := json.RawMessage(`{"schema_version":1}`)
		bundleID := "sub2api-" + strings.ReplaceAll(component, "_", "-")
		if component == cityWorldVersionComponentEconomicPolicy {
			bindingMetadata = json.RawMessage(`{"schema_version":1,"projection_version":0}`)
		}
		if component == cityWorldVersionComponentWorldgenPlan {
			bindingMetadata = json.RawMessage(`{"schema_version":1,"context_hash":"` + strings.Repeat("b", 64) + `","bootstrap_plan_hash":"` + strings.Repeat("a", 64) + `"}`)
		}
		if component == cityWorldVersionComponentContentCatalog {
			bindingMetadata = metadata
			bundleID = cityWorldVersionContentCatalogV16BundleID
		}
		vector.Bindings = append(vector.Bindings, CityWorldVersionBinding{
			ComponentCode: component, BundleID: bundleID, BundleVersion: "1.0.0",
			ContentHash: strings.Repeat("a", 64), Metadata: bindingMetadata,
		})
	}
	require.NoError(t, validateCityWorldVersionVector(vector))
	for index := range vector.Bindings {
		if vector.Bindings[index].ComponentCode != cityWorldVersionComponentContentCatalog {
			continue
		}
		invalid := map[string]any{}
		require.NoError(t, json.Unmarshal(vector.Bindings[index].Metadata, &invalid))
		invalid["enterprise_freight_terminal_contract"] = "unsealed"
		vector.Bindings[index].Metadata, err = json.Marshal(invalid)
		require.NoError(t, err)
	}
	require.Error(t, validateCityWorldVersionVector(vector))
}

func TestCityWorldVersionVectorRejectsIncompleteV9MobilityMetadata(t *testing.T) {
	metadata, err := cityWorldVersionBindingMetadataWithServiceImpactAndMobility(
		nil, "", "", strings.Repeat("a", 64), "1.0.0", strings.Repeat("b", 64), "1.0.0",
		strings.Repeat("c", 64), "1.0.0",
	)
	require.NoError(t, err)
	vector := CityWorldVersionVector{
		SchemaVersion: cityWorldVersionVectorSchemaVersion,
		Generation:    4,
		Bindings:      make([]CityWorldVersionBinding, 0, len(cityWorldVersionVectorComponentOrder)),
	}
	for _, component := range cityWorldVersionVectorComponentOrder {
		bindingMetadata := json.RawMessage(`{"schema_version":1}`)
		bundleID := "sub2api-" + strings.ReplaceAll(component, "_", "-")
		if component == cityWorldVersionComponentEconomicPolicy {
			bindingMetadata = json.RawMessage(`{"schema_version":1,"projection_version":0}`)
		}
		if component == cityWorldVersionComponentWorldgenPlan {
			bindingMetadata = json.RawMessage(`{"schema_version":1,"context_hash":"` + strings.Repeat("b", 64) + `","bootstrap_plan_hash":"` + strings.Repeat("a", 64) + `"}`)
		}
		if component == cityWorldVersionComponentContentCatalog {
			bindingMetadata = metadata
			bundleID = cityWorldVersionContentCatalogV9BundleID
		}
		vector.Bindings = append(vector.Bindings, CityWorldVersionBinding{
			ComponentCode: component, BundleID: bundleID, BundleVersion: "1.0.0",
			ContentHash: strings.Repeat("a", 64), Metadata: bindingMetadata,
		})
	}
	require.NoError(t, validateCityWorldVersionVector(vector))
	for index := range vector.Bindings {
		if vector.Bindings[index].ComponentCode == cityWorldVersionComponentContentCatalog {
			vector.Bindings[index].Metadata = json.RawMessage(`{"schema_version":1,"service_profile_hash":"` + strings.Repeat("a", 64) + `","service_profile_version":"1.0.0","impact_profile_hash":"` + strings.Repeat("b", 64) + `","impact_profile_version":"1.0.0"}`)
		}
	}
	require.Error(t, validateCityWorldVersionVector(vector))
}

func TestCityWorldVersionVectorRejectsIncompleteV8ImpactMetadata(t *testing.T) {
	metadata, err := cityWorldVersionBindingMetadataWithServiceAndImpact(
		nil, "", "", strings.Repeat("a", 64), "1.0.0", strings.Repeat("b", 64), "1.0.0",
	)
	require.NoError(t, err)
	vector := CityWorldVersionVector{
		SchemaVersion: cityWorldVersionVectorSchemaVersion,
		Generation:    3,
		Bindings:      make([]CityWorldVersionBinding, 0, len(cityWorldVersionVectorComponentOrder)),
	}
	for _, component := range cityWorldVersionVectorComponentOrder {
		bindingMetadata := json.RawMessage(`{"schema_version":1}`)
		bundleID := "sub2api-" + strings.ReplaceAll(component, "_", "-")
		if component == cityWorldVersionComponentEconomicPolicy {
			bindingMetadata = json.RawMessage(`{"schema_version":1,"projection_version":0}`)
		}
		if component == cityWorldVersionComponentWorldgenPlan {
			bindingMetadata = json.RawMessage(`{"schema_version":1,"context_hash":"` + strings.Repeat("b", 64) + `","bootstrap_plan_hash":"` + strings.Repeat("a", 64) + `"}`)
		}
		if component == cityWorldVersionComponentContentCatalog {
			bindingMetadata = metadata
			bundleID = cityWorldVersionContentCatalogV8BundleID
		}
		vector.Bindings = append(vector.Bindings, CityWorldVersionBinding{
			ComponentCode: component,
			BundleID:      bundleID,
			BundleVersion: "1.0.0",
			ContentHash:   strings.Repeat("a", 64),
			Metadata:      bindingMetadata,
		})
	}
	require.NoError(t, validateCityWorldVersionVector(vector))
	for index := range vector.Bindings {
		if vector.Bindings[index].ComponentCode == cityWorldVersionComponentContentCatalog {
			vector.Bindings[index].Metadata = json.RawMessage(`{"schema_version":1,"service_profile_hash":"` + strings.Repeat("a", 64) + `","service_profile_version":"1.0.0"}`)
		}
	}
	require.Error(t, validateCityWorldVersionVector(vector))
}

func TestCityWorldVersionVectorRequiresCanonicalCompleteBindingSet(t *testing.T) {
	metadata := json.RawMessage(`{"schema_version":1}`)
	vector := CityWorldVersionVector{
		SchemaVersion: cityWorldVersionVectorSchemaVersion,
		Generation:    1,
		BaselineTick:  0,
		Bindings:      make([]CityWorldVersionBinding, 0, len(cityWorldVersionVectorComponentOrder)),
	}
	for _, component := range cityWorldVersionVectorComponentOrder {
		bindingMetadata := metadata
		if component == cityWorldVersionComponentEconomicPolicy {
			bindingMetadata = json.RawMessage(`{"schema_version":1,"projection_version":0}`)
		}
		if component == cityWorldVersionComponentWorldgenPlan {
			bindingMetadata = json.RawMessage(`{"schema_version":1,"context_hash":"` + strings.Repeat("b", 64) + `","bootstrap_plan_hash":"` + strings.Repeat("a", 64) + `"}`)
		}
		vector.Bindings = append(vector.Bindings, CityWorldVersionBinding{
			ComponentCode: component,
			BundleID:      "sub2api-" + strings.ReplaceAll(component, "_", "-"),
			BundleVersion: "1.0.0",
			ContentHash:   strings.Repeat("a", 64),
			Metadata:      bindingMetadata,
		})
	}
	require.NoError(t, validateCityWorldVersionVector(vector))

	missing := vector
	missing.Bindings = append([]CityWorldVersionBinding(nil), vector.Bindings[:len(vector.Bindings)-1]...)
	require.Error(t, validateCityWorldVersionVector(missing))

	wrongOrder := vector
	wrongOrder.Bindings = append([]CityWorldVersionBinding(nil), vector.Bindings...)
	wrongOrder.Bindings[0], wrongOrder.Bindings[1] = wrongOrder.Bindings[1], wrongOrder.Bindings[0]
	require.Error(t, validateCityWorldVersionVector(wrongOrder))

	badMetadata := vector
	badMetadata.Bindings = append([]CityWorldVersionBinding(nil), vector.Bindings...)
	badMetadata.Bindings[0].Metadata = json.RawMessage(`{"schema_version":2}`)
	require.Error(t, validateCityWorldVersionVector(badMetadata))

	badGeneration := vector
	badGeneration.Generation = 0
	require.Error(t, validateCityWorldVersionVector(badGeneration))

	badWorldgenMetadata := vector
	badWorldgenMetadata.Bindings = append([]CityWorldVersionBinding(nil), vector.Bindings...)
	for index := range badWorldgenMetadata.Bindings {
		if badWorldgenMetadata.Bindings[index].ComponentCode == cityWorldVersionComponentWorldgenPlan {
			badWorldgenMetadata.Bindings[index].Metadata = json.RawMessage(`{"schema_version":1,"context_hash":"` + strings.Repeat("b", 64) + `","bootstrap_plan_hash":"` + strings.Repeat("c", 64) + `"}`)
		}
	}
	require.Error(t, validateCityWorldVersionVector(badWorldgenMetadata))
}

func TestCityWorldVersionBindingMetadataPreservesLargeCarrierRecoveryAmount(t *testing.T) {
	recovery := newValidCityOpenWorldCarrierRecoveryState(t)
	commerce := newValidCityOpenWorldCarrierCommerceState(t)
	metadata, err := cityWorldVersionBindingMetadataWithCarrierRecovery(
		json.RawMessage(`{"schema_version":1}`), recovery.Policy,
	)
	require.NoError(t, err)
	metadata, err = cityWorldVersionBindingMetadataWithCarrierCommerce(metadata, commerce.Policy)
	require.NoError(t, err)
	var decoded struct {
		Amount int64 `json:"carrier_recovery_maximum_amount_units"`
	}
	require.NoError(t, json.Unmarshal(metadata, &decoded))
	require.Equal(t, cityOpenWorldCarrierRecoveryMaximumAmountUnits, decoded.Amount)
}
