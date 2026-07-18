package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/lib/pq"
)

const (
	CityCommandTypeResourceTransfer = "resource.transfer"
	CityCommandTypeResourceConsume  = "resource.consume"
	CityCommandTypeResourceProduce  = "resource.produce"

	cityDefaultResourceOperationLimit = 50
	cityMaximumResourceOperationLimit = 200
	cityMaximumResourceUnits          = int64(math.MaxInt64 / 2)

	cityResourceRejectionScopeNotFound = "CITY_RESOURCE_SCOPE_NOT_FOUND"
	cityResourceRejectionInsufficient  = "CITY_RESOURCE_INSUFFICIENT_INVENTORY"
	cityResourceRejectionRecipe        = "CITY_RESOURCE_RECIPE_NOT_ALLOWED"
	cityResourceRejectionCapacity      = "CITY_RESOURCE_CAPACITY_EXCEEDED"
	cityResourceRejectionEntityType    = "CITY_RESOURCE_ENTITY_TYPE_INVALID"
)

var (
	ErrCityResourceOperationNotFound = infraerrors.NotFound("CITY_RESOURCE_OPERATION_NOT_FOUND", "city resource operation not found")
	cityPhysicalCodePattern          = regexp.MustCompile(`^[a-z][a-z0-9_]{1,47}$`)
)

type CityResourceEntry struct {
	ID                   int64     `json:"id"`
	OperationID          int64     `json:"operation_id"`
	LineNo               int       `json:"line_no"`
	BalanceID            int64     `json:"balance_id"`
	EntityID             int64     `json:"entity_id"`
	EntityType           string    `json:"entity_type"`
	EntityCode           string    `json:"entity_code"`
	EntityName           string    `json:"entity_name"`
	DistrictID           int64     `json:"district_id"`
	DistrictCode         string    `json:"district_code"`
	ResourceID           int64     `json:"resource_id"`
	ResourceCode         string    `json:"resource_code"`
	ResourceName         string    `json:"resource_name"`
	UnitCode             string    `json:"unit_code"`
	UnitScale            int       `json:"unit_scale"`
	Direction            string    `json:"direction"`
	QuantityUnits        int64     `json:"quantity_units"`
	QuantityBeforeUnits  int64     `json:"quantity_before_units"`
	QuantityAfterUnits   int64     `json:"quantity_after_units"`
	BalanceVersionBefore int64     `json:"balance_version_before"`
	BalanceVersionAfter  int64     `json:"balance_version_after"`
	Memo                 string    `json:"memo"`
	CreatedAt            time.Time `json:"created_at"`
}

type CityResourceOperation struct {
	ID                 int64                `json:"id"`
	WorldID            int64                `json:"world_id"`
	Tick               int64                `json:"tick"`
	Sequence           int64                `json:"sequence"`
	OperationKey       string               `json:"operation_key"`
	OperationType      string               `json:"operation_type"`
	SourceCommandID    *int64               `json:"source_command_id,omitempty"`
	MarketSettlementID *int64               `json:"market_settlement_id,omitempty"`
	ActorEntityID      int64                `json:"actor_entity_id"`
	ActorEntityCode    string               `json:"actor_entity_code"`
	ActorEntityName    string               `json:"actor_entity_name"`
	DistrictID         int64                `json:"district_id"`
	DistrictCode       string               `json:"district_code"`
	DistrictName       string               `json:"district_name"`
	RecipeID           *int64               `json:"recipe_id,omitempty"`
	RecipeCode         *string              `json:"recipe_code,omitempty"`
	RecipeName         *string              `json:"recipe_name,omitempty"`
	BatchCount         *int64               `json:"batch_count,omitempty"`
	Description        string               `json:"description"`
	Metadata           map[string]any       `json:"metadata"`
	EntryCount         int                  `json:"entry_count"`
	IncomingUnits      int64                `json:"incoming_units"`
	OutgoingUnits      int64                `json:"outgoing_units"`
	CreatedAt          time.Time            `json:"created_at"`
	PostedAt           time.Time            `json:"posted_at"`
	Entries            []*CityResourceEntry `json:"entries,omitempty"`
}

type CityResourceOperationCursor struct {
	Tick     int64 `json:"tick"`
	Sequence int64 `json:"sequence"`
}

type CityResourceOperationPage struct {
	Items      []*CityResourceOperation     `json:"items"`
	NextCursor *CityResourceOperationCursor `json:"next_cursor,omitempty"`
}

type CityResourceOperationListInput struct {
	UserID        int64
	WorldID       int64
	AfterTick     int64
	AfterSequence int64
	Limit         int
}

type cityResourceTransferPayload struct {
	FromEntityID     int64  `json:"from_entity_id"`
	ToEntityID       int64  `json:"to_entity_id"`
	FromDistrictCode string `json:"from_district_code"`
	ToDistrictCode   string `json:"to_district_code"`
	ResourceCode     string `json:"resource_code"`
	QuantityUnits    int64  `json:"quantity_units"`
	Memo             string `json:"memo,omitempty"`
}

type cityResourceConsumePayload struct {
	EntityID      int64  `json:"entity_id"`
	DistrictCode  string `json:"district_code"`
	ResourceCode  string `json:"resource_code"`
	QuantityUnits int64  `json:"quantity_units"`
	Purpose       string `json:"purpose"`
}

