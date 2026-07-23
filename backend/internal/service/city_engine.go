package service

import (
	"encoding/json"
	"fmt"
)

const (
	CitySimulationVersionF5   = "city-f5-v1"
	CitySimulationVersionF6   = "city-f6-v1"
	CitySimulationVersionF6V2 = "city-f6-v2"
	CitySimulationVersionF6V3 = "city-f6-v3"
	CitySimulationVersionF7   = "city-f7-v1"
	CitySimulationVersionF7V2 = "city-f7-v2"
	CitySimulationVersionF7V3 = "city-f7-v3"
	CitySimulationVersionF7V4 = "city-f7-v4"
	CitySimulationVersionF7V5 = "city-f7-v5"
	CitySimulationVersionF7V6 = "city-f7-v6"
	CitySimulationVersionF7V7 = "city-f7-v7"
	CitySimulationVersionF7V8 = "city-f7-v8"
	CitySimulationVersionF7V9 = "city-f7-v9"
	CitySimulationVersionF8   = "city-f8-v1"
	// CitySimulationVersionOpenWorld is a separate genesis pipeline. It shares
	// the generic ledger/demography base, but never enters the frozen F7 map.
	CitySimulationVersionOpenWorld = "city-openworld-v1"
	// CitySimulationVersionOpenWorldV2 replaces the fixed bootstrap-sector
	// contract with immutable, on-demand region plans.  It intentionally has
	// no upgrade path from v1: the two generators persist different facts.
	CitySimulationVersionOpenWorldV2 = "city-openworld-v2"
	// CitySimulationVersionOpenWorldV3 adds sealed vertical floor facts and
	// immutable portal topology.  V2 remains readable/materializable with its
	// original ground-only generator contract.
	CitySimulationVersionOpenWorldV3 = "city-openworld-v3"
	// CitySimulationVersionOpenWorldV4 keeps the V3 static generator contract
	// and adds an independent actor/runtime domain.  It never reads F7's fixed
	// overmap, district, or spatial-profile tables.
	CitySimulationVersionOpenWorldV4 = "city-openworld-v4"
	// CitySimulationVersionOpenWorldV5 freezes the first social-world contract
	// on top of V4: scenario binding, facilities, deterministic NPC LOD and
	// open-world navigation are all V5-owned facts.  V4 snapshots therefore
	// remain valid rather than receiving a silent genesis rewrite.
	CitySimulationVersionOpenWorldV5 = "city-openworld-v5"
	// CitySimulationVersionOpenWorldV6 makes the complete world-version vector
	// (engine, scenario, policy, spatial, worldgen, content and rules) a
	// canonical fact. It is the only default for newly created worlds; V5 stays
	// readable and can be upgraded explicitly with an audited baseline.
	CitySimulationVersionOpenWorldV6 = "city-openworld-v6"
	// CitySimulationVersionOpenWorldV7 adds the first open-world-native social
	// service contract: versioned service catalogues, spatial reachability,
	// capacity queues and fact-backed responses.  The legacy F8 service tables
	// remain a historical compatibility domain and are never used by V7 worlds.
	CitySimulationVersionOpenWorldV7 = "city-openworld-v7"
	// CitySimulationVersionOpenWorldV8 keeps the V7 service state machine and
	// adds a separately-versioned impact bridge. Service responses first create
	// a pending impact record; a later tick alone may apply it to a target
	// domain metric. This keeps cross-domain causality explicit and prevents a
	// response from mutating another projection in its own tick.
	CitySimulationVersionOpenWorldV8 = "city-openworld-v8"
	// CitySimulationVersionOpenWorldV9 adds the first durable aggregate
	// mobility contract. It freezes a world-specific hub/edge topology and
	// resolves actor trip demands through fact-backed, capacity-bounded routes.
	// It deliberately does not replace V5's cell-level navigation reducer.
	CitySimulationVersionOpenWorldV9 = "city-openworld-v9"
	// CitySimulationVersionOpenWorldV10 consumes a completed V9 route through
	// an explicit, fact-backed macro-to-local arrival bridge.  It validates a
	// materialized V5 open-world landing cell before changing an actor's local
	// coordinate; V9 route history itself remains immutable and coordinate-free.
	CitySimulationVersionOpenWorldV10 = "city-openworld-v10"
	// CitySimulationVersionOpenWorldV11 adds versioned automatic OD sources
	// and immutable closed-cycle transport metrics. It keeps V9 scheduling and
	// V10 landing as independent lower-level contracts rather than replacing
	// either with an NPC-only traffic shortcut.
	CitySimulationVersionOpenWorldV11 = "city-openworld-v11"
	// CitySimulationVersionOpenWorldV12 seals capacity-limited residence and
	// employment bindings for eligible NPCs. Future commute sources consume it
	// through explicit facts rather than mutating the V5 social projection.
	CitySimulationVersionOpenWorldV12 = "city-openworld-v12"
	// CitySimulationVersionOpenWorldV13 turns each V12 binding into two
	// independently-audited commute sources. A source must verify the actor at
	// its facility presence domain before it creates a V9 demand, so historical
	// V11 visits and V10 arrivals remain immutable lower-level evidence.
	CitySimulationVersionOpenWorldV13 = "city-openworld-v13"
	// CitySimulationVersionOpenWorldV14 layers an append-only assignment
	// lifecycle over V12/V13's sealed evidence. A reassignment is a successor
	// epoch rather than a mutation of historical commute bindings or sources.
	CitySimulationVersionOpenWorldV14 = "city-openworld-v14"
	// CitySimulationVersionOpenWorldV15 adds F10.0's fact-backed enterprise
	// orders, inventory reservations, settlement and atomic delivery contract.
	// It reuses F2/F3 truth rather than introducing a parallel economy.
	CitySimulationVersionOpenWorldV15 = "city-openworld-v15"
	// CitySimulationVersionOpenWorldV16 adds the narrow F9.2.C enterprise
	// freight source adapter. It consumes sealed V15 dispatch evidence into
	// V9 cargo demand without granting a route completion delivery semantics.
	CitySimulationVersionOpenWorldV16 = "city-openworld-v16"
	// CitySimulationVersionOpenWorldV17 adds an explicit custody/receipt layer
	// over V16 freight evidence. V15 remains the only inventory authority;
	// network completion only makes a shipment eligible for receipt.
	CitySimulationVersionOpenWorldV17 = "city-openworld-v17"
	// CitySimulationVersionOpenWorldV18 batches only V16 overflow sources into
	// capacity-bounded V9 consignments. It preserves V15's single atomic
	// delivery and adds proof that every batch arrived before that delivery.
	CitySimulationVersionOpenWorldV18 = "city-openworld-v18"
	// CitySimulationVersionOpenWorldV19 freezes a profile-selected spatial
	// identity for every V9 hub and edge. It does not replace V9 scheduling;
	// later F9.3 revisions can extend these stable nodes/corridors with mutable
	// segments, stations, maintenance and construction facts.
	CitySimulationVersionOpenWorldV19 = "city-openworld-v19"
	// CitySimulationVersionOpenWorldV20 adds generic, fact-backed mutable
	// infrastructure assets over V19's immutable node/corridor identities. It
	// intentionally records state only; V9 scheduling remains unchanged until
	// a later, explicit capacity-consumption engine revision.
	CitySimulationVersionOpenWorldV20 = "city-openworld-v20"
	// CitySimulationVersionOpenWorldV21 is the explicit F9.3.2 bridge that
	// lets V9 admit only future routes whose V20 corridor-segment capacity is
	// available. Accepted V9 routes remain immutable historical evidence.
	CitySimulationVersionOpenWorldV21 = "city-openworld-v21"
	// CitySimulationVersionOpenWorldV22 adds F10.3's partial freight-settlement
	// successor.  It consumes post-baseline V17/V18 custody evidence and records
	// per-line outcomes without rewriting historic V15 atomic delivery facts.
	CitySimulationVersionOpenWorldV22 = "city-openworld-v22"
	// CitySimulationVersionOpenWorldV23 adds F10.3.1a's manual carrier reserve
	// and one-to-one carrier-claim recovery evidence. It leaves V22 settlement
	// rows immutable and deliberately does not introduce insurance or pricing.
	CitySimulationVersionOpenWorldV23 = "city-openworld-v23"
	// CitySimulationVersionOpenWorldV24 adds F10.3.1b's immutable carrier
	// service contracts and cash-only freight-fee settlement. It intentionally
	// leaves route pricing, insurance, credit and liability semantics for later
	// explicitly versioned generations.
	CitySimulationVersionOpenWorldV24 = "city-openworld-v24"
	// CitySimulationVersionRealtimeV1 is intentionally not a V24 feature flag.
	// It owns Temporal Frames and elapsed microsecond time, so it never enters
	// the legacy hourly tick reducer or the V24 static/open-world foundation.
	CitySimulationVersionRealtimeV1 = "city-openworld-realtime-v1"
	// CitySimulationVersionRealtimeV2 is the first realtime engine that owns a
	// sealed, deterministic static worldgen projection. It remains separate
	// from the legacy open-world lineage: V24's hourly reducer, mutable sector
	// materialization, and actor runtime must never be reached through this
	// version. Its own temporal-frame actor runtime is intentionally isolated
	// in the city_realtime_actor_* namespace.
	CitySimulationVersionRealtimeV2 = "city-openworld-realtime-v2"

	CurrentCitySimulationVersion = CitySimulationVersionOpenWorldV24

	// CitySimulationVersionV1 remains the public compatibility name used by
	// existing callers. New engine code should use the explicit F5/F6 names.
	CitySimulationVersionV1 = CurrentCitySimulationVersion
)

type cityEngineStage string

const (
	cityEngineStageControl                            cityEngineStage = "control"
	cityEngineStageLedger                             cityEngineStage = "ledger"
	cityEngineStageResources                          cityEngineStage = "resources"
	cityEngineStageCalendarDemography                 cityEngineStage = "calendar_demography"
	cityEngineStageOpenWorld                          cityEngineStage = "open_world"
	cityEngineStageOpenWorldServices                  cityEngineStage = "open_world_services"
	cityEngineStageOpenWorldImpacts                   cityEngineStage = "open_world_impacts"
	cityEngineStageOpenWorldMobilityOD                cityEngineStage = "open_world_mobility_od"
	cityEngineStageOpenWorldCommutes                  cityEngineStage = "open_world_commutes"
	cityEngineStageOpenWorldCommuteLifecycle          cityEngineStage = "open_world_commute_lifecycle"
	cityEngineStageOpenWorldSupplyChain               cityEngineStage = "open_world_supply_chain"
	cityEngineStageOpenWorldEnterpriseFreight         cityEngineStage = "open_world_enterprise_freight"
	cityEngineStageOpenWorldEnterpriseFreightReceipts cityEngineStage = "open_world_enterprise_freight_receipts"
	cityEngineStageOpenWorldEnterpriseFreightBatches  cityEngineStage = "open_world_enterprise_freight_batches"
	cityEngineStageOpenWorldFreightSettlements        cityEngineStage = "open_world_freight_settlements"
	cityEngineStageOpenWorldCarrierRecovery           cityEngineStage = "open_world_carrier_recovery"
	cityEngineStageOpenWorldCarrierCommerce           cityEngineStage = "open_world_carrier_commerce"
	cityEngineStageOpenWorldInfrastructure            cityEngineStage = "open_world_infrastructure"
	cityEngineStageOpenWorldMobility                  cityEngineStage = "open_world_mobility"
	cityEngineStageOpenWorldEffectiveCapacity         cityEngineStage = "open_world_effective_capacity"
	cityEngineStageOpenWorldArrivals                  cityEngineStage = "open_world_arrivals"
	cityEngineStageSpatial                            cityEngineStage = "spatial"
	cityEngineStageDevelopment                        cityEngineStage = "development"
	cityEngineStageEnterpriseLocation                 cityEngineStage = "enterprise_location"
	cityEngineStageWorldRuntime                       cityEngineStage = "world_runtime"
	cityEngineStagePublicServices                     cityEngineStage = "public_services"
	cityEngineStageMarkets                            cityEngineStage = "markets"
)

type cityEngineDefinition struct {
	version string
	stages  []cityEngineStage
}

