package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

const (
	cityRealtimeCharacterProgressionSchemaVersion            = 1
	cityRealtimeCharacterProgressionStateSchemaVersion       = 1
	cityRealtimeCharacterProgressionEventSchemaVersion       = 1
	cityRealtimeCharacterRoleAction                          = "character.role"
	cityRealtimeCharacterDefaultArchetypeCode                = "resident.generalist"
	cityRealtimeCharacterProgressionChainNamespace           = "city-realtime-character-progression-genesis-v1"
	cityRealtimeCharacterMaximumExperienceUnits        int64 = 250000
)

// CityRealtimeCharacterExperienceDelta is a server-derived experience change.
// It deliberately accepts no browser-provided amount or attribute selection.
type CityRealtimeCharacterExperienceDelta struct {
	AttributeCode   string `json:"attribute_code"`
	ExperienceUnits int64  `json:"experience_units"`
}

// CityRealtimeCharacterAttribute is an owner-private current attribute state.
// ValueMilli is deterministically derived from the sealed archetype baseline
// and accumulated experience; it is never written directly by a client.
type CityRealtimeCharacterAttribute struct {
	Code              string `json:"code"`
	ValueMilli        int64  `json:"value_milli"`
	ExperienceUnits   int64  `json:"experience_units"`
	Revision          int64  `json:"revision"`
	LastFrameSequence int64  `json:"last_frame_sequence"`
}

// CityRealtimeCharacterRole is a currently active role assignment. A
// character may hold one role per catalog category, allowing future systems
// (profession, civic office, faction, license) to evolve independently.
type CityRealtimeCharacterRole struct {
	Code                 string `json:"code"`
	CategoryCode         string `json:"category_code"`
	GrantedFrameSequence int64  `json:"granted_frame_sequence"`
	Revision             int64  `json:"revision"`
}

type CityRealtimeCharacterAttributeRequirement struct {
	AttributeCode     string `json:"attribute_code"`
	MinimumValueMilli int64  `json:"minimum_value_milli"`
}

// CityRealtimeCharacterRoleRequirements is catalog-owned advisory detail for
// the owner UI. The mutation endpoint recomputes it inside its world lock.
type CityRealtimeCharacterRoleRequirements struct {
	MinimumCivicStandingMilli   int64                                       `json:"minimum_civic_standing_milli,omitempty"`
	MinimumTotalExperienceUnits int64                                       `json:"minimum_total_experience_units,omitempty"`
	Attributes                  []CityRealtimeCharacterAttributeRequirement `json:"attributes,omitempty"`
	RequiredRoleCodes           []string                                    `json:"required_role_codes,omitempty"`
}

type CityRealtimeCharacterRoleAvailability struct {
	Code         string                                `json:"code"`
	CategoryCode string                                `json:"category_code"`
	Available    bool                                  `json:"available"`
	ReasonCode   string                                `json:"reason_code,omitempty"`
	Requirements CityRealtimeCharacterRoleRequirements `json:"requirements"`
}

// CityRealtimeCharacterArchetypeOption is a finite public catalog choice
// presented before a member creates their one character in a world. It has no
// provider, prompt, account, or security material.
type CityRealtimeCharacterArchetypeOption struct {
	Code              string                           `json:"code"`
	InitialRoleCode   string                           `json:"initial_role_code"`
	InitialAttributes []CityRealtimeCharacterAttribute `json:"initial_attributes"`
}

// CityRealtimeCharacterProgression is owner-private character development
// state. Public actor/map projections never include it.
type CityRealtimeCharacterProgression struct {
	SchemaVersion  int                                     `json:"schema_version"`
	ArchetypeCode  string                                  `json:"archetype_code"`
	Revision       int64                                   `json:"revision"`
	Attributes     []CityRealtimeCharacterAttribute        `json:"attributes"`
	Roles          []CityRealtimeCharacterRole             `json:"roles"`
	AvailableRoles []CityRealtimeCharacterRoleAvailability `json:"available_roles"`
}

type CityRealtimeCharacterRoleChangeResult struct {
	CategoryCode string `json:"category_code"`
	FromRoleCode string `json:"from_role_code"`
	ToRoleCode   string `json:"to_role_code"`
}

type CityRealtimeCharacterRoleChangeInput struct {
	UserID         int64
	WorldID        int64
	RoleCode       string
	IdempotencyKey string
}

type cityRealtimeCharacterAttributeDefinition struct {
	Code                    string `json:"code"`
	ExperiencePerValueMilli int64  `json:"experience_per_value_milli"`
	MaximumExperienceUnits  int64  `json:"maximum_experience_units"`
}

type cityRealtimeCharacterArchetypeAttributeDefinition struct {
	AttributeCode     string `json:"attribute_code"`
	InitialValueMilli int64  `json:"initial_value_milli"`
}

type cityRealtimeCharacterArchetypeDefinition struct {
	Code              string                                              `json:"code"`
	InitialRoleCode   string                                              `json:"initial_role_code"`
	InitialAttributes []cityRealtimeCharacterArchetypeAttributeDefinition `json:"initial_attributes"`
}

type cityRealtimeCharacterRoleDefinition struct {
	Code         string                                `json:"code"`
	CategoryCode string                                `json:"category_code"`
	Requirements CityRealtimeCharacterRoleRequirements `json:"requirements"`
}

type cityRealtimeCharacterActivityProgressionDefinition struct {
	RequiredRoleCodes []string                               `json:"required_role_codes,omitempty"`
	ExperienceRewards []CityRealtimeCharacterExperienceDelta `json:"experience_rewards,omitempty"`
}

type cityRealtimeCharacterProgressionDefinition struct {
	SchemaVersion int                                        `json:"schema_version"`
	Attributes    []cityRealtimeCharacterAttributeDefinition `json:"attributes"`
	Archetypes    []cityRealtimeCharacterArchetypeDefinition `json:"archetypes"`
	Roles         []cityRealtimeCharacterRoleDefinition      `json:"roles"`
}

type cityRealtimeCharacterAttributeState struct {
	AttributeCode     string `json:"attribute_code"`
	ExperienceUnits   int64  `json:"experience_units"`
	Revision          int64  `json:"revision"`
	LastFrameSequence int64  `json:"last_frame_sequence"`
	StateHash         string `json:"state_hash"`
}

type cityRealtimeCharacterRoleAssignment struct {
	CategoryCode         string `json:"category_code"`
	RoleCode             string `json:"role_code"`
	GrantedFrameSequence int64  `json:"granted_frame_sequence"`
	Revision             int64  `json:"revision"`
	StateHash            string `json:"state_hash"`
}

type cityRealtimeCharacterProgressionEventRecord struct {
	ActorCode             string
	EventSequence         int64
	FrameSequence         int64
	EventKind             string
	ActivityEventSequence *int64
	CategoryCode          *string
	FromRoleCode          *string
	ToRoleCode            *string
	ExperienceDeltas      []CityRealtimeCharacterExperienceDelta
	PreviousEventHash     string
	EventHash             string
}

