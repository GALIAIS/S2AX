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
	cityOpenWorldMobilityODSchemaVersion                = 1
	cityOpenWorldMobilityODProfileID                    = "sub2api-open-world-mobility-od"
	cityOpenWorldMobilityODProfileVersion               = "1.0.0"
	cityOpenWorldMobilityODGenerationContract           = "versioned_source_od_adapter_v1"
	cityOpenWorldMobilityODMetricContract               = "next_cycle_mobility_metrics_v1"
	cityOpenWorldMobilityODSourceKindNPCWorkVisit       = "npc.assigned_facility_visit"
	cityOpenWorldMobilityODSourceStatusActive           = "active"
	cityOpenWorldMobilityODCycleTicks             int64 = 24
	cityOpenWorldMobilityODMaximumPerTick               = 64
)

// CityOpenWorldMobilityODPolicy seals the first automatic OD adapter. The
// profile owns scheduling cadence and metric boundaries, while V9 continues to
// own route allocation and V10 continues to own macro-to-local arrival.
type CityOpenWorldMobilityODPolicy struct {
	ProfileID              string          `json:"profile_id"`
	ProfileVersion         string          `json:"profile_version"`
	ContentHash            string          `json:"content_hash"`
	BaselineTick           int64           `json:"baseline_tick"`
	GenerationContract     string          `json:"generation_contract"`
	MetricContract         string          `json:"metric_contract"`
	CycleTicks             int64           `json:"cycle_ticks"`
	MaximumGenerationsTick int             `json:"maximum_generations_tick"`
	SourceCount            int64           `json:"source_count"`
	GeneratedCount         int64           `json:"generated_count"`
	SuppressedCount        int64           `json:"suppressed_count"`
	MetricCount            int64           `json:"metric_count"`
	Revision               int64           `json:"revision"`
	Metadata               json.RawMessage `json:"metadata"`
}

