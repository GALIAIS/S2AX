import type {
  CityBuilding,
  CityBuildingLayoutCell,
  CityBuildingPortal,
  CityBuildingUnitPool,
  CityDevelopmentProject,
  CityDevelopmentState,
  CityEnterpriseLocationState,
  CityEnterpriseSite,
  CityHousingAllocation,
  CityLandState,
  CityLandUse,
  CityMapChunk,
  CityOpenWorldBuildingInterior,
  CityOpenWorldChunk,
  CityOvermapTile,
  CityParcelLayoutCell,
  CityParcel,
  CitySpatialDefinition,
  CitySpatialRuleKind,
  CitySpatialRuleSet,
  WorldActor
} from '@/api/citySpatial'

export interface ClassicVisual {
  definition: CitySpatialDefinition
  glyph: string
  foreground: string
  background?: string
  glyphSourceID: string
  fallbackPath: string[]
}

export interface ProjectedCellLayer {
  kind: CitySpatialRuleKind
  definitionID: string
  name: string
  glyph: string
  movementCost: number
  flags: string[]
}

export interface ProjectedCityCell {
  worldX: number
  worldY: number
  z: number
  chunkX: number
  chunkY: number
  localX: number
  localY: number
  glyph: string
  foreground: string
  background: string
  terrainDefinitionID: string
  furnitureDefinitionID?: string
  stack: ProjectedCellLayer[]
}

export interface ProjectedCityChunk {
  key: string
  chunkX: number
  chunkY: number
  z: number
  width: number
  height: number
  revision: number
  payloadHash: string
  districtCode: string
  generatedTick: number
  cells: ProjectedCityCell[]
}

export interface CameraState {
  worldX: number
  worldY: number
  z: number
  cellSize: number
}

export interface ViewportSize {
  width: number
  height: number
}

export interface ClassicSceneCell extends ProjectedCityCell {
  column: number
  row: number
}

export interface ClassicLocalScene {
  mode: 'local'
  width: number
  height: number
  cellSize: number
  columns: number
  rows: number
  startWorldX: number
  startWorldY: number
  cells: Array<ClassicSceneCell | null>
}

export interface ClassicOvermapCell {
  tile: CityOvermapTile
  glyph: string
  foreground: string
  background: string
  landUses: CityLandUse[]
  parcelCount: number
  buildingCount: number
  activeProjectCount: number
  completedProjectCount: number
  activeEnterpriseSiteCount: number
  enterpriseFirmCount: number
  enterpriseOccupiedUnits: number
  actorCount: number
  actorCodes: string[]
  x: number
  y: number
  size: number
}

export interface ClassicOvermapScene {
  mode: 'overmap'
  width: number
  height: number
  cellSize: number
  offsetX: number
  offsetY: number
  cells: ClassicOvermapCell[]
}

export type ClassicScene = ClassicLocalScene | ClassicOvermapScene

export interface ChunkViewportBounds {
  min_x: number
  max_x: number
  min_y: number
  max_y: number
  z: number
}

export interface CityLandCellContext {
  parcel: CityParcel | null
  building: CityBuilding | null
  layoutCell: CityBuildingLayoutCell | null
  layoutArchetype: string | null
  parcelLayoutCell: CityParcelLayoutCell | null
  parcelLayoutStyle: string | null
  unitPools: CityBuildingUnitPool[]
  housingAllocations: CityHousingAllocation[]
  portals: CityBuildingPortal[]
}

export interface CityLandTileSummary {
  landUses: CityLandUse[]
  parcels: CityParcel[]
  buildings: CityBuilding[]
}

const ANSI16 = [
  '#000000', '#800000', '#008000', '#808000', '#000080', '#800080', '#008080', '#c0c0c0',
  '#808080', '#ff0000', '#00ff00', '#ffff00', '#0000ff', '#ff00ff', '#00ffff', '#ffffff'
] as const

const DEFAULT_MAP_BACKGROUND = '#0d0f12'
const UNLOADED_BACKGROUND = '#111318'
const LAND_USE_ORDER: CityLandUse[] = ['residential', 'commercial', 'industrial']
const LAND_USE_COLORS: Record<CityLandUse, string> = {
  residential: '#d6c6a5',
  commercial: '#6fa8c7',
  industrial: '#c58b57'
}

export function cityLandUseColor(use: CityLandUse): string {
  return LAND_USE_COLORS[use]
}

interface LandProjectionIndex {
  parcelsByCode: Map<string, CityParcel>
  parcelsByCell: Map<string, CityParcel>
  buildingsByCode: Map<string, CityBuilding>
  buildingsByCell: Map<string, CityBuilding>
  layoutCellsByCell: Map<string, CityBuildingLayoutCell>
  layoutBuildingCodes: Set<string>
  layoutArchetypesByBuilding: Map<string, string>
  parcelLayoutCellsByCell: Map<string, CityParcelLayoutCell>
  parcelLayoutStylesByParcel: Map<string, string>
  poolsByBuilding: Map<string, CityBuildingUnitPool[]>
  allocationsByPool: Map<string, CityHousingAllocation[]>
  portalsByCell: Map<string, CityBuildingPortal[]>
}

const landProjectionCache = new WeakMap<CityLandState, Map<string, LandProjectionIndex>>()
const developmentProjectCache = new WeakMap<CityDevelopmentState, Map<string, CityDevelopmentProject[]>>()
const enterpriseSiteCache = new WeakMap<CityEnterpriseLocationState, Map<string, CityEnterpriseSite[]>>()

function coordinateKey(worldX: number, worldY: number, z: number): string {
  return `${worldX}/${worldY}/${z}`
}

function rectangleWorldBounds(
  rectangle: CityParcel['geometry'],
  chunkSize: number
): { minX: number; maxX: number; minY: number; maxY: number } {
  return {
    minX: rectangle.chunk_x * chunkSize + rectangle.local_min_x,
    maxX: rectangle.chunk_x * chunkSize + rectangle.local_max_x,
    minY: rectangle.chunk_y * chunkSize + rectangle.local_min_y,
    maxY: rectangle.chunk_y * chunkSize + rectangle.local_max_y
  }
}

function appendToIndex<T>(index: Map<string, T[]>, key: string, value: T): void {
  const values = index.get(key)
  if (values) values.push(value)
  else index.set(key, [value])
}