func cityRealtimeCharacterProgressionCatalogDefinition() *cityRealtimeCharacterProgressionDefinition {
	return &cityRealtimeCharacterProgressionDefinition{
		SchemaVersion: cityRealtimeCharacterProgressionSchemaVersion,
		Attributes: []cityRealtimeCharacterAttributeDefinition{
			{Code: "communication", ExperiencePerValueMilli: 5, MaximumExperienceUnits: cityRealtimeCharacterMaximumExperienceUnits},
			{Code: "coordination", ExperiencePerValueMilli: 5, MaximumExperienceUnits: cityRealtimeCharacterMaximumExperienceUnits},
			{Code: "discipline", ExperiencePerValueMilli: 5, MaximumExperienceUnits: cityRealtimeCharacterMaximumExperienceUnits},
			{Code: "reasoning", ExperiencePerValueMilli: 5, MaximumExperienceUnits: cityRealtimeCharacterMaximumExperienceUnits},
			{Code: "vitality", ExperiencePerValueMilli: 5, MaximumExperienceUnits: cityRealtimeCharacterMaximumExperienceUnits},
		},
		Archetypes: []cityRealtimeCharacterArchetypeDefinition{
			{
				Code: "resident.generalist", InitialRoleCode: "profession.resident",
				InitialAttributes: []cityRealtimeCharacterArchetypeAttributeDefinition{
					{AttributeCode: "communication", InitialValueMilli: 410},
					{AttributeCode: "coordination", InitialValueMilli: 430},
					{AttributeCode: "discipline", InitialValueMilli: 420},
					{AttributeCode: "reasoning", InitialValueMilli: 440},
					{AttributeCode: "vitality", InitialValueMilli: 460},
				},
			},
			{
				Code: "resident.social", InitialRoleCode: "profession.resident",
				InitialAttributes: []cityRealtimeCharacterArchetypeAttributeDefinition{
					{AttributeCode: "communication", InitialValueMilli: 490},
					{AttributeCode: "coordination", InitialValueMilli: 400},
					{AttributeCode: "discipline", InitialValueMilli: 410},
					{AttributeCode: "reasoning", InitialValueMilli: 430},
					{AttributeCode: "vitality", InitialValueMilli: 430},
				},
			},
			{
				Code: "resident.technical", InitialRoleCode: "profession.resident",
				InitialAttributes: []cityRealtimeCharacterArchetypeAttributeDefinition{
					{AttributeCode: "communication", InitialValueMilli: 380},
					{AttributeCode: "coordination", InitialValueMilli: 480},
					{AttributeCode: "discipline", InitialValueMilli: 430},
					{AttributeCode: "reasoning", InitialValueMilli: 500},
					{AttributeCode: "vitality", InitialValueMilli: 450},
				},
			},
		},
		Roles: []cityRealtimeCharacterRoleDefinition{
			{
				Code: "profession.civic_aide", CategoryCode: "profession",
				Requirements: CityRealtimeCharacterRoleRequirements{
					MinimumCivicStandingMilli: 820, MinimumTotalExperienceUnits: 64,
					Attributes: []CityRealtimeCharacterAttributeRequirement{
						{AttributeCode: "communication", MinimumValueMilli: 450},
						{AttributeCode: "discipline", MinimumValueMilli: 465},
					},
				},
			},
			{
				Code: "profession.community_steward", CategoryCode: "profession",
				Requirements: CityRealtimeCharacterRoleRequirements{
					MinimumCivicStandingMilli: 900, MinimumTotalExperienceUnits: 240,
					Attributes: []CityRealtimeCharacterAttributeRequirement{
						{AttributeCode: "communication", MinimumValueMilli: 520},
						{AttributeCode: "discipline", MinimumValueMilli: 520},
						{AttributeCode: "reasoning", MinimumValueMilli: 500},
					},
				},
			},
			{
				Code: "profession.maintenance_worker", CategoryCode: "profession",
				Requirements: CityRealtimeCharacterRoleRequirements{
					MinimumCivicStandingMilli: 800, MinimumTotalExperienceUnits: 80,
					Attributes: []CityRealtimeCharacterAttributeRequirement{
						{AttributeCode: "coordination", MinimumValueMilli: 470},
						{AttributeCode: "vitality", MinimumValueMilli: 480},
					},
				},
			},
			{Code: "profession.resident", CategoryCode: "profession", Requirements: CityRealtimeCharacterRoleRequirements{}},
		},
	}
}

func cityRealtimeCharacterProgressionActivities(
	definitions []cityRealtimeCharacterActivityDefinition,
) []cityRealtimeCharacterActivityDefinition {
	items := append([]cityRealtimeCharacterActivityDefinition(nil), definitions...)
	for index := range items {
		switch items[index].Code {
		case "rest.short":
			items[index].Progression = &cityRealtimeCharacterActivityProgressionDefinition{
				ExperienceRewards: []CityRealtimeCharacterExperienceDelta{{AttributeCode: "vitality", ExperienceUnits: 4}},
			}
		case "work.civic_shift":
			items[index].Progression = &cityRealtimeCharacterActivityProgressionDefinition{
				ExperienceRewards: []CityRealtimeCharacterExperienceDelta{
					{AttributeCode: "communication", ExperienceUnits: 12},
					{AttributeCode: "discipline", ExperienceUnits: 24},
				},
			}
		case "consume.ration":
			items[index].Progression = &cityRealtimeCharacterActivityProgressionDefinition{
				ExperienceRewards: []CityRealtimeCharacterExperienceDelta{{AttributeCode: "vitality", ExperienceUnits: 2}},
			}
		case "civic.cleanup":
			items[index].Progression = &cityRealtimeCharacterActivityProgressionDefinition{
				ExperienceRewards: []CityRealtimeCharacterExperienceDelta{
					{AttributeCode: "coordination", ExperienceUnits: 26},
					{AttributeCode: "vitality", ExperienceUnits: 12},
				},
			}
		}
	}
	items = append(items,
		cityRealtimeCharacterActivityDefinition{
			Code: "study.public_service", CategoryCode: "training", LocationRequirement: "traversable", PublicVisibility: false,
			MinimumIntervalUS: cityRealtimeCharacterActivityMinimumIntervalUS, MinimumEnergyMilli: 70, MinimumSatietyMilli: 60,
			EnergyDeltaMilli: -35, SatietyDeltaMilli: -25, MoraleDeltaMilli: 12, CityCreditDelta: -3,
			Progression: &cityRealtimeCharacterActivityProgressionDefinition{ExperienceRewards: []CityRealtimeCharacterExperienceDelta{
				{AttributeCode: "communication", ExperienceUnits: 12}, {AttributeCode: "reasoning", ExperienceUnits: 24},
			}},
		},
		cityRealtimeCharacterActivityDefinition{
			Code: "work.civic_service", CategoryCode: "work", LocationRequirement: "road_or_sidewalk", PublicVisibility: true,
			MinimumIntervalUS: cityRealtimeCharacterActivityMinimumIntervalUS, MinimumEnergyMilli: 180, MinimumSatietyMilli: 140,
			EnergyDeltaMilli: -135, SatietyDeltaMilli: -82, MoraleDeltaMilli: 24, CivicStandingDelta: 18, CityCreditDelta: 42,
			ItemCode: cityRealtimeCharacterRationItemCode, ItemQuantityDelta: 1,
			Progression: &cityRealtimeCharacterActivityProgressionDefinition{
				RequiredRoleCodes: []string{"profession.civic_aide"},
				ExperienceRewards: []CityRealtimeCharacterExperienceDelta{
					{AttributeCode: "communication", ExperienceUnits: 28}, {AttributeCode: "discipline", ExperienceUnits: 32},
				},
			},
		},
		cityRealtimeCharacterActivityDefinition{
			Code: "work.maintenance_shift", CategoryCode: "work", LocationRequirement: "road_or_sidewalk", PublicVisibility: true,
			MinimumIntervalUS: cityRealtimeCharacterActivityMinimumIntervalUS, MinimumEnergyMilli: 190, MinimumSatietyMilli: 145,
			EnergyDeltaMilli: -150, SatietyDeltaMilli: -90, MoraleDeltaMilli: 18, CivicStandingDelta: 14, CityCreditDelta: 48,
			ItemCode: cityRealtimeCharacterRationItemCode, ItemQuantityDelta: 1,
			Progression: &cityRealtimeCharacterActivityProgressionDefinition{
				RequiredRoleCodes: []string{"profession.maintenance_worker"},
				ExperienceRewards: []CityRealtimeCharacterExperienceDelta{
					{AttributeCode: "coordination", ExperienceUnits: 32}, {AttributeCode: "vitality", ExperienceUnits: 26},
				},
			},
		},
	)
	return items
}

func cityRealtimeCharacterProgressionCodeValid(value string, maximum int) bool {
	if len(value) < 2 || len(value) > maximum || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for _, character := range value[1:] {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' ||
			character == '.' || character == '_' || character == '-' {
			continue
		}
		return false
	}
	return true
}

func cityRealtimeCharacterProgressionDefinitionValid(definition cityRealtimeCharacterProgressionDefinition) bool {
	if definition.SchemaVersion != cityRealtimeCharacterProgressionSchemaVersion || len(definition.Attributes) == 0 ||
		len(definition.Archetypes) == 0 || len(definition.Roles) == 0 {
		return false
	}
	attributes := make(map[string]cityRealtimeCharacterAttributeDefinition, len(definition.Attributes))
	for _, attribute := range definition.Attributes {
		if !cityRealtimeCharacterProgressionCodeValid(attribute.Code, 64) || attribute.ExperiencePerValueMilli <= 0 ||
			attribute.ExperiencePerValueMilli > cityRealtimeCharacterMaximumExperienceUnits ||
			attribute.MaximumExperienceUnits <= 0 || attribute.MaximumExperienceUnits > cityRealtimeCharacterMaximumExperienceUnits {
			return false
		}
		if _, duplicate := attributes[attribute.Code]; duplicate {
			return false
		}
		attributes[attribute.Code] = attribute
	}
	roles := make(map[string]cityRealtimeCharacterRoleDefinition, len(definition.Roles))
	for _, role := range definition.Roles {
		if !cityRealtimeCharacterProgressionCodeValid(role.Code, 96) ||
			!cityRealtimeCharacterProgressionCodeValid(role.CategoryCode, 64) {
			return false
		}
		if _, duplicate := roles[role.Code]; duplicate {
			return false
		}
		roles[role.Code] = role
	}
	for _, role := range definition.Roles {
		if !cityRealtimeCharacterRoleRequirementsValid(role.Requirements, attributes, roles) {
			return false
		}
	}
	archetypes := make(map[string]struct{}, len(definition.Archetypes))
	for _, archetype := range definition.Archetypes {
		if !cityRealtimeCharacterProgressionCodeValid(archetype.Code, 96) {
			return false
		}
		if _, duplicate := archetypes[archetype.Code]; duplicate {
			return false
		}
		archetypes[archetype.Code] = struct{}{}
		initialRole, exists := roles[archetype.InitialRoleCode]
		if !exists || initialRole.CategoryCode == "" || len(archetype.InitialAttributes) != len(attributes) {
			return false
		}
		seen := make(map[string]struct{}, len(archetype.InitialAttributes))
		for _, initial := range archetype.InitialAttributes {
			if _, exists = attributes[initial.AttributeCode]; !exists || initial.InitialValueMilli < 0 || initial.InitialValueMilli > 1000 {
				return false
			}
			if _, duplicate := seen[initial.AttributeCode]; duplicate {
				return false
			}
			seen[initial.AttributeCode] = struct{}{}
		}
	}
	return true
}

