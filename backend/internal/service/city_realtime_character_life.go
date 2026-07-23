package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/cityspatial"
)

const (
	cityRealtimeCharacterLifeSchemaVersion            = 1
	cityRealtimeCharacterActivityCatalogID            = "city-realtime-character-core"
	cityRealtimeCharacterActivityLegacyCatalogVersion = "1.0.0"
	cityRealtimeCharacterActivityCatalogV110          = "1.1.0"
	cityRealtimeCharacterActivityCatalogV120          = "1.2.0"
	cityRealtimeCharacterActivityCatalogVersion       = "1.3.0"
	cityRealtimeCharacterActivityBindingVersion       = "city-realtime-character-activity-binding-v1"
	cityRealtimeCharacterActivityAction               = "character.activity"
	cityRealtimeCharacterActivityMinimumIntervalUS    = int64(5 * cityRealtimeTimeQuantumUS)
	cityRealtimeCharacterProfileSchemaLegacy          = 1
	cityRealtimeCharacterProfileSchemaMetabolism      = 2
	cityRealtimeCharacterProfileSchemaProgression     = 3
	cityRealtimeCharacterInitialEnergyMilli           = int64(760)
	cityRealtimeCharacterInitialSatietyMilli          = int64(720)
	cityRealtimeCharacterInitialMoraleMilli           = int64(640)
	cityRealtimeCharacterInitialCivicStandingMilli    = int64(800)
	cityRealtimeCharacterInitialCityCreditUnits       = int64(0)
	cityRealtimeCharacterInitialRationQuantity        = int64(2)
	cityRealtimeCharacterLifeMinimumCreditUnits       = int64(-100000000)
	cityRealtimeCharacterLifeMaximumCreditUnits       = int64(100000000)
	cityRealtimeCharacterMetabolismMinimumIntervalUS  = int64(60 * cityRealtimeTimeQuantumUS)
	cityRealtimeCharacterActivityDefaultEventLimit    = 24
	cityRealtimeCharacterActivityMaximumEventLimit    = 100
	cityRealtimeCharacterLifeActivityChainNamespace   = "city-realtime-character-activity-genesis-v1"
	cityRealtimeCharacterLifeLawChainNamespace        = "city-realtime-character-law-genesis-v1"
)

const cityRealtimeCharacterRationItemCode = "item.food.ration"

// CityRealtimeCharacterLife is an owner-private projection of the shared
// character's current life state. City credit is intentionally local to the
// simulated world; it is not a Sub2API wallet balance and has no direct
// redemption path.
type CityRealtimeCharacterLife struct {
	EnergyMilli               int64                                 `json:"energy_milli"`
	SatietyMilli              int64                                 `json:"satiety_milli"`
	MoraleMilli               int64                                 `json:"morale_milli"`
	CivicStandingMilli        int64                                 `json:"civic_standing_milli"`
	CityCreditUnits           int64                                 `json:"city_credit_units"`
	Revision                  int64                                 `json:"revision"`
	ActivityRevision          int64                                 `json:"activity_revision"`
	LawRevision               int64                                 `json:"law_revision"`
	MetabolismRevision        int64                                 `json:"metabolism_revision"`
	LastFrameSequence         int64                                 `json:"last_frame_sequence"`
	LastActivityWorldTimeUS   int64                                 `json:"last_activity_world_time_us"`
	LastMetabolismWorldTimeUS int64                                 `json:"last_metabolism_world_time_us"`
	Inventory                 []CityRealtimeCharacterInventoryStack `json:"inventory"`
	Progression               *CityRealtimeCharacterProgression     `json:"progression,omitempty"`
}

// CityRealtimeCharacterInventoryStack exposes only the owner's own in-world
// items. It never contains account, provider, personality, or Agent data.
type CityRealtimeCharacterInventoryStack struct {
	ItemCode          string `json:"item_code"`
	Quantity          int64  `json:"quantity"`
	Revision          int64  `json:"revision"`
	LastFrameSequence int64  `json:"last_frame_sequence"`
}

// CityRealtimeCharacterActivityAvailability is advisory UI state. The
// mutation endpoint re-evaluates every condition under the world lock.
type CityRealtimeCharacterActivityAvailability struct {
	Code                string   `json:"code"`
	CategoryCode        string   `json:"category_code"`
	Available           bool     `json:"available"`
	ReasonCode          string   `json:"reason_code,omitempty"`
	CooldownRemainingUS int64    `json:"cooldown_remaining_us,omitempty"`
	RequiredRoleCodes   []string `json:"required_role_codes,omitempty"`
}

// CityRealtimeCharacterActivityResult contains the sealed, owner-safe outcome
// of one activity. It is suitable for idempotency receipt replay.
type CityRealtimeCharacterActivityResult struct {
	Code                    string                                 `json:"code"`
	CategoryCode            string                                 `json:"category_code"`
	Outcome                 string                                 `json:"outcome"`
	PublicVisibility        bool                                   `json:"public_visibility"`
	EnergyDeltaMilli        int64                                  `json:"energy_delta_milli"`
	SatietyDeltaMilli       int64                                  `json:"satiety_delta_milli"`
	MoraleDeltaMilli        int64                                  `json:"morale_delta_milli"`
	CivicStandingDeltaMilli int64                                  `json:"civic_standing_delta_milli"`
	CityCreditDeltaUnits    int64                                  `json:"city_credit_delta_units"`
	ItemCode                string                                 `json:"item_code,omitempty"`
	ItemQuantityDelta       int64                                  `json:"item_quantity_delta,omitempty"`
	LawCaseCode             string                                 `json:"law_case_code,omitempty"`
	ExperienceDeltas        []CityRealtimeCharacterExperienceDelta `json:"experience_deltas,omitempty"`
}

// CityRealtimeCharacterActivityInput accepts only a catalog activity code.
// The server derives character identity, effects, timing, items, and law
// outcome from sealed world state and its bound catalog.
type CityRealtimeCharacterActivityInput struct {
	UserID         int64
	WorldID        int64
	ActivityCode   string
	IdempotencyKey string
}

// CityRealtimeCharacterEventPage is a private, bounded life timeline. It is
// deliberately separate from public member-safe world event projections.
type CityRealtimeCharacterEventPage struct {
	Items              []CityRealtimeCharacterActivityEvent `json:"items"`
	NextBeforeSequence *int64                               `json:"next_before_sequence,omitempty"`
}

type CityRealtimeCharacterEventListInput struct {
	UserID         int64
	WorldID        int64
	BeforeSequence int64
	Limit          int
}

type CityRealtimeCharacterActivityEvent struct {
	Sequence                int64  `json:"sequence"`
	FrameSequence           int64  `json:"frame_sequence"`
	ActivityCode            string `json:"activity_code"`
	CategoryCode            string `json:"category_code"`
	Outcome                 string `json:"outcome"`
	PublicVisibility        bool   `json:"public_visibility"`
	EnergyDeltaMilli        int64  `json:"energy_delta_milli"`
	SatietyDeltaMilli       int64  `json:"satiety_delta_milli"`
	MoraleDeltaMilli        int64  `json:"morale_delta_milli"`
	CivicStandingDeltaMilli int64  `json:"civic_standing_delta_milli"`
	CityCreditDeltaUnits    int64  `json:"city_credit_delta_units"`
	ItemCode                string `json:"item_code,omitempty"`
	ItemQuantityDelta       int64  `json:"item_quantity_delta,omitempty"`
	LawCaseCode             string `json:"law_case_code,omitempty"`
	LawRuleCode             string `json:"law_rule_code,omitempty"`
	LawDisposition          string `json:"law_disposition,omitempty"`
	LawPenaltyCreditUnits   int64  `json:"law_penalty_city_credit_units,omitempty"`
}

// CityRealtimePublicCharacterEvent contains only a member-safe shared-world
// consequence. Private needs, inventory and credit deltas remain absent.
type CityRealtimePublicCharacterEvent struct {
	FrameSequence  int64  `json:"frame_sequence"`
	ActorCode      string `json:"actor_code"`
	PublicLabel    string `json:"public_label"`
	ActivityCode   string `json:"activity_code"`
	CategoryCode   string `json:"category_code"`
	Outcome        string `json:"outcome"`
	LawRuleCode    string `json:"law_rule_code,omitempty"`
	LawDisposition string `json:"law_disposition,omitempty"`
}

type CityRealtimePublicCharacterEventListInput struct {
	UserID       int64
	WorldID      int64
	BeforeCursor string
	Limit        int
}

type CityRealtimePublicCharacterEventPage struct {
	Items      []CityRealtimePublicCharacterEvent `json:"items"`
	NextCursor *string                            `json:"next_cursor,omitempty"`
}

type cityRealtimeCharacterActivityCatalogBinding struct {
	CatalogID      string `json:"catalog_id"`
	CatalogVersion string `json:"catalog_version"`
	CatalogHash    string `json:"catalog_hash"`
	BindingHash    string `json:"binding_hash"`
}

type cityRealtimeCharacterActivityCatalogManifest struct {
	SchemaVersion int                                         `json:"schema_version"`
	CreditUnit    string                                      `json:"credit_unit"`
	Activities    []cityRealtimeCharacterActivityDefinition   `json:"activities"`
	Metabolism    *cityRealtimeCharacterMetabolismDefinition  `json:"metabolism,omitempty"`
	Progression   *cityRealtimeCharacterProgressionDefinition `json:"progression,omitempty"`
}

// cityRealtimeCharacterMetabolismDefinition is a finite, server-owned rule
// for passive realtime needs. It never carries browser/model input.
type cityRealtimeCharacterMetabolismDefinition struct {
	IntervalUS        int64 `json:"interval_us"`
	EnergyDeltaMilli  int64 `json:"energy_delta"`
	SatietyDeltaMilli int64 `json:"satiety_delta"`
	MoraleDeltaMilli  int64 `json:"morale_delta"`
}

type cityRealtimeCharacterActivityCatalogSpec struct {
	Definitions []cityRealtimeCharacterActivityDefinition
	Metabolism  *cityRealtimeCharacterMetabolismDefinition
	Progression *cityRealtimeCharacterProgressionDefinition
}

type cityRealtimeCharacterActivityDefinition struct {
	Code                string                                              `json:"code"`
	CategoryCode        string                                              `json:"category"`
	LocationRequirement string                                              `json:"location_requirement"`
	PublicVisibility    bool                                                `json:"public_visibility"`
	MinimumIntervalUS   int64                                               `json:"minimum_interval_us"`
	MinimumEnergyMilli  int64                                               `json:"minimum_energy_milli"`
	MinimumSatietyMilli int64                                               `json:"minimum_satiety_milli"`
	EnergyDeltaMilli    int64                                               `json:"energy_delta"`
	SatietyDeltaMilli   int64                                               `json:"satiety_delta"`
	MoraleDeltaMilli    int64                                               `json:"morale_delta"`
	CivicStandingDelta  int64                                               `json:"civic_standing_delta"`
	CityCreditDelta     int64                                               `json:"city_credit_delta"`
	ItemCode            string                                              `json:"item_code"`
	ItemQuantityDelta   int64                                               `json:"item_quantity_delta"`
	Law                 *cityRealtimeCharacterLawEffectDefinition           `json:"law,omitempty"`
	Progression         *cityRealtimeCharacterActivityProgressionDefinition `json:"progression,omitempty"`
}

type cityRealtimeCharacterLawEffectDefinition struct {
	RuleCode               string `json:"rule_code"`
	Disposition            string `json:"disposition"`
	PenaltyCityCreditUnits int64  `json:"penalty_city_credit_units"`
	StandingDeltaMilli     int64  `json:"standing_delta_milli"`
}

type cityRealtimeCharacterLifeRuntime struct {
	Binding     cityRealtimeCharacterActivityCatalogBinding
	Definitions map[string]cityRealtimeCharacterActivityDefinition
	Metabolism  *cityRealtimeCharacterMetabolismDefinition
	Progression *cityRealtimeCharacterProgressionDefinition
}

type cityRealtimeCharacterProfile struct {
	StateSchemaVersion        int
	ActorCode                 string
	EnergyMilli               int64
	SatietyMilli              int64
	MoraleMilli               int64
	CivicStandingMilli        int64
	CityCreditUnits           int64
	Revision                  int64
	ActivityRevision          int64
	LawRevision               int64
	MetabolismRevision        int64
	ProgressionRevision       int64
	SpawnedFrameSequence      int64
	LastFrameSequence         int64
	LastActivityWorldTimeUS   int64
	LastMetabolismWorldTimeUS int64
	StateHash                 string
	ActivityEventChainHash    string
	LawEventChainHash         string
	ArchetypeCode             string
	ProgressionEventChainHash string
	ProgressionStateHash      string
	Inventory                 []cityRealtimeCharacterInventoryStack
	Attributes                []cityRealtimeCharacterAttributeState
	Roles                     []cityRealtimeCharacterRoleAssignment
}

type cityRealtimeCharacterInventoryStack struct {
	ItemCode          string
	Quantity          int64
	Revision          int64
	LastFrameSequence int64
	StateHash         string
}

type cityRealtimeCharacterActivityEventRecord struct {
	ActorCode               string
	EventSequence           int64
	FrameSequence           int64
	ActivityCode            string
	CategoryCode            string
	Outcome                 string
	PublicVisibility        bool
	EnergyDeltaMilli        int64
	SatietyDeltaMilli       int64
	MoraleDeltaMilli        int64
	CivicStandingDeltaMilli int64
	CityCreditDeltaUnits    int64
	ItemCode                *string
	ItemQuantityDelta       int64
	LawCaseCode             *string
	PreviousEventHash       string
	EventHash               string
}