function createLandProjectionIndex(
  land: CityLandState,
  chunkSize: number,
  z: number
): LandProjectionIndex {
  const cacheKey = `${chunkSize}/${z}`
  let stateCache = landProjectionCache.get(land)
  if (!stateCache) {
    stateCache = new Map()
    landProjectionCache.set(land, stateCache)
  }
  const cached = stateCache.get(cacheKey)
  if (cached) return cached

  const index: LandProjectionIndex = {
    parcelsByCode: new Map(land.parcels.map(parcel => [parcel.code, parcel])),
    parcelsByCell: new Map(),
    buildingsByCode: new Map(land.buildings.map(building => [building.code, building])),
    buildingsByCell: new Map(),
    layoutCellsByCell: new Map(),
    layoutBuildingCodes: new Set(),
    layoutArchetypesByBuilding: new Map(),
    parcelLayoutCellsByCell: new Map(),
    parcelLayoutStylesByParcel: new Map(),
    poolsByBuilding: new Map(),
    allocationsByPool: new Map(),
    portalsByCell: new Map()
  }
  for (const pool of land.unit_pools) appendToIndex(index.poolsByBuilding, pool.building_code, pool)
  for (const allocation of land.housing_allocations) {
    appendToIndex(index.allocationsByPool, allocation.pool_code, allocation)
  }
  for (const parcel of land.parcels) {
    if (parcel.geometry.z !== z) continue
    const bounds = rectangleWorldBounds(parcel.geometry, chunkSize)
    for (let worldY = bounds.minY; worldY <= bounds.maxY; worldY += 1) {
      for (let worldX = bounds.minX; worldX <= bounds.maxX; worldX += 1) {
        index.parcelsByCell.set(coordinateKey(worldX, worldY, z), parcel)
      }
    }
  }
  for (const layout of land.building_layouts ?? []) {
    const building = index.buildingsByCode.get(layout.building_code)
    if (!building) continue
    index.layoutBuildingCodes.add(building.code)
    index.layoutArchetypesByBuilding.set(building.code, layout.archetype)
    for (const layoutCell of layout.cells) {
      if (layoutCell.z !== z) continue
      const key = coordinateKey(layoutCell.x, layoutCell.y, z)
      index.layoutCellsByCell.set(key, layoutCell)
      index.buildingsByCell.set(key, building)
    }
  }
  for (const layout of land.parcel_layouts ?? []) {
    index.parcelLayoutStylesByParcel.set(layout.parcel_code, layout.style)
    for (const layoutCell of layout.cells) {
      if (layoutCell.z !== z) continue
      index.parcelLayoutCellsByCell.set(coordinateKey(layoutCell.x, layoutCell.y, z), layoutCell)
    }
  }
  for (const building of land.buildings) {
    if (z < building.base_z || z > building.top_z) continue
    if (index.layoutBuildingCodes.has(building.code)) continue
    const bounds = rectangleWorldBounds(building.footprint, chunkSize)
    for (let worldY = bounds.minY; worldY <= bounds.maxY; worldY += 1) {
      for (let worldX = bounds.minX; worldX <= bounds.maxX; worldX += 1) {
        index.buildingsByCell.set(coordinateKey(worldX, worldY, z), building)
      }
    }
  }
  for (const portal of land.portals) {
    if (portal.from_z === z) {
      appendToIndex(index.portalsByCell, coordinateKey(portal.from_x, portal.from_y, z), portal)
    }
    if (
      portal.to_z === z &&
      (portal.to_x !== portal.from_x || portal.to_y !== portal.from_y || portal.to_z !== portal.from_z)
    ) {
      appendToIndex(index.portalsByCell, coordinateKey(portal.to_x, portal.to_y, z), portal)
    }
  }
  stateCache.set(cacheKey, index)
  return index
}

export function getCityLandCellContext(
  land: CityLandState | null | undefined,
  worldX: number,
  worldY: number,
  z: number,
  chunkSize: number
): CityLandCellContext | null {
  if (!land || chunkSize <= 0) return null
  const index = createLandProjectionIndex(land, chunkSize, z)
  const key = coordinateKey(worldX, worldY, z)
  const portals = [...(index.portalsByCell.get(key) ?? [])]
  const layoutCell = index.layoutCellsByCell.get(key) ?? null
  const parcelLayoutCell = index.parcelLayoutCellsByCell.get(key) ?? null
  const building = index.buildingsByCell.get(key)
    ?? (portals[0] ? index.buildingsByCode.get(portals[0].building_code) : undefined)
    ?? null
  const parcel = index.parcelsByCell.get(key)
    ?? (building ? index.parcelsByCode.get(building.parcel_code) : undefined)
    ?? null
  if (!parcel && !building && portals.length === 0 && !parcelLayoutCell) return null
  const unitPools = building ? [...(index.poolsByBuilding.get(building.code) ?? [])] : []
  const housingAllocations = unitPools.flatMap(pool => index.allocationsByPool.get(pool.code) ?? [])
  const layoutArchetype = building ? index.layoutArchetypesByBuilding.get(building.code) ?? null : null
  const parcelLayoutStyle = parcel ? index.parcelLayoutStylesByParcel.get(parcel.code) ?? null : null
  return {
    parcel,
    building,
    layoutCell,
    layoutArchetype,
    parcelLayoutCell,
    parcelLayoutStyle,
    unitPools,
    housingAllocations,
    portals
  }
}

export function getCityLandTileSummary(
  land: CityLandState | null | undefined,
  tile: Pick<CityOvermapTile, 'chunk_x' | 'chunk_y' | 'z'>
): CityLandTileSummary {
  if (!land) return { landUses: [], parcels: [], buildings: [] }
  const parcels = land.parcels.filter(parcel => (
    parcel.geometry.chunk_x === tile.chunk_x &&
    parcel.geometry.chunk_y === tile.chunk_y &&
    parcel.geometry.z === tile.z
  ))
  const buildings = land.buildings.filter(building => (
    building.footprint.chunk_x === tile.chunk_x &&
    building.footprint.chunk_y === tile.chunk_y &&
    tile.z >= building.base_z && tile.z <= building.top_z
  ))
  const uses = new Set<CityLandUse>([
    ...parcels.map(parcel => parcel.zone_code as CityLandUse),
    ...buildings.map(building => building.primary_use)
  ])
  return {
    landUses: LAND_USE_ORDER.filter(use => uses.has(use)),
    parcels,
    buildings
  }
}

