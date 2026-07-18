package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/lib/pq"
)

const (
	cityEnterpriseRejectionFirmNotFound       = "CITY_ENTERPRISE_FIRM_NOT_FOUND"
	cityEnterpriseRejectionSiteNotFound       = "CITY_ENTERPRISE_SITE_NOT_FOUND"
	cityEnterpriseRejectionPoolNotFound       = "CITY_ENTERPRISE_POOL_NOT_FOUND"
	cityEnterpriseRejectionUseIncompatible    = "CITY_ENTERPRISE_USE_INCOMPATIBLE"
	cityEnterpriseRejectionCapacity           = "CITY_ENTERPRISE_CAPACITY_INSUFFICIENT"
	cityEnterpriseRejectionMinimumCapacity    = "CITY_ENTERPRISE_MINIMUM_CAPACITY"
	cityEnterpriseRejectionSiteLimit          = "CITY_ENTERPRISE_SITE_LIMIT"
	cityEnterpriseRejectionRequiredSite       = "CITY_ENTERPRISE_REQUIRED_SITE"
	cityEnterpriseRejectionStateConflict      = "CITY_ENTERPRISE_STATE_CONFLICT"
	cityEnterpriseRejectionRelocationConflict = "CITY_ENTERPRISE_RELOCATION_CONFLICT"
	cityEnterpriseRejectionInventoryTransfer  = "CITY_ENTERPRISE_INVENTORY_TRANSFER"

	cityEnterpriseMaximumOccupiedUnits int64 = 1_000_000_000
)

var cityEnterpriseLongCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{1,159}$`)

type cityEnterpriseSiteOpenPayload struct {
	FirmEntityID        int64  `json:"firm_entity_id"`
	PoolCode            string `json:"pool_code"`
	SiteType            string `json:"site_type"`
	Name                string `json:"name"`
	TargetOccupiedUnits *int64 `json:"target_occupied_units,omitempty"`
}

type cityEnterpriseSiteResizePayload struct {
	SiteCode            string `json:"site_code"`
	TargetOccupiedUnits int64  `json:"target_occupied_units"`
}

type cityEnterpriseSiteClosePayload struct {
	SiteCode string `json:"site_code"`
	Reason   string `json:"reason"`
}

type cityEnterpriseRelocatePayload struct {
	FirmEntityID         int64  `json:"firm_entity_id"`
	HeadquartersPoolCode string `json:"headquarters_pool_code"`
	ProductionPoolCode   string `json:"production_pool_code"`
	Reason               string `json:"reason"`
}

type cityEnterpriseBusinessError struct{ code string }

func (err *cityEnterpriseBusinessError) Error() string { return err.code }

func cityEnterpriseReject(code string) error {
	return &cityEnterpriseBusinessError{code: code}
}

func cityEnterpriseBusinessRejectionCode(err error) string {
	var businessErr *cityEnterpriseBusinessError
	if errors.As(err, &businessErr) {
		return businessErr.code
	}
	if code := cityResourceBusinessRejectionCode(err); code != "" {
		return cityEnterpriseRejectionInventoryTransfer
	}
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		switch {
		case strings.Contains(pqErr.Message, "enterprise occupancy exceeds"),
			strings.Contains(pqErr.Message, "capacity"):
			return cityEnterpriseRejectionCapacity
		case strings.Contains(pqErr.Constraint, "one_active_headquarters"),
			strings.Contains(pqErr.Constraint, "one_primary_per_type"):
			return cityEnterpriseRejectionRequiredSite
		}
	}
	return ""
}

func normalizeCityEnterpriseLocationCommand(commandType string, rawPayload json.RawMessage) (any, bool, error) {
	normalizeCode := func(value *string) error {
		*value = strings.ToLower(strings.TrimSpace(*value))
		if !cityEnterpriseLongCodePattern.MatchString(*value) {
			return ErrCityInvalidInput
		}
		return nil
	}
	normalizeText := func(value *string, required bool, maximum int) error {
		*value = strings.TrimSpace(*value)
		length := utf8.RuneCountInString(*value)
		if length > maximum || required && length == 0 {
			return ErrCityInvalidInput
		}
		return nil
	}
	validUnits := func(value int64) bool {
		return value > 0 && value <= cityEnterpriseMaximumOccupiedUnits
	}

	switch commandType {
	case CityCommandTypeEnterpriseSiteOpen:
		var payload cityEnterpriseSiteOpenPayload
		if err := decodeStrictCityObject(rawPayload, &payload); err != nil {
			return nil, true, ErrCityInvalidInput.WithCause(err)
		}
		payload.SiteType = strings.ToLower(strings.TrimSpace(payload.SiteType))
		if payload.FirmEntityID <= 0 || normalizeCode(&payload.PoolCode) != nil ||
			normalizeText(&payload.Name, true, 128) != nil {
			return nil, true, ErrCityInvalidInput
		}
		if _, allowed := cityEnterpriseAllowedPoolUse(payload.SiteType); !allowed {
			return nil, true, ErrCityInvalidInput
		}
		if payload.TargetOccupiedUnits != nil && !validUnits(*payload.TargetOccupiedUnits) {
			return nil, true, ErrCityInvalidInput
		}
		return payload, true, nil
	case CityCommandTypeEnterpriseSiteResize:
		var payload cityEnterpriseSiteResizePayload
		if err := decodeStrictCityObject(rawPayload, &payload); err != nil {
			return nil, true, ErrCityInvalidInput.WithCause(err)
		}
		if normalizeCode(&payload.SiteCode) != nil || !validUnits(payload.TargetOccupiedUnits) {
			return nil, true, ErrCityInvalidInput
		}
		return payload, true, nil
	case CityCommandTypeEnterpriseSiteClose:
		var payload cityEnterpriseSiteClosePayload
		if err := decodeStrictCityObject(rawPayload, &payload); err != nil {
			return nil, true, ErrCityInvalidInput.WithCause(err)
		}
		if normalizeCode(&payload.SiteCode) != nil || normalizeText(&payload.Reason, true, 256) != nil {
			return nil, true, ErrCityInvalidInput
		}
		return payload, true, nil
	case CityCommandTypeEnterpriseRelocate:
		var payload cityEnterpriseRelocatePayload
		if err := decodeStrictCityObject(rawPayload, &payload); err != nil {
			return nil, true, ErrCityInvalidInput.WithCause(err)
		}
		if payload.FirmEntityID <= 0 || normalizeCode(&payload.HeadquartersPoolCode) != nil ||
			normalizeCode(&payload.ProductionPoolCode) != nil ||
			payload.HeadquartersPoolCode == payload.ProductionPoolCode ||
			normalizeText(&payload.Reason, true, 256) != nil {
			return nil, true, ErrCityInvalidInput
		}
		return payload, true, nil
	default:
		return nil, false, nil
	}
}

type cityEnterpriseFirmRef struct {
	entityID                int64
	entityCode              string
	name                    string
	districtID              int64
	districtCode            string
	employeeUnits           int64
	capitalStockUnits       int64
	productionCapacityUnits int64
	version                 int64
}

type cityEnterprisePoolRef struct {
	id                 int64
	code               string
	buildingID         int64
	buildingCode       string
	districtID         int64
	districtCode       string
	useType            string
	effectiveUnitCount int64
	occupiedUnitCount  int64
}

type cityEnterpriseSiteRef struct {
	id   int64
	firm cityEnterpriseFirmRef
	pool cityEnterprisePoolRef
	site CityEnterpriseSite
}

type cityEnterpriseLocationFactRecord struct {
	id   int64
	fact CityEnterpriseLocationFact
}

type cityEnterpriseLocationExecution struct {
	pending            cityPendingEvent
	fact               *CityEnterpriseLocationFact
	resourceOperations []*CityResourceOperation
}

func (s *CityEconomyService) applyCityEnterpriseLocationCommand(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, resourceSequence int64,
	command *CityCommand,
) (cityEnterpriseLocationExecution, error) {
	if _, err := tx.ExecContext(ctx, `SAVEPOINT city_enterprise_location_command`); err != nil {
		return cityEnterpriseLocationExecution{}, fmt.Errorf("create city enterprise location command savepoint: %w", err)
	}
	execution, err := s.postCityEnterpriseLocationCommand(
		ctx, tx, worldID, targetTick, factSequence, resourceSequence, command,
	)
	if err != nil {
		if _, rollbackErr := tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT city_enterprise_location_command`); rollbackErr != nil {
			return cityEnterpriseLocationExecution{}, fmt.Errorf(
				"rollback city enterprise location command savepoint after %v: %w", err, rollbackErr,
			)
		}
		if _, releaseErr := tx.ExecContext(ctx, `RELEASE SAVEPOINT city_enterprise_location_command`); releaseErr != nil {
			return cityEnterpriseLocationExecution{}, fmt.Errorf("release rejected city enterprise location command savepoint: %w", releaseErr)
		}
		if code := cityEnterpriseBusinessRejectionCode(err); code != "" {
			return cityEnterpriseLocationExecution{pending: rejectedCityCommand(command, code)}, nil
		}
		return cityEnterpriseLocationExecution{}, err
	}
	if _, err = tx.ExecContext(ctx, `RELEASE SAVEPOINT city_enterprise_location_command`); err != nil {
		return cityEnterpriseLocationExecution{}, fmt.Errorf("release city enterprise location command savepoint: %w", err)
	}
	return execution, nil
}

