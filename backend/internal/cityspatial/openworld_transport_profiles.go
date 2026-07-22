package cityspatial

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const (
	openWorldTransportStyleSchemaVersion = 1
	openWorldTransportStyleCatalog       = "worldgen_transport_style_catalog_v1"
)

// OpenWorldTransportHubRule maps a V9 hub kind to a profile-owned spatial
// node class. The reducer consumes the value as data; it never derives urban
// style from a country conditional.
type OpenWorldTransportHubRule struct {
	HubKind   string `json:"hub_kind"`
	NodeClass string `json:"node_class"`
}

// OpenWorldTransportModeRule maps a V9 mode/tier pair to a profile-owned
// corridor class. It intentionally describes only static identity; dynamic
// capacity, maintenance and signalling belong to later F9.3 revisions.
type OpenWorldTransportModeRule struct {
	ModeCode           string `json:"mode_code"`
	LocalCorridorClass string `json:"local_corridor_class"`
	TrunkCorridorClass string `json:"trunk_corridor_class"`
}

// OpenWorldTransportStyleProfile is an immutable content item attached to an
// already-versioned worldgen profile. Keeping it separate from WorldgenProfile
// preserves the hashes of existing V2/V3/V6 worldgen bindings while allowing
// V19 to pin its own transport-specific content contract.
type OpenWorldTransportStyleProfile struct {
	ID                      string                       `json:"id"`
	Version                 string                       `json:"version"`
	SourceWorldgenProfileID string                       `json:"source_worldgen_profile_id"`
	CatalogContract         string                       `json:"catalog_contract"`
	HubRules                []OpenWorldTransportHubRule  `json:"hub_rules"`
	ModeRules               []OpenWorldTransportModeRule `json:"mode_rules"`
	ContentHash             string                       `json:"content_hash"`
}

// OpenWorldTransportStyleProfileForWorldgenProfile returns a normalized copy
// of the content catalog entry bound to profileID. The catalog lookup is data
// driven: adding a future city style adds a catalog item rather than a network
// reducer branch.
func OpenWorldTransportStyleProfileForWorldgenProfile(profileID string) (*OpenWorldTransportStyleProfile, error) {
	profileID = strings.TrimSpace(profileID)
	seed, found := openWorldTransportStyleCatalogEntries()[profileID]
	if !found {
		return nil, fmt.Errorf("%w: transport style for worldgen profile %q", ErrInvalidWorldgenInput, profileID)
	}
	profile, err := normalizeOpenWorldTransportStyleProfile(seed)
	if err != nil {
		return nil, err
	}
	return &profile, nil
}

// OpenWorldTransportStyleNodeClass resolves a V9 hub kind through a frozen
// transport style profile.
func OpenWorldTransportStyleNodeClass(profile OpenWorldTransportStyleProfile, hubKind string) (string, bool) {
	for _, rule := range profile.HubRules {
		if rule.HubKind == hubKind {
			return rule.NodeClass, true
		}
	}
	return "", false
}

// OpenWorldTransportStyleCorridorClass resolves a V9 mode/tier pair through
// a frozen transport style profile.
func OpenWorldTransportStyleCorridorClass(profile OpenWorldTransportStyleProfile, modeCode, tier string) (string, bool) {
	for _, rule := range profile.ModeRules {
		if rule.ModeCode != modeCode {
			continue
		}
		switch tier {
		case "local":
			return rule.LocalCorridorClass, true
		case "trunk":
			return rule.TrunkCorridorClass, true
		default:
			return "", false
		}
	}
	return "", false
}

func openWorldTransportStyleCatalogEntries() map[string]OpenWorldTransportStyleProfile {
	return map[string]OpenWorldTransportStyleProfile{
		DefaultWorldgenProfileID: {
			ID:                      "sub2api-transport-temperate-metropolitan",
			Version:                 "1.0.0",
			SourceWorldgenProfileID: DefaultWorldgenProfileID,
			CatalogContract:         openWorldTransportStyleCatalog,
			HubRules: []OpenWorldTransportHubRule{
				{HubKind: "facility", NodeClass: "facility_access"},
				{HubKind: "interchange", NodeClass: "city_interchange"},
				{HubKind: "zone", NodeClass: "district_connector"},
			},
			ModeRules: []OpenWorldTransportModeRule{
				{ModeCode: "freight", LocalCorridorClass: "service_road", TrunkCorridorClass: "freight_arterial"},
				{ModeCode: "transit", LocalCorridorClass: "local_transit_way", TrunkCorridorClass: "rapid_transit_spine"},
				{ModeCode: "walk", LocalCorridorClass: "pedestrian_street", TrunkCorridorClass: "pedestrian_spine"},
			},
		},
		WorldgenProfileJapanMetropolitan: {
			ID:                      "sub2api-transport-jp-metropolitan",
			Version:                 "1.0.0",
			SourceWorldgenProfileID: WorldgenProfileJapanMetropolitan,
			CatalogContract:         openWorldTransportStyleCatalog,
			HubRules: []OpenWorldTransportHubRule{
				{HubKind: "facility", NodeClass: "station_frontage"},
				{HubKind: "interchange", NodeClass: "station_concourse"},
				{HubKind: "zone", NodeClass: "neighborhood_stop"},
			},
			ModeRules: []OpenWorldTransportModeRule{
				{ModeCode: "freight", LocalCorridorClass: "service_alley", TrunkCorridorClass: "logistics_spine"},
				{ModeCode: "transit", LocalCorridorClass: "station_approach", TrunkCorridorClass: "rail_trunk"},
				{ModeCode: "walk", LocalCorridorClass: "arcade_walkway", TrunkCorridorClass: "pedestrian_arcade"},
			},
		},
		WorldgenProfileChinaMetropolitan: {
			ID:                      "sub2api-transport-cn-metropolitan",
			Version:                 "1.0.0",
			SourceWorldgenProfileID: WorldgenProfileChinaMetropolitan,
			CatalogContract:         openWorldTransportStyleCatalog,
			HubRules: []OpenWorldTransportHubRule{
				{HubKind: "facility", NodeClass: "compound_gate"},
				{HubKind: "interchange", NodeClass: "metro_transfer"},
				{HubKind: "zone", NodeClass: "district_gateway"},
			},
			ModeRules: []OpenWorldTransportModeRule{
				{ModeCode: "freight", LocalCorridorClass: "loading_road", TrunkCorridorClass: "industrial_arterial"},
				{ModeCode: "transit", LocalCorridorClass: "collector_transit_way", TrunkCorridorClass: "metro_boulevard"},
				{ModeCode: "walk", LocalCorridorClass: "plaza_walkway", TrunkCorridorClass: "civic_promenade"},
			},
		},
	}
}

