import { describe, expect, it } from 'vitest'
import type {
  CityDevelopmentState,
  CityEnterpriseLocationState,
  CityLandState,
  CityMapChunk,
  CityOvermapTile,
  CitySpatialRuleSet
} from '@/api/citySpatial'
import {
  applyCityDevelopmentOverlay,
  applyCityEnterpriseOverlay,
  applyCityLandOverlay,
  buildLocalScene,
  buildOvermapScene,
  chunkKey,
  exportProjectedChunkText,
  floorDiv,
  getCityLandCellContext,
  getCityLandTileSummary,
  hitTestClassicScene,
  projectCityChunk,
  resolveClassicVisual,
  viewportChunkBounds,
  xterm256Color
} from '../projection'

const ruleSet: CitySpatialRuleSet = {
  id: 'test-classic',
  version: '1.0.0',
  name: 'Test Classic',
  chunk_size: 2,
  min_z: -2,
  max_z: 2,
  content_hash: 'a'.repeat(64),
  palette: [
    { id: 'ground', name: 'Ground', classic_foreground: 244, classic_background: 234 },
    { id: 'green', name: 'Green', classic_foreground: 71, classic_background: 234 },
    { id: 'danger', name: 'Danger', classic_foreground: 203, classic_background: 52 },
    { id: 'road', name: 'Road', classic_foreground: 250, classic_background: 238 },
    { id: 'water', name: 'Water', classic_foreground: 75, classic_background: 24 }
  ],
  definitions: [
    { id: 'missing.terrain', kind: 'terrain', name: 'Unknown terrain', glyph: '?', foreground: 'danger', movement_cost: 0, flags: [], metadata: {} },
    { id: 'missing.furniture', kind: 'furniture', name: 'Unknown furniture', glyph: '?', foreground: 'danger', movement_cost: 0, flags: [], metadata: {} },
    { id: 'terrain.ground', kind: 'terrain', name: 'Ground', glyph: '.', foreground: 'ground', movement_cost: 100, flags: ['passable'], metadata: {} },
    { id: 'terrain.grass', kind: 'terrain', name: 'Grass', foreground: 'green', looks_like: 'terrain.ground', movement_cost: 110, flags: ['passable'], metadata: {} },
    { id: 'terrain.road', kind: 'terrain', name: 'Road', glyph: '=', foreground: 'road', background: 'road', movement_cost: 80, flags: ['passable'], metadata: {} },
    { id: 'terrain.deep_water', kind: 'terrain', name: 'Water', glyph: '≈', foreground: 'water', background: 'water', movement_cost: 400, flags: ['liquid'], metadata: {} },
    { id: 'furniture.tree', kind: 'furniture', name: 'Tree', glyph: '♣', foreground: 'green', movement_cost: 0, flags: ['blocks_items'], metadata: {} }
  ]
}

const chunk: CityMapChunk = {
  world_id: 1,
  chunk_x: -1,
  chunk_y: 0,
  z: 0,
  district_code: 'west',
  generator_id: 'test',
  generator_version: '1.0.0',
  generation_proof: 'proof',
  revision: 1,
  payload_hash: 'b'.repeat(64),
  generated_tick: 3,
  rule_set_hash: ruleSet.content_hash,
  payload: {
    format: 'city-chunk-v1',
    width: 2,
    height: 2,
    terrain_runs: [
      { definition_id: 'terrain.grass', length: 2 },
      { definition_id: 'terrain.ground', length: 2 }
    ],
    furniture: [{ x: 1, y: 0, definition_id: 'furniture.tree' }]
  },
  metadata: {},
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z'
}

