package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	CityCommandTypeActorCreate          = "actor.create"
	CityCommandTypeActorActivityPerform = "actor.activity.perform"
	CityCommandTypeActorRoleTransition  = "actor.role.transition"

	WorldRuntimeDefinitionActorType = "actor_type"
	WorldRuntimeDefinitionArchetype = "archetype"
	WorldRuntimeDefinitionAttribute = "attribute"
	WorldRuntimeDefinitionActivity  = "activity"
	WorldRuntimeDefinitionRole      = "role"
	WorldRuntimeDefinitionStatus    = "status"
	WorldRuntimeDefinitionRule      = "rule"

	WorldRuntimeFactActorCreated      = "actor.created"
	WorldRuntimeFactActivityPerformed = "actor.activity.performed"
	WorldRuntimeFactRoleTransitioned  = "actor.role.transitioned"
	WorldRuntimeFactStatusExpired     = "actor.status.expired"
	WorldRuntimeFactRuleConsequent    = "rule.consequence.applied"

	WorldRuntimeEffectAttributeSet  = "attribute.set"
	WorldRuntimeEffectAttributeAdd  = "attribute.add"
	WorldRuntimeEffectExperienceAdd = "experience.add"
	WorldRuntimeEffectRoleGrant     = "role.grant"
	WorldRuntimeEffectRoleRevoke    = "role.revoke"
	WorldRuntimeEffectStatusGrant   = "status.grant"
	WorldRuntimeEffectStatusRevoke  = "status.revoke"
	WorldRuntimeEffectStatusExpire  = "status.expire"

	worldRuntimeID                     = "sub2api-open-world-runtime"
	worldRuntimeVersion                = "1.0.0"
	worldRuntimeCatalogVersion         = "1.0.0"
	worldRuntimeExecutorVersion        = "1.0.0"
	worldRuntimeMaximumEffects         = 64
	worldRuntimeMaximumRuleCases       = 32
	worldRuntimeMaximumDefinitions     = 4096
	worldRuntimeMaximumDefinitionBytes = 256 * 1024
)

var (
	ErrWorldRuntimeStateNotFound = infraerrors.NotFound(
		"WORLD_RUNTIME_STATE_NOT_FOUND", "world runtime state not found",
	)
	ErrWorldActorNotFound = infraerrors.NotFound(
		"WORLD_ACTOR_NOT_FOUND", "world actor not found",
	)
	ErrWorldRuntimeDefinitionNotFound = infraerrors.NotFound(
		"WORLD_RUNTIME_DEFINITION_NOT_FOUND", "world runtime definition not found",
	)
	errWorldRuntimeInvalidDefinition = errors.New("invalid world runtime definition")
)

type WorldRuntimeProfile struct {
	RuntimeID                    string          `json:"runtime_id"`
	RuntimeVersion               string          `json:"runtime_version"`
	CatalogVersion               string          `json:"catalog_version"`
	CatalogHash                  string          `json:"catalog_hash"`
	BaselineTick                 int64           `json:"baseline_tick"`
	MaximumPlayerActorsPerMember int             `json:"maximum_player_actors_per_member"`
	ActorCount                   int64           `json:"actor_count"`
	FactCount                    int64           `json:"fact_count"`
	EffectCount                  int64           `json:"effect_count"`
	CaseCount                    int64           `json:"case_count"`
	Revision                     int64           `json:"revision"`
	Metadata                     json.RawMessage `json:"metadata"`
}

type WorldRuntimeDefinition struct {
	Kind       string          `json:"kind"`
	Code       string          `json:"code"`
	Version    string          `json:"version"`
	Hash       string          `json:"hash"`
	Visibility string          `json:"visibility"`
	Payload    json.RawMessage `json:"payload"`
}

type WorldActor struct {
	Code             string          `json:"code"`
	OwnerUserID      *int64          `json:"owner_user_id,omitempty"`
	ActorTypeCode    string          `json:"actor_type_code"`
	Name             string          `json:"name"`
	Status           string          `json:"status"`
	ArchetypeCode    *string         `json:"archetype_code,omitempty"`
	ArchetypeVersion *string         `json:"archetype_version,omitempty"`
	CreatedTick      int64           `json:"created_tick"`
	UpdatedTick      int64           `json:"updated_tick"`
	Version          int64           `json:"version"`
	Metadata         json.RawMessage `json:"metadata"`
}

type WorldActorAttribute struct {
	ActorCode       string          `json:"actor_code"`
	AttributeCode   string          `json:"attribute_code"`
	ValueUnits      int64           `json:"value_units"`
	ExperienceUnits int64           `json:"experience_units"`
	LastChangedTick int64           `json:"last_changed_tick"`
	Version         int64           `json:"version"`
	Metadata        json.RawMessage `json:"metadata"`
}

type WorldActorRole struct {
	ActorCode    string          `json:"actor_code"`
	RoleCode     string          `json:"role_code"`
	CategoryCode string          `json:"category_code"`
	Status       string          `json:"status"`
	GrantedTick  int64           `json:"granted_tick"`
	RevokedTick  *int64          `json:"revoked_tick,omitempty"`
	Version      int64           `json:"version"`
	Metadata     json.RawMessage `json:"metadata"`
}

