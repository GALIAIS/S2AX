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
	cityRealtimeVisualBindingVersion          = "city-realtime-visual-binding-v1"
	cityRealtimeDefaultVisualPackID           = "city-pixel-core"
	cityRealtimeDefaultVisualPackVersion      = "1.0.0"
	cityRealtimeDefaultVisualManifestSchema   = 1
	cityRealtimeProceduralPixelRenderContract = "procedural_pixel_v1"
	cityRealtimeAtlasPixelRenderContract      = "atlas_pixel_v1"
)

// CityRealtimeVisualBinding is a frozen content-plane identity. It deliberately
// excludes asset URLs and generated image metadata: those belong to the
// separately fetched manifest and must never affect collision or world state.
type CityRealtimeVisualBinding struct {
	PackID                    string `json:"pack_id"`
	PackVersion               string `json:"pack_version"`
	SpatialProfileID          string `json:"spatial_profile_id"`
	SemanticProjectionVersion string `json:"semantic_projection_version"`
	RenderContractVersion     string `json:"render_contract_version"`
	ManifestHash              string `json:"manifest_hash"`
	AssetSetHash              string `json:"asset_set_hash"`
	BindingHash               string `json:"binding_hash"`
}

// CityRealtimeVisualManifest is a member-safe, content-addressed renderer
// contract. It contains only a published manifest; operational generation
// jobs, reviewer data, source prompts, and storage credentials never cross
// this API boundary.
type CityRealtimeVisualManifest struct {
	WorldID  int64                     `json:"world_id"`
	Binding  CityRealtimeVisualBinding `json:"binding"`
	Manifest json.RawMessage           `json:"manifest"`
}

type cityRealtimeVisualPackRecord struct {
	Status                    string
	PackID                    string
	PackVersion               string
	SemanticProjectionVersion string
	RenderContractVersion     string
	ManifestHash              string
	AssetSetHash              string
	Compatibility             json.RawMessage
}