const landState: CityLandState = {
  profile: {
    rule_set_id: 'sub2api-land', rule_set_version: '1.0.0', rule_set_hash: 'c'.repeat(64),
    spatial_overmap_root_hash: 'd'.repeat(64), nominal_cell_area_sqm: 1500,
    baseline_hash: 'e'.repeat(64), baseline_tick: 0, zoning_rule_count: 3,
    parcel_count: 1, building_count: 1, unit_pool_count: 1,
    housing_allocation_count: 1, portal_count: 2, revision: 1
  },
  zoning_rules: [{
    code: 'residential', name: 'Residential', primary_use: 'residential',
    max_floor_area_ratio_milli: 3000, max_coverage_milli: 450,
    max_floors: 12, sqm_per_capacity_unit: 90
  }],
  parcels: [{
    code: 'parcel_west', district_code: 'west', zone_code: 'residential',
    geometry: { chunk_x: -1, chunk_y: 0, z: 0, local_min_x: 0, local_min_y: 0, local_max_x: 1, local_max_y: 1 },
    area_sqm: 6000, developable_area_sqm: 6000, status: 'active', version: 1
  }],
  buildings: [{
    code: 'building_west', parcel_code: 'parcel_west', district_code: 'west', primary_use: 'residential',
    footprint: { chunk_x: -1, chunk_y: 0, z: 0, local_min_x: 0, local_min_y: 0, local_max_x: 1, local_max_y: 1 },
    base_z: 0, top_z: 1, floor_count: 2, footprint_area_sqm: 2700,
    floor_area_sqm: 5400, capacity_units: 60, occupied_units: 25,
    quality_milli: 1000, status: 'active', completed_tick: 0, version: 1
  }],
  unit_pools: [{
    code: 'pool_building_west', building_code: 'building_west', district_code: 'west',
    use_type: 'residential', unit_count: 60, occupied_unit_count: 25,
    capacity_units_per_unit: 1, version: 1
  }],
  housing_allocations: [{
    pool_code: 'pool_building_west', district_code: 'west', cohort_key: 'west/household/medium',
    allocated_units: 25, status: 'active', version: 1
  }],
  portals: [
    {
      code: 'entrance', building_code: 'building_west', district_code: 'west', portal_type: 'entrance',
      from_x: -3, from_y: 0, from_z: 0, to_x: -2, to_y: 0, to_z: 0,
      bidirectional: true, status: 'active', version: 1
    },
    {
      code: 'stair_000_001', building_code: 'building_west', district_code: 'west', portal_type: 'stair',
      from_x: -1, from_y: 1, from_z: 0, to_x: -1, to_y: 1, to_z: 1,
      bidirectional: true, status: 'active', version: 1
    }
  ]
}

const developmentState: CityDevelopmentState = {
  profile: {
    policy_id: 'sub2api-development', policy_version: '1.0.0', policy_hash: 'f'.repeat(64),
    baseline_tick: 0, baseline_hash: '0'.repeat(64), project_count: 1,
    fact_count: 3, adjustment_count: 0, revision: 3
  },
  projects: [{
    code: 'development_7', name: 'West Extension', project_type: 'vertical_expansion',
    district_code: 'west', parcel_code: 'parcel_west', building_code: 'building_west',
    primary_use: 'residential', developer_entity_code: 'firm_west', target_floor_count: 3,
    added_floor_count: 1, added_floor_area_sqm: 2700, added_capacity_units: 30,
    quality_delta_milli: 0, required_basic_material_units: 270,
    required_capital_goods_units: 27, required_labor_units: 54,
    planned_duration_ticks: 7, status: 'under_construction', progress_milli: 428,
    submitted_tick: 1, reviewed_tick: 2, started_tick: 3,
    planned_completion_tick: 10, version: 3, metadata: {}
  }],
  facts: [], adjustments: [], developers: []
}

const enterpriseState: CityEnterpriseLocationState = {
  profile: {
    policy_id: 'sub2api-enterprise-location', policy_version: '1.0.0', policy_hash: '1'.repeat(64),
    baseline_tick: 0, baseline_hash: '2'.repeat(64), baseline_site_count: 1,
    site_count: 1, fact_count: 0, revision: 1
  },
  baseline_sites: [],
  sites: [{
    code: 'site_firm_west_headquarters', firm_entity_code: 'firm_west', district_code: 'west',
    building_code: 'building_west', pool_code: 'pool_building_west', site_type: 'headquarters',
    name: 'West Firm Headquarters', occupied_units: 5, is_primary: true, status: 'active',
    opened_tick: 0, last_changed_tick: 0, version: 1, metadata: {}
  }],
  facts: [],
  firms: [{
    entity_id: 9, entity_code: 'firm_west', entity_name: 'West Firm', district_code: 'west',
    employee_units: 20, capital_stock_units: 10, production_capacity_units: 4, active_site_count: 1
  }],
  pools: [{
    code: 'pool_building_west', building_code: 'building_west', district_code: 'west',
    use_type: 'commercial', effective_unit_count: 60, occupied_unit_count: 5, available_unit_count: 55
  }]
}

