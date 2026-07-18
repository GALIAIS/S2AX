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
	CityCommandTypeHouseholdAdjust     = "household.adjust"
	CityCommandTypeHouseholdReclassify = "household.reclassify"

	CityHouseholdMovementFormation              = "formation"
	CityHouseholdMovementSplit                  = "split"
	CityHouseholdMovementMerge                  = "merge"
	CityHouseholdMovementDissolution            = "dissolution"
	CityHouseholdMovementIncomeReclassification = "income_reclassification"

	CityHouseholdMovementOriginCommand         = "command"
	CityHouseholdMovementOriginDemographyGuard = "demography_guard"

	cityHouseholdMovementDirectionInflow  = "inflow"
	cityHouseholdMovementDirectionOutflow = "outflow"
	cityHouseholdMovementMaximumUnits     = int64(1_000_000_000)
	cityHouseholdMovementDefaultLimit     = 50
	cityHouseholdMovementMaximumLimit     = 200

	cityHouseholdRejectionScope       = "CITY_HOUSEHOLD_SCOPE_NOT_FOUND"
	cityHouseholdRejectionPopulation  = "CITY_HOUSEHOLD_POPULATION_FLOOR"
	cityHouseholdRejectionEmployment  = "CITY_HOUSEHOLD_EMPLOYMENT_FLOOR"
	cityHouseholdRejectionOccupancy   = "CITY_HOUSEHOLD_OCCUPANCY_FLOOR"
	cityHouseholdRejectionNonAdjacent = "CITY_HOUSEHOLD_RECLASSIFICATION_NON_ADJACENT"
	cityHouseholdRejectionEntity      = "CITY_HOUSEHOLD_ENTITY_BOUNDARY"
)

var ErrCityHouseholdMovementNotFound = infraerrors.NotFound(
	"CITY_HOUSEHOLD_MOVEMENT_NOT_FOUND", "city household movement not found",
)

type CityHouseholdMovementLine struct {
	ID                       int64     `json:"id"`
	MovementID               int64     `json:"movement_id"`
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
	OccupancyVersionBefore   int64     `json:"occupancy_version_before"`
	OccupancyVersionAfter    int64     `json:"occupancy_version_after"`
	ChildUnits               int64     `json:"child_units"`
	WorkingUnits             int64     `json:"working_units"`
	SeniorUnits              int64     `json:"senior_units"`
	EmployedUnits            int64     `json:"employed_units"`
	HouseholdUnits           int64     `json:"household_units"`
	OccupiedUnits            int64     `json:"occupied_units"`
	ChildUnitsBefore         int64     `json:"child_units_before"`
	WorkingUnitsBefore       int64     `json:"working_units_before"`
	SeniorUnitsBefore        int64     `json:"senior_units_before"`
	EmployedUnitsBefore      int64     `json:"employed_units_before"`
	HouseholdUnitsBefore     int64     `json:"household_units_before"`
	OccupiedUnitsBefore      int64     `json:"occupied_units_before"`
	UnmetUnitsBefore         int64     `json:"unmet_units_before"`
	ChildUnitsAfter          int64     `json:"child_units_after"`
	WorkingUnitsAfter        int64     `json:"working_units_after"`
	SeniorUnitsAfter         int64     `json:"senior_units_after"`
	EmployedUnitsAfter       int64     `json:"employed_units_after"`
	HouseholdUnitsAfter      int64     `json:"household_units_after"`
	OccupiedUnitsAfter       int64     `json:"occupied_units_after"`
	UnmetUnitsAfter          int64     `json:"unmet_units_after"`
	CreatedAt                time.Time `json:"created_at"`
}

type CityHouseholdMovement struct {
	ID                 int64                        `json:"id"`
	WorldID            int64                        `json:"world_id"`
	Tick               int64                        `json:"tick"`
	Sequence           int64                        `json:"sequence"`
	Origin             string                       `json:"origin"`
	SourceCommandID    *int64                       `json:"source_command_id,omitempty"`
	MovementType       string                       `json:"movement_type"`
	SourceCohortID     *int64                       `json:"source_cohort_id,omitempty"`
	SourceDistrictCode *string                      `json:"source_district_code,omitempty"`
	SourceEntityCode   *string                      `json:"source_entity_code,omitempty"`
	SourceIncomeBand   *string                      `json:"source_income_band,omitempty"`
	TargetCohortID     *int64                       `json:"target_cohort_id,omitempty"`
	TargetDistrictCode *string                      `json:"target_district_code,omitempty"`
	TargetEntityCode   *string                      `json:"target_entity_code,omitempty"`
	TargetIncomeBand   *string                      `json:"target_income_band,omitempty"`
	ChildUnits         int64                        `json:"child_units"`
	WorkingUnits       int64                        `json:"working_units"`
	SeniorUnits        int64                        `json:"senior_units"`
	EmployedUnits      int64                        `json:"employed_units"`
	HouseholdUnits     int64                        `json:"household_units"`
	OccupiedUnits      int64                        `json:"occupied_units"`
	ExpectedLineCount  int                          `json:"expected_line_count"`
	Metadata           map[string]any               `json:"metadata"`
	PostedAt           *time.Time                   `json:"posted_at,omitempty"`
	CreatedAt          time.Time                    `json:"created_at"`
	Lines              []*CityHouseholdMovementLine `json:"lines,omitempty"`
}

type CityHouseholdMovementCursor struct {
	Tick     int64 `json:"tick"`
	Sequence int64 `json:"sequence"`
}

type CityHouseholdMovementPage struct {
	Items      []*CityHouseholdMovement     `json:"items"`
	NextCursor *CityHouseholdMovementCursor `json:"next_cursor,omitempty"`
}

type CityHouseholdMovementListInput struct {
	UserID        int64
	WorldID       int64
	AfterTick     int64
	AfterSequence int64
	Limit         int
}

type cityHouseholdAdjustmentPayload struct {
	DistrictCode   string `json:"district_code"`
	IncomeBand     string `json:"income_band"`
	MovementType   string `json:"movement_type"`
	HouseholdUnits int64  `json:"household_units"`
	Reason         string `json:"reason,omitempty"`
}

type cityHouseholdReclassificationPayload struct {
	DistrictCode     string `json:"district_code"`
	SourceIncomeBand string `json:"source_income_band"`
	TargetIncomeBand string `json:"target_income_band"`
	ChildUnits       int64  `json:"child_units"`
	WorkingUnits     int64  `json:"working_units"`
	SeniorUnits      int64  `json:"senior_units"`
	EmployedUnits    int64  `json:"employed_units"`
	HouseholdUnits   int64  `json:"household_units"`
	OccupiedUnits    int64  `json:"occupied_units"`
	Reason           string `json:"reason,omitempty"`
}

type cityHouseholdMovementUnits struct {
	child     int64
	working   int64
	senior    int64
	employed  int64
	household int64
	occupied  int64
}

type cityHouseholdCohortRef struct {
	demographicID      int64
	cohortID           int64
	occupancyID        int64
	districtCode       string
	entityCode         string
	entityID           int64
	incomeBand         string
	childUnits         int64
	workingUnits       int64
	seniorUnits        int64
	employedUnits      int64
	householdUnits     int64
	occupiedUnits      int64
	unmetUnits         int64
	demographicVersion int64
	cohortVersion      int64
	occupancyVersion   int64
}

