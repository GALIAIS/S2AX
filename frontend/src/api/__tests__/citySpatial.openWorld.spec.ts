import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post } = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }))

vi.mock('@/api/client', () => ({
  apiClient: { get, post }
}))

import {
  getCitySpatialRuleSet,
	getOpenWorldBuildingInterior,
	getOpenWorldGeneration,
	getOpenWorldMap,
	getOpenWorldVerification,
  listOpenWorldBuildingPortals,
  listOpenWorldStyleProfiles,
  submitOpenWorldSectorMaterialization
} from '@/api/citySpatial'

describe('open-world city API', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
  })

  it('reads the server-owned style catalog and frozen glyph rule set', async () => {
    get
      .mockResolvedValueOnce({ data: [{ id: 'jp.metropolitan', version: '1.0.0', name: 'Japanese Metropolitan', content_hash: 'a'.repeat(64) }] })
      .mockResolvedValueOnce({ data: { id: 'sub2api-classic', version: '1.0.0', content_hash: 'b'.repeat(64), palette: [], definitions: [], chunk_size: 32, min_z: -32, max_z: 127, name: 'Classic' } })

    await expect(listOpenWorldStyleProfiles()).resolves.toHaveLength(1)
    await expect(getCitySpatialRuleSet('sub2api-classic')).resolves.toMatchObject({ id: 'sub2api-classic' })

    expect(get).toHaveBeenNthCalledWith(1, '/city/open-world/styles')
    expect(get).toHaveBeenNthCalledWith(2, '/city/spatial/rule-sets/sub2api-classic')
  })

	it('requests V2 genesis and bounded persisted surface facts without F7 endpoints', async () => {
    const binding = {
      world_id: 7, generator_id: 'city-openworld', generator_version: '2.0.0',
      rule_set_id: 'sub2api-classic', rule_set_version: '1.0.0', rule_set_hash: 'c'.repeat(64),
      profile_id: 'cn.metropolitan', profile_version: '1.0.0', profile_hash: 'd'.repeat(64),
      context_hash: 'e'.repeat(64), seed: 8, spawn_sector_x: 0, spawn_sector_y: 0,
      spawn_x: 12, spawn_y: 18, spawn_z: 0, epoch: 1,
      bootstrap_plan_hash: 'f'.repeat(64), genesis_hash: '0'.repeat(64),
      created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z'
    }
    get
      .mockResolvedValueOnce({ data: { binding, regions: [], sectors: [] } })
      .mockResolvedValueOnce({ data: { binding, chunks: [], buildings: [] } })

    await expect(getOpenWorldGeneration(7)).resolves.toMatchObject({ binding: { profile_id: 'cn.metropolitan' } })
    await expect(getOpenWorldMap(7, { min_x: -128, max_x: 127, min_y: -128, max_y: 127, z: 0 })).resolves.toMatchObject({ chunks: [] })

    expect(get).toHaveBeenNthCalledWith(1, '/city/worlds/7/open-world/generation')
    expect(get).toHaveBeenNthCalledWith(2, '/city/worlds/7/open-world/map', {
      params: { min_x: -128, max_x: 127, min_y: -128, max_y: 127, z: 0 }
    })
	})

	it('verifies either the whole world or one materialized region', async () => {
		get
			.mockResolvedValueOnce({ data: { scope: 'world', canonical_state_verified: true } })
			.mockResolvedValueOnce({ data: { scope: 'region', region_x: 1, region_y: -1, canonical_state_verified: false } })

		await expect(getOpenWorldVerification(7)).resolves.toMatchObject({
			scope: 'world', canonical_state_verified: true
		})
		await expect(getOpenWorldVerification(7, { region_x: 1, region_y: -1 })).resolves.toMatchObject({
			scope: 'region', region_x: 1, region_y: -1, canonical_state_verified: false
		})

		expect(get).toHaveBeenNthCalledWith(1, '/city/worlds/7/open-world/verification', { params: undefined })
		expect(get).toHaveBeenNthCalledWith(2, '/city/worlds/7/open-world/verification', {
			params: { region_x: 1, region_y: -1 }
		})
	})

	it('reads a sealed building floor by code and floor index', async () => {
    get.mockResolvedValueOnce({
      data: {
        building_code: 'building_core_001', floor_index: 0, z: 0,
        layout_version: '1.0.0', layout_style: 'rowhouse', cells: [],
        content_hash: 'a'.repeat(64), revision: 1
      }
    })

    await expect(getOpenWorldBuildingInterior(7, 'building_core_001', 0)).resolves.toMatchObject({
      building_code: 'building_core_001', floor_index: 0
    })
    expect(get).toHaveBeenCalledWith('/city/worlds/7/open-world/buildings/building_core_001/interiors/0')
  })

  it('reads immutable vertical portal topology separately from mutable runtime state', async () => {
    get.mockResolvedValueOnce({
      data: [{
        code: 'building_core_001.stairs.00.01', building_code: 'building_core_001', portal_type: 'stairs',
        from_floor_index: 0, to_floor_index: 1,
        from: { x: 1, y: 2, z: 0 }, to: { x: 1, y: 2, z: 1 }, bidirectional: true,
        topology_hash: 'a'.repeat(64), revision: 1
      }]
    })

    await expect(listOpenWorldBuildingPortals(7, 'building_core_001')).resolves.toHaveLength(1)
    expect(get).toHaveBeenCalledWith('/city/worlds/7/open-world/buildings/building_core_001/portals')
  })

  it('submits V2 sector materialization through the audited command queue', async () => {
    post.mockResolvedValueOnce({ data: { id: 71, command_type: 'open_world.sector.materialize', status: 'pending' } })

    await expect(submitOpenWorldSectorMaterialization(7, 4, -1, 12)).resolves.toMatchObject({ id: 71 })
    expect(post).toHaveBeenCalledWith(
      '/city/worlds/7/commands',
      {
        command_type: 'open_world.sector.materialize',
        payload: { sector_x: 4, sector_y: -1 },
        expected_world_tick: 12
      },
      expect.objectContaining({ headers: expect.objectContaining({ 'Idempotency-Key': expect.stringContaining('open-world-sector-7-4--1') }) })
    )
  })
})
