package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	cityRealtimeAgentModelProfileDefaultCode    = "system.fake.deterministic"
	cityRealtimeAgentModelProfileDefaultVersion = 1
	cityRealtimeAgentModelObservationSchema     = "city-realtime-agent-observation-v1"

	cityRealtimeAgentModelProviderFake    = "fake.deterministic"
	cityRealtimeAgentModelProviderGateway = "sub2api.gateway"

	cityRealtimeAgentModelProfileStatusActive   = "active"
	cityRealtimeAgentModelProfileStatusDisabled = "disabled"
	cityRealtimeAgentModelProfileStatusRetired  = "retired"

	cityRealtimeAgentModelBindingStatusActive     = "active"
	cityRealtimeAgentModelBindingStatusSuperseded = "superseded"
	cityRealtimeAgentModelBindingStatusDisabled   = "disabled"
)

var cityRealtimeAgentModelDefinitionCodes = []string{
	"character.npc",
	"character.user",
	"system.npc_manager",
	"system.root",
}

// CityRealtimeAgentModelProfile is the administrator-visible projection of an
// immutable Model Profile version. It intentionally contains no endpoint,
// credential, account, upstream response or prompt body.
type CityRealtimeAgentModelProfile struct {
	Code                        string    `json:"code"`
	Version                     int       `json:"version"`
	DisplayName                 string    `json:"display_name"`
	ProviderCode                string    `json:"provider_code"`
	ProviderClass               string    `json:"provider_class"`
	RouteRef                    string    `json:"route_ref"`
	PlatformGroupID             *int64    `json:"platform_group_id,omitempty"`
	ModelIdentifier             string    `json:"model_identifier"`
	AllowedAgentDefinitionCodes []string  `json:"allowed_agent_definition_codes"`
	RequestSchemaVersion        string    `json:"request_schema_version"`
	ResponseSchemaVersion       string    `json:"response_schema_version"`
	Temperature                 float64   `json:"temperature"`
	MaxInputTokens              int       `json:"max_input_tokens"`
	MaxOutputTokens             int       `json:"max_output_tokens"`
	TimeoutMS                   int       `json:"timeout_ms"`
	MaxConcurrency              int       `json:"max_concurrency"`
	RetryLimit                  int       `json:"retry_limit"`
	MaxProfileHourlyRequests    int       `json:"max_profile_hourly_requests"`
	MaxProfileHourlyTokens      int64     `json:"max_profile_hourly_tokens"`
	MaxWorldHourlyRequests      int       `json:"max_world_hourly_requests"`
	MaxWorldHourlyTokens        int64     `json:"max_world_hourly_tokens"`
	MaxAgentHourlyRequests      int       `json:"max_agent_hourly_requests"`
	MaxAgentHourlyTokens        int64     `json:"max_agent_hourly_tokens"`
	MaxOwnerHourlyRequests      int       `json:"max_owner_hourly_requests"`
	MaxOwnerHourlyTokens        int64     `json:"max_owner_hourly_tokens"`
	CircuitBreakerFailures      int       `json:"circuit_breaker_failure_threshold"`
	CircuitBreakerCooldownSecs  int       `json:"circuit_breaker_cooldown_seconds"`
	PrivacyClass                string    `json:"privacy_class"`
	RetentionPolicy             string    `json:"retention_policy"`
	FallbackPolicy              string    `json:"fallback_policy"`
	ProfileHash                 string    `json:"profile_hash"`
	BudgetHash                  string    `json:"budget_hash"`
	Status                      string    `json:"status,omitempty"`
	CreatedByUserID             *int64    `json:"created_by_user_id,omitempty"`
	CreatedAt                   time.Time `json:"created_at"`
}

// CityRealtimeAgentModelProfileCreateInput never accepts a raw URL, API key,
// account ID, prompt or response. The service derives route_ref and all
// snapshot hashes from this bounded configuration.
type CityRealtimeAgentModelProfileCreateInput struct {
	AdministratorUserID         int64
	Code                        string
	DisplayName                 string
	ProviderCode                string
	PlatformGroupID             *int64
	ModelIdentifier             string
	AllowedAgentDefinitionCodes []string
	Temperature                 float64
	MaxInputTokens              int
	MaxOutputTokens             int
	TimeoutMS                   int
	MaxConcurrency              int
	RetryLimit                  int
	MaxProfileHourlyRequests    int
	MaxProfileHourlyTokens      int64
	MaxWorldHourlyRequests      int
	MaxWorldHourlyTokens        int64
	MaxAgentHourlyRequests      int
	MaxAgentHourlyTokens        int64
	MaxOwnerHourlyRequests      int
	MaxOwnerHourlyTokens        int64
	CircuitBreakerFailures      int
	CircuitBreakerCooldownSecs  int
	PrivacyClass                string
	RetentionPolicy             string
	FallbackPolicy              string
}

type CityRealtimeAgentModelProfileHeadUpdateInput struct {
	AdministratorUserID int64
	Code                string
	Version             int
	Status              string
}