type WorldActorStatus struct {
	ActorCode      string          `json:"actor_code"`
	InstanceCode   string          `json:"instance_code"`
	StatusCode     string          `json:"status_code"`
	Lifecycle      string          `json:"lifecycle_status"`
	IntensityUnits int64           `json:"intensity_units"`
	Stacks         int             `json:"stacks"`
	GrantedTick    int64           `json:"granted_tick"`
	ExpiresTick    *int64          `json:"expires_tick,omitempty"`
	EndedTick      *int64          `json:"ended_tick,omitempty"`
	SourceFactTick int64           `json:"source_fact_tick"`
	SourceFactSeq  int64           `json:"source_fact_sequence"`
	Version        int64           `json:"version"`
	Metadata       json.RawMessage `json:"metadata"`
}

type WorldRuntimeFactRef struct {
	Tick     int64 `json:"tick"`
	Sequence int64 `json:"sequence"`
}

type WorldRuntimeFact struct {
	Tick                  int64                `json:"tick"`
	Sequence              int64                `json:"sequence"`
	SourceCommandSequence *int64               `json:"source_command_sequence,omitempty"`
	Parent                *WorldRuntimeFactRef `json:"parent,omitempty"`
	ActorCode             *string              `json:"actor_code,omitempty"`
	FactType              string               `json:"fact_type"`
	DefinitionKind        *string              `json:"definition_kind,omitempty"`
	DefinitionCode        *string              `json:"definition_code,omitempty"`
	DefinitionVersion     *string              `json:"definition_version,omitempty"`
	DefinitionHash        *string              `json:"definition_hash,omitempty"`
	Payload               json.RawMessage      `json:"payload"`
}

type WorldEffectOperation struct {
	Tick            int64               `json:"tick"`
	Sequence        int64               `json:"sequence"`
	SourceFact      WorldRuntimeFactRef `json:"source_fact"`
	OperationIndex  int                 `json:"operation_index"`
	EffectType      string              `json:"effect_type"`
	ExecutorVersion string              `json:"executor_version"`
	TargetActorCode *string             `json:"target_actor_code,omitempty"`
	TargetKey       *string             `json:"target_key,omitempty"`
	BeforeUnits     *int64              `json:"before_units,omitempty"`
	DeltaUnits      *int64              `json:"delta_units,omitempty"`
	AfterUnits      *int64              `json:"after_units,omitempty"`
	Payload         json.RawMessage     `json:"payload"`
}

type WorldRuleCase struct {
	Code             string               `json:"code"`
	Tick             int64                `json:"tick"`
	Sequence         int64                `json:"sequence"`
	SourceFact       WorldRuntimeFactRef  `json:"source_fact"`
	ConsequenceFact  *WorldRuntimeFactRef `json:"consequence_fact,omitempty"`
	SubjectActorCode string               `json:"subject_actor_code"`
	RuleCode         string               `json:"rule_code"`
	RuleVersion      string               `json:"rule_version"`
	RuleHash         string               `json:"rule_hash"`
	CategoryCode     string               `json:"category_code"`
	ScopeKind        string               `json:"scope_kind"`
	ScopeCode        string               `json:"scope_code"`
	Status           string               `json:"status"`
	SeverityUnits    int64                `json:"severity_units"`
	DecisionCode     *string              `json:"decision_code,omitempty"`
	CreatedTick      int64                `json:"created_tick"`
	DecidedTick      *int64               `json:"decided_tick,omitempty"`
	ClosedTick       *int64               `json:"closed_tick,omitempty"`
	Payload          json.RawMessage      `json:"payload"`
}

type WorldRuntimeState struct {
	Profile     WorldRuntimeProfile      `json:"profile"`
	Definitions []WorldRuntimeDefinition `json:"definitions"`
	Actors      []WorldActor             `json:"actors"`
	Attributes  []WorldActorAttribute    `json:"attributes"`
	Roles       []WorldActorRole         `json:"roles"`
	Statuses    []WorldActorStatus       `json:"statuses"`
	Facts       []WorldRuntimeFact       `json:"facts"`
	Effects     []WorldEffectOperation   `json:"effects"`
	RuleCases   []WorldRuleCase          `json:"rule_cases"`
}

type worldRuntimeHashState = WorldRuntimeState

type WorldActorState struct {
	Actor       WorldActor            `json:"actor"`
	Attributes  []WorldActorAttribute `json:"attributes"`
	Roles       []WorldActorRole      `json:"roles"`
	Statuses    []WorldActorStatus    `json:"statuses"`
	RecentFacts []WorldRuntimeFact    `json:"recent_facts"`
}

type worldRuntimeAttributeDefinition struct {
	NameKey        string `json:"name_key"`
	CategoryCode   string `json:"category_code"`
	MinimumUnits   int64  `json:"minimum_units"`
	MaximumUnits   int64  `json:"maximum_units"`
	DefaultUnits   int64  `json:"default_units"`
	OverflowPolicy string `json:"overflow_policy"`
}

type worldRuntimeArchetypeDefinition struct {
	NameKey           string           `json:"name_key"`
	DescriptionKey    string           `json:"description_key"`
	ActorTypeCode     string           `json:"actor_type_code"`
	InitialAttributes map[string]int64 `json:"initial_attributes"`
	InitialRoles      []string         `json:"initial_roles"`
}

type worldRuntimeEffectSpec struct {
	Type           string `json:"type"`
	Key            string `json:"key"`
	ValueUnits     int64  `json:"value_units,omitempty"`
	IntensityUnits int64  `json:"intensity_units,omitempty"`
	Stacks         int    `json:"stacks,omitempty"`
	DurationTicks  int64  `json:"duration_ticks,omitempty"`
}

