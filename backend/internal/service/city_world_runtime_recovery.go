package service

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"sort"
)

type worldRuntimeRecoveryIdentity struct {
	tick     int64
	sequence int64
}

type worldRuntimeRecoveryIDs struct {
	facts   map[worldRuntimeRecoveryIdentity]int64
	effects map[worldRuntimeRecoveryIdentity]int64
	cases   map[worldRuntimeRecoveryIdentity]int64
}

func replayWorldRuntimeFacts(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, tick int64,
	state *cityHashState,
) error {
	if state == nil || state.WorldRuntime == nil || !cityEngineSupportsWorldRuntime(state.SimulationVersion) {
		return fmt.Errorf("world runtime replay state is unavailable")
	}
	facts, err := loadWorldRuntimeFactsForTick(ctx, queryer, worldID, tick)
	if err != nil {
		return err
	}
	effects, err := loadWorldEffectOperations(ctx, queryer, worldID, tick)
	if err != nil {
		return err
	}
	cases, err := loadWorldRuleCasesForTick(ctx, queryer, worldID, tick)
	if err != nil {
		return err
	}
	runtime := state.WorldRuntime
	for _, fact := range facts {
		if fact.DefinitionCode != nil {
			definition := findWorldRuntimeDefinition(runtime.Definitions, *fact.DefinitionKind, *fact.DefinitionCode)
			if definition == nil || definition.Version != *fact.DefinitionVersion || definition.Hash != *fact.DefinitionHash {
				return fmt.Errorf("world runtime fact %d/%d definition proof mismatch", fact.Tick, fact.Sequence)
			}
		}
		if fact.FactType == WorldRuntimeFactActorCreated {
			var payload struct {
				SchemaVersion int        `json:"schema_version"`
				Actor         WorldActor `json:"actor"`
				ArchetypeCode string     `json:"archetype_code"`
			}
			if err = json.Unmarshal(fact.Payload, &payload); err != nil || payload.SchemaVersion != 1 {
				return fmt.Errorf("decode replay actor creation fact: %w", err)
			}
			if findWorldRuntimeActor(runtime.Actors, payload.Actor.Code) >= 0 {
				return fmt.Errorf("replay actor code %s already exists", payload.Actor.Code)
			}
			runtime.Actors = append(runtime.Actors, payload.Actor)
		}
		runtime.Facts = append(runtime.Facts, fact)
	}
	for _, effect := range effects {
		if err = replayWorldRuntimeEffect(runtime, effect); err != nil {
			return fmt.Errorf("replay world effect %d/%d: %w", effect.Tick, effect.Sequence, err)
		}
		runtime.Effects = append(runtime.Effects, effect)
	}
	for _, fact := range facts {
		if fact.ActorCode == nil || (fact.FactType != WorldRuntimeFactActivityPerformed &&
			fact.FactType != WorldRuntimeFactRoleTransitioned && fact.FactType != WorldRuntimeFactStatusExpired &&
			fact.FactType != WorldRuntimeFactLocationMoved && fact.FactType != WorldRuntimeFactControlGranted &&
			fact.FactType != WorldRuntimeFactControlRevoked && fact.FactType != WorldRuntimeFactPortalStateChanged &&
			!isWorldNavigationIntentFact(fact.FactType)) {
			continue
		}
		index := findWorldRuntimeActor(runtime.Actors, *fact.ActorCode)
		if index < 0 {
			return fmt.Errorf("replay world fact actor %s does not exist", *fact.ActorCode)
		}
		runtime.Actors[index].UpdatedTick = tick
		runtime.Actors[index].Version++
	}
	if runtime.Locations != nil {
		sort.SliceStable(*runtime.Locations, func(left, right int) bool {
			return (*runtime.Locations)[left].ActorCode < (*runtime.Locations)[right].ActorCode
		})
	}
	if runtime.ControlGrants != nil {
		sort.SliceStable(*runtime.ControlGrants, func(left, right int) bool {
			leftGrant, rightGrant := (*runtime.ControlGrants)[left], (*runtime.ControlGrants)[right]
			if leftGrant.ActorCode != rightGrant.ActorCode {
				return leftGrant.ActorCode < rightGrant.ActorCode
			}
			if leftGrant.Capability != rightGrant.Capability {
				return leftGrant.Capability < rightGrant.Capability
			}
			if leftGrant.GrantedTick != rightGrant.GrantedTick {
				return leftGrant.GrantedTick < rightGrant.GrantedTick
			}
			return leftGrant.Code < rightGrant.Code
		})
	}
	if runtime.PortalStates != nil {
		sort.SliceStable(*runtime.PortalStates, func(left, right int) bool {
			leftState, rightState := (*runtime.PortalStates)[left], (*runtime.PortalStates)[right]
			if leftState.BuildingCode != rightState.BuildingCode {
				return leftState.BuildingCode < rightState.BuildingCode
			}
			return leftState.PortalCode < rightState.PortalCode
		})
	}
	if runtime.NavigationIntents != nil {
		sortWorldNavigationIntents(*runtime.NavigationIntents)
	}
	runtime.RuleCases = append(runtime.RuleCases, cases...)
	runtime.Profile.ActorCount = int64(len(runtime.Actors))
	runtime.Profile.FactCount = int64(len(runtime.Facts))
	runtime.Profile.EffectCount = int64(len(runtime.Effects))
	runtime.Profile.CaseCount = int64(len(runtime.RuleCases))
	runtime.Profile.Revision = runtime.Profile.FactCount + 1
	return nil
}

