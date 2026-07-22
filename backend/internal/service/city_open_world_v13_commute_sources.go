package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
)

const (
	cityOpenWorldCommuteSourceSchemaVersion                = 1
	cityOpenWorldCommuteSourceProfileID                    = "sub2api-open-world-commute-source"
	cityOpenWorldCommuteSourceProfileVersion               = "1.0.0"
	cityOpenWorldCommuteSourceGenerationContract           = "verified_facility_presence_od_v1"
	cityOpenWorldCommuteSourceOriginContract               = "facility_interior_or_surface_egress_v1"
	cityOpenWorldCommuteSourceDirectionOutbound            = "outbound"
	cityOpenWorldCommuteSourceDirectionReturn              = "return"
	cityOpenWorldCommuteSourceKindOutbound                 = "npc.residence_to_work"
	cityOpenWorldCommuteSourceKindReturn                   = "npc.work_to_residence"
	cityOpenWorldCommuteSourceStatusActive                 = "active"
	cityOpenWorldCommuteSourceModeWalk                     = "walk"
	cityOpenWorldCommuteSourcePurposeOutbound              = "routine.commute.outbound"
	cityOpenWorldCommuteSourcePurposeReturn                = "routine.commute.return"
	cityOpenWorldCommuteSourceSurfaceEgressRadius    int64 = 24
	cityOpenWorldCommuteSourceMaximumGenerationsTick       = 128
)

// CityOpenWorldCommuteSourcePolicy seals V13's two-direction commute demand
// adapter. It does not own V9 routes or V10 arrivals; it only owns the
// verified decision that a binding may create a demand at a particular tick.
type CityOpenWorldCommuteSourcePolicy struct {
	ProfileID              string          `json:"profile_id"`
	ProfileVersion         string          `json:"profile_version"`
	ContentHash            string          `json:"content_hash"`
	BaselineTick           int64           `json:"baseline_tick"`
	GenerationContract     string          `json:"generation_contract"`
	OriginContract         string          `json:"origin_contract"`
	PeriodTicks            int64           `json:"period_ticks"`
	SurfaceEgressRadius    int64           `json:"surface_egress_radius"`
	MaximumGenerationsTick int             `json:"maximum_generations_tick"`
	SourceCount            int64           `json:"source_count"`
	GeneratedCount         int64           `json:"generated_count"`
	SuppressedCount        int64           `json:"suppressed_count"`
	MetricCount            int64           `json:"metric_count"`
	Revision               int64           `json:"revision"`
	Metadata               json.RawMessage `json:"metadata"`
}

