import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post } = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }))

vi.mock('@/api/client', () => ({ apiClient: { get, post } }))

import {
  getCityPhysicalNetworkCatalog,
  getCityPhysicalNetworkDiagnostics,
  getCityServiceCatalog,
  listCityPhysicalNetworkEdges,
  listCityPhysicalNetworkFacts,
  listCityPhysicalNetworkFlows,
  listCityPhysicalNetworkNodes,
  listCityPhysicalNetworks,
  listCityServiceConnections,
  listCityServiceDemands,
  listCityServiceFacilities,
  listCityServiceSettlements,
  submitCityServiceCommand
} from '@/api/citySpatial'

describe('city public-service API', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
    get.mockResolvedValue({ data: { availability: 'available', items: [] } })
    post.mockResolvedValue({ data: { id: 41, status: 'pending' } })
  })

  it('uses scoped service endpoints and preserves exact filter cursors', async () => {
    await getCityServiceCatalog(7)
    await listCityServiceFacilities(7, { service: 'electric_power', after_code: 'facility_a', limit: 50 })
    await listCityServiceDemands(7, { district: 'central', status: 'active' })
    await listCityServiceConnections(7, { facility: 'facility_a', demand: 'demand_a' })
    await listCityServiceSettlements(7, { service: 'electric_power', after_tick: 9, after_sequence: 3 })

    expect(get).toHaveBeenNthCalledWith(1, '/city/worlds/7/services/catalog')
    expect(get).toHaveBeenNthCalledWith(2, '/city/worlds/7/services/facilities', {
      params: { limit: 50, service: 'electric_power', after_code: 'facility_a' }
    })
    expect(get).toHaveBeenNthCalledWith(3, '/city/worlds/7/services/demands', {
      params: { limit: 200, district: 'central', status: 'active' }
    })
    expect(get).toHaveBeenNthCalledWith(4, '/city/worlds/7/services/connections', {
      params: { limit: 200, facility: 'facility_a', demand: 'demand_a' }
    })
    expect(get).toHaveBeenNthCalledWith(5, '/city/worlds/7/services/settlements', {
      params: { after_tick: 9, after_sequence: 3, limit: 200, service: 'electric_power' }
    })
  })

  it('posts the exact versioned command with expected tick and an idempotency key', async () => {
    const payload = {
      facility_code: 'facility_a', service_code: 'electric_power',
      installed_capacity_units: 1000, availability_milli: 950,
      expected_version: 1, metadata: {}
    }

    await submitCityServiceCommand(7, 'facility.capacity.configure', payload, 12)

    expect(post).toHaveBeenCalledWith(
      '/city/worlds/7/commands',
      { command_type: 'facility.capacity.configure', payload, expected_world_tick: 12 },
      { headers: { 'Idempotency-Key': expect.stringContaining('city-service-7-facility.capacity.configure') } }
    )
  })

  it('uses physical-network topology, flow, fact, and diagnostic endpoints with exact queries', async () => {
    await getCityPhysicalNetworkCatalog(7)
    await listCityPhysicalNetworks(7, { service: 'electric_power', after_code: 'grid_a', limit: 25 })
    await listCityPhysicalNetworkNodes(7, { network: 'grid_a', role: 'supply' })
    await listCityPhysicalNetworkEdges(7, { network: 'grid_a', status: 'active' })
    await listCityPhysicalNetworkFlows(7, { network: 'grid_a', after_tick: 12, after_sequence: 4 })
    await listCityPhysicalNetworkFacts(7, { phase: 'command', fact_type: 'edge.configured', limit: 40 })
    await getCityPhysicalNetworkDiagnostics(7, { network: 'grid_a', source: 'source', sink: 'sink', probe_units: 75 })

    expect(get).toHaveBeenNthCalledWith(1, '/city/worlds/7/services/networks/catalog')
    expect(get).toHaveBeenNthCalledWith(2, '/city/worlds/7/services/networks', {
      params: { limit: 25, service: 'electric_power', after_code: 'grid_a' }
    })
    expect(get).toHaveBeenNthCalledWith(3, '/city/worlds/7/services/networks/nodes', {
      params: { limit: 200, network: 'grid_a', role: 'supply' }
    })
    expect(get).toHaveBeenNthCalledWith(4, '/city/worlds/7/services/networks/edges', {
      params: { limit: 200, network: 'grid_a', status: 'active' }
    })
    expect(get).toHaveBeenNthCalledWith(5, '/city/worlds/7/services/networks/flows', {
      params: { after_tick: 12, after_sequence: 4, limit: 100, network: 'grid_a' }
    })
    expect(get).toHaveBeenNthCalledWith(6, '/city/worlds/7/services/networks/facts', {
      params: { after_tick: 0, after_sequence: 0, limit: 40, phase: 'command', fact_type: 'edge.configured' }
    })
    expect(get).toHaveBeenNthCalledWith(7, '/city/worlds/7/services/networks/diagnostics', {
      params: { network: 'grid_a', source: 'source', sink: 'sink', probe_units: 75 }
    })
  })
})
