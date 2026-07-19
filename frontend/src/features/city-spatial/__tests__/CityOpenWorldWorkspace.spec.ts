import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h, nextTick } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import type {
  CityOpenWorldBuildingInterior,
  CityOpenWorldGenerationState,
  CityOpenWorldMap,
  CitySpatialRuleSet,
  CityWorld
} from '@/api/citySpatial'
import zh from '@/i18n/locales/zh/common'

const {
  getCitySpatialRuleSet,
  getOpenWorldBuildingInterior,
  getOpenWorldGeneration,
  getOpenWorldMap,
	getOpenWorldVerification,
  listOpenWorldBuildingPortals
} = vi.hoisted(() => ({
  getCitySpatialRuleSet: vi.fn(),
  getOpenWorldBuildingInterior: vi.fn(),
  getOpenWorldGeneration: vi.fn(),
  getOpenWorldMap: vi.fn(),
	getOpenWorldVerification: vi.fn(),
  listOpenWorldBuildingPortals: vi.fn()
}))

vi.mock('@/api/citySpatial', async () => {
  const actual = await vi.importActual<typeof import('@/api/citySpatial')>('@/api/citySpatial')
  return {
    ...actual,
    getCitySpatialRuleSet,
    getOpenWorldBuildingInterior,
    getOpenWorldGeneration,
    getOpenWorldMap,
	getOpenWorldVerification,
    listOpenWorldBuildingPortals
  }
})

import CityOpenWorldWorkspace from '../CityOpenWorldWorkspace.vue'

function runtimeMessages(value: unknown): any {
  if (typeof value === 'string') return () => value
  if (!value || typeof value !== 'object') return value
  return Object.fromEntries(Object.entries(value).map(([key, child]) => [key, runtimeMessages(child)]))
}

const i18n = createI18n({ legacy: false, locale: 'zh', messages: { zh: runtimeMessages(zh) } })

const ruleSet: CitySpatialRuleSet = {
  id: 'sub2api-classic', version: '1.0.0', name: 'CLASSIC', chunk_size: 32,
  min_z: -32, max_z: 127, content_hash: 'a'.repeat(64),
  palette: [
    { id: 'ground', name: 'Ground', classic_foreground: 244, classic_background: 234 },
    { id: 'accent', name: 'Accent', classic_foreground: 75, classic_background: 234 }
  ],
  definitions: [
    { id: 'missing.terrain', kind: 'terrain', name: 'Unknown terrain', glyph: '?', foreground: 'accent', movement_cost: 0, flags: [], metadata: {} },
    { id: 'missing.structure', kind: 'structure', name: 'Unknown structure', glyph: '?', foreground: 'accent', movement_cost: 0, flags: [], metadata: {} },
    { id: 'missing.furniture', kind: 'furniture', name: 'Unknown furniture', glyph: '?', foreground: 'accent', movement_cost: 0, flags: [], metadata: {} },
    { id: 'missing.portal', kind: 'portal', name: 'Unknown portal', glyph: '?', foreground: 'accent', movement_cost: 0, flags: [], metadata: {} },
    { id: 'missing.item', kind: 'item', name: 'Unknown item', glyph: '?', foreground: 'accent', movement_cost: 0, flags: [], metadata: {} },
    { id: 'terrain.floor', kind: 'terrain', name: 'Floor', glyph: '.', foreground: 'ground', movement_cost: 100, flags: ['passable'], metadata: {} },
    { id: 'structure.wall', kind: 'structure', name: 'Wall', glyph: '#', foreground: 'ground', movement_cost: 0, flags: [], metadata: {} },
    { id: 'structure.window', kind: 'structure', name: 'Window', glyph: '□', foreground: 'accent', movement_cost: 0, flags: [], metadata: {} },
    { id: 'portal.door_open', kind: 'portal', name: 'Door', glyph: '/', foreground: 'accent', movement_cost: 100, flags: ['passable'], metadata: {} },
    { id: 'portal.stairs_up', kind: 'portal', name: 'Stairs', glyph: '↕', foreground: 'accent', movement_cost: 100, flags: ['passable'], metadata: {} },
    { id: 'furniture.bed', kind: 'furniture', name: 'Bed', glyph: 'H', foreground: 'ground', movement_cost: 0, flags: [], metadata: {} },
    { id: 'furniture.chair', kind: 'furniture', name: 'Chair', glyph: 'h', foreground: 'ground', movement_cost: 0, flags: [], metadata: {} },
    { id: 'furniture.table', kind: 'furniture', name: 'Table', glyph: 'T', foreground: 'ground', movement_cost: 0, flags: [], metadata: {} },
    { id: 'item.crate', kind: 'item', name: 'Crate', glyph: 'B', foreground: 'ground', movement_cost: 0, flags: [], metadata: {} }
  ]
}

const world: CityWorld = {
  id: 7, name: 'Test City', owner_user_id: 7, status: 'active', simulation_version: 'city-openworld-v3',
  current_tick: 0, speed_multiplier: 1, timezone: 'Asia/Shanghai', settings: {}, member_role: 'owner',
  created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z'
}

const binding = {
  world_id: 7, generator_id: 'city-openworld', generator_version: '2.0.0',
  rule_set_id: ruleSet.id, rule_set_version: ruleSet.version, rule_set_hash: ruleSet.content_hash,
  profile_id: 'cn.metropolitan', profile_version: '1.0.0', profile_hash: 'b'.repeat(64),
  context_hash: 'c'.repeat(64), seed: 42, spawn_sector_x: 0, spawn_sector_y: 0,
  spawn_x: 0, spawn_y: 0, spawn_z: 0, epoch: 1,
  bootstrap_plan_hash: 'd'.repeat(64), genesis_hash: 'e'.repeat(64),
  created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z'
}

