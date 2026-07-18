package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

func (s *CityEconomyService) GetDevelopmentState(
	ctx context.Context,
	input CityDevelopmentQueryInput,
) (*CityDevelopmentState, error) {
	input.Status = strings.ToLower(strings.TrimSpace(input.Status))
	input.BuildingCode = strings.ToLower(strings.TrimSpace(input.BuildingCode))
	if input.UserID <= 0 || input.WorldID <= 0 || input.AfterTick < 0 ||
		input.AfterSequence < 0 || !isCityDevelopmentStatusFilter(input.Status) ||
		(input.BuildingCode != "" && !cityDevelopmentBuildingCodePattern.MatchString(input.BuildingCode)) {
		return nil, ErrCityInvalidInput
	}
	if input.AfterTick == 0 && input.AfterSequence != 0 {
		return nil, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "development_cursor"})
	}
	if input.Limit <= 0 {
		input.Limit = cityDevelopmentDefaultLimit
	}
	if input.Limit > cityDevelopmentMaximumLimit {
		return nil, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "limit"})
	}
	if err := authorizeCityWorldRead(ctx, s.db, input.UserID, input.WorldID); err != nil {
		return nil, err
	}
	var version string
	if err := s.db.QueryRowContext(ctx,
		`SELECT simulation_version FROM city_worlds WHERE id = $1`, input.WorldID,
	).Scan(&version); err != nil {
		return nil, fmt.Errorf("load city development world binding: %w", err)
	}
	if !cityEngineSupportsDevelopment(version) {
		return nil, ErrCityDevelopmentStateNotFound
	}

	state := &CityDevelopmentState{
		Projects:    make([]CityDevelopmentProject, 0),
		Facts:       make([]CityDevelopmentFact, 0),
		Adjustments: make([]CityBuildingAdjustment, 0),
		Developers:  make([]CityDevelopmentDeveloper, 0),
	}
	if err := loadCityDevelopmentProfile(ctx, s.db, input.WorldID, &state.Profile); err != nil {
		return nil, err
	}
	if err := loadCityDevelopmentProjects(ctx, s.db, input, state); err != nil {
		return nil, err
	}
	if err := loadCityDevelopmentFactPage(ctx, s.db, input, state); err != nil {
		return nil, err
	}
	if err := loadCityDevelopmentAdjustments(ctx, s.db, input, state); err != nil {
		return nil, err
	}
	if err := loadCityDevelopmentDevelopers(ctx, s.db, input.WorldID, state); err != nil {
		return nil, err
	}
	return state, nil
}

func isCityDevelopmentStatusFilter(status string) bool {
	switch status {
	case "", CityDevelopmentStatusSubmitted, CityDevelopmentStatusApproved,
		CityDevelopmentStatusRejected, CityDevelopmentStatusUnderConstruction,
		CityDevelopmentStatusCompleted, CityDevelopmentStatusCancelled:
		return true
	default:
		return false
	}
}

func loadCityDevelopmentProfile(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	profile *CityDevelopmentProfile,
) error {
	err := queryer.QueryRowContext(ctx, `
SELECT profile.policy_id, profile.policy_version, profile.policy_hash,
       profile.baseline_tick, baseline.baseline_hash, profile.project_count,
       profile.fact_count, profile.adjustment_count, profile.revision
FROM city_development_profiles profile
JOIN city_development_baselines baseline ON baseline.world_id = profile.world_id
WHERE profile.world_id = $1`, worldID).Scan(
		&profile.PolicyID, &profile.PolicyVersion, &profile.PolicyHash,
		&profile.BaselineTick, &profile.BaselineHash, &profile.ProjectCount,
		&profile.FactCount, &profile.AdjustmentCount, &profile.Revision,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrCityDevelopmentStateNotFound
	}
	if err != nil {
		return fmt.Errorf("load city development profile: %w", err)
	}
	return nil
}

func loadCityDevelopmentProjects(
	ctx context.Context,
	queryer citySQLQueryer,
	input CityDevelopmentQueryInput,
	state *CityDevelopmentState,
) error {
	rows, err := queryer.QueryContext(ctx, cityDevelopmentProjectCanonicalSelect+`
WHERE project.world_id = $1
  AND ($2 = '' OR project.status = $2)
  AND ($3 = '' OR building.code = $3)
ORDER BY project.submitted_tick DESC, project.code DESC
LIMIT $4`, input.WorldID, input.Status, input.BuildingCode, input.Limit)
	if err != nil {
		return fmt.Errorf("list city development projects: %w", err)
	}
	for rows.Next() {
		project, scanErr := scanCityDevelopmentProject(rows)
		if scanErr != nil {
			_ = rows.Close()
			return fmt.Errorf("scan city development project: %w", scanErr)
		}
		state.Projects = append(state.Projects, *project)
	}
	return closeCityRows(rows, "iterate city development projects")
}

