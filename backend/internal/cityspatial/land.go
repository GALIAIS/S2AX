package cityspatial

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
)

const (
	DefaultLandRuleSetID      = "sub2api-land"
	DefaultLandRuleSetVersion = "1.0.0"
	DefaultNominalCellAreaSQM = int64(1500)
)

type LandUse string

const (
	LandUseResidential LandUse = "residential"
	LandUseCommercial  LandUse = "commercial"
	LandUseIndustrial  LandUse = "industrial"
)

var (
	ErrInvalidLandRuleSet   = errors.New("invalid city land rule set")
	ErrInvalidLandInput     = errors.New("invalid city land generation input")
	ErrLandCapacityOverflow = errors.New("city land capacity exceeds zoning envelope")
)

type LandZoningRule struct {
	Code                   string  `json:"code"`
	Name                   string  `json:"name"`
	PrimaryUse             LandUse `json:"primary_use"`
	MaxFloorAreaRatioMilli int64   `json:"max_floor_area_ratio_milli"`
	MaxCoverageMilli       int64   `json:"max_coverage_milli"`
	MaxFloors              int32   `json:"max_floors"`
	SQMPerCapacityUnit     int64   `json:"sqm_per_capacity_unit"`
}

type LandRuleSet struct {
	ID                 string           `json:"id"`
	Version            string           `json:"version"`
	Name               string           `json:"name"`
	NominalCellAreaSQM int64            `json:"nominal_cell_area_sqm"`
	Rules              []LandZoningRule `json:"rules"`
	ContentHash        string           `json:"content_hash"`
}

type LandGeneratorBinding struct {
	SimulationVersion  string `json:"simulation_version"`
	WorldSeed          int64  `json:"world_seed"`
	SpatialRuleSetHash string `json:"spatial_rule_set_hash"`
	OvermapRootHash    string `json:"overmap_root_hash"`
	LandRuleSetID      string `json:"land_rule_set_id"`
	LandRuleSetVersion string `json:"land_rule_set_version"`
	LandRuleSetHash    string `json:"land_rule_set_hash"`
}

type HouseholdLandSeed struct {
	EntityCode     string `json:"entity_code"`
	IncomeBand     string `json:"income_band"`
	HouseholdUnits int64  `json:"household_units"`
}

type DistrictLandSeed struct {
	Code                     string              `json:"code"`
	SortOrder                int                 `json:"sort_order"`
	AreaSQM                  int64               `json:"area_sqm"`
	DevelopableAreaSQM       int64               `json:"developable_area_sqm"`
	ResidentialCapacityUnits int64               `json:"residential_capacity_units"`
	CommercialCapacityUnits  int64               `json:"commercial_capacity_units"`
	IndustrialCapacityUnits  int64               `json:"industrial_capacity_units"`
	Households               []HouseholdLandSeed `json:"households"`
}

type LandRectangle struct {
	ChunkX    int64 `json:"chunk_x"`
	ChunkY    int64 `json:"chunk_y"`
	Z         int32 `json:"z"`
	LocalMinX int32 `json:"local_min_x"`
	LocalMinY int32 `json:"local_min_y"`
	LocalMaxX int32 `json:"local_max_x"`
	LocalMaxY int32 `json:"local_max_y"`
}

type GeneratedParcel struct {
	Code               string        `json:"code"`
	DistrictCode       string        `json:"district_code"`
	ZoneCode           string        `json:"zone_code"`
	Geometry           LandRectangle `json:"geometry"`
	AreaSQM            int64         `json:"area_sqm"`
	DevelopableAreaSQM int64         `json:"developable_area_sqm"`
	Status             string        `json:"status"`
	Version            int64         `json:"version"`
}

type GeneratedBuilding struct {
	Code             string        `json:"code"`
	ParcelCode       string        `json:"parcel_code"`
	DistrictCode     string        `json:"district_code"`
	PrimaryUse       LandUse       `json:"primary_use"`
	Footprint        LandRectangle `json:"footprint"`
	BaseZ            int32         `json:"base_z"`
	TopZ             int32         `json:"top_z"`
	FloorCount       int32         `json:"floor_count"`
	FootprintAreaSQM int64         `json:"footprint_area_sqm"`
	FloorAreaSQM     int64         `json:"floor_area_sqm"`
	CapacityUnits    int64         `json:"capacity_units"`
	OccupiedUnits    int64         `json:"occupied_units"`
	QualityMilli     int64         `json:"quality_milli"`
	Status           string        `json:"status"`
	CompletedTick    int64         `json:"completed_tick"`
	Version          int64         `json:"version"`
}