function landOverlayLayer(
  kind: CitySpatialRuleKind,
  definitionID: string,
  name: string,
  glyph: string,
  flags: string[]
): ProjectedCellLayer {
  return { kind, definitionID, name, glyph, movementCost: 0, flags }
}

interface BuildingLayoutVisual {
  kind: CitySpatialRuleKind
  definitionID: string
  glyph: string
  foreground: string
  background?: string
}

interface ParcelLayoutVisual {
  kind: CitySpatialRuleKind
  definitionID: string
  glyph: string
  foreground: string
  background?: string
}

const BUILDING_LAYOUT_FURNITURE_GLYPHS: Record<string, string> = {
  bed: 'H',
  table: 'T',
  chair: 'h',
  counter: '=',
  shelf: 's',
  machine: 'M',
  crate: 'B'
}

function resolveBuildingLayoutVisual(
  layoutCell: CityBuildingLayoutCell,
  building: CityBuilding
): BuildingLayoutVisual {
  const structure = LAND_USE_COLORS[building.primary_use]
  switch (layoutCell.kind) {
    case 'wall':
      return {
        kind: 'structure', definitionID: 'structure.wall', glyph: '#',
        foreground: structure, background: '#26282d'
      }
    case 'window':
      return {
        kind: 'structure', definitionID: 'structure.window', glyph: '□',
        foreground: '#9cc4d5', background: '#26282d'
      }
    case 'door':
      return {
        kind: 'portal', definitionID: 'portal.door_open', glyph: '+',
        foreground: '#f0c674', background: '#25272c'
      }
    case 'furniture':
      return {
        kind: 'furniture', definitionID: 'furniture.table',
        glyph: BUILDING_LAYOUT_FURNITURE_GLYPHS[layoutCell.feature ?? ''] ?? '*',
        foreground: '#c8af82', background: '#25272c'
      }
    default:
      if (layoutCell.feature === 'stairs') {
        return {
          kind: 'portal', definitionID: 'portal.stairs', glyph: '↕',
          foreground: '#89c5e9', background: '#25272c'
        }
      }
      return {
        kind: 'terrain', definitionID: 'terrain.floor', glyph: '·',
        foreground: '#d3d7dc', background: '#25272c'
      }
  }
}

function resolveParcelLayoutVisual(layoutCell: CityParcelLayoutCell): ParcelLayoutVisual {
  switch (layoutCell.kind) {
    case 'tree':
      return {
        kind: 'furniture', definitionID: 'furniture.tree', glyph: '♣', foreground: '#78a56d'
      }
    case 'garden':
      return {
        kind: 'terrain', definitionID: 'terrain.grass', glyph: '"', foreground: '#91b879', background: '#20241e'
      }
    case 'sidewalk':
      return {
        kind: 'terrain', definitionID: 'terrain.sidewalk', glyph: ':', foreground: '#c5c7ca', background: '#2b2d31'
      }
    case 'parking':
      return {
        kind: 'terrain', definitionID: 'terrain.road', glyph: '=', foreground: '#acb0b8', background: '#303238'
      }
    case 'loading':
      return {
        kind: 'overlay', definitionID: 'site.loading', glyph: '▤', foreground: '#d6a36c', background: '#302a24'
      }
    default:
      return {
        kind: 'terrain', definitionID: 'terrain.sidewalk', glyph: '·', foreground: '#d8d2be', background: '#292a28'
      }
  }
}

function enterpriseSitesByBuilding(
  enterprise: CityEnterpriseLocationState | null | undefined
): ReadonlyMap<string, CityEnterpriseSite[]> {
  if (!enterprise) return new Map()
  const cached = enterpriseSiteCache.get(enterprise)
  if (cached) return cached
  const sites = new Map<string, CityEnterpriseSite[]>()
  for (const site of enterprise.sites) {
    if (site.status === 'active') appendToIndex(sites, site.building_code, site)
  }
  enterpriseSiteCache.set(enterprise, sites)
  return sites
}

export function getCityEnterpriseSitesForBuilding(
  enterprise: CityEnterpriseLocationState | null | undefined,
  buildingCode: string | null | undefined
): CityEnterpriseSite[] {
  if (!buildingCode) return []
  return [...(enterpriseSitesByBuilding(enterprise).get(buildingCode) ?? [])]
}

export function applyCityEnterpriseOverlay(
  cell: ProjectedCityCell,
  land: CityLandState | null | undefined,
  enterprise: CityEnterpriseLocationState | null | undefined,
  chunkSize: number
): ProjectedCityCell {
  const context = getCityLandCellContext(land, cell.worldX, cell.worldY, cell.z, chunkSize)
  const building = context?.building
  if (!building) return cell
  const sites = getCityEnterpriseSitesForBuilding(enterprise, building.code)
  if (sites.length === 0) return cell
  const bounds = rectangleWorldBounds(building.footprint, chunkSize)
  const isAnchor = cell.z === building.base_z && cell.worldX === bounds.minX && cell.worldY === bounds.minY
  if (!isAnchor) return cell
  const portalVisible = cell.stack.some(layer => layer.kind === 'portal')
  const glyph = portalVisible ? cell.glyph : '&'
  return {
    ...cell,
    glyph,
    foreground: portalVisible ? cell.foreground : '#6fa8c7',
    stack: [
      ...cell.stack,
      landOverlayLayer(
        'entity',
        `enterprise:${building.code}`,
        sites.map(site => site.name).join(' / '),
        glyph,
        ['enterprise', `sites:${sites.length}`, ...sites.map(site => `site:${site.code}`)]
      )
    ]
  }
}