// CityOpenWorldMobilityODSource is a versioned adapter input rather than an
// implicit NPC behavior. V11 ships one source kind backed by V5's assigned
// work facility. Future household, enterprise-order, visitor, or emergency
// sources can use the same durable contract without modifying V9 routes.
type CityOpenWorldMobilityODSource struct {
	Code                    string                       `json:"code"`
	SourceKind              string                       `json:"source_kind"`
	ActorCode               string                       `json:"actor_code"`
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

// CityOpenWorldMobilityODCycleMetric is immutable evidence closed at the
// first tick after a fixed cycle. It intentionally reports event occurrence
// during that cycle instead of retrospectively rewriting an earlier report
// when a long-running route finishes later.
type CityOpenWorldMobilityODCycleMetric struct {
	CycleStartTick       int64                       `json:"cycle_start_tick"`
	CycleEndTick         int64                       `json:"cycle_end_tick"`
	ClosedTick           int64                       `json:"closed_tick"`
	SourceFact           CityOpenWorldRuntimeFactRef `json:"source_fact"`
	GeneratedCount       int64                       `json:"generated_count"`
	SuppressedCount      int64                       `json:"suppressed_count"`
	NetworkRequested     int64                       `json:"network_requested_count"`
	NetworkScheduled     int64                       `json:"network_scheduled_count"`
	NetworkCompleted     int64                       `json:"network_completed_count"`
	NetworkExpired       int64                       `json:"network_expired_count"`
	PendingDemandCount   int64                       `json:"pending_demand_count"`
	ArrivalLanded        int64                       `json:"arrival_landed_count"`
	ArrivalBlocked       int64                       `json:"arrival_blocked_count"`
	ArrivalFailed        int64                       `json:"arrival_failed_count"`
	TravelTicksTotal     int64                       `json:"travel_ticks_total"`
	CongestionTicksTotal int64                       `json:"congestion_ticks_total"`
	PeakOccupancyMilli   int                         `json:"peak_occupancy_milli"`
	Metadata             json.RawMessage             `json:"metadata"`
}

// CityOpenWorldMobilityODState is the V11 successor state. It keeps source
// lifecycle and sealed cycle observations separate from V9's route truth.
type CityOpenWorldMobilityODState struct {
	Policy  CityOpenWorldMobilityODPolicy        `json:"policy"`
	Sources []CityOpenWorldMobilityODSource      `json:"sources"`
	Metrics []CityOpenWorldMobilityODCycleMetric `json:"metrics"`
}

type cityOpenWorldMobilityODSourceSeed struct {
	ActorID                 int64
	ActorCode               string
	DestinationFacilityCode string
	DestinationHubCode      string
	PhaseOffset             int64
	Metadata                json.RawMessage
}

func cityOpenWorldMobilityODSourceCode(actorCode string) string {
	sum := sha256.Sum256([]byte("npc.assigned_facility_visit\x00" + actorCode))
	return "mobility.od.source." + hex.EncodeToString(sum[:20])
}

func cityOpenWorldMobilityODPolicyHash(
	generationContract, metricContract string,
	cycleTicks int64,
	maximumGenerationsTick int,
) (string, error) {
	raw, err := json.Marshal(struct {
		SchemaVersion          int    `json:"schema_version"`
		ProfileID              string `json:"profile_id"`
		ProfileVersion         string `json:"profile_version"`
		GenerationContract     string `json:"generation_contract"`
		MetricContract         string `json:"metric_contract"`
		CycleTicks             int64  `json:"cycle_ticks"`
		MaximumGenerationsTick int    `json:"maximum_generations_tick"`
	}{
		SchemaVersion: cityOpenWorldMobilityODSchemaVersion,
		ProfileID:     cityOpenWorldMobilityODProfileID, ProfileVersion: cityOpenWorldMobilityODProfileVersion,
		GenerationContract: generationContract, MetricContract: metricContract,
		CycleTicks: cycleTicks, MaximumGenerationsTick: maximumGenerationsTick,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func activateCityOpenWorldMobilityODBootstrapWrite(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if _, err := tx.ExecContext(ctx, `
SELECT set_config('sub2api.city_open_world_mobility_od_bootstrap_world_id', $1, TRUE)`, fmt.Sprintf("%d", worldID)); err != nil {
		return fmt.Errorf("enable open-world V11 OD bootstrap: %w", err)
	}
	return nil
}

func activateCityOpenWorldMobilityODWrite(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if _, err := tx.ExecContext(ctx, `
SELECT set_config('sub2api.city_open_world_mobility_od_world_id', $1, TRUE)`, fmt.Sprintf("%d", worldID)); err != nil {
		return fmt.Errorf("enable open-world V11 OD write: %w", err)
	}
	return nil
}

// initializeCityOpenWorldV11MobilityODFoundation snapshots the initial source
// adapters from existing V5 NPC profiles. It neither invents homes nor treats
// a work facility as a residence: the source requests a visit from the actor's
// current validated local position when its cycle becomes due.
func initializeCityOpenWorldV11MobilityODFoundation(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if worldID <= 0 {
		return ErrCityInvalidInput
	}
	var simulationVersion string
	var baselineTick int64
	if err := tx.QueryRowContext(ctx, `
SELECT simulation_version, current_tick
FROM city_worlds WHERE id = $1 FOR UPDATE`, worldID).Scan(&simulationVersion, &baselineTick); err != nil {
		return fmt.Errorf("load V11 OD world: %w", err)
	}
	if !cityEngineSupportsOpenWorldMobilityOD(simulationVersion) {
		return ErrCitySimulationVersion.WithMetadata(map[string]string{"version": simulationVersion})
	}
	if _, err := tx.ExecContext(ctx, `SELECT assert_city_open_world_arrival_foundation($1)`, worldID); err != nil {
		return fmt.Errorf("validate V11 OD arrival prerequisite: %w", err)
	}
	if err := activateCityOpenWorldMobilityODBootstrapWrite(ctx, tx, worldID); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `
SELECT actor.id, actor.code, facility.code, hub.code, profile.schedule_offset
FROM city_open_world_npc_profiles profile
JOIN city_open_world_actors actor
  ON actor.id = profile.actor_id AND actor.world_id = profile.world_id
JOIN city_open_world_facilities facility
  ON facility.id = profile.work_facility_id AND facility.world_id = profile.world_id
JOIN city_open_world_mobility_hubs hub
  ON hub.world_id = facility.world_id AND hub.facility_id = facility.id
WHERE profile.world_id = $1
  AND profile.work_facility_id IS NOT NULL
  AND profile.lod_tier <> 'dormant'
  AND actor.status = 'active'
  AND facility.state = 'active'
  AND hub.hub_kind = 'facility'
ORDER BY actor.code ASC, hub.code ASC`, worldID)
	if err != nil {
		return fmt.Errorf("load V11 OD NPC source seeds: %w", err)
	}
	seeds := make([]cityOpenWorldMobilityODSourceSeed, 0)
	for rows.Next() {
		seed := cityOpenWorldMobilityODSourceSeed{}
		var scheduleOffset int
		if err = rows.Scan(&seed.ActorID, &seed.ActorCode, &seed.DestinationFacilityCode, &seed.DestinationHubCode, &scheduleOffset); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan V11 OD NPC source seed: %w", err)
		}
		seed.PhaseOffset = int64(scheduleOffset) % cityOpenWorldMobilityODCycleTicks
		seed.Metadata, err = json.Marshal(map[string]any{
			"schema_version": cityOpenWorldMobilityODSchemaVersion,
			"adapter":        "v5_npc_work_facility_v1",
		})
		if err != nil {
			_ = rows.Close()
			return fmt.Errorf("marshal V11 OD NPC source metadata: %w", err)
		}
		seeds = append(seeds, seed)
	}
	if err = closeCityRows(rows, "iterate V11 OD NPC source seeds"); err != nil {
		return err
	}
	if len(seeds) == 0 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v11_od_sources"})
	}
	contentHash, err := cityOpenWorldMobilityODPolicyHash(
		cityOpenWorldMobilityODGenerationContract, cityOpenWorldMobilityODMetricContract,
		cityOpenWorldMobilityODCycleTicks, cityOpenWorldMobilityODMaximumPerTick,
	)
	if err != nil {
		return fmt.Errorf("hash V11 OD policy: %w", err)
	}
	metadata, err := json.Marshal(map[string]any{
		"schema_version":          cityOpenWorldMobilityODSchemaVersion,
		"source_seed_adapter":     "v5_npc_work_facility_v1",
		"baseline_scope":          "post_baseline_automatic_od_only",
		"arrival_origin_contract": "captured_current_location_v1",
	})
	if err != nil {
		return fmt.Errorf("marshal V11 OD profile metadata: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_mobility_od_profiles
    (world_id, profile_id, profile_version, content_hash, baseline_tick,
     generation_contract, metric_contract, cycle_ticks, maximum_generations_tick,
     source_count, generated_count, suppressed_count, metric_count, revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9,
        $10, 0, 0, 0, 1, $11::jsonb)`,
		worldID, cityOpenWorldMobilityODProfileID, cityOpenWorldMobilityODProfileVersion,
		contentHash, baselineTick, cityOpenWorldMobilityODGenerationContract,
		cityOpenWorldMobilityODMetricContract, cityOpenWorldMobilityODCycleTicks,
		cityOpenWorldMobilityODMaximumPerTick, len(seeds), []byte(metadata)); err != nil {
		return fmt.Errorf("insert V11 OD profile: %w", err)
	}
	for _, seed := range seeds {
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_mobility_od_sources
    (world_id, code, source_kind, actor_id, destination_facility_code,
     destination_hub_code, mode_code, purpose_code, requested_units, status,
     period_ticks, phase_offset, next_due_tick, last_transition_tick,
     generated_count, suppressed_count, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, 'walk', 'routine.facility_visit', 1, 'active',
        $7, $8, $9, $10, 0, 0, 1, $11::jsonb)`,
			worldID, cityOpenWorldMobilityODSourceCode(seed.ActorCode), cityOpenWorldMobilityODSourceKindNPCWorkVisit,
			seed.ActorID, seed.DestinationFacilityCode, seed.DestinationHubCode,
			cityOpenWorldMobilityODCycleTicks, seed.PhaseOffset,
			baselineTick+1+seed.PhaseOffset, baselineTick, []byte(seed.Metadata)); err != nil {
			return fmt.Errorf("insert V11 OD source %s: %w", seed.ActorCode, err)
		}
	}
	if _, err = tx.ExecContext(ctx, `SELECT assert_city_open_world_mobility_od_foundation($1)`, worldID); err != nil {
		return fmt.Errorf("validate V11 OD foundation: %w", err)
	}
	return nil
}

func loadCityOpenWorldMobilityODState(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*CityOpenWorldMobilityODState, error) {
	state := &CityOpenWorldMobilityODState{
		Sources: make([]CityOpenWorldMobilityODSource, 0), Metrics: make([]CityOpenWorldMobilityODCycleMetric, 0),
	}
	if err := queryer.QueryRowContext(ctx, `
SELECT profile_id, profile_version, content_hash, baseline_tick,
       generation_contract, metric_contract, cycle_ticks, maximum_generations_tick,
       source_count, generated_count, suppressed_count, metric_count, revision, metadata
FROM city_open_world_mobility_od_profiles
WHERE world_id = $1`, worldID).Scan(
		&state.Policy.ProfileID, &state.Policy.ProfileVersion, &state.Policy.ContentHash,
		&state.Policy.BaselineTick, &state.Policy.GenerationContract, &state.Policy.MetricContract,
		&state.Policy.CycleTicks, &state.Policy.MaximumGenerationsTick, &state.Policy.SourceCount,
		&state.Policy.GeneratedCount, &state.Policy.SuppressedCount, &state.Policy.MetricCount,
		&state.Policy.Revision, &state.Policy.Metadata,
	); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCityOpenWorldRuntimeNotFound
	} else if err != nil {
		return nil, fmt.Errorf("load V11 OD profile: %w", err)
	}
	sourceRows, err := queryer.QueryContext(ctx, `
SELECT source.code, source.source_kind, actor.code, source.destination_facility_code,
       source.destination_hub_code, source.mode_code, source.purpose_code,
       source.requested_units, source.status, source.period_ticks, source.phase_offset,
       source.next_due_tick, source.last_transition_tick,
       last_fact.tick, last_fact.sequence, source.generated_count,
       source.suppressed_count, source.version, source.metadata
FROM city_open_world_mobility_od_sources source
JOIN city_open_world_actors actor
  ON actor.id = source.actor_id AND actor.world_id = source.world_id
LEFT JOIN city_open_world_runtime_facts last_fact
  ON last_fact.id = source.last_fact_id AND last_fact.world_id = source.world_id
WHERE source.world_id = $1
ORDER BY source.code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V11 OD sources: %w", err)
	}
	for sourceRows.Next() {
		item := CityOpenWorldMobilityODSource{}
		var factTick, factSequence sql.NullInt64
		if err = sourceRows.Scan(
			&item.Code, &item.SourceKind, &item.ActorCode, &item.DestinationFacilityCode,
			&item.DestinationHubCode, &item.ModeCode, &item.PurposeCode, &item.RequestedUnits,
			&item.Status, &item.PeriodTicks, &item.PhaseOffset, &item.NextDueTick,
			&item.LastTransitionTick, &factTick, &factSequence, &item.GeneratedCount,
			&item.SuppressedCount, &item.Version, &item.Metadata,
		); err != nil {
			_ = sourceRows.Close()
			return nil, fmt.Errorf("scan V11 OD source: %w", err)
		}
		if factTick.Valid {
			item.LastFact = &CityOpenWorldRuntimeFactRef{Tick: factTick.Int64, Sequence: factSequence.Int64}
		}
		state.Sources = append(state.Sources, item)
	}
	if err = closeCityRows(sourceRows, "iterate V11 OD sources"); err != nil {
		return nil, err
	}
	metricRows, err := queryer.QueryContext(ctx, `
SELECT metric.cycle_start_tick, metric.cycle_end_tick, metric.closed_tick,
       fact.tick, fact.sequence, metric.generated_count, metric.suppressed_count,
       metric.network_requested_count, metric.network_scheduled_count,
       metric.network_completed_count, metric.network_expired_count,
       metric.pending_demand_count, metric.arrival_landed_count,
       metric.arrival_blocked_count, metric.arrival_failed_count,
       metric.travel_ticks_total, metric.congestion_ticks_total,
       metric.peak_occupancy_milli, metric.metadata
FROM city_open_world_mobility_od_cycle_metrics metric
JOIN city_open_world_runtime_facts fact
  ON fact.id = metric.source_fact_id AND fact.world_id = metric.world_id
WHERE metric.world_id = $1
ORDER BY metric.cycle_start_tick ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V11 OD cycle metrics: %w", err)
	}
	for metricRows.Next() {
		item := CityOpenWorldMobilityODCycleMetric{}
		if err = metricRows.Scan(
			&item.CycleStartTick, &item.CycleEndTick, &item.ClosedTick,
			&item.SourceFact.Tick, &item.SourceFact.Sequence, &item.GeneratedCount,
			&item.SuppressedCount, &item.NetworkRequested, &item.NetworkScheduled,
			&item.NetworkCompleted, &item.NetworkExpired, &item.PendingDemandCount,
			&item.ArrivalLanded, &item.ArrivalBlocked, &item.ArrivalFailed,
			&item.TravelTicksTotal, &item.CongestionTicksTotal, &item.PeakOccupancyMilli,
			&item.Metadata,
		); err != nil {
			_ = metricRows.Close()
			return nil, fmt.Errorf("scan V11 OD cycle metric: %w", err)
		}
		state.Metrics = append(state.Metrics, item)
	}
	if err = closeCityRows(metricRows, "iterate V11 OD cycle metrics"); err != nil {
		return nil, err
	}
	if err = validateCityOpenWorldMobilityODState(state); err != nil {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v11_od"}).WithCause(err)
	}
	return state, nil
}