func replayWorldRuntimeEffect(runtime *worldRuntimeHashState, effect WorldEffectOperation) error {
	portalEffect := effect.EffectType == WorldRuntimeEffectPortalStateSet ||
		effect.EffectType == WorldRuntimeEffectPortalAccessSet
	if runtime == nil || (!portalEffect && effect.TargetActorCode == nil) || effect.TargetKey == nil ||
		effect.BeforeUnits == nil || effect.DeltaUnits == nil || effect.AfterUnits == nil ||
		*effect.BeforeUnits+*effect.DeltaUnits != *effect.AfterUnits {
		return fmt.Errorf("effect envelope is incomplete")
	}
	if effect.TargetActorCode != nil && findWorldRuntimeActor(runtime.Actors, *effect.TargetActorCode) < 0 {
		return fmt.Errorf("effect target actor does not exist")
	}
	var payload struct {
		SchemaVersion          int                         `json:"schema_version"`
		AttributeAfter         *WorldActorAttribute        `json:"attribute_after,omitempty"`
		RoleAfter              *WorldActorRole             `json:"role_after,omitempty"`
		StatusAfter            *WorldActorStatus           `json:"status_after,omitempty"`
		LocationBefore         *WorldActorLocation         `json:"location_before,omitempty"`
		LocationAfter          *WorldActorLocation         `json:"location_after,omitempty"`
		ControlGrantAfter      *WorldActorControlGrant     `json:"control_grant_after,omitempty"`
		PortalBefore           *WorldPortalState           `json:"portal_before,omitempty"`
		PortalAfter            *WorldPortalState           `json:"portal_after,omitempty"`
		NavigationIntentBefore *WorldActorNavigationIntent `json:"navigation_intent_before,omitempty"`
		NavigationIntentAfter  *WorldActorNavigationIntent `json:"navigation_intent_after,omitempty"`
	}
	if err := json.Unmarshal(effect.Payload, &payload); err != nil || payload.SchemaVersion != 1 {
		return fmt.Errorf("invalid effect payload")
	}
	switch effect.EffectType {
	case WorldRuntimeEffectAttributeSet, WorldRuntimeEffectAttributeAdd, WorldRuntimeEffectExperienceAdd:
		if payload.AttributeAfter == nil || payload.AttributeAfter.ActorCode != *effect.TargetActorCode ||
			payload.AttributeAfter.AttributeCode != *effect.TargetKey {
			return fmt.Errorf("attribute effect payload mismatch")
		}
		index := findWorldRuntimeAttribute(runtime.Attributes, *effect.TargetActorCode, *effect.TargetKey)
		before := int64(0)
		if index >= 0 {
			if effect.EffectType == WorldRuntimeEffectExperienceAdd {
				before = runtime.Attributes[index].ExperienceUnits
			} else {
				before = runtime.Attributes[index].ValueUnits
			}
		} else if effect.EffectType == WorldRuntimeEffectAttributeAdd {
			definition := findWorldRuntimeDefinition(
				runtime.Definitions, WorldRuntimeDefinitionAttribute, *effect.TargetKey,
			)
			if definition == nil {
				return fmt.Errorf("attribute effect definition is unavailable")
			}
			attributeDefinition, err := decodeWorldRuntimeDefinition[worldRuntimeAttributeDefinition](definition)
			if err != nil {
				return fmt.Errorf("decode attribute effect definition: %w", err)
			}
			before = attributeDefinition.DefaultUnits
		}
		if before != *effect.BeforeUnits {
			return fmt.Errorf("attribute effect before value mismatch")
		}
		payloadAfter := payload.AttributeAfter.ValueUnits
		if effect.EffectType == WorldRuntimeEffectExperienceAdd {
			payloadAfter = payload.AttributeAfter.ExperienceUnits
		}
		if payloadAfter != *effect.AfterUnits {
			return fmt.Errorf("attribute effect after value mismatch")
		}
		if index < 0 {
			runtime.Attributes = append(runtime.Attributes, *payload.AttributeAfter)
		} else {
			runtime.Attributes[index] = *payload.AttributeAfter
		}
	case WorldRuntimeEffectRoleGrant, WorldRuntimeEffectRoleRevoke:
		if payload.RoleAfter == nil || payload.RoleAfter.ActorCode != *effect.TargetActorCode ||
			payload.RoleAfter.RoleCode != *effect.TargetKey {
			return fmt.Errorf("role effect payload mismatch")
		}
		index := findActiveWorldRuntimeRole(runtime.Roles, *effect.TargetActorCode, *effect.TargetKey)
		if effect.EffectType == WorldRuntimeEffectRoleGrant {
			if *effect.BeforeUnits != 0 || *effect.AfterUnits != 1 || index >= 0 || payload.RoleAfter.Status != "active" {
				return fmt.Errorf("role grant before state mismatch")
			}
			runtime.Roles = append(runtime.Roles, *payload.RoleAfter)
		} else {
			if *effect.BeforeUnits != 1 || *effect.AfterUnits != 0 || index < 0 || payload.RoleAfter.Status != "revoked" {
				return fmt.Errorf("role revoke before state mismatch")
			}
			runtime.Roles[index] = *payload.RoleAfter
		}
	case WorldRuntimeEffectStatusGrant, WorldRuntimeEffectStatusRevoke, WorldRuntimeEffectStatusExpire:
		if payload.StatusAfter == nil || payload.StatusAfter.ActorCode != *effect.TargetActorCode ||
			payload.StatusAfter.StatusCode != *effect.TargetKey {
			return fmt.Errorf("status effect payload mismatch")
		}
		index := findWorldRuntimeStatus(runtime.Statuses, payload.StatusAfter.InstanceCode)
		before := int64(0)
		if index >= 0 && runtime.Statuses[index].Lifecycle == "active" {
			before = int64(runtime.Statuses[index].Stacks)
		}
		if before != *effect.BeforeUnits {
			return fmt.Errorf("status effect before state mismatch")
		}
		if effect.EffectType == WorldRuntimeEffectStatusGrant {
			if payload.StatusAfter.Lifecycle != "active" || int64(payload.StatusAfter.Stacks) != *effect.AfterUnits {
				return fmt.Errorf("status grant after state mismatch")
			}
		} else if *effect.AfterUnits != 0 || payload.StatusAfter.Lifecycle == "active" {
			return fmt.Errorf("status end after state mismatch")
		}
		if index < 0 {
			runtime.Statuses = append(runtime.Statuses, *payload.StatusAfter)
		} else {
			runtime.Statuses[index] = *payload.StatusAfter
		}
	case WorldRuntimeEffectLocationSet:
		if runtime.Locations == nil || effect.ExecutorVersion != worldRuntimeSpatialControlVersion ||
			payload.LocationAfter == nil || payload.LocationAfter.ActorCode != *effect.TargetActorCode ||
			*effect.TargetKey != "position" || payload.LocationAfter.Version != *effect.AfterUnits {
			return fmt.Errorf("location effect payload mismatch")
		}
		index := findWorldRuntimeLocation(*runtime.Locations, *effect.TargetActorCode)
		if index < 0 {
			if payload.LocationBefore != nil || *effect.BeforeUnits != 0 || payload.LocationAfter.Version != 1 {
				return fmt.Errorf("location effect before state mismatch")
			}
			*runtime.Locations = append(*runtime.Locations, *payload.LocationAfter)
		} else {
			before := (*runtime.Locations)[index]
			if payload.LocationBefore == nil || !reflect.DeepEqual(before, *payload.LocationBefore) ||
				before.Version != *effect.BeforeUnits || payload.LocationAfter.Version != before.Version+1 {
				return fmt.Errorf("location effect before state mismatch")
			}
			(*runtime.Locations)[index] = *payload.LocationAfter
		}
	case WorldRuntimeEffectControlGrant, WorldRuntimeEffectControlRevoke:
		if runtime.ControlGrants == nil || effect.ExecutorVersion != worldRuntimeSpatialControlVersion ||
			payload.ControlGrantAfter == nil || payload.ControlGrantAfter.ActorCode != *effect.TargetActorCode ||
			payload.ControlGrantAfter.Capability != *effect.TargetKey {
			return fmt.Errorf("control effect payload mismatch")
		}
		grant := *payload.ControlGrantAfter
		index := findWorldRuntimeControlGrant(*runtime.ControlGrants, grant.Code)
		if effect.EffectType == WorldRuntimeEffectControlGrant {
			if index >= 0 || grant.Status != "active" || grant.Version != 1 ||
				*effect.BeforeUnits != 0 || *effect.AfterUnits != 1 {
				return fmt.Errorf("control grant before state mismatch")
			}
			*runtime.ControlGrants = append(*runtime.ControlGrants, grant)
		} else {
			if index < 0 || (*runtime.ControlGrants)[index].Status != "active" ||
				grant.Status != "revoked" || grant.Version != (*runtime.ControlGrants)[index].Version+1 ||
				*effect.BeforeUnits != 1 || *effect.AfterUnits != 0 {
				return fmt.Errorf("control revoke before state mismatch")
			}
			(*runtime.ControlGrants)[index] = grant
		}
	case WorldRuntimeEffectPortalStateSet, WorldRuntimeEffectPortalAccessSet:
		if runtime.PortalStates == nil || effect.ExecutorVersion != worldRuntimePortalAccessVersion ||
			payload.PortalBefore == nil || payload.PortalAfter == nil ||
			worldPortalTargetKey(payload.PortalAfter.BuildingCode, payload.PortalAfter.PortalCode) != *effect.TargetKey ||
			payload.PortalAfter.BuildingCode != payload.PortalBefore.BuildingCode ||
			payload.PortalAfter.PortalCode != payload.PortalBefore.PortalCode ||
			payload.PortalAfter.PortalType != payload.PortalBefore.PortalType ||
			payload.PortalAfter.ChangedTick != effect.Tick ||
			payload.PortalAfter.SourceFact == nil || *payload.PortalAfter.SourceFact != effect.SourceFact {
			return fmt.Errorf("portal effect payload mismatch")
		}
		beforeRequirement, _, beforeHash, beforeErr := canonicalWorldPortalAccessRequirement(
			payload.PortalBefore.AccessRequirement,
		)
		afterRequirement, _, afterHash, afterErr := canonicalWorldPortalAccessRequirement(
			payload.PortalAfter.AccessRequirement,
		)
		if beforeErr != nil || afterErr != nil ||
			beforeHash != payload.PortalBefore.AccessPolicyHash ||
			afterHash != payload.PortalAfter.AccessPolicyHash {
			return fmt.Errorf("portal effect access policy mismatch")
		}
		payload.PortalBefore.AccessRequirement = beforeRequirement
		payload.PortalAfter.AccessRequirement = afterRequirement
		index := findWorldRuntimePortalState(
			*runtime.PortalStates, payload.PortalAfter.BuildingCode, payload.PortalAfter.PortalCode,
		)
		if index < 0 {
			return fmt.Errorf("portal effect before state mismatch: portal is absent")
		}
		if field := worldPortalReplayStateMismatch((*runtime.PortalStates)[index], *payload.PortalBefore); field != "" {
			return fmt.Errorf("portal effect before state mismatch: %s", field)
		}
		if payload.PortalBefore.Version != *effect.BeforeUnits ||
			payload.PortalAfter.Version != *effect.AfterUnits ||
			payload.PortalAfter.Version != payload.PortalBefore.Version+1 {
			return fmt.Errorf("portal effect before state mismatch: version")
		}
		if !worldRuntimeJSONEqual(payload.PortalAfter.Metadata, json.RawMessage(`{"schema_version":1}`)) {
			return fmt.Errorf("portal effect after state mismatch: metadata")
		}
		if effect.EffectType == WorldRuntimeEffectPortalStateSet {
			validTransition := false
			for _, action := range []string{
				WorldPortalActionOpen, WorldPortalActionClose,
				WorldPortalActionLock, WorldPortalActionUnlock,
			} {
				if next, valid := nextWorldPortalState(payload.PortalBefore.StateCode, action); valid &&
					next == payload.PortalAfter.StateCode {
					validTransition = true
					break
				}
			}
			if effect.TargetActorCode == nil || !validTransition ||
				payload.PortalAfter.AccessPolicyHash != payload.PortalBefore.AccessPolicyHash ||
				!reflect.DeepEqual(payload.PortalAfter.AccessRequirement, payload.PortalBefore.AccessRequirement) {
				return fmt.Errorf("portal state effect after state mismatch")
			}
		} else {
			if effect.TargetActorCode != nil ||
				payload.PortalAfter.StateCode != payload.PortalBefore.StateCode ||
				payload.PortalAfter.AccessPolicyHash == payload.PortalBefore.AccessPolicyHash ||
				reflect.DeepEqual(payload.PortalAfter.AccessRequirement, payload.PortalBefore.AccessRequirement) {
				return fmt.Errorf("portal access effect after state mismatch")
			}
		}
		(*runtime.PortalStates)[index] = *payload.PortalAfter
	case WorldRuntimeEffectNavigationIntentSet:
		if runtime.NavigationIntents == nil ||
			effect.ExecutorVersion != worldRuntimeNavigationIntentVersion ||
			payload.NavigationIntentAfter == nil || effect.TargetActorCode == nil ||
			payload.NavigationIntentAfter.ActorCode != *effect.TargetActorCode ||
			*effect.TargetKey != "navigation.intent" ||
			payload.NavigationIntentAfter.SourceFact != effect.SourceFact ||
			payload.NavigationIntentAfter.UpdatedTick != effect.Tick ||
			payload.NavigationIntentAfter.Version != *effect.AfterUnits ||
			payload.NavigationIntentAfter.BudgetUnits < 0 ||
			payload.NavigationIntentAfter.BudgetUnits > payload.NavigationIntentAfter.BudgetCapUnits {
			return fmt.Errorf("navigation intent effect payload mismatch")
		}
		index := findWorldRuntimeNavigationIntent(
			*runtime.NavigationIntents, *effect.TargetActorCode,
		)
		if index < 0 {
			if payload.NavigationIntentBefore != nil || *effect.BeforeUnits != 0 ||
				payload.NavigationIntentAfter.Version != 1 {
				return fmt.Errorf("navigation intent effect before state mismatch")
			}
			*runtime.NavigationIntents = append(
				*runtime.NavigationIntents, *payload.NavigationIntentAfter,
			)
		} else {
			before := (*runtime.NavigationIntents)[index]
			if payload.NavigationIntentBefore == nil ||
				worldNavigationIntentReplayMismatch(before, *payload.NavigationIntentBefore) != "" ||
				before.Version != *effect.BeforeUnits ||
				payload.NavigationIntentAfter.Version != before.Version+1 {
				return fmt.Errorf("navigation intent effect before state mismatch")
			}
			(*runtime.NavigationIntents)[index] = *payload.NavigationIntentAfter
		}
	default:
		return fmt.Errorf("unknown effect type %s", effect.EffectType)
	}
	return nil
}

