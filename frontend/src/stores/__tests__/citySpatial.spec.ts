import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import type {
  CityDevelopmentState,
  CityEnterpriseLocationState,
  CityLandState,
  CityMapChunk,
  CityMapChunkSummary,
  CityOvermapState,
  CitySpatialMutationPage,
  CityWorld,
  CityWorldSpatialRuleSet,
  WorldActor,
  WorldActorRoleOption,
  WorldActorState,
  WorldRuntimeCatalog,
  WorldRuntimeDefinition
} from '@/api/citySpatial'

const apiMocks = vi.hoisted(() => ({
  listWorlds: vi.fn(),
  createWorld: vi.fn(),
  getWorldSpatialRuleSet: vi.fn(),
  getOvermap: vi.fn(),
  getLandState: vi.fn(),
  getDevelopmentState: vi.fn(),
  getEnterpriseLocationState: vi.fn(),
  getWorldRuntimeCatalog: vi.fn(),
  listWorldActors: vi.fn(),
  getWorldActorState: vi.fn(),
  getWorldActorRoleOptions: vi.fn(),
  listWorldRuntimeRules: vi.fn(),
  listWorldRuleCases: vi.fn(),
  listMapChunks: vi.fn(),
  getMapChunk: vi.fn(),
  listSpatialChanges: vi.fn(),
  submitGenerateChunk: vi.fn(),
  submitDevelopmentCommand: vi.fn(),
  submitEnterpriseLocationCommand: vi.fn(),
  submitWorldRuntimeCommand: vi.fn(),
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
  created_tick: 1, updated_tick: 2, version: 2, metadata: {}
}

function runtimeActorState(reasoningUnits: number, tick: number): WorldActorState {
  return {
    actor: { ...runtimeActor, updated_tick: tick, version: tick },
    attributes: [{
      actor_code: runtimeActor.code, attribute_code: 'reasoning', value_units: reasoningUnits,
      experience_units: Math.max(0, reasoningUnits - 16000), last_changed_tick: tick, version: tick, metadata: {}
    }],
    roles: [], statuses: [], recent_facts: []
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
  apiMocks.getWorldSpatialRuleSet.mockResolvedValue(ruleBundle)
  apiMocks.getOvermap.mockResolvedValue(overmap)
  apiMocks.getLandState.mockResolvedValue(landState)
  apiMocks.getDevelopmentState.mockResolvedValue(developmentState)
  apiMocks.getEnterpriseLocationState.mockResolvedValue(enterpriseLocationState)
  apiMocks.getWorldRuntimeCatalog.mockRejectedValue({
    status: 404,
    code: 'WORLD_RUNTIME_STATE_NOT_FOUND',
    message: 'world runtime state not found'
  })
  apiMocks.listWorldActors.mockResolvedValue([])
  apiMocks.listWorldRuntimeRules.mockResolvedValue([])
  apiMocks.listWorldRuleCases.mockResolvedValue({ items: [] })
  apiMocks.listSpatialChanges.mockResolvedValue(emptyChanges)
  apiMocks.listMapChunks.mockResolvedValue(chunks)
  apiMocks.getMapChunk.mockResolvedValue(chunk)
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

    await store.runWorldRuntimeCommand(
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
  })
})
