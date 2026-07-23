package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/cityspatial"
)

const (
	cityRealtimeCharacterNavigationPlanSchemaVersion  = 1
	cityRealtimeCharacterNavigationPlanBindingVersion = "city-realtime-character-navigation-plan-binding-v1"
	cityRealtimeCharacterNavigationPlanStateVersion   = "city-realtime-character-navigation-plan-state-v1"
	cityRealtimeCharacterNavigationPlanChainVersion   = "city-realtime-character-navigation-plan-chain-v1"
	cityRealtimeCharacterNavigationPlanEventVersion   = "city-realtime-character-navigation-plan-event-v1"
	cityRealtimeCharacterNavigationPlanRunVersion     = "city-realtime-character-navigation-plan-run-v1"

	// A plan has a deliberately finite surface: it is a server-derived route to
	// one static entrance endpoint, and its next cell is recomputed on every
	// realtime movement frame.  It does not expose a browser route, arbitrary
	// coordinate, traffic reservation, wallet, reward, or provider authority.
	cityRealtimeCharacterNavigationPlanMaximumSteps      int64 = 32
	cityRealtimeCharacterNavigationPlanMaximumCandidates       = 8
	cityRealtimeCharacterNavigationPlanPortalSearchLimit       = 64
	cityRealtimeCharacterNavigationPlanMaximumExpanded         = 4096
	cityRealtimeCharacterNavigationPlanDuePriority             = 90

	cityRealtimeCharacterNavigationPlanActive    = "active"
	cityRealtimeCharacterNavigationPlanArrived   = "arrived"
	cityRealtimeCharacterNavigationPlanBlocked   = "blocked"
	cityRealtimeCharacterNavigationPlanCancelled = "cancelled"

	cityRealtimeCharacterNavigationPlanReasonArrived          = "arrived"
	cityRealtimeCharacterNavigationPlanReasonBlockedPath      = "blocked_path"
	cityRealtimeCharacterNavigationPlanReasonBlockedOccupied  = "blocked_occupied"
	cityRealtimeCharacterNavigationPlanReasonBlockedLimit     = "blocked_step_limit"
	cityRealtimeCharacterNavigationPlanReasonCancelledControl = "cancelled_control"

	cityRealtimeCharacterNavigationPlanEventPlanned   = "navigation_planned"
	cityRealtimeCharacterNavigationPlanEventStep      = "navigation_step"
	cityRealtimeCharacterNavigationPlanEventArrived   = "navigation_arrived"
	cityRealtimeCharacterNavigationPlanEventBlocked   = "navigation_blocked"
	cityRealtimeCharacterNavigationPlanEventCancelled = "navigation_cancelled"

	cityRealtimeDueEventTypeCharacterNavigationStep = "system.realtime.character_navigation_step"
)

// CityRealtimeCharacterNavigationPlan is the owner-private projection of a
// finite autonomous navigation run.  It deliberately excludes the sealed
// Agent observation, source intent, route-search cache, provider data and
// other actors' positions.  The public actor projection remains the source of
// shared-map position rendering.
type CityRealtimeCharacterNavigationPlan struct {
	NavigationRunCode     string                        `json:"navigation_run_code"`
	DestinationPortalCode string                        `json:"destination_portal_code"`
	Destination           CityRealtimeCharacterLocation `json:"destination"`
	Status                string                        `json:"status"`
	TerminalReasonCode    string                        `json:"terminal_reason_code,omitempty"`
	StepsCompleted        int64                         `json:"steps_completed"`
	MaximumSteps          int64                         `json:"maximum_steps"`
	Revision              int64                         `json:"revision"`
	AcceptedFrameSequence int64                         `json:"accepted_frame_sequence"`
	LastFrameSequence     int64                         `json:"last_frame_sequence"`
}

type CityRealtimeCharacterNavigationPlanListInput struct {
	UserID  int64
	WorldID int64
	Limit   int
}

type cityRealtimeCharacterNavigationPlanBinding struct {
	SchemaVersion      int    `json:"schema_version"`
	AgentBindingHash   string `json:"agent_binding_hash"`
	SpatialContextHash string `json:"spatial_context_hash"`
	BindingHash        string `json:"binding_hash"`
}

type cityRealtimeCharacterNavigationPlanRuntime struct {
	Binding cityRealtimeCharacterNavigationPlanBinding
}

// cityRealtimeCharacterNavigationPlanHead is the mutable projection of one
// finite route run.  The static destination and source intent never change;
// every state transition is paired with one append-only event and a due-event
// boundary.  At most one active plan exists per character.
type cityRealtimeCharacterNavigationPlanHead struct {
	ActorCode             string
	NavigationRunCode     string
	DestinationPortalCode string
	Destination           cityRealtimeActorSpawnCandidate
	SourceIntentCode      string
	PlanRevision          int64
	PlanStatus            string
	TerminalReasonCode    string
	StepsCompleted        int64
	MaximumSteps          int64
	AcceptedFrameSequence int64
	LastFrameSequence     int64
	LastDueWorldTimeUS    int64
	NextDueWorldTimeUS    *int64
	EventChainHash        string
	StateHash             string
}

// cityRealtimeCharacterNavigationPlanEvent contains only the deterministic
// plan transition.  A movement event hash is retained only when the plan made
// an actual actor move, allowing the immutable plan history to prove that it
// did not synthesize a position change outside the actor ledger.
type cityRealtimeCharacterNavigationPlanEvent struct {
	ActorCode              string
	NavigationRunCode      string
	DestinationPortalCode  string
	Destination            cityRealtimeActorSpawnCandidate
	SourceIntentCode       string
	EventSequence          int64
	FrameSequence          int64
	EventType              string
	From                   cityRealtimeActorSpawnCandidate
	To                     cityRealtimeActorSpawnCandidate
	StepsCompleted         int64
	TerminalReasonCode     string
	ActorPositionEventHash string
	PreviousEventHash      string
	EventHash              string
}

type cityRealtimeCharacterNavigationPlanHashState struct {
	SchemaVersion int                                         `json:"schema_version"`
	Binding       *cityRealtimeCharacterNavigationPlanBinding `json:"binding,omitempty"`
	Heads         []cityRealtimeCharacterNavigationPlanHead   `json:"heads"`
}

type cityRealtimeCharacterNavigationPlanDuePayload struct {
	SchemaVersion         int    `json:"schema_version"`
	ActorCode             string `json:"actor_code"`
	NavigationRunCode     string `json:"navigation_run_code"`
	DestinationPortalCode string `json:"destination_portal_code"`
	PlanRevision          int64  `json:"plan_revision"`
}

type cityRealtimeCharacterNavigationDestination struct {
	PortalCode string
	Target     cityRealtimeActorSpawnCandidate
	PathLength int64
}

func cityRealtimeCharacterNavigationPlanRuntimeEnabled(binding cityRealtimeAgentPolicyBinding) bool {
	return binding.PolicyID == cityRealtimeAgentCorePolicyID &&
		binding.PolicyVersion == cityRealtimeAgentCorePolicyVersionNavigationPlan
}

func cityRealtimeCharacterNavigationPlanRunCode(
	actorCode, destinationPortalCode, sourceIntentCode string,
) (string, error) {
	if !cityRealtimePlayerActorCodeValid(actorCode) ||
		!cityRealtimeDueEventIdentifierValid(destinationPortalCode, 128) ||
		!cityRealtimeAgentIdentifierValid(sourceIntentCode, 96) {
		return "", ErrCityInvalidInput
	}
	code := "navigation.run." + cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeCharacterNavigationPlanRunVersion,
		actorCode,
		destinationPortalCode,
		sourceIntentCode,
	}, "\x1f")))
	if !cityRealtimeAgentIdentifierValid(code, 96) {
		return "", ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_navigation_run_code"})
	}
	return code, nil
}

func cityRealtimeCharacterNavigationPlanStatusValid(status string) bool {
	switch status {
	case cityRealtimeCharacterNavigationPlanActive,
		cityRealtimeCharacterNavigationPlanArrived,
		cityRealtimeCharacterNavigationPlanBlocked,
		cityRealtimeCharacterNavigationPlanCancelled:
		return true
	default:
		return false
	}
}

func cityRealtimeCharacterNavigationPlanTerminalReasonValid(status, reason string) bool {
	switch status {
	case cityRealtimeCharacterNavigationPlanActive:
		return reason == ""
	case cityRealtimeCharacterNavigationPlanArrived:
		return reason == cityRealtimeCharacterNavigationPlanReasonArrived
	case cityRealtimeCharacterNavigationPlanBlocked:
		return reason == cityRealtimeCharacterNavigationPlanReasonBlockedPath ||
			reason == cityRealtimeCharacterNavigationPlanReasonBlockedOccupied ||
			reason == cityRealtimeCharacterNavigationPlanReasonBlockedLimit
	case cityRealtimeCharacterNavigationPlanCancelled:
		return reason == cityRealtimeCharacterNavigationPlanReasonCancelledControl
	default:
		return false
	}
}

func cityRealtimeCharacterNavigationPlanEventTypeValid(eventType string) bool {
	switch eventType {
	case cityRealtimeCharacterNavigationPlanEventPlanned,
		cityRealtimeCharacterNavigationPlanEventStep,
		cityRealtimeCharacterNavigationPlanEventArrived,
		cityRealtimeCharacterNavigationPlanEventBlocked,
		cityRealtimeCharacterNavigationPlanEventCancelled:
		return true
	default:
		return false
	}
}

func cityRealtimeCharacterNavigationPlanBindingHash(binding cityRealtimeCharacterNavigationPlanBinding) string {
	return cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeCharacterNavigationPlanBindingVersion,
		binding.AgentBindingHash,
		binding.SpatialContextHash,
	}, "\x1f")))
}

func cityRealtimeCharacterNavigationPlanBindingValid(binding cityRealtimeCharacterNavigationPlanBinding) bool {
	return binding.SchemaVersion == cityRealtimeCharacterNavigationPlanSchemaVersion &&
		cityRealtimeSHA256Hex(binding.AgentBindingHash) && cityRealtimeSHA256Hex(binding.SpatialContextHash) &&
		cityRealtimeSHA256Hex(binding.BindingHash) &&
		binding.BindingHash == cityRealtimeCharacterNavigationPlanBindingHash(binding)
}

func cityRealtimeCharacterNavigationPlanDestinationValid(destination cityRealtimeActorSpawnCandidate) bool {
	return destination.Z == cityspatial.SurfaceZ
}

func cityRealtimeCharacterNavigationPlanStaticFieldsValid(head cityRealtimeCharacterNavigationPlanHead) bool {
	return cityRealtimePlayerActorCodeValid(head.ActorCode) &&
		cityRealtimeAgentIdentifierValid(head.NavigationRunCode, 96) &&
		strings.HasPrefix(head.NavigationRunCode, "navigation.run.") &&
		cityRealtimeDueEventIdentifierValid(head.DestinationPortalCode, 128) &&
		cityRealtimeCharacterNavigationPlanDestinationValid(head.Destination) &&
		cityRealtimeAgentIdentifierValid(head.SourceIntentCode, 96) &&
		head.MaximumSteps == cityRealtimeCharacterNavigationPlanMaximumSteps &&
		head.AcceptedFrameSequence > 0
}

func cityRealtimeCharacterNavigationPlanStateHashUnchecked(head cityRealtimeCharacterNavigationPlanHead) string {
	nextDue := ""
	if head.NextDueWorldTimeUS != nil {
		nextDue = strconv.FormatInt(*head.NextDueWorldTimeUS, 10)
	}
	return cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeCharacterNavigationPlanStateVersion,
		head.ActorCode,
		head.NavigationRunCode,
		head.DestinationPortalCode,
		strconv.FormatInt(head.Destination.X, 10),
		strconv.FormatInt(head.Destination.Y, 10),
		strconv.FormatInt(int64(head.Destination.Z), 10),
		head.SourceIntentCode,
		strconv.FormatInt(head.PlanRevision, 10),
		head.PlanStatus,
		head.TerminalReasonCode,
		strconv.FormatInt(head.StepsCompleted, 10),
		strconv.FormatInt(head.MaximumSteps, 10),
		strconv.FormatInt(head.AcceptedFrameSequence, 10),
		strconv.FormatInt(head.LastFrameSequence, 10),
		strconv.FormatInt(head.LastDueWorldTimeUS, 10),
		nextDue,
		head.EventChainHash,
	}, "\x1f")))
}

