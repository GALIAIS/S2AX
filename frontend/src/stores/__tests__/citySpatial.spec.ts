import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import type {
  CityDevelopmentState,
  CityEnterpriseLocationState,
  CityLandState,
  CityMapChunk,
  CityMapChunkSummary,
  CityMember,
  CityNavigationPath,
  CityOvermapState,
  CityPhysicalNetworkCatalogView,
  CityPhysicalNetworkDiagnosticsView,
  CityPhysicalNetworkEdgePage,
  CityPhysicalNetworkFactPage,
  CityPhysicalNetworkFlowPage,
  CityPhysicalNetworkNodePage,
  CityPhysicalNetworkPage,
  CityServiceCatalogView,
  CityServiceConnectionPage,
  CityServiceDemandPage,
  CityServiceFacilityPage,
  CityServiceSettlementPage,
  CitySpatialMutationPage,
  CityWorld,
  CityWorldSpatialRuleSet,
  WorldActor,
  WorldActorNavigationIntent,
  WorldActorRoleOption,
  WorldActorState,
  WorldNavigationReservation,
  WorldPortalAccessView,
  WorldRuntimeCatalog,
  WorldRuntimeDefinition
} from '@/api/citySpatial'

const apiMocks = vi.hoisted(() => ({
  listWorlds: vi.fn(),
  getWorld: vi.fn(),
  createWorld: vi.fn(),
  listWorldMembers: vi.fn(),
  addWorldMember: vi.fn(),
  updateWorldMember: vi.fn(),
  getWorldSpatialRuleSet: vi.fn(),
  getOvermap: vi.fn(),
  getLandState: vi.fn(),
  getDevelopmentState: vi.fn(),
  getEnterpriseLocationState: vi.fn(),
  getServiceCatalog: vi.fn(),
  listServiceFacilities: vi.fn(),
  listServiceDemands: vi.fn(),
  listServiceConnections: vi.fn(),
  listServiceSettlements: vi.fn(),
  getPhysicalNetworkCatalog: vi.fn(),
  listPhysicalNetworks: vi.fn(),
  listPhysicalNetworkNodes: vi.fn(),
  listPhysicalNetworkEdges: vi.fn(),
  listPhysicalNetworkFlows: vi.fn(),
  listPhysicalNetworkFacts: vi.fn(),
  getPhysicalNetworkDiagnostics: vi.fn(),
  getWorldRuntimeCatalog: vi.fn(),
  listWorldActors: vi.fn(),
  getWorldActorState: vi.fn(),
  findWorldActorPath: vi.fn(),
  listWorldPortalStates: vi.fn(),
  listWorldNavigationIntents: vi.fn(),
  getWorldNavigationIntent: vi.fn(),
  listWorldNavigationReservations: vi.fn(),
  getWorldActorRoleOptions: vi.fn(),
  listWorldRuntimeRules: vi.fn(),
  listWorldRuleCases: vi.fn(),
  listMapChunks: vi.fn(),
  getMapChunk: vi.fn(),
  listSpatialChanges: vi.fn(),
  submitGenerateChunk: vi.fn(),
  submitDevelopmentCommand: vi.fn(),
  submitEnterpriseLocationCommand: vi.fn(),
  submitServiceCommand: vi.fn(),
  submitWorldRuntimeCommand: vi.fn(),
  submitWorldControlCommand: vi.fn(),
  getCommand: vi.fn(),
  listCommands: vi.fn(),
  stepWorld: vi.fn()
}))

vi.mock('@/api/citySpatial', () => ({ default: apiMocks }))

import { useCitySpatialStore } from '../citySpatial'

const world: CityWorld = {
  id: 7,
  name: 'Harbor City',
  owner_user_id: 1,
  status: 'paused',
  simulation_version: 'city-f7-v3',
  current_tick: 0,
  speed_multiplier: 1,
  timezone: 'Asia/Shanghai',
  settings: {},
  member_role: 'owner',
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z'
}

const worldMembers: CityMember[] = [
  {
    user_id: 1, email: 'owner@example.com', username: 'owner', role: 'owner', status: 'active',
    joined_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z'
  },
  {
    user_id: 2, email: 'delegate@example.com', username: 'delegate', role: 'planner', status: 'active',
    joined_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z'
  }
]

const ruleBundle: CityWorldSpatialRuleSet = {
  profile: {
    world_id: 7,
    rule_set_id: 'test-classic',
    rule_set_version: '1.0.0',
    rule_set_hash: 'a'.repeat(64),
    chunk_size: 32,
    minimum_z: -2,
    maximum_z: 2,
    generator_id: 'test-mapgen',
    generator_version: '1.0.0',
    minimum_chunk_x: 0,
    maximum_chunk_x: 0,
    minimum_chunk_y: 0,
    maximum_chunk_y: 0,
    overmap_seed_proof: 'proof',
    overmap_root_hash: 'root',
    overmap_revision: 1,
    metadata: {},
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z'
  },
  rule_set: {
    id: 'test-classic',
    version: '1.0.0',
    name: 'Test Classic',
    chunk_size: 32,
    min_z: -2,
    max_z: 2,
    content_hash: 'a'.repeat(64),
    palette: [
      { id: 'ground', name: 'Ground', classic_foreground: 244, classic_background: 234 },
      { id: 'danger', name: 'Danger', classic_foreground: 203, classic_background: 52 }
    ],
    definitions: [
      { id: 'missing.terrain', kind: 'terrain', name: 'Unknown', glyph: '?', foreground: 'danger', movement_cost: 0, flags: [], metadata: {} },
      { id: 'missing.furniture', kind: 'furniture', name: 'Unknown', glyph: '?', foreground: 'danger', movement_cost: 0, flags: [], metadata: {} },
      { id: 'terrain.ground', kind: 'terrain', name: 'Ground', glyph: '.', foreground: 'ground', movement_cost: 100, flags: ['passable'], metadata: {} }
    ]
  }
}

const overmap: CityOvermapState = {
  profile: ruleBundle.profile,
  tiles: [{
    chunk_x: 0,
    chunk_y: 0,
    z: 0,
    district_code: 'central',
    terrain_definition_id: 'terrain.ground',
    road_mask: 0,
    river_mask: 0,
    variant: 0,
    tile_hash: 'tile',
    metadata: {}
  }]
}

const summary: CityMapChunkSummary = {
  chunk_x: 0,
  chunk_y: 0,
  z: 0,
  district_code: 'central',
  generator_id: 'test-mapgen',
  generator_version: '1.0.0',
  generation_proof: 'proof',
  revision: 1,
  payload_hash: 'b'.repeat(64),
  generated_tick: 1
}

const chunk: CityMapChunk = {
  ...summary,
  world_id: 7,
  rule_set_hash: ruleBundle.rule_set.content_hash,
  payload: {
    format: 'city-chunk-v1',
    width: 32,
    height: 32,
    terrain_runs: [{ definition_id: 'terrain.ground', length: 1024 }],
    furniture: []
  },
  metadata: {},
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z'
}

const emptyChanges: CitySpatialMutationPage = { items: [] }

const landState: CityLandState = {
  profile: {
    rule_set_id: 'sub2api-land', rule_set_version: '1.0.0', rule_set_hash: 'c'.repeat(64),
    spatial_overmap_root_hash: ruleBundle.profile.overmap_root_hash,
    nominal_cell_area_sqm: 1500, baseline_hash: 'd'.repeat(64), baseline_tick: 0,
    zoning_rule_count: 3, parcel_count: 1, building_count: 1, unit_pool_count: 1,
    housing_allocation_count: 0, portal_count: 1, revision: 1
  },
  zoning_rules: [],
  parcels: [{
    code: 'parcel_central', district_code: 'central', zone_code: 'commercial',
    geometry: { chunk_x: 0, chunk_y: 0, z: 0, local_min_x: 1, local_min_y: 1, local_max_x: 12, local_max_y: 12 },
    area_sqm: 10000, developable_area_sqm: 10000, status: 'active', version: 1
  }],
  buildings: [{
    code: 'building_central', parcel_code: 'parcel_central', district_code: 'central', primary_use: 'commercial',
    footprint: { chunk_x: 0, chunk_y: 0, z: 0, local_min_x: 4, local_min_y: 4, local_max_x: 9, local_max_y: 9 },
    base_z: 0, top_z: 1, floor_count: 2, footprint_area_sqm: 4500, floor_area_sqm: 9000,
    capacity_units: 100, occupied_units: 0, quality_milli: 1000,
    status: 'active', completed_tick: 0, version: 1
  }],
  unit_pools: [{
    code: 'pool_building_central', building_code: 'building_central', district_code: 'central',
    use_type: 'commercial', unit_count: 100, occupied_unit_count: 0,
    capacity_units_per_unit: 1, version: 1
  }],
  housing_allocations: [],
  portals: [{
    code: 'entrance', building_code: 'building_central', district_code: 'central', portal_type: 'entrance',
    from_x: 3, from_y: 6, from_z: 0, to_x: 4, to_y: 6, to_z: 0,
    bidirectional: true, status: 'active', version: 1
  }]
}

const developmentState: CityDevelopmentState = {
  profile: {
    policy_id: 'sub2api-development', policy_version: '1.0.0', policy_hash: 'e'.repeat(64),
    baseline_tick: 0, baseline_hash: 'f'.repeat(64), project_count: 0,
    fact_count: 0, adjustment_count: 0, revision: 0
  },
  projects: [], facts: [], adjustments: [], developers: []
}

const enterpriseLocationState: CityEnterpriseLocationState = {
  profile: {
    policy_id: 'sub2api-enterprise-location', policy_version: '1.0.0', policy_hash: '1'.repeat(64),
    baseline_tick: 0, baseline_hash: '2'.repeat(64), baseline_site_count: 2,
    site_count: 2, fact_count: 0, revision: 1
  },
  baseline_sites: [],
  sites: [{
    code: 'site_firm_central_headquarters', firm_entity_code: 'firm_central',
    district_code: 'central', building_code: 'building_central', pool_code: 'pool_building_central',
    site_type: 'headquarters', name: 'Central Works Headquarters', occupied_units: 5,
    is_primary: true, status: 'active', opened_tick: 0, last_changed_tick: 0,
    version: 1, metadata: {}
  }],
  facts: [],
  firms: [{
    entity_id: 42, entity_code: 'firm_central', entity_name: 'Central Works', district_code: 'central',
    employee_units: 20, capital_stock_units: 30, production_capacity_units: 10, active_site_count: 2
  }],
  pools: [{
    code: 'pool_building_central', building_code: 'building_central', district_code: 'central',
    use_type: 'commercial', effective_unit_count: 100, occupied_unit_count: 5, available_unit_count: 95
  }]
}

