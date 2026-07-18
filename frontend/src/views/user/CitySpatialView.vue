<template>
  <AppLayout>
    <div class="city-spatial-page">
      <header class="city-page-heading">
        <div>
          <div class="city-heading-index">
            <span>C-07</span>
            <span>{{ t('citySpatial.classic') }}</span>
          </div>
          <h1>{{ t('citySpatial.title') }}</h1>
          <p>{{ t('citySpatial.description') }}</p>
        </div>
        <button type="button" class="btn btn-secondary btn-sm" @click="showCreateDialog = true">
          <Icon name="plus" size="sm" />
          {{ t('citySpatial.createWorld.action') }}
        </button>
      </header>

      <div v-if="store.loadError" class="city-error-banner" role="alert">
        <Icon name="exclamationTriangle" size="sm" />
        <span>{{ store.loadError }}</span>
        <button type="button" @click="void store.refresh()">{{ t('common.retry') }}</button>
      </div>

      <section v-if="store.initialLoading && !store.ruleSet" class="city-loading-shell" aria-live="polite">
        <div class="city-loading-mark">▦</div>
        <strong>{{ t('citySpatial.loading') }}</strong>
        <span>{{ t('citySpatial.loadingDescription') }}</span>
      </section>

      <section v-else-if="store.worlds.length === 0" class="city-empty-world">
        <div class="city-empty-map" aria-hidden="true">
          <span>· · · · · · ·</span>
          <span>· · ═ ═ ═ · ·</span>
          <span>· · · @ · · ·</span>
          <span>· · ≈ ≈ ≈ · ·</span>
          <span>· · · · · · ·</span>
        </div>
        <div>
          <p class="city-panel-eyebrow">{{ t('citySpatial.empty.eyebrow') }}</p>
          <h2>{{ t('citySpatial.empty.title') }}</h2>
          <p>{{ t('citySpatial.empty.description') }}</p>
          <button type="button" class="btn btn-primary" @click="showCreateDialog = true">
            <Icon name="plus" size="sm" />
            {{ t('citySpatial.createWorld.action') }}
          </button>
        </div>
      </section>

      <template v-else-if="store.ruleSet && store.overmap && store.profile">
        <section class="city-command-deck" aria-label="Map controls">
          <div class="city-world-control">
            <label>{{ t('citySpatial.controls.world') }}</label>
            <Select
              :model-value="store.activeWorldID"
              :options="worldOptions"
              :searchable="false"
              @update:model-value="handleWorldSelection"
            >
              <template #selected="{ option }">
                <span class="city-world-option">
                  <strong>{{ option?.label ?? t('citySpatial.controls.selectWorld') }}</strong>
                  <small v-if="activeWorld">T{{ activeWorld.current_tick }} · {{ activeWorld.status }}</small>
                </span>
              </template>
            </Select>
          </div>

          <div class="city-mode-tabs" role="tablist" :aria-label="t('citySpatial.controls.viewMode')">
            <button
              type="button"
              role="tab"
              :aria-selected="store.mapMode === 'overmap'"
              :class="{ active: store.mapMode === 'overmap' }"
              @click="store.showOvermap"
            >
              <span>01</span>{{ t('citySpatial.controls.overmap') }}
            </button>
            <button
              type="button"
              role="tab"
              :aria-selected="store.mapMode === 'local'"
              :class="{ active: store.mapMode === 'local' }"
              @click="store.showLocalMap"
            >
              <span>02</span>{{ t('citySpatial.controls.localMap') }}
            </button>
          </div>

          <div class="city-coordinate-readout">
            <span>{{ store.mapMode === 'local' ? 'XYZ' : 'CHUNK' }}</span>
            <strong>{{ coordinateReadout }}</strong>
          </div>

          <div class="city-command-actions">
            <button type="button" :title="t('citySpatial.controls.zoomOut')" :disabled="store.mapMode !== 'local'" @click="store.changeZoom(-1)">
              <span aria-hidden="true">−</span>
            </button>
            <span class="city-zoom-readout">{{ store.camera.cellSize }}PX</span>
            <button type="button" :title="t('citySpatial.controls.zoomIn')" :disabled="store.mapMode !== 'local'" @click="store.changeZoom(1)">
              <span aria-hidden="true">＋</span>
            </button>
            <button type="button" :title="t('citySpatial.controls.refresh')" :disabled="store.refreshing" @click="void store.refresh()">
              <Icon name="refresh" size="sm" :class="{ 'animate-spin': store.refreshing }" />
            </button>
            <button type="button" :title="t('citySpatial.controls.export')" :disabled="!store.selectedChunk" @click="exportSelectedChunk">
              <Icon name="download" size="sm" />
            </button>
            <button type="button" :title="t('citySpatial.help.action')" @click="showHelpDialog = true">
              <Icon name="questionCircle" size="sm" />
            </button>
          </div>
        </section>

        <section class="city-workbench">
          <nav class="city-depth-rail" :aria-label="t('citySpatial.controls.depth')">
            <span class="city-depth-label">Z</span>
            <button type="button" :disabled="store.mapMode !== 'local' || store.camera.z >= store.profile.maximum_z" @click="store.setZ(store.camera.z + 1)">
              <Icon name="chevronUp" size="sm" />
              <span class="sr-only">{{ t('citySpatial.controls.layerUp') }}</span>
            </button>
            <strong>{{ signedZ }}</strong>
            <button type="button" :disabled="store.mapMode !== 'local' || store.camera.z <= store.profile.minimum_z" @click="store.setZ(store.camera.z - 1)">
              <Icon name="chevronDown" size="sm" />
              <span class="sr-only">{{ t('citySpatial.controls.layerDown') }}</span>
            </button>
            <button type="button" class="city-surface-button" :disabled="store.mapMode !== 'local' || store.camera.z === 0" @click="store.setZ(0)">0</button>
          </nav>

          <article class="city-map-panel">
            <header class="city-map-header">
              <div>
                <span class="city-map-status-dot" />
                <strong>{{ mapHeaderTitle }}</strong>
                <small>{{ mapHeaderSubtitle }}</small>
              </div>
              <div class="city-map-legend" aria-label="Map legend">
                <span><i class="legend-ready" />{{ t('citySpatial.legend.generated') }}</span>
                <span><i class="legend-structure" />{{ t('citySpatial.legend.structure') }}</span>
                <span><i class="legend-selected" />{{ t('citySpatial.legend.selected') }}</span>
                <span><i class="legend-unloaded" />{{ t('citySpatial.legend.unloaded') }}</span>
              </div>
            </header>

            <CityClassicViewport
              :scene="scene"
              :selected-coordinate="store.selectedCoordinate"
              :selected-tile="store.selectedTile"
              :generated-chunk-keys="store.generatedChunkKeys"
              :glyph-characters="glyphCharacters"
              :busy="store.chunkLoading || store.landLoading || store.refreshing"
              :viewport-label="t('citySpatial.viewportAria')"
              @resize="store.setViewportSize"
              @select-cell="store.selectCell"
              @hover-cell="store.hoverCell"
              @select-tile="store.selectOvermapTile"
              @activate-tile="store.openOvermapTile"
              @pan="event => store.panCamera(event.x, event.y)"
              @zoom="store.changeZoom"
              @change-z="direction => store.setZ(store.camera.z + direction)"
              @surface="store.setZ(0)"
              @toggle-mode="toggleMapMode"
              @show-overmap="store.showOvermap"
              @activate-selection="activateSelection"
              @show-help="showHelpDialog = true"
            />

            <footer class="city-map-footer">
              <span>{{ t('citySpatial.controls.dragHint') }}</span>
              <span><kbd>M</kbd> {{ t('citySpatial.controls.modeHint') }}</span>
              <span><kbd>[</kbd><kbd>]</kbd> {{ t('citySpatial.controls.depthHint') }}</span>
              <span><kbd>?</kbd> {{ t('citySpatial.controls.helpHint') }}</span>
            </footer>
          </article>

          <CitySpatialInspector
            :mode="store.mapMode"
            :tile="store.selectedTile"
            :coordinate="store.selectedCoordinate"
            :cell="store.selectedCell"
            :chunk="store.selectedChunk"
            :rule-set="store.ruleSet"
            :land-state="store.activeLandState"
            :development-state="store.developmentState"
            :enterprise-location-state="store.enterpriseLocationState"
            :chunk-size="store.profile.chunk_size"
            :generated="selectedTileGenerated"
          />
        </section>

        <section v-if="store.mapMode === 'overmap' && store.selectedTile" class="city-context-command">
          <div>
            <span>{{ t('citySpatial.context.chunk', { x: store.selectedTile.chunk_x, y: store.selectedTile.chunk_y, z: store.selectedTile.z }) }}</span>
            <strong>{{ store.selectedTile.district_code }} · {{ selectedTileGenerated ? t('citySpatial.inspector.generated') : t('citySpatial.inspector.notGenerated') }}</strong>
          </div>
          <div>
            <button v-if="selectedTileGenerated" type="button" class="btn btn-primary btn-sm" @click="store.openOvermapTile()">
              <Icon name="search" size="sm" />
              {{ t('citySpatial.context.inspect') }}
            </button>
            <button
              v-else
              type="button"
              class="btn btn-primary btn-sm"
              :disabled="!store.canGenerateSelectedTile || Boolean(store.generatingChunkKey)"
              @click="generateSelectedChunk"
            >
              <Icon name="bolt" size="sm" />
              {{ store.generatingChunkKey ? t('citySpatial.context.generating') : t('citySpatial.context.generate') }}
            </button>
          </div>
        </section>

        <CityDevelopmentPanel
          :state="store.developmentState"
          :land-state="store.activeLandState"
          :selected-building-code="selectedBuildingCode"
          :owner="activeWorld?.member_role === 'owner'"
          :busy-project-code="store.developmentCommandCode"
          @command="runDevelopmentCommand"
        />

        <CityEnterpriseLocationPanel
          :state="store.enterpriseLocationState"
          :owner="activeWorld?.member_role === 'owner'"
          :busy-command-code="store.enterpriseLocationCommandCode"
          @command="runEnterpriseLocationCommand"
        />

        <CityWorldRuntimePanel
          v-if="store.worldRuntimeAvailability !== 'unavailable'"
          :catalog="store.worldRuntimeCatalog"
          :actors="store.worldActors"
          :selected-actor-code="store.selectedActorCode"
          :actor-state="store.worldActorState"
          :role-options="store.worldActorRoleOptions"
          :rules="store.worldRuntimeRules"
          :cases="store.worldRuleCases"
          :loading="store.worldRuntimeLoading"
          :busy-command-code="store.worldRuntimeCommandCode"
          @select-actor="actorCode => void store.selectWorldActor(actorCode)"
          @command="runWorldRuntimeCommand"
        />

        <section class="city-change-log">
          <header>
            <div>
              <p class="city-panel-eyebrow">{{ t('citySpatial.changes.eyebrow') }}</p>
              <h2>{{ t('citySpatial.changes.title') }}</h2>
            </div>
            <span>{{ t('citySpatial.changes.count', { count: store.latestChanges.length }) }}</span>
          </header>
          <div v-if="store.latestChanges.length" class="city-change-list">
            <button
              v-for="change in store.latestChanges.slice(0, 12)"
              :key="change.id"
              type="button"
              @click="jumpToChange(change.lines[0])"
            >
              <span class="city-change-tick">T{{ change.tick }}.{{ change.sequence }}</span>
              <strong>{{ change.mutation_type }}</strong>
              <span v-if="change.lines[0]" class="city-change-coordinate">
                {{ change.lines[0].chunk_x }}, {{ change.lines[0].chunk_y }}, {{ change.lines[0].z }}
              </span>
              <time>{{ formatChangeTime(change.posted_at) }}</time>
            </button>
          </div>
          <div v-else class="city-change-empty">
            <Icon name="inbox" size="md" />
            <span>{{ t('citySpatial.changes.empty') }}</span>
          </div>
        </section>

        <p class="sr-only" aria-live="polite">{{ liveSummary }}</p>
      </template>
    </div>

    <BaseDialog :show="showCreateDialog" :title="t('citySpatial.createWorld.title')" width="narrow" @close="showCreateDialog = false">
      <form class="city-create-form" @submit.prevent="createWorld">
        <div>
          <label class="input-label" for="city-world-name">{{ t('citySpatial.createWorld.name') }}</label>
          <input id="city-world-name" v-model.trim="createForm.name" class="input" maxlength="80" required :placeholder="t('citySpatial.createWorld.namePlaceholder')" />
        </div>
        <div>
          <label class="input-label" for="city-world-timezone">{{ t('citySpatial.createWorld.timezone') }}</label>
          <input id="city-world-timezone" v-model.trim="createForm.timezone" class="input font-mono" maxlength="64" required />
          <p class="input-hint">{{ t('citySpatial.createWorld.timezoneHint') }}</p>
        </div>
      </form>
      <template #footer>
        <button type="button" class="btn btn-secondary" @click="showCreateDialog = false">{{ t('common.cancel') }}</button>
        <button type="button" class="btn btn-primary" :disabled="!createForm.name || store.creatingWorld" @click="createWorld">
          {{ store.creatingWorld ? t('citySpatial.createWorld.creating') : t('citySpatial.createWorld.confirm') }}
        </button>
      </template>
    </BaseDialog>

    <BaseDialog :show="showHelpDialog" :title="t('citySpatial.help.title')" width="normal" @close="showHelpDialog = false">
      <div class="city-help-grid">
        <div v-for="shortcut in shortcuts" :key="shortcut.keys">
          <kbd>{{ shortcut.keys }}</kbd>
          <span>{{ shortcut.label }}</span>
        </div>
      </div>
      <p class="city-help-note">{{ t('citySpatial.help.note') }}</p>
    </BaseDialog>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import type {
  CityEnterpriseLocationCommandType,
  CitySpatialMutationLine,
  WorldRuntimeCommandType
} from '@/api/citySpatial'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select, { type SelectOption } from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import CityClassicViewport from '@/features/city-spatial/CityClassicViewport.vue'
import CityDevelopmentPanel from '@/features/city-spatial/CityDevelopmentPanel.vue'
import CityEnterpriseLocationPanel from '@/features/city-spatial/CityEnterpriseLocationPanel.vue'
import CitySpatialInspector from '@/features/city-spatial/CitySpatialInspector.vue'
import CityWorldRuntimePanel from '@/features/city-spatial/CityWorldRuntimePanel.vue'
import {
  buildLocalScene,
  buildOvermapScene,
  chunkKey,
  exportProjectedChunkText,
  getCityLandCellContext,
  type ClassicScene
} from '@/features/city-spatial/projection'
import { useAppStore } from '@/stores/app'
import { useCitySpatialStore } from '@/stores/citySpatial'