func validateCityOpenWorldMobilityODPolicy(policy CityOpenWorldMobilityODPolicy) error {
	expectedHash, err := cityOpenWorldMobilityODPolicyHash(
		policy.GenerationContract, policy.MetricContract, policy.CycleTicks, policy.MaximumGenerationsTick,
	)
	if err != nil || policy.ProfileID != cityOpenWorldMobilityODProfileID ||
		policy.ProfileVersion != cityOpenWorldMobilityODProfileVersion || policy.ContentHash != expectedHash ||
		policy.BaselineTick < 0 || policy.GenerationContract != cityOpenWorldMobilityODGenerationContract ||
		policy.MetricContract != cityOpenWorldMobilityODMetricContract ||
		policy.CycleTicks != cityOpenWorldMobilityODCycleTicks ||
		policy.MaximumGenerationsTick < 1 || policy.MaximumGenerationsTick > 100000 ||
		policy.SourceCount < 0 || policy.GeneratedCount < 0 || policy.SuppressedCount < 0 ||
		policy.MetricCount < 0 || policy.Revision < 1 || !json.Valid(policy.Metadata) {
		return fmt.Errorf("invalid V11 OD policy")
	}
	return nil
}

func validateCityOpenWorldMobilityODState(state *CityOpenWorldMobilityODState) error {
	if state == nil {
		return fmt.Errorf("V11 OD state is missing")
	}
	if err := validateCityOpenWorldMobilityODPolicy(state.Policy); err != nil {
		return err
	}
	seenSources := make(map[string]struct{}, len(state.Sources))
	generated, suppressed := int64(0), int64(0)
	for _, source := range state.Sources {
		transitions := source.GeneratedCount + source.SuppressedCount
		if _, duplicate := seenSources[source.Code]; duplicate || !worldRuntimeCodeValid(source.Code, 160) ||
			source.SourceKind != cityOpenWorldMobilityODSourceKindNPCWorkVisit ||
			!worldRuntimeCodeValid(source.ActorCode, 128) || !worldRuntimeCodeValid(source.DestinationFacilityCode, 160) ||
			!worldRuntimeCodeValid(source.DestinationHubCode, 160) || !worldRuntimeCodeValid(source.ModeCode, 64) ||
			!worldRuntimeCodeValid(source.PurposeCode, 96) || source.ModeCode != "walk" ||
			source.PurposeCode != "routine.facility_visit" || source.RequestedUnits < 1 || source.RequestedUnits > 1000 ||
			source.Status != cityOpenWorldMobilityODSourceStatusActive || source.PeriodTicks != state.Policy.CycleTicks ||
			source.PhaseOffset < 0 || source.PhaseOffset >= source.PeriodTicks ||
			source.LastTransitionTick < state.Policy.BaselineTick || source.NextDueTick <= source.LastTransitionTick ||
			source.GeneratedCount < 0 || source.SuppressedCount < 0 ||
			source.Version != 1+source.GeneratedCount+source.SuppressedCount || !json.Valid(source.Metadata) {
			return fmt.Errorf("invalid V11 OD source %s", source.Code)
		}
		if transitions == 0 && (source.LastFact != nil ||
			source.LastTransitionTick != state.Policy.BaselineTick ||
			source.NextDueTick != state.Policy.BaselineTick+1+source.PhaseOffset) {
			return fmt.Errorf("invalid V11 OD baseline source %s", source.Code)
		}
		if transitions > 0 && source.NextDueTick != source.LastTransitionTick+source.PeriodTicks {
			return fmt.Errorf("invalid V11 OD source cadence %s", source.Code)
		}
		if source.LastFact == nil && transitions != 0 {
			return fmt.Errorf("V11 OD source %s lacks transition fact", source.Code)
		}
		if source.LastFact != nil && (source.LastFact.Tick != source.LastTransitionTick || source.LastFact.Sequence < 1) {
			return fmt.Errorf("invalid V11 OD source fact %s", source.Code)
		}
		seenSources[source.Code] = struct{}{}
		generated += source.GeneratedCount
		suppressed += source.SuppressedCount
	}
	if state.Policy.SourceCount != int64(len(state.Sources)) || state.Policy.GeneratedCount != generated ||
		state.Policy.SuppressedCount != suppressed {
		return fmt.Errorf("V11 OD source counters are inconsistent")
	}
	seenCycles := make(map[int64]struct{}, len(state.Metrics))
	for _, metric := range state.Metrics {
		if _, duplicate := seenCycles[metric.CycleStartTick]; duplicate ||
			metric.CycleStartTick < state.Policy.BaselineTick+1 || metric.CycleEndTick < metric.CycleStartTick ||
			metric.CycleEndTick-metric.CycleStartTick+1 != state.Policy.CycleTicks ||
			metric.ClosedTick != metric.CycleEndTick+1 || metric.SourceFact.Tick != metric.ClosedTick ||
			metric.SourceFact.Sequence < 1 || metric.GeneratedCount < 0 || metric.SuppressedCount < 0 ||
			metric.NetworkRequested < 0 || metric.NetworkScheduled < 0 || metric.NetworkCompleted < 0 ||
			metric.NetworkExpired < 0 || metric.PendingDemandCount < 0 || metric.ArrivalLanded < 0 ||
			metric.ArrivalBlocked < 0 || metric.ArrivalFailed < 0 || metric.TravelTicksTotal < 0 ||
			metric.CongestionTicksTotal < 0 || metric.PeakOccupancyMilli < 0 ||
			metric.PeakOccupancyMilli > 1000 || !json.Valid(metric.Metadata) {
			return fmt.Errorf("invalid V11 OD cycle metric %d", metric.CycleStartTick)
		}
		seenCycles[metric.CycleStartTick] = struct{}{}
	}
	if state.Policy.MetricCount != int64(len(state.Metrics)) {
		return fmt.Errorf("V11 OD metric counters are inconsistent")
	}
	return nil
}

