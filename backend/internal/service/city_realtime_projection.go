package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/cityspatial"
)

const cityRealtimeSemanticProjectionVersion = "city-realtime-semantic-pixel-v1"

// CityRealtimeViewerScope is deliberately self-scoped. It does not include
// any roster, email, username, actor-control grant, or operational-worker
// data, so a member can join the same world without learning other players'
// private account facts.
type CityRealtimeViewerScope struct {
	MembershipRole       string `json:"membership_role"`
	CanViewSharedWorld   bool   `json:"can_view_shared_world"`
	CanManageWorld       bool   `json:"can_manage_world"`
	RedactionPolicy      string `json:"redaction_policy"`
	ProjectionScopeEpoch int64  `json:"projection_scope_epoch"`
}

// CityRealtimeWorldProjection is the read-side handshake for a shared
// realtime V2 world. A renderer first obtains immutable semantic and visual
// binding identities, then fetches the published visual manifest, chunks, and
// cursor patches separately. Images remain outside canonical physical state.
type CityRealtimeWorldProjection struct {
	WorldID                   int64                      `json:"world_id"`
	WorldStatus               string                     `json:"world_status"`
	TemporalEngineVersion     string                     `json:"temporal_engine_version"`
	TimelineFrameSequence     int64                      `json:"timeline_frame_sequence"`
	TimelineCursor            string                     `json:"timeline_cursor"`
	SemanticProjectionVersion string                     `json:"semantic_projection_version"`
	StaticProjectionHash      string                     `json:"static_projection_hash"`
	Viewer                    CityRealtimeViewerScope    `json:"viewer"`
	Spatial                   CityRealtimeSpatialBinding `json:"spatial"`
	Visual                    CityRealtimeVisualBinding  `json:"visual"`
}

type CityRealtimePixelChunkInput struct {
	UserID  int64
	WorldID int64
	ChunkX  int64
	ChunkY  int64
	Z       int32
}

// CityRealtimePixelChunkProjection is a semantic, not image, chunk. The
// browser can resolve terrain/layer definitions through a visual manifest in a
// later renderer phase, but it cannot invent collisions, doors, or buildings.
type CityRealtimePixelChunkProjection struct {
	WorldID                   int64                          `json:"world_id"`
	TimelineFrameSequence     int64                          `json:"timeline_frame_sequence"`
	TimelineCursor            string                         `json:"timeline_cursor"`
	SemanticProjectionVersion string                         `json:"semantic_projection_version"`
	StaticProjectionHash      string                         `json:"static_projection_hash"`
	Chunk                     CityRealtimeSemanticChunk      `json:"chunk"`
	Buildings                 []CityRealtimeSemanticBuilding `json:"buildings"`
}

type CityRealtimeSemanticChunk struct {
	ChunkX      int64           `json:"chunk_x"`
	ChunkY      int64           `json:"chunk_y"`
	Z           int32           `json:"z"`
	Payload     json.RawMessage `json:"payload"`
	PayloadHash string          `json:"payload_hash"`
	Revision    int64           `json:"revision"`
}

type CityRealtimeSemanticBuilding struct {
	Code          string                      `json:"code"`
	PrimaryUse    string                      `json:"primary_use"`
	ArchetypeCode string                      `json:"archetype_code"`
	LayoutStyle   string                      `json:"layout_style"`
	FloorCount    int32                       `json:"floor_count"`
	Entrance      cityspatial.WorldgenPoint   `json:"entrance"`
	Footprint     []cityspatial.WorldgenPoint `json:"footprint"`
	FootprintHash string                      `json:"footprint_hash"`
	Revision      int64                       `json:"revision"`
}

type CityRealtimePatchListInput struct {
	UserID             int64
	WorldID            int64
	AfterFrameSequence int64
	Limit              int
}

