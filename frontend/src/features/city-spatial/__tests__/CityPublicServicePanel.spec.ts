import { afterEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import type {
  CityLandState,
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
  CityServiceSettlementPage
} from '@/api/citySpatial'
import zh from '@/i18n/locales/zh/common'
import CityPublicServicePanel from '../CityPublicServicePanel.vue'

function runtimeMessages(value: unknown): any {
  if (typeof value === 'string') return () => value
  if (!value || typeof value !== 'object') return value
  return Object.fromEntries(Object.entries(value).map(([key, child]) => [key, runtimeMessages(child)]))
}

const i18n = createI18n({ legacy: false, locale: 'zh', messages: { zh: runtimeMessages(zh) } })

const catalog: CityServiceCatalogView = {
  availability: 'available', simulation_version: 'city-f8-v1', required_version: 'city-f8-v1',
  profile: {
    catalog_id: 'sub2api-public-services', catalog_version: '1.0.0', catalog_hash: 'a'.repeat(64),
    settlement_version: '1.0.0', baseline_tick: 0, service_definition_count: 1,
    facility_type_count: 1, facility_count: 1, capacity_count: 1, demand_count: 1,
    connection_count: 1, fact_count: 6, allocation_count: 1, settlement_count: 1,
    revision: 7, metadata: {}
  },
  overview: {
    facility_count: 1, operational_facility_count: 1, active_capacity_count: 1,
    dispatch_capacity_units: '950', active_demand_count: 1, requested_units_per_tick: '800',
    latest_settlement_tick: 6, latest_requested_units: '800', latest_delivered_units: '784',
    latest_shortage_units: '16', latest_weighted_quality_milli: 980
  },
  service_definitions: [{
    code: 'electric_power', definition_version: '1.0.0', definition_hash: 'b'.repeat(64),
    name: 'Electric power', category: 'utility', unit_code: 'energy_unit', flow_kind: 'delivery',
    status: 'active', sort_order: 10, payload: {}
  }],
  facility_types: [{
    code: 'power_plant', definition_version: '1.0.0', definition_hash: 'c'.repeat(64),
    name: 'Power plant', minimum_floor_area_sqm: 1, default_reliability_milli: 950,
    allowed_service_codes: ['electric_power'], status: 'active', sort_order: 20, payload: {}
  }]
}

const facilities: CityServiceFacilityPage = {
  availability: 'available', simulation_version: 'city-f8-v1', required_version: 'city-f8-v1',
  items: [{
    facility: {
      code: 'facility_central_power', name: 'Central Power', facility_type_code: 'power_plant',
      facility_type_version: '1.0.0', facility_type_hash: 'c'.repeat(64), district_code: 'central',
      building_code: 'building_central', status: 'operational', reliability_milli: 950,
      created_tick: 1, updated_tick: 3, version: 2, source_fact_tick: 3,
      source_fact_sequence: 1, metadata: {}
    },
    capacities: [{
      facility_code: 'facility_central_power', service_code: 'electric_power', service_version: '1.0.0',
      service_hash: 'b'.repeat(64), installed_capacity_units: 1000, availability_milli: 950,
      available_capacity_units: 950, dispatch_capacity_units: 950, updated_tick: 2, version: 1,
      source_fact_tick: 2, source_fact_sequence: 1, metadata: {}
    }]
  }]
}

const demands: CityServiceDemandPage = {
  availability: 'available', simulation_version: 'city-f8-v1', required_version: 'city-f8-v1',
  items: [{
    demand: {
      code: 'demand_central_power', service_code: 'electric_power', service_version: '1.0.0',
      service_hash: 'b'.repeat(64), subject_kind: 'district', subject_code: 'central',
      district_code: 'central', requested_units_per_tick: 800, priority: 800, status: 'active',
      created_tick: 4, updated_tick: 4, version: 1, source_fact_tick: 4,
      source_fact_sequence: 1, metadata: {}
    },
    latest_settlement: {
      tick: 6, sequence: 1, service_code: 'electric_power', demand_code: 'demand_central_power',
      demand_version: 1, requested_units: 800, delivered_units: 784, shortage_units: 16,
      allocation_count: 1, quality_milli: 980, metadata: {}
    }
  }]
}

const connections: CityServiceConnectionPage = {
  availability: 'available', simulation_version: 'city-f8-v1', required_version: 'city-f8-v1',
  items: [{
    code: 'connection_central_power', facility_code: 'facility_central_power', service_code: 'electric_power',
    demand_code: 'demand_central_power', max_flow_units_per_tick: 800, loss_milli: 20,
    preference: 900, status: 'active', created_tick: 5, updated_tick: 5, version: 1,
    source_fact_tick: 5, source_fact_sequence: 1, metadata: {}
  }]
}

const settlements: CityServiceSettlementPage = {
  availability: 'available', simulation_version: 'city-f8-v1', required_version: 'city-f8-v1',
  items: [{
    settlement: {
      tick: 6, sequence: 1, service_code: 'electric_power', demand_code: 'demand_central_power',
      demand_version: 1, requested_units: 800, delivered_units: 784, shortage_units: 16,
      allocation_count: 1, quality_milli: 980, metadata: {}
    },
    allocations: [{
      tick: 6, sequence: 1, allocation_index: 1, service_code: 'electric_power',
      facility_code: 'facility_central_power', demand_code: 'demand_central_power',
      connection_code: 'connection_central_power', capacity_version: 1, demand_version: 1,
      connection_version: 1, facility_capacity_units: 950, connection_capacity_units: 800,
      loss_milli: 20, dispatched_units: 800, delivered_units: 784, loss_units: 16, metadata: {}
    }]
  }],
  next_cursor: { tick: 6, sequence: 1 }
}

const physicalNetworkCatalog: CityPhysicalNetworkCatalogView = {
  availability: 'available', simulation_version: 'city-f8-v3', required_version: 'city-f8-v3',
  profile: {
    policy_id: 'sub2api-physical-networks', policy_version: '1.0.0', policy_hash: 'd'.repeat(64),
    baseline_tick: 0, policy_count: 1, network_count: 1, node_count: 2, edge_count: 1,
    fact_count: 2, batch_count: 1, path_count: 1, segment_count: 1, revision: 4, metadata: {}
  },
  overview: {
    active_network_count: 1, active_node_count: 2, active_edge_count: 1,
    isolated_edge_count: 0, failed_edge_count: 0, installed_edge_capacity_units: '900',
    available_edge_capacity_units: '855', latest_flow_tick: 6, latest_dispatched_units: '800',
    latest_network_received_units: '784', latest_network_loss_units: '16', latest_delivery_ratio_milli: 980
  },
  policies: [{
    service_code: 'electric_power', policy_version: '1.0.0', policy_hash: 'e'.repeat(64),
    network_required: true, route_direction: 'supply_to_demand', maximum_nodes: 100,
    maximum_edges: 200, maximum_paths: 8, maximum_hops: 32, loss_cost_weight: 5,
    allow_bidirectional: true, algorithm_version: 'integer-dijkstra-v1', payload: {}
  }]
}

const physicalNetworks: CityPhysicalNetworkPage = {
  availability: 'available', simulation_version: 'city-f8-v3', required_version: 'city-f8-v3',
  items: [{
    code: 'grid_central', name: 'Central Grid', service_code: 'electric_power', status: 'active',
    topology_revision: 3, created_tick: 1, updated_tick: 5, version: 2,
    source_fact_tick: 5, source_fact_sequence: 1, metadata: { baseline_mode: 'explicit' }
  }]
}

const physicalNodes: CityPhysicalNetworkNodePage = {
  availability: 'available', simulation_version: 'city-f8-v3', required_version: 'city-f8-v3',
  items: [{
    code: 'supply_power', network_code: 'grid_central', role: 'supply',
    capacity_code: 'facility_central_power.electric_power', world_x: 1, world_y: 1, world_z: 0,
    status: 'active', created_tick: 1, updated_tick: 2, version: 1,
    source_fact_tick: 2, source_fact_sequence: 1, metadata: {}
  }, {
    code: 'demand_power', network_code: 'grid_central', role: 'demand',
    demand_code: 'demand_central_power', world_x: 10, world_y: 8, world_z: 0,
    status: 'active', created_tick: 1, updated_tick: 2, version: 1,
    source_fact_tick: 2, source_fact_sequence: 2, metadata: {}
  }]
}

const physicalEdges: CityPhysicalNetworkEdgePage = {
  availability: 'available', simulation_version: 'city-f8-v3', required_version: 'city-f8-v3',
  items: [{
    code: 'line_power', network_code: 'grid_central', from_node_code: 'supply_power',
    to_node_code: 'demand_power', direction: 'directed', installed_capacity_units: 900,
    availability_milli: 950, available_capacity_units: 855, loss_milli: 20, base_cost_units: 1,
    status: 'active', condition_milli: 1000, failure_count: 0, created_tick: 1,
    updated_tick: 5, version: 2, source_fact_tick: 5, source_fact_sequence: 2, metadata: {}
  }]
}

const physicalFlows: CityPhysicalNetworkFlowPage = {
  availability: 'available', simulation_version: 'city-f8-v3', required_version: 'city-f8-v3',
  items: [{
    batch: {
      tick: 6, sequence: 2, network_code: 'grid_central', service_code: 'electric_power',
      topology_revision: 3, allocation_count: 1, path_count: 1, segment_count: 1,
      dispatched_units: 800, network_received_units: 784, network_loss_units: 16,
      source_fact_tick: 6, source_fact_sequence: 2, metadata: {}
    },
    paths: [{
      tick: 6, sequence: 2, service_sequence: 1, allocation_index: 1, path_index: 1,
      network_code: 'grid_central', connection_code: 'connection_central_power',
      source_node_code: 'supply_power', sink_node_code: 'demand_power', hop_count: 1,
      dispatched_units: 800, network_received_units: 784, network_loss_units: 16,
      path_cost_units: 101, path_hash: 'f'.repeat(64), metadata: {}
    }],
    segments: [{
      tick: 6, sequence: 2, service_sequence: 1, allocation_index: 1, path_index: 1,
      segment_index: 1, edge_code: 'line_power', edge_version: 2, direction: 'forward',
      from_node_code: 'supply_power', to_node_code: 'demand_power', edge_capacity_units: 855,
      loss_milli: 20, input_units: 800, output_units: 784, loss_units: 16, metadata: {}
    }]
  }]
}

const physicalFacts: CityPhysicalNetworkFactPage = {
  availability: 'available', simulation_version: 'city-f8-v3', required_version: 'city-f8-v3',
  items: [{
    tick: 6, sequence: 2, phase: 'settlement', fact_type: 'network.flow_settled',
    subject_kind: 'flow_batch', subject_code: 'grid_central', version_before: 0,
    version_after: 1, payload: { schema_version: 1 }
  }]
}

const physicalDiagnostics: CityPhysicalNetworkDiagnosticsView = {
  availability: 'available', simulation_version: 'city-f8-v3', required_version: 'city-f8-v3',
  network: physicalNetworks.items[0], policy: physicalNetworkCatalog.policies[0], latest_flow_tick: 6,
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
  truncated_edge_diagnostic_count: 0,
  route: {
    source_node_code: 'supply_power', sink_node_code: 'demand_power', probe_units: 100,
    reachable: true, reason_code: 'reachable', dispatched_units: 100,
    network_received_units: 98, network_loss_units: 2, paths: []
  }
}

const landState: CityLandState = {
  profile: {
    rule_set_id: 'sub2api-land', rule_set_version: '1.0.0', rule_set_hash: 'd'.repeat(64),
    spatial_overmap_root_hash: 'e'.repeat(64), nominal_cell_area_sqm: 1500,
    baseline_hash: 'f'.repeat(64), baseline_tick: 0, zoning_rule_count: 1, parcel_count: 1,
    building_count: 1, unit_pool_count: 0, housing_allocation_count: 0, portal_count: 0, revision: 1
  },
  zoning_rules: [],
  parcels: [{
    code: 'parcel_central', district_code: 'central', zone_code: 'industrial',
    geometry: { chunk_x: 0, chunk_y: 0, z: 0, local_min_x: 0, local_min_y: 0, local_max_x: 10, local_max_y: 10 },
    area_sqm: 10000, developable_area_sqm: 10000, status: 'active', version: 1
  }],
  buildings: [{
    code: 'building_central', parcel_code: 'parcel_central', district_code: 'central', primary_use: 'industrial',
    footprint: { chunk_x: 0, chunk_y: 0, z: 0, local_min_x: 1, local_min_y: 1, local_max_x: 8, local_max_y: 8 },
    base_z: 0, top_z: 0, floor_count: 1, footprint_area_sqm: 1000, floor_area_sqm: 1000,
    capacity_units: 10, occupied_units: 0, quality_milli: 1000, status: 'active', completed_tick: 0, version: 1
  }],
  unit_pools: [], housing_allocations: [], portals: []
}

afterEach(() => { document.body.innerHTML = '' })

function mountPanel() {
  return mount(CityPublicServicePanel, {
    props: {
      catalog, facilities, demands, connections, settlements, availability: 'available',
      physicalNetworkCatalog, physicalNetworks, physicalNetworkNodes: physicalNodes,
      physicalNetworkEdges: physicalEdges, physicalNetworkFlows: physicalFlows,
      physicalNetworkFacts: physicalFacts, physicalNetworkDiagnostics: physicalDiagnostics,
      physicalNetworkAvailability: 'available',
      physicalNetworkLoading: false,
      landState, enterpriseState: null, actors: [], owner: true, loading: false, busyCommandCode: null
    },
    global: { plugins: [i18n] },
    attachTo: document.body
  })
}

describe('CityPublicServicePanel', () => {
  it('renders exact server aggregates and posts a CAS facility transition', async () => {
    const wrapper = mountPanel()
    expect(wrapper.text()).toContain('950')
    expect(wrapper.text()).toContain('784')
    expect(wrapper.text()).toContain('16')

    await wrapper.findAll('button').find(button => (
      button.attributes('role') === 'tab' && button.text().includes('设施')
    ))!.trigger('click')
    await wrapper.findAll('button').find(button => button.text() === '转换状态')!.trigger('click')
    const confirm = Array.from(document.body.querySelectorAll('button')).find(button => button.textContent?.includes('确认并封账')) as HTMLButtonElement
    confirm.click()
    await wrapper.vm.$nextTick()

    expect(wrapper.emitted('command')?.[0]?.[0]).toEqual({
      commandType: 'facility.status.transition',
      payload: {
        facility_code: 'facility_central_power', to_status: 'degraded',
        expected_version: 2, metadata: {}
      },
      commandCode: 'status:facility_central_power'
    })
  })

  it('keeps settlement data mounted and emits the exact next cursor', async () => {
    const wrapper = mountPanel()
    await wrapper.findAll('button').find(button => button.text().includes('结算'))!.trigger('click')
    expect(wrapper.text()).toContain('T6.1')
    expect(wrapper.text()).toContain('demand_central_power')

    await wrapper.findAll('button').find(button => button.text() === '继续载入')!.trigger('click')
    expect(wrapper.emitted('query')?.[0]?.[0]).toEqual({
      section: 'settlements',
      query: { limit: 100, after_tick: 6, after_sequence: 1 },
      append: true
    })
  })

  it('renders authoritative topology and posts an edge-state CAS command', async () => {
    const wrapper = mountPanel()
    await wrapper.findAll('button').find(button => button.text().includes('物理网络'))!.trigger('click')

    expect(wrapper.text()).toContain('Central Grid')
    expect(wrapper.text()).toContain('855')
    expect(wrapper.findAll('svg .city-network-node')).toHaveLength(2)
    expect(wrapper.findAll('svg .city-network-edge')).toHaveLength(1)

    await wrapper.findAll('button').find(button => button.text().includes('line_power'))!.trigger('click')
    await wrapper.findAll('button').find(button => button.text() === '转换状态')!.trigger('click')
    const confirm = Array.from(document.body.querySelectorAll('button')).find(button => button.textContent?.includes('确认并封账')) as HTMLButtonElement
    confirm.click()
    await wrapper.vm.$nextTick()

    expect(wrapper.emitted('command')?.at(-1)?.[0]).toEqual({
      commandType: 'network.edge.transition',
      payload: { edge_code: 'line_power', to_status: 'isolated', expected_version: 2, metadata: {} },
      commandCode: 'network-edge-status:line_power'
    })
  })

  it('renders authoritative diagnostics and emits an exact read-only route probe', async () => {
    const wrapper = mountPanel()
    await wrapper.findAll('button').find(button => button.text().includes('物理网络'))!.trigger('click')
    await wrapper.vm.$nextTick()

    expect(wrapper.emitted('network-diagnose')?.[0]?.[0]).toEqual({ network: 'grid_central' })
    await wrapper.findAll('button').find(button => button.text().includes('网络诊断'))!.trigger('click')
    expect(wrapper.text()).toContain('93.5%')
    expect(wrapper.text()).toContain('可达')

    const probe = wrapper.find('.city-network-diagnostic-probe')
    await probe.find('input[type="number"]').setValue(75)
    await probe.trigger('submit')
    expect(wrapper.emitted('network-diagnose')?.at(-1)?.[0]).toEqual({
      network: 'grid_central', source: 'supply_power', sink: 'demand_power', probe_units: 75
    })
  })
})