func isWorldNavigationIntentFact(factType string) bool {
	switch factType {
	case WorldRuntimeFactNavigationIntentCreated, WorldRuntimeFactNavigationIntentReplaced,
		WorldRuntimeFactNavigationIntentCancelled, WorldRuntimeFactNavigationIntentWaited,
		WorldRuntimeFactNavigationIntentBlocked, WorldRuntimeFactNavigationIntentProgressed,
		WorldRuntimeFactNavigationIntentArrived, WorldRuntimeFactNavigationIntentFailed:
		return true
	default:
		return false
	}
}

func worldNavigationIntentReplayMismatch(
	actual, expected WorldActorNavigationIntent,
) string {
	actualMetadata, expectedMetadata := actual.Metadata, expected.Metadata
	actual.Metadata, expected.Metadata = nil, nil
	if !reflect.DeepEqual(actual, expected) {
		return "state"
	}
	if !worldRuntimeJSONEqual(actualMetadata, expectedMetadata) {
		return "metadata"
	}
	return ""
}

func worldPortalReplayStateMismatch(actual, expected WorldPortalState) string {
	if actual.BuildingCode != expected.BuildingCode {
		return "building_code"
	}
	if actual.PortalCode != expected.PortalCode {
		return "portal_code"
	}
	if actual.PortalType != expected.PortalType {
		return "portal_type"
	}
	if actual.StateCode != expected.StateCode {
		return "state_code"
	}
	actualRequirement, _, actualHash, actualErr := canonicalWorldPortalAccessRequirement(actual.AccessRequirement)
	expectedRequirement, _, expectedHash, expectedErr := canonicalWorldPortalAccessRequirement(expected.AccessRequirement)
	if actualErr != nil || expectedErr != nil {
		return "access_requirement_invalid"
	}
	if actualHash != actual.AccessPolicyHash {
		return "actual_access_policy_hash"
	}
	if expectedHash != expected.AccessPolicyHash {
		return "expected_access_policy_hash"
	}
	if !reflect.DeepEqual(actualRequirement, expectedRequirement) || actual.AccessPolicyHash != expected.AccessPolicyHash {
		return "access_requirement"
	}
	if actual.ChangedTick != expected.ChangedTick {
		return "changed_tick"
	}
	if !reflect.DeepEqual(actual.SourceFact, expected.SourceFact) {
		return "source_fact"
	}
	if actual.Version != expected.Version {
		return "version"
	}
	if !worldRuntimeJSONEqual(actual.Metadata, expected.Metadata) {
		return "metadata"
	}
	return ""
}

