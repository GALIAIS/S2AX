package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	CityPopulationMovementNaturalChange = "natural_change"
	cityDemographicRateScale            = int64(1_000_000)
	cityDemographicPeriodsPerYear       = 12
	cityPopulationMovementDefaultLimit  = 50
	cityPopulationMovementMaximumLimit  = 200
)

var ErrCityPopulationMovementNotFound = infraerrors.NotFound(
	"CITY_POPULATION_MOVEMENT_NOT_FOUND", "city population movement not found",
)

type CityCalendarState struct {
	WorldID           int64          `json:"world_id"`
	LocalDate         string         `json:"local_date"`
	DayIndex          int64          `json:"day_index"`
	MonthIndex        int64          `json:"month_index"`
	QuarterIndex      int64          `json:"quarter_index"`
	YearIndex         int64          `json:"year_index"`
	LastDailyTick     *int64         `json:"last_daily_tick,omitempty"`
	LastMonthlyTick   *int64         `json:"last_monthly_tick,omitempty"`
	LastQuarterlyTick *int64         `json:"last_quarterly_tick,omitempty"`
	LastAnnualTick    *int64         `json:"last_annual_tick,omitempty"`
	Version           int64          `json:"version"`
	Metadata          map[string]any `json:"metadata"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
}

type CityDemographicPolicy struct {
	WorldID                int64          `json:"world_id"`
	ParameterSetCode       string         `json:"parameter_set_code"`
	ParameterVersion       int            `json:"parameter_version"`
	PeriodsPerYear         int            `json:"periods_per_year"`
	BirthRatePPM           int            `json:"birth_rate_ppm"`
	ChildDeathRatePPM      int            `json:"child_death_rate_ppm"`
	WorkingDeathRatePPM    int            `json:"working_death_rate_ppm"`
	SeniorDeathRatePPM     int            `json:"senior_death_rate_ppm"`
	ChildToWorkingRatePPM  int            `json:"child_to_working_rate_ppm"`
	WorkingToSeniorRatePPM int            `json:"working_to_senior_rate_ppm"`
	Version                int64          `json:"version"`
	Metadata               map[string]any `json:"metadata"`
	CreatedAt              time.Time      `json:"created_at"`
	UpdatedAt              time.Time      `json:"updated_at"`
}

type CityDemographicCohort struct {
	ID                    int64          `json:"id"`
	WorldID               int64          `json:"world_id"`
	CohortID              int64          `json:"cohort_id"`
	DistrictCode          string         `json:"district_code"`
	EntityCode            string         `json:"entity_code"`
	IncomeBand            string         `json:"income_band"`
	ChildUnits            int64          `json:"child_units"`
	WorkingUnits          int64          `json:"working_units"`
	SeniorUnits           int64          `json:"senior_units"`
	BirthRemainder        int64          `json:"birth_remainder"`
	ChildDeathRemainder   int64          `json:"child_death_remainder"`
	WorkingDeathRemainder int64          `json:"working_death_remainder"`
	SeniorDeathRemainder  int64          `json:"senior_death_remainder"`
	ChildAgingRemainder   int64          `json:"child_aging_remainder"`
	WorkingAgingRemainder int64          `json:"working_aging_remainder"`
	Version               int64          `json:"version"`
	Metadata              map[string]any `json:"metadata"`
	CreatedAt             time.Time      `json:"created_at"`
	UpdatedAt             time.Time      `json:"updated_at"`
}

type CityDemographyState struct {
	Calendar *CityCalendarState       `json:"calendar"`
	Policy   *CityDemographicPolicy   `json:"policy"`
	Cohorts  []*CityDemographicCohort `json:"cohorts"`
}

type CityPopulationState struct {
	Policy  *CityDemographicPolicy   `json:"policy"`
	Cohorts []*CityDemographicCohort `json:"cohorts"`
}

type CityCalendarBoundary struct {
	ID                int64     `json:"id"`
	WorldID           int64     `json:"world_id"`
	Tick              int64     `json:"tick"`
	Sequence          int       `json:"sequence"`
	BoundaryType      string    `json:"boundary_type"`
	PreviousLocalDate string    `json:"previous_local_date"`
	LocalDate         string    `json:"local_date"`
	PeriodIndex       int64     `json:"period_index"`
	CreatedAt         time.Time `json:"created_at"`
}

type CityPopulationMovementLine struct {
	ID                          int64     `json:"id"`
	MovementID                  int64     `json:"movement_id"`
	WorldID                     int64     `json:"world_id"`
	LineNo                      int       `json:"line_no"`
	CohortID                    int64     `json:"cohort_id"`
	DistrictCode                string    `json:"district_code"`
	EntityCode                  string    `json:"entity_code"`
	IncomeBand                  string    `json:"income_band"`
	DemographicVersionBefore    int64     `json:"demographic_version_before"`
	DemographicVersionAfter     int64     `json:"demographic_version_after"`
	CohortVersionBefore         int64     `json:"cohort_version_before"`
	CohortVersionAfter          int64     `json:"cohort_version_after"`
	BirthUnits                  int64     `json:"birth_units"`
	ChildDeathUnits             int64     `json:"child_death_units"`
	WorkingDeathUnits           int64     `json:"working_death_units"`
	SeniorDeathUnits            int64     `json:"senior_death_units"`
	ChildToWorkingUnits         int64     `json:"child_to_working_units"`
	WorkingToSeniorUnits        int64     `json:"working_to_senior_units"`
	ChildUnitsBefore            int64     `json:"child_units_before"`
	WorkingUnitsBefore          int64     `json:"working_units_before"`
	SeniorUnitsBefore           int64     `json:"senior_units_before"`
	ChildUnitsAfter             int64     `json:"child_units_after"`
	WorkingUnitsAfter           int64     `json:"working_units_after"`
	SeniorUnitsAfter            int64     `json:"senior_units_after"`
	BirthRemainderBefore        int64     `json:"birth_remainder_before"`
	ChildDeathRemainderBefore   int64     `json:"child_death_remainder_before"`
	WorkingDeathRemainderBefore int64     `json:"working_death_remainder_before"`
	SeniorDeathRemainderBefore  int64     `json:"senior_death_remainder_before"`
	ChildAgingRemainderBefore   int64     `json:"child_aging_remainder_before"`
	WorkingAgingRemainderBefore int64     `json:"working_aging_remainder_before"`
	BirthRemainderAfter         int64     `json:"birth_remainder_after"`
	ChildDeathRemainderAfter    int64     `json:"child_death_remainder_after"`
	WorkingDeathRemainderAfter  int64     `json:"working_death_remainder_after"`
	SeniorDeathRemainderAfter   int64     `json:"senior_death_remainder_after"`
	ChildAgingRemainderAfter    int64     `json:"child_aging_remainder_after"`
	WorkingAgingRemainderAfter  int64     `json:"working_aging_remainder_after"`
	CreatedAt                   time.Time `json:"created_at"`
}

type CityPopulationMovement struct {
	ID                   int64                         `json:"id"`
	WorldID              int64                         `json:"world_id"`
	Tick                 int64                         `json:"tick"`
	Sequence             int                           `json:"sequence"`
	BoundaryID           int64                         `json:"boundary_id"`
	MovementType         string                        `json:"movement_type"`
	LocalMonth           string                        `json:"local_month"`
	ParameterSetCode     string                        `json:"parameter_set_code"`
	ParameterVersion     int                           `json:"parameter_version"`
	ExpectedLineCount    int                           `json:"expected_line_count"`
	TotalBirthUnits      int64                         `json:"total_birth_units"`
	TotalDeathUnits      int64                         `json:"total_death_units"`
	TotalTransitionUnits int64                         `json:"total_transition_units"`
	Metadata             map[string]any                `json:"metadata"`
	PostedAt             *time.Time                    `json:"posted_at,omitempty"`
	CreatedAt            time.Time                     `json:"created_at"`
	Lines                []*CityPopulationMovementLine `json:"lines,omitempty"`
}

type CityPopulationMovementCursor struct {
	Tick     int64 `json:"tick"`
	Sequence int   `json:"sequence"`
}

type CityPopulationMovementPage struct {
	Items      []*CityPopulationMovement     `json:"items"`
	NextCursor *CityPopulationMovementCursor `json:"next_cursor,omitempty"`
}

type CityPopulationMovementListInput struct {
	UserID        int64
	WorldID       int64
	AfterTick     int64
	AfterSequence int
	Limit         int
}

const cityPopulationMovementColumns = `
m.id, m.world_id, m.tick, m.sequence, m.boundary_id, m.movement_type,
m.local_month, m.parameter_set_code, m.parameter_version, m.expected_line_count,
m.total_birth_units, m.total_death_units, m.total_transition_units,
m.metadata, m.posted_at, m.created_at`

func (s *CityEconomyService) GetDemographyState(ctx context.Context, userID, worldID int64) (*CityDemographyState, error) {
	if userID <= 0 || worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, userID, worldID); err != nil {
		return nil, err
	}
	return loadCityDemographyState(ctx, s.db, worldID)
}

func (s *CityEconomyService) GetCalendarState(ctx context.Context, userID, worldID int64) (*CityCalendarState, error) {
	state, err := s.GetDemographyState(ctx, userID, worldID)
	if err != nil {
		return nil, err
	}
	return state.Calendar, nil
}

func (s *CityEconomyService) GetPopulationState(ctx context.Context, userID, worldID int64) (*CityPopulationState, error) {
	state, err := s.GetDemographyState(ctx, userID, worldID)
	if err != nil {
		return nil, err
	}
	return &CityPopulationState{Policy: state.Policy, Cohorts: state.Cohorts}, nil
}

func loadCityDemographyState(ctx context.Context, queryer citySQLQueryer, worldID int64) (*CityDemographyState, error) {
	state := &CityDemographyState{Calendar: &CityCalendarState{}, Policy: &CityDemographicPolicy{}, Cohorts: make([]*CityDemographicCohort, 0)}
	var localDate time.Time
	var dailyTick, monthlyTick, quarterlyTick, annualTick sql.NullInt64
	var metadata []byte
	if err := queryer.QueryRowContext(ctx, `
SELECT world_id, local_date, day_index, month_index, quarter_index, year_index,
       last_daily_tick, last_monthly_tick, last_quarterly_tick, last_annual_tick, version,
       metadata, created_at, updated_at
FROM city_calendar_states WHERE world_id = $1`, worldID).Scan(
		&state.Calendar.WorldID, &localDate, &state.Calendar.DayIndex,
		&state.Calendar.MonthIndex, &state.Calendar.QuarterIndex,
		&state.Calendar.YearIndex, &dailyTick, &monthlyTick, &quarterlyTick,
		&annualTick, &state.Calendar.Version, &metadata,
		&state.Calendar.CreatedAt, &state.Calendar.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("load city calendar state: %w", err)
	}
	state.Calendar.LocalDate = localDate.Format(time.DateOnly)
	state.Calendar.LastDailyTick = nullInt64Pointer(dailyTick)
	state.Calendar.LastMonthlyTick = nullInt64Pointer(monthlyTick)
	state.Calendar.LastQuarterlyTick = nullInt64Pointer(quarterlyTick)
	state.Calendar.LastAnnualTick = nullInt64Pointer(annualTick)
	calendarMetadata, err := decodeCityJSONMap(metadata)
	if err != nil {
		return nil, fmt.Errorf("decode city calendar metadata: %w", err)
	}
	state.Calendar.Metadata = calendarMetadata

	metadata = nil
	if err = queryer.QueryRowContext(ctx, `
SELECT world_id, parameter_set_code, parameter_version, periods_per_year,
       birth_rate_ppm, child_death_rate_ppm, working_death_rate_ppm,
       senior_death_rate_ppm, child_to_working_rate_ppm,
       working_to_senior_rate_ppm, version, metadata, created_at, updated_at
FROM city_demographic_policies WHERE world_id = $1`, worldID).Scan(
		&state.Policy.WorldID, &state.Policy.ParameterSetCode, &state.Policy.ParameterVersion,
		&state.Policy.PeriodsPerYear, &state.Policy.BirthRatePPM,
		&state.Policy.ChildDeathRatePPM, &state.Policy.WorkingDeathRatePPM,
		&state.Policy.SeniorDeathRatePPM, &state.Policy.ChildToWorkingRatePPM,
		&state.Policy.WorkingToSeniorRatePPM, &state.Policy.Version, &metadata,
		&state.Policy.CreatedAt, &state.Policy.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("load city demographic policy: %w", err)
	}
	policyMetadata, err := decodeCityJSONMap(metadata)
	if err != nil {
		return nil, fmt.Errorf("decode city demographic policy metadata: %w", err)
	}
	state.Policy.Metadata = policyMetadata

	rows, err := queryer.QueryContext(ctx, `
SELECT demographic.id, demographic.world_id, demographic.cohort_id,
       district.code, entity.code, cohort.income_band,
       demographic.child_units, demographic.working_units, demographic.senior_units,
       demographic.birth_remainder, demographic.child_death_remainder,
       demographic.working_death_remainder, demographic.senior_death_remainder,
       demographic.child_aging_remainder, demographic.working_aging_remainder,
       demographic.version, demographic.metadata, demographic.created_at, demographic.updated_at
FROM city_demographic_cohort_states demographic
JOIN city_household_cohorts cohort ON cohort.id = demographic.cohort_id
JOIN city_districts district ON district.id = cohort.district_id
JOIN city_economic_entities entity ON entity.id = cohort.entity_id
WHERE demographic.world_id = $1
ORDER BY district.sort_order ASC,
         CASE cohort.income_band WHEN 'low' THEN 1 WHEN 'middle' THEN 2 ELSE 3 END`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load city demographic cohorts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		item := &CityDemographicCohort{}
		metadata = nil
		if err = rows.Scan(
			&item.ID, &item.WorldID, &item.CohortID, &item.DistrictCode,
			&item.EntityCode, &item.IncomeBand, &item.ChildUnits, &item.WorkingUnits,
			&item.SeniorUnits, &item.BirthRemainder, &item.ChildDeathRemainder,
			&item.WorkingDeathRemainder, &item.SeniorDeathRemainder,
			&item.ChildAgingRemainder, &item.WorkingAgingRemainder,
			&item.Version, &metadata, &item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		item.Metadata, err = decodeCityJSONMap(metadata)
		if err != nil {
			return nil, fmt.Errorf("decode city demographic cohort metadata: %w", err)
		}
		state.Cohorts = append(state.Cohorts, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate city demographic cohorts: %w", err)
	}
	return state, nil
}

func (s *CityEconomyService) ListPopulationMovements(ctx context.Context, input CityPopulationMovementListInput) (*CityPopulationMovementPage, error) {
	if input.UserID <= 0 || input.WorldID <= 0 || input.AfterTick < 0 || input.AfterSequence < 0 {
		return nil, ErrCityInvalidInput
	}
	if input.Limit <= 0 {
		input.Limit = cityPopulationMovementDefaultLimit
	}
	if input.Limit > cityPopulationMovementMaximumLimit {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, input.UserID, input.WorldID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT `+cityPopulationMovementColumns+`
FROM city_population_movements m
WHERE m.world_id = $1 AND m.posted_at IS NOT NULL
  AND (m.tick > $2 OR (m.tick = $2 AND m.sequence > $3))
ORDER BY m.tick ASC, m.sequence ASC
LIMIT $4`, input.WorldID, input.AfterTick, input.AfterSequence, input.Limit+1)
	if err != nil {
		return nil, fmt.Errorf("list city population movements: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]*CityPopulationMovement, 0, input.Limit)
	for rows.Next() {
		item, scanErr := scanCityPopulationMovement(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate city population movements: %w", err)
	}
	page := &CityPopulationMovementPage{Items: items}
	if len(items) > input.Limit {
		items = items[:input.Limit]
		page.Items = items
		last := items[len(items)-1]
		page.NextCursor = &CityPopulationMovementCursor{Tick: last.Tick, Sequence: last.Sequence}
	}
	return page, nil
}

func (s *CityEconomyService) GetPopulationMovement(ctx context.Context, userID, worldID, tick int64, sequence int) (*CityPopulationMovement, error) {
	if userID <= 0 || worldID <= 0 || tick <= 0 || sequence <= 0 {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, userID, worldID); err != nil {
		return nil, err
	}
	item, err := loadCityPopulationMovementByCursor(ctx, s.db, worldID, tick, sequence, true)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCityPopulationMovementNotFound
	}
	return item, err
}

func loadCityPopulationMovementByCursor(ctx context.Context, queryer citySQLQueryer, worldID, tick int64, sequence int, withLines bool) (*CityPopulationMovement, error) {
	item, err := scanCityPopulationMovement(queryer.QueryRowContext(ctx, `
SELECT `+cityPopulationMovementColumns+`
FROM city_population_movements m
WHERE m.world_id = $1 AND m.tick = $2 AND m.sequence = $3 AND m.posted_at IS NOT NULL`,
		worldID, tick, sequence))
	if err != nil {
		return nil, err
	}
	if withLines {
		item.Lines, err = loadCityPopulationMovementLines(ctx, queryer, item.ID)
	}
	return item, err
}

func loadCityPopulationMovementsForTick(ctx context.Context, queryer citySQLQueryer, worldID, tick int64) ([]*CityPopulationMovement, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT `+cityPopulationMovementColumns+`
FROM city_population_movements m
WHERE m.world_id = $1 AND m.tick = $2 AND m.posted_at IS NOT NULL
ORDER BY m.sequence ASC`, worldID, tick)
	if err != nil {
		return nil, fmt.Errorf("load city population movements for tick: %w", err)
	}
	items := make([]*CityPopulationMovement, 0, 1)
	for rows.Next() {
		item, scanErr := scanCityPopulationMovement(rows)
		if scanErr != nil {
			_ = rows.Close()
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err = closeCityRows(rows, "iterate city population movements for tick"); err != nil {
		return nil, err
	}
	for _, item := range items {
		item.Lines, err = loadCityPopulationMovementLines(ctx, queryer, item.ID)
		if err != nil {
			return nil, err
		}
	}
	return items, nil
}

func scanCityPopulationMovement(scanner cityScannable) (*CityPopulationMovement, error) {
	item := &CityPopulationMovement{}
	var localMonth time.Time
	var metadata []byte
	var postedAt sql.NullTime
	if err := scanner.Scan(
		&item.ID, &item.WorldID, &item.Tick, &item.Sequence, &item.BoundaryID,
		&item.MovementType, &localMonth, &item.ParameterSetCode, &item.ParameterVersion,
		&item.ExpectedLineCount, &item.TotalBirthUnits, &item.TotalDeathUnits,
		&item.TotalTransitionUnits, &metadata, &postedAt, &item.CreatedAt,
	); err != nil {
		return nil, err
	}
	item.LocalMonth = localMonth.Format(time.DateOnly)
	item.PostedAt = nullTimePointer(postedAt)
	decoded, err := decodeCityJSONMap(metadata)
	if err != nil {
		return nil, fmt.Errorf("decode city population movement metadata: %w", err)
	}
	item.Metadata = decoded
	return item, nil
}

func loadCityPopulationMovementLines(ctx context.Context, queryer citySQLQueryer, movementID int64) ([]*CityPopulationMovementLine, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT line.id, line.movement_id, line.world_id, line.line_no, line.cohort_id,
       district.code, entity.code, cohort.income_band,
       line.demographic_version_before, line.demographic_version_after,
       line.cohort_version_before, line.cohort_version_after,
       line.birth_units, line.child_death_units, line.working_death_units,
       line.senior_death_units, line.child_to_working_units, line.working_to_senior_units,
       line.child_units_before, line.working_units_before, line.senior_units_before,
       line.child_units_after, line.working_units_after, line.senior_units_after,
       line.birth_remainder_before, line.child_death_remainder_before,
       line.working_death_remainder_before, line.senior_death_remainder_before,
       line.child_aging_remainder_before, line.working_aging_remainder_before,
       line.birth_remainder_after, line.child_death_remainder_after,
       line.working_death_remainder_after, line.senior_death_remainder_after,
       line.child_aging_remainder_after, line.working_aging_remainder_after,
       line.created_at
FROM city_population_movement_lines line
JOIN city_household_cohorts cohort ON cohort.id = line.cohort_id
JOIN city_districts district ON district.id = cohort.district_id
JOIN city_economic_entities entity ON entity.id = cohort.entity_id
WHERE line.movement_id = $1
ORDER BY line.line_no ASC`, movementID)
	if err != nil {
		return nil, fmt.Errorf("load city population movement lines: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]*CityPopulationMovementLine, 0)
	for rows.Next() {
		item := &CityPopulationMovementLine{}
		if err = rows.Scan(
			&item.ID, &item.MovementID, &item.WorldID, &item.LineNo, &item.CohortID,
			&item.DistrictCode, &item.EntityCode, &item.IncomeBand,
			&item.DemographicVersionBefore, &item.DemographicVersionAfter,
			&item.CohortVersionBefore, &item.CohortVersionAfter,
			&item.BirthUnits, &item.ChildDeathUnits, &item.WorkingDeathUnits,
			&item.SeniorDeathUnits, &item.ChildToWorkingUnits, &item.WorkingToSeniorUnits,
			&item.ChildUnitsBefore, &item.WorkingUnitsBefore, &item.SeniorUnitsBefore,
			&item.ChildUnitsAfter, &item.WorkingUnitsAfter, &item.SeniorUnitsAfter,
			&item.BirthRemainderBefore, &item.ChildDeathRemainderBefore,
			&item.WorkingDeathRemainderBefore, &item.SeniorDeathRemainderBefore,
			&item.ChildAgingRemainderBefore, &item.WorkingAgingRemainderBefore,
			&item.BirthRemainderAfter, &item.ChildDeathRemainderAfter,
			&item.WorkingDeathRemainderAfter, &item.SeniorDeathRemainderAfter,
			&item.ChildAgingRemainderAfter, &item.WorkingAgingRemainderAfter,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate city population movement lines: %w", err)
	}
	return items, nil
}

type cityDemographyHashState struct {
	Calendar cityDemographyHashCalendar `json:"calendar"`
	Policy   cityDemographyHashPolicy   `json:"policy"`
	Cohorts  []cityDemographyHashCohort `json:"cohorts"`
}

type cityDemographyHashCalendar struct {
	LocalDate         string          `json:"local_date"`
	DayIndex          int64           `json:"day_index"`
	MonthIndex        int64           `json:"month_index"`
	QuarterIndex      int64           `json:"quarter_index"`
	YearIndex         int64           `json:"year_index"`
	LastDailyTick     *int64          `json:"last_daily_tick"`
	LastMonthlyTick   *int64          `json:"last_monthly_tick"`
	LastQuarterlyTick *int64          `json:"last_quarterly_tick"`
	LastAnnualTick    *int64          `json:"last_annual_tick"`
	Version           int64           `json:"version"`
	Metadata          json.RawMessage `json:"metadata"`
}

type cityDemographyHashPolicy struct {
	ParameterSetCode       string          `json:"parameter_set_code"`
	ParameterVersion       int             `json:"parameter_version"`
	PeriodsPerYear         int             `json:"periods_per_year"`
	BirthRatePPM           int             `json:"birth_rate_ppm"`
	ChildDeathRatePPM      int             `json:"child_death_rate_ppm"`
	WorkingDeathRatePPM    int             `json:"working_death_rate_ppm"`
	SeniorDeathRatePPM     int             `json:"senior_death_rate_ppm"`
	ChildToWorkingRatePPM  int             `json:"child_to_working_rate_ppm"`
	WorkingToSeniorRatePPM int             `json:"working_to_senior_rate_ppm"`
	Version                int64           `json:"version"`
	Metadata               json.RawMessage `json:"metadata"`
}

type cityDemographyHashCohort struct {
	DistrictCode          string          `json:"district_code"`
	EntityCode            string          `json:"entity_code"`
	IncomeBand            string          `json:"income_band"`
	ChildUnits            int64           `json:"child_units"`
	WorkingUnits          int64           `json:"working_units"`
	SeniorUnits           int64           `json:"senior_units"`
	BirthRemainder        int64           `json:"birth_remainder"`
	ChildDeathRemainder   int64           `json:"child_death_remainder"`
	WorkingDeathRemainder int64           `json:"working_death_remainder"`
	SeniorDeathRemainder  int64           `json:"senior_death_remainder"`
	ChildAgingRemainder   int64           `json:"child_aging_remainder"`
	WorkingAgingRemainder int64           `json:"working_aging_remainder"`
	Version               int64           `json:"version"`
	Metadata              json.RawMessage `json:"metadata"`
}

func loadCityDemographyHashState(ctx context.Context, queryer citySQLQueryer, worldID int64) (cityDemographyHashState, error) {
	state := cityDemographyHashState{Cohorts: make([]cityDemographyHashCohort, 0)}
	var localDate time.Time
	var daily, monthly, quarterly, annual sql.NullInt64
	if err := queryer.QueryRowContext(ctx, `
SELECT local_date, day_index, month_index, quarter_index, year_index,
       last_daily_tick, last_monthly_tick, last_quarterly_tick, last_annual_tick, version, metadata
FROM city_calendar_states WHERE world_id = $1`, worldID).Scan(
		&localDate, &state.Calendar.DayIndex, &state.Calendar.MonthIndex,
		&state.Calendar.QuarterIndex, &state.Calendar.YearIndex,
		&daily, &monthly, &quarterly, &annual,
		&state.Calendar.Version, &state.Calendar.Metadata,
	); err != nil {
		return state, fmt.Errorf("load city calendar hash state: %w", err)
	}
	state.Calendar.LocalDate = localDate.Format(time.DateOnly)
	state.Calendar.LastDailyTick = nullInt64Pointer(daily)
	state.Calendar.LastMonthlyTick = nullInt64Pointer(monthly)
	state.Calendar.LastQuarterlyTick = nullInt64Pointer(quarterly)
	state.Calendar.LastAnnualTick = nullInt64Pointer(annual)

	if err := queryer.QueryRowContext(ctx, `
SELECT parameter_set_code, parameter_version, periods_per_year,
       birth_rate_ppm, child_death_rate_ppm, working_death_rate_ppm,
       senior_death_rate_ppm, child_to_working_rate_ppm,
       working_to_senior_rate_ppm, version, metadata
FROM city_demographic_policies WHERE world_id = $1`, worldID).Scan(
		&state.Policy.ParameterSetCode, &state.Policy.ParameterVersion,
		&state.Policy.PeriodsPerYear, &state.Policy.BirthRatePPM,
		&state.Policy.ChildDeathRatePPM, &state.Policy.WorkingDeathRatePPM,
		&state.Policy.SeniorDeathRatePPM, &state.Policy.ChildToWorkingRatePPM,
		&state.Policy.WorkingToSeniorRatePPM, &state.Policy.Version,
		&state.Policy.Metadata,
	); err != nil {
		return state, fmt.Errorf("load city demographic policy hash state: %w", err)
	}

	rows, err := queryer.QueryContext(ctx, `
SELECT district.code, entity.code, cohort.income_band,
       demographic.child_units, demographic.working_units, demographic.senior_units,
       demographic.birth_remainder, demographic.child_death_remainder,
       demographic.working_death_remainder, demographic.senior_death_remainder,
       demographic.child_aging_remainder, demographic.working_aging_remainder,
       demographic.version, demographic.metadata
FROM city_demographic_cohort_states demographic
JOIN city_household_cohorts cohort ON cohort.id = demographic.cohort_id
JOIN city_districts district ON district.id = cohort.district_id
JOIN city_economic_entities entity ON entity.id = cohort.entity_id
WHERE demographic.world_id = $1
ORDER BY district.sort_order ASC,
         CASE cohort.income_band WHEN 'low' THEN 1 WHEN 'middle' THEN 2 ELSE 3 END`, worldID)
	if err != nil {
		return state, fmt.Errorf("load city demographic cohort hash state: %w", err)
	}
	for rows.Next() {
		var item cityDemographyHashCohort
		if err = rows.Scan(
			&item.DistrictCode, &item.EntityCode, &item.IncomeBand,
			&item.ChildUnits, &item.WorkingUnits, &item.SeniorUnits,
			&item.BirthRemainder, &item.ChildDeathRemainder,
			&item.WorkingDeathRemainder, &item.SeniorDeathRemainder,
			&item.ChildAgingRemainder, &item.WorkingAgingRemainder,
			&item.Version, &item.Metadata,
		); err != nil {
			_ = rows.Close()
			return state, err
		}
		state.Cohorts = append(state.Cohorts, item)
	}
	if err = closeCityRows(rows, "iterate city demographic cohort hash state"); err != nil {
		return state, err
	}
	return state, nil
}

type cityDemographicRemainders struct {
	birth        int64
	childDeath   int64
	workingDeath int64
	seniorDeath  int64
	childAging   int64
	workingAging int64
}

type cityDemographicCohortRef struct {
	demographicID                 int64
	cohortID                      int64
	districtCode                  string
	entityCode                    string
	incomeBand                    string
	childUnits                    int64
	workingUnits                  int64
	seniorUnits                   int64
	employedUnits                 int64
	housingDemandUnits            int64
	allowsHouseholdReconciliation bool
	demographicVersion            int64
	cohortVersion                 int64
	remainders                    cityDemographicRemainders
}

type cityDemographicPlan struct {
	before               cityDemographicCohortRef
	childAfter           int64
	workingAfter         int64
	seniorAfter          int64
	birthUnits           int64
	childDeathUnits      int64
	workingDeathUnits    int64
	seniorDeathUnits     int64
	childToWorkingUnits  int64
	workingToSeniorUnits int64
	afterRemainders      cityDemographicRemainders
}

func cityRateAccrual(quantity int64, ratePPM int, remainder int64, periodsPerYear int) (int64, int64, error) {
	if quantity < 0 || ratePPM < 0 || ratePPM > int(cityDemographicRateScale) ||
		periodsPerYear <= 0 || remainder < 0 {
		return 0, 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "demographic_rate"})
	}
	if int64(periodsPerYear) > math.MaxInt64/cityDemographicRateScale {
		return 0, 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "demographic_period"})
	}
	denominator := int64(periodsPerYear) * cityDemographicRateScale
	if remainder >= denominator {
		return 0, 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "demographic_remainder"})
	}
	numerator, err := cityMultiplyUnits(quantity, int64(ratePPM))
	if err != nil {
		return 0, 0, err
	}
	numerator, err = addCityLedgerUnits(numerator, remainder)
	if err != nil {
		return 0, 0, err
	}
	return numerator / denominator, numerator % denominator, nil
}

