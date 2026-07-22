import { describe, expect, it } from 'vitest'
import {
  resolveRealtimeBuildingColor,
  resolveRealtimeLayerColor,
  resolveRealtimePixelPalette,
  resolveRealtimeTerrainColor
} from '../realtimeVisualResolver'

describe('realtime visual resolver', () => {
  it('uses only validated palette colors for a bound profile', () => {
    const palette = resolveRealtimePixelPalette({
      schema_version: 1,
      render_mode: 'procedural_pixel_v1',
      logical_tile_px: 16,
      profile_palettes: {
        'jp.metropolitan': { ground: '#6B9468', road: '#6d7370', water: 'url(javascript:bad)' }
      }
    }, 'jp.metropolitan')

    expect(palette.ground).toBe('#6b9468')
    expect(palette.road).toBe('#6d7370')
    expect(palette.water).toBe('#3b6f97')
  })

  it('resolves semantic facts without treating visual data as authority', () => {
    const palette = resolveRealtimePixelPalette(undefined, undefined)
    expect(resolveRealtimeTerrainColor('terrain.deep_water', palette)).toBe(palette.water)
    expect(resolveRealtimeTerrainColor('terrain.road', palette)).toBe(palette.road)
    expect(resolveRealtimeBuildingColor('commercial', palette)).toBe(palette.buildingCommercial)
    expect(resolveRealtimeLayerColor({ x: 0, y: 0, kind: 'portal', definition_id: 'portal.entrance' }, palette)).toBe(palette.portal)
  })
})
