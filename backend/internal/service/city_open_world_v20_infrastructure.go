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
)

const (
	cityOpenWorldInfrastructureSchemaVersion       = 1
	cityOpenWorldInfrastructureProfileID           = "sub2api-open-world-infrastructure-assets"
	cityOpenWorldInfrastructureProfileVersion      = "1.0.0"
	cityOpenWorldInfrastructureAssetContract       = "v19_node_corridor_asset_seed_v1"
	cityOpenWorldInfrastructureStateContract       = "append_only_asset_transition_state_v1"
	cityOpenWorldInfrastructureMaximumAssets       = 65536
	cityOpenWorldInfrastructureAssetKindNode       = "network_node"
	cityOpenWorldInfrastructureAssetKindSegment    = "corridor_segment"
	cityOpenWorldInfrastructureStateOperational    = "operational"
	cityOpenWorldInfrastructureStateRestricted     = "restricted"
	cityOpenWorldInfrastructureStateMaintenance    = "maintenance"
	cityOpenWorldInfrastructureStateConstruction   = "construction"
	cityOpenWorldInfrastructureStateClosed         = "closed"
	cityOpenWorldInfrastructureReasonBaseline      = "baseline_initialized"
	cityOpenWorldInfrastructureFactAssetTransition = "infrastructure.asset.transitioned"
)

// CityOpenWorldInfrastructurePolicy pins V20's generic mutable asset
// protocol. It intentionally does not give V9's scheduler permission to use
// CapacityMilli yet; a later engine version must make that bridge explicit.
type CityOpenWorldInfrastructurePolicy struct {
	ProfileID          string          `json:"profile_id"`
	ProfileVersion     string          `json:"profile_version"`
	ContentHash        string          `json:"content_hash"`
	BaselineTick       int64           `json:"baseline_tick"`
	AssetContract      string          `json:"asset_contract"`
	StateContract      string          `json:"state_contract"`
	MaximumAssets      int             `json:"maximum_assets"`
	AssetCount         int64           `json:"asset_count"`
	NodeAssetCount     int64           `json:"node_asset_count"`
	SegmentAssetCount  int64           `json:"segment_asset_count"`
	TransitionCount    int64           `json:"transition_count"`
	Revision           int64           `json:"revision"`
	Metadata           json.RawMessage `json:"metadata"`
}

// CityOpenWorldInfrastructureAsset is immutable V20 identity. A node asset
// belongs to exactly one V19 node; a segment asset belongs to one V19
// corridor and has an ordered segment ordinal. V20 seeds ordinal one only.
type CityOpenWorldInfrastructureAsset struct {
	Code                string          `json:"code"`
	AssetKind           string          `json:"asset_kind"`
	SpatialNodeCode     *string         `json:"spatial_node_code,omitempty"`
	SpatialCorridorCode *string         `json:"spatial_corridor_code,omitempty"`
	SegmentOrdinal      int             `json:"segment_ordinal"`
	AssetClass          string          `json:"asset_class"`
	DefinitionVersion   string          `json:"definition_version"`
	ContentHash         string          `json:"content_hash"`
	Metadata            json.RawMessage `json:"metadata"`
}

// CityOpenWorldInfrastructureAssetState is the authoritative current state
// projection. SourceFact is nil only for the deterministic baseline row.
type CityOpenWorldInfrastructureAssetState struct {
	AssetCode     string                       `json:"asset_code"`
	State         string                       `json:"state"`
	CapacityMilli int64                        `json:"capacity_milli"`
	EffectiveTick int64                        `json:"effective_tick"`
	SourceFact    *CityOpenWorldRuntimeFactRef `json:"source_fact,omitempty"`
	Version       int64                        `json:"version"`
	Metadata      json.RawMessage              `json:"metadata"`
}

// CityOpenWorldInfrastructureAssetTransition preserves the state timeline.
// The baseline transition has an empty FromState and no SourceFact; all
// normal transitions are command-backed runtime facts.
type CityOpenWorldInfrastructureAssetTransition struct {
	AssetCode      string                       `json:"asset_code"`
	TransitionTick int64                        `json:"transition_tick"`
	TransitionSeq  int64                        `json:"transition_sequence"`
	FromState      string                       `json:"from_state"`
	ToState        string                       `json:"to_state"`
	CapacityMilli  int64                        `json:"capacity_milli"`
	ReasonCode     string                       `json:"reason_code"`
	SourceFact     *CityOpenWorldRuntimeFactRef `json:"source_fact,omitempty"`
	Metadata       json.RawMessage              `json:"metadata"`
}

