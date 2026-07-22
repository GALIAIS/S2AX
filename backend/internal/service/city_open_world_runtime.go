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
	"strconv"
	"strings"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// V4 commands are intentionally namespaced under open_world.  They share the
// generic city command journal, but cannot accidentally execute against the
// F7 world-runtime tables or command contracts.
const (
	CityCommandTypeOpenWorldActorCreate           = "open_world.actor.create"
	CityCommandTypeOpenWorldActorActivityPerform  = "open_world.actor.activity.perform"
	CityCommandTypeOpenWorldActorRoleTransition   = "open_world.actor.role.transition"
	CityCommandTypeOpenWorldActorMove             = "open_world.actor.move"
	CityCommandTypeOpenWorldActorPortalUse        = "open_world.actor.portal.use"
	CityCommandTypeOpenWorldActorControlGrant     = "open_world.actor.control.grant"
	CityCommandTypeOpenWorldActorControlRevoke    = "open_world.actor.control.revoke"
	CityCommandTypeOpenWorldPortalStateSet        = "open_world.portal.state.set"
	CityCommandTypeOpenWorldPortalAccessSet       = "open_world.portal.access.set"
	CityCommandTypeOpenWorldActorNavigationSet    = "open_world.actor.navigation.set"
	CityCommandTypeOpenWorldActorNavigationCancel = "open_world.actor.navigation.cancel"
	CityCommandTypeOpenWorldActorMobilityRequest  = "open_world.actor.mobility.request"
	// V14 lifecycle commands are administrator-only. They alter the current
	// effective assignment through fact-backed successor epochs/transitions;
	// V12 bindings and V13 sources are never mutated.
	CityCommandTypeOpenWorldCommuteAssignmentRebind   = "open_world.commute.assignment.rebind"
	CityCommandTypeOpenWorldCommuteAssignmentSetState = "open_world.commute.assignment.state.set"
	// V20 mutates only the generic infrastructure asset lifecycle. Its
	// capacity state is intentionally not consumed by V9 routing yet.
	CityCommandTypeOpenWorldInfrastructureAssetTransition = "open_world.infrastructure.asset.transition"

	CityOpenWorldRuntimeFactActorCreated           = "actor.created"
	CityOpenWorldRuntimeFactActivityPerformed      = "actor.activity.performed"
	CityOpenWorldRuntimeFactRoleTransitioned       = "actor.role.transitioned"
	CityOpenWorldRuntimeFactLocationMoved          = "actor.location.moved"
	CityOpenWorldRuntimeFactPortalTraversed        = "actor.portal.traversed"
	CityOpenWorldRuntimeFactControlGranted         = "actor.control.granted"
	CityOpenWorldRuntimeFactControlRevoked         = "actor.control.revoked"
	CityOpenWorldRuntimeFactPortalStateChanged     = "portal.state.changed"
	CityOpenWorldRuntimeFactPortalAccessChanged    = "portal.access.changed"
	CityOpenWorldRuntimeFactRuleCaseOpened         = "rule.case.opened"
	CityOpenWorldRuntimeFactRuleConsequence        = "rule.consequence.applied"
	CityOpenWorldRuntimeFactStatusExpired          = "actor.status.expired"
	CityOpenWorldRuntimeFactNavigationCreated      = "actor.navigation.intent.created"
	CityOpenWorldRuntimeFactNavigationReplaced     = "actor.navigation.intent.replaced"
	CityOpenWorldRuntimeFactNavigationCancelled    = "actor.navigation.intent.cancelled"
	CityOpenWorldRuntimeFactNavigationProgressed   = "actor.navigation.intent.progressed"
	CityOpenWorldRuntimeFactNavigationArrived      = "actor.navigation.intent.arrived"
	CityOpenWorldRuntimeFactNavigationBlocked      = "actor.navigation.intent.blocked"
	CityOpenWorldRuntimeFactNavigationFailed       = "actor.navigation.intent.failed"
	CityOpenWorldRuntimeFactMobilityRequested      = "mobility.requested"
	CityOpenWorldRuntimeFactMobilityScheduled      = "mobility.scheduled"
	CityOpenWorldRuntimeFactMobilityCompleted      = "mobility.completed"
	CityOpenWorldRuntimeFactMobilityExpired        = "mobility.expired"
	CityOpenWorldRuntimeFactMobilityArrivalPending = "mobility.arrival.pending"
	CityOpenWorldRuntimeFactMobilityArrivalBlocked = "mobility.arrival.blocked"
	CityOpenWorldRuntimeFactMobilityArrivalLanded  = "mobility.arrival.landed"
	CityOpenWorldRuntimeFactMobilityArrivalFailed  = "mobility.arrival.failed"

	cityOpenWorldRuntimeID                     = "sub2api-city-open-world-runtime"
	cityOpenWorldRuntimeVersion                = "1.0.0"
	cityOpenWorldRuntimeCatalogVersion         = "1.0.0"
	cityOpenWorldSocialRuntimeID               = "sub2api-city-open-world-social-runtime"
	cityOpenWorldSocialRuntimeVersion          = "2.0.0"
	cityOpenWorldSocialRuntimeCatalogVersion   = "2.0.0"
	cityOpenWorldRuntimeExecutorVersion        = "1.0.0"
	cityOpenWorldRuntimeMaximumEffects         = 64
	cityOpenWorldRuntimeMaximumRuleCases       = 32
	cityOpenWorldRuntimeMaximumActorsPerMember = 3
)