func cityEngineForVersion(version string) (cityEngineDefinition, error) {
	var engine cityEngineDefinition
	switch version {
	case CitySimulationVersionF5:
		engine = cityEngineDefinition{version: version, stages: []cityEngineStage{
			cityEngineStageControl,
			cityEngineStageLedger,
			cityEngineStageResources,
			cityEngineStageMarkets,
		}}
	case CitySimulationVersionF6, CitySimulationVersionF6V2, CitySimulationVersionF6V3:
		engine = cityEngineDefinition{version: version, stages: []cityEngineStage{
			cityEngineStageControl,
			cityEngineStageLedger,
			cityEngineStageResources,
			cityEngineStageCalendarDemography,
			cityEngineStageMarkets,
		}}
	case CitySimulationVersionRealtimeV1, CitySimulationVersionRealtimeV2:
		// The realtime kernel reuses only the generic economic foundations. Its
		// lifecycle, commands, due events, and scheduler live behind the
		// separate temporal-frame pipeline rather than legacy tick stages.
		engine = cityEngineDefinition{version: version, stages: []cityEngineStage{
			cityEngineStageControl,
			cityEngineStageLedger,
			cityEngineStageResources,
			cityEngineStageCalendarDemography,
			cityEngineStageMarkets,
		}}
	case CitySimulationVersionOpenWorld:
		engine = cityEngineDefinition{version: version, stages: []cityEngineStage{
			cityEngineStageControl,
			cityEngineStageLedger,
			cityEngineStageResources,
			cityEngineStageCalendarDemography,
			cityEngineStageMarkets,
		}}
	case CitySimulationVersionOpenWorldV2, CitySimulationVersionOpenWorldV3,
		CitySimulationVersionOpenWorldV4, CitySimulationVersionOpenWorldV5,
		CitySimulationVersionOpenWorldV6:
		engine = cityEngineDefinition{version: version, stages: []cityEngineStage{
			cityEngineStageControl,
			cityEngineStageLedger,
			cityEngineStageResources,
			cityEngineStageCalendarDemography,
			cityEngineStageOpenWorld,
			cityEngineStageMarkets,
		}}
	case CitySimulationVersionOpenWorldV7:
		engine = cityEngineDefinition{version: version, stages: []cityEngineStage{
			cityEngineStageControl,
			cityEngineStageLedger,
			cityEngineStageResources,
			cityEngineStageCalendarDemography,
			cityEngineStageOpenWorld,
			cityEngineStageOpenWorldServices,
			cityEngineStageMarkets,
		}}
	case CitySimulationVersionOpenWorldV8:
		engine = cityEngineDefinition{version: version, stages: []cityEngineStage{
			cityEngineStageControl,
			cityEngineStageLedger,
			cityEngineStageResources,
			cityEngineStageCalendarDemography,
			cityEngineStageOpenWorld,
			cityEngineStageOpenWorldServices,
			cityEngineStageOpenWorldImpacts,
			cityEngineStageMarkets,
		}}
	case CitySimulationVersionOpenWorldV9:
		engine = cityEngineDefinition{version: version, stages: []cityEngineStage{
			cityEngineStageControl,
			cityEngineStageLedger,
			cityEngineStageResources,
			cityEngineStageCalendarDemography,
			cityEngineStageOpenWorld,
			cityEngineStageOpenWorldServices,
			cityEngineStageOpenWorldImpacts,
			cityEngineStageOpenWorldMobility,
			cityEngineStageMarkets,
		}}
	case CitySimulationVersionOpenWorldV10:
		engine = cityEngineDefinition{version: version, stages: []cityEngineStage{
			cityEngineStageControl,
			cityEngineStageLedger,
			cityEngineStageResources,
			cityEngineStageCalendarDemography,
			cityEngineStageOpenWorld,
			cityEngineStageOpenWorldServices,
			cityEngineStageOpenWorldImpacts,
			cityEngineStageOpenWorldMobility,
			cityEngineStageOpenWorldArrivals,
			cityEngineStageMarkets,
		}}
	case CitySimulationVersionOpenWorldV11, CitySimulationVersionOpenWorldV12:
		engine = cityEngineDefinition{version: version, stages: []cityEngineStage{
			cityEngineStageControl,
			cityEngineStageLedger,
			cityEngineStageResources,
			cityEngineStageCalendarDemography,
			cityEngineStageOpenWorld,
			cityEngineStageOpenWorldServices,
			cityEngineStageOpenWorldImpacts,
			cityEngineStageOpenWorldMobilityOD,
			cityEngineStageOpenWorldMobility,
			cityEngineStageOpenWorldArrivals,
			cityEngineStageMarkets,
		}}
	case CitySimulationVersionOpenWorldV13:
		engine = cityEngineDefinition{version: version, stages: []cityEngineStage{
			cityEngineStageControl,
			cityEngineStageLedger,
			cityEngineStageResources,
			cityEngineStageCalendarDemography,
			cityEngineStageOpenWorld,
			cityEngineStageOpenWorldServices,
			cityEngineStageOpenWorldImpacts,
			cityEngineStageOpenWorldMobilityOD,
			cityEngineStageOpenWorldCommutes,
			cityEngineStageOpenWorldMobility,
			cityEngineStageOpenWorldArrivals,
			cityEngineStageMarkets,
		}}
	case CitySimulationVersionOpenWorldV14:
		engine = cityEngineDefinition{version: version, stages: []cityEngineStage{
			cityEngineStageControl,
			cityEngineStageLedger,
			cityEngineStageResources,
			cityEngineStageCalendarDemography,
			cityEngineStageOpenWorld,
			cityEngineStageOpenWorldServices,
			cityEngineStageOpenWorldImpacts,
			cityEngineStageOpenWorldMobilityOD,
			cityEngineStageOpenWorldCommuteLifecycle,
			cityEngineStageOpenWorldMobility,
			cityEngineStageOpenWorldArrivals,
			cityEngineStageMarkets,
		}}
	case CitySimulationVersionOpenWorldV15:
		engine = cityEngineDefinition{version: version, stages: []cityEngineStage{
			cityEngineStageControl,
			cityEngineStageLedger,
			cityEngineStageResources,
			cityEngineStageCalendarDemography,
			cityEngineStageOpenWorld,
			cityEngineStageOpenWorldServices,
			cityEngineStageOpenWorldImpacts,
			cityEngineStageOpenWorldMobilityOD,
			cityEngineStageOpenWorldCommuteLifecycle,
			cityEngineStageOpenWorldSupplyChain,
			cityEngineStageOpenWorldMobility,
			cityEngineStageOpenWorldArrivals,
			cityEngineStageMarkets,
		}}
	case CitySimulationVersionOpenWorldV16:
		engine = cityEngineDefinition{version: version, stages: []cityEngineStage{
			cityEngineStageControl,
			cityEngineStageLedger,
			cityEngineStageResources,
			cityEngineStageCalendarDemography,
			cityEngineStageOpenWorld,
			cityEngineStageOpenWorldServices,
			cityEngineStageOpenWorldImpacts,
			cityEngineStageOpenWorldMobilityOD,
			cityEngineStageOpenWorldCommuteLifecycle,
			cityEngineStageOpenWorldSupplyChain,
			cityEngineStageOpenWorldEnterpriseFreight,
			cityEngineStageOpenWorldMobility,
			cityEngineStageOpenWorldArrivals,
			cityEngineStageMarkets,
		}}
	case CitySimulationVersionOpenWorldV17:
		engine = cityEngineDefinition{version: version, stages: []cityEngineStage{
			cityEngineStageControl,
			cityEngineStageLedger,
			cityEngineStageResources,
			cityEngineStageCalendarDemography,
			cityEngineStageOpenWorld,
			cityEngineStageOpenWorldServices,
			cityEngineStageOpenWorldImpacts,
			cityEngineStageOpenWorldMobilityOD,
			cityEngineStageOpenWorldCommuteLifecycle,
			cityEngineStageOpenWorldSupplyChain,
			cityEngineStageOpenWorldEnterpriseFreight,
			cityEngineStageOpenWorldEnterpriseFreightReceipts,
			cityEngineStageOpenWorldMobility,
			cityEngineStageOpenWorldArrivals,
			cityEngineStageMarkets,
		}}
	case CitySimulationVersionOpenWorldV18, CitySimulationVersionOpenWorldV19:
		engine = cityEngineDefinition{version: version, stages: []cityEngineStage{
			cityEngineStageControl,
			cityEngineStageLedger,
			cityEngineStageResources,
			cityEngineStageCalendarDemography,
			cityEngineStageOpenWorld,
			cityEngineStageOpenWorldServices,
			cityEngineStageOpenWorldImpacts,
			cityEngineStageOpenWorldMobilityOD,
			cityEngineStageOpenWorldCommuteLifecycle,
			cityEngineStageOpenWorldSupplyChain,
			cityEngineStageOpenWorldEnterpriseFreight,
			cityEngineStageOpenWorldEnterpriseFreightReceipts,
			cityEngineStageOpenWorldEnterpriseFreightBatches,
			cityEngineStageOpenWorldMobility,
			cityEngineStageOpenWorldArrivals,
			cityEngineStageMarkets,
		}}
	case CitySimulationVersionOpenWorldV20:
		engine = cityEngineDefinition{version: version, stages: []cityEngineStage{
			cityEngineStageControl,
			cityEngineStageLedger,
			cityEngineStageResources,
			cityEngineStageCalendarDemography,
			cityEngineStageOpenWorld,
			cityEngineStageOpenWorldServices,
			cityEngineStageOpenWorldImpacts,
			cityEngineStageOpenWorldMobilityOD,
			cityEngineStageOpenWorldCommuteLifecycle,
			cityEngineStageOpenWorldSupplyChain,
			cityEngineStageOpenWorldEnterpriseFreight,
			cityEngineStageOpenWorldEnterpriseFreightReceipts,
			cityEngineStageOpenWorldEnterpriseFreightBatches,
			cityEngineStageOpenWorldInfrastructure,
			cityEngineStageOpenWorldMobility,
			cityEngineStageOpenWorldArrivals,
			cityEngineStageMarkets,
		}}
	case CitySimulationVersionOpenWorldV21:
		engine = cityEngineDefinition{version: version, stages: []cityEngineStage{
			cityEngineStageControl,
			cityEngineStageLedger,
			cityEngineStageResources,
			cityEngineStageCalendarDemography,
			cityEngineStageOpenWorld,
			cityEngineStageOpenWorldServices,
			cityEngineStageOpenWorldImpacts,
			cityEngineStageOpenWorldMobilityOD,
			cityEngineStageOpenWorldCommuteLifecycle,
			cityEngineStageOpenWorldSupplyChain,
			cityEngineStageOpenWorldEnterpriseFreight,
			cityEngineStageOpenWorldEnterpriseFreightReceipts,
			cityEngineStageOpenWorldEnterpriseFreightBatches,
			cityEngineStageOpenWorldInfrastructure,
			cityEngineStageOpenWorldMobility,
			cityEngineStageOpenWorldEffectiveCapacity,
			cityEngineStageOpenWorldArrivals,
			cityEngineStageMarkets,
		}}
	case CitySimulationVersionOpenWorldV22:
		engine = cityEngineDefinition{version: version, stages: []cityEngineStage{
			cityEngineStageControl,
			cityEngineStageLedger,
			cityEngineStageResources,
			cityEngineStageCalendarDemography,
			cityEngineStageOpenWorld,
			cityEngineStageOpenWorldServices,
			cityEngineStageOpenWorldImpacts,
			cityEngineStageOpenWorldMobilityOD,
			cityEngineStageOpenWorldCommuteLifecycle,
			cityEngineStageOpenWorldSupplyChain,
			cityEngineStageOpenWorldEnterpriseFreight,
			cityEngineStageOpenWorldEnterpriseFreightReceipts,
			cityEngineStageOpenWorldEnterpriseFreightBatches,
			cityEngineStageOpenWorldFreightSettlements,
			cityEngineStageOpenWorldInfrastructure,
			cityEngineStageOpenWorldMobility,
			cityEngineStageOpenWorldEffectiveCapacity,
			cityEngineStageOpenWorldArrivals,
			cityEngineStageMarkets,
		}}
	case CitySimulationVersionOpenWorldV23:
		engine = cityEngineDefinition{version: version, stages: []cityEngineStage{
			cityEngineStageControl,
			cityEngineStageLedger,
			cityEngineStageResources,
			cityEngineStageCalendarDemography,
			cityEngineStageOpenWorld,
			cityEngineStageOpenWorldServices,
			cityEngineStageOpenWorldImpacts,
			cityEngineStageOpenWorldMobilityOD,
			cityEngineStageOpenWorldCommuteLifecycle,
			cityEngineStageOpenWorldSupplyChain,
			cityEngineStageOpenWorldEnterpriseFreight,
			cityEngineStageOpenWorldEnterpriseFreightReceipts,
			cityEngineStageOpenWorldEnterpriseFreightBatches,
			cityEngineStageOpenWorldFreightSettlements,
			cityEngineStageOpenWorldCarrierRecovery,
			cityEngineStageOpenWorldInfrastructure,
			cityEngineStageOpenWorldMobility,
			cityEngineStageOpenWorldEffectiveCapacity,
			cityEngineStageOpenWorldArrivals,
			cityEngineStageMarkets,
		}}
	case CitySimulationVersionOpenWorldV24:
		engine = cityEngineDefinition{version: version, stages: []cityEngineStage{
			cityEngineStageControl,
			cityEngineStageLedger,
			cityEngineStageResources,
			cityEngineStageCalendarDemography,
			cityEngineStageOpenWorld,
			cityEngineStageOpenWorldServices,
			cityEngineStageOpenWorldImpacts,
			cityEngineStageOpenWorldMobilityOD,
			cityEngineStageOpenWorldCommuteLifecycle,
			cityEngineStageOpenWorldSupplyChain,
			cityEngineStageOpenWorldEnterpriseFreight,
			cityEngineStageOpenWorldEnterpriseFreightReceipts,
			cityEngineStageOpenWorldEnterpriseFreightBatches,
			cityEngineStageOpenWorldFreightSettlements,
			cityEngineStageOpenWorldCarrierRecovery,
			cityEngineStageOpenWorldCarrierCommerce,
			cityEngineStageOpenWorldInfrastructure,
			cityEngineStageOpenWorldMobility,
			cityEngineStageOpenWorldEffectiveCapacity,
			cityEngineStageOpenWorldArrivals,
			cityEngineStageMarkets,
		}}
	case CitySimulationVersionF7, CitySimulationVersionF7V2:
		engine = cityEngineDefinition{version: version, stages: []cityEngineStage{
			cityEngineStageControl,
			cityEngineStageLedger,
			cityEngineStageResources,
			cityEngineStageCalendarDemography,
			cityEngineStageSpatial,
			cityEngineStageMarkets,
		}}
	case CitySimulationVersionF7V3:
		engine = cityEngineDefinition{version: version, stages: []cityEngineStage{
			cityEngineStageControl,
			cityEngineStageLedger,
			cityEngineStageResources,
			cityEngineStageCalendarDemography,
			cityEngineStageSpatial,
			cityEngineStageDevelopment,
			cityEngineStageMarkets,
		}}
	case CitySimulationVersionF7V4:
		engine = cityEngineDefinition{version: version, stages: []cityEngineStage{
			cityEngineStageControl,
			cityEngineStageLedger,
			cityEngineStageResources,
			cityEngineStageCalendarDemography,
			cityEngineStageSpatial,
			cityEngineStageDevelopment,
			cityEngineStageEnterpriseLocation,
			cityEngineStageMarkets,
		}}
	case CitySimulationVersionF7V5, CitySimulationVersionF7V6, CitySimulationVersionF7V7,
		CitySimulationVersionF7V8, CitySimulationVersionF7V9:
		engine = cityEngineDefinition{version: version, stages: []cityEngineStage{
			cityEngineStageControl,
			cityEngineStageLedger,
			cityEngineStageResources,
			cityEngineStageCalendarDemography,
			cityEngineStageSpatial,
			cityEngineStageDevelopment,
			cityEngineStageEnterpriseLocation,
			cityEngineStageWorldRuntime,
			cityEngineStageMarkets,
		}}
	case CitySimulationVersionF8, CitySimulationVersionF8V2, CitySimulationVersionF8V3:
		engine = cityEngineDefinition{version: version, stages: []cityEngineStage{
			cityEngineStageControl,
			cityEngineStageLedger,
			cityEngineStageResources,
			cityEngineStageCalendarDemography,
			cityEngineStageSpatial,
			cityEngineStageDevelopment,
			cityEngineStageEnterpriseLocation,
			cityEngineStageWorldRuntime,
			cityEngineStagePublicServices,
			cityEngineStageMarkets,
		}}
	default:
		return cityEngineDefinition{}, fmt.Errorf("unsupported city engine version %q", version)
	}
	if err := engine.validate(); err != nil {
		return cityEngineDefinition{}, fmt.Errorf("invalid city engine definition %q: %w", version, err)
	}
	return engine, nil
}