type cityHouseholdMovementPlan struct {
	before             cityHouseholdCohortRef
	direction          string
	units              cityHouseholdMovementUnits
	demographicChanged bool
	childAfter         int64
	workingAfter       int64
	seniorAfter        int64
	employedAfter      int64
	householdAfter     int64
	occupiedAfter      int64
	unmetAfter         int64
}

type cityHouseholdMovementBusinessError struct{ code string }

func (e *cityHouseholdMovementBusinessError) Error() string { return e.code }

func cityHouseholdMovementReject(code string) error {
	return &cityHouseholdMovementBusinessError{code: code}
}

func normalizeCityHouseholdMovementCommand(commandType string, rawPayload json.RawMessage) (any, bool, error) {
	normalizeScope := func(value *string, field string) error {
		*value = strings.ToLower(strings.TrimSpace(*value))
		if utf8.RuneCountInString(*value) > 32 || !cityPhysicalCodePattern.MatchString(*value) {
			return ErrCityInvalidInput.WithMetadata(map[string]string{"field": field})
		}
		return nil
	}
	normalizeIncomeBand := func(value *string, field string) error {
		*value = strings.ToLower(strings.TrimSpace(*value))
		if cityIncomeBandRank(*value) == 0 {
			return ErrCityInvalidInput.WithMetadata(map[string]string{"field": field})
		}
		return nil
	}
	normalizeReason := func(value *string) error {
		*value = strings.TrimSpace(*value)
		if utf8.RuneCountInString(*value) > 120 {
			return ErrCityInvalidInput.WithMetadata(map[string]string{"field": "reason"})
		}
		return nil
	}
	switch commandType {
	case CityCommandTypeHouseholdAdjust:
		var value cityHouseholdAdjustmentPayload
		if err := decodeStrictCityObject(rawPayload, &value); err != nil {
			return nil, true, ErrCityInvalidInput.WithCause(err)
		}
		if err := normalizeScope(&value.DistrictCode, "district_code"); err != nil {
			return nil, true, err
		}
		if err := normalizeIncomeBand(&value.IncomeBand, "income_band"); err != nil {
			return nil, true, err
		}
		value.MovementType = strings.ToLower(strings.TrimSpace(value.MovementType))
		switch value.MovementType {
		case CityHouseholdMovementFormation, CityHouseholdMovementSplit,
			CityHouseholdMovementMerge, CityHouseholdMovementDissolution:
		default:
			return nil, true, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "movement_type"})
		}
		if value.HouseholdUnits <= 0 || value.HouseholdUnits > cityHouseholdMovementMaximumUnits {
			return nil, true, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "household_units"})
		}
		if err := normalizeReason(&value.Reason); err != nil {
			return nil, true, err
		}
		return value, true, nil
	case CityCommandTypeHouseholdReclassify:
		var value cityHouseholdReclassificationPayload
		if err := decodeStrictCityObject(rawPayload, &value); err != nil {
			return nil, true, ErrCityInvalidInput.WithCause(err)
		}
		if err := normalizeScope(&value.DistrictCode, "district_code"); err != nil {
			return nil, true, err
		}
		if err := normalizeIncomeBand(&value.SourceIncomeBand, "source_income_band"); err != nil {
			return nil, true, err
		}
		if err := normalizeIncomeBand(&value.TargetIncomeBand, "target_income_band"); err != nil {
			return nil, true, err
		}
		units := cityHouseholdMovementUnits{
			child: value.ChildUnits, working: value.WorkingUnits, senior: value.SeniorUnits,
			employed: value.EmployedUnits, household: value.HouseholdUnits, occupied: value.OccupiedUnits,
		}
		if err := validateCityHouseholdReclassificationUnits(units); err != nil {
			return nil, true, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "movement_units"}).WithCause(err)
		}
		if err := normalizeReason(&value.Reason); err != nil {
			return nil, true, err
		}
		return value, true, nil
	default:
		return nil, false, nil
	}
}

func isCityHouseholdMovementCommand(commandType string) bool {
	return commandType == CityCommandTypeHouseholdAdjust || commandType == CityCommandTypeHouseholdReclassify
}

func cityIncomeBandRank(value string) int {
	switch value {
	case "low":
		return 1
	case "middle":
		return 2
	case "high":
		return 3
	default:
		return 0
	}
}

func validateCityHouseholdReclassificationUnits(units cityHouseholdMovementUnits) error {
	for _, value := range []int64{units.child, units.working, units.senior, units.employed, units.household, units.occupied} {
		if value < 0 || value > cityHouseholdMovementMaximumUnits {
			return fmt.Errorf("household movement unit is out of range")
		}
	}
	total, err := addCityLedgerUnits(units.child, units.working)
	if err != nil {
		return err
	}
	total, err = addCityLedgerUnits(total, units.senior)
	if err != nil {
		return err
	}
	if total <= 0 || total > cityHouseholdMovementMaximumUnits || units.household <= 0 ||
		units.household > total || units.employed > units.working || units.occupied > units.household {
		return fmt.Errorf("household movement units violate aggregate bounds")
	}
	return nil
}

func calculateCityHouseholdCountPlan(
	ref cityHouseholdCohortRef,
	movementType string,
	householdUnits int64,
) (cityHouseholdMovementPlan, error) {
	plan := cityHouseholdMovementPlan{
		before: ref, units: cityHouseholdMovementUnits{household: householdUnits},
		childAfter: ref.childUnits, workingAfter: ref.workingUnits,
		seniorAfter: ref.seniorUnits, employedAfter: ref.employedUnits,
	}
	if householdUnits <= 0 || householdUnits > cityHouseholdMovementMaximumUnits ||
		ref.cohortVersion == math.MaxInt64 || ref.occupancyVersion == math.MaxInt64 {
		return plan, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "household_count_movement"})
	}
	population, err := cityHouseholdPopulation(ref.childUnits, ref.workingUnits, ref.seniorUnits)
	if err != nil {
		return plan, err
	}
	switch movementType {
	case CityHouseholdMovementFormation, CityHouseholdMovementSplit:
		plan.direction = cityHouseholdMovementDirectionInflow
		plan.householdAfter, err = addCityLedgerUnits(ref.householdUnits, householdUnits)
		if err != nil {
			return plan, err
		}
		if plan.householdAfter > population {
			return plan, cityHouseholdMovementReject(cityHouseholdRejectionPopulation)
		}
		plan.occupiedAfter = ref.occupiedUnits
	case CityHouseholdMovementMerge, CityHouseholdMovementDissolution:
		plan.direction = cityHouseholdMovementDirectionOutflow
		if householdUnits >= ref.householdUnits {
			return plan, cityHouseholdMovementReject(cityHouseholdRejectionPopulation)
		}
		plan.householdAfter = ref.householdUnits - householdUnits
		plan.occupiedAfter = min(ref.occupiedUnits, plan.householdAfter)
		plan.units.occupied = ref.occupiedUnits - plan.occupiedAfter
	default:
		return plan, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "household_movement_type"})
	}
	plan.unmetAfter = plan.householdAfter - plan.occupiedAfter
	return plan, nil
}