type worldRuntimeActivityDefinition struct {
	NameKey        string                   `json:"name_key"`
	DescriptionKey string                   `json:"description_key"`
	Requirements   WorldRequirementNode     `json:"requirements"`
	Effects        []worldRuntimeEffectSpec `json:"effects"`
	TriggerTags    []string                 `json:"trigger_tags"`
	MaximumPerTick int                      `json:"maximum_per_tick"`
}

type worldRuntimeRoleDefinition struct {
	NameKey         string                   `json:"name_key"`
	DescriptionKey  string                   `json:"description_key"`
	CategoryCode    string                   `json:"category_code"`
	Requirements    WorldRequirementNode     `json:"requirements"`
	CooldownTicks   int64                    `json:"cooldown_ticks"`
	OnGrantEffects  []worldRuntimeEffectSpec `json:"on_grant_effects"`
	OnRevokeEffects []worldRuntimeEffectSpec `json:"on_revoke_effects"`
}

type worldRuntimeStatusDefinition struct {
	NameKey        string `json:"name_key"`
	DescriptionKey string `json:"description_key"`
	MaximumStacks  int    `json:"maximum_stacks"`
}

type worldRuntimeRuleTier struct {
	MinimumOccurrences int                      `json:"minimum_occurrences"`
	SeverityUnits      int64                    `json:"severity_units"`
	Effects            []worldRuntimeEffectSpec `json:"effects"`
}

type worldRuntimeRuleDefinition struct {
	NameKey               string                 `json:"name_key"`
	DescriptionKey        string                 `json:"description_key"`
	CategoryCode          string                 `json:"category_code"`
	Triggers              []string               `json:"triggers"`
	ScopeKind             string                 `json:"scope_kind"`
	ScopeCode             string                 `json:"scope_code"`
	Requirements          WorldRequirementNode   `json:"requirements"`
	OccurrenceWindowTicks int64                  `json:"occurrence_window_ticks"`
	Tiers                 []worldRuntimeRuleTier `json:"tiers"`
}

type worldRuntimeDefinitionSeed struct {
	kind       string
	code       string
	visibility string
	payload    any
}

func isWorldRuntimeCommand(commandType string) bool {
	switch commandType {
	case CityCommandTypeActorCreate, CityCommandTypeActorActivityPerform,
		CityCommandTypeActorRoleTransition:
		return true
	default:
		return false
	}
}

type worldActorCreatePayload struct {
	ArchetypeCode string `json:"archetype_code"`
	Name          string `json:"name"`
}

type worldActorActivityPayload struct {
	ActorCode    string `json:"actor_code"`
	ActivityCode string `json:"activity_code"`
}

type worldActorRoleTransitionPayload struct {
	ActorCode string `json:"actor_code"`
	RoleCode  string `json:"role_code"`
}

func normalizeWorldRuntimeCommand(commandType string, rawPayload json.RawMessage) (any, bool, error) {
	normalizeCode := func(value *string) error {
		*value = strings.ToLower(strings.TrimSpace(*value))
		if !worldRuntimeCodeValid(*value, 128) {
			return ErrCityInvalidInput.WithMetadata(map[string]string{"field": "code"})
		}
		return nil
	}
	switch commandType {
	case CityCommandTypeActorCreate:
		var value worldActorCreatePayload
		if err := decodeStrictCityObject(rawPayload, &value); err != nil {
			return nil, true, ErrCityInvalidInput.WithCause(err)
		}
		if err := normalizeCode(&value.ArchetypeCode); err != nil {
			return nil, true, err
		}
		value.Name = strings.TrimSpace(value.Name)
		if utf8.RuneCountInString(value.Name) < 1 || utf8.RuneCountInString(value.Name) > 96 {
			return nil, true, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "name"})
		}
		return value, true, nil
	case CityCommandTypeActorActivityPerform:
		var value worldActorActivityPayload
		if err := decodeStrictCityObject(rawPayload, &value); err != nil {
			return nil, true, ErrCityInvalidInput.WithCause(err)
		}
		if err := normalizeCode(&value.ActorCode); err != nil {
			return nil, true, err
		}
		if err := normalizeCode(&value.ActivityCode); err != nil {
			return nil, true, err
		}
		return value, true, nil
	case CityCommandTypeActorRoleTransition:
		var value worldActorRoleTransitionPayload
		if err := decodeStrictCityObject(rawPayload, &value); err != nil {
			return nil, true, ErrCityInvalidInput.WithCause(err)
		}
		if err := normalizeCode(&value.ActorCode); err != nil {
			return nil, true, err
		}
		if err := normalizeCode(&value.RoleCode); err != nil {
			return nil, true, err
		}
		return value, true, nil
	default:
		return nil, false, nil
	}
}

func worldRuntimeCodeValid(value string, maximum int) bool {
	if len(value) < 2 || len(value) > maximum || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for index := 1; index < len(value); index++ {
		char := value[index]
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') ||
			char == '_' || char == '.' || char == '-' {
			continue
		}
		return false
	}
	return true
}