func (engine cityEngineDefinition) hasStage(stage cityEngineStage) bool {
	for _, candidate := range engine.stages {
		if candidate == stage {
			return true
		}
	}
	return false
}

func (engine cityEngineDefinition) validate() error {
	if engine.version == "" || len(engine.stages) == 0 {
		return fmt.Errorf("version and stages are required")
	}
	positions := make(map[cityEngineStage]int, len(engine.stages))
	for index, stage := range engine.stages {
		switch stage {
		case cityEngineStageControl, cityEngineStageLedger, cityEngineStageResources,
			cityEngineStageCalendarDemography, cityEngineStageOpenWorld, cityEngineStageOpenWorldServices, cityEngineStageOpenWorldImpacts, cityEngineStageOpenWorldMobilityOD, cityEngineStageOpenWorldCommutes, cityEngineStageOpenWorldCommuteLifecycle, cityEngineStageOpenWorldSupplyChain, cityEngineStageOpenWorldEnterpriseFreight, cityEngineStageOpenWorldMobility, cityEngineStageOpenWorldArrivals, cityEngineStageSpatial,
			cityEngineStageOpenWorldEnterpriseFreightReceipts, cityEngineStageOpenWorldEnterpriseFreightBatches, cityEngineStageOpenWorldInfrastructure,
			cityEngineStageOpenWorldFreightSettlements, cityEngineStageOpenWorldCarrierRecovery, cityEngineStageOpenWorldCarrierCommerce, cityEngineStageOpenWorldEffectiveCapacity,
			cityEngineStageDevelopment, cityEngineStageEnterpriseLocation,
			cityEngineStageWorldRuntime, cityEngineStagePublicServices,
			cityEngineStageMarkets:
		default:
			return fmt.Errorf("unknown stage %q", stage)
		}
		if _, duplicate := positions[stage]; duplicate {
			return fmt.Errorf("duplicate stage %q", stage)
		}
		positions[stage] = index
	}
	control, hasControl := positions[cityEngineStageControl]
	ledger, hasLedger := positions[cityEngineStageLedger]
	resources, hasResources := positions[cityEngineStageResources]
	markets, hasMarkets := positions[cityEngineStageMarkets]
	if !hasControl {
		return fmt.Errorf("control stage is required")
	}
	if hasLedger && ledger < control {
		return fmt.Errorf("ledger stage must follow control")
	}
	if hasResources && (!hasLedger || resources < ledger) {
		return fmt.Errorf("resources stage requires ledger and must follow it")
	}
	if demography, ok := positions[cityEngineStageCalendarDemography]; ok &&
		(!hasResources || demography < resources || hasMarkets && demography > markets) {
		return fmt.Errorf("calendar demography must follow resources and precede markets")
	}
	if openWorld, ok := positions[cityEngineStageOpenWorld]; ok {
		demography, hasDemography := positions[cityEngineStageCalendarDemography]
		if !hasDemography || openWorld < demography || hasMarkets && openWorld > markets {
			return fmt.Errorf("open-world stage must follow calendar demography and precede markets")
		}
	}
	if services, ok := positions[cityEngineStageOpenWorldServices]; ok {
		openWorld, hasOpenWorld := positions[cityEngineStageOpenWorld]
		if !hasOpenWorld || services < openWorld || hasMarkets && services > markets {
			return fmt.Errorf("open-world services stage must follow open-world and precede markets")
		}
	}
	if impacts, ok := positions[cityEngineStageOpenWorldImpacts]; ok {
		services, hasServices := positions[cityEngineStageOpenWorldServices]
		if !hasServices || impacts < services || hasMarkets && impacts > markets {
			return fmt.Errorf("open-world impacts stage must follow open-world services and precede markets")
		}
	}
	if od, ok := positions[cityEngineStageOpenWorldMobilityOD]; ok {
		impacts, hasImpacts := positions[cityEngineStageOpenWorldImpacts]
		if !hasImpacts || od < impacts || hasMarkets && od > markets {
			return fmt.Errorf("open-world mobility OD stage must follow open-world impacts and precede markets")
		}
	}
	if commutes, ok := positions[cityEngineStageOpenWorldCommutes]; ok {
		od, hasOD := positions[cityEngineStageOpenWorldMobilityOD]
		if !hasOD || commutes < od || hasMarkets && commutes > markets {
			return fmt.Errorf("open-world commute source stage must follow open-world mobility OD and precede markets")
		}
	}
	if lifecycle, ok := positions[cityEngineStageOpenWorldCommuteLifecycle]; ok {
		od, hasOD := positions[cityEngineStageOpenWorldMobilityOD]
		if !hasOD || lifecycle < od || hasMarkets && lifecycle > markets {
			return fmt.Errorf("open-world commute lifecycle stage must follow open-world mobility OD and precede markets")
		}
	}
	if supplyChain, ok := positions[cityEngineStageOpenWorldSupplyChain]; ok {
		lifecycle, hasLifecycle := positions[cityEngineStageOpenWorldCommuteLifecycle]
		if !hasLifecycle || supplyChain < lifecycle || hasMarkets && supplyChain > markets {
			return fmt.Errorf("open-world supply-chain stage must follow commute lifecycle and precede markets")
		}
	}
	if freight, ok := positions[cityEngineStageOpenWorldEnterpriseFreight]; ok {
		supplyChain, hasSupplyChain := positions[cityEngineStageOpenWorldSupplyChain]
		if !hasSupplyChain || freight < supplyChain || hasMarkets && freight > markets {
			return fmt.Errorf("open-world enterprise freight stage must follow supply chain and precede markets")
		}
	}
	if receipts, ok := positions[cityEngineStageOpenWorldEnterpriseFreightReceipts]; ok {
		freight, hasFreight := positions[cityEngineStageOpenWorldEnterpriseFreight]
		if !hasFreight || receipts < freight || hasMarkets && receipts > markets {
			return fmt.Errorf("open-world enterprise freight receipt stage must follow enterprise freight and precede markets")
		}
	}
	if batches, ok := positions[cityEngineStageOpenWorldEnterpriseFreightBatches]; ok {
		receipts, hasReceipts := positions[cityEngineStageOpenWorldEnterpriseFreightReceipts]
		if !hasReceipts || batches < receipts || hasMarkets && batches > markets {
			return fmt.Errorf("open-world freight-batch stage must follow enterprise freight receipts and precede markets")
		}
	}
	if settlements, ok := positions[cityEngineStageOpenWorldFreightSettlements]; ok {
		batches, hasBatches := positions[cityEngineStageOpenWorldEnterpriseFreightBatches]
		if !hasBatches || settlements < batches || hasMarkets && settlements > markets {
			return fmt.Errorf("open-world freight-settlement stage must follow freight batches and precede markets")
		}
	}
	if carrierRecovery, ok := positions[cityEngineStageOpenWorldCarrierRecovery]; ok {
		settlements, hasSettlements := positions[cityEngineStageOpenWorldFreightSettlements]
		if !hasSettlements || carrierRecovery < settlements || hasMarkets && carrierRecovery > markets {
			return fmt.Errorf("open-world carrier-recovery stage must follow freight settlements and precede markets")
		}
	}
	if carrierCommerce, ok := positions[cityEngineStageOpenWorldCarrierCommerce]; ok {
		carrierRecovery, hasCarrierRecovery := positions[cityEngineStageOpenWorldCarrierRecovery]
		if !hasCarrierRecovery || carrierCommerce < carrierRecovery || hasMarkets && carrierCommerce > markets {
			return fmt.Errorf("open-world carrier-commerce stage must follow carrier recovery and precede markets")
		}
	}
	if infrastructure, ok := positions[cityEngineStageOpenWorldInfrastructure]; ok {
		batches, hasBatches := positions[cityEngineStageOpenWorldEnterpriseFreightBatches]
		if !hasBatches || infrastructure < batches || hasMarkets && infrastructure > markets {
			return fmt.Errorf("open-world infrastructure stage must follow freight batches and precede markets")
		}
		if settlements, hasSettlements := positions[cityEngineStageOpenWorldFreightSettlements]; hasSettlements && infrastructure < settlements {
			return fmt.Errorf("open-world infrastructure stage must follow freight settlements")
		}
		if carrierRecovery, hasCarrierRecovery := positions[cityEngineStageOpenWorldCarrierRecovery]; hasCarrierRecovery && infrastructure < carrierRecovery {
			return fmt.Errorf("open-world infrastructure stage must follow carrier recovery")
		}
		if carrierCommerce, hasCarrierCommerce := positions[cityEngineStageOpenWorldCarrierCommerce]; hasCarrierCommerce && infrastructure < carrierCommerce {
			return fmt.Errorf("open-world infrastructure stage must follow carrier commerce")
		}
	}
	if mobility, ok := positions[cityEngineStageOpenWorldMobility]; ok {
		od, hasOD := positions[cityEngineStageOpenWorldMobilityOD]
		if hasOD && mobility < od {
			return fmt.Errorf("open-world mobility stage must follow open-world mobility OD")
		}
		commutes, hasCommutes := positions[cityEngineStageOpenWorldCommutes]
		if hasCommutes && mobility < commutes {
			return fmt.Errorf("open-world mobility stage must follow open-world commute sources")
		}
		lifecycle, hasLifecycle := positions[cityEngineStageOpenWorldCommuteLifecycle]
		if hasLifecycle && mobility < lifecycle {
			return fmt.Errorf("open-world mobility stage must follow open-world commute lifecycle")
		}
		supplyChain, hasSupplyChain := positions[cityEngineStageOpenWorldSupplyChain]
		if hasSupplyChain && mobility < supplyChain {
			return fmt.Errorf("open-world mobility stage must follow open-world supply chain")
		}
		freight, hasFreight := positions[cityEngineStageOpenWorldEnterpriseFreight]
		if hasFreight && mobility < freight {
			return fmt.Errorf("open-world mobility stage must follow open-world enterprise freight")
		}
		receipts, hasReceipts := positions[cityEngineStageOpenWorldEnterpriseFreightReceipts]
		if hasReceipts && mobility < receipts {
			return fmt.Errorf("open-world mobility stage must follow enterprise freight receipts")
		}
		batches, hasBatches := positions[cityEngineStageOpenWorldEnterpriseFreightBatches]
		if hasBatches && mobility < batches {
			return fmt.Errorf("open-world mobility stage must follow freight batches")
		}
		settlements, hasSettlements := positions[cityEngineStageOpenWorldFreightSettlements]
		if hasSettlements && mobility < settlements {
			return fmt.Errorf("open-world mobility stage must follow freight settlements")
		}
		carrierRecovery, hasCarrierRecovery := positions[cityEngineStageOpenWorldCarrierRecovery]
		if hasCarrierRecovery && mobility < carrierRecovery {
			return fmt.Errorf("open-world mobility stage must follow carrier recovery")
		}
		carrierCommerce, hasCarrierCommerce := positions[cityEngineStageOpenWorldCarrierCommerce]
		if hasCarrierCommerce && mobility < carrierCommerce {
			return fmt.Errorf("open-world mobility stage must follow carrier commerce")
		}
		infrastructure, hasInfrastructure := positions[cityEngineStageOpenWorldInfrastructure]
		if hasInfrastructure && mobility < infrastructure {
			return fmt.Errorf("open-world mobility stage must follow infrastructure")
		}
		impacts, hasImpacts := positions[cityEngineStageOpenWorldImpacts]
		if !hasImpacts || mobility < impacts || hasMarkets && mobility > markets {
			return fmt.Errorf("open-world mobility stage must follow open-world impacts and precede markets")
		}
	}
	if effectiveCapacity, ok := positions[cityEngineStageOpenWorldEffectiveCapacity]; ok {
		infrastructure, hasInfrastructure := positions[cityEngineStageOpenWorldInfrastructure]
		mobility, hasMobility := positions[cityEngineStageOpenWorldMobility]
		if !hasInfrastructure || !hasMobility || effectiveCapacity < infrastructure || effectiveCapacity < mobility ||
			hasMarkets && effectiveCapacity > markets {
			return fmt.Errorf("open-world effective-capacity stage must follow infrastructure and mobility and precede markets")
		}
	}
	if arrivals, ok := positions[cityEngineStageOpenWorldArrivals]; ok {
		mobility, hasMobility := positions[cityEngineStageOpenWorldMobility]
		if !hasMobility || arrivals < mobility || hasMarkets && arrivals > markets {
			return fmt.Errorf("open-world arrivals stage must follow open-world mobility and precede markets")
		}
		if effectiveCapacity, hasEffectiveCapacity := positions[cityEngineStageOpenWorldEffectiveCapacity]; hasEffectiveCapacity && arrivals < effectiveCapacity {
			return fmt.Errorf("open-world arrivals stage must follow effective capacity")
		}
	}
	if spatial, ok := positions[cityEngineStageSpatial]; ok {
		demography, hasDemography := positions[cityEngineStageCalendarDemography]
		if !hasDemography || spatial < demography || hasMarkets && spatial > markets {
			return fmt.Errorf("spatial stage must follow calendar demography and precede markets")
		}
	}
	if development, ok := positions[cityEngineStageDevelopment]; ok {
		spatial, hasSpatial := positions[cityEngineStageSpatial]
		if !hasSpatial || development < spatial || hasMarkets && development > markets {
			return fmt.Errorf("development stage must follow spatial and precede markets")
		}
	}
	if enterpriseLocation, ok := positions[cityEngineStageEnterpriseLocation]; ok {
		development, hasDevelopment := positions[cityEngineStageDevelopment]
		if !hasDevelopment || enterpriseLocation < development || hasMarkets && enterpriseLocation > markets {
			return fmt.Errorf("enterprise location stage must follow development and precede markets")
		}
	}
	if runtime, ok := positions[cityEngineStageWorldRuntime]; ok {
		enterpriseLocation, hasEnterpriseLocation := positions[cityEngineStageEnterpriseLocation]
		if !hasEnterpriseLocation || runtime < enterpriseLocation || hasMarkets && runtime > markets {
			return fmt.Errorf("world runtime stage must follow enterprise location and precede markets")
		}
	}
	if services, ok := positions[cityEngineStagePublicServices]; ok {
		runtime, hasRuntime := positions[cityEngineStageWorldRuntime]
		if !hasRuntime || services < runtime || hasMarkets && services > markets {
			return fmt.Errorf("public services stage must follow world runtime and precede markets")
		}
	}
	if hasMarkets && (!hasLedger || !hasResources || markets < resources) {
		return fmt.Errorf("markets stage requires ledger and resources and must follow them")
	}
	return nil
}

