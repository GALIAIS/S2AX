package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
)

const (
	cityWorldVersionVectorSchemaVersion = 1

	cityWorldVersionComponentContentCatalog     = "content_catalog"
	cityWorldVersionComponentEconomicPolicy     = "economic_policy"
	cityWorldVersionComponentEngine             = "engine"
	cityWorldVersionComponentRuleBundle         = "rule_bundle"
	cityWorldVersionComponentScenario           = "scenario"
	cityWorldVersionComponentSpatialProfile     = "spatial_profile"
	cityWorldVersionComponentWorldgenPlan       = "worldgen_plan"
	cityWorldVersionEngineBundleID              = "sub2api-city-engine"
	cityWorldVersionEconomicPolicyBundleID      = "sub2api-city-economic-policy"
	cityWorldVersionRuleBundleID                = "sub2api-open-world-rule-bundle"
	cityWorldVersionEngineBundleVersionV6       = "6.0.0"
	cityWorldVersionEngineBundleVersionV7       = "7.0.0"
	cityWorldVersionEngineBundleVersionV8       = "8.0.0"
	cityWorldVersionEngineBundleVersionV9       = "9.0.0"
	cityWorldVersionEngineBundleVersionV10      = "10.0.0"
	cityWorldVersionEngineBundleVersionV11      = "11.0.0"
	cityWorldVersionEngineBundleVersionV12      = "12.0.0"
	cityWorldVersionEngineBundleVersionV13      = "13.0.0"
	cityWorldVersionEngineBundleVersionV14      = "14.0.0"
	cityWorldVersionEngineBundleVersionV15      = "15.0.0"
	cityWorldVersionEngineBundleVersionV16      = "16.0.0"
	cityWorldVersionEngineBundleVersionV17      = "17.0.0"
	cityWorldVersionEngineBundleVersionV18      = "18.0.0"
	cityWorldVersionEngineBundleVersionV19      = "19.0.0"
	cityWorldVersionEngineBundleVersionV20      = "20.0.0"
	cityWorldVersionEngineBundleVersionV21      = "21.0.0"
	cityWorldVersionEngineBundleVersionV22      = "22.0.0"
	cityWorldVersionEngineBundleVersionV23      = "23.0.0"
	cityWorldVersionEngineBundleVersionV24      = "24.0.0"
	cityWorldVersionEconomicPolicyBundleVersion = "1.0.0"
	cityWorldVersionContentCatalogV7BundleID    = "sub2api-open-world-service-catalog"
	cityWorldVersionContentCatalogV7Version     = "1.0.0"
	cityWorldVersionContentCatalogV8BundleID    = "sub2api-open-world-impact-catalog"
	cityWorldVersionContentCatalogV8Version     = "1.0.0"
	cityWorldVersionContentCatalogV9BundleID    = "sub2api-open-world-mobility-catalog"
	cityWorldVersionContentCatalogV9Version     = "1.0.0"
	cityWorldVersionContentCatalogV10BundleID   = "sub2api-open-world-mobility-arrival-catalog"
	cityWorldVersionContentCatalogV10Version    = "1.0.0"
	cityWorldVersionContentCatalogV11BundleID   = "sub2api-open-world-mobility-od-catalog"
	cityWorldVersionContentCatalogV11Version    = "1.0.0"
	cityWorldVersionContentCatalogV12BundleID   = "sub2api-open-world-commute-catalog"
	cityWorldVersionContentCatalogV12Version    = "1.0.0"
	cityWorldVersionContentCatalogV13BundleID   = "sub2api-open-world-commute-source-catalog"
	cityWorldVersionContentCatalogV13Version    = "1.0.0"
	cityWorldVersionContentCatalogV14BundleID   = "sub2api-open-world-commute-lifecycle-catalog"
	cityWorldVersionContentCatalogV14Version    = "1.0.0"
	cityWorldVersionContentCatalogV15BundleID   = "sub2api-open-world-supply-chain-catalog"
	cityWorldVersionContentCatalogV15Version    = "1.0.0"
	cityWorldVersionContentCatalogV16BundleID   = "sub2api-open-world-enterprise-freight-catalog"
	cityWorldVersionContentCatalogV16Version    = "1.0.0"
	cityWorldVersionContentCatalogV17BundleID   = "sub2api-open-world-enterprise-freight-receipt-catalog"
	cityWorldVersionContentCatalogV17Version    = "1.0.0"
	cityWorldVersionContentCatalogV18BundleID   = "sub2api-open-world-freight-batch-catalog"
	cityWorldVersionContentCatalogV18Version    = "1.0.0"
	cityWorldVersionContentCatalogV19BundleID   = "sub2api-open-world-spatial-network-catalog"
	cityWorldVersionContentCatalogV19Version    = "1.0.0"
	cityWorldVersionContentCatalogV20BundleID   = "sub2api-open-world-infrastructure-catalog"
	cityWorldVersionContentCatalogV20Version    = "1.0.0"
	cityWorldVersionContentCatalogV21BundleID   = "sub2api-open-world-effective-capacity-catalog"
	cityWorldVersionContentCatalogV21Version    = "1.0.0"
	cityWorldVersionContentCatalogV22BundleID   = "sub2api-open-world-freight-settlement-catalog"
	cityWorldVersionContentCatalogV22Version    = "1.0.0"
	cityWorldVersionContentCatalogV23BundleID   = "sub2api-open-world-carrier-recovery-catalog"
	cityWorldVersionContentCatalogV23Version    = "1.0.0"
	cityWorldVersionContentCatalogV24BundleID   = "sub2api-open-world-carrier-commerce-catalog"
	cityWorldVersionContentCatalogV24Version    = "1.0.0"
)

var cityWorldVersionVectorComponentOrder = []string{
	cityWorldVersionComponentContentCatalog,
	cityWorldVersionComponentEconomicPolicy,
	cityWorldVersionComponentEngine,
	cityWorldVersionComponentRuleBundle,
	cityWorldVersionComponentScenario,
	cityWorldVersionComponentSpatialProfile,
	cityWorldVersionComponentWorldgenPlan,
}

// CityWorldVersionBinding names one immutable input bundle used by a world.
// The vector is intentionally separate from the mutable configuration and
// projections: a running world may act on facts, but it never silently adopts
// whatever bundle happens to ship with a later server build.
type CityWorldVersionBinding struct {
	ComponentCode string          `json:"component_code"`
	BundleID      string          `json:"bundle_id"`
	BundleVersion string          `json:"bundle_version"`
	ContentHash   string          `json:"content_hash"`
	Metadata      json.RawMessage `json:"metadata"`
}

// CityWorldVersionVector is a complete, canonicalized description of the
// immutable version inputs for one versioned open-world generation.
type CityWorldVersionVector struct {
	SchemaVersion int                       `json:"schema_version"`
	Generation    int                       `json:"generation"`
	BaselineTick  int64                     `json:"baseline_tick"`
	Bindings      []CityWorldVersionBinding `json:"bindings"`
}

type cityWorldVersionEconomicPolicy struct {
	SchemaVersion                int   `json:"schema_version"`
	ProjectionVersion            int64 `json:"projection_version"`
	LaborDemandCapacityMilli     int   `json:"labor_demand_capacity_milli"`
	GoodsDemandPopulationDivisor int64 `json:"goods_demand_population_divisor"`
	HouseholdWageTaxMilli        int   `json:"household_wage_tax_milli"`
	FirmSalesTaxMilli            int   `json:"firm_sales_tax_milli"`
	ProcurementShareMilli        int   `json:"procurement_share_milli"`
	SocialSupportShareMilli      int   `json:"social_support_share_milli"`
}

type cityWorldVersionRuleDescriptor struct {
	Code    string `json:"code"`
	Version string `json:"version"`
	Hash    string `json:"hash"`
}

type cityWorldVersionServiceCatalogDescriptor struct {
	Code    string `json:"code"`
	Version string `json:"version"`
	Hash    string `json:"hash"`
}

type cityWorldVersionV7ContentCatalog struct {
	RuntimeID             string                                     `json:"runtime_id"`
	RuntimeCatalogVersion string                                     `json:"runtime_catalog_version"`
	RuntimeCatalogHash    string                                     `json:"runtime_catalog_hash"`
	ServiceProfileID      string                                     `json:"service_profile_id"`
	ServiceProfileVersion string                                     `json:"service_profile_version"`
	ServiceProfileHash    string                                     `json:"service_profile_hash"`
	Services              []cityWorldVersionServiceCatalogDescriptor `json:"services"`
}

type cityWorldVersionImpactCatalogDescriptor struct {
	Code    string `json:"code"`
	Version string `json:"version"`
	Hash    string `json:"hash"`
}

// cityWorldVersionV8ContentCatalog deliberately embeds the V7 descriptor.
// The immutable impact profile/catalog is part of the same content input
// bundle, while service queues and dynamic metric projections remain runtime
// state and therefore do not alter the version vector.
type cityWorldVersionV8ContentCatalog struct {
	cityWorldVersionV7ContentCatalog
	ImpactProfileID      string                                    `json:"impact_profile_id"`
	ImpactProfileVersion string                                    `json:"impact_profile_version"`
	ImpactProfileHash    string                                    `json:"impact_profile_hash"`
	Impacts              []cityWorldVersionImpactCatalogDescriptor `json:"impacts"`
}

type cityWorldVersionMobilityModeDescriptor struct {
	Code    string `json:"code"`
	Version string `json:"version"`
	Hash    string `json:"hash"`
}

type cityWorldVersionMobilityHubDescriptor struct {
	Code    string `json:"code"`
	Version string `json:"version"`
	Hash    string `json:"hash"`
}

type cityWorldVersionMobilityEdgeDescriptor struct {
	Code    string `json:"code"`
	Version string `json:"version"`
	Hash    string `json:"hash"`
}

// cityWorldVersionV9ContentCatalog extends the sealed V8 catalog with the
// aggregate mobility topology. Dynamic demand/route evidence remains runtime
// state and therefore never rebinds a running world.
type cityWorldVersionV9ContentCatalog struct {
	cityWorldVersionV8ContentCatalog
	MobilityProfileID      string                                   `json:"mobility_profile_id"`
	MobilityProfileVersion string                                   `json:"mobility_profile_version"`
	MobilityProfileHash    string                                   `json:"mobility_profile_hash"`
	Modes                  []cityWorldVersionMobilityModeDescriptor `json:"modes"`
	Hubs                   []cityWorldVersionMobilityHubDescriptor  `json:"hubs"`
	Edges                  []cityWorldVersionMobilityEdgeDescriptor `json:"edges"`
}

// cityWorldVersionV10ContentCatalog keeps the bridge policy with its V9
// topology descriptor. Dynamic arrivals are runtime evidence and therefore
// deliberately do not alter an immutable world-version vector.
type cityWorldVersionV10ContentCatalog struct {
	cityWorldVersionV9ContentCatalog
	ArrivalProfileID      string `json:"arrival_profile_id"`
	ArrivalProfileVersion string `json:"arrival_profile_version"`
	ArrivalProfileHash    string `json:"arrival_profile_hash"`
}

// cityWorldVersionV11ContentCatalog extends the sealed V10 arrival policy
// with the static automatic-OD adapter policy. Source lifecycle and closed
// metrics remain runtime evidence and are intentionally excluded.
type cityWorldVersionV11ContentCatalog struct {
	cityWorldVersionV10ContentCatalog
	ODProfileID      string `json:"od_profile_id"`
	ODProfileVersion string `json:"od_profile_version"`
	ODProfileHash    string `json:"od_profile_hash"`
}

// cityWorldVersionV12ContentCatalog extends the sealed V11 OD policy with
// the immutable residence/employment binding policy. Individual bindings are
// world evidence, not a deploy-time content catalog, so they remain outside
// this vector and are protected by the canonical snapshot instead.
type cityWorldVersionV12ContentCatalog struct {
	cityWorldVersionV11ContentCatalog
	CommuteProfileID      string `json:"commute_profile_id"`
	CommuteProfileVersion string `json:"commute_profile_version"`
	CommuteProfileHash    string `json:"commute_profile_hash"`
}

// cityWorldVersionV13ContentCatalog adds only the sealed source policy. The
// per-actor source rows and their transitions remain canonical runtime
// evidence and therefore stay outside the immutable version vector.
type cityWorldVersionV13ContentCatalog struct {
	cityWorldVersionV12ContentCatalog
	CommuteSourceProfileID      string `json:"commute_source_profile_id"`
	CommuteSourceProfileVersion string `json:"commute_source_profile_version"`
	CommuteSourceProfileHash    string `json:"commute_source_profile_hash"`
}

// cityWorldVersionV14ContentCatalog pins the static lifecycle policy while
// keeping every assignment epoch, transition, source counter and cycle metric
// in canonical runtime evidence instead of the deploy-time vector.
type cityWorldVersionV14ContentCatalog struct {
	cityWorldVersionV13ContentCatalog
	CommuteLifecycleProfileID      string `json:"commute_lifecycle_profile_id"`
	CommuteLifecycleProfileVersion string `json:"commute_lifecycle_profile_version"`
	CommuteLifecycleProfileHash    string `json:"commute_lifecycle_profile_hash"`
}

// cityWorldVersionV15ContentCatalog pins the F10.0 supply-chain policy and
// contracts. Orders, reservations, deliveries and settlements are append-only
// world evidence, so they remain exclusively in the canonical runtime state.
type cityWorldVersionV15ContentCatalog struct {
	cityWorldVersionV14ContentCatalog
	SupplyChainProfileID          string `json:"supply_chain_profile_id"`
	SupplyChainProfileVersion     string `json:"supply_chain_profile_version"`
	SupplyChainProfileHash        string `json:"supply_chain_profile_hash"`
	SupplyChainNodeContract       string `json:"supply_chain_node_contract"`
	SupplyChainOrderContract      string `json:"supply_chain_order_contract"`
	SupplyChainSettlementContract string `json:"supply_chain_settlement_contract"`
	SupplyChainDeliveryContract   string `json:"supply_chain_delivery_contract"`
}

// cityWorldVersionV16ContentCatalog extends the frozen V15 content contract
// with the static, deliberately non-settling enterprise-freight adapter.
// Source rows, V9 demands and V15 order state remain runtime evidence and are
// therefore excluded from this immutable bundle.
type cityWorldVersionV16ContentCatalog struct {
	cityWorldVersionV15ContentCatalog
	EnterpriseFreightProfileID          string `json:"enterprise_freight_profile_id"`
	EnterpriseFreightProfileVersion     string `json:"enterprise_freight_profile_version"`
	EnterpriseFreightProfileHash        string `json:"enterprise_freight_profile_hash"`
	EnterpriseFreightSourceContract     string `json:"enterprise_freight_source_contract"`
	EnterpriseFreightDemandContract     string `json:"enterprise_freight_demand_contract"`
	EnterpriseFreightCompletionContract string `json:"enterprise_freight_completion_contract"`
	EnterpriseFreightTerminalContract   string `json:"enterprise_freight_terminal_contract"`
	EnterpriseFreightCarrierActorCode   string `json:"enterprise_freight_carrier_actor_code"`
}

// cityWorldVersionV17ContentCatalog freezes the V17 custody and receipt-gate
// contract without serializing mutable shipments, facts, or receipts. Those
// remain canonical world evidence and are independently replayed.
type cityWorldVersionV17ContentCatalog struct {
	cityWorldVersionV16ContentCatalog
	EnterpriseFreightReceiptProfileID      string `json:"enterprise_freight_receipt_profile_id"`
	EnterpriseFreightReceiptProfileVersion string `json:"enterprise_freight_receipt_profile_version"`
	EnterpriseFreightReceiptProfileHash    string `json:"enterprise_freight_receipt_profile_hash"`
	EnterpriseFreightShipmentContract      string `json:"enterprise_freight_shipment_contract"`
	EnterpriseFreightReceiptContract       string `json:"enterprise_freight_receipt_contract"`
	EnterpriseFreightLegacyContract        string `json:"enterprise_freight_legacy_contract"`
}

// cityWorldVersionV18ContentCatalog seals the bounded-overflow transport
// adapter. Mutable plans, consignments, transport evidence, and receipts stay
// in the canonical runtime projection; this bundle contains only the policy
// that determines their admissible shape.
type cityWorldVersionV18ContentCatalog struct {
	cityWorldVersionV17ContentCatalog
	FreightBatchProfileID                  string `json:"freight_batch_profile_id"`
	FreightBatchProfileVersion             string `json:"freight_batch_profile_version"`
	FreightBatchProfileHash                string `json:"freight_batch_profile_hash"`
	FreightBatchSourceContract             string `json:"freight_batch_source_contract"`
	FreightBatchPackingContract            string `json:"freight_batch_packing_contract"`
	FreightBatchTransportContract          string `json:"freight_batch_transport_contract"`
	FreightBatchReceiptContract            string `json:"freight_batch_receipt_contract"`
	FreightBatchMaximumUnits               int64  `json:"freight_batch_maximum_units"`
	FreightBatchMaximumConsignmentsPerPlan int    `json:"freight_batch_maximum_consignments_per_plan"`
	FreightBatchMaximumPlansPerTick        int    `json:"freight_batch_maximum_plans_per_tick"`
	FreightBatchMaximumObservationsPerTick int    `json:"freight_batch_maximum_observations_per_tick"`
}

// cityWorldVersionV19ContentCatalog seals the static F9.3.0 policy and its
// profile-selected transport vocabulary. The per-world node/corridor mapping
// remains canonical world state and is validated against this policy instead
// of being silently regenerated from current server content.
type cityWorldVersionV19ContentCatalog struct {
	cityWorldVersionV18ContentCatalog
	SpatialNetworkProfileID                    string `json:"spatial_network_profile_id"`
	SpatialNetworkProfileVersion               string `json:"spatial_network_profile_version"`
	SpatialNetworkProfileHash                  string `json:"spatial_network_profile_hash"`
	SpatialNetworkTopologyContract             string `json:"spatial_network_topology_contract"`
	SpatialNetworkStyleContract                string `json:"spatial_network_style_contract"`
	SpatialNetworkTransportStyleID             string `json:"spatial_network_transport_style_id"`
	SpatialNetworkTransportStyleVersion        string `json:"spatial_network_transport_style_version"`
	SpatialNetworkTransportStyleHash           string `json:"spatial_network_transport_style_hash"`
	SpatialNetworkSourceWorldgenProfileID      string `json:"spatial_network_source_worldgen_profile_id"`
	SpatialNetworkSourceWorldgenProfileVersion string `json:"spatial_network_source_worldgen_profile_version"`
	SpatialNetworkSourceWorldgenProfileHash    string `json:"spatial_network_source_worldgen_profile_hash"`
	SpatialNetworkMaximumNodes                 int    `json:"spatial_network_maximum_nodes"`
	SpatialNetworkMaximumCorridors             int    `json:"spatial_network_maximum_corridors"`
	SpatialNetworkNodeCount                    int64  `json:"spatial_network_node_count"`
	SpatialNetworkCorridorCount                int64  `json:"spatial_network_corridor_count"`
}

// cityWorldVersionV20ContentCatalog seals only V20's immutable
// infrastructure protocol and the static V19-derived asset inventory. Asset
// states, transition history, revisions, and capacities are live canonical
// world state, so they deliberately do not make a version vector drift after
// an owner records a maintenance or closure fact.
type cityWorldVersionV20ContentCatalog struct {
	cityWorldVersionV19ContentCatalog
	InfrastructureProfileID      string `json:"infrastructure_profile_id"`
	InfrastructureProfileVersion string `json:"infrastructure_profile_version"`
	InfrastructureProfileHash    string `json:"infrastructure_profile_hash"`
	InfrastructureAssetContract  string `json:"infrastructure_asset_contract"`
	InfrastructureStateContract  string `json:"infrastructure_state_contract"`
	InfrastructureMaximumAssets  int    `json:"infrastructure_maximum_assets"`
	InfrastructureAssetCount     int64  `json:"infrastructure_asset_count"`
	InfrastructureNodeAssetCount int64  `json:"infrastructure_node_asset_count"`
	InfrastructureSegmentCount   int64  `json:"infrastructure_segment_asset_count"`
}

// cityWorldVersionV21ContentCatalog seals V21's admission policy only.
// Admission rows, occupancy, congestion, and infrastructure transitions are
// live fact-backed evidence and must never cause a world-version vector to
// drift during ordinary simulation.
type cityWorldVersionV21ContentCatalog struct {
	cityWorldVersionV20ContentCatalog
	EffectiveCapacityProfileID          string `json:"effective_capacity_profile_id"`
	EffectiveCapacityProfileVersion     string `json:"effective_capacity_profile_version"`
	EffectiveCapacityProfileHash        string `json:"effective_capacity_profile_hash"`
	EffectiveCapacityTopologyContract   string `json:"effective_capacity_topology_contract"`
	EffectiveCapacityAssetContract      string `json:"effective_capacity_asset_contract"`
	EffectiveCapacityAdmissionContract  string `json:"effective_capacity_admission_contract"`
	EffectiveCapacityVisibilityContract string `json:"effective_capacity_visibility_contract"`
	EffectiveCapacityMaximumAdmissions  int    `json:"effective_capacity_maximum_admissions"`
}

// cityWorldVersionV22ContentCatalog freezes V22's settlement protocol and
// quantitative safeguards only. Orders, cases, receipts, refunds, claims and
// all resolved quantities are live canonical evidence, not version inputs.
type cityWorldVersionV22ContentCatalog struct {
	cityWorldVersionV21ContentCatalog
	FreightSettlementProfileID              string `json:"freight_settlement_profile_id"`
	FreightSettlementProfileVersion         string `json:"freight_settlement_profile_version"`
	FreightSettlementProfileHash            string `json:"freight_settlement_profile_hash"`
	FreightSettlementSourceContract         string `json:"freight_settlement_source_contract"`
	FreightSettlementReceiptContract        string `json:"freight_settlement_receipt_contract"`
	FreightSettlementResourceContract       string `json:"freight_settlement_resource_contract"`
	FreightSettlementFinancialContract      string `json:"freight_settlement_financial_contract"`
	FreightSettlementLiabilityContract      string `json:"freight_settlement_liability_contract"`
	FreightSettlementMaximumOrders          int    `json:"freight_settlement_maximum_orders"`
	FreightSettlementMaximumCasesPerOrder   int    `json:"freight_settlement_maximum_cases_per_order"`
	FreightSettlementMaximumReceiptsPerCase int    `json:"freight_settlement_maximum_receipts_per_case"`
	FreightSettlementMaximumReceiptsPerTick int    `json:"freight_settlement_maximum_receipts_per_tick"`
}

// cityWorldVersionV23ContentCatalog adds the sealed manual carrier-reserve
// policy. Funding and claim recovery rows remain canonical runtime evidence,
// so only the immutable profile contract appears in the version vector.
type cityWorldVersionV23ContentCatalog struct {
	cityWorldVersionV22ContentCatalog
	CarrierRecoveryProfileID              string `json:"carrier_recovery_profile_id"`
	CarrierRecoveryProfileVersion         string `json:"carrier_recovery_profile_version"`
	CarrierRecoveryProfileHash            string `json:"carrier_recovery_profile_hash"`
	CarrierRecoveryActorCode              string `json:"carrier_recovery_actor_code"`
	CarrierRecoveryFirmCode               string `json:"carrier_recovery_firm_code"`
	CarrierRecoveryFundingContract        string `json:"carrier_recovery_funding_contract"`
	CarrierRecoveryRecoveryContract       string `json:"carrier_recovery_recovery_contract"`
	CarrierRecoveryReservePolicy          string `json:"carrier_recovery_reserve_policy"`
	CarrierRecoveryMaximumFundingsPerTick int    `json:"carrier_recovery_maximum_fundings_per_tick"`
	CarrierRecoveryMaximumRecoveriesTick  int    `json:"carrier_recovery_maximum_recoveries_per_tick"`
	CarrierRecoveryMaximumAmountUnits     int64  `json:"carrier_recovery_maximum_amount_units"`
}