const { t, locale } = useI18n()
const route = useRoute()
const router = useRouter()
const appStore = useAppStore()
const store = useCitySpatialStore()

const showCreateDialog = ref(false)
const showHelpDialog = ref(false)
const createForm = reactive({
  name: '',
  timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'
})

const activeWorld = computed(() => store.activeWorld)
const worldOptions = computed<SelectOption[]>(() => store.worlds.map(world => ({
  value: world.id,
  label: world.name
})))
const glyphCharacters = computed(() => `${store.ruleSet?.definitions.map(definition => definition.glyph ?? '').join('') ?? ''}#+↕%&`)
const scene = computed<ClassicScene>(() => {
  if (!store.ruleSet || !store.overmap || !store.profile) {
    return { mode: 'overmap', width: store.viewport.width, height: store.viewport.height, cellSize: 40, offsetX: 0, offsetY: 0, cells: [] }
  }
  return store.mapMode === 'overmap'
    ? buildOvermapScene(
        store.overmap.tiles,
        store.ruleSet,
        store.viewport,
        store.activeLandState,
        store.developmentState,
        store.enterpriseLocationState
      )
    : buildLocalScene(
        store.projectedChunks,
        store.camera,
        store.viewport,
        store.profile.chunk_size,
        store.activeLandState,
        store.developmentState,
        store.enterpriseLocationState
      )
})
const selectedTileGenerated = computed(() => {
  const tile = store.selectedTile
  return Boolean(tile && store.generatedChunkKeys.has(chunkKey(tile.chunk_x, tile.chunk_y, tile.z)))
})
const selectedBuildingCode = computed(() => {
  const coordinate = store.selectedCoordinate
  if (!coordinate || !store.profile) return null
  return getCityLandCellContext(
    store.activeLandState,
    coordinate.worldX,
    coordinate.worldY,
    coordinate.z,
    store.profile.chunk_size
  )?.building?.code ?? null
})
const signedZ = computed(() => store.camera.z > 0 ? `+${store.camera.z}` : String(store.camera.z))
const coordinateReadout = computed(() => {
  if (store.mapMode === 'overmap') {
    const tile = store.selectedTile
    return tile ? `${tile.chunk_x} / ${tile.chunk_y} / ${tile.z}` : '— / — / —'
  }
  const selected = store.selectedCoordinate ?? store.camera
  return `${selected.worldX} / ${selected.worldY} / ${selected.z}`
})
const mapHeaderTitle = computed(() => store.mapMode === 'overmap'
  ? t('citySpatial.mapHeader.overmap')
  : t('citySpatial.mapHeader.local'))
