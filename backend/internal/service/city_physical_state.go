package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type CityDistrict struct {
	ID                       int64          `json:"id"`
	WorldID                  int64          `json:"world_id"`
	Code                     string         `json:"code"`
	Name                     string         `json:"name"`
	SortOrder                int            `json:"sort_order"`
	AreaUnits                int64          `json:"area_units"`
	DevelopableAreaUnits     int64          `json:"developable_area_units"`
	ResidentialCapacityUnits int64          `json:"residential_capacity_units"`
	CommercialCapacityUnits  int64          `json:"commercial_capacity_units"`
	IndustrialCapacityUnits  int64          `json:"industrial_capacity_units"`
	Metadata                 map[string]any `json:"metadata"`
	CreatedAt                time.Time      `json:"created_at"`
	UpdatedAt                time.Time      `json:"updated_at"`
}

type CityHouseholdCohort struct {
	ID                        int64          `json:"id"`
	WorldID                   int64          `json:"world_id"`
	DistrictID                int64          `json:"district_id"`
	DistrictCode              string         `json:"district_code"`
	DistrictName              string         `json:"district_name"`
	EntityID                  int64          `json:"entity_id"`
	EntityCode                string         `json:"entity_code"`
	IncomeBand                string         `json:"income_band"`
	PopulationUnits           int64          `json:"population_units"`
	WorkingAgeUnits           int64          `json:"working_age_units"`
	EmployedUnits             int64          `json:"employed_units"`
	HouseholdUnits            int64          `json:"household_units,omitempty"`
	AverageHouseholdSizeMilli int64          `json:"average_household_size_milli,omitempty"`
	HousingDemandUnits        int64          `json:"housing_demand_units"`
	Version                   int64          `json:"version"`
	Metadata                  map[string]any `json:"metadata"`
	CreatedAt                 time.Time      `json:"created_at"`
	UpdatedAt                 time.Time      `json:"updated_at"`
}

type CityFirmState struct {
	ID                      int64          `json:"id"`
	WorldID                 int64          `json:"world_id"`
	EntityID                int64          `json:"entity_id"`
	EntityCode              string         `json:"entity_code"`
	EntityName              string         `json:"entity_name"`
	DistrictID              int64          `json:"district_id"`
	DistrictCode            string         `json:"district_code"`
	IndustryCode            string         `json:"industry_code"`
	EmployeeUnits           int64          `json:"employee_units"`
	CapitalStockUnits       int64          `json:"capital_stock_units"`
	ProductionCapacityUnits int64          `json:"production_capacity_units"`
	ProductivityMilli       int64          `json:"productivity_milli"`
	Version                 int64          `json:"version"`
	Metadata                map[string]any `json:"metadata"`
	CreatedAt               time.Time      `json:"created_at"`
	UpdatedAt               time.Time      `json:"updated_at"`
}

type CityGovernmentState struct {
	ID                          int64          `json:"id"`
	WorldID                     int64          `json:"world_id"`
	EntityID                    int64          `json:"entity_id"`
	EntityCode                  string         `json:"entity_code"`
	EntityName                  string         `json:"entity_name"`
	AdministrativeCapacityUnits int64          `json:"administrative_capacity_units"`
	PublicServiceCapacityUnits  int64          `json:"public_service_capacity_units"`
	Version                     int64          `json:"version"`
	Metadata                    map[string]any `json:"metadata"`
	CreatedAt                   time.Time      `json:"created_at"`
	UpdatedAt                   time.Time      `json:"updated_at"`
}

type CityGovernmentBudgetLine struct {
	ID                 int64          `json:"id"`
	WorldID            int64          `json:"world_id"`
	GovernmentEntityID int64          `json:"government_entity_id"`
	MonetaryUnitID     int64          `json:"monetary_unit_id"`
	MonetaryUnitCode   string         `json:"monetary_unit_code"`
	MonetaryUnitScale  int            `json:"monetary_unit_scale"`
	Code               string         `json:"code"`
	Name               string         `json:"name"`
	AppropriatedUnits  int64          `json:"appropriated_units"`
	CommittedUnits     int64          `json:"committed_units"`
	SpentUnits         int64          `json:"spent_units"`
	AvailableUnits     int64          `json:"available_units"`
	Version            int64          `json:"version"`
	Metadata           map[string]any `json:"metadata"`
	CreatedAt          time.Time      `json:"created_at"`
	UpdatedAt          time.Time      `json:"updated_at"`
}