func cityRealtimeCharacterRoleRequirementsValid(
	requirements CityRealtimeCharacterRoleRequirements,
	attributes map[string]cityRealtimeCharacterAttributeDefinition,
	roles map[string]cityRealtimeCharacterRoleDefinition,
) bool {
	if requirements.MinimumCivicStandingMilli < 0 || requirements.MinimumCivicStandingMilli > 1000 ||
		requirements.MinimumTotalExperienceUnits < 0 || requirements.MinimumTotalExperienceUnits > cityRealtimeCharacterMaximumExperienceUnits {
		return false
	}
	seen := make(map[string]struct{}, len(requirements.Attributes))
	for _, requirement := range requirements.Attributes {
		if _, exists := attributes[requirement.AttributeCode]; !exists || requirement.MinimumValueMilli < 0 || requirement.MinimumValueMilli > 1000 {
			return false
		}
		if _, duplicate := seen[requirement.AttributeCode]; duplicate {
			return false
		}
		seen[requirement.AttributeCode] = struct{}{}
	}
	seen = make(map[string]struct{}, len(requirements.RequiredRoleCodes))
	for _, code := range requirements.RequiredRoleCodes {
		if !cityRealtimeCharacterProgressionCodeValid(code, 96) {
			return false
		}
		if _, exists := roles[code]; !exists {
			return false
		}
		if _, duplicate := seen[code]; duplicate {
			return false
		}
		seen[code] = struct{}{}
	}
	return true
}

func cityRealtimeCharacterActivityProgressionDefinitionValid(
	definition *cityRealtimeCharacterActivityProgressionDefinition,
	progression *cityRealtimeCharacterProgressionDefinition,
) bool {
	if definition == nil {
		return progression == nil
	}
	if progression == nil || !cityRealtimeCharacterProgressionDefinitionValid(*progression) {
		return false
	}
	attributeCodes := make(map[string]struct{}, len(progression.Attributes))
	roleCodes := make(map[string]struct{}, len(progression.Roles))
	for _, attribute := range progression.Attributes {
		attributeCodes[attribute.Code] = struct{}{}
	}
	for _, role := range progression.Roles {
		roleCodes[role.Code] = struct{}{}
	}
	seen := make(map[string]struct{}, len(definition.ExperienceRewards))
	for _, reward := range definition.ExperienceRewards {
		if _, exists := attributeCodes[reward.AttributeCode]; !exists || reward.ExperienceUnits <= 0 ||
			reward.ExperienceUnits > cityRealtimeCharacterMaximumExperienceUnits {
			return false
		}
		if _, duplicate := seen[reward.AttributeCode]; duplicate {
			return false
		}
		seen[reward.AttributeCode] = struct{}{}
	}
	seen = make(map[string]struct{}, len(definition.RequiredRoleCodes))
	for _, code := range definition.RequiredRoleCodes {
		if !cityRealtimeCharacterProgressionCodeValid(code, 96) {
			return false
		}
		if _, exists := roleCodes[code]; !exists {
			return false
		}
		if _, duplicate := seen[code]; duplicate {
			return false
		}
		seen[code] = struct{}{}
	}
	return true
}

func cityRealtimeCharacterProgressionDefinitionEqual(
	left, right *cityRealtimeCharacterProgressionDefinition,
) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	if !cityRealtimeCharacterProgressionDefinitionValid(*left) || !cityRealtimeCharacterProgressionDefinitionValid(*right) {
		return false
	}
	return reflect.DeepEqual(left, right)
}

func cityRealtimeCharacterActivityProgressionDefinitionEqual(
	left, right *cityRealtimeCharacterActivityProgressionDefinition,
) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return reflect.DeepEqual(left, right)
}

func cityRealtimeCharacterProgressionRuntimeEnabled(runtime *cityRealtimeCharacterLifeRuntime) bool {
	return runtime != nil && runtime.Progression != nil && cityRealtimeCharacterProgressionDefinitionValid(*runtime.Progression)
}

func cityRealtimeCharacterProgressionArchetype(
	definition *cityRealtimeCharacterProgressionDefinition,
	code string,
) (cityRealtimeCharacterArchetypeDefinition, bool) {
	if definition == nil {
		return cityRealtimeCharacterArchetypeDefinition{}, false
	}
	for _, archetype := range definition.Archetypes {
		if archetype.Code == code {
			return archetype, true
		}
	}
	return cityRealtimeCharacterArchetypeDefinition{}, false
}

func cityRealtimeCharacterProgressionRole(
	definition *cityRealtimeCharacterProgressionDefinition,
	code string,
) (cityRealtimeCharacterRoleDefinition, bool) {
	if definition == nil {
		return cityRealtimeCharacterRoleDefinition{}, false
	}
	for _, role := range definition.Roles {
		if role.Code == code {
			return role, true
		}
	}
	return cityRealtimeCharacterRoleDefinition{}, false
}

func cityRealtimeCharacterProgressionAttribute(
	definition *cityRealtimeCharacterProgressionDefinition,
	code string,
) (cityRealtimeCharacterAttributeDefinition, bool) {
	if definition == nil {
		return cityRealtimeCharacterAttributeDefinition{}, false
	}
	for _, attribute := range definition.Attributes {
		if attribute.Code == code {
			return attribute, true
		}
	}
	return cityRealtimeCharacterAttributeDefinition{}, false
}

func cityRealtimeCharacterResolveArchetype(
	runtime *cityRealtimeCharacterLifeRuntime,
	code string,
) (cityRealtimeCharacterArchetypeDefinition, error) {
	if !cityRealtimeCharacterProgressionRuntimeEnabled(runtime) {
		if strings.TrimSpace(code) == "" {
			return cityRealtimeCharacterArchetypeDefinition{}, nil
		}
		return cityRealtimeCharacterArchetypeDefinition{}, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "archetype_code"})
	}
	code = strings.TrimSpace(code)
	if code == "" {
		code = cityRealtimeCharacterDefaultArchetypeCode
	}
	archetype, found := cityRealtimeCharacterProgressionArchetype(runtime.Progression, code)
	if !found {
		return cityRealtimeCharacterArchetypeDefinition{}, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "archetype_code"})
	}
	return archetype, nil
}

func cityRealtimeCharacterArchetypeOptions(
	runtime *cityRealtimeCharacterLifeRuntime,
) []CityRealtimeCharacterArchetypeOption {
	if !cityRealtimeCharacterProgressionRuntimeEnabled(runtime) {
		return nil
	}
	items := make([]CityRealtimeCharacterArchetypeOption, 0, len(runtime.Progression.Archetypes))
	for _, archetype := range runtime.Progression.Archetypes {
		attributes := make([]CityRealtimeCharacterAttribute, 0, len(archetype.InitialAttributes))
		for _, initial := range archetype.InitialAttributes {
			attributes = append(attributes, CityRealtimeCharacterAttribute{
				Code: initial.AttributeCode, ValueMilli: initial.InitialValueMilli,
			})
		}
		items = append(items, CityRealtimeCharacterArchetypeOption{
			Code: archetype.Code, InitialRoleCode: archetype.InitialRoleCode, InitialAttributes: attributes,
		})
	}
	return items
}

func cityRealtimeCharacterAttributeStateValid(state cityRealtimeCharacterAttributeState) bool {
	return cityRealtimeCharacterProgressionCodeValid(state.AttributeCode, 64) &&
		state.ExperienceUnits >= 0 && state.ExperienceUnits <= cityRealtimeCharacterMaximumExperienceUnits &&
		state.Revision > 0 && state.LastFrameSequence > 0 && cityRealtimeSHA256Hex(state.StateHash)
}

func cityRealtimeCharacterRoleAssignmentValid(assignment cityRealtimeCharacterRoleAssignment) bool {
	return cityRealtimeCharacterProgressionCodeValid(assignment.CategoryCode, 64) &&
		cityRealtimeCharacterProgressionCodeValid(assignment.RoleCode, 96) &&
		assignment.GrantedFrameSequence > 0 && assignment.Revision > 0 && cityRealtimeSHA256Hex(assignment.StateHash)
}

