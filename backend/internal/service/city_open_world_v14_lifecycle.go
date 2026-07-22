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
)

const (
	cityOpenWorldCommuteLifecycleSchemaVersion                   = 1
	cityOpenWorldCommuteLifecycleProfileID                       = "sub2api-open-world-commute-lifecycle"
	cityOpenWorldCommuteLifecycleProfileVersion                  = "1.0.0"
	cityOpenWorldCommuteLifecycleAssignmentContract              = "immutable_assignment_epoch_lifecycle_v1"
	cityOpenWorldCommuteLifecycleSourceContract                  = "active_epoch_verified_facility_presence_od_v1"
	cityOpenWorldCommuteLifecycleOriginBaseline                  = "v13_baseline"
	cityOpenWorldCommuteLifecycleOriginAdminRebind               = "admin_rebind"
	cityOpenWorldCommuteLifecycleAssignmentKind                  = "npc.residence_employment"
	cityOpenWorldCommuteLifecycleStateActive                     = "active"
	cityOpenWorldCommuteLifecycleStateSuspended                  = "suspended"
	cityOpenWorldCommuteLifecycleStateSuperseded                 = "superseded"
	cityOpenWorldCommuteLifecycleStateTerminated                 = "terminated"
	cityOpenWorldCommuteLifecycleReasonBaseline                  = "baseline_initialized"
	cityOpenWorldCommuteLifecycleReasonAdminRebind               = "admin_rebind"
	cityOpenWorldCommuteLifecycleReasonAdminSuspended            = "admin_suspended"
	cityOpenWorldCommuteLifecycleReasonAdminResumed              = "admin_resumed"
	cityOpenWorldCommuteLifecycleReasonActorInactive             = "actor_inactive"
	cityOpenWorldCommuteLifecycleReasonActorRestored             = "actor_restored"
	cityOpenWorldCommuteLifecycleReasonEmploymentRoleInactive    = "employment_role_inactive"
	cityOpenWorldCommuteLifecycleReasonEmploymentRoleRestored    = "employment_role_restored"
	cityOpenWorldCommuteLifecycleReasonOriginFacilityUnavailable = "origin_facility_unavailable"
	cityOpenWorldCommuteLifecycleReasonOriginFacilityRestored    = "origin_facility_restored"
	cityOpenWorldCommuteLifecycleReasonDestinationUnavailable    = "destination_facility_unavailable"
	cityOpenWorldCommuteLifecycleReasonDestinationRestored       = "destination_facility_restored"
	cityOpenWorldCommuteLifecycleReasonProfileMismatch           = "profile_assignment_mismatch"
	cityOpenWorldCommuteLifecycleSourceKindOutbound              = "npc.residence_to_work"
	cityOpenWorldCommuteLifecycleSourceKindReturn                = "npc.work_to_residence"
	cityOpenWorldCommuteLifecyclePurposeOutbound                 = "routine.commute.outbound"
	cityOpenWorldCommuteLifecyclePurposeReturn                   = "routine.commute.return"
	cityOpenWorldCommuteLifecycleSourceStatusActive              = "active"
	cityOpenWorldCommuteLifecycleMaximumAssignments              = 4096
	cityOpenWorldCommuteLifecycleMaximumTransitionsTick          = 512
	cityOpenWorldCommuteLifecycleMaximumGenerationsTick          = 128
)

// CityOpenWorldCommuteLifecyclePolicy owns only V14's successor assignment
// projection. V12 bindings and V13 sources remain sealed historical inputs.
type CityOpenWorldCommuteLifecyclePolicy struct {
	ProfileID                 string          `json:"profile_id"`
	ProfileVersion            string          `json:"profile_version"`
	ContentHash               string          `json:"content_hash"`
	BaselineTick              int64           `json:"baseline_tick"`
	AssignmentContract        string          `json:"assignment_contract"`
	SourceContract            string          `json:"source_contract"`
	PeriodTicks               int64           `json:"period_ticks"`
	MaximumAssignments        int             `json:"maximum_assignments"`
	MaximumTransitionsTick    int             `json:"maximum_transitions_tick"`
	MaximumGenerationsTick    int             `json:"maximum_generations_tick"`
	AssignmentCount           int64           `json:"assignment_count"`
	ActiveAssignmentCount     int64           `json:"active_assignment_count"`
	SuspendedAssignmentCount  int64           `json:"suspended_assignment_count"`
	SupersededAssignmentCount int64           `json:"superseded_assignment_count"`
	TerminatedAssignmentCount int64           `json:"terminated_assignment_count"`
	SourceCount               int64           `json:"source_count"`
	GeneratedCount            int64           `json:"generated_count"`
	SuppressedCount           int64           `json:"suppressed_count"`
	TransitionCount           int64           `json:"transition_count"`
	MetricCount               int64           `json:"metric_count"`
	Revision                  int64           `json:"revision"`
	Metadata                  json.RawMessage `json:"metadata"`
}

// CityOpenWorldCommuteAssignmentEpoch is an immutable effective assignment
// identity. Later changes always create a successor epoch and a transition
// on the old one instead of mutating V12/V13 evidence.
type CityOpenWorldCommuteAssignmentEpoch struct {
	Code             string                       `json:"code"`
	BindingCode      string                       `json:"binding_code"`
	ActorCode        string                       `json:"actor_code"`
	EpochNumber      int64                        `json:"epoch_number"`
	AssignmentKind   string                       `json:"assignment_kind"`
	EmploymentRole   string                       `json:"employment_role_code"`
	HomeFacilityCode string                       `json:"home_facility_code"`
	HomeHubCode      string                       `json:"home_hub_code"`
	WorkFacilityCode string                       `json:"work_facility_code"`
	WorkHubCode      string                       `json:"work_hub_code"`
	PeriodTicks      int64                        `json:"period_ticks"`
	OutboundPhase    int64                        `json:"outbound_phase"`
	ReturnPhase      int64                        `json:"return_phase"`
	OriginKind       string                       `json:"origin_kind"`
	OpenedTick       int64                        `json:"opened_tick"`
	OpenedFact       *CityOpenWorldRuntimeFactRef `json:"opened_fact,omitempty"`
	Metadata         json.RawMessage              `json:"metadata"`
}

// CityOpenWorldCommuteAssignmentTransition is append-only. A nil SourceFact
// is permitted only for the genesis/upgrade baseline transition.
type CityOpenWorldCommuteAssignmentTransition struct {
	AssignmentCode string                       `json:"assignment_code"`
	TransitionTick int64                        `json:"transition_tick"`
	TransitionSeq  int64                        `json:"transition_sequence"`
	State          string                       `json:"state"`
	ReasonCode     string                       `json:"reason_code"`
	SourceFact     *CityOpenWorldRuntimeFactRef `json:"source_fact,omitempty"`
	Metadata       json.RawMessage              `json:"metadata"`
}

