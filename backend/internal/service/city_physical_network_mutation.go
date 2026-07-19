package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	CityCommandTypePhysicalNetworkConfigure = "network.configure"
	CityCommandTypePhysicalNodeConfigure    = "network.node.configure"
	CityCommandTypePhysicalEdgeConfigure    = "network.edge.configure"
	CityCommandTypePhysicalEdgeTransition   = "network.edge.transition"

	cityPhysicalNetworkRejectionNetworkNotFound = "CITY_PHYSICAL_NETWORK_NOT_FOUND"
	cityPhysicalNetworkRejectionNodeNotFound    = "CITY_PHYSICAL_NETWORK_NODE_NOT_FOUND"
	cityPhysicalNetworkRejectionEdgeNotFound    = "CITY_PHYSICAL_NETWORK_EDGE_NOT_FOUND"
	cityPhysicalNetworkRejectionConflict        = "CITY_PHYSICAL_NETWORK_CONFLICT"
	cityPhysicalNetworkRejectionVersion         = "CITY_PHYSICAL_NETWORK_VERSION_CONFLICT"
	cityPhysicalNetworkRejectionTransition      = "CITY_PHYSICAL_NETWORK_TRANSITION_INVALID"
	cityPhysicalNetworkRejectionBinding         = "CITY_PHYSICAL_NETWORK_BINDING_INVALID"
	cityPhysicalNetworkRejectionTopology        = "CITY_PHYSICAL_NETWORK_TOPOLOGY_INVALID"
	cityPhysicalNetworkRejectionLimit           = "CITY_PHYSICAL_NETWORK_LIMIT_REACHED"
)

type cityPhysicalNetworkConfigurePayload struct {
	Code            string          `json:"code"`
	Name            string          `json:"name"`
	ServiceCode     string          `json:"service_code"`
	Status          string          `json:"status"`
	ExpectedVersion int64           `json:"expected_version"`
	Metadata        json.RawMessage `json:"metadata,omitempty"`
}

type cityPhysicalNetworkNodeConfigurePayload struct {
	Code            string          `json:"code"`
	NetworkCode     string          `json:"network_code"`
	Role            string          `json:"role"`
	CapacityCode    string          `json:"capacity_code,omitempty"`
	DemandCode      string          `json:"demand_code,omitempty"`
	DistrictCode    string          `json:"district_code,omitempty"`
	BuildingCode    string          `json:"building_code,omitempty"`
	WorldX          *int64          `json:"world_x,omitempty"`
	WorldY          *int64          `json:"world_y,omitempty"`
	WorldZ          *int            `json:"world_z,omitempty"`
	Status          string          `json:"status"`
	ExpectedVersion int64           `json:"expected_version"`
	Metadata        json.RawMessage `json:"metadata,omitempty"`
}

type cityPhysicalNetworkEdgeConfigurePayload struct {
	Code                   string          `json:"code"`
	NetworkCode            string          `json:"network_code"`
	FromNodeCode           string          `json:"from_node_code"`
	ToNodeCode             string          `json:"to_node_code"`
	Direction              string          `json:"direction"`
	InstalledCapacityUnits int64           `json:"installed_capacity_units"`
	AvailabilityMilli      int             `json:"availability_milli"`
	LossMilli              int             `json:"loss_milli"`
	BaseCostUnits          int64           `json:"base_cost_units"`
	Status                 string          `json:"status"`
	ExpectedVersion        int64           `json:"expected_version"`
	Metadata               json.RawMessage `json:"metadata,omitempty"`
}

type cityPhysicalNetworkEdgeTransitionPayload struct {
	EdgeCode        string          `json:"edge_code"`
	ToStatus        string          `json:"to_status"`
	ExpectedVersion int64           `json:"expected_version"`
	Metadata        json.RawMessage `json:"metadata,omitempty"`
}

type cityPhysicalNetworkConfigureFactPayload struct {
	SchemaVersion int                  `json:"schema_version"`
	NetworkBefore *CityPhysicalNetwork `json:"network_before,omitempty"`
	NetworkAfter  CityPhysicalNetwork  `json:"network_after"`
}

type cityPhysicalNetworkNodeConfigureFactPayload struct {
	SchemaVersion int                      `json:"schema_version"`
	NetworkBefore CityPhysicalNetwork      `json:"network_before"`
	NetworkAfter  CityPhysicalNetwork      `json:"network_after"`
	NodeBefore    *CityPhysicalNetworkNode `json:"node_before,omitempty"`
	NodeAfter     CityPhysicalNetworkNode  `json:"node_after"`
}

type cityPhysicalNetworkEdgeConfigureFactPayload struct {
	SchemaVersion int                      `json:"schema_version"`
	NetworkBefore CityPhysicalNetwork      `json:"network_before"`
	NetworkAfter  CityPhysicalNetwork      `json:"network_after"`
	EdgeBefore    *CityPhysicalNetworkEdge `json:"edge_before,omitempty"`
	EdgeAfter     CityPhysicalNetworkEdge  `json:"edge_after"`
}

type cityPhysicalNetworkBusinessError struct{ code string }

func (err *cityPhysicalNetworkBusinessError) Error() string { return err.code }

func cityPhysicalNetworkReject(code string) error {
	return &cityPhysicalNetworkBusinessError{code: code}
}

func cityPhysicalNetworkBusinessRejectionCode(err error) string {
	var businessErr *cityPhysicalNetworkBusinessError
	if errors.As(err, &businessErr) {
		return businessErr.code
	}
	return ""
}

func isCityPhysicalNetworkCommand(commandType string) bool {
	switch commandType {
	case CityCommandTypePhysicalNetworkConfigure,
		CityCommandTypePhysicalNodeConfigure,
		CityCommandTypePhysicalEdgeConfigure,
		CityCommandTypePhysicalEdgeTransition:
		return true
	default:
		return false
	}
}

func normalizeCityPhysicalNetworkCommand(
	commandType string, raw json.RawMessage,
) (any, bool, error) {
	normalizeCode := func(value *string, optional bool) bool {
		*value = strings.ToLower(strings.TrimSpace(*value))
		return optional && *value == "" || cityServiceCodePattern.MatchString(*value)
	}
	normalizeMetadata := func(value *json.RawMessage) bool {
		metadata, err := normalizeCityServiceMetadata(*value)
		if err != nil {
			return false
		}
		*value = metadata
		return true
	}
	switch commandType {
	case CityCommandTypePhysicalNetworkConfigure:
		var value cityPhysicalNetworkConfigurePayload
		if err := decodeStrictCityObject(raw, &value); err != nil {
			return nil, true, ErrCityInvalidInput.WithCause(err)
		}
		value.Name = strings.TrimSpace(value.Name)
		value.Status = strings.ToLower(strings.TrimSpace(value.Status))
		if !normalizeCode(&value.Code, false) || !normalizeCode(&value.ServiceCode, false) ||
			utf8.RuneCountInString(value.Name) < 1 || utf8.RuneCountInString(value.Name) > 96 ||
			!isCityPhysicalNetworkStatus(value.Status) || value.Status == CityNetworkStatusRetired && value.ExpectedVersion == 0 ||
			value.ExpectedVersion < 0 || !normalizeMetadata(&value.Metadata) {
			return nil, true, ErrCityInvalidInput
		}
		return value, true, nil
	case CityCommandTypePhysicalNodeConfigure:
		var value cityPhysicalNetworkNodeConfigurePayload
		if err := decodeStrictCityObject(raw, &value); err != nil {
			return nil, true, ErrCityInvalidInput.WithCause(err)
		}
		value.Role = strings.ToLower(strings.TrimSpace(value.Role))
		value.Status = strings.ToLower(strings.TrimSpace(value.Status))
		codesValid := normalizeCode(&value.Code, false) &&
			normalizeCode(&value.NetworkCode, false) &&
			normalizeCode(&value.CapacityCode, true) &&
			normalizeCode(&value.DemandCode, true) &&
			normalizeCode(&value.DistrictCode, true) &&
			normalizeCode(&value.BuildingCode, true)
		coordinatesComplete := value.WorldX != nil && value.WorldY != nil && value.WorldZ != nil
		coordinatesEmpty := value.WorldX == nil && value.WorldY == nil && value.WorldZ == nil
		bindingValid := (value.Role == CityNetworkNodeRoleSupply && value.CapacityCode != "" && value.DemandCode == "") ||
			(value.Role == CityNetworkNodeRoleDemand && value.CapacityCode == "" && value.DemandCode != "") ||
			(value.Role == CityNetworkNodeRoleJunction || value.Role == CityNetworkNodeRoleStorage ||
				value.Role == CityNetworkNodeRoleGateway) && value.CapacityCode == "" && value.DemandCode == ""
		if !codesValid ||
			!isCityPhysicalNetworkNodeRole(value.Role) || !isCityPhysicalNetworkNodeStatus(value.Status) ||
			value.Status == CityNetworkNodeStatusRetired && value.ExpectedVersion == 0 ||
			(!coordinatesComplete && !coordinatesEmpty) || !bindingValid || value.ExpectedVersion < 0 ||
			!normalizeMetadata(&value.Metadata) {
			return nil, true, ErrCityInvalidInput
		}
		return value, true, nil
	case CityCommandTypePhysicalEdgeConfigure:
		var value cityPhysicalNetworkEdgeConfigurePayload
		if err := decodeStrictCityObject(raw, &value); err != nil {
			return nil, true, ErrCityInvalidInput.WithCause(err)
		}
		value.Direction = strings.ToLower(strings.TrimSpace(value.Direction))
		value.Status = strings.ToLower(strings.TrimSpace(value.Status))
		if !normalizeCode(&value.Code, false) || !normalizeCode(&value.NetworkCode, false) ||
			!normalizeCode(&value.FromNodeCode, false) || !normalizeCode(&value.ToNodeCode, false) ||
			value.FromNodeCode == value.ToNodeCode || !isCityPhysicalNetworkEdgeDirection(value.Direction) ||
			value.InstalledCapacityUnits <= 0 || value.InstalledCapacityUnits > cityServiceMaximumConfiguredUnits ||
			value.AvailabilityMilli < 0 || value.AvailabilityMilli > 1000 ||
			value.LossMilli < 0 || value.LossMilli > 999 || value.BaseCostUnits <= 0 ||
			value.BaseCostUnits > cityServiceMaximumConfiguredUnits ||
			!isCityPhysicalNetworkEdgeStatus(value.Status) ||
			(value.Status == CityNetworkEdgeStatusFailed || value.Status == CityNetworkEdgeStatusRetired) && value.ExpectedVersion == 0 ||
			value.ExpectedVersion < 0 || !normalizeMetadata(&value.Metadata) {
			return nil, true, ErrCityInvalidInput
		}
		return value, true, nil
	case CityCommandTypePhysicalEdgeTransition:
		var value cityPhysicalNetworkEdgeTransitionPayload
		if err := decodeStrictCityObject(raw, &value); err != nil {
			return nil, true, ErrCityInvalidInput.WithCause(err)
		}
		value.ToStatus = strings.ToLower(strings.TrimSpace(value.ToStatus))
		if !normalizeCode(&value.EdgeCode, false) || !isCityPhysicalNetworkEdgeStatus(value.ToStatus) ||
			value.ExpectedVersion <= 0 || !normalizeMetadata(&value.Metadata) {
			return nil, true, ErrCityInvalidInput
		}
		return value, true, nil
	default:
		return nil, false, nil
	}
}