func cityRealtimeCharacterAttributeStateHash(
	actorCode string,
	state cityRealtimeCharacterAttributeState,
) (string, error) {
	_, hash, err := cityRealtimeCanonicalJSONObject(map[string]any{
		"schema_version":      cityRealtimeCharacterProgressionStateSchemaVersion,
		"actor_code":          actorCode,
		"attribute_code":      state.AttributeCode,
		"experience_units":    state.ExperienceUnits,
		"revision":            state.Revision,
		"last_frame_sequence": state.LastFrameSequence,
	})
	if err != nil {
		return "", fmt.Errorf("canonicalize realtime character attribute state: %w", err)
	}
	return hash, nil
}

func cityRealtimeCharacterRoleAssignmentStateHash(
	actorCode string,
	assignment cityRealtimeCharacterRoleAssignment,
) (string, error) {
	_, hash, err := cityRealtimeCanonicalJSONObject(map[string]any{
		"schema_version":         cityRealtimeCharacterProgressionStateSchemaVersion,
		"actor_code":             actorCode,
		"category_code":          assignment.CategoryCode,
		"role_code":              assignment.RoleCode,
		"granted_frame_sequence": assignment.GrantedFrameSequence,
		"revision":               assignment.Revision,
	})
	if err != nil {
		return "", fmt.Errorf("canonicalize realtime character role assignment: %w", err)
	}
	return hash, nil
}

func cityRealtimeCharacterProgressionStateHash(profile cityRealtimeCharacterProfile) (string, error) {
	if profile.StateSchemaVersion != cityRealtimeCharacterProfileSchemaProgression ||
		!cityRealtimePlayerActorCodeValid(profile.ActorCode) || !cityRealtimeCharacterProgressionCodeValid(profile.ArchetypeCode, 96) ||
		profile.Attributes == nil || profile.Roles == nil {
		return "", ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_progression_state"})
	}
	attributes := make([]map[string]any, 0, len(profile.Attributes))
	for _, state := range profile.Attributes {
		attributes = append(attributes, map[string]any{
			"attribute_code":      state.AttributeCode,
			"experience_units":    state.ExperienceUnits,
			"revision":            state.Revision,
			"last_frame_sequence": state.LastFrameSequence,
			"state_hash":          state.StateHash,
		})
	}
	roles := make([]map[string]any, 0, len(profile.Roles))
	for _, assignment := range profile.Roles {
		roles = append(roles, map[string]any{
			"category_code":          assignment.CategoryCode,
			"role_code":              assignment.RoleCode,
			"granted_frame_sequence": assignment.GrantedFrameSequence,
			"revision":               assignment.Revision,
			"state_hash":             assignment.StateHash,
		})
	}
	_, hash, err := cityRealtimeCanonicalJSONObject(map[string]any{
		"schema_version": cityRealtimeCharacterProgressionStateSchemaVersion,
		"actor_code":     profile.ActorCode,
		"archetype_code": profile.ArchetypeCode,
		"attributes":     attributes,
		"roles":          roles,
	})
	if err != nil {
		return "", fmt.Errorf("canonicalize realtime character progression state: %w", err)
	}
	return hash, nil
}

func cityRealtimeCharacterProgressionChainGenesisHash(actorCode string, frameSequence int64) (string, error) {
	_, hash, err := cityRealtimeCanonicalJSONObject(map[string]any{
		"schema_version": cityRealtimeCharacterProgressionEventSchemaVersion,
		"namespace":      cityRealtimeCharacterProgressionChainNamespace,
		"actor_code":     actorCode,
		"frame_sequence": frameSequence,
	})
	if err != nil {
		return "", fmt.Errorf("canonicalize realtime character progression genesis: %w", err)
	}
	return hash, nil
}

func cityRealtimeCharacterProgressionEventHash(event cityRealtimeCharacterProgressionEventRecord) (string, error) {
	if !cityRealtimePlayerActorCodeValid(event.ActorCode) || event.EventSequence <= 0 || event.FrameSequence <= 0 ||
		(event.EventKind != "activity" && event.EventKind != "role") || !cityRealtimeSHA256Hex(event.PreviousEventHash) {
		return "", ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_progression_event"})
	}
	deltas := make([]map[string]any, 0, len(event.ExperienceDeltas))
	for _, delta := range event.ExperienceDeltas {
		deltas = append(deltas, map[string]any{
			"attribute_code": delta.AttributeCode, "experience_units": delta.ExperienceUnits,
		})
	}
	value := map[string]any{
		"schema_version":      cityRealtimeCharacterProgressionEventSchemaVersion,
		"actor_code":          event.ActorCode,
		"event_sequence":      event.EventSequence,
		"frame_sequence":      event.FrameSequence,
		"event_kind":          event.EventKind,
		"experience_deltas":   deltas,
		"previous_event_hash": event.PreviousEventHash,
	}
	if event.ActivityEventSequence != nil {
		value["activity_event_sequence"] = *event.ActivityEventSequence
	}
	if event.CategoryCode != nil {
		value["category_code"] = *event.CategoryCode
	}
	if event.FromRoleCode != nil {
		value["from_role_code"] = *event.FromRoleCode
	}
	if event.ToRoleCode != nil {
		value["to_role_code"] = *event.ToRoleCode
	}
	_, hash, err := cityRealtimeCanonicalJSONObject(value)
	if err != nil {
		return "", fmt.Errorf("canonicalize realtime character progression event: %w", err)
	}
	return hash, nil
}

func cityRealtimeCharacterProgressionProfileValid(profile cityRealtimeCharacterProfile) bool {
	if profile.StateSchemaVersion != cityRealtimeCharacterProfileSchemaProgression ||
		!cityRealtimeCharacterProgressionCodeValid(profile.ArchetypeCode, 96) ||
		profile.ProgressionRevision < 0 || !cityRealtimeSHA256Hex(profile.ProgressionEventChainHash) ||
		!cityRealtimeSHA256Hex(profile.ProgressionStateHash) || len(profile.Attributes) == 0 || len(profile.Roles) == 0 {
		return false
	}
	for index, state := range profile.Attributes {
		if !cityRealtimeCharacterAttributeStateValid(state) ||
			(index > 0 && profile.Attributes[index-1].AttributeCode >= state.AttributeCode) {
			return false
		}
		expected, err := cityRealtimeCharacterAttributeStateHash(profile.ActorCode, state)
		if err != nil || expected != state.StateHash {
			return false
		}
	}
	for index, assignment := range profile.Roles {
		if !cityRealtimeCharacterRoleAssignmentValid(assignment) ||
			(index > 0 && profile.Roles[index-1].CategoryCode >= assignment.CategoryCode) {
			return false
		}
		expected, err := cityRealtimeCharacterRoleAssignmentStateHash(profile.ActorCode, assignment)
		if err != nil || expected != assignment.StateHash {
			return false
		}
	}
	expectedHash, err := cityRealtimeCharacterProgressionStateHash(profile)
	return err == nil && expectedHash == profile.ProgressionStateHash
}

func cityRealtimeCharacterProfileMatchesProgressionRuntime(
	profile cityRealtimeCharacterProfile,
	runtime *cityRealtimeCharacterLifeRuntime,
) bool {
	if !cityRealtimeCharacterProgressionRuntimeEnabled(runtime) || !cityRealtimeCharacterProgressionProfileValid(profile) {
		return false
	}
	archetype, found := cityRealtimeCharacterProgressionArchetype(runtime.Progression, profile.ArchetypeCode)
	if !found || len(profile.Attributes) != len(runtime.Progression.Attributes) || len(profile.Roles) == 0 {
		return false
	}
	if _, found = cityRealtimeCharacterProgressionRole(runtime.Progression, archetype.InitialRoleCode); !found {
		return false
	}
	for index, state := range profile.Attributes {
		definition, exists := cityRealtimeCharacterProgressionAttribute(runtime.Progression, state.AttributeCode)
		if !exists || state.ExperienceUnits > definition.MaximumExperienceUnits ||
			(index > 0 && profile.Attributes[index-1].AttributeCode >= state.AttributeCode) {
			return false
		}
	}
	for _, assignment := range profile.Roles {
		role, exists := cityRealtimeCharacterProgressionRole(runtime.Progression, assignment.RoleCode)
		if !exists || role.CategoryCode != assignment.CategoryCode {
			return false
		}
	}
	return true
}

func cityRealtimeCharacterProfileTotalExperience(profile cityRealtimeCharacterProfile) int64 {
	var total int64
	for _, state := range profile.Attributes {
		if state.ExperienceUnits > cityRealtimeCharacterMaximumExperienceUnits-total {
			return cityRealtimeCharacterMaximumExperienceUnits
		}
		total += state.ExperienceUnits
	}
	return total
}