// CityOpenWorldCommuteLifecycleSource is a V14 direction source owned by an
// effective epoch. It deliberately uses a separate code namespace from V13
// so historical V13 closed-cycle metrics never absorb successor traffic.
type CityOpenWorldCommuteLifecycleSource struct {
	Code                    string                       `json:"code"`
	AssignmentCode          string                       `json:"assignment_code"`
	BindingCode             string                       `json:"binding_code"`
	ActorCode               string                       `json:"actor_code"`
	SourceKind              string                       `json:"source_kind"`
	Direction               string                       `json:"direction"`
	EmploymentRoleCode      string                       `json:"employment_role_code"`
	OriginFacilityCode      string                       `json:"origin_facility_code"`
	OriginHubCode           string                       `json:"origin_hub_code"`
	DestinationFacilityCode string                       `json:"destination_facility_code"`
	DestinationHubCode      string                       `json:"destination_hub_code"`
	ModeCode                string                       `json:"mode_code"`
	PurposeCode             string                       `json:"purpose_code"`
	RequestedUnits          int64                        `json:"requested_units"`
	Status                  string                       `json:"status"`
	PeriodTicks             int64                        `json:"period_ticks"`
	PhaseOffset             int64                        `json:"phase_offset"`
	NextDueTick             int64                        `json:"next_due_tick"`
	LastTransitionTick      int64                        `json:"last_transition_tick"`
	LastFact                *CityOpenWorldRuntimeFactRef `json:"last_fact,omitempty"`
	GeneratedCount          int64                        `json:"generated_count"`
	SuppressedCount         int64                        `json:"suppressed_count"`
	Version                 int64                        `json:"version"`
	Metadata                json.RawMessage              `json:"metadata"`
}

type CityOpenWorldCommuteLifecycleCycleMetric struct {
	CycleStartTick       int64                       `json:"cycle_start_tick"`
	CycleEndTick         int64                       `json:"cycle_end_tick"`
	ClosedTick           int64                       `json:"closed_tick"`
	SourceFact           CityOpenWorldRuntimeFactRef `json:"source_fact"`
	TransitionCount      int64                       `json:"transition_count"`
	RebindCount          int64                       `json:"rebind_count"`
	GeneratedCount       int64                       `json:"generated_count"`
	SuppressedCount      int64                       `json:"suppressed_count"`
	ScheduledDemandCount int64                       `json:"scheduled_demand_count"`
	CompletedDemandCount int64                       `json:"completed_demand_count"`
	ExpiredDemandCount   int64                       `json:"expired_demand_count"`
	PendingDemandCount   int64                       `json:"pending_demand_count"`
	ArrivalLandedCount   int64                       `json:"arrival_landed_count"`
	ArrivalBlockedCount  int64                       `json:"arrival_blocked_count"`
	ArrivalFailedCount   int64                       `json:"arrival_failed_count"`
	Metadata             json.RawMessage             `json:"metadata"`
}

type CityOpenWorldCommuteLifecycleState struct {
	Policy      CityOpenWorldCommuteLifecyclePolicy        `json:"policy"`
	Assignments []CityOpenWorldCommuteAssignmentEpoch      `json:"assignments"`
	Transitions []CityOpenWorldCommuteAssignmentTransition `json:"transitions"`
	Sources     []CityOpenWorldCommuteLifecycleSource      `json:"sources"`
	Metrics     []CityOpenWorldCommuteLifecycleCycleMetric `json:"metrics"`
}

type cityOpenWorldCommuteLifecycleSeed struct {
	actorID          int64
	bindingCode      string
	actorCode        string
	employmentRole   string
	homeFacilityCode string
	homeHubCode      string
	workFacilityCode string
	workHubCode      string
	periodTicks      int64
	outboundPhase    int64
	returnPhase      int64
}

func cityOpenWorldCommuteAssignmentEpochCode(bindingCode string, epochNumber int64) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("commute.assignment.epoch.v1\x00%s\x00%d", bindingCode, epochNumber)))
	return "commute.assignment." + hex.EncodeToString(sum[:20])
}

func cityOpenWorldCommuteLifecycleSourceCode(assignmentCode, direction string) string {
	sum := sha256.Sum256([]byte("commute.lifecycle.source.v1\x00" + assignmentCode + "\x00" + direction))
	return "commute.lifecycle.source." + hex.EncodeToString(sum[:20])
}

