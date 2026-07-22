import { computed, ref, shallowRef } from 'vue'
import { defineStore } from 'pinia'
import citySpatialAPI, {
  type AddCityWorldMemberRequest,
  type CityCommand,
  type CityDevelopmentProject,
  type CityDevelopmentState,
  type CityEnterpriseLocationCommandType,
  type CityEnterpriseLocationState,
  type CityOpenWorldRuntimeCommandType,
  type CityPhysicalNetworkCatalogView,
  type CityPhysicalNetworkDiagnosticQuery,
  type CityPhysicalNetworkDiagnosticsView,
  type CityPhysicalNetworkEdgePage,
  type CityPhysicalNetworkFactPage,
  type CityPhysicalNetworkFlowPage,
  type CityPhysicalNetworkListQuery,
  type CityPhysicalNetworkNodePage,
  type CityPhysicalNetworkPage,
  type CityServiceCatalogView,
  type CityServiceCommandType,
  type CityServiceConnectionPage,
  type CityServiceDemandPage,
  type CityServiceFacilityPage,
  type CityServiceListQuery,
  type CityServiceSettlementPage,
  type CityRuntimeCommandType,
  type CityWorldControlCommandType,
  type CityLandState,
  type CityMapChunkSummary,
  type CityNavigationCoordinate,
  type CityNavigationPath,
  type CityOvermapState,
  type CityOvermapTile,
  type CitySpatialMutation,
  type CityMember,
  type CityWorld,
  type CityWorldSpatialRuleSet,
  type CreateCityWorldRequest,
  type UpdateCityWorldMemberRequest,
  type WorldActor,
  type WorldActorNavigationIntent,
  type WorldActorRoleOption,
  type WorldActorState,
  type WorldNavigationReservation,
  type WorldRuleCase,
  type WorldPortalAccessView,
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
export type WorldPortalAccessAvailability = 'unknown' | 'available' | 'unavailable'
export type WorldNavigationIntentAvailability = 'unknown' | 'available' | 'unavailable'
export type CityPublicServiceAvailability = 'unknown' | 'available' | 'unsupported'

// CLASSIC is a glyph renderer, not a tile renderer. Keep the two compact
// steps available so a 960×560 viewport can show 96×56 cells by default and
// 120×70 cells on the densest supported zoom level.
const CELL_SIZE_STEPS = [8, 10, 12, 16, 20, 24, 32] as const
const DEFAULT_VIEWPORT: ViewportSize = { width: 960, height: 560 }
const CHUNK_CACHE_CAPACITY = 64
const OPEN_WORLD_SIMULATION_PREFIX = 'city-openworld-'
// A running open-world scheduler advances independently of the rendered
// snapshot. Keep an actor's automatic travel projection fresh without
// toggling the page-level loading state or forcing a full workspace reload.
const OPEN_WORLD_NAVIGATION_REFRESH_MS = 1200

function readableError(error: unknown): string {
  if (isApiError(error)) return error.message
  if (error instanceof Error) return error.message
  return 'Unknown city spatial error'
}

function isExpectedWorldTickConflict(error: unknown): boolean {
  // Backend error envelopes retain the HTTP status in `code` and expose the
  // application error identifier in `reason`. Tests and older clients may
  // still carry the identifier in `code`, so accept both representations.
  return isApiError(error) && (
    error.code === 'CITY_EXPECTED_TICK_CONFLICT' ||
    error.reason === 'CITY_EXPECTED_TICK_CONFLICT'
  )
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
  const cityServiceCatalog = shallowRef<CityServiceCatalogView | null>(null)
  const cityServiceFacilities = shallowRef<CityServiceFacilityPage | null>(null)
  const cityServiceDemands = shallowRef<CityServiceDemandPage | null>(null)
  const cityServiceConnections = shallowRef<CityServiceConnectionPage | null>(null)
  const cityServiceSettlements = shallowRef<CityServiceSettlementPage | null>(null)
  const cityServiceAvailability = ref<CityPublicServiceAvailability>('unknown')
  const cityPhysicalNetworkCatalog = shallowRef<CityPhysicalNetworkCatalogView | null>(null)
  const cityPhysicalNetworks = shallowRef<CityPhysicalNetworkPage | null>(null)
  const cityPhysicalNetworkNodes = shallowRef<CityPhysicalNetworkNodePage | null>(null)
  const cityPhysicalNetworkEdges = shallowRef<CityPhysicalNetworkEdgePage | null>(null)
  const cityPhysicalNetworkFlows = shallowRef<CityPhysicalNetworkFlowPage | null>(null)
  const cityPhysicalNetworkFacts = shallowRef<CityPhysicalNetworkFactPage | null>(null)
  const cityPhysicalNetworkDiagnostics = shallowRef<CityPhysicalNetworkDiagnosticsView | null>(null)
  const cityPhysicalNetworkAvailability = ref<CityPublicServiceAvailability>('unknown')
  const worldRuntimeCatalog = shallowRef<WorldRuntimeCatalog | null>(null)
  const worldActors = ref<WorldActor[]>([])
  const selectedActorCode = ref<string | null>(null)
  const worldActorState = shallowRef<WorldActorState | null>(null)
  const navigationPath = shallowRef<CityNavigationPath | null>(null)
  const navigationLoading = ref(false)
  const navigationError = ref<string | null>(null)
  const worldPortalStates = ref<WorldPortalAccessView[]>([])
  const worldPortalAccessAvailability = ref<WorldPortalAccessAvailability>('unknown')
  const worldNavigationIntents = ref<WorldActorNavigationIntent[]>([])
  const worldNavigationReservations = ref<WorldNavigationReservation[]>([])
  const worldNavigationIntentAvailability = ref<WorldNavigationIntentAvailability>('unknown')
  const worldNavigationIntentLoading = ref(false)
  const worldNavigationIntentError = ref<string | null>(null)
  const worldActorRoleOptions = ref<WorldActorRoleOption[]>([])
  const worldRuntimeRules = ref<WorldRuntimeDefinition[]>([])
  const worldRuleCases = ref<WorldRuleCase[]>([])
  const worldMembers = ref<CityMember[]>([])
  const worldCommandReceipts = ref<CityCommand[]>([])
  const worldRuntimeAvailability = ref<WorldRuntimeAvailability>('unknown')
  const chunkSummaries = shallowRef<ReadonlyMap<string, CityMapChunkSummary>>(new Map())
  const projectedChunks = shallowRef<ReadonlyMap<string, ProjectedCityChunk>>(new Map())
  const spatialChanges = ref<CitySpatialMutation[]>([])
  const mapMode = ref<CityMapMode>('overmap')
  const camera = ref<CameraState>({ worldX: 16, worldY: 16, z: 0, cellSize: CELL_SIZE_STEPS[1] })
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
  const cityServiceLoading = ref(false)
  const cityPhysicalNetworkLoading = ref(false)
  const worldRuntimeLoading = ref(false)
  const worldPortalLoading = ref(false)
  const worldMembersLoading = ref(false)
  const creatingWorld = ref(false)
  const generatingChunkKey = ref<string | null>(null)
  const developmentCommandCode = ref<string | null>(null)
  const enterpriseLocationCommandCode = ref<string | null>(null)
  const cityServiceCommandCode = ref<string | null>(null)
  const worldRuntimeCommandCode = ref<string | null>(null)
  const worldLifecycleCommandCode = ref<string | null>(null)
  const worldMemberMutationKey = ref<string | null>(null)
  const loadError = ref<string | null>(null)

  const cache = new CityChunkCache(CHUNK_CACHE_CAPACITY)
  const inFlightChunks = new Map<string, Promise<ProjectedCityChunk>>()
  const inFlightLandLayers = new Map<string, Promise<CityLandState | null>>()
  const inFlightCommandReceipts = new Map<number, Promise<void>>()
  let worldGeneration = 0
  let navigationRequestGeneration = 0
  let worldNavigationRequestGeneration = 0
  let cityServiceRequestGeneration = 0
  const cityServiceSectionGeneration = {
    facilities: 0,
    demands: 0,
    connections: 0,
    settlements: 0
  }
  let cityPhysicalNetworkRequestGeneration = 0
  let cityPhysicalNetworkDiagnosticGeneration = 0
  let lastCityPhysicalNetworkDiagnosticQuery: CityPhysicalNetworkDiagnosticQuery | null = null
  const cityPhysicalNetworkSectionGeneration = {
    networks: 0,
    nodes: 0,
    edges: 0,
    flows: 0,
    facts: 0
  }
  let activeCityServiceLoads = 0
  let activeCityPhysicalNetworkLoads = 0
  let activeChunkLoads = 0
  let activeLandLoads = 0
  let visibleLoadTimer: ReturnType<typeof setTimeout> | null = null
  let openWorldNavigationRefreshTimer: ReturnType<typeof setTimeout> | null = null
  let openWorldNavigationRefreshGeneration = 0
  let openWorldNavigationRefreshInFlight = false

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

  function isOpenWorld(world: CityWorld | null | undefined): boolean {
    return typeof world?.simulation_version === 'string' &&
      world.simulation_version.startsWith(OPEN_WORLD_SIMULATION_PREFIX)
  }

  function openWorldActorLocation(actorCode: string): WorldActorState['location'] | undefined {
    if (worldActorState.value?.actor.code === actorCode) {
      return worldActorState.value.location ?? worldActorState.value.actor.location
    }
    return worldActors.value.find(actor => actor.code === actorCode)?.location
  }

  function requireIntegerPayload(value: unknown, field: string): number {
    const parsed = Number(value)
    if (!Number.isSafeInteger(parsed)) throw new Error(`Open world command requires integer ${field}`)
    return parsed
  }

  function adaptOpenWorldRuntimeCommand(
    commandType: WorldRuntimeCommandType,
    payload: Record<string, unknown>
  ): { commandType: CityRuntimeCommandType; payload: Record<string, unknown> } {
    const actorCode = typeof payload.actor_code === 'string' ? payload.actor_code : selectedActorCode.value
    const location = actorCode ? openWorldActorLocation(actorCode) : undefined
    const locationContext = () => ({
      space_kind: location?.space_kind ?? 'surface',
      ...(location?.space_kind === 'interior'
        ? { building_code: location.anchor_code ?? location.space_code }
        : {}),
      // Open-world interiors use z as the authoritative floor index. Keeping
      // that context is essential after a player has crossed an entrance or a
      // stair; sending floor 0 would make every upper-floor move invalid.
      floor_index: location?.space_kind === 'interior'
        ? requireIntegerPayload(location.z, 'location.z')
        : 0
    })
    switch (commandType) {
      case 'actor.create':
        return { commandType: 'open_world.actor.create', payload }
      case 'actor.activity.perform':
        return { commandType: 'open_world.actor.activity.perform', payload }
      case 'actor.role.transition':
        return { commandType: 'open_world.actor.role.transition', payload }
      case 'actor.location.move':
        if (!actorCode) throw new Error('Open world movement requires a selected actor')
        return {
          commandType: 'open_world.actor.move',
          payload: {
            actor_code: actorCode,
            ...locationContext(),
            x: requireIntegerPayload(payload.x, 'x'),
            y: requireIntegerPayload(payload.y, 'y'),
            z: requireIntegerPayload(payload.z, 'z')
          }
        }
      case 'actor.control.grant':
        return { commandType: 'open_world.actor.control.grant', payload }
      case 'actor.control.revoke':
        return { commandType: 'open_world.actor.control.revoke', payload }
      case 'portal.state.transition':
        if (!actorCode || typeof payload.portal_code !== 'string' || typeof payload.action !== 'string') {
          throw new Error('Open world portal action requires an actor and portal')
        }
        return {
          commandType: 'open_world.portal.state.set',
          payload: { actor_code: actorCode, portal_code: payload.portal_code, action: payload.action }
        }
      case 'portal.access.configure':
        if (!actorCode || typeof payload.portal_code !== 'string' || !payload.requirements) {
          throw new Error('Open world portal policy requires an actor and portal')
        }
        return {
          commandType: 'open_world.portal.access.set',
          payload: { actor_code: actorCode, portal_code: payload.portal_code, requirements: payload.requirements }
        }
      case 'actor.navigation.intent.set': {
        if (!actorCode || !payload.destination || typeof payload.destination !== 'object') {
          throw new Error('Open world navigation requires a selected actor and destination')
        }
        const destination = payload.destination as Record<string, unknown>
        const targetZ = requireIntegerPayload(destination.z, 'destination.z')
        const targetLocationContext = location?.space_kind === 'interior'
          ? {
              space_kind: 'interior',
              building_code: location.anchor_code ?? location.space_code,
              // This payload describes the destination, not the actor's
              // current floor. A player can therefore set an intent that
              // walks to and traverses registered stairs in the same building.
              floor_index: targetZ
            }
          : (() => {
              if (targetZ !== 0) {
                throw new Error('Open world surface navigation must enter a building before targeting an interior floor')
              }
              return { space_kind: 'surface', floor_index: 0 }
            })()
        return {
          commandType: 'open_world.actor.navigation.set',
          payload: {
            actor_code: actorCode,
            ...targetLocationContext,
            x: requireIntegerPayload(destination.x, 'destination.x'),
            y: requireIntegerPayload(destination.y, 'destination.y'),
            z: targetZ,
            priority: requireIntegerPayload(payload.priority, 'priority'),
            maximum_steps: requireIntegerPayload(payload.max_steps, 'max_steps')
          }
        }
      }
      case 'actor.navigation.intent.cancel':
        if (!actorCode) throw new Error('Open world navigation requires a selected actor')
        return { commandType: 'open_world.actor.navigation.cancel', payload: { actor_code: actorCode } }
    }
  }

  function isNativeOpenWorldRuntimeCommand(
    commandType: CityRuntimeCommandType
  ): commandType is CityOpenWorldRuntimeCommandType {
    return commandType.startsWith('open_world.')
  }

  function publishCache(): void {
    projectedChunks.value = cache.snapshot()
  }

  function resetSpatialState(): void {
    invalidateOpenWorldNavigationRefresh()
    ++navigationRequestGeneration
    ++worldNavigationRequestGeneration
    ++cityServiceRequestGeneration
    ++cityPhysicalNetworkRequestGeneration
    ++cityPhysicalNetworkDiagnosticGeneration
    cityServiceSectionGeneration.facilities++
    cityServiceSectionGeneration.demands++
    cityServiceSectionGeneration.connections++
    cityServiceSectionGeneration.settlements++
    cityPhysicalNetworkSectionGeneration.networks++
    cityPhysicalNetworkSectionGeneration.nodes++
    cityPhysicalNetworkSectionGeneration.edges++
    cityPhysicalNetworkSectionGeneration.flows++
    cityPhysicalNetworkSectionGeneration.facts++
    activeCityServiceLoads = 0
    activeCityPhysicalNetworkLoads = 0
    lastCityPhysicalNetworkDiagnosticQuery = null
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
    cityServiceCatalog.value = null
    cityServiceFacilities.value = null
    cityServiceDemands.value = null
    cityServiceConnections.value = null
    cityServiceSettlements.value = null
    cityServiceAvailability.value = 'unknown'
    cityPhysicalNetworkCatalog.value = null
    cityPhysicalNetworks.value = null
    cityPhysicalNetworkNodes.value = null
    cityPhysicalNetworkEdges.value = null
    cityPhysicalNetworkFlows.value = null
    cityPhysicalNetworkFacts.value = null
    cityPhysicalNetworkDiagnostics.value = null
    cityPhysicalNetworkAvailability.value = 'unknown'
    cityServiceCommandCode.value = null
    worldRuntimeCommandCode.value = null
    worldLifecycleCommandCode.value = null
    worldRuntimeCatalog.value = null
    worldActors.value = []
    selectedActorCode.value = null
    worldActorState.value = null
    navigationPath.value = null
    navigationLoading.value = false
    navigationError.value = null
    worldPortalStates.value = []
    worldPortalAccessAvailability.value = 'unknown'
    worldNavigationIntents.value = []
    worldNavigationReservations.value = []
    worldNavigationIntentAvailability.value = 'unknown'
    worldNavigationIntentLoading.value = false
    worldNavigationIntentError.value = null
    worldActorRoleOptions.value = []
    worldRuntimeRules.value = []
    worldRuleCases.value = []
    worldMembers.value = []
    worldCommandReceipts.value = []
    worldRuntimeAvailability.value = 'unknown'
    cityServiceLoading.value = false
    cityPhysicalNetworkLoading.value = false
    worldPortalLoading.value = false
    worldMembersLoading.value = false
    spatialChanges.value = []
    selectedTile.value = null
    selectedCoordinate.value = null
    hoveredCoordinate.value = null
    mapMode.value = 'overmap'
    camera.value = { worldX: 16, worldY: 16, z: 0, cellSize: CELL_SIZE_STEPS[1] }
  }

  function updateWorldTick(tick: number): void {
    worlds.value = worlds.value.map(world => (
      world.id === activeWorldID.value ? { ...world, current_tick: tick } : world
    ))
  }

  function applyWorldLifecycleResult(
    commandType: CityWorldControlCommandType,
    payload: Record<string, unknown>
  ): void {
    worlds.value = worlds.value.map(world => {
      if (world.id !== activeWorldID.value) return world
      if (commandType === 'world.pause') {
        return { ...world, status: 'paused', next_tick_at: undefined }
      }
      if (commandType === 'world.resume') {
        return { ...world, status: 'running' }
      }
      const speedMilli = Number(payload.speed_milli)
      if (!Number.isSafeInteger(speedMilli) || speedMilli < 1) return world
      return { ...world, speed_multiplier: speedMilli / 1000 }
    })
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

  function isWorldPortalAccessNotFound(error: unknown): boolean {
    return isApiError(error) && (
      error.status === 404 || String(error.code ?? '') === 'WORLD_PORTAL_ACCESS_UNAVAILABLE'
    )
  }

  function isWorldNavigationIntentNotFound(error: unknown): boolean {
    return isApiError(error) && (
      error.status === 404 || String(error.code ?? '') === 'WORLD_NAVIGATION_INTENT_UNAVAILABLE'
    )
  }

  async function requestWorldPortalStates(
    worldID: number,
    actorCode: string | undefined,
    force: boolean
  ): Promise<{ items: WorldPortalAccessView[]; availability: WorldPortalAccessAvailability }> {
    if (worldPortalAccessAvailability.value === 'unavailable' && !force) {
      return { items: [], availability: 'unavailable' }
    }
    try {
      return {
        items: await citySpatialAPI.listWorldPortalStates(worldID, actorCode),
        availability: 'available'
      }
    } catch (error: unknown) {
      if (isWorldPortalAccessNotFound(error)) {
        return { items: [], availability: 'unavailable' }
      }
      throw error
    }
  }

  async function loadWorldPortalStates(
    actorCode: string | null = selectedActorCode.value,
    force = false
  ): Promise<WorldPortalAccessView[]> {
    const worldID = activeWorldID.value
    if (!worldID) return []
    const normalizedActorCode = actorCode || undefined
    const token = worldGeneration
    worldPortalLoading.value = true
    try {
      const snapshot = await requestWorldPortalStates(worldID, normalizedActorCode, force)
      if (
        token !== worldGeneration || activeWorldID.value !== worldID ||
        (normalizedActorCode ?? null) !== selectedActorCode.value
      ) return []
      worldPortalStates.value = snapshot.items
      worldPortalAccessAvailability.value = snapshot.availability
      return snapshot.items
    } catch (error: unknown) {
      if (token === worldGeneration && activeWorldID.value === worldID) {
        loadError.value = readableError(error)
      }
      throw error
    } finally {
      if (token === worldGeneration) worldPortalLoading.value = false
    }
  }

  async function requestWorldNavigationSnapshot(
    worldID: number,
    force: boolean
  ): Promise<{
    intents: WorldActorNavigationIntent[]
    reservations: WorldNavigationReservation[]
    availability: WorldNavigationIntentAvailability
  }> {
    if (worldNavigationIntentAvailability.value === 'unavailable' && !force) {
      return { intents: [], reservations: [], availability: 'unavailable' }
    }
    try {
      const [intents, reservations] = await Promise.all([
        citySpatialAPI.listWorldNavigationIntents(worldID),
        citySpatialAPI.listWorldNavigationReservations(worldID)
      ])
      return { intents, reservations, availability: 'available' }
    } catch (error: unknown) {
      if (isWorldNavigationIntentNotFound(error)) {
        return { intents: [], reservations: [], availability: 'unavailable' }
      }
      throw error
    }
  }

  async function loadWorldNavigationState(force = false): Promise<void> {
    const worldID = activeWorldID.value
    if (!worldID) return
    const requestToken = ++worldNavigationRequestGeneration
    const worldToken = worldGeneration
    worldNavigationIntentLoading.value = true
    worldNavigationIntentError.value = null
    try {
      const snapshot = await requestWorldNavigationSnapshot(worldID, force)
      if (
        requestToken !== worldNavigationRequestGeneration || worldToken !== worldGeneration ||
        activeWorldID.value !== worldID
      ) return
      worldNavigationIntents.value = snapshot.intents
      worldNavigationReservations.value = snapshot.reservations
      worldNavigationIntentAvailability.value = snapshot.availability
    } catch (error: unknown) {
      if (
        requestToken === worldNavigationRequestGeneration && worldToken === worldGeneration &&
        activeWorldID.value === worldID
      ) {
        worldNavigationIntentError.value = readableError(error)
      }
    } finally {
      if (
        requestToken === worldNavigationRequestGeneration && worldToken === worldGeneration &&
        activeWorldID.value === worldID
      ) {
        worldNavigationIntentLoading.value = false
      }
    }
  }

  function hasActiveSelectedNavigationIntent(actorCode = selectedActorCode.value): boolean {
    if (!actorCode) return false
    if (
      worldActorState.value?.actor.code === actorCode &&
      worldActorState.value.navigation_intent?.status === 'active'
    ) return true
    return worldNavigationIntents.value.some(intent => (
      intent.actor_code === actorCode && intent.status === 'active'
    ))
  }

  function shouldRefreshOpenWorldNavigation(): boolean {
    const world = activeWorld.value
    return Boolean(
      isOpenWorld(world) &&
      world?.status === 'running' &&
      selectedActorCode.value &&
      hasActiveSelectedNavigationIntent()
    )
  }

  function invalidateOpenWorldNavigationRefresh(): void {
    ++openWorldNavigationRefreshGeneration
    if (openWorldNavigationRefreshTimer) clearTimeout(openWorldNavigationRefreshTimer)
    openWorldNavigationRefreshTimer = null
  }

  function scheduleOpenWorldNavigationRefresh(delay = OPEN_WORLD_NAVIGATION_REFRESH_MS): void {
    if (openWorldNavigationRefreshTimer) return
    const token = openWorldNavigationRefreshGeneration
    openWorldNavigationRefreshTimer = setTimeout(() => {
      openWorldNavigationRefreshTimer = null
      void refreshOpenWorldNavigationProjection(token)
    }, delay)
  }

  function syncOpenWorldNavigationRefresh(): void {
    if (!shouldRefreshOpenWorldNavigation()) {
      invalidateOpenWorldNavigationRefresh()
      return
    }
    scheduleOpenWorldNavigationRefresh()
  }

  async function refreshOpenWorldNavigationProjection(token: number): Promise<void> {
    if (token !== openWorldNavigationRefreshGeneration) return
    if (!shouldRefreshOpenWorldNavigation()) {
      invalidateOpenWorldNavigationRefresh()
      return
    }
    if (openWorldNavigationRefreshInFlight) {
      scheduleOpenWorldNavigationRefresh(250)
      return
    }

    const worldID = activeWorldID.value
    const actorCode = selectedActorCode.value
    const worldToken = worldGeneration
    if (!worldID || !actorCode) {
      invalidateOpenWorldNavigationRefresh()
      return
    }

    openWorldNavigationRefreshInFlight = true
    let shouldContinue = false
    try {
      const [world, state, navigation, portals] = await Promise.all([
        citySpatialAPI.getWorld(worldID),
        citySpatialAPI.getWorldActorState(worldID, actorCode),
        requestWorldNavigationSnapshot(worldID, true),
        requestWorldPortalStates(worldID, actorCode, true)
      ])
      if (
        token !== openWorldNavigationRefreshGeneration ||
        worldToken !== worldGeneration ||
        activeWorldID.value !== worldID ||
        selectedActorCode.value !== actorCode
      ) return

      replaceWorldSnapshot(world)
      worldActorState.value = state
      const location = state.location ?? state.actor.location
      worldActors.value = worldActors.value.map(actor => (
        actor.code === actorCode
          ? { ...actor, ...state.actor, location: location ?? actor.location }
          : actor
      ))
      worldNavigationIntents.value = navigation.intents
      worldNavigationReservations.value = navigation.reservations
      worldNavigationIntentAvailability.value = navigation.availability
      worldPortalStates.value = portals.items
      worldPortalAccessAvailability.value = portals.availability
      shouldContinue = shouldRefreshOpenWorldNavigation()
    } catch {
      // Navigation projection is auxiliary to the server-authoritative
      // scheduler. Keep a transient polling failure invisible and retry while
      // the selected actor still has a live travel intent.
      shouldContinue = token === openWorldNavigationRefreshGeneration &&
        worldToken === worldGeneration &&
        activeWorldID.value === worldID &&
        selectedActorCode.value === actorCode &&
        shouldRefreshOpenWorldNavigation()
    } finally {
      openWorldNavigationRefreshInFlight = false
      if (token !== openWorldNavigationRefreshGeneration) return
      if (shouldContinue) {
        scheduleOpenWorldNavigationRefresh()
      } else if (!shouldRefreshOpenWorldNavigation()) {
        invalidateOpenWorldNavigationRefresh()
      }
    }
  }

  function upsertWorldCommandReceipt(command: CityCommand): void {
    worldCommandReceipts.value = [
      command,
      ...worldCommandReceipts.value.filter(item => item.id !== command.id)
    ]
      .sort((left, right) => right.sequence - left.sequence)
      .slice(0, 24)
  }

  function replaceWorldSnapshot(world: CityWorld): void {
    worlds.value = worlds.value.map(item => item.id === world.id ? world : item)
  }

  function waitForReceiptPoll(milliseconds: number): Promise<void> {
    return new Promise(resolve => setTimeout(resolve, milliseconds))
  }

  async function pollWorldCommandReceipt(worldID: number, commandID: number, token: number): Promise<void> {
    const existing = inFlightCommandReceipts.get(commandID)
    if (existing) return existing
    const request = (async () => {
      const pollDelays = [0, 400, 600, 800, 1000, 1400, 1800, 2400, 3000, 4000, 5000, 6000]
      for (const delay of pollDelays) {
        if (delay > 0) await waitForReceiptPoll(delay)
        if (token !== worldGeneration || activeWorldID.value !== worldID) return
        try {
          const receipt = await citySpatialAPI.getCommand(worldID, commandID)
          if (token !== worldGeneration || activeWorldID.value !== worldID) return
          upsertWorldCommandReceipt(receipt)
          if (receipt.status === 'pending') continue
          if (receipt.processed_tick !== undefined) updateWorldTick(receipt.processed_tick)
          try {
            const world = await citySpatialAPI.getWorld(worldID)
            if (token === worldGeneration && activeWorldID.value === worldID) replaceWorldSnapshot(world)
          } catch {
            // The terminal command receipt remains authoritative when the world snapshot refresh is transiently unavailable.
          }
          if (receipt.status === 'applied' && token === worldGeneration && activeWorldID.value === worldID) {
            await loadWorldRuntime(true)
          }
          return
        } catch {
          // Receipt polling tolerates transient network failures and retries within a bounded window.
        }
      }
    })().finally(() => {
      if (inFlightCommandReceipts.get(commandID) === request) inFlightCommandReceipts.delete(commandID)
    })
    inFlightCommandReceipts.set(commandID, request)
    return request
  }

  async function loadWorldMembers(): Promise<CityMember[]> {
    const worldID = activeWorldID.value
    if (!worldID) return []
    const token = worldGeneration
    worldMembersLoading.value = true
    try {
      const members = await citySpatialAPI.listWorldMembers(worldID)
      if (token !== worldGeneration || activeWorldID.value !== worldID) return []
      worldMembers.value = members
      return members
    } catch (error: unknown) {
      if (token === worldGeneration && activeWorldID.value === worldID) loadError.value = readableError(error)
      throw error
    } finally {
      if (token === worldGeneration) worldMembersLoading.value = false
    }
  }

  async function loadWorldCommandReceipts(): Promise<CityCommand[]> {
    const worldID = activeWorldID.value
    if (!worldID) return []
    const token = worldGeneration
    try {
      const page = await citySpatialAPI.listCommands(worldID, { latest: true, limit: 24 })
      if (token !== worldGeneration || activeWorldID.value !== worldID) return []
      const merged = new Map<number, CityCommand>()
      for (const receipt of worldCommandReceipts.value) merged.set(receipt.id, receipt)
      for (const receipt of page.items) merged.set(receipt.id, receipt)
      worldCommandReceipts.value = [...merged.values()]
        .sort((left, right) => right.sequence - left.sequence)
        .slice(0, 24)
      return worldCommandReceipts.value
    } catch (error: unknown) {
      if (token === worldGeneration && activeWorldID.value === worldID) loadError.value = readableError(error)
      throw error
    }
  }

  async function addWorldMember(request: AddCityWorldMemberRequest): Promise<CityMember> {
    const world = activeWorld.value
    if (!world || world.member_role !== 'owner' || worldMemberMutationKey.value) {
      throw new Error('City member management is unavailable')
    }
    worldMemberMutationKey.value = 'add'
    loadError.value = null
    try {
      const member = await citySpatialAPI.addWorldMember(world.id, request)
      worldMembers.value = [member, ...worldMembers.value.filter(item => item.user_id !== member.user_id)]
      return member
    } catch (error: unknown) {
      loadError.value = readableError(error)
      throw error
    } finally {
      worldMemberMutationKey.value = null
    }
  }

  async function updateWorldMember(
    userID: number,
    request: UpdateCityWorldMemberRequest
  ): Promise<CityMember> {
    const world = activeWorld.value
    if (!world || world.member_role !== 'owner' || worldMemberMutationKey.value || userID <= 0) {
      throw new Error('City member management is unavailable')
    }
    worldMemberMutationKey.value = `member:${userID}`
    loadError.value = null
    try {
      const member = await citySpatialAPI.updateWorldMember(world.id, userID, request)
      worldMembers.value = member.status === 'active'
        ? worldMembers.value.map(item => item.user_id === member.user_id ? member : item)
        : worldMembers.value.filter(item => item.user_id !== member.user_id)
      return member
    } catch (error: unknown) {
      loadError.value = readableError(error)
      throw error
    } finally {
      worldMemberMutationKey.value = null
    }
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

  function beginCityServiceLoad(): void {
    activeCityServiceLoads++
    cityServiceLoading.value = true
  }

  function endCityServiceLoad(): void {
    activeCityServiceLoads = Math.max(0, activeCityServiceLoads - 1)
    cityServiceLoading.value = activeCityServiceLoads > 0
  }

  async function loadCityServices(force = false): Promise<CityServiceCatalogView | null> {
    const worldID = activeWorldID.value
    if (!worldID || (cityServiceAvailability.value === 'unsupported' && !force)) return null
    if (cityServiceCatalog.value && !force) return cityServiceCatalog.value
    const worldToken = worldGeneration
    const requestToken = ++cityServiceRequestGeneration
    cityServiceSectionGeneration.facilities++
    cityServiceSectionGeneration.demands++
    cityServiceSectionGeneration.connections++
    cityServiceSectionGeneration.settlements++
    beginCityServiceLoad()
    try {
      const catalog = await citySpatialAPI.getServiceCatalog(worldID)
      if (
        worldToken !== worldGeneration || requestToken !== cityServiceRequestGeneration ||
        activeWorldID.value !== worldID
      ) return null
      cityServiceCatalog.value = catalog
      if (catalog.availability === 'unsupported') {
        cityServiceFacilities.value = null
        cityServiceDemands.value = null
        cityServiceConnections.value = null
        cityServiceSettlements.value = null
        cityServiceAvailability.value = 'unsupported'
        return catalog
      }
      const [facilities, demands, connections, settlements] = await Promise.all([
        citySpatialAPI.listServiceFacilities(worldID),
        citySpatialAPI.listServiceDemands(worldID),
        citySpatialAPI.listServiceConnections(worldID),
        citySpatialAPI.listServiceSettlements(worldID)
      ])
      if (
        worldToken !== worldGeneration || requestToken !== cityServiceRequestGeneration ||
        activeWorldID.value !== worldID
      ) return null
      if ([facilities, demands, connections, settlements].some(page => page.availability !== 'available')) {
        throw new Error('City public-service API availability is inconsistent')
      }
      cityServiceFacilities.value = facilities
      cityServiceDemands.value = demands
      cityServiceConnections.value = connections
      cityServiceSettlements.value = settlements
      cityServiceAvailability.value = 'available'
      return catalog
    } catch (error: unknown) {
      if (
        worldToken === worldGeneration && requestToken === cityServiceRequestGeneration &&
        activeWorldID.value === worldID
      ) {
        cityServiceAvailability.value = 'unknown'
        loadError.value = readableError(error)
      }
      throw error
    } finally {
      endCityServiceLoad()
    }
  }

  async function queryCityServiceSection(
    section: keyof typeof cityServiceSectionGeneration,
    query: CityServiceListQuery = {},
    append = false
  ): Promise<void> {
    const worldID = activeWorldID.value
    if (!worldID || cityServiceAvailability.value !== 'available') return
    const worldToken = worldGeneration
    const requestToken = ++cityServiceSectionGeneration[section]
    beginCityServiceLoad()
    try {
      if (section === 'facilities') {
        const page = await citySpatialAPI.listServiceFacilities(worldID, query)
        if (worldToken !== worldGeneration || requestToken !== cityServiceSectionGeneration.facilities || activeWorldID.value !== worldID) return
        cityServiceFacilities.value = append && cityServiceFacilities.value
          ? { ...page, items: [...cityServiceFacilities.value.items, ...page.items] }
          : page
      } else if (section === 'demands') {
        const page = await citySpatialAPI.listServiceDemands(worldID, query)
        if (worldToken !== worldGeneration || requestToken !== cityServiceSectionGeneration.demands || activeWorldID.value !== worldID) return
        cityServiceDemands.value = append && cityServiceDemands.value
          ? { ...page, items: [...cityServiceDemands.value.items, ...page.items] }
          : page
      } else if (section === 'connections') {
        const page = await citySpatialAPI.listServiceConnections(worldID, query)
        if (worldToken !== worldGeneration || requestToken !== cityServiceSectionGeneration.connections || activeWorldID.value !== worldID) return
        cityServiceConnections.value = append && cityServiceConnections.value
          ? { ...page, items: [...cityServiceConnections.value.items, ...page.items] }
          : page
      } else {
        const page = await citySpatialAPI.listServiceSettlements(worldID, query)
        if (worldToken !== worldGeneration || requestToken !== cityServiceSectionGeneration.settlements || activeWorldID.value !== worldID) return
        cityServiceSettlements.value = append && cityServiceSettlements.value
          ? { ...page, items: [...cityServiceSettlements.value.items, ...page.items] }
          : page
      }
    } catch (error: unknown) {
      if (worldToken === worldGeneration && activeWorldID.value === worldID) {
        loadError.value = readableError(error)
      }
      throw error
    } finally {
      endCityServiceLoad()
    }
  }

  function beginCityPhysicalNetworkLoad(): void {
    activeCityPhysicalNetworkLoads++
    cityPhysicalNetworkLoading.value = true
  }

  function endCityPhysicalNetworkLoad(): void {
    activeCityPhysicalNetworkLoads = Math.max(0, activeCityPhysicalNetworkLoads - 1)
    cityPhysicalNetworkLoading.value = activeCityPhysicalNetworkLoads > 0
  }

  function clearCityPhysicalNetworkPages(): void {
    cityPhysicalNetworks.value = null
    cityPhysicalNetworkNodes.value = null
    cityPhysicalNetworkEdges.value = null
    cityPhysicalNetworkFlows.value = null
    cityPhysicalNetworkFacts.value = null
    cityPhysicalNetworkDiagnostics.value = null
    lastCityPhysicalNetworkDiagnosticQuery = null
  }

  async function loadCityPhysicalNetworks(force = false): Promise<CityPhysicalNetworkCatalogView | null> {
    const worldID = activeWorldID.value
    if (!worldID || (cityPhysicalNetworkAvailability.value === 'unsupported' && !force)) return null
    if (cityPhysicalNetworkCatalog.value && !force) return cityPhysicalNetworkCatalog.value
    const worldToken = worldGeneration
    const requestToken = ++cityPhysicalNetworkRequestGeneration
    const diagnosticQuery = lastCityPhysicalNetworkDiagnosticQuery
      ? { ...lastCityPhysicalNetworkDiagnosticQuery }
      : null
    ++cityPhysicalNetworkDiagnosticGeneration
    cityPhysicalNetworkSectionGeneration.networks++
    cityPhysicalNetworkSectionGeneration.nodes++
    cityPhysicalNetworkSectionGeneration.edges++
    cityPhysicalNetworkSectionGeneration.flows++
    cityPhysicalNetworkSectionGeneration.facts++
    beginCityPhysicalNetworkLoad()
    try {
      const catalog = await citySpatialAPI.getPhysicalNetworkCatalog(worldID)
      if (
        worldToken !== worldGeneration || requestToken !== cityPhysicalNetworkRequestGeneration ||
        activeWorldID.value !== worldID
      ) return null
      cityPhysicalNetworkCatalog.value = catalog
      if (catalog.availability === 'unsupported') {
        clearCityPhysicalNetworkPages()
        cityPhysicalNetworkAvailability.value = 'unsupported'
        return catalog
      }
      const [networks, nodes, edges, flows, facts] = await Promise.all([
        citySpatialAPI.listPhysicalNetworks(worldID),
        citySpatialAPI.listPhysicalNetworkNodes(worldID),
        citySpatialAPI.listPhysicalNetworkEdges(worldID),
        citySpatialAPI.listPhysicalNetworkFlows(worldID),
        citySpatialAPI.listPhysicalNetworkFacts(worldID)
      ])
      if (
        worldToken !== worldGeneration || requestToken !== cityPhysicalNetworkRequestGeneration ||
        activeWorldID.value !== worldID
      ) return null
      if ([networks, nodes, edges, flows, facts].some(page => page.availability !== 'available')) {
        throw new Error('City physical-network API availability is inconsistent')
      }
      cityPhysicalNetworks.value = networks
      cityPhysicalNetworkNodes.value = nodes
      cityPhysicalNetworkEdges.value = edges
      cityPhysicalNetworkFlows.value = flows
      cityPhysicalNetworkFacts.value = facts
      cityPhysicalNetworkAvailability.value = 'available'
      if (diagnosticQuery) {
        if (networks.items.some(item => item.code === diagnosticQuery.network)) {
          try {
            await queryCityPhysicalNetworkDiagnostics(diagnosticQuery)
          } catch {
            // Keep the last committed diagnostic projection; an explicit probe will surface the error.
          }
        } else {
          cityPhysicalNetworkDiagnostics.value = null
          lastCityPhysicalNetworkDiagnosticQuery = null
        }
      }
      return catalog
    } catch (error: unknown) {
      if (
        worldToken === worldGeneration && requestToken === cityPhysicalNetworkRequestGeneration &&
        activeWorldID.value === worldID
      ) {
        if (!cityPhysicalNetworkCatalog.value) cityPhysicalNetworkAvailability.value = 'unknown'
        loadError.value = readableError(error)
      }
      throw error
    } finally {
      endCityPhysicalNetworkLoad()
    }
  }

  async function queryCityPhysicalNetworkSection(
    section: keyof typeof cityPhysicalNetworkSectionGeneration,
    query: CityPhysicalNetworkListQuery = {},
    append = false
  ): Promise<void> {
    const worldID = activeWorldID.value
    if (!worldID || cityPhysicalNetworkAvailability.value !== 'available') return
    const worldToken = worldGeneration
    const requestToken = ++cityPhysicalNetworkSectionGeneration[section]
    beginCityPhysicalNetworkLoad()
    try {
      if (section === 'networks') {
        const page = await citySpatialAPI.listPhysicalNetworks(worldID, query)
        if (worldToken !== worldGeneration || requestToken !== cityPhysicalNetworkSectionGeneration.networks || activeWorldID.value !== worldID) return
        cityPhysicalNetworks.value = append && cityPhysicalNetworks.value
          ? { ...page, items: [...cityPhysicalNetworks.value.items, ...page.items] }
          : page
      } else if (section === 'nodes') {
        const page = await citySpatialAPI.listPhysicalNetworkNodes(worldID, query)
        if (worldToken !== worldGeneration || requestToken !== cityPhysicalNetworkSectionGeneration.nodes || activeWorldID.value !== worldID) return
        cityPhysicalNetworkNodes.value = append && cityPhysicalNetworkNodes.value
          ? { ...page, items: [...cityPhysicalNetworkNodes.value.items, ...page.items] }
          : page
      } else if (section === 'edges') {
        const page = await citySpatialAPI.listPhysicalNetworkEdges(worldID, query)
        if (worldToken !== worldGeneration || requestToken !== cityPhysicalNetworkSectionGeneration.edges || activeWorldID.value !== worldID) return
        cityPhysicalNetworkEdges.value = append && cityPhysicalNetworkEdges.value
          ? { ...page, items: [...cityPhysicalNetworkEdges.value.items, ...page.items] }
          : page
      } else if (section === 'flows') {
        const page = await citySpatialAPI.listPhysicalNetworkFlows(worldID, query)
        if (worldToken !== worldGeneration || requestToken !== cityPhysicalNetworkSectionGeneration.flows || activeWorldID.value !== worldID) return
        cityPhysicalNetworkFlows.value = append && cityPhysicalNetworkFlows.value
          ? { ...page, items: [...cityPhysicalNetworkFlows.value.items, ...page.items] }
          : page
      } else {
        const page = await citySpatialAPI.listPhysicalNetworkFacts(worldID, query)
        if (worldToken !== worldGeneration || requestToken !== cityPhysicalNetworkSectionGeneration.facts || activeWorldID.value !== worldID) return
        cityPhysicalNetworkFacts.value = append && cityPhysicalNetworkFacts.value
          ? { ...page, items: [...cityPhysicalNetworkFacts.value.items, ...page.items] }
          : page
      }
    } catch (error: unknown) {
      if (worldToken === worldGeneration && activeWorldID.value === worldID) {
        loadError.value = readableError(error)
      }
      throw error
    } finally {
      endCityPhysicalNetworkLoad()
    }
  }

  async function queryCityPhysicalNetworkDiagnostics(
    query: CityPhysicalNetworkDiagnosticQuery
  ): Promise<CityPhysicalNetworkDiagnosticsView | null> {
    const worldID = activeWorldID.value
    if (!worldID || cityPhysicalNetworkAvailability.value !== 'available' || !query.network) return null
    lastCityPhysicalNetworkDiagnosticQuery = { ...query }
    const worldToken = worldGeneration
    const requestToken = ++cityPhysicalNetworkDiagnosticGeneration
    beginCityPhysicalNetworkLoad()
    try {
      const diagnostics = await citySpatialAPI.getPhysicalNetworkDiagnostics(worldID, query)
      if (
        worldToken !== worldGeneration || requestToken !== cityPhysicalNetworkDiagnosticGeneration ||
        activeWorldID.value !== worldID
      ) return null
      cityPhysicalNetworkDiagnostics.value = diagnostics
      return diagnostics
    } catch (error: unknown) {
      if (
        worldToken === worldGeneration && requestToken === cityPhysicalNetworkDiagnosticGeneration &&
        activeWorldID.value === worldID
      ) loadError.value = readableError(error)
      throw error
    } finally {
      endCityPhysicalNetworkLoad()
    }
  }

  async function loadWorldActor(actorCode: string): Promise<WorldActorState | null> {
    const worldID = activeWorldID.value
    if (!worldID || !actorCode || worldRuntimeAvailability.value === 'unavailable') return null
    const token = worldGeneration
    worldRuntimeLoading.value = true
    worldPortalLoading.value = true
    try {
      const [state, options, cases, portals] = await Promise.all([
        citySpatialAPI.getWorldActorState(worldID, actorCode),
        citySpatialAPI.getWorldActorRoleOptions(worldID, actorCode),
        citySpatialAPI.listWorldRuleCases(worldID, { actor_code: actorCode, limit: 100 }),
        requestWorldPortalStates(worldID, actorCode, false),
        loadWorldNavigationState(false)
      ])
      if (token !== worldGeneration || activeWorldID.value !== worldID || selectedActorCode.value !== actorCode) {
        return null
      }
      worldActorState.value = state
      worldActorRoleOptions.value = options
      worldRuleCases.value = cases.items
      worldPortalStates.value = portals.items
      worldPortalAccessAvailability.value = portals.availability
      syncOpenWorldNavigationRefresh()
      return state
    } catch (error: unknown) {
      if (token === worldGeneration && activeWorldID.value === worldID) {
        loadError.value = readableError(error)
      }
      throw error
    } finally {
      if (token === worldGeneration) worldRuntimeLoading.value = false
      if (token === worldGeneration) worldPortalLoading.value = false
    }
  }

  async function selectWorldActor(actorCode: string): Promise<void> {
    if (!worldActors.value.some(actor => actor.code === actorCode)) return
    invalidateOpenWorldNavigationRefresh()
    clearNavigationPath()
    selectedActorCode.value = actorCode
    worldActorState.value = null
    worldActorRoleOptions.value = []
    worldPortalStates.value = []
    await loadWorldActor(actorCode)
  }

  async function focusWorldActor(actorCode: string): Promise<void> {
    const actor = worldActors.value.find(item => item.code === actorCode)
    if (!actor) return
    await selectWorldActor(actorCode)
    const location = worldActorState.value?.location ?? actor.location
    if (!location) return
    mapMode.value = 'local'
    camera.value = clampCamera({
      ...camera.value,
      worldX: location.x,
      worldY: location.y,
      z: location.z
    })
    selectedCoordinate.value = {
      worldX: location.x,
      worldY: location.y,
      z: location.z
    }
    selectedTile.value = overmap.value?.tiles.find(tile => (
      tile.chunk_x === location.chunk_x && tile.chunk_y === location.chunk_y && tile.z === 0
    )) ?? null
    scheduleVisibleChunkLoad()
    await loadLandLayer(location.z)
  }

  function clearNavigationPath(): void {
    ++navigationRequestGeneration
    navigationPath.value = null
    navigationLoading.value = false
    navigationError.value = null
  }

  async function previewWorldActorPath(
    destination?: CityNavigationCoordinate,
    maxSteps = 256
  ): Promise<CityNavigationPath | null> {
    const worldID = activeWorldID.value
    const actorCode = selectedActorCode.value
    const selected = selectedCoordinate.value
    const target = destination ?? (selected
      ? { x: selected.worldX, y: selected.worldY, z: selected.z }
      : null)
    if (!worldID || !actorCode || !target || !Number.isSafeInteger(maxSteps) || maxSteps < 1 || maxSteps > 1024) {
      throw new Error('City actor navigation path is unavailable')
    }
    const requestToken = ++navigationRequestGeneration
    const worldToken = worldGeneration
    navigationLoading.value = true
    navigationError.value = null
    try {
      const path = await citySpatialAPI.findWorldActorPath(worldID, actorCode, target, maxSteps)
      if (
        requestToken !== navigationRequestGeneration || worldToken !== worldGeneration ||
        activeWorldID.value !== worldID || selectedActorCode.value !== actorCode
      ) return null
      navigationPath.value = path
      return path
    } catch (error: unknown) {
      if (requestToken === navigationRequestGeneration && worldToken === worldGeneration) {
        navigationPath.value = null
        navigationError.value = readableError(error)
      }
      throw error
    } finally {
      if (requestToken === navigationRequestGeneration && worldToken === worldGeneration) {
        navigationLoading.value = false
      }
    }
  }

  async function loadWorldRuntime(force = false): Promise<WorldRuntimeCatalog | null> {
    const worldID = activeWorldID.value
    if (!worldID || (worldRuntimeAvailability.value === 'unavailable' && !force)) return null
    if (worldRuntimeCatalog.value && !force) return worldRuntimeCatalog.value
    const token = worldGeneration
    worldRuntimeLoading.value = true
    worldPortalLoading.value = true
    try {
      const [catalog, actors, rules] = await Promise.all([
        citySpatialAPI.getWorldRuntimeCatalog(worldID),
        citySpatialAPI.listWorldActors(worldID),
        citySpatialAPI.listWorldRuntimeRules(worldID),
        loadWorldNavigationState(force)
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
        const portals = await requestWorldPortalStates(worldID, undefined, force)
        if (token !== worldGeneration || activeWorldID.value !== worldID || selectedActorCode.value !== null) {
          return null
        }
        worldActorState.value = null
        worldActorRoleOptions.value = []
        worldRuleCases.value = []
        worldPortalStates.value = portals.items
        worldPortalAccessAvailability.value = portals.availability
        syncOpenWorldNavigationRefresh()
        return catalog
      }
      const [state, options, cases, portals] = await Promise.all([
        citySpatialAPI.getWorldActorState(worldID, actorCode),
        citySpatialAPI.getWorldActorRoleOptions(worldID, actorCode),
        citySpatialAPI.listWorldRuleCases(worldID, { actor_code: actorCode, limit: 100 }),
        requestWorldPortalStates(worldID, actorCode, force)
      ])
      if (token !== worldGeneration || activeWorldID.value !== worldID || selectedActorCode.value !== actorCode) {
        return null
      }
      worldActorState.value = state
      worldActorRoleOptions.value = options
      worldRuleCases.value = cases.items
      worldPortalStates.value = portals.items
      worldPortalAccessAvailability.value = portals.availability
      syncOpenWorldNavigationRefresh()
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
        worldPortalStates.value = []
        worldPortalAccessAvailability.value = 'unavailable'
        ++worldNavigationRequestGeneration
        worldNavigationIntents.value = []
        worldNavigationReservations.value = []
        worldNavigationIntentAvailability.value = 'unavailable'
        worldNavigationIntentLoading.value = false
        worldNavigationIntentError.value = null
        worldRuntimeAvailability.value = 'unavailable'
        invalidateOpenWorldNavigationRefresh()
        return null
      }
      loadError.value = readableError(error)
      throw error
    } finally {
      if (token === worldGeneration) worldRuntimeLoading.value = false
      if (token === worldGeneration) worldPortalLoading.value = false
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
    const targetWorld = worlds.value.find(world => world.id === worldID)
    if (!targetWorld) return
    const token = ++worldGeneration
    activeWorldID.value = worldID
    loadError.value = null
    resetSpatialState()
    initialLoading.value = true
    try {
      if (isOpenWorld(targetWorld)) {
        await Promise.all([
          loadWorldRuntime(true),
          loadWorldMembers(),
          loadWorldCommandReceipts()
        ])
        return
      }
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
        loadCityServices(),
        loadCityPhysicalNetworks(),
        loadWorldRuntime(),
        loadWorldMembers(),
        loadWorldCommandReceipts()
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
    clearNavigationPath()
    loadError.value = null
    try {
      const nextWorlds = await citySpatialAPI.listWorlds()
      if (token !== worldGeneration || activeWorldID.value !== worldID) return
      worlds.value = nextWorlds
      if (isOpenWorld(nextWorlds.find(world => world.id === worldID))) {
        await Promise.all([
          loadWorldRuntime(true),
          loadWorldMembers(),
          loadWorldCommandReceipts()
        ])
        return
      }
      const [nextRuleBundle, nextOvermap, changes] = await Promise.all([
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
      ruleBundle.value = nextRuleBundle
      overmap.value = nextOvermap
      spatialChanges.value = changes.items
      const landLevels = new Set([0, mapMode.value === 'local' ? camera.value.z : 0])
      await Promise.all([
        loadVisibleChunks(true),
        ...[...landLevels].map(z => loadLandLayer(z, true)),
        loadDevelopmentState(true),
        loadEnterpriseLocationState(true),
        loadCityServices(true),
        loadCityPhysicalNetworks(true),
        loadWorldRuntime(true),
        loadWorldMembers(),
        loadWorldCommandReceipts()
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

  async function runCityServiceCommand(
    commandType: CityServiceCommandType,
    payload: Record<string, unknown>,
    commandCode: string = commandType
  ): Promise<void> {
    const world = activeWorld.value
    if (
      !world || world.member_role !== 'owner' ||
      cityServiceAvailability.value !== 'available' || cityServiceCommandCode.value
    ) {
      throw new Error('City public-service command is unavailable')
    }
    cityServiceCommandCode.value = commandCode
    loadError.value = null
    try {
      const command = await citySpatialAPI.submitServiceCommand(
        world.id,
        commandType,
        payload,
        world.current_tick
      )
      const result = await citySpatialAPI.stepWorld(world.id, world.current_tick)
      const processed = result.commands.find(item => item.id === command.id)
      if (!processed || processed.status !== 'applied') {
        throw new Error(processed?.error_code ?? 'City public-service command was rejected')
      }
      updateWorldTick(result.tick.tick)
      await Promise.all([loadCityServices(true), loadCityPhysicalNetworks(true)])
    } catch (error: unknown) {
      loadError.value = readableError(error)
      throw error
    } finally {
      cityServiceCommandCode.value = null
    }
  }

  async function runWorldRuntimeCommand(
    commandType: CityRuntimeCommandType,
    payload: Record<string, unknown>,
    commandCode: string = commandType
  ): Promise<'applied' | 'queued'> {
    const world = activeWorld.value
    if (
      !world || worldRuntimeAvailability.value !== 'available' ||
      worldRuntimeCommandCode.value || worldLifecycleCommandCode.value
    ) {
      throw new Error('Open world runtime command is unavailable')
    }
    worldRuntimeCommandCode.value = commandCode
    clearNavigationPath()
    loadError.value = null
    try {
      const submission = isOpenWorld(world) && !isNativeOpenWorldRuntimeCommand(commandType)
        ? adaptOpenWorldRuntimeCommand(commandType, payload)
        : { commandType, payload }
      // An open-world clock can advance several times between a rendered
      // snapshot and a player interaction. Runtime commands are still
      // serialised and revalidated by the server at their processing tick, so
      // a stale UI tick must not prevent movement, portal traversal, or an
      // activity from ever entering that queue.
      const expectedWorldTick = isOpenWorld(world) && world.status === 'running'
        ? undefined
        : world.current_tick
      const command = await citySpatialAPI.submitWorldRuntimeCommand(
        world.id,
        submission.commandType,
        submission.payload,
        expectedWorldTick
      )
      upsertWorldCommandReceipt(command)
      if (world.member_role !== 'owner') {
        void pollWorldCommandReceipt(world.id, command.id, worldGeneration)
        return 'queued'
      }
      try {
        const result = await citySpatialAPI.stepWorld(world.id, world.current_tick)
        const processed = result.commands.find(item => item.id === command.id)
        if (processed) upsertWorldCommandReceipt(processed)
        if (processed?.status === 'applied') {
          updateWorldTick(result.tick.tick)
          await loadWorldRuntime(true)
          return 'applied'
        }
        if (processed?.status === 'rejected') {
          throw new Error(processed.error_code ?? 'Open world runtime command was rejected')
        }
      } catch (error: unknown) {
        // The server tick scheduler can seal the command between submission and
        // an owner-triggered step.  That is a successful concurrent outcome,
        // not a player-visible failure: reconcile the authoritative receipt
        // instead of leaving a stale expected tick in the client.
        if (!isExpectedWorldTickConflict(error)) throw error
      }

      await pollWorldCommandReceipt(world.id, command.id, worldGeneration)
      const settled = worldCommandReceipts.value.find(item => item.id === command.id)
      if (settled?.status === 'applied') return 'applied'
      if (settled?.status === 'rejected') {
        throw new Error(settled.error_code ?? 'Open world runtime command was rejected')
      }
      return 'queued'
    } catch (error: unknown) {
      loadError.value = readableError(error)
      throw error
    } finally {
      worldRuntimeCommandCode.value = null
    }
  }

  async function runWorldLifecycleCommand(
    commandType: CityWorldControlCommandType,
    payload: Record<string, unknown> = {},
    commandCode: string = commandType
  ): Promise<'applied' | 'queued'> {
    const world = activeWorld.value
    if (!world || worldLifecycleCommandCode.value || worldRuntimeCommandCode.value) {
      throw new Error('World lifecycle command is unavailable')
    }
    worldLifecycleCommandCode.value = commandCode
    loadError.value = null
    try {
      // Pausing or changing the speed of an already-running world follows
      // the same scheduler cadence as player actions. Let the server queue
      // the control command against its authoritative tick rather than
      // rejecting a control rendered a few seconds earlier.
      const expectedWorldTick = world.status === 'running' ? undefined : world.current_tick
      const command = await citySpatialAPI.submitWorldControlCommand(
        world.id,
        commandType,
        payload,
        expectedWorldTick
      )
      upsertWorldCommandReceipt(command)
      try {
        const result = await citySpatialAPI.stepWorld(world.id, world.current_tick)
        const processed = result.commands.find(item => item.id === command.id)
        if (processed) upsertWorldCommandReceipt(processed)
        if (processed?.status === 'applied') {
          updateWorldTick(result.tick.tick)
          applyWorldLifecycleResult(commandType, payload)
          try {
            const snapshot = await citySpatialAPI.getWorld(world.id)
            if (activeWorldID.value === world.id) replaceWorldSnapshot(snapshot)
          } catch {
            // The terminal command result remains enough to keep the local
            // lifecycle controls truthful until the next normal refresh.
          }
          await loadWorldRuntime(true)
          return 'applied'
        }
        if (processed?.status === 'rejected') {
          throw new Error(processed.error_code ?? 'World lifecycle command was rejected')
        }
      } catch (error: unknown) {
        // A running scheduler may seal the same command first. Reconcile its
        // terminal receipt instead of reporting a spurious tick conflict.
        if (!isExpectedWorldTickConflict(error)) throw error
      }

      await pollWorldCommandReceipt(world.id, command.id, worldGeneration)
      const settled = worldCommandReceipts.value.find(item => item.id === command.id)
      if (settled?.status === 'applied') {
        applyWorldLifecycleResult(commandType, payload)
        return 'applied'
      }
      if (settled?.status === 'rejected') {
        throw new Error(settled.error_code ?? 'World lifecycle command was rejected')
      }
      return 'queued'
    } catch (error: unknown) {
      loadError.value = readableError(error)
      throw error
    } finally {
      worldLifecycleCommandCode.value = null
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
    clearNavigationPath()
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
    clearNavigationPath()
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
    clearNavigationPath()
    selectedTile.value = tile
    selectedCoordinate.value = null
  }

  function openOvermapTile(tile = selectedTile.value): void {
    const currentProfile = profile.value
    if (!tile || !currentProfile) return
    clearNavigationPath()
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
    clearNavigationPath()
    mapMode.value = 'overmap'
    selectedCoordinate.value = null
    void loadLandLayer(0).catch(() => undefined)
  }

  function showLocalMap(): void {
    mapMode.value = 'local'
    if (!selectedCoordinate.value) {
      clearNavigationPath()
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
    clearNavigationPath()
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
    inFlightCommandReceipts.clear()
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
    cityServiceCatalog,
    cityServiceFacilities,
    cityServiceDemands,
    cityServiceConnections,
    cityServiceSettlements,
    cityServiceAvailability,
    cityPhysicalNetworkCatalog,
    cityPhysicalNetworks,
    cityPhysicalNetworkNodes,
    cityPhysicalNetworkEdges,
    cityPhysicalNetworkFlows,
    cityPhysicalNetworkFacts,
    cityPhysicalNetworkDiagnostics,
    cityPhysicalNetworkAvailability,
    worldRuntimeCatalog,
    worldActors,
    selectedActorCode,
    worldActorState,
    navigationPath,
    navigationLoading,
    navigationError,
    worldPortalStates,
    worldPortalAccessAvailability,
    worldNavigationIntents,
    worldNavigationReservations,
    worldNavigationIntentAvailability,
    worldNavigationIntentLoading,
    worldNavigationIntentError,
    worldActorRoleOptions,
    worldRuntimeRules,
    worldRuleCases,
    worldMembers,
    worldCommandReceipts,
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
    cityServiceLoading,
    cityPhysicalNetworkLoading,
    worldRuntimeLoading,
    worldPortalLoading,
    worldMembersLoading,
    creatingWorld,
    generatingChunkKey,
    developmentCommandCode,
    enterpriseLocationCommandCode,
    cityServiceCommandCode,
    worldRuntimeCommandCode,
    worldLifecycleCommandCode,
    worldMemberMutationKey,
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
    loadCityServices,
    queryCityServiceSection,
    loadCityPhysicalNetworks,
    queryCityPhysicalNetworkSection,
    queryCityPhysicalNetworkDiagnostics,
    loadWorldRuntime,
    loadWorldPortalStates,
    loadWorldNavigationState,
    loadWorldMembers,
    loadWorldCommandReceipts,
    loadWorldActor,
    selectWorldActor,
    focusWorldActor,
    previewWorldActorPath,
    clearNavigationPath,
    runDevelopmentCommand,
    runEnterpriseLocationCommand,
    runCityServiceCommand,
    runWorldRuntimeCommand,
    runWorldLifecycleCommand,
    addWorldMember,
    updateWorldMember,
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
