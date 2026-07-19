package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	worldRuntimeNavigationIntentVersion = "1.3.0"
	worldNavigationProfileVersion       = "1.0.0"

	WorldNavigationIntentStatusActive    = "active"
	WorldNavigationIntentStatusBlocked   = "blocked"
	WorldNavigationIntentStatusArrived   = "arrived"
	WorldNavigationIntentStatusCancelled = "cancelled"
	WorldNavigationIntentStatusFailed    = "failed"

	WorldNavigationOnBlockedRetry  = "retry"
	WorldNavigationOnBlockedCancel = "cancel"

	WorldNavigationReasonBudgetInsufficient = "budget_insufficient"
	WorldNavigationReasonTargetReached      = "target_reached"
	WorldNavigationReasonUserCancelled      = "user_cancelled"
	WorldNavigationReasonTargetInvalid      = "target_invalid"
	WorldNavigationReasonReservationTarget  = "reservation_target_conflict"
	WorldNavigationReasonReservationEdge    = "reservation_edge_conflict"

	worldNavigationDefaultMaximumIntentsPerTick  = 128
	worldNavigationDefaultBudgetGainUnits        = int64(100)
	worldNavigationDefaultBudgetCapUnits         = int64(400)
	worldNavigationDefaultMaxSteps               = 256
	worldNavigationDefaultMaximumBlockedAttempts = 64
	worldNavigationDefaultMaximumRetryDelayTicks = int64(8)
	worldNavigationDefaultFairnessAgingCap       = int64(1024)

	worldRuntimeRejectionNavigationIntentUnavailable = "WORLD_NAVIGATION_INTENT_UNAVAILABLE"
	worldRuntimeRejectionNavigationIntentInvalid     = "WORLD_NAVIGATION_INTENT_INVALID"
	worldRuntimeRejectionNavigationIntentTerminal    = "WORLD_NAVIGATION_INTENT_TERMINAL"
)

var ErrWorldNavigationIntentUnavailable = infraerrors.NotFound(
	"WORLD_NAVIGATION_INTENT_UNAVAILABLE", "world navigation intents are unavailable",
)

type WorldNavigationIntentQueryInput struct {
	UserID    int64
	WorldID   int64
	ActorCode string
}

type WorldNavigationReservationQueryInput struct {
	UserID  int64
	WorldID int64
	Tick    *int64
}

type worldNavigationIntentRecord struct {
	id           int64
	actorID      int64
	sourceFactID int64
	intent       WorldActorNavigationIntent
}

