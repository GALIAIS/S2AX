package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strconv"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	CityMarketLabor      = "labor"
	CityMarketBasicGoods = "basic_goods"
	CityMarketHousing    = "housing"
	CitySettlementFiscal = "fiscal"

	cityDefaultSettlementLimit = 50
	cityMaximumSettlementLimit = 200
)

var ErrCityMarketSettlementNotFound = infraerrors.NotFound(
	"CITY_MARKET_SETTLEMENT_NOT_FOUND", "city market settlement not found",
)

type CityEconomicCycleState struct {
	WorldID         int64          `json:"world_id"`
	CycleIndex      int64          `json:"cycle_index"`
	CadenceTicks    int            `json:"cadence_ticks"`
	NextDueTick     int64          `json:"next_due_tick"`
	LastSettledTick *int64         `json:"last_settled_tick,omitempty"`
	Version         int64          `json:"version"`
	Metadata        map[string]any `json:"metadata"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

type CityEconomicPolicy struct {
	WorldID                      int64          `json:"world_id"`
	LaborDemandCapacityMilli     int            `json:"labor_demand_capacity_milli"`
	GoodsDemandPopulationDivisor int64          `json:"goods_demand_population_divisor"`
	HouseholdWageTaxMilli        int            `json:"household_wage_tax_milli"`
	FirmSalesTaxMilli            int            `json:"firm_sales_tax_milli"`
	ProcurementShareMilli        int            `json:"procurement_share_milli"`
	SocialSupportShareMilli      int            `json:"social_support_share_milli"`
	Version                      int64          `json:"version"`
	Metadata                     map[string]any `json:"metadata"`
	CreatedAt                    time.Time      `json:"created_at"`
	UpdatedAt                    time.Time      `json:"updated_at"`
}

type CityMarketState struct {
	ID                     int64          `json:"id"`
	WorldID                int64          `json:"world_id"`
	MonetaryUnitID         int64          `json:"monetary_unit_id"`
	MonetaryUnitCode       string         `json:"monetary_unit_code"`
	ResourceID             *int64         `json:"resource_id,omitempty"`
	ResourceCode           *string        `json:"resource_code,omitempty"`
	MarketCode             string         `json:"market_code"`
	QuoteUnits             int64          `json:"quote_units"`
	FloorUnits             int64          `json:"floor_units"`
	CeilingUnits           int64          `json:"ceiling_units"`
	MaximumAdjustmentMilli int            `json:"maximum_adjustment_milli"`
	LastClearingTick       *int64         `json:"last_clearing_tick,omitempty"`
	LastClearingPriceUnits *int64         `json:"last_clearing_price_units,omitempty"`
	LastDemandUnits        int64          `json:"last_demand_units"`
	LastSupplyUnits        int64          `json:"last_supply_units"`
	LastClearedUnits       int64          `json:"last_cleared_units"`
	LastUnmetDemandUnits   int64          `json:"last_unmet_demand_units"`
	LastExcessSupplyUnits  int64          `json:"last_excess_supply_units"`
	Version                int64          `json:"version"`
	Metadata               map[string]any `json:"metadata"`
	CreatedAt              time.Time      `json:"created_at"`
	UpdatedAt              time.Time      `json:"updated_at"`
}

type CityHousingOccupancy struct {
	ID              int64          `json:"id"`
	WorldID         int64          `json:"world_id"`
	CohortID        int64          `json:"cohort_id"`
	IncomeBand      string         `json:"income_band"`
	DistrictID      int64          `json:"district_id"`
	DistrictCode    string         `json:"district_code"`
	OccupiedUnits   int64          `json:"occupied_units"`
	UnmetUnits      int64          `json:"unmet_units"`
	RentPriceUnits  int64          `json:"rent_price_units"`
	LastSettledTick *int64         `json:"last_settled_tick,omitempty"`
	Version         int64          `json:"version"`
	Metadata        map[string]any `json:"metadata"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

type CityMarketOverview struct {
	WorldID     int64                   `json:"world_id"`
	AsOfTick    int64                   `json:"as_of_tick"`
	Cycle       *CityEconomicCycleState `json:"cycle"`
	Policy      *CityEconomicPolicy     `json:"policy"`
	Markets     []*CityMarketState      `json:"markets"`
	Occupancies []*CityHousingOccupancy `json:"occupancies"`
}

type CityMarketAllocation struct {
	ID             int64          `json:"id"`
	SettlementID   int64          `json:"settlement_id"`
	LineNo         int            `json:"line_no"`
	AllocationType string         `json:"allocation_type"`
	CohortID       *int64         `json:"cohort_id,omitempty"`
	IncomeBand     *string        `json:"income_band,omitempty"`
	FromEntityID   *int64         `json:"from_entity_id,omitempty"`
	FromEntityCode *string        `json:"from_entity_code,omitempty"`
	ToEntityID     *int64         `json:"to_entity_id,omitempty"`
	ToEntityCode   *string        `json:"to_entity_code,omitempty"`
	DistrictID     *int64         `json:"district_id,omitempty"`
	DistrictCode   *string        `json:"district_code,omitempty"`
	ResourceID     *int64         `json:"resource_id,omitempty"`
	ResourceCode   *string        `json:"resource_code,omitempty"`
	QuantityUnits  int64          `json:"quantity_units"`
	UnitPriceUnits int64          `json:"unit_price_units"`
	AmountUnits    int64          `json:"amount_units"`
	Metadata       map[string]any `json:"metadata"`
	CreatedAt      time.Time      `json:"created_at"`
}

type CityBudgetMovement struct {
	ID                  int64     `json:"id"`
	SettlementID        int64     `json:"settlement_id"`
	LineNo              int       `json:"line_no"`
	BudgetLineID        int64     `json:"budget_line_id"`
	BudgetCode          string    `json:"budget_code"`
	MovementType        string    `json:"movement_type"`
	AmountUnits         int64     `json:"amount_units"`
	SpentBeforeUnits    int64     `json:"spent_before_units"`
	SpentAfterUnits     int64     `json:"spent_after_units"`
	BudgetVersionBefore int64     `json:"budget_version_before"`
	BudgetVersionAfter  int64     `json:"budget_version_after"`
	Memo                string    `json:"memo"`
	CreatedAt           time.Time `json:"created_at"`
}

type CityMarketSettlement struct {
	ID                     int64                   `json:"id"`
	WorldID                int64                   `json:"world_id"`
	MonetaryUnitID         int64                   `json:"monetary_unit_id"`
	MonetaryUnitCode       string                  `json:"monetary_unit_code"`
	MonetaryUnitScale      int                     `json:"monetary_unit_scale"`
	Tick                   int64                   `json:"tick"`
	Sequence               int                     `json:"sequence"`
	CycleIndex             int64                   `json:"cycle_index"`
	SettlementKey          string                  `json:"settlement_key"`
	SettlementType         string                  `json:"settlement_type"`
	ClearingPriceUnits     int64                   `json:"clearing_price_units"`
	DemandUnits            int64                   `json:"demand_units"`
	SupplyUnits            int64                   `json:"supply_units"`
	ClearedUnits           int64                   `json:"cleared_units"`
	UnmetDemandUnits       int64                   `json:"unmet_demand_units"`
	ExcessSupplyUnits      int64                   `json:"excess_supply_units"`
	GrossAmountUnits       int64                   `json:"gross_amount_units"`
	JournalCount           int                     `json:"journal_count"`
	ResourceOperationCount int                     `json:"resource_operation_count"`
	AllocationCount        int                     `json:"allocation_count"`
	BudgetMovementCount    int                     `json:"budget_movement_count"`
	Metadata               map[string]any          `json:"metadata"`
	CreatedAt              time.Time               `json:"created_at"`
	PostedAt               time.Time               `json:"posted_at"`
	Allocations            []*CityMarketAllocation `json:"allocations,omitempty"`
	BudgetMovements        []*CityBudgetMovement   `json:"budget_movements,omitempty"`
}

type CityMarketSettlementCursor struct {
	Tick     int64 `json:"tick"`
	Sequence int   `json:"sequence"`
}

type CityMarketSettlementPage struct {
	Items      []*CityMarketSettlement     `json:"items"`
	NextCursor *CityMarketSettlementCursor `json:"next_cursor,omitempty"`
}

type CityMarketSettlementListInput struct {
	UserID        int64
	WorldID       int64
	AfterTick     int64
	AfterSequence int
	Limit         int
}

func (s *CityEconomyService) GetMarketOverview(ctx context.Context, userID, worldID int64) (*CityMarketOverview, error) {
	if userID <= 0 || worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, userID, worldID); err != nil {
		return nil, err
	}
	return loadCityMarketOverview(ctx, s.db, worldID)
}

func (s *CityEconomyService) ListMarketSettlements(ctx context.Context, input CityMarketSettlementListInput) (*CityMarketSettlementPage, error) {
	if input.UserID <= 0 || input.WorldID <= 0 || input.AfterTick < 0 || input.AfterSequence < 0 {
		return nil, ErrCityInvalidInput
	}
	if input.Limit <= 0 {
		input.Limit = cityDefaultSettlementLimit
	}
	if input.Limit > cityMaximumSettlementLimit {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, input.UserID, input.WorldID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, cityMarketSettlementSelect+`
WHERE settlement.world_id = $1 AND settlement.posted_at IS NOT NULL
  AND (settlement.tick > $2 OR (settlement.tick = $2 AND settlement.sequence > $3))
ORDER BY settlement.tick ASC, settlement.sequence ASC
LIMIT $4`, input.WorldID, input.AfterTick, input.AfterSequence, input.Limit+1)
	if err != nil {
		return nil, fmt.Errorf("list city market settlements: %w", err)
	}
	items := make([]*CityMarketSettlement, 0, input.Limit+1)
	for rows.Next() {
		item, scanErr := scanCityMarketSettlement(rows)
		if scanErr != nil {
			_ = rows.Close()
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err = closeCityRows(rows, "iterate city market settlements"); err != nil {
		return nil, err
	}
	page := &CityMarketSettlementPage{Items: items}
	if len(items) > input.Limit {
		items = items[:input.Limit]
		page.Items = items
		last := items[len(items)-1]
		page.NextCursor = &CityMarketSettlementCursor{Tick: last.Tick, Sequence: last.Sequence}
	}
	return page, nil
}

func (s *CityEconomyService) GetMarketSettlement(ctx context.Context, userID, worldID, tick int64, sequence int) (*CityMarketSettlement, error) {
	if userID <= 0 || worldID <= 0 || tick <= 0 || sequence <= 0 {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, userID, worldID); err != nil {
		return nil, err
	}
	item, err := loadCityMarketSettlementByCursor(ctx, s.db, worldID, tick, sequence, true)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCityMarketSettlementNotFound
	}
	return item, err
}

const cityMarketSettlementSelect = `
SELECT settlement.id, settlement.world_id, settlement.monetary_unit_id,
       unit.code, unit.scale, settlement.tick, settlement.sequence,
       settlement.cycle_index, settlement.settlement_key, settlement.settlement_type,
       settlement.clearing_price_units, settlement.demand_units, settlement.supply_units,
       settlement.cleared_units, settlement.unmet_demand_units,
       settlement.excess_supply_units, settlement.gross_amount_units,
       settlement.journal_count, settlement.resource_operation_count,
       settlement.allocation_count, settlement.budget_movement_count,
       settlement.metadata, settlement.created_at, settlement.posted_at
FROM city_market_settlements settlement
JOIN city_monetary_units unit
  ON unit.id = settlement.monetary_unit_id AND unit.world_id = settlement.world_id
`

func scanCityMarketSettlement(row cityScannable) (*CityMarketSettlement, error) {
	item := &CityMarketSettlement{}
	var metadata []byte
	var postedAt sql.NullTime
	if err := row.Scan(
		&item.ID, &item.WorldID, &item.MonetaryUnitID, &item.MonetaryUnitCode,
		&item.MonetaryUnitScale, &item.Tick, &item.Sequence, &item.CycleIndex,
		&item.SettlementKey, &item.SettlementType, &item.ClearingPriceUnits,
		&item.DemandUnits, &item.SupplyUnits, &item.ClearedUnits,
		&item.UnmetDemandUnits, &item.ExcessSupplyUnits, &item.GrossAmountUnits,
		&item.JournalCount, &item.ResourceOperationCount, &item.AllocationCount,
		&item.BudgetMovementCount, &metadata, &item.CreatedAt, &postedAt,
	); err != nil {
		return nil, err
	}
	if !postedAt.Valid {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"settlement_id": strconv.FormatInt(item.ID, 10)})
	}
	item.PostedAt = postedAt.Time
	var err error
	item.Metadata, err = decodeCityJSONMap(metadata)
	if err != nil {
		return nil, fmt.Errorf("decode city market settlement metadata: %w", err)
	}
	return item, nil
}