// CityOpenWorldCommuteSource is one immutable direction from a V12 binding.
// Dynamic source lifecycle is limited to due/fact/counter fields so V12's
// historical residence/employment identity never becomes a mutable shortcut.
type CityOpenWorldCommuteSource struct {
	Code                    string                       `json:"code"`
	BindingCode             string                       `json:"binding_code"`
	SourceKind              string                       `json:"source_kind"`
	Direction               string                       `json:"direction"`
	ActorCode               string                       `json:"actor_code"`
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

// CityOpenWorldCommuteSourceCycleMetric only aggregates demand/arrival rows
// carrying V13's strong source metadata. It never labels player/V11 traffic as
// commute traffic merely because an actor happens to have a commute binding.
type CityOpenWorldCommuteSourceCycleMetric struct {
	CycleStartTick                 int64                       `json:"cycle_start_tick"`
	CycleEndTick                   int64                       `json:"cycle_end_tick"`
	ClosedTick                     int64                       `json:"closed_tick"`
	SourceFact                     CityOpenWorldRuntimeFactRef `json:"source_fact"`
	OutboundGeneratedCount         int64                       `json:"outbound_generated_count"`
	OutboundSuppressedCount        int64                       `json:"outbound_suppressed_count"`
	OutboundOriginUnavailableCount int64                       `json:"outbound_origin_unavailable_count"`
	ReturnGeneratedCount           int64                       `json:"return_generated_count"`
	ReturnSuppressedCount          int64                       `json:"return_suppressed_count"`
	ReturnOriginUnavailableCount   int64                       `json:"return_origin_unavailable_count"`
	ScheduledDemandCount           int64                       `json:"scheduled_demand_count"`
	CompletedDemandCount           int64                       `json:"completed_demand_count"`
	ExpiredDemandCount             int64                       `json:"expired_demand_count"`
	PendingDemandCount             int64                       `json:"pending_demand_count"`
	ArrivalLandedCount             int64                       `json:"arrival_landed_count"`
	ArrivalBlockedCount            int64                       `json:"arrival_blocked_count"`
	ArrivalFailedCount             int64                       `json:"arrival_failed_count"`
	Metadata                       json.RawMessage             `json:"metadata"`
}

// CityOpenWorldCommuteSourceState is the V13 dynamic adapter state. The V12
// bindings remain a sibling canonical projection rather than being copied or
// re-owned by this source state.
type CityOpenWorldCommuteSourceState struct {
	Policy  CityOpenWorldCommuteSourcePolicy        `json:"policy"`
	Sources []CityOpenWorldCommuteSource            `json:"sources"`
	Metrics []CityOpenWorldCommuteSourceCycleMetric `json:"metrics"`
}

type cityOpenWorldCommuteSourceSeed struct {
	actorID            int64
	bindingCode        string
	actorCode          string
	employmentRoleCode string
	homeFacilityCode   string
	homeHubCode        string
	workFacilityCode   string
	workHubCode        string
	periodTicks        int64
	outboundPhase      int64
	returnPhase        int64
}

func cityOpenWorldCommuteSourceCode(bindingCode, direction string) string {
	sum := sha256.Sum256([]byte("npc.commute.source.v1\x00" + bindingCode + "\x00" + direction))
	return "commute.source." + hex.EncodeToString(sum[:20])
}

func cityOpenWorldCommuteSourcePolicyHash(
	generationContract, originContract string,
	periodTicks, surfaceEgressRadius int64,
	maximumGenerationsTick int,
) (string, error) {
	raw, err := json.Marshal(struct {
		SchemaVersion          int    `json:"schema_version"`
		ProfileID              string `json:"profile_id"`
		ProfileVersion         string `json:"profile_version"`
		GenerationContract     string `json:"generation_contract"`
		OriginContract         string `json:"origin_contract"`
		PeriodTicks            int64  `json:"period_ticks"`
		SurfaceEgressRadius    int64  `json:"surface_egress_radius"`
		MaximumGenerationsTick int    `json:"maximum_generations_tick"`
	}{
		SchemaVersion:          cityOpenWorldCommuteSourceSchemaVersion,
		ProfileID:              cityOpenWorldCommuteSourceProfileID,
		ProfileVersion:         cityOpenWorldCommuteSourceProfileVersion,
		GenerationContract:     generationContract,
		OriginContract:         originContract,
		PeriodTicks:            periodTicks,
		SurfaceEgressRadius:    surfaceEgressRadius,
		MaximumGenerationsTick: maximumGenerationsTick,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func activateCityOpenWorldCommuteSourceBootstrapWrite(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if _, err := tx.ExecContext(ctx, `
SELECT set_config('sub2api.city_open_world_commute_source_bootstrap_world_id', $1, TRUE)`, fmt.Sprintf("%d", worldID)); err != nil {
		return fmt.Errorf("enable open-world V13 commute source bootstrap: %w", err)
	}
	return nil
}

func activateCityOpenWorldCommuteSourceWrite(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if _, err := tx.ExecContext(ctx, `
SELECT set_config('sub2api.city_open_world_commute_source_world_id', $1, TRUE)`, fmt.Sprintf("%d", worldID)); err != nil {
		return fmt.Errorf("enable open-world V13 commute source write: %w", err)
	}
	return nil
}

// initializeCityOpenWorldV13CommuteSourceFoundation creates future source
// rows only. Historical V11/V9/V10 traffic remains immutable evidence and the
// current local position is deliberately not rewritten to force a commute.
func initializeCityOpenWorldV13CommuteSourceFoundation(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if worldID <= 0 {
		return ErrCityInvalidInput
	}
	var simulationVersion string
	var baselineTick int64
	if err := tx.QueryRowContext(ctx, `
SELECT simulation_version, current_tick
FROM city_worlds
WHERE id = $1
FOR UPDATE`, worldID).Scan(&simulationVersion, &baselineTick); err != nil {
		return fmt.Errorf("load V13 commute source world: %w", err)
	}
	if !cityEngineSupportsOpenWorldCommuteSources(simulationVersion) {
		return ErrCitySimulationVersion.WithMetadata(map[string]string{"version": simulationVersion})
	}
	if _, err := tx.ExecContext(ctx, `SELECT assert_city_open_world_commute_foundation($1)`, worldID); err != nil {
		return fmt.Errorf("validate V13 commute binding prerequisite: %w", err)
	}
	if err := activateCityOpenWorldCommuteSourceBootstrapWrite(ctx, tx, worldID); err != nil {
		return err
	}
	seeds, err := loadCityOpenWorldCommuteSourceSeeds(ctx, tx, worldID)
	if err != nil {
		return err
	}
	sources, err := cityOpenWorldCommuteSourcesForSeeds(seeds, baselineTick)
	if err != nil {
		return err
	}
	contentHash, err := cityOpenWorldCommuteSourcePolicyHash(
		cityOpenWorldCommuteSourceGenerationContract,
		cityOpenWorldCommuteSourceOriginContract,
		cityOpenWorldCommutePeriodTicks,
		cityOpenWorldCommuteSourceSurfaceEgressRadius,
		cityOpenWorldCommuteSourceMaximumGenerationsTick,
	)
	if err != nil {
		return fmt.Errorf("hash V13 commute source policy: %w", err)
	}
	metadata, err := json.Marshal(map[string]any{
		"schema_version":          cityOpenWorldCommuteSourceSchemaVersion,
		"binding_adapter":         "v12_residence_employment_binding_v1",
		"legacy_od_contract":      "v11_superseded_by_commute_source_v1",
		"origin_boundary":         "facility_interior_or_surface_egress_v1",
		"arrival_origin_contract": "captured_request_location_v1",
	})
	if err != nil {
		return fmt.Errorf("marshal V13 commute source profile metadata: %w", err)
	}
	policy := CityOpenWorldCommuteSourcePolicy{
		ProfileID:              cityOpenWorldCommuteSourceProfileID,
		ProfileVersion:         cityOpenWorldCommuteSourceProfileVersion,
		ContentHash:            contentHash,
		BaselineTick:           baselineTick,
		GenerationContract:     cityOpenWorldCommuteSourceGenerationContract,
		OriginContract:         cityOpenWorldCommuteSourceOriginContract,
		PeriodTicks:            cityOpenWorldCommutePeriodTicks,
		SurfaceEgressRadius:    cityOpenWorldCommuteSourceSurfaceEgressRadius,
		MaximumGenerationsTick: cityOpenWorldCommuteSourceMaximumGenerationsTick,
		SourceCount:            int64(len(sources)),
		Revision:               1,
		Metadata:               metadata,
	}
	state := &CityOpenWorldCommuteSourceState{Policy: policy, Sources: sources, Metrics: make([]CityOpenWorldCommuteSourceCycleMetric, 0)}
	if err = validateCityOpenWorldCommuteSourceState(state); err != nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v13_commute_source"}).WithCause(err)
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_commute_source_profiles
    (world_id, profile_id, profile_version, content_hash, baseline_tick,
     generation_contract, origin_contract, period_ticks, surface_egress_radius,
     maximum_generations_tick, source_count, generated_count, suppressed_count,
     metric_count, revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, 0, 0, 0, 1, $12::jsonb)`,
		worldID, policy.ProfileID, policy.ProfileVersion, policy.ContentHash,
		policy.BaselineTick, policy.GenerationContract, policy.OriginContract,
		policy.PeriodTicks, policy.SurfaceEgressRadius, policy.MaximumGenerationsTick,
		policy.SourceCount, []byte(policy.Metadata)); err != nil {
		return fmt.Errorf("insert V13 commute source profile: %w", err)
	}
	for _, source := range sources {
		actorID := int64(0)
		for _, seed := range seeds {
			if seed.bindingCode == source.BindingCode {
				actorID = seed.actorID
				break
			}
		}
		if actorID <= 0 {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v13_commute_source_actor"})
		}
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_commute_sources
    (world_id, code, binding_code, source_kind, direction, actor_id,
     employment_role_code, origin_facility_code, origin_hub_code,
     destination_facility_code, destination_hub_code, mode_code, purpose_code,
     requested_units, status, period_ticks, phase_offset, next_due_tick,
     last_transition_tick, generated_count, suppressed_count, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
        $14, $15, $16, $17, $18, $19, 0, 0, 1, $20::jsonb)`,
			worldID, source.Code, source.BindingCode, source.SourceKind, source.Direction, actorID,
			source.EmploymentRoleCode, source.OriginFacilityCode, source.OriginHubCode,
			source.DestinationFacilityCode, source.DestinationHubCode, source.ModeCode,
			source.PurposeCode, source.RequestedUnits, source.Status, source.PeriodTicks,
			source.PhaseOffset, source.NextDueTick, source.LastTransitionTick,
			[]byte(source.Metadata)); err != nil {
			return fmt.Errorf("insert V13 commute source %s: %w", source.Code, err)
		}
	}
	if _, err = tx.ExecContext(ctx, `SELECT assert_city_open_world_commute_source_foundation($1)`, worldID); err != nil {
		return fmt.Errorf("validate V13 commute source foundation: %w", err)
	}
	return nil
}

func loadCityOpenWorldCommuteSourceSeeds(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) ([]cityOpenWorldCommuteSourceSeed, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT binding.code, actor.id, actor.code, binding.employment_role_code,
       binding.home_facility_code, binding.home_hub_code,
       binding.work_facility_code, binding.work_hub_code,
       binding.period_ticks, binding.outbound_phase, binding.return_phase
FROM city_open_world_commute_bindings binding
JOIN city_open_world_actors actor
  ON actor.id = binding.actor_id AND actor.world_id = binding.world_id
WHERE binding.world_id = $1
  AND binding.status = 'active'
ORDER BY binding.code`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V13 commute source binding seeds: %w", err)
	}
	items := make([]cityOpenWorldCommuteSourceSeed, 0)
	for rows.Next() {
		item := cityOpenWorldCommuteSourceSeed{}
		if err = rows.Scan(
			&item.bindingCode, &item.actorID, &item.actorCode, &item.employmentRoleCode,
			&item.homeFacilityCode, &item.homeHubCode, &item.workFacilityCode,
			&item.workHubCode, &item.periodTicks, &item.outboundPhase, &item.returnPhase,
		); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan V13 commute source binding seed: %w", err)
		}
		items = append(items, item)
	}
	if err = closeCityRows(rows, "iterate V13 commute source binding seeds"); err != nil {
		return nil, err
	}
	return items, nil
}

func cityOpenWorldCommuteSourcesForSeeds(
	seeds []cityOpenWorldCommuteSourceSeed,
	baselineTick int64,
) ([]CityOpenWorldCommuteSource, error) {
	items := make([]CityOpenWorldCommuteSource, 0, len(seeds)*2)
	for _, seed := range seeds {
		if seed.actorID <= 0 || seed.periodTicks != cityOpenWorldCommutePeriodTicks ||
			seed.outboundPhase < 0 || seed.outboundPhase >= seed.periodTicks ||
			seed.returnPhase != (seed.outboundPhase+seed.periodTicks/2)%seed.periodTicks {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v13_commute_source_binding"})
		}
		for _, shape := range []struct {
			direction           string
			sourceKind          string
			originFacility      string
			originHub           string
			destinationFacility string
			destinationHub      string
			purpose             string
			phase               int64
		}{
			{cityOpenWorldCommuteSourceDirectionOutbound, cityOpenWorldCommuteSourceKindOutbound,
				seed.homeFacilityCode, seed.homeHubCode, seed.workFacilityCode, seed.workHubCode,
				cityOpenWorldCommuteSourcePurposeOutbound, seed.outboundPhase},
			{cityOpenWorldCommuteSourceDirectionReturn, cityOpenWorldCommuteSourceKindReturn,
				seed.workFacilityCode, seed.workHubCode, seed.homeFacilityCode, seed.homeHubCode,
				cityOpenWorldCommuteSourcePurposeReturn, seed.returnPhase},
		} {
			metadata, err := json.Marshal(map[string]any{
				"schema_version":  cityOpenWorldCommuteSourceSchemaVersion,
				"binding_adapter": "v12_residence_employment_binding_v1",
				"direction":       shape.direction,
			})
			if err != nil {
				return nil, fmt.Errorf("marshal V13 commute source metadata: %w", err)
			}
			items = append(items, CityOpenWorldCommuteSource{
				Code:                    cityOpenWorldCommuteSourceCode(seed.bindingCode, shape.direction),
				BindingCode:             seed.bindingCode,
				SourceKind:              shape.sourceKind,
				Direction:               shape.direction,
				ActorCode:               seed.actorCode,
				EmploymentRoleCode:      seed.employmentRoleCode,
				OriginFacilityCode:      shape.originFacility,
				OriginHubCode:           shape.originHub,
				DestinationFacilityCode: shape.destinationFacility,
				DestinationHubCode:      shape.destinationHub,
				ModeCode:                cityOpenWorldCommuteSourceModeWalk,
				PurposeCode:             shape.purpose,
				RequestedUnits:          1,
				Status:                  cityOpenWorldCommuteSourceStatusActive,
				PeriodTicks:             seed.periodTicks,
				PhaseOffset:             shape.phase,
				NextDueTick:             baselineTick + 1 + shape.phase,
				LastTransitionTick:      baselineTick,
				Version:                 1,
				Metadata:                metadata,
			})
		}
	}
	sortCityOpenWorldCommuteSources(items)
	return items, nil
}

func validateCityOpenWorldCommuteSourcePolicy(policy CityOpenWorldCommuteSourcePolicy) error {
	expectedHash, err := cityOpenWorldCommuteSourcePolicyHash(
		policy.GenerationContract, policy.OriginContract, policy.PeriodTicks,
		policy.SurfaceEgressRadius, policy.MaximumGenerationsTick,
	)
	if err != nil || policy.ProfileID != cityOpenWorldCommuteSourceProfileID ||
		policy.ProfileVersion != cityOpenWorldCommuteSourceProfileVersion ||
		policy.ContentHash != expectedHash || policy.BaselineTick < 0 ||
		policy.GenerationContract != cityOpenWorldCommuteSourceGenerationContract ||
		policy.OriginContract != cityOpenWorldCommuteSourceOriginContract ||
		policy.PeriodTicks != cityOpenWorldCommutePeriodTicks ||
		policy.SurfaceEgressRadius != cityOpenWorldCommuteSourceSurfaceEgressRadius ||
		policy.MaximumGenerationsTick != cityOpenWorldCommuteSourceMaximumGenerationsTick ||
		policy.SourceCount < 0 || policy.GeneratedCount < 0 || policy.SuppressedCount < 0 ||
		policy.MetricCount < 0 || policy.Revision < 1 || !json.Valid(policy.Metadata) {
		return errors.New("invalid V13 commute source policy")
	}
	return nil
}

func validateCityOpenWorldCommuteSourceState(state *CityOpenWorldCommuteSourceState) error {
	if state == nil {
		return errors.New("missing V13 commute source state")
	}
	if err := validateCityOpenWorldCommuteSourcePolicy(state.Policy); err != nil {
		return err
	}
	seenSources := make(map[string]struct{}, len(state.Sources))
	seenBindingDirection := make(map[string]struct{}, len(state.Sources))
	bindingDirections := make(map[string]map[string]struct{}, len(state.Sources)/2)
	generated, suppressed := int64(0), int64(0)
	for _, source := range state.Sources {
		transitions := source.GeneratedCount + source.SuppressedCount
		key := source.BindingCode + "\x00" + source.Direction
		if _, duplicate := seenSources[source.Code]; duplicate {
			return fmt.Errorf("duplicate V13 commute source %s", source.Code)
		}
		if _, duplicate := seenBindingDirection[key]; duplicate ||
			!worldRuntimeCodeValid(source.Code, 160) || !worldRuntimeCodeValid(source.BindingCode, 160) ||
			!worldRuntimeCodeValid(source.ActorCode, 128) || !worldRuntimeCodeValid(source.EmploymentRoleCode, 96) ||
			!worldRuntimeCodeValid(source.OriginFacilityCode, 160) || !worldRuntimeCodeValid(source.OriginHubCode, 160) ||
			!worldRuntimeCodeValid(source.DestinationFacilityCode, 160) || !worldRuntimeCodeValid(source.DestinationHubCode, 160) ||
			source.OriginFacilityCode == source.DestinationFacilityCode || source.ModeCode != cityOpenWorldCommuteSourceModeWalk ||
			source.RequestedUnits != 1 || source.Status != cityOpenWorldCommuteSourceStatusActive ||
			source.PeriodTicks != state.Policy.PeriodTicks || source.PhaseOffset < 0 ||
			source.PhaseOffset >= source.PeriodTicks || source.LastTransitionTick < state.Policy.BaselineTick ||
			source.NextDueTick <= source.LastTransitionTick || source.GeneratedCount < 0 || source.SuppressedCount < 0 ||
			source.Version != 1+transitions || !json.Valid(source.Metadata) {
			return fmt.Errorf("invalid V13 commute source %s", source.Code)
		}
		switch source.Direction {
		case cityOpenWorldCommuteSourceDirectionOutbound:
			if source.SourceKind != cityOpenWorldCommuteSourceKindOutbound || source.PurposeCode != cityOpenWorldCommuteSourcePurposeOutbound {
				return fmt.Errorf("invalid outbound V13 commute source %s", source.Code)
			}
		case cityOpenWorldCommuteSourceDirectionReturn:
			if source.SourceKind != cityOpenWorldCommuteSourceKindReturn || source.PurposeCode != cityOpenWorldCommuteSourcePurposeReturn {
				return fmt.Errorf("invalid return V13 commute source %s", source.Code)
			}
		default:
			return fmt.Errorf("invalid V13 commute source direction %s", source.Direction)
		}
		if source.Code != cityOpenWorldCommuteSourceCode(source.BindingCode, source.Direction) {
			return fmt.Errorf("invalid V13 commute source code %s", source.Code)
		}
		if transitions == 0 && (source.LastFact != nil || source.LastTransitionTick != state.Policy.BaselineTick ||
			source.NextDueTick != state.Policy.BaselineTick+1+source.PhaseOffset) {
			return fmt.Errorf("invalid V13 commute source baseline %s", source.Code)
		}
		if transitions > 0 && source.NextDueTick != source.LastTransitionTick+source.PeriodTicks {
			return fmt.Errorf("invalid V13 commute source cadence %s", source.Code)
		}
		if source.LastFact == nil && transitions != 0 {
			return fmt.Errorf("V13 commute source %s lacks transition fact", source.Code)
		}
		if source.LastFact != nil && (source.LastFact.Tick != source.LastTransitionTick || source.LastFact.Sequence < 1) {
			return fmt.Errorf("invalid V13 commute source fact %s", source.Code)
		}
		seenSources[source.Code] = struct{}{}
		seenBindingDirection[key] = struct{}{}
		if bindingDirections[source.BindingCode] == nil {
			bindingDirections[source.BindingCode] = make(map[string]struct{}, 2)
		}
		bindingDirections[source.BindingCode][source.Direction] = struct{}{}
		generated += source.GeneratedCount
		suppressed += source.SuppressedCount
	}
	if state.Policy.SourceCount != int64(len(state.Sources)) || state.Policy.SourceCount%2 != 0 ||
		state.Policy.GeneratedCount != generated || state.Policy.SuppressedCount != suppressed {
		return errors.New("V13 commute source counters are inconsistent")
	}
	for bindingCode, directions := range bindingDirections {
		if len(directions) != 2 {
			return fmt.Errorf("incomplete V13 commute source pair for binding %s", bindingCode)
		}
		if _, found := directions[cityOpenWorldCommuteSourceDirectionOutbound]; !found {
			return fmt.Errorf("missing outbound V13 commute source for binding %s", bindingCode)
		}
		if _, found := directions[cityOpenWorldCommuteSourceDirectionReturn]; !found {
			return fmt.Errorf("missing return V13 commute source for binding %s", bindingCode)
		}
	}
	seenCycles := make(map[int64]struct{}, len(state.Metrics))
	for _, metric := range state.Metrics {
		if _, duplicate := seenCycles[metric.CycleStartTick]; duplicate ||
			metric.CycleStartTick < state.Policy.BaselineTick+1 || metric.CycleEndTick < metric.CycleStartTick ||
			metric.CycleEndTick-metric.CycleStartTick+1 != state.Policy.PeriodTicks ||
			metric.ClosedTick != metric.CycleEndTick+1 || metric.SourceFact.Tick != metric.ClosedTick ||
			metric.SourceFact.Sequence < 1 || metric.OutboundGeneratedCount < 0 || metric.OutboundSuppressedCount < 0 ||
			metric.OutboundOriginUnavailableCount < 0 || metric.ReturnGeneratedCount < 0 ||
			metric.ReturnSuppressedCount < 0 || metric.ReturnOriginUnavailableCount < 0 ||
			metric.ScheduledDemandCount < 0 || metric.CompletedDemandCount < 0 || metric.ExpiredDemandCount < 0 ||
			metric.PendingDemandCount < 0 || metric.ArrivalLandedCount < 0 || metric.ArrivalBlockedCount < 0 ||
			metric.ArrivalFailedCount < 0 || !json.Valid(metric.Metadata) {
			return fmt.Errorf("invalid V13 commute cycle metric %d", metric.CycleStartTick)
		}
		if metric.OutboundOriginUnavailableCount > metric.OutboundSuppressedCount ||
			metric.ReturnOriginUnavailableCount > metric.ReturnSuppressedCount {
			return fmt.Errorf("invalid V13 commute origin-unavailable metric %d", metric.CycleStartTick)
		}
		seenCycles[metric.CycleStartTick] = struct{}{}
	}
	if state.Policy.MetricCount != int64(len(state.Metrics)) {
		return errors.New("V13 commute source metric counters are inconsistent")
	}
	return nil
}

func cityOpenWorldCommuteSourceStaticCheckpointEqual(previous, checkpoint *CityOpenWorldCommuteSourceState) bool {
	if previous == nil || checkpoint == nil {
		return previous == checkpoint
	}
	left, right := previous.Policy, checkpoint.Policy
	if left.ProfileID != right.ProfileID || left.ProfileVersion != right.ProfileVersion ||
		left.ContentHash != right.ContentHash || left.BaselineTick != right.BaselineTick ||
		left.GenerationContract != right.GenerationContract || left.OriginContract != right.OriginContract ||
		left.PeriodTicks != right.PeriodTicks || left.SurfaceEgressRadius != right.SurfaceEgressRadius ||
		left.MaximumGenerationsTick != right.MaximumGenerationsTick || !reflect.DeepEqual(left.Metadata, right.Metadata) ||
		len(previous.Sources) != len(checkpoint.Sources) {
		return false
	}
	for index := range previous.Sources {
		leftSource, rightSource := previous.Sources[index], checkpoint.Sources[index]
		if leftSource.Code != rightSource.Code || leftSource.BindingCode != rightSource.BindingCode ||
			leftSource.SourceKind != rightSource.SourceKind || leftSource.Direction != rightSource.Direction ||
			leftSource.ActorCode != rightSource.ActorCode || leftSource.EmploymentRoleCode != rightSource.EmploymentRoleCode ||
			leftSource.OriginFacilityCode != rightSource.OriginFacilityCode || leftSource.OriginHubCode != rightSource.OriginHubCode ||
			leftSource.DestinationFacilityCode != rightSource.DestinationFacilityCode || leftSource.DestinationHubCode != rightSource.DestinationHubCode ||
			leftSource.ModeCode != rightSource.ModeCode || leftSource.PurposeCode != rightSource.PurposeCode ||
			leftSource.RequestedUnits != rightSource.RequestedUnits || leftSource.Status != rightSource.Status ||
			leftSource.PeriodTicks != rightSource.PeriodTicks || leftSource.PhaseOffset != rightSource.PhaseOffset ||
			!reflect.DeepEqual(leftSource.Metadata, rightSource.Metadata) {
			return false
		}
	}
	return true
}

func loadCityOpenWorldCommuteSourceState(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*CityOpenWorldCommuteSourceState, error) {
	state := &CityOpenWorldCommuteSourceState{
		Sources: make([]CityOpenWorldCommuteSource, 0), Metrics: make([]CityOpenWorldCommuteSourceCycleMetric, 0),
	}
	if err := queryer.QueryRowContext(ctx, `
SELECT profile_id, profile_version, content_hash, baseline_tick,
       generation_contract, origin_contract, period_ticks, surface_egress_radius,
       maximum_generations_tick, source_count, generated_count, suppressed_count,
       metric_count, revision, metadata
FROM city_open_world_commute_source_profiles
WHERE world_id = $1`, worldID).Scan(
		&state.Policy.ProfileID, &state.Policy.ProfileVersion, &state.Policy.ContentHash,
		&state.Policy.BaselineTick, &state.Policy.GenerationContract, &state.Policy.OriginContract,
		&state.Policy.PeriodTicks, &state.Policy.SurfaceEgressRadius,
		&state.Policy.MaximumGenerationsTick, &state.Policy.SourceCount,
		&state.Policy.GeneratedCount, &state.Policy.SuppressedCount, &state.Policy.MetricCount,
		&state.Policy.Revision, &state.Policy.Metadata,
	); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v13_commute_source_profile"})
	} else if err != nil {
		return nil, fmt.Errorf("load V13 commute source profile: %w", err)
	}
	sourceRows, err := queryer.QueryContext(ctx, `
SELECT source.code, source.binding_code, source.source_kind, source.direction,
       actor.code, source.employment_role_code, source.origin_facility_code,
       source.origin_hub_code, source.destination_facility_code,
       source.destination_hub_code, source.mode_code, source.purpose_code,
       source.requested_units, source.status, source.period_ticks, source.phase_offset,
       source.next_due_tick, source.last_transition_tick, last_fact.tick,
       last_fact.sequence, source.generated_count, source.suppressed_count,
       source.version, source.metadata
FROM city_open_world_commute_sources source
JOIN city_open_world_actors actor
  ON actor.id = source.actor_id AND actor.world_id = source.world_id
LEFT JOIN city_open_world_runtime_facts last_fact
  ON last_fact.id = source.last_fact_id AND last_fact.world_id = source.world_id
WHERE source.world_id = $1
ORDER BY source.code`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V13 commute sources: %w", err)
	}
	for sourceRows.Next() {
		item := CityOpenWorldCommuteSource{}
		var factTick, factSequence sql.NullInt64
		if err = sourceRows.Scan(
			&item.Code, &item.BindingCode, &item.SourceKind, &item.Direction, &item.ActorCode,
			&item.EmploymentRoleCode, &item.OriginFacilityCode, &item.OriginHubCode,
			&item.DestinationFacilityCode, &item.DestinationHubCode, &item.ModeCode,
			&item.PurposeCode, &item.RequestedUnits, &item.Status, &item.PeriodTicks,
			&item.PhaseOffset, &item.NextDueTick, &item.LastTransitionTick,
			&factTick, &factSequence, &item.GeneratedCount, &item.SuppressedCount,
			&item.Version, &item.Metadata,
		); err != nil {
			_ = sourceRows.Close()
			return nil, fmt.Errorf("scan V13 commute source: %w", err)
		}
		if factTick.Valid {
			item.LastFact = &CityOpenWorldRuntimeFactRef{Tick: factTick.Int64, Sequence: factSequence.Int64}
		}
		state.Sources = append(state.Sources, item)
	}
	if err = closeCityRows(sourceRows, "iterate V13 commute sources"); err != nil {
		return nil, err
	}
	metricRows, err := queryer.QueryContext(ctx, `
SELECT metric.cycle_start_tick, metric.cycle_end_tick, metric.closed_tick,
       fact.tick, fact.sequence, metric.outbound_generated_count,
       metric.outbound_suppressed_count, metric.outbound_origin_unavailable_count,
       metric.return_generated_count, metric.return_suppressed_count,
       metric.return_origin_unavailable_count, metric.scheduled_demand_count,
       metric.completed_demand_count, metric.expired_demand_count,
       metric.pending_demand_count, metric.arrival_landed_count,
       metric.arrival_blocked_count, metric.arrival_failed_count, metric.metadata
FROM city_open_world_commute_cycle_metrics metric
JOIN city_open_world_runtime_facts fact
  ON fact.id = metric.source_fact_id AND fact.world_id = metric.world_id
WHERE metric.world_id = $1
ORDER BY metric.cycle_start_tick`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V13 commute cycle metrics: %w", err)
	}
	for metricRows.Next() {
		item := CityOpenWorldCommuteSourceCycleMetric{}
		if err = metricRows.Scan(
			&item.CycleStartTick, &item.CycleEndTick, &item.ClosedTick,
			&item.SourceFact.Tick, &item.SourceFact.Sequence, &item.OutboundGeneratedCount,
			&item.OutboundSuppressedCount, &item.OutboundOriginUnavailableCount,
			&item.ReturnGeneratedCount, &item.ReturnSuppressedCount,
			&item.ReturnOriginUnavailableCount, &item.ScheduledDemandCount,
			&item.CompletedDemandCount, &item.ExpiredDemandCount, &item.PendingDemandCount,
			&item.ArrivalLandedCount, &item.ArrivalBlockedCount, &item.ArrivalFailedCount,
			&item.Metadata,
		); err != nil {
			_ = metricRows.Close()
			return nil, fmt.Errorf("scan V13 commute cycle metric: %w", err)
		}
		state.Metrics = append(state.Metrics, item)
	}
	if err = closeCityRows(metricRows, "iterate V13 commute cycle metrics"); err != nil {
		return nil, err
	}
	sortCityOpenWorldCommuteSources(state.Sources)
	if err = validateCityOpenWorldCommuteSourceState(state); err != nil {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v13_commute_source"}).WithCause(err)
	}
	return state, nil
}

// GetCityOpenWorldCommuteSourceState follows V11/V12 privacy rules: source
// identities are filtered to controlled actors unless caller has full access;
// closed aggregate metrics stay world-readable.
func (s *CityEconomyService) GetCityOpenWorldCommuteSourceState(
	ctx context.Context,
	userID, worldID int64,
) (*CityOpenWorldCommuteSourceState, error) {
	if userID <= 0 || worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, userID, worldID); err != nil {
		return nil, err
	}
	var version string
	if err := s.db.QueryRowContext(ctx, `SELECT simulation_version FROM city_worlds WHERE id = $1`, worldID).Scan(&version); err != nil {
		return nil, fmt.Errorf("load V13 commute source world version: %w", err)
	}
	if !cityEngineSupportsOpenWorldCommuteSources(version) {
		return nil, ErrCitySimulationVersion.WithMetadata(map[string]string{"version": version})
	}
	state, err := loadCityOpenWorldCommuteSourceState(ctx, s.db, worldID)
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
	filtered := make([]CityOpenWorldCommuteSource, 0, len(state.Sources))
	for _, source := range state.Sources {
		if _, found := visible[source.ActorCode]; found {
			filtered = append(filtered, source)
		}
	}
	state.Sources = filtered
	return state, nil
}

func sortCityOpenWorldCommuteSources(items []CityOpenWorldCommuteSource) {
	sort.Slice(items, func(left, right int) bool { return items[left].Code < items[right].Code })
}