func cityRealtimeCharacterProfileRoleSet(profile cityRealtimeCharacterProfile) map[string]struct{} {
	roles := make(map[string]struct{}, len(profile.Roles))
	for _, assignment := range profile.Roles {
		roles[assignment.RoleCode] = struct{}{}
	}
	return roles
}

func cityRealtimeCharacterAttributeValue(
	archetype cityRealtimeCharacterArchetypeDefinition,
	definition cityRealtimeCharacterAttributeDefinition,
	state cityRealtimeCharacterAttributeState,
) (int64, bool) {
	if definition.Code != state.AttributeCode || definition.ExperiencePerValueMilli <= 0 ||
		state.ExperienceUnits < 0 || state.ExperienceUnits > definition.MaximumExperienceUnits {
		return 0, false
	}
	var initial int64 = -1
	for _, candidate := range archetype.InitialAttributes {
		if candidate.AttributeCode == definition.Code {
			initial = candidate.InitialValueMilli
			break
		}
	}
	if initial < 0 || initial > 1000 {
		return 0, false
	}
	value := initial + state.ExperienceUnits/definition.ExperiencePerValueMilli
	if value > 1000 {
		value = 1000
	}
	return value, true
}

func cityRealtimeCharacterProfileAttributeValue(
	profile cityRealtimeCharacterProfile,
	runtime *cityRealtimeCharacterLifeRuntime,
	attributeCode string,
) (int64, bool) {
	if !cityRealtimeCharacterProgressionRuntimeEnabled(runtime) {
		return 0, false
	}
	archetype, found := cityRealtimeCharacterProgressionArchetype(runtime.Progression, profile.ArchetypeCode)
	if !found {
		return 0, false
	}
	definition, found := cityRealtimeCharacterProgressionAttribute(runtime.Progression, attributeCode)
	if !found {
		return 0, false
	}
	for _, state := range profile.Attributes {
		if state.AttributeCode == attributeCode {
			return cityRealtimeCharacterAttributeValue(archetype, definition, state)
		}
	}
	return 0, false
}

func cityRealtimeCharacterRoleAvailabilityForProfile(
	profile cityRealtimeCharacterProfile,
	runtime *cityRealtimeCharacterLifeRuntime,
	role cityRealtimeCharacterRoleDefinition,
) CityRealtimeCharacterRoleAvailability {
	item := CityRealtimeCharacterRoleAvailability{
		Code: role.Code, CategoryCode: role.CategoryCode, Requirements: role.Requirements,
	}
	for _, assignment := range profile.Roles {
		if assignment.CategoryCode == role.CategoryCode && assignment.RoleCode == role.Code {
			item.ReasonCode = "active"
			return item
		}
	}
	if profile.CivicStandingMilli < role.Requirements.MinimumCivicStandingMilli {
		item.ReasonCode = "civic_standing"
		return item
	}
	if cityRealtimeCharacterProfileTotalExperience(profile) < role.Requirements.MinimumTotalExperienceUnits {
		item.ReasonCode = "experience"
		return item
	}
	activeRoles := cityRealtimeCharacterProfileRoleSet(profile)
	for _, required := range role.Requirements.RequiredRoleCodes {
		if _, found := activeRoles[required]; !found {
			item.ReasonCode = "role"
			return item
		}
	}
	for _, requirement := range role.Requirements.Attributes {
		value, found := cityRealtimeCharacterProfileAttributeValue(profile, runtime, requirement.AttributeCode)
		if !found || value < requirement.MinimumValueMilli {
			item.ReasonCode = "attribute"
			return item
		}
	}
	item.Available = true
	return item
}

func cityRealtimeCharacterActivityProgressionAvailable(
	profile cityRealtimeCharacterProfile,
	runtime *cityRealtimeCharacterLifeRuntime,
	definition cityRealtimeCharacterActivityDefinition,
) (bool, string) {
	if definition.Progression == nil {
		return true, ""
	}
	if !cityRealtimeCharacterProgressionRuntimeEnabled(runtime) || !cityRealtimeCharacterProfileMatchesProgressionRuntime(profile, runtime) ||
		!cityRealtimeCharacterActivityProgressionDefinitionValid(definition.Progression, runtime.Progression) {
		return false, "progression"
	}
	activeRoles := cityRealtimeCharacterProfileRoleSet(profile)
	for _, code := range definition.Progression.RequiredRoleCodes {
		if _, found := activeRoles[code]; !found {
			return false, "role"
		}
	}
	return true, ""
}