func loadCityMarketSettlementByCursor(ctx context.Context, queryer citySQLQueryer, worldID, tick int64, sequence int, withDetails bool) (*CityMarketSettlement, error) {
	item, err := scanCityMarketSettlement(queryer.QueryRowContext(ctx, cityMarketSettlementSelect+`
WHERE settlement.world_id = $1 AND settlement.tick = $2 AND settlement.sequence = $3
  AND settlement.posted_at IS NOT NULL`, worldID, tick, sequence))
	if err != nil {
		return nil, err
	}
	if withDetails {
		item.Allocations, err = loadCityMarketAllocations(ctx, queryer, item.ID)
		if err == nil {
			item.BudgetMovements, err = loadCityBudgetMovements(ctx, queryer, item.ID)
		}
	}
	return item, err
}

func loadCityMarketSettlementsForTick(ctx context.Context, queryer citySQLQueryer, worldID, tick int64) ([]*CityMarketSettlement, error) {
	rows, err := queryer.QueryContext(ctx, cityMarketSettlementSelect+`
WHERE settlement.world_id = $1 AND settlement.tick = $2 AND settlement.posted_at IS NOT NULL
ORDER BY settlement.sequence ASC`, worldID, tick)
	if err != nil {
		return nil, fmt.Errorf("load city tick market settlements: %w", err)
	}
	items := make([]*CityMarketSettlement, 0, 4)
	for rows.Next() {
		item, scanErr := scanCityMarketSettlement(rows)
		if scanErr != nil {
			_ = rows.Close()
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err = closeCityRows(rows, "iterate city tick market settlements"); err != nil {
		return nil, err
	}
	for _, item := range items {
		item.Allocations, err = loadCityMarketAllocations(ctx, queryer, item.ID)
		if err != nil {
			return nil, err
		}
		item.BudgetMovements, err = loadCityBudgetMovements(ctx, queryer, item.ID)
		if err != nil {
			return nil, err
		}
	}
	return items, nil
}

func loadCityMarketAllocations(ctx context.Context, queryer citySQLQueryer, settlementID int64) ([]*CityMarketAllocation, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT allocation.id, allocation.settlement_id, allocation.line_no,
       allocation.allocation_type, allocation.cohort_id, cohort.income_band,
       allocation.from_entity_id, from_entity.code,
       allocation.to_entity_id, to_entity.code,
       allocation.district_id, district.code,
       allocation.resource_id, resource.code,
       allocation.quantity_units, allocation.unit_price_units,
       allocation.amount_units, allocation.metadata, allocation.created_at
FROM city_market_allocations allocation
LEFT JOIN city_household_cohorts cohort ON cohort.id = allocation.cohort_id
LEFT JOIN city_economic_entities from_entity ON from_entity.id = allocation.from_entity_id
LEFT JOIN city_economic_entities to_entity ON to_entity.id = allocation.to_entity_id
LEFT JOIN city_districts district ON district.id = allocation.district_id
LEFT JOIN city_resources resource ON resource.id = allocation.resource_id
WHERE allocation.settlement_id = $1
ORDER BY allocation.line_no ASC`, settlementID)
	if err != nil {
		return nil, fmt.Errorf("load city market allocations: %w", err)
	}
	items := make([]*CityMarketAllocation, 0)
	for rows.Next() {
		item := &CityMarketAllocation{}
		var cohortID, fromID, toID, districtID, resourceID sql.NullInt64
		var incomeBand, fromCode, toCode, districtCode, resourceCode sql.NullString
		var metadata []byte
		if err = rows.Scan(&item.ID, &item.SettlementID, &item.LineNo,
			&item.AllocationType, &cohortID, &incomeBand, &fromID, &fromCode,
			&toID, &toCode, &districtID, &districtCode, &resourceID, &resourceCode,
			&item.QuantityUnits, &item.UnitPriceUnits, &item.AmountUnits,
			&metadata, &item.CreatedAt); err != nil {
			_ = rows.Close()
			return nil, err
		}
		item.CohortID = nullInt64Pointer(cohortID)
		item.FromEntityID = nullInt64Pointer(fromID)
		item.ToEntityID = nullInt64Pointer(toID)
		item.DistrictID = nullInt64Pointer(districtID)
		item.ResourceID = nullInt64Pointer(resourceID)
		item.IncomeBand = nullStringPointer(incomeBand)
		item.FromEntityCode = nullStringPointer(fromCode)
		item.ToEntityCode = nullStringPointer(toCode)
		item.DistrictCode = nullStringPointer(districtCode)
		item.ResourceCode = nullStringPointer(resourceCode)
		item.Metadata, err = decodeCityJSONMap(metadata)
		if err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("decode city market allocation metadata: %w", err)
		}
		items = append(items, item)
	}
	if err = closeCityRows(rows, "iterate city market allocations"); err != nil {
		return nil, err
	}
	return items, nil
}

func loadCityBudgetMovements(ctx context.Context, queryer citySQLQueryer, settlementID int64) ([]*CityBudgetMovement, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT movement.id, movement.settlement_id, movement.line_no,
       movement.budget_line_id, budget.code, movement.movement_type,
       movement.amount_units, movement.spent_before_units, movement.spent_after_units,
       movement.budget_version_before, movement.budget_version_after,
       movement.memo, movement.created_at
FROM city_budget_movements movement
JOIN city_government_budget_lines budget ON budget.id = movement.budget_line_id
WHERE movement.settlement_id = $1
ORDER BY movement.line_no ASC`, settlementID)
	if err != nil {
		return nil, fmt.Errorf("load city budget movements: %w", err)
	}
	items := make([]*CityBudgetMovement, 0)
	for rows.Next() {
		item := &CityBudgetMovement{}
		if err = rows.Scan(&item.ID, &item.SettlementID, &item.LineNo,
			&item.BudgetLineID, &item.BudgetCode, &item.MovementType,
			&item.AmountUnits, &item.SpentBeforeUnits, &item.SpentAfterUnits,
			&item.BudgetVersionBefore, &item.BudgetVersionAfter,
			&item.Memo, &item.CreatedAt); err != nil {
			_ = rows.Close()
			return nil, err
		}
		items = append(items, item)
	}
	if err = closeCityRows(rows, "iterate city budget movements"); err != nil {
		return nil, err
	}
	return items, nil
}

func loadCityMarketOverview(ctx context.Context, queryer citySQLQueryer, worldID int64) (*CityMarketOverview, error) {
	overview := &CityMarketOverview{
		WorldID: worldID, Markets: make([]*CityMarketState, 0, 3),
		Occupancies: make([]*CityHousingOccupancy, 0),
	}
	if err := queryer.QueryRowContext(ctx, `SELECT current_tick FROM city_worlds WHERE id = $1`, worldID).Scan(&overview.AsOfTick); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrCityWorldNotFound
		}
		return nil, fmt.Errorf("load city market overview tick: %w", err)
	}
	cycle := &CityEconomicCycleState{}
	var cycleMetadata []byte
	var lastSettledTick sql.NullInt64
	if err := queryer.QueryRowContext(ctx, `
SELECT world_id, cycle_index, cadence_ticks, next_due_tick, last_settled_tick,
       version, metadata, created_at, updated_at
FROM city_economic_cycle_states WHERE world_id = $1`, worldID).Scan(
		&cycle.WorldID, &cycle.CycleIndex, &cycle.CadenceTicks, &cycle.NextDueTick,
		&lastSettledTick, &cycle.Version, &cycleMetadata, &cycle.CreatedAt, &cycle.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("load city economic cycle state: %w", err)
	}
	cycle.LastSettledTick = nullInt64Pointer(lastSettledTick)
	var err error
	cycle.Metadata, err = decodeCityJSONMap(cycleMetadata)
	if err != nil {
		return nil, fmt.Errorf("decode city economic cycle metadata: %w", err)
	}
	overview.Cycle = cycle

	policy := &CityEconomicPolicy{}
	var policyMetadata []byte
	if err = queryer.QueryRowContext(ctx, `
SELECT world_id, labor_demand_capacity_milli, goods_demand_population_divisor,
       household_wage_tax_milli, firm_sales_tax_milli,
       procurement_share_milli, social_support_share_milli,
       version, metadata, created_at, updated_at
FROM city_economic_policies WHERE world_id = $1`, worldID).Scan(
		&policy.WorldID, &policy.LaborDemandCapacityMilli,
		&policy.GoodsDemandPopulationDivisor, &policy.HouseholdWageTaxMilli,
		&policy.FirmSalesTaxMilli, &policy.ProcurementShareMilli,
		&policy.SocialSupportShareMilli, &policy.Version, &policyMetadata,
		&policy.CreatedAt, &policy.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("load city economic policy: %w", err)
	}
	policy.Metadata, err = decodeCityJSONMap(policyMetadata)
	if err != nil {
		return nil, fmt.Errorf("decode city economic policy metadata: %w", err)
	}
	overview.Policy = policy

	rows, err := queryer.QueryContext(ctx, `
SELECT market.id, market.world_id, market.monetary_unit_id, unit.code,
       market.resource_id, resource.code, market.market_code, market.quote_units,
       market.floor_units, market.ceiling_units, market.maximum_adjustment_milli,
       market.last_clearing_tick, market.last_clearing_price_units,
       market.last_demand_units, market.last_supply_units, market.last_cleared_units,
       market.last_unmet_demand_units, market.last_excess_supply_units,
       market.version, market.metadata, market.created_at, market.updated_at
FROM city_market_states market
JOIN city_monetary_units unit ON unit.id = market.monetary_unit_id
LEFT JOIN city_resources resource ON resource.id = market.resource_id
WHERE market.world_id = $1
ORDER BY CASE market.market_code WHEN 'labor' THEN 1 WHEN 'basic_goods' THEN 2 ELSE 3 END`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load city market states: %w", err)
	}
	for rows.Next() {
		item := &CityMarketState{}
		var resourceID, lastTick, lastPrice sql.NullInt64
		var resourceCode sql.NullString
		var metadata []byte
		if err = rows.Scan(&item.ID, &item.WorldID, &item.MonetaryUnitID,
			&item.MonetaryUnitCode, &resourceID, &resourceCode, &item.MarketCode,
			&item.QuoteUnits, &item.FloorUnits, &item.CeilingUnits,
			&item.MaximumAdjustmentMilli, &lastTick, &lastPrice,
			&item.LastDemandUnits, &item.LastSupplyUnits, &item.LastClearedUnits,
			&item.LastUnmetDemandUnits, &item.LastExcessSupplyUnits,
			&item.Version, &metadata, &item.CreatedAt, &item.UpdatedAt); err != nil {
			_ = rows.Close()
			return nil, err
		}
		item.ResourceID = nullInt64Pointer(resourceID)
		item.ResourceCode = nullStringPointer(resourceCode)
		item.LastClearingTick = nullInt64Pointer(lastTick)
		item.LastClearingPriceUnits = nullInt64Pointer(lastPrice)
		item.Metadata, err = decodeCityJSONMap(metadata)
		if err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("decode city market state metadata: %w", err)
		}
		overview.Markets = append(overview.Markets, item)
	}
	if err = closeCityRows(rows, "iterate city market states"); err != nil {
		return nil, err
	}

	rows, err = queryer.QueryContext(ctx, `
SELECT occupancy.id, occupancy.world_id, occupancy.cohort_id, cohort.income_band,
       occupancy.district_id, district.code, occupancy.occupied_units,
       occupancy.unmet_units, occupancy.rent_price_units,
       occupancy.last_settled_tick, occupancy.version, occupancy.metadata,
       occupancy.created_at, occupancy.updated_at
FROM city_housing_occupancies occupancy
JOIN city_household_cohorts cohort ON cohort.id = occupancy.cohort_id
JOIN city_districts district ON district.id = occupancy.district_id
WHERE occupancy.world_id = $1
ORDER BY district.sort_order ASC,
         CASE cohort.income_band WHEN 'low' THEN 1 WHEN 'middle' THEN 2 ELSE 3 END`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load city housing occupancies: %w", err)
	}
	for rows.Next() {
		item := &CityHousingOccupancy{}
		var lastTick sql.NullInt64
		var metadata []byte
		if err = rows.Scan(&item.ID, &item.WorldID, &item.CohortID, &item.IncomeBand,
			&item.DistrictID, &item.DistrictCode, &item.OccupiedUnits,
			&item.UnmetUnits, &item.RentPriceUnits, &lastTick, &item.Version,
			&metadata, &item.CreatedAt, &item.UpdatedAt); err != nil {
			_ = rows.Close()
			return nil, err
		}
		item.LastSettledTick = nullInt64Pointer(lastTick)
		item.Metadata, err = decodeCityJSONMap(metadata)
		if err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("decode city housing occupancy metadata: %w", err)
		}
		overview.Occupancies = append(overview.Occupancies, item)
	}
	if err = closeCityRows(rows, "iterate city housing occupancies"); err != nil {
		return nil, err
	}
	if len(overview.Markets) != 3 || len(overview.Occupancies) == 0 {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "market_overview"})
	}
	return overview, nil
}

func nullStringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

type cityAllocationWeight struct {
	key       int64
	weight    int64
	remainder *big.Int
	index     int
}

func cityProportionalAllocation(total int64, weights []cityAllocationWeight) (map[int64]int64, error) {
	if total < 0 {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "allocation_total"})
	}
	result := make(map[int64]int64, len(weights))
	var weightTotal int64
	for index := range weights {
		if weights[index].key <= 0 || weights[index].weight < 0 {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "allocation_weight"})
		}
		if _, duplicate := result[weights[index].key]; duplicate {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "allocation_key"})
		}
		// A zero allocation is still a projection update. Keeping every key in
		// the result lets callers clear stale quantities when demand or
		// affordability falls to zero.
		result[weights[index].key] = 0
		var err error
		weightTotal, err = addCityLedgerUnits(weightTotal, weights[index].weight)
		if err != nil {
			return nil, err
		}
		weights[index].index = index
	}
	if total == 0 || weightTotal == 0 {
		return result, nil
	}
	var allocated int64
	denominator := big.NewInt(weightTotal)
	for index := range weights {
		product := new(big.Int).Mul(big.NewInt(weights[index].weight), big.NewInt(total))
		quotient, remainder := new(big.Int), new(big.Int)
		quotient.QuoRem(product, denominator, remainder)
		if !quotient.IsInt64() {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "allocation_overflow"})
		}
		value := quotient.Int64()
		result[weights[index].key] = value
		allocated += value
		weights[index].remainder = remainder
	}
	sort.SliceStable(weights, func(i, j int) bool {
		comparison := weights[i].remainder.Cmp(weights[j].remainder)
		if comparison != 0 {
			return comparison > 0
		}
		return weights[i].index < weights[j].index
	})
	remaining := total - allocated
	if remaining < 0 || remaining > int64(len(weights)) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "allocation_remainder"})
	}
	for index := int64(0); index < remaining; index++ {
		result[weights[index].key]++
	}
	return result, nil
}

func cityMultiplyUnits(left, right int64) (int64, error) {
	if left < 0 || right < 0 || left != 0 && right > math.MaxInt64/left {
		return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "quantity_amount_overflow"})
	}
	return left * right, nil
}

func cityMulDivFloor(value int64, multiplier int, divisor int64) (int64, error) {
	if value < 0 || multiplier < 0 || divisor <= 0 {
		return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "ratio"})
	}
	product := new(big.Int).Mul(big.NewInt(value), big.NewInt(int64(multiplier)))
	quotient := new(big.Int).Quo(product, big.NewInt(divisor))
	if !quotient.IsInt64() {
		return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "ratio_overflow"})
	}
	return quotient.Int64(), nil
}

func cityDivideRoundUp(value, divisor int64) (int64, error) {
	if value < 0 || divisor <= 0 {
		return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "division"})
	}
	quotient := value / divisor
	if value%divisor != 0 {
		if quotient == math.MaxInt64 {
			return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "division_overflow"})
		}
		quotient++
	}
	return quotient, nil
}

func cityNextMarketQuote(current, floor, ceiling int64, maximumAdjustmentMilli int, demand, supply int64) (int64, error) {
	if current <= 0 || floor <= 0 || ceiling < floor || current < floor || current > ceiling ||
		maximumAdjustmentMilli <= 0 || maximumAdjustmentMilli > 500 || demand < 0 || supply < 0 {
		return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "market_quote"})
	}
	if demand == supply {
		return current, nil
	}
	base := demand
	if supply > base {
		base = supply
	}
	if base == 0 {
		return current, nil
	}
	difference := new(big.Int).Sub(big.NewInt(demand), big.NewInt(supply))
	direction := difference.Sign()
	difference.Abs(difference)
	change := new(big.Int).Mul(difference, big.NewInt(int64(maximumAdjustmentMilli)))
	change.Quo(change, big.NewInt(base))
	if change.Sign() == 0 {
		change.SetInt64(1)
	}
	if change.Cmp(big.NewInt(int64(maximumAdjustmentMilli))) > 0 {
		change.SetInt64(int64(maximumAdjustmentMilli))
	}
	factor := int64(1000)
	if direction > 0 {
		factor += change.Int64()
	} else {
		factor -= change.Int64()
	}
	numerator := new(big.Int).Mul(big.NewInt(current), big.NewInt(factor))
	numerator.Add(numerator, big.NewInt(500))
	next := new(big.Int).Quo(numerator, big.NewInt(1000))
	if !next.IsInt64() {
		return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "market_quote_overflow"})
	}
	value := next.Int64()
	if value < floor {
		value = floor
	}
	if value > ceiling {
		value = ceiling
	}
	return value, nil
}

type cityCycleStateRef struct {
	cycleIndex   int64
	cadenceTicks int
	nextDueTick  int64
	version      int64
}

type cityPolicyRef struct {
	laborDemandCapacityMilli     int
	goodsDemandPopulationDivisor int64
	householdWageTaxMilli        int
	firmSalesTaxMilli            int
	procurementShareMilli        int
	socialSupportShareMilli      int
}

type cityMarketStateRef struct {
	id                     int64
	marketCode             string
	resourceID             *int64
	quoteUnits             int64
	floorUnits             int64
	ceilingUnits           int64
	maximumAdjustmentMilli int
	version                int64
}

type cityMarketCohortRef struct {
	id                 int64
	districtID         int64
	districtCode       string
	incomeBand         string
	populationUnits    int64
	workingAgeUnits    int64
	housingDemandUnits int64
	version            int64
}

type cityMarketFirmRef struct {
	entityID           int64
	districtID         int64
	districtCode       string
	employeeUnits      int64
	productionCapacity int64
	version            int64
}

type cityMarketOccupancyRef struct {
	id       int64
	cohortID int64
	version  int64
}

type cityBudgetLineRef struct {
	id                int64
	code              string
	appropriatedUnits int64
	committedUnits    int64
	spentUnits        int64
	version           int64
}

type cityMarketAllocationPlan struct {
	allocationType string
	cohortID       *int64
	fromEntityID   *int64
	toEntityID     *int64
	districtID     *int64
	resourceID     *int64
	quantityUnits  int64
	unitPriceUnits int64
	amountUnits    int64
	metadata       map[string]any
}

type cityLaborProjectionPlan struct {
	firmEntityID   int64
	firmVersion    int64
	employeeUnits  int64
	cohortVersions map[int64]int64
	cohortUnits    map[int64]int64
}

type cityHousingProjectionPlan struct {
	occupancyID    int64
	version        int64
	occupiedUnits  int64
	unmetUnits     int64
	rentPriceUnits int64
}

type cityBudgetSpendPlan struct {
	budgetLineID int64
	version      int64
	amountUnits  int64
	memo         string
}

type cityMarketSettlementPlan struct {
	settlementType string
	sequence       int
	cycleIndex     int64
	settlementKey  string
	clearingPrice  int64
	demandUnits    int64
	supplyUnits    int64
	clearedUnits   int64
	unmetUnits     int64
	excessUnits    int64
	grossAmount    int64
	metadata       map[string]any
	allocations    []cityMarketAllocationPlan
	journalSpecs   []cityLedgerJournalSpec
	resourceSpecs  []cityResourceOperationSpec
	marketState    *cityMarketStateRef
	nextQuote      int64
	labor          *cityLaborProjectionPlan
	housing        []cityHousingProjectionPlan
	budgetSpends   []cityBudgetSpendPlan
}

type cityMarketCyclePlan struct {
	worldID      int64
	tick         int64
	cycleBefore  int64
	cycleIndex   int64
	cadenceTicks int
	unit         *cityLedgerBaseUnit
	settlements  []*cityMarketSettlementPlan
}

type cityMarketCycleEvent struct {
	eventType string
	payload   map[string]any
}

type cityMarketCycleExecution struct {
	settlements          []*CityMarketSettlement
	events               []cityMarketCycleEvent
	nextJournalSequence  int64
	nextResourceSequence int64
}

func cityEconomyCycleDue(ctx context.Context, tx *sql.Tx, worldID, targetTick int64) (bool, error) {
	var nextDueTick int64
	if err := tx.QueryRowContext(ctx, `
SELECT next_due_tick FROM city_economic_cycle_states
WHERE world_id = $1 FOR UPDATE`, worldID).Scan(&nextDueTick); err != nil {
		return false, fmt.Errorf("load city economic cycle due tick: %w", err)
	}
	return targetTick >= nextDueTick, nil
}

func (s *CityEconomyService) settleCityEconomicCycle(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, journalSequence, resourceSequence int64,
	unit *cityLedgerBaseUnit,
) (*cityMarketCycleExecution, error) {
	plan, err := s.buildCityMarketCyclePlan(ctx, tx, worldID, targetTick, unit)
	if err != nil {
		return nil, err
	}
	return executeCityMarketCyclePlan(ctx, tx, plan, journalSequence, resourceSequence)
}