func isCityPhysicalNetworkStatus(value string) bool {
	return value == CityNetworkStatusActive || value == CityNetworkStatusSuspended ||
		value == CityNetworkStatusRetired
}

func isCityPhysicalNetworkNodeStatus(value string) bool {
	return value == CityNetworkNodeStatusActive || value == CityNetworkNodeStatusOffline ||
		value == CityNetworkNodeStatusRetired
}

func isCityPhysicalNetworkEdgeDirection(value string) bool {
	return value == CityNetworkEdgeDirectionDirected || value == CityNetworkEdgeDirectionBidirectional
}

func isCityPhysicalNetworkEdgeStatus(value string) bool {
	return value == CityNetworkEdgeStatusActive || value == CityNetworkEdgeStatusIsolated ||
		value == CityNetworkEdgeStatusFailed || value == CityNetworkEdgeStatusRetired
}

type cityPhysicalNetworkExecution struct {
	pending cityPendingEvent
	fact    *CityPhysicalNetworkFact
}

type cityPhysicalNetworkFactRecord struct {
	id   int64
	fact CityPhysicalNetworkFact
}

type cityPhysicalNetworkFactInsert struct {
	worldID         int64
	tick            int64
	sequence        int64
	sourceCommandID int64
	factType        string
	subjectKind     string
	subjectCode     string
	versionBefore   int64
	versionAfter    int64
	payload         any
}

type cityPhysicalNetworkProfileDelta struct {
	networks int64
	nodes    int64
	edges    int64
}