func cityOpenWorldMobilityODStaticCheckpointEqual(
	previous, checkpoint *CityOpenWorldMobilityODState,
) bool {
	if previous == nil || checkpoint == nil {
		return previous == checkpoint
	}
	left, right := previous.Policy, checkpoint.Policy
	if left.ProfileID != right.ProfileID || left.ProfileVersion != right.ProfileVersion ||
		left.ContentHash != right.ContentHash || left.BaselineTick != right.BaselineTick ||
		left.GenerationContract != right.GenerationContract || left.MetricContract != right.MetricContract ||
		left.CycleTicks != right.CycleTicks || left.MaximumGenerationsTick != right.MaximumGenerationsTick ||
		!reflect.DeepEqual(left.Metadata, right.Metadata) || len(previous.Sources) != len(checkpoint.Sources) {
		return false
	}
	for index := range previous.Sources {
		leftSource, rightSource := previous.Sources[index], checkpoint.Sources[index]
		if leftSource.Code != rightSource.Code || leftSource.SourceKind != rightSource.SourceKind ||
			leftSource.ActorCode != rightSource.ActorCode || leftSource.DestinationFacilityCode != rightSource.DestinationFacilityCode ||
			leftSource.DestinationHubCode != rightSource.DestinationHubCode || leftSource.ModeCode != rightSource.ModeCode ||
			leftSource.PurposeCode != rightSource.PurposeCode || leftSource.RequestedUnits != rightSource.RequestedUnits ||
			leftSource.Status != rightSource.Status || leftSource.PeriodTicks != rightSource.PeriodTicks ||
			leftSource.PhaseOffset != rightSource.PhaseOffset || !reflect.DeepEqual(leftSource.Metadata, rightSource.Metadata) {
			return false
		}
	}
	return true
}