// CityRealtimeAgentModelProfileWorldBinding is the admin-only projection of a
// revisioned world definition binding. A user profile picker is intentionally
// deferred until the owner-safe intersection policy exists.
type CityRealtimeAgentModelProfileWorldBinding struct {
	WorldID             int64     `json:"world_id"`
	AgentDefinitionCode string    `json:"agent_definition_code"`
	BindingVersion      int       `json:"binding_version"`
	ProfileCode         string    `json:"profile_code"`
	ProfileVersion      int       `json:"profile_version"`
	ProfileHash         string    `json:"profile_hash"`
	BudgetHash          string    `json:"budget_hash"`
	BindingHash         string    `json:"binding_hash"`
	BindingStatus       string    `json:"binding_status"`
	OwnerSelectable     bool      `json:"owner_selectable"`
	BindingSource       string    `json:"binding_source"`
	ConfiguredByUserID  *int64    `json:"configured_by_user_id,omitempty"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type CityRealtimeAgentModelProfileWorldBindingInput struct {
	AdministratorUserID int64
	WorldID             int64
	AgentDefinitionCode string
	ProfileCode         string
}

// cityRealtimeAgentModelProfileSnapshot is the worker-only exact execution
// contract copied into a request and attempt. It remains outside canonical
// state, but is immutable and database-validated for audit/replay evidence.
type cityRealtimeAgentModelProfileSnapshot struct {
	CityRealtimeAgentModelProfile
}

func cityRealtimeAgentModelProfileCodeValid(value string) bool {
	return cityRealtimeAgentIdentifierValid(value, 64) && len(value) >= 3
}

func cityRealtimeAgentModelIdentifierValid(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for index, character := range value {
		if index == 0 {
			if !((character >= 'A' && character <= 'Z') || (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9')) {
				return false
			}
			continue
		}
		if (character >= 'A' && character <= 'Z') || (character >= 'a' && character <= 'z') ||
			(character >= '0' && character <= '9') || character == '.' || character == '_' ||
			character == ':' || character == '/' || character == '-' {
			continue
		}
		return false
	}
	return true
}

func cityRealtimeAgentModelDefinitionCodeAllowed(value string) bool {
	for _, candidate := range cityRealtimeAgentModelDefinitionCodes {
		if candidate == value {
			return true
		}
	}
	return false
}

func cityRealtimeAgentModelDefinitionCodesNormalized(values []string) ([]string, bool) {
	if len(values) == 0 || len(values) > len(cityRealtimeAgentModelDefinitionCodes) {
		return nil, false
	}
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if !cityRealtimeAgentModelDefinitionCodeAllowed(value) {
			return nil, false
		}
		normalized = append(normalized, value)
	}
	sort.Strings(normalized)
	for index := 1; index < len(normalized); index++ {
		if normalized[index] == normalized[index-1] {
			return nil, false
		}
	}
	return normalized, true
}

func cityRealtimeAgentModelProfileRecordValid(profile CityRealtimeAgentModelProfile) bool {
	if !cityRealtimeAgentModelProfileCodeValid(profile.Code) || profile.Version <= 0 ||
		strings.TrimSpace(profile.DisplayName) == "" || len(profile.DisplayName) > 120 ||
		!cityRealtimeAgentIdentifierValid(profile.ProviderCode, 64) ||
		!cityRealtimeAgentModelIdentifierValid(profile.ModelIdentifier) ||
		profile.RequestSchemaVersion != cityRealtimeAgentModelObservationSchema ||
		profile.ResponseSchemaVersion != cityRealtimeAgentDecisionEnvelopeVersion ||
		math.IsNaN(profile.Temperature) || math.IsInf(profile.Temperature, 0) ||
		profile.Temperature < 0 || profile.Temperature > 2 ||
		profile.MaxInputTokens < 1 || profile.MaxInputTokens > 262144 ||
		profile.MaxOutputTokens < 1 || profile.MaxOutputTokens > 65536 ||
		profile.TimeoutMS < 100 || profile.TimeoutMS > 300000 ||
		profile.MaxConcurrency < 1 || profile.MaxConcurrency > 4096 ||
		profile.RetryLimit < 0 || profile.RetryLimit > 8 ||
		profile.MaxProfileHourlyRequests < 1 || profile.MaxProfileHourlyRequests > 1000000 ||
		profile.MaxProfileHourlyTokens < 1 || profile.MaxProfileHourlyTokens > 10000000000 ||
		profile.MaxWorldHourlyRequests < 1 || profile.MaxWorldHourlyRequests > 1000000 ||
		profile.MaxWorldHourlyTokens < 1 || profile.MaxWorldHourlyTokens > 10000000000 ||
		profile.MaxAgentHourlyRequests < 1 || profile.MaxAgentHourlyRequests > 100000 ||
		profile.MaxAgentHourlyTokens < 1 || profile.MaxAgentHourlyTokens > 1000000000 ||
		profile.MaxOwnerHourlyRequests < 1 || profile.MaxOwnerHourlyRequests > 100000 ||
		profile.MaxOwnerHourlyTokens < 1 || profile.MaxOwnerHourlyTokens > 1000000000 ||
		profile.CircuitBreakerFailures < 1 || profile.CircuitBreakerFailures > 100 ||
		profile.CircuitBreakerCooldownSecs < 1 || profile.CircuitBreakerCooldownSecs > 86400 ||
		(profile.PrivacyClass != "hash_only" && profile.PrivacyClass != "redacted") ||
		(profile.RetentionPolicy != "hash_only" && profile.RetentionPolicy != "audit_minimum") ||
		(profile.FallbackPolicy != "no_op" && profile.FallbackPolicy != "defer") ||
		!cityRealtimeSHA256Hex(profile.ProfileHash) || !cityRealtimeSHA256Hex(profile.BudgetHash) {
		return false
	}
	if _, ok := cityRealtimeAgentModelDefinitionCodesNormalized(profile.AllowedAgentDefinitionCodes); !ok {
		return false
	}
	switch profile.ProviderCode {
	case cityRealtimeAgentModelProviderFake:
		return profile.ProviderClass == "deterministic" && profile.RouteRef == "system.fake.deterministic" &&
			profile.PlatformGroupID == nil && profile.ModelIdentifier == "deterministic-v1"
	case cityRealtimeAgentModelProviderGateway:
		return profile.ProviderClass == "sub2api_group" && profile.PlatformGroupID != nil && *profile.PlatformGroupID > 0 &&
			profile.RouteRef == "group:"+strconv.FormatInt(*profile.PlatformGroupID, 10)
	default:
		return false
	}
}

func cityRealtimeAgentModelProfileCreateInputNormalized(input CityRealtimeAgentModelProfileCreateInput) (CityRealtimeAgentModelProfileCreateInput, string, string, error) {
	input.Code = strings.ToLower(strings.TrimSpace(input.Code))
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.ProviderCode = strings.ToLower(strings.TrimSpace(input.ProviderCode))
	input.ModelIdentifier = strings.TrimSpace(input.ModelIdentifier)
	input.PrivacyClass = strings.ToLower(strings.TrimSpace(input.PrivacyClass))
	input.RetentionPolicy = strings.ToLower(strings.TrimSpace(input.RetentionPolicy))
	input.FallbackPolicy = strings.ToLower(strings.TrimSpace(input.FallbackPolicy))
	definitions, definitionsOK := cityRealtimeAgentModelDefinitionCodesNormalized(input.AllowedAgentDefinitionCodes)
	if !definitionsOK {
		return CityRealtimeAgentModelProfileCreateInput{}, "", "", ErrCityInvalidInput.WithMetadata(map[string]string{"field": "allowed_agent_definition_codes"})
	}
	input.AllowedAgentDefinitionCodes = definitions
	if input.AdministratorUserID <= 0 || !cityRealtimeAgentModelProfileCodeValid(input.Code) ||
		input.DisplayName == "" || len(input.DisplayName) > 120 || !cityRealtimeAgentModelIdentifierValid(input.ModelIdentifier) ||
		math.IsNaN(input.Temperature) || math.IsInf(input.Temperature, 0) || input.Temperature < 0 || input.Temperature > 2 ||
		input.MaxInputTokens < 1 || input.MaxInputTokens > 262144 ||
		input.MaxOutputTokens < 1 || input.MaxOutputTokens > 65536 ||
		input.TimeoutMS < 100 || input.TimeoutMS > 300000 ||
		input.MaxConcurrency < 1 || input.MaxConcurrency > 4096 ||
		input.RetryLimit < 0 || input.RetryLimit > 8 ||
		input.MaxProfileHourlyRequests < 1 || input.MaxProfileHourlyRequests > 1000000 ||
		input.MaxProfileHourlyTokens < 1 || input.MaxProfileHourlyTokens > 10000000000 ||
		input.MaxWorldHourlyRequests < 1 || input.MaxWorldHourlyRequests > 1000000 ||
		input.MaxWorldHourlyTokens < 1 || input.MaxWorldHourlyTokens > 10000000000 ||
		input.MaxAgentHourlyRequests < 1 || input.MaxAgentHourlyRequests > 100000 ||
		input.MaxAgentHourlyTokens < 1 || input.MaxAgentHourlyTokens > 1000000000 ||
		input.MaxOwnerHourlyRequests < 1 || input.MaxOwnerHourlyRequests > 100000 ||
		input.MaxOwnerHourlyTokens < 1 || input.MaxOwnerHourlyTokens > 1000000000 ||
		input.CircuitBreakerFailures < 1 || input.CircuitBreakerFailures > 100 ||
		input.CircuitBreakerCooldownSecs < 1 || input.CircuitBreakerCooldownSecs > 86400 ||
		(input.PrivacyClass != "hash_only" && input.PrivacyClass != "redacted") ||
		(input.RetentionPolicy != "hash_only" && input.RetentionPolicy != "audit_minimum") ||
		(input.FallbackPolicy != "no_op" && input.FallbackPolicy != "defer") {
		return CityRealtimeAgentModelProfileCreateInput{}, "", "", ErrCityInvalidInput
	}
	switch input.ProviderCode {
	case cityRealtimeAgentModelProviderFake:
		if input.PlatformGroupID != nil || input.ModelIdentifier != "deterministic-v1" {
			return CityRealtimeAgentModelProfileCreateInput{}, "", "", ErrCityInvalidInput.WithMetadata(map[string]string{"field": "deterministic_route"})
		}
		return input, "deterministic", "system.fake.deterministic", nil
	case cityRealtimeAgentModelProviderGateway:
		if input.PlatformGroupID == nil || *input.PlatformGroupID <= 0 {
			return CityRealtimeAgentModelProfileCreateInput{}, "", "", ErrCityInvalidInput.WithMetadata(map[string]string{"field": "platform_group_id"})
		}
		return input, "sub2api_group", "group:" + strconv.FormatInt(*input.PlatformGroupID, 10), nil
	default:
		return CityRealtimeAgentModelProfileCreateInput{}, "", "", ErrCityInvalidInput.WithMetadata(map[string]string{"field": "provider_code"})
	}
}

func enableCityRealtimeAgentModelProfileConfigGate(ctx context.Context, tx *sql.Tx) error {
	if tx == nil {
		return ErrCityInvalidInput
	}
	if _, err := tx.ExecContext(ctx, `SELECT set_config('sub2api.city_realtime_agent_model_profile_config', 'on', TRUE)`); err != nil {
		return fmt.Errorf("enable realtime agent model profile configuration gate: %w", err)
	}
	return nil
}

func enableCityRealtimeAgentModelProfileGenesisGate(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if tx == nil || worldID <= 0 {
		return ErrCityInvalidInput
	}
	if _, err := tx.ExecContext(ctx,
		`SELECT set_config('sub2api.city_realtime_agent_model_profile_genesis_world_id', $1, TRUE)`,
		strconv.FormatInt(worldID, 10),
	); err != nil {
		return fmt.Errorf("enable realtime agent model profile genesis gate: %w", err)
	}
	return nil
}

func enableCityRealtimeAgentModelBudgetWorkerGate(ctx context.Context, tx *sql.Tx, worldID int64, requestCode string) error {
	if err := enableCityRealtimeAgentDecisionWorkerGate(ctx, tx, worldID, requestCode); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `SELECT set_config('sub2api.city_realtime_agent_model_budget_worker', 'on', TRUE)`); err != nil {
		return fmt.Errorf("enable realtime agent model budget worker gate: %w", err)
	}
	return nil
}

const cityRealtimeAgentModelProfileColumns = `
profile_code, profile_version, display_name, provider_code, provider_class,
route_ref, platform_group_id, model_identifier, allowed_agent_definition_codes,
request_schema_version, response_schema_version, temperature,
max_input_tokens, max_output_tokens, timeout_ms, max_concurrency, retry_limit,
max_profile_hourly_requests, max_profile_hourly_tokens,
max_world_hourly_requests, max_world_hourly_tokens,
max_agent_hourly_requests, max_agent_hourly_tokens,
max_owner_hourly_requests, max_owner_hourly_tokens,
circuit_breaker_failure_threshold, circuit_breaker_cooldown_seconds,
privacy_class, retention_policy, fallback_policy, profile_hash, budget_hash,
created_by_user_id, created_at`

func cityRealtimeAgentModelProfileColumnsFor(alias string) string {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return cityRealtimeAgentModelProfileColumns
	}
	columns := strings.TrimSpace(cityRealtimeAgentModelProfileColumns)
	columns = strings.ReplaceAll(columns, ", ", ", "+alias+".")
	columns = strings.ReplaceAll(columns, ",\n", ",\n"+alias+".")
	return alias + "." + columns
}

func scanCityRealtimeAgentModelProfile(row cityScannable) (*CityRealtimeAgentModelProfile, error) {
	profile := &CityRealtimeAgentModelProfile{}
	var platformGroupID, createdByUserID sql.NullInt64
	var rawDefinitions []byte
	if err := row.Scan(
		&profile.Code, &profile.Version, &profile.DisplayName, &profile.ProviderCode, &profile.ProviderClass,
		&profile.RouteRef, &platformGroupID, &profile.ModelIdentifier, &rawDefinitions,
		&profile.RequestSchemaVersion, &profile.ResponseSchemaVersion, &profile.Temperature,
		&profile.MaxInputTokens, &profile.MaxOutputTokens, &profile.TimeoutMS, &profile.MaxConcurrency, &profile.RetryLimit,
		&profile.MaxProfileHourlyRequests, &profile.MaxProfileHourlyTokens,
		&profile.MaxWorldHourlyRequests, &profile.MaxWorldHourlyTokens,
		&profile.MaxAgentHourlyRequests, &profile.MaxAgentHourlyTokens,
		&profile.MaxOwnerHourlyRequests, &profile.MaxOwnerHourlyTokens,
		&profile.CircuitBreakerFailures, &profile.CircuitBreakerCooldownSecs,
		&profile.PrivacyClass, &profile.RetentionPolicy, &profile.FallbackPolicy,
		&profile.ProfileHash, &profile.BudgetHash, &createdByUserID, &profile.CreatedAt,
	); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(rawDefinitions, &profile.AllowedAgentDefinitionCodes); err != nil {
		return nil, fmt.Errorf("decode realtime agent model profile definitions: %w", err)
	}
	profile.PlatformGroupID = cityRealtimeAgentNullInt64Pointer(platformGroupID)
	profile.CreatedByUserID = cityRealtimeAgentNullInt64Pointer(createdByUserID)
	if !cityRealtimeAgentModelProfileRecordValid(*profile) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_model_profile"})
	}
	return profile, nil
}

func loadCityRealtimeAgentModelProfileVersion(
	ctx context.Context,
	queryer citySQLQueryer,
	profileCode string,
	profileVersion int,
) (*CityRealtimeAgentModelProfile, bool, error) {
	if queryer == nil || !cityRealtimeAgentModelProfileCodeValid(profileCode) || profileVersion <= 0 {
		return nil, false, ErrCityInvalidInput
	}
	profile, err := scanCityRealtimeAgentModelProfile(queryer.QueryRowContext(ctx, `
SELECT `+cityRealtimeAgentModelProfileColumns+`
FROM city_realtime_agent_model_profile_versions
WHERE profile_code = $1 AND profile_version = $2`, profileCode, profileVersion))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("load realtime agent model profile version: %w", err)
	}
	return profile, true, nil
}

func loadCityRealtimeAgentModelProfileHead(
	ctx context.Context,
	queryer citySQLQueryer,
	profileCode string,
	forUpdate bool,
) (int, string, bool, error) {
	if queryer == nil || !cityRealtimeAgentModelProfileCodeValid(profileCode) {
		return 0, "", false, ErrCityInvalidInput
	}
	query := `
SELECT active_version, status
FROM city_realtime_agent_model_profile_heads
WHERE profile_code = $1`
	if forUpdate {
		query += ` FOR UPDATE`
	}
	var version int
	var status string
	err := queryer.QueryRowContext(ctx, query, profileCode).Scan(&version, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", false, nil
	}
	if err != nil {
		return 0, "", false, fmt.Errorf("load realtime agent model profile head: %w", err)
	}
	if version <= 0 || (status != cityRealtimeAgentModelProfileStatusActive && status != cityRealtimeAgentModelProfileStatusDisabled && status != cityRealtimeAgentModelProfileStatusRetired) {
		return 0, "", false, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_model_profile_head"})
	}
	return version, status, true, nil
}

func cityRealtimeAgentModelProfileAuditHash(payload map[string]any) (string, error) {
	_, hash, err := cityRealtimeCanonicalJSONObject(payload)
	if err != nil {
		return "", fmt.Errorf("canonicalize realtime agent model profile audit: %w", err)
	}
	return hash, nil
}

func insertCityRealtimeAgentModelProfileAuditEvent(
	ctx context.Context,
	tx *sql.Tx,
	profileCode string,
	profileVersion *int,
	worldID *int64,
	definitionCode *string,
	eventType string,
	actorUserID *int64,
	payload map[string]any,
) error {
	if tx == nil || !cityRealtimeAgentModelProfileCodeValid(profileCode) {
		return ErrCityInvalidInput
	}
	payloadHash, err := cityRealtimeAgentModelProfileAuditHash(payload)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_realtime_agent_model_profile_audit_events
    (profile_code, profile_version, world_id, agent_definition_code,
     event_type, actor_user_id, payload_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, '{}'::jsonb)`,
		profileCode, profileVersion, worldID, definitionCode, eventType, actorUserID, payloadHash,
	); err != nil {
		return fmt.Errorf("insert realtime agent model profile audit event: %w", err)
	}
	return nil
}