func cityRealtimeCharacterLifeProjection(
	profile cityRealtimeCharacterProfile,
	runtime *cityRealtimeCharacterLifeRuntime,
) (CityRealtimeCharacterLife, error) {
	life := profile.projection()
	if runtime == nil || runtime.Progression == nil {
		return life, nil
	}
	if !cityRealtimeCharacterProfileMatchesProgressionRuntime(profile, runtime) {
		return CityRealtimeCharacterLife{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_progression_projection"})
	}
	archetype, _ := cityRealtimeCharacterProgressionArchetype(runtime.Progression, profile.ArchetypeCode)
	progression := &CityRealtimeCharacterProgression{
		SchemaVersion:  cityRealtimeCharacterProgressionSchemaVersion,
		ArchetypeCode:  profile.ArchetypeCode,
		Revision:       profile.ProgressionRevision,
		Attributes:     make([]CityRealtimeCharacterAttribute, 0, len(profile.Attributes)),
		Roles:          make([]CityRealtimeCharacterRole, 0, len(profile.Roles)),
		AvailableRoles: make([]CityRealtimeCharacterRoleAvailability, 0, len(runtime.Progression.Roles)),
	}
	for _, state := range profile.Attributes {
		definition, _ := cityRealtimeCharacterProgressionAttribute(runtime.Progression, state.AttributeCode)
		value, valid := cityRealtimeCharacterAttributeValue(archetype, definition, state)
		if !valid {
			return CityRealtimeCharacterLife{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_attribute_projection"})
		}
		progression.Attributes = append(progression.Attributes, CityRealtimeCharacterAttribute{
			Code: state.AttributeCode, ValueMilli: value, ExperienceUnits: state.ExperienceUnits,
			Revision: state.Revision, LastFrameSequence: state.LastFrameSequence,
		})
	}
	for _, assignment := range profile.Roles {
		progression.Roles = append(progression.Roles, CityRealtimeCharacterRole{
			Code: assignment.RoleCode, CategoryCode: assignment.CategoryCode,
			GrantedFrameSequence: assignment.GrantedFrameSequence, Revision: assignment.Revision,
		})
	}
	for _, role := range runtime.Progression.Roles {
		progression.AvailableRoles = append(progression.AvailableRoles,
			cityRealtimeCharacterRoleAvailabilityForProfile(profile, runtime, role))
	}
	life.Progression = progression
	return life, nil
}

func cityRealtimeCharacterApplyExperienceRewards(
	profile cityRealtimeCharacterProfile,
	runtime *cityRealtimeCharacterLifeRuntime,
	rewards []CityRealtimeCharacterExperienceDelta,
	frameSequence int64,
) (cityRealtimeCharacterProfile, []cityRealtimeCharacterAttributeState, []CityRealtimeCharacterExperienceDelta, error) {
	if !cityRealtimeCharacterProfileMatchesProgressionRuntime(profile, runtime) || frameSequence <= profile.LastFrameSequence {
		return cityRealtimeCharacterProfile{}, nil, nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_progression_activity"})
	}
	if len(rewards) == 0 {
		return profile, nil, nil, nil
	}
	sortedRewards := append([]CityRealtimeCharacterExperienceDelta(nil), rewards...)
	sort.Slice(sortedRewards, func(left, right int) bool {
		return sortedRewards[left].AttributeCode < sortedRewards[right].AttributeCode
	})
	if !cityRealtimeCharacterActivityProgressionDefinitionValid(&cityRealtimeCharacterActivityProgressionDefinition{ExperienceRewards: sortedRewards}, runtime.Progression) {
		return cityRealtimeCharacterProfile{}, nil, nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_progression_rewards"})
	}
	next := profile
	next.Attributes = append([]cityRealtimeCharacterAttributeState(nil), profile.Attributes...)
	updates := make([]cityRealtimeCharacterAttributeState, 0, len(sortedRewards))
	for _, reward := range sortedRewards {
		definition, _ := cityRealtimeCharacterProgressionAttribute(runtime.Progression, reward.AttributeCode)
		index := sort.Search(len(next.Attributes), func(index int) bool {
			return next.Attributes[index].AttributeCode >= reward.AttributeCode
		})
		if index >= len(next.Attributes) || next.Attributes[index].AttributeCode != reward.AttributeCode ||
			next.Attributes[index].ExperienceUnits > definition.MaximumExperienceUnits-reward.ExperienceUnits {
			return cityRealtimeCharacterProfile{}, nil, nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_progression_experience"})
		}
		state := next.Attributes[index]
		state.ExperienceUnits += reward.ExperienceUnits
		state.Revision++
		state.LastFrameSequence = frameSequence
		var err error
		state.StateHash, err = cityRealtimeCharacterAttributeStateHash(next.ActorCode, state)
		if err != nil {
			return cityRealtimeCharacterProfile{}, nil, nil, err
		}
		next.Attributes[index] = state
		updates = append(updates, state)
	}
	return next, updates, sortedRewards, nil
}

func cityRealtimeCharacterSeedProgression(
	profile *cityRealtimeCharacterProfile,
	runtime *cityRealtimeCharacterLifeRuntime,
	archetype cityRealtimeCharacterArchetypeDefinition,
	frameSequence int64,
) error {
	if profile == nil || !cityRealtimeCharacterProgressionRuntimeEnabled(runtime) || frameSequence <= 0 ||
		archetype.Code == "" || profile.ActorCode == "" {
		return ErrCityInvalidInput
	}
	if _, found := cityRealtimeCharacterProgressionArchetype(runtime.Progression, archetype.Code); !found {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_archetype"})
	}
	initialByAttribute := make(map[string]int64, len(archetype.InitialAttributes))
	for _, initial := range archetype.InitialAttributes {
		initialByAttribute[initial.AttributeCode] = initial.InitialValueMilli
	}
	attributes := make([]cityRealtimeCharacterAttributeState, 0, len(runtime.Progression.Attributes))
	for _, definition := range runtime.Progression.Attributes {
		if _, found := initialByAttribute[definition.Code]; !found {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_archetype_attributes"})
		}
		state := cityRealtimeCharacterAttributeState{
			AttributeCode: definition.Code, ExperienceUnits: 0, Revision: 1, LastFrameSequence: frameSequence,
		}
		var err error
		state.StateHash, err = cityRealtimeCharacterAttributeStateHash(profile.ActorCode, state)
		if err != nil {
			return err
		}
		attributes = append(attributes, state)
	}
	initialRole, found := cityRealtimeCharacterProgressionRole(runtime.Progression, archetype.InitialRoleCode)
	if !found {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_archetype_role"})
	}
	assignment := cityRealtimeCharacterRoleAssignment{
		CategoryCode: initialRole.CategoryCode, RoleCode: initialRole.Code,
		GrantedFrameSequence: frameSequence, Revision: 1,
	}
	var err error
	assignment.StateHash, err = cityRealtimeCharacterRoleAssignmentStateHash(profile.ActorCode, assignment)
	if err != nil {
		return err
	}
	chainHash, err := cityRealtimeCharacterProgressionChainGenesisHash(profile.ActorCode, frameSequence)
	if err != nil {
		return err
	}
	profile.StateSchemaVersion = cityRealtimeCharacterProfileSchemaProgression
	profile.ArchetypeCode = archetype.Code
	profile.ProgressionRevision = 0
	profile.ProgressionEventChainHash = chainHash
	profile.Attributes = attributes
	profile.Roles = []cityRealtimeCharacterRoleAssignment{assignment}
	profile.ProgressionStateHash, err = cityRealtimeCharacterProgressionStateHash(*profile)
	if err != nil {
		return err
	}
	if !cityRealtimeCharacterProfileMatchesProgressionRuntime(*profile, runtime) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_progression_seed"})
	}
	return nil
}

func seedCityRealtimeCharacterProgressionState(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	profile cityRealtimeCharacterProfile,
) error {
	if tx == nil || worldID <= 0 || profile.StateSchemaVersion != cityRealtimeCharacterProfileSchemaProgression ||
		!cityRealtimeCharacterProgressionProfileValid(profile) {
		return ErrCityInvalidInput
	}
	for _, state := range profile.Attributes {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_realtime_character_attribute_states
    (world_id, actor_code, attribute_code, experience_units, revision, last_frame_sequence, state_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, '{}'::jsonb)`,
			worldID, profile.ActorCode, state.AttributeCode, state.ExperienceUnits, state.Revision,
			state.LastFrameSequence, state.StateHash,
		); err != nil {
			return fmt.Errorf("seed realtime character attribute %s: %w", state.AttributeCode, err)
		}
	}
	for _, assignment := range profile.Roles {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_realtime_character_role_assignments
    (world_id, actor_code, category_code, role_code, granted_frame_sequence, revision, state_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, '{}'::jsonb)`,
			worldID, profile.ActorCode, assignment.CategoryCode, assignment.RoleCode,
			assignment.GrantedFrameSequence, assignment.Revision, assignment.StateHash,
		); err != nil {
			return fmt.Errorf("seed realtime character role %s: %w", assignment.RoleCode, err)
		}
	}
	return nil
}

func nullableCityRealtimeCharacterProgressionString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func loadCityRealtimeCharacterProgressionState(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	actorCode string,
	forUpdate bool,
) ([]cityRealtimeCharacterAttributeState, []cityRealtimeCharacterRoleAssignment, error) {
	if worldID <= 0 || !cityRealtimePlayerActorCodeValid(actorCode) {
		return nil, nil, ErrCityInvalidInput
	}
	attributeQuery := `
SELECT attribute_code, experience_units, revision, last_frame_sequence, state_hash
FROM city_realtime_character_attribute_states
WHERE world_id = $1 AND actor_code = $2
ORDER BY attribute_code ASC`
	if forUpdate {
		attributeQuery += " FOR UPDATE"
	}
	rows, err := queryer.QueryContext(ctx, attributeQuery, worldID, actorCode)
	if err != nil {
		return nil, nil, fmt.Errorf("load realtime character attributes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	attributes := make([]cityRealtimeCharacterAttributeState, 0)
	for rows.Next() {
		state := cityRealtimeCharacterAttributeState{}
		if err = rows.Scan(&state.AttributeCode, &state.ExperienceUnits, &state.Revision, &state.LastFrameSequence, &state.StateHash); err != nil {
			return nil, nil, err
		}
		attributes = append(attributes, state)
	}
	if err = rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate realtime character attributes: %w", err)
	}
	if err = rows.Close(); err != nil {
		return nil, nil, fmt.Errorf("close realtime character attributes: %w", err)
	}
	roleQuery := `
SELECT category_code, role_code, granted_frame_sequence, revision, state_hash
FROM city_realtime_character_role_assignments
WHERE world_id = $1 AND actor_code = $2
ORDER BY category_code ASC`
	if forUpdate {
		roleQuery += " FOR UPDATE"
	}
	rows, err = queryer.QueryContext(ctx, roleQuery, worldID, actorCode)
	if err != nil {
		return nil, nil, fmt.Errorf("load realtime character role assignments: %w", err)
	}
	defer func() { _ = rows.Close() }()
	roles := make([]cityRealtimeCharacterRoleAssignment, 0)
	for rows.Next() {
		assignment := cityRealtimeCharacterRoleAssignment{}
		if err = rows.Scan(
			&assignment.CategoryCode, &assignment.RoleCode, &assignment.GrantedFrameSequence,
			&assignment.Revision, &assignment.StateHash,
		); err != nil {
			return nil, nil, err
		}
		roles = append(roles, assignment)
	}
	if err = rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate realtime character role assignments: %w", err)
	}
	if err = rows.Close(); err != nil {
		return nil, nil, fmt.Errorf("close realtime character role assignments: %w", err)
	}
	return attributes, roles, nil
}

func updateCityRealtimeCharacterAttributeState(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	actorCode string,
	next cityRealtimeCharacterAttributeState,
) error {
	if tx == nil || worldID <= 0 || !cityRealtimePlayerActorCodeValid(actorCode) || !cityRealtimeCharacterAttributeStateValid(next) {
		return ErrCityInvalidInput
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_realtime_character_attribute_states
SET experience_units = $4, revision = $5, last_frame_sequence = $6, state_hash = $7, updated_at = NOW()
WHERE world_id = $1 AND actor_code = $2 AND attribute_code = $3
  AND revision = $8 AND last_frame_sequence < $6`,
		worldID, actorCode, next.AttributeCode, next.ExperienceUnits, next.Revision,
		next.LastFrameSequence, next.StateHash, next.Revision-1,
	)
	if err != nil {
		return fmt.Errorf("update realtime character attribute %s: %w", next.AttributeCode, err)
	}
	rows, rowsErr := result.RowsAffected()
	if rowsErr != nil {
		return fmt.Errorf("check realtime character attribute update: %w", rowsErr)
	}
	if rows != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_attribute_revision"})
	}
	return nil
}

func updateCityRealtimeCharacterRoleAssignment(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	actorCode string,
	previous, next cityRealtimeCharacterRoleAssignment,
) error {
	if tx == nil || worldID <= 0 || !cityRealtimePlayerActorCodeValid(actorCode) ||
		previous.CategoryCode != next.CategoryCode || !cityRealtimeCharacterRoleAssignmentValid(next) ||
		next.Revision != previous.Revision+1 || next.GrantedFrameSequence <= previous.GrantedFrameSequence {
		return ErrCityInvalidInput
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_realtime_character_role_assignments
SET role_code = $4, granted_frame_sequence = $5, revision = $6, state_hash = $7, updated_at = NOW()
WHERE world_id = $1 AND actor_code = $2 AND category_code = $3
  AND role_code = $8 AND revision = $9 AND granted_frame_sequence < $5`,
		worldID, actorCode, next.CategoryCode, next.RoleCode, next.GrantedFrameSequence,
		next.Revision, next.StateHash, previous.RoleCode, previous.Revision,
	)
	if err != nil {
		return fmt.Errorf("update realtime character role assignment %s: %w", next.CategoryCode, err)
	}
	rows, rowsErr := result.RowsAffected()
	if rowsErr != nil {
		return fmt.Errorf("check realtime character role assignment update: %w", rowsErr)
	}
	if rows != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_role_revision"})
	}
	return nil
}