describe('city spatial projection', () => {
  it('converts xterm colors and negative coordinates deterministically', () => {
    expect(xterm256Color(16)).toBe('#000000')
    expect(xterm256Color(231)).toBe('#ffffff')
    expect(xterm256Color(232)).toBe('#080808')
    expect(floorDiv(-1, 32)).toBe(-1)
    expect(floorDiv(-32, 32)).toBe(-1)
    expect(floorDiv(-33, 32)).toBe(-2)
  })

  it('uses looks-like glyph fallback while preserving the requested visual color', () => {
    const visual = resolveClassicVisual(ruleSet, 'terrain', 'terrain.grass')
    expect(visual.glyph).toBe('.')
    expect(visual.glyphSourceID).toBe('terrain.ground')
    expect(visual.definition.id).toBe('terrain.grass')
    expect(visual.foreground).toBe(xterm256Color(71))
    expect(visual.fallbackPath).toEqual(['terrain.grass', 'terrain.ground'])
  })

  it('decodes RLE payloads and applies furniture display priority', () => {
    const projected = projectCityChunk(chunk, ruleSet)
    expect(projected.key).toBe('z:0/x:-1/y:0')
    expect(projected.cells).toHaveLength(4)
    expect(projected.cells[0]).toMatchObject({ worldX: -2, worldY: 0, glyph: '.', terrainDefinitionID: 'terrain.grass' })
    expect(projected.cells[1]).toMatchObject({ worldX: -1, worldY: 0, glyph: '♣', furnitureDefinitionID: 'furniture.tree' })
    expect(projected.cells[1].stack.map(layer => layer.kind)).toEqual(['terrain', 'furniture'])
  })

  it('rejects incomplete RLE payloads', () => {
    const invalid = structuredClone(chunk)
    invalid.payload.terrain_runs = [{ definition_id: 'terrain.ground', length: 3 }]
    expect(() => projectCityChunk(invalid, ruleSet)).toThrow('Incomplete city chunk terrain payload')
  })

  it('builds a hit-testable local scene and exports canonical text rows', () => {
    const projected = projectCityChunk(chunk, ruleSet)
    const chunks = new Map([[projected.key, projected]])
    const scene = buildLocalScene(chunks, { worldX: -1, worldY: 1, z: 0, cellSize: 16 }, { width: 32, height: 32 }, 2)
    const hit = hitTestClassicScene(scene, 8, 8)
    expect(hit && 'worldX' in hit ? hit.worldX : null).toBe(-2)
    expect(exportProjectedChunkText(projected)).toContain('.♣\n..')
    expect(exportProjectedChunkText(projected)).toContain(`# payload_hash=${chunk.payload_hash}`)
  })

  it('projects server-backed buildings and portals above furniture without changing the Chunk', () => {
    const projected = projectCityChunk(chunk, ruleSet)
    const entranceCell = applyCityLandOverlay(projected.cells[0], landState, 2)
    const stairCell = applyCityLandOverlay(projected.cells[3], landState, 2)

    expect(entranceCell.glyph).toBe('+')
    expect(entranceCell.stack.map(layer => layer.kind)).toEqual(['terrain', 'structure', 'portal'])
    expect(stairCell.glyph).toBe('↕')
    expect(projected.cells[0].glyph).toBe('.')

    const context = getCityLandCellContext(landState, -1, 1, 0, 2)
    expect(context?.building?.code).toBe('building_west')
    expect(context?.unitPools[0]?.occupied_unit_count).toBe(25)
    expect(context?.housingAllocations[0]?.cohort_key).toBe('west/household/medium')
    expect(context?.portals[0]?.portal_type).toBe('stair')
  })

  it('adds construction facts as a non-destructive CLASSIC overlay and Overmap count', () => {
    const projected = projectCityChunk(chunk, ruleSet)
    const landCell = applyCityLandOverlay(projected.cells[0], landState, 2)
    const constructionCell = applyCityDevelopmentOverlay(landCell, landState, developmentState, 2)
    expect(constructionCell.glyph).toBe('%')
    expect(constructionCell.foreground).toBe('#d99b52')
    expect(constructionCell.stack.at(-1)).toMatchObject({
      kind: 'overlay', definitionID: 'development:development_7'
    })
    expect(landCell.glyph).toBe('+')

    const tile: CityOvermapTile = {
      chunk_x: -1, chunk_y: 0, z: 0, district_code: 'west',
      terrain_definition_id: 'terrain.grass', road_mask: 0, river_mask: 0,
      variant: 0, tile_hash: 'development', metadata: {}
    }
    const scene = buildOvermapScene(
      [tile], ruleSet, { width: 300, height: 240 }, landState, developmentState
    )
    expect(scene.cells[0]).toMatchObject({ activeProjectCount: 1, completedProjectCount: 0 })
  })

  it('adds enterprise sites at one deterministic building anchor without hiding portals or construction', () => {
    const projected = projectCityChunk(chunk, ruleSet)
    const anchor = applyCityLandOverlay(projected.cells[0], landState, 2)
    const enterprise = applyCityEnterpriseOverlay(anchor, landState, enterpriseState, 2)
    const construction = applyCityDevelopmentOverlay(enterprise, landState, developmentState, 2)

    expect(enterprise.stack.at(-1)).toMatchObject({
      kind: 'entity', definitionID: 'enterprise:building_west'
    })
    expect(enterprise.glyph).toBe('+')
    expect(construction.stack.some(layer => layer.kind === 'entity')).toBe(true)

    const tile: CityOvermapTile = {
      chunk_x: -1, chunk_y: 0, z: 0, district_code: 'west',
      terrain_definition_id: 'terrain.grass', road_mask: 0, river_mask: 0,
      variant: 0, tile_hash: 'enterprise', metadata: {}
    }
    const scene = buildOvermapScene(
      [tile], ruleSet, { width: 300, height: 240 }, landState, developmentState, enterpriseState
    )
    expect(scene.cells[0]).toMatchObject({
      activeEnterpriseSiteCount: 1, enterpriseFirmCount: 1, enterpriseOccupiedUnits: 5
    })
  })

  it('projects real road and river masks on the Overmap', () => {
    const tiles: CityOvermapTile[] = [
      { chunk_x: 0, chunk_y: 0, z: 0, district_code: 'central', terrain_definition_id: 'terrain.grass', road_mask: 5, river_mask: 0, variant: 0, tile_hash: 'road', metadata: {} },
      { chunk_x: 1, chunk_y: 0, z: 0, district_code: 'east', terrain_definition_id: 'terrain.deep_water', road_mask: 0, river_mask: 5, variant: 0, tile_hash: 'river', metadata: {} }
    ]
    const scene = buildOvermapScene(tiles, ruleSet, { width: 400, height: 300 })
    expect(scene.cells.map(cell => cell.glyph)).toEqual(['│', '≈'])
    expect(hitTestClassicScene(scene, scene.cells[0].x + 1, scene.cells[0].y + 1)).toEqual(tiles[0])
  })

  it('adds deterministic zoning and building summaries to Overmap cells', () => {
    const tile: CityOvermapTile = {
      chunk_x: -1, chunk_y: 0, z: 0, district_code: 'west',
      terrain_definition_id: 'terrain.grass', road_mask: 0, river_mask: 0,
      variant: 0, tile_hash: 'land', metadata: {}
    }
    const summary = getCityLandTileSummary(landState, tile)
    const scene = buildOvermapScene([tile], ruleSet, { width: 300, height: 240 }, landState)

    expect(summary.landUses).toEqual(['residential'])
    expect(summary.buildings[0]?.code).toBe('building_west')
    expect(scene.cells[0]).toMatchObject({ landUses: ['residential'], parcelCount: 1, buildingCount: 1 })
  })

  it('limits viewport prefetch bounds to the server Overmap', () => {
    expect(viewportChunkBounds(
      { worldX: -128, worldY: 127, z: 0, cellSize: 16 },
      { width: 640, height: 480 },
      32,
      { minX: -4, maxX: 4, minY: -4, maxY: 4 }
    )).toEqual({ min_x: -4, max_x: -3, min_y: 2, max_y: 4, z: 0 })
  })

  it('produces stable chunk keys', () => {
    expect(chunkKey(-4, 2, -1)).toBe('z:-1/x:-4/y:2')
  })
})
