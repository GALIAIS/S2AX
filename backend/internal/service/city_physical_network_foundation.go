package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const (
	cityPhysicalNetworkAlgorithmVersion = "1.0.0"
	cityPhysicalNetworkDefaultMaxPaths  = 16
	cityPhysicalNetworkDefaultMaxHops   = 64
	cityPhysicalNetworkLossCostWeight   = int64(10)
)

var cityPhysicalNetworkRequiredServices = map[string]struct{}{
	"electric_power": {},
	"potable_water":  {},
	"solid_waste":    {},
	"wastewater":     {},
}

type cityPhysicalNetworkPolicySeed struct {
	ServiceCode        string `json:"service_code"`
	NetworkRequired    bool   `json:"network_required"`
	RouteDirection     string `json:"route_direction"`
	MaximumNodes       int    `json:"maximum_nodes"`
	MaximumEdges       int    `json:"maximum_edges"`
	MaximumPaths       int    `json:"maximum_paths"`
	MaximumHops        int    `json:"maximum_hops"`
	LossCostWeight     int64  `json:"loss_cost_weight"`
	AllowBidirectional bool   `json:"allow_bidirectional"`
	AlgorithmVersion   string `json:"algorithm_version"`
}

type cityPhysicalNetworkServiceReference struct {
	ID       int64
	Code     string
	Name     string
	FlowKind string
}

type cityPhysicalNetworkBaselineConnection struct {
	Code                 string
	Status               string
	MaxFlowUnits         int64
	Preference           int
	ServiceID            int64
	ServiceCode          string
	ServiceName          string
	FlowKind             string
	CapacityID           int64
	FacilityCode         string
	FacilityDistrictID   int64
	FacilityDistrictCode string
	FacilityBuildingID   int64
	FacilityBuildingCode string
	FacilityStatus       string
	DemandID             int64
	DemandCode           string
	DemandDistrictID     int64
	DemandDistrictCode   string
	DemandBuildingID     sql.NullInt64
	DemandBuildingCode   sql.NullString
	DemandStatus         string
}

func cityPhysicalNetworkPolicyCatalog(
	definitions []CityServiceDefinition,
) ([]CityPhysicalNetworkPolicy, string, error) {
	policies := make([]CityPhysicalNetworkPolicy, 0, len(definitions))
	seen := make(map[string]struct{}, len(definitions))
	for _, definition := range definitions {
		if !cityServiceCodePattern.MatchString(definition.Code) {
			return nil, "", fmt.Errorf("invalid city physical network service code %q", definition.Code)
		}
		if _, duplicate := seen[definition.Code]; duplicate {
			return nil, "", fmt.Errorf("duplicate city physical network service code %q", definition.Code)
		}
		seen[definition.Code] = struct{}{}
		direction := CityNetworkRouteSupplyToDemand
		if definition.FlowKind == "collection" {
			direction = CityNetworkRouteDemandToFacility
		} else if definition.FlowKind != "delivery" && definition.FlowKind != "capacity" {
			return nil, "", fmt.Errorf("invalid city physical network flow kind %q", definition.FlowKind)
		}
		_, required := cityPhysicalNetworkRequiredServices[definition.Code]
		seed := cityPhysicalNetworkPolicySeed{
			ServiceCode: definition.Code, NetworkRequired: required,
			RouteDirection: direction, MaximumNodes: cityPhysicalNetworkMaximumNodes,
			MaximumEdges:       cityPhysicalNetworkMaximumEdges,
			MaximumPaths:       cityPhysicalNetworkDefaultMaxPaths,
			MaximumHops:        cityPhysicalNetworkDefaultMaxHops,
			LossCostWeight:     cityPhysicalNetworkLossCostWeight,
			AllowBidirectional: required, AlgorithmVersion: cityPhysicalNetworkAlgorithmVersion,
		}
		payload, err := json.Marshal(seed)
		if err != nil {
			return nil, "", fmt.Errorf("marshal city physical network policy %s: %w", definition.Code, err)
		}
		sum := sha256.Sum256(payload)
		policies = append(policies, CityPhysicalNetworkPolicy{
			ServiceCode: definition.Code, PolicyVersion: cityPhysicalNetworkPolicyVersion,
			PolicyHash: hex.EncodeToString(sum[:]), NetworkRequired: required,
			RouteDirection: direction, MaximumNodes: seed.MaximumNodes,
			MaximumEdges: seed.MaximumEdges, MaximumPaths: seed.MaximumPaths,
			MaximumHops: seed.MaximumHops, LossCostWeight: seed.LossCostWeight,
			AllowBidirectional: seed.AllowBidirectional,
			AlgorithmVersion:   seed.AlgorithmVersion, Payload: payload,
		})
	}
	sort.Slice(policies, func(i, j int) bool { return policies[i].ServiceCode < policies[j].ServiceCode })
	canonical, err := json.Marshal(struct {
		PolicyID      string                      `json:"policy_id"`
		PolicyVersion string                      `json:"policy_version"`
		Policies      []CityPhysicalNetworkPolicy `json:"policies"`
	}{cityPhysicalNetworkPolicyID, cityPhysicalNetworkPolicyVersion, policies})
	if err != nil {
		return nil, "", fmt.Errorf("marshal city physical network policy catalog: %w", err)
	}
	sum := sha256.Sum256(canonical)
	return policies, hex.EncodeToString(sum[:]), nil
}

