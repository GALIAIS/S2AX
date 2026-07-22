package service

import (
	"context"
	"database/sql"
	"fmt"
)

// cityOpenWorldRuntimeRecoveryIdentity is the stable, canonical identity of a
// runtime fact/effect/case. Storage IDs are intentionally not serialized into
// snapshots, but preserving them when they already exist keeps future foreign
// projections from being needlessly invalidated by a recovery run.
type cityOpenWorldRuntimeRecoveryIdentity struct {
	tick     int64
	sequence int64
}

type cityOpenWorldRuntimeRecoveryIDs struct {
	facts   map[cityOpenWorldRuntimeRecoveryIdentity]int64
	effects map[cityOpenWorldRuntimeRecoveryIdentity]int64
	cases   map[cityOpenWorldRuntimeRecoveryIdentity]int64
}

func loadCityOpenWorldRuntimeRecoveryIDs(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (cityOpenWorldRuntimeRecoveryIDs, error) {
	ids := cityOpenWorldRuntimeRecoveryIDs{
		facts:   make(map[cityOpenWorldRuntimeRecoveryIdentity]int64),
		effects: make(map[cityOpenWorldRuntimeRecoveryIdentity]int64),
		cases:   make(map[cityOpenWorldRuntimeRecoveryIdentity]int64),
	}
	load := func(query, label string, target map[cityOpenWorldRuntimeRecoveryIdentity]int64) error {
		rows, err := queryer.QueryContext(ctx, query, worldID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id, tick, sequence int64
			if err = rows.Scan(&id, &tick, &sequence); err != nil {
				_ = rows.Close()
				return err
			}
			target[cityOpenWorldRuntimeRecoveryIdentity{tick: tick, sequence: sequence}] = id
		}
		return closeCityRows(rows, label)
	}
	if err := load(
		`SELECT id, tick, sequence FROM city_open_world_runtime_facts WHERE world_id = $1 ORDER BY tick, sequence`,
		"iterate open-world runtime fact recovery identities", ids.facts,
	); err != nil {
		return ids, fmt.Errorf("load open-world runtime fact identities: %w", err)
	}
	if err := load(
		`SELECT id, tick, sequence FROM city_open_world_runtime_effects WHERE world_id = $1 ORDER BY tick, sequence`,
		"iterate open-world runtime effect recovery identities", ids.effects,
	); err != nil {
		return ids, fmt.Errorf("load open-world runtime effect identities: %w", err)
	}
	if err := load(
		`SELECT id, tick, sequence FROM city_open_world_rule_cases WHERE world_id = $1 ORDER BY tick, sequence`,
		"iterate open-world runtime case recovery identities", ids.cases,
	); err != nil {
		return ids, fmt.Errorf("load open-world runtime case identities: %w", err)
	}
	return ids, nil
}

// clearCityOpenWorldRuntimeProjection intentionally does not touch generated
// bindings, regions, sectors, chunks, buildings, interiors, or portals. The
// snapshot format stores their immutable provenance/hash graph, rather than a
// duplicate payload archive. StartRecovery verifies that graph against the
// target snapshot after this mutable projection is rebuilt.
func clearCityOpenWorldRuntimeProjection(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	simulationVersion string,
) (int, error) {
	statements := make([]string, 0, 64)
	if cityEngineSupportsOpenWorldCarrierCommerce(simulationVersion) {
		// V24 payments reference V24 contracts, V22 cases, and journals. Clear
		// this newest append-only overlay before rebuilding any predecessor.
		statements = append(statements,
			`DELETE FROM city_open_world_carrier_fee_payments WHERE world_id = $1`,
			`DELETE FROM city_open_world_carrier_service_contracts WHERE world_id = $1`,
			`DELETE FROM city_open_world_carrier_commerce_profiles WHERE world_id = $1`,
		)
	}
	if cityEngineSupportsOpenWorldCarrierRecovery(simulationVersion) {
		// V23 recovery evidence references V22 claim rows, commands, journals,
		// and the manually seeded reserve firm. Remove this successor overlay
		// before the V22 claim graph is cleared and rebuilt.
		statements = append(statements,
			`DELETE FROM city_open_world_freight_claim_recoveries WHERE world_id = $1`,
			`DELETE FROM city_open_world_carrier_reserve_fundings WHERE world_id = $1`,
			`DELETE FROM city_open_world_carrier_recovery_profiles WHERE world_id = $1`,
		)
	}
	if cityEngineSupportsOpenWorldFreightSettlements(simulationVersion) {
		// V22 receipts refer to durable command/resource/journal evidence and
		// observe V15/V17/V18 projections. Remove the newest settlement overlay
		// before restoring any predecessor graph it proves against.
		statements = append(statements,
			`DELETE FROM city_open_world_freight_settlement_claims WHERE world_id = $1`,
			`DELETE FROM city_open_world_freight_settlement_receipt_lines WHERE world_id = $1`,
			`DELETE FROM city_open_world_freight_settlement_receipts WHERE world_id = $1`,
			`DELETE FROM city_open_world_freight_settlement_case_lines WHERE world_id = $1`,
			`DELETE FROM city_open_world_freight_settlement_cases WHERE world_id = $1`,
			`DELETE FROM city_open_world_freight_settlement_orders WHERE world_id = $1`,
			`DELETE FROM city_open_world_freight_settlement_profiles WHERE world_id = $1`,
		)
	}
	if cityEngineSupportsOpenWorldEffectiveCapacity(simulationVersion) {
		// V21 admissions reference both V9 allocations and V20 asset-state
		// facts. Remove this newest audit projection before rebuilding either
		// predecessor graph.
		statements = append(statements,
			`DELETE FROM city_open_world_effective_capacity_admissions WHERE world_id = $1`,
			`DELETE FROM city_open_world_effective_capacity_profiles WHERE world_id = $1`,
		)
	}
	if cityEngineSupportsOpenWorldInfrastructure(simulationVersion) {
		// V20 state and transition history refer to V19's frozen topology and
		// runtime facts. Remove the newest mutable projection first, then let
		// its immutable V19 prerequisite be rebuilt below.
		statements = append(statements,
			`DELETE FROM city_open_world_infrastructure_asset_transitions WHERE world_id = $1`,
			`DELETE FROM city_open_world_infrastructure_asset_states WHERE world_id = $1`,
			`DELETE FROM city_open_world_infrastructure_assets WHERE world_id = $1`,
			`DELETE FROM city_open_world_infrastructure_profiles WHERE world_id = $1`,
		)
	}
	if cityEngineSupportsOpenWorldSpatialNetwork(simulationVersion) {
		// V19's static rows depend on V9 hub/edge topology. Clear them before
		// the predecessor projections so recovery can restore the exact frozen
		// mapping only after V9 has been reconstructed.
		statements = append(statements,
			`DELETE FROM city_open_world_spatial_network_corridors WHERE world_id = $1`,
			`DELETE FROM city_open_world_spatial_network_nodes WHERE world_id = $1`,
			`DELETE FROM city_open_world_spatial_network_profiles WHERE world_id = $1`,
		)
	}
	if cityEngineSupportsOpenWorldFreightBatches(simulationVersion) {
		// V18 receipts depend on V15 delivery/resource evidence and every batch
		// row depends on V16/V9 transport projections. Remove this newest
		// adapter first so the older evidence can be rebuilt independently.
		statements = append(statements,
			`DELETE FROM city_open_world_freight_batch_receipts WHERE world_id = $1`,
			`DELETE FROM city_open_world_freight_batch_transitions WHERE world_id = $1`,
			`DELETE FROM city_open_world_freight_batch_facts WHERE world_id = $1`,
			`DELETE FROM city_open_world_freight_batch_lines WHERE world_id = $1`,
			`DELETE FROM city_open_world_freight_batch_consignments WHERE world_id = $1`,
			`DELETE FROM city_open_world_freight_batch_plans WHERE world_id = $1`,
			`DELETE FROM city_open_world_freight_batch_profiles WHERE world_id = $1`,
		)
	}
	if cityEngineSupportsOpenWorldEnterpriseFreightReceipts(simulationVersion) {
		// V17 receipt rows refer to V15 delivery and V16 freight evidence. They
		// must disappear before either predecessor is rebuilt during recovery.
		statements = append(statements,
			`DELETE FROM city_open_world_enterprise_freight_receipts WHERE world_id = $1`,
			`DELETE FROM city_open_world_enterprise_freight_shipment_transitions WHERE world_id = $1`,
			// Shipments and receipt facts intentionally form a provenance cycle:
			// a shipment remembers its last receipt fact while each fact belongs to
			// a shipment. Break only the derived pointer inside recovery before
			// deleting either side of that cycle.
			`UPDATE city_open_world_enterprise_freight_shipments SET last_receipt_fact_id = NULL WHERE world_id = $1`,
			`DELETE FROM city_open_world_enterprise_freight_shipment_lines WHERE world_id = $1`,
			`DELETE FROM city_open_world_enterprise_freight_receipt_facts WHERE world_id = $1`,
			`DELETE FROM city_open_world_enterprise_freight_shipments WHERE world_id = $1`,
			`DELETE FROM city_open_world_enterprise_freight_receipt_profiles WHERE world_id = $1`,
		)
	}
	if cityEngineSupportsOpenWorldEnterpriseFreight(simulationVersion) {
		// V16 source rows reference both the V15 dispatch ledger and V9
		// mobility evidence. Clear this newest adapter before either predecessor
		// graph so recovery can rebuild every causal link deterministically.
		statements = append(statements,
			`DELETE FROM city_open_world_enterprise_freight_transitions WHERE world_id = $1`,
			`DELETE FROM city_open_world_enterprise_freight_facts WHERE world_id = $1`,
			`DELETE FROM city_open_world_enterprise_freight_source_lines WHERE world_id = $1`,
			`DELETE FROM city_open_world_enterprise_freight_sources WHERE world_id = $1`,
			`DELETE FROM city_open_world_enterprise_freight_profiles WHERE world_id = $1`,
		)
	}
	if cityEngineSupportsOpenWorldSupplyChain(simulationVersion) {
		// V15 references V14 facilities plus durable F2/F3 evidence. Delete the
		// mutable supply-chain graph in strict dependent-first order; journals
		// and resource operations remain authoritative historical projections.
		statements = append(statements,
			`DELETE FROM city_open_world_supply_chain_settlements WHERE world_id = $1`,
			`DELETE FROM city_open_world_supply_chain_deliveries WHERE world_id = $1`,
			`DELETE FROM city_open_world_supply_chain_dispatches WHERE world_id = $1`,
			`DELETE FROM city_open_world_supply_chain_reservation_releases WHERE world_id = $1`,
			`DELETE FROM city_open_world_supply_chain_reservations WHERE world_id = $1`,
			`DELETE FROM city_open_world_supply_chain_order_transitions WHERE world_id = $1`,
			`DELETE FROM city_open_world_supply_chain_order_lines WHERE world_id = $1`,
			`DELETE FROM city_open_world_supply_chain_orders WHERE world_id = $1`,
			`DELETE FROM city_open_world_supply_chain_facts WHERE world_id = $1`,
			`DELETE FROM city_open_world_supply_chain_nodes WHERE world_id = $1`,
			`DELETE FROM city_open_world_supply_chain_profiles WHERE world_id = $1`,
		)
	}
	if cityEngineSupportsOpenWorldCommuteLifecycle(simulationVersion) {
		// V14 epochs/sources point at V12 bindings, V13 history, and runtime
		// facts. Clear this successor layer before the sealed predecessors so a
		// recovery run reconstructs the complete evidence chain in dependency
		// order.
		statements = append(statements,
			`DELETE FROM city_open_world_commute_lifecycle_cycle_metrics WHERE world_id = $1`,
			`DELETE FROM city_open_world_commute_lifecycle_sources WHERE world_id = $1`,
			`DELETE FROM city_open_world_commute_assignment_transitions WHERE world_id = $1`,
			`DELETE FROM city_open_world_commute_assignment_epochs WHERE world_id = $1`,
			`DELETE FROM city_open_world_commute_lifecycle_profiles WHERE world_id = $1`,
		)
	}
	if cityEngineSupportsOpenWorldCommuteSources(simulationVersion) {
		// V13 source rows depend on V12 bindings and runtime facts. Remove this
		// newest layer first so recovery can reconstruct the predecessor chain
		// without weakening any foreign-key or fact-provenance constraint.
		statements = append(statements,
			`DELETE FROM city_open_world_commute_cycle_metrics WHERE world_id = $1`,
			`DELETE FROM city_open_world_commute_sources WHERE world_id = $1`,
			`DELETE FROM city_open_world_commute_source_profiles WHERE world_id = $1`,
		)
	}
	if cityEngineSupportsOpenWorldCommuteBindings(simulationVersion) {
		// V12 bindings are the newest immutable social bridge and reference V5
		// actors/facilities plus V9 hubs. Clear them before their predecessor
		// profiles so recovery can rebuild the complete dependency chain.
		statements = append(statements,
			`DELETE FROM city_open_world_commute_bindings WHERE world_id = $1`,
			`DELETE FROM city_open_world_commute_profiles WHERE world_id = $1`,
		)
	}
	if cityEngineSupportsOpenWorldMobilityOD(simulationVersion) {
		// V11 source state references V5 actors/facilities, V9 topology and
		// runtime facts. Remove it before rebuilding any predecessor layer.
		statements = append(statements,
			`DELETE FROM city_open_world_mobility_od_cycle_metrics WHERE world_id = $1`,
			`DELETE FROM city_open_world_mobility_od_sources WHERE world_id = $1`,
			`DELETE FROM city_open_world_mobility_od_profiles WHERE world_id = $1`,
		)
	}
	if cityEngineSupportsOpenWorldArrivalBridge(simulationVersion) {
		// V10 arrivals reference V9 routes/demands and runtime facts. Remove the
		// bridge projection before rebuilding its older aggregate prerequisites.
		statements = append(statements,
			`DELETE FROM city_open_world_mobility_arrivals WHERE world_id = $1`,
			`DELETE FROM city_open_world_mobility_arrival_profiles WHERE world_id = $1`,
		)
	}
	if cityEngineSupportsOpenWorldMobility(simulationVersion) {
		// Dynamic mobility evidence owns references to runtime facts, actors,
		// facilities, and the V8 impact profile. Clear it first so recovery can
		// rebuild older projections in their dependency order.
		statements = append(statements,
			`DELETE FROM city_open_world_mobility_allocations WHERE world_id = $1`,
			`DELETE FROM city_open_world_mobility_actor_metrics WHERE world_id = $1`,
			// Routes and demands hold a deliberately deferred, checked cycle. Delete
			// the demand side first, then its routes; both references are absent by
			// transaction commit and PostgreSQL can validate the cycle intact.
			`DELETE FROM city_open_world_mobility_demands WHERE world_id = $1`,
			`DELETE FROM city_open_world_mobility_routes WHERE world_id = $1`,
			`DELETE FROM city_open_world_mobility_edges WHERE world_id = $1`,
			`DELETE FROM city_open_world_mobility_hubs WHERE world_id = $1`,
			`DELETE FROM city_open_world_mobility_modes WHERE world_id = $1`,
			`DELETE FROM city_open_world_mobility_profiles WHERE world_id = $1`,
		)
	}
	if cityEngineSupportsOpenWorldImpactBridge(simulationVersion) {
		statements = append(statements,
			`DELETE FROM city_open_world_impact_metrics WHERE world_id = $1`,
			`DELETE FROM city_open_world_impact_effects WHERE world_id = $1`,
			`DELETE FROM city_open_world_impact_catalog WHERE world_id = $1`,
			`DELETE FROM city_open_world_impact_profiles WHERE world_id = $1`,
		)
	}
	if cityEngineSupportsOpenWorldServiceCoordination(simulationVersion) {
		statements = append(statements,
			`DELETE FROM city_open_world_service_responses WHERE world_id = $1`,
			`DELETE FROM city_open_world_service_requests WHERE world_id = $1`,
			`DELETE FROM city_open_world_service_providers WHERE world_id = $1`,
			`DELETE FROM city_open_world_service_catalog WHERE world_id = $1`,
			`DELETE FROM city_open_world_service_profiles WHERE world_id = $1`,
		)
	}
	if cityEngineSupportsOpenWorldSocialRuntime(simulationVersion) {
		statements = append(statements,
			`DELETE FROM city_open_world_actor_navigation_intents WHERE world_id = $1`,
			`DELETE FROM city_open_world_npc_profiles WHERE world_id = $1`,
			`DELETE FROM city_open_world_facilities WHERE world_id = $1`,
			`DELETE FROM city_open_world_scenario_bindings WHERE world_id = $1`,
		)
	}
	statements = append(statements,
		`DELETE FROM city_open_world_runtime_effects WHERE world_id = $1`,
		`DELETE FROM city_open_world_rule_cases WHERE world_id = $1`,
		`DELETE FROM city_open_world_portal_states WHERE world_id = $1`,
		`DELETE FROM city_open_world_actor_controls WHERE world_id = $1`,
		`DELETE FROM city_open_world_actor_locations WHERE world_id = $1`,
		`DELETE FROM city_open_world_actor_statuses WHERE world_id = $1`,
		`DELETE FROM city_open_world_actor_roles WHERE world_id = $1`,
		`DELETE FROM city_open_world_actor_attributes WHERE world_id = $1`,
		`DELETE FROM city_open_world_runtime_facts WHERE world_id = $1`,
		`DELETE FROM city_open_world_actors WHERE world_id = $1`,
		`DELETE FROM city_open_world_runtime_definitions WHERE world_id = $1`,
		`DELETE FROM city_open_world_runtime_profiles WHERE world_id = $1`,
	)
	count := 0
	for _, statement := range statements {
		result, err := tx.ExecContext(ctx, statement, worldID)
		if err != nil {
			return count, fmt.Errorf("clear open-world runtime projection: %w", err)
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return count, err
		}
		count += int(rows)
	}
	return count, nil
}

func validateCityOpenWorldRuntimeRecoveryState(
	simulationVersion string,
	runtime *cityOpenWorldRuntimeHashState,
) error {
	if runtime == nil || !cityEngineSupportsOpenWorldRuntime(simulationVersion) {
		return fmt.Errorf("open-world runtime recovery state is unavailable")
	}
	runtimeID, runtimeVersion, catalogVersion, err := cityOpenWorldRuntimeProfileIdentity(simulationVersion)
	if err != nil {
		return err
	}
	profile := runtime.Profile
	if profile.RuntimeID != runtimeID || profile.RuntimeVersion != runtimeVersion ||
		profile.CatalogVersion != catalogVersion || profile.ActorCount != int64(len(runtime.Actors)) ||
		profile.FactCount != int64(len(runtime.Facts)) || profile.EffectCount != int64(len(runtime.Effects)) ||
		profile.CaseCount != int64(len(runtime.RuleCases)) || profile.Revision != int64(len(runtime.Facts))+1 {
		return fmt.Errorf("open-world runtime recovery profile is inconsistent")
	}
	if cityEngineSupportsOpenWorldSocialRuntime(simulationVersion) {
		if runtime.Social == nil {
			return fmt.Errorf("open-world social recovery state is unavailable")
		}
	} else if runtime.Social != nil {
		return fmt.Errorf("legacy open-world recovery state contains social data")
	}
	if cityEngineSupportsOpenWorldServiceCoordination(simulationVersion) {
		if runtime.Services == nil {
			return fmt.Errorf("open-world service recovery state is unavailable")
		}
	} else if runtime.Services != nil {
		return fmt.Errorf("pre-V7 open-world recovery state contains service data")
	}
	if cityEngineSupportsOpenWorldImpactBridge(simulationVersion) {
		if runtime.Impacts == nil {
			return fmt.Errorf("open-world impact recovery state is unavailable")
		}
		if err := validateCityOpenWorldImpactState(runtime.Impacts); err != nil {
			return fmt.Errorf("open-world impact recovery state is invalid: %w", err)
		}
	} else if runtime.Impacts != nil {
		return fmt.Errorf("pre-V8 open-world recovery state contains impact data")
	}
	if cityEngineSupportsOpenWorldMobility(simulationVersion) {
		if runtime.Mobility == nil {
			return fmt.Errorf("open-world mobility recovery state is unavailable")
		}
		if err := validateCityOpenWorldMobilityState(runtime.Mobility); err != nil {
			return fmt.Errorf("open-world mobility recovery state is invalid: %w", err)
		}
	} else if runtime.Mobility != nil {
		return fmt.Errorf("pre-V9 open-world recovery state contains mobility data")
	}
	if cityEngineSupportsOpenWorldArrivalBridge(simulationVersion) {
		if runtime.Arrivals == nil {
			return fmt.Errorf("open-world arrival recovery state is unavailable")
		}
		if err := validateCityOpenWorldMobilityArrivalState(runtime.Arrivals); err != nil {
			return fmt.Errorf("open-world arrival recovery state is invalid: %w", err)
		}
	} else if runtime.Arrivals != nil {
		return fmt.Errorf("pre-V10 open-world recovery state contains arrival data")
	}
	if cityEngineSupportsOpenWorldMobilityOD(simulationVersion) {
		if runtime.OD == nil {
			return fmt.Errorf("open-world V11 OD recovery state is unavailable")
		}
		if err := validateCityOpenWorldMobilityODState(runtime.OD); err != nil {
			return fmt.Errorf("open-world V11 OD recovery state is invalid: %w", err)
		}
	} else if runtime.OD != nil {
		return fmt.Errorf("pre-V11 open-world recovery state contains OD data")
	}
	if cityEngineSupportsOpenWorldCommuteBindings(simulationVersion) {
		if runtime.Commutes == nil {
			return fmt.Errorf("open-world V12 commute recovery state is unavailable")
		}
		if err := validateCityOpenWorldCommuteState(runtime.Commutes); err != nil {
			return fmt.Errorf("open-world V12 commute recovery state is invalid: %w", err)
		}
	} else if runtime.Commutes != nil {
		return fmt.Errorf("pre-V12 open-world recovery state contains commute data")
	}
	if cityEngineSupportsOpenWorldCommuteSources(simulationVersion) {
		if runtime.CommuteSources == nil {
			return fmt.Errorf("open-world V13 commute source recovery state is unavailable")
		}
		if err := validateCityOpenWorldCommuteSourceState(runtime.CommuteSources); err != nil {
			return fmt.Errorf("open-world V13 commute source recovery state is invalid: %w", err)
		}
	} else if runtime.CommuteSources != nil {
		return fmt.Errorf("pre-V13 open-world recovery state contains commute source data")
	}
	if cityEngineSupportsOpenWorldCommuteLifecycle(simulationVersion) {
		if runtime.CommuteLifecycle == nil {
			return fmt.Errorf("open-world V14 commute lifecycle recovery state is unavailable")
		}
		if err := validateCityOpenWorldCommuteLifecycleState(runtime.CommuteLifecycle); err != nil {
			return fmt.Errorf("open-world V14 commute lifecycle recovery state is invalid: %w", err)
		}
	} else if runtime.CommuteLifecycle != nil {
		return fmt.Errorf("pre-V14 open-world recovery state contains commute lifecycle data")
	}
	if cityEngineSupportsOpenWorldSupplyChain(simulationVersion) {
		if runtime.SupplyChain == nil {
			return fmt.Errorf("open-world V15 supply-chain recovery state is unavailable")
		}
		if err := validateCityOpenWorldSupplyChainState(runtime.SupplyChain); err != nil {
			return fmt.Errorf("open-world V15 supply-chain recovery state is invalid: %w", err)
		}
	} else if runtime.SupplyChain != nil {
		return fmt.Errorf("pre-V15 open-world recovery state contains supply-chain data")
	}
	if cityEngineSupportsOpenWorldEnterpriseFreight(simulationVersion) {
		if runtime.EnterpriseFreight == nil {
			return fmt.Errorf("open-world V16 enterprise-freight recovery state is unavailable")
		}
		if err := validateCityOpenWorldEnterpriseFreightState(runtime.EnterpriseFreight); err != nil {
			return fmt.Errorf("open-world V16 enterprise-freight recovery state is invalid: %w", err)
		}
	} else if runtime.EnterpriseFreight != nil {
		return fmt.Errorf("pre-V16 open-world recovery state contains enterprise-freight data")
	}
	if cityEngineSupportsOpenWorldEnterpriseFreightReceipts(simulationVersion) {
		if runtime.EnterpriseFreightReceipts == nil {
			return fmt.Errorf("open-world V17 freight-receipt recovery state is unavailable")
		}
		if err := validateCityOpenWorldEnterpriseFreightReceiptState(runtime.EnterpriseFreightReceipts); err != nil {
			return fmt.Errorf("open-world V17 freight-receipt recovery state is invalid: %w", err)
		}
	} else if runtime.EnterpriseFreightReceipts != nil {
		return fmt.Errorf("pre-V17 open-world recovery state contains freight-receipt data")
	}
	if cityEngineSupportsOpenWorldFreightBatches(simulationVersion) {
		if runtime.EnterpriseFreightBatches == nil {
			return fmt.Errorf("open-world V18 freight-batch recovery state is unavailable")
		}
		if err := validateCityOpenWorldFreightBatchState(runtime.EnterpriseFreightBatches); err != nil {
			return fmt.Errorf("open-world V18 freight-batch recovery state is invalid: %w", err)
		}
	} else if runtime.EnterpriseFreightBatches != nil {
		return fmt.Errorf("pre-V18 open-world recovery state contains freight-batch data")
	}
	if cityEngineSupportsOpenWorldSpatialNetwork(simulationVersion) {
		if runtime.SpatialNetwork == nil {
			return fmt.Errorf("open-world V19 spatial-network recovery state is unavailable")
		}
		if err := validateCityOpenWorldSpatialNetworkState(runtime.SpatialNetwork); err != nil {
			return fmt.Errorf("open-world V19 spatial-network recovery state is invalid: %w", err)
		}
	} else if runtime.SpatialNetwork != nil {
		return fmt.Errorf("pre-V19 open-world recovery state contains spatial-network data")
	}
	if cityEngineSupportsOpenWorldInfrastructure(simulationVersion) {
		if runtime.Infrastructure == nil {
			return fmt.Errorf("open-world V20 infrastructure recovery state is unavailable")
		}
		if err := validateCityOpenWorldInfrastructureState(runtime.Infrastructure); err != nil {
			return fmt.Errorf("open-world V20 infrastructure recovery state is invalid: %w", err)
		}
	} else if runtime.Infrastructure != nil {
		return fmt.Errorf("pre-V20 open-world recovery state contains infrastructure data")
	}
	if cityEngineSupportsOpenWorldEffectiveCapacity(simulationVersion) {
		if runtime.EffectiveCapacity == nil {
			return fmt.Errorf("open-world V21 effective-capacity recovery state is unavailable")
		}
		if err := validateCityOpenWorldEffectiveCapacityRuntimeState(runtime); err != nil {
			return fmt.Errorf("open-world V21 effective-capacity recovery state is invalid: %w", err)
		}
	} else if runtime.EffectiveCapacity != nil {
		return fmt.Errorf("pre-V21 open-world recovery state contains effective-capacity data")
	}
	if cityEngineSupportsOpenWorldFreightSettlements(simulationVersion) {
		if runtime.FreightSettlements == nil {
			return fmt.Errorf("open-world V22 freight-settlement recovery state is unavailable")
		}
		if err := validateCityOpenWorldFreightSettlementState(runtime.FreightSettlements); err != nil {
			return fmt.Errorf("open-world V22 freight-settlement recovery state is invalid: %w", err)
		}
	} else if runtime.FreightSettlements != nil {
		return fmt.Errorf("pre-V22 open-world recovery state contains freight-settlement data")
	}
	if cityEngineSupportsOpenWorldCarrierRecovery(simulationVersion) {
		if runtime.CarrierRecovery == nil {
			return fmt.Errorf("open-world V23 carrier-recovery state is unavailable")
		}
		if err := validateCityOpenWorldCarrierRecoveryState(runtime.CarrierRecovery); err != nil {
			return fmt.Errorf("open-world V23 carrier-recovery state is invalid: %w", err)
		}
	} else if runtime.CarrierRecovery != nil {
		return fmt.Errorf("pre-V23 open-world recovery state contains carrier-recovery data")
	}
	if cityEngineSupportsOpenWorldCarrierCommerce(simulationVersion) {
		if runtime.CarrierCommerce == nil {
			return fmt.Errorf("open-world V24 carrier-commerce recovery state is unavailable")
		}
		if err := validateCityOpenWorldCarrierCommerceState(runtime.CarrierCommerce); err != nil {
			return fmt.Errorf("open-world V24 carrier-commerce recovery state is invalid: %w", err)
		}
	} else if runtime.CarrierCommerce != nil {
		return fmt.Errorf("pre-V24 open-world recovery state contains carrier-commerce data")
	}
	return nil
}

func loadCityOpenWorldRecoveryCommandIDs(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (map[int64]int64, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT sequence, id
FROM city_commands
WHERE world_id = $1`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load open-world recovery commands: %w", err)
	}
	defer func() { _ = rows.Close() }()
	ids := make(map[int64]int64)
	for rows.Next() {
		var sequence, id int64
		if err = rows.Scan(&sequence, &id); err != nil {
			return nil, fmt.Errorf("scan open-world recovery command: %w", err)
		}
		ids[sequence] = id
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate open-world recovery commands: %w", err)
	}
	return ids, nil
}

func requireCityOpenWorldRecoveryActorID(actorIDs map[string]int64, code string) (int64, error) {
	id, found := actorIDs[code]
	if !found || id <= 0 {
		return 0, fmt.Errorf("unknown actor %s", code)
	}
	return id, nil
}

func requireCityOpenWorldRecoveryFactID(
	factIDs map[cityOpenWorldRuntimeRecoveryIdentity]int64,
	reference CityOpenWorldRuntimeFactRef,
) (int64, error) {
	id, found := factIDs[cityOpenWorldRuntimeRecoveryIdentity{tick: reference.Tick, sequence: reference.Sequence}]
	if !found || id <= 0 {
		return 0, fmt.Errorf("unknown fact %d/%d", reference.Tick, reference.Sequence)
	}
	return id, nil
}

func resolveOptionalCityOpenWorldRecoveryFactID(
	factIDs map[cityOpenWorldRuntimeRecoveryIdentity]int64,
	reference *CityOpenWorldRuntimeFactRef,
) (any, error) {
	if reference == nil {
		return nil, nil
	}
	id, err := requireCityOpenWorldRecoveryFactID(factIDs, *reference)
	if err != nil {
		return nil, err
	}
	return id, nil
}

func cityOpenWorldRecoveryNullableInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func restoreCityOpenWorldRuntimeProjection(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	simulationVersion string,
	runtime *cityOpenWorldRuntimeHashState,
) (int, error) {
	if err := validateCityOpenWorldRuntimeRecoveryState(simulationVersion, runtime); err != nil {
		return 0, err
	}
	preserved, err := loadCityOpenWorldRuntimeRecoveryIDs(ctx, tx, worldID)
	if err != nil {
		return 0, err
	}
	count, err := clearCityOpenWorldRuntimeProjection(ctx, tx, worldID, simulationVersion)
	if err != nil {
		return 0, err
	}
	commandIDs, err := loadCityOpenWorldRecoveryCommandIDs(ctx, tx, worldID)
	if err != nil {
		return count, err
	}
	profile := runtime.Profile
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_runtime_profiles
    (world_id, runtime_id, runtime_version, catalog_version, catalog_hash,
     baseline_tick, maximum_player_actors_per_member, actor_count, fact_count,
     effect_count, case_count, revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13::jsonb)`,
		worldID, profile.RuntimeID, profile.RuntimeVersion, profile.CatalogVersion,
		profile.CatalogHash, profile.BaselineTick, profile.MaximumPlayerActorsPerMember,
		profile.ActorCount, profile.FactCount, profile.EffectCount, profile.CaseCount,
		profile.Revision, []byte(profile.Metadata)); err != nil {
		return count, fmt.Errorf("restore open-world runtime profile: %w", err)
	}
	count++
	for _, definition := range runtime.Definitions {
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_runtime_definitions
    (world_id, definition_kind, code, definition_version, content_hash, visibility, payload)
VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)`,
			worldID, definition.Kind, definition.Code, definition.Version, definition.Hash,
			definition.Visibility, []byte(definition.Payload)); err != nil {
			return count, fmt.Errorf("restore open-world runtime definition %s/%s: %w", definition.Kind, definition.Code, err)
		}
		count++
	}
	actorIDs := make(map[string]int64, len(runtime.Actors))
	for _, actor := range runtime.Actors {
		var actorID int64
		if err = tx.QueryRowContext(ctx, `
INSERT INTO city_open_world_actors
    (world_id, code, owner_user_id, actor_type_code, name, status,
     archetype_code, archetype_version, created_tick, updated_tick, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb)
RETURNING id`,
			worldID, actor.Code, cityNullableInt64(actor.OwnerUserID), actor.ActorTypeCode,
			actor.Name, actor.Status, nullableStringValue(actor.ArchetypeCode),
			nullableStringValue(actor.ArchetypeVersion), actor.CreatedTick, actor.UpdatedTick,
			actor.Version, []byte(actor.Metadata)).Scan(&actorID); err != nil {
			return count, fmt.Errorf("restore open-world actor %s: %w", actor.Code, err)
		}
		actorIDs[actor.Code] = actorID
		count++
	}
	for _, attribute := range runtime.Attributes {
		actorID, actorErr := requireCityOpenWorldRecoveryActorID(actorIDs, attribute.ActorCode)
		if actorErr != nil {
			return count, fmt.Errorf("restore open-world actor attribute %s/%s: %w", attribute.ActorCode, attribute.AttributeCode, actorErr)
		}
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_actor_attributes
    (world_id, actor_id, attribute_code, value_units, experience_units,
     last_changed_tick, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb)`,
			worldID, actorID, attribute.AttributeCode, attribute.ValueUnits,
			attribute.ExperienceUnits, attribute.LastChangedTick, attribute.Version,
			[]byte(attribute.Metadata)); err != nil {
			return count, fmt.Errorf("restore open-world actor attribute %s/%s: %w", attribute.ActorCode, attribute.AttributeCode, err)
		}
		count++
	}
	for _, role := range runtime.Roles {
		actorID, actorErr := requireCityOpenWorldRecoveryActorID(actorIDs, role.ActorCode)
		if actorErr != nil {
			return count, fmt.Errorf("restore open-world actor role %s/%s: %w", role.ActorCode, role.RoleCode, actorErr)
		}
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_actor_roles
    (world_id, actor_id, role_code, category_code, status, granted_tick,
     revoked_tick, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb)`,
			worldID, actorID, role.RoleCode, role.CategoryCode, role.Status,
			role.GrantedTick, cityNullableInt64(role.RevokedTick), role.Version,
			[]byte(role.Metadata)); err != nil {
			return count, fmt.Errorf("restore open-world actor role %s/%s: %w", role.ActorCode, role.RoleCode, err)
		}
		count++
	}
	factIDs := make(map[cityOpenWorldRuntimeRecoveryIdentity]int64, len(runtime.Facts))
	for _, fact := range runtime.Facts {
		identity := cityOpenWorldRuntimeRecoveryIdentity{tick: fact.Tick, sequence: fact.Sequence}
		var sourceCommandID, parentFactID, actorID any
		if fact.SourceCommandSequence != nil {
			resolved, found := commandIDs[*fact.SourceCommandSequence]
			if !found || resolved <= 0 {
				return count, fmt.Errorf("restore open-world fact %d/%d references unknown command sequence %d", fact.Tick, fact.Sequence, *fact.SourceCommandSequence)
			}
			sourceCommandID = resolved
		}
		if fact.Parent != nil {
			resolved, factErr := requireCityOpenWorldRecoveryFactID(factIDs, *fact.Parent)
			if factErr != nil {
				return count, fmt.Errorf("restore open-world fact %d/%d parent: %w", fact.Tick, fact.Sequence, factErr)
			}
			parentFactID = resolved
		}
		if fact.ActorCode != nil {
			resolved, actorErr := requireCityOpenWorldRecoveryActorID(actorIDs, *fact.ActorCode)
			if actorErr != nil {
				return count, fmt.Errorf("restore open-world fact %d/%d actor: %w", fact.Tick, fact.Sequence, actorErr)
			}
			actorID = resolved
		}
		query := `
INSERT INTO city_open_world_runtime_facts
    (world_id, tick, sequence, source_command_id, parent_fact_id, actor_id,
     fact_type, definition_kind, definition_code, definition_version,
     definition_hash, payload, posted_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb, NOW())
RETURNING id`
		args := []any{worldID, fact.Tick, fact.Sequence, sourceCommandID, parentFactID, actorID,
			fact.FactType, nullableStringValue(fact.DefinitionKind), nullableStringValue(fact.DefinitionCode),
			nullableStringValue(fact.DefinitionVersion), nullableStringValue(fact.DefinitionHash), []byte(fact.Payload)}
		if preservedID := preserved.facts[identity]; preservedID > 0 {
			query = `
INSERT INTO city_open_world_runtime_facts
    (id, world_id, tick, sequence, source_command_id, parent_fact_id, actor_id,
     fact_type, definition_kind, definition_code, definition_version,
     definition_hash, payload, posted_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13::jsonb, NOW())
RETURNING id`
			args = append([]any{preservedID}, args...)
		}
		var id int64
		if err = tx.QueryRowContext(ctx, query, args...).Scan(&id); err != nil {
			return count, fmt.Errorf("restore open-world fact %d/%d: %w", fact.Tick, fact.Sequence, err)
		}
		factIDs[identity] = id
		count++
	}
	for _, location := range runtime.Locations {
		actorID, actorErr := requireCityOpenWorldRecoveryActorID(actorIDs, location.ActorCode)
		if actorErr != nil {
			return count, fmt.Errorf("restore open-world location %s: %w", location.ActorCode, actorErr)
		}
		sourceFactID, factErr := resolveOptionalCityOpenWorldRecoveryFactID(factIDs, location.SourceFact)
		if factErr != nil {
			return count, fmt.Errorf("restore open-world location %s: %w", location.ActorCode, factErr)
		}
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_actor_locations
    (world_id, actor_id, space_kind, location_scope, building_code, floor_index,
     x, y, z, sector_x, sector_y, chunk_x, chunk_y, local_x, local_y,
     moved_tick, source_fact_id, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14,
        $15, $16, $17, $18, $19::jsonb)`,
			worldID, actorID, location.SpaceKind, location.LocationScope,
			nullableStringValue(location.BuildingCode), location.FloorIndex, location.X, location.Y,
			location.Z, location.SectorX, location.SectorY, location.ChunkX, location.ChunkY,
			location.LocalX, location.LocalY, location.MovedTick, sourceFactID,
			location.Version, []byte(location.Metadata)); err != nil {
			return count, fmt.Errorf("restore open-world location %s: %w", location.ActorCode, err)
		}
		count++
	}
	for _, grant := range runtime.ControlGrants {
		actorID, actorErr := requireCityOpenWorldRecoveryActorID(actorIDs, grant.ActorCode)
		if actorErr != nil {
			return count, fmt.Errorf("restore open-world control grant %s: %w", grant.Code, actorErr)
		}
		grantFactID, factErr := resolveOptionalCityOpenWorldRecoveryFactID(factIDs, grant.GrantSourceFact)
		if factErr != nil {
			return count, fmt.Errorf("restore open-world control grant %s: %w", grant.Code, factErr)
		}
		revokeFactID, factErr := resolveOptionalCityOpenWorldRecoveryFactID(factIDs, grant.RevokeSourceFact)
		if factErr != nil {
			return count, fmt.Errorf("restore open-world control grant %s: %w", grant.Code, factErr)
		}
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_actor_controls
    (world_id, code, actor_id, user_id, capability, status, granted_by_user_id,
     granted_tick, revoked_tick, grant_source_fact_id, revoke_source_fact_id,
     version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13::jsonb)`,
			worldID, grant.Code, actorID, grant.UserID, grant.Capability, grant.Status,
			grant.GrantedByUserID, grant.GrantedTick, cityNullableInt64(grant.RevokedTick),
			grantFactID, revokeFactID, grant.Version, []byte(grant.Metadata)); err != nil {
			return count, fmt.Errorf("restore open-world control grant %s: %w", grant.Code, err)
		}
		count++
	}
	for _, portalState := range runtime.PortalStates {
		if portalState.SourceFact == nil {
			return count, fmt.Errorf("restore open-world portal state %s has no source fact", portalState.PortalCode)
		}
		sourceFactID, factErr := requireCityOpenWorldRecoveryFactID(factIDs, *portalState.SourceFact)
		if factErr != nil {
			return count, fmt.Errorf("restore open-world portal state %s: %w", portalState.PortalCode, factErr)
		}
		canonicalRequirement, requirementRaw, policyHash, requirementErr := canonicalWorldPortalAccessRequirement(portalState.AccessRequirement)
		if requirementErr != nil || policyHash != portalState.AccessPolicyHash {
			return count, fmt.Errorf("restore open-world portal state %s has invalid access policy", portalState.PortalCode)
		}
		portalState.AccessRequirement = canonicalRequirement
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_portal_states
    (world_id, portal_code, state_code, access_requirement, access_policy_hash,
     changed_tick, source_fact_id, version, metadata)
VALUES ($1, $2, $3, $4::jsonb, $5, $6, $7, $8, $9::jsonb)`,
			worldID, portalState.PortalCode, portalState.StateCode, []byte(requirementRaw),
			portalState.AccessPolicyHash, portalState.ChangedTick, sourceFactID,
			portalState.Version, []byte(portalState.Metadata)); err != nil {
			return count, fmt.Errorf("restore open-world portal state %s: %w", portalState.PortalCode, err)
		}
		count++
	}
	for _, status := range runtime.Statuses {
		actorID, actorErr := requireCityOpenWorldRecoveryActorID(actorIDs, status.ActorCode)
		if actorErr != nil {
			return count, fmt.Errorf("restore open-world actor status %s: %w", status.InstanceCode, actorErr)
		}
		sourceFactID, factErr := resolveOptionalCityOpenWorldRecoveryFactID(factIDs, status.SourceFact)
		if factErr != nil {
			return count, fmt.Errorf("restore open-world actor status %s: %w", status.InstanceCode, factErr)
		}
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_actor_statuses
    (world_id, actor_id, instance_code, status_code, lifecycle_status,
     intensity_units, stacks, granted_tick, expires_tick, ended_tick,
     source_fact_id, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13::jsonb)`,
			worldID, actorID, status.InstanceCode, status.StatusCode, status.Lifecycle,
			status.IntensityUnits, status.Stacks, status.GrantedTick,
			cityNullableInt64(status.ExpiresTick), cityNullableInt64(status.EndedTick),
			sourceFactID, status.Version, []byte(status.Metadata)); err != nil {
			return count, fmt.Errorf("restore open-world actor status %s: %w", status.InstanceCode, err)
		}
		count++
	}
	for _, effect := range runtime.Effects {
		sourceFactID, factErr := requireCityOpenWorldRecoveryFactID(factIDs, effect.SourceFact)
		if factErr != nil {
			return count, fmt.Errorf("restore open-world effect %d/%d: %w", effect.Tick, effect.Sequence, factErr)
		}
		var targetActorID any
		if effect.TargetActorCode != nil {
			resolved, actorErr := requireCityOpenWorldRecoveryActorID(actorIDs, *effect.TargetActorCode)
			if actorErr != nil {
				return count, fmt.Errorf("restore open-world effect %d/%d: %w", effect.Tick, effect.Sequence, actorErr)
			}
			targetActorID = resolved
		}
		identity := cityOpenWorldRuntimeRecoveryIdentity{tick: effect.Tick, sequence: effect.Sequence}
		query := `
INSERT INTO city_open_world_runtime_effects
    (world_id, tick, sequence, source_fact_id, operation_index, effect_type,
     target_actor_id, target_key, before_units, delta_units, after_units, payload)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb)`
		args := []any{worldID, effect.Tick, effect.Sequence, sourceFactID, effect.OperationIndex,
			effect.EffectType, targetActorID, nullableStringValue(effect.TargetKey),
			cityNullableInt64(effect.BeforeUnits), cityNullableInt64(effect.DeltaUnits),
			cityNullableInt64(effect.AfterUnits), []byte(effect.Payload)}
		if preservedID := preserved.effects[identity]; preservedID > 0 {
			query = `