// CityRealtimePatchPage is the cursor-pull contract. R2's static map never
// changes after genesis, so frames carry temporal deltas while the immutable
// StaticProjectionHash tells a client whether its cached semantic chunks still
// match the shared world's baseline.
type CityRealtimePatchPage struct {
	WorldID                int64                `json:"world_id"`
	AfterFrameSequence     int64                `json:"after_frame_sequence"`
	CurrentFrameSequence   int64                `json:"current_frame_sequence"`
	CurrentCursor          string               `json:"current_cursor"`
	StaticProjectionHash   string               `json:"static_projection_hash"`
	FullResyncRequired     bool                 `json:"full_resync_required"`
	Items                  []*CityTemporalFrame `json:"items"`
	NextAfterFrameSequence *int64               `json:"next_after_frame_sequence,omitempty"`
}

// GetRealtimeWorldProjection returns the member-safe shared-world handshake.
func (s *CityEconomyService) GetRealtimeWorldProjection(
	ctx context.Context,
	userID, worldID int64,
) (*CityRealtimeWorldProjection, error) {
	if userID <= 0 || worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, fmt.Errorf("begin city realtime projection transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	item, err := loadCityRealtimeWorldProjection(ctx, tx, userID, worldID)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit city realtime projection transaction: %w", err)
	}
	return item, nil
}

