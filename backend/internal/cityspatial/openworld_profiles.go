package cityspatial

import (
	"strings"
)

const (
	WorldgenProfileJapanMetropolitan = "jp.metropolitan"
	WorldgenProfileChinaMetropolitan = "cn.metropolitan"
)

// WorldgenProfileSummary is the immutable catalog entry a client chooses
// before a new open-world is created. The full profile remains server-owned.
type WorldgenProfileSummary struct {
	ID          string `json:"id"`
	Version     string `json:"version"`
	Name        string `json:"name"`
	ContentHash string `json:"content_hash"`
}

// ListWorldgenProfiles returns copies so callers cannot alter the process-wide
// catalog used for new world bindings.
func ListWorldgenProfiles() ([]WorldgenProfileSummary, error) {
	profiles := []string{
		DefaultWorldgenProfileID,
		WorldgenProfileJapanMetropolitan,
		WorldgenProfileChinaMetropolitan,
	}
	result := make([]WorldgenProfileSummary, 0, len(profiles))
	for _, id := range profiles {
		profile, err := WorldgenProfileByID(id)
		if err != nil {
			return nil, err
		}
		result = append(result, WorldgenProfileSummary{
			ID: profile.ID, Version: profile.Version, Name: profile.Name, ContentHash: profile.ContentHash,
		})
	}
	return result, nil
}

// WorldgenProfileByID exposes the small built-in style catalog. Country style
// lives in data values (road/lot/archetype rules), not country branches in the
// generator itself.
func WorldgenProfileByID(id string) (*WorldgenProfile, error) {
	profile, err := DefaultWorldgenProfile()
	if err != nil {
		return nil, err
	}
	switch strings.TrimSpace(id) {
	case "", DefaultWorldgenProfileID:
		return profile, nil
	case WorldgenProfileJapanMetropolitan:
		profile.ID = WorldgenProfileJapanMetropolitan
		profile.Version = "1.0.0"
		profile.Name = "Japanese Metropolitan"
		profile.ArterialRoadWidth = 3
		profile.LocalStreetWidth = 1
		// Keep the narrow Japanese frontage, but leave a four-cell interior
		// after the generator's two-cell frontage setback.  The profile
		// validator requires every district/use combination to fit its
		// smallest legal parcel.
		profile.MinimumLotFrontage = 8
		profile.MaximumLotFrontage = 13
		profile.MinimumLotDepth = 8
		profile.MaximumLotDepth = 16
		profile.BuildingArchetypes = []WorldgenBuildingArchetype{
			{Code: "jp.station_arcade", PrimaryUse: LandUseCommercial, Weight: 30, MinimumWidth: 8, MaximumWidth: 16, MinimumDepth: 8, MaximumDepth: 16, MinimumFloors: 2, MaximumFloors: 6, LayoutStyle: "arcade", DistrictCodes: []string{"core", "inner"}},
			{Code: "jp.shotengai", PrimaryUse: LandUseCommercial, Weight: 48, MinimumWidth: 5, MaximumWidth: 11, MinimumDepth: 6, MaximumDepth: 14, MinimumFloors: 1, MaximumFloors: 3, LayoutStyle: "shopfront"},
			{Code: "jp.workshop", PrimaryUse: LandUseIndustrial, Weight: 1, MinimumWidth: 6, MaximumWidth: 14, MinimumDepth: 6, MaximumDepth: 16, MinimumFloors: 1, MaximumFloors: 3, LayoutStyle: "workshop"},
			{Code: "jp.rowhouse", PrimaryUse: LandUseResidential, Weight: 62, MinimumWidth: 5, MaximumWidth: 10, MinimumDepth: 6, MaximumDepth: 14, MinimumFloors: 1, MaximumFloors: 3, LayoutStyle: "rowhouse"},
			{Code: "jp.walkup", PrimaryUse: LandUseResidential, Weight: 28, MinimumWidth: 10, MaximumWidth: 16, MinimumDepth: 10, MaximumDepth: 18, MinimumFloors: 3, MaximumFloors: 7, LayoutStyle: "walkup", DistrictCodes: []string{"inner"}},
		}
	case WorldgenProfileChinaMetropolitan:
		profile.ID = WorldgenProfileChinaMetropolitan
		profile.Version = "1.0.0"
		profile.Name = "Chinese Metropolitan"
		profile.ArterialRoadWidth = 5
		profile.LocalStreetWidth = 2
		// Chinese podium and tower archetypes deliberately need a larger
		// minimum parcel than the Japanese street-front catalog.  Model that
		// constraint in the profile instead of letting a later building pass
		// fail on undersized lots.
		profile.MinimumLotFrontage = 16
		profile.MaximumLotFrontage = 24
		profile.MinimumLotDepth = 16
		profile.MaximumLotDepth = 26
		profile.BuildingArchetypes = []WorldgenBuildingArchetype{
			{Code: "cn.podium_tower", PrimaryUse: LandUseCommercial, Weight: 25, MinimumWidth: 16, MaximumWidth: 22, MinimumDepth: 16, MaximumDepth: 24, MinimumFloors: 6, MaximumFloors: 14, LayoutStyle: "tower", DistrictCodes: []string{"core"}},
			{Code: "cn.street_market", PrimaryUse: LandUseCommercial, Weight: 38, MinimumWidth: 8, MaximumWidth: 18, MinimumDepth: 8, MaximumDepth: 20, MinimumFloors: 1, MaximumFloors: 5, LayoutStyle: "arcade"},
			{Code: "cn.logistics_depot", PrimaryUse: LandUseIndustrial, Weight: 1, MinimumWidth: 14, MaximumWidth: 24, MinimumDepth: 14, MaximumDepth: 26, MinimumFloors: 1, MaximumFloors: 3, LayoutStyle: "loading_depot"},
			{Code: "cn.courtyard_block", PrimaryUse: LandUseResidential, Weight: 42, MinimumWidth: 14, MaximumWidth: 22, MinimumDepth: 14, MaximumDepth: 24, MinimumFloors: 3, MaximumFloors: 8, LayoutStyle: "courtyard", DistrictCodes: []string{"core", "inner"}},
			{Code: "cn.outlying_block", PrimaryUse: LandUseResidential, Weight: 24, MinimumWidth: 14, MaximumWidth: 20, MinimumDepth: 14, MaximumDepth: 22, MinimumFloors: 2, MaximumFloors: 5, LayoutStyle: "courtyard"},
			{Code: "cn.residential_tower", PrimaryUse: LandUseResidential, Weight: 36, MinimumWidth: 14, MaximumWidth: 22, MinimumDepth: 14, MaximumDepth: 24, MinimumFloors: 6, MaximumFloors: 15, LayoutStyle: "tower", DistrictCodes: []string{"inner"}},
		}
	default:
		return nil, ErrInvalidWorldgenInput
	}
	normalized, err := normalizeWorldgenProfile(*profile)
	if err != nil {
		return nil, err
	}
	return &normalized, nil
}