type cityRealtimeCharacterLawEventRecord struct {
	ActorCode              string
	EventSequence          int64
	ActivityEventSequence  int64
	FrameSequence          int64
	CaseCode               string
	RuleCode               string
	Disposition            string
	PenaltyCityCreditUnits int64
	StandingDeltaMilli     int64
	PublicVisibility       bool
	PreviousEventHash      string
	EventHash              string
}

type cityRealtimeCharacterLifeHash struct {
	ActorCode               string                                `json:"actor_code"`
	EnergyMilli             int64                                 `json:"energy_milli"`
	SatietyMilli            int64                                 `json:"satiety_milli"`
	MoraleMilli             int64                                 `json:"morale_milli"`
	CivicStandingMilli      int64                                 `json:"civic_standing_milli"`
	CityCreditUnits         int64                                 `json:"city_credit_units"`
	Revision                int64                                 `json:"revision"`
	ActivityRevision        int64                                 `json:"activity_revision"`
	LawRevision             int64                                 `json:"law_revision"`
	SpawnedFrameSequence    int64                                 `json:"spawned_frame_sequence"`
	LastFrameSequence       int64                                 `json:"last_frame_sequence"`
	LastActivityWorldTimeUS int64                                 `json:"last_activity_world_time_us"`
	StateHash               string                                `json:"state_hash"`
	ActivityEventChainHash  string                                `json:"activity_event_chain_hash"`
	LawEventChainHash       string                                `json:"law_event_chain_hash"`
	Inventory               []cityRealtimeCharacterInventoryStack `json:"inventory"`
	Progression             *cityRealtimeCharacterProgressionHash `json:"progression,omitempty"`
}

type cityRealtimeCharacterProgressionHash struct {
	ArchetypeCode  string                                `json:"archetype_code"`
	Revision       int64                                 `json:"revision"`
	EventChainHash string                                `json:"event_chain_hash"`
	StateHash      string                                `json:"state_hash"`
	Attributes     []cityRealtimeCharacterAttributeState `json:"attributes"`
	Roles          []cityRealtimeCharacterRoleAssignment `json:"roles"`
}

type cityRealtimeCharacterLifeHashState struct {
	SchemaVersion int                                          `json:"schema_version"`
	Binding       *cityRealtimeCharacterActivityCatalogBinding `json:"binding,omitempty"`
	Profiles      []cityRealtimeCharacterLifeHash              `json:"profiles"`
	Metabolism    []cityRealtimeCharacterMetabolismHash        `json:"metabolism,omitempty"`
}

// The legacy profile list remains byte-for-byte compatible for worlds bound
// to catalog 1.0/1.1. This optional extension becomes canonical only for a
// world explicitly bound to the realtime-metabolism catalog.
type cityRealtimeCharacterMetabolismHash struct {
	ActorCode                 string `json:"actor_code"`
	StateSchemaVersion        int    `json:"state_schema_version"`
	MetabolismRevision        int64  `json:"metabolism_revision"`
	LastMetabolismWorldTimeUS int64  `json:"last_metabolism_world_time_us"`
	StateHash                 string `json:"state_hash"`
}

func cityRealtimeCharacterActivityDefinitions() []cityRealtimeCharacterActivityDefinition {
	spec, _ := cityRealtimeCharacterActivityCatalogSpecForVersion(cityRealtimeCharacterActivityCatalogVersion)
	return spec.Definitions
}

func cityRealtimeCharacterActivityDefinitionsForVersion(version string) ([]cityRealtimeCharacterActivityDefinition, bool) {
	spec, supported := cityRealtimeCharacterActivityCatalogSpecForVersion(version)
	return spec.Definitions, supported
}

func cityRealtimeCharacterActivityCatalogSpecForVersion(version string) (cityRealtimeCharacterActivityCatalogSpec, bool) {
	legacy := []cityRealtimeCharacterActivityDefinition{
		{Code: "rest.short", CategoryCode: "recovery", LocationRequirement: "traversable", PublicVisibility: false, MinimumIntervalUS: cityRealtimeCharacterActivityMinimumIntervalUS, EnergyDeltaMilli: 160, SatietyDeltaMilli: -20, MoraleDeltaMilli: 10},
		{Code: "work.civic_shift", CategoryCode: "work", LocationRequirement: "road_or_sidewalk", PublicVisibility: true, MinimumIntervalUS: cityRealtimeCharacterActivityMinimumIntervalUS, EnergyDeltaMilli: -120, SatietyDeltaMilli: -75, MoraleDeltaMilli: 20, CivicStandingDelta: 10, CityCreditDelta: 24},
		{Code: "consume.ration", CategoryCode: "consumption", LocationRequirement: "traversable", PublicVisibility: false, MinimumIntervalUS: cityRealtimeCharacterActivityMinimumIntervalUS, EnergyDeltaMilli: 35, SatietyDeltaMilli: 260, MoraleDeltaMilli: 10, ItemCode: cityRealtimeCharacterRationItemCode, ItemQuantityDelta: -1},
		{Code: "civic.cleanup", CategoryCode: "civic", LocationRequirement: "road_or_sidewalk", PublicVisibility: true, MinimumIntervalUS: cityRealtimeCharacterActivityMinimumIntervalUS, EnergyDeltaMilli: -70, SatietyDeltaMilli: -50, MoraleDeltaMilli: 30, CivicStandingDelta: 20, CityCreditDelta: 10},
		{Code: "conduct.disruption", CategoryCode: "conduct", LocationRequirement: "road_or_sidewalk", PublicVisibility: true, MinimumIntervalUS: cityRealtimeCharacterActivityMinimumIntervalUS, EnergyDeltaMilli: -15, SatietyDeltaMilli: -15, MoraleDeltaMilli: -50, CivicStandingDelta: -140, CityCreditDelta: -12, Law: &cityRealtimeCharacterLawEffectDefinition{RuleCode: "rule.public_disruption", Disposition: "fine", PenaltyCityCreditUnits: 12, StandingDeltaMilli: -140}},
	}
	switch version {
	case cityRealtimeCharacterActivityLegacyCatalogVersion:
		return cityRealtimeCharacterActivityCatalogSpec{Definitions: legacy}, true
	case cityRealtimeCharacterActivityCatalogV110, cityRealtimeCharacterActivityCatalogV120, cityRealtimeCharacterActivityCatalogVersion:
		upgraded := append([]cityRealtimeCharacterActivityDefinition(nil), legacy...)
		upgraded[1].MinimumEnergyMilli = 160
		upgraded[1].MinimumSatietyMilli = 120
		upgraded[1].ItemCode = cityRealtimeCharacterRationItemCode
		upgraded[1].ItemQuantityDelta = 1
		upgraded[3].MinimumEnergyMilli = 100
		upgraded[3].MinimumSatietyMilli = 80
		upgraded[4].MinimumEnergyMilli = 40
		upgraded[4].MinimumSatietyMilli = 40
		spec := cityRealtimeCharacterActivityCatalogSpec{Definitions: upgraded}
		if version == cityRealtimeCharacterActivityCatalogV120 || version == cityRealtimeCharacterActivityCatalogVersion {
			spec.Metabolism = &cityRealtimeCharacterMetabolismDefinition{
				IntervalUS:        cityRealtimeCharacterMetabolismMinimumIntervalUS,
				EnergyDeltaMilli:  -6,
				SatietyDeltaMilli: -8,
				MoraleDeltaMilli:  -2,
			}
		}
		if version == cityRealtimeCharacterActivityCatalogVersion {
			spec.Progression = cityRealtimeCharacterProgressionCatalogDefinition()
			spec.Definitions = cityRealtimeCharacterProgressionActivities(spec.Definitions)
		}
		return spec, true
	default:
		return cityRealtimeCharacterActivityCatalogSpec{}, false
	}
}

func initializeCityRealtimeCharacterActivityFoundation(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	simulationVersion string,
) error {
	if tx == nil || worldID <= 0 || !cityEngineSupportsRealtimeStaticWorldgen(simulationVersion) {
		return ErrCitySimulationVersion.WithMetadata(map[string]string{"version": simulationVersion})
	}
	runtime, err := loadCityRealtimeCharacterActivityCoreRuntime(ctx, tx)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx,
		`SELECT set_config('sub2api.city_realtime_character_activity_initialize_world_id', $1, TRUE)`,
		strconv.FormatInt(worldID, 10)); err != nil {
		return fmt.Errorf("activate realtime character activity initialization gate: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_realtime_character_activity_world_bindings
    (world_id, catalog_id, catalog_version, catalog_hash, binding_hash, genesis_frame_sequence, metadata)
VALUES ($1, $2, $3, $4, $5, 0, '{}'::jsonb)`,
		worldID, runtime.Binding.CatalogID, runtime.Binding.CatalogVersion,
		runtime.Binding.CatalogHash, runtime.Binding.BindingHash,
	); err != nil {
		return fmt.Errorf("create realtime character activity world binding: %w", err)
	}
	return nil
}

func loadCityRealtimeCharacterActivityCoreRuntime(
	ctx context.Context,
	queryer citySQLQueryer,
) (*cityRealtimeCharacterLifeRuntime, error) {
	binding := cityRealtimeCharacterActivityCatalogBinding{
		CatalogID: cityRealtimeCharacterActivityCatalogID, CatalogVersion: cityRealtimeCharacterActivityCatalogVersion,
	}
	var status string
	var rawManifest []byte
	err := queryer.QueryRowContext(ctx, `
SELECT catalog_hash, status, manifest
FROM city_realtime_character_activity_catalogs
WHERE catalog_id = $1 AND catalog_version = $2`, binding.CatalogID, binding.CatalogVersion,
	).Scan(&binding.CatalogHash, &status, &rawManifest)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_activity_catalog"})
	}
	if err != nil {
		return nil, fmt.Errorf("load realtime character activity core catalog: %w", err)
	}
	definitions, metabolism, progression, err := decodeCityRealtimeCharacterActivityCatalogManifest(rawManifest, binding.CatalogVersion)
	if err != nil || status != "published" || !cityRealtimeSHA256Hex(binding.CatalogHash) {
		if err == nil {
			err = errors.New("invalid character activity catalog")
		}
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_activity_catalog"}).WithCause(err)
	}
	binding.BindingHash = cityRealtimeCharacterActivityBindingHash(binding)
	return &cityRealtimeCharacterLifeRuntime{
		Binding: binding, Definitions: definitions, Metabolism: metabolism, Progression: progression,
	}, nil
}

func loadCityRealtimeCharacterLifeRuntime(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*cityRealtimeCharacterLifeRuntime, error) {
	if worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	binding := cityRealtimeCharacterActivityCatalogBinding{}
	err := queryer.QueryRowContext(ctx, `
SELECT catalog_id, catalog_version, catalog_hash, binding_hash
FROM city_realtime_character_activity_world_bindings
WHERE world_id = $1`, worldID).Scan(
		&binding.CatalogID, &binding.CatalogVersion, &binding.CatalogHash, &binding.BindingHash,
	)
	if errors.Is(err, sql.ErrNoRows) {
		var profileCount int
		if countErr := queryer.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM city_realtime_character_profiles WHERE world_id = $1`, worldID,
		).Scan(&profileCount); countErr != nil {
			return nil, fmt.Errorf("check historical realtime character life state: %w", countErr)
		}
		if profileCount != 0 {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_activity_binding"})
		}
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load realtime character activity world binding: %w", err)
	}
	if !validateCityRealtimeCharacterActivityBinding(binding) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_activity_binding"})
	}
	var storedHash, status string
	var rawManifest []byte
	err = queryer.QueryRowContext(ctx, `
SELECT catalog_hash, status, manifest
FROM city_realtime_character_activity_catalogs
WHERE catalog_id = $1 AND catalog_version = $2`, binding.CatalogID, binding.CatalogVersion,
	).Scan(&storedHash, &status, &rawManifest)
	if errors.Is(err, sql.ErrNoRows) || status != "published" || storedHash != binding.CatalogHash {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_activity_catalog"})
	}
	if err != nil {
		return nil, fmt.Errorf("load realtime character bound catalog: %w", err)
	}
	definitions, metabolism, progression, err := decodeCityRealtimeCharacterActivityCatalogManifest(rawManifest, binding.CatalogVersion)
	if err != nil {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_activity_manifest"}).WithCause(err)
	}
	return &cityRealtimeCharacterLifeRuntime{
		Binding: binding, Definitions: definitions, Metabolism: metabolism, Progression: progression,
	}, nil
}

