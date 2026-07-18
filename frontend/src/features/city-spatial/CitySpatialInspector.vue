<template>
  <aside class="city-inspector" :aria-label="t('citySpatial.inspector.title')">
    <header class="city-panel-header">
      <div>
        <p class="city-panel-eyebrow">{{ t('citySpatial.inspector.eyebrow') }}</p>
        <h2>{{ t('citySpatial.inspector.title') }}</h2>
      </div>
      <span class="city-inspector-mode">{{ mode === 'local' ? 'CELL' : 'TILE' }}</span>
    </header>

    <div v-if="mode === 'overmap' && tile" class="city-inspector-content">
      <section class="city-coordinate-block">
        <span>{{ t('citySpatial.inspector.chunk') }}</span>
        <strong>{{ tile.chunk_x }}, {{ tile.chunk_y }}, {{ tile.z }}</strong>
      </section>
      <dl class="city-data-list">
        <div>
          <dt>{{ t('citySpatial.inspector.district') }}</dt>
          <dd>{{ tile.district_code }}</dd>
        </div>
        <div>
          <dt>{{ t('citySpatial.inspector.terrain') }}</dt>
          <dd>{{ tileDefinition?.name ?? tile.terrain_definition_id }}</dd>
        </div>
        <div>
          <dt>{{ t('citySpatial.inspector.variant') }}</dt>
          <dd>{{ tile.variant }}</dd>
        </div>
        <div>
          <dt>{{ t('citySpatial.inspector.roadMask') }}</dt>
          <dd class="font-mono">{{ maskLabel(tile.road_mask) }}</dd>
        </div>
        <div>
          <dt>{{ t('citySpatial.inspector.riverMask') }}</dt>
          <dd class="font-mono">{{ maskLabel(tile.river_mask) }}</dd>
        </div>
        <div>
          <dt>{{ t('citySpatial.inspector.state') }}</dt>
          <dd>
            <span :class="generated ? 'city-state-ready' : 'city-state-pending'">
              {{ generated ? t('citySpatial.inspector.generated') : t('citySpatial.inspector.notGenerated') }}
            </span>
          </dd>
        </div>
        <div v-if="landState">
          <dt>{{ t('citySpatial.inspector.parcels') }}</dt>
          <dd>{{ tileLandSummary.parcels.length }}</dd>
        </div>
        <div v-if="landState">
          <dt>{{ t('citySpatial.inspector.buildings') }}</dt>
          <dd>{{ tileLandSummary.buildings.length }}</dd>
        </div>
      </dl>
      <section v-if="tileLandSummary.landUses.length" class="city-land-section">
        <div class="city-section-label">
          <span>{{ t('citySpatial.inspector.zoning') }}</span>
          <span>F7.3</span>
        </div>
        <div class="city-use-list">
          <span v-for="use in tileLandSummary.landUses" :key="use" :data-use="use">
            {{ t(`citySpatial.landUse.${use}`) }}
          </span>
        </div>
        <div class="city-building-list">
          <article v-for="building in tileLandSummary.buildings" :key="building.code">
            <div>
              <strong>{{ building.code }}</strong>
              <span>{{ t(`citySpatial.landUse.${building.primary_use}`) }}</span>
            </div>
            <small>{{ t('citySpatial.inspector.floorSummary', { count: building.floor_count, capacity: building.capacity_units }) }}</small>
          </article>
        </div>
      </section>
      <section v-if="tileDevelopmentProjects.length" class="city-land-section">
        <div class="city-section-label">
          <span>{{ t('citySpatial.development.tileProjects') }}</span>
          <span>F7.4 · {{ tileDevelopmentProjects.length }}</span>
        </div>
        <div class="city-development-list">
          <article v-for="project in tileDevelopmentProjects" :key="project.code">
            <div>
              <strong>{{ project.name || project.code }}</strong>
              <small>{{ project.code }} · {{ project.building_code }}</small>
            </div>
            <div class="city-development-state">
              <span :data-status="project.status">{{ t(`citySpatial.development.status.${project.status}`) }}</span>
              <progress :value="project.progress_milli" max="1000" />
            </div>
          </article>
        </div>
      </section>
      <section v-if="tileEnterpriseSites.length" class="city-land-section">
        <div class="city-section-label">
          <span>{{ t('citySpatial.enterprise.inspector.tileSites') }}</span>
          <span>F7.5 · {{ tileEnterpriseSites.length }}</span>
        </div>
        <div class="city-enterprise-inspector-list">
          <article v-for="site in tileEnterpriseSites" :key="site.code">
            <span class="city-land-glyph" :data-use="site.site_type === 'production' || site.site_type === 'warehouse' ? 'industrial' : 'commercial'">&amp;</span>
            <div>
              <strong>{{ site.name }}</strong>
              <small>{{ site.firm_entity_code }} · {{ t(`citySpatial.enterprise.siteType.${site.site_type}`) }}</small>
              <code>{{ site.building_code }}</code>
            </div>
            <b>{{ formatInteger(site.occupied_units) }}</b>
          </article>
        </div>
      </section>
      <div class="city-hash-block">
        <span>{{ t('citySpatial.inspector.tileHash') }}</span>
        <code>{{ tile.tile_hash }}</code>
      </div>
    </div>

    <div v-else-if="mode === 'local' && coordinate" class="city-inspector-content">
      <section class="city-coordinate-block">
        <span>{{ t('citySpatial.inspector.worldCoordinate') }}</span>
        <strong>{{ coordinate.worldX }}, {{ coordinate.worldY }}, {{ coordinate.z }}</strong>
      </section>
      <dl class="city-data-list city-data-list-compact">
        <div>
          <dt>{{ t('citySpatial.inspector.chunk') }}</dt>
          <dd>{{ chunkCoordinate }}</dd>
        </div>
        <div>
          <dt>{{ t('citySpatial.inspector.localCoordinate') }}</dt>
          <dd>{{ cell ? `${cell.localX}, ${cell.localY}` : '—' }}</dd>
        </div>
        <div v-if="chunk">
          <dt>{{ t('citySpatial.inspector.revision') }}</dt>
          <dd>{{ chunk.revision }}</dd>
        </div>
        <div v-if="chunk">
          <dt>{{ t('citySpatial.inspector.generatedTick') }}</dt>
          <dd>{{ chunk.generatedTick }}</dd>
        </div>
      </dl>

      <section v-if="landContext" class="city-land-section">
        <div class="city-section-label">
          <span>{{ t('citySpatial.inspector.landStack') }}</span>
          <span>{{ landContext.portals.length + (landContext.building ? 1 : 0) + (landContext.parcel ? 1 : 0) }}</span>
        </div>

        <article v-if="landContext.parcel" class="city-land-card">
          <header>
            <span class="city-land-glyph" :data-use="landContext.parcel.zone_code">▱</span>
            <div>
              <strong>{{ landContext.parcel.code }}</strong>
              <small>{{ t('citySpatial.inspector.parcel') }} · {{ t(`citySpatial.landUse.${landContext.parcel.zone_code}`) }}</small>
            </div>
          </header>
          <dl>
            <div><dt>{{ t('citySpatial.inspector.area') }}</dt><dd>{{ formatInteger(landContext.parcel.area_sqm) }} m²</dd></div>
            <div><dt>{{ t('citySpatial.inspector.version') }}</dt><dd>v{{ landContext.parcel.version }}</dd></div>
          </dl>
        </article>

        <article v-if="landContext.building" class="city-land-card">
          <header>
            <span class="city-land-glyph" :data-use="landContext.building.primary_use">#</span>
            <div>
              <strong>{{ landContext.building.code }}</strong>
              <small>{{ t('citySpatial.inspector.building') }} · {{ t(`citySpatial.landUse.${landContext.building.primary_use}`) }}</small>
            </div>
          </header>
          <dl>
            <div><dt>{{ t('citySpatial.inspector.floors') }}</dt><dd>{{ landContext.building.base_z }}…{{ landContext.building.top_z }}</dd></div>
            <div><dt>{{ t('citySpatial.inspector.floorArea') }}</dt><dd>{{ formatInteger(landContext.building.floor_area_sqm) }} m²</dd></div>
            <div><dt>{{ t('citySpatial.inspector.occupancy') }}</dt><dd>{{ landContext.building.occupied_units }} / {{ landContext.building.capacity_units }}</dd></div>
            <div><dt>{{ t('citySpatial.inspector.quality') }}</dt><dd>{{ formatMilli(landContext.building.quality_milli) }}</dd></div>
          </dl>
          <div v-if="landContext.unitPools.length" class="city-pool-list">
            <div v-for="pool in landContext.unitPools" :key="pool.code">
              <span>{{ pool.code }}</span>
              <strong>{{ pool.occupied_unit_count }} / {{ pool.unit_count }}</strong>
            </div>
          </div>
          <div v-if="buildingEnterpriseSites.length" class="city-enterprise-inspector-list city-enterprise-building-list">
            <article v-for="site in buildingEnterpriseSites" :key="site.code">
              <span class="city-land-glyph" :data-use="site.site_type === 'production' || site.site_type === 'warehouse' ? 'industrial' : 'commercial'">&amp;</span>
              <div>
                <strong>{{ site.name }}</strong>
                <small>{{ t(`citySpatial.enterprise.siteType.${site.site_type}`) }} · {{ site.firm_entity_code }}</small>
                <code>{{ site.code }}</code>
              </div>
              <div class="city-enterprise-capacity">
                <b>{{ formatInteger(site.occupied_units) }}</b>
                <small>{{ enterprisePoolAvailability(site.pool_code) }}</small>
              </div>
            </article>
          </div>
          <div v-if="buildingProjects.length" class="city-building-projects">
            <div v-for="project in buildingProjects" :key="project.code">
              <span>
                <strong>{{ project.name || project.code }}</strong>
                <small>{{ t(`citySpatial.development.status.${project.status}`) }}</small>
              </span>
              <span class="city-project-progress">
                <b>{{ formatMilli(project.progress_milli) }}</b>
                <progress :value="project.progress_milli" max="1000" />
              </span>
            </div>
          </div>
          <dl v-if="buildingAdjustments.length" class="city-adjustment-summary">
            <div><dt>{{ t('citySpatial.development.adjustments') }}</dt><dd>{{ buildingAdjustments.length }}</dd></div>
            <div><dt>{{ t('citySpatial.development.addedFloors') }}</dt><dd>+{{ addedFloors }}</dd></div>
            <div><dt>{{ t('citySpatial.development.addedCapacity') }}</dt><dd>+{{ formatInteger(addedCapacity) }}</dd></div>
            <div><dt>{{ t('citySpatial.development.qualityGain') }}</dt><dd>+{{ formatMilli(qualityGain) }}</dd></div>
          </dl>
        </article>

        <article v-for="portal in landContext.portals" :key="`${portal.building_code}:${portal.code}`" class="city-portal-card">
          <span class="city-land-glyph">{{ portal.portal_type === 'stair' ? '↕' : '+' }}</span>
          <div>
            <strong>{{ portal.code }}</strong>
            <small>{{ t(`citySpatial.portalType.${portal.portal_type}`) }} · {{ portal.from_x }},{{ portal.from_y }},{{ portal.from_z }} → {{ portal.to_x }},{{ portal.to_y }},{{ portal.to_z }}</small>
          </div>
        </article>

        <details v-if="landContext.housingAllocations.length" class="city-allocation-list">
          <summary>{{ t('citySpatial.inspector.allocations', { count: landContext.housingAllocations.length }) }}</summary>
          <div v-for="allocation in landContext.housingAllocations" :key="`${allocation.pool_code}:${allocation.cohort_key}`">
            <code>{{ allocation.cohort_key }}</code>
            <strong>{{ allocation.allocated_units }}</strong>
          </div>
        </details>
      </section>

      <template v-if="cell">
        <section class="city-stack-section">
          <div class="city-section-label">
            <span>{{ t('citySpatial.inspector.cellStack') }}</span>
            <span>{{ cell.stack.length }}</span>
          </div>
          <article v-for="layer in [...cell.stack].reverse()" :key="`${layer.kind}:${layer.definitionID}`" class="city-layer-card">
            <span class="city-layer-glyph" aria-hidden="true">{{ layer.glyph }}</span>
            <div class="city-layer-copy">
              <strong>{{ layer.name }}</strong>
              <code>{{ layer.definitionID }}</code>
            </div>
            <span class="city-layer-kind">{{ layer.kind }}</span>
            <div v-if="layer.flags.length" class="city-flag-list">
              <span v-for="flag in layer.flags" :key="flag">{{ flag }}</span>
            </div>
            <p v-if="layer.movementCost > 0" class="city-movement-cost">
              {{ t('citySpatial.inspector.movementCost', { value: layer.movementCost }) }}
            </p>
          </article>
        </section>
        <div v-if="chunk" class="city-hash-block">
          <span>{{ t('citySpatial.inspector.payloadHash') }}</span>
          <code>{{ chunk.payloadHash }}</code>
        </div>
      </template>
      <div v-else class="city-inspector-empty">
        <Icon name="inbox" size="lg" />
        <strong>{{ t('citySpatial.inspector.unavailableTitle') }}</strong>
        <p>{{ t('citySpatial.inspector.unavailableDescription') }}</p>
      </div>
    </div>

    <div v-else class="city-inspector-empty city-inspector-empty-main">
      <Icon name="search" size="lg" />
      <strong>{{ t('citySpatial.inspector.emptyTitle') }}</strong>
      <p>{{ t('citySpatial.inspector.emptyDescription') }}</p>
    </div>
  </aside>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type {
  CityDevelopmentState,
  CityEnterpriseLocationState,
  CityLandState,
  CityOvermapTile,
  CitySpatialRuleSet
} from '@/api/citySpatial'
import Icon from '@/components/icons/Icon.vue'
import {
  floorDiv,
  getCityLandCellContext,
  getCityLandTileSummary,
  getCityDevelopmentProjectsForBuilding,
  getCityEnterpriseSitesForBuilding,
  type ProjectedCityCell,
  type ProjectedCityChunk
} from './projection'
import type { CityMapMode } from '@/stores/citySpatial'

