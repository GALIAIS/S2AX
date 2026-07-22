package service

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/cityspatial"
	"github.com/stretchr/testify/require"
)

func TestCityEngineDefinitionsKeepLegacyAndCurrentPipelinesSeparate(t *testing.T) {
	f5, err := cityEngineForVersion(CitySimulationVersionF5)
	require.NoError(t, err)
	require.False(t, f5.hasStage(cityEngineStageCalendarDemography))
	require.Equal(t, []string{CitySimulationVersionF6}, cityEngineUpgradeTargets(CitySimulationVersionF5))

	f6, err := cityEngineForVersion(CitySimulationVersionF6)
	require.NoError(t, err)
	require.True(t, f6.hasStage(cityEngineStageCalendarDemography))
	require.Equal(t, []string{CitySimulationVersionF6V2}, cityEngineUpgradeTargets(CitySimulationVersionF6))
	require.True(t, cityEngineCanUpgrade(CitySimulationVersionF5, CitySimulationVersionF6))
	require.False(t, cityEngineCanUpgrade(CitySimulationVersionF6, CitySimulationVersionF5))
	require.True(t, f6.supportsCommand(CityCommandTypeWorldRename))
	require.True(t, f6.supportsCommand(CityCommandTypeLedgerCashTransfer))
	require.True(t, f6.supportsCommand(CityCommandTypeResourceTransfer))
	require.False(t, f6.supportsCommand(CityCommandTypePopulationRelocate))
	require.False(t, f6.supportsCommand("unknown.command"))

	realtime, err := cityEngineForVersion(CitySimulationVersionRealtimeV1)
	require.NoError(t, err)
	require.True(t, cityEngineIsRealtime(CitySimulationVersionRealtimeV1))
	require.False(t, cityEngineSupportsOpenWorld(CitySimulationVersionRealtimeV1))
	require.True(t, realtime.hasStage(cityEngineStageCalendarDemography))
	require.Empty(t, cityEngineUpgradeTargets(CitySimulationVersionRealtimeV1))
	require.False(t, cityEngineCanUpgrade(CitySimulationVersionOpenWorldV24, CitySimulationVersionRealtimeV1))
	realtimeStatic, err := cityEngineForVersion(CitySimulationVersionRealtimeV2)
	require.NoError(t, err)
	require.True(t, cityEngineIsRealtime(CitySimulationVersionRealtimeV2))
	require.True(t, cityEngineSupportsRealtimeStaticWorldgen(CitySimulationVersionRealtimeV2))
	require.False(t, cityEngineSupportsOpenWorld(CitySimulationVersionRealtimeV2))
	require.True(t, realtimeStatic.hasStage(cityEngineStageCalendarDemography))
	require.Empty(t, cityEngineUpgradeTargets(CitySimulationVersionRealtimeV2))
	require.False(t, cityEngineCanUpgrade(CitySimulationVersionOpenWorldV24, CitySimulationVersionRealtimeV2))

	openWorldV2, err := cityEngineForVersion(CitySimulationVersionOpenWorldV2)
	require.NoError(t, err)
	require.True(t, openWorldV2.hasStage(cityEngineStageOpenWorld))
	require.True(t, openWorldV2.supportsCommand(CityCommandTypeOpenWorldSectorMaterialize))
	require.False(t, f6.supportsCommand(CityCommandTypeOpenWorldSectorMaterialize))
	require.True(t, cityEngineSupportsOpenWorld(CitySimulationVersionOpenWorldV2))
	require.True(t, cityEngineSupportsOpenWorldMaterialization(CitySimulationVersionOpenWorldV2))
	require.False(t, cityEngineSupportsOpenWorldMaterialization(CitySimulationVersionOpenWorld))
	openWorldV3, err := cityEngineForVersion(CitySimulationVersionOpenWorldV3)
	require.NoError(t, err)
	require.True(t, openWorldV3.hasStage(cityEngineStageOpenWorld))
	require.True(t, openWorldV3.supportsCommand(CityCommandTypeOpenWorldSectorMaterialize))
	require.True(t, cityEngineSupportsOpenWorldMaterialization(CitySimulationVersionOpenWorldV3))
	require.True(t, cityEngineSupportsOpenWorldVerticalTopology(CitySimulationVersionOpenWorldV3))
	require.False(t, cityEngineSupportsOpenWorldVerticalTopology(CitySimulationVersionOpenWorldV2))

	openWorldV5, err := cityEngineForVersion(CitySimulationVersionOpenWorldV5)
	require.NoError(t, err)
	require.True(t, openWorldV5.hasStage(cityEngineStageOpenWorld))
	require.True(t, cityEngineSupportsOpenWorldRuntime(CitySimulationVersionOpenWorldV5))
	require.True(t, cityEngineSupportsOpenWorldSocialRuntime(CitySimulationVersionOpenWorldV5))
	require.Equal(t, []string{CitySimulationVersionOpenWorldV6}, cityEngineUpgradeTargets(CitySimulationVersionOpenWorldV5))

	openWorldV6, err := cityEngineForVersion(CitySimulationVersionOpenWorldV6)
	require.NoError(t, err)
	require.True(t, openWorldV6.hasStage(cityEngineStageOpenWorld))
	require.True(t, cityEngineSupportsOpenWorldMaterialization(CitySimulationVersionOpenWorldV6))
	require.True(t, cityEngineSupportsOpenWorldVerticalTopology(CitySimulationVersionOpenWorldV6))
	require.True(t, cityEngineSupportsOpenWorldRuntime(CitySimulationVersionOpenWorldV6))
	require.True(t, cityEngineSupportsOpenWorldSocialRuntime(CitySimulationVersionOpenWorldV6))
	require.True(t, cityEngineSupportsWorldVersionVector(CitySimulationVersionOpenWorldV6))
	require.Equal(t, []string{CitySimulationVersionOpenWorldV7}, cityEngineUpgradeTargets(CitySimulationVersionOpenWorldV6))
	require.True(t, cityEngineCanUpgrade(CitySimulationVersionOpenWorldV5, CitySimulationVersionOpenWorldV6))
	require.False(t, cityEngineCanUpgrade(CitySimulationVersionOpenWorldV6, CitySimulationVersionOpenWorldV5))

	openWorldV7, err := cityEngineForVersion(CitySimulationVersionOpenWorldV7)
	require.NoError(t, err)
	require.True(t, openWorldV7.hasStage(cityEngineStageOpenWorld))
	require.True(t, openWorldV7.hasStage(cityEngineStageOpenWorldServices))
	require.True(t, openWorldV7.supportsCommand(CityCommandTypeOpenWorldActorServiceRequest))
	require.False(t, openWorldV6.supportsCommand(CityCommandTypeOpenWorldActorServiceRequest))
	require.True(t, cityEngineSupportsOpenWorldServiceCoordination(CitySimulationVersionOpenWorldV7))
	require.Equal(t, []string{CitySimulationVersionOpenWorldV8}, cityEngineUpgradeTargets(CitySimulationVersionOpenWorldV7))
	require.True(t, cityEngineCanUpgrade(CitySimulationVersionOpenWorldV6, CitySimulationVersionOpenWorldV7))
	require.False(t, cityEngineCanUpgrade(CitySimulationVersionOpenWorldV7, CitySimulationVersionOpenWorldV6))

	openWorldV8, err := cityEngineForVersion(CitySimulationVersionOpenWorldV8)
	require.NoError(t, err)
	require.True(t, openWorldV8.hasStage(cityEngineStageOpenWorld))
	require.True(t, openWorldV8.hasStage(cityEngineStageOpenWorldServices))
	require.True(t, openWorldV8.hasStage(cityEngineStageOpenWorldImpacts))
	require.True(t, openWorldV8.supportsCommand(CityCommandTypeOpenWorldActorServiceRequest))
	require.True(t, cityEngineSupportsOpenWorldServiceCoordination(CitySimulationVersionOpenWorldV8))
	require.True(t, cityEngineSupportsOpenWorldImpactBridge(CitySimulationVersionOpenWorldV8))
	require.True(t, cityEngineCanUpgrade(CitySimulationVersionOpenWorldV7, CitySimulationVersionOpenWorldV8))
	require.False(t, cityEngineCanUpgrade(CitySimulationVersionOpenWorldV8, CitySimulationVersionOpenWorldV7))

	openWorldV9, err := cityEngineForVersion(CitySimulationVersionOpenWorldV9)
	require.NoError(t, err)
	require.True(t, openWorldV9.hasStage(cityEngineStageOpenWorld))
	require.True(t, openWorldV9.hasStage(cityEngineStageOpenWorldServices))
	require.True(t, openWorldV9.hasStage(cityEngineStageOpenWorldImpacts))
	require.True(t, openWorldV9.hasStage(cityEngineStageOpenWorldMobility))
	require.True(t, openWorldV9.supportsCommand(CityCommandTypeOpenWorldActorMobilityRequest))
	require.False(t, openWorldV8.supportsCommand(CityCommandTypeOpenWorldActorMobilityRequest))
	require.True(t, cityEngineSupportsOpenWorldMobility(CitySimulationVersionOpenWorldV9))
	require.Equal(t, []string{CitySimulationVersionOpenWorldV10}, cityEngineUpgradeTargets(CitySimulationVersionOpenWorldV9))
	require.True(t, cityEngineCanUpgrade(CitySimulationVersionOpenWorldV8, CitySimulationVersionOpenWorldV9))
	require.False(t, cityEngineCanUpgrade(CitySimulationVersionOpenWorldV9, CitySimulationVersionOpenWorldV8))

	openWorldV10, err := cityEngineForVersion(CitySimulationVersionOpenWorldV10)
	require.NoError(t, err)
	require.True(t, openWorldV10.hasStage(cityEngineStageOpenWorld))
	require.True(t, openWorldV10.hasStage(cityEngineStageOpenWorldMobility))
	require.True(t, openWorldV10.hasStage(cityEngineStageOpenWorldArrivals))
	require.True(t, cityEngineSupportsOpenWorldArrivalBridge(CitySimulationVersionOpenWorldV10))
	require.True(t, cityEngineCanUpgrade(CitySimulationVersionOpenWorldV9, CitySimulationVersionOpenWorldV10))
	require.False(t, cityEngineCanUpgrade(CitySimulationVersionOpenWorldV10, CitySimulationVersionOpenWorldV9))

	openWorldV11, err := cityEngineForVersion(CitySimulationVersionOpenWorldV11)
	require.NoError(t, err)
	require.True(t, openWorldV11.hasStage(cityEngineStageOpenWorld))
	require.True(t, openWorldV11.hasStage(cityEngineStageOpenWorldMobilityOD))
	require.True(t, openWorldV11.hasStage(cityEngineStageOpenWorldMobility))
	require.True(t, openWorldV11.hasStage(cityEngineStageOpenWorldArrivals))
	require.True(t, cityEngineSupportsOpenWorldMobilityOD(CitySimulationVersionOpenWorldV11))
	require.Equal(t, []string{CitySimulationVersionOpenWorldV11}, cityEngineUpgradeTargets(CitySimulationVersionOpenWorldV10))
	require.True(t, cityEngineCanUpgrade(CitySimulationVersionOpenWorldV10, CitySimulationVersionOpenWorldV11))
	require.False(t, cityEngineCanUpgrade(CitySimulationVersionOpenWorldV11, CitySimulationVersionOpenWorldV10))

	openWorldV12, err := cityEngineForVersion(CitySimulationVersionOpenWorldV12)
	require.NoError(t, err)
	require.True(t, openWorldV12.hasStage(cityEngineStageOpenWorld))
	require.True(t, openWorldV12.hasStage(cityEngineStageOpenWorldMobilityOD))
	require.True(t, openWorldV12.hasStage(cityEngineStageOpenWorldMobility))
	require.True(t, openWorldV12.hasStage(cityEngineStageOpenWorldArrivals))
	require.True(t, cityEngineSupportsOpenWorldMobilityOD(CitySimulationVersionOpenWorldV12))
	require.True(t, cityEngineSupportsOpenWorldCommuteBindings(CitySimulationVersionOpenWorldV12))
	require.Equal(t, []string{CitySimulationVersionOpenWorldV12}, cityEngineUpgradeTargets(CitySimulationVersionOpenWorldV11))
	require.True(t, cityEngineCanUpgrade(CitySimulationVersionOpenWorldV11, CitySimulationVersionOpenWorldV12))
	require.False(t, cityEngineCanUpgrade(CitySimulationVersionOpenWorldV12, CitySimulationVersionOpenWorldV11))

	openWorldV13, err := cityEngineForVersion(CitySimulationVersionOpenWorldV13)
	require.NoError(t, err)
	require.True(t, openWorldV13.hasStage(cityEngineStageOpenWorld))
	require.True(t, openWorldV13.hasStage(cityEngineStageOpenWorldMobilityOD))
	require.True(t, openWorldV13.hasStage(cityEngineStageOpenWorldCommutes))
	require.True(t, openWorldV13.hasStage(cityEngineStageOpenWorldMobility))
	require.True(t, openWorldV13.hasStage(cityEngineStageOpenWorldArrivals))
	require.True(t, cityEngineSupportsOpenWorldCommuteBindings(CitySimulationVersionOpenWorldV13))
	require.True(t, cityEngineSupportsOpenWorldCommuteSources(CitySimulationVersionOpenWorldV13))
	require.Equal(t, []string{CitySimulationVersionOpenWorldV13}, cityEngineUpgradeTargets(CitySimulationVersionOpenWorldV12))
	require.True(t, cityEngineCanUpgrade(CitySimulationVersionOpenWorldV12, CitySimulationVersionOpenWorldV13))
	require.False(t, cityEngineCanUpgrade(CitySimulationVersionOpenWorldV13, CitySimulationVersionOpenWorldV12))

	openWorldV14, err := cityEngineForVersion(CitySimulationVersionOpenWorldV14)
	require.NoError(t, err)
	require.True(t, openWorldV14.hasStage(cityEngineStageOpenWorld))
	require.True(t, openWorldV14.hasStage(cityEngineStageOpenWorldCommuteLifecycle))
	require.True(t, cityEngineSupportsOpenWorldCommuteBindings(CitySimulationVersionOpenWorldV14))
	require.True(t, cityEngineSupportsOpenWorldCommuteSources(CitySimulationVersionOpenWorldV14))
	require.True(t, cityEngineSupportsOpenWorldCommuteLifecycle(CitySimulationVersionOpenWorldV14))
	require.True(t, openWorldV14.supportsCommand(CityCommandTypeOpenWorldCommuteAssignmentRebind))
	require.True(t, openWorldV14.supportsCommand(CityCommandTypeOpenWorldCommuteAssignmentSetState))
	require.False(t, openWorldV13.supportsCommand(CityCommandTypeOpenWorldCommuteAssignmentRebind))
	require.Equal(t, []string{CitySimulationVersionOpenWorldV14}, cityEngineUpgradeTargets(CitySimulationVersionOpenWorldV13))
	require.True(t, cityEngineCanUpgrade(CitySimulationVersionOpenWorldV13, CitySimulationVersionOpenWorldV14))
	require.False(t, cityEngineCanUpgrade(CitySimulationVersionOpenWorldV14, CitySimulationVersionOpenWorldV13))

	openWorldV15, err := cityEngineForVersion(CitySimulationVersionOpenWorldV15)
	require.NoError(t, err)
	require.True(t, openWorldV15.hasStage(cityEngineStageOpenWorld))
	require.True(t, openWorldV15.hasStage(cityEngineStageOpenWorldCommuteLifecycle))
	require.True(t, openWorldV15.hasStage(cityEngineStageOpenWorldSupplyChain))
	require.True(t, cityEngineSupportsOpenWorldSupplyChain(CitySimulationVersionOpenWorldV15))
	require.True(t, openWorldV15.supportsCommand(CityCommandTypeOpenWorldSupplyOrderCreate))
	require.True(t, openWorldV15.supportsCommand(CityCommandTypeOpenWorldSupplyOrderAccept))
	require.False(t, openWorldV14.supportsCommand(CityCommandTypeOpenWorldSupplyOrderCreate))
	require.Equal(t, []string{CitySimulationVersionOpenWorldV15}, cityEngineUpgradeTargets(CitySimulationVersionOpenWorldV14))
	require.True(t, cityEngineCanUpgrade(CitySimulationVersionOpenWorldV14, CitySimulationVersionOpenWorldV15))
	require.False(t, cityEngineCanUpgrade(CitySimulationVersionOpenWorldV15, CitySimulationVersionOpenWorldV14))

	openWorldV16, err := cityEngineForVersion(CitySimulationVersionOpenWorldV16)
	require.NoError(t, err)
	require.True(t, openWorldV16.hasStage(cityEngineStageOpenWorld))
	require.True(t, openWorldV16.hasStage(cityEngineStageOpenWorldSupplyChain))
	require.True(t, openWorldV16.hasStage(cityEngineStageOpenWorldEnterpriseFreight))
	require.True(t, cityEngineSupportsOpenWorldSupplyChain(CitySimulationVersionOpenWorldV16))
	require.True(t, cityEngineSupportsOpenWorldEnterpriseFreight(CitySimulationVersionOpenWorldV16))
	require.Equal(t, []string{CitySimulationVersionOpenWorldV16}, cityEngineUpgradeTargets(CitySimulationVersionOpenWorldV15))
	require.True(t, cityEngineCanUpgrade(CitySimulationVersionOpenWorldV15, CitySimulationVersionOpenWorldV16))
	require.False(t, cityEngineCanUpgrade(CitySimulationVersionOpenWorldV16, CitySimulationVersionOpenWorldV15))

	openWorldV17, err := cityEngineForVersion(CitySimulationVersionOpenWorldV17)
	require.NoError(t, err)
	require.True(t, openWorldV17.hasStage(cityEngineStageOpenWorld))
	require.True(t, openWorldV17.hasStage(cityEngineStageOpenWorldSupplyChain))
	require.True(t, openWorldV17.hasStage(cityEngineStageOpenWorldEnterpriseFreight))
	require.True(t, openWorldV17.hasStage(cityEngineStageOpenWorldEnterpriseFreightReceipts))
	require.True(t, cityEngineSupportsOpenWorldEnterpriseFreight(CitySimulationVersionOpenWorldV17))
	require.True(t, cityEngineSupportsOpenWorldEnterpriseFreightReceipts(CitySimulationVersionOpenWorldV17))
	require.Equal(t, []string{CitySimulationVersionOpenWorldV17}, cityEngineUpgradeTargets(CitySimulationVersionOpenWorldV16))
	require.True(t, cityEngineCanUpgrade(CitySimulationVersionOpenWorldV16, CitySimulationVersionOpenWorldV17))
	require.False(t, cityEngineCanUpgrade(CitySimulationVersionOpenWorldV17, CitySimulationVersionOpenWorldV16))

	openWorldV18, err := cityEngineForVersion(CitySimulationVersionOpenWorldV18)
	require.NoError(t, err)
	require.True(t, openWorldV18.hasStage(cityEngineStageOpenWorld))
	require.True(t, openWorldV18.hasStage(cityEngineStageOpenWorldSupplyChain))
	require.True(t, openWorldV18.hasStage(cityEngineStageOpenWorldEnterpriseFreight))
	require.True(t, openWorldV18.hasStage(cityEngineStageOpenWorldEnterpriseFreightReceipts))
	require.True(t, openWorldV18.hasStage(cityEngineStageOpenWorldEnterpriseFreightBatches))
	require.True(t, cityEngineSupportsOpenWorldFreightBatches(CitySimulationVersionOpenWorldV18))
	require.Equal(t, []string{CitySimulationVersionOpenWorldV18}, cityEngineUpgradeTargets(CitySimulationVersionOpenWorldV17))
	require.True(t, cityEngineCanUpgrade(CitySimulationVersionOpenWorldV17, CitySimulationVersionOpenWorldV18))
	require.False(t, cityEngineCanUpgrade(CitySimulationVersionOpenWorldV18, CitySimulationVersionOpenWorldV17))

	openWorldV19, err := cityEngineForVersion(CitySimulationVersionOpenWorldV19)
	require.NoError(t, err)
	require.True(t, openWorldV19.hasStage(cityEngineStageOpenWorld))
	require.True(t, openWorldV19.hasStage(cityEngineStageOpenWorldSupplyChain))
	require.True(t, openWorldV19.hasStage(cityEngineStageOpenWorldEnterpriseFreight))
	require.True(t, openWorldV19.hasStage(cityEngineStageOpenWorldEnterpriseFreightReceipts))
	require.True(t, openWorldV19.hasStage(cityEngineStageOpenWorldEnterpriseFreightBatches))
	require.True(t, cityEngineSupportsOpenWorldFreightBatches(CitySimulationVersionOpenWorldV19))
	require.True(t, cityEngineSupportsOpenWorldSpatialNetwork(CitySimulationVersionOpenWorldV19))
	require.False(t, cityEngineSupportsOpenWorldSpatialNetwork(CitySimulationVersionOpenWorldV18))
	require.Equal(t, []string{CitySimulationVersionOpenWorldV19}, cityEngineUpgradeTargets(CitySimulationVersionOpenWorldV18))
	require.True(t, cityEngineCanUpgrade(CitySimulationVersionOpenWorldV18, CitySimulationVersionOpenWorldV19))
	require.False(t, cityEngineCanUpgrade(CitySimulationVersionOpenWorldV19, CitySimulationVersionOpenWorldV18))

	openWorldV20, err := cityEngineForVersion(CitySimulationVersionOpenWorldV20)
	require.NoError(t, err)
	require.True(t, openWorldV20.hasStage(cityEngineStageOpenWorld))
	require.True(t, openWorldV20.hasStage(cityEngineStageOpenWorldSupplyChain))
	require.True(t, openWorldV20.hasStage(cityEngineStageOpenWorldEnterpriseFreight))
	require.True(t, openWorldV20.hasStage(cityEngineStageOpenWorldEnterpriseFreightReceipts))
	require.True(t, openWorldV20.hasStage(cityEngineStageOpenWorldEnterpriseFreightBatches))
	require.True(t, openWorldV20.hasStage(cityEngineStageOpenWorldInfrastructure))
	require.True(t, cityEngineSupportsOpenWorldSpatialNetwork(CitySimulationVersionOpenWorldV20))
	require.True(t, cityEngineSupportsOpenWorldInfrastructure(CitySimulationVersionOpenWorldV20))
	require.False(t, cityEngineSupportsOpenWorldInfrastructure(CitySimulationVersionOpenWorldV19))
	require.Equal(t, []string{CitySimulationVersionOpenWorldV20}, cityEngineUpgradeTargets(CitySimulationVersionOpenWorldV19))
	require.Equal(t, []string{CitySimulationVersionOpenWorldV21}, cityEngineUpgradeTargets(CitySimulationVersionOpenWorldV20))
	require.True(t, cityEngineCanUpgrade(CitySimulationVersionOpenWorldV20, CitySimulationVersionOpenWorldV21))
	require.False(t, cityEngineCanUpgrade(CitySimulationVersionOpenWorldV21, CitySimulationVersionOpenWorldV20))

	openWorldV21, err := cityEngineForVersion(CitySimulationVersionOpenWorldV21)
	require.NoError(t, err)
	require.True(t, openWorldV21.hasStage(cityEngineStageOpenWorldInfrastructure))
	require.True(t, openWorldV21.hasStage(cityEngineStageOpenWorldMobility))
	require.True(t, openWorldV21.hasStage(cityEngineStageOpenWorldEffectiveCapacity))
	require.True(t, cityEngineSupportsOpenWorldEffectiveCapacity(CitySimulationVersionOpenWorldV21))
	require.False(t, cityEngineSupportsOpenWorldEffectiveCapacity(CitySimulationVersionOpenWorldV20))
	require.Equal(t, []string{CitySimulationVersionOpenWorldV22}, cityEngineUpgradeTargets(CitySimulationVersionOpenWorldV21))
	require.True(t, cityEngineCanUpgrade(CitySimulationVersionOpenWorldV21, CitySimulationVersionOpenWorldV22))
	require.False(t, cityEngineCanUpgrade(CitySimulationVersionOpenWorldV22, CitySimulationVersionOpenWorldV21))

	openWorldV22, err := cityEngineForVersion(CitySimulationVersionOpenWorldV22)
	require.NoError(t, err)
	require.True(t, openWorldV22.hasStage(cityEngineStageOpenWorldInfrastructure))
	require.True(t, openWorldV22.hasStage(cityEngineStageOpenWorldMobility))
	require.True(t, openWorldV22.hasStage(cityEngineStageOpenWorldEffectiveCapacity))
	require.True(t, openWorldV22.hasStage(cityEngineStageOpenWorldFreightSettlements))
	require.True(t, openWorldV22.supportsCommand(CityCommandTypeOpenWorldFreightSettlementReceipt))
	require.True(t, cityEngineSupportsOpenWorldFreightSettlements(CitySimulationVersionOpenWorldV22))
	require.False(t, cityEngineSupportsOpenWorldFreightSettlements(CitySimulationVersionOpenWorldV21))
	require.Equal(t, []string{CitySimulationVersionOpenWorldV23}, cityEngineUpgradeTargets(CitySimulationVersionOpenWorldV22))
	require.True(t, cityEngineCanUpgrade(CitySimulationVersionOpenWorldV22, CitySimulationVersionOpenWorldV23))
	require.False(t, cityEngineCanUpgrade(CitySimulationVersionOpenWorldV23, CitySimulationVersionOpenWorldV22))

	openWorldV23, err := cityEngineForVersion(CitySimulationVersionOpenWorldV23)
	require.NoError(t, err)
	require.True(t, openWorldV23.hasStage(cityEngineStageOpenWorldFreightSettlements))
	require.True(t, openWorldV23.hasStage(cityEngineStageOpenWorldCarrierRecovery))
	require.True(t, openWorldV23.supportsCommand(CityCommandTypeOpenWorldCarrierReserveFund))
	require.True(t, openWorldV23.supportsCommand(CityCommandTypeOpenWorldFreightClaimResolve))
	require.True(t, cityEngineSupportsOpenWorldCarrierRecovery(CitySimulationVersionOpenWorldV23))
	require.False(t, cityEngineSupportsOpenWorldCarrierRecovery(CitySimulationVersionOpenWorldV22))
	require.Equal(t, []string{CitySimulationVersionOpenWorldV24}, cityEngineUpgradeTargets(CitySimulationVersionOpenWorldV23))
	require.True(t, cityEngineCanUpgrade(CitySimulationVersionOpenWorldV23, CitySimulationVersionOpenWorldV24))
	require.False(t, cityEngineCanUpgrade(CitySimulationVersionOpenWorldV24, CitySimulationVersionOpenWorldV23))

	openWorldV24, err := cityEngineForVersion(CitySimulationVersionOpenWorldV24)
	require.NoError(t, err)
	require.True(t, openWorldV24.hasStage(cityEngineStageOpenWorldFreightSettlements))
	require.True(t, openWorldV24.hasStage(cityEngineStageOpenWorldCarrierRecovery))
	require.True(t, openWorldV24.hasStage(cityEngineStageOpenWorldCarrierCommerce))
	require.True(t, cityEngineSupportsOpenWorldCarrierCommerce(CitySimulationVersionOpenWorldV24))
	require.False(t, cityEngineSupportsOpenWorldCarrierCommerce(CitySimulationVersionOpenWorldV23))
	require.Equal(t, CitySimulationVersionOpenWorldV24, CurrentCitySimulationVersion)
	require.True(t, cityEngineCanUpgrade(CitySimulationVersionOpenWorldV19, CitySimulationVersionOpenWorldV20))
	require.False(t, cityEngineCanUpgrade(CitySimulationVersionOpenWorldV20, CitySimulationVersionOpenWorldV19))

	f6v2, err := cityEngineForVersion(CitySimulationVersionF6V2)
	require.NoError(t, err)
	require.True(t, f6v2.hasStage(cityEngineStageCalendarDemography))
	require.True(t, f6v2.supportsCommand(CityCommandTypePopulationImmigrate))
	require.True(t, f6v2.supportsCommand(CityCommandTypePopulationEmigrate))
	require.True(t, f6v2.supportsCommand(CityCommandTypePopulationRelocate))
	require.False(t, f6v2.supportsCommand(CityCommandTypeHouseholdAdjust))
	require.Equal(t, []string{CitySimulationVersionF6V3}, cityEngineUpgradeTargets(CitySimulationVersionF6V2))
	require.True(t, cityEngineCanUpgrade(CitySimulationVersionF6, CitySimulationVersionF6V2))
	require.False(t, cityEngineCanUpgrade(CitySimulationVersionF6V2, CitySimulationVersionF6))

	f6v3, err := cityEngineForVersion(CitySimulationVersionF6V3)
	require.NoError(t, err)
	require.True(t, f6v3.supportsCommand(CityCommandTypePopulationRelocate))
	require.True(t, f6v3.supportsCommand(CityCommandTypeHouseholdAdjust))
	require.True(t, f6v3.supportsCommand(CityCommandTypeHouseholdReclassify))
	require.False(t, f6v3.supportsCommand(CityCommandTypeSpatialGenerateChunk))
	require.Equal(t, []string{CitySimulationVersionF7}, cityEngineUpgradeTargets(CitySimulationVersionF6V3))
	require.True(t, cityEngineCanUpgrade(CitySimulationVersionF6V2, CitySimulationVersionF6V3))

	f7, err := cityEngineForVersion(CitySimulationVersionF7)
	require.NoError(t, err)
	require.True(t, f7.hasStage(cityEngineStageSpatial))
	require.True(t, f7.supportsCommand(CityCommandTypePopulationRelocate))
	require.True(t, f7.supportsCommand(CityCommandTypeHouseholdAdjust))
	require.True(t, f7.supportsCommand(CityCommandTypeSpatialGenerateChunk))
	require.Equal(t, []string{CitySimulationVersionF7V2}, cityEngineUpgradeTargets(CitySimulationVersionF7))
	require.True(t, cityEngineCanUpgrade(CitySimulationVersionF6V3, CitySimulationVersionF7))
	require.False(t, cityEngineCanUpgrade(CitySimulationVersionF7, CitySimulationVersionF6V3))

	f7v2, err := cityEngineForVersion(CitySimulationVersionF7V2)
	require.NoError(t, err)
	require.True(t, f7v2.hasStage(cityEngineStageSpatial))
	require.True(t, f7v2.supportsCommand(CityCommandTypeSpatialGenerateChunk))
	require.False(t, f7v2.supportsCommand(CityCommandTypeDevelopmentSubmit))
	require.Equal(t, []string{CitySimulationVersionF7V3}, cityEngineUpgradeTargets(CitySimulationVersionF7V2))
	require.True(t, cityEngineCanUpgrade(CitySimulationVersionF7, CitySimulationVersionF7V2))
	require.True(t, cityEngineCanUpgrade(CitySimulationVersionF7V2, CitySimulationVersionF7V3))

	f7v3, err := cityEngineForVersion(CitySimulationVersionF7V3)
	require.NoError(t, err)
	require.True(t, f7v3.hasStage(cityEngineStageSpatial))
	require.True(t, cityEngineSupportsLand(CitySimulationVersionF7V3))
	require.True(t, f7v3.hasStage(cityEngineStageDevelopment))
	require.True(t, f7v3.supportsCommand(CityCommandTypeDevelopmentSubmit))
	require.True(t, f7v3.supportsCommand(CityCommandTypeDevelopmentReview))
	require.True(t, f7v3.supportsCommand(CityCommandTypeDevelopmentStart))
	require.True(t, f7v3.supportsCommand(CityCommandTypeDevelopmentCancel))
	require.False(t, f7v3.supportsCommand(CityCommandTypeEnterpriseSiteOpen))
	require.Equal(t, []string{CitySimulationVersionF7V4}, cityEngineUpgradeTargets(CitySimulationVersionF7V3))

	f7v4, err := cityEngineForVersion(CitySimulationVersionF7V4)
	require.NoError(t, err)
	require.True(t, f7v4.hasStage(cityEngineStageEnterpriseLocation))
	require.True(t, f7v4.supportsCommand(CityCommandTypeDevelopmentSubmit))
	require.True(t, f7v4.supportsCommand(CityCommandTypeEnterpriseSiteOpen))
	require.True(t, f7v4.supportsCommand(CityCommandTypeEnterpriseSiteResize))
	require.True(t, f7v4.supportsCommand(CityCommandTypeEnterpriseSiteClose))
	require.True(t, f7v4.supportsCommand(CityCommandTypeEnterpriseRelocate))
	require.Equal(t, []string{CitySimulationVersionF7V5}, cityEngineUpgradeTargets(CitySimulationVersionF7V4))
	require.True(t, cityEngineCanUpgrade(CitySimulationVersionF7V3, CitySimulationVersionF7V4))

	f7v5, err := cityEngineForVersion(CitySimulationVersionF7V5)
	require.NoError(t, err)
	require.True(t, f7v5.hasStage(cityEngineStageWorldRuntime))
	require.True(t, f7v5.supportsCommand(CityCommandTypeActorCreate))
	require.True(t, f7v5.supportsCommand(CityCommandTypeActorActivityPerform))
	require.True(t, f7v5.supportsCommand(CityCommandTypeActorRoleTransition))
	require.False(t, f7v5.supportsCommand(CityCommandTypeActorLocationMove))
	require.Equal(t, []string{CitySimulationVersionF7V6}, cityEngineUpgradeTargets(CitySimulationVersionF7V5))
	require.True(t, cityEngineCanUpgrade(CitySimulationVersionF7V4, CitySimulationVersionF7V5))
	require.False(t, f7v4.supportsCommand(CityCommandTypeActorCreate))

	f7v6, err := cityEngineForVersion(CitySimulationVersionF7V6)
	require.NoError(t, err)
	require.True(t, f7v6.hasStage(cityEngineStageWorldRuntime))
	require.True(t, f7v6.supportsCommand(CityCommandTypeActorLocationMove))
	require.True(t, f7v6.supportsCommand(CityCommandTypeActorControlGrant))
	require.True(t, f7v6.supportsCommand(CityCommandTypeActorControlRevoke))
	require.Equal(t, []string{CitySimulationVersionF7V7}, cityEngineUpgradeTargets(CitySimulationVersionF7V6))
	require.True(t, cityEngineCanUpgrade(CitySimulationVersionF7V5, CitySimulationVersionF7V6))
	require.False(t, cityEngineSupportsWorldActorNavigation(CitySimulationVersionF7V6))

	f7v7, err := cityEngineForVersion(CitySimulationVersionF7V7)
	require.NoError(t, err)
	require.True(t, f7v7.hasStage(cityEngineStageWorldRuntime))
	require.True(t, f7v7.supportsCommand(CityCommandTypeActorLocationMove))
	require.True(t, cityEngineSupportsWorldActorNavigation(CitySimulationVersionF7V7))
	require.False(t, cityEngineSupportsWorldPortalAccess(CitySimulationVersionF7V7))
	require.Equal(t, []string{CitySimulationVersionF7V8}, cityEngineUpgradeTargets(CitySimulationVersionF7V7))
	require.True(t, cityEngineCanUpgrade(CitySimulationVersionF7V6, CitySimulationVersionF7V7))

	f7v8, err := cityEngineForVersion(CitySimulationVersionF7V8)
	require.NoError(t, err)
	require.True(t, f7v8.hasStage(cityEngineStageWorldRuntime))
	require.True(t, f7v8.supportsCommand(CityCommandTypeActorLocationMove))
	require.True(t, f7v8.supportsCommand(CityCommandTypePortalStateTransition))
	require.True(t, f7v8.supportsCommand(CityCommandTypePortalAccessConfigure))
	require.True(t, cityEngineSupportsWorldActorNavigation(CitySimulationVersionF7V8))
	require.True(t, cityEngineSupportsWorldPortalAccess(CitySimulationVersionF7V8))
	require.False(t, cityEngineSupportsWorldNavigationIntents(CitySimulationVersionF7V8))
	require.Equal(t, []string{CitySimulationVersionF7V9}, cityEngineUpgradeTargets(CitySimulationVersionF7V8))
	require.True(t, cityEngineCanUpgrade(CitySimulationVersionF7V7, CitySimulationVersionF7V8))

	f7v9, err := cityEngineForVersion(CitySimulationVersionF7V9)
	require.NoError(t, err)
	require.True(t, f7v9.supportsCommand(CityCommandTypeActorNavigationIntentSet))
	require.True(t, f7v9.supportsCommand(CityCommandTypeActorNavigationIntentCancel))
	require.True(t, cityEngineSupportsWorldNavigationIntents(CitySimulationVersionF7V9))
	require.False(t, f7v9.supportsCommand(CityCommandTypeFacilityRegister))
	require.Equal(t, []string{CitySimulationVersionF8}, cityEngineUpgradeTargets(CitySimulationVersionF7V9))
	require.True(t, cityEngineCanUpgrade(CitySimulationVersionF7V8, CitySimulationVersionF7V9))

	f8, err := cityEngineForVersion(CitySimulationVersionF8)
	require.NoError(t, err)
	require.True(t, f8.hasStage(cityEngineStagePublicServices))
	require.True(t, f8.supportsCommand(CityCommandTypeFacilityRegister))
	require.True(t, f8.supportsCommand(CityCommandTypeFacilityStatusTransition))
	require.True(t, f8.supportsCommand(CityCommandTypeFacilityCapacityConfigure))
	require.True(t, f8.supportsCommand(CityCommandTypeServiceDemandConfigure))
	require.True(t, f8.supportsCommand(CityCommandTypeServiceConnectionConfigure))
	require.True(t, cityEngineSupportsWorldActorSpatialControl(CitySimulationVersionF8))
	require.True(t, cityEngineSupportsWorldActorNavigation(CitySimulationVersionF8))
	require.True(t, cityEngineSupportsWorldPortalAccess(CitySimulationVersionF8))
	require.True(t, cityEngineSupportsWorldNavigationIntents(CitySimulationVersionF8))
	require.True(t, cityEngineSupportsPublicServices(CitySimulationVersionF8))
	require.False(t, cityEngineSupportsFacilityLifecycle(CitySimulationVersionF8))
	require.Equal(t, []string{CitySimulationVersionF8V2}, cityEngineUpgradeTargets(CitySimulationVersionF8))
	require.True(t, cityEngineCanUpgrade(CitySimulationVersionF7V9, CitySimulationVersionF8))
	require.True(t, cityEngineCanUpgrade(CitySimulationVersionF8, CitySimulationVersionF8V2))

	f8v2, err := cityEngineForVersion(CitySimulationVersionF8V2)
	require.NoError(t, err)
	require.True(t, f8v2.hasStage(cityEngineStagePublicServices))
	require.True(t, f8v2.supportsCommand(CityCommandTypeFacilityOperationSchedule))
	require.True(t, f8v2.supportsCommand(CityCommandTypeFacilityOperationStart))
	require.True(t, f8v2.supportsCommand(CityCommandTypeFacilityOperationCancel))
	require.True(t, f8v2.supportsCommand(CityCommandTypeFacilityStaffingConfigure))
	require.True(t, cityEngineSupportsFacilityLifecycle(CitySimulationVersionF8V2))
	require.False(t, f8v2.supportsCommand(CityCommandTypePhysicalNetworkConfigure))
	require.Equal(t, []string{CitySimulationVersionF8V3}, cityEngineUpgradeTargets(CitySimulationVersionF8V2))
	require.True(t, cityEngineCanUpgrade(CitySimulationVersionF8V2, CitySimulationVersionF8V3))

	f8v3, err := cityEngineForVersion(CitySimulationVersionF8V3)
	require.NoError(t, err)
	require.True(t, f8v3.hasStage(cityEngineStagePublicServices))
	require.True(t, cityEngineSupportsFacilityLifecycle(CitySimulationVersionF8V3))
	require.True(t, cityEngineSupportsPhysicalNetworks(CitySimulationVersionF8V3))
	require.True(t, f8v3.supportsCommand(CityCommandTypePhysicalNetworkConfigure))
	require.True(t, f8v3.supportsCommand(CityCommandTypePhysicalNodeConfigure))
	require.True(t, f8v3.supportsCommand(CityCommandTypePhysicalEdgeConfigure))
	require.True(t, f8v3.supportsCommand(CityCommandTypePhysicalEdgeTransition))
	require.Empty(t, cityEngineUpgradeTargets(CitySimulationVersionF8V3))

	_, err = cityEngineForVersion("city-unknown-v1")
	require.Error(t, err)
}