type CityOpenWorldInfrastructureState struct {
	Policy      CityOpenWorldInfrastructurePolicy            `json:"policy"`
	Assets      []CityOpenWorldInfrastructureAsset           `json:"assets"`
	States      []CityOpenWorldInfrastructureAssetState      `json:"states"`
	Transitions []CityOpenWorldInfrastructureAssetTransition `json:"transitions"`
}

func cityOpenWorldInfrastructurePolicyHash() (string, error) {
	raw, err := json.Marshal(struct {
		SchemaVersion int    `json:"schema_version"`
		ProfileID     string `json:"profile_id"`
		ProfileVersion string `json:"profile_version"`
		AssetContract string `json:"asset_contract"`
		StateContract string `json:"state_contract"`
		MaximumAssets int    `json:"maximum_assets"`
	}{
		SchemaVersion: cityOpenWorldInfrastructureSchemaVersion,
		ProfileID:     cityOpenWorldInfrastructureProfileID,
		ProfileVersion: cityOpenWorldInfrastructureProfileVersion,
		AssetContract: cityOpenWorldInfrastructureAssetContract,
		StateContract: cityOpenWorldInfrastructureStateContract,
		MaximumAssets: cityOpenWorldInfrastructureMaximumAssets,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func cityOpenWorldInfrastructureNodeAssetCode(nodeCode string) string {
	sum := sha256.Sum256([]byte("v20.infrastructure.node\x00" + nodeCode))
	return "infrastructure.asset.node." + hex.EncodeToString(sum[:20])
}

func cityOpenWorldInfrastructureSegmentAssetCode(corridorCode string, ordinal int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("v20.infrastructure.segment\x00%s\x00%d", corridorCode, ordinal)))
	return "infrastructure.asset.segment." + hex.EncodeToString(sum[:20])
}

func cityOpenWorldInfrastructureAssetContentHash(asset CityOpenWorldInfrastructureAsset) (string, error) {
	raw, err := json.Marshal(struct {
		Code                string  `json:"code"`
		AssetKind           string  `json:"asset_kind"`
		SpatialNodeCode     *string `json:"spatial_node_code,omitempty"`
		SpatialCorridorCode *string `json:"spatial_corridor_code,omitempty"`
		SegmentOrdinal      int     `json:"segment_ordinal"`
		AssetClass          string  `json:"asset_class"`
		DefinitionVersion   string  `json:"definition_version"`
	}{
		Code: asset.Code, AssetKind: asset.AssetKind, SpatialNodeCode: asset.SpatialNodeCode,
		SpatialCorridorCode: asset.SpatialCorridorCode, SegmentOrdinal: asset.SegmentOrdinal,
		AssetClass: asset.AssetClass, DefinitionVersion: asset.DefinitionVersion,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func cityOpenWorldInfrastructureAssetMetadata(kind, sourceCode string) (json.RawMessage, error) {
	return json.Marshal(map[string]any{
		"schema_version": cityOpenWorldInfrastructureSchemaVersion,
		"source":         "v19_spatial_network",
		"asset_kind":     kind,
		"source_code":    sourceCode,
	})
}

func cityOpenWorldInfrastructureBaselineMetadata() (json.RawMessage, error) {
	return json.Marshal(map[string]any{
		"schema_version": cityOpenWorldInfrastructureSchemaVersion,
		"origin":         "baseline",
		"scheduler":      "not_consumed_by_v9",
	})
}

func cityOpenWorldInfrastructureCommandMetadata(previousState string) (json.RawMessage, error) {
	return json.Marshal(map[string]any{
		"schema_version": cityOpenWorldInfrastructureSchemaVersion,
		"origin":         "command",
		"previous_state": previousState,
		"scheduler":      "not_consumed_by_v9",
	})
}

func cityOpenWorldInfrastructureTransitionAllowed(from, to string) bool {
	switch from {
	case cityOpenWorldInfrastructureStateOperational:
		return to == cityOpenWorldInfrastructureStateRestricted || to == cityOpenWorldInfrastructureStateMaintenance || to == cityOpenWorldInfrastructureStateClosed
	case cityOpenWorldInfrastructureStateRestricted:
		return to == cityOpenWorldInfrastructureStateOperational || to == cityOpenWorldInfrastructureStateMaintenance || to == cityOpenWorldInfrastructureStateClosed
	case cityOpenWorldInfrastructureStateMaintenance:
		return to == cityOpenWorldInfrastructureStateOperational || to == cityOpenWorldInfrastructureStateClosed
	case cityOpenWorldInfrastructureStateClosed:
		return to == cityOpenWorldInfrastructureStateConstruction
	case cityOpenWorldInfrastructureStateConstruction:
		return to == cityOpenWorldInfrastructureStateOperational || to == cityOpenWorldInfrastructureStateClosed
	default:
		return false
	}
}

func cityOpenWorldInfrastructureStateCapacityValid(state string, capacityMilli int64) bool {
	switch state {
	case cityOpenWorldInfrastructureStateOperational:
		return capacityMilli == 1000
	case cityOpenWorldInfrastructureStateRestricted:
		return capacityMilli >= 1 && capacityMilli <= 999
	case cityOpenWorldInfrastructureStateMaintenance,
		cityOpenWorldInfrastructureStateConstruction,
		cityOpenWorldInfrastructureStateClosed:
		return capacityMilli == 0
	default:
		return false
	}
}

func cityOpenWorldInfrastructureStateMetadataValid(raw json.RawMessage, origin string, previousState *string) bool {
	var metadata struct {
		SchemaVersion int    `json:"schema_version"`
		Origin        string `json:"origin"`
		PreviousState string `json:"previous_state"`
		Scheduler     string `json:"scheduler"`
	}
	if json.Unmarshal(raw, &metadata) != nil || metadata.SchemaVersion != cityOpenWorldInfrastructureSchemaVersion ||
		metadata.Origin != origin || metadata.Scheduler != "not_consumed_by_v9" {
		return false
	}
	return previousState == nil || metadata.PreviousState == *previousState
}

func cityOpenWorldInfrastructureAssetMetadataValid(raw json.RawMessage, asset CityOpenWorldInfrastructureAsset) bool {
	var metadata struct {
		SchemaVersion int    `json:"schema_version"`
		Source        string `json:"source"`
		AssetKind     string `json:"asset_kind"`
		SourceCode    string `json:"source_code"`
	}
	if json.Unmarshal(raw, &metadata) != nil || metadata.SchemaVersion != cityOpenWorldInfrastructureSchemaVersion ||
		metadata.Source != "v19_spatial_network" || metadata.AssetKind != asset.AssetKind {
		return false
	}
	if asset.AssetKind == cityOpenWorldInfrastructureAssetKindNode && asset.SpatialNodeCode != nil {
		return metadata.SourceCode == *asset.SpatialNodeCode
	}
	return asset.AssetKind == cityOpenWorldInfrastructureAssetKindSegment &&
		asset.SpatialCorridorCode != nil && metadata.SourceCode == *asset.SpatialCorridorCode
}

func buildCityOpenWorldInfrastructureAssets(network *CityOpenWorldSpatialNetworkState) ([]CityOpenWorldInfrastructureAsset, error) {
	if err := validateCityOpenWorldSpatialNetworkState(network); err != nil {
		return nil, fmt.Errorf("validate V19 spatial-network prerequisite: %w", err)
	}
	assets := make([]CityOpenWorldInfrastructureAsset, 0, len(network.Nodes)+len(network.Corridors))
	for _, node := range network.Nodes {
		nodeCode := node.Code
		metadata, err := cityOpenWorldInfrastructureAssetMetadata(cityOpenWorldInfrastructureAssetKindNode, node.Code)
		if err != nil {
			return nil, err
		}
		asset := CityOpenWorldInfrastructureAsset{
			Code: cityOpenWorldInfrastructureNodeAssetCode(node.Code), AssetKind: cityOpenWorldInfrastructureAssetKindNode,
			SpatialNodeCode: &nodeCode, SegmentOrdinal: 0, AssetClass: "node." + node.NodeClass,
			DefinitionVersion: cityOpenWorldInfrastructureProfileVersion, Metadata: metadata,
		}
		asset.ContentHash, err = cityOpenWorldInfrastructureAssetContentHash(asset)
		if err != nil {
			return nil, err
		}
		assets = append(assets, asset)
	}
	for _, corridor := range network.Corridors {
		corridorCode := corridor.Code
		metadata, err := cityOpenWorldInfrastructureAssetMetadata(cityOpenWorldInfrastructureAssetKindSegment, corridor.Code)
		if err != nil {
			return nil, err
		}
		asset := CityOpenWorldInfrastructureAsset{
			Code: cityOpenWorldInfrastructureSegmentAssetCode(corridor.Code, 1), AssetKind: cityOpenWorldInfrastructureAssetKindSegment,
			SpatialCorridorCode: &corridorCode, SegmentOrdinal: 1, AssetClass: "segment." + corridor.CorridorClass,
			DefinitionVersion: cityOpenWorldInfrastructureProfileVersion, Metadata: metadata,
		}
		asset.ContentHash, err = cityOpenWorldInfrastructureAssetContentHash(asset)
		if err != nil {
			return nil, err
		}
		assets = append(assets, asset)
	}
	sort.Slice(assets, func(i, j int) bool { return assets[i].Code < assets[j].Code })
	if len(assets) == 0 || len(assets) > cityOpenWorldInfrastructureMaximumAssets {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v20_infrastructure_assets"})
	}
	return assets, nil
}

func cityOpenWorldInfrastructureBaselineState(assetCode string, baselineTick int64, metadata json.RawMessage) CityOpenWorldInfrastructureAssetState {
	return CityOpenWorldInfrastructureAssetState{
		AssetCode: assetCode, State: cityOpenWorldInfrastructureStateOperational, CapacityMilli: 1000,
		EffectiveTick: baselineTick, Version: 1, Metadata: append(json.RawMessage(nil), metadata...),
	}
}

func cityOpenWorldInfrastructureBaselineTransition(assetCode string, baselineTick int64, metadata json.RawMessage) CityOpenWorldInfrastructureAssetTransition {
	return CityOpenWorldInfrastructureAssetTransition{
		AssetCode: assetCode, TransitionTick: baselineTick, TransitionSeq: 0, FromState: "",
		ToState: cityOpenWorldInfrastructureStateOperational, CapacityMilli: 1000,
		ReasonCode: cityOpenWorldInfrastructureReasonBaseline, Metadata: append(json.RawMessage(nil), metadata...),
	}
}

func activateCityOpenWorldInfrastructureBootstrapWrite(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if _, err := tx.ExecContext(ctx, `
SELECT set_config('sub2api.city_open_world_infrastructure_bootstrap_world_id', $1, TRUE)`, fmt.Sprintf("%d", worldID)); err != nil {
		return fmt.Errorf("enable open-world V20 infrastructure bootstrap: %w", err)
	}
	return nil
}

func activateCityOpenWorldInfrastructureWrite(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if _, err := tx.ExecContext(ctx, `
SELECT set_config('sub2api.city_open_world_infrastructure_world_id', $1, TRUE)`, fmt.Sprintf("%d", worldID)); err != nil {
		return fmt.Errorf("enable open-world V20 infrastructure write: %w", err)
	}
	return nil
}

func activateCityOpenWorldInfrastructureRecoveryWrite(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if _, err := tx.ExecContext(ctx, `
SELECT set_config('sub2api.city_open_world_infrastructure_recovery_world_id', $1, TRUE)`, fmt.Sprintf("%d", worldID)); err != nil {
		return fmt.Errorf("enable open-world V20 infrastructure recovery: %w", err)
	}
	return nil
}

func assertCityOpenWorldInfrastructureFoundation(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if _, err := tx.ExecContext(ctx, `SELECT assert_city_open_world_infrastructure_foundation($1)`, worldID); err != nil {
		return fmt.Errorf("validate V20 infrastructure foundation: %w", err)
	}
	return nil
}

func initializeCityOpenWorldV20InfrastructureFoundation(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if worldID <= 0 {
		return ErrCityInvalidInput
	}
	var simulationVersion string
	var baselineTick int64
	if err := tx.QueryRowContext(ctx, `
SELECT simulation_version, current_tick
FROM city_worlds
WHERE id = $1
FOR UPDATE`, worldID).Scan(&simulationVersion, &baselineTick); err != nil {
		return fmt.Errorf("lock V20 infrastructure world: %w", err)
	}
	if !cityEngineSupportsOpenWorldInfrastructure(simulationVersion) || baselineTick < 0 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v20_infrastructure_world"})
	}
	if err := assertCityOpenWorldSpatialNetworkFoundation(ctx, tx, worldID); err != nil {
		return fmt.Errorf("validate V20 infrastructure V19 prerequisite: %w", err)
	}
	network, err := loadCityOpenWorldSpatialNetworkState(ctx, tx, worldID)
	if err != nil {
		return fmt.Errorf("load V20 spatial-network prerequisite: %w", err)
	}
	assets, err := buildCityOpenWorldInfrastructureAssets(network)
	if err != nil {
		return err
	}
	contentHash, err := cityOpenWorldInfrastructurePolicyHash()
	if err != nil {
		return fmt.Errorf("hash V20 infrastructure policy: %w", err)
	}
	baselineMetadata, err := cityOpenWorldInfrastructureBaselineMetadata()
	if err != nil {
		return fmt.Errorf("marshal V20 infrastructure baseline metadata: %w", err)
	}
	policyMetadata, err := json.Marshal(map[string]any{
		"schema_version": cityOpenWorldInfrastructureSchemaVersion,
		"scope":          "v19_assets_mutable_state_only",
		"scheduler":      "not_consumed_by_v9",
		"legacy":         "v19_topology_seeded_at_baseline",
	})
	if err != nil {
		return fmt.Errorf("marshal V20 infrastructure policy metadata: %w", err)
	}
	var nodeCount, segmentCount int64
	for _, asset := range assets {
		if asset.AssetKind == cityOpenWorldInfrastructureAssetKindNode {
			nodeCount++
		} else {
			segmentCount++
		}
	}
	if err = activateCityOpenWorldInfrastructureBootstrapWrite(ctx, tx, worldID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_infrastructure_profiles
    (world_id, profile_id, profile_version, content_hash, baseline_tick,
     asset_contract, state_contract, maximum_assets, asset_count, node_asset_count,
     segment_asset_count, transition_count, revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, 1, $13::jsonb)`,
		worldID, cityOpenWorldInfrastructureProfileID, cityOpenWorldInfrastructureProfileVersion,
		contentHash, baselineTick, cityOpenWorldInfrastructureAssetContract,
		cityOpenWorldInfrastructureStateContract, cityOpenWorldInfrastructureMaximumAssets,
		len(assets), nodeCount, segmentCount, len(assets), []byte(policyMetadata)); err != nil {
		return fmt.Errorf("insert V20 infrastructure profile: %w", err)
	}
	for _, asset := range assets {
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_infrastructure_assets
    (world_id, code, asset_kind, spatial_node_code, spatial_corridor_code,
     segment_ordinal, asset_class, definition_version, content_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb)`,
			worldID, asset.Code, asset.AssetKind, cityOpenWorldNullableString(asset.SpatialNodeCode),
			cityOpenWorldNullableString(asset.SpatialCorridorCode), asset.SegmentOrdinal,
			asset.AssetClass, asset.DefinitionVersion, asset.ContentHash, []byte(asset.Metadata)); err != nil {
			return fmt.Errorf("insert V20 infrastructure asset %s: %w", asset.Code, err)
		}
		state := cityOpenWorldInfrastructureBaselineState(asset.Code, baselineTick, baselineMetadata)
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_infrastructure_asset_states
    (world_id, asset_code, state, capacity_milli, effective_tick, source_fact_id, version, metadata)
VALUES ($1, $2, $3, $4, $5, NULL, $6, $7::jsonb)`,
			worldID, state.AssetCode, state.State, state.CapacityMilli, state.EffectiveTick,
			state.Version, []byte(state.Metadata)); err != nil {
			return fmt.Errorf("insert V20 infrastructure baseline state %s: %w", asset.Code, err)
		}
		transition := cityOpenWorldInfrastructureBaselineTransition(asset.Code, baselineTick, baselineMetadata)
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_infrastructure_asset_transitions
    (world_id, asset_code, transition_tick, transition_sequence, from_state,
     to_state, capacity_milli, reason_code, source_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NULL, $9::jsonb)`,
			worldID, transition.AssetCode, transition.TransitionTick, transition.TransitionSeq,
			transition.FromState, transition.ToState, transition.CapacityMilli,
			transition.ReasonCode, []byte(transition.Metadata)); err != nil {
			return fmt.Errorf("insert V20 infrastructure baseline transition %s: %w", asset.Code, err)
		}
	}
	return assertCityOpenWorldInfrastructureFoundation(ctx, tx, worldID)
}

func loadCityOpenWorldInfrastructureState(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*CityOpenWorldInfrastructureState, error) {
	state := &CityOpenWorldInfrastructureState{
		Assets: make([]CityOpenWorldInfrastructureAsset, 0),
		States: make([]CityOpenWorldInfrastructureAssetState, 0),
		Transitions: make([]CityOpenWorldInfrastructureAssetTransition, 0),
	}
	err := queryer.QueryRowContext(ctx, `
SELECT profile_id, profile_version, content_hash, baseline_tick, asset_contract,
       state_contract, maximum_assets, asset_count, node_asset_count,
       segment_asset_count, transition_count, revision, metadata
FROM city_open_world_infrastructure_profiles
WHERE world_id = $1`, worldID).Scan(
		&state.Policy.ProfileID, &state.Policy.ProfileVersion, &state.Policy.ContentHash,
		&state.Policy.BaselineTick, &state.Policy.AssetContract, &state.Policy.StateContract,
		&state.Policy.MaximumAssets, &state.Policy.AssetCount, &state.Policy.NodeAssetCount,
		&state.Policy.SegmentAssetCount, &state.Policy.TransitionCount, &state.Policy.Revision,
		&state.Policy.Metadata,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v20_infrastructure_profile"})
	}
	if err != nil {
		return nil, fmt.Errorf("load V20 infrastructure profile: %w", err)
	}
	assetRows, err := queryer.QueryContext(ctx, `
SELECT code, asset_kind, spatial_node_code, spatial_corridor_code, segment_ordinal,
       asset_class, definition_version, content_hash, metadata
FROM city_open_world_infrastructure_assets
WHERE world_id = $1
ORDER BY code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V20 infrastructure assets: %w", err)
	}
	for assetRows.Next() {
		item := CityOpenWorldInfrastructureAsset{}
		var nodeCode, corridorCode sql.NullString
		if err = assetRows.Scan(&item.Code, &item.AssetKind, &nodeCode, &corridorCode,
			&item.SegmentOrdinal, &item.AssetClass, &item.DefinitionVersion,
			&item.ContentHash, &item.Metadata); err != nil {
			_ = assetRows.Close()
			return nil, fmt.Errorf("scan V20 infrastructure asset: %w", err)
		}
		if nodeCode.Valid {
			item.SpatialNodeCode = &nodeCode.String
		}
		if corridorCode.Valid {
			item.SpatialCorridorCode = &corridorCode.String
		}
		state.Assets = append(state.Assets, item)
	}
	if err = closeCityRows(assetRows, "iterate V20 infrastructure assets"); err != nil {
		return nil, err
	}
	stateRows, err := queryer.QueryContext(ctx, `
SELECT state.asset_code, state.state, state.capacity_milli, state.effective_tick,
       fact.tick, fact.sequence, state.version, state.metadata
FROM city_open_world_infrastructure_asset_states state
LEFT JOIN city_open_world_runtime_facts fact ON fact.id = state.source_fact_id
WHERE state.world_id = $1
ORDER BY state.asset_code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V20 infrastructure states: %w", err)
	}
	for stateRows.Next() {
		item := CityOpenWorldInfrastructureAssetState{}
		var factTick, factSequence sql.NullInt64
		if err = stateRows.Scan(&item.AssetCode, &item.State, &item.CapacityMilli, &item.EffectiveTick,
			&factTick, &factSequence, &item.Version, &item.Metadata); err != nil {
			_ = stateRows.Close()
			return nil, fmt.Errorf("scan V20 infrastructure state: %w", err)
		}
		if factTick.Valid != factSequence.Valid {
			_ = stateRows.Close()
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v20_infrastructure_state_fact"})
		}
		if factTick.Valid {
			item.SourceFact = &CityOpenWorldRuntimeFactRef{Tick: factTick.Int64, Sequence: factSequence.Int64}
		}
		state.States = append(state.States, item)
	}
	if err = closeCityRows(stateRows, "iterate V20 infrastructure states"); err != nil {
		return nil, err
	}
	transitionRows, err := queryer.QueryContext(ctx, `
SELECT transition.asset_code, transition.transition_tick, transition.transition_sequence,
       transition.from_state, transition.to_state, transition.capacity_milli,
       transition.reason_code, fact.tick, fact.sequence, transition.metadata
FROM city_open_world_infrastructure_asset_transitions transition
LEFT JOIN city_open_world_runtime_facts fact ON fact.id = transition.source_fact_id
WHERE transition.world_id = $1
ORDER BY transition.asset_code ASC, transition.transition_tick ASC, transition.transition_sequence ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V20 infrastructure transitions: %w", err)
	}
	for transitionRows.Next() {
		item := CityOpenWorldInfrastructureAssetTransition{}
		var factTick, factSequence sql.NullInt64
		if err = transitionRows.Scan(&item.AssetCode, &item.TransitionTick, &item.TransitionSeq,
			&item.FromState, &item.ToState, &item.CapacityMilli, &item.ReasonCode,
			&factTick, &factSequence, &item.Metadata); err != nil {
			_ = transitionRows.Close()
			return nil, fmt.Errorf("scan V20 infrastructure transition: %w", err)
		}
		if factTick.Valid != factSequence.Valid {
			_ = transitionRows.Close()
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v20_infrastructure_transition_fact"})
		}
		if factTick.Valid {
			item.SourceFact = &CityOpenWorldRuntimeFactRef{Tick: factTick.Int64, Sequence: factSequence.Int64}
		}
		state.Transitions = append(state.Transitions, item)
	}
	if err = closeCityRows(transitionRows, "iterate V20 infrastructure transitions"); err != nil {
		return nil, err
	}
	sortCityOpenWorldInfrastructureState(state)
	if err = validateCityOpenWorldInfrastructureState(state); err != nil {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v20_infrastructure_state"}).WithCause(err)
	}
	return state, nil
}

func validateCityOpenWorldInfrastructureState(state *CityOpenWorldInfrastructureState) error {
	if state == nil {
		return errors.New("infrastructure state is required")
	}
	policy := state.Policy
	expectedHash, err := cityOpenWorldInfrastructurePolicyHash()
	if err != nil || policy.ProfileID != cityOpenWorldInfrastructureProfileID ||
		policy.ProfileVersion != cityOpenWorldInfrastructureProfileVersion || policy.ContentHash != expectedHash ||
		policy.BaselineTick < 0 || policy.AssetContract != cityOpenWorldInfrastructureAssetContract ||
		policy.StateContract != cityOpenWorldInfrastructureStateContract || policy.MaximumAssets != cityOpenWorldInfrastructureMaximumAssets ||
		policy.AssetCount != int64(len(state.Assets)) || policy.TransitionCount != int64(len(state.Transitions)) ||
		len(state.Assets) == 0 || len(state.Assets) > policy.MaximumAssets ||
		!cityOpenWorldInfrastructurePolicyMetadataValid(policy.Metadata) {
		return errors.New("invalid infrastructure policy")
	}
	assets := make(map[string]CityOpenWorldInfrastructureAsset, len(state.Assets))
	var nodes, segments int64
	for _, asset := range state.Assets {
		if !cityOpenWorldSupplyChainCodeValid(asset.Code) || asset.DefinitionVersion != cityOpenWorldInfrastructureProfileVersion ||
			!cityOpenWorldInfrastructureAssetMetadataValid(asset.Metadata, asset) {
			return errors.New("invalid infrastructure asset")
		}
		expectedAssetHash, hashErr := cityOpenWorldInfrastructureAssetContentHash(asset)
		if hashErr != nil || expectedAssetHash != asset.ContentHash {
			return errors.New("infrastructure asset content hash is invalid")
		}
		switch asset.AssetKind {
		case cityOpenWorldInfrastructureAssetKindNode:
			if asset.SpatialNodeCode == nil || asset.SpatialCorridorCode != nil || asset.SegmentOrdinal != 0 ||
				asset.Code != cityOpenWorldInfrastructureNodeAssetCode(*asset.SpatialNodeCode) || asset.AssetClass == "" {
				return errors.New("invalid infrastructure node asset")
			}
			nodes++
		case cityOpenWorldInfrastructureAssetKindSegment:
			if asset.SpatialNodeCode != nil || asset.SpatialCorridorCode == nil || asset.SegmentOrdinal < 1 ||
				asset.Code != cityOpenWorldInfrastructureSegmentAssetCode(*asset.SpatialCorridorCode, asset.SegmentOrdinal) || asset.AssetClass == "" {
				return errors.New("invalid infrastructure segment asset")
			}
			segments++
		default:
			return errors.New("unknown infrastructure asset kind")
		}
		if _, exists := assets[asset.Code]; exists {
			return errors.New("duplicate infrastructure asset")
		}
		assets[asset.Code] = asset
	}
	if policy.NodeAssetCount != nodes || policy.SegmentAssetCount != segments || nodes+segments != policy.AssetCount {
		return errors.New("infrastructure asset counters are inconsistent")
	}
	states := make(map[string]CityOpenWorldInfrastructureAssetState, len(state.States))
	for _, current := range state.States {
		if _, exists := assets[current.AssetCode]; !exists || !cityOpenWorldInfrastructureStateCapacityValid(current.State, current.CapacityMilli) ||
			current.EffectiveTick < policy.BaselineTick || current.Version < 1 ||
			(current.SourceFact == nil && !cityOpenWorldInfrastructureStateMetadataValid(current.Metadata, "baseline", nil)) ||
			(current.SourceFact != nil && (current.SourceFact.Tick < policy.BaselineTick || current.SourceFact.Sequence < 1 ||
				!cityOpenWorldInfrastructureStateMetadataValid(current.Metadata, "command", nil))) {
			return errors.New("invalid infrastructure current state")
		}
		if _, exists := states[current.AssetCode]; exists {
			return errors.New("duplicate infrastructure current state")
		}
		states[current.AssetCode] = current
	}
	if len(states) != len(assets) {
		return errors.New("infrastructure state coverage is incomplete")
	}
	transitionsByAsset := make(map[string][]CityOpenWorldInfrastructureAssetTransition, len(assets))
	for _, transition := range state.Transitions {
		if _, exists := assets[transition.AssetCode]; !exists || transition.TransitionTick < policy.BaselineTick ||
			transition.TransitionSeq < 0 || !cityOpenWorldInfrastructureStateCapacityValid(transition.ToState, transition.CapacityMilli) ||
			!cityOpenWorldSupplyChainCodeValid(transition.ReasonCode) {
			return errors.New("invalid infrastructure transition")
		}
		transitionsByAsset[transition.AssetCode] = append(transitionsByAsset[transition.AssetCode], transition)
	}
	for assetCode, current := range states {
		transitions := transitionsByAsset[assetCode]
		if len(transitions) == 0 {
			return errors.New("infrastructure transition coverage is incomplete")
		}
		sort.Slice(transitions, func(i, j int) bool {
			if transitions[i].TransitionTick != transitions[j].TransitionTick {
				return transitions[i].TransitionTick < transitions[j].TransitionTick
			}
			return transitions[i].TransitionSeq < transitions[j].TransitionSeq
		})
		baseline := transitions[0]
		if baseline.TransitionTick != policy.BaselineTick || baseline.TransitionSeq != 0 || baseline.FromState != "" ||
			baseline.ToState != cityOpenWorldInfrastructureStateOperational || baseline.CapacityMilli != 1000 ||
			baseline.ReasonCode != cityOpenWorldInfrastructureReasonBaseline || baseline.SourceFact != nil ||
			!cityOpenWorldInfrastructureStateMetadataValid(baseline.Metadata, "baseline", nil) {
			return errors.New("infrastructure baseline transition is invalid")
		}
		previous := baseline
		for index := 1; index < len(transitions); index++ {
			transition := transitions[index]
			if transition.SourceFact == nil || transition.SourceFact.Tick != transition.TransitionTick ||
				transition.SourceFact.Sequence != transition.TransitionSeq || transition.FromState != previous.ToState ||
				!cityOpenWorldInfrastructureTransitionAllowed(transition.FromState, transition.ToState) ||
				!cityOpenWorldInfrastructureStateMetadataValid(transition.Metadata, "command", &transition.FromState) {
				return errors.New("infrastructure transition state machine is invalid")
			}
			if transition.TransitionTick < previous.TransitionTick ||
				(transition.TransitionTick == previous.TransitionTick && transition.TransitionSeq <= previous.TransitionSeq) {
				return errors.New("infrastructure transition order is invalid")
			}
			previous = transition
		}
		if current.State != previous.ToState || current.CapacityMilli != previous.CapacityMilli ||
			current.EffectiveTick != previous.TransitionTick || current.Version != int64(len(transitions)) ||
			!cityOpenWorldInfrastructureFactRefEqual(current.SourceFact, previous.SourceFact) {
			return errors.New("infrastructure current state does not match transition history")
		}
	}
	if policy.Revision != 1+(policy.TransitionCount-policy.AssetCount) || policy.Revision < 1 {
		return errors.New("infrastructure policy revision is inconsistent")
	}
	return nil
}

func cityOpenWorldInfrastructureFactRefEqual(left, right *CityOpenWorldRuntimeFactRef) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Tick == right.Tick && left.Sequence == right.Sequence
}

func cityOpenWorldInfrastructurePolicyMetadataValid(raw json.RawMessage) bool {
	var metadata struct {
		SchemaVersion int    `json:"schema_version"`
		Scope         string `json:"scope"`
		Scheduler     string `json:"scheduler"`
		Legacy        string `json:"legacy"`
	}
	return json.Unmarshal(raw, &metadata) == nil && metadata.SchemaVersion == cityOpenWorldInfrastructureSchemaVersion &&
		metadata.Scope == "v19_assets_mutable_state_only" && metadata.Scheduler == "not_consumed_by_v9" &&
		metadata.Legacy == "v19_topology_seeded_at_baseline"
}

func sortCityOpenWorldInfrastructureState(state *CityOpenWorldInfrastructureState) {
	if state == nil {
		return
	}
	sort.Slice(state.Assets, func(i, j int) bool { return state.Assets[i].Code < state.Assets[j].Code })
	sort.Slice(state.States, func(i, j int) bool { return state.States[i].AssetCode < state.States[j].AssetCode })
	sort.Slice(state.Transitions, func(i, j int) bool {
		if state.Transitions[i].AssetCode != state.Transitions[j].AssetCode {
			return state.Transitions[i].AssetCode < state.Transitions[j].AssetCode
		}
		if state.Transitions[i].TransitionTick != state.Transitions[j].TransitionTick {
			return state.Transitions[i].TransitionTick < state.Transitions[j].TransitionTick
		}
		return state.Transitions[i].TransitionSeq < state.Transitions[j].TransitionSeq
	})
}