var (
	ErrCityOpenWorldRuntimeNotFound = infraerrors.NotFound(
		"CITY_OPEN_WORLD_RUNTIME_NOT_FOUND", "open-world runtime state not found",
	)
	ErrCityOpenWorldActorNotFound = infraerrors.NotFound(
		"CITY_OPEN_WORLD_ACTOR_NOT_FOUND", "open-world actor not found",
	)
	ErrCityOpenWorldRuntimeDefinitionNotFound = infraerrors.NotFound(
		"CITY_OPEN_WORLD_RUNTIME_DEFINITION_NOT_FOUND", "open-world runtime definition not found",
	)
	errCityOpenWorldRuntimeInvalidDefinition = errors.New("invalid open-world runtime definition")
)

// CityOpenWorldRuntimeDefinition deliberately shares the proven definition
// wire format with the generic runtime.  Its storage, catalog binding and
// executor are separate V4 facts, so it has no dependency on the F7 runtime.
type CityOpenWorldRuntimeDefinition = WorldRuntimeDefinition

type CityOpenWorldRuntimeProfile struct {
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

type CityOpenWorldActor struct {
	Code             string                      `json:"code"`
	OwnerUserID      *int64                      `json:"owner_user_id,omitempty"`
	ActorTypeCode    string                      `json:"actor_type_code"`
	Name             string                      `json:"name"`
	Status           string                      `json:"status"`
	ArchetypeCode    *string                     `json:"archetype_code,omitempty"`
	ArchetypeVersion *string                     `json:"archetype_version,omitempty"`
	CreatedTick      int64                       `json:"created_tick"`
	UpdatedTick      int64                       `json:"updated_tick"`
	Version          int64                       `json:"version"`
	Metadata         json.RawMessage             `json:"metadata"`
	Location         *CityOpenWorldActorLocation `json:"location,omitempty"`
}

type CityOpenWorldActorLocation struct {
	ActorCode     string                       `json:"actor_code"`
	SpaceKind     string                       `json:"space_kind"`
	LocationScope string                       `json:"location_scope"`
	BuildingCode  *string                      `json:"building_code,omitempty"`
	FloorIndex    int32                        `json:"floor_index"`
	X             int64                        `json:"x"`
	Y             int64                        `json:"y"`
	Z             int32                        `json:"z"`
	SectorX       int64                        `json:"sector_x"`
	SectorY       int64                        `json:"sector_y"`
	ChunkX        int64                        `json:"chunk_x"`
	ChunkY        int64                        `json:"chunk_y"`
	LocalX        int32                        `json:"local_x"`
	LocalY        int32                        `json:"local_y"`
	MovedTick     int64                        `json:"moved_tick"`
	SourceFact    *CityOpenWorldRuntimeFactRef `json:"source_fact,omitempty"`
	Version       int64                        `json:"version"`
	Metadata      json.RawMessage              `json:"metadata"`
}

type CityOpenWorldActorAttribute struct {
	ActorCode       string          `json:"actor_code"`
	AttributeCode   string          `json:"attribute_code"`
	ValueUnits      int64           `json:"value_units"`
	ExperienceUnits int64           `json:"experience_units"`
	LastChangedTick int64           `json:"last_changed_tick"`
	Version         int64           `json:"version"`
	Metadata        json.RawMessage `json:"metadata"`
}

type CityOpenWorldActorRole struct {
	ActorCode    string          `json:"actor_code"`
	RoleCode     string          `json:"role_code"`
	CategoryCode string          `json:"category_code"`
	Status       string          `json:"status"`
	GrantedTick  int64           `json:"granted_tick"`
	RevokedTick  *int64          `json:"revoked_tick,omitempty"`
	Version      int64           `json:"version"`
	Metadata     json.RawMessage `json:"metadata"`
}

type CityOpenWorldActorStatus struct {
	ActorCode      string                       `json:"actor_code"`
	InstanceCode   string                       `json:"instance_code"`
	StatusCode     string                       `json:"status_code"`
	Lifecycle      string                       `json:"lifecycle_status"`
	IntensityUnits int64                        `json:"intensity_units"`
	Stacks         int                          `json:"stacks"`
	GrantedTick    int64                        `json:"granted_tick"`
	ExpiresTick    *int64                       `json:"expires_tick,omitempty"`
	EndedTick      *int64                       `json:"ended_tick,omitempty"`
	SourceFact     *CityOpenWorldRuntimeFactRef `json:"source_fact,omitempty"`
	Version        int64                        `json:"version"`
	Metadata       json.RawMessage              `json:"metadata"`
}

type CityOpenWorldActorControlGrant struct {
	Code             string                       `json:"code"`
	ActorCode        string                       `json:"actor_code"`
	UserID           int64                        `json:"user_id"`
	Capability       string                       `json:"capability"`
	Status           string                       `json:"status"`
	GrantedByUserID  int64                        `json:"granted_by_user_id"`
	GrantedTick      int64                        `json:"granted_tick"`
	RevokedTick      *int64                       `json:"revoked_tick,omitempty"`
	GrantSourceFact  *CityOpenWorldRuntimeFactRef `json:"grant_source_fact,omitempty"`
	RevokeSourceFact *CityOpenWorldRuntimeFactRef `json:"revoke_source_fact,omitempty"`
	Version          int64                        `json:"version"`
	Metadata         json.RawMessage              `json:"metadata"`
}

type CityOpenWorldPortalState struct {
	PortalCode        string                       `json:"portal_code"`
	BuildingCode      string                       `json:"building_code"`
	PortalType        string                       `json:"portal_type"`
	StateCode         string                       `json:"state_code"`
	AccessRequirement WorldRequirementNode         `json:"access_requirement"`
	AccessPolicyHash  string                       `json:"access_policy_hash"`
	ChangedTick       int64                        `json:"changed_tick"`
	SourceFact        *CityOpenWorldRuntimeFactRef `json:"source_fact,omitempty"`
	Version           int64                        `json:"version"`
	Metadata          json.RawMessage              `json:"metadata"`
}

type CityOpenWorldRuntimeFactRef struct {
	Tick     int64 `json:"tick"`
	Sequence int64 `json:"sequence"`
}

type CityOpenWorldRuntimeFact struct {
	Tick                  int64                        `json:"tick"`
	Sequence              int64                        `json:"sequence"`
	SourceCommandSequence *int64                       `json:"source_command_sequence,omitempty"`
	Parent                *CityOpenWorldRuntimeFactRef `json:"parent,omitempty"`
	ActorCode             *string                      `json:"actor_code,omitempty"`
	FactType              string                       `json:"fact_type"`
	DefinitionKind        *string                      `json:"definition_kind,omitempty"`
	DefinitionCode        *string                      `json:"definition_code,omitempty"`
	DefinitionVersion     *string                      `json:"definition_version,omitempty"`
	DefinitionHash        *string                      `json:"definition_hash,omitempty"`
	Payload               json.RawMessage              `json:"payload"`
}

type CityOpenWorldRuntimeEffect struct {
	Tick            int64                       `json:"tick"`
	Sequence        int64                       `json:"sequence"`
	SourceFact      CityOpenWorldRuntimeFactRef `json:"source_fact"`
	OperationIndex  int                         `json:"operation_index"`
	EffectType      string                      `json:"effect_type"`
	ExecutorVersion string                      `json:"executor_version"`
	TargetActorCode *string                     `json:"target_actor_code,omitempty"`
	TargetKey       *string                     `json:"target_key,omitempty"`
	BeforeUnits     *int64                      `json:"before_units,omitempty"`
	DeltaUnits      *int64                      `json:"delta_units,omitempty"`
	AfterUnits      *int64                      `json:"after_units,omitempty"`
	Payload         json.RawMessage             `json:"payload"`
}

type CityOpenWorldRuleCase struct {
	Code             string                       `json:"code"`
	Tick             int64                        `json:"tick"`
	Sequence         int64                        `json:"sequence"`
	SourceFact       CityOpenWorldRuntimeFactRef  `json:"source_fact"`
	ConsequenceFact  *CityOpenWorldRuntimeFactRef `json:"consequence_fact,omitempty"`
	SubjectActorCode string                       `json:"subject_actor_code"`
	RuleCode         string                       `json:"rule_code"`
	RuleVersion      string                       `json:"rule_version"`
	RuleHash         string                       `json:"rule_hash"`
	CategoryCode     string                       `json:"category_code"`
	ScopeKind        string                       `json:"scope_kind"`
	ScopeCode        string                       `json:"scope_code"`
	Status           string                       `json:"status"`
	SeverityUnits    int64                        `json:"severity_units"`
	DecisionCode     *string                      `json:"decision_code,omitempty"`
	CreatedTick      int64                        `json:"created_tick"`
	DecidedTick      *int64                       `json:"decided_tick,omitempty"`
	ClosedTick       *int64                       `json:"closed_tick,omitempty"`
	Payload          json.RawMessage              `json:"payload"`
}

type CityOpenWorldRuntimeState struct {
	Profile       CityOpenWorldRuntimeProfile      `json:"profile"`
	Definitions   []CityOpenWorldRuntimeDefinition `json:"definitions"`
	Actors        []CityOpenWorldActor             `json:"actors"`
	Attributes    []CityOpenWorldActorAttribute    `json:"attributes"`
	Roles         []CityOpenWorldActorRole         `json:"roles"`
	Statuses      []CityOpenWorldActorStatus       `json:"statuses"`
	Locations     []CityOpenWorldActorLocation     `json:"locations"`
	ControlGrants []CityOpenWorldActorControlGrant `json:"control_grants"`
	PortalStates  []CityOpenWorldPortalState       `json:"portal_states"`
	Facts         []CityOpenWorldRuntimeFact       `json:"facts"`
	Effects       []CityOpenWorldRuntimeEffect     `json:"effects"`
	RuleCases     []CityOpenWorldRuleCase          `json:"rule_cases"`
	// Social is present only for the V5 contract. Keeping it inside the
	// independent open-world runtime state makes the scenario/facility/NPC
	// baseline part of the same canonical snapshot without leaking into F7.
	Social *CityOpenWorldSocialRuntimeState `json:"social,omitempty"`
	// Services is present only for the V7 contract. It is deliberately a
	// sibling of Social: service dispatch consumes the V5 facility projection,
	// but owns a separate, fact-backed access and queue state machine.
	Services *CityOpenWorldServiceState `json:"services,omitempty"`
	// Impacts is present only for the V8 contract. It bridges immutable service
	// responses into delayed, explicit domain-metric applications without
	// letting the service reducer mutate another domain in the response tick.
	Impacts *CityOpenWorldImpactState `json:"impacts,omitempty"`
	// Mobility is present only for V9. It carries the aggregate hub graph,
	// route demand, capacity allocation, and route history, but never owns the
	// V5 cell-level actor location projection.
	Mobility *CityOpenWorldMobilityState `json:"mobility,omitempty"`
	// Arrivals is V10's explicit boundary between a completed V9 aggregate
	// route and a validated V5 local surface landing. It never rewrites the
	// V9 route itself and retains its own fact chain for audit/recovery.
	Arrivals *CityOpenWorldMobilityArrivalState `json:"arrivals,omitempty"`
	// OD is V11's versioned source adapter and closed-cycle metric ledger. It
	// creates V9-compatible demands but never owns routes, allocations, or V10
	// local arrival transitions.
	OD *CityOpenWorldMobilityODState `json:"od,omitempty"`
	// Commutes is V12's immutable residence/employment binding baseline. It
	// holds no live travel state; future adapters must still go through V9/V10.
	Commutes *CityOpenWorldCommuteState `json:"commutes,omitempty"`
	// CommuteSources is V13's fact-backed dual-direction source lifecycle.
	// It consumes V12 bindings without owning V9 routes or V10 arrivals.
	CommuteSources *CityOpenWorldCommuteSourceState `json:"commute_sources,omitempty"`
	// CommuteLifecycle is V14's successor-epoch projection. It retains V12
	// bindings and V13 source history as immutable inputs while allowing
	// administrator-authorized rebinds and pauses through append-only facts.
	CommuteLifecycle *CityOpenWorldCommuteLifecycleState `json:"commute_lifecycle,omitempty"`
	// SupplyChain is F10.0's firm/facility/district order lifecycle. It owns
	// reservations, settlement, and delivery evidence without modifying sealed
	// V5–V14 social or commute projections.
	SupplyChain *CityOpenWorldSupplyChainState `json:"supply_chain,omitempty"`
	// EnterpriseFreight is V16's narrow dispatch-to-network adapter. It proves
	// the V15 dispatch-to-V9 demand relationship without granting route
	// completion any inventory or settlement semantics.
	EnterpriseFreight *CityOpenWorldEnterpriseFreightState `json:"enterprise_freight,omitempty"`
	// EnterpriseFreightReceipts is V17's fact-backed custody and receipt gate.
	// It observes V16 transport evidence and V15 delivery evidence but never
	// becomes a second mutable inventory ledger.
	EnterpriseFreightReceipts *CityOpenWorldEnterpriseFreightReceiptState `json:"enterprise_freight_receipts,omitempty"`
	// EnterpriseFreightBatches is V18's overflow-only consignment planner. It
	// proves all capacity-bounded arrivals before V15's one atomic delivery.
	EnterpriseFreightBatches *CityOpenWorldFreightBatchState `json:"enterprise_freight_batches,omitempty"`
	// SpatialNetwork is V19's frozen public identity layer over V9 hubs and
	// edges. It intentionally contains no live routing or mutable capacity.
	SpatialNetwork *CityOpenWorldSpatialNetworkState `json:"spatial_network,omitempty"`
	// Infrastructure is V20's generic asset lifecycle over V19 identities.
	// It records capacity/state facts only; a later engine version owns any
	// scheduler consumption of those values.
	Infrastructure *CityOpenWorldInfrastructureState `json:"infrastructure,omitempty"`
	// EffectiveCapacity is V21's fact-backed proof that new V9 reservations
	// consumed the matching V20 corridor-segment capacity. It stores admission
	// evidence only; current capacity remains derived from V9/V19/V20 state.
	EffectiveCapacity *CityOpenWorldEffectiveCapacityState `json:"effective_capacity,omitempty"`
	// FreightSettlements is V22's partial freight outcome layer. It never
	// rewrites prior V15 atomic deliveries; only post-baseline V17/V18 sources
	// may reach the V15 settled successor through this projection.
	FreightSettlements *CityOpenWorldFreightSettlementState `json:"freight_settlements,omitempty"`
	// CarrierRecovery is V23's separate manual reserve and carrier-claim
	// closure ledger. It consumes V22 claims but never rewrites V22 receipts.
	CarrierRecovery *CityOpenWorldCarrierRecoveryState `json:"carrier_recovery,omitempty"`
	// CarrierCommerce is V24's append-only contract and cash-fee projection.
	// It is deliberately separate from V22 custody and V23 recovery evidence.
	CarrierCommerce *CityOpenWorldCarrierCommerceState `json:"carrier_commerce,omitempty"`
}

type cityOpenWorldRuntimeHashState = CityOpenWorldRuntimeState

type CityOpenWorldRuntimeCatalog struct {
	Profile     CityOpenWorldRuntimeProfile      `json:"profile"`
	Definitions []CityOpenWorldRuntimeDefinition `json:"definitions"`
}

type CityOpenWorldActorState struct {
	Actor         CityOpenWorldActor               `json:"actor"`
	Attributes    []CityOpenWorldActorAttribute    `json:"attributes"`
	Roles         []CityOpenWorldActorRole         `json:"roles"`
	Statuses      []CityOpenWorldActorStatus       `json:"statuses"`
	RecentFacts   []CityOpenWorldRuntimeFact       `json:"recent_facts"`
	ControlGrants []CityOpenWorldActorControlGrant `json:"control_grants"`
	Capabilities  []string                         `json:"capabilities"`
}

type cityOpenWorldActorCreatePayload struct {
	ArchetypeCode string `json:"archetype_code"`
	Name          string `json:"name"`
}

type cityOpenWorldActorActivityPayload struct {
	ActorCode    string `json:"actor_code"`
	ActivityCode string `json:"activity_code"`
}

type cityOpenWorldActorRoleTransitionPayload struct {
	ActorCode string `json:"actor_code"`
	RoleCode  string `json:"role_code"`
}

type cityOpenWorldActorMovePayload struct {
	ActorCode    string `json:"actor_code"`
	SpaceKind    string `json:"space_kind"`
	BuildingCode string `json:"building_code,omitempty"`
	FloorIndex   int32  `json:"floor_index"`
	X            int64  `json:"x"`
	Y            int64  `json:"y"`
	Z            int32  `json:"z"`
}

type cityOpenWorldActorPortalUsePayload struct {
	ActorCode  string `json:"actor_code"`
	PortalCode string `json:"portal_code"`
}

type cityOpenWorldActorControlPayload struct {
	ActorCode    string   `json:"actor_code"`
	UserID       int64    `json:"user_id"`
	Capabilities []string `json:"capabilities"`
}

type cityOpenWorldPortalStatePayload struct {
	ActorCode  string `json:"actor_code"`
	PortalCode string `json:"portal_code"`
	Action     string `json:"action"`
}

type cityOpenWorldPortalAccessPayload struct {
	ActorCode    string               `json:"actor_code"`
	PortalCode   string               `json:"portal_code"`
	Requirements WorldRequirementNode `json:"requirements"`
}

type cityOpenWorldActorNavigationSetPayload struct {
	ActorCode    string `json:"actor_code"`
	SpaceKind    string `json:"space_kind"`
	BuildingCode string `json:"building_code,omitempty"`
	FloorIndex   int32  `json:"floor_index"`
	X            int64  `json:"x"`
	Y            int64  `json:"y"`
	Z            int32  `json:"z"`
	Priority     int    `json:"priority"`
	MaximumSteps int    `json:"maximum_steps"`
}

type cityOpenWorldActorNavigationCancelPayload struct {
	ActorCode string `json:"actor_code"`
}

// cityOpenWorldCommuteAssignmentRebindPayload selects the successor epoch's
// effective facilities. Hubs are resolved from the sealed V9 facility hub
// topology so callers cannot manufacture arbitrary route endpoints.
type cityOpenWorldCommuteAssignmentRebindPayload struct {
	ActorCode          string `json:"actor_code"`
	EmploymentRoleCode string `json:"employment_role_code"`
	HomeFacilityCode   string `json:"home_facility_code"`
	WorkFacilityCode   string `json:"work_facility_code"`
	OutboundPhase      *int64 `json:"outbound_phase,omitempty"`
	ReasonCode         string `json:"reason_code,omitempty"`
}

type cityOpenWorldCommuteAssignmentSetStatePayload struct {
	ActorCode  string `json:"actor_code"`
	State      string `json:"state"`
	ReasonCode string `json:"reason_code,omitempty"`
}

func isCityOpenWorldRuntimeCommand(commandType string) bool {
	switch commandType {
	case CityCommandTypeOpenWorldActorCreate,
		CityCommandTypeOpenWorldActorActivityPerform,
		CityCommandTypeOpenWorldActorRoleTransition,
		CityCommandTypeOpenWorldActorMove,
		CityCommandTypeOpenWorldActorPortalUse,
		CityCommandTypeOpenWorldActorControlGrant,
		CityCommandTypeOpenWorldActorControlRevoke,
		CityCommandTypeOpenWorldPortalStateSet,
		CityCommandTypeOpenWorldPortalAccessSet,
		CityCommandTypeOpenWorldActorNavigationSet,
		CityCommandTypeOpenWorldActorNavigationCancel,
		CityCommandTypeOpenWorldActorServiceRequest,
		CityCommandTypeOpenWorldActorMobilityRequest,
		CityCommandTypeOpenWorldCommuteAssignmentRebind,
		CityCommandTypeOpenWorldCommuteAssignmentSetState,
		CityCommandTypeOpenWorldInfrastructureAssetTransition:
		return true
	default:
		return false
	}
}

func isCityOpenWorldSocialRuntimeCommand(commandType string) bool {
	return commandType == CityCommandTypeOpenWorldActorNavigationSet ||
		commandType == CityCommandTypeOpenWorldActorNavigationCancel
}

func isCityOpenWorldCommuteLifecycleCommand(commandType string) bool {
	return commandType == CityCommandTypeOpenWorldCommuteAssignmentRebind ||
		commandType == CityCommandTypeOpenWorldCommuteAssignmentSetState
}

func normalizeCityOpenWorldRuntimeCommand(commandType string, rawPayload json.RawMessage) (any, bool, error) {
	normalizeCode := func(value *string, field string) error {
		*value = strings.ToLower(strings.TrimSpace(*value))
		if !worldRuntimeCodeValid(*value, 128) {
			return ErrCityInvalidInput.WithMetadata(map[string]string{"field": field})
		}
		return nil
	}
	switch commandType {
	case CityCommandTypeOpenWorldActorCreate:
		var value cityOpenWorldActorCreatePayload
		if err := decodeStrictCityObject(rawPayload, &value); err != nil {
			return nil, true, ErrCityInvalidInput.WithCause(err)
		}
		if err := normalizeCode(&value.ArchetypeCode, "archetype_code"); err != nil {
			return nil, true, err
		}
		value.Name = strings.TrimSpace(value.Name)
		if utf8.RuneCountInString(value.Name) < 1 || utf8.RuneCountInString(value.Name) > 96 {
			return nil, true, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "name"})
		}
		return value, true, nil
	case CityCommandTypeOpenWorldActorActivityPerform:
		var value cityOpenWorldActorActivityPayload
		if err := decodeStrictCityObject(rawPayload, &value); err != nil {
			return nil, true, ErrCityInvalidInput.WithCause(err)
		}
		if err := normalizeCode(&value.ActorCode, "actor_code"); err != nil {
			return nil, true, err
		}
		if err := normalizeCode(&value.ActivityCode, "activity_code"); err != nil {
			return nil, true, err
		}
		return value, true, nil
	case CityCommandTypeOpenWorldActorRoleTransition:
		var value cityOpenWorldActorRoleTransitionPayload
		if err := decodeStrictCityObject(rawPayload, &value); err != nil {
			return nil, true, ErrCityInvalidInput.WithCause(err)
		}
		if err := normalizeCode(&value.ActorCode, "actor_code"); err != nil {
			return nil, true, err
		}
		if err := normalizeCode(&value.RoleCode, "role_code"); err != nil {
			return nil, true, err
		}
		return value, true, nil
	case CityCommandTypeOpenWorldActorMove:
		var value cityOpenWorldActorMovePayload
		if err := decodeStrictCityObject(rawPayload, &value); err != nil {
			return nil, true, ErrCityInvalidInput.WithCause(err)
		}
		if err := normalizeCode(&value.ActorCode, "actor_code"); err != nil {
			return nil, true, err
		}
		value.SpaceKind = strings.ToLower(strings.TrimSpace(value.SpaceKind))
		value.BuildingCode = strings.ToLower(strings.TrimSpace(value.BuildingCode))
		if value.X < -cityOpenWorldMaximumWorldCoordinate || value.X > cityOpenWorldMaximumWorldCoordinate ||
			value.Y < -cityOpenWorldMaximumWorldCoordinate || value.Y > cityOpenWorldMaximumWorldCoordinate {
			return nil, true, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "coordinate"})
		}
		switch value.SpaceKind {
		case "surface":
			if value.BuildingCode != "" || value.FloorIndex != 0 || value.Z != 0 {
				return nil, true, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "surface_location"})
			}
		case "interior":
			if !worldRuntimeCodeValid(value.BuildingCode, 96) || value.FloorIndex < 0 || value.Z != value.FloorIndex {
				return nil, true, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "interior_location"})
			}
		default:
			return nil, true, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "space_kind"})
		}
		return value, true, nil
	case CityCommandTypeOpenWorldActorPortalUse:
		var value cityOpenWorldActorPortalUsePayload
		if err := decodeStrictCityObject(rawPayload, &value); err != nil {
			return nil, true, ErrCityInvalidInput.WithCause(err)
		}
		if err := normalizeCode(&value.ActorCode, "actor_code"); err != nil {
			return nil, true, err
		}
		if err := normalizeCode(&value.PortalCode, "portal_code"); err != nil {
			return nil, true, err
		}
		return value, true, nil
	case CityCommandTypeOpenWorldActorControlGrant, CityCommandTypeOpenWorldActorControlRevoke:
		var value cityOpenWorldActorControlPayload
		if err := decodeStrictCityObject(rawPayload, &value); err != nil {
			return nil, true, ErrCityInvalidInput.WithCause(err)
		}
		if err := normalizeCode(&value.ActorCode, "actor_code"); err != nil {
			return nil, true, err
		}
		if value.UserID <= 0 || len(value.Capabilities) == 0 || len(value.Capabilities) > 2 {
			return nil, true, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "capabilities"})
		}
		seen := make(map[string]struct{}, len(value.Capabilities))
		for index := range value.Capabilities {
			value.Capabilities[index] = strings.ToLower(strings.TrimSpace(value.Capabilities[index]))
			if value.Capabilities[index] != WorldActorCapabilityCommand && value.Capabilities[index] != WorldActorCapabilityManageControl {
				return nil, true, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "capabilities"})
			}
			if _, exists := seen[value.Capabilities[index]]; exists {
				return nil, true, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "capabilities"})
			}
			seen[value.Capabilities[index]] = struct{}{}
		}
		sort.Strings(value.Capabilities)
		return value, true, nil
	case CityCommandTypeOpenWorldPortalStateSet:
		var value cityOpenWorldPortalStatePayload
		if err := decodeStrictCityObject(rawPayload, &value); err != nil {
			return nil, true, ErrCityInvalidInput.WithCause(err)
		}
		if err := normalizeCode(&value.ActorCode, "actor_code"); err != nil {
			return nil, true, err
		}
		if err := normalizeCode(&value.PortalCode, "portal_code"); err != nil {
			return nil, true, err
		}
		value.Action = strings.ToLower(strings.TrimSpace(value.Action))
		if value.Action != WorldPortalActionOpen && value.Action != WorldPortalActionClose &&
			value.Action != WorldPortalActionLock && value.Action != WorldPortalActionUnlock {
			return nil, true, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "action"})
		}
		return value, true, nil
	case CityCommandTypeOpenWorldPortalAccessSet:
		var value cityOpenWorldPortalAccessPayload
		if err := decodeStrictCityObject(rawPayload, &value); err != nil {
			return nil, true, ErrCityInvalidInput.WithCause(err)
		}
		if err := normalizeCode(&value.ActorCode, "actor_code"); err != nil {
			return nil, true, err
		}
		if err := normalizeCode(&value.PortalCode, "portal_code"); err != nil {
			return nil, true, err
		}
		normalizeWorldRequirementNode(&value.Requirements)
		if err := validateWorldRequirement(value.Requirements); err != nil {
			return nil, true, ErrCityInvalidInput.WithCause(err).WithMetadata(map[string]string{"field": "requirements"})
		}
		return value, true, nil
	case CityCommandTypeOpenWorldActorNavigationSet:
		var value cityOpenWorldActorNavigationSetPayload
		if err := decodeStrictCityObject(rawPayload, &value); err != nil {
			return nil, true, ErrCityInvalidInput.WithCause(err)
		}
		if err := normalizeCode(&value.ActorCode, "actor_code"); err != nil {
			return nil, true, err
		}
		value.SpaceKind = strings.ToLower(strings.TrimSpace(value.SpaceKind))
		value.BuildingCode = strings.ToLower(strings.TrimSpace(value.BuildingCode))
		if value.MaximumSteps == 0 {
			value.MaximumSteps = cityOpenWorldV5NavigationDefaultMaximumSteps
		}
		if value.Priority < -1000 || value.Priority > 1000 ||
			value.MaximumSteps < 1 || value.MaximumSteps > cityOpenWorldV5NavigationMaximumSteps ||
			!cityOpenWorldV5NavigationLocationPayloadValid(value) {
			return nil, true, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "open_world_navigation"})
		}
		return value, true, nil
	case CityCommandTypeOpenWorldActorNavigationCancel:
		var value cityOpenWorldActorNavigationCancelPayload
		if err := decodeStrictCityObject(rawPayload, &value); err != nil {
			return nil, true, ErrCityInvalidInput.WithCause(err)
		}
		if err := normalizeCode(&value.ActorCode, "actor_code"); err != nil {
			return nil, true, err
		}
		return value, true, nil
	case CityCommandTypeOpenWorldActorServiceRequest:
		value, err := normalizeCityOpenWorldActorServiceRequest(rawPayload)
		if err != nil {
			return nil, true, err
		}
		return value, true, nil
	case CityCommandTypeOpenWorldActorMobilityRequest:
		value, err := normalizeCityOpenWorldActorMobilityRequest(rawPayload)
		if err != nil {
			return nil, true, err
		}
		return value, true, nil
	case CityCommandTypeOpenWorldCommuteAssignmentRebind:
		var value cityOpenWorldCommuteAssignmentRebindPayload
		if err := decodeStrictCityObject(rawPayload, &value); err != nil {
			return nil, true, ErrCityInvalidInput.WithCause(err)
		}
		if err := normalizeCode(&value.ActorCode, "actor_code"); err != nil {
			return nil, true, err
		}
		if err := normalizeCode(&value.EmploymentRoleCode, "employment_role_code"); err != nil {
			return nil, true, err
		}
		if err := normalizeCode(&value.HomeFacilityCode, "home_facility_code"); err != nil {
			return nil, true, err
		}
		if err := normalizeCode(&value.WorkFacilityCode, "work_facility_code"); err != nil {
			return nil, true, err
		}
		if value.HomeFacilityCode == value.WorkFacilityCode {
			return nil, true, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "facility_assignment"})
		}
		if value.OutboundPhase != nil && (*value.OutboundPhase < 0 || *value.OutboundPhase >= cityOpenWorldCommutePeriodTicks) {
			return nil, true, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "outbound_phase"})
		}
		value.ReasonCode = strings.ToLower(strings.TrimSpace(value.ReasonCode))
		if value.ReasonCode == "" {
			value.ReasonCode = cityOpenWorldCommuteLifecycleReasonAdminRebind
		}
		if !worldRuntimeCodeValid(value.ReasonCode, 96) {
			return nil, true, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "reason_code"})
		}
		return value, true, nil
	case CityCommandTypeOpenWorldCommuteAssignmentSetState:
		var value cityOpenWorldCommuteAssignmentSetStatePayload
		if err := decodeStrictCityObject(rawPayload, &value); err != nil {
			return nil, true, ErrCityInvalidInput.WithCause(err)
		}
		if err := normalizeCode(&value.ActorCode, "actor_code"); err != nil {
			return nil, true, err
		}
		value.State = strings.ToLower(strings.TrimSpace(value.State))
		if value.State != cityOpenWorldCommuteLifecycleStateActive && value.State != cityOpenWorldCommuteLifecycleStateSuspended {
			return nil, true, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "state"})
		}
		value.ReasonCode = strings.ToLower(strings.TrimSpace(value.ReasonCode))
		if value.ReasonCode == "" {
			if value.State == cityOpenWorldCommuteLifecycleStateActive {
				value.ReasonCode = cityOpenWorldCommuteLifecycleReasonAdminResumed
			} else {
				value.ReasonCode = cityOpenWorldCommuteLifecycleReasonAdminSuspended
			}
		}
		if !worldRuntimeCodeValid(value.ReasonCode, 96) {
			return nil, true, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "reason_code"})
		}
		return value, true, nil
	case CityCommandTypeOpenWorldInfrastructureAssetTransition:
		return normalizeCityOpenWorldInfrastructureCommand(rawPayload)
	default:
		return nil, false, nil
	}
}