func (s *CityEconomyService) buildCityMarketCyclePlan(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	unit *cityLedgerBaseUnit,
) (*cityMarketCyclePlan, error) {
	if unit == nil {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "market_base_unit"})
	}
	cycle := cityCycleStateRef{}
	if err := tx.QueryRowContext(ctx, `
SELECT cycle_index, cadence_ticks, next_due_tick, version
FROM city_economic_cycle_states WHERE world_id = $1 FOR UPDATE`, worldID).Scan(
		&cycle.cycleIndex, &cycle.cadenceTicks, &cycle.nextDueTick, &cycle.version,
	); err != nil {
		return nil, fmt.Errorf("load city economic cycle: %w", err)
	}
	if targetTick < cycle.nextDueTick {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "market_cycle_not_due"})
	}
	policy := cityPolicyRef{}
	if err := tx.QueryRowContext(ctx, `
SELECT labor_demand_capacity_milli, goods_demand_population_divisor,
       household_wage_tax_milli, firm_sales_tax_milli,
       procurement_share_milli, social_support_share_milli
FROM city_economic_policies WHERE world_id = $1 FOR UPDATE`, worldID).Scan(
		&policy.laborDemandCapacityMilli, &policy.goodsDemandPopulationDivisor,
		&policy.householdWageTaxMilli, &policy.firmSalesTaxMilli,
		&policy.procurementShareMilli, &policy.socialSupportShareMilli,
	); err != nil {
		return nil, fmt.Errorf("load city economic policy: %w", err)
	}
	marketStates, err := loadCityMarketStateRefs(ctx, tx, worldID)
	if err != nil {
		return nil, err
	}
	cohorts, err := loadCityMarketCohorts(ctx, tx, worldID)
	if err != nil {
		return nil, err
	}
	firm, err := loadCityMarketFirm(ctx, tx, worldID)
	if err != nil {
		return nil, err
	}
	householdID, governmentID, err := loadCityMarketEntityIDs(ctx, tx, worldID)
	if err != nil {
		return nil, err
	}
	occupancies, err := loadCityMarketOccupancyRefs(ctx, tx, worldID)
	if err != nil {
		return nil, err
	}
	budgets, err := loadCityBudgetLineRefs(ctx, tx, worldID)
	if err != nil {
		return nil, err
	}
	accounts, err := loadCityMarketAccounts(ctx, tx, worldID, unit, householdID, firm.entityID, governmentID)
	if err != nil {
		return nil, err
	}

	plan := &cityMarketCyclePlan{
		worldID: worldID, tick: targetTick, cycleBefore: cycle.cycleIndex,
		cycleIndex: cycle.cycleIndex + 1, cadenceTicks: cycle.cadenceTicks, unit: unit,
		settlements: make([]*cityMarketSettlementPlan, 0, 4),
	}
	virtualCash := map[int64]int64{
		householdID:   accounts["household.cash"].balanceUnits,
		firm.entityID: accounts["firm.cash"].balanceUnits,
		governmentID:  accounts["government.cash"].balanceUnits,
	}

	labor, wageAmount, err := buildCityLaborSettlementPlan(
		plan, marketStates[CityMarketLabor], policy, cohorts, firm,
		householdID, accounts, virtualCash,
	)
	if err != nil {
		return nil, err
	}
	plan.settlements = append(plan.settlements, labor)

	goods, goodsAmount, err := buildCityGoodsSettlementPlan(
		ctx, tx, plan, marketStates[CityMarketBasicGoods], policy, cohorts, firm,
		householdID, accounts, virtualCash,
	)
	if err != nil {
		return nil, err
	}
	plan.settlements = append(plan.settlements, goods)

	housing, err := buildCityHousingSettlementPlan(
		ctx, tx, plan, marketStates[CityMarketHousing], cohorts, occupancies,
		householdID, governmentID, accounts, virtualCash,
	)
	if err != nil {
		return nil, err
	}
	plan.settlements = append(plan.settlements, housing)

	fiscal, err := buildCityFiscalSettlementPlan(
		plan, policy, budgets, householdID, firm.entityID, governmentID,
		wageAmount, goodsAmount, accounts, virtualCash,
	)
	if err != nil {
		return nil, err
	}
	plan.settlements = append(plan.settlements, fiscal)
	if len(plan.settlements) != 4 {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "market_settlement_count"})
	}
	return plan, nil
}

func loadCityMarketStateRefs(ctx context.Context, tx *sql.Tx, worldID int64) (map[string]*cityMarketStateRef, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT id, market_code, resource_id, quote_units, floor_units, ceiling_units,
       maximum_adjustment_milli, version
FROM city_market_states WHERE world_id = $1
ORDER BY market_code ASC FOR UPDATE`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load city market state refs: %w", err)
	}
	items := make(map[string]*cityMarketStateRef, 3)
	for rows.Next() {
		item := &cityMarketStateRef{}
		var resourceID sql.NullInt64
		if err = rows.Scan(&item.id, &item.marketCode, &resourceID, &item.quoteUnits,
			&item.floorUnits, &item.ceilingUnits, &item.maximumAdjustmentMilli,
			&item.version); err != nil {
			_ = rows.Close()
			return nil, err
		}
		item.resourceID = nullInt64Pointer(resourceID)
		items[item.marketCode] = item
	}
	if err = closeCityRows(rows, "iterate city market state refs"); err != nil {
		return nil, err
	}
	for _, code := range []string{CityMarketLabor, CityMarketBasicGoods, CityMarketHousing} {
		if items[code] == nil {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"market_code": code})
		}
	}
	return items, nil
}

func loadCityMarketCohorts(ctx context.Context, tx *sql.Tx, worldID int64) ([]cityMarketCohortRef, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT cohort.id, cohort.district_id, district.code, cohort.income_band,
       cohort.population_units, cohort.working_age_units,
       cohort.housing_demand_units, cohort.version
FROM city_household_cohorts cohort
JOIN city_districts district ON district.id = cohort.district_id
WHERE cohort.world_id = $1
ORDER BY district.sort_order ASC,
         CASE cohort.income_band WHEN 'low' THEN 1 WHEN 'middle' THEN 2 ELSE 3 END
FOR UPDATE OF cohort`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load city market cohorts: %w", err)
	}
	items := make([]cityMarketCohortRef, 0, 18)
	for rows.Next() {
		var item cityMarketCohortRef
		if err = rows.Scan(&item.id, &item.districtID, &item.districtCode,
			&item.incomeBand, &item.populationUnits, &item.workingAgeUnits,
			&item.housingDemandUnits, &item.version); err != nil {
			_ = rows.Close()
			return nil, err
		}
		items = append(items, item)
	}
	if err = closeCityRows(rows, "iterate city market cohorts"); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "market_cohorts"})
	}
	return items, nil
}

func loadCityMarketFirm(ctx context.Context, tx *sql.Tx, worldID int64) (*cityMarketFirmRef, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT firm.entity_id, firm.district_id, district.code, firm.employee_units,
       firm.production_capacity_units, firm.version
FROM city_firm_states firm
JOIN city_districts district ON district.id = firm.district_id
WHERE firm.world_id = $1
ORDER BY firm.entity_id ASC
LIMIT 2 FOR UPDATE OF firm`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load city market firm: %w", err)
	}
	items := make([]cityMarketFirmRef, 0, 2)
	for rows.Next() {
		var item cityMarketFirmRef
		if err = rows.Scan(&item.entityID, &item.districtID, &item.districtCode,
			&item.employeeUnits, &item.productionCapacity, &item.version); err != nil {
			_ = rows.Close()
			return nil, err
		}
		items = append(items, item)
	}
	if err = closeCityRows(rows, "iterate city market firms"); err != nil {
		return nil, err
	}
	if len(items) != 1 {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "market_firm_count"})
	}
	return &items[0], nil
}

func loadCityMarketEntityIDs(ctx context.Context, tx *sql.Tx, worldID int64) (int64, int64, error) {
	var householdID, governmentID int64
	rows, err := tx.QueryContext(ctx, `
SELECT id, entity_type FROM city_economic_entities
WHERE world_id = $1 AND status = 'active' AND entity_type IN ('household', 'government')
ORDER BY entity_type ASC, code ASC FOR SHARE`, worldID)
	if err != nil {
		return 0, 0, fmt.Errorf("load city market entities: %w", err)
	}
	counts := map[string]int{}
	for rows.Next() {
		var id int64
		var entityType string
		if err = rows.Scan(&id, &entityType); err != nil {
			_ = rows.Close()
			return 0, 0, err
		}
		counts[entityType]++
		if entityType == CityEntityTypeHousehold {
			householdID = id
		} else {
			governmentID = id
		}
	}
	if err = closeCityRows(rows, "iterate city market entities"); err != nil {
		return 0, 0, err
	}
	if counts[CityEntityTypeHousehold] != 1 || counts[CityEntityTypeGovernment] != 1 {
		return 0, 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "market_entity_count"})
	}
	return householdID, governmentID, nil
}

func loadCityMarketOccupancyRefs(ctx context.Context, tx *sql.Tx, worldID int64) (map[int64]cityMarketOccupancyRef, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT id, cohort_id, version FROM city_housing_occupancies
WHERE world_id = $1 ORDER BY cohort_id ASC FOR UPDATE`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load city occupancy refs: %w", err)
	}
	items := make(map[int64]cityMarketOccupancyRef)
	for rows.Next() {
		var item cityMarketOccupancyRef
		if err = rows.Scan(&item.id, &item.cohortID, &item.version); err != nil {
			_ = rows.Close()
			return nil, err
		}
		items[item.cohortID] = item
	}
	if err = closeCityRows(rows, "iterate city occupancy refs"); err != nil {
		return nil, err
	}
	return items, nil
}