func TestCityEngineDefinitionRejectsInvalidSubsystemGraphs(t *testing.T) {
	for _, engine := range []cityEngineDefinition{
		{version: "missing-control", stages: []cityEngineStage{cityEngineStageLedger}},
		{version: "duplicate", stages: []cityEngineStage{cityEngineStageControl, cityEngineStageControl}},
		{version: "missing-ledger", stages: []cityEngineStage{cityEngineStageControl, cityEngineStageResources}},
		{version: "wrong-order", stages: []cityEngineStage{
			cityEngineStageControl, cityEngineStageLedger, cityEngineStageMarkets, cityEngineStageResources,
		}},
		{version: "unknown", stages: []cityEngineStage{cityEngineStageControl, "mystery"}},
		{version: "service-without-runtime", stages: []cityEngineStage{
			cityEngineStageControl, cityEngineStageLedger, cityEngineStageResources,
			cityEngineStagePublicServices, cityEngineStageMarkets,
		}},
		{version: "impact-without-services", stages: []cityEngineStage{
			cityEngineStageControl, cityEngineStageLedger, cityEngineStageResources,
			cityEngineStageCalendarDemography, cityEngineStageOpenWorld,
			cityEngineStageOpenWorldImpacts, cityEngineStageMarkets,
		}},
		{version: "mobility-without-impacts", stages: []cityEngineStage{
			cityEngineStageControl, cityEngineStageLedger, cityEngineStageResources,
			cityEngineStageCalendarDemography, cityEngineStageOpenWorld,
			cityEngineStageOpenWorldServices, cityEngineStageOpenWorldMobility, cityEngineStageMarkets,
		}},
	} {
		require.Error(t, engine.validate(), engine.version)
	}
}