const serviceCatalog: CityServiceCatalogView = {
  availability: 'available', simulation_version: 'city-f8-v1', required_version: 'city-f8-v1',
  profile: {
    catalog_id: 'sub2api-public-services', catalog_version: '1.0.0', catalog_hash: '8'.repeat(64),
    settlement_version: '1.0.0', baseline_tick: 0, service_definition_count: 1,
    facility_type_count: 1, facility_count: 1, capacity_count: 1, demand_count: 1,
    connection_count: 1, fact_count: 4, allocation_count: 1, settlement_count: 1,
    revision: 4, metadata: {}
  },
  overview: {
    facility_count: 1, operational_facility_count: 1, active_capacity_count: 1,
    dispatch_capacity_units: '950', active_demand_count: 1, requested_units_per_tick: '800',
    latest_settlement_tick: 4, latest_requested_units: '800', latest_delivered_units: '784',
    latest_shortage_units: '16', latest_weighted_quality_milli: 980
  },
  service_definitions: [{
    code: 'electric_power', definition_version: '1.0.0', definition_hash: '9'.repeat(64),
    name: 'Electric power', category: 'utility', unit_code: 'energy_unit', flow_kind: 'delivery',
    status: 'active', sort_order: 10, payload: {}
  }],
  facility_types: [{
    code: 'power_plant', definition_version: '1.0.0', definition_hash: 'a'.repeat(64),
    name: 'Power plant', minimum_floor_area_sqm: 1, default_reliability_milli: 950,
    allowed_service_codes: ['electric_power'], status: 'active', sort_order: 20, payload: {}
  }]
}

const serviceFacilities: CityServiceFacilityPage = {
  availability: 'available', simulation_version: 'city-f8-v1', required_version: 'city-f8-v1',
  items: [{
    facility: {
      code: 'facility_central_power', name: 'Central Power', facility_type_code: 'power_plant',
      facility_type_version: '1.0.0', facility_type_hash: 'a'.repeat(64), district_code: 'central',
      building_code: 'building_central', status: 'operational', reliability_milli: 950,
      created_tick: 1, updated_tick: 3, version: 2, source_fact_tick: 3,
      source_fact_sequence: 1, metadata: {}
    },
    capacities: [{
      facility_code: 'facility_central_power', service_code: 'electric_power', service_version: '1.0.0',
      service_hash: '9'.repeat(64), installed_capacity_units: 1000, availability_milli: 950,
      available_capacity_units: 950, dispatch_capacity_units: 950, updated_tick: 2,
      version: 1, source_fact_tick: 2, source_fact_sequence: 1, metadata: {}
    }]
  }]
}

const serviceDemands: CityServiceDemandPage = {
  availability: 'available', simulation_version: 'city-f8-v1', required_version: 'city-f8-v1',
  items: [{
    demand: {
      code: 'demand_central_power', service_code: 'electric_power', service_version: '1.0.0',
      service_hash: '9'.repeat(64), subject_kind: 'district', subject_code: 'central',
      district_code: 'central', requested_units_per_tick: 800, priority: 800, status: 'active',
      created_tick: 3, updated_tick: 3, version: 1, source_fact_tick: 3,
      source_fact_sequence: 2, metadata: {}
    }
  }]
}

const serviceConnections: CityServiceConnectionPage = {
  availability: 'available', simulation_version: 'city-f8-v1', required_version: 'city-f8-v1',
  items: [{
    code: 'connection_central_power', facility_code: 'facility_central_power', service_code: 'electric_power',
    demand_code: 'demand_central_power', max_flow_units_per_tick: 800, loss_milli: 20,
    preference: 900, status: 'active', created_tick: 4, updated_tick: 4, version: 1,
    source_fact_tick: 4, source_fact_sequence: 1, metadata: {}
  }]
}

const serviceSettlements: CityServiceSettlementPage = {
  availability: 'available', simulation_version: 'city-f8-v1', required_version: 'city-f8-v1',
  items: [{
    settlement: {
      tick: 4, sequence: 2, service_code: 'electric_power', demand_code: 'demand_central_power',
      demand_version: 1, requested_units: 800, delivered_units: 784, shortage_units: 16,
      allocation_count: 1, quality_milli: 980, metadata: {}
    },
    allocations: []
  }]
}

const physicalNetworkCatalog: CityPhysicalNetworkCatalogView = {
  availability: 'available', simulation_version: 'city-f8-v3', required_version: 'city-f8-v3',
  profile: {
    policy_id: 'sub2api-physical-networks', policy_version: '1.0.0', policy_hash: 'b'.repeat(64),
    baseline_tick: 0, policy_count: 1, network_count: 1, node_count: 2, edge_count: 1,
    fact_count: 3, batch_count: 1, path_count: 1, segment_count: 1, revision: 3, metadata: {}
  },
  overview: {
    active_network_count: 1, active_node_count: 2, active_edge_count: 1,
    isolated_edge_count: 0, failed_edge_count: 0,
    installed_edge_capacity_units: '900', available_edge_capacity_units: '855', latest_flow_tick: 4,
    latest_dispatched_units: '800', latest_network_received_units: '784', latest_network_loss_units: '16',
    latest_delivery_ratio_milli: 980
  },
  policies: [{
    service_code: 'electric_power', policy_version: '1.0.0', policy_hash: 'c'.repeat(64),
    network_required: true, route_direction: 'supply_to_demand', maximum_nodes: 100,
    maximum_edges: 200, maximum_paths: 8, maximum_hops: 32, loss_cost_weight: 5,
    allow_bidirectional: true, algorithm_version: 'integer-dijkstra-v1', payload: {}
  }]
}

const physicalNetworks: CityPhysicalNetworkPage = {
  availability: 'available', simulation_version: 'city-f8-v3', required_version: 'city-f8-v3',
  items: [{
    code: 'grid_central', name: 'Central Grid', service_code: 'electric_power', status: 'active',
    topology_revision: 3, created_tick: 1, updated_tick: 3, version: 2,
    source_fact_tick: 3, source_fact_sequence: 1, metadata: { baseline_mode: 'explicit' }
  }]
}

const physicalNetworkNodes: CityPhysicalNetworkNodePage = {
  availability: 'available', simulation_version: 'city-f8-v3', required_version: 'city-f8-v3',
  items: [{
    code: 'supply_power', network_code: 'grid_central', role: 'supply',
    capacity_code: 'facility_central_power.electric_power', district_code: 'central',
    building_code: 'building_central', world_x: 2, world_y: 4, world_z: 0, status: 'active',
    created_tick: 1, updated_tick: 2, version: 1, source_fact_tick: 2, source_fact_sequence: 1, metadata: {}
  }, {
    code: 'demand_power', network_code: 'grid_central', role: 'demand',
    demand_code: 'demand_central_power', district_code: 'central', world_x: 14, world_y: 10,
    world_z: 0, status: 'active', created_tick: 1, updated_tick: 2, version: 1,
    source_fact_tick: 2, source_fact_sequence: 2, metadata: {}
  }]
}

const physicalNetworkEdges: CityPhysicalNetworkEdgePage = {
  availability: 'available', simulation_version: 'city-f8-v3', required_version: 'city-f8-v3',
  items: [{
    code: 'line_power', network_code: 'grid_central', from_node_code: 'supply_power',
    to_node_code: 'demand_power', direction: 'directed', installed_capacity_units: 900,
    availability_milli: 950, available_capacity_units: 855, loss_milli: 20, base_cost_units: 1,
    status: 'active', condition_milli: 1000, failure_count: 0, created_tick: 1,
    updated_tick: 3, version: 2, source_fact_tick: 3, source_fact_sequence: 2, metadata: {}
  }]
}

const physicalNetworkFlows: CityPhysicalNetworkFlowPage = {
  availability: 'available', simulation_version: 'city-f8-v3', required_version: 'city-f8-v3',
  items: [{
    batch: {
      tick: 4, sequence: 3, network_code: 'grid_central', service_code: 'electric_power',
      topology_revision: 3, allocation_count: 1, path_count: 1, segment_count: 1,
      dispatched_units: 800, network_received_units: 784, network_loss_units: 16,
      source_fact_tick: 4, source_fact_sequence: 3, metadata: {}
    },
    paths: [{
      tick: 4, sequence: 3, service_sequence: 2, allocation_index: 1, path_index: 1,
      network_code: 'grid_central', connection_code: 'connection_central_power',
      source_node_code: 'supply_power', sink_node_code: 'demand_power', hop_count: 1,
      dispatched_units: 800, network_received_units: 784, network_loss_units: 16,
      path_cost_units: 101, path_hash: 'd'.repeat(64), metadata: {}
    }],
    segments: [{
      tick: 4, sequence: 3, service_sequence: 2, allocation_index: 1, path_index: 1,
      segment_index: 1, edge_code: 'line_power', edge_version: 2, direction: 'forward',
      from_node_code: 'supply_power', to_node_code: 'demand_power', edge_capacity_units: 855,
      loss_milli: 20, input_units: 800, output_units: 784, loss_units: 16, metadata: {}
    }]
  }]
}

const physicalNetworkFacts: CityPhysicalNetworkFactPage = {
  availability: 'available', simulation_version: 'city-f8-v3', required_version: 'city-f8-v3',
  items: [{
    tick: 4, sequence: 3, phase: 'settlement', fact_type: 'network.flow_settled',
    subject_kind: 'flow_batch', subject_code: 'grid_central', version_before: 0,
    version_after: 1, payload: { schema_version: 1 }
  }]
}

const physicalNetworkDiagnostics: CityPhysicalNetworkDiagnosticsView = {
  availability: 'available', simulation_version: 'city-f8-v3', required_version: 'city-f8-v3',
  network: physicalNetworks.items[0], policy: physicalNetworkCatalog.policies[0], latest_flow_tick: 4,
  active_node_count: 2, active_edge_count: 1, component_count: 1, isolated_node_count: 0,
  service_island_count: 0, bottleneck_edge_count: 1, saturated_edge_count: 0,
  components: [{
    index: 1, node_count: 2, edge_count: 1, supply_node_count: 1, demand_node_count: 1,
    node_codes: ['demand_power', 'supply_power'], service_island: false
  }],
  edge_diagnostics: [{
    edge_code: 'line_power', status: 'active', available_capacity_units: 855,
    latest_input_units: 800, latest_output_units: 784, latest_loss_units: 16,
    utilization_milli: 935, saturated: false, bottleneck: true
  }],
  truncated_edge_diagnostic_count: 0
}

const runtimeDefinitions: WorldRuntimeDefinition[] = [
  {
    kind: 'attribute', code: 'reasoning', version: '1.0.0', hash: '3'.repeat(64), visibility: 'public',
    payload: { minimum_units: 0, maximum_units: 100000 }
  },
  {
    kind: 'archetype', code: 'urban_apprentice', version: '1.0.0', hash: '4'.repeat(64), visibility: 'public',
    payload: { initial_attributes: { reasoning: 16000 } }
  },
  {
    kind: 'activity', code: 'technical_study', version: '1.0.0', hash: '5'.repeat(64), visibility: 'public',
    payload: { effects: [{ type: 'attribute.add', key: 'reasoning', value_units: 2500 }] }
  },
  {
    kind: 'role', code: 'profession.technician', version: '1.0.0', hash: '6'.repeat(64), visibility: 'public',
    payload: { category_code: 'profession' }
  }
]