type cityResourceProducePayload struct {
	FirmEntityID int64  `json:"firm_entity_id"`
	DistrictCode string `json:"district_code"`
	RecipeCode   string `json:"recipe_code"`
	BatchCount   int64  `json:"batch_count"`
	Memo         string `json:"memo,omitempty"`
}

func normalizeCityResourceCommand(commandType string, rawPayload json.RawMessage) (any, bool, error) {
	normalizeCode := func(value *string) error {
		*value = strings.ToLower(strings.TrimSpace(*value))
		if !cityPhysicalCodePattern.MatchString(*value) {
			return ErrCityInvalidInput
		}
		return nil
	}
	normalizeMemo := func(value *string) error {
		*value = strings.TrimSpace(*value)
		if utf8.RuneCountInString(*value) > 256 {
			return ErrCityInvalidInput
		}
		return nil
	}
	validQuantity := func(value int64) bool {
		return value > 0 && value <= cityMaximumResourceUnits
	}

	switch commandType {
	case CityCommandTypeResourceTransfer:
		var payload cityResourceTransferPayload
		if err := decodeStrictCityObject(rawPayload, &payload); err != nil {
			return nil, true, ErrCityInvalidInput.WithCause(err)
		}
		if payload.FromEntityID <= 0 || payload.ToEntityID <= 0 ||
			payload.FromEntityID == payload.ToEntityID && strings.EqualFold(payload.FromDistrictCode, payload.ToDistrictCode) ||
			!validQuantity(payload.QuantityUnits) {
			return nil, true, ErrCityInvalidInput
		}
		if err := normalizeCode(&payload.FromDistrictCode); err != nil {
			return nil, true, err
		}
		if err := normalizeCode(&payload.ToDistrictCode); err != nil {
			return nil, true, err
		}
		if err := normalizeCode(&payload.ResourceCode); err != nil {
			return nil, true, err
		}
		if payload.FromEntityID == payload.ToEntityID && payload.FromDistrictCode == payload.ToDistrictCode {
			return nil, true, ErrCityInvalidInput
		}
		if err := normalizeMemo(&payload.Memo); err != nil {
			return nil, true, err
		}
		return payload, true, nil
	case CityCommandTypeResourceConsume:
		var payload cityResourceConsumePayload
		if err := decodeStrictCityObject(rawPayload, &payload); err != nil {
			return nil, true, ErrCityInvalidInput.WithCause(err)
		}
		payload.Purpose = strings.TrimSpace(payload.Purpose)
		if payload.EntityID <= 0 || !validQuantity(payload.QuantityUnits) ||
			utf8.RuneCountInString(payload.Purpose) < 1 || utf8.RuneCountInString(payload.Purpose) > 128 {
			return nil, true, ErrCityInvalidInput
		}
		if err := normalizeCode(&payload.DistrictCode); err != nil {
			return nil, true, err
		}
		if err := normalizeCode(&payload.ResourceCode); err != nil {
			return nil, true, err
		}
		return payload, true, nil
	case CityCommandTypeResourceProduce:
		var payload cityResourceProducePayload
		if err := decodeStrictCityObject(rawPayload, &payload); err != nil {
			return nil, true, ErrCityInvalidInput.WithCause(err)
		}
		if payload.FirmEntityID <= 0 || payload.BatchCount <= 0 || payload.BatchCount > 1_000_000 {
			return nil, true, ErrCityInvalidInput
		}
		if err := normalizeCode(&payload.DistrictCode); err != nil {
			return nil, true, err
		}
		if err := normalizeCode(&payload.RecipeCode); err != nil {
			return nil, true, err
		}
		if err := normalizeMemo(&payload.Memo); err != nil {
			return nil, true, err
		}
		return payload, true, nil
	default:
		return nil, false, nil
	}
}

func isCityResourceCommand(commandType string) bool {
	return commandType == CityCommandTypeResourceTransfer ||
		commandType == CityCommandTypeResourceConsume ||
		commandType == CityCommandTypeResourceProduce
}