type GeneratedBuildingUnitPool struct {
	Code                 string  `json:"code"`
	BuildingCode         string  `json:"building_code"`
	DistrictCode         string  `json:"district_code"`
	UseType              LandUse `json:"use_type"`
	UnitCount            int64   `json:"unit_count"`
	OccupiedUnitCount    int64   `json:"occupied_unit_count"`
	CapacityUnitsPerUnit int64   `json:"capacity_units_per_unit"`
	Version              int64   `json:"version"`
}

type GeneratedHousingAllocation struct {
	PoolCode       string `json:"pool_code"`
	DistrictCode   string `json:"district_code"`
	CohortKey      string `json:"cohort_key"`
	AllocatedUnits int64  `json:"allocated_units"`
	Status         string `json:"status"`
	Version        int64  `json:"version"`
}

type GeneratedBuildingPortal struct {
	Code          string `json:"code"`
	BuildingCode  string `json:"building_code"`
	DistrictCode  string `json:"district_code"`
	PortalType    string `json:"portal_type"`
	FromX         int64  `json:"from_x"`
	FromY         int64  `json:"from_y"`
	FromZ         int32  `json:"from_z"`
	ToX           int64  `json:"to_x"`
	ToY           int64  `json:"to_y"`
	ToZ           int32  `json:"to_z"`
	Bidirectional bool   `json:"bidirectional"`
	Status        string `json:"status"`
	Version       int64  `json:"version"`
}

type GeneratedLandFoundation struct {
	Binding            LandGeneratorBinding         `json:"binding"`
	RuleSet            LandRuleSet                  `json:"rule_set"`
	Parcels            []GeneratedParcel            `json:"parcels"`
	Buildings          []GeneratedBuilding          `json:"buildings"`
	UnitPools          []GeneratedBuildingUnitPool  `json:"unit_pools"`
	HousingAllocations []GeneratedHousingAllocation `json:"housing_allocations"`
	Portals            []GeneratedBuildingPortal    `json:"portals"`
	BaselineHash       string                       `json:"baseline_hash"`
}

type landQuadrant struct {
	index        int
	use          LandUse
	parcelMinX   int32
	parcelMinY   int32
	parcelMaxX   int32
	parcelMaxY   int32
	buildingMinX int32
	buildingMinY int32
	buildingMaxX int32
	buildingMaxY int32
}

var defaultLandQuadrants = []landQuadrant{
	{index: 0, use: LandUseResidential, parcelMinX: 1, parcelMinY: 1, parcelMaxX: 12, parcelMaxY: 12, buildingMinX: 4, buildingMinY: 4, buildingMaxX: 9, buildingMaxY: 9},
	{index: 1, use: LandUseResidential, parcelMinX: 19, parcelMinY: 1, parcelMaxX: 30, parcelMaxY: 12, buildingMinX: 22, buildingMinY: 4, buildingMaxX: 27, buildingMaxY: 9},
	{index: 2, use: LandUseCommercial, parcelMinX: 1, parcelMinY: 19, parcelMaxX: 12, parcelMaxY: 30, buildingMinX: 4, buildingMinY: 22, buildingMaxX: 9, buildingMaxY: 27},
	{index: 3, use: LandUseIndustrial, parcelMinX: 19, parcelMinY: 19, parcelMaxX: 30, parcelMaxY: 30, buildingMinX: 22, buildingMinY: 22, buildingMaxX: 27, buildingMaxY: 27},
}

