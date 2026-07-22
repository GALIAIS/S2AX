package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
)

const (
	CityOpenWorldRuntimeFactImpactApplied = "impact.applied"

	cityOpenWorldImpactSchemaVersion           = 1
	cityOpenWorldImpactProfileID               = "sub2api-open-world-impact-bridge"
	cityOpenWorldImpactProfileVersion          = "1.0.0"
	cityOpenWorldImpactSourceContractVersion   = "service_response-v1"
	cityOpenWorldImpactDeliveryContractVersion = "next_tick_only-v1"
	cityOpenWorldImpactMaximumSchedulesPerTick = 256
	cityOpenWorldImpactTargetDomainActor       = "actor"
	cityOpenWorldImpactSourceKindService       = "service.response"
)

// CityOpenWorldImpactPolicy freezes the causal bridge used by a V8 world. It
// is intentionally independent of both the service profile and a future
// population, enterprise, building, or environment adapter. The bridge only
// turns a sealed source response into a delayed domain metric application.
type CityOpenWorldImpactPolicy struct {
	ProfileID               string          `json:"profile_id"`
	ProfileVersion          string          `json:"profile_version"`
	ContentHash             string          `json:"content_hash"`
	BaselineTick            int64           `json:"baseline_tick"`
	SourceContractVersion   string          `json:"source_contract_version"`
	DeliveryContractVersion string          `json:"delivery_contract_version"`
	MaximumSchedulesPerTick int             `json:"maximum_schedules_per_tick"`
	EffectCount             int64           `json:"effect_count"`
	AppliedCount            int64           `json:"applied_count"`
	MetricCount             int64           `json:"metric_count"`
	Revision                int64           `json:"revision"`
	Metadata                json.RawMessage `json:"metadata"`
}

// CityOpenWorldImpactCatalogEntry maps one source response outcome to one
// domain metric. New domains use the same sealed contract rather than adding
// a backdoor directly from a service queue into their mutable projection.
type CityOpenWorldImpactCatalogEntry struct {
	Code                    string          `json:"code"`
	SourceKind              string          `json:"source_kind"`
	ServiceCode             string          `json:"service_code"`
	Outcome                 string          `json:"outcome"`
	TargetDomain            string          `json:"target_domain"`
	MetricCode              string          `json:"metric_code"`
	DeltaUnitsPerSourceUnit int64           `json:"delta_units_per_source_unit"`
	Version                 string          `json:"version"`
	ContentHash             string          `json:"content_hash"`
	Metadata                json.RawMessage `json:"metadata"`
}

// CityOpenWorldImpactEffect is a response-backed projection. scheduled rows
// cannot be read as a current effect: only an applied row has an application
// fact, before/after values, and a runtime-effect audit record.
type CityOpenWorldImpactEffect struct {
	Code               string                       `json:"code"`
	SourceResponseCode string                       `json:"source_response_code"`
	SourceFact         CityOpenWorldRuntimeFactRef  `json:"source_fact"`
	CatalogCode        string                       `json:"catalog_code"`
	TargetDomain       string                       `json:"target_domain"`
	TargetCode         string                       `json:"target_code"`
	MetricCode         string                       `json:"metric_code"`
	SourceUnits        int64                        `json:"source_units"`
	DeltaUnits         int64                        `json:"delta_units"`
	ScheduledTick      int64                        `json:"scheduled_tick"`
	EffectiveTick      int64                        `json:"effective_tick"`
	Status             string                       `json:"status"`
	AppliedTick        *int64                       `json:"applied_tick,omitempty"`
	ApplicationFact    *CityOpenWorldRuntimeFactRef `json:"application_fact,omitempty"`
	BeforeUnits        *int64                       `json:"before_units,omitempty"`
	AfterUnits         *int64                       `json:"after_units,omitempty"`
	Version            int64                        `json:"version"`
	Metadata           json.RawMessage              `json:"metadata"`
}