func (s *CityEconomyService) postCityEnterpriseLocationCommand(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, resourceSequence int64,
	command *CityCommand,
) (cityEnterpriseLocationExecution, error) {
	switch command.CommandType {
	case CityCommandTypeEnterpriseSiteOpen:
		payload, err := decodeStoredCityCommandPayload[cityEnterpriseSiteOpenPayload](command)
		if err != nil {
			return cityEnterpriseLocationExecution{}, err
		}
		return s.openCityEnterpriseSite(ctx, tx, worldID, targetTick, factSequence, command, payload)
	case CityCommandTypeEnterpriseSiteResize:
		payload, err := decodeStoredCityCommandPayload[cityEnterpriseSiteResizePayload](command)
		if err != nil {
			return cityEnterpriseLocationExecution{}, err
		}
		return s.resizeCityEnterpriseSite(ctx, tx, worldID, targetTick, factSequence, command, payload)
	case CityCommandTypeEnterpriseSiteClose:
		payload, err := decodeStoredCityCommandPayload[cityEnterpriseSiteClosePayload](command)
		if err != nil {
			return cityEnterpriseLocationExecution{}, err
		}
		return s.closeCityEnterpriseSite(ctx, tx, worldID, targetTick, factSequence, command, payload)
	case CityCommandTypeEnterpriseRelocate:
		payload, err := decodeStoredCityCommandPayload[cityEnterpriseRelocatePayload](command)
		if err != nil {
			return cityEnterpriseLocationExecution{}, err
		}
		return s.relocateCityEnterprise(ctx, tx, worldID, targetTick, factSequence, resourceSequence, command, payload)
	default:
		return cityEnterpriseLocationExecution{}, ErrCitySimulationInvariant.WithMetadata(
			map[string]string{"command_type": command.CommandType},
		)
	}
}