INSERT INTO city_open_world_runtime_effects
    (id, world_id, tick, sequence, source_fact_id, operation_index, effect_type,
     target_actor_id, target_key, before_units, delta_units, after_units, payload)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13::jsonb)`
			args = append([]any{preservedID}, args...)
		}
		if _, err = tx.ExecContext(ctx, query, args...); err != nil {
			return count, fmt.Errorf("restore open-world effect %d/%d: %w", effect.Tick, effect.Sequence, err)
		}
		count++
	}
	for _, ruleCase := range runtime.RuleCases {
		sourceFactID, factErr := requireCityOpenWorldRecoveryFactID(factIDs, ruleCase.SourceFact)
		if factErr != nil {
			return count, fmt.Errorf("restore open-world rule case %s: %w", ruleCase.Code, factErr)
		}
		consequenceFactID, factErr := resolveOptionalCityOpenWorldRecoveryFactID(factIDs, ruleCase.ConsequenceFact)
		if factErr != nil {
			return count, fmt.Errorf("restore open-world rule case %s: %w", ruleCase.Code, factErr)
		}
		subjectActorID, actorErr := requireCityOpenWorldRecoveryActorID(actorIDs, ruleCase.SubjectActorCode)
		if actorErr != nil {
			return count, fmt.Errorf("restore open-world rule case %s: %w", ruleCase.Code, actorErr)
		}
		identity := cityOpenWorldRuntimeRecoveryIdentity{tick: ruleCase.Tick, sequence: ruleCase.Sequence}
		query := `