const mapHeaderSubtitle = computed(() => store.mapMode === 'overmap'
  ? t('citySpatial.mapHeader.overmapSubtitle', {
      count: store.overmap?.tiles.length ?? 0,
      buildings: store.activeLandState?.profile.building_count ?? 0
    })
  : t('citySpatial.mapHeader.localSubtitle', {
      count: store.projectedChunks.size,
      buildings: store.activeLandState?.buildings.length ?? 0
    }))
const liveSummary = computed(() => store.mapMode === 'overmap'
  ? t('citySpatial.live.overmap', { coordinate: coordinateReadout.value })
  : t('citySpatial.live.local', { coordinate: coordinateReadout.value, layers: store.selectedCell?.stack.length ?? 0 }))

const shortcuts = computed(() => [
  { keys: '← ↑ ↓ →', label: t('citySpatial.help.pan') },
  { keys: 'WHEEL', label: t('citySpatial.help.zoom') },
  { keys: '[ / ]', label: t('citySpatial.help.depth') },
  { keys: '0', label: t('citySpatial.help.surface') },
  { keys: 'M', label: t('citySpatial.help.mode') },
  { keys: 'ENTER', label: t('citySpatial.help.inspect') },
  { keys: 'ESC', label: t('citySpatial.help.back') },
  { keys: '?', label: t('citySpatial.help.openHelp') }
])

