package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// V16 keeps all cross-projection references as stable codes or fact cursors in
// the canonical snapshot. Recovery resolves those cursors after the V15 and V9
// projections are restored; it never derives a new delivery, receipt or actor
// location from a freight route.
type cityOpenWorldEnterpriseFreightRecoveryFactKey struct {
	sourceCode string
	tick       int64
	sequence   int64
}

func loadCityOpenWorldEnterpriseFreightRecoveryRuntimeFactID(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	reference CityOpenWorldRuntimeFactRef,
) (int64, error) {
	var id int64
	err := tx.QueryRowContext(ctx, `
SELECT id
FROM city_open_world_runtime_facts
WHERE world_id = $1 AND tick = $2 AND sequence = $3 AND posted_at IS NOT NULL`,
		worldID, reference.Tick, reference.Sequence,
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("enterprise-freight runtime fact %d/%d is unavailable", reference.Tick, reference.Sequence)
	}
	if err != nil {
		return 0, fmt.Errorf("resolve enterprise-freight runtime fact %d/%d: %w", reference.Tick, reference.Sequence, err)
	}
	return id, nil
}

func loadCityOpenWorldEnterpriseFreightRecoveryDispatchFactID(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	orderCode string,
	reference CityOpenWorldRuntimeFactRef,
) (int64, error) {
	var id int64
	err := tx.QueryRowContext(ctx, `
SELECT id
FROM city_open_world_supply_chain_facts
WHERE world_id = $1 AND order_code = $2 AND tick = $3 AND sequence = $4
  AND fact_type = 'order.dispatched'`,
		worldID, orderCode, reference.Tick, reference.Sequence,
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("enterprise-freight dispatch %s/%d/%d is unavailable", orderCode, reference.Tick, reference.Sequence)
	}
	if err != nil {
		return 0, fmt.Errorf("resolve enterprise-freight dispatch %s: %w", orderCode, err)
	}
	return id, nil
}

func loadCityOpenWorldEnterpriseFreightRecoveryCarrierID(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	carrierCode string,
) (int64, error) {
	var id int64
	err := tx.QueryRowContext(ctx, `
SELECT id
FROM city_open_world_actors
WHERE world_id = $1 AND code = $2 AND actor_type_code = $3
  AND status = 'active' AND owner_user_id IS NULL`,
		worldID, carrierCode, cityOpenWorldEnterpriseFreightCarrierActorType,
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("enterprise-freight carrier %s is unavailable", carrierCode)
	}
	if err != nil {
		return 0, fmt.Errorf("resolve enterprise-freight carrier %s: %w", carrierCode, err)
	}
	return id, nil
}

func loadCityOpenWorldEnterpriseFreightRecoveryDemandID(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	code *string,
) (any, error) {
	if code == nil {
		return nil, nil
	}
	var id int64
	err := tx.QueryRowContext(ctx, `
SELECT id FROM city_open_world_mobility_demands
WHERE world_id = $1 AND code = $2`, worldID, *code).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("enterprise-freight mobility demand %s is unavailable", *code)
	}
	if err != nil {
		return nil, fmt.Errorf("resolve enterprise-freight mobility demand %s: %w", *code, err)
	}
	return id, nil
}

func loadCityOpenWorldEnterpriseFreightRecoveryRouteID(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	code *string,
) (any, error) {
	if code == nil {
		return nil, nil
	}
	var id int64
	err := tx.QueryRowContext(ctx, `
SELECT id FROM city_open_world_mobility_routes
WHERE world_id = $1 AND code = $2`, worldID, *code).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("enterprise-freight mobility route %s is unavailable", *code)
	}
	if err != nil {
		return nil, fmt.Errorf("resolve enterprise-freight mobility route %s: %w", *code, err)
	}
	return id, nil
}