func cityEngineStageForCommand(commandType string) (cityEngineStage, bool) {
	switch {
	case commandType == CityCommandTypeWorldRename,
		commandType == CityCommandTypeWorldSetSpeed,
		commandType == CityCommandTypeWorldPause,
		commandType == CityCommandTypeWorldResume:
		return cityEngineStageControl, true
	case isCityLedgerCommand(commandType):
		return cityEngineStageLedger, true
	case isCityResourceCommand(commandType):
		return cityEngineStageResources, true
	case isCityPopulationMigrationCommand(commandType):
		return cityEngineStageCalendarDemography, true
	case isCityHouseholdMovementCommand(commandType):
		return cityEngineStageCalendarDemography, true
	case isCityOpenWorldMobilityCommand(commandType):
		return cityEngineStageOpenWorldMobility, true
	case isCityOpenWorldCommuteLifecycleCommand(commandType):
		return cityEngineStageOpenWorldCommuteLifecycle, true
	case isCityOpenWorldFreightSettlementCommand(commandType):
		return cityEngineStageOpenWorldFreightSettlements, true
	case isCityOpenWorldCarrierRecoveryCommand(commandType):
		return cityEngineStageOpenWorldCarrierRecovery, true
	case isCityOpenWorldSupplyChainCommand(commandType):
		return cityEngineStageOpenWorldSupplyChain, true
	case isCityOpenWorldServiceCommand(commandType):
		return cityEngineStageOpenWorldServices, true
	case isCityOpenWorldCommand(commandType):
		return cityEngineStageOpenWorld, true
	case isCitySpatialCommand(commandType):
		return cityEngineStageSpatial, true
	case isCityDevelopmentCommand(commandType):
		return cityEngineStageDevelopment, true
	case isCityEnterpriseLocationCommand(commandType):
		return cityEngineStageEnterpriseLocation, true
	case isWorldRuntimeCommand(commandType):
		return cityEngineStageWorldRuntime, true
	case isCityFacilityLifecycleCommand(commandType):
		return cityEngineStagePublicServices, true
	case isCityServiceCommand(commandType):
		return cityEngineStagePublicServices, true
	case isCityPhysicalNetworkCommand(commandType):
		return cityEngineStagePublicServices, true
	default:
		return "", false
	}
}

func (engine cityEngineDefinition) supportsCommand(commandType string) bool {
	if isCityPopulationMigrationCommand(commandType) && !cityEngineSupportsPopulationMigration(engine.version) {
		return false
	}
	if isCityHouseholdMovementCommand(commandType) && !cityEngineSupportsHouseholdLifecycle(engine.version) {
		return false
	}
	if isCityOpenWorldRuntimeCommand(commandType) && !cityEngineSupportsOpenWorldRuntime(engine.version) {
		return false
	}
	if isCityOpenWorldMobilityCommand(commandType) && !cityEngineSupportsOpenWorldMobility(engine.version) {
		return false
	}
	if isCityOpenWorldCommuteLifecycleCommand(commandType) && !cityEngineSupportsOpenWorldCommuteLifecycle(engine.version) {
		return false
	}
	if isCityOpenWorldInfrastructureCommand(commandType) && !cityEngineSupportsOpenWorldInfrastructure(engine.version) {
		return false
	}
	if isCityOpenWorldFreightSettlementCommand(commandType) && !cityEngineSupportsOpenWorldFreightSettlements(engine.version) {
		return false
	}
	if isCityOpenWorldCarrierRecoveryCommand(commandType) && !cityEngineSupportsOpenWorldCarrierRecovery(engine.version) {
		return false
	}
	if isCityOpenWorldSupplyChainCommand(commandType) && !cityEngineSupportsOpenWorldSupplyChain(engine.version) {
		return false
	}
	if isCityOpenWorldSocialRuntimeCommand(commandType) && !cityEngineSupportsOpenWorldSocialRuntime(engine.version) {
		return false
	}
	if isCityOpenWorldServiceCommand(commandType) && !cityEngineSupportsOpenWorldServiceCoordination(engine.version) {
		return false
	}
	if isCityOpenWorldCommand(commandType) && !isCityOpenWorldRuntimeCommand(commandType) &&
		!cityEngineSupportsOpenWorldMaterialization(engine.version) {
		return false
	}
	if isCitySpatialCommand(commandType) && !cityEngineSupportsSpatial(engine.version) {
		return false
	}
	if isCityDevelopmentCommand(commandType) && !cityEngineSupportsDevelopment(engine.version) {
		return false
	}
	if isCityEnterpriseLocationCommand(commandType) && !cityEngineSupportsEnterpriseLocation(engine.version) {
		return false
	}
	if isWorldRuntimeCommand(commandType) && !cityEngineSupportsWorldRuntime(engine.version) {
		return false
	}
	if isWorldActorSpatialControlCommand(commandType) && !cityEngineSupportsWorldActorSpatialControl(engine.version) {
		return false
	}
	if isWorldPortalAccessCommand(commandType) && !cityEngineSupportsWorldPortalAccess(engine.version) {
		return false
	}
	if isWorldNavigationIntentCommand(commandType) && !cityEngineSupportsWorldNavigationIntents(engine.version) {
		return false
	}
	if isCityServiceCommand(commandType) && !cityEngineSupportsPublicServices(engine.version) {
		return false
	}
	if isCityFacilityLifecycleCommand(commandType) && !cityEngineSupportsFacilityLifecycle(engine.version) {
		return false
	}
	if isCityPhysicalNetworkCommand(commandType) && !cityEngineSupportsPhysicalNetworks(engine.version) {
		return false
	}
	stage, known := cityEngineStageForCommand(commandType)
	return known && engine.hasStage(stage)
}

func cityEngineUpgradeTargets(version string) []string {
	switch version {
	case CitySimulationVersionF5:
		return []string{CitySimulationVersionF6}
	case CitySimulationVersionF6:
		return []string{CitySimulationVersionF6V2}
	case CitySimulationVersionF6V2:
		return []string{CitySimulationVersionF6V3}
	case CitySimulationVersionF6V3:
		return []string{CitySimulationVersionF7}
	case CitySimulationVersionF7:
		return []string{CitySimulationVersionF7V2}
	case CitySimulationVersionF7V2:
		return []string{CitySimulationVersionF7V3}
	case CitySimulationVersionF7V3:
		return []string{CitySimulationVersionF7V4}
	case CitySimulationVersionF7V4:
		return []string{CitySimulationVersionF7V5}
	case CitySimulationVersionF7V5:
		return []string{CitySimulationVersionF7V6}
	case CitySimulationVersionF7V6:
		return []string{CitySimulationVersionF7V7}
	case CitySimulationVersionF7V7:
		return []string{CitySimulationVersionF7V8}
	case CitySimulationVersionF7V8:
		return []string{CitySimulationVersionF7V9}
	case CitySimulationVersionF7V9:
		return []string{CitySimulationVersionF8}
	case CitySimulationVersionF8:
		return []string{CitySimulationVersionF8V2}
	case CitySimulationVersionF8V2:
		return []string{CitySimulationVersionF8V3}
	case CitySimulationVersionOpenWorldV5:
		return []string{CitySimulationVersionOpenWorldV6}
	case CitySimulationVersionOpenWorldV6:
		return []string{CitySimulationVersionOpenWorldV7}
	case CitySimulationVersionOpenWorldV7:
		return []string{CitySimulationVersionOpenWorldV8}
	case CitySimulationVersionOpenWorldV8:
		return []string{CitySimulationVersionOpenWorldV9}
	case CitySimulationVersionOpenWorldV9:
		return []string{CitySimulationVersionOpenWorldV10}
	case CitySimulationVersionOpenWorldV10:
		return []string{CitySimulationVersionOpenWorldV11}
	case CitySimulationVersionOpenWorldV11:
		return []string{CitySimulationVersionOpenWorldV12}
	case CitySimulationVersionOpenWorldV12:
		return []string{CitySimulationVersionOpenWorldV13}
	case CitySimulationVersionOpenWorldV13:
		return []string{CitySimulationVersionOpenWorldV14}
	case CitySimulationVersionOpenWorldV14:
		return []string{CitySimulationVersionOpenWorldV15}
	case CitySimulationVersionOpenWorldV15:
		return []string{CitySimulationVersionOpenWorldV16}
	case CitySimulationVersionOpenWorldV16:
		return []string{CitySimulationVersionOpenWorldV17}
	case CitySimulationVersionOpenWorldV17:
		return []string{CitySimulationVersionOpenWorldV18}
	case CitySimulationVersionOpenWorldV18:
		return []string{CitySimulationVersionOpenWorldV19}
	case CitySimulationVersionOpenWorldV19:
		return []string{CitySimulationVersionOpenWorldV20}
	case CitySimulationVersionOpenWorldV20:
		return []string{CitySimulationVersionOpenWorldV21}
	case CitySimulationVersionOpenWorldV21:
		return []string{CitySimulationVersionOpenWorldV22}
	case CitySimulationVersionOpenWorldV22:
		return []string{CitySimulationVersionOpenWorldV23}
	case CitySimulationVersionOpenWorldV23:
		return []string{CitySimulationVersionOpenWorldV24}
	default:
		return []string{}
	}
}

func cityEngineCanUpgrade(fromVersion, toVersion string) bool {
	return (fromVersion == CitySimulationVersionF5 && toVersion == CitySimulationVersionF6) ||
		(fromVersion == CitySimulationVersionF6 && toVersion == CitySimulationVersionF6V2) ||
		(fromVersion == CitySimulationVersionF6V2 && toVersion == CitySimulationVersionF6V3) ||
		(fromVersion == CitySimulationVersionF6V3 && toVersion == CitySimulationVersionF7) ||
		(fromVersion == CitySimulationVersionF7 && toVersion == CitySimulationVersionF7V2) ||
		(fromVersion == CitySimulationVersionF7V2 && toVersion == CitySimulationVersionF7V3) ||
		(fromVersion == CitySimulationVersionF7V3 && toVersion == CitySimulationVersionF7V4) ||
		(fromVersion == CitySimulationVersionF7V4 && toVersion == CitySimulationVersionF7V5) ||
		(fromVersion == CitySimulationVersionF7V5 && toVersion == CitySimulationVersionF7V6) ||
		(fromVersion == CitySimulationVersionF7V6 && toVersion == CitySimulationVersionF7V7) ||
		(fromVersion == CitySimulationVersionF7V7 && toVersion == CitySimulationVersionF7V8) ||
		(fromVersion == CitySimulationVersionF7V8 && toVersion == CitySimulationVersionF7V9) ||
		(fromVersion == CitySimulationVersionF7V9 && toVersion == CitySimulationVersionF8) ||
		(fromVersion == CitySimulationVersionF8 && toVersion == CitySimulationVersionF8V2) ||
		(fromVersion == CitySimulationVersionF8V2 && toVersion == CitySimulationVersionF8V3) ||
		(fromVersion == CitySimulationVersionOpenWorldV5 && toVersion == CitySimulationVersionOpenWorldV6) ||
		(fromVersion == CitySimulationVersionOpenWorldV6 && toVersion == CitySimulationVersionOpenWorldV7) ||
		(fromVersion == CitySimulationVersionOpenWorldV7 && toVersion == CitySimulationVersionOpenWorldV8) ||
		(fromVersion == CitySimulationVersionOpenWorldV8 && toVersion == CitySimulationVersionOpenWorldV9) ||
		(fromVersion == CitySimulationVersionOpenWorldV9 && toVersion == CitySimulationVersionOpenWorldV10) ||
		(fromVersion == CitySimulationVersionOpenWorldV10 && toVersion == CitySimulationVersionOpenWorldV11) ||
		(fromVersion == CitySimulationVersionOpenWorldV11 && toVersion == CitySimulationVersionOpenWorldV12) ||
		(fromVersion == CitySimulationVersionOpenWorldV12 && toVersion == CitySimulationVersionOpenWorldV13) ||
		(fromVersion == CitySimulationVersionOpenWorldV13 && toVersion == CitySimulationVersionOpenWorldV14) ||
		(fromVersion == CitySimulationVersionOpenWorldV14 && toVersion == CitySimulationVersionOpenWorldV15) ||
		(fromVersion == CitySimulationVersionOpenWorldV15 && toVersion == CitySimulationVersionOpenWorldV16) ||
		(fromVersion == CitySimulationVersionOpenWorldV16 && toVersion == CitySimulationVersionOpenWorldV17) ||
		(fromVersion == CitySimulationVersionOpenWorldV17 && toVersion == CitySimulationVersionOpenWorldV18) ||
		(fromVersion == CitySimulationVersionOpenWorldV18 && toVersion == CitySimulationVersionOpenWorldV19) ||
		(fromVersion == CitySimulationVersionOpenWorldV19 && toVersion == CitySimulationVersionOpenWorldV20) ||
		(fromVersion == CitySimulationVersionOpenWorldV20 && toVersion == CitySimulationVersionOpenWorldV21) ||
		(fromVersion == CitySimulationVersionOpenWorldV21 && toVersion == CitySimulationVersionOpenWorldV22) ||
		(fromVersion == CitySimulationVersionOpenWorldV22 && toVersion == CitySimulationVersionOpenWorldV23) ||
		(fromVersion == CitySimulationVersionOpenWorldV23 && toVersion == CitySimulationVersionOpenWorldV24)
}