func initializeWorldNavigationIntentFoundation(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	targetSimulationVersion string,
) error {
	var simulationVersion string
	var baselineTick int64
	if err := tx.QueryRowContext(ctx, `
SELECT simulation_version, current_tick
FROM city_worlds WHERE id = $1 FOR UPDATE`, worldID).Scan(&simulationVersion, &baselineTick); err != nil {
		return fmt.Errorf("load world navigation-intent version: %w", err)
	}
	if !cityEngineSupportsWorldNavigationIntents(targetSimulationVersion) ||
		(simulationVersion != targetSimulationVersion &&
			!cityEngineCanUpgrade(simulationVersion, targetSimulationVersion)) {
		return fmt.Errorf("world navigation-intent foundation requires a portal-aware runtime")
	}
	if _, err := tx.ExecContext(ctx, `
SELECT set_config('sub2api.world_runtime_bootstrap_world_id', $1, TRUE)`, fmt.Sprintf("%d", worldID)); err != nil {
		return fmt.Errorf("enable world navigation-intent bootstrap: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE world_runtime_profiles
SET runtime_version = $2, updated_at = NOW()
WHERE world_id = $1`, worldID, worldRuntimeNavigationIntentVersion); err != nil {
		return fmt.Errorf("upgrade world navigation-intent runtime profile: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO world_navigation_profiles
    (world_id, profile_version, baseline_tick, maximum_intents_per_tick,
     default_budget_gain_units, default_budget_cap_units, default_max_steps,
     maximum_blocked_attempts, maximum_retry_delay_ticks, fairness_aging_cap,
     revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 1,
        '{"schema_version":1}'::jsonb)
ON CONFLICT (world_id) DO NOTHING`, worldID, worldNavigationProfileVersion,
		baselineTick, worldNavigationDefaultMaximumIntentsPerTick,
		worldNavigationDefaultBudgetGainUnits, worldNavigationDefaultBudgetCapUnits,
		worldNavigationDefaultMaxSteps, worldNavigationDefaultMaximumBlockedAttempts,
		worldNavigationDefaultMaximumRetryDelayTicks,
		worldNavigationDefaultFairnessAgingCap); err != nil {
		return fmt.Errorf("bootstrap world navigation profile: %w", err)
	}
	return nil
}

func loadWorldNavigationProfile(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*WorldNavigationProfile, error) {
	profile := &WorldNavigationProfile{}
	err := queryer.QueryRowContext(ctx, `
SELECT profile_version, baseline_tick, maximum_intents_per_tick,
       default_budget_gain_units, default_budget_cap_units, default_max_steps,
       maximum_blocked_attempts, maximum_retry_delay_ticks, fairness_aging_cap,
       revision, metadata
FROM world_navigation_profiles WHERE world_id = $1`, worldID).Scan(
		&profile.ProfileVersion, &profile.BaselineTick, &profile.MaximumIntentsPerTick,
		&profile.DefaultBudgetGainUnits, &profile.DefaultBudgetCapUnits,
		&profile.DefaultMaxSteps, &profile.MaximumBlockedAttempts,
		&profile.MaximumRetryDelayTicks, &profile.FairnessAgingCap,
		&profile.Revision, &profile.Metadata,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrWorldNavigationIntentUnavailable
	}
	if err != nil {
		return nil, fmt.Errorf("load world navigation profile: %w", err)
	}
	if err = validateWorldNavigationProfile(*profile); err != nil {
		return nil, err
	}
	return profile, nil
}

func validateWorldNavigationProfile(profile WorldNavigationProfile) error {
	if profile.ProfileVersion != worldNavigationProfileVersion ||
		profile.BaselineTick < 0 || profile.MaximumIntentsPerTick < 1 ||
		profile.MaximumIntentsPerTick > 4096 || profile.DefaultBudgetGainUnits < 1 ||
		profile.DefaultBudgetCapUnits < profile.DefaultBudgetGainUnits ||
		profile.DefaultMaxSteps < 1 || profile.DefaultMaxSteps > 1024 ||
		profile.MaximumBlockedAttempts < 1 || profile.MaximumRetryDelayTicks < 1 ||
		profile.FairnessAgingCap < 1 || profile.Revision != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{
			"field": "world_navigation_profile",
		})
	}
	return nil
}

const worldNavigationIntentSelect = `
SELECT intent.id, intent.actor_id, source.id, actor.code, intent.intent_code,
       intent.destination_x, intent.destination_y, intent.destination_z,
       intent.status, intent.on_blocked, intent.priority, intent.max_steps,
       intent.budget_units, intent.budget_gain_units, intent.budget_cap_units,
       intent.blocked_attempts, intent.last_reason, intent.next_attempt_tick,
       intent.created_tick, intent.updated_tick, source.tick, source.sequence,
       intent.version, intent.metadata
FROM world_actor_navigation_intents intent
JOIN world_actors actor
  ON actor.id = intent.actor_id AND actor.world_id = intent.world_id
JOIN world_runtime_facts source
  ON source.id = intent.source_fact_id AND source.world_id = intent.world_id
`