func normalizeOpenWorldTransportStyleProfile(profile OpenWorldTransportStyleProfile) (OpenWorldTransportStyleProfile, error) {
	profile.ID = strings.TrimSpace(profile.ID)
	profile.Version = strings.TrimSpace(profile.Version)
	profile.SourceWorldgenProfileID = strings.TrimSpace(profile.SourceWorldgenProfileID)
	profile.CatalogContract = strings.TrimSpace(profile.CatalogContract)
	profile.HubRules = append([]OpenWorldTransportHubRule(nil), profile.HubRules...)
	profile.ModeRules = append([]OpenWorldTransportModeRule(nil), profile.ModeRules...)
	for index := range profile.HubRules {
		profile.HubRules[index].HubKind = strings.TrimSpace(profile.HubRules[index].HubKind)
		profile.HubRules[index].NodeClass = strings.TrimSpace(profile.HubRules[index].NodeClass)
	}
	for index := range profile.ModeRules {
		profile.ModeRules[index].ModeCode = strings.TrimSpace(profile.ModeRules[index].ModeCode)
		profile.ModeRules[index].LocalCorridorClass = strings.TrimSpace(profile.ModeRules[index].LocalCorridorClass)
		profile.ModeRules[index].TrunkCorridorClass = strings.TrimSpace(profile.ModeRules[index].TrunkCorridorClass)
	}
	sort.Slice(profile.HubRules, func(i, j int) bool { return profile.HubRules[i].HubKind < profile.HubRules[j].HubKind })
	sort.Slice(profile.ModeRules, func(i, j int) bool { return profile.ModeRules[i].ModeCode < profile.ModeRules[j].ModeCode })
	if err := validateOpenWorldTransportStyleProfile(profile); err != nil {
		return OpenWorldTransportStyleProfile{}, err
	}
	profile.ContentHash = ""
	raw, err := json.Marshal(struct {
		SchemaVersion           int                          `json:"schema_version"`
		ID                      string                       `json:"id"`
		Version                 string                       `json:"version"`
		SourceWorldgenProfileID string                       `json:"source_worldgen_profile_id"`
		CatalogContract         string                       `json:"catalog_contract"`
		HubRules                []OpenWorldTransportHubRule  `json:"hub_rules"`
		ModeRules               []OpenWorldTransportModeRule `json:"mode_rules"`
	}{
		SchemaVersion:           openWorldTransportStyleSchemaVersion,
		ID:                      profile.ID,
		Version:                 profile.Version,
		SourceWorldgenProfileID: profile.SourceWorldgenProfileID,
		CatalogContract:         profile.CatalogContract,
		HubRules:                profile.HubRules,
		ModeRules:               profile.ModeRules,
	})
	if err != nil {
		return OpenWorldTransportStyleProfile{}, fmt.Errorf("marshal transport style profile: %w", err)
	}
	sum := sha256.Sum256(raw)
	profile.ContentHash = hex.EncodeToString(sum[:])
	return profile, nil
}

func validateOpenWorldTransportStyleProfile(profile OpenWorldTransportStyleProfile) error {
	if !openWorldTransportStyleCodeValid(profile.ID) || profile.Version == "" ||
		!openWorldTransportStyleCodeValid(profile.SourceWorldgenProfileID) ||
		profile.CatalogContract != openWorldTransportStyleCatalog || len(profile.HubRules) != 3 || len(profile.ModeRules) != 3 {
		return ErrInvalidWorldgenInput
	}
	expectedHubKinds := map[string]struct{}{"facility": {}, "interchange": {}, "zone": {}}
	for _, rule := range profile.HubRules {
		if _, found := expectedHubKinds[rule.HubKind]; !found || !openWorldTransportStyleCodeValid(rule.NodeClass) {
			return ErrInvalidWorldgenInput
		}
		delete(expectedHubKinds, rule.HubKind)
	}
	if len(expectedHubKinds) != 0 {
		return ErrInvalidWorldgenInput
	}
	expectedModes := map[string]struct{}{"freight": {}, "transit": {}, "walk": {}}
	for _, rule := range profile.ModeRules {
		if _, found := expectedModes[rule.ModeCode]; !found || !openWorldTransportStyleCodeValid(rule.LocalCorridorClass) ||
			!openWorldTransportStyleCodeValid(rule.TrunkCorridorClass) {
			return ErrInvalidWorldgenInput
		}
		delete(expectedModes, rule.ModeCode)
	}
	if len(expectedModes) != 0 {
		return ErrInvalidWorldgenInput
	}
	return nil
}

func openWorldTransportStyleCodeValid(value string) bool {
	if len(value) < 2 || len(value) > 96 {
		return false
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' ||
			character == '.' || character == '_' || character == '-' {
			continue
		}
		return false
	}
	return true
}
