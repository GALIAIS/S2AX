import { describe, expect, it } from 'vitest'
import { CityChunkCache } from '../chunkCache'
import type { ProjectedCityChunk } from '../projection'

function projected(key: string): ProjectedCityChunk {
  return {
    key,
    chunkX: 0,
    chunkY: 0,
    z: 0,
    width: 1,
    height: 1,
    revision: 1,
    payloadHash: key,
    districtCode: 'central',
    generatedTick: 0,
    cells: []
  }
}

describe('CityChunkCache', () => {
  it('evicts the least recently used Chunk', () => {
    const cache = new CityChunkCache(2)
    cache.set(projected('a'))
    cache.set(projected('b'))
    expect(cache.get('a')?.key).toBe('a')
    cache.set(projected('c'))
    expect(cache.peek('a')).toBeDefined()
    expect(cache.peek('b')).toBeUndefined()
    expect(cache.peek('c')).toBeDefined()
  })

  it('publishes an isolated read-only snapshot', () => {
    const cache = new CityChunkCache(2)
    cache.set(projected('a'))
    const snapshot = cache.snapshot()
    cache.clear()
    expect(snapshot.has('a')).toBe(true)
    expect(cache.size).toBe(0)
  })

  it('rejects invalid capacity', () => {
    expect(() => new CityChunkCache(0)).toThrow('capacity')
  })
})