// CreateRealtimeAgentModelProfile appends one immutable profile version and
// atomically moves the administrator-controlled head to it. Existing bindings
// and queued requests remain pinned to their prior snapshots.
func (s *CityEconomyService) CreateRealtimeAgentModelProfile(
	ctx context.Context,
	input CityRealtimeAgentModelProfileCreateInput,
) (*CityRealtimeAgentModelProfile, error) {
	if !IsCitySystemAdministrator(ctx) {
		return nil, ErrCityManagementRequired
	}
	normalized, providerClass, routeRef, err := cityRealtimeAgentModelProfileCreateInputNormalized(input)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin realtime agent model profile create: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1)::bigint)`, normalized.Code); err != nil {
		return nil, fmt.Errorf("lock realtime agent model profile code: %w", err)
	}
	if err = enableCityRealtimeAgentModelProfileConfigGate(ctx, tx); err != nil {
		return nil, err
	}
	if normalized.ProviderCode == cityRealtimeAgentModelProviderGateway {
		var active bool
		if err = tx.QueryRowContext(ctx, `
SELECT status = 'active' AND deleted_at IS NULL
FROM groups
WHERE id = $1`, *normalized.PlatformGroupID).Scan(&active); errors.Is(err, sql.ErrNoRows) || !active {
			return nil, ErrCityRealtimeAgentModelProfileUnavailable.WithMetadata(map[string]string{"field": "platform_group_id"})
		} else if err != nil {
			return nil, fmt.Errorf("load realtime agent model profile route group: %w", err)
		}
	}
	var nextVersion int
	if err = tx.QueryRowContext(ctx, `
SELECT COALESCE(MAX(profile_version), 0) + 1
FROM city_realtime_agent_model_profile_versions
WHERE profile_code = $1`, normalized.Code).Scan(&nextVersion); err != nil {
		return nil, fmt.Errorf("allocate realtime agent model profile version: %w", err)
	}
	rawDefinitions, err := json.Marshal(normalized.AllowedAgentDefinitionCodes)
	if err != nil {
		return nil, fmt.Errorf("marshal realtime agent model profile definitions: %w", err)
	}
	profile := &CityRealtimeAgentModelProfile{
		Code: normalized.Code, Version: nextVersion, DisplayName: normalized.DisplayName,
		ProviderCode: normalized.ProviderCode, ProviderClass: providerClass, RouteRef: routeRef,
		PlatformGroupID: normalized.PlatformGroupID, ModelIdentifier: normalized.ModelIdentifier,
		AllowedAgentDefinitionCodes: normalized.AllowedAgentDefinitionCodes,
		RequestSchemaVersion:        cityRealtimeAgentModelObservationSchema,
		ResponseSchemaVersion:       cityRealtimeAgentDecisionEnvelopeVersion,
		Temperature:                 normalized.Temperature, MaxInputTokens: normalized.MaxInputTokens,
		MaxOutputTokens: normalized.MaxOutputTokens, TimeoutMS: normalized.TimeoutMS,
		MaxConcurrency: normalized.MaxConcurrency, RetryLimit: normalized.RetryLimit,
		MaxProfileHourlyRequests: normalized.MaxProfileHourlyRequests, MaxProfileHourlyTokens: normalized.MaxProfileHourlyTokens,
		MaxWorldHourlyRequests: normalized.MaxWorldHourlyRequests, MaxWorldHourlyTokens: normalized.MaxWorldHourlyTokens,
		MaxAgentHourlyRequests: normalized.MaxAgentHourlyRequests, MaxAgentHourlyTokens: normalized.MaxAgentHourlyTokens,
		MaxOwnerHourlyRequests: normalized.MaxOwnerHourlyRequests, MaxOwnerHourlyTokens: normalized.MaxOwnerHourlyTokens,
		CircuitBreakerFailures: normalized.CircuitBreakerFailures, CircuitBreakerCooldownSecs: normalized.CircuitBreakerCooldownSecs,
		PrivacyClass: normalized.PrivacyClass, RetentionPolicy: normalized.RetentionPolicy, FallbackPolicy: normalized.FallbackPolicy,
	}
	if err = tx.QueryRowContext(ctx, `