func builtInWorldRuntimeDefinitions() ([]WorldRuntimeDefinition, string, error) {
	always := WorldRequirementNode{Operator: WorldRequirementAll, Items: []WorldRequirementNode{}}
	seeds := []worldRuntimeDefinitionSeed{
		{WorldRuntimeDefinitionActorType, "character", "public", map[string]any{
			"name_key": "worldRuntime.actorTypes.character", "controllable": true,
		}},
		{WorldRuntimeDefinitionAttribute, "vitality", "public", worldRuntimeAttributeDefinition{
			NameKey: "worldRuntime.attributes.vitality", CategoryCode: "core",
			MinimumUnits: 0, MaximumUnits: 100000, DefaultUnits: 45000, OverflowPolicy: "clamp",
		}},
		{WorldRuntimeDefinitionAttribute, "reasoning", "public", worldRuntimeAttributeDefinition{
			NameKey: "worldRuntime.attributes.reasoning", CategoryCode: "core",
			MinimumUnits: 0, MaximumUnits: 100000, DefaultUnits: 45000, OverflowPolicy: "clamp",
		}},
		{WorldRuntimeDefinitionAttribute, "coordination", "public", worldRuntimeAttributeDefinition{
			NameKey: "worldRuntime.attributes.coordination", CategoryCode: "core",
			MinimumUnits: 0, MaximumUnits: 100000, DefaultUnits: 45000, OverflowPolicy: "clamp",
		}},
		{WorldRuntimeDefinitionAttribute, "communication", "public", worldRuntimeAttributeDefinition{
			NameKey: "worldRuntime.attributes.communication", CategoryCode: "core",
			MinimumUnits: 0, MaximumUnits: 100000, DefaultUnits: 45000, OverflowPolicy: "clamp",
		}},
		{WorldRuntimeDefinitionAttribute, "discipline", "public", worldRuntimeAttributeDefinition{
			NameKey: "worldRuntime.attributes.discipline", CategoryCode: "core",
			MinimumUnits: 0, MaximumUnits: 100000, DefaultUnits: 45000, OverflowPolicy: "clamp",
		}},
		{WorldRuntimeDefinitionStatus, "civic_warning", "public", worldRuntimeStatusDefinition{
			NameKey: "worldRuntime.statuses.civicWarning", DescriptionKey: "worldRuntime.statuses.civicWarningDescription", MaximumStacks: 3,
		}},
		{WorldRuntimeDefinitionStatus, "community_service_order", "public", worldRuntimeStatusDefinition{
			NameKey: "worldRuntime.statuses.communityServiceOrder", DescriptionKey: "worldRuntime.statuses.communityServiceOrderDescription", MaximumStacks: 8,
		}},
		{WorldRuntimeDefinitionRole, "identity.resident", "public", worldRuntimeRoleDefinition{
			NameKey: "worldRuntime.roles.resident", DescriptionKey: "worldRuntime.roles.residentDescription",
			CategoryCode: "identity", Requirements: always,
		}},
		{WorldRuntimeDefinitionRole, "profession.apprentice", "public", worldRuntimeRoleDefinition{
			NameKey: "worldRuntime.roles.apprentice", DescriptionKey: "worldRuntime.roles.apprenticeDescription",
			CategoryCode: "profession", Requirements: WorldRequirementNode{Operator: WorldRequirementAttributeGTE, AttributeCode: "discipline", ValueUnits: 35000},
			CooldownTicks: 1,
		}},
		{WorldRuntimeDefinitionRole, "profession.technician", "public", worldRuntimeRoleDefinition{
			NameKey: "worldRuntime.roles.technician", DescriptionKey: "worldRuntime.roles.technicianDescription",
			CategoryCode: "profession", CooldownTicks: 2,
			Requirements: WorldRequirementNode{Operator: WorldRequirementAll, Items: []WorldRequirementNode{
				{Operator: WorldRequirementAttributeGTE, AttributeCode: "reasoning", ValueUnits: 60000},
				{Operator: WorldRequirementAttributeGTE, AttributeCode: "coordination", ValueUnits: 50000},
				{Operator: WorldRequirementRoleActive, RoleCode: "profession.apprentice"},
			}},
		}},
		{WorldRuntimeDefinitionArchetype, "resident_generalist", "public", worldRuntimeArchetypeDefinition{
			NameKey: "worldRuntime.archetypes.residentGeneralist", DescriptionKey: "worldRuntime.archetypes.residentGeneralistDescription",
			ActorTypeCode: "character", InitialAttributes: map[string]int64{
				"vitality": 50000, "reasoning": 50000, "coordination": 50000,
				"communication": 50000, "discipline": 50000,
			}, InitialRoles: []string{"identity.resident", "profession.apprentice"},
		}},
		{WorldRuntimeDefinitionArchetype, "urban_apprentice", "public", worldRuntimeArchetypeDefinition{
			NameKey: "worldRuntime.archetypes.urbanApprentice", DescriptionKey: "worldRuntime.archetypes.urbanApprenticeDescription",
			ActorTypeCode: "character", InitialAttributes: map[string]int64{
				"vitality": 44000, "reasoning": 56000, "coordination": 50000,
				"communication": 47000, "discipline": 53000,
			}, InitialRoles: []string{"identity.resident", "profession.apprentice"},
		}},
		{WorldRuntimeDefinitionArchetype, "field_survivor", "public", worldRuntimeArchetypeDefinition{
			NameKey: "worldRuntime.archetypes.fieldSurvivor", DescriptionKey: "worldRuntime.archetypes.fieldSurvivorDescription",
			ActorTypeCode: "character", InitialAttributes: map[string]int64{
				"vitality": 59000, "reasoning": 44000, "coordination": 56000,
				"communication": 41000, "discipline": 50000,
			}, InitialRoles: []string{"identity.resident", "profession.apprentice"},
		}},
		{WorldRuntimeDefinitionActivity, "technical_study", "public", worldRuntimeActivityDefinition{
			NameKey: "worldRuntime.activities.technicalStudy", DescriptionKey: "worldRuntime.activities.technicalStudyDescription",
			Requirements: always, MaximumPerTick: 2,
			Effects: []worldRuntimeEffectSpec{
				{Type: WorldRuntimeEffectAttributeAdd, Key: "reasoning", ValueUnits: 2500},
				{Type: WorldRuntimeEffectExperienceAdd, Key: "reasoning", ValueUnits: 1000},
				{Type: WorldRuntimeEffectAttributeAdd, Key: "discipline", ValueUnits: 500},
			}, TriggerTags: []string{"activity.study"},
		}},
		{WorldRuntimeDefinitionActivity, "physical_training", "public", worldRuntimeActivityDefinition{
			NameKey: "worldRuntime.activities.physicalTraining", DescriptionKey: "worldRuntime.activities.physicalTrainingDescription",
			Requirements: always, MaximumPerTick: 2,
			Effects: []worldRuntimeEffectSpec{
				{Type: WorldRuntimeEffectAttributeAdd, Key: "vitality", ValueUnits: 2000},
				{Type: WorldRuntimeEffectAttributeAdd, Key: "coordination", ValueUnits: 1000},
				{Type: WorldRuntimeEffectExperienceAdd, Key: "vitality", ValueUnits: 800},
			}, TriggerTags: []string{"activity.training"},
		}},
		{WorldRuntimeDefinitionActivity, "community_service", "public", worldRuntimeActivityDefinition{
			NameKey: "worldRuntime.activities.communityService", DescriptionKey: "worldRuntime.activities.communityServiceDescription",
			Requirements: always, MaximumPerTick: 1,
			Effects: []worldRuntimeEffectSpec{
				{Type: WorldRuntimeEffectAttributeAdd, Key: "discipline", ValueUnits: 2000},
				{Type: WorldRuntimeEffectAttributeAdd, Key: "communication", ValueUnits: 1000},
			}, TriggerTags: []string{"activity.civic"},
		}},
		{WorldRuntimeDefinitionActivity, "disruptive_noise", "public", worldRuntimeActivityDefinition{
			NameKey: "worldRuntime.activities.disruptiveNoise", DescriptionKey: "worldRuntime.activities.disruptiveNoiseDescription",
			Requirements: always, MaximumPerTick: 3,
			Effects: []worldRuntimeEffectSpec{
				{Type: WorldRuntimeEffectAttributeAdd, Key: "discipline", ValueUnits: -1000},
			}, TriggerTags: []string{"conduct.noise"},
		}},
		{WorldRuntimeDefinitionRule, "law.public_order.noise", "public", worldRuntimeRuleDefinition{
			NameKey: "worldRuntime.rules.publicOrderNoise", DescriptionKey: "worldRuntime.rules.publicOrderNoiseDescription",
			CategoryCode: "law", Triggers: []string{"conduct.noise"}, ScopeKind: "world",
			ScopeCode: "world", Requirements: always, OccurrenceWindowTicks: 20,
			Tiers: []worldRuntimeRuleTier{
				{MinimumOccurrences: 1, SeverityUnits: 20000, Effects: []worldRuntimeEffectSpec{
					{Type: WorldRuntimeEffectStatusGrant, Key: "civic_warning", IntensityUnits: 20000, Stacks: 1, DurationTicks: 3},
				}},
				{MinimumOccurrences: 3, SeverityUnits: 50000, Effects: []worldRuntimeEffectSpec{
					{Type: WorldRuntimeEffectStatusGrant, Key: "community_service_order", IntensityUnits: 50000, Stacks: 1, DurationTicks: 8},
				}},
			},
		}},
	}
	definitions := make([]WorldRuntimeDefinition, 0, len(seeds))
	for _, seed := range seeds {
		raw, err := json.Marshal(seed.payload)
		if err != nil {
			return nil, "", fmt.Errorf("marshal world runtime definition %s/%s: %w", seed.kind, seed.code, err)
		}
		sum := sha256.Sum256(raw)
		definition := WorldRuntimeDefinition{
			Kind: seed.kind, Code: seed.code, Version: worldRuntimeCatalogVersion,
			Hash: hex.EncodeToString(sum[:]), Visibility: seed.visibility, Payload: raw,
		}
		if err = validateWorldRuntimeDefinition(definition); err != nil {
			return nil, "", fmt.Errorf("validate world runtime definition %s/%s: %w", seed.kind, seed.code, err)
		}
		definitions = append(definitions, definition)
	}
	sort.Slice(definitions, func(i, j int) bool {
		if definitions[i].Kind != definitions[j].Kind {
			return definitions[i].Kind < definitions[j].Kind
		}
		return definitions[i].Code < definitions[j].Code
	})
	if err := validateWorldRuntimeCatalog(definitions); err != nil {
		return nil, "", fmt.Errorf("validate world runtime catalog references: %w", err)
	}
	descriptors := make([]struct {
		Kind    string `json:"kind"`
		Code    string `json:"code"`
		Version string `json:"version"`
		Hash    string `json:"hash"`
	}, 0, len(definitions))
	for _, definition := range definitions {
		descriptors = append(descriptors, struct {
			Kind    string `json:"kind"`
			Code    string `json:"code"`
			Version string `json:"version"`
			Hash    string `json:"hash"`
		}{definition.Kind, definition.Code, definition.Version, definition.Hash})
	}
	raw, err := json.Marshal(descriptors)
	if err != nil {
		return nil, "", fmt.Errorf("marshal world runtime catalog: %w", err)
	}
	sum := sha256.Sum256(raw)
	return definitions, hex.EncodeToString(sum[:]), nil
}