const props = withDefaults(defineProps<{
  mode: CityMapMode
  tile: CityOvermapTile | null
  coordinate: { worldX: number; worldY: number; z: number } | null
  cell: ProjectedCityCell | null
  chunk: ProjectedCityChunk | null
  ruleSet: CitySpatialRuleSet | null
  landState: CityLandState | null
  developmentState?: CityDevelopmentState | null
  enterpriseLocationState?: CityEnterpriseLocationState | null
  chunkSize: number
  generated: boolean
}>(), {
  developmentState: null,
  enterpriseLocationState: null
})

const { t, locale } = useI18n()

const tileDefinition = computed(() => (
  props.ruleSet?.definitions.find(definition => definition.id === props.tile?.terrain_definition_id) ?? null
))

const chunkCoordinate = computed(() => {
  if (!props.coordinate || props.chunkSize <= 0) return '—'
  return `${floorDiv(props.coordinate.worldX, props.chunkSize)}, ${floorDiv(props.coordinate.worldY, props.chunkSize)}, ${props.coordinate.z}`
})

const tileLandSummary = computed(() => props.tile
  ? getCityLandTileSummary(props.landState, props.tile)
  : { landUses: [], parcels: [], buildings: [] })

const landContext = computed(() => props.coordinate
  ? getCityLandCellContext(
      props.landState,
      props.coordinate.worldX,
      props.coordinate.worldY,
      props.coordinate.z,
      props.chunkSize
    )
  : null)

