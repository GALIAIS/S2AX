import { computed, ref, shallowRef } from 'vue'
import { defineStore } from 'pinia'
import citySpatialAPI, {
  type CityDevelopmentProject,
  type CityDevelopmentState,
  type CityEnterpriseLocationCommandType,
  type CityEnterpriseLocationState,
  type CityLandState,
  type CityMapChunkSummary,
  type CityOvermapState,
  type CityOvermapTile,
  type CitySpatialMutation,
  type CityWorld,
  type CityWorldSpatialRuleSet,
  type CreateCityWorldRequest,
  type WorldActor,
  type WorldActorRoleOption,
  type WorldActorState,
  type WorldRuleCase,
  type WorldRuntimeCatalog,
  type WorldRuntimeCommandType,
  type WorldRuntimeDefinition
} from '@/api/citySpatial'
import { isApiError } from '@/api/client'
import { CityChunkCache } from '@/features/city-spatial/chunkCache'
import {
  applyCityDevelopmentOverlay,
  applyCityEnterpriseOverlay,
  applyCityLandOverlay,
  chunkKey,
  floorDiv,
  getProjectedCell,
  projectCityChunk,
  viewportChunkBounds,
  type CameraState,
  type ProjectedCityCell,
  type ProjectedCityChunk,
  type ViewportSize
} from '@/features/city-spatial/projection'

export type CityMapMode = 'overmap' | 'local'
export type CityLandAvailability = 'unknown' | 'available' | 'unavailable'
export type CityDevelopmentAvailability = 'unknown' | 'available' | 'unavailable'
export type CityEnterpriseLocationAvailability = 'unknown' | 'available' | 'unavailable'
export type WorldRuntimeAvailability = 'unknown' | 'available' | 'unavailable'

const CELL_SIZE_STEPS = [12, 16, 20, 24, 32] as const
const DEFAULT_VIEWPORT: ViewportSize = { width: 960, height: 560 }
const CHUNK_CACHE_CAPACITY = 64

function readableError(error: unknown): string {
  if (isApiError(error)) return error.message
  if (error instanceof Error) return error.message
  return 'Unknown city spatial error'
}