// initializeCityRealtimeVisualBinding pins the release-policy-selected
// renderer contract while a V2 world is still inside its genesis transaction.
// There is intentionally no browser parameter for a visual pack: choosing
// assets is a server-side publication decision and not a way to probe files or
// mutate a shared world's appearance. A later policy change affects only the
// next world genesis; this binding is immutable once inserted.
func initializeCityRealtimeVisualBinding(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
) error {
	if worldID <= 0 {
		return ErrCityInvalidInput
	}
	var profileID string
	if err := tx.QueryRowContext(ctx, `
SELECT profile_id
FROM city_realtime_spatial_bindings
WHERE world_id = $1`, worldID).Scan(&profileID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_visual_spatial_binding"})
		}
		return fmt.Errorf("load realtime visual spatial profile: %w", err)
	}
	pack, err := loadCityRealtimeVisualReleasePack(ctx, tx, profileID)
	if err != nil {
		return err
	}
	if !cityRealtimeVisualPackSupports(pack.Compatibility, profileID, pack.SemanticProjectionVersion) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_visual_pack_compatibility"})
	}
	binding := CityRealtimeVisualBinding{
		PackID:                    pack.PackID,
		PackVersion:               pack.PackVersion,
		SpatialProfileID:          profileID,
		SemanticProjectionVersion: pack.SemanticProjectionVersion,
		RenderContractVersion:     pack.RenderContractVersion,
		ManifestHash:              pack.ManifestHash,
		AssetSetHash:              pack.AssetSetHash,
	}
	binding.BindingHash = cityRealtimeVisualBindingHash(binding)
	if err = validateCityRealtimeVisualBinding(binding); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx,
		`SELECT set_config('sub2api.city_realtime_visual_binding_initialize_world_id', $1, TRUE)`,
		fmt.Sprintf("%d", worldID),
	); err != nil {
		return fmt.Errorf("activate realtime visual binding write gate: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_world_visual_bindings
    (world_id, pack_id, pack_version, spatial_profile_id, semantic_projection_version,
     render_contract_version, manifest_hash, asset_set_hash, binding_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, '{}'::jsonb)`,
		worldID, binding.PackID, binding.PackVersion, binding.SpatialProfileID,
		binding.SemanticProjectionVersion, binding.RenderContractVersion,
		binding.ManifestHash, binding.AssetSetHash, binding.BindingHash,
	); err != nil {
		return fmt.Errorf("create realtime visual binding: %w", err)
	}
	return nil
}

// loadCityRealtimeVisualReleasePack resolves the one explicitly published
// release policy for a new V2 world. An exact profile policy takes precedence
// over the wildcard. It intentionally fails closed if that policy is stale or
// retired instead of silently applying another country's visual language.
func loadCityRealtimeVisualReleasePack(
	ctx context.Context,
	tx *sql.Tx,
	profileID string,
) (cityRealtimeVisualPackRecord, error) {
	if tx == nil || !cityRealtimeVisualIdentifierValid(profileID, 64) {
		return cityRealtimeVisualPackRecord{}, ErrCityInvalidInput
	}
	pack := cityRealtimeVisualPackRecord{}
	var compatibility []byte
	err := tx.QueryRowContext(ctx, `
SELECT pack.status,
       pack.pack_id, pack.pack_version, pack.semantic_projection_version,
       pack.render_contract_version, pack.manifest_hash, pack.asset_set_hash,
       pack.compatibility
FROM city_visual_pack_release_policies policy
JOIN city_visual_packs pack
  ON pack.pack_id = policy.pack_id
 AND pack.pack_version = policy.pack_version
WHERE policy.semantic_projection_version = $1
  AND policy.spatial_profile_id IN ($2, '*')
ORDER BY CASE WHEN policy.spatial_profile_id = $2 THEN 0 ELSE 1 END
LIMIT 1`, cityRealtimeSemanticProjectionVersion, profileID).Scan(
		&pack.Status,
		&pack.PackID, &pack.PackVersion, &pack.SemanticProjectionVersion,
		&pack.RenderContractVersion, &pack.ManifestHash, &pack.AssetSetHash,
		&compatibility,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return cityRealtimeVisualPackRecord{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_visual_release_policy"})
	}
	if err != nil {
		return cityRealtimeVisualPackRecord{}, fmt.Errorf("load realtime visual release policy: %w", err)
	}
	pack.Compatibility = append(json.RawMessage(nil), compatibility...)
	if pack.Status != "published" || pack.RenderContractVersion != cityRealtimeProceduralPixelRenderContract {
		return cityRealtimeVisualPackRecord{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_visual_release_pack"})
	}
	if err = validateCityRealtimeVisualPackRecord(pack); err != nil {
		return cityRealtimeVisualPackRecord{}, err
	}
	return pack, nil
}

func loadCityRealtimeVisualBinding(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*CityRealtimeVisualBinding, error) {
	if worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	item := &CityRealtimeVisualBinding{}
	err := queryer.QueryRowContext(ctx, `
SELECT binding.pack_id, binding.pack_version, binding.spatial_profile_id,
       binding.semantic_projection_version, binding.render_contract_version,
       binding.manifest_hash, binding.asset_set_hash, binding.binding_hash
FROM city_world_visual_bindings binding
JOIN city_visual_packs pack
  ON pack.pack_id = binding.pack_id
 AND pack.pack_version = binding.pack_version
 AND pack.manifest_hash = binding.manifest_hash
 AND pack.asset_set_hash = binding.asset_set_hash
 AND pack.semantic_projection_version = binding.semantic_projection_version
 AND pack.render_contract_version = binding.render_contract_version
JOIN city_realtime_spatial_bindings spatial
  ON spatial.world_id = binding.world_id
 AND spatial.profile_id = binding.spatial_profile_id
WHERE binding.world_id = $1
  AND pack.status IN ('published', 'retired')`, worldID).Scan(
		&item.PackID, &item.PackVersion, &item.SpatialProfileID,
		&item.SemanticProjectionVersion, &item.RenderContractVersion,
		&item.ManifestHash, &item.AssetSetHash, &item.BindingHash,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_visual_binding"})
	}
	if err != nil {
		return nil, fmt.Errorf("load realtime visual binding: %w", err)
	}
	if err = validateCityRealtimeVisualBinding(*item); err != nil {
		return nil, err
	}
	return item, nil
}

// GetRealtimeVisualManifest returns the one public pack bound to the shared
// V2 world. Asset retrieval remains content-addressed by a later atlas loader;
// no caller can replace a pack ID, storage path, or version through this API.
func (s *CityEconomyService) GetRealtimeVisualManifest(
	ctx context.Context,
	userID, worldID int64,
) (*CityRealtimeVisualManifest, error) {
	if userID <= 0 || worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, fmt.Errorf("begin city realtime visual manifest transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	world, err := loadCityRealtimeWorldProjection(ctx, tx, userID, worldID)
	if err != nil {
		return nil, err
	}
	item := &CityRealtimeVisualManifest{WorldID: worldID, Binding: world.Visual}
	err = tx.QueryRowContext(ctx, `
SELECT manifest
FROM city_visual_packs
WHERE pack_id = $1 AND pack_version = $2
  AND manifest_hash = $3 AND asset_set_hash = $4
  AND semantic_projection_version = $5 AND render_contract_version = $6
  AND status IN ('published', 'retired')`,
		item.Binding.PackID, item.Binding.PackVersion,
		item.Binding.ManifestHash, item.Binding.AssetSetHash,
		item.Binding.SemanticProjectionVersion, item.Binding.RenderContractVersion,
	).Scan(&item.Manifest)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_visual_manifest"})
	}
	if err != nil {
		return nil, fmt.Errorf("load realtime visual manifest: %w", err)
	}
	if err = validateCityRealtimeVisualManifest(item.Manifest, item.Binding.RenderContractVersion); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit city realtime visual manifest transaction: %w", err)
	}
	return item, nil
}

func validateCityRealtimeVisualPackRecord(pack cityRealtimeVisualPackRecord) error {
	if !cityRealtimeVisualIdentifierValid(pack.PackID, 96) ||
		!cityRealtimeVisualVersionValid(pack.PackVersion) ||
		!cityRealtimeSHA256Hex(pack.ManifestHash) || !cityRealtimeSHA256Hex(pack.AssetSetHash) ||
		pack.SemanticProjectionVersion != cityRealtimeSemanticProjectionVersion ||
		!cityRealtimeVisualRenderContractValid(pack.RenderContractVersion) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_visual_pack"})
	}
	return nil
}

func validateCityRealtimeVisualBinding(binding CityRealtimeVisualBinding) error {
	if !cityRealtimeVisualIdentifierValid(binding.PackID, 96) ||
		!cityRealtimeVisualVersionValid(binding.PackVersion) ||
		!cityRealtimeVisualIdentifierValid(binding.SpatialProfileID, 64) ||
		binding.SemanticProjectionVersion != cityRealtimeSemanticProjectionVersion ||
		!cityRealtimeVisualRenderContractValid(binding.RenderContractVersion) ||
		!cityRealtimeSHA256Hex(binding.ManifestHash) || !cityRealtimeSHA256Hex(binding.AssetSetHash) ||
		!cityRealtimeSHA256Hex(binding.BindingHash) ||
		binding.BindingHash != cityRealtimeVisualBindingHash(binding) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_visual_binding"})
	}
	return nil
}

func validateCityRealtimeVisualManifest(raw json.RawMessage, contract string) error {
	if !cityRealtimeVisualRenderContractValid(contract) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_visual_render_contract"})
	}
	var manifest struct {
		SchemaVersion   int                          `json:"schema_version"`
		RenderMode      string                       `json:"render_mode"`
		LogicalTilePX   int                          `json:"logical_tile_px"`
		ProfilePalettes map[string]map[string]string `json:"profile_palettes"`
		Assets          json.RawMessage              `json:"assets"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &manifest) != nil ||
		manifest.SchemaVersion != cityRealtimeDefaultVisualManifestSchema ||
		manifest.RenderMode != contract ||
		manifest.LogicalTilePX != 16 ||
		len(manifest.ProfilePalettes) == 0 || manifest.ProfilePalettes["default"] == nil ||
		!cityRealtimeVisualJSONArray(manifest.Assets) ||
		cityRealtimeVisualManifestContainsUnsafeTransport(raw) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_visual_manifest"})
	}
	for profileID, palette := range manifest.ProfilePalettes {
		if (profileID != "default" && !cityRealtimeVisualIdentifierValid(profileID, 64)) || len(palette) == 0 {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_visual_manifest"})
		}
		for semanticKey, color := range palette {
			if !cityRealtimeVisualPaletteKeyAllowed(semanticKey) || !cityRealtimeVisualHexColor(color) {
				return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_visual_manifest"})
			}
		}
	}
	return nil
}

