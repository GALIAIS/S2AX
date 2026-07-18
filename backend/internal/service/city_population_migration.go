package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	CityCommandTypePopulationImmigrate = "population.immigrate"
	CityCommandTypePopulationEmigrate  = "population.emigrate"
	CityCommandTypePopulationRelocate  = "population.relocate"

	CityPopulationMigrationImmigration        = "immigration"
	CityPopulationMigrationEmigration         = "emigration"
	CityPopulationMigrationDistrictRelocation = "district_relocation"

	cityPopulationMigrationDirectionInflow  = "inflow"
	cityPopulationMigrationDirectionOutflow = "outflow"
	cityPopulationMigrationMaximumUnits     = int64(1_000_000_000)
	cityPopulationMigrationDefaultLimit     = 50
	cityPopulationMigrationMaximumLimit     = 200

	cityPopulationMigrationRejectionScope      = "CITY_POPULATION_MIGRATION_SCOPE_NOT_FOUND"
	cityPopulationMigrationRejectionPopulation = "CITY_POPULATION_MIGRATION_POPULATION_UNAVAILABLE"
	cityPopulationMigrationRejectionEmployment = "CITY_POPULATION_MIGRATION_EMPLOYMENT_FLOOR"
	cityPopulationMigrationRejectionHousing    = "CITY_POPULATION_MIGRATION_HOUSING_FLOOR"
)

var ErrCityPopulationMigrationNotFound = infraerrors.NotFound(
	"CITY_POPULATION_MIGRATION_NOT_FOUND", "city population migration not found",
)

type CityPopulationMigrationLine struct {
	ID                       int64     `json:"id"`
	MigrationID              int64     `json:"migration_id"`
	WorldID                  int64     `json:"world_id"`
	LineNo                   int       `json:"line_no"`
	CohortID                 int64     `json:"cohort_id"`
	DistrictCode             string    `json:"district_code"`
	EntityCode               string    `json:"entity_code"`
	IncomeBand               string    `json:"income_band"`
	Direction                string    `json:"direction"`
	DemographicVersionBefore int64     `json:"demographic_version_before"`
	DemographicVersionAfter  int64     `json:"demographic_version_after"`
	CohortVersionBefore      int64     `json:"cohort_version_before"`
	CohortVersionAfter       int64     `json:"cohort_version_after"`
	ChildUnits               int64     `json:"child_units"`
	WorkingUnits             int64     `json:"working_units"`
	SeniorUnits              int64     `json:"senior_units"`
	ChildUnitsBefore         int64     `json:"child_units_before"`
	WorkingUnitsBefore       int64     `json:"working_units_before"`
	SeniorUnitsBefore        int64     `json:"senior_units_before"`
	ChildUnitsAfter          int64     `json:"child_units_after"`
	WorkingUnitsAfter        int64     `json:"working_units_after"`
	SeniorUnitsAfter         int64     `json:"senior_units_after"`
	CreatedAt                time.Time `json:"created_at"`
}

type CityPopulationMigration struct {
	ID                 int64                          `json:"id"`
	WorldID            int64                          `json:"world_id"`
	Tick               int64                          `json:"tick"`
	Sequence           int64                          `json:"sequence"`
	SourceCommandID    int64                          `json:"source_command_id"`
	MigrationType      string                         `json:"migration_type"`
	SourceCohortID     *int64                         `json:"source_cohort_id,omitempty"`
	SourceDistrictCode *string                        `json:"source_district_code,omitempty"`
	SourceEntityCode   *string                        `json:"source_entity_code,omitempty"`
	SourceIncomeBand   *string                        `json:"source_income_band,omitempty"`
	TargetCohortID     *int64                         `json:"target_cohort_id,omitempty"`
	TargetDistrictCode *string                        `json:"target_district_code,omitempty"`
	TargetEntityCode   *string                        `json:"target_entity_code,omitempty"`
	TargetIncomeBand   *string                        `json:"target_income_band,omitempty"`
	ChildUnits         int64                          `json:"child_units"`
	WorkingUnits       int64                          `json:"working_units"`
	SeniorUnits        int64                          `json:"senior_units"`
	ExpectedLineCount  int                            `json:"expected_line_count"`
	Metadata           map[string]any                 `json:"metadata"`
	PostedAt           *time.Time                     `json:"posted_at,omitempty"`
	CreatedAt          time.Time                      `json:"created_at"`
	Lines              []*CityPopulationMigrationLine `json:"lines,omitempty"`
}

type CityPopulationMigrationCursor struct {
	Tick     int64 `json:"tick"`
	Sequence int64 `json:"sequence"`
}

type CityPopulationMigrationPage struct {
	Items      []*CityPopulationMigration     `json:"items"`
	NextCursor *CityPopulationMigrationCursor `json:"next_cursor,omitempty"`
}

type CityPopulationMigrationListInput struct {
	UserID        int64
	WorldID       int64
	AfterTick     int64
	AfterSequence int64
	Limit         int
}

type cityPopulationUnits struct {
	child   int64
	working int64
	senior  int64
}