func (s *CityEconomyService) applyCityPhysicalNetworkCommand(
	ctx context.Context, tx *sql.Tx, worldID, targetTick, factSequence int64,
	command *CityCommand,
) (cityPhysicalNetworkExecution, error) {
	if _, err := tx.ExecContext(ctx, `SAVEPOINT city_physical_network_command`); err != nil {
		return cityPhysicalNetworkExecution{}, fmt.Errorf("create physical network command savepoint: %w", err)
	}
	execution, err := s.postCityPhysicalNetworkCommand(
		ctx, tx, worldID, targetTick, factSequence, command,
	)
	if err != nil {
		if _, rollbackErr := tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT city_physical_network_command`); rollbackErr != nil {
			return cityPhysicalNetworkExecution{}, fmt.Errorf("rollback physical network command after %v: %w", err, rollbackErr)
		}
		if _, releaseErr := tx.ExecContext(ctx, `RELEASE SAVEPOINT city_physical_network_command`); releaseErr != nil {
			return cityPhysicalNetworkExecution{}, fmt.Errorf("release rejected physical network command: %w", releaseErr)
		}
		if code := cityPhysicalNetworkBusinessRejectionCode(err); code != "" {
			return cityPhysicalNetworkExecution{pending: rejectedCityCommand(command, code)}, nil
		}
		return cityPhysicalNetworkExecution{}, err
	}
	if _, err = tx.ExecContext(ctx, `RELEASE SAVEPOINT city_physical_network_command`); err != nil {
		return cityPhysicalNetworkExecution{}, fmt.Errorf("release physical network command: %w", err)
	}
	return execution, nil
}

func (s *CityEconomyService) postCityPhysicalNetworkCommand(
	ctx context.Context, tx *sql.Tx, worldID, targetTick, factSequence int64,
	command *CityCommand,
) (cityPhysicalNetworkExecution, error) {
	switch command.CommandType {
	case CityCommandTypePhysicalNetworkConfigure:
		payload, err := decodeStoredCityCommandPayload[cityPhysicalNetworkConfigurePayload](command)
		if err != nil {
			return cityPhysicalNetworkExecution{}, err
		}
		return s.configureCityPhysicalNetwork(ctx, tx, worldID, targetTick, factSequence, command, payload)
	case CityCommandTypePhysicalNodeConfigure:
		payload, err := decodeStoredCityCommandPayload[cityPhysicalNetworkNodeConfigurePayload](command)
		if err != nil {
			return cityPhysicalNetworkExecution{}, err
		}
		return s.configureCityPhysicalNetworkNode(ctx, tx, worldID, targetTick, factSequence, command, payload)
	case CityCommandTypePhysicalEdgeConfigure:
		payload, err := decodeStoredCityCommandPayload[cityPhysicalNetworkEdgeConfigurePayload](command)
		if err != nil {
			return cityPhysicalNetworkExecution{}, err
		}
		return s.configureCityPhysicalNetworkEdge(ctx, tx, worldID, targetTick, factSequence, command, payload)
	case CityCommandTypePhysicalEdgeTransition:
		payload, err := decodeStoredCityCommandPayload[cityPhysicalNetworkEdgeTransitionPayload](command)
		if err != nil {
			return cityPhysicalNetworkExecution{}, err
		}
		return s.transitionCityPhysicalNetworkEdge(ctx, tx, worldID, targetTick, factSequence, command, payload)
	default:
		return cityPhysicalNetworkExecution{}, ErrCitySimulationInvariant.WithMetadata(
			map[string]string{"command_type": command.CommandType},
		)
	}
}

type cityPhysicalNetworkCommandNetworkRef struct {
	id                 int64
	serviceID          int64
	maximumNodes       int
	maximumEdges       int
	allowBidirectional bool
	value              CityPhysicalNetwork
}

type cityPhysicalNetworkCommandNodeRef struct {
	id        int64
	networkID int64
	value     CityPhysicalNetworkNode
}

type cityPhysicalNetworkCommandEdgeRef struct {
	id        int64
	networkID int64
	fromID    int64
	toID      int64
	value     CityPhysicalNetworkEdge
}

func (s *CityEconomyService) configureCityPhysicalNetwork(
	ctx context.Context, tx *sql.Tx, worldID, targetTick, factSequence int64,
	command *CityCommand, payload cityPhysicalNetworkConfigurePayload,
) (cityPhysicalNetworkExecution, error) {
	existing, err := loadCityPhysicalNetworkCommandNetwork(ctx, tx, worldID, payload.Code, true)
	if err != nil {
		return cityPhysicalNetworkExecution{}, err
	}
	var serviceID int64
	var maximumNodes, maximumEdges int
	var allowBidirectional bool
	err = tx.QueryRowContext(ctx, `
SELECT service.id, policy.maximum_nodes, policy.maximum_edges,
       policy.allow_bidirectional
FROM city_service_definitions service
JOIN city_physical_network_policies policy
  ON policy.service_definition_id = service.id AND policy.world_id = service.world_id
WHERE service.world_id = $1 AND service.code = $2`, worldID, payload.ServiceCode).Scan(
		&serviceID, &maximumNodes, &maximumEdges, &allowBidirectional,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return cityPhysicalNetworkExecution{}, cityPhysicalNetworkReject(cityPhysicalNetworkRejectionBinding)
	}
	if err != nil {
		return cityPhysicalNetworkExecution{}, fmt.Errorf("load physical network service policy: %w", err)
	}
	metadata, err := cityPhysicalNetworkExplicitMetadata(nil, payload.Metadata)
	if err != nil {
		return cityPhysicalNetworkExecution{}, err
	}
	var before *CityPhysicalNetwork
	var after CityPhysicalNetwork
	delta := int64(0)
	if existing == nil {
		if payload.ExpectedVersion != 0 {
			return cityPhysicalNetworkExecution{}, cityPhysicalNetworkReject(cityPhysicalNetworkRejectionNetworkNotFound)
		}
		var networkCount, conflicting int64
		if err = tx.QueryRowContext(ctx, `
SELECT profile.network_count,
       (SELECT COUNT(*) FROM city_physical_networks network
        WHERE network.world_id = profile.world_id
          AND network.service_definition_id = $2 AND network.status <> 'retired')
FROM city_physical_network_profiles profile WHERE profile.world_id = $1
FOR UPDATE`, worldID, serviceID).Scan(&networkCount, &conflicting); err != nil {
			return cityPhysicalNetworkExecution{}, fmt.Errorf("inspect physical network capacity: %w", err)
		}
		if networkCount >= cityPhysicalNetworkMaximumNetworks {
			return cityPhysicalNetworkExecution{}, cityPhysicalNetworkReject(cityPhysicalNetworkRejectionLimit)
		}
		if conflicting != 0 {
			return cityPhysicalNetworkExecution{}, cityPhysicalNetworkReject(cityPhysicalNetworkRejectionConflict)
		}
		after = CityPhysicalNetwork{
			Code: payload.Code, Name: payload.Name, ServiceCode: payload.ServiceCode,
			Status: payload.Status, TopologyRevision: 1,
			CreatedTick: targetTick, UpdatedTick: targetTick, Version: 1,
			SourceFactTick: targetTick, SourceFactSequence: factSequence,
			Metadata: metadata,
		}
		delta = 1
	} else {
		if payload.ExpectedVersion != existing.value.Version {
			return cityPhysicalNetworkExecution{}, cityPhysicalNetworkReject(cityPhysicalNetworkRejectionVersion)
		}
		if payload.ServiceCode != existing.value.ServiceCode || serviceID != existing.serviceID {
			return cityPhysicalNetworkExecution{}, cityPhysicalNetworkReject(cityPhysicalNetworkRejectionBinding)
		}
		if !isCityPhysicalNetworkTransition(existing.value.Status, payload.Status) {
			return cityPhysicalNetworkExecution{}, cityPhysicalNetworkReject(cityPhysicalNetworkRejectionTransition)
		}
		beforeValue := existing.value
		before = &beforeValue
		after = existing.value
		after.Name = payload.Name
		after.Status = payload.Status
		after.TopologyRevision++
		after.UpdatedTick = targetTick
		after.Version++
		after.SourceFactTick = targetTick
		after.SourceFactSequence = factSequence
		after.Metadata = metadata
	}
	if existing != nil && payload.Status == CityNetworkStatusRetired {
		var remaining int64
		if err = tx.QueryRowContext(ctx, `
SELECT (SELECT COUNT(*) FROM city_physical_network_nodes node
        WHERE node.world_id = $1 AND node.network_id = $2 AND node.status <> 'retired')
     + (SELECT COUNT(*) FROM city_physical_network_edges edge
        WHERE edge.world_id = $1 AND edge.network_id = $2 AND edge.status <> 'retired')`,
			worldID, existing.id).Scan(&remaining); err != nil {
			return cityPhysicalNetworkExecution{}, fmt.Errorf("inspect retiring physical network: %w", err)
		}
		if remaining != 0 {
			return cityPhysicalNetworkExecution{}, cityPhysicalNetworkReject(cityPhysicalNetworkRejectionTopology)
		}
	}
	fact, err := insertCityPhysicalNetworkCommandFact(ctx, tx, cityPhysicalNetworkFactInsert{
		worldID: worldID, tick: targetTick, sequence: factSequence,
		sourceCommandID: command.ID, factType: CityPhysicalNetworkFactNetworkConfigured,
		subjectKind: "network", subjectCode: after.Code,
		versionBefore: payload.ExpectedVersion, versionAfter: after.Version,
		payload: cityPhysicalNetworkConfigureFactPayload{
			SchemaVersion: cityPhysicalNetworkSchemaVersion,
			NetworkBefore: before, NetworkAfter: after,
		},
	})
	if err != nil {
		return cityPhysicalNetworkExecution{}, err
	}
	var writeResult sql.Result
	if existing == nil {
		writeResult, err = tx.ExecContext(ctx, `
INSERT INTO city_physical_networks
    (world_id, code, name, service_definition_id, status, topology_revision,
     created_tick, updated_tick, version, source_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5, 1, $6, $6, 1, $7, $8::jsonb)`,
			worldID, after.Code, after.Name, serviceID, after.Status,
			targetTick, fact.id, after.Metadata)
	} else {
		writeResult, err = tx.ExecContext(ctx, `
UPDATE city_physical_networks
SET name = $3, status = $4, topology_revision = $5,
    updated_tick = $6, version = $7, source_fact_id = $8,
    metadata = $9::jsonb, updated_at = NOW()
WHERE world_id = $1 AND id = $2 AND version = $10`,
			worldID, existing.id, after.Name, after.Status, after.TopologyRevision,
			targetTick, after.Version, fact.id, after.Metadata, payload.ExpectedVersion)
	}
	if err != nil {
		return cityPhysicalNetworkExecution{}, fmt.Errorf("persist configured physical network: %w", err)
	}
	if err = requireCityPhysicalNetworkProjectionWrite(writeResult); err != nil {
		return cityPhysicalNetworkExecution{}, err
	}
	if err = advanceCityPhysicalNetworkProfile(ctx, tx, worldID,
		cityPhysicalNetworkProfileDelta{networks: delta}); err != nil {
		return cityPhysicalNetworkExecution{}, err
	}
	if err = postCityPhysicalNetworkFact(ctx, tx, fact.id); err != nil {
		return cityPhysicalNetworkExecution{}, err
	}
	return appliedCityPhysicalNetworkExecution(command, fact,
		"city.physical_network.configured", map[string]any{
			"network_code": after.Code, "service_code": after.ServiceCode,
			"status": after.Status, "version": after.Version,
		}), nil
}

func loadCityPhysicalNetworkCommandNetwork(
	ctx context.Context, queryer citySQLQueryer, worldID int64, code string, forUpdate bool,
) (*cityPhysicalNetworkCommandNetworkRef, error) {
	locking := ""
	if forUpdate {
		locking = " FOR UPDATE OF network"
	}
	item := &cityPhysicalNetworkCommandNetworkRef{}
	err := queryer.QueryRowContext(ctx, `
SELECT network.id, network.service_definition_id, policy.maximum_nodes,
       policy.maximum_edges, policy.allow_bidirectional,
       network.code, network.name, service.code, network.status,
       network.topology_revision, network.created_tick, network.updated_tick,
       network.version, COALESCE(fact.tick, 0), COALESCE(fact.sequence, 0),
       network.metadata
FROM city_physical_networks network
JOIN city_service_definitions service ON service.id = network.service_definition_id
JOIN city_physical_network_policies policy
  ON policy.world_id = network.world_id
 AND policy.service_definition_id = network.service_definition_id
LEFT JOIN city_physical_network_facts fact ON fact.id = network.source_fact_id
WHERE network.world_id = $1 AND network.code = $2`+locking, worldID, code).Scan(
		&item.id, &item.serviceID, &item.maximumNodes, &item.maximumEdges,
		&item.allowBidirectional, &item.value.Code, &item.value.Name,
		&item.value.ServiceCode, &item.value.Status, &item.value.TopologyRevision,
		&item.value.CreatedTick, &item.value.UpdatedTick, &item.value.Version,
		&item.value.SourceFactTick, &item.value.SourceFactSequence,
		&item.value.Metadata,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load physical network %s: %w", code, err)
	}
	return item, nil
}

func isCityPhysicalNetworkTransition(from, to string) bool {
	if from == CityNetworkStatusRetired {
		return to == CityNetworkStatusRetired
	}
	return isCityPhysicalNetworkStatus(from) && isCityPhysicalNetworkStatus(to)
}

func cityPhysicalNetworkExplicitMetadata(
	base, overlay json.RawMessage,
) (json.RawMessage, error) {
	values := make(map[string]any)
	for _, raw := range []json.RawMessage{base, overlay} {
		if len(raw) == 0 {
			continue
		}
		var decoded map[string]any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return nil, fmt.Errorf("decode physical network metadata: %w", err)
		}
		for key, value := range decoded {
			values[key] = value
		}
	}
	values["baseline_mode"] = "explicit"
	values["schema_version"] = cityPhysicalNetworkSchemaVersion
	result, err := json.Marshal(values)
	if err != nil {
		return nil, fmt.Errorf("encode physical network metadata: %w", err)
	}
	return result, nil
}

func cityPhysicalNetworkIsExplicit(item CityPhysicalNetwork) bool {
	var metadata struct {
		BaselineMode string `json:"baseline_mode"`
	}
	return json.Unmarshal(item.Metadata, &metadata) == nil && metadata.BaselineMode == "explicit"
}

func insertCityPhysicalNetworkCommandFact(
	ctx context.Context, tx *sql.Tx, input cityPhysicalNetworkFactInsert,
) (*cityPhysicalNetworkFactRecord, error) {
	payload, err := json.Marshal(input.payload)
	if err != nil {
		return nil, fmt.Errorf("marshal physical network command fact: %w", err)
	}
	var id int64
	if err = tx.QueryRowContext(ctx, `
INSERT INTO city_physical_network_facts
    (world_id, tick, sequence, phase, source_command_id, fact_type,
     subject_kind, subject_code, version_before, version_after, payload)
VALUES ($1, $2, $3, 'command', $4, $5, $6, $7, $8, $9, $10::jsonb)
RETURNING id`, input.worldID, input.tick, input.sequence, input.sourceCommandID,
		input.factType, input.subjectKind, input.subjectCode,
		input.versionBefore, input.versionAfter, payload).Scan(&id); err != nil {
		return nil, fmt.Errorf("insert physical network command fact: %w", err)
	}
	if _, err = tx.ExecContext(ctx,
		`SELECT set_config('sub2api.city_physical_network_fact_id', $1, TRUE)`,
		fmt.Sprintf("%d", id)); err != nil {
		return nil, fmt.Errorf("activate physical network command fact: %w", err)
	}
	commandSequence := int64(0)
	if err = tx.QueryRowContext(ctx, `
SELECT sequence FROM city_commands WHERE id = $1 AND world_id = $2`,
		input.sourceCommandID, input.worldID).Scan(&commandSequence); err != nil {
		return nil, fmt.Errorf("load physical network command sequence: %w", err)
	}
	return &cityPhysicalNetworkFactRecord{id: id, fact: CityPhysicalNetworkFact{
		Tick: input.tick, Sequence: input.sequence, Phase: "command",
		SourceCommandSequence: &commandSequence, FactType: input.factType,
		SubjectKind: input.subjectKind, SubjectCode: input.subjectCode,
		VersionBefore: input.versionBefore, VersionAfter: input.versionAfter,
		Payload: payload,
	}}, nil
}

func advanceCityPhysicalNetworkProfile(
	ctx context.Context, tx *sql.Tx, worldID int64,
	delta cityPhysicalNetworkProfileDelta,
) error {
	result, err := tx.ExecContext(ctx, `
UPDATE city_physical_network_profiles
SET network_count = network_count + $2,
    node_count = node_count + $3,
    edge_count = edge_count + $4,
    fact_count = fact_count + 1,
    revision = revision + 1,
    updated_at = NOW()
WHERE world_id = $1`, worldID, delta.networks, delta.nodes, delta.edges)
	if err != nil {
		return fmt.Errorf("advance physical network profile: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "physical_network_profile"})
	}
	return nil
}

func requireCityPhysicalNetworkProjectionWrite(result sql.Result) error {
	if result == nil {
		return ErrCitySimulationInvariant.WithMetadata(
			map[string]string{"field": "physical_network_projection_write"},
		)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect physical network projection write: %w", err)
	}
	if rows != 1 {
		return cityPhysicalNetworkReject(cityPhysicalNetworkRejectionVersion)
	}
	return nil
}

func postCityPhysicalNetworkFact(ctx context.Context, tx *sql.Tx, factID int64) error {
	result, err := tx.ExecContext(ctx, `
UPDATE city_physical_network_facts SET posted_at = NOW()
WHERE id = $1 AND posted_at IS NULL`, factID)
	if err != nil {
		return fmt.Errorf("post physical network fact: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "physical_network_fact"})
	}
	return nil
}

func appliedCityPhysicalNetworkExecution(
	command *CityCommand, fact *cityPhysicalNetworkFactRecord,
	eventType string, result map[string]any,
) cityPhysicalNetworkExecution {
	return cityPhysicalNetworkExecution{
		pending: cityPendingEvent{
			command: command, status: CityCommandStatusApplied,
			result: result, eventType: eventType, payload: result,
		},
		fact: &fact.fact,
	}
}

type cityPhysicalNetworkResolvedNodeBinding struct {
	capacityID   *int64
	demandID     *int64
	districtID   *int64
	buildingID   *int64
	capacityCode *string
	demandCode   *string
	districtCode *string
	buildingCode *string
}

func (s *CityEconomyService) configureCityPhysicalNetworkNode(
	ctx context.Context, tx *sql.Tx, worldID, targetTick, factSequence int64,
	command *CityCommand, payload cityPhysicalNetworkNodeConfigurePayload,
) (cityPhysicalNetworkExecution, error) {
	network, err := loadCityPhysicalNetworkCommandNetwork(ctx, tx, worldID, payload.NetworkCode, true)
	if err != nil {
		return cityPhysicalNetworkExecution{}, err
	}
	if network == nil {
		return cityPhysicalNetworkExecution{}, cityPhysicalNetworkReject(cityPhysicalNetworkRejectionNetworkNotFound)
	}
	if network.value.Status == CityNetworkStatusRetired || !cityPhysicalNetworkIsExplicit(network.value) {
		return cityPhysicalNetworkExecution{}, cityPhysicalNetworkReject(cityPhysicalNetworkRejectionTransition)
	}
	if payload.Status == CityNetworkNodeStatusActive && network.value.Status != CityNetworkStatusActive {
		return cityPhysicalNetworkExecution{}, cityPhysicalNetworkReject(cityPhysicalNetworkRejectionTopology)
	}
	existing, err := loadCityPhysicalNetworkCommandNode(ctx, tx, worldID, payload.Code, true)
	if err != nil {
		return cityPhysicalNetworkExecution{}, err
	}
	if existing == nil && payload.ExpectedVersion != 0 {
		return cityPhysicalNetworkExecution{}, cityPhysicalNetworkReject(cityPhysicalNetworkRejectionNodeNotFound)
	}
	if existing != nil {
		if payload.ExpectedVersion != existing.value.Version {
			return cityPhysicalNetworkExecution{}, cityPhysicalNetworkReject(cityPhysicalNetworkRejectionVersion)
		}
		if existing.networkID != network.id || existing.value.NetworkCode != network.value.Code {
			return cityPhysicalNetworkExecution{}, cityPhysicalNetworkReject(cityPhysicalNetworkRejectionConflict)
		}
		if !isCityPhysicalNodeTransition(existing.value.Status, payload.Status) {
			return cityPhysicalNetworkExecution{}, cityPhysicalNetworkReject(cityPhysicalNetworkRejectionTransition)
		}
	}
	binding, err := resolveCityPhysicalNetworkNodeBinding(ctx, tx, worldID, network, payload)
	if err != nil {
		return cityPhysicalNetworkExecution{}, err
	}
	if payload.Status == CityNetworkNodeStatusActive {
		var duplicate int64
		if err = tx.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM city_physical_network_nodes node
WHERE node.world_id = $1 AND node.network_id = $2 AND node.status = 'active'
  AND node.id <> $3
  AND (($4::BIGINT IS NOT NULL AND node.capacity_id = $4)
       OR ($5::BIGINT IS NOT NULL AND node.demand_id = $5))`,
			worldID, network.id, cityPhysicalNetworkExistingID(existing),
			binding.capacityID, binding.demandID).Scan(&duplicate); err != nil {
			return cityPhysicalNetworkExecution{}, fmt.Errorf("inspect physical network node binding: %w", err)
		}
		if duplicate != 0 {
			return cityPhysicalNetworkExecution{}, cityPhysicalNetworkReject(cityPhysicalNetworkRejectionConflict)
		}
	}
	if existing != nil && existing.value.Status == CityNetworkNodeStatusActive &&
		(payload.Status != CityNetworkNodeStatusActive ||
			existing.value.Role != payload.Role ||
			!sameOptionalString(existing.value.CapacityCode, binding.capacityCode) ||
			!sameOptionalString(existing.value.DemandCode, binding.demandCode)) {
		var activeEdges int64
		if err = tx.QueryRowContext(ctx, `
SELECT COUNT(*) FROM city_physical_network_edges edge
WHERE edge.world_id = $1 AND edge.status = 'active'
  AND (edge.from_node_id = $2 OR edge.to_node_id = $2)`,
			worldID, existing.id).Scan(&activeEdges); err != nil {
			return cityPhysicalNetworkExecution{}, fmt.Errorf("inspect active node edges: %w", err)
		}
		if activeEdges != 0 {
			return cityPhysicalNetworkExecution{}, cityPhysicalNetworkReject(cityPhysicalNetworkRejectionTopology)
		}
	}
	if existing == nil {
		var totalCount, networkCount int64
		if err = tx.QueryRowContext(ctx, `
SELECT profile.node_count,
       (SELECT COUNT(*) FROM city_physical_network_nodes node
        WHERE node.world_id = profile.world_id AND node.network_id = $2)
FROM city_physical_network_profiles profile WHERE profile.world_id = $1
FOR UPDATE`, worldID, network.id).Scan(&totalCount, &networkCount); err != nil {
			return cityPhysicalNetworkExecution{}, fmt.Errorf("inspect physical network node capacity: %w", err)
		}
		if totalCount >= cityPhysicalNetworkMaximumNodes || networkCount >= int64(network.maximumNodes) {
			return cityPhysicalNetworkExecution{}, cityPhysicalNetworkReject(cityPhysicalNetworkRejectionLimit)
		}
	}
	metadataBase := json.RawMessage(nil)
	if existing != nil {
		metadataBase = existing.value.Metadata
	}
	metadata, err := cityPhysicalNetworkExplicitMetadata(metadataBase, payload.Metadata)
	if err != nil {
		return cityPhysicalNetworkExecution{}, err
	}
	networkBefore := network.value
	networkAfter := advanceCityPhysicalNetworkTopologySnapshot(networkBefore, targetTick, factSequence)
	nodeAfter := CityPhysicalNetworkNode{
		Code: payload.Code, NetworkCode: network.value.Code, Role: payload.Role,
		CapacityCode: binding.capacityCode, DemandCode: binding.demandCode,
		DistrictCode: binding.districtCode, BuildingCode: binding.buildingCode,
		WorldX: payload.WorldX, WorldY: payload.WorldY, WorldZ: payload.WorldZ,
		Status: payload.Status, CreatedTick: targetTick, UpdatedTick: targetTick,
		Version: 1, SourceFactTick: targetTick, SourceFactSequence: factSequence,
		Metadata: metadata,
	}
	var nodeBefore *CityPhysicalNetworkNode
	delta := int64(1)
	if existing != nil {
		beforeValue := existing.value
		nodeBefore = &beforeValue
		nodeAfter.CreatedTick = existing.value.CreatedTick
		nodeAfter.Version = existing.value.Version + 1
		delta = 0
	}
	fact, err := insertCityPhysicalNetworkCommandFact(ctx, tx, cityPhysicalNetworkFactInsert{
		worldID: worldID, tick: targetTick, sequence: factSequence,
		sourceCommandID: command.ID, factType: CityPhysicalNetworkFactNodeConfigured,
		subjectKind: "node", subjectCode: nodeAfter.Code,
		versionBefore: payload.ExpectedVersion, versionAfter: nodeAfter.Version,
		payload: cityPhysicalNetworkNodeConfigureFactPayload{
			SchemaVersion: cityPhysicalNetworkSchemaVersion,
			NetworkBefore: networkBefore, NetworkAfter: networkAfter,
			NodeBefore: nodeBefore, NodeAfter: nodeAfter,
		},
	})
	if err != nil {
		return cityPhysicalNetworkExecution{}, err
	}
	if err = updateCityPhysicalNetworkTopologySnapshot(ctx, tx, worldID, network.id,
		networkBefore.Version, networkAfter, fact.id); err != nil {
		return cityPhysicalNetworkExecution{}, err
	}
	var writeResult sql.Result
	if existing == nil {
		writeResult, err = tx.ExecContext(ctx, `
INSERT INTO city_physical_network_nodes
    (world_id, network_id, code, role, capacity_id, demand_id, district_id,
     building_id, world_x, world_y, world_z, status, created_tick,
     updated_tick, version, source_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12,
        $13, $13, 1, $14, $15::jsonb)`, worldID, network.id, nodeAfter.Code,
			nodeAfter.Role, binding.capacityID, binding.demandID, binding.districtID,
			binding.buildingID, nodeAfter.WorldX, nodeAfter.WorldY, nodeAfter.WorldZ,
			nodeAfter.Status, targetTick, fact.id, nodeAfter.Metadata)
	} else {
		writeResult, err = tx.ExecContext(ctx, `
UPDATE city_physical_network_nodes
SET role = $3, capacity_id = $4, demand_id = $5, district_id = $6,
    building_id = $7, world_x = $8, world_y = $9, world_z = $10,
    status = $11, updated_tick = $12, version = $13,
    source_fact_id = $14, metadata = $15::jsonb, updated_at = NOW()
WHERE world_id = $1 AND id = $2 AND version = $16`, worldID, existing.id,
			nodeAfter.Role, binding.capacityID, binding.demandID, binding.districtID,
			binding.buildingID, nodeAfter.WorldX, nodeAfter.WorldY, nodeAfter.WorldZ,
			nodeAfter.Status, targetTick, nodeAfter.Version, fact.id,
			nodeAfter.Metadata, payload.ExpectedVersion)
	}
	if err != nil {
		return cityPhysicalNetworkExecution{}, fmt.Errorf("persist configured physical network node: %w", err)
	}
	if err = requireCityPhysicalNetworkProjectionWrite(writeResult); err != nil {
		return cityPhysicalNetworkExecution{}, err
	}
	if err = advanceCityPhysicalNetworkProfile(ctx, tx, worldID,
		cityPhysicalNetworkProfileDelta{nodes: delta}); err != nil {
		return cityPhysicalNetworkExecution{}, err
	}
	if err = postCityPhysicalNetworkFact(ctx, tx, fact.id); err != nil {
		return cityPhysicalNetworkExecution{}, err
	}
	return appliedCityPhysicalNetworkExecution(command, fact,
		"city.physical_network.node.configured", map[string]any{
			"network_code": nodeAfter.NetworkCode, "node_code": nodeAfter.Code,
			"role": nodeAfter.Role, "status": nodeAfter.Status,
			"version": nodeAfter.Version,
		}), nil
}