func initializeCityPhysicalNetworkFoundation(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if worldID <= 0 {
		return ErrCityInvalidInput
	}
	var sourceVersion string
	var baselineTick int64
	if err := tx.QueryRowContext(ctx, `
SELECT simulation_version, current_tick FROM city_worlds WHERE id = $1`, worldID).
		Scan(&sourceVersion, &baselineTick); err != nil {
		return fmt.Errorf("load city physical network baseline: %w", err)
	}
	if sourceVersion != CitySimulationVersionF8V2 &&
		!(sourceVersion == CitySimulationVersionF8V3 && baselineTick == 0) {
		return fmt.Errorf("physical network foundation requires F8.1 upgrade or direct F8.2 creation")
	}
	serviceRows, err := tx.QueryContext(ctx, `
SELECT id, code, name, flow_kind
FROM city_service_definitions WHERE world_id = $1 ORDER BY code`, worldID)
	if err != nil {
		return fmt.Errorf("load physical network service definitions: %w", err)
	}
	services := make([]cityPhysicalNetworkServiceReference, 0)
	definitions := make([]CityServiceDefinition, 0)
	for serviceRows.Next() {
		var item cityPhysicalNetworkServiceReference
		if err = serviceRows.Scan(&item.ID, &item.Code, &item.Name, &item.FlowKind); err != nil {
			_ = serviceRows.Close()
			return fmt.Errorf("scan physical network service definition: %w", err)
		}
		services = append(services, item)
		definitions = append(definitions, CityServiceDefinition{Code: item.Code, FlowKind: item.FlowKind})
	}
	if err = closeCityRows(serviceRows, "iterate physical network service definitions"); err != nil {
		return err
	}
	policies, policyHash, err := cityPhysicalNetworkPolicyCatalog(definitions)
	if err != nil {
		return err
	}
	serviceIDs := make(map[string]int64, len(services))
	for _, service := range services {
		serviceIDs[service.Code] = service.ID
	}
	if _, err = tx.ExecContext(ctx,
		`SELECT set_config('sub2api.city_physical_network_bootstrap_world_id', $1, true)`,
		fmt.Sprintf("%d", worldID)); err != nil {
		return fmt.Errorf("enable physical network bootstrap: %w", err)
	}
	metadata := json.RawMessage(`{"schema_version":1,"topology":"legacy_direct"}`)
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_physical_network_profiles
    (world_id, policy_id, policy_version, policy_hash, baseline_tick,
     policy_count, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)`, worldID,
		cityPhysicalNetworkPolicyID, cityPhysicalNetworkPolicyVersion,
		policyHash, baselineTick, len(policies), metadata); err != nil {
		return fmt.Errorf("insert physical network profile: %w", err)
	}
	for _, policy := range policies {
		serviceID, exists := serviceIDs[policy.ServiceCode]
		if !exists {
			return fmt.Errorf("physical network policy service %s is missing", policy.ServiceCode)
		}
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_physical_network_policies
    (world_id, service_definition_id, policy_version, policy_hash,
     network_required, route_direction, maximum_nodes, maximum_edges,
     maximum_paths, maximum_hops, loss_cost_weight, allow_bidirectional,
     algorithm_version, payload)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14::jsonb)`,
			worldID, serviceID, policy.PolicyVersion, policy.PolicyHash,
			policy.NetworkRequired, policy.RouteDirection, policy.MaximumNodes,
			policy.MaximumEdges, policy.MaximumPaths, policy.MaximumHops,
			policy.LossCostWeight, policy.AllowBidirectional,
			policy.AlgorithmVersion, policy.Payload); err != nil {
			return fmt.Errorf("insert physical network policy %s: %w", policy.ServiceCode, err)
		}
	}
	connections, err := loadCityPhysicalNetworkBaselineConnections(ctx, tx, worldID)
	if err != nil {
		return err
	}
	counts, err := insertCityPhysicalNetworkBaseline(ctx, tx, worldID, baselineTick, connections)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `
UPDATE city_physical_network_profiles
SET network_count = $2, node_count = $3, edge_count = $4, updated_at = NOW()
WHERE world_id = $1`, worldID, counts.networks, counts.nodes, counts.edges); err != nil {
		return fmt.Errorf("finalize physical network baseline counts: %w", err)
	}
	return nil
}