INSERT INTO city_open_world_rule_cases
    (world_id, code, tick, sequence, source_fact_id, consequence_fact_id,
     subject_actor_id, rule_code, rule_version, rule_hash, category_code,
     scope_kind, scope_code, status, severity_units, decision_code,
     created_tick, decided_tick, closed_tick, payload)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
        $14, $15, $16, $17, $18, $19, $20::jsonb)`
		args := []any{worldID, ruleCase.Code, ruleCase.Tick, ruleCase.Sequence,
			sourceFactID, consequenceFactID, subjectActorID, ruleCase.RuleCode,
			ruleCase.RuleVersion, ruleCase.RuleHash, ruleCase.CategoryCode,
			ruleCase.ScopeKind, ruleCase.ScopeCode, ruleCase.Status, ruleCase.SeverityUnits,
			nullableStringValue(ruleCase.DecisionCode), ruleCase.CreatedTick,
			cityNullableInt64(ruleCase.DecidedTick), cityNullableInt64(ruleCase.ClosedTick), []byte(ruleCase.Payload)}
		if preservedID := preserved.cases[identity]; preservedID > 0 {
			query = `
INSERT INTO city_open_world_rule_cases
    (id, world_id, code, tick, sequence, source_fact_id, consequence_fact_id,
     subject_actor_id, rule_code, rule_version, rule_hash, category_code,
     scope_kind, scope_code, status, severity_units, decision_code,
     created_tick, decided_tick, closed_tick, payload)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
        $14, $15, $16, $17, $18, $19, $20, $21::jsonb)`
			args = append([]any{preservedID}, args...)
		}
		if _, err = tx.ExecContext(ctx, query, args...); err != nil {
			return count, fmt.Errorf("restore open-world rule case %s: %w", ruleCase.Code, err)
		}
		count++
	}
	var facilityIDs map[string]int64
	if cityEngineSupportsOpenWorldSocialRuntime(simulationVersion) {
		socialCount, restoredFacilityIDs, socialErr := restoreCityOpenWorldSocialRuntimeProjection(
			ctx, tx, worldID, *runtime.Social, actorIDs, factIDs,
		)
		if socialErr != nil {
			return count, socialErr
		}
		facilityIDs = restoredFacilityIDs
		count += socialCount
	}
	if cityEngineSupportsOpenWorldServiceCoordination(simulationVersion) {
		serviceCount, responseIDs, serviceErr := restoreCityOpenWorldServiceProjection(
			ctx, tx, worldID, *runtime.Services, actorIDs, facilityIDs, factIDs,
		)
		if serviceErr != nil {
			return count, serviceErr
		}
		count += serviceCount
		if _, err = tx.ExecContext(ctx, `SELECT assert_city_open_world_service_foundation($1)`, worldID); err != nil {
			return count, fmt.Errorf("validate restored open-world V7 service foundation: %w", err)
		}
		if cityEngineSupportsOpenWorldImpactBridge(simulationVersion) {
			impactCount, impactErr := restoreCityOpenWorldImpactProjection(
				ctx, tx, worldID, *runtime.Impacts, actorIDs, factIDs, responseIDs,
			)
			if impactErr != nil {
				return count, impactErr
			}
			count += impactCount
			if _, err = tx.ExecContext(ctx, `SELECT assert_city_open_world_impact_foundation($1)`, worldID); err != nil {
				return count, fmt.Errorf("validate restored open-world V8 impact foundation: %w", err)
			}
		}
		if cityEngineSupportsOpenWorldMobility(simulationVersion) {
			mobilityCount, mobilityErr := restoreCityOpenWorldMobilityProjection(
				ctx, tx, worldID, *runtime.Mobility, actorIDs, facilityIDs, factIDs,
			)
			if mobilityErr != nil {
				return count, mobilityErr
			}
			count += mobilityCount
			if _, err = tx.ExecContext(ctx, `SELECT assert_city_open_world_mobility_foundation($1)`, worldID); err != nil {
				return count, fmt.Errorf("validate restored open-world V9 mobility foundation: %w", err)
			}
		}
		if cityEngineSupportsOpenWorldArrivalBridge(simulationVersion) {
			arrivalCount, arrivalErr := restoreCityOpenWorldMobilityArrivalProjection(
				ctx, tx, worldID, *runtime.Arrivals, factIDs,
			)
			if arrivalErr != nil {
				return count, arrivalErr
			}
			count += arrivalCount
			if _, err = tx.ExecContext(ctx, `SELECT assert_city_open_world_arrival_foundation($1)`, worldID); err != nil {
				return count, fmt.Errorf("validate restored open-world V10 arrival foundation: %w", err)
			}
		}
		if cityEngineSupportsOpenWorldMobilityOD(simulationVersion) {
			odCount, odErr := restoreCityOpenWorldMobilityODProjection(
				ctx, tx, worldID, *runtime.OD, actorIDs, factIDs,
			)
			if odErr != nil {
				return count, odErr
			}
			count += odCount
			if _, err = tx.ExecContext(ctx, `SELECT assert_city_open_world_mobility_od_foundation($1)`, worldID); err != nil {
				return count, fmt.Errorf("validate restored open-world V11 OD foundation: %w", err)
			}
		}
		if cityEngineSupportsOpenWorldCommuteBindings(simulationVersion) {
			commuteCount, commuteErr := restoreCityOpenWorldCommuteProjection(
				ctx, tx, worldID, *runtime.Commutes, actorIDs,
			)
			if commuteErr != nil {
				return count, commuteErr
			}
			count += commuteCount
			if _, err = tx.ExecContext(ctx, `SELECT assert_city_open_world_commute_foundation($1)`, worldID); err != nil {
				return count, fmt.Errorf("validate restored open-world V12 commute foundation: %w", err)
			}
		}
		if cityEngineSupportsOpenWorldCommuteSources(simulationVersion) {
			sourceCount, sourceErr := restoreCityOpenWorldCommuteSourceProjection(
				ctx, tx, worldID, *runtime.CommuteSources, actorIDs, factIDs,
			)
			if sourceErr != nil {
				return count, sourceErr
			}
			count += sourceCount
			if _, err = tx.ExecContext(ctx, `SELECT assert_city_open_world_commute_source_foundation($1)`, worldID); err != nil {
				return count, fmt.Errorf("validate restored open-world V13 commute source foundation: %w", err)
			}
		}
		if cityEngineSupportsOpenWorldCommuteLifecycle(simulationVersion) {
			lifecycleCount, lifecycleErr := restoreCityOpenWorldCommuteLifecycleProjection(
				ctx, tx, worldID, *runtime.CommuteLifecycle, actorIDs, factIDs,
			)
			if lifecycleErr != nil {
				return count, lifecycleErr
			}
			count += lifecycleCount
			if err = assertCityOpenWorldCommuteLifecycleFoundation(ctx, tx, worldID); err != nil {
				return count, fmt.Errorf("validate restored open-world V14 commute lifecycle foundation: %w", err)
			}
		}
		if cityEngineSupportsOpenWorldSupplyChain(simulationVersion) {
			supplyChainCount, supplyChainErr := restoreCityOpenWorldSupplyChainProjection(
				ctx, tx, worldID, *runtime.SupplyChain, commandIDs,
			)
			if supplyChainErr != nil {
				return count, supplyChainErr
			}
			count += supplyChainCount
		}
		if cityEngineSupportsOpenWorldEnterpriseFreight(simulationVersion) {
			freightCount, freightErr := restoreCityOpenWorldEnterpriseFreightProjection(
				ctx, tx, worldID, *runtime.EnterpriseFreight,
			)
			if freightErr != nil {
				return count, freightErr
			}
			count += freightCount
		}
		if cityEngineSupportsOpenWorldEnterpriseFreightReceipts(simulationVersion) {
			receiptCount, receiptErr := restoreCityOpenWorldEnterpriseFreightReceiptProjection(
				ctx, tx, worldID, *runtime.EnterpriseFreightReceipts,
			)
			if receiptErr != nil {
				return count, receiptErr
			}
			count += receiptCount
		}
		if cityEngineSupportsOpenWorldFreightBatches(simulationVersion) {
			batchCount, batchErr := restoreCityOpenWorldFreightBatchProjection(
				ctx, tx, worldID, *runtime.EnterpriseFreightBatches,
			)
			if batchErr != nil {
				return count, batchErr
			}
			count += batchCount
		}
		if cityEngineSupportsOpenWorldSpatialNetwork(simulationVersion) {
			networkCount, networkErr := restoreCityOpenWorldSpatialNetworkProjection(
				ctx, tx, worldID, *runtime.SpatialNetwork,
			)
			if networkErr != nil {
				return count, networkErr
			}
			count += networkCount
		}
		if cityEngineSupportsOpenWorldInfrastructure(simulationVersion) {
			infrastructureCount, infrastructureErr := restoreCityOpenWorldInfrastructureProjection(
				ctx, tx, worldID, *runtime.Infrastructure, factIDs,
			)
			if infrastructureErr != nil {
				return count, infrastructureErr
			}
			count += infrastructureCount
		}
		if cityEngineSupportsOpenWorldEffectiveCapacity(simulationVersion) {
			effectiveCapacityCount, effectiveCapacityErr := restoreCityOpenWorldEffectiveCapacityProjection(
				ctx, tx, worldID, *runtime.EffectiveCapacity, factIDs,
			)
			if effectiveCapacityErr != nil {
				return count, effectiveCapacityErr
			}
			count += effectiveCapacityCount
		}
		if cityEngineSupportsOpenWorldFreightSettlements(simulationVersion) {
			settlementCount, settlementErr := restoreCityOpenWorldFreightSettlementProjection(
				ctx, tx, worldID, *runtime.FreightSettlements, commandIDs,
			)
			if settlementErr != nil {
				return count, settlementErr
			}
			count += settlementCount
		}
		if cityEngineSupportsOpenWorldCarrierRecovery(simulationVersion) {
			carrierRecoveryCount, carrierRecoveryErr := restoreCityOpenWorldCarrierRecoveryProjection(
				ctx, tx, worldID, *runtime.CarrierRecovery, commandIDs,
			)
			if carrierRecoveryErr != nil {
				return count, carrierRecoveryErr
			}
			count += carrierRecoveryCount
		}
		if cityEngineSupportsOpenWorldCarrierCommerce(simulationVersion) {
			carrierCommerceCount, carrierCommerceErr := restoreCityOpenWorldCarrierCommerceProjection(
				ctx, tx, worldID, *runtime.CarrierCommerce,
			)
			if carrierCommerceErr != nil {
				return count, carrierCommerceErr
			}
			count += carrierCommerceCount
		}
	}
	return count, nil
}

func restoreCityOpenWorldSocialRuntimeProjection(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	social CityOpenWorldSocialRuntimeState,
	actorIDs map[string]int64,
	factIDs map[cityOpenWorldRuntimeRecoveryIdentity]int64,
) (int, map[string]int64, error) {
	count := 0
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_scenario_bindings
    (world_id, scenario_id, scenario_version, scenario_hash,
     profile_id, profile_version, profile_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb)`,
		worldID, social.Scenario.ScenarioID, social.Scenario.ScenarioVersion,
		social.Scenario.ScenarioHash, social.Scenario.ProfileID,
		social.Scenario.ProfileVersion, social.Scenario.ProfileHash,
		[]byte(social.Scenario.Metadata)); err != nil {
		return count, nil, fmt.Errorf("restore open-world scenario binding: %w", err)
	}
	count++
	facilityIDs := make(map[string]int64, len(social.Facilities))
	for _, facility := range social.Facilities {
		sourceFactID, factErr := resolveOptionalCityOpenWorldRecoveryFactID(factIDs, facility.SourceFact)
		if factErr != nil {
			return count, nil, fmt.Errorf("restore open-world facility %s: %w", facility.Code, factErr)
		}
		var facilityID int64
		if err := tx.QueryRowContext(ctx, `
INSERT INTO city_open_world_facilities
    (world_id, code, building_code, facility_type_code, state, capacity_units,
     anchor_x, anchor_y, anchor_z, last_settled_tick, source_fact_id,
     version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13::jsonb)
RETURNING id`,
			worldID, facility.Code, facility.BuildingCode, facility.FacilityTypeCode,
			facility.State, facility.CapacityUnits, facility.AnchorX, facility.AnchorY,
			facility.AnchorZ, facility.LastSettledTick, sourceFactID, facility.Version,
			[]byte(facility.Metadata)).Scan(&facilityID); err != nil {
			return count, nil, fmt.Errorf("restore open-world facility %s: %w", facility.Code, err)
		}
		facilityIDs[facility.Code] = facilityID
		count++
	}
	for _, profile := range social.NPCProfiles {
		actorID, actorErr := requireCityOpenWorldRecoveryActorID(actorIDs, profile.ActorCode)
		if actorErr != nil {
			return count, nil, fmt.Errorf("restore open-world NPC profile %s: %w", profile.ActorCode, actorErr)
		}
		homeID, facilityErr := resolveOptionalCityOpenWorldRecoveryFacilityID(facilityIDs, profile.HomeFacilityCode)
		if facilityErr != nil {
			return count, nil, fmt.Errorf("restore open-world NPC profile %s: %w", profile.ActorCode, facilityErr)
		}
		workID, facilityErr := resolveOptionalCityOpenWorldRecoveryFacilityID(facilityIDs, profile.WorkFacilityCode)
		if facilityErr != nil {
			return count, nil, fmt.Errorf("restore open-world NPC profile %s: %w", profile.ActorCode, facilityErr)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_npc_profiles
    (world_id, actor_id, behavior_code, behavior_version, behavior_hash,
     home_facility_id, work_facility_id, lod_tier, schedule_offset,
     next_action_tick, last_action_tick, behavior_state, version)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb, $13)`,
			worldID, actorID, profile.BehaviorCode, profile.BehaviorVersion,
			profile.BehaviorHash, homeID, workID, profile.LODTier, profile.ScheduleOffset,
			profile.NextActionTick, profile.LastActionTick, []byte(profile.BehaviorState),
			profile.Version); err != nil {
			return count, nil, fmt.Errorf("restore open-world NPC profile %s: %w", profile.ActorCode, err)
		}
		count++
	}
	for _, intent := range social.NavigationIntents {
		actorID, actorErr := requireCityOpenWorldRecoveryActorID(actorIDs, intent.ActorCode)
		if actorErr != nil {
			return count, nil, fmt.Errorf("restore open-world navigation intent %s: %w", intent.ActorCode, actorErr)
		}
		sourceFactID, factErr := requireCityOpenWorldRecoveryFactID(factIDs, intent.SourceFact)
		if factErr != nil {
			return count, nil, fmt.Errorf("restore open-world navigation intent %s: %w", intent.ActorCode, factErr)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_actor_navigation_intents
    (world_id, actor_id, intent_code, target_space_kind, target_location_scope,
     target_building_code, target_floor_index, target_x, target_y, target_z,
     status, priority, maximum_steps, completed_steps, blocked_attempts,
     next_attempt_tick, created_tick, updated_tick, source_fact_id, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15,
        $16, $17, $18, $19, $20, $21::jsonb)`,
			worldID, actorID, intent.IntentCode, intent.TargetSpaceKind,
			intent.TargetLocationScope, nullableStringValue(intent.TargetBuildingCode),
			intent.TargetFloorIndex, intent.TargetX, intent.TargetY, intent.TargetZ,
			intent.Status, intent.Priority, intent.MaximumSteps, intent.CompletedSteps,
			intent.BlockedAttempts, intent.NextAttemptTick, intent.CreatedTick,
			intent.UpdatedTick, sourceFactID, intent.Version, []byte(intent.Metadata)); err != nil {
			return count, nil, fmt.Errorf("restore open-world navigation intent %s: %w", intent.ActorCode, err)
		}
		count++
	}
	return count, facilityIDs, nil
}