func calculateCityDemographicPlan(ref cityDemographicCohortRef, policy cityDemographyHashPolicy) (cityDemographicPlan, error) {
	plan := cityDemographicPlan{before: ref}
	if ref.demographicVersion == math.MaxInt64 || ref.cohortVersion == math.MaxInt64 ||
		ref.childUnits < 0 || ref.workingUnits < 0 || ref.seniorUnits < 0 {
		return plan, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "demographic_version"})
	}
	total, err := addCityLedgerUnits(ref.childUnits, ref.workingUnits)
	if err != nil {
		return plan, err
	}
	total, err = addCityLedgerUnits(total, ref.seniorUnits)
	if err != nil {
		return plan, err
	}
	if total <= 0 || ref.workingUnits < ref.employedUnits || total < ref.housingDemandUnits {
		return plan, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "demographic_baseline"})
	}
	plan.birthUnits, plan.afterRemainders.birth, err = cityRateAccrual(total, policy.BirthRatePPM, ref.remainders.birth, policy.PeriodsPerYear)
	if err != nil {
		return plan, err
	}
	plan.childDeathUnits, plan.afterRemainders.childDeath, err = cityRateAccrual(ref.childUnits, policy.ChildDeathRatePPM, ref.remainders.childDeath, policy.PeriodsPerYear)
	if err != nil {
		return plan, err
	}
	plan.workingDeathUnits, plan.afterRemainders.workingDeath, err = cityRateAccrual(ref.workingUnits, policy.WorkingDeathRatePPM, ref.remainders.workingDeath, policy.PeriodsPerYear)
	if err != nil {
		return plan, err
	}
	plan.seniorDeathUnits, plan.afterRemainders.seniorDeath, err = cityRateAccrual(ref.seniorUnits, policy.SeniorDeathRatePPM, ref.remainders.seniorDeath, policy.PeriodsPerYear)
	if err != nil {
		return plan, err
	}
	plan.childToWorkingUnits, plan.afterRemainders.childAging, err = cityRateAccrual(ref.childUnits, policy.ChildToWorkingRatePPM, ref.remainders.childAging, policy.PeriodsPerYear)
	if err != nil {
		return plan, err
	}
	plan.workingToSeniorUnits, plan.afterRemainders.workingAging, err = cityRateAccrual(ref.workingUnits, policy.WorkingToSeniorRatePPM, ref.remainders.workingAging, policy.PeriodsPerYear)
	if err != nil {
		return plan, err
	}
	childOut, err := addCityLedgerUnits(plan.childDeathUnits, plan.childToWorkingUnits)
	if err != nil || childOut > ref.childUnits {
		return plan, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "child_population"}).WithCause(err)
	}
	workingOut, err := addCityLedgerUnits(plan.workingDeathUnits, plan.workingToSeniorUnits)
	if err != nil || workingOut > ref.workingUnits {
		return plan, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "working_population"}).WithCause(err)
	}
	if plan.seniorDeathUnits > ref.seniorUnits {
		return plan, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "senior_population"})
	}
	childWithBirths, err := addCityLedgerUnits(ref.childUnits, plan.birthUnits)
	if err != nil {
		return plan, err
	}
	plan.childAfter = childWithBirths - childOut
	workingWithEntrants, err := addCityLedgerUnits(ref.workingUnits, plan.childToWorkingUnits)
	if err != nil {
		return plan, err
	}
	plan.workingAfter = workingWithEntrants - workingOut
	seniorWithEntrants, err := addCityLedgerUnits(ref.seniorUnits, plan.workingToSeniorUnits)
	if err != nil {
		return plan, err
	}
	plan.seniorAfter = seniorWithEntrants - plan.seniorDeathUnits
	afterTotal, err := addCityLedgerUnits(plan.childAfter, plan.workingAfter)
	if err != nil {
		return plan, err
	}
	afterTotal, err = addCityLedgerUnits(afterTotal, plan.seniorAfter)
	if err != nil {
		return plan, err
	}
	if plan.workingAfter < ref.employedUnits || afterTotal <= 0 ||
		(!ref.allowsHouseholdReconciliation && afterTotal < ref.housingDemandUnits) {
		return plan, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "demographic_constraints"})
	}
	return plan, nil
}