func cityRealtimeCharacterNavigationPlanHeadValid(head cityRealtimeCharacterNavigationPlanHead) bool {
	if !cityRealtimeCharacterNavigationPlanStaticFieldsValid(head) ||
		head.PlanRevision <= 0 || !cityRealtimeCharacterNavigationPlanStatusValid(head.PlanStatus) ||
		!cityRealtimeCharacterNavigationPlanTerminalReasonValid(head.PlanStatus, head.TerminalReasonCode) ||
		head.StepsCompleted < 0 || head.StepsCompleted > head.MaximumSteps ||
		head.LastFrameSequence < head.AcceptedFrameSequence || head.LastDueWorldTimeUS < 0 ||
		!cityRealtimeSHA256Hex(head.EventChainHash) || !cityRealtimeSHA256Hex(head.StateHash) {
		return false
	}
	if head.PlanStatus == cityRealtimeCharacterNavigationPlanActive {
		if head.NextDueWorldTimeUS == nil || *head.NextDueWorldTimeUS <= head.LastDueWorldTimeUS ||
			*head.NextDueWorldTimeUS-head.LastDueWorldTimeUS != cityRealtimeTimeQuantumUS ||
			head.StepsCompleted >= head.MaximumSteps {
			return false
		}
	} else if head.NextDueWorldTimeUS != nil {
		return false
	}
	return head.StateHash == cityRealtimeCharacterNavigationPlanStateHashUnchecked(head)
}

func cityRealtimeCharacterNavigationPlanChainGenesisHash(head cityRealtimeCharacterNavigationPlanHead) (string, error) {
	if !cityRealtimeCharacterNavigationPlanStaticFieldsValid(head) {
		return "", ErrCityInvalidInput
	}
	return cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeCharacterNavigationPlanChainVersion,
		head.ActorCode,
		head.NavigationRunCode,
		head.DestinationPortalCode,
		strconv.FormatInt(head.Destination.X, 10),
		strconv.FormatInt(head.Destination.Y, 10),
		strconv.FormatInt(int64(head.Destination.Z), 10),
		head.SourceIntentCode,
		strconv.FormatInt(head.AcceptedFrameSequence, 10),
	}, "\x1f"))), nil
}

func cityRealtimeCharacterNavigationPlanEventHash(event cityRealtimeCharacterNavigationPlanEvent) (string, error) {
	if !cityRealtimeCharacterNavigationPlanEventShapeValid(event) {
		return "", ErrCityInvalidInput
	}
	return cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeCharacterNavigationPlanEventVersion,
		event.ActorCode,
		event.NavigationRunCode,
		event.DestinationPortalCode,
		strconv.FormatInt(event.Destination.X, 10),
		strconv.FormatInt(event.Destination.Y, 10),
		strconv.FormatInt(int64(event.Destination.Z), 10),
		event.SourceIntentCode,
		strconv.FormatInt(event.EventSequence, 10),
		strconv.FormatInt(event.FrameSequence, 10),
		event.EventType,
		strconv.FormatInt(event.From.X, 10),
		strconv.FormatInt(event.From.Y, 10),
		strconv.FormatInt(int64(event.From.Z), 10),
		strconv.FormatInt(event.To.X, 10),
		strconv.FormatInt(event.To.Y, 10),
		strconv.FormatInt(int64(event.To.Z), 10),
		strconv.FormatInt(event.StepsCompleted, 10),
		event.TerminalReasonCode,
		event.ActorPositionEventHash,
		event.PreviousEventHash,
	}, "\x1f"))), nil
}

func cityRealtimeCharacterNavigationPlanEventShapeValid(event cityRealtimeCharacterNavigationPlanEvent) bool {
	if !cityRealtimePlayerActorCodeValid(event.ActorCode) ||
		!cityRealtimeAgentIdentifierValid(event.NavigationRunCode, 96) ||
		!cityRealtimeDueEventIdentifierValid(event.DestinationPortalCode, 128) ||
		!cityRealtimeCharacterNavigationPlanDestinationValid(event.Destination) ||
		!cityRealtimeAgentIdentifierValid(event.SourceIntentCode, 96) ||
		event.EventSequence <= 0 || event.FrameSequence <= 0 ||
		!cityRealtimeCharacterNavigationPlanEventTypeValid(event.EventType) ||
		!cityRealtimeCharacterNavigationPlanDestinationValid(event.From) ||
		!cityRealtimeCharacterNavigationPlanDestinationValid(event.To) ||
		event.StepsCompleted < 0 || event.StepsCompleted > cityRealtimeCharacterNavigationPlanMaximumSteps ||
		!cityRealtimeSHA256Hex(event.PreviousEventHash) {
		return false
	}
	return event.ActorPositionEventHash == "" || cityRealtimeSHA256Hex(event.ActorPositionEventHash)
}

func cityRealtimeCharacterNavigationPlanEventValid(event cityRealtimeCharacterNavigationPlanEvent) bool {
	return cityRealtimeCharacterNavigationPlanEventShapeValid(event) && cityRealtimeSHA256Hex(event.EventHash)
}

func cityRealtimeCharacterNavigationPlanNextDueWorldTime(currentWorldTimeUS int64) (int64, error) {
	if currentWorldTimeUS < 0 || currentWorldTimeUS > cityRealtimeMaximumWorldTimeUS-cityRealtimeTimeQuantumUS {
		return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_navigation_due_time"})
	}
	next := currentWorldTimeUS + cityRealtimeTimeQuantumUS
	if next%cityRealtimeTimeQuantumUS != 0 {
		return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_navigation_due_time"})
	}
	return next, nil
}

func cityRealtimeCharacterNavigationPlanNew(
	actorState cityRealtimeActorState,
	destination cityRealtimeCharacterNavigationDestination,
	sourceIntentCode string,
	frameSequence, currentWorldTimeUS int64,
) (cityRealtimeCharacterNavigationPlanHead, cityRealtimeCharacterNavigationPlanEvent, error) {
	if !cityRealtimeActorStateValid(actorState) || actorState.Z != cityspatial.SurfaceZ ||
		!cityRealtimeDueEventIdentifierValid(destination.PortalCode, 128) ||
		!cityRealtimeCharacterNavigationPlanDestinationValid(destination.Target) || destination.PathLength <= 0 ||
		destination.PathLength > cityRealtimeCharacterNavigationPlanMaximumSteps ||
		!cityRealtimeAgentIdentifierValid(sourceIntentCode, 96) || frameSequence <= 0 || currentWorldTimeUS < 0 {
		return cityRealtimeCharacterNavigationPlanHead{}, cityRealtimeCharacterNavigationPlanEvent{}, ErrCityInvalidInput
	}
	runCode, err := cityRealtimeCharacterNavigationPlanRunCode(actorState.ActorCode, destination.PortalCode, sourceIntentCode)
	if err != nil {
		return cityRealtimeCharacterNavigationPlanHead{}, cityRealtimeCharacterNavigationPlanEvent{}, err
	}
	nextDue, err := cityRealtimeCharacterNavigationPlanNextDueWorldTime(currentWorldTimeUS)
	if err != nil {
		return cityRealtimeCharacterNavigationPlanHead{}, cityRealtimeCharacterNavigationPlanEvent{}, err
	}
	head := cityRealtimeCharacterNavigationPlanHead{
		ActorCode:             actorState.ActorCode,
		NavigationRunCode:     runCode,
		DestinationPortalCode: destination.PortalCode,
		Destination:           destination.Target,
		SourceIntentCode:      sourceIntentCode,
		PlanRevision:          1,
		PlanStatus:            cityRealtimeCharacterNavigationPlanActive,
		StepsCompleted:        0,
		MaximumSteps:          cityRealtimeCharacterNavigationPlanMaximumSteps,
		AcceptedFrameSequence: frameSequence,
		LastFrameSequence:     frameSequence,
		LastDueWorldTimeUS:    currentWorldTimeUS,
		NextDueWorldTimeUS:    &nextDue,
	}
	genesisHash, err := cityRealtimeCharacterNavigationPlanChainGenesisHash(head)
	if err != nil {
		return cityRealtimeCharacterNavigationPlanHead{}, cityRealtimeCharacterNavigationPlanEvent{}, err
	}
	event := cityRealtimeCharacterNavigationPlanEvent{
		ActorCode:             head.ActorCode,
		NavigationRunCode:     head.NavigationRunCode,
		DestinationPortalCode: head.DestinationPortalCode,
		Destination:           head.Destination,
		SourceIntentCode:      head.SourceIntentCode,
		EventSequence:         1,
		FrameSequence:         frameSequence,
		EventType:             cityRealtimeCharacterNavigationPlanEventPlanned,
		From:                  cityRealtimeActorSpawnCandidate{X: actorState.X, Y: actorState.Y, Z: actorState.Z},
		To:                    cityRealtimeActorSpawnCandidate{X: actorState.X, Y: actorState.Y, Z: actorState.Z},
		StepsCompleted:        0,
		PreviousEventHash:     genesisHash,
	}
	if event.EventHash, err = cityRealtimeCharacterNavigationPlanEventHash(event); err != nil {
		return cityRealtimeCharacterNavigationPlanHead{}, cityRealtimeCharacterNavigationPlanEvent{}, err
	}
	head.EventChainHash = event.EventHash
	head.StateHash = cityRealtimeCharacterNavigationPlanStateHashUnchecked(head)
	if !cityRealtimeCharacterNavigationPlanHeadValid(head) || !cityRealtimeCharacterNavigationPlanEventValid(event) {
		return cityRealtimeCharacterNavigationPlanHead{}, cityRealtimeCharacterNavigationPlanEvent{},
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_navigation_plan_genesis"})
	}
	return head, event, nil
}

func cityRealtimeCharacterNavigationPlanTerminalEventType(status string) string {
	switch status {
	case cityRealtimeCharacterNavigationPlanArrived:
		return cityRealtimeCharacterNavigationPlanEventArrived
	case cityRealtimeCharacterNavigationPlanBlocked:
		return cityRealtimeCharacterNavigationPlanEventBlocked
	case cityRealtimeCharacterNavigationPlanCancelled:
		return cityRealtimeCharacterNavigationPlanEventCancelled
	default:
		return ""
	}
}

