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

	CurrentCitySimulationVersion = CitySimulationVersionF7V5

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
	cityEngineStageSpatial            cityEngineStage = "spatial"
	cityEngineStageDevelopment        cityEngineStage = "development"
	cityEngineStageEnterpriseLocation cityEngineStage = "enterprise_location"
	cityEngineStageWorldRuntime       cityEngineStage = "world_runtime"
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
	case CitySimulationVersionF7V5:
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
			cityEngineStageCalendarDemography, cityEngineStageSpatial,
			cityEngineStageDevelopment, cityEngineStageEnterpriseLocation,
			cityEngineStageWorldRuntime, cityEngineStageMarkets:
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
	case isCitySpatialCommand(commandType):
		return cityEngineStageSpatial, true
	case isCityDevelopmentCommand(commandType):
		return cityEngineStageDevelopment, true
	case isCityEnterpriseLocationCommand(commandType):
		return cityEngineStageEnterpriseLocation, true
	case isWorldRuntimeCommand(commandType):
		return cityEngineStageWorldRuntime, true
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
		(fromVersion == CitySimulationVersionF7V4 && toVersion == CitySimulationVersionF7V5)
}

func marshalCanonicalCityState(state cityHashState) ([]byte, error) {
	switch state.SimulationVersion {
	case CitySimulationVersionF7V5:
		if state.Spatial == nil || state.Land == nil || state.Development == nil ||
			state.EnterpriseLocation == nil || state.WorldRuntime == nil {
			return nil, fmt.Errorf("city F7.6 canonical state requires spatial, land, development, enterprise location, and world runtime state")
		}
		return json.Marshal(state)
	case CitySimulationVersionF7V4:
		if state.Spatial == nil || state.Land == nil || state.Development == nil || state.EnterpriseLocation == nil {
			return nil, fmt.Errorf("city F7.5 canonical state requires spatial, land, development, and enterprise location state")
		}
		state.WorldRuntime = nil
		return json.Marshal(state)
	case CitySimulationVersionF7V3:
		if state.Spatial == nil || state.Land == nil || state.Development == nil {
			return nil, fmt.Errorf("city F7.4 canonical state requires spatial, land, and development state")
		}
		state.EnterpriseLocation = nil
		state.WorldRuntime = nil
		return json.Marshal(state)
	case CitySimulationVersionF7V2:
		if state.Spatial == nil || state.Land == nil {
			return nil, fmt.Errorf("city F7.3 canonical state requires spatial and land state")
		}
		state.Development = nil
		state.EnterpriseLocation = nil
		state.WorldRuntime = nil
		return json.Marshal(state)
	case CitySimulationVersionF7:
		if state.Spatial == nil {
			return nil, fmt.Errorf("city F7 canonical state requires spatial state")
		}
		state.Land = nil
		state.Development = nil
		state.EnterpriseLocation = nil
		state.WorldRuntime = nil
		return json.Marshal(state)
	case CitySimulationVersionF6V3:
		state.Spatial = nil
		state.Land = nil
		state.Development = nil
		state.EnterpriseLocation = nil
		state.WorldRuntime = nil
		return json.Marshal(state)
	case CitySimulationVersionF6, CitySimulationVersionF6V2:
		state.Spatial = nil
		state.Land = nil
		state.Development = nil
		state.EnterpriseLocation = nil
		state.WorldRuntime = nil
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
		version == CitySimulationVersionF7V5
}

func cityEngineSupportsHouseholdLifecycle(version string) bool {
	return version == CitySimulationVersionF6V3 || version == CitySimulationVersionF7 ||
		version == CitySimulationVersionF7V2 || version == CitySimulationVersionF7V3 ||
		version == CitySimulationVersionF7V4 || version == CitySimulationVersionF7V5
}

func cityEngineSupportsSpatial(version string) bool {
	return version == CitySimulationVersionF7 || version == CitySimulationVersionF7V2 ||
		version == CitySimulationVersionF7V3 || version == CitySimulationVersionF7V4 ||
		version == CitySimulationVersionF7V5
}

func cityEngineSupportsLand(version string) bool {
	return version == CitySimulationVersionF7V2 || version == CitySimulationVersionF7V3 ||
		version == CitySimulationVersionF7V4 || version == CitySimulationVersionF7V5
}

func cityEngineSupportsDevelopment(version string) bool {
	return version == CitySimulationVersionF7V3 || version == CitySimulationVersionF7V4 ||
		version == CitySimulationVersionF7V5
}

func cityEngineSupportsEnterpriseLocation(version string) bool {
	return version == CitySimulationVersionF7V4 || version == CitySimulationVersionF7V5
}

func cityEngineSupportsWorldRuntime(version string) bool {
	return version == CitySimulationVersionF7V5
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