func cityDemographyCohortKey(districtCode, entityCode, incomeBand string) string {
	return districtCode + "\x00" + entityCode + "\x00" + incomeBand
}

func cityLogicalDate(value time.Time) time.Time {
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}

func cityRowsAffectedExactlyOne(result sql.Result, label string) error {
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s rows affected: %w", label, err)
	}
	if rows != 1 {
		return fmt.Errorf("%s affected %d rows", label, rows)
	}
	return nil
}

func parseCityDate(value string) (time.Time, error) {
	parsed, err := time.Parse(time.DateOnly, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse city local date %q: %w", value, err)
	}
	return parsed, nil
}

func formatCityDate(value time.Time) string {
	return value.Format(time.DateOnly)
}

func intPointer(value int64) *int64 {
	return &value
}

func cityIntString(value int64) string {
	return strconv.FormatInt(value, 10)
}

type cityDemographyEvent struct {
	eventType string
	payload   map[string]any
}

type cityDemographyExecution struct {
	events        []cityDemographyEvent
	movementCount int
}

type cityCalendarStateRef struct {
	localDate         time.Time
	dayIndex          int64
	monthIndex        int64
	quarterIndex      int64
	yearIndex         int64
	lastDailyTick     *int64
	lastMonthlyTick   *int64
	lastQuarterlyTick *int64
	lastAnnualTick    *int64
	version           int64
}