// DefaultOpenWorldgenBinding gives V2 a stable context hash without borrowing
// any frozen F7 overmap identity.
func DefaultOpenWorldgenBinding(
	simulationVersion string,
	worldSeed int64,
	profile *WorldgenProfile,
) (WorldgenBinding, error) {
	normalized, err := normalizeWorldgenProfileValue(profile)
	if err != nil {
		return WorldgenBinding{}, err
	}
	contextHash := sha256Hex([]byte(strings.Join([]string{
		"city-openworld-context-v1", simulationVersion, normalized.ID,
		normalized.Version, normalized.ContentHash,
	}, "\x00")))
	return DefaultWorldgenBinding(simulationVersion, worldSeed, contextHash, &normalized)
}

// DefaultOpenWorldgenBindingV2 pins a V2 world to the region-planning
// generator.  The version is part of every plan and content hash, so later
// generator changes cannot silently alter already materialized sectors.
func DefaultOpenWorldgenBindingV2(
	simulationVersion string,
	worldSeed int64,
	profile *WorldgenProfile,
) (WorldgenBinding, error) {
	binding, err := DefaultOpenWorldgenBinding(simulationVersion, worldSeed, profile)
	if err != nil {
		return WorldgenBinding{}, err
	}
	binding.GeneratorVersion = OpenWorldRegionGeneratorVersion
	return binding, nil
}

// DefaultOpenWorldgenBindingV3 binds a new world to the first vertical V2
// content contract.  The planner namespace remains region-safe, while the
// generator version records that every building floor and topology edge is
// part of the sealed sector content.
func DefaultOpenWorldgenBindingV3(
	simulationVersion string,
	worldSeed int64,
	profile *WorldgenProfile,
) (WorldgenBinding, error) {
	binding, err := DefaultOpenWorldgenBinding(simulationVersion, worldSeed, profile)
	if err != nil {
		return WorldgenBinding{}, err
	}
	binding.GeneratorVersion = OpenWorldVerticalGeneratorVersion
	return binding, nil
}