func loadCityPhysicalNetworkCommandNode(
	ctx context.Context, queryer citySQLQueryer, worldID int64, code string, forUpdate bool,
) (*cityPhysicalNetworkCommandNodeRef, error) {
	locking := ""
	if forUpdate {
		locking = " FOR UPDATE OF node"
	}
	item := &cityPhysicalNetworkCommandNodeRef{}
	var capacity, demand, district, building sql.NullString
	var worldX, worldY sql.NullInt64
	var worldZ sql.NullInt32
	err := queryer.QueryRowContext(ctx, `
SELECT node.id, node.network_id, node.code, network.code, node.role,
       CASE WHEN capacity.id IS NULL THEN NULL ELSE facility.code || '.' || service.code END,
       demand.code, district.code, building.code,
       node.world_x, node.world_y, node.world_z, node.status,
       node.created_tick, node.updated_tick, node.version,
       COALESCE(fact.tick, 0), COALESCE(fact.sequence, 0), node.metadata
FROM city_physical_network_nodes node
JOIN city_physical_networks network ON network.id = node.network_id
LEFT JOIN city_facility_service_capacities capacity ON capacity.id = node.capacity_id
LEFT JOIN city_facilities facility ON facility.id = capacity.facility_id
LEFT JOIN city_service_definitions service ON service.id = capacity.service_definition_id
LEFT JOIN city_service_demands demand ON demand.id = node.demand_id
LEFT JOIN city_districts district ON district.id = node.district_id
LEFT JOIN city_buildings building ON building.id = node.building_id
LEFT JOIN city_physical_network_facts fact ON fact.id = node.source_fact_id
WHERE node.world_id = $1 AND node.code = $2`+locking, worldID, code).Scan(
		&item.id, &item.networkID, &item.value.Code, &item.value.NetworkCode,
		&item.value.Role, &capacity, &demand, &district, &building,
		&worldX, &worldY, &worldZ, &item.value.Status,
		&item.value.CreatedTick, &item.value.UpdatedTick, &item.value.Version,
		&item.value.SourceFactTick, &item.value.SourceFactSequence,
		&item.value.Metadata,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load physical network node %s: %w", code, err)
	}
	item.value.CapacityCode = nullStringPointer(capacity)
	item.value.DemandCode = nullStringPointer(demand)
	item.value.DistrictCode = nullStringPointer(district)
	item.value.BuildingCode = nullStringPointer(building)
	item.value.WorldX = nullInt64Pointer(worldX)
	item.value.WorldY = nullInt64Pointer(worldY)
	if worldZ.Valid {
		value := int(worldZ.Int32)
		item.value.WorldZ = &value
	}
	return item, nil
}