func decodeCityRealtimeCharacterActivityCatalogManifest(
	raw []byte,
	catalogVersion string,
) (map[string]cityRealtimeCharacterActivityDefinition, *cityRealtimeCharacterMetabolismDefinition, *cityRealtimeCharacterProgressionDefinition, error) {
	manifest := cityRealtimeCharacterActivityCatalogManifest{}
	if len(raw) == 0 || json.Unmarshal(raw, &manifest) != nil {
		return nil, nil, nil, errors.New("invalid activity catalog manifest JSON")
	}
	if manifest.SchemaVersion != cityRealtimeCharacterLifeSchemaVersion || manifest.CreditUnit != "city_credit" {
		return nil, nil, nil, errors.New("invalid activity catalog schema")
	}
	expectedSpec, supported := cityRealtimeCharacterActivityCatalogSpecForVersion(catalogVersion)
	if !supported {
		return nil, nil, nil, errors.New("unsupported activity catalog version")
	}
	if !cityRealtimeCharacterMetabolismDefinitionEqual(expectedSpec.Metabolism, manifest.Metabolism) {
		return nil, nil, nil, errors.New("unexpected activity catalog metabolism")
	}
	if !cityRealtimeCharacterProgressionDefinitionEqual(expectedSpec.Progression, manifest.Progression) {
		return nil, nil, nil, errors.New("unexpected activity catalog progression")
	}
	expected := make(map[string]cityRealtimeCharacterActivityDefinition)
	for _, definition := range expectedSpec.Definitions {
		expected[definition.Code] = definition
	}
	if len(manifest.Activities) != len(expected) {
		return nil, nil, nil, errors.New("unexpected activity catalog size")
	}
	definitions := make(map[string]cityRealtimeCharacterActivityDefinition, len(manifest.Activities))
	for _, definition := range manifest.Activities {
		baseline, found := expected[definition.Code]
		if !found || !cityRealtimeCharacterActivityDefinitionEqual(baseline, definition) {
			return nil, nil, nil, fmt.Errorf("unexpected activity definition %q", definition.Code)
		}
		if _, duplicate := definitions[definition.Code]; duplicate {
			return nil, nil, nil, fmt.Errorf("duplicate activity definition %q", definition.Code)
		}
		definitions[definition.Code] = definition
	}
	return definitions, manifest.Metabolism, manifest.Progression, nil
}

func cityRealtimeCharacterMetabolismDefinitionEqual(
	left, right *cityRealtimeCharacterMetabolismDefinition,
) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.IntervalUS == right.IntervalUS &&
		left.EnergyDeltaMilli == right.EnergyDeltaMilli &&
		left.SatietyDeltaMilli == right.SatietyDeltaMilli &&
		left.MoraleDeltaMilli == right.MoraleDeltaMilli &&
		cityRealtimeCharacterMetabolismDefinitionValid(*left)
}

func cityRealtimeCharacterMetabolismDefinitionValid(definition cityRealtimeCharacterMetabolismDefinition) bool {
	return definition.IntervalUS >= cityRealtimeCharacterMetabolismMinimumIntervalUS &&
		definition.IntervalUS%cityRealtimeTimeQuantumUS == 0 &&
		definition.EnergyDeltaMilli >= -1000 && definition.EnergyDeltaMilli <= 0 &&
		definition.SatietyDeltaMilli >= -1000 && definition.SatietyDeltaMilli <= 0 &&
		definition.MoraleDeltaMilli >= -1000 && definition.MoraleDeltaMilli <= 0 &&
		(definition.EnergyDeltaMilli < 0 || definition.SatietyDeltaMilli < 0 || definition.MoraleDeltaMilli < 0)
}

func cityRealtimeCharacterActivityDefinitionEqual(
	left, right cityRealtimeCharacterActivityDefinition,
) bool {
	if left.Code != right.Code || left.CategoryCode != right.CategoryCode ||
		left.LocationRequirement != right.LocationRequirement || left.PublicVisibility != right.PublicVisibility ||
		left.MinimumIntervalUS != right.MinimumIntervalUS || left.MinimumEnergyMilli != right.MinimumEnergyMilli ||
		left.MinimumSatietyMilli != right.MinimumSatietyMilli || left.EnergyDeltaMilli != right.EnergyDeltaMilli ||
		left.SatietyDeltaMilli != right.SatietyDeltaMilli || left.MoraleDeltaMilli != right.MoraleDeltaMilli ||
		left.CivicStandingDelta != right.CivicStandingDelta || left.CityCreditDelta != right.CityCreditDelta ||
		left.ItemCode != right.ItemCode || left.ItemQuantityDelta != right.ItemQuantityDelta ||
		!cityRealtimeCharacterActivityProgressionDefinitionEqual(left.Progression, right.Progression) {
		return false
	}
	if left.Law == nil || right.Law == nil {
		return left.Law == nil && right.Law == nil
	}
	return left.Law.RuleCode == right.Law.RuleCode && left.Law.Disposition == right.Law.Disposition &&
		left.Law.PenaltyCityCreditUnits == right.Law.PenaltyCityCreditUnits &&
		left.Law.StandingDeltaMilli == right.Law.StandingDeltaMilli
}

func cityRealtimeCharacterActivityBindingHash(binding cityRealtimeCharacterActivityCatalogBinding) string {
	return cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeCharacterActivityBindingVersion,
		binding.CatalogID,
		binding.CatalogVersion,
		binding.CatalogHash,
	}, "\x1f")))
}

func validateCityRealtimeCharacterActivityBinding(binding cityRealtimeCharacterActivityCatalogBinding) bool {
	return binding.CatalogID == cityRealtimeCharacterActivityCatalogID &&
		cityRealtimeCharacterActivityCatalogVersionSupported(binding.CatalogVersion) &&
		cityRealtimeSHA256Hex(binding.CatalogHash) && cityRealtimeSHA256Hex(binding.BindingHash) &&
		binding.BindingHash == cityRealtimeCharacterActivityBindingHash(binding)
}

func cityRealtimeCharacterActivityCatalogVersionSupported(version string) bool {
	_, supported := cityRealtimeCharacterActivityDefinitionsForVersion(version)
	return supported
}

func cityRealtimeCharacterProfileStateHash(profile cityRealtimeCharacterProfile) (string, error) {
	schemaVersion := profile.StateSchemaVersion
	if schemaVersion == 0 {
		schemaVersion = cityRealtimeCharacterProfileSchemaLegacy
	}
	inventory := make([]map[string]any, 0, len(profile.Inventory))
	for _, stack := range profile.Inventory {
		inventory = append(inventory, map[string]any{
			"item_code":           stack.ItemCode,
			"quantity":            stack.Quantity,
			"revision":            stack.Revision,
			"last_frame_sequence": stack.LastFrameSequence,
			"state_hash":          stack.StateHash,
		})
	}
	value := map[string]any{
		"schema_version":              schemaVersion,
		"actor_code":                  profile.ActorCode,
		"energy_milli":                profile.EnergyMilli,
		"satiety_milli":               profile.SatietyMilli,
		"morale_milli":                profile.MoraleMilli,
		"civic_standing_milli":        profile.CivicStandingMilli,
		"city_credit_units":           profile.CityCreditUnits,
		"revision":                    profile.Revision,
		"activity_revision":           profile.ActivityRevision,
		"law_revision":                profile.LawRevision,
		"spawned_frame_sequence":      profile.SpawnedFrameSequence,
		"last_frame_sequence":         profile.LastFrameSequence,
		"last_activity_world_time_us": profile.LastActivityWorldTimeUS,
		"activity_event_chain_hash":   profile.ActivityEventChainHash,
		"law_event_chain_hash":        profile.LawEventChainHash,
		"inventory":                   inventory,
	}
	switch schemaVersion {
	case cityRealtimeCharacterProfileSchemaLegacy:
	case cityRealtimeCharacterProfileSchemaMetabolism:
		value["metabolism_revision"] = profile.MetabolismRevision
		value["last_metabolism_world_time_us"] = profile.LastMetabolismWorldTimeUS
	case cityRealtimeCharacterProfileSchemaProgression:
		value["metabolism_revision"] = profile.MetabolismRevision
		value["last_metabolism_world_time_us"] = profile.LastMetabolismWorldTimeUS
		value["archetype_code"] = profile.ArchetypeCode
		value["progression_revision"] = profile.ProgressionRevision
		value["progression_event_chain_hash"] = profile.ProgressionEventChainHash
		value["progression_state_hash"] = profile.ProgressionStateHash
	default:
		return "", ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_profile_schema"})
	}
	_, hash, err := cityRealtimeCanonicalJSONObject(value)
	if err != nil {
		return "", fmt.Errorf("canonicalize realtime character life profile: %w", err)
	}
	return hash, nil
}

func cityRealtimeCharacterInventoryStateHash(actorCode string, stack cityRealtimeCharacterInventoryStack) (string, error) {
	_, hash, err := cityRealtimeCanonicalJSONObject(map[string]any{
		"schema_version":      cityRealtimeCharacterLifeSchemaVersion,
		"actor_code":          actorCode,
		"item_code":           stack.ItemCode,
		"quantity":            stack.Quantity,
		"revision":            stack.Revision,
		"last_frame_sequence": stack.LastFrameSequence,
	})
	if err != nil {
		return "", fmt.Errorf("canonicalize realtime character inventory stack: %w", err)
	}
	return hash, nil
}

func cityRealtimeCharacterActivityChainGenesisHash(actorCode string, frameSequence int64) (string, error) {
	_, hash, err := cityRealtimeCanonicalJSONObject(map[string]any{
		"schema_version": cityRealtimeCharacterLifeSchemaVersion,
		"namespace":      cityRealtimeCharacterLifeActivityChainNamespace,
		"actor_code":     actorCode,
		"frame_sequence": frameSequence,
	})
	if err != nil {
		return "", fmt.Errorf("canonicalize realtime character activity genesis: %w", err)
	}
	return hash, nil
}

func cityRealtimeCharacterLawChainGenesisHash(actorCode string, frameSequence int64) (string, error) {
	_, hash, err := cityRealtimeCanonicalJSONObject(map[string]any{
		"schema_version": cityRealtimeCharacterLifeSchemaVersion,
		"namespace":      cityRealtimeCharacterLifeLawChainNamespace,
		"actor_code":     actorCode,
		"frame_sequence": frameSequence,
	})
	if err != nil {
		return "", fmt.Errorf("canonicalize realtime character law genesis: %w", err)
	}
	return hash, nil
}

func cityRealtimeCharacterActivityEventHash(event cityRealtimeCharacterActivityEventRecord) (string, error) {
	value := map[string]any{
		"schema_version":             cityRealtimeCharacterLifeSchemaVersion,
		"actor_code":                 event.ActorCode,
		"event_sequence":             event.EventSequence,
		"frame_sequence":             event.FrameSequence,
		"activity_code":              event.ActivityCode,
		"category_code":              event.CategoryCode,
		"outcome":                    event.Outcome,
		"public_visibility":          event.PublicVisibility,
		"energy_delta_milli":         event.EnergyDeltaMilli,
		"satiety_delta_milli":        event.SatietyDeltaMilli,
		"morale_delta_milli":         event.MoraleDeltaMilli,
		"civic_standing_delta_milli": event.CivicStandingDeltaMilli,
		"city_credit_delta_units":    event.CityCreditDeltaUnits,
		"item_quantity_delta":        event.ItemQuantityDelta,
		"previous_event_hash":        event.PreviousEventHash,
	}
	if event.ItemCode != nil {
		value["item_code"] = *event.ItemCode
	}
	if event.LawCaseCode != nil {
		value["law_case_code"] = *event.LawCaseCode
	}
	_, hash, err := cityRealtimeCanonicalJSONObject(value)
	if err != nil {
		return "", fmt.Errorf("canonicalize realtime character activity event: %w", err)
	}
	return hash, nil
}

func cityRealtimeCharacterLawEventHash(event cityRealtimeCharacterLawEventRecord) (string, error) {
	_, hash, err := cityRealtimeCanonicalJSONObject(map[string]any{
		"schema_version":            cityRealtimeCharacterLifeSchemaVersion,
		"actor_code":                event.ActorCode,
		"event_sequence":            event.EventSequence,
		"activity_event_sequence":   event.ActivityEventSequence,
		"frame_sequence":            event.FrameSequence,
		"case_code":                 event.CaseCode,
		"rule_code":                 event.RuleCode,
		"disposition":               event.Disposition,
		"penalty_city_credit_units": event.PenaltyCityCreditUnits,
		"standing_delta_milli":      event.StandingDeltaMilli,
		"public_visibility":         event.PublicVisibility,
		"previous_event_hash":       event.PreviousEventHash,
	})
	if err != nil {
		return "", fmt.Errorf("canonicalize realtime character law event: %w", err)
	}
	return hash, nil
}

func cityRealtimeCharacterProfileValid(profile cityRealtimeCharacterProfile) bool {
	if profile.StateSchemaVersion != cityRealtimeCharacterProfileSchemaLegacy &&
		profile.StateSchemaVersion != cityRealtimeCharacterProfileSchemaMetabolism &&
		profile.StateSchemaVersion != cityRealtimeCharacterProfileSchemaProgression {
		return false
	}
	if !cityRealtimePlayerActorCodeValid(profile.ActorCode) ||
		profile.EnergyMilli < 0 || profile.EnergyMilli > 1000 ||
		profile.SatietyMilli < 0 || profile.SatietyMilli > 1000 ||
		profile.MoraleMilli < 0 || profile.MoraleMilli > 1000 ||
		profile.CivicStandingMilli < 0 || profile.CivicStandingMilli > 1000 ||
		profile.CityCreditUnits < cityRealtimeCharacterLifeMinimumCreditUnits ||
		profile.CityCreditUnits > cityRealtimeCharacterLifeMaximumCreditUnits ||
		profile.Revision <= 0 || profile.ActivityRevision < 0 || profile.LawRevision < 0 || profile.MetabolismRevision < 0 ||
		profile.ProgressionRevision < 0 || profile.ActivityRevision >= profile.Revision || profile.LawRevision >= profile.Revision ||
		profile.MetabolismRevision >= profile.Revision || profile.ProgressionRevision >= profile.Revision ||
		profile.SpawnedFrameSequence <= 0 || profile.LastFrameSequence < profile.SpawnedFrameSequence ||
		profile.LastActivityWorldTimeUS < 0 || profile.LastMetabolismWorldTimeUS < 0 || !cityRealtimeSHA256Hex(profile.StateHash) ||
		!cityRealtimeSHA256Hex(profile.ActivityEventChainHash) || !cityRealtimeSHA256Hex(profile.LawEventChainHash) ||
		profile.Inventory == nil {
		return false
	}
	if profile.StateSchemaVersion == cityRealtimeCharacterProfileSchemaLegacy &&
		(profile.MetabolismRevision != 0 || profile.LastMetabolismWorldTimeUS != 0 ||
			profile.ProgressionRevision != 0 || profile.ArchetypeCode != "" ||
			profile.ProgressionEventChainHash != "" || profile.ProgressionStateHash != "" ||
			len(profile.Attributes) != 0 || len(profile.Roles) != 0) {
		return false
	}
	if profile.StateSchemaVersion == cityRealtimeCharacterProfileSchemaMetabolism &&
		(profile.ProgressionRevision != 0 || profile.ArchetypeCode != "" ||
			profile.ProgressionEventChainHash != "" || profile.ProgressionStateHash != "" ||
			len(profile.Attributes) != 0 || len(profile.Roles) != 0) {
		return false
	}
	for index, stack := range profile.Inventory {
		if !cityRealtimeCharacterInventoryStackValid(stack) ||
			(index > 0 && profile.Inventory[index-1].ItemCode >= stack.ItemCode) {
			return false
		}
	}
	if profile.StateSchemaVersion == cityRealtimeCharacterProfileSchemaProgression &&
		!cityRealtimeCharacterProgressionProfileValid(profile) {
		return false
	}
	return true
}