func builtInCityOpenWorldRuntimeDefinitions() ([]CityOpenWorldRuntimeDefinition, string, error) {
	return builtInCityOpenWorldRuntimeDefinitionsForVersion(CitySimulationVersionOpenWorldV4)
}

func builtInCityOpenWorldRuntimeDefinitionsForVersion(simulationVersion string) ([]CityOpenWorldRuntimeDefinition, string, error) {
	definitions, _, err := builtInWorldRuntimeDefinitions()
	if err != nil {
		return nil, "", err
	}
	// NPCs use the same deterministic attributes/roles and are not a browser-only
	// decoration.  The explicit actor type leaves room for autonomous behavior
	// and ownership rules without overloading player characters.
	npcPayload, err := json.Marshal(map[string]any{
		"name_key": "openWorldRuntime.actorTypes.npc", "controllable": false,
	})
	if err != nil {
		return nil, "", fmt.Errorf("marshal open-world NPC actor type: %w", err)
	}
	sum := sha256.Sum256(npcPayload)
	definitions = append(definitions, CityOpenWorldRuntimeDefinition{
		Kind: WorldRuntimeDefinitionActorType, Code: "npc", Version: cityOpenWorldRuntimeCatalogVersion,
		Hash: hex.EncodeToString(sum[:]), Visibility: "public", Payload: npcPayload,
	})
	if cityEngineSupportsOpenWorldSocialRuntime(simulationVersion) {
		socialDefinitions, socialErr := builtInCityOpenWorldSocialRuntimeDefinitions()
		if socialErr != nil {
			return nil, "", socialErr
		}
		definitions = append(definitions, socialDefinitions...)
	}
	sort.Slice(definitions, func(i, j int) bool {
		if definitions[i].Kind != definitions[j].Kind {
			return definitions[i].Kind < definitions[j].Kind
		}
		return definitions[i].Code < definitions[j].Code
	})
	if err = validateWorldRuntimeCatalog(definitions); err != nil {
		return nil, "", fmt.Errorf("validate open-world runtime catalog: %w", err)
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
		return nil, "", fmt.Errorf("marshal open-world runtime catalog: %w", err)
	}
	catalogHash := sha256.Sum256(raw)
	return definitions, hex.EncodeToString(catalogHash[:]), nil
}

