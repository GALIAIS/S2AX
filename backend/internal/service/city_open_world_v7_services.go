package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const (
	CityCommandTypeOpenWorldActorServiceRequest = "open_world.actor.service.request"

	CityOpenWorldRuntimeFactServiceRequested  = "service.requested"
	CityOpenWorldRuntimeFactServiceQueued     = "service.queued"
	CityOpenWorldRuntimeFactServiceDispatched = "service.dispatched"
	CityOpenWorldRuntimeFactServiceResponded  = "service.responded"
	CityOpenWorldRuntimeFactServiceExpired    = "service.expired"

	cityOpenWorldServiceSchemaVersion           = 1
	cityOpenWorldServiceProfileID               = "sub2api-open-world-service-coordination"
	cityOpenWorldServiceProfileVersion          = "1.0.0"
	cityOpenWorldServiceAccessModelVersion      = "anchor_manhattan-v1"
	cityOpenWorldServiceDispatchModelVersion    = "capacity_queue-v1"
	cityOpenWorldServiceMaximumQueuePerProvider = 512
	cityOpenWorldServiceMaximumRequestsPerTick  = 256
	cityOpenWorldServiceMaximumUnitsPerRequest  = 1_000
)

// CityOpenWorldServiceCatalogEntry is a frozen service definition selected at
// V7 genesis or upgrade.  It intentionally contains only service semantics;
// facilities and actors remain separately owned projections.
type CityOpenWorldServiceCatalogEntry struct {
	Code                 string          `json:"code"`
	NameKey              string          `json:"name_key"`
	CategoryCode         string          `json:"category_code"`
	Version              string          `json:"version"`
	ContentHash          string          `json:"content_hash"`
	MaximumWaitTicks     int64           `json:"maximum_wait_ticks"`
	TargetResponseTicks  int64           `json:"target_response_ticks"`
	DefaultPriorityMilli int             `json:"default_priority_milli"`
	Metadata             json.RawMessage `json:"metadata"`
}

// CityOpenWorldServicePolicy is a sealed per-world policy profile.  Its
// versions declare the only reachability and queue semantics reducers may use.
type CityOpenWorldServicePolicy struct {
	ProfileID               string          `json:"profile_id"`
	ProfileVersion          string          `json:"profile_version"`
	ContentHash             string          `json:"content_hash"`
	BaselineTick            int64           `json:"baseline_tick"`
	AccessModelVersion      string          `json:"access_model_version"`
	DispatchModelVersion    string          `json:"dispatch_model_version"`
	MaximumQueuePerProvider int             `json:"maximum_queue_per_provider"`
	Revision                int64           `json:"revision"`
	Metadata                json.RawMessage `json:"metadata"`
}

// CityOpenWorldServiceProvider is an operational capacity endpoint attached
// to a V5 facility.  It does not reinterpret a building as a legacy F8
// facility: the provider is a distinct V7 service projection with its own
// capacity, accessibility radius and reducer history.
type CityOpenWorldServiceProvider struct {
	Code                 string          `json:"code"`
	FacilityCode         string          `json:"facility_code"`
	ServiceCode          string          `json:"service_code"`
	ProviderKind         string          `json:"provider_kind"`
	Status               string          `json:"status"`
	CapacityUnitsPerTick int64           `json:"capacity_units_per_tick"`
	AccessRadiusUnits    int64           `json:"access_radius_units"`
	AnchorX              int64           `json:"anchor_x"`
	AnchorY              int64           `json:"anchor_y"`
	AnchorZ              int32           `json:"anchor_z"`
	LastSettledTick      int64           `json:"last_settled_tick"`
	Version              int64           `json:"version"`
	Metadata             json.RawMessage `json:"metadata"`
}

// CityOpenWorldServiceRequest is the queue projection for an actor service
// intent.  A request cannot be submitted with a provider, route or outcome;
// these are selected exclusively by the deterministic V7 reducer.
type CityOpenWorldServiceRequest struct {
	Code                 string                       `json:"code"`
	ActorCode            string                       `json:"actor_code"`
	ServiceCode          string                       `json:"service_code"`
	Status               string                       `json:"status"`
	PriorityMilli        int                          `json:"priority_milli"`
	RequestedUnits       int64                        `json:"requested_units"`
	RequestedTick        int64                        `json:"requested_tick"`
	EarliestDispatchTick int64                        `json:"earliest_dispatch_tick"`
	DeadlineTick         int64                        `json:"deadline_tick"`
	QueuedTick           *int64                       `json:"queued_tick,omitempty"`
	ProviderCode         *string                      `json:"provider_code,omitempty"`
	DispatchedTick       *int64                       `json:"dispatched_tick,omitempty"`
	ResolvedTick         *int64                       `json:"resolved_tick,omitempty"`
	QueuePosition        *int                         `json:"queue_position,omitempty"`
	SourceFact           CityOpenWorldRuntimeFactRef  `json:"source_fact"`
	LastFact             *CityOpenWorldRuntimeFactRef `json:"last_fact,omitempty"`
	Version              int64                        `json:"version"`
	Metadata             json.RawMessage              `json:"metadata"`
}