// cityWorldVersionV24ContentCatalog seals only the immutable fee contract.
// Individual V22 cases, V24 quotes, affordability decisions and journal rows
// remain runtime evidence, so they never make a version vector drift.
type cityWorldVersionV24ContentCatalog struct {
	cityWorldVersionV23ContentCatalog
	CarrierCommerceProfileID        string `json:"carrier_commerce_profile_id"`
	CarrierCommerceProfileVersion   string `json:"carrier_commerce_profile_version"`
	CarrierCommerceProfileHash      string `json:"carrier_commerce_profile_hash"`
	CarrierCommerceActorCode        string `json:"carrier_commerce_actor_code"`
	CarrierCommerceFirmCode         string `json:"carrier_commerce_firm_code"`
	CarrierCommerceServiceContract  string `json:"carrier_commerce_service_contract"`
	CarrierCommercePaymentContract  string `json:"carrier_commerce_payment_contract"`
	CarrierCommerceFeePerCargoUnit  int64  `json:"carrier_commerce_fee_per_cargo_unit"`
	CarrierCommerceMaximumContracts int    `json:"carrier_commerce_maximum_contracts_per_tick"`
	CarrierCommerceMaximumPayments  int    `json:"carrier_commerce_maximum_payments_per_tick"`
}

func initializeCityWorldVersionVector(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if worldID <= 0 {
		return ErrCityInvalidInput
	}
	var version string
	var generation int
	var baselineTick int64
	if err := tx.QueryRowContext(ctx, `
SELECT simulation_version, version_vector_generation, current_tick
FROM city_worlds WHERE id = $1 FOR UPDATE`, worldID).Scan(
		&version, &generation, &baselineTick,
	); err != nil {
		return fmt.Errorf("load city version-vector world: %w", err)
	}
	if !cityEngineSupportsWorldVersionVector(version) {
		return ErrCitySimulationVersion.WithMetadata(map[string]string{"version": version})
	}
	if generation == 0 {
		generation = 1
		if _, err := tx.ExecContext(ctx, `
UPDATE city_worlds
SET version_vector_generation = $2, updated_at = NOW()
WHERE id = $1`, worldID, generation); err != nil {
			return fmt.Errorf("set city version-vector generation: %w", err)
		}
	}
	if generation < 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "world_version_vector_generation"})
	}
	if err := activateCityWorldVersionVectorWrite(ctx, tx, worldID, generation); err != nil {
		return err
	}
	vector, err := deriveCityWorldVersionVector(ctx, tx, worldID, version)
	if err != nil {
		return err
	}
	vector.Generation = generation
	vector.BaselineTick = baselineTick
	if err := validateCityWorldVersionVector(vector); err != nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "world_version_vector"}).WithCause(err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_world_version_vectors
    (world_id, generation, engine_version, baseline_tick, source_upgrade_run_id)
VALUES (
    $1, $2, $3, $4,
    NULLIF(current_setting('sub2api.city_upgrade_run_id', TRUE), '')::BIGINT
)`, worldID, generation, version, baselineTick); err != nil {
		return fmt.Errorf("insert city version-vector header: %w", err)
	}
	for _, binding := range vector.Bindings {
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_world_version_bindings
	    (world_id, generation, component_code, bundle_id, bundle_version, content_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)`,
			worldID, generation, binding.ComponentCode, binding.BundleID, binding.BundleVersion,
			binding.ContentHash, []byte(binding.Metadata)); err != nil {
			return fmt.Errorf("insert city version-vector component %s: %w", binding.ComponentCode, err)
		}
	}
	return nil
}