function preferredWorldID(): number | undefined {
  const raw = Array.isArray(route.query.world) ? route.query.world[0] : route.query.world
  const parsed = Number(raw)
  return Number.isSafeInteger(parsed) && parsed > 0 ? parsed : undefined
}

function handleWorldSelection(value: string | number | boolean | null): void {
  const worldID = Number(value)
  if (Number.isSafeInteger(worldID) && worldID > 0 && worldID !== store.activeWorldID) {
    void store.selectWorld(worldID)
  }
}

function toggleMapMode(): void {
  if (store.mapMode === 'overmap') store.showLocalMap()
  else store.showOvermap()
}

function activateSelection(): void {
  if (store.mapMode === 'overmap' && store.selectedTile) store.openOvermapTile()
}

async function generateSelectedChunk(): Promise<void> {
  try {
    await store.generateSelectedChunk()
    store.openOvermapTile()
    appStore.showSuccess(t('citySpatial.context.generatedSuccess'))
  } catch {
    appStore.showError(store.loadError ?? t('citySpatial.context.generateFailed'))
  }
}

async function runDevelopmentCommand(input: {
  commandType: 'development.submit' | 'development.review' | 'development.start' | 'development.cancel'
  payload: Record<string, unknown>
  projectCode: string
}): Promise<void> {
  try {
    await store.runDevelopmentCommand(input.commandType, input.payload, input.projectCode)
    appStore.showSuccess(t('citySpatial.development.commandSuccess'))
  } catch {
    appStore.showError(store.loadError ?? t('citySpatial.development.commandFailed'))
  }
}

