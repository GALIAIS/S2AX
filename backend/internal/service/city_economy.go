package service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/lib/pq"
)

const (
	CityWorldStatusPaused = "paused"
	CityMemberRoleOwner   = "owner"

	CityEntityTypeHousehold  = "household"
	CityEntityTypeFirm       = "firm"
	CityEntityTypeGovernment = "government"
	CityEntityTypeClearing   = "clearing"
)

var (
	ErrCityWorldNotFound = infraerrors.NotFound("CITY_WORLD_NOT_FOUND", "city world not found")
	ErrCityWorldExists   = infraerrors.Conflict("CITY_WORLD_EXISTS", "an active private city already exists")
	ErrCityInvalidInput  = infraerrors.BadRequest("CITY_INVALID_INPUT", "invalid city request")
)

var cityMonetaryUnitCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{1,15}$`)

type CityWorld struct {
	ID                int64          `json:"id"`
	Name              string         `json:"name"`
	OwnerUserID       int64          `json:"owner_user_id"`
	GroupID           *int64         `json:"group_id,omitempty"`
	Status            string         `json:"status"`
	SimulationVersion string         `json:"simulation_version"`
	Seed              int64          `json:"-"`
	CurrentTick       int64          `json:"current_tick"`
	SimulatedAt       *time.Time     `json:"simulated_at,omitempty"`
	NextTickAt        *time.Time     `json:"next_tick_at,omitempty"`
	SpeedMultiplier   float64        `json:"speed_multiplier"`
	Timezone          string         `json:"timezone"`
	StateHash         *string        `json:"state_hash,omitempty"`
	Settings          map[string]any `json:"settings"`
	MemberRole        string         `json:"member_role"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
}