type cityCalendarTransition struct {
	fromDate       time.Time
	toDate         time.Time
	dayChanged     bool
	monthChanged   bool
	quarterChanged bool
	yearChanged    bool
}

func resolveCityCalendarTransition(
	simulatedFrom, simulatedTo time.Time,
	timezone string,
) (cityCalendarTransition, error) {
	transition := cityCalendarTransition{}
	if !simulatedTo.After(simulatedFrom) {
		return transition, fmt.Errorf("simulation time must advance")
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return transition, fmt.Errorf("load city timezone %q: %w", timezone, err)
	}
	transition.fromDate = cityLogicalDate(simulatedFrom.In(location))
	transition.toDate = cityLogicalDate(simulatedTo.In(location))
	if transition.toDate.Equal(transition.fromDate) {
		return transition, nil
	}
	if !transition.toDate.Equal(transition.fromDate.AddDate(0, 0, 1)) {
		return transition, fmt.Errorf("city calendar boundary skips a local date")
	}
	transition.dayChanged = true
	transition.monthChanged = transition.fromDate.Year() != transition.toDate.Year() ||
		transition.fromDate.Month() != transition.toDate.Month()
	transition.quarterChanged = transition.fromDate.Year() != transition.toDate.Year() ||
		(int(transition.fromDate.Month())-1)/3 != (int(transition.toDate.Month())-1)/3
	transition.yearChanged = transition.fromDate.Year() != transition.toDate.Year()
	return transition, nil
}

