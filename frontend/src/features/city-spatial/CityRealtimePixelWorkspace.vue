<template>
  <section class="realtime-pixel-workspace">
    <header class="realtime-pixel-command-bar">
      <label class="realtime-pixel-world-select">
        <span>{{ t('citySpatial.realtime.world') }}</span>
        <select :value="world.id" @change="selectWorld">
          <option v-for="option in worldOptions" :key="option.id" :value="option.id">
            {{ option.name }}
          </option>
        </select>
      </label>

      <div class="realtime-pixel-identity">
        <span>{{ t('citySpatial.realtime.sharedWorld') }}</span>
        <strong>{{ projection?.spatial.profile_id ?? '—' }}</strong>
        <small>{{ visualPackLabel }}</small>
      </div>

      <div class="realtime-pixel-clock" :data-state="clock?.world_time.clock_state ?? 'initializing'">
        <span>{{ t('citySpatial.realtime.worldTime') }}</span>
        <strong>{{ formattedWorldTime }}</strong>
        <small>
          {{ projection?.timeline_cursor ?? '—' }} ·
          {{ clock?.world_time.live_projection ? t('citySpatial.realtime.clock.live') : t('citySpatial.realtime.clock.committed') }}
        </small>
      </div>

      <div class="realtime-pixel-actions">
        <button type="button" :disabled="camera.cellSize <= cellSizeSteps[0]" :title="t('citySpatial.realtime.zoomOut')" @click="changeZoom(-1)">−</button>
        <span>{{ camera.cellSize }}PX</span>
        <button type="button" :disabled="camera.cellSize >= cellSizeSteps[cellSizeSteps.length - 1]" :title="t('citySpatial.realtime.zoomIn')" @click="changeZoom(1)">＋</button>
        <button type="button" :disabled="refreshing" :title="t('citySpatial.realtime.refresh')" @click="void refreshWorld()">
          <Icon name="refresh" size="sm" :class="{ 'animate-spin': refreshing }" />
        </button>
        <button type="button" :title="t('citySpatial.realtime.returnToSpawn')" @click="resetCamera">
          <Icon name="home" size="sm" />
        </button>
      </div>
    </header>

    <div v-if="error" class="realtime-pixel-error" role="alert">
      <Icon name="exclamationTriangle" size="sm" />
      <span>{{ error }}</span>
      <button type="button" @click="void refreshWorld()">{{ t('common.retry') }}</button>
    </div>

    <div class="realtime-pixel-layout">
      <article class="realtime-pixel-map-panel">
        <header class="realtime-pixel-map-heading">
          <div>
            <span class="realtime-pixel-live-dot" :data-state="clock?.world_time.clock_state ?? 'initializing'" />
            <strong>{{ t('citySpatial.realtime.mapTitle') }}</strong>
            <small>{{ t('citySpatial.realtime.mapSubtitle', { chunks: loadedChunkCount, actors: visibleActorCount, cursor: projection?.timeline_cursor ?? '—' }) }}</small>
          </div>
          <span>{{ selectedCell ? `${selectedCell.worldX} / ${selectedCell.worldY} / ${selectedCell.z}` : 'X / Y / Z' }}</span>
        </header>

        <div ref="canvasHostRef" class="realtime-pixel-canvas-host" :class="{ 'is-dragging': dragging }">
          <canvas
            ref="canvasRef"
            class="realtime-pixel-canvas"
            role="application"
            tabindex="0"
            :aria-label="t('citySpatial.realtime.viewportAria')"
            @pointerdown="handlePointerDown"
            @pointermove="handlePointerMove"
            @pointerup="handlePointerUp"
            @pointercancel="endPointerInteraction"
            @wheel.prevent="handleWheel"
            @keydown="handleKeyDown"
          />
          <div v-if="initialLoading" class="realtime-pixel-initial-loading" aria-live="polite">
            <span class="realtime-pixel-loader" aria-hidden="true" />
            <strong>{{ t('citySpatial.realtime.loading') }}</strong>
            <small>{{ t('citySpatial.realtime.loadingDescription') }}</small>
          </div>
          <div v-else-if="pendingChunkCount > 0" class="realtime-pixel-chunk-loading" aria-live="polite">
            <span class="realtime-pixel-loader" aria-hidden="true" />
            {{ t('citySpatial.realtime.loadingChunks') }}
          </div>
        </div>

        <footer>
          <span>{{ t('citySpatial.realtime.dragHint') }}</span>
          <span><kbd>←</kbd><kbd>↑</kbd><kbd>↓</kbd><kbd>→</kbd> {{ t('citySpatial.realtime.panHint') }}</span>
          <span><kbd>WHEEL</kbd> {{ t('citySpatial.realtime.zoomHint') }}</span>
          <span><kbd>0</kbd> {{ t('citySpatial.realtime.spawnHint') }}</span>
        </footer>
      </article>

      <aside class="realtime-pixel-inspector" aria-live="polite">
        <header>
          <span>{{ t('citySpatial.realtime.inspector.eyebrow') }}</span>
          <h2>{{ t('citySpatial.realtime.inspector.title') }}</h2>
        </header>

        <section class="realtime-pixel-character" :aria-busy="characterLoading || characterBusy">
          <div class="realtime-pixel-character-heading">
            <span>{{ t('citySpatial.realtime.character.eyebrow') }}</span>
            <strong>{{ t('citySpatial.realtime.character.title') }}</strong>
          </div>

          <p v-if="characterLoading" class="realtime-pixel-character-note">
            {{ t('citySpatial.realtime.character.loading') }}
          </p>

          <p v-else-if="!myCharacter?.runtime_ready" class="realtime-pixel-character-note">
            {{ t('citySpatial.realtime.character.runtimeUnavailable') }}
          </p>

          <form v-else-if="!myCharacter.exists" class="realtime-pixel-character-create" @submit.prevent="void createMyCharacter()">
            <label>
              <span>{{ t('citySpatial.realtime.character.label') }}</span>
              <input v-model="characterLabel" :maxlength="64" :placeholder="t('citySpatial.realtime.character.labelPlaceholder')" autocomplete="off">
            </label>
            <label v-if="characterArchetypes.length > 0">
              <span>{{ t('citySpatial.realtime.character.progression.archetypeSelect') }}</span>
              <select v-model="selectedArchetypeCode">
                <option v-for="archetype in characterArchetypes" :key="archetype.code" :value="archetype.code">
                  {{ archetypeLabel(archetype.code) }}
                </option>
              </select>
            </label>
            <div v-if="selectedArchetype" class="realtime-pixel-character-archetype-preview">
              <strong>{{ archetypeLabel(selectedArchetype.code) }}</strong>
              <small>{{ roleLabel(selectedArchetype.initial_role_code) }}</small>
              <span v-for="attribute in selectedArchetype.initial_attributes" :key="attribute.code">
                {{ attributeLabel(attribute.code) }} {{ attribute.value_milli }}
              </span>
            </div>
            <p>{{ t('citySpatial.realtime.character.createHint') }}</p>
            <button type="submit" :disabled="characterBusy || !characterLabel.trim() || (characterArchetypes.length > 0 && !selectedArchetypeCode)">
              {{ characterBusy ? t('citySpatial.realtime.character.creating') : t('citySpatial.realtime.character.create') }}
            </button>
          </form>

          <div v-else-if="myCharacter.character" class="realtime-pixel-character-active">
            <strong>{{ myCharacter.character.public_label }}</strong>
            <span>{{ myCharacter.character.x }} / {{ myCharacter.character.y }} / {{ myCharacter.character.z }}</span>
            <small>{{ myCharacter.character.control_mode }} · {{ myCharacter.character.motion_state }}</small>

            <section v-if="characterAgent" class="realtime-pixel-character-agent" :data-mode="characterAgent.control_mode">
              <header>
                <span>{{ t('citySpatial.realtime.character.agent.title') }}</span>
                <small>{{ characterAgentModeLabel(characterAgent.control_mode) }}</small>
              </header>
              <p>{{ t('citySpatial.realtime.character.agent.description') }}</p>
              <dl>
                <div>
                  <dt>{{ t('citySpatial.realtime.character.agent.personalityRevision') }}</dt>
                  <dd>{{ characterAgent.personality?.revision ?? '—' }}</dd>
                </div>
                <div>
                  <dt>{{ t('citySpatial.realtime.character.agent.queue') }}</dt>
                  <dd>{{ characterAgentQueueLabel }}</dd>
                </div>
              </dl>
              <form @submit.prevent="void configureMyCharacterAgent()">
                <label>
                  <span>{{ t('citySpatial.realtime.character.agent.mode') }}</span>
                  <select v-model="characterAgentTargetMode" :disabled="characterBusy" @change="characterAgentControlDirty = true">
                    <option value="autonomous">{{ t('citySpatial.realtime.character.agent.modes.autonomous') }}</option>
                    <option value="suspended">{{ t('citySpatial.realtime.character.agent.modes.suspended') }}</option>
                  </select>
                </label>
                <label>
                  <span>{{ t('citySpatial.realtime.character.agent.values') }}</span>
                  <input v-model="characterAgentValues" :maxlength="512" :placeholder="t('citySpatial.realtime.character.agent.valuesPlaceholder')" autocomplete="off" @input="characterAgentPersonalityDirty = true">
                </label>
                <label>
                  <span>{{ t('citySpatial.realtime.character.agent.boundaries') }}</span>
                  <input v-model="characterAgentBoundaries" :maxlength="512" :placeholder="t('citySpatial.realtime.character.agent.boundariesPlaceholder')" autocomplete="off" @input="characterAgentPersonalityDirty = true">
                </label>
                <label>
                  <span>{{ t('citySpatial.realtime.character.agent.background') }}</span>
                  <textarea v-model="characterAgentBackground" :maxlength="600" :placeholder="t('citySpatial.realtime.character.agent.backgroundPlaceholder')" @input="characterAgentPersonalityDirty = true"></textarea>
                </label>
                <label>
                  <span>{{ t('citySpatial.realtime.character.agent.notes') }}</span>
                  <textarea v-model="characterAgentNotes" :maxlength="600" :placeholder="t('citySpatial.realtime.character.agent.notesPlaceholder')" @input="characterAgentPersonalityDirty = true"></textarea>
                </label>
                <p>{{ t('citySpatial.realtime.character.agent.ownerPrivate') }}</p>
                <button type="submit" :disabled="characterBusy || !canSubmitCharacterAgentConfiguration">
                  {{ characterBusy ? t('citySpatial.realtime.character.agent.saving') : t('citySpatial.realtime.character.agent.save') }}
                </button>
              </form>
            </section>

            <p v-if="!characterManualControlsAvailable" class="realtime-pixel-character-control-note">
              {{ t('citySpatial.realtime.character.agent.manualControlUnavailable', { mode: characterAgentModeLabel(myCharacter.character.control_mode) }) }}
            </p>

            <p v-if="selectedCell">
              {{ t('citySpatial.realtime.character.selectedTarget', { x: selectedCell.worldX, y: selectedCell.worldY, z: selectedCell.z }) }}
            </p>
            <p v-else>{{ t('citySpatial.realtime.character.selectTarget') }}</p>
            <button type="button" :disabled="characterBusy || !canMoveToSelectedCell || !characterManualControlsAvailable" @click="void moveMyCharacter()">
              {{ characterBusy ? t('citySpatial.realtime.character.moving') : t('citySpatial.realtime.character.move') }}
            </button>
            <small v-if="selectedCell && !canMoveToSelectedCell">{{ t('citySpatial.realtime.character.adjacentOnly') }}</small>

            <section v-if="characterPortals.length > 0" class="realtime-pixel-character-portals" :aria-label="t('citySpatial.realtime.character.portals.title')">
              <header>
                <span>{{ t('citySpatial.realtime.character.portals.title') }}</span>
                <small>{{ t('citySpatial.realtime.character.portals.serverAuthoritative') }}</small>
              </header>
              <div>
                <button
                  v-for="portal in characterPortals"
                  :key="portal.portal_code"
                  type="button"
                  :disabled="characterBusy || !characterManualControlsAvailable"
                  @click="void traverseMyCharacterPortal(portal)"
                >
                  <strong>{{ portalDirectionLabel(portal.direction) }}</strong>
                  <small>{{ portal.building_code }} · {{ portal.target.x }} / {{ portal.target.y }} / {{ portal.target.z }}</small>
                </button>
              </div>
            </section>

            <section v-if="characterInterior && interiorGrid.cells.length > 0" class="realtime-pixel-character-interior" :aria-label="t('citySpatial.realtime.character.interior.title')">
              <header>
                <span>{{ t('citySpatial.realtime.character.interior.title') }}</span>
                <small>{{ characterInterior.building_code }} · {{ t('citySpatial.realtime.character.interior.floor', { floor: characterInterior.floor_index + 1 }) }}</small>
              </header>
              <div class="realtime-pixel-character-interior-grid" :style="{ gridTemplateColumns: `repeat(${interiorGrid.width}, minmax(1.4rem, 1fr))` }">
                <button
                  v-for="tile in interiorGrid.cells"
                  :key="`${tile.cell.x}/${tile.cell.y}/${tile.cell.z}`"
                  type="button"
                  :class="{
                    'is-current': isCurrentInteriorCell(tile.cell),
                    'is-traversable': tile.cell.traversable,
                    'is-blocked': !tile.cell.traversable
                  }"
                  :style="{ gridColumn: tile.column, gridRow: tile.row }"
                  :disabled="characterBusy || !canMoveToInteriorCell(tile.cell) || !characterManualControlsAvailable"
                  :title="interiorCellDescription(tile.cell)"
                  @click="void moveMyCharacterTo({ x: tile.cell.x, y: tile.cell.y, z: tile.cell.z })"
                >
                  {{ interiorCellGlyph(tile.cell) }}
                </button>
              </div>
            </section>

            <section v-if="characterLife" class="realtime-pixel-character-life" :aria-label="t('citySpatial.realtime.character.life.title')">
              <header>
                <span>{{ t('citySpatial.realtime.character.life.title') }}</span>
                <small>{{ t('citySpatial.realtime.character.life.localOnly') }}</small>
              </header>
              <div class="realtime-pixel-character-needs">
                <div v-for="need in characterNeeds" :key="need.code" class="realtime-pixel-character-need">
                  <span>{{ need.label }}</span>
                  <strong>{{ need.value }}‰</strong>
                  <i aria-hidden="true"><b :style="{ width: `${need.value / 10}%` }" /></i>
                </div>
              </div>
              <dl>
                <div>
                  <dt>{{ t('citySpatial.realtime.character.life.cityCredit') }}</dt>
                  <dd>{{ formatCityCredit(characterLife.city_credit_units) }}</dd>
                </div>
                <div>
                  <dt>{{ t('citySpatial.realtime.character.life.rations') }}</dt>
                  <dd>{{ characterRationQuantity }}</dd>
                </div>
              </dl>
            </section>

            <section v-if="characterProgression" class="realtime-pixel-character-progression" :aria-label="t('citySpatial.realtime.character.progression.title')">
              <header>
                <span>{{ t('citySpatial.realtime.character.progression.title') }}</span>
                <small>{{ t('citySpatial.realtime.character.progression.private') }}</small>
              </header>
              <div class="realtime-pixel-character-progression-summary">
                <div>
                  <span>{{ t('citySpatial.realtime.character.progression.archetype') }}</span>
                  <strong>{{ archetypeLabel(characterProgression.archetype_code) }}</strong>
                </div>
                <div>
                  <span>{{ t('citySpatial.realtime.character.progression.experience') }}</span>
                  <strong>{{ characterTotalExperience }}</strong>
                </div>
              </div>
              <div class="realtime-pixel-character-attributes">
                <div v-for="attribute in characterProgression.attributes" :key="attribute.code" class="realtime-pixel-character-attribute">
                  <span>{{ attributeLabel(attribute.code) }}</span>
                  <strong>{{ attribute.value_milli }}</strong>
                  <small>XP {{ attribute.experience_units }}</small>
                  <i aria-hidden="true"><b :style="{ width: `${Math.min(100, attribute.value_milli / 10)}%` }" /></i>
                </div>
              </div>
              <div class="realtime-pixel-character-active-roles">
                <span>{{ t('citySpatial.realtime.character.progression.currentRoles') }}</span>
                <strong v-for="role in characterProgression.roles" :key="role.category_code">{{ roleLabel(role.code) }}</strong>
              </div>
              <div class="realtime-pixel-character-role-options">
                <article
                  v-for="role in characterProgression.available_roles"
                  :key="role.code"
                  :data-available="role.available"
                  :title="roleRequirementSummary(role.requirements)"
                >
                  <div>
                    <strong>{{ roleLabel(role.code) }}</strong>
                    <small>{{ roleAvailabilityDescription(role) }}</small>
                  </div>
                  <button
                    v-if="role.available"
                    type="button"
                    :disabled="characterBusy || !characterManualControlsAvailable"
                    @click="void changeMyCharacterRole(role.code)"
                  >
                    {{ characterBusy ? t('citySpatial.realtime.character.progression.changing') : t('citySpatial.realtime.character.progression.change') }}
                  </button>
                </article>
              </div>
            </section>

            <section v-if="characterActivities.length > 0" class="realtime-pixel-character-activities" :aria-label="t('citySpatial.realtime.character.activities.title')">
              <header>
                <span>{{ t('citySpatial.realtime.character.activities.title') }}</span>
                <small>{{ t('citySpatial.realtime.character.activities.serverAuthoritative') }}</small>
              </header>
              <div>
                <button
                  v-for="activity in characterActivities"
                  :key="activity.code"
                  type="button"
                  :disabled="characterBusy || !activity.available || !characterManualControlsAvailable"
                  :title="activityAvailabilityDescription(activity)"
                  @click="void performMyCharacterActivity(activity.code)"
                >
                  <strong>{{ activityLabel(activity.code) }}</strong>
                  <small v-if="!activity.available">{{ activityAvailabilityDescription(activity) }}</small>
                </button>
              </div>
            </section>

            <section v-if="characterEvents.length > 0" class="realtime-pixel-character-history" :aria-label="t('citySpatial.realtime.character.history.title')">
              <header>
                <span>{{ t('citySpatial.realtime.character.history.title') }}</span>
                <small>{{ t('citySpatial.realtime.character.history.private') }}</small>
              </header>
              <ol>
                <li v-for="event in characterEvents.slice(0, 3)" :key="event.sequence">
                  <strong>{{ activityLabel(event.activity_code) }}</strong>
                  <span :data-outcome="event.outcome">{{ event.outcome === 'penalized' ? t('citySpatial.realtime.character.history.penalized') : t('citySpatial.realtime.character.history.completed') }}</span>
                </li>
              </ol>
            </section>
          </div>

          <p v-if="characterActivityFeedback" class="realtime-pixel-character-feedback" aria-live="polite">{{ characterActivityFeedback }}</p>
          <p v-if="characterError" class="realtime-pixel-character-error" role="alert">{{ characterError }}</p>
        </section>

        <template v-if="selectedCell">
          <dl>
            <div>
              <dt>{{ t('citySpatial.realtime.inspector.coordinate') }}</dt>
              <dd>{{ selectedCell.worldX }} / {{ selectedCell.worldY }} / {{ selectedCell.z }}</dd>
            </div>
            <div>
              <dt>{{ t('citySpatial.realtime.inspector.chunk') }}</dt>
              <dd>{{ selectedCell.chunkX }} / {{ selectedCell.chunkY }}</dd>
            </div>
            <div>
              <dt>{{ t('citySpatial.realtime.inspector.terrain') }}</dt>
              <dd><code>{{ selectedCell.terrainDefinitionID }}</code></dd>
            </div>
          </dl>

          <section v-if="selectedBuilding" class="realtime-pixel-building">
            <span>{{ t('citySpatial.realtime.inspector.building') }}</span>
            <strong>{{ selectedBuilding.code }}</strong>
            <small>{{ selectedBuilding.primary_use }} · {{ selectedBuilding.floor_count }} {{ t('citySpatial.realtime.inspector.floors') }}</small>
          </section>

          <section v-if="selectedActor" class="realtime-pixel-actor">
            <span>{{ t('citySpatial.realtime.inspector.actor') }}</span>
            <strong>{{ selectedActor.public_label }}</strong>
            <small>{{ selectedActor.actor_kind }} · {{ selectedActor.motion_state }}</small>
            <code>{{ selectedActor.actor_code }}</code>
          </section>

          <section class="realtime-pixel-stack">
            <h3>{{ t('citySpatial.realtime.inspector.layers') }}</h3>
            <ol v-if="selectedCell.layers.length > 0">
              <li v-for="layer in selectedCell.layers" :key="`${layer.kind}/${layer.definition_id}`">
                <strong>{{ layer.kind }}</strong>
                <code>{{ layer.definition_id }}</code>
              </li>
            </ol>
            <p v-else>{{ t('citySpatial.realtime.inspector.noLayers') }}</p>
          </section>
        </template>

        <div v-else class="realtime-pixel-inspector-empty">
          <Icon name="grid" size="md" />
          <strong>{{ t('citySpatial.realtime.inspector.emptyTitle') }}</strong>
          <p>{{ t('citySpatial.realtime.inspector.emptyDescription') }}</p>
        </div>

        <footer v-if="projection">
          <span>{{ t('citySpatial.realtime.inspector.viewerScope') }}</span>
          <strong>{{ projection.viewer.membership_role }}</strong>
          <span>{{ t('citySpatial.realtime.inspector.staticHash') }}</span>
          <code :title="projection.static_projection_hash">{{ shortHash(projection.static_projection_hash) }}</code>
        </footer>
      </aside>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type {
  CityRealtimeClock,
  CityRealtimeActorSnapshot,
  CityRealtimeCharacterActivityAvailability,
  CityRealtimeCharacterActivityEvent,
  CityRealtimeCharacterAgentConfiguration,
  CityRealtimeCharacterArchetypeOption,
  CityRealtimeCharacterControlMode,
  CityRealtimeCharacterPersonalitySeed,
  CityRealtimeCharacterInteriorCell,
  CityRealtimeCharacterLocation,
  CityRealtimeCharacterMutationResult,
  CityRealtimeCharacterPortalTransition,
  CityRealtimeCharacterRoleAvailability,
  CityRealtimeCharacterRoleRequirements,
  CityRealtimeMyCharacterProjection,
  CityRealtimePixelChunkProjection,
  CityRealtimePublicActor,
  CityRealtimeSemanticBuilding,
  CityRealtimeSemanticLayer,
  CityRealtimeVisualManifest,
  CityRealtimeWorldProjection,
  CityWorld
} from '@/api/citySpatial'
import {
  getRealtimeClock,
  getRealtimeActors,
  getRealtimeMyCharacter,
  getRealtimePixelChunk,
  getRealtimeVisualManifest,
  getRealtimeWorldProjection,
  listRealtimeMyCharacterEvents,
  listRealtimePatches,
  createRealtimeCharacter,
  configureRealtimeCharacterAgent,
  changeRealtimeCharacterRole,
  moveRealtimeCharacter,
  traverseRealtimeCharacterPortal,
  performRealtimeCharacterActivity
} from '@/api/citySpatial'
import Icon from '@/components/icons/Icon.vue'
import {
  decodeRealtimeChunkPayload,
  floorDivide,
  realtimeChunkKey,
  type DecodedRealtimeChunkPayload
} from './realtimePixelProjection'
import {
  indexRealtimePublicActors,
  realtimeActorCellKey,
  resolveRealtimeActorSpritePalette
} from './realtimeActorProjection'
import {
  resolveRealtimeBuildingColor,
  resolveRealtimeLayerColor,
  resolveRealtimePixelPalette,
  resolveRealtimeTerrainColor
} from './realtimeVisualResolver'