func cityOpenWorldRuntimeProfileIdentity(simulationVersion string) (runtimeID, runtimeVersion, catalogVersion string, err error) {
	switch simulationVersion {
	case CitySimulationVersionOpenWorldV4:
		return cityOpenWorldRuntimeID, cityOpenWorldRuntimeVersion, cityOpenWorldRuntimeCatalogVersion, nil
	case CitySimulationVersionOpenWorldV5, CitySimulationVersionOpenWorldV6,
		CitySimulationVersionOpenWorldV7, CitySimulationVersionOpenWorldV8, CitySimulationVersionOpenWorldV9, CitySimulationVersionOpenWorldV10, CitySimulationVersionOpenWorldV11, CitySimulationVersionOpenWorldV12, CitySimulationVersionOpenWorldV13, CitySimulationVersionOpenWorldV14, CitySimulationVersionOpenWorldV15, CitySimulationVersionOpenWorldV16, CitySimulationVersionOpenWorldV17, CitySimulationVersionOpenWorldV18, CitySimulationVersionOpenWorldV19, CitySimulationVersionOpenWorldV20, CitySimulationVersionOpenWorldV21, CitySimulationVersionOpenWorldV22, CitySimulationVersionOpenWorldV23, CitySimulationVersionOpenWorldV24:
		return cityOpenWorldSocialRuntimeID, cityOpenWorldSocialRuntimeVersion, cityOpenWorldSocialRuntimeCatalogVersion, nil
	default:
		return "", "", "", ErrCitySimulationVersion.WithMetadata(map[string]string{"version": simulationVersion})
	}
}