type CityResource struct {
	ID           int64          `json:"id"`
	WorldID      int64          `json:"world_id"`
	Code         string         `json:"code"`
	Name         string         `json:"name"`
	ResourceKind string         `json:"resource_kind"`
	UnitCode     string         `json:"unit_code"`
	UnitScale    int            `json:"unit_scale"`
	Storable     bool           `json:"storable"`
	Status       string         `json:"status"`
	Metadata     map[string]any `json:"metadata"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

type CityInventoryBalance struct {
	ID                   int64          `json:"id"`
	WorldID              int64          `json:"world_id"`
	EntityID             int64          `json:"entity_id"`
	EntityType           string         `json:"entity_type"`
	EntityCode           string         `json:"entity_code"`
	EntityName           string         `json:"entity_name"`
	DistrictID           int64          `json:"district_id"`
	DistrictCode         string         `json:"district_code"`
	DistrictName         string         `json:"district_name"`
	ResourceID           int64          `json:"resource_id"`
	ResourceCode         string         `json:"resource_code"`
	ResourceName         string         `json:"resource_name"`
	ResourceKind         string         `json:"resource_kind"`
	UnitCode             string         `json:"unit_code"`
	UnitScale            int            `json:"unit_scale"`
	OpeningQuantityUnits int64          `json:"opening_quantity_units"`
	QuantityUnits        int64          `json:"quantity_units"`
	Version              int64          `json:"version"`
	Status               string         `json:"status"`
	Metadata             map[string]any `json:"metadata"`
	CreatedAt            time.Time      `json:"created_at"`
	UpdatedAt            time.Time      `json:"updated_at"`
}

type CityProductionRecipeLine struct {
	ID            int64  `json:"id"`
	ResourceID    int64  `json:"resource_id"`
	ResourceCode  string `json:"resource_code"`
	ResourceName  string `json:"resource_name"`
	Direction     string `json:"direction"`
	QuantityUnits int64  `json:"quantity_units"`
}

type CityProductionRecipe struct {
	ID                    int64                       `json:"id"`
	WorldID               int64                       `json:"world_id"`
	Code                  string                      `json:"code"`
	Name                  string                      `json:"name"`
	IndustryCode          string                      `json:"industry_code"`
	CapacityUnitsPerBatch int64                       `json:"capacity_units_per_batch"`
	Status                string                      `json:"status"`
	Metadata              map[string]any              `json:"metadata"`
	Lines                 []*CityProductionRecipeLine `json:"lines"`
	FirmEntityIDs         []int64                     `json:"firm_entity_ids"`
	CreatedAt             time.Time                   `json:"created_at"`
	UpdatedAt             time.Time                   `json:"updated_at"`
}

type CityPhysicalState struct {
	WorldID           int64                       `json:"world_id"`
	AsOfTick          int64                       `json:"as_of_tick"`
	SimulationVersion string                      `json:"simulation_version"`
	Districts         []*CityDistrict             `json:"districts"`
	HouseholdCohorts  []*CityHouseholdCohort      `json:"household_cohorts"`
	Firms             []*CityFirmState            `json:"firms"`
	Government        *CityGovernmentState        `json:"government"`
	BudgetLines       []*CityGovernmentBudgetLine `json:"budget_lines"`
	Resources         []*CityResource             `json:"resources"`
	Recipes           []*CityProductionRecipe     `json:"recipes"`
	Inventories       []*CityInventoryBalance     `json:"inventories"`
}

func (s *CityEconomyService) GetPhysicalState(ctx context.Context, userID, worldID int64) (*CityPhysicalState, error) {
	if userID <= 0 || worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, userID, worldID); err != nil {
		return nil, err
	}
	return loadCityPhysicalState(ctx, s.db, worldID)
}

func loadCityPhysicalState(ctx context.Context, queryer citySQLQueryer, worldID int64) (*CityPhysicalState, error) {
	state := &CityPhysicalState{
		WorldID: worldID, Districts: make([]*CityDistrict, 0),
		HouseholdCohorts: make([]*CityHouseholdCohort, 0), Firms: make([]*CityFirmState, 0),
		BudgetLines: make([]*CityGovernmentBudgetLine, 0), Resources: make([]*CityResource, 0),
		Recipes: make([]*CityProductionRecipe, 0), Inventories: make([]*CityInventoryBalance, 0),
	}
	if err := queryer.QueryRowContext(ctx, `
SELECT current_tick, simulation_version
FROM city_worlds
WHERE id = $1`, worldID).Scan(&state.AsOfTick, &state.SimulationVersion); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrCityWorldNotFound
		}
		return nil, fmt.Errorf("load city physical state tick: %w", err)
	}

	rows, err := queryer.QueryContext(ctx, `
SELECT id, world_id, code, name, sort_order, area_units, developable_area_units,
       residential_capacity_units, commercial_capacity_units, industrial_capacity_units,
       metadata, created_at, updated_at
FROM city_districts WHERE world_id = $1
ORDER BY sort_order ASC, code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load city districts: %w", err)
	}
	for rows.Next() {
		item := &CityDistrict{}
		var metadata []byte
		if err = rows.Scan(&item.ID, &item.WorldID, &item.Code, &item.Name, &item.SortOrder,
			&item.AreaUnits, &item.DevelopableAreaUnits, &item.ResidentialCapacityUnits,
			&item.CommercialCapacityUnits, &item.IndustrialCapacityUnits, &metadata,
			&item.CreatedAt, &item.UpdatedAt); err != nil {
			_ = rows.Close()
			return nil, err
		}
		item.Metadata, err = decodeCityJSONMap(metadata)
		if err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("decode city district metadata: %w", err)
		}
		state.Districts = append(state.Districts, item)
	}
	if err = closeCityRows(rows, "iterate city districts"); err != nil {
		return nil, err
	}

	rows, err = queryer.QueryContext(ctx, `
SELECT cohort.id, cohort.world_id, cohort.district_id, district.code, district.name,
       cohort.entity_id, entity.code, cohort.income_band, cohort.population_units,
       cohort.working_age_units, cohort.employed_units, cohort.household_units,
       FLOOR(cohort.population_units::NUMERIC * 1000 / cohort.household_units)::BIGINT,
       cohort.housing_demand_units,
       cohort.version, cohort.metadata, cohort.created_at, cohort.updated_at
FROM city_household_cohorts cohort
JOIN city_districts district ON district.id = cohort.district_id
JOIN city_economic_entities entity ON entity.id = cohort.entity_id
WHERE cohort.world_id = $1
ORDER BY district.sort_order ASC,
         CASE cohort.income_band WHEN 'low' THEN 1 WHEN 'middle' THEN 2 ELSE 3 END`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load city household cohorts: %w", err)
	}
	for rows.Next() {
		item := &CityHouseholdCohort{}
		var metadata []byte
		if err = rows.Scan(&item.ID, &item.WorldID, &item.DistrictID, &item.DistrictCode,
			&item.DistrictName, &item.EntityID, &item.EntityCode, &item.IncomeBand,
			&item.PopulationUnits, &item.WorkingAgeUnits, &item.EmployedUnits,
			&item.HouseholdUnits, &item.AverageHouseholdSizeMilli,
			&item.HousingDemandUnits, &item.Version, &metadata, &item.CreatedAt,
			&item.UpdatedAt); err != nil {
			_ = rows.Close()
			return nil, err
		}
		item.Metadata, err = decodeCityJSONMap(metadata)
		if err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("decode city household cohort metadata: %w", err)
		}
		if !cityEngineSupportsHouseholdLifecycle(state.SimulationVersion) {
			// The migration gives legacy rows an internal compatibility value, but
			// household facts only become authoritative after the explicit v3 upgrade.
			item.HouseholdUnits = 0
			item.AverageHouseholdSizeMilli = 0
		}
		state.HouseholdCohorts = append(state.HouseholdCohorts, item)
	}
	if err = closeCityRows(rows, "iterate city household cohorts"); err != nil {
		return nil, err
	}

	rows, err = queryer.QueryContext(ctx, `
SELECT firm.id, firm.world_id, firm.entity_id, entity.code, entity.name,
       firm.district_id, district.code, firm.industry_code, firm.employee_units,
       firm.capital_stock_units, firm.production_capacity_units, firm.productivity_milli,
       firm.version, firm.metadata, firm.created_at, firm.updated_at
FROM city_firm_states firm
JOIN city_economic_entities entity ON entity.id = firm.entity_id
JOIN city_districts district ON district.id = firm.district_id
WHERE firm.world_id = $1
ORDER BY entity.code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load city firm states: %w", err)
	}
	for rows.Next() {
		item := &CityFirmState{}
		var metadata []byte
		if err = rows.Scan(&item.ID, &item.WorldID, &item.EntityID, &item.EntityCode,
			&item.EntityName, &item.DistrictID, &item.DistrictCode, &item.IndustryCode,
			&item.EmployeeUnits, &item.CapitalStockUnits, &item.ProductionCapacityUnits,
			&item.ProductivityMilli, &item.Version, &metadata, &item.CreatedAt,
			&item.UpdatedAt); err != nil {
			_ = rows.Close()
			return nil, err
		}
		item.Metadata, err = decodeCityJSONMap(metadata)
		if err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("decode city firm state metadata: %w", err)
		}
		state.Firms = append(state.Firms, item)
	}
	if err = closeCityRows(rows, "iterate city firm states"); err != nil {
		return nil, err
	}

	state.Government = &CityGovernmentState{}
	var governmentMetadata []byte
	err = queryer.QueryRowContext(ctx, `
SELECT government.id, government.world_id, government.entity_id, entity.code, entity.name,
       government.administrative_capacity_units, government.public_service_capacity_units,
       government.version, government.metadata, government.created_at, government.updated_at
FROM city_government_states government
JOIN city_economic_entities entity ON entity.id = government.entity_id
WHERE government.world_id = $1`, worldID).Scan(
		&state.Government.ID, &state.Government.WorldID, &state.Government.EntityID,
		&state.Government.EntityCode, &state.Government.EntityName,
		&state.Government.AdministrativeCapacityUnits, &state.Government.PublicServiceCapacityUnits,
		&state.Government.Version, &governmentMetadata, &state.Government.CreatedAt,
		&state.Government.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("load city government state: %w", err)
	}
	state.Government.Metadata, err = decodeCityJSONMap(governmentMetadata)
	if err != nil {
		return nil, fmt.Errorf("decode city government state metadata: %w", err)
	}

	rows, err = queryer.QueryContext(ctx, `
SELECT budget.id, budget.world_id, budget.government_entity_id, budget.monetary_unit_id,
       unit.code, unit.scale, budget.code, budget.name, budget.appropriated_units,
       budget.committed_units, budget.spent_units,
       budget.appropriated_units - budget.committed_units - budget.spent_units,
       budget.version, budget.metadata, budget.created_at, budget.updated_at
FROM city_government_budget_lines budget
JOIN city_monetary_units unit ON unit.id = budget.monetary_unit_id
WHERE budget.world_id = $1
ORDER BY budget.code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load city government budget lines: %w", err)
	}
	for rows.Next() {
		item := &CityGovernmentBudgetLine{}
		var metadata []byte
		if err = rows.Scan(&item.ID, &item.WorldID, &item.GovernmentEntityID,
			&item.MonetaryUnitID, &item.MonetaryUnitCode, &item.MonetaryUnitScale,
			&item.Code, &item.Name, &item.AppropriatedUnits, &item.CommittedUnits,
			&item.SpentUnits, &item.AvailableUnits, &item.Version, &metadata,
			&item.CreatedAt, &item.UpdatedAt); err != nil {
			_ = rows.Close()
			return nil, err
		}
		item.Metadata, err = decodeCityJSONMap(metadata)
		if err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("decode city budget metadata: %w", err)
		}
		state.BudgetLines = append(state.BudgetLines, item)
	}
	if err = closeCityRows(rows, "iterate city budget lines"); err != nil {
		return nil, err
	}

	rows, err = queryer.QueryContext(ctx, `
SELECT id, world_id, code, name, resource_kind, unit_code, unit_scale, storable,
       status, metadata, created_at, updated_at
FROM city_resources WHERE world_id = $1
ORDER BY code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load city resources: %w", err)
	}
	for rows.Next() {
		item := &CityResource{}
		var metadata []byte
		if err = rows.Scan(&item.ID, &item.WorldID, &item.Code, &item.Name,
			&item.ResourceKind, &item.UnitCode, &item.UnitScale, &item.Storable,
			&item.Status, &metadata, &item.CreatedAt, &item.UpdatedAt); err != nil {
			_ = rows.Close()
			return nil, err
		}
		item.Metadata, err = decodeCityJSONMap(metadata)
		if err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("decode city resource metadata: %w", err)
		}
		state.Resources = append(state.Resources, item)
	}
	if err = closeCityRows(rows, "iterate city resources"); err != nil {
		return nil, err
	}

	recipeByID := make(map[int64]*CityProductionRecipe)
	rows, err = queryer.QueryContext(ctx, `
SELECT id, world_id, code, name, industry_code, capacity_units_per_batch,
       status, metadata, created_at, updated_at
FROM city_production_recipes WHERE world_id = $1
ORDER BY code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load city production recipes: %w", err)
	}
	for rows.Next() {
		item := &CityProductionRecipe{Lines: make([]*CityProductionRecipeLine, 0), FirmEntityIDs: make([]int64, 0)}
		var metadata []byte
		if err = rows.Scan(&item.ID, &item.WorldID, &item.Code, &item.Name,
			&item.IndustryCode, &item.CapacityUnitsPerBatch, &item.Status, &metadata,
			&item.CreatedAt, &item.UpdatedAt); err != nil {
			_ = rows.Close()
			return nil, err
		}
		item.Metadata, err = decodeCityJSONMap(metadata)
		if err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("decode city recipe metadata: %w", err)
		}
		recipeByID[item.ID] = item
		state.Recipes = append(state.Recipes, item)
	}
	if err = closeCityRows(rows, "iterate city production recipes"); err != nil {
		return nil, err
	}

	rows, err = queryer.QueryContext(ctx, `
SELECT line.id, line.recipe_id, line.resource_id, resource.code, resource.name,
       line.direction, line.quantity_units
FROM city_production_recipe_lines line
JOIN city_resources resource ON resource.id = line.resource_id
JOIN city_production_recipes recipe ON recipe.id = line.recipe_id
WHERE line.world_id = $1
ORDER BY recipe.code ASC,
         CASE line.direction WHEN 'input' THEN 1 ELSE 2 END,
         resource.code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load city production recipe lines: %w", err)
	}
	for rows.Next() {
		item := &CityProductionRecipeLine{}
		var recipeID int64
		if err = rows.Scan(&item.ID, &recipeID, &item.ResourceID, &item.ResourceCode,
			&item.ResourceName, &item.Direction, &item.QuantityUnits); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if recipe := recipeByID[recipeID]; recipe != nil {
			recipe.Lines = append(recipe.Lines, item)
		}
	}
	if err = closeCityRows(rows, "iterate city production recipe lines"); err != nil {
		return nil, err
	}

	rows, err = queryer.QueryContext(ctx, `
SELECT recipe_id, firm_entity_id
FROM city_firm_recipes
WHERE world_id = $1 AND status = 'active'
ORDER BY recipe_id ASC, firm_entity_id ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load city firm recipe grants: %w", err)
	}
	for rows.Next() {
		var recipeID, firmID int64
		if err = rows.Scan(&recipeID, &firmID); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if recipe := recipeByID[recipeID]; recipe != nil {
			recipe.FirmEntityIDs = append(recipe.FirmEntityIDs, firmID)
		}
	}
	if err = closeCityRows(rows, "iterate city firm recipe grants"); err != nil {
		return nil, err
	}

	rows, err = queryer.QueryContext(ctx, `
SELECT balance.id, balance.world_id, balance.entity_id, balance.entity_type,
       entity.code, entity.name, balance.district_id, district.code, district.name,
       balance.resource_id, resource.code, resource.name, resource.resource_kind,
       resource.unit_code, resource.unit_scale, balance.opening_quantity_units,
       balance.quantity_units, balance.version, balance.status, balance.metadata,
       balance.created_at, balance.updated_at
FROM city_inventory_balances balance
JOIN city_economic_entities entity ON entity.id = balance.entity_id
JOIN city_districts district ON district.id = balance.district_id
JOIN city_resources resource ON resource.id = balance.resource_id
WHERE balance.world_id = $1
ORDER BY entity.code ASC, district.sort_order ASC, resource.code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load city inventory balances: %w", err)
	}
	for rows.Next() {
		item := &CityInventoryBalance{}
		var metadata []byte
		if err = rows.Scan(&item.ID, &item.WorldID, &item.EntityID, &item.EntityType,
			&item.EntityCode, &item.EntityName, &item.DistrictID, &item.DistrictCode,
			&item.DistrictName, &item.ResourceID, &item.ResourceCode, &item.ResourceName,
			&item.ResourceKind, &item.UnitCode, &item.UnitScale, &item.OpeningQuantityUnits,
			&item.QuantityUnits, &item.Version, &item.Status, &metadata, &item.CreatedAt,
			&item.UpdatedAt); err != nil {
			_ = rows.Close()
			return nil, err
		}
		item.Metadata, err = decodeCityJSONMap(metadata)
		if err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("decode city inventory metadata: %w", err)
		}
		state.Inventories = append(state.Inventories, item)
	}
	if err = closeCityRows(rows, "iterate city inventory balances"); err != nil {
		return nil, err
	}
	return state, nil
}

func closeCityRows(rows *sql.Rows, label string) error {
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("%s: %w", label, err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close city rows: %w", err)
	}
	return nil
}

type cityPhysicalHashState struct {
	Districts        []cityHashDistrict        `json:"districts"`
	HouseholdCohorts []cityHashHouseholdCohort `json:"household_cohorts"`
	Firms            []cityHashFirm            `json:"firms"`
	Government       cityHashGovernment        `json:"government"`
	BudgetLines      []cityHashBudgetLine      `json:"budget_lines"`
	Resources        []cityHashResource        `json:"resources"`
	Recipes          []cityHashRecipe          `json:"recipes"`
	FirmRecipes      []cityHashFirmRecipe      `json:"firm_recipes"`
	Inventories      []cityHashInventory       `json:"inventories"`
}

type cityHashDistrict struct {
	Code                 string          `json:"code"`
	Name                 string          `json:"name"`
	SortOrder            int             `json:"sort_order"`
	AreaUnits            int64           `json:"area_units"`
	DevelopableAreaUnits int64           `json:"developable_area_units"`
	ResidentialCapacity  int64           `json:"residential_capacity_units"`
	CommercialCapacity   int64           `json:"commercial_capacity_units"`
	IndustrialCapacity   int64           `json:"industrial_capacity_units"`
	Metadata             json.RawMessage `json:"metadata"`
}

type cityHashHouseholdCohort struct {
	DistrictCode       string          `json:"district_code"`
	EntityCode         string          `json:"entity_code"`
	IncomeBand         string          `json:"income_band"`
	PopulationUnits    int64           `json:"population_units"`
	WorkingAgeUnits    int64           `json:"working_age_units"`
	EmployedUnits      int64           `json:"employed_units"`
	HouseholdUnits     int64           `json:"household_units,omitempty"`
	HousingDemandUnits int64           `json:"housing_demand_units"`
	Version            int64           `json:"version"`
	Metadata           json.RawMessage `json:"metadata"`
}

type cityHashFirm struct {
	EntityCode              string          `json:"entity_code"`
	DistrictCode            string          `json:"district_code"`
	IndustryCode            string          `json:"industry_code"`
	EmployeeUnits           int64           `json:"employee_units"`
	CapitalStockUnits       int64           `json:"capital_stock_units"`
	ProductionCapacityUnits int64           `json:"production_capacity_units"`
	ProductivityMilli       int64           `json:"productivity_milli"`
	Version                 int64           `json:"version"`
	Metadata                json.RawMessage `json:"metadata"`
}

type cityHashGovernment struct {
	EntityCode                  string          `json:"entity_code"`
	AdministrativeCapacityUnits int64           `json:"administrative_capacity_units"`
	PublicServiceCapacityUnits  int64           `json:"public_service_capacity_units"`
	Version                     int64           `json:"version"`
	Metadata                    json.RawMessage `json:"metadata"`
}

type cityHashBudgetLine struct {
	EntityCode        string          `json:"entity_code"`
	MonetaryUnitCode  string          `json:"monetary_unit_code"`
	Code              string          `json:"code"`
	Name              string          `json:"name"`
	AppropriatedUnits int64           `json:"appropriated_units"`
	CommittedUnits    int64           `json:"committed_units"`
	SpentUnits        int64           `json:"spent_units"`
	Version           int64           `json:"version"`
	Metadata          json.RawMessage `json:"metadata"`
}

type cityHashResource struct {
	Code         string          `json:"code"`
	Name         string          `json:"name"`
	ResourceKind string          `json:"resource_kind"`
	UnitCode     string          `json:"unit_code"`
	UnitScale    int             `json:"unit_scale"`
	Storable     bool            `json:"storable"`
	Status       string          `json:"status"`
	Metadata     json.RawMessage `json:"metadata"`
}

type cityHashRecipeLine struct {
	ResourceCode  string `json:"resource_code"`
	Direction     string `json:"direction"`
	QuantityUnits int64  `json:"quantity_units"`
}

type cityHashRecipe struct {
	Code                  string               `json:"code"`
	Name                  string               `json:"name"`
	IndustryCode          string               `json:"industry_code"`
	CapacityUnitsPerBatch int64                `json:"capacity_units_per_batch"`
	Status                string               `json:"status"`
	Metadata              json.RawMessage      `json:"metadata"`
	Lines                 []cityHashRecipeLine `json:"lines"`
}

type cityHashFirmRecipe struct {
	FirmEntityCode string `json:"firm_entity_code"`
	RecipeCode     string `json:"recipe_code"`
	Status         string `json:"status"`
}

type cityHashInventory struct {
	EntityType           string          `json:"entity_type"`
	EntityCode           string          `json:"entity_code"`
	DistrictCode         string          `json:"district_code"`
	ResourceCode         string          `json:"resource_code"`
	OpeningQuantityUnits int64           `json:"opening_quantity_units"`
	QuantityUnits        int64           `json:"quantity_units"`
	Version              int64           `json:"version"`
	Status               string          `json:"status"`
	Metadata             json.RawMessage `json:"metadata"`
}

func loadCityPhysicalHashState(ctx context.Context, queryer citySQLQueryer, worldID int64) (cityPhysicalHashState, error) {
	state := cityPhysicalHashState{
		Districts: make([]cityHashDistrict, 0), HouseholdCohorts: make([]cityHashHouseholdCohort, 0),
		Firms: make([]cityHashFirm, 0), BudgetLines: make([]cityHashBudgetLine, 0),
		Resources: make([]cityHashResource, 0), Recipes: make([]cityHashRecipe, 0),
		FirmRecipes: make([]cityHashFirmRecipe, 0), Inventories: make([]cityHashInventory, 0),
	}
	rows, err := queryer.QueryContext(ctx, `
SELECT code, name, sort_order, area_units, developable_area_units,
       residential_capacity_units, commercial_capacity_units, industrial_capacity_units, metadata
FROM city_districts WHERE world_id = $1 ORDER BY sort_order ASC, code ASC`, worldID)
	if err != nil {
		return state, fmt.Errorf("load city districts for hash: %w", err)
	}
	for rows.Next() {
		var item cityHashDistrict
		if err = rows.Scan(&item.Code, &item.Name, &item.SortOrder, &item.AreaUnits,
			&item.DevelopableAreaUnits, &item.ResidentialCapacity, &item.CommercialCapacity,
			&item.IndustrialCapacity, &item.Metadata); err != nil {
			_ = rows.Close()
			return state, err
		}
		state.Districts = append(state.Districts, item)
	}
	if err = closeCityRows(rows, "iterate city districts for hash"); err != nil {
		return state, err
	}

	rows, err = queryer.QueryContext(ctx, `
SELECT district.code, entity.code, cohort.income_band, cohort.population_units,
       cohort.working_age_units, cohort.employed_units, cohort.household_units,
       cohort.housing_demand_units,
       cohort.version, cohort.metadata
FROM city_household_cohorts cohort
JOIN city_districts district ON district.id = cohort.district_id
JOIN city_economic_entities entity ON entity.id = cohort.entity_id
WHERE cohort.world_id = $1
ORDER BY district.sort_order ASC,
         CASE cohort.income_band WHEN 'low' THEN 1 WHEN 'middle' THEN 2 ELSE 3 END`, worldID)
	if err != nil {
		return state, fmt.Errorf("load city cohorts for hash: %w", err)
	}
	for rows.Next() {
		var item cityHashHouseholdCohort
		if err = rows.Scan(&item.DistrictCode, &item.EntityCode, &item.IncomeBand,
			&item.PopulationUnits, &item.WorkingAgeUnits, &item.EmployedUnits,
			&item.HouseholdUnits, &item.HousingDemandUnits, &item.Version, &item.Metadata); err != nil {
			_ = rows.Close()
			return state, err
		}
		state.HouseholdCohorts = append(state.HouseholdCohorts, item)
	}
	if err = closeCityRows(rows, "iterate city cohorts for hash"); err != nil {
		return state, err
	}

	rows, err = queryer.QueryContext(ctx, `
SELECT entity.code, district.code, firm.industry_code, firm.employee_units,
       firm.capital_stock_units, firm.production_capacity_units, firm.productivity_milli,
       firm.version, firm.metadata
FROM city_firm_states firm
JOIN city_economic_entities entity ON entity.id = firm.entity_id
JOIN city_districts district ON district.id = firm.district_id
WHERE firm.world_id = $1 ORDER BY entity.code ASC`, worldID)
	if err != nil {
		return state, fmt.Errorf("load city firms for hash: %w", err)
	}
	for rows.Next() {
		var item cityHashFirm
		if err = rows.Scan(&item.EntityCode, &item.DistrictCode, &item.IndustryCode,
			&item.EmployeeUnits, &item.CapitalStockUnits, &item.ProductionCapacityUnits,
			&item.ProductivityMilli, &item.Version, &item.Metadata); err != nil {
			_ = rows.Close()
			return state, err
		}
		state.Firms = append(state.Firms, item)
	}
	if err = closeCityRows(rows, "iterate city firms for hash"); err != nil {
		return state, err
	}

	err = queryer.QueryRowContext(ctx, `
SELECT entity.code, government.administrative_capacity_units,
       government.public_service_capacity_units, government.version, government.metadata
FROM city_government_states government
JOIN city_economic_entities entity ON entity.id = government.entity_id
WHERE government.world_id = $1`, worldID).Scan(
		&state.Government.EntityCode, &state.Government.AdministrativeCapacityUnits,
		&state.Government.PublicServiceCapacityUnits, &state.Government.Version,
		&state.Government.Metadata,
	)
	if err != nil {
		return state, fmt.Errorf("load city government for hash: %w", err)
	}

	rows, err = queryer.QueryContext(ctx, `
SELECT entity.code, unit.code, budget.code, budget.name, budget.appropriated_units,
       budget.committed_units, budget.spent_units, budget.version, budget.metadata
FROM city_government_budget_lines budget
JOIN city_economic_entities entity ON entity.id = budget.government_entity_id
JOIN city_monetary_units unit ON unit.id = budget.monetary_unit_id
WHERE budget.world_id = $1 ORDER BY budget.code ASC`, worldID)
	if err != nil {
		return state, fmt.Errorf("load city budgets for hash: %w", err)
	}
	for rows.Next() {
		var item cityHashBudgetLine
		if err = rows.Scan(&item.EntityCode, &item.MonetaryUnitCode, &item.Code, &item.Name,
			&item.AppropriatedUnits, &item.CommittedUnits, &item.SpentUnits,
			&item.Version, &item.Metadata); err != nil {
			_ = rows.Close()
			return state, err
		}
		state.BudgetLines = append(state.BudgetLines, item)
	}
	if err = closeCityRows(rows, "iterate city budgets for hash"); err != nil {
		return state, err
	}

	rows, err = queryer.QueryContext(ctx, `
SELECT code, name, resource_kind, unit_code, unit_scale, storable, status, metadata
FROM city_resources WHERE world_id = $1 ORDER BY code ASC`, worldID)
	if err != nil {
		return state, fmt.Errorf("load city resources for hash: %w", err)
	}
	for rows.Next() {
		var item cityHashResource
		if err = rows.Scan(&item.Code, &item.Name, &item.ResourceKind, &item.UnitCode,
			&item.UnitScale, &item.Storable, &item.Status, &item.Metadata); err != nil {
			_ = rows.Close()
			return state, err
		}
		state.Resources = append(state.Resources, item)
	}
	if err = closeCityRows(rows, "iterate city resources for hash"); err != nil {
		return state, err
	}

	recipeIndex := make(map[int64]int)
	rows, err = queryer.QueryContext(ctx, `
SELECT id, code, name, industry_code, capacity_units_per_batch, status, metadata
FROM city_production_recipes WHERE world_id = $1 ORDER BY code ASC`, worldID)
	if err != nil {
		return state, fmt.Errorf("load city recipes for hash: %w", err)
	}
	for rows.Next() {
		var id int64
		item := cityHashRecipe{Lines: make([]cityHashRecipeLine, 0)}
		if err = rows.Scan(&id, &item.Code, &item.Name, &item.IndustryCode,
			&item.CapacityUnitsPerBatch, &item.Status, &item.Metadata); err != nil {
			_ = rows.Close()
			return state, err
		}
		recipeIndex[id] = len(state.Recipes)
		state.Recipes = append(state.Recipes, item)
	}
	if err = closeCityRows(rows, "iterate city recipes for hash"); err != nil {
		return state, err
	}

	rows, err = queryer.QueryContext(ctx, `
SELECT line.recipe_id, resource.code, line.direction, line.quantity_units
FROM city_production_recipe_lines line
JOIN city_resources resource ON resource.id = line.resource_id
JOIN city_production_recipes recipe ON recipe.id = line.recipe_id
WHERE line.world_id = $1
ORDER BY recipe.code ASC,
         CASE line.direction WHEN 'input' THEN 1 ELSE 2 END,
         resource.code ASC`, worldID)
	if err != nil {
		return state, fmt.Errorf("load city recipe lines for hash: %w", err)
	}
	for rows.Next() {
		var recipeID int64
		var line cityHashRecipeLine
		if err = rows.Scan(&recipeID, &line.ResourceCode, &line.Direction, &line.QuantityUnits); err != nil {
			_ = rows.Close()
			return state, err
		}
		if index, ok := recipeIndex[recipeID]; ok {
			state.Recipes[index].Lines = append(state.Recipes[index].Lines, line)
		}
	}
	if err = closeCityRows(rows, "iterate city recipe lines for hash"); err != nil {
		return state, err
	}

	rows, err = queryer.QueryContext(ctx, `
SELECT entity.code, recipe.code, firm_recipe.status
FROM city_firm_recipes firm_recipe
JOIN city_economic_entities entity ON entity.id = firm_recipe.firm_entity_id
JOIN city_production_recipes recipe ON recipe.id = firm_recipe.recipe_id
WHERE firm_recipe.world_id = $1 ORDER BY entity.code ASC, recipe.code ASC`, worldID)
	if err != nil {
		return state, fmt.Errorf("load city firm recipes for hash: %w", err)
	}
	for rows.Next() {
		var item cityHashFirmRecipe
		if err = rows.Scan(&item.FirmEntityCode, &item.RecipeCode, &item.Status); err != nil {
			_ = rows.Close()
			return state, err
		}
		state.FirmRecipes = append(state.FirmRecipes, item)
	}
	if err = closeCityRows(rows, "iterate city firm recipes for hash"); err != nil {
		return state, err
	}

	rows, err = queryer.QueryContext(ctx, `
SELECT balance.entity_type, entity.code, district.code, resource.code,
       balance.opening_quantity_units, balance.quantity_units, balance.version,
       balance.status, balance.metadata
FROM city_inventory_balances balance
JOIN city_economic_entities entity ON entity.id = balance.entity_id
JOIN city_districts district ON district.id = balance.district_id
JOIN city_resources resource ON resource.id = balance.resource_id
WHERE balance.world_id = $1
ORDER BY entity.code ASC, district.sort_order ASC, resource.code ASC`, worldID)
	if err != nil {
		return state, fmt.Errorf("load city inventories for hash: %w", err)
	}
	for rows.Next() {
		var item cityHashInventory
		if err = rows.Scan(&item.EntityType, &item.EntityCode, &item.DistrictCode,
			&item.ResourceCode, &item.OpeningQuantityUnits, &item.QuantityUnits,
			&item.Version, &item.Status, &item.Metadata); err != nil {
			_ = rows.Close()
			return state, err
		}
		state.Inventories = append(state.Inventories, item)
	}
	if err = closeCityRows(rows, "iterate city inventories for hash"); err != nil {
		return state, err
	}
	return state, nil
}