func loadCityEnterpriseFirmRef(
	ctx context.Context,
	tx *sql.Tx,
	worldID, firmEntityID int64,
) (*cityEnterpriseFirmRef, error) {
	firm := &cityEnterpriseFirmRef{}
	err := tx.QueryRowContext(ctx, `
SELECT entity.id, entity.code, entity.name, district.id, district.code,
       firm.employee_units, firm.capital_stock_units, firm.production_capacity_units,
       firm.version
FROM city_economic_entities entity
JOIN city_firm_states firm
  ON firm.entity_id = entity.id AND firm.world_id = entity.world_id
JOIN city_districts district
  ON district.id = firm.district_id AND district.world_id = firm.world_id
WHERE entity.world_id = $1 AND entity.id = $2 AND entity.entity_type = 'firm'
  AND entity.status = 'active'
FOR UPDATE OF entity, firm`, worldID, firmEntityID).Scan(
		&firm.entityID, &firm.entityCode, &firm.name, &firm.districtID,
		&firm.districtCode, &firm.employeeUnits, &firm.capitalStockUnits,
		&firm.productionCapacityUnits, &firm.version,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, cityEnterpriseReject(cityEnterpriseRejectionFirmNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("load city enterprise firm: %w", err)
	}
	return firm, nil
}

func loadCityEnterprisePoolRef(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	poolCode string,
) (*cityEnterprisePoolRef, error) {
	pool := &cityEnterprisePoolRef{}
	var baselineUnits, capacityPerUnit, addedCapacity int64
	err := tx.QueryRowContext(ctx, `
SELECT pool.id, pool.code, building.id, building.code, district.id, district.code,
       pool.use_type, pool.unit_count, pool.capacity_units_per_unit,
       COALESCE((
           SELECT SUM(adjustment.added_capacity_units)::BIGINT
           FROM city_building_adjustments adjustment
           WHERE adjustment.world_id = pool.world_id
             AND adjustment.building_id = pool.building_id
       ), 0)::BIGINT
FROM city_building_unit_pools pool
JOIN city_buildings building
  ON building.id = pool.building_id AND building.world_id = pool.world_id
JOIN city_parcels parcel
  ON parcel.id = building.parcel_id AND parcel.world_id = building.world_id
JOIN city_districts district
  ON district.id = pool.district_id AND district.world_id = pool.world_id
WHERE pool.world_id = $1 AND pool.code = $2
  AND building.status = 'active' AND parcel.status = 'active'
FOR UPDATE OF pool, building, parcel`, worldID, poolCode).Scan(
		&pool.id, &pool.code, &pool.buildingID, &pool.buildingCode,
		&pool.districtID, &pool.districtCode, &pool.useType,
		&baselineUnits, &capacityPerUnit, &addedCapacity,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, cityEnterpriseReject(cityEnterpriseRejectionPoolNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("load city enterprise building pool: %w", err)
	}
	if capacityPerUnit <= 0 || addedCapacity < 0 || baselineUnits < 0 {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "enterprise_pool_capacity"})
	}
	pool.effectiveUnitCount = baselineUnits + addedCapacity/capacityPerUnit
	if err = tx.QueryRowContext(ctx, `
SELECT COALESCE(SUM(site.occupied_units), 0)::BIGINT
FROM city_enterprise_sites site
WHERE site.world_id = $1 AND site.pool_id = $2 AND site.status = 'active'`,
		worldID, pool.id).Scan(&pool.occupiedUnitCount); err != nil {
		return nil, fmt.Errorf("load city enterprise building pool occupancy: %w", err)
	}
	return pool, nil
}

func loadCityEnterpriseSiteRef(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	siteCode string,
) (*cityEnterpriseSiteRef, error) {
	ref := &cityEnterpriseSiteRef{}
	var closedTick sql.NullInt64
	err := tx.QueryRowContext(ctx, `
SELECT site.id, site.code, firm.id, firm.code, firm.name,
       firm_district.id, firm_district.code,
       firm_state.employee_units, firm_state.capital_stock_units,
       firm_state.production_capacity_units, firm_state.version,
       pool.id, pool.code, building.id, building.code, site_district.id,
       site_district.code, pool.use_type,
       site.site_type, site.name, site.occupied_units, site.is_primary,
       site.status, site.opened_tick, site.last_changed_tick, site.closed_tick,
       site.version, site.metadata
FROM city_enterprise_sites site
JOIN city_economic_entities firm
  ON firm.id = site.firm_entity_id AND firm.world_id = site.world_id
JOIN city_firm_states firm_state
  ON firm_state.entity_id = firm.id AND firm_state.world_id = firm.world_id
JOIN city_districts firm_district
  ON firm_district.id = firm_state.district_id AND firm_district.world_id = firm_state.world_id
JOIN city_building_unit_pools pool
  ON pool.id = site.pool_id AND pool.world_id = site.world_id
JOIN city_buildings building
  ON building.id = site.building_id AND building.world_id = site.world_id
JOIN city_districts site_district
  ON site_district.id = site.district_id AND site_district.world_id = site.world_id
WHERE site.world_id = $1 AND site.code = $2 AND firm.status = 'active'
FOR UPDATE OF site, firm_state`, worldID, siteCode).Scan(
		&ref.id, &ref.site.Code, &ref.firm.entityID, &ref.firm.entityCode,
		&ref.firm.name, &ref.firm.districtID, &ref.firm.districtCode,
		&ref.firm.employeeUnits, &ref.firm.capitalStockUnits,
		&ref.firm.productionCapacityUnits, &ref.firm.version,
		&ref.pool.id, &ref.pool.code, &ref.pool.buildingID, &ref.pool.buildingCode,
		&ref.pool.districtID, &ref.pool.districtCode, &ref.pool.useType,
		&ref.site.SiteType, &ref.site.Name, &ref.site.OccupiedUnits,
		&ref.site.IsPrimary, &ref.site.Status, &ref.site.OpenedTick,
		&ref.site.LastChangedTick, &closedTick, &ref.site.Version, &ref.site.Metadata,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, cityEnterpriseReject(cityEnterpriseRejectionSiteNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("load city enterprise site: %w", err)
	}
	ref.site.FirmEntityCode = ref.firm.entityCode
	ref.site.DistrictCode = ref.pool.districtCode
	ref.site.BuildingCode = ref.pool.buildingCode
	ref.site.PoolCode = ref.pool.code
	ref.site.ClosedTick = nullableInt64Pointer(closedTick)
	return ref, nil
}

func cityEnterpriseRequiredUnits(firm *cityEnterpriseFirmRef, siteType string) (int64, error) {
	units, err := cityEnterpriseMinimumOccupiedUnits(
		siteType, firm.employeeUnits, firm.capitalStockUnits, firm.productionCapacityUnits,
	)
	if err != nil {
		return 0, ErrCitySimulationInvariant.WithCause(err)
	}
	return units, nil
}

func validateCityEnterprisePoolUse(pool *cityEnterprisePoolRef, siteType string) error {
	allowedUse, valid := cityEnterpriseAllowedPoolUse(siteType)
	if !valid || pool.useType != allowedUse {
		return cityEnterpriseReject(cityEnterpriseRejectionUseIncompatible)
	}
	return nil
}

func validateCityEnterprisePoolCapacity(pool *cityEnterprisePoolRef, requested, replacing int64) error {
	if requested <= 0 || replacing < 0 || pool.effectiveUnitCount < pool.occupiedUnitCount ||
		requested > pool.effectiveUnitCount-pool.occupiedUnitCount+replacing {
		return cityEnterpriseReject(cityEnterpriseRejectionCapacity)
	}
	return nil
}

func insertCityEnterpriseLocationFact(
	ctx context.Context,
	tx *sql.Tx,
	worldID, tick, sequence int64,
	command *CityCommand,
	firmID int64,
	siteCode *string,
	factType string,
	fromStatus, toStatus *string,
	occupiedBefore, occupiedAfter, versionBefore, versionAfter int64,
	metadata cityEnterpriseLocationFactMetadata,
) (*cityEnterpriseLocationFactRecord, error) {
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("marshal city enterprise location fact metadata: %w", err)
	}
	var factID int64
	err = tx.QueryRowContext(ctx, `
INSERT INTO city_enterprise_location_facts
    (world_id, tick, sequence, source_command_id, firm_entity_id, entity_type,
     site_code, fact_type, from_status, to_status, occupied_before_units,
     occupied_after_units, site_version_before, site_version_after, metadata)
VALUES ($1, $2, $3, $4, $5, 'firm', $6, $7, $8, $9, $10, $11, $12, $13, $14::jsonb)
RETURNING id`, worldID, tick, sequence, command.ID, firmID,
		enterpriseNullableString(siteCode), factType, enterpriseNullableString(fromStatus),
		enterpriseNullableString(toStatus), occupiedBefore, occupiedAfter,
		versionBefore, versionAfter, metadataJSON).Scan(&factID)
	if err != nil {
		return nil, fmt.Errorf("insert city enterprise location fact draft: %w", err)
	}
	if _, err = tx.ExecContext(ctx,
		`SELECT set_config('sub2api.city_enterprise_location_fact_id', $1, TRUE)`,
		strconv.FormatInt(factID, 10)); err != nil {
		return nil, fmt.Errorf("activate city enterprise location fact write gate: %w", err)
	}
	fact := CityEnterpriseLocationFact{
		Tick: tick, Sequence: sequence, SourceCommandSequence: command.Sequence,
		SiteCode: siteCode, FactType: factType, FromStatus: fromStatus, ToStatus: toStatus,
		OccupiedBeforeUnits: occupiedBefore, OccupiedAfterUnits: occupiedAfter,
		SiteVersionBefore: versionBefore, SiteVersionAfter: versionAfter,
		Metadata: metadataJSON,
	}
	return &cityEnterpriseLocationFactRecord{id: factID, fact: fact}, nil
}