type CityMonetaryUnit struct {
	ID        int64          `json:"id"`
	WorldID   int64          `json:"world_id"`
	Code      string         `json:"code"`
	Name      string         `json:"name"`
	Symbol    string         `json:"symbol"`
	Scale     int            `json:"scale"`
	Status    string         `json:"status"`
	IsBase    bool           `json:"is_base"`
	Metadata  map[string]any `json:"metadata"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

type CityAccountTemplate struct {
	ID            int64          `json:"id"`
	WorldID       int64          `json:"world_id"`
	EntityType    string         `json:"entity_type"`
	Code          string         `json:"code"`
	Name          string         `json:"name"`
	AccountClass  string         `json:"account_class"`
	NormalSide    string         `json:"normal_side"`
	AllowNegative bool           `json:"allow_negative"`
	IsRequired    bool           `json:"is_required"`
	SortOrder     int            `json:"sort_order"`
	Metadata      map[string]any `json:"metadata"`
	CreatedAt     time.Time      `json:"created_at"`
}

type CityAccount struct {
	ID                  int64          `json:"id"`
	WorldID             int64          `json:"world_id"`
	EntityID            int64          `json:"entity_id"`
	EntityType          string         `json:"entity_type"`
	MonetaryUnitID      int64          `json:"monetary_unit_id"`
	TemplateID          int64          `json:"template_id"`
	Code                string         `json:"code"`
	Name                string         `json:"name"`
	AccountClass        string         `json:"account_class"`
	NormalSide          string         `json:"normal_side"`
	AllowNegative       bool           `json:"allow_negative"`
	CurrentBalanceUnits int64          `json:"current_balance_units"`
	Version             int64          `json:"version"`
	Status              string         `json:"status"`
	Metadata            map[string]any `json:"metadata"`
	CreatedAt           time.Time      `json:"created_at"`
	UpdatedAt           time.Time      `json:"updated_at"`
}

type CityEconomicEntity struct {
	ID          int64          `json:"id"`
	WorldID     int64          `json:"world_id"`
	EntityType  string         `json:"entity_type"`
	Code        string         `json:"code"`
	Name        string         `json:"name"`
	OwnerUserID *int64         `json:"owner_user_id,omitempty"`
	Status      string         `json:"status"`
	Metadata    map[string]any `json:"metadata"`
	Accounts    []*CityAccount `json:"accounts"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

type CityWorldFoundation struct {
	World            *CityWorld             `json:"world"`
	MonetaryUnits    []*CityMonetaryUnit    `json:"monetary_units"`
	AccountTemplates []*CityAccountTemplate `json:"account_templates"`
	Entities         []*CityEconomicEntity  `json:"entities"`
	Physical         *CityPhysicalState     `json:"physical"`
	Markets          *CityMarketOverview    `json:"markets"`
}

type CityMonetaryUnitCreateInput struct {
	Code   string
	Name   string
	Symbol string
	Scale  *int
}

type CityWorldCreateInput struct {
	OwnerUserID int64
	Name        string
	Timezone    string
	Seed        *int64
	StartAt     *time.Time
	// SimulationVersion is used by migrations and compatibility tests. User
	// handlers intentionally leave it empty so new worlds always use current.
	SimulationVersion string
	MonetaryUnit      CityMonetaryUnitCreateInput
}

type cityAccountTemplateSeed struct {
	entityType    string
	code          string
	name          string
	accountClass  string
	normalSide    string
	allowNegative bool
	sortOrder     int
}

type cityEntitySeed struct {
	entityType string
	code       string
	name       string
	owned      bool
}

type normalizedCityWorldCreateInput struct {
	ownerUserID       int64
	name              string
	timezone          string
	simulationVersion string
	unitCode          string
	unitName          string
	unitSymbol        string
	unitScale         int
	startAt           time.Time
}

type citySQLQueryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type cityScannable interface {
	Scan(dest ...any) error
}

// CityEconomyService owns the first stable city boundary: world identity,
// membership, internal monetary units, economic entities and their chart of accounts.
type CityEconomyService struct {
	db *sql.DB
}

func NewCityEconomyService(db *sql.DB) *CityEconomyService {
	return &CityEconomyService{db: db}
}

func (s *CityEconomyService) CreateWorld(ctx context.Context, input CityWorldCreateInput) (*CityWorldFoundation, error) {
	normalized, err := normalizeCityWorldCreateInput(input)
	if err != nil {
		return nil, err
	}
	var seed int64
	if input.Seed != nil {
		if *input.Seed <= 0 {
			return nil, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "seed"})
		}
		seed = *input.Seed
	} else {
		seed, err = newCitySeed()
		if err != nil {
			return nil, fmt.Errorf("generate city seed: %w", err)
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin city world transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var worldID int64
	err = tx.QueryRowContext(ctx, `
INSERT INTO city_worlds
    (name, owner_user_id, status, simulation_version, seed, timezone, simulated_at, settings)
VALUES ($1, $2, $3, $4, $5, $6, $7, '{}'::jsonb)
RETURNING id`, normalized.name, normalized.ownerUserID, CityWorldStatusPaused,
		normalized.simulationVersion, seed, normalized.timezone, normalized.startAt).Scan(&worldID)
	if err != nil {
		return nil, mapCityCreateError(err)
	}

	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_members (world_id, user_id, role, status)
VALUES ($1, $2, $3, 'active')`, worldID, normalized.ownerUserID, CityMemberRoleOwner); err != nil {
		return nil, fmt.Errorf("create city owner membership: %w", err)
	}

	var monetaryUnitID int64
	err = tx.QueryRowContext(ctx, `
INSERT INTO city_monetary_units
    (world_id, code, name, symbol, scale, status, is_base, metadata)
VALUES ($1, $2, $3, $4, $5, 'active', TRUE, '{}'::jsonb)
RETURNING id`, worldID, normalized.unitCode, normalized.unitName,
		normalized.unitSymbol, normalized.unitScale).Scan(&monetaryUnitID)
	if err != nil {
		return nil, fmt.Errorf("create city base monetary unit: %w", err)
	}

	templates := defaultCityAccountTemplates()
	templateIDs := make(map[string]int64, len(templates))
	for _, template := range templates {
		var templateID int64
		err = tx.QueryRowContext(ctx, `
INSERT INTO city_account_templates
    (world_id, entity_type, code, name, account_class, normal_side,
     allow_negative, is_required, sort_order, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, TRUE, $8, '{}'::jsonb)
RETURNING id`, worldID, template.entityType, template.code, template.name,
			template.accountClass, template.normalSide, template.allowNegative, template.sortOrder).Scan(&templateID)
		if err != nil {
			return nil, fmt.Errorf("create city account template %s.%s: %w", template.entityType, template.code, err)
		}
		templateIDs[template.entityType+"/"+template.code] = templateID
	}

	for _, entity := range defaultCityEntities() {
		var ownerUserID any
		if entity.owned {
			ownerUserID = normalized.ownerUserID
		}
		var entityID int64
		err = tx.QueryRowContext(ctx, `
INSERT INTO city_economic_entities
    (world_id, entity_type, code, name, owner_user_id, status, metadata)
VALUES ($1, $2, $3, $4, $5, 'active', '{}'::jsonb)
RETURNING id`, worldID, entity.entityType, entity.code, entity.name, ownerUserID).Scan(&entityID)
		if err != nil {
			return nil, fmt.Errorf("create city entity %s: %w", entity.code, err)
		}

		for _, template := range templates {
			if template.entityType != entity.entityType {
				continue
			}
			if _, err = tx.ExecContext(ctx, `
INSERT INTO city_accounts
    (world_id, entity_id, entity_type, monetary_unit_id, template_id,
     allow_negative, current_balance_units, version, status, metadata)
VALUES ($1, $2, $3, $4, $5, $6, 0, 0, 'active', '{}'::jsonb)`,
				worldID, entityID, entity.entityType, monetaryUnitID,
				templateIDs[template.entityType+"/"+template.code], template.allowNegative); err != nil {
				return nil, fmt.Errorf("create city account %s.%s: %w", entity.code, template.code, err)
			}
		}
	}
	if _, err = tx.ExecContext(ctx, `SELECT initialize_city_f3_foundation($1)`, worldID); err != nil {
		return nil, fmt.Errorf("initialize city F3 foundation: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `SELECT assert_city_f3_foundation($1)`, worldID); err != nil {
		return nil, fmt.Errorf("validate city F3 foundation: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `SELECT initialize_city_f4_foundation($1)`, worldID); err != nil {
		return nil, fmt.Errorf("initialize city F4 foundation: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `SELECT assert_city_f4_foundation($1)`, worldID); err != nil {
		return nil, fmt.Errorf("validate city F4 foundation: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `SELECT initialize_city_f6_foundation($1)`, worldID); err != nil {
		return nil, fmt.Errorf("initialize city F6 foundation: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `SELECT assert_city_demography_projection($1)`, worldID); err != nil {
		return nil, fmt.Errorf("validate city F6 foundation: %w", err)
	}
	if cityEngineSupportsHouseholdLifecycle(normalized.simulationVersion) {
		if _, err = tx.ExecContext(ctx, `SELECT initialize_city_f63_foundation($1)`, worldID); err != nil {
			return nil, fmt.Errorf("initialize city F6.3 foundation: %w", err)
		}
		if _, err = tx.ExecContext(ctx, `SELECT assert_city_household_projection($1)`, worldID); err != nil {
			return nil, fmt.Errorf("validate city F6.3 foundation: %w", err)
		}
	}
	if cityEngineSupportsSpatial(normalized.simulationVersion) {
		if err = initializeCityF7SpatialFoundation(
			ctx, tx, worldID, seed, normalized.simulationVersion,
		); err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, `SELECT assert_city_spatial_foundation($1)`, worldID); err != nil {
			return nil, fmt.Errorf("validate city F7 spatial foundation: %w", err)
		}
	}
	if cityEngineSupportsLand(normalized.simulationVersion) {
		if err = initializeCityLandFoundation(
			ctx, tx, worldID, seed, normalized.simulationVersion,
		); err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, `SELECT assert_city_land_foundation($1)`, worldID); err != nil {
			return nil, fmt.Errorf("validate city F7.3 land foundation: %w", err)
		}
	}
	if cityEngineSupportsDevelopment(normalized.simulationVersion) {
		if err = initializeCityDevelopmentFoundation(ctx, tx, worldID); err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, `SELECT assert_city_development_foundation($1)`, worldID); err != nil {
			return nil, fmt.Errorf("validate city F7.4 development foundation: %w", err)
		}
	}
	if cityEngineSupportsEnterpriseLocation(normalized.simulationVersion) {
		if err = initializeCityEnterpriseLocationFoundation(ctx, tx, worldID); err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, `SELECT assert_city_enterprise_location_foundation($1)`, worldID); err != nil {
			return nil, fmt.Errorf("validate city F7.5 enterprise location foundation: %w", err)
		}
	}
	if cityEngineSupportsWorldRuntime(normalized.simulationVersion) {
		if err = initializeWorldRuntimeFoundation(ctx, tx, worldID); err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, `SELECT assert_world_runtime_foundation($1)`, worldID); err != nil {
			return nil, fmt.Errorf("validate open world runtime foundation: %w", err)
		}
	}
	_, canonical, stateHash, err := canonicalCityWorldState(ctx, tx, worldID)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE city_worlds SET state_hash = $2 WHERE id = $1`, worldID, stateHash); err != nil {
		return nil, fmt.Errorf("store city genesis state hash: %w", err)
	}
	if _, err = captureCitySnapshot(ctx, tx, citySnapshotCapture{
		worldID: worldID, tick: 0, simulationVersion: normalized.simulationVersion,
		reason: CitySnapshotReasonGenesis, canonical: canonical, stateHash: stateHash,
	}); err != nil {
		return nil, err
	}

	foundation, err := loadCityWorldFoundation(ctx, tx, normalized.ownerUserID, worldID)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit city world transaction: %w", err)
	}
	return foundation, nil
}