func activateCityWorldVersionVectorWrite(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	generation int,
) error {
	if _, err := tx.ExecContext(ctx, `
SELECT set_config('sub2api.city_world_version_binding_world_id', $1, TRUE)`, fmt.Sprintf("%d", worldID)); err != nil {
		return fmt.Errorf("enable city version-vector write gate: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
SELECT set_config('sub2api.city_world_version_binding_generation', $1, TRUE)`, fmt.Sprintf("%d", generation)); err != nil {
		return fmt.Errorf("enable city version-vector generation gate: %w", err)
	}
	return nil
}

func deriveCityWorldVersionVector(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	simulationVersion string,
) (CityWorldVersionVector, error) {
	if !cityEngineSupportsWorldVersionVector(simulationVersion) {
		return CityWorldVersionVector{}, ErrCitySimulationVersion.WithMetadata(map[string]string{"version": simulationVersion})
	}
	binding, err := loadCityOpenWorldBinding(ctx, queryer, worldID)
	if err != nil {
		return CityWorldVersionVector{}, err
	}
	var scenarioID, scenarioVersion, scenarioHash string
	if err = queryer.QueryRowContext(ctx, `
SELECT scenario_id, scenario_version, scenario_hash
FROM city_open_world_scenario_bindings WHERE world_id = $1`, worldID).Scan(
		&scenarioID, &scenarioVersion, &scenarioHash,
	); err != nil {
		return CityWorldVersionVector{}, fmt.Errorf("load city version-vector scenario: %w", err)
	}
	var policy cityWorldVersionEconomicPolicy
	policy.SchemaVersion = cityWorldVersionVectorSchemaVersion
	if err = queryer.QueryRowContext(ctx, `
SELECT labor_demand_capacity_milli, goods_demand_population_divisor,
       household_wage_tax_milli, firm_sales_tax_milli,
       procurement_share_milli, social_support_share_milli, version
FROM city_economic_policies WHERE world_id = $1`, worldID).Scan(
		&policy.LaborDemandCapacityMilli, &policy.GoodsDemandPopulationDivisor,
		&policy.HouseholdWageTaxMilli, &policy.FirmSalesTaxMilli,
		&policy.ProcurementShareMilli, &policy.SocialSupportShareMilli,
		&policy.ProjectionVersion,
	); err != nil {
		return CityWorldVersionVector{}, fmt.Errorf("load city version-vector economic policy: %w", err)
	}
	policyHash, err := cityWorldVersionBundleHash(policy)
	if err != nil {
		return CityWorldVersionVector{}, err
	}
	var runtimeID, catalogVersion, catalogHash string
	if err = queryer.QueryRowContext(ctx, `
SELECT runtime_id, catalog_version, catalog_hash
FROM city_open_world_runtime_profiles WHERE world_id = $1`, worldID).Scan(
		&runtimeID, &catalogVersion, &catalogHash,
	); err != nil {
		return CityWorldVersionVector{}, fmt.Errorf("load city version-vector content catalog: %w", err)
	}
	contentCatalogBundleID, contentCatalogBundleVersion, contentCatalogHash := runtimeID, catalogVersion, catalogHash
	serviceProfileHash, serviceProfileVersion := "", ""
	impactProfileHash, impactProfileVersion := "", ""
	if cityEngineSupportsOpenWorldGeneration(simulationVersion, 7) {
		serviceCatalog, serviceErr := loadCityWorldVersionV7ServiceCatalog(ctx, queryer, worldID)
		if serviceErr != nil {
			return CityWorldVersionVector{}, serviceErr
		}
		serviceProfileHash = serviceCatalog.ServiceProfileHash
		serviceProfileVersion = serviceCatalog.ServiceProfileVersion
		contentCatalogBundleID = cityWorldVersionContentCatalogV7BundleID
		contentCatalogBundleVersion = cityWorldVersionContentCatalogV7Version
		contentCatalogHash, serviceErr = cityWorldVersionBundleHash(serviceCatalog)
		if serviceErr != nil {
			return CityWorldVersionVector{}, serviceErr
		}
	}
	if cityEngineSupportsOpenWorldGeneration(simulationVersion, 8) {
		impactCatalog, impactErr := loadCityWorldVersionV8ImpactCatalog(ctx, queryer, worldID)
		if impactErr != nil {
			return CityWorldVersionVector{}, impactErr
		}
		impactProfileHash = impactCatalog.ImpactProfileHash
		impactProfileVersion = impactCatalog.ImpactProfileVersion
		contentCatalogBundleID = cityWorldVersionContentCatalogV8BundleID
		contentCatalogBundleVersion = cityWorldVersionContentCatalogV8Version
		contentCatalogHash, impactErr = cityWorldVersionBundleHash(impactCatalog)
		if impactErr != nil {
			return CityWorldVersionVector{}, impactErr
		}
	}
	rules, ruleVersion, err := loadCityWorldVersionRuleBundle(ctx, queryer, worldID)
	if err != nil {
		return CityWorldVersionVector{}, err
	}
	ruleHash, err := cityWorldVersionBundleHash(rules)
	if err != nil {
		return CityWorldVersionVector{}, err
	}
	engineHash, err := cityWorldVersionBundleHash(struct {
		EngineVersion       string `json:"engine_version"`
		CanonicalFormat     string `json:"canonical_format"`
		VectorSchemaVersion int    `json:"vector_schema_version"`
	}{
		EngineVersion: simulationVersion, CanonicalFormat: citySnapshotFormat,
		VectorSchemaVersion: cityWorldVersionVectorSchemaVersion,
	})
	if err != nil {
		return CityWorldVersionVector{}, err
	}
	policyVersion := policy.ProjectionVersion
	mobilityProfileHash, mobilityProfileVersion := "", ""
	if cityEngineSupportsOpenWorldGeneration(simulationVersion, 9) {
		mobilityCatalog, mobilityErr := loadCityWorldVersionV9MobilityCatalog(ctx, queryer, worldID)
		if mobilityErr != nil {
			return CityWorldVersionVector{}, mobilityErr
		}
		mobilityProfileHash = mobilityCatalog.MobilityProfileHash
		mobilityProfileVersion = mobilityCatalog.MobilityProfileVersion
		contentCatalogBundleID = cityWorldVersionContentCatalogV9BundleID
		contentCatalogBundleVersion = cityWorldVersionContentCatalogV9Version
		contentCatalogHash, mobilityErr = cityWorldVersionBundleHash(mobilityCatalog)
		if mobilityErr != nil {
			return CityWorldVersionVector{}, mobilityErr
		}
	}
	arrivalProfileHash, arrivalProfileVersion := "", ""
	if cityEngineSupportsOpenWorldGeneration(simulationVersion, 10) {
		arrivalCatalog, arrivalErr := loadCityWorldVersionV10ArrivalCatalog(ctx, queryer, worldID)
		if arrivalErr != nil {
			return CityWorldVersionVector{}, arrivalErr
		}
		arrivalProfileHash = arrivalCatalog.ArrivalProfileHash
		arrivalProfileVersion = arrivalCatalog.ArrivalProfileVersion
		contentCatalogBundleID = cityWorldVersionContentCatalogV10BundleID
		contentCatalogBundleVersion = cityWorldVersionContentCatalogV10Version
		contentCatalogHash, arrivalErr = cityWorldVersionBundleHash(arrivalCatalog)
		if arrivalErr != nil {
			return CityWorldVersionVector{}, arrivalErr
		}
	}
	odProfileHash, odProfileVersion := "", ""
	if cityEngineSupportsOpenWorldGeneration(simulationVersion, 11) {
		odCatalog, odErr := loadCityWorldVersionV11ODCatalog(ctx, queryer, worldID)
		if odErr != nil {
			return CityWorldVersionVector{}, odErr
		}
		odProfileHash = odCatalog.ODProfileHash
		odProfileVersion = odCatalog.ODProfileVersion
		contentCatalogBundleID = cityWorldVersionContentCatalogV11BundleID
		contentCatalogBundleVersion = cityWorldVersionContentCatalogV11Version
		contentCatalogHash, odErr = cityWorldVersionBundleHash(odCatalog)
		if odErr != nil {
			return CityWorldVersionVector{}, odErr
		}
	}
	commuteProfileHash, commuteProfileVersion := "", ""
	if cityEngineSupportsOpenWorldGeneration(simulationVersion, 12) {
		commuteCatalog, commuteErr := loadCityWorldVersionV12CommuteCatalog(ctx, queryer, worldID)
		if commuteErr != nil {
			return CityWorldVersionVector{}, commuteErr
		}
		commuteProfileHash = commuteCatalog.CommuteProfileHash
		commuteProfileVersion = commuteCatalog.CommuteProfileVersion
		contentCatalogBundleID = cityWorldVersionContentCatalogV12BundleID
		contentCatalogBundleVersion = cityWorldVersionContentCatalogV12Version
		contentCatalogHash, commuteErr = cityWorldVersionBundleHash(commuteCatalog)
		if commuteErr != nil {
			return CityWorldVersionVector{}, commuteErr
		}
	}
	commuteSourceProfileHash, commuteSourceProfileVersion := "", ""
	if cityEngineSupportsOpenWorldGeneration(simulationVersion, 13) {
		commuteSourceCatalog, sourceErr := loadCityWorldVersionV13CommuteSourceCatalog(ctx, queryer, worldID)
		if sourceErr != nil {
			return CityWorldVersionVector{}, sourceErr
		}
		commuteSourceProfileHash = commuteSourceCatalog.CommuteSourceProfileHash
		commuteSourceProfileVersion = commuteSourceCatalog.CommuteSourceProfileVersion
		contentCatalogBundleID = cityWorldVersionContentCatalogV13BundleID
		contentCatalogBundleVersion = cityWorldVersionContentCatalogV13Version
		contentCatalogHash, sourceErr = cityWorldVersionBundleHash(commuteSourceCatalog)
		if sourceErr != nil {
			return CityWorldVersionVector{}, sourceErr
		}
	}
	commuteLifecycleProfileHash, commuteLifecycleProfileVersion := "", ""
	if cityEngineSupportsOpenWorldGeneration(simulationVersion, 14) {
		lifecycleCatalog, lifecycleErr := loadCityWorldVersionV14CommuteLifecycleCatalog(ctx, queryer, worldID)
		if lifecycleErr != nil {
			return CityWorldVersionVector{}, lifecycleErr
		}
		commuteLifecycleProfileHash = lifecycleCatalog.CommuteLifecycleProfileHash
		commuteLifecycleProfileVersion = lifecycleCatalog.CommuteLifecycleProfileVersion
		contentCatalogBundleID = cityWorldVersionContentCatalogV14BundleID
		contentCatalogBundleVersion = cityWorldVersionContentCatalogV14Version
		contentCatalogHash, lifecycleErr = cityWorldVersionBundleHash(lifecycleCatalog)
		if lifecycleErr != nil {
			return CityWorldVersionVector{}, lifecycleErr
		}
	}
	supplyChainProfileHash, supplyChainProfileVersion := "", ""
	supplyChainNodeContract, supplyChainOrderContract := "", ""
	supplyChainSettlementContract, supplyChainDeliveryContract := "", ""
	if cityEngineSupportsOpenWorldGeneration(simulationVersion, 15) {
		supplyChainCatalog, supplyChainErr := loadCityWorldVersionV15SupplyChainCatalog(ctx, queryer, worldID)
		if supplyChainErr != nil {
			return CityWorldVersionVector{}, supplyChainErr
		}
		supplyChainProfileHash = supplyChainCatalog.SupplyChainProfileHash
		supplyChainProfileVersion = supplyChainCatalog.SupplyChainProfileVersion
		supplyChainNodeContract = supplyChainCatalog.SupplyChainNodeContract
		supplyChainOrderContract = supplyChainCatalog.SupplyChainOrderContract
		supplyChainSettlementContract = supplyChainCatalog.SupplyChainSettlementContract
		supplyChainDeliveryContract = supplyChainCatalog.SupplyChainDeliveryContract
		contentCatalogBundleID = cityWorldVersionContentCatalogV15BundleID
		contentCatalogBundleVersion = cityWorldVersionContentCatalogV15Version
		contentCatalogHash, supplyChainErr = cityWorldVersionBundleHash(supplyChainCatalog)
		if supplyChainErr != nil {
			return CityWorldVersionVector{}, supplyChainErr
		}
	}
	freightProfileHash, freightProfileVersion := "", ""
	freightSourceContract, freightDemandContract := "", ""
	freightCompletionContract, freightTerminalContract, freightCarrierActorCode := "", "", ""
	if cityEngineSupportsOpenWorldGeneration(simulationVersion, 16) {
		freightCatalog, freightErr := loadCityWorldVersionV16EnterpriseFreightCatalog(ctx, queryer, worldID)
		if freightErr != nil {
			return CityWorldVersionVector{}, freightErr
		}
		freightProfileHash = freightCatalog.EnterpriseFreightProfileHash
		freightProfileVersion = freightCatalog.EnterpriseFreightProfileVersion
		freightSourceContract = freightCatalog.EnterpriseFreightSourceContract
		freightDemandContract = freightCatalog.EnterpriseFreightDemandContract
		freightCompletionContract = freightCatalog.EnterpriseFreightCompletionContract
		freightTerminalContract = freightCatalog.EnterpriseFreightTerminalContract
		freightCarrierActorCode = freightCatalog.EnterpriseFreightCarrierActorCode
		contentCatalogBundleID = cityWorldVersionContentCatalogV16BundleID
		contentCatalogBundleVersion = cityWorldVersionContentCatalogV16Version
		contentCatalogHash, freightErr = cityWorldVersionBundleHash(freightCatalog)
		if freightErr != nil {
			return CityWorldVersionVector{}, freightErr
		}
	}
	receiptProfileHash, receiptProfileVersion := "", ""
	freightShipmentContract, freightReceiptContract, freightLegacyContract := "", "", ""
	if cityEngineSupportsOpenWorldGeneration(simulationVersion, 17) {
		receiptCatalog, receiptErr := loadCityWorldVersionV17EnterpriseFreightReceiptCatalog(ctx, queryer, worldID)
		if receiptErr != nil {
			return CityWorldVersionVector{}, receiptErr
		}
		receiptProfileHash = receiptCatalog.EnterpriseFreightReceiptProfileHash
		receiptProfileVersion = receiptCatalog.EnterpriseFreightReceiptProfileVersion
		freightShipmentContract = receiptCatalog.EnterpriseFreightShipmentContract
		freightReceiptContract = receiptCatalog.EnterpriseFreightReceiptContract
		freightLegacyContract = receiptCatalog.EnterpriseFreightLegacyContract
		contentCatalogBundleID = cityWorldVersionContentCatalogV17BundleID
		contentCatalogBundleVersion = cityWorldVersionContentCatalogV17Version
		contentCatalogHash, receiptErr = cityWorldVersionBundleHash(receiptCatalog)
		if receiptErr != nil {
			return CityWorldVersionVector{}, receiptErr
		}
	}
	freightBatchProfileHash, freightBatchProfileVersion := "", ""
	freightBatchSourceContract, freightBatchPackingContract := "", ""
	freightBatchTransportContract, freightBatchReceiptContract := "", ""
	var freightBatchMaximumUnits int64
	freightBatchMaximumConsignmentsPerPlan := 0
	freightBatchMaximumPlansPerTick := 0
	freightBatchMaximumObservationsPerTick := 0
	if cityEngineSupportsOpenWorldGeneration(simulationVersion, 18) {
		freightBatchCatalog, freightBatchErr := loadCityWorldVersionV18FreightBatchCatalog(ctx, queryer, worldID)
		if freightBatchErr != nil {
			return CityWorldVersionVector{}, freightBatchErr
		}
		freightBatchProfileHash = freightBatchCatalog.FreightBatchProfileHash
		freightBatchProfileVersion = freightBatchCatalog.FreightBatchProfileVersion
		freightBatchSourceContract = freightBatchCatalog.FreightBatchSourceContract
		freightBatchPackingContract = freightBatchCatalog.FreightBatchPackingContract
		freightBatchTransportContract = freightBatchCatalog.FreightBatchTransportContract
		freightBatchReceiptContract = freightBatchCatalog.FreightBatchReceiptContract
		freightBatchMaximumUnits = freightBatchCatalog.FreightBatchMaximumUnits
		freightBatchMaximumConsignmentsPerPlan = freightBatchCatalog.FreightBatchMaximumConsignmentsPerPlan
		freightBatchMaximumPlansPerTick = freightBatchCatalog.FreightBatchMaximumPlansPerTick
		freightBatchMaximumObservationsPerTick = freightBatchCatalog.FreightBatchMaximumObservationsPerTick
		contentCatalogBundleID = cityWorldVersionContentCatalogV18BundleID
		contentCatalogBundleVersion = cityWorldVersionContentCatalogV18Version
		contentCatalogHash, freightBatchErr = cityWorldVersionBundleHash(freightBatchCatalog)
		if freightBatchErr != nil {
			return CityWorldVersionVector{}, freightBatchErr
		}
	}
	spatialNetworkPolicy := CityOpenWorldSpatialNetworkPolicy{}
	if cityEngineSupportsOpenWorldSpatialNetwork(simulationVersion) {
		spatialNetworkCatalog, spatialNetworkErr := loadCityWorldVersionV19SpatialNetworkCatalog(ctx, queryer, worldID)
		if spatialNetworkErr != nil {
			return CityWorldVersionVector{}, spatialNetworkErr
		}
		spatialNetworkPolicy = CityOpenWorldSpatialNetworkPolicy{
			ProfileID:                    spatialNetworkCatalog.SpatialNetworkProfileID,
			ProfileVersion:               spatialNetworkCatalog.SpatialNetworkProfileVersion,
			ContentHash:                  spatialNetworkCatalog.SpatialNetworkProfileHash,
			TopologyContract:             spatialNetworkCatalog.SpatialNetworkTopologyContract,
			StyleContract:                spatialNetworkCatalog.SpatialNetworkStyleContract,
			TransportStyleID:             spatialNetworkCatalog.SpatialNetworkTransportStyleID,
			TransportStyleVersion:        spatialNetworkCatalog.SpatialNetworkTransportStyleVersion,
			TransportStyleHash:           spatialNetworkCatalog.SpatialNetworkTransportStyleHash,
			SourceWorldgenProfileID:      spatialNetworkCatalog.SpatialNetworkSourceWorldgenProfileID,
			SourceWorldgenProfileVersion: spatialNetworkCatalog.SpatialNetworkSourceWorldgenProfileVersion,
			SourceWorldgenProfileHash:    spatialNetworkCatalog.SpatialNetworkSourceWorldgenProfileHash,
			MaximumNodes:                 spatialNetworkCatalog.SpatialNetworkMaximumNodes,
			MaximumCorridors:             spatialNetworkCatalog.SpatialNetworkMaximumCorridors,
			NodeCount:                    spatialNetworkCatalog.SpatialNetworkNodeCount,
			CorridorCount:                spatialNetworkCatalog.SpatialNetworkCorridorCount,
		}
		contentCatalogBundleID = cityWorldVersionContentCatalogV19BundleID
		contentCatalogBundleVersion = cityWorldVersionContentCatalogV19Version
		contentCatalogHash, spatialNetworkErr = cityWorldVersionBundleHash(spatialNetworkCatalog)
		if spatialNetworkErr != nil {
			return CityWorldVersionVector{}, spatialNetworkErr
		}
	}
	infrastructurePolicy := CityOpenWorldInfrastructurePolicy{}
	if cityEngineSupportsOpenWorldInfrastructure(simulationVersion) {
		infrastructureCatalog, infrastructureErr := loadCityWorldVersionV20InfrastructureCatalog(ctx, queryer, worldID)
		if infrastructureErr != nil {
			return CityWorldVersionVector{}, infrastructureErr
		}
		infrastructurePolicy = CityOpenWorldInfrastructurePolicy{
			ProfileID:         infrastructureCatalog.InfrastructureProfileID,
			ProfileVersion:    infrastructureCatalog.InfrastructureProfileVersion,
			ContentHash:       infrastructureCatalog.InfrastructureProfileHash,
			AssetContract:     infrastructureCatalog.InfrastructureAssetContract,
			StateContract:     infrastructureCatalog.InfrastructureStateContract,
			MaximumAssets:     infrastructureCatalog.InfrastructureMaximumAssets,
			AssetCount:        infrastructureCatalog.InfrastructureAssetCount,
			NodeAssetCount:    infrastructureCatalog.InfrastructureNodeAssetCount,
			SegmentAssetCount: infrastructureCatalog.InfrastructureSegmentCount,
		}
		contentCatalogBundleID = cityWorldVersionContentCatalogV20BundleID
		contentCatalogBundleVersion = cityWorldVersionContentCatalogV20Version
		contentCatalogHash, infrastructureErr = cityWorldVersionBundleHash(infrastructureCatalog)
		if infrastructureErr != nil {
			return CityWorldVersionVector{}, infrastructureErr
		}
	}
	effectiveCapacityPolicy := CityOpenWorldEffectiveCapacityPolicy{}
	if cityEngineSupportsOpenWorldEffectiveCapacity(simulationVersion) {
		effectiveCapacityCatalog, effectiveCapacityErr := loadCityWorldVersionV21EffectiveCapacityCatalog(ctx, queryer, worldID)
		if effectiveCapacityErr != nil {
			return CityWorldVersionVector{}, effectiveCapacityErr
		}
		effectiveCapacityPolicy = CityOpenWorldEffectiveCapacityPolicy{
			ProfileID:          effectiveCapacityCatalog.EffectiveCapacityProfileID,
			ProfileVersion:     effectiveCapacityCatalog.EffectiveCapacityProfileVersion,
			ContentHash:        effectiveCapacityCatalog.EffectiveCapacityProfileHash,
			TopologyContract:   effectiveCapacityCatalog.EffectiveCapacityTopologyContract,
			AssetContract:      effectiveCapacityCatalog.EffectiveCapacityAssetContract,
			AdmissionContract:  effectiveCapacityCatalog.EffectiveCapacityAdmissionContract,
			VisibilityContract: effectiveCapacityCatalog.EffectiveCapacityVisibilityContract,
			MaximumAdmissions:  effectiveCapacityCatalog.EffectiveCapacityMaximumAdmissions,
		}
		contentCatalogBundleID = cityWorldVersionContentCatalogV21BundleID
		contentCatalogBundleVersion = cityWorldVersionContentCatalogV21Version
		contentCatalogHash, effectiveCapacityErr = cityWorldVersionBundleHash(effectiveCapacityCatalog)
		if effectiveCapacityErr != nil {
			return CityWorldVersionVector{}, effectiveCapacityErr
		}
	}
	freightSettlementPolicy := CityOpenWorldFreightSettlementPolicy{}
	if cityEngineSupportsOpenWorldFreightSettlements(simulationVersion) {
		freightSettlementCatalog, freightSettlementErr := loadCityWorldVersionV22FreightSettlementCatalog(ctx, queryer, worldID)
		if freightSettlementErr != nil {
			return CityWorldVersionVector{}, freightSettlementErr
		}
		freightSettlementPolicy = CityOpenWorldFreightSettlementPolicy{
			ProfileID:              freightSettlementCatalog.FreightSettlementProfileID,
			ProfileVersion:         freightSettlementCatalog.FreightSettlementProfileVersion,
			ContentHash:            freightSettlementCatalog.FreightSettlementProfileHash,
			SourceContract:         freightSettlementCatalog.FreightSettlementSourceContract,
			ReceiptContract:        freightSettlementCatalog.FreightSettlementReceiptContract,
			ResourceContract:       freightSettlementCatalog.FreightSettlementResourceContract,
			FinancialContract:      freightSettlementCatalog.FreightSettlementFinancialContract,
			LiabilityContract:      freightSettlementCatalog.FreightSettlementLiabilityContract,
			MaximumOrders:          freightSettlementCatalog.FreightSettlementMaximumOrders,
			MaximumCasesPerOrder:   freightSettlementCatalog.FreightSettlementMaximumCasesPerOrder,
			MaximumReceiptsPerCase: freightSettlementCatalog.FreightSettlementMaximumReceiptsPerCase,
			MaximumReceiptsPerTick: freightSettlementCatalog.FreightSettlementMaximumReceiptsPerTick,
		}
		contentCatalogBundleID = cityWorldVersionContentCatalogV22BundleID
		contentCatalogBundleVersion = cityWorldVersionContentCatalogV22Version
		contentCatalogHash, freightSettlementErr = cityWorldVersionBundleHash(freightSettlementCatalog)
		if freightSettlementErr != nil {
			return CityWorldVersionVector{}, freightSettlementErr
		}
	}
	carrierRecoveryPolicy := CityOpenWorldCarrierRecoveryPolicy{}
	if cityEngineSupportsOpenWorldCarrierRecovery(simulationVersion) {
		carrierRecoveryCatalog, carrierRecoveryErr := loadCityWorldVersionV23CarrierRecoveryCatalog(ctx, queryer, worldID)
		if carrierRecoveryErr != nil {
			return CityWorldVersionVector{}, carrierRecoveryErr
		}
		carrierRecoveryPolicy = CityOpenWorldCarrierRecoveryPolicy{
			ProfileID:              carrierRecoveryCatalog.CarrierRecoveryProfileID,
			ProfileVersion:         carrierRecoveryCatalog.CarrierRecoveryProfileVersion,
			ContentHash:            carrierRecoveryCatalog.CarrierRecoveryProfileHash,
			CarrierActorCode:       carrierRecoveryCatalog.CarrierRecoveryActorCode,
			CarrierFirmCode:        carrierRecoveryCatalog.CarrierRecoveryFirmCode,
			FundingContract:        carrierRecoveryCatalog.CarrierRecoveryFundingContract,
			RecoveryContract:       carrierRecoveryCatalog.CarrierRecoveryRecoveryContract,
			ReservePolicy:          carrierRecoveryCatalog.CarrierRecoveryReservePolicy,
			MaximumFundingsPerTick: carrierRecoveryCatalog.CarrierRecoveryMaximumFundingsPerTick,
			MaximumRecoveriesTick:  carrierRecoveryCatalog.CarrierRecoveryMaximumRecoveriesTick,
			MaximumAmountUnits:     carrierRecoveryCatalog.CarrierRecoveryMaximumAmountUnits,
		}
		contentCatalogBundleID = cityWorldVersionContentCatalogV23BundleID
		contentCatalogBundleVersion = cityWorldVersionContentCatalogV23Version
		contentCatalogHash, carrierRecoveryErr = cityWorldVersionBundleHash(carrierRecoveryCatalog)
		if carrierRecoveryErr != nil {
			return CityWorldVersionVector{}, carrierRecoveryErr
		}
	}
	carrierCommercePolicy := CityOpenWorldCarrierCommercePolicy{}
	if cityEngineSupportsOpenWorldCarrierCommerce(simulationVersion) {
		carrierCommerceCatalog, carrierCommerceErr := loadCityWorldVersionV24CarrierCommerceCatalog(ctx, queryer, worldID)
		if carrierCommerceErr != nil {
			return CityWorldVersionVector{}, carrierCommerceErr
		}
		carrierCommercePolicy = CityOpenWorldCarrierCommercePolicy{
			ProfileID:               carrierCommerceCatalog.CarrierCommerceProfileID,
			ProfileVersion:          carrierCommerceCatalog.CarrierCommerceProfileVersion,
			ContentHash:             carrierCommerceCatalog.CarrierCommerceProfileHash,
			CarrierActorCode:        carrierCommerceCatalog.CarrierCommerceActorCode,
			CarrierFirmCode:         carrierCommerceCatalog.CarrierCommerceFirmCode,
			ServiceContract:         carrierCommerceCatalog.CarrierCommerceServiceContract,
			PaymentContract:         carrierCommerceCatalog.CarrierCommercePaymentContract,
			FeePerCargoUnit:         carrierCommerceCatalog.CarrierCommerceFeePerCargoUnit,
			MaximumContractsPerTick: carrierCommerceCatalog.CarrierCommerceMaximumContracts,
			MaximumPaymentsPerTick:  carrierCommerceCatalog.CarrierCommerceMaximumPayments,
		}
		contentCatalogBundleID = cityWorldVersionContentCatalogV24BundleID
		contentCatalogBundleVersion = cityWorldVersionContentCatalogV24Version
		contentCatalogHash, carrierCommerceErr = cityWorldVersionBundleHash(carrierCommerceCatalog)
		if carrierCommerceErr != nil {
			return CityWorldVersionVector{}, carrierCommerceErr
		}
	}
	metadata, err := cityWorldVersionBindingMetadata(nil, "", "")
	if err != nil {
		return CityWorldVersionVector{}, err
	}
	contentCatalogMetadata, err := cityWorldVersionBindingMetadataWithServiceAndImpact(
		nil, "", "", serviceProfileHash, serviceProfileVersion, impactProfileHash, impactProfileVersion,
	)
	if (simulationVersion == CitySimulationVersionOpenWorldV9 || simulationVersion == CitySimulationVersionOpenWorldV10 || simulationVersion == CitySimulationVersionOpenWorldV11 || simulationVersion == CitySimulationVersionOpenWorldV12 || simulationVersion == CitySimulationVersionOpenWorldV13 || simulationVersion == CitySimulationVersionOpenWorldV14) && err == nil {
		contentCatalogMetadata, err = cityWorldVersionBindingMetadataWithServiceImpactAndMobility(
			nil, "", "", serviceProfileHash, serviceProfileVersion, impactProfileHash, impactProfileVersion,
			mobilityProfileHash, mobilityProfileVersion,
		)
	}
	if (simulationVersion == CitySimulationVersionOpenWorldV10 || simulationVersion == CitySimulationVersionOpenWorldV11 || simulationVersion == CitySimulationVersionOpenWorldV12 || simulationVersion == CitySimulationVersionOpenWorldV13 || simulationVersion == CitySimulationVersionOpenWorldV14) && err == nil {
		contentCatalogMetadata, err = cityWorldVersionBindingMetadataWithServiceImpactMobilityAndArrival(
			nil, "", "", serviceProfileHash, serviceProfileVersion, impactProfileHash, impactProfileVersion,
			mobilityProfileHash, mobilityProfileVersion, arrivalProfileHash, arrivalProfileVersion,
		)
	}
	if (simulationVersion == CitySimulationVersionOpenWorldV11 || simulationVersion == CitySimulationVersionOpenWorldV12 || simulationVersion == CitySimulationVersionOpenWorldV13 || simulationVersion == CitySimulationVersionOpenWorldV14) && err == nil {
		contentCatalogMetadata, err = cityWorldVersionBindingMetadataWithServiceImpactMobilityArrivalAndOD(
			nil, "", "", serviceProfileHash, serviceProfileVersion, impactProfileHash, impactProfileVersion,
			mobilityProfileHash, mobilityProfileVersion, arrivalProfileHash, arrivalProfileVersion,
			odProfileHash, odProfileVersion,
		)
	}
	if (simulationVersion == CitySimulationVersionOpenWorldV12 || simulationVersion == CitySimulationVersionOpenWorldV13 || simulationVersion == CitySimulationVersionOpenWorldV14) && err == nil {
		contentCatalogMetadata, err = cityWorldVersionBindingMetadataWithServiceImpactMobilityArrivalODAndCommute(
			nil, "", "", serviceProfileHash, serviceProfileVersion, impactProfileHash, impactProfileVersion,
			mobilityProfileHash, mobilityProfileVersion, arrivalProfileHash, arrivalProfileVersion,
			odProfileHash, odProfileVersion, commuteProfileHash, commuteProfileVersion,
		)
	}
	if simulationVersion == CitySimulationVersionOpenWorldV13 && err == nil {
		contentCatalogMetadata, err = cityWorldVersionBindingMetadataWithServiceImpactMobilityArrivalODCommuteAndCommuteSource(
			nil, "", "", serviceProfileHash, serviceProfileVersion, impactProfileHash, impactProfileVersion,
			mobilityProfileHash, mobilityProfileVersion, arrivalProfileHash, arrivalProfileVersion,
			odProfileHash, odProfileVersion, commuteProfileHash, commuteProfileVersion,
			commuteSourceProfileHash, commuteSourceProfileVersion,
		)
	}
	if simulationVersion == CitySimulationVersionOpenWorldV14 && err == nil {
		contentCatalogMetadata, err = cityWorldVersionBindingMetadataWithServiceImpactMobilityArrivalODCommuteSourceAndLifecycle(
			nil, "", "", serviceProfileHash, serviceProfileVersion, impactProfileHash, impactProfileVersion,
			mobilityProfileHash, mobilityProfileVersion, arrivalProfileHash, arrivalProfileVersion,
			odProfileHash, odProfileVersion, commuteProfileHash, commuteProfileVersion,
			commuteSourceProfileHash, commuteSourceProfileVersion,
			commuteLifecycleProfileHash, commuteLifecycleProfileVersion,
		)
	}
	if simulationVersion == CitySimulationVersionOpenWorldV15 && err == nil {
		contentCatalogMetadata, err = cityWorldVersionBindingMetadataWithSupplyChain(
			nil, "", "", serviceProfileHash, serviceProfileVersion, impactProfileHash, impactProfileVersion,
			mobilityProfileHash, mobilityProfileVersion, arrivalProfileHash, arrivalProfileVersion,
			odProfileHash, odProfileVersion, commuteProfileHash, commuteProfileVersion,
			commuteSourceProfileHash, commuteSourceProfileVersion,
			commuteLifecycleProfileHash, commuteLifecycleProfileVersion,
			supplyChainProfileHash, supplyChainProfileVersion,
			supplyChainNodeContract, supplyChainOrderContract,
			supplyChainSettlementContract, supplyChainDeliveryContract,
		)
	}
	if simulationVersion == CitySimulationVersionOpenWorldV16 && err == nil {
		contentCatalogMetadata, err = cityWorldVersionBindingMetadataWithEnterpriseFreight(
			nil, "", "", serviceProfileHash, serviceProfileVersion, impactProfileHash, impactProfileVersion,
			mobilityProfileHash, mobilityProfileVersion, arrivalProfileHash, arrivalProfileVersion,
			odProfileHash, odProfileVersion, commuteProfileHash, commuteProfileVersion,
			commuteSourceProfileHash, commuteSourceProfileVersion,
			commuteLifecycleProfileHash, commuteLifecycleProfileVersion,
			supplyChainProfileHash, supplyChainProfileVersion,
			supplyChainNodeContract, supplyChainOrderContract,
			supplyChainSettlementContract, supplyChainDeliveryContract,
			freightProfileHash, freightProfileVersion, freightSourceContract,
			freightDemandContract, freightCompletionContract, freightTerminalContract,
			freightCarrierActorCode,
		)
	}
	if simulationVersion == CitySimulationVersionOpenWorldV17 && err == nil {
		contentCatalogMetadata, err = cityWorldVersionBindingMetadataWithEnterpriseFreightReceipts(
			nil, "", "", serviceProfileHash, serviceProfileVersion, impactProfileHash, impactProfileVersion,
			mobilityProfileHash, mobilityProfileVersion, arrivalProfileHash, arrivalProfileVersion,
			odProfileHash, odProfileVersion, commuteProfileHash, commuteProfileVersion,
			commuteSourceProfileHash, commuteSourceProfileVersion,
			commuteLifecycleProfileHash, commuteLifecycleProfileVersion,
			supplyChainProfileHash, supplyChainProfileVersion,
			supplyChainNodeContract, supplyChainOrderContract,
			supplyChainSettlementContract, supplyChainDeliveryContract,
			freightProfileHash, freightProfileVersion, freightSourceContract,
			freightDemandContract, freightCompletionContract, freightTerminalContract,
			freightCarrierActorCode,
			receiptProfileHash, receiptProfileVersion, freightShipmentContract,
			freightReceiptContract, freightLegacyContract,
		)
	}
	if cityEngineSupportsOpenWorldGeneration(simulationVersion, 18) && err == nil {
		contentCatalogMetadata, err = cityWorldVersionBindingMetadataWithFreightBatches(
			nil, "", "", serviceProfileHash, serviceProfileVersion, impactProfileHash, impactProfileVersion,
			mobilityProfileHash, mobilityProfileVersion, arrivalProfileHash, arrivalProfileVersion,
			odProfileHash, odProfileVersion, commuteProfileHash, commuteProfileVersion,
			commuteSourceProfileHash, commuteSourceProfileVersion,
			commuteLifecycleProfileHash, commuteLifecycleProfileVersion,
			supplyChainProfileHash, supplyChainProfileVersion,
			supplyChainNodeContract, supplyChainOrderContract,
			supplyChainSettlementContract, supplyChainDeliveryContract,
			freightProfileHash, freightProfileVersion, freightSourceContract,
			freightDemandContract, freightCompletionContract, freightTerminalContract,
			freightCarrierActorCode,
			receiptProfileHash, receiptProfileVersion, freightShipmentContract,
			freightReceiptContract, freightLegacyContract,
			freightBatchProfileHash, freightBatchProfileVersion,
			freightBatchSourceContract, freightBatchPackingContract,
			freightBatchTransportContract, freightBatchReceiptContract,
			freightBatchMaximumUnits, freightBatchMaximumConsignmentsPerPlan,
			freightBatchMaximumPlansPerTick, freightBatchMaximumObservationsPerTick,
		)
	}
	if cityEngineSupportsOpenWorldSpatialNetwork(simulationVersion) && err == nil {
		contentCatalogMetadata, err = cityWorldVersionBindingMetadataWithSpatialNetwork(
			contentCatalogMetadata, spatialNetworkPolicy,
		)
	}
	if cityEngineSupportsOpenWorldInfrastructure(simulationVersion) && err == nil {
		contentCatalogMetadata, err = cityWorldVersionBindingMetadataWithInfrastructure(
			contentCatalogMetadata, infrastructurePolicy,
		)
	}
	if cityEngineSupportsOpenWorldEffectiveCapacity(simulationVersion) && err == nil {
		contentCatalogMetadata, err = cityWorldVersionBindingMetadataWithEffectiveCapacity(
			contentCatalogMetadata, effectiveCapacityPolicy,
		)
	}
	if cityEngineSupportsOpenWorldFreightSettlements(simulationVersion) && err == nil {
		contentCatalogMetadata, err = cityWorldVersionBindingMetadataWithFreightSettlements(
			contentCatalogMetadata, freightSettlementPolicy,
		)
	}
	if cityEngineSupportsOpenWorldCarrierRecovery(simulationVersion) && err == nil {
		contentCatalogMetadata, err = cityWorldVersionBindingMetadataWithCarrierRecovery(
			contentCatalogMetadata, carrierRecoveryPolicy,
		)
	}
	if cityEngineSupportsOpenWorldCarrierCommerce(simulationVersion) && err == nil {
		contentCatalogMetadata, err = cityWorldVersionBindingMetadataWithCarrierCommerce(
			contentCatalogMetadata, carrierCommercePolicy,
		)
	}
	if err != nil {
		return CityWorldVersionVector{}, err
	}
	policyMetadata, err := cityWorldVersionBindingMetadata(&policyVersion, "", "")
	if err != nil {
		return CityWorldVersionVector{}, err
	}
	worldgenMetadata, err := cityWorldVersionBindingMetadata(
		nil, binding.ContextHash, binding.BootstrapPlanHash,
	)
	if err != nil {
		return CityWorldVersionVector{}, err
	}
	vector := CityWorldVersionVector{
		SchemaVersion: cityWorldVersionVectorSchemaVersion,
		Bindings: []CityWorldVersionBinding{
			{ComponentCode: cityWorldVersionComponentEngine, BundleID: cityWorldVersionEngineBundleID, BundleVersion: cityWorldVersionEngineBundleVersionFor(simulationVersion), ContentHash: engineHash, Metadata: metadata},
			{ComponentCode: cityWorldVersionComponentScenario, BundleID: scenarioID, BundleVersion: scenarioVersion, ContentHash: scenarioHash, Metadata: metadata},
			{ComponentCode: cityWorldVersionComponentEconomicPolicy, BundleID: cityWorldVersionEconomicPolicyBundleID, BundleVersion: cityWorldVersionEconomicPolicyBundleVersion, ContentHash: policyHash, Metadata: policyMetadata},
			{ComponentCode: cityWorldVersionComponentSpatialProfile, BundleID: binding.ProfileID, BundleVersion: binding.ProfileVersion, ContentHash: binding.ProfileHash, Metadata: metadata},
			{ComponentCode: cityWorldVersionComponentWorldgenPlan, BundleID: binding.GeneratorID, BundleVersion: binding.GeneratorVersion, ContentHash: binding.BootstrapPlanHash, Metadata: worldgenMetadata},
			{ComponentCode: cityWorldVersionComponentContentCatalog, BundleID: contentCatalogBundleID, BundleVersion: contentCatalogBundleVersion, ContentHash: contentCatalogHash, Metadata: contentCatalogMetadata},
			{ComponentCode: cityWorldVersionComponentRuleBundle, BundleID: cityWorldVersionRuleBundleID, BundleVersion: ruleVersion, ContentHash: ruleHash, Metadata: metadata},
		},
	}
	sort.Slice(vector.Bindings, func(i, j int) bool {
		return vector.Bindings[i].ComponentCode < vector.Bindings[j].ComponentCode
	})
	// Derivation owns only immutable component bindings. The active generation
	// and baseline tick are assigned by initializeCityWorldVersionVector after
	// the locked world row has supplied them, so do not apply the full-vector
	// header validation here.
	if err = validateCityWorldVersionBindings(vector.Bindings); err != nil {
		return CityWorldVersionVector{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "world_version_vector"}).WithCause(err)
	}
	return vector, nil
}

func cityWorldVersionEngineBundleVersionFor(simulationVersion string) string {
	switch simulationVersion {
	case CitySimulationVersionOpenWorldV6:
		return cityWorldVersionEngineBundleVersionV6
	case CitySimulationVersionOpenWorldV7:
		return cityWorldVersionEngineBundleVersionV7
	case CitySimulationVersionOpenWorldV8:
		return cityWorldVersionEngineBundleVersionV8
	case CitySimulationVersionOpenWorldV9:
		return cityWorldVersionEngineBundleVersionV9
	case CitySimulationVersionOpenWorldV10:
		return cityWorldVersionEngineBundleVersionV10
	case CitySimulationVersionOpenWorldV11:
		return cityWorldVersionEngineBundleVersionV11
	case CitySimulationVersionOpenWorldV12:
		return cityWorldVersionEngineBundleVersionV12
	case CitySimulationVersionOpenWorldV13:
		return cityWorldVersionEngineBundleVersionV13
	case CitySimulationVersionOpenWorldV14:
		return cityWorldVersionEngineBundleVersionV14
	case CitySimulationVersionOpenWorldV15:
		return cityWorldVersionEngineBundleVersionV15
	case CitySimulationVersionOpenWorldV16:
		return cityWorldVersionEngineBundleVersionV16
	case CitySimulationVersionOpenWorldV17:
		return cityWorldVersionEngineBundleVersionV17
	case CitySimulationVersionOpenWorldV18:
		return cityWorldVersionEngineBundleVersionV18
	case CitySimulationVersionOpenWorldV19:
		return cityWorldVersionEngineBundleVersionV19
	case CitySimulationVersionOpenWorldV20:
		return cityWorldVersionEngineBundleVersionV20
	case CitySimulationVersionOpenWorldV21:
		return cityWorldVersionEngineBundleVersionV21
	case CitySimulationVersionOpenWorldV22:
		return cityWorldVersionEngineBundleVersionV22
	case CitySimulationVersionOpenWorldV23:
		return cityWorldVersionEngineBundleVersionV23
	case CitySimulationVersionOpenWorldV24:
		return cityWorldVersionEngineBundleVersionV24
	default:
		return "0.0.0"
	}
}

func loadCityWorldVersionV7ServiceCatalog(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (cityWorldVersionV7ContentCatalog, error) {
	item := cityWorldVersionV7ContentCatalog{Services: make([]cityWorldVersionServiceCatalogDescriptor, 0)}
	if err := queryer.QueryRowContext(ctx, `
SELECT profile_id, profile_version, content_hash
FROM city_open_world_service_profiles
WHERE world_id = $1`, worldID).Scan(
		&item.ServiceProfileID, &item.ServiceProfileVersion, &item.ServiceProfileHash,
	); err != nil {
		return item, fmt.Errorf("load V7 version-vector service profile: %w", err)
	}
	var runtimeID, runtimeCatalogVersion, runtimeCatalogHash string
	if err := queryer.QueryRowContext(ctx, `
SELECT runtime_id, catalog_version, catalog_hash
FROM city_open_world_runtime_profiles
WHERE world_id = $1`, worldID).Scan(&runtimeID, &runtimeCatalogVersion, &runtimeCatalogHash); err != nil {
		return item, fmt.Errorf("load V7 version-vector runtime catalog: %w", err)
	}
	item.RuntimeID, item.RuntimeCatalogVersion, item.RuntimeCatalogHash = runtimeID, runtimeCatalogVersion, runtimeCatalogHash
	rows, err := queryer.QueryContext(ctx, `
SELECT code, definition_version, content_hash
FROM city_open_world_service_catalog
WHERE world_id = $1
ORDER BY code ASC`, worldID)
	if err != nil {
		return item, fmt.Errorf("load V7 version-vector service catalog: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		descriptor := cityWorldVersionServiceCatalogDescriptor{}
		if err = rows.Scan(&descriptor.Code, &descriptor.Version, &descriptor.Hash); err != nil {
			return item, fmt.Errorf("scan V7 version-vector service catalog: %w", err)
		}
		item.Services = append(item.Services, descriptor)
	}
	if err = rows.Err(); err != nil {
		return item, fmt.Errorf("iterate V7 version-vector service catalog: %w", err)
	}
	if len(item.Services) != 4 || !cityWorldVersionHashValid(item.ServiceProfileHash) ||
		!cityWorldVersionHashValid(item.RuntimeCatalogHash) {
		return item, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "world_version_v7_service_catalog"})
	}
	return item, nil
}

func loadCityWorldVersionV8ImpactCatalog(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (cityWorldVersionV8ContentCatalog, error) {
	serviceCatalog, err := loadCityWorldVersionV7ServiceCatalog(ctx, queryer, worldID)
	if err != nil {
		return cityWorldVersionV8ContentCatalog{}, err
	}
	item := cityWorldVersionV8ContentCatalog{
		cityWorldVersionV7ContentCatalog: serviceCatalog,
		Impacts:                          make([]cityWorldVersionImpactCatalogDescriptor, 0),
	}
	if err = queryer.QueryRowContext(ctx, `
SELECT profile_id, profile_version, content_hash
FROM city_open_world_impact_profiles
WHERE world_id = $1`, worldID).Scan(
		&item.ImpactProfileID, &item.ImpactProfileVersion, &item.ImpactProfileHash,
	); err != nil {
		return item, fmt.Errorf("load V8 version-vector impact profile: %w", err)
	}
	rows, err := queryer.QueryContext(ctx, `
SELECT code, definition_version, content_hash
FROM city_open_world_impact_catalog
WHERE world_id = $1
ORDER BY code ASC`, worldID)
	if err != nil {
		return item, fmt.Errorf("load V8 version-vector impact catalog: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		descriptor := cityWorldVersionImpactCatalogDescriptor{}
		if err = rows.Scan(&descriptor.Code, &descriptor.Version, &descriptor.Hash); err != nil {
			return item, fmt.Errorf("scan V8 version-vector impact catalog: %w", err)
		}
		item.Impacts = append(item.Impacts, descriptor)
	}
	if err = rows.Err(); err != nil {
		return item, fmt.Errorf("iterate V8 version-vector impact catalog: %w", err)
	}
	if item.ImpactProfileID != cityOpenWorldImpactProfileID ||
		item.ImpactProfileVersion != cityOpenWorldImpactProfileVersion ||
		len(item.Impacts) != 8 || !cityWorldVersionHashValid(item.ImpactProfileHash) {
		return item, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "world_version_v8_impact_catalog"})
	}
	for _, descriptor := range item.Impacts {
		if descriptor.Code == "" || descriptor.Version == "" || !cityWorldVersionHashValid(descriptor.Hash) {
			return item, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "world_version_v8_impact_catalog"})
		}
	}
	return item, nil
}

func loadCityWorldVersionV9MobilityCatalog(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (cityWorldVersionV9ContentCatalog, error) {
	impactCatalog, err := loadCityWorldVersionV8ImpactCatalog(ctx, queryer, worldID)
	if err != nil {
		return cityWorldVersionV9ContentCatalog{}, err
	}
	item := cityWorldVersionV9ContentCatalog{
		cityWorldVersionV8ContentCatalog: impactCatalog,
		Modes:                            make([]cityWorldVersionMobilityModeDescriptor, 0),
		Hubs:                             make([]cityWorldVersionMobilityHubDescriptor, 0),
		Edges:                            make([]cityWorldVersionMobilityEdgeDescriptor, 0),
	}
	if err = queryer.QueryRowContext(ctx, `
SELECT profile_id, profile_version, content_hash
FROM city_open_world_mobility_profiles
WHERE world_id = $1`, worldID).Scan(
		&item.MobilityProfileID, &item.MobilityProfileVersion, &item.MobilityProfileHash,
	); err != nil {
		return item, fmt.Errorf("load V9 version-vector mobility profile: %w", err)
	}
	modeRows, err := queryer.QueryContext(ctx, `
SELECT code, definition_version, content_hash
FROM city_open_world_mobility_modes
WHERE world_id = $1
ORDER BY code ASC`, worldID)
	if err != nil {
		return item, fmt.Errorf("load V9 version-vector mobility modes: %w", err)
	}
	for modeRows.Next() {
		descriptor := cityWorldVersionMobilityModeDescriptor{}
		if err = modeRows.Scan(&descriptor.Code, &descriptor.Version, &descriptor.Hash); err != nil {
			_ = modeRows.Close()
			return item, fmt.Errorf("scan V9 version-vector mobility mode: %w", err)
		}
		item.Modes = append(item.Modes, descriptor)
	}
	if err = closeCityRows(modeRows, "iterate V9 version-vector mobility modes"); err != nil {
		return item, err
	}
	hubRows, err := queryer.QueryContext(ctx, `
SELECT code, definition_version, content_hash
FROM city_open_world_mobility_hubs
WHERE world_id = $1
ORDER BY code ASC`, worldID)
	if err != nil {
		return item, fmt.Errorf("load V9 version-vector mobility hubs: %w", err)
	}
	for hubRows.Next() {
		descriptor := cityWorldVersionMobilityHubDescriptor{}
		if err = hubRows.Scan(&descriptor.Code, &descriptor.Version, &descriptor.Hash); err != nil {
			_ = hubRows.Close()
			return item, fmt.Errorf("scan V9 version-vector mobility hub: %w", err)
		}
		item.Hubs = append(item.Hubs, descriptor)
	}
	if err = closeCityRows(hubRows, "iterate V9 version-vector mobility hubs"); err != nil {
		return item, err
	}
	edgeRows, err := queryer.QueryContext(ctx, `
SELECT code, definition_version, content_hash
FROM city_open_world_mobility_edges
WHERE world_id = $1
ORDER BY code ASC`, worldID)
	if err != nil {
		return item, fmt.Errorf("load V9 version-vector mobility edges: %w", err)
	}
	for edgeRows.Next() {
		descriptor := cityWorldVersionMobilityEdgeDescriptor{}
		if err = edgeRows.Scan(&descriptor.Code, &descriptor.Version, &descriptor.Hash); err != nil {
			_ = edgeRows.Close()
			return item, fmt.Errorf("scan V9 version-vector mobility edge: %w", err)
		}
		item.Edges = append(item.Edges, descriptor)
	}
	if err = closeCityRows(edgeRows, "iterate V9 version-vector mobility edges"); err != nil {
		return item, err
	}
	if item.MobilityProfileID != cityOpenWorldMobilityProfileID ||
		item.MobilityProfileVersion != cityOpenWorldMobilityProfileVersion ||
		!cityWorldVersionHashValid(item.MobilityProfileHash) || len(item.Modes) != 3 ||
		len(item.Hubs) < 3 || len(item.Edges) == 0 {
		return item, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "world_version_v9_mobility_catalog"})
	}
	for _, descriptor := range item.Modes {
		if descriptor.Code == "" || descriptor.Version != cityOpenWorldMobilityProfileVersion || !cityWorldVersionHashValid(descriptor.Hash) {
			return item, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "world_version_v9_mobility_catalog"})
		}
	}
	for _, descriptor := range item.Hubs {
		if descriptor.Code == "" || descriptor.Version != cityOpenWorldMobilityProfileVersion || !cityWorldVersionHashValid(descriptor.Hash) {
			return item, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "world_version_v9_mobility_catalog"})
		}
	}
	for _, descriptor := range item.Edges {
		if descriptor.Code == "" || descriptor.Version != cityOpenWorldMobilityProfileVersion || !cityWorldVersionHashValid(descriptor.Hash) {
			return item, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "world_version_v9_mobility_catalog"})
		}
	}
	return item, nil
}

func loadCityWorldVersionV10ArrivalCatalog(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (cityWorldVersionV10ContentCatalog, error) {
	mobilityCatalog, err := loadCityWorldVersionV9MobilityCatalog(ctx, queryer, worldID)
	if err != nil {
		return cityWorldVersionV10ContentCatalog{}, err
	}
	item := cityWorldVersionV10ContentCatalog{cityWorldVersionV9ContentCatalog: mobilityCatalog}
	if err = queryer.QueryRowContext(ctx, `
SELECT profile_id, profile_version, content_hash
FROM city_open_world_mobility_arrival_profiles
WHERE world_id = $1`, worldID).Scan(
		&item.ArrivalProfileID, &item.ArrivalProfileVersion, &item.ArrivalProfileHash,
	); err != nil {
		return item, fmt.Errorf("load V10 version-vector arrival profile: %w", err)
	}
	if item.ArrivalProfileID != cityOpenWorldMobilityArrivalProfileID ||
		item.ArrivalProfileVersion != cityOpenWorldMobilityArrivalProfileVersion ||
		!cityWorldVersionHashValid(item.ArrivalProfileHash) {
		return item, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "world_version_v10_arrival_catalog"})
	}
	return item, nil
}

func loadCityWorldVersionV11ODCatalog(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (cityWorldVersionV11ContentCatalog, error) {
	arrivalCatalog, err := loadCityWorldVersionV10ArrivalCatalog(ctx, queryer, worldID)
	if err != nil {
		return cityWorldVersionV11ContentCatalog{}, err
	}
	item := cityWorldVersionV11ContentCatalog{cityWorldVersionV10ContentCatalog: arrivalCatalog}
	if err = queryer.QueryRowContext(ctx, `
SELECT profile_id, profile_version, content_hash
FROM city_open_world_mobility_od_profiles
WHERE world_id = $1`, worldID).Scan(
		&item.ODProfileID, &item.ODProfileVersion, &item.ODProfileHash,
	); err != nil {
		return item, fmt.Errorf("load V11 version-vector OD profile: %w", err)
	}
	if item.ODProfileID != cityOpenWorldMobilityODProfileID ||
		item.ODProfileVersion != cityOpenWorldMobilityODProfileVersion ||
		!cityWorldVersionHashValid(item.ODProfileHash) {
		return item, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "world_version_v11_od_catalog"})
	}
	return item, nil
}

func loadCityWorldVersionV12CommuteCatalog(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (cityWorldVersionV12ContentCatalog, error) {
	odCatalog, err := loadCityWorldVersionV11ODCatalog(ctx, queryer, worldID)
	if err != nil {
		return cityWorldVersionV12ContentCatalog{}, err
	}
	item := cityWorldVersionV12ContentCatalog{cityWorldVersionV11ContentCatalog: odCatalog}
	if err = queryer.QueryRowContext(ctx, `
SELECT profile_id, profile_version, content_hash
FROM city_open_world_commute_profiles
WHERE world_id = $1`, worldID).Scan(
		&item.CommuteProfileID, &item.CommuteProfileVersion, &item.CommuteProfileHash,
	); err != nil {
		return item, fmt.Errorf("load V12 version-vector commute profile: %w", err)
	}
	expectedHash, hashErr := cityOpenWorldCommutePolicyHash(
		cityOpenWorldCommuteAssignmentContract,
		cityOpenWorldCommutePeriodTicks,
		cityOpenWorldCommuteMaximumBindings,
	)
	if hashErr != nil {
		return item, fmt.Errorf("hash V12 version-vector commute profile: %w", hashErr)
	}
	if item.CommuteProfileID != cityOpenWorldCommuteProfileID ||
		item.CommuteProfileVersion != cityOpenWorldCommuteProfileVersion ||
		item.CommuteProfileHash != expectedHash {
		return item, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "world_version_v12_commute_catalog"})
	}
	return item, nil
}

func loadCityWorldVersionV13CommuteSourceCatalog(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (cityWorldVersionV13ContentCatalog, error) {
	commuteCatalog, err := loadCityWorldVersionV12CommuteCatalog(ctx, queryer, worldID)
	if err != nil {
		return cityWorldVersionV13ContentCatalog{}, err
	}
	item := cityWorldVersionV13ContentCatalog{cityWorldVersionV12ContentCatalog: commuteCatalog}
	if err = queryer.QueryRowContext(ctx, `
SELECT profile_id, profile_version, content_hash
FROM city_open_world_commute_source_profiles
WHERE world_id = $1`, worldID).Scan(
		&item.CommuteSourceProfileID, &item.CommuteSourceProfileVersion, &item.CommuteSourceProfileHash,
	); err != nil {
		return item, fmt.Errorf("load V13 version-vector commute source profile: %w", err)
	}
	expectedHash, hashErr := cityOpenWorldCommuteSourcePolicyHash(
		cityOpenWorldCommuteSourceGenerationContract,
		cityOpenWorldCommuteSourceOriginContract,
		cityOpenWorldCommutePeriodTicks,
		cityOpenWorldCommuteSourceSurfaceEgressRadius,
		cityOpenWorldCommuteSourceMaximumGenerationsTick,
	)
	if hashErr != nil {
		return item, fmt.Errorf("hash V13 version-vector commute source profile: %w", hashErr)
	}
	if item.CommuteSourceProfileID != cityOpenWorldCommuteSourceProfileID ||
		item.CommuteSourceProfileVersion != cityOpenWorldCommuteSourceProfileVersion ||
		item.CommuteSourceProfileHash != expectedHash {
		return item, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "world_version_v13_commute_source_catalog"})
	}
	return item, nil
}

func loadCityWorldVersionV14CommuteLifecycleCatalog(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (cityWorldVersionV14ContentCatalog, error) {
	sourceCatalog, err := loadCityWorldVersionV13CommuteSourceCatalog(ctx, queryer, worldID)
	if err != nil {
		return cityWorldVersionV14ContentCatalog{}, err
	}
	item := cityWorldVersionV14ContentCatalog{cityWorldVersionV13ContentCatalog: sourceCatalog}
	if err = queryer.QueryRowContext(ctx, `
SELECT profile_id, profile_version, content_hash
FROM city_open_world_commute_lifecycle_profiles
WHERE world_id = $1`, worldID).Scan(
		&item.CommuteLifecycleProfileID,
		&item.CommuteLifecycleProfileVersion,
		&item.CommuteLifecycleProfileHash,
	); err != nil {
		return item, fmt.Errorf("load V14 version-vector commute lifecycle profile: %w", err)
	}
	expectedHash, hashErr := cityOpenWorldCommuteLifecyclePolicyHash()
	if hashErr != nil {
		return item, fmt.Errorf("hash V14 version-vector commute lifecycle profile: %w", hashErr)
	}
	if item.CommuteLifecycleProfileID != cityOpenWorldCommuteLifecycleProfileID ||
		item.CommuteLifecycleProfileVersion != cityOpenWorldCommuteLifecycleProfileVersion ||
		item.CommuteLifecycleProfileHash != expectedHash {
		return item, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "world_version_v14_commute_lifecycle_catalog"})
	}
	return item, nil
}

func loadCityWorldVersionV15SupplyChainCatalog(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (cityWorldVersionV15ContentCatalog, error) {
	lifecycleCatalog, err := loadCityWorldVersionV14CommuteLifecycleCatalog(ctx, queryer, worldID)
	if err != nil {
		return cityWorldVersionV15ContentCatalog{}, err
	}
	item := cityWorldVersionV15ContentCatalog{cityWorldVersionV14ContentCatalog: lifecycleCatalog}
	if err = queryer.QueryRowContext(ctx, `
SELECT profile_id, profile_version, content_hash,
       node_contract, order_contract, settlement_contract, delivery_contract
FROM city_open_world_supply_chain_profiles
WHERE world_id = $1`, worldID).Scan(
		&item.SupplyChainProfileID,
		&item.SupplyChainProfileVersion,
		&item.SupplyChainProfileHash,
		&item.SupplyChainNodeContract,
		&item.SupplyChainOrderContract,
		&item.SupplyChainSettlementContract,
		&item.SupplyChainDeliveryContract,
	); err != nil {
		return item, fmt.Errorf("load V15 version-vector supply-chain profile: %w", err)
	}
	expectedHash, hashErr := cityOpenWorldSupplyChainPolicyHash()
	if hashErr != nil {
		return item, fmt.Errorf("hash V15 version-vector supply-chain profile: %w", hashErr)
	}
	if item.SupplyChainProfileID != cityOpenWorldSupplyChainProfileID ||
		item.SupplyChainProfileVersion != cityOpenWorldSupplyChainProfileVersion ||
		item.SupplyChainProfileHash != expectedHash ||
		item.SupplyChainNodeContract != cityOpenWorldSupplyChainNodeContract ||
		item.SupplyChainOrderContract != cityOpenWorldSupplyChainOrderContract ||
		item.SupplyChainSettlementContract != cityOpenWorldSupplyChainSettlementContract ||
		item.SupplyChainDeliveryContract != cityOpenWorldSupplyChainDeliveryContract {
		return item, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "world_version_v15_supply_chain_catalog"})
	}
	return item, nil
}

func loadCityWorldVersionV16EnterpriseFreightCatalog(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (cityWorldVersionV16ContentCatalog, error) {
	supplyChainCatalog, err := loadCityWorldVersionV15SupplyChainCatalog(ctx, queryer, worldID)
	if err != nil {
		return cityWorldVersionV16ContentCatalog{}, err
	}
	item := cityWorldVersionV16ContentCatalog{cityWorldVersionV15ContentCatalog: supplyChainCatalog}
	if err = queryer.QueryRowContext(ctx, `
SELECT profile_id, profile_version, content_hash,
       source_contract, demand_contract, completion_contract, terminal_contract,
       carrier_actor_code
FROM city_open_world_enterprise_freight_profiles
WHERE world_id = $1`, worldID).Scan(
		&item.EnterpriseFreightProfileID,
		&item.EnterpriseFreightProfileVersion,
		&item.EnterpriseFreightProfileHash,
		&item.EnterpriseFreightSourceContract,
		&item.EnterpriseFreightDemandContract,
		&item.EnterpriseFreightCompletionContract,
		&item.EnterpriseFreightTerminalContract,
		&item.EnterpriseFreightCarrierActorCode,
	); err != nil {
		return item, fmt.Errorf("load V16 version-vector enterprise-freight profile: %w", err)
	}
	expectedHash, hashErr := cityOpenWorldEnterpriseFreightPolicyHash()
	if hashErr != nil {
		return item, fmt.Errorf("hash V16 version-vector enterprise-freight profile: %w", hashErr)
	}
	if item.EnterpriseFreightProfileID != cityOpenWorldEnterpriseFreightProfileID ||
		item.EnterpriseFreightProfileVersion != cityOpenWorldEnterpriseFreightProfileVersion ||
		item.EnterpriseFreightProfileHash != expectedHash ||
		item.EnterpriseFreightSourceContract != cityOpenWorldEnterpriseFreightSourceContract ||
		item.EnterpriseFreightDemandContract != cityOpenWorldEnterpriseFreightDemandContract ||
		item.EnterpriseFreightCompletionContract != cityOpenWorldEnterpriseFreightCompletionContract ||
		item.EnterpriseFreightTerminalContract != cityOpenWorldEnterpriseFreightTerminalContract ||
		item.EnterpriseFreightCarrierActorCode != cityOpenWorldEnterpriseFreightCarrierActorCode {
		return item, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "world_version_v16_enterprise_freight_catalog"})
	}
	return item, nil
}

func loadCityWorldVersionV17EnterpriseFreightReceiptCatalog(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (cityWorldVersionV17ContentCatalog, error) {
	freightCatalog, err := loadCityWorldVersionV16EnterpriseFreightCatalog(ctx, queryer, worldID)
	if err != nil {
		return cityWorldVersionV17ContentCatalog{}, err
	}
	item := cityWorldVersionV17ContentCatalog{cityWorldVersionV16ContentCatalog: freightCatalog}
	if err = queryer.QueryRowContext(ctx, `
SELECT profile_id, profile_version, content_hash,
       shipment_contract, receipt_contract, legacy_contract
FROM city_open_world_enterprise_freight_receipt_profiles
WHERE world_id = $1`, worldID).Scan(
		&item.EnterpriseFreightReceiptProfileID,
		&item.EnterpriseFreightReceiptProfileVersion,
		&item.EnterpriseFreightReceiptProfileHash,
		&item.EnterpriseFreightShipmentContract,
		&item.EnterpriseFreightReceiptContract,
		&item.EnterpriseFreightLegacyContract,
	); err != nil {
		return item, fmt.Errorf("load V17 version-vector freight-receipt profile: %w", err)
	}
	expectedHash, hashErr := cityOpenWorldEnterpriseFreightReceiptPolicyHash()
	if hashErr != nil {
		return item, fmt.Errorf("hash V17 version-vector freight-receipt profile: %w", hashErr)
	}
	if item.EnterpriseFreightReceiptProfileID != cityOpenWorldEnterpriseFreightReceiptProfileID ||
		item.EnterpriseFreightReceiptProfileVersion != cityOpenWorldEnterpriseFreightReceiptProfileVersion ||
		item.EnterpriseFreightReceiptProfileHash != expectedHash ||
		item.EnterpriseFreightShipmentContract != cityOpenWorldEnterpriseFreightReceiptShipmentContract ||
		item.EnterpriseFreightReceiptContract != cityOpenWorldEnterpriseFreightReceiptReceiptContract ||
		item.EnterpriseFreightLegacyContract != cityOpenWorldEnterpriseFreightReceiptLegacyContract {
		return item, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "world_version_v17_freight_receipt_catalog"})
	}
	return item, nil
}

func loadCityWorldVersionV18FreightBatchCatalog(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (cityWorldVersionV18ContentCatalog, error) {
	receiptCatalog, err := loadCityWorldVersionV17EnterpriseFreightReceiptCatalog(ctx, queryer, worldID)
	if err != nil {
		return cityWorldVersionV18ContentCatalog{}, err
	}
	item := cityWorldVersionV18ContentCatalog{cityWorldVersionV17ContentCatalog: receiptCatalog}
	if err = queryer.QueryRowContext(ctx, `
SELECT profile_id, profile_version, content_hash,
       source_contract, packing_contract, transport_contract, receipt_contract,
       maximum_units, maximum_consignments_per_plan, maximum_plans_per_tick,
       maximum_observations_per_tick
FROM city_open_world_freight_batch_profiles
WHERE world_id = $1`, worldID).Scan(
		&item.FreightBatchProfileID,
		&item.FreightBatchProfileVersion,
		&item.FreightBatchProfileHash,
		&item.FreightBatchSourceContract,
		&item.FreightBatchPackingContract,
		&item.FreightBatchTransportContract,
		&item.FreightBatchReceiptContract,
		&item.FreightBatchMaximumUnits,
		&item.FreightBatchMaximumConsignmentsPerPlan,
		&item.FreightBatchMaximumPlansPerTick,
		&item.FreightBatchMaximumObservationsPerTick,
	); err != nil {
		return item, fmt.Errorf("load V18 version-vector freight-batch profile: %w", err)
	}
	expectedHash, hashErr := cityOpenWorldFreightBatchPolicyHash()
	if hashErr != nil {
		return item, fmt.Errorf("hash V18 version-vector freight-batch profile: %w", hashErr)
	}
	if item.FreightBatchProfileID != cityOpenWorldFreightBatchProfileID ||
		item.FreightBatchProfileVersion != cityOpenWorldFreightBatchProfileVersion ||
		item.FreightBatchProfileHash != expectedHash ||
		item.FreightBatchSourceContract != cityOpenWorldFreightBatchSourceContract ||
		item.FreightBatchPackingContract != cityOpenWorldFreightBatchPackingContract ||
		item.FreightBatchTransportContract != cityOpenWorldFreightBatchTransportContract ||
		item.FreightBatchReceiptContract != cityOpenWorldFreightBatchReceiptContract ||
		item.FreightBatchMaximumUnits != cityOpenWorldFreightBatchMaximumUnits ||
		item.FreightBatchMaximumConsignmentsPerPlan != cityOpenWorldFreightBatchMaximumConsignmentsPerPlan ||
		item.FreightBatchMaximumPlansPerTick != cityOpenWorldFreightBatchMaximumPlansPerTick ||
		item.FreightBatchMaximumObservationsPerTick != cityOpenWorldFreightBatchMaximumObservationsPerTick {
		return item, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "world_version_v18_freight_batch_catalog"})
	}
	return item, nil
}

func loadCityWorldVersionV19SpatialNetworkCatalog(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (cityWorldVersionV19ContentCatalog, error) {
	batchCatalog, err := loadCityWorldVersionV18FreightBatchCatalog(ctx, queryer, worldID)
	if err != nil {
		return cityWorldVersionV19ContentCatalog{}, err
	}
	network, err := loadCityOpenWorldSpatialNetworkState(ctx, queryer, worldID)
	if err != nil {
		return cityWorldVersionV19ContentCatalog{}, fmt.Errorf("load V19 version-vector spatial-network state: %w", err)
	}
	policy := network.Policy
	item := cityWorldVersionV19ContentCatalog{
		cityWorldVersionV18ContentCatalog:          batchCatalog,
		SpatialNetworkProfileID:                    policy.ProfileID,
		SpatialNetworkProfileVersion:               policy.ProfileVersion,
		SpatialNetworkProfileHash:                  policy.ContentHash,
		SpatialNetworkTopologyContract:             policy.TopologyContract,
		SpatialNetworkStyleContract:                policy.StyleContract,
		SpatialNetworkTransportStyleID:             policy.TransportStyleID,
		SpatialNetworkTransportStyleVersion:        policy.TransportStyleVersion,
		SpatialNetworkTransportStyleHash:           policy.TransportStyleHash,
		SpatialNetworkSourceWorldgenProfileID:      policy.SourceWorldgenProfileID,
		SpatialNetworkSourceWorldgenProfileVersion: policy.SourceWorldgenProfileVersion,
		SpatialNetworkSourceWorldgenProfileHash:    policy.SourceWorldgenProfileHash,
		SpatialNetworkMaximumNodes:                 policy.MaximumNodes,
		SpatialNetworkMaximumCorridors:             policy.MaximumCorridors,
		SpatialNetworkNodeCount:                    policy.NodeCount,
		SpatialNetworkCorridorCount:                policy.CorridorCount,
	}
	if item.SpatialNetworkProfileID != cityOpenWorldSpatialNetworkProfileID ||
		item.SpatialNetworkProfileVersion != cityOpenWorldSpatialNetworkProfileVersion ||
		!cityWorldVersionHashValid(item.SpatialNetworkProfileHash) ||
		item.SpatialNetworkTopologyContract != cityOpenWorldSpatialNetworkTopologyContract ||
		item.SpatialNetworkStyleContract != cityOpenWorldSpatialNetworkStyleContract ||
		item.SpatialNetworkTransportStyleID == "" || item.SpatialNetworkTransportStyleVersion == "" ||
		!cityWorldVersionHashValid(item.SpatialNetworkTransportStyleHash) ||
		item.SpatialNetworkSourceWorldgenProfileID == "" || item.SpatialNetworkSourceWorldgenProfileVersion == "" ||
		!cityWorldVersionHashValid(item.SpatialNetworkSourceWorldgenProfileHash) ||
		item.SpatialNetworkMaximumNodes != cityOpenWorldSpatialNetworkMaximumNodes ||
		item.SpatialNetworkMaximumCorridors != cityOpenWorldSpatialNetworkMaximumCorridors ||
		item.SpatialNetworkNodeCount != int64(len(network.Nodes)) ||
		item.SpatialNetworkCorridorCount != int64(len(network.Corridors)) {
		return item, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "world_version_v19_spatial_network_catalog"})
	}
	return item, nil
}

func loadCityWorldVersionV20InfrastructureCatalog(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (cityWorldVersionV20ContentCatalog, error) {
	spatialCatalog, err := loadCityWorldVersionV19SpatialNetworkCatalog(ctx, queryer, worldID)
	if err != nil {
		return cityWorldVersionV20ContentCatalog{}, err
	}
	infrastructure, err := loadCityOpenWorldInfrastructureState(ctx, queryer, worldID)
	if err != nil {
		return cityWorldVersionV20ContentCatalog{}, fmt.Errorf("load V20 version-vector infrastructure state: %w", err)
	}
	policy := infrastructure.Policy
	item := cityWorldVersionV20ContentCatalog{
		cityWorldVersionV19ContentCatalog: spatialCatalog,
		InfrastructureProfileID:           policy.ProfileID,
		InfrastructureProfileVersion:      policy.ProfileVersion,
		InfrastructureProfileHash:         policy.ContentHash,
		InfrastructureAssetContract:       policy.AssetContract,
		InfrastructureStateContract:       policy.StateContract,
		InfrastructureMaximumAssets:       policy.MaximumAssets,
		InfrastructureAssetCount:          policy.AssetCount,
		InfrastructureNodeAssetCount:      policy.NodeAssetCount,
		InfrastructureSegmentCount:        policy.SegmentAssetCount,
	}
	if item.InfrastructureProfileID != cityOpenWorldInfrastructureProfileID ||
		item.InfrastructureProfileVersion != cityOpenWorldInfrastructureProfileVersion ||
		!cityWorldVersionHashValid(item.InfrastructureProfileHash) ||
		item.InfrastructureAssetContract != cityOpenWorldInfrastructureAssetContract ||
		item.InfrastructureStateContract != cityOpenWorldInfrastructureStateContract ||
		item.InfrastructureMaximumAssets != cityOpenWorldInfrastructureMaximumAssets ||
		item.InfrastructureAssetCount != int64(len(infrastructure.Assets)) ||
		item.InfrastructureNodeAssetCount != infrastructure.Policy.NodeAssetCount ||
		item.InfrastructureSegmentCount != infrastructure.Policy.SegmentAssetCount ||
		item.InfrastructureAssetCount != item.InfrastructureNodeAssetCount+item.InfrastructureSegmentCount ||
		item.InfrastructureAssetCount <= 0 ||
		item.InfrastructureAssetCount > int64(item.InfrastructureMaximumAssets) {
		return item, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "world_version_v20_infrastructure_catalog"})
	}
	return item, nil
}

func loadCityWorldVersionV21EffectiveCapacityCatalog(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (cityWorldVersionV21ContentCatalog, error) {
	infrastructureCatalog, err := loadCityWorldVersionV20InfrastructureCatalog(ctx, queryer, worldID)
	if err != nil {
		return cityWorldVersionV21ContentCatalog{}, err
	}
	effectiveCapacity, err := loadCityOpenWorldEffectiveCapacityState(ctx, queryer, worldID)
	if err != nil {
		return cityWorldVersionV21ContentCatalog{}, fmt.Errorf("load V21 version-vector effective-capacity state: %w", err)
	}
	policy := effectiveCapacity.Policy
	item := cityWorldVersionV21ContentCatalog{
		cityWorldVersionV20ContentCatalog:   infrastructureCatalog,
		EffectiveCapacityProfileID:          policy.ProfileID,
		EffectiveCapacityProfileVersion:     policy.ProfileVersion,
		EffectiveCapacityProfileHash:        policy.ContentHash,
		EffectiveCapacityTopologyContract:   policy.TopologyContract,
		EffectiveCapacityAssetContract:      policy.AssetContract,
		EffectiveCapacityAdmissionContract:  policy.AdmissionContract,
		EffectiveCapacityVisibilityContract: policy.VisibilityContract,
		EffectiveCapacityMaximumAdmissions:  policy.MaximumAdmissions,
	}
	if item.EffectiveCapacityProfileID != cityOpenWorldEffectiveCapacityProfileID ||
		item.EffectiveCapacityProfileVersion != cityOpenWorldEffectiveCapacityProfileVersion ||
		!cityWorldVersionHashValid(item.EffectiveCapacityProfileHash) ||
		item.EffectiveCapacityTopologyContract != cityOpenWorldEffectiveCapacityTopologyContract ||
		item.EffectiveCapacityAssetContract != cityOpenWorldEffectiveCapacityAssetContract ||
		item.EffectiveCapacityAdmissionContract != cityOpenWorldEffectiveCapacityAdmissionContract ||
		item.EffectiveCapacityVisibilityContract != cityOpenWorldEffectiveCapacityVisibilityContract ||
		item.EffectiveCapacityMaximumAdmissions != cityOpenWorldEffectiveCapacityMaximumAdmissions {
		return item, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "world_version_v21_effective_capacity_catalog"})
	}
	return item, nil
}

func loadCityWorldVersionV22FreightSettlementCatalog(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (cityWorldVersionV22ContentCatalog, error) {
	effectiveCapacityCatalog, err := loadCityWorldVersionV21EffectiveCapacityCatalog(ctx, queryer, worldID)
	if err != nil {
		return cityWorldVersionV22ContentCatalog{}, err
	}
	freightSettlement, err := loadCityOpenWorldFreightSettlementState(ctx, queryer, worldID)
	if err != nil {
		return cityWorldVersionV22ContentCatalog{}, fmt.Errorf("load V22 version-vector freight-settlement state: %w", err)
	}
	policy := freightSettlement.Policy
	item := cityWorldVersionV22ContentCatalog{
		cityWorldVersionV21ContentCatalog:       effectiveCapacityCatalog,
		FreightSettlementProfileID:              policy.ProfileID,
		FreightSettlementProfileVersion:         policy.ProfileVersion,
		FreightSettlementProfileHash:            policy.ContentHash,
		FreightSettlementSourceContract:         policy.SourceContract,
		FreightSettlementReceiptContract:        policy.ReceiptContract,
		FreightSettlementResourceContract:       policy.ResourceContract,
		FreightSettlementFinancialContract:      policy.FinancialContract,
		FreightSettlementLiabilityContract:      policy.LiabilityContract,
		FreightSettlementMaximumOrders:          policy.MaximumOrders,
		FreightSettlementMaximumCasesPerOrder:   policy.MaximumCasesPerOrder,
		FreightSettlementMaximumReceiptsPerCase: policy.MaximumReceiptsPerCase,
		FreightSettlementMaximumReceiptsPerTick: policy.MaximumReceiptsPerTick,
	}
	if item.FreightSettlementProfileID != cityOpenWorldFreightSettlementProfileID ||
		item.FreightSettlementProfileVersion != cityOpenWorldFreightSettlementProfileVersion ||
		!cityWorldVersionHashValid(item.FreightSettlementProfileHash) ||
		item.FreightSettlementSourceContract != cityOpenWorldFreightSettlementSourceContract ||
		item.FreightSettlementReceiptContract != cityOpenWorldFreightSettlementReceiptContract ||
		item.FreightSettlementResourceContract != cityOpenWorldFreightSettlementResourceContract ||
		item.FreightSettlementFinancialContract != cityOpenWorldFreightSettlementFinancialContract ||
		item.FreightSettlementLiabilityContract != cityOpenWorldFreightSettlementLiabilityContract ||
		item.FreightSettlementMaximumOrders != cityOpenWorldFreightSettlementMaximumOrders ||
		item.FreightSettlementMaximumCasesPerOrder != cityOpenWorldFreightSettlementMaximumCasesPerOrder ||
		item.FreightSettlementMaximumReceiptsPerCase != cityOpenWorldFreightSettlementMaximumReceiptsPerCase ||
		item.FreightSettlementMaximumReceiptsPerTick != cityOpenWorldFreightSettlementMaximumReceiptsPerTick {
		return item, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "world_version_v22_freight_settlement_catalog"})
	}
	return item, nil
}

func loadCityWorldVersionV23CarrierRecoveryCatalog(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (cityWorldVersionV23ContentCatalog, error) {
	freightSettlementCatalog, err := loadCityWorldVersionV22FreightSettlementCatalog(ctx, queryer, worldID)
	if err != nil {
		return cityWorldVersionV23ContentCatalog{}, err
	}
	carrierRecovery, err := loadCityOpenWorldCarrierRecoveryState(ctx, queryer, worldID)
	if err != nil {
		return cityWorldVersionV23ContentCatalog{}, fmt.Errorf("load V23 version-vector carrier-recovery state: %w", err)
	}
	policy := carrierRecovery.Policy
	item := cityWorldVersionV23ContentCatalog{
		cityWorldVersionV22ContentCatalog:     freightSettlementCatalog,
		CarrierRecoveryProfileID:              policy.ProfileID,
		CarrierRecoveryProfileVersion:         policy.ProfileVersion,
		CarrierRecoveryProfileHash:            policy.ContentHash,
		CarrierRecoveryActorCode:              policy.CarrierActorCode,
		CarrierRecoveryFirmCode:               policy.CarrierFirmCode,
		CarrierRecoveryFundingContract:        policy.FundingContract,
		CarrierRecoveryRecoveryContract:       policy.RecoveryContract,
		CarrierRecoveryReservePolicy:          policy.ReservePolicy,
		CarrierRecoveryMaximumFundingsPerTick: policy.MaximumFundingsPerTick,
		CarrierRecoveryMaximumRecoveriesTick:  policy.MaximumRecoveriesTick,
		CarrierRecoveryMaximumAmountUnits:     policy.MaximumAmountUnits,
	}
	if item.CarrierRecoveryProfileID != cityOpenWorldCarrierRecoveryProfileID ||
		item.CarrierRecoveryProfileVersion != cityOpenWorldCarrierRecoveryProfileVersion ||
		!cityWorldVersionHashValid(item.CarrierRecoveryProfileHash) ||
		item.CarrierRecoveryActorCode != cityOpenWorldEnterpriseFreightCarrierActorCode ||
		item.CarrierRecoveryFirmCode != cityOpenWorldCarrierRecoveryFirmCode ||
		item.CarrierRecoveryFundingContract != cityOpenWorldCarrierRecoveryFundingContract ||
		item.CarrierRecoveryRecoveryContract != cityOpenWorldCarrierRecoveryRecoveryContract ||
		item.CarrierRecoveryReservePolicy != cityOpenWorldCarrierRecoveryReservePolicy ||
		item.CarrierRecoveryMaximumFundingsPerTick != cityOpenWorldCarrierRecoveryMaximumFundingsPerTick ||
		item.CarrierRecoveryMaximumRecoveriesTick != cityOpenWorldCarrierRecoveryMaximumRecoveriesTick ||
		item.CarrierRecoveryMaximumAmountUnits != cityOpenWorldCarrierRecoveryMaximumAmountUnits {
		return item, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "world_version_v23_carrier_recovery_catalog"})
	}
	return item, nil
}

func loadCityWorldVersionV24CarrierCommerceCatalog(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (cityWorldVersionV24ContentCatalog, error) {
	carrierRecoveryCatalog, err := loadCityWorldVersionV23CarrierRecoveryCatalog(ctx, queryer, worldID)
	if err != nil {
		return cityWorldVersionV24ContentCatalog{}, err
	}
	carrierCommerce, err := loadCityOpenWorldCarrierCommerceState(ctx, queryer, worldID)
	if err != nil {
		return cityWorldVersionV24ContentCatalog{}, fmt.Errorf("load V24 version-vector carrier-commerce state: %w", err)
	}
	policy := carrierCommerce.Policy
	item := cityWorldVersionV24ContentCatalog{
		cityWorldVersionV23ContentCatalog: carrierRecoveryCatalog,
		CarrierCommerceProfileID:          policy.ProfileID,
		CarrierCommerceProfileVersion:     policy.ProfileVersion,
		CarrierCommerceProfileHash:        policy.ContentHash,
		CarrierCommerceActorCode:          policy.CarrierActorCode,
		CarrierCommerceFirmCode:           policy.CarrierFirmCode,
		CarrierCommerceServiceContract:    policy.ServiceContract,
		CarrierCommercePaymentContract:    policy.PaymentContract,
		CarrierCommerceFeePerCargoUnit:    policy.FeePerCargoUnit,
		CarrierCommerceMaximumContracts:   policy.MaximumContractsPerTick,
		CarrierCommerceMaximumPayments:    policy.MaximumPaymentsPerTick,
	}
	if item.CarrierCommerceProfileID != cityOpenWorldCarrierCommerceProfileID ||
		item.CarrierCommerceProfileVersion != cityOpenWorldCarrierCommerceProfileVersion ||
		!cityWorldVersionHashValid(item.CarrierCommerceProfileHash) ||
		item.CarrierCommerceActorCode != cityOpenWorldEnterpriseFreightCarrierActorCode ||
		item.CarrierCommerceFirmCode != cityOpenWorldCarrierRecoveryFirmCode ||
		item.CarrierCommerceServiceContract != cityOpenWorldCarrierCommerceContractContract ||
		item.CarrierCommercePaymentContract != cityOpenWorldCarrierCommercePaymentContract ||
		item.CarrierCommerceFeePerCargoUnit != cityOpenWorldCarrierCommerceFeePerCargoUnit ||
		item.CarrierCommerceMaximumContracts != cityOpenWorldCarrierCommerceMaximumContractsPerTick ||
		item.CarrierCommerceMaximumPayments != cityOpenWorldCarrierCommerceMaximumPaymentsPerTick {
		return item, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "world_version_v24_carrier_commerce_catalog"})
	}
	return item, nil
}

func loadCityWorldVersionRuleBundle(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) ([]cityWorldVersionRuleDescriptor, string, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT code, definition_version, content_hash
FROM city_open_world_runtime_definitions
WHERE world_id = $1 AND definition_kind = $2
ORDER BY code ASC`, worldID, WorldRuntimeDefinitionRule)
	if err != nil {
		return nil, "", fmt.Errorf("load city version-vector rule bundle: %w", err)
	}
	defer func() { _ = rows.Close() }()
	rules := make([]cityWorldVersionRuleDescriptor, 0)
	ruleVersion := ""
	for rows.Next() {
		var rule cityWorldVersionRuleDescriptor
		if err = rows.Scan(&rule.Code, &rule.Version, &rule.Hash); err != nil {
			return nil, "", fmt.Errorf("scan city version-vector rule: %w", err)
		}
		if ruleVersion == "" {
			ruleVersion = rule.Version
		} else if ruleVersion != rule.Version {
			return nil, "", ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "world_version_rule_versions"})
		}
		rules = append(rules, rule)
	}
	if err = rows.Err(); err != nil {
		return nil, "", fmt.Errorf("iterate city version-vector rules: %w", err)
	}
	if len(rules) == 0 || ruleVersion == "" {
		return nil, "", ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "world_version_rule_bundle"})
	}
	return rules, ruleVersion, nil
}