// CityOpenWorldImpactMetric is the current deterministic projection of all
// applied bridge effects for one domain target. V8 seeds actor metrics only;
// later adapters can consume additional target domains without changing the
// response/queue semantics that created them.
type CityOpenWorldImpactMetric struct {
	TargetDomain    string          `json:"target_domain"`
	TargetCode      string          `json:"target_code"`
	MetricCode      string          `json:"metric_code"`
	ValueUnits      int64           `json:"value_units"`
	LastAppliedTick int64           `json:"last_applied_tick"`
	LastEffectCode  string          `json:"last_effect_code"`
	Version         int64           `json:"version"`
	Metadata        json.RawMessage `json:"metadata"`
}

// CityOpenWorldImpactState is retained in V8 canonical state. Empty effect
// and metric lists are meaningful: they prove the bridge exists but no prior
// response has crossed the one-tick causal boundary yet.
type CityOpenWorldImpactState struct {
	Policy  CityOpenWorldImpactPolicy         `json:"policy"`
	Catalog []CityOpenWorldImpactCatalogEntry `json:"catalog"`
	Effects []CityOpenWorldImpactEffect       `json:"effects"`
	Metrics []CityOpenWorldImpactMetric       `json:"metrics"`
}

type cityOpenWorldImpactCatalogSeed struct {
	Code                    string `json:"code"`
	ServiceCode             string `json:"service_code"`
	Outcome                 string `json:"outcome"`
	TargetDomain            string `json:"target_domain"`
	MetricCode              string `json:"metric_code"`
	DeltaUnitsPerSourceUnit int64  `json:"delta_units_per_source_unit"`
}