func initializeCityOpenWorldRuntimeFoundation(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if worldID <= 0 {
		return ErrCityInvalidInput
	}
	var baselineTick int64
	var simulationVersion string
	var err error
	if err = tx.QueryRowContext(ctx, `
SELECT current_tick, simulation_version FROM city_worlds WHERE id = $1`, worldID).Scan(
		&baselineTick, &simulationVersion,
	); err != nil {
		return fmt.Errorf("load open-world runtime baseline: %w", err)
	}
	if !cityEngineSupportsOpenWorldRuntime(simulationVersion) || baselineTick != 0 {
		return ErrCitySimulationVersion.WithMetadata(map[string]string{"version": simulationVersion})
	}
	runtimeID, runtimeVersion, catalogVersion, err := cityOpenWorldRuntimeProfileIdentity(simulationVersion)
	if err != nil {
		return err
	}
	definitions, catalogHash, err := builtInCityOpenWorldRuntimeDefinitionsForVersion(simulationVersion)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `
SELECT set_config('sub2api.city_open_world_runtime_bootstrap_world_id', $1, TRUE)`, strconv.FormatInt(worldID, 10)); err != nil {
		return fmt.Errorf("enable open-world runtime bootstrap: %w", err)
	}
	metadata := json.RawMessage(`{"schema_version":1,"requirement_ast_version":"1.0.0","effect_executor_version":"1.0.0","location_contract":"surface-or-interior-v1"}`)
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_runtime_profiles
    (world_id, runtime_id, runtime_version, catalog_version, catalog_hash,
     baseline_tick, maximum_player_actors_per_member, actor_count, fact_count,
     effect_count, case_count, revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, 0, 0, 0, 0, 1, $8::jsonb)`,
		worldID, runtimeID, runtimeVersion,
		catalogVersion, catalogHash, baselineTick,
		cityOpenWorldRuntimeMaximumActorsPerMember, []byte(metadata)); err != nil {
		return fmt.Errorf("insert open-world runtime profile: %w", err)
	}
	for _, definition := range definitions {
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_runtime_definitions
    (world_id, definition_kind, code, definition_version, content_hash, visibility, payload)
VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)`, worldID, definition.Kind, definition.Code,
			definition.Version, definition.Hash, definition.Visibility, []byte(definition.Payload)); err != nil {
			return fmt.Errorf("insert open-world runtime definition %s/%s: %w", definition.Kind, definition.Code, err)
		}
	}
	return nil
}

func activateCityOpenWorldRuntimeFactWrite(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if _, err := tx.ExecContext(ctx, `
SELECT set_config('sub2api.city_open_world_runtime_fact_world_id', $1, TRUE)`, strconv.FormatInt(worldID, 10)); err != nil {
		return fmt.Errorf("enable open-world runtime fact write: %w", err)
	}
	return nil
}