// GetCityOpenWorldMobilityODState exposes global closed traffic metrics while
// restricting source records to actors visible to the caller, matching the
// privacy behavior of V9 demands and V10 arrivals.
func (s *CityEconomyService) GetCityOpenWorldMobilityODState(
	ctx context.Context,
	userID, worldID int64,
) (*CityOpenWorldMobilityODState, error) {
	if userID <= 0 || worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, userID, worldID); err != nil {
		return nil, err
	}
	var version string
	if err := s.db.QueryRowContext(ctx, `SELECT simulation_version FROM city_worlds WHERE id = $1`, worldID).Scan(&version); err != nil {
		return nil, fmt.Errorf("load V11 OD world version: %w", err)
	}
	if !cityEngineSupportsOpenWorldMobilityOD(version) {
		return nil, ErrCitySimulationVersion.WithMetadata(map[string]string{"version": version})
	}
	state, err := loadCityOpenWorldMobilityODState(ctx, s.db, worldID)
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
	filtered := make([]CityOpenWorldMobilityODSource, 0, len(state.Sources))
	for _, source := range state.Sources {
		if _, found := visible[source.ActorCode]; found {
			filtered = append(filtered, source)
		}
	}
	state.Sources = filtered
	return state, nil
}

func sortCityOpenWorldMobilityODSources(items []CityOpenWorldMobilityODSource) {
	sort.Slice(items, func(i, j int) bool { return items[i].Code < items[j].Code })
}