// CityOpenWorldServiceResponse is immutable evidence that an eligible
// request was served or expired.  A later V8 effect bridge reads this table on
// a later tick; it never lets the response mutate another domain immediately.
type CityOpenWorldServiceResponse struct {
	Code           string                      `json:"code"`
	RequestCode    string                      `json:"request_code"`
	ActorCode      string                      `json:"actor_code"`
	ServiceCode    string                      `json:"service_code"`
	ProviderCode   *string                     `json:"provider_code,omitempty"`
	Outcome        string                      `json:"outcome"`
	RequestedTick  int64                       `json:"requested_tick"`
	QueuedTick     *int64                      `json:"queued_tick,omitempty"`
	DispatchedTick *int64                      `json:"dispatched_tick,omitempty"`
	ResolvedTick   int64                       `json:"resolved_tick"`
	ResponseTicks  int64                       `json:"response_ticks"`
	DeliveredUnits int64                       `json:"delivered_units"`
	SourceFact     CityOpenWorldRuntimeFactRef `json:"source_fact"`
	Metadata       json.RawMessage             `json:"metadata"`
}

// CityOpenWorldServiceState is part of V7 canonical state. Empty result
// slices are deliberately retained: a world with no player requests is
// different from a pre-V7 world that does not own this subsystem.
type CityOpenWorldServiceState struct {
	Policy    CityOpenWorldServicePolicy         `json:"policy"`
	Catalog   []CityOpenWorldServiceCatalogEntry `json:"catalog"`
	Providers []CityOpenWorldServiceProvider     `json:"providers"`
	Requests  []CityOpenWorldServiceRequest      `json:"requests"`
	Responses []CityOpenWorldServiceResponse     `json:"responses"`
}

// cityOpenWorldActorServiceRequestPayload intentionally excludes provider,
// route, delivery time and outcome.  Those values are reducer-owned so a
// player cannot steer capacity selection or skip the service latency model.
type cityOpenWorldActorServiceRequestPayload struct {
	ActorCode      string `json:"actor_code"`
	ServiceCode    string `json:"service_code"`
	RequestedUnits int64  `json:"requested_units"`
	PriorityMilli  *int   `json:"priority_milli,omitempty"`
}

func isCityOpenWorldServiceCommand(commandType string) bool {
	return commandType == CityCommandTypeOpenWorldActorServiceRequest
}

func normalizeCityOpenWorldActorServiceRequest(rawPayload json.RawMessage) (cityOpenWorldActorServiceRequestPayload, error) {
	var value cityOpenWorldActorServiceRequestPayload
	if err := decodeStrictCityObject(rawPayload, &value); err != nil {
		return value, ErrCityInvalidInput.WithCause(err)
	}
	value.ActorCode = strings.ToLower(strings.TrimSpace(value.ActorCode))
	value.ServiceCode = strings.ToLower(strings.TrimSpace(value.ServiceCode))
	if !worldRuntimeCodeValid(value.ActorCode, 128) {
		return value, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "actor_code"})
	}
	if !worldRuntimeCodeValid(value.ServiceCode, 64) {
		return value, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "service_code"})
	}
	if value.RequestedUnits == 0 {
		value.RequestedUnits = 1
	}
	if value.RequestedUnits < 1 || value.RequestedUnits > cityOpenWorldServiceMaximumUnitsPerRequest {
		return value, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "requested_units"})
	}
	if value.PriorityMilli != nil && (*value.PriorityMilli < -100_000 || *value.PriorityMilli > 100_000) {
		return value, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "priority_milli"})
	}
	return value, nil
}

type cityOpenWorldServiceDefinitionSeed struct {
	Code                 string `json:"code"`
	NameKey              string `json:"name_key"`
	CategoryCode         string `json:"category_code"`
	MaximumWaitTicks     int64  `json:"maximum_wait_ticks"`
	TargetResponseTicks  int64  `json:"target_response_ticks"`
	DefaultPriorityMilli int    `json:"default_priority_milli"`
}