func loadCityBudgetLineRefs(ctx context.Context, tx *sql.Tx, worldID int64) (map[string]cityBudgetLineRef, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT id, code, appropriated_units, committed_units, spent_units, version
FROM city_government_budget_lines WHERE world_id = $1
ORDER BY code ASC FOR UPDATE`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load city budget line refs: %w", err)
	}
	items := make(map[string]cityBudgetLineRef)
	for rows.Next() {
		var item cityBudgetLineRef
		if err = rows.Scan(&item.id, &item.code, &item.appropriatedUnits,
			&item.committedUnits, &item.spentUnits, &item.version); err != nil {
			_ = rows.Close()
			return nil, err
		}
		items[item.code] = item
	}
	if err = closeCityRows(rows, "iterate city budget line refs"); err != nil {
		return nil, err
	}
	for _, code := range []string{"healthcare", "social_protection"} {
		if _, ok := items[code]; !ok {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"budget_code": code})
		}
	}
	return items, nil
}

func loadCityMarketAccounts(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	unit *cityLedgerBaseUnit,
	householdID, firmID, governmentID int64,
) (map[string]*cityLedgerAccountRef, error) {
	type accountRequest struct {
		key, entityType, code string
		entityID              int64
	}
	requests := []accountRequest{
		{"household.cash", CityEntityTypeHousehold, "cash", householdID},
		{"household.wage_income", CityEntityTypeHousehold, "wage_income", householdID},
		{"household.consumption_expense", CityEntityTypeHousehold, "consumption_expense", householdID},
		{"household.rent_expense", CityEntityTypeHousehold, "rent_expense", householdID},
		{"household.tax_expense", CityEntityTypeHousehold, "tax_expense", householdID},
		{"household.other_income", CityEntityTypeHousehold, "other_income", householdID},
		{"firm.cash", CityEntityTypeFirm, "cash", firmID},
		{"firm.wage_expense", CityEntityTypeFirm, "wage_expense", firmID},
		{"firm.revenue", CityEntityTypeFirm, "revenue", firmID},
		{"firm.tax_expense", CityEntityTypeFirm, "tax_expense", firmID},
		{"government.cash", CityEntityTypeGovernment, "cash", governmentID},
		{"government.tax_revenue", CityEntityTypeGovernment, "tax_revenue", governmentID},
		{"government.rental_revenue", CityEntityTypeGovernment, "rental_revenue", governmentID},
		{"government.public_service_expense", CityEntityTypeGovernment, "public_service_expense", governmentID},
		{"government.subsidy_expense", CityEntityTypeGovernment, "subsidy_expense", governmentID},
	}
	items := make(map[string]*cityLedgerAccountRef, len(requests))
	for _, request := range requests {
		account, err := loadCityLedgerAccount(ctx, tx, worldID, unit.id, request.entityID, request.entityType, request.code)
		if err != nil {
			return nil, err
		}
		items[request.key] = account
	}
	return items, nil
}

func cityAccount(accounts map[string]*cityLedgerAccountRef, key string) (*cityLedgerAccountRef, error) {
	account := accounts[key]
	if account == nil {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"account": key})
	}
	return account, nil
}

func cityMinimum(values ...int64) int64 {
	if len(values) == 0 {
		return 0
	}
	result := values[0]
	for _, value := range values[1:] {
		if value < result {
			result = value
		}
	}
	return result
}

func cityApplyVirtualTransfer(cash map[int64]int64, fromID, toID, amount int64) error {
	if amount < 0 || cash[fromID] < amount {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "market_cash"})
	}
	cash[fromID] -= amount
	next, err := addCityLedgerUnits(cash[toID], amount)
	if err != nil {
		return err
	}
	cash[toID] = next
	return nil
}

func citySettlementGross(allocations []cityMarketAllocationPlan) (int64, error) {
	var total int64
	for _, allocation := range allocations {
		var err error
		total, err = addCityLedgerUnits(total, allocation.amountUnits)
		if err != nil {
			return 0, err
		}
	}
	return total, nil
}

func buildCityLaborSettlementPlan(
	cycle *cityMarketCyclePlan,
	market *cityMarketStateRef,
	policy cityPolicyRef,
	cohorts []cityMarketCohortRef,
	firm *cityMarketFirmRef,
	householdID int64,
	accounts map[string]*cityLedgerAccountRef,
	virtualCash map[int64]int64,
) (*cityMarketSettlementPlan, int64, error) {
	if market == nil || firm == nil {
		return nil, 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "labor_market"})
	}
	var supply int64
	weights := make([]cityAllocationWeight, 0, len(cohorts))
	cohortVersions := make(map[int64]int64, len(cohorts))
	for _, cohort := range cohorts {
		var err error
		supply, err = addCityLedgerUnits(supply, cohort.workingAgeUnits)
		if err != nil {
			return nil, 0, err
		}
		weights = append(weights, cityAllocationWeight{key: cohort.id, weight: cohort.workingAgeUnits})
		cohortVersions[cohort.id] = cohort.version
	}
	demand, err := cityMulDivFloor(firm.productionCapacity, policy.laborDemandCapacityMilli, 1000)
	if err != nil {
		return nil, 0, err
	}
	affordable := virtualCash[firm.entityID] / market.quoteUnits
	cleared := cityMinimum(supply, demand, affordable)
	if cleared < 0 {
		return nil, 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "labor_cleared"})
	}
	wageAmount, err := cityMultiplyUnits(cleared, market.quoteUnits)
	if err != nil {
		return nil, 0, err
	}
	cohortUnits, err := cityProportionalAllocation(cleared, weights)
	if err != nil {
		return nil, 0, err
	}
	allocations := make([]cityMarketAllocationPlan, 0, len(cohorts))
	for _, cohort := range cohorts {
		quantity := cohortUnits[cohort.id]
		if quantity == 0 {
			continue
		}
		amount, multiplyErr := cityMultiplyUnits(quantity, market.quoteUnits)
		if multiplyErr != nil {
			return nil, 0, multiplyErr
		}
		cohortID, districtID := cohort.id, cohort.districtID
		fromID, toID := householdID, firm.entityID
		allocations = append(allocations, cityMarketAllocationPlan{
			allocationType: "employment", cohortID: &cohortID,
			fromEntityID: &fromID, toEntityID: &toID, districtID: &districtID,
			quantityUnits: quantity, unitPriceUnits: market.quoteUnits, amountUnits: amount,
			metadata: map[string]any{"income_band": cohort.incomeBand, "schema_version": 1},
		})
	}
	journalSpecs := make([]cityLedgerJournalSpec, 0, 1)
	if wageAmount > 0 {
		journalSpecs = append(journalSpecs, cityLedgerJournalSpec{
			worldID: cycle.worldID, unit: cycle.unit, tick: cycle.tick,
			operationKey: fmt.Sprintf("market:v1:%d:labor:wage", cycle.cycleIndex),
			journalType:  "wage", description: "Economic cycle wage settlement",
			metadata: map[string]any{
				"cycle_index": cycle.cycleIndex, "employee_units": cleared,
				"wage_units": market.quoteUnits, "schema_version": 1,
			},
			lines: []cityLedgerPostingLine{
				{account: accounts["firm.wage_expense"], debitUnits: wageAmount, memo: "Cycle wages"},
				{account: accounts["household.cash"], debitUnits: wageAmount, memo: "Cycle wages"},
				{account: accounts["firm.cash"], creditUnits: wageAmount, memo: "Cycle wages"},
				{account: accounts["household.wage_income"], creditUnits: wageAmount, memo: "Cycle wages"},
			},
		})
		if err = cityApplyVirtualTransfer(virtualCash, firm.entityID, householdID, wageAmount); err != nil {
			return nil, 0, err
		}
	}
	nextQuote, err := cityNextMarketQuote(
		market.quoteUnits, market.floorUnits, market.ceilingUnits,
		market.maximumAdjustmentMilli, demand, supply,
	)
	if err != nil {
		return nil, 0, err
	}
	gross, err := citySettlementGross(allocations)
	if err != nil || gross != wageAmount {
		if err != nil {
			return nil, 0, err
		}
		return nil, 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "labor_gross"})
	}
	return &cityMarketSettlementPlan{
		settlementType: CityMarketLabor, sequence: 1, cycleIndex: cycle.cycleIndex,
		settlementKey: fmt.Sprintf("cycle:v1:%d:labor", cycle.cycleIndex),
		clearingPrice: market.quoteUnits, demandUnits: demand, supplyUnits: supply,
		clearedUnits: cleared, unmetUnits: demand - cleared, excessUnits: supply - cleared,
		grossAmount: gross, allocations: allocations, journalSpecs: journalSpecs,
		resourceSpecs: make([]cityResourceOperationSpec, 0), marketState: market,
		nextQuote: nextQuote,
		labor: &cityLaborProjectionPlan{
			firmEntityID: firm.entityID, firmVersion: firm.version, employeeUnits: cleared,
			cohortVersions: cohortVersions, cohortUnits: cohortUnits,
		},
		metadata: map[string]any{
			"schema_version": 1, "affordable_units": affordable,
			"next_quote_units": nextQuote,
		},
	}, wageAmount, nil
}

func buildCityGoodsSettlementPlan(
	ctx context.Context,
	tx *sql.Tx,
	cycle *cityMarketCyclePlan,
	market *cityMarketStateRef,
	policy cityPolicyRef,
	cohorts []cityMarketCohortRef,
	firm *cityMarketFirmRef,
	householdID int64,
	accounts map[string]*cityLedgerAccountRef,
	virtualCash map[int64]int64,
) (*cityMarketSettlementPlan, int64, error) {
	if market == nil || market.resourceID == nil {
		return nil, 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "goods_market"})
	}
	var population int64
	for _, cohort := range cohorts {
		var err error
		population, err = addCityLedgerUnits(population, cohort.populationUnits)
		if err != nil {
			return nil, 0, err
		}
	}
	demand, err := cityDivideRoundUp(population, policy.goodsDemandPopulationDivisor)
	if err != nil {
		return nil, 0, err
	}
	firmGoods, err := ensureCityInventoryRef(ctx, tx, cycle.worldID, firm.entityID, firm.districtCode, "consumer_goods")
	if err != nil {
		return nil, 0, err
	}
	householdGoods, err := ensureCityInventoryRef(ctx, tx, cycle.worldID, householdID, firm.districtCode, "consumer_goods")
	if err != nil {
		return nil, 0, err
	}
	if firmGoods.resourceID != *market.resourceID || householdGoods.resourceID != *market.resourceID {
		return nil, 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "goods_resource"})
	}
	recipe, err := loadCityRecipeExecution(ctx, tx, cycle.worldID, firm.entityID, firm.districtCode, "basic_goods")
	if err != nil {
		return nil, 0, err
	}
	var usedCapacity int64
	if err = tx.QueryRowContext(ctx, `
SELECT COALESCE(SUM(operation.batch_count * recipe.capacity_units_per_batch), 0)::BIGINT
FROM city_resource_operations operation
JOIN city_production_recipes recipe ON recipe.id = operation.recipe_id
WHERE operation.world_id = $1 AND operation.tick = $2
  AND operation.actor_entity_id = $3 AND operation.operation_type = 'production'
  AND operation.posted_at IS NOT NULL`, cycle.worldID, cycle.tick, firm.entityID).Scan(&usedCapacity); err != nil {
		return nil, 0, fmt.Errorf("load city market production capacity: %w", err)
	}
	remainingCapacity := recipe.productionCapacity - usedCapacity
	if remainingCapacity < 0 {
		return nil, 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "production_capacity_projection"})
	}
	maxBatches := remainingCapacity / recipe.capacityUnitsPerBatch
	balances := make(map[string]*cityInventoryRef, len(recipe.lines))
	var outputPerBatch int64
	for _, line := range recipe.lines {
		balance, loadErr := ensureCityInventoryRef(ctx, tx, cycle.worldID, firm.entityID, firm.districtCode, line.resourceCode)
		if loadErr != nil {
			return nil, 0, loadErr
		}
		balances[line.resourceCode] = balance
		if line.direction == "input" {
			maxBatches = cityMinimum(maxBatches, balance.quantityUnits/line.quantityUnits)
		} else if line.resourceCode == "consumer_goods" {
			outputPerBatch = line.quantityUnits
		}
	}
	if outputPerBatch <= 0 {
		return nil, 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "goods_recipe_output"})
	}
	shortage := demand - firmGoods.quantityUnits
	if shortage < 0 {
		shortage = 0
	}
	batchesForDemand, err := cityDivideRoundUp(shortage, outputPerBatch)
	if err != nil {
		return nil, 0, err
	}
	batchCount := cityMinimum(maxBatches, batchesForDemand)
	resourceSpecs := make([]cityResourceOperationSpec, 0, 3)
	projectedSupply := firmGoods.quantityUnits
	if batchCount > 0 {
		lines := make([]cityResourcePostingLine, 0, len(recipe.lines))
		for _, recipeLine := range recipe.lines {
			quantity, multiplyErr := cityMultiplyUnits(recipeLine.quantityUnits, batchCount)
			if multiplyErr != nil {
				return nil, 0, multiplyErr
			}
			direction := "in"
			if recipeLine.direction == "input" {
				direction = "out"
			}
			lines = append(lines, cityResourcePostingLine{
				balance: balances[recipeLine.resourceCode], direction: direction,
				quantityUnits: quantity, memo: "Automatic basic goods production",
			})
		}
		recipeID, batches := recipe.id, batchCount
		resourceSpecs = append(resourceSpecs, cityResourceOperationSpec{
			worldID: cycle.worldID, tick: cycle.tick,
			operationKey:  fmt.Sprintf("market:v1:%d:goods:production", cycle.cycleIndex),
			operationType: "production", actorEntityID: firm.entityID,
			districtID: firm.districtID, recipeID: &recipeID, batchCount: &batches,
			description: "Automatic basic goods production",
			metadata: map[string]any{
				"cycle_index": cycle.cycleIndex, "recipe_code": recipe.code,
				"batch_count": batchCount, "schema_version": 1,
			},
			lines: lines,
		})
		produced, multiplyErr := cityMultiplyUnits(outputPerBatch, batchCount)
		if multiplyErr != nil {
			return nil, 0, multiplyErr
		}
		projectedSupply, err = addCityLedgerUnits(projectedSupply, produced)
		if err != nil {
			return nil, 0, err
		}
	}
	affordable := virtualCash[householdID] / market.quoteUnits
	cleared := cityMinimum(demand, projectedSupply, affordable)
	goodsAmount, err := cityMultiplyUnits(cleared, market.quoteUnits)
	if err != nil {
		return nil, 0, err
	}
	allocations := make([]cityMarketAllocationPlan, 0, 1)
	journalSpecs := make([]cityLedgerJournalSpec, 0, 1)
	if cleared > 0 {
		fromID, toID := firm.entityID, householdID
		districtID, resourceID := firm.districtID, firmGoods.resourceID
		allocations = append(allocations, cityMarketAllocationPlan{
			allocationType: "goods", fromEntityID: &fromID, toEntityID: &toID,
			districtID: &districtID, resourceID: &resourceID,
			quantityUnits: cleared, unitPriceUnits: market.quoteUnits,
			amountUnits: goodsAmount,
			metadata:    map[string]any{"consumed_in_cycle": true, "schema_version": 1},
		})
		journalSpecs = append(journalSpecs, cityLedgerJournalSpec{
			worldID: cycle.worldID, unit: cycle.unit, tick: cycle.tick,
			operationKey: fmt.Sprintf("market:v1:%d:goods:purchase", cycle.cycleIndex),
			journalType:  "purchase", description: "Basic goods market settlement",
			metadata: map[string]any{
				"cycle_index": cycle.cycleIndex, "quantity_units": cleared,
				"unit_price_units": market.quoteUnits, "schema_version": 1,
			},
			lines: []cityLedgerPostingLine{
				{account: accounts["household.consumption_expense"], debitUnits: goodsAmount, memo: "Basic goods"},
				{account: accounts["firm.cash"], debitUnits: goodsAmount, memo: "Basic goods"},
				{account: accounts["household.cash"], creditUnits: goodsAmount, memo: "Basic goods"},
				{account: accounts["firm.revenue"], creditUnits: goodsAmount, memo: "Basic goods"},
			},
		})
		resourceSpecs = append(resourceSpecs,
			cityResourceOperationSpec{
				worldID: cycle.worldID, tick: cycle.tick,
				operationKey:  fmt.Sprintf("market:v1:%d:goods:delivery", cycle.cycleIndex),
				operationType: "transfer", actorEntityID: firm.entityID, districtID: firm.districtID,
				description: "Basic goods delivery",
				metadata:    map[string]any{"cycle_index": cycle.cycleIndex, "schema_version": 1},
				lines: []cityResourcePostingLine{
					{balance: firmGoods, direction: "out", quantityUnits: cleared, memo: "Goods sold"},
					{balance: householdGoods, direction: "in", quantityUnits: cleared, memo: "Goods purchased"},
				},
			},
			cityResourceOperationSpec{
				worldID: cycle.worldID, tick: cycle.tick,
				operationKey:  fmt.Sprintf("market:v1:%d:goods:consumption", cycle.cycleIndex),
				operationType: "consumption", actorEntityID: householdID, districtID: firm.districtID,
				description: "Household basic goods consumption",
				metadata:    map[string]any{"cycle_index": cycle.cycleIndex, "schema_version": 1},
				lines: []cityResourcePostingLine{
					{balance: householdGoods, direction: "out", quantityUnits: cleared, memo: "Household consumption"},
				},
			},
		)
		if err = cityApplyVirtualTransfer(virtualCash, householdID, firm.entityID, goodsAmount); err != nil {
			return nil, 0, err
		}
	}
	nextQuote, err := cityNextMarketQuote(
		market.quoteUnits, market.floorUnits, market.ceilingUnits,
		market.maximumAdjustmentMilli, demand, projectedSupply,
	)
	if err != nil {
		return nil, 0, err
	}
	gross, err := citySettlementGross(allocations)
	if err != nil || gross != goodsAmount {
		if err != nil {
			return nil, 0, err
		}
		return nil, 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "goods_gross"})
	}
	return &cityMarketSettlementPlan{
		settlementType: CityMarketBasicGoods, sequence: 2, cycleIndex: cycle.cycleIndex,
		settlementKey: fmt.Sprintf("cycle:v1:%d:basic_goods", cycle.cycleIndex),
		clearingPrice: market.quoteUnits, demandUnits: demand, supplyUnits: projectedSupply,
		clearedUnits: cleared, unmetUnits: demand - cleared, excessUnits: projectedSupply - cleared,
		grossAmount: gross, allocations: allocations, journalSpecs: journalSpecs,
		resourceSpecs: resourceSpecs, marketState: market, nextQuote: nextQuote,
		metadata: map[string]any{
			"schema_version": 1, "production_batches": batchCount,
			"affordable_units": affordable, "next_quote_units": nextQuote,
		},
	}, goodsAmount, nil
}

func buildCityHousingSettlementPlan(
	ctx context.Context,
	tx *sql.Tx,
	cycle *cityMarketCyclePlan,
	market *cityMarketStateRef,
	cohorts []cityMarketCohortRef,
	occupancies map[int64]cityMarketOccupancyRef,
	householdID, governmentID int64,
	accounts map[string]*cityLedgerAccountRef,
	virtualCash map[int64]int64,
) (*cityMarketSettlementPlan, error) {
	if market == nil || market.resourceID == nil {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "housing_market"})
	}
	var demand int64
	weights := make([]cityAllocationWeight, 0, len(cohorts))
	for _, cohort := range cohorts {
		var err error
		demand, err = addCityLedgerUnits(demand, cohort.housingDemandUnits)
		if err != nil {
			return nil, err
		}
		weights = append(weights, cityAllocationWeight{key: cohort.id, weight: cohort.housingDemandUnits})
	}
	governmentHousing, err := ensureCityInventoryRef(ctx, tx, cycle.worldID, governmentID, "central", "housing_units")
	if err != nil {
		return nil, err
	}
	if governmentHousing.resourceID != *market.resourceID {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "housing_resource"})
	}
	supply := governmentHousing.quantityUnits
	affordable := virtualCash[householdID] / market.quoteUnits
	cleared := cityMinimum(demand, supply, affordable)
	rentAmount, err := cityMultiplyUnits(cleared, market.quoteUnits)
	if err != nil {
		return nil, err
	}
	cohortUnits, err := cityProportionalAllocation(cleared, weights)
	if err != nil {
		return nil, err
	}
	allocations := make([]cityMarketAllocationPlan, 0, len(cohorts))
	housingProjection := make([]cityHousingProjectionPlan, 0, len(cohorts))
	for _, cohort := range cohorts {
		occupancy, ok := occupancies[cohort.id]
		if !ok {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"cohort_id": strconv.FormatInt(cohort.id, 10)})
		}
		quantity := cohortUnits[cohort.id]
		housingProjection = append(housingProjection, cityHousingProjectionPlan{
			occupancyID: occupancy.id, version: occupancy.version,
			occupiedUnits: quantity, unmetUnits: cohort.housingDemandUnits - quantity,
			rentPriceUnits: market.quoteUnits,
		})
		if quantity == 0 {
			continue
		}
		amount, multiplyErr := cityMultiplyUnits(quantity, market.quoteUnits)
		if multiplyErr != nil {
			return nil, multiplyErr
		}
		cohortID, districtID := cohort.id, cohort.districtID
		fromID, toID := governmentID, householdID
		resourceID := governmentHousing.resourceID
		allocations = append(allocations, cityMarketAllocationPlan{
			allocationType: "housing", cohortID: &cohortID,
			fromEntityID: &fromID, toEntityID: &toID, districtID: &districtID,
			resourceID: &resourceID, quantityUnits: quantity,
			unitPriceUnits: market.quoteUnits, amountUnits: amount,
			metadata: map[string]any{
				"income_band": cohort.incomeBand, "tenure": "public_rental",
				"schema_version": 1,
			},
		})
	}
	journalSpecs := make([]cityLedgerJournalSpec, 0, 1)
	if rentAmount > 0 {
		journalSpecs = append(journalSpecs, cityLedgerJournalSpec{
			worldID: cycle.worldID, unit: cycle.unit, tick: cycle.tick,
			operationKey: fmt.Sprintf("market:v1:%d:housing:rent", cycle.cycleIndex),
			journalType:  "rent", description: "Public housing rent settlement",
			metadata: map[string]any{
				"cycle_index": cycle.cycleIndex, "occupied_units": cleared,
				"rent_price_units": market.quoteUnits, "schema_version": 1,
			},
			lines: []cityLedgerPostingLine{
				{account: accounts["household.rent_expense"], debitUnits: rentAmount, memo: "Public housing rent"},
				{account: accounts["government.cash"], debitUnits: rentAmount, memo: "Public housing rent"},
				{account: accounts["household.cash"], creditUnits: rentAmount, memo: "Public housing rent"},
				{account: accounts["government.rental_revenue"], creditUnits: rentAmount, memo: "Public housing rent"},
			},
		})
		if err = cityApplyVirtualTransfer(virtualCash, householdID, governmentID, rentAmount); err != nil {
			return nil, err
		}
	}
	nextQuote, err := cityNextMarketQuote(
		market.quoteUnits, market.floorUnits, market.ceilingUnits,
		market.maximumAdjustmentMilli, demand, supply,
	)
	if err != nil {
		return nil, err
	}
	gross, err := citySettlementGross(allocations)
	if err != nil || gross != rentAmount {
		if err != nil {
			return nil, err
		}
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "housing_gross"})
	}
	return &cityMarketSettlementPlan{
		settlementType: CityMarketHousing, sequence: 3, cycleIndex: cycle.cycleIndex,
		settlementKey: fmt.Sprintf("cycle:v1:%d:housing", cycle.cycleIndex),
		clearingPrice: market.quoteUnits, demandUnits: demand, supplyUnits: supply,
		clearedUnits: cleared, unmetUnits: demand - cleared, excessUnits: supply - cleared,
		grossAmount: gross, allocations: allocations, journalSpecs: journalSpecs,
		resourceSpecs: make([]cityResourceOperationSpec, 0), marketState: market,
		nextQuote: nextQuote, housing: housingProjection,
		metadata: map[string]any{
			"schema_version": 1, "tenure": "public_rental",
			"affordable_units": affordable, "next_quote_units": nextQuote,
		},
	}, nil
}

func buildCityFiscalSettlementPlan(
	cycle *cityMarketCyclePlan,
	policy cityPolicyRef,
	budgets map[string]cityBudgetLineRef,
	householdID, firmID, governmentID int64,
	wageAmount, goodsAmount int64,
	accounts map[string]*cityLedgerAccountRef,
	virtualCash map[int64]int64,
) (*cityMarketSettlementPlan, error) {
	householdTax, err := cityMulDivFloor(wageAmount, policy.householdWageTaxMilli, 1000)
	if err != nil {
		return nil, err
	}
	firmTax, err := cityMulDivFloor(goodsAmount, policy.firmSalesTaxMilli, 1000)
	if err != nil {
		return nil, err
	}
	householdTax = cityMinimum(householdTax, virtualCash[householdID])
	firmTax = cityMinimum(firmTax, virtualCash[firmID])
	taxTotal, err := addCityLedgerUnits(householdTax, firmTax)
	if err != nil {
		return nil, err
	}
	allocations := make([]cityMarketAllocationPlan, 0, 4)
	journalSpecs := make([]cityLedgerJournalSpec, 0, 4)
	if householdTax > 0 {
		fromID, toID := householdID, governmentID
		allocations = append(allocations, cityMarketAllocationPlan{
			allocationType: "tax", fromEntityID: &fromID, toEntityID: &toID,
			amountUnits: householdTax,
			metadata:    map[string]any{"tax_base": "wage", "rate_milli": policy.householdWageTaxMilli, "schema_version": 1},
		})
		journalSpecs = append(journalSpecs, cityLedgerJournalSpec{
			worldID: cycle.worldID, unit: cycle.unit, tick: cycle.tick,
			operationKey: fmt.Sprintf("market:v1:%d:fiscal:household_tax", cycle.cycleIndex),
			journalType:  "tax", description: "Household wage tax settlement",
			metadata: map[string]any{"cycle_index": cycle.cycleIndex, "tax_base": "wage", "schema_version": 1},
			lines: []cityLedgerPostingLine{
				{account: accounts["household.tax_expense"], debitUnits: householdTax, memo: "Wage tax"},
				{account: accounts["government.cash"], debitUnits: householdTax, memo: "Wage tax"},
				{account: accounts["household.cash"], creditUnits: householdTax, memo: "Wage tax"},
				{account: accounts["government.tax_revenue"], creditUnits: householdTax, memo: "Wage tax"},
			},
		})
		if err = cityApplyVirtualTransfer(virtualCash, householdID, governmentID, householdTax); err != nil {
			return nil, err
		}
	}
	if firmTax > 0 {
		fromID, toID := firmID, governmentID
		allocations = append(allocations, cityMarketAllocationPlan{
			allocationType: "tax", fromEntityID: &fromID, toEntityID: &toID,
			amountUnits: firmTax,
			metadata:    map[string]any{"tax_base": "sales", "rate_milli": policy.firmSalesTaxMilli, "schema_version": 1},
		})
		journalSpecs = append(journalSpecs, cityLedgerJournalSpec{
			worldID: cycle.worldID, unit: cycle.unit, tick: cycle.tick,
			operationKey: fmt.Sprintf("market:v1:%d:fiscal:firm_tax", cycle.cycleIndex),
			journalType:  "tax", description: "Firm sales tax settlement",
			metadata: map[string]any{"cycle_index": cycle.cycleIndex, "tax_base": "sales", "schema_version": 1},
			lines: []cityLedgerPostingLine{
				{account: accounts["firm.tax_expense"], debitUnits: firmTax, memo: "Sales tax"},
				{account: accounts["government.cash"], debitUnits: firmTax, memo: "Sales tax"},
				{account: accounts["firm.cash"], creditUnits: firmTax, memo: "Sales tax"},
				{account: accounts["government.tax_revenue"], creditUnits: firmTax, memo: "Sales tax"},
			},
		})
		if err = cityApplyVirtualTransfer(virtualCash, firmID, governmentID, firmTax); err != nil {
			return nil, err
		}
	}

	procurement, err := cityMulDivFloor(taxTotal, policy.procurementShareMilli, 1000)
	if err != nil {
		return nil, err
	}
	support, err := cityMulDivFloor(taxTotal, policy.socialSupportShareMilli, 1000)
	if err != nil {
		return nil, err
	}
	healthcareBudget := budgets["healthcare"]
	supportBudget := budgets["social_protection"]
	healthcareRemaining := healthcareBudget.appropriatedUnits - healthcareBudget.committedUnits - healthcareBudget.spentUnits
	supportRemaining := supportBudget.appropriatedUnits - supportBudget.committedUnits - supportBudget.spentUnits
	if healthcareRemaining < 0 || supportRemaining < 0 {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "budget_remaining"})
	}
	procurement = cityMinimum(procurement, healthcareRemaining, virtualCash[governmentID])
	if procurement > 0 {
		fromID, toID := governmentID, firmID
		allocations = append(allocations, cityMarketAllocationPlan{
			allocationType: "spending", fromEntityID: &fromID, toEntityID: &toID,
			amountUnits: procurement,
			metadata:    map[string]any{"budget_code": "healthcare", "purpose": "public_service_procurement", "schema_version": 1},
		})
		journalSpecs = append(journalSpecs, cityLedgerJournalSpec{
			worldID: cycle.worldID, unit: cycle.unit, tick: cycle.tick,
			operationKey: fmt.Sprintf("market:v1:%d:fiscal:procurement", cycle.cycleIndex),
			journalType:  "government_spend", description: "Public service procurement",
			metadata: map[string]any{"cycle_index": cycle.cycleIndex, "budget_code": "healthcare", "schema_version": 1},
			lines: []cityLedgerPostingLine{
				{account: accounts["government.public_service_expense"], debitUnits: procurement, memo: "Public services"},
				{account: accounts["firm.cash"], debitUnits: procurement, memo: "Public services"},
				{account: accounts["government.cash"], creditUnits: procurement, memo: "Public services"},
				{account: accounts["firm.revenue"], creditUnits: procurement, memo: "Public services"},
			},
		})
		if err = cityApplyVirtualTransfer(virtualCash, governmentID, firmID, procurement); err != nil {
			return nil, err
		}
	}
	support = cityMinimum(support, supportRemaining, virtualCash[governmentID])
	if support > 0 {
		fromID, toID := governmentID, householdID
		allocations = append(allocations, cityMarketAllocationPlan{
			allocationType: "spending", fromEntityID: &fromID, toEntityID: &toID,
			amountUnits: support,
			metadata:    map[string]any{"budget_code": "social_protection", "purpose": "income_support", "schema_version": 1},
		})
		journalSpecs = append(journalSpecs, cityLedgerJournalSpec{
			worldID: cycle.worldID, unit: cycle.unit, tick: cycle.tick,
			operationKey: fmt.Sprintf("market:v1:%d:fiscal:support", cycle.cycleIndex),
			journalType:  "subsidy", description: "Social protection transfer",
			metadata: map[string]any{"cycle_index": cycle.cycleIndex, "budget_code": "social_protection", "schema_version": 1},
			lines: []cityLedgerPostingLine{
				{account: accounts["government.subsidy_expense"], debitUnits: support, memo: "Income support"},
				{account: accounts["household.cash"], debitUnits: support, memo: "Income support"},
				{account: accounts["government.cash"], creditUnits: support, memo: "Income support"},
				{account: accounts["household.other_income"], creditUnits: support, memo: "Income support"},
			},
		})
		if err = cityApplyVirtualTransfer(virtualCash, governmentID, householdID, support); err != nil {
			return nil, err
		}
	}
	budgetSpends := make([]cityBudgetSpendPlan, 0, 2)
	if procurement > 0 {
		budgetSpends = append(budgetSpends, cityBudgetSpendPlan{
			budgetLineID: healthcareBudget.id, version: healthcareBudget.version,
			amountUnits: procurement, memo: "Public service procurement",
		})
	}
	if support > 0 {
		budgetSpends = append(budgetSpends, cityBudgetSpendPlan{
			budgetLineID: supportBudget.id, version: supportBudget.version,
			amountUnits: support, memo: "Social protection transfer",
		})
	}
	gross, err := citySettlementGross(allocations)
	if err != nil {
		return nil, err
	}
	spendingTotal, err := addCityLedgerUnits(procurement, support)
	if err != nil {
		return nil, err
	}
	expectedGross, err := addCityLedgerUnits(taxTotal, spendingTotal)
	if err != nil || gross != expectedGross {
		if err != nil {
			return nil, err
		}
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "fiscal_gross"})
	}
	return &cityMarketSettlementPlan{
		settlementType: CitySettlementFiscal, sequence: 4, cycleIndex: cycle.cycleIndex,
		settlementKey: fmt.Sprintf("cycle:v1:%d:fiscal", cycle.cycleIndex),
		clearingPrice: 0, demandUnits: 0, supplyUnits: 0,
		clearedUnits: 0, unmetUnits: 0, excessUnits: 0,
		grossAmount: gross, allocations: allocations, journalSpecs: journalSpecs,
		resourceSpecs: make([]cityResourceOperationSpec, 0), budgetSpends: budgetSpends,
		metadata: map[string]any{
			"schema_version": 1, "tax_collected_units": taxTotal,
			"government_spending_units": spendingTotal,
			"household_wage_tax_milli":  policy.householdWageTaxMilli,
			"firm_sales_tax_milli":      policy.firmSalesTaxMilli,
		},
	}, nil
}

func executeCityMarketCyclePlan(
	ctx context.Context,
	tx *sql.Tx,
	plan *cityMarketCyclePlan,
	journalSequence, resourceSequence int64,
) (*cityMarketCycleExecution, error) {
	if plan == nil || plan.unit == nil || len(plan.settlements) != 4 {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "market_cycle_plan"})
	}
	execution := &cityMarketCycleExecution{
		settlements:          make([]*CityMarketSettlement, 0, 4),
		events:               make([]cityMarketCycleEvent, 0, 5),
		nextJournalSequence:  journalSequence,
		nextResourceSequence: resourceSequence,
	}
	for _, settlementPlan := range plan.settlements {
		metadata, err := json.Marshal(settlementPlan.metadata)
		if err != nil {
			return nil, fmt.Errorf("marshal city market settlement metadata: %w", err)
		}
		var settlementID int64
		err = tx.QueryRowContext(ctx, `
INSERT INTO city_market_settlements
    (world_id, monetary_unit_id, tick, sequence, cycle_index,
     settlement_key, settlement_type, clearing_price_units,
     demand_units, supply_units, cleared_units, unmet_demand_units,
     excess_supply_units, gross_amount_units, journal_count,
     resource_operation_count, allocation_count, budget_movement_count, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14,
        $15, $16, $17, $18, $19::jsonb)
RETURNING id`, plan.worldID, plan.unit.id, plan.tick, settlementPlan.sequence,
			settlementPlan.cycleIndex, settlementPlan.settlementKey,
			settlementPlan.settlementType, settlementPlan.clearingPrice,
			settlementPlan.demandUnits, settlementPlan.supplyUnits,
			settlementPlan.clearedUnits, settlementPlan.unmetUnits,
			settlementPlan.excessUnits, settlementPlan.grossAmount,
			len(settlementPlan.journalSpecs), len(settlementPlan.resourceSpecs),
			len(settlementPlan.allocations), len(settlementPlan.budgetSpends), metadata,
		).Scan(&settlementID)
		if err != nil {
			return nil, fmt.Errorf("create city market settlement draft: %w", err)
		}
		for index, allocation := range settlementPlan.allocations {
			allocationMetadata, marshalErr := json.Marshal(allocation.metadata)
			if marshalErr != nil {
				return nil, fmt.Errorf("marshal city market allocation metadata: %w", marshalErr)
			}
			if _, err = tx.ExecContext(ctx, `
INSERT INTO city_market_allocations
    (settlement_id, world_id, monetary_unit_id, line_no, allocation_type,
     cohort_id, from_entity_id, to_entity_id, district_id, resource_id,
     quantity_units, unit_price_units, amount_units, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14::jsonb)`,
				settlementID, plan.worldID, plan.unit.id, index+1,
				allocation.allocationType, cityNullableInt64(allocation.cohortID),
				cityNullableInt64(allocation.fromEntityID), cityNullableInt64(allocation.toEntityID),
				cityNullableInt64(allocation.districtID), cityNullableInt64(allocation.resourceID),
				allocation.quantityUnits, allocation.unitPriceUnits, allocation.amountUnits,
				allocationMetadata); err != nil {
				return nil, fmt.Errorf("insert city market allocation %d: %w", index+1, err)
			}
		}
		for index := range settlementPlan.journalSpecs {
			spec := settlementPlan.journalSpecs[index]
			spec.sequence = execution.nextJournalSequence
			spec.marketSettlementID = &settlementID
			if _, err = postCityJournal(ctx, tx, spec); err != nil {
				return nil, fmt.Errorf("post city market journal %s: %w", settlementPlan.settlementType, err)
			}
			execution.nextJournalSequence++
		}
		for index := range settlementPlan.resourceSpecs {
			spec := settlementPlan.resourceSpecs[index]
			spec.sequence = execution.nextResourceSequence
			spec.marketSettlementID = &settlementID
			if _, err = postCityResourceOperation(ctx, tx, spec); err != nil {
				return nil, fmt.Errorf("post city market resource operation %s: %w", settlementPlan.settlementType, err)
			}
			execution.nextResourceSequence++
		}
		if settlementPlan.labor != nil {
			cohortIDs := make([]int64, 0, len(settlementPlan.labor.cohortUnits))
			for cohortID := range settlementPlan.labor.cohortUnits {
				cohortIDs = append(cohortIDs, cohortID)
			}
			sort.Slice(cohortIDs, func(i, j int) bool { return cohortIDs[i] < cohortIDs[j] })
			for _, cohortID := range cohortIDs {
				if _, err = tx.ExecContext(ctx, `SELECT post_city_household_employment($1, $2, $3, $4)`,
					cohortID, plan.worldID, settlementPlan.labor.cohortVersions[cohortID],
					settlementPlan.labor.cohortUnits[cohortID]); err != nil {
					return nil, fmt.Errorf("post city household employment: %w", err)
				}
			}
			if _, err = tx.ExecContext(ctx, `SELECT post_city_firm_employment($1, $2, $3, $4)`,
				settlementPlan.labor.firmEntityID, plan.worldID,
				settlementPlan.labor.firmVersion, settlementPlan.labor.employeeUnits); err != nil {
				return nil, fmt.Errorf("post city firm employment: %w", err)
			}
		}
		for _, occupancy := range settlementPlan.housing {
			if _, err = tx.ExecContext(ctx, `SELECT post_city_housing_occupancy($1, $2, $3, $4, $5, $6)`,
				settlementID, occupancy.occupancyID, occupancy.version,
				occupancy.occupiedUnits, occupancy.unmetUnits, occupancy.rentPriceUnits); err != nil {
				return nil, fmt.Errorf("post city housing occupancy: %w", err)
			}
		}
		for index, budget := range settlementPlan.budgetSpends {
			var movementID int64
			if err = tx.QueryRowContext(ctx, `
SELECT post_city_budget_spend($1, $2, $3, $4, $5, $6)`,
				settlementID, budget.budgetLineID, index+1, budget.version,
				budget.amountUnits, budget.memo).Scan(&movementID); err != nil {
				return nil, fmt.Errorf("post city budget spend: %w", err)
			}
		}
		if settlementPlan.marketState != nil {
			if _, err = tx.ExecContext(ctx, `SELECT post_city_market_state($1, $2, $3, $4)`,
				settlementID, settlementPlan.marketState.id,
				settlementPlan.marketState.version, settlementPlan.nextQuote); err != nil {
				return nil, fmt.Errorf("post city market state: %w", err)
			}
		}
		result, updateErr := tx.ExecContext(ctx, `
UPDATE city_market_settlements SET posted_at = NOW()
WHERE id = $1 AND posted_at IS NULL`, settlementID)
		if updateErr != nil {
			return nil, fmt.Errorf("seal city market settlement: %w", updateErr)
		}
		rowsAffected, rowsErr := result.RowsAffected()
		if rowsErr != nil || rowsAffected != 1 {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"settlement_id": strconv.FormatInt(settlementID, 10)})
		}
		item, loadErr := loadCityMarketSettlementByCursor(ctx, tx, plan.worldID, plan.tick, settlementPlan.sequence, true)
		if loadErr != nil {
			return nil, fmt.Errorf("load posted city market settlement: %w", loadErr)
		}
		execution.settlements = append(execution.settlements, item)
		execution.events = append(execution.events, cityMarketCycleEvent{
			eventType: "city.market." + settlementPlan.settlementType + "_settled",
			payload: map[string]any{
				"cycle_index": plan.cycleIndex, "settlement_type": settlementPlan.settlementType,
				"tick": plan.tick, "cleared_units": settlementPlan.clearedUnits,
				"gross_amount_units":  settlementPlan.grossAmount,
				"settlement_sequence": settlementPlan.sequence,
			},
		})
	}
	if _, err := tx.ExecContext(ctx, `SELECT advance_city_economic_cycle($1, $2, $3)`,
		plan.worldID, plan.cycleBefore, plan.tick); err != nil {
		return nil, fmt.Errorf("advance city economic cycle: %w", err)
	}
	execution.events = append(execution.events, cityMarketCycleEvent{
		eventType: "city.economy.cycle_completed",
		payload: map[string]any{
			"cycle_index": plan.cycleIndex, "tick": plan.tick,
			"settlement_count": len(execution.settlements),
			"next_due_tick":    plan.tick + int64(plan.cadenceTicks),
		},
	})
	return execution, nil
}

func cityNullableInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

type cityMarketHashState struct {
	Cycle       cityMarketHashCycle       `json:"cycle"`
	Policy      cityMarketHashPolicy      `json:"policy"`
	Markets     []cityMarketHashMarket    `json:"markets"`
	Occupancies []cityMarketHashOccupancy `json:"occupancies"`
}

type cityMarketHashCycle struct {
	CycleIndex      int64  `json:"cycle_index"`
	CadenceTicks    int    `json:"cadence_ticks"`
	NextDueTick     int64  `json:"next_due_tick"`
	LastSettledTick *int64 `json:"last_settled_tick,omitempty"`
	Version         int64  `json:"version"`
}

type cityMarketHashPolicy struct {
	LaborDemandCapacityMilli     int   `json:"labor_demand_capacity_milli"`
	GoodsDemandPopulationDivisor int64 `json:"goods_demand_population_divisor"`
	HouseholdWageTaxMilli        int   `json:"household_wage_tax_milli"`
	FirmSalesTaxMilli            int   `json:"firm_sales_tax_milli"`
	ProcurementShareMilli        int   `json:"procurement_share_milli"`
	SocialSupportShareMilli      int   `json:"social_support_share_milli"`
	Version                      int64 `json:"version"`
}

type cityMarketHashMarket struct {
	MarketCode             string  `json:"market_code"`
	MonetaryUnitCode       string  `json:"monetary_unit_code"`
	ResourceCode           *string `json:"resource_code,omitempty"`
	QuoteUnits             int64   `json:"quote_units"`
	FloorUnits             int64   `json:"floor_units"`
	CeilingUnits           int64   `json:"ceiling_units"`
	MaximumAdjustmentMilli int     `json:"maximum_adjustment_milli"`
	LastClearingTick       *int64  `json:"last_clearing_tick,omitempty"`
	LastClearingPriceUnits *int64  `json:"last_clearing_price_units,omitempty"`
	LastDemandUnits        int64   `json:"last_demand_units"`
	LastSupplyUnits        int64   `json:"last_supply_units"`
	LastClearedUnits       int64   `json:"last_cleared_units"`
	LastUnmetDemandUnits   int64   `json:"last_unmet_demand_units"`
	LastExcessSupplyUnits  int64   `json:"last_excess_supply_units"`
	Version                int64   `json:"version"`
}

type cityMarketHashOccupancy struct {
	DistrictCode    string `json:"district_code"`
	IncomeBand      string `json:"income_band"`
	OccupiedUnits   int64  `json:"occupied_units"`
	UnmetUnits      int64  `json:"unmet_units"`
	RentPriceUnits  int64  `json:"rent_price_units"`
	LastSettledTick *int64 `json:"last_settled_tick,omitempty"`
	Version         int64  `json:"version"`
}

func loadCityMarketHashState(ctx context.Context, queryer citySQLQueryer, worldID int64) (cityMarketHashState, error) {
	state := cityMarketHashState{
		Markets:     make([]cityMarketHashMarket, 0, 3),
		Occupancies: make([]cityMarketHashOccupancy, 0),
	}
	var lastCycleTick sql.NullInt64
	if err := queryer.QueryRowContext(ctx, `
SELECT cycle_index, cadence_ticks, next_due_tick, last_settled_tick, version
FROM city_economic_cycle_states WHERE world_id = $1`, worldID).Scan(
		&state.Cycle.CycleIndex, &state.Cycle.CadenceTicks,
		&state.Cycle.NextDueTick, &lastCycleTick, &state.Cycle.Version,
	); err != nil {
		return state, fmt.Errorf("load city market cycle for hash: %w", err)
	}
	state.Cycle.LastSettledTick = nullInt64Pointer(lastCycleTick)
	if err := queryer.QueryRowContext(ctx, `
SELECT labor_demand_capacity_milli, goods_demand_population_divisor,
       household_wage_tax_milli, firm_sales_tax_milli,
       procurement_share_milli, social_support_share_milli, version
FROM city_economic_policies WHERE world_id = $1`, worldID).Scan(
		&state.Policy.LaborDemandCapacityMilli,
		&state.Policy.GoodsDemandPopulationDivisor,
		&state.Policy.HouseholdWageTaxMilli, &state.Policy.FirmSalesTaxMilli,
		&state.Policy.ProcurementShareMilli, &state.Policy.SocialSupportShareMilli,
		&state.Policy.Version,
	); err != nil {
		return state, fmt.Errorf("load city market policy for hash: %w", err)
	}
	rows, err := queryer.QueryContext(ctx, `
SELECT market.market_code, unit.code, resource.code,
       market.quote_units, market.floor_units, market.ceiling_units,
       market.maximum_adjustment_milli, market.last_clearing_tick,
       market.last_clearing_price_units, market.last_demand_units,
       market.last_supply_units, market.last_cleared_units,
       market.last_unmet_demand_units, market.last_excess_supply_units,
       market.version
FROM city_market_states market
JOIN city_monetary_units unit ON unit.id = market.monetary_unit_id
LEFT JOIN city_resources resource ON resource.id = market.resource_id
WHERE market.world_id = $1 ORDER BY market.market_code ASC`, worldID)
	if err != nil {
		return state, fmt.Errorf("load city market states for hash: %w", err)
	}
	for rows.Next() {
		var item cityMarketHashMarket
		var resourceCode sql.NullString
		var lastTick, lastPrice sql.NullInt64
		if err = rows.Scan(&item.MarketCode, &item.MonetaryUnitCode, &resourceCode,
			&item.QuoteUnits, &item.FloorUnits, &item.CeilingUnits,
			&item.MaximumAdjustmentMilli, &lastTick, &lastPrice,
			&item.LastDemandUnits, &item.LastSupplyUnits, &item.LastClearedUnits,
			&item.LastUnmetDemandUnits, &item.LastExcessSupplyUnits,
			&item.Version); err != nil {
			_ = rows.Close()
			return state, err
		}
		item.ResourceCode = nullStringPointer(resourceCode)
		item.LastClearingTick = nullInt64Pointer(lastTick)
		item.LastClearingPriceUnits = nullInt64Pointer(lastPrice)
		state.Markets = append(state.Markets, item)
	}
	if err = closeCityRows(rows, "iterate city market states for hash"); err != nil {
		return state, err
	}
	rows, err = queryer.QueryContext(ctx, `
SELECT district.code, cohort.income_band, occupancy.occupied_units,
       occupancy.unmet_units, occupancy.rent_price_units,
       occupancy.last_settled_tick, occupancy.version
FROM city_housing_occupancies occupancy
JOIN city_household_cohorts cohort ON cohort.id = occupancy.cohort_id
JOIN city_districts district ON district.id = occupancy.district_id
WHERE occupancy.world_id = $1
ORDER BY district.sort_order ASC,
         CASE cohort.income_band WHEN 'low' THEN 1 WHEN 'middle' THEN 2 ELSE 3 END`, worldID)
	if err != nil {
		return state, fmt.Errorf("load city housing occupancies for hash: %w", err)
	}
	for rows.Next() {
		var item cityMarketHashOccupancy
		var lastTick sql.NullInt64
		if err = rows.Scan(&item.DistrictCode, &item.IncomeBand,
			&item.OccupiedUnits, &item.UnmetUnits, &item.RentPriceUnits,
			&lastTick, &item.Version); err != nil {
			_ = rows.Close()
			return state, err
		}
		item.LastSettledTick = nullInt64Pointer(lastTick)
		state.Occupancies = append(state.Occupancies, item)
	}
	if err = closeCityRows(rows, "iterate city housing occupancies for hash"); err != nil {
		return state, err
	}
	return state, nil
}