func marshalCanonicalCityState(state cityHashState) ([]byte, error) {
	if state.SimulationVersion != CitySimulationVersionF8V3 {
		state.PhysicalNetworks = nil
	}
	switch state.SimulationVersion {
	case CitySimulationVersionOpenWorld, CitySimulationVersionOpenWorldV2, CitySimulationVersionOpenWorldV3:
		if state.OpenWorld == nil {
			return nil, fmt.Errorf("city open-world canonical state requires V2 genesis state")
		}
		state.Spatial = nil
		state.Land = nil
		state.Development = nil
		state.EnterpriseLocation = nil
		state.WorldRuntime = nil
		state.OpenWorldRuntime = nil
		state.PublicServices = nil
		state.FacilityLifecycle = nil
		state.Physical = cityPhysicalStateWithoutHouseholdUnits(state.Physical)
		return json.Marshal(state)
	case CitySimulationVersionOpenWorldV11, CitySimulationVersionOpenWorldV12, CitySimulationVersionOpenWorldV13, CitySimulationVersionOpenWorldV14, CitySimulationVersionOpenWorldV15, CitySimulationVersionOpenWorldV16, CitySimulationVersionOpenWorldV17, CitySimulationVersionOpenWorldV18, CitySimulationVersionOpenWorldV19, CitySimulationVersionOpenWorldV20, CitySimulationVersionOpenWorldV21, CitySimulationVersionOpenWorldV22, CitySimulationVersionOpenWorldV23, CitySimulationVersionOpenWorldV24:
		if state.OpenWorld == nil || state.OpenWorldRuntime == nil ||
			state.OpenWorldRuntime.Profile.RuntimeID != cityOpenWorldSocialRuntimeID ||
			state.OpenWorldRuntime.Social == nil || state.OpenWorldRuntime.Services == nil ||
			state.OpenWorldRuntime.Impacts == nil || state.OpenWorldRuntime.Mobility == nil ||
			state.OpenWorldRuntime.Arrivals == nil || state.OpenWorldRuntime.OD == nil || state.VersionVector == nil {
			return nil, fmt.Errorf("city open-world V11+ canonical state requires static, social-runtime, service, impact, mobility, arrival, OD, and version-vector state")
		}
		if state.SimulationVersion == CitySimulationVersionOpenWorldV12 || state.SimulationVersion == CitySimulationVersionOpenWorldV13 || state.SimulationVersion == CitySimulationVersionOpenWorldV14 || state.SimulationVersion == CitySimulationVersionOpenWorldV15 || state.SimulationVersion == CitySimulationVersionOpenWorldV16 || state.SimulationVersion == CitySimulationVersionOpenWorldV17 || state.SimulationVersion == CitySimulationVersionOpenWorldV18 || state.SimulationVersion == CitySimulationVersionOpenWorldV19 || state.SimulationVersion == CitySimulationVersionOpenWorldV20 || state.SimulationVersion == CitySimulationVersionOpenWorldV21 || state.SimulationVersion == CitySimulationVersionOpenWorldV22 || state.SimulationVersion == CitySimulationVersionOpenWorldV23 || state.SimulationVersion == CitySimulationVersionOpenWorldV24 {
			if state.OpenWorldRuntime.Commutes == nil {
				return nil, fmt.Errorf("city open-world V12 canonical state requires commute binding state")
			}
			if err := validateCityOpenWorldCommuteState(state.OpenWorldRuntime.Commutes); err != nil {
				return nil, fmt.Errorf("city open-world V12 canonical state has invalid commute state: %w", err)
			}
		} else if state.OpenWorldRuntime.Commutes != nil {
			return nil, fmt.Errorf("city open-world V11 canonical state contains V12 commute state")
		}
		if state.SimulationVersion == CitySimulationVersionOpenWorldV13 || state.SimulationVersion == CitySimulationVersionOpenWorldV14 || state.SimulationVersion == CitySimulationVersionOpenWorldV15 || state.SimulationVersion == CitySimulationVersionOpenWorldV16 || state.SimulationVersion == CitySimulationVersionOpenWorldV17 || state.SimulationVersion == CitySimulationVersionOpenWorldV18 || state.SimulationVersion == CitySimulationVersionOpenWorldV19 || state.SimulationVersion == CitySimulationVersionOpenWorldV20 || state.SimulationVersion == CitySimulationVersionOpenWorldV21 || state.SimulationVersion == CitySimulationVersionOpenWorldV22 || state.SimulationVersion == CitySimulationVersionOpenWorldV23 || state.SimulationVersion == CitySimulationVersionOpenWorldV24 {
			if state.OpenWorldRuntime.CommuteSources == nil {
				return nil, fmt.Errorf("city open-world V13 canonical state requires commute source state")
			}
			if err := validateCityOpenWorldCommuteSourceState(state.OpenWorldRuntime.CommuteSources); err != nil {
				return nil, fmt.Errorf("city open-world V13 canonical state has invalid commute source state: %w", err)
			}
		} else if state.OpenWorldRuntime.CommuteSources != nil {
			return nil, fmt.Errorf("city open-world pre-V13 canonical state contains commute source state")
		}
		if state.SimulationVersion == CitySimulationVersionOpenWorldV14 || state.SimulationVersion == CitySimulationVersionOpenWorldV15 || state.SimulationVersion == CitySimulationVersionOpenWorldV16 || state.SimulationVersion == CitySimulationVersionOpenWorldV17 || state.SimulationVersion == CitySimulationVersionOpenWorldV18 || state.SimulationVersion == CitySimulationVersionOpenWorldV19 || state.SimulationVersion == CitySimulationVersionOpenWorldV20 || state.SimulationVersion == CitySimulationVersionOpenWorldV21 || state.SimulationVersion == CitySimulationVersionOpenWorldV22 || state.SimulationVersion == CitySimulationVersionOpenWorldV23 || state.SimulationVersion == CitySimulationVersionOpenWorldV24 {
			if state.OpenWorldRuntime.CommuteLifecycle == nil {
				return nil, fmt.Errorf("city open-world V14 canonical state requires commute lifecycle state")
			}
			if err := validateCityOpenWorldCommuteLifecycleState(state.OpenWorldRuntime.CommuteLifecycle); err != nil {
				return nil, fmt.Errorf("city open-world V14 canonical state has invalid commute lifecycle state: %w", err)
			}
		} else if state.OpenWorldRuntime.CommuteLifecycle != nil {
			return nil, fmt.Errorf("city open-world pre-V14 canonical state contains commute lifecycle state")
		}
		if state.SimulationVersion == CitySimulationVersionOpenWorldV15 || state.SimulationVersion == CitySimulationVersionOpenWorldV16 || state.SimulationVersion == CitySimulationVersionOpenWorldV17 || state.SimulationVersion == CitySimulationVersionOpenWorldV18 || state.SimulationVersion == CitySimulationVersionOpenWorldV19 || state.SimulationVersion == CitySimulationVersionOpenWorldV20 || state.SimulationVersion == CitySimulationVersionOpenWorldV21 || state.SimulationVersion == CitySimulationVersionOpenWorldV22 || state.SimulationVersion == CitySimulationVersionOpenWorldV23 || state.SimulationVersion == CitySimulationVersionOpenWorldV24 {
			if state.OpenWorldRuntime.SupplyChain == nil {
				return nil, fmt.Errorf("city open-world V15 canonical state requires supply-chain state")
			}
			if err := validateCityOpenWorldSupplyChainState(state.OpenWorldRuntime.SupplyChain); err != nil {
				return nil, fmt.Errorf("city open-world V15 canonical state has invalid supply-chain state: %w", err)
			}
		} else if state.OpenWorldRuntime.SupplyChain != nil {
			return nil, fmt.Errorf("city open-world pre-V15 canonical state contains supply-chain state")
		}
		if state.SimulationVersion == CitySimulationVersionOpenWorldV16 || state.SimulationVersion == CitySimulationVersionOpenWorldV17 || state.SimulationVersion == CitySimulationVersionOpenWorldV18 || state.SimulationVersion == CitySimulationVersionOpenWorldV19 || state.SimulationVersion == CitySimulationVersionOpenWorldV20 || state.SimulationVersion == CitySimulationVersionOpenWorldV21 || state.SimulationVersion == CitySimulationVersionOpenWorldV22 || state.SimulationVersion == CitySimulationVersionOpenWorldV23 || state.SimulationVersion == CitySimulationVersionOpenWorldV24 {
			if state.OpenWorldRuntime.EnterpriseFreight == nil {
				return nil, fmt.Errorf("city open-world V16 canonical state requires enterprise-freight state")
			}
			if err := validateCityOpenWorldEnterpriseFreightState(state.OpenWorldRuntime.EnterpriseFreight); err != nil {
				return nil, fmt.Errorf("city open-world V16 canonical state has invalid enterprise-freight state: %w", err)
			}
		} else if state.OpenWorldRuntime.EnterpriseFreight != nil {
			return nil, fmt.Errorf("city open-world pre-V16 canonical state contains enterprise-freight state")
		}
		if state.SimulationVersion == CitySimulationVersionOpenWorldV17 || state.SimulationVersion == CitySimulationVersionOpenWorldV18 || state.SimulationVersion == CitySimulationVersionOpenWorldV19 || state.SimulationVersion == CitySimulationVersionOpenWorldV20 || state.SimulationVersion == CitySimulationVersionOpenWorldV21 || state.SimulationVersion == CitySimulationVersionOpenWorldV22 || state.SimulationVersion == CitySimulationVersionOpenWorldV23 || state.SimulationVersion == CitySimulationVersionOpenWorldV24 {
			if state.OpenWorldRuntime.EnterpriseFreightReceipts == nil {
				return nil, fmt.Errorf("city open-world V17 canonical state requires freight-receipt state")
			}
			if err := validateCityOpenWorldEnterpriseFreightReceiptState(state.OpenWorldRuntime.EnterpriseFreightReceipts); err != nil {
				return nil, fmt.Errorf("city open-world V17 canonical state has invalid freight-receipt state: %w", err)
			}
		} else if state.OpenWorldRuntime.EnterpriseFreightReceipts != nil {
			return nil, fmt.Errorf("city open-world pre-V17 canonical state contains freight-receipt state")
		}
		if state.SimulationVersion == CitySimulationVersionOpenWorldV18 || state.SimulationVersion == CitySimulationVersionOpenWorldV19 || state.SimulationVersion == CitySimulationVersionOpenWorldV20 || state.SimulationVersion == CitySimulationVersionOpenWorldV21 || state.SimulationVersion == CitySimulationVersionOpenWorldV22 || state.SimulationVersion == CitySimulationVersionOpenWorldV23 || state.SimulationVersion == CitySimulationVersionOpenWorldV24 {
			if state.OpenWorldRuntime.EnterpriseFreightBatches == nil {
				return nil, fmt.Errorf("city open-world V18 canonical state requires freight-batch state")
			}
			if err := validateCityOpenWorldFreightBatchState(state.OpenWorldRuntime.EnterpriseFreightBatches); err != nil {
				return nil, fmt.Errorf("city open-world V18 canonical state has invalid freight-batch state: %w", err)
			}
		} else if state.OpenWorldRuntime.EnterpriseFreightBatches != nil {
			return nil, fmt.Errorf("city open-world pre-V18 canonical state contains freight-batch state")
		}
		if state.SimulationVersion == CitySimulationVersionOpenWorldV19 || state.SimulationVersion == CitySimulationVersionOpenWorldV20 || state.SimulationVersion == CitySimulationVersionOpenWorldV21 || state.SimulationVersion == CitySimulationVersionOpenWorldV22 || state.SimulationVersion == CitySimulationVersionOpenWorldV23 || state.SimulationVersion == CitySimulationVersionOpenWorldV24 {
			if state.OpenWorldRuntime.SpatialNetwork == nil {
				return nil, fmt.Errorf("city open-world V19 canonical state requires spatial-network state")
			}
			if err := validateCityOpenWorldSpatialNetworkState(state.OpenWorldRuntime.SpatialNetwork); err != nil {
				return nil, fmt.Errorf("city open-world V19 canonical state has invalid spatial-network state: %w", err)
			}
		} else if state.OpenWorldRuntime.SpatialNetwork != nil {
			return nil, fmt.Errorf("city open-world pre-V19 canonical state contains spatial-network state")
		}
		if state.SimulationVersion == CitySimulationVersionOpenWorldV20 || state.SimulationVersion == CitySimulationVersionOpenWorldV21 || state.SimulationVersion == CitySimulationVersionOpenWorldV22 || state.SimulationVersion == CitySimulationVersionOpenWorldV23 || state.SimulationVersion == CitySimulationVersionOpenWorldV24 {
			if state.OpenWorldRuntime.Infrastructure == nil {
				return nil, fmt.Errorf("city open-world V20 canonical state requires infrastructure state")
			}
			if err := validateCityOpenWorldInfrastructureState(state.OpenWorldRuntime.Infrastructure); err != nil {
				return nil, fmt.Errorf("city open-world V20 canonical state has invalid infrastructure state: %w", err)
			}
		} else if state.OpenWorldRuntime.Infrastructure != nil {
			return nil, fmt.Errorf("city open-world pre-V20 canonical state contains infrastructure state")
		}
		if state.SimulationVersion == CitySimulationVersionOpenWorldV21 || state.SimulationVersion == CitySimulationVersionOpenWorldV22 || state.SimulationVersion == CitySimulationVersionOpenWorldV23 || state.SimulationVersion == CitySimulationVersionOpenWorldV24 {
			if state.OpenWorldRuntime.EffectiveCapacity == nil {
				return nil, fmt.Errorf("city open-world V21 canonical state requires effective-capacity state")
			}
			if err := validateCityOpenWorldEffectiveCapacityRuntimeState(state.OpenWorldRuntime); err != nil {
				return nil, fmt.Errorf("city open-world V21 canonical state has invalid effective-capacity state: %w", err)
			}
		} else if state.OpenWorldRuntime.EffectiveCapacity != nil {
			return nil, fmt.Errorf("city open-world pre-V21 canonical state contains effective-capacity state")
		}
		if state.SimulationVersion == CitySimulationVersionOpenWorldV22 || state.SimulationVersion == CitySimulationVersionOpenWorldV23 || state.SimulationVersion == CitySimulationVersionOpenWorldV24 {
			if state.OpenWorldRuntime.FreightSettlements == nil {
				return nil, fmt.Errorf("city open-world V22 canonical state requires freight-settlement state")
			}
			if err := validateCityOpenWorldFreightSettlementState(state.OpenWorldRuntime.FreightSettlements); err != nil {
				return nil, fmt.Errorf("city open-world V22 canonical state has invalid freight-settlement state: %w", err)
			}
		} else if state.OpenWorldRuntime.FreightSettlements != nil {
			return nil, fmt.Errorf("city open-world pre-V22 canonical state contains freight-settlement state")
		}
		if state.SimulationVersion == CitySimulationVersionOpenWorldV23 || state.SimulationVersion == CitySimulationVersionOpenWorldV24 {
			if state.OpenWorldRuntime.CarrierRecovery == nil {
				return nil, fmt.Errorf("city open-world V23 canonical state requires carrier-recovery state")
			}
			if err := validateCityOpenWorldCarrierRecoveryState(state.OpenWorldRuntime.CarrierRecovery); err != nil {
				return nil, fmt.Errorf("city open-world V23 canonical state has invalid carrier-recovery state: %w", err)
			}
		} else if state.OpenWorldRuntime.CarrierRecovery != nil {
			return nil, fmt.Errorf("city open-world pre-V23 canonical state contains carrier-recovery state")
		}
		if state.SimulationVersion == CitySimulationVersionOpenWorldV24 {
			if state.OpenWorldRuntime.CarrierCommerce == nil {
				return nil, fmt.Errorf("city open-world V24 canonical state requires carrier-commerce state")
			}
			if err := validateCityOpenWorldCarrierCommerceState(state.OpenWorldRuntime.CarrierCommerce); err != nil {
				return nil, fmt.Errorf("city open-world V24 canonical state has invalid carrier-commerce state: %w", err)
			}
		} else if state.OpenWorldRuntime.CarrierCommerce != nil {
			return nil, fmt.Errorf("city open-world pre-V24 canonical state contains carrier-commerce state")
		}
		if err := validateCityWorldVersionVector(*state.VersionVector); err != nil {
			return nil, fmt.Errorf("city open-world V11+ canonical state has invalid version vector: %w", err)
		}
		if err := validateCityOpenWorldImpactState(state.OpenWorldRuntime.Impacts); err != nil {
			return nil, fmt.Errorf("city open-world V11+ canonical state has invalid impact state: %w", err)
		}
		if err := validateCityOpenWorldMobilityState(state.OpenWorldRuntime.Mobility); err != nil {
			return nil, fmt.Errorf("city open-world V11+ canonical state has invalid mobility state: %w", err)
		}
		if err := validateCityOpenWorldMobilityArrivalState(state.OpenWorldRuntime.Arrivals); err != nil {
			return nil, fmt.Errorf("city open-world V11+ canonical state has invalid arrival state: %w", err)
		}
		if err := validateCityOpenWorldMobilityODState(state.OpenWorldRuntime.OD); err != nil {
			return nil, fmt.Errorf("city open-world V11+ canonical state has invalid OD state: %w", err)
		}
		state.Spatial = nil
		state.Land = nil
		state.Development = nil
		state.EnterpriseLocation = nil
		state.WorldRuntime = nil
		state.PublicServices = nil
		state.FacilityLifecycle = nil
		state.Physical = cityPhysicalStateWithoutHouseholdUnits(state.Physical)
		return json.Marshal(state)
	case CitySimulationVersionOpenWorldV4:
		if state.OpenWorld == nil || state.OpenWorldRuntime == nil ||
			state.OpenWorldRuntime.Profile.RuntimeID != cityOpenWorldRuntimeID ||
			state.OpenWorldRuntime.Social != nil || state.OpenWorldRuntime.Services != nil {
			return nil, fmt.Errorf("city open-world V4 canonical state requires static and runtime state")
		}
		state.Spatial = nil
		state.Land = nil
		state.Development = nil
		state.EnterpriseLocation = nil
		state.WorldRuntime = nil
		state.PublicServices = nil
		state.FacilityLifecycle = nil
		state.Physical = cityPhysicalStateWithoutHouseholdUnits(state.Physical)
		return json.Marshal(state)
	case CitySimulationVersionOpenWorldV5:
		if state.OpenWorld == nil || state.OpenWorldRuntime == nil ||
			state.OpenWorldRuntime.Profile.RuntimeID != cityOpenWorldSocialRuntimeID ||
			state.OpenWorldRuntime.Social == nil || state.OpenWorldRuntime.Services != nil {
			return nil, fmt.Errorf("city open-world V5 canonical state requires static, runtime, and social state")
		}
		state.Spatial = nil
		state.Land = nil
		state.Development = nil
		state.EnterpriseLocation = nil
		state.WorldRuntime = nil
		state.PublicServices = nil
		state.FacilityLifecycle = nil
		state.Physical = cityPhysicalStateWithoutHouseholdUnits(state.Physical)
		return json.Marshal(state)
	case CitySimulationVersionOpenWorldV6:
		if state.OpenWorld == nil || state.OpenWorldRuntime == nil ||
			state.OpenWorldRuntime.Profile.RuntimeID != cityOpenWorldSocialRuntimeID ||
			state.OpenWorldRuntime.Social == nil || state.OpenWorldRuntime.Services != nil || state.VersionVector == nil {
			return nil, fmt.Errorf("city open-world V6 canonical state requires static, social runtime, and version-vector state")
		}
		if err := validateCityWorldVersionVector(*state.VersionVector); err != nil {
			return nil, fmt.Errorf("city open-world V6 canonical state has invalid version vector: %w", err)
		}
		state.Spatial = nil
		state.Land = nil
		state.Development = nil
		state.EnterpriseLocation = nil
		state.WorldRuntime = nil
		state.PublicServices = nil
		state.FacilityLifecycle = nil
		state.Physical = cityPhysicalStateWithoutHouseholdUnits(state.Physical)
		return json.Marshal(state)
	case CitySimulationVersionOpenWorldV7:
		if state.OpenWorld == nil || state.OpenWorldRuntime == nil ||
			state.OpenWorldRuntime.Profile.RuntimeID != cityOpenWorldSocialRuntimeID ||
			state.OpenWorldRuntime.Social == nil || state.OpenWorldRuntime.Services == nil ||
			state.OpenWorldRuntime.Impacts != nil || state.VersionVector == nil {
			return nil, fmt.Errorf("city open-world V7 canonical state requires static, social-runtime, service, and version-vector state")
		}
		if err := validateCityWorldVersionVector(*state.VersionVector); err != nil {
			return nil, fmt.Errorf("city open-world V7 canonical state has invalid version vector: %w", err)
		}
		state.Spatial = nil
		state.Land = nil
		state.Development = nil
		state.EnterpriseLocation = nil
		state.WorldRuntime = nil
		state.PublicServices = nil
		state.FacilityLifecycle = nil
		state.Physical = cityPhysicalStateWithoutHouseholdUnits(state.Physical)
		return json.Marshal(state)
	case CitySimulationVersionOpenWorldV8:
		if state.OpenWorld == nil || state.OpenWorldRuntime == nil ||
			state.OpenWorldRuntime.Profile.RuntimeID != cityOpenWorldSocialRuntimeID ||
			state.OpenWorldRuntime.Social == nil || state.OpenWorldRuntime.Services == nil ||
			state.OpenWorldRuntime.Impacts == nil || state.VersionVector == nil {
			return nil, fmt.Errorf("city open-world V8 canonical state requires static, social-runtime, service, impact, and version-vector state")
		}
		if err := validateCityWorldVersionVector(*state.VersionVector); err != nil {
			return nil, fmt.Errorf("city open-world V8 canonical state has invalid version vector: %w", err)
		}
		if err := validateCityOpenWorldImpactState(state.OpenWorldRuntime.Impacts); err != nil {
			return nil, fmt.Errorf("city open-world V8 canonical state has invalid impact state: %w", err)
		}
		state.Spatial = nil
		state.Land = nil
		state.Development = nil
		state.EnterpriseLocation = nil
		state.WorldRuntime = nil
		state.PublicServices = nil
		state.FacilityLifecycle = nil
		state.Physical = cityPhysicalStateWithoutHouseholdUnits(state.Physical)
		return json.Marshal(state)
	case CitySimulationVersionOpenWorldV9:
		if state.OpenWorld == nil || state.OpenWorldRuntime == nil ||
			state.OpenWorldRuntime.Profile.RuntimeID != cityOpenWorldSocialRuntimeID ||
			state.OpenWorldRuntime.Social == nil || state.OpenWorldRuntime.Services == nil ||
			state.OpenWorldRuntime.Impacts == nil || state.OpenWorldRuntime.Mobility == nil || state.VersionVector == nil {
			return nil, fmt.Errorf("city open-world V9 canonical state requires static, social-runtime, service, impact, mobility, and version-vector state")
		}
		if err := validateCityWorldVersionVector(*state.VersionVector); err != nil {
			return nil, fmt.Errorf("city open-world V9 canonical state has invalid version vector: %w", err)
		}
		if err := validateCityOpenWorldImpactState(state.OpenWorldRuntime.Impacts); err != nil {
			return nil, fmt.Errorf("city open-world V9 canonical state has invalid impact state: %w", err)
		}
		if err := validateCityOpenWorldMobilityState(state.OpenWorldRuntime.Mobility); err != nil {
			return nil, fmt.Errorf("city open-world V9 canonical state has invalid mobility state: %w", err)
		}
		state.Spatial = nil
		state.Land = nil
		state.Development = nil
		state.EnterpriseLocation = nil
		state.WorldRuntime = nil
		state.PublicServices = nil
		state.FacilityLifecycle = nil
		state.Physical = cityPhysicalStateWithoutHouseholdUnits(state.Physical)
		return json.Marshal(state)
	case CitySimulationVersionOpenWorldV10:
		if state.OpenWorld == nil || state.OpenWorldRuntime == nil ||
			state.OpenWorldRuntime.Profile.RuntimeID != cityOpenWorldSocialRuntimeID ||
			state.OpenWorldRuntime.Social == nil || state.OpenWorldRuntime.Services == nil ||
			state.OpenWorldRuntime.Impacts == nil || state.OpenWorldRuntime.Mobility == nil ||
			state.OpenWorldRuntime.Arrivals == nil || state.VersionVector == nil {
			return nil, fmt.Errorf("city open-world V10 canonical state requires static, social-runtime, service, impact, mobility, arrival, and version-vector state")
		}
		if err := validateCityWorldVersionVector(*state.VersionVector); err != nil {
			return nil, fmt.Errorf("city open-world V10 canonical state has invalid version vector: %w", err)
		}
		if err := validateCityOpenWorldImpactState(state.OpenWorldRuntime.Impacts); err != nil {
			return nil, fmt.Errorf("city open-world V10 canonical state has invalid impact state: %w", err)
		}
		if err := validateCityOpenWorldMobilityState(state.OpenWorldRuntime.Mobility); err != nil {
			return nil, fmt.Errorf("city open-world V10 canonical state has invalid mobility state: %w", err)
		}
		if err := validateCityOpenWorldMobilityArrivalState(state.OpenWorldRuntime.Arrivals); err != nil {
			return nil, fmt.Errorf("city open-world V10 canonical state has invalid arrival state: %w", err)
		}
		state.Spatial = nil
		state.Land = nil
		state.Development = nil
		state.EnterpriseLocation = nil
		state.WorldRuntime = nil
		state.PublicServices = nil
		state.FacilityLifecycle = nil
		state.Physical = cityPhysicalStateWithoutHouseholdUnits(state.Physical)
		return json.Marshal(state)
	case CitySimulationVersionF8V3:
		if state.Spatial == nil || state.Land == nil || state.Development == nil ||
			state.EnterpriseLocation == nil || state.WorldRuntime == nil ||
			state.WorldRuntime.Locations == nil || state.WorldRuntime.ControlGrants == nil ||
			state.WorldRuntime.PortalStates == nil || state.WorldRuntime.NavigationProfile == nil ||
			state.WorldRuntime.NavigationIntents == nil || state.PublicServices == nil ||
			state.FacilityLifecycle == nil || state.PhysicalNetworks == nil {
			return nil, fmt.Errorf("city F8.2 canonical state requires F8.1 state and physical network state")
		}
		return json.Marshal(state)
	case CitySimulationVersionF8V2:
		if state.Spatial == nil || state.Land == nil || state.Development == nil ||
			state.EnterpriseLocation == nil || state.WorldRuntime == nil ||
			state.WorldRuntime.Locations == nil || state.WorldRuntime.ControlGrants == nil ||
			state.WorldRuntime.PortalStates == nil || state.WorldRuntime.NavigationProfile == nil ||
			state.WorldRuntime.NavigationIntents == nil || state.PublicServices == nil ||
			state.FacilityLifecycle == nil {
			return nil, fmt.Errorf("city F8.1 canonical state requires F8.0 services and facility lifecycle state")
		}
		return json.Marshal(state)
	case CitySimulationVersionF8:
		if state.Spatial == nil || state.Land == nil || state.Development == nil ||
			state.EnterpriseLocation == nil || state.WorldRuntime == nil ||
			state.WorldRuntime.Locations == nil || state.WorldRuntime.ControlGrants == nil ||
			state.WorldRuntime.PortalStates == nil || state.WorldRuntime.NavigationProfile == nil ||
			state.WorldRuntime.NavigationIntents == nil || state.PublicServices == nil {
			return nil, fmt.Errorf("city F8 canonical state requires the complete F7.11 state and public-service state")
		}
		state.FacilityLifecycle = nil
		return json.Marshal(state)
	case CitySimulationVersionF7V9:
		if state.Spatial == nil || state.Land == nil || state.Development == nil ||
			state.EnterpriseLocation == nil || state.WorldRuntime == nil ||
			state.WorldRuntime.Locations == nil || state.WorldRuntime.ControlGrants == nil ||
			state.WorldRuntime.PortalStates == nil || state.WorldRuntime.NavigationProfile == nil ||
			state.WorldRuntime.NavigationIntents == nil {
			return nil, fmt.Errorf("city F7.11 canonical state requires spatial, land, development, enterprise location, actor spatial-control, portal, and navigation-intent state")
		}
		state.PublicServices = nil
		state.FacilityLifecycle = nil
		return json.Marshal(state)
	case CitySimulationVersionF7V8:
		if state.Spatial == nil || state.Land == nil || state.Development == nil ||
			state.EnterpriseLocation == nil || state.WorldRuntime == nil ||
			state.WorldRuntime.Locations == nil || state.WorldRuntime.ControlGrants == nil ||
			state.WorldRuntime.PortalStates == nil {
			return nil, fmt.Errorf("city F7.10 canonical state requires spatial, land, development, enterprise location, actor spatial-control, and portal state")
		}
		state.WorldRuntime.NavigationProfile = nil
		state.WorldRuntime.NavigationIntents = nil
		state.PublicServices = nil
		return json.Marshal(state)
	case CitySimulationVersionF7V6, CitySimulationVersionF7V7:
		if state.Spatial == nil || state.Land == nil || state.Development == nil ||
			state.EnterpriseLocation == nil || state.WorldRuntime == nil ||
			state.WorldRuntime.Locations == nil || state.WorldRuntime.ControlGrants == nil {
			return nil, fmt.Errorf("city F7.7 canonical state requires spatial, land, development, enterprise location, actor location, and control grant state")
		}
		state.WorldRuntime.PortalStates = nil
		state.WorldRuntime.NavigationProfile = nil
		state.WorldRuntime.NavigationIntents = nil
		state.PublicServices = nil
		return json.Marshal(state)
	case CitySimulationVersionF7V5:
		if state.Spatial == nil || state.Land == nil || state.Development == nil ||
			state.EnterpriseLocation == nil || state.WorldRuntime == nil {
			return nil, fmt.Errorf("city F7.6 canonical state requires spatial, land, development, enterprise location, and world runtime state")
		}
		state.WorldRuntime.Locations = nil
		state.WorldRuntime.ControlGrants = nil
		state.WorldRuntime.PortalStates = nil
		state.WorldRuntime.NavigationProfile = nil
		state.WorldRuntime.NavigationIntents = nil
		state.PublicServices = nil
		return json.Marshal(state)
	case CitySimulationVersionF7V4:
		if state.Spatial == nil || state.Land == nil || state.Development == nil || state.EnterpriseLocation == nil {
			return nil, fmt.Errorf("city F7.5 canonical state requires spatial, land, development, and enterprise location state")
		}
		state.WorldRuntime = nil
		state.PublicServices = nil
		return json.Marshal(state)
	case CitySimulationVersionF7V3:
		if state.Spatial == nil || state.Land == nil || state.Development == nil {
			return nil, fmt.Errorf("city F7.4 canonical state requires spatial, land, and development state")
		}
		state.EnterpriseLocation = nil
		state.WorldRuntime = nil
		state.PublicServices = nil
		return json.Marshal(state)
	case CitySimulationVersionF7V2:
		if state.Spatial == nil || state.Land == nil {
			return nil, fmt.Errorf("city F7.3 canonical state requires spatial and land state")
		}
		state.Development = nil
		state.EnterpriseLocation = nil
		state.WorldRuntime = nil
		state.PublicServices = nil
		return json.Marshal(state)
	case CitySimulationVersionF7:
		if state.Spatial == nil {
			return nil, fmt.Errorf("city F7 canonical state requires spatial state")
		}
		state.Land = nil
		state.Development = nil
		state.EnterpriseLocation = nil
		state.WorldRuntime = nil
		state.PublicServices = nil
		return json.Marshal(state)
	case CitySimulationVersionF6V3:
		state.Spatial = nil
		state.Land = nil
		state.Development = nil
		state.EnterpriseLocation = nil
		state.WorldRuntime = nil
		state.PublicServices = nil
		return json.Marshal(state)
	case CitySimulationVersionRealtimeV1:
		if state.Realtime == nil || state.Realtime.TemporalEngineVersion != CitySimulationVersionRealtimeV1 {
			return nil, fmt.Errorf("city realtime canonical state requires temporal state")
		}
		if err := validateCityRealtimeHashState(state.Realtime); err != nil {
			return nil, fmt.Errorf("city realtime canonical state has invalid temporal state: %w", err)
		}
		if state.RealtimeSpatial != nil || state.RealtimeActors != nil || state.RealtimeAgents != nil {
			return nil, fmt.Errorf("city realtime v1 canonical state must not contain static worldgen")
		}
		state.Spatial = nil
		state.Land = nil
		state.Development = nil
		state.EnterpriseLocation = nil
		state.WorldRuntime = nil
		state.PublicServices = nil
		state.Physical = cityPhysicalStateWithoutHouseholdUnits(state.Physical)
		return json.Marshal(state)
	case CitySimulationVersionRealtimeV2:
		if state.Realtime == nil || state.Realtime.TemporalEngineVersion != CitySimulationVersionRealtimeV2 {
			return nil, fmt.Errorf("city realtime v2 canonical state requires temporal state")
		}
		if err := validateCityRealtimeHashState(state.Realtime); err != nil {
			return nil, fmt.Errorf("city realtime v2 canonical temporal state is invalid: %w", err)
		}
		if state.RealtimeSpatial == nil {
			return nil, fmt.Errorf("city realtime v2 canonical state requires static worldgen")
		}
		if err := validateCityRealtimeSpatialHashState(state.RealtimeSpatial); err != nil {
			return nil, fmt.Errorf("city realtime v2 canonical static worldgen is invalid: %w", err)
		}
		if state.RealtimeActors == nil {
			return nil, fmt.Errorf("city realtime v2 canonical state requires actor runtime")
		}
		if err := validateCityRealtimeActorHashState(state.RealtimeActors); err != nil {
			return nil, fmt.Errorf("city realtime v2 canonical actor runtime is invalid: %w", err)
		}
		if state.RealtimeAgents != nil {
			if err := validateCityRealtimeAgentHashState(state.RealtimeAgents); err != nil {
				return nil, fmt.Errorf("city realtime v2 canonical agent runtime is invalid: %w", err)
			}
		}
		// Rule/Case response state is optional because pre-1.4 worlds must keep
		// their historical canonical shape. When the genesis-pinned adapter is
		// present, validate it before hashing so a malformed acknowledgement
		// chain can never be silently canonicalized.
		if state.RealtimeCharacterCases != nil {
			if err := validateCityRealtimeCharacterCaseResponseHashState(state.RealtimeCharacterCases); err != nil {
				return nil, fmt.Errorf("city realtime v2 canonical character case-response state is invalid: %w", err)
			}
		}
		if state.RealtimeCharacterCaseReviews != nil {
			if err := validateCityRealtimeCharacterCaseReviewHashState(state.RealtimeCharacterCaseReviews); err != nil {
				return nil, fmt.Errorf("city realtime v2 canonical character case-review state is invalid: %w", err)
			}
		}
		if state.RealtimeCharacterCaseReports != nil {
			if err := validateCityRealtimeCharacterCaseReportHashState(state.RealtimeCharacterCaseReports); err != nil {
				return nil, fmt.Errorf("city realtime v2 canonical character case-report state is invalid: %w", err)
			}
		}
		if state.RealtimeCharacterCaseIntakes != nil {
			if err := validateCityRealtimeCharacterCaseIntakeHashState(state.RealtimeCharacterCaseIntakes); err != nil {
				return nil, fmt.Errorf("city realtime v2 canonical character case-intake state is invalid: %w", err)
			}
		}
		if state.RealtimeCharacterCaseEvidence != nil {
			if err := validateCityRealtimeCharacterCaseEvidenceHashState(state.RealtimeCharacterCaseEvidence); err != nil {
				return nil, fmt.Errorf("city realtime v2 canonical character case-evidence state is invalid: %w", err)
			}
		}
		if state.RealtimeCharacterCaseEvidenceAssignments != nil {
			if err := validateCityRealtimeCharacterCaseEvidenceAssignmentHashState(state.RealtimeCharacterCaseEvidenceAssignments); err != nil {
				return nil, fmt.Errorf("city realtime v2 canonical character case-evidence assignment state is invalid: %w", err)
			}
		}
		if state.RealtimeCharacterCaseProcedureDispatches != nil {
			if err := validateCityRealtimeCharacterCaseProcedureDispatchHashState(state.RealtimeCharacterCaseProcedureDispatches); err != nil {
				return nil, fmt.Errorf("city realtime v2 canonical character case-procedure dispatch state is invalid: %w", err)
			}
		}
		if state.RealtimeCharacterTasks != nil {
			if err := validateCityRealtimeCharacterTaskHashState(state.RealtimeCharacterTasks); err != nil {
				return nil, fmt.Errorf("city realtime v2 canonical character task state is invalid: %w", err)
			}
		}
		if state.RealtimeCharacterNavigationPlans != nil {
			if err := validateCityRealtimeCharacterNavigationPlanHashState(state.RealtimeCharacterNavigationPlans); err != nil {
				return nil, fmt.Errorf("city realtime v2 canonical character navigation plan state is invalid: %w", err)
			}
		}
		if state.RealtimeCharacterTrafficReservations != nil {
			if err := validateCityRealtimeCharacterTrafficReservationHashState(state.RealtimeCharacterTrafficReservations); err != nil {
				return nil, fmt.Errorf("city realtime v2 canonical character traffic reservation state is invalid: %w", err)
			}
		}
		if state.RealtimeCharacterSocial != nil {
			if err := validateCityRealtimeCharacterSocialHashState(state.RealtimeCharacterSocial); err != nil {
				return nil, fmt.Errorf("city realtime v2 canonical character social state is invalid: %w", err)
			}
		}
		state.Spatial = nil
		state.Land = nil
		state.Development = nil
		state.EnterpriseLocation = nil
		state.WorldRuntime = nil
		state.PublicServices = nil
		state.Physical = cityPhysicalStateWithoutHouseholdUnits(state.Physical)
		return json.Marshal(state)
	case CitySimulationVersionF6, CitySimulationVersionF6V2:
		state.Spatial = nil
		state.Land = nil
		state.Development = nil
		state.EnterpriseLocation = nil
		state.WorldRuntime = nil
		state.PublicServices = nil
		state.Physical = cityPhysicalStateWithoutHouseholdUnits(state.Physical)
		return json.Marshal(state)
	case CitySimulationVersionF5:
		return json.Marshal(cityHashStateF5{
			Name: state.Name, Status: state.Status,
			SimulationVersion: state.SimulationVersion, Seed: state.Seed,
			CurrentTick: state.CurrentTick, SimulatedAt: state.SimulatedAt,
			SpeedMilli: state.SpeedMilli, Timezone: state.Timezone,
			Settings: state.Settings, MonetaryUnits: state.MonetaryUnits,
			AccountTemplates: state.AccountTemplates, Entities: state.Entities,
			Accounts: state.Accounts,
			Physical: cityPhysicalStateWithoutHouseholdUnits(state.Physical), Markets: state.Markets,
		})
	default:
		return nil, fmt.Errorf("unsupported city canonical state version %q", state.SimulationVersion)
	}
}