func loadCityDevelopmentFactPage(
	ctx context.Context,
	queryer citySQLQueryer,
	input CityDevelopmentQueryInput,
	state *CityDevelopmentState,
) error {
	rows, err := queryer.QueryContext(ctx, cityDevelopmentFactCanonicalSelect+`
JOIN city_development_projects project
  ON project.world_id = fact.world_id AND project.code = fact.project_code
JOIN city_buildings building
  ON building.world_id = project.world_id AND building.id = project.building_id
WHERE fact.world_id = $1 AND fact.posted_at IS NOT NULL
  AND (fact.tick > $2 OR (fact.tick = $2 AND fact.sequence > $3))
  AND ($4 = '' OR project.status = $4)
  AND ($5 = '' OR building.code = $5)
ORDER BY fact.tick ASC, fact.sequence ASC
LIMIT $6`, input.WorldID, input.AfterTick, input.AfterSequence,
		input.Status, input.BuildingCode, input.Limit+1)
	if err != nil {
		return fmt.Errorf("list city development facts: %w", err)
	}
	for rows.Next() {
		fact, scanErr := scanCityDevelopmentFact(rows)
		if scanErr != nil {
			_ = rows.Close()
			return fmt.Errorf("scan city development fact: %w", scanErr)
		}
		state.Facts = append(state.Facts, *fact)
	}
	if err = closeCityRows(rows, "iterate city development facts"); err != nil {
		return err
	}
	if len(state.Facts) > input.Limit {
		state.Facts = state.Facts[:input.Limit]
		last := state.Facts[len(state.Facts)-1]
		state.NextCursor = &CityDevelopmentFactCursor{Tick: last.Tick, Sequence: last.Sequence}
	}
	return nil
}

func loadCityDevelopmentAdjustments(
	ctx context.Context,
	queryer citySQLQueryer,
	input CityDevelopmentQueryInput,
	state *CityDevelopmentState,
) error {
	if input.Status != "" && input.Status != CityDevelopmentStatusCompleted {
		return nil
	}
	rows, err := queryer.QueryContext(ctx, cityBuildingAdjustmentCanonicalSelect+`
WHERE adjustment.world_id = $1
  AND ($2 = '' OR building.code = $2)
ORDER BY adjustment.completed_tick DESC, building.code ASC, adjustment.project_code ASC
LIMIT $3`, input.WorldID, input.BuildingCode, input.Limit)
	if err != nil {
		return fmt.Errorf("list city building adjustments: %w", err)
	}
	for rows.Next() {
		adjustment, scanErr := scanCityBuildingAdjustment(rows)
		if scanErr != nil {
			_ = rows.Close()
			return fmt.Errorf("scan city building adjustment: %w", scanErr)
		}
		state.Adjustments = append(state.Adjustments, *adjustment)
	}
	return closeCityRows(rows, "iterate city building adjustments")
}

func loadCityDevelopmentDevelopers(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	state *CityDevelopmentState,
) error {
	rows, err := queryer.QueryContext(ctx, `
SELECT entity.id, entity.code, entity.name, district.code,
       firm.employee_units,
       COALESCE(SUM(project.required_labor_units)
         FILTER (WHERE project.status = 'under_construction'), 0)::BIGINT AS reserved_labor_units
FROM city_economic_entities entity
JOIN city_firm_states firm
  ON firm.world_id = entity.world_id AND firm.entity_id = entity.id
JOIN city_districts district
  ON district.world_id = firm.world_id AND district.id = firm.district_id
LEFT JOIN city_development_projects project
  ON project.world_id = entity.world_id AND project.developer_entity_id = entity.id
WHERE entity.world_id = $1 AND entity.entity_type = 'firm' AND entity.status = 'active'
GROUP BY entity.id, entity.code, entity.name, district.code, firm.employee_units
ORDER BY district.code ASC, entity.code ASC`, worldID)
	if err != nil {
		return fmt.Errorf("list city development developers: %w", err)
	}
	for rows.Next() {
		var developer CityDevelopmentDeveloper
		if err = rows.Scan(
			&developer.EntityID, &developer.EntityCode, &developer.EntityName,
			&developer.DistrictCode, &developer.EmployeeUnits,
			&developer.ReservedLaborUnits,
		); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan city development developer: %w", err)
		}
		developer.AvailableLaborUnits = developer.EmployeeUnits - developer.ReservedLaborUnits
		if developer.AvailableLaborUnits < 0 {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{
				"field":       "development_developer_labor",
				"entity_code": developer.EntityCode,
			})
		}
		state.Developers = append(state.Developers, developer)
	}
	return closeCityRows(rows, "iterate city development developers")
}
