import type { CityRealtimePublicActor } from '@/api/citySpatial'

export interface RealtimeActorSpritePalette {
  body: string
  accent: string
  outline: string
}

const actorCodePattern = /^[a-z][a-z0-9_.-]{1,95}$/
const knownMotionStates = new Set(['idle', 'walking', 'inside', 'unavailable'])
const knownActorKinds = new Set(['npc', 'character', 'service'])
const knownLifecycleStates = new Set(['active', 'inactive', 'retired'])

const spritePalettes: Record<string, RealtimeActorSpritePalette> = {
  'resident.ochre': { body: '#d59b4d', accent: '#f2d58a', outline: '#30271d' },
  'resident.teal': { body: '#4cae9c', accent: '#9cd8cb', outline: '#17312d' },
  'resident.indigo': { body: '#7287cf', accent: '#bdc8f3', outline: '#202947' },
  'resident.rose': { body: '#c46d83', accent: '#efb8c5', outline: '#41212b' },
  'resident.slate': { body: '#8a99a3', accent: '#c5d0d5', outline: '#2a3338' },
  'resident.olive': { body: '#929a51', accent: '#d2d69a', outline: '#32351e' },
  'player.cobalt': { body: '#4b86d1', accent: '#b9dafc', outline: '#17283e' }
}

const fallbackPalette: RealtimeActorSpritePalette = {
  body: '#9a8461', accent: '#e6d2ac', outline: '#29231b'
}

export function realtimeActorCellKey(x: number, y: number, z: number): string {
  return `${x}:${y}:${z}`
}

function publicLabelValid(value: unknown): value is string {
  if (typeof value !== 'string' || value.length === 0 || value.trim() !== value || Array.from(value).length > 64) return false
  for (const character of value) {
    if (/^[\p{L}\p{N}]$/u.test(character)) continue
    if (character === ' ' || character === '.' || character === '_' || character === '-' ||
        character === "'" || character === '·' || character === '・') continue
    return false
  }
  return true
}

// The client validates only the bounded, public rendering contract. This is
// intentionally strict so a malformed response cannot create unbounded map
// indexes or turn an account/agent field into a visible map label.
export function indexRealtimePublicActors(
  actors: CityRealtimePublicActor[],
  maximumActorCount = 256
): Map<string, CityRealtimePublicActor[]> {
  if (!Array.isArray(actors) || !Number.isInteger(maximumActorCount) || maximumActorCount <= 0 ||
      actors.length > maximumActorCount) {
    throw new Error('Invalid realtime actor snapshot')
  }
  const actorCodes = new Set<string>()
  const index = new Map<string, CityRealtimePublicActor[]>()
  for (const actor of actors) {
    if (!actor || !actorCodePattern.test(actor.actor_code) || !publicLabelValid(actor.public_label) ||
        !knownActorKinds.has(actor.actor_kind) || !knownLifecycleStates.has(actor.lifecycle_status) ||
        !knownMotionStates.has(actor.motion_state) || typeof actor.appearance_variant !== 'string' ||
        !/^[a-z][a-z0-9_.-]{1,63}$/.test(actor.appearance_variant) ||
        !Number.isSafeInteger(actor.x) || !Number.isSafeInteger(actor.y) || !Number.isSafeInteger(actor.z) ||
        !Number.isSafeInteger(actor.position_revision) || actor.position_revision <= 0 ||
        !Number.isSafeInteger(actor.last_frame_sequence) || actor.last_frame_sequence < 0 || actorCodes.has(actor.actor_code)) {
      throw new Error('Invalid realtime public actor')
    }
    actorCodes.add(actor.actor_code)
    const key = realtimeActorCellKey(actor.x, actor.y, actor.z)
    const cell = index.get(key) ?? []
    cell.push(actor)
    index.set(key, cell)
  }
  for (const cell of index.values()) cell.sort((left, right) => left.actor_code.localeCompare(right.actor_code))
  return index
}

export function resolveRealtimeActorSpritePalette(appearanceVariant: string): RealtimeActorSpritePalette {
  return spritePalettes[appearanceVariant] ?? fallbackPalette
}
