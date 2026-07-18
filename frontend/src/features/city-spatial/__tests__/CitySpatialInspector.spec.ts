import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import type { CityEnterpriseLocationState, CityLandState, CityOvermapTile, CitySpatialRuleSet } from '@/api/citySpatial'
import CitySpatialInspector from '../CitySpatialInspector.vue'

const message = (value: string) => () => value
const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      citySpatial: {
        inspector: {
          title: message('Inspector'), eyebrow: message('Spatial facts'), chunk: message('Chunk coordinate'),
          district: message('District'), terrain: message('Base terrain'), variant: message('Variant'),
          roadMask: message('Roads'), riverMask: message('Rivers'), none: message('None'), state: message('State'),
          generated: message('Generated'), parcels: message('Parcels'), buildings: message('Buildings'),
          zoning: message('Zoning'), floorSummary: ({ named }: any) => `${named('count')} floors · ${named('capacity')} capacity`,
          tileHash: message('Tile hash'), worldCoordinate: message('World coordinate'),
          localCoordinate: message('Local coordinate'), landStack: message('Land facts'), parcel: message('Parcel'),
          area: message('Area'), version: message('Version'), building: message('Building'), floors: message('Floors'),
          floorArea: message('Floor area'), occupancy: message('Occupancy'), quality: message('Quality'),
          allocations: ({ named }: any) => `${named('count')} housing allocations`,
          unavailableTitle: message('Unavailable'), unavailableDescription: message('Chunk unavailable')
        },
        landUse: { residential: message('Residential') },
        enterprise: {
          siteType: { headquarters: message('Headquarters') },
          inspector: { tileSites: message('Regional enterprise sites'), poolCapacity: ({ named }: any) => `Pool ${named('occupied')} / ${named('effective')}` }
        },
        portalType: { entrance: message('Entrance') }
      }
    }
  }
})

const tile: CityOvermapTile = {
  chunk_x: 0, chunk_y: 0, z: 0, district_code: 'central',
  terrain_definition_id: 'terrain.ground', road_mask: 0, river_mask: 0,
  variant: 0, tile_hash: 'tile-hash', metadata: {}
}

const ruleSet: CitySpatialRuleSet = {
  id: 'rules', version: '1', name: 'Rules', chunk_size: 32, min_z: -2, max_z: 2,
  content_hash: 'a'.repeat(64), palette: [], definitions: [{
    id: 'terrain.ground', kind: 'terrain', name: 'Ground', glyph: '.', foreground: 'ground',
    movement_cost: 100, flags: ['passable'], metadata: {}
  }]
}

const landState: CityLandState = {
  profile: {
    rule_set_id: 'sub2api-land', rule_set_version: '1.0.0', rule_set_hash: 'b'.repeat(64),
    spatial_overmap_root_hash: 'c'.repeat(64), nominal_cell_area_sqm: 1500,
    baseline_hash: 'd'.repeat(64), baseline_tick: 0, zoning_rule_count: 3,
    parcel_count: 1, building_count: 1, unit_pool_count: 1,
    housing_allocation_count: 1, portal_count: 1, revision: 1
  },
  zoning_rules: [],
  parcels: [{
    code: 'parcel_central', district_code: 'central', zone_code: 'residential',
    geometry: { chunk_x: 0, chunk_y: 0, z: 0, local_min_x: 1, local_min_y: 1, local_max_x: 12, local_max_y: 12 },
    area_sqm: 12000, developable_area_sqm: 12000, status: 'active', version: 1
  }],
  buildings: [{
    code: 'building_central', parcel_code: 'parcel_central', district_code: 'central', primary_use: 'residential',
    footprint: { chunk_x: 0, chunk_y: 0, z: 0, local_min_x: 4, local_min_y: 4, local_max_x: 9, local_max_y: 9 },
    base_z: 0, top_z: 1, floor_count: 2, footprint_area_sqm: 5000, floor_area_sqm: 10000,
    capacity_units: 40, occupied_units: 10, quality_milli: 1000,
    status: 'active', completed_tick: 0, version: 1
  }],
  unit_pools: [{
    code: 'pool_building_central', building_code: 'building_central', district_code: 'central',
    use_type: 'residential', unit_count: 40, occupied_unit_count: 10,
    capacity_units_per_unit: 1, version: 1
  }],
  housing_allocations: [{
    pool_code: 'pool_building_central', district_code: 'central',
    cohort_key: 'central/household/medium', allocated_units: 10, status: 'active', version: 1
  }],
  portals: [{
    code: 'entrance', building_code: 'building_central', district_code: 'central', portal_type: 'entrance',
    from_x: 3, from_y: 6, from_z: 0, to_x: 4, to_y: 6, to_z: 0,
    bidirectional: true, status: 'active', version: 1
  }]
}