func cityRealtimeCharacterInventoryStackValid(stack cityRealtimeCharacterInventoryStack) bool {
	return strings.HasPrefix(stack.ItemCode, "item.") && len(stack.ItemCode) <= 64 &&
		stack.Quantity >= 0 && stack.Quantity <= 1000000 && stack.Revision > 0 &&
		stack.LastFrameSequence > 0 && cityRealtimeSHA256Hex(stack.StateHash)
}

func (profile cityRealtimeCharacterProfile) projection() CityRealtimeCharacterLife {
	items := make([]CityRealtimeCharacterInventoryStack, 0, len(profile.Inventory))
	for _, stack := range profile.Inventory {
		items = append(items, CityRealtimeCharacterInventoryStack{
			ItemCode: stack.ItemCode, Quantity: stack.Quantity, Revision: stack.Revision,
			LastFrameSequence: stack.LastFrameSequence,
		})
	}
	return CityRealtimeCharacterLife{
		EnergyMilli: profile.EnergyMilli, SatietyMilli: profile.SatietyMilli,
		MoraleMilli: profile.MoraleMilli, CivicStandingMilli: profile.CivicStandingMilli,
		CityCreditUnits: profile.CityCreditUnits, Revision: profile.Revision,
		ActivityRevision: profile.ActivityRevision, LawRevision: profile.LawRevision,
		MetabolismRevision:        profile.MetabolismRevision,
		LastFrameSequence:         profile.LastFrameSequence,
		LastActivityWorldTimeUS:   profile.LastActivityWorldTimeUS,
		LastMetabolismWorldTimeUS: profile.LastMetabolismWorldTimeUS,
		Inventory:                 items,
	}
}

func loadCityRealtimeCharacterProfile(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	actorCode string,
	forUpdate bool,
) (cityRealtimeCharacterProfile, bool, error) {
	if worldID <= 0 || !strings.HasPrefix(actorCode, cityRealtimePlayerActorCodePrefix) {
		return cityRealtimeCharacterProfile{}, false, ErrCityInvalidInput
	}
	query := `
SELECT state_schema_version, actor_code, energy_milli, satiety_milli, morale_milli,
       civic_standing_milli, city_credit_units, revision, activity_revision,
       law_revision, metabolism_revision, spawned_frame_sequence, last_frame_sequence,
       last_activity_world_time_us, last_metabolism_world_time_us, state_hash, activity_event_chain_hash,
       law_event_chain_hash, COALESCE(archetype_code, ''), progression_revision,
       COALESCE(progression_event_chain_hash, ''), COALESCE(progression_state_hash, '')
FROM city_realtime_character_profiles
WHERE world_id = $1 AND actor_code = $2`
	if forUpdate {
		query += " FOR UPDATE"
	}
	profile := cityRealtimeCharacterProfile{Inventory: make([]cityRealtimeCharacterInventoryStack, 0)}
	err := queryer.QueryRowContext(ctx, query, worldID, actorCode).Scan(
		&profile.StateSchemaVersion, &profile.ActorCode, &profile.EnergyMilli, &profile.SatietyMilli, &profile.MoraleMilli,
		&profile.CivicStandingMilli, &profile.CityCreditUnits, &profile.Revision, &profile.ActivityRevision,
		&profile.LawRevision, &profile.MetabolismRevision, &profile.SpawnedFrameSequence, &profile.LastFrameSequence,
		&profile.LastActivityWorldTimeUS, &profile.LastMetabolismWorldTimeUS, &profile.StateHash, &profile.ActivityEventChainHash,
		&profile.LawEventChainHash, &profile.ArchetypeCode, &profile.ProgressionRevision,
		&profile.ProgressionEventChainHash, &profile.ProgressionStateHash,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return cityRealtimeCharacterProfile{}, false, nil
	}
	if err != nil {
		return cityRealtimeCharacterProfile{}, false, fmt.Errorf("load realtime character profile: %w", err)
	}
	stackQuery := `
SELECT item_code, quantity, revision, last_frame_sequence, state_hash
FROM city_realtime_character_inventory_stacks
WHERE world_id = $1 AND actor_code = $2
ORDER BY item_code ASC`
	if forUpdate {
		stackQuery += " FOR UPDATE"
	}
	rows, err := queryer.QueryContext(ctx, stackQuery, worldID, actorCode)
	if err != nil {
		return cityRealtimeCharacterProfile{}, false, fmt.Errorf("load realtime character inventory: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		stack := cityRealtimeCharacterInventoryStack{}
		if err = rows.Scan(&stack.ItemCode, &stack.Quantity, &stack.Revision, &stack.LastFrameSequence, &stack.StateHash); err != nil {
			return cityRealtimeCharacterProfile{}, false, err
		}
		profile.Inventory = append(profile.Inventory, stack)
	}
	if err = rows.Err(); err != nil {
		return cityRealtimeCharacterProfile{}, false, fmt.Errorf("iterate realtime character inventory: %w", err)
	}
	if profile.StateSchemaVersion == cityRealtimeCharacterProfileSchemaProgression {
		profile.Attributes, profile.Roles, err = loadCityRealtimeCharacterProgressionState(
			ctx, queryer, worldID, actorCode, forUpdate,
		)
		if err != nil {
			return cityRealtimeCharacterProfile{}, false, err
		}
	}
	if !cityRealtimeCharacterProfileValid(profile) {
		return cityRealtimeCharacterProfile{}, false, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_profile"})
	}
	expectedStateHash, hashErr := cityRealtimeCharacterProfileStateHash(profile)
	if hashErr != nil || expectedStateHash != profile.StateHash {
		return cityRealtimeCharacterProfile{}, false, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_profile_hash"}).WithCause(hashErr)
	}
	for _, stack := range profile.Inventory {
		expectedStackHash, stackHashErr := cityRealtimeCharacterInventoryStateHash(profile.ActorCode, stack)
		if stackHashErr != nil || expectedStackHash != stack.StateHash {
			return cityRealtimeCharacterProfile{}, false, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_inventory_hash"}).WithCause(stackHashErr)
		}
	}
	if err = validateCityRealtimeCharacterLifeEventHeads(ctx, queryer, worldID, profile); err != nil {
		return cityRealtimeCharacterProfile{}, false, err
	}
	return profile, true, nil
}

func validateCityRealtimeCharacterLifeEventHeads(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	profile cityRealtimeCharacterProfile,
) error {
	activityGenesis, err := cityRealtimeCharacterActivityChainGenesisHash(profile.ActorCode, profile.SpawnedFrameSequence)
	if err != nil {
		return err
	}
	lawGenesis, err := cityRealtimeCharacterLawChainGenesisHash(profile.ActorCode, profile.SpawnedFrameSequence)
	if err != nil {
		return err
	}
	if err = validateCityRealtimeCharacterLifeEventHead(ctx, queryer, `
SELECT event_sequence, event_hash
FROM city_realtime_character_activity_events
WHERE world_id = $1 AND actor_code = $2
ORDER BY event_sequence DESC LIMIT 1`, worldID, profile.ActorCode,
		profile.ActivityRevision, profile.ActivityEventChainHash, activityGenesis); err != nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_activity_head"}).WithCause(err)
	}
	if err = validateCityRealtimeCharacterLifeEventHead(ctx, queryer, `
SELECT event_sequence, event_hash
FROM city_realtime_character_law_events
WHERE world_id = $1 AND actor_code = $2
ORDER BY event_sequence DESC LIMIT 1`, worldID, profile.ActorCode,
		profile.LawRevision, profile.LawEventChainHash, lawGenesis); err != nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_law_head"}).WithCause(err)
	}
	if profile.StateSchemaVersion == cityRealtimeCharacterProfileSchemaProgression {
		progressionGenesis, progressionErr := cityRealtimeCharacterProgressionChainGenesisHash(
			profile.ActorCode, profile.SpawnedFrameSequence,
		)
		if progressionErr != nil {
			return progressionErr
		}
		if err = validateCityRealtimeCharacterLifeEventHead(ctx, queryer, `
SELECT event_sequence, event_hash
FROM city_realtime_character_progression_events
WHERE world_id = $1 AND actor_code = $2
ORDER BY event_sequence DESC LIMIT 1`, worldID, profile.ActorCode,
			profile.ProgressionRevision, profile.ProgressionEventChainHash, progressionGenesis); err != nil {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_progression_head"}).WithCause(err)
		}
	}
	return nil
}

func validateCityRealtimeCharacterLifeEventHead(
	ctx context.Context,
	queryer citySQLQueryer,
	query string,
	worldID int64,
	actorCode string,
	expectedRevision int64,
	expectedHash string,
	genesisHash string,
) error {
	var sequence int64
	var hash string
	err := queryer.QueryRowContext(ctx, query, worldID, actorCode).Scan(&sequence, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		if expectedRevision == 0 && expectedHash == genesisHash {
			return nil
		}
		return errors.New("event history is absent")
	}
	if err != nil {
		return err
	}
	if sequence != expectedRevision || hash != expectedHash {
		return errors.New("event head does not match profile")
	}
	return nil
}