func worldRuntimeJSONEqual(left, right json.RawMessage) bool {
	leftCanonical, leftErr := canonicalWorldRuntimeJSON(left)
	rightCanonical, rightErr := canonicalWorldRuntimeJSON(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftCanonical, rightCanonical)
}

func canonicalWorldRuntimeJSON(raw json.RawMessage) ([]byte, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("empty JSON value")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("multiple JSON values")
		}
		return nil, err
	}
	return json.Marshal(value)
}

func findWorldRuntimeDefinition(items []WorldRuntimeDefinition, kind, code string) *WorldRuntimeDefinition {
	for index := range items {
		if items[index].Kind == kind && items[index].Code == code {
			return &items[index]
		}
	}
	return nil
}

func findWorldRuntimeActor(items []WorldActor, code string) int {
	for index := range items {
		if items[index].Code == code {
			return index
		}
	}
	return -1
}

func findWorldRuntimeAttribute(items []WorldActorAttribute, actorCode, attributeCode string) int {
	for index := range items {
		if items[index].ActorCode == actorCode && items[index].AttributeCode == attributeCode {
			return index
		}
	}
	return -1
}

func findActiveWorldRuntimeRole(items []WorldActorRole, actorCode, roleCode string) int {
	for index := range items {
		if items[index].ActorCode == actorCode && items[index].RoleCode == roleCode && items[index].Status == "active" {
			return index
		}
	}
	return -1
}

func findWorldRuntimeStatus(items []WorldActorStatus, instanceCode string) int {
	for index := range items {
		if items[index].InstanceCode == instanceCode {
			return index
		}
	}
	return -1
}

func findWorldRuntimeLocation(items []WorldActorLocation, actorCode string) int {
	for index := range items {
		if items[index].ActorCode == actorCode {
			return index
		}
	}
	return -1
}

func findWorldRuntimeControlGrant(items []WorldActorControlGrant, code string) int {
	for index := range items {
		if items[index].Code == code {
			return index
		}
	}
	return -1
}

func findWorldRuntimePortalState(items []WorldPortalState, buildingCode, portalCode string) int {
	for index := range items {
		if items[index].BuildingCode == buildingCode && items[index].PortalCode == portalCode {
			return index
		}
	}
	return -1
}

func findWorldRuntimeNavigationIntent(items []WorldActorNavigationIntent, actorCode string) int {
	for index := range items {
		if items[index].ActorCode == actorCode {
			return index
		}
	}
	return -1
}