func cityRealtimeCharacterNavigationPlanAdvance(
	head cityRealtimeCharacterNavigationPlanHead,
	frameSequence, currentWorldTimeUS int64,
	from, to cityRealtimeActorSpawnCandidate,
	actorPositionEventHash string,
	terminalStatus, terminalReason string,
) (cityRealtimeCharacterNavigationPlanHead, cityRealtimeCharacterNavigationPlanEvent, error) {
	if !cityRealtimeCharacterNavigationPlanHeadValid(head) || head.PlanStatus != cityRealtimeCharacterNavigationPlanActive ||
		frameSequence <= head.LastFrameSequence || currentWorldTimeUS != head.LastDueWorldTimeUS+cityRealtimeTimeQuantumUS ||
		!cityRealtimeCharacterNavigationPlanDestinationValid(from) || !cityRealtimeCharacterNavigationPlanDestinationValid(to) ||
		terminalStatus != "" && terminalStatus != cityRealtimeCharacterNavigationPlanArrived &&
			terminalStatus != cityRealtimeCharacterNavigationPlanBlocked && terminalStatus != cityRealtimeCharacterNavigationPlanCancelled {
		return cityRealtimeCharacterNavigationPlanHead{}, cityRealtimeCharacterNavigationPlanEvent{}, ErrCityInvalidInput
	}
	next := head
	next.PlanRevision++
	next.LastFrameSequence = frameSequence
	next.LastDueWorldTimeUS = currentWorldTimeUS
	eventType := cityRealtimeCharacterNavigationPlanEventStep

	if actorPositionEventHash != "" {
		if !cityRealtimeSHA256Hex(actorPositionEventHash) || !cityRealtimeCharacterAdjacentStep(
			cityRealtimeActorState{X: from.X, Y: from.Y, Z: from.Z}, to,
		) {
			return cityRealtimeCharacterNavigationPlanHead{}, cityRealtimeCharacterNavigationPlanEvent{}, ErrCityInvalidInput
		}
		next.StepsCompleted++
		if next.StepsCompleted > next.MaximumSteps {
			return cityRealtimeCharacterNavigationPlanHead{}, cityRealtimeCharacterNavigationPlanEvent{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_navigation_plan_steps"})
		}
	} else if from != to {
		return cityRealtimeCharacterNavigationPlanHead{}, cityRealtimeCharacterNavigationPlanEvent{}, ErrCityInvalidInput
	}

	if terminalStatus == "" && cityRealtimeCharacterPositionEquals(to, head.Destination) {
		terminalStatus = cityRealtimeCharacterNavigationPlanArrived
		terminalReason = cityRealtimeCharacterNavigationPlanReasonArrived
	}
	if terminalStatus == "" && next.StepsCompleted >= next.MaximumSteps {
		terminalStatus = cityRealtimeCharacterNavigationPlanBlocked
		terminalReason = cityRealtimeCharacterNavigationPlanReasonBlockedLimit
	}
	if terminalStatus == "" {
		nextDue, err := cityRealtimeCharacterNavigationPlanNextDueWorldTime(currentWorldTimeUS)
		if err != nil {
			return cityRealtimeCharacterNavigationPlanHead{}, cityRealtimeCharacterNavigationPlanEvent{}, err
		}
		next.NextDueWorldTimeUS = &nextDue
	} else {
		if !cityRealtimeCharacterNavigationPlanTerminalReasonValid(terminalStatus, terminalReason) {
			return cityRealtimeCharacterNavigationPlanHead{}, cityRealtimeCharacterNavigationPlanEvent{}, ErrCityInvalidInput
		}
		next.PlanStatus = terminalStatus
		next.TerminalReasonCode = terminalReason
		next.NextDueWorldTimeUS = nil
		eventType = cityRealtimeCharacterNavigationPlanTerminalEventType(terminalStatus)
	}
	event := cityRealtimeCharacterNavigationPlanEvent{
		ActorCode:              next.ActorCode,
		NavigationRunCode:      next.NavigationRunCode,
		DestinationPortalCode:  next.DestinationPortalCode,
		Destination:            next.Destination,
		SourceIntentCode:       next.SourceIntentCode,
		EventSequence:          next.PlanRevision,
		FrameSequence:          frameSequence,
		EventType:              eventType,
		From:                   from,
		To:                     to,
		StepsCompleted:         next.StepsCompleted,
		TerminalReasonCode:     next.TerminalReasonCode,
		ActorPositionEventHash: actorPositionEventHash,
		PreviousEventHash:      head.EventChainHash,
	}
	var err error
	if event.EventHash, err = cityRealtimeCharacterNavigationPlanEventHash(event); err != nil {
		return cityRealtimeCharacterNavigationPlanHead{}, cityRealtimeCharacterNavigationPlanEvent{}, err
	}
	next.EventChainHash = event.EventHash
	next.StateHash = cityRealtimeCharacterNavigationPlanStateHashUnchecked(next)
	if !cityRealtimeCharacterNavigationPlanHeadValid(next) || !cityRealtimeCharacterNavigationPlanEventValid(event) {
		return cityRealtimeCharacterNavigationPlanHead{}, cityRealtimeCharacterNavigationPlanEvent{},
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_navigation_plan_transition"})
	}
	return next, event, nil
}

func cityRealtimeCharacterNavigationPlanProjection(head cityRealtimeCharacterNavigationPlanHead) CityRealtimeCharacterNavigationPlan {
	return CityRealtimeCharacterNavigationPlan{
		NavigationRunCode:     head.NavigationRunCode,
		DestinationPortalCode: head.DestinationPortalCode,
		Destination: CityRealtimeCharacterLocation{
			X: head.Destination.X,
			Y: head.Destination.Y,
			Z: head.Destination.Z,
		},
		Status:                head.PlanStatus,
		TerminalReasonCode:    head.TerminalReasonCode,
		StepsCompleted:        head.StepsCompleted,
		MaximumSteps:          head.MaximumSteps,
		Revision:              head.PlanRevision,
		AcceptedFrameSequence: head.AcceptedFrameSequence,
		LastFrameSequence:     head.LastFrameSequence,
	}
}

func initializeCityRealtimeCharacterNavigationPlanFoundation(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	simulationVersion string,
) error {
	if tx == nil || worldID <= 0 || !cityEngineSupportsRealtimeStaticWorldgen(simulationVersion) {
		return ErrCitySimulationVersion.WithMetadata(map[string]string{"version": simulationVersion})
	}
	agentState, err := loadCityRealtimeAgentHashState(ctx, tx, worldID)
	if err != nil {
		return err
	}
	if agentState == nil || agentState.Binding == nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_navigation_policy"})
	}
	if !cityRealtimeCharacterNavigationPlanRuntimeEnabled(*agentState.Binding) {
		return nil
	}
	binding, err := cityRealtimeCharacterNavigationPlanBindingForWorld(ctx, tx, worldID, *agentState.Binding)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx,
		`SELECT set_config('sub2api.city_realtime_character_navigation_initialize_world_id', $1, TRUE)`,
		strconv.FormatInt(worldID, 10),
	); err != nil {
		return fmt.Errorf("activate realtime character navigation initialization gate: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_realtime_character_navigation_plan_world_bindings
    (world_id, schema_version, agent_binding_hash, spatial_context_hash, binding_hash, metadata)
VALUES ($1, $2, $3, $4, $5, '{}'::jsonb)`,
		worldID, binding.SchemaVersion, binding.AgentBindingHash, binding.SpatialContextHash, binding.BindingHash,
	); err != nil {
		return fmt.Errorf("create realtime character navigation binding: %w", err)
	}
	return nil
}

func cityRealtimeCharacterNavigationPlanBindingForWorld(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	agentBinding cityRealtimeAgentPolicyBinding,
) (cityRealtimeCharacterNavigationPlanBinding, error) {
	if worldID <= 0 || !cityRealtimeCharacterNavigationPlanRuntimeEnabled(agentBinding) {
		return cityRealtimeCharacterNavigationPlanBinding{}, ErrCityInvalidInput
	}
	binding := cityRealtimeCharacterNavigationPlanBinding{
		SchemaVersion:    cityRealtimeCharacterNavigationPlanSchemaVersion,
		AgentBindingHash: agentBinding.BindingHash,
	}
	var spatialWorldID int64
	err := queryer.QueryRowContext(ctx, `
SELECT spatial.world_id, spatial.context_hash
FROM city_realtime_spatial_bindings spatial
JOIN city_realtime_agent_world_bindings agent ON agent.world_id = spatial.world_id
WHERE spatial.world_id = $1 AND agent.binding_hash = $2`, worldID, agentBinding.BindingHash,
	).Scan(&spatialWorldID, &binding.SpatialContextHash)
	if errors.Is(err, sql.ErrNoRows) {
		return cityRealtimeCharacterNavigationPlanBinding{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_navigation_spatial_binding"})
	}
	if err != nil {
		return cityRealtimeCharacterNavigationPlanBinding{}, fmt.Errorf("load realtime character navigation world spatial binding: %w", err)
	}
	if spatialWorldID != worldID {
		return cityRealtimeCharacterNavigationPlanBinding{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_navigation_spatial_world"})
	}
	binding.BindingHash = cityRealtimeCharacterNavigationPlanBindingHash(binding)
	if !cityRealtimeCharacterNavigationPlanBindingValid(binding) {
		return cityRealtimeCharacterNavigationPlanBinding{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_navigation_binding"})
	}
	return binding, nil
}

func enableCityRealtimeCharacterNavigationPlanMutationGate(
	ctx context.Context,
	tx *sql.Tx,
	worldID, frameSequence, dueWorldTimeUS int64,
) error {
	if tx == nil || worldID <= 0 || frameSequence <= 0 || dueWorldTimeUS < 0 ||
		dueWorldTimeUS%cityRealtimeTimeQuantumUS != 0 {
		return ErrCityInvalidInput
	}
	for _, setting := range []struct {
		name  string
		value int64
	}{
		{name: "sub2api.city_realtime_character_navigation_world_id", value: worldID},
		{name: "sub2api.city_realtime_character_navigation_frame_sequence", value: frameSequence},
		{name: "sub2api.city_realtime_character_navigation_due_world_time_us", value: dueWorldTimeUS},
	} {
		if _, err := tx.ExecContext(ctx, `SELECT set_config($1, $2, TRUE)`, setting.name, strconv.FormatInt(setting.value, 10)); err != nil {
			return fmt.Errorf("activate realtime character navigation gate %s: %w", setting.name, err)
		}
	}
	return nil
}

func loadCityRealtimeCharacterNavigationPlanRuntime(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*cityRealtimeCharacterNavigationPlanRuntime, error) {
	if worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	binding := cityRealtimeCharacterNavigationPlanBinding{}
	var policyID, policyVersion, agentBindingHash, spatialContextHash string
	err := queryer.QueryRowContext(ctx, `
SELECT navigation.schema_version, navigation.agent_binding_hash, navigation.spatial_context_hash,
       navigation.binding_hash, agent.policy_id, agent.policy_version, agent.binding_hash,
       spatial.context_hash
FROM city_realtime_character_navigation_plan_world_bindings navigation
JOIN city_realtime_agent_world_bindings agent ON agent.world_id = navigation.world_id
JOIN city_realtime_spatial_bindings spatial ON spatial.world_id = navigation.world_id
WHERE navigation.world_id = $1`, worldID,
	).Scan(&binding.SchemaVersion, &binding.AgentBindingHash, &binding.SpatialContextHash,
		&binding.BindingHash, &policyID, &policyVersion, &agentBindingHash, &spatialContextHash)
	if errors.Is(err, sql.ErrNoRows) {
		var headCount int
		if countErr := queryer.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM city_realtime_character_navigation_plan_heads WHERE world_id = $1`, worldID,
		).Scan(&headCount); countErr != nil {
			return nil, fmt.Errorf("check historical realtime character navigation state: %w", countErr)
		}
		if headCount != 0 {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_navigation_binding"})
		}
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load realtime character navigation binding: %w", err)
	}
	if policyID != cityRealtimeAgentCorePolicyID || policyVersion != cityRealtimeAgentCorePolicyVersionNavigationPlan ||
		binding.AgentBindingHash != agentBindingHash || binding.SpatialContextHash != spatialContextHash ||
		!cityRealtimeCharacterNavigationPlanBindingValid(binding) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_navigation_binding"})
	}
	return &cityRealtimeCharacterNavigationPlanRuntime{Binding: binding}, nil
}

func scanCityRealtimeCharacterNavigationPlanHead(scanner cityScannable) (cityRealtimeCharacterNavigationPlanHead, error) {
	head := cityRealtimeCharacterNavigationPlanHead{}
	var nextDue sql.NullInt64
	err := scanner.Scan(
		&head.ActorCode, &head.NavigationRunCode, &head.DestinationPortalCode,
		&head.Destination.X, &head.Destination.Y, &head.Destination.Z,
		&head.SourceIntentCode, &head.PlanRevision, &head.PlanStatus, &head.TerminalReasonCode,
		&head.StepsCompleted, &head.MaximumSteps, &head.AcceptedFrameSequence,
		&head.LastFrameSequence, &head.LastDueWorldTimeUS, &nextDue,
		&head.EventChainHash, &head.StateHash,
	)
	if err != nil {
		return cityRealtimeCharacterNavigationPlanHead{}, err
	}
	head.NextDueWorldTimeUS = nullInt64Pointer(nextDue)
	if !cityRealtimeCharacterNavigationPlanHeadValid(head) {
		return cityRealtimeCharacterNavigationPlanHead{},
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_navigation_plan_head"})
	}
	return head, nil
}

func loadCityRealtimeCharacterNavigationPlanHead(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	actorCode, runCode string,
	forUpdate bool,
) (cityRealtimeCharacterNavigationPlanHead, bool, error) {
	if worldID <= 0 || !cityRealtimePlayerActorCodeValid(actorCode) ||
		!cityRealtimeAgentIdentifierValid(runCode, 96) {
		return cityRealtimeCharacterNavigationPlanHead{}, false, ErrCityInvalidInput
	}
	query := `
SELECT actor_code, navigation_run_code, destination_portal_code,
       destination_x, destination_y, destination_z, source_intent_code,
       plan_revision, plan_status, terminal_reason_code, steps_completed,
       maximum_steps, accepted_frame_sequence, last_frame_sequence,
       last_due_world_time_us, next_due_world_time_us, event_chain_hash, state_hash
FROM city_realtime_character_navigation_plan_heads
WHERE world_id = $1 AND actor_code = $2 AND navigation_run_code = $3`
	if forUpdate {
		query += " FOR UPDATE"
	}
	head, err := scanCityRealtimeCharacterNavigationPlanHead(queryer.QueryRowContext(ctx, query, worldID, actorCode, runCode))
	if errors.Is(err, sql.ErrNoRows) {
		return cityRealtimeCharacterNavigationPlanHead{}, false, nil
	}
	if err != nil {
		return cityRealtimeCharacterNavigationPlanHead{}, false, fmt.Errorf("load realtime character navigation plan head: %w", err)
	}
	return head, true, nil
}

