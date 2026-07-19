package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/Wei-Shaw/sub2api/internal/cityspatial"
)

const (
	worldRuntimeSpaceKindCityGrid = "city_grid"
	worldRuntimeSpaceCodePrimary  = "primary"

	worldRuntimeRejectionLocationUnavailable = "WORLD_ACTOR_LOCATION_UNAVAILABLE"
	worldRuntimeRejectionLocationInvalid     = "WORLD_ACTOR_LOCATION_INVALID"
	worldRuntimeRejectionMovementInvalid     = "WORLD_ACTOR_MOVEMENT_INVALID"
	worldRuntimeRejectionControlInvalid      = "WORLD_ACTOR_CONTROL_INVALID"
)

func expectedWorldRuntimeVersion(simulationVersion string) string {
	if cityEngineSupportsWorldNavigationIntents(simulationVersion) {
		return worldRuntimeNavigationIntentVersion
	}
	if cityEngineSupportsWorldPortalAccess(simulationVersion) {
		return worldRuntimePortalAccessVersion
	}
	if cityEngineSupportsWorldActorSpatialControl(simulationVersion) {
		return worldRuntimeSpatialControlVersion
	}
	return worldRuntimeVersion
}

func initializeWorldActorSpatialControlFoundation(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	targetSimulationVersion string,
) error {
	var simulationVersion string
	if err := tx.QueryRowContext(ctx, `
SELECT simulation_version FROM city_worlds WHERE id = $1 FOR UPDATE`, worldID).Scan(&simulationVersion); err != nil {
		return fmt.Errorf("load world actor spatial-control version: %w", err)
	}
	if !cityEngineSupportsWorldActorSpatialControl(targetSimulationVersion) ||
		(simulationVersion != targetSimulationVersion &&
			!cityEngineCanUpgrade(simulationVersion, targetSimulationVersion)) {
		return fmt.Errorf("world actor spatial-control foundation requires a world-runtime engine with actor spatial control")
	}
	if _, err := tx.ExecContext(ctx, `
SELECT set_config('sub2api.world_runtime_bootstrap_world_id', $1, TRUE)`, fmt.Sprintf("%d", worldID)); err != nil {
		return fmt.Errorf("enable world actor spatial-control bootstrap: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE world_runtime_profiles
SET runtime_version = $2, updated_at = NOW()
WHERE world_id = $1`, worldID, worldRuntimeSpatialControlVersion); err != nil {
		return fmt.Errorf("upgrade world runtime spatial-control profile: %w", err)
	}

	rows, err := tx.QueryContext(ctx, `
SELECT id, code, owner_user_id, created_tick
FROM world_actors
WHERE world_id = $1
ORDER BY code ASC`, worldID)
	if err != nil {
		return fmt.Errorf("load actors for spatial-control bootstrap: %w", err)
	}
	type bootstrapActor struct {
		id          int64
		code        string
		ownerUserID sql.NullInt64
		createdTick int64
	}
	actors := make([]bootstrapActor, 0)
	for rows.Next() {
		var actor bootstrapActor
		if err = rows.Scan(&actor.id, &actor.code, &actor.ownerUserID, &actor.createdTick); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan actor for spatial-control bootstrap: %w", err)
		}
		actors = append(actors, actor)
	}
	if err = closeCityRows(rows, "iterate actors for spatial-control bootstrap"); err != nil {
		return err
	}
	for _, actor := range actors {
		location, resolveErr := resolveInitialWorldActorLocation(ctx, tx, worldID, actor.code)
		if resolveErr != nil {
			return fmt.Errorf("resolve bootstrap location for %s: %w", actor.code, resolveErr)
		}
		location.MovedTick = actor.createdTick
		location.Version = 1
		location.Metadata = json.RawMessage(`{"schema_version":1,"source":"baseline"}`)
		if _, err = tx.ExecContext(ctx, `
INSERT INTO world_actor_locations
    (world_id, actor_id, space_kind, space_code, x, y, z, chunk_x, chunk_y,
     local_x, local_y, anchor_kind, anchor_code, jurisdiction_code, moved_tick,
     source_fact_id, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14,
        $15, NULL, 1, $16::jsonb)
ON CONFLICT (world_id, actor_id) DO NOTHING`, worldID, actor.id, location.SpaceKind,
			location.SpaceCode, location.X, location.Y, location.Z, location.ChunkX,
			location.ChunkY, location.LocalX, location.LocalY,
			nullableStringValue(location.AnchorKind), nullableStringValue(location.AnchorCode),
			location.JurisdictionCode, location.MovedTick, []byte(location.Metadata)); err != nil {
			return fmt.Errorf("bootstrap actor location %s: %w", actor.code, err)
		}
		if !actor.ownerUserID.Valid {
			continue
		}
		for index, capability := range []string{WorldActorCapabilityCommand, WorldActorCapabilityManageControl} {
			grantCode := fmt.Sprintf("grant_baseline_%d_%d", actor.id, index+1)
			if _, err = tx.ExecContext(ctx, `
INSERT INTO world_actor_control_grants
    (world_id, code, actor_id, user_id, capability, status, granted_by_user_id,
     granted_tick, revoked_tick, grant_source_fact_id, revoke_source_fact_id,
     version, metadata)
VALUES ($1, $2, $3, $4, $5, 'active', $4, $6, NULL, NULL, NULL, 1,
        '{"schema_version":1,"source":"baseline"}'::jsonb)
ON CONFLICT DO NOTHING`, worldID, grantCode, actor.id, actor.ownerUserID.Int64,
				capability, actor.createdTick); err != nil {
				return fmt.Errorf("bootstrap actor control grant %s/%s: %w", actor.code, capability, err)
			}
		}
	}
	return nil
}

func worldRuntimeUsesSpatialControl(ctx context.Context, queryer citySQLQueryer, worldID int64) (bool, error) {
	var version string
	if err := queryer.QueryRowContext(ctx, `SELECT simulation_version FROM city_worlds WHERE id = $1`, worldID).Scan(&version); err != nil {
		return false, fmt.Errorf("load world runtime engine version: %w", err)
	}
	return cityEngineSupportsWorldActorSpatialControl(version), nil
}

func resolveWorldActorLocation(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	actorCode string,
	x, y int64,
	z int32,
	anchorKind, anchorCode string,
) (WorldActorLocation, error) {
	var chunkSize, minimumChunkX, maximumChunkX, minimumChunkY, maximumChunkY int64
	var minimumZ, maximumZ int32
	if err := queryer.QueryRowContext(ctx, `
SELECT chunk_size, minimum_chunk_x, maximum_chunk_x, minimum_chunk_y,
       maximum_chunk_y, minimum_z, maximum_z
FROM city_spatial_profiles WHERE world_id = $1`, worldID).Scan(
		&chunkSize, &minimumChunkX, &maximumChunkX, &minimumChunkY,
		&maximumChunkY, &minimumZ, &maximumZ,
	); err != nil {
		return WorldActorLocation{}, fmt.Errorf("load actor location spatial profile: %w", err)
	}
	address, err := cityspatial.SplitWorldCoordinate(cityspatial.WorldCoordinate{X: x, Y: y, Z: z}, chunkSize)
	if err != nil || address.Chunk.X < minimumChunkX || address.Chunk.X > maximumChunkX ||
		address.Chunk.Y < minimumChunkY || address.Chunk.Y > maximumChunkY ||
		z < minimumZ || z > maximumZ {
		return WorldActorLocation{}, worldRuntimeReject(worldRuntimeRejectionLocationInvalid)
	}
	location := WorldActorLocation{
		ActorCode: actorCode, SpaceKind: worldRuntimeSpaceKindCityGrid,
		SpaceCode: worldRuntimeSpaceCodePrimary, X: x, Y: y, Z: z,
		ChunkX: address.Chunk.X, ChunkY: address.Chunk.Y,
		LocalX: address.Local.X, LocalY: address.Local.Y,
		Metadata: json.RawMessage(`{"schema_version":1}`),
	}
	chunkCode := worldRuntimeChunkAnchorCode(address.Chunk.X, address.Chunk.Y, z)
	if anchorKind == "" {
		if z != 0 {
			return WorldActorLocation{}, worldRuntimeReject(worldRuntimeRejectionLocationInvalid)
		}
		anchorKind, anchorCode = "chunk", chunkCode
	}
	switch anchorKind {
	case "chunk":
		if z != 0 || anchorCode != chunkCode {
			return WorldActorLocation{}, worldRuntimeReject(worldRuntimeRejectionLocationInvalid)
		}
		if err = queryer.QueryRowContext(ctx, `
SELECT district.code
FROM city_overmap_tiles tile
JOIN city_districts district ON district.id = tile.district_id AND district.world_id = tile.world_id
WHERE tile.world_id = $1 AND tile.chunk_x = $2 AND tile.chunk_y = $3 AND tile.z = 0`,
			worldID, address.Chunk.X, address.Chunk.Y).Scan(&location.JurisdictionCode); err != nil {
			return WorldActorLocation{}, worldRuntimeReject(worldRuntimeRejectionLocationInvalid)
		}
	case "building":
		if err = queryer.QueryRowContext(ctx, `
SELECT district.code
FROM city_buildings building
JOIN city_districts district ON district.id = building.district_id AND district.world_id = building.world_id
WHERE building.world_id = $1 AND building.code = $2
	  AND $3 BETWEEN building.chunk_x * $6 + building.local_min_x AND building.chunk_x * $6 + building.local_max_x
	  AND $4 BETWEEN building.chunk_y * $6 + building.local_min_y AND building.chunk_y * $6 + building.local_max_y
	  AND $5 BETWEEN building.base_z AND building.top_z`, worldID, anchorCode, x, y, z, chunkSize).
			Scan(&location.JurisdictionCode); errors.Is(err, sql.ErrNoRows) {
			return WorldActorLocation{}, worldRuntimeReject(worldRuntimeRejectionLocationInvalid)
		} else if err != nil {
			return WorldActorLocation{}, fmt.Errorf("resolve actor building location: %w", err)
		}
	case "site":
		if err = queryer.QueryRowContext(ctx, `
SELECT district.code
FROM city_enterprise_sites site
JOIN city_buildings building ON building.id = site.building_id AND building.world_id = site.world_id
JOIN city_districts district ON district.id = site.district_id AND district.world_id = site.world_id
WHERE site.world_id = $1 AND site.code = $2 AND site.status = 'active'
	  AND $3 BETWEEN building.chunk_x * $6 + building.local_min_x AND building.chunk_x * $6 + building.local_max_x
	  AND $4 BETWEEN building.chunk_y * $6 + building.local_min_y AND building.chunk_y * $6 + building.local_max_y
	  AND $5 BETWEEN building.base_z AND building.top_z`, worldID, anchorCode, x, y, z, chunkSize).
			Scan(&location.JurisdictionCode); errors.Is(err, sql.ErrNoRows) {
			return WorldActorLocation{}, worldRuntimeReject(worldRuntimeRejectionLocationInvalid)
		} else if err != nil {
			return WorldActorLocation{}, fmt.Errorf("resolve actor enterprise-site location: %w", err)
		}
	default:
		return WorldActorLocation{}, worldRuntimeReject(worldRuntimeRejectionLocationInvalid)
	}
	location.AnchorKind = stringPointer(anchorKind)
	location.AnchorCode = stringPointer(anchorCode)
	return location, nil
}

func resolveInitialWorldActorLocation(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	actorCode string,
) (WorldActorLocation, error) {
	var chunkSize, chunkX, chunkY int64
	if err := queryer.QueryRowContext(ctx, `
SELECT profile.chunk_size, tile.chunk_x, tile.chunk_y
FROM city_spatial_profiles profile
JOIN city_overmap_tiles tile ON tile.world_id = profile.world_id AND tile.z = 0
WHERE profile.world_id = $1
ORDER BY ABS(tile.chunk_x) + ABS(tile.chunk_y), tile.chunk_x, tile.chunk_y
LIMIT 1`, worldID).Scan(&chunkSize, &chunkX, &chunkY); err != nil {
		return WorldActorLocation{}, fmt.Errorf("resolve initial actor tile: %w", err)
	}
	return resolveWorldActorLocation(
		ctx, queryer, worldID, actorCode,
		chunkX*chunkSize+chunkSize/2, chunkY*chunkSize+chunkSize/2, 0, "", "",
	)
}

func worldRuntimeChunkAnchorCode(chunkX, chunkY int64, z int32) string {
	return fmt.Sprintf("chunk.z%d.x%d.y%d", z, chunkX, chunkY)
}

func validateWorldActorStep(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	from, to WorldActorLocation,
) error {
	dx, dy := absoluteInt64(to.X-from.X), absoluteInt64(to.Y-from.Y)
	dz := int64(to.Z - from.Z)
	if dz < 0 {
		dz = -dz
	}
	if (dx == 0 && dy == 0 && dz == 0) || dx > 1 || dy > 1 || dz > 1 {
		return worldRuntimeReject(worldRuntimeRejectionMovementInvalid)
	}
	if dz == 0 {
		return nil
	}
	var portalCount int
	if err := queryer.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM city_building_portals
WHERE world_id = $1 AND bidirectional
  AND ((from_x = $2 AND from_y = $3 AND from_z = $4 AND to_x = $5 AND to_y = $6 AND to_z = $7)
    OR (to_x = $2 AND to_y = $3 AND to_z = $4 AND from_x = $5 AND from_y = $6 AND from_z = $7))`,
		worldID, from.X, from.Y, from.Z, to.X, to.Y, to.Z).Scan(&portalCount); err != nil {
		return fmt.Errorf("validate actor vertical movement portal: %w", err)
	}
	if portalCount != 1 {
		return worldRuntimeReject(worldRuntimeRejectionMovementInvalid)
	}
	return nil
}

func absoluteInt64(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}

func loadWorldActorLocationForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	worldID, actorID int64,
	actorCode string,
) (*WorldActorLocation, error) {
	location, err := scanWorldActorLocation(tx.QueryRowContext(ctx, `
SELECT actor.code, location.space_kind, location.space_code, location.x, location.y,
       location.z, location.chunk_x, location.chunk_y, location.local_x, location.local_y,
       location.anchor_kind, location.anchor_code, location.jurisdiction_code,
       location.moved_tick, fact.tick, fact.sequence, location.version, location.metadata
FROM world_actor_locations location
JOIN world_actors actor ON actor.id = location.actor_id AND actor.world_id = location.world_id
LEFT JOIN world_runtime_facts fact ON fact.id = location.source_fact_id AND fact.world_id = location.world_id
WHERE location.world_id = $1 AND location.actor_id = $2 AND actor.code = $3
FOR UPDATE OF location`, worldID, actorID, actorCode))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, worldRuntimeReject(worldRuntimeRejectionLocationUnavailable)
	}
	if err != nil {
		return nil, fmt.Errorf("load actor location: %w", err)
	}
	return location, nil
}

