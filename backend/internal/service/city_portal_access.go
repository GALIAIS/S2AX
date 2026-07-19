package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	worldRuntimePortalAccessVersion = "1.2.0"

	WorldPortalStateOpen   = "open"
	WorldPortalStateClosed = "closed"
	WorldPortalStateLocked = "locked"

	WorldPortalActionOpen   = "open"
	WorldPortalActionClose  = "close"
	WorldPortalActionLock   = "lock"
	WorldPortalActionUnlock = "unlock"

	worldRuntimeRejectionPortalNotFound      = "WORLD_PORTAL_NOT_FOUND"
	worldRuntimeRejectionPortalStateInvalid  = "WORLD_PORTAL_STATE_INVALID"
	worldRuntimeRejectionPortalAccessDenied  = "WORLD_PORTAL_ACCESS_DENIED"
	worldRuntimeRejectionPortalOutOfReach    = "WORLD_PORTAL_OUT_OF_REACH"
	worldRuntimeRejectionPortalPolicyInvalid = "WORLD_PORTAL_POLICY_INVALID"
)

var ErrWorldPortalAccessUnavailable = infraerrors.NotFound(
	"WORLD_PORTAL_ACCESS_UNAVAILABLE", "world portal access control is unavailable",
)

type WorldPortalAccessView struct {
	State            WorldPortalState            `json:"state"`
	From             CityNavigationCoordinate    `json:"from"`
	To               CityNavigationCoordinate    `json:"to"`
	Bidirectional    bool                        `json:"bidirectional"`
	Accessible       *bool                       `json:"accessible,omitempty"`
	AccessEvaluation *WorldRequirementEvaluation `json:"access_evaluation,omitempty"`
}

type WorldPortalAccessQueryInput struct {
	UserID    int64
	WorldID   int64
	ActorCode string
}

type worldPortalStateRecord struct {
	id            int64
	portalID      int64
	state         WorldPortalState
	from          CityNavigationCoordinate
	to            CityNavigationCoordinate
	bidirectional bool
}

func publicWorldPortalAccessRequirement() WorldRequirementNode {
	return WorldRequirementNode{Operator: WorldRequirementAll}
}

func normalizeWorldRequirementNode(node *WorldRequirementNode) {
	if node == nil {
		return
	}
	node.Operator = strings.ToLower(strings.TrimSpace(node.Operator))
	node.AttributeCode = strings.ToLower(strings.TrimSpace(node.AttributeCode))
	node.RoleCode = strings.ToLower(strings.TrimSpace(node.RoleCode))
	node.StatusCode = strings.ToLower(strings.TrimSpace(node.StatusCode))
	node.FactType = strings.ToLower(strings.TrimSpace(node.FactType))
	for index := range node.Items {
		normalizeWorldRequirementNode(&node.Items[index])
	}
	if len(node.Items) == 0 {
		node.Items = nil
	}
	normalizeWorldRequirementNode(node.Item)
}

func canonicalWorldPortalAccessRequirement(
	requirement WorldRequirementNode,
) (WorldRequirementNode, json.RawMessage, string, error) {
	normalizeWorldRequirementNode(&requirement)
	if err := validateWorldRequirement(requirement); err != nil {
		return WorldRequirementNode{}, nil, "", err
	}
	raw, err := json.Marshal(requirement)
	if err != nil {
		return WorldRequirementNode{}, nil, "", fmt.Errorf("marshal portal access requirement: %w", err)
	}
	digest := sha256.Sum256(raw)
	return requirement, raw, hex.EncodeToString(digest[:]), nil
}

func validateWorldPortalRequirementReferences(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	requirement WorldRequirementNode,
) error {
	if err := validateWorldRequirement(requirement); err != nil {
		return err
	}
	type reference struct{ kind, code string }
	references := make(map[reference]struct{})
	var collect func(WorldRequirementNode)
	collect = func(node WorldRequirementNode) {
		switch node.Operator {
		case WorldRequirementAll, WorldRequirementAny:
			for _, item := range node.Items {
				collect(item)
			}
		case WorldRequirementNot:
			if node.Item != nil {
				collect(*node.Item)
			}
		case WorldRequirementAttributeGTE, WorldRequirementAttributeLTE, WorldRequirementExperienceGTE:
			references[reference{WorldRuntimeDefinitionAttribute, node.AttributeCode}] = struct{}{}
		case WorldRequirementRoleActive, WorldRequirementRoleInactive:
			references[reference{WorldRuntimeDefinitionRole, node.RoleCode}] = struct{}{}
		case WorldRequirementStatusPresent, WorldRequirementStatusAbsent:
			references[reference{WorldRuntimeDefinitionStatus, node.StatusCode}] = struct{}{}
		}
	}
	collect(requirement)
	for item := range references {
		var exists bool
		if err := queryer.QueryRowContext(ctx, `
SELECT EXISTS (
    SELECT 1 FROM world_runtime_definitions
    WHERE world_id = $1 AND definition_kind = $2 AND code = $3
)`, worldID, item.kind, item.code).Scan(&exists); err != nil {
			return fmt.Errorf("validate portal access requirement reference %s/%s: %w", item.kind, item.code, err)
		}
		if !exists {
			return worldRuntimeReject(worldRuntimeRejectionPortalPolicyInvalid)
		}
	}
	return nil
}