export const useCitySpatialStore = defineStore('citySpatial', () => {
  const worlds = ref<CityWorld[]>([])
  const activeWorldID = ref<number | null>(null)
  const ruleBundle = shallowRef<CityWorldSpatialRuleSet | null>(null)
  const overmap = shallowRef<CityOvermapState | null>(null)
  const landLayers = shallowRef<ReadonlyMap<number, CityLandState>>(new Map())
  const landAvailability = ref<CityLandAvailability>('unknown')
  const developmentState = shallowRef<CityDevelopmentState | null>(null)
  const developmentAvailability = ref<CityDevelopmentAvailability>('unknown')
  const enterpriseLocationState = shallowRef<CityEnterpriseLocationState | null>(null)
  const enterpriseLocationAvailability = ref<CityEnterpriseLocationAvailability>('unknown')
  const worldRuntimeCatalog = shallowRef<WorldRuntimeCatalog | null>(null)
  const worldActors = ref<WorldActor[]>([])
  const selectedActorCode = ref<string | null>(null)
  const worldActorState = shallowRef<WorldActorState | null>(null)
  const worldActorRoleOptions = ref<WorldActorRoleOption[]>([])
  const worldRuntimeRules = ref<WorldRuntimeDefinition[]>([])
  const worldRuleCases = ref<WorldRuleCase[]>([])
  const worldRuntimeAvailability = ref<WorldRuntimeAvailability>('unknown')
  const chunkSummaries = shallowRef<ReadonlyMap<string, CityMapChunkSummary>>(new Map())
  const projectedChunks = shallowRef<ReadonlyMap<string, ProjectedCityChunk>>(new Map())
  const spatialChanges = ref<CitySpatialMutation[]>([])
  const mapMode = ref<CityMapMode>('overmap')
  const camera = ref<CameraState>({ worldX: 16, worldY: 16, z: 0, cellSize: CELL_SIZE_STEPS[2] })
  const viewport = ref<ViewportSize>({ ...DEFAULT_VIEWPORT })
  const selectedTile = ref<CityOvermapTile | null>(null)
  const selectedCoordinate = ref<{ worldX: number; worldY: number; z: number } | null>(null)
  const hoveredCoordinate = ref<{ worldX: number; worldY: number; z: number } | null>(null)
  const initialLoading = ref(false)
  const refreshing = ref(false)
  const chunkLoading = ref(false)
  const landLoading = ref(false)
  const developmentLoading = ref(false)
  const enterpriseLocationLoading = ref(false)
  const worldRuntimeLoading = ref(false)
  const creatingWorld = ref(false)
  const generatingChunkKey = ref<string | null>(null)
  const developmentCommandCode = ref<string | null>(null)
  const enterpriseLocationCommandCode = ref<string | null>(null)
  const worldRuntimeCommandCode = ref<string | null>(null)
  const loadError = ref<string | null>(null)

  const cache = new CityChunkCache(CHUNK_CACHE_CAPACITY)
  const inFlightChunks = new Map<string, Promise<ProjectedCityChunk>>()
  const inFlightLandLayers = new Map<string, Promise<CityLandState | null>>()
  let worldGeneration = 0
  let activeChunkLoads = 0
  let activeLandLoads = 0
  let visibleLoadTimer: ReturnType<typeof setTimeout> | null = null

  const activeWorld = computed(() => (
    worlds.value.find(world => world.id === activeWorldID.value) ?? null
  ))
  const profile = computed(() => ruleBundle.value?.profile ?? null)
  const ruleSet = computed(() => ruleBundle.value?.rule_set ?? null)
  const activeLandState = computed(() => (
    landLayers.value.get(mapMode.value === 'overmap' ? 0 : camera.value.z) ?? null
  ))
  const latestChanges = computed(() => [...spatialChanges.value].sort((left, right) => (
    right.tick - left.tick || right.sequence - left.sequence
  )))
  const activeDevelopmentProjects = computed(() => developmentState.value?.projects.filter(project => (
    project.status === 'submitted' || project.status === 'approved' || project.status === 'under_construction'
  )) ?? [])
  const developmentProjectsByBuilding = computed<ReadonlyMap<string, CityDevelopmentProject[]>>(() => {
    const projects = new Map<string, CityDevelopmentProject[]>()
    for (const project of developmentState.value?.projects ?? []) {
      const items = projects.get(project.building_code)
      if (items) items.push(project)
      else projects.set(project.building_code, [project])
    }
    return projects
  })
  const selectedCell = computed<ProjectedCityCell | null>(() => {
    const selected = selectedCoordinate.value
    const currentProfile = profile.value
    if (!selected || !currentProfile) return null
    const cell = getProjectedCell(
      projectedChunks.value,
      selected.worldX,
      selected.worldY,
      selected.z,
      currentProfile.chunk_size
    )
    if (!cell) return null
    const landCell = applyCityLandOverlay(cell, activeLandState.value, currentProfile.chunk_size)
    const enterpriseCell = applyCityEnterpriseOverlay(
      landCell,
      activeLandState.value,
      enterpriseLocationState.value,
      currentProfile.chunk_size
    )
    return applyCityDevelopmentOverlay(
      enterpriseCell,
      activeLandState.value,
      developmentState.value,
      currentProfile.chunk_size
    )
  })
  const selectedChunk = computed<ProjectedCityChunk | null>(() => {
    const currentProfile = profile.value
    const selected = selectedCoordinate.value
    if (!currentProfile || !selected) return null
    const x = floorDiv(selected.worldX, currentProfile.chunk_size)
    const y = floorDiv(selected.worldY, currentProfile.chunk_size)
    return projectedChunks.value.get(chunkKey(x, y, selected.z)) ?? null
  })
  const generatedChunkKeys = computed(() => new Set(chunkSummaries.value.keys()))
  const canGenerateSelectedTile = computed(() => {
    const world = activeWorld.value
    const tile = selectedTile.value
    if (!world || !tile || world.member_role !== 'owner' || tile.z !== 0) return false
    return !chunkSummaries.value.has(chunkKey(tile.chunk_x, tile.chunk_y, tile.z))
  })

  function publishCache(): void {
    projectedChunks.value = cache.snapshot()
  }

  function resetSpatialState(): void {
    cache.clear()
    publishCache()
    chunkSummaries.value = new Map()
    ruleBundle.value = null
    overmap.value = null
    landLayers.value = new Map()
    landAvailability.value = 'unknown'
    developmentState.value = null
    developmentAvailability.value = 'unknown'
    enterpriseLocationState.value = null
    enterpriseLocationAvailability.value = 'unknown'
    worldRuntimeCatalog.value = null
    worldActors.value = []
    selectedActorCode.value = null
    worldActorState.value = null
    worldActorRoleOptions.value = []
    worldRuntimeRules.value = []
    worldRuleCases.value = []
    worldRuntimeAvailability.value = 'unknown'
    spatialChanges.value = []
    selectedTile.value = null
    selectedCoordinate.value = null
    hoveredCoordinate.value = null
    mapMode.value = 'overmap'
    camera.value = { worldX: 16, worldY: 16, z: 0, cellSize: CELL_SIZE_STEPS[2] }
  }

  function updateWorldTick(tick: number): void {
    worlds.value = worlds.value.map(world => (
      world.id === activeWorldID.value ? { ...world, current_tick: tick } : world
    ))
  }

  function clampCamera(next: CameraState): CameraState {
    const currentProfile = profile.value
    if (!currentProfile) return next
    const chunkSize = currentProfile.chunk_size
    return {
      ...next,
      worldX: Math.max(
        currentProfile.minimum_chunk_x * chunkSize,
        Math.min((currentProfile.maximum_chunk_x + 1) * chunkSize - 1, Math.trunc(next.worldX))
      ),
      worldY: Math.max(
        currentProfile.minimum_chunk_y * chunkSize,
        Math.min((currentProfile.maximum_chunk_y + 1) * chunkSize - 1, Math.trunc(next.worldY))
      ),
      z: Math.max(currentProfile.minimum_z, Math.min(currentProfile.maximum_z, Math.trunc(next.z)))
    }
  }

  function currentViewportBounds() {
    const currentProfile = profile.value
    if (!currentProfile) return null
    return viewportChunkBounds(
      camera.value,
      viewport.value,
      currentProfile.chunk_size,
      {
        minX: currentProfile.minimum_chunk_x,
        maxX: currentProfile.maximum_chunk_x,
        minY: currentProfile.minimum_chunk_y,
        maxY: currentProfile.maximum_chunk_y
      }
    )
  }

  function isLandStateNotFound(error: unknown): boolean {
    return isApiError(error) && (
      error.status === 404 || String(error.code ?? '') === 'CITY_LAND_STATE_NOT_FOUND'
    )
  }

  function isDevelopmentStateNotFound(error: unknown): boolean {
    return isApiError(error) && (
      error.status === 404 || String(error.code ?? '') === 'CITY_DEVELOPMENT_STATE_NOT_FOUND'
    )
  }

  function isEnterpriseLocationStateNotFound(error: unknown): boolean {
    return isApiError(error) && (
      error.status === 404 || String(error.code ?? '') === 'CITY_ENTERPRISE_LOCATION_STATE_NOT_FOUND'
    )
  }

  function isWorldRuntimeStateNotFound(error: unknown): boolean {
    return isApiError(error) && (
      error.status === 404 || String(error.code ?? '') === 'WORLD_RUNTIME_STATE_NOT_FOUND'
    )
  }

  async function loadDevelopmentState(force = false): Promise<CityDevelopmentState | null> {
    const worldID = activeWorldID.value
    if (!worldID || (developmentAvailability.value === 'unavailable' && !force)) return null
    if (developmentState.value && !force) return developmentState.value
    const token = worldGeneration
    developmentLoading.value = true
    try {
      const state = await citySpatialAPI.getDevelopmentState(worldID)
      if (token !== worldGeneration || activeWorldID.value !== worldID) return null
      developmentState.value = state
      developmentAvailability.value = 'available'
      return state
    } catch (error: unknown) {
      if (token !== worldGeneration || activeWorldID.value !== worldID) return null
      if (isDevelopmentStateNotFound(error)) {
        developmentState.value = null
        developmentAvailability.value = 'unavailable'
        return null
      }
      loadError.value = readableError(error)
      throw error
    } finally {
      if (token === worldGeneration) developmentLoading.value = false
    }
  }

  async function loadEnterpriseLocationState(force = false): Promise<CityEnterpriseLocationState | null> {
    const worldID = activeWorldID.value
    if (!worldID || (enterpriseLocationAvailability.value === 'unavailable' && !force)) return null
    if (enterpriseLocationState.value && !force) return enterpriseLocationState.value
    const token = worldGeneration
    enterpriseLocationLoading.value = true
    try {
      const state = await citySpatialAPI.getEnterpriseLocationState(worldID)
      if (token !== worldGeneration || activeWorldID.value !== worldID) return null
      enterpriseLocationState.value = state
      enterpriseLocationAvailability.value = 'available'
      return state
    } catch (error: unknown) {
      if (token !== worldGeneration || activeWorldID.value !== worldID) return null
      if (isEnterpriseLocationStateNotFound(error)) {
        enterpriseLocationState.value = null
        enterpriseLocationAvailability.value = 'unavailable'
        return null
      }
      loadError.value = readableError(error)
      throw error
    } finally {
      if (token === worldGeneration) enterpriseLocationLoading.value = false
    }
  }

  async function loadWorldActor(actorCode: string): Promise<WorldActorState | null> {
    const worldID = activeWorldID.value
    if (!worldID || !actorCode || worldRuntimeAvailability.value === 'unavailable') return null
    const token = worldGeneration
    worldRuntimeLoading.value = true
    try {
      const [state, options, cases] = await Promise.all([
        citySpatialAPI.getWorldActorState(worldID, actorCode),
        citySpatialAPI.getWorldActorRoleOptions(worldID, actorCode),
        citySpatialAPI.listWorldRuleCases(worldID, { actor_code: actorCode, limit: 100 })
      ])
      if (token !== worldGeneration || activeWorldID.value !== worldID || selectedActorCode.value !== actorCode) {
        return null
      }
      worldActorState.value = state
      worldActorRoleOptions.value = options
      worldRuleCases.value = cases.items
      return state
    } catch (error: unknown) {
      if (token === worldGeneration && activeWorldID.value === worldID) {
        loadError.value = readableError(error)
      }
      throw error
    } finally {
      if (token === worldGeneration) worldRuntimeLoading.value = false
    }
  }

  async function selectWorldActor(actorCode: string): Promise<void> {
    if (!worldActors.value.some(actor => actor.code === actorCode)) return
    selectedActorCode.value = actorCode
    worldActorState.value = null
    worldActorRoleOptions.value = []
    await loadWorldActor(actorCode)
  }

  async function loadWorldRuntime(force = false): Promise<WorldRuntimeCatalog | null> {
    const worldID = activeWorldID.value
    if (!worldID || (worldRuntimeAvailability.value === 'unavailable' && !force)) return null
    if (worldRuntimeCatalog.value && !force) return worldRuntimeCatalog.value
    const token = worldGeneration
    worldRuntimeLoading.value = true
    try {
      const [catalog, actors, rules] = await Promise.all([
        citySpatialAPI.getWorldRuntimeCatalog(worldID),
        citySpatialAPI.listWorldActors(worldID),
        citySpatialAPI.listWorldRuntimeRules(worldID)
      ])
      if (token !== worldGeneration || activeWorldID.value !== worldID) return null
      worldRuntimeCatalog.value = catalog
      worldActors.value = actors
      worldRuntimeRules.value = rules
      worldRuntimeAvailability.value = 'available'
      const actorCode = actors.some(actor => actor.code === selectedActorCode.value)
        ? selectedActorCode.value
        : (actors[0]?.code ?? null)
      selectedActorCode.value = actorCode
      if (!actorCode) {
        worldActorState.value = null
        worldActorRoleOptions.value = []
        worldRuleCases.value = []
        return catalog
      }
      const [state, options, cases] = await Promise.all([
        citySpatialAPI.getWorldActorState(worldID, actorCode),
        citySpatialAPI.getWorldActorRoleOptions(worldID, actorCode),
        citySpatialAPI.listWorldRuleCases(worldID, { actor_code: actorCode, limit: 100 })
      ])
      if (token !== worldGeneration || activeWorldID.value !== worldID || selectedActorCode.value !== actorCode) {
        return null
      }
      worldActorState.value = state
      worldActorRoleOptions.value = options
      worldRuleCases.value = cases.items
      return catalog
    } catch (error: unknown) {
      if (token !== worldGeneration || activeWorldID.value !== worldID) return null
      if (isWorldRuntimeStateNotFound(error)) {
        worldRuntimeCatalog.value = null
        worldActors.value = []
        selectedActorCode.value = null
        worldActorState.value = null
        worldActorRoleOptions.value = []
        worldRuntimeRules.value = []
        worldRuleCases.value = []
        worldRuntimeAvailability.value = 'unavailable'
        return null
      }
      loadError.value = readableError(error)
      throw error
    } finally {
      if (token === worldGeneration) worldRuntimeLoading.value = false
    }
  }

  async function loadLandLayer(z: number, force = false): Promise<CityLandState | null> {
    const worldID = activeWorldID.value
    const currentProfile = profile.value
    if (!worldID || !currentProfile || landAvailability.value === 'unavailable') return null
    const normalizedZ = Math.max(currentProfile.minimum_z, Math.min(currentProfile.maximum_z, Math.trunc(z)))
    const cached = landLayers.value.get(normalizedZ)
    if (cached && !force) return cached
    const requestKey = `${worldID}/${normalizedZ}`
    const existing = inFlightLandLayers.get(requestKey)
    if (existing) return existing
    const token = worldGeneration
    activeLandLoads += 1
    landLoading.value = true
    const request = citySpatialAPI.getLandState(worldID, {
      min_x: currentProfile.minimum_chunk_x,
      max_x: currentProfile.maximum_chunk_x,
      min_y: currentProfile.minimum_chunk_y,
      max_y: currentProfile.maximum_chunk_y,
      z: normalizedZ
    }).then(state => {
      if (token !== worldGeneration || activeWorldID.value !== worldID) return null
      if (state.profile.spatial_overmap_root_hash !== currentProfile.overmap_root_hash) {
        throw new Error('City land state does not match the bound Overmap')
      }
      const nextLayers = new Map(landLayers.value)
      nextLayers.set(normalizedZ, state)
      landLayers.value = nextLayers
      landAvailability.value = 'available'
      return state
    }).catch((error: unknown) => {
      if (token !== worldGeneration || activeWorldID.value !== worldID) return null
      if (isLandStateNotFound(error)) {
        landAvailability.value = 'unavailable'
        landLayers.value = new Map()
        return null
      }
      loadError.value = readableError(error)
      throw error
    }).finally(() => {
      if (inFlightLandLayers.get(requestKey) === request) inFlightLandLayers.delete(requestKey)
      activeLandLoads = Math.max(0, activeLandLoads - 1)
      landLoading.value = activeLandLoads > 0
    })
    inFlightLandLayers.set(requestKey, request)
    return request
  }

  async function loadChunkDetail(
    worldID: number,
    summary: CityMapChunkSummary,
    token: number
  ): Promise<ProjectedCityChunk> {
    const key = chunkKey(summary.chunk_x, summary.chunk_y, summary.z)
    const requestKey = `${worldID}/${key}`
    const existing = inFlightChunks.get(requestKey)
    if (existing) return existing
    const request = citySpatialAPI
      .getMapChunk(worldID, summary.chunk_x, summary.chunk_y, summary.z)
      .then(chunk => {
        if (token !== worldGeneration || activeWorldID.value !== worldID || !ruleSet.value) {
          throw new Error('Stale city chunk response')
        }
        if (chunk.payload_hash !== summary.payload_hash || chunk.revision !== summary.revision) {
          throw new Error('City chunk summary no longer matches payload')
        }
        const projected = projectCityChunk(chunk, ruleSet.value)
        cache.set(projected)
        publishCache()
        return projected
      })
      .finally(() => {
        if (inFlightChunks.get(requestKey) === request) inFlightChunks.delete(requestKey)
      })
    inFlightChunks.set(requestKey, request)
    return request
  }

  async function loadVisibleChunks(force = false): Promise<void> {
    const worldID = activeWorldID.value
    const bounds = currentViewportBounds()
    if (!worldID || !bounds || !ruleSet.value) return
    const token = worldGeneration
    activeChunkLoads += 1
    chunkLoading.value = true
    try {
      const summaries = await citySpatialAPI.listMapChunks(worldID, bounds)
      if (token !== worldGeneration || activeWorldID.value !== worldID) return
      const nextSummaries = new Map(chunkSummaries.value)
      for (const summary of summaries) nextSummaries.set(chunkKey(summary.chunk_x, summary.chunk_y, summary.z), summary)
      chunkSummaries.value = nextSummaries
      await Promise.all(summaries.map(summary => {
        const key = chunkKey(summary.chunk_x, summary.chunk_y, summary.z)
        const cached = cache.peek(key)
        if (!force && cached?.revision === summary.revision && cached.payloadHash === summary.payload_hash) {
          return Promise.resolve(cached)
        }
        return loadChunkDetail(worldID, summary, token)
      }))
    } catch (error: unknown) {
      if (token === worldGeneration) loadError.value = readableError(error)
      throw error
    } finally {
      activeChunkLoads = Math.max(0, activeChunkLoads - 1)
      chunkLoading.value = activeChunkLoads > 0
    }
  }

  function scheduleVisibleChunkLoad(): void {
    if (visibleLoadTimer) clearTimeout(visibleLoadTimer)
    visibleLoadTimer = setTimeout(() => {
      visibleLoadTimer = null
      void loadVisibleChunks().catch(() => undefined)
    }, 80)
  }

  async function selectWorld(worldID: number): Promise<void> {
    if (!worlds.value.some(world => world.id === worldID)) return
    const token = ++worldGeneration
    activeWorldID.value = worldID
    loadError.value = null
    resetSpatialState()
    initialLoading.value = true
    try {
      const [nextRuleBundle, nextOvermap, changes] = await Promise.all([
        citySpatialAPI.getWorldSpatialRuleSet(worldID),
        citySpatialAPI.getOvermap(worldID),
        citySpatialAPI.listSpatialChanges(worldID, 200)
      ])
      if (token !== worldGeneration || activeWorldID.value !== worldID) return
      if (nextRuleBundle.profile.rule_set_hash !== nextRuleBundle.rule_set.content_hash) {
        throw new Error('City spatial rule set hash mismatch')
      }
      ruleBundle.value = nextRuleBundle
      overmap.value = nextOvermap
      spatialChanges.value = changes.items
      const origin = nextOvermap.tiles.find(tile => tile.chunk_x === 0 && tile.chunk_y === 0 && tile.z === 0)
        ?? nextOvermap.tiles[0]
      if (origin) {
        selectedTile.value = origin
        const center = Math.floor(nextRuleBundle.profile.chunk_size / 2)
        camera.value = clampCamera({
          ...camera.value,
          worldX: origin.chunk_x * nextRuleBundle.profile.chunk_size + center,
          worldY: origin.chunk_y * nextRuleBundle.profile.chunk_size + center,
          z: origin.z
        })
      }
      await Promise.all([
        loadVisibleChunks(),
        loadLandLayer(0),
        loadDevelopmentState(),
        loadEnterpriseLocationState(),
        loadWorldRuntime()
      ])
    } catch (error: unknown) {
      if (token === worldGeneration) loadError.value = readableError(error)
    } finally {
      if (token === worldGeneration) initialLoading.value = false
    }
  }

  async function initialize(preferredWorldID?: number): Promise<void> {
    initialLoading.value = true
    loadError.value = null
    try {
      const items = await citySpatialAPI.listWorlds()
      worlds.value = items
      if (items.length === 0) {
        ++worldGeneration
        activeWorldID.value = null
        resetSpatialState()
        return
      }
      const target = items.find(world => world.id === preferredWorldID)
        ?? items.find(world => world.id === activeWorldID.value)
        ?? items[0]
      await selectWorld(target.id)
    } catch (error: unknown) {
      loadError.value = readableError(error)
    } finally {
      initialLoading.value = false
    }
  }

  async function refresh(): Promise<void> {
    const worldID = activeWorldID.value
    if (!worldID || refreshing.value) return
    const token = worldGeneration
    refreshing.value = true
    loadError.value = null
    try {
      const [nextWorlds, nextRuleBundle, nextOvermap, changes] = await Promise.all([
        citySpatialAPI.listWorlds(),
        citySpatialAPI.getWorldSpatialRuleSet(worldID),
        citySpatialAPI.getOvermap(worldID),
        citySpatialAPI.listSpatialChanges(worldID, 200)
      ])
      if (token !== worldGeneration || activeWorldID.value !== worldID) return
      if (nextRuleBundle.profile.rule_set_hash !== nextRuleBundle.rule_set.content_hash) {
        throw new Error('City spatial rule set hash mismatch')
      }
      if (
        ruleBundle.value &&
        nextRuleBundle.profile.rule_set_hash !== ruleBundle.value.profile.rule_set_hash
      ) {
        throw new Error('City spatial rule set binding changed unexpectedly')
      }
      worlds.value = nextWorlds
      ruleBundle.value = nextRuleBundle
      overmap.value = nextOvermap
      spatialChanges.value = changes.items
      const landLevels = new Set([0, mapMode.value === 'local' ? camera.value.z : 0])
      await Promise.all([
        loadVisibleChunks(true),
        ...[...landLevels].map(z => loadLandLayer(z, true)),
        loadDevelopmentState(true),
        loadEnterpriseLocationState(true),
        loadWorldRuntime(true)
      ])
    } catch (error: unknown) {
      if (token === worldGeneration) loadError.value = readableError(error)
    } finally {
      if (token === worldGeneration) refreshing.value = false
    }
  }

  async function createWorld(request: CreateCityWorldRequest): Promise<CityWorld> {
    creatingWorld.value = true
    loadError.value = null
    try {
      const foundation = await citySpatialAPI.createWorld(request)
      worlds.value = [foundation.world, ...worlds.value.filter(world => world.id !== foundation.world.id)]
      await selectWorld(foundation.world.id)
      return foundation.world
    } catch (error: unknown) {
      loadError.value = readableError(error)
      throw error
    } finally {
      creatingWorld.value = false
    }
  }

  async function generateSelectedChunk(): Promise<void> {
    const world = activeWorld.value
    const tile = selectedTile.value
    if (!world || !tile || !canGenerateSelectedTile.value || generatingChunkKey.value) return
    const key = chunkKey(tile.chunk_x, tile.chunk_y, tile.z)
    generatingChunkKey.value = key
    loadError.value = null
    try {
      const command = await citySpatialAPI.submitGenerateChunk(
        world.id,
        tile.chunk_x,
        tile.chunk_y,
        tile.z,
        world.current_tick
      )
      const result = await citySpatialAPI.stepWorld(world.id, world.current_tick)
      const processed = result.commands.find(item => item.id === command.id)
      if (!processed || processed.status !== 'applied') {
        throw new Error(processed?.error_code ?? 'City chunk generation was rejected')
      }
      updateWorldTick(result.tick.tick)
      cache.delete(key)
      publishCache()
      const nextSummaries = new Map(chunkSummaries.value)
      nextSummaries.delete(key)
      chunkSummaries.value = nextSummaries
      spatialChanges.value = result.spatial_mutations.length > 0
        ? [...spatialChanges.value, ...result.spatial_mutations]
        : spatialChanges.value
      await loadVisibleChunks(true)
    } catch (error: unknown) {
      loadError.value = readableError(error)
      throw error
    } finally {
      generatingChunkKey.value = null
    }
  }

  async function runDevelopmentCommand(
    commandType: 'development.submit' | 'development.review' | 'development.start' | 'development.cancel',
    payload: Record<string, unknown>,
    projectCode = 'new'
  ): Promise<void> {
    const world = activeWorld.value
    if (!world || world.member_role !== 'owner' || developmentCommandCode.value) return
    developmentCommandCode.value = projectCode
    loadError.value = null
    try {
      const command = await citySpatialAPI.submitDevelopmentCommand(
        world.id,
        commandType,
        payload,
        world.current_tick
      )
      const result = await citySpatialAPI.stepWorld(world.id, world.current_tick)
      const processed = result.commands.find(item => item.id === command.id)
      if (!processed || processed.status !== 'applied') {
        throw new Error(processed?.error_code ?? 'City development command was rejected')
      }
      updateWorldTick(result.tick.tick)
      const levels = new Set<number>([0, camera.value.z, ...landLayers.value.keys()])
      await Promise.all([
        loadDevelopmentState(true),
        ...[...levels].map(z => loadLandLayer(z, true))
      ])
    } catch (error: unknown) {
      loadError.value = readableError(error)
      throw error
    } finally {
      developmentCommandCode.value = null
    }
  }

  async function runEnterpriseLocationCommand(
    commandType: CityEnterpriseLocationCommandType,
    payload: Record<string, unknown>,
    commandCode = 'enterprise'
  ): Promise<void> {
    const world = activeWorld.value
    if (!world || world.member_role !== 'owner' || enterpriseLocationCommandCode.value) return
    enterpriseLocationCommandCode.value = commandCode
    loadError.value = null
    try {
      const command = await citySpatialAPI.submitEnterpriseLocationCommand(
        world.id,
        commandType,
        payload,
        world.current_tick
      )
      const result = await citySpatialAPI.stepWorld(world.id, world.current_tick)
      const processed = result.commands.find(item => item.id === command.id)
      if (!processed || processed.status !== 'applied') {
        throw new Error(processed?.error_code ?? 'City enterprise location command was rejected')
      }
      updateWorldTick(result.tick.tick)
      const levels = new Set<number>([0, camera.value.z, ...landLayers.value.keys()])
      await Promise.all([
        loadEnterpriseLocationState(true),
        ...[...levels].map(z => loadLandLayer(z, true))
      ])
    } catch (error: unknown) {
      loadError.value = readableError(error)
      throw error
    } finally {
      enterpriseLocationCommandCode.value = null
    }
  }

  async function runWorldRuntimeCommand(
    commandType: WorldRuntimeCommandType,
    payload: Record<string, unknown>,
    commandCode: string = commandType
  ): Promise<void> {
    const world = activeWorld.value
    if (!world || worldRuntimeAvailability.value !== 'available' || worldRuntimeCommandCode.value) return
    worldRuntimeCommandCode.value = commandCode
    loadError.value = null
    try {
      const command = await citySpatialAPI.submitWorldRuntimeCommand(
        world.id,
        commandType,
        payload,
        world.current_tick
      )
      const result = await citySpatialAPI.stepWorld(world.id, world.current_tick)
      const processed = result.commands.find(item => item.id === command.id)
      if (!processed || processed.status !== 'applied') {
        throw new Error(processed?.error_code ?? 'Open world runtime command was rejected')
      }
      updateWorldTick(result.tick.tick)
      await loadWorldRuntime(true)
    } catch (error: unknown) {
      loadError.value = readableError(error)
      throw error
    } finally {
      worldRuntimeCommandCode.value = null
    }
  }

  function setViewportSize(size: ViewportSize): void {
    const width = Math.max(240, Math.trunc(size.width))
    const height = Math.max(240, Math.trunc(size.height))
    if (viewport.value.width === width && viewport.value.height === height) return
    viewport.value = { width, height }
    if (mapMode.value === 'local') scheduleVisibleChunkLoad()
  }

  function panCamera(deltaX: number, deltaY: number): void {
    camera.value = clampCamera({
      ...camera.value,
      worldX: camera.value.worldX + Math.trunc(deltaX),
      worldY: camera.value.worldY + Math.trunc(deltaY)
    })
    selectedCoordinate.value = {
      worldX: camera.value.worldX,
      worldY: camera.value.worldY,
      z: camera.value.z
    }
    scheduleVisibleChunkLoad()
  }

  function changeZoom(direction: number): void {
    const currentIndex = CELL_SIZE_STEPS.findIndex(size => size === camera.value.cellSize)
    const nextIndex = Math.max(0, Math.min(CELL_SIZE_STEPS.length - 1, currentIndex + Math.sign(direction)))
    if (CELL_SIZE_STEPS[nextIndex] === camera.value.cellSize) return
    camera.value = { ...camera.value, cellSize: CELL_SIZE_STEPS[nextIndex] }
    scheduleVisibleChunkLoad()
  }

  function setZ(z: number): void {
    const next = clampCamera({ ...camera.value, z })
    if (next.z === camera.value.z) return
    camera.value = next
    selectedCoordinate.value = {
      worldX: next.worldX,
      worldY: next.worldY,
      z: next.z
    }
    scheduleVisibleChunkLoad()
    void loadLandLayer(next.z).catch(() => undefined)
  }

  function selectOvermapTile(tile: CityOvermapTile | null): void {
    selectedTile.value = tile
    selectedCoordinate.value = null
  }

  function openOvermapTile(tile = selectedTile.value): void {
    const currentProfile = profile.value
    if (!tile || !currentProfile) return
    const center = Math.floor(currentProfile.chunk_size / 2)
    selectedTile.value = tile
    mapMode.value = 'local'
    camera.value = clampCamera({
      ...camera.value,
      worldX: tile.chunk_x * currentProfile.chunk_size + center,
      worldY: tile.chunk_y * currentProfile.chunk_size + center,
      z: tile.z
    })
    selectedCoordinate.value = {
      worldX: camera.value.worldX,
      worldY: camera.value.worldY,
      z: camera.value.z
    }
    scheduleVisibleChunkLoad()
    void loadLandLayer(camera.value.z).catch(() => undefined)
  }

  function showOvermap(): void {
    mapMode.value = 'overmap'
    selectedCoordinate.value = null
    void loadLandLayer(0).catch(() => undefined)
  }

  function showLocalMap(): void {
    mapMode.value = 'local'
    if (!selectedCoordinate.value) {
      selectedCoordinate.value = {
        worldX: camera.value.worldX,
        worldY: camera.value.worldY,
        z: camera.value.z
      }
    }
    scheduleVisibleChunkLoad()
    void loadLandLayer(camera.value.z).catch(() => undefined)
  }

  function selectCell(cell: ProjectedCityCell | null): void {
    if (!cell) return
    selectedCoordinate.value = { worldX: cell.worldX, worldY: cell.worldY, z: cell.z }
  }

  function hoverCell(cell: ProjectedCityCell | null): void {
    hoveredCoordinate.value = cell
      ? { worldX: cell.worldX, worldY: cell.worldY, z: cell.z }
      : null
  }

  function clear(): void {
    ++worldGeneration
    if (visibleLoadTimer) clearTimeout(visibleLoadTimer)
    visibleLoadTimer = null
    activeWorldID.value = null
    worlds.value = []
    resetSpatialState()
    loadError.value = null
  }

  return {
    worlds,
    activeWorldID,
    activeWorld,
    ruleBundle,
    ruleSet,
    profile,
    overmap,
    landLayers,
    activeLandState,
    landAvailability,
    developmentState,
    developmentAvailability,
    enterpriseLocationState,
    enterpriseLocationAvailability,
    worldRuntimeCatalog,
    worldActors,
    selectedActorCode,
    worldActorState,
    worldActorRoleOptions,
    worldRuntimeRules,
    worldRuleCases,
    worldRuntimeAvailability,
    activeDevelopmentProjects,
    developmentProjectsByBuilding,
    chunkSummaries,
    projectedChunks,
    generatedChunkKeys,
    spatialChanges,
    latestChanges,
    mapMode,
    camera,
    viewport,
    selectedTile,
    selectedCoordinate,
    selectedCell,
    selectedChunk,
    hoveredCoordinate,
    initialLoading,
    refreshing,
    chunkLoading,
    landLoading,
    developmentLoading,
    enterpriseLocationLoading,
    worldRuntimeLoading,
    creatingWorld,
    generatingChunkKey,
    developmentCommandCode,
    enterpriseLocationCommandCode,
    worldRuntimeCommandCode,
    canGenerateSelectedTile,
    loadError,
    initialize,
    selectWorld,
    refresh,
    createWorld,
    generateSelectedChunk,
    loadVisibleChunks,
    loadLandLayer,
    loadDevelopmentState,
    loadEnterpriseLocationState,
    loadWorldRuntime,
    loadWorldActor,
    selectWorldActor,
    runDevelopmentCommand,
    runEnterpriseLocationCommand,
    runWorldRuntimeCommand,
    setViewportSize,
    panCamera,
    changeZoom,
    setZ,
    selectOvermapTile,
    openOvermapTile,
    showOvermap,
    showLocalMap,
    selectCell,
    hoverCell,
    clear
  }
})