INSERT INTO city_realtime_agent_model_profile_versions (
    profile_code, profile_version, display_name, provider_code, provider_class,
    route_ref, platform_group_id, model_identifier, allowed_agent_definition_codes,
    request_schema_version, response_schema_version, temperature,
    max_input_tokens, max_output_tokens, timeout_ms, max_concurrency, retry_limit,
    max_profile_hourly_requests, max_profile_hourly_tokens,
    max_world_hourly_requests, max_world_hourly_tokens,
    max_agent_hourly_requests, max_agent_hourly_tokens,
    max_owner_hourly_requests, max_owner_hourly_tokens,
    circuit_breaker_failure_threshold, circuit_breaker_cooldown_seconds,
    privacy_class, retention_policy, fallback_policy, profile_hash, budget_hash,
    created_by_user_id, metadata
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb, $10, $11, $12,
        $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25,
        $26, $27, $28, $29, $30, '', '', $31, '{}'::jsonb)
RETURNING profile_hash, budget_hash, created_at`,
		profile.Code, profile.Version, profile.DisplayName, profile.ProviderCode, profile.ProviderClass,
		profile.RouteRef, profile.PlatformGroupID, profile.ModelIdentifier, rawDefinitions,
		profile.RequestSchemaVersion, profile.ResponseSchemaVersion, profile.Temperature,
		profile.MaxInputTokens, profile.MaxOutputTokens, profile.TimeoutMS, profile.MaxConcurrency, profile.RetryLimit,
		profile.MaxProfileHourlyRequests, profile.MaxProfileHourlyTokens,
		profile.MaxWorldHourlyRequests, profile.MaxWorldHourlyTokens,
		profile.MaxAgentHourlyRequests, profile.MaxAgentHourlyTokens,
		profile.MaxOwnerHourlyRequests, profile.MaxOwnerHourlyTokens,
		profile.CircuitBreakerFailures, profile.CircuitBreakerCooldownSecs,
		profile.PrivacyClass, profile.RetentionPolicy, profile.FallbackPolicy,
		normalized.AdministratorUserID,
	).Scan(&profile.ProfileHash, &profile.BudgetHash, &profile.CreatedAt); err != nil {
		return nil, fmt.Errorf("insert realtime agent model profile version: %w", err)
	}
	profile.Status = cityRealtimeAgentModelProfileStatusActive
	profile.CreatedByUserID = &normalized.AdministratorUserID
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_realtime_agent_model_profile_heads
    (profile_code, active_version, status, updated_by_user_id, metadata)
VALUES ($1, $2, 'active', $3, '{}'::jsonb)
ON CONFLICT (profile_code) DO UPDATE
SET active_version = EXCLUDED.active_version,
    status = 'active',
    updated_by_user_id = EXCLUDED.updated_by_user_id,
    updated_at = NOW()`, profile.Code, profile.Version, normalized.AdministratorUserID); err != nil {
		return nil, fmt.Errorf("move realtime agent model profile head: %w", err)
	}
	version := profile.Version
	actor := normalized.AdministratorUserID
	if err = insertCityRealtimeAgentModelProfileAuditEvent(ctx, tx, profile.Code, &version, nil, nil,
		"profile_created", &actor, map[string]any{
			"profile_code": profile.Code, "profile_version": profile.Version,
			"profile_hash": profile.ProfileHash, "budget_hash": profile.BudgetHash,
		}); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit realtime agent model profile create: %w", err)
	}
	return profile, nil
}

func (s *CityEconomyService) SetRealtimeAgentModelProfileHead(
	ctx context.Context,
	input CityRealtimeAgentModelProfileHeadUpdateInput,
) (*CityRealtimeAgentModelProfile, error) {
	if !IsCitySystemAdministrator(ctx) || input.AdministratorUserID <= 0 {
		return nil, ErrCityManagementRequired
	}
	profileCode := strings.ToLower(strings.TrimSpace(input.Code))
	status := strings.ToLower(strings.TrimSpace(input.Status))
	if !cityRealtimeAgentModelProfileCodeValid(profileCode) || input.Version <= 0 ||
		(status != cityRealtimeAgentModelProfileStatusActive && status != cityRealtimeAgentModelProfileStatusDisabled && status != cityRealtimeAgentModelProfileStatusRetired) {
		return nil, ErrCityInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin realtime agent model profile head update: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err = enableCityRealtimeAgentModelProfileConfigGate(ctx, tx); err != nil {
		return nil, err
	}
	profile, found, err := loadCityRealtimeAgentModelProfileVersion(ctx, tx, profileCode, input.Version)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrCityRealtimeAgentModelProfileNotFound
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_realtime_agent_model_profile_heads
SET active_version = $2, status = $3, updated_by_user_id = $4, updated_at = NOW()
WHERE profile_code = $1`, profileCode, input.Version, status, input.AdministratorUserID)
	if err != nil {
		return nil, fmt.Errorf("update realtime agent model profile head: %w", err)
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr != nil {
		return nil, fmt.Errorf("count realtime agent model profile head update: %w", rowsErr)
	} else if rows != 1 {
		return nil, ErrCityRealtimeAgentModelProfileNotFound
	}
	version := input.Version
	actor := input.AdministratorUserID
	if err = insertCityRealtimeAgentModelProfileAuditEvent(ctx, tx, profileCode, &version, nil, nil,
		"profile_head_updated", &actor, map[string]any{
			"profile_code": profileCode, "profile_version": input.Version, "status": status,
			"profile_hash": profile.ProfileHash,
		}); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit realtime agent model profile head update: %w", err)
	}
	profile.Status = status
	return profile, nil
}

func (s *CityEconomyService) ListRealtimeAgentModelProfiles(ctx context.Context) ([]CityRealtimeAgentModelProfile, error) {
	if !IsCitySystemAdministrator(ctx) {
		return nil, ErrCityManagementRequired
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT `+cityRealtimeAgentModelProfileColumnsFor("profile")+`
FROM city_realtime_agent_model_profile_heads head
JOIN city_realtime_agent_model_profile_versions profile
  ON profile.profile_code = head.profile_code AND profile.profile_version = head.active_version
ORDER BY profile.profile_code ASC`)
	if err != nil {
		return nil, fmt.Errorf("list realtime agent model profiles: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]CityRealtimeAgentModelProfile, 0)
	for rows.Next() {
		profile, scanErr := scanCityRealtimeAgentModelProfile(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		_, status, found, headErr := loadCityRealtimeAgentModelProfileHead(ctx, s.db, profile.Code, false)
		if headErr != nil {
			return nil, headErr
		}
		if !found {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_model_profile_head"})
		}
		profile.Status = status
		items = append(items, *profile)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate realtime agent model profiles: %w", err)
	}
	return items, nil
}

func scanCityRealtimeAgentModelProfileWorldBinding(row cityScannable) (*CityRealtimeAgentModelProfileWorldBinding, error) {
	binding := &CityRealtimeAgentModelProfileWorldBinding{}
	var configuredByUserID sql.NullInt64
	if err := row.Scan(
		&binding.WorldID, &binding.AgentDefinitionCode, &binding.BindingVersion,
		&binding.ProfileCode, &binding.ProfileVersion, &binding.ProfileHash, &binding.BudgetHash,
		&binding.BindingHash, &binding.BindingStatus, &binding.OwnerSelectable, &binding.BindingSource,
		&configuredByUserID, &binding.CreatedAt, &binding.UpdatedAt,
	); err != nil {
		return nil, err
	}
	binding.ConfiguredByUserID = cityRealtimeAgentNullInt64Pointer(configuredByUserID)
	if binding.WorldID <= 0 || !cityRealtimeAgentModelDefinitionCodeAllowed(binding.AgentDefinitionCode) ||
		binding.BindingVersion <= 0 || !cityRealtimeAgentModelProfileCodeValid(binding.ProfileCode) ||
		binding.ProfileVersion <= 0 || !cityRealtimeSHA256Hex(binding.ProfileHash) ||
		!cityRealtimeSHA256Hex(binding.BudgetHash) || !cityRealtimeSHA256Hex(binding.BindingHash) ||
		(binding.BindingStatus != cityRealtimeAgentModelBindingStatusActive &&
			binding.BindingStatus != cityRealtimeAgentModelBindingStatusSuperseded &&
			binding.BindingStatus != cityRealtimeAgentModelBindingStatusDisabled) ||
		(binding.BindingSource != "system_genesis" && binding.BindingSource != "administrator") {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_model_profile_world_binding"})
	}
	if (binding.BindingSource == "system_genesis" && binding.ConfiguredByUserID != nil) ||
		(binding.BindingSource == "administrator" && binding.ConfiguredByUserID == nil) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_model_profile_world_binding_actor"})
	}
	return binding, nil
}

const cityRealtimeAgentModelProfileWorldBindingColumns = `
world_id, agent_definition_code, binding_version,
profile_code, profile_version, profile_hash, budget_hash,
binding_hash, binding_status, owner_selectable, binding_source,
configured_by_user_id, created_at, updated_at`

func loadCityRealtimeAgentModelProfileWorldBinding(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	definitionCode string,
	bindingVersion int,
	forUpdate bool,
) (*CityRealtimeAgentModelProfileWorldBinding, bool, error) {
	if queryer == nil || worldID <= 0 || !cityRealtimeAgentModelDefinitionCodeAllowed(definitionCode) || bindingVersion <= 0 {
		return nil, false, ErrCityInvalidInput
	}
	query := `
SELECT ` + cityRealtimeAgentModelProfileWorldBindingColumns + `
FROM city_realtime_agent_model_profile_world_bindings
WHERE world_id = $1 AND agent_definition_code = $2 AND binding_version = $3`
	if forUpdate {
		query += ` FOR UPDATE`
	}
	binding, err := scanCityRealtimeAgentModelProfileWorldBinding(queryer.QueryRowContext(ctx, query, worldID, definitionCode, bindingVersion))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("load realtime agent model profile world binding: %w", err)
	}
	return binding, true, nil
}

func cityRealtimeAgentModelProfileForAgent(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	agent cityRealtimeAgentInstance,
) (*cityRealtimeAgentModelProfileSnapshot, error) {
	if queryer == nil || worldID <= 0 || !cityRealtimeAgentIdentifierValid(agent.AgentCode, 96) ||
		!cityRealtimeAgentModelDefinitionCodeAllowed(agent.DefinitionCode) {
		return nil, ErrCityInvalidInput
	}
	profile, err := scanCityRealtimeAgentModelProfile(queryer.QueryRowContext(ctx, `
SELECT `+cityRealtimeAgentModelProfileColumnsFor("profile")+`
FROM city_realtime_agent_model_profile_world_bindings binding
JOIN city_realtime_agent_model_profile_versions profile
  ON profile.profile_code = binding.profile_code
 AND profile.profile_version = binding.profile_version
WHERE binding.world_id = $1
  AND binding.agent_definition_code = $2
  AND binding.binding_status = 'active'`, worldID, agent.DefinitionCode))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("resolve realtime agent model profile binding: %w", err)
	}
	_, status, found, err := loadCityRealtimeAgentModelProfileHead(ctx, queryer, profile.Code, false)
	if err != nil {
		return nil, err
	}
	if !found || status != cityRealtimeAgentModelProfileStatusActive {
		return nil, ErrCityRealtimeAgentModelProfileUnavailable.WithMetadata(map[string]string{"field": "profile_head"})
	}
	allowed := false
	for _, definitionCode := range profile.AllowedAgentDefinitionCodes {
		if definitionCode == agent.DefinitionCode {
			allowed = true
			break
		}
	}
	if !allowed {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_model_profile_definition"})
	}
	profile.Status = status
	return &cityRealtimeAgentModelProfileSnapshot{CityRealtimeAgentModelProfile: *profile}, nil
}