func DefaultLandRuleSet() (*LandRuleSet, error) {
	rules := []LandZoningRule{
		{Code: string(LandUseCommercial), Name: "Commercial", PrimaryUse: LandUseCommercial, MaxFloorAreaRatioMilli: 4000, MaxCoverageMilli: 600, MaxFloors: 16, SQMPerCapacityUnit: 25},
		{Code: string(LandUseIndustrial), Name: "Industrial", PrimaryUse: LandUseIndustrial, MaxFloorAreaRatioMilli: 1500, MaxCoverageMilli: 700, MaxFloors: 4, SQMPerCapacityUnit: 40},
		{Code: string(LandUseResidential), Name: "Residential", PrimaryUse: LandUseResidential, MaxFloorAreaRatioMilli: 3000, MaxCoverageMilli: 450, MaxFloors: 12, SQMPerCapacityUnit: 90},
	}
	ruleSet := &LandRuleSet{
		ID: DefaultLandRuleSetID, Version: DefaultLandRuleSetVersion,
		Name: "Sub2API Land Foundation", NominalCellAreaSQM: DefaultNominalCellAreaSQM,
		Rules: rules,
	}
	if err := validateLandRuleSet(ruleSet); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(struct {
		ID                 string           `json:"id"`
		Version            string           `json:"version"`
		Name               string           `json:"name"`
		NominalCellAreaSQM int64            `json:"nominal_cell_area_sqm"`
		Rules              []LandZoningRule `json:"rules"`
	}{ruleSet.ID, ruleSet.Version, ruleSet.Name, ruleSet.NominalCellAreaSQM, ruleSet.Rules})
	if err != nil {
		return nil, fmt.Errorf("marshal city land rule set: %w", err)
	}
	ruleSet.ContentHash = sha256Hex(raw)
	return ruleSet, nil
}

func DefaultLandGeneratorBinding(
	simulationVersion string,
	worldSeed int64,
	spatialRuleSetHash, overmapRootHash string,
	ruleSet *LandRuleSet,
) (LandGeneratorBinding, error) {
	if ruleSet == nil || validateLandRuleSet(ruleSet) != nil ||
		strings.TrimSpace(simulationVersion) == "" || worldSeed <= 0 ||
		!validSHA256Hex(spatialRuleSetHash) || !validSHA256Hex(overmapRootHash) ||
		!validSHA256Hex(ruleSet.ContentHash) {
		return LandGeneratorBinding{}, ErrInvalidLandInput
	}
	return LandGeneratorBinding{
		SimulationVersion: strings.TrimSpace(simulationVersion), WorldSeed: worldSeed,
		SpatialRuleSetHash: spatialRuleSetHash, OvermapRootHash: overmapRootHash,
		LandRuleSetID: ruleSet.ID, LandRuleSetVersion: ruleSet.Version,
		LandRuleSetHash: ruleSet.ContentHash,
	}, nil
}