func loadCityRealtimeCharacterActiveNavigationPlan(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	actorCode string,
	forUpdate bool,
) (cityRealtimeCharacterNavigationPlanHead, bool, error) {
	if worldID <= 0 || !cityRealtimePlayerActorCodeValid(actorCode) {
		return cityRealtimeCharacterNavigationPlanHead{}, false, ErrCityInvalidInput
	}
	query := `
SELECT actor_code, navigation_run_code, destination_portal_code,
       destination_x, destination_y, destination_z, source_intent_code,
       plan_revision, plan_status, terminal_reason_code, steps_completed,
       maximum_steps, accepted_frame_sequence, last_frame_sequence,
       last_due_world_time_us, next_due_world_time_us, event_chain_hash, state_hash
FROM city_realtime_character_navigation_plan_heads
WHERE world_id = $1 AND actor_code = $2 AND plan_status = 'active'`
	if forUpdate {
		query += " FOR UPDATE"
	}
	head, err := scanCityRealtimeCharacterNavigationPlanHead(queryer.QueryRowContext(ctx, query, worldID, actorCode))
	if errors.Is(err, sql.ErrNoRows) {
		return cityRealtimeCharacterNavigationPlanHead{}, false, nil
	}
	if err != nil {
		return cityRealtimeCharacterNavigationPlanHead{}, false, fmt.Errorf("load active realtime character navigation plan: %w", err)
	}
	return head, true, nil
}

func scanCityRealtimeCharacterNavigationPlanEvent(scanner cityScannable) (cityRealtimeCharacterNavigationPlanEvent, error) {
	event := cityRealtimeCharacterNavigationPlanEvent{}
	err := scanner.Scan(
		&event.ActorCode, &event.NavigationRunCode, &event.DestinationPortalCode,
		&event.Destination.X, &event.Destination.Y, &event.Destination.Z,
		&event.SourceIntentCode, &event.EventSequence, &event.FrameSequence, &event.EventType,
		&event.From.X, &event.From.Y, &event.From.Z, &event.To.X, &event.To.Y, &event.To.Z,
		&event.StepsCompleted, &event.TerminalReasonCode, &event.ActorPositionEventHash,
		&event.PreviousEventHash, &event.EventHash,
	)
	if err != nil {
		return cityRealtimeCharacterNavigationPlanEvent{}, err
	}
	if !cityRealtimeCharacterNavigationPlanEventValid(event) {
		return cityRealtimeCharacterNavigationPlanEvent{},
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_navigation_plan_event"})
	}
	return event, nil
}

func loadCityRealtimeCharacterNavigationPlanEvents(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	head cityRealtimeCharacterNavigationPlanHead,
) ([]cityRealtimeCharacterNavigationPlanEvent, error) {
	if worldID <= 0 || !cityRealtimeCharacterNavigationPlanHeadValid(head) {
		return nil, ErrCityInvalidInput
	}
	rows, err := queryer.QueryContext(ctx, `
SELECT actor_code, navigation_run_code, destination_portal_code,
       destination_x, destination_y, destination_z, source_intent_code,
       event_sequence, frame_sequence, event_type,
       from_x, from_y, from_z, to_x, to_y, to_z,
       steps_completed, terminal_reason_code, actor_position_event_hash,
       previous_event_hash, event_hash
FROM city_realtime_character_navigation_plan_events
WHERE world_id = $1 AND actor_code = $2 AND navigation_run_code = $3
ORDER BY event_sequence ASC`, worldID, head.ActorCode, head.NavigationRunCode)
	if err != nil {
		return nil, fmt.Errorf("load realtime character navigation plan events: %w", err)
	}
	defer func() { _ = rows.Close() }()
	events := make([]cityRealtimeCharacterNavigationPlanEvent, 0, head.PlanRevision)
	for rows.Next() {
		event, scanErr := scanCityRealtimeCharacterNavigationPlanEvent(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		events = append(events, event)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate realtime character navigation plan events: %w", err)
	}
	return events, nil
}

func validateCityRealtimeCharacterNavigationPlanHeadHistory(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	head cityRealtimeCharacterNavigationPlanHead,
) error {
	if !cityRealtimeCharacterNavigationPlanHeadValid(head) {
		return ErrCityInvalidInput
	}
	events, err := loadCityRealtimeCharacterNavigationPlanEvents(ctx, queryer, worldID, head)
	if err != nil {
		return err
	}
	if int64(len(events)) != head.PlanRevision || len(events) == 0 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_navigation_plan_history"})
	}
	genesisHash, err := cityRealtimeCharacterNavigationPlanChainGenesisHash(head)
	if err != nil {
		return err
	}
	previousHash := genesisHash
	previousSteps := int64(0)
	for index, event := range events {
		if event.ActorCode != head.ActorCode || event.NavigationRunCode != head.NavigationRunCode ||
			event.DestinationPortalCode != head.DestinationPortalCode || event.Destination != head.Destination ||
			event.SourceIntentCode != head.SourceIntentCode || event.EventSequence != int64(index+1) ||
			event.PreviousEventHash != previousHash || !cityRealtimeSHA256Hex(event.EventHash) {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_navigation_plan_history"})
		}
		expectedHash, hashErr := cityRealtimeCharacterNavigationPlanEventHash(event)
		if hashErr != nil || expectedHash != event.EventHash {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_navigation_plan_event_hash"}).WithCause(hashErr)
		}
		if index == 0 {
			if event.EventType != cityRealtimeCharacterNavigationPlanEventPlanned || event.FrameSequence != head.AcceptedFrameSequence ||
				event.From != event.To || event.StepsCompleted != 0 || event.TerminalReasonCode != "" ||
				event.ActorPositionEventHash != "" {
				return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_navigation_plan_genesis_event"})
			}
		} else {
			if event.FrameSequence <= events[index-1].FrameSequence || event.StepsCompleted < previousSteps ||
				event.StepsCompleted > previousSteps+1 {
				return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_navigation_plan_sequence"})
			}
			if event.EventType == cityRealtimeCharacterNavigationPlanEventStep {
				if event.StepsCompleted != previousSteps+1 || event.ActorPositionEventHash == "" ||
					!cityRealtimeCharacterAdjacentStep(cityRealtimeActorState{X: event.From.X, Y: event.From.Y, Z: event.From.Z}, event.To) {
					return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_navigation_plan_step"})
				}
			} else if event.EventType == cityRealtimeCharacterNavigationPlanEventArrived {
				if event.ActorPositionEventHash != "" {
					if event.StepsCompleted != previousSteps+1 ||
						!cityRealtimeCharacterAdjacentStep(cityRealtimeActorState{X: event.From.X, Y: event.From.Y, Z: event.From.Z}, event.To) {
						return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_navigation_plan_arrival"})
					}
				} else if event.From != event.To || event.To != head.Destination || event.StepsCompleted != previousSteps {
					return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_navigation_plan_arrival"})
				}
			} else if event.EventType == cityRealtimeCharacterNavigationPlanEventBlocked {
				if event.ActorPositionEventHash != "" {
					if event.TerminalReasonCode != cityRealtimeCharacterNavigationPlanReasonBlockedLimit ||
						event.StepsCompleted != previousSteps+1 ||
						!cityRealtimeCharacterAdjacentStep(cityRealtimeActorState{X: event.From.X, Y: event.From.Y, Z: event.From.Z}, event.To) {
						return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_navigation_plan_terminal"})
					}
				} else if event.From != event.To || event.StepsCompleted != previousSteps {
					return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_navigation_plan_terminal"})
				}
			} else if event.EventType == cityRealtimeCharacterNavigationPlanEventCancelled {
				if event.From != event.To || event.ActorPositionEventHash != "" || event.StepsCompleted != previousSteps {
					return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_navigation_plan_terminal"})
				}
			} else {
				return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_navigation_plan_event_type"})
			}
		}
		previousHash = event.EventHash
		previousSteps = event.StepsCompleted
	}
	last := events[len(events)-1]
	if previousHash != head.EventChainHash || previousSteps != head.StepsCompleted ||
		last.FrameSequence != head.LastFrameSequence {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_navigation_plan_head_chain"})
	}
	if head.PlanStatus == cityRealtimeCharacterNavigationPlanActive {
		if last.EventType != cityRealtimeCharacterNavigationPlanEventPlanned && last.EventType != cityRealtimeCharacterNavigationPlanEventStep {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_navigation_plan_active"})
		}
	} else if last.EventType != cityRealtimeCharacterNavigationPlanTerminalEventType(head.PlanStatus) ||
		last.TerminalReasonCode != head.TerminalReasonCode {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_navigation_plan_terminal"})
	}
	return nil
}

func insertCityRealtimeCharacterNavigationPlanHead(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	head cityRealtimeCharacterNavigationPlanHead,
) error {
	if tx == nil || worldID <= 0 || !cityRealtimeCharacterNavigationPlanHeadValid(head) ||
		head.PlanRevision != 1 || head.PlanStatus != cityRealtimeCharacterNavigationPlanActive ||
		head.StepsCompleted != 0 || head.NextDueWorldTimeUS == nil {
		return ErrCityInvalidInput
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_realtime_character_navigation_plan_heads
    (world_id, actor_code, navigation_run_code, destination_portal_code,
     destination_x, destination_y, destination_z, source_intent_code,
     plan_revision, plan_status, terminal_reason_code, steps_completed,
     maximum_steps, accepted_frame_sequence, last_frame_sequence,
     last_due_world_time_us, next_due_world_time_us, event_chain_hash, state_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14,
        $15, $16, $17, $18, $19, '{}'::jsonb)`,
		worldID, head.ActorCode, head.NavigationRunCode, head.DestinationPortalCode,
		head.Destination.X, head.Destination.Y, head.Destination.Z, head.SourceIntentCode,
		head.PlanRevision, head.PlanStatus, head.TerminalReasonCode, head.StepsCompleted,
		head.MaximumSteps, head.AcceptedFrameSequence, head.LastFrameSequence,
		head.LastDueWorldTimeUS, head.NextDueWorldTimeUS, head.EventChainHash, head.StateHash,
	); err != nil {
		return fmt.Errorf("insert realtime character navigation plan head: %w", err)
	}
	return nil
}

func updateCityRealtimeCharacterNavigationPlanHead(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	previous, next cityRealtimeCharacterNavigationPlanHead,
) error {
	if tx == nil || worldID <= 0 || !cityRealtimeCharacterNavigationPlanHeadValid(previous) ||
		!cityRealtimeCharacterNavigationPlanHeadValid(next) || previous.ActorCode != next.ActorCode ||
		previous.NavigationRunCode != next.NavigationRunCode ||
		next.PlanRevision != previous.PlanRevision+1 {
		return ErrCityInvalidInput
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_realtime_character_navigation_plan_heads
SET plan_revision = $4, plan_status = $5, terminal_reason_code = $6,
    steps_completed = $7, last_frame_sequence = $8, last_due_world_time_us = $9,
    next_due_world_time_us = $10, event_chain_hash = $11, state_hash = $12,
    updated_at = NOW()
WHERE world_id = $1 AND actor_code = $2 AND navigation_run_code = $3
  AND plan_revision = $13 AND plan_status = $14 AND terminal_reason_code = $15
  AND steps_completed = $16 AND last_frame_sequence = $17 AND last_due_world_time_us = $18
  AND next_due_world_time_us IS NOT DISTINCT FROM $19
  AND event_chain_hash = $20 AND state_hash = $21`,
		worldID, next.ActorCode, next.NavigationRunCode, next.PlanRevision, next.PlanStatus,
		next.TerminalReasonCode, next.StepsCompleted, next.LastFrameSequence, next.LastDueWorldTimeUS,
		next.NextDueWorldTimeUS, next.EventChainHash, next.StateHash,
		previous.PlanRevision, previous.PlanStatus, previous.TerminalReasonCode,
		previous.StepsCompleted, previous.LastFrameSequence, previous.LastDueWorldTimeUS,
		previous.NextDueWorldTimeUS, previous.EventChainHash, previous.StateHash,
	)
	if err != nil {
		return fmt.Errorf("advance realtime character navigation plan head: %w", err)
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr != nil {
		return fmt.Errorf("check realtime character navigation plan update: %w", rowsErr)
	} else if rows != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_navigation_plan_revision"})
	}
	return nil
}