const generation: CityOpenWorldGenerationState = {
  binding,
  regions: [],
  sectors: [{
    sector_x: 0, sector_y: 0, epoch: 1, chunk_size: 32, sector_size_chunks: 8,
    status: 'generated', plan_hash: 'd'.repeat(64), content_hash: 'f'.repeat(64),
    generated_tick: 0, revision: 1, created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z'
  }]
}

const interiorHash = '9'.repeat(64)
const openWorldMap: CityOpenWorldMap = {
  binding,
  chunks: [{
    chunk_x: 0, chunk_y: 0, z: 0, payload_hash: '7'.repeat(64), revision: 1,
    payload: {
      format: 'city-openworld-chunk-v1', width: 32, height: 32,
      terrain_runs: [{ definition_id: 'terrain.floor', length: 32 * 32 }],
      layers: [{ x: 0, y: 0, kind: 'structure', definition_id: 'structure.wall' }]
    }
  }],
  buildings: [{
    code: 'building_core_001', city_code: 'city_core', lot_code: 'lot_core', primary_use: 'residential',
    archetype_code: 'cn.courtyard_block', layout_style: 'courtyard', floor_count: 4,
    entrance: { x: 0, y: 0, z: 0 }, footprint: [{ x: 0, y: 0, z: 0 }], footprint_hash: '8'.repeat(64),
    interior_floor_count: 1, ground_interior_version: '1.0.0', ground_interior_hash: interiorHash, revision: 1
  }]
}

const interior: CityOpenWorldBuildingInterior = {
  building_code: 'building_core_001', floor_index: 0, z: 0, layout_version: '1.0.0', layout_style: 'courtyard',
  cells: [{ x: 0, y: 0, z: 0, kind: 'door' }, { x: 1, y: 0, z: 0, kind: 'furniture', feature: 'bed' }],
  content_hash: interiorHash, revision: 1
}

const ViewportStub = defineComponent({
  name: 'CityClassicViewport',
  props: { scene: { type: Object, required: true } },
  emits: ['resize', 'select-cell', 'pan', 'zoom'],
  setup(props, { emit }) {
    return () => h('button', {
      type: 'button', 'data-test': 'classic-viewport',
      onClick: () => {
        const scene = props.scene as { mode: string; cells?: Array<unknown> }
        const cell = scene.mode === 'local' ? scene.cells?.find(Boolean) : null
        if (cell) emit('select-cell', cell)
      }
    }, 'map')
  }
})

const DialogStub = defineComponent({
  name: 'BaseDialog',
  props: { show: Boolean, title: String },
  emits: ['close'],
  template: '<section v-if="show" data-test="interior-dialog"><h2>{{ title }}</h2><slot /></section>'
})

describe('CityOpenWorldWorkspace', () => {
  beforeEach(() => {
    getOpenWorldGeneration.mockReset().mockResolvedValue(generation)
    getCitySpatialRuleSet.mockReset().mockResolvedValue(ruleSet)
    getOpenWorldMap.mockReset().mockResolvedValue(openWorldMap)
    getOpenWorldBuildingInterior.mockReset().mockResolvedValue(interior)
	getOpenWorldVerification.mockReset().mockResolvedValue({
		world_id: 7, simulation_version: 'city-openworld-v3', scope: 'region', region_x: 0, region_y: 0,
		current_tick: 0, state_hash: 'f'.repeat(64), canonical_state_verified: false,
		region_count: 1, sector_count: 1, chunk_count: 64, building_count: 1, interior_count: 4, portal_count: 4,
		verified_at: '2026-01-01T00:00:00Z'
	})
    listOpenWorldBuildingPortals.mockReset().mockResolvedValue([])
  })

  it('opens a hash-verified server floor in the CLASSIC glyph inspector', async () => {
    const wrapper = mount(CityOpenWorldWorkspace, {
		props: { world, worlds: [world], systemAdmin: true },
      global: {
        plugins: [i18n],
        stubs: {
          CityClassicViewport: ViewportStub,
          BaseDialog: DialogStub,
          Select: true,
          Icon: true
        }
      }
    })

    await flushPromises()
    await nextTick()
    await wrapper.get('[data-test="classic-viewport"]').trigger('click')
    await nextTick()

    await wrapper.get('.open-world-building button').trigger('click')
    await flushPromises()

    expect(getOpenWorldBuildingInterior).toHaveBeenCalledWith(7, 'building_core_001', 0)
    expect(listOpenWorldBuildingPortals).toHaveBeenCalledWith(7, 'building_core_001')
    expect(wrapper.get('[data-test="interior-dialog"]').text()).toContain('building_core_001')
    expect(wrapper.text()).toContain('CLASSIC / INTERIOR FACTS')
  })

	it('verifies the bounded region at the current camera without reloading the world', async () => {
		const wrapper = mount(CityOpenWorldWorkspace, {
			props: { world, worlds: [world], systemAdmin: true },
			global: {
				plugins: [i18n],
				stubs: { CityClassicViewport: ViewportStub, BaseDialog: DialogStub, Select: true, Icon: true }
			}
		})

		await flushPromises()
		await wrapper.get('[data-test="verify-open-world-region"]').trigger('click')
		await flushPromises()

		expect(getOpenWorldVerification).toHaveBeenCalledWith(7, { region_x: 0, region_y: 0 })
		expect(getOpenWorldGeneration).toHaveBeenCalledTimes(1)
		expect(wrapper.text()).toContain('当前 Region 的地图事实已验证')
	})
})
