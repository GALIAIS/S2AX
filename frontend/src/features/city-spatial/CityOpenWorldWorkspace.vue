<template>
  <section class="open-world-workspace">
    <header class="open-world-command-bar">
      <div class="open-world-world-control">
        <label>{{ t('citySpatial.openWorld.world') }}</label>
        <Select
          :model-value="world.id"
          :options="worldOptions"
          :searchable="false"
          @update:model-value="selectWorld"
        />
      </div>

      <div class="open-world-binding" aria-live="polite">
        <span>{{ t('citySpatial.openWorld.profile') }}</span>
        <strong>{{ generation?.binding.profile_id ?? '—' }}</strong>
        <small>{{ t('citySpatial.openWorld.generator', { version: generation?.binding.generator_version ?? '—' }) }}</small>
      </div>

      <div class="open-world-coordinate">
        <span>XYZ</span>
        <strong>{{ coordinateReadout }}</strong>
      </div>

      <div class="open-world-actions">
        <button type="button" :disabled="camera.cellSize <= cellSizeSteps[0]" @click="changeZoom(-1)">−</button>
        <span>{{ camera.cellSize }}PX</span>
        <button type="button" :disabled="camera.cellSize >= cellSizeSteps[cellSizeSteps.length - 1]" @click="changeZoom(1)">＋</button>
        <button type="button" :disabled="loading || materializing" :title="t('citySpatial.openWorld.refresh')" @click="void loadWorld({ preserveCamera: true })">
          <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
        </button>
        <button
          v-if="isV2 && systemAdmin"
          type="button"
          :disabled="loading || materializing"
          :title="t('citySpatial.openWorld.materializeSector')"
          @click="void materializeCameraSector()"
        >
          <Icon name="plus" size="sm" :class="{ 'animate-spin': materializing }" />
        </button>
        <button
          v-if="isV2 && systemAdmin"
          type="button"
		  data-test="verify-open-world-region"
          :disabled="loading || materializing || verifying || !generation"
          :title="t('citySpatial.openWorld.verifyCurrentRegion')"
          @click="void verifyCameraRegion()"
        >
          <Icon name="shield" size="sm" />
        </button>
        <button type="button" :disabled="!generation" :title="t('citySpatial.openWorld.returnToSpawn')" @click="resetToSpawn">
          <Icon name="home" size="sm" />
        </button>
        <button type="button" :disabled="!nearestInteriorBuilding" :title="t('citySpatial.openWorld.nearestInterior')" @click="focusNearestInterior">
          <Icon name="grid" size="sm" />
        </button>
      </div>
    </header>

    <section class="open-world-lifecycle" :data-status="world.status">
      <div class="open-world-lifecycle-state" aria-live="polite">
        <span>{{ t('citySpatial.openWorld.lifecycle.title') }}</span>
        <strong>{{ lifecycleStatusLabel(world.status) }}</strong>
        <small>{{ t('citySpatial.openWorld.lifecycle.tick', {
          tick: world.current_tick,
          cadence: lifecycleCadence
        }) }}</small>
      </div>
      <p v-if="world.status !== 'running'" class="open-world-lifecycle-notice">
        {{ t('citySpatial.openWorld.lifecycle.pausedHint') }}
      </p>
      <div v-if="systemAdmin" class="open-world-lifecycle-controls">
        <label>
          <span>{{ t('citySpatial.openWorld.lifecycle.speed') }}</span>
          <Select
            :model-value="currentWorldSpeedMilli"
            :options="worldSpeedOptions"
            :searchable="false"
            :disabled="Boolean(lifecycleBusyCommandCode)"
            @update:model-value="setWorldSpeed"
          />
        </label>
        <button
          type="button"
          class="btn btn-primary btn-sm"
          data-test="open-world-lifecycle-toggle"
          :disabled="Boolean(lifecycleBusyCommandCode)"
          @click="toggleWorldLifecycle"
        >
          {{ lifecycleBusyCommandCode
            ? t('citySpatial.runtime.processing')
            : world.status === 'running'
              ? t('citySpatial.openWorld.lifecycle.pause')
              : t('citySpatial.openWorld.lifecycle.start') }}
        </button>
      </div>
    </section>

    <div v-if="error" class="open-world-error" role="alert">
      <Icon name="exclamationTriangle" size="sm" />
      <span>{{ error }}</span>
      <button type="button" @click="void loadWorld()">{{ t('common.retry') }}</button>
    </div>

    <div class="open-world-layout">
      <article class="open-world-map-panel">
        <header>
          <div>
            <span class="open-world-status" :data-status="world.status" />
            <strong>{{ t('citySpatial.openWorld.mapTitle') }}</strong>
            <small>{{ t('citySpatial.openWorld.mapSubtitle', { chunks: projectedChunks.size, buildings: materializedInteriorCount }) }}</small>
          </div>
          <span class="open-world-surface">Z 0</span>
        </header>

        <CityClassicViewport
          :scene="scene"
          :selected-coordinate="selectedCoordinate"
          :generated-chunk-keys="generatedChunkKeys"
          :glyph-characters="glyphCharacters"
          :busy="loading"
          :viewport-label="t('citySpatial.openWorld.viewportAria')"
          @resize="setViewport"
          @select-cell="selectCell"
          @hover-cell="hoveredCell = $event"
          @pan="panCamera"
          @zoom="changeZoom"
        />

        <footer>
          <span>{{ t('citySpatial.openWorld.dragHint') }}</span>
          <span><kbd>←</kbd><kbd>↑</kbd><kbd>↓</kbd><kbd>→</kbd> {{ t('citySpatial.openWorld.panHint') }}</span>
          <span><kbd>WHEEL</kbd> {{ t('citySpatial.openWorld.zoomHint') }}</span>
        </footer>
      </article>

      <aside class="open-world-inspector" aria-live="polite">
        <header>
          <span>{{ t('citySpatial.openWorld.inspector.eyebrow') }}</span>
          <h2>{{ t('citySpatial.openWorld.inspector.title') }}</h2>
        </header>

        <section v-if="verification" class="open-world-verification" :class="{ 'is-world-proof': verification.canonical_state_verified }">
          <Icon :name="verification.canonical_state_verified ? 'checkCircle' : 'shield'" size="sm" />
          <div>
            <strong>{{ verification.canonical_state_verified ? t('citySpatial.openWorld.worldVerified') : t('citySpatial.openWorld.regionVerified') }}</strong>
            <small>{{ t('citySpatial.openWorld.verificationSummary', { sectors: verification.sector_count, chunks: verification.chunk_count }) }}</small>
          </div>
        </section>

        <template v-if="selectedCell">
          <dl>
            <div>
              <dt>{{ t('citySpatial.openWorld.inspector.coordinate') }}</dt>
              <dd>{{ selectedCell.worldX }} / {{ selectedCell.worldY }} / {{ selectedCell.z }}</dd>
            </div>
            <div>
              <dt>{{ t('citySpatial.openWorld.inspector.chunk') }}</dt>
              <dd>{{ selectedCell.chunkX }} / {{ selectedCell.chunkY }}</dd>
            </div>
            <div>
              <dt>{{ t('citySpatial.openWorld.inspector.terrain') }}</dt>
              <dd><code>{{ selectedCell.terrainDefinitionID }}</code></dd>
            </div>
          </dl>

          <section class="open-world-stack">
            <h3>{{ t('citySpatial.openWorld.inspector.stack') }}</h3>
            <ol>
              <li v-for="layer in selectedCell.stack" :key="`${layer.kind}/${layer.definitionID}`">
                <span>{{ layer.glyph }}</span>
                <div>
                  <strong>{{ layer.name }}</strong>
                  <small>{{ layer.kind }} · {{ layer.definitionID }}</small>
                </div>
              </li>
            </ol>
          </section>

          <section v-if="selectedBuilding" class="open-world-building">
            <h3>{{ t('citySpatial.openWorld.inspector.building') }}</h3>
            <strong>{{ selectedBuilding.code }}</strong>
            <span>{{ selectedBuilding.archetype_code }} · {{ t('citySpatial.openWorld.inspector.floors', { count: selectedBuilding.floor_count }) }}</span>
            <small v-if="selectedBuilding.interior_floor_count > 0">
              {{ t('citySpatial.openWorld.inspector.interiorReady', { count: selectedBuilding.interior_floor_count }) }}
            </small>
            <small v-else>{{ t('citySpatial.openWorld.inspector.interiorUnavailable') }}</small>
            <div v-if="selectedBuilding.interior_floor_count > 0" class="open-world-floor-actions">
              <button
                v-for="floorIndex in interiorFloorIndexes(selectedBuilding)"
                :key="floorIndex"
                type="button"
                :disabled="!selectedBuilding.ground_interior_hash"
                @click="void openInterior(selectedBuilding, floorIndex)"
              >
                {{ t('citySpatial.openWorld.interior.openFloor', { floor: floorIndex + 1 }) }}
              </button>
            </div>
            <button
              type="button"
              :disabled="!selectedBuilding.ground_interior_hash"
              @click="focusBuilding(selectedBuilding)"
            >
              {{ t('citySpatial.openWorld.inspector.focusInterior') }}
            </button>
          </section>
        </template>

        <div v-else class="open-world-inspector-empty">
          <Icon name="grid" size="md" />
          <strong>{{ t('citySpatial.openWorld.inspector.emptyTitle') }}</strong>
          <p>{{ t('citySpatial.openWorld.inspector.emptyDescription') }}</p>
        </div>

        <footer v-if="generation">
          <span>{{ t('citySpatial.openWorld.inspector.seed') }}</span>
          <code>{{ generation.binding.seed }}</code>
          <span>{{ t('citySpatial.openWorld.inspector.hash') }}</span>
          <code :title="generation.binding.genesis_hash">{{ shortHash(generation.binding.genesis_hash) }}</code>
        </footer>
      </aside>
    </div>

    <BaseDialog
      :show="showInteriorDialog"
      :title="interiorDialogTitle"
      width="full"
      @close="closeInterior"
    >
      <section class="open-world-interior-dialog" aria-live="polite">
        <header class="open-world-interior-header">
          <div>
            <span>CLASSIC / INTERIOR FACTS</span>
            <strong v-if="interior">
              {{ t('citySpatial.openWorld.interior.floor', { floor: interior.floor_index, z: interior.z }) }}
            </strong>
            <strong v-else>{{ t('citySpatial.openWorld.interior.loading') }}</strong>
          </div>
          <div v-if="interior" class="open-world-interior-metadata">
            <span>{{ interior.layout_style }}</span>
            <span>{{ t('citySpatial.openWorld.interior.cells', { count: interior.cells.length }) }}</span>
            <code :title="interior.content_hash">{{ shortHash(interior.content_hash) }}</code>
          </div>
        </header>

        <div v-if="interiorLoading" class="open-world-interior-state">
          <Icon name="refresh" size="md" class="animate-spin" />
          <p>{{ t('citySpatial.openWorld.interior.loading') }}</p>
        </div>

        <div v-else-if="interiorError" class="open-world-interior-error" role="alert">
          <Icon name="exclamationTriangle" size="sm" />
          <span>{{ interiorError }}</span>
          <button type="button" @click="retryInterior">{{ t('common.retry') }}</button>
        </div>

        <div v-else-if="interior" class="open-world-interior-layout">
          <div class="open-world-interior-map">
            <CityClassicViewport
              class="open-world-interior-viewport"
              :scene="interiorScene"
              :selected-coordinate="interiorSelectedCoordinate"
              :glyph-characters="glyphCharacters"
              :viewport-label="t('citySpatial.openWorld.interior.viewportAria')"
              @resize="setInteriorViewport"
              @select-cell="selectInteriorCell"
              @pan="panInteriorCamera"
              @zoom="changeInteriorZoom"
            />
            <footer>
              <span>{{ t('citySpatial.openWorld.interior.dragHint') }}</span>
              <span><kbd>←</kbd><kbd>↑</kbd><kbd>↓</kbd><kbd>→</kbd> {{ t('citySpatial.openWorld.panHint') }}</span>
              <span><kbd>WHEEL</kbd> {{ t('citySpatial.openWorld.zoomHint') }}</span>
              <span>{{ interiorCamera.cellSize }}PX</span>
            </footer>
          </div>

          <aside class="open-world-interior-inspector">
            <header>
              <span>{{ t('citySpatial.openWorld.interior.serverFacts') }}</span>
              <strong>{{ interior.building_code }}</strong>
            </header>

            <template v-if="interiorSelectedCell">
              <dl>
                <div>
                  <dt>{{ t('citySpatial.openWorld.inspector.coordinate') }}</dt>
                  <dd>{{ interiorSelectedCell.worldX }} / {{ interiorSelectedCell.worldY }} / {{ interiorSelectedCell.z }}</dd>
                </div>
                <div>
                  <dt>{{ t('citySpatial.openWorld.inspector.terrain') }}</dt>
                  <dd><code>{{ interiorSelectedCell.terrainDefinitionID }}</code></dd>
                </div>
              </dl>
              <ol>
                <li v-for="layer in interiorSelectedCell.stack" :key="`${layer.kind}/${layer.definitionID}`">
                  <span>{{ layer.glyph }}</span>
                  <div>
                    <strong>{{ layer.name }}</strong>
                    <small>{{ layer.kind }} · {{ layer.definitionID }}</small>
                  </div>
                </li>
              </ol>

            </template>

            <div v-else class="open-world-interior-empty">
              <Icon name="grid" size="md" />
              <p>{{ t('citySpatial.openWorld.interior.selectCell') }}</p>
            </div>

            <section v-if="activeInteriorPortals.length" class="open-world-interior-portals">
              <span>{{ t('citySpatial.openWorld.interior.connections') }}</span>
              <button
                v-for="portal in activeInteriorPortals"
                :key="portal.code"
                type="button"
                @click="void followInteriorPortal(portal)"
              >
                <code>{{ portal.portal_type }}</code>
                <strong>{{ t('citySpatial.openWorld.interior.openFloor', { floor: interiorPortalTarget(portal).floor + 1 }) }}</strong>
              </button>
            </section>

            <footer>
              <span>{{ t('citySpatial.openWorld.inspector.hash') }}</span>
              <code :title="interior.content_hash">{{ shortHash(interior.content_hash) }}</code>
              <span>{{ t('citySpatial.openWorld.interior.layout') }}</span>
              <code>{{ interior.layout_version }}</code>
            </footer>
          </aside>
        </div>
      </section>
    </BaseDialog>

    <p v-if="hoveredCell" class="sr-only" aria-live="polite">
      {{ t('citySpatial.openWorld.hovered', { x: hoveredCell.worldX, y: hoveredCell.worldY, z: hoveredCell.z }) }}
    </p>
  </section>