func loadCityPhysicalNetworkBaselineConnections(
	ctx context.Context, queryer citySQLQueryer, worldID int64,
) ([]cityPhysicalNetworkBaselineConnection, error) {
	return loadCityPhysicalNetworkManagedConnections(ctx, queryer, worldID, false)
}

func loadCityPhysicalNetworkManagedConnections(
	ctx context.Context, queryer citySQLQueryer, worldID int64, includeRetired bool,
) ([]cityPhysicalNetworkBaselineConnection, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT connection.code, connection.status, connection.max_flow_units_per_tick,
       connection.preference, service.id, service.code, service.name, service.flow_kind,
       capacity.id, facility.code, facility.district_id, facility_district.code,
       facility.building_id, facility_building.code, facility.status,
       demand.id, demand.code, demand.district_id, demand_district.code,
       demand.building_id, demand_building.code, demand.status
FROM city_service_connections connection
JOIN city_facility_service_capacities capacity ON capacity.id = connection.capacity_id
JOIN city_facilities facility ON facility.id = capacity.facility_id
JOIN city_service_definitions service ON service.id = capacity.service_definition_id
JOIN city_service_demands demand ON demand.id = connection.demand_id
JOIN city_districts facility_district ON facility_district.id = facility.district_id
JOIN city_buildings facility_building ON facility_building.id = facility.building_id
JOIN city_districts demand_district ON demand_district.id = demand.district_id
LEFT JOIN city_buildings demand_building ON demand_building.id = demand.building_id
WHERE connection.world_id = $1
  AND service.code IN ('electric_power', 'potable_water', 'wastewater', 'solid_waste')
ORDER BY service.code, connection.code`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load legacy physical network connections: %w", err)
	}
	items := make([]cityPhysicalNetworkBaselineConnection, 0)
	for rows.Next() {
		var item cityPhysicalNetworkBaselineConnection
		if err = rows.Scan(
			&item.Code, &item.Status, &item.MaxFlowUnits, &item.Preference,
			&item.ServiceID, &item.ServiceCode, &item.ServiceName, &item.FlowKind,
			&item.CapacityID, &item.FacilityCode, &item.FacilityDistrictID,
			&item.FacilityDistrictCode, &item.FacilityBuildingID,
			&item.FacilityBuildingCode, &item.FacilityStatus, &item.DemandID,
			&item.DemandCode, &item.DemandDistrictID, &item.DemandDistrictCode,
			&item.DemandBuildingID, &item.DemandBuildingCode, &item.DemandStatus,
		); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan legacy physical network connection: %w", err)
		}
		if includeRetired || item.Status != CityServiceProjectionStatusRetired {
			items = append(items, item)
		}
	}
	if err = closeCityRows(rows, "iterate legacy physical network connections"); err != nil {
		return nil, err
	}
	return items, nil
}

type cityPhysicalNetworkBaselineCounts struct {
	networks int64
	nodes    int64
	edges    int64
}

func insertCityPhysicalNetworkBaseline(
	ctx context.Context,
	tx *sql.Tx,
	worldID, baselineTick int64,
	connections []cityPhysicalNetworkBaselineConnection,
) (cityPhysicalNetworkBaselineCounts, error) {
	var counts cityPhysicalNetworkBaselineCounts
	networkIDs := make(map[string]int64)
	supplyNodeIDs := make(map[int64]int64)
	demandNodeIDs := make(map[int64]int64)
	for _, connection := range connections {
		networkID, exists := networkIDs[connection.ServiceCode]
		if !exists {
			code, codeErr := cityPhysicalNetworkBaselineCode("network", connection.ServiceCode)
			if codeErr != nil {
				return counts, codeErr
			}
			networkMetadata, marshalErr := json.Marshal(map[string]any{
				"baseline_mode": "legacy_direct", "schema_version": 1,
			})
			if marshalErr != nil {
				return counts, marshalErr
			}
			if err := tx.QueryRowContext(ctx, `
INSERT INTO city_physical_networks
    (world_id, code, name, service_definition_id, status, topology_revision,
     created_tick, updated_tick, version, metadata)
VALUES ($1, $2, $3, $4, 'active', 1, $5, $5, 1, $6::jsonb)
RETURNING id`, worldID, code, connection.ServiceName+" network",
				connection.ServiceID, baselineTick, networkMetadata).Scan(&networkID); err != nil {
				return counts, fmt.Errorf("insert baseline physical network %s: %w", connection.ServiceCode, err)
			}
			networkIDs[connection.ServiceCode] = networkID
			counts.networks++
		}
		supplyNodeID, exists := supplyNodeIDs[connection.CapacityID]
		if !exists {
			code, codeErr := cityPhysicalNetworkBaselineCode(
				"supply", connection.ServiceCode, connection.FacilityCode,
			)
			if codeErr != nil {
				return counts, codeErr
			}
			status := CityNetworkNodeStatusActive
			if connection.FacilityStatus == CityFacilityStatusRetired {
				status = CityNetworkNodeStatusRetired
			}
			nodeMetadata, marshalErr := json.Marshal(map[string]any{
				"baseline_mode": "legacy_direct", "capacity_id": connection.CapacityID,
			})
			if marshalErr != nil {
				return counts, marshalErr
			}
			if err := tx.QueryRowContext(ctx, `
INSERT INTO city_physical_network_nodes
    (world_id, network_id, code, role, capacity_id, district_id, building_id,
     status, created_tick, updated_tick, version, metadata)
VALUES ($1, $2, $3, 'supply', $4, $5, $6, $7, $8, $8, 1, $9::jsonb)
RETURNING id`, worldID, networkID, code, connection.CapacityID,
				connection.FacilityDistrictID, connection.FacilityBuildingID,
				status, baselineTick, nodeMetadata).Scan(&supplyNodeID); err != nil {
				return counts, fmt.Errorf("insert baseline supply node %s: %w", code, err)
			}
			supplyNodeIDs[connection.CapacityID] = supplyNodeID
			counts.nodes++
		}
		demandNodeID, exists := demandNodeIDs[connection.DemandID]
		if !exists {
			code, codeErr := cityPhysicalNetworkBaselineCode(
				"demand", connection.ServiceCode, connection.DemandCode,
			)
			if codeErr != nil {
				return counts, codeErr
			}
			status := CityNetworkNodeStatusActive
			if connection.DemandStatus == CityServiceProjectionStatusSuspended {
				status = CityNetworkNodeStatusOffline
			} else if connection.DemandStatus == CityServiceProjectionStatusRetired {
				status = CityNetworkNodeStatusRetired
			}
			nodeMetadata, marshalErr := json.Marshal(map[string]any{
				"baseline_mode": "legacy_direct", "demand_id": connection.DemandID,
			})
			if marshalErr != nil {
				return counts, marshalErr
			}
			if err := tx.QueryRowContext(ctx, `
INSERT INTO city_physical_network_nodes
    (world_id, network_id, code, role, demand_id, district_id, building_id,
     status, created_tick, updated_tick, version, metadata)
VALUES ($1, $2, $3, 'demand', $4, $5, $6, $7, $8, $8, 1, $9::jsonb)
RETURNING id`, worldID, networkID, code, connection.DemandID,
				connection.DemandDistrictID, connection.DemandBuildingID,
				status, baselineTick, nodeMetadata).Scan(&demandNodeID); err != nil {
				return counts, fmt.Errorf("insert baseline demand node %s: %w", code, err)
			}
			demandNodeIDs[connection.DemandID] = demandNodeID
			counts.nodes++
		}
		fromNodeID, toNodeID := supplyNodeID, demandNodeID
		if connection.FlowKind == "collection" {
			fromNodeID, toNodeID = demandNodeID, supplyNodeID
		}
		edgeStatus := CityNetworkEdgeStatusIsolated
		if connection.Status == CityServiceProjectionStatusActive &&
			connection.FacilityStatus != CityFacilityStatusRetired &&
			connection.DemandStatus == CityServiceProjectionStatusActive {
			edgeStatus = CityNetworkEdgeStatusActive
		}
		code, codeErr := cityPhysicalNetworkBaselineCode("edge", connection.ServiceCode, connection.Code)
		if codeErr != nil {
			return counts, codeErr
		}
		edgeMetadata, marshalErr := json.Marshal(map[string]any{
			"baseline_mode": "legacy_direct", "connection_code": connection.Code,
		})
		if marshalErr != nil {
			return counts, marshalErr
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_physical_network_edges
    (world_id, network_id, code, from_node_id, to_node_id, direction,
     installed_capacity_units, availability_milli, available_capacity_units,
     loss_milli, base_cost_units, status, condition_milli, created_tick,
     updated_tick, version, metadata)
VALUES ($1, $2, $3, $4, $5, 'directed', $6, 1000, $6, 0, $7, $8,
        1000, $9, $9, 1, $10::jsonb)`, worldID, networkID, code,
			fromNodeID, toNodeID, connection.MaxFlowUnits,
			int64(1001-connection.Preference), edgeStatus, baselineTick,
			edgeMetadata); err != nil {
			return counts, fmt.Errorf("insert baseline physical edge %s: %w", code, err)
		}
		counts.edges++
	}
	return counts, nil
}

func cityPhysicalNetworkBaselineCode(prefix string, components ...string) (string, error) {
	parts := append([]string{prefix}, components...)
	candidate := strings.Join(parts, ".")
	if len(candidate) <= 96 && cityServiceCodePattern.MatchString(candidate) {
		return candidate, nil
	}
	sum := sha256.Sum256([]byte(candidate))
	suffix := hex.EncodeToString(sum[:6])
	maximumHead := 96 - len(suffix) - 1
	if maximumHead <= 1 {
		return "", fmt.Errorf("invalid physical network code prefix %q", prefix)
	}
	head := strings.TrimRight(candidate[:maximumHead], ".-_")
	candidate = head + "." + suffix
	if !cityServiceCodePattern.MatchString(candidate) {
		return "", fmt.Errorf("cannot derive physical network code from %q", strings.Join(parts, "/"))
	}
	return candidate, nil
}