func scanWorldActorLocation(row cityScannable) (*WorldActorLocation, error) {
	item := &WorldActorLocation{}
	var anchorKind, anchorCode sql.NullString
	var sourceTick, sourceSequence sql.NullInt64
	if err := row.Scan(&item.ActorCode, &item.SpaceKind, &item.SpaceCode, &item.X, &item.Y,
		&item.Z, &item.ChunkX, &item.ChunkY, &item.LocalX, &item.LocalY,
		&anchorKind, &anchorCode, &item.JurisdictionCode, &item.MovedTick,
		&sourceTick, &sourceSequence, &item.Version, &item.Metadata); err != nil {
		return nil, err
	}
	if anchorKind.Valid {
		item.AnchorKind, item.AnchorCode = stringPointer(anchorKind.String), stringPointer(anchorCode.String)
	}
	if sourceTick.Valid {
		item.SourceFact = &WorldRuntimeFactRef{Tick: sourceTick.Int64, Sequence: sourceSequence.Int64}
	}
	return item, nil
}

func applyWorldActorLocationEffect(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, effectSequence int64,
	operationIndex int,
	actor *worldRuntimeActorRef,
	fact *worldRuntimeFactRecord,
	destination WorldActorLocation,
	before *WorldActorLocation,
) (*WorldEffectOperation, error) {
	destination.ActorCode = actor.actor.Code
	destination.MovedTick = targetTick
	destination.SourceFact = &WorldRuntimeFactRef{Tick: fact.fact.Tick, Sequence: fact.fact.Sequence}
	beforeVersion := int64(0)
	if before != nil {
		beforeVersion = before.Version
	}
	destination.Version = beforeVersion + 1
	if before == nil {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO world_actor_locations
    (world_id, actor_id, space_kind, space_code, x, y, z, chunk_x, chunk_y,
     local_x, local_y, anchor_kind, anchor_code, jurisdiction_code, moved_tick,
     source_fact_id, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14,
        $15, $16, 1, $17::jsonb)`, worldID, actor.id, destination.SpaceKind,
			destination.SpaceCode, destination.X, destination.Y, destination.Z,
			destination.ChunkX, destination.ChunkY, destination.LocalX, destination.LocalY,
			nullableStringValue(destination.AnchorKind), nullableStringValue(destination.AnchorCode),
			destination.JurisdictionCode, targetTick, fact.id, []byte(destination.Metadata)); err != nil {
			return nil, fmt.Errorf("insert actor location projection: %w", err)
		}
	} else if _, err := tx.ExecContext(ctx, `
UPDATE world_actor_locations
SET space_kind = $3, space_code = $4, x = $5, y = $6, z = $7,
    chunk_x = $8, chunk_y = $9, local_x = $10, local_y = $11,
    anchor_kind = $12, anchor_code = $13, jurisdiction_code = $14,
    moved_tick = $15, source_fact_id = $16, version = $17,
    metadata = $18::jsonb, updated_at = NOW()
WHERE world_id = $1 AND actor_id = $2`, worldID, actor.id, destination.SpaceKind,
		destination.SpaceCode, destination.X, destination.Y, destination.Z,
		destination.ChunkX, destination.ChunkY, destination.LocalX, destination.LocalY,
		nullableStringValue(destination.AnchorKind), nullableStringValue(destination.AnchorCode),
		destination.JurisdictionCode, targetTick, fact.id, destination.Version,
		[]byte(destination.Metadata)); err != nil {
		return nil, fmt.Errorf("update actor location projection: %w", err)
	}
	payload, err := json.Marshal(map[string]any{
		"schema_version": 1, "location_before": before, "location_after": destination,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal actor location effect: %w", err)
	}
	return insertWorldRuntimeSpecialEffect(
		ctx, tx, worldID, targetTick, effectSequence, operationIndex, actor, fact,
		WorldRuntimeEffectLocationSet, "position", beforeVersion, destination.Version, payload,
	)
}

func applyWorldActorControlEffect(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, effectSequence int64,
	operationIndex int,
	actor *worldRuntimeActorRef,
	fact *worldRuntimeFactRecord,
	grantorUserID, targetUserID int64,
	capability string,
	grant bool,
) (*WorldEffectOperation, error) {
	if grant {
		var memberCount int
		if err := tx.QueryRowContext(ctx, `
SELECT COUNT(*) FROM city_members
WHERE world_id = $1 AND user_id = $2 AND status = 'active'`, worldID, targetUserID).Scan(&memberCount); err != nil {
			return nil, fmt.Errorf("validate actor control grantee membership: %w", err)
		}
		if memberCount != 1 {
			return nil, worldRuntimeReject(worldRuntimeRejectionControlInvalid)
		}
		var activeCount int
		if err := tx.QueryRowContext(ctx, `
SELECT COUNT(*) FROM world_actor_control_grants
WHERE world_id = $1 AND actor_id = $2 AND user_id = $3 AND capability = $4 AND status = 'active'`,
			worldID, actor.id, targetUserID, capability).Scan(&activeCount); err != nil {
			return nil, fmt.Errorf("check active actor control grant: %w", err)
		}
		if activeCount != 0 {
			return nil, worldRuntimeReject(worldRuntimeRejectionControlInvalid)
		}
		grantCode := fmt.Sprintf("grant_%d_%d", targetTick, effectSequence)
		grantValue := WorldActorControlGrant{
			Code: grantCode, ActorCode: actor.actor.Code, UserID: targetUserID,
			Capability: capability, Status: "active", GrantedByUserID: grantorUserID,
			GrantedTick:     targetTick,
			GrantSourceFact: &WorldRuntimeFactRef{Tick: fact.fact.Tick, Sequence: fact.fact.Sequence},
			Version:         1, Metadata: json.RawMessage(`{"schema_version":1}`),
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO world_actor_control_grants
    (world_id, code, actor_id, user_id, capability, status, granted_by_user_id,
     granted_tick, grant_source_fact_id, version, metadata)
VALUES ($1, $2, $3, $4, $5, 'active', $6, $7, $8, 1, $9::jsonb)`,
			worldID, grantCode, actor.id, targetUserID, capability, grantorUserID,
			targetTick, fact.id, []byte(grantValue.Metadata)); err != nil {
			return nil, fmt.Errorf("insert actor control grant: %w", err)
		}
		payload, err := json.Marshal(map[string]any{
			"schema_version": 1, "control_grant_before": nil, "control_grant_after": grantValue,
		})
		if err != nil {
			return nil, fmt.Errorf("marshal actor control grant effect: %w", err)
		}
		return insertWorldRuntimeSpecialEffect(ctx, tx, worldID, targetTick, effectSequence,
			operationIndex, actor, fact, WorldRuntimeEffectControlGrant, capability, 0, 1, payload)
	}

	if targetUserID == actor.actor.OwnerUserIDValue() {
		return nil, worldRuntimeReject(worldRuntimeRejectionControlInvalid)
	}
	var id int64
	var value WorldActorControlGrant
	var grantTick int64
	var grantFactTick, grantFactSequence sql.NullInt64
	var metadata json.RawMessage
	err := tx.QueryRowContext(ctx, `
SELECT value.id, value.code, value.granted_by_user_id, value.granted_tick,
       fact.tick, fact.sequence, value.version, value.metadata
FROM world_actor_control_grants value
LEFT JOIN world_runtime_facts fact ON fact.id = value.grant_source_fact_id AND fact.world_id = value.world_id
WHERE value.world_id = $1 AND value.actor_id = $2 AND value.user_id = $3
  AND value.capability = $4 AND value.status = 'active'
FOR UPDATE OF value`, worldID, actor.id, targetUserID, capability).Scan(
		&id, &value.Code, &value.GrantedByUserID, &grantTick,
		&grantFactTick, &grantFactSequence, &value.Version, &metadata,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, worldRuntimeReject(worldRuntimeRejectionControlInvalid)
	}
	if err != nil {
		return nil, fmt.Errorf("load actor control grant for revoke: %w", err)
	}
	value.ActorCode = actor.actor.Code
	value.UserID = targetUserID
	value.Capability = capability
	value.Status = "revoked"
	value.GrantedTick = grantTick
	value.RevokedTick = int64Pointer(targetTick)
	if grantFactTick.Valid {
		value.GrantSourceFact = &WorldRuntimeFactRef{Tick: grantFactTick.Int64, Sequence: grantFactSequence.Int64}
	}
	value.RevokeSourceFact = &WorldRuntimeFactRef{Tick: fact.fact.Tick, Sequence: fact.fact.Sequence}
	value.Version++
	value.Metadata = metadata
	if _, err = tx.ExecContext(ctx, `
UPDATE world_actor_control_grants
SET status = 'revoked', revoked_tick = $3, revoke_source_fact_id = $4,
    version = $5, updated_at = NOW()
WHERE world_id = $1 AND id = $2`, worldID, id, targetTick, fact.id, value.Version); err != nil {
		return nil, fmt.Errorf("revoke actor control grant: %w", err)
	}
	payload, err := json.Marshal(map[string]any{
		"schema_version": 1, "control_grant_after": value,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal actor control revoke effect: %w", err)
	}
	return insertWorldRuntimeSpecialEffect(ctx, tx, worldID, targetTick, effectSequence,
		operationIndex, actor, fact, WorldRuntimeEffectControlRevoke, capability, 1, 0, payload)
}