const tileDevelopmentProjects = computed(() => {
  const buildingCodes = new Set(tileLandSummary.value.buildings.map(building => building.code))
  return props.developmentState?.projects.filter(project => buildingCodes.has(project.building_code)) ?? []
})

const tileEnterpriseSites = computed(() => {
  const buildingCodes = new Set(tileLandSummary.value.buildings.map(building => building.code))
  return props.enterpriseLocationState?.sites.filter(site => (
    site.status === 'active' && buildingCodes.has(site.building_code)
  )) ?? []
})

const buildingProjects = computed(() => getCityDevelopmentProjectsForBuilding(
  props.developmentState,
  landContext.value?.building?.code
))

const buildingEnterpriseSites = computed(() => getCityEnterpriseSitesForBuilding(
  props.enterpriseLocationState,
  landContext.value?.building?.code
))

const buildingAdjustments = computed(() => {
  const buildingCode = landContext.value?.building?.code
  return buildingCode
    ? props.developmentState?.adjustments.filter(item => item.building_code === buildingCode) ?? []
    : []
})
const addedFloors = computed(() => buildingAdjustments.value.reduce((total, item) => total + item.added_floor_count, 0))
const addedCapacity = computed(() => buildingAdjustments.value.reduce((total, item) => total + item.added_capacity_units, 0))
const qualityGain = computed(() => buildingAdjustments.value.reduce((total, item) => total + item.quality_delta_milli, 0))