func initializeWorldPortalAccessFoundation(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	targetSimulationVersion string,
) error {
	var simulationVersion string
	var baselineTick int64
	if err := tx.QueryRowContext(ctx, `
SELECT simulation_version, current_tick
FROM city_worlds WHERE id = $1 FOR UPDATE`, worldID).Scan(&simulationVersion, &baselineTick); err != nil {
		return fmt.Errorf("load world portal-access version: %w", err)
	}
	if !cityEngineSupportsWorldPortalAccess(targetSimulationVersion) ||
		(simulationVersion != targetSimulationVersion &&
			!cityEngineCanUpgrade(simulationVersion, targetSimulationVersion)) {
		return fmt.Errorf("world portal-access foundation requires an actor-navigation capable runtime")
	}
	if _, err := tx.ExecContext(ctx, `
SELECT set_config('sub2api.world_runtime_bootstrap_world_id', $1, TRUE)`, fmt.Sprintf("%d", worldID)); err != nil {
		return fmt.Errorf("enable world portal-access bootstrap: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE world_runtime_profiles
SET runtime_version = $2, updated_at = NOW()
WHERE world_id = $1`, worldID, worldRuntimePortalAccessVersion); err != nil {
		return fmt.Errorf("upgrade world portal-access runtime profile: %w", err)
	}
	_, raw, policyHash, err := canonicalWorldPortalAccessRequirement(publicWorldPortalAccessRequirement())
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO world_portal_states
    (world_id, portal_id, state_code, access_requirement, access_policy_hash,
     changed_tick, source_fact_id, version, metadata)
SELECT portal.world_id, portal.id, 'open', $2::jsonb, $3, $4, NULL, 1,
       '{"schema_version":1,"source":"baseline"}'::jsonb
FROM city_building_portals portal
WHERE portal.world_id = $1 AND portal.status = 'active'
ON CONFLICT (world_id, portal_id) DO NOTHING`, worldID, []byte(raw), policyHash, baselineTick); err != nil {
		return fmt.Errorf("bootstrap world portal states: %w", err)
	}
	return nil
}

func scanWorldPortalStateRecord(row cityScannable) (*worldPortalStateRecord, error) {
	record := &worldPortalStateRecord{}
	var requirementRaw []byte
	var sourceTick, sourceSequence sql.NullInt64
	if err := row.Scan(
		&record.id, &record.portalID, &record.state.BuildingCode, &record.state.PortalCode,
		&record.state.PortalType, &record.from.X, &record.from.Y, &record.from.Z,
		&record.to.X, &record.to.Y, &record.to.Z, &record.bidirectional,
		&record.state.StateCode, &requirementRaw, &record.state.AccessPolicyHash,
		&record.state.ChangedTick, &sourceTick, &sourceSequence,
		&record.state.Version, &record.state.Metadata,
	); err != nil {
		return nil, err
	}
	if err := decodeStrictCityObject(json.RawMessage(requirementRaw), &record.state.AccessRequirement); err != nil {
		return nil, fmt.Errorf("decode world portal access requirement: %w", err)
	}
	requirement, _, policyHash, err := canonicalWorldPortalAccessRequirement(record.state.AccessRequirement)
	if err != nil {
		return nil, fmt.Errorf("validate world portal access requirement: %w", err)
	}
	if policyHash != record.state.AccessPolicyHash {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "world_portal_access_policy_hash"})
	}
	record.state.AccessRequirement = requirement
	if sourceTick.Valid {
		record.state.SourceFact = &WorldRuntimeFactRef{Tick: sourceTick.Int64, Sequence: sourceSequence.Int64}
	}
	return record, nil
}

const worldPortalStateSelect = `
SELECT state_value.id, state_value.portal_id, building.code, portal.code, portal.portal_type,
       portal.from_x, portal.from_y, portal.from_z,
       portal.to_x, portal.to_y, portal.to_z, portal.bidirectional,
       state_value.state_code, state_value.access_requirement,
       state_value.access_policy_hash, state_value.changed_tick,
       source.tick, source.sequence, state_value.version, state_value.metadata
FROM world_portal_states state_value
JOIN city_building_portals portal
  ON portal.id = state_value.portal_id AND portal.world_id = state_value.world_id
JOIN city_buildings building
  ON building.id = portal.building_id AND building.world_id = portal.world_id
LEFT JOIN world_runtime_facts source
  ON source.id = state_value.source_fact_id AND source.world_id = state_value.world_id
`