func resolveCityPhysicalNetworkNodeBinding(
	ctx context.Context, queryer citySQLQueryer, worldID int64,
	network *cityPhysicalNetworkCommandNetworkRef,
	payload cityPhysicalNetworkNodeConfigurePayload,
) (cityPhysicalNetworkResolvedNodeBinding, error) {
	result := cityPhysicalNetworkResolvedNodeBinding{}
	var bindingDistrictID int64
	var bindingDistrictCode string
	var bindingBuildingID sql.NullInt64
	var bindingBuildingCode sql.NullString
	switch payload.Role {
	case CityNetworkNodeRoleSupply:
		var capacityID int64
		var serviceCode string
		err := queryer.QueryRowContext(ctx, `
SELECT capacity.id, service.code, district.id, district.code,
       facility.building_id, building.code
FROM city_facility_service_capacities capacity
JOIN city_facilities facility ON facility.id = capacity.facility_id
JOIN city_service_definitions service ON service.id = capacity.service_definition_id
JOIN city_districts district ON district.id = facility.district_id
JOIN city_buildings building ON building.id = facility.building_id
WHERE capacity.world_id = $1 AND facility.code || '.' || service.code = $2`,
			worldID, payload.CapacityCode).Scan(&capacityID, &serviceCode,
			&bindingDistrictID, &bindingDistrictCode, &bindingBuildingID,
			&bindingBuildingCode)
		if errors.Is(err, sql.ErrNoRows) || serviceCode != network.value.ServiceCode {
			return result, cityPhysicalNetworkReject(cityPhysicalNetworkRejectionBinding)
		}
		if err != nil {
			return result, fmt.Errorf("resolve physical network supply binding: %w", err)
		}
		result.capacityID = &capacityID
		result.capacityCode = stringPointer(payload.CapacityCode)
	case CityNetworkNodeRoleDemand:
		var demandID int64
		var serviceCode string
		err := queryer.QueryRowContext(ctx, `
SELECT demand.id, service.code, district.id, district.code,
       demand.building_id, building.code
FROM city_service_demands demand
JOIN city_service_definitions service ON service.id = demand.service_definition_id
JOIN city_districts district ON district.id = demand.district_id
LEFT JOIN city_buildings building ON building.id = demand.building_id
WHERE demand.world_id = $1 AND demand.code = $2`, worldID, payload.DemandCode).Scan(
			&demandID, &serviceCode, &bindingDistrictID, &bindingDistrictCode,
			&bindingBuildingID, &bindingBuildingCode,
		)
		if errors.Is(err, sql.ErrNoRows) || serviceCode != network.value.ServiceCode {
			return result, cityPhysicalNetworkReject(cityPhysicalNetworkRejectionBinding)
		}
		if err != nil {
			return result, fmt.Errorf("resolve physical network demand binding: %w", err)
		}
		result.demandID = &demandID
		result.demandCode = stringPointer(payload.DemandCode)
	default:
		if payload.BuildingCode != "" {
			if err := queryer.QueryRowContext(ctx, `
SELECT building.id, district.id, district.code
FROM city_buildings building
JOIN city_districts district ON district.id = building.district_id
WHERE building.world_id = $1 AND building.code = $2`,
				worldID, payload.BuildingCode).Scan(
				&bindingBuildingID, &bindingDistrictID, &bindingDistrictCode,
			); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return result, cityPhysicalNetworkReject(cityPhysicalNetworkRejectionBinding)
				}
				return result, fmt.Errorf("resolve physical network node building: %w", err)
			}
			bindingBuildingCode = sql.NullString{String: payload.BuildingCode, Valid: true}
		} else if payload.DistrictCode != "" {
			if err := queryer.QueryRowContext(ctx, `
SELECT id, code FROM city_districts WHERE world_id = $1 AND code = $2`,
				worldID, payload.DistrictCode).Scan(&bindingDistrictID, &bindingDistrictCode); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return result, cityPhysicalNetworkReject(cityPhysicalNetworkRejectionBinding)
				}
				return result, fmt.Errorf("resolve physical network node district: %w", err)
			}
		}
	}
	if payload.DistrictCode != "" && bindingDistrictCode != payload.DistrictCode {
		return result, cityPhysicalNetworkReject(cityPhysicalNetworkRejectionBinding)
	}
	if payload.BuildingCode != "" && (!bindingBuildingCode.Valid || bindingBuildingCode.String != payload.BuildingCode) {
		return result, cityPhysicalNetworkReject(cityPhysicalNetworkRejectionBinding)
	}
	if bindingDistrictID > 0 {
		result.districtID = int64Pointer(bindingDistrictID)
		result.districtCode = stringPointer(bindingDistrictCode)
	}
	if bindingBuildingID.Valid {
		result.buildingID = int64Pointer(bindingBuildingID.Int64)
		result.buildingCode = stringPointer(bindingBuildingCode.String)
	}
	return result, nil
}

