package service

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"sort"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/cityspatial"
)

// cityRealtimeAgentMoveTarget is a bounded, server-derived adjacent target.
// It is not a route, teleport coordinate, or client-selected destination.
type cityRealtimeAgentMoveTarget struct {
	X int64 `json:"x"`
	Y int64 `json:"y"`
	Z int32 `json:"z"`
}

// cityRealtimeAgentDecisionActionContext is the complete, finite action
// surface published to a 1.3 Character Agent decision.  It is intentionally
// data-only: the provider gets no route, database identifier, private seed,
// owner identity, or authority to synthesize a new target.
type cityRealtimeAgentDecisionActionContext struct {
	SchemaVersion          int                           `json:"schema_version"`
	AvailableActivityCodes []string                      `json:"available_activity_codes"`
	AvailableMoveTargets   []cityRealtimeAgentMoveTarget `json:"available_move_targets"`
	AvailablePortalCodes   []string                      `json:"available_portal_codes"`
	AvailableRoleCodes     []string                      `json:"available_role_codes"`
}

func cityRealtimeAgentActionContextSortedUnique(values []string, valid func(string) bool) bool {
	for index, value := range values {
		if !valid(value) || (index > 0 && values[index-1] >= value) {
			return false
		}
	}
	return true
}

func cityRealtimeAgentDecisionActionContextValid(contextPayload cityRealtimeAgentDecisionActionContext) bool {
	if contextPayload.SchemaVersion != 1 ||
		contextPayload.AvailableActivityCodes == nil ||
		contextPayload.AvailableMoveTargets == nil ||
		contextPayload.AvailablePortalCodes == nil ||
		contextPayload.AvailableRoleCodes == nil ||
		!cityRealtimeAgentActionContextSortedUnique(contextPayload.AvailableActivityCodes, func(code string) bool {
			return cityRealtimeAgentIdentifierValid(code, 64)
		}) ||
		!cityRealtimeAgentActionContextSortedUnique(contextPayload.AvailablePortalCodes, func(code string) bool {
			return cityRealtimeDueEventIdentifierValid(code, 128)
		}) ||
		!cityRealtimeAgentActionContextSortedUnique(contextPayload.AvailableRoleCodes, func(code string) bool {
			normalized, err := normalizeCityRealtimeCharacterRoleCode(code)
			return err == nil && normalized == code
		}) {
		return false
	}
	for index, target := range contextPayload.AvailableMoveTargets {
		if err := cityspatial.ValidateZ(target.Z, cityspatial.MinimumZ, cityspatial.MaximumZ); err != nil {
			return false
		}
		if index == 0 {
			continue
		}
		previous := contextPayload.AvailableMoveTargets[index-1]
		if previous.X > target.X ||
			(previous.X == target.X && previous.Y > target.Y) ||
			(previous.X == target.X && previous.Y == target.Y && previous.Z >= target.Z) {
			return false
		}
	}
	return true
}

func cityRealtimeAgentActionContextContains(values []string, candidate string) bool {
	index := sort.SearchStrings(values, candidate)
	return index < len(values) && values[index] == candidate
}

func cityRealtimeAgentActionContextStringSlicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func cityRealtimeAgentDecisionActionContextContainsMoveTarget(
	values []cityRealtimeAgentMoveTarget,
	candidate cityRealtimeActorSpawnCandidate,
) bool {
	for _, value := range values {
		if value.X == candidate.X && value.Y == candidate.Y && value.Z == candidate.Z {
			return true
		}
	}
	return false
}

// cityRealtimeAgentDecisionActionContextFromObservation parses only the
// server-sealed A3.2 observation shape. A malformed stored observation is an
// invariant violation rather than a recoverable provider response.
func cityRealtimeAgentDecisionActionContextFromObservation(
	payload json.RawMessage,
) (cityRealtimeAgentDecisionActionContext, []string, error) {
	var snapshot struct {
		AllowedActions []string `json:"allowed_actions"`
		Character      struct {
			ActionContext *cityRealtimeAgentDecisionActionContext `json:"action_context"`
		} `json:"character"`
	}
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		return cityRealtimeAgentDecisionActionContext{}, nil,
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_action_context"}).WithCause(err)
	}
	if snapshot.Character.ActionContext == nil || !cityRealtimeAgentDecisionActionContextValid(*snapshot.Character.ActionContext) {
		return cityRealtimeAgentDecisionActionContext{}, nil,
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_action_context"})
	}
	if snapshot.AllowedActions == nil || !cityRealtimeAgentActionContextSortedUnique(snapshot.AllowedActions, func(action string) bool {
		return cityRealtimeAgentIdentifierValid(action, 64)
	}) {
		return cityRealtimeAgentDecisionActionContext{}, nil,
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_allowed_actions"})
	}
	return *snapshot.Character.ActionContext, snapshot.AllowedActions, nil
}