export function applyCityLandOverlay(
  cell: ProjectedCityCell,
  land: CityLandState | null | undefined,
  chunkSize: number
): ProjectedCityCell {
  const context = getCityLandCellContext(land, cell.worldX, cell.worldY, cell.z, chunkSize)
  if (!context) return cell
  const building = context.building
  let projected = cell
  if (building) {
    if (context.layoutCell) {
      const visual = resolveBuildingLayoutVisual(context.layoutCell, building)
      const feature = context.layoutCell.feature
      projected = {
        ...projected,
        glyph: visual.glyph,
        foreground: visual.foreground,
        background: visual.background ?? projected.background,
        stack: [
          ...projected.stack,
          landOverlayLayer(
            visual.kind,
            visual.definitionID,
            building.code + ' · ' + context.layoutCell.kind,
            visual.glyph,
            [
              'building', 'layout', 'kind:' + context.layoutCell.kind,
              'archetype:' + (context.layoutArchetype ?? 'legacy'),
              building.primary_use,
              ...(feature ? ['feature:' + feature] : [])
            ]
          )
        ]
      }
    } else {
      const bounds = rectangleWorldBounds(building.footprint, chunkSize)
      const inside = cell.worldX >= bounds.minX && cell.worldX <= bounds.maxX &&
        cell.worldY >= bounds.minY && cell.worldY <= bounds.maxY
      if (inside) {
        const edge = cell.worldX === bounds.minX || cell.worldX === bounds.maxX ||
          cell.worldY === bounds.minY || cell.worldY === bounds.maxY
        const glyph = edge ? '#' : '·'
        projected = {
          ...projected,
          glyph,
          foreground: LAND_USE_COLORS[building.primary_use],
          stack: [
            ...projected.stack,
            landOverlayLayer(
              'structure',
              'building:' + building.code,
              building.code,
              glyph,
              edge ? ['building', 'edge', building.primary_use] : ['building', 'floor', building.primary_use]
            )
          ]
        }
      }
    }
  } else if (context.parcelLayoutCell) {
    const visual = resolveParcelLayoutVisual(context.parcelLayoutCell)
    projected = {
      ...projected,
      glyph: visual.glyph,
      foreground: visual.foreground,
      background: visual.background ?? projected.background,
      stack: [
        ...projected.stack,
        landOverlayLayer(
          visual.kind,
          visual.definitionID,
          (context.parcel?.code ?? 'site') + ' · ' + context.parcelLayoutCell.kind,
          visual.glyph,
          [
            'parcel',
            'site_layout',
            'kind:' + context.parcelLayoutCell.kind,
            ...(context.parcelLayoutStyle ? ['style:' + context.parcelLayoutStyle] : [])
          ]
        )
      ]
    }
  }
  if (context.portals.length > 0) {
    const portal = context.portals[0]
    const glyph = portal.portal_type === 'stair' ? '↕' : '+'
    projected = {
      ...projected,
      glyph,
      foreground: '#f0c674',
      stack: [
        ...projected.stack,
        landOverlayLayer('portal', `portal:${portal.building_code}:${portal.code}`, portal.code, glyph, [portal.portal_type])
      ]
    }
  }
  return projected
}

function developmentProjectsByBuilding(
  development: CityDevelopmentState | null | undefined
): ReadonlyMap<string, CityDevelopmentProject[]> {
  if (!development) return new Map()
  const cached = developmentProjectCache.get(development)
  if (cached) return cached
  const projects = new Map<string, CityDevelopmentProject[]>()
  for (const project of development.projects) appendToIndex(projects, project.building_code, project)
  developmentProjectCache.set(development, projects)
  return projects
}

export function getCityDevelopmentProjectsForBuilding(
  development: CityDevelopmentState | null | undefined,
  buildingCode: string | null | undefined
): CityDevelopmentProject[] {
  if (!buildingCode) return []
  return [...(developmentProjectsByBuilding(development).get(buildingCode) ?? [])]
}

export function applyCityDevelopmentOverlay(
  cell: ProjectedCityCell,
  land: CityLandState | null | undefined,
  development: CityDevelopmentState | null | undefined,
  chunkSize: number
): ProjectedCityCell {
  const context = getCityLandCellContext(land, cell.worldX, cell.worldY, cell.z, chunkSize)
  const building = context?.building
  if (!building) return cell
  const project = getCityDevelopmentProjectsForBuilding(development, building.code)
    .find(item => item.status === 'under_construction')
  if (!project) return cell
  const bounds = rectangleWorldBounds(building.footprint, chunkSize)
  const centerX = Math.floor((bounds.minX + bounds.maxX) / 2)
  const centerY = Math.floor((bounds.minY + bounds.maxY) / 2)
  const marker = cell.worldX === centerX && cell.worldY === centerY
  const glyph = marker ? '%' : cell.glyph
  return {
    ...cell,
    glyph,
    foreground: '#d99b52',
    stack: [
      ...cell.stack,
      landOverlayLayer(
        'overlay',
        `development:${project.code}`,
        project.name || project.code,
        glyph,
        ['development', 'under_construction', `progress:${project.progress_milli}`]
      )
    ]
  }
}

export function applyWorldActorOverlay(
  cell: ProjectedCityCell,
  actors: readonly WorldActor[]
): ProjectedCityCell {
  const present = actors.filter(actor => actor.location &&
    actor.location.x === cell.worldX &&
    actor.location.y === cell.worldY &&
    actor.location.z === cell.z)
  if (present.length === 0) return cell
  const glyph = present.length === 1 ? '@' : '&'
  return {
    ...cell,
    glyph,
    foreground: '#f4f7fb',
    stack: [
      ...cell.stack,
      landOverlayLayer(
        'entity',
        `actor:${present.map(actor => actor.code).join('+')}`,
        present.map(actor => actor.name).join(' / '),
        glyph,
        ['actor', `count:${present.length}`, ...present.map(actor => `actor:${actor.code}`)]
      )
    ]
  }
}

export function xterm256Color(index: number): string {
  const value = Math.max(0, Math.min(255, Math.trunc(index)))
  if (value < 16) return ANSI16[value]
  if (value < 232) {
    const cube = value - 16
    const red = Math.floor(cube / 36)
    const green = Math.floor((cube % 36) / 6)
    const blue = cube % 6
    const level = (channel: number): number => channel === 0 ? 0 : 55 + channel * 40
    return `#${[level(red), level(green), level(blue)]
      .map(channel => channel.toString(16).padStart(2, '0'))
      .join('')}`
  }
  const gray = 8 + (value - 232) * 10
  const hex = gray.toString(16).padStart(2, '0')
  return `#${hex}${hex}${hex}`
}

export function chunkKey(chunkX: number, chunkY: number, z: number): string {
  return `z:${z}/x:${chunkX}/y:${chunkY}`
}