func cityWorldVersionBindingMetadata(
	policyVersion *int64,
	contextHash, bootstrapPlanHash string,
) (json.RawMessage, error) {
	return cityWorldVersionBindingMetadataWithServiceAndImpact(
		policyVersion, contextHash, bootstrapPlanHash, "", "", "", "",
	)
}

func cityWorldVersionBindingMetadataWithService(
	policyVersion *int64,
	contextHash, bootstrapPlanHash, serviceProfileHash, serviceProfileVersion string,
) (json.RawMessage, error) {
	return cityWorldVersionBindingMetadataWithServiceAndImpact(
		policyVersion, contextHash, bootstrapPlanHash, serviceProfileHash, serviceProfileVersion, "", "",
	)
}

func cityWorldVersionBindingMetadataWithServiceAndImpact(
	policyVersion *int64,
	contextHash, bootstrapPlanHash, serviceProfileHash, serviceProfileVersion,
	impactProfileHash, impactProfileVersion string,
) (json.RawMessage, error) {
	raw, err := json.Marshal(struct {
		SchemaVersion         int    `json:"schema_version"`
		ProjectionVersion     *int64 `json:"projection_version,omitempty"`
		ContextHash           string `json:"context_hash,omitempty"`
		BootstrapPlanHash     string `json:"bootstrap_plan_hash,omitempty"`
		ServiceProfileHash    string `json:"service_profile_hash,omitempty"`
		ServiceProfileVersion string `json:"service_profile_version,omitempty"`
		ImpactProfileHash     string `json:"impact_profile_hash,omitempty"`
		ImpactProfileVersion  string `json:"impact_profile_version,omitempty"`
	}{
		SchemaVersion:         cityWorldVersionVectorSchemaVersion,
		ProjectionVersion:     policyVersion,
		ContextHash:           contextHash,
		BootstrapPlanHash:     bootstrapPlanHash,
		ServiceProfileHash:    serviceProfileHash,
		ServiceProfileVersion: serviceProfileVersion,
		ImpactProfileHash:     impactProfileHash,
		ImpactProfileVersion:  impactProfileVersion,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal city version-vector metadata: %w", err)
	}
	return raw, nil
}

// cityWorldVersionBindingMetadataWithServiceImpactAndMobility is intentionally
// separate from the V6-V8 helper above.  That preserves the exact historical
// JSON bytes for existing version vectors while adding V9-only metadata to the
// new aggregate-mobility catalog binding.
func cityWorldVersionBindingMetadataWithServiceImpactAndMobility(
	policyVersion *int64,
	contextHash, bootstrapPlanHash, serviceProfileHash, serviceProfileVersion,
	impactProfileHash, impactProfileVersion, mobilityProfileHash, mobilityProfileVersion string,
) (json.RawMessage, error) {
	raw, err := json.Marshal(struct {
		SchemaVersion          int    `json:"schema_version"`
		ProjectionVersion      *int64 `json:"projection_version,omitempty"`
		ContextHash            string `json:"context_hash,omitempty"`
		BootstrapPlanHash      string `json:"bootstrap_plan_hash,omitempty"`
		ServiceProfileHash     string `json:"service_profile_hash,omitempty"`
		ServiceProfileVersion  string `json:"service_profile_version,omitempty"`
		ImpactProfileHash      string `json:"impact_profile_hash,omitempty"`
		ImpactProfileVersion   string `json:"impact_profile_version,omitempty"`
		MobilityProfileHash    string `json:"mobility_profile_hash,omitempty"`
		MobilityProfileVersion string `json:"mobility_profile_version,omitempty"`
	}{
		SchemaVersion:          cityWorldVersionVectorSchemaVersion,
		ProjectionVersion:      policyVersion,
		ContextHash:            contextHash,
		BootstrapPlanHash:      bootstrapPlanHash,
		ServiceProfileHash:     serviceProfileHash,
		ServiceProfileVersion:  serviceProfileVersion,
		ImpactProfileHash:      impactProfileHash,
		ImpactProfileVersion:   impactProfileVersion,
		MobilityProfileHash:    mobilityProfileHash,
		MobilityProfileVersion: mobilityProfileVersion,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal city version-vector V9 metadata: %w", err)
	}
	return raw, nil
}

// cityWorldVersionBindingMetadataWithServiceImpactMobilityAndArrival keeps
// V10 metadata byte-shape separate from V6-V9 vectors. That preserves old
// sealed vectors while making the arrival bridge policy an immutable input.
func cityWorldVersionBindingMetadataWithServiceImpactMobilityAndArrival(
	policyVersion *int64,
	contextHash, bootstrapPlanHash, serviceProfileHash, serviceProfileVersion,
	impactProfileHash, impactProfileVersion, mobilityProfileHash, mobilityProfileVersion,
	arrivalProfileHash, arrivalProfileVersion string,
) (json.RawMessage, error) {
	raw, err := json.Marshal(struct {
		SchemaVersion          int    `json:"schema_version"`
		ProjectionVersion      *int64 `json:"projection_version,omitempty"`
		ContextHash            string `json:"context_hash,omitempty"`
		BootstrapPlanHash      string `json:"bootstrap_plan_hash,omitempty"`
		ServiceProfileHash     string `json:"service_profile_hash,omitempty"`
		ServiceProfileVersion  string `json:"service_profile_version,omitempty"`
		ImpactProfileHash      string `json:"impact_profile_hash,omitempty"`
		ImpactProfileVersion   string `json:"impact_profile_version,omitempty"`
		MobilityProfileHash    string `json:"mobility_profile_hash,omitempty"`
		MobilityProfileVersion string `json:"mobility_profile_version,omitempty"`
		ArrivalProfileHash     string `json:"arrival_profile_hash,omitempty"`
		ArrivalProfileVersion  string `json:"arrival_profile_version,omitempty"`
	}{
		SchemaVersion:          cityWorldVersionVectorSchemaVersion,
		ProjectionVersion:      policyVersion,
		ContextHash:            contextHash,
		BootstrapPlanHash:      bootstrapPlanHash,
		ServiceProfileHash:     serviceProfileHash,
		ServiceProfileVersion:  serviceProfileVersion,
		ImpactProfileHash:      impactProfileHash,
		ImpactProfileVersion:   impactProfileVersion,
		MobilityProfileHash:    mobilityProfileHash,
		MobilityProfileVersion: mobilityProfileVersion,
		ArrivalProfileHash:     arrivalProfileHash,
		ArrivalProfileVersion:  arrivalProfileVersion,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal city version-vector V10 metadata: %w", err)
	}
	return raw, nil
}

// cityWorldVersionBindingMetadataWithServiceImpactMobilityArrivalAndOD is a
// new V11-only shape. Keeping it separate leaves all previously sealed
// version-vector metadata byte-for-byte stable.
func cityWorldVersionBindingMetadataWithServiceImpactMobilityArrivalAndOD(
	policyVersion *int64,
	contextHash, bootstrapPlanHash, serviceProfileHash, serviceProfileVersion,
	impactProfileHash, impactProfileVersion, mobilityProfileHash, mobilityProfileVersion,
	arrivalProfileHash, arrivalProfileVersion, odProfileHash, odProfileVersion string,
) (json.RawMessage, error) {
	raw, err := json.Marshal(struct {
		SchemaVersion          int    `json:"schema_version"`
		ProjectionVersion      *int64 `json:"projection_version,omitempty"`
		ContextHash            string `json:"context_hash,omitempty"`
		BootstrapPlanHash      string `json:"bootstrap_plan_hash,omitempty"`
		ServiceProfileHash     string `json:"service_profile_hash,omitempty"`
		ServiceProfileVersion  string `json:"service_profile_version,omitempty"`
		ImpactProfileHash      string `json:"impact_profile_hash,omitempty"`
		ImpactProfileVersion   string `json:"impact_profile_version,omitempty"`
		MobilityProfileHash    string `json:"mobility_profile_hash,omitempty"`
		MobilityProfileVersion string `json:"mobility_profile_version,omitempty"`
		ArrivalProfileHash     string `json:"arrival_profile_hash,omitempty"`
		ArrivalProfileVersion  string `json:"arrival_profile_version,omitempty"`
		ODProfileHash          string `json:"od_profile_hash,omitempty"`
		ODProfileVersion       string `json:"od_profile_version,omitempty"`
	}{
		SchemaVersion:          cityWorldVersionVectorSchemaVersion,
		ProjectionVersion:      policyVersion,
		ContextHash:            contextHash,
		BootstrapPlanHash:      bootstrapPlanHash,
		ServiceProfileHash:     serviceProfileHash,
		ServiceProfileVersion:  serviceProfileVersion,
		ImpactProfileHash:      impactProfileHash,
		ImpactProfileVersion:   impactProfileVersion,
		MobilityProfileHash:    mobilityProfileHash,
		MobilityProfileVersion: mobilityProfileVersion,
		ArrivalProfileHash:     arrivalProfileHash,
		ArrivalProfileVersion:  arrivalProfileVersion,
		ODProfileHash:          odProfileHash,
		ODProfileVersion:       odProfileVersion,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal city version-vector V11 metadata: %w", err)
	}
	return raw, nil
}

// cityWorldVersionBindingMetadataWithServiceImpactMobilityArrivalODAndCommute
// is intentionally V12-only. Version-vector metadata is itself immutable, so
// this separate shape preserves every predecessor's historical hash exactly.
func cityWorldVersionBindingMetadataWithServiceImpactMobilityArrivalODAndCommute(
	policyVersion *int64,
	contextHash, bootstrapPlanHash, serviceProfileHash, serviceProfileVersion,
	impactProfileHash, impactProfileVersion, mobilityProfileHash, mobilityProfileVersion,
	arrivalProfileHash, arrivalProfileVersion, odProfileHash, odProfileVersion,
	commuteProfileHash, commuteProfileVersion string,
) (json.RawMessage, error) {
	raw, err := json.Marshal(struct {
		SchemaVersion          int    `json:"schema_version"`
		ProjectionVersion      *int64 `json:"projection_version,omitempty"`
		ContextHash            string `json:"context_hash,omitempty"`
		BootstrapPlanHash      string `json:"bootstrap_plan_hash,omitempty"`
		ServiceProfileHash     string `json:"service_profile_hash,omitempty"`
		ServiceProfileVersion  string `json:"service_profile_version,omitempty"`
		ImpactProfileHash      string `json:"impact_profile_hash,omitempty"`
		ImpactProfileVersion   string `json:"impact_profile_version,omitempty"`
		MobilityProfileHash    string `json:"mobility_profile_hash,omitempty"`
		MobilityProfileVersion string `json:"mobility_profile_version,omitempty"`
		ArrivalProfileHash     string `json:"arrival_profile_hash,omitempty"`
		ArrivalProfileVersion  string `json:"arrival_profile_version,omitempty"`
		ODProfileHash          string `json:"od_profile_hash,omitempty"`
		ODProfileVersion       string `json:"od_profile_version,omitempty"`
		CommuteProfileHash     string `json:"commute_profile_hash,omitempty"`
		CommuteProfileVersion  string `json:"commute_profile_version,omitempty"`
	}{
		SchemaVersion:          cityWorldVersionVectorSchemaVersion,
		ProjectionVersion:      policyVersion,
		ContextHash:            contextHash,
		BootstrapPlanHash:      bootstrapPlanHash,
		ServiceProfileHash:     serviceProfileHash,
		ServiceProfileVersion:  serviceProfileVersion,
		ImpactProfileHash:      impactProfileHash,
		ImpactProfileVersion:   impactProfileVersion,
		MobilityProfileHash:    mobilityProfileHash,
		MobilityProfileVersion: mobilityProfileVersion,
		ArrivalProfileHash:     arrivalProfileHash,
		ArrivalProfileVersion:  arrivalProfileVersion,
		ODProfileHash:          odProfileHash,
		ODProfileVersion:       odProfileVersion,
		CommuteProfileHash:     commuteProfileHash,
		CommuteProfileVersion:  commuteProfileVersion,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal city version-vector V12 metadata: %w", err)
	}
	return raw, nil
}

// cityWorldVersionBindingMetadataWithServiceImpactMobilityArrivalODCommuteAndCommuteSource
// is V13-only. It preserves every predecessor metadata shape and pins the
// source policy without treating dynamic source counters as catalog input.
func cityWorldVersionBindingMetadataWithServiceImpactMobilityArrivalODCommuteAndCommuteSource(
	policyVersion *int64,
	contextHash, bootstrapPlanHash, serviceProfileHash, serviceProfileVersion,
	impactProfileHash, impactProfileVersion, mobilityProfileHash, mobilityProfileVersion,
	arrivalProfileHash, arrivalProfileVersion, odProfileHash, odProfileVersion,
	commuteProfileHash, commuteProfileVersion, commuteSourceProfileHash, commuteSourceProfileVersion string,
) (json.RawMessage, error) {
	raw, err := json.Marshal(struct {
		SchemaVersion               int    `json:"schema_version"`
		ProjectionVersion           *int64 `json:"projection_version,omitempty"`
		ContextHash                 string `json:"context_hash,omitempty"`
		BootstrapPlanHash           string `json:"bootstrap_plan_hash,omitempty"`
		ServiceProfileHash          string `json:"service_profile_hash,omitempty"`
		ServiceProfileVersion       string `json:"service_profile_version,omitempty"`
		ImpactProfileHash           string `json:"impact_profile_hash,omitempty"`
		ImpactProfileVersion        string `json:"impact_profile_version,omitempty"`
		MobilityProfileHash         string `json:"mobility_profile_hash,omitempty"`
		MobilityProfileVersion      string `json:"mobility_profile_version,omitempty"`
		ArrivalProfileHash          string `json:"arrival_profile_hash,omitempty"`
		ArrivalProfileVersion       string `json:"arrival_profile_version,omitempty"`
		ODProfileHash               string `json:"od_profile_hash,omitempty"`
		ODProfileVersion            string `json:"od_profile_version,omitempty"`
		CommuteProfileHash          string `json:"commute_profile_hash,omitempty"`
		CommuteProfileVersion       string `json:"commute_profile_version,omitempty"`
		CommuteSourceProfileHash    string `json:"commute_source_profile_hash,omitempty"`
		CommuteSourceProfileVersion string `json:"commute_source_profile_version,omitempty"`
	}{
		SchemaVersion:               cityWorldVersionVectorSchemaVersion,
		ProjectionVersion:           policyVersion,
		ContextHash:                 contextHash,
		BootstrapPlanHash:           bootstrapPlanHash,
		ServiceProfileHash:          serviceProfileHash,
		ServiceProfileVersion:       serviceProfileVersion,
		ImpactProfileHash:           impactProfileHash,
		ImpactProfileVersion:        impactProfileVersion,
		MobilityProfileHash:         mobilityProfileHash,
		MobilityProfileVersion:      mobilityProfileVersion,
		ArrivalProfileHash:          arrivalProfileHash,
		ArrivalProfileVersion:       arrivalProfileVersion,
		ODProfileHash:               odProfileHash,
		ODProfileVersion:            odProfileVersion,
		CommuteProfileHash:          commuteProfileHash,
		CommuteProfileVersion:       commuteProfileVersion,
		CommuteSourceProfileHash:    commuteSourceProfileHash,
		CommuteSourceProfileVersion: commuteSourceProfileVersion,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal city version-vector V13 metadata: %w", err)
	}
	return raw, nil
}

// cityWorldVersionBindingMetadataWithServiceImpactMobilityArrivalODCommuteSourceAndLifecycle
// is V14-only. As with every earlier stage, its dedicated byte shape keeps
// predecessor vectors immutable while pinning the lifecycle policy.
func cityWorldVersionBindingMetadataWithServiceImpactMobilityArrivalODCommuteSourceAndLifecycle(
	policyVersion *int64,
	contextHash, bootstrapPlanHash, serviceProfileHash, serviceProfileVersion,
	impactProfileHash, impactProfileVersion, mobilityProfileHash, mobilityProfileVersion,
	arrivalProfileHash, arrivalProfileVersion, odProfileHash, odProfileVersion,
	commuteProfileHash, commuteProfileVersion, commuteSourceProfileHash, commuteSourceProfileVersion,
	commuteLifecycleProfileHash, commuteLifecycleProfileVersion string,
) (json.RawMessage, error) {
	raw, err := json.Marshal(struct {
		SchemaVersion                  int    `json:"schema_version"`
		ProjectionVersion              *int64 `json:"projection_version,omitempty"`
		ContextHash                    string `json:"context_hash,omitempty"`
		BootstrapPlanHash              string `json:"bootstrap_plan_hash,omitempty"`
		ServiceProfileHash             string `json:"service_profile_hash,omitempty"`
		ServiceProfileVersion          string `json:"service_profile_version,omitempty"`
		ImpactProfileHash              string `json:"impact_profile_hash,omitempty"`
		ImpactProfileVersion           string `json:"impact_profile_version,omitempty"`
		MobilityProfileHash            string `json:"mobility_profile_hash,omitempty"`
		MobilityProfileVersion         string `json:"mobility_profile_version,omitempty"`
		ArrivalProfileHash             string `json:"arrival_profile_hash,omitempty"`
		ArrivalProfileVersion          string `json:"arrival_profile_version,omitempty"`
		ODProfileHash                  string `json:"od_profile_hash,omitempty"`
		ODProfileVersion               string `json:"od_profile_version,omitempty"`
		CommuteProfileHash             string `json:"commute_profile_hash,omitempty"`
		CommuteProfileVersion          string `json:"commute_profile_version,omitempty"`
		CommuteSourceProfileHash       string `json:"commute_source_profile_hash,omitempty"`
		CommuteSourceProfileVersion    string `json:"commute_source_profile_version,omitempty"`
		CommuteLifecycleProfileHash    string `json:"commute_lifecycle_profile_hash,omitempty"`
		CommuteLifecycleProfileVersion string `json:"commute_lifecycle_profile_version,omitempty"`
	}{
		SchemaVersion:                  cityWorldVersionVectorSchemaVersion,
		ProjectionVersion:              policyVersion,
		ContextHash:                    contextHash,
		BootstrapPlanHash:              bootstrapPlanHash,
		ServiceProfileHash:             serviceProfileHash,
		ServiceProfileVersion:          serviceProfileVersion,
		ImpactProfileHash:              impactProfileHash,
		ImpactProfileVersion:           impactProfileVersion,
		MobilityProfileHash:            mobilityProfileHash,
		MobilityProfileVersion:         mobilityProfileVersion,
		ArrivalProfileHash:             arrivalProfileHash,
		ArrivalProfileVersion:          arrivalProfileVersion,
		ODProfileHash:                  odProfileHash,
		ODProfileVersion:               odProfileVersion,
		CommuteProfileHash:             commuteProfileHash,
		CommuteProfileVersion:          commuteProfileVersion,
		CommuteSourceProfileHash:       commuteSourceProfileHash,
		CommuteSourceProfileVersion:    commuteSourceProfileVersion,
		CommuteLifecycleProfileHash:    commuteLifecycleProfileHash,
		CommuteLifecycleProfileVersion: commuteLifecycleProfileVersion,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal city version-vector V14 metadata: %w", err)
	}
	return raw, nil
}

// cityWorldVersionBindingMetadataWithSupplyChain is V15-only. Its distinct
// serialized shape prevents a later supply-chain field from mutating any
// predecessor vector hash while pinning the complete static F10.0 contract.
func cityWorldVersionBindingMetadataWithSupplyChain(
	policyVersion *int64,
	contextHash, bootstrapPlanHash, serviceProfileHash, serviceProfileVersion,
	impactProfileHash, impactProfileVersion, mobilityProfileHash, mobilityProfileVersion,
	arrivalProfileHash, arrivalProfileVersion, odProfileHash, odProfileVersion,
	commuteProfileHash, commuteProfileVersion, commuteSourceProfileHash, commuteSourceProfileVersion,
	commuteLifecycleProfileHash, commuteLifecycleProfileVersion,
	supplyChainProfileHash, supplyChainProfileVersion,
	supplyChainNodeContract, supplyChainOrderContract,
	supplyChainSettlementContract, supplyChainDeliveryContract string,
) (json.RawMessage, error) {
	raw, err := json.Marshal(struct {
		SchemaVersion                  int    `json:"schema_version"`
		ProjectionVersion              *int64 `json:"projection_version,omitempty"`
		ContextHash                    string `json:"context_hash,omitempty"`
		BootstrapPlanHash              string `json:"bootstrap_plan_hash,omitempty"`
		ServiceProfileHash             string `json:"service_profile_hash,omitempty"`
		ServiceProfileVersion          string `json:"service_profile_version,omitempty"`
		ImpactProfileHash              string `json:"impact_profile_hash,omitempty"`
		ImpactProfileVersion           string `json:"impact_profile_version,omitempty"`
		MobilityProfileHash            string `json:"mobility_profile_hash,omitempty"`
		MobilityProfileVersion         string `json:"mobility_profile_version,omitempty"`
		ArrivalProfileHash             string `json:"arrival_profile_hash,omitempty"`
		ArrivalProfileVersion          string `json:"arrival_profile_version,omitempty"`
		ODProfileHash                  string `json:"od_profile_hash,omitempty"`
		ODProfileVersion               string `json:"od_profile_version,omitempty"`
		CommuteProfileHash             string `json:"commute_profile_hash,omitempty"`
		CommuteProfileVersion          string `json:"commute_profile_version,omitempty"`
		CommuteSourceProfileHash       string `json:"commute_source_profile_hash,omitempty"`
		CommuteSourceProfileVersion    string `json:"commute_source_profile_version,omitempty"`
		CommuteLifecycleProfileHash    string `json:"commute_lifecycle_profile_hash,omitempty"`
		CommuteLifecycleProfileVersion string `json:"commute_lifecycle_profile_version,omitempty"`
		SupplyChainProfileHash         string `json:"supply_chain_profile_hash,omitempty"`
		SupplyChainProfileVersion      string `json:"supply_chain_profile_version,omitempty"`
		SupplyChainNodeContract        string `json:"supply_chain_node_contract,omitempty"`
		SupplyChainOrderContract       string `json:"supply_chain_order_contract,omitempty"`
		SupplyChainSettlementContract  string `json:"supply_chain_settlement_contract,omitempty"`
		SupplyChainDeliveryContract    string `json:"supply_chain_delivery_contract,omitempty"`
	}{
		SchemaVersion:                  cityWorldVersionVectorSchemaVersion,
		ProjectionVersion:              policyVersion,
		ContextHash:                    contextHash,
		BootstrapPlanHash:              bootstrapPlanHash,
		ServiceProfileHash:             serviceProfileHash,
		ServiceProfileVersion:          serviceProfileVersion,
		ImpactProfileHash:              impactProfileHash,
		ImpactProfileVersion:           impactProfileVersion,
		MobilityProfileHash:            mobilityProfileHash,
		MobilityProfileVersion:         mobilityProfileVersion,
		ArrivalProfileHash:             arrivalProfileHash,
		ArrivalProfileVersion:          arrivalProfileVersion,
		ODProfileHash:                  odProfileHash,
		ODProfileVersion:               odProfileVersion,
		CommuteProfileHash:             commuteProfileHash,
		CommuteProfileVersion:          commuteProfileVersion,
		CommuteSourceProfileHash:       commuteSourceProfileHash,
		CommuteSourceProfileVersion:    commuteSourceProfileVersion,
		CommuteLifecycleProfileHash:    commuteLifecycleProfileHash,
		CommuteLifecycleProfileVersion: commuteLifecycleProfileVersion,
		SupplyChainProfileHash:         supplyChainProfileHash,
		SupplyChainProfileVersion:      supplyChainProfileVersion,
		SupplyChainNodeContract:        supplyChainNodeContract,
		SupplyChainOrderContract:       supplyChainOrderContract,
		SupplyChainSettlementContract:  supplyChainSettlementContract,
		SupplyChainDeliveryContract:    supplyChainDeliveryContract,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal city version-vector V15 metadata: %w", err)
	}
	return raw, nil
}

func cityWorldVersionBindingMetadataMap(raw json.RawMessage) (map[string]any, error) {
	metadata := map[string]any{}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&metadata); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("unexpected trailing JSON value")
		}
		return nil, err
	}
	return metadata, nil
}

// cityWorldVersionBindingMetadataWithEnterpriseFreight is isolated from the
// V15 serializer so adding V16 fields cannot perturb a historical supply-chain
// vector. The resulting map is canonical under encoding/json's sorted-key
// encoding and contains the complete V15 predecessor metadata plus V16's
// narrow adapter contract.
func cityWorldVersionBindingMetadataWithEnterpriseFreight(
	policyVersion *int64,
	contextHash, bootstrapPlanHash, serviceProfileHash, serviceProfileVersion,
	impactProfileHash, impactProfileVersion, mobilityProfileHash, mobilityProfileVersion,
	arrivalProfileHash, arrivalProfileVersion, odProfileHash, odProfileVersion,
	commuteProfileHash, commuteProfileVersion, commuteSourceProfileHash, commuteSourceProfileVersion,
	commuteLifecycleProfileHash, commuteLifecycleProfileVersion,
	supplyChainProfileHash, supplyChainProfileVersion,
	supplyChainNodeContract, supplyChainOrderContract,
	supplyChainSettlementContract, supplyChainDeliveryContract,
	freightProfileHash, freightProfileVersion, freightSourceContract,
	freightDemandContract, freightCompletionContract, freightTerminalContract,
	freightCarrierActorCode string,
) (json.RawMessage, error) {
	raw, err := cityWorldVersionBindingMetadataWithSupplyChain(
		policyVersion, contextHash, bootstrapPlanHash, serviceProfileHash, serviceProfileVersion,
		impactProfileHash, impactProfileVersion, mobilityProfileHash, mobilityProfileVersion,
		arrivalProfileHash, arrivalProfileVersion, odProfileHash, odProfileVersion,
		commuteProfileHash, commuteProfileVersion, commuteSourceProfileHash, commuteSourceProfileVersion,
		commuteLifecycleProfileHash, commuteLifecycleProfileVersion,
		supplyChainProfileHash, supplyChainProfileVersion,
		supplyChainNodeContract, supplyChainOrderContract,
		supplyChainSettlementContract, supplyChainDeliveryContract,
	)
	if err != nil {
		return nil, err
	}
	metadata, err := cityWorldVersionBindingMetadataMap(raw)
	if err != nil {
		return nil, fmt.Errorf("decode city version-vector V15 metadata for V16: %w", err)
	}
	metadata["enterprise_freight_profile_hash"] = freightProfileHash
	metadata["enterprise_freight_profile_version"] = freightProfileVersion
	metadata["enterprise_freight_source_contract"] = freightSourceContract
	metadata["enterprise_freight_demand_contract"] = freightDemandContract
	metadata["enterprise_freight_completion_contract"] = freightCompletionContract
	metadata["enterprise_freight_terminal_contract"] = freightTerminalContract
	metadata["enterprise_freight_carrier_actor_code"] = freightCarrierActorCode
	result, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("marshal city version-vector V16 metadata: %w", err)
	}
	return result, nil
}

// cityWorldVersionBindingMetadataWithEnterpriseFreightReceipts preserves the
// V16 serialized shape and appends only the immutable V17 custody contract.
// Mutable shipment/receipt evidence remains outside the version vector.
func cityWorldVersionBindingMetadataWithEnterpriseFreightReceipts(
	policyVersion *int64,
	contextHash, bootstrapPlanHash, serviceProfileHash, serviceProfileVersion,
	impactProfileHash, impactProfileVersion, mobilityProfileHash, mobilityProfileVersion,
	arrivalProfileHash, arrivalProfileVersion, odProfileHash, odProfileVersion,
	commuteProfileHash, commuteProfileVersion, commuteSourceProfileHash, commuteSourceProfileVersion,
	commuteLifecycleProfileHash, commuteLifecycleProfileVersion,
	supplyChainProfileHash, supplyChainProfileVersion,
	supplyChainNodeContract, supplyChainOrderContract,
	supplyChainSettlementContract, supplyChainDeliveryContract,
	freightProfileHash, freightProfileVersion, freightSourceContract,
	freightDemandContract, freightCompletionContract, freightTerminalContract,
	freightCarrierActorCode,
	receiptProfileHash, receiptProfileVersion, shipmentContract,
	receiptContract, legacyContract string,
) (json.RawMessage, error) {
	raw, err := cityWorldVersionBindingMetadataWithEnterpriseFreight(
		policyVersion, contextHash, bootstrapPlanHash, serviceProfileHash, serviceProfileVersion,
		impactProfileHash, impactProfileVersion, mobilityProfileHash, mobilityProfileVersion,
		arrivalProfileHash, arrivalProfileVersion, odProfileHash, odProfileVersion,
		commuteProfileHash, commuteProfileVersion, commuteSourceProfileHash, commuteSourceProfileVersion,
		commuteLifecycleProfileHash, commuteLifecycleProfileVersion,
		supplyChainProfileHash, supplyChainProfileVersion,
		supplyChainNodeContract, supplyChainOrderContract,
		supplyChainSettlementContract, supplyChainDeliveryContract,
		freightProfileHash, freightProfileVersion, freightSourceContract,
		freightDemandContract, freightCompletionContract, freightTerminalContract,
		freightCarrierActorCode,
	)
	if err != nil {
		return nil, err
	}
	metadata, err := cityWorldVersionBindingMetadataMap(raw)
	if err != nil {
		return nil, fmt.Errorf("decode city version-vector V16 metadata for V17: %w", err)
	}
	metadata["enterprise_freight_receipt_profile_hash"] = receiptProfileHash
	metadata["enterprise_freight_receipt_profile_version"] = receiptProfileVersion
	metadata["enterprise_freight_shipment_contract"] = shipmentContract
	metadata["enterprise_freight_receipt_contract"] = receiptContract
	metadata["enterprise_freight_legacy_contract"] = legacyContract
	result, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("marshal city version-vector V17 metadata: %w", err)
	}
	return result, nil
}

// cityWorldVersionBindingMetadataWithFreightBatches preserves the V17 content
// shape and appends only V18's deterministic capacity policy. Plans and
// consignment evidence are runtime state, not version-vector data.
func cityWorldVersionBindingMetadataWithFreightBatches(
	policyVersion *int64,
	contextHash, bootstrapPlanHash, serviceProfileHash, serviceProfileVersion,
	impactProfileHash, impactProfileVersion, mobilityProfileHash, mobilityProfileVersion,
	arrivalProfileHash, arrivalProfileVersion, odProfileHash, odProfileVersion,
	commuteProfileHash, commuteProfileVersion, commuteSourceProfileHash, commuteSourceProfileVersion,
	commuteLifecycleProfileHash, commuteLifecycleProfileVersion,
	supplyChainProfileHash, supplyChainProfileVersion,
	supplyChainNodeContract, supplyChainOrderContract,
	supplyChainSettlementContract, supplyChainDeliveryContract,
	freightProfileHash, freightProfileVersion, freightSourceContract,
	freightDemandContract, freightCompletionContract, freightTerminalContract,
	freightCarrierActorCode,
	receiptProfileHash, receiptProfileVersion, shipmentContract,
	receiptContract, legacyContract,
	batchProfileHash, batchProfileVersion, batchSourceContract, batchPackingContract,
	batchTransportContract, batchReceiptContract string,
	batchMaximumUnits int64,
	batchMaximumConsignmentsPerPlan, batchMaximumPlansPerTick,
	batchMaximumObservationsPerTick int,
) (json.RawMessage, error) {
	raw, err := cityWorldVersionBindingMetadataWithEnterpriseFreightReceipts(
		policyVersion, contextHash, bootstrapPlanHash, serviceProfileHash, serviceProfileVersion,
		impactProfileHash, impactProfileVersion, mobilityProfileHash, mobilityProfileVersion,
		arrivalProfileHash, arrivalProfileVersion, odProfileHash, odProfileVersion,
		commuteProfileHash, commuteProfileVersion, commuteSourceProfileHash, commuteSourceProfileVersion,
		commuteLifecycleProfileHash, commuteLifecycleProfileVersion,
		supplyChainProfileHash, supplyChainProfileVersion,
		supplyChainNodeContract, supplyChainOrderContract,
		supplyChainSettlementContract, supplyChainDeliveryContract,
		freightProfileHash, freightProfileVersion, freightSourceContract,
		freightDemandContract, freightCompletionContract, freightTerminalContract,
		freightCarrierActorCode,
		receiptProfileHash, receiptProfileVersion, shipmentContract,
		receiptContract, legacyContract,
	)
	if err != nil {
		return nil, err
	}
	metadata, err := cityWorldVersionBindingMetadataMap(raw)
	if err != nil {
		return nil, fmt.Errorf("decode city version-vector V17 metadata for V18: %w", err)
	}
	metadata["freight_batch_profile_hash"] = batchProfileHash
	metadata["freight_batch_profile_version"] = batchProfileVersion
	metadata["freight_batch_source_contract"] = batchSourceContract
	metadata["freight_batch_packing_contract"] = batchPackingContract
	metadata["freight_batch_transport_contract"] = batchTransportContract
	metadata["freight_batch_receipt_contract"] = batchReceiptContract
	metadata["freight_batch_maximum_units"] = batchMaximumUnits
	metadata["freight_batch_maximum_consignments_per_plan"] = batchMaximumConsignmentsPerPlan
	metadata["freight_batch_maximum_plans_per_tick"] = batchMaximumPlansPerTick
	metadata["freight_batch_maximum_observations_per_tick"] = batchMaximumObservationsPerTick
	result, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("marshal city version-vector V18 metadata: %w", err)
	}
	return result, nil
}

// cityWorldVersionBindingMetadataWithSpatialNetwork preserves V18's canonical
// metadata exactly and appends V19's static transport identity policy. The
// node/corridor rows themselves are canonical runtime state, never a mutable
// configuration lookup.
func cityWorldVersionBindingMetadataWithSpatialNetwork(
	base json.RawMessage,
	policy CityOpenWorldSpatialNetworkPolicy,
) (json.RawMessage, error) {
	metadata, err := cityWorldVersionBindingMetadataMap(base)
	if err != nil {
		return nil, fmt.Errorf("decode city version-vector V18 metadata for V19: %w", err)
	}
	metadata["spatial_network_profile_hash"] = policy.ContentHash
	metadata["spatial_network_profile_version"] = policy.ProfileVersion
	metadata["spatial_network_topology_contract"] = policy.TopologyContract
	metadata["spatial_network_style_contract"] = policy.StyleContract
	metadata["spatial_network_transport_style_id"] = policy.TransportStyleID
	metadata["spatial_network_transport_style_version"] = policy.TransportStyleVersion
	metadata["spatial_network_transport_style_hash"] = policy.TransportStyleHash
	metadata["spatial_network_source_worldgen_profile_id"] = policy.SourceWorldgenProfileID
	metadata["spatial_network_source_worldgen_profile_version"] = policy.SourceWorldgenProfileVersion
	metadata["spatial_network_source_worldgen_profile_hash"] = policy.SourceWorldgenProfileHash
	metadata["spatial_network_maximum_nodes"] = policy.MaximumNodes
	metadata["spatial_network_maximum_corridors"] = policy.MaximumCorridors
	metadata["spatial_network_node_count"] = policy.NodeCount
	metadata["spatial_network_corridor_count"] = policy.CorridorCount
	result, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("marshal city version-vector V19 metadata: %w", err)
	}
	return result, nil
}

// cityWorldVersionBindingMetadataWithInfrastructure extends the V19 catalog
// metadata with V20's immutable protocol and seed inventory. It intentionally
// excludes mutable state, transitions, capacity, and revision values.
func cityWorldVersionBindingMetadataWithInfrastructure(
	base json.RawMessage,
	policy CityOpenWorldInfrastructurePolicy,
) (json.RawMessage, error) {
	metadata, err := cityWorldVersionBindingMetadataMap(base)
	if err != nil {
		return nil, fmt.Errorf("decode city version-vector V19 metadata for V20: %w", err)
	}
	metadata["infrastructure_profile_hash"] = policy.ContentHash
	metadata["infrastructure_profile_version"] = policy.ProfileVersion
	metadata["infrastructure_asset_contract"] = policy.AssetContract
	metadata["infrastructure_state_contract"] = policy.StateContract
	metadata["infrastructure_maximum_assets"] = policy.MaximumAssets
	metadata["infrastructure_asset_count"] = policy.AssetCount
	metadata["infrastructure_node_asset_count"] = policy.NodeAssetCount
	metadata["infrastructure_segment_asset_count"] = policy.SegmentAssetCount
	result, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("marshal city version-vector V20 metadata: %w", err)
	}
	return result, nil
}