func validateWorldRuntimeDefinition(definition WorldRuntimeDefinition) error {
	if !worldRuntimeCodeValid(definition.Code, 128) || definition.Version != worldRuntimeCatalogVersion ||
		len(definition.Hash) != 64 || len(definition.Payload) == 0 ||
		len(definition.Payload) > worldRuntimeMaximumDefinitionBytes ||
		(definition.Visibility != "public" && definition.Visibility != "discoverable" && definition.Visibility != "hidden") {
		return errWorldRuntimeInvalidDefinition
	}
	var destination any
	switch definition.Kind {
	case WorldRuntimeDefinitionActorType:
		destination = &map[string]any{}
	case WorldRuntimeDefinitionArchetype:
		destination = &worldRuntimeArchetypeDefinition{}
	case WorldRuntimeDefinitionAttribute:
		destination = &worldRuntimeAttributeDefinition{}
	case WorldRuntimeDefinitionActivity:
		destination = &worldRuntimeActivityDefinition{}
	case WorldRuntimeDefinitionRole:
		destination = &worldRuntimeRoleDefinition{}
	case WorldRuntimeDefinitionStatus:
		destination = &worldRuntimeStatusDefinition{}
	case WorldRuntimeDefinitionRule:
		destination = &worldRuntimeRuleDefinition{}
	default:
		return errWorldRuntimeInvalidDefinition
	}
	if err := decodeStrictCityObject(definition.Payload, destination); err != nil {
		return fmt.Errorf("%w: %v", errWorldRuntimeInvalidDefinition, err)
	}
	sum := sha256.Sum256(definition.Payload)
	if hex.EncodeToString(sum[:]) != definition.Hash {
		return fmt.Errorf("%w: content hash mismatch", errWorldRuntimeInvalidDefinition)
	}
	switch value := destination.(type) {
	case *worldRuntimeAttributeDefinition:
		if value.MinimumUnits > value.DefaultUnits || value.DefaultUnits > value.MaximumUnits ||
			value.OverflowPolicy != "clamp" || !worldRuntimeCodeValid(value.CategoryCode, 128) {
			return errWorldRuntimeInvalidDefinition
		}
	case *worldRuntimeArchetypeDefinition:
		if !worldRuntimeCodeValid(value.ActorTypeCode, 128) || len(value.InitialAttributes) == 0 ||
			len(value.InitialAttributes) > worldRuntimeMaximumEffects || len(value.InitialRoles) == 0 {
			return errWorldRuntimeInvalidDefinition
		}
		for code := range value.InitialAttributes {
			if !worldRuntimeCodeValid(code, 128) {
				return errWorldRuntimeInvalidDefinition
			}
		}
		if !worldRuntimeCodesUnique(value.InitialRoles, 128) {
			return errWorldRuntimeInvalidDefinition
		}
	case *worldRuntimeActivityDefinition:
		if value.MaximumPerTick < 1 || value.MaximumPerTick > 64 ||
			!worldRuntimeEffectSpecsValid(value.Effects, true) ||
			!worldRuntimeCodesUnique(value.TriggerTags, 128) {
			return errWorldRuntimeInvalidDefinition
		}
		if err := validateWorldRequirement(value.Requirements); err != nil {
			return err
		}
	case *worldRuntimeRoleDefinition:
		if !worldRuntimeCodeValid(value.CategoryCode, 128) || value.CooldownTicks < 0 || value.CooldownTicks > 1000000 ||
			!worldRuntimeEffectSpecsValid(value.OnGrantEffects, true) ||
			!worldRuntimeEffectSpecsValid(value.OnRevokeEffects, true) {
			return errWorldRuntimeInvalidDefinition
		}
		if err := validateWorldRequirement(value.Requirements); err != nil {
			return err
		}
	case *worldRuntimeStatusDefinition:
		if value.MaximumStacks < 1 || value.MaximumStacks > 1000000 {
			return errWorldRuntimeInvalidDefinition
		}
	case *worldRuntimeRuleDefinition:
		if !worldRuntimeCodeValid(value.CategoryCode, 128) || !worldRuntimeCodeValid(value.ScopeKind, 64) ||
			!worldRuntimeCodeValid(value.ScopeCode, 160) || !worldRuntimeCodesUnique(value.Triggers, 128) ||
			len(value.Tiers) == 0 || len(value.Tiers) > 16 || value.OccurrenceWindowTicks < 1 {
			return errWorldRuntimeInvalidDefinition
		}
		if err := validateWorldRequirement(value.Requirements); err != nil {
			return err
		}
		previous := 0
		for _, tier := range value.Tiers {
			if tier.MinimumOccurrences <= previous || tier.SeverityUnits < 0 ||
				!worldRuntimeEffectSpecsValid(tier.Effects, true) {
				return errWorldRuntimeInvalidDefinition
			}
			previous = tier.MinimumOccurrences
		}
	}
	return nil
}

