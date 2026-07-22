import { describe, expect, it } from 'vitest'
import {
  decodeRealtimeChunkPayload,
  floorDivide,
  realtimeChunkKey
} from '../realtimePixelProjection'

describe('realtime pixel projection helpers', () => {
  it('decodes bounded semantic terrain runs and preserves layer coordinates', () => {
    const decoded = decodeRealtimeChunkPayload({
      format: 'city-openworld-chunk-v1',
      width: 2,
      height: 2,
      terrain_runs: [
        { definition_id: 'terrain.grass', length: 2 },
        { definition_id: 'terrain.road', length: 2 }
      ],
      layers: [{ x: 1, y: 0, kind: 'structure', definition_id: 'structure.wall' }]
    })

    expect(decoded.terrain).toEqual([
      'terrain.grass', 'terrain.grass', 'terrain.road', 'terrain.road'
    ])
    expect(decoded.layersByCell.get(1)).toEqual([
      { x: 1, y: 0, kind: 'structure', definition_id: 'structure.wall' }
    ])
  })

  it('keeps negative world coordinates in their mathematical chunk', () => {
    expect(floorDivide(-1, 32)).toBe(-1)
    expect(floorDivide(-32, 32)).toBe(-1)
    expect(floorDivide(-33, 32)).toBe(-2)
    expect(realtimeChunkKey(-2, 3)).toBe('-2:3:0')
  })
})