type cityRealtimeCharacterActivityProgressionTransition struct {
	Profile          cityRealtimeCharacterProfile
	Activity         cityRealtimeCharacterActivityEventRecord
	Law              *cityRealtimeCharacterLawEventRecord
	Inventory        *cityRealtimeCharacterInventoryStack
	AttributeUpdates []cityRealtimeCharacterAttributeState
	ProgressionEvent *cityRealtimeCharacterProgressionEventRecord
	Result           CityRealtimeCharacterActivityResult
}

func cityRealtimeCharacterApplyActivityWithRuntime(
	profile cityRealtimeCharacterProfile,
	runtime *cityRealtimeCharacterLifeRuntime,
	definition cityRealtimeCharacterActivityDefinition,
	frameSequence, worldTimeUS int64,
) (cityRealtimeCharacterActivityProgressionTransition, error) {
	if runtime == nil || !cityRealtimeCharacterProfileMatchesRuntime(profile, runtime) {
		return cityRealtimeCharacterActivityProgressionTransition{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{
			"field": "realtime_character_activity_runtime",
		})
	}
	if available, reason := cityRealtimeCharacterActivityProgressionAvailable(profile, runtime, definition); !available {
		return cityRealtimeCharacterActivityProgressionTransition{}, ErrCityRealtimeCharacterActivityUnavailable.WithMetadata(map[string]string{
			"reason": reason,
		})
	}
	next, activity, law, inventory, result, err := cityRealtimeCharacterApplyActivity(
		profile, definition, frameSequence, worldTimeUS,
	)
	if err != nil {
		return cityRealtimeCharacterActivityProgressionTransition{}, err
	}
	transition := cityRealtimeCharacterActivityProgressionTransition{
		Profile: next, Activity: activity, Law: law, Inventory: inventory, Result: result,
	}
	if runtime.Progression == nil || definition.Progression == nil || len(definition.Progression.ExperienceRewards) == 0 {
		return transition, nil
	}
	progressed, updates, deltas, err := cityRealtimeCharacterApplyExperienceRewards(
		profile, runtime, definition.Progression.ExperienceRewards, frameSequence,
	)
	if err != nil {
		return cityRealtimeCharacterActivityProgressionTransition{}, err
	}
	if len(updates) == 0 {
		return transition, nil
	}
	if next.ProgressionRevision >= cityRealtimeCharacterMaximumExperienceUnits {
		return cityRealtimeCharacterActivityProgressionTransition{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{
			"field": "realtime_character_progression_revision",
		})
	}
	next.Attributes = progressed.Attributes
	next.ProgressionRevision++
	activitySequence := activity.EventSequence
	progressionEvent := cityRealtimeCharacterProgressionEventRecord{
		ActorCode: profile.ActorCode, EventSequence: next.ProgressionRevision, FrameSequence: frameSequence,
		EventKind: "activity", ActivityEventSequence: &activitySequence,
		ExperienceDeltas: deltas, PreviousEventHash: profile.ProgressionEventChainHash,
	}
	progressionEvent.EventHash, err = cityRealtimeCharacterProgressionEventHash(progressionEvent)
	if err != nil {
		return cityRealtimeCharacterActivityProgressionTransition{}, err
	}
	next.ProgressionEventChainHash = progressionEvent.EventHash
	next.ProgressionStateHash, err = cityRealtimeCharacterProgressionStateHash(next)
	if err != nil {
		return cityRealtimeCharacterActivityProgressionTransition{}, err
	}
	next.StateHash, err = cityRealtimeCharacterProfileStateHash(next)
	if err != nil {
		return cityRealtimeCharacterActivityProgressionTransition{}, err
	}
	if !cityRealtimeCharacterProfileMatchesRuntime(next, runtime) {
		return cityRealtimeCharacterActivityProgressionTransition{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{
			"field": "realtime_character_activity_progression_state",
		})
	}
	result.ExperienceDeltas = append([]CityRealtimeCharacterExperienceDelta(nil), deltas...)
	transition.Profile = next
	transition.AttributeUpdates = updates
	transition.ProgressionEvent = &progressionEvent
	transition.Result = result
	return transition, nil
}

func insertCityRealtimeCharacterProgressionEvent(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	actorCode string,
	event cityRealtimeCharacterProgressionEventRecord,
) error {
	if tx == nil || worldID <= 0 || actorCode == "" || event.ActorCode != actorCode || event.EventSequence <= 0 ||
		event.FrameSequence <= 0 || !cityRealtimeSHA256Hex(event.PreviousEventHash) || !cityRealtimeSHA256Hex(event.EventHash) ||
		(event.EventKind != "activity" && event.EventKind != "role") {
		return ErrCityInvalidInput
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_realtime_character_progression_events
    (world_id, actor_code, event_sequence, frame_sequence, event_kind, activity_event_sequence,
     category_code, from_role_code, to_role_code, experience_deltas, previous_event_hash, event_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb, $11, $12, '{}'::jsonb)`,
		worldID, actorCode, event.EventSequence, event.FrameSequence, event.EventKind, event.ActivityEventSequence,
		event.CategoryCode, event.FromRoleCode, event.ToRoleCode, cityRealtimeCharacterExperienceDeltasJSON(event.ExperienceDeltas),
		event.PreviousEventHash, event.EventHash,
	); err != nil {
		return fmt.Errorf("append realtime character progression event: %w", err)
	}
	return nil
}

func cityRealtimeCharacterExperienceDeltasJSON(deltas []CityRealtimeCharacterExperienceDelta) []byte {
	if deltas == nil {
		return []byte("[]")
	}
	value, err := json.Marshal(deltas)
	if err != nil {
		return []byte("[]")
	}
	return value
}

func insertCityRealtimeCharacterRoleAssignment(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	actorCode string,
	assignment cityRealtimeCharacterRoleAssignment,
) error {
	if tx == nil || worldID <= 0 || !cityRealtimePlayerActorCodeValid(actorCode) ||
		!cityRealtimeCharacterRoleAssignmentValid(assignment) || assignment.Revision != 1 {
		return ErrCityInvalidInput
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_realtime_character_role_assignments
    (world_id, actor_code, category_code, role_code, granted_frame_sequence, revision, state_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, '{}'::jsonb)`,
		worldID, actorCode, assignment.CategoryCode, assignment.RoleCode,
		assignment.GrantedFrameSequence, assignment.Revision, assignment.StateHash,
	); err != nil {
		return fmt.Errorf("insert realtime character role assignment %s: %w", assignment.CategoryCode, err)
	}
	return nil
}