export function floorDiv(value: number, divisor: number): number {
  if (!Number.isInteger(value) || !Number.isInteger(divisor) || divisor <= 0) {
    throw new Error('Invalid integer coordinate')
  }
  return Math.floor(value / divisor)
}

function definitionMaps(ruleSet: CitySpatialRuleSet): {
  definitions: Map<string, CitySpatialDefinition>
  palette: Map<string, { foreground: string; background?: string }>
} {
  const definitions = new Map(ruleSet.definitions.map(definition => [definition.id, definition]))
  const palette = new Map(
    ruleSet.palette.map(entry => [
      entry.id,
      {
        foreground: xterm256Color(entry.classic_foreground),
        background: entry.classic_background === undefined
          ? undefined
          : xterm256Color(entry.classic_background)
      }
    ])
  )
  return { definitions, palette }
}

export function createClassicVisualResolver(ruleSet: CitySpatialRuleSet): (
  kind: CitySpatialRuleKind,
  requestedDefinitionID: string
) => ClassicVisual {
  const { definitions, palette } = definitionMaps(ruleSet)
  const cache = new Map<string, ClassicVisual>()
  return (kind, requestedDefinitionID) => {
    const cacheKey = `${kind}:${requestedDefinitionID}`
    const cached = cache.get(cacheKey)
    if (cached) return cached
    const requested = definitions.get(requestedDefinitionID)
    const missingID = `missing.${kind}`
    const original = requested?.kind === kind ? requested : definitions.get(missingID)
    if (!original) throw new Error(`Missing spatial fallback definition: ${missingID}`)

    const fallbackPath: string[] = []
    let current: CitySpatialDefinition | undefined = original
    let glyph = ''
    let glyphSourceID = ''
    for (let depth = 0; current && depth <= 32; depth += 1) {
      fallbackPath.push(current.id)
      if (!glyph && current.glyph) {
        glyph = current.glyph
        glyphSourceID = current.id
      }
      if (!current.looks_like) break
      current = definitions.get(current.looks_like)
    }
    if (!glyph) throw new Error(`Spatial definition has no glyph fallback: ${original.id}`)

    const foregroundPalette = palette.get(original.foreground)
    const backgroundPalette = original.background ? palette.get(original.background) : undefined
    const resolved = {
      definition: original,
      glyph,
      foreground: foregroundPalette?.foreground ?? '#ff5f5f',
      background: backgroundPalette?.background ?? backgroundPalette?.foreground,
      glyphSourceID,
      fallbackPath
    }
    cache.set(cacheKey, resolved)
    return resolved
  }
}

export function resolveClassicVisual(
  ruleSet: CitySpatialRuleSet,
  kind: CitySpatialRuleKind,
  requestedDefinitionID: string
): ClassicVisual {
  return createClassicVisualResolver(ruleSet)(kind, requestedDefinitionID)
}

function cellLayer(visual: ClassicVisual): ProjectedCellLayer {
  return {
    kind: visual.definition.kind,
    definitionID: visual.definition.id,
    name: visual.definition.name,
    glyph: visual.glyph,
    movementCost: visual.definition.movement_cost,
    flags: [...visual.definition.flags]
  }
}

export function projectCityChunk(chunk: CityMapChunk, ruleSet: CitySpatialRuleSet): ProjectedCityChunk {
  const { payload } = chunk
  if (
    payload.format !== 'city-chunk-v1' ||
    payload.width !== ruleSet.chunk_size ||
    payload.height !== ruleSet.chunk_size ||
    payload.width <= 0
  ) {
    throw new Error('Unsupported city chunk payload')
  }

  const expectedCellCount = payload.width * payload.height
  const terrainIDs: string[] = []
  for (const run of payload.terrain_runs) {
    if (!Number.isInteger(run.length) || run.length <= 0 || terrainIDs.length + run.length > expectedCellCount) {
      throw new Error('Invalid city chunk terrain run')
    }
    for (let index = 0; index < run.length; index += 1) terrainIDs.push(run.definition_id)
  }
  if (terrainIDs.length !== expectedCellCount) throw new Error('Incomplete city chunk terrain payload')

  const furnitureByCell = new Map<number, string>()
  for (const furniture of payload.furniture) {
    if (
      !Number.isInteger(furniture.x) || !Number.isInteger(furniture.y) ||
      furniture.x < 0 || furniture.x >= payload.width || furniture.y < 0 || furniture.y >= payload.height
    ) {
      throw new Error('Invalid city chunk furniture coordinate')
    }
    const index = furniture.y * payload.width + furniture.x
    if (furnitureByCell.has(index)) throw new Error('Duplicate city chunk furniture cell')
    furnitureByCell.set(index, furniture.definition_id)
  }

  const resolveVisual = createClassicVisualResolver(ruleSet)
  const cells = terrainIDs.map((terrainDefinitionID, index): ProjectedCityCell => {
    const localX = index % payload.width
    const localY = Math.floor(index / payload.width)
    const terrain = resolveVisual('terrain', terrainDefinitionID)
    const furnitureDefinitionID = furnitureByCell.get(index)
    const furniture = furnitureDefinitionID
      ? resolveVisual('furniture', furnitureDefinitionID)
      : undefined
    const top = furniture ?? terrain
    return {
      worldX: chunk.chunk_x * payload.width + localX,
      worldY: chunk.chunk_y * payload.height + localY,
      z: chunk.z,
      chunkX: chunk.chunk_x,
      chunkY: chunk.chunk_y,
      localX,
      localY,
      glyph: top.glyph,
      foreground: top.foreground,
      background: top.background ?? terrain.background ?? DEFAULT_MAP_BACKGROUND,
      terrainDefinitionID: terrain.definition.id,
      furnitureDefinitionID: furniture?.definition.id,
      stack: furniture ? [cellLayer(terrain), cellLayer(furniture)] : [cellLayer(terrain)]
    }
  })

  return {
    key: chunkKey(chunk.chunk_x, chunk.chunk_y, chunk.z),
    chunkX: chunk.chunk_x,
    chunkY: chunk.chunk_y,
    z: chunk.z,
    width: payload.width,
    height: payload.height,
    revision: chunk.revision,
    payloadHash: chunk.payload_hash,
    districtCode: chunk.district_code,
    generatedTick: chunk.generated_tick,
    cells
  }
}