function formatInteger(value: number): string {
  return new Intl.NumberFormat(locale.value, { maximumFractionDigits: 0 }).format(value)
}

function formatMilli(value: number): string {
  return new Intl.NumberFormat(locale.value, {
    style: 'percent',
    minimumFractionDigits: 0,
    maximumFractionDigits: 1
  }).format(value / 1000)
}

function enterprisePoolAvailability(poolCode: string): string {
  const pool = props.enterpriseLocationState?.pools.find(item => item.code === poolCode)
  return pool
    ? t('citySpatial.enterprise.inspector.poolCapacity', {
        occupied: formatInteger(pool.occupied_unit_count),
        effective: formatInteger(pool.effective_unit_count)
      })
    : poolCode
}

function maskLabel(mask: number): string {
  if (!mask) return `0000 · ${t('citySpatial.inspector.none')}`
  const directions = [
    [1, 'N'], [2, 'E'], [4, 'S'], [8, 'W']
  ] as const
  return `${mask.toString(2).padStart(4, '0')} · ${directions.filter(([bit]) => (mask & bit) !== 0).map(([, name]) => name).join(' ')}`
}
</script>

<style scoped>
.city-inspector {
  display: flex;
  min-width: 0;
  min-height: 31rem;
  flex-direction: column;
  border: 1px solid var(--ui-separator);
  background: var(--ui-surface);
  color: var(--ui-label);
}