func cityOpenWorldCommuteLifecyclePolicyHash() (string, error) {
	raw, err := json.Marshal(struct {
		SchemaVersion          int    `json:"schema_version"`
		ProfileID              string `json:"profile_id"`
		ProfileVersion         string `json:"profile_version"`
		AssignmentContract     string `json:"assignment_contract"`
		SourceContract         string `json:"source_contract"`
		PeriodTicks            int64  `json:"period_ticks"`
		MaximumAssignments     int    `json:"maximum_assignments"`
		MaximumTransitionsTick int    `json:"maximum_transitions_tick"`
		MaximumGenerationsTick int    `json:"maximum_generations_tick"`
	}{
		SchemaVersion: cityOpenWorldCommuteLifecycleSchemaVersion,
		ProfileID:     cityOpenWorldCommuteLifecycleProfileID, ProfileVersion: cityOpenWorldCommuteLifecycleProfileVersion,
		AssignmentContract:     cityOpenWorldCommuteLifecycleAssignmentContract,
		SourceContract:         cityOpenWorldCommuteLifecycleSourceContract,
		PeriodTicks:            cityOpenWorldCommutePeriodTicks,
		MaximumAssignments:     cityOpenWorldCommuteLifecycleMaximumAssignments,
		MaximumTransitionsTick: cityOpenWorldCommuteLifecycleMaximumTransitionsTick,
		MaximumGenerationsTick: cityOpenWorldCommuteLifecycleMaximumGenerationsTick,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func activateCityOpenWorldCommuteLifecycleBootstrapWrite(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if _, err := tx.ExecContext(ctx, `
SELECT set_config('sub2api.city_open_world_commute_lifecycle_bootstrap_world_id', $1, TRUE)`, fmt.Sprintf("%d", worldID)); err != nil {
		return fmt.Errorf("enable open-world V14 commute lifecycle bootstrap: %w", err)
	}
	return nil
}

func activateCityOpenWorldCommuteLifecycleWrite(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if _, err := tx.ExecContext(ctx, `
SELECT set_config('sub2api.city_open_world_commute_lifecycle_world_id', $1, TRUE)`, fmt.Sprintf("%d", worldID)); err != nil {
		return fmt.Errorf("enable open-world V14 commute lifecycle write: %w", err)
	}
	return nil
}

// assertCityOpenWorldCommuteLifecycleFoundation keeps immediate Go-side
// validation aligned with the deferred database trigger. The base assertion
// checks aggregate counters; the successor assertion closes the gaps that can
// otherwise appear only after a rebinding or recovery chain is assembled.
func assertCityOpenWorldCommuteLifecycleFoundation(ctx context.Context, tx *sql.Tx, worldID int64) error {
	for _, assertion := range []string{
		"assert_city_open_world_commute_lifecycle_foundation",
		"assert_city_open_world_commute_lifecycle_successor_integrity",
	} {
		if _, err := tx.ExecContext(ctx, `SELECT `+assertion+`($1)`, worldID); err != nil {
			return fmt.Errorf("validate V14 commute lifecycle with %s: %w", assertion, err)
		}
	}
	return nil
}

// initializeCityOpenWorldV14CommuteLifecycleFoundation forks a fresh V14
// successor projection from immutable V12 bindings. The V13 source state is
// initialized separately and retained as historical audit evidence.
func initializeCityOpenWorldV14CommuteLifecycleFoundation(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if worldID <= 0 {
		return ErrCityInvalidInput
	}
	var simulationVersion string
	var baselineTick int64
	if err := tx.QueryRowContext(ctx, `
SELECT simulation_version, current_tick FROM city_worlds WHERE id = $1 FOR UPDATE`, worldID).Scan(&simulationVersion, &baselineTick); err != nil {
		return fmt.Errorf("load V14 commute lifecycle world: %w", err)
	}
	if !cityEngineSupportsOpenWorldCommuteLifecycle(simulationVersion) {
		return ErrCitySimulationVersion.WithMetadata(map[string]string{"version": simulationVersion})
	}
	if _, err := tx.ExecContext(ctx, `SELECT assert_city_open_world_commute_source_foundation($1)`, worldID); err != nil {
		return fmt.Errorf("validate V14 commute lifecycle source prerequisite: %w", err)
	}
	if err := activateCityOpenWorldCommuteLifecycleBootstrapWrite(ctx, tx, worldID); err != nil {
		return err
	}
	seeds, err := loadCityOpenWorldCommuteLifecycleSeeds(ctx, tx, worldID)
	if err != nil {
		return err
	}
	if len(seeds) > cityOpenWorldCommuteLifecycleMaximumAssignments {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v14_assignment_limit"})
	}
	policyHash, err := cityOpenWorldCommuteLifecyclePolicyHash()
	if err != nil {
		return fmt.Errorf("hash V14 commute lifecycle policy: %w", err)
	}
	metadata, err := json.Marshal(map[string]any{
		"schema_version":   cityOpenWorldCommuteLifecycleSchemaVersion,
		"baseline_adapter": "v12_binding_v13_source_history_v1",
		"rebind_contract":  "command_fact_successor_epoch_v1",
	})
	if err != nil {
		return fmt.Errorf("marshal V14 commute lifecycle profile metadata: %w", err)
	}
	policy := CityOpenWorldCommuteLifecyclePolicy{
		ProfileID: cityOpenWorldCommuteLifecycleProfileID, ProfileVersion: cityOpenWorldCommuteLifecycleProfileVersion,
		ContentHash: policyHash, BaselineTick: baselineTick,
		AssignmentContract:     cityOpenWorldCommuteLifecycleAssignmentContract,
		SourceContract:         cityOpenWorldCommuteLifecycleSourceContract,
		PeriodTicks:            cityOpenWorldCommutePeriodTicks,
		MaximumAssignments:     cityOpenWorldCommuteLifecycleMaximumAssignments,
		MaximumTransitionsTick: cityOpenWorldCommuteLifecycleMaximumTransitionsTick,
		MaximumGenerationsTick: cityOpenWorldCommuteLifecycleMaximumGenerationsTick,
		AssignmentCount:        int64(len(seeds)), ActiveAssignmentCount: int64(len(seeds)),
		SourceCount: int64(len(seeds) * 2), TransitionCount: int64(len(seeds)),
		Revision: 1, Metadata: metadata,
	}
	assignments := make([]CityOpenWorldCommuteAssignmentEpoch, 0, len(seeds))
	transitions := make([]CityOpenWorldCommuteAssignmentTransition, 0, len(seeds))
	sources := make([]CityOpenWorldCommuteLifecycleSource, 0, len(seeds)*2)
	for _, seed := range seeds {
		assignment := cityOpenWorldCommuteLifecycleAssignmentForSeed(seed, baselineTick)
		assignments = append(assignments, assignment)
		transitions = append(transitions, CityOpenWorldCommuteAssignmentTransition{
			AssignmentCode: assignment.Code, TransitionTick: baselineTick, TransitionSeq: 0,
			State: cityOpenWorldCommuteLifecycleStateActive, ReasonCode: cityOpenWorldCommuteLifecycleReasonBaseline,
			Metadata: json.RawMessage(`{"schema_version":1,"origin":"baseline"}`),
		})
		sources = append(sources, cityOpenWorldCommuteLifecycleSourcesForAssignment(assignment, baselineTick)...)
	}
	state := &CityOpenWorldCommuteLifecycleState{
		Policy: policy, Assignments: assignments, Transitions: transitions, Sources: sources,
		Metrics: make([]CityOpenWorldCommuteLifecycleCycleMetric, 0),
	}
	if err = validateCityOpenWorldCommuteLifecycleState(state); err != nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v14_commute_lifecycle"}).WithCause(err)
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_commute_lifecycle_profiles
    (world_id, profile_id, profile_version, content_hash, baseline_tick,
     assignment_contract, source_contract, period_ticks, maximum_assignments,
     maximum_transitions_tick, maximum_generations_tick, assignment_count,
     active_assignment_count, suspended_assignment_count, superseded_assignment_count,
     terminated_assignment_count, source_count, generated_count, suppressed_count,
     transition_count, metric_count, revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14,
        $15, $16, $17, $18, $19, $20, $21, $22, $23::jsonb)`,
		worldID, policy.ProfileID, policy.ProfileVersion, policy.ContentHash, policy.BaselineTick,
		policy.AssignmentContract, policy.SourceContract, policy.PeriodTicks, policy.MaximumAssignments,
		policy.MaximumTransitionsTick, policy.MaximumGenerationsTick, policy.AssignmentCount,
		policy.ActiveAssignmentCount, policy.SuspendedAssignmentCount, policy.SupersededAssignmentCount,
		policy.TerminatedAssignmentCount, policy.SourceCount, policy.GeneratedCount, policy.SuppressedCount,
		policy.TransitionCount, policy.MetricCount, policy.Revision, []byte(policy.Metadata)); err != nil {
		return fmt.Errorf("insert V14 commute lifecycle profile: %w", err)
	}
	for index := range assignments {
		assignment := assignments[index]
		seed := seeds[index]
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_commute_assignment_epochs
    (world_id, code, binding_code, actor_id, epoch_number, assignment_kind,
     employment_role_code, home_facility_code, home_hub_code, work_facility_code,
     work_hub_code, period_ticks, outbound_phase, return_phase, origin_kind,
     opened_tick, opened_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14,
        $15, $16, NULL, $17::jsonb)`,
			worldID, assignment.Code, assignment.BindingCode, seed.actorID, assignment.EpochNumber,
			assignment.AssignmentKind, assignment.EmploymentRole, assignment.HomeFacilityCode,
			assignment.HomeHubCode, assignment.WorkFacilityCode, assignment.WorkHubCode,
			assignment.PeriodTicks, assignment.OutboundPhase, assignment.ReturnPhase,
			assignment.OriginKind, assignment.OpenedTick, assignment.Metadata); err != nil {
			return fmt.Errorf("insert V14 commute assignment %s: %w", assignment.Code, err)
		}
		transition := transitions[index]
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_commute_assignment_transitions
    (world_id, assignment_code, transition_tick, transition_sequence, state,
     reason_code, source_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, NULL, $7::jsonb)`,
			worldID, transition.AssignmentCode, transition.TransitionTick, transition.TransitionSeq,
			transition.State, transition.ReasonCode, transition.Metadata); err != nil {
			return fmt.Errorf("insert V14 baseline transition %s: %w", assignment.Code, err)
		}
	}
	for _, source := range sources {
		actorID, found := cityOpenWorldCommuteLifecycleActorIDForCode(seeds, source.ActorCode)
		if !found {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v14_source_actor"})
		}
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_commute_lifecycle_sources
    (world_id, code, assignment_code, binding_code, actor_id, source_kind, direction,
     employment_role_code, origin_facility_code, origin_hub_code, destination_facility_code,
     destination_hub_code, mode_code, purpose_code, requested_units, status, period_ticks,
     phase_offset, next_due_tick, last_transition_tick, last_fact_id, generated_count,
     suppressed_count, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14,
        $15, $16, $17, $18, $19, $20, NULL, $21, $22, $23, $24::jsonb)`,
			worldID, source.Code, source.AssignmentCode, source.BindingCode, actorID,
			source.SourceKind, source.Direction, source.EmploymentRoleCode,
			source.OriginFacilityCode, source.OriginHubCode, source.DestinationFacilityCode,
			source.DestinationHubCode, source.ModeCode, source.PurposeCode, source.RequestedUnits,
			source.Status, source.PeriodTicks, source.PhaseOffset, source.NextDueTick,
			source.LastTransitionTick, source.GeneratedCount, source.SuppressedCount,
			source.Version, source.Metadata); err != nil {
			return fmt.Errorf("insert V14 commute lifecycle source %s: %w", source.Code, err)
		}
	}
	if err = assertCityOpenWorldCommuteLifecycleFoundation(ctx, tx, worldID); err != nil {
		return fmt.Errorf("validate V14 commute lifecycle foundation: %w", err)
	}
	return nil
}