const runtimeCatalog: WorldRuntimeCatalog = {
  profile: {
    runtime_id: 'sub2api-open-world', runtime_version: '1.0.0', catalog_version: '1.0.0',
    catalog_hash: '7'.repeat(64), baseline_tick: 0, maximum_player_actors_per_member: 3,
    actor_count: 1, fact_count: 2, effect_count: 2, case_count: 0, revision: 2, metadata: {}
  },
  definitions: runtimeDefinitions
}

const runtimeActor: WorldActor = {
  code: 'actor_00000001', owner_user_id: 1, actor_type_code: 'character', name: 'Aster',
  status: 'active', archetype_code: 'urban_apprentice', archetype_version: '1.0.0',
  created_tick: 1, updated_tick: 2, version: 2, metadata: {},
  location: {
    actor_code: 'actor_00000001', space_kind: 'world', space_code: 'world',
    x: 6, y: 7, z: 0, chunk_x: 0, chunk_y: 0, local_x: 6, local_y: 7,
    jurisdiction_code: 'central', moved_tick: 1, version: 1, metadata: {}
  }
}

const runtimePortal: WorldPortalAccessView = {
  state: {
    building_code: 'building_central', portal_code: 'entrance_main', portal_type: 'entrance',
    state_code: 'open', access_requirement: { op: 'role_active', role_code: 'profession.technician' },
    access_policy_hash: '8'.repeat(64), changed_tick: 0, version: 1, metadata: {}
  },
  from: { x: 6, y: 7, z: 0 },
  to: { x: 7, y: 7, z: 0 },
  bidirectional: true,
  accessible: false,
  access_evaluation: {
    satisfied: false,
    failures: [{
      path: 'requirements', operator: 'role_active', code: 'profession.technician',
      message_code: 'requirement.role_active'
    }]
  }
}

const runtimeNavigationIntent: WorldActorNavigationIntent = {
  actor_code: runtimeActor.code,
  intent_code: 'navigation_intent_00000000000000000023',
  destination: { x: 9, y: 7, z: 0 },
  status: 'active',
  on_blocked: 'retry',
  priority: 1,
  max_steps: 64,
  budget_units: 80,
  budget_gain_units: 100,
  budget_cap_units: 400,
  blocked_attempts: 0,
  next_attempt_tick: 2,
  created_tick: 1,
  updated_tick: 1,
  source_fact: { tick: 1, sequence: 4 },
  version: 1,
  metadata: { schema_version: 1 }
}

const runtimeNavigationReservation: WorldNavigationReservation = {
  tick: 2,
  sequence: 1,
  actor_code: runtimeActor.code,
  intent_code: runtimeNavigationIntent.intent_code,
  from: { x: 6, y: 7, z: 0 },
  to: { x: 7, y: 7, z: 0 },
  target_key: '7:7:0',
  edge_key: '6:7:0|7:7:0',
  step_cost: 80,
  source_fact: { tick: 2, sequence: 3 },
  status: 'consumed',
  metadata: { schema_version: 1 }
}

function runtimeActorState(reasoningUnits: number, tick: number): WorldActorState {
  return {
    actor: { ...runtimeActor, updated_tick: tick, version: tick },
    attributes: [{
      actor_code: runtimeActor.code, attribute_code: 'reasoning', value_units: reasoningUnits,
      experience_units: Math.max(0, reasoningUnits - 16000), last_changed_tick: tick, version: tick, metadata: {}
    }],
    roles: [], statuses: [], recent_facts: [], location: runtimeActor.location,
    control_grants: [], capabilities: ['actor.command', 'actor.control.manage']
  }
}

const runtimeRoleOptions: WorldActorRoleOption[] = [{
  definition: runtimeDefinitions[3]!, active: false, eligible: false,
  current_category_role: 'profession.apprentice', cooldown_remaining_ticks: 0,
  blocked_reason_codes: ['requirement.attribute_min'],
  evaluation: {
    satisfied: false,
    failures: [{
      path: 'requirements.0', operator: 'attribute_min', code: 'reasoning',
      actual_units: 16000, required_units: 20000, message_code: 'requirement.attribute_min'
    }]
  }
}]

function configureReadAPI(chunks: CityMapChunkSummary[] = [summary]): void {
  apiMocks.listWorlds.mockResolvedValue([world])
  apiMocks.getWorld.mockResolvedValue(world)
  apiMocks.listWorldMembers.mockResolvedValue(worldMembers)
  apiMocks.listCommands.mockResolvedValue({ items: [] })
  apiMocks.getWorldSpatialRuleSet.mockResolvedValue(ruleBundle)
  apiMocks.getOvermap.mockResolvedValue(overmap)
  apiMocks.getLandState.mockResolvedValue(landState)
  apiMocks.getDevelopmentState.mockResolvedValue(developmentState)
  apiMocks.getEnterpriseLocationState.mockResolvedValue(enterpriseLocationState)
  apiMocks.getServiceCatalog.mockResolvedValue({
    availability: 'unsupported', simulation_version: world.simulation_version,
    required_version: 'city-f8-v1', service_definitions: [], facility_types: []
  })
  apiMocks.getPhysicalNetworkCatalog.mockResolvedValue({
    availability: 'unsupported', simulation_version: world.simulation_version,
    required_version: 'city-f8-v3', policies: []
  })
  apiMocks.getWorldRuntimeCatalog.mockRejectedValue({
    status: 404,
    code: 'WORLD_RUNTIME_STATE_NOT_FOUND',
    message: 'world runtime state not found'
  })
  apiMocks.listWorldActors.mockResolvedValue([])
  apiMocks.listWorldRuntimeRules.mockResolvedValue([])
  apiMocks.listWorldRuleCases.mockResolvedValue({ items: [] })
  apiMocks.listWorldPortalStates.mockResolvedValue([])
  apiMocks.listWorldNavigationIntents.mockRejectedValue({
    status: 404,
    code: 'WORLD_NAVIGATION_INTENT_UNAVAILABLE',
    message: 'world navigation intents are unavailable'
  })
  apiMocks.listWorldNavigationReservations.mockRejectedValue({
    status: 404,
    code: 'WORLD_NAVIGATION_INTENT_UNAVAILABLE',
    message: 'world navigation intents are unavailable'
  })
  apiMocks.listSpatialChanges.mockResolvedValue(emptyChanges)
  apiMocks.listMapChunks.mockResolvedValue(chunks)
  apiMocks.getMapChunk.mockResolvedValue(chunk)
}

function configureServiceAPI(): void {
  apiMocks.getServiceCatalog.mockResolvedValue(serviceCatalog)
  apiMocks.listServiceFacilities.mockResolvedValue(serviceFacilities)
  apiMocks.listServiceDemands.mockResolvedValue(serviceDemands)
  apiMocks.listServiceConnections.mockResolvedValue(serviceConnections)
  apiMocks.listServiceSettlements.mockResolvedValue(serviceSettlements)
  apiMocks.getPhysicalNetworkCatalog.mockResolvedValue(physicalNetworkCatalog)
  apiMocks.listPhysicalNetworks.mockResolvedValue(physicalNetworks)
  apiMocks.listPhysicalNetworkNodes.mockResolvedValue(physicalNetworkNodes)
  apiMocks.listPhysicalNetworkEdges.mockResolvedValue(physicalNetworkEdges)
  apiMocks.listPhysicalNetworkFlows.mockResolvedValue(physicalNetworkFlows)
  apiMocks.listPhysicalNetworkFacts.mockResolvedValue(physicalNetworkFacts)
  apiMocks.getPhysicalNetworkDiagnostics.mockResolvedValue(physicalNetworkDiagnostics)
}