func builtInCityOpenWorldImpactCatalog() ([]CityOpenWorldImpactCatalogEntry, string, error) {
	seeds := []cityOpenWorldImpactCatalogSeed{
		{Code: "impact.education.basic.served", ServiceCode: "education.basic", Outcome: "served", TargetDomain: cityOpenWorldImpactTargetDomainActor, MetricCode: "service.education.basic.access", DeltaUnitsPerSourceUnit: 1_000},
		{Code: "impact.education.basic.expired", ServiceCode: "education.basic", Outcome: "expired", TargetDomain: cityOpenWorldImpactTargetDomainActor, MetricCode: "service.education.basic.shortage", DeltaUnitsPerSourceUnit: 1_000},
		{Code: "impact.health.primary.served", ServiceCode: "health.primary", Outcome: "served", TargetDomain: cityOpenWorldImpactTargetDomainActor, MetricCode: "service.health.primary.access", DeltaUnitsPerSourceUnit: 1_000},
		{Code: "impact.health.primary.expired", ServiceCode: "health.primary", Outcome: "expired", TargetDomain: cityOpenWorldImpactTargetDomainActor, MetricCode: "service.health.primary.shortage", DeltaUnitsPerSourceUnit: 1_000},
		{Code: "impact.safety.emergency.served", ServiceCode: "safety.emergency", Outcome: "served", TargetDomain: cityOpenWorldImpactTargetDomainActor, MetricCode: "service.safety.emergency.access", DeltaUnitsPerSourceUnit: 1_000},
		{Code: "impact.safety.emergency.expired", ServiceCode: "safety.emergency", Outcome: "expired", TargetDomain: cityOpenWorldImpactTargetDomainActor, MetricCode: "service.safety.emergency.shortage", DeltaUnitsPerSourceUnit: 1_000},
		{Code: "impact.civic.support.served", ServiceCode: "civic.support", Outcome: "served", TargetDomain: cityOpenWorldImpactTargetDomainActor, MetricCode: "service.civic.support.access", DeltaUnitsPerSourceUnit: 1_000},
		{Code: "impact.civic.support.expired", ServiceCode: "civic.support", Outcome: "expired", TargetDomain: cityOpenWorldImpactTargetDomainActor, MetricCode: "service.civic.support.shortage", DeltaUnitsPerSourceUnit: 1_000},
	}
	entries := make([]CityOpenWorldImpactCatalogEntry, 0, len(seeds))
	for _, seed := range seeds {
		raw, err := json.Marshal(struct {
			SchemaVersion int                            `json:"schema_version"`
			Definition    cityOpenWorldImpactCatalogSeed `json:"definition"`
		}{SchemaVersion: cityOpenWorldImpactSchemaVersion, Definition: seed})
		if err != nil {
			return nil, "", fmt.Errorf("marshal open-world impact catalog %s: %w", seed.Code, err)
		}
		sum := sha256.Sum256(raw)
		metadata, err := json.Marshal(map[string]any{
			"schema_version":      cityOpenWorldImpactSchemaVersion,
			"effect_visibility":   "next_tick_only",
			"adapter_contract":    "domain_metric_v1",
			"metric_accumulation": "saturating_add",
		})
		if err != nil {
			return nil, "", fmt.Errorf("marshal open-world impact metadata %s: %w", seed.Code, err)
		}
		entries = append(entries, CityOpenWorldImpactCatalogEntry{
			Code: seed.Code, SourceKind: cityOpenWorldImpactSourceKindService,
			ServiceCode: seed.ServiceCode, Outcome: seed.Outcome, TargetDomain: seed.TargetDomain,
			MetricCode: seed.MetricCode, DeltaUnitsPerSourceUnit: seed.DeltaUnitsPerSourceUnit,
			Version: cityOpenWorldImpactProfileVersion, ContentHash: hex.EncodeToString(sum[:]), Metadata: metadata,
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Code < entries[j].Code })
	raw, err := json.Marshal(entries)
	if err != nil {
		return nil, "", fmt.Errorf("marshal open-world impact catalog: %w", err)
	}
	sum := sha256.Sum256(raw)
	return entries, hex.EncodeToString(sum[:]), nil
}

func cityOpenWorldImpactProfileHash(catalogHash string) (string, error) {
	raw, err := json.Marshal(struct {
		SchemaVersion           int    `json:"schema_version"`
		CatalogHash             string `json:"catalog_hash"`
		SourceContractVersion   string `json:"source_contract_version"`
		DeliveryContractVersion string `json:"delivery_contract_version"`
		MaximumSchedulesPerTick int    `json:"maximum_schedules_per_tick"`
	}{
		SchemaVersion: cityOpenWorldImpactSchemaVersion, CatalogHash: catalogHash,
		SourceContractVersion:   cityOpenWorldImpactSourceContractVersion,
		DeliveryContractVersion: cityOpenWorldImpactDeliveryContractVersion,
		MaximumSchedulesPerTick: cityOpenWorldImpactMaximumSchedulesPerTick,
	})
	if err != nil {
		return "", fmt.Errorf("marshal open-world impact profile: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func activateCityOpenWorldImpactBootstrapWrite(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if _, err := tx.ExecContext(ctx, `
SELECT set_config('sub2api.city_open_world_impact_bootstrap_world_id', $1, TRUE)`,
		fmt.Sprintf("%d", worldID)); err != nil {
		return fmt.Errorf("enable open-world impact bootstrap: %w", err)
	}
	return nil
}

// initializeCityOpenWorldV8ImpactFoundation writes the immutable V8 bridge
// baseline at genesis or during a paused V7 -> V8 upgrade. Existing V7
// responses remain historical evidence; the baseline tick prevents an
// upgrade from retroactively creating impact rows for them.
func initializeCityOpenWorldV8ImpactFoundation(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if worldID <= 0 {
		return ErrCityInvalidInput
	}
	var simulationVersion string
	var baselineTick int64
	if err := tx.QueryRowContext(ctx, `
SELECT simulation_version, current_tick
FROM city_worlds WHERE id = $1 FOR UPDATE`, worldID).Scan(&simulationVersion, &baselineTick); err != nil {
		return fmt.Errorf("load V8 impact world: %w", err)
	}
	if !cityEngineSupportsOpenWorldImpactBridge(simulationVersion) {
		return ErrCitySimulationVersion.WithMetadata(map[string]string{"version": simulationVersion})
	}
	if _, err := tx.ExecContext(ctx, `SELECT assert_city_open_world_service_foundation($1)`, worldID); err != nil {
		return fmt.Errorf("validate V8 service prerequisite: %w", err)
	}
	if err := activateCityOpenWorldImpactBootstrapWrite(ctx, tx, worldID); err != nil {
		return err
	}
	catalog, catalogHash, err := builtInCityOpenWorldImpactCatalog()
	if err != nil {
		return err
	}
	profileHash, err := cityOpenWorldImpactProfileHash(catalogHash)
	if err != nil {
		return err
	}
	metadata, err := json.Marshal(map[string]any{
		"schema_version":        cityOpenWorldImpactSchemaVersion,
		"catalog_hash":          catalogHash,
		"source_contract":       cityOpenWorldImpactSourceContractVersion,
		"delivery_contract":     cityOpenWorldImpactDeliveryContractVersion,
		"retroactive_responses": "excluded_before_baseline",
		"default_target_domain": cityOpenWorldImpactTargetDomainActor,
	})
	if err != nil {
		return fmt.Errorf("marshal V8 impact profile metadata: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_impact_profiles
    (world_id, profile_id, profile_version, content_hash, baseline_tick,
     source_contract_version, delivery_contract_version, maximum_schedules_per_tick,
     effect_count, applied_count, metric_count, revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 0, 0, 0, 1, $9::jsonb)`,
		worldID, cityOpenWorldImpactProfileID, cityOpenWorldImpactProfileVersion,
		profileHash, baselineTick, cityOpenWorldImpactSourceContractVersion,
		cityOpenWorldImpactDeliveryContractVersion, cityOpenWorldImpactMaximumSchedulesPerTick,
		[]byte(metadata)); err != nil {
		return fmt.Errorf("insert V8 impact profile: %w", err)
	}
	for _, entry := range catalog {
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_impact_catalog
    (world_id, code, source_kind, service_code, outcome, target_domain,
     metric_code, delta_units_per_source_unit, definition_version,
     content_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb)`,
			worldID, entry.Code, entry.SourceKind, entry.ServiceCode, entry.Outcome,
			entry.TargetDomain, entry.MetricCode, entry.DeltaUnitsPerSourceUnit,
			entry.Version, entry.ContentHash, []byte(entry.Metadata)); err != nil {
			return fmt.Errorf("insert V8 impact catalog %s: %w", entry.Code, err)
		}
	}
	if _, err = tx.ExecContext(ctx, `SELECT assert_city_open_world_impact_foundation($1)`, worldID); err != nil {
		return fmt.Errorf("validate V8 impact foundation: %w", err)
	}
	return nil
}

func loadCityOpenWorldImpactState(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*CityOpenWorldImpactState, error) {
	state := &CityOpenWorldImpactState{
		Catalog: make([]CityOpenWorldImpactCatalogEntry, 0),
		Effects: make([]CityOpenWorldImpactEffect, 0),
		Metrics: make([]CityOpenWorldImpactMetric, 0),
	}
	if err := queryer.QueryRowContext(ctx, `
SELECT profile_id, profile_version, content_hash, baseline_tick,
       source_contract_version, delivery_contract_version, maximum_schedules_per_tick,
       effect_count, applied_count, metric_count, revision, metadata
FROM city_open_world_impact_profiles WHERE world_id = $1`, worldID).Scan(
		&state.Policy.ProfileID, &state.Policy.ProfileVersion, &state.Policy.ContentHash,
		&state.Policy.BaselineTick, &state.Policy.SourceContractVersion,
		&state.Policy.DeliveryContractVersion, &state.Policy.MaximumSchedulesPerTick,
		&state.Policy.EffectCount, &state.Policy.AppliedCount, &state.Policy.MetricCount,
		&state.Policy.Revision, &state.Policy.Metadata,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v8_impact_profile"})
		}
		return nil, fmt.Errorf("load V8 impact profile: %w", err)
	}
	catalogRows, err := queryer.QueryContext(ctx, `
SELECT code, source_kind, service_code, outcome, target_domain, metric_code,
       delta_units_per_source_unit, definition_version, content_hash, metadata
FROM city_open_world_impact_catalog
WHERE world_id = $1
ORDER BY code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V8 impact catalog: %w", err)
	}
	for catalogRows.Next() {
		item := CityOpenWorldImpactCatalogEntry{}
		if err = catalogRows.Scan(
			&item.Code, &item.SourceKind, &item.ServiceCode, &item.Outcome,
			&item.TargetDomain, &item.MetricCode, &item.DeltaUnitsPerSourceUnit,
			&item.Version, &item.ContentHash, &item.Metadata,
		); err != nil {
			_ = catalogRows.Close()
			return nil, fmt.Errorf("scan V8 impact catalog: %w", err)
		}
		state.Catalog = append(state.Catalog, item)
	}
	if err = closeCityRows(catalogRows, "iterate V8 impact catalog"); err != nil {
		return nil, err
	}
	effectRows, err := queryer.QueryContext(ctx, `
SELECT effect_value.code, response.code, source_fact.tick, source_fact.sequence,
       effect_value.catalog_code, effect_value.target_domain, effect_value.target_code,
       effect_value.metric_code, effect_value.source_units, effect_value.delta_units,
       effect_value.scheduled_tick, effect_value.effective_tick, effect_value.status,
       effect_value.applied_tick, application_fact.tick, application_fact.sequence,
       effect_value.before_units, effect_value.after_units, effect_value.version,
       effect_value.metadata
FROM city_open_world_impact_effects effect_value
JOIN city_open_world_service_responses response
  ON response.id = effect_value.source_response_id AND response.world_id = effect_value.world_id
JOIN city_open_world_runtime_facts source_fact
  ON source_fact.id = effect_value.source_fact_id AND source_fact.world_id = effect_value.world_id
LEFT JOIN city_open_world_runtime_facts application_fact
  ON application_fact.id = effect_value.application_fact_id AND application_fact.world_id = effect_value.world_id
WHERE effect_value.world_id = $1
ORDER BY effect_value.effective_tick ASC, effect_value.code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V8 impact effects: %w", err)
	}
	for effectRows.Next() {
		item := CityOpenWorldImpactEffect{}
		var appliedTick, applicationTick, applicationSequence sql.NullInt64
		var beforeUnits, afterUnits sql.NullInt64
		if err = effectRows.Scan(
			&item.Code, &item.SourceResponseCode, &item.SourceFact.Tick, &item.SourceFact.Sequence,
			&item.CatalogCode, &item.TargetDomain, &item.TargetCode, &item.MetricCode,
			&item.SourceUnits, &item.DeltaUnits, &item.ScheduledTick, &item.EffectiveTick,
			&item.Status, &appliedTick, &applicationTick, &applicationSequence,
			&beforeUnits, &afterUnits, &item.Version, &item.Metadata,
		); err != nil {
			_ = effectRows.Close()
			return nil, fmt.Errorf("scan V8 impact effect: %w", err)
		}
		if appliedTick.Valid {
			item.AppliedTick = cityOpenWorldInt64Pointer(appliedTick.Int64)
		}
		if applicationTick.Valid {
			item.ApplicationFact = &CityOpenWorldRuntimeFactRef{Tick: applicationTick.Int64, Sequence: applicationSequence.Int64}
		}
		item.BeforeUnits = nullInt64Pointer(beforeUnits)
		item.AfterUnits = nullInt64Pointer(afterUnits)
		state.Effects = append(state.Effects, item)
	}
	if err = closeCityRows(effectRows, "iterate V8 impact effects"); err != nil {
		return nil, err
	}
	metricRows, err := queryer.QueryContext(ctx, `
SELECT metric.target_domain, metric.target_code, metric.metric_code,
       metric.value_units, metric.last_applied_tick, effect_value.code,
       metric.version, metric.metadata
FROM city_open_world_impact_metrics metric
JOIN city_open_world_impact_effects effect_value
  ON effect_value.id = metric.last_effect_id AND effect_value.world_id = metric.world_id
WHERE metric.world_id = $1
ORDER BY metric.target_domain ASC, metric.target_code ASC, metric.metric_code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V8 impact metrics: %w", err)
	}
	for metricRows.Next() {
		item := CityOpenWorldImpactMetric{}
		if err = metricRows.Scan(
			&item.TargetDomain, &item.TargetCode, &item.MetricCode, &item.ValueUnits,
			&item.LastAppliedTick, &item.LastEffectCode, &item.Version, &item.Metadata,
		); err != nil {
			_ = metricRows.Close()
			return nil, fmt.Errorf("scan V8 impact metric: %w", err)
		}
		state.Metrics = append(state.Metrics, item)
	}
	if err = closeCityRows(metricRows, "iterate V8 impact metrics"); err != nil {
		return nil, err
	}
	if err = validateCityOpenWorldImpactState(state); err != nil {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v8_impact_state"}).WithCause(err)
	}
	return state, nil
}

func validateCityOpenWorldImpactState(state *CityOpenWorldImpactState) error {
	if state == nil || state.Policy.ProfileID != cityOpenWorldImpactProfileID ||
		state.Policy.ProfileVersion != cityOpenWorldImpactProfileVersion ||
		state.Policy.SourceContractVersion != cityOpenWorldImpactSourceContractVersion ||
		state.Policy.DeliveryContractVersion != cityOpenWorldImpactDeliveryContractVersion ||
		state.Policy.MaximumSchedulesPerTick != cityOpenWorldImpactMaximumSchedulesPerTick ||
		state.Policy.BaselineTick < 0 || state.Policy.Revision < 1 || !cityWorldVersionHashValid(state.Policy.ContentHash) ||
		!json.Valid(state.Policy.Metadata) {
		return fmt.Errorf("invalid V8 impact profile")
	}
	expected, catalogHash, err := builtInCityOpenWorldImpactCatalog()
	if err != nil {
		return err
	}
	profileHash, err := cityOpenWorldImpactProfileHash(catalogHash)
	if err != nil {
		return err
	}
	if state.Policy.ContentHash != profileHash || len(state.Catalog) != len(expected) {
		return fmt.Errorf("invalid V8 impact catalog profile binding")
	}
	expectedByCode := make(map[string]CityOpenWorldImpactCatalogEntry, len(expected))
	for _, entry := range expected {
		expectedByCode[entry.Code] = entry
	}
	catalogByCode := make(map[string]CityOpenWorldImpactCatalogEntry, len(state.Catalog))
	for _, entry := range state.Catalog {
		expectedEntry, found := expectedByCode[entry.Code]
		if !found || entry.SourceKind != expectedEntry.SourceKind ||
			entry.ServiceCode != expectedEntry.ServiceCode || entry.Outcome != expectedEntry.Outcome ||
			entry.TargetDomain != expectedEntry.TargetDomain || entry.MetricCode != expectedEntry.MetricCode ||
			entry.DeltaUnitsPerSourceUnit != expectedEntry.DeltaUnitsPerSourceUnit ||
			entry.Version != expectedEntry.Version || entry.ContentHash != expectedEntry.ContentHash ||
			!cityOpenWorldJSONEquivalent(entry.Metadata, expectedEntry.Metadata) {
			return fmt.Errorf("invalid V8 impact catalog entry %s", entry.Code)
		}
		if _, exists := catalogByCode[entry.Code]; exists {
			return fmt.Errorf("duplicate V8 impact catalog entry %s", entry.Code)
		}
		catalogByCode[entry.Code] = entry
	}
	appliedCount := int64(0)
	effectByCode := make(map[string]CityOpenWorldImpactEffect, len(state.Effects))
	for _, effect := range state.Effects {
		catalogEntry, catalogFound := catalogByCode[effect.CatalogCode]
		if _, exists := effectByCode[effect.Code]; exists || !catalogFound || !worldRuntimeCodeValid(effect.Code, 160) ||
			effect.SourceResponseCode == "" || effect.SourceFact.Tick < 1 || effect.SourceFact.Sequence < 1 ||
			effect.TargetDomain != cityOpenWorldImpactTargetDomainActor || !worldRuntimeCodeValid(effect.TargetCode, 128) ||
			!worldRuntimeCodeValid(effect.MetricCode, 128) || effect.SourceUnits < 1 || effect.DeltaUnits == 0 ||
			effect.ScheduledTick <= state.Policy.BaselineTick || effect.EffectiveTick != effect.ScheduledTick+1 ||
			effect.TargetDomain != catalogEntry.TargetDomain || effect.MetricCode != catalogEntry.MetricCode ||
			effect.DeltaUnits != effect.SourceUnits*catalogEntry.DeltaUnitsPerSourceUnit ||
			effect.Code != cityOpenWorldImpactEffectCode(effect.SourceResponseCode, effect.CatalogCode) ||
			!json.Valid(effect.Metadata) {
			return fmt.Errorf("invalid V8 impact effect %s", effect.Code)
		}
		effectByCode[effect.Code] = effect
		switch effect.Status {
		case "scheduled":
			if effect.AppliedTick != nil || effect.ApplicationFact != nil || effect.BeforeUnits != nil || effect.AfterUnits != nil {
				return fmt.Errorf("scheduled V8 impact effect %s carries application state", effect.Code)
			}
		case "applied":
			if effect.AppliedTick == nil || *effect.AppliedTick < effect.EffectiveTick ||
				effect.ApplicationFact == nil || effect.ApplicationFact.Tick != *effect.AppliedTick ||
				effect.ApplicationFact.Sequence < 1 ||
				effect.BeforeUnits == nil || effect.AfterUnits == nil ||
				cityOpenWorldRuntimeSaturatingAdd(*effect.BeforeUnits, effect.DeltaUnits) != *effect.AfterUnits {
				return fmt.Errorf("applied V8 impact effect %s is inconsistent", effect.Code)
			}
			appliedCount++
		default:
			return fmt.Errorf("unknown V8 impact effect status %s", effect.Status)
		}
	}
	if state.Policy.EffectCount != int64(len(state.Effects)) || state.Policy.AppliedCount != appliedCount ||
		state.Policy.MetricCount != int64(len(state.Metrics)) {
		return fmt.Errorf("V8 impact profile counters are inconsistent")
	}
	metricKeys := make(map[string]struct{}, len(state.Metrics))
	for _, metric := range state.Metrics {
		key := metric.TargetDomain + "\x00" + metric.TargetCode + "\x00" + metric.MetricCode
		lastEffect, found := effectByCode[metric.LastEffectCode]
		if _, exists := metricKeys[key]; exists || metric.TargetDomain != cityOpenWorldImpactTargetDomainActor ||
			!worldRuntimeCodeValid(metric.TargetCode, 128) || !worldRuntimeCodeValid(metric.MetricCode, 128) ||
			metric.LastAppliedTick < 1 || metric.LastEffectCode == "" || metric.Version < 1 || !json.Valid(metric.Metadata) ||
			!found || lastEffect.Status != "applied" || lastEffect.AppliedTick == nil ||
			metric.LastAppliedTick != *lastEffect.AppliedTick || lastEffect.TargetDomain != metric.TargetDomain ||
			lastEffect.TargetCode != metric.TargetCode || lastEffect.MetricCode != metric.MetricCode {
			return fmt.Errorf("invalid V8 impact metric %s", key)
		}
		metricKeys[key] = struct{}{}
	}
	return nil
}

func cityOpenWorldJSONEquivalent(left, right json.RawMessage) bool {
	var leftValue any
	var rightValue any
	if json.Unmarshal(left, &leftValue) != nil || json.Unmarshal(right, &rightValue) != nil {
		return false
	}
	return reflect.DeepEqual(leftValue, rightValue)
}

// GetCityOpenWorldImpactState applies the same actor visibility contract as
// V7 service history. Catalog/profile data are world-visible, while a normal
// member only receives impact effects and metrics for actors they own or have
// an active read/control grant for.
func (s *CityEconomyService) GetCityOpenWorldImpactState(
	ctx context.Context,
	userID, worldID int64,
) (*CityOpenWorldImpactState, error) {
	if userID <= 0 || worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, userID, worldID); err != nil {
		return nil, err
	}
	state, err := loadCityOpenWorldImpactState(ctx, s.db, worldID)
	if err != nil {
		return nil, err
	}
	all, err := cityOpenWorldServiceMayReadAll(ctx, s.db, userID, worldID)
	if err != nil {
		return nil, err
	}
	if all {
		return state, nil
	}
	visible, err := cityOpenWorldServiceVisibleActorCodes(ctx, s.db, userID, worldID)
	if err != nil {
		return nil, err
	}
	filteredEffects := make([]CityOpenWorldImpactEffect, 0, len(state.Effects))
	for _, effect := range state.Effects {
		if effect.TargetDomain == cityOpenWorldImpactTargetDomainActor {
			if _, found := visible[effect.TargetCode]; found {
				filteredEffects = append(filteredEffects, effect)
			}
		}
	}
	filteredMetrics := make([]CityOpenWorldImpactMetric, 0, len(state.Metrics))
	for _, metric := range state.Metrics {
		if metric.TargetDomain == cityOpenWorldImpactTargetDomainActor {
			if _, found := visible[metric.TargetCode]; found {
				filteredMetrics = append(filteredMetrics, metric)
			}
		}
	}
	state.Effects, state.Metrics = filteredEffects, filteredMetrics
	return state, nil
}