func calculateCityHouseholdReclassificationPlans(
	source, target cityHouseholdCohortRef,
	units cityHouseholdMovementUnits,
) ([]cityHouseholdMovementPlan, error) {
	if err := validateCityHouseholdReclassificationUnits(units); err != nil {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "household_reclassification_units"}).WithCause(err)
	}
	if source.districtCode != target.districtCode {
		return nil, cityHouseholdMovementReject(cityHouseholdRejectionScope)
	}
	if source.entityID != target.entityID {
		return nil, cityHouseholdMovementReject(cityHouseholdRejectionEntity)
	}
	if absInt(cityIncomeBandRank(source.incomeBand)-cityIncomeBandRank(target.incomeBand)) != 1 {
		return nil, cityHouseholdMovementReject(cityHouseholdRejectionNonAdjacent)
	}
	for _, ref := range []cityHouseholdCohortRef{source, target} {
		if ref.demographicVersion == math.MaxInt64 || ref.cohortVersion == math.MaxInt64 ||
			ref.occupancyVersion == math.MaxInt64 {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "household_reclassification_version"})
		}
	}
	if source.childUnits < units.child || source.workingUnits < units.working ||
		source.seniorUnits < units.senior || source.householdUnits <= units.household {
		return nil, cityHouseholdMovementReject(cityHouseholdRejectionPopulation)
	}
	if source.employedUnits < units.employed || source.workingUnits-units.working < source.employedUnits-units.employed {
		return nil, cityHouseholdMovementReject(cityHouseholdRejectionEmployment)
	}
	if source.occupiedUnits < units.occupied {
		return nil, cityHouseholdMovementReject(cityHouseholdRejectionOccupancy)
	}
	sourcePlan := cityHouseholdMovementPlan{
		before: source, direction: cityHouseholdMovementDirectionOutflow, units: units, demographicChanged: true,
		childAfter:     source.childUnits - units.child,
		workingAfter:   source.workingUnits - units.working,
		seniorAfter:    source.seniorUnits - units.senior,
		employedAfter:  source.employedUnits - units.employed,
		householdAfter: source.householdUnits - units.household,
		occupiedAfter:  source.occupiedUnits - units.occupied,
	}
	sourcePlan.unmetAfter = sourcePlan.householdAfter - sourcePlan.occupiedAfter
	sourcePopulation, err := cityHouseholdPopulation(sourcePlan.childAfter, sourcePlan.workingAfter, sourcePlan.seniorAfter)
	if err != nil {
		return nil, err
	}
	if sourcePopulation <= 0 || sourcePlan.householdAfter > sourcePopulation {
		return nil, cityHouseholdMovementReject(cityHouseholdRejectionPopulation)
	}
	if sourcePlan.unmetAfter < 0 {
		return nil, cityHouseholdMovementReject(cityHouseholdRejectionOccupancy)
	}
	targetPlan := cityHouseholdMovementPlan{
		before: target, direction: cityHouseholdMovementDirectionInflow, units: units, demographicChanged: true,
	}
	for destination, pair := range map[*int64][2]int64{
		&targetPlan.childAfter:     {target.childUnits, units.child},
		&targetPlan.workingAfter:   {target.workingUnits, units.working},
		&targetPlan.seniorAfter:    {target.seniorUnits, units.senior},
		&targetPlan.employedAfter:  {target.employedUnits, units.employed},
		&targetPlan.householdAfter: {target.householdUnits, units.household},
		&targetPlan.occupiedAfter:  {target.occupiedUnits, units.occupied},
	} {
		*destination, err = addCityLedgerUnits(pair[0], pair[1])
		if err != nil {
			return nil, err
		}
	}
	targetPlan.unmetAfter = targetPlan.householdAfter - targetPlan.occupiedAfter
	targetPopulation, err := cityHouseholdPopulation(targetPlan.childAfter, targetPlan.workingAfter, targetPlan.seniorAfter)
	if err != nil {
		return nil, err
	}
	if targetPlan.employedAfter > targetPlan.workingAfter {
		return nil, cityHouseholdMovementReject(cityHouseholdRejectionEmployment)
	}
	if targetPlan.householdAfter > targetPopulation {
		return nil, cityHouseholdMovementReject(cityHouseholdRejectionPopulation)
	}
	if targetPlan.unmetAfter < 0 {
		return nil, cityHouseholdMovementReject(cityHouseholdRejectionOccupancy)
	}
	return []cityHouseholdMovementPlan{sourcePlan, targetPlan}, nil
}