func loadWorldRuntimeRecoveryIDs(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (worldRuntimeRecoveryIDs, error) {
	ids := worldRuntimeRecoveryIDs{
		facts:   make(map[worldRuntimeRecoveryIdentity]int64),
		effects: make(map[worldRuntimeRecoveryIdentity]int64),
		cases:   make(map[worldRuntimeRecoveryIdentity]int64),
	}
	load := func(query string, target map[worldRuntimeRecoveryIdentity]int64) error {
		rows, err := queryer.QueryContext(ctx, query, worldID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id, tick, sequence int64
			if err = rows.Scan(&id, &tick, &sequence); err != nil {
				_ = rows.Close()
				return err
			}
			target[worldRuntimeRecoveryIdentity{tick: tick, sequence: sequence}] = id
		}
		return closeCityRows(rows, "iterate world runtime recovery identities")
	}
	if err := load(`SELECT id, tick, sequence FROM world_runtime_facts WHERE world_id = $1 ORDER BY tick, sequence`, ids.facts); err != nil {
		return ids, fmt.Errorf("load world runtime fact identities: %w", err)
	}
	if err := load(`SELECT id, tick, sequence FROM world_effect_operations WHERE world_id = $1 ORDER BY tick, sequence`, ids.effects); err != nil {
		return ids, fmt.Errorf("load world runtime effect identities: %w", err)
	}
	if err := load(`SELECT id, tick, sequence FROM world_rule_cases WHERE world_id = $1 ORDER BY tick, sequence`, ids.cases); err != nil {
		return ids, fmt.Errorf("load world runtime case identities: %w", err)
	}
	return ids, nil
}

func clearWorldRuntimeProjection(ctx context.Context, tx *sql.Tx, worldID int64) (int, error) {
	count := 0
	for _, statement := range []string{
		`DELETE FROM world_rule_cases WHERE world_id = $1`,
		`DELETE FROM world_navigation_reservations WHERE world_id = $1`,
		`DELETE FROM world_actor_navigation_intents WHERE world_id = $1`,
		`DELETE FROM world_effect_operations WHERE world_id = $1`,
		`DELETE FROM world_portal_states WHERE world_id = $1`,
		`DELETE FROM world_actor_control_grants WHERE world_id = $1`,
		`DELETE FROM world_actor_locations WHERE world_id = $1`,
		`DELETE FROM world_actor_statuses WHERE world_id = $1`,
		`DELETE FROM world_actor_roles WHERE world_id = $1`,
		`DELETE FROM world_actor_attributes WHERE world_id = $1`,
		`DELETE FROM world_runtime_facts WHERE world_id = $1`,
		`DELETE FROM world_actors WHERE world_id = $1`,
		`DELETE FROM world_runtime_definitions WHERE world_id = $1`,
		`DELETE FROM world_navigation_profiles WHERE world_id = $1`,
		`DELETE FROM world_runtime_profiles WHERE world_id = $1`,
	} {
		result, err := tx.ExecContext(ctx, statement, worldID)
		if err != nil {
			return count, fmt.Errorf("clear world runtime projection: %w", err)
		}
		rows, rowsErr := result.RowsAffected()
		if rowsErr != nil {
			return count, rowsErr
		}
		count += int(rows)
	}
	return count, nil
}

func restoreWorldRuntimeProjection(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	state *cityHashState,
	preserved worldRuntimeRecoveryIDs,
) (int, error) {
	if state == nil || state.WorldRuntime == nil || !cityEngineSupportsWorldRuntime(state.SimulationVersion) {
		return 0, fmt.Errorf("recovery world runtime state is unavailable")
	}
	runtime := state.WorldRuntime
	if runtime.Profile.RuntimeID != worldRuntimeID ||
		runtime.Profile.RuntimeVersion != expectedWorldRuntimeVersion(state.SimulationVersion) ||
		runtime.Profile.CatalogVersion != worldRuntimeCatalogVersion ||
		runtime.Profile.ActorCount != int64(len(runtime.Actors)) ||
		runtime.Profile.FactCount != int64(len(runtime.Facts)) ||
		runtime.Profile.EffectCount != int64(len(runtime.Effects)) ||
		runtime.Profile.CaseCount != int64(len(runtime.RuleCases)) ||
		runtime.Profile.Revision != int64(len(runtime.Facts))+1 {
		return 0, fmt.Errorf("recovery world runtime profile is inconsistent")
	}
	if cityEngineSupportsWorldActorSpatialControl(state.SimulationVersion) {
		if runtime.Locations == nil || runtime.ControlGrants == nil || len(*runtime.Locations) != len(runtime.Actors) {
			return 0, fmt.Errorf("recovery world actor spatial-control state is inconsistent")
		}
	} else if runtime.Locations != nil || runtime.ControlGrants != nil {
		return 0, fmt.Errorf("legacy recovery state contains world actor spatial-control data")
	}
	if cityEngineSupportsWorldPortalAccess(state.SimulationVersion) {
		if runtime.PortalStates == nil {
			return 0, fmt.Errorf("recovery world portal-access state is inconsistent")
		}
	} else if runtime.PortalStates != nil {
		return 0, fmt.Errorf("legacy recovery state contains world portal-access data")
	}
	if cityEngineSupportsWorldNavigationIntents(state.SimulationVersion) {
		if runtime.NavigationProfile == nil || runtime.NavigationIntents == nil {
			return 0, fmt.Errorf("recovery world navigation-intent state is inconsistent")
		}
		if err := validateWorldNavigationProfile(*runtime.NavigationProfile); err != nil {
			return 0, fmt.Errorf("recovery world navigation profile is invalid: %w", err)
		}
	} else if runtime.NavigationProfile != nil || runtime.NavigationIntents != nil {
		return 0, fmt.Errorf("legacy recovery state contains world navigation-intent data")
	}
	count, err := clearWorldRuntimeProjection(ctx, tx, worldID)
	if err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO world_runtime_profiles
    (world_id, runtime_id, runtime_version, catalog_version, catalog_hash,
     baseline_tick, maximum_player_actors_per_member, actor_count, fact_count,
     effect_count, case_count, revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13::jsonb)`,
		worldID, runtime.Profile.RuntimeID, runtime.Profile.RuntimeVersion,
		runtime.Profile.CatalogVersion, runtime.Profile.CatalogHash, runtime.Profile.BaselineTick,
		runtime.Profile.MaximumPlayerActorsPerMember, runtime.Profile.ActorCount,
		runtime.Profile.FactCount, runtime.Profile.EffectCount, runtime.Profile.CaseCount,
		runtime.Profile.Revision, []byte(runtime.Profile.Metadata)); err != nil {
		return count, fmt.Errorf("restore world runtime profile: %w", err)
	}
	count++
	if runtime.NavigationProfile != nil {
		profile := runtime.NavigationProfile
		if _, err = tx.ExecContext(ctx, `
INSERT INTO world_navigation_profiles
    (world_id, profile_version, baseline_tick, maximum_intents_per_tick,
     default_budget_gain_units, default_budget_cap_units, default_max_steps,
     maximum_blocked_attempts, maximum_retry_delay_ticks, fairness_aging_cap,
     revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb)`,
			worldID, profile.ProfileVersion, profile.BaselineTick,
			profile.MaximumIntentsPerTick, profile.DefaultBudgetGainUnits,
			profile.DefaultBudgetCapUnits, profile.DefaultMaxSteps,
			profile.MaximumBlockedAttempts, profile.MaximumRetryDelayTicks,
			profile.FairnessAgingCap, profile.Revision, []byte(profile.Metadata)); err != nil {
			return count, fmt.Errorf("restore world navigation profile: %w", err)
		}
		count++
	}
	for _, definition := range runtime.Definitions {
		if _, err = tx.ExecContext(ctx, `
INSERT INTO world_runtime_definitions
    (world_id, definition_kind, code, definition_version, content_hash, visibility, payload)
VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)`, worldID, definition.Kind,
			definition.Code, definition.Version, definition.Hash, definition.Visibility,
			[]byte(definition.Payload)); err != nil {
			return count, fmt.Errorf("restore world runtime definition %s/%s: %w", definition.Kind, definition.Code, err)
		}
		count++
	}
	actorIDs := make(map[string]int64, len(runtime.Actors))
	for _, actor := range runtime.Actors {
		var actorID int64
		if err = tx.QueryRowContext(ctx, `
INSERT INTO world_actors
    (world_id, code, owner_user_id, actor_type_code, name, status,
     archetype_code, archetype_version, created_tick, updated_tick, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb)
RETURNING id`, worldID, actor.Code, cityNullableInt64(actor.OwnerUserID), actor.ActorTypeCode,
			actor.Name, actor.Status, nullableStringValue(actor.ArchetypeCode),
			nullableStringValue(actor.ArchetypeVersion), actor.CreatedTick, actor.UpdatedTick,
			actor.Version, []byte(actor.Metadata)).Scan(&actorID); err != nil {
			return count, fmt.Errorf("restore world actor %s: %w", actor.Code, err)
		}
		actorIDs[actor.Code] = actorID
		count++
	}
	for _, attribute := range runtime.Attributes {
		if _, err = tx.ExecContext(ctx, `
INSERT INTO world_actor_attributes
    (world_id, actor_id, attribute_code, value_units, experience_units,
     last_changed_tick, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb)`, worldID, actorIDs[attribute.ActorCode],
			attribute.AttributeCode, attribute.ValueUnits, attribute.ExperienceUnits,
			attribute.LastChangedTick, attribute.Version, []byte(attribute.Metadata)); err != nil {
			return count, fmt.Errorf("restore world actor attribute: %w", err)
		}
		count++
	}
	for _, role := range runtime.Roles {
		if _, err = tx.ExecContext(ctx, `
INSERT INTO world_actor_roles
    (world_id, actor_id, role_code, category_code, status, granted_tick,
     revoked_tick, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb)`, worldID, actorIDs[role.ActorCode],
			role.RoleCode, role.CategoryCode, role.Status, role.GrantedTick,
			cityNullableInt64(role.RevokedTick), role.Version, []byte(role.Metadata)); err != nil {
			return count, fmt.Errorf("restore world actor role: %w", err)
		}
		count++
	}
	factIDs := make(map[worldRuntimeRecoveryIdentity]int64, len(runtime.Facts))
	for _, fact := range runtime.Facts {
		identity := worldRuntimeRecoveryIdentity{tick: fact.Tick, sequence: fact.Sequence}
		var sourceCommandID, parentFactID, actorID any
		if fact.SourceCommandSequence != nil {
			var id int64
			if err = tx.QueryRowContext(ctx, `SELECT id FROM city_commands WHERE world_id = $1 AND sequence = $2`,
				worldID, *fact.SourceCommandSequence).Scan(&id); err != nil {
				return count, fmt.Errorf("resolve recovery world runtime command: %w", err)
			}
			sourceCommandID = id
		}
		if fact.Parent != nil {
			parentFactID = factIDs[worldRuntimeRecoveryIdentity{tick: fact.Parent.Tick, sequence: fact.Parent.Sequence}]
		}
		if fact.ActorCode != nil {
			actorID = actorIDs[*fact.ActorCode]
		}
		preservedID := preserved.facts[identity]
		query := `
INSERT INTO world_runtime_facts
    (world_id, tick, sequence, source_command_id, parent_fact_id, actor_id,
     fact_type, definition_kind, definition_code, definition_version,
     definition_hash, payload, posted_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb, NOW())
RETURNING id`
		args := []any{worldID, fact.Tick, fact.Sequence, sourceCommandID, parentFactID, actorID,
			fact.FactType, nullableStringValue(fact.DefinitionKind), nullableStringValue(fact.DefinitionCode),
			nullableStringValue(fact.DefinitionVersion), nullableStringValue(fact.DefinitionHash), []byte(fact.Payload)}
		if preservedID > 0 {
			query = `
INSERT INTO world_runtime_facts
    (id, world_id, tick, sequence, source_command_id, parent_fact_id, actor_id,
     fact_type, definition_kind, definition_code, definition_version,
     definition_hash, payload, posted_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13::jsonb, NOW())
RETURNING id`
			args = append([]any{preservedID}, args...)
		}
		var id int64
		if err = tx.QueryRowContext(ctx, query, args...).Scan(&id); err != nil {
			return count, fmt.Errorf("restore world runtime fact %d/%d: %w", fact.Tick, fact.Sequence, err)
		}
		factIDs[identity] = id
		count++
	}
	if runtime.Locations != nil {
		for _, location := range *runtime.Locations {
			actorID, exists := actorIDs[location.ActorCode]
			if !exists {
				return count, fmt.Errorf("restore world actor location references unknown actor %s", location.ActorCode)
			}
			var sourceFactID any
			if location.SourceFact != nil {
				resolved, found := factIDs[worldRuntimeRecoveryIdentity{
					tick: location.SourceFact.Tick, sequence: location.SourceFact.Sequence,
				}]
				if !found {
					return count, fmt.Errorf("restore world actor location references unknown fact %d/%d",
						location.SourceFact.Tick, location.SourceFact.Sequence)
				}
				sourceFactID = resolved
			}
			if _, err = tx.ExecContext(ctx, `
INSERT INTO world_actor_locations
    (world_id, actor_id, space_kind, space_code, x, y, z, chunk_x, chunk_y,
     local_x, local_y, anchor_kind, anchor_code, jurisdiction_code, moved_tick,
     source_fact_id, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14,
        $15, $16, $17, $18::jsonb)`, worldID, actorID, location.SpaceKind,
				location.SpaceCode, location.X, location.Y, location.Z, location.ChunkX,
				location.ChunkY, location.LocalX, location.LocalY,
				nullableStringValue(location.AnchorKind), nullableStringValue(location.AnchorCode),
				location.JurisdictionCode, location.MovedTick, sourceFactID,
				location.Version, []byte(location.Metadata)); err != nil {
				return count, fmt.Errorf("restore world actor location %s: %w", location.ActorCode, err)
			}
			count++
		}
	}
	if runtime.ControlGrants != nil {
		for _, grant := range *runtime.ControlGrants {
			actorID, exists := actorIDs[grant.ActorCode]
			if !exists {
				return count, fmt.Errorf("restore world actor control grant references unknown actor %s", grant.ActorCode)
			}
			grantFactID, factErr := resolveOptionalWorldRuntimeRecoveryFactID(factIDs, grant.GrantSourceFact)
			if factErr != nil {
				return count, fmt.Errorf("restore world actor control grant %s: %w", grant.Code, factErr)
			}
			revokeFactID, factErr := resolveOptionalWorldRuntimeRecoveryFactID(factIDs, grant.RevokeSourceFact)
			if factErr != nil {
				return count, fmt.Errorf("restore world actor control grant %s: %w", grant.Code, factErr)
			}
			if _, err = tx.ExecContext(ctx, `
INSERT INTO world_actor_control_grants
    (world_id, code, actor_id, user_id, capability, status, granted_by_user_id,
     granted_tick, revoked_tick, grant_source_fact_id, revoke_source_fact_id,
     version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13::jsonb)`,
				worldID, grant.Code, actorID, grant.UserID, grant.Capability, grant.Status,
				grant.GrantedByUserID, grant.GrantedTick, cityNullableInt64(grant.RevokedTick),
				grantFactID, revokeFactID, grant.Version, []byte(grant.Metadata)); err != nil {
				return count, fmt.Errorf("restore world actor control grant %s: %w", grant.Code, err)
			}
			count++
		}
	}
	if runtime.PortalStates != nil {
		for _, portalState := range *runtime.PortalStates {
			var portalID int64
			if err = tx.QueryRowContext(ctx, `
SELECT portal.id
FROM city_building_portals portal
JOIN city_buildings building
  ON building.id = portal.building_id AND building.world_id = portal.world_id
WHERE portal.world_id = $1 AND building.code = $2 AND portal.code = $3`,
				worldID, portalState.BuildingCode, portalState.PortalCode).Scan(&portalID); err != nil {
				return count, fmt.Errorf("resolve recovery world portal %s/%s: %w",
					portalState.BuildingCode, portalState.PortalCode, err)
			}
			requirement, requirementRaw, policyHash, requirementErr :=
				canonicalWorldPortalAccessRequirement(portalState.AccessRequirement)
			if requirementErr != nil || policyHash != portalState.AccessPolicyHash {
				return count, fmt.Errorf("restore world portal access policy %s/%s is invalid",
					portalState.BuildingCode, portalState.PortalCode)
			}
			portalState.AccessRequirement = requirement
			sourceFactID, factErr := resolveOptionalWorldRuntimeRecoveryFactID(factIDs, portalState.SourceFact)
			if factErr != nil {
				return count, fmt.Errorf("restore world portal state %s/%s: %w",
					portalState.BuildingCode, portalState.PortalCode, factErr)
			}
			if _, err = tx.ExecContext(ctx, `
INSERT INTO world_portal_states
    (world_id, portal_id, state_code, access_requirement, access_policy_hash,
     changed_tick, source_fact_id, version, metadata)
VALUES ($1, $2, $3, $4::jsonb, $5, $6, $7, $8, $9::jsonb)`,
				worldID, portalID, portalState.StateCode, []byte(requirementRaw),
				portalState.AccessPolicyHash, portalState.ChangedTick, sourceFactID,
				portalState.Version, []byte(portalState.Metadata)); err != nil {
				return count, fmt.Errorf("restore world portal state %s/%s: %w",
					portalState.BuildingCode, portalState.PortalCode, err)
			}
			count++
		}
	}
	if runtime.NavigationIntents != nil {
		for _, intent := range *runtime.NavigationIntents {
			actorID, exists := actorIDs[intent.ActorCode]
			if !exists {
				return count, fmt.Errorf("restore world navigation intent references unknown actor %s", intent.ActorCode)
			}
			sourceFactID, found := factIDs[worldRuntimeRecoveryIdentity{
				tick: intent.SourceFact.Tick, sequence: intent.SourceFact.Sequence,
			}]
			if !found {
				return count, fmt.Errorf("restore world navigation intent references unknown fact %d/%d",
					intent.SourceFact.Tick, intent.SourceFact.Sequence)
			}
			if _, err = tx.ExecContext(ctx, `
INSERT INTO world_actor_navigation_intents
    (world_id, actor_id, intent_code, destination_x, destination_y, destination_z,
     status, on_blocked, priority, max_steps, budget_units, budget_gain_units,
     budget_cap_units, blocked_attempts, last_reason, next_attempt_tick,
     created_tick, updated_tick, source_fact_id, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
        $14, $15, $16, $17, $18, $19, $20, $21::jsonb)`,
				worldID, actorID, intent.IntentCode, intent.Destination.X,
				intent.Destination.Y, intent.Destination.Z, intent.Status,
				intent.OnBlocked, intent.Priority, intent.MaxSteps,
				intent.BudgetUnits, intent.BudgetGainUnits, intent.BudgetCapUnits,
				intent.BlockedAttempts, nullableStringValue(intent.LastReason),
				intent.NextAttemptTick, intent.CreatedTick, intent.UpdatedTick,
				sourceFactID, intent.Version, []byte(intent.Metadata)); err != nil {
				return count, fmt.Errorf("restore world navigation intent %s: %w", intent.ActorCode, err)
			}
			count++
		}
		for _, fact := range runtime.Facts {
			if fact.FactType != WorldRuntimeFactNavigationIntentProgressed || fact.ActorCode == nil {
				continue
			}
			var payload struct {
				SchemaVersion int                        `json:"schema_version"`
				Reservation   WorldNavigationReservation `json:"reservation"`
			}
			if err = json.Unmarshal(fact.Payload, &payload); err != nil ||
				payload.SchemaVersion != 1 || payload.Reservation.ActorCode != *fact.ActorCode ||
				payload.Reservation.SourceFact != fact.Ref() {
				return count, fmt.Errorf("restore world navigation reservation fact %d/%d is invalid",
					fact.Tick, fact.Sequence)
			}
			actorID, exists := actorIDs[*fact.ActorCode]
			if !exists {
				return count, fmt.Errorf("restore world navigation reservation references unknown actor %s", *fact.ActorCode)
			}
			sourceFactID := factIDs[worldRuntimeRecoveryIdentity{tick: fact.Tick, sequence: fact.Sequence}]
			reservation := payload.Reservation
			if _, err = tx.ExecContext(ctx, `
INSERT INTO world_navigation_reservations
    (world_id, tick, sequence, actor_id, intent_code,
     from_x, from_y, from_z, to_x, to_y, to_z, target_key, edge_key,
     step_cost, source_fact_id, status, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
        $14, $15, $16, $17::jsonb)`,
				worldID, reservation.Tick, reservation.Sequence, actorID,
				reservation.IntentCode, reservation.From.X, reservation.From.Y,
				reservation.From.Z, reservation.To.X, reservation.To.Y,
				reservation.To.Z, reservation.TargetKey, reservation.EdgeKey,
				reservation.StepCost, sourceFactID, reservation.Status,
				[]byte(reservation.Metadata)); err != nil {
				return count, fmt.Errorf("restore world navigation reservation %d/%d: %w",
					reservation.Tick, reservation.Sequence, err)
			}
			count++
		}
	}
	for _, status := range runtime.Statuses {
		sourceFactID := factIDs[worldRuntimeRecoveryIdentity{tick: status.SourceFactTick, sequence: status.SourceFactSeq}]
		if _, err = tx.ExecContext(ctx, `
INSERT INTO world_actor_statuses
    (world_id, actor_id, instance_code, status_code, lifecycle_status,
     intensity_units, stacks, granted_tick, expires_tick, ended_tick,
     source_fact_id, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13::jsonb)`,
			worldID, actorIDs[status.ActorCode], status.InstanceCode, status.StatusCode,
			status.Lifecycle, status.IntensityUnits, status.Stacks, status.GrantedTick,
			cityNullableInt64(status.ExpiresTick), cityNullableInt64(status.EndedTick),
			sourceFactID, status.Version, []byte(status.Metadata)); err != nil {
			return count, fmt.Errorf("restore world actor status: %w", err)
		}
		count++
	}
	for _, effect := range runtime.Effects {
		identity := worldRuntimeRecoveryIdentity{tick: effect.Tick, sequence: effect.Sequence}
		preservedID := preserved.effects[identity]
		query := `
INSERT INTO world_effect_operations
    (world_id, tick, sequence, source_fact_id, operation_index, effect_type,
     executor_version, target_actor_id, target_key, before_units, delta_units,
     after_units, payload)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13::jsonb)`
		args := []any{worldID, effect.Tick, effect.Sequence,
			factIDs[worldRuntimeRecoveryIdentity{tick: effect.SourceFact.Tick, sequence: effect.SourceFact.Sequence}],
			effect.OperationIndex, effect.EffectType, effect.ExecutorVersion,
			nullableWorldRuntimeActorID(actorIDs, effect.TargetActorCode), nullableStringValue(effect.TargetKey),
			cityNullableInt64(effect.BeforeUnits), cityNullableInt64(effect.DeltaUnits),
			cityNullableInt64(effect.AfterUnits), []byte(effect.Payload)}
		if preservedID > 0 {
			query = `