func (s *CityEconomyService) advanceCityCalendarAndDemography(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	simulatedFrom, simulatedTo time.Time,
	timezone string,
) (cityDemographyExecution, error) {
	execution := cityDemographyExecution{events: make([]cityDemographyEvent, 0, 5)}
	if worldID <= 0 || targetTick <= 0 || !simulatedTo.After(simulatedFrom) {
		return execution, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "calendar_tick"})
	}
	transition, err := resolveCityCalendarTransition(simulatedFrom, simulatedTo, timezone)
	if err != nil {
		return execution, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "calendar_transition"}).WithCause(err)
	}
	calendar, err := lockCityCalendarState(ctx, tx, worldID)
	if err != nil {
		return execution, err
	}
	fromDate := transition.fromDate
	toDate := transition.toDate
	if !calendar.localDate.Equal(fromDate) {
		return execution, ErrCitySimulationInvariant.WithMetadata(map[string]string{
			"field": "calendar_projection", "expected": formatCityDate(fromDate),
			"actual": formatCityDate(calendar.localDate),
		})
	}
	if !transition.dayChanged {
		return execution, nil
	}
	if calendar.dayIndex == math.MaxInt64 || calendar.version == math.MaxInt64 {
		return execution, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "calendar_version"})
	}
	if transition.monthChanged && calendar.monthIndex == math.MaxInt64 {
		return execution, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "calendar_month_index"})
	}
	if transition.quarterChanged && calendar.quarterIndex == math.MaxInt64 {
		return execution, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "calendar_quarter_index"})
	}
	if transition.yearChanged && calendar.yearIndex == math.MaxInt64 {
		return execution, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "calendar_year_index"})
	}

	sequence := 1
	dayIndex := calendar.dayIndex + 1
	dayBoundary, err := insertCityCalendarBoundary(
		ctx, tx, worldID, targetTick, sequence, "day", fromDate, toDate, dayIndex,
	)
	if err != nil {
		return execution, err
	}
	execution.events = append(execution.events, cityCalendarBoundaryEvent(dayBoundary))
	sequence++
	monthIndex := calendar.monthIndex
	quarterIndex := calendar.quarterIndex
	yearIndex := calendar.yearIndex
	lastMonthlyTick := calendar.lastMonthlyTick
	lastQuarterlyTick := calendar.lastQuarterlyTick
	lastAnnualTick := calendar.lastAnnualTick
	var monthBoundary *CityCalendarBoundary
	if transition.monthChanged {
		monthIndex++
		monthBoundary, err = insertCityCalendarBoundary(
			ctx, tx, worldID, targetTick, sequence, "month", fromDate, toDate, monthIndex,
		)
		if err != nil {
			return execution, err
		}
		execution.events = append(execution.events, cityCalendarBoundaryEvent(monthBoundary))
		lastMonthlyTick = intPointer(targetTick)
		sequence++
	}
	if transition.quarterChanged {
		quarterIndex++
		quarterBoundary, insertErr := insertCityCalendarBoundary(
			ctx, tx, worldID, targetTick, sequence, "quarter", fromDate, toDate, quarterIndex,
		)
		if insertErr != nil {
			return execution, insertErr
		}
		execution.events = append(execution.events, cityCalendarBoundaryEvent(quarterBoundary))
		lastQuarterlyTick = intPointer(targetTick)
		sequence++
	}
	if transition.yearChanged {
		yearIndex++
		yearBoundary, insertErr := insertCityCalendarBoundary(
			ctx, tx, worldID, targetTick, sequence, "year", fromDate, toDate, yearIndex,
		)
		if insertErr != nil {
			return execution, insertErr
		}
		execution.events = append(execution.events, cityCalendarBoundaryEvent(yearBoundary))
		lastAnnualTick = intPointer(targetTick)
	}
	if _, err = tx.ExecContext(ctx, `
SELECT set_config('sub2api.city_f6_boundary_id', $1, TRUE)`, cityIntString(dayBoundary.ID)); err != nil {
		return execution, fmt.Errorf("activate city calendar write gate: %w", err)
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_calendar_states
SET local_date = $3, day_index = $4, month_index = $5, quarter_index = $6, year_index = $7,
    last_daily_tick = $8, last_monthly_tick = $9, last_quarterly_tick = $10,
    last_annual_tick = $11, version = $12, updated_at = NOW()
WHERE world_id = $1 AND local_date = $2 AND version = $13`,
		worldID, formatCityDate(calendar.localDate), formatCityDate(toDate), dayIndex,
		monthIndex, quarterIndex, yearIndex, targetTick, cityNullableInt64(lastMonthlyTick),
		cityNullableInt64(lastQuarterlyTick), cityNullableInt64(lastAnnualTick),
		calendar.version+1, calendar.version)
	if err != nil {
		return execution, fmt.Errorf("advance city calendar projection: %w", err)
	}
	if err = cityRowsAffectedExactlyOne(result, "advance city calendar projection"); err != nil {
		return execution, ErrCitySimulationInvariant.WithCause(err)
	}
	if monthBoundary != nil {
		movement, movementErr := s.settleCityNaturalChange(ctx, tx, worldID, targetTick, monthBoundary, toDate)
		if movementErr != nil {
			return execution, movementErr
		}
		execution.movementCount = 1
		execution.events = append(execution.events, cityDemographyEvent{
			eventType: "city.population.natural_change_posted",
			payload: map[string]any{
				"movement_id": movement.ID, "tick": movement.Tick,
				"local_month":      movement.LocalMonth,
				"birth_units":      movement.TotalBirthUnits,
				"death_units":      movement.TotalDeathUnits,
				"transition_units": movement.TotalTransitionUnits,
			},
		})
	}
	return execution, nil
}

func lockCityCalendarState(ctx context.Context, tx *sql.Tx, worldID int64) (cityCalendarStateRef, error) {
	var item cityCalendarStateRef
	var daily, monthly, quarterly, annual sql.NullInt64
	if err := tx.QueryRowContext(ctx, `
SELECT local_date, day_index, month_index, quarter_index, year_index,
       last_daily_tick, last_monthly_tick, last_quarterly_tick, last_annual_tick, version
FROM city_calendar_states WHERE world_id = $1 FOR UPDATE`, worldID).Scan(
		&item.localDate, &item.dayIndex, &item.monthIndex, &item.quarterIndex,
		&item.yearIndex, &daily, &monthly, &quarterly, &annual, &item.version,
	); err != nil {
		return item, fmt.Errorf("lock city calendar state: %w", err)
	}
	item.localDate = cityLogicalDate(item.localDate)
	item.lastDailyTick = nullInt64Pointer(daily)
	item.lastMonthlyTick = nullInt64Pointer(monthly)
	item.lastQuarterlyTick = nullInt64Pointer(quarterly)
	item.lastAnnualTick = nullInt64Pointer(annual)
	return item, nil
}

func insertCityCalendarBoundary(
	ctx context.Context,
	tx *sql.Tx,
	worldID, tick int64,
	sequence int,
	boundaryType string,
	previousDate, localDate time.Time,
	periodIndex int64,
) (*CityCalendarBoundary, error) {
	item := &CityCalendarBoundary{}
	var previous, current time.Time
	if err := tx.QueryRowContext(ctx, `
INSERT INTO city_calendar_boundaries AS boundary
    (world_id, tick, sequence, boundary_type, previous_local_date, local_date, period_index)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING boundary.id, boundary.world_id, boundary.tick, boundary.sequence,
          boundary.boundary_type, boundary.previous_local_date, boundary.local_date,
          boundary.period_index, boundary.created_at`,
		worldID, tick, sequence, boundaryType, formatCityDate(previousDate),
		formatCityDate(localDate), periodIndex).Scan(
		&item.ID, &item.WorldID, &item.Tick, &item.Sequence, &item.BoundaryType,
		&previous, &current, &item.PeriodIndex, &item.CreatedAt,
	); err != nil {
		return nil, fmt.Errorf("insert city %s boundary: %w", boundaryType, err)
	}
	item.PreviousLocalDate = formatCityDate(previous)
	item.LocalDate = formatCityDate(current)
	return item, nil
}

func cityCalendarBoundaryEvent(boundary *CityCalendarBoundary) cityDemographyEvent {
	return cityDemographyEvent{
		eventType: "city.calendar." + boundary.BoundaryType + "_started",
		payload: map[string]any{
			"boundary_id": boundary.ID, "tick": boundary.Tick,
			"local_date": boundary.LocalDate, "period_index": boundary.PeriodIndex,
		},
	}
}

func (s *CityEconomyService) settleCityNaturalChange(
	ctx context.Context,
	tx *sql.Tx,
	worldID, tick int64,
	boundary *CityCalendarBoundary,
	localMonth time.Time,
) (*CityPopulationMovement, error) {
	if boundary == nil || boundary.BoundaryType != "month" || boundary.Tick != tick || localMonth.Day() != 1 {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "population_boundary"})
	}
	policy, err := loadCityDemographicPolicyForUpdate(ctx, tx, worldID)
	if err != nil {
		return nil, err
	}
	cohorts, err := loadCityDemographicCohortRefsForUpdate(ctx, tx, worldID)
	if err != nil {
		return nil, err
	}
	if len(cohorts) == 0 {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "demographic_cohorts"})
	}
	plans := make([]cityDemographicPlan, 0, len(cohorts))
	var totalBirths, totalDeaths, totalTransitions int64
	for _, cohort := range cohorts {
		plan, planErr := calculateCityDemographicPlan(cohort, policy)
		if planErr != nil {
			return nil, planErr
		}
		plans = append(plans, plan)
		totalBirths, err = addCityLedgerUnits(totalBirths, plan.birthUnits)
		if err != nil {
			return nil, err
		}
		deaths, addErr := addCityLedgerUnits(plan.childDeathUnits, plan.workingDeathUnits)
		if addErr != nil {
			return nil, addErr
		}
		deaths, addErr = addCityLedgerUnits(deaths, plan.seniorDeathUnits)
		if addErr != nil {
			return nil, addErr
		}
		totalDeaths, err = addCityLedgerUnits(totalDeaths, deaths)
		if err != nil {
			return nil, err
		}
		transitions, addErr := addCityLedgerUnits(plan.childToWorkingUnits, plan.workingToSeniorUnits)
		if addErr != nil {
			return nil, addErr
		}
		totalTransitions, err = addCityLedgerUnits(totalTransitions, transitions)
		if err != nil {
			return nil, err
		}
	}

	movement, err := scanCityPopulationMovement(tx.QueryRowContext(ctx, `
INSERT INTO city_population_movements AS m
    (world_id, tick, sequence, boundary_id, movement_type, local_month,
     parameter_set_code, parameter_version, expected_line_count,
     total_birth_units, total_death_units, total_transition_units, metadata)
VALUES ($1, $2, 1, $3, $4, $5, $6, $7, $8, $9, $10, $11,
        '{"schema_version":1}'::jsonb)
RETURNING `+cityPopulationMovementColumns,
		worldID, tick, boundary.ID, CityPopulationMovementNaturalChange,
		formatCityDate(localMonth), policy.ParameterSetCode, policy.ParameterVersion,
		len(plans), totalBirths, totalDeaths, totalTransitions))
	if err != nil {
		return nil, fmt.Errorf("insert city population movement: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
SELECT set_config('sub2api.city_f6_movement_id', $1, TRUE)`, cityIntString(movement.ID)); err != nil {
		return nil, fmt.Errorf("activate city population movement write gate: %w", err)
	}
	for index, plan := range plans {
		if err = applyCityDemographicPlan(ctx, tx, movement.ID, worldID, index+1, plan); err != nil {
			return nil, err
		}
	}
	movement, err = scanCityPopulationMovement(tx.QueryRowContext(ctx, `
UPDATE city_population_movements AS m
SET posted_at = NOW()
WHERE m.id = $1 AND m.posted_at IS NULL
RETURNING `+cityPopulationMovementColumns, movement.ID))
	if err != nil {
		return nil, fmt.Errorf("post city population movement: %w", err)
	}
	movement.Lines, err = loadCityPopulationMovementLines(ctx, tx, movement.ID)
	if err != nil {
		return nil, err
	}
	return movement, nil
}

func loadCityDemographicPolicyForUpdate(ctx context.Context, tx *sql.Tx, worldID int64) (cityDemographyHashPolicy, error) {
	var policy cityDemographyHashPolicy
	if err := tx.QueryRowContext(ctx, `
SELECT parameter_set_code, parameter_version, periods_per_year,
       birth_rate_ppm, child_death_rate_ppm, working_death_rate_ppm,
       senior_death_rate_ppm, child_to_working_rate_ppm,
       working_to_senior_rate_ppm, version, metadata
FROM city_demographic_policies WHERE world_id = $1 FOR UPDATE`, worldID).Scan(
		&policy.ParameterSetCode, &policy.ParameterVersion, &policy.PeriodsPerYear,
		&policy.BirthRatePPM, &policy.ChildDeathRatePPM, &policy.WorkingDeathRatePPM,
		&policy.SeniorDeathRatePPM, &policy.ChildToWorkingRatePPM,
		&policy.WorkingToSeniorRatePPM, &policy.Version, &policy.Metadata,
	); err != nil {
		return policy, fmt.Errorf("lock city demographic policy: %w", err)
	}
	if policy.PeriodsPerYear != cityDemographicPeriodsPerYear {
		return policy, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "demographic_periods"})
	}
	return policy, nil
}