func cityPhysicalNetworkExistingID(item *cityPhysicalNetworkCommandNodeRef) int64 {
	if item == nil {
		return 0
	}
	return item.id
}

func isCityPhysicalNodeTransition(from, to string) bool {
	if from == CityNetworkNodeStatusRetired {
		return to == CityNetworkNodeStatusRetired
	}
	return isCityPhysicalNetworkNodeStatus(from) && isCityPhysicalNetworkNodeStatus(to)
}

func advanceCityPhysicalNetworkTopologySnapshot(
	before CityPhysicalNetwork, targetTick, factSequence int64,
) CityPhysicalNetwork {
	after := before
	after.TopologyRevision++
	after.UpdatedTick = targetTick
	after.Version++
	after.SourceFactTick = targetTick
	after.SourceFactSequence = factSequence
	return after
}

func updateCityPhysicalNetworkTopologySnapshot(
	ctx context.Context, tx *sql.Tx, worldID, networkID, expectedVersion int64,
	after CityPhysicalNetwork, factID int64,
) error {
	result, err := tx.ExecContext(ctx, `
UPDATE city_physical_networks
SET topology_revision = $4, updated_tick = $5, version = $6,
    source_fact_id = $7, updated_at = NOW()
WHERE world_id = $1 AND id = $2 AND version = $3`, worldID, networkID,
		expectedVersion, after.TopologyRevision, after.UpdatedTick,
		after.Version, factID)
	if err != nil {
		return fmt.Errorf("advance physical network topology revision: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return cityPhysicalNetworkReject(cityPhysicalNetworkRejectionVersion)
	}
	return nil
}

func (s *CityEconomyService) configureCityPhysicalNetworkEdge(
	ctx context.Context, tx *sql.Tx, worldID, targetTick, factSequence int64,
	command *CityCommand, payload cityPhysicalNetworkEdgeConfigurePayload,
) (cityPhysicalNetworkExecution, error) {
	network, err := loadCityPhysicalNetworkCommandNetwork(ctx, tx, worldID, payload.NetworkCode, true)
	if err != nil {
		return cityPhysicalNetworkExecution{}, err
	}
	if network == nil {
		return cityPhysicalNetworkExecution{}, cityPhysicalNetworkReject(cityPhysicalNetworkRejectionNetworkNotFound)
	}
	if network.value.Status == CityNetworkStatusRetired || !cityPhysicalNetworkIsExplicit(network.value) {
		return cityPhysicalNetworkExecution{}, cityPhysicalNetworkReject(cityPhysicalNetworkRejectionTransition)
	}
	if payload.Direction == CityNetworkEdgeDirectionBidirectional && !network.allowBidirectional {
		return cityPhysicalNetworkExecution{}, cityPhysicalNetworkReject(cityPhysicalNetworkRejectionTopology)
	}
	from, err := loadCityPhysicalNetworkCommandNode(ctx, tx, worldID, payload.FromNodeCode, true)
	if err != nil {
		return cityPhysicalNetworkExecution{}, err
	}
	to, err := loadCityPhysicalNetworkCommandNode(ctx, tx, worldID, payload.ToNodeCode, true)
	if err != nil {
		return cityPhysicalNetworkExecution{}, err
	}
	if from == nil || to == nil || from.networkID != network.id || to.networkID != network.id {
		return cityPhysicalNetworkExecution{}, cityPhysicalNetworkReject(cityPhysicalNetworkRejectionBinding)
	}
	if payload.Status == CityNetworkEdgeStatusActive &&
		(network.value.Status != CityNetworkStatusActive || from.value.Status != CityNetworkNodeStatusActive ||
			to.value.Status != CityNetworkNodeStatusActive) {
		return cityPhysicalNetworkExecution{}, cityPhysicalNetworkReject(cityPhysicalNetworkRejectionTopology)
	}
	existing, err := loadCityPhysicalNetworkCommandEdge(ctx, tx, worldID, payload.Code, true)
	if err != nil {
		return cityPhysicalNetworkExecution{}, err
	}
	if existing == nil && payload.ExpectedVersion != 0 {
		return cityPhysicalNetworkExecution{}, cityPhysicalNetworkReject(cityPhysicalNetworkRejectionEdgeNotFound)
	}
	if existing != nil {
		if payload.ExpectedVersion != existing.value.Version {
			return cityPhysicalNetworkExecution{}, cityPhysicalNetworkReject(cityPhysicalNetworkRejectionVersion)
		}
		if existing.networkID != network.id || existing.value.NetworkCode != network.value.Code {
			return cityPhysicalNetworkExecution{}, cityPhysicalNetworkReject(cityPhysicalNetworkRejectionConflict)
		}
		if existing.value.Status == CityNetworkEdgeStatusRetired || payload.Status != existing.value.Status {
			return cityPhysicalNetworkExecution{}, cityPhysicalNetworkReject(cityPhysicalNetworkRejectionTransition)
		}
		if existing.value.Status == CityNetworkEdgeStatusActive &&
			(existing.value.FromNodeCode != payload.FromNodeCode ||
				existing.value.ToNodeCode != payload.ToNodeCode ||
				existing.value.Direction != payload.Direction) {
			return cityPhysicalNetworkExecution{}, cityPhysicalNetworkReject(cityPhysicalNetworkRejectionTopology)
		}
	}
	if existing == nil {
		var totalCount, networkCount int64
		if err = tx.QueryRowContext(ctx, `
SELECT profile.edge_count,
       (SELECT COUNT(*) FROM city_physical_network_edges edge
        WHERE edge.world_id = profile.world_id AND edge.network_id = $2)
FROM city_physical_network_profiles profile WHERE profile.world_id = $1
FOR UPDATE`, worldID, network.id).Scan(&totalCount, &networkCount); err != nil {
			return cityPhysicalNetworkExecution{}, fmt.Errorf("inspect physical network edge capacity: %w", err)
		}
		if totalCount >= cityPhysicalNetworkMaximumEdges || networkCount >= int64(network.maximumEdges) {
			return cityPhysicalNetworkExecution{}, cityPhysicalNetworkReject(cityPhysicalNetworkRejectionLimit)
		}
	}
	available, err := cityMulDivFloor(payload.InstalledCapacityUnits, payload.AvailabilityMilli, 1000)
	if err != nil {
		return cityPhysicalNetworkExecution{}, ErrCitySimulationInvariant.WithMetadata(
			map[string]string{"field": "physical_network_edge_capacity"},
		)
	}
	if payload.Status == CityNetworkEdgeStatusActive && available <= 0 {
		return cityPhysicalNetworkExecution{}, cityPhysicalNetworkReject(cityPhysicalNetworkRejectionTopology)
	}
	metadataBase := json.RawMessage(nil)
	if existing != nil {
		metadataBase = existing.value.Metadata
	}
	metadata, err := cityPhysicalNetworkExplicitMetadata(metadataBase, payload.Metadata)
	if err != nil {
		return cityPhysicalNetworkExecution{}, err
	}
	networkBefore := network.value
	networkAfter := advanceCityPhysicalNetworkTopologySnapshot(networkBefore, targetTick, factSequence)
	edgeAfter := CityPhysicalNetworkEdge{
		Code: payload.Code, NetworkCode: network.value.Code,
		FromNodeCode: payload.FromNodeCode, ToNodeCode: payload.ToNodeCode,
		Direction: payload.Direction, InstalledCapacityUnits: payload.InstalledCapacityUnits,
		AvailabilityMilli: payload.AvailabilityMilli, AvailableCapacityUnits: available,
		LossMilli: payload.LossMilli, BaseCostUnits: payload.BaseCostUnits,
		Status: payload.Status, ConditionMilli: 1000, FailureCount: 0,
		CreatedTick: targetTick, UpdatedTick: targetTick, Version: 1,
		SourceFactTick: targetTick, SourceFactSequence: factSequence,
		Metadata: metadata,
	}
	var edgeBefore *CityPhysicalNetworkEdge
	delta := int64(1)
	if existing != nil {
		beforeValue := existing.value
		edgeBefore = &beforeValue
		edgeAfter.ConditionMilli = existing.value.ConditionMilli
		edgeAfter.FailureCount = existing.value.FailureCount
		edgeAfter.CreatedTick = existing.value.CreatedTick
		edgeAfter.Version = existing.value.Version + 1
		delta = 0
	}
	fact, err := insertCityPhysicalNetworkCommandFact(ctx, tx, cityPhysicalNetworkFactInsert{
		worldID: worldID, tick: targetTick, sequence: factSequence,
		sourceCommandID: command.ID, factType: CityPhysicalNetworkFactEdgeConfigured,
		subjectKind: "edge", subjectCode: edgeAfter.Code,
		versionBefore: payload.ExpectedVersion, versionAfter: edgeAfter.Version,
		payload: cityPhysicalNetworkEdgeConfigureFactPayload{
			SchemaVersion: cityPhysicalNetworkSchemaVersion,
			NetworkBefore: networkBefore, NetworkAfter: networkAfter,
			EdgeBefore: edgeBefore, EdgeAfter: edgeAfter,
		},
	})
	if err != nil {
		return cityPhysicalNetworkExecution{}, err
	}
	if err = updateCityPhysicalNetworkTopologySnapshot(ctx, tx, worldID, network.id,
		networkBefore.Version, networkAfter, fact.id); err != nil {
		return cityPhysicalNetworkExecution{}, err
	}
	var writeResult sql.Result
	if existing == nil {
		writeResult, err = tx.ExecContext(ctx, `
INSERT INTO city_physical_network_edges
    (world_id, network_id, code, from_node_id, to_node_id, direction,
     installed_capacity_units, availability_milli, available_capacity_units,
     loss_milli, base_cost_units, status, condition_milli, failure_count,
     created_tick, updated_tick, version, source_fact_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12,
        $13, 0, $14, $14, 1, $15, $16::jsonb)`, worldID, network.id,
			edgeAfter.Code, from.id, to.id, edgeAfter.Direction,
			edgeAfter.InstalledCapacityUnits, edgeAfter.AvailabilityMilli,
			edgeAfter.AvailableCapacityUnits, edgeAfter.LossMilli,
			edgeAfter.BaseCostUnits, edgeAfter.Status, edgeAfter.ConditionMilli,
			targetTick, fact.id, edgeAfter.Metadata)
	} else {
		writeResult, err = tx.ExecContext(ctx, `
UPDATE city_physical_network_edges
SET from_node_id = $3, to_node_id = $4, direction = $5,
    installed_capacity_units = $6, availability_milli = $7,
    available_capacity_units = $8, loss_milli = $9, base_cost_units = $10,
    condition_milli = $11, updated_tick = $12, version = $13,
    source_fact_id = $14, metadata = $15::jsonb, updated_at = NOW()
WHERE world_id = $1 AND id = $2 AND version = $16`, worldID, existing.id,
			from.id, to.id, edgeAfter.Direction, edgeAfter.InstalledCapacityUnits,
			edgeAfter.AvailabilityMilli, edgeAfter.AvailableCapacityUnits,
			edgeAfter.LossMilli, edgeAfter.BaseCostUnits, edgeAfter.ConditionMilli,
			targetTick, edgeAfter.Version, fact.id, edgeAfter.Metadata,
			payload.ExpectedVersion)
	}
	if err != nil {
		return cityPhysicalNetworkExecution{}, fmt.Errorf("persist configured physical network edge: %w", err)
	}
	if err = requireCityPhysicalNetworkProjectionWrite(writeResult); err != nil {
		return cityPhysicalNetworkExecution{}, err
	}
	if err = advanceCityPhysicalNetworkProfile(ctx, tx, worldID,
		cityPhysicalNetworkProfileDelta{edges: delta}); err != nil {
		return cityPhysicalNetworkExecution{}, err
	}
	if err = postCityPhysicalNetworkFact(ctx, tx, fact.id); err != nil {
		return cityPhysicalNetworkExecution{}, err
	}
	return appliedCityPhysicalNetworkExecution(command, fact,
		"city.physical_network.edge.configured", map[string]any{
			"network_code": edgeAfter.NetworkCode, "edge_code": edgeAfter.Code,
			"status": edgeAfter.Status, "version": edgeAfter.Version,
		}), nil
}

func (s *CityEconomyService) transitionCityPhysicalNetworkEdge(
	ctx context.Context, tx *sql.Tx, worldID, targetTick, factSequence int64,
	command *CityCommand, payload cityPhysicalNetworkEdgeTransitionPayload,
) (cityPhysicalNetworkExecution, error) {
	edge, err := loadCityPhysicalNetworkCommandEdge(ctx, tx, worldID, payload.EdgeCode, true)
	if err != nil {
		return cityPhysicalNetworkExecution{}, err
	}
	if edge == nil {
		return cityPhysicalNetworkExecution{}, cityPhysicalNetworkReject(cityPhysicalNetworkRejectionEdgeNotFound)
	}
	if payload.ExpectedVersion != edge.value.Version {
		return cityPhysicalNetworkExecution{}, cityPhysicalNetworkReject(cityPhysicalNetworkRejectionVersion)
	}
	if !isCityPhysicalEdgeTransition(edge.value.Status, payload.ToStatus) {
		return cityPhysicalNetworkExecution{}, cityPhysicalNetworkReject(cityPhysicalNetworkRejectionTransition)
	}
	network, err := loadCityPhysicalNetworkCommandNetwork(ctx, tx, worldID, edge.value.NetworkCode, true)
	if err != nil {
		return cityPhysicalNetworkExecution{}, err
	}
	if network == nil || network.id != edge.networkID || !cityPhysicalNetworkIsExplicit(network.value) {
		return cityPhysicalNetworkExecution{}, cityPhysicalNetworkReject(cityPhysicalNetworkRejectionTopology)
	}
	if payload.ToStatus == CityNetworkEdgeStatusActive {
		from, loadErr := loadCityPhysicalNetworkCommandNode(ctx, tx, worldID, edge.value.FromNodeCode, true)
		if loadErr != nil {
			return cityPhysicalNetworkExecution{}, loadErr
		}
		to, loadErr := loadCityPhysicalNetworkCommandNode(ctx, tx, worldID, edge.value.ToNodeCode, true)
		if loadErr != nil {
			return cityPhysicalNetworkExecution{}, loadErr
		}
		if network.value.Status != CityNetworkStatusActive || from == nil || to == nil ||
			from.value.Status != CityNetworkNodeStatusActive || to.value.Status != CityNetworkNodeStatusActive ||
			edge.value.AvailableCapacityUnits <= 0 || edge.value.ConditionMilli <= 0 {
			return cityPhysicalNetworkExecution{}, cityPhysicalNetworkReject(cityPhysicalNetworkRejectionTopology)
		}
	}
	metadata, err := cityPhysicalNetworkExplicitMetadata(edge.value.Metadata, payload.Metadata)
	if err != nil {
		return cityPhysicalNetworkExecution{}, err
	}
	networkBefore := network.value
	networkAfter := advanceCityPhysicalNetworkTopologySnapshot(networkBefore, targetTick, factSequence)
	edgeBefore := edge.value
	edgeAfter := edge.value
	edgeAfter.Status = payload.ToStatus
	edgeAfter.UpdatedTick = targetTick
	edgeAfter.Version++
	edgeAfter.SourceFactTick = targetTick
	edgeAfter.SourceFactSequence = factSequence
	edgeAfter.Metadata = metadata
	if payload.ToStatus == CityNetworkEdgeStatusFailed {
		edgeAfter.ConditionMilli = 0
		edgeAfter.FailureCount++
	}
	fact, err := insertCityPhysicalNetworkCommandFact(ctx, tx, cityPhysicalNetworkFactInsert{
		worldID: worldID, tick: targetTick, sequence: factSequence,
		sourceCommandID: command.ID, factType: CityPhysicalNetworkFactEdgeStateChanged,
		subjectKind: "edge", subjectCode: edgeAfter.Code,
		versionBefore: edgeBefore.Version, versionAfter: edgeAfter.Version,
		payload: cityPhysicalNetworkEdgeConfigureFactPayload{
			SchemaVersion: cityPhysicalNetworkSchemaVersion,
			NetworkBefore: networkBefore, NetworkAfter: networkAfter,
			EdgeBefore: &edgeBefore, EdgeAfter: edgeAfter,
		},
	})
	if err != nil {
		return cityPhysicalNetworkExecution{}, err
	}
	if err = updateCityPhysicalNetworkTopologySnapshot(ctx, tx, worldID, network.id,
		networkBefore.Version, networkAfter, fact.id); err != nil {
		return cityPhysicalNetworkExecution{}, err
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_physical_network_edges
SET status = $3, condition_milli = $4, failure_count = $5,
    updated_tick = $6, version = $7, source_fact_id = $8,
    metadata = $9::jsonb, updated_at = NOW()
WHERE world_id = $1 AND id = $2 AND version = $10`, worldID, edge.id,
		edgeAfter.Status, edgeAfter.ConditionMilli, edgeAfter.FailureCount,
		targetTick, edgeAfter.Version, fact.id, edgeAfter.Metadata,
		payload.ExpectedVersion)
	if err != nil {
		return cityPhysicalNetworkExecution{}, fmt.Errorf("persist physical network edge transition: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return cityPhysicalNetworkExecution{}, cityPhysicalNetworkReject(cityPhysicalNetworkRejectionVersion)
	}
	if err = advanceCityPhysicalNetworkProfile(ctx, tx, worldID,
		cityPhysicalNetworkProfileDelta{}); err != nil {
		return cityPhysicalNetworkExecution{}, err
	}
	if err = postCityPhysicalNetworkFact(ctx, tx, fact.id); err != nil {
		return cityPhysicalNetworkExecution{}, err
	}
	return appliedCityPhysicalNetworkExecution(command, fact,
		"city.physical_network.edge.state_changed", map[string]any{
			"network_code": edgeAfter.NetworkCode, "edge_code": edgeAfter.Code,
			"from_status": edgeBefore.Status, "to_status": edgeAfter.Status,
			"version": edgeAfter.Version,
		}), nil
}

func loadCityPhysicalNetworkCommandEdge(
	ctx context.Context, queryer citySQLQueryer, worldID int64, code string, forUpdate bool,
) (*cityPhysicalNetworkCommandEdgeRef, error) {
	locking := ""
	if forUpdate {
		locking = " FOR UPDATE OF edge"
	}
	item := &cityPhysicalNetworkCommandEdgeRef{}
	err := queryer.QueryRowContext(ctx, `
SELECT edge.id, edge.network_id, edge.from_node_id, edge.to_node_id,
       edge.code, network.code, source.code, sink.code, edge.direction,
       edge.installed_capacity_units, edge.availability_milli,
       edge.available_capacity_units, edge.loss_milli, edge.base_cost_units,
       edge.status, edge.condition_milli, edge.failure_count,
       edge.created_tick, edge.updated_tick, edge.version,
       COALESCE(fact.tick, 0), COALESCE(fact.sequence, 0), edge.metadata
FROM city_physical_network_edges edge
JOIN city_physical_networks network ON network.id = edge.network_id
JOIN city_physical_network_nodes source ON source.id = edge.from_node_id
JOIN city_physical_network_nodes sink ON sink.id = edge.to_node_id
LEFT JOIN city_physical_network_facts fact ON fact.id = edge.source_fact_id
WHERE edge.world_id = $1 AND edge.code = $2`+locking, worldID, code).Scan(
		&item.id, &item.networkID, &item.fromID, &item.toID,
		&item.value.Code, &item.value.NetworkCode, &item.value.FromNodeCode,
		&item.value.ToNodeCode, &item.value.Direction,
		&item.value.InstalledCapacityUnits, &item.value.AvailabilityMilli,
		&item.value.AvailableCapacityUnits, &item.value.LossMilli,
		&item.value.BaseCostUnits, &item.value.Status, &item.value.ConditionMilli,
		&item.value.FailureCount, &item.value.CreatedTick, &item.value.UpdatedTick,
		&item.value.Version, &item.value.SourceFactTick,
		&item.value.SourceFactSequence, &item.value.Metadata,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load physical network edge %s: %w", code, err)
	}
	return item, nil
}

func isCityPhysicalEdgeTransition(from, to string) bool {
	if from == to || from == CityNetworkEdgeStatusRetired {
		return false
	}
	switch from {
	case CityNetworkEdgeStatusActive:
		return to == CityNetworkEdgeStatusIsolated || to == CityNetworkEdgeStatusFailed ||
			to == CityNetworkEdgeStatusRetired
	case CityNetworkEdgeStatusIsolated:
		return to == CityNetworkEdgeStatusActive || to == CityNetworkEdgeStatusFailed ||
			to == CityNetworkEdgeStatusRetired
	case CityNetworkEdgeStatusFailed:
		return to == CityNetworkEdgeStatusIsolated || to == CityNetworkEdgeStatusRetired
	default:
		return false
	}
}