function openWorldLayerDisplayRank(kind: CitySpatialRuleKind): number {
  switch (kind) {
    case 'structure': return 1
    case 'furniture': return 2
    case 'portal': return 3
    case 'item': return 4
    case 'entity': return 5
    case 'field': return 6
    case 'overlay': return 7
    default: return -1
  }
}

// projectOpenWorldChunk uses only persisted V2 layers. It does not infer a
// building outline in the browser: walls, doors, and later entities remain
// separate server facts with an explicit display order.
export function projectOpenWorldChunk(
  chunk: CityOpenWorldChunk,
  ruleSet: CitySpatialRuleSet
): ProjectedCityChunk {
  const { payload } = chunk
  if (
    payload.format !== 'city-openworld-chunk-v1' ||
    payload.width !== ruleSet.chunk_size || payload.height !== ruleSet.chunk_size ||
    payload.width <= 0
  ) {
    throw new Error('Unsupported open-world chunk payload')
  }

  const expectedCellCount = payload.width * payload.height
  const terrainIDs: string[] = []
  for (const run of payload.terrain_runs) {
    if (!Number.isInteger(run.length) || run.length <= 0 || terrainIDs.length + run.length > expectedCellCount) {
      throw new Error('Invalid open-world chunk terrain run')
    }
    for (let index = 0; index < run.length; index += 1) terrainIDs.push(run.definition_id)
  }
  if (terrainIDs.length !== expectedCellCount) throw new Error('Incomplete open-world chunk terrain payload')

  const layersByCell = new Map<number, Array<{ kind: CitySpatialRuleKind; definitionID: string }>>()
  let lastX = -1
  let lastY = -1
  let lastRank = -1
  for (const layer of payload.layers) {
    const rank = openWorldLayerDisplayRank(layer.kind)
    if (
      !Number.isInteger(layer.x) || !Number.isInteger(layer.y) || layer.x < 0 || layer.x >= payload.width ||
      layer.y < 0 || layer.y >= payload.height || !layer.definition_id || rank < 0 ||
      layer.y < lastY || (layer.y === lastY && (layer.x < lastX || (layer.x === lastX && rank <= lastRank)))
    ) {
      throw new Error('Invalid open-world chunk layer')
    }
    const index = layer.y * payload.width + layer.x
    const stack = layersByCell.get(index) ?? []
    stack.push({ kind: layer.kind, definitionID: layer.definition_id })
    layersByCell.set(index, stack)
    lastX = layer.x
    lastY = layer.y
    lastRank = rank
  }

  const resolveVisual = createClassicVisualResolver(ruleSet)
  const cells = terrainIDs.map((terrainDefinitionID, index): ProjectedCityCell => {
    const localX = index % payload.width
    const localY = Math.floor(index / payload.width)
    const terrain = resolveVisual('terrain', terrainDefinitionID)
    const layers = (layersByCell.get(index) ?? []).map(layer => resolveVisual(layer.kind, layer.definitionID))
    const top = layers.length > 0 ? layers[layers.length - 1] : terrain
    let furnitureDefinitionID: string | undefined
    for (const layer of layers) {
      if (layer.definition.kind === 'furniture') furnitureDefinitionID = layer.definition.id
    }
    return {
      worldX: chunk.chunk_x * payload.width + localX,
      worldY: chunk.chunk_y * payload.height + localY,
      z: chunk.z,
      chunkX: chunk.chunk_x,
      chunkY: chunk.chunk_y,
      localX,
      localY,
      glyph: top.glyph,
      foreground: top.foreground,
      background: top.background ?? terrain.background ?? DEFAULT_MAP_BACKGROUND,
      terrainDefinitionID: terrain.definition.id,
      furnitureDefinitionID,
      stack: [cellLayer(terrain), ...layers.map(cellLayer)]
    }
  })

  return {
    key: chunkKey(chunk.chunk_x, chunk.chunk_y, chunk.z),
    chunkX: chunk.chunk_x,
    chunkY: chunk.chunk_y,
    z: chunk.z,
    width: payload.width,
    height: payload.height,
    revision: chunk.revision,
    payloadHash: chunk.payload_hash,
    districtCode: '',
    generatedTick: 0,
    cells
  }
}

interface OpenWorldInteriorLayer {
  kind: CitySpatialRuleKind
  definitionID: string
}

// openWorldInteriorLayer deliberately maps only the immutable cell facts that
// the V2 generator sealed. It does not draw a rectangle around a building or
// infer rooms in the browser: absent cells remain absent, which preserves
// courtyard cuts, arcades, and other irregular footprints in the CLASSIC view.
function openWorldInteriorLayer(cell: CityOpenWorldBuildingInterior['cells'][number]): OpenWorldInteriorLayer | null {
  const feature = cell.feature ?? ''
  switch (cell.kind) {
    case 'wall':
      return { kind: 'structure', definitionID: 'structure.wall' }
    case 'window':
      return { kind: 'structure', definitionID: 'structure.window' }
    case 'door':
      return { kind: 'portal', definitionID: 'portal.door_open' }
    case 'furniture':
      switch (feature) {
        case 'bed': return { kind: 'furniture', definitionID: 'furniture.bed' }
        case 'chair': return { kind: 'furniture', definitionID: 'furniture.chair' }
        case 'crate': return { kind: 'item', definitionID: 'item.crate' }
        case 'table':
        case 'counter':
        case 'shelf':
        case 'machine':
          return { kind: 'furniture', definitionID: 'furniture.table' }
        default:
          throw new Error(`Unsupported open-world interior furnishing: ${feature || 'empty'}`)
      }
    case 'floor':
      if (!feature) return null
      if (feature === 'stairs') return { kind: 'portal', definitionID: 'portal.stairs_up' }
      throw new Error(`Unsupported open-world interior floor feature: ${feature}`)
    default:
      throw new Error(`Unsupported open-world interior cell kind: ${String(cell.kind)}`)
  }
}