func cityRealtimeVisualPackSupports(raw json.RawMessage, profileID, semanticVersion string) bool {
	var compatibility struct {
		SpatialProfileIDs          []string `json:"spatial_profile_ids"`
		SemanticProjectionVersions []string `json:"semantic_projection_versions"`
	}
	if json.Unmarshal(raw, &compatibility) != nil ||
		!cityRealtimeVisualIdentifierValid(profileID, 64) ||
		semanticVersion != cityRealtimeSemanticProjectionVersion {
		return false
	}
	return cityRealtimeVisualStringIncluded(compatibility.SpatialProfileIDs, profileID, true) &&
		cityRealtimeVisualStringIncluded(compatibility.SemanticProjectionVersions, semanticVersion, false)
}

func cityRealtimeVisualStringIncluded(values []string, target string, allowWildcard bool) bool {
	for _, value := range values {
		if value == target || (allowWildcard && value == "*") {
			return true
		}
	}
	return false
}

func cityRealtimeVisualBindingHash(binding CityRealtimeVisualBinding) string {
	return cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeVisualBindingVersion,
		binding.PackID,
		binding.PackVersion,
		binding.SpatialProfileID,
		binding.SemanticProjectionVersion,
		binding.RenderContractVersion,
		binding.ManifestHash,
		binding.AssetSetHash,
	}, "\x1f")))
}