func (s *CityEconomyService) ListResourceOperations(ctx context.Context, input CityResourceOperationListInput) (*CityResourceOperationPage, error) {
	if input.UserID <= 0 || input.WorldID <= 0 || input.AfterTick < 0 || input.AfterSequence < 0 {
		return nil, ErrCityInvalidInput
	}
	if input.Limit <= 0 {
		input.Limit = cityDefaultResourceOperationLimit
	}
	if input.Limit > cityMaximumResourceOperationLimit {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, input.UserID, input.WorldID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, cityResourceOperationSelect+`
WHERE operation.world_id = $1
  AND operation.posted_at IS NOT NULL
  AND (operation.tick > $2 OR (operation.tick = $2 AND operation.sequence > $3))
ORDER BY operation.tick ASC, operation.sequence ASC
LIMIT $4`, input.WorldID, input.AfterTick, input.AfterSequence, input.Limit+1)
	if err != nil {
		return nil, fmt.Errorf("list city resource operations: %w", err)
	}
	items := make([]*CityResourceOperation, 0, input.Limit+1)
	for rows.Next() {
		item, scanErr := scanCityResourceOperation(rows)
		if scanErr != nil {
			_ = rows.Close()
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err = closeCityRows(rows, "iterate city resource operations"); err != nil {
		return nil, err
	}
	page := &CityResourceOperationPage{Items: items}
	if len(items) > input.Limit {
		items = items[:input.Limit]
		page.Items = items
		last := items[len(items)-1]
		page.NextCursor = &CityResourceOperationCursor{Tick: last.Tick, Sequence: last.Sequence}
	}
	return page, nil
}

func (s *CityEconomyService) GetResourceOperation(ctx context.Context, userID, worldID, tick, sequence int64) (*CityResourceOperation, error) {
	if userID <= 0 || worldID <= 0 || tick <= 0 || sequence <= 0 {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, userID, worldID); err != nil {
		return nil, err
	}
	operation, err := loadCityResourceOperationByCursor(ctx, s.db, worldID, tick, sequence, true)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCityResourceOperationNotFound
	}
	if err != nil {
		return nil, err
	}
	return operation, nil
}

const cityResourceOperationSelect = `
SELECT operation.id, operation.world_id, operation.tick, operation.sequence,
       operation.operation_key, operation.operation_type, operation.source_command_id,
       operation.market_settlement_id, operation.actor_entity_id, actor.code, actor.name, operation.district_id,
       district.code, district.name, operation.recipe_id, recipe.code, recipe.name,
       operation.batch_count, operation.description, operation.metadata,
       (SELECT COUNT(*) FROM city_resource_entries entry WHERE entry.operation_id = operation.id),
       (SELECT COALESCE(SUM(entry.quantity_units), 0) FROM city_resource_entries entry
        WHERE entry.operation_id = operation.id AND entry.direction = 'in'),
       (SELECT COALESCE(SUM(entry.quantity_units), 0) FROM city_resource_entries entry
        WHERE entry.operation_id = operation.id AND entry.direction = 'out'),
       operation.created_at, operation.posted_at
FROM city_resource_operations operation
JOIN city_economic_entities actor ON actor.id = operation.actor_entity_id
JOIN city_districts district ON district.id = operation.district_id
LEFT JOIN city_production_recipes recipe ON recipe.id = operation.recipe_id
`

func scanCityResourceOperation(row cityScannable) (*CityResourceOperation, error) {
	item := &CityResourceOperation{}
	var sourceCommandID, marketSettlementID, recipeID, batchCount sql.NullInt64
	var recipeCode, recipeName sql.NullString
	var metadata []byte
	if err := row.Scan(&item.ID, &item.WorldID, &item.Tick, &item.Sequence,
		&item.OperationKey, &item.OperationType, &sourceCommandID, &marketSettlementID, &item.ActorEntityID,
		&item.ActorEntityCode, &item.ActorEntityName, &item.DistrictID, &item.DistrictCode,
		&item.DistrictName, &recipeID, &recipeCode, &recipeName, &batchCount,
		&item.Description, &metadata, &item.EntryCount, &item.IncomingUnits,
		&item.OutgoingUnits, &item.CreatedAt, &item.PostedAt); err != nil {
		return nil, err
	}
	item.SourceCommandID = nullInt64Pointer(sourceCommandID)
	item.MarketSettlementID = nullInt64Pointer(marketSettlementID)
	item.RecipeID = nullInt64Pointer(recipeID)
	item.BatchCount = nullInt64Pointer(batchCount)
	if recipeCode.Valid {
		item.RecipeCode = &recipeCode.String
	}
	if recipeName.Valid {
		item.RecipeName = &recipeName.String
	}
	var err error
	item.Metadata, err = decodeCityJSONMap(metadata)
	if err != nil {
		return nil, fmt.Errorf("decode city resource operation metadata: %w", err)
	}
	return item, nil
}

func loadCityResourceOperationByCursor(ctx context.Context, queryer citySQLQueryer, worldID, tick, sequence int64, withEntries bool) (*CityResourceOperation, error) {
	operation, err := scanCityResourceOperation(queryer.QueryRowContext(ctx, cityResourceOperationSelect+`
WHERE operation.world_id = $1 AND operation.tick = $2 AND operation.sequence = $3
  AND operation.posted_at IS NOT NULL`, worldID, tick, sequence))
	if err != nil {
		return nil, err
	}
	if withEntries {
		operation.Entries, err = loadCityResourceEntries(ctx, queryer, operation.ID)
	}
	return operation, err
}

func loadCityResourceOperationByID(ctx context.Context, queryer citySQLQueryer, worldID, operationID int64, withEntries bool) (*CityResourceOperation, error) {
	operation, err := scanCityResourceOperation(queryer.QueryRowContext(ctx, cityResourceOperationSelect+`
WHERE operation.world_id = $1 AND operation.id = $2 AND operation.posted_at IS NOT NULL`, worldID, operationID))
	if err != nil {
		return nil, err
	}
	if withEntries {
		operation.Entries, err = loadCityResourceEntries(ctx, queryer, operation.ID)
	}
	return operation, err
}

func loadCityResourceOperationsForTick(ctx context.Context, queryer citySQLQueryer, worldID, tick int64) ([]*CityResourceOperation, error) {
	rows, err := queryer.QueryContext(ctx, cityResourceOperationSelect+`
WHERE operation.world_id = $1 AND operation.tick = $2 AND operation.posted_at IS NOT NULL
ORDER BY operation.sequence ASC`, worldID, tick)
	if err != nil {
		return nil, fmt.Errorf("load city tick resource operations: %w", err)
	}
	items := make([]*CityResourceOperation, 0)
	for rows.Next() {
		item, scanErr := scanCityResourceOperation(rows)
		if scanErr != nil {
			_ = rows.Close()
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err = closeCityRows(rows, "iterate city tick resource operations"); err != nil {
		return nil, err
	}
	for _, item := range items {
		item.Entries, err = loadCityResourceEntries(ctx, queryer, item.ID)
		if err != nil {
			return nil, err
		}
	}
	return items, nil
}

func loadCityResourceEntries(ctx context.Context, queryer citySQLQueryer, operationID int64) ([]*CityResourceEntry, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT entry.id, entry.operation_id, entry.line_no, entry.balance_id,
       balance.entity_id, balance.entity_type, entity.code, entity.name,
       balance.district_id, district.code, entry.resource_id, resource.code,
       resource.name, resource.unit_code, resource.unit_scale, entry.direction,
       entry.quantity_units, entry.quantity_before_units, entry.quantity_after_units,
       entry.balance_version_before, entry.balance_version_after, entry.memo, entry.created_at
FROM city_resource_entries entry
JOIN city_inventory_balances balance ON balance.id = entry.balance_id
JOIN city_economic_entities entity ON entity.id = balance.entity_id
JOIN city_districts district ON district.id = balance.district_id
JOIN city_resources resource ON resource.id = entry.resource_id
WHERE entry.operation_id = $1
ORDER BY entry.line_no ASC`, operationID)
	if err != nil {
		return nil, fmt.Errorf("load city resource entries: %w", err)
	}
	items := make([]*CityResourceEntry, 0)
	for rows.Next() {
		item := &CityResourceEntry{}
		if err = rows.Scan(&item.ID, &item.OperationID, &item.LineNo, &item.BalanceID,
			&item.EntityID, &item.EntityType, &item.EntityCode, &item.EntityName,
			&item.DistrictID, &item.DistrictCode, &item.ResourceID, &item.ResourceCode,
			&item.ResourceName, &item.UnitCode, &item.UnitScale, &item.Direction,
			&item.QuantityUnits, &item.QuantityBeforeUnits, &item.QuantityAfterUnits,
			&item.BalanceVersionBefore, &item.BalanceVersionAfter, &item.Memo,
			&item.CreatedAt); err != nil {
			_ = rows.Close()
			return nil, err
		}
		items = append(items, item)
	}
	if err = closeCityRows(rows, "iterate city resource entries"); err != nil {
		return nil, err
	}
	return items, nil
}

type cityInventoryRef struct {
	id            int64
	entityID      int64
	entityType    string
	entityCode    string
	districtID    int64
	districtCode  string
	resourceID    int64
	resourceCode  string
	quantityUnits int64
	version       int64
}

type cityResourcePostingLine struct {
	balance       *cityInventoryRef
	direction     string
	quantityUnits int64
	memo          string
}

type cityResourceOperationSpec struct {
	worldID            int64
	tick               int64
	sequence           int64
	operationKey       string
	operationType      string
	sourceCommandID    *int64
	marketSettlementID *int64
	actorEntityID      int64
	districtID         int64
	recipeID           *int64
	batchCount         *int64
	description        string
	metadata           map[string]any
	lines              []cityResourcePostingLine
}

type cityResourceBootstrapEvent struct {
	eventType string
	payload   map[string]any
}

type cityResourceBusinessError struct{ code string }

func (e *cityResourceBusinessError) Error() string { return e.code }
func cityResourceReject(code string) error         { return &cityResourceBusinessError{code: code} }

func ensureCityInventoryRef(ctx context.Context, tx *sql.Tx, worldID, entityID int64, districtCode, resourceCode string) (*cityInventoryRef, error) {
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_inventory_balances
    (world_id, entity_id, entity_type, district_id, resource_id, opening_quantity_units)
SELECT $1, entity.id, entity.entity_type, district.id, resource.id, 0
FROM city_economic_entities entity
JOIN city_districts district ON district.world_id = entity.world_id AND district.code = $3
JOIN city_resources resource ON resource.world_id = entity.world_id AND resource.code = $4
WHERE entity.world_id = $1 AND entity.id = $2 AND entity.status = 'active'
  AND entity.entity_type IN ('household', 'firm', 'government')
  AND resource.status = 'active' AND resource.storable
ON CONFLICT (world_id, entity_id, district_id, resource_id) DO NOTHING`,
		worldID, entityID, districtCode, resourceCode); err != nil {
		return nil, fmt.Errorf("ensure city inventory balance: %w", err)
	}
	item := &cityInventoryRef{}
	err := tx.QueryRowContext(ctx, `
SELECT balance.id, balance.entity_id, balance.entity_type, entity.code,
       balance.district_id, district.code, balance.resource_id, resource.code,
       balance.quantity_units, balance.version
FROM city_inventory_balances balance
JOIN city_economic_entities entity ON entity.id = balance.entity_id AND entity.status = 'active'
JOIN city_districts district ON district.id = balance.district_id
JOIN city_resources resource ON resource.id = balance.resource_id AND resource.status = 'active' AND resource.storable
WHERE balance.world_id = $1 AND balance.entity_id = $2 AND district.code = $3
  AND resource.code = $4 AND balance.status = 'active'`,
		worldID, entityID, districtCode, resourceCode).Scan(
		&item.id, &item.entityID, &item.entityType, &item.entityCode,
		&item.districtID, &item.districtCode, &item.resourceID, &item.resourceCode,
		&item.quantityUnits, &item.version,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, cityResourceReject(cityResourceRejectionScopeNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("load city inventory balance: %w", err)
	}
	return item, nil
}

func (s *CityEconomyService) ensureCityResourceBootstrap(ctx context.Context, tx *sql.Tx, worldID, targetTick, sequence int64) ([]cityResourceBootstrapEvent, int64, error) {
	var postedOpeningCount, expectedOpeningCount int
	if err := tx.QueryRowContext(ctx, `
SELECT
    (SELECT COUNT(*) FROM city_resource_operations
     WHERE world_id = $1 AND operation_type = 'opening' AND posted_at IS NOT NULL),
    (SELECT COUNT(DISTINCT (entity_id, district_id)) FROM city_inventory_balances
     WHERE world_id = $1 AND opening_quantity_units > 0)`, worldID).Scan(
		&postedOpeningCount, &expectedOpeningCount,
	); err != nil {
		return nil, sequence, fmt.Errorf("inspect city resource bootstrap: %w", err)
	}
	if expectedOpeningCount <= 0 || postedOpeningCount < 0 || postedOpeningCount > expectedOpeningCount {
		return nil, sequence, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "resource_opening_count"})
	}
	if postedOpeningCount == expectedOpeningCount {
		return nil, sequence, nil
	}
	if postedOpeningCount != 0 {
		return nil, sequence, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "resource_opening_partial"})
	}

	rows, err := tx.QueryContext(ctx, `
SELECT balance.id, balance.entity_id, balance.entity_type, entity.code,
       balance.district_id, district.code, balance.resource_id, resource.code,
       balance.quantity_units, balance.version, balance.opening_quantity_units
FROM city_inventory_balances balance
JOIN city_economic_entities entity ON entity.id = balance.entity_id
JOIN city_districts district ON district.id = balance.district_id
JOIN city_resources resource ON resource.id = balance.resource_id
WHERE balance.world_id = $1 AND balance.opening_quantity_units > 0
ORDER BY entity.code ASC, district.sort_order ASC, resource.code ASC
FOR UPDATE OF balance`, worldID)
	if err != nil {
		return nil, sequence, fmt.Errorf("load city resource opening balances: %w", err)
	}
	type openingLine struct {
		ref      *cityInventoryRef
		quantity int64
	}
	groups := make([][]openingLine, 0)
	for rows.Next() {
		ref := &cityInventoryRef{}
		var openingQuantity int64
		if err = rows.Scan(&ref.id, &ref.entityID, &ref.entityType, &ref.entityCode,
			&ref.districtID, &ref.districtCode, &ref.resourceID, &ref.resourceCode,
			&ref.quantityUnits, &ref.version, &openingQuantity); err != nil {
			_ = rows.Close()
			return nil, sequence, err
		}
		if ref.quantityUnits != 0 || ref.version != 0 {
			_ = rows.Close()
			return nil, sequence, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "resource_opening_projection"})
		}
		if len(groups) == 0 || groups[len(groups)-1][0].ref.entityID != ref.entityID ||
			groups[len(groups)-1][0].ref.districtID != ref.districtID {
			groups = append(groups, make([]openingLine, 0))
		}
		groups[len(groups)-1] = append(groups[len(groups)-1], openingLine{ref: ref, quantity: openingQuantity})
	}
	if err = closeCityRows(rows, "iterate city resource opening balances"); err != nil {
		return nil, sequence, err
	}
	if len(groups) == 0 {
		return nil, sequence, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "resource_opening_balances"})
	}

	events := make([]cityResourceBootstrapEvent, 0, len(groups))
	for _, group := range groups {
		first := group[0].ref
		lines := make([]cityResourcePostingLine, 0, len(group))
		for _, opening := range group {
			lines = append(lines, cityResourcePostingLine{
				balance: opening.ref, direction: "in", quantityUnits: opening.quantity,
				memo: "F3 opening inventory",
			})
		}
		operation, postErr := postCityResourceOperation(ctx, tx, cityResourceOperationSpec{
			worldID: worldID, tick: targetTick, sequence: sequence,
			operationKey:  "opening:" + first.entityCode + ":" + first.districtCode,
			operationType: "opening", actorEntityID: first.entityID, districtID: first.districtID,
			description: "F3 opening inventory", metadata: map[string]any{"schema_version": 1}, lines: lines,
		})
		if postErr != nil {
			return nil, sequence, postErr
		}
		events = append(events, cityResourceBootstrapEvent{
			eventType: "city.resource.opening_posted",
			payload: map[string]any{
				"operation_tick": operation.Tick, "operation_sequence": operation.Sequence,
				"actor_entity_code": operation.ActorEntityCode, "entry_count": operation.EntryCount,
			},
		})
		sequence++
	}
	return events, sequence, nil
}