func postCityEnterpriseLocationFact(ctx context.Context, tx *sql.Tx, factID int64) error {
	result, err := tx.ExecContext(ctx, `
UPDATE city_enterprise_location_facts SET posted_at = NOW()
WHERE id = $1 AND posted_at IS NULL`, factID)
	if err != nil {
		return fmt.Errorf("post city enterprise location fact: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{
			"enterprise_location_fact_id": strconv.FormatInt(factID, 10),
		})
	}
	return nil
}

func advanceCityEnterpriseLocationProfile(
	ctx context.Context,
	tx *sql.Tx,
	worldID, siteDelta int64,
) error {
	result, err := tx.ExecContext(ctx, `
UPDATE city_enterprise_location_profiles
SET site_count = site_count + $2, fact_count = fact_count + 1,
    revision = revision + 1, updated_at = NOW()
WHERE world_id = $1`, worldID, siteDelta)
	if err != nil {
		return fmt.Errorf("advance city enterprise location profile: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "enterprise_location_profile"})
	}
	return nil
}

func (s *CityEconomyService) openCityEnterpriseSite(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence int64,
	command *CityCommand,
	payload cityEnterpriseSiteOpenPayload,
) (cityEnterpriseLocationExecution, error) {
	firm, err := loadCityEnterpriseFirmRef(ctx, tx, worldID, payload.FirmEntityID)
	if err != nil {
		return cityEnterpriseLocationExecution{}, err
	}
	var activeSiteCount, activeHeadquarters int64
	if err = tx.QueryRowContext(ctx, `
SELECT COUNT(*)::BIGINT,
       COUNT(*) FILTER (WHERE site_type = 'headquarters')::BIGINT
FROM city_enterprise_sites
WHERE world_id = $1 AND firm_entity_id = $2 AND status = 'active'`,
		worldID, firm.entityID).Scan(&activeSiteCount, &activeHeadquarters); err != nil {
		return cityEnterpriseLocationExecution{}, fmt.Errorf("inspect city enterprise active sites: %w", err)
	}
	if activeSiteCount >= cityEnterpriseMaximumActiveSitesPerFirm {
		return cityEnterpriseLocationExecution{}, cityEnterpriseReject(cityEnterpriseRejectionSiteLimit)
	}
	if payload.SiteType == CityEnterpriseSiteHeadquarters && activeHeadquarters != 0 {
		return cityEnterpriseLocationExecution{}, cityEnterpriseReject(cityEnterpriseRejectionRequiredSite)
	}
	pool, err := loadCityEnterprisePoolRef(ctx, tx, worldID, payload.PoolCode)
	if err != nil {
		return cityEnterpriseLocationExecution{}, err
	}
	if err = validateCityEnterprisePoolUse(pool, payload.SiteType); err != nil {
		return cityEnterpriseLocationExecution{}, err
	}
	minimum, err := cityEnterpriseRequiredUnits(firm, payload.SiteType)
	if err != nil {
		return cityEnterpriseLocationExecution{}, err
	}
	targetUnits := minimum
	if payload.TargetOccupiedUnits != nil {
		targetUnits = *payload.TargetOccupiedUnits
	}
	if targetUnits < minimum {
		return cityEnterpriseLocationExecution{}, cityEnterpriseReject(cityEnterpriseRejectionMinimumCapacity)
	}
	if err = validateCityEnterprisePoolCapacity(pool, targetUnits, 0); err != nil {
		return cityEnterpriseLocationExecution{}, err
	}

	siteCode := "enterprise_site_" + strconv.FormatInt(command.Sequence, 10)
	site := CityEnterpriseSite{
		Code: siteCode, FirmEntityCode: firm.entityCode,
		DistrictCode: pool.districtCode, BuildingCode: pool.buildingCode, PoolCode: pool.code,
		SiteType: payload.SiteType, Name: payload.Name, OccupiedUnits: targetUnits,
		IsPrimary: payload.SiteType == CityEnterpriseSiteHeadquarters,
		Status:    CityEnterpriseSiteStatusActive, OpenedTick: targetTick,
		LastChangedTick: targetTick, Version: 1,
		Metadata: json.RawMessage(`{"policy_hash":"` + cityEnterpriseLocationPolicyHash + `","schema_version":1,"source":"command"}`),
	}
	active := CityEnterpriseSiteStatusActive
	fact, err := insertCityEnterpriseLocationFact(
		ctx, tx, worldID, targetTick, factSequence, command, firm.entityID,
		&siteCode, CityEnterpriseLocationFactOpened, nil, &active,
		0, targetUnits, 0, 1,
		cityEnterpriseLocationFactMetadata{SchemaVersion: 1, Site: &site},
	)
	if err != nil {
		return cityEnterpriseLocationExecution{}, err
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_enterprise_sites
    (world_id, code, firm_entity_id, entity_type, district_id, building_id, pool_id,
     site_type, name, occupied_units, is_primary, status, opened_tick,
     last_changed_tick, version, metadata)
VALUES ($1, $2, $3, 'firm', $4, $5, $6, $7, $8, $9, $10, 'active', $11, $11, 1, $12::jsonb)`,
		worldID, site.Code, firm.entityID, pool.districtID, pool.buildingID, pool.id,
		site.SiteType, site.Name, site.OccupiedUnits, site.IsPrimary, targetTick,
		string(site.Metadata)); err != nil {
		return cityEnterpriseLocationExecution{}, fmt.Errorf("insert city enterprise site: %w", err)
	}
	if err = advanceCityEnterpriseLocationProfile(ctx, tx, worldID, 1); err != nil {
		return cityEnterpriseLocationExecution{}, err
	}
	if err = postCityEnterpriseLocationFact(ctx, tx, fact.id); err != nil {
		return cityEnterpriseLocationExecution{}, err
	}
	fact.fact.FirmEntityCode = firm.entityCode
	return cityEnterpriseLocationExecution{
		pending: cityEnterpriseAppliedEvent(command, "city.enterprise.site_opened", firm.entityCode, &site, fact.fact, nil),
		fact:    &fact.fact, resourceOperations: make([]*CityResourceOperation, 0),
	}, nil
}

func (s *CityEconomyService) resizeCityEnterpriseSite(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence int64,
	command *CityCommand,
	payload cityEnterpriseSiteResizePayload,
) (cityEnterpriseLocationExecution, error) {
	ref, err := loadCityEnterpriseSiteRef(ctx, tx, worldID, payload.SiteCode)
	if err != nil {
		return cityEnterpriseLocationExecution{}, err
	}
	if ref.site.Status != CityEnterpriseSiteStatusActive || payload.TargetOccupiedUnits == ref.site.OccupiedUnits {
		return cityEnterpriseLocationExecution{}, cityEnterpriseReject(cityEnterpriseRejectionStateConflict)
	}
	pool, err := loadCityEnterprisePoolRef(ctx, tx, worldID, ref.pool.code)
	if err != nil {
		return cityEnterpriseLocationExecution{}, err
	}
	minimum, err := cityEnterpriseRequiredUnits(&ref.firm, ref.site.SiteType)
	if err != nil {
		return cityEnterpriseLocationExecution{}, err
	}
	if payload.TargetOccupiedUnits < minimum {
		return cityEnterpriseLocationExecution{}, cityEnterpriseReject(cityEnterpriseRejectionMinimumCapacity)
	}
	if err = validateCityEnterprisePoolCapacity(pool, payload.TargetOccupiedUnits, ref.site.OccupiedUnits); err != nil {
		return cityEnterpriseLocationExecution{}, err
	}

	before := ref.site
	after := ref.site
	after.OccupiedUnits = payload.TargetOccupiedUnits
	after.LastChangedTick = targetTick
	after.Version++
	active := CityEnterpriseSiteStatusActive
	fact, err := insertCityEnterpriseLocationFact(
		ctx, tx, worldID, targetTick, factSequence, command, ref.firm.entityID,
		&after.Code, CityEnterpriseLocationFactResized, &active, &active,
		before.OccupiedUnits, after.OccupiedUnits, before.Version, after.Version,
		cityEnterpriseLocationFactMetadata{SchemaVersion: 1, Site: &after},
	)
	if err != nil {
		return cityEnterpriseLocationExecution{}, err
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_enterprise_sites
SET occupied_units = $3, last_changed_tick = $4, version = version + 1,
    updated_at = NOW()
WHERE world_id = $1 AND code = $2 AND status = 'active' AND version = $5`,
		worldID, after.Code, after.OccupiedUnits, targetTick, before.Version)
	if err != nil {
		return cityEnterpriseLocationExecution{}, fmt.Errorf("resize city enterprise site: %w", err)
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr != nil || rows != 1 {
		return cityEnterpriseLocationExecution{}, cityEnterpriseReject(cityEnterpriseRejectionStateConflict)
	}
	if err = advanceCityEnterpriseLocationProfile(ctx, tx, worldID, 0); err != nil {
		return cityEnterpriseLocationExecution{}, err
	}
	if err = postCityEnterpriseLocationFact(ctx, tx, fact.id); err != nil {
		return cityEnterpriseLocationExecution{}, err
	}
	fact.fact.FirmEntityCode = ref.firm.entityCode
	return cityEnterpriseLocationExecution{
		pending: cityEnterpriseAppliedEvent(command, "city.enterprise.site_resized", ref.firm.entityCode, &after, fact.fact, nil),
		fact:    &fact.fact, resourceOperations: make([]*CityResourceOperation, 0),
	}, nil
}

func (s *CityEconomyService) closeCityEnterpriseSite(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence int64,
	command *CityCommand,
	payload cityEnterpriseSiteClosePayload,
) (cityEnterpriseLocationExecution, error) {
	ref, err := loadCityEnterpriseSiteRef(ctx, tx, worldID, payload.SiteCode)
	if err != nil {
		return cityEnterpriseLocationExecution{}, err
	}
	if ref.site.Status != CityEnterpriseSiteStatusActive {
		return cityEnterpriseLocationExecution{}, cityEnterpriseReject(cityEnterpriseRejectionStateConflict)
	}
	if ref.site.SiteType == CityEnterpriseSiteHeadquarters || ref.site.IsPrimary {
		return cityEnterpriseLocationExecution{}, cityEnterpriseReject(cityEnterpriseRejectionRequiredSite)
	}
	if ref.site.SiteType == CityEnterpriseSiteProduction && ref.firm.productionCapacityUnits > 0 {
		var activeProduction int64
		if err = tx.QueryRowContext(ctx, `
SELECT COUNT(*)::BIGINT FROM city_enterprise_sites
WHERE world_id = $1 AND firm_entity_id = $2 AND site_type = 'production'
  AND status = 'active'`, worldID, ref.firm.entityID).Scan(&activeProduction); err != nil {
			return cityEnterpriseLocationExecution{}, fmt.Errorf("inspect city enterprise production sites: %w", err)
		}
		if activeProduction <= 1 {
			return cityEnterpriseLocationExecution{}, cityEnterpriseReject(cityEnterpriseRejectionRequiredSite)
		}
	}

	before := ref.site
	after := ref.site
	after.Status = CityEnterpriseSiteStatusClosed
	after.LastChangedTick = targetTick
	after.ClosedTick = &targetTick
	after.Version++
	active := CityEnterpriseSiteStatusActive
	closed := CityEnterpriseSiteStatusClosed
	fact, err := insertCityEnterpriseLocationFact(
		ctx, tx, worldID, targetTick, factSequence, command, ref.firm.entityID,
		&after.Code, CityEnterpriseLocationFactClosed, &active, &closed,
		before.OccupiedUnits, 0, before.Version, after.Version,
		cityEnterpriseLocationFactMetadata{SchemaVersion: 1, Site: &after, Reason: payload.Reason},
	)
	if err != nil {
		return cityEnterpriseLocationExecution{}, err
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_enterprise_sites
SET status = 'closed', last_changed_tick = $3, closed_tick = $3,
    version = version + 1, updated_at = NOW()
WHERE world_id = $1 AND code = $2 AND status = 'active' AND version = $4`,
		worldID, after.Code, targetTick, before.Version)
	if err != nil {
		return cityEnterpriseLocationExecution{}, fmt.Errorf("close city enterprise site: %w", err)
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr != nil || rows != 1 {
		return cityEnterpriseLocationExecution{}, cityEnterpriseReject(cityEnterpriseRejectionStateConflict)
	}
	if err = advanceCityEnterpriseLocationProfile(ctx, tx, worldID, 0); err != nil {
		return cityEnterpriseLocationExecution{}, err
	}
	if err = postCityEnterpriseLocationFact(ctx, tx, fact.id); err != nil {
		return cityEnterpriseLocationExecution{}, err
	}
	fact.fact.FirmEntityCode = ref.firm.entityCode
	return cityEnterpriseLocationExecution{
		pending: cityEnterpriseAppliedEvent(command, "city.enterprise.site_closed", ref.firm.entityCode, &after, fact.fact, nil),
		fact:    &fact.fact, resourceOperations: make([]*CityResourceOperation, 0),
	}, nil
}

func (s *CityEconomyService) relocateCityEnterprise(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, resourceSequence int64,
	command *CityCommand,
	payload cityEnterpriseRelocatePayload,
) (cityEnterpriseLocationExecution, error) {
	firm, err := loadCityEnterpriseFirmRef(ctx, tx, worldID, payload.FirmEntityID)
	if err != nil {
		return cityEnterpriseLocationExecution{}, err
	}
	var activeProjectCount int64
	if err = tx.QueryRowContext(ctx, `
SELECT COUNT(*)::BIGINT FROM city_development_projects
WHERE world_id = $1 AND developer_entity_id = $2 AND status = 'under_construction'`,
		worldID, firm.entityID).Scan(&activeProjectCount); err != nil {
		return cityEnterpriseLocationExecution{}, fmt.Errorf("inspect city enterprise relocation projects: %w", err)
	}
	if activeProjectCount != 0 {
		return cityEnterpriseLocationExecution{}, cityEnterpriseReject(cityEnterpriseRejectionRelocationConflict)
	}

	poolCodes := []string{payload.HeadquartersPoolCode, payload.ProductionPoolCode}
	sort.Strings(poolCodes)
	loadedPools := make(map[string]*cityEnterprisePoolRef, 2)
	for _, code := range poolCodes {
		pool, loadErr := loadCityEnterprisePoolRef(ctx, tx, worldID, code)
		if loadErr != nil {
			return cityEnterpriseLocationExecution{}, loadErr
		}
		loadedPools[code] = pool
	}
	headquartersPool := loadedPools[payload.HeadquartersPoolCode]
	productionPool := loadedPools[payload.ProductionPoolCode]
	if headquartersPool.districtID != productionPool.districtID ||
		headquartersPool.districtID == firm.districtID {
		return cityEnterpriseLocationExecution{}, cityEnterpriseReject(cityEnterpriseRejectionRelocationConflict)
	}
	if err = validateCityEnterprisePoolUse(headquartersPool, CityEnterpriseSiteHeadquarters); err != nil {
		return cityEnterpriseLocationExecution{}, err
	}
	if err = validateCityEnterprisePoolUse(productionPool, CityEnterpriseSiteProduction); err != nil {
		return cityEnterpriseLocationExecution{}, err
	}
	headquartersUnits, err := cityEnterpriseRequiredUnits(firm, CityEnterpriseSiteHeadquarters)
	if err != nil {
		return cityEnterpriseLocationExecution{}, err
	}
	productionUnits, err := cityEnterpriseRequiredUnits(firm, CityEnterpriseSiteProduction)
	if err != nil {
		return cityEnterpriseLocationExecution{}, err
	}
	if err = validateCityEnterprisePoolCapacity(headquartersPool, headquartersUnits, 0); err != nil {
		return cityEnterpriseLocationExecution{}, err
	}
	if err = validateCityEnterprisePoolCapacity(productionPool, productionUnits, 0); err != nil {
		return cityEnterpriseLocationExecution{}, err
	}

	primarySites, err := loadCityEnterprisePrimarySites(ctx, tx, worldID, firm.entityID)
	if err != nil {
		return cityEnterpriseLocationExecution{}, err
	}
	if len(primarySites) != 2 || primarySites[0].SiteType != CityEnterpriseSiteHeadquarters ||
		primarySites[1].SiteType != CityEnterpriseSiteProduction {
		return cityEnterpriseLocationExecution{}, cityEnterpriseReject(cityEnterpriseRejectionRequiredSite)
	}
	sitesBefore := append([]CityEnterpriseSite(nil), primarySites...)
	sitesAfter := make([]CityEnterpriseSite, 0, 4)
	for _, previous := range primarySites {
		closedSite := previous
		closedSite.Status = CityEnterpriseSiteStatusClosed
		closedSite.LastChangedTick = targetTick
		closedSite.ClosedTick = &targetTick
		closedSite.Version++
		sitesAfter = append(sitesAfter, closedSite)
	}
	newSites := []CityEnterpriseSite{
		{
			Code:           "enterprise_site_" + strconv.FormatInt(command.Sequence, 10) + "_headquarters",
			FirmEntityCode: firm.entityCode, DistrictCode: headquartersPool.districtCode,
			BuildingCode: headquartersPool.buildingCode, PoolCode: headquartersPool.code,
			SiteType: CityEnterpriseSiteHeadquarters, Name: firm.name + " Headquarters",
			OccupiedUnits: headquartersUnits, IsPrimary: true,
			Status: CityEnterpriseSiteStatusActive, OpenedTick: targetTick,
			LastChangedTick: targetTick, Version: 1,
			Metadata: json.RawMessage(`{"policy_hash":"` + cityEnterpriseLocationPolicyHash + `","schema_version":1,"source":"relocation"}`),
		},
		{
			Code:           "enterprise_site_" + strconv.FormatInt(command.Sequence, 10) + "_production",
			FirmEntityCode: firm.entityCode, DistrictCode: productionPool.districtCode,
			BuildingCode: productionPool.buildingCode, PoolCode: productionPool.code,
			SiteType: CityEnterpriseSiteProduction, Name: firm.name + " Production Site",
			OccupiedUnits: productionUnits, IsPrimary: true,
			Status: CityEnterpriseSiteStatusActive, OpenedTick: targetTick,
			LastChangedTick: targetTick, Version: 1,
			Metadata: json.RawMessage(`{"policy_hash":"` + cityEnterpriseLocationPolicyHash + `","schema_version":1,"source":"relocation"}`),
		},
	}
	sitesAfter = append(sitesAfter, newSites...)
	resourceBalances, err := loadCityEnterpriseRelocationInventory(ctx, tx, worldID, firm)
	if err != nil {
		return cityEnterpriseLocationExecution{}, err
	}
	resourceSequences := make([]int64, 0, 1)
	if len(resourceBalances) != 0 {
		resourceSequences = append(resourceSequences, resourceSequence)
	}
	metadata := cityEnterpriseLocationFactMetadata{
		SchemaVersion: 1, SitesBefore: sitesBefore, SitesAfter: sitesAfter,
		FirmBefore:                 &cityEnterpriseFirmLocationSnapshot{DistrictCode: firm.districtCode, Version: firm.version},
		FirmAfter:                  &cityEnterpriseFirmLocationSnapshot{DistrictCode: headquartersPool.districtCode, Version: firm.version + 1},
		ResourceOperationSequences: resourceSequences, Reason: payload.Reason,
	}
	fact, err := insertCityEnterpriseLocationFact(
		ctx, tx, worldID, targetTick, factSequence, command, firm.entityID,
		nil, CityEnterpriseLocationFactRelocated, nil, nil, 0, 0, 0, 0, metadata,
	)
	if err != nil {
		return cityEnterpriseLocationExecution{}, err
	}

	operations := make([]*CityResourceOperation, 0, 1)
	if len(resourceBalances) != 0 {
		operation, operationErr := postCityEnterpriseRelocationInventory(
			ctx, tx, worldID, targetTick, resourceSequence, command, fact.id,
			firm, headquartersPool, resourceBalances,
		)
		if operationErr != nil {
			return cityEnterpriseLocationExecution{}, cityEnterpriseReject(cityEnterpriseRejectionInventoryTransfer)
		}
		operations = append(operations, operation)
	}
	for _, site := range primarySites {
		result, updateErr := tx.ExecContext(ctx, `
UPDATE city_enterprise_sites
SET status = 'closed', last_changed_tick = $3, closed_tick = $3,
    version = version + 1, updated_at = NOW()
WHERE world_id = $1 AND code = $2 AND status = 'active' AND version = $4`,
			worldID, site.Code, targetTick, site.Version)
		if updateErr != nil {
			return cityEnterpriseLocationExecution{}, fmt.Errorf("close relocated city enterprise site: %w", updateErr)
		}
		if rows, rowsErr := result.RowsAffected(); rowsErr != nil || rows != 1 {
			return cityEnterpriseLocationExecution{}, cityEnterpriseReject(cityEnterpriseRejectionStateConflict)
		}
	}
	for index, site := range newSites {
		pool := headquartersPool
		if site.SiteType == CityEnterpriseSiteProduction {
			pool = productionPool
		}
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_enterprise_sites
    (world_id, code, firm_entity_id, entity_type, district_id, building_id, pool_id,
     site_type, name, occupied_units, is_primary, status, opened_tick,
     last_changed_tick, version, metadata)
VALUES ($1, $2, $3, 'firm', $4, $5, $6, $7, $8, $9, TRUE, 'active', $10, $10, 1, $11::jsonb)`,
			worldID, site.Code, firm.entityID, pool.districtID, pool.buildingID, pool.id,
			site.SiteType, site.Name, site.OccupiedUnits, targetTick, string(site.Metadata)); err != nil {
			return cityEnterpriseLocationExecution{}, fmt.Errorf("insert relocated city enterprise site %d: %w", index+1, err)
		}
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_firm_states
SET district_id = $3, version = version + 1, updated_at = NOW()
WHERE world_id = $1 AND entity_id = $2 AND district_id = $4 AND version = $5`,
		worldID, firm.entityID, headquartersPool.districtID, firm.districtID, firm.version)
	if err != nil {
		return cityEnterpriseLocationExecution{}, fmt.Errorf("relocate city firm state: %w", err)
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr != nil || rows != 1 {
		return cityEnterpriseLocationExecution{}, cityEnterpriseReject(cityEnterpriseRejectionStateConflict)
	}
	if err = advanceCityEnterpriseLocationProfile(ctx, tx, worldID, int64(len(newSites))); err != nil {
		return cityEnterpriseLocationExecution{}, err
	}
	if err = postCityEnterpriseLocationFact(ctx, tx, fact.id); err != nil {
		return cityEnterpriseLocationExecution{}, err
	}
	fact.fact.FirmEntityCode = firm.entityCode
	return cityEnterpriseLocationExecution{
		pending: cityEnterpriseAppliedEvent(command, "city.enterprise.relocated", firm.entityCode, nil, fact.fact, operations),
		fact:    &fact.fact, resourceOperations: operations,
	}, nil
}

func loadCityEnterprisePrimarySites(
	ctx context.Context,
	tx *sql.Tx,
	worldID, firmEntityID int64,
) ([]CityEnterpriseSite, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT site.code, firm.code, district.code, building.code, pool.code,
       site.site_type, site.name, site.occupied_units, site.is_primary,
       site.status, site.opened_tick, site.last_changed_tick, site.closed_tick,
       site.version, site.metadata
FROM city_enterprise_sites site
JOIN city_economic_entities firm
  ON firm.id = site.firm_entity_id AND firm.world_id = site.world_id
JOIN city_districts district
  ON district.id = site.district_id AND district.world_id = site.world_id
JOIN city_buildings building
  ON building.id = site.building_id AND building.world_id = site.world_id
JOIN city_building_unit_pools pool
  ON pool.id = site.pool_id AND pool.world_id = site.world_id
WHERE site.world_id = $1 AND site.firm_entity_id = $2 AND site.status = 'active'
  AND site.is_primary AND site.site_type IN ('headquarters', 'production')
ORDER BY CASE site.site_type WHEN 'headquarters' THEN 1 ELSE 2 END, site.code ASC
FOR UPDATE OF site`, worldID, firmEntityID)
	if err != nil {
		return nil, fmt.Errorf("load city enterprise primary sites: %w", err)
	}
	items := make([]CityEnterpriseSite, 0, 2)
	for rows.Next() {
		var site CityEnterpriseSite
		var closedTick sql.NullInt64
		if err = rows.Scan(
			&site.Code, &site.FirmEntityCode, &site.DistrictCode, &site.BuildingCode,
			&site.PoolCode, &site.SiteType, &site.Name, &site.OccupiedUnits,
			&site.IsPrimary, &site.Status, &site.OpenedTick, &site.LastChangedTick,
			&closedTick, &site.Version, &site.Metadata,
		); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan city enterprise primary site: %w", err)
		}
		site.ClosedTick = nullableInt64Pointer(closedTick)
		items = append(items, site)
	}
	if err = closeCityRows(rows, "iterate city enterprise primary sites"); err != nil {
		return nil, err
	}
	return items, nil
}