func loadWorldPortalStateRecord(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	buildingCode, portalCode string,
	forUpdate bool,
) (*worldPortalStateRecord, error) {
	query := worldPortalStateSelect + `
WHERE state_value.world_id = $1 AND building.code = $2 AND portal.code = $3
  AND portal.status = 'active' AND building.status = 'active'`
	if forUpdate {
		query += ` FOR UPDATE OF state_value`
	}
	record, err := scanWorldPortalStateRecord(queryer.QueryRowContext(ctx, query, worldID, buildingCode, portalCode))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, worldRuntimeReject(worldRuntimeRejectionPortalNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("load world portal state %s/%s: %w", buildingCode, portalCode, err)
	}
	return record, nil
}

func loadWorldPortalStateRecords(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) ([]worldPortalStateRecord, error) {
	rows, err := queryer.QueryContext(ctx, worldPortalStateSelect+`
WHERE state_value.world_id = $1 AND portal.status = 'active' AND building.status = 'active'
ORDER BY building.code ASC, portal.code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load world portal states: %w", err)
	}
	items := make([]worldPortalStateRecord, 0)
	for rows.Next() {
		item, scanErr := scanWorldPortalStateRecord(rows)
		if scanErr != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan world portal state: %w", scanErr)
		}
		items = append(items, *item)
	}
	if err = closeCityRows(rows, "iterate world portal states"); err != nil {
		return nil, err
	}
	return items, nil
}

func loadWorldPortalStates(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) ([]WorldPortalState, error) {
	records, err := loadWorldPortalStateRecords(ctx, queryer, worldID)
	if err != nil {
		return nil, err
	}
	items := make([]WorldPortalState, len(records))
	for index := range records {
		items[index] = records[index].state
	}
	return items, nil
}

func evaluateWorldPortalAccess(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, actorID, worldTick int64,
	requirement WorldRequirementNode,
) (WorldRequirementEvaluation, error) {
	return evaluateWorldRequirement(ctx, queryer, worldID, actorID, worldTick, requirement)
}

func (s *CityEconomyService) ListWorldPortalStates(
	ctx context.Context,
	input WorldPortalAccessQueryInput,
) ([]WorldPortalAccessView, error) {
	input.ActorCode = strings.ToLower(strings.TrimSpace(input.ActorCode))
	if input.UserID <= 0 || input.WorldID <= 0 ||
		(input.ActorCode != "" && !worldRuntimeCodeValid(input.ActorCode, 128)) {
		return nil, ErrCityInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("begin world portal state snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err = authorizeCityWorldRead(ctx, tx, input.UserID, input.WorldID); err != nil {
		return nil, err
	}
	var simulationVersion string
	var worldTick int64
	if err = tx.QueryRowContext(ctx, `
SELECT simulation_version, current_tick FROM city_worlds WHERE id = $1`, input.WorldID).Scan(
		&simulationVersion, &worldTick,
	); err != nil {
		return nil, fmt.Errorf("load world portal state version: %w", err)
	}
	if !cityEngineSupportsWorldPortalAccess(simulationVersion) {
		return nil, ErrWorldPortalAccessUnavailable
	}
	actorID := int64(0)
	if input.ActorCode != "" {
		actorID, _, _, err = loadCityNavigationActor(
			ctx, tx, input.WorldID, input.UserID, input.ActorCode,
		)
		if err != nil {
			return nil, err
		}
	}
	records, err := loadWorldPortalStateRecords(ctx, tx, input.WorldID)
	if err != nil {
		return nil, err
	}
	items := make([]WorldPortalAccessView, 0, len(records))
	for _, record := range records {
		item := WorldPortalAccessView{
			State: record.state, From: record.from, To: record.to,
			Bidirectional: record.bidirectional,
		}
		if actorID > 0 {
			evaluation, evaluateErr := evaluateWorldPortalAccess(
				ctx, tx, input.WorldID, actorID, worldTick, record.state.AccessRequirement,
			)
			if evaluateErr != nil {
				return nil, evaluateErr
			}
			accessible := record.state.StateCode == WorldPortalStateOpen && evaluation.Satisfied
			item.Accessible = &accessible
			item.AccessEvaluation = &evaluation
		}
		items = append(items, item)
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit world portal state snapshot: %w", err)
	}
	return items, nil
}