// cityWorldVersionBindingMetadataWithEffectiveCapacity extends V20's sealed
// catalog with V21's immutable admission protocol. Dynamic admissions and
// profile counters are intentionally omitted.
func cityWorldVersionBindingMetadataWithEffectiveCapacity(
	base json.RawMessage,
	policy CityOpenWorldEffectiveCapacityPolicy,
) (json.RawMessage, error) {
	metadata, err := cityWorldVersionBindingMetadataMap(base)
	if err != nil {
		return nil, fmt.Errorf("decode city version-vector V20 metadata for V21: %w", err)
	}
	metadata["effective_capacity_profile_hash"] = policy.ContentHash
	metadata["effective_capacity_profile_version"] = policy.ProfileVersion
	metadata["effective_capacity_topology_contract"] = policy.TopologyContract
	metadata["effective_capacity_asset_contract"] = policy.AssetContract
	metadata["effective_capacity_admission_contract"] = policy.AdmissionContract
	metadata["effective_capacity_visibility_contract"] = policy.VisibilityContract
	metadata["effective_capacity_maximum_admissions"] = policy.MaximumAdmissions
	result, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("marshal city version-vector V21 metadata: %w", err)
	}
	return result, nil
}

func cityWorldVersionBindingMetadataWithFreightSettlements(
	base json.RawMessage,
	policy CityOpenWorldFreightSettlementPolicy,
) (json.RawMessage, error) {
	metadata, err := cityWorldVersionBindingMetadataMap(base)
	if err != nil {
		return nil, fmt.Errorf("decode city version-vector V21 metadata for V22: %w", err)
	}
	metadata["freight_settlement_profile_hash"] = policy.ContentHash
	metadata["freight_settlement_profile_version"] = policy.ProfileVersion
	metadata["freight_settlement_source_contract"] = policy.SourceContract
	metadata["freight_settlement_receipt_contract"] = policy.ReceiptContract
	metadata["freight_settlement_resource_contract"] = policy.ResourceContract
	metadata["freight_settlement_financial_contract"] = policy.FinancialContract
	metadata["freight_settlement_liability_contract"] = policy.LiabilityContract
	metadata["freight_settlement_maximum_orders"] = policy.MaximumOrders
	metadata["freight_settlement_maximum_cases_per_order"] = policy.MaximumCasesPerOrder
	metadata["freight_settlement_maximum_receipts_per_case"] = policy.MaximumReceiptsPerCase
	metadata["freight_settlement_maximum_receipts_per_tick"] = policy.MaximumReceiptsPerTick
	result, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("marshal city version-vector V22 metadata: %w", err)
	}
	return result, nil
}

