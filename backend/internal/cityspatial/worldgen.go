package cityspatial

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// The world-generator is intentionally independent from the frozen F7
// spatial and land baselines. It is a deterministic, versioned planning
// pipeline that can be materialized by a later simulation version without
// rewriting existing worlds.
const (
	DefaultWorldgenID      = "sub2api-openworld-citygen"
	DefaultWorldgenVersion = "1.3.0"
	// OpenWorldRegionGeneratorVersion fixes the planning boundary to one
	// persisted region.  A V2 world never recomputes a smaller per-sector
	// plan, because that would make city placement and roads depend on which
	// sector happened to be requested first.
	OpenWorldRegionGeneratorVersion = "1.4.0"
	// OpenWorldVerticalGeneratorVersion keeps the exact same region planner as
	// 1.4.0 but adds sealed multi-floor interiors and topology portals.  It is
	// intentionally a new version: existing V2 worlds must never receive a
	// different sector content hash after creation.
	OpenWorldVerticalGeneratorVersion = "1.5.0"
	DefaultWorldgenProfileID          = "sub2api-temperate-openworld"
	DefaultWorldgenProfileVersion     = "1.2.0"
	maximumWorldgenAxis               = 32
	maximumWorldgenCities             = 8
	maximumWorldgenLots               = 512
)

var ErrInvalidWorldgenInput = errors.New("invalid city world-generation input")

type WorldgenBounds struct {
	MinimumChunkX int64 `json:"minimum_chunk_x"`
	MaximumChunkX int64 `json:"maximum_chunk_x"`
	MinimumChunkY int64 `json:"minimum_chunk_y"`
	MaximumChunkY int64 `json:"maximum_chunk_y"`
	Z             int32 `json:"z"`
}

type WorldgenPoint struct {
	X int64 `json:"x"`
	Y int64 `json:"y"`
	Z int32 `json:"z"`
}

type WorldgenRectangle struct {
	MinimumX int64 `json:"minimum_x"`
	MaximumX int64 `json:"maximum_x"`
	MinimumY int64 `json:"minimum_y"`
	MaximumY int64 `json:"maximum_y"`
	Z        int32 `json:"z"`
}

type WorldgenTerrainRule struct {
	Code                  string `json:"code"`
	GroundDefinitionID    string `json:"ground_definition_id"`
	Priority              int    `json:"priority"`
	MinimumElevationMilli int    `json:"minimum_elevation_milli"`
	MaximumElevationMilli int    `json:"maximum_elevation_milli"`
	MinimumMoistureMilli  int    `json:"minimum_moisture_milli"`
	MaximumMoistureMilli  int    `json:"maximum_moisture_milli"`
}

// WorldgenCityPlacementRule keeps settlement siting data-driven.  Excluded
// biome codes let a new regional profile keep cities off water, marshes, or
// any custom hazardous terrain without a generator-code fork.  The neighbor
// requirement makes a single isolated dry tile an unsuitable city seed.
type WorldgenCityPlacementRule struct {
	ExcludedBiomeCodes    []string `json:"excluded_biome_codes,omitempty"`
	MinimumElevationMilli int      `json:"minimum_elevation_milli"`
	MaximumMoistureMilli  int      `json:"maximum_moisture_milli"`
	MinimumDryNeighbors   int      `json:"minimum_dry_neighbors"`
}

// WorldgenLandUseWeight describes a district-local mix.  It deliberately
// remains separate from building archetypes: a district chooses its demand
// mix first, then an archetype is selected for the resulting land use.
type WorldgenLandUseWeight struct {
	PrimaryUse LandUse `json:"primary_use"`
	Weight     int     `json:"weight"`
}

// WorldgenDistrictRule is a radial city band expressed in thousandths of a
// city's radius.  Ordered, gap-free bands reproduce the useful C:DDA idea of
// changing building selection with city-center distance while keeping the
// weights fully profile-defined.
type WorldgenDistrictRule struct {
	Code                 string                  `json:"code"`
	MinimumDistanceMilli int                     `json:"minimum_distance_milli"`
	MaximumDistanceMilli int                     `json:"maximum_distance_milli"`
	UseWeights           []WorldgenLandUseWeight `json:"use_weights"`
}

type WorldgenBuildingArchetype struct {
	Code          string  `json:"code"`
	PrimaryUse    LandUse `json:"primary_use"`
	Weight        int     `json:"weight"`
	MinimumWidth  int     `json:"minimum_width"`
	MaximumWidth  int     `json:"maximum_width"`
	MinimumDepth  int     `json:"minimum_depth"`
	MaximumDepth  int     `json:"maximum_depth"`
	MinimumFloors int32   `json:"minimum_floors"`
	MaximumFloors int32   `json:"maximum_floors"`
	LayoutStyle   string  `json:"layout_style"`
	// DistrictCodes is optional.  An empty value means the archetype is
	// eligible in every district compatible with PrimaryUse.
	DistrictCodes []string `json:"district_codes,omitempty"`
}

type WorldgenProfile struct {
	ID                 string                      `json:"id"`
	Version            string                      `json:"version"`
	Name               string                      `json:"name"`
	CityCount          int                         `json:"city_count"`
	MinimumCityRadius  int                         `json:"minimum_city_radius_chunks"`
	MaximumCityRadius  int                         `json:"maximum_city_radius_chunks"`
	ArterialRoadWidth  int                         `json:"arterial_road_width"`
	LocalStreetWidth   int                         `json:"local_street_width"`
	MinimumLotFrontage int                         `json:"minimum_lot_frontage"`
	MaximumLotFrontage int                         `json:"maximum_lot_frontage"`
	MinimumLotDepth    int                         `json:"minimum_lot_depth"`
	MaximumLotDepth    int                         `json:"maximum_lot_depth"`
	TerrainRules       []WorldgenTerrainRule       `json:"terrain_rules"`
	CityPlacement      WorldgenCityPlacementRule   `json:"city_placement"`
	DistrictRules      []WorldgenDistrictRule      `json:"district_rules"`
	BuildingArchetypes []WorldgenBuildingArchetype `json:"building_archetypes"`
	ContentHash        string                      `json:"content_hash,omitempty"`
}

type WorldgenBinding struct {
	SimulationVersion string `json:"simulation_version"`
	WorldSeed         int64  `json:"world_seed"`
	SpatialRootHash   string `json:"spatial_root_hash"`
	ProfileID         string `json:"profile_id"`
	ProfileVersion    string `json:"profile_version"`
	ProfileHash       string `json:"profile_hash"`
	GeneratorID       string `json:"generator_id"`
	GeneratorVersion  string `json:"generator_version"`
}

type GeneratedWorldgenTerrainPatch struct {
	ChunkX         int64  `json:"chunk_x"`
	ChunkY         int64  `json:"chunk_y"`
	Z              int32  `json:"z"`
	BiomeCode      string `json:"biome_code"`
	DefinitionID   string `json:"definition_id"`
	ElevationMilli int    `json:"elevation_milli"`
	MoistureMilli  int    `json:"moisture_milli"`
}

type GeneratedWorldgenCity struct {
	Code           string        `json:"code"`
	Center         WorldgenPoint `json:"center"`
	RadiusChunks   int           `json:"radius_chunks"`
	BiomeCode      string        `json:"biome_code"`
	ElevationMilli int           `json:"elevation_milli"`
	MoistureMilli  int           `json:"moisture_milli"`
	PlacementMode  string        `json:"placement_mode"`
}

type WorldgenRoadClass string

const (
	WorldgenRoadArterial WorldgenRoadClass = "arterial"
	WorldgenRoadLocal    WorldgenRoadClass = "local"
)

type GeneratedWorldgenRoad struct {
	Code     string            `json:"code"`
	CityCode string            `json:"city_code"`
	Class    WorldgenRoadClass `json:"class"`
	Width    int               `json:"width"`
	Points   []WorldgenPoint   `json:"points"`
}

type GeneratedWorldgenLot struct {
	Code              string            `json:"code"`
	CityCode          string            `json:"city_code"`
	DistrictCode      string            `json:"district_code"`
	PrimaryUse        LandUse           `json:"primary_use"`
	Bounds            WorldgenRectangle `json:"bounds"`
	FrontageRoadCode  string            `json:"frontage_road_code"`
	FrontageDirection string            `json:"frontage_direction"`
}

type GeneratedWorldgenBuilding struct {
	Code          string          `json:"code"`
	CityCode      string          `json:"city_code"`
	LotCode       string          `json:"lot_code"`
	PrimaryUse    LandUse         `json:"primary_use"`
	ArchetypeCode string          `json:"archetype_code"`
	LayoutStyle   string          `json:"layout_style"`
	FloorCount    int32           `json:"floor_count"`
	Entrance      WorldgenPoint   `json:"entrance"`
	Footprint     []WorldgenPoint `json:"footprint"`
}

type GeneratedWorldgenPlan struct {
	Binding      WorldgenBinding                 `json:"binding"`
	Profile      WorldgenProfile                 `json:"profile"`
	Bounds       WorldgenBounds                  `json:"bounds"`
	Terrain      []GeneratedWorldgenTerrainPatch `json:"terrain"`
	Cities       []GeneratedWorldgenCity         `json:"cities"`
	Roads        []GeneratedWorldgenRoad         `json:"roads"`
	Lots         []GeneratedWorldgenLot          `json:"lots"`
	Buildings    []GeneratedWorldgenBuilding     `json:"buildings"`
	BaselineHash string                          `json:"baseline_hash"`
}

// GeneratedWorldgenWindow is the query-safe projection of a whole plan. It
// preserves the global plan hash while returning only terrain and sites that
// affect the requested view. Road polylines are kept intact when they cross a
// view so callers can render a continuous network at map edges.
type GeneratedWorldgenWindow struct {
	GeneratorID      string                          `json:"generator_id"`
	GeneratorVersion string                          `json:"generator_version"`
	ProfileID        string                          `json:"profile_id"`
	ProfileVersion   string                          `json:"profile_version"`
	PlanHash         string                          `json:"plan_hash"`
	Bounds           WorldgenBounds                  `json:"bounds"`
	Terrain          []GeneratedWorldgenTerrainPatch `json:"terrain"`
	Cities           []GeneratedWorldgenCity         `json:"cities"`
	Roads            []GeneratedWorldgenRoad         `json:"roads"`
	Lots             []GeneratedWorldgenLot          `json:"lots"`
	Buildings        []GeneratedWorldgenBuilding     `json:"buildings"`
}

