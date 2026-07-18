import { afterEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import type { CityDevelopmentState, CityLandState } from '@/api/citySpatial'
import zh from '@/i18n/locales/zh/common'
import CityDevelopmentPanel from '../CityDevelopmentPanel.vue'

function runtimeMessages(value: unknown): any {
  if (typeof value === 'string') return () => value
  if (!value || typeof value !== 'object') return value
  return Object.fromEntries(Object.entries(value).map(([key, child]) => [key, runtimeMessages(child)]))
}

const i18n = createI18n({ legacy: false, locale: 'zh', messages: { zh: runtimeMessages(zh) } })

const landState: CityLandState = {
  profile: {
    rule_set_id: 'sub2api-land', rule_set_version: '1.0.0', rule_set_hash: 'a'.repeat(64),
    spatial_overmap_root_hash: 'b'.repeat(64), nominal_cell_area_sqm: 1500,
    baseline_hash: 'c'.repeat(64), baseline_tick: 0, zoning_rule_count: 1,
    parcel_count: 1, building_count: 1, unit_pool_count: 1,
    housing_allocation_count: 0, portal_count: 1, revision: 1
  },
  zoning_rules: [{
    code: 'residential', name: 'Residential', primary_use: 'residential',
    max_floor_area_ratio_milli: 4000, max_coverage_milli: 500,
    max_floors: 8, sqm_per_capacity_unit: 50
  }],
  parcels: [{
    code: 'parcel_central', district_code: 'central', zone_code: 'residential',
    geometry: { chunk_x: 0, chunk_y: 0, z: 0, local_min_x: 0, local_min_y: 0, local_max_x: 10, local_max_y: 10 },
    area_sqm: 1000, developable_area_sqm: 1000, status: 'active', version: 1
  }],
  buildings: [{
    code: 'building_central', parcel_code: 'parcel_central', district_code: 'central',
    primary_use: 'residential',
    footprint: { chunk_x: 0, chunk_y: 0, z: 0, local_min_x: 2, local_min_y: 2, local_max_x: 8, local_max_y: 8 },
    base_z: 0, top_z: 1, floor_count: 2, footprint_area_sqm: 200,
    floor_area_sqm: 400, capacity_units: 8, occupied_units: 4,
    quality_milli: 1000, status: 'active', completed_tick: 0, version: 1
  }],
  unit_pools: [], housing_allocations: [], portals: []
}

const developmentState: CityDevelopmentState = {
  profile: {
    policy_id: 'sub2api-development', policy_version: '1.0.0', policy_hash: 'd'.repeat(64),
    baseline_tick: 0, baseline_hash: 'e'.repeat(64), project_count: 1,
    fact_count: 1, adjustment_count: 0, revision: 1
  },
  projects: [{
    code: 'development_7', name: 'Central Extension', project_type: 'vertical_expansion',
    district_code: 'central', parcel_code: 'parcel_central', building_code: 'building_central',
    primary_use: 'residential', developer_entity_code: 'firm_central', target_floor_count: 3,
    added_floor_count: 1, added_floor_area_sqm: 200, added_capacity_units: 4,
    quality_delta_milli: 0, required_basic_material_units: 20,
    required_capital_goods_units: 2, required_labor_units: 4,
    planned_duration_ticks: 2, status: 'submitted', progress_milli: 0,
    submitted_tick: 1, version: 1, metadata: {}
  }],
  facts: [], adjustments: [],
  developers: [{
    entity_id: 42, entity_code: 'firm_central', entity_name: 'Central Works',
    district_code: 'central', employee_units: 20, reserved_labor_units: 0,
    available_labor_units: 20
  }]
}

afterEach(() => {
  document.body.innerHTML = ''
})

describe('CityDevelopmentPanel', () => {
  it('renders server-derived project values and emits the exact approval command', async () => {
    const wrapper = mount(CityDevelopmentPanel, {
      props: {
        state: developmentState,
        landState,
        selectedBuildingCode: 'building_central',
        owner: true,
        busyProjectCode: null
      },
      global: { plugins: [i18n] },
      attachTo: document.body
    })

    expect(wrapper.text()).toContain('Central Extension')
    expect(wrapper.text()).toContain('20')
    expect(wrapper.text()).toContain('待审批')
    const approve = wrapper.findAll('button').find(button => button.text() === '批准')
    expect(approve).toBeTruthy()
    await approve!.trigger('click')
    expect(wrapper.emitted('command')?.[0]?.[0]).toEqual({
      commandType: 'development.review',
      payload: { project_code: 'development_7', decision: 'approve' },
      projectCode: 'development_7'
    })
  })

  it('requires a selected building before opening a new deterministic project', () => {
    const wrapper = mount(CityDevelopmentPanel, {
      props: {
        state: developmentState,
        landState,
        selectedBuildingCode: null,
        owner: true,
        busyProjectCode: null
      },
      global: { plugins: [i18n] }
    })
    const create = wrapper.findAll('button').find(button => button.text().includes('提交开发申请'))
    expect(create?.attributes('disabled')).toBeDefined()
  })
})