.city-panel-header {
  display: flex;
  min-height: 4.5rem;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  border-bottom: 1px solid var(--ui-separator);
  padding: 0.9rem 1rem;
}

.city-panel-header h2 {
  margin: 0.15rem 0 0;
  font-size: 0.95rem;
  font-weight: 700;
}

.city-panel-eyebrow,
.city-section-label,
.city-inspector-mode,
.city-layer-kind {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.65rem;
  letter-spacing: 0.12em;
  text-transform: uppercase;
}

.city-panel-eyebrow {
  margin: 0;
  color: var(--ui-label-secondary);
}

.city-inspector-mode {
  border: 1px solid var(--ui-separator);
  padding: 0.25rem 0.4rem;
  color: var(--ui-accent);
}

.city-inspector-content {
  min-height: 0;
  flex: 1;
  overflow: auto;
  padding: 1rem;
}

.city-coordinate-block {
  border-left: 3px solid var(--ui-accent);
  padding: 0.25rem 0 0.25rem 0.8rem;
}

.city-coordinate-block span,
.city-hash-block span {
  display: block;
  color: var(--ui-label-secondary);
  font-size: 0.7rem;
}

.city-coordinate-block strong {
  display: block;
  margin-top: 0.15rem;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 1.05rem;
}

.city-data-list {
  display: grid;
  gap: 0;
  margin: 1rem 0 0;
  border-top: 1px solid var(--ui-separator);
}

.city-data-list div {
  display: grid;
  grid-template-columns: minmax(6.5rem, 0.8fr) minmax(0, 1.2fr);
  gap: 0.75rem;
  border-bottom: 1px solid var(--ui-separator);
  padding: 0.65rem 0;
}

.city-data-list dt {
  color: var(--ui-label-secondary);
  font-size: 0.75rem;
}

.city-data-list dd {
  min-width: 0;
  margin: 0;
  overflow-wrap: anywhere;
  font-size: 0.78rem;
  text-align: right;
}