INSERT INTO world_effect_operations
    (id, world_id, tick, sequence, source_fact_id, operation_index, effect_type,
     executor_version, target_actor_id, target_key, before_units, delta_units,
     after_units, payload)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14::jsonb)`
			args = append([]any{preservedID}, args...)
		}
		if _, err = tx.ExecContext(ctx, query, args...); err != nil {
			return count, fmt.Errorf("restore world effect %d/%d: %w", effect.Tick, effect.Sequence, err)
		}
		count++
	}
	for _, worldCase := range runtime.RuleCases {
		identity := worldRuntimeRecoveryIdentity{tick: worldCase.Tick, sequence: worldCase.Sequence}
		preservedID := preserved.cases[identity]
		query := `
INSERT INTO world_rule_cases
    (world_id, code, tick, sequence, source_fact_id, consequence_fact_id,
     subject_actor_id, rule_code, rule_version, rule_hash, category_code,
     scope_kind, scope_code, status, severity_units, decision_code,
     created_tick, decided_tick, closed_tick, payload)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
        $14, $15, $16, $17, $18, $19, $20::jsonb)`
		consequenceID := any(nil)
		if worldCase.ConsequenceFact != nil {
			consequenceID = factIDs[worldRuntimeRecoveryIdentity{tick: worldCase.ConsequenceFact.Tick, sequence: worldCase.ConsequenceFact.Sequence}]
		}
		args := []any{worldID, worldCase.Code, worldCase.Tick, worldCase.Sequence,
			factIDs[worldRuntimeRecoveryIdentity{tick: worldCase.SourceFact.Tick, sequence: worldCase.SourceFact.Sequence}],
			consequenceID, actorIDs[worldCase.SubjectActorCode], worldCase.RuleCode,
			worldCase.RuleVersion, worldCase.RuleHash, worldCase.CategoryCode,
			worldCase.ScopeKind, worldCase.ScopeCode, worldCase.Status, worldCase.SeverityUnits,
			nullableStringValue(worldCase.DecisionCode), worldCase.CreatedTick,
			cityNullableInt64(worldCase.DecidedTick), cityNullableInt64(worldCase.ClosedTick), []byte(worldCase.Payload)}
		if preservedID > 0 {
			query = `