func GenerateDefaultLandFoundation(
	binding LandGeneratorBinding,
	ruleSet *LandRuleSet,
	overmap *Overmap,
	districts []DistrictLandSeed,
) (*GeneratedLandFoundation, error) {
	if err := validateLandGenerationInput(binding, ruleSet, overmap, districts); err != nil {
		return nil, err
	}
	districtByCode := make(map[string]DistrictLandSeed, len(districts))
	for _, district := range districts {
		district.Households = append([]HouseholdLandSeed(nil), district.Households...)
		sort.Slice(district.Households, func(i, j int) bool {
			left, right := district.Households[i], district.Households[j]
			if incomeBandOrder(left.IncomeBand) != incomeBandOrder(right.IncomeBand) {
				return incomeBandOrder(left.IncomeBand) < incomeBandOrder(right.IncomeBand)
			}
			return left.EntityCode < right.EntityCode
		})
		districtByCode[district.Code] = district
	}

	foundation := &GeneratedLandFoundation{
		Binding: binding, RuleSet: *cloneLandRuleSet(ruleSet),
		Parcels:            make([]GeneratedParcel, 0, len(overmap.Tiles)*4),
		Buildings:          make([]GeneratedBuilding, 0, len(overmap.Tiles)*4),
		UnitPools:          make([]GeneratedBuildingUnitPool, 0, len(overmap.Tiles)*4),
		HousingAllocations: make([]GeneratedHousingAllocation, 0),
		Portals:            make([]GeneratedBuildingPortal, 0, len(overmap.Tiles)*4),
	}

	for _, tile := range overmap.Tiles {
		if tile.Z != SurfaceZ || tile.RiverMask != 0 || tile.TerrainID == "terrain.deep_water" {
			continue
		}
		for _, quadrant := range defaultLandQuadrants {
			code := landObjectCode("parcel", tile.DistrictCode, tile.ChunkX, tile.ChunkY, quadrant.index)
			foundation.Parcels = append(foundation.Parcels, GeneratedParcel{
				Code: code, DistrictCode: tile.DistrictCode, ZoneCode: string(quadrant.use),
				Geometry: LandRectangle{
					ChunkX: tile.ChunkX, ChunkY: tile.ChunkY, Z: tile.Z,
					LocalMinX: quadrant.parcelMinX, LocalMinY: quadrant.parcelMinY,
					LocalMaxX: quadrant.parcelMaxX, LocalMaxY: quadrant.parcelMaxY,
				},
				Status: "active", Version: 1,
			})
		}
	}
	sort.Slice(foundation.Parcels, func(i, j int) bool { return foundation.Parcels[i].Code < foundation.Parcels[j].Code })

	ruleByUse := make(map[LandUse]LandZoningRule, len(ruleSet.Rules))
	for _, rule := range ruleSet.Rules {
		ruleByUse[rule.PrimaryUse] = rule
	}
	for _, district := range districts {
		parcelIndexes := parcelIndexesForDistrict(foundation.Parcels, district.Code, "")
		if len(parcelIndexes) == 0 {
			return nil, fmt.Errorf("%w: district %s has no developable parcel", ErrInvalidLandInput, district.Code)
		}
		areas := distributeLandUnits(district.DevelopableAreaSQM, len(parcelIndexes))
		for offset, parcelIndex := range parcelIndexes {
			foundation.Parcels[parcelIndex].AreaSQM = areas[offset]
			foundation.Parcels[parcelIndex].DevelopableAreaSQM = areas[offset]
		}
		capacities := map[LandUse]int64{
			LandUseResidential: district.ResidentialCapacityUnits,
			LandUseCommercial:  district.CommercialCapacityUnits,
			LandUseIndustrial:  district.IndustrialCapacityUnits,
		}
		for _, use := range []LandUse{LandUseResidential, LandUseCommercial, LandUseIndustrial} {
			useParcelIndexes := parcelIndexesForDistrict(foundation.Parcels, district.Code, string(use))
			if capacities[use] > 0 && len(useParcelIndexes) == 0 {
				return nil, fmt.Errorf("%w: district %s lacks %s parcels", ErrInvalidLandInput, district.Code, use)
			}
			allocated := distributeLandUnits(capacities[use], len(useParcelIndexes))
			for offset, parcelIndex := range useParcelIndexes {
				if allocated[offset] == 0 {
					continue
				}
				parcel := foundation.Parcels[parcelIndex]
				quadrant, ok := quadrantForParcel(parcel)
				if !ok {
					return nil, ErrInvalidLandInput
				}
				building, pool, portals, err := generateLandBuilding(parcel, quadrant, allocated[offset], ruleByUse[use])
				if err != nil {
					return nil, err
				}
				foundation.Buildings = append(foundation.Buildings, building)
				foundation.UnitPools = append(foundation.UnitPools, pool)
				foundation.Portals = append(foundation.Portals, portals...)
			}
		}
	}
	sort.Slice(foundation.Buildings, func(i, j int) bool { return foundation.Buildings[i].Code < foundation.Buildings[j].Code })
	sort.Slice(foundation.UnitPools, func(i, j int) bool { return foundation.UnitPools[i].Code < foundation.UnitPools[j].Code })
	sort.Slice(foundation.Portals, func(i, j int) bool {
		left, right := foundation.Portals[i], foundation.Portals[j]
		if left.BuildingCode != right.BuildingCode {
			return left.BuildingCode < right.BuildingCode
		}
		if left.FromZ != right.FromZ {
			return left.FromZ < right.FromZ
		}
		return left.Code < right.Code
	})
	if err := allocateFoundationHousing(foundation, districts); err != nil {
		return nil, err
	}
	if err := validateGeneratedLandFoundation(foundation, districtByCode); err != nil {
		return nil, err
	}
	baselineHash, err := ComputeLandFoundationBaselineHash(foundation)
	if err != nil {
		return nil, err
	}
	foundation.BaselineHash = baselineHash
	return foundation, nil
}

// ComputeLandFoundationBaselineHash returns the canonical hash used by both
// generation and persisted-projection verification. Database identities and
// timestamps are deliberately absent from this immutable format.
func ComputeLandFoundationBaselineHash(foundation *GeneratedLandFoundation) (string, error) {
	if foundation == nil {
		return "", ErrInvalidLandInput
	}
	raw, err := json.Marshal(struct {
		Binding            LandGeneratorBinding         `json:"binding"`
		RuleSet            LandRuleSet                  `json:"rule_set"`
		Parcels            []GeneratedParcel            `json:"parcels"`
		Buildings          []GeneratedBuilding          `json:"buildings"`
		UnitPools          []GeneratedBuildingUnitPool  `json:"unit_pools"`
		HousingAllocations []GeneratedHousingAllocation `json:"housing_allocations"`
		Portals            []GeneratedBuildingPortal    `json:"portals"`
	}{foundation.Binding, foundation.RuleSet, foundation.Parcels, foundation.Buildings,
		foundation.UnitPools, foundation.HousingAllocations, foundation.Portals})
	if err != nil {
		return "", fmt.Errorf("marshal city land foundation: %w", err)
	}
	return sha256Hex(raw), nil
}