func (s *CityEconomyService) BindRealtimeAgentModelProfileToWorld(
	ctx context.Context,
	input CityRealtimeAgentModelProfileWorldBindingInput,
) (*CityRealtimeAgentModelProfileWorldBinding, error) {
	if !IsCitySystemAdministrator(ctx) || input.AdministratorUserID <= 0 {
		return nil, ErrCityManagementRequired
	}
	profileCode := strings.ToLower(strings.TrimSpace(input.ProfileCode))
	definitionCode := strings.TrimSpace(input.AgentDefinitionCode)
	if input.WorldID <= 0 || !cityRealtimeAgentModelProfileCodeValid(profileCode) || !cityRealtimeAgentModelDefinitionCodeAllowed(definitionCode) {
		return nil, ErrCityInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin realtime agent model profile world binding: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, cityWorldLockKey(input.WorldID)); err != nil {
		return nil, fmt.Errorf("lock realtime agent model profile world: %w", err)
	}
	if err = enableCityRealtimeAgentModelProfileConfigGate(ctx, tx); err != nil {
		return nil, err
	}
	if _, err = lockCityWorld(ctx, tx, 0, input.WorldID); err != nil {
		return nil, err
	}
	profileVersion, status, found, err := loadCityRealtimeAgentModelProfileHead(ctx, tx, profileCode, true)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrCityRealtimeAgentModelProfileNotFound
	}
	if status != cityRealtimeAgentModelProfileStatusActive {
		return nil, ErrCityRealtimeAgentModelProfileUnavailable.WithMetadata(map[string]string{"field": "profile_head"})
	}
	profile, profileFound, err := loadCityRealtimeAgentModelProfileVersion(ctx, tx, profileCode, profileVersion)
	if err != nil {
		return nil, err
	}
	if !profileFound {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_model_profile_head_version"})
	}
	allowed := false
	for _, candidate := range profile.AllowedAgentDefinitionCodes {
		if candidate == definitionCode {
			allowed = true
			break
		}
	}
	if !allowed {
		return nil, ErrCityRealtimeAgentModelProfileUnavailable.WithMetadata(map[string]string{"field": "agent_definition_code"})
	}
	var existingVersion int
	var existingProfileCode string
	var existingProfileVersion int
	err = tx.QueryRowContext(ctx, `
SELECT binding_version, profile_code, profile_version
FROM city_realtime_agent_model_profile_world_bindings
WHERE world_id = $1 AND agent_definition_code = $2 AND binding_status = 'active'
FOR UPDATE`, input.WorldID, definitionCode).Scan(&existingVersion, &existingProfileCode, &existingProfileVersion)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("load active realtime agent model profile world binding: %w", err)
	}
	if err == nil && existingProfileCode == profileCode && existingProfileVersion == profileVersion {
		binding, bindingFound, loadErr := loadCityRealtimeAgentModelProfileWorldBinding(ctx, tx, input.WorldID, definitionCode, existingVersion, false)
		if loadErr != nil {
			return nil, loadErr
		}
		if !bindingFound {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_model_profile_world_binding"})
		}
		if err = tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit realtime agent model profile world binding replay: %w", err)
		}
		return binding, nil
	}
	var nextBindingVersion int
	if err = tx.QueryRowContext(ctx, `
SELECT COALESCE(MAX(binding_version), 0) + 1
FROM city_realtime_agent_model_profile_world_bindings
WHERE world_id = $1 AND agent_definition_code = $2`, input.WorldID, definitionCode).Scan(&nextBindingVersion); err != nil {
		return nil, fmt.Errorf("allocate realtime agent model profile binding version: %w", err)
	}
	if existingVersion > 0 {
		if _, err = tx.ExecContext(ctx, `
UPDATE city_realtime_agent_model_profile_world_bindings
SET binding_status = 'superseded', updated_at = NOW()
WHERE world_id = $1 AND agent_definition_code = $2 AND binding_version = $3
  AND binding_status = 'active'`, input.WorldID, definitionCode, existingVersion); err != nil {
			return nil, fmt.Errorf("supersede realtime agent model profile world binding: %w", err)
		}
		oldVersion := existingProfileVersion
		actor := input.AdministratorUserID
		worldID := input.WorldID
		definition := definitionCode
		if err = insertCityRealtimeAgentModelProfileAuditEvent(ctx, tx, existingProfileCode, &oldVersion, &worldID, &definition,
			"world_binding_superseded", &actor, map[string]any{
				"world_id": worldID, "agent_definition_code": definitionCode,
				"binding_version": existingVersion,
			}); err != nil {
			return nil, err
		}
	}
	binding := &CityRealtimeAgentModelProfileWorldBinding{
		WorldID: input.WorldID, AgentDefinitionCode: definitionCode, BindingVersion: nextBindingVersion,
		ProfileCode: profileCode, ProfileVersion: profileVersion,
		BindingStatus: cityRealtimeAgentModelBindingStatusActive, OwnerSelectable: false,
		BindingSource: "administrator", ConfiguredByUserID: &input.AdministratorUserID,
	}
	if err = tx.QueryRowContext(ctx, `
INSERT INTO city_realtime_agent_model_profile_world_bindings (
    world_id, agent_definition_code, binding_version,
    profile_code, profile_version, profile_hash, budget_hash, binding_hash,
    binding_status, owner_selectable, binding_source, configured_by_user_id, metadata
)
VALUES ($1, $2, $3, $4, $5, '', '', $6, 'active', FALSE, 'administrator', $7, '{}'::jsonb)
RETURNING profile_hash, budget_hash, binding_hash, created_at, updated_at`,
		binding.WorldID, binding.AgentDefinitionCode, binding.BindingVersion,
		binding.ProfileCode, binding.ProfileVersion, strings.Repeat("0", 64), input.AdministratorUserID,
	).Scan(&binding.ProfileHash, &binding.BudgetHash, &binding.BindingHash, &binding.CreatedAt, &binding.UpdatedAt); err != nil {
		return nil, fmt.Errorf("insert realtime agent model profile world binding: %w", err)
	}
	version := profileVersion
	actor := input.AdministratorUserID
	worldID := input.WorldID
	definition := definitionCode
	if err = insertCityRealtimeAgentModelProfileAuditEvent(ctx, tx, profileCode, &version, &worldID, &definition,
		"world_binding_created", &actor, map[string]any{
			"world_id": worldID, "agent_definition_code": definitionCode,
			"binding_version": binding.BindingVersion, "binding_hash": binding.BindingHash,
			"profile_hash": binding.ProfileHash,
		}); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit realtime agent model profile world binding: %w", err)
	}
	return binding, nil
}