func cityEngineSupportsPopulationMigration(version string) bool {
	return version == CitySimulationVersionF6V2 || version == CitySimulationVersionF6V3 ||
		version == CitySimulationVersionF7 || version == CitySimulationVersionF7V2 ||
		version == CitySimulationVersionF7V3 || version == CitySimulationVersionF7V4 ||
		version == CitySimulationVersionF7V5 || version == CitySimulationVersionF7V6 ||
		version == CitySimulationVersionF7V7 || version == CitySimulationVersionF7V8 ||
		version == CitySimulationVersionF7V9 || version == CitySimulationVersionF8 ||
		version == CitySimulationVersionF8V2 || version == CitySimulationVersionF8V3
}

func cityEngineSupportsHouseholdLifecycle(version string) bool {
	return version == CitySimulationVersionF6V3 || version == CitySimulationVersionF7 ||
		version == CitySimulationVersionF7V2 || version == CitySimulationVersionF7V3 ||
		version == CitySimulationVersionF7V4 || version == CitySimulationVersionF7V5 ||
		version == CitySimulationVersionF7V6 || version == CitySimulationVersionF7V7 ||
		version == CitySimulationVersionF7V8 || version == CitySimulationVersionF7V9 ||
		version == CitySimulationVersionF8 || version == CitySimulationVersionF8V2 ||
		version == CitySimulationVersionF8V3
}