func validateLandRuleSet(ruleSet *LandRuleSet) error {
	if ruleSet == nil || ruleSet.ID != DefaultLandRuleSetID ||
		ruleSet.Version != DefaultLandRuleSetVersion || strings.TrimSpace(ruleSet.Name) == "" ||
		ruleSet.NominalCellAreaSQM <= 0 || len(ruleSet.Rules) != 3 {
		return ErrInvalidLandRuleSet
	}
	previous := ""
	uses := make(map[LandUse]struct{}, len(ruleSet.Rules))
	for _, rule := range ruleSet.Rules {
		if rule.Code == "" || rule.Code <= previous || rule.Code != string(rule.PrimaryUse) ||
			strings.TrimSpace(rule.Name) == "" || rule.MaxFloorAreaRatioMilli <= 0 ||
			rule.MaxCoverageMilli <= 0 || rule.MaxCoverageMilli > 1000 ||
			rule.MaxFloors <= 0 || rule.MaxFloors > MaximumZ-SurfaceZ+1 ||
			rule.SQMPerCapacityUnit <= 0 {
			return ErrInvalidLandRuleSet
		}
		switch rule.PrimaryUse {
		case LandUseResidential, LandUseCommercial, LandUseIndustrial:
		default:
			return ErrInvalidLandRuleSet
		}
		if _, duplicate := uses[rule.PrimaryUse]; duplicate {
			return ErrInvalidLandRuleSet
		}
		uses[rule.PrimaryUse] = struct{}{}
		previous = rule.Code
	}
	return nil
}

func validateLandGenerationInput(
	binding LandGeneratorBinding,
	ruleSet *LandRuleSet,
	overmap *Overmap,
	districts []DistrictLandSeed,
) error {
	if ruleSet == nil || validateLandRuleSet(ruleSet) != nil ||
		binding.SimulationVersion == "" || binding.WorldSeed <= 0 ||
		binding.LandRuleSetID != ruleSet.ID || binding.LandRuleSetVersion != ruleSet.Version ||
		binding.LandRuleSetHash != ruleSet.ContentHash || !validSHA256Hex(binding.LandRuleSetHash) ||
		!validSHA256Hex(binding.SpatialRuleSetHash) || !validSHA256Hex(binding.OvermapRootHash) ||
		overmap == nil || overmap.RootHash != binding.OvermapRootHash || len(overmap.Tiles) == 0 ||
		len(districts) == 0 {
		return ErrInvalidLandInput
	}
	districtCodes := make(map[string]struct{}, len(districts))
	for _, district := range districts {
		if district.Code == "" || district.AreaSQM <= 0 || district.DevelopableAreaSQM <= 0 ||
			district.DevelopableAreaSQM > district.AreaSQM || district.ResidentialCapacityUnits < 0 ||
			district.CommercialCapacityUnits < 0 || district.IndustrialCapacityUnits < 0 {
			return ErrInvalidLandInput
		}
		if _, duplicate := districtCodes[district.Code]; duplicate {
			return ErrInvalidLandInput
		}
		districtCodes[district.Code] = struct{}{}
		cohortKeys := make(map[string]struct{}, len(district.Households))
		for _, household := range district.Households {
			key := household.EntityCode + "/" + household.IncomeBand
			if household.EntityCode == "" || incomeBandOrder(household.IncomeBand) == 0 ||
				household.HouseholdUnits < 0 {
				return ErrInvalidLandInput
			}
			if _, duplicate := cohortKeys[key]; duplicate {
				return ErrInvalidLandInput
			}
			cohortKeys[key] = struct{}{}
		}
	}
	for _, tile := range overmap.Tiles {
		if _, ok := districtCodes[tile.DistrictCode]; !ok {
			return ErrInvalidLandInput
		}
	}
	return nil
}