func insertCityRealtimeCharacterNavigationPlanEvent(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	event cityRealtimeCharacterNavigationPlanEvent,
) error {
	if tx == nil || worldID <= 0 || !cityRealtimeCharacterNavigationPlanEventValid(event) {
		return ErrCityInvalidInput
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_realtime_character_navigation_plan_events
    (world_id, actor_code, navigation_run_code, destination_portal_code,
     destination_x, destination_y, destination_z, source_intent_code,
     event_sequence, frame_sequence, event_type,
     from_x, from_y, from_z, to_x, to_y, to_z,
     steps_completed, terminal_reason_code, actor_position_event_hash,
     previous_event_hash, event_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11,
        $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, '{}'::jsonb)`,
		worldID, event.ActorCode, event.NavigationRunCode, event.DestinationPortalCode,
		event.Destination.X, event.Destination.Y, event.Destination.Z, event.SourceIntentCode,
		event.EventSequence, event.FrameSequence, event.EventType,
		event.From.X, event.From.Y, event.From.Z, event.To.X, event.To.Y, event.To.Z,
		event.StepsCompleted, event.TerminalReasonCode, event.ActorPositionEventHash,
		event.PreviousEventHash, event.EventHash,
	); err != nil {
		return fmt.Errorf("append realtime character navigation plan event: %w", err)
	}
	return nil
}

func cityRealtimeCharacterNavigationPlanDueDedupKey(head cityRealtimeCharacterNavigationPlanHead) (string, error) {
	if !cityRealtimeCharacterNavigationPlanHeadValid(head) || head.PlanStatus != cityRealtimeCharacterNavigationPlanActive ||
		head.NextDueWorldTimeUS == nil {
		return "", ErrCityInvalidInput
	}
	key := "navigation.step." + cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		"city-realtime-character-navigation-plan-dedup-v1",
		head.ActorCode,
		head.NavigationRunCode,
		strconv.FormatInt(head.PlanRevision, 10),
		strconv.FormatInt(*head.NextDueWorldTimeUS, 10),
	}, "\x1f")))
	if !cityRealtimeDueEventIdentifierValid(key, 160) {
		return "", ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_navigation_dedup"})
	}
	return key, nil
}

func cityRealtimeCharacterNavigationPlanAggregateKey(head cityRealtimeCharacterNavigationPlanHead) (string, error) {
	if !cityRealtimeCharacterNavigationPlanHeadValid(head) {
		return "", ErrCityInvalidInput
	}
	key := "navigation.aggregate." + cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		"city-realtime-character-navigation-plan-aggregate-v1",
		head.ActorCode,
		head.NavigationRunCode,
	}, "\x1f")))
	if !cityRealtimeDueEventIdentifierValid(key, 160) {
		return "", ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_navigation_aggregate"})
	}
	return key, nil
}

func scheduleCityRealtimeCharacterNavigationPlanStepDueEvent(
	ctx context.Context,
	tx *sql.Tx,
	worldID, createdFrameSequence int64,
	head cityRealtimeCharacterNavigationPlanHead,
) error {
	if tx == nil || worldID <= 0 || createdFrameSequence <= 0 ||
		!cityRealtimeCharacterNavigationPlanHeadValid(head) ||
		head.PlanStatus != cityRealtimeCharacterNavigationPlanActive || head.NextDueWorldTimeUS == nil {
		return ErrCityInvalidInput
	}
	dedupKey, err := cityRealtimeCharacterNavigationPlanDueDedupKey(head)
	if err != nil {
		return err
	}
	aggregateKey, err := cityRealtimeCharacterNavigationPlanAggregateKey(head)
	if err != nil {
		return err
	}
	payload, payloadHash, err := cityRealtimeCanonicalJSONObject(map[string]any{
		"schema_version":          cityRealtimeCharacterNavigationPlanSchemaVersion,
		"actor_code":              head.ActorCode,
		"navigation_run_code":     head.NavigationRunCode,
		"destination_portal_code": head.DestinationPortalCode,
		"plan_revision":           head.PlanRevision,
	})
	if err != nil {
		return fmt.Errorf("canonicalize realtime character navigation step payload: %w", err)
	}
	// Traffic capacity is a server-only runtime augmentation. Historical 1.13
	// worlds without its immutable binding retain their original navigation
	// semantics; newly created worlds receive a deterministic reservation due
	// event that runs before this movement boundary.
	trafficRuntime, trafficErr := loadCityRealtimeCharacterTrafficReservationRuntime(ctx, tx, worldID)
	if trafficErr != nil {
		return trafficErr
	}
	if trafficRuntime != nil {
		if trafficErr = scheduleCityRealtimeCharacterTrafficReservationDueEvent(
			ctx, tx, worldID, createdFrameSequence, head, trafficRuntime,
		); trafficErr != nil {
			return trafficErr
		}
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_due_events
    (world_id, event_type, schema_version, due_world_time_us, temporal_phase,
     priority, aggregate_type, aggregate_key, dedup_key, source_kind,
     source_reference, payload, payload_hash, expected_version, status,
     created_frame_sequence)
VALUES ($1, $2, 1, $3, 'movement', $4, 'realtime_character_navigation', $5, $6, 'system',
        'realtime_character_navigation_plan', $7::jsonb, $8, $9, 'pending', $10)`,
		worldID, cityRealtimeDueEventTypeCharacterNavigationStep, *head.NextDueWorldTimeUS,
		cityRealtimeCharacterNavigationPlanDuePriority, aggregateKey, dedupKey, []byte(payload), payloadHash,
		head.PlanRevision, createdFrameSequence,
	); err != nil {
		return fmt.Errorf("schedule realtime character navigation step: %w", err)
	}
	return nil
}

func decodeCityRealtimeCharacterNavigationPlanDuePayload(
	event cityRealtimeDueEventRecord,
) (cityRealtimeCharacterNavigationPlanDuePayload, bool) {
	payload := cityRealtimeCharacterNavigationPlanDuePayload{}
	if err := decodeStrictCityObject(event.Payload, &payload); err != nil ||
		payload.SchemaVersion != cityRealtimeCharacterNavigationPlanSchemaVersion ||
		!cityRealtimePlayerActorCodeValid(payload.ActorCode) ||
		!cityRealtimeAgentIdentifierValid(payload.NavigationRunCode, 96) ||
		!cityRealtimeDueEventIdentifierValid(payload.DestinationPortalCode, 128) || payload.PlanRevision <= 0 {
		return cityRealtimeCharacterNavigationPlanDuePayload{}, false
	}
	_, payloadHash, err := cityRealtimeCanonicalJSONObject(map[string]any{
		"schema_version":          payload.SchemaVersion,
		"actor_code":              payload.ActorCode,
		"navigation_run_code":     payload.NavigationRunCode,
		"destination_portal_code": payload.DestinationPortalCode,
		"plan_revision":           payload.PlanRevision,
	})
	if err != nil || payloadHash != event.PayloadHash {
		return cityRealtimeCharacterNavigationPlanDuePayload{}, false
	}
	return payload, true
}

// cityRealtimeNavigationSurfaceCache bounds a single finite route search to
// its touched static chunks.  It deliberately reuses the same terrain and
// structure semantics as manual surface movement; only the cache lifetime is
// different.  // ponytail: replace bounded local BFS with an immutable sector
// routing graph only when multi-sector traffic/reservations have a real data
// model, rather than pre-optimising a 32-cell autonomous plan.
type cityRealtimeNavigationSurfaceCache struct {
	ctx     context.Context
	queryer citySQLQueryer
	worldID int64
	chunks  map[cityRealtimeNavigationChunkKey]cityspatial.OpenWorldChunkPayload
}

type cityRealtimeNavigationChunkKey struct {
	X int64
	Y int64
	Z int32
}

func newCityRealtimeNavigationSurfaceCache(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) *cityRealtimeNavigationSurfaceCache {
	return &cityRealtimeNavigationSurfaceCache{
		ctx: ctx, queryer: queryer, worldID: worldID,
		chunks: make(map[cityRealtimeNavigationChunkKey]cityspatial.OpenWorldChunkPayload),
	}
}

func (cache *cityRealtimeNavigationSurfaceCache) traversable(position cityRealtimeActorSpawnCandidate) (bool, error) {
	if cache == nil || cache.queryer == nil || cache.worldID <= 0 || position.Z != cityspatial.SurfaceZ {
		return false, ErrCityInvalidInput
	}
	address, err := cityspatial.SplitWorldCoordinate(cityspatial.WorldCoordinate{
		X: position.X, Y: position.Y, Z: position.Z,
	}, cityspatial.DefaultChunkSize)
	if err != nil {
		return false, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "destination"}).WithCause(err)
	}
	key := cityRealtimeNavigationChunkKey{X: address.Chunk.X, Y: address.Chunk.Y, Z: address.Chunk.Z}
	payload, found := cache.chunks[key]
	if !found {
		var rawPayload []byte
		err = cache.queryer.QueryRowContext(cache.ctx, `
SELECT payload
FROM city_realtime_spatial_chunks
WHERE world_id = $1 AND chunk_x = $2 AND chunk_y = $3 AND z = $4`,
			cache.worldID, key.X, key.Y, key.Z,
		).Scan(&rawPayload)
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("load realtime navigation chunk: %w", err)
		}
		if err = json.Unmarshal(rawPayload, &payload); err != nil {
			return false, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_navigation_payload"}).WithCause(err)
		}
		if err = cityspatial.ValidateOpenWorldChunkPayload(payload); err != nil {
			return false, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_navigation_payload"}).WithCause(err)
		}
		cache.chunks[key] = payload
	}
	cellIndex := int(address.Local.Y)*payload.Width + int(address.Local.X)
	if cellIndex < 0 || cellIndex >= payload.Width*payload.Height {
		return false, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_navigation_cell"})
	}
	for _, layer := range payload.Layers {
		if int(layer.X) == int(address.Local.X) && int(layer.Y) == int(address.Local.Y) && layer.Kind == cityspatial.RuleKindStructure {
			return false, nil
		}
	}
	terrainID, ok := cityRealtimeTerrainDefinitionAt(payload.TerrainRuns, cellIndex)
	if !ok {
		return false, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_navigation_terrain"})
	}
	switch terrainID {
	case "terrain.road", "terrain.sidewalk", "terrain.grass", "terrain.ground", "terrain.soil":
		return true, nil
	default:
		return false, nil
	}
}

func cityRealtimeCharacterNavigationNeighborCandidates(
	position cityRealtimeActorSpawnCandidate,
) []cityRealtimeActorSpawnCandidate {
	items := make([]cityRealtimeActorSpawnCandidate, 0, 4)
	if position.X > math.MinInt64 {
		items = append(items, cityRealtimeActorSpawnCandidate{X: position.X - 1, Y: position.Y, Z: position.Z})
	}
	if position.Y > math.MinInt64 {
		items = append(items, cityRealtimeActorSpawnCandidate{X: position.X, Y: position.Y - 1, Z: position.Z})
	}
	if position.Y < math.MaxInt64 {
		items = append(items, cityRealtimeActorSpawnCandidate{X: position.X, Y: position.Y + 1, Z: position.Z})
	}
	if position.X < math.MaxInt64 {
		items = append(items, cityRealtimeActorSpawnCandidate{X: position.X + 1, Y: position.Y, Z: position.Z})
	}
	return items
}

func cityRealtimeCharacterNavigationBoundedCoordinate(value, delta int64) int64 {
	if delta < 0 && value < math.MinInt64-delta {
		return math.MinInt64
	}
	if delta > 0 && value > math.MaxInt64-delta {
		return math.MaxInt64
	}
	return value + delta
}