func requireCityOpenWorldEnterpriseFreightRecoveryFactID(
	factIDs map[cityOpenWorldEnterpriseFreightRecoveryFactKey]int64,
	sourceCode string,
	reference CityOpenWorldRuntimeFactRef,
) (int64, error) {
	id, found := factIDs[cityOpenWorldEnterpriseFreightRecoveryFactKey{
		sourceCode: sourceCode, tick: reference.Tick, sequence: reference.Sequence,
	}]
	if !found || id <= 0 {
		return 0, fmt.Errorf("enterprise-freight source fact %s/%d/%d is unavailable", sourceCode, reference.Tick, reference.Sequence)
	}
	return id, nil
}

func restoreCityOpenWorldEnterpriseFreightProjection(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	freight CityOpenWorldEnterpriseFreightState,
) (int, error) {
	if err := validateCityOpenWorldEnterpriseFreightState(&freight); err != nil {
		return 0, fmt.Errorf("validate V16 enterprise-freight recovery input: %w", err)
	}
	count := 0
	policy := freight.Policy
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_enterprise_freight_profiles
    (world_id, profile_id, profile_version, content_hash, baseline_tick,
     source_contract, demand_contract, completion_contract, terminal_contract,
     carrier_actor_code, maximum_sources, maximum_generations_per_tick,
     source_count, pending_count, demand_count, scheduled_count, completed_count,
     expired_count, voided_count, orphaned_count, suppressed_count, fact_count,
     transition_count, revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12,
        $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25::jsonb)`,
		worldID, policy.ProfileID, policy.ProfileVersion, policy.ContentHash, policy.BaselineTick,
		policy.SourceContract, policy.DemandContract, policy.CompletionContract, policy.TerminalContract,
		policy.CarrierActorCode, policy.MaximumSources, policy.MaximumGenerationsPerTick,
		policy.SourceCount, policy.PendingCount, policy.DemandCount, policy.ScheduledCount,
		policy.CompletedCount, policy.ExpiredCount, policy.VoidedCount, policy.OrphanedCount,
		policy.SuppressedCount, policy.FactCount, policy.TransitionCount, policy.Revision,
		[]byte(policy.Metadata)); err != nil {
		return count, fmt.Errorf("restore V16 enterprise-freight profile: %w", err)
	}
	count++

	for _, source := range freight.Sources {
		carrierID, carrierErr := loadCityOpenWorldEnterpriseFreightRecoveryCarrierID(ctx, tx, worldID, source.CarrierActorCode)
		if carrierErr != nil {
			return count, carrierErr
		}
		dispatchFactID, dispatchErr := loadCityOpenWorldEnterpriseFreightRecoveryDispatchFactID(
			ctx, tx, worldID, source.OrderCode, source.DispatchFact,
		)
		if dispatchErr != nil {
			return count, dispatchErr
		}
		sourceRuntimeFactID, sourceFactErr := loadCityOpenWorldEnterpriseFreightRecoveryRuntimeFactID(
			ctx, tx, worldID, source.SourceFact,
		)
		if sourceFactErr != nil {
			return count, sourceFactErr
		}
		lastRuntimeFactID, lastFactErr := loadCityOpenWorldEnterpriseFreightRecoveryRuntimeFactID(
			ctx, tx, worldID, source.LastFact,
		)
		if lastFactErr != nil {
			return count, lastFactErr
		}
		demandID, demandErr := loadCityOpenWorldEnterpriseFreightRecoveryDemandID(ctx, tx, worldID, source.DemandCode)
		if demandErr != nil {
			return count, demandErr
		}
		routeID, routeErr := loadCityOpenWorldEnterpriseFreightRecoveryRouteID(ctx, tx, worldID, source.RouteCode)
		if routeErr != nil {
			return count, routeErr
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_enterprise_freight_sources
    (world_id, code, order_code, seller_node_code, buyer_node_code,
     source_hub_code, destination_hub_code, carrier_actor_id, dispatch_fact_id,
     dispatch_tick, source_tick, mobility_deadline_tick, requested_units, state,
     mobility_demand_id, mobility_route_id, source_runtime_fact_id,
     last_runtime_fact_id, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14,
        $15, $16, $17, $18, $19, $20::jsonb)`,
			worldID, source.Code, source.OrderCode, source.SellerNodeCode, source.BuyerNodeCode,
			source.SourceHubCode, source.DestinationHubCode, carrierID, dispatchFactID,
			source.DispatchTick, source.SourceTick, source.MobilityDeadlineTick, source.RequestedUnits,
			source.State, demandID, routeID, sourceRuntimeFactID, lastRuntimeFactID, source.Version,
			[]byte(source.Metadata)); err != nil {
			return count, fmt.Errorf("restore V16 enterprise-freight source %s: %w", source.Code, err)
		}
		count++
	}

	for _, line := range freight.Lines {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_enterprise_freight_source_lines
    (world_id, source_code, line_no, resource_code, source_firm_code,
     source_district_code, destination_firm_code, destination_district_code,
     quantity_units, unit_price_units, total_price_units, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb)`,
			worldID, line.SourceCode, line.LineNo, line.ResourceCode, line.SourceFirmCode,
			line.SourceDistrictCode, line.DestinationFirmCode, line.DestinationDistrictCode,
			line.QuantityUnits, line.UnitPriceUnits, line.TotalPriceUnits, []byte(line.Metadata)); err != nil {
			return count, fmt.Errorf("restore V16 enterprise-freight line %s/%d: %w", line.SourceCode, line.LineNo, err)
		}
		count++
	}

	factIDs := make(map[cityOpenWorldEnterpriseFreightRecoveryFactKey]int64, len(freight.Facts))
	for _, fact := range freight.Facts {
		runtimeFactID, resolveErr := loadCityOpenWorldEnterpriseFreightRecoveryRuntimeFactID(ctx, tx, worldID, fact.RuntimeFact)
		if resolveErr != nil {
			return count, fmt.Errorf("restore V16 enterprise-freight fact %s/%d/%d: %w", fact.SourceCode, fact.Tick, fact.Sequence, resolveErr)
		}
		var factID int64
		if err := tx.QueryRowContext(ctx, `
INSERT INTO city_open_world_enterprise_freight_facts
    (world_id, tick, sequence, source_code, fact_type, runtime_fact_id, payload)
VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)
RETURNING id`,
			worldID, fact.Tick, fact.Sequence, fact.SourceCode, fact.FactType, runtimeFactID,
			[]byte(fact.Payload)).Scan(&factID); err != nil {
			return count, fmt.Errorf("restore V16 enterprise-freight fact %s/%d/%d: %w", fact.SourceCode, fact.Tick, fact.Sequence, err)
		}
		factIDs[cityOpenWorldEnterpriseFreightRecoveryFactKey{
			sourceCode: fact.SourceCode, tick: fact.Tick, sequence: fact.Sequence,
		}] = factID
		count++
	}

	for _, transition := range freight.Transitions {
		sourceFactID, resolveErr := requireCityOpenWorldEnterpriseFreightRecoveryFactID(
			factIDs, transition.SourceCode, transition.SourceFact,
		)
		if resolveErr != nil {
			return count, fmt.Errorf("restore V16 enterprise-freight transition %s/%d/%d: %w", transition.SourceCode, transition.TransitionTick, transition.TransitionSequence, resolveErr)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_enterprise_freight_transitions
    (world_id, source_code, transition_tick, transition_sequence, state,
     reason_code, source_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb)`,
			worldID, transition.SourceCode, transition.TransitionTick, transition.TransitionSequence,
			transition.State, transition.ReasonCode, sourceFactID, []byte(transition.Metadata)); err != nil {
			return count, fmt.Errorf("restore V16 enterprise-freight transition %s/%d/%d: %w", transition.SourceCode, transition.TransitionTick, transition.TransitionSequence, err)
		}
		count++
	}
	if err := assertCityOpenWorldEnterpriseFreightFoundation(ctx, tx, worldID); err != nil {
		return count, fmt.Errorf("validate restored V16 enterprise-freight foundation: %w", err)
	}
	return count, nil
}