func generateLandBuilding(
	parcel GeneratedParcel,
	quadrant landQuadrant,
	capacity int64,
	rule LandZoningRule,
) (GeneratedBuilding, GeneratedBuildingUnitPool, []GeneratedBuildingPortal, error) {
	buildingCode := strings.Replace(parcel.Code, "parcel_", "building_", 1)
	footprintArea, err := checkedMulDiv(parcel.AreaSQM, rule.MaxCoverageMilli, 1000)
	if err != nil || footprintArea <= 0 {
		return GeneratedBuilding{}, GeneratedBuildingUnitPool{}, nil, ErrLandCapacityOverflow
	}
	requiredFloorArea, err := checkedMul(capacity, rule.SQMPerCapacityUnit)
	if err != nil {
		return GeneratedBuilding{}, GeneratedBuildingUnitPool{}, nil, err
	}
	floorArea := requiredFloorArea
	if floorArea < footprintArea {
		floorArea = footprintArea
	}
	maximumFloorArea, err := checkedMulDiv(parcel.AreaSQM, rule.MaxFloorAreaRatioMilli, 1000)
	if err != nil || floorArea > maximumFloorArea {
		return GeneratedBuilding{}, GeneratedBuildingUnitPool{}, nil,
			fmt.Errorf("%w: %s", ErrLandCapacityOverflow, parcel.Code)
	}
	floors := ceilPositive(floorArea, footprintArea)
	if floors <= 0 || floors > int64(rule.MaxFloors) {
		return GeneratedBuilding{}, GeneratedBuildingUnitPool{}, nil,
			fmt.Errorf("%w: %s", ErrLandCapacityOverflow, parcel.Code)
	}
	footprint := LandRectangle{
		ChunkX: parcel.Geometry.ChunkX, ChunkY: parcel.Geometry.ChunkY, Z: parcel.Geometry.Z,
		LocalMinX: quadrant.buildingMinX, LocalMinY: quadrant.buildingMinY,
		LocalMaxX: quadrant.buildingMaxX, LocalMaxY: quadrant.buildingMaxY,
	}
	building := GeneratedBuilding{
		Code: buildingCode, ParcelCode: parcel.Code, DistrictCode: parcel.DistrictCode,
		PrimaryUse: rule.PrimaryUse, Footprint: footprint, BaseZ: SurfaceZ,
		TopZ: SurfaceZ + int32(floors) - 1, FloorCount: int32(floors),
		FootprintAreaSQM: footprintArea, FloorAreaSQM: floorArea,
		CapacityUnits: capacity, QualityMilli: 1000, Status: "active",
		CompletedTick: 0, Version: 1,
	}
	pool := GeneratedBuildingUnitPool{
		Code: "pool_" + buildingCode, BuildingCode: buildingCode, DistrictCode: parcel.DistrictCode,
		UseType: rule.PrimaryUse, UnitCount: capacity, CapacityUnitsPerUnit: 1, Version: 1,
	}
	centerX := footprint.ChunkX*DefaultChunkSize + int64((footprint.LocalMinX+footprint.LocalMaxX)/2)
	centerY := footprint.ChunkY*DefaultChunkSize + int64((footprint.LocalMinY+footprint.LocalMaxY)/2)
	insideX := footprint.ChunkX*DefaultChunkSize + int64(footprint.LocalMinX)
	portals := []GeneratedBuildingPortal{{
		Code: "entrance", BuildingCode: buildingCode, DistrictCode: parcel.DistrictCode,
		PortalType: "entrance", FromX: insideX - 1, FromY: centerY, FromZ: SurfaceZ,
		ToX: insideX, ToY: centerY, ToZ: SurfaceZ, Bidirectional: true,
		Status: "active", Version: 1,
	}}
	for level := int32(0); level < building.FloorCount-1; level++ {
		portals = append(portals, GeneratedBuildingPortal{
			Code:         fmt.Sprintf("stair_%03d_%03d", level, level+1),
			BuildingCode: buildingCode, DistrictCode: parcel.DistrictCode,
			PortalType: "stair", FromX: centerX, FromY: centerY, FromZ: level,
			ToX: centerX, ToY: centerY, ToZ: level + 1, Bidirectional: true,
			Status: "active", Version: 1,
		})
	}
	return building, pool, portals, nil
}