func loadCityEnterpriseRelocationInventory(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	firm *cityEnterpriseFirmRef,
) ([]*cityInventoryRef, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT balance.id, balance.entity_id, balance.entity_type, entity.code,
       balance.district_id, district.code, balance.resource_id, resource.code,
       balance.quantity_units, balance.version
FROM city_inventory_balances balance
JOIN city_economic_entities entity
  ON entity.id = balance.entity_id AND entity.world_id = balance.world_id
JOIN city_districts district
  ON district.id = balance.district_id AND district.world_id = balance.world_id
JOIN city_resources resource
  ON resource.id = balance.resource_id AND resource.world_id = balance.world_id
WHERE balance.world_id = $1 AND balance.entity_id = $2 AND balance.district_id = $3
  AND balance.status = 'active' AND balance.quantity_units > 0
  AND resource.status = 'active' AND resource.storable
ORDER BY resource.code ASC
FOR UPDATE OF balance`, worldID, firm.entityID, firm.districtID)
	if err != nil {
		return nil, fmt.Errorf("load city enterprise relocation inventory: %w", err)
	}
	items := make([]*cityInventoryRef, 0)
	for rows.Next() {
		item := &cityInventoryRef{}
		if err = rows.Scan(
			&item.id, &item.entityID, &item.entityType, &item.entityCode,
			&item.districtID, &item.districtCode, &item.resourceID,
			&item.resourceCode, &item.quantityUnits, &item.version,
		); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan city enterprise relocation inventory: %w", err)
		}
		items = append(items, item)
	}
	if err = closeCityRows(rows, "iterate city enterprise relocation inventory"); err != nil {
		return nil, err
	}
	return items, nil
}

func postCityEnterpriseRelocationInventory(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, resourceSequence int64,
	command *CityCommand,
	factID int64,
	firm *cityEnterpriseFirmRef,
	targetPool *cityEnterprisePoolRef,
	sources []*cityInventoryRef,
) (*CityResourceOperation, error) {
	lines := make([]cityResourcePostingLine, 0, len(sources)*2)
	for _, source := range sources {
		target, err := ensureCityInventoryRef(
			ctx, tx, worldID, firm.entityID, targetPool.districtCode, source.resourceCode,
		)
		if err != nil {
			return nil, err
		}
		if source.id == target.id || source.resourceID != target.resourceID {
			return nil, cityResourceReject(cityResourceRejectionScopeNotFound)
		}
		memo := "Enterprise relocation"
		lines = append(lines,
			cityResourcePostingLine{balance: source, direction: "out", quantityUnits: source.quantityUnits, memo: memo},
			cityResourcePostingLine{balance: target, direction: "in", quantityUnits: source.quantityUnits, memo: memo},
		)
	}
	return postCityResourceOperation(ctx, tx, cityResourceOperationSpec{
		worldID: worldID, tick: targetTick, sequence: resourceSequence,
		operationKey:  "enterprise-relocation:" + strconv.FormatInt(command.Sequence, 10),
		operationType: "transfer", sourceCommandID: &command.ID,
		actorEntityID: firm.entityID, districtID: firm.districtID,
		description: "Enterprise relocation inventory transfer",
		metadata: map[string]any{
			"schema_version": 1, "enterprise_location_fact_id": factID,
			"command_sequence": command.Sequence, "firm_entity_code": firm.entityCode,
			"from_district_code": firm.districtCode, "to_district_code": targetPool.districtCode,
			"resource_count": len(sources),
		},
		lines: lines,
	})
}

func cityEnterpriseAppliedEvent(
	command *CityCommand,
	eventType, firmCode string,
	site *CityEnterpriseSite,
	fact CityEnterpriseLocationFact,
	operations []*CityResourceOperation,
) cityPendingEvent {
	payload := map[string]any{
		"firm_entity_code": firmCode,
		"fact_tick":        fact.Tick, "fact_sequence": fact.Sequence,
		"fact_type":                fact.FactType,
		"resource_operation_count": len(operations),
	}
	result := map[string]any{
		"applied": true, "fact_tick": fact.Tick,
		"fact_sequence": fact.Sequence, "fact_type": fact.FactType,
	}
	if site != nil {
		payload["site_code"] = site.Code
		payload["site_type"] = site.SiteType
		payload["district_code"] = site.DistrictCode
		payload["occupied_units"] = site.OccupiedUnits
		result["site_code"] = site.Code
	}
	if len(operations) != 0 {
		payload["resource_operation_sequence"] = operations[0].Sequence
		result["resource_operation_sequence"] = operations[0].Sequence
	}
	return cityPendingEvent{
		command: command, status: CityCommandStatusApplied, eventType: eventType,
		payload: payload, result: result,
	}
}