type cityPopulationImmigrationPayload struct {
	TargetDistrictCode string `json:"target_district_code"`
	IncomeBand         string `json:"income_band"`
	ChildUnits         int64  `json:"child_units"`
	WorkingUnits       int64  `json:"working_units"`
	SeniorUnits        int64  `json:"senior_units"`
	Reason             string `json:"reason,omitempty"`
}

type cityPopulationEmigrationPayload struct {
	SourceDistrictCode string `json:"source_district_code"`
	IncomeBand         string `json:"income_band"`
	ChildUnits         int64  `json:"child_units"`
	WorkingUnits       int64  `json:"working_units"`
	SeniorUnits        int64  `json:"senior_units"`
	Reason             string `json:"reason,omitempty"`
}

type cityPopulationRelocationPayload struct {
	SourceDistrictCode string `json:"source_district_code"`
	TargetDistrictCode string `json:"target_district_code"`
	IncomeBand         string `json:"income_band"`
	ChildUnits         int64  `json:"child_units"`
	WorkingUnits       int64  `json:"working_units"`
	SeniorUnits        int64  `json:"senior_units"`
	Reason             string `json:"reason,omitempty"`
}

type cityPopulationMigrationPlan struct {
	before       cityDemographicCohortRef
	direction    string
	units        cityPopulationUnits
	childAfter   int64
	workingAfter int64
	seniorAfter  int64
}

type cityPopulationMigrationBusinessError struct {
	code string
}

func (e *cityPopulationMigrationBusinessError) Error() string { return e.code }

func cityPopulationMigrationReject(code string) error {
	return &cityPopulationMigrationBusinessError{code: code}
}

func normalizeCityPopulationMigrationCommand(commandType string, rawPayload json.RawMessage) (any, bool, error) {
	normalizeScope := func(value *string, field string) error {
		*value = strings.ToLower(strings.TrimSpace(*value))
		if utf8.RuneCountInString(*value) > 32 || !cityPhysicalCodePattern.MatchString(*value) {
			return ErrCityInvalidInput.WithMetadata(map[string]string{"field": field})
		}
		return nil
	}
	normalizeIncomeBand := func(value *string) error {
		*value = strings.ToLower(strings.TrimSpace(*value))
		if *value != "low" && *value != "middle" && *value != "high" {
			return ErrCityInvalidInput.WithMetadata(map[string]string{"field": "income_band"})
		}
		return nil
	}
	normalizeReason := func(value *string) error {
		*value = strings.TrimSpace(*value)
		if utf8.RuneCountInString(*value) > 256 {
			return ErrCityInvalidInput.WithMetadata(map[string]string{"field": "reason"})
		}
		return nil
	}
	validateUnits := func(child, working, senior int64) error {
		_, err := validateCityPopulationMigrationUnits(cityPopulationUnits{child: child, working: working, senior: senior})
		if err != nil {
			return ErrCityInvalidInput.WithMetadata(map[string]string{"field": "population_units"}).WithCause(err)
		}
		return nil
	}

	switch commandType {
	case CityCommandTypePopulationImmigrate:
		var value cityPopulationImmigrationPayload
		if err := decodeStrictCityObject(rawPayload, &value); err != nil {
			return nil, true, ErrCityInvalidInput.WithCause(err)
		}
		if err := normalizeScope(&value.TargetDistrictCode, "target_district_code"); err != nil {
			return nil, true, err
		}
		if err := normalizeIncomeBand(&value.IncomeBand); err != nil {
			return nil, true, err
		}
		if err := validateUnits(value.ChildUnits, value.WorkingUnits, value.SeniorUnits); err != nil {
			return nil, true, err
		}
		if err := normalizeReason(&value.Reason); err != nil {
			return nil, true, err
		}
		return value, true, nil
	case CityCommandTypePopulationEmigrate:
		var value cityPopulationEmigrationPayload
		if err := decodeStrictCityObject(rawPayload, &value); err != nil {
			return nil, true, ErrCityInvalidInput.WithCause(err)
		}
		if err := normalizeScope(&value.SourceDistrictCode, "source_district_code"); err != nil {
			return nil, true, err
		}
		if err := normalizeIncomeBand(&value.IncomeBand); err != nil {
			return nil, true, err
		}
		if err := validateUnits(value.ChildUnits, value.WorkingUnits, value.SeniorUnits); err != nil {
			return nil, true, err
		}
		if err := normalizeReason(&value.Reason); err != nil {
			return nil, true, err
		}
		return value, true, nil
	case CityCommandTypePopulationRelocate:
		var value cityPopulationRelocationPayload
		if err := decodeStrictCityObject(rawPayload, &value); err != nil {
			return nil, true, ErrCityInvalidInput.WithCause(err)
		}
		if err := normalizeScope(&value.SourceDistrictCode, "source_district_code"); err != nil {
			return nil, true, err
		}
		if err := normalizeScope(&value.TargetDistrictCode, "target_district_code"); err != nil {
			return nil, true, err
		}
		if value.SourceDistrictCode == value.TargetDistrictCode {
			return nil, true, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "target_district_code"})
		}
		if err := normalizeIncomeBand(&value.IncomeBand); err != nil {
			return nil, true, err
		}
		if err := validateUnits(value.ChildUnits, value.WorkingUnits, value.SeniorUnits); err != nil {
			return nil, true, err
		}
		if err := normalizeReason(&value.Reason); err != nil {
			return nil, true, err
		}
		return value, true, nil
	default:
		return nil, false, nil
	}
}