const enterpriseState: CityEnterpriseLocationState = {
  profile: {
    policy_id: 'enterprise', policy_version: '1', policy_hash: 'e'.repeat(64), baseline_tick: 0,
    baseline_hash: 'f'.repeat(64), baseline_site_count: 1, site_count: 1, fact_count: 0, revision: 1
  },
  baseline_sites: [],
  sites: [{
    code: 'site_firm_central_headquarters', firm_entity_code: 'firm_central', district_code: 'central',
    building_code: 'building_central', pool_code: 'pool_building_central', site_type: 'headquarters',
    name: 'Central Firm Headquarters', occupied_units: 5, is_primary: true, status: 'active',
    opened_tick: 0, last_changed_tick: 0, version: 1, metadata: {}
  }],
  facts: [], firms: [],
  pools: [{
    code: 'pool_building_central', building_code: 'building_central', district_code: 'central',
    use_type: 'commercial', effective_unit_count: 40, occupied_unit_count: 5, available_unit_count: 35
  }]
}

const baseProps = {
  tile,
  coordinate: null,
  cell: null,
  chunk: null,
  ruleSet,
  landState,
  chunkSize: 32,
  generated: true
}

describe('CitySpatialInspector', () => {
  it('shows the selected Overmap tile land-use and building facts', () => {
    const wrapper = mount(CitySpatialInspector, {
      props: { ...baseProps, mode: 'overmap' },
      global: { plugins: [i18n], stubs: { Icon: true } }
    })

    expect(wrapper.text()).toContain('Parcels')
    expect(wrapper.text()).toContain('Buildings')
    expect(wrapper.text()).toContain('Residential')
    expect(wrapper.text()).toContain('building_central')
    expect(wrapper.text()).toContain('2 floors · 40 capacity')
  })

  it('shows the complete parcel, building, occupancy, pool, allocation, and portal chain', () => {
    const wrapper = mount(CitySpatialInspector, {
      props: {
        ...baseProps,
        mode: 'local',
        tile: null,
        coordinate: { worldX: 4, worldY: 6, z: 0 }
      },
      global: { plugins: [i18n], stubs: { Icon: true } }
    })

    const text = wrapper.text()
    expect(text).toContain('parcel_central')
    expect(text).toContain('building_central')
    expect(text).toContain('10 / 40')
    expect(text).toContain('pool_building_central')
    expect(text).toContain('central/household/medium')
    expect(text).toContain('entrance')
    expect(text).toContain('3,6,0 → 4,6,0')
  })

  it('shows enterprise sites from the server location projection for tile and building inspection', () => {
    const overmap = mount(CitySpatialInspector, {
      props: { ...baseProps, mode: 'overmap', enterpriseLocationState: enterpriseState },
      global: { plugins: [i18n], stubs: { Icon: true } }
    })
    expect(overmap.text()).toContain('Regional enterprise sites')
    expect(overmap.text()).toContain('Central Firm Headquarters')

    const local = mount(CitySpatialInspector, {
      props: {
        ...baseProps,
        mode: 'local', tile: null, coordinate: { worldX: 4, worldY: 6, z: 0 },
        enterpriseLocationState: enterpriseState
      },
      global: { plugins: [i18n], stubs: { Icon: true } }
    })
    expect(local.text()).toContain('site_firm_central_headquarters')
    expect(local.text()).toContain('Pool 5 / 40')
  })
})