func loadCityDemographicCohortRefsForUpdate(ctx context.Context, tx *sql.Tx, worldID int64) ([]cityDemographicCohortRef, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT demographic.id, cohort.id, district.code, entity.code, cohort.income_band,
       demographic.child_units, demographic.working_units, demographic.senior_units,
       cohort.employed_units, cohort.housing_demand_units,
       world.simulation_version = 'city-f6-v3',
       demographic.version, cohort.version,
       demographic.birth_remainder, demographic.child_death_remainder,
       demographic.working_death_remainder, demographic.senior_death_remainder,
       demographic.child_aging_remainder, demographic.working_aging_remainder
FROM city_demographic_cohort_states demographic
JOIN city_household_cohorts cohort ON cohort.id = demographic.cohort_id
JOIN city_districts district ON district.id = cohort.district_id
JOIN city_economic_entities entity ON entity.id = cohort.entity_id
JOIN city_worlds world ON world.id = cohort.world_id
WHERE demographic.world_id = $1
ORDER BY district.sort_order ASC,
         CASE cohort.income_band WHEN 'low' THEN 1 WHEN 'middle' THEN 2 ELSE 3 END
FOR UPDATE OF demographic, cohort`, worldID)
	if err != nil {
		return nil, fmt.Errorf("lock city demographic cohorts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]cityDemographicCohortRef, 0)
	for rows.Next() {
		var item cityDemographicCohortRef
		if err = rows.Scan(
			&item.demographicID, &item.cohortID, &item.districtCode,
			&item.entityCode, &item.incomeBand, &item.childUnits,
			&item.workingUnits, &item.seniorUnits, &item.employedUnits,
			&item.housingDemandUnits, &item.allowsHouseholdReconciliation,
			&item.demographicVersion, &item.cohortVersion,
			&item.remainders.birth, &item.remainders.childDeath,
			&item.remainders.workingDeath, &item.remainders.seniorDeath,
			&item.remainders.childAging, &item.remainders.workingAging,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate locked city demographic cohorts: %w", err)
	}
	return items, nil
}

func applyCityDemographicPlan(
	ctx context.Context,
	tx *sql.Tx,
	movementID, worldID int64,
	lineNo int,
	plan cityDemographicPlan,
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
    birth_remainder = $6, child_death_remainder = $7,
    working_death_remainder = $8, senior_death_remainder = $9,
    child_aging_remainder = $10, working_aging_remainder = $11,
    version = $12, updated_at = NOW()
WHERE id = $1 AND world_id = $2 AND version = $13
  AND child_units = $14 AND working_units = $15 AND senior_units = $16
  AND birth_remainder = $17 AND child_death_remainder = $18
  AND working_death_remainder = $19 AND senior_death_remainder = $20
  AND child_aging_remainder = $21 AND working_aging_remainder = $22`,
		plan.before.demographicID, worldID, plan.childAfter, plan.workingAfter,
		plan.seniorAfter, plan.afterRemainders.birth, plan.afterRemainders.childDeath,
		plan.afterRemainders.workingDeath, plan.afterRemainders.seniorDeath,
		plan.afterRemainders.childAging, plan.afterRemainders.workingAging,
		demographicVersionAfter, plan.before.demographicVersion,
		plan.before.childUnits, plan.before.workingUnits, plan.before.seniorUnits,
		plan.before.remainders.birth, plan.before.remainders.childDeath,
		plan.before.remainders.workingDeath, plan.before.remainders.seniorDeath,
		plan.before.remainders.childAging, plan.before.remainders.workingAging)
	if err != nil {
		return fmt.Errorf("post city demographic cohort %d: %w", plan.before.cohortID, err)
	}
	if err = cityRowsAffectedExactlyOne(result, "post city demographic cohort"); err != nil {
		return ErrCitySimulationInvariant.WithCause(err)
	}
	result, err = tx.ExecContext(ctx, `
UPDATE city_household_cohorts
SET population_units = $3, working_age_units = $4, version = $5, updated_at = NOW()
WHERE id = $1 AND world_id = $2 AND version = $6
  AND population_units = $7 AND working_age_units = $8`,
		plan.before.cohortID, worldID, afterPopulation, plan.workingAfter,
		cohortVersionAfter, plan.before.cohortVersion, beforePopulation,
		plan.before.workingUnits)
	if err != nil {
		return fmt.Errorf("post city household demographic projection %d: %w", plan.before.cohortID, err)
	}
	if err = cityRowsAffectedExactlyOne(result, "post city household demographic projection"); err != nil {
		return ErrCitySimulationInvariant.WithCause(err)
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO city_population_movement_lines
    (movement_id, world_id, line_no, cohort_id,
     demographic_version_before, demographic_version_after,
     cohort_version_before, cohort_version_after,
     birth_units, child_death_units, working_death_units, senior_death_units,
     child_to_working_units, working_to_senior_units,
     child_units_before, working_units_before, senior_units_before,
     child_units_after, working_units_after, senior_units_after,
     birth_remainder_before, child_death_remainder_before,
     working_death_remainder_before, senior_death_remainder_before,
     child_aging_remainder_before, working_aging_remainder_before,
     birth_remainder_after, child_death_remainder_after,
     working_death_remainder_after, senior_death_remainder_after,
     child_aging_remainder_after, working_aging_remainder_after)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14,
        $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26,
        $27, $28, $29, $30, $31, $32)`,
		movementID, worldID, lineNo, plan.before.cohortID,
		plan.before.demographicVersion, demographicVersionAfter,
		plan.before.cohortVersion, cohortVersionAfter,
		plan.birthUnits, plan.childDeathUnits, plan.workingDeathUnits,
		plan.seniorDeathUnits, plan.childToWorkingUnits, plan.workingToSeniorUnits,
		plan.before.childUnits, plan.before.workingUnits, plan.before.seniorUnits,
		plan.childAfter, plan.workingAfter, plan.seniorAfter,
		plan.before.remainders.birth, plan.before.remainders.childDeath,
		plan.before.remainders.workingDeath, plan.before.remainders.seniorDeath,
		plan.before.remainders.childAging, plan.before.remainders.workingAging,
		plan.afterRemainders.birth, plan.afterRemainders.childDeath,
		plan.afterRemainders.workingDeath, plan.afterRemainders.seniorDeath,
		plan.afterRemainders.childAging, plan.afterRemainders.workingAging)
	if err != nil {
		return fmt.Errorf("insert city population movement line %d: %w", lineNo, err)
	}
	return nil
}

func replayCityCalendarAndPopulation(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, tick int64,
	state *cityHashState,
) error {
	migrations, err := loadCityPopulationMigrationsForTick(ctx, queryer, worldID, tick)
	if err != nil {
		return err
	}
	householdMovements, err := loadCityHouseholdMovementsForTick(ctx, queryer, worldID, tick)
	if err != nil {
		return err
	}
	for index, migration := range migrations {
		if migration.WorldID != worldID || migration.Tick != tick || migration.Sequence != int64(index+1) {
			return fmt.Errorf("population migration fact sequence is broken")
		}
	}
	for index, movement := range householdMovements {
		if movement.WorldID != worldID || movement.Tick != tick || movement.Sequence != int64(index+1) {
			return fmt.Errorf("household movement fact sequence is broken")
		}
	}
	if len(migrations) > 0 && !cityEngineSupportsPopulationMigration(state.SimulationVersion) {
		return fmt.Errorf("population migration facts require a migration-capable engine")
	}
	if len(householdMovements) > 0 && !cityEngineSupportsHouseholdLifecycle(state.SimulationVersion) {
		return fmt.Errorf("household movement facts require a household-capable engine")
	}
	if err = replayCityCalendarCommandFacts(ctx, queryer, worldID, tick, migrations, householdMovements, state); err != nil {
		return err
	}
	boundaries, err := loadCityCalendarBoundariesForTick(ctx, queryer, worldID, tick)
	if err != nil {
		return err
	}
	movements, err := loadCityPopulationMovementsForTick(ctx, queryer, worldID, tick)
	if err != nil {
		return err
	}
	var monthBoundary *CityCalendarBoundary
	if len(boundaries) > 0 {
		if err = replayCityCalendarBoundaries(tick, boundaries, &state.Demography.Calendar); err != nil {
			return err
		}
		for _, boundary := range boundaries {
			if boundary.BoundaryType == "month" {
				monthBoundary = boundary
			}
		}
	}
	if monthBoundary != nil {
		if len(movements) != 1 {
			return fmt.Errorf("monthly boundary requires one population movement")
		}
		movement := movements[0]
		if movement.WorldID != worldID || movement.Tick != tick || movement.Sequence != 1 ||
			movement.BoundaryID != monthBoundary.ID || movement.LocalMonth != monthBoundary.LocalDate ||
			movement.LocalMonth != state.Demography.Calendar.LocalDate {
			return fmt.Errorf("population movement does not match its monthly boundary")
		}
	} else if len(movements) != 0 {
		return fmt.Errorf("population movement exists without a monthly boundary")
	}
	for _, movement := range movements {
		if err = replayCityPopulationMovement(movement, state); err != nil {
			return err
		}
	}
	for _, movement := range householdMovements {
		if movement.Origin != CityHouseholdMovementOriginDemographyGuard {
			continue
		}
		if err = replayCityHouseholdMovement(movement, state); err != nil {
			return err
		}
	}
	if cityEngineSupportsHouseholdLifecycle(state.SimulationVersion) {
		return validateCityHouseholdHashProjection(state)
	}
	return nil
}

func replayCityCalendarCommandFacts(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, tick int64,
	migrations []*CityPopulationMigration,
	householdMovements []*CityHouseholdMovement,
	state *cityHashState,
) error {
	migrationByCommand := make(map[int64]*CityPopulationMigration, len(migrations))
	for _, migration := range migrations {
		migrationByCommand[migration.SourceCommandID] = migration
	}
	householdByCommand := make(map[int64]*CityHouseholdMovement, len(householdMovements))
	for _, movement := range householdMovements {
		if movement.Origin == CityHouseholdMovementOriginCommand {
			if movement.SourceCommandID == nil {
				return fmt.Errorf("command household movement is missing its source command")
			}
			householdByCommand[*movement.SourceCommandID] = movement
		}
	}
	rows, err := queryer.QueryContext(ctx, `