func (s *CityEconomyService) ListRealtimeAgentModelProfileWorldBindings(
	ctx context.Context,
	worldID int64,
) ([]CityRealtimeAgentModelProfileWorldBinding, error) {
	if !IsCitySystemAdministrator(ctx) {
		return nil, ErrCityManagementRequired
	}
	if worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT `+cityRealtimeAgentModelProfileWorldBindingColumns+`
FROM city_realtime_agent_model_profile_world_bindings
WHERE world_id = $1
ORDER BY agent_definition_code ASC, binding_version DESC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("list realtime agent model profile world bindings: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]CityRealtimeAgentModelProfileWorldBinding, 0)
	for rows.Next() {
		binding, scanErr := scanCityRealtimeAgentModelProfileWorldBinding(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, *binding)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate realtime agent model profile world bindings: %w", err)
	}
	return items, nil
}

type cityRealtimeAgentModelBudgetScope struct {
	Kind           string
	Key            string
	MaxRequests    int
	MaxTotalTokens int64
}

const (
	cityRealtimeAgentModelCircuitBreakerClosed   = "closed"
	cityRealtimeAgentModelCircuitBreakerOpen     = "open"
	cityRealtimeAgentModelCircuitBreakerHalfOpen = "half_open"
)

type cityRealtimeAgentModelCircuitBreaker struct {
	ProfileCode                 string
	ProfileVersion              int
	ProfileHash                 string
	BudgetHash                  string
	State                       string
	ConsecutiveProviderFailures int
	OpenedAt                    *time.Time
	CooldownUntil               *time.Time
	ProbeRequestCode            *string
	ProbeLeaseExpiresAt         *time.Time
	LastProviderFailureAt       *time.Time
	LastSuccessAt               *time.Time
}

func lockCityRealtimeAgentModelProfileRuntime(ctx context.Context, tx *sql.Tx, profile *CityRealtimeAgentModelProfile) error {
	if tx == nil || profile == nil || !cityRealtimeAgentModelProfileRecordValid(*profile) {
		return ErrCityInvalidInput
	}
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1)::bigint)`,
		"city-realtime-agent-model-runtime:"+profile.Code+":"+strconv.Itoa(profile.Version),
	); err != nil {
		return fmt.Errorf("lock realtime agent model profile runtime: %w", err)
	}
	return nil
}

func scanCityRealtimeAgentModelCircuitBreaker(row cityScannable) (*cityRealtimeAgentModelCircuitBreaker, error) {
	item := &cityRealtimeAgentModelCircuitBreaker{}
	var openedAt, cooldownUntil, probeLeaseExpiresAt, lastProviderFailureAt, lastSuccessAt sql.NullTime
	var probeRequestCode sql.NullString
	if err := row.Scan(
		&item.ProfileCode, &item.ProfileVersion, &item.ProfileHash, &item.BudgetHash,
		&item.State, &item.ConsecutiveProviderFailures,
		&openedAt, &cooldownUntil, &probeRequestCode, &probeLeaseExpiresAt,
		&lastProviderFailureAt, &lastSuccessAt,
	); err != nil {
		return nil, err
	}
	item.OpenedAt = cityRealtimeAgentNullTimePointer(openedAt)
	item.CooldownUntil = cityRealtimeAgentNullTimePointer(cooldownUntil)
	item.ProbeRequestCode = cityRealtimeAgentNullStringPointer(probeRequestCode)
	item.ProbeLeaseExpiresAt = cityRealtimeAgentNullTimePointer(probeLeaseExpiresAt)
	item.LastProviderFailureAt = cityRealtimeAgentNullTimePointer(lastProviderFailureAt)
	item.LastSuccessAt = cityRealtimeAgentNullTimePointer(lastSuccessAt)
	if !cityRealtimeAgentModelCircuitBreakerValid(*item) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_model_circuit_breaker"})
	}
	return item, nil
}

func cityRealtimeAgentNullTimePointer(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time.UTC().Truncate(time.Microsecond)
	return &result
}

func cityRealtimeAgentModelCircuitBreakerValid(item cityRealtimeAgentModelCircuitBreaker) bool {
	if !cityRealtimeAgentModelProfileCodeValid(item.ProfileCode) || item.ProfileVersion <= 0 ||
		!cityRealtimeSHA256Hex(item.ProfileHash) || !cityRealtimeSHA256Hex(item.BudgetHash) ||
		item.ConsecutiveProviderFailures < 0 || item.ConsecutiveProviderFailures > 32767 {
		return false
	}
	switch item.State {
	case cityRealtimeAgentModelCircuitBreakerClosed:
		return item.OpenedAt == nil && item.CooldownUntil == nil &&
			item.ProbeRequestCode == nil && item.ProbeLeaseExpiresAt == nil
	case cityRealtimeAgentModelCircuitBreakerOpen:
		return item.ConsecutiveProviderFailures > 0 && item.OpenedAt != nil && item.CooldownUntil != nil &&
			item.CooldownUntil.After(*item.OpenedAt) && item.ProbeRequestCode == nil && item.ProbeLeaseExpiresAt == nil
	case cityRealtimeAgentModelCircuitBreakerHalfOpen:
		return item.ConsecutiveProviderFailures > 0 && item.OpenedAt != nil && item.CooldownUntil != nil &&
			item.CooldownUntil.After(*item.OpenedAt) && item.ProbeRequestCode != nil &&
			cityRealtimeAgentIdentifierValid(*item.ProbeRequestCode, 96) && item.ProbeLeaseExpiresAt != nil &&
			item.ProbeLeaseExpiresAt.After(*item.CooldownUntil)
	default:
		return false
	}
}

func loadCityRealtimeAgentModelCircuitBreakerForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	profile *CityRealtimeAgentModelProfile,
) (*cityRealtimeAgentModelCircuitBreaker, bool, error) {
	if tx == nil || profile == nil || !cityRealtimeAgentModelProfileRecordValid(*profile) {
		return nil, false, ErrCityInvalidInput
	}
	item, err := scanCityRealtimeAgentModelCircuitBreaker(tx.QueryRowContext(ctx, `
SELECT profile_code, profile_version, profile_hash, budget_hash,
       breaker_state, consecutive_provider_failures,
       opened_at, cooldown_until, probe_request_code, probe_lease_expires_at,
       last_provider_failure_at, last_success_at
FROM city_realtime_agent_model_circuit_breakers
WHERE profile_code = $1 AND profile_version = $2
FOR UPDATE`, profile.Code, profile.Version))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("load realtime agent model circuit breaker: %w", err)
	}
	if item.ProfileHash != profile.ProfileHash || item.BudgetHash != profile.BudgetHash {
		return nil, false, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_model_circuit_snapshot"})
	}
	return item, true, nil
}

func ensureCityRealtimeAgentModelCircuitBreakerClosed(
	ctx context.Context,
	tx *sql.Tx,
	profile *CityRealtimeAgentModelProfile,
) (*cityRealtimeAgentModelCircuitBreaker, error) {
	item, found, err := loadCityRealtimeAgentModelCircuitBreakerForUpdate(ctx, tx, profile)
	if err != nil {
		return nil, err
	}
	if found {
		return item, nil
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_realtime_agent_model_circuit_breakers (
    profile_code, profile_version, profile_hash, budget_hash,
    breaker_state, consecutive_provider_failures, metadata
)
VALUES ($1, $2, $3, $4, 'closed', 0, '{}'::jsonb)`,
		profile.Code, profile.Version, profile.ProfileHash, profile.BudgetHash,
	); err != nil {
		return nil, fmt.Errorf("initialize realtime agent model circuit breaker: %w", err)
	}
	return &cityRealtimeAgentModelCircuitBreaker{
		ProfileCode: profile.Code, ProfileVersion: profile.Version,
		ProfileHash: profile.ProfileHash, BudgetHash: profile.BudgetHash,
		State: cityRealtimeAgentModelCircuitBreakerClosed,
	}, nil
}

