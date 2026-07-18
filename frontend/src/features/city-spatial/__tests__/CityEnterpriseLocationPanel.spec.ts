import { afterEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import type { CityEnterpriseLocationState } from '@/api/citySpatial'
import zh from '@/i18n/locales/zh/common'
import CityEnterpriseLocationPanel from '../CityEnterpriseLocationPanel.vue'

function runtimeMessages(value: unknown): any {
  if (typeof value === 'string') return () => value
  if (!value || typeof value !== 'object') return value
  return Object.fromEntries(Object.entries(value).map(([key, child]) => [key, runtimeMessages(child)]))
}

const i18n = createI18n({ legacy: false, locale: 'zh', messages: { zh: runtimeMessages(zh) } })

const state: CityEnterpriseLocationState = {
  profile: {
    policy_id: 'sub2api-enterprise-location', policy_version: '1.0.0', policy_hash: 'a'.repeat(64),
    baseline_tick: 0, baseline_hash: 'b'.repeat(64), baseline_site_count: 2,
    site_count: 3, fact_count: 1, revision: 2
  },
  baseline_sites: [],
  sites: [
    {
      code: 'site_firm_a_headquarters', firm_entity_code: 'firm_a', district_code: 'central',
      building_code: 'building_central', pool_code: 'pool_central_commercial', site_type: 'headquarters',
      name: 'Firm A Headquarters', occupied_units: 5, is_primary: true, status: 'active',
      opened_tick: 0, last_changed_tick: 0, version: 1, metadata: {}
    },
    {
      code: 'site_firm_a_production', firm_entity_code: 'firm_a', district_code: 'central',
      building_code: 'plant_central', pool_code: 'pool_central_industrial', site_type: 'production',
      name: 'Firm A Plant', occupied_units: 6, is_primary: true, status: 'active',
      opened_tick: 0, last_changed_tick: 0, version: 1, metadata: {}
    },
    {
      code: 'enterprise_site_7', firm_entity_code: 'firm_a', district_code: 'central',
      building_code: 'building_central', pool_code: 'pool_central_commercial', site_type: 'office',
      name: 'Firm A Office', occupied_units: 2, is_primary: false, status: 'active',
      opened_tick: 1, last_changed_tick: 1, version: 1, metadata: {}
    }
  ],
  facts: [{
    tick: 1, sequence: 1, source_command_sequence: 7, firm_entity_code: 'firm_a',
    site_code: 'enterprise_site_7', fact_type: 'opened', to_status: 'active',
    occupied_before_units: 0, occupied_after_units: 2, site_version_before: 0,
    site_version_after: 1, metadata: {}
  }],
  firms: [{
    entity_id: 42, entity_code: 'firm_a', entity_name: 'Firm A', district_code: 'central',
    employee_units: 20, capital_stock_units: 30, production_capacity_units: 12, active_site_count: 3
  }],
  pools: [
    {
      code: 'pool_central_commercial', building_code: 'building_central', district_code: 'central',
      use_type: 'commercial', effective_unit_count: 30, occupied_unit_count: 7, available_unit_count: 23
    },
    {
      code: 'pool_north_commercial', building_code: 'building_north', district_code: 'north',
      use_type: 'commercial', effective_unit_count: 40, occupied_unit_count: 0, available_unit_count: 40
    },
    {
      code: 'pool_north_industrial', building_code: 'plant_north', district_code: 'north',
      use_type: 'industrial', effective_unit_count: 40, occupied_unit_count: 0, available_unit_count: 40
    }
  ]
}

afterEach(() => {
  document.body.innerHTML = ''
})

describe('CityEnterpriseLocationPanel', () => {
  it('renders server-derived sites and emits an exact resize command', async () => {
    const wrapper = mount(CityEnterpriseLocationPanel, {
      props: { state, owner: true, busyCommandCode: null },
      global: { plugins: [i18n] },
      attachTo: document.body
    })

    expect(wrapper.text()).toContain('Firm A Office')
    expect(wrapper.text()).toContain('T1.1')
    const resizeButtons = wrapper.findAll('button').filter(button => button.text() === '调整占用')
    await resizeButtons[2]!.trigger('click')
    const input = document.body.querySelector<HTMLInputElement>('input[type="number"]')!
    input.value = '4'
    input.dispatchEvent(new Event('input'))
    await wrapper.vm.$nextTick()
    const confirm = Array.from(document.body.querySelectorAll('button')).find(button => button.textContent?.includes('确认并提交')) as HTMLButtonElement
    confirm.click()
    await wrapper.vm.$nextTick()

    expect(wrapper.emitted('command')?.[0]?.[0]).toEqual({
      commandType: 'enterprise.site.resize',
      payload: { site_code: 'enterprise_site_7', target_occupied_units: 4 },
      commandCode: 'enterprise_site_7'
    })
  })

  it('offers only districts with both required pool uses and emits deterministic relocation intent', async () => {
    const wrapper = mount(CityEnterpriseLocationPanel, {
      props: { state, owner: true, busyCommandCode: null },
      global: { plugins: [i18n] },
      attachTo: document.body
    })
    const relocate = wrapper.findAll('button').find(button => button.text() === '跨区迁址')!
    expect(relocate.attributes('disabled')).toBeUndefined()
    await relocate.trigger('click')
    const textarea = document.body.querySelector<HTMLTextAreaElement>('textarea')!
    textarea.value = 'Move to the northern district'
    textarea.dispatchEvent(new Event('input'))
    await wrapper.vm.$nextTick()
    const confirm = Array.from(document.body.querySelectorAll('button')).find(button => button.textContent?.includes('确认并提交')) as HTMLButtonElement
    confirm.click()
    await wrapper.vm.$nextTick()

    expect(wrapper.emitted('command')?.[0]?.[0]).toEqual({
      commandType: 'enterprise.relocate',
      payload: {
        firm_entity_id: 42,
        headquarters_pool_code: 'pool_north_commercial',
        production_pool_code: 'pool_north_industrial',
        reason: 'Move to the northern district'
      },
      commandCode: 'firm_a'
    })
  })
})