interface CachedChunk {
  projection: CityRealtimePixelChunkProjection
  decoded: DecodedRealtimeChunkPayload
}

interface PixelSelection {
  worldX: number
  worldY: number
  z: number
  chunkX: number
  chunkY: number
  terrainDefinitionID: string
  layers: CityRealtimeSemanticLayer[]
}

interface InteriorGridTile {
  cell: CityRealtimeCharacterInteriorCell
  column: number
  row: number
}

const props = defineProps<{
  world: CityWorld
  worlds: CityWorld[]
}>()

const emit = defineEmits<{
  (event: 'select-world', worldID: number): void
}>()

const { t, locale } = useI18n()

const cellSizeSteps = [4, 6, 8, 10, 12, 16]
const canvasRef = ref<HTMLCanvasElement | null>(null)
const canvasHostRef = ref<HTMLDivElement | null>(null)
const projection = ref<CityRealtimeWorldProjection | null>(null)
const clock = ref<CityRealtimeClock | null>(null)
const visualManifest = ref<CityRealtimeVisualManifest | null>(null)
const actorSnapshot = ref<CityRealtimeActorSnapshot | null>(null)
const myCharacter = ref<CityRealtimeMyCharacterProjection | null>(null)
const characterLabel = ref('')
const selectedArchetypeCode = ref('')
const characterAgentTargetMode = ref<Extract<CityRealtimeCharacterControlMode, 'autonomous' | 'suspended'>>('autonomous')
const characterAgentValues = ref('')
const characterAgentBoundaries = ref('')
const characterAgentBackground = ref('')
const characterAgentNotes = ref('')
const characterAgentPersonalityDirty = ref(false)
const characterAgentControlDirty = ref(false)
const characterLoading = ref(true)
const characterBusy = ref(false)
const characterError = ref<string | null>(null)
const characterActivityFeedback = ref<string | null>(null)
const characterEvents = ref<CityRealtimeCharacterActivityEvent[]>([])
const initialLoading = ref(true)
const refreshing = ref(false)
const error = ref<string | null>(null)
const dragging = ref(false)
const selectedCell = ref<PixelSelection | null>(null)
const viewport = reactive({ width: 0, height: 0 })
const camera = reactive({ centerX: 0, centerY: 0, cellSize: 8 })
const cacheVersion = ref(0)
const pendingChunkCount = ref(0)

const chunks = new Map<string, CachedChunk>()
const buildings = new Map<string, CityRealtimeSemanticBuilding>()
const pendingChunks = new Set<string>()
const failedChunks = new Set<string>()
let worldRequestEpoch = 0
let renderFrame: number | null = null
let chunkLoadTimer: number | null = null
let actorLoadTimer: number | null = null
let patchTimer: number | null = null
let resizeObserver: ResizeObserver | null = null
let pointerID: number | null = null
let pointerLast = { x: 0, y: 0 }
let pointerMoved = false
let characterCreateAttempt: { label: string; archetypeCode: string; idempotencyKey: string } | null = null
let characterMoveAttempt: { x: number; y: number; z: number; idempotencyKey: string } | null = null
let characterPortalAttempt: { portalCode: string; idempotencyKey: string } | null = null
let characterActivityAttempt: { code: string; idempotencyKey: string } | null = null
let characterRoleAttempt: { code: string; idempotencyKey: string } | null = null
let characterAgentConfigureAttempt: { requestHash: string; idempotencyKey: string } | null = null