func TestMarshalCanonicalCityStatePreservesVersionedShape(t *testing.T) {
	f5, err := marshalCanonicalCityState(cityHashState{
		SimulationVersion: CitySimulationVersionF5,
		Demography:        cityDemographyHashState{Calendar: cityDemographyHashCalendar{LocalDate: "2000-01-01"}},
	})
	require.NoError(t, err)
	require.NotContains(t, string(f5), `"demography"`)

	f6, err := marshalCanonicalCityState(cityHashState{
		SimulationVersion: CitySimulationVersionF6,
		Demography:        cityDemographyHashState{Calendar: cityDemographyHashCalendar{LocalDate: "2000-01-01"}},
	})
	require.NoError(t, err)
	require.Contains(t, string(f6), `"demography"`)

	f6v2, err := marshalCanonicalCityState(cityHashState{
		SimulationVersion: CitySimulationVersionF6V2,
		Demography:        cityDemographyHashState{Calendar: cityDemographyHashCalendar{LocalDate: "2000-01-01"}},
		Physical:          cityPhysicalHashState{HouseholdCohorts: []cityHashHouseholdCohort{{HouseholdUnits: 2}}},
	})
	require.NoError(t, err)
	require.Contains(t, string(f6v2), `"demography"`)
	require.NotContains(t, string(f6v2), `"household_units"`)
	require.NotEqual(t, string(f6), string(f6v2))

	f6v3, err := marshalCanonicalCityState(cityHashState{
		SimulationVersion: CitySimulationVersionF6V3,
		Physical:          cityPhysicalHashState{HouseholdCohorts: []cityHashHouseholdCohort{{HouseholdUnits: 2}}},
	})
	require.NoError(t, err)
	require.Contains(t, string(f6v3), `"household_units":2`)
	require.NotContains(t, string(f6v3), `"spatial"`)

	f7, err := marshalCanonicalCityState(cityHashState{
		SimulationVersion: CitySimulationVersionF7,
		Physical:          cityPhysicalHashState{HouseholdCohorts: []cityHashHouseholdCohort{{HouseholdUnits: 2}}},
		Spatial: &citySpatialHashState{
			Profile: cityHashSpatialProfile{Metadata: json.RawMessage(`{}`)},
			Chunks:  make([]cityHashSpatialChunk, 0),
		},
	})
	require.NoError(t, err)
	require.Contains(t, string(f7), `"spatial"`)
	require.Contains(t, string(f7), `"household_units":2`)
	require.NotContains(t, string(f7), `"land"`)

	f7v2, err := marshalCanonicalCityState(cityHashState{
		SimulationVersion: CitySimulationVersionF7V2,
		Spatial: &citySpatialHashState{
			Profile: cityHashSpatialProfile{Metadata: json.RawMessage(`{}`)},
			Chunks:  make([]cityHashSpatialChunk, 0),
		},
		Land: &cityLandHashState{
			ZoningRules:        make([]cityspatial.LandZoningRule, 0),
			Parcels:            make([]cityspatial.GeneratedParcel, 0),
			Buildings:          make([]cityspatial.GeneratedBuilding, 0),
			UnitPools:          make([]cityspatial.GeneratedBuildingUnitPool, 0),
			HousingAllocations: make([]cityspatial.GeneratedHousingAllocation, 0),
			Portals:            make([]cityspatial.GeneratedBuildingPortal, 0),
		},
	})
	require.NoError(t, err)
	require.Contains(t, string(f7v2), `"land"`)
	require.NotContains(t, string(f7v2), `"development"`)

	f7v3, err := marshalCanonicalCityState(cityHashState{
		SimulationVersion: CitySimulationVersionF7V3,
		Spatial: &citySpatialHashState{
			Profile: cityHashSpatialProfile{Metadata: json.RawMessage(`{}`)},
			Chunks:  make([]cityHashSpatialChunk, 0),
		},
		Land: &cityLandHashState{
			ZoningRules:        make([]cityspatial.LandZoningRule, 0),
			Parcels:            make([]cityspatial.GeneratedParcel, 0),
			Buildings:          make([]cityspatial.GeneratedBuilding, 0),
			UnitPools:          make([]cityspatial.GeneratedBuildingUnitPool, 0),
			HousingAllocations: make([]cityspatial.GeneratedHousingAllocation, 0),
			Portals:            make([]cityspatial.GeneratedBuildingPortal, 0),
		},
		Development: &cityDevelopmentHashState{
			Projects:    make([]CityDevelopmentProject, 0),
			Facts:       make([]CityDevelopmentFact, 0),
			Adjustments: make([]CityBuildingAdjustment, 0),
		},
	})
	require.NoError(t, err)
	require.Contains(t, string(f7v3), `"development"`)
	require.NotContains(t, string(f7v3), `"enterprise_location"`)

	f7v4, err := marshalCanonicalCityState(cityHashState{
		SimulationVersion: CitySimulationVersionF7V4,
		Spatial:           &citySpatialHashState{},
		Land:              &cityLandHashState{},
		Development:       &cityDevelopmentHashState{},
		EnterpriseLocation: &cityEnterpriseLocationHashState{
			BaselineSites: make([]CityEnterpriseSite, 0),
			Sites:         make([]CityEnterpriseSite, 0),
			Facts:         make([]CityEnterpriseLocationFact, 0),
		},
	})
	require.NoError(t, err)
	require.Contains(t, string(f7v4), `"enterprise_location"`)
	require.NotContains(t, string(f7v4), `"world_runtime"`)

	f7v5, err := marshalCanonicalCityState(cityHashState{
		SimulationVersion:  CitySimulationVersionF7V5,
		Spatial:            &citySpatialHashState{},
		Land:               &cityLandHashState{},
		Development:        &cityDevelopmentHashState{},
		EnterpriseLocation: &cityEnterpriseLocationHashState{},
		WorldRuntime: &worldRuntimeHashState{
			Definitions: make([]WorldRuntimeDefinition, 0), Actors: make([]WorldActor, 0),
			Attributes: make([]WorldActorAttribute, 0), Roles: make([]WorldActorRole, 0),
			Statuses: make([]WorldActorStatus, 0), Facts: make([]WorldRuntimeFact, 0),
			Effects: make([]WorldEffectOperation, 0), RuleCases: make([]WorldRuleCase, 0),
		},
	})
	require.NoError(t, err)
	require.Contains(t, string(f7v5), `"world_runtime"`)
	require.NotContains(t, string(f7v5), `"locations"`)
	require.NotContains(t, string(f7v5), `"control_grants"`)

	locations := make([]WorldActorLocation, 0)
	controlGrants := make([]WorldActorControlGrant, 0)
	f7v6, err := marshalCanonicalCityState(cityHashState{
		SimulationVersion:  CitySimulationVersionF7V6,
		Spatial:            &citySpatialHashState{},
		Land:               &cityLandHashState{},
		Development:        &cityDevelopmentHashState{},
		EnterpriseLocation: &cityEnterpriseLocationHashState{},
		WorldRuntime: &worldRuntimeHashState{
			Definitions: make([]WorldRuntimeDefinition, 0), Actors: make([]WorldActor, 0),
			Attributes: make([]WorldActorAttribute, 0), Roles: make([]WorldActorRole, 0),
			Statuses: make([]WorldActorStatus, 0), Facts: make([]WorldRuntimeFact, 0),
			Effects: make([]WorldEffectOperation, 0), RuleCases: make([]WorldRuleCase, 0),
			Locations: &locations, ControlGrants: &controlGrants,
		},
	})
	require.NoError(t, err)
	require.Contains(t, string(f7v6), `"locations":[]`)
	require.Contains(t, string(f7v6), `"control_grants":[]`)

	f7v7, err := marshalCanonicalCityState(cityHashState{
		SimulationVersion:  CitySimulationVersionF7V7,
		Spatial:            &citySpatialHashState{},
		Land:               &cityLandHashState{},
		Development:        &cityDevelopmentHashState{},
		EnterpriseLocation: &cityEnterpriseLocationHashState{},
		WorldRuntime: &worldRuntimeHashState{
			Definitions: make([]WorldRuntimeDefinition, 0), Actors: make([]WorldActor, 0),
			Attributes: make([]WorldActorAttribute, 0), Roles: make([]WorldActorRole, 0),
			Statuses: make([]WorldActorStatus, 0), Facts: make([]WorldRuntimeFact, 0),
			Effects: make([]WorldEffectOperation, 0), RuleCases: make([]WorldRuleCase, 0),
			Locations: &locations, ControlGrants: &controlGrants,
		},
	})
	require.NoError(t, err)
	require.Contains(t, string(f7v7), `"locations":[]`)
	require.Contains(t, string(f7v7), `"control_grants":[]`)
	require.NotContains(t, string(f7v7), `"portal_states"`)

	portalStates := make([]WorldPortalState, 0)
	f7v8, err := marshalCanonicalCityState(cityHashState{
		SimulationVersion:  CitySimulationVersionF7V8,
		Spatial:            &citySpatialHashState{},
		Land:               &cityLandHashState{},
		Development:        &cityDevelopmentHashState{},
		EnterpriseLocation: &cityEnterpriseLocationHashState{},
		WorldRuntime: &worldRuntimeHashState{
			Definitions: make([]WorldRuntimeDefinition, 0), Actors: make([]WorldActor, 0),
			Attributes: make([]WorldActorAttribute, 0), Roles: make([]WorldActorRole, 0),
			Statuses: make([]WorldActorStatus, 0), Facts: make([]WorldRuntimeFact, 0),
			Effects: make([]WorldEffectOperation, 0), RuleCases: make([]WorldRuleCase, 0),
			Locations: &locations, ControlGrants: &controlGrants, PortalStates: &portalStates,
		},
	})
	require.NoError(t, err)
	require.Contains(t, string(f7v8), `"portal_states":[]`)
	require.NotContains(t, string(f7v8), `"navigation_profile"`)

	navigationIntents := make([]WorldActorNavigationIntent, 0)
	f7v9, err := marshalCanonicalCityState(cityHashState{
		SimulationVersion:  CitySimulationVersionF7V9,
		Spatial:            &citySpatialHashState{},
		Land:               &cityLandHashState{},
		Development:        &cityDevelopmentHashState{},
		EnterpriseLocation: &cityEnterpriseLocationHashState{},
		WorldRuntime: &worldRuntimeHashState{
			Definitions: make([]WorldRuntimeDefinition, 0), Actors: make([]WorldActor, 0),
			Attributes: make([]WorldActorAttribute, 0), Roles: make([]WorldActorRole, 0),
			Statuses: make([]WorldActorStatus, 0), Facts: make([]WorldRuntimeFact, 0),
			Effects: make([]WorldEffectOperation, 0), RuleCases: make([]WorldRuleCase, 0),
			Locations: &locations, ControlGrants: &controlGrants, PortalStates: &portalStates,
			NavigationProfile: &WorldNavigationProfile{
				ProfileVersion: worldNavigationProfileVersion, Revision: 1,
			},
			NavigationIntents: &navigationIntents,
		},
	})
	require.NoError(t, err)
	require.Contains(t, string(f7v9), `"navigation_profile"`)
	require.Contains(t, string(f7v9), `"navigation_intents":[]`)
	require.NotContains(t, string(f7v9), `"public_services"`)

	f8, err := marshalCanonicalCityState(cityHashState{
		SimulationVersion:  CitySimulationVersionF8,
		Spatial:            &citySpatialHashState{},
		Land:               &cityLandHashState{},
		Development:        &cityDevelopmentHashState{},
		EnterpriseLocation: &cityEnterpriseLocationHashState{},
		WorldRuntime: &worldRuntimeHashState{
			Definitions: make([]WorldRuntimeDefinition, 0), Actors: make([]WorldActor, 0),
			Attributes: make([]WorldActorAttribute, 0), Roles: make([]WorldActorRole, 0),
			Statuses: make([]WorldActorStatus, 0), Facts: make([]WorldRuntimeFact, 0),
			Effects: make([]WorldEffectOperation, 0), RuleCases: make([]WorldRuleCase, 0),
			Locations: &locations, ControlGrants: &controlGrants, PortalStates: &portalStates,
			NavigationProfile: &WorldNavigationProfile{
				ProfileVersion: worldNavigationProfileVersion, Revision: 1,
			},
			NavigationIntents: &navigationIntents,
		},
		PublicServices: &cityPublicServiceHashState{
			ServiceDefinitions: make([]CityServiceDefinition, 0),
			FacilityTypes:      make([]CityFacilityTypeDefinition, 0),
			Facilities:         make([]CityFacility, 0), Capacities: make([]CityFacilityServiceCapacity, 0),
			Demands: make([]CityServiceDemand, 0), Connections: make([]CityServiceConnection, 0),
			Facts: make([]CityServiceFact, 0), Allocations: make([]CityServiceAllocation, 0),
			Settlements: make([]CityServiceSettlement, 0),
		},
	})
	require.NoError(t, err)
	require.Contains(t, string(f8), `"public_services"`)

	_, err = marshalCanonicalCityState(cityHashState{SimulationVersion: CitySimulationVersionF7})
	require.Error(t, err)
	_, err = marshalCanonicalCityState(cityHashState{
		SimulationVersion: CitySimulationVersionF7V2,
		Spatial:           &citySpatialHashState{},
	})
	require.Error(t, err)
	_, err = marshalCanonicalCityState(cityHashState{
		SimulationVersion: CitySimulationVersionF7V3,
		Spatial:           &citySpatialHashState{},
		Land:              &cityLandHashState{},
	})
	require.Error(t, err)
	_, err = marshalCanonicalCityState(cityHashState{
		SimulationVersion: CitySimulationVersionF7V4,
		Spatial:           &citySpatialHashState{},
		Land:              &cityLandHashState{},
		Development:       &cityDevelopmentHashState{},
	})
	require.Error(t, err)
	_, err = marshalCanonicalCityState(cityHashState{
		SimulationVersion:  CitySimulationVersionF7V5,
		Spatial:            &citySpatialHashState{},
		Land:               &cityLandHashState{},
		Development:        &cityDevelopmentHashState{},
		EnterpriseLocation: &cityEnterpriseLocationHashState{},
	})
	require.Error(t, err)
	_, err = marshalCanonicalCityState(cityHashState{
		SimulationVersion:  CitySimulationVersionF7V6,
		Spatial:            &citySpatialHashState{},
		Land:               &cityLandHashState{},
		Development:        &cityDevelopmentHashState{},
		EnterpriseLocation: &cityEnterpriseLocationHashState{},
		WorldRuntime:       &worldRuntimeHashState{},
	})
	require.Error(t, err)
	_, err = marshalCanonicalCityState(cityHashState{
		SimulationVersion:  CitySimulationVersionF7V7,
		Spatial:            &citySpatialHashState{},
		Land:               &cityLandHashState{},
		Development:        &cityDevelopmentHashState{},
		EnterpriseLocation: &cityEnterpriseLocationHashState{},
		WorldRuntime:       &worldRuntimeHashState{},
	})
	require.Error(t, err)
	_, err = marshalCanonicalCityState(cityHashState{
		SimulationVersion:  CitySimulationVersionF7V8,
		Spatial:            &citySpatialHashState{},
		Land:               &cityLandHashState{},
		Development:        &cityDevelopmentHashState{},
		EnterpriseLocation: &cityEnterpriseLocationHashState{},
		WorldRuntime: &worldRuntimeHashState{
			Locations: &locations, ControlGrants: &controlGrants,
		},
	})
	require.Error(t, err)
	_, err = marshalCanonicalCityState(cityHashState{
		SimulationVersion:  CitySimulationVersionF7V9,
		Spatial:            &citySpatialHashState{},
		Land:               &cityLandHashState{},
		Development:        &cityDevelopmentHashState{},
		EnterpriseLocation: &cityEnterpriseLocationHashState{},
		WorldRuntime: &worldRuntimeHashState{
			Locations: &locations, ControlGrants: &controlGrants, PortalStates: &portalStates,
		},
	})
	require.Error(t, err)
	_, err = marshalCanonicalCityState(cityHashState{
		SimulationVersion:  CitySimulationVersionF8,
		Spatial:            &citySpatialHashState{},
		Land:               &cityLandHashState{},
		Development:        &cityDevelopmentHashState{},
		EnterpriseLocation: &cityEnterpriseLocationHashState{},
		WorldRuntime: &worldRuntimeHashState{
			Locations: &locations, ControlGrants: &controlGrants, PortalStates: &portalStates,
			NavigationProfile: &WorldNavigationProfile{}, NavigationIntents: &navigationIntents,
		},
	})
	require.Error(t, err)
}