describe('city spatial store', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    setActivePinia(createPinia())
  })

  it('loads the bound rules, Overmap, land baseline, summaries, and projected Chunk as one world state', async () => {
    configureReadAPI()
    const store = useCitySpatialStore()
    await store.initialize(7)

    expect(store.activeWorldID).toBe(7)
    expect(store.ruleSet?.content_hash).toBe(ruleBundle.profile.rule_set_hash)
    expect(store.overmap?.tiles).toHaveLength(1)
    expect(store.landAvailability).toBe('available')
    expect(store.activeLandState?.profile.baseline_hash).toBe(landState.profile.baseline_hash)
    expect(store.activeLandState?.buildings[0]?.code).toBe('building_central')
    expect(store.chunkSummaries.get('z:0/x:0/y:0')).toEqual(summary)
    expect(store.projectedChunks.get('z:0/x:0/y:0')?.cells).toHaveLength(1024)
    expect(store.selectedTile?.district_code).toBe('central')
    expect(store.camera.cellSize).toBe(10)
    store.changeZoom(-1)
    expect(store.camera.cellSize).toBe(8)
  })

  it('loads and caches each requested Z land layer without clearing the visible layer', async () => {
    configureReadAPI()
    const store = useCitySpatialStore()
    await store.initialize(7)
    const surface = store.activeLandState

    store.showLocalMap()
    store.setZ(1)
    await vi.waitFor(() => expect(store.activeLandState?.buildings[0]?.top_z).toBe(1))

    expect(surface).toBe(landState)
    expect(store.landLayers.get(0)).toBe(landState)
    expect(store.landLayers.get(1)).toBe(landState)
    expect(apiMocks.getLandState).toHaveBeenLastCalledWith(7, {
      min_x: 0, max_x: 0, min_y: 0, max_y: 0, z: 1
    })
  })

  it('keeps legacy spatial worlds usable when the land capability is not available', async () => {
    configureReadAPI()
    apiMocks.listWorlds.mockResolvedValue([{ ...world, simulation_version: 'city-f7-v1' }])
    apiMocks.getLandState.mockRejectedValue({
      status: 404,
      code: 'CITY_LAND_STATE_NOT_FOUND',
      message: 'city land state not found'
    })
    apiMocks.getDevelopmentState.mockRejectedValue({
      status: 404,
      code: 'CITY_DEVELOPMENT_STATE_NOT_FOUND',
      message: 'city development state not found'
    })
    apiMocks.getEnterpriseLocationState.mockRejectedValue({
      status: 404,
      code: 'CITY_ENTERPRISE_LOCATION_STATE_NOT_FOUND',
      message: 'city enterprise location state not found'
    })
    const store = useCitySpatialStore()

    await store.initialize(7)

    expect(store.landAvailability).toBe('unavailable')
    expect(store.activeLandState).toBeNull()
    expect(store.loadError).toBeNull()
    expect(store.projectedChunks.size).toBe(1)
    expect(store.enterpriseLocationAvailability).toBe('unavailable')
  })

  it('selects current open-world generations without probing legacy F7 endpoints', async () => {
    configureReadAPI()
    apiMocks.listWorlds.mockResolvedValue([{ ...world, simulation_version: 'city-openworld-v24' }])
    apiMocks.getWorldRuntimeCatalog.mockResolvedValue(runtimeCatalog)
    const store = useCitySpatialStore()

    await store.initialize(7)

    expect(store.activeWorld?.simulation_version).toBe('city-openworld-v24')
    expect(store.ruleSet).toBeNull()
    expect(store.overmap).toBeNull()
    expect(apiMocks.getWorldSpatialRuleSet).not.toHaveBeenCalled()
    expect(apiMocks.getOvermap).not.toHaveBeenCalled()
    expect(apiMocks.listMapChunks).not.toHaveBeenCalled()
    expect(apiMocks.getLandState).not.toHaveBeenCalled()
    expect(apiMocks.getWorldRuntimeCatalog).toHaveBeenCalledWith(7)

    await store.refresh()
    expect(apiMocks.getWorldSpatialRuleSet).not.toHaveBeenCalled()
    expect(apiMocks.getOvermap).not.toHaveBeenCalled()
  })

  it('uses the open-world command contract when a current open-world player moves', async () => {
    configureReadAPI()
    apiMocks.listWorlds.mockResolvedValue([{ ...world, simulation_version: 'city-openworld-v24' }])
    apiMocks.getWorldRuntimeCatalog.mockResolvedValue(runtimeCatalog)
    apiMocks.listWorldActors.mockResolvedValue([{
      ...runtimeActor,
      location: {
        ...runtimeActor.location!, space_kind: 'surface', space_code: 'surface',
        anchor_kind: 'chunk', anchor_code: 'chunk.0.0'
      }
    }])
    apiMocks.getWorldActorState
      .mockResolvedValueOnce({
        ...runtimeActorState(16000, 1),
        actor: {
          ...runtimeActor,
          location: {
            ...runtimeActor.location!, space_kind: 'surface', space_code: 'surface',
            anchor_kind: 'chunk', anchor_code: 'chunk.0.0'
          }
        },
        location: {
          ...runtimeActor.location!, space_kind: 'surface', space_code: 'surface',
          anchor_kind: 'chunk', anchor_code: 'chunk.0.0'
        }
      })
      .mockResolvedValueOnce(runtimeActorState(16000, 2))
    apiMocks.getWorldActorRoleOptions.mockResolvedValue(runtimeRoleOptions)
    apiMocks.listWorldRuntimeRules.mockResolvedValue([])
    apiMocks.listWorldRuleCases.mockResolvedValue({ items: [] })
    apiMocks.submitWorldRuntimeCommand.mockResolvedValue({
      id: 31, world_id: 7, user_id: 1, sequence: 5,
      client_request_id: 'open-world-move', command_type: 'open_world.actor.move',
      payload: { actor_code: runtimeActor.code, space_kind: 'surface', floor_index: 0, x: 7, y: 7, z: 0 },
      expected_world_tick: 0, status: 'pending', result: {},
      submitted_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z'
    })
    apiMocks.stepWorld.mockResolvedValue({
      tick: {
        id: 1, world_id: 7, tick: 1, state_hash: 'state', command_count: 1,
        applied_command_count: 1, rejected_command_count: 0, event_count: 1, duration_ms: 1
      },
      commands: [{ id: 31, status: 'applied' }], spatial_mutations: [], development_facts: [],
      building_adjustments: [], enterprise_location_facts: [], world_runtime_facts: [],
      world_effect_operations: [], world_rule_cases: [], events: []
    })

    const store = useCitySpatialStore()
    await store.initialize(7)

    await expect(store.runWorldRuntimeCommand(
      'actor.location.move',
      { actor_code: runtimeActor.code, x: 7, y: 7, z: 0 },
      'move:east'
    )).resolves.toBe('applied')

    expect(apiMocks.submitWorldRuntimeCommand).toHaveBeenCalledWith(
      7,
      'open_world.actor.move',
      {
        actor_code: runtimeActor.code,
        space_kind: 'surface',
        floor_index: 0,
        x: 7,
        y: 7,
        z: 0
      },
      0
    )
  })

  it('queues a running open-world player command without a stale client tick precondition', async () => {
    configureReadAPI()
    const runningOpenWorld: CityWorld = {
      ...world,
      status: 'running',
      simulation_version: 'city-openworld-v24',
      speed_multiplier: 1000,
      current_tick: 42
    }
    apiMocks.listWorlds.mockResolvedValue([runningOpenWorld])
    apiMocks.getWorld.mockResolvedValue({ ...runningOpenWorld, current_tick: 43 })
    apiMocks.getWorldRuntimeCatalog.mockResolvedValue(runtimeCatalog)
    apiMocks.listWorldActors.mockResolvedValue([runtimeActor])
    apiMocks.getWorldActorState.mockResolvedValue(runtimeActorState(16000, 43))
    apiMocks.getWorldActorRoleOptions.mockResolvedValue(runtimeRoleOptions)
    apiMocks.listWorldRuntimeRules.mockResolvedValue([])
    apiMocks.listWorldRuleCases.mockResolvedValue({ items: [] })
    apiMocks.submitWorldRuntimeCommand.mockResolvedValue({
      id: 32, world_id: 7, user_id: 1, sequence: 6,
      client_request_id: 'running-portal-use', command_type: 'open_world.actor.portal.use',
      payload: { actor_code: runtimeActor.code, portal_code: 'building_central.entrance_main' },
      status: 'pending', result: {},
      submitted_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z'
    })
    apiMocks.stepWorld.mockRejectedValue({
      status: 409,
      code: 409,
      reason: 'CITY_EXPECTED_TICK_CONFLICT',
      message: 'city world tick no longer matches the expected tick'
    })
    apiMocks.getCommand.mockResolvedValue({
      id: 32, world_id: 7, user_id: 1, sequence: 6,
      client_request_id: 'running-portal-use', command_type: 'open_world.actor.portal.use',
      payload: { actor_code: runtimeActor.code, portal_code: 'building_central.entrance_main' },
      status: 'applied', processed_tick: 43, result: {},
      submitted_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:01Z'
    })

    const store = useCitySpatialStore()
    await store.initialize(7)

    await expect(store.runWorldRuntimeCommand(
      'open_world.actor.portal.use',
      { actor_code: runtimeActor.code, portal_code: 'building_central.entrance_main' },
      'portal:use:building_central/entrance_main'
    )).resolves.toBe('applied')

    expect(apiMocks.submitWorldRuntimeCommand).toHaveBeenCalledWith(
      7,
      'open_world.actor.portal.use',
      { actor_code: runtimeActor.code, portal_code: 'building_central.entrance_main' },
      undefined
    )
    expect(apiMocks.stepWorld).toHaveBeenCalledWith(7, 42)
    expect(apiMocks.getCommand).toHaveBeenCalledWith(7, 32)
    expect(store.activeWorld?.current_tick).toBe(43)
  })

  it('starts an open world at a playable cadence through the administrator lifecycle command', async () => {
    configureReadAPI()
    const pausedOpenWorld: CityWorld = {
      ...world,
      status: 'paused',
      simulation_version: 'city-openworld-v24',
      speed_multiplier: 1
    }
    const runningOpenWorld: CityWorld = {
      ...pausedOpenWorld,
      status: 'running',
      speed_multiplier: 1000,
      current_tick: 1
    }
    apiMocks.listWorlds.mockResolvedValue([pausedOpenWorld])
    apiMocks.getWorld.mockResolvedValue(runningOpenWorld)
    apiMocks.getWorldRuntimeCatalog.mockResolvedValue(runtimeCatalog)
    apiMocks.listWorldActors.mockResolvedValue([])
    apiMocks.listWorldRuntimeRules.mockResolvedValue([])
    apiMocks.listWorldRuleCases.mockResolvedValue({ items: [] })
    apiMocks.submitWorldControlCommand.mockResolvedValue({
      id: 41, world_id: 7, user_id: 1, sequence: 9,
      client_request_id: 'world-resume', command_type: 'world.resume',
      payload: {}, expected_world_tick: 0, status: 'pending', result: {},
      submitted_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z'
    })
    apiMocks.stepWorld.mockResolvedValue({
      tick: {
        id: 1, world_id: 7, tick: 1, state_hash: 'state', command_count: 1,
        applied_command_count: 1, rejected_command_count: 0, event_count: 1, duration_ms: 1
      },
      commands: [{ id: 41, status: 'applied' }], spatial_mutations: [], development_facts: [],
      building_adjustments: [], enterprise_location_facts: [], world_runtime_facts: [],
      world_effect_operations: [], world_rule_cases: [], events: []
    })

    const store = useCitySpatialStore()
    await store.initialize(7)

    await expect(store.runWorldLifecycleCommand('world.resume')).resolves.toBe('applied')

    expect(apiMocks.submitWorldControlCommand).toHaveBeenCalledWith(7, 'world.resume', {}, 0)
    expect(apiMocks.stepWorld).toHaveBeenCalledWith(7, 0)
    expect(store.activeWorld).toMatchObject({ status: 'running', speed_multiplier: 1000, current_tick: 1 })
    expect(store.worldLifecycleCommandCode).toBeNull()
  })

  it('reconciles a lifecycle command when the scheduler has already advanced the world tick', async () => {
    configureReadAPI()
    const pausedOpenWorld: CityWorld = {
      ...world,
      status: 'paused',
      simulation_version: 'city-openworld-v24',
      speed_multiplier: 1000
    }
    const runningOpenWorld: CityWorld = {
      ...pausedOpenWorld,
      status: 'running',
      current_tick: 2
    }
    apiMocks.listWorlds.mockResolvedValue([pausedOpenWorld])
    apiMocks.getWorld.mockResolvedValue(runningOpenWorld)
    apiMocks.getWorldRuntimeCatalog.mockResolvedValue(runtimeCatalog)
    apiMocks.listWorldActors.mockResolvedValue([])
    apiMocks.listWorldRuntimeRules.mockResolvedValue([])
    apiMocks.listWorldRuleCases.mockResolvedValue({ items: [] })
    apiMocks.submitWorldControlCommand.mockResolvedValue({
      id: 42, world_id: 7, user_id: 1, sequence: 10,
      client_request_id: 'world-resume-race', command_type: 'world.resume',
      payload: {}, expected_world_tick: 0, status: 'pending', result: {},
      submitted_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z'
    })
    apiMocks.stepWorld.mockRejectedValue({
      status: 409,
      code: 409,
      reason: 'CITY_EXPECTED_TICK_CONFLICT',
      message: 'city world tick no longer matches the expected tick'
    })
    apiMocks.getCommand.mockResolvedValue({
      id: 42, world_id: 7, user_id: 1, sequence: 10,
      client_request_id: 'world-resume-race', command_type: 'world.resume',
      payload: {}, expected_world_tick: 0, status: 'applied', processed_tick: 2, result: { status: 'running' },
      submitted_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:01Z'
    })

    const store = useCitySpatialStore()
    await store.initialize(7)

    await expect(store.runWorldLifecycleCommand('world.resume')).resolves.toBe('applied')

    expect(apiMocks.getCommand).toHaveBeenCalledWith(7, 42)
    expect(apiMocks.getWorld).toHaveBeenCalledWith(7)
    expect(store.activeWorld).toMatchObject({ status: 'running', speed_multiplier: 1000, current_tick: 2 })
    expect(store.loadError).toBeNull()
  })

  it('preserves the active interior floor and forwards registered portal traversal unchanged', async () => {
    configureReadAPI()
    const interiorLocation = {
      ...runtimeActor.location!,
      space_kind: 'interior',
      space_code: 'building_central',
      anchor_kind: 'building' as const,
      anchor_code: 'building_central',
      x: 4,
      y: 5,
      z: 2,
      version: 2
    }
    const interiorActor = { ...runtimeActor, location: interiorLocation }
    const interiorState = {
      ...runtimeActorState(16000, 2),
      actor: interiorActor,
      location: interiorLocation
    }
    const delegatedOpenWorld = {
      ...world, simulation_version: 'city-openworld-v24', member_role: 'planner', owner_user_id: 9
    }
    apiMocks.listWorlds.mockResolvedValue([delegatedOpenWorld])
    apiMocks.getWorld.mockResolvedValue(delegatedOpenWorld)
    apiMocks.getWorldRuntimeCatalog.mockResolvedValue(runtimeCatalog)
    apiMocks.listWorldActors.mockResolvedValue([interiorActor])
    apiMocks.getWorldActorState.mockResolvedValue(interiorState)
    apiMocks.getWorldActorRoleOptions.mockResolvedValue(runtimeRoleOptions)
    apiMocks.listWorldRuntimeRules.mockResolvedValue([])
    apiMocks.listWorldRuleCases.mockResolvedValue({ items: [] })
    apiMocks.submitWorldRuntimeCommand
      .mockResolvedValueOnce({
        id: 33, world_id: 7, user_id: 2, sequence: 7,
        client_request_id: 'interior-move', command_type: 'open_world.actor.move',
        payload: {}, expected_world_tick: 0, status: 'pending', result: {},
        submitted_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z'
      })
      .mockResolvedValueOnce({
        id: 34, world_id: 7, user_id: 2, sequence: 8,
        client_request_id: 'portal-use', command_type: 'open_world.actor.portal.use',
        payload: {}, expected_world_tick: 0, status: 'pending', result: {},
        submitted_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z'
      })
    apiMocks.getCommand.mockResolvedValue({
      id: 34, world_id: 7, user_id: 2, sequence: 8,
      client_request_id: 'portal-use', command_type: 'open_world.actor.portal.use',
      payload: {}, expected_world_tick: 0, status: 'applied', processed_tick: 1, result: {},
      submitted_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:01Z'
    })

    const store = useCitySpatialStore()
    await store.initialize(7)

    await expect(store.runWorldRuntimeCommand(
      'actor.location.move',
      { actor_code: runtimeActor.code, x: 5, y: 5, z: 2 },
      'move:interior-east'
    )).resolves.toBe('queued')
    await expect(store.runWorldRuntimeCommand(
      'open_world.actor.portal.use',
      { actor_code: runtimeActor.code, portal_code: 'building_central.entrance_main' },
      'portal:use:building_central/entrance_main'
    )).resolves.toBe('queued')

    expect(apiMocks.submitWorldRuntimeCommand).toHaveBeenNthCalledWith(
      1,
      7,
      'open_world.actor.move',
      {
        actor_code: runtimeActor.code,
        space_kind: 'interior',
        building_code: 'building_central',
        floor_index: 2,
        x: 5,
        y: 5,
        z: 2
      },
      0
    )
    expect(apiMocks.submitWorldRuntimeCommand).toHaveBeenNthCalledWith(
      2,
      7,
      'open_world.actor.portal.use',
      { actor_code: runtimeActor.code, portal_code: 'building_central.entrance_main' },
      0
    )
    expect(apiMocks.stepWorld).not.toHaveBeenCalled()
  })

  it('uses the target interior floor when an open-world navigation intent crosses levels', async () => {
    configureReadAPI()
    const interiorLocation = {
      ...runtimeActor.location!,
      space_kind: 'interior',
      space_code: 'building_central',
      anchor_kind: 'building' as const,
      anchor_code: 'building_central',
      x: 4,
      y: 5,
      z: 2,
      version: 2
    }
    const interiorActor = { ...runtimeActor, location: interiorLocation }
    const interiorState: WorldActorState = {
      ...runtimeActorState(16000, 2),
      actor: interiorActor,
      location: interiorLocation
    }
    apiMocks.listWorlds.mockResolvedValue([{ ...world, simulation_version: 'city-openworld-v24' }])
    apiMocks.getWorldRuntimeCatalog.mockResolvedValue(runtimeCatalog)
    apiMocks.listWorldActors.mockResolvedValue([interiorActor])
    apiMocks.getWorldActorState.mockResolvedValue(interiorState)
    apiMocks.getWorldActorRoleOptions.mockResolvedValue(runtimeRoleOptions)
    apiMocks.listWorldRuntimeRules.mockResolvedValue([])
    apiMocks.listWorldRuleCases.mockResolvedValue({ items: [] })
    apiMocks.submitWorldRuntimeCommand.mockResolvedValue({
      id: 35, world_id: 7, user_id: 1, sequence: 9,
      client_request_id: 'cross-floor-navigation', command_type: 'open_world.actor.navigation.set',
      payload: {}, expected_world_tick: 0, status: 'pending', result: {},
      submitted_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z'
    })
    apiMocks.stepWorld.mockResolvedValue({
      tick: {
        id: 1, world_id: 7, tick: 1, state_hash: 'state', command_count: 1,
        applied_command_count: 1, rejected_command_count: 0, event_count: 1, duration_ms: 1
      },
      commands: [{ id: 35, status: 'applied' }], spatial_mutations: [], development_facts: [],
      building_adjustments: [], enterprise_location_facts: [], world_runtime_facts: [],
      world_effect_operations: [], world_rule_cases: [], events: []
    })

    const store = useCitySpatialStore()
    await store.initialize(7)

    await expect(store.runWorldRuntimeCommand(
      'actor.navigation.intent.set',
      {
        actor_code: runtimeActor.code,
        destination: { x: 2, y: 8, z: 3 },
        priority: 0,
        max_steps: 256,
        on_blocked: 'retry'
      },
      'navigation:intent:set:actor_00000001'
    )).resolves.toBe('applied')

    expect(apiMocks.submitWorldRuntimeCommand).toHaveBeenCalledWith(
      7,
      'open_world.actor.navigation.set',
      {
        actor_code: runtimeActor.code,
        space_kind: 'interior',
        building_code: 'building_central',
        floor_index: 3,
        x: 2,
        y: 8,
        z: 3,
        priority: 0,
        maximum_steps: 256
      },
      0
    )
  })

  it('reconciles an owner command when the scheduler seals its tick first', async () => {
    configureReadAPI()
    apiMocks.listWorlds.mockResolvedValue([{ ...world, simulation_version: 'city-openworld-v24' }])
    apiMocks.getWorld.mockResolvedValue({ ...world, simulation_version: 'city-openworld-v24', current_tick: 1 })
    apiMocks.getWorldRuntimeCatalog.mockResolvedValue(runtimeCatalog)
    apiMocks.listWorldActors.mockResolvedValue([runtimeActor])
    apiMocks.getWorldActorState.mockResolvedValue(runtimeActorState(16000, 1))
    apiMocks.getWorldActorRoleOptions.mockResolvedValue(runtimeRoleOptions)
    apiMocks.listWorldRuntimeRules.mockResolvedValue([])
    apiMocks.listWorldRuleCases.mockResolvedValue({ items: [] })
    apiMocks.submitWorldRuntimeCommand.mockResolvedValue({
      id: 32, world_id: 7, user_id: 1, sequence: 6,
      client_request_id: 'scheduler-race', command_type: 'open_world.actor.activity.perform',
      payload: { actor_code: runtimeActor.code, activity_code: 'technical_study' },
      expected_world_tick: 0, status: 'pending', result: {},
      submitted_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z'
    })
    apiMocks.stepWorld.mockRejectedValue({
      status: 409,
      code: 409,
      reason: 'CITY_EXPECTED_TICK_CONFLICT',
      message: 'city world tick no longer matches the expected tick'
    })
    apiMocks.getCommand.mockResolvedValue({
      id: 32, world_id: 7, user_id: 1, sequence: 6,
      client_request_id: 'scheduler-race', command_type: 'open_world.actor.activity.perform',
      payload: { actor_code: runtimeActor.code, activity_code: 'technical_study' },
      expected_world_tick: 0, status: 'applied', processed_tick: 1,
      result: { actor_code: runtimeActor.code },
      submitted_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:01Z'
    })

    const store = useCitySpatialStore()
    await store.initialize(7)

    await expect(store.runWorldRuntimeCommand(
      'actor.activity.perform',
      { actor_code: runtimeActor.code, activity_code: 'technical_study' },
      'activity:technical_study'
    )).resolves.toBe('applied')

    expect(apiMocks.stepWorld).toHaveBeenCalledWith(7, 0)
    expect(apiMocks.getCommand).toHaveBeenCalledWith(7, 32)
    expect(store.worldCommandReceipts[0]?.status).toBe('applied')
    expect(store.activeWorld?.current_tick).toBe(1)
    expect(store.loadError).toBeNull()
  })

  it('keeps the current projection visible during a refresh', async () => {
    configureReadAPI()
    const store = useCitySpatialStore()
    await store.initialize(7)
    const before = store.projectedChunks

    let finishOvermap!: (value: CityOvermapState) => void
    apiMocks.getOvermap.mockReturnValueOnce(new Promise(resolve => { finishOvermap = resolve }))
    const refreshPromise = store.refresh()
    await Promise.resolve()

    expect(store.refreshing).toBe(true)
    expect(store.projectedChunks).toBe(before)
    expect(store.projectedChunks.size).toBe(1)

    finishOvermap(overmap)
    await refreshPromise
    expect(store.refreshing).toBe(false)
    expect(store.projectedChunks.size).toBe(1)
  })

  it('loads public-service projections and replaces only the queried section after the response arrives', async () => {
    configureReadAPI()
    configureServiceAPI()
    const store = useCitySpatialStore()
    await store.initialize(7)

    expect(store.cityServiceAvailability).toBe('available')
    expect(store.cityServiceCatalog?.overview?.latest_shortage_units).toBe('16')
    const visibleScene = store.projectedChunks
    const visibleFacilities = store.cityServiceFacilities

    let finishQuery!: (value: CityServiceFacilityPage) => void
    apiMocks.listServiceFacilities.mockReturnValueOnce(new Promise(resolve => { finishQuery = resolve }))
    const query = store.queryCityServiceSection('facilities', { status: 'operational' })
    await Promise.resolve()

    expect(store.cityServiceLoading).toBe(true)
    expect(store.cityServiceFacilities).toBe(visibleFacilities)
    expect(store.projectedChunks).toBe(visibleScene)

    const updated = {
      ...serviceFacilities,
      items: [{
        ...serviceFacilities.items[0]!,
        facility: { ...serviceFacilities.items[0]!.facility, name: 'Central Power Updated' }
      }]
    }
    finishQuery(updated)
    await query

    expect(apiMocks.listServiceFacilities).toHaveBeenLastCalledWith(7, { status: 'operational' })
    expect(store.cityServiceFacilities?.items[0]?.facility.name).toBe('Central Power Updated')
    expect(store.cityServiceLoading).toBe(false)
    expect(store.projectedChunks).toBe(visibleScene)
  })

  it('loads physical topology and preserves it while a filtered edge query is in flight', async () => {
    configureReadAPI()
    configureServiceAPI()
    const store = useCitySpatialStore()
    await store.initialize(7)

    expect(store.cityPhysicalNetworkAvailability).toBe('available')
    expect(store.cityPhysicalNetworkCatalog?.overview?.latest_network_loss_units).toBe('16')
    expect(store.cityPhysicalNetworkNodes?.items).toHaveLength(2)
    const visibleEdges = store.cityPhysicalNetworkEdges

    let finishQuery!: (value: CityPhysicalNetworkEdgePage) => void
    apiMocks.listPhysicalNetworkEdges.mockReturnValueOnce(new Promise(resolve => { finishQuery = resolve }))
    const query = store.queryCityPhysicalNetworkSection('edges', { network: 'grid_central', status: 'active' })
    await Promise.resolve()

    expect(store.cityPhysicalNetworkLoading).toBe(true)
    expect(store.cityPhysicalNetworkEdges).toBe(visibleEdges)

    const updated = {
      ...physicalNetworkEdges,
      items: [{ ...physicalNetworkEdges.items[0]!, availability_milli: 900, available_capacity_units: 810 }]
    }
    finishQuery(updated)
    await query

    expect(apiMocks.listPhysicalNetworkEdges).toHaveBeenLastCalledWith(7, {
      network: 'grid_central', status: 'active'
    })
    expect(store.cityPhysicalNetworkEdges?.items[0]?.available_capacity_units).toBe(810)
    expect(store.cityPhysicalNetworkLoading).toBe(false)

    await store.queryCityPhysicalNetworkDiagnostics({ network: 'grid_central' })
    expect(store.cityPhysicalNetworkDiagnostics?.component_count).toBe(1)
    const visibleDiagnostics = store.cityPhysicalNetworkDiagnostics
    let finishDiagnostics!: (value: CityPhysicalNetworkDiagnosticsView) => void
    apiMocks.getPhysicalNetworkDiagnostics.mockReturnValueOnce(new Promise(resolve => { finishDiagnostics = resolve }))
    const diagnosticQuery = store.queryCityPhysicalNetworkDiagnostics({
      network: 'grid_central', source: 'supply_power', sink: 'demand_power', probe_units: 100
    })
    await Promise.resolve()

    expect(store.cityPhysicalNetworkLoading).toBe(true)
    expect(store.cityPhysicalNetworkDiagnostics).toBe(visibleDiagnostics)
    const routedDiagnostics: CityPhysicalNetworkDiagnosticsView = {
      ...physicalNetworkDiagnostics,
      route: {
        source_node_code: 'supply_power', sink_node_code: 'demand_power', probe_units: 100,
        reachable: true, reason_code: 'reachable', dispatched_units: 100,
        network_received_units: 98, network_loss_units: 2, paths: []
      }
    }
    finishDiagnostics(routedDiagnostics)
    await diagnosticQuery

    expect(apiMocks.getPhysicalNetworkDiagnostics).toHaveBeenLastCalledWith(7, {
      network: 'grid_central', source: 'supply_power', sink: 'demand_power', probe_units: 100
    })
    expect(store.cityPhysicalNetworkDiagnostics?.route?.reachable).toBe(true)
    expect(store.cityPhysicalNetworkLoading).toBe(false)

    apiMocks.getPhysicalNetworkDiagnostics.mockResolvedValue(routedDiagnostics)
    await store.loadCityPhysicalNetworks(true)
    expect(apiMocks.getPhysicalNetworkDiagnostics).toHaveBeenLastCalledWith(7, {
      network: 'grid_central', source: 'supply_power', sink: 'demand_power', probe_units: 100
    })
    expect(store.cityPhysicalNetworkDiagnostics?.route?.reachable).toBe(true)
  })

  it('posts a public-service CAS command through a tick and reloads only service projections', async () => {
    configureReadAPI()
    configureServiceAPI()
    apiMocks.submitServiceCommand.mockResolvedValue({
      id: 24, world_id: 7, user_id: 1, sequence: 6,
      client_request_id: 'service-request', command_type: 'facility.status.transition',
      payload: { facility_code: 'facility_central_power', to_status: 'degraded', expected_version: 2 },
      expected_world_tick: 0, status: 'pending', result: {},
      submitted_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z'
    })
    apiMocks.stepWorld.mockResolvedValue({
      tick: {
        id: 1, world_id: 7, tick: 1, state_hash: 'state', command_count: 1,
        applied_command_count: 1, rejected_command_count: 0, event_count: 2, duration_ms: 1
      },
      commands: [{ id: 24, status: 'applied' }], spatial_mutations: [],
      development_facts: [], building_adjustments: [], enterprise_location_facts: [],
      service_facts: [], service_allocations: [], service_settlements: [], events: []
    })
    const store = useCitySpatialStore()
    await store.initialize(7)
    const visibleScene = store.projectedChunks

    const payload = {
      facility_code: 'facility_central_power', to_status: 'degraded',
      expected_version: 2, metadata: {}
    }
    await store.runCityServiceCommand('facility.status.transition', payload, 'status:facility_central_power')

    expect(apiMocks.submitServiceCommand).toHaveBeenCalledWith(
      7, 'facility.status.transition', payload, 0
    )
    expect(apiMocks.stepWorld).toHaveBeenCalledWith(7, 0)
    expect(store.activeWorld?.current_tick).toBe(1)
    expect(store.cityServiceCommandCode).toBeNull()
    expect(store.projectedChunks).toBe(visibleScene)
    expect(apiMocks.getServiceCatalog).toHaveBeenCalledTimes(2)
  })

  it('generates a missing Chunk only through command plus tick and then loads its real payload', async () => {
    configureReadAPI([])
    apiMocks.listMapChunks
      .mockResolvedValueOnce([])
      .mockResolvedValueOnce([summary])
    apiMocks.submitGenerateChunk.mockResolvedValue({
      id: 19,
      world_id: 7,
      user_id: 1,
      sequence: 1,
      client_request_id: 'request',
      command_type: 'spatial.generate_chunk',
      payload: { chunk_x: 0, chunk_y: 0, z: 0 },
      expected_world_tick: 0,
      status: 'pending',
      result: {},
      submitted_at: '2026-01-01T00:00:00Z',
      updated_at: '2026-01-01T00:00:00Z'
    })
    apiMocks.stepWorld.mockResolvedValue({
      tick: { id: 1, world_id: 7, tick: 1, state_hash: 'state', command_count: 1, applied_command_count: 1, rejected_command_count: 0, event_count: 1, duration_ms: 1 },
      commands: [{ id: 19, status: 'applied' }],
      spatial_mutations: [{ id: 2, world_id: 7, tick: 1, sequence: 1, source_command_id: 19, mutation_type: 'chunk_generated', expected_line_count: 1, metadata: {}, posted_at: '2026-01-01T01:00:00Z', created_at: '2026-01-01T01:00:00Z', lines: [] }],
      events: []
    })

    const store = useCitySpatialStore()
    await store.initialize(7)
    expect(store.canGenerateSelectedTile).toBe(true)
    await store.generateSelectedChunk()

    expect(apiMocks.submitGenerateChunk).toHaveBeenCalledWith(7, 0, 0, 0, 0)
    expect(apiMocks.stepWorld).toHaveBeenCalledWith(7, 0)
    expect(store.activeWorld?.current_tick).toBe(1)
    expect(store.projectedChunks.has('z:0/x:0/y:0')).toBe(true)
    expect(store.latestChanges[0]?.mutation_type).toBe('chunk_generated')
  })

  it('posts development intent through command plus tick without clearing the visible scene', async () => {
    configureReadAPI()
    const store = useCitySpatialStore()
    await store.initialize(7)
    const visibleScene = store.projectedChunks
    apiMocks.submitDevelopmentCommand.mockResolvedValue({
      id: 20, world_id: 7, user_id: 1, sequence: 2,
      client_request_id: 'development-request', command_type: 'development.review',
      payload: { project_code: 'development_7', decision: 'approve' },
      expected_world_tick: 0, status: 'pending', result: {},
      submitted_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z'
    })
    apiMocks.stepWorld.mockResolvedValue({
      tick: {
        id: 1, world_id: 7, tick: 1, state_hash: 'state', command_count: 1,
        applied_command_count: 1, rejected_command_count: 0, event_count: 1, duration_ms: 1
      },
      commands: [{ id: 20, status: 'applied' }], spatial_mutations: [],
      development_facts: [], building_adjustments: [], events: []
    })

    await store.runDevelopmentCommand(
      'development.review',
      { project_code: 'development_7', decision: 'approve' },
      'development_7'
    )

    expect(apiMocks.submitDevelopmentCommand).toHaveBeenCalledWith(
      7,
      'development.review',
      { project_code: 'development_7', decision: 'approve' },
      0
    )
    expect(apiMocks.stepWorld).toHaveBeenCalledWith(7, 0)
    expect(store.activeWorld?.current_tick).toBe(1)
    expect(store.projectedChunks).toBe(visibleScene)
    expect(store.developmentCommandCode).toBeNull()
  })

  it('posts an enterprise location intent through command plus tick and retains the map projection', async () => {
    configureReadAPI()
    const store = useCitySpatialStore()
    await store.initialize(7)
    const visibleScene = store.projectedChunks
    apiMocks.submitEnterpriseLocationCommand.mockResolvedValue({
      id: 21, world_id: 7, user_id: 1, sequence: 3,
      client_request_id: 'enterprise-request', command_type: 'enterprise.site.resize',
      payload: { site_code: 'site_firm_central_headquarters', target_occupied_units: 6 },
      expected_world_tick: 0, status: 'pending', result: {},
      submitted_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z'
    })
    apiMocks.stepWorld.mockResolvedValue({
      tick: {
        id: 1, world_id: 7, tick: 1, state_hash: 'state', command_count: 1,
        applied_command_count: 1, rejected_command_count: 0, event_count: 1, duration_ms: 1
      },
      commands: [{ id: 21, status: 'applied' }], spatial_mutations: [],
      development_facts: [], building_adjustments: [], enterprise_location_facts: [], events: []
    })

    await store.runEnterpriseLocationCommand(
      'enterprise.site.resize',
      { site_code: 'site_firm_central_headquarters', target_occupied_units: 6 },
      'site_firm_central_headquarters'
    )

    expect(apiMocks.submitEnterpriseLocationCommand).toHaveBeenCalledWith(
      7,
      'enterprise.site.resize',
      { site_code: 'site_firm_central_headquarters', target_occupied_units: 6 },
      0
    )
    expect(store.activeWorld?.current_tick).toBe(1)
    expect(store.projectedChunks).toBe(visibleScene)
    expect(store.enterpriseLocationCommandCode).toBeNull()
  })

  it('loads the open-world runtime and seals an activity without replacing the visible map', async () => {
    configureReadAPI()
    apiMocks.getWorldRuntimeCatalog.mockResolvedValue(runtimeCatalog)
    apiMocks.listWorldActors.mockResolvedValue([runtimeActor])
    apiMocks.getWorldActorState
      .mockResolvedValueOnce(runtimeActorState(16000, 1))
      .mockResolvedValueOnce(runtimeActorState(18500, 2))
    apiMocks.getWorldActorRoleOptions.mockResolvedValue(runtimeRoleOptions)
    apiMocks.listWorldRuntimeRules.mockResolvedValue([])
    apiMocks.listWorldRuleCases.mockResolvedValue({ items: [] })
    apiMocks.submitWorldRuntimeCommand.mockResolvedValue({
      id: 22, world_id: 7, user_id: 1, sequence: 4,
      client_request_id: 'runtime-request', command_type: 'actor.activity.perform',
      payload: { actor_code: runtimeActor.code, activity_code: 'technical_study' },
      expected_world_tick: 0, status: 'pending', result: {},
      submitted_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z'
    })
    apiMocks.stepWorld.mockResolvedValue({
      tick: {
        id: 1, world_id: 7, tick: 1, state_hash: 'state', command_count: 1,
        applied_command_count: 1, rejected_command_count: 0, event_count: 1, duration_ms: 1
      },
      commands: [{ id: 22, status: 'applied' }], spatial_mutations: [], development_facts: [],
      building_adjustments: [], enterprise_location_facts: [], world_runtime_facts: [],
      world_effect_operations: [], world_rule_cases: [], events: []
    })

    const store = useCitySpatialStore()
    await store.initialize(7)
    const visibleScene = store.projectedChunks

    expect(store.worldRuntimeAvailability).toBe('available')
    expect(store.selectedActorCode).toBe(runtimeActor.code)
    expect(store.worldActorState?.attributes[0]?.value_units).toBe(16000)
    expect(apiMocks.listWorldRuleCases).toHaveBeenCalledWith(7, {
      actor_code: runtimeActor.code, limit: 100
    })

    const result = await store.runWorldRuntimeCommand(
      'actor.activity.perform',
      { actor_code: runtimeActor.code, activity_code: 'technical_study' },
      'activity:technical_study'
    )

    expect(apiMocks.submitWorldRuntimeCommand).toHaveBeenCalledWith(
      7,
      'actor.activity.perform',
      { actor_code: runtimeActor.code, activity_code: 'technical_study' },
      0
    )
    expect(apiMocks.stepWorld).toHaveBeenCalledWith(7, 0)
    expect(store.activeWorld?.current_tick).toBe(1)
    expect(store.projectedChunks).toBe(visibleScene)
    expect(store.worldActorState?.attributes[0]?.value_units).toBe(18500)
    expect(store.worldRuntimeCommandCode).toBeNull()
    expect(result).toBe('applied')
  })

  it('loads actor-evaluated portal access without making legacy runtime loading dependent on it', async () => {
    configureReadAPI()
    apiMocks.getWorldRuntimeCatalog.mockResolvedValue(runtimeCatalog)
    apiMocks.listWorldActors.mockResolvedValue([runtimeActor])
    apiMocks.getWorldActorState.mockResolvedValue(runtimeActorState(16000, 1))
    apiMocks.getWorldActorRoleOptions.mockResolvedValue(runtimeRoleOptions)
    apiMocks.listWorldRuntimeRules.mockResolvedValue([])
    apiMocks.listWorldRuleCases.mockResolvedValue({ items: [] })
    apiMocks.listWorldPortalStates.mockResolvedValue([runtimePortal])

    const store = useCitySpatialStore()
    await store.initialize(7)

    expect(apiMocks.listWorldPortalStates).toHaveBeenCalledWith(7, runtimeActor.code)
    expect(store.worldPortalAccessAvailability).toBe('available')
    expect(store.worldPortalStates).toEqual([runtimePortal])
    expect(store.worldPortalLoading).toBe(false)

    apiMocks.listWorldPortalStates.mockRejectedValueOnce({
      status: 404, code: 'WORLD_PORTAL_ACCESS_UNAVAILABLE', message: 'unavailable'
    })
    await expect(store.loadWorldPortalStates(runtimeActor.code, true)).resolves.toEqual([])
    expect(store.worldPortalAccessAvailability).toBe('unavailable')
    expect(store.worldRuntimeAvailability).toBe('available')
  })

  it('publishes movement intents and reservations atomically while suppressing stale refreshes', async () => {
    configureReadAPI()
    apiMocks.getWorldRuntimeCatalog.mockResolvedValue(runtimeCatalog)
    apiMocks.listWorldActors.mockResolvedValue([runtimeActor])
    apiMocks.getWorldActorState.mockResolvedValue({
      ...runtimeActorState(16000, 1), navigation_intent: runtimeNavigationIntent
    })
    apiMocks.getWorldActorRoleOptions.mockResolvedValue(runtimeRoleOptions)
    apiMocks.listWorldRuntimeRules.mockResolvedValue([])
    apiMocks.listWorldRuleCases.mockResolvedValue({ items: [] })
    apiMocks.listWorldNavigationIntents.mockResolvedValue([runtimeNavigationIntent])
    apiMocks.listWorldNavigationReservations.mockResolvedValue([runtimeNavigationReservation])

    const store = useCitySpatialStore()
    await store.initialize(7)

    expect(store.worldNavigationIntentAvailability).toBe('available')
    expect(store.worldNavigationIntents).toEqual([runtimeNavigationIntent])
    expect(store.worldNavigationReservations).toEqual([runtimeNavigationReservation])
    expect(store.worldNavigationIntentLoading).toBe(false)

    let resolveStale!: (value: WorldActorNavigationIntent[]) => void
    apiMocks.listWorldNavigationIntents.mockReturnValueOnce(new Promise(resolve => { resolveStale = resolve }))
    apiMocks.listWorldNavigationReservations.mockResolvedValueOnce([])
    const staleRefresh = store.loadWorldNavigationState(true)

    const latestIntent: WorldActorNavigationIntent = {
      ...runtimeNavigationIntent,
      destination: { x: 10, y: 7, z: 0 },
      budget_units: 20,
      updated_tick: 2,
      source_fact: { tick: 2, sequence: 5 },
      version: 2
    }
    apiMocks.listWorldNavigationIntents.mockResolvedValueOnce([latestIntent])
    apiMocks.listWorldNavigationReservations.mockResolvedValueOnce([])
    await store.loadWorldNavigationState(true)
    resolveStale([runtimeNavigationIntent])
    await staleRefresh

    expect(store.worldNavigationIntents).toEqual([latestIntent])
    expect(store.worldNavigationReservations).toEqual([])
    expect(store.worldNavigationIntentLoading).toBe(false)
  })

  it('silently projects a running selected actor navigation until its server intent arrives', async () => {
    vi.useFakeTimers()
    try {
      configureReadAPI()
      const runningOpenWorld: CityWorld = {
        ...world,
        status: 'running',
        simulation_version: 'city-openworld-v24',
        speed_multiplier: 1000,
        current_tick: 4
      }
      const initialLocation = {
        ...runtimeActor.location!,
        space_kind: 'interior',
        space_code: 'building_central',
        anchor_kind: 'building' as const,
        anchor_code: 'building_central',
        x: 7,
        y: 7,
        z: 0,
        moved_tick: 4,
        version: 4
      }
      const arrivedLocation = {
        ...initialLocation,
        x: 9,
        y: 8,
        z: 1,
        moved_tick: 6,
        version: 6
      }
      const activeIntent: WorldActorNavigationIntent = {
        ...runtimeNavigationIntent,
        destination: { x: 9, y: 8, z: 1 },
        status: 'active',
        updated_tick: 4
      }
      const arrivedIntent: WorldActorNavigationIntent = {
        ...activeIntent,
        status: 'arrived',
        updated_tick: 6,
        version: 2
      }
      const activeState: WorldActorState = {
        ...runtimeActorState(16000, 4),
        actor: { ...runtimeActor, location: initialLocation, updated_tick: 4, version: 4 },
        location: initialLocation,
        navigation_intent: activeIntent
      }
      const arrivedState: WorldActorState = {
        ...runtimeActorState(16000, 6),
        actor: { ...runtimeActor, location: arrivedLocation, updated_tick: 6, version: 6 },
        location: arrivedLocation,
        navigation_intent: arrivedIntent
      }
      const refreshedPortal: WorldPortalAccessView = { ...runtimePortal, accessible: true }

      apiMocks.listWorlds.mockResolvedValue([runningOpenWorld])
      apiMocks.getWorld.mockResolvedValue({ ...runningOpenWorld, current_tick: 6 })
      apiMocks.getWorldRuntimeCatalog.mockResolvedValue(runtimeCatalog)
      apiMocks.listWorldActors.mockResolvedValue([{ ...runtimeActor, location: initialLocation }])
      apiMocks.getWorldActorState
        .mockResolvedValueOnce(activeState)
        .mockResolvedValueOnce(arrivedState)
      apiMocks.getWorldActorRoleOptions.mockResolvedValue(runtimeRoleOptions)
      apiMocks.listWorldRuntimeRules.mockResolvedValue([])
      apiMocks.listWorldRuleCases.mockResolvedValue({ items: [] })
      apiMocks.listWorldNavigationIntents
        .mockResolvedValueOnce([activeIntent])
        .mockResolvedValueOnce([arrivedIntent])
      apiMocks.listWorldNavigationReservations
        .mockResolvedValueOnce([runtimeNavigationReservation])
        .mockResolvedValueOnce([])
      apiMocks.listWorldPortalStates
        .mockResolvedValueOnce([runtimePortal])
        .mockResolvedValueOnce([refreshedPortal])

      const store = useCitySpatialStore()
      await store.initialize(7)

      expect(store.worldActorState?.location).toMatchObject({ x: 7, y: 7, z: 0 })
      expect(store.worldNavigationIntents[0]?.status).toBe('active')
      expect(store.worldRuntimeLoading).toBe(false)
      expect(store.worldPortalLoading).toBe(false)
      expect(store.worldNavigationIntentLoading).toBe(false)

      await vi.advanceTimersByTimeAsync(1200)
      await Promise.resolve()
      await Promise.resolve()

      expect(store.activeWorld?.current_tick).toBe(6)
      expect(store.worldActorState?.location).toMatchObject({ x: 9, y: 8, z: 1 })
      expect(store.worldActors[0]?.location).toMatchObject({ x: 9, y: 8, z: 1 })
      expect(store.worldNavigationIntents[0]?.status).toBe('arrived')
      expect(store.worldNavigationReservations).toEqual([])
      expect(store.worldPortalStates).toEqual([refreshedPortal])
      expect(store.worldRuntimeLoading).toBe(false)
      expect(store.worldPortalLoading).toBe(false)
      expect(store.worldNavigationIntentLoading).toBe(false)

      await vi.advanceTimersByTimeAsync(3600)
      expect(apiMocks.getWorldActorState).toHaveBeenCalledTimes(2)
    } finally {
      useCitySpatialStore().clear()
      vi.useRealTimers()
    }
  })

  it('queues an authorized member command without attempting the owner-only world tick', async () => {
    configureReadAPI()
    apiMocks.listWorlds.mockResolvedValue([{ ...world, member_role: 'planner', owner_user_id: 9 }])
    apiMocks.getWorld.mockResolvedValue({ ...world, member_role: 'planner', owner_user_id: 9, current_tick: 1 })
    apiMocks.getWorldRuntimeCatalog.mockResolvedValue(runtimeCatalog)
    apiMocks.listWorldActors.mockResolvedValue([runtimeActor])
    apiMocks.getWorldActorState.mockResolvedValue(runtimeActorState(16000, 1))
    apiMocks.getWorldActorRoleOptions.mockResolvedValue(runtimeRoleOptions)
    apiMocks.listWorldRuntimeRules.mockResolvedValue([])
    apiMocks.listWorldRuleCases.mockResolvedValue({ items: [] })
    apiMocks.submitWorldRuntimeCommand.mockResolvedValue({
      id: 23, world_id: 7, user_id: 2, sequence: 5,
      client_request_id: 'delegate-runtime-request', command_type: 'actor.location.move',
      payload: { actor_code: runtimeActor.code, x: 7, y: 7, z: 0 },
      expected_world_tick: 0, status: 'pending', result: {},
      submitted_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z'
    })
    apiMocks.getCommand.mockResolvedValue({
      id: 23, world_id: 7, user_id: 2, sequence: 5,
      client_request_id: 'delegate-runtime-request', command_type: 'actor.location.move',
      payload: { actor_code: runtimeActor.code, x: 7, y: 7, z: 0 },
      expected_world_tick: 0, status: 'applied', processed_tick: 1, result: { actor_code: runtimeActor.code },
      submitted_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T01:00:00Z'
    })

    const store = useCitySpatialStore()
    await store.initialize(7)
    const result = await store.runWorldRuntimeCommand(
      'actor.location.move',
      { actor_code: runtimeActor.code, x: 7, y: 7, z: 0 },
      'move:east'
    )

    expect(result).toBe('queued')
    expect(apiMocks.submitWorldRuntimeCommand).toHaveBeenCalledWith(
      7,
      'actor.location.move',
      { actor_code: runtimeActor.code, x: 7, y: 7, z: 0 },
      0
    )
    expect(apiMocks.stepWorld).not.toHaveBeenCalled()
    expect(store.worldRuntimeCommandCode).toBeNull()
    await vi.waitFor(() => expect(store.worldCommandReceipts[0]?.status).toBe('applied'))
    expect(apiMocks.getCommand).toHaveBeenCalledWith(7, 23)
    expect(store.activeWorld?.current_tick).toBe(1)
  })

  it('manages active world members by exact identity while preserving the loaded simulation state', async () => {
    configureReadAPI()
    const added: CityMember = {
      user_id: 3, email: 'viewer@example.com', username: 'viewer', role: 'viewer', status: 'active',
      joined_at: '2026-01-02T00:00:00Z', updated_at: '2026-01-02T00:00:00Z'
    }
    apiMocks.addWorldMember.mockResolvedValue(added)
    apiMocks.updateWorldMember.mockResolvedValue({
      ...added, status: 'left', left_at: '2026-01-03T00:00:00Z', updated_at: '2026-01-03T00:00:00Z'
    })
    const store = useCitySpatialStore()
    await store.initialize(7)
    const visibleScene = store.projectedChunks

    await store.addWorldMember({ identity: 'viewer@example.com', role: 'viewer' })
    expect(apiMocks.addWorldMember).toHaveBeenCalledWith(7, {
      identity: 'viewer@example.com', role: 'viewer'
    })
    expect(store.worldMembers.some(member => member.user_id === 3)).toBe(true)

    await store.updateWorldMember(3, { status: 'left' })
    expect(apiMocks.updateWorldMember).toHaveBeenCalledWith(7, 3, { status: 'left' })
    expect(store.worldMembers.some(member => member.user_id === 3)).toBe(false)
    expect(store.projectedChunks).toBe(visibleScene)
    expect(store.worldMemberMutationKey).toBeNull()
  })

  it('focuses an authoritative actor location without replacing cached Chunk projections', async () => {
    configureReadAPI()
    apiMocks.getWorldRuntimeCatalog.mockResolvedValue(runtimeCatalog)
    apiMocks.listWorldActors.mockResolvedValue([runtimeActor])
    apiMocks.getWorldActorState.mockResolvedValue(runtimeActorState(16000, 1))
    apiMocks.getWorldActorRoleOptions.mockResolvedValue(runtimeRoleOptions)
    apiMocks.listWorldRuntimeRules.mockResolvedValue([])
    apiMocks.listWorldRuleCases.mockResolvedValue({ items: [] })

    const store = useCitySpatialStore()
    await store.initialize(7)
    const visibleScene = store.projectedChunks
    await store.focusWorldActor(runtimeActor.code)

    expect(store.mapMode).toBe('local')
    expect(store.camera).toMatchObject({ worldX: 6, worldY: 7, z: 0 })
    expect(store.selectedCoordinate).toEqual({ worldX: 6, worldY: 7, z: 0 })
    expect(store.projectedChunks).toBe(visibleScene)
  })

  it('previews a bounded server-authoritative actor route and invalidates stale results on map selection', async () => {
    configureReadAPI()
    apiMocks.getWorldRuntimeCatalog.mockResolvedValue(runtimeCatalog)
    apiMocks.listWorldActors.mockResolvedValue([runtimeActor])
    apiMocks.getWorldActorState.mockResolvedValue(runtimeActorState(16000, 1))
    apiMocks.getWorldActorRoleOptions.mockResolvedValue(runtimeRoleOptions)
    apiMocks.listWorldRuntimeRules.mockResolvedValue([])
    apiMocks.listWorldRuleCases.mockResolvedValue({ items: [] })
    const path: CityNavigationPath = {
      navigation_version: '1.0.0', world_tick: 0, spatial_rule_hash: 'a'.repeat(64),
      actor_code: runtimeActor.code,
      from: { x: 6, y: 7, z: 0 }, to: { x: 8, y: 7, z: 0 }, reachable: true,
      total_cost: 160, expanded_nodes: 3,
      steps: [
        { coordinate: { x: 6, y: 7, z: 0 }, step_cost: 0, total_cost: 0, anchor_kind: 'chunk', anchor_code: 'chunk.z0.x0.y0', jurisdiction_code: 'central' },
        { coordinate: { x: 7, y: 7, z: 0 }, step_cost: 80, total_cost: 80, anchor_kind: 'chunk', anchor_code: 'chunk.z0.x0.y0', jurisdiction_code: 'central' },
        { coordinate: { x: 8, y: 7, z: 0 }, step_cost: 80, total_cost: 160, anchor_kind: 'chunk', anchor_code: 'chunk.z0.x0.y0', jurisdiction_code: 'central' }
      ]
    }
    apiMocks.findWorldActorPath.mockResolvedValue(path)

    const store = useCitySpatialStore()
    await store.initialize(7)
    const result = await store.previewWorldActorPath({ x: 8, y: 7, z: 0 }, 32)

    expect(apiMocks.findWorldActorPath).toHaveBeenCalledWith(7, runtimeActor.code, { x: 8, y: 7, z: 0 }, 32)
    expect(result).toEqual(path)
    expect(store.navigationPath).toEqual(path)
    expect(store.navigationLoading).toBe(false)

    const selectedCell = store.projectedChunks.get('z:0/x:0/y:0')?.cells[9 * 32 + 9]
    expect(selectedCell).toBeTruthy()
    store.selectCell(selectedCell!)
    expect(store.navigationPath).toBeNull()
  })

  it('does not publish a navigation response after the request was explicitly cleared', async () => {
    configureReadAPI()
    apiMocks.getWorldRuntimeCatalog.mockResolvedValue(runtimeCatalog)
    apiMocks.listWorldActors.mockResolvedValue([runtimeActor])
    apiMocks.getWorldActorState.mockResolvedValue(runtimeActorState(16000, 1))
    apiMocks.getWorldActorRoleOptions.mockResolvedValue(runtimeRoleOptions)
    apiMocks.listWorldRuntimeRules.mockResolvedValue([])
    apiMocks.listWorldRuleCases.mockResolvedValue({ items: [] })
    let resolvePath!: (value: CityNavigationPath) => void
    apiMocks.findWorldActorPath.mockReturnValue(new Promise(resolve => { resolvePath = resolve }))

    const store = useCitySpatialStore()
    await store.initialize(7)
    const pending = store.previewWorldActorPath({ x: 8, y: 7, z: 0 })
    expect(store.navigationLoading).toBe(true)
    store.clearNavigationPath()
    resolvePath({
      navigation_version: '1.0.0', world_tick: 0, spatial_rule_hash: 'a'.repeat(64),
      actor_code: runtimeActor.code, from: { x: 6, y: 7, z: 0 }, to: { x: 8, y: 7, z: 0 },
      reachable: false, reason: 'unreachable', total_cost: 0, expanded_nodes: 4, steps: []
    })

    await expect(pending).resolves.toBeNull()
    expect(store.navigationPath).toBeNull()
    expect(store.navigationLoading).toBe(false)
  })
})