function projectOpenWorldInteriorCell(
  cell: CityOpenWorldBuildingInterior['cells'][number],
  ruleSet: CitySpatialRuleSet,
  resolveVisual: ReturnType<typeof createClassicVisualResolver>
): ProjectedCityCell {
  if (!Number.isSafeInteger(cell.x) || !Number.isSafeInteger(cell.y) || cell.z < ruleSet.min_z || cell.z > ruleSet.max_z) {
    throw new Error('Invalid open-world interior cell coordinate')
  }
  const terrain = resolveVisual('terrain', 'terrain.floor')
  const layer = openWorldInteriorLayer(cell)
  const overlay = layer ? resolveVisual(layer.kind, layer.definitionID) : null
  const chunkX = floorDiv(cell.x, ruleSet.chunk_size)
  const chunkY = floorDiv(cell.y, ruleSet.chunk_size)
  return {
    worldX: cell.x,
    worldY: cell.y,
    z: cell.z,
    chunkX,
    chunkY,
    localX: cell.x - chunkX * ruleSet.chunk_size,
    localY: cell.y - chunkY * ruleSet.chunk_size,
    glyph: overlay?.glyph ?? terrain.glyph,
    foreground: overlay?.foreground ?? terrain.foreground,
    background: overlay?.background ?? terrain.background ?? DEFAULT_MAP_BACKGROUND,
    terrainDefinitionID: terrain.definition.id,
    furnitureDefinitionID: overlay?.definition.kind === 'furniture' ? overlay.definition.id : undefined,
    stack: overlay ? [cellLayer(terrain), cellLayer(overlay)] : [cellLayer(terrain)]
  }
}

// buildOpenWorldInteriorScene renders the exact persisted floor record in a
// C:DDA-style viewport. Unlike the surface map this scene is intentionally
// sparse: it only contains cells returned by the interior endpoint, so large
// rooms, furniture density, cut corners, and void/courtyard cells are all
// shown as authored server facts rather than a front-end reconstruction.
export function buildOpenWorldInteriorScene(
  interior: CityOpenWorldBuildingInterior,
  ruleSet: CitySpatialRuleSet,
  camera: CameraState,
  viewport: ViewportSize
): ClassicLocalScene {
  if (!interior || interior.cells.length === 0 || !Number.isInteger(interior.z)) {
    throw new Error('Open-world interior has no materialized cells')
  }
  const resolveVisual = createClassicVisualResolver(ruleSet)
  const projectedByCoordinate = new Map<string, ProjectedCityCell>()
  for (const cell of interior.cells) {
    if (cell.z !== interior.z) throw new Error('Open-world interior contains multiple z layers')
    const projected = projectOpenWorldInteriorCell(cell, ruleSet, resolveVisual)
    const key = coordinateKey(projected.worldX, projected.worldY, projected.z)
    if (projectedByCoordinate.has(key)) throw new Error('Duplicate open-world interior cell')
    projectedByCoordinate.set(key, projected)
  }

  const cellSize = Math.max(8, Math.trunc(camera.cellSize))
  const width = Math.max(cellSize, Math.trunc(viewport.width))
  const height = Math.max(cellSize, Math.trunc(viewport.height))
  const columns = Math.max(1, Math.ceil(width / cellSize))
  const rows = Math.max(1, Math.ceil(height / cellSize))
  const startWorldX = Math.trunc(camera.worldX) - Math.floor(columns / 2)
  const startWorldY = Math.trunc(camera.worldY) - Math.floor(rows / 2)
  const cells: Array<ClassicSceneCell | null> = new Array(columns * rows).fill(null)
  for (let row = 0; row < rows; row += 1) {
    for (let column = 0; column < columns; column += 1) {
      const cell = projectedByCoordinate.get(coordinateKey(startWorldX + column, startWorldY + row, interior.z))
      if (cell) cells[row * columns + column] = { ...cell, column, row }
    }
  }
  return {
    mode: 'local', width, height, cellSize, columns, rows,
    startWorldX, startWorldY, cells
  }
}

export function getProjectedCell(
  chunks: ReadonlyMap<string, ProjectedCityChunk>,
  worldX: number,
  worldY: number,
  z: number,
  chunkSize: number
): ProjectedCityCell | null {
  const chunkX = floorDiv(worldX, chunkSize)
  const chunkY = floorDiv(worldY, chunkSize)
  const chunk = chunks.get(chunkKey(chunkX, chunkY, z))
  if (!chunk) return null
  const localX = worldX - chunkX * chunkSize
  const localY = worldY - chunkY * chunkSize
  return chunk.cells[localY * chunk.width + localX] ?? null
}

export function buildLocalScene(
  chunks: ReadonlyMap<string, ProjectedCityChunk>,
  camera: CameraState,
  viewport: ViewportSize,
  chunkSize: number,
  land?: CityLandState | null,
  development?: CityDevelopmentState | null,
  enterprise?: CityEnterpriseLocationState | null,
  actors: readonly WorldActor[] = []
): ClassicLocalScene {
  const cellSize = Math.max(8, Math.trunc(camera.cellSize))
  const width = Math.max(cellSize, Math.trunc(viewport.width))
  const height = Math.max(cellSize, Math.trunc(viewport.height))
  const columns = Math.max(1, Math.ceil(width / cellSize))
  const rows = Math.max(1, Math.ceil(height / cellSize))
  const startWorldX = Math.trunc(camera.worldX) - Math.floor(columns / 2)
  const startWorldY = Math.trunc(camera.worldY) - Math.floor(rows / 2)
  const cells: Array<ClassicSceneCell | null> = new Array(columns * rows).fill(null)
  for (let row = 0; row < rows; row += 1) {
    for (let column = 0; column < columns; column += 1) {
      const cell = getProjectedCell(
        chunks,
        startWorldX + column,
        startWorldY + row,
        camera.z,
        chunkSize
      )
      if (!cell) {
        cells[row * columns + column] = null
        continue
      }
      const landCell = applyCityLandOverlay(cell, land, chunkSize)
      const enterpriseCell = applyCityEnterpriseOverlay(landCell, land, enterprise, chunkSize)
      cells[row * columns + column] = {
        ...applyWorldActorOverlay(
          applyCityDevelopmentOverlay(enterpriseCell, land, development, chunkSize),
          actors
        ),
        column,
        row
      }
    }
  }
  return {
    mode: 'local', width, height, cellSize, columns, rows,
    startWorldX, startWorldY, cells
  }
}

function overmapRoadGlyph(mask: number): string {
  const normalized = mask & 15
  const glyphs: Record<number, string> = {
    1: '╵', 2: '╶', 3: '└', 4: '╷', 5: '│', 6: '┌', 7: '├', 8: '╴',
    9: '┘', 10: '─', 11: '┴', 12: '┐', 13: '┤', 14: '┬', 15: '┼'
  }
  return glyphs[normalized] ?? '='
}