func isCityPopulationMigrationCommand(commandType string) bool {
	switch commandType {
	case CityCommandTypePopulationImmigrate, CityCommandTypePopulationEmigrate,
		CityCommandTypePopulationRelocate:
		return true
	default:
		return false
	}
}

func validateCityPopulationMigrationUnits(units cityPopulationUnits) (int64, error) {
	if units.child < 0 || units.working < 0 || units.senior < 0 {
		return 0, fmt.Errorf("population migration units cannot be negative")
	}
	total, err := addCityLedgerUnits(units.child, units.working)
	if err != nil {
		return 0, err
	}
	total, err = addCityLedgerUnits(total, units.senior)
	if err != nil {
		return 0, err
	}
	if total <= 0 || total > cityPopulationMigrationMaximumUnits {
		return 0, fmt.Errorf("population migration total is out of range")
	}
	return total, nil
}

func calculateCityPopulationMigrationPlan(
	ref cityDemographicCohortRef,
	direction string,
	units cityPopulationUnits,
) (cityPopulationMigrationPlan, error) {
	plan := cityPopulationMigrationPlan{before: ref, direction: direction, units: units}
	if _, err := validateCityPopulationMigrationUnits(units); err != nil {
		return plan, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "population_migration_units"}).WithCause(err)
	}
	if ref.demographicVersion == math.MaxInt64 || ref.cohortVersion == math.MaxInt64 {
		return plan, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "population_migration_version"})
	}
	switch direction {
	case cityPopulationMigrationDirectionInflow:
		var err error
		plan.childAfter, err = addCityLedgerUnits(ref.childUnits, units.child)
		if err != nil {
			return plan, err
		}
		plan.workingAfter, err = addCityLedgerUnits(ref.workingUnits, units.working)
		if err != nil {
			return plan, err
		}
		plan.seniorAfter, err = addCityLedgerUnits(ref.seniorUnits, units.senior)
		if err != nil {
			return plan, err
		}
	case cityPopulationMigrationDirectionOutflow:
		if units.child > ref.childUnits || units.working > ref.workingUnits || units.senior > ref.seniorUnits {
			return plan, cityPopulationMigrationReject(cityPopulationMigrationRejectionPopulation)
		}
		plan.childAfter = ref.childUnits - units.child
		plan.workingAfter = ref.workingUnits - units.working
		plan.seniorAfter = ref.seniorUnits - units.senior
	default:
		return plan, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "population_migration_direction"})
	}
	afterPopulation, err := addCityLedgerUnits(plan.childAfter, plan.workingAfter)
	if err != nil {
		return plan, err
	}
	afterPopulation, err = addCityLedgerUnits(afterPopulation, plan.seniorAfter)
	if err != nil {
		return plan, err
	}
	if afterPopulation <= 0 {
		return plan, cityPopulationMigrationReject(cityPopulationMigrationRejectionPopulation)
	}
	if plan.workingAfter < ref.employedUnits {
		return plan, cityPopulationMigrationReject(cityPopulationMigrationRejectionEmployment)
	}
	if afterPopulation < ref.housingDemandUnits {
		return plan, cityPopulationMigrationReject(cityPopulationMigrationRejectionHousing)
	}
	return plan, nil
}

