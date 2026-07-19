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

	CurrentCitySimulationVersion = CitySimulationVersionF8V3

	// CitySimulationVersionV1 remains the public compatibility name used by
	// existing callers. New engine code should use the explicit F5/F6 names.
	CitySimulationVersionV1 = CurrentCitySimulationVersion
)

type cityEngineStage string

const (
	cityEngineStageControl            cityEngineStage = "control"
	cityEngineStageLedger             cityEngineStage = "ledger"
	cityEngineStageResources          cityEngineStage = "resources"
	cityEngineStageCalendarDemography cityEngineStage = "calendar_demography"
	cityEngineStageOpenWorld          cityEngineStage = "open_world"
	cityEngineStageSpatial            cityEngineStage = "spatial"
	cityEngineStageDevelopment        cityEngineStage = "development"
	cityEngineStageEnterpriseLocation cityEngineStage = "enterprise_location"
	cityEngineStageWorldRuntime       cityEngineStage = "world_runtime"
	cityEngineStagePublicServices     cityEngineStage = "public_services"
	cityEngineStageMarkets            cityEngineStage = "markets"
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
	case CitySimulationVersionOpenWorld:
		engine = cityEngineDefinition{version: version, stages: []cityEngineStage{
			cityEngineStageControl,
			cityEngineStageLedger,
			cityEngineStageResources,
			cityEngineStageCalendarDemography,
			cityEngineStageMarkets,
		}}
	case CitySimulationVersionOpenWorldV2, CitySimulationVersionOpenWorldV3,
		CitySimulationVersionOpenWorldV4, CitySimulationVersionOpenWorldV5:
		engine = cityEngineDefinition{version: version, stages: []cityEngineStage{
			cityEngineStageControl,
			cityEngineStageLedger,
			cityEngineStageResources,
			cityEngineStageCalendarDemography,
			cityEngineStageOpenWorld,
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
			cityEngineStageCalendarDemography, cityEngineStageOpenWorld, cityEngineStageSpatial,
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
	if isCityOpenWorldSocialRuntimeCommand(commandType) && !cityEngineSupportsOpenWorldSocialRuntime(engine.version) {
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
		(fromVersion == CitySimulationVersionF8V2 && toVersion == CitySimulationVersionF8V3)
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
	case CitySimulationVersionOpenWorldV4:
		if state.OpenWorld == nil || state.OpenWorldRuntime == nil ||
			state.OpenWorldRuntime.Profile.RuntimeID != cityOpenWorldRuntimeID ||
			state.OpenWorldRuntime.Social != nil {
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
			state.OpenWorldRuntime.Social == nil {
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
		version == CitySimulationVersionOpenWorldV5
}

func cityEngineSupportsOpenWorldMaterialization(version string) bool {
	return version == CitySimulationVersionOpenWorldV2 || version == CitySimulationVersionOpenWorldV3 ||
		version == CitySimulationVersionOpenWorldV4 || version == CitySimulationVersionOpenWorldV5
}

func cityEngineSupportsOpenWorldVerticalTopology(version string) bool {
	return version == CitySimulationVersionOpenWorldV3 || version == CitySimulationVersionOpenWorldV4 ||
		version == CitySimulationVersionOpenWorldV5
}

func cityEngineSupportsOpenWorldRuntime(version string) bool {
	return version == CitySimulationVersionOpenWorldV4 || version == CitySimulationVersionOpenWorldV5
}

func cityEngineSupportsOpenWorldSocialRuntime(version string) bool {
	return version == CitySimulationVersionOpenWorldV5
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