func worldRuntimeCodesUnique(values []string, maximum int) bool {
	if len(values) == 0 || len(values) > worldRuntimeMaximumEffects {
		return false
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !worldRuntimeCodeValid(value, maximum) {
			return false
		}
		if _, exists := seen[value]; exists {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func worldRuntimeEffectSpecsValid(specs []worldRuntimeEffectSpec, catalog bool) bool {
	if len(specs) > worldRuntimeMaximumEffects {
		return false
	}
	for _, spec := range specs {
		if !worldRuntimeCodeValid(spec.Key, 160) {
			return false
		}
		switch spec.Type {
		case WorldRuntimeEffectAttributeSet, WorldRuntimeEffectAttributeAdd, WorldRuntimeEffectExperienceAdd:
			if spec.IntensityUnits != 0 || spec.Stacks != 0 || spec.DurationTicks != 0 {
				return false
			}
		case WorldRuntimeEffectRoleGrant, WorldRuntimeEffectRoleRevoke:
			if spec.ValueUnits != 0 || spec.IntensityUnits != 0 || spec.Stacks != 0 || spec.DurationTicks != 0 {
				return false
			}
		case WorldRuntimeEffectStatusGrant:
			if spec.ValueUnits != 0 || spec.IntensityUnits < 0 || spec.Stacks < 0 ||
				spec.Stacks > 1000000 || spec.DurationTicks < 0 || spec.DurationTicks > 1000000 {
				return false
			}
		case WorldRuntimeEffectStatusRevoke:
			if spec.ValueUnits != 0 || spec.IntensityUnits != 0 || spec.Stacks != 0 || spec.DurationTicks != 0 {
				return false
			}
		case WorldRuntimeEffectStatusExpire:
			if catalog || spec.ValueUnits != 0 || spec.IntensityUnits != 0 || spec.Stacks != 0 || spec.DurationTicks != 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func validateWorldRuntimeCatalog(definitions []WorldRuntimeDefinition) error {
	if len(definitions) == 0 || len(definitions) > worldRuntimeMaximumDefinitions {
		return fmt.Errorf("%w: catalog definition count is out of range", errWorldRuntimeInvalidDefinition)
	}
	byIdentity := make(map[string]WorldRuntimeDefinition, len(definitions))
	identity := func(kind, code string) string { return kind + "/" + code }
	for _, definition := range definitions {
		if err := validateWorldRuntimeDefinition(definition); err != nil {
			return err
		}
		key := identity(definition.Kind, definition.Code)
		if _, duplicate := byIdentity[key]; duplicate {
			return fmt.Errorf("%w: duplicate definition %s", errWorldRuntimeInvalidDefinition, key)
		}
		byIdentity[key] = definition
	}
	requireDefinition := func(kind, code string) error {
		if _, exists := byIdentity[identity(kind, code)]; !exists {
			return fmt.Errorf("%w: missing referenced definition %s/%s", errWorldRuntimeInvalidDefinition, kind, code)
		}
		return nil
	}
	validateRequirementReferences := func(root WorldRequirementNode) error {
		var visit func(WorldRequirementNode) error
		visit = func(node WorldRequirementNode) error {
			switch node.Operator {
			case WorldRequirementAll, WorldRequirementAny:
				for _, item := range node.Items {
					if err := visit(item); err != nil {
						return err
					}
				}
			case WorldRequirementNot:
				if node.Item != nil {
					return visit(*node.Item)
				}
			case WorldRequirementAttributeGTE, WorldRequirementAttributeLTE, WorldRequirementExperienceGTE:
				return requireDefinition(WorldRuntimeDefinitionAttribute, node.AttributeCode)
			case WorldRequirementRoleActive, WorldRequirementRoleInactive:
				return requireDefinition(WorldRuntimeDefinitionRole, node.RoleCode)
			case WorldRequirementStatusPresent, WorldRequirementStatusAbsent:
				return requireDefinition(WorldRuntimeDefinitionStatus, node.StatusCode)
			}
			return nil
		}
		return visit(root)
	}
	validateEffectReferences := func(specs []worldRuntimeEffectSpec) error {
		for _, spec := range specs {
			kind := ""
			switch spec.Type {
			case WorldRuntimeEffectAttributeSet, WorldRuntimeEffectAttributeAdd, WorldRuntimeEffectExperienceAdd:
				kind = WorldRuntimeDefinitionAttribute
			case WorldRuntimeEffectRoleGrant, WorldRuntimeEffectRoleRevoke:
				kind = WorldRuntimeDefinitionRole
			case WorldRuntimeEffectStatusGrant, WorldRuntimeEffectStatusRevoke, WorldRuntimeEffectStatusExpire:
				kind = WorldRuntimeDefinitionStatus
			}
			if kind != "" {
				if err := requireDefinition(kind, spec.Key); err != nil {
					return err
				}
			}
		}
		return nil
	}
	roleDependencies := make(map[string][]string)
	for _, definition := range definitions {
		switch definition.Kind {
		case WorldRuntimeDefinitionArchetype:
			value, err := decodeWorldRuntimeDefinition[worldRuntimeArchetypeDefinition](&definition)
			if err != nil {
				return err
			}
			if err = requireDefinition(WorldRuntimeDefinitionActorType, value.ActorTypeCode); err != nil {
				return err
			}
			for code, initial := range value.InitialAttributes {
				attributeDefinition, exists := byIdentity[identity(WorldRuntimeDefinitionAttribute, code)]
				if !exists {
					return fmt.Errorf("%w: missing referenced definition %s/%s", errWorldRuntimeInvalidDefinition, WorldRuntimeDefinitionAttribute, code)
				}
				attribute, decodeErr := decodeWorldRuntimeDefinition[worldRuntimeAttributeDefinition](&attributeDefinition)
				if decodeErr != nil {
					return decodeErr
				}
				if initial < attribute.MinimumUnits || initial > attribute.MaximumUnits {
					return fmt.Errorf("%w: archetype attribute %s is outside definition bounds", errWorldRuntimeInvalidDefinition, code)
				}
			}
			for _, code := range value.InitialRoles {
				if err = requireDefinition(WorldRuntimeDefinitionRole, code); err != nil {
					return err
				}
			}
		case WorldRuntimeDefinitionActivity:
			value, err := decodeWorldRuntimeDefinition[worldRuntimeActivityDefinition](&definition)
			if err != nil {
				return err
			}
			if err = validateRequirementReferences(value.Requirements); err != nil {
				return err
			}
			if err = validateEffectReferences(value.Effects); err != nil {
				return err
			}
		case WorldRuntimeDefinitionRole:
			value, err := decodeWorldRuntimeDefinition[worldRuntimeRoleDefinition](&definition)
			if err != nil {
				return err
			}
			if err = validateRequirementReferences(value.Requirements); err != nil {
				return err
			}
			if err = validateEffectReferences(value.OnGrantEffects); err != nil {
				return err
			}
			if err = validateEffectReferences(value.OnRevokeEffects); err != nil {
				return err
			}
			var collectRoleReferences func(WorldRequirementNode)
			collectRoleReferences = func(node WorldRequirementNode) {
				switch node.Operator {
				case WorldRequirementRoleActive, WorldRequirementRoleInactive:
					roleDependencies[definition.Code] = append(roleDependencies[definition.Code], node.RoleCode)
				case WorldRequirementAll, WorldRequirementAny:
					for _, item := range node.Items {
						collectRoleReferences(item)
					}
				case WorldRequirementNot:
					if node.Item != nil {
						collectRoleReferences(*node.Item)
					}
				}
			}
			collectRoleReferences(value.Requirements)
		case WorldRuntimeDefinitionRule:
			value, err := decodeWorldRuntimeDefinition[worldRuntimeRuleDefinition](&definition)
			if err != nil {
				return err
			}
			if err = validateRequirementReferences(value.Requirements); err != nil {
				return err
			}
			for _, tier := range value.Tiers {
				if err = validateEffectReferences(tier.Effects); err != nil {
					return err
				}
			}
		}
	}
	visiting := make(map[string]bool)
	visited := make(map[string]bool)
	var visitRole func(string) error
	visitRole = func(code string) error {
		if visiting[code] {
			return fmt.Errorf("%w: cyclic role requirement at %s", errWorldRuntimeInvalidDefinition, code)
		}
		if visited[code] {
			return nil
		}
		visiting[code] = true
		for _, dependency := range roleDependencies[code] {
			if err := visitRole(dependency); err != nil {
				return err
			}
		}
		visiting[code] = false
		visited[code] = true
		return nil
	}
	for code := range roleDependencies {
		if err := visitRole(code); err != nil {
			return err
		}
	}
	return nil
}

func initializeWorldRuntimeFoundation(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if worldID <= 0 {
		return ErrCityInvalidInput
	}
	definitions, catalogHash, err := builtInWorldRuntimeDefinitions()
	if err != nil {
		return err
	}
	var baselineTick int64
	if err = tx.QueryRowContext(ctx, `SELECT current_tick FROM city_worlds WHERE id = $1`, worldID).Scan(&baselineTick); err != nil {
		return fmt.Errorf("load world runtime baseline tick: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `SELECT set_config('sub2api.world_runtime_bootstrap_world_id', $1, TRUE)`,
		fmt.Sprintf("%d", worldID)); err != nil {
		return fmt.Errorf("enable world runtime bootstrap: %w", err)
	}
	for _, definition := range definitions {
		if _, err = tx.ExecContext(ctx, `
INSERT INTO world_runtime_definitions
    (world_id, definition_kind, code, definition_version, content_hash, visibility, payload)
VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)`, worldID, definition.Kind, definition.Code,
			definition.Version, definition.Hash, definition.Visibility, []byte(definition.Payload)); err != nil {
			return fmt.Errorf("insert world runtime definition %s/%s: %w", definition.Kind, definition.Code, err)
		}
	}
	metadata := json.RawMessage(`{"schema_version":1,"requirement_ast_version":"1.0.0","effect_executor_version":"1.0.0"}`)
	if _, err = tx.ExecContext(ctx, `
INSERT INTO world_runtime_profiles
    (world_id, runtime_id, runtime_version, catalog_version, catalog_hash,
     baseline_tick, maximum_player_actors_per_member, actor_count, fact_count,
     effect_count, case_count, revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, 1, 0, 0, 0, 0, 1, $7::jsonb)`, worldID,
		worldRuntimeID, worldRuntimeVersion, worldRuntimeCatalogVersion, catalogHash,
		baselineTick, []byte(metadata)); err != nil {
		return fmt.Errorf("insert world runtime profile: %w", err)
	}
	return nil
}

func loadWorldRuntimeDefinition(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	kind, code string,
) (*WorldRuntimeDefinition, error) {
	item := &WorldRuntimeDefinition{}
	err := queryer.QueryRowContext(ctx, `
SELECT definition_kind, code, definition_version, content_hash, visibility, payload
FROM world_runtime_definitions
WHERE world_id = $1 AND definition_kind = $2 AND code = $3`, worldID, kind, code).Scan(
		&item.Kind, &item.Code, &item.Version, &item.Hash, &item.Visibility, &item.Payload,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrWorldRuntimeDefinitionNotFound.WithMetadata(map[string]string{"kind": kind, "code": code})
	}
	if err != nil {
		return nil, fmt.Errorf("load world runtime definition %s/%s: %w", kind, code, err)
	}
	return item, nil
}

func decodeWorldRuntimeDefinition[T any](definition *WorldRuntimeDefinition) (T, error) {
	var value T
	if definition == nil {
		return value, ErrWorldRuntimeDefinitionNotFound
	}
	if err := decodeStrictCityObject(definition.Payload, &value); err != nil {
		return value, fmt.Errorf("decode world runtime definition %s/%s: %w", definition.Kind, definition.Code, err)
	}
	return value, nil
}