// cityRealtimeAgentDecisionValidatePublishedAction prevents a model adapter
// from replacing a sealed, finite proposal with an arbitrary coordinate,
// portal, or role.  The due-event reducer repeats topology and domain checks
// after this boundary, because the world may still change before execution.
func cityRealtimeAgentDecisionValidatePublishedAction(
	binding cityRealtimeAgentPolicyBinding,
	agent cityRealtimeAgentInstance,
	observation cityRealtimeAgentObservationRecord,
	actionCode string,
	arguments map[string]any,
) error {
	if !cityRealtimeAgentCharacterActionRuntimeEnabled(binding) || agent.AgentSubtype != "character.user" {
		return nil
	}
	contextPayload, observedActions, err := cityRealtimeAgentDecisionActionContextFromObservation(observation.Payload)
	if err != nil {
		return err
	}
	expectedActions, available := cityRealtimeAgentDecisionAllowedActions(binding, agent)
	if !available || !cityRealtimeAgentActionContextStringSlicesEqual(observedActions, expectedActions) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_action_scope"})
	}
	if !cityRealtimeAgentActionContextContains(observedActions, actionCode) {
		return ErrCityRealtimeAgentDecisionUnavailable.WithMetadata(map[string]string{"field": "published_action"})
	}
	switch actionCode {
	case cityRealtimeAgentIntentActionWait:
		if len(arguments) != 0 {
			return ErrCityRealtimeAgentDecisionUnavailable.WithMetadata(map[string]string{"field": "action_arguments"})
		}
		return nil
	case cityRealtimeAgentIntentActionActivity:
		code, parseErr := cityRealtimeAgentDecisionActivityCodeFromArguments(arguments)
		if parseErr != nil || !cityRealtimeAgentActionContextContains(contextPayload.AvailableActivityCodes, code) {
			return ErrCityRealtimeAgentDecisionUnavailable.WithMetadata(map[string]string{"field": "published_activity"})
		}
		return nil
	case cityRealtimeAgentIntentActionMove:
		target, parseErr := cityRealtimeAgentDecisionMoveTargetFromArguments(arguments)
		if parseErr != nil || !cityRealtimeAgentDecisionActionContextContainsMoveTarget(contextPayload.AvailableMoveTargets, target) {
			return ErrCityRealtimeAgentDecisionUnavailable.WithMetadata(map[string]string{"field": "published_move_target"})
		}
		return nil
	case cityRealtimeAgentIntentActionPortal:
		code, parseErr := cityRealtimeAgentDecisionPortalCodeFromArguments(arguments)
		if parseErr != nil || !cityRealtimeAgentActionContextContains(contextPayload.AvailablePortalCodes, code) {
			return ErrCityRealtimeAgentDecisionUnavailable.WithMetadata(map[string]string{"field": "published_portal"})
		}
		return nil
	case cityRealtimeAgentIntentActionRole:
		code, parseErr := cityRealtimeAgentDecisionRoleCodeFromArguments(arguments)
		if parseErr != nil || !cityRealtimeAgentActionContextContains(contextPayload.AvailableRoleCodes, code) {
			return ErrCityRealtimeAgentDecisionUnavailable.WithMetadata(map[string]string{"field": "published_role"})
		}
		return nil
	default:
		return ErrCityRealtimeAgentDecisionUnavailable.WithMetadata(map[string]string{"field": "published_action"})
	}
}