func scanWorldNavigationIntentRecord(row cityScannable) (*worldNavigationIntentRecord, error) {
	record := &worldNavigationIntentRecord{}
	var lastReason sql.NullString
	if err := row.Scan(
		&record.id, &record.actorID, &record.sourceFactID, &record.intent.ActorCode,
		&record.intent.IntentCode, &record.intent.Destination.X,
		&record.intent.Destination.Y, &record.intent.Destination.Z,
		&record.intent.Status, &record.intent.OnBlocked, &record.intent.Priority,
		&record.intent.MaxSteps, &record.intent.BudgetUnits,
		&record.intent.BudgetGainUnits, &record.intent.BudgetCapUnits,
		&record.intent.BlockedAttempts, &lastReason,
		&record.intent.NextAttemptTick, &record.intent.CreatedTick,
		&record.intent.UpdatedTick, &record.intent.SourceFact.Tick,
		&record.intent.SourceFact.Sequence, &record.intent.Version,
		&record.intent.Metadata,
	); err != nil {
		return nil, err
	}
	if lastReason.Valid {
		record.intent.LastReason = stringPointer(lastReason.String)
	}
	return record, nil
}

func loadWorldNavigationIntentRecord(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	actorCode string,
	forUpdate bool,
) (*worldNavigationIntentRecord, error) {
	query := worldNavigationIntentSelect + `
WHERE intent.world_id = $1 AND actor.code = $2`
	if forUpdate {
		query += ` FOR UPDATE OF intent`
	}
	record, err := scanWorldNavigationIntentRecord(queryer.QueryRowContext(ctx, query, worldID, actorCode))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrWorldNavigationIntentUnavailable
	}
	if err != nil {
		return nil, fmt.Errorf("load world navigation intent %s: %w", actorCode, err)
	}
	return record, nil
}