func cityEngineSupportsSpatial(version string) bool {
	return version == CitySimulationVersionF7 || version == CitySimulationVersionF7V2 ||
		version == CitySimulationVersionF7V3 || version == CitySimulationVersionF7V4 ||
		version == CitySimulationVersionF7V5 || version == CitySimulationVersionF7V6 ||
		version == CitySimulationVersionF7V7 || version == CitySimulationVersionF7V8 ||
		version == CitySimulationVersionF7V9 || version == CitySimulationVersionF8 ||
		version == CitySimulationVersionF8V2 || version == CitySimulationVersionF8V3
}

func cityEngineSupportsLand(version string) bool {
	return version == CitySimulationVersionF7V2 || version == CitySimulationVersionF7V3 ||
		version == CitySimulationVersionF7V4 || version == CitySimulationVersionF7V5 ||
		version == CitySimulationVersionF7V6 || version == CitySimulationVersionF7V7 ||
		version == CitySimulationVersionF7V8 || version == CitySimulationVersionF7V9 ||
		version == CitySimulationVersionF8 || version == CitySimulationVersionF8V2 ||
		version == CitySimulationVersionF8V3
}

func cityEngineSupportsDevelopment(version string) bool {
	return version == CitySimulationVersionF7V3 || version == CitySimulationVersionF7V4 ||
		version == CitySimulationVersionF7V5 || version == CitySimulationVersionF7V6 ||
		version == CitySimulationVersionF7V7 || version == CitySimulationVersionF7V8 ||
		version == CitySimulationVersionF7V9 || version == CitySimulationVersionF8 ||
		version == CitySimulationVersionF8V2 || version == CitySimulationVersionF8V3
}