func loadCityRealtimeCharacterNavigationOccupiedSurfaceCells(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	excludedActorCode string,
	origin cityRealtimeActorSpawnCandidate,
	maximumSteps int64,
) (map[cityRealtimeActorSpawnCandidate]struct{}, error) {
	if worldID <= 0 || !cityRealtimeCharacterNavigationPlanDestinationValid(origin) ||
		maximumSteps < 0 || (excludedActorCode != "" && !cityRealtimeAgentIdentifierValid(excludedActorCode, 96)) {
		return nil, ErrCityInvalidInput
	}
	minimumX := cityRealtimeCharacterNavigationBoundedCoordinate(origin.X, -maximumSteps)
	maximumX := cityRealtimeCharacterNavigationBoundedCoordinate(origin.X, maximumSteps)
	minimumY := cityRealtimeCharacterNavigationBoundedCoordinate(origin.Y, -maximumSteps)
	maximumY := cityRealtimeCharacterNavigationBoundedCoordinate(origin.Y, maximumSteps)
	rows, err := queryer.QueryContext(ctx, `
SELECT state.x, state.y, state.z
FROM city_realtime_actor_states state
JOIN city_realtime_actor_identities identity
  ON identity.world_id = state.world_id AND identity.actor_code = state.actor_code
WHERE state.world_id = $1 AND state.z = $2
  AND ($3 = '' OR state.actor_code <> $3)
  AND identity.lifecycle_status = 'active'
  AND state.x BETWEEN $4 AND $5
  AND state.y BETWEEN $6 AND $7`,
		worldID, cityspatial.SurfaceZ, excludedActorCode, minimumX, maximumX, minimumY, maximumY,
	)
	if err != nil {
		return nil, fmt.Errorf("load realtime character navigation occupancy: %w", err)
	}
	defer func() { _ = rows.Close() }()
	occupied := make(map[cityRealtimeActorSpawnCandidate]struct{})
	for rows.Next() {
		position := cityRealtimeActorSpawnCandidate{}
		if scanErr := rows.Scan(&position.X, &position.Y, &position.Z); scanErr != nil {
			return nil, scanErr
		}
		if !cityRealtimeCharacterNavigationPlanDestinationValid(position) {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_navigation_occupancy"})
		}
		occupied[position] = struct{}{}
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate realtime character navigation occupancy: %w", err)
	}
	return occupied, nil
}

