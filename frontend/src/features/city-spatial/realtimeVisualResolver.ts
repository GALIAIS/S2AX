import type { CityRealtimeSemanticLayer, CityRealtimeVisualManifestPayload } from '@/api/citySpatial'

export interface RealtimePixelPalette {
  mapBackground: string
  ground: string
  soil: string
  road: string
  water: string
  buildingResidential: string
  buildingCommercial: string
  buildingIndustrial: string
  structure: string
  portal: string
  furniture: string
  item: string
  entity: string
  overlay: string
  window: string
}

const fallbackPalette: RealtimePixelPalette = {
  mapBackground: '#162018',
  ground: '#5f8259',
  soil: '#a57a50',
  road: '#77736b',
  water: '#3b6f97',
  buildingResidential: '#b66f69',
  buildingCommercial: '#d29a55',
  buildingIndustrial: '#8393a4',
  structure: '#343332',
  portal: '#e1bd66',
  furniture: '#aa704a',
  item: '#dda971',
  entity: '#e7eef2',
  overlay: '#70b8aa',
  window: '#92bed1'
}

const semanticKeys: Record<keyof RealtimePixelPalette, string> = {
  mapBackground: 'map_background',
  ground: 'ground',
  soil: 'soil',
  road: 'road',
  water: 'water',
  buildingResidential: 'building_residential',
  buildingCommercial: 'building_commercial',
  buildingIndustrial: 'building_industrial',
  structure: 'structure',
  portal: 'portal',
  furniture: 'furniture',
  item: 'item',
  entity: 'entity',
  overlay: 'overlay',
  window: 'window'
}

export function resolveRealtimePixelPalette(
  manifest: CityRealtimeVisualManifestPayload | null | undefined,
  profileID: string | undefined
): RealtimePixelPalette {
  const palette = manifest?.profile_palettes?.[profileID ?? ''] ?? manifest?.profile_palettes?.default
  if (!palette) return { ...fallbackPalette }
  const resolved = { ...fallbackPalette }
  for (const [property, semanticKey] of Object.entries(semanticKeys) as Array<[keyof RealtimePixelPalette, string]>) {
    const candidate = palette[semanticKey]
    if (isSafeHexColor(candidate)) resolved[property] = candidate.toLowerCase()
  }
  return resolved
}

export function resolveRealtimeTerrainColor(definitionID: string, palette: RealtimePixelPalette): string {
  if (definitionID.includes('deep_water') || definitionID.includes('water')) return palette.water
  if (definitionID.includes('road')) return palette.road
  if (definitionID.includes('floor')) return palette.soil
  if (definitionID.includes('soil') || definitionID.includes('sand')) return palette.soil
  if (definitionID.includes('grass')) return palette.ground
  return palette.ground
}

export function resolveRealtimeBuildingColor(primaryUse: string, palette: RealtimePixelPalette): string {
  if (primaryUse === 'commercial') return palette.buildingCommercial
  if (primaryUse === 'industrial') return palette.buildingIndustrial
  return palette.buildingResidential
}

export function resolveRealtimeLayerColor(layer: CityRealtimeSemanticLayer, palette: RealtimePixelPalette): string {
  if (layer.kind === 'portal') return palette.portal
  if (layer.kind === 'furniture') return palette.furniture
  if (layer.kind === 'item') return palette.item
  if (layer.kind === 'entity') return palette.entity
  if (layer.kind === 'field' || layer.kind === 'overlay') return palette.overlay
  if (layer.definition_id.includes('window')) return palette.window
  return palette.structure
}

function isSafeHexColor(value: unknown): value is string {
  return typeof value === 'string' && /^#[0-9a-f]{6}$/i.test(value)
}