INSERT INTO world_rule_cases
    (id, world_id, code, tick, sequence, source_fact_id, consequence_fact_id,
     subject_actor_id, rule_code, rule_version, rule_hash, category_code,
     scope_kind, scope_code, status, severity_units, decision_code,
     created_tick, decided_tick, closed_tick, payload)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
        $14, $15, $16, $17, $18, $19, $20, $21::jsonb)`
			args = append([]any{preservedID}, args...)
		}
		if _, err = tx.ExecContext(ctx, query, args...); err != nil {
			return count, fmt.Errorf("restore world rule case %s: %w", worldCase.Code, err)
		}
		count++
	}
	return count, nil
}

func nullableStringValue(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableWorldRuntimeActorID(actorIDs map[string]int64, actorCode *string) any {
	if actorCode == nil {
		return nil
	}
	actorID, exists := actorIDs[*actorCode]
	if !exists {
		return nil
	}
	return actorID
}

func resolveOptionalWorldRuntimeRecoveryFactID(
	factIDs map[worldRuntimeRecoveryIdentity]int64,
	reference *WorldRuntimeFactRef,
) (any, error) {
	if reference == nil {
		return nil, nil
	}
	id, exists := factIDs[worldRuntimeRecoveryIdentity{tick: reference.Tick, sequence: reference.Sequence}]
	if !exists {
		return nil, fmt.Errorf("unknown source fact %d/%d", reference.Tick, reference.Sequence)
	}
	return id, nil
}