func DefaultWorldgenProfile() (*WorldgenProfile, error) {
	profile := WorldgenProfile{
		ID: DefaultWorldgenProfileID, Version: DefaultWorldgenProfileVersion,
		Name: "Temperate Open-world City", CityCount: 1,
		MinimumCityRadius: 3, MaximumCityRadius: 5,
		ArterialRoadWidth: 4, LocalStreetWidth: 2,
		// The V2 plan works in individual world cells. Give the default
		// profile enough parcel depth to generate C:DDA-scale interiors rather
		// than repeating the former six-cell F7 rooms.
		MinimumLotFrontage: 8, MaximumLotFrontage: 22,
		MinimumLotDepth: 8, MaximumLotDepth: 24,
		TerrainRules: []WorldgenTerrainRule{
			{Code: "water.deep", GroundDefinitionID: "terrain.deep_water", Priority: 10, MinimumElevationMilli: 0, MaximumElevationMilli: 120, MinimumMoistureMilli: 0, MaximumMoistureMilli: 1000},
			{Code: "water.shallow", GroundDefinitionID: "terrain.shallow_water", Priority: 20, MinimumElevationMilli: 0, MaximumElevationMilli: 190, MinimumMoistureMilli: 0, MaximumMoistureMilli: 1000},
			{Code: "wetland", GroundDefinitionID: "terrain.soil", Priority: 30, MinimumElevationMilli: 0, MaximumElevationMilli: 1000, MinimumMoistureMilli: 840, MaximumMoistureMilli: 1000},
			{Code: "woodland", GroundDefinitionID: "terrain.grass", Priority: 40, MinimumElevationMilli: 0, MaximumElevationMilli: 1000, MinimumMoistureMilli: 640, MaximumMoistureMilli: 1000},
			{Code: "meadow", GroundDefinitionID: "terrain.grass", Priority: 100, MinimumElevationMilli: 0, MaximumElevationMilli: 1000, MinimumMoistureMilli: 0, MaximumMoistureMilli: 1000},
		},
		CityPlacement: WorldgenCityPlacementRule{
			ExcludedBiomeCodes:    []string{"water.deep", "water.shallow", "wetland"},
			MinimumElevationMilli: 191,
			MaximumMoistureMilli:  839,
			MinimumDryNeighbors:   2,
		},
		DistrictRules: []WorldgenDistrictRule{
			{Code: "core", MinimumDistanceMilli: 0, MaximumDistanceMilli: 450, UseWeights: []WorldgenLandUseWeight{
				{PrimaryUse: LandUseCommercial, Weight: 55}, {PrimaryUse: LandUseResidential, Weight: 37}, {PrimaryUse: LandUseIndustrial, Weight: 8},
			}},
			{Code: "inner", MinimumDistanceMilli: 451, MaximumDistanceMilli: 950, UseWeights: []WorldgenLandUseWeight{
				{PrimaryUse: LandUseResidential, Weight: 56}, {PrimaryUse: LandUseCommercial, Weight: 26}, {PrimaryUse: LandUseIndustrial, Weight: 18},
			}},
			{Code: "fringe", MinimumDistanceMilli: 951, MaximumDistanceMilli: 1000, UseWeights: []WorldgenLandUseWeight{
				{PrimaryUse: LandUseIndustrial, Weight: 48}, {PrimaryUse: LandUseResidential, Weight: 36}, {PrimaryUse: LandUseCommercial, Weight: 16},
			}},
		},
		BuildingArchetypes: []WorldgenBuildingArchetype{
			{Code: "commercial.arcade", PrimaryUse: LandUseCommercial, Weight: 18, MinimumWidth: 10, MaximumWidth: 22, MinimumDepth: 10, MaximumDepth: 22, MinimumFloors: 2, MaximumFloors: 6, LayoutStyle: "arcade", DistrictCodes: []string{"core", "inner"}},
			{Code: "commercial.shopfront", PrimaryUse: LandUseCommercial, Weight: 42, MinimumWidth: 5, MaximumWidth: 15, MinimumDepth: 5, MaximumDepth: 18, MinimumFloors: 1, MaximumFloors: 4, LayoutStyle: "shopfront"},
			{Code: "commercial.tower_lobby", PrimaryUse: LandUseCommercial, Weight: 12, MinimumWidth: 14, MaximumWidth: 22, MinimumDepth: 14, MaximumDepth: 22, MinimumFloors: 5, MaximumFloors: 12, LayoutStyle: "tower", DistrictCodes: []string{"core"}},
			{Code: "industrial.depot", PrimaryUse: LandUseIndustrial, Weight: 28, MinimumWidth: 14, MaximumWidth: 24, MinimumDepth: 12, MaximumDepth: 24, MinimumFloors: 1, MaximumFloors: 2, LayoutStyle: "loading_depot", DistrictCodes: []string{"inner", "fringe"}},
			{Code: "industrial.workshop", PrimaryUse: LandUseIndustrial, Weight: 42, MinimumWidth: 6, MaximumWidth: 18, MinimumDepth: 6, MaximumDepth: 20, MinimumFloors: 1, MaximumFloors: 3, LayoutStyle: "workshop"},
			{Code: "residential.courtyard", PrimaryUse: LandUseResidential, Weight: 18, MinimumWidth: 12, MaximumWidth: 22, MinimumDepth: 12, MaximumDepth: 22, MinimumFloors: 2, MaximumFloors: 6, LayoutStyle: "courtyard", DistrictCodes: []string{"core", "inner"}},
			{Code: "residential.rowhouse", PrimaryUse: LandUseResidential, Weight: 50, MinimumWidth: 5, MaximumWidth: 14, MinimumDepth: 5, MaximumDepth: 16, MinimumFloors: 1, MaximumFloors: 4, LayoutStyle: "rowhouse"},
			{Code: "residential.walkup", PrimaryUse: LandUseResidential, Weight: 24, MinimumWidth: 12, MaximumWidth: 20, MinimumDepth: 12, MaximumDepth: 22, MinimumFloors: 3, MaximumFloors: 8, LayoutStyle: "walkup", DistrictCodes: []string{"inner"}},
		},
	}
	normalized, err := normalizeWorldgenProfile(profile)
	if err != nil {
		return nil, err
	}
	return &normalized, nil
}

func DefaultWorldgenBinding(
	simulationVersion string,
	worldSeed int64,
	spatialRootHash string,
	profile *WorldgenProfile,
) (WorldgenBinding, error) {
	if strings.TrimSpace(simulationVersion) == "" || worldSeed <= 0 || !validSHA256Hex(spatialRootHash) {
		return WorldgenBinding{}, ErrInvalidWorldgenInput
	}
	normalized, err := normalizeWorldgenProfileValue(profile)
	if err != nil {
		return WorldgenBinding{}, err
	}
	return WorldgenBinding{
		SimulationVersion: strings.TrimSpace(simulationVersion), WorldSeed: worldSeed,
		SpatialRootHash: spatialRootHash, ProfileID: normalized.ID,
		ProfileVersion: normalized.Version, ProfileHash: normalized.ContentHash,
		GeneratorID: DefaultWorldgenID, GeneratorVersion: DefaultWorldgenVersion,
	}, nil
}

// GenerateWorldgenPlan is the open-world stage pipeline:
// terrain patches -> city seeds -> branching roads -> road-front lots ->
// archetype-selected buildings. Each stage consumes only versioned inputs, so
// adding a terrain rule or archetype is deterministic and independently
// testable.
func GenerateWorldgenPlan(
	binding WorldgenBinding,
	profile *WorldgenProfile,
	bounds WorldgenBounds,
) (*GeneratedWorldgenPlan, error) {
	normalized, err := normalizeWorldgenProfileValue(profile)
	if err != nil {
		return nil, err
	}
	if err = validateWorldgenBinding(binding, normalized); err != nil {
		return nil, err
	}
	if err = validateWorldgenBounds(bounds); err != nil {
		return nil, err
	}

	terrain := generateWorldgenTerrain(binding, normalized, bounds)
	cities := generateWorldgenCities(binding, normalized, bounds, terrain)
	roads := generateWorldgenRoads(binding, normalized, bounds, cities)
	roadCells := worldgenRoadCells(bounds, roads)
	lots := generateWorldgenLots(binding, normalized, bounds, cities, roads, roadCells)
	buildings, err := generateWorldgenBuildings(binding, normalized, lots)
	if err != nil {
		return nil, err
	}
	plan := &GeneratedWorldgenPlan{
		Binding: binding, Profile: normalized, Bounds: bounds, Terrain: terrain,
		Cities: cities, Roads: roads, Lots: lots, Buildings: buildings,
	}
	if err = validateGeneratedWorldgenPlan(plan); err != nil {
		return nil, err
	}
	plan.BaselineHash, err = ComputeWorldgenPlanHash(plan)
	if err != nil {
		return nil, err
	}
	return plan, nil
}