const worldOptions = computed(() => props.worlds.map(item => ({ id: item.id, name: item.name })))
const loadedChunkCount = computed(() => {
  cacheVersion.value
  return chunks.size
})
const selectedBuilding = computed(() => {
  cacheVersion.value
  const selected = selectedCell.value
  if (!selected) return null
  for (const building of buildings.values()) {
    if (building.footprint.some(point => point.x === selected.worldX && point.y === selected.worldY && point.z === selected.z)) {
      return building
    }
  }
  return null
})
const visibleActorCount = computed(() => actorSnapshot.value?.actors.length ?? 0)
const actorsByCell = computed(() => {
  const actors = actorSnapshot.value?.actors ?? []
  return indexRealtimePublicActors(actors)
})
const selectedActor = computed<CityRealtimePublicActor | null>(() => {
  const selected = selectedCell.value
  if (!selected) return null
  return actorsByCell.value.get(realtimeActorCellKey(selected.worldX, selected.worldY, selected.z))?.[0] ?? null
})
const characterAgent = computed<CityRealtimeCharacterAgentConfiguration | null>(() => myCharacter.value?.agent ?? null)
const characterManualControlsAvailable = computed(() => myCharacter.value?.character?.control_mode === 'manual')
const characterAgentValuesList = computed(() => splitCharacterAgentList(characterAgentValues.value))
const characterAgentBoundariesList = computed(() => splitCharacterAgentList(characterAgentBoundaries.value))
const characterAgentHasPersonalityInput = computed(() => (
  characterAgentValues.value.trim().length > 0 ||
  characterAgentBoundaries.value.trim().length > 0 ||
  characterAgentBackground.value.trim().length > 0 ||
  characterAgentNotes.value.trim().length > 0
))
const canMoveToSelectedCell = computed(() => {
  const character = myCharacter.value?.character
  const target = selectedCell.value
  if (!character || !target || character.z !== target.z || character.lifecycle_status !== 'active' || !characterManualControlsAvailable.value) return false
  return Math.abs(character.x - target.worldX) + Math.abs(character.y - target.worldY) === 1
})
const characterAgentQueueLabel = computed(() => {
  const agent = characterAgent.value
  if (!agent) return '—'
  if (agent.pending_intent) return t('citySpatial.realtime.character.agent.queueIntent')
  if (agent.pending_decision) return t('citySpatial.realtime.character.agent.queueDecision')
  return t('citySpatial.realtime.character.agent.queueIdle')
})
const canSubmitCharacterAgentConfiguration = computed(() => {
  const agent = characterAgent.value
  if (!agent?.autonomy_runtime_available) return false
  const controlChanged = characterAgentControlDirty.value && agent.control_mode !== characterAgentTargetMode.value
  if (!controlChanged && !characterAgentPersonalityDirty.value) return false
  if (characterAgentPersonalityDirty.value) {
    if (!characterAgentHasPersonalityInput.value || characterAgentValuesList.value.length === 0 ||
        characterAgentValuesList.value.length > 8 || characterAgentBoundariesList.value.length > 8 ||
        characterAgentListHasDuplicates(characterAgentValuesList.value) ||
        characterAgentListHasDuplicates(characterAgentBoundariesList.value)) return false
  }
  if (characterAgentTargetMode.value === 'autonomous' && !characterAgentPersonalityDirty.value && !agent.personality) return false
  return true
})
const characterLife = computed(() => myCharacter.value?.life ?? null)
const characterProgression = computed(() => characterLife.value?.progression ?? null)
const characterArchetypes = computed(() => myCharacter.value?.available_archetypes ?? [])
const selectedArchetype = computed<CityRealtimeCharacterArchetypeOption | null>(() => (
  characterArchetypes.value.find(item => item.code === selectedArchetypeCode.value) ?? null
))
const characterTotalExperience = computed(() => (
  characterProgression.value?.attributes.reduce((total, attribute) => total + attribute.experience_units, 0) ?? 0
))
const characterActivities = computed(() => myCharacter.value?.available_activities ?? [])
const characterPortals = computed(() => myCharacter.value?.available_portals ?? [])
const characterInterior = computed(() => myCharacter.value?.current_interior ?? null)
const interiorGrid = computed(() => {
  const cells = characterInterior.value?.cells ?? []
  if (cells.length === 0 || !cells.every(cell => Number.isSafeInteger(cell.x) && Number.isSafeInteger(cell.y))) {
    return { width: 0, cells: [] as InteriorGridTile[] }
  }
  const minimumX = Math.min(...cells.map(cell => cell.x))
  const maximumX = Math.max(...cells.map(cell => cell.x))
  const minimumY = Math.min(...cells.map(cell => cell.y))
  const maximumY = Math.max(...cells.map(cell => cell.y))
  const width = maximumX - minimumX + 1
  const height = maximumY - minimumY + 1
  if (!Number.isSafeInteger(width) || !Number.isSafeInteger(height) || width <= 0 || height <= 0 || width > 64 || height > 64) {
    return { width: 0, cells: [] as InteriorGridTile[] }
  }
  const gridCells = cells.map(cell => ({
    cell,
    column: cell.x - minimumX + 1,
    row: cell.y - minimumY + 1
  }))
  gridCells.sort((left, right) => left.row - right.row || left.column - right.column)
  return { width, cells: gridCells }
})
const characterRationQuantity = computed(() => characterLife.value?.inventory.find(item => item.item_code === 'item.food.ration')?.quantity ?? 0)
const characterNeeds = computed(() => {
  const life = characterLife.value
  if (!life) return []
  return [
    { code: 'energy', label: t('citySpatial.realtime.character.life.energy'), value: life.energy_milli },
    { code: 'satiety', label: t('citySpatial.realtime.character.life.satiety'), value: life.satiety_milli },
    { code: 'morale', label: t('citySpatial.realtime.character.life.morale'), value: life.morale_milli },
    { code: 'standing', label: t('citySpatial.realtime.character.life.standing'), value: life.civic_standing_milli }
  ]
})
const formattedWorldTime = computed(() => {
  const raw = clock.value?.world_time.local_time
  if (!raw) return '—'
  const date = new Date(raw)
  if (Number.isNaN(date.getTime())) return raw
  return new Intl.DateTimeFormat(locale.value, {
    dateStyle: 'medium', timeStyle: 'medium', timeZone: clock.value?.world_time.timezone
  }).format(date)
})
const activePalette = computed(() => resolveRealtimePixelPalette(
  visualManifest.value?.manifest,
  projection.value?.spatial.profile_id
))
const visualPackLabel = computed(() => {
  const visual = projection.value?.visual
  if (!visual) return shortHash(projection.value?.static_projection_hash)
  return `${visual.pack_id}@${visual.pack_version} · ${shortHash(visual.manifest_hash)}`
})

function shortHash(value: string | undefined): string {
  if (!value) return '—'
  return `${value.slice(0, 8)}…${value.slice(-6)}`
}