func cityOpenWorldCommuteLifecycleActorIDForCode(seeds []cityOpenWorldCommuteLifecycleSeed, actorCode string) (int64, bool) {
	for _, seed := range seeds {
		if seed.actorCode == actorCode {
			return seed.actorID, true
		}
	}
	return 0, false
}

func loadCityOpenWorldCommuteLifecycleSeeds(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) ([]cityOpenWorldCommuteLifecycleSeed, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT binding.code, actor.id, actor.code, binding.employment_role_code,
       binding.home_facility_code, binding.home_hub_code,
       binding.work_facility_code, binding.work_hub_code,
       binding.period_ticks, binding.outbound_phase, binding.return_phase
FROM city_open_world_commute_bindings binding
JOIN city_open_world_actors actor
  ON actor.id = binding.actor_id AND actor.world_id = binding.world_id
WHERE binding.world_id = $1
ORDER BY binding.code`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V14 commute lifecycle binding seeds: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]cityOpenWorldCommuteLifecycleSeed, 0)
	for rows.Next() {
		item := cityOpenWorldCommuteLifecycleSeed{}
		if err = rows.Scan(&item.bindingCode, &item.actorID, &item.actorCode, &item.employmentRole,
			&item.homeFacilityCode, &item.homeHubCode, &item.workFacilityCode, &item.workHubCode,
			&item.periodTicks, &item.outboundPhase, &item.returnPhase); err != nil {
			return nil, fmt.Errorf("scan V14 commute lifecycle binding seed: %w", err)
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate V14 commute lifecycle binding seeds: %w", err)
	}
	return items, nil
}

func cityOpenWorldCommuteLifecycleAssignmentForSeed(
	seed cityOpenWorldCommuteLifecycleSeed,
	baselineTick int64,
) CityOpenWorldCommuteAssignmentEpoch {
	metadata := json.RawMessage(`{"schema_version":1,"binding_adapter":"v12_binding_v13_source_history_v1"}`)
	return CityOpenWorldCommuteAssignmentEpoch{
		Code: cityOpenWorldCommuteAssignmentEpochCode(seed.bindingCode, 1), BindingCode: seed.bindingCode,
		ActorCode: seed.actorCode, EpochNumber: 1, AssignmentKind: cityOpenWorldCommuteLifecycleAssignmentKind,
		EmploymentRole: seed.employmentRole, HomeFacilityCode: seed.homeFacilityCode, HomeHubCode: seed.homeHubCode,
		WorkFacilityCode: seed.workFacilityCode, WorkHubCode: seed.workHubCode,
		PeriodTicks: seed.periodTicks, OutboundPhase: seed.outboundPhase, ReturnPhase: seed.returnPhase,
		OriginKind: cityOpenWorldCommuteLifecycleOriginBaseline, OpenedTick: baselineTick, Metadata: metadata,
	}
}

func cityOpenWorldCommuteLifecycleSourcesForAssignment(
	assignment CityOpenWorldCommuteAssignmentEpoch,
	baselineTick int64,
) []CityOpenWorldCommuteLifecycleSource {
	items := make([]CityOpenWorldCommuteLifecycleSource, 0, 2)
	for _, direction := range []string{cityOpenWorldCommuteSourceDirectionOutbound, cityOpenWorldCommuteSourceDirectionReturn} {
		originFacility, originHub := assignment.HomeFacilityCode, assignment.HomeHubCode
		destinationFacility, destinationHub := assignment.WorkFacilityCode, assignment.WorkHubCode
		sourceKind, purpose, phase := cityOpenWorldCommuteLifecycleSourceKindOutbound, cityOpenWorldCommuteLifecyclePurposeOutbound, assignment.OutboundPhase
		if direction == cityOpenWorldCommuteSourceDirectionReturn {
			originFacility, originHub = assignment.WorkFacilityCode, assignment.WorkHubCode
			destinationFacility, destinationHub = assignment.HomeFacilityCode, assignment.HomeHubCode
			sourceKind, purpose, phase = cityOpenWorldCommuteLifecycleSourceKindReturn, cityOpenWorldCommuteLifecyclePurposeReturn, assignment.ReturnPhase
		}
		metadata := json.RawMessage(`{"schema_version":1,"source_adapter":"v14_assignment_epoch_v1"}`)
		items = append(items, CityOpenWorldCommuteLifecycleSource{
			Code: cityOpenWorldCommuteLifecycleSourceCode(assignment.Code, direction), AssignmentCode: assignment.Code,
			BindingCode: assignment.BindingCode, ActorCode: assignment.ActorCode, SourceKind: sourceKind, Direction: direction,
			EmploymentRoleCode: assignment.EmploymentRole, OriginFacilityCode: originFacility, OriginHubCode: originHub,
			DestinationFacilityCode: destinationFacility, DestinationHubCode: destinationHub,
			ModeCode: cityOpenWorldCommuteSourceModeWalk, PurposeCode: purpose, RequestedUnits: 1,
			Status: cityOpenWorldCommuteLifecycleSourceStatusActive, PeriodTicks: assignment.PeriodTicks,
			PhaseOffset: phase, NextDueTick: baselineTick + 1 + phase, LastTransitionTick: baselineTick,
			Version: 1, Metadata: metadata,
		})
	}
	return items
}

func validateCityOpenWorldCommuteLifecyclePolicy(policy CityOpenWorldCommuteLifecyclePolicy) error {
	if policy.ProfileID != cityOpenWorldCommuteLifecycleProfileID ||
		policy.ProfileVersion != cityOpenWorldCommuteLifecycleProfileVersion ||
		len(policy.ContentHash) != 64 || policy.BaselineTick < 0 ||
		policy.AssignmentContract != cityOpenWorldCommuteLifecycleAssignmentContract ||
		policy.SourceContract != cityOpenWorldCommuteLifecycleSourceContract ||
		policy.PeriodTicks != cityOpenWorldCommutePeriodTicks ||
		policy.MaximumAssignments != cityOpenWorldCommuteLifecycleMaximumAssignments ||
		policy.MaximumTransitionsTick != cityOpenWorldCommuteLifecycleMaximumTransitionsTick ||
		policy.MaximumGenerationsTick != cityOpenWorldCommuteLifecycleMaximumGenerationsTick ||
		policy.AssignmentCount < 0 || policy.AssignmentCount > int64(policy.MaximumAssignments) ||
		policy.ActiveAssignmentCount < 0 || policy.SuspendedAssignmentCount < 0 ||
		policy.SupersededAssignmentCount < 0 || policy.TerminatedAssignmentCount < 0 ||
		policy.ActiveAssignmentCount+policy.SuspendedAssignmentCount+policy.SupersededAssignmentCount+policy.TerminatedAssignmentCount != policy.AssignmentCount ||
		policy.SourceCount != policy.AssignmentCount*2 || policy.GeneratedCount < 0 || policy.SuppressedCount < 0 ||
		policy.TransitionCount < policy.AssignmentCount || policy.MetricCount < 0 || policy.Revision < 1 ||
		!json.Valid(policy.Metadata) {
		return fmt.Errorf("invalid V14 commute lifecycle policy")
	}
	hash, err := cityOpenWorldCommuteLifecyclePolicyHash()
	if err != nil || hash != policy.ContentHash {
		return fmt.Errorf("V14 commute lifecycle policy hash mismatch")
	}
	return nil
}

func cityOpenWorldCommuteLifecycleFactRefsEqual(left, right *CityOpenWorldRuntimeFactRef) bool {
	if left == nil || right == nil {
		return left == right
	}
	return left.Tick == right.Tick && left.Sequence == right.Sequence
}

func validateCityOpenWorldCommuteLifecycleState(state *CityOpenWorldCommuteLifecycleState) error {
	if state == nil || validateCityOpenWorldCommuteLifecyclePolicy(state.Policy) != nil ||
		state.Assignments == nil || state.Transitions == nil || state.Sources == nil || state.Metrics == nil {
		return fmt.Errorf("V14 commute lifecycle state is incomplete")
	}
	assignmentByCode := make(map[string]CityOpenWorldCommuteAssignmentEpoch, len(state.Assignments))
	assignmentEpochsByBinding := make(map[string]map[int64]CityOpenWorldCommuteAssignmentEpoch, len(state.Assignments))
	assignmentActorByBinding := make(map[string]string, len(state.Assignments))
	for _, assignment := range state.Assignments {
		if assignment.Code == "" || assignment.BindingCode == "" || assignment.ActorCode == "" || assignment.EpochNumber < 1 ||
			assignment.Code != cityOpenWorldCommuteAssignmentEpochCode(assignment.BindingCode, assignment.EpochNumber) ||
			assignment.AssignmentKind != cityOpenWorldCommuteLifecycleAssignmentKind ||
			assignment.EmploymentRole == "" || assignment.HomeFacilityCode == "" || assignment.HomeHubCode == "" ||
			assignment.WorkFacilityCode == "" || assignment.WorkHubCode == "" ||
			assignment.HomeFacilityCode == assignment.WorkFacilityCode || assignment.PeriodTicks != state.Policy.PeriodTicks ||
			assignment.OutboundPhase < 0 || assignment.OutboundPhase >= assignment.PeriodTicks ||
			assignment.ReturnPhase != (assignment.OutboundPhase+assignment.PeriodTicks/2)%assignment.PeriodTicks ||
			(assignment.OriginKind != cityOpenWorldCommuteLifecycleOriginBaseline && assignment.OriginKind != cityOpenWorldCommuteLifecycleOriginAdminRebind) ||
			(assignment.OriginKind == cityOpenWorldCommuteLifecycleOriginBaseline &&
				(assignment.EpochNumber != 1 || assignment.OpenedFact != nil)) ||
			(assignment.OriginKind == cityOpenWorldCommuteLifecycleOriginAdminRebind &&
				(assignment.EpochNumber <= 1 || assignment.OpenedFact == nil || assignment.OpenedFact.Tick != assignment.OpenedTick || assignment.OpenedFact.Sequence < 0)) ||
			assignment.OpenedTick < state.Policy.BaselineTick || !json.Valid(assignment.Metadata) {
			return fmt.Errorf("invalid V14 commute assignment %q", assignment.Code)
		}
		if _, duplicate := assignmentByCode[assignment.Code]; duplicate {
			return fmt.Errorf("duplicate V14 commute assignment %q", assignment.Code)
		}
		if assignmentEpochsByBinding[assignment.BindingCode] == nil {
			assignmentEpochsByBinding[assignment.BindingCode] = make(map[int64]CityOpenWorldCommuteAssignmentEpoch)
		}
		if _, duplicate := assignmentEpochsByBinding[assignment.BindingCode][assignment.EpochNumber]; duplicate {
			return fmt.Errorf("duplicate V14 commute assignment epoch %q/%d", assignment.BindingCode, assignment.EpochNumber)
		}
		if actorCode, found := assignmentActorByBinding[assignment.BindingCode]; found && actorCode != assignment.ActorCode {
			return fmt.Errorf("V14 commute assignment binding %q changed actor", assignment.BindingCode)
		}
		assignmentEpochsByBinding[assignment.BindingCode][assignment.EpochNumber] = assignment
		assignmentActorByBinding[assignment.BindingCode] = assignment.ActorCode
		assignmentByCode[assignment.Code] = assignment
	}
	if int64(len(assignmentByCode)) != state.Policy.AssignmentCount {
		return fmt.Errorf("V14 commute assignment count mismatch")
	}
	for bindingCode, epochs := range assignmentEpochsByBinding {
		for epochNumber := int64(1); epochNumber <= int64(len(epochs)); epochNumber++ {
			assignment, found := epochs[epochNumber]
			if !found {
				return fmt.Errorf("V14 commute assignment binding %q has a gap before epoch %d", bindingCode, epochNumber)
			}
			if epochNumber == 1 && assignment.OriginKind != cityOpenWorldCommuteLifecycleOriginBaseline {
				return fmt.Errorf("V14 commute assignment binding %q lacks a baseline epoch", bindingCode)
			}
			if epochNumber > 1 && assignment.OriginKind != cityOpenWorldCommuteLifecycleOriginAdminRebind {
				return fmt.Errorf("V14 commute assignment binding %q has a non-successor epoch", bindingCode)
			}
		}
	}
	latest := make(map[string]CityOpenWorldCommuteAssignmentTransition, len(assignmentByCode))
	first := make(map[string]CityOpenWorldCommuteAssignmentTransition, len(assignmentByCode))
	transitionCounts := map[string]int64{
		cityOpenWorldCommuteLifecycleStateActive:     0,
		cityOpenWorldCommuteLifecycleStateSuspended:  0,
		cityOpenWorldCommuteLifecycleStateSuperseded: 0,
		cityOpenWorldCommuteLifecycleStateTerminated: 0,
	}
	for _, transition := range state.Transitions {
		assignment, found := assignmentByCode[transition.AssignmentCode]
		if !found || transition.TransitionTick < assignment.OpenedTick || transition.TransitionSeq < 0 ||
			!cityOpenWorldCommuteLifecycleTransitionStateValid(transition.State) || transition.ReasonCode == "" ||
			(transition.SourceFact != nil && (transition.SourceFact.Tick != transition.TransitionTick || transition.SourceFact.Sequence < 0)) ||
			!json.Valid(transition.Metadata) {
			return fmt.Errorf("invalid V14 commute assignment transition %q", transition.AssignmentCode)
		}
		if transition.SourceFact == nil && (transition.State != cityOpenWorldCommuteLifecycleStateActive ||
			transition.ReasonCode != cityOpenWorldCommuteLifecycleReasonBaseline || transition.TransitionTick != assignment.OpenedTick || transition.TransitionSeq != 0) {
			return fmt.Errorf("V14 commute transition without fact is not baseline")
		}
		previous, exists := latest[transition.AssignmentCode]
		if !exists {
			first[transition.AssignmentCode] = transition
		}
		if exists {
			if transition.TransitionTick < previous.TransitionTick ||
				(transition.TransitionTick == previous.TransitionTick && transition.TransitionSeq <= previous.TransitionSeq) ||
				!cityOpenWorldCommuteLifecycleTransitionAllowed(previous.State, transition.State) {
				return fmt.Errorf("invalid V14 commute transition order for %q", transition.AssignmentCode)
			}
		}
		latest[transition.AssignmentCode] = transition
	}
	if int64(len(state.Transitions)) != state.Policy.TransitionCount || len(latest) != len(assignmentByCode) {
		return fmt.Errorf("V14 commute lifecycle transition count mismatch")
	}
	effectiveByBinding := make(map[string]int, len(assignmentEpochsByBinding))
	for assignmentCode, transition := range latest {
		assignment := assignmentByCode[assignmentCode]
		initial := first[assignmentCode]
		if assignment.OriginKind == cityOpenWorldCommuteLifecycleOriginBaseline {
			if initial.State != cityOpenWorldCommuteLifecycleStateActive || initial.ReasonCode != cityOpenWorldCommuteLifecycleReasonBaseline ||
				initial.SourceFact != nil || initial.TransitionTick != assignment.OpenedTick || initial.TransitionSeq != 0 {
				return fmt.Errorf("V14 commute assignment %q has an invalid baseline transition", assignmentCode)
			}
		} else if initial.State != cityOpenWorldCommuteLifecycleStateActive ||
			!cityOpenWorldCommuteLifecycleFactRefsEqual(initial.SourceFact, assignment.OpenedFact) ||
			initial.TransitionTick != assignment.OpenedTick || initial.TransitionSeq != assignment.OpenedFact.Sequence {
			return fmt.Errorf("V14 commute assignment %q has an invalid successor opening transition", assignmentCode)
		}
		transitionCounts[transition.State]++
		if transition.State == cityOpenWorldCommuteLifecycleStateActive || transition.State == cityOpenWorldCommuteLifecycleStateSuspended {
			effectiveByBinding[assignment.BindingCode]++
			if effectiveByBinding[assignment.BindingCode] > 1 {
				return fmt.Errorf("V14 commute binding %q has multiple effective epochs", assignment.BindingCode)
			}
		}
	}
	if transitionCounts[cityOpenWorldCommuteLifecycleStateActive] != state.Policy.ActiveAssignmentCount ||
		transitionCounts[cityOpenWorldCommuteLifecycleStateSuspended] != state.Policy.SuspendedAssignmentCount ||
		transitionCounts[cityOpenWorldCommuteLifecycleStateSuperseded] != state.Policy.SupersededAssignmentCount ||
		transitionCounts[cityOpenWorldCommuteLifecycleStateTerminated] != state.Policy.TerminatedAssignmentCount {
		return fmt.Errorf("V14 commute lifecycle state counters mismatch")
	}
	sourceByAssignment := make(map[string]map[string]CityOpenWorldCommuteLifecycleSource, len(assignmentByCode))
	var generated, suppressed int64
	for _, source := range state.Sources {
		assignment, found := assignmentByCode[source.AssignmentCode]
		activityCount := source.GeneratedCount + source.SuppressedCount
		if !found || source.Code != cityOpenWorldCommuteLifecycleSourceCode(source.AssignmentCode, source.Direction) ||
			source.BindingCode != assignment.BindingCode || source.ActorCode != assignment.ActorCode ||
			source.EmploymentRoleCode != assignment.EmploymentRole || source.Status != cityOpenWorldCommuteLifecycleSourceStatusActive ||
			source.PeriodTicks != assignment.PeriodTicks || source.PhaseOffset < 0 || source.PhaseOffset >= source.PeriodTicks ||
			source.RequestedUnits != 1 || source.ModeCode != cityOpenWorldCommuteSourceModeWalk ||
			source.NextDueTick <= source.LastTransitionTick || source.GeneratedCount < 0 || source.SuppressedCount < 0 ||
			source.Version != 1+source.GeneratedCount+source.SuppressedCount ||
			(source.LastFact != nil && (source.LastFact.Tick != source.LastTransitionTick || source.LastFact.Sequence < 0)) ||
			(activityCount > 0 && source.LastFact == nil) || !json.Valid(source.Metadata) {
			return fmt.Errorf("invalid V14 commute lifecycle source %q", source.Code)
		}
		if activityCount == 0 {
			if source.LastTransitionTick != assignment.OpenedTick ||
				source.NextDueTick != assignment.OpenedTick+1+source.PhaseOffset {
				return fmt.Errorf("V14 commute lifecycle source %q has an invalid initial cadence", source.Code)
			}
			if assignment.OriginKind == cityOpenWorldCommuteLifecycleOriginBaseline && source.LastFact != nil {
				return fmt.Errorf("V14 baseline commute lifecycle source %q has an opening fact", source.Code)
			}
			if assignment.OriginKind == cityOpenWorldCommuteLifecycleOriginAdminRebind &&
				!cityOpenWorldCommuteLifecycleFactRefsEqual(source.LastFact, assignment.OpenedFact) {
				return fmt.Errorf("V14 successor commute lifecycle source %q lacks its opening fact", source.Code)
			}
		} else if source.NextDueTick != source.LastTransitionTick+source.PeriodTicks {
			return fmt.Errorf("V14 commute lifecycle source %q has an invalid advanced cadence", source.Code)
		}
		if sourceByAssignment[source.AssignmentCode] == nil {
			sourceByAssignment[source.AssignmentCode] = make(map[string]CityOpenWorldCommuteLifecycleSource, 2)
		}
		if _, duplicate := sourceByAssignment[source.AssignmentCode][source.Direction]; duplicate ||
			!cityOpenWorldCommuteLifecycleSourceMatchesAssignment(source, assignment) {
			return fmt.Errorf("invalid V14 commute lifecycle source identity %q", source.Code)
		}
		sourceByAssignment[source.AssignmentCode][source.Direction] = source
		generated += source.GeneratedCount
		suppressed += source.SuppressedCount
	}
	if int64(len(state.Sources)) != state.Policy.SourceCount || generated != state.Policy.GeneratedCount || suppressed != state.Policy.SuppressedCount {
		return fmt.Errorf("V14 commute lifecycle source counters mismatch")
	}
	for assignmentCode := range assignmentByCode {
		pair := sourceByAssignment[assignmentCode]
		if len(pair) != 2 || pair[cityOpenWorldCommuteSourceDirectionOutbound].Code == "" || pair[cityOpenWorldCommuteSourceDirectionReturn].Code == "" {
			return fmt.Errorf("V14 commute assignment %q does not have a source pair", assignmentCode)
		}
	}
	if int64(len(state.Metrics)) != state.Policy.MetricCount {
		return fmt.Errorf("V14 commute lifecycle metric count mismatch")
	}
	nextCycleStart := state.Policy.BaselineTick + 1
	for index, metric := range state.Metrics {
		if metric.CycleStartTick <= state.Policy.BaselineTick || metric.CycleEndTick < metric.CycleStartTick ||
			metric.CycleStartTick != nextCycleStart || metric.CycleEndTick-metric.CycleStartTick+1 != state.Policy.PeriodTicks ||
			metric.ClosedTick != metric.CycleEndTick+1 || metric.SourceFact.Tick != metric.ClosedTick || metric.SourceFact.Sequence < 0 ||
			metric.TransitionCount < 0 || metric.RebindCount < 0 || metric.GeneratedCount < 0 || metric.SuppressedCount < 0 ||
			metric.ScheduledDemandCount < 0 || metric.CompletedDemandCount < 0 || metric.ExpiredDemandCount < 0 ||
			metric.PendingDemandCount < 0 || metric.ArrivalLandedCount < 0 || metric.ArrivalBlockedCount < 0 || metric.ArrivalFailedCount < 0 ||
			!json.Valid(metric.Metadata) {
			return fmt.Errorf("invalid V14 commute lifecycle metric")
		}
		if index > 0 && state.Metrics[index-1].CycleStartTick >= metric.CycleStartTick {
			return fmt.Errorf("V14 commute lifecycle metrics are not ordered")
		}
		nextCycleStart = metric.CycleEndTick + 1
	}
	return nil
}

func cityOpenWorldCommuteLifecycleTransitionStateValid(state string) bool {
	return state == cityOpenWorldCommuteLifecycleStateActive || state == cityOpenWorldCommuteLifecycleStateSuspended ||
		state == cityOpenWorldCommuteLifecycleStateSuperseded || state == cityOpenWorldCommuteLifecycleStateTerminated
}

func cityOpenWorldCommuteLifecycleTransitionAllowed(previous, next string) bool {
	switch previous {
	case cityOpenWorldCommuteLifecycleStateActive:
		return next == cityOpenWorldCommuteLifecycleStateSuspended || next == cityOpenWorldCommuteLifecycleStateSuperseded || next == cityOpenWorldCommuteLifecycleStateTerminated
	case cityOpenWorldCommuteLifecycleStateSuspended:
		return next == cityOpenWorldCommuteLifecycleStateActive || next == cityOpenWorldCommuteLifecycleStateSuperseded || next == cityOpenWorldCommuteLifecycleStateTerminated
	default:
		return false
	}
}

func cityOpenWorldCommuteLifecycleSourceMatchesAssignment(source CityOpenWorldCommuteLifecycleSource, assignment CityOpenWorldCommuteAssignmentEpoch) bool {
	if source.Direction == cityOpenWorldCommuteSourceDirectionOutbound {
		return source.SourceKind == cityOpenWorldCommuteLifecycleSourceKindOutbound && source.PurposeCode == cityOpenWorldCommuteLifecyclePurposeOutbound &&
			source.PhaseOffset == assignment.OutboundPhase && source.OriginFacilityCode == assignment.HomeFacilityCode &&
			source.OriginHubCode == assignment.HomeHubCode && source.DestinationFacilityCode == assignment.WorkFacilityCode && source.DestinationHubCode == assignment.WorkHubCode
	}
	return source.Direction == cityOpenWorldCommuteSourceDirectionReturn && source.SourceKind == cityOpenWorldCommuteLifecycleSourceKindReturn &&
		source.PurposeCode == cityOpenWorldCommuteLifecyclePurposeReturn && source.PhaseOffset == assignment.ReturnPhase &&
		source.OriginFacilityCode == assignment.WorkFacilityCode && source.OriginHubCode == assignment.WorkHubCode &&
		source.DestinationFacilityCode == assignment.HomeFacilityCode && source.DestinationHubCode == assignment.HomeHubCode
}

func loadCityOpenWorldCommuteLifecycleState(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*CityOpenWorldCommuteLifecycleState, error) {
	state := &CityOpenWorldCommuteLifecycleState{
		Assignments: make([]CityOpenWorldCommuteAssignmentEpoch, 0),
		Transitions: make([]CityOpenWorldCommuteAssignmentTransition, 0),
		Sources:     make([]CityOpenWorldCommuteLifecycleSource, 0),
		Metrics:     make([]CityOpenWorldCommuteLifecycleCycleMetric, 0),
	}
	err := queryer.QueryRowContext(ctx, `
SELECT profile_id, profile_version, content_hash, baseline_tick, assignment_contract,
       source_contract, period_ticks, maximum_assignments, maximum_transitions_tick,
       maximum_generations_tick, assignment_count, active_assignment_count,
       suspended_assignment_count, superseded_assignment_count, terminated_assignment_count,
       source_count, generated_count, suppressed_count, transition_count, metric_count,
       revision, metadata
FROM city_open_world_commute_lifecycle_profiles
WHERE world_id = $1`, worldID).Scan(
		&state.Policy.ProfileID, &state.Policy.ProfileVersion, &state.Policy.ContentHash, &state.Policy.BaselineTick,
		&state.Policy.AssignmentContract, &state.Policy.SourceContract, &state.Policy.PeriodTicks,
		&state.Policy.MaximumAssignments, &state.Policy.MaximumTransitionsTick, &state.Policy.MaximumGenerationsTick,
		&state.Policy.AssignmentCount, &state.Policy.ActiveAssignmentCount, &state.Policy.SuspendedAssignmentCount,
		&state.Policy.SupersededAssignmentCount, &state.Policy.TerminatedAssignmentCount, &state.Policy.SourceCount,
		&state.Policy.GeneratedCount, &state.Policy.SuppressedCount, &state.Policy.TransitionCount,
		&state.Policy.MetricCount, &state.Policy.Revision, &state.Policy.Metadata,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v14_commute_lifecycle_profile"})
	}
	if err != nil {
		return nil, fmt.Errorf("load V14 commute lifecycle profile: %w", err)
	}
	assignmentRows, err := queryer.QueryContext(ctx, `
SELECT epoch.code, epoch.binding_code, actor.code, epoch.epoch_number,
       epoch.assignment_kind, epoch.employment_role_code, epoch.home_facility_code,
       epoch.home_hub_code, epoch.work_facility_code, epoch.work_hub_code,
       epoch.period_ticks, epoch.outbound_phase, epoch.return_phase, epoch.origin_kind,
       epoch.opened_tick, opened_fact.tick, opened_fact.sequence, epoch.metadata
FROM city_open_world_commute_assignment_epochs epoch
JOIN city_open_world_actors actor
  ON actor.id = epoch.actor_id AND actor.world_id = epoch.world_id
LEFT JOIN city_open_world_runtime_facts opened_fact
  ON opened_fact.id = epoch.opened_fact_id AND opened_fact.world_id = epoch.world_id
WHERE epoch.world_id = $1
ORDER BY epoch.binding_code, epoch.epoch_number`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V14 commute assignments: %w", err)
	}
	for assignmentRows.Next() {
		item := CityOpenWorldCommuteAssignmentEpoch{}
		var factTick, factSequence sql.NullInt64
		if err = assignmentRows.Scan(&item.Code, &item.BindingCode, &item.ActorCode, &item.EpochNumber,
			&item.AssignmentKind, &item.EmploymentRole, &item.HomeFacilityCode, &item.HomeHubCode,
			&item.WorkFacilityCode, &item.WorkHubCode, &item.PeriodTicks, &item.OutboundPhase,
			&item.ReturnPhase, &item.OriginKind, &item.OpenedTick, &factTick, &factSequence, &item.Metadata); err != nil {
			_ = assignmentRows.Close()
			return nil, fmt.Errorf("scan V14 commute assignment: %w", err)
		}
		if factTick.Valid {
			item.OpenedFact = &CityOpenWorldRuntimeFactRef{Tick: factTick.Int64, Sequence: factSequence.Int64}
		}
		state.Assignments = append(state.Assignments, item)
	}
	if err = closeCityRows(assignmentRows, "iterate V14 commute assignments"); err != nil {
		return nil, err
	}
	transitionRows, err := queryer.QueryContext(ctx, `
SELECT transition.assignment_code, transition.transition_tick, transition.transition_sequence,
       transition.state, transition.reason_code, fact.tick, fact.sequence, transition.metadata
FROM city_open_world_commute_assignment_transitions transition
LEFT JOIN city_open_world_runtime_facts fact
  ON fact.id = transition.source_fact_id AND fact.world_id = transition.world_id
WHERE transition.world_id = $1
ORDER BY transition.assignment_code, transition.transition_tick, transition.transition_sequence`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V14 commute transitions: %w", err)
	}
	for transitionRows.Next() {
		item := CityOpenWorldCommuteAssignmentTransition{}
		var factTick, factSequence sql.NullInt64
		if err = transitionRows.Scan(&item.AssignmentCode, &item.TransitionTick, &item.TransitionSeq,
			&item.State, &item.ReasonCode, &factTick, &factSequence, &item.Metadata); err != nil {
			_ = transitionRows.Close()
			return nil, fmt.Errorf("scan V14 commute transition: %w", err)
		}
		if factTick.Valid {
			item.SourceFact = &CityOpenWorldRuntimeFactRef{Tick: factTick.Int64, Sequence: factSequence.Int64}
		}
		state.Transitions = append(state.Transitions, item)
	}
	if err = closeCityRows(transitionRows, "iterate V14 commute transitions"); err != nil {
		return nil, err
	}
	sourceRows, err := queryer.QueryContext(ctx, `
SELECT source.code, source.assignment_code, source.binding_code, actor.code,
       source.source_kind, source.direction, source.employment_role_code,
       source.origin_facility_code, source.origin_hub_code, source.destination_facility_code,
       source.destination_hub_code, source.mode_code, source.purpose_code, source.requested_units,
       source.status, source.period_ticks, source.phase_offset, source.next_due_tick,
       source.last_transition_tick, last_fact.tick, last_fact.sequence,
       source.generated_count, source.suppressed_count, source.version, source.metadata
FROM city_open_world_commute_lifecycle_sources source
JOIN city_open_world_actors actor
  ON actor.id = source.actor_id AND actor.world_id = source.world_id
LEFT JOIN city_open_world_runtime_facts last_fact
  ON last_fact.id = source.last_fact_id AND last_fact.world_id = source.world_id
WHERE source.world_id = $1
ORDER BY source.code`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V14 commute lifecycle sources: %w", err)
	}
	for sourceRows.Next() {
		item := CityOpenWorldCommuteLifecycleSource{}
		var factTick, factSequence sql.NullInt64
		if err = sourceRows.Scan(&item.Code, &item.AssignmentCode, &item.BindingCode, &item.ActorCode,
			&item.SourceKind, &item.Direction, &item.EmploymentRoleCode, &item.OriginFacilityCode,
			&item.OriginHubCode, &item.DestinationFacilityCode, &item.DestinationHubCode, &item.ModeCode,
			&item.PurposeCode, &item.RequestedUnits, &item.Status, &item.PeriodTicks, &item.PhaseOffset,
			&item.NextDueTick, &item.LastTransitionTick, &factTick, &factSequence, &item.GeneratedCount,
			&item.SuppressedCount, &item.Version, &item.Metadata); err != nil {
			_ = sourceRows.Close()
			return nil, fmt.Errorf("scan V14 commute lifecycle source: %w", err)
		}
		if factTick.Valid {
			item.LastFact = &CityOpenWorldRuntimeFactRef{Tick: factTick.Int64, Sequence: factSequence.Int64}
		}
		state.Sources = append(state.Sources, item)
	}
	if err = closeCityRows(sourceRows, "iterate V14 commute lifecycle sources"); err != nil {
		return nil, err
	}
	metricRows, err := queryer.QueryContext(ctx, `
SELECT metric.cycle_start_tick, metric.cycle_end_tick, metric.closed_tick,
       fact.tick, fact.sequence, metric.transition_count, metric.rebind_count,
       metric.generated_count, metric.suppressed_count, metric.scheduled_demand_count,
       metric.completed_demand_count, metric.expired_demand_count, metric.pending_demand_count,
       metric.arrival_landed_count, metric.arrival_blocked_count, metric.arrival_failed_count,
       metric.metadata
FROM city_open_world_commute_lifecycle_cycle_metrics metric
JOIN city_open_world_runtime_facts fact
  ON fact.id = metric.source_fact_id AND fact.world_id = metric.world_id
WHERE metric.world_id = $1
ORDER BY metric.cycle_start_tick`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V14 commute lifecycle metrics: %w", err)
	}
	for metricRows.Next() {
		item := CityOpenWorldCommuteLifecycleCycleMetric{}
		if err = metricRows.Scan(&item.CycleStartTick, &item.CycleEndTick, &item.ClosedTick,
			&item.SourceFact.Tick, &item.SourceFact.Sequence, &item.TransitionCount, &item.RebindCount,
			&item.GeneratedCount, &item.SuppressedCount, &item.ScheduledDemandCount,
			&item.CompletedDemandCount, &item.ExpiredDemandCount, &item.PendingDemandCount,
			&item.ArrivalLandedCount, &item.ArrivalBlockedCount, &item.ArrivalFailedCount,
			&item.Metadata); err != nil {
			_ = metricRows.Close()
			return nil, fmt.Errorf("scan V14 commute lifecycle metric: %w", err)
		}
		state.Metrics = append(state.Metrics, item)
	}
	if err = closeCityRows(metricRows, "iterate V14 commute lifecycle metrics"); err != nil {
		return nil, err
	}
	sortCityOpenWorldCommuteLifecycleState(state)
	if err = validateCityOpenWorldCommuteLifecycleState(state); err != nil {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v14_commute_lifecycle"}).WithCause(err)
	}
	return state, nil
}

func sortCityOpenWorldCommuteLifecycleState(state *CityOpenWorldCommuteLifecycleState) {
	if state == nil {
		return
	}
	sort.Slice(state.Assignments, func(i, j int) bool {
		if state.Assignments[i].BindingCode == state.Assignments[j].BindingCode {
			return state.Assignments[i].EpochNumber < state.Assignments[j].EpochNumber
		}
		return state.Assignments[i].BindingCode < state.Assignments[j].BindingCode
	})
	sort.Slice(state.Transitions, func(i, j int) bool {
		if state.Transitions[i].AssignmentCode == state.Transitions[j].AssignmentCode {
			if state.Transitions[i].TransitionTick == state.Transitions[j].TransitionTick {
				return state.Transitions[i].TransitionSeq < state.Transitions[j].TransitionSeq
			}
			return state.Transitions[i].TransitionTick < state.Transitions[j].TransitionTick
		}
		return state.Transitions[i].AssignmentCode < state.Transitions[j].AssignmentCode
	})
	sort.Slice(state.Sources, func(i, j int) bool { return state.Sources[i].Code < state.Sources[j].Code })
	sort.Slice(state.Metrics, func(i, j int) bool { return state.Metrics[i].CycleStartTick < state.Metrics[j].CycleStartTick })
}

func (s *CityEconomyService) GetCityOpenWorldCommuteLifecycleState(
	ctx context.Context,
	userID, worldID int64,
) (*CityOpenWorldCommuteLifecycleState, error) {
	if userID <= 0 || worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, userID, worldID); err != nil {
		return nil, err
	}
	var version string
	if err := s.db.QueryRowContext(ctx, `SELECT simulation_version FROM city_worlds WHERE id = $1`, worldID).Scan(&version); err != nil {
		return nil, fmt.Errorf("load V14 commute lifecycle world version: %w", err)
	}
	if !cityEngineSupportsOpenWorldCommuteLifecycle(version) {
		return nil, ErrCitySimulationVersion.WithMetadata(map[string]string{"version": version})
	}
	state, err := loadCityOpenWorldCommuteLifecycleState(ctx, s.db, worldID)
	if err != nil {
		return nil, err
	}
	all, err := cityOpenWorldServiceMayReadAll(ctx, s.db, userID, worldID)
	if err != nil || all {
		return state, err
	}
	visible, err := cityOpenWorldServiceVisibleActorCodes(ctx, s.db, userID, worldID)
	if err != nil {
		return nil, err
	}
	assignmentCodes := make(map[string]struct{}, len(state.Assignments))
	assignments := make([]CityOpenWorldCommuteAssignmentEpoch, 0, len(state.Assignments))
	for _, assignment := range state.Assignments {
		if _, found := visible[assignment.ActorCode]; found {
			assignments = append(assignments, assignment)
			assignmentCodes[assignment.Code] = struct{}{}
		}
	}
	transitions := make([]CityOpenWorldCommuteAssignmentTransition, 0, len(state.Transitions))
	for _, transition := range state.Transitions {
		if _, found := assignmentCodes[transition.AssignmentCode]; found {
			transitions = append(transitions, transition)
		}
	}
	sources := make([]CityOpenWorldCommuteLifecycleSource, 0, len(state.Sources))
	for _, source := range state.Sources {
		if _, found := assignmentCodes[source.AssignmentCode]; found {
			sources = append(sources, source)
		}
	}
	state.Assignments, state.Transitions, state.Sources = assignments, transitions, sources
	return state, nil
}