func cityWorldVersionBindingMetadataWithCarrierRecovery(
	base json.RawMessage,
	policy CityOpenWorldCarrierRecoveryPolicy,
) (json.RawMessage, error) {
	metadata, err := cityWorldVersionBindingMetadataMap(base)
	if err != nil {
		return nil, fmt.Errorf("decode city version-vector V22 metadata for V23: %w", err)
	}
	metadata["carrier_recovery_profile_hash"] = policy.ContentHash
	metadata["carrier_recovery_profile_version"] = policy.ProfileVersion
	metadata["carrier_recovery_actor_code"] = policy.CarrierActorCode
	metadata["carrier_recovery_firm_code"] = policy.CarrierFirmCode
	metadata["carrier_recovery_funding_contract"] = policy.FundingContract
	metadata["carrier_recovery_recovery_contract"] = policy.RecoveryContract
	metadata["carrier_recovery_reserve_policy"] = policy.ReservePolicy
	metadata["carrier_recovery_maximum_fundings_per_tick"] = policy.MaximumFundingsPerTick
	metadata["carrier_recovery_maximum_recoveries_per_tick"] = policy.MaximumRecoveriesTick
	metadata["carrier_recovery_maximum_amount_units"] = policy.MaximumAmountUnits
	result, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("marshal city version-vector V23 metadata: %w", err)
	}
	return result, nil
}

