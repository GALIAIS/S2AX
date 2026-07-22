import { describe, expect, it } from 'vitest'
import {
  indexRealtimePublicActors,
  realtimeActorCellKey,
  resolveRealtimeActorSpritePalette
} from '../realtimeActorProjection'

describe('realtime actor projection helpers', () => {
  it('indexes only bounded member-safe public actor fields by map cell', () => {
    const index = indexRealtimePublicActors([
      {
        actor_code: 'npc.resident.02', actor_kind: 'npc', public_label: 'Resident 02',
        appearance_variant: 'resident.teal', lifecycle_status: 'active',
        x: 8, y: 4, z: 0, motion_state: 'walking', position_revision: 3, last_frame_sequence: 8
      },
      {
        actor_code: 'npc.resident.01', actor_kind: 'npc', public_label: 'Resident 01',
        appearance_variant: 'resident.ochre', lifecycle_status: 'active',
        x: 8, y: 4, z: 0, motion_state: 'idle', position_revision: 1, last_frame_sequence: 0
      }
    ])

    expect(index.get(realtimeActorCellKey(8, 4, 0))?.map(actor => actor.actor_code)).toEqual([
      'npc.resident.01', 'npc.resident.02'
    ])
    expect(resolveRealtimeActorSpritePalette('resident.teal')).toEqual({
      body: '#4cae9c', accent: '#9cd8cb', outline: '#17312d'
    })
  })

  it('rejects duplicate or account-shaped actor data before canvas rendering', () => {
    expect(() => indexRealtimePublicActors([
      {
        actor_code: 'npc.resident.01', actor_kind: 'npc', public_label: 'Resident 01',
        appearance_variant: 'resident.ochre', lifecycle_status: 'active',
        x: 1, y: 1, z: 0, motion_state: 'idle', position_revision: 1, last_frame_sequence: 0
      },
      {
        actor_code: 'npc.resident.01', actor_kind: 'npc', public_label: 'resident@example.com',
        appearance_variant: 'resident.ochre', lifecycle_status: 'active',
        x: 2, y: 1, z: 0, motion_state: 'idle', position_revision: 1, last_frame_sequence: 0
      }
    ])).toThrow('Invalid realtime public actor')
  })

  it('accepts the bounded Unicode player-name contract and resolves the player sprite', () => {
    const index = indexRealtimePublicActors([
      {
        actor_code: 'character.player.0123456789abcdef0123456789abcdef', actor_kind: 'character', public_label: '春日 花子',
        appearance_variant: 'player.cobalt', lifecycle_status: 'active',
        x: 4, y: 6, z: 0, motion_state: 'idle', position_revision: 1, last_frame_sequence: 2
      }
    ])

    expect(index.get(realtimeActorCellKey(4, 6, 0))?.[0]?.public_label).toBe('春日 花子')
    expect(resolveRealtimeActorSpritePalette('player.cobalt')).toEqual({
      body: '#4b86d1', accent: '#b9dafc', outline: '#17283e'
    })
    expect(() => indexRealtimePublicActors([
      {
        actor_code: 'character.player.0123456789abcdef0123456789abcdef', actor_kind: 'character', public_label: '玩家<script>',
        appearance_variant: 'player.cobalt', lifecycle_status: 'active',
        x: 4, y: 6, z: 0, motion_state: 'idle', position_revision: 1, last_frame_sequence: 2
      }
    ])).toThrow('Invalid realtime public actor')
  })
})
