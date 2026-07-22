import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post } = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }))

vi.mock('@/api/client', () => ({
  apiClient: { get, post }
}))

import {
	changeRealtimeCharacterRole,
  configureRealtimeCharacterAgent,
  createRealtimeCharacter,
  getRealtimeMyCharacter,
  listRealtimeMyCharacterEvents,
  listRealtimePublicCharacterEvents,
  performRealtimeCharacterActivity,
  moveRealtimeCharacter,
  traverseRealtimeCharacterPortal
} from '@/api/citySpatial'

describe('realtime player character API', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
  })

  it('uses owner-only and durable command routes without sending an Actor code', async () => {
    get.mockResolvedValueOnce({
      data: { world_id: 7, timeline_frame_sequence: 3, timeline_cursor: 'twf_000000000003', runtime_ready: true, exists: false }
    })
    post
      .mockResolvedValueOnce({
        data: {
          character: { actor_code: 'character.player.0123456789abcdef0123456789abcdef', public_label: '春日 花子' },
          frame: { world_id: 7, frame_sequence: 4, timeline_cursor: 'twf_000000000004' }
        }
      })
      .mockResolvedValueOnce({
        data: {
          character: { actor_code: 'character.player.0123456789abcdef0123456789abcdef', x: 13, y: 18, z: 0 },
          frame: { world_id: 7, frame_sequence: 5, timeline_cursor: 'twf_000000000005' }
        }
      })
      .mockResolvedValueOnce({
        data: {
          character: { actor_code: 'character.player.0123456789abcdef0123456789abcdef', x: 14, y: 18, z: 0 },
          frame: { world_id: 7, frame_sequence: 6, timeline_cursor: 'twf_000000000006' }
        }
      })
		.mockResolvedValueOnce({
			data: {
				character: { actor_code: 'character.player.0123456789abcdef0123456789abcdef', x: 14, y: 18, z: 0 },
				role_change: { category_code: 'profession', from_role_code: 'profession.resident', to_role_code: 'profession.civic_aide' },
				frame: { world_id: 7, frame_sequence: 7, timeline_cursor: 'twf_000000000007' }
			}
		})

    await expect(getRealtimeMyCharacter(7)).resolves.toMatchObject({ exists: false })
	await expect(createRealtimeCharacter(7, { public_label: '春日 花子', archetype_code: 'resident.social' }, 'character-create-7')).resolves.toMatchObject({
      character: { public_label: '春日 花子' }
    })
    await expect(moveRealtimeCharacter(7, { x: 13, y: 18, z: 0 }, 'character-move-7')).resolves.toMatchObject({
      frame: { frame_sequence: 5 }
    })
    await expect(traverseRealtimeCharacterPortal(7, { portal_code: 'portal.building.entrance.01' }, 'character-portal-7')).resolves.toMatchObject({
      frame: { frame_sequence: 6 }
    })
		await expect(changeRealtimeCharacterRole(7, { role_code: 'profession.civic_aide' }, 'character-role-7')).resolves.toMatchObject({
			role_change: { to_role_code: 'profession.civic_aide' }
		})

    expect(get).toHaveBeenCalledWith('/city/worlds/7/realtime/character')
    expect(post).toHaveBeenNthCalledWith(
      1,
      '/city/worlds/7/realtime/character',
			{ public_label: '春日 花子', archetype_code: 'resident.social' },
      { headers: { 'Idempotency-Key': 'character-create-7' } }
    )
    expect(post).toHaveBeenNthCalledWith(
      2,
      '/city/worlds/7/realtime/character/move',
      { x: 13, y: 18, z: 0 },
      { headers: { 'Idempotency-Key': 'character-move-7' } }
    )
    expect(post).toHaveBeenNthCalledWith(
      3,
      '/city/worlds/7/realtime/character/portals',
      { portal_code: 'portal.building.entrance.01' },
      { headers: { 'Idempotency-Key': 'character-portal-7' } }
    )
		expect(post).toHaveBeenNthCalledWith(
			4,
			'/city/worlds/7/realtime/character/roles',
			{ role_code: 'profession.civic_aide' },
			{ headers: { 'Idempotency-Key': 'character-role-7' } }
		)
  })

  it('keeps activity effects server-owned and uses separate private/public timeline routes', async () => {
    post.mockResolvedValueOnce({
      data: {
        character: { actor_code: 'character.player.0123456789abcdef0123456789abcdef', public_label: '春日 花子' },
        life: { energy_milli: 920, satiety_milli: 700, morale_milli: 650, civic_standing_milli: 800, city_credit_units: 0, inventory: [] },
        activity: { code: 'rest.short', category_code: 'recovery', outcome: 'completed' },
        frame: { world_id: 7, frame_sequence: 6, timeline_cursor: 'twf_000000000006' }
      }
    })
    get
      .mockResolvedValueOnce({ data: { items: [] } })
      .mockResolvedValueOnce({ data: { items: [] } })

    await expect(performRealtimeCharacterActivity(7, { activity_code: 'rest.short' }, 'character-activity-7')).resolves.toMatchObject({
      activity: { code: 'rest.short' }
    })
    await expect(listRealtimeMyCharacterEvents(7, { before_sequence: 4, limit: 12 })).resolves.toMatchObject({ items: [] })
    await expect(listRealtimePublicCharacterEvents(7, { before_cursor: '9|character.player.0123456789abcdef0123456789abcdef|1', limit: 12 })).resolves.toMatchObject({ items: [] })

    expect(post).toHaveBeenCalledWith(
      '/city/worlds/7/realtime/character/activities',
      { activity_code: 'rest.short' },
      { headers: { 'Idempotency-Key': 'character-activity-7' } }
    )
    expect(get).toHaveBeenNthCalledWith(
      1,
      '/city/worlds/7/realtime/character/events',
      { params: { before_sequence: 4, limit: 12 } }
    )
    expect(get).toHaveBeenNthCalledWith(
      2,
      '/city/worlds/7/realtime/events',
      { params: { before_cursor: '9|character.player.0123456789abcdef0123456789abcdef|1', limit: 12 } }
    )
  })

  it('configures only the caller-owned Character Agent contract', async () => {
    post.mockResolvedValueOnce({
      data: {
        character: { actor_code: 'character.player.0123456789abcdef0123456789abcdef', control_mode: 'autonomous' },
        agent: {
          control_mode: 'autonomous',
          personality: {
            schema_version: 1,
            revision: 2,
            seed_hash: 'personality-hash',
            seed: {
              values: ['community'],
              preferences: {},
              background: '',
              hard_boundaries: ['avoid harm'],
              freeform_notes: ''
            }
          },
          pending_decision: false,
          pending_intent: false,
          autonomy_runtime_available: true
        },
        frame: { world_id: 7, frame_sequence: 8, timeline_cursor: 'twf_000000000008' }
      }
    })

    await expect(configureRealtimeCharacterAgent(7, {
      control_mode: 'autonomous',
      personality: {
        values: ['community'],
        preferences: {},
        background: '',
        hard_boundaries: ['avoid harm'],
        freeform_notes: ''
      }
    }, 'character-agent-7')).resolves.toMatchObject({
      agent: { control_mode: 'autonomous', personality: { revision: 2 } }
    })

    expect(post).toHaveBeenCalledWith(
      '/city/worlds/7/realtime/character/agent',
      {
        control_mode: 'autonomous',
        personality: {
          values: ['community'],
          preferences: {},
          background: '',
          hard_boundaries: ['avoid harm'],
          freeform_notes: ''
        }
      },
      { headers: { 'Idempotency-Key': 'character-agent-7' } }
    )
  })
})