SELECT id, command_type, status
FROM city_commands
WHERE world_id = $1 AND processed_tick = $2
  AND command_type IN ('population.immigrate', 'population.emigrate', 'population.relocate',
                       'household.adjust', 'household.reclassify')
ORDER BY sequence ASC`, worldID, tick)
	if err != nil {
		return fmt.Errorf("load replay calendar commands: %w", err)
	}
	defer func() { _ = rows.Close() }()
	consumedMigrations := 0
	consumedHouseholds := 0
	for rows.Next() {
		var commandID int64
		var commandType, status string
		if err = rows.Scan(&commandID, &commandType, &status); err != nil {
			return err
		}
		if status != CityCommandStatusApplied {
			continue
		}
		if isCityPopulationMigrationCommand(commandType) {
			migration, ok := migrationByCommand[commandID]
			if !ok {
				return fmt.Errorf("applied population migration command is missing its fact")
			}
			if err = replayCityPopulationMigration(migration, state); err != nil {
				return err
			}
			consumedMigrations++
			continue
		}
		movement, ok := householdByCommand[commandID]
		if !ok {
			return fmt.Errorf("applied household command is missing its fact")
		}
		if err = replayCityHouseholdMovement(movement, state); err != nil {
			return err
		}
		consumedHouseholds++
	}
	if err = rows.Err(); err != nil {
		return fmt.Errorf("iterate replay calendar commands: %w", err)
	}
	if consumedMigrations != len(migrationByCommand) || consumedHouseholds != len(householdByCommand) {
		return fmt.Errorf("calendar command facts contain an orphan fact")
	}
	return nil
}

func loadCityCalendarBoundariesForTick(ctx context.Context, queryer citySQLQueryer, worldID, tick int64) ([]*CityCalendarBoundary, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT id, world_id, tick, sequence, boundary_type,
       previous_local_date, local_date, period_index, created_at
FROM city_calendar_boundaries
WHERE world_id = $1 AND tick = $2
ORDER BY sequence ASC`, worldID, tick)
	if err != nil {
		return nil, fmt.Errorf("load replay calendar boundaries: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]*CityCalendarBoundary, 0, 4)
	for rows.Next() {
		item := &CityCalendarBoundary{}
		var previous, current time.Time
		if err = rows.Scan(
			&item.ID, &item.WorldID, &item.Tick, &item.Sequence,
			&item.BoundaryType, &previous, &current, &item.PeriodIndex,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		item.PreviousLocalDate = formatCityDate(previous)
		item.LocalDate = formatCityDate(current)
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate replay calendar boundaries: %w", err)
	}
	return items, nil
}

func replayCityCalendarBoundaries(
	tick int64,
	boundaries []*CityCalendarBoundary,
	calendar *cityDemographyHashCalendar,
) error {
	if calendar == nil || len(boundaries) == 0 || calendar.Version == math.MaxInt64 {
		return fmt.Errorf("calendar boundary projection is invalid")
	}
	previousDate := calendar.LocalDate
	localDate := ""
	expectedSequence := 1
	seenDay, seenMonth, seenQuarter, seenYear := false, false, false, false
	for _, boundary := range boundaries {
		if boundary.Tick != tick || boundary.Sequence != expectedSequence ||
			boundary.PreviousLocalDate != previousDate ||
			(localDate != "" && boundary.LocalDate != localDate) {
			return fmt.Errorf("calendar boundary fact chain is broken")
		}
		localDate = boundary.LocalDate
		switch boundary.BoundaryType {
		case "day":
			if seenDay || boundary.PeriodIndex != calendar.DayIndex+1 {
				return fmt.Errorf("daily calendar boundary index is invalid")
			}
			seenDay = true
			calendar.DayIndex = boundary.PeriodIndex
			calendar.LastDailyTick = intPointer(tick)
		case "month":
			if !seenDay || seenMonth || boundary.PeriodIndex != calendar.MonthIndex+1 {
				return fmt.Errorf("monthly calendar boundary index is invalid")
			}
			seenMonth = true
			calendar.MonthIndex = boundary.PeriodIndex
			calendar.LastMonthlyTick = intPointer(tick)
		case "quarter":
			if !seenMonth || seenQuarter || boundary.PeriodIndex != calendar.QuarterIndex+1 {
				return fmt.Errorf("quarterly calendar boundary index is invalid")
			}
			seenQuarter = true
			calendar.QuarterIndex = boundary.PeriodIndex
			calendar.LastQuarterlyTick = intPointer(tick)
		case "year":
			if !seenQuarter || seenYear || boundary.PeriodIndex != calendar.YearIndex+1 {
				return fmt.Errorf("annual calendar boundary index is invalid")
			}
			seenYear = true
			calendar.YearIndex = boundary.PeriodIndex
			calendar.LastAnnualTick = intPointer(tick)
		default:
			return fmt.Errorf("unknown calendar boundary type %s", boundary.BoundaryType)
		}
		expectedSequence++
	}
	if !seenDay {
		return fmt.Errorf("calendar boundary set is missing day boundary")
	}
	parsedPrevious, err := parseCityDate(previousDate)
	if err != nil {
		return err
	}
	parsedCurrent, err := parseCityDate(localDate)
	if err != nil || !parsedCurrent.Equal(parsedPrevious.AddDate(0, 0, 1)) {
		return fmt.Errorf("calendar boundary date is not contiguous")
	}
	monthChanged := parsedPrevious.Year() != parsedCurrent.Year() ||
		parsedPrevious.Month() != parsedCurrent.Month()
	quarterChanged := parsedPrevious.Year() != parsedCurrent.Year() ||
		(int(parsedPrevious.Month())-1)/3 != (int(parsedCurrent.Month())-1)/3
	yearChanged := parsedPrevious.Year() != parsedCurrent.Year()
	if seenMonth != monthChanged || seenQuarter != quarterChanged || seenYear != yearChanged {
		return fmt.Errorf("calendar boundary types do not match the local date transition")
	}
	calendar.LocalDate = localDate
	calendar.Version++
	return nil
}

func replayCityPopulationAfter(before, inflow int64, outflows ...int64) (int64, error) {
	totalOutflow := int64(0)
	var err error
	for _, outflow := range outflows {
		totalOutflow, err = addCityLedgerUnits(totalOutflow, outflow)
		if err != nil {
			return 0, err
		}
	}
	if before < 0 || inflow < 0 || totalOutflow > before {
		return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "population_movement_equation"})
	}
	return addCityLedgerUnits(before-totalOutflow, inflow)
}

func cityDemographicRemainderValid(value int64, periodsPerYear int) bool {
	if periodsPerYear <= 0 || int64(periodsPerYear) > math.MaxInt64/cityDemographicRateScale {
		return false
	}
	return value >= 0 && value < int64(periodsPerYear)*cityDemographicRateScale
}

