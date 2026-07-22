package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const (
	cityOpenWorldInfrastructureRejectionAssetNotFound  = "OPEN_WORLD_INFRASTRUCTURE_ASSET_NOT_FOUND"
	cityOpenWorldInfrastructureRejectionTransition     = "OPEN_WORLD_INFRASTRUCTURE_TRANSITION_INVALID"
	cityOpenWorldInfrastructureRejectionStateUnchanged = "OPEN_WORLD_INFRASTRUCTURE_STATE_UNCHANGED"
	cityOpenWorldInfrastructureRejectionOwnerRequired  = "OPEN_WORLD_INFRASTRUCTURE_OWNER_REQUIRED"
)

type cityOpenWorldInfrastructureAssetTransitionPayload struct {
	AssetCode     string `json:"asset_code"`
	State         string `json:"state"`
	CapacityMilli *int64 `json:"capacity_milli,omitempty"`
	ReasonCode    string `json:"reason_code,omitempty"`
}

type cityOpenWorldInfrastructureAssetRecord struct {
	asset CityOpenWorldInfrastructureAsset
	state CityOpenWorldInfrastructureAssetState
}

func isCityOpenWorldInfrastructureCommand(commandType string) bool {
	return commandType == CityCommandTypeOpenWorldInfrastructureAssetTransition
}

func normalizeCityOpenWorldInfrastructureCommand(rawPayload json.RawMessage) (any, bool, error) {
	var value cityOpenWorldInfrastructureAssetTransitionPayload
	if err := decodeStrictCityObject(rawPayload, &value); err != nil {
		return nil, true, ErrCityInvalidInput.WithCause(err)
	}
	value.AssetCode = strings.ToLower(strings.TrimSpace(value.AssetCode))
	if !worldRuntimeCodeValid(value.AssetCode, 160) {
		return nil, true, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "asset_code"})
	}
	value.State = strings.ToLower(strings.TrimSpace(value.State))
	value.ReasonCode = strings.ToLower(strings.TrimSpace(value.ReasonCode))
	if value.ReasonCode == "" {
		value.ReasonCode = "operator." + value.State
	}
	if !worldRuntimeCodeValid(value.ReasonCode, 96) {
		return nil, true, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "reason_code"})
	}
	capacity := int64(0)
	if value.CapacityMilli != nil {
		capacity = *value.CapacityMilli
	}
	switch value.State {
	case cityOpenWorldInfrastructureStateOperational:
		if value.CapacityMilli != nil && capacity != 1000 {
			return nil, true, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "capacity_milli"})
		}
		capacity = 1000
	case cityOpenWorldInfrastructureStateRestricted:
		if value.CapacityMilli == nil || capacity < 1 || capacity > 999 {
			return nil, true, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "capacity_milli"})
		}
	case cityOpenWorldInfrastructureStateMaintenance,
		cityOpenWorldInfrastructureStateConstruction,
		cityOpenWorldInfrastructureStateClosed:
		if value.CapacityMilli != nil && capacity != 0 {
			return nil, true, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "capacity_milli"})
		}
		capacity = 0
	default:
		return nil, true, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "state"})
	}
	value.CapacityMilli = &capacity
	return value, true, nil
}

func ensureCityOpenWorldInfrastructureEngine(ctx context.Context, tx *sql.Tx, worldID int64) (string, error) {
	var simulationVersion string
	if err := tx.QueryRowContext(ctx, `
SELECT simulation_version
FROM city_worlds
WHERE id = $1
FOR UPDATE`, worldID).Scan(&simulationVersion); err != nil {
		return "", fmt.Errorf("lock V20 infrastructure world: %w", err)
	}
	if !cityEngineSupportsOpenWorldInfrastructure(simulationVersion) {
		return "", ErrCitySimulationVersion.WithMetadata(map[string]string{"version": simulationVersion})
	}
	return simulationVersion, nil
}

func ensureCityOpenWorldInfrastructureOwner(
	ctx context.Context,
	tx *sql.Tx,
	worldID, userID int64,
) error {
	var owner bool
	if err := tx.QueryRowContext(ctx, `
SELECT EXISTS (
    SELECT 1
    FROM city_members
    WHERE world_id = $1 AND user_id = $2 AND role = $3 AND status = 'active'
)`, worldID, userID, CityMemberRoleOwner).Scan(&owner); err != nil {
		return fmt.Errorf("verify V20 infrastructure owner: %w", err)
	}
	if !owner {
		return cityOpenWorldRuntimeReject(cityOpenWorldInfrastructureRejectionOwnerRequired)
	}
	return nil
}

func loadCityOpenWorldInfrastructureAssetForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	assetCode string,
) (*cityOpenWorldInfrastructureAssetRecord, error) {
	record := &cityOpenWorldInfrastructureAssetRecord{}
	var nodeCode, corridorCode sql.NullString
	var factTick, factSequence sql.NullInt64
	err := tx.QueryRowContext(ctx, `
SELECT asset.code, asset.asset_kind, asset.spatial_node_code, asset.spatial_corridor_code,
       asset.segment_ordinal, asset.asset_class, asset.definition_version, asset.content_hash,
       asset.metadata, state.state, state.capacity_milli, state.effective_tick,
       fact.tick, fact.sequence, state.version, state.metadata
FROM city_open_world_infrastructure_assets asset
JOIN city_open_world_infrastructure_asset_states state
  ON state.world_id = asset.world_id AND state.asset_code = asset.code
LEFT JOIN city_open_world_runtime_facts fact ON fact.id = state.source_fact_id
WHERE asset.world_id = $1 AND asset.code = $2
FOR UPDATE OF asset, state`, worldID, assetCode).Scan(
		&record.asset.Code, &record.asset.AssetKind, &nodeCode, &corridorCode,
		&record.asset.SegmentOrdinal, &record.asset.AssetClass, &record.asset.DefinitionVersion,
		&record.asset.ContentHash, &record.asset.Metadata, &record.state.State,
		&record.state.CapacityMilli, &record.state.EffectiveTick, &factTick, &factSequence,
		&record.state.Version, &record.state.Metadata,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, cityOpenWorldRuntimeReject(cityOpenWorldInfrastructureRejectionAssetNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("lock V20 infrastructure asset %s: %w", assetCode, err)
	}
	if nodeCode.Valid {
		record.asset.SpatialNodeCode = &nodeCode.String
	}
	if corridorCode.Valid {
		record.asset.SpatialCorridorCode = &corridorCode.String
	}
	record.state.AssetCode = record.asset.Code
	if factTick.Valid != factSequence.Valid {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v20_infrastructure_state_fact"})
	}
	if factTick.Valid {
		record.state.SourceFact = &CityOpenWorldRuntimeFactRef{Tick: factTick.Int64, Sequence: factSequence.Int64}
	}
	if expectedHash, hashErr := cityOpenWorldInfrastructureAssetContentHash(record.asset); hashErr != nil ||
		expectedHash != record.asset.ContentHash || !cityOpenWorldInfrastructureStateCapacityValid(record.state.State, record.state.CapacityMilli) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v20_infrastructure_asset_state"})
	}
	return record, nil
}