type cityOpenWorldServiceProviderTemplate struct {
	ServiceCode       string
	ProviderKind      string
	CapacityDivisor   int64
	MinimumCapacity   int64
	AccessRadiusUnits int64
}

type cityOpenWorldServiceFacilitySeed struct {
	ID            int64
	Code          string
	FacilityType  string
	CapacityUnits int64
	AnchorX       int64
	AnchorY       int64
	AnchorZ       int32
}

func builtInCityOpenWorldServiceCatalog() ([]CityOpenWorldServiceCatalogEntry, string, error) {
	seeds := []cityOpenWorldServiceDefinitionSeed{
		{Code: "education.basic", NameKey: "openWorld.services.educationBasic", CategoryCode: "education", MaximumWaitTicks: 48, TargetResponseTicks: 4, DefaultPriorityMilli: 0},
		{Code: "health.primary", NameKey: "openWorld.services.healthPrimary", CategoryCode: "health", MaximumWaitTicks: 24, TargetResponseTicks: 2, DefaultPriorityMilli: 250},
		{Code: "safety.emergency", NameKey: "openWorld.services.safetyEmergency", CategoryCode: "safety", MaximumWaitTicks: 8, TargetResponseTicks: 1, DefaultPriorityMilli: 900},
		{Code: "civic.support", NameKey: "openWorld.services.civicSupport", CategoryCode: "civic", MaximumWaitTicks: 72, TargetResponseTicks: 8, DefaultPriorityMilli: -100},
	}
	entries := make([]CityOpenWorldServiceCatalogEntry, 0, len(seeds))
	for _, seed := range seeds {
		raw, err := json.Marshal(struct {
			SchemaVersion int                                `json:"schema_version"`
			Definition    cityOpenWorldServiceDefinitionSeed `json:"definition"`
		}{SchemaVersion: cityOpenWorldServiceSchemaVersion, Definition: seed})
		if err != nil {
			return nil, "", fmt.Errorf("marshal open-world service definition %s: %w", seed.Code, err)
		}
		sum := sha256.Sum256(raw)
		metadata, err := json.Marshal(map[string]any{
			"schema_version": cityOpenWorldServiceSchemaVersion,
			"access_model":   cityOpenWorldServiceAccessModelVersion,
		})
		if err != nil {
			return nil, "", fmt.Errorf("marshal open-world service metadata %s: %w", seed.Code, err)
		}
		entries = append(entries, CityOpenWorldServiceCatalogEntry{
			Code: seed.Code, NameKey: seed.NameKey, CategoryCode: seed.CategoryCode,
			Version: cityOpenWorldServiceProfileVersion, ContentHash: hex.EncodeToString(sum[:]),
			MaximumWaitTicks: seed.MaximumWaitTicks, TargetResponseTicks: seed.TargetResponseTicks,
			DefaultPriorityMilli: seed.DefaultPriorityMilli, Metadata: metadata,
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Code < entries[j].Code })
	raw, err := json.Marshal(entries)
	if err != nil {
		return nil, "", fmt.Errorf("marshal open-world service catalogue: %w", err)
	}
	sum := sha256.Sum256(raw)
	return entries, hex.EncodeToString(sum[:]), nil
}

func cityOpenWorldServiceProviderTemplates(facilityType string) []cityOpenWorldServiceProviderTemplate {
	switch facilityType {
	case "residence":
		return []cityOpenWorldServiceProviderTemplate{
			{ServiceCode: "civic.support", ProviderKind: "residential_support", CapacityDivisor: 12, MinimumCapacity: 1, AccessRadiusUnits: 192},
		}
	case "commerce":
		return []cityOpenWorldServiceProviderTemplate{
			{ServiceCode: "education.basic", ProviderKind: "community_hub", CapacityDivisor: 10, MinimumCapacity: 1, AccessRadiusUnits: 512},
			{ServiceCode: "health.primary", ProviderKind: "community_hub", CapacityDivisor: 16, MinimumCapacity: 1, AccessRadiusUnits: 384},
			{ServiceCode: "civic.support", ProviderKind: "community_hub", CapacityDivisor: 8, MinimumCapacity: 1, AccessRadiusUnits: 512},
		}
	case "industry":
		return []cityOpenWorldServiceProviderTemplate{
			{ServiceCode: "health.primary", ProviderKind: "workplace_support", CapacityDivisor: 24, MinimumCapacity: 1, AccessRadiusUnits: 256},
			{ServiceCode: "safety.emergency", ProviderKind: "emergency_post", CapacityDivisor: 32, MinimumCapacity: 1, AccessRadiusUnits: 768},
		}
	default:
		return nil
	}
}

func cityOpenWorldServiceProviderCapacity(capacity, divisor, minimum int64) (int64, error) {
	if capacity <= 0 || divisor <= 0 || minimum <= 0 {
		return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_service_provider_capacity"})
	}
	value := capacity / divisor
	if value < minimum {
		value = minimum
	}
	if value > 1_000_000 {
		return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_service_provider_capacity"})
	}
	return value, nil
}

func cityOpenWorldServiceProviderCode(facilityCode, serviceCode string) string {
	base := "provider." + facilityCode + "." + serviceCode
	if len(base) <= 159 {
		return base
	}
	sum := sha256.Sum256([]byte(base))
	prefix := facilityCode
	if len(prefix) > 96 {
		prefix = prefix[:96]
	}
	return "provider." + prefix + "." + hex.EncodeToString(sum[:8])
}

func cityOpenWorldServiceProfileHash(catalogHash string) (string, error) {
	raw, err := json.Marshal(struct {
		SchemaVersion           int    `json:"schema_version"`
		CatalogHash             string `json:"catalog_hash"`
		AccessModelVersion      string `json:"access_model_version"`
		DispatchModelVersion    string `json:"dispatch_model_version"`
		MaximumQueuePerProvider int    `json:"maximum_queue_per_provider"`
	}{
		SchemaVersion: cityOpenWorldServiceSchemaVersion, CatalogHash: catalogHash,
		AccessModelVersion:      cityOpenWorldServiceAccessModelVersion,
		DispatchModelVersion:    cityOpenWorldServiceDispatchModelVersion,
		MaximumQueuePerProvider: cityOpenWorldServiceMaximumQueuePerProvider,
	})
	if err != nil {
		return "", fmt.Errorf("marshal open-world service profile: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// initializeCityOpenWorldV7ServiceFoundation creates the world-owned social
// service baseline shared by V7 and V8. It is valid at genesis and during a
// paused V6 -> V7 upgrade; V8 creates the same sealed service contract before
// installing its separate impact bridge.
func initializeCityOpenWorldV7ServiceFoundation(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if worldID <= 0 {
		return ErrCityInvalidInput
	}
	var simulationVersion string
	var baselineTick int64
	if err := tx.QueryRowContext(ctx, `
SELECT simulation_version, current_tick
FROM city_worlds WHERE id = $1 FOR UPDATE`, worldID).Scan(&simulationVersion, &baselineTick); err != nil {
		return fmt.Errorf("load V7 service world: %w", err)
	}
	if !cityEngineSupportsOpenWorldServiceCoordination(simulationVersion) {
		return ErrCitySimulationVersion.WithMetadata(map[string]string{"version": simulationVersion})
	}
	if err := activateCityOpenWorldServiceBootstrapWrite(ctx, tx, worldID); err != nil {
		return err
	}
	catalog, catalogHash, err := builtInCityOpenWorldServiceCatalog()
	if err != nil {
		return err
	}
	profileHash, err := cityOpenWorldServiceProfileHash(catalogHash)
	if err != nil {
		return err
	}
	metadata, err := json.Marshal(map[string]any{
		"schema_version":               cityOpenWorldServiceSchemaVersion,
		"catalog_hash":                 catalogHash,
		"queue_order":                  "priority_desc_requested_tick_asc_request_code_asc_provider_code_asc",
		"cross_domain_effect_contract": "next_tick_only",
	})
	if err != nil {
		return fmt.Errorf("marshal V7 service profile metadata: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_service_profiles
    (world_id, profile_id, profile_version, content_hash, baseline_tick,
     access_model_version, dispatch_model_version, maximum_queue_per_provider,
     revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 1, $9::jsonb)`,
		worldID, cityOpenWorldServiceProfileID, cityOpenWorldServiceProfileVersion,
		profileHash, baselineTick, cityOpenWorldServiceAccessModelVersion,
		cityOpenWorldServiceDispatchModelVersion, cityOpenWorldServiceMaximumQueuePerProvider,
		[]byte(metadata)); err != nil {
		return fmt.Errorf("insert V7 service profile: %w", err)
	}
	for _, entry := range catalog {
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_service_catalog
    (world_id, code, name_key, category_code, definition_version, content_hash,
     maximum_wait_ticks, target_response_ticks, default_priority_milli, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb)`,
			worldID, entry.Code, entry.NameKey, entry.CategoryCode, entry.Version, entry.ContentHash,
			entry.MaximumWaitTicks, entry.TargetResponseTicks, entry.DefaultPriorityMilli, []byte(entry.Metadata)); err != nil {
			return fmt.Errorf("insert V7 service catalog %s: %w", entry.Code, err)
		}
	}
	rows, err := tx.QueryContext(ctx, `
SELECT id, code, facility_type_code, capacity_units, anchor_x, anchor_y, anchor_z
FROM city_open_world_facilities
WHERE world_id = $1
ORDER BY code ASC`, worldID)
	if err != nil {
		return fmt.Errorf("load V7 service facilities: %w", err)
	}
	facilities := make([]cityOpenWorldServiceFacilitySeed, 0)
	for rows.Next() {
		item := cityOpenWorldServiceFacilitySeed{}
		if scanErr := rows.Scan(&item.ID, &item.Code, &item.FacilityType, &item.CapacityUnits, &item.AnchorX, &item.AnchorY, &item.AnchorZ); scanErr != nil {
			_ = rows.Close()
			return fmt.Errorf("scan V7 service facility: %w", scanErr)
		}
		facilities = append(facilities, item)
	}
	if err = closeCityRows(rows, "iterate V7 service facilities"); err != nil {
		return err
	}
	for _, facility := range facilities {
		for _, template := range cityOpenWorldServiceProviderTemplates(facility.FacilityType) {
			capacity, capacityErr := cityOpenWorldServiceProviderCapacity(
				facility.CapacityUnits, template.CapacityDivisor, template.MinimumCapacity,
			)
			if capacityErr != nil {
				return capacityErr
			}
			providerMetadata, marshalErr := json.Marshal(map[string]any{
				"schema_version": cityOpenWorldServiceSchemaVersion,
				"facility_type":  facility.FacilityType,
				"baseline_tick":  baselineTick,
			})
			if marshalErr != nil {
				return fmt.Errorf("marshal V7 provider metadata: %w", marshalErr)
			}
			if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_service_providers
    (world_id, code, facility_id, service_code, provider_kind, status,
     capacity_units_per_tick, access_radius_units, anchor_x, anchor_y, anchor_z,
     last_settled_tick, version, metadata)
VALUES ($1, $2, $3, $4, $5, 'active', $6, $7, $8, $9, $10, $11, 1, $12::jsonb)`,
				worldID, cityOpenWorldServiceProviderCode(facility.Code, template.ServiceCode),
				facility.ID, template.ServiceCode, template.ProviderKind, capacity,
				template.AccessRadiusUnits, facility.AnchorX, facility.AnchorY, facility.AnchorZ,
				baselineTick, []byte(providerMetadata)); err != nil {
				return fmt.Errorf("insert V7 service provider for %s/%s: %w", facility.Code, template.ServiceCode, err)
			}
		}
	}
	return nil
}

func activateCityOpenWorldServiceBootstrapWrite(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if _, err := tx.ExecContext(ctx, `
SELECT set_config('sub2api.city_open_world_service_bootstrap_world_id', $1, TRUE)`, strconv.FormatInt(worldID, 10)); err != nil {
		return fmt.Errorf("enable V7 service bootstrap: %w", err)
	}
	return nil
}

// loadCityOpenWorldServiceState is used exclusively by V7 canonical-state
// loading.  The explicit joins preserve semantic references as stable codes
// and fact coordinates rather than leaking storage IDs into snapshots.
func loadCityOpenWorldServiceState(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*CityOpenWorldServiceState, error) {
	if worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	state := &CityOpenWorldServiceState{
		Catalog:   make([]CityOpenWorldServiceCatalogEntry, 0),
		Providers: make([]CityOpenWorldServiceProvider, 0),
		Requests:  make([]CityOpenWorldServiceRequest, 0),
		Responses: make([]CityOpenWorldServiceResponse, 0),
	}
	if err := queryer.QueryRowContext(ctx, `
SELECT profile_id, profile_version, content_hash, baseline_tick,
       access_model_version, dispatch_model_version, maximum_queue_per_provider,
       revision, metadata
FROM city_open_world_service_profiles
WHERE world_id = $1`, worldID).Scan(
		&state.Policy.ProfileID, &state.Policy.ProfileVersion, &state.Policy.ContentHash,
		&state.Policy.BaselineTick, &state.Policy.AccessModelVersion,
		&state.Policy.DispatchModelVersion, &state.Policy.MaximumQueuePerProvider,
		&state.Policy.Revision, &state.Policy.Metadata,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v7_service_profile"})
		}
		return nil, fmt.Errorf("load V7 service profile: %w", err)
	}

	catalogRows, err := queryer.QueryContext(ctx, `
SELECT code, name_key, category_code, definition_version, content_hash,
       maximum_wait_ticks, target_response_ticks, default_priority_milli, metadata
FROM city_open_world_service_catalog
WHERE world_id = $1
ORDER BY code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V7 service catalog: %w", err)
	}
	for catalogRows.Next() {
		item := CityOpenWorldServiceCatalogEntry{}
		if scanErr := catalogRows.Scan(
			&item.Code, &item.NameKey, &item.CategoryCode, &item.Version, &item.ContentHash,
			&item.MaximumWaitTicks, &item.TargetResponseTicks, &item.DefaultPriorityMilli, &item.Metadata,
		); scanErr != nil {
			_ = catalogRows.Close()
			return nil, fmt.Errorf("scan V7 service catalog: %w", scanErr)
		}
		state.Catalog = append(state.Catalog, item)
	}
	if err = closeCityRows(catalogRows, "iterate V7 service catalog"); err != nil {
		return nil, err
	}

	providerRows, err := queryer.QueryContext(ctx, `
SELECT provider.code, facility.code, provider.service_code, provider.provider_kind,
       provider.status, provider.capacity_units_per_tick, provider.access_radius_units,
       provider.anchor_x, provider.anchor_y, provider.anchor_z, provider.last_settled_tick,
       provider.version, provider.metadata
FROM city_open_world_service_providers provider
JOIN city_open_world_facilities facility
  ON facility.id = provider.facility_id AND facility.world_id = provider.world_id
WHERE provider.world_id = $1
ORDER BY provider.code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V7 service providers: %w", err)
	}
	for providerRows.Next() {
		item := CityOpenWorldServiceProvider{}
		if scanErr := providerRows.Scan(
			&item.Code, &item.FacilityCode, &item.ServiceCode, &item.ProviderKind,
			&item.Status, &item.CapacityUnitsPerTick, &item.AccessRadiusUnits,
			&item.AnchorX, &item.AnchorY, &item.AnchorZ, &item.LastSettledTick,
			&item.Version, &item.Metadata,
		); scanErr != nil {
			_ = providerRows.Close()
			return nil, fmt.Errorf("scan V7 service provider: %w", scanErr)
		}
		state.Providers = append(state.Providers, item)
	}
	if err = closeCityRows(providerRows, "iterate V7 service providers"); err != nil {
		return nil, err
	}

	requestRows, err := queryer.QueryContext(ctx, `
SELECT request_value.code, actor.code, request_value.service_code, request_value.status,
       request_value.priority_milli, request_value.requested_units, request_value.requested_tick,
       request_value.earliest_dispatch_tick, request_value.deadline_tick, request_value.queued_tick,
       provider.code, request_value.dispatched_tick, request_value.resolved_tick,
       request_value.queue_position, source_fact.tick, source_fact.sequence,
       last_fact.tick, last_fact.sequence, request_value.version, request_value.metadata
FROM city_open_world_service_requests request_value
JOIN city_open_world_actors actor
  ON actor.id = request_value.actor_id AND actor.world_id = request_value.world_id
LEFT JOIN city_open_world_service_providers provider
  ON provider.id = request_value.provider_id AND provider.world_id = request_value.world_id
JOIN city_open_world_runtime_facts source_fact
  ON source_fact.id = request_value.source_fact_id AND source_fact.world_id = request_value.world_id
LEFT JOIN city_open_world_runtime_facts last_fact
  ON last_fact.id = request_value.last_fact_id AND last_fact.world_id = request_value.world_id
WHERE request_value.world_id = $1
ORDER BY request_value.requested_tick ASC, request_value.code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V7 service requests: %w", err)
	}
	for requestRows.Next() {
		item := CityOpenWorldServiceRequest{}
		var queuedTick, dispatchedTick, resolvedTick sql.NullInt64
		var providerCode sql.NullString
		var queuePosition sql.NullInt64
		var lastTick, lastSequence sql.NullInt64
		if scanErr := requestRows.Scan(
			&item.Code, &item.ActorCode, &item.ServiceCode, &item.Status,
			&item.PriorityMilli, &item.RequestedUnits, &item.RequestedTick,
			&item.EarliestDispatchTick, &item.DeadlineTick, &queuedTick,
			&providerCode, &dispatchedTick, &resolvedTick, &queuePosition,
			&item.SourceFact.Tick, &item.SourceFact.Sequence,
			&lastTick, &lastSequence, &item.Version, &item.Metadata,
		); scanErr != nil {
			_ = requestRows.Close()
			return nil, fmt.Errorf("scan V7 service request: %w", scanErr)
		}
		if queuedTick.Valid {
			item.QueuedTick = cityOpenWorldInt64Pointer(queuedTick.Int64)
		}
		if providerCode.Valid {
			item.ProviderCode = cityOpenWorldStringPointer(providerCode.String)
		}
		if dispatchedTick.Valid {
			item.DispatchedTick = cityOpenWorldInt64Pointer(dispatchedTick.Int64)
		}
		if resolvedTick.Valid {
			item.ResolvedTick = cityOpenWorldInt64Pointer(resolvedTick.Int64)
		}
		if queuePosition.Valid {
			position := int(queuePosition.Int64)
			item.QueuePosition = &position
		}
		if lastTick.Valid {
			item.LastFact = &CityOpenWorldRuntimeFactRef{Tick: lastTick.Int64, Sequence: lastSequence.Int64}
		}
		state.Requests = append(state.Requests, item)
	}
	if err = closeCityRows(requestRows, "iterate V7 service requests"); err != nil {
		return nil, err
	}

	responseRows, err := queryer.QueryContext(ctx, `
SELECT response.code, request_value.code, actor.code, response.service_code,
       provider.code, response.outcome, response.requested_tick, response.queued_tick,
       response.dispatched_tick, response.resolved_tick, response.response_ticks,
       response.delivered_units, source_fact.tick, source_fact.sequence, response.metadata
FROM city_open_world_service_responses response
JOIN city_open_world_service_requests request_value
  ON request_value.id = response.request_id AND request_value.world_id = response.world_id
JOIN city_open_world_actors actor
  ON actor.id = response.actor_id AND actor.world_id = response.world_id
LEFT JOIN city_open_world_service_providers provider
  ON provider.id = response.provider_id AND provider.world_id = response.world_id
JOIN city_open_world_runtime_facts source_fact
  ON source_fact.id = response.source_fact_id AND source_fact.world_id = response.world_id
WHERE response.world_id = $1
ORDER BY response.resolved_tick ASC, response.code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V7 service responses: %w", err)
	}
	for responseRows.Next() {
		item := CityOpenWorldServiceResponse{}
		var providerCode sql.NullString
		var queuedTick, dispatchedTick sql.NullInt64
		if scanErr := responseRows.Scan(
			&item.Code, &item.RequestCode, &item.ActorCode, &item.ServiceCode,
			&providerCode, &item.Outcome, &item.RequestedTick, &queuedTick,
			&dispatchedTick, &item.ResolvedTick, &item.ResponseTicks,
			&item.DeliveredUnits, &item.SourceFact.Tick, &item.SourceFact.Sequence, &item.Metadata,
		); scanErr != nil {
			_ = responseRows.Close()
			return nil, fmt.Errorf("scan V7 service response: %w", scanErr)
		}
		if providerCode.Valid {
			item.ProviderCode = cityOpenWorldStringPointer(providerCode.String)
		}
		if queuedTick.Valid {
			item.QueuedTick = cityOpenWorldInt64Pointer(queuedTick.Int64)
		}
		if dispatchedTick.Valid {
			item.DispatchedTick = cityOpenWorldInt64Pointer(dispatchedTick.Int64)
		}
		state.Responses = append(state.Responses, item)
	}
	if err = closeCityRows(responseRows, "iterate V7 service responses"); err != nil {
		return nil, err
	}
	return state, nil
}

// GetCityOpenWorldServiceState returns the V7 public service contract.  The
// catalog and providers are world-visible; request/response history is scoped
// to actors the caller owns or controls unless the caller is the city owner or
// a system administrator.
func (s *CityEconomyService) GetCityOpenWorldServiceState(
	ctx context.Context,
	userID, worldID int64,
) (*CityOpenWorldServiceState, error) {
	if userID <= 0 || worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, userID, worldID); err != nil {
		return nil, err
	}
	var simulationVersion string
	if err := s.db.QueryRowContext(ctx, `
SELECT simulation_version FROM city_worlds WHERE id = $1`, worldID).Scan(&simulationVersion); err != nil {
		return nil, fmt.Errorf("load V7 service world version: %w", err)
	}
	if !cityEngineSupportsOpenWorldServiceCoordination(simulationVersion) {
		return nil, ErrCitySimulationVersion.WithMetadata(map[string]string{"version": simulationVersion})
	}
	state, err := loadCityOpenWorldServiceState(ctx, s.db, worldID)
	if err != nil {
		return nil, err
	}
	mayReadAll, err := cityOpenWorldServiceMayReadAll(ctx, s.db, userID, worldID)
	if err != nil {
		return nil, err
	}
	if mayReadAll {
		return state, nil
	}
	visibleActors, err := cityOpenWorldServiceVisibleActorCodes(ctx, s.db, userID, worldID)
	if err != nil {
		return nil, err
	}
	requests := make([]CityOpenWorldServiceRequest, 0)
	for _, request := range state.Requests {
		if _, visible := visibleActors[request.ActorCode]; visible {
			requests = append(requests, request)
		}
	}
	responses := make([]CityOpenWorldServiceResponse, 0)
	for _, response := range state.Responses {
		if _, visible := visibleActors[response.ActorCode]; visible {
			responses = append(responses, response)
		}
	}
	state.Requests, state.Responses = requests, responses
	return state, nil
}

func (s *CityEconomyService) ListCityOpenWorldServiceProviders(
	ctx context.Context,
	userID, worldID int64,
) ([]CityOpenWorldServiceProvider, error) {
	state, err := s.GetCityOpenWorldServiceState(ctx, userID, worldID)
	if err != nil {
		return nil, err
	}
	return state.Providers, nil
}

func (s *CityEconomyService) ListCityOpenWorldServiceRequests(
	ctx context.Context,
	userID, worldID int64,
) ([]CityOpenWorldServiceRequest, error) {
	state, err := s.GetCityOpenWorldServiceState(ctx, userID, worldID)
	if err != nil {
		return nil, err
	}
	return state.Requests, nil
}

func (s *CityEconomyService) ListCityOpenWorldServiceResponses(
	ctx context.Context,
	userID, worldID int64,
) ([]CityOpenWorldServiceResponse, error) {
	state, err := s.GetCityOpenWorldServiceState(ctx, userID, worldID)
	if err != nil {
		return nil, err
	}
	return state.Responses, nil
}

func cityOpenWorldServiceMayReadAll(
	ctx context.Context,
	queryer citySQLQueryer,
	userID, worldID int64,
) (bool, error) {
	if IsCitySystemAdministrator(ctx) {
		return true, nil
	}
	var role string
	err := queryer.QueryRowContext(ctx, `
SELECT role FROM city_members
WHERE world_id = $1 AND user_id = $2 AND status = 'active'`, worldID, userID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrCityWorldNotFound
	}
	if err != nil {
		return false, fmt.Errorf("load V7 service reader role: %w", err)
	}
	return role == CityMemberRoleOwner, nil
}

func cityOpenWorldServiceVisibleActorCodes(
	ctx context.Context,
	queryer citySQLQueryer,
	userID, worldID int64,
) (map[string]struct{}, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT actor.code
FROM city_open_world_actors actor
WHERE actor.world_id = $1 AND actor.status = 'active'
  AND (
      actor.owner_user_id = $2 OR EXISTS (
          SELECT 1
          FROM city_open_world_actor_controls grant_value
          JOIN city_members member
            ON member.world_id = grant_value.world_id AND member.user_id = grant_value.user_id
           AND member.status = 'active'
          WHERE grant_value.world_id = actor.world_id
            AND grant_value.actor_id = actor.id
            AND grant_value.user_id = $2
            AND grant_value.status = 'active'
            AND grant_value.capability IN ('actor.command', 'actor.control.manage')
      )
  )
ORDER BY actor.code ASC`, worldID, userID)
	if err != nil {
		return nil, fmt.Errorf("list V7 visible service actors: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make(map[string]struct{})
	for rows.Next() {
		var code string
		if err = rows.Scan(&code); err != nil {
			return nil, fmt.Errorf("scan V7 visible service actor: %w", err)
		}
		items[code] = struct{}{}
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate V7 visible service actors: %w", err)
	}
	return items, nil
}