func cityRealtimeVisualIdentifierValid(value string, maximumLength int) bool {
	if maximumLength <= 0 || len(value) < 2 || len(value) > maximumLength {
		return false
	}
	for index, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') ||
			(character == '.' || character == '_' || character == '-') && index > 0 {
			continue
		}
		return false
	}
	return value[0] >= 'a' && value[0] <= 'z'
}

func cityRealtimeVisualVersionValid(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" || len(part) > 8 {
			return false
		}
		for _, character := range part {
			if character < '0' || character > '9' {
				return false
			}
		}
	}
	return true
}

func cityRealtimeVisualRenderContractValid(value string) bool {
	return value == cityRealtimeProceduralPixelRenderContract || value == cityRealtimeAtlasPixelRenderContract
}

func cityRealtimeVisualJSONArray(raw json.RawMessage) bool {
	value := strings.TrimSpace(string(raw))
	if len(value) == 0 || value[0] != '[' {
		return false
	}
	var values []json.RawMessage
	return json.Unmarshal([]byte(value), &values) == nil
}

func cityRealtimeVisualManifestContainsUnsafeTransport(raw json.RawMessage) bool {
	value := strings.ToLower(string(raw))
	return strings.Contains(value, "http://") || strings.Contains(value, "https://") ||
		strings.Contains(value, "data:") || strings.Contains(value, "javascript:") ||
		strings.Contains(value, "<svg") || strings.Contains(value, "<script")
}

func cityRealtimeVisualPaletteKeyAllowed(value string) bool {
	switch value {
	case "map_background", "ground", "soil", "road", "water",
		"building_residential", "building_commercial", "building_industrial",
		"structure", "portal", "furniture", "item", "entity", "overlay", "window":
		return true
	default:
		return false
	}
}

func cityRealtimeVisualHexColor(value string) bool {
	if len(value) != 7 || value[0] != '#' {
		return false
	}
	for _, character := range value[1:] {
		if (character < '0' || character > '9') &&
			(character < 'a' || character > 'f') &&
			(character < 'A' || character > 'F') {
			return false
		}
	}
	return true
}