export function buildOvermapScene(
  tiles: CityOvermapTile[],
  ruleSet: CitySpatialRuleSet,
  viewport: ViewportSize,
  land?: CityLandState | null,
  development?: CityDevelopmentState | null,
  enterprise?: CityEnterpriseLocationState | null,
  actors: readonly WorldActor[] = []
): ClassicOvermapScene {
  const width = Math.max(240, Math.trunc(viewport.width))
  const height = Math.max(240, Math.trunc(viewport.height))
  if (tiles.length === 0) {
    return { mode: 'overmap', width, height, cellSize: 40, offsetX: 0, offsetY: 0, cells: [] }
  }
  const minX = Math.min(...tiles.map(tile => tile.chunk_x))
  const maxX = Math.max(...tiles.map(tile => tile.chunk_x))
  const minY = Math.min(...tiles.map(tile => tile.chunk_y))
  const maxY = Math.max(...tiles.map(tile => tile.chunk_y))
  const columns = maxX - minX + 1
  const rows = maxY - minY + 1
  const cellSize = Math.max(28, Math.min(72, Math.floor(Math.min((width - 32) / columns, (height - 32) / rows))))
  const offsetX = Math.floor((width - columns * cellSize) / 2)
  const offsetY = Math.floor((height - rows * cellSize) / 2)
  const resolveVisual = createClassicVisualResolver(ruleSet)
  const road = resolveVisual('terrain', 'terrain.road')
  const river = resolveVisual('terrain', 'terrain.deep_water')
  const cells = tiles.map((tile): ClassicOvermapCell => {
    const terrain = resolveVisual('terrain', tile.terrain_definition_id)
    const visual = tile.river_mask ? river : tile.road_mask ? road : terrain
    const landSummary = getCityLandTileSummary(land, tile)
    const buildingCodes = new Set(landSummary.buildings.map(building => building.code))
    const projects = development?.projects.filter(project => buildingCodes.has(project.building_code)) ?? []
    const enterpriseSites = enterprise?.sites.filter(site => (
      site.status === 'active' && buildingCodes.has(site.building_code)
    )) ?? []
    const tileActors = actors.filter(actor => actor.location &&
      actor.location.chunk_x === tile.chunk_x && actor.location.chunk_y === tile.chunk_y)
    return {
      tile,
      glyph: tileActors.length > 0
        ? (tileActors.length === 1 ? '@' : '&')
        : tile.river_mask ? '≈' : tile.road_mask ? overmapRoadGlyph(tile.road_mask) : visual.glyph,
      foreground: tileActors.length > 0 ? '#f4f7fb' : visual.foreground,
      background: terrain.background ?? DEFAULT_MAP_BACKGROUND,
      landUses: landSummary.landUses,
      parcelCount: landSummary.parcels.length,
      buildingCount: landSummary.buildings.length,
      activeProjectCount: projects.filter(project => (
        project.status === 'submitted' || project.status === 'approved' || project.status === 'under_construction'
      )).length,
      completedProjectCount: projects.filter(project => project.status === 'completed').length,
      activeEnterpriseSiteCount: enterpriseSites.length,
      enterpriseFirmCount: new Set(enterpriseSites.map(site => site.firm_entity_code)).size,
      enterpriseOccupiedUnits: enterpriseSites.reduce((sum, site) => sum + site.occupied_units, 0),
      actorCount: tileActors.length,
      actorCodes: tileActors.map(actor => actor.code),
      x: offsetX + (tile.chunk_x - minX) * cellSize,
      y: offsetY + (tile.chunk_y - minY) * cellSize,
      size: cellSize
    }
  })
  return { mode: 'overmap', width, height, cellSize, offsetX, offsetY, cells }
}

export function hitTestClassicScene(scene: ClassicScene, x: number, y: number): ProjectedCityCell | CityOvermapTile | null {
  if (scene.mode === 'overmap') {
    return scene.cells.find(cell => (
      x >= cell.x && x < cell.x + cell.size && y >= cell.y && y < cell.y + cell.size
    ))?.tile ?? null
  }
  const column = Math.floor(x / scene.cellSize)
  const row = Math.floor(y / scene.cellSize)
  if (column < 0 || row < 0 || column >= scene.columns || row >= scene.rows) return null
  return scene.cells[row * scene.columns + column] ?? null
}

export function viewportChunkBounds(
  camera: CameraState,
  viewport: ViewportSize,
  chunkSize: number,
  limits: { minX: number; maxX: number; minY: number; maxY: number },
  prefetchRing = 1
): ChunkViewportBounds {
  const columns = Math.max(1, Math.ceil(viewport.width / Math.max(8, camera.cellSize)))
  const rows = Math.max(1, Math.ceil(viewport.height / Math.max(8, camera.cellSize)))
  const halfColumns = Math.ceil(columns / 2)
  const halfRows = Math.ceil(rows / 2)
  const minX = Math.max(limits.minX, floorDiv(camera.worldX - halfColumns, chunkSize) - prefetchRing)
  const maxX = Math.min(limits.maxX, floorDiv(camera.worldX + halfColumns, chunkSize) + prefetchRing)
  const minY = Math.max(limits.minY, floorDiv(camera.worldY - halfRows, chunkSize) - prefetchRing)
  const maxY = Math.min(limits.maxY, floorDiv(camera.worldY + halfRows, chunkSize) + prefetchRing)
  return { min_x: minX, max_x: maxX, min_y: minY, max_y: maxY, z: camera.z }
}

export function exportProjectedChunkText(chunk: ProjectedCityChunk): string {
  const rows: string[] = []
  for (let y = 0; y < chunk.height; y += 1) {
    rows.push(chunk.cells.slice(y * chunk.width, (y + 1) * chunk.width).map(cell => cell.glyph).join(''))
  }
  return [
    '# Sub2API City CLASSIC viewport',
    `# chunk=${chunk.chunkX},${chunk.chunkY},${chunk.z}`,
    `# district=${chunk.districtCode}`,
    `# revision=${chunk.revision}`,
    `# generated_tick=${chunk.generatedTick}`,
    `# payload_hash=${chunk.payloadHash}`,
    ...rows,
    ''
  ].join('\n')
}

export function unloadedMapBackground(): string {
  return UNLOADED_BACKGROUND
}