func ComputeWorldgenPlanHash(plan *GeneratedWorldgenPlan) (string, error) {
	if plan == nil {
		return "", ErrInvalidWorldgenInput
	}
	raw, err := json.Marshal(struct {
		Binding   WorldgenBinding                 `json:"binding"`
		Profile   WorldgenProfile                 `json:"profile"`
		Bounds    WorldgenBounds                  `json:"bounds"`
		Terrain   []GeneratedWorldgenTerrainPatch `json:"terrain"`
		Cities    []GeneratedWorldgenCity         `json:"cities"`
		Roads     []GeneratedWorldgenRoad         `json:"roads"`
		Lots      []GeneratedWorldgenLot          `json:"lots"`
		Buildings []GeneratedWorldgenBuilding     `json:"buildings"`
	}{plan.Binding, plan.Profile, plan.Bounds, plan.Terrain, plan.Cities, plan.Roads, plan.Lots, plan.Buildings})
	if err != nil {
		return "", fmt.Errorf("marshal city world-generation plan: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func FilterWorldgenPlan(plan *GeneratedWorldgenPlan, bounds WorldgenBounds) (*GeneratedWorldgenWindow, error) {
	if plan == nil || validateWorldgenQueryBounds(bounds) != nil ||
		bounds.MinimumChunkX < plan.Bounds.MinimumChunkX || bounds.MaximumChunkX > plan.Bounds.MaximumChunkX ||
		bounds.MinimumChunkY < plan.Bounds.MinimumChunkY || bounds.MaximumChunkY > plan.Bounds.MaximumChunkY {
		return nil, ErrInvalidWorldgenInput
	}
	window := &GeneratedWorldgenWindow{
		GeneratorID: plan.Binding.GeneratorID, GeneratorVersion: plan.Binding.GeneratorVersion,
		ProfileID: plan.Profile.ID, ProfileVersion: plan.Profile.Version,
		PlanHash: plan.BaselineHash, Bounds: bounds,
		Terrain: make([]GeneratedWorldgenTerrainPatch, 0), Cities: make([]GeneratedWorldgenCity, 0),
		Roads: make([]GeneratedWorldgenRoad, 0), Lots: make([]GeneratedWorldgenLot, 0),
		Buildings: make([]GeneratedWorldgenBuilding, 0),
	}
	if bounds.Z != SurfaceZ {
		return window, nil
	}
	for _, terrain := range plan.Terrain {
		if worldgenChunkInBounds(bounds, terrain.ChunkX, terrain.ChunkY, terrain.Z) {
			window.Terrain = append(window.Terrain, terrain)
		}
	}
	for _, city := range plan.Cities {
		if worldgenPointInBounds(bounds, city.Center) {
			window.Cities = append(window.Cities, city)
		}
	}
	for _, road := range plan.Roads {
		if worldgenRoadIntersectsBounds(road, bounds) {
			window.Roads = append(window.Roads, cloneWorldgenRoad(road))
		}
	}
	selectedLots := make(map[string]struct{})
	for _, lot := range plan.Lots {
		if worldgenRectangleIntersectsBounds(lot.Bounds, bounds) {
			window.Lots = append(window.Lots, lot)
			selectedLots[lot.Code] = struct{}{}
		}
	}
	for _, building := range plan.Buildings {
		if _, selected := selectedLots[building.LotCode]; selected || worldgenFootprintIntersectsBounds(building.Footprint, bounds) {
			window.Buildings = append(window.Buildings, cloneWorldgenBuilding(building))
		}
	}
	return window, nil
}

func normalizeWorldgenProfileValue(profile *WorldgenProfile) (WorldgenProfile, error) {
	if profile == nil {
		return WorldgenProfile{}, ErrInvalidWorldgenInput
	}
	return normalizeWorldgenProfile(*profile)
}

func normalizeWorldgenProfile(profile WorldgenProfile) (WorldgenProfile, error) {
	profile.ID = strings.TrimSpace(profile.ID)
	profile.Version = strings.TrimSpace(profile.Version)
	profile.Name = strings.TrimSpace(profile.Name)
	profile.ContentHash = ""
	profile.TerrainRules = append([]WorldgenTerrainRule(nil), profile.TerrainRules...)
	profile.CityPlacement.ExcludedBiomeCodes = worldgenNormalizeStrings(profile.CityPlacement.ExcludedBiomeCodes)
	profile.DistrictRules = append([]WorldgenDistrictRule(nil), profile.DistrictRules...)
	profile.BuildingArchetypes = append([]WorldgenBuildingArchetype(nil), profile.BuildingArchetypes...)
	for index := range profile.TerrainRules {
		profile.TerrainRules[index].Code = strings.TrimSpace(profile.TerrainRules[index].Code)
		profile.TerrainRules[index].GroundDefinitionID = strings.TrimSpace(profile.TerrainRules[index].GroundDefinitionID)
	}
	for index := range profile.BuildingArchetypes {
		profile.BuildingArchetypes[index].Code = strings.TrimSpace(profile.BuildingArchetypes[index].Code)
		profile.BuildingArchetypes[index].LayoutStyle = strings.TrimSpace(profile.BuildingArchetypes[index].LayoutStyle)
		profile.BuildingArchetypes[index].DistrictCodes = worldgenNormalizeStrings(profile.BuildingArchetypes[index].DistrictCodes)
	}
	for index := range profile.DistrictRules {
		profile.DistrictRules[index].Code = strings.TrimSpace(profile.DistrictRules[index].Code)
		profile.DistrictRules[index].UseWeights = append([]WorldgenLandUseWeight(nil), profile.DistrictRules[index].UseWeights...)
		sort.Slice(profile.DistrictRules[index].UseWeights, func(i, j int) bool {
			return profile.DistrictRules[index].UseWeights[i].PrimaryUse < profile.DistrictRules[index].UseWeights[j].PrimaryUse
		})
	}
	sort.Slice(profile.TerrainRules, func(i, j int) bool {
		if profile.TerrainRules[i].Priority != profile.TerrainRules[j].Priority {
			return profile.TerrainRules[i].Priority < profile.TerrainRules[j].Priority
		}
		return profile.TerrainRules[i].Code < profile.TerrainRules[j].Code
	})
	sort.Slice(profile.BuildingArchetypes, func(i, j int) bool {
		return profile.BuildingArchetypes[i].Code < profile.BuildingArchetypes[j].Code
	})
	sort.Slice(profile.DistrictRules, func(i, j int) bool {
		if profile.DistrictRules[i].MinimumDistanceMilli != profile.DistrictRules[j].MinimumDistanceMilli {
			return profile.DistrictRules[i].MinimumDistanceMilli < profile.DistrictRules[j].MinimumDistanceMilli
		}
		return profile.DistrictRules[i].Code < profile.DistrictRules[j].Code
	})
	if err := validateWorldgenProfile(profile); err != nil {
		return WorldgenProfile{}, err
	}
	raw, err := json.Marshal(struct {
		ID                 string                      `json:"id"`
		Version            string                      `json:"version"`
		Name               string                      `json:"name"`
		CityCount          int                         `json:"city_count"`
		MinimumCityRadius  int                         `json:"minimum_city_radius_chunks"`
		MaximumCityRadius  int                         `json:"maximum_city_radius_chunks"`
		ArterialRoadWidth  int                         `json:"arterial_road_width"`
		LocalStreetWidth   int                         `json:"local_street_width"`
		MinimumLotFrontage int                         `json:"minimum_lot_frontage"`
		MaximumLotFrontage int                         `json:"maximum_lot_frontage"`
		MinimumLotDepth    int                         `json:"minimum_lot_depth"`
		MaximumLotDepth    int                         `json:"maximum_lot_depth"`
		TerrainRules       []WorldgenTerrainRule       `json:"terrain_rules"`
		CityPlacement      WorldgenCityPlacementRule   `json:"city_placement"`
		DistrictRules      []WorldgenDistrictRule      `json:"district_rules"`
		Archetypes         []WorldgenBuildingArchetype `json:"building_archetypes"`
	}{
		profile.ID, profile.Version, profile.Name, profile.CityCount,
		profile.MinimumCityRadius, profile.MaximumCityRadius,
		profile.ArterialRoadWidth, profile.LocalStreetWidth,
		profile.MinimumLotFrontage, profile.MaximumLotFrontage,
		profile.MinimumLotDepth, profile.MaximumLotDepth,
		profile.TerrainRules, profile.CityPlacement, profile.DistrictRules, profile.BuildingArchetypes,
	})
	if err != nil {
		return WorldgenProfile{}, fmt.Errorf("marshal world-generation profile: %w", err)
	}
	sum := sha256.Sum256(raw)
	profile.ContentHash = hex.EncodeToString(sum[:])
	return profile, nil
}

func validateWorldgenProfile(profile WorldgenProfile) error {
	if profile.ID == "" || profile.Version == "" || profile.Name == "" || profile.CityCount <= 0 ||
		profile.CityCount > maximumWorldgenCities || profile.MinimumCityRadius < 2 ||
		profile.MaximumCityRadius < profile.MinimumCityRadius || profile.MaximumCityRadius > maximumWorldgenAxis ||
		profile.ArterialRoadWidth < 2 || profile.ArterialRoadWidth > 12 ||
		profile.LocalStreetWidth < 1 || profile.LocalStreetWidth > profile.ArterialRoadWidth ||
		profile.MinimumLotFrontage < 4 || profile.MaximumLotFrontage < profile.MinimumLotFrontage ||
		profile.MinimumLotDepth < 5 || profile.MaximumLotDepth < profile.MinimumLotDepth ||
		len(profile.TerrainRules) == 0 || len(profile.DistrictRules) == 0 || len(profile.BuildingArchetypes) == 0 ||
		profile.CityPlacement.MinimumElevationMilli < 0 || profile.CityPlacement.MinimumElevationMilli > 1000 ||
		profile.CityPlacement.MaximumMoistureMilli < 0 || profile.CityPlacement.MaximumMoistureMilli > 1000 ||
		profile.CityPlacement.MinimumDryNeighbors < 0 || profile.CityPlacement.MinimumDryNeighbors > 8 {
		return ErrInvalidWorldgenInput
	}
	terrainCodes := make(map[string]struct{}, len(profile.TerrainRules))
	for _, rule := range profile.TerrainRules {
		if rule.Code == "" || rule.GroundDefinitionID == "" || rule.Priority < 0 ||
			rule.MinimumElevationMilli < 0 || rule.MaximumElevationMilli > 1000 ||
			rule.MinimumElevationMilli > rule.MaximumElevationMilli ||
			rule.MinimumMoistureMilli < 0 || rule.MaximumMoistureMilli > 1000 ||
			rule.MinimumMoistureMilli > rule.MaximumMoistureMilli {
			return ErrInvalidWorldgenInput
		}
		if _, duplicate := terrainCodes[rule.Code]; duplicate {
			return ErrInvalidWorldgenInput
		}
		terrainCodes[rule.Code] = struct{}{}
	}
	for _, code := range profile.CityPlacement.ExcludedBiomeCodes {
		if code == "" {
			return ErrInvalidWorldgenInput
		}
	}
	districtCodes := make(map[string]struct{}, len(profile.DistrictRules))
	expectedMinimumDistance := 0
	for _, district := range profile.DistrictRules {
		if district.Code == "" || district.MinimumDistanceMilli != expectedMinimumDistance ||
			district.MaximumDistanceMilli < district.MinimumDistanceMilli || district.MaximumDistanceMilli > 1000 ||
			len(district.UseWeights) == 0 {
			return ErrInvalidWorldgenInput
		}
		if _, duplicate := districtCodes[district.Code]; duplicate {
			return ErrInvalidWorldgenInput
		}
		districtCodes[district.Code] = struct{}{}
		useWeights := make(map[LandUse]struct{}, len(district.UseWeights))
		for _, useWeight := range district.UseWeights {
			switch useWeight.PrimaryUse {
			case LandUseResidential, LandUseCommercial, LandUseIndustrial:
			default:
				return ErrInvalidWorldgenInput
			}
			if useWeight.Weight <= 0 {
				return ErrInvalidWorldgenInput
			}
			if _, duplicate := useWeights[useWeight.PrimaryUse]; duplicate {
				return ErrInvalidWorldgenInput
			}
			useWeights[useWeight.PrimaryUse] = struct{}{}
		}
		expectedMinimumDistance = district.MaximumDistanceMilli + 1
	}
	if expectedMinimumDistance != 1001 {
		return ErrInvalidWorldgenInput
	}
	archetypeCodes := make(map[string]struct{}, len(profile.BuildingArchetypes))
	for _, archetype := range profile.BuildingArchetypes {
		if archetype.Code == "" || archetype.Weight <= 0 || archetype.MinimumWidth < 4 ||
			archetype.MaximumWidth < archetype.MinimumWidth || archetype.MinimumDepth < 4 ||
			archetype.MaximumDepth < archetype.MinimumDepth || archetype.MinimumFloors <= 0 ||
			archetype.MaximumFloors < archetype.MinimumFloors || archetype.LayoutStyle == "" {
			return ErrInvalidWorldgenInput
		}
		switch archetype.PrimaryUse {
		case LandUseResidential, LandUseCommercial, LandUseIndustrial:
		default:
			return ErrInvalidWorldgenInput
		}
		if _, duplicate := archetypeCodes[archetype.Code]; duplicate {
			return ErrInvalidWorldgenInput
		}
		for _, districtCode := range archetype.DistrictCodes {
			if _, found := districtCodes[districtCode]; !found {
				return ErrInvalidWorldgenInput
			}
		}
		archetypeCodes[archetype.Code] = struct{}{}
	}
	// Every district/use pair that the profile can select must have at least
	// one archetype that fits the smallest permitted lot.  This catches a
	// configuration error at profile-load time instead of discovering it halfway
	// through a deterministic generation run.
	for _, district := range profile.DistrictRules {
		for _, useWeight := range district.UseWeights {
			if !worldgenProfileHasViableArchetype(profile, district.Code, useWeight.PrimaryUse) {
				return ErrInvalidWorldgenInput
			}
		}
	}
	return nil
}

func worldgenProfileHasViableArchetype(profile WorldgenProfile, districtCode string, primaryUse LandUse) bool {
	minimumAvailableWidth := profile.MinimumLotFrontage - 2
	minimumAvailableDepth := profile.MinimumLotDepth - 2
	for _, archetype := range profile.BuildingArchetypes {
		if archetype.PrimaryUse == primaryUse && worldgenArchetypeAllowsDistrict(archetype, districtCode) &&
			archetype.MinimumWidth <= minimumAvailableWidth && archetype.MinimumDepth <= minimumAvailableDepth {
			return true
		}
	}
	return false
}

func validateWorldgenBinding(binding WorldgenBinding, profile WorldgenProfile) error {
	if strings.TrimSpace(binding.SimulationVersion) == "" || binding.WorldSeed <= 0 ||
		!validSHA256Hex(binding.SpatialRootHash) || binding.ProfileID != profile.ID ||
		binding.ProfileVersion != profile.Version || binding.ProfileHash != profile.ContentHash ||
		binding.GeneratorID != DefaultWorldgenID || !isSupportedWorldgenVersion(binding.GeneratorVersion) {
		return ErrInvalidWorldgenInput
	}
	return nil
}

func isSupportedWorldgenVersion(version string) bool {
	return version == DefaultWorldgenVersion || version == OpenWorldRegionGeneratorVersion ||
		version == OpenWorldVerticalGeneratorVersion
}

func validateWorldgenBounds(bounds WorldgenBounds) error {
	if bounds.Z != SurfaceZ || validateWorldgenQueryBounds(bounds) != nil {
		return ErrInvalidWorldgenInput
	}
	return nil
}

func validateWorldgenQueryBounds(bounds WorldgenBounds) error {
	if bounds.Z < MinimumZ || bounds.Z > MaximumZ || bounds.MinimumChunkX > bounds.MaximumChunkX ||
		bounds.MinimumChunkY > bounds.MaximumChunkY ||
		bounds.MaximumChunkX-bounds.MinimumChunkX+1 > maximumWorldgenAxis ||
		bounds.MaximumChunkY-bounds.MinimumChunkY+1 > maximumWorldgenAxis {
		return ErrInvalidWorldgenInput
	}
	return nil
}

func generateWorldgenTerrain(
	binding WorldgenBinding,
	profile WorldgenProfile,
	bounds WorldgenBounds,
) []GeneratedWorldgenTerrainPatch {
	result := make([]GeneratedWorldgenTerrainPatch, 0, int((bounds.MaximumChunkX-bounds.MinimumChunkX+1)*(bounds.MaximumChunkY-bounds.MinimumChunkY+1)))
	for chunkY := bounds.MinimumChunkY; chunkY <= bounds.MaximumChunkY; chunkY++ {
		for chunkX := bounds.MinimumChunkX; chunkX <= bounds.MaximumChunkX; chunkX++ {
			elevation := worldgenField(binding, "terrain.elevation", chunkX, chunkY)
			moisture := worldgenField(binding, "terrain.moisture", chunkX, chunkY)
			rule := profile.TerrainRules[len(profile.TerrainRules)-1]
			for _, candidate := range profile.TerrainRules {
				if elevation >= candidate.MinimumElevationMilli && elevation <= candidate.MaximumElevationMilli &&
					moisture >= candidate.MinimumMoistureMilli && moisture <= candidate.MaximumMoistureMilli {
					rule = candidate
					break
				}
			}
			result = append(result, GeneratedWorldgenTerrainPatch{
				ChunkX: chunkX, ChunkY: chunkY, Z: bounds.Z, BiomeCode: rule.Code,
				DefinitionID: rule.GroundDefinitionID, ElevationMilli: elevation, MoistureMilli: moisture,
			})
		}
	}
	return result
}

func generateWorldgenCities(
	binding WorldgenBinding,
	profile WorldgenProfile,
	bounds WorldgenBounds,
	terrain []GeneratedWorldgenTerrainPatch,
) []GeneratedWorldgenCity {
	type candidate struct {
		x, y      int64
		score     uint64
		terrain   GeneratedWorldgenTerrainPatch
		preferred bool
		radius    int
		interior  bool
	}
	terrainByChunk := make(map[WorldgenPoint]GeneratedWorldgenTerrainPatch, len(terrain))
	for _, patch := range terrain {
		terrainByChunk[WorldgenPoint{X: patch.ChunkX, Y: patch.ChunkY, Z: patch.Z}] = patch
	}
	maximumRadius := profile.MaximumCityRadius
	minimumAxis := bounds.MaximumChunkX - bounds.MinimumChunkX + 1
	if axis := bounds.MaximumChunkY - bounds.MinimumChunkY + 1; axis < minimumAxis {
		minimumAxis = axis
	}
	if boundedMaximum := int((minimumAxis - 3) / 2); boundedMaximum >= profile.MinimumCityRadius {
		maximumRadius = minWorldgenInt(maximumRadius, boundedMaximum)
	}
	preferredInterior := make([]candidate, 0)
	preferredEdge := make([]candidate, 0)
	fallbackInterior := make([]candidate, 0)
	fallbackEdge := make([]candidate, 0)
	for y := bounds.MinimumChunkY; y <= bounds.MaximumChunkY; y++ {
		for x := bounds.MinimumChunkX; x <= bounds.MaximumChunkX; x++ {
			patch, found := terrainByChunk[WorldgenPoint{X: x, Y: y, Z: bounds.Z}]
			if !found {
				continue
			}
			radius := profile.MinimumCityRadius + int(worldgenDraw(binding, "city.radius", x, y)%uint64(maximumRadius-profile.MinimumCityRadius+1))
			value := candidate{
				x: x, y: y, terrain: patch,
				score:     worldgenDraw(binding, "city.candidate", x, y),
				preferred: worldgenCityPlacementSuitable(profile, bounds, terrainByChunk, x, y),
				radius:    radius,
				interior:  worldgenCityNeighborhoodFitsBounds(bounds, x, y, radius+1),
			}
			switch {
			case value.preferred && value.interior:
				preferredInterior = append(preferredInterior, value)
			case value.preferred:
				preferredEdge = append(preferredEdge, value)
			case value.interior:
				fallbackInterior = append(fallbackInterior, value)
			default:
				fallbackEdge = append(fallbackEdge, value)
			}
		}
	}
	sort.Slice(preferredInterior, func(i, j int) bool {
		if preferredInterior[i].score != preferredInterior[j].score {
			return preferredInterior[i].score < preferredInterior[j].score
		}
		if preferredInterior[i].y != preferredInterior[j].y {
			return preferredInterior[i].y < preferredInterior[j].y
		}
		return preferredInterior[i].x < preferredInterior[j].x
	})
	sort.Slice(preferredEdge, func(i, j int) bool {
		if preferredEdge[i].score != preferredEdge[j].score {
			return preferredEdge[i].score < preferredEdge[j].score
		}
		if preferredEdge[i].y != preferredEdge[j].y {
			return preferredEdge[i].y < preferredEdge[j].y
		}
		return preferredEdge[i].x < preferredEdge[j].x
	})
	// A custom profile can deliberately make every patch hostile.  The fallback
	// keeps generation total and deterministic while clearly labelling the seed
	// so a later materializer can require terrain remediation instead of
	// silently treating it as a normal settlement site.
	sortFallback := func(values []candidate) {
		sort.Slice(values, func(i, j int) bool {
			if values[i].terrain.ElevationMilli != values[j].terrain.ElevationMilli {
				return values[i].terrain.ElevationMilli > values[j].terrain.ElevationMilli
			}
			if values[i].terrain.MoistureMilli != values[j].terrain.MoistureMilli {
				return values[i].terrain.MoistureMilli < values[j].terrain.MoistureMilli
			}
			if values[i].score != values[j].score {
				return values[i].score < values[j].score
			}
			if values[i].y != values[j].y {
				return values[i].y < values[j].y
			}
			return values[i].x < values[j].x
		})
	}
	sortFallback(fallbackInterior)
	sortFallback(fallbackEdge)
	// A planning extent must show a complete settlement before it shows a
	// better but clipped edge candidate. The fallback remains explicit in the
	// resulting plan so materialization can require remediation when needed.
	candidates := append(preferredInterior, fallbackInterior...)
	candidates = append(candidates, preferredEdge...)
	candidates = append(candidates, fallbackEdge...)
	result := make([]GeneratedWorldgenCity, 0, profile.CityCount)
	for _, candidate := range candidates {
		if len(result) >= profile.CityCount {
			break
		}
		radius := candidate.radius
		valid := true
		for _, existing := range result {
			existingChunkX := worldgenChunkFromWorld(existing.Center.X)
			existingChunkY := worldgenChunkFromWorld(existing.Center.Y)
			if worldgenAbs(candidate.x-existingChunkX)+worldgenAbs(candidate.y-existingChunkY) < int64(maxWorldgenInt(3, radius+existing.RadiusChunks-1)) {
				valid = false
				break
			}
		}
		if !valid {
			continue
		}
		placementMode := "fallback"
		if candidate.preferred {
			placementMode = "preferred"
		}
		result = append(result, GeneratedWorldgenCity{
			Code:           worldgenCityCode(binding, bounds, len(result)+1),
			Center:         worldgenChunkCenter(candidate.x, candidate.y, bounds.Z),
			RadiusChunks:   radius,
			BiomeCode:      candidate.terrain.BiomeCode,
			ElevationMilli: candidate.terrain.ElevationMilli,
			MoistureMilli:  candidate.terrain.MoistureMilli,
			PlacementMode:  placementMode,
		})
	}
	return result
}

func worldgenCityCode(binding WorldgenBinding, bounds WorldgenBounds, index int) string {
	if binding.GeneratorVersion == OpenWorldRegionGeneratorVersion ||
		binding.GeneratorVersion == OpenWorldVerticalGeneratorVersion {
		// V2 materializes many independent regions.  Prefix the local ordinal
		// with the immutable region origin so city, lot, and building codes are
		// globally unique within a world rather than colliding at city_01.
		return fmt.Sprintf("city_r%d_%d_%02d", bounds.MinimumChunkX, bounds.MinimumChunkY, index)
	}
	return fmt.Sprintf("city_%02d", index)
}

func worldgenCityPlacementSuitable(
	profile WorldgenProfile,
	bounds WorldgenBounds,
	terrainByChunk map[WorldgenPoint]GeneratedWorldgenTerrainPatch,
	chunkX, chunkY int64,
) bool {
	patch, found := terrainByChunk[WorldgenPoint{X: chunkX, Y: chunkY, Z: bounds.Z}]
	if !found || !worldgenTerrainBuildableForCity(profile.CityPlacement, patch) {
		return false
	}
	dryNeighbors := 0
	for deltaY := int64(-1); deltaY <= 1; deltaY++ {
		for deltaX := int64(-1); deltaX <= 1; deltaX++ {
			if deltaX == 0 && deltaY == 0 {
				continue
			}
			neighbor, found := terrainByChunk[WorldgenPoint{X: chunkX + deltaX, Y: chunkY + deltaY, Z: bounds.Z}]
			if found && worldgenTerrainBuildableForCity(profile.CityPlacement, neighbor) {
				dryNeighbors++
			}
		}
	}
	return dryNeighbors >= profile.CityPlacement.MinimumDryNeighbors
}

func worldgenTerrainBuildableForCity(
	rule WorldgenCityPlacementRule,
	patch GeneratedWorldgenTerrainPatch,
) bool {
	if patch.ElevationMilli < rule.MinimumElevationMilli || patch.MoistureMilli > rule.MaximumMoistureMilli {
		return false
	}
	for _, excluded := range rule.ExcludedBiomeCodes {
		if patch.BiomeCode == excluded {
			return false
		}
	}
	return true
}

func worldgenCityNeighborhoodFitsBounds(bounds WorldgenBounds, chunkX, chunkY int64, radius int) bool {
	margin := int64(radius)
	return chunkX-margin >= bounds.MinimumChunkX && chunkX+margin <= bounds.MaximumChunkX &&
		chunkY-margin >= bounds.MinimumChunkY && chunkY+margin <= bounds.MaximumChunkY
}

func generateWorldgenRoads(
	binding WorldgenBinding,
	profile WorldgenProfile,
	bounds WorldgenBounds,
	cities []GeneratedWorldgenCity,
) []GeneratedWorldgenRoad {
	roads := make([]GeneratedWorldgenRoad, 0, len(cities)*24)
	for _, city := range cities {
		centerChunkX := worldgenChunkFromWorld(city.Center.X)
		centerChunkY := worldgenChunkFromWorld(city.Center.Y)
		localIndex := 0
		for direction := 0; direction < 4; direction++ {
			directionX, directionY := worldgenRoadDirection(direction)
			length := maxWorldgenInt(2, city.RadiusChunks-1+int(worldgenDraw(binding, "road.arterial.length/"+city.Code+"/"+strconv.Itoa(direction), centerChunkX, centerChunkY)%3))
			destination := worldgenClampWorldPoint(bounds, WorldgenPoint{
				X: city.Center.X + directionX*int64(length)*DefaultChunkSize,
				Y: city.Center.Y + directionY*int64(length)*DefaultChunkSize,
				Z: city.Center.Z,
			})
			if destination == city.Center {
				continue
			}
			code := fmt.Sprintf("road_%s_a%02d", city.Code, direction+1)
			roads = append(roads, GeneratedWorldgenRoad{
				Code: code, CityCode: city.Code, Class: WorldgenRoadArterial, Width: profile.ArterialRoadWidth,
				Points: []WorldgenPoint{city.Center, destination},
			})
			armLength := int((worldgenAbs(destination.X-city.Center.X) + worldgenAbs(destination.Y-city.Center.Y)) / DefaultChunkSize)
			for offset := 1; offset < armLength; {
				origin := WorldgenPoint{
					X: city.Center.X + directionX*int64(offset)*DefaultChunkSize,
					Y: city.Center.Y + directionY*int64(offset)*DefaultChunkSize,
					Z: city.Center.Z,
				}
				namespace := "road.local/" + city.Code + "/" + strconv.Itoa(direction) + "/" + strconv.Itoa(offset)
				side := -1
				if worldgenDraw(binding, namespace+"/side", origin.X, origin.Y)&1 == 1 {
					side = 1
				}
				sides := []int{side}
				if worldgenDraw(binding, namespace+"/second-side", origin.X, origin.Y)%4 == 0 {
					sides = append(sides, -side)
				}
				for _, currentSide := range sides {
					lateralLength := 1 + int(worldgenDraw(binding, namespace+"/length/"+strconv.Itoa(currentSide), origin.X, origin.Y)%uint64(maxWorldgenInt(1, city.RadiusChunks/2)))
					hookLength := 1 + int(worldgenDraw(binding, namespace+"/hook/"+strconv.Itoa(currentSide), origin.X, origin.Y)%uint64(maxWorldgenInt(1, city.RadiusChunks/2)))
					hookForward := worldgenDraw(binding, namespace+"/hook-direction/"+strconv.Itoa(currentSide), origin.X, origin.Y)&1 == 0
					points := worldgenLocalStreetPoints(bounds, origin, directionX, directionY, currentSide, lateralLength, hookLength, hookForward)
					if len(points) < 2 {
						continue
					}
					localIndex++
					roads = append(roads, GeneratedWorldgenRoad{
						Code: fmt.Sprintf("road_%s_l%03d", city.Code, localIndex), CityCode: city.Code,
						Class: WorldgenRoadLocal, Width: profile.LocalStreetWidth, Points: points,
					})
				}
				offset += 1 + int(worldgenDraw(binding, namespace+"/spacing", origin.X, origin.Y)%2)
			}
		}
	}
	sort.Slice(roads, func(i, j int) bool { return roads[i].Code < roads[j].Code })
	return roads
}

func generateWorldgenLots(
	binding WorldgenBinding,
	profile WorldgenProfile,
	bounds WorldgenBounds,
	cities []GeneratedWorldgenCity,
	roads []GeneratedWorldgenRoad,
	roadCells map[WorldgenPoint]string,
) []GeneratedWorldgenLot {
	citiesByCode := make(map[string]GeneratedWorldgenCity, len(cities))
	for _, city := range cities {
		citiesByCode[city.Code] = city
	}
	lotCells := make(map[WorldgenPoint]string)
	lots := make([]GeneratedWorldgenLot, 0, 96)
	for _, road := range roads {
		if len(lots) >= maximumWorldgenLots {
			break
		}
		city, found := citiesByCode[road.CityCode]
		if !found {
			continue
		}
		for segmentIndex := 1; segmentIndex < len(road.Points) && len(lots) < maximumWorldgenLots; segmentIndex++ {
			start, end := road.Points[segmentIndex-1], road.Points[segmentIndex]
			for _, candidate := range worldgenLotCandidates(binding, profile, bounds, road, start, end) {
				if len(lots) >= maximumWorldgenLots {
					break
				}
				if !worldgenRectangleAvailable(candidate.Bounds, roadCells, lotCells) {
					continue
				}
				candidate.Code = fmt.Sprintf("lot_%s_%03d", road.CityCode, len(lots)+1)
				candidate.CityCode = road.CityCode
				candidate.PrimaryUse, candidate.DistrictCode = worldgenSelectLotUse(binding, profile, city, candidate.Bounds)
				candidate.FrontageRoadCode = road.Code
				lots = append(lots, candidate)
				worldgenReserveRectangle(candidate.Bounds, lotCells, candidate.Code)
			}
		}
	}
	sort.Slice(lots, func(i, j int) bool { return lots[i].Code < lots[j].Code })
	return lots
}

func worldgenLotCandidates(
	binding WorldgenBinding,
	profile WorldgenProfile,
	bounds WorldgenBounds,
	road GeneratedWorldgenRoad,
	start, end WorldgenPoint,
) []GeneratedWorldgenLot {
	if start.Z != bounds.Z || end.Z != bounds.Z || (start.X != end.X && start.Y != end.Y) {
		return []GeneratedWorldgenLot{}
	}
	result := make([]GeneratedWorldgenLot, 0)
	isHorizontal := start.Y == end.Y
	segmentMinimum, segmentMaximum := start.X, end.X
	if !isHorizontal {
		segmentMinimum, segmentMaximum = start.Y, end.Y
	}
	if segmentMinimum > segmentMaximum {
		segmentMinimum, segmentMaximum = segmentMaximum, segmentMinimum
	}
	for cursor, lotIndex := segmentMinimum+6, 0; cursor+int64(profile.MinimumLotFrontage)+5 <= segmentMaximum; lotIndex++ {
		frontage := profile.MinimumLotFrontage + int(worldgenDraw(binding, road.Code+"/lot.frontage/"+strconv.Itoa(lotIndex), cursor, int64(lotIndex))%uint64(profile.MaximumLotFrontage-profile.MinimumLotFrontage+1))
		depth := profile.MinimumLotDepth + int(worldgenDraw(binding, road.Code+"/lot.depth/"+strconv.Itoa(lotIndex), cursor, int64(lotIndex))%uint64(profile.MaximumLotDepth-profile.MinimumLotDepth+1))
		stride := int64(frontage + 2 + int(worldgenDraw(binding, road.Code+"/lot.gap/"+strconv.Itoa(lotIndex), cursor, int64(lotIndex))%3))
		for side := 0; side < 2; side++ {
			candidate := GeneratedWorldgenLot{Bounds: WorldgenRectangle{Z: bounds.Z}}
			if isHorizontal {
				candidate.Bounds.MinimumX, candidate.Bounds.MaximumX = cursor, cursor+int64(frontage)-1
				if side == 0 {
					candidate.Bounds.MaximumY = start.Y - int64((road.Width+1)/2) - 1
					candidate.Bounds.MinimumY = candidate.Bounds.MaximumY - int64(depth) + 1
					candidate.FrontageDirection = "south"
				} else {
					candidate.Bounds.MinimumY = start.Y + int64(road.Width/2) + 1
					candidate.Bounds.MaximumY = candidate.Bounds.MinimumY + int64(depth) - 1
					candidate.FrontageDirection = "north"
				}
			} else {
				candidate.Bounds.MinimumY, candidate.Bounds.MaximumY = cursor, cursor+int64(frontage)-1
				if side == 0 {
					candidate.Bounds.MaximumX = start.X - int64((road.Width+1)/2) - 1
					candidate.Bounds.MinimumX = candidate.Bounds.MaximumX - int64(depth) + 1
					candidate.FrontageDirection = "east"
				} else {
					candidate.Bounds.MinimumX = start.X + int64(road.Width/2) + 1
					candidate.Bounds.MaximumX = candidate.Bounds.MinimumX + int64(depth) - 1
					candidate.FrontageDirection = "west"
				}
			}
			if worldgenRectangleWithinBounds(candidate.Bounds, bounds) {
				result = append(result, candidate)
			}
		}
		cursor += stride
	}
	return result
}

func generateWorldgenBuildings(
	binding WorldgenBinding,
	profile WorldgenProfile,
	lots []GeneratedWorldgenLot,
) ([]GeneratedWorldgenBuilding, error) {
	buildings := make([]GeneratedWorldgenBuilding, 0, len(lots))
	for _, lot := range lots {
		archetype, found := worldgenSelectArchetype(binding, profile, lot)
		if !found {
			return nil, fmt.Errorf("%w: no archetype for %s", ErrInvalidWorldgenInput, lot.PrimaryUse)
		}
		building, err := worldgenBuildingForLot(binding, lot, archetype)
		if err != nil {
			return nil, err
		}
		buildings = append(buildings, building)
	}
	sort.Slice(buildings, func(i, j int) bool { return buildings[i].Code < buildings[j].Code })
	return buildings, nil
}

func worldgenSelectArchetype(
	binding WorldgenBinding,
	profile WorldgenProfile,
	lot GeneratedWorldgenLot,
) (WorldgenBuildingArchetype, bool) {
	choices := make([]WorldgenBuildingArchetype, 0)
	totalWeight := 0
	for _, archetype := range profile.BuildingArchetypes {
		if archetype.PrimaryUse != lot.PrimaryUse || archetype.MinimumWidth > worldgenRectangleWidth(lot.Bounds)-2 ||
			archetype.MinimumDepth > worldgenRectangleHeight(lot.Bounds)-2 ||
			!worldgenArchetypeAllowsDistrict(archetype, lot.DistrictCode) {
			continue
		}
		choices = append(choices, archetype)
		totalWeight += archetype.Weight
	}
	if totalWeight <= 0 {
		return WorldgenBuildingArchetype{}, false
	}
	target := int(worldgenDraw(binding, "building.archetype/"+lot.Code, lot.Bounds.MinimumX, lot.Bounds.MinimumY) % uint64(totalWeight))
	for _, archetype := range choices {
		if target < archetype.Weight {
			return archetype, true
		}
		target -= archetype.Weight
	}
	return choices[len(choices)-1], true
}

func worldgenArchetypeAllowsDistrict(archetype WorldgenBuildingArchetype, districtCode string) bool {
	if len(archetype.DistrictCodes) == 0 {
		return true
	}
	for _, allowed := range archetype.DistrictCodes {
		if allowed == districtCode {
			return true
		}
	}
	return false
}

func worldgenBuildingForLot(
	binding WorldgenBinding,
	lot GeneratedWorldgenLot,
	archetype WorldgenBuildingArchetype,
) (GeneratedWorldgenBuilding, error) {
	availableWidth := worldgenRectangleWidth(lot.Bounds) - 2
	availableDepth := worldgenRectangleHeight(lot.Bounds) - 2
	if availableWidth < archetype.MinimumWidth || availableDepth < archetype.MinimumDepth {
		return GeneratedWorldgenBuilding{}, ErrInvalidWorldgenInput
	}
	maximumWidth := minWorldgenInt(archetype.MaximumWidth, availableWidth)
	maximumDepth := minWorldgenInt(archetype.MaximumDepth, availableDepth)
	width := archetype.MinimumWidth + int(worldgenDraw(binding, "building.width/"+lot.Code, lot.Bounds.MinimumX, lot.Bounds.MaximumX)%uint64(maximumWidth-archetype.MinimumWidth+1))
	depth := archetype.MinimumDepth + int(worldgenDraw(binding, "building.depth/"+lot.Code, lot.Bounds.MinimumY, lot.Bounds.MaximumY)%uint64(maximumDepth-archetype.MinimumDepth+1))
	minimumX := lot.Bounds.MinimumX + int64((worldgenRectangleWidth(lot.Bounds)-width)/2)
	minimumY := lot.Bounds.MinimumY + int64((worldgenRectangleHeight(lot.Bounds)-depth)/2)
	switch lot.FrontageDirection {
	case "north":
		minimumY = lot.Bounds.MinimumY + 1
	case "south":
		minimumY = lot.Bounds.MaximumY - int64(depth)
	case "west":
		minimumX = lot.Bounds.MinimumX + 1
	case "east":
		minimumX = lot.Bounds.MaximumX - int64(width)
	default:
		return GeneratedWorldgenBuilding{}, ErrInvalidWorldgenInput
	}
	footprint := make(map[WorldgenPoint]struct{}, width*depth)
	for y := minimumY; y < minimumY+int64(depth); y++ {
		for x := minimumX; x < minimumX+int64(width); x++ {
			footprint[WorldgenPoint{X: x, Y: y, Z: lot.Bounds.Z}] = struct{}{}
		}
	}
	worldgenShapeFootprint(footprint, width, depth, minimumX, minimumY, lot.FrontageDirection, archetype.LayoutStyle,
		worldgenDraw(binding, "building.shape/"+lot.Code, minimumX, minimumY))
	entrance := worldgenBuildingEntrance(footprint, lot.FrontageDirection, minimumX, minimumY, width, depth, lot.Bounds.Z)
	if _, present := footprint[entrance]; !present {
		return GeneratedWorldgenBuilding{}, ErrInvalidWorldgenInput
	}
	floors := archetype.MinimumFloors + int32(worldgenDraw(binding, "building.floors/"+lot.Code, minimumX, minimumY)%uint64(archetype.MaximumFloors-archetype.MinimumFloors+1))
	points := make([]WorldgenPoint, 0, len(footprint))
	for point := range footprint {
		points = append(points, point)
	}
	sort.Slice(points, func(i, j int) bool {
		if points[i].Y != points[j].Y {
			return points[i].Y < points[j].Y
		}
		return points[i].X < points[j].X
	})
	return GeneratedWorldgenBuilding{
		Code: "building_" + strings.TrimPrefix(lot.Code, "lot_"), CityCode: lot.CityCode,
		LotCode: lot.Code, PrimaryUse: lot.PrimaryUse, ArchetypeCode: archetype.Code,
		LayoutStyle: archetype.LayoutStyle, FloorCount: floors, Entrance: entrance, Footprint: points,
	}, nil
}

func worldgenShapeFootprint(
	footprint map[WorldgenPoint]struct{},
	width, depth int,
	minimumX, minimumY int64,
	frontage, style string,
	seed uint64,
) {
	if width < 6 || depth < 6 {
		return
	}
	remove := func(x, y int64) {
		delete(footprint, WorldgenPoint{X: x, Y: y, Z: SurfaceZ})
	}
	removeRectangle := func(startX, startY, endX, endY int64) {
		for y := startY; y <= endY; y++ {
			for x := startX; x <= endX; x++ {
				remove(x, y)
			}
		}
	}
	minimumCornerX, maximumCornerX := minimumX, minimumX+int64(width)-1
	minimumCornerY, maximumCornerY := minimumY, minimumY+int64(depth)-1
	cutCorner := func(cutWidth, cutDepth int64) {
		startX, startY := minimumCornerX, minimumCornerY
		if seed&1 == 1 {
			startX = maximumCornerX - cutWidth + 1
		}
		if seed>>1&1 == 1 {
			startY = maximumCornerY - cutDepth + 1
		}
		removeRectangle(startX, startY, startX+cutWidth-1, startY+cutDepth-1)
	}
	switch style {
	case "courtyard":
		if width >= 9 && depth >= 9 {
			centerX, centerY := minimumX+int64(width/2), minimumY+int64(depth/2)
			removeRectangle(centerX-1, centerY-1, centerX+1, centerY+1)
		}
	case "rowhouse", "shopfront", "walkup":
		cutCorner(int64(maxWorldgenInt(2, width/3)), int64(maxWorldgenInt(2, depth/3)))
	case "workshop", "loading_depot":
		cutCorner(int64(maxWorldgenInt(2, width/3)), int64(maxWorldgenInt(2, depth/2)))
	case "tower":
		corner := int64(maxWorldgenInt(1, minWorldgenInt(width, depth)/5))
		removeRectangle(minimumCornerX, minimumCornerY, minimumCornerX+corner-1, minimumCornerY+corner-1)
		removeRectangle(maximumCornerX-corner+1, minimumCornerY, maximumCornerX, minimumCornerY+corner-1)
		removeRectangle(minimumCornerX, maximumCornerY-corner+1, minimumCornerX+corner-1, maximumCornerY)
		removeRectangle(maximumCornerX-corner+1, maximumCornerY-corner+1, maximumCornerX, maximumCornerY)
	case "arcade":
		if width >= 8 {
			for offset := int64(2); offset < int64(width)-2; offset += 3 {
				if frontage == "north" || frontage == "south" {
					y := minimumY
					if frontage == "south" {
						y = minimumY + int64(depth) - 1
					}
					remove(minimumX+offset, y)
				} else {
					x := minimumX
					if frontage == "east" {
						x = minimumX + int64(width) - 1
					}
					remove(x, minimumY+offset%int64(depth))
				}
			}
		}
		if width >= 10 && depth >= 10 {
			centerX, centerY := minimumX+int64(width/2), minimumY+int64(depth/2)
			removeRectangle(centerX-1, centerY-1, centerX+1, centerY+1)
		}
	}
}

func worldgenBuildingEntrance(
	footprint map[WorldgenPoint]struct{},
	frontage string,
	minimumX, minimumY int64,
	width, depth int,
	z int32,
) WorldgenPoint {
	point := WorldgenPoint{X: minimumX + int64(width/2), Y: minimumY + int64(depth/2), Z: z}
	switch frontage {
	case "north":
		point.Y = minimumY
	case "south":
		point.Y = minimumY + int64(depth) - 1
	case "west":
		point.X = minimumX
	case "east":
		point.X = minimumX + int64(width) - 1
	}
	if _, present := footprint[point]; present {
		return point
	}
	if frontage == "north" || frontage == "south" {
		for x := minimumX; x < minimumX+int64(width); x++ {
			candidate := WorldgenPoint{X: x, Y: point.Y, Z: z}
			if _, present := footprint[candidate]; present {
				return candidate
			}
		}
	} else {
		for y := minimumY; y < minimumY+int64(depth); y++ {
			candidate := WorldgenPoint{X: point.X, Y: y, Z: z}
			if _, present := footprint[candidate]; present {
				return candidate
			}
		}
	}
	return point
}

func worldgenSelectLotUse(
	binding WorldgenBinding,
	profile WorldgenProfile,
	city GeneratedWorldgenCity,
	bounds WorldgenRectangle,
) (LandUse, string) {
	centerX := (bounds.MinimumX + bounds.MaximumX) / 2
	centerY := (bounds.MinimumY + bounds.MaximumY) / 2
	distance := worldgenAbs(centerX-city.Center.X) + worldgenAbs(centerY-city.Center.Y)
	radius := int64(maxWorldgenInt(1, city.RadiusChunks)) * DefaultChunkSize
	distanceMilli := int((distance * 1000) / radius)
	if distanceMilli > 1000 {
		distanceMilli = 1000
	}
	district := profile.DistrictRules[len(profile.DistrictRules)-1]
	for _, candidate := range profile.DistrictRules {
		if distanceMilli >= candidate.MinimumDistanceMilli && distanceMilli <= candidate.MaximumDistanceMilli {
			district = candidate
			break
		}
	}
	totalWeight := 0
	for _, useWeight := range district.UseWeights {
		totalWeight += useWeight.Weight
	}
	target := int(worldgenDraw(binding, "lot.use/"+city.Code+"/"+district.Code, centerX, centerY) % uint64(totalWeight))
	for _, useWeight := range district.UseWeights {
		if target < useWeight.Weight {
			return useWeight.PrimaryUse, district.Code
		}
		target -= useWeight.Weight
	}
	last := district.UseWeights[len(district.UseWeights)-1]
	return last.PrimaryUse, district.Code
}

func worldgenRoadCells(bounds WorldgenBounds, roads []GeneratedWorldgenRoad) map[WorldgenPoint]string {
	result := make(map[WorldgenPoint]string)
	for _, road := range roads {
		for index := 1; index < len(road.Points); index++ {
			start, end := road.Points[index-1], road.Points[index]
			if start.X != end.X && start.Y != end.Y {
				continue
			}
			minimum, maximum := start.X, end.X
			if start.X == end.X {
				minimum, maximum = start.Y, end.Y
			}
			if minimum > maximum {
				minimum, maximum = maximum, minimum
			}
			for coordinate := minimum; coordinate <= maximum; coordinate++ {
				for offset := -road.Width / 2; offset <= (road.Width-1)/2; offset++ {
					point := WorldgenPoint{Z: bounds.Z}
					if start.X == end.X {
						point.X, point.Y = start.X+int64(offset), coordinate
					} else {
						point.X, point.Y = coordinate, start.Y+int64(offset)
					}
					if worldgenPointInBounds(bounds, point) {
						result[point] = road.Code
					}
				}
			}
		}
	}
	return result
}

func worldgenRectangleAvailable(rectangle WorldgenRectangle, roadCells, lotCells map[WorldgenPoint]string) bool {
	for y := rectangle.MinimumY; y <= rectangle.MaximumY; y++ {
		for x := rectangle.MinimumX; x <= rectangle.MaximumX; x++ {
			point := WorldgenPoint{X: x, Y: y, Z: rectangle.Z}
			if _, road := roadCells[point]; road {
				return false
			}
			if _, occupied := lotCells[point]; occupied {
				return false
			}
		}
	}
	return true
}

func worldgenReserveRectangle(rectangle WorldgenRectangle, cells map[WorldgenPoint]string, code string) {
	for y := rectangle.MinimumY; y <= rectangle.MaximumY; y++ {
		for x := rectangle.MinimumX; x <= rectangle.MaximumX; x++ {
			cells[WorldgenPoint{X: x, Y: y, Z: rectangle.Z}] = code
		}
	}
}

func validateGeneratedWorldgenPlan(plan *GeneratedWorldgenPlan) error {
	if plan == nil || validateWorldgenBounds(plan.Bounds) != nil ||
		validateWorldgenBinding(plan.Binding, plan.Profile) != nil {
		return ErrInvalidWorldgenInput
	}
	expectedTerrain := int((plan.Bounds.MaximumChunkX - plan.Bounds.MinimumChunkX + 1) * (plan.Bounds.MaximumChunkY - plan.Bounds.MinimumChunkY + 1))
	if len(plan.Terrain) != expectedTerrain || len(plan.Cities) == 0 {
		return ErrInvalidWorldgenInput
	}
	terrainCoordinates := make(map[string]struct{}, len(plan.Terrain))
	terrainByChunk := make(map[WorldgenPoint]GeneratedWorldgenTerrainPatch, len(plan.Terrain))
	for _, terrain := range plan.Terrain {
		if !worldgenChunkInBounds(plan.Bounds, terrain.ChunkX, terrain.ChunkY, terrain.Z) || terrain.BiomeCode == "" || terrain.DefinitionID == "" ||
			terrain.ElevationMilli < 0 || terrain.ElevationMilli > 1000 || terrain.MoistureMilli < 0 || terrain.MoistureMilli > 1000 {
			return ErrInvalidWorldgenInput
		}
		key := fmt.Sprintf("%d/%d/%d", terrain.ChunkX, terrain.ChunkY, terrain.Z)
		if _, duplicate := terrainCoordinates[key]; duplicate {
			return ErrInvalidWorldgenInput
		}
		terrainCoordinates[key] = struct{}{}
		terrainByChunk[WorldgenPoint{X: terrain.ChunkX, Y: terrain.ChunkY, Z: terrain.Z}] = terrain
	}
	cities := make(map[string]GeneratedWorldgenCity, len(plan.Cities))
	for _, city := range plan.Cities {
		if city.Code == "" || city.RadiusChunks <= 0 || !worldgenPointInBounds(plan.Bounds, city.Center) ||
			(city.PlacementMode != "preferred" && city.PlacementMode != "fallback") {
			return ErrInvalidWorldgenInput
		}
		terrain, found := terrainByChunk[WorldgenPoint{
			X: worldgenChunkFromWorld(city.Center.X), Y: worldgenChunkFromWorld(city.Center.Y), Z: city.Center.Z,
		}]
		if !found || city.BiomeCode != terrain.BiomeCode || city.ElevationMilli != terrain.ElevationMilli || city.MoistureMilli != terrain.MoistureMilli {
			return ErrInvalidWorldgenInput
		}
		if _, duplicate := cities[city.Code]; duplicate {
			return ErrInvalidWorldgenInput
		}
		cities[city.Code] = city
	}
	roads := make(map[string]GeneratedWorldgenRoad, len(plan.Roads))
	roadCells := worldgenRoadCells(plan.Bounds, plan.Roads)
	for _, road := range plan.Roads {
		if road.Code == "" || road.Width <= 0 || len(road.Points) < 2 || road.Class != WorldgenRoadArterial && road.Class != WorldgenRoadLocal {
			return ErrInvalidWorldgenInput
		}
		if _, found := cities[road.CityCode]; !found {
			return ErrInvalidWorldgenInput
		}
		if _, duplicate := roads[road.Code]; duplicate {
			return ErrInvalidWorldgenInput
		}
		for index, point := range road.Points {
			if !worldgenPointInBounds(plan.Bounds, point) {
				return ErrInvalidWorldgenInput
			}
			if index > 0 && point.X != road.Points[index-1].X && point.Y != road.Points[index-1].Y {
				return ErrInvalidWorldgenInput
			}
		}
		roads[road.Code] = road
	}
	districts := make(map[string]WorldgenDistrictRule, len(plan.Profile.DistrictRules))
	for _, district := range plan.Profile.DistrictRules {
		districts[district.Code] = district
	}
	archetypes := make(map[string]WorldgenBuildingArchetype, len(plan.Profile.BuildingArchetypes))
	for _, archetype := range plan.Profile.BuildingArchetypes {
		archetypes[archetype.Code] = archetype
	}
	lotCells := make(map[WorldgenPoint]string)
	lots := make(map[string]GeneratedWorldgenLot, len(plan.Lots))
	for _, lot := range plan.Lots {
		if lot.Code == "" || !worldgenRectangleWithinBounds(lot.Bounds, plan.Bounds) ||
			lot.FrontageDirection != "north" && lot.FrontageDirection != "east" && lot.FrontageDirection != "south" && lot.FrontageDirection != "west" {
			return ErrInvalidWorldgenInput
		}
		if _, found := cities[lot.CityCode]; !found {
			return ErrInvalidWorldgenInput
		}
		road, found := roads[lot.FrontageRoadCode]
		if !found || road.CityCode != lot.CityCode {
			return ErrInvalidWorldgenInput
		}
		district, found := districts[lot.DistrictCode]
		if !found || !worldgenDistrictAllowsUse(district, lot.PrimaryUse) {
			return ErrInvalidWorldgenInput
		}
		if _, duplicate := lots[lot.Code]; duplicate || !worldgenRectangleAvailable(lot.Bounds, roadCells, lotCells) {
			return ErrInvalidWorldgenInput
		}
		worldgenReserveRectangle(lot.Bounds, lotCells, lot.Code)
		lots[lot.Code] = lot
	}
	buildings := make(map[string]struct{}, len(plan.Buildings))
	buildingCountByLot := make(map[string]int, len(plan.Lots))
	buildingCells := make(map[WorldgenPoint]string)
	for _, building := range plan.Buildings {
		lot, found := lots[building.LotCode]
		if !found || building.Code == "" || building.CityCode != lot.CityCode || building.PrimaryUse != lot.PrimaryUse ||
			building.ArchetypeCode == "" || building.LayoutStyle == "" || building.FloorCount <= 0 || len(building.Footprint) == 0 {
			return ErrInvalidWorldgenInput
		}
		if _, duplicate := buildings[building.Code]; duplicate {
			return ErrInvalidWorldgenInput
		}
		archetype, found := archetypes[building.ArchetypeCode]
		if !found || archetype.PrimaryUse != building.PrimaryUse || archetype.LayoutStyle != building.LayoutStyle || !worldgenArchetypeAllowsDistrict(archetype, lot.DistrictCode) ||
			building.FloorCount < archetype.MinimumFloors || building.FloorCount > archetype.MaximumFloors {
			return ErrInvalidWorldgenInput
		}
		buildings[building.Code] = struct{}{}
		buildingCountByLot[building.LotCode]++
		entranceFound := false
		for _, point := range building.Footprint {
			if !worldgenPointInRectangle(point, lot.Bounds) {
				return ErrInvalidWorldgenInput
			}
			if _, duplicate := buildingCells[point]; duplicate {
				return ErrInvalidWorldgenInput
			}
			buildingCells[point] = building.Code
			if point == building.Entrance {
				entranceFound = true
			}
		}
		if !entranceFound {
			return ErrInvalidWorldgenInput
		}
	}
	for lotCode := range lots {
		if buildingCountByLot[lotCode] != 1 {
			return ErrInvalidWorldgenInput
		}
	}
	return nil
}

func worldgenDistrictAllowsUse(district WorldgenDistrictRule, primaryUse LandUse) bool {
	for _, useWeight := range district.UseWeights {
		if useWeight.PrimaryUse == primaryUse {
			return true
		}
	}
	return false
}

func worldgenField(binding WorldgenBinding, namespace string, x, y int64) int {
	// A small deterministic value-noise blend gives coherent terrain without a
	// floating-point dependency or hidden global random state.
	fine := int(worldgenDraw(binding, namespace+"/fine", x, y) % 1001)
	coarseX, coarseY := worldgenFloorDiv(x, 3), worldgenFloorDiv(y, 3)
	coarse := int(worldgenDraw(binding, namespace+"/coarse", coarseX, coarseY) % 1001)
	return (fine*35 + coarse*65) / 100
}

func worldgenDraw(binding WorldgenBinding, namespace string, x, y int64) uint64 {
	hasher := sha256.New()
	writeGenerationString(hasher, binding.SimulationVersion)
	writeGenerationInt64(hasher, binding.WorldSeed)
	writeGenerationString(hasher, binding.SpatialRootHash)
	writeGenerationString(hasher, binding.ProfileID)
	writeGenerationString(hasher, binding.ProfileVersion)
	writeGenerationString(hasher, binding.ProfileHash)
	writeGenerationString(hasher, binding.GeneratorID)
	writeGenerationString(hasher, binding.GeneratorVersion)
	writeGenerationString(hasher, namespace)
	writeGenerationInt64(hasher, x)
	writeGenerationInt64(hasher, y)
	var value [8]byte
	copy(value[:], hasher.Sum(nil))
	return binary.BigEndian.Uint64(value[:])
}

func worldgenOrthogonalRoute(start, end WorldgenPoint, horizontalFirst bool) []WorldgenPoint {
	if start == end {
		return []WorldgenPoint{start}
	}
	points := []WorldgenPoint{start}
	if start.X != end.X && start.Y != end.Y {
		bend := WorldgenPoint{Z: start.Z}
		if horizontalFirst {
			bend.X, bend.Y = end.X, start.Y
		} else {
			bend.X, bend.Y = start.X, end.Y
		}
		points = append(points, bend)
	}
	points = append(points, end)
	return points
}

func worldgenRoadDirection(direction int) (int64, int64) {
	switch direction {
	case 0:
		return 0, -1
	case 1:
		return 1, 0
	case 2:
		return 0, 1
	default:
		return -1, 0
	}
}

func worldgenLocalStreetPoints(
	bounds WorldgenBounds,
	origin WorldgenPoint,
	directionX, directionY int64,
	side, lateralLength, hookLength int,
	hookForward bool,
) []WorldgenPoint {
	lateral := worldgenClampWorldPoint(bounds, WorldgenPoint{
		X: origin.X - directionY*int64(side*lateralLength)*DefaultChunkSize,
		Y: origin.Y + directionX*int64(side*lateralLength)*DefaultChunkSize,
		Z: origin.Z,
	})
	if lateral == origin {
		return nil
	}
	points := []WorldgenPoint{origin, lateral}
	hookDirection := int64(hookLength)
	if !hookForward {
		hookDirection = -hookDirection
	}
	hook := worldgenClampWorldPoint(bounds, WorldgenPoint{
		X: lateral.X + directionX*hookDirection*DefaultChunkSize,
		Y: lateral.Y + directionY*hookDirection*DefaultChunkSize,
		Z: lateral.Z,
	})
	if hook != lateral {
		points = append(points, hook)
	}
	return points
}

func worldgenChunkCenter(chunkX, chunkY int64, z int32) WorldgenPoint {
	return WorldgenPoint{X: chunkX*DefaultChunkSize + DefaultChunkSize/2, Y: chunkY*DefaultChunkSize + DefaultChunkSize/2, Z: z}
}

func worldgenChunkFromWorld(value int64) int64 {
	return worldgenFloorDiv(value, DefaultChunkSize)
}

func worldgenFloorDiv(value, divisor int64) int64 {
	if value >= 0 {
		return value / divisor
	}
	return -((-value + divisor - 1) / divisor)
}

func worldgenNormalizeStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func worldgenPointInBounds(bounds WorldgenBounds, point WorldgenPoint) bool {
	return point.Z == bounds.Z && point.X >= bounds.MinimumChunkX*DefaultChunkSize &&
		point.X <= (bounds.MaximumChunkX+1)*DefaultChunkSize-1 &&
		point.Y >= bounds.MinimumChunkY*DefaultChunkSize &&
		point.Y <= (bounds.MaximumChunkY+1)*DefaultChunkSize-1
}

func worldgenClampWorldPoint(bounds WorldgenBounds, point WorldgenPoint) WorldgenPoint {
	point.X = worldgenClampInt64(point.X, bounds.MinimumChunkX*DefaultChunkSize, (bounds.MaximumChunkX+1)*DefaultChunkSize-1)
	point.Y = worldgenClampInt64(point.Y, bounds.MinimumChunkY*DefaultChunkSize, (bounds.MaximumChunkY+1)*DefaultChunkSize-1)
	point.Z = bounds.Z
	return point
}

func worldgenChunkInBounds(bounds WorldgenBounds, x, y int64, z int32) bool {
	return z == bounds.Z && x >= bounds.MinimumChunkX && x <= bounds.MaximumChunkX && y >= bounds.MinimumChunkY && y <= bounds.MaximumChunkY
}

func worldgenRectangleWithinBounds(rectangle WorldgenRectangle, bounds WorldgenBounds) bool {
	return rectangle.Z == bounds.Z && rectangle.MinimumX <= rectangle.MaximumX && rectangle.MinimumY <= rectangle.MaximumY &&
		worldgenPointInBounds(bounds, WorldgenPoint{X: rectangle.MinimumX, Y: rectangle.MinimumY, Z: rectangle.Z}) &&
		worldgenPointInBounds(bounds, WorldgenPoint{X: rectangle.MaximumX, Y: rectangle.MaximumY, Z: rectangle.Z})
}

func worldgenPointInRectangle(point WorldgenPoint, rectangle WorldgenRectangle) bool {
	return point.Z == rectangle.Z && point.X >= rectangle.MinimumX && point.X <= rectangle.MaximumX &&
		point.Y >= rectangle.MinimumY && point.Y <= rectangle.MaximumY
}

func worldgenRectangleIntersectsBounds(rectangle WorldgenRectangle, bounds WorldgenBounds) bool {
	if rectangle.Z != bounds.Z {
		return false
	}
	minimumX, maximumX := bounds.MinimumChunkX*DefaultChunkSize, (bounds.MaximumChunkX+1)*DefaultChunkSize-1
	minimumY, maximumY := bounds.MinimumChunkY*DefaultChunkSize, (bounds.MaximumChunkY+1)*DefaultChunkSize-1
	return rectangle.MinimumX <= maximumX && rectangle.MaximumX >= minimumX && rectangle.MinimumY <= maximumY && rectangle.MaximumY >= minimumY
}

func worldgenFootprintIntersectsBounds(footprint []WorldgenPoint, bounds WorldgenBounds) bool {
	for _, point := range footprint {
		if worldgenPointInBounds(bounds, point) {
			return true
		}
	}
	return false
}

func worldgenRoadIntersectsBounds(road GeneratedWorldgenRoad, bounds WorldgenBounds) bool {
	for index := 1; index < len(road.Points); index++ {
		start, end := road.Points[index-1], road.Points[index]
		if start.Z != bounds.Z || end.Z != bounds.Z {
			continue
		}
		if start.X == end.X {
			minimumY, maximumY := start.Y, end.Y
			if minimumY > maximumY {
				minimumY, maximumY = maximumY, minimumY
			}
			if start.X >= bounds.MinimumChunkX*DefaultChunkSize && start.X <= (bounds.MaximumChunkX+1)*DefaultChunkSize-1 &&
				maximumY >= bounds.MinimumChunkY*DefaultChunkSize && minimumY <= (bounds.MaximumChunkY+1)*DefaultChunkSize-1 {
				return true
			}
		} else if start.Y == end.Y {
			minimumX, maximumX := start.X, end.X
			if minimumX > maximumX {
				minimumX, maximumX = maximumX, minimumX
			}
			if start.Y >= bounds.MinimumChunkY*DefaultChunkSize && start.Y <= (bounds.MaximumChunkY+1)*DefaultChunkSize-1 &&
				maximumX >= bounds.MinimumChunkX*DefaultChunkSize && minimumX <= (bounds.MaximumChunkX+1)*DefaultChunkSize-1 {
				return true
			}
		}
	}
	return false
}

func worldgenRectangleWidth(rectangle WorldgenRectangle) int {
	return int(rectangle.MaximumX - rectangle.MinimumX + 1)
}

func worldgenRectangleHeight(rectangle WorldgenRectangle) int {
	return int(rectangle.MaximumY - rectangle.MinimumY + 1)
}

func worldgenAbs(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}

func worldgenClampInt64(value, minimum, maximum int64) int64 {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

func maxWorldgenInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func minWorldgenInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func cloneWorldgenRoad(source GeneratedWorldgenRoad) GeneratedWorldgenRoad {
	source.Points = append([]WorldgenPoint(nil), source.Points...)
	return source
}

func cloneWorldgenBuilding(source GeneratedWorldgenBuilding) GeneratedWorldgenBuilding {
	source.Footprint = append([]WorldgenPoint(nil), source.Footprint...)
	return source
}