func loadWorldNavigationIntentRecords(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) ([]worldNavigationIntentRecord, error) {
	rows, err := queryer.QueryContext(ctx, worldNavigationIntentSelect+`
WHERE intent.world_id = $1
ORDER BY actor.code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load world navigation intents: %w", err)
	}
	items := make([]worldNavigationIntentRecord, 0)
	for rows.Next() {
		item, scanErr := scanWorldNavigationIntentRecord(rows)
		if scanErr != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan world navigation intent: %w", scanErr)
		}
		items = append(items, *item)
	}
	if err = closeCityRows(rows, "iterate world navigation intents"); err != nil {
		return nil, err
	}
	return items, nil
}

func loadWorldNavigationIntents(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) ([]WorldActorNavigationIntent, error) {
	records, err := loadWorldNavigationIntentRecords(ctx, queryer, worldID)
	if err != nil {
		return nil, err
	}
	items := make([]WorldActorNavigationIntent, len(records))
	for index := range records {
		items[index] = records[index].intent
	}
	return items, nil
}

func (s *CityEconomyService) ListWorldNavigationIntents(
	ctx context.Context,
	input WorldNavigationIntentQueryInput,
) ([]WorldActorNavigationIntent, error) {
	if input.UserID <= 0 || input.WorldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, input.UserID, input.WorldID); err != nil {
		return nil, err
	}
	var version string
	if err := s.db.QueryRowContext(ctx, `
SELECT simulation_version FROM city_worlds WHERE id = $1`, input.WorldID).Scan(&version); err != nil {
		return nil, fmt.Errorf("load navigation-intent world version: %w", err)
	}
	if !cityEngineSupportsWorldNavigationIntents(version) {
		return nil, ErrWorldNavigationIntentUnavailable
	}
	return loadWorldNavigationIntents(ctx, s.db, input.WorldID)
}

func (s *CityEconomyService) GetWorldNavigationIntent(
	ctx context.Context,
	input WorldNavigationIntentQueryInput,
) (*WorldActorNavigationIntent, error) {
	input.ActorCode = strings.ToLower(strings.TrimSpace(input.ActorCode))
	if input.UserID <= 0 || input.WorldID <= 0 ||
		!worldRuntimeCodeValid(input.ActorCode, 128) {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, input.UserID, input.WorldID); err != nil {
		return nil, err
	}
	record, err := loadWorldNavigationIntentRecord(ctx, s.db, input.WorldID, input.ActorCode, false)
	if err != nil {
		return nil, err
	}
	return &record.intent, nil
}

func (s *CityEconomyService) ListWorldNavigationReservations(
	ctx context.Context,
	input WorldNavigationReservationQueryInput,
) ([]WorldNavigationReservation, error) {
	if input.UserID <= 0 || input.WorldID <= 0 || input.Tick != nil && *input.Tick < 1 {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, input.UserID, input.WorldID); err != nil {
		return nil, err
	}
	var version string
	var currentTick int64
	if err := s.db.QueryRowContext(ctx, `
SELECT simulation_version, current_tick FROM city_worlds WHERE id = $1`, input.WorldID).Scan(
		&version, &currentTick,
	); err != nil {
		return nil, fmt.Errorf("load navigation reservation world: %w", err)
	}
	if !cityEngineSupportsWorldNavigationIntents(version) {
		return nil, ErrWorldNavigationIntentUnavailable
	}
	tick := currentTick
	if input.Tick != nil {
		tick = *input.Tick
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT reservation.tick, reservation.sequence, actor.code, reservation.intent_code,
       reservation.from_x, reservation.from_y, reservation.from_z,
       reservation.to_x, reservation.to_y, reservation.to_z,
       reservation.target_key, reservation.edge_key, reservation.step_cost,
       source.tick, source.sequence, reservation.status, reservation.metadata
FROM world_navigation_reservations reservation
JOIN world_actors actor
  ON actor.id = reservation.actor_id AND actor.world_id = reservation.world_id
JOIN world_runtime_facts source
  ON source.id = reservation.source_fact_id AND source.world_id = reservation.world_id
WHERE reservation.world_id = $1 AND reservation.tick = $2
ORDER BY reservation.sequence ASC`, input.WorldID, tick)
	if err != nil {
		return nil, fmt.Errorf("load world navigation reservations: %w", err)
	}
	items := make([]WorldNavigationReservation, 0)
	for rows.Next() {
		var item WorldNavigationReservation
		if err = rows.Scan(
			&item.Tick, &item.Sequence, &item.ActorCode, &item.IntentCode,
			&item.From.X, &item.From.Y, &item.From.Z,
			&item.To.X, &item.To.Y, &item.To.Z, &item.TargetKey,
			&item.EdgeKey, &item.StepCost, &item.SourceFact.Tick,
			&item.SourceFact.Sequence, &item.Status, &item.Metadata,
		); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan world navigation reservation: %w", err)
		}
		items = append(items, item)
	}
	if err = closeCityRows(rows, "iterate world navigation reservations"); err != nil {
		return nil, err
	}
	return items, nil
}

func worldNavigationCoordinateKey(coordinate CityNavigationCoordinate) string {
	return strconv.FormatInt(coordinate.X, 10) + ":" +
		strconv.FormatInt(coordinate.Y, 10) + ":" + strconv.FormatInt(int64(coordinate.Z), 10)
}

func worldNavigationEdgeKey(from, to CityNavigationCoordinate) string {
	left, right := worldNavigationCoordinateKey(from), worldNavigationCoordinateKey(to)
	if left > right {
		left, right = right, left
	}
	return left + "|" + right
}

func worldNavigationIntentCode(commandSequence int64) string {
	return fmt.Sprintf("navigation_intent_%020d", commandSequence)
}

func sortWorldNavigationIntents(items []WorldActorNavigationIntent) {
	sort.SliceStable(items, func(left, right int) bool {
		return items[left].ActorCode < items[right].ActorCode
	})
}

func worldNavigationLastReason(value string) *string {
	if value == "" {
		return nil
	}
	return stringPointer(value)
}