func resolveOptionalCityOpenWorldRecoveryFacilityID(
	facilityIDs map[string]int64,
	code *string,
) (any, error) {
	if code == nil {
		return nil, nil
	}
	id, found := facilityIDs[*code]
	if !found || id <= 0 {
		return nil, fmt.Errorf("unknown facility %s", *code)
	}
	return id, nil
}

func requireCityOpenWorldRecoveryFacilityID(facilityIDs map[string]int64, code string) (int64, error) {
	id, found := facilityIDs[code]
	if !found || id <= 0 {
		return 0, fmt.Errorf("unknown facility %s", code)
	}
	return id, nil
}

func restoreCityOpenWorldServiceProjection(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	services CityOpenWorldServiceState,
	actorIDs map[string]int64,
	facilityIDs map[string]int64,
	factIDs map[cityOpenWorldRuntimeRecoveryIdentity]int64,
) (int, map[string]int64, error) {
	if len(facilityIDs) == 0 {
		return 0, nil, fmt.Errorf("open-world V7 service recovery requires social facilities")
	}
	count := 0
	responseIDs := make(map[string]int64, len(services.Responses))
	policy := services.Policy
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_service_profiles
    (world_id, profile_id, profile_version, content_hash, baseline_tick,
     access_model_version, dispatch_model_version, maximum_queue_per_provider,
     revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb)`,
		worldID, policy.ProfileID, policy.ProfileVersion, policy.ContentHash,
		policy.BaselineTick, policy.AccessModelVersion, policy.DispatchModelVersion,
		policy.MaximumQueuePerProvider, policy.Revision, []byte(policy.Metadata)); err != nil {
		return count, nil, fmt.Errorf("restore open-world V7 service profile: %w", err)
	}
	count++
	for _, entry := range services.Catalog {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_service_catalog
    (world_id, code, name_key, category_code, definition_version, content_hash,
     maximum_wait_ticks, target_response_ticks, default_priority_milli, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb)`,
			worldID, entry.Code, entry.NameKey, entry.CategoryCode, entry.Version,
			entry.ContentHash, entry.MaximumWaitTicks, entry.TargetResponseTicks,
			entry.DefaultPriorityMilli, []byte(entry.Metadata)); err != nil {
			return count, nil, fmt.Errorf("restore open-world V7 service catalog %s: %w", entry.Code, err)
		}
		count++
	}
	providerIDs := make(map[string]int64, len(services.Providers))
	for _, provider := range services.Providers {
		facilityID, facilityErr := requireCityOpenWorldRecoveryFacilityID(facilityIDs, provider.FacilityCode)
		if facilityErr != nil {
			return count, nil, fmt.Errorf("restore open-world V7 service provider %s: %w", provider.Code, facilityErr)
		}
		var providerID int64
		if err := tx.QueryRowContext(ctx, `
INSERT INTO city_open_world_service_providers
    (world_id, code, facility_id, service_code, provider_kind, status,
     capacity_units_per_tick, access_radius_units, anchor_x, anchor_y, anchor_z,
     last_settled_tick, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14::jsonb)
RETURNING id`,
			worldID, provider.Code, facilityID, provider.ServiceCode, provider.ProviderKind,
			provider.Status, provider.CapacityUnitsPerTick, provider.AccessRadiusUnits,
			provider.AnchorX, provider.AnchorY, provider.AnchorZ, provider.LastSettledTick,
			provider.Version, []byte(provider.Metadata)).Scan(&providerID); err != nil {
			return count, nil, fmt.Errorf("restore open-world V7 service provider %s: %w", provider.Code, err)
		}
		providerIDs[provider.Code] = providerID
		count++
	}
	requestIDs := make(map[string]int64, len(services.Requests))
	for _, request := range services.Requests {
		actorID, actorErr := requireCityOpenWorldRecoveryActorID(actorIDs, request.ActorCode)
		if actorErr != nil {
			return count, nil, fmt.Errorf("restore open-world V7 service request %s: %w", request.Code, actorErr)
		}
		sourceFactID, factErr := requireCityOpenWorldRecoveryFactID(factIDs, request.SourceFact)
		if factErr != nil {
			return count, nil, fmt.Errorf("restore open-world V7 service request %s: %w", request.Code, factErr)
		}
		lastFactID, factErr := resolveOptionalCityOpenWorldRecoveryFactID(factIDs, request.LastFact)
		if factErr != nil {
			return count, nil, fmt.Errorf("restore open-world V7 service request %s: %w", request.Code, factErr)
		}
		providerID, providerErr := resolveOptionalCityOpenWorldRecoveryProviderID(providerIDs, request.ProviderCode)
		if providerErr != nil {
			return count, nil, fmt.Errorf("restore open-world V7 service request %s: %w", request.Code, providerErr)
		}
		var requestID int64
		if err := tx.QueryRowContext(ctx, `
INSERT INTO city_open_world_service_requests
    (world_id, code, actor_id, service_code, status, priority_milli,
     requested_units, requested_tick, earliest_dispatch_tick, deadline_tick,
     queued_tick, provider_id, dispatched_tick, resolved_tick, queue_position,
     source_fact_id, last_fact_id, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14,
        $15, $16, $17, $18, $19::jsonb)
RETURNING id`,
			worldID, request.Code, actorID, request.ServiceCode, request.Status,
			request.PriorityMilli, request.RequestedUnits, request.RequestedTick,
			request.EarliestDispatchTick, request.DeadlineTick,
			cityNullableInt64(request.QueuedTick), providerID,
			cityNullableInt64(request.DispatchedTick), cityNullableInt64(request.ResolvedTick),
			cityOpenWorldRecoveryNullableInt(request.QueuePosition), sourceFactID, lastFactID,
			request.Version, []byte(request.Metadata)).Scan(&requestID); err != nil {
			return count, nil, fmt.Errorf("restore open-world V7 service request %s: %w", request.Code, err)
		}
		requestIDs[request.Code] = requestID
		count++
	}
	for _, response := range services.Responses {
		requestID, found := requestIDs[response.RequestCode]
		if !found || requestID <= 0 {
			return count, nil, fmt.Errorf("restore open-world V7 service response %s references unknown request %s", response.Code, response.RequestCode)
		}
		actorID, actorErr := requireCityOpenWorldRecoveryActorID(actorIDs, response.ActorCode)
		if actorErr != nil {
			return count, nil, fmt.Errorf("restore open-world V7 service response %s: %w", response.Code, actorErr)
		}
		providerID, providerErr := resolveOptionalCityOpenWorldRecoveryProviderID(providerIDs, response.ProviderCode)
		if providerErr != nil {
			return count, nil, fmt.Errorf("restore open-world V7 service response %s: %w", response.Code, providerErr)
		}
		sourceFactID, factErr := requireCityOpenWorldRecoveryFactID(factIDs, response.SourceFact)
		if factErr != nil {
			return count, nil, fmt.Errorf("restore open-world V7 service response %s: %w", response.Code, factErr)
		}
		var responseID int64
		if err := tx.QueryRowContext(ctx, `
INSERT INTO city_open_world_service_responses
    (world_id, code, request_id, actor_id, service_code, provider_id, outcome,
     requested_tick, queued_tick, dispatched_tick, resolved_tick, response_ticks,
     delivered_units, source_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15::jsonb)
RETURNING id`,
			worldID, response.Code, requestID, actorID, response.ServiceCode, providerID,
			response.Outcome, response.RequestedTick, cityNullableInt64(response.QueuedTick),
			cityNullableInt64(response.DispatchedTick), response.ResolvedTick,
			response.ResponseTicks, response.DeliveredUnits, sourceFactID,
			[]byte(response.Metadata)).Scan(&responseID); err != nil {
			return count, nil, fmt.Errorf("restore open-world V7 service response %s: %w", response.Code, err)
		}
		responseIDs[response.Code] = responseID
		count++
	}
	return count, responseIDs, nil
}

func resolveOptionalCityOpenWorldRecoveryProviderID(
	providerIDs map[string]int64,
	code *string,
) (any, error) {
	if code == nil {
		return nil, nil
	}
	id, found := providerIDs[*code]
	if !found || id <= 0 {
		return nil, fmt.Errorf("unknown service provider %s", *code)
	}
	return id, nil
}