func (s *CityEconomyService) applyCityPopulationMigrationCommand(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, sequence int64,
	command *CityCommand,
) (cityPendingEvent, *CityPopulationMigration, error) {
	if _, err := tx.ExecContext(ctx, `SAVEPOINT city_population_migration_command`); err != nil {
		return cityPendingEvent{}, nil, fmt.Errorf("create city population migration savepoint: %w", err)
	}
	migration, err := s.postCityPopulationMigrationCommand(ctx, tx, worldID, targetTick, sequence, command)
	if err != nil {
		if _, rollbackErr := tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT city_population_migration_command`); rollbackErr != nil {
			return cityPendingEvent{}, nil, fmt.Errorf("rollback city population migration after %v: %w", err, rollbackErr)
		}
		if _, releaseErr := tx.ExecContext(ctx, `RELEASE SAVEPOINT city_population_migration_command`); releaseErr != nil {
			return cityPendingEvent{}, nil, fmt.Errorf("release rejected city population migration: %w", releaseErr)
		}
		var businessErr *cityPopulationMigrationBusinessError
		if errors.As(err, &businessErr) {
			return rejectedCityCommand(command, businessErr.code), nil, nil
		}
		return cityPendingEvent{}, nil, err
	}
	if _, err = tx.ExecContext(ctx, `RELEASE SAVEPOINT city_population_migration_command`); err != nil {
		return cityPendingEvent{}, nil, fmt.Errorf("release city population migration savepoint: %w", err)
	}
	eventType := map[string]string{
		CityPopulationMigrationImmigration:        "city.population.immigrated",
		CityPopulationMigrationEmigration:         "city.population.emigrated",
		CityPopulationMigrationDistrictRelocation: "city.population.relocated",
	}[migration.MigrationType]
	payload := map[string]any{
		"migration_tick": migration.Tick, "migration_sequence": migration.Sequence,
		"migration_type": migration.MigrationType, "child_units": migration.ChildUnits,
		"working_units": migration.WorkingUnits, "senior_units": migration.SeniorUnits,
	}
	if migration.SourceDistrictCode != nil {
		payload["source_district_code"] = *migration.SourceDistrictCode
	}
	if migration.TargetDistrictCode != nil {
		payload["target_district_code"] = *migration.TargetDistrictCode
	}
	return cityPendingEvent{
		command: command, status: CityCommandStatusApplied, eventType: eventType, payload: payload,
		result: map[string]any{
			"applied": true, "migration_tick": migration.Tick,
			"migration_sequence": migration.Sequence, "migration_type": migration.MigrationType,
		},
	}, migration, nil
}

func (s *CityEconomyService) postCityPopulationMigrationCommand(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, sequence int64,
	command *CityCommand,
) (*CityPopulationMigration, error) {
	var migrationType, sourceDistrict, targetDistrict, incomeBand, reason string
	var units cityPopulationUnits
	switch command.CommandType {
	case CityCommandTypePopulationImmigrate:
		payload, err := decodeStoredCityCommandPayload[cityPopulationImmigrationPayload](command)
		if err != nil {
			return nil, err
		}
		migrationType, targetDistrict, incomeBand, reason = CityPopulationMigrationImmigration,
			payload.TargetDistrictCode, payload.IncomeBand, payload.Reason
		units = cityPopulationUnits{child: payload.ChildUnits, working: payload.WorkingUnits, senior: payload.SeniorUnits}
	case CityCommandTypePopulationEmigrate:
		payload, err := decodeStoredCityCommandPayload[cityPopulationEmigrationPayload](command)
		if err != nil {
			return nil, err
		}
		migrationType, sourceDistrict, incomeBand, reason = CityPopulationMigrationEmigration,
			payload.SourceDistrictCode, payload.IncomeBand, payload.Reason
		units = cityPopulationUnits{child: payload.ChildUnits, working: payload.WorkingUnits, senior: payload.SeniorUnits}
	case CityCommandTypePopulationRelocate:
		payload, err := decodeStoredCityCommandPayload[cityPopulationRelocationPayload](command)
		if err != nil {
			return nil, err
		}
		migrationType, sourceDistrict, targetDistrict, incomeBand, reason = CityPopulationMigrationDistrictRelocation,
			payload.SourceDistrictCode, payload.TargetDistrictCode, payload.IncomeBand, payload.Reason
		units = cityPopulationUnits{child: payload.ChildUnits, working: payload.WorkingUnits, senior: payload.SeniorUnits}
	default:
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "population_migration_command"})
	}
	if _, err := validateCityPopulationMigrationUnits(units); err != nil {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "population_migration_units"}).WithCause(err)
	}
	refs, err := loadCityDemographicCohortRefsForUpdate(ctx, tx, worldID)
	if err != nil {
		return nil, err
	}
	find := func(district string) (cityDemographicCohortRef, bool) {
		for _, ref := range refs {
			if ref.districtCode == district && ref.incomeBand == incomeBand {
				return ref, true
			}
		}
		return cityDemographicCohortRef{}, false
	}
	var sourceRef, targetRef cityDemographicCohortRef
	var sourceID, targetID *int64
	plans := make([]cityPopulationMigrationPlan, 0, 2)
	if sourceDistrict != "" {
		var found bool
		sourceRef, found = find(sourceDistrict)
		if !found {
			return nil, cityPopulationMigrationReject(cityPopulationMigrationRejectionScope)
		}
		sourceID = intPointer(sourceRef.cohortID)
		plan, planErr := calculateCityPopulationMigrationPlan(sourceRef, cityPopulationMigrationDirectionOutflow, units)
		if planErr != nil {
			return nil, planErr
		}
		plans = append(plans, plan)
	}
	if targetDistrict != "" {
		var found bool
		targetRef, found = find(targetDistrict)
		if !found {
			return nil, cityPopulationMigrationReject(cityPopulationMigrationRejectionScope)
		}
		targetID = intPointer(targetRef.cohortID)
		plan, planErr := calculateCityPopulationMigrationPlan(targetRef, cityPopulationMigrationDirectionInflow, units)
		if planErr != nil {
			return nil, planErr
		}
		plans = append(plans, plan)
	}
	if len(plans) == 0 || sourceID != nil && targetID != nil && *sourceID == *targetID {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "population_migration_scope"})
	}
	metadata := map[string]any{"command_sequence": command.Sequence, "schema_version": 1}
	if reason != "" {
		metadata["reason"] = reason
	}
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("marshal city population migration metadata: %w", err)
	}
	var migrationID int64
	if err = tx.QueryRowContext(ctx, `
INSERT INTO city_population_migrations
    (world_id, tick, sequence, source_command_id, migration_type,
     source_cohort_id, target_cohort_id, child_units, working_units,
     senior_units, expected_line_count, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb)
RETURNING id`, worldID, targetTick, sequence, command.ID, migrationType,
		cityNullableInt64(sourceID), cityNullableInt64(targetID), units.child,
		units.working, units.senior, len(plans), metadataJSON).Scan(&migrationID); err != nil {
		return nil, fmt.Errorf("insert city population migration: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
SELECT set_config('sub2api.city_f62_migration_id', $1, TRUE)`, cityIntString(migrationID)); err != nil {
		return nil, fmt.Errorf("activate city population migration write gate: %w", err)
	}
	for index, plan := range plans {
		if err = applyCityPopulationMigrationPlan(ctx, tx, migrationID, worldID, index+1, plan); err != nil {
			return nil, err
		}
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_population_migrations SET posted_at = NOW()
WHERE id = $1 AND posted_at IS NULL`, migrationID)
	if err != nil {
		return nil, fmt.Errorf("post city population migration: %w", err)
	}
	if err = cityRowsAffectedExactlyOne(result, "post city population migration"); err != nil {
		return nil, ErrCitySimulationInvariant.WithCause(err)
	}
	return loadCityPopulationMigrationByID(ctx, tx, migrationID, true)
}

func applyCityPopulationMigrationPlan(
	ctx context.Context,
	tx *sql.Tx,
	migrationID, worldID int64,
	lineNo int,
	plan cityPopulationMigrationPlan,
) error {
	beforePopulation, err := addCityLedgerUnits(plan.before.childUnits, plan.before.workingUnits)
	if err != nil {
		return err
	}
	beforePopulation, err = addCityLedgerUnits(beforePopulation, plan.before.seniorUnits)
	if err != nil {
		return err
	}
	afterPopulation, err := addCityLedgerUnits(plan.childAfter, plan.workingAfter)
	if err != nil {
		return err
	}
	afterPopulation, err = addCityLedgerUnits(afterPopulation, plan.seniorAfter)
	if err != nil {
		return err
	}
	demographicVersionAfter := plan.before.demographicVersion + 1
	cohortVersionAfter := plan.before.cohortVersion + 1
	result, err := tx.ExecContext(ctx, `
UPDATE city_demographic_cohort_states
SET child_units = $3, working_units = $4, senior_units = $5,
    version = $6, updated_at = NOW()
WHERE id = $1 AND world_id = $2 AND version = $7
  AND child_units = $8 AND working_units = $9 AND senior_units = $10`,
		plan.before.demographicID, worldID, plan.childAfter, plan.workingAfter,
		plan.seniorAfter, demographicVersionAfter, plan.before.demographicVersion,
		plan.before.childUnits, plan.before.workingUnits, plan.before.seniorUnits)
	if err != nil {
		return fmt.Errorf("post city migration demographic cohort %d: %w", plan.before.cohortID, err)
	}
	if err = cityRowsAffectedExactlyOne(result, "post city migration demographic cohort"); err != nil {
		return ErrCitySimulationInvariant.WithCause(err)
	}
	result, err = tx.ExecContext(ctx, `
UPDATE city_household_cohorts
SET population_units = $3, working_age_units = $4, version = $5, updated_at = NOW()
WHERE id = $1 AND world_id = $2 AND version = $6
  AND population_units = $7 AND working_age_units = $8`,
		plan.before.cohortID, worldID, afterPopulation, plan.workingAfter,
		cohortVersionAfter, plan.before.cohortVersion, beforePopulation, plan.before.workingUnits)
	if err != nil {
		return fmt.Errorf("post city migration household cohort %d: %w", plan.before.cohortID, err)
	}
	if err = cityRowsAffectedExactlyOne(result, "post city migration household cohort"); err != nil {
		return ErrCitySimulationInvariant.WithCause(err)
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO city_population_migration_lines
    (migration_id, world_id, line_no, cohort_id, direction,
     demographic_version_before, demographic_version_after,
     cohort_version_before, cohort_version_after,
     child_units, working_units, senior_units,
     child_units_before, working_units_before, senior_units_before,
     child_units_after, working_units_after, senior_units_after)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12,
        $13, $14, $15, $16, $17, $18)`,
		migrationID, worldID, lineNo, plan.before.cohortID, plan.direction,
		plan.before.demographicVersion, demographicVersionAfter,
		plan.before.cohortVersion, cohortVersionAfter,
		plan.units.child, plan.units.working, plan.units.senior,
		plan.before.childUnits, plan.before.workingUnits, plan.before.seniorUnits,
		plan.childAfter, plan.workingAfter, plan.seniorAfter)
	if err != nil {
		return fmt.Errorf("insert city population migration line %d: %w", lineNo, err)
	}
	return nil
}