// acquireCityRealtimeAgentModelCircuitBreaker permits a single provider call
// or returns a fail-closed availability error. The half-open claim is tied to
// a request and short lease so a crashed worker cannot permanently strand a
// profile after its cooldown.
func acquireCityRealtimeAgentModelCircuitBreaker(
	ctx context.Context,
	tx *sql.Tx,
	profile *CityRealtimeAgentModelProfile,
	requestCode string,
	now time.Time,
) error {
	if profile == nil || !cityRealtimeAgentIdentifierValid(requestCode, 96) || now.IsZero() {
		return ErrCityInvalidInput
	}
	if err := lockCityRealtimeAgentModelProfileRuntime(ctx, tx, profile); err != nil {
		return err
	}
	breaker, err := ensureCityRealtimeAgentModelCircuitBreakerClosed(ctx, tx, profile)
	if err != nil {
		return err
	}
	now = now.UTC().Truncate(time.Microsecond)
	switch breaker.State {
	case cityRealtimeAgentModelCircuitBreakerClosed:
		return nil
	case cityRealtimeAgentModelCircuitBreakerOpen:
		if breaker.CooldownUntil == nil || breaker.OpenedAt == nil {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_model_circuit_open"})
		}
		if breaker.CooldownUntil.After(now) {
			return ErrCityRealtimeAgentProviderUnavailable.WithMetadata(map[string]string{"state": breaker.State})
		}
	case cityRealtimeAgentModelCircuitBreakerHalfOpen:
		if breaker.ProbeLeaseExpiresAt == nil || breaker.OpenedAt == nil || breaker.CooldownUntil == nil {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_model_circuit_half_open"})
		}
		if breaker.ProbeLeaseExpiresAt.After(now) {
			return ErrCityRealtimeAgentProviderUnavailable.WithMetadata(map[string]string{"state": breaker.State})
		}
	default:
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_model_circuit_state"})
	}
	probeLeaseExpiresAt := now.Add(cityRealtimeAgentDecisionLeaseDurationForProfile(profile)).UTC().Truncate(time.Microsecond)
	result, err := tx.ExecContext(ctx, `
UPDATE city_realtime_agent_model_circuit_breakers
SET breaker_state = 'half_open', probe_request_code = $3,
    probe_lease_expires_at = $4, updated_at = NOW()
WHERE profile_code = $1 AND profile_version = $2`,
		profile.Code, profile.Version, requestCode, probeLeaseExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("claim realtime agent model circuit probe: %w", err)
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr != nil {
		return fmt.Errorf("count realtime agent model circuit probe claim: %w", rowsErr)
	} else if rows != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_model_circuit_probe"})
	}
	return nil
}

func closeCityRealtimeAgentModelCircuitBreaker(
	ctx context.Context,
	tx *sql.Tx,
	profile *CityRealtimeAgentModelProfile,
	now time.Time,
) error {
	if profile == nil || now.IsZero() {
		return ErrCityInvalidInput
	}
	if err := lockCityRealtimeAgentModelProfileRuntime(ctx, tx, profile); err != nil {
		return err
	}
	if _, err := ensureCityRealtimeAgentModelCircuitBreakerClosed(ctx, tx, profile); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_realtime_agent_model_circuit_breakers
SET breaker_state = 'closed', consecutive_provider_failures = 0,
    opened_at = NULL, cooldown_until = NULL,
    probe_request_code = NULL, probe_lease_expires_at = NULL,
    last_success_at = $3, updated_at = NOW()
WHERE profile_code = $1 AND profile_version = $2`,
		profile.Code, profile.Version, now.UTC().Truncate(time.Microsecond),
	)
	if err != nil {
		return fmt.Errorf("close realtime agent model circuit breaker: %w", err)
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr != nil {
		return fmt.Errorf("count realtime agent model circuit close: %w", rowsErr)
	} else if rows != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_model_circuit_close"})
	}
	return nil
}

func cityRealtimeAgentModelProviderFailureAttributable(errorCode string) bool {
	switch strings.TrimSpace(errorCode) {
	case "provider_timeout", "provider_rate_limited", "provider_unavailable", "provider_transport", "provider_5xx":
		return true
	default:
		return false
	}
}

func recordCityRealtimeAgentModelProviderFailure(
	ctx context.Context,
	tx *sql.Tx,
	profile *CityRealtimeAgentModelProfile,
	errorCode string,
	now time.Time,
) error {
	if profile == nil || !cityRealtimeAgentModelProviderFailureAttributable(errorCode) || now.IsZero() {
		return ErrCityInvalidInput
	}
	if err := lockCityRealtimeAgentModelProfileRuntime(ctx, tx, profile); err != nil {
		return err
	}
	breaker, err := ensureCityRealtimeAgentModelCircuitBreakerClosed(ctx, tx, profile)
	if err != nil {
		return err
	}
	now = now.UTC().Truncate(time.Microsecond)
	failures := breaker.ConsecutiveProviderFailures + 1
	if breaker.State == cityRealtimeAgentModelCircuitBreakerHalfOpen && failures < profile.CircuitBreakerFailures {
		failures = profile.CircuitBreakerFailures
	}
	if failures >= profile.CircuitBreakerFailures {
		openedAt := now
		cooldownUntil := openedAt.Add(time.Duration(profile.CircuitBreakerCooldownSecs) * time.Second)
		result, updateErr := tx.ExecContext(ctx, `
UPDATE city_realtime_agent_model_circuit_breakers
SET breaker_state = 'open', consecutive_provider_failures = $3,
    opened_at = $4, cooldown_until = $5,
    probe_request_code = NULL, probe_lease_expires_at = NULL,
    last_provider_failure_at = $4, updated_at = NOW()
WHERE profile_code = $1 AND profile_version = $2`,
			profile.Code, profile.Version, failures, openedAt, cooldownUntil,
		)
		if updateErr != nil {
			return fmt.Errorf("open realtime agent model circuit breaker: %w", updateErr)
		}
		if rows, rowsErr := result.RowsAffected(); rowsErr != nil {
			return fmt.Errorf("count realtime agent model circuit open: %w", rowsErr)
		} else if rows != 1 {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_model_circuit_open"})
		}
		return nil
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_realtime_agent_model_circuit_breakers
SET breaker_state = 'closed', consecutive_provider_failures = $3,
    opened_at = NULL, cooldown_until = NULL,
    probe_request_code = NULL, probe_lease_expires_at = NULL,
    last_provider_failure_at = $4, updated_at = NOW()
WHERE profile_code = $1 AND profile_version = $2`,
		profile.Code, profile.Version, failures, now,
	)
	if err != nil {
		return fmt.Errorf("record realtime agent model provider failure: %w", err)
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr != nil {
		return fmt.Errorf("count realtime agent model provider failure: %w", rowsErr)
	} else if rows != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_model_circuit_failure"})
	}
	return nil
}