func (s *CityEconomyService) transitionCityOpenWorldInfrastructureAsset(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, effectSequence, caseSequence int64,
	command *CityCommand,
	payload cityOpenWorldInfrastructureAssetTransitionPayload,
) (cityOpenWorldRuntimeExecution, error) {
	if command == nil || payload.CapacityMilli == nil {
		return cityOpenWorldRuntimeExecution{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v20_infrastructure_command"})
	}
	simulationVersion, err := ensureCityOpenWorldInfrastructureEngine(ctx, tx, worldID)
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if err := ensureCityOpenWorldInfrastructureOwner(ctx, tx, worldID, command.UserID); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	record, err := loadCityOpenWorldInfrastructureAssetForUpdate(ctx, tx, worldID, payload.AssetCode)
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if record.state.State == payload.State && record.state.CapacityMilli == *payload.CapacityMilli {
		return cityOpenWorldRuntimeExecution{}, cityOpenWorldRuntimeReject(cityOpenWorldInfrastructureRejectionStateUnchanged)
	}
	if !cityOpenWorldInfrastructureTransitionAllowed(record.state.State, payload.State) ||
		!cityOpenWorldInfrastructureStateCapacityValid(payload.State, *payload.CapacityMilli) {
		return cityOpenWorldRuntimeExecution{}, cityOpenWorldRuntimeReject(cityOpenWorldInfrastructureRejectionTransition)
	}
	if err = activateCityOpenWorldRuntimeFactWrite(ctx, tx, worldID); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	schedulerEffect := "none"
	schedulerEffectiveFromTick := int64(0)
	if cityEngineSupportsOpenWorldEffectiveCapacity(simulationVersion) {
		schedulerEffect = cityOpenWorldEffectiveCapacitySchedulerEffect
		schedulerEffectiveFromTick = targetTick + 1
	}
	factPayload, err := json.Marshal(map[string]any{
		"asset_code": payload.AssetCode, "asset_kind": record.asset.AssetKind,
		"from_state": record.state.State, "to_state": payload.State,
		"capacity_milli": *payload.CapacityMilli, "reason_code": payload.ReasonCode,
		"v9_scheduler_effect":              schedulerEffect,
		"v9_scheduler_effective_from_tick": schedulerEffectiveFromTick,
	})
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, fmt.Errorf("marshal V20 infrastructure transition fact: %w", err)
	}
	fact, err := insertCityOpenWorldRuntimeFact(ctx, tx, cityOpenWorldRuntimeFactInsert{
		worldID: worldID, tick: targetTick, sequence: factSequence, sourceCommandID: &command.ID,
		factType: cityOpenWorldInfrastructureFactAssetTransition, payload: factPayload,
	})
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	metadata, err := cityOpenWorldInfrastructureCommandMetadata(record.state.State)
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, fmt.Errorf("marshal V20 infrastructure transition metadata: %w", err)
	}
	if err = activateCityOpenWorldInfrastructureWrite(ctx, tx, worldID); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if _, err = tx.ExecContext(ctx, `
UPDATE city_open_world_infrastructure_asset_states
SET state = $3, capacity_milli = $4, effective_tick = $5, source_fact_id = $6,
    version = version + 1, metadata = $7::jsonb, updated_at = NOW()
WHERE world_id = $1 AND asset_code = $2`,
		worldID, payload.AssetCode, payload.State, *payload.CapacityMilli, targetTick,
		fact.id, []byte(metadata)); err != nil {
		return cityOpenWorldRuntimeExecution{}, fmt.Errorf("update V20 infrastructure state: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_infrastructure_asset_transitions
    (world_id, asset_code, transition_tick, transition_sequence, from_state,
     to_state, capacity_milli, reason_code, source_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb)`,
		worldID, payload.AssetCode, targetTick, factSequence, record.state.State,
		payload.State, *payload.CapacityMilli, payload.ReasonCode, fact.id, []byte(metadata)); err != nil {
		return cityOpenWorldRuntimeExecution{}, fmt.Errorf("insert V20 infrastructure transition: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
UPDATE city_open_world_infrastructure_profiles
SET transition_count = transition_count + 1, revision = revision + 1, updated_at = NOW()
WHERE world_id = $1`, worldID); err != nil {
		return cityOpenWorldRuntimeExecution{}, fmt.Errorf("update V20 infrastructure profile: %w", err)
	}
	if err = postCityOpenWorldRuntimeFact(ctx, tx, fact.id); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if err = updateCityOpenWorldRuntimeProfile(ctx, tx, worldID, 0, 1, 0, 0); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if err = assertCityOpenWorldInfrastructureFoundation(ctx, tx, worldID); err != nil {
		return cityOpenWorldRuntimeExecution{}, fmt.Errorf("validate V20 infrastructure transition command: %w", err)
	}
	return cityOpenWorldRuntimeExecution{
		pending: cityOpenWorldRuntimePending(command, "city.open_world.infrastructure.asset.transitioned", map[string]any{
			"asset_code": payload.AssetCode, "from_state": record.state.State,
			"state": payload.State, "capacity_milli": *payload.CapacityMilli,
			"v9_scheduler_effect":              schedulerEffect,
			"v9_scheduler_effective_from_tick": schedulerEffectiveFromTick,
		}),
		facts: []CityOpenWorldRuntimeFact{fact.fact}, effects: []CityOpenWorldRuntimeEffect{}, cases: []CityOpenWorldRuleCase{},
		nextFactSeq: factSequence + 1, nextEffectSeq: effectSequence, nextCaseSeq: caseSequence,
	}, nil
}