func insertWorldRuntimeSpecialEffect(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, effectSequence int64,
	operationIndex int,
	actor *worldRuntimeActorRef,
	fact *worldRuntimeFactRecord,
	effectType, targetKey string,
	before, after int64,
	payload json.RawMessage,
) (*WorldEffectOperation, error) {
	delta := after - before
	operation := &WorldEffectOperation{
		Tick: targetTick, Sequence: effectSequence,
		SourceFact:     WorldRuntimeFactRef{Tick: fact.fact.Tick, Sequence: fact.fact.Sequence},
		OperationIndex: operationIndex, EffectType: effectType,
		ExecutorVersion: worldRuntimeSpatialControlVersion,
		TargetActorCode: stringPointer(actor.actor.Code), TargetKey: stringPointer(targetKey),
		BeforeUnits: &before, DeltaUnits: &delta, AfterUnits: &after, Payload: payload,
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO world_effect_operations
    (world_id, tick, sequence, source_fact_id, operation_index, effect_type,
     executor_version, target_actor_id, target_key, before_units, delta_units,
     after_units, payload)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13::jsonb)`,
		worldID, targetTick, effectSequence, fact.id, operationIndex, effectType,
		operation.ExecutorVersion, actor.id, targetKey, before, delta, after, []byte(payload)); err != nil {
		return nil, fmt.Errorf("insert world spatial-control effect %s: %w", effectType, err)
	}
	return operation, nil
}

func (actor WorldActor) OwnerUserIDValue() int64 {
	if actor.OwnerUserID == nil {
		return 0
	}
	return *actor.OwnerUserID
}

type worldRuleScopeResolution struct {
	Matched     bool   `json:"matched"`
	Kind        string `json:"kind"`
	Requested   string `json:"requested"`
	Resolved    string `json:"resolved,omitempty"`
	MatchedBy   string `json:"matched_by,omitempty"`
	LocationRef string `json:"location_ref,omitempty"`
}

func resolveWorldRuleScope(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, actorID int64,
	rule *worldRuntimeRuleDefinition,
) (worldRuleScopeResolution, error) {
	resolution := worldRuleScopeResolution{Kind: rule.ScopeKind, Requested: rule.ScopeCode}
	switch rule.ScopeKind {
	case "world":
		resolution.Matched = rule.ScopeCode == "world"
		resolution.Resolved = "world"
		resolution.MatchedBy = "world"
		return resolution, nil
	case "organization":
		var roleCode string
		err := queryer.QueryRowContext(ctx, `
SELECT role_code
FROM world_actor_roles
WHERE world_id = $1 AND actor_id = $2 AND category_code = 'organization'
  AND role_code = $3 AND status = 'active'
ORDER BY granted_tick DESC, id DESC
LIMIT 1`, worldID, actorID, rule.ScopeCode).Scan(&roleCode)
		if errors.Is(err, sql.ErrNoRows) {
			return resolution, nil
		}
		if err != nil {
			return resolution, fmt.Errorf("resolve organization rule scope: %w", err)
		}
		resolution.Matched = true
		resolution.Resolved = roleCode
		resolution.MatchedBy = "active_role"
		return resolution, nil
	case "jurisdiction", "location":
		var location WorldActorLocation
		var anchorKind, anchorCode sql.NullString
		err := queryer.QueryRowContext(ctx, `
SELECT location.space_kind, location.space_code, location.x, location.y, location.z,
       location.chunk_x, location.chunk_y, location.anchor_kind, location.anchor_code,
       location.jurisdiction_code
FROM world_actor_locations location
WHERE location.world_id = $1 AND location.actor_id = $2`, worldID, actorID).Scan(
			&location.SpaceKind, &location.SpaceCode, &location.X, &location.Y, &location.Z,
			&location.ChunkX, &location.ChunkY, &anchorKind, &anchorCode,
			&location.JurisdictionCode,
		)
		if errors.Is(err, sql.ErrNoRows) {
			return resolution, nil
		}
		if err != nil {
			return resolution, fmt.Errorf("resolve spatial rule scope: %w", err)
		}
		resolution.LocationRef = fmt.Sprintf("position.z%d.x%d.y%d", location.Z, location.X, location.Y)
		if rule.ScopeKind == "jurisdiction" {
			resolution.Matched = location.JurisdictionCode == rule.ScopeCode
			resolution.Resolved = location.JurisdictionCode
			resolution.MatchedBy = "jurisdiction"
			return resolution, nil
		}
		candidates := []struct {
			code string
			kind string
		}{
			{code: resolution.LocationRef, kind: "position"},
			{code: worldRuntimeChunkAnchorCode(location.ChunkX, location.ChunkY, location.Z), kind: "chunk"},
			{code: fmt.Sprintf("space.%s.%s", location.SpaceKind, location.SpaceCode), kind: "space"},
		}
		if anchorKind.Valid && anchorCode.Valid {
			candidates = append([]struct {
				code string
				kind string
			}{{code: anchorCode.String, kind: anchorKind.String}}, candidates...)
		}
		for _, candidate := range candidates {
			if candidate.code == rule.ScopeCode {
				resolution.Matched = true
				resolution.Resolved = candidate.code
				resolution.MatchedBy = candidate.kind
				break
			}
		}
		return resolution, nil
	default:
		return resolution, fmt.Errorf("unsupported world rule scope kind %q", rule.ScopeKind)
	}
}

func (s *CityEconomyService) moveWorldActor(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, effectSequence, caseSequence int64,
	command *CityCommand,
	payload worldActorLocationMovePayload,
) (worldRuntimeExecution, error) {
	actor, err := loadWorldActorWithCapability(
		ctx, tx, worldID, command.UserID, payload.ActorCode, WorldActorCapabilityCommand,
	)
	if err != nil {
		return worldRuntimeExecution{}, err
	}
	current, err := loadWorldActorLocationForUpdate(ctx, tx, worldID, actor.id, actor.actor.Code)
	if err != nil {
		return worldRuntimeExecution{}, err
	}
	anchorKind, anchorCode := payload.AnchorKind, payload.AnchorCode
	var simulationVersion string
	if err = tx.QueryRowContext(ctx, `
SELECT simulation_version FROM city_worlds WHERE id = $1`, worldID).Scan(&simulationVersion); err != nil {
		return worldRuntimeExecution{}, fmt.Errorf("load actor movement engine version: %w", err)
	}
	if cityEngineSupportsWorldActorNavigation(simulationVersion) {
		cell, navigationErr := validateWorldActorNavigationStep(
			ctx, tx, worldID, actor.actor.Code,
			CityNavigationCoordinate{X: current.X, Y: current.Y, Z: current.Z},
			CityNavigationCoordinate{X: payload.X, Y: payload.Y, Z: payload.Z},
		)
		if navigationErr != nil {
			return worldRuntimeExecution{}, navigationErr
		}
		anchorKind, anchorCode, err = resolveWorldActorNavigationAnchor(cell, anchorKind, anchorCode)
		if err != nil {
			return worldRuntimeExecution{}, err
		}
	} else if anchorKind == "" && payload.Z != 0 && current.AnchorKind != nil && current.AnchorCode != nil {
		anchorKind, anchorCode = *current.AnchorKind, *current.AnchorCode
	}
	destination, err := resolveWorldActorLocation(
		ctx, tx, worldID, actor.actor.Code, payload.X, payload.Y, payload.Z, anchorKind, anchorCode,
	)
	if err != nil {
		return worldRuntimeExecution{}, err
	}
	if !cityEngineSupportsWorldActorNavigation(simulationVersion) {
		err = validateWorldActorStep(ctx, tx, worldID, *current, destination)
	}
	if err != nil {
		return worldRuntimeExecution{}, err
	}
	factPayload, err := json.Marshal(map[string]any{
		"schema_version": 1, "from": current, "to": destination,
	})
	if err != nil {
		return worldRuntimeExecution{}, fmt.Errorf("marshal actor movement fact: %w", err)
	}
	root, err := insertWorldRuntimeFact(ctx, tx, worldRuntimeFactInsert{
		worldID: worldID, tick: targetTick, sequence: factSequence,
		sourceCommandID: &command.ID, actorID: &actor.id,
		factType: WorldRuntimeFactLocationMoved, payload: factPayload,
	})
	if err != nil {
		return worldRuntimeExecution{}, err
	}
	if err = enableWorldRuntimeFactWrite(ctx, tx, root.id); err != nil {
		return worldRuntimeExecution{}, err
	}
	operation, err := applyWorldActorLocationEffect(
		ctx, tx, worldID, targetTick, effectSequence, 1, actor, root, destination, current,
	)
	if err != nil {
		return worldRuntimeExecution{}, err
	}
	if err = touchWorldActor(ctx, tx, worldID, actor.id, targetTick); err != nil {
		return worldRuntimeExecution{}, err
	}
	if err = updateWorldRuntimeProfile(ctx, tx, worldID, 0, 1, 1, 0); err != nil {
		return worldRuntimeExecution{}, err
	}
	if err = postWorldRuntimeFact(ctx, tx, root.id); err != nil {
		return worldRuntimeExecution{}, err
	}
	root.fact.ActorCode = stringPointer(actor.actor.Code)
	pending := cityPendingEvent{
		command: command, status: CityCommandStatusApplied, eventType: "world.actor.location_moved",
		payload: map[string]any{"actor_code": actor.actor.Code, "location": destination},
		result:  map[string]any{"applied": true, "actor_code": actor.actor.Code, "location": destination},
	}
	return worldRuntimeExecution{
		pending: pending, facts: []WorldRuntimeFact{root.fact},
		effects: []WorldEffectOperation{*operation}, cases: []WorldRuleCase{},
		nextFactSeq: factSequence + 1, nextEffectSeq: effectSequence + 1, nextCaseSeq: caseSequence,
	}, nil
}

func (s *CityEconomyService) changeWorldActorControl(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, effectSequence, caseSequence int64,
	command *CityCommand,
	payload worldActorControlPayload,
	grant bool,
) (worldRuntimeExecution, error) {
	actor, err := loadWorldActorWithCapability(
		ctx, tx, worldID, command.UserID, payload.ActorCode, WorldActorCapabilityManageControl,
	)
	if err != nil {
		return worldRuntimeExecution{}, err
	}
	factType, eventType := WorldRuntimeFactControlRevoked, "world.actor.control_revoked"
	if grant {
		factType, eventType = WorldRuntimeFactControlGranted, "world.actor.control_granted"
	}
	factPayload, err := json.Marshal(map[string]any{
		"schema_version": 1, "target_user_id": payload.UserID,
		"capabilities": payload.Capabilities,
	})
	if err != nil {
		return worldRuntimeExecution{}, fmt.Errorf("marshal actor control fact: %w", err)
	}
	root, err := insertWorldRuntimeFact(ctx, tx, worldRuntimeFactInsert{
		worldID: worldID, tick: targetTick, sequence: factSequence,
		sourceCommandID: &command.ID, actorID: &actor.id,
		factType: factType, payload: factPayload,
	})
	if err != nil {
		return worldRuntimeExecution{}, err
	}
	if err = enableWorldRuntimeFactWrite(ctx, tx, root.id); err != nil {
		return worldRuntimeExecution{}, err
	}
	capabilities := append([]string(nil), payload.Capabilities...)
	sort.Strings(capabilities)
	operations := make([]WorldEffectOperation, 0, len(capabilities))
	nextEffectSequence := effectSequence
	for index, capability := range capabilities {
		operation, effectErr := applyWorldActorControlEffect(
			ctx, tx, worldID, targetTick, nextEffectSequence, index+1, actor, root,
			command.UserID, payload.UserID, capability, grant,
		)
		if effectErr != nil {
			return worldRuntimeExecution{}, effectErr
		}
		operations = append(operations, *operation)
		nextEffectSequence++
	}
	if err = touchWorldActor(ctx, tx, worldID, actor.id, targetTick); err != nil {
		return worldRuntimeExecution{}, err
	}
	if err = updateWorldRuntimeProfile(ctx, tx, worldID, 0, 1, int64(len(operations)), 0); err != nil {
		return worldRuntimeExecution{}, err
	}
	if err = postWorldRuntimeFact(ctx, tx, root.id); err != nil {
		return worldRuntimeExecution{}, err
	}
	root.fact.ActorCode = stringPointer(actor.actor.Code)
	pending := cityPendingEvent{
		command: command, status: CityCommandStatusApplied, eventType: eventType,
		payload: map[string]any{"actor_code": actor.actor.Code, "target_user_id": payload.UserID},
		result: map[string]any{
			"applied": true, "actor_code": actor.actor.Code,
			"target_user_id": payload.UserID, "capabilities": capabilities,
		},
	}
	return worldRuntimeExecution{
		pending: pending, facts: []WorldRuntimeFact{root.fact}, effects: operations,
		cases: []WorldRuleCase{}, nextFactSeq: factSequence + 1,
		nextEffectSeq: nextEffectSequence, nextCaseSeq: caseSequence,
	}, nil
}