func cityWorldVersionBindingMetadataWithCarrierCommerce(
	base json.RawMessage,
	policy CityOpenWorldCarrierCommercePolicy,
) (json.RawMessage, error) {
	metadata, err := cityWorldVersionBindingMetadataMap(base)
	if err != nil {
		return nil, fmt.Errorf("decode city version-vector V23 metadata for V24: %w", err)
	}
	metadata["carrier_commerce_profile_hash"] = policy.ContentHash
	metadata["carrier_commerce_profile_version"] = policy.ProfileVersion
	metadata["carrier_commerce_actor_code"] = policy.CarrierActorCode
	metadata["carrier_commerce_firm_code"] = policy.CarrierFirmCode
	metadata["carrier_commerce_service_contract"] = policy.ServiceContract
	metadata["carrier_commerce_payment_contract"] = policy.PaymentContract
	metadata["carrier_commerce_fee_per_cargo_unit"] = policy.FeePerCargoUnit
	metadata["carrier_commerce_maximum_contracts_per_tick"] = policy.MaximumContractsPerTick
	metadata["carrier_commerce_maximum_payments_per_tick"] = policy.MaximumPaymentsPerTick
	result, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("marshal city version-vector V24 metadata: %w", err)
	}
	return result, nil
}

func cityWorldVersionBundleHash(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("marshal city version-vector bundle: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func loadCityWorldVersionVector(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*CityWorldVersionVector, error) {
	vector := &CityWorldVersionVector{
		SchemaVersion: cityWorldVersionVectorSchemaVersion,
		Bindings:      make([]CityWorldVersionBinding, 0, len(cityWorldVersionVectorComponentOrder)),
	}
	if err := queryer.QueryRowContext(ctx, `
SELECT vector.generation, vector.baseline_tick
FROM city_worlds world
JOIN city_world_version_vectors vector
  ON vector.world_id = world.id
 AND vector.generation = world.version_vector_generation
WHERE world.id = $1`, worldID).Scan(&vector.Generation, &vector.BaselineTick); err != nil {
		return nil, fmt.Errorf("load active city world version vector: %w", err)
	}
	rows, err := queryer.QueryContext(ctx, `
SELECT component_code, bundle_id, bundle_version, content_hash, metadata
FROM city_world_version_bindings
WHERE world_id = $1 AND generation = $2
ORDER BY component_code ASC`, worldID, vector.Generation)
	if err != nil {
		return nil, fmt.Errorf("load city world version vector: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		binding := CityWorldVersionBinding{}
		if err = rows.Scan(
			&binding.ComponentCode, &binding.BundleID, &binding.BundleVersion,
			&binding.ContentHash, &binding.Metadata,
		); err != nil {
			return nil, fmt.Errorf("scan city world version-vector binding: %w", err)
		}
		vector.Bindings = append(vector.Bindings, binding)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate city world version-vector bindings: %w", err)
	}
	if err = validateCityWorldVersionVector(*vector); err != nil {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "world_version_vector"}).WithCause(err)
	}
	return vector, nil
}

func validateCityWorldVersionVector(vector CityWorldVersionVector) error {
	if vector.SchemaVersion != cityWorldVersionVectorSchemaVersion ||
		vector.Generation < 1 || vector.BaselineTick < 0 ||
		len(vector.Bindings) != len(cityWorldVersionVectorComponentOrder) {
		return fmt.Errorf("unexpected world version-vector shape")
	}
	return validateCityWorldVersionBindings(vector.Bindings)
}