// reserveCityRealtimeAgentModelAttemptBudget creates an append-only upper
// bound for one started attempt. It deliberately never trusts a provider's
// later usage report and never returns a reservation on failure: retrying a
// request must not turn a transient provider failure into unlimited free work.
func reserveCityRealtimeAgentModelAttemptBudget(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	request cityRealtimeAgentDecisionRequestRecord,
	attempt cityRealtimeAgentDecisionAttemptRecord,
	profile *CityRealtimeAgentModelProfile,
	now time.Time,
) error {
	if tx == nil || worldID <= 0 || profile == nil ||
		!cityRealtimeAgentDecisionRequestRecordValid(request) ||
		!cityRealtimeAgentDecisionAttemptRecordValid(attempt) ||
		request.ModelProfileCode == nil || attempt.ModelProfileCode == nil ||
		request.RequestCode != attempt.RequestCode || request.AgentCode == "" ||
		profile.Code != *request.ModelProfileCode || profile.Version != *request.ModelProfileVersion ||
		profile.ProfileHash != *request.ModelProfileHash || profile.BudgetHash != *request.ModelBudgetHash ||
		attempt.ModelProfileVersion == nil ||
		attempt.ModelProfileHash == nil || attempt.ModelBudgetHash == nil ||
		attempt.ReservedInputTokens == nil || attempt.ReservedOutputTokens == nil ||
		*attempt.ModelProfileCode != profile.Code || *attempt.ModelProfileVersion != profile.Version ||
		*attempt.ModelProfileHash != profile.ProfileHash || *attempt.ModelBudgetHash != profile.BudgetHash ||
		*attempt.ReservedInputTokens != profile.MaxInputTokens || *attempt.ReservedOutputTokens != profile.MaxOutputTokens {
		return ErrCityInvalidInput
	}
	if now.IsZero() {
		return ErrCityInvalidInput
	}
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1)::bigint)`,
		"city-realtime-agent-model-budget:"+profile.Code+":"+strconv.Itoa(profile.Version),
	); err != nil {
		return fmt.Errorf("lock realtime agent model budget profile: %w", err)
	}
	var activeAttempts int
	if err := tx.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM city_realtime_agent_decision_attempts
WHERE model_profile_code = $1 AND model_profile_version = $2
  AND model_profile_hash = $3 AND model_budget_hash = $4
  AND status = 'started'`, profile.Code, profile.Version, profile.ProfileHash, profile.BudgetHash,
	).Scan(&activeAttempts); err != nil {
		return fmt.Errorf("count realtime agent model profile concurrency: %w", err)
	}
	if activeAttempts > profile.MaxConcurrency {
		return ErrCityRealtimeAgentModelBudgetExceeded.WithMetadata(map[string]string{"scope": "profile_concurrency"})
	}

	var ownerUserID sql.NullInt64
	err := tx.QueryRowContext(ctx, `
SELECT owner_user_id
FROM city_realtime_agent_instances
WHERE world_id = $1 AND agent_code = $2`, worldID, request.AgentCode).Scan(&ownerUserID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_model_budget_agent"})
	}
	if err != nil {
		return fmt.Errorf("load realtime agent model budget owner: %w", err)
	}
	windowStartedAt := now.UTC().Truncate(time.Hour)
	if windowStartedAt.IsZero() {
		return ErrCityInvalidInput
	}
	scopes := []cityRealtimeAgentModelBudgetScope{
		{
			Kind:           "profile",
			Key:            profile.Code + "@" + strconv.Itoa(profile.Version),
			MaxRequests:    profile.MaxProfileHourlyRequests,
			MaxTotalTokens: profile.MaxProfileHourlyTokens,
		},
		{
			Kind:           "world",
			Key:            strconv.FormatInt(worldID, 10),
			MaxRequests:    profile.MaxWorldHourlyRequests,
			MaxTotalTokens: profile.MaxWorldHourlyTokens,
		},
		{
			Kind:           "agent",
			Key:            strconv.FormatInt(worldID, 10) + "/" + request.AgentCode,
			MaxRequests:    profile.MaxAgentHourlyRequests,
			MaxTotalTokens: profile.MaxAgentHourlyTokens,
		},
	}
	if ownerUserID.Valid {
		if ownerUserID.Int64 <= 0 {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_model_budget_owner"})
		}
		scopes = append(scopes, cityRealtimeAgentModelBudgetScope{
			Kind:           "owner",
			Key:            strconv.FormatInt(worldID, 10) + "/" + strconv.FormatInt(ownerUserID.Int64, 10),
			MaxRequests:    profile.MaxOwnerHourlyRequests,
			MaxTotalTokens: profile.MaxOwnerHourlyTokens,
		})
	}
	for _, scope := range scopes {
		if err = reserveCityRealtimeAgentModelUsageWindow(
			ctx, tx, profile, windowStartedAt, scope, worldID, request.RequestCode,
			*attempt.ReservedInputTokens, *attempt.ReservedOutputTokens,
		); err != nil {
			return err
		}
	}
	var ownerValue any
	if ownerUserID.Valid {
		ownerValue = ownerUserID.Int64
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_realtime_agent_model_attempt_budget_reservations (
    world_id, attempt_code, request_code, agent_code, owner_user_id,
    profile_code, profile_version, profile_hash, budget_hash, window_started_at,
    reserved_input_tokens, reserved_output_tokens, metadata
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, '{}'::jsonb)`,
		worldID, attempt.AttemptCode, request.RequestCode, request.AgentCode, ownerValue,
		profile.Code, profile.Version, profile.ProfileHash, profile.BudgetHash, windowStartedAt,
		*attempt.ReservedInputTokens, *attempt.ReservedOutputTokens,
	); err != nil {
		return fmt.Errorf("insert realtime agent model attempt budget reservation: %w", err)
	}
	return nil
}

func reserveCityRealtimeAgentModelUsageWindow(
	ctx context.Context,
	tx *sql.Tx,
	profile *CityRealtimeAgentModelProfile,
	windowStartedAt time.Time,
	scope cityRealtimeAgentModelBudgetScope,
	worldID int64,
	requestCode string,
	reservedInputTokens int,
	reservedOutputTokens int,
) error {
	if tx == nil || profile == nil || worldID <= 0 || windowStartedAt.IsZero() ||
		(scope.Kind != "profile" && scope.Kind != "world" && scope.Kind != "agent" && scope.Kind != "owner") ||
		strings.TrimSpace(scope.Key) == "" || scope.MaxRequests <= 0 || scope.MaxTotalTokens <= 0 ||
		reservedInputTokens <= 0 || reservedOutputTokens <= 0 ||
		int64(reservedInputTokens) > scope.MaxTotalTokens || int64(reservedOutputTokens) > scope.MaxTotalTokens ||
		int64(reservedInputTokens)+int64(reservedOutputTokens) > scope.MaxTotalTokens {
		return ErrCityInvalidInput
	}
	var reservedRequestCount int64
	err := tx.QueryRowContext(ctx, `
INSERT INTO city_realtime_agent_model_usage_windows (
    profile_code, profile_version, profile_hash, budget_hash, window_started_at,
    scope_kind, scope_key, source_world_id, source_request_code,
    reserved_request_count, reserved_input_tokens, reserved_output_tokens
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 1, $10, $11)
ON CONFLICT (profile_code, profile_version, window_started_at, scope_kind, scope_key) DO UPDATE
SET reserved_request_count = city_realtime_agent_model_usage_windows.reserved_request_count + 1,
    reserved_input_tokens = city_realtime_agent_model_usage_windows.reserved_input_tokens + EXCLUDED.reserved_input_tokens,
    reserved_output_tokens = city_realtime_agent_model_usage_windows.reserved_output_tokens + EXCLUDED.reserved_output_tokens,
    updated_at = NOW()
WHERE city_realtime_agent_model_usage_windows.reserved_request_count + 1 <= $12
  AND city_realtime_agent_model_usage_windows.reserved_input_tokens
      + city_realtime_agent_model_usage_windows.reserved_output_tokens
      + EXCLUDED.reserved_input_tokens + EXCLUDED.reserved_output_tokens <= $13
RETURNING reserved_request_count`,
		profile.Code, profile.Version, profile.ProfileHash, profile.BudgetHash, windowStartedAt,
		scope.Kind, scope.Key, worldID, requestCode, reservedInputTokens, reservedOutputTokens,
		scope.MaxRequests, scope.MaxTotalTokens,
	).Scan(&reservedRequestCount)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrCityRealtimeAgentModelBudgetExceeded.WithMetadata(map[string]string{"scope": scope.Kind})
	}
	if err != nil {
		return fmt.Errorf("reserve realtime agent model usage window %s: %w", scope.Kind, err)
	}
	if reservedRequestCount <= 0 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_model_usage_window"})
	}
	return nil
}

// initializeCityRealtimeAgentModelProfileFoundation creates explicit, pinned
// deterministic profile bindings for newly created 1.13 realtime worlds. It
// never backfills historical worlds, preserving their original runtime shape.
func initializeCityRealtimeAgentModelProfileFoundation(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	simulationVersion string,
) error {
	if tx == nil || worldID <= 0 || !cityEngineSupportsRealtimeStaticWorldgen(simulationVersion) {
		return ErrCityInvalidInput
	}
	profile, found, err := loadCityRealtimeAgentModelProfileVersion(
		ctx, tx, cityRealtimeAgentModelProfileDefaultCode, cityRealtimeAgentModelProfileDefaultVersion,
	)
	if err != nil {
		return err
	}
	if !found {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_model_profile_default"})
	}
	activeVersion, status, headFound, err := loadCityRealtimeAgentModelProfileHead(ctx, tx, profile.Code, true)
	if err != nil {
		return err
	}
	if !headFound || status != cityRealtimeAgentModelProfileStatusActive || activeVersion != profile.Version {
		return ErrCityRealtimeAgentModelProfileUnavailable.WithMetadata(map[string]string{"field": "realtime_agent_model_profile_default"})
	}
	if err = enableCityRealtimeAgentModelProfileGenesisGate(ctx, tx, worldID); err != nil {
		return err
	}
	for _, definitionCode := range cityRealtimeAgentModelDefinitionCodes {
		result, insertErr := tx.ExecContext(ctx, `
INSERT INTO city_realtime_agent_model_profile_world_bindings (
    world_id, agent_definition_code, binding_version,
    profile_code, profile_version, profile_hash, budget_hash, binding_hash,
    binding_status, owner_selectable, binding_source, configured_by_user_id, metadata
)
VALUES ($1, $2, 1, $3, $4, $5, $6, $7,
        'active', FALSE, 'system_genesis', NULL, '{}'::jsonb)
ON CONFLICT (world_id, agent_definition_code, binding_version) DO NOTHING`,
			worldID, definitionCode, profile.Code, profile.Version,
			profile.ProfileHash, profile.BudgetHash, strings.Repeat("0", 64),
		)
		if insertErr != nil {
			return fmt.Errorf("insert realtime agent model profile genesis binding: %w", insertErr)
		}
		rows, rowsErr := result.RowsAffected()
		if rowsErr != nil {
			return fmt.Errorf("count realtime agent model profile genesis binding: %w", rowsErr)
		}
		if rows == 1 {
			version := profile.Version
			world := worldID
			definition := definitionCode
			if auditErr := insertCityRealtimeAgentModelProfileAuditEvent(ctx, tx, profile.Code, &version, &world, &definition,
				"world_binding_created", nil, map[string]any{
					"world_id": worldID, "agent_definition_code": definitionCode,
					"binding_version": 1, "source": "system_genesis",
				}); auditErr != nil {
				return auditErr
			}
		}
	}
	if _, err = tx.ExecContext(ctx, `SELECT assert_city_realtime_agent_model_profile_genesis($1)`, worldID); err != nil {
		return fmt.Errorf("validate realtime agent model profile genesis: %w", err)
	}
	return nil
}