function newCharacterIdempotencyKey(operation: 'create' | 'move' | 'portal' | 'activity' | 'role' | 'agent'): string {
  const id = globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random().toString(36).slice(2)}`
  return `realtime-character-${operation}-${props.world.id}-${id}`
}

function splitCharacterAgentList(value: string): string[] {
  return value
    .split(/[\n,]/u)
    .map(item => item.trim())
    .filter(Boolean)
}

function characterAgentListHasDuplicates(items: string[]): boolean {
  return new Set(items).size !== items.length
}

function characterAgentModeLabel(mode: CityRealtimeCharacterControlMode): string {
  const knownModes: CityRealtimeCharacterControlMode[] = ['manual', 'assisted', 'autonomous', 'suspended']
  return knownModes.includes(mode) ? t(`citySpatial.realtime.character.agent.modes.${mode}`) : mode
}

function synchronizeCharacterAgentForm(item: CityRealtimeMyCharacterProjection): void {
  const agent = item.agent
  if (!agent) return
  if (!characterAgentControlDirty.value) {
    characterAgentTargetMode.value = agent.control_mode === 'suspended' ? 'suspended' : 'autonomous'
  }
  if (characterAgentPersonalityDirty.value) return
  const seed = agent.personality?.seed
  if (!seed) {
    characterAgentValues.value = ''
    characterAgentBoundaries.value = ''
    characterAgentBackground.value = ''
    characterAgentNotes.value = ''
    return
  }
  characterAgentValues.value = seed.values.join(', ')
  characterAgentBoundaries.value = seed.hard_boundaries.join(', ')
  characterAgentBackground.value = seed.background
  characterAgentNotes.value = seed.freeform_notes
}

function characterAgentPersonalityFromForm(): CityRealtimeCharacterPersonalitySeed | null {
  if (!characterAgentPersonalityDirty.value) return null
  const values = characterAgentValuesList.value
  const boundaries = characterAgentBoundariesList.value
  if (values.length === 0 || values.length > 8 || boundaries.length > 8 ||
      characterAgentListHasDuplicates(values) || characterAgentListHasDuplicates(boundaries)) return null
  return {
    values,
    preferences: {},
    background: characterAgentBackground.value.trim(),
    hard_boundaries: boundaries,
    freeform_notes: characterAgentNotes.value.trim()
  }
}

function formatCityCredit(value: number): string {
  return new Intl.NumberFormat(locale.value, { maximumFractionDigits: 0 }).format(value)
}

function activityLabel(code: string): string {
  const keyByCode: Record<string, string> = {
    'rest.short': 'restShort',
    'work.civic_shift': 'civicShift',
    'consume.ration': 'consumeRation',
    'civic.cleanup': 'civicCleanup',
    'conduct.disruption': 'conductDisruption',
    'study.public_service': 'publicServiceStudy',
    'work.civic_service': 'civicService',
    'work.maintenance_shift': 'maintenanceShift'
  }
  const key = keyByCode[code]
  return key ? t(`citySpatial.realtime.character.activities.labels.${key}`) : code
}

function attributeLabel(code: string): string {
  const keyByCode: Record<string, string> = {
    communication: 'communication', coordination: 'coordination', discipline: 'discipline',
    reasoning: 'reasoning', vitality: 'vitality'
  }
  const key = keyByCode[code]
  return key ? t(`citySpatial.realtime.character.progression.attributes.${key}`) : code
}

function archetypeLabel(code: string): string {
  const keyByCode: Record<string, string> = {
    'resident.generalist': 'residentGeneralist',
    'resident.social': 'residentSocial',
    'resident.technical': 'residentTechnical'
  }
  const key = keyByCode[code]
  return key ? t(`citySpatial.realtime.character.progression.archetypes.${key}`) : code
}

function roleLabel(code: string): string {
  const keyByCode: Record<string, string> = {
    'profession.resident': 'resident',
    'profession.civic_aide': 'civicAide',
    'profession.maintenance_worker': 'maintenanceWorker',
    'profession.community_steward': 'communitySteward'
  }
  const key = keyByCode[code]
  return key ? t(`citySpatial.realtime.character.progression.roles.${key}`) : code
}

function roleRequirementSummary(requirements: CityRealtimeCharacterRoleRequirements): string {
  const parts: string[] = []
  if (requirements.minimum_civic_standing_milli) {
    parts.push(t('citySpatial.realtime.character.progression.requirements.standing', {
      value: requirements.minimum_civic_standing_milli
    }))
  }
  if (requirements.minimum_total_experience_units) {
    parts.push(t('citySpatial.realtime.character.progression.requirements.experience', {
      value: requirements.minimum_total_experience_units
    }))
  }
  for (const requirement of requirements.attributes ?? []) {
    parts.push(t('citySpatial.realtime.character.progression.requirements.attribute', {
      attribute: attributeLabel(requirement.attribute_code), value: requirement.minimum_value_milli
    }))
  }
  for (const requiredRole of requirements.required_role_codes ?? []) {
    parts.push(t('citySpatial.realtime.character.progression.requirements.role', { role: roleLabel(requiredRole) }))
  }
  return parts.join(' · ') || t('citySpatial.realtime.character.progression.requirements.none')
}

function roleAvailabilityDescription(role: CityRealtimeCharacterRoleAvailability): string {
  if (role.available) return t('citySpatial.realtime.character.progression.available')
  if (role.reason_code === 'active') return t('citySpatial.realtime.character.progression.active')
  const reasonKey = role.reason_code ?? 'requirements'
  return t(`citySpatial.realtime.character.progression.unavailable.${reasonKey}`, {
    requirements: roleRequirementSummary(role.requirements)
  })
}

function portalDirectionLabel(direction: CityRealtimeCharacterPortalTransition['direction']): string {
  return t(`citySpatial.realtime.character.portals.directions.${direction}`)
}

function interiorCellKindLabel(cell: CityRealtimeCharacterInteriorCell): string {
  return t(`citySpatial.realtime.character.interior.kinds.${cell.kind}`)
}

function interiorCellGlyph(cell: CityRealtimeCharacterInteriorCell): string {
  if (cell.feature === 'stairs') return '⇅'
  const glyphs: Record<CityRealtimeCharacterInteriorCell['kind'], string> = {
    wall: '█',
    window: '░',
    floor: '·',
    door: '+',
    furniture: '▣'
  }
  return glyphs[cell.kind]
}

function interiorCellDescription(cell: CityRealtimeCharacterInteriorCell): string {
  return t('citySpatial.realtime.character.interior.cellDescription', {
    kind: interiorCellKindLabel(cell),
    x: cell.x,
    y: cell.y,
    z: cell.z
  })
}

function isCurrentInteriorCell(cell: CityRealtimeCharacterInteriorCell): boolean {
  const character = myCharacter.value?.character
  return character?.x === cell.x && character.y === cell.y && character.z === cell.z
}

function canMoveToInteriorCell(cell: CityRealtimeCharacterInteriorCell): boolean {
  const character = myCharacter.value?.character
  if (!character || !characterManualControlsAvailable.value || character.lifecycle_status !== 'active' || !cell.traversable || character.z !== cell.z) return false
  return Math.abs(character.x - cell.x) + Math.abs(character.y - cell.y) === 1
}

function activityAvailabilityDescription(activity: CityRealtimeCharacterActivityAvailability): string {
  if (activity.available) return t('citySpatial.realtime.character.activities.available')
  if (activity.reason_code === 'cooldown') {
    return t('citySpatial.realtime.character.activities.cooldown', {
      seconds: Math.max(1, Math.ceil((activity.cooldown_remaining_us ?? 0) / 1_000_000))
    })
  }
  if (activity.reason_code === 'location') return t('citySpatial.realtime.character.activities.locationRequired')
  if (activity.reason_code === 'inventory') return t('citySpatial.realtime.character.activities.inventoryRequired')
  if (activity.reason_code === 'needs') return t('citySpatial.realtime.character.activities.needsRequired')
  if (activity.reason_code === 'role') return t('citySpatial.realtime.character.activities.roleRequired')
  if (activity.reason_code === 'progression') return t('citySpatial.realtime.character.activities.progressionRequired')
  return t('citySpatial.realtime.character.activities.unavailable')
}

function activityOutcomeDescription(result: CityRealtimeCharacterMutationResult): string | null {
  const activity = result.activity
  if (!activity) return null
  const label = activityLabel(activity.code)
  if (activity.outcome === 'penalized') {
    return t('citySpatial.realtime.character.activities.penalized', {
      activity: label,
      amount: Math.abs(activity.city_credit_delta_units)
    })
  }
  if ((activity.experience_deltas?.length ?? 0) > 0) {
    return t('citySpatial.realtime.character.activities.completedWithExperience', {
      activity: label,
      experience: activity.experience_deltas!.map(delta => `${attributeLabel(delta.attribute_code)} +${delta.experience_units}`).join(' · ')
    })
  }
  return t('citySpatial.realtime.character.activities.completed', { activity: label })
}

function roleChangeOutcomeDescription(result: CityRealtimeCharacterMutationResult): string | null {
  const roleChange = result.role_change
  if (!roleChange) return null
  return t('citySpatial.realtime.character.progression.changed', {
    from: roleChange.from_role_code ? roleLabel(roleChange.from_role_code) : t('citySpatial.realtime.character.progression.none'),
    to: roleLabel(roleChange.to_role_code)
  })
}

function synchronizeArchetypeSelection(item: CityRealtimeMyCharacterProjection): void {
  if (item.exists) return
  const options = item.available_archetypes ?? []
  if (options.length === 0) {
    selectedArchetypeCode.value = ''
    return
  }
  if (!options.some(option => option.code === selectedArchetypeCode.value)) {
    selectedArchetypeCode.value = options[0].code
  }
}

async function loadMyCharacterEvents(epoch: number): Promise<void> {
  const worldID = props.world.id
  try {
    const page = await listRealtimeMyCharacterEvents(worldID, { limit: 12 })
    if (!isCurrentEpoch(epoch)) return
    characterEvents.value = page.items
  } catch {
    // The activity controls remain usable if an optional history read is
    // transiently unavailable. Never clear an already verified timeline.
  }
}

async function loadMyCharacter(epoch: number, showLoading = true): Promise<void> {
  const worldID = props.world.id
  if (showLoading) characterLoading.value = true
  characterError.value = null
  try {
    const item = await getRealtimeMyCharacter(worldID)
    if (!isCurrentEpoch(epoch) || item.world_id !== worldID) return
    myCharacter.value = item
    synchronizeArchetypeSelection(item)
    synchronizeCharacterAgentForm(item)
    if (item.exists) void loadMyCharacterEvents(epoch)
    else characterEvents.value = []
  } catch (caught) {
    if (!isCurrentEpoch(epoch)) return
    characterError.value = caught instanceof Error ? caught.message : t('citySpatial.realtime.character.loadFailed')
  } finally {
    if (showLoading && isCurrentEpoch(epoch)) characterLoading.value = false
  }
}

async function refreshMyCharacterAfterMutation(epoch: number): Promise<void> {
  const worldID = props.world.id
  try {
    const item = await getRealtimeMyCharacter(worldID)
    if (!isCurrentEpoch(epoch) || item.world_id !== worldID) return
    myCharacter.value = item
    synchronizeArchetypeSelection(item)
    synchronizeCharacterAgentForm(item)
    if (item.exists) await loadMyCharacterEvents(epoch)
  } catch {
    // The mutation response remains authoritative for the immediately visible
    // owner state. A later regular refresh reconciles availability if needed.
  }
}

function applyCharacterMutation(result: CityRealtimeCharacterMutationResult): void {
  const currentProjection = projection.value
  if (!currentProjection || result.frame.world_id !== currentProjection.world_id) return
  const previousCharacter = myCharacter.value
  myCharacter.value = {
    world_id: currentProjection.world_id,
    timeline_frame_sequence: result.frame.frame_sequence,
    timeline_cursor: result.frame.timeline_cursor,
    runtime_ready: true,
    exists: true,
    character: result.character,
    life: result.life ?? previousCharacter?.life,
    agent: result.agent ?? previousCharacter?.agent,
    available_archetypes: previousCharacter?.available_archetypes ?? [],
    available_activities: previousCharacter?.available_activities ?? [],
    available_portals: previousCharacter?.available_portals ?? [],
    current_interior: previousCharacter?.current_interior
  }
  if (myCharacter.value) synchronizeCharacterAgentForm(myCharacter.value)
  if (result.frame.frame_sequence >= currentProjection.timeline_frame_sequence) {
    projection.value = {
      ...currentProjection,
      timeline_frame_sequence: result.frame.frame_sequence,
      timeline_cursor: result.frame.timeline_cursor
    }
  }
  const currentActors = actorSnapshot.value
  if (currentActors) {
    const actor: CityRealtimePublicActor = {
      actor_code: result.character.actor_code,
      actor_kind: 'character',
      public_label: result.character.public_label,
      appearance_variant: result.character.appearance_variant,
      lifecycle_status: result.character.lifecycle_status,
      x: result.character.x,
      y: result.character.y,
      z: result.character.z,
      motion_state: result.character.motion_state,
      position_revision: result.character.position_revision,
      last_frame_sequence: result.character.last_frame_sequence
    }
    const actors = currentActors.actors.filter(item => item.actor_code !== actor.actor_code)
    actors.push(actor)
    actorSnapshot.value = {
      ...currentActors,
      timeline_frame_sequence: result.frame.frame_sequence,
      timeline_cursor: result.frame.timeline_cursor,
      actors
    }
  }
  scheduleRender()
  void ensureVisibleActors(worldRequestEpoch)
}

async function createMyCharacter(): Promise<void> {
  const currentProjection = projection.value
  const label = characterLabel.value.trim()
  const archetypeCode = selectedArchetypeCode.value
  if (!currentProjection || !label || characterBusy.value || !myCharacter.value?.runtime_ready || myCharacter.value.exists ||
      (characterArchetypes.value.length > 0 && !archetypeCode)) return
  if (!characterCreateAttempt || characterCreateAttempt.label !== label || characterCreateAttempt.archetypeCode !== archetypeCode) {
    characterCreateAttempt = { label, archetypeCode, idempotencyKey: newCharacterIdempotencyKey('create') }
  }
  const attempt = characterCreateAttempt
  const epoch = worldRequestEpoch
  characterBusy.value = true
  characterError.value = null
  characterActivityFeedback.value = null
  try {
    const result = await createRealtimeCharacter(currentProjection.world_id, {
      public_label: label,
      ...(archetypeCode ? { archetype_code: archetypeCode } : {})
    }, attempt.idempotencyKey)
    if (!isCurrentEpoch(epoch)) return
    applyCharacterMutation(result)
    characterLabel.value = result.character.public_label
    characterCreateAttempt = null
    await refreshMyCharacterAfterMutation(epoch)
  } catch (caught) {
    if (isCurrentEpoch(epoch)) characterError.value = caught instanceof Error ? caught.message : t('citySpatial.realtime.character.createFailed')
  } finally {
    if (isCurrentEpoch(epoch)) characterBusy.value = false
  }
}

async function configureMyCharacterAgent(): Promise<void> {
  const currentProjection = projection.value
  const agent = characterAgent.value
  if (!currentProjection || !agent || characterBusy.value || !canSubmitCharacterAgentConfiguration.value) return
  const personality = characterAgentPersonalityFromForm()
  if (characterAgentPersonalityDirty.value && !personality) {
    characterError.value = t('citySpatial.realtime.character.agent.personalityInvalid')
    return
  }
  if (characterAgentTargetMode.value === 'autonomous' && !personality && !agent.personality) {
    characterError.value = t('citySpatial.realtime.character.agent.personalityRequired')
    return
  }
  const request = {
    control_mode: characterAgentTargetMode.value,
    ...(personality ? { personality } : {})
  }
  const requestHash = JSON.stringify(request)
  if (!characterAgentConfigureAttempt || characterAgentConfigureAttempt.requestHash !== requestHash) {
    characterAgentConfigureAttempt = {
      requestHash,
      idempotencyKey: newCharacterIdempotencyKey('agent')
    }
  }
  const attempt = characterAgentConfigureAttempt
  const epoch = worldRequestEpoch
  characterBusy.value = true
  characterError.value = null
  characterActivityFeedback.value = null
  try {
    const result = await configureRealtimeCharacterAgent(currentProjection.world_id, request, attempt.idempotencyKey)
    if (!isCurrentEpoch(epoch)) return
    applyCharacterMutation(result)
    characterAgentPersonalityDirty.value = false
    characterAgentControlDirty.value = false
    characterAgentConfigureAttempt = null
    characterActivityFeedback.value = t('citySpatial.realtime.character.agent.saved', {
      mode: characterAgentModeLabel(result.agent?.control_mode ?? result.character.control_mode)
    })
    await refreshMyCharacterAfterMutation(epoch)
  } catch (caught) {
    if (isCurrentEpoch(epoch)) characterError.value = caught instanceof Error ? caught.message : t('citySpatial.realtime.character.agent.saveFailed')
  } finally {
    if (isCurrentEpoch(epoch)) characterBusy.value = false
  }
}

async function moveMyCharacter(): Promise<void> {
  const target = selectedCell.value
  if (!target) return
  await moveMyCharacterTo({ x: target.worldX, y: target.worldY, z: target.z })
}

async function moveMyCharacterTo(target: CityRealtimeCharacterLocation): Promise<void> {
  const currentProjection = projection.value
  const character = myCharacter.value?.character
  if (!currentProjection || !character || characterBusy.value || !characterManualControlsAvailable.value || character.lifecycle_status !== 'active' ||
      character.z !== target.z || Math.abs(character.x - target.x) + Math.abs(character.y - target.y) !== 1) return
  if (!characterMoveAttempt || characterMoveAttempt.x !== target.x || characterMoveAttempt.y !== target.y || characterMoveAttempt.z !== target.z) {
    characterMoveAttempt = {
      x: target.x,
      y: target.y,
      z: target.z,
      idempotencyKey: newCharacterIdempotencyKey('move')
    }
  }
  const attempt = characterMoveAttempt
  const epoch = worldRequestEpoch
  characterBusy.value = true
  characterError.value = null
  characterActivityFeedback.value = null
  try {
    const result = await moveRealtimeCharacter(currentProjection.world_id, {
      x: target.x, y: target.y, z: target.z
    }, attempt.idempotencyKey)
    if (!isCurrentEpoch(epoch)) return
    applyCharacterMutation(result)
    characterMoveAttempt = null
    await refreshMyCharacterAfterMutation(epoch)
  } catch (caught) {
    if (isCurrentEpoch(epoch)) characterError.value = caught instanceof Error ? caught.message : t('citySpatial.realtime.character.moveFailed')
  } finally {
    if (isCurrentEpoch(epoch)) characterBusy.value = false
  }
}

async function traverseMyCharacterPortal(portal: CityRealtimeCharacterPortalTransition): Promise<void> {
  const currentProjection = projection.value
  const character = myCharacter.value?.character
  if (!currentProjection || !character || characterBusy.value || !characterManualControlsAvailable.value || !characterPortals.value.some(item => item.portal_code === portal.portal_code)) return
  if (!characterPortalAttempt || characterPortalAttempt.portalCode !== portal.portal_code) {
    characterPortalAttempt = { portalCode: portal.portal_code, idempotencyKey: newCharacterIdempotencyKey('portal') }
  }
  const attempt = characterPortalAttempt
  const epoch = worldRequestEpoch
  characterBusy.value = true
  characterError.value = null
  characterActivityFeedback.value = null
  try {
    const result = await traverseRealtimeCharacterPortal(
      currentProjection.world_id,
      { portal_code: portal.portal_code },
      attempt.idempotencyKey
    )
    if (!isCurrentEpoch(epoch)) return
    applyCharacterMutation(result)
    characterActivityFeedback.value = t('citySpatial.realtime.character.portals.completed', {
      direction: portalDirectionLabel(portal.direction)
    })
    characterPortalAttempt = null
    await refreshMyCharacterAfterMutation(epoch)
  } catch (caught) {
    if (isCurrentEpoch(epoch)) characterError.value = caught instanceof Error ? caught.message : t('citySpatial.realtime.character.portals.failed')
  } finally {
    if (isCurrentEpoch(epoch)) characterBusy.value = false
  }
}

async function performMyCharacterActivity(activityCode: string): Promise<void> {
  const currentProjection = projection.value
  const activity = characterActivities.value.find(item => item.code === activityCode)
  if (!currentProjection || !activity || !activity.available || characterBusy.value || !characterManualControlsAvailable.value || !myCharacter.value?.character) return
  if (!characterActivityAttempt || characterActivityAttempt.code !== activityCode) {
    characterActivityAttempt = { code: activityCode, idempotencyKey: newCharacterIdempotencyKey('activity') }
  }
  const attempt = characterActivityAttempt
  const epoch = worldRequestEpoch
  characterBusy.value = true
  characterError.value = null
  characterActivityFeedback.value = null
  try {
    const result = await performRealtimeCharacterActivity(
      currentProjection.world_id,
      { activity_code: activityCode },
      attempt.idempotencyKey
    )
    if (!isCurrentEpoch(epoch)) return
    applyCharacterMutation(result)
    characterActivityFeedback.value = activityOutcomeDescription(result)
    characterActivityAttempt = null
    await refreshMyCharacterAfterMutation(epoch)
  } catch (caught) {
    if (isCurrentEpoch(epoch)) characterError.value = caught instanceof Error ? caught.message : t('citySpatial.realtime.character.activities.failed')
  } finally {
    if (isCurrentEpoch(epoch)) characterBusy.value = false
  }
}

async function changeMyCharacterRole(roleCode: string): Promise<void> {
  const currentProjection = projection.value
  const role = characterProgression.value?.available_roles.find(item => item.code === roleCode)
  if (!currentProjection || !role?.available || characterBusy.value || !characterManualControlsAvailable.value || !myCharacter.value?.character) return
  if (!characterRoleAttempt || characterRoleAttempt.code !== roleCode) {
    characterRoleAttempt = { code: roleCode, idempotencyKey: newCharacterIdempotencyKey('role') }
  }
  const attempt = characterRoleAttempt
  const epoch = worldRequestEpoch
  characterBusy.value = true
  characterError.value = null
  characterActivityFeedback.value = null
  try {
    const result = await changeRealtimeCharacterRole(
      currentProjection.world_id,
      { role_code: roleCode },
      attempt.idempotencyKey
    )
    if (!isCurrentEpoch(epoch)) return
    applyCharacterMutation(result)
    characterActivityFeedback.value = roleChangeOutcomeDescription(result)
    characterRoleAttempt = null
    await refreshMyCharacterAfterMutation(epoch)
  } catch (caught) {
    if (isCurrentEpoch(epoch)) characterError.value = caught instanceof Error ? caught.message : t('citySpatial.realtime.character.progression.changeFailed')
  } finally {
    if (isCurrentEpoch(epoch)) characterBusy.value = false
  }
}

function isCurrentEpoch(epoch: number): boolean {
  return epoch === worldRequestEpoch && projection.value?.world_id === props.world.id
}

function currentBounds(): {
  minimumChunkX: number
  maximumChunkX: number
  minimumChunkY: number
  maximumChunkY: number
} | null {
  const spatial = projection.value?.spatial
  if (!spatial || !Number.isInteger(spatial.chunk_size) || spatial.chunk_size <= 0 ||
      !Number.isInteger(spatial.sector_size_chunks) || spatial.sector_size_chunks <= 0) return null
  return {
    minimumChunkX: spatial.spawn_sector_x * spatial.sector_size_chunks,
    maximumChunkX: spatial.spawn_sector_x * spatial.sector_size_chunks + spatial.sector_size_chunks - 1,
    minimumChunkY: spatial.spawn_sector_y * spatial.sector_size_chunks,
    maximumChunkY: spatial.spawn_sector_y * spatial.sector_size_chunks + spatial.sector_size_chunks - 1
  }
}

function clampCamera(): void {
  const spatial = projection.value?.spatial
  const bounds = currentBounds()
  if (!spatial || !bounds || viewport.width <= 0 || viewport.height <= 0) return
  const halfWidth = Math.floor(viewport.width / Math.max(1, camera.cellSize) / 2)
  const halfHeight = Math.floor(viewport.height / Math.max(1, camera.cellSize) / 2)
  const minimumWorldX = bounds.minimumChunkX * spatial.chunk_size
  const maximumWorldX = (bounds.maximumChunkX + 1) * spatial.chunk_size - 1
  const minimumWorldY = bounds.minimumChunkY * spatial.chunk_size
  const maximumWorldY = (bounds.maximumChunkY + 1) * spatial.chunk_size - 1
  const centerXMinimum = minimumWorldX + halfWidth
  const centerXMaximum = maximumWorldX - halfWidth
  const centerYMinimum = minimumWorldY + halfHeight
  const centerYMaximum = maximumWorldY - halfHeight
  camera.centerX = centerXMinimum > centerXMaximum
    ? Math.floor((minimumWorldX + maximumWorldX) / 2)
    : Math.max(centerXMinimum, Math.min(centerXMaximum, Math.round(camera.centerX)))
  camera.centerY = centerYMinimum > centerYMaximum
    ? Math.floor((minimumWorldY + maximumWorldY) / 2)
    : Math.max(centerYMinimum, Math.min(centerYMaximum, Math.round(camera.centerY)))
}

function resetCamera(): void {
  const spatial = projection.value?.spatial
  if (!spatial) return
  camera.centerX = spatial.spawn_x
  camera.centerY = spatial.spawn_y
  clampCamera()
  scheduleRender()
  scheduleVisibleChunkLoad()
  scheduleVisibleActorLoad()
}

function selectWorld(event: Event): void {
  const target = event.target as HTMLSelectElement
  const worldID = Number(target.value)
  if (Number.isSafeInteger(worldID) && worldID > 0 && worldID !== props.world.id) emit('select-world', worldID)
}

function changeZoom(direction: number): void {
  const currentIndex = cellSizeSteps.indexOf(camera.cellSize)
  const nextIndex = Math.max(0, Math.min(cellSizeSteps.length - 1, currentIndex + Math.sign(direction)))
  camera.cellSize = cellSizeSteps[nextIndex]
  clampCamera()
  scheduleRender()
  scheduleVisibleChunkLoad()
  scheduleVisibleActorLoad()
}

function scheduleRender(): void {
  if (renderFrame !== null) return
  renderFrame = requestAnimationFrame(() => {
    renderFrame = null
    drawScene()
  })
}

function scheduleVisibleChunkLoad(): void {
  if (chunkLoadTimer !== null) window.clearTimeout(chunkLoadTimer)
  chunkLoadTimer = window.setTimeout(() => {
    chunkLoadTimer = null
    void ensureVisibleChunks(worldRequestEpoch)
  }, 80)
}

function scheduleVisibleActorLoad(): void {
  if (actorLoadTimer !== null) window.clearTimeout(actorLoadTimer)
  actorLoadTimer = window.setTimeout(() => {
    actorLoadTimer = null
    void ensureVisibleActors(worldRequestEpoch)
  }, 120)
}

function visibleChunkCoordinates(): Array<{ x: number; y: number }> {
  const spatial = projection.value?.spatial
  const bounds = currentBounds()
  if (!spatial || !bounds || viewport.width <= 0 || viewport.height <= 0) return []
  const halfWidth = viewport.width / Math.max(1, camera.cellSize) / 2
  const halfHeight = viewport.height / Math.max(1, camera.cellSize) / 2
  const minimumX = floorDivide(Math.floor(camera.centerX - halfWidth), spatial.chunk_size)
  const maximumX = floorDivide(Math.ceil(camera.centerX + halfWidth), spatial.chunk_size)
  const minimumY = floorDivide(Math.floor(camera.centerY - halfHeight), spatial.chunk_size)
  const maximumY = floorDivide(Math.ceil(camera.centerY + halfHeight), spatial.chunk_size)
  const centerChunkX = floorDivide(camera.centerX, spatial.chunk_size)
  const centerChunkY = floorDivide(camera.centerY, spatial.chunk_size)
  const result: Array<{ x: number; y: number }> = []
  for (let y = Math.max(bounds.minimumChunkY, minimumY - 1); y <= Math.min(bounds.maximumChunkY, maximumY + 1); y++) {
    for (let x = Math.max(bounds.minimumChunkX, minimumX - 1); x <= Math.min(bounds.maximumChunkX, maximumX + 1); x++) {
      result.push({ x, y })
    }
  }
  return result.sort((left, right) => {
    const leftDistance = Math.abs(left.x - centerChunkX) + Math.abs(left.y - centerChunkY)
    const rightDistance = Math.abs(right.x - centerChunkX) + Math.abs(right.y - centerChunkY)
    return leftDistance - rightDistance || left.y - right.y || left.x - right.x
  })
}

function visibleActorWindow(): {
  minChunkX: number
  maxChunkX: number
  minChunkY: number
  maxChunkY: number
} | null {
  const coordinates = visibleChunkCoordinates()
  if (coordinates.length === 0) return null
  return coordinates.reduce((window, coordinate) => ({
    minChunkX: Math.min(window.minChunkX, coordinate.x),
    maxChunkX: Math.max(window.maxChunkX, coordinate.x),
    minChunkY: Math.min(window.minChunkY, coordinate.y),
    maxChunkY: Math.max(window.maxChunkY, coordinate.y)
  }), {
    minChunkX: coordinates[0].x,
    maxChunkX: coordinates[0].x,
    minChunkY: coordinates[0].y,
    maxChunkY: coordinates[0].y
  })
}

async function ensureVisibleChunks(epoch: number): Promise<void> {
  const projectionValue = projection.value
  if (!projectionValue || !isCurrentEpoch(epoch)) return
  const candidates = visibleChunkCoordinates().filter(({ x, y }) => {
    const key = realtimeChunkKey(x, y)
    return !chunks.has(key) && !pendingChunks.has(key) && !failedChunks.has(key)
  }).slice(0, 12)
  if (candidates.length === 0) return

  for (const coordinate of candidates) pendingChunks.add(realtimeChunkKey(coordinate.x, coordinate.y))
  pendingChunkCount.value = pendingChunks.size
  await Promise.all(candidates.map(async ({ x, y }) => {
    const key = realtimeChunkKey(x, y)
    try {
      const item = await getRealtimePixelChunk(projectionValue.world_id, x, y)
      if (!isCurrentEpoch(epoch)) return
      if (item.static_projection_hash !== projectionValue.static_projection_hash) {
        void bootstrapWorld()
        return
      }
      chunks.set(key, { projection: item, decoded: decodeRealtimeChunkPayload(item.chunk.payload) })
      for (const building of item.buildings) buildings.set(building.code, building)
    } catch {
      if (isCurrentEpoch(epoch)) failedChunks.add(key)
    } finally {
      pendingChunks.delete(key)
      if (isCurrentEpoch(epoch)) pendingChunkCount.value = pendingChunks.size
    }
  }))
  if (!isCurrentEpoch(epoch)) return
  cacheVersion.value++
  scheduleRender()
  if (visibleChunkCoordinates().some(({ x, y }) => {
    const key = realtimeChunkKey(x, y)
    return !chunks.has(key) && !pendingChunks.has(key) && !failedChunks.has(key)
  })) scheduleVisibleChunkLoad()
}

async function ensureVisibleActors(epoch: number): Promise<void> {
  const projectionValue = projection.value
  const window = visibleActorWindow()
  if (!projectionValue || !window || !isCurrentEpoch(epoch)) return
  try {
    const item = await getRealtimeActors(projectionValue.world_id, {
      min_chunk_x: window.minChunkX,
      max_chunk_x: window.maxChunkX,
      min_chunk_y: window.minChunkY,
      max_chunk_y: window.maxChunkY,
      z: 0,
      limit: 128
    })
    if (!isCurrentEpoch(epoch) || item.world_id !== projectionValue.world_id) return
    if (item.static_projection_hash !== projectionValue.static_projection_hash) {
      void bootstrapWorld()
      return
    }
    indexRealtimePublicActors(item.actors)
    actorSnapshot.value = item
    if (item.timeline_frame_sequence > projectionValue.timeline_frame_sequence) {
      projection.value = {
        ...projectionValue,
        timeline_frame_sequence: item.timeline_frame_sequence,
        timeline_cursor: item.timeline_cursor
      }
    }
    scheduleRender()
  } catch {
    // Preserve the last verified actor overlay if a transient content-plane
    // request fails. The static map remains independently usable.
  }
}

async function bootstrapWorld(): Promise<void> {
  const epoch = ++worldRequestEpoch
  initialLoading.value = true
  error.value = null
  selectedCell.value = null
  actorSnapshot.value = null
  myCharacter.value = null
  characterLabel.value = ''
  selectedArchetypeCode.value = ''
  characterError.value = null
  characterActivityFeedback.value = null
  characterEvents.value = []
  characterAgentTargetMode.value = 'autonomous'
  characterAgentValues.value = ''
  characterAgentBoundaries.value = ''
  characterAgentBackground.value = ''
  characterAgentNotes.value = ''
  characterAgentPersonalityDirty.value = false
  characterAgentControlDirty.value = false
  characterLoading.value = true
  characterBusy.value = false
  characterCreateAttempt = null
  characterMoveAttempt = null
  characterPortalAttempt = null
  characterActivityAttempt = null
  characterRoleAttempt = null
  characterAgentConfigureAttempt = null
  chunks.clear()
  buildings.clear()
  pendingChunks.clear()
  failedChunks.clear()
  pendingChunkCount.value = 0
  cacheVersion.value++
  scheduleRender()
  try {
    const [nextProjection, nextClock] = await Promise.all([
      getRealtimeWorldProjection(props.world.id),
      getRealtimeClock(props.world.id)
    ])
    if (epoch !== worldRequestEpoch || props.world.id !== nextProjection.world_id) return
    projection.value = nextProjection
    clock.value = nextClock
    try {
      const nextVisualManifest = await getRealtimeVisualManifest(props.world.id)
      if (epoch === worldRequestEpoch && nextVisualManifest.binding.binding_hash === nextProjection.visual.binding_hash) {
        visualManifest.value = nextVisualManifest
      }
    } catch {
      // The sealed semantic map stays readable with the finite local fallback
      // palette if a manifest CDN/content read is temporarily unavailable.
      visualManifest.value = null
    }
    resetCamera()
    await Promise.all([ensureVisibleChunks(epoch), ensureVisibleActors(epoch), loadMyCharacter(epoch)])
  } catch (caught) {
    if (epoch !== worldRequestEpoch) return
    error.value = caught instanceof Error ? caught.message : t('citySpatial.realtime.loadFailed')
  } finally {
    if (epoch === worldRequestEpoch) initialLoading.value = false
  }
}

async function refreshWorld(): Promise<void> {
  if (refreshing.value) return
  refreshing.value = true
  error.value = null
  const epoch = worldRequestEpoch
  try {
    const [nextProjection, nextClock] = await Promise.all([
      getRealtimeWorldProjection(props.world.id),
      getRealtimeClock(props.world.id)
    ])
    if (!isCurrentEpoch(epoch)) return
    if (projection.value?.static_projection_hash !== nextProjection.static_projection_hash ||
        projection.value?.visual.binding_hash !== nextProjection.visual.binding_hash) {
      await bootstrapWorld()
      return
    }
    projection.value = nextProjection
    clock.value = nextClock
    failedChunks.clear()
    scheduleRender()
    await Promise.all([ensureVisibleChunks(epoch), ensureVisibleActors(epoch), loadMyCharacter(epoch, false)])
  } catch (caught) {
    if (isCurrentEpoch(epoch)) error.value = caught instanceof Error ? caught.message : t('citySpatial.realtime.loadFailed')
  } finally {
    if (isCurrentEpoch(epoch)) refreshing.value = false
  }
}

async function synchronizePatches(): Promise<void> {
  const currentProjection = projection.value
  if (!currentProjection || refreshing.value) return
  try {
    const page = await listRealtimePatches(currentProjection.world_id, currentProjection.timeline_frame_sequence)
    if (projection.value !== currentProjection || page.static_projection_hash !== currentProjection.static_projection_hash) {
      if (projection.value === currentProjection) void bootstrapWorld()
      return
    }
    const timelineAdvanced = page.current_frame_sequence !== currentProjection.timeline_frame_sequence ||
      page.current_cursor !== currentProjection.timeline_cursor
    projection.value = {
      ...currentProjection,
      timeline_frame_sequence: page.current_frame_sequence,
      timeline_cursor: page.current_cursor
    }
    clock.value = await getRealtimeClock(currentProjection.world_id)
    if (timelineAdvanced) {
      void ensureVisibleActors(worldRequestEpoch)
      // Passive server reducers (for example character metabolism) advance
      // owner-private life state without a browser mutation. Refresh it
      // quietly with the new shared timeline so need bars stay truthful and
      // no panel is remounted or flashed.
      void loadMyCharacter(worldRequestEpoch, false)
    }
  } catch {
    // A transport failure must not discard the already-rendered static world.
  }
}

function resizeCanvas(entries: ResizeObserverEntry[]): void {
  const entry = entries[0]
  if (!entry) return
  viewport.width = Math.max(320, Math.floor(entry.contentRect.width))
  viewport.height = Math.max(320, Math.floor(entry.contentRect.height))
  clampCamera()
  scheduleRender()
  scheduleVisibleChunkLoad()
  scheduleVisibleActorLoad()
}

function drawScene(): void {
  const canvas = canvasRef.value
  const spatial = projection.value?.spatial
  if (!canvas || !spatial || viewport.width <= 0 || viewport.height <= 0) return
  const context = canvas.getContext('2d')
  if (!context) return
  const deviceScale = Math.min(2, window.devicePixelRatio || 1)
  const pixelWidth = Math.floor(viewport.width * deviceScale)
  const pixelHeight = Math.floor(viewport.height * deviceScale)
  if (canvas.width !== pixelWidth || canvas.height !== pixelHeight) {
    canvas.width = pixelWidth
    canvas.height = pixelHeight
    canvas.style.width = `${viewport.width}px`
    canvas.style.height = `${viewport.height}px`
  }
  context.setTransform(deviceScale, 0, 0, deviceScale, 0, 0)
  context.imageSmoothingEnabled = false
  const palette = activePalette.value
  context.fillStyle = palette.mapBackground
  context.fillRect(0, 0, viewport.width, viewport.height)

  const cellSize = camera.cellSize
  const worldToScreen = (worldX: number, worldY: number): { x: number; y: number } => ({
    x: Math.round((worldX - camera.centerX) * cellSize + viewport.width / 2),
    y: Math.round((worldY - camera.centerY) * cellSize + viewport.height / 2)
  })
  const visibleEntries = [...chunks.values()]

  for (const entry of visibleEntries) {
    const { chunk } = entry.projection
    const { decoded } = entry
    for (let localY = 0; localY < decoded.height; localY++) {
      const worldY = chunk.chunk_y * spatial.chunk_size + localY
      const screenY = worldToScreen(0, worldY).y
      if (screenY + cellSize < 0 || screenY > viewport.height) continue
      for (let localX = 0; localX < decoded.width; localX++) {
        const worldX = chunk.chunk_x * spatial.chunk_size + localX
        const screenX = worldToScreen(worldX, 0).x
        if (screenX + cellSize < 0 || screenX > viewport.width) continue
        const terrainID = decoded.terrain[localY * decoded.width + localX]
        context.fillStyle = resolveRealtimeTerrainColor(terrainID, palette)
        context.fillRect(screenX, screenY, cellSize + 1, cellSize + 1)
        if (terrainID.includes('road') && cellSize >= 8 && (worldX + worldY) % 4 === 0) {
          context.fillStyle = '#beb7a8'
          context.fillRect(screenX + Math.floor(cellSize / 2), screenY + Math.floor(cellSize / 2), 1, 1)
        }
      }
    }
  }

  for (const building of buildings.values()) {
    context.fillStyle = resolveRealtimeBuildingColor(building.primary_use, palette)
    context.globalAlpha = 0.14
    for (const point of building.footprint) {
      const screen = worldToScreen(point.x, point.y)
      if (screen.x + cellSize < 0 || screen.x > viewport.width || screen.y + cellSize < 0 || screen.y > viewport.height) continue
      context.fillRect(screen.x + 1, screen.y + 1, Math.max(1, cellSize - 2), Math.max(1, cellSize - 2))
    }
    context.globalAlpha = 1
  }

  for (const entry of visibleEntries) {
    const { chunk } = entry.projection
    const { decoded } = entry
    for (const [index, layers] of decoded.layersByCell) {
      const localX = index % decoded.width
      const localY = Math.floor(index / decoded.width)
      const screen = worldToScreen(chunk.chunk_x * spatial.chunk_size + localX, chunk.chunk_y * spatial.chunk_size + localY)
      if (screen.x + cellSize < 0 || screen.x > viewport.width || screen.y + cellSize < 0 || screen.y > viewport.height) continue
      for (const layer of layers) {
        context.fillStyle = resolveRealtimeLayerColor(layer, palette)
        if (layer.kind === 'structure') {
          context.fillRect(screen.x, screen.y, cellSize + 1, cellSize + 1)
          if (layer.definition_id.includes('window') && cellSize >= 6) {
            context.fillStyle = '#d8e6e8'
            context.fillRect(screen.x + Math.floor(cellSize * 0.25), screen.y + Math.floor(cellSize * 0.25), Math.max(1, Math.floor(cellSize * 0.5)), Math.max(1, Math.floor(cellSize * 0.5)))
          }
        } else {
          const inset = Math.max(1, Math.floor(cellSize * 0.22))
          context.fillRect(screen.x + inset, screen.y + inset, Math.max(1, cellSize - inset * 2), Math.max(1, cellSize - inset * 2))
        }
      }
    }
  }

  const actorIndex = actorsByCell.value
  for (const actors of actorIndex.values()) {
    for (const actor of actors) {
      if (actor.z !== 0) continue
      const actorChunkKey = realtimeChunkKey(
        floorDivide(actor.x, spatial.chunk_size),
        floorDivide(actor.y, spatial.chunk_size),
        actor.z
      )
      if (!chunks.has(actorChunkKey)) continue
      const screen = worldToScreen(actor.x, actor.y)
      if (screen.x + cellSize < 0 || screen.x > viewport.width || screen.y + cellSize < 0 || screen.y > viewport.height) continue
      const sprite = resolveRealtimeActorSpritePalette(actor.appearance_variant)
      const bodySize = Math.max(2, Math.floor(cellSize * 0.56))
      const bodyX = screen.x + Math.floor((cellSize - bodySize) / 2)
      const bodyY = screen.y + Math.max(0, cellSize - bodySize - Math.floor(cellSize * 0.1))
      context.fillStyle = sprite.outline
      context.fillRect(bodyX - 1, bodyY - 1, bodySize + 2, bodySize + 2)
      context.fillStyle = sprite.body
      context.fillRect(bodyX, bodyY, bodySize, bodySize)
      if (cellSize >= 8) {
        context.fillStyle = sprite.accent
        context.fillRect(bodyX + Math.floor(bodySize / 2), bodyY + 1, Math.max(1, Math.floor(bodySize / 3)), Math.max(1, Math.floor(bodySize / 3)))
      }
      if (cellSize >= 16) {
        context.fillStyle = '#f2ead8'
        context.font = '10px ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace'
        context.fillText(actor.public_label, screen.x + cellSize + 2, screen.y + cellSize - 1)
      }
    }
  }

  const selected = selectedCell.value
  if (selected) {
    const screen = worldToScreen(selected.worldX, selected.worldY)
    context.strokeStyle = '#f5eac8'
    context.lineWidth = Math.max(1, Math.floor(cellSize / 5))
    context.strokeRect(screen.x + 0.5, screen.y + 0.5, Math.max(1, cellSize - 1), Math.max(1, cellSize - 1))
  }
}

function canvasPoint(event: PointerEvent): { x: number; y: number } {
  const canvas = canvasRef.value
  if (!canvas) return { x: 0, y: 0 }
  const rect = canvas.getBoundingClientRect()
  return { x: event.clientX - rect.left, y: event.clientY - rect.top }
}

function selectAtPoint(point: { x: number; y: number }): void {
  const spatial = projection.value?.spatial
  if (!spatial) return
  const worldX = Math.floor((point.x - viewport.width / 2) / camera.cellSize + camera.centerX)
  const worldY = Math.floor((point.y - viewport.height / 2) / camera.cellSize + camera.centerY)
  const chunkX = floorDivide(worldX, spatial.chunk_size)
  const chunkY = floorDivide(worldY, spatial.chunk_size)
  const entry = chunks.get(realtimeChunkKey(chunkX, chunkY))
  if (!entry) return
  const localX = worldX - chunkX * spatial.chunk_size
  const localY = worldY - chunkY * spatial.chunk_size
  if (localX < 0 || localX >= entry.decoded.width || localY < 0 || localY >= entry.decoded.height) return
  const index = localY * entry.decoded.width + localX
  selectedCell.value = {
    worldX,
    worldY,
    z: 0,
    chunkX,
    chunkY,
    terrainDefinitionID: entry.decoded.terrain[index],
    layers: entry.decoded.layersByCell.get(index) ?? []
  }
  scheduleRender()
}

function handlePointerDown(event: PointerEvent): void {
  if (event.button !== 0) return
  pointerID = event.pointerId
  pointerLast = canvasPoint(event)
  pointerMoved = false
  dragging.value = true
  canvasRef.value?.setPointerCapture?.(event.pointerId)
  canvasRef.value?.focus({ preventScroll: true })
}

function handlePointerMove(event: PointerEvent): void {
  if (pointerID !== event.pointerId || !dragging.value) return
  const point = canvasPoint(event)
  const deltaX = point.x - pointerLast.x
  const deltaY = point.y - pointerLast.y
  pointerLast = point
  if (Math.hypot(deltaX, deltaY) > 1) pointerMoved = true
  if (!pointerMoved) return
  camera.centerX -= deltaX / camera.cellSize
  camera.centerY -= deltaY / camera.cellSize
  clampCamera()
  scheduleRender()
  scheduleVisibleChunkLoad()
  scheduleVisibleActorLoad()
}

function endPointerInteraction(event: PointerEvent): void {
  if (pointerID !== event.pointerId) return
  canvasRef.value?.releasePointerCapture?.(event.pointerId)
  pointerID = null
  dragging.value = false
}

function handlePointerUp(event: PointerEvent): void {
  if (pointerID !== event.pointerId) return
  const point = canvasPoint(event)
  const shouldSelect = !pointerMoved
  endPointerInteraction(event)
  if (shouldSelect) selectAtPoint(point)
}

function handleWheel(event: WheelEvent): void {
  changeZoom(event.deltaY < 0 ? 1 : -1)
}

function handleKeyDown(event: KeyboardEvent): void {
  const panBy = Math.max(1, Math.floor(12 / camera.cellSize))
  switch (event.key) {
    case 'ArrowLeft': camera.centerX -= panBy; break
    case 'ArrowRight': camera.centerX += panBy; break
    case 'ArrowUp': camera.centerY -= panBy; break
    case 'ArrowDown': camera.centerY += panBy; break
    case '+':
    case '=': changeZoom(1); event.preventDefault(); return
    case '-': changeZoom(-1); event.preventDefault(); return
    case '0': resetCamera(); event.preventDefault(); return
    default: return
  }
  event.preventDefault()
  clampCamera()
  scheduleRender()
  scheduleVisibleChunkLoad()
  scheduleVisibleActorLoad()
}

watch(() => props.world.id, () => {
  void bootstrapWorld()
})

onMounted(() => {
  resizeObserver = new ResizeObserver(resizeCanvas)
  if (canvasHostRef.value) resizeObserver.observe(canvasHostRef.value)
  void bootstrapWorld()
  patchTimer = window.setInterval(() => { void synchronizePatches() }, 3_000)
})

onBeforeUnmount(() => {
  worldRequestEpoch++
  resizeObserver?.disconnect()
  if (renderFrame !== null) cancelAnimationFrame(renderFrame)
  if (chunkLoadTimer !== null) window.clearTimeout(chunkLoadTimer)
  if (actorLoadTimer !== null) window.clearTimeout(actorLoadTimer)
  if (patchTimer !== null) window.clearInterval(patchTimer)
})
</script>

<style scoped>
.realtime-pixel-workspace {
  border: 1px solid var(--ui-separator);
  background: var(--ui-surface);
}

.realtime-pixel-command-bar {
  display: grid;
  grid-template-columns: minmax(14rem, 1.1fr) minmax(10rem, 0.75fr) minmax(14rem, 0.9fr) auto;
  gap: 0.75rem;
  align-items: end;
  border-bottom: 1px solid var(--ui-separator);
  padding: 0.85rem;
}

.realtime-pixel-world-select,
.realtime-pixel-identity,
.realtime-pixel-clock { min-width: 0; }

.realtime-pixel-world-select > span,
.realtime-pixel-identity > span,
.realtime-pixel-clock > span,
.realtime-pixel-inspector > header > span {
  display: block;
  margin-bottom: 0.3rem;
  color: var(--ui-label-secondary);
  font: 0.62rem/1.2 ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  letter-spacing: 0.1em;
  text-transform: uppercase;
}

.realtime-pixel-world-select select {
  width: 100%;
  height: 2.5rem;
  border: 1px solid var(--ui-separator);
  padding: 0 2rem 0 0.7rem;
  color: var(--ui-label);
  background: var(--ui-control);
  font: 0.8rem ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
}

.realtime-pixel-identity,
.realtime-pixel-clock { min-height: 2.5rem; }
.realtime-pixel-identity strong,
.realtime-pixel-clock strong { display: block; overflow: hidden; color: var(--ui-label); font-size: 0.82rem; text-overflow: ellipsis; white-space: nowrap; }
.realtime-pixel-identity small,
.realtime-pixel-clock small { display: block; overflow: hidden; margin-top: 0.16rem; color: var(--ui-label-secondary); font: 0.63rem ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; text-overflow: ellipsis; white-space: nowrap; }
.realtime-pixel-clock[data-state='healthy'] strong { color: #2fbf71; }
.realtime-pixel-clock[data-state='unsafe'] strong { color: #d85f5f; }

.realtime-pixel-actions { display: flex; height: 2.5rem; border: 1px solid var(--ui-separator); }
.realtime-pixel-actions button,
.realtime-pixel-actions span { display: grid; min-width: 2.5rem; place-items: center; border-right: 1px solid var(--ui-separator); color: var(--ui-label-secondary); background: var(--ui-control); }
.realtime-pixel-actions button:last-child { border-right: 0; }
.realtime-pixel-actions span { min-width: 3.55rem; background: var(--ui-canvas-raised); font: 0.62rem ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; }
.realtime-pixel-actions button:hover:not(:disabled) { color: var(--ui-label); background: var(--ui-control-hover); }
.realtime-pixel-actions button:disabled { cursor: not-allowed; opacity: 0.35; }

.realtime-pixel-error { display: flex; gap: 0.55rem; align-items: center; border-bottom: 1px solid rgb(220 38 38 / 40%); padding: 0.65rem 0.85rem; color: #dc2626; background: rgb(220 38 38 / 7%); font-size: 0.76rem; }
.realtime-pixel-error span { min-width: 0; flex: 1; overflow-wrap: anywhere; }
.realtime-pixel-error button { color: inherit; font-weight: 700; text-decoration: underline; }

.realtime-pixel-layout { display: grid; grid-template-columns: minmax(0, 1fr) minmax(16rem, 20rem); min-height: 38rem; }
.realtime-pixel-map-panel { display: grid; min-width: 0; grid-template-rows: auto minmax(0, 1fr) auto; border-right: 1px solid var(--ui-separator); background: #172019; }
.realtime-pixel-map-heading { display: flex; min-height: 4rem; align-items: center; justify-content: space-between; gap: 1rem; border-bottom: 1px solid #334039; padding: 0.75rem 0.9rem; color: #e8e2d6; background: #202923; }
.realtime-pixel-map-heading > div { display: grid; min-width: 0; grid-template-columns: auto minmax(0, 1fr); align-items: center; column-gap: 0.55rem; }
.realtime-pixel-map-heading strong { font-size: 0.82rem; }
.realtime-pixel-map-heading small { grid-column: 2; overflow: hidden; margin-top: 0.18rem; color: #a0aaa1; font: 0.62rem ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; text-overflow: ellipsis; white-space: nowrap; }
.realtime-pixel-map-heading > span { flex: none; color: #a0aaa1; font: 0.65rem ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; }
.realtime-pixel-live-dot { width: 0.48rem; height: 0.48rem; background: #aa954f; }
.realtime-pixel-live-dot[data-state='healthy'] { background: #2fbf71; box-shadow: 0 0 0 3px rgb(47 191 113 / 12%); }
.realtime-pixel-live-dot[data-state='unsafe'] { background: #d85f5f; }

.realtime-pixel-canvas-host { position: relative; min-height: 31rem; overflow: hidden; background: #162018; touch-action: none; }
.realtime-pixel-canvas-host.is-dragging { cursor: grabbing; }
.realtime-pixel-canvas { display: block; width: 100%; height: 100%; cursor: crosshair; image-rendering: pixelated; outline: none; }
.realtime-pixel-canvas:focus-visible { box-shadow: inset 0 0 0 2px #f5eac8; }
.realtime-pixel-initial-loading { position: absolute; inset: 0; display: grid; place-content: center; justify-items: center; gap: 0.45rem; color: #cdd4c9; background: #162018; text-align: center; }
.realtime-pixel-initial-loading strong { color: #f2ead8; font-size: 0.84rem; }
.realtime-pixel-initial-loading small { color: #a0aaa1; font-size: 0.7rem; }
.realtime-pixel-chunk-loading { position: absolute; right: 0.75rem; bottom: 0.75rem; display: inline-flex; align-items: center; gap: 0.38rem; border: 1px solid rgb(226 218 194 / 25%); padding: 0.38rem 0.5rem; color: #d8d2c2; background: rgb(22 32 24 / 92%); font: 0.62rem ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; }
.realtime-pixel-loader { width: 0.8rem; height: 0.8rem; border: 2px solid rgb(229 189 100 / 28%); border-top-color: #e5bd64; border-radius: 50%; animation: realtime-pixel-spin 0.75s linear infinite; }
@keyframes realtime-pixel-spin { to { transform: rotate(360deg); } }

.realtime-pixel-map-panel > footer { display: flex; min-height: 2.4rem; flex-wrap: wrap; align-items: center; gap: 0.85rem; border-top: 1px solid #334039; padding: 0.45rem 0.8rem; color: #919b92; background: #202923; font-size: 0.61rem; }
.realtime-pixel-map-panel kbd { border: 1px solid currentColor; padding: 0.05rem 0.2rem; font: inherit; }

.realtime-pixel-inspector { display: grid; min-height: 0; grid-template-rows: auto minmax(0, 1fr) auto; overflow-y: auto; background: var(--ui-surface); }
.realtime-pixel-inspector > header { border-bottom: 1px solid var(--ui-separator); padding: 0.85rem; }
.realtime-pixel-inspector > header h2 { margin: 0; font-size: 0.95rem; }
.realtime-pixel-character { display: grid; gap: 0.55rem; border-bottom: 1px solid var(--ui-separator); padding: 0.75rem 0.85rem; background: var(--ui-canvas-raised); }
.realtime-pixel-character-heading { display: grid; gap: 0.18rem; }
.realtime-pixel-character-heading > span,
.realtime-pixel-character-create label > span { color: var(--ui-label-secondary); font: 0.61rem/1.2 ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; letter-spacing: 0.08em; text-transform: uppercase; }
.realtime-pixel-character-heading > strong { color: var(--ui-label); font-size: 0.78rem; }
.realtime-pixel-character-note,
.realtime-pixel-character-create > p,
.realtime-pixel-character-active > p { margin: 0; color: var(--ui-label-secondary); font-size: 0.68rem; line-height: 1.5; }
.realtime-pixel-character-create { display: grid; gap: 0.48rem; }
.realtime-pixel-character-create label { display: grid; gap: 0.32rem; }
.realtime-pixel-character-create input,
.realtime-pixel-character-create select { width: 100%; min-width: 0; height: 2rem; border: 1px solid var(--ui-separator); padding: 0 0.55rem; color: var(--ui-label); background: var(--ui-control); font: 0.72rem ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; }
.realtime-pixel-character-create select { cursor: pointer; }
.realtime-pixel-character-create input:focus-visible,
.realtime-pixel-character-create select:focus-visible { outline: 2px solid var(--ui-accent); outline-offset: -2px; }
.realtime-pixel-character-archetype-preview { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 0.32rem 0.45rem; border: 1px solid var(--ui-separator); border-left: 2px solid var(--ui-accent); padding: 0.42rem 0.48rem; background: var(--ui-control); }
.realtime-pixel-character-archetype-preview > strong { min-width: 0; overflow: hidden; color: var(--ui-label); font-size: 0.68rem; text-overflow: ellipsis; white-space: nowrap; }
.realtime-pixel-character-archetype-preview > small { align-self: center; color: var(--ui-accent); font: 0.58rem ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; text-align: right; }
.realtime-pixel-character-archetype-preview > span { color: var(--ui-label-secondary); font: 0.56rem ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; }
.realtime-pixel-character-create button,
.realtime-pixel-character-active button { min-height: 2rem; border: 1px solid var(--ui-accent); padding: 0.35rem 0.55rem; color: #fff; background: var(--ui-accent); font-size: 0.7rem; font-weight: 700; }
.realtime-pixel-character-create button:hover:not(:disabled),
.realtime-pixel-character-active button:hover:not(:disabled) { filter: brightness(1.07); }
.realtime-pixel-character-create button:disabled,
.realtime-pixel-character-active button:disabled { cursor: not-allowed; opacity: 0.45; }
.realtime-pixel-character-active { display: grid; gap: 0.3rem; }
.realtime-pixel-character-active > strong { color: var(--ui-label); font: 0.78rem ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; }
.realtime-pixel-character-active > span { color: var(--ui-accent); font: 0.68rem ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; }
.realtime-pixel-character-active > small { color: var(--ui-label-secondary); font: 0.64rem ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; }
.realtime-pixel-character-agent { display: grid; gap: 0.42rem; border-top: 1px solid var(--ui-separator); border-left: 2px solid var(--ui-accent); padding: 0.56rem 0 0.1rem 0.48rem; }
.realtime-pixel-character-agent[data-mode='suspended'] { border-left-color: var(--ui-label-secondary); }
.realtime-pixel-character-agent > header { display: flex; align-items: baseline; justify-content: space-between; gap: 0.5rem; }
.realtime-pixel-character-agent > header > span { color: var(--ui-label); font: 0.66rem ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; font-weight: 700; letter-spacing: 0.04em; text-transform: uppercase; }
.realtime-pixel-character-agent > header > small { color: var(--ui-accent); font: 0.58rem ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; text-align: right; }
.realtime-pixel-character-agent[data-mode='suspended'] > header > small { color: var(--ui-label-secondary); }
.realtime-pixel-character-agent > p,
.realtime-pixel-character-agent form > p,
.realtime-pixel-character-control-note { margin: 0; color: var(--ui-label-secondary); font-size: 0.6rem; line-height: 1.45; }
.realtime-pixel-character-agent > dl { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); margin: 0; border: 1px solid var(--ui-separator); background: var(--ui-canvas-raised); }
.realtime-pixel-character-agent > dl > div { display: grid; gap: 0.12rem; min-width: 0; padding: 0.34rem 0.4rem; }
.realtime-pixel-character-agent > dl > div + div { border-left: 1px solid var(--ui-separator); }
.realtime-pixel-character-agent dt { color: var(--ui-label-secondary); font-size: 0.55rem; }
.realtime-pixel-character-agent dd { overflow: hidden; margin: 0; color: var(--ui-label); font: 0.61rem ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; text-overflow: ellipsis; white-space: nowrap; }
.realtime-pixel-character-agent form { display: grid; gap: 0.38rem; }
.realtime-pixel-character-agent form label { display: grid; gap: 0.22rem; }
.realtime-pixel-character-agent form label > span { color: var(--ui-label-secondary); font: 0.57rem/1.2 ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; letter-spacing: 0.07em; text-transform: uppercase; }
.realtime-pixel-character-agent input,
.realtime-pixel-character-agent select,
.realtime-pixel-character-agent textarea { width: 100%; min-width: 0; border: 1px solid var(--ui-separator); border-radius: 0; padding: 0.38rem 0.46rem; color: var(--ui-label); background: var(--ui-control); font: 0.64rem/1.35 ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; }
.realtime-pixel-character-agent select { min-height: 1.9rem; cursor: pointer; }
.realtime-pixel-character-agent textarea { min-height: 3.25rem; resize: vertical; }
.realtime-pixel-character-agent input:focus-visible,
.realtime-pixel-character-agent select:focus-visible,
.realtime-pixel-character-agent textarea:focus-visible { outline: 2px solid var(--ui-accent); outline-offset: -2px; }
.realtime-pixel-character-active .realtime-pixel-character-agent button { min-height: 1.88rem; border-color: var(--ui-accent); padding: 0.28rem 0.46rem; }
.realtime-pixel-character-control-note { border-left: 2px solid var(--ui-label-secondary); padding: 0.34rem 0.45rem; background: var(--ui-control); }
.realtime-pixel-character-life,
.realtime-pixel-character-progression,
.realtime-pixel-character-activities,
.realtime-pixel-character-history,
.realtime-pixel-character-portals,
.realtime-pixel-character-interior { display: grid; gap: 0.42rem; border-top: 1px solid var(--ui-separator); padding-top: 0.56rem; }
.realtime-pixel-character-life > header,
.realtime-pixel-character-progression > header,
.realtime-pixel-character-activities > header,
.realtime-pixel-character-history > header,
.realtime-pixel-character-portals > header,
.realtime-pixel-character-interior > header { display: flex; align-items: baseline; justify-content: space-between; gap: 0.5rem; }
.realtime-pixel-character-life > header > span,
.realtime-pixel-character-progression > header > span,
.realtime-pixel-character-activities > header > span,
.realtime-pixel-character-history > header > span,
.realtime-pixel-character-portals > header > span,
.realtime-pixel-character-interior > header > span { color: var(--ui-label); font: 0.66rem ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; font-weight: 700; letter-spacing: 0.04em; text-transform: uppercase; }
.realtime-pixel-character-life > header > small,
.realtime-pixel-character-progression > header > small,
.realtime-pixel-character-activities > header > small,
.realtime-pixel-character-history > header > small,
.realtime-pixel-character-portals > header > small,
.realtime-pixel-character-interior > header > small { color: var(--ui-label-secondary); font-size: 0.59rem; text-align: right; }
.realtime-pixel-character-needs { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 0.36rem; }
.realtime-pixel-character-need { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 0.18rem 0.35rem; border: 1px solid var(--ui-separator); padding: 0.34rem 0.4rem; background: var(--ui-control); }
.realtime-pixel-character-need > span { min-width: 0; overflow: hidden; color: var(--ui-label-secondary); font-size: 0.6rem; text-overflow: ellipsis; white-space: nowrap; }
.realtime-pixel-character-need > strong { color: var(--ui-label); font: 0.62rem ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; }
.realtime-pixel-character-need > i { grid-column: 1 / -1; display: block; height: 0.22rem; overflow: hidden; background: var(--ui-separator); }
.realtime-pixel-character-need > i > b { display: block; height: 100%; background: var(--ui-accent); transition: width 150ms ease-out; }
.realtime-pixel-inspector .realtime-pixel-character-life dl { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); margin: 0; border: 0; }
.realtime-pixel-inspector .realtime-pixel-character-life dl > div { display: grid; grid-template-columns: 1fr; gap: 0.14rem; border: 1px solid var(--ui-separator); padding: 0.36rem 0.4rem; background: var(--ui-canvas-raised); }
.realtime-pixel-inspector .realtime-pixel-character-life dt { color: var(--ui-label-secondary); font-size: 0.58rem; }
.realtime-pixel-inspector .realtime-pixel-character-life dd { color: var(--ui-label); font: 0.66rem ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; }
.realtime-pixel-character-progression-summary { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); border: 1px solid var(--ui-separator); background: var(--ui-canvas-raised); }
.realtime-pixel-character-progression-summary > div { display: grid; gap: 0.14rem; min-width: 0; padding: 0.36rem 0.4rem; }
.realtime-pixel-character-progression-summary > div + div { border-left: 1px solid var(--ui-separator); }
.realtime-pixel-character-progression-summary span,
.realtime-pixel-character-active-roles > span { color: var(--ui-label-secondary); font-size: 0.58rem; }
.realtime-pixel-character-progression-summary strong { overflow: hidden; color: var(--ui-label); font: 0.66rem ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; text-overflow: ellipsis; white-space: nowrap; }
.realtime-pixel-character-attributes { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 0.32rem; }
.realtime-pixel-character-attribute { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 0.12rem 0.35rem; border: 1px solid var(--ui-separator); padding: 0.32rem 0.4rem; background: var(--ui-control); }
.realtime-pixel-character-attribute > span { min-width: 0; overflow: hidden; color: var(--ui-label-secondary); font-size: 0.58rem; text-overflow: ellipsis; white-space: nowrap; }
.realtime-pixel-character-attribute > strong { color: var(--ui-label); font: 0.61rem ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; }
.realtime-pixel-character-attribute > small { grid-column: 1 / -1; color: var(--ui-label-secondary); font: 0.54rem ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; }
.realtime-pixel-character-attribute > i { grid-column: 1 / -1; display: block; height: 0.18rem; overflow: hidden; background: var(--ui-separator); }
.realtime-pixel-character-attribute > i > b { display: block; height: 100%; background: var(--ui-accent); transition: width 150ms ease-out; }
.realtime-pixel-character-active-roles { display: flex; min-height: 1.7rem; flex-wrap: wrap; align-items: center; gap: 0.3rem; border: 1px solid var(--ui-separator); padding: 0.28rem 0.38rem; background: var(--ui-canvas-raised); }
.realtime-pixel-character-active-roles > span { margin-right: auto; }
.realtime-pixel-character-active-roles > strong { border-left: 2px solid var(--ui-accent); padding-left: 0.32rem; color: var(--ui-label); font: 0.58rem ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; }
.realtime-pixel-character-role-options { display: grid; gap: 0.3rem; }
.realtime-pixel-character-role-options article { display: grid; grid-template-columns: minmax(0, 1fr) auto; align-items: center; gap: 0.42rem; border: 1px solid var(--ui-separator); border-left: 2px solid var(--ui-label-secondary); padding: 0.34rem 0.4rem; background: var(--ui-control); }
.realtime-pixel-character-role-options article[data-available='true'] { border-left-color: var(--ui-accent); background: var(--ui-canvas-raised); }
.realtime-pixel-character-role-options article > div { display: grid; min-width: 0; gap: 0.12rem; }
.realtime-pixel-character-role-options article strong { overflow: hidden; color: var(--ui-label); font-size: 0.63rem; text-overflow: ellipsis; white-space: nowrap; }
.realtime-pixel-character-role-options article small { overflow: hidden; color: var(--ui-label-secondary); font-size: 0.55rem; text-overflow: ellipsis; white-space: nowrap; }
.realtime-pixel-character-active .realtime-pixel-character-role-options button { min-height: 1.66rem; padding: 0.2rem 0.36rem; font-size: 0.58rem; }
.realtime-pixel-character-activities > div { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 0.36rem; }
.realtime-pixel-character-active .realtime-pixel-character-activities button { display: grid; min-height: 2.28rem; align-content: center; gap: 0.12rem; border-color: var(--ui-separator); padding: 0.32rem 0.42rem; color: var(--ui-label); background: var(--ui-control); text-align: left; }
.realtime-pixel-character-active .realtime-pixel-character-activities button:hover:not(:disabled) { border-color: var(--ui-accent); color: var(--ui-accent); background: var(--ui-canvas-raised); filter: none; }
.realtime-pixel-character-active .realtime-pixel-character-activities button > strong { overflow: hidden; font-size: 0.62rem; text-overflow: ellipsis; white-space: nowrap; }
.realtime-pixel-character-active .realtime-pixel-character-activities button > small { overflow: hidden; color: var(--ui-label-secondary); font-size: 0.55rem; text-overflow: ellipsis; white-space: nowrap; }
.realtime-pixel-character-active .realtime-pixel-character-activities button:disabled { opacity: 0.58; }
.realtime-pixel-character-portals > div { display: grid; gap: 0.36rem; }
.realtime-pixel-character-active .realtime-pixel-character-portals button { display: grid; min-height: 2.28rem; align-content: center; gap: 0.12rem; border-color: var(--ui-separator); padding: 0.32rem 0.42rem; color: var(--ui-label); background: var(--ui-control); text-align: left; }
.realtime-pixel-character-active .realtime-pixel-character-portals button:hover:not(:disabled) { border-color: var(--ui-accent); color: var(--ui-accent); background: var(--ui-canvas-raised); filter: none; }
.realtime-pixel-character-active .realtime-pixel-character-portals button > strong,
.realtime-pixel-character-active .realtime-pixel-character-portals button > small { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.realtime-pixel-character-active .realtime-pixel-character-portals button > strong { font-size: 0.62rem; }
.realtime-pixel-character-active .realtime-pixel-character-portals button > small { color: var(--ui-label-secondary); font: 0.55rem ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; }
.realtime-pixel-character-active .realtime-pixel-character-portals button:disabled { opacity: 0.58; }
.realtime-pixel-character-interior-grid { display: grid; max-height: 16rem; gap: 1px; overflow: auto; border: 1px solid var(--ui-separator); padding: 0.35rem; background: var(--ui-canvas-raised); }
.realtime-pixel-character-interior-grid button { display: grid; min-width: 1.4rem; min-height: 1.4rem; aspect-ratio: 1; place-items: center; border: 1px solid transparent; padding: 0; color: var(--ui-label-secondary); background: var(--ui-control); font: 0.76rem/1 ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; }
.realtime-pixel-character-interior-grid button.is-blocked { color: var(--ui-separator); background: var(--ui-surface); }
.realtime-pixel-character-interior-grid button.is-traversable { color: var(--ui-label); }
.realtime-pixel-character-interior-grid button:hover:not(:disabled) { border-color: var(--ui-accent); color: var(--ui-accent); }
.realtime-pixel-character-interior-grid button.is-current { border-color: var(--ui-accent); color: #fff; background: var(--ui-accent); }
.realtime-pixel-character-interior-grid button:disabled:not(.is-current) { cursor: default; opacity: 0.74; }
.realtime-pixel-character-history ol { display: grid; gap: 0.22rem; margin: 0; padding: 0; list-style: none; }
.realtime-pixel-character-history li { display: flex; align-items: center; justify-content: space-between; gap: 0.4rem; border-left: 2px solid var(--ui-separator); padding: 0.28rem 0.38rem; background: var(--ui-control); }
.realtime-pixel-character-history li > strong { min-width: 0; overflow: hidden; color: var(--ui-label); font-size: 0.62rem; text-overflow: ellipsis; white-space: nowrap; }
.realtime-pixel-character-history li > span { flex: 0 0 auto; color: var(--ui-label-secondary); font-size: 0.58rem; }
.realtime-pixel-character-history li > span[data-outcome='penalized'] { color: #d85f5f; }
.realtime-pixel-character-feedback { margin: 0; border-left: 2px solid var(--ui-accent); padding: 0.34rem 0.45rem; color: var(--ui-label); background: var(--ui-control); font-size: 0.66rem; line-height: 1.45; }
.realtime-pixel-character-error { margin: 0; color: #dc2626; font-size: 0.67rem; line-height: 1.45; overflow-wrap: anywhere; }
.realtime-pixel-inspector dl { display: grid; margin: 0; border-bottom: 1px solid var(--ui-separator); }
.realtime-pixel-inspector dl > div { display: grid; grid-template-columns: 5.3rem minmax(0, 1fr); gap: 0.55rem; border-bottom: 1px solid var(--ui-separator); padding: 0.65rem 0.85rem; }
.realtime-pixel-inspector dl > div:last-child { border-bottom: 0; }
.realtime-pixel-inspector dt { color: var(--ui-label-secondary); font-size: 0.7rem; }
.realtime-pixel-inspector dd { min-width: 0; margin: 0; overflow: hidden; color: var(--ui-label); font: 0.72rem ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; text-overflow: ellipsis; white-space: nowrap; }
.realtime-pixel-inspector code { font: inherit; }
.realtime-pixel-building { display: grid; gap: 0.22rem; border-bottom: 1px solid var(--ui-separator); padding: 0.8rem 0.85rem; }
.realtime-pixel-building > span { color: var(--ui-label-secondary); font-size: 0.66rem; }
.realtime-pixel-building strong { font: 0.73rem ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; }
.realtime-pixel-building small { color: var(--ui-label-secondary); font-size: 0.68rem; }
.realtime-pixel-actor { display: grid; gap: 0.22rem; border-bottom: 1px solid var(--ui-separator); padding: 0.8rem 0.85rem; background: var(--ui-control); }
.realtime-pixel-actor > span { color: var(--ui-label-secondary); font-size: 0.66rem; }
.realtime-pixel-actor strong { color: var(--ui-label); font: 0.74rem ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; }
.realtime-pixel-actor small { color: var(--ui-label-secondary); font-size: 0.68rem; }
.realtime-pixel-actor code { overflow: hidden; color: var(--ui-label-secondary); font: 0.64rem ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; text-overflow: ellipsis; white-space: nowrap; }
.realtime-pixel-stack { padding: 0.85rem; }
.realtime-pixel-stack h3 { margin: 0 0 0.55rem; font-size: 0.75rem; }
.realtime-pixel-stack ol { display: grid; gap: 0.35rem; margin: 0; padding: 0; list-style: none; }
.realtime-pixel-stack li { display: grid; grid-template-columns: 5rem minmax(0, 1fr); gap: 0.45rem; border-left: 2px solid var(--ui-accent); padding: 0.32rem 0.45rem; background: var(--ui-control); }
.realtime-pixel-stack li strong { color: var(--ui-label-secondary); font-size: 0.63rem; text-transform: uppercase; }
.realtime-pixel-stack li code { min-width: 0; overflow: hidden; font-size: 0.65rem; text-overflow: ellipsis; white-space: nowrap; }
.realtime-pixel-stack p { margin: 0; color: var(--ui-label-secondary); font-size: 0.72rem; }
.realtime-pixel-inspector-empty { display: grid; min-height: 18rem; place-content: center; justify-items: center; gap: 0.5rem; padding: 1rem; color: var(--ui-label-secondary); text-align: center; }
.realtime-pixel-inspector-empty strong { color: var(--ui-label); font-size: 0.82rem; }
.realtime-pixel-inspector-empty p { max-width: 14rem; margin: 0; font-size: 0.72rem; line-height: 1.6; }
.realtime-pixel-inspector > footer { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 0.3rem 0.65rem; border-top: 1px solid var(--ui-separator); padding: 0.75rem 0.85rem; }
.realtime-pixel-inspector > footer span { color: var(--ui-label-secondary); font-size: 0.62rem; }
.realtime-pixel-inspector > footer strong,
.realtime-pixel-inspector > footer code { overflow: hidden; color: var(--ui-label); font: 0.65rem ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; text-align: right; text-overflow: ellipsis; white-space: nowrap; }

@media (max-width: 900px) {
  .realtime-pixel-command-bar { grid-template-columns: minmax(0, 1fr) minmax(0, 1fr); }
  .realtime-pixel-actions { justify-self: start; }
  .realtime-pixel-layout { grid-template-columns: 1fr; }
  .realtime-pixel-map-panel { border-right: 0; border-bottom: 1px solid var(--ui-separator); }
  .realtime-pixel-inspector { min-height: 22rem; }
}

@media (max-width: 560px) {
  .realtime-pixel-command-bar { grid-template-columns: 1fr; }
  .realtime-pixel-map-heading > span { display: none; }
  .realtime-pixel-canvas-host { min-height: 25rem; }
  .realtime-pixel-character-attributes { grid-template-columns: 1fr; }
}
</style>