func cityEngineSupportsEnterpriseLocation(version string) bool {
	return version == CitySimulationVersionF7V4 || version == CitySimulationVersionF7V5 ||
		version == CitySimulationVersionF7V6 || version == CitySimulationVersionF7V7 ||
		version == CitySimulationVersionF7V8 || version == CitySimulationVersionF7V9 ||
		version == CitySimulationVersionF8 || version == CitySimulationVersionF8V2 ||
		version == CitySimulationVersionF8V3
}

func cityEngineSupportsWorldRuntime(version string) bool {
	return version == CitySimulationVersionF7V5 || version == CitySimulationVersionF7V6 ||
		version == CitySimulationVersionF7V7 || version == CitySimulationVersionF7V8 ||
		version == CitySimulationVersionF7V9 || version == CitySimulationVersionF8 ||
		version == CitySimulationVersionF8V2 || version == CitySimulationVersionF8V3
}

func cityEngineSupportsWorldActorSpatialControl(version string) bool {
	return version == CitySimulationVersionF7V6 || version == CitySimulationVersionF7V7 ||
		version == CitySimulationVersionF7V8 || version == CitySimulationVersionF7V9 ||
		version == CitySimulationVersionF8 || version == CitySimulationVersionF8V2 ||
		version == CitySimulationVersionF8V3
}

func cityEngineSupportsWorldActorNavigation(version string) bool {
	return version == CitySimulationVersionF7V7 || version == CitySimulationVersionF7V8 ||
		version == CitySimulationVersionF7V9 || version == CitySimulationVersionF8 ||
		version == CitySimulationVersionF8V2 || version == CitySimulationVersionF8V3
}

func cityEngineSupportsWorldPortalAccess(version string) bool {
	return version == CitySimulationVersionF7V8 || version == CitySimulationVersionF7V9 ||
		version == CitySimulationVersionF8 || version == CitySimulationVersionF8V2 ||
		version == CitySimulationVersionF8V3
}

func cityEngineSupportsWorldNavigationIntents(version string) bool {
	return version == CitySimulationVersionF7V9 || version == CitySimulationVersionF8 ||
		version == CitySimulationVersionF8V2 || version == CitySimulationVersionF8V3
}

func cityEngineSupportsPublicServices(version string) bool {
	return version == CitySimulationVersionF8 || version == CitySimulationVersionF8V2 ||
		version == CitySimulationVersionF8V3
}

func cityEngineSupportsFacilityLifecycle(version string) bool {
	return version == CitySimulationVersionF8V2 || version == CitySimulationVersionF8V3
}

func cityEngineSupportsPhysicalNetworks(version string) bool {
	return version == CitySimulationVersionF8V3
}

func cityEngineSupportsOpenWorld(version string) bool {
	return version == CitySimulationVersionOpenWorld || version == CitySimulationVersionOpenWorldV2 ||
		version == CitySimulationVersionOpenWorldV3 || version == CitySimulationVersionOpenWorldV4 ||
		version == CitySimulationVersionOpenWorldV5 || version == CitySimulationVersionOpenWorldV6 ||
		version == CitySimulationVersionOpenWorldV7 || version == CitySimulationVersionOpenWorldV8 ||
		version == CitySimulationVersionOpenWorldV9 || version == CitySimulationVersionOpenWorldV10 ||
		version == CitySimulationVersionOpenWorldV11 || version == CitySimulationVersionOpenWorldV12 ||
		version == CitySimulationVersionOpenWorldV13 || version == CitySimulationVersionOpenWorldV14 ||
		version == CitySimulationVersionOpenWorldV15 || version == CitySimulationVersionOpenWorldV16 ||
		version == CitySimulationVersionOpenWorldV17 || version == CitySimulationVersionOpenWorldV18 ||
		version == CitySimulationVersionOpenWorldV19 || version == CitySimulationVersionOpenWorldV20 ||
		version == CitySimulationVersionOpenWorldV21 || version == CitySimulationVersionOpenWorldV22 ||
		version == CitySimulationVersionOpenWorldV23 || version == CitySimulationVersionOpenWorldV24
}

func cityEngineIsRealtime(version string) bool {
	return version == CitySimulationVersionRealtimeV1 || version == CitySimulationVersionRealtimeV2
}

// cityEngineSupportsRealtimeStaticWorldgen is intentionally distinct from
// cityEngineSupportsOpenWorld. The latter identifies legacy tick-driven
// worldgen and runtime capabilities; admitting a realtime world there would
// silently route it through V24 mutation code.
func cityEngineSupportsRealtimeStaticWorldgen(version string) bool {
	return version == CitySimulationVersionRealtimeV2
}

func cityOpenWorldEngineGeneration(version string) int {
	switch version {
	case CitySimulationVersionOpenWorld:
		return 1
	case CitySimulationVersionOpenWorldV2:
		return 2
	case CitySimulationVersionOpenWorldV3:
		return 3
	case CitySimulationVersionOpenWorldV4:
		return 4
	case CitySimulationVersionOpenWorldV5:
		return 5
	case CitySimulationVersionOpenWorldV6:
		return 6
	case CitySimulationVersionOpenWorldV7:
		return 7
	case CitySimulationVersionOpenWorldV8:
		return 8
	case CitySimulationVersionOpenWorldV9:
		return 9
	case CitySimulationVersionOpenWorldV10:
		return 10
	case CitySimulationVersionOpenWorldV11:
		return 11
	case CitySimulationVersionOpenWorldV12:
		return 12
	case CitySimulationVersionOpenWorldV13:
		return 13
	case CitySimulationVersionOpenWorldV14:
		return 14
	case CitySimulationVersionOpenWorldV15:
		return 15
	case CitySimulationVersionOpenWorldV16:
		return 16
	case CitySimulationVersionOpenWorldV17:
		return 17
	case CitySimulationVersionOpenWorldV18:
		return 18
	case CitySimulationVersionOpenWorldV19:
		return 19
	case CitySimulationVersionOpenWorldV20:
		return 20
	case CitySimulationVersionOpenWorldV21:
		return 21
	case CitySimulationVersionOpenWorldV22:
		return 22
	case CitySimulationVersionOpenWorldV23:
		return 23
	case CitySimulationVersionOpenWorldV24:
		return 24
	default:
		return 0
	}
}

func cityEngineSupportsOpenWorldGeneration(version string, generation int) bool {
	return cityOpenWorldEngineGeneration(version) >= generation
}

func cityEngineSupportsOpenWorldMaterialization(version string) bool {
	return cityEngineSupportsOpenWorldGeneration(version, 2)
}

func cityEngineSupportsOpenWorldVerticalTopology(version string) bool {
	return cityEngineSupportsOpenWorldGeneration(version, 3)
}

func cityEngineSupportsOpenWorldRuntime(version string) bool {
	return cityEngineSupportsOpenWorldGeneration(version, 4)
}

func cityEngineSupportsOpenWorldSocialRuntime(version string) bool {
	return cityEngineSupportsOpenWorldGeneration(version, 5)
}

func cityEngineSupportsWorldVersionVector(version string) bool {
	return cityEngineSupportsOpenWorldGeneration(version, 6)
}

func cityEngineSupportsOpenWorldServiceCoordination(version string) bool {
	return cityEngineSupportsOpenWorldGeneration(version, 7)
}

func cityEngineSupportsOpenWorldImpactBridge(version string) bool {
	return cityEngineSupportsOpenWorldGeneration(version, 8)
}

func cityEngineSupportsOpenWorldMobility(version string) bool {
	return cityEngineSupportsOpenWorldGeneration(version, 9)
}

func cityEngineSupportsOpenWorldArrivalBridge(version string) bool {
	return cityEngineSupportsOpenWorldGeneration(version, 10)
}

func cityEngineSupportsOpenWorldMobilityOD(version string) bool {
	return cityEngineSupportsOpenWorldGeneration(version, 11)
}

func cityEngineSupportsOpenWorldCommuteBindings(version string) bool {
	return cityEngineSupportsOpenWorldGeneration(version, 12)
}

func cityEngineSupportsOpenWorldCommuteSources(version string) bool {
	return cityEngineSupportsOpenWorldGeneration(version, 13)
}

// cityEngineRunsOpenWorldV13CommuteSources separates V13's mutable source
// projection from its historical availability in V14 canonical state. V14
// retains those rows as evidence but never lets the legacy generator create
// successor traffic.
func cityEngineRunsOpenWorldV13CommuteSources(version string) bool {
	return version == CitySimulationVersionOpenWorldV13
}

func cityEngineSupportsOpenWorldCommuteLifecycle(version string) bool {
	return cityEngineSupportsOpenWorldGeneration(version, 14)
}

// cityEngineSupportsOpenWorldSupplyChain keeps F10.0's order and settlement
// evidence isolated from older social-runtime projections.
func cityEngineSupportsOpenWorldSupplyChain(version string) bool {
	return cityEngineSupportsOpenWorldGeneration(version, 15)
}

// cityEngineSupportsOpenWorldEnterpriseFreight keeps V16's source adapter
// isolated from V15 settlement and V9 route truth. Later transport contracts
// must explicitly supersede it rather than silently widening its authority.
func cityEngineSupportsOpenWorldEnterpriseFreight(version string) bool {
	return cityEngineSupportsOpenWorldGeneration(version, 16)
}

// cityEngineSupportsOpenWorldEnterpriseFreightReceipts is V17's custody and
// receipt-gate successor. It consumes V16 evidence but does not widen V16's
// non-settling transport contract for older worlds.
func cityEngineSupportsOpenWorldEnterpriseFreightReceipts(version string) bool {
	return cityEngineSupportsOpenWorldGeneration(version, 17)
}

// cityEngineSupportsOpenWorldFreightBatches adds V18's overflow
// planner without silently widening freight semantics for older worlds.
func cityEngineSupportsOpenWorldFreightBatches(version string) bool {
	return cityEngineSupportsOpenWorldGeneration(version, 18)
}

// cityEngineSupportsOpenWorldSpatialNetwork keeps V19's static transport
// identity layer isolated from the dynamic V9 scheduler. Later F9.3 revisions
// must explicitly add mutable infrastructure rather than changing V19 data.
func cityEngineSupportsOpenWorldSpatialNetwork(version string) bool {
	return cityEngineSupportsOpenWorldGeneration(version, 19)
}

// cityEngineSupportsOpenWorldInfrastructure makes V20's mutable asset
// lifecycle explicit without suggesting that dynamic capacity already affects
// the V9 scheduler.
func cityEngineSupportsOpenWorldInfrastructure(version string) bool {
	return cityEngineSupportsOpenWorldGeneration(version, 20)
}

// cityEngineSupportsOpenWorldEffectiveCapacity is V21's explicit bridge from
// V20 corridor-segment state to future V9 route admission. Older worlds keep
// their frozen V9 capacity semantics even when served by newer binaries.
func cityEngineSupportsOpenWorldEffectiveCapacity(version string) bool {
	return cityEngineSupportsOpenWorldGeneration(version, 21)
}

// cityEngineSupportsOpenWorldFreightSettlements is F10.3's future-only
// partial settlement layer. It never changes delivery semantics for a world
// whose sealed engine version predates V22.
func cityEngineSupportsOpenWorldFreightSettlements(version string) bool {
	return cityEngineSupportsOpenWorldGeneration(version, 22)
}

// cityEngineSupportsOpenWorldCarrierRecovery is the V23 successor that adds
// a manual reserve and explicit claim closure without changing V22 receipt or
// settlement semantics for worlds sealed at an earlier generation.
func cityEngineSupportsOpenWorldCarrierRecovery(version string) bool {
	return cityEngineSupportsOpenWorldGeneration(version, 23)
}

// cityEngineSupportsOpenWorldCarrierCommerce is V24's narrow freight-fee
// contract. Earlier worlds retain their sealed delivery and recovery truth;
// a newer binary never synthesizes a commerce projection for them.
func cityEngineSupportsOpenWorldCarrierCommerce(version string) bool {
	return cityEngineSupportsOpenWorldGeneration(version, 24)
}

// F7.3 land generation is a frozen compatibility domain. F7.4 layers posted
// adjustments on top and must not rebind the immutable baseline proof.
func cityLandGeneratorVersion(version string) (string, error) {
	if !cityEngineSupportsLand(version) {
		return "", fmt.Errorf("city engine %q does not support land generation", version)
	}
	return CitySimulationVersionF7V2, nil
}

// F7.1 spatial generation is a frozen compatibility domain. Newer engines may
// consume its immutable Overmap and Chunk facts, but must not silently rebind
// their proofs to a newer simulation version.
func citySpatialGeneratorVersion(version string) (string, error) {
	if !cityEngineSupportsSpatial(version) {
		return "", fmt.Errorf("city engine %q does not support spatial generation", version)
	}
	return CitySimulationVersionF7, nil
}

func cityPhysicalStateWithoutHouseholdUnits(state cityPhysicalHashState) cityPhysicalHashState {
	state.HouseholdCohorts = append([]cityHashHouseholdCohort(nil), state.HouseholdCohorts...)
	for index := range state.HouseholdCohorts {
		state.HouseholdCohorts[index].HouseholdUnits = 0
	}
	return state
}