func cityHouseholdPopulation(child, working, senior int64) (int64, error) {
	total, err := addCityLedgerUnits(child, working)
	if err != nil {
		return 0, err
	}
	return addCityLedgerUnits(total, senior)
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func (s *CityEconomyService) applyCityHouseholdMovementCommand(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, sequence int64,
	command *CityCommand,
) (cityPendingEvent, *CityHouseholdMovement, error) {
	if _, err := tx.ExecContext(ctx, `SAVEPOINT city_household_movement_command`); err != nil {
		return cityPendingEvent{}, nil, fmt.Errorf("create city household movement savepoint: %w", err)
	}
	movement, err := s.postCityHouseholdMovementCommand(ctx, tx, worldID, targetTick, sequence, command)
	if err != nil {
		if _, rollbackErr := tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT city_household_movement_command`); rollbackErr != nil {
			return cityPendingEvent{}, nil, fmt.Errorf("rollback city household movement after %v: %w", err, rollbackErr)
		}
		if _, releaseErr := tx.ExecContext(ctx, `RELEASE SAVEPOINT city_household_movement_command`); releaseErr != nil {
			return cityPendingEvent{}, nil, fmt.Errorf("release rejected city household movement: %w", releaseErr)
		}
		var businessErr *cityHouseholdMovementBusinessError
		if errors.As(err, &businessErr) {
			return rejectedCityCommand(command, businessErr.code), nil, nil
		}
		return cityPendingEvent{}, nil, err
	}
	if _, err = tx.ExecContext(ctx, `RELEASE SAVEPOINT city_household_movement_command`); err != nil {
		return cityPendingEvent{}, nil, fmt.Errorf("release city household movement savepoint: %w", err)
	}
	payload := cityHouseholdMovementEventPayload(movement)
	return cityPendingEvent{
		command: command, status: CityCommandStatusApplied,
		eventType: "city.household." + movement.MovementType, payload: payload,
		result: map[string]any{
			"applied": true, "movement_tick": movement.Tick,
			"movement_sequence": movement.Sequence, "movement_type": movement.MovementType,
		},
	}, movement, nil
}

func (s *CityEconomyService) postCityHouseholdMovementCommand(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, sequence int64,
	command *CityCommand,
) (*CityHouseholdMovement, error) {
	refs, err := loadCityHouseholdCohortRefsForUpdate(ctx, tx, worldID)
	if err != nil {
		return nil, err
	}
	find := func(district, band string) (cityHouseholdCohortRef, bool) {
		for _, ref := range refs {
			if ref.districtCode == district && ref.incomeBand == band {
				return ref, true
			}
		}
		return cityHouseholdCohortRef{}, false
	}
	switch command.CommandType {
	case CityCommandTypeHouseholdAdjust:
		payload, decodeErr := decodeStoredCityCommandPayload[cityHouseholdAdjustmentPayload](command)
		if decodeErr != nil {
			return nil, decodeErr
		}
		ref, found := find(payload.DistrictCode, payload.IncomeBand)
		if !found {
			return nil, cityHouseholdMovementReject(cityHouseholdRejectionScope)
		}
		plan, planErr := calculateCityHouseholdCountPlan(ref, payload.MovementType, payload.HouseholdUnits)
		if planErr != nil {
			return nil, planErr
		}
		return postCityHouseholdMovement(ctx, tx, cityHouseholdMovementPost{
			worldID: worldID, tick: targetTick, sequence: sequence,
			origin: CityHouseholdMovementOriginCommand, command: command,
			movementType: payload.MovementType, plans: []cityHouseholdMovementPlan{plan}, reason: payload.Reason,
		})
	case CityCommandTypeHouseholdReclassify:
		payload, decodeErr := decodeStoredCityCommandPayload[cityHouseholdReclassificationPayload](command)
		if decodeErr != nil {
			return nil, decodeErr
		}
		source, sourceFound := find(payload.DistrictCode, payload.SourceIncomeBand)
		target, targetFound := find(payload.DistrictCode, payload.TargetIncomeBand)
		if !sourceFound || !targetFound {
			return nil, cityHouseholdMovementReject(cityHouseholdRejectionScope)
		}
		plans, planErr := calculateCityHouseholdReclassificationPlans(source, target, cityHouseholdMovementUnits{
			child: payload.ChildUnits, working: payload.WorkingUnits, senior: payload.SeniorUnits,
			employed: payload.EmployedUnits, household: payload.HouseholdUnits, occupied: payload.OccupiedUnits,
		})
		if planErr != nil {
			return nil, planErr
		}
		return postCityHouseholdMovement(ctx, tx, cityHouseholdMovementPost{
			worldID: worldID, tick: targetTick, sequence: sequence,
			origin: CityHouseholdMovementOriginCommand, command: command,
			movementType: CityHouseholdMovementIncomeReclassification, plans: plans, reason: payload.Reason,
		})
	default:
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "household_movement_command"})
	}
}

type cityHouseholdMovementPost struct {
	worldID      int64
	tick         int64
	sequence     int64
	origin       string
	command      *CityCommand
	movementType string
	plans        []cityHouseholdMovementPlan
	reason       string
}

func postCityHouseholdMovement(
	ctx context.Context,
	tx *sql.Tx,
	input cityHouseholdMovementPost,
) (*CityHouseholdMovement, error) {
	if len(input.plans) == 0 || len(input.plans) > 2 {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "household_movement_plans"})
	}
	var sourceID, targetID *int64
	for index := range input.plans {
		plan := input.plans[index]
		if plan.direction == cityHouseholdMovementDirectionOutflow {
			sourceID = intPointer(plan.before.cohortID)
		} else if plan.direction == cityHouseholdMovementDirectionInflow {
			targetID = intPointer(plan.before.cohortID)
		} else {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "household_movement_direction"})
		}
	}
	units := input.plans[0].units
	metadata := map[string]any{"schema_version": 1}
	if input.command != nil {
		metadata["command_sequence"] = input.command.Sequence
	}
	if input.reason != "" {
		metadata["reason"] = input.reason
	}
	if input.movementType == CityHouseholdMovementIncomeReclassification {
		metadata["account_control_mode"] = "shared_entity_unchanged"
	}
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("marshal city household movement metadata: %w", err)
	}
	var commandID *int64
	if input.command != nil {
		commandID = &input.command.ID
	}
	var movementID int64
	if err = tx.QueryRowContext(ctx, `
INSERT INTO city_household_movements
    (world_id, tick, sequence, origin, source_command_id, movement_type,
     source_cohort_id, target_cohort_id, child_units, working_units, senior_units,
     employed_units, household_units, occupied_units, expected_line_count, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16::jsonb)
RETURNING id`, input.worldID, input.tick, input.sequence, input.origin,
		cityNullableInt64(commandID), input.movementType, cityNullableInt64(sourceID), cityNullableInt64(targetID),
		units.child, units.working, units.senior, units.employed, units.household,
		units.occupied, len(input.plans), metadataJSON).Scan(&movementID); err != nil {
		return nil, fmt.Errorf("insert city household movement: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
SELECT set_config('sub2api.city_f63_household_movement_id', $1, TRUE)`, cityIntString(movementID)); err != nil {
		return nil, fmt.Errorf("activate city household movement write gate: %w", err)
	}
	for index, plan := range input.plans {
		if err = applyCityHouseholdMovementPlan(ctx, tx, movementID, input.worldID, index+1, plan); err != nil {
			return nil, err
		}
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_household_movements SET posted_at = NOW()
WHERE id = $1 AND posted_at IS NULL`, movementID)
	if err != nil {
		return nil, fmt.Errorf("post city household movement: %w", err)
	}
	if err = cityRowsAffectedExactlyOne(result, "post city household movement"); err != nil {
		return nil, ErrCitySimulationInvariant.WithCause(err)
	}
	return loadCityHouseholdMovementByID(ctx, tx, movementID, true)
}

func applyCityHouseholdMovementPlan(
	ctx context.Context,
	tx *sql.Tx,
	movementID, worldID int64,
	lineNo int,
	plan cityHouseholdMovementPlan,
) error {
	demographicVersionAfter := plan.before.demographicVersion
	if plan.demographicChanged {
		demographicVersionAfter++
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
			return fmt.Errorf("post household movement demographic cohort %d: %w", plan.before.cohortID, err)
		}
		if err = cityRowsAffectedExactlyOne(result, "post household movement demographic cohort"); err != nil {
			return ErrCitySimulationInvariant.WithCause(err)
		}
	}
	beforePopulation, err := cityHouseholdPopulation(plan.before.childUnits, plan.before.workingUnits, plan.before.seniorUnits)
	if err != nil {
		return err
	}
	afterPopulation, err := cityHouseholdPopulation(plan.childAfter, plan.workingAfter, plan.seniorAfter)
	if err != nil {
		return err
	}
	cohortVersionAfter := plan.before.cohortVersion + 1
	result, err := tx.ExecContext(ctx, `
UPDATE city_household_cohorts
SET population_units = $3, working_age_units = $4, employed_units = $5,
    household_units = $6, housing_demand_units = $6,
    version = $7, updated_at = NOW()
WHERE id = $1 AND world_id = $2 AND version = $8
  AND population_units = $9 AND working_age_units = $10
  AND employed_units = $11 AND household_units = $12`,
		plan.before.cohortID, worldID, afterPopulation, plan.workingAfter,
		plan.employedAfter, plan.householdAfter, cohortVersionAfter,
		plan.before.cohortVersion, beforePopulation, plan.before.workingUnits,
		plan.before.employedUnits, plan.before.householdUnits)
	if err != nil {
		return fmt.Errorf("post household movement cohort %d: %w", plan.before.cohortID, err)
	}
	if err = cityRowsAffectedExactlyOne(result, "post household movement cohort"); err != nil {
		return ErrCitySimulationInvariant.WithCause(err)
	}
	occupancyVersionAfter := plan.before.occupancyVersion + 1
	result, err = tx.ExecContext(ctx, `
UPDATE city_housing_occupancies
SET occupied_units = $3, unmet_units = $4, version = $5, updated_at = NOW()
WHERE id = $1 AND world_id = $2 AND version = $6
  AND occupied_units = $7 AND unmet_units = $8`,
		plan.before.occupancyID, worldID, plan.occupiedAfter, plan.unmetAfter,
		occupancyVersionAfter, plan.before.occupancyVersion,
		plan.before.occupiedUnits, plan.before.unmetUnits)
	if err != nil {
		return fmt.Errorf("post household movement occupancy %d: %w", plan.before.cohortID, err)
	}
	if err = cityRowsAffectedExactlyOne(result, "post household movement occupancy"); err != nil {
		return ErrCitySimulationInvariant.WithCause(err)
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO city_household_movement_lines
    (movement_id, world_id, line_no, cohort_id, direction,
     demographic_version_before, demographic_version_after,
     cohort_version_before, cohort_version_after,
     occupancy_version_before, occupancy_version_after,
     child_units, working_units, senior_units, employed_units, household_units, occupied_units,
     child_units_before, working_units_before, senior_units_before, employed_units_before,
     household_units_before, occupied_units_before, unmet_units_before,
     child_units_after, working_units_after, senior_units_after, employed_units_after,
     household_units_after, occupied_units_after, unmet_units_after)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11,
        $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24,
        $25, $26, $27, $28, $29, $30, $31)`,
		movementID, worldID, lineNo, plan.before.cohortID, plan.direction,
		plan.before.demographicVersion, demographicVersionAfter,
		plan.before.cohortVersion, cohortVersionAfter,
		plan.before.occupancyVersion, occupancyVersionAfter,
		plan.units.child, plan.units.working, plan.units.senior, plan.units.employed,
		plan.units.household, plan.units.occupied,
		plan.before.childUnits, plan.before.workingUnits, plan.before.seniorUnits,
		plan.before.employedUnits, plan.before.householdUnits,
		plan.before.occupiedUnits, plan.before.unmetUnits,
		plan.childAfter, plan.workingAfter, plan.seniorAfter, plan.employedAfter,
		plan.householdAfter, plan.occupiedAfter, plan.unmetAfter)
	if err != nil {
		return fmt.Errorf("insert city household movement line %d: %w", lineNo, err)
	}
	return nil
}

func loadCityHouseholdCohortRefsForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
) ([]cityHouseholdCohortRef, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT demographic.id, cohort.id, occupancy.id, district.code, entity.code, entity.id,
       cohort.income_band, demographic.child_units, demographic.working_units,
       demographic.senior_units, cohort.employed_units, cohort.household_units,
       occupancy.occupied_units, occupancy.unmet_units,
       demographic.version, cohort.version, occupancy.version
FROM city_household_cohorts cohort
JOIN city_demographic_cohort_states demographic
  ON demographic.world_id = cohort.world_id AND demographic.cohort_id = cohort.id
JOIN city_housing_occupancies occupancy
  ON occupancy.world_id = cohort.world_id AND occupancy.cohort_id = cohort.id
JOIN city_districts district ON district.id = cohort.district_id
JOIN city_economic_entities entity ON entity.id = cohort.entity_id
WHERE cohort.world_id = $1
ORDER BY district.sort_order ASC,
         CASE cohort.income_band WHEN 'low' THEN 1 WHEN 'middle' THEN 2 ELSE 3 END
FOR UPDATE OF demographic, cohort, occupancy`, worldID)
	if err != nil {
		return nil, fmt.Errorf("lock city household lifecycle cohorts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]cityHouseholdCohortRef, 0)
	for rows.Next() {
		var item cityHouseholdCohortRef
		if err = rows.Scan(
			&item.demographicID, &item.cohortID, &item.occupancyID,
			&item.districtCode, &item.entityCode, &item.entityID, &item.incomeBand,
			&item.childUnits, &item.workingUnits, &item.seniorUnits,
			&item.employedUnits, &item.householdUnits, &item.occupiedUnits,
			&item.unmetUnits, &item.demographicVersion, &item.cohortVersion,
			&item.occupancyVersion,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate locked city household lifecycle cohorts: %w", err)
	}
	return items, nil
}

func reconcileCityHouseholdsAfterDemography(
	ctx context.Context,
	tx *sql.Tx,
	worldID, tick, firstSequence int64,
) ([]*CityHouseholdMovement, []cityDemographyEvent, int64, error) {
	refs, err := loadCityHouseholdCohortRefsForUpdate(ctx, tx, worldID)
	if err != nil {
		return nil, nil, firstSequence, err
	}
	movements := make([]*CityHouseholdMovement, 0)
	events := make([]cityDemographyEvent, 0)
	nextSequence := firstSequence
	for _, ref := range refs {
		population, populationErr := cityHouseholdPopulation(ref.childUnits, ref.workingUnits, ref.seniorUnits)
		if populationErr != nil {
			return nil, nil, nextSequence, populationErr
		}
		current := ref
		remaining := ref.householdUnits - population
		for remaining > 0 {
			units := min(remaining, cityHouseholdMovementMaximumUnits)
			plan, planErr := calculateCityHouseholdCountPlan(
				current, CityHouseholdMovementDissolution, units,
			)
			if planErr != nil {
				return nil, nil, nextSequence, planErr
			}
			movement, postErr := postCityHouseholdMovement(ctx, tx, cityHouseholdMovementPost{
				worldID: worldID, tick: tick, sequence: nextSequence,
				origin:       CityHouseholdMovementOriginDemographyGuard,
				movementType: CityHouseholdMovementDissolution,
				plans:        []cityHouseholdMovementPlan{plan}, reason: "population_floor_reconciliation",
			})
			if postErr != nil {
				return nil, nil, nextSequence, postErr
			}
			movements = append(movements, movement)
			events = append(events, cityDemographyEvent{
				eventType: "city.household.demography_guard_dissolution",
				payload:   cityHouseholdMovementEventPayload(movement),
			})
			current.householdUnits = plan.householdAfter
			current.occupiedUnits = plan.occupiedAfter
			current.unmetUnits = plan.unmetAfter
			current.cohortVersion++
			current.occupancyVersion++
			remaining -= units
			nextSequence++
		}
	}
	return movements, events, nextSequence, nil
}

func cityHouseholdMovementEventPayload(movement *CityHouseholdMovement) map[string]any {
	payload := map[string]any{
		"movement_tick": movement.Tick, "movement_sequence": movement.Sequence,
		"movement_type": movement.MovementType, "origin": movement.Origin,
		"child_units": movement.ChildUnits, "working_units": movement.WorkingUnits,
		"senior_units": movement.SeniorUnits, "employed_units": movement.EmployedUnits,
		"household_units": movement.HouseholdUnits, "occupied_units": movement.OccupiedUnits,
	}
	if movement.SourceDistrictCode != nil {
		payload["source_district_code"] = *movement.SourceDistrictCode
		payload["source_income_band"] = *movement.SourceIncomeBand
	}
	if movement.TargetDistrictCode != nil {
		payload["target_district_code"] = *movement.TargetDistrictCode
		payload["target_income_band"] = *movement.TargetIncomeBand
	}
	return payload
}

const cityHouseholdMovementColumns = `
movement.id, movement.world_id, movement.tick, movement.sequence, movement.origin,
movement.source_command_id, movement.movement_type,
movement.source_cohort_id, source_district.code, source_entity.code, source.income_band,
movement.target_cohort_id, target_district.code, target_entity.code, target.income_band,
movement.child_units, movement.working_units, movement.senior_units,
movement.employed_units, movement.household_units, movement.occupied_units,
movement.expected_line_count, movement.metadata, movement.posted_at, movement.created_at`

const cityHouseholdMovementJoins = `
LEFT JOIN city_household_cohorts source ON source.id = movement.source_cohort_id
LEFT JOIN city_districts source_district ON source_district.id = source.district_id
LEFT JOIN city_economic_entities source_entity ON source_entity.id = source.entity_id
LEFT JOIN city_household_cohorts target ON target.id = movement.target_cohort_id
LEFT JOIN city_districts target_district ON target_district.id = target.district_id
LEFT JOIN city_economic_entities target_entity ON target_entity.id = target.entity_id`

func (s *CityEconomyService) ListHouseholdMovements(
	ctx context.Context,
	input CityHouseholdMovementListInput,
) (*CityHouseholdMovementPage, error) {
	if input.UserID <= 0 || input.WorldID <= 0 || input.AfterTick < 0 || input.AfterSequence < 0 {
		return nil, ErrCityInvalidInput
	}
	if input.Limit <= 0 {
		input.Limit = cityHouseholdMovementDefaultLimit
	}
	if input.Limit > cityHouseholdMovementMaximumLimit {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, input.UserID, input.WorldID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT `+cityHouseholdMovementColumns+`
FROM city_household_movements movement
`+cityHouseholdMovementJoins+`
WHERE movement.world_id = $1 AND movement.posted_at IS NOT NULL
  AND (movement.tick > $2 OR (movement.tick = $2 AND movement.sequence > $3))
ORDER BY movement.tick ASC, movement.sequence ASC
LIMIT $4`, input.WorldID, input.AfterTick, input.AfterSequence, input.Limit+1)
	if err != nil {
		return nil, fmt.Errorf("list city household movements: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]*CityHouseholdMovement, 0, input.Limit)
	for rows.Next() {
		item, scanErr := scanCityHouseholdMovement(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate city household movements: %w", err)
	}
	page := &CityHouseholdMovementPage{Items: items}
	if len(items) > input.Limit {
		items = items[:input.Limit]
		page.Items = items
		last := items[len(items)-1]
		page.NextCursor = &CityHouseholdMovementCursor{Tick: last.Tick, Sequence: last.Sequence}
	}
	return page, nil
}

func (s *CityEconomyService) GetHouseholdMovement(
	ctx context.Context,
	userID, worldID, tick, sequence int64,
) (*CityHouseholdMovement, error) {
	if userID <= 0 || worldID <= 0 || tick <= 0 || sequence <= 0 {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, userID, worldID); err != nil {
		return nil, err
	}
	item, err := loadCityHouseholdMovementByCursor(ctx, s.db, worldID, tick, sequence, true)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCityHouseholdMovementNotFound
	}
	return item, err
}

func loadCityHouseholdMovementByID(
	ctx context.Context,
	queryer citySQLQueryer,
	movementID int64,
	withLines bool,
) (*CityHouseholdMovement, error) {
	item, err := scanCityHouseholdMovement(queryer.QueryRowContext(ctx, `
SELECT `+cityHouseholdMovementColumns+`
FROM city_household_movements movement
`+cityHouseholdMovementJoins+`
WHERE movement.id = $1 AND movement.posted_at IS NOT NULL`, movementID))
	if err != nil {
		return nil, err
	}
	if withLines {
		item.Lines, err = loadCityHouseholdMovementLines(ctx, queryer, item.ID)
	}
	return item, err
}

func loadCityHouseholdMovementByCursor(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, tick, sequence int64,
	withLines bool,
) (*CityHouseholdMovement, error) {
	item, err := scanCityHouseholdMovement(queryer.QueryRowContext(ctx, `
SELECT `+cityHouseholdMovementColumns+`
FROM city_household_movements movement
`+cityHouseholdMovementJoins+`
WHERE movement.world_id = $1 AND movement.tick = $2 AND movement.sequence = $3
  AND movement.posted_at IS NOT NULL`, worldID, tick, sequence))
	if err != nil {
		return nil, err
	}
	if withLines {
		item.Lines, err = loadCityHouseholdMovementLines(ctx, queryer, item.ID)
	}
	return item, err
}

func loadCityHouseholdMovementsForTick(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, tick int64,
) ([]*CityHouseholdMovement, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT `+cityHouseholdMovementColumns+`
FROM city_household_movements movement
`+cityHouseholdMovementJoins+`
WHERE movement.world_id = $1 AND movement.tick = $2 AND movement.posted_at IS NOT NULL
ORDER BY movement.sequence ASC`, worldID, tick)
	if err != nil {
		return nil, fmt.Errorf("load city household movements for tick: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]*CityHouseholdMovement, 0)
	for rows.Next() {
		item, scanErr := scanCityHouseholdMovement(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate city household movements for tick: %w", err)
	}
	for _, item := range items {
		item.Lines, err = loadCityHouseholdMovementLines(ctx, queryer, item.ID)
		if err != nil {
			return nil, err
		}
	}
	return items, nil
}

func scanCityHouseholdMovement(row cityScannable) (*CityHouseholdMovement, error) {
	item := &CityHouseholdMovement{}
	var sourceCommandID, sourceCohortID, targetCohortID sql.NullInt64
	var sourceDistrict, sourceEntity, sourceBand sql.NullString
	var targetDistrict, targetEntity, targetBand sql.NullString
	var metadata []byte
	var postedAt sql.NullTime
	if err := row.Scan(
		&item.ID, &item.WorldID, &item.Tick, &item.Sequence, &item.Origin,
		&sourceCommandID, &item.MovementType,
		&sourceCohortID, &sourceDistrict, &sourceEntity, &sourceBand,
		&targetCohortID, &targetDistrict, &targetEntity, &targetBand,
		&item.ChildUnits, &item.WorkingUnits, &item.SeniorUnits,
		&item.EmployedUnits, &item.HouseholdUnits, &item.OccupiedUnits,
		&item.ExpectedLineCount, &metadata, &postedAt, &item.CreatedAt,
	); err != nil {
		return nil, err
	}
	item.SourceCommandID = nullInt64Pointer(sourceCommandID)
	item.SourceCohortID = nullInt64Pointer(sourceCohortID)
	item.SourceDistrictCode = nullStringPointer(sourceDistrict)
	item.SourceEntityCode = nullStringPointer(sourceEntity)
	item.SourceIncomeBand = nullStringPointer(sourceBand)
	item.TargetCohortID = nullInt64Pointer(targetCohortID)
	item.TargetDistrictCode = nullStringPointer(targetDistrict)
	item.TargetEntityCode = nullStringPointer(targetEntity)
	item.TargetIncomeBand = nullStringPointer(targetBand)
	item.PostedAt = nullTimePointer(postedAt)
	var err error
	item.Metadata, err = decodeCityJSONMap(metadata)
	if err != nil {
		return nil, fmt.Errorf("decode city household movement metadata: %w", err)
	}
	return item, nil
}

func loadCityHouseholdMovementLines(
	ctx context.Context,
	queryer citySQLQueryer,
	movementID int64,
) ([]*CityHouseholdMovementLine, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT line.id, line.movement_id, line.world_id, line.line_no, line.cohort_id,
       district.code, entity.code, cohort.income_band, line.direction,
       line.demographic_version_before, line.demographic_version_after,
       line.cohort_version_before, line.cohort_version_after,
       line.occupancy_version_before, line.occupancy_version_after,
       line.child_units, line.working_units, line.senior_units,
       line.employed_units, line.household_units, line.occupied_units,
       line.child_units_before, line.working_units_before, line.senior_units_before,
       line.employed_units_before, line.household_units_before,
       line.occupied_units_before, line.unmet_units_before,
       line.child_units_after, line.working_units_after, line.senior_units_after,
       line.employed_units_after, line.household_units_after,
       line.occupied_units_after, line.unmet_units_after, line.created_at
FROM city_household_movement_lines line
JOIN city_household_cohorts cohort ON cohort.id = line.cohort_id
JOIN city_districts district ON district.id = cohort.district_id
JOIN city_economic_entities entity ON entity.id = cohort.entity_id
WHERE line.movement_id = $1 ORDER BY line.line_no ASC`, movementID)
	if err != nil {
		return nil, fmt.Errorf("load city household movement lines: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]*CityHouseholdMovementLine, 0, 2)
	for rows.Next() {
		item := &CityHouseholdMovementLine{}
		if err = rows.Scan(
			&item.ID, &item.MovementID, &item.WorldID, &item.LineNo, &item.CohortID,
			&item.DistrictCode, &item.EntityCode, &item.IncomeBand, &item.Direction,
			&item.DemographicVersionBefore, &item.DemographicVersionAfter,
			&item.CohortVersionBefore, &item.CohortVersionAfter,
			&item.OccupancyVersionBefore, &item.OccupancyVersionAfter,
			&item.ChildUnits, &item.WorkingUnits, &item.SeniorUnits,
			&item.EmployedUnits, &item.HouseholdUnits, &item.OccupiedUnits,
			&item.ChildUnitsBefore, &item.WorkingUnitsBefore, &item.SeniorUnitsBefore,
			&item.EmployedUnitsBefore, &item.HouseholdUnitsBefore,
			&item.OccupiedUnitsBefore, &item.UnmetUnitsBefore,
			&item.ChildUnitsAfter, &item.WorkingUnitsAfter, &item.SeniorUnitsAfter,
			&item.EmployedUnitsAfter, &item.HouseholdUnitsAfter,
			&item.OccupiedUnitsAfter, &item.UnmetUnitsAfter, &item.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate city household movement lines: %w", err)
	}
	return items, nil
}

func replayCityHouseholdMovement(movement *CityHouseholdMovement, state *cityHashState) error {
	if movement == nil || movement.PostedAt == nil || movement.ExpectedLineCount != len(movement.Lines) ||
		movement.HouseholdUnits <= 0 {
		return fmt.Errorf("household movement header is invalid")
	}
	demographicIndex := make(map[string]int, len(state.Demography.Cohorts))
	for index, cohort := range state.Demography.Cohorts {
		demographicIndex[cityDemographyCohortKey(cohort.DistrictCode, cohort.EntityCode, cohort.IncomeBand)] = index
	}
	physicalIndex := make(map[string]int, len(state.Physical.HouseholdCohorts))
	for index, cohort := range state.Physical.HouseholdCohorts {
		physicalIndex[cityDemographyCohortKey(cohort.DistrictCode, cohort.EntityCode, cohort.IncomeBand)] = index
	}
	occupancyIndex := make(map[string]int, len(state.Markets.Occupancies))
	for index, occupancy := range state.Markets.Occupancies {
		occupancyIndex[occupancy.DistrictCode+"\x00"+occupancy.IncomeBand] = index
	}
	seen := make(map[string]struct{}, len(movement.Lines))
	for expectedLine, line := range movement.Lines {
		if line == nil || line.LineNo != expectedLine+1 || line.MovementID != movement.ID ||
			line.WorldID != movement.WorldID || line.ChildUnits != movement.ChildUnits ||
			line.WorkingUnits != movement.WorkingUnits || line.SeniorUnits != movement.SeniorUnits ||
			line.EmployedUnits != movement.EmployedUnits || line.HouseholdUnits != movement.HouseholdUnits ||
			line.OccupiedUnits != movement.OccupiedUnits {
			return fmt.Errorf("household movement line summary is invalid")
		}
		key := cityDemographyCohortKey(line.DistrictCode, line.EntityCode, line.IncomeBand)
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf("household movement contains duplicate cohort")
		}
		seen[key] = struct{}{}
		demographicPosition, demographicFound := demographicIndex[key]
		physicalPosition, physicalFound := physicalIndex[key]
		occupancyPosition, occupancyFound := occupancyIndex[line.DistrictCode+"\x00"+line.IncomeBand]
		if !demographicFound || !physicalFound || !occupancyFound {
			return fmt.Errorf("household movement references unknown projection")
		}
		demographic := &state.Demography.Cohorts[demographicPosition]
		physical := &state.Physical.HouseholdCohorts[physicalPosition]
		occupancy := &state.Markets.Occupancies[occupancyPosition]
		beforePopulation, populationErr := cityHouseholdPopulation(
			line.ChildUnitsBefore, line.WorkingUnitsBefore, line.SeniorUnitsBefore,
		)
		if populationErr != nil {
			return populationErr
		}
		if demographic.Version != line.DemographicVersionBefore ||
			demographic.ChildUnits != line.ChildUnitsBefore ||
			demographic.WorkingUnits != line.WorkingUnitsBefore ||
			demographic.SeniorUnits != line.SeniorUnitsBefore ||
			physical.Version != line.CohortVersionBefore ||
			physical.PopulationUnits != beforePopulation ||
			physical.WorkingAgeUnits != line.WorkingUnitsBefore ||
			physical.EmployedUnits != line.EmployedUnitsBefore ||
			physical.HouseholdUnits != line.HouseholdUnitsBefore ||
			occupancy.Version != line.OccupancyVersionBefore ||
			occupancy.OccupiedUnits != line.OccupiedUnitsBefore ||
			occupancy.UnmetUnits != line.UnmetUnitsBefore {
			return fmt.Errorf("household movement projection chain is broken")
		}
		if err := validateCityHouseholdMovementLine(movement.MovementType, line); err != nil {
			return err
		}
		demographic.ChildUnits = line.ChildUnitsAfter
		demographic.WorkingUnits = line.WorkingUnitsAfter
		demographic.SeniorUnits = line.SeniorUnitsAfter
		demographic.Version = line.DemographicVersionAfter
		afterPopulation, populationErr := cityHouseholdPopulation(
			line.ChildUnitsAfter, line.WorkingUnitsAfter, line.SeniorUnitsAfter,
		)
		if populationErr != nil {
			return populationErr
		}
		physical.PopulationUnits = afterPopulation
		physical.WorkingAgeUnits = line.WorkingUnitsAfter
		physical.EmployedUnits = line.EmployedUnitsAfter
		physical.HouseholdUnits = line.HouseholdUnitsAfter
		physical.HousingDemandUnits = line.HouseholdUnitsAfter
		physical.Version = line.CohortVersionAfter
		occupancy.OccupiedUnits = line.OccupiedUnitsAfter
		occupancy.UnmetUnits = line.UnmetUnitsAfter
		occupancy.Version = line.OccupancyVersionAfter
	}
	return validateCityHouseholdMovementShape(movement)
}

func validateCityHouseholdMovementLine(movementType string, line *CityHouseholdMovementLine) error {
	if line.CohortVersionAfter != line.CohortVersionBefore+1 ||
		line.OccupancyVersionAfter != line.OccupancyVersionBefore+1 ||
		line.HouseholdUnitsAfter <= 0 || line.OccupiedUnitsAfter < 0 ||
		line.OccupiedUnitsAfter+line.UnmetUnitsAfter != line.HouseholdUnitsAfter ||
		line.EmployedUnitsAfter > line.WorkingUnitsAfter {
		return fmt.Errorf("household movement line version or invariant is invalid")
	}
	if movementType == CityHouseholdMovementIncomeReclassification {
		if line.DemographicVersionAfter != line.DemographicVersionBefore+1 {
			return fmt.Errorf("household reclassification demographic version is invalid")
		}
	} else if line.DemographicVersionAfter != line.DemographicVersionBefore ||
		line.ChildUnitsAfter != line.ChildUnitsBefore ||
		line.WorkingUnitsAfter != line.WorkingUnitsBefore ||
		line.SeniorUnitsAfter != line.SeniorUnitsBefore ||
		line.EmployedUnitsAfter != line.EmployedUnitsBefore {
		return fmt.Errorf("household count movement changed population or employment")
	}
	before := []int64{
		line.ChildUnitsBefore, line.WorkingUnitsBefore, line.SeniorUnitsBefore,
		line.EmployedUnitsBefore, line.HouseholdUnitsBefore, line.OccupiedUnitsBefore,
	}
	after := []int64{
		line.ChildUnitsAfter, line.WorkingUnitsAfter, line.SeniorUnitsAfter,
		line.EmployedUnitsAfter, line.HouseholdUnitsAfter, line.OccupiedUnitsAfter,
	}
	units := []int64{line.ChildUnits, line.WorkingUnits, line.SeniorUnits, line.EmployedUnits, line.HouseholdUnits, line.OccupiedUnits}
	for index := range before {
		expected, err := cityHouseholdDirectionalAfter(before[index], units[index], line.Direction)
		if err != nil || expected != after[index] {
			return fmt.Errorf("household movement line equation is invalid")
		}
	}
	return nil
}

func cityHouseholdDirectionalAfter(before, units int64, direction string) (int64, error) {
	switch direction {
	case cityHouseholdMovementDirectionInflow:
		return addCityLedgerUnits(before, units)
	case cityHouseholdMovementDirectionOutflow:
		if units > before {
			return 0, fmt.Errorf("household movement outflow exceeds projection")
		}
		return before - units, nil
	default:
		return 0, fmt.Errorf("household movement direction is invalid")
	}
}

func validateCityHouseholdMovementShape(movement *CityHouseholdMovement) error {
	if movement.MovementType == CityHouseholdMovementIncomeReclassification {
		if len(movement.Lines) != 2 || movement.SourceDistrictCode == nil || movement.TargetDistrictCode == nil ||
			movement.SourceEntityCode == nil || movement.TargetEntityCode == nil ||
			movement.SourceIncomeBand == nil || movement.TargetIncomeBand == nil ||
			*movement.SourceDistrictCode != *movement.TargetDistrictCode ||
			*movement.SourceEntityCode != *movement.TargetEntityCode ||
			absInt(cityIncomeBandRank(*movement.SourceIncomeBand)-cityIncomeBandRank(*movement.TargetIncomeBand)) != 1 ||
			movement.Lines[0].Direction != cityHouseholdMovementDirectionOutflow ||
			movement.Lines[1].Direction != cityHouseholdMovementDirectionInflow ||
			movement.Lines[0].DistrictCode != *movement.SourceDistrictCode ||
			movement.Lines[0].EntityCode != *movement.SourceEntityCode ||
			movement.Lines[0].IncomeBand != *movement.SourceIncomeBand ||
			movement.Lines[1].DistrictCode != *movement.TargetDistrictCode ||
			movement.Lines[1].EntityCode != *movement.TargetEntityCode ||
			movement.Lines[1].IncomeBand != *movement.TargetIncomeBand {
			return fmt.Errorf("household reclassification shape is invalid")
		}
		return nil
	}
	if len(movement.Lines) != 1 {
		return fmt.Errorf("household count movement shape is invalid")
	}
	expectedDirection := cityHouseholdMovementDirectionInflow
	if movement.MovementType == CityHouseholdMovementMerge || movement.MovementType == CityHouseholdMovementDissolution {
		expectedDirection = cityHouseholdMovementDirectionOutflow
	}
	if movement.Lines[0].Direction != expectedDirection {
		return fmt.Errorf("household count movement direction is invalid")
	}
	if expectedDirection == cityHouseholdMovementDirectionInflow {
		if movement.TargetDistrictCode == nil || movement.TargetEntityCode == nil || movement.TargetIncomeBand == nil ||
			movement.Lines[0].DistrictCode != *movement.TargetDistrictCode ||
			movement.Lines[0].EntityCode != *movement.TargetEntityCode ||
			movement.Lines[0].IncomeBand != *movement.TargetIncomeBand {
			return fmt.Errorf("household count movement target is invalid")
		}
	} else if movement.SourceDistrictCode == nil || movement.SourceEntityCode == nil || movement.SourceIncomeBand == nil ||
		movement.Lines[0].DistrictCode != *movement.SourceDistrictCode ||
		movement.Lines[0].EntityCode != *movement.SourceEntityCode ||
		movement.Lines[0].IncomeBand != *movement.SourceIncomeBand {
		return fmt.Errorf("household count movement source is invalid")
	}
	return nil
}

func validateCityHouseholdHashProjection(state *cityHashState) error {
	occupancies := make(map[string]cityMarketHashOccupancy, len(state.Markets.Occupancies))
	for _, occupancy := range state.Markets.Occupancies {
		occupancies[occupancy.DistrictCode+"\x00"+occupancy.IncomeBand] = occupancy
	}
	for _, cohort := range state.Physical.HouseholdCohorts {
		occupancy, ok := occupancies[cohort.DistrictCode+"\x00"+cohort.IncomeBand]
		if !ok || cohort.HouseholdUnits <= 0 || cohort.HouseholdUnits > cohort.PopulationUnits ||
			cohort.HousingDemandUnits != cohort.HouseholdUnits ||
			occupancy.OccupiedUnits < 0 || occupancy.OccupiedUnits > cohort.HouseholdUnits ||
			occupancy.UnmetUnits != cohort.HouseholdUnits-occupancy.OccupiedUnits {
			return fmt.Errorf("household canonical projection is inconsistent")
		}
	}
	return nil
}