func allocateFoundationHousing(foundation *GeneratedLandFoundation, districts []DistrictLandSeed) error {
	poolIndexes := make(map[string][]int)
	buildingIndex := make(map[string]int, len(foundation.Buildings))
	for index := range foundation.Buildings {
		buildingIndex[foundation.Buildings[index].Code] = index
	}
	for index, pool := range foundation.UnitPools {
		if pool.UseType == LandUseResidential {
			poolIndexes[pool.DistrictCode] = append(poolIndexes[pool.DistrictCode], index)
		}
	}
	for _, district := range districts {
		indexes := poolIndexes[district.Code]
		cursor := 0
		for _, household := range district.Households {
			remaining := household.HouseholdUnits
			for remaining > 0 && cursor < len(indexes) {
				poolIndex := indexes[cursor]
				pool := &foundation.UnitPools[poolIndex]
				available := pool.UnitCount - pool.OccupiedUnitCount
				if available <= 0 {
					cursor++
					continue
				}
				allocated := remaining
				if allocated > available {
					allocated = available
				}
				pool.OccupiedUnitCount += allocated
				buildingPosition, ok := buildingIndex[pool.BuildingCode]
				if !ok {
					return ErrInvalidLandInput
				}
				foundation.Buildings[buildingPosition].OccupiedUnits += allocated
				foundation.HousingAllocations = append(foundation.HousingAllocations, GeneratedHousingAllocation{
					PoolCode: pool.Code, DistrictCode: district.Code,
					CohortKey:      district.Code + "/" + household.EntityCode + "/" + household.IncomeBand,
					AllocatedUnits: allocated, Status: "active", Version: 1,
				})
				remaining -= allocated
				if pool.OccupiedUnitCount == pool.UnitCount {
					cursor++
				}
			}
			if remaining != 0 {
				return fmt.Errorf("%w: district %s housing shortfall", ErrLandCapacityOverflow, district.Code)
			}
		}
	}
	sort.Slice(foundation.HousingAllocations, func(i, j int) bool {
		left, right := foundation.HousingAllocations[i], foundation.HousingAllocations[j]
		if left.DistrictCode != right.DistrictCode {
			return left.DistrictCode < right.DistrictCode
		}
		if left.CohortKey != right.CohortKey {
			return left.CohortKey < right.CohortKey
		}
		return left.PoolCode < right.PoolCode
	})
	return nil
}

func validateGeneratedLandFoundation(
	foundation *GeneratedLandFoundation,
	districts map[string]DistrictLandSeed,
) error {
	if foundation == nil || len(foundation.Parcels) == 0 || len(foundation.Buildings) == 0 ||
		len(foundation.Buildings) != len(foundation.UnitPools) {
		return ErrInvalidLandInput
	}
	areas := make(map[string]int64, len(districts))
	capacity := make(map[string]map[LandUse]int64, len(districts))
	occupied := make(map[string]int64, len(districts))
	parcelCodes := make(map[string]GeneratedParcel, len(foundation.Parcels))
	for _, parcel := range foundation.Parcels {
		if parcel.AreaSQM <= 0 || parcel.DevelopableAreaSQM != parcel.AreaSQM || !validLandRectangle(parcel.Geometry) {
			return ErrInvalidLandInput
		}
		if _, duplicate := parcelCodes[parcel.Code]; duplicate {
			return ErrInvalidLandInput
		}
		parcelCodes[parcel.Code] = parcel
		areas[parcel.DistrictCode] += parcel.AreaSQM
	}
	buildingCodes := make(map[string]GeneratedBuilding, len(foundation.Buildings))
	for _, building := range foundation.Buildings {
		parcel, ok := parcelCodes[building.ParcelCode]
		if !ok || parcel.DistrictCode != building.DistrictCode || !rectangleContains(parcel.Geometry, building.Footprint) ||
			building.CapacityUnits <= 0 || building.OccupiedUnits < 0 ||
			building.OccupiedUnits > building.CapacityUnits || building.FloorCount <= 0 ||
			building.TopZ-building.BaseZ+1 != building.FloorCount {
			return ErrInvalidLandInput
		}
		if _, duplicate := buildingCodes[building.Code]; duplicate {
			return ErrInvalidLandInput
		}
		buildingCodes[building.Code] = building
		if capacity[building.DistrictCode] == nil {
			capacity[building.DistrictCode] = make(map[LandUse]int64)
		}
		capacity[building.DistrictCode][building.PrimaryUse] += building.CapacityUnits
		if building.PrimaryUse == LandUseResidential {
			occupied[building.DistrictCode] += building.OccupiedUnits
		}
	}
	for code, district := range districts {
		if areas[code] != district.DevelopableAreaSQM ||
			capacity[code][LandUseResidential] != district.ResidentialCapacityUnits ||
			capacity[code][LandUseCommercial] != district.CommercialCapacityUnits ||
			capacity[code][LandUseIndustrial] != district.IndustrialCapacityUnits {
			return ErrInvalidLandInput
		}
		expectedOccupied := int64(0)
		for _, household := range district.Households {
			expectedOccupied += household.HouseholdUnits
		}
		if occupied[code] != expectedOccupied {
			return ErrInvalidLandInput
		}
	}
	return nil
}