func (s *CityEconomyService) ListWorlds(ctx context.Context, userID int64) ([]*CityWorld, error) {
	if userID <= 0 {
		return nil, ErrCityInvalidInput
	}
	rows, err := s.db.QueryContext(ctx, cityWorldSelect+`
WHERE m.user_id = $1 AND m.status = 'active'
ORDER BY w.created_at DESC, w.id DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("list city worlds: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]*CityWorld, 0)
	for rows.Next() {
		item, scanErr := scanCityWorld(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate city worlds: %w", err)
	}
	return items, nil
}

func (s *CityEconomyService) GetWorld(ctx context.Context, userID, worldID int64) (*CityWorldFoundation, error) {
	if userID <= 0 || worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	return loadCityWorldFoundation(ctx, s.db, userID, worldID)
}

const cityWorldSelect = `
SELECT w.id, w.name, w.owner_user_id, w.group_id, w.status, w.simulation_version,
       w.seed, w.current_tick, w.simulated_at, w.next_tick_at, w.speed_multiplier,
       w.timezone, w.state_hash, w.settings, m.role, w.created_at, w.updated_at
FROM city_worlds w
JOIN city_members m ON m.world_id = w.id `

func loadCityWorldFoundation(ctx context.Context, queryer citySQLQueryer, userID, worldID int64) (*CityWorldFoundation, error) {
	world, err := scanCityWorld(queryer.QueryRowContext(ctx, cityWorldSelect+`
WHERE w.id = $1 AND m.user_id = $2 AND m.status = 'active'`, worldID, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCityWorldNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get city world: %w", err)
	}

	units, err := loadCityMonetaryUnits(ctx, queryer, worldID)
	if err != nil {
		return nil, err
	}
	templates, err := loadCityAccountTemplates(ctx, queryer, worldID)
	if err != nil {
		return nil, err
	}
	entities, err := loadCityEconomicEntities(ctx, queryer, worldID)
	if err != nil {
		return nil, err
	}
	if err = loadCityAccounts(ctx, queryer, worldID, entities); err != nil {
		return nil, err
	}
	physical, err := loadCityPhysicalState(ctx, queryer, worldID)
	if err != nil {
		return nil, err
	}
	markets, err := loadCityMarketOverview(ctx, queryer, worldID)
	if err != nil {
		return nil, err
	}
	return &CityWorldFoundation{
		World:            world,
		MonetaryUnits:    units,
		AccountTemplates: templates,
		Entities:         entities,
		Physical:         physical,
		Markets:          markets,
	}, nil
}

func scanCityWorld(row cityScannable) (*CityWorld, error) {
	item := &CityWorld{}
	var groupID sql.NullInt64
	var simulatedAt, nextTickAt sql.NullTime
	var stateHash sql.NullString
	var settings []byte
	if err := row.Scan(&item.ID, &item.Name, &item.OwnerUserID, &groupID, &item.Status,
		&item.SimulationVersion, &item.Seed, &item.CurrentTick, &simulatedAt, &nextTickAt,
		&item.SpeedMultiplier, &item.Timezone, &stateHash, &settings, &item.MemberRole,
		&item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	item.GroupID = nullInt64Pointer(groupID)
	item.SimulatedAt = nullTimePointer(simulatedAt)
	item.NextTickAt = nullTimePointer(nextTickAt)
	if stateHash.Valid {
		item.StateHash = &stateHash.String
	}
	var err error
	item.Settings, err = decodeCityJSONMap(settings)
	if err != nil {
		return nil, fmt.Errorf("decode city world settings: %w", err)
	}
	return item, nil
}

func loadCityMonetaryUnits(ctx context.Context, queryer citySQLQueryer, worldID int64) ([]*CityMonetaryUnit, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT id, world_id, code, name, symbol, scale, status, is_base, metadata, created_at, updated_at
FROM city_monetary_units
WHERE world_id = $1
ORDER BY is_base DESC, code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("list city monetary units: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]*CityMonetaryUnit, 0)
	for rows.Next() {
		item := &CityMonetaryUnit{}
		var metadata []byte
		if err = rows.Scan(&item.ID, &item.WorldID, &item.Code, &item.Name, &item.Symbol,
			&item.Scale, &item.Status, &item.IsBase, &metadata, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.Metadata, err = decodeCityJSONMap(metadata)
		if err != nil {
			return nil, fmt.Errorf("decode city monetary unit metadata: %w", err)
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate city monetary units: %w", err)
	}
	return items, nil
}

func loadCityAccountTemplates(ctx context.Context, queryer citySQLQueryer, worldID int64) ([]*CityAccountTemplate, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT id, world_id, entity_type, code, name, account_class, normal_side,
       allow_negative, is_required, sort_order, metadata, created_at
FROM city_account_templates
WHERE world_id = $1
ORDER BY entity_type ASC, sort_order ASC, id ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("list city account templates: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]*CityAccountTemplate, 0)
	for rows.Next() {
		item := &CityAccountTemplate{}
		var metadata []byte
		if err = rows.Scan(&item.ID, &item.WorldID, &item.EntityType, &item.Code, &item.Name,
			&item.AccountClass, &item.NormalSide, &item.AllowNegative, &item.IsRequired,
			&item.SortOrder, &metadata, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.Metadata, err = decodeCityJSONMap(metadata)
		if err != nil {
			return nil, fmt.Errorf("decode city account template metadata: %w", err)
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate city account templates: %w", err)
	}
	return items, nil
}

func loadCityEconomicEntities(ctx context.Context, queryer citySQLQueryer, worldID int64) ([]*CityEconomicEntity, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT id, world_id, entity_type, code, name, owner_user_id, status, metadata, created_at, updated_at
FROM city_economic_entities
WHERE world_id = $1
ORDER BY entity_type ASC, code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("list city economic entities: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]*CityEconomicEntity, 0)
	for rows.Next() {
		item := &CityEconomicEntity{Accounts: make([]*CityAccount, 0)}
		var ownerUserID sql.NullInt64
		var metadata []byte
		if err = rows.Scan(&item.ID, &item.WorldID, &item.EntityType, &item.Code, &item.Name,
			&ownerUserID, &item.Status, &metadata, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.OwnerUserID = nullInt64Pointer(ownerUserID)
		item.Metadata, err = decodeCityJSONMap(metadata)
		if err != nil {
			return nil, fmt.Errorf("decode city economic entity metadata: %w", err)
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate city economic entities: %w", err)
	}
	return items, nil
}

func loadCityAccounts(ctx context.Context, queryer citySQLQueryer, worldID int64, entities []*CityEconomicEntity) error {
	byID := make(map[int64]*CityEconomicEntity, len(entities))
	for _, entity := range entities {
		byID[entity.ID] = entity
	}
	rows, err := queryer.QueryContext(ctx, `
SELECT a.id, a.world_id, a.entity_id, a.entity_type, a.monetary_unit_id, a.template_id,
       t.code, t.name, t.account_class, t.normal_side, a.allow_negative,
       a.current_balance_units, a.version, a.status, a.metadata, a.created_at, a.updated_at
FROM city_accounts a
JOIN city_account_templates t ON t.id = a.template_id
WHERE a.world_id = $1
ORDER BY a.entity_id ASC, t.sort_order ASC, a.id ASC`, worldID)
	if err != nil {
		return fmt.Errorf("list city accounts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		item := &CityAccount{}
		var metadata []byte
		if err = rows.Scan(&item.ID, &item.WorldID, &item.EntityID, &item.EntityType,
			&item.MonetaryUnitID, &item.TemplateID, &item.Code, &item.Name,
			&item.AccountClass, &item.NormalSide, &item.AllowNegative,
			&item.CurrentBalanceUnits, &item.Version, &item.Status, &metadata,
			&item.CreatedAt, &item.UpdatedAt); err != nil {
			return err
		}
		item.Metadata, err = decodeCityJSONMap(metadata)
		if err != nil {
			return fmt.Errorf("decode city account metadata: %w", err)
		}
		entity := byID[item.EntityID]
		if entity == nil {
			return fmt.Errorf("city account %d references an unloaded entity", item.ID)
		}
		entity.Accounts = append(entity.Accounts, item)
	}
	if err = rows.Err(); err != nil {
		return fmt.Errorf("iterate city accounts: %w", err)
	}
	return nil
}

func normalizeCityWorldCreateInput(input CityWorldCreateInput) (*normalizedCityWorldCreateInput, error) {
	name := strings.TrimSpace(input.Name)
	if input.OwnerUserID <= 0 || utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 80 {
		return nil, ErrCityInvalidInput
	}
	timezone := strings.TrimSpace(input.Timezone)
	if timezone == "" {
		timezone = "UTC"
	}
	if len(timezone) > 64 {
		return nil, ErrCityInvalidInput
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return nil, ErrCityInvalidInput
	}
	simulationVersion := strings.TrimSpace(input.SimulationVersion)
	if simulationVersion == "" {
		simulationVersion = CurrentCitySimulationVersion
	}
	if _, err := cityEngineForVersion(simulationVersion); err != nil {
		return nil, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "simulation_version"})
	}

	code := strings.ToLower(strings.TrimSpace(input.MonetaryUnit.Code))
	if code == "" {
		code = "credit"
	}
	if !cityMonetaryUnitCodePattern.MatchString(code) {
		return nil, ErrCityInvalidInput
	}
	unitName := strings.TrimSpace(input.MonetaryUnit.Name)
	if unitName == "" {
		unitName = "City Credit"
	}
	if utf8.RuneCountInString(unitName) > 64 {
		return nil, ErrCityInvalidInput
	}
	unitSymbol := strings.TrimSpace(input.MonetaryUnit.Symbol)
	if unitSymbol == "" {
		unitSymbol = "C"
	}
	if utf8.RuneCountInString(unitSymbol) > 16 {
		return nil, ErrCityInvalidInput
	}
	unitScale := 2
	if input.MonetaryUnit.Scale != nil {
		unitScale = *input.MonetaryUnit.Scale
	}
	if unitScale < 0 || unitScale > 8 {
		return nil, ErrCityInvalidInput
	}
	startAt := cityTickEpochTime
	if input.StartAt != nil {
		startAt = input.StartAt.UTC()
		if startAt.IsZero() || !startAt.Equal(startAt.Truncate(time.Hour)) {
			return nil, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "start_at"})
		}
	}
	return &normalizedCityWorldCreateInput{
		ownerUserID:       input.OwnerUserID,
		name:              name,
		timezone:          timezone,
		simulationVersion: simulationVersion,
		unitCode:          code,
		unitName:          unitName,
		unitSymbol:        unitSymbol,
		unitScale:         unitScale,
		startAt:           startAt,
	}, nil
}

func newCitySeed() (int64, error) {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return 0, err
	}
	seed := int64(binary.BigEndian.Uint64(raw[:]) & uint64(1<<63-1))
	if seed == 0 {
		seed = 1
	}
	return seed, nil
}

func mapCityCreateError(err error) error {
	var pqErr *pq.Error
	if !errors.As(err, &pqErr) {
		return fmt.Errorf("create city world: %w", err)
	}
	if pqErr.Code == "23505" && pqErr.Constraint == "idx_city_worlds_one_private_active_per_owner" {
		return ErrCityWorldExists
	}
	if pqErr.Code == "23503" || pqErr.Code == "23514" {
		return ErrCityInvalidInput
	}
	return fmt.Errorf("create city world: %w", err)
}

func decodeCityJSONMap(raw []byte) (map[string]any, error) {
	result := make(map[string]any)
	if len(raw) == 0 {
		return result, nil
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func nullInt64Pointer(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	return &value.Int64
}

func nullTimePointer(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	return &value.Time
}

func defaultCityEntities() []cityEntitySeed {
	return []cityEntitySeed{
		{entityType: CityEntityTypeHousehold, code: "founding_household", name: "Founding Household", owned: true},
		{entityType: CityEntityTypeFirm, code: "municipal_services", name: "Municipal Services"},
		{entityType: CityEntityTypeGovernment, code: "city_government", name: "City Government"},
		{entityType: CityEntityTypeClearing, code: "market_clearing", name: "Market Clearing"},
	}
}

func defaultCityAccountTemplates() []cityAccountTemplateSeed {
	account := func(entityType, code, name, class, side string, allowNegative bool, order int) cityAccountTemplateSeed {
		return cityAccountTemplateSeed{
			entityType: entityType, code: code, name: name, accountClass: class,
			normalSide: side, allowNegative: allowNegative, sortOrder: order,
		}
	}
	return []cityAccountTemplateSeed{
		account(CityEntityTypeHousehold, "cash", "Cash", "asset", "debit", false, 10),
		account(CityEntityTypeHousehold, "receivable", "Receivable", "asset", "debit", false, 20),
		account(CityEntityTypeHousehold, "payable", "Payable", "liability", "credit", false, 30),
		account(CityEntityTypeHousehold, "capital", "Capital", "equity", "credit", false, 40),
		account(CityEntityTypeHousehold, "wage_income", "Wage Income", "revenue", "credit", false, 50),
		account(CityEntityTypeHousehold, "other_income", "Other Income", "revenue", "credit", false, 55),
		account(CityEntityTypeHousehold, "consumption_expense", "Consumption Expense", "expense", "debit", false, 60),
		account(CityEntityTypeHousehold, "rent_expense", "Rent Expense", "expense", "debit", false, 70),
		account(CityEntityTypeHousehold, "transfer_expense", "Transfer Expense", "expense", "debit", false, 75),
		account(CityEntityTypeHousehold, "tax_expense", "Tax Expense", "expense", "debit", false, 80),

		account(CityEntityTypeFirm, "cash", "Cash", "asset", "debit", false, 10),
		account(CityEntityTypeFirm, "accounts_receivable", "Accounts Receivable", "asset", "debit", false, 20),
		account(CityEntityTypeFirm, "inventory", "Inventory", "asset", "debit", false, 30),
		account(CityEntityTypeFirm, "fixed_assets", "Fixed Assets", "asset", "debit", false, 40),
		account(CityEntityTypeFirm, "accounts_payable", "Accounts Payable", "liability", "credit", false, 50),
		account(CityEntityTypeFirm, "debt", "Debt", "liability", "credit", false, 60),
		account(CityEntityTypeFirm, "equity", "Equity", "equity", "credit", false, 70),
		account(CityEntityTypeFirm, "revenue", "Revenue", "revenue", "credit", false, 80),
		account(CityEntityTypeFirm, "other_income", "Other Income", "revenue", "credit", false, 85),
		account(CityEntityTypeFirm, "wage_expense", "Wage Expense", "expense", "debit", false, 90),
		account(CityEntityTypeFirm, "transfer_expense", "Transfer Expense", "expense", "debit", false, 95),
		account(CityEntityTypeFirm, "tax_expense", "Tax Expense", "expense", "debit", false, 100),

		account(CityEntityTypeGovernment, "cash", "Cash", "asset", "debit", false, 10),
		account(CityEntityTypeGovernment, "tax_receivable", "Tax Receivable", "asset", "debit", false, 20),
		account(CityEntityTypeGovernment, "public_assets", "Public Assets", "asset", "debit", false, 30),
		account(CityEntityTypeGovernment, "accounts_payable", "Accounts Payable", "liability", "credit", false, 40),
		account(CityEntityTypeGovernment, "debt", "Debt", "liability", "credit", false, 50),
		account(CityEntityTypeGovernment, "fund_balance", "Fund Balance", "equity", "credit", false, 60),
		account(CityEntityTypeGovernment, "tax_revenue", "Tax Revenue", "revenue", "credit", false, 70),
		account(CityEntityTypeGovernment, "rental_revenue", "Rental Revenue", "revenue", "credit", false, 75),
		account(CityEntityTypeGovernment, "public_service_expense", "Public Service Expense", "expense", "debit", false, 80),
		account(CityEntityTypeGovernment, "capital_expenditure", "Capital Expenditure", "expense", "debit", false, 90),
		account(CityEntityTypeGovernment, "subsidy_expense", "Subsidy Expense", "expense", "debit", false, 100),

		account(CityEntityTypeClearing, "goods_receivable", "Goods Receivable", "asset", "debit", false, 10),
		account(CityEntityTypeClearing, "goods_payable", "Goods Payable", "liability", "credit", false, 20),
		account(CityEntityTypeClearing, "payroll_receivable", "Payroll Receivable", "asset", "debit", false, 30),
		account(CityEntityTypeClearing, "payroll_payable", "Payroll Payable", "liability", "credit", false, 40),
		account(CityEntityTypeClearing, "rounding", "Rounding Difference", "asset", "debit", true, 50),
	}
}