func loadCityRealtimeCharacterLifeHashState(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*cityRealtimeCharacterLifeHashState, error) {
	runtime, err := loadCityRealtimeCharacterLifeRuntime(ctx, queryer, worldID)
	if err != nil || runtime == nil {
		return nil, err
	}
	state := &cityRealtimeCharacterLifeHashState{
		SchemaVersion: cityRealtimeCharacterLifeSchemaVersion,
		Binding:       &runtime.Binding,
		Profiles:      make([]cityRealtimeCharacterLifeHash, 0),
	}
	if runtime.Metabolism != nil {
		state.Metabolism = make([]cityRealtimeCharacterMetabolismHash, 0)
	}
	rows, err := queryer.QueryContext(ctx, `
SELECT actor_code
FROM city_realtime_character_profiles
WHERE world_id = $1
ORDER BY actor_code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load realtime character life profiles: %w", err)
	}
	actorCodes := make([]string, 0)
	for rows.Next() {
		var actorCode string
		if err = rows.Scan(&actorCode); err != nil {
			_ = rows.Close()
			return nil, err
		}
		actorCodes = append(actorCodes, actorCode)
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("iterate realtime character life profiles: %w", err)
	}
	if err = rows.Close(); err != nil {
		return nil, fmt.Errorf("close realtime character life profile rows: %w", err)
	}
	for _, actorCode := range actorCodes {
		profile, found, loadErr := loadCityRealtimeCharacterProfile(ctx, queryer, worldID, actorCode, false)
		if loadErr != nil {
			return nil, loadErr
		}
		if !found {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_profile_row"})
		}
		item := cityRealtimeCharacterLifeHash{
			ActorCode: profile.ActorCode, EnergyMilli: profile.EnergyMilli, SatietyMilli: profile.SatietyMilli,
			MoraleMilli: profile.MoraleMilli, CivicStandingMilli: profile.CivicStandingMilli,
			CityCreditUnits: profile.CityCreditUnits, Revision: profile.Revision,
			ActivityRevision: profile.ActivityRevision, LawRevision: profile.LawRevision,
			SpawnedFrameSequence: profile.SpawnedFrameSequence, LastFrameSequence: profile.LastFrameSequence,
			LastActivityWorldTimeUS: profile.LastActivityWorldTimeUS, StateHash: profile.StateHash,
			ActivityEventChainHash: profile.ActivityEventChainHash, LawEventChainHash: profile.LawEventChainHash,
			Inventory: append([]cityRealtimeCharacterInventoryStack(nil), profile.Inventory...),
		}
		if profile.StateSchemaVersion == cityRealtimeCharacterProfileSchemaProgression {
			item.Progression = &cityRealtimeCharacterProgressionHash{
				ArchetypeCode: profile.ArchetypeCode, Revision: profile.ProgressionRevision,
				EventChainHash: profile.ProgressionEventChainHash, StateHash: profile.ProgressionStateHash,
				Attributes: append([]cityRealtimeCharacterAttributeState(nil), profile.Attributes...),
				Roles:      append([]cityRealtimeCharacterRoleAssignment(nil), profile.Roles...),
			}
		}
		state.Profiles = append(state.Profiles, item)
		if runtime.Metabolism != nil {
			state.Metabolism = append(state.Metabolism, cityRealtimeCharacterMetabolismHash{
				ActorCode: profile.ActorCode, StateSchemaVersion: profile.StateSchemaVersion,
				MetabolismRevision:        profile.MetabolismRevision,
				LastMetabolismWorldTimeUS: profile.LastMetabolismWorldTimeUS,
				StateHash:                 profile.StateHash,
			})
		}
	}
	if err = validateCityRealtimeCharacterLifeHashState(state); err != nil {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_life_state"}).WithCause(err)
	}
	return state, nil
}

func validateCityRealtimeCharacterLifeHashState(state *cityRealtimeCharacterLifeHashState) error {
	if state == nil || state.SchemaVersion != cityRealtimeCharacterLifeSchemaVersion || state.Binding == nil || state.Profiles == nil ||
		!validateCityRealtimeCharacterActivityBinding(*state.Binding) {
		return errors.New("invalid realtime character life hash state")
	}
	spec, supported := cityRealtimeCharacterActivityCatalogSpecForVersion(state.Binding.CatalogVersion)
	if !supported {
		return errors.New("unsupported realtime character life catalog")
	}
	metabolismByActor := make(map[string]cityRealtimeCharacterMetabolismHash, len(state.Metabolism))
	if spec.Metabolism == nil {
		if len(state.Metabolism) != 0 {
			return errors.New("legacy realtime character life state has metabolism")
		}
	} else {
		if len(state.Metabolism) != len(state.Profiles) {
			return errors.New("realtime character metabolism state is incomplete")
		}
		expectedProfileSchema := cityRealtimeCharacterProfileSchemaMetabolism
		if spec.Progression != nil {
			expectedProfileSchema = cityRealtimeCharacterProfileSchemaProgression
		}
		for index, item := range state.Metabolism {
			if item.ActorCode == "" || item.StateSchemaVersion != expectedProfileSchema ||
				item.MetabolismRevision < 0 || item.LastMetabolismWorldTimeUS < 0 ||
				!cityRealtimeSHA256Hex(item.StateHash) ||
				(index > 0 && state.Metabolism[index-1].ActorCode >= item.ActorCode) {
				return errors.New("invalid or unordered realtime character metabolism state")
			}
			metabolismByActor[item.ActorCode] = item
		}
	}
	for index, item := range state.Profiles {
		profile := cityRealtimeCharacterProfile{
			ActorCode: item.ActorCode, EnergyMilli: item.EnergyMilli, SatietyMilli: item.SatietyMilli,
			MoraleMilli: item.MoraleMilli, CivicStandingMilli: item.CivicStandingMilli,
			CityCreditUnits: item.CityCreditUnits, Revision: item.Revision, ActivityRevision: item.ActivityRevision,
			LawRevision: item.LawRevision, SpawnedFrameSequence: item.SpawnedFrameSequence,
			LastFrameSequence: item.LastFrameSequence, LastActivityWorldTimeUS: item.LastActivityWorldTimeUS,
			StateHash: item.StateHash, ActivityEventChainHash: item.ActivityEventChainHash,
			LawEventChainHash: item.LawEventChainHash, Inventory: item.Inventory,
		}
		if spec.Metabolism == nil {
			if item.Progression != nil {
				return errors.New("legacy realtime character life state has progression")
			}
			profile.StateSchemaVersion = cityRealtimeCharacterProfileSchemaLegacy
		} else {
			metabolism, found := metabolismByActor[item.ActorCode]
			if !found || metabolism.StateHash != item.StateHash {
				return errors.New("realtime character metabolism profile mismatch")
			}
			profile.StateSchemaVersion = metabolism.StateSchemaVersion
			profile.MetabolismRevision = metabolism.MetabolismRevision
			profile.LastMetabolismWorldTimeUS = metabolism.LastMetabolismWorldTimeUS
			if spec.Progression != nil {
				if item.Progression == nil {
					return errors.New("realtime character progression state is incomplete")
				}
				profile.ArchetypeCode = item.Progression.ArchetypeCode
				profile.ProgressionRevision = item.Progression.Revision
				profile.ProgressionEventChainHash = item.Progression.EventChainHash
				profile.ProgressionStateHash = item.Progression.StateHash
				profile.Attributes = append([]cityRealtimeCharacterAttributeState(nil), item.Progression.Attributes...)
				profile.Roles = append([]cityRealtimeCharacterRoleAssignment(nil), item.Progression.Roles...)
			} else if item.Progression != nil {
				return errors.New("pre-progression realtime character state has progression")
			}
		}
		if !cityRealtimeCharacterProfileValid(profile) ||
			(index > 0 && state.Profiles[index-1].ActorCode >= item.ActorCode) {
			return errors.New("invalid or unordered realtime character life profile")
		}
		if spec.Progression != nil && !cityRealtimeCharacterProfileMatchesProgressionRuntime(profile, &cityRealtimeCharacterLifeRuntime{
			Metabolism: spec.Metabolism, Progression: spec.Progression,
		}) {
			return errors.New("realtime character progression profile mismatch")
		}
		expected, err := cityRealtimeCharacterProfileStateHash(profile)
		if err != nil || expected != profile.StateHash {
			return errors.New("realtime character life profile hash mismatch")
		}
	}
	return nil
}

func seedCityRealtimeCharacterLife(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	actorCode string,
	frameSequence, worldTimeUS int64,
	runtime *cityRealtimeCharacterLifeRuntime,
	archetypeCode string,
) (cityRealtimeCharacterProfile, error) {
	if tx == nil || worldID <= 0 || frameSequence <= 0 || worldTimeUS < 0 ||
		!cityRealtimePlayerActorCodeValid(actorCode) || runtime == nil {
		return cityRealtimeCharacterProfile{}, ErrCityInvalidInput
	}
	stateSchemaVersion := cityRealtimeCharacterProfileSchemaLegacy
	lastMetabolismWorldTimeUS := int64(0)
	if runtime.Metabolism != nil {
		if !cityRealtimeCharacterMetabolismDefinitionValid(*runtime.Metabolism) {
			return cityRealtimeCharacterProfile{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{
				"field": "realtime_character_metabolism_catalog",
			})
		}
		stateSchemaVersion = cityRealtimeCharacterProfileSchemaMetabolism
		lastMetabolismWorldTimeUS = worldTimeUS
	}
	archetype, err := cityRealtimeCharacterResolveArchetype(runtime, archetypeCode)
	if err != nil {
		return cityRealtimeCharacterProfile{}, err
	}
	activityChainHash, err := cityRealtimeCharacterActivityChainGenesisHash(actorCode, frameSequence)
	if err != nil {
		return cityRealtimeCharacterProfile{}, err
	}
	lawChainHash, err := cityRealtimeCharacterLawChainGenesisHash(actorCode, frameSequence)
	if err != nil {
		return cityRealtimeCharacterProfile{}, err
	}
	profile := cityRealtimeCharacterProfile{
		StateSchemaVersion: stateSchemaVersion, ActorCode: actorCode, EnergyMilli: cityRealtimeCharacterInitialEnergyMilli,
		SatietyMilli: cityRealtimeCharacterInitialSatietyMilli, MoraleMilli: cityRealtimeCharacterInitialMoraleMilli,
		CivicStandingMilli: cityRealtimeCharacterInitialCivicStandingMilli,
		CityCreditUnits:    cityRealtimeCharacterInitialCityCreditUnits, Revision: 1,
		ActivityRevision: 0, LawRevision: 0, MetabolismRevision: 0, SpawnedFrameSequence: frameSequence,
		LastFrameSequence: frameSequence, LastActivityWorldTimeUS: worldTimeUS,
		LastMetabolismWorldTimeUS: lastMetabolismWorldTimeUS,
		ActivityEventChainHash:    activityChainHash, LawEventChainHash: lawChainHash,
		Inventory: []cityRealtimeCharacterInventoryStack{{
			ItemCode: cityRealtimeCharacterRationItemCode, Quantity: cityRealtimeCharacterInitialRationQuantity,
			Revision: 1, LastFrameSequence: frameSequence,
		}},
	}
	if cityRealtimeCharacterProgressionRuntimeEnabled(runtime) {
		if err = cityRealtimeCharacterSeedProgression(&profile, runtime, archetype, frameSequence); err != nil {
			return cityRealtimeCharacterProfile{}, err
		}
	}
	for index := range profile.Inventory {
		profile.Inventory[index].StateHash, err = cityRealtimeCharacterInventoryStateHash(profile.ActorCode, profile.Inventory[index])
		if err != nil {
			return cityRealtimeCharacterProfile{}, err
		}
	}
	profile.StateHash, err = cityRealtimeCharacterProfileStateHash(profile)
	if err != nil {
		return cityRealtimeCharacterProfile{}, err
	}
	if !cityRealtimeCharacterProfileValid(profile) {
		return cityRealtimeCharacterProfile{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_life_seed"})
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_realtime_character_profiles
    (world_id, state_schema_version, actor_code, energy_milli, satiety_milli, morale_milli,
     civic_standing_milli, city_credit_units, revision, activity_revision,
     law_revision, metabolism_revision, spawned_frame_sequence, last_frame_sequence,
     last_activity_world_time_us, last_metabolism_world_time_us, state_hash, activity_event_chain_hash,
     law_event_chain_hash, archetype_code, progression_revision, progression_event_chain_hash,
     progression_state_hash, metadata)

VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14,
        $15, $16, $17, $18, $19, $20, $21, $22, $23, '{}'::jsonb)`,
		worldID, profile.StateSchemaVersion, profile.ActorCode, profile.EnergyMilli, profile.SatietyMilli, profile.MoraleMilli,
		profile.CivicStandingMilli, profile.CityCreditUnits, profile.Revision, profile.ActivityRevision,
		profile.LawRevision, profile.MetabolismRevision, profile.SpawnedFrameSequence, profile.LastFrameSequence,
		profile.LastActivityWorldTimeUS, profile.LastMetabolismWorldTimeUS, profile.StateHash, profile.ActivityEventChainHash,
		profile.LawEventChainHash, nullableCityRealtimeCharacterProgressionString(profile.ArchetypeCode),
		profile.ProgressionRevision, nullableCityRealtimeCharacterProgressionString(profile.ProgressionEventChainHash),
		nullableCityRealtimeCharacterProgressionString(profile.ProgressionStateHash),
	); err != nil {
		return cityRealtimeCharacterProfile{}, fmt.Errorf("seed realtime character life profile: %w", err)
	}
	for _, stack := range profile.Inventory {
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_realtime_character_inventory_stacks
    (world_id, actor_code, item_code, quantity, revision, last_frame_sequence, state_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, '{}'::jsonb)`,
			worldID, profile.ActorCode, stack.ItemCode, stack.Quantity, stack.Revision,
			stack.LastFrameSequence, stack.StateHash,
		); err != nil {
			return cityRealtimeCharacterProfile{}, fmt.Errorf("seed realtime character inventory %s: %w", stack.ItemCode, err)
		}
	}
	if profile.StateSchemaVersion == cityRealtimeCharacterProfileSchemaProgression {
		if err = seedCityRealtimeCharacterProgressionState(ctx, tx, worldID, profile); err != nil {
			return cityRealtimeCharacterProfile{}, err
		}
	}
	return profile, nil
}

func enableCityRealtimeCharacterActivityMutationGates(
	ctx context.Context,
	tx *sql.Tx,
	worldID, frameSequence int64,
) error {
	if tx == nil || worldID <= 0 || frameSequence <= 0 {
		return ErrCityInvalidInput
	}
	for _, setting := range []struct {
		name  string
		value int64
	}{
		{name: "sub2api.city_realtime_character_activity_world_id", value: worldID},
		{name: "sub2api.city_realtime_character_activity_frame_sequence", value: frameSequence},
	} {
		if _, err := tx.ExecContext(ctx, `SELECT set_config($1, $2, TRUE)`, setting.name, strconv.FormatInt(setting.value, 10)); err != nil {
			return fmt.Errorf("activate realtime character activity gate %s: %w", setting.name, err)
		}
	}
	return nil
}

func cityRealtimeCharacterActivityAvailability(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, worldTimeUS int64,
	actorState cityRealtimeActorState,
	profile cityRealtimeCharacterProfile,
	runtime *cityRealtimeCharacterLifeRuntime,
) ([]CityRealtimeCharacterActivityAvailability, error) {
	if worldID <= 0 || worldTimeUS < 0 || !cityRealtimeCharacterProfileValid(profile) ||
		!cityRealtimeActorStateValid(actorState) || runtime == nil || len(runtime.Definitions) == 0 {
		return nil, ErrCityInvalidInput
	}
	cell, err := cityRealtimeCharacterActivityLocationContext(ctx, queryer, worldID, cityRealtimeActorSpawnCandidate{
		X: actorState.X, Y: actorState.Y, Z: actorState.Z,
	})
	if err != nil {
		return nil, err
	}
	codes := make([]string, 0, len(runtime.Definitions))
	for code := range runtime.Definitions {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	items := make([]CityRealtimeCharacterActivityAvailability, 0, len(codes))
	for _, code := range codes {
		definition := runtime.Definitions[code]
		availability := CityRealtimeCharacterActivityAvailability{Code: definition.Code, CategoryCode: definition.CategoryCode, Available: true}
		if definition.Progression != nil {
			availability.RequiredRoleCodes = append([]string(nil), definition.Progression.RequiredRoleCodes...)
		}
		if profile.ActivityRevision > 0 && worldTimeUS < profile.LastActivityWorldTimeUS+definition.MinimumIntervalUS {
			availability.Available = false
			availability.ReasonCode = "cooldown"
			availability.CooldownRemainingUS = profile.LastActivityWorldTimeUS + definition.MinimumIntervalUS - worldTimeUS
		} else if profile.EnergyMilli < definition.MinimumEnergyMilli || profile.SatietyMilli < definition.MinimumSatietyMilli {
			availability.Available = false
			availability.ReasonCode = "needs"
		} else if !cityRealtimeCharacterActivityLocationAllowed(definition, cell) {
			availability.Available = false
			availability.ReasonCode = "location"
		} else if definition.ItemQuantityDelta < 0 &&
			cityRealtimeCharacterInventoryQuantity(profile.Inventory, definition.ItemCode) < -definition.ItemQuantityDelta {
			availability.Available = false
			availability.ReasonCode = "inventory"
		} else if available, reason := cityRealtimeCharacterActivityProgressionAvailable(profile, runtime, definition); !available {
			availability.Available = false
			availability.ReasonCode = reason
		}
		items = append(items, availability)
	}
	return items, nil
}

type cityRealtimeCharacterSurfaceCell struct {
	Traversable bool
	TerrainID   string
	Indoor      bool
}

// cityRealtimeCharacterActivityLocationContext keeps the activity catalog's
// existing `traversable` rule meaningful inside immutable generated buildings
// without treating a wall, window, furnishing, or arbitrary upper-floor
// coordinate as a usable location. Road-specific work remains surface-only.
func cityRealtimeCharacterActivityLocationContext(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	position cityRealtimeActorSpawnCandidate,
) (cityRealtimeCharacterSurfaceCell, error) {
	interior, found, err := loadCityRealtimeCharacterInteriorAt(ctx, queryer, worldID, position)
	if err != nil {
		return cityRealtimeCharacterSurfaceCell{}, err
	}
	if found {
		return cityRealtimeCharacterSurfaceCell{
			Traversable: cityRealtimeCharacterInteriorCellTraversable(interior.Cell),
			Indoor:      true,
		}, nil
	}
	return cityRealtimeCharacterSurfaceCellContext(ctx, queryer, worldID, position)
}

func cityRealtimeCharacterSurfaceCellContext(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	position cityRealtimeActorSpawnCandidate,
) (cityRealtimeCharacterSurfaceCell, error) {
	if position.Z != cityspatial.SurfaceZ {
		return cityRealtimeCharacterSurfaceCell{}, nil
	}
	address, err := cityspatial.SplitWorldCoordinate(cityspatial.WorldCoordinate{
		X: position.X, Y: position.Y, Z: position.Z,
	}, cityspatial.DefaultChunkSize)
	if err != nil {
		return cityRealtimeCharacterSurfaceCell{}, err
	}
	var rawPayload []byte
	err = queryer.QueryRowContext(ctx, `
SELECT payload
FROM city_realtime_spatial_chunks
WHERE world_id = $1 AND chunk_x = $2 AND chunk_y = $3 AND z = $4`,
		worldID, address.Chunk.X, address.Chunk.Y, position.Z,
	).Scan(&rawPayload)
	if errors.Is(err, sql.ErrNoRows) {
		return cityRealtimeCharacterSurfaceCell{}, nil
	}
	if err != nil {
		return cityRealtimeCharacterSurfaceCell{}, fmt.Errorf("load realtime character activity cell chunk: %w", err)
	}
	payload := cityspatial.OpenWorldChunkPayload{}
	if err = json.Unmarshal(rawPayload, &payload); err != nil {
		return cityRealtimeCharacterSurfaceCell{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_activity_payload"}).WithCause(err)
	}
	if err = cityspatial.ValidateOpenWorldChunkPayload(payload); err != nil {
		return cityRealtimeCharacterSurfaceCell{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_activity_payload"}).WithCause(err)
	}
	cellIndex := int(address.Local.Y)*payload.Width + int(address.Local.X)
	if cellIndex < 0 || cellIndex >= payload.Width*payload.Height {
		return cityRealtimeCharacterSurfaceCell{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_activity_cell"})
	}
	for _, layer := range payload.Layers {
		if layer.Kind == cityspatial.RuleKindStructure && layer.X == address.Local.X && layer.Y == address.Local.Y {
			return cityRealtimeCharacterSurfaceCell{}, nil
		}
	}
	terrainID, ok := cityRealtimeTerrainDefinitionAt(payload.TerrainRuns, cellIndex)
	if !ok {
		return cityRealtimeCharacterSurfaceCell{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_activity_terrain"})
	}
	cell := cityRealtimeCharacterSurfaceCell{TerrainID: terrainID}
	switch terrainID {
	case "terrain.road", "terrain.sidewalk", "terrain.grass", "terrain.ground", "terrain.soil":
		cell.Traversable = true
	}
	return cell, nil
}

func cityRealtimeCharacterActivityLocationAllowed(
	definition cityRealtimeCharacterActivityDefinition,
	cell cityRealtimeCharacterSurfaceCell,
) bool {
	if !cell.Traversable {
		return false
	}
	switch definition.LocationRequirement {
	case "traversable":
		return true
	case "road_or_sidewalk":
		return cell.TerrainID == "terrain.road" || cell.TerrainID == "terrain.sidewalk"
	default:
		return false
	}
}

func cityRealtimeCharacterInventoryQuantity(stacks []cityRealtimeCharacterInventoryStack, itemCode string) int64 {
	for _, stack := range stacks {
		if stack.ItemCode == itemCode {
			return stack.Quantity
		}
	}
	return 0
}

func (s *CityEconomyService) PerformRealtimeCharacterActivity(
	ctx context.Context,
	input CityRealtimeCharacterActivityInput,
) (*CityRealtimeCharacterMutationResult, error) {
	activityCode := strings.TrimSpace(input.ActivityCode)
	idempotencyKey, err := normalizeCityRealtimeCharacterIdempotencyKey(input.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	if input.UserID <= 0 || input.WorldID <= 0 || !cityRealtimeAgentIdentifierValid(activityCode, 64) {
		return nil, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "activity_code"})
	}
	requestHash, err := cityRealtimeCharacterRequestHash(cityRealtimeCharacterActivityAction, map[string]any{
		"world_id": input.WorldID,
		"user_id":  input.UserID,
		"activity": activityCode,
	})
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin realtime character activity transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err = lockCityRealtimeCharacterWorld(ctx, tx, input.UserID, input.WorldID); err != nil {
		return nil, err
	}
	world, err := lockCityWorld(ctx, tx, input.UserID, input.WorldID)
	if err != nil {
		return nil, err
	}
	state, err := lockCityRealtimeState(ctx, tx, input.WorldID)
	if err != nil {
		return nil, err
	}
	if err = validateCityRealtimeCharacterCommandWindow(world, state); err != nil {
		return nil, err
	}
	if receipt, found, receiptErr := loadCityRealtimeCharacterActionReceipt(ctx, tx, input.WorldID, input.UserID, idempotencyKey); receiptErr != nil {
		return nil, receiptErr
	} else if found {
		return completeCityRealtimeCharacterReceipt(tx, receipt, cityRealtimeCharacterActivityAction, requestHash)
	}
	agentState, err := loadCityRealtimeAgentHashState(ctx, tx, input.WorldID)
	if err != nil {
		return nil, err
	}
	lifeRuntime, err := loadCityRealtimeCharacterLifeRuntime(ctx, tx, input.WorldID)
	if err != nil {
		return nil, err
	}
	if agentState == nil || agentState.Binding == nil || lifeRuntime == nil {
		return nil, ErrCityRealtimeCharacterRuntimeUnavailable
	}
	definition, exists := lifeRuntime.Definitions[activityCode]
	if !exists {
		return nil, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "activity_code"})
	}
	record, found, err := loadCityRealtimeOwnedCharacter(ctx, tx, input.WorldID, input.UserID, true)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrCityRealtimeCharacterNotFound
	}
	if record.agent.LifecycleStatus != "active" || record.identity.LifecycleStatus != "active" || record.agent.ControlMode != "manual" {
		return nil, ErrCityRealtimeCharacterControlUnavailable
	}
	profile, found, err := loadCityRealtimeCharacterProfile(ctx, tx, input.WorldID, record.identity.ActorCode, true)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrCityRealtimeCharacterRuntimeUnavailable
	}
	if !cityRealtimeCharacterProfileMatchesRuntime(profile, lifeRuntime) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_profile_runtime"})
	}
	availability, err := cityRealtimeCharacterActivityAvailability(ctx, tx, input.WorldID, state.currentWorldTimeUS, record.state, profile, lifeRuntime)
	if err != nil {
		return nil, err
	}
	if item := cityRealtimeCharacterActivityAvailabilityByCode(availability, activityCode); item == nil || !item.Available {
		metadata := map[string]string{"field": "activity_code"}
		if item != nil && item.ReasonCode != "" {
			metadata["reason"] = item.ReasonCode
		}
		return nil, ErrCityRealtimeCharacterActivityUnavailable.WithMetadata(metadata)
	}
	if err = cityRealtimeRejectPendingDueAtCurrentTime(ctx, tx, input.WorldID, state.currentWorldTimeUS); err != nil {
		return nil, err
	}
	frameSequence, cursor, err := cityRealtimeCharacterNextFrame(state)
	if err != nil {
		return nil, err
	}
	if err = enableCityRealtimeCharacterActivityMutationGates(ctx, tx, input.WorldID, frameSequence); err != nil {
		return nil, err
	}
	transition, err := cityRealtimeCharacterApplyActivityWithRuntime(
		profile, lifeRuntime, definition, frameSequence, state.currentWorldTimeUS,
	)
	if err != nil {
		return nil, err
	}
	if transition.Inventory != nil {
		if err = updateCityRealtimeCharacterInventoryStack(ctx, tx, input.WorldID, record.identity.ActorCode, *transition.Inventory); err != nil {
			return nil, err
		}
	}
	for _, attribute := range transition.AttributeUpdates {
		if err = updateCityRealtimeCharacterAttributeState(ctx, tx, input.WorldID, record.identity.ActorCode, attribute); err != nil {
			return nil, err
		}
	}
	if err = insertCityRealtimeCharacterActivityEvent(ctx, tx, input.WorldID, record.identity.ActorCode, transition.Activity); err != nil {
		return nil, err
	}
	if transition.Law != nil {
		if err = insertCityRealtimeCharacterLawEvent(ctx, tx, input.WorldID, record.identity.ActorCode, *transition.Law); err != nil {
			return nil, err
		}
	}
	evidenceCaptured := false
	if transition.Law != nil {
		if evidenceCaptured, err = captureCityRealtimeCharacterCaseEvidenceFromLaw(
			ctx, tx, input.WorldID, frameSequence, state.currentWorldTimeUS, *transition.Law,
		); err != nil {
			return nil, err
		}
	}
	if transition.ProgressionEvent != nil {
		if err = insertCityRealtimeCharacterProgressionEvent(ctx, tx, input.WorldID, record.identity.ActorCode, *transition.ProgressionEvent); err != nil {
			return nil, err
		}
	}
	if err = updateCityRealtimeCharacterProfile(ctx, tx, input.WorldID, profile, transition.Profile); err != nil {
		return nil, err
	}
	life, lifeErr := cityRealtimeCharacterLifeProjection(transition.Profile, lifeRuntime)
	if lifeErr != nil {
		return nil, lifeErr
	}
	result := &CityRealtimeCharacterMutationResult{
		Character: record.projection(), Life: cityRealtimeCharacterLifePointer(life), Activity: &transition.Result,
	}
	if result.Frame, err = sealCityRealtimeCharacterFrame(ctx, tx, input.WorldID, world, state, frameSequence, cursor,
		cityRealtimeCharacterActivityAction, map[string]any{
			"character_created": 0, "character_moved": 0, "character_activity": 1,
			"character_law_event":         boolToCityRealtimeCount(transition.Law != nil),
			"character_case_evidence":     boolToCityRealtimeCount(evidenceCaptured),
			"character_progression_event": boolToCityRealtimeCount(transition.ProgressionEvent != nil),
		}); err != nil {
		return nil, err
	}
	if err = canonicalizeCityRealtimeCharacterMutationResult(result); err != nil {
		return nil, err
	}
	if err = storeCityRealtimeCharacterActionReceipt(ctx, tx, input.WorldID, input.UserID, idempotencyKey,
		record.identity.ActorCode, cityRealtimeCharacterActivityAction, requestHash, frameSequence, *result); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit realtime character activity: %w", err)
	}
	return result, nil
}

// ListRealtimeMyCharacterEvents returns the caller's own sealed activity and
// rule history. It intentionally resolves ownership from the session-bound
// Character Agent rather than accepting an Actor code from the browser.
func (s *CityEconomyService) ListRealtimeMyCharacterEvents(
	ctx context.Context,
	input CityRealtimeCharacterEventListInput,
) (*CityRealtimeCharacterEventPage, error) {
	limit, err := normalizeCityRealtimeCharacterActivityEventLimit(input.Limit)
	if err != nil {
		return nil, err
	}
	if input.UserID <= 0 || input.WorldID <= 0 || input.BeforeSequence < 0 {
		return nil, ErrCityInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, fmt.Errorf("begin realtime character event projection: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err = requireCityRealtimeWorldRead(ctx, tx, input.UserID, input.WorldID); err != nil {
		return nil, err
	}
	runtime, err := loadCityRealtimeCharacterLifeRuntime(ctx, tx, input.WorldID)
	if err != nil {
		return nil, err
	}
	if runtime == nil {
		return nil, ErrCityRealtimeCharacterRuntimeUnavailable
	}
	record, found, err := loadCityRealtimeOwnedCharacter(ctx, tx, input.WorldID, input.UserID, false)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrCityRealtimeCharacterNotFound
	}
	profile, found, err := loadCityRealtimeCharacterProfile(ctx, tx, input.WorldID, record.identity.ActorCode, false)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrCityRealtimeCharacterRuntimeUnavailable
	}
	if !cityRealtimeCharacterProfileMatchesRuntime(profile, runtime) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_profile_runtime"})
	}
	rows, err := tx.QueryContext(ctx, `
SELECT activity.event_sequence, activity.frame_sequence, activity.activity_code,
       activity.category_code, activity.outcome, activity.public_visibility,
       activity.energy_delta_milli, activity.satiety_delta_milli,
       activity.morale_delta_milli, activity.civic_standing_delta_milli,
       activity.city_credit_delta_units, COALESCE(activity.item_code, ''),
       activity.item_quantity_delta, COALESCE(activity.law_case_code, ''),
       COALESCE(law.rule_code, ''), COALESCE(law.disposition, ''),
       COALESCE(law.penalty_city_credit_units, 0)
FROM city_realtime_character_activity_events activity
LEFT JOIN city_realtime_character_law_events law
  ON law.world_id = activity.world_id
 AND law.actor_code = activity.actor_code
 AND law.activity_event_sequence = activity.event_sequence
WHERE activity.world_id = $1
  AND activity.actor_code = $2
  AND ($3 = 0 OR activity.event_sequence < $3)
ORDER BY activity.event_sequence DESC
LIMIT $4`, input.WorldID, record.identity.ActorCode, input.BeforeSequence, limit+1)
	if err != nil {
		return nil, fmt.Errorf("list realtime character private activity events: %w", err)
	}
	defer func() { _ = rows.Close() }()
	page := &CityRealtimeCharacterEventPage{Items: make([]CityRealtimeCharacterActivityEvent, 0, limit)}
	for rows.Next() {
		item := CityRealtimeCharacterActivityEvent{}
		if err = rows.Scan(
			&item.Sequence, &item.FrameSequence, &item.ActivityCode, &item.CategoryCode,
			&item.Outcome, &item.PublicVisibility, &item.EnergyDeltaMilli,
			&item.SatietyDeltaMilli, &item.MoraleDeltaMilli, &item.CivicStandingDeltaMilli,
			&item.CityCreditDeltaUnits, &item.ItemCode, &item.ItemQuantityDelta,
			&item.LawCaseCode, &item.LawRuleCode, &item.LawDisposition,
			&item.LawPenaltyCreditUnits,
		); err != nil {
			return nil, fmt.Errorf("scan realtime character private activity event: %w", err)
		}
		if err = validateCityRealtimeCharacterActivityEventProjection(item, runtime.Definitions); err != nil {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_activity_event_projection"}).WithCause(err)
		}
		page.Items = append(page.Items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate realtime character private activity events: %w", err)
	}
	if len(page.Items) > limit {
		next := page.Items[limit-1].Sequence
		page.NextBeforeSequence = &next
		page.Items = page.Items[:limit]
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit realtime character private activity projection: %w", err)
	}
	return page, nil
}

// ListRealtimePublicCharacterEvents exposes the member-safe public portion of
// shared player activity. Need values, inventory, city-credit deltas, owner
// relationships, and Agent data are deliberately absent from this stream.
func (s *CityEconomyService) ListRealtimePublicCharacterEvents(
	ctx context.Context,
	input CityRealtimePublicCharacterEventListInput,
) (*CityRealtimePublicCharacterEventPage, error) {
	limit, err := normalizeCityRealtimeCharacterActivityEventLimit(input.Limit)
	if err != nil {
		return nil, err
	}
	if input.UserID <= 0 || input.WorldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	cursor, err := parseCityRealtimePublicCharacterEventCursor(input.BeforeCursor)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, fmt.Errorf("begin realtime public character event projection: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err = requireCityRealtimeWorldRead(ctx, tx, input.UserID, input.WorldID); err != nil {
		return nil, err
	}
	runtime, err := loadCityRealtimeCharacterLifeRuntime(ctx, tx, input.WorldID)
	if err != nil {
		return nil, err
	}
	page := &CityRealtimePublicCharacterEventPage{Items: make([]CityRealtimePublicCharacterEvent, 0, limit)}
	if runtime == nil {
		if err = tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit historical realtime public character event projection: %w", err)
		}
		return page, nil
	}
	rows, err := tx.QueryContext(ctx, `
SELECT activity.frame_sequence, activity.actor_code, identity.public_label,
       activity.event_sequence, activity.activity_code, activity.category_code,
       activity.outcome, COALESCE(law.rule_code, ''), COALESCE(law.disposition, '')
FROM city_realtime_character_activity_events activity
JOIN city_realtime_actor_identities identity
  ON identity.world_id = activity.world_id AND identity.actor_code = activity.actor_code
LEFT JOIN city_realtime_character_law_events law
  ON law.world_id = activity.world_id
 AND law.actor_code = activity.actor_code
 AND law.activity_event_sequence = activity.event_sequence
 AND law.public_visibility = TRUE
WHERE activity.world_id = $1
  AND activity.public_visibility = TRUE
  AND (
      $2 = 0
      OR activity.frame_sequence < $2
      OR (activity.frame_sequence = $2 AND activity.actor_code < $3)
      OR (activity.frame_sequence = $2 AND activity.actor_code = $3 AND activity.event_sequence < $4)
  )
ORDER BY activity.frame_sequence DESC, activity.actor_code DESC, activity.event_sequence DESC
LIMIT $5`, input.WorldID, cursor.FrameSequence, cursor.ActorCode, cursor.EventSequence, limit+1)
	if err != nil {
		return nil, fmt.Errorf("list realtime public character events: %w", err)
	}
	defer func() { _ = rows.Close() }()
	cursors := make([]cityRealtimePublicCharacterEventCursor, 0, limit+1)
	for rows.Next() {
		item := CityRealtimePublicCharacterEvent{}
		var eventSequence int64
		if err = rows.Scan(
			&item.FrameSequence, &item.ActorCode, &item.PublicLabel, &eventSequence,
			&item.ActivityCode, &item.CategoryCode, &item.Outcome,
			&item.LawRuleCode, &item.LawDisposition,
		); err != nil {
			return nil, fmt.Errorf("scan realtime public character event: %w", err)
		}
		if err = validateCityRealtimePublicCharacterEventProjection(item, runtime.Definitions); err != nil {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_public_character_event_projection"}).WithCause(err)
		}
		page.Items = append(page.Items, item)
		cursors = append(cursors, cityRealtimePublicCharacterEventCursor{
			FrameSequence: item.FrameSequence, ActorCode: item.ActorCode, EventSequence: eventSequence,
		})
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate realtime public character events: %w", err)
	}
	if len(page.Items) > limit {
		lastCursor := cursors[limit-1]
		page.Items = page.Items[:limit]
		next := lastCursor.String()
		page.NextCursor = &next
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit realtime public character event projection: %w", err)
	}
	return page, nil
}

func normalizeCityRealtimeCharacterActivityEventLimit(value int) (int, error) {
	if value == 0 {
		return cityRealtimeCharacterActivityDefaultEventLimit, nil
	}
	if value < 1 || value > cityRealtimeCharacterActivityMaximumEventLimit {
		return 0, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "limit"})
	}
	return value, nil
}

type cityRealtimePublicCharacterEventCursor struct {
	FrameSequence int64
	ActorCode     string
	EventSequence int64
}

func parseCityRealtimePublicCharacterEventCursor(value string) (cityRealtimePublicCharacterEventCursor, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return cityRealtimePublicCharacterEventCursor{}, nil
	}
	parts := strings.Split(value, "|")
	if len(parts) != 3 {
		return cityRealtimePublicCharacterEventCursor{}, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "before_cursor"})
	}
	frameSequence, frameErr := strconv.ParseInt(parts[0], 10, 64)
	eventSequence, eventErr := strconv.ParseInt(parts[2], 10, 64)
	if frameErr != nil || eventErr != nil || frameSequence <= 0 || eventSequence <= 0 ||
		!cityRealtimePlayerActorCodeValid(parts[1]) {
		return cityRealtimePublicCharacterEventCursor{}, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "before_cursor"})
	}
	return cityRealtimePublicCharacterEventCursor{
		FrameSequence: frameSequence, ActorCode: parts[1], EventSequence: eventSequence,
	}, nil
}

func (cursor cityRealtimePublicCharacterEventCursor) String() string {
	return strconv.FormatInt(cursor.FrameSequence, 10) + "|" + cursor.ActorCode + "|" + strconv.FormatInt(cursor.EventSequence, 10)
}

func validateCityRealtimeCharacterActivityEventProjection(
	event CityRealtimeCharacterActivityEvent,
	definitions map[string]cityRealtimeCharacterActivityDefinition,
) error {
	definition, found := definitions[event.ActivityCode]
	if !found || event.Sequence <= 0 || event.FrameSequence <= 0 ||
		event.CategoryCode != definition.CategoryCode ||
		event.PublicVisibility != definition.PublicVisibility ||
		(event.Outcome != "completed" && event.Outcome != "penalized") ||
		event.EnergyDeltaMilli < -1000 || event.EnergyDeltaMilli > 1000 ||
		event.SatietyDeltaMilli < -1000 || event.SatietyDeltaMilli > 1000 ||
		event.MoraleDeltaMilli < -1000 || event.MoraleDeltaMilli > 1000 ||
		event.CivicStandingDeltaMilli < -1000 || event.CivicStandingDeltaMilli > 1000 ||
		event.CityCreditDeltaUnits < -1000000 || event.CityCreditDeltaUnits > 1000000 {
		return errors.New("invalid activity event projection")
	}
	if definition.ItemCode == "" {
		if event.ItemCode != "" || event.ItemQuantityDelta != 0 {
			return errors.New("unexpected activity inventory projection")
		}
	} else if event.ItemCode != definition.ItemCode || event.ItemQuantityDelta != definition.ItemQuantityDelta {
		return errors.New("invalid activity inventory projection")
	}
	if definition.Law == nil {
		if event.Outcome != "completed" || event.LawCaseCode != "" || event.LawRuleCode != "" ||
			event.LawDisposition != "" || event.LawPenaltyCreditUnits != 0 {
			return errors.New("unexpected activity law projection")
		}
		return nil
	}
	if event.Outcome != "penalized" || event.LawCaseCode == "" || event.LawRuleCode != definition.Law.RuleCode ||
		event.LawDisposition != definition.Law.Disposition || event.LawPenaltyCreditUnits != definition.Law.PenaltyCityCreditUnits {
		return errors.New("invalid activity law projection")
	}
	return nil
}

func validateCityRealtimePublicCharacterEventProjection(
	event CityRealtimePublicCharacterEvent,
	definitions map[string]cityRealtimeCharacterActivityDefinition,
) error {
	definition, found := definitions[event.ActivityCode]
	if !found || !definition.PublicVisibility || event.FrameSequence <= 0 ||
		!cityRealtimePlayerActorCodeValid(event.ActorCode) || !cityRealtimeActorPublicLabelValid(event.PublicLabel) ||
		event.CategoryCode != definition.CategoryCode || (event.Outcome != "completed" && event.Outcome != "penalized") {
		return errors.New("invalid public activity event projection")
	}
	if definition.Law == nil {
		if event.Outcome != "completed" || event.LawRuleCode != "" || event.LawDisposition != "" {
			return errors.New("unexpected public activity law projection")
		}
		return nil
	}
	if event.Outcome != "penalized" || event.LawRuleCode != definition.Law.RuleCode ||
		event.LawDisposition != definition.Law.Disposition {
		return errors.New("invalid public activity law projection")
	}
	return nil
}

func cityRealtimeCharacterLifePointer(value CityRealtimeCharacterLife) *CityRealtimeCharacterLife {
	return &value
}

func boolToCityRealtimeCount(value bool) int {
	if value {
		return 1
	}
	return 0
}

func cityRealtimeCharacterActivityAvailabilityByCode(
	items []CityRealtimeCharacterActivityAvailability,
	code string,
) *CityRealtimeCharacterActivityAvailability {
	for index := range items {
		if items[index].Code == code {
			return &items[index]
		}
	}
	return nil
}

func cityRealtimeCharacterApplyActivity(
	profile cityRealtimeCharacterProfile,
	definition cityRealtimeCharacterActivityDefinition,
	frameSequence, worldTimeUS int64,
) (cityRealtimeCharacterProfile, cityRealtimeCharacterActivityEventRecord, *cityRealtimeCharacterLawEventRecord, *cityRealtimeCharacterInventoryStack, CityRealtimeCharacterActivityResult, error) {
	if !cityRealtimeCharacterProfileValid(profile) || frameSequence <= profile.LastFrameSequence || worldTimeUS < profile.LastActivityWorldTimeUS {
		return cityRealtimeCharacterProfile{}, cityRealtimeCharacterActivityEventRecord{}, nil, nil, CityRealtimeCharacterActivityResult{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_activity_state"})
	}
	if profile.EnergyMilli < definition.MinimumEnergyMilli || profile.SatietyMilli < definition.MinimumSatietyMilli {
		return cityRealtimeCharacterProfile{}, cityRealtimeCharacterActivityEventRecord{}, nil, nil, CityRealtimeCharacterActivityResult{}, ErrCityRealtimeCharacterActivityUnavailable.WithMetadata(map[string]string{"reason": "needs"})
	}
	next := profile
	next.Revision++
	next.ActivityRevision++
	next.LastFrameSequence = frameSequence
	next.LastActivityWorldTimeUS = worldTimeUS
	next.EnergyMilli = clampCityRealtimeCharacterMilli(profile.EnergyMilli + definition.EnergyDeltaMilli)
	next.SatietyMilli = clampCityRealtimeCharacterMilli(profile.SatietyMilli + definition.SatietyDeltaMilli)
	next.MoraleMilli = clampCityRealtimeCharacterMilli(profile.MoraleMilli + definition.MoraleDeltaMilli)
	next.CivicStandingMilli = clampCityRealtimeCharacterMilli(profile.CivicStandingMilli + definition.CivicStandingDelta)
	credit, err := addCityRealtimeCharacterCredit(profile.CityCreditUnits, definition.CityCreditDelta)
	if err != nil {
		return cityRealtimeCharacterProfile{}, cityRealtimeCharacterActivityEventRecord{}, nil, nil, CityRealtimeCharacterActivityResult{}, err
	}
	next.CityCreditUnits = credit

	var nextInventory *cityRealtimeCharacterInventoryStack
	if definition.ItemQuantityDelta != 0 {
		stackIndex := -1
		for index := range next.Inventory {
			if next.Inventory[index].ItemCode == definition.ItemCode {
				stackIndex = index
				break
			}
		}
		if stackIndex < 0 || definition.ItemQuantityDelta < 0 && next.Inventory[stackIndex].Quantity < -definition.ItemQuantityDelta {
			return cityRealtimeCharacterProfile{}, cityRealtimeCharacterActivityEventRecord{}, nil, nil, CityRealtimeCharacterActivityResult{}, ErrCityRealtimeCharacterActivityUnavailable.WithMetadata(map[string]string{"reason": "inventory"})
		}
		stack := next.Inventory[stackIndex]
		stack.Quantity += definition.ItemQuantityDelta
		stack.Revision++
		stack.LastFrameSequence = frameSequence
		stack.StateHash, err = cityRealtimeCharacterInventoryStateHash(profile.ActorCode, stack)
		if err != nil {
			return cityRealtimeCharacterProfile{}, cityRealtimeCharacterActivityEventRecord{}, nil, nil, CityRealtimeCharacterActivityResult{}, err
		}
		next.Inventory[stackIndex] = stack
		nextInventory = &stack
	}

	var lawEvent *cityRealtimeCharacterLawEventRecord
	var lawCaseCode *string
	if definition.Law != nil {
		if definition.Law.PenaltyCityCreditUnits != -definition.CityCreditDelta ||
			definition.Law.StandingDeltaMilli != definition.CivicStandingDelta {
			return cityRealtimeCharacterProfile{}, cityRealtimeCharacterActivityEventRecord{}, nil, nil, CityRealtimeCharacterActivityResult{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_activity_law_definition"})
		}
		next.LawRevision++
		caseCode, caseErr := cityRealtimeCharacterLawCaseCode(profile.ActorCode, next.LawRevision)
		if caseErr != nil {
			return cityRealtimeCharacterProfile{}, cityRealtimeCharacterActivityEventRecord{}, nil, nil, CityRealtimeCharacterActivityResult{}, caseErr
		}
		lawCaseCode = &caseCode
		law := cityRealtimeCharacterLawEventRecord{
			ActorCode: profile.ActorCode, EventSequence: next.LawRevision, ActivityEventSequence: next.ActivityRevision,
			FrameSequence: frameSequence, CaseCode: caseCode, RuleCode: definition.Law.RuleCode,
			Disposition: definition.Law.Disposition, PenaltyCityCreditUnits: definition.Law.PenaltyCityCreditUnits,
			StandingDeltaMilli: definition.Law.StandingDeltaMilli, PublicVisibility: definition.PublicVisibility,
			PreviousEventHash: profile.LawEventChainHash,
		}
		law.EventHash, err = cityRealtimeCharacterLawEventHash(law)
		if err != nil {
			return cityRealtimeCharacterProfile{}, cityRealtimeCharacterActivityEventRecord{}, nil, nil, CityRealtimeCharacterActivityResult{}, err
		}
		next.LawEventChainHash = law.EventHash
		lawEvent = &law
	}

	activity := cityRealtimeCharacterActivityEventRecord{
		ActorCode: profile.ActorCode, EventSequence: next.ActivityRevision, FrameSequence: frameSequence,
		ActivityCode: definition.Code, CategoryCode: definition.CategoryCode,
		Outcome: "completed", PublicVisibility: definition.PublicVisibility,
		EnergyDeltaMilli:        next.EnergyMilli - profile.EnergyMilli,
		SatietyDeltaMilli:       next.SatietyMilli - profile.SatietyMilli,
		MoraleDeltaMilli:        next.MoraleMilli - profile.MoraleMilli,
		CivicStandingDeltaMilli: next.CivicStandingMilli - profile.CivicStandingMilli,
		CityCreditDeltaUnits:    next.CityCreditUnits - profile.CityCreditUnits,
		ItemQuantityDelta:       definition.ItemQuantityDelta, PreviousEventHash: profile.ActivityEventChainHash,
		LawCaseCode: lawCaseCode,
	}
	if definition.ItemCode != "" {
		itemCode := definition.ItemCode
		activity.ItemCode = &itemCode
	}
	if lawEvent != nil {
		activity.Outcome = "penalized"
	}
	activity.EventHash, err = cityRealtimeCharacterActivityEventHash(activity)
	if err != nil {
		return cityRealtimeCharacterProfile{}, cityRealtimeCharacterActivityEventRecord{}, nil, nil, CityRealtimeCharacterActivityResult{}, err
	}
	next.ActivityEventChainHash = activity.EventHash
	next.StateHash, err = cityRealtimeCharacterProfileStateHash(next)
	if err != nil {
		return cityRealtimeCharacterProfile{}, cityRealtimeCharacterActivityEventRecord{}, nil, nil, CityRealtimeCharacterActivityResult{}, err
	}
	if !cityRealtimeCharacterProfileValid(next) {
		return cityRealtimeCharacterProfile{}, cityRealtimeCharacterActivityEventRecord{}, nil, nil, CityRealtimeCharacterActivityResult{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_activity_next_state"})
	}
	result := CityRealtimeCharacterActivityResult{
		Code: activity.ActivityCode, CategoryCode: activity.CategoryCode, Outcome: activity.Outcome,
		PublicVisibility: activity.PublicVisibility, EnergyDeltaMilli: activity.EnergyDeltaMilli,
		SatietyDeltaMilli: activity.SatietyDeltaMilli, MoraleDeltaMilli: activity.MoraleDeltaMilli,
		CivicStandingDeltaMilli: activity.CivicStandingDeltaMilli,
		CityCreditDeltaUnits:    activity.CityCreditDeltaUnits, ItemQuantityDelta: activity.ItemQuantityDelta,
	}
	if activity.ItemCode != nil {
		result.ItemCode = *activity.ItemCode
	}
	if activity.LawCaseCode != nil {
		result.LawCaseCode = *activity.LawCaseCode
	}
	return next, activity, lawEvent, nextInventory, result, nil
}

func clampCityRealtimeCharacterMilli(value int64) int64 {
	if value < 0 {
		return 0
	}
	if value > 1000 {
		return 1000
	}
	return value
}

func addCityRealtimeCharacterCredit(balance, delta int64) (int64, error) {
	if delta > 0 && balance > cityRealtimeCharacterLifeMaximumCreditUnits-delta ||
		delta < 0 && balance < cityRealtimeCharacterLifeMinimumCreditUnits-delta {
		return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_city_credit"})
	}
	next := balance + delta
	if next < cityRealtimeCharacterLifeMinimumCreditUnits || next > cityRealtimeCharacterLifeMaximumCreditUnits {
		return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_city_credit"})
	}
	return next, nil
}

func cityRealtimeCharacterLawCaseCode(actorCode string, sequence int64) (string, error) {
	_, hash, err := cityRealtimeCanonicalJSONObject(map[string]any{
		"schema_version": cityRealtimeCharacterLifeSchemaVersion,
		"actor_code":     actorCode,
		"law_sequence":   sequence,
	})
	if err != nil {
		return "", fmt.Errorf("canonicalize realtime character law case: %w", err)
	}
	return "law." + hash[:16] + "." + strconv.FormatInt(sequence, 10), nil
}

func updateCityRealtimeCharacterInventoryStack(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	actorCode string,
	next cityRealtimeCharacterInventoryStack,
) error {
	result, err := tx.ExecContext(ctx, `
UPDATE city_realtime_character_inventory_stacks
SET quantity = $4, revision = $5, last_frame_sequence = $6, state_hash = $7, updated_at = NOW()
WHERE world_id = $1 AND actor_code = $2 AND item_code = $3
  AND revision = $8 AND last_frame_sequence < $6`,
		worldID, actorCode, next.ItemCode, next.Quantity, next.Revision,
		next.LastFrameSequence, next.StateHash, next.Revision-1,
	)
	if err != nil {
		return fmt.Errorf("update realtime character inventory stack: %w", err)
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr != nil {
		return fmt.Errorf("check realtime character inventory update: %w", rowsErr)
	} else if rows != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_inventory_revision"})
	}
	return nil
}

func insertCityRealtimeCharacterActivityEvent(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	actorCode string,
	event cityRealtimeCharacterActivityEventRecord,
) error {
	if actorCode == "" || event.ActorCode != actorCode || event.EventSequence <= 0 || event.FrameSequence <= 0 || !cityRealtimeSHA256Hex(event.EventHash) ||
		!cityRealtimeSHA256Hex(event.PreviousEventHash) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_activity_event"})
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_realtime_character_activity_events
    (world_id, actor_code, event_sequence, frame_sequence, activity_code,
     category_code, outcome, public_visibility, energy_delta_milli,
     satiety_delta_milli, morale_delta_milli, civic_standing_delta_milli,
     city_credit_delta_units, item_code, item_quantity_delta, law_case_code,
     previous_event_hash, event_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
        $14, $15, $16, $17, $18, '{}'::jsonb)`,
		worldID, actorCode, event.EventSequence, event.FrameSequence, event.ActivityCode,
		event.CategoryCode, event.Outcome, event.PublicVisibility, event.EnergyDeltaMilli,
		event.SatietyDeltaMilli, event.MoraleDeltaMilli, event.CivicStandingDeltaMilli,
		event.CityCreditDeltaUnits, event.ItemCode, event.ItemQuantityDelta, event.LawCaseCode,
		event.PreviousEventHash, event.EventHash,
	); err != nil {
		return fmt.Errorf("append realtime character activity event: %w", err)
	}
	return nil
}