// validateCityWorldVersionBindings verifies the immutable, sorted component
// payload independently from a vector header. This is intentionally shared by
// derivation and persisted-vector validation: generation and baseline tick are
// world-row data, while the bindings are derived from sealed subsystem state.
func validateCityWorldVersionBindings(bindings []CityWorldVersionBinding) error {
	if len(bindings) != len(cityWorldVersionVectorComponentOrder) {
		return fmt.Errorf("unexpected world version-vector binding shape")
	}
	for index, binding := range bindings {
		if binding.ComponentCode != cityWorldVersionVectorComponentOrder[index] ||
			binding.BundleID == "" || binding.BundleVersion == "" ||
			!cityWorldVersionHashValid(binding.ContentHash) || len(binding.Metadata) == 0 {
			return fmt.Errorf("invalid world version-vector component %q", binding.ComponentCode)
		}
		var metadata struct {
			SchemaVersion                              int    `json:"schema_version"`
			ProjectionVersion                          *int64 `json:"projection_version,omitempty"`
			ContextHash                                string `json:"context_hash,omitempty"`
			BootstrapPlanHash                          string `json:"bootstrap_plan_hash,omitempty"`
			ServiceProfileHash                         string `json:"service_profile_hash,omitempty"`
			ServiceProfileVersion                      string `json:"service_profile_version,omitempty"`
			ImpactProfileHash                          string `json:"impact_profile_hash,omitempty"`
			ImpactProfileVersion                       string `json:"impact_profile_version,omitempty"`
			MobilityProfileHash                        string `json:"mobility_profile_hash,omitempty"`
			MobilityProfileVersion                     string `json:"mobility_profile_version,omitempty"`
			ArrivalProfileHash                         string `json:"arrival_profile_hash,omitempty"`
			ArrivalProfileVersion                      string `json:"arrival_profile_version,omitempty"`
			ODProfileHash                              string `json:"od_profile_hash,omitempty"`
			ODProfileVersion                           string `json:"od_profile_version,omitempty"`
			CommuteProfileHash                         string `json:"commute_profile_hash,omitempty"`
			CommuteProfileVersion                      string `json:"commute_profile_version,omitempty"`
			CommuteSourceProfileHash                   string `json:"commute_source_profile_hash,omitempty"`
			CommuteSourceProfileVersion                string `json:"commute_source_profile_version,omitempty"`
			CommuteLifecycleProfileHash                string `json:"commute_lifecycle_profile_hash,omitempty"`
			CommuteLifecycleProfileVersion             string `json:"commute_lifecycle_profile_version,omitempty"`
			SupplyChainProfileHash                     string `json:"supply_chain_profile_hash,omitempty"`
			SupplyChainProfileVersion                  string `json:"supply_chain_profile_version,omitempty"`
			SupplyChainNodeContract                    string `json:"supply_chain_node_contract,omitempty"`
			SupplyChainOrderContract                   string `json:"supply_chain_order_contract,omitempty"`
			SupplyChainSettlementContract              string `json:"supply_chain_settlement_contract,omitempty"`
			SupplyChainDeliveryContract                string `json:"supply_chain_delivery_contract,omitempty"`
			EnterpriseFreightProfileHash               string `json:"enterprise_freight_profile_hash,omitempty"`
			EnterpriseFreightProfileVersion            string `json:"enterprise_freight_profile_version,omitempty"`
			EnterpriseFreightSourceContract            string `json:"enterprise_freight_source_contract,omitempty"`
			EnterpriseFreightDemandContract            string `json:"enterprise_freight_demand_contract,omitempty"`
			EnterpriseFreightCompletionContract        string `json:"enterprise_freight_completion_contract,omitempty"`
			EnterpriseFreightTerminalContract          string `json:"enterprise_freight_terminal_contract,omitempty"`
			EnterpriseFreightCarrierActorCode          string `json:"enterprise_freight_carrier_actor_code,omitempty"`
			EnterpriseFreightReceiptProfileHash        string `json:"enterprise_freight_receipt_profile_hash,omitempty"`
			EnterpriseFreightReceiptProfileVersion     string `json:"enterprise_freight_receipt_profile_version,omitempty"`
			EnterpriseFreightShipmentContract          string `json:"enterprise_freight_shipment_contract,omitempty"`
			EnterpriseFreightReceiptContract           string `json:"enterprise_freight_receipt_contract,omitempty"`
			EnterpriseFreightLegacyContract            string `json:"enterprise_freight_legacy_contract,omitempty"`
			FreightBatchProfileHash                    string `json:"freight_batch_profile_hash,omitempty"`
			FreightBatchProfileVersion                 string `json:"freight_batch_profile_version,omitempty"`
			FreightBatchSourceContract                 string `json:"freight_batch_source_contract,omitempty"`
			FreightBatchPackingContract                string `json:"freight_batch_packing_contract,omitempty"`
			FreightBatchTransportContract              string `json:"freight_batch_transport_contract,omitempty"`
			FreightBatchReceiptContract                string `json:"freight_batch_receipt_contract,omitempty"`
			FreightBatchMaximumUnits                   int64  `json:"freight_batch_maximum_units,omitempty"`
			FreightBatchMaximumConsignmentsPerPlan     int    `json:"freight_batch_maximum_consignments_per_plan,omitempty"`
			FreightBatchMaximumPlansPerTick            int    `json:"freight_batch_maximum_plans_per_tick,omitempty"`
			FreightBatchMaximumObservationsPerTick     int    `json:"freight_batch_maximum_observations_per_tick,omitempty"`
			SpatialNetworkProfileHash                  string `json:"spatial_network_profile_hash,omitempty"`
			SpatialNetworkProfileVersion               string `json:"spatial_network_profile_version,omitempty"`
			SpatialNetworkTopologyContract             string `json:"spatial_network_topology_contract,omitempty"`
			SpatialNetworkStyleContract                string `json:"spatial_network_style_contract,omitempty"`
			SpatialNetworkTransportStyleID             string `json:"spatial_network_transport_style_id,omitempty"`
			SpatialNetworkTransportStyleVersion        string `json:"spatial_network_transport_style_version,omitempty"`
			SpatialNetworkTransportStyleHash           string `json:"spatial_network_transport_style_hash,omitempty"`
			SpatialNetworkSourceWorldgenProfileID      string `json:"spatial_network_source_worldgen_profile_id,omitempty"`
			SpatialNetworkSourceWorldgenProfileVersion string `json:"spatial_network_source_worldgen_profile_version,omitempty"`
			SpatialNetworkSourceWorldgenProfileHash    string `json:"spatial_network_source_worldgen_profile_hash,omitempty"`
			SpatialNetworkMaximumNodes                 int    `json:"spatial_network_maximum_nodes,omitempty"`
			SpatialNetworkMaximumCorridors             int    `json:"spatial_network_maximum_corridors,omitempty"`
			SpatialNetworkNodeCount                    int64  `json:"spatial_network_node_count,omitempty"`
			SpatialNetworkCorridorCount                int64  `json:"spatial_network_corridor_count,omitempty"`
			InfrastructureProfileHash                  string `json:"infrastructure_profile_hash,omitempty"`
			InfrastructureProfileVersion               string `json:"infrastructure_profile_version,omitempty"`
			InfrastructureAssetContract                string `json:"infrastructure_asset_contract,omitempty"`
			InfrastructureStateContract                string `json:"infrastructure_state_contract,omitempty"`
			InfrastructureMaximumAssets                int    `json:"infrastructure_maximum_assets,omitempty"`
			InfrastructureAssetCount                   int64  `json:"infrastructure_asset_count,omitempty"`
			InfrastructureNodeAssetCount               int64  `json:"infrastructure_node_asset_count,omitempty"`
			InfrastructureSegmentAssetCount            int64  `json:"infrastructure_segment_asset_count,omitempty"`
			EffectiveCapacityProfileHash               string `json:"effective_capacity_profile_hash,omitempty"`
			EffectiveCapacityProfileVersion            string `json:"effective_capacity_profile_version,omitempty"`
			EffectiveCapacityTopologyContract          string `json:"effective_capacity_topology_contract,omitempty"`
			EffectiveCapacityAssetContract             string `json:"effective_capacity_asset_contract,omitempty"`
			EffectiveCapacityAdmissionContract         string `json:"effective_capacity_admission_contract,omitempty"`
			EffectiveCapacityVisibilityContract        string `json:"effective_capacity_visibility_contract,omitempty"`
			EffectiveCapacityMaximumAdmissions         int    `json:"effective_capacity_maximum_admissions,omitempty"`
			FreightSettlementProfileHash               string `json:"freight_settlement_profile_hash,omitempty"`
			FreightSettlementProfileVersion            string `json:"freight_settlement_profile_version,omitempty"`
			FreightSettlementSourceContract            string `json:"freight_settlement_source_contract,omitempty"`
			FreightSettlementReceiptContract           string `json:"freight_settlement_receipt_contract,omitempty"`
			FreightSettlementResourceContract          string `json:"freight_settlement_resource_contract,omitempty"`
			FreightSettlementFinancialContract         string `json:"freight_settlement_financial_contract,omitempty"`
			FreightSettlementLiabilityContract         string `json:"freight_settlement_liability_contract,omitempty"`
			FreightSettlementMaximumOrders             int    `json:"freight_settlement_maximum_orders,omitempty"`
			FreightSettlementMaximumCasesPerOrder      int    `json:"freight_settlement_maximum_cases_per_order,omitempty"`
			FreightSettlementMaximumReceiptsPerCase    int    `json:"freight_settlement_maximum_receipts_per_case,omitempty"`
			FreightSettlementMaximumReceiptsPerTick    int    `json:"freight_settlement_maximum_receipts_per_tick,omitempty"`
			CarrierRecoveryProfileHash                 string `json:"carrier_recovery_profile_hash,omitempty"`
			CarrierRecoveryProfileVersion              string `json:"carrier_recovery_profile_version,omitempty"`
			CarrierRecoveryActorCode                   string `json:"carrier_recovery_actor_code,omitempty"`
			CarrierRecoveryFirmCode                    string `json:"carrier_recovery_firm_code,omitempty"`
			CarrierRecoveryFundingContract             string `json:"carrier_recovery_funding_contract,omitempty"`
			CarrierRecoveryRecoveryContract            string `json:"carrier_recovery_recovery_contract,omitempty"`
			CarrierRecoveryReservePolicy               string `json:"carrier_recovery_reserve_policy,omitempty"`
			CarrierRecoveryMaximumFundingsPerTick      int    `json:"carrier_recovery_maximum_fundings_per_tick,omitempty"`
			CarrierRecoveryMaximumRecoveriesPerTick    int    `json:"carrier_recovery_maximum_recoveries_per_tick,omitempty"`
			CarrierRecoveryMaximumAmountUnits          int64  `json:"carrier_recovery_maximum_amount_units,omitempty"`
			CarrierCommerceProfileHash                 string `json:"carrier_commerce_profile_hash,omitempty"`
			CarrierCommerceProfileVersion              string `json:"carrier_commerce_profile_version,omitempty"`
			CarrierCommerceActorCode                   string `json:"carrier_commerce_actor_code,omitempty"`
			CarrierCommerceFirmCode                    string `json:"carrier_commerce_firm_code,omitempty"`
			CarrierCommerceServiceContract             string `json:"carrier_commerce_service_contract,omitempty"`
			CarrierCommercePaymentContract             string `json:"carrier_commerce_payment_contract,omitempty"`
			CarrierCommerceFeePerCargoUnit             int64  `json:"carrier_commerce_fee_per_cargo_unit,omitempty"`
			CarrierCommerceMaximumContractsPerTick     int    `json:"carrier_commerce_maximum_contracts_per_tick,omitempty"`
			CarrierCommerceMaximumPaymentsPerTick      int    `json:"carrier_commerce_maximum_payments_per_tick,omitempty"`
		}
		if err := json.Unmarshal(binding.Metadata, &metadata); err != nil ||
			metadata.SchemaVersion != cityWorldVersionVectorSchemaVersion {
			return fmt.Errorf("invalid world version-vector metadata for %q", binding.ComponentCode)
		}
		switch binding.ComponentCode {
		case cityWorldVersionComponentEconomicPolicy:
			if metadata.ProjectionVersion == nil || *metadata.ProjectionVersion < 0 {
				return fmt.Errorf("invalid world version-vector policy metadata")
			}
		case cityWorldVersionComponentWorldgenPlan:
			if !cityWorldVersionHashValid(metadata.ContextHash) ||
				!cityWorldVersionHashValid(metadata.BootstrapPlanHash) ||
				metadata.BootstrapPlanHash != binding.ContentHash {
				return fmt.Errorf("invalid world version-vector worldgen metadata")
			}
		case cityWorldVersionComponentContentCatalog:
			if binding.BundleID == cityWorldVersionContentCatalogV7BundleID &&
				(!cityWorldVersionHashValid(metadata.ServiceProfileHash) || metadata.ServiceProfileVersion == "") {
				return fmt.Errorf("invalid world version-vector V7 service catalog metadata")
			}
			if binding.BundleID == cityWorldVersionContentCatalogV8BundleID &&
				(!cityWorldVersionHashValid(metadata.ServiceProfileHash) || metadata.ServiceProfileVersion == "" ||
					!cityWorldVersionHashValid(metadata.ImpactProfileHash) || metadata.ImpactProfileVersion == "") {
				return fmt.Errorf("invalid world version-vector V8 impact catalog metadata")
			}
			if binding.BundleID == cityWorldVersionContentCatalogV9BundleID &&
				(!cityWorldVersionHashValid(metadata.ServiceProfileHash) || metadata.ServiceProfileVersion == "" ||
					!cityWorldVersionHashValid(metadata.ImpactProfileHash) || metadata.ImpactProfileVersion == "" ||
					!cityWorldVersionHashValid(metadata.MobilityProfileHash) || metadata.MobilityProfileVersion == "") {
				return fmt.Errorf("invalid world version-vector V9 mobility catalog metadata")
			}
			if binding.BundleID == cityWorldVersionContentCatalogV10BundleID &&
				(!cityWorldVersionHashValid(metadata.ServiceProfileHash) || metadata.ServiceProfileVersion == "" ||
					!cityWorldVersionHashValid(metadata.ImpactProfileHash) || metadata.ImpactProfileVersion == "" ||
					!cityWorldVersionHashValid(metadata.MobilityProfileHash) || metadata.MobilityProfileVersion == "" ||
					!cityWorldVersionHashValid(metadata.ArrivalProfileHash) || metadata.ArrivalProfileVersion == "") {
				return fmt.Errorf("invalid world version-vector V10 arrival catalog metadata")
			}
			if binding.BundleID == cityWorldVersionContentCatalogV11BundleID &&
				(!cityWorldVersionHashValid(metadata.ServiceProfileHash) || metadata.ServiceProfileVersion == "" ||
					!cityWorldVersionHashValid(metadata.ImpactProfileHash) || metadata.ImpactProfileVersion == "" ||
					!cityWorldVersionHashValid(metadata.MobilityProfileHash) || metadata.MobilityProfileVersion == "" ||
					!cityWorldVersionHashValid(metadata.ArrivalProfileHash) || metadata.ArrivalProfileVersion == "" ||
					!cityWorldVersionHashValid(metadata.ODProfileHash) || metadata.ODProfileVersion == "") {
				return fmt.Errorf("invalid world version-vector V11 OD catalog metadata")
			}
			if binding.BundleID == cityWorldVersionContentCatalogV12BundleID &&
				(!cityWorldVersionHashValid(metadata.ServiceProfileHash) || metadata.ServiceProfileVersion == "" ||
					!cityWorldVersionHashValid(metadata.ImpactProfileHash) || metadata.ImpactProfileVersion == "" ||
					!cityWorldVersionHashValid(metadata.MobilityProfileHash) || metadata.MobilityProfileVersion == "" ||
					!cityWorldVersionHashValid(metadata.ArrivalProfileHash) || metadata.ArrivalProfileVersion == "" ||
					!cityWorldVersionHashValid(metadata.ODProfileHash) || metadata.ODProfileVersion == "" ||
					!cityWorldVersionHashValid(metadata.CommuteProfileHash) || metadata.CommuteProfileVersion == "") {
				return fmt.Errorf("invalid world version-vector V12 commute catalog metadata")
			}
			if binding.BundleID == cityWorldVersionContentCatalogV13BundleID &&
				(!cityWorldVersionHashValid(metadata.ServiceProfileHash) || metadata.ServiceProfileVersion == "" ||
					!cityWorldVersionHashValid(metadata.ImpactProfileHash) || metadata.ImpactProfileVersion == "" ||
					!cityWorldVersionHashValid(metadata.MobilityProfileHash) || metadata.MobilityProfileVersion == "" ||
					!cityWorldVersionHashValid(metadata.ArrivalProfileHash) || metadata.ArrivalProfileVersion == "" ||
					!cityWorldVersionHashValid(metadata.ODProfileHash) || metadata.ODProfileVersion == "" ||
					!cityWorldVersionHashValid(metadata.CommuteProfileHash) || metadata.CommuteProfileVersion == "" ||
					!cityWorldVersionHashValid(metadata.CommuteSourceProfileHash) || metadata.CommuteSourceProfileVersion == "") {
				return fmt.Errorf("invalid world version-vector V13 commute source catalog metadata")
			}
			if binding.BundleID == cityWorldVersionContentCatalogV14BundleID &&
				(!cityWorldVersionHashValid(metadata.ServiceProfileHash) || metadata.ServiceProfileVersion == "" ||
					!cityWorldVersionHashValid(metadata.ImpactProfileHash) || metadata.ImpactProfileVersion == "" ||
					!cityWorldVersionHashValid(metadata.MobilityProfileHash) || metadata.MobilityProfileVersion == "" ||
					!cityWorldVersionHashValid(metadata.ArrivalProfileHash) || metadata.ArrivalProfileVersion == "" ||
					!cityWorldVersionHashValid(metadata.ODProfileHash) || metadata.ODProfileVersion == "" ||
					!cityWorldVersionHashValid(metadata.CommuteProfileHash) || metadata.CommuteProfileVersion == "" ||
					!cityWorldVersionHashValid(metadata.CommuteSourceProfileHash) || metadata.CommuteSourceProfileVersion == "" ||
					!cityWorldVersionHashValid(metadata.CommuteLifecycleProfileHash) || metadata.CommuteLifecycleProfileVersion == "") {
				return fmt.Errorf("invalid world version-vector V14 commute lifecycle catalog metadata")
			}
			if binding.BundleID == cityWorldVersionContentCatalogV15BundleID &&
				(!cityWorldVersionHashValid(metadata.ServiceProfileHash) || metadata.ServiceProfileVersion == "" ||
					!cityWorldVersionHashValid(metadata.ImpactProfileHash) || metadata.ImpactProfileVersion == "" ||
					!cityWorldVersionHashValid(metadata.MobilityProfileHash) || metadata.MobilityProfileVersion == "" ||
					!cityWorldVersionHashValid(metadata.ArrivalProfileHash) || metadata.ArrivalProfileVersion == "" ||
					!cityWorldVersionHashValid(metadata.ODProfileHash) || metadata.ODProfileVersion == "" ||
					!cityWorldVersionHashValid(metadata.CommuteProfileHash) || metadata.CommuteProfileVersion == "" ||
					!cityWorldVersionHashValid(metadata.CommuteSourceProfileHash) || metadata.CommuteSourceProfileVersion == "" ||
					!cityWorldVersionHashValid(metadata.CommuteLifecycleProfileHash) || metadata.CommuteLifecycleProfileVersion == "" ||
					!cityWorldVersionHashValid(metadata.SupplyChainProfileHash) || metadata.SupplyChainProfileVersion == "" ||
					metadata.SupplyChainNodeContract != cityOpenWorldSupplyChainNodeContract ||
					metadata.SupplyChainOrderContract != cityOpenWorldSupplyChainOrderContract ||
					metadata.SupplyChainSettlementContract != cityOpenWorldSupplyChainSettlementContract ||
					metadata.SupplyChainDeliveryContract != cityOpenWorldSupplyChainDeliveryContract) {
				return fmt.Errorf("invalid world version-vector V15 supply-chain catalog metadata")
			}
			if binding.BundleID == cityWorldVersionContentCatalogV16BundleID &&
				(!cityWorldVersionHashValid(metadata.ServiceProfileHash) || metadata.ServiceProfileVersion == "" ||
					!cityWorldVersionHashValid(metadata.ImpactProfileHash) || metadata.ImpactProfileVersion == "" ||
					!cityWorldVersionHashValid(metadata.MobilityProfileHash) || metadata.MobilityProfileVersion == "" ||
					!cityWorldVersionHashValid(metadata.ArrivalProfileHash) || metadata.ArrivalProfileVersion == "" ||
					!cityWorldVersionHashValid(metadata.ODProfileHash) || metadata.ODProfileVersion == "" ||
					!cityWorldVersionHashValid(metadata.CommuteProfileHash) || metadata.CommuteProfileVersion == "" ||
					!cityWorldVersionHashValid(metadata.CommuteSourceProfileHash) || metadata.CommuteSourceProfileVersion == "" ||
					!cityWorldVersionHashValid(metadata.CommuteLifecycleProfileHash) || metadata.CommuteLifecycleProfileVersion == "" ||
					!cityWorldVersionHashValid(metadata.SupplyChainProfileHash) || metadata.SupplyChainProfileVersion == "" ||
					metadata.SupplyChainNodeContract != cityOpenWorldSupplyChainNodeContract ||
					metadata.SupplyChainOrderContract != cityOpenWorldSupplyChainOrderContract ||
					metadata.SupplyChainSettlementContract != cityOpenWorldSupplyChainSettlementContract ||
					metadata.SupplyChainDeliveryContract != cityOpenWorldSupplyChainDeliveryContract ||
					!cityWorldVersionHashValid(metadata.EnterpriseFreightProfileHash) || metadata.EnterpriseFreightProfileVersion == "" ||
					metadata.EnterpriseFreightSourceContract != cityOpenWorldEnterpriseFreightSourceContract ||
					metadata.EnterpriseFreightDemandContract != cityOpenWorldEnterpriseFreightDemandContract ||
					metadata.EnterpriseFreightCompletionContract != cityOpenWorldEnterpriseFreightCompletionContract ||
					metadata.EnterpriseFreightTerminalContract != cityOpenWorldEnterpriseFreightTerminalContract ||
					metadata.EnterpriseFreightCarrierActorCode != cityOpenWorldEnterpriseFreightCarrierActorCode) {
				return fmt.Errorf("invalid world version-vector V16 enterprise-freight catalog metadata")
			}
			if (binding.BundleID == cityWorldVersionContentCatalogV17BundleID || binding.BundleID == cityWorldVersionContentCatalogV18BundleID || binding.BundleID == cityWorldVersionContentCatalogV19BundleID || binding.BundleID == cityWorldVersionContentCatalogV20BundleID || binding.BundleID == cityWorldVersionContentCatalogV21BundleID || binding.BundleID == cityWorldVersionContentCatalogV22BundleID || binding.BundleID == cityWorldVersionContentCatalogV23BundleID || binding.BundleID == cityWorldVersionContentCatalogV24BundleID) &&
				(!cityWorldVersionHashValid(metadata.ServiceProfileHash) || metadata.ServiceProfileVersion == "" ||
					!cityWorldVersionHashValid(metadata.ImpactProfileHash) || metadata.ImpactProfileVersion == "" ||
					!cityWorldVersionHashValid(metadata.MobilityProfileHash) || metadata.MobilityProfileVersion == "" ||
					!cityWorldVersionHashValid(metadata.ArrivalProfileHash) || metadata.ArrivalProfileVersion == "" ||
					!cityWorldVersionHashValid(metadata.ODProfileHash) || metadata.ODProfileVersion == "" ||
					!cityWorldVersionHashValid(metadata.CommuteProfileHash) || metadata.CommuteProfileVersion == "" ||
					!cityWorldVersionHashValid(metadata.CommuteSourceProfileHash) || metadata.CommuteSourceProfileVersion == "" ||
					!cityWorldVersionHashValid(metadata.CommuteLifecycleProfileHash) || metadata.CommuteLifecycleProfileVersion == "" ||
					!cityWorldVersionHashValid(metadata.SupplyChainProfileHash) || metadata.SupplyChainProfileVersion == "" ||
					metadata.SupplyChainNodeContract != cityOpenWorldSupplyChainNodeContract ||
					metadata.SupplyChainOrderContract != cityOpenWorldSupplyChainOrderContract ||
					metadata.SupplyChainSettlementContract != cityOpenWorldSupplyChainSettlementContract ||
					metadata.SupplyChainDeliveryContract != cityOpenWorldSupplyChainDeliveryContract ||
					!cityWorldVersionHashValid(metadata.EnterpriseFreightProfileHash) || metadata.EnterpriseFreightProfileVersion == "" ||
					metadata.EnterpriseFreightSourceContract != cityOpenWorldEnterpriseFreightSourceContract ||
					metadata.EnterpriseFreightDemandContract != cityOpenWorldEnterpriseFreightDemandContract ||
					metadata.EnterpriseFreightCompletionContract != cityOpenWorldEnterpriseFreightCompletionContract ||
					metadata.EnterpriseFreightTerminalContract != cityOpenWorldEnterpriseFreightTerminalContract ||
					metadata.EnterpriseFreightCarrierActorCode != cityOpenWorldEnterpriseFreightCarrierActorCode ||
					!cityWorldVersionHashValid(metadata.EnterpriseFreightReceiptProfileHash) ||
					metadata.EnterpriseFreightReceiptProfileVersion == "" ||
					metadata.EnterpriseFreightShipmentContract != cityOpenWorldEnterpriseFreightReceiptShipmentContract ||
					metadata.EnterpriseFreightReceiptContract != cityOpenWorldEnterpriseFreightReceiptReceiptContract ||
					metadata.EnterpriseFreightLegacyContract != cityOpenWorldEnterpriseFreightReceiptLegacyContract) {
				return fmt.Errorf("invalid world version-vector V17 freight-receipt catalog metadata")
			}
			if (binding.BundleID == cityWorldVersionContentCatalogV18BundleID || binding.BundleID == cityWorldVersionContentCatalogV19BundleID || binding.BundleID == cityWorldVersionContentCatalogV20BundleID || binding.BundleID == cityWorldVersionContentCatalogV21BundleID || binding.BundleID == cityWorldVersionContentCatalogV22BundleID || binding.BundleID == cityWorldVersionContentCatalogV23BundleID || binding.BundleID == cityWorldVersionContentCatalogV24BundleID) &&
				(!cityWorldVersionHashValid(metadata.FreightBatchProfileHash) ||
					metadata.FreightBatchProfileVersion == "" ||
					metadata.FreightBatchSourceContract != cityOpenWorldFreightBatchSourceContract ||
					metadata.FreightBatchPackingContract != cityOpenWorldFreightBatchPackingContract ||
					metadata.FreightBatchTransportContract != cityOpenWorldFreightBatchTransportContract ||
					metadata.FreightBatchReceiptContract != cityOpenWorldFreightBatchReceiptContract ||
					metadata.FreightBatchMaximumUnits != cityOpenWorldFreightBatchMaximumUnits ||
					metadata.FreightBatchMaximumConsignmentsPerPlan != cityOpenWorldFreightBatchMaximumConsignmentsPerPlan ||
					metadata.FreightBatchMaximumPlansPerTick != cityOpenWorldFreightBatchMaximumPlansPerTick ||
					metadata.FreightBatchMaximumObservationsPerTick != cityOpenWorldFreightBatchMaximumObservationsPerTick) {
				return fmt.Errorf("invalid world version-vector V18 freight-batch catalog metadata")
			}
			if (binding.BundleID == cityWorldVersionContentCatalogV19BundleID || binding.BundleID == cityWorldVersionContentCatalogV20BundleID || binding.BundleID == cityWorldVersionContentCatalogV21BundleID || binding.BundleID == cityWorldVersionContentCatalogV22BundleID || binding.BundleID == cityWorldVersionContentCatalogV23BundleID || binding.BundleID == cityWorldVersionContentCatalogV24BundleID) &&
				(!cityWorldVersionHashValid(metadata.SpatialNetworkProfileHash) ||
					metadata.SpatialNetworkProfileVersion != cityOpenWorldSpatialNetworkProfileVersion ||
					metadata.SpatialNetworkTopologyContract != cityOpenWorldSpatialNetworkTopologyContract ||
					metadata.SpatialNetworkStyleContract != cityOpenWorldSpatialNetworkStyleContract ||
					metadata.SpatialNetworkTransportStyleID == "" ||
					metadata.SpatialNetworkTransportStyleVersion == "" ||
					!cityWorldVersionHashValid(metadata.SpatialNetworkTransportStyleHash) ||
					metadata.SpatialNetworkSourceWorldgenProfileID == "" ||
					metadata.SpatialNetworkSourceWorldgenProfileVersion == "" ||
					!cityWorldVersionHashValid(metadata.SpatialNetworkSourceWorldgenProfileHash) ||
					metadata.SpatialNetworkMaximumNodes != cityOpenWorldSpatialNetworkMaximumNodes ||
					metadata.SpatialNetworkMaximumCorridors != cityOpenWorldSpatialNetworkMaximumCorridors ||
					metadata.SpatialNetworkNodeCount <= 0 ||
					metadata.SpatialNetworkCorridorCount <= 0 ||
					metadata.SpatialNetworkNodeCount > int64(metadata.SpatialNetworkMaximumNodes) ||
					metadata.SpatialNetworkCorridorCount > int64(metadata.SpatialNetworkMaximumCorridors)) {
				return fmt.Errorf("invalid world version-vector V19 spatial-network catalog metadata")
			}
			if (binding.BundleID == cityWorldVersionContentCatalogV20BundleID || binding.BundleID == cityWorldVersionContentCatalogV21BundleID || binding.BundleID == cityWorldVersionContentCatalogV22BundleID || binding.BundleID == cityWorldVersionContentCatalogV23BundleID || binding.BundleID == cityWorldVersionContentCatalogV24BundleID) &&
				(!cityWorldVersionHashValid(metadata.InfrastructureProfileHash) ||
					metadata.InfrastructureProfileVersion != cityOpenWorldInfrastructureProfileVersion ||
					metadata.InfrastructureAssetContract != cityOpenWorldInfrastructureAssetContract ||
					metadata.InfrastructureStateContract != cityOpenWorldInfrastructureStateContract ||
					metadata.InfrastructureMaximumAssets != cityOpenWorldInfrastructureMaximumAssets ||
					metadata.InfrastructureAssetCount <= 0 ||
					metadata.InfrastructureAssetCount != metadata.InfrastructureNodeAssetCount+metadata.InfrastructureSegmentAssetCount ||
					metadata.InfrastructureAssetCount > int64(metadata.InfrastructureMaximumAssets)) {
				return fmt.Errorf("invalid world version-vector V20 infrastructure catalog metadata")
			}
			if (binding.BundleID == cityWorldVersionContentCatalogV21BundleID || binding.BundleID == cityWorldVersionContentCatalogV22BundleID || binding.BundleID == cityWorldVersionContentCatalogV23BundleID || binding.BundleID == cityWorldVersionContentCatalogV24BundleID) &&
				(!cityWorldVersionHashValid(metadata.EffectiveCapacityProfileHash) ||
					metadata.EffectiveCapacityProfileVersion != cityOpenWorldEffectiveCapacityProfileVersion ||
					metadata.EffectiveCapacityTopologyContract != cityOpenWorldEffectiveCapacityTopologyContract ||
					metadata.EffectiveCapacityAssetContract != cityOpenWorldEffectiveCapacityAssetContract ||
					metadata.EffectiveCapacityAdmissionContract != cityOpenWorldEffectiveCapacityAdmissionContract ||
					metadata.EffectiveCapacityVisibilityContract != cityOpenWorldEffectiveCapacityVisibilityContract ||
					metadata.EffectiveCapacityMaximumAdmissions != cityOpenWorldEffectiveCapacityMaximumAdmissions) {
				return fmt.Errorf("invalid world version-vector V21 effective-capacity catalog metadata")
			}
			if (binding.BundleID == cityWorldVersionContentCatalogV22BundleID || binding.BundleID == cityWorldVersionContentCatalogV23BundleID || binding.BundleID == cityWorldVersionContentCatalogV24BundleID) &&
				(!cityWorldVersionHashValid(metadata.FreightSettlementProfileHash) ||
					metadata.FreightSettlementProfileVersion != cityOpenWorldFreightSettlementProfileVersion ||
					metadata.FreightSettlementSourceContract != cityOpenWorldFreightSettlementSourceContract ||
					metadata.FreightSettlementReceiptContract != cityOpenWorldFreightSettlementReceiptContract ||
					metadata.FreightSettlementResourceContract != cityOpenWorldFreightSettlementResourceContract ||
					metadata.FreightSettlementFinancialContract != cityOpenWorldFreightSettlementFinancialContract ||
					metadata.FreightSettlementLiabilityContract != cityOpenWorldFreightSettlementLiabilityContract ||
					metadata.FreightSettlementMaximumOrders != cityOpenWorldFreightSettlementMaximumOrders ||
					metadata.FreightSettlementMaximumCasesPerOrder != cityOpenWorldFreightSettlementMaximumCasesPerOrder ||
					metadata.FreightSettlementMaximumReceiptsPerCase != cityOpenWorldFreightSettlementMaximumReceiptsPerCase ||
					metadata.FreightSettlementMaximumReceiptsPerTick != cityOpenWorldFreightSettlementMaximumReceiptsPerTick) {
				return fmt.Errorf("invalid world version-vector V22 freight-settlement catalog metadata")
			}
			if (binding.BundleID == cityWorldVersionContentCatalogV23BundleID || binding.BundleID == cityWorldVersionContentCatalogV24BundleID) &&
				(!cityWorldVersionHashValid(metadata.CarrierRecoveryProfileHash) ||
					metadata.CarrierRecoveryProfileVersion != cityOpenWorldCarrierRecoveryProfileVersion ||
					metadata.CarrierRecoveryActorCode != cityOpenWorldEnterpriseFreightCarrierActorCode ||
					metadata.CarrierRecoveryFirmCode != cityOpenWorldCarrierRecoveryFirmCode ||
					metadata.CarrierRecoveryFundingContract != cityOpenWorldCarrierRecoveryFundingContract ||
					metadata.CarrierRecoveryRecoveryContract != cityOpenWorldCarrierRecoveryRecoveryContract ||
					metadata.CarrierRecoveryReservePolicy != cityOpenWorldCarrierRecoveryReservePolicy ||
					metadata.CarrierRecoveryMaximumFundingsPerTick != cityOpenWorldCarrierRecoveryMaximumFundingsPerTick ||
					metadata.CarrierRecoveryMaximumRecoveriesPerTick != cityOpenWorldCarrierRecoveryMaximumRecoveriesTick ||
					metadata.CarrierRecoveryMaximumAmountUnits != cityOpenWorldCarrierRecoveryMaximumAmountUnits) {
				return fmt.Errorf("invalid world version-vector V23 carrier-recovery catalog metadata")
			}
			if binding.BundleID == cityWorldVersionContentCatalogV24BundleID &&
				(!cityWorldVersionHashValid(metadata.CarrierCommerceProfileHash) ||
					metadata.CarrierCommerceProfileVersion != cityOpenWorldCarrierCommerceProfileVersion ||
					metadata.CarrierCommerceActorCode != cityOpenWorldEnterpriseFreightCarrierActorCode ||
					metadata.CarrierCommerceFirmCode != cityOpenWorldCarrierRecoveryFirmCode ||
					metadata.CarrierCommerceServiceContract != cityOpenWorldCarrierCommerceContractContract ||
					metadata.CarrierCommercePaymentContract != cityOpenWorldCarrierCommercePaymentContract ||
					metadata.CarrierCommerceFeePerCargoUnit != cityOpenWorldCarrierCommerceFeePerCargoUnit ||
					metadata.CarrierCommerceMaximumContractsPerTick != cityOpenWorldCarrierCommerceMaximumContractsPerTick ||
					metadata.CarrierCommerceMaximumPaymentsPerTick != cityOpenWorldCarrierCommerceMaximumPaymentsPerTick) {
				return fmt.Errorf("invalid world version-vector V24 carrier-commerce catalog metadata")
			}
		}
	}
	return nil
}

func cityWorldVersionHashValid(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	for index := 0; index < len(value); index++ {
		char := value[index]
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}