// GetRealtimePixelChunk returns one canonical semantic surface chunk plus the
// building footprints intersecting it. It uses one repeatable-read snapshot so
// its cursor, static binding, chunk and building facts cannot come from
// different database moments.
func (s *CityEconomyService) GetRealtimePixelChunk(
	ctx context.Context,
	input CityRealtimePixelChunkInput,
) (*CityRealtimePixelChunkProjection, error) {
	if input.UserID <= 0 || input.WorldID <= 0 || input.Z != cityspatial.SurfaceZ {
		return nil, ErrCityInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, fmt.Errorf("begin city realtime pixel chunk transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	world, err := loadCityRealtimeWorldProjection(ctx, tx, input.UserID, input.WorldID)
	if err != nil {
		return nil, err
	}
	item, err := loadCityRealtimePixelChunk(ctx, tx, world, input)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit city realtime pixel chunk transaction: %w", err)
	}
	return item, nil
}

// ListRealtimePatches is the cursor-pull transport for an online shared
// world. It exposes the same safe temporal summaries as /timeline and makes
// initial chunk bootstrap explicit instead of inferring it from a missing
// browser cache.
func (s *CityEconomyService) ListRealtimePatches(
	ctx context.Context,
	input CityRealtimePatchListInput,
) (*CityRealtimePatchPage, error) {
	if input.UserID <= 0 || input.WorldID <= 0 || input.AfterFrameSequence < -1 {
		return nil, ErrCityInvalidInput
	}
	if input.Limit <= 0 {
		input.Limit = cityRealtimeDefaultTimelineLimit
	}
	if input.Limit > cityRealtimeMaximumTimelineLimit {
		return nil, ErrCityInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, fmt.Errorf("begin city realtime patch transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	world, err := loadCityRealtimeWorldProjection(ctx, tx, input.UserID, input.WorldID)
	if err != nil {
		return nil, err
	}
	if input.AfterFrameSequence > world.TimelineFrameSequence {
		return nil, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "after_frame_sequence"})
	}
	rows, err := tx.QueryContext(ctx, `
SELECT frame.world_id, frame.frame_sequence, frame.timeline_cursor,
       frame.world_time_from_us, frame.world_time_to_us,
       segment.segment_sequence, frame.frame_kind, frame.state_hash,
       frame.previous_state_hash, frame.due_event_digest, frame.phase_summary,
       frame.effective_utc_from, frame.effective_utc_to, frame.created_at
FROM city_temporal_frames frame
JOIN city_world_clock_segments segment
  ON segment.world_id = frame.world_id AND segment.id = frame.clock_segment_id
WHERE frame.world_id = $1 AND frame.frame_sequence > $2
ORDER BY frame.frame_sequence ASC
LIMIT $3`, input.WorldID, input.AfterFrameSequence, input.Limit+1)
	if err != nil {
		return nil, fmt.Errorf("list city realtime patches: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]*CityTemporalFrame, 0, input.Limit)
	for rows.Next() {
		item, scanErr := scanCityTemporalFrame(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate city realtime patches: %w", err)
	}
	page := &CityRealtimePatchPage{
		WorldID:              input.WorldID,
		AfterFrameSequence:   input.AfterFrameSequence,
		CurrentFrameSequence: world.TimelineFrameSequence,
		CurrentCursor:        world.TimelineCursor,
		StaticProjectionHash: world.StaticProjectionHash,
		FullResyncRequired:   input.AfterFrameSequence == -1,
		Items:                items,
	}
	if len(page.Items) > input.Limit {
		page.Items = page.Items[:input.Limit]
		next := page.Items[len(page.Items)-1].FrameSequence
		page.NextAfterFrameSequence = &next
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit city realtime patch transaction: %w", err)
	}
	return page, nil
}

func loadCityRealtimeWorldProjection(
	ctx context.Context,
	queryer citySQLQueryer,
	userID, worldID int64,
) (*CityRealtimeWorldProjection, error) {
	if err := requireCityRealtimeWorldRead(ctx, queryer, userID, worldID); err != nil {
		return nil, err
	}
	item := &CityRealtimeWorldProjection{
		WorldID:                   worldID,
		SemanticProjectionVersion: cityRealtimeSemanticProjectionVersion,
	}
	err := queryer.QueryRowContext(ctx, `
SELECT world.status, state.temporal_engine_version,
       state.timeline_frame_sequence, state.timeline_cursor
FROM city_worlds world
JOIN city_world_time_states state ON state.world_id = world.id
WHERE world.id = $1`, worldID).Scan(
		&item.WorldStatus, &item.TemporalEngineVersion,
		&item.TimelineFrameSequence, &item.TimelineCursor,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_time_state"})
	}
	if err != nil {
		return nil, fmt.Errorf("load city realtime world projection: %w", err)
	}
	if !cityEngineSupportsRealtimeStaticWorldgen(item.TemporalEngineVersion) {
		return nil, ErrCityRealtimeStaticWorldRequired.WithMetadata(map[string]string{"version": item.TemporalEngineVersion})
	}
	expectedCursor, err := cityRealtimeTimelineCursor(item.TimelineFrameSequence)
	if err != nil || item.TimelineCursor != expectedCursor {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_timeline_cursor"})
	}
	spatial, err := loadCityRealtimeSpatialBinding(ctx, queryer, worldID)
	if err != nil {
		return nil, err
	}
	item.Spatial = *spatial
	item.StaticProjectionHash = item.Spatial.GenesisHash
	visual, err := loadCityRealtimeVisualBinding(ctx, queryer, worldID)
	if err != nil {
		return nil, err
	}
	item.Visual = *visual
	item.Viewer, err = loadCityRealtimeViewerScope(ctx, queryer, userID, worldID)
	if err != nil {
		return nil, err
	}
	return item, nil
}

func loadCityRealtimeViewerScope(
	ctx context.Context,
	queryer citySQLQueryer,
	userID, worldID int64,
) (CityRealtimeViewerScope, error) {
	scope := CityRealtimeViewerScope{
		CanViewSharedWorld:   true,
		RedactionPolicy:      "member_safe_v1",
		ProjectionScopeEpoch: 1,
	}
	if IsCitySystemAdministrator(ctx) {
		scope.MembershipRole = "system_administrator"
		scope.CanManageWorld = true
		return scope, nil
	}
	if err := queryer.QueryRowContext(ctx, `
SELECT role
FROM city_members
WHERE world_id = $1 AND user_id = $2 AND status = 'active'`, worldID, userID).Scan(&scope.MembershipRole); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return CityRealtimeViewerScope{}, ErrCityWorldNotFound
		}
		return CityRealtimeViewerScope{}, fmt.Errorf("load city realtime viewer scope: %w", err)
	}
	scope.CanManageWorld = scope.MembershipRole == CityMemberRoleOwner
	return scope, nil
}

func loadCityRealtimePixelChunk(
	ctx context.Context,
	queryer citySQLQueryer,
	world *CityRealtimeWorldProjection,
	input CityRealtimePixelChunkInput,
) (*CityRealtimePixelChunkProjection, error) {
	if world == nil || !cityEngineSupportsRealtimeStaticWorldgen(world.TemporalEngineVersion) {
		return nil, ErrCityRealtimeStaticWorldRequired
	}
	item := &CityRealtimePixelChunkProjection{
		WorldID:                   input.WorldID,
		TimelineFrameSequence:     world.TimelineFrameSequence,
		TimelineCursor:            world.TimelineCursor,
		SemanticProjectionVersion: cityRealtimeSemanticProjectionVersion,
		StaticProjectionHash:      world.StaticProjectionHash,
		Buildings:                 make([]CityRealtimeSemanticBuilding, 0),
	}
	var sectorX, sectorY int64
	var rawPayload []byte
	err := queryer.QueryRowContext(ctx, `
SELECT sector_x, sector_y, payload, payload_hash, revision
FROM city_realtime_spatial_chunks
WHERE world_id = $1 AND chunk_x = $2 AND chunk_y = $3 AND z = $4`,
		input.WorldID, input.ChunkX, input.ChunkY, input.Z,
	).Scan(&sectorX, &sectorY, &rawPayload, &item.Chunk.PayloadHash, &item.Chunk.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCityWorldNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load city realtime pixel chunk: %w", err)
	}
	var payload cityspatial.OpenWorldChunkPayload
	if err = json.Unmarshal(rawPayload, &payload); err != nil {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_static_chunk_payload"}).WithCause(err)
	}
	if err = cityspatial.ValidateOpenWorldChunkPayload(payload); err != nil {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_static_chunk_payload"}).WithCause(err)
	}
	canonicalPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("canonicalize city realtime pixel chunk: %w", err)
	}
	if cityOpenWorldPayloadHash(canonicalPayload) != item.Chunk.PayloadHash {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_static_chunk_hash"})
	}
	item.Chunk.ChunkX = input.ChunkX
	item.Chunk.ChunkY = input.ChunkY
	item.Chunk.Z = input.Z
	item.Chunk.Payload = canonicalPayload
	buildingRows, err := queryer.QueryContext(ctx, `
SELECT code, primary_use, archetype_code, layout_style, floor_count,
       entrance_x, entrance_y, entrance_z, footprint, footprint_hash, revision
FROM city_realtime_spatial_buildings
WHERE world_id = $1 AND sector_x = $2 AND sector_y = $3
ORDER BY code ASC`, input.WorldID, sectorX, sectorY)
	if err != nil {
		return nil, fmt.Errorf("load city realtime pixel chunk buildings: %w", err)
	}
	defer func() { _ = buildingRows.Close() }()
	for buildingRows.Next() {
		var building CityRealtimeSemanticBuilding
		var footprintRaw []byte
		if err = buildingRows.Scan(&building.Code, &building.PrimaryUse, &building.ArchetypeCode, &building.LayoutStyle,
			&building.FloorCount, &building.Entrance.X, &building.Entrance.Y, &building.Entrance.Z,
			&footprintRaw, &building.FootprintHash, &building.Revision); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(footprintRaw, &building.Footprint); err != nil {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_static_building_footprint"}).WithCause(err)
		}
		canonicalFootprint, marshalErr := json.Marshal(building.Footprint)
		if marshalErr != nil {
			return nil, fmt.Errorf("canonicalize city realtime building %s: %w", building.Code, marshalErr)
		}
		if cityOpenWorldPayloadHash(canonicalFootprint) != building.FootprintHash || building.Revision != 1 {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_static_building_hash"})
		}
		if cityOpenWorldFootprintIntersectsChunkWindow(
			building.Footprint, input.ChunkX, input.ChunkX, input.ChunkY, input.ChunkY,
		) {
			item.Buildings = append(item.Buildings, building)
		}
	}
	if err = buildingRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate city realtime pixel chunk buildings: %w", err)
	}
	return item, nil
}