func insertCityRealtimeCharacterLawEvent(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	actorCode string,
	event cityRealtimeCharacterLawEventRecord,
) error {
	if actorCode == "" || event.ActorCode != actorCode || event.EventSequence <= 0 || event.ActivityEventSequence <= 0 || event.FrameSequence <= 0 ||
		!cityRealtimeSHA256Hex(event.EventHash) || !cityRealtimeSHA256Hex(event.PreviousEventHash) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_law_event"})
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_realtime_character_law_events
    (world_id, actor_code, event_sequence, activity_event_sequence, frame_sequence,
     case_code, rule_code, disposition, penalty_city_credit_units,
     standing_delta_milli, public_visibility, previous_event_hash, event_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, '{}'::jsonb)`,
		worldID, actorCode, event.EventSequence, event.ActivityEventSequence, event.FrameSequence,
		event.CaseCode, event.RuleCode, event.Disposition, event.PenaltyCityCreditUnits,
		event.StandingDeltaMilli, event.PublicVisibility, event.PreviousEventHash, event.EventHash,
	); err != nil {
		return fmt.Errorf("append realtime character law event: %w", err)
	}
	return nil
}

func updateCityRealtimeCharacterProfile(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	previous, next cityRealtimeCharacterProfile,
) error {
	result, err := tx.ExecContext(ctx, `
UPDATE city_realtime_character_profiles
SET state_schema_version = $3, energy_milli = $4, satiety_milli = $5, morale_milli = $6,
    civic_standing_milli = $7, city_credit_units = $8, revision = $9,
    activity_revision = $10, law_revision = $11, metabolism_revision = $12,
    last_frame_sequence = $13, last_activity_world_time_us = $14,
    last_metabolism_world_time_us = $15, state_hash = $16,
    activity_event_chain_hash = $17, law_event_chain_hash = $18,
    progression_revision = $19, progression_event_chain_hash = $20,
    progression_state_hash = $21, updated_at = NOW()
WHERE world_id = $1 AND actor_code = $2 AND revision = $22
  AND last_frame_sequence = $23 AND state_schema_version = $24`,
		worldID, next.ActorCode, next.StateSchemaVersion, next.EnergyMilli, next.SatietyMilli, next.MoraleMilli,
		next.CivicStandingMilli, next.CityCreditUnits, next.Revision, next.ActivityRevision,
		next.LawRevision, next.MetabolismRevision, next.LastFrameSequence, next.LastActivityWorldTimeUS,
		next.LastMetabolismWorldTimeUS, next.StateHash, next.ActivityEventChainHash, next.LawEventChainHash,
		next.ProgressionRevision, nullableCityRealtimeCharacterProgressionString(next.ProgressionEventChainHash),
		nullableCityRealtimeCharacterProgressionString(next.ProgressionStateHash), previous.Revision,
		previous.LastFrameSequence, previous.StateSchemaVersion,
	)
	if err != nil {
		return fmt.Errorf("advance realtime character life profile: %w", err)
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr != nil {
		return fmt.Errorf("check realtime character profile update: %w", rowsErr)
	} else if rows != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_profile_revision"})
	}
	return nil
}