.city-state-ready { color: #16a36a; }
.city-state-pending { color: #d97706; }

.city-stack-section { margin-top: 1.2rem; }
.city-land-section { margin-top: 1.2rem; }

.city-section-label {
  display: flex;
  justify-content: space-between;
  margin-bottom: 0.5rem;
  color: var(--ui-label-secondary);
}

.city-layer-card {
  display: grid;
  grid-template-columns: 2rem minmax(0, 1fr) auto;
  gap: 0.65rem;
  border: 1px solid var(--ui-separator);
  padding: 0.75rem;
}

.city-layer-card + .city-layer-card { border-top: 0; }

.city-layer-glyph {
  display: grid;
  height: 2rem;
  place-items: center;
  background: #111318;
  color: #f5f5f5;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 1.1rem;
}

.city-layer-copy { min-width: 0; }
.city-layer-copy strong { display: block; font-size: 0.8rem; }
.city-layer-copy code { display: block; overflow: hidden; color: var(--ui-label-secondary); font-size: 0.66rem; text-overflow: ellipsis; white-space: nowrap; }
.city-layer-kind { align-self: start; color: var(--ui-label-secondary); }

.city-use-list {
  display: flex;
  flex-wrap: wrap;
  gap: 0.35rem;
  margin-bottom: 0.55rem;
}

.city-use-list span {
  border: 1px solid var(--ui-separator);
  border-left-width: 3px;
  padding: 0.22rem 0.42rem;
  color: var(--ui-label-secondary);
  font-size: 0.68rem;
}

.city-use-list span[data-use='residential'], .city-land-glyph[data-use='residential'] { border-left-color: #d6c6a5; color: #9b7d43; }
.city-use-list span[data-use='commercial'], .city-land-glyph[data-use='commercial'] { border-left-color: #6fa8c7; color: #397b9f; }
.city-use-list span[data-use='industrial'], .city-land-glyph[data-use='industrial'] { border-left-color: #c58b57; color: #9a5c25; }

.city-building-list,
.city-development-list,
.city-pool-list,
.city-allocation-list {
  border: 1px solid var(--ui-separator);
}

.city-building-list article,
.city-development-list article,
.city-pool-list > div,
.city-allocation-list > div {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  padding: 0.55rem 0.65rem;
}

.city-building-list article + article,
.city-development-list article + article,
.city-pool-list > div + div,
.city-allocation-list > div + div { border-top: 1px solid var(--ui-separator); }
.city-building-list strong, .city-building-list span { display: block; }
.city-building-list strong { overflow-wrap: anywhere; font: 0.7rem ui-monospace, monospace; }
.city-building-list span, .city-building-list small { color: var(--ui-label-secondary); font-size: 0.64rem; }

.city-development-list article {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 6.5rem;
}

.city-development-list strong,
.city-development-list small { display: block; min-width: 0; overflow-wrap: anywhere; }
.city-development-list strong { font-size: 0.7rem; }
.city-development-list small { margin-top: 0.15rem; color: var(--ui-label-secondary); font: 0.6rem ui-monospace, monospace; }
.city-development-state { display: grid; justify-items: end; gap: 0.3rem; }
.city-development-state span { font-size: 0.62rem; }
.city-development-state span[data-status='under_construction'] { color: #d99b52; }
.city-development-state span[data-status='completed'] { color: #16a36a; }
.city-development-state progress { width: 6rem; height: 0.3rem; accent-color: var(--ui-accent); }

.city-enterprise-inspector-list { border: 1px solid var(--ui-separator); }
.city-enterprise-inspector-list article {
  display: grid;
  grid-template-columns: 2rem minmax(0, 1fr) auto;
  align-items: center;
  gap: 0.65rem;
  padding: 0.55rem 0.65rem;
}
.city-enterprise-inspector-list article + article { border-top: 1px solid var(--ui-separator); }
.city-enterprise-inspector-list strong,
.city-enterprise-inspector-list small,
.city-enterprise-inspector-list code { display: block; min-width: 0; overflow-wrap: anywhere; }
.city-enterprise-inspector-list strong { font-size: 0.68rem; }
.city-enterprise-inspector-list small { margin-top: 0.1rem; color: var(--ui-label-secondary); font-size: 0.6rem; }
.city-enterprise-inspector-list code { margin-top: 0.12rem; color: var(--ui-label-secondary); font-size: 0.58rem; }
.city-enterprise-inspector-list article > b { font: 0.7rem ui-monospace, monospace; }
.city-enterprise-building-list { margin-top: 0.65rem; }
.city-enterprise-capacity { display: grid; justify-items: end; gap: 0.12rem; }
.city-enterprise-capacity b { font: 0.68rem ui-monospace, monospace; }
.city-enterprise-capacity small { white-space: nowrap; }

.city-land-card {
  border: 1px solid var(--ui-separator);
  padding: 0.7rem;
}

.city-land-card + .city-land-card,
.city-land-card + .city-portal-card,
.city-portal-card + .city-portal-card,
.city-portal-card + .city-allocation-list { border-top: 0; }

.city-land-card > header,
.city-portal-card {
  display: grid;
  grid-template-columns: 2rem minmax(0, 1fr);
  align-items: center;
  gap: 0.65rem;
}

.city-land-card header strong,
.city-land-card header small,
.city-portal-card strong,
.city-portal-card small { display: block; min-width: 0; overflow-wrap: anywhere; }
.city-land-card header strong, .city-portal-card strong { font: 0.72rem ui-monospace, monospace; }
.city-land-card header small, .city-portal-card small { margin-top: 0.12rem; color: var(--ui-label-secondary); font-size: 0.63rem; line-height: 1.45; }

.city-land-glyph {
  display: grid;
  height: 2rem;
  place-items: center;
  border-left: 3px solid #f0c674;
  background: var(--ui-control);
  color: var(--ui-label);
  font: 1rem ui-monospace, monospace;
}

.city-land-card dl {
  display: grid;
  grid-template-columns: 1fr 1fr;
  margin: 0.65rem 0 0;
  border-top: 1px solid var(--ui-separator);
}

.city-land-card dl div { padding: 0.45rem 0.2rem 0; }
.city-land-card dl dt { color: var(--ui-label-secondary); font-size: 0.62rem; }
.city-land-card dl dd { margin: 0.12rem 0 0; font: 0.68rem ui-monospace, monospace; }
.city-pool-list { margin-top: 0.65rem; }
.city-pool-list span, .city-allocation-list code { min-width: 0; overflow: hidden; font: 0.61rem ui-monospace, monospace; text-overflow: ellipsis; white-space: nowrap; }
.city-pool-list strong, .city-allocation-list strong { flex: none; font: 0.65rem ui-monospace, monospace; }

.city-building-projects {
  margin-top: 0.65rem;
  border: 1px solid var(--ui-separator);
}

.city-building-projects > div {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  padding: 0.5rem 0.6rem;
}

.city-building-projects > div + div { border-top: 1px solid var(--ui-separator); }
.city-building-projects strong,
.city-building-projects small { display: block; }
.city-building-projects strong { font: 0.65rem ui-monospace, monospace; }
.city-building-projects small { margin-top: 0.1rem; color: var(--ui-label-secondary); font-size: 0.6rem; }
.city-project-progress { display: grid; min-width: 5rem; justify-items: end; gap: 0.2rem; }
.city-project-progress b { font: 0.62rem ui-monospace, monospace; }
.city-project-progress progress { width: 5rem; height: 0.25rem; accent-color: #d99b52; }

.city-adjustment-summary { margin-top: 0.65rem !important; }

.city-portal-card {
  border: 1px solid var(--ui-separator);
  padding: 0.65rem;
}

.city-allocation-list { margin-top: 0.65rem; }
.city-allocation-list summary { cursor: pointer; padding: 0.55rem 0.65rem; color: var(--ui-label-secondary); font-size: 0.66rem; }

.city-flag-list {
  display: flex;
  grid-column: 2 / -1;
  flex-wrap: wrap;
  gap: 0.25rem;
}

.city-flag-list span {
  border: 1px solid var(--ui-separator);
  padding: 0.12rem 0.28rem;
  color: var(--ui-label-secondary);
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.6rem;
}

.city-movement-cost {
  grid-column: 2 / -1;
  margin: 0;
  color: var(--ui-label-secondary);
  font-size: 0.7rem;
}

.city-hash-block {
  margin-top: 1rem;
  border: 1px solid var(--ui-separator);
  padding: 0.65rem;
}

.city-hash-block code {
  display: block;
  margin-top: 0.35rem;
  overflow-wrap: anywhere;
  color: var(--ui-label-secondary);
  font-size: 0.62rem;
  line-height: 1.5;
}

.city-inspector-empty {
  display: grid;
  justify-items: center;
  gap: 0.4rem;
  padding: 2.5rem 1rem;
  color: var(--ui-label-secondary);
  text-align: center;
}

.city-inspector-empty-main { margin: auto 0; }
.city-inspector-empty strong { color: var(--ui-label); font-size: 0.85rem; }
.city-inspector-empty p { max-width: 16rem; margin: 0; font-size: 0.75rem; line-height: 1.6; }
</style>