const cityPopulationMigrationColumns = `
migration.id, migration.world_id, migration.tick, migration.sequence,
migration.source_command_id, migration.migration_type,
migration.source_cohort_id, source_district.code, source_entity.code, source.income_band,
migration.target_cohort_id, target_district.code, target_entity.code, target.income_band,
migration.child_units, migration.working_units, migration.senior_units,
migration.expected_line_count, migration.metadata, migration.posted_at, migration.created_at`

const cityPopulationMigrationJoins = `
LEFT JOIN city_household_cohorts source ON source.id = migration.source_cohort_id
LEFT JOIN city_districts source_district ON source_district.id = source.district_id
LEFT JOIN city_economic_entities source_entity ON source_entity.id = source.entity_id
LEFT JOIN city_household_cohorts target ON target.id = migration.target_cohort_id
LEFT JOIN city_districts target_district ON target_district.id = target.district_id
LEFT JOIN city_economic_entities target_entity ON target_entity.id = target.entity_id`

func (s *CityEconomyService) ListPopulationMigrations(
	ctx context.Context,
	input CityPopulationMigrationListInput,
) (*CityPopulationMigrationPage, error) {
	if input.UserID <= 0 || input.WorldID <= 0 || input.AfterTick < 0 || input.AfterSequence < 0 {
		return nil, ErrCityInvalidInput
	}
	if input.Limit <= 0 {
		input.Limit = cityPopulationMigrationDefaultLimit
	}
	if input.Limit > cityPopulationMigrationMaximumLimit {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, input.UserID, input.WorldID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT `+cityPopulationMigrationColumns+`
FROM city_population_migrations migration
`+cityPopulationMigrationJoins+`
WHERE migration.world_id = $1 AND migration.posted_at IS NOT NULL
  AND (migration.tick > $2 OR (migration.tick = $2 AND migration.sequence > $3))
ORDER BY migration.tick ASC, migration.sequence ASC
LIMIT $4`, input.WorldID, input.AfterTick, input.AfterSequence, input.Limit+1)
	if err != nil {
		return nil, fmt.Errorf("list city population migrations: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]*CityPopulationMigration, 0, input.Limit)
	for rows.Next() {
		item, scanErr := scanCityPopulationMigration(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate city population migrations: %w", err)
	}
	page := &CityPopulationMigrationPage{Items: items}
	if len(items) > input.Limit {
		items = items[:input.Limit]
		page.Items = items
		last := items[len(items)-1]
		page.NextCursor = &CityPopulationMigrationCursor{Tick: last.Tick, Sequence: last.Sequence}
	}
	return page, nil
}

func (s *CityEconomyService) GetPopulationMigration(
	ctx context.Context,
	userID, worldID, tick, sequence int64,
) (*CityPopulationMigration, error) {
	if userID <= 0 || worldID <= 0 || tick <= 0 || sequence <= 0 {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, userID, worldID); err != nil {
		return nil, err
	}
	item, err := loadCityPopulationMigrationByCursor(ctx, s.db, worldID, tick, sequence, true)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCityPopulationMigrationNotFound
	}
	return item, err
}

func loadCityPopulationMigrationByID(
	ctx context.Context,
	queryer citySQLQueryer,
	migrationID int64,
	withLines bool,
) (*CityPopulationMigration, error) {
	item, err := scanCityPopulationMigration(queryer.QueryRowContext(ctx, `
SELECT `+cityPopulationMigrationColumns+`
FROM city_population_migrations migration
`+cityPopulationMigrationJoins+`
WHERE migration.id = $1 AND migration.posted_at IS NOT NULL`, migrationID))
	if err != nil {
		return nil, err
	}
	if withLines {
		item.Lines, err = loadCityPopulationMigrationLines(ctx, queryer, item.ID)
	}
	return item, err
}

func loadCityPopulationMigrationByCursor(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, tick, sequence int64,
	withLines bool,
) (*CityPopulationMigration, error) {
	item, err := scanCityPopulationMigration(queryer.QueryRowContext(ctx, `
SELECT `+cityPopulationMigrationColumns+`
FROM city_population_migrations migration
`+cityPopulationMigrationJoins+`
WHERE migration.world_id = $1 AND migration.tick = $2 AND migration.sequence = $3
  AND migration.posted_at IS NOT NULL`, worldID, tick, sequence))
	if err != nil {
		return nil, err
	}
	if withLines {
		item.Lines, err = loadCityPopulationMigrationLines(ctx, queryer, item.ID)
	}
	return item, err
}

func loadCityPopulationMigrationsForTick(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, tick int64,
) ([]*CityPopulationMigration, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT `+cityPopulationMigrationColumns+`
FROM city_population_migrations migration
`+cityPopulationMigrationJoins+`
WHERE migration.world_id = $1 AND migration.tick = $2 AND migration.posted_at IS NOT NULL
ORDER BY migration.sequence ASC`, worldID, tick)
	if err != nil {
		return nil, fmt.Errorf("load city population migrations for tick: %w", err)
	}
	items := make([]*CityPopulationMigration, 0)
	for rows.Next() {
		item, scanErr := scanCityPopulationMigration(rows)
		if scanErr != nil {
			_ = rows.Close()
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err = closeCityRows(rows, "iterate city population migrations for tick"); err != nil {
		return nil, err
	}
	for _, item := range items {
		item.Lines, err = loadCityPopulationMigrationLines(ctx, queryer, item.ID)
		if err != nil {
			return nil, err
		}
	}
	return items, nil
}

func scanCityPopulationMigration(scanner cityScannable) (*CityPopulationMigration, error) {
	item := &CityPopulationMigration{}
	var sourceID, targetID sql.NullInt64
	var sourceDistrict, sourceEntity, sourceBand sql.NullString
	var targetDistrict, targetEntity, targetBand sql.NullString
	var metadata []byte
	var postedAt sql.NullTime
	if err := scanner.Scan(
		&item.ID, &item.WorldID, &item.Tick, &item.Sequence,
		&item.SourceCommandID, &item.MigrationType,
		&sourceID, &sourceDistrict, &sourceEntity, &sourceBand,
		&targetID, &targetDistrict, &targetEntity, &targetBand,
		&item.ChildUnits, &item.WorkingUnits, &item.SeniorUnits,
		&item.ExpectedLineCount, &metadata, &postedAt, &item.CreatedAt,
	); err != nil {
		return nil, err
	}
	item.SourceCohortID = nullInt64Pointer(sourceID)
	item.SourceDistrictCode = nullStringPointer(sourceDistrict)
	item.SourceEntityCode = nullStringPointer(sourceEntity)
	item.SourceIncomeBand = nullStringPointer(sourceBand)
	item.TargetCohortID = nullInt64Pointer(targetID)
	item.TargetDistrictCode = nullStringPointer(targetDistrict)
	item.TargetEntityCode = nullStringPointer(targetEntity)
	item.TargetIncomeBand = nullStringPointer(targetBand)
	item.PostedAt = nullTimePointer(postedAt)
	decoded, err := decodeCityJSONMap(metadata)
	if err != nil {
		return nil, fmt.Errorf("decode city population migration metadata: %w", err)
	}
	item.Metadata = decoded
	return item, nil
}

func loadCityPopulationMigrationLines(
	ctx context.Context,
	queryer citySQLQueryer,
	migrationID int64,
) ([]*CityPopulationMigrationLine, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT line.id, line.migration_id, line.world_id, line.line_no, line.cohort_id,
       district.code, entity.code, cohort.income_band, line.direction,
       line.demographic_version_before, line.demographic_version_after,
       line.cohort_version_before, line.cohort_version_after,
       line.child_units, line.working_units, line.senior_units,
       line.child_units_before, line.working_units_before, line.senior_units_before,
       line.child_units_after, line.working_units_after, line.senior_units_after,
       line.created_at
FROM city_population_migration_lines line
JOIN city_household_cohorts cohort ON cohort.id = line.cohort_id
JOIN city_districts district ON district.id = cohort.district_id
JOIN city_economic_entities entity ON entity.id = cohort.entity_id
WHERE line.migration_id = $1
ORDER BY line.line_no ASC`, migrationID)
	if err != nil {
		return nil, fmt.Errorf("load city population migration lines: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]*CityPopulationMigrationLine, 0, 2)
	for rows.Next() {
		item := &CityPopulationMigrationLine{}
		if err = rows.Scan(
			&item.ID, &item.MigrationID, &item.WorldID, &item.LineNo, &item.CohortID,
			&item.DistrictCode, &item.EntityCode, &item.IncomeBand, &item.Direction,
			&item.DemographicVersionBefore, &item.DemographicVersionAfter,
			&item.CohortVersionBefore, &item.CohortVersionAfter,
			&item.ChildUnits, &item.WorkingUnits, &item.SeniorUnits,
			&item.ChildUnitsBefore, &item.WorkingUnitsBefore, &item.SeniorUnitsBefore,
			&item.ChildUnitsAfter, &item.WorkingUnitsAfter, &item.SeniorUnitsAfter,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate city population migration lines: %w", err)
	}
	return items, nil
}

func replayCityPopulationMigration(migration *CityPopulationMigration, state *cityHashState) error {
	if migration == nil || state == nil || migration.PostedAt == nil || migration.SourceCommandID <= 0 ||
		migration.ExpectedLineCount != len(migration.Lines) {
		return fmt.Errorf("population migration header is invalid")
	}
	units := cityPopulationUnits{child: migration.ChildUnits, working: migration.WorkingUnits, senior: migration.SeniorUnits}
	if _, err := validateCityPopulationMigrationUnits(units); err != nil {
		return fmt.Errorf("population migration units are invalid: %w", err)
	}
	type expectedLine struct {
		direction string
		key       string
	}
	keyFor := func(district, entity, band *string) (string, bool) {
		if district == nil || entity == nil || band == nil {
			return "", false
		}
		return cityDemographyCohortKey(*district, *entity, *band), true
	}
	expected := make([]expectedLine, 0, 2)
	sourceKey, hasSource := keyFor(migration.SourceDistrictCode, migration.SourceEntityCode, migration.SourceIncomeBand)
	targetKey, hasTarget := keyFor(migration.TargetDistrictCode, migration.TargetEntityCode, migration.TargetIncomeBand)
	switch migration.MigrationType {
	case CityPopulationMigrationImmigration:
		if hasSource || !hasTarget || migration.ExpectedLineCount != 1 {
			return fmt.Errorf("immigration header shape is invalid")
		}
		expected = append(expected, expectedLine{cityPopulationMigrationDirectionInflow, targetKey})
	case CityPopulationMigrationEmigration:
		if !hasSource || hasTarget || migration.ExpectedLineCount != 1 {
			return fmt.Errorf("emigration header shape is invalid")
		}
		expected = append(expected, expectedLine{cityPopulationMigrationDirectionOutflow, sourceKey})
	case CityPopulationMigrationDistrictRelocation:
		if !hasSource || !hasTarget || sourceKey == targetKey || migration.ExpectedLineCount != 2 ||
			migration.SourceIncomeBand == nil || migration.TargetIncomeBand == nil ||
			*migration.SourceIncomeBand != *migration.TargetIncomeBand ||
			migration.SourceDistrictCode == nil || migration.TargetDistrictCode == nil ||
			*migration.SourceDistrictCode == *migration.TargetDistrictCode {
			return fmt.Errorf("district relocation header shape is invalid")
		}
		expected = append(expected,
			expectedLine{cityPopulationMigrationDirectionOutflow, sourceKey},
			expectedLine{cityPopulationMigrationDirectionInflow, targetKey},
		)
	default:
		return fmt.Errorf("unknown population migration type %s", migration.MigrationType)
	}

	demographicIndex := make(map[string]int, len(state.Demography.Cohorts))
	for index, cohort := range state.Demography.Cohorts {
		demographicIndex[cityDemographyCohortKey(cohort.DistrictCode, cohort.EntityCode, cohort.IncomeBand)] = index
	}
	physicalIndex := make(map[string]int, len(state.Physical.HouseholdCohorts))
	for index, cohort := range state.Physical.HouseholdCohorts {
		physicalIndex[cityDemographyCohortKey(cohort.DistrictCode, cohort.EntityCode, cohort.IncomeBand)] = index
	}
	for index, line := range migration.Lines {
		if line == nil || line.LineNo != index+1 || line.MigrationID != migration.ID ||
			line.WorldID != migration.WorldID || line.Direction != expected[index].direction ||
			line.ChildUnits != migration.ChildUnits || line.WorkingUnits != migration.WorkingUnits ||
			line.SeniorUnits != migration.SeniorUnits {
			return fmt.Errorf("population migration line order is invalid")
		}
		key := cityDemographyCohortKey(line.DistrictCode, line.EntityCode, line.IncomeBand)
		if key != expected[index].key {
			return fmt.Errorf("population migration line scope is invalid")
		}
		demographicPosition, ok := demographicIndex[key]
		if !ok {
			return fmt.Errorf("population migration references unknown demographic cohort")
		}
		physicalPosition, ok := physicalIndex[key]
		if !ok {
			return fmt.Errorf("population migration references unknown household cohort")
		}
		demographic := &state.Demography.Cohorts[demographicPosition]
		physical := &state.Physical.HouseholdCohorts[physicalPosition]
		if demographic.Version != line.DemographicVersionBefore ||
			line.DemographicVersionAfter != line.DemographicVersionBefore+1 ||
			demographic.ChildUnits != line.ChildUnitsBefore ||
			demographic.WorkingUnits != line.WorkingUnitsBefore ||
			demographic.SeniorUnits != line.SeniorUnitsBefore ||
			physical.Version != line.CohortVersionBefore ||
			line.CohortVersionAfter != line.CohortVersionBefore+1 {
			return fmt.Errorf("population migration projection chain is broken")
		}
		beforePopulation, err := addCityLedgerUnits(line.ChildUnitsBefore, line.WorkingUnitsBefore)
		if err != nil {
			return err
		}
		beforePopulation, err = addCityLedgerUnits(beforePopulation, line.SeniorUnitsBefore)
		if err != nil {
			return err
		}
		if physical.PopulationUnits != beforePopulation || physical.WorkingAgeUnits != line.WorkingUnitsBefore {
			return fmt.Errorf("population migration household projection chain is broken")
		}
		plan, err := calculateCityPopulationMigrationPlan(cityDemographicCohortRef{
			childUnits: line.ChildUnitsBefore, workingUnits: line.WorkingUnitsBefore,
			seniorUnits: line.SeniorUnitsBefore, employedUnits: physical.EmployedUnits,
			housingDemandUnits: physical.HousingDemandUnits,
			demographicVersion: line.DemographicVersionBefore, cohortVersion: line.CohortVersionBefore,
		}, line.Direction, units)
		if err != nil {
			return fmt.Errorf("population migration line violates projection constraints: %w", err)
		}
		if plan.childAfter != line.ChildUnitsAfter || plan.workingAfter != line.WorkingUnitsAfter ||
			plan.seniorAfter != line.SeniorUnitsAfter {
			return fmt.Errorf("population migration line equation is invalid")
		}
		afterPopulation, err := addCityLedgerUnits(plan.childAfter, plan.workingAfter)
		if err != nil {
			return err
		}
		afterPopulation, err = addCityLedgerUnits(afterPopulation, plan.seniorAfter)
		if err != nil {
			return err
		}
		demographic.ChildUnits = plan.childAfter
		demographic.WorkingUnits = plan.workingAfter
		demographic.SeniorUnits = plan.seniorAfter
		demographic.Version = line.DemographicVersionAfter
		physical.PopulationUnits = afterPopulation
		physical.WorkingAgeUnits = plan.workingAfter
		physical.Version = line.CohortVersionAfter
	}
	return nil
}