func parcelIndexesForDistrict(parcels []GeneratedParcel, district, zone string) []int {
	result := make([]int, 0)
	for index, parcel := range parcels {
		if parcel.DistrictCode == district && (zone == "" || parcel.ZoneCode == zone) {
			result = append(result, index)
		}
	}
	return result
}

func distributeLandUnits(total int64, count int) []int64 {
	if count <= 0 {
		return []int64{}
	}
	result := make([]int64, count)
	base := total / int64(count)
	remainder := total % int64(count)
	for index := range result {
		result[index] = base
		if int64(index) < remainder {
			result[index]++
		}
	}
	return result
}

func quadrantForParcel(parcel GeneratedParcel) (landQuadrant, bool) {
	for _, quadrant := range defaultLandQuadrants {
		if parcel.Geometry.LocalMinX == quadrant.parcelMinX && parcel.Geometry.LocalMinY == quadrant.parcelMinY &&
			parcel.Geometry.LocalMaxX == quadrant.parcelMaxX && parcel.Geometry.LocalMaxY == quadrant.parcelMaxY &&
			parcel.ZoneCode == string(quadrant.use) {
			return quadrant, true
		}
	}
	return landQuadrant{}, false
}

func landObjectCode(prefix, district string, chunkX, chunkY int64, quadrant int) string {
	return fmt.Sprintf("%s_%s_%s_%s_q%d", prefix, district,
		landCoordinateToken(chunkX), landCoordinateToken(chunkY), quadrant)
}

func landCoordinateToken(value int64) string {
	if value < 0 {
		return fmt.Sprintf("n%d", -value)
	}
	return fmt.Sprintf("p%d", value)
}

func validLandRectangle(value LandRectangle) bool {
	return value.Z >= MinimumZ && value.Z <= MaximumZ &&
		value.LocalMinX >= 0 && value.LocalMinY >= 0 &&
		value.LocalMaxX < int32(DefaultChunkSize) && value.LocalMaxY < int32(DefaultChunkSize) &&
		value.LocalMinX <= value.LocalMaxX && value.LocalMinY <= value.LocalMaxY
}

func rectangleContains(outer, inner LandRectangle) bool {
	return validLandRectangle(outer) && validLandRectangle(inner) &&
		outer.ChunkX == inner.ChunkX && outer.ChunkY == inner.ChunkY && outer.Z == inner.Z &&
		inner.LocalMinX >= outer.LocalMinX && inner.LocalMinY >= outer.LocalMinY &&
		inner.LocalMaxX <= outer.LocalMaxX && inner.LocalMaxY <= outer.LocalMaxY
}

func checkedMul(left, right int64) (int64, error) {
	if left < 0 || right < 0 || left != 0 && right > math.MaxInt64/left {
		return 0, ErrLandCapacityOverflow
	}
	return left * right, nil
}

func checkedMulDiv(left, right, divisor int64) (int64, error) {
	product, err := checkedMul(left, right)
	if err != nil || divisor <= 0 {
		return 0, ErrLandCapacityOverflow
	}
	return product / divisor, nil
}

func ceilPositive(value, divisor int64) int64 {
	if value <= 0 || divisor <= 0 {
		return 0
	}
	return 1 + (value-1)/divisor
}

func incomeBandOrder(value string) int {
	switch value {
	case "low":
		return 1
	case "middle":
		return 2
	case "high":
		return 3
	default:
		return 0
	}
}

func validSHA256Hex(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && strings.ToLower(value) == value
}

func cloneLandRuleSet(ruleSet *LandRuleSet) *LandRuleSet {
	if ruleSet == nil {
		return nil
	}
	copyRuleSet := *ruleSet
	copyRuleSet.Rules = append([]LandZoningRule(nil), ruleSet.Rules...)
	return &copyRuleSet
}