func cityRealtimeCharacterApplyRoleChange(
	profile cityRealtimeCharacterProfile,
	runtime *cityRealtimeCharacterLifeRuntime,
	roleCode string,
	frameSequence int64,
) (cityRealtimeCharacterProfile, *cityRealtimeCharacterRoleAssignment, *cityRealtimeCharacterRoleAssignment, cityRealtimeCharacterProgressionEventRecord, CityRealtimeCharacterRoleChangeResult, error) {
	if !cityRealtimeCharacterProfileMatchesProgressionRuntime(profile, runtime) || frameSequence <= profile.LastFrameSequence ||
		!cityRealtimeCharacterProgressionCodeValid(roleCode, 96) {
		return cityRealtimeCharacterProfile{}, nil, nil, cityRealtimeCharacterProgressionEventRecord{}, CityRealtimeCharacterRoleChangeResult{},
			ErrCityInvalidInput.WithMetadata(map[string]string{"field": "role_code"})
	}
	role, found := cityRealtimeCharacterProgressionRole(runtime.Progression, roleCode)
	if !found {
		return cityRealtimeCharacterProfile{}, nil, nil, cityRealtimeCharacterProgressionEventRecord{}, CityRealtimeCharacterRoleChangeResult{},
			ErrCityInvalidInput.WithMetadata(map[string]string{"field": "role_code"})
	}
	availability := cityRealtimeCharacterRoleAvailabilityForProfile(profile, runtime, role)
	if !availability.Available {
		return cityRealtimeCharacterProfile{}, nil, nil, cityRealtimeCharacterProgressionEventRecord{}, CityRealtimeCharacterRoleChangeResult{},
			ErrCityRealtimeCharacterRoleUnavailable.WithMetadata(map[string]string{"reason": availability.ReasonCode})
	}
	next := profile
	next.Roles = append([]cityRealtimeCharacterRoleAssignment(nil), profile.Roles...)
	index := sort.Search(len(next.Roles), func(index int) bool {
		return next.Roles[index].CategoryCode >= role.CategoryCode
	})
	var previous *cityRealtimeCharacterRoleAssignment
	assignment := cityRealtimeCharacterRoleAssignment{
		CategoryCode: role.CategoryCode, RoleCode: role.Code, GrantedFrameSequence: frameSequence, Revision: 1,
	}
	if index < len(next.Roles) && next.Roles[index].CategoryCode == role.CategoryCode {
		old := next.Roles[index]
		previous = &old
		assignment.Revision = old.Revision + 1
		next.Roles[index] = assignment
	} else {
		next.Roles = append(next.Roles, cityRealtimeCharacterRoleAssignment{})
		copy(next.Roles[index+1:], next.Roles[index:])
		next.Roles[index] = assignment
	}
	var err error
	assignment.StateHash, err = cityRealtimeCharacterRoleAssignmentStateHash(profile.ActorCode, assignment)
	if err != nil {
		return cityRealtimeCharacterProfile{}, nil, nil, cityRealtimeCharacterProgressionEventRecord{}, CityRealtimeCharacterRoleChangeResult{}, err
	}
	next.Roles[index] = assignment
	if next.ProgressionRevision >= cityRealtimeCharacterMaximumExperienceUnits {
		return cityRealtimeCharacterProfile{}, nil, nil, cityRealtimeCharacterProgressionEventRecord{}, CityRealtimeCharacterRoleChangeResult{},
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_progression_revision"})
	}
	next.Revision++
	next.ProgressionRevision++
	next.LastFrameSequence = frameSequence
	var categoryCode, fromRoleCode string
	categoryCode = role.CategoryCode
	if previous != nil {
		fromRoleCode = previous.RoleCode
	}
	toRoleCode := role.Code
	event := cityRealtimeCharacterProgressionEventRecord{
		ActorCode: profile.ActorCode, EventSequence: next.ProgressionRevision, FrameSequence: frameSequence,
		EventKind: "role", CategoryCode: &categoryCode, ToRoleCode: &toRoleCode,
		ExperienceDeltas: []CityRealtimeCharacterExperienceDelta{}, PreviousEventHash: profile.ProgressionEventChainHash,
	}
	if previous != nil {
		event.FromRoleCode = &fromRoleCode
	}
	event.EventHash, err = cityRealtimeCharacterProgressionEventHash(event)
	if err != nil {
		return cityRealtimeCharacterProfile{}, nil, nil, cityRealtimeCharacterProgressionEventRecord{}, CityRealtimeCharacterRoleChangeResult{}, err
	}
	next.ProgressionEventChainHash = event.EventHash
	next.ProgressionStateHash, err = cityRealtimeCharacterProgressionStateHash(next)
	if err != nil {
		return cityRealtimeCharacterProfile{}, nil, nil, cityRealtimeCharacterProgressionEventRecord{}, CityRealtimeCharacterRoleChangeResult{}, err
	}
	next.StateHash, err = cityRealtimeCharacterProfileStateHash(next)
	if err != nil {
		return cityRealtimeCharacterProfile{}, nil, nil, cityRealtimeCharacterProgressionEventRecord{}, CityRealtimeCharacterRoleChangeResult{}, err
	}
	if !cityRealtimeCharacterProfileMatchesProgressionRuntime(next, runtime) {
		return cityRealtimeCharacterProfile{}, nil, nil, cityRealtimeCharacterProgressionEventRecord{}, CityRealtimeCharacterRoleChangeResult{},
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_role_state"})
	}
	result := CityRealtimeCharacterRoleChangeResult{
		CategoryCode: role.CategoryCode, FromRoleCode: fromRoleCode, ToRoleCode: role.Code,
	}
	return next, previous, &assignment, event, result, nil
}

func normalizeCityRealtimeCharacterRoleCode(value string) (string, error) {
	value = strings.TrimSpace(value)
	if !cityRealtimeCharacterProgressionCodeValid(value, 96) {
		return "", ErrCityInvalidInput.WithMetadata(map[string]string{"field": "role_code"})
	}
	return value, nil
}

// ChangeRealtimeCharacterRole applies a catalog-validated role transition in
// one sealed realtime frame. The browser cannot submit attributes, experience,
// role categories, or a replacement source role.
func (s *CityEconomyService) ChangeRealtimeCharacterRole(
	ctx context.Context,
	input CityRealtimeCharacterRoleChangeInput,
) (*CityRealtimeCharacterMutationResult, error) {
	roleCode, err := normalizeCityRealtimeCharacterRoleCode(input.RoleCode)
	if err != nil {
		return nil, err
	}
	idempotencyKey, err := normalizeCityRealtimeCharacterIdempotencyKey(input.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	if input.UserID <= 0 || input.WorldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	requestHash, err := cityRealtimeCharacterRequestHash(cityRealtimeCharacterRoleAction, map[string]any{
		"world_id": input.WorldID, "user_id": input.UserID, "role_code": roleCode,
	})
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin realtime character role transaction: %w", err)
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
	if receipt, found, receiptErr := loadCityRealtimeCharacterActionReceipt(
		ctx, tx, input.WorldID, input.UserID, idempotencyKey,
	); receiptErr != nil {
		return nil, receiptErr
	} else if found {
		return completeCityRealtimeCharacterReceipt(tx, receipt, cityRealtimeCharacterRoleAction, requestHash)
	}
	agentState, err := loadCityRealtimeAgentHashState(ctx, tx, input.WorldID)
	if err != nil {
		return nil, err
	}
	runtime, err := loadCityRealtimeCharacterLifeRuntime(ctx, tx, input.WorldID)
	if err != nil {
		return nil, err
	}
	if agentState == nil || agentState.Binding == nil || !cityRealtimeCharacterProgressionRuntimeEnabled(runtime) {
		return nil, ErrCityRealtimeCharacterRuntimeUnavailable
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
	if !found || !cityRealtimeCharacterProfileMatchesRuntime(profile, runtime) {
		return nil, ErrCityRealtimeCharacterRuntimeUnavailable
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
	next, previousAssignment, nextAssignment, progressionEvent, roleChange, err := cityRealtimeCharacterApplyRoleChange(
		profile, runtime, roleCode, frameSequence,
	)
	if err != nil {
		return nil, err
	}
	if previousAssignment == nil {
		if err = insertCityRealtimeCharacterRoleAssignment(ctx, tx, input.WorldID, record.identity.ActorCode, *nextAssignment); err != nil {
			return nil, err
		}
	} else if err = updateCityRealtimeCharacterRoleAssignment(
		ctx, tx, input.WorldID, record.identity.ActorCode, *previousAssignment, *nextAssignment,
	); err != nil {
		return nil, err
	}
	if err = insertCityRealtimeCharacterProgressionEvent(ctx, tx, input.WorldID, record.identity.ActorCode, progressionEvent); err != nil {
		return nil, err
	}
	if err = updateCityRealtimeCharacterProfile(ctx, tx, input.WorldID, profile, next); err != nil {
		return nil, err
	}
	life, lifeErr := cityRealtimeCharacterLifeProjection(next, runtime)
	if lifeErr != nil {
		return nil, lifeErr
	}
	result := &CityRealtimeCharacterMutationResult{
		Character: record.projection(), Life: cityRealtimeCharacterLifePointer(life), RoleChange: &roleChange,
	}
	if result.Frame, err = sealCityRealtimeCharacterFrame(ctx, tx, input.WorldID, world, state, frameSequence, cursor,
		cityRealtimeCharacterRoleAction, map[string]any{
			"character_created": 0, "character_moved": 0, "character_role_changed": 1,
			"character_progression_event": 1,
		}); err != nil {
		return nil, err
	}
	if err = canonicalizeCityRealtimeCharacterMutationResult(result); err != nil {
		return nil, err
	}
	if err = storeCityRealtimeCharacterActionReceipt(
		ctx, tx, input.WorldID, input.UserID, idempotencyKey, record.identity.ActorCode,
		cityRealtimeCharacterRoleAction, requestHash, frameSequence, *result,
	); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit realtime character role: %w", err)
	}
	return result, nil
}