func cityRealtimeAgentDecisionMoveTargetFromArguments(
	arguments map[string]any,
) (cityRealtimeActorSpawnCandidate, error) {
	if len(arguments) != 3 {
		return cityRealtimeActorSpawnCandidate{}, ErrCityRealtimeAgentDecisionUnavailable.WithMetadata(map[string]string{"field": "action_arguments"})
	}
	for _, key := range []string{"x", "y", "z"} {
		if _, found := arguments[key]; !found {
			return cityRealtimeActorSpawnCandidate{}, ErrCityRealtimeAgentDecisionUnavailable.WithMetadata(map[string]string{"field": "action_arguments"})
		}
	}
	raw, err := json.Marshal(arguments)
	if err != nil {
		return cityRealtimeActorSpawnCandidate{}, ErrCityRealtimeAgentDecisionUnavailable.WithMetadata(map[string]string{"field": "action_arguments"}).WithCause(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var target cityRealtimeAgentMoveTarget
	if err = decoder.Decode(&target); err != nil {
		return cityRealtimeActorSpawnCandidate{}, ErrCityRealtimeAgentDecisionUnavailable.WithMetadata(map[string]string{"field": "action_arguments"}).WithCause(err)
	}
	if err = cityspatial.ValidateZ(target.Z, cityspatial.MinimumZ, cityspatial.MaximumZ); err != nil {
		return cityRealtimeActorSpawnCandidate{}, ErrCityRealtimeAgentDecisionUnavailable.WithMetadata(map[string]string{"field": "action_arguments"}).WithCause(err)
	}
	return cityRealtimeActorSpawnCandidate{X: target.X, Y: target.Y, Z: target.Z}, nil
}

func cityRealtimeAgentDecisionMoveTargetFromRawArguments(
	arguments json.RawMessage,
) (cityRealtimeActorSpawnCandidate, error) {
	decoded := make(map[string]any)
	if err := json.Unmarshal(arguments, &decoded); err != nil {
		return cityRealtimeActorSpawnCandidate{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_intent_arguments"}).WithCause(err)
	}
	return cityRealtimeAgentDecisionMoveTargetFromArguments(decoded)
}

func cityRealtimeAgentDecisionPortalCodeFromArguments(arguments map[string]any) (string, error) {
	if len(arguments) != 1 {
		return "", ErrCityRealtimeAgentDecisionUnavailable.WithMetadata(map[string]string{"field": "action_arguments"})
	}
	rawCode, found := arguments["portal_code"]
	portalCode, ok := rawCode.(string)
	portalCode = strings.TrimSpace(portalCode)
	if !found || !ok || !cityRealtimeDueEventIdentifierValid(portalCode, 128) {
		return "", ErrCityRealtimeAgentDecisionUnavailable.WithMetadata(map[string]string{"field": "action_arguments"})
	}
	return portalCode, nil
}

func cityRealtimeAgentDecisionPortalCodeFromRawArguments(arguments json.RawMessage) (string, error) {
	decoded := make(map[string]any)
	if err := json.Unmarshal(arguments, &decoded); err != nil {
		return "", ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_intent_arguments"}).WithCause(err)
	}
	return cityRealtimeAgentDecisionPortalCodeFromArguments(decoded)
}

func cityRealtimeAgentDecisionRoleCodeFromArguments(arguments map[string]any) (string, error) {
	if len(arguments) != 1 {
		return "", ErrCityRealtimeAgentDecisionUnavailable.WithMetadata(map[string]string{"field": "action_arguments"})
	}
	rawCode, found := arguments["role_code"]
	roleCode, ok := rawCode.(string)
	if !found || !ok {
		return "", ErrCityRealtimeAgentDecisionUnavailable.WithMetadata(map[string]string{"field": "action_arguments"})
	}
	roleCode, err := normalizeCityRealtimeCharacterRoleCode(roleCode)
	if err != nil {
		return "", ErrCityRealtimeAgentDecisionUnavailable.WithMetadata(map[string]string{"field": "action_arguments"}).WithCause(err)
	}
	return roleCode, nil
}

func cityRealtimeAgentDecisionRoleCodeFromRawArguments(arguments json.RawMessage) (string, error) {
	decoded := make(map[string]any)
	if err := json.Unmarshal(arguments, &decoded); err != nil {
		return "", ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_intent_arguments"}).WithCause(err)
	}
	return cityRealtimeAgentDecisionRoleCodeFromArguments(decoded)
}

func cityRealtimeAgentDecisionAvailableMoveTargets(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	actorCode string,
	current cityRealtimeActorState,
) ([]cityRealtimeAgentMoveTarget, error) {
	if worldID <= 0 || !cityRealtimeAgentIdentifierValid(actorCode, 96) || !cityRealtimeActorStateValid(current) {
		return nil, ErrCityInvalidInput
	}
	candidates := make([]cityRealtimeActorSpawnCandidate, 0, 4)
	if current.X > math.MinInt64 {
		candidates = append(candidates, cityRealtimeActorSpawnCandidate{X: current.X - 1, Y: current.Y, Z: current.Z})
	}
	if current.Y > math.MinInt64 {
		candidates = append(candidates, cityRealtimeActorSpawnCandidate{X: current.X, Y: current.Y - 1, Z: current.Z})
	}
	if current.Y < math.MaxInt64 {
		candidates = append(candidates, cityRealtimeActorSpawnCandidate{X: current.X, Y: current.Y + 1, Z: current.Z})
	}
	if current.X < math.MaxInt64 {
		candidates = append(candidates, cityRealtimeActorSpawnCandidate{X: current.X + 1, Y: current.Y, Z: current.Z})
	}
	targets := make([]cityRealtimeAgentMoveTarget, 0, len(candidates))
	for _, candidate := range candidates {
		motionState, traversable, err := cityRealtimeCharacterWalkMotionState(ctx, queryer, worldID, current, candidate)
		if err != nil {
			return nil, err
		}
		if !traversable || (motionState != "walking" && motionState != "inside") {
			continue
		}
		occupied, err := cityRealtimeActorPositionOccupied(ctx, queryer, worldID, actorCode, candidate)
		if err != nil {
			return nil, err
		}
		if occupied {
			continue
		}
		targets = append(targets, cityRealtimeAgentMoveTarget{X: candidate.X, Y: candidate.Y, Z: candidate.Z})
	}
	sort.Slice(targets, func(left, right int) bool {
		if targets[left].X != targets[right].X {
			return targets[left].X < targets[right].X
		}
		if targets[left].Y != targets[right].Y {
			return targets[left].Y < targets[right].Y
		}
		return targets[left].Z < targets[right].Z
	})
	return targets, nil
}

// cityRealtimeAgentDecisionCharacterActionContext is emitted only by the
// A3.2 policy.  It contains server-derived, finite choices and its canonical
// hash becomes part of the decision precondition.  A model therefore cannot
// expand a one-cell movement choice into an arbitrary coordinate, portal, or
// role transition after the observation was sealed.
func cityRealtimeAgentDecisionCharacterActionContext(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, worldTimeUS int64,
	binding cityRealtimeAgentPolicyBinding,
	agent cityRealtimeAgentInstance,
	actorState cityRealtimeActorState,
	profile cityRealtimeCharacterProfile,
	runtime *cityRealtimeCharacterLifeRuntime,
) (cityRealtimeAgentDecisionActionContext, error) {
	if !cityRealtimeAgentCharacterActionRuntimeEnabled(binding) ||
		worldID <= 0 || worldTimeUS < 0 || agent.AgentSubtype != "character.user" ||
		agent.ActorCode == nil || *agent.ActorCode != actorState.ActorCode ||
		!cityRealtimeCharacterProfileMatchesRuntime(profile, runtime) {
		return cityRealtimeAgentDecisionActionContext{}, ErrCityInvalidInput
	}
	contextPayload := cityRealtimeAgentDecisionActionContext{
		SchemaVersion:          1,
		AvailableActivityCodes: make([]string, 0),
		AvailableMoveTargets:   make([]cityRealtimeAgentMoveTarget, 0),
		AvailablePortalCodes:   make([]string, 0),
		AvailableRoleCodes:     make([]string, 0),
	}
	if cityRealtimeAgentDecisionActionAllowed(binding, agent, cityRealtimeAgentIntentActionActivity) {
		availability, err := cityRealtimeCharacterActivityAvailability(
			ctx, queryer, worldID, worldTimeUS, actorState, profile, runtime,
		)
		if err != nil {
			return cityRealtimeAgentDecisionActionContext{}, err
		}
		codes := make([]string, 0, len(availability))
		for _, item := range availability {
			if item.Available {
				codes = append(codes, item.Code)
			}
		}
		sort.Strings(codes)
		contextPayload.AvailableActivityCodes = codes
	}
	if cityRealtimeAgentDecisionActionAllowed(binding, agent, cityRealtimeAgentIntentActionMove) {
		targets, err := cityRealtimeAgentDecisionAvailableMoveTargets(ctx, queryer, worldID, *agent.ActorCode, actorState)
		if err != nil {
			return cityRealtimeAgentDecisionActionContext{}, err
		}
		contextPayload.AvailableMoveTargets = targets
	}
	if cityRealtimeAgentDecisionActionAllowed(binding, agent, cityRealtimeAgentIntentActionPortal) {
		portals, err := cityRealtimeCharacterAvailablePortals(
			ctx, queryer, worldID, *agent.ActorCode,
			cityRealtimeActorSpawnCandidate{X: actorState.X, Y: actorState.Y, Z: actorState.Z},
		)
		if err != nil {
			return cityRealtimeAgentDecisionActionContext{}, err
		}
		codes := make([]string, 0, len(portals))
		for _, portal := range portals {
			codes = append(codes, portal.PortalCode)
		}
		sort.Strings(codes)
		contextPayload.AvailablePortalCodes = codes
	}
	if cityRealtimeAgentDecisionActionAllowed(binding, agent, cityRealtimeAgentIntentActionRole) &&
		cityRealtimeCharacterProgressionRuntimeEnabled(runtime) {
		codes := make([]string, 0, len(runtime.Progression.Roles))
		for _, role := range runtime.Progression.Roles {
			availability := cityRealtimeCharacterRoleAvailabilityForProfile(profile, runtime, role)
			if availability.Available {
				codes = append(codes, role.Code)
			}
		}
		sort.Strings(codes)
		contextPayload.AvailableRoleCodes = codes
	}
	if !cityRealtimeAgentDecisionActionContextValid(contextPayload) {
		return cityRealtimeAgentDecisionActionContext{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_action_context"})
	}
	return contextPayload, nil
}
