import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post } = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }))

vi.mock('@/api/client', () => ({
  apiClient: { get, post }
}))

import {
  findWorldActorPath,
  getWorldNavigationIntent,
  listWorldNavigationIntents,
  listWorldNavigationReservations,
  listWorldPortalStates
} from '@/api/citySpatial'

describe('city spatial navigation API', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
    post.mockResolvedValue({
      data: {
        navigation_version: '1.0.0', world_tick: 4, spatial_rule_hash: 'a'.repeat(64),
        actor_code: 'actor_00000001', from: { x: 1, y: 2, z: 0 }, to: { x: 4, y: 2, z: 0 },
        reachable: true, total_cost: 240, expanded_nodes: 4, steps: []
      }
    })
  })

  it('loads the versioned portal projection for the selected actor', async () => {
    get.mockResolvedValue({
      data: [{
        state: {
          building_code: 'building_hall', portal_code: 'entrance_main', portal_type: 'entrance',
          state_code: 'open', access_requirement: { op: 'all' }, access_policy_hash: 'b'.repeat(64),
          changed_tick: 4, version: 1, metadata: {}
        },
        from: { x: 10, y: 20, z: 0 }, to: { x: 11, y: 20, z: 0 }, bidirectional: true,
        accessible: true, access_evaluation: { satisfied: true, failures: [] }
      }]
    })

    const result = await listWorldPortalStates(7, 'actor_00000001')

    expect(get).toHaveBeenCalledWith('/city/worlds/7/navigation/portals', {
      params: { actor_code: 'actor_00000001' }
    })
    expect(result[0]?.state.access_policy_hash).toHaveLength(64)
  })

  it('posts the actor, exact XYZ destination, and bounded search depth', async () => {
    const result = await findWorldActorPath(
      7,
      'actor_00000001',
      { x: 4, y: 2, z: 0 },
      64
    )

    expect(post).toHaveBeenCalledWith('/city/worlds/7/navigation/path', {
      actor_code: 'actor_00000001',
      destination: { x: 4, y: 2, z: 0 },
      max_steps: 64
    })
    expect(result.reachable).toBe(true)
  })

  it('loads movement intents, exact actor intent, and tick-scoped reservations', async () => {
    const intent = {
      actor_code: 'actor_00000001', intent_code: 'navigation_intent_00000000000000000009',
      destination: { x: 4, y: 2, z: 0 }, status: 'active', on_blocked: 'retry',
      priority: 1, max_steps: 64, budget_units: 80, budget_gain_units: 100, budget_cap_units: 400,
      blocked_attempts: 0, next_attempt_tick: 5, created_tick: 4, updated_tick: 4,
      source_fact: { tick: 4, sequence: 2 }, version: 1, metadata: { schema_version: 1 }
    }
    const reservation = {
      tick: 5, sequence: 1, actor_code: intent.actor_code, intent_code: intent.intent_code,
      from: { x: 1, y: 2, z: 0 }, to: { x: 2, y: 2, z: 0 }, target_key: '2:2:0',
      edge_key: '1:2:0|2:2:0', step_cost: 80, source_fact: { tick: 5, sequence: 3 },
      status: 'consumed', metadata: { schema_version: 1 }
    }
    get
      .mockResolvedValueOnce({ data: [intent] })
      .mockResolvedValueOnce({ data: intent })
      .mockResolvedValueOnce({ data: [reservation] })
      .mockResolvedValueOnce({ data: [] })

    await expect(listWorldNavigationIntents(7)).resolves.toEqual([intent])
    await expect(getWorldNavigationIntent(7, intent.actor_code)).resolves.toEqual(intent)
    await expect(listWorldNavigationReservations(7, 5)).resolves.toEqual([reservation])
    await expect(listWorldNavigationReservations(7)).resolves.toEqual([])

    expect(get).toHaveBeenNthCalledWith(1, '/city/worlds/7/navigation/intents')
    expect(get).toHaveBeenNthCalledWith(2, '/city/worlds/7/navigation/intents/actor_00000001')
    expect(get).toHaveBeenNthCalledWith(3, '/city/worlds/7/navigation/reservations', { params: { tick: 5 } })
    expect(get).toHaveBeenNthCalledWith(4, '/city/worlds/7/navigation/reservations', { params: undefined })
  })
})