func (s *CityEconomyService) applyCityResourceCommand(ctx context.Context, tx *sql.Tx, worldID, targetTick, sequence int64, command *CityCommand) (cityPendingEvent, *CityResourceOperation, error) {
	if _, err := tx.ExecContext(ctx, `SAVEPOINT city_resource_command`); err != nil {
		return cityPendingEvent{}, nil, fmt.Errorf("create city resource command savepoint: %w", err)
	}
	operation, err := s.postCityResourceCommand(ctx, tx, worldID, targetTick, sequence, command)
	if err != nil {
		if _, rollbackErr := tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT city_resource_command`); rollbackErr != nil {
			return cityPendingEvent{}, nil, fmt.Errorf("rollback city resource command savepoint after %v: %w", err, rollbackErr)
		}
		if _, releaseErr := tx.ExecContext(ctx, `RELEASE SAVEPOINT city_resource_command`); releaseErr != nil {
			return cityPendingEvent{}, nil, fmt.Errorf("release rejected city resource command savepoint: %w", releaseErr)
		}
		if code := cityResourceBusinessRejectionCode(err); code != "" {
			return rejectedCityCommand(command, code), nil, nil
		}
		return cityPendingEvent{}, nil, err
	}
	if _, err = tx.ExecContext(ctx, `RELEASE SAVEPOINT city_resource_command`); err != nil {
		return cityPendingEvent{}, nil, fmt.Errorf("release city resource command savepoint: %w", err)
	}
	eventType := map[string]string{
		CityCommandTypeResourceTransfer: "city.resource.transferred",
		CityCommandTypeResourceConsume:  "city.resource.consumed",
		CityCommandTypeResourceProduce:  "city.resource.produced",
	}[command.CommandType]
	payload := map[string]any{
		"operation_tick": operation.Tick, "operation_sequence": operation.Sequence,
		"operation_type": operation.OperationType, "actor_entity_code": operation.ActorEntityCode,
		"district_code": operation.DistrictCode, "entry_count": operation.EntryCount,
	}
	if operation.RecipeCode != nil {
		payload["recipe_code"] = *operation.RecipeCode
		payload["batch_count"] = *operation.BatchCount
	}
	return cityPendingEvent{
		command: command, status: CityCommandStatusApplied, eventType: eventType, payload: payload,
		result: map[string]any{
			"applied": true, "operation_tick": operation.Tick,
			"operation_sequence": operation.Sequence, "operation_type": operation.OperationType,
		},
	}, operation, nil
}

func cityResourceBusinessRejectionCode(err error) string {
	var businessErr *cityResourceBusinessError
	if errors.As(err, &businessErr) {
		return businessErr.code
	}
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		switch {
		case strings.Contains(pqErr.Message, "insufficient quantity"):
			return cityResourceRejectionInsufficient
		case strings.Contains(pqErr.Message, "production capacity exceeded"):
			return cityResourceRejectionCapacity
		case strings.Contains(pqErr.Message, "production actor"),
			strings.Contains(pqErr.Message, "not granted"):
			return cityResourceRejectionRecipe
		}
	}
	return ""
}

type cityRecipeExecutionLine struct {
	resourceCode  string
	direction     string
	quantityUnits int64
}

type cityRecipeExecution struct {
	id                    int64
	firmEntityID          int64
	districtID            int64
	districtCode          string
	code                  string
	capacityUnitsPerBatch int64
	productionCapacity    int64
	lines                 []cityRecipeExecutionLine
}

func loadCityRecipeExecution(ctx context.Context, tx *sql.Tx, worldID, firmEntityID int64, districtCode, recipeCode string) (*cityRecipeExecution, error) {
	item := &cityRecipeExecution{lines: make([]cityRecipeExecutionLine, 0)}
	err := tx.QueryRowContext(ctx, `
SELECT recipe.id, firm.entity_id, firm.district_id, district.code, recipe.code,
       recipe.capacity_units_per_batch, firm.production_capacity_units
FROM city_production_recipes recipe
JOIN city_firm_recipes firm_recipe
  ON firm_recipe.recipe_id = recipe.id AND firm_recipe.world_id = recipe.world_id AND firm_recipe.status = 'active'
JOIN city_firm_states firm
  ON firm.entity_id = firm_recipe.firm_entity_id AND firm.world_id = firm_recipe.world_id
JOIN city_economic_entities entity
  ON entity.id = firm.entity_id AND entity.status = 'active'
JOIN city_districts district ON district.id = firm.district_id
WHERE recipe.world_id = $1 AND recipe.code = $2 AND recipe.status = 'active'
  AND firm.entity_id = $3 AND district.code = $4
FOR UPDATE OF firm`, worldID, recipeCode, firmEntityID, districtCode).Scan(
		&item.id, &item.firmEntityID, &item.districtID, &item.districtCode, &item.code,
		&item.capacityUnitsPerBatch, &item.productionCapacity,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, cityResourceReject(cityResourceRejectionRecipe)
	}
	if err != nil {
		return nil, fmt.Errorf("load city production recipe: %w", err)
	}
	rows, err := tx.QueryContext(ctx, `
SELECT resource.code, line.direction, line.quantity_units
FROM city_production_recipe_lines line
JOIN city_resources resource ON resource.id = line.resource_id AND resource.status = 'active' AND resource.storable
WHERE line.recipe_id = $1
ORDER BY CASE line.direction WHEN 'input' THEN 1 ELSE 2 END, resource.code ASC`, item.id)
	if err != nil {
		return nil, fmt.Errorf("load city production recipe lines: %w", err)
	}
	for rows.Next() {
		var line cityRecipeExecutionLine
		if err = rows.Scan(&line.resourceCode, &line.direction, &line.quantityUnits); err != nil {
			_ = rows.Close()
			return nil, err
		}
		item.lines = append(item.lines, line)
	}
	if err = closeCityRows(rows, "iterate city production recipe execution lines"); err != nil {
		return nil, err
	}
	if len(item.lines) < 2 {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "production_recipe_lines"})
	}
	return item, nil
}

func (s *CityEconomyService) postCityResourceCommand(ctx context.Context, tx *sql.Tx, worldID, targetTick, sequence int64, command *CityCommand) (*CityResourceOperation, error) {
	spec := cityResourceOperationSpec{
		worldID: worldID, tick: targetTick, sequence: sequence,
		operationKey:    "command:" + strconv.FormatInt(command.Sequence, 10),
		sourceCommandID: &command.ID,
		metadata:        map[string]any{"command_sequence": command.Sequence, "schema_version": 1},
	}
	switch command.CommandType {
	case CityCommandTypeResourceTransfer:
		payload, err := decodeStoredCityCommandPayload[cityResourceTransferPayload](command)
		if err != nil {
			return nil, err
		}
		from, err := ensureCityInventoryRef(ctx, tx, worldID, payload.FromEntityID, payload.FromDistrictCode, payload.ResourceCode)
		if err != nil {
			return nil, err
		}
		to, err := ensureCityInventoryRef(ctx, tx, worldID, payload.ToEntityID, payload.ToDistrictCode, payload.ResourceCode)
		if err != nil {
			return nil, err
		}
		if from.id == to.id || from.resourceID != to.resourceID {
			return nil, cityResourceReject(cityResourceRejectionScopeNotFound)
		}
		spec.operationType = "transfer"
		spec.actorEntityID = from.entityID
		spec.districtID = from.districtID
		spec.description = "Resource transfer"
		spec.metadata["from_entity_code"] = from.entityCode
		spec.metadata["to_entity_code"] = to.entityCode
		spec.metadata["from_district_code"] = from.districtCode
		spec.metadata["to_district_code"] = to.districtCode
		spec.metadata["resource_code"] = from.resourceCode
		spec.metadata["quantity_units"] = payload.QuantityUnits
		spec.lines = []cityResourcePostingLine{
			{balance: from, direction: "out", quantityUnits: payload.QuantityUnits, memo: payload.Memo},
			{balance: to, direction: "in", quantityUnits: payload.QuantityUnits, memo: payload.Memo},
		}
	case CityCommandTypeResourceConsume:
		payload, err := decodeStoredCityCommandPayload[cityResourceConsumePayload](command)
		if err != nil {
			return nil, err
		}
		balance, err := ensureCityInventoryRef(ctx, tx, worldID, payload.EntityID, payload.DistrictCode, payload.ResourceCode)
		if err != nil {
			return nil, err
		}
		if balance.entityType == CityEntityTypeClearing {
			return nil, cityResourceReject(cityResourceRejectionEntityType)
		}
		spec.operationType = "consumption"
		spec.actorEntityID = balance.entityID
		spec.districtID = balance.districtID
		spec.description = payload.Purpose
		spec.metadata["entity_code"] = balance.entityCode
		spec.metadata["district_code"] = balance.districtCode
		spec.metadata["resource_code"] = balance.resourceCode
		spec.metadata["quantity_units"] = payload.QuantityUnits
		spec.metadata["purpose"] = payload.Purpose
		spec.lines = []cityResourcePostingLine{
			{balance: balance, direction: "out", quantityUnits: payload.QuantityUnits, memo: payload.Purpose},
		}
	case CityCommandTypeResourceProduce:
		payload, err := decodeStoredCityCommandPayload[cityResourceProducePayload](command)
		if err != nil {
			return nil, err
		}
		recipe, err := loadCityRecipeExecution(ctx, tx, worldID, payload.FirmEntityID, payload.DistrictCode, payload.RecipeCode)
		if err != nil {
			return nil, err
		}
		if recipe.capacityUnitsPerBatch > math.MaxInt64/payload.BatchCount {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "production_capacity"})
		}
		requestedCapacity := recipe.capacityUnitsPerBatch * payload.BatchCount
		var usedCapacity int64
		if err = tx.QueryRowContext(ctx, `
SELECT COALESCE(SUM(operation.batch_count * recipe.capacity_units_per_batch), 0)::BIGINT
FROM city_resource_operations operation
JOIN city_production_recipes recipe ON recipe.id = operation.recipe_id
WHERE operation.world_id = $1 AND operation.tick = $2
  AND operation.actor_entity_id = $3 AND operation.operation_type = 'production'
  AND operation.posted_at IS NOT NULL`, worldID, targetTick, payload.FirmEntityID).Scan(&usedCapacity); err != nil {
			return nil, fmt.Errorf("load city production capacity use: %w", err)
		}
		if requestedCapacity > recipe.productionCapacity-usedCapacity {
			return nil, cityResourceReject(cityResourceRejectionCapacity)
		}
		lines := make([]cityResourcePostingLine, 0, len(recipe.lines))
		for _, recipeLine := range recipe.lines {
			if recipeLine.quantityUnits > math.MaxInt64/payload.BatchCount {
				return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "production_quantity"})
			}
			quantity := recipeLine.quantityUnits * payload.BatchCount
			balance, loadErr := ensureCityInventoryRef(ctx, tx, worldID, payload.FirmEntityID, payload.DistrictCode, recipeLine.resourceCode)
			if loadErr != nil {
				return nil, loadErr
			}
			direction := "in"
			if recipeLine.direction == "input" {
				direction = "out"
			}
			lines = append(lines, cityResourcePostingLine{
				balance: balance, direction: direction, quantityUnits: quantity, memo: payload.Memo,
			})
		}
		recipeID := recipe.id
		batchCount := payload.BatchCount
		spec.operationType = "production"
		spec.actorEntityID = recipe.firmEntityID
		spec.districtID = recipe.districtID
		spec.recipeID = &recipeID
		spec.batchCount = &batchCount
		spec.description = "Resource production"
		spec.metadata["firm_entity_id"] = recipe.firmEntityID
		spec.metadata["district_code"] = recipe.districtCode
		spec.metadata["recipe_code"] = recipe.code
		spec.metadata["batch_count"] = payload.BatchCount
		spec.metadata["capacity_units"] = requestedCapacity
		spec.lines = lines
	default:
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"command_type": command.CommandType})
	}
	return postCityResourceOperation(ctx, tx, spec)
}

func postCityResourceOperation(ctx context.Context, tx *sql.Tx, spec cityResourceOperationSpec) (*CityResourceOperation, error) {
	if spec.worldID <= 0 || spec.tick <= 0 || spec.sequence <= 0 || spec.actorEntityID <= 0 ||
		spec.districtID <= 0 || len(spec.operationKey) < 1 || len(spec.operationKey) > 128 ||
		utf8.RuneCountInString(spec.description) > 256 || len(spec.lines) == 0 {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "resource_operation_header"})
	}
	seenBalances := make(map[int64]struct{}, len(spec.lines))
	balanceIDs := make([]int64, 0, len(spec.lines))
	for _, line := range spec.lines {
		if line.balance == nil || line.balance.id <= 0 || line.quantityUnits <= 0 ||
			line.quantityUnits > cityMaximumResourceUnits ||
			(line.direction != "in" && line.direction != "out") || utf8.RuneCountInString(line.memo) > 256 {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "resource_operation_line"})
		}
		if _, exists := seenBalances[line.balance.id]; exists {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "resource_operation_balance_duplicate"})
		}
		seenBalances[line.balance.id] = struct{}{}
		balanceIDs = append(balanceIDs, line.balance.id)
	}
	sort.Slice(balanceIDs, func(i, j int) bool { return balanceIDs[i] < balanceIDs[j] })
	rows, err := tx.QueryContext(ctx, `
SELECT id FROM city_inventory_balances
WHERE world_id = $1 AND id = ANY($2) AND status = 'active'
ORDER BY id ASC FOR UPDATE`, spec.worldID, pq.Array(balanceIDs))
	if err != nil {
		return nil, fmt.Errorf("lock city inventory balances: %w", err)
	}
	lockedCount := 0
	for rows.Next() {
		var ignored int64
		if err = rows.Scan(&ignored); err != nil {
			_ = rows.Close()
			return nil, err
		}
		lockedCount++
	}
	if err = closeCityRows(rows, "iterate locked city inventory balances"); err != nil {
		return nil, err
	}
	if lockedCount != len(balanceIDs) {
		return nil, cityResourceReject(cityResourceRejectionScopeNotFound)
	}
	metadata, err := json.Marshal(spec.metadata)
	if err != nil {
		return nil, fmt.Errorf("marshal city resource operation metadata: %w", err)
	}
	var sourceCommandID, marketSettlementID, recipeID, batchCount any
	if spec.sourceCommandID != nil {
		sourceCommandID = *spec.sourceCommandID
	}
	if spec.marketSettlementID != nil {
		marketSettlementID = *spec.marketSettlementID
	}
	if spec.recipeID != nil {
		recipeID = *spec.recipeID
	}
	if spec.batchCount != nil {
		batchCount = *spec.batchCount
	}
	var operationID int64
	err = tx.QueryRowContext(ctx, `
INSERT INTO city_resource_operations
    (world_id, tick, sequence, operation_key, operation_type, source_command_id,
     market_settlement_id, actor_entity_id, district_id, recipe_id, batch_count, description, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13::jsonb)
RETURNING id`, spec.worldID, spec.tick, spec.sequence, spec.operationKey,
		spec.operationType, sourceCommandID, marketSettlementID, spec.actorEntityID,
		spec.districtID, recipeID, batchCount, spec.description, metadata).Scan(&operationID)
	if err != nil {
		return nil, fmt.Errorf("create city resource operation draft: %w", err)
	}
	for index, line := range spec.lines {
		var entryID int64
		if err = tx.QueryRowContext(ctx, `
SELECT post_city_resource_entry($1, $2, $3, $4, $5, $6)`,
			operationID, line.balance.id, index+1, line.direction,
			line.quantityUnits, line.memo).Scan(&entryID); err != nil {
			return nil, fmt.Errorf("post city resource entry %d: %w", index+1, err)
		}
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_resource_operations SET posted_at = NOW()
WHERE id = $1 AND posted_at IS NULL`, operationID)
	if err != nil {
		return nil, fmt.Errorf("seal city resource operation: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil || rowsAffected != 1 {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"resource_operation_id": strconv.FormatInt(operationID, 10)})
	}
	operation, err := loadCityResourceOperationByID(ctx, tx, spec.worldID, operationID, true)
	if err != nil {
		return nil, fmt.Errorf("load posted city resource operation: %w", err)
	}
	return operation, nil
}