</template>

<script setup lang="ts">
import { computed, ref, shallowRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type {
  CityOpenWorldBuilding,
  CityOpenWorldBuildingInterior,
  CityOpenWorldGenerationState,
  CityOpenWorldMap,
  CityOpenWorldPortal,
  CityOpenWorldVerification,
  CitySpatialRuleSet,
  CityWorld,
  CityWorldControlCommandType
} from '@/api/citySpatial'
import {
  getCitySpatialRuleSet,
  getOpenWorldBuildingInterior,
  getOpenWorldGeneration,
  getOpenWorldMap,
  getOpenWorldVerification,
  listOpenWorldBuildingPortals,
  stepCityWorld,
  submitOpenWorldSectorMaterialization
} from '@/api/citySpatial'
import Select, { type SelectOption } from '@/components/common/Select.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import CityClassicViewport from './CityClassicViewport.vue'
import {
  buildLocalScene,
  buildOpenWorldInteriorScene,
  projectOpenWorldChunk,
  type CameraState,
  type ClassicLocalScene,
  type ClassicScene,
  type ProjectedCityCell,
  type ProjectedCityChunk,
  type ViewportSize
} from './projection'

const props = withDefaults(defineProps<{
  world: CityWorld
  worlds: CityWorld[]
  systemAdmin?: boolean
  lifecycleBusyCommandCode?: string | null
}>(), {
  systemAdmin: false,
  lifecycleBusyCommandCode: null
})

const emit = defineEmits<{
  (event: 'select-world', worldID: number): void
  (event: 'world-control', commandType: CityWorldControlCommandType, payload: Record<string, unknown>, commandCode: string): void
}>()

const { t } = useI18n()
const cellSizeSteps = [8, 10, 12, 16, 20, 24, 32] as const
const viewport = ref<ViewportSize>({ width: 960, height: 560 })
const camera = ref<CameraState>({ worldX: 0, worldY: 0, z: 0, cellSize: cellSizeSteps[1] })
const generation = shallowRef<CityOpenWorldGenerationState | null>(null)
const mapData = shallowRef<CityOpenWorldMap | null>(null)
const ruleSet = shallowRef<CitySpatialRuleSet | null>(null)
const projectedChunks = shallowRef<ReadonlyMap<string, ProjectedCityChunk>>(new Map())
const selectedCoordinate = ref<{ worldX: number; worldY: number; z: number } | null>(null)
const hoveredCell = ref<ProjectedCityCell | null>(null)
const loading = ref(false)
const materializing = ref(false)
const verifying = ref(false)
const verification = shallowRef<CityOpenWorldVerification | null>(null)
const error = ref<string | null>(null)
const showInteriorDialog = ref(false)
const interior = shallowRef<CityOpenWorldBuildingInterior | null>(null)
const interiorPortals = shallowRef<CityOpenWorldPortal[]>([])
const interiorBuilding = shallowRef<CityOpenWorldBuilding | null>(null)
const interiorLoading = ref(false)
const interiorError = ref<string | null>(null)
const interiorViewport = ref<ViewportSize>({ width: 960, height: 560 })
const interiorCamera = ref<CameraState>({ worldX: 0, worldY: 0, z: 0, cellSize: 16 })
const interiorSelectedCoordinate = ref<{ worldX: number; worldY: number; z: number } | null>(null)
let requestVersion = 0
let interiorRequestVersion = 0
const knownWorldTick = ref(props.world.current_tick)

const worldOptions = computed<SelectOption[]>(() => props.worlds.map(world => ({
  value: world.id,
  label: world.name
})))
const currentWorldSpeedMilli = computed(() => Math.max(1, Math.round(props.world.speed_multiplier * 1000)))
const worldSpeedOptions = computed<SelectOption[]>(() => {
  const presetValues = [1_000, 60_000, 300_000, 1_000_000]
  const values = presetValues.includes(currentWorldSpeedMilli.value)
    ? presetValues
    : [currentWorldSpeedMilli.value, ...presetValues]
  return values.map(value => ({
    value,
    label: lifecycleSpeedLabel(value)
  }))
})
const glyphCharacters = computed(() => ruleSet.value?.definitions.map(item => item.glyph ?? '').join('') ?? '')
const isV2 = computed(() => (
  props.world.simulation_version === 'city-openworld-v2' ||
  props.world.simulation_version === 'city-openworld-v3'
))
const sectorWorldWidth = computed(() => {
  const sector = generation.value?.sectors[0]
  return sector ? sector.chunk_size * sector.sector_size_chunks : 256
})
const cameraSector = computed(() => ({
  x: Math.floor(camera.value.worldX / sectorWorldWidth.value),
  y: Math.floor(camera.value.worldY / sectorWorldWidth.value)
}))
const cameraRegion = computed(() => ({
  x: Math.floor(cameraSector.value.x / 4),
  y: Math.floor(cameraSector.value.y / 4)
}))
const generatedChunkKeys = computed(() => new Set(projectedChunks.value.keys()))
const materializedInteriorCount = computed(() => (
  mapData.value?.buildings.filter(building => building.interior_floor_count > 0).length ?? 0
))
const nearestInteriorBuilding = computed<CityOpenWorldBuilding | null>(() => {
  const binding = generation.value?.binding
  if (!binding) return null
  let nearest: CityOpenWorldBuilding | null = null
  let distance = Number.POSITIVE_INFINITY
  for (const building of mapData.value?.buildings ?? []) {
    if (!building.ground_interior_hash) continue
    const candidateDistance = Math.abs(building.entrance.x - binding.spawn_x) + Math.abs(building.entrance.y - binding.spawn_y)
    if (candidateDistance < distance || candidateDistance === distance && (nearest === null || building.code < nearest.code)) {
      nearest = building
      distance = candidateDistance
    }
  }
  return nearest
})
const mapBounds = computed(() => {
  const state = generation.value
  if (!state) return null
  const sector = state.sectors.find(item => (
    item.sector_x === state.binding.spawn_sector_x && item.sector_y === state.binding.spawn_sector_y
  )) ?? state.sectors[0]
  if (!sector) return null
  const width = sector.chunk_size * sector.sector_size_chunks
  const minX = sector.sector_x * width
  const minY = sector.sector_y * width
  return { minX, maxX: minX + width - 1, minY, maxY: minY + width - 1, z: 0 }
})
const scene = computed<ClassicScene>(() => {
  if (!ruleSet.value) {
    return {
      mode: 'local', width: viewport.value.width, height: viewport.value.height,
      cellSize: camera.value.cellSize, columns: 1, rows: 1,
      startWorldX: camera.value.worldX, startWorldY: camera.value.worldY, cells: [null]
    }
  }
  return buildLocalScene(projectedChunks.value, camera.value, viewport.value, ruleSet.value.chunk_size)
})
const interiorScene = computed<ClassicLocalScene>(() => {
  if (!interior.value || !ruleSet.value) {
    return {
      mode: 'local', width: interiorViewport.value.width, height: interiorViewport.value.height,
      cellSize: interiorCamera.value.cellSize, columns: 1, rows: 1,
      startWorldX: interiorCamera.value.worldX, startWorldY: interiorCamera.value.worldY, cells: [null]
    }
  }
  return buildOpenWorldInteriorScene(interior.value, ruleSet.value, interiorCamera.value, interiorViewport.value)
})
const interiorDialogTitle = computed(() => {
  const code = interior.value?.building_code ?? selectedBuilding.value?.code
  return code
    ? `${t('citySpatial.openWorld.interior.title')} / ${code}`
    : t('citySpatial.openWorld.interior.title')
})
const selectedCell = computed(() => {
  const selected = selectedCoordinate.value
  if (!selected || !ruleSet.value) return null
  const chunkSize = ruleSet.value.chunk_size
  const chunkX = Math.floor(selected.worldX / chunkSize)
  const chunkY = Math.floor(selected.worldY / chunkSize)
  const chunk = projectedChunks.value.get(`z:${selected.z}/x:${chunkX}/y:${chunkY}`)
  if (!chunk) return null
  const localX = selected.worldX - chunkX * chunkSize
  const localY = selected.worldY - chunkY * chunkSize
  return chunk.cells[localY * chunk.width + localX] ?? null
})
const interiorSelectedCell = computed(() => {
  const selected = interiorSelectedCoordinate.value
  if (!selected) return null
  return interiorScene.value.cells.find(cell => (
    cell?.worldX === selected.worldX && cell.worldY === selected.worldY && cell.z === selected.z
  )) ?? null
})
const selectedBuilding = computed<CityOpenWorldBuilding | null>(() => {
  const selected = selectedCoordinate.value
  if (!selected) return null
  return mapData.value?.buildings.find(building => building.footprint.some(point => (
    point.x === selected.worldX && point.y === selected.worldY && point.z === selected.z
  ))) ?? null
})
const activeInteriorPortals = computed(() => {
  if (!interior.value) return []
  return interiorPortals.value.filter(portal => (
    portal.portal_type === 'stairs' && (
      portal.from_floor_index === interior.value?.floor_index ||
      portal.to_floor_index === interior.value?.floor_index
    )
  ))
})
const coordinateReadout = computed(() => {
  const coordinate = selectedCoordinate.value ?? camera.value
  return `${coordinate.worldX} / ${coordinate.worldY} / ${coordinate.z}`
})

function readableError(reason: unknown): string {
  return reason instanceof Error ? reason.message : t('citySpatial.openWorld.loadFailed')
}

function shortHash(value: string): string {
  return value.length > 16 ? `${value.slice(0, 8)}…${value.slice(-6)}` : value
}

function selectWorld(value: string | number | boolean | null): void {
  const worldID = Number(value)
  if (Number.isSafeInteger(worldID) && worldID > 0 && worldID !== props.world.id) emit('select-world', worldID)
}

function lifecycleStatusLabel(status: string): string {
  return status === 'running'
    ? t('citySpatial.openWorld.lifecycle.status.running')
    : status === 'paused'
      ? t('citySpatial.openWorld.lifecycle.status.paused')
      : status
}

function lifecycleSpeedLabel(speedMilli: number): string {
  const multiplier = speedMilli / 1000
  const seconds = Math.ceil(3_600_000_000 / speedMilli) / 1000
  const multiplierText = Number.isInteger(multiplier) ? String(multiplier) : multiplier.toFixed(3)
  const cadence = seconds < 60
    ? `${seconds.toFixed(seconds < 10 ? 1 : 0)}s`
    : `${Math.ceil(seconds / 60)}m`
  return `${multiplierText}× · ${cadence}/TICK`
}

const lifecycleCadence = computed(() => lifecycleSpeedLabel(currentWorldSpeedMilli.value))

function setWorldSpeed(value: string | number | boolean | null): void {
  if (!props.systemAdmin || props.lifecycleBusyCommandCode) return
  const speedMilli = Number(value)
  if (!Number.isSafeInteger(speedMilli) || speedMilli < 1 || speedMilli > 1_000_000) return
  if (speedMilli === currentWorldSpeedMilli.value) return
  emit('world-control', 'world.set_speed', { speed_milli: speedMilli }, `world:set-speed:${speedMilli}`)
}

function toggleWorldLifecycle(): void {
  if (!props.systemAdmin || props.lifecycleBusyCommandCode) return
  const commandType: CityWorldControlCommandType = props.world.status === 'running'
    ? 'world.pause'
    : 'world.resume'
  emit('world-control', commandType, {}, commandType)
}

function clampCamera(next: CameraState): CameraState {
	if (isV2.value) {
		const maximumCoordinate = 1_000_000 * sectorWorldWidth.value
		return {
			...next,
			worldX: Math.max(-maximumCoordinate, Math.min(maximumCoordinate, Math.trunc(next.worldX))),
			worldY: Math.max(-maximumCoordinate, Math.min(maximumCoordinate, Math.trunc(next.worldY))),
			z: 0
		}
	}
  const bounds = mapBounds.value
  if (!bounds) return next
  return {
    ...next,
    worldX: Math.max(bounds.minX, Math.min(bounds.maxX, Math.trunc(next.worldX))),
    worldY: Math.max(bounds.minY, Math.min(bounds.maxY, Math.trunc(next.worldY))),
    z: 0
  }
}

function resetToSpawn(): void {
  const binding = generation.value?.binding
  if (!binding) return
  camera.value = clampCamera({ ...camera.value, worldX: binding.spawn_x, worldY: binding.spawn_y, z: binding.spawn_z })
  selectedCoordinate.value = { worldX: binding.spawn_x, worldY: binding.spawn_y, z: binding.spawn_z }
}

function panCamera(delta: { x: number; y: number }): void {
  camera.value = clampCamera({
    ...camera.value,
    worldX: camera.value.worldX + Math.trunc(delta.x),
    worldY: camera.value.worldY + Math.trunc(delta.y)
  })
}

function changeZoom(direction: number): void {
  const current = cellSizeSteps.indexOf(camera.value.cellSize as typeof cellSizeSteps[number])
  const next = Math.max(0, Math.min(cellSizeSteps.length - 1, current + Math.sign(direction)))
  camera.value = { ...camera.value, cellSize: cellSizeSteps[next] }
}

function setViewport(value: ViewportSize): void {
  viewport.value = { width: value.width, height: value.height }
}

function setInteriorViewport(value: ViewportSize): void {
  interiorViewport.value = { width: value.width, height: value.height }
}

function selectCell(cell: ProjectedCityCell): void {
  selectedCoordinate.value = { worldX: cell.worldX, worldY: cell.worldY, z: cell.z }
}

function focusBuilding(building: CityOpenWorldBuilding): void {
  const focusedSize = cellSizeSteps.find(size => size >= 16) ?? camera.value.cellSize
  camera.value = clampCamera({
    ...camera.value,
    worldX: building.entrance.x,
    worldY: building.entrance.y,
    z: building.entrance.z,
    cellSize: Math.max(camera.value.cellSize, focusedSize)
  })
  selectedCoordinate.value = {
    worldX: building.entrance.x,
    worldY: building.entrance.y,
    z: building.entrance.z
  }
}

function focusNearestInterior(): void {
  if (nearestInteriorBuilding.value) focusBuilding(nearestInteriorBuilding.value)
}

function interiorBounds(value: CityOpenWorldBuildingInterior): { minX: number; maxX: number; minY: number; maxY: number; z: number } {
  const first = value.cells[0]
  if (!first) throw new Error('Open-world interior has no materialized cells')
  let minX = first.x
  let maxX = first.x
  let minY = first.y
  let maxY = first.y
  for (const cell of value.cells.slice(1)) {
    minX = Math.min(minX, cell.x)
    maxX = Math.max(maxX, cell.x)
    minY = Math.min(minY, cell.y)
    maxY = Math.max(maxY, cell.y)
  }
  return { minX, maxX, minY, maxY, z: value.z }
}

function clampInteriorCamera(next: CameraState): CameraState {
  if (!interior.value) return next
  const bounds = interiorBounds(interior.value)
  return {
    ...next,
    worldX: Math.max(bounds.minX, Math.min(bounds.maxX, Math.trunc(next.worldX))),
    worldY: Math.max(bounds.minY, Math.min(bounds.maxY, Math.trunc(next.worldY))),
    z: bounds.z
  }
}

function focusInteriorCamera(value: CityOpenWorldBuildingInterior, coordinate?: { x: number; y: number; z: number }): void {
  const bounds = interiorBounds(value)
  const centerX = coordinate?.x ?? Math.floor((bounds.minX + bounds.maxX) / 2)
  const centerY = coordinate?.y ?? Math.floor((bounds.minY + bounds.maxY) / 2)
  interiorCamera.value = {
    worldX: Math.max(bounds.minX, Math.min(bounds.maxX, centerX)),
    worldY: Math.max(bounds.minY, Math.min(bounds.maxY, centerY)),
    z: bounds.z,
    cellSize: 16
  }
}

function interiorFloorIndexes(building: CityOpenWorldBuilding): number[] {
  return Array.from({ length: Math.min(building.floor_count, building.interior_floor_count) }, (_, index) => index)
}

function interiorPortalTarget(portal: CityOpenWorldPortal): { floor: number; coordinate: { x: number; y: number; z: number } } {
  const currentFloor = interior.value?.floor_index
  if (currentFloor === portal.to_floor_index && portal.bidirectional) {
    return { floor: portal.from_floor_index, coordinate: portal.from }
  }
  return { floor: portal.to_floor_index, coordinate: portal.to }
}

async function followInteriorPortal(portal: CityOpenWorldPortal): Promise<void> {
  const building = interiorBuilding.value
  if (!building) return
  const target = interiorPortalTarget(portal)
  await openInterior(building, target.floor, target.coordinate)
}

async function openInterior(
  building: CityOpenWorldBuilding,
  floorIndex = 0,
  focus?: { x: number; y: number; z: number }
): Promise<void> {
  if (!building.ground_interior_hash) return
  const worldID = props.world.id
  const version = ++interiorRequestVersion
  showInteriorDialog.value = true
  interior.value = null
  interiorPortals.value = []
  interiorBuilding.value = building
  interiorError.value = null
  interiorLoading.value = true
  interiorSelectedCoordinate.value = null
  try {
    const [next, portals] = await Promise.all([
      getOpenWorldBuildingInterior(worldID, building.code, floorIndex),
      listOpenWorldBuildingPortals(worldID, building.code)
    ])
    if (version !== interiorRequestVersion || props.world.id !== worldID) return
    if (next.building_code !== building.code || next.floor_index !== floorIndex ||
      (floorIndex === 0 && next.content_hash !== building.ground_interior_hash)) {
      throw new Error(t('citySpatial.openWorld.interior.verificationFailed'))
    }
    interior.value = next
    interiorPortals.value = portals
    const initialFocus = focus ?? (floorIndex === 0 ? building.entrance : undefined)
    focusInteriorCamera(next, initialFocus)
    if (initialFocus) {
      interiorSelectedCoordinate.value = { worldX: initialFocus.x, worldY: initialFocus.y, z: next.z }
    }
  } catch (reason: unknown) {
    if (version === interiorRequestVersion && props.world.id === worldID) {
      interiorError.value = reason instanceof Error ? reason.message : t('citySpatial.openWorld.interior.loadFailed')
    }
  } finally {
    if (version === interiorRequestVersion) interiorLoading.value = false
  }
}

function closeInterior(): void {
  interiorRequestVersion += 1
  showInteriorDialog.value = false
  interior.value = null
  interiorPortals.value = []
  interiorBuilding.value = null
  interiorLoading.value = false
  interiorError.value = null
  interiorSelectedCoordinate.value = null
}

function retryInterior(): void {
  const building = interiorBuilding.value ?? selectedBuilding.value
  if (building) void openInterior(building, interior.value?.floor_index ?? 0)
}

function selectInteriorCell(cell: ProjectedCityCell): void {
  interiorSelectedCoordinate.value = { worldX: cell.worldX, worldY: cell.worldY, z: cell.z }
}

function panInteriorCamera(delta: { x: number; y: number }): void {
  interiorCamera.value = clampInteriorCamera({
    ...interiorCamera.value,
    worldX: interiorCamera.value.worldX + Math.trunc(delta.x),
    worldY: interiorCamera.value.worldY + Math.trunc(delta.y)
  })
}

function changeInteriorZoom(direction: number): void {
  const current = cellSizeSteps.indexOf(interiorCamera.value.cellSize as typeof cellSizeSteps[number])
  const next = Math.max(0, Math.min(cellSizeSteps.length - 1, current + Math.sign(direction)))
  interiorCamera.value = { ...interiorCamera.value, cellSize: cellSizeSteps[next] }
}

type OpenWorldLoadOptions = {
  preserveCamera?: boolean
  sector?: { x: number; y: number }
}

function materializedSector(
  state: CityOpenWorldGenerationState,
  preferred: { x: number; y: number }
) {
  return state.sectors.find(item => item.sector_x === preferred.x && item.sector_y === preferred.y)
    ?? state.sectors.find(item => (
      item.sector_x === state.binding.spawn_sector_x && item.sector_y === state.binding.spawn_sector_y
    ))
    ?? state.sectors[0]
}

async function loadWorld(options: OpenWorldLoadOptions = {}): Promise<void> {
  const worldID = props.world.id
  const version = ++requestVersion
  loading.value = true
  error.value = null
  try {
    const nextGeneration = await getOpenWorldGeneration(worldID)
    const preferredSector = options.sector ?? (options.preserveCamera
      ? cameraSector.value
      : { x: nextGeneration.binding.spawn_sector_x, y: nextGeneration.binding.spawn_sector_y })
    const sector = materializedSector(nextGeneration, preferredSector)
    if (!sector) throw new Error('Open-world generation has no materialized sector')
    const [nextRuleSet, nextMap] = await Promise.all([
      getCitySpatialRuleSet(nextGeneration.binding.rule_set_id),
      (() => {
        const width = sector.chunk_size * sector.sector_size_chunks
        const minX = sector.sector_x * width
        const minY = sector.sector_y * width
        return getOpenWorldMap(worldID, { min_x: minX, max_x: minX + width - 1, min_y: minY, max_y: minY + width - 1, z: 0 })
      })()
    ])
    if (version !== requestVersion || props.world.id !== worldID) return
    if (
      nextRuleSet.id !== nextGeneration.binding.rule_set_id ||
      nextRuleSet.version !== nextGeneration.binding.rule_set_version ||
      nextRuleSet.content_hash !== nextGeneration.binding.rule_set_hash ||
      nextMap.binding.genesis_hash !== nextGeneration.binding.genesis_hash
    ) {
      throw new Error('Open-world binding verification failed')
    }
    const chunks = new Map<string, ProjectedCityChunk>()
    for (const chunk of nextMap.chunks) {
      const projected = projectOpenWorldChunk(chunk, nextRuleSet)
      chunks.set(projected.key, projected)
    }
    generation.value = nextGeneration
    ruleSet.value = nextRuleSet
    mapData.value = nextMap
    projectedChunks.value = chunks
    knownWorldTick.value = Math.max(knownWorldTick.value, props.world.current_tick)
    if (options.preserveCamera) {
      camera.value = clampCamera(camera.value)
    } else {
      resetToSpawn()
    }
  } catch (reason: unknown) {
    if (version === requestVersion && props.world.id === worldID) error.value = readableError(reason)
  } finally {
    if (version === requestVersion) loading.value = false
  }
}

async function materializeCameraSector(): Promise<void> {
  if (!props.systemAdmin || !isV2.value || materializing.value) return
  const target = cameraSector.value
  const worldID = props.world.id
  if (generation.value?.sectors.some(item => item.sector_x === target.x && item.sector_y === target.y)) {
    await loadWorld({ preserveCamera: true, sector: target })
    return
  }
  materializing.value = true
  error.value = null
  try {
    const command = await submitOpenWorldSectorMaterialization(worldID, target.x, target.y, knownWorldTick.value)
    const step = await stepCityWorld(worldID, knownWorldTick.value)
    if (props.world.id !== worldID) return
    const processed = step.commands.find(item => item.id === command.id)
    if (!processed || processed.status !== 'applied') {
      throw new Error(t('citySpatial.openWorld.materializeFailed'))
    }
    knownWorldTick.value = step.tick.tick
    await loadWorld({ preserveCamera: true, sector: target })
  } catch (reason: unknown) {
    if (props.world.id === worldID) error.value = readableError(reason)
  } finally {
    if (props.world.id === worldID) materializing.value = false
  }
}

async function verifyCameraRegion(): Promise<void> {
  if (!props.systemAdmin || !isV2.value || verifying.value || !generation.value) return
  const worldID = props.world.id
  const region = cameraRegion.value
  verifying.value = true
  error.value = null
  try {
    const next = await getOpenWorldVerification(worldID, { region_x: region.x, region_y: region.y })
    if (props.world.id !== worldID || next.scope !== 'region' || next.region_x !== region.x || next.region_y !== region.y) {
      throw new Error(t('citySpatial.openWorld.verificationFailed'))
    }
    verification.value = next
  } catch (reason: unknown) {
    if (props.world.id === worldID) error.value = readableError(reason)
  } finally {
    if (props.world.id === worldID) verifying.value = false
  }
}

watch(() => props.world.id, () => {
  closeInterior()
  generation.value = null
  mapData.value = null
  ruleSet.value = null
  projectedChunks.value = new Map()
  selectedCoordinate.value = null
  hoveredCell.value = null
  knownWorldTick.value = props.world.current_tick
  materializing.value = false
  verifying.value = false
  verification.value = null
  void loadWorld()
}, { immediate: true })

watch(() => props.world.current_tick, tick => {
  // A parent-side scheduler refresh can advance the world while this
  // workspace stays mounted. Keep the command precondition monotonic so a
  // sector request never submits an already-stale expected tick.
  knownWorldTick.value = Math.max(knownWorldTick.value, tick)
})
</script>

<style scoped>
.open-world-workspace { display: grid; gap: 0; border: 1px solid var(--ui-separator); background: var(--ui-surface); }

.open-world-command-bar { display: grid; grid-template-columns: minmax(14rem, 1.1fr) minmax(12rem, 0.8fr) minmax(10rem, 0.65fr) auto; gap: 0.75rem; align-items: end; padding: 0.85rem; border-bottom: 1px solid var(--ui-separator); }
.open-world-world-control label, .open-world-binding > span, .open-world-coordinate > span { display: block; margin-bottom: 0.35rem; color: var(--ui-label-secondary); font: 0.62rem ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; letter-spacing: 0.11em; text-transform: uppercase; }
.open-world-binding, .open-world-coordinate { min-height: 2.75rem; padding: 0.4rem 0.65rem; border: 1px solid var(--ui-separator); background: var(--ui-canvas-raised); }
.open-world-binding strong, .open-world-coordinate strong { display: block; overflow: hidden; color: var(--ui-label); font: 0.75rem ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; text-overflow: ellipsis; white-space: nowrap; }
.open-world-binding small { display: block; margin-top: 0.14rem; color: var(--ui-label-secondary); font-size: 0.62rem; }
.open-world-actions { display: flex; height: 2.75rem; border: 1px solid var(--ui-separator); }
.open-world-actions button { display: grid; width: 2.75rem; place-items: center; border-right: 1px solid var(--ui-separator); color: var(--ui-label-secondary); background: var(--ui-control); }
.open-world-actions button:last-child { border-right: 0; }
.open-world-actions button:hover:not(:disabled) { color: var(--ui-label); background: var(--ui-control-hover); }
.open-world-actions button:disabled { cursor: not-allowed; opacity: 0.35; }
.open-world-actions span { display: grid; min-width: 3.4rem; place-items: center; border-right: 1px solid var(--ui-separator); font: 0.65rem ui-monospace, monospace; }

.open-world-lifecycle { display: grid; grid-template-columns: minmax(9rem, auto) minmax(0, 1fr) auto; gap: 0.75rem; align-items: center; min-height: 4.25rem; padding: 0.65rem 0.85rem; border-bottom: 1px solid var(--ui-separator); background: var(--ui-canvas-raised); }
.open-world-lifecycle-state { display: grid; grid-template-columns: auto auto; column-gap: 0.5rem; align-items: baseline; }
.open-world-lifecycle-state > span, .open-world-lifecycle-state small, .open-world-lifecycle-controls label > span { color: var(--ui-label-secondary); font: 0.62rem ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; letter-spacing: 0.08em; text-transform: uppercase; }
.open-world-lifecycle-state strong { color: var(--ui-label); font-size: 0.78rem; }
.open-world-lifecycle-state small { grid-column: 1 / -1; margin-top: 0.18rem; letter-spacing: 0.03em; }
.open-world-lifecycle[data-status='running'] .open-world-lifecycle-state strong { color: #31d17c; }
.open-world-lifecycle-notice { min-width: 0; margin: 0; color: var(--ui-label-secondary); font-size: 0.72rem; line-height: 1.45; }
.open-world-lifecycle-controls { display: flex; align-items: end; gap: 0.55rem; }
.open-world-lifecycle-controls label { display: grid; gap: 0.3rem; min-width: 11.5rem; }
.open-world-lifecycle-controls .btn { min-height: 2.35rem; white-space: nowrap; }

.open-world-error { display: flex; align-items: center; gap: 0.65rem; margin: 0.8rem; border: 1px solid rgb(239 68 68 / 42%); border-left-width: 3px; padding: 0.7rem 0.85rem; color: #dc2626; background: rgb(239 68 68 / 6%); font-size: 0.8rem; }
.open-world-error span { min-width: 0; flex: 1; overflow-wrap: anywhere; }
.open-world-error button { color: inherit; font-weight: 700; text-decoration: underline; }

.open-world-layout { display: grid; grid-template-columns: minmax(0, 1fr) minmax(17rem, 21rem); min-height: 31rem; }
.open-world-map-panel { display: grid; min-width: 0; grid-template-rows: auto minmax(31rem, 1fr) auto; border-right: 1px solid var(--ui-separator); }
.open-world-map-panel > header { display: flex; align-items: center; justify-content: space-between; gap: 1rem; min-height: 3.6rem; padding: 0.65rem 0.85rem; border-bottom: 1px solid var(--ui-separator); }
.open-world-map-panel header > div { display: grid; grid-template-columns: auto 1fr; column-gap: 0.5rem; align-items: center; }
.open-world-map-panel header strong { color: var(--ui-label); font-size: 0.85rem; }
.open-world-map-panel header small { grid-column: 2; margin-top: 0.12rem; color: var(--ui-label-secondary); font-size: 0.68rem; }
.open-world-status { width: 0.45rem; height: 0.45rem; background: #31d17c; box-shadow: 0 0 0 2px rgb(49 209 124 / 14%); }
.open-world-surface { border: 1px solid var(--ui-separator); padding: 0.22rem 0.38rem; color: var(--ui-label-secondary); background: var(--ui-canvas-raised); font: 0.62rem ui-monospace, monospace; }
.open-world-map-panel > footer { display: flex; flex-wrap: wrap; gap: 0.8rem; min-height: 2.35rem; align-items: center; padding: 0.5rem 0.85rem; border-top: 1px solid var(--ui-separator); color: var(--ui-label-secondary); font-size: 0.68rem; }
.open-world-map-panel kbd { border: 1px solid var(--ui-separator); padding: 0.08rem 0.22rem; color: var(--ui-label); background: var(--ui-canvas-raised); font: inherit; }

.open-world-inspector { display: flex; min-width: 0; flex-direction: column; background: var(--ui-surface); }
.open-world-inspector > header { padding: 1rem 1rem 0.75rem; border-bottom: 1px solid var(--ui-separator); }
.open-world-inspector > header span { display: block; color: var(--ui-label-secondary); font: 0.62rem ui-monospace, monospace; letter-spacing: 0.12em; text-transform: uppercase; }
.open-world-inspector h2 { margin: 0.25rem 0 0; color: var(--ui-label); font-size: 1rem; }
.open-world-verification { display: grid; grid-template-columns: auto minmax(0, 1fr); gap: 0.55rem; align-items: center; padding: 0.7rem 1rem; border-bottom: 1px solid var(--ui-separator); color: var(--ui-label-secondary); background: var(--ui-canvas-raised); }
.open-world-verification svg { color: var(--ui-accent); }
.open-world-verification.is-world-proof svg { color: #31d17c; }
.open-world-verification strong, .open-world-verification small { display: block; }
.open-world-verification strong { color: var(--ui-label); font-size: 0.7rem; }
.open-world-verification small { margin-top: 0.12rem; color: var(--ui-label-secondary); font: 0.61rem ui-monospace, monospace; }
.open-world-inspector dl { display: grid; margin: 0; border-bottom: 1px solid var(--ui-separator); }
.open-world-inspector dl > div { display: grid; grid-template-columns: 6.6rem minmax(0, 1fr); gap: 0.5rem; padding: 0.65rem 1rem; border-bottom: 1px solid var(--ui-separator); }
.open-world-inspector dl > div:last-child { border-bottom: 0; }
.open-world-inspector dt { color: var(--ui-label-secondary); font-size: 0.7rem; }
.open-world-inspector dd { min-width: 0; margin: 0; overflow: hidden; color: var(--ui-label); font: 0.7rem ui-monospace, monospace; text-overflow: ellipsis; white-space: nowrap; }
.open-world-inspector code { font: inherit; }
.open-world-stack, .open-world-building { padding: 0.85rem 1rem; border-bottom: 1px solid var(--ui-separator); }
.open-world-stack h3, .open-world-building h3 { margin: 0 0 0.65rem; color: var(--ui-label-secondary); font-size: 0.68rem; letter-spacing: 0.08em; text-transform: uppercase; }
.open-world-stack ol { display: grid; gap: 0.4rem; margin: 0; padding: 0; list-style: none; }
.open-world-stack li { display: grid; grid-template-columns: 1.8rem minmax(0, 1fr); gap: 0.5rem; align-items: center; padding: 0.45rem; border: 1px solid var(--ui-separator); background: var(--ui-canvas-raised); }
.open-world-stack li > span { color: var(--ui-accent); font: 1rem ui-monospace, monospace; text-align: center; }
.open-world-stack li strong, .open-world-stack li small { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.open-world-stack li strong { color: var(--ui-label); font-size: 0.72rem; }
.open-world-stack li small, .open-world-building small { margin-top: 0.1rem; color: var(--ui-label-secondary); font: 0.62rem ui-monospace, monospace; }
.open-world-building { display: grid; gap: 0.3rem; }
.open-world-building strong { color: var(--ui-label); font: 0.75rem ui-monospace, monospace; }
.open-world-building span { color: var(--ui-label-secondary); font-size: 0.7rem; }
.open-world-building button { justify-self: start; margin-top: 0.2rem; border: 1px solid var(--ui-separator); padding: 0.36rem 0.52rem; color: var(--ui-label); background: var(--ui-control); font-size: 0.68rem; }
.open-world-building button:hover:not(:disabled) { background: var(--ui-control-hover); }
.open-world-building button:disabled { cursor: not-allowed; opacity: 0.42; }
.open-world-floor-actions { display: flex; flex-wrap: wrap; gap: 0.35rem; }
.open-world-floor-actions button { margin-top: 0; }
.open-world-inspector-empty { display: grid; flex: 1; place-content: center; justify-items: center; gap: 0.5rem; padding: 2rem; color: var(--ui-label-secondary); text-align: center; }
.open-world-inspector-empty strong { color: var(--ui-label); font-size: 0.84rem; }
.open-world-inspector-empty p { margin: 0; font-size: 0.72rem; line-height: 1.6; }
.open-world-inspector > footer { display: grid; grid-template-columns: auto minmax(0, 1fr); gap: 0.25rem 0.6rem; padding: 0.8rem 1rem; border-top: 1px solid var(--ui-separator); color: var(--ui-label-secondary); font-size: 0.65rem; }
.open-world-inspector > footer code { min-width: 0; overflow: hidden; color: var(--ui-label); font: 0.65rem ui-monospace, monospace; text-overflow: ellipsis; white-space: nowrap; }

.open-world-interior-dialog { display: grid; gap: 0; min-height: 34rem; border: 1px solid var(--ui-separator); background: var(--ui-surface); }
.open-world-interior-header { display: flex; align-items: center; justify-content: space-between; gap: 1rem; min-height: 3.5rem; padding: 0.7rem 0.85rem; border-bottom: 1px solid var(--ui-separator); }
.open-world-interior-header > div:first-child { display: grid; gap: 0.18rem; }
.open-world-interior-header > div:first-child > span, .open-world-interior-inspector header > span { color: var(--ui-label-secondary); font: 0.61rem ui-monospace, monospace; letter-spacing: 0.11em; text-transform: uppercase; }
.open-world-interior-header strong { color: var(--ui-label); font: 0.82rem ui-monospace, monospace; }
.open-world-interior-metadata { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 0.35rem; }
.open-world-interior-metadata span, .open-world-interior-metadata code { border: 1px solid var(--ui-separator); padding: 0.22rem 0.38rem; color: var(--ui-label-secondary); background: var(--ui-canvas-raised); font: 0.62rem ui-monospace, monospace; }
.open-world-interior-state { display: grid; min-height: 30rem; place-content: center; justify-items: center; gap: 0.65rem; color: var(--ui-label-secondary); font-size: 0.75rem; }
.open-world-interior-state p { margin: 0; }
.open-world-interior-error { display: flex; align-items: center; gap: 0.65rem; margin: 0.85rem; border: 1px solid rgb(239 68 68 / 42%); border-left-width: 3px; padding: 0.7rem 0.85rem; color: #dc2626; background: rgb(239 68 68 / 6%); font-size: 0.76rem; }
.open-world-interior-error span { min-width: 0; flex: 1; overflow-wrap: anywhere; }
.open-world-interior-error button { color: inherit; font-weight: 700; text-decoration: underline; }
.open-world-interior-layout { display: grid; grid-template-columns: minmax(0, 1fr) minmax(16rem, 20rem); min-height: 33rem; }
.open-world-interior-map { display: grid; min-width: 0; grid-template-rows: minmax(31rem, 1fr) auto; border-right: 1px solid var(--ui-separator); }
.open-world-interior-viewport { min-height: 31rem; }
.open-world-interior-map > footer { display: flex; flex-wrap: wrap; gap: 0.8rem; align-items: center; min-height: 2.3rem; padding: 0.48rem 0.75rem; border-top: 1px solid var(--ui-separator); color: var(--ui-label-secondary); font-size: 0.65rem; }
.open-world-interior-map kbd { border: 1px solid var(--ui-separator); padding: 0.08rem 0.22rem; color: var(--ui-label); background: var(--ui-canvas-raised); font: inherit; }
.open-world-interior-map > footer > span:last-child { margin-left: auto; color: var(--ui-label); font: 0.65rem ui-monospace, monospace; }
.open-world-interior-inspector { display: flex; min-width: 0; flex-direction: column; background: var(--ui-surface); }
.open-world-interior-inspector > header { display: grid; gap: 0.35rem; padding: 0.85rem 0.9rem; border-bottom: 1px solid var(--ui-separator); }
.open-world-interior-inspector > header strong { overflow: hidden; color: var(--ui-label); font: 0.72rem ui-monospace, monospace; text-overflow: ellipsis; white-space: nowrap; }
.open-world-interior-inspector dl { display: grid; margin: 0; border-bottom: 1px solid var(--ui-separator); }
.open-world-interior-inspector dl > div { display: grid; grid-template-columns: 5.8rem minmax(0, 1fr); gap: 0.5rem; padding: 0.62rem 0.9rem; border-bottom: 1px solid var(--ui-separator); }
.open-world-interior-inspector dl > div:last-child { border-bottom: 0; }
.open-world-interior-inspector dt { color: var(--ui-label-secondary); font-size: 0.67rem; }
.open-world-interior-inspector dd { min-width: 0; margin: 0; overflow: hidden; color: var(--ui-label); font: 0.68rem ui-monospace, monospace; text-overflow: ellipsis; white-space: nowrap; }
.open-world-interior-inspector ol { display: grid; gap: 0.42rem; margin: 0; padding: 0.75rem 0.9rem; list-style: none; }
.open-world-interior-inspector li { display: grid; grid-template-columns: 1.7rem minmax(0, 1fr); gap: 0.5rem; align-items: center; padding: 0.42rem; border: 1px solid var(--ui-separator); background: var(--ui-canvas-raised); }
.open-world-interior-inspector li > span { color: var(--ui-accent); font: 0.94rem ui-monospace, monospace; text-align: center; }
.open-world-interior-inspector li strong, .open-world-interior-inspector li small { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.open-world-interior-inspector li strong { color: var(--ui-label); font-size: 0.7rem; }
.open-world-interior-inspector li small { color: var(--ui-label-secondary); font: 0.6rem ui-monospace, monospace; }
.open-world-interior-portals { display: grid; gap: 0.38rem; padding: 0 0.9rem 0.8rem; }
.open-world-interior-portals > span { color: var(--ui-label-secondary); font: 0.61rem ui-monospace, monospace; letter-spacing: 0.1em; text-transform: uppercase; }
.open-world-interior-portals button { display: flex; min-width: 0; align-items: center; justify-content: space-between; gap: 0.5rem; border: 1px solid var(--ui-separator); padding: 0.4rem 0.48rem; color: var(--ui-label); background: var(--ui-canvas-raised); text-align: left; }
.open-world-interior-portals button:hover { background: var(--ui-control-hover); }
.open-world-interior-portals code { color: var(--ui-label-secondary); font: 0.6rem ui-monospace, monospace; }
.open-world-interior-portals strong { overflow: hidden; font-size: 0.65rem; text-overflow: ellipsis; white-space: nowrap; }
.open-world-interior-empty { display: grid; flex: 1; place-content: center; justify-items: center; gap: 0.55rem; min-height: 14rem; padding: 1.5rem; color: var(--ui-label-secondary); text-align: center; }
.open-world-interior-empty p { margin: 0; font-size: 0.7rem; line-height: 1.6; }
.open-world-interior-inspector > footer { display: grid; grid-template-columns: auto minmax(0, 1fr); gap: 0.3rem 0.55rem; margin-top: auto; padding: 0.75rem 0.9rem; border-top: 1px solid var(--ui-separator); color: var(--ui-label-secondary); font-size: 0.62rem; }
.open-world-interior-inspector > footer code { min-width: 0; overflow: hidden; color: var(--ui-label); font: 0.62rem ui-monospace, monospace; text-overflow: ellipsis; white-space: nowrap; }

@media (max-width: 60rem) {
  .open-world-command-bar { grid-template-columns: minmax(12rem, 1fr) minmax(10rem, 1fr) auto; }
  .open-world-lifecycle { grid-template-columns: minmax(9rem, auto) minmax(0, 1fr); }
  .open-world-lifecycle-controls { grid-column: 1 / -1; justify-content: flex-end; }
  .open-world-coordinate { display: none; }
  .open-world-layout { grid-template-columns: 1fr; }
  .open-world-map-panel { border-right: 0; }
  .open-world-inspector { border-top: 1px solid var(--ui-separator); }
  .open-world-inspector-empty { min-height: 13rem; }
  .open-world-interior-layout { grid-template-columns: 1fr; }
  .open-world-interior-map { border-right: 0; }
  .open-world-interior-inspector { border-top: 1px solid var(--ui-separator); }
  .open-world-interior-empty { min-height: 11rem; }
}

@media (max-width: 40rem) {
  .open-world-command-bar { grid-template-columns: 1fr auto; }
  .open-world-lifecycle { grid-template-columns: 1fr; }
  .open-world-lifecycle-controls { justify-content: stretch; }
  .open-world-lifecycle-controls label { min-width: 0; flex: 1; }
  .open-world-binding { display: none; }
  .open-world-map-panel { grid-template-rows: auto minmax(23rem, 1fr) auto; }
  .open-world-interior-header { align-items: flex-start; flex-direction: column; }
  .open-world-interior-metadata { justify-content: flex-start; }
  .open-world-interior-map { grid-template-rows: minmax(23rem, 1fr) auto; }
  .open-world-interior-viewport { min-height: 23rem; }
}
</style>