func replayCityPopulationMovement(movement *CityPopulationMovement, state *cityHashState) error {
	if movement == nil || movement.MovementType != CityPopulationMovementNaturalChange ||
		movement.PostedAt == nil || movement.ExpectedLineCount != len(movement.Lines) ||
		movement.ExpectedLineCount != len(state.Demography.Cohorts) ||
		movement.ParameterSetCode != state.Demography.Policy.ParameterSetCode ||
		movement.ParameterVersion != state.Demography.Policy.ParameterVersion {
		return fmt.Errorf("population movement header is invalid")
	}
	demographicIndex := make(map[string]int, len(state.Demography.Cohorts))
	for index, cohort := range state.Demography.Cohorts {
		demographicIndex[cityDemographyCohortKey(cohort.DistrictCode, cohort.EntityCode, cohort.IncomeBand)] = index
	}
	physicalIndex := make(map[string]int, len(state.Physical.HouseholdCohorts))
	for index, cohort := range state.Physical.HouseholdCohorts {
		physicalIndex[cityDemographyCohortKey(cohort.DistrictCode, cohort.EntityCode, cohort.IncomeBand)] = index
	}
	var totalBirths, totalDeaths, totalTransitions int64
	seenCohorts := make(map[string]struct{}, len(movement.Lines))
	for expectedLine, line := range movement.Lines {
		if line == nil || line.LineNo != expectedLine+1 || line.WorldID != movement.WorldID ||
			line.MovementID != movement.ID || line.DemographicVersionBefore == math.MaxInt64 ||
			line.CohortVersionBefore == math.MaxInt64 {
			return fmt.Errorf("population movement line order is invalid")
		}
		key := cityDemographyCohortKey(line.DistrictCode, line.EntityCode, line.IncomeBand)
		if _, exists := seenCohorts[key]; exists {
			return fmt.Errorf("population movement contains a duplicate cohort")
		}
		seenCohorts[key] = struct{}{}
		demographicPosition, ok := demographicIndex[key]
		if !ok {
			return fmt.Errorf("population movement references unknown demographic cohort")
		}
		physicalPosition, ok := physicalIndex[key]
		if !ok {
			return fmt.Errorf("population movement references unknown household cohort")
		}
		demographic := &state.Demography.Cohorts[demographicPosition]
		physical := &state.Physical.HouseholdCohorts[physicalPosition]
		if demographic.Version != line.DemographicVersionBefore ||
			line.DemographicVersionAfter != line.DemographicVersionBefore+1 ||
			demographic.ChildUnits != line.ChildUnitsBefore ||
			demographic.WorkingUnits != line.WorkingUnitsBefore ||
			demographic.SeniorUnits != line.SeniorUnitsBefore ||
			demographic.BirthRemainder != line.BirthRemainderBefore ||
			demographic.ChildDeathRemainder != line.ChildDeathRemainderBefore ||
			demographic.WorkingDeathRemainder != line.WorkingDeathRemainderBefore ||
			demographic.SeniorDeathRemainder != line.SeniorDeathRemainderBefore ||
			demographic.ChildAgingRemainder != line.ChildAgingRemainderBefore ||
			demographic.WorkingAgingRemainder != line.WorkingAgingRemainderBefore {
			return fmt.Errorf("demographic movement projection chain is broken")
		}
		beforePopulation, addErr := addCityLedgerUnits(line.ChildUnitsBefore, line.WorkingUnitsBefore)
		if addErr != nil {
			return addErr
		}
		beforePopulation, addErr = addCityLedgerUnits(beforePopulation, line.SeniorUnitsBefore)
		if addErr != nil {
			return addErr
		}
		if physical.Version != line.CohortVersionBefore ||
			line.CohortVersionAfter != line.CohortVersionBefore+1 ||
			physical.PopulationUnits != beforePopulation ||
			physical.WorkingAgeUnits != line.WorkingUnitsBefore {
			return fmt.Errorf("household demographic projection chain is broken")
		}
		expectedPlan, planErr := calculateCityDemographicPlan(cityDemographicCohortRef{
			childUnits: line.ChildUnitsBefore, workingUnits: line.WorkingUnitsBefore,
			seniorUnits: line.SeniorUnitsBefore, employedUnits: physical.EmployedUnits,
			housingDemandUnits:            physical.HousingDemandUnits,
			allowsHouseholdReconciliation: cityEngineSupportsHouseholdLifecycle(state.SimulationVersion),
			demographicVersion:            line.DemographicVersionBefore,
			cohortVersion:                 line.CohortVersionBefore,
			remainders: cityDemographicRemainders{
				birth: line.BirthRemainderBefore, childDeath: line.ChildDeathRemainderBefore,
				workingDeath: line.WorkingDeathRemainderBefore,
				seniorDeath:  line.SeniorDeathRemainderBefore,
				childAging:   line.ChildAgingRemainderBefore,
				workingAging: line.WorkingAgingRemainderBefore,
			},
		}, state.Demography.Policy)
		if planErr != nil {
			return planErr
		}
		if expectedPlan.birthUnits != line.BirthUnits ||
			expectedPlan.childDeathUnits != line.ChildDeathUnits ||
			expectedPlan.workingDeathUnits != line.WorkingDeathUnits ||
			expectedPlan.seniorDeathUnits != line.SeniorDeathUnits ||
			expectedPlan.childToWorkingUnits != line.ChildToWorkingUnits ||
			expectedPlan.workingToSeniorUnits != line.WorkingToSeniorUnits ||
			expectedPlan.childAfter != line.ChildUnitsAfter ||
			expectedPlan.workingAfter != line.WorkingUnitsAfter ||
			expectedPlan.seniorAfter != line.SeniorUnitsAfter ||
			expectedPlan.afterRemainders.birth != line.BirthRemainderAfter ||
			expectedPlan.afterRemainders.childDeath != line.ChildDeathRemainderAfter ||
			expectedPlan.afterRemainders.workingDeath != line.WorkingDeathRemainderAfter ||
			expectedPlan.afterRemainders.seniorDeath != line.SeniorDeathRemainderAfter ||
			expectedPlan.afterRemainders.childAging != line.ChildAgingRemainderAfter ||
			expectedPlan.afterRemainders.workingAging != line.WorkingAgingRemainderAfter {
			return fmt.Errorf("population movement does not match its versioned demographic policy")
		}
		expectedChild, equationErr := replayCityPopulationAfter(
			line.ChildUnitsBefore, line.BirthUnits,
			line.ChildDeathUnits, line.ChildToWorkingUnits,
		)
		if equationErr != nil {
			return equationErr
		}
		expectedWorking, equationErr := replayCityPopulationAfter(
			line.WorkingUnitsBefore, line.ChildToWorkingUnits,
			line.WorkingDeathUnits, line.WorkingToSeniorUnits,
		)
		if equationErr != nil {
			return equationErr
		}
		expectedSenior, equationErr := replayCityPopulationAfter(
			line.SeniorUnitsBefore, line.WorkingToSeniorUnits,
			line.SeniorDeathUnits,
		)
		if equationErr != nil {
			return equationErr
		}
		if expectedChild != line.ChildUnitsAfter || expectedWorking != line.WorkingUnitsAfter ||
			expectedSenior != line.SeniorUnitsAfter {
			return fmt.Errorf("population movement equations do not balance")
		}
		for _, remainder := range []int64{
			line.BirthRemainderAfter, line.ChildDeathRemainderAfter,
			line.WorkingDeathRemainderAfter, line.SeniorDeathRemainderAfter,
			line.ChildAgingRemainderAfter, line.WorkingAgingRemainderAfter,
		} {
			if !cityDemographicRemainderValid(remainder, state.Demography.Policy.PeriodsPerYear) {
				return fmt.Errorf("population movement remainder is invalid")
			}
		}
		afterPopulation, addErr := addCityLedgerUnits(expectedChild, expectedWorking)
		if addErr != nil {
			return addErr
		}
		afterPopulation, addErr = addCityLedgerUnits(afterPopulation, expectedSenior)
		if addErr != nil {
			return addErr
		}
		if expectedWorking < physical.EmployedUnits || afterPopulation <= 0 ||
			(!cityEngineSupportsHouseholdLifecycle(state.SimulationVersion) && afterPopulation < physical.HousingDemandUnits) {
			return fmt.Errorf("population movement violates household constraints")
		}
		demographic.ChildUnits = line.ChildUnitsAfter
		demographic.WorkingUnits = line.WorkingUnitsAfter
		demographic.SeniorUnits = line.SeniorUnitsAfter
		demographic.BirthRemainder = line.BirthRemainderAfter
		demographic.ChildDeathRemainder = line.ChildDeathRemainderAfter
		demographic.WorkingDeathRemainder = line.WorkingDeathRemainderAfter
		demographic.SeniorDeathRemainder = line.SeniorDeathRemainderAfter
		demographic.ChildAgingRemainder = line.ChildAgingRemainderAfter
		demographic.WorkingAgingRemainder = line.WorkingAgingRemainderAfter
		demographic.Version = line.DemographicVersionAfter
		physical.PopulationUnits = afterPopulation
		physical.WorkingAgeUnits = line.WorkingUnitsAfter
		physical.Version = line.CohortVersionAfter
		totalBirths, addErr = addCityLedgerUnits(totalBirths, line.BirthUnits)
		if addErr != nil {
			return addErr
		}
		deaths, addErr := addCityLedgerUnits(line.ChildDeathUnits, line.WorkingDeathUnits)
		if addErr != nil {
			return addErr
		}
		deaths, addErr = addCityLedgerUnits(deaths, line.SeniorDeathUnits)
		if addErr != nil {
			return addErr
		}
		totalDeaths, addErr = addCityLedgerUnits(totalDeaths, deaths)
		if addErr != nil {
			return addErr
		}
		transitions, addErr := addCityLedgerUnits(line.ChildToWorkingUnits, line.WorkingToSeniorUnits)
		if addErr != nil {
			return addErr
		}
		totalTransitions, addErr = addCityLedgerUnits(totalTransitions, transitions)
		if addErr != nil {
			return addErr
		}
	}
	if totalBirths != movement.TotalBirthUnits || totalDeaths != movement.TotalDeathUnits ||
		totalTransitions != movement.TotalTransitionUnits {
		return fmt.Errorf("population movement totals do not match lines")
	}
	return nil
}

func restoreCityDemographyProjection(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	state cityDemographyHashState,
	apply func(string, string, ...any) error,
) error {
	localDate, err := parseCityDate(state.Calendar.LocalDate)
	if err != nil {
		return err
	}
	if err = apply("calendar", `
UPDATE city_calendar_states
SET local_date = $2, day_index = $3, month_index = $4, quarter_index = $5, year_index = $6,
    last_daily_tick = $7, last_monthly_tick = $8, last_quarterly_tick = $9,
    last_annual_tick = $10, version = $11, metadata = $12::jsonb, updated_at = NOW()
WHERE world_id = $1`, worldID, formatCityDate(localDate), state.Calendar.DayIndex,
		state.Calendar.MonthIndex, state.Calendar.QuarterIndex, state.Calendar.YearIndex,
		cityNullableInt64(state.Calendar.LastDailyTick),
		cityNullableInt64(state.Calendar.LastMonthlyTick),
		cityNullableInt64(state.Calendar.LastQuarterlyTick),
		cityNullableInt64(state.Calendar.LastAnnualTick), state.Calendar.Version,
		[]byte(state.Calendar.Metadata)); err != nil {
		return err
	}
	if err = apply("demographic policy", `
UPDATE city_demographic_policies
SET parameter_set_code = $2, parameter_version = $3, periods_per_year = $4,
    birth_rate_ppm = $5, child_death_rate_ppm = $6,
    working_death_rate_ppm = $7, senior_death_rate_ppm = $8,
    child_to_working_rate_ppm = $9, working_to_senior_rate_ppm = $10,
    version = $11, metadata = $12::jsonb, updated_at = NOW()
WHERE world_id = $1`, worldID, state.Policy.ParameterSetCode,
		state.Policy.ParameterVersion, state.Policy.PeriodsPerYear,
		state.Policy.BirthRatePPM, state.Policy.ChildDeathRatePPM,
		state.Policy.WorkingDeathRatePPM, state.Policy.SeniorDeathRatePPM,
		state.Policy.ChildToWorkingRatePPM, state.Policy.WorkingToSeniorRatePPM,
		state.Policy.Version, []byte(state.Policy.Metadata)); err != nil {
		return err
	}
	for _, cohort := range state.Cohorts {
		label := "demographic cohort " + cohort.DistrictCode + "." + cohort.IncomeBand
		if err = apply(label, `
UPDATE city_demographic_cohort_states demographic
SET child_units = $5, working_units = $6, senior_units = $7,
    birth_remainder = $8, child_death_remainder = $9,
    working_death_remainder = $10, senior_death_remainder = $11,
    child_aging_remainder = $12, working_aging_remainder = $13,
    version = $14, metadata = $15::jsonb, updated_at = NOW()
FROM city_household_cohorts household
JOIN city_districts district ON district.id = household.district_id
JOIN city_economic_entities entity ON entity.id = household.entity_id
WHERE demographic.world_id = $1 AND demographic.cohort_id = household.id
  AND district.code = $2 AND entity.code = $3 AND household.income_band = $4`,
			worldID, cohort.DistrictCode, cohort.EntityCode, cohort.IncomeBand,
			cohort.ChildUnits, cohort.WorkingUnits, cohort.SeniorUnits,
			cohort.BirthRemainder, cohort.ChildDeathRemainder,
			cohort.WorkingDeathRemainder, cohort.SeniorDeathRemainder,
			cohort.ChildAgingRemainder, cohort.WorkingAgingRemainder,
			cohort.Version, []byte(cohort.Metadata)); err != nil {
			return err
		}
	}
	return nil
}