async function runEnterpriseLocationCommand(input: {
  commandType: CityEnterpriseLocationCommandType
  payload: Record<string, unknown>
  commandCode: string
}): Promise<void> {
  try {
    await store.runEnterpriseLocationCommand(input.commandType, input.payload, input.commandCode)
    appStore.showSuccess(t('citySpatial.enterprise.commandSuccess'))
  } catch {
    appStore.showError(store.loadError ?? t('citySpatial.enterprise.commandFailed'))
  }
}

async function runWorldRuntimeCommand(
  commandType: WorldRuntimeCommandType,
  payload: Record<string, unknown>,
  commandCode: string
): Promise<void> {
  try {
    await store.runWorldRuntimeCommand(commandType, payload, commandCode)
    appStore.showSuccess(t('citySpatial.runtime.commandSuccess'))
  } catch {
    appStore.showError(store.loadError ?? t('citySpatial.runtime.commandFailed'))
  }
}

function exportSelectedChunk(): void {
  const chunk = store.selectedChunk
  const world = store.activeWorld
  if (!chunk || !world) {
    appStore.showError(t('citySpatial.export.unavailable'))
    return
  }
  const content = exportProjectedChunkText(chunk)
  const blob = new Blob([content], { type: 'text/plain;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = `city-${world.id}-chunk-${chunk.chunkX}-${chunk.chunkY}-${chunk.z}.txt`
  document.body.appendChild(link)
  link.click()
  link.remove()
  URL.revokeObjectURL(url)
  appStore.showSuccess(t('citySpatial.export.success'))
}

async function createWorld(): Promise<void> {
  if (!createForm.name || store.creatingWorld) return
  try {
    await store.createWorld({ name: createForm.name, timezone: createForm.timezone })
    showCreateDialog.value = false
    createForm.name = ''
    appStore.showSuccess(t('citySpatial.createWorld.success'))
  } catch {
    appStore.showError(store.loadError ?? t('citySpatial.createWorld.failed'))
  }
}

function jumpToChange(line?: CitySpatialMutationLine): void {
  if (!line || !store.overmap) return
  const tile = store.overmap.tiles.find(item => (
    item.chunk_x === line.chunk_x && item.chunk_y === line.chunk_y && item.z === line.z
  ))
  if (tile) store.openOvermapTile(tile)
}

function formatChangeTime(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return new Intl.DateTimeFormat(locale.value, {
    month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit'
  }).format(date)
}

watch(() => store.activeWorldID, worldID => {
  if (!worldID || String(route.query.world ?? '') === String(worldID)) return
  void router.replace({ query: { ...route.query, world: String(worldID) } })
})

onMounted(() => {
  void store.initialize(preferredWorldID())
})
</script>

<style scoped>
.city-spatial-page {
  width: min(100%, 112rem);
  margin: 0 auto;
  color: var(--ui-label);
}

.city-page-heading {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 1.5rem;
  margin-bottom: 1.25rem;
}

.city-page-heading h1 {
  margin: 0.3rem 0 0.15rem;
  font-size: clamp(1.65rem, 2.4vw, 2.35rem);
  font-weight: 760;
  letter-spacing: -0.04em;
}

.city-page-heading p {
  margin: 0;
  color: var(--ui-label-secondary);
  font-size: 0.9rem;
}

.city-heading-index {
  display: flex;
  align-items: center;
  gap: 0.6rem;
  color: var(--ui-label-secondary);
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.68rem;
  letter-spacing: 0.15em;
  text-transform: uppercase;
}

.city-heading-index span:first-child {
  border: 1px solid var(--ui-separator);
  padding: 0.18rem 0.35rem;
  color: var(--ui-accent);
}

.city-error-banner {
  display: flex;
  align-items: center;
  gap: 0.65rem;
  margin-bottom: 0.75rem;
  border: 1px solid rgb(239 68 68 / 42%);
  border-left-width: 3px;
  padding: 0.75rem 0.9rem;
  color: #dc2626;
  background: rgb(239 68 68 / 6%);
  font-size: 0.8rem;
}

.city-error-banner span { min-width: 0; flex: 1; overflow-wrap: anywhere; }
.city-error-banner button { color: inherit; font-weight: 700; text-decoration: underline; }

.city-loading-shell,
.city-empty-world {
  min-height: 34rem;
  border: 1px solid var(--ui-separator);
  background: var(--ui-surface);
}

.city-loading-shell {
  display: grid;
  place-content: center;
  justify-items: center;
  gap: 0.5rem;
  color: var(--ui-label-secondary);
}

.city-loading-shell strong { color: var(--ui-label); }
.city-loading-mark { color: var(--ui-accent); font: 2.5rem/1 ui-monospace, monospace; animation: city-pulse 1.2s steps(2, end) infinite; }

.city-empty-world {
  display: grid;
  grid-template-columns: minmax(18rem, 0.9fr) minmax(18rem, 1.1fr);
  align-items: center;
  gap: clamp(2rem, 6vw, 7rem);
  padding: clamp(2rem, 6vw, 6rem);
}

.city-empty-world h2 { margin: 0.35rem 0 0.55rem; font-size: 1.5rem; }
.city-empty-world p:not(.city-panel-eyebrow) { max-width: 34rem; margin: 0 0 1.4rem; color: var(--ui-label-secondary); line-height: 1.7; }

.city-empty-map {
  display: grid;
  gap: 0.55rem;
  border: 1px solid var(--ui-separator);
  padding: 2.5rem;
  color: #9ca3af;
  background: #111318;
  font: 1.35rem/1 ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  text-align: center;
}

.city-empty-map span:nth-child(2) { color: #d4d4d8; }
.city-empty-map span:nth-child(3) { color: #5aa2ff; }
.city-empty-map span:nth-child(4) { color: #5f87af; }

.city-command-deck {
  display: grid;
  grid-template-columns: minmax(14rem, 1.15fr) auto minmax(11rem, 0.75fr) auto;
  align-items: end;
  gap: 0.75rem;
  border: 1px solid var(--ui-separator);
  border-bottom: 0;
  padding: 0.85rem;
  background: var(--ui-surface);
}

.city-world-control label {
  display: block;
  margin-bottom: 0.35rem;
  color: var(--ui-label-secondary);
  font-size: 0.7rem;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

.city-world-option { display: flex; min-width: 0; align-items: center; justify-content: space-between; gap: 0.75rem; }
.city-world-option strong { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.city-world-option small { flex: none; color: var(--ui-label-secondary); font: 0.65rem ui-monospace, monospace; text-transform: uppercase; }

.city-mode-tabs {
  display: flex;
  border: 1px solid var(--ui-separator);
}

.city-mode-tabs button {
  display: flex;
  height: 2.75rem;
  align-items: center;
  gap: 0.45rem;
  border-right: 1px solid var(--ui-separator);
  padding: 0 0.8rem;
  color: var(--ui-label-secondary);
  background: var(--ui-control);
  font-size: 0.75rem;
}

.city-mode-tabs button:last-child { border-right: 0; }
.city-mode-tabs button:hover { color: var(--ui-label); background: var(--ui-control-hover); }
.city-mode-tabs button.active { color: #fff; background: var(--ui-accent); }
.city-mode-tabs button span { font: 0.6rem ui-monospace, monospace; opacity: 0.7; }

.city-coordinate-readout {
  height: 2.75rem;
  border: 1px solid var(--ui-separator);
  padding: 0.45rem 0.7rem;
  background: var(--ui-canvas-raised);
}

.city-coordinate-readout span { display: block; color: var(--ui-label-secondary); font: 0.56rem ui-monospace, monospace; letter-spacing: 0.12em; }
.city-coordinate-readout strong { display: block; margin-top: 0.15rem; font: 0.74rem ui-monospace, monospace; }

.city-command-actions { display: flex; height: 2.75rem; border: 1px solid var(--ui-separator); }
.city-command-actions button,
.city-depth-rail button {
  display: grid;
  width: 2.75rem;
  place-items: center;
  border-right: 1px solid var(--ui-separator);
  color: var(--ui-label-secondary);
  background: var(--ui-control);
}
.city-command-actions button:hover:not(:disabled), .city-depth-rail button:hover:not(:disabled) { color: var(--ui-label); background: var(--ui-control-hover); }
.city-command-actions button:disabled, .city-depth-rail button:disabled { cursor: not-allowed; opacity: 0.35; }
.city-zoom-readout { display: grid; min-width: 3.4rem; place-items: center; border-right: 1px solid var(--ui-separator); font: 0.65rem ui-monospace, monospace; }

.city-workbench {
  display: grid;
  grid-template-columns: 3.25rem minmax(0, 1fr) minmax(17rem, 21rem);
  align-items: stretch;
  gap: 0;
}

.city-depth-rail {
  display: flex;
  min-height: 31rem;
  flex-direction: column;
  align-items: stretch;
  border: 1px solid var(--ui-separator);
  border-right: 0;
  background: var(--ui-surface);
}

.city-depth-label { padding: 0.75rem 0; color: var(--ui-label-secondary); font: 0.65rem ui-monospace, monospace; text-align: center; }
.city-depth-rail button { width: 100%; height: 2.75rem; border-top: 1px solid var(--ui-separator); border-right: 0; }
.city-depth-rail strong { padding: 0.9rem 0; color: var(--ui-accent); font: 0.8rem ui-monospace, monospace; text-align: center; }
.city-surface-button { margin-top: auto; border-bottom: 0; font: 0.75rem ui-monospace, monospace; }

.city-map-panel {
  min-width: 0;
  border: 1px solid var(--ui-separator);
  background: #111318;
}

.city-map-header,
.city-map-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  color: #d4d4d8;
  background: #181a1f;
}

.city-map-header { min-height: 4.5rem; border-bottom: 1px solid #343941; padding: 0.8rem 1rem; }
.city-map-header > div:first-child { display: grid; grid-template-columns: auto 1fr; align-items: center; column-gap: 0.55rem; }
.city-map-header strong { font-size: 0.85rem; }
.city-map-header small { grid-column: 2; margin-top: 0.18rem; color: #8b919c; font: 0.65rem ui-monospace, monospace; }
.city-map-status-dot { width: 0.45rem; height: 0.45rem; background: #31d17c; box-shadow: 0 0 0 3px rgb(49 209 124 / 12%); }

.city-map-legend { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 0.8rem; color: #8b919c; font-size: 0.65rem; }
.city-map-legend span { display: inline-flex; align-items: center; gap: 0.3rem; }
.city-map-legend i { display: inline-block; width: 0.45rem; height: 0.45rem; }
.legend-ready { background: #31d17c; }
.legend-structure { border-bottom: 2px solid #d6c6a5; }
.legend-selected { border: 1px solid #5aa2ff; }
.legend-unloaded { background: #343941; }

.city-map-footer { min-height: 2.5rem; justify-content: flex-start; flex-wrap: wrap; border-top: 1px solid #343941; padding: 0.45rem 0.8rem; color: #737985; font-size: 0.62rem; }
.city-map-footer kbd, .city-help-grid kbd { border: 1px solid currentColor; padding: 0.08rem 0.25rem; font: inherit; }

.city-context-command {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  border: 1px solid var(--ui-separator);
  border-top: 0;
  padding: 0.75rem 1rem;
  background: var(--ui-canvas-raised);
}

.city-context-command span { display: block; color: var(--ui-label-secondary); font: 0.63rem ui-monospace, monospace; }
.city-context-command strong { display: block; margin-top: 0.2rem; font-size: 0.78rem; }

.city-change-log { margin-top: 1rem; border: 1px solid var(--ui-separator); background: var(--ui-surface); }
.city-change-log > header { display: flex; align-items: center; justify-content: space-between; gap: 1rem; border-bottom: 1px solid var(--ui-separator); padding: 0.85rem 1rem; }
.city-change-log h2 { margin: 0.15rem 0 0; font-size: 0.9rem; }
.city-change-log > header > span { color: var(--ui-label-secondary); font: 0.65rem ui-monospace, monospace; }
.city-panel-eyebrow { margin: 0; color: var(--ui-label-secondary); font: 0.62rem ui-monospace, monospace; letter-spacing: 0.13em; text-transform: uppercase; }

.city-change-list { display: grid; grid-template-columns: repeat(auto-fit, minmax(17rem, 1fr)); max-height: 14rem; overflow: auto; }
.city-change-list button { display: grid; grid-template-columns: auto minmax(0, 1fr) auto; align-items: center; gap: 0.6rem; border-right: 1px solid var(--ui-separator); border-bottom: 1px solid var(--ui-separator); padding: 0.75rem 0.85rem; text-align: left; }
.city-change-list button:hover { background: var(--ui-control); }
.city-change-list strong { overflow: hidden; font: 0.7rem ui-monospace, monospace; text-overflow: ellipsis; white-space: nowrap; }
.city-change-tick { color: var(--ui-accent); font: 0.65rem ui-monospace, monospace; }
.city-change-coordinate { color: var(--ui-label-secondary); font: 0.65rem ui-monospace, monospace; }
.city-change-list time { grid-column: 2 / -1; color: var(--ui-label-secondary); font-size: 0.63rem; }
.city-change-empty { display: flex; align-items: center; justify-content: center; gap: 0.5rem; min-height: 5rem; color: var(--ui-label-secondary); font-size: 0.75rem; }

.city-create-form { display: grid; gap: 1rem; }
.city-help-grid { display: grid; grid-template-columns: 1fr 1fr; border-top: 1px solid var(--ui-separator); border-left: 1px solid var(--ui-separator); }
.city-help-grid div { display: grid; grid-template-columns: 5.5rem minmax(0, 1fr); align-items: center; gap: 0.75rem; border-right: 1px solid var(--ui-separator); border-bottom: 1px solid var(--ui-separator); padding: 0.75rem; }
.city-help-grid kbd { width: max-content; color: var(--ui-accent); font: 0.65rem ui-monospace, monospace; }
.city-help-grid span { color: var(--ui-label-secondary); font-size: 0.75rem; }
.city-help-note { margin: 1rem 0 0; color: var(--ui-label-secondary); font-size: 0.72rem; line-height: 1.6; }

@keyframes city-pulse { 50% { opacity: 0.35; } }

@media (max-width: 1280px) {
  .city-command-deck { grid-template-columns: minmax(14rem, 1fr) auto auto; }
  .city-coordinate-readout { display: none; }
  .city-workbench { grid-template-columns: 3.25rem minmax(0, 1fr); }
  .city-workbench :deep(.city-inspector) { grid-column: 2; min-height: 0; max-height: 28rem; }
}

@media (max-width: 850px) {
  .city-page-heading { align-items: flex-start; }
  .city-command-deck { grid-template-columns: 1fr; align-items: stretch; }
  .city-mode-tabs button { flex: 1; justify-content: center; }
  .city-command-actions { justify-self: start; }
  .city-workbench { grid-template-columns: 1fr; }
  .city-depth-rail { min-height: 0; flex-direction: row; border-right: 1px solid var(--ui-separator); border-bottom: 0; }
  .city-depth-label, .city-depth-rail strong { display: grid; min-width: 3rem; place-items: center; padding: 0; }
  .city-depth-rail button { width: 2.75rem; border-top: 0; border-left: 1px solid var(--ui-separator); }
  .city-surface-button { margin-top: 0; margin-left: auto; }
  .city-map-legend { display: none; }
  .city-workbench :deep(.city-inspector) { grid-column: 1; }
  .city-empty-world { grid-template-columns: 1fr; }
}

@media (max-width: 560px) {
  .city-page-heading { display: grid; }
  .city-page-heading .btn { width: max-content; }
  .city-map-header { min-height: 3.75rem; }
  .city-map-footer span:first-child { width: 100%; }
  .city-context-command { align-items: flex-start; flex-direction: column; }
  .city-help-grid { grid-template-columns: 1fr; }
  .city-empty-world { padding: 1rem; }
  .city-empty-map { padding: 1.5rem 0.5rem; font-size: 1rem; }
}

@media (prefers-reduced-motion: reduce) {
  .city-loading-mark { animation: none; }
}
</style>