func loadCityRealtimeCharacterNavigationDestinationPortals(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	origin cityRealtimeActorSpawnCandidate,
) ([]cityRealtimeCharacterNavigationDestination, error) {
	if worldID <= 0 || !cityRealtimeCharacterNavigationPlanDestinationValid(origin) {
		return nil, ErrCityInvalidInput
	}
	// Cast to numeric before subtraction so a malformed signed coordinate can
	// never overflow the planner's SQL ordering expression.
	rows, err := queryer.QueryContext(ctx, `
SELECT code, building_code, portal_type, from_floor_index, to_floor_index,
       from_x, from_y, from_z, to_x, to_y, to_z, bidirectional,
       topology_hash, revision
FROM city_realtime_spatial_portals
WHERE world_id = $1 AND portal_type = 'entrance' AND from_z = 0
ORDER BY abs(from_x::NUMERIC - $2::NUMERIC) + abs(from_y::NUMERIC - $3::NUMERIC) ASC,
         code ASC
LIMIT $4`, worldID, origin.X, origin.Y, cityRealtimeCharacterNavigationPlanPortalSearchLimit,
	)
	if err != nil {
		return nil, fmt.Errorf("load realtime character navigation destinations: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]cityRealtimeCharacterNavigationDestination, 0)
	seenPortal := make(map[string]struct{})
	for rows.Next() {
		portal, scanErr := scanCityRealtimeCharacterPortal(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		if portal.PortalType != "entrance" || portal.From.Z != cityspatial.SurfaceZ || portal.To.Z != cityspatial.SurfaceZ {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_navigation_portal"})
		}
		if _, duplicate := seenPortal[portal.Code]; duplicate {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_navigation_portal_duplicate"})
		}
		seenPortal[portal.Code] = struct{}{}
		items = append(items, cityRealtimeCharacterNavigationDestination{
			PortalCode: portal.Code,
			Target:     portal.From,
		})
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate realtime character navigation destinations: %w", err)
	}
	return items, nil
}

type cityRealtimeCharacterNavigationSearchNode struct {
	Position cityRealtimeActorSpawnCandidate
	Steps    int64
}

func cityRealtimeCharacterNavigationFindPath(
	cache *cityRealtimeNavigationSurfaceCache,
	origin, destination cityRealtimeActorSpawnCandidate,
	occupied map[cityRealtimeActorSpawnCandidate]struct{},
	maximumSteps int64,
) ([]cityRealtimeActorSpawnCandidate, bool, error) {
	if cache == nil || !cityRealtimeCharacterNavigationPlanDestinationValid(origin) ||
		!cityRealtimeCharacterNavigationPlanDestinationValid(destination) || maximumSteps <= 0 ||
		maximumSteps > cityRealtimeCharacterNavigationPlanMaximumSteps {
		return nil, false, ErrCityInvalidInput
	}
	if origin == destination {
		return []cityRealtimeActorSpawnCandidate{origin}, true, nil
	}
	queue := []cityRealtimeCharacterNavigationSearchNode{{Position: origin, Steps: 0}}
	visited := map[cityRealtimeActorSpawnCandidate]struct{}{origin: {}}
	parents := make(map[cityRealtimeActorSpawnCandidate]cityRealtimeActorSpawnCandidate)
	expanded := 0
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		expanded++
		if expanded > cityRealtimeCharacterNavigationPlanMaximumExpanded {
			return nil, false, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_navigation_search_budget"})
		}
		if current.Steps >= maximumSteps {
			continue
		}
		for _, candidate := range cityRealtimeCharacterNavigationNeighborCandidates(current.Position) {
			if _, seen := visited[candidate]; seen {
				continue
			}
			if _, blocked := occupied[candidate]; blocked {
				continue
			}
			traversable, traverseErr := cache.traversable(candidate)
			if traverseErr != nil {
				return nil, false, traverseErr
			}
			if !traversable {
				continue
			}
			visited[candidate] = struct{}{}
			parents[candidate] = current.Position
			nextSteps := current.Steps + 1
			if candidate == destination {
				path := []cityRealtimeActorSpawnCandidate{candidate}
				for cursor := candidate; cursor != origin; {
					parent, found := parents[cursor]
					if !found {
						return nil, false, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_navigation_path"})
					}
					path = append(path, parent)
					cursor = parent
				}
				for left, right := 0, len(path)-1; left < right; left, right = left+1, right-1 {
					path[left], path[right] = path[right], path[left]
				}
				return path, true, nil
			}
			queue = append(queue, cityRealtimeCharacterNavigationSearchNode{Position: candidate, Steps: nextSteps})
		}
	}
	return nil, false, nil
}

func cityRealtimeCharacterAvailableNavigationDestinations(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	actorCode string,
	actorState cityRealtimeActorState,
	agentBinding cityRealtimeAgentPolicyBinding,
) ([]cityRealtimeCharacterNavigationDestination, error) {
	if worldID <= 0 || !cityRealtimePlayerActorCodeValid(actorCode) || actorState.ActorCode != actorCode ||
		!cityRealtimeActorStateValid(actorState) || actorState.Z != cityspatial.SurfaceZ ||
		!cityRealtimeCharacterNavigationPlanRuntimeEnabled(agentBinding) {
		return nil, ErrCityInvalidInput
	}
	runtime, err := loadCityRealtimeCharacterNavigationPlanRuntime(ctx, queryer, worldID)
	if err != nil {
		return nil, err
	}
	if runtime == nil || runtime.Binding.AgentBindingHash != agentBinding.BindingHash {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_navigation_runtime"})
	}
	if _, active, activeErr := loadCityRealtimeCharacterActiveNavigationPlan(ctx, queryer, worldID, actorCode, false); activeErr != nil {
		return nil, activeErr
	} else if active {
		return []cityRealtimeCharacterNavigationDestination{}, nil
	}
	origin := cityRealtimeActorSpawnCandidate{X: actorState.X, Y: actorState.Y, Z: actorState.Z}
	destinationPortals, err := loadCityRealtimeCharacterNavigationDestinationPortals(ctx, queryer, worldID, origin)
	if err != nil {
		return nil, err
	}
	occupied, err := loadCityRealtimeCharacterNavigationOccupiedSurfaceCells(
		ctx, queryer, worldID, actorCode, origin, cityRealtimeCharacterNavigationPlanMaximumSteps,
	)
	if err != nil {
		return nil, err
	}
	cache := newCityRealtimeNavigationSurfaceCache(ctx, queryer, worldID)
	items := make([]cityRealtimeCharacterNavigationDestination, 0, cityRealtimeCharacterNavigationPlanMaximumCandidates)
	for _, destination := range destinationPortals {
		if destination.Target == origin {
			continue
		}
		path, found, searchErr := cityRealtimeCharacterNavigationFindPath(
			cache, origin, destination.Target, occupied, cityRealtimeCharacterNavigationPlanMaximumSteps,
		)
		if searchErr != nil {
			return nil, searchErr
		}
		if !found || len(path) < 2 {
			continue
		}
		destination.PathLength = int64(len(path) - 1)
		items = append(items, destination)
	}
	sort.Slice(items, func(left, right int) bool {
		if items[left].PathLength != items[right].PathLength {
			return items[left].PathLength < items[right].PathLength
		}
		return items[left].PortalCode < items[right].PortalCode
	})
	if len(items) > cityRealtimeCharacterNavigationPlanMaximumCandidates {
		items = items[:cityRealtimeCharacterNavigationPlanMaximumCandidates]
	}
	return items, nil
}

func cityRealtimeCharacterAvailableNavigationDestinationPortalCodes(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	actorCode string,
	actorState cityRealtimeActorState,
	agentBinding cityRealtimeAgentPolicyBinding,
) ([]string, error) {
	destinations, err := cityRealtimeCharacterAvailableNavigationDestinations(
		ctx, queryer, worldID, actorCode, actorState, agentBinding,
	)
	if err != nil {
		return nil, err
	}
	codes := make([]string, 0, len(destinations))
	for _, destination := range destinations {
		codes = append(codes, destination.PortalCode)
	}
	sort.Strings(codes)
	return codes, nil
}

func cityRealtimeCharacterNavigationDestinationByPortalCode(
	items []cityRealtimeCharacterNavigationDestination,
	portalCode string,
) (cityRealtimeCharacterNavigationDestination, bool) {
	for _, item := range items {
		if item.PortalCode == portalCode {
			return item, true
		}
	}
	return cityRealtimeCharacterNavigationDestination{}, false
}

func cityRealtimeAgentDecisionNavigationPortalCodeFromArguments(arguments map[string]any) (string, error) {
	if len(arguments) != 1 {
		return "", ErrCityRealtimeAgentDecisionUnavailable.WithMetadata(map[string]string{"field": "action_arguments"})
	}
	rawCode, found := arguments["destination_portal_code"]
	portalCode, ok := rawCode.(string)
	portalCode = strings.TrimSpace(portalCode)
	if !found || !ok || !cityRealtimeDueEventIdentifierValid(portalCode, 128) {
		return "", ErrCityRealtimeAgentDecisionUnavailable.WithMetadata(map[string]string{"field": "action_arguments"})
	}
	return portalCode, nil
}

func cityRealtimeAgentDecisionNavigationPortalCodeFromRawArguments(arguments json.RawMessage) (string, error) {
	decoded := make(map[string]any)
	if err := json.Unmarshal(arguments, &decoded); err != nil {
		return "", ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_intent_arguments"}).WithCause(err)
	}
	return cityRealtimeAgentDecisionNavigationPortalCodeFromArguments(decoded)
}

func loadCityRealtimeCharacterNavigationRecordForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	actorCode string,
) (cityRealtimeCharacterRecord, bool, error) {
	if tx == nil || worldID <= 0 || !cityRealtimePlayerActorCodeValid(actorCode) {
		return cityRealtimeCharacterRecord{}, false, ErrCityInvalidInput
	}
	identity := cityRealtimeActorIdentity{}
	state := cityRealtimeActorState{}
	err := tx.QueryRowContext(ctx, `
SELECT identity.actor_code, identity.actor_kind, identity.public_label,
       identity.appearance_variant, identity.lifecycle_status,
       identity.spawn_x, identity.spawn_y, identity.spawn_z,
       identity.spawn_frame_sequence, identity.identity_hash,
       state.x, state.y, state.z, state.motion_state, state.position_revision,
       state.last_frame_sequence, state.state_hash, state.event_chain_hash
FROM city_realtime_actor_identities identity
JOIN city_realtime_actor_states state
  ON state.world_id = identity.world_id AND state.actor_code = identity.actor_code
WHERE identity.world_id = $1 AND identity.actor_code = $2
FOR UPDATE OF state`, worldID, actorCode).Scan(
		&identity.ActorCode, &identity.ActorKind, &identity.PublicLabel, &identity.AppearanceVariant,
		&identity.LifecycleStatus, &identity.SpawnX, &identity.SpawnY, &identity.SpawnZ,
		&identity.SpawnFrameSequence, &identity.IdentityHash,
		&state.X, &state.Y, &state.Z, &state.MotionState, &state.PositionRevision,
		&state.LastFrameSequence, &state.StateHash, &state.EventChainHash,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return cityRealtimeCharacterRecord{}, false, nil
	}
	if err != nil {
		return cityRealtimeCharacterRecord{}, false, fmt.Errorf("lock realtime character navigation actor: %w", err)
	}
	state.ActorCode = identity.ActorCode
	expectedStateHash, hashErr := cityRealtimeActorStateHash(state)
	if !cityRealtimeActorIdentityValid(identity) || !cityRealtimeActorStateValid(state) ||
		hashErr != nil || state.StateHash != expectedStateHash {
		return cityRealtimeCharacterRecord{}, false,
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_navigation_actor"})
	}
	return cityRealtimeCharacterRecord{identity: identity, state: state}, true, nil
}

func cityRealtimeCharacterNavigationAgentForActor(
	state *cityRealtimeAgentHashState,
	actorCode string,
) (cityRealtimeAgentInstance, bool) {
	if state == nil || !cityRealtimePlayerActorCodeValid(actorCode) {
		return cityRealtimeAgentInstance{}, false
	}
	var result cityRealtimeAgentInstance
	found := false
	for _, agent := range state.Agents {
		if agent.AgentSubtype != "character.user" || agent.ActorCode == nil || *agent.ActorCode != actorCode {
			continue
		}
		if found {
			return cityRealtimeAgentInstance{}, false
		}
		result = agent
		found = true
	}
	return result, found
}

func cityRealtimeCharacterNavigationPlanCancel(
	head cityRealtimeCharacterNavigationPlanHead,
	frameSequence, currentWorldTimeUS int64,
	position cityRealtimeActorSpawnCandidate,
) (cityRealtimeCharacterNavigationPlanHead, cityRealtimeCharacterNavigationPlanEvent, error) {
	if !cityRealtimeCharacterNavigationPlanHeadValid(head) || head.PlanStatus != cityRealtimeCharacterNavigationPlanActive ||
		frameSequence <= head.LastFrameSequence || currentWorldTimeUS < head.LastDueWorldTimeUS ||
		currentWorldTimeUS%cityRealtimeTimeQuantumUS != 0 || !cityRealtimeCharacterNavigationPlanDestinationValid(position) {
		return cityRealtimeCharacterNavigationPlanHead{}, cityRealtimeCharacterNavigationPlanEvent{}, ErrCityInvalidInput
	}
	next := head
	next.PlanRevision++
	next.PlanStatus = cityRealtimeCharacterNavigationPlanCancelled
	next.TerminalReasonCode = cityRealtimeCharacterNavigationPlanReasonCancelledControl
	next.LastFrameSequence = frameSequence
	next.LastDueWorldTimeUS = currentWorldTimeUS
	next.NextDueWorldTimeUS = nil
	event := cityRealtimeCharacterNavigationPlanEvent{
		ActorCode:             next.ActorCode,
		NavigationRunCode:     next.NavigationRunCode,
		DestinationPortalCode: next.DestinationPortalCode,
		Destination:           next.Destination,
		SourceIntentCode:      next.SourceIntentCode,
		EventSequence:         next.PlanRevision,
		FrameSequence:         frameSequence,
		EventType:             cityRealtimeCharacterNavigationPlanEventCancelled,
		From:                  position,
		To:                    position,
		StepsCompleted:        next.StepsCompleted,
		TerminalReasonCode:    next.TerminalReasonCode,
		PreviousEventHash:     head.EventChainHash,
	}
	var err error
	if event.EventHash, err = cityRealtimeCharacterNavigationPlanEventHash(event); err != nil {
		return cityRealtimeCharacterNavigationPlanHead{}, cityRealtimeCharacterNavigationPlanEvent{}, err
	}
	next.EventChainHash = event.EventHash
	next.StateHash = cityRealtimeCharacterNavigationPlanStateHashUnchecked(next)
	if !cityRealtimeCharacterNavigationPlanHeadValid(next) || !cityRealtimeCharacterNavigationPlanEventValid(event) {
		return cityRealtimeCharacterNavigationPlanHead{}, cityRealtimeCharacterNavigationPlanEvent{},
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_navigation_plan_cancel"})
	}
	return next, event, nil
}

func applyCityRealtimeCharacterNavigationPlanStepDueEvent(
	ctx context.Context,
	tx *sql.Tx,
	worldID, frameSequence int64,
	event cityRealtimeDueEventRecord,
) (bool, error) {
	if tx == nil || worldID <= 0 || frameSequence <= 0 ||
		event.EventType != cityRealtimeDueEventTypeCharacterNavigationStep ||
		event.SchemaVersion != cityRealtimeCharacterNavigationPlanSchemaVersion ||
		event.SourceKind != cityRealtimeDueEventSourceKindSystem || event.TemporalPhase != "movement" ||
		event.AggregateType != "realtime_character_navigation" ||
		event.SourceReference != "realtime_character_navigation_plan" || event.ExpectedVersion == nil {
		return false, nil
	}
	payload, validPayload := decodeCityRealtimeCharacterNavigationPlanDuePayload(event)
	if !validPayload || *event.ExpectedVersion != payload.PlanRevision {
		return false, nil
	}
	head, found, err := loadCityRealtimeCharacterNavigationPlanHead(
		ctx, tx, worldID, payload.ActorCode, payload.NavigationRunCode, true,
	)
	if err != nil {
		return false, err
	}
	if !found || head.PlanStatus != cityRealtimeCharacterNavigationPlanActive ||
		head.PlanRevision != payload.PlanRevision || head.DestinationPortalCode != payload.DestinationPortalCode ||
		head.NextDueWorldTimeUS == nil || *head.NextDueWorldTimeUS != event.DueWorldTimeUS {
		return false, nil
	}
	expectedAggregateKey, aggregateErr := cityRealtimeCharacterNavigationPlanAggregateKey(head)
	expectedDedupKey, dedupErr := cityRealtimeCharacterNavigationPlanDueDedupKey(head)
	if aggregateErr != nil || dedupErr != nil || event.AggregateKey != expectedAggregateKey || event.DedupKey != expectedDedupKey {
		return false, nil
	}
	runtime, err := loadCityRealtimeCharacterNavigationPlanRuntime(ctx, tx, worldID)
	if err != nil {
		return false, err
	}
	if runtime == nil {
		return false, nil
	}
	if err = validateCityRealtimeCharacterNavigationPlanHeadHistory(ctx, tx, worldID, head); err != nil {
		return false, err
	}
	agentState, err := loadCityRealtimeAgentHashState(ctx, tx, worldID)
	if err != nil {
		return false, err
	}
	if agentState == nil || agentState.Binding == nil ||
		!cityRealtimeCharacterNavigationPlanRuntimeEnabled(*agentState.Binding) ||
		runtime.Binding.AgentBindingHash != agentState.Binding.BindingHash {
		return false, nil
	}
	record, recordFound, err := loadCityRealtimeCharacterNavigationRecordForUpdate(ctx, tx, worldID, head.ActorCode)
	if err != nil {
		return false, err
	}
	if !recordFound || record.identity.LifecycleStatus != "active" || record.state.Z != cityspatial.SurfaceZ {
		return false, nil
	}
	agent, agentFound := cityRealtimeCharacterNavigationAgentForActor(agentState, head.ActorCode)
	if !agentFound || agent.LifecycleStatus != "active" || agent.ControlMode != "autonomous" {
		next, planEvent, transitionErr := cityRealtimeCharacterNavigationPlanAdvance(
			head, frameSequence, event.DueWorldTimeUS,
			cityRealtimeActorSpawnCandidate{X: record.state.X, Y: record.state.Y, Z: record.state.Z},
			cityRealtimeActorSpawnCandidate{X: record.state.X, Y: record.state.Y, Z: record.state.Z},
			"", cityRealtimeCharacterNavigationPlanCancelled, cityRealtimeCharacterNavigationPlanReasonCancelledControl,
		)
		if transitionErr != nil {
			return false, transitionErr
		}
		if gateErr := enableCityRealtimeCharacterMutationGates(ctx, tx, worldID, frameSequence, true); gateErr != nil {
			return false, gateErr
		}
		if gateErr := enableCityRealtimeCharacterNavigationPlanMutationGate(ctx, tx, worldID, frameSequence, event.DueWorldTimeUS); gateErr != nil {
			return false, gateErr
		}
		if updateErr := updateCityRealtimeCharacterNavigationPlanHead(ctx, tx, worldID, head, next); updateErr != nil {
			return false, updateErr
		}
		if insertErr := insertCityRealtimeCharacterNavigationPlanEvent(ctx, tx, worldID, planEvent); insertErr != nil {
			return false, insertErr
		}
		return true, nil
	}
	portal, portalFound, portalErr := loadCityRealtimeCharacterPortal(ctx, tx, worldID, head.DestinationPortalCode)
	if portalErr != nil {
		return false, portalErr
	}
	if !portalFound || portal.PortalType != "entrance" || portal.From != head.Destination || portal.From.Z != cityspatial.SurfaceZ {
		return false, nil
	}
	origin := cityRealtimeActorSpawnCandidate{X: record.state.X, Y: record.state.Y, Z: record.state.Z}
	occupied, occupancyErr := loadCityRealtimeCharacterNavigationOccupiedSurfaceCells(
		ctx, tx, worldID, head.ActorCode, origin, cityRealtimeCharacterNavigationPlanMaximumSteps,
	)
	if occupancyErr != nil {
		return false, occupancyErr
	}
	cache := newCityRealtimeNavigationSurfaceCache(ctx, tx, worldID)
	path, pathFound, pathErr := cityRealtimeCharacterNavigationFindPath(
		cache, origin, head.Destination, occupied, cityRealtimeCharacterNavigationPlanMaximumSteps-head.StepsCompleted,
	)
	if pathErr != nil {
		return false, pathErr
	}
	var next cityRealtimeCharacterNavigationPlanHead
	var planEvent cityRealtimeCharacterNavigationPlanEvent
	if !pathFound {
		reason := cityRealtimeCharacterNavigationPlanReasonBlockedPath
		if _, destinationOccupied := occupied[head.Destination]; destinationOccupied {
			reason = cityRealtimeCharacterNavigationPlanReasonBlockedOccupied
		}
		next, planEvent, err = cityRealtimeCharacterNavigationPlanAdvance(
			head, frameSequence, event.DueWorldTimeUS, origin, origin, "",
			cityRealtimeCharacterNavigationPlanBlocked, reason,
		)
		if err != nil {
			return false, err
		}
	} else if len(path) == 1 {
		next, planEvent, err = cityRealtimeCharacterNavigationPlanAdvance(
			head, frameSequence, event.DueWorldTimeUS, origin, origin, "",
			cityRealtimeCharacterNavigationPlanArrived, cityRealtimeCharacterNavigationPlanReasonArrived,
		)
		if err != nil {
			return false, err
		}
	} else {
		target := path[1]
		motionState, traversable, motionErr := cityRealtimeCharacterWalkMotionState(ctx, tx, worldID, record.state, target)
		if motionErr != nil {
			return false, motionErr
		}
		if !traversable || motionState != "walking" {
			next, planEvent, err = cityRealtimeCharacterNavigationPlanAdvance(
				head, frameSequence, event.DueWorldTimeUS, origin, origin, "",
				cityRealtimeCharacterNavigationPlanBlocked, cityRealtimeCharacterNavigationPlanReasonBlockedPath,
			)
			if err != nil {
				return false, err
			}
		} else {
			isOccupied, occupiedErr := cityRealtimeActorPositionOccupied(ctx, tx, worldID, head.ActorCode, target)
			if occupiedErr != nil {
				return false, occupiedErr
			}
			if isOccupied {
				next, planEvent, err = cityRealtimeCharacterNavigationPlanAdvance(
					head, frameSequence, event.DueWorldTimeUS, origin, origin, "",
					cityRealtimeCharacterNavigationPlanBlocked, cityRealtimeCharacterNavigationPlanReasonBlockedOccupied,
				)
				if err != nil {
					return false, err
				}
			} else {
				trafficRuntime, trafficErr := loadCityRealtimeCharacterTrafficReservationRuntime(ctx, tx, worldID)
				if trafficErr != nil {
					return false, trafficErr
				}
				trafficGranted := true
				if trafficRuntime != nil {
					reservation, reservationFound, reservationErr := loadCityRealtimeCharacterTrafficReservationHead(
						ctx, tx, worldID, head.ActorCode, head.NavigationRunCode, head.PlanRevision, true,
					)
					if reservationErr != nil {
						return false, reservationErr
					}
					trafficGranted = reservationFound &&
						reservation.ReservationStatus == cityRealtimeCharacterTrafficReservationGranted &&
						reservation.DueWorldTimeUS == event.DueWorldTimeUS &&
						reservation.From == origin && reservation.Target == target
				}
				if !trafficGranted {
					// The exact traffic reason lives in the owner-private reservation
					// ledger. The frozen 1.13 navigation state machine treats any
					// unconsumable shared cell as occupied rather than adding a new
					// public movement outcome to historical navigation plans.
					next, planEvent, err = cityRealtimeCharacterNavigationPlanAdvance(
						head, frameSequence, event.DueWorldTimeUS, origin, origin, "",
						cityRealtimeCharacterNavigationPlanBlocked, cityRealtimeCharacterNavigationPlanReasonBlockedOccupied,
					)
					if err != nil {
						return false, err
					}
				} else {
					if gateErr := enableCityRealtimeCharacterMutationGates(ctx, tx, worldID, frameSequence, true); gateErr != nil {
						return false, gateErr
					}
					if gateErr := enableCityRealtimeCharacterNavigationPlanMutationGate(ctx, tx, worldID, frameSequence, event.DueWorldTimeUS); gateErr != nil {
						return false, gateErr
					}
					if advanceErr := advanceCityRealtimeCharacterPosition(
						ctx, tx, worldID, frameSequence, &record, target, "move", "walking", "",
					); advanceErr != nil {
						return false, advanceErr
					}
					if trafficRuntime != nil {
						consumed, consumeErr := consumeCityRealtimeCharacterTrafficReservation(
							ctx, tx, worldID, frameSequence, event.DueWorldTimeUS, head, origin, target, record.state.EventChainHash,
						)
						if consumeErr != nil {
							return false, consumeErr
						}
						if !consumed {
							return false, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_traffic_reservation_consumption"})
						}
					}
					next, planEvent, err = cityRealtimeCharacterNavigationPlanAdvance(
						head, frameSequence, event.DueWorldTimeUS, origin, target, record.state.EventChainHash, "", "",
					)
					if err != nil {
						return false, err
					}
				}
			}
		}
	}
	if gateErr := enableCityRealtimeCharacterMutationGates(ctx, tx, worldID, frameSequence, true); gateErr != nil {
		return false, gateErr
	}
	if gateErr := enableCityRealtimeCharacterNavigationPlanMutationGate(ctx, tx, worldID, frameSequence, event.DueWorldTimeUS); gateErr != nil {
		return false, gateErr
	}
	if next.PlanStatus == cityRealtimeCharacterNavigationPlanActive {
		if scheduleErr := scheduleCityRealtimeCharacterNavigationPlanStepDueEvent(ctx, tx, worldID, frameSequence, next); scheduleErr != nil {
			return false, scheduleErr
		}
	}
	if updateErr := updateCityRealtimeCharacterNavigationPlanHead(ctx, tx, worldID, head, next); updateErr != nil {
		return false, updateErr
	}
	if insertErr := insertCityRealtimeCharacterNavigationPlanEvent(ctx, tx, worldID, planEvent); insertErr != nil {
		return false, insertErr
	}
	if next.PlanStatus != cityRealtimeCharacterNavigationPlanActive && agent.ControlMode == "autonomous" {
		if wakeupErr := cityRealtimeAgentScheduleAutonomousActionWakeup(
			ctx, tx, worldID, frameSequence, event.DueWorldTimeUS, cityRealtimeTimeQuantumUS, *agentState.Binding, agent,
		); wakeupErr != nil {
			return false, wakeupErr
		}
	}
	return true, nil
}

// cancelCityRealtimeCharacterActiveNavigationPlan is used only by the
// owner-controlled mode transition.  It seals cancellation in the same frame
// that makes the Agent non-autonomous, while any already-scheduled movement
// event later rejects because the plan no longer remains active.
func cancelCityRealtimeCharacterActiveNavigationPlan(
	ctx context.Context,
	tx *sql.Tx,
	worldID, frameSequence, currentWorldTimeUS int64,
	actorCode string,
) (bool, error) {
	if tx == nil || worldID <= 0 || frameSequence <= 0 || currentWorldTimeUS < 0 ||
		currentWorldTimeUS%cityRealtimeTimeQuantumUS != 0 || !cityRealtimePlayerActorCodeValid(actorCode) {
		return false, ErrCityInvalidInput
	}
	head, found, err := loadCityRealtimeCharacterActiveNavigationPlan(ctx, tx, worldID, actorCode, true)
	if err != nil || !found {
		return false, err
	}
	if err = validateCityRealtimeCharacterNavigationPlanHeadHistory(ctx, tx, worldID, head); err != nil {
		return false, err
	}
	record, recordFound, err := loadCityRealtimeCharacterNavigationRecordForUpdate(ctx, tx, worldID, actorCode)
	if err != nil || !recordFound {
		return false, err
	}
	next, planEvent, err := cityRealtimeCharacterNavigationPlanCancel(
		head, frameSequence, currentWorldTimeUS,
		cityRealtimeActorSpawnCandidate{X: record.state.X, Y: record.state.Y, Z: record.state.Z},
	)
	if err != nil {
		return false, err
	}
	if err = enableCityRealtimeCharacterNavigationPlanMutationGate(ctx, tx, worldID, frameSequence, currentWorldTimeUS); err != nil {
		return false, err
	}
	if err = updateCityRealtimeCharacterNavigationPlanHead(ctx, tx, worldID, head, next); err != nil {
		return false, err
	}
	if err = insertCityRealtimeCharacterNavigationPlanEvent(ctx, tx, worldID, planEvent); err != nil {
		return false, err
	}
	return true, nil
}

func loadCityRealtimeCharacterNavigationPlanHashState(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*cityRealtimeCharacterNavigationPlanHashState, error) {
	runtime, err := loadCityRealtimeCharacterNavigationPlanRuntime(ctx, queryer, worldID)
	if err != nil || runtime == nil {
		return nil, err
	}
	state := &cityRealtimeCharacterNavigationPlanHashState{
		SchemaVersion: cityRealtimeCharacterNavigationPlanSchemaVersion,
		Binding:       &runtime.Binding,
		Heads:         make([]cityRealtimeCharacterNavigationPlanHead, 0),
	}
	rows, err := queryer.QueryContext(ctx, `
SELECT actor_code, navigation_run_code, destination_portal_code,
       destination_x, destination_y, destination_z, source_intent_code,
       plan_revision, plan_status, terminal_reason_code, steps_completed,
       maximum_steps, accepted_frame_sequence, last_frame_sequence,
       last_due_world_time_us, next_due_world_time_us, event_chain_hash, state_hash
FROM city_realtime_character_navigation_plan_heads
WHERE world_id = $1
ORDER BY actor_code ASC, navigation_run_code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load realtime character navigation plan heads: %w", err)
	}
	for rows.Next() {
		head, scanErr := scanCityRealtimeCharacterNavigationPlanHead(rows)
		if scanErr != nil {
			_ = rows.Close()
			return nil, scanErr
		}
		state.Heads = append(state.Heads, head)
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("iterate realtime character navigation plan heads: %w", err)
	}
	if err = rows.Close(); err != nil {
		return nil, fmt.Errorf("close realtime character navigation plan heads: %w", err)
	}
	for _, head := range state.Heads {
		if err = validateCityRealtimeCharacterNavigationPlanHeadHistory(ctx, queryer, worldID, head); err != nil {
			return nil, err
		}
	}
	if err = validateCityRealtimeCharacterNavigationPlanHashState(state); err != nil {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_navigation_plan_state"}).WithCause(err)
	}
	return state, nil
}

func validateCityRealtimeCharacterNavigationPlanHashState(state *cityRealtimeCharacterNavigationPlanHashState) error {
	if state == nil || state.SchemaVersion != cityRealtimeCharacterNavigationPlanSchemaVersion ||
		state.Binding == nil || state.Heads == nil || !cityRealtimeCharacterNavigationPlanBindingValid(*state.Binding) {
		return errors.New("invalid realtime character navigation plan hash state")
	}
	activeByActor := make(map[string]struct{})
	for index, head := range state.Heads {
		if !cityRealtimeCharacterNavigationPlanHeadValid(head) {
			return errors.New("invalid realtime character navigation plan head")
		}
		if index > 0 {
			previous := state.Heads[index-1]
			if previous.ActorCode > head.ActorCode ||
				(previous.ActorCode == head.ActorCode && previous.NavigationRunCode >= head.NavigationRunCode) {
				return errors.New("unordered realtime character navigation plan heads")
			}
		}
		if head.PlanStatus == cityRealtimeCharacterNavigationPlanActive {
			if _, duplicate := activeByActor[head.ActorCode]; duplicate {
				return errors.New("multiple active realtime character navigation plans")
			}
			activeByActor[head.ActorCode] = struct{}{}
		}
	}
	return nil
}

// ListRealtimeCharacterNavigationPlans returns only the caller's own route
// ledger.  There is intentionally no direct planning or cancellation API:
// autonomous Agents may pick only a sealed static portal candidate and mode
// changes are the sole owner authority that may cancel a currently active run.
func (s *CityEconomyService) ListRealtimeCharacterNavigationPlans(
	ctx context.Context,
	input CityRealtimeCharacterNavigationPlanListInput,
) ([]CityRealtimeCharacterNavigationPlan, error) {
	if input.UserID <= 0 || input.WorldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	limit := input.Limit
	if limit == 0 {
		limit = 100
	}
	if limit < 1 || limit > 200 {
		return nil, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "limit"})
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("begin realtime character navigation read transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err = lockCityRealtimeCharacterWorld(ctx, tx, input.UserID, input.WorldID); err != nil {
		return nil, err
	}
	record, found, err := loadCityRealtimeOwnedCharacter(ctx, tx, input.WorldID, input.UserID, false)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrCityRealtimeCharacterNotFound
	}
	runtime, err := loadCityRealtimeCharacterNavigationPlanRuntime(ctx, tx, input.WorldID)
	if err != nil {
		return nil, err
	}
	if runtime == nil {
		return []CityRealtimeCharacterNavigationPlan{}, nil
	}
	rows, err := tx.QueryContext(ctx, `
SELECT actor_code, navigation_run_code, destination_portal_code,
       destination_x, destination_y, destination_z, source_intent_code,
       plan_revision, plan_status, terminal_reason_code, steps_completed,
       maximum_steps, accepted_frame_sequence, last_frame_sequence,
       last_due_world_time_us, next_due_world_time_us, event_chain_hash, state_hash
FROM city_realtime_character_navigation_plan_heads
WHERE world_id = $1 AND actor_code = $2
ORDER BY accepted_frame_sequence DESC, navigation_run_code DESC
LIMIT $3`, input.WorldID, record.identity.ActorCode, limit)
	if err != nil {
		return nil, fmt.Errorf("list realtime character navigation plans: %w", err)
	}
	heads := make([]cityRealtimeCharacterNavigationPlanHead, 0)
	for rows.Next() {
		head, scanErr := scanCityRealtimeCharacterNavigationPlanHead(rows)
		if scanErr != nil {
			_ = rows.Close()
			return nil, scanErr
		}
		heads = append(heads, head)
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("iterate realtime character navigation plans: %w", err)
	}
	if err = rows.Close(); err != nil {
		return nil, fmt.Errorf("close realtime character navigation plans: %w", err)
	}
	items := make([]CityRealtimeCharacterNavigationPlan, 0, len(heads))
	for _, head := range heads {
		if err = validateCityRealtimeCharacterNavigationPlanHeadHistory(ctx, tx, input.WorldID, head); err != nil {
			return nil, err
		}
		items = append(items, cityRealtimeCharacterNavigationPlanProjection(head))
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit realtime character navigation read: %w", err)
	}
	return items, nil
}
