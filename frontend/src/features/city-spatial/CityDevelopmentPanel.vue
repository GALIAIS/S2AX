<template>
  <section class="city-development-panel" aria-labelledby="city-development-title">
    <header class="city-development-header">
      <div>
        <p>{{ t('citySpatial.development.eyebrow') }}</p>
        <h2 id="city-development-title">{{ t('citySpatial.development.title') }}</h2>
        <span>{{ t('citySpatial.development.description') }}</span>
      </div>
      <button
        v-if="owner && state"
        type="button"
        class="btn btn-primary btn-sm"
        :disabled="!selectedBuilding || Boolean(busyProjectCode)"
        @click="openSubmitDialog"
      >
        <Icon name="plus" size="sm" />
        {{ t('citySpatial.development.newProject') }}
      </button>
    </header>

    <div v-if="!state" class="city-development-empty">
      <span aria-hidden="true">%</span>
      <p>{{ t('citySpatial.development.unavailable') }}</p>
    </div>

    <template v-else>
      <div class="city-development-summary">
        <div><span>PROJECTS</span><strong>{{ state.profile.project_count }}</strong></div>
        <div><span>ACTIVE</span><strong>{{ activeCount }}</strong></div>
        <div><span>FACTS</span><strong>{{ state.profile.fact_count }}</strong></div>
        <div><span>ADJUSTMENTS</span><strong>{{ state.profile.adjustment_count }}</strong></div>
        <code>{{ state.profile.policy_id }}@{{ state.profile.policy_version }}</code>
      </div>

      <nav class="city-development-tabs" :aria-label="t('citySpatial.development.title')">
        <button
          v-for="tab in statusTabs"
          :key="tab.value"
          type="button"
          :class="{ active: statusFilter === tab.value }"
          @click="statusFilter = tab.value"
        >
          {{ tab.label }}
          <span>{{ tab.count }}</span>
        </button>
      </nav>

      <div v-if="filteredProjects.length" class="city-development-grid">
        <article v-for="project in filteredProjects" :key="project.code" class="city-development-project">
          <header>
            <div>
              <span class="city-project-code">{{ project.code }}</span>
              <h3>{{ project.name || project.code }}</h3>
              <p>{{ project.building_code }} · {{ t(`citySpatial.development.type.${project.project_type}`) }}</p>
            </div>
            <span class="city-project-status" :data-status="project.status">
              {{ t(`citySpatial.development.status.${project.status}`) }}
            </span>
          </header>

          <div class="city-project-progress-block">
            <div>
              <span>{{ t('citySpatial.development.progress') }}</span>
              <strong>{{ formatPercent(project.progress_milli) }}</strong>
            </div>
            <progress :value="project.progress_milli" max="1000" />
          </div>

          <dl>
            <div><dt>{{ t('citySpatial.development.basicMaterial') }}</dt><dd>{{ formatInteger(project.required_basic_material_units) }}</dd></div>
            <div><dt>{{ t('citySpatial.development.capitalGoods') }}</dt><dd>{{ formatInteger(project.required_capital_goods_units) }}</dd></div>
            <div><dt>{{ t('citySpatial.development.labor') }}</dt><dd>{{ formatInteger(project.required_labor_units) }}</dd></div>
            <div><dt>{{ t('citySpatial.development.duration') }}</dt><dd>{{ t('citySpatial.development.ticks', { count: project.planned_duration_ticks }) }}</dd></div>
          </dl>

          <footer>
            <span>{{ t('citySpatial.development.submittedAt', { tick: project.submitted_tick }) }}</span>
            <span v-if="project.planned_completion_tick">{{ t('citySpatial.development.completionAt', { tick: project.planned_completion_tick }) }}</span>
            <div v-if="owner && projectActions(project).length" class="city-project-actions">
              <button
                v-for="action in projectActions(project)"
                :key="action"
                type="button"
                class="btn btn-secondary btn-sm"
                :disabled="Boolean(busyProjectCode)"
                @click="handleProjectAction(project, action)"
              >
                {{ t(`citySpatial.development.action.${action}`) }}
              </button>
            </div>
          </footer>
        </article>
      </div>
      <div v-else class="city-development-empty city-development-empty-list">
        <span aria-hidden="true">·</span>
        <p>{{ t('citySpatial.development.noProjects') }}</p>
      </div>
    </template>

    <BaseDialog
      :show="showSubmitDialog"
      :title="t('citySpatial.development.newProject')"
      width="normal"
      @close="showSubmitDialog = false"
    >
      <form class="city-development-form" @submit.prevent="submitProject">
        <div class="city-development-target">
          <span>{{ t('citySpatial.development.selectedBuilding') }}</span>
          <strong>{{ selectedBuilding?.code ?? '—' }}</strong>
          <small v-if="selectedBuilding">
            {{ t(`citySpatial.landUse.${selectedBuilding.primary_use}`) }} ·
            {{ selectedBuilding.floor_count }}F · {{ formatInteger(selectedBuilding.capacity_units) }}
          </small>
          <small v-else>{{ t('citySpatial.development.selectBuildingHint') }}</small>
        </div>

        <div class="city-development-type-tabs">
          <button
            v-for="projectType in projectTypes"
            :key="projectType"
            type="button"
            :class="{ active: submitForm.projectType === projectType }"
            @click="submitForm.projectType = projectType"
          >
            {{ t(`citySpatial.development.type.${projectType}`) }}
          </button>
        </div>

        <div class="city-development-form-grid">
          <label>
            <span>{{ t('citySpatial.development.projectName') }}</span>
            <input v-model.trim="submitForm.name" class="input" maxlength="128" :placeholder="t('citySpatial.development.projectNamePlaceholder')" />
          </label>
          <label>
            <span>{{ t('citySpatial.development.developer') }}</span>
            <Select v-model="submitForm.developerID" :options="developerOptions" :searchable="developerOptions.length > 6" />
          </label>
          <label v-if="submitForm.projectType === 'vertical_expansion'">
            <span>{{ t('citySpatial.development.targetFloors') }}</span>
            <input v-model.number="submitForm.targetFloorCount" class="input font-mono" type="number" :min="minimumTargetFloor" :max="selectedZoningRule?.max_floors ?? 128" />
          </label>
          <label v-else>
            <span>{{ t('citySpatial.development.targetQuality') }}</span>
            <input v-model.number="submitForm.targetQualityMilli" class="input font-mono" type="number" :min="minimumTargetQuality" max="1500" step="1" />
            <small>{{ t('citySpatial.development.targetQualityHint') }}</small>
          </label>
        </div>

        <section class="city-development-estimate">
          <header>
            <span>{{ t('citySpatial.development.resources') }}</span>
            <code>{{ state?.profile.policy_id }}@{{ state?.profile.policy_version }}</code>
          </header>
          <dl>
            <div><dt>{{ t('citySpatial.development.basicMaterial') }}</dt><dd>{{ estimate ? formatInteger(estimate.material) : '—' }}</dd></div>
            <div><dt>{{ t('citySpatial.development.capitalGoods') }}</dt><dd>{{ estimate ? formatInteger(estimate.capital) : '—' }}</dd></div>
            <div><dt>{{ t('citySpatial.development.labor') }}</dt><dd>{{ estimate ? formatInteger(estimate.labor) : '—' }}</dd></div>
            <div><dt>{{ t('citySpatial.development.duration') }}</dt><dd>{{ estimate ? t('citySpatial.development.ticks', { count: estimate.duration }) : '—' }}</dd></div>
          </dl>
          <p>{{ t('citySpatial.development.serverAuthoritative') }}</p>
        </section>
      </form>
      <template #footer>
        <button type="button" class="btn btn-secondary" @click="showSubmitDialog = false">{{ t('common.cancel') }}</button>
        <button type="button" class="btn btn-primary" :disabled="!canSubmit || Boolean(busyProjectCode)" @click="submitProject">
          {{ busyProjectCode ? t('citySpatial.development.action.processing') : t('citySpatial.development.action.submit') }}
        </button>
      </template>
    </BaseDialog>

    <BaseDialog
      :show="Boolean(pendingAction)"
      :title="pendingAction ? t(`citySpatial.development.action.${pendingAction.action}`) : ''"
      width="narrow"
      @close="pendingAction = null"
    >
      <div class="city-development-action-form">
        <p>{{ t('citySpatial.development.actionPrompt') }}</p>
        <label>
          <span>{{ pendingAction?.action === 'reject' ? t('citySpatial.development.reviewNote') : t('citySpatial.development.cancellationReason') }}</span>
          <textarea v-model.trim="actionReason" class="input" rows="4" maxlength="256" />
        </label>
      </div>
      <template #footer>
        <button type="button" class="btn btn-secondary" @click="pendingAction = null">{{ t('common.cancel') }}</button>
        <button type="button" class="btn btn-primary" :disabled="!canConfirmAction || Boolean(busyProjectCode)" @click="confirmPendingAction">
          {{ t('citySpatial.development.action.confirm') }}
        </button>
      </template>
    </BaseDialog>
  </section>
</template>

<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type {
  CityBuilding,
  CityDevelopmentProject,
  CityDevelopmentProjectType,
  CityDevelopmentState,
  CityDevelopmentStatus,
  CityLandState
} from '@/api/citySpatial'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select, { type SelectOption } from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'

type ProjectAction = 'approve' | 'reject' | 'start' | 'cancel'
type StatusFilter = 'all' | 'active' | CityDevelopmentStatus

const props = defineProps<{
  state: CityDevelopmentState | null
  landState: CityLandState | null
  selectedBuildingCode: string | null
  owner: boolean
  busyProjectCode: string | null
}>()

const emit = defineEmits<{
  (event: 'command', value: {
    commandType: 'development.submit' | 'development.review' | 'development.start' | 'development.cancel'
    payload: Record<string, unknown>
    projectCode: string
  }): void
}>()

const { t, locale } = useI18n()
const statusFilter = ref<StatusFilter>('all')
const showSubmitDialog = ref(false)
const pendingAction = ref<{ project: CityDevelopmentProject; action: 'reject' | 'cancel' } | null>(null)
const actionReason = ref('')
const projectTypes: CityDevelopmentProjectType[] = ['vertical_expansion', 'renovation']
const submitForm = reactive({
  projectType: 'vertical_expansion' as CityDevelopmentProjectType,
  developerID: null as number | null,
  targetFloorCount: 1,
  targetQualityMilli: 1050,
  name: ''
})

const selectedBuilding = computed<CityBuilding | null>(() => (
  props.landState?.buildings.find(building => building.code === props.selectedBuildingCode) ?? null
))
const selectedParcel = computed(() => props.landState?.parcels.find(
  parcel => parcel.code === selectedBuilding.value?.parcel_code
) ?? null)
const selectedZoningRule = computed(() => props.landState?.zoning_rules.find(
  rule => rule.code === selectedParcel.value?.zone_code
) ?? null)
const minimumTargetFloor = computed(() => (selectedBuilding.value?.floor_count ?? 0) + 1)
const minimumTargetQuality = computed(() => (selectedBuilding.value?.quality_milli ?? 1000) + 1)
const eligibleDevelopers = computed(() => props.state?.developers.filter(developer => (
  developer.district_code === selectedBuilding.value?.district_code
)) ?? [])
const developerOptions = computed<SelectOption[]>(() => eligibleDevelopers.value.map(developer => ({
  value: developer.entity_id,
  label: `${developer.entity_name} · ${developer.available_labor_units}/${developer.employee_units}`
})))
const activeCount = computed(() => props.state?.projects.filter(project => (
  project.status === 'submitted' || project.status === 'approved' || project.status === 'under_construction'
)).length ?? 0)
const statusTabs = computed(() => {
  const projects = props.state?.projects ?? []
  const count = (status: StatusFilter): number => status === 'all'
    ? projects.length
    : status === 'active'
      ? activeCount.value
      : projects.filter(project => project.status === status).length
  const tabs: Array<{ value: StatusFilter; label: string }> = [
    { value: 'all', label: t('citySpatial.development.all') },
    { value: 'active', label: t('citySpatial.development.active') },
    { value: 'submitted', label: t('citySpatial.development.status.submitted') },
    { value: 'approved', label: t('citySpatial.development.status.approved') },
    { value: 'under_construction', label: t('citySpatial.development.status.under_construction') },
    { value: 'completed', label: t('citySpatial.development.status.completed') }
  ]
  return tabs.map(tab => ({ ...tab, count: count(tab.value) }))
})
const filteredProjects = computed(() => {
  const projects = props.state?.projects ?? []
  if (statusFilter.value === 'all') return projects
  if (statusFilter.value === 'active') return projects.filter(project => (
    project.status === 'submitted' || project.status === 'approved' || project.status === 'under_construction'
  ))
  return projects.filter(project => project.status === statusFilter.value)
})
const estimate = computed(() => {
  const building = selectedBuilding.value
  const rule = selectedZoningRule.value
  if (!building || !rule) return null
  if (submitForm.projectType === 'vertical_expansion') {
    const addedFloors = Math.trunc(submitForm.targetFloorCount) - building.floor_count
    if (addedFloors <= 0 || submitForm.targetFloorCount > rule.max_floors) return null
    const area = building.footprint_area_sqm * addedFloors
    if (building.floor_area_sqm + area > selectedParcel.value!.area_sqm * rule.max_floor_area_ratio_milli / 1000) return null
    const labor = Math.max(1, Math.ceil(area / 5_000))
    return {
      material: Math.max(1, Math.ceil(area / 1_000)), capital: Math.max(1, Math.ceil(area / 10_000)),
      labor, duration: Math.max(2, Math.min(720, Math.ceil(labor / 8)))
    }
  }
  const delta = Math.trunc(submitForm.targetQualityMilli) - building.quality_milli
  if (delta <= 0 || submitForm.targetQualityMilli > 1500) return null
  const weightedArea = building.floor_area_sqm * delta
  const labor = Math.max(1, Math.ceil(weightedArea / 100_000))
  return {
    material: Math.max(1, Math.ceil(weightedArea / 50_000)),
    capital: Math.max(1, Math.ceil(weightedArea / 200_000)),
    labor, duration: Math.max(1, Math.min(360, Math.ceil(labor / 8)))
  }
})
const canSubmit = computed(() => Boolean(
  selectedBuilding.value && estimate.value && submitForm.developerID &&
  eligibleDevelopers.value.some(developer => developer.entity_id === submitForm.developerID)
))
const canConfirmAction = computed(() => Boolean(
  pendingAction.value && (pendingAction.value.action === 'reject' || actionReason.value.length > 0)
))

function openSubmitDialog(): void {
  if (!selectedBuilding.value) return
  submitForm.projectType = 'vertical_expansion'
  submitForm.developerID = eligibleDevelopers.value[0]?.entity_id ?? null
  submitForm.targetFloorCount = minimumTargetFloor.value
  submitForm.targetQualityMilli = Math.min(1500, selectedBuilding.value.quality_milli + 50)
  submitForm.name = ''
  showSubmitDialog.value = true
}

function submitProject(): void {
  if (!canSubmit.value || !selectedBuilding.value || !submitForm.developerID) return
  const payload: Record<string, unknown> = {
    project_type: submitForm.projectType,
    building_code: selectedBuilding.value.code,
    developer_entity_id: submitForm.developerID
  }
  if (submitForm.name) payload.name = submitForm.name
  if (submitForm.projectType === 'vertical_expansion') payload.target_floor_count = Math.trunc(submitForm.targetFloorCount)
  else payload.target_quality_milli = Math.trunc(submitForm.targetQualityMilli)
  emit('command', { commandType: 'development.submit', payload, projectCode: 'new' })
  showSubmitDialog.value = false
}

function projectActions(project: CityDevelopmentProject): ProjectAction[] {
  if (project.status === 'submitted') return ['approve', 'reject', 'cancel']
  if (project.status === 'approved') return ['start', 'cancel']
  if (project.status === 'under_construction') return ['cancel']
  return []
}

function handleProjectAction(project: CityDevelopmentProject, action: ProjectAction): void {
  if (action === 'approve') {
    emit('command', {
      commandType: 'development.review',
      payload: { project_code: project.code, decision: 'approve' },
      projectCode: project.code
    })
    return
  }
  if (action === 'start') {
    emit('command', {
      commandType: 'development.start', payload: { project_code: project.code }, projectCode: project.code
    })
    return
  }
  actionReason.value = ''
  pendingAction.value = { project, action }
}

function confirmPendingAction(): void {
  const pending = pendingAction.value
  if (!pending || !canConfirmAction.value) return
  if (pending.action === 'reject') {
    emit('command', {
      commandType: 'development.review',
      payload: { project_code: pending.project.code, decision: 'reject', note: actionReason.value },
      projectCode: pending.project.code
    })
  } else {
    emit('command', {
      commandType: 'development.cancel',
      payload: { project_code: pending.project.code, reason: actionReason.value },
      projectCode: pending.project.code
    })
  }
  pendingAction.value = null
}

function formatInteger(value: number): string {
  return new Intl.NumberFormat(locale.value, { maximumFractionDigits: 0 }).format(value)
}

function formatPercent(value: number): string {
  return new Intl.NumberFormat(locale.value, { style: 'percent', maximumFractionDigits: 1 }).format(value / 1000)
}
</script>

<style scoped>
.city-development-panel { margin-top: 1rem; border: 1px solid var(--ui-separator); background: var(--ui-surface); }
.city-development-header { display: flex; align-items: center; justify-content: space-between; gap: 1rem; border-bottom: 1px solid var(--ui-separator); padding: 1rem; }
.city-development-header p { margin: 0; color: var(--ui-accent); font: 0.62rem ui-monospace, monospace; letter-spacing: 0.12em; text-transform: uppercase; }
.city-development-header h2 { margin: 0.2rem 0 0.15rem; font-size: 1rem; }
.city-development-header > div > span { color: var(--ui-label-secondary); font-size: 0.75rem; }
.city-development-summary { display: grid; grid-template-columns: repeat(4, minmax(5rem, 0.6fr)) minmax(12rem, 1fr); border-bottom: 1px solid var(--ui-separator); }
.city-development-summary > div { border-right: 1px solid var(--ui-separator); padding: 0.7rem 0.85rem; }
.city-development-summary span { display: block; color: var(--ui-label-secondary); font: 0.58rem ui-monospace, monospace; letter-spacing: 0.08em; }
.city-development-summary strong { display: block; margin-top: 0.15rem; font: 1rem ui-monospace, monospace; }
.city-development-summary code { align-self: center; justify-self: end; padding: 0 0.85rem; color: var(--ui-label-secondary); font-size: 0.65rem; }
.city-development-tabs { display: flex; overflow-x: auto; border-bottom: 1px solid var(--ui-separator); padding: 0 1rem; }
.city-development-tabs button { display: flex; min-height: 2.65rem; flex: none; align-items: center; gap: 0.45rem; border-bottom: 2px solid transparent; padding: 0 0.75rem; color: var(--ui-label-secondary); font-size: 0.72rem; }
.city-development-tabs button.active { border-bottom-color: var(--ui-accent); color: var(--ui-label); }
.city-development-tabs button span { min-width: 1.2rem; padding: 0.08rem 0.25rem; background: var(--ui-control); font: 0.58rem ui-monospace, monospace; text-align: center; }
.city-development-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(23rem, 1fr)); gap: 1px; background: var(--ui-separator); }
.city-development-project { min-width: 0; padding: 1rem; background: var(--ui-surface); }
.city-development-project > header { display: flex; align-items: flex-start; justify-content: space-between; gap: 0.75rem; }
.city-project-code { color: var(--ui-accent); font: 0.6rem ui-monospace, monospace; }
.city-development-project h3 { margin: 0.22rem 0 0; font-size: 0.86rem; }
.city-development-project header p { margin: 0.2rem 0 0; color: var(--ui-label-secondary); font: 0.62rem ui-monospace, monospace; overflow-wrap: anywhere; }
.city-project-status { flex: none; border-left: 3px solid var(--ui-separator); padding: 0.25rem 0.4rem; color: var(--ui-label-secondary); font-size: 0.65rem; }
.city-project-status[data-status='under_construction'] { border-left-color: #d99b52; color: #b36f27; }
.city-project-status[data-status='completed'] { border-left-color: #16a36a; color: #16865a; }
.city-project-status[data-status='rejected'], .city-project-status[data-status='cancelled'] { opacity: 0.65; }
.city-project-progress-block { margin-top: 0.9rem; }
.city-project-progress-block > div { display: flex; justify-content: space-between; color: var(--ui-label-secondary); font-size: 0.65rem; }
.city-project-progress-block strong { color: var(--ui-label); font: 0.65rem ui-monospace, monospace; }
.city-project-progress-block progress { width: 100%; height: 0.35rem; margin-top: 0.35rem; accent-color: var(--ui-accent); }
.city-development-project dl, .city-development-estimate dl { display: grid; grid-template-columns: 1fr 1fr; margin: 0.8rem 0 0; border: 1px solid var(--ui-separator); }
.city-development-project dl div, .city-development-estimate dl div { padding: 0.5rem 0.6rem; }
.city-development-project dl div:nth-child(even), .city-development-estimate dl div:nth-child(even) { border-left: 1px solid var(--ui-separator); }
.city-development-project dl div:nth-child(n+3), .city-development-estimate dl div:nth-child(n+3) { border-top: 1px solid var(--ui-separator); }
.city-development-project dt, .city-development-estimate dt { color: var(--ui-label-secondary); font-size: 0.6rem; }
.city-development-project dd, .city-development-estimate dd { margin: 0.12rem 0 0; font: 0.68rem ui-monospace, monospace; }
.city-development-project > footer { display: flex; min-height: 2rem; align-items: end; gap: 0.6rem; margin-top: 0.75rem; color: var(--ui-label-secondary); font-size: 0.6rem; }
.city-project-actions { display: flex; flex-wrap: wrap; gap: 0.35rem; margin-left: auto; }
.city-development-empty { display: grid; min-height: 10rem; place-content: center; justify-items: center; gap: 0.5rem; color: var(--ui-label-secondary); }
.city-development-empty > span { color: var(--ui-accent); font: 1.5rem ui-monospace, monospace; }
.city-development-empty p { margin: 0; font-size: 0.75rem; }
.city-development-empty-list { min-height: 13rem; }
.city-development-form { display: grid; gap: 1rem; }
.city-development-target { border-left: 3px solid var(--ui-accent); padding: 0.2rem 0.75rem; }
.city-development-target span, .city-development-target strong, .city-development-target small { display: block; }
.city-development-target span { color: var(--ui-label-secondary); font-size: 0.65rem; }
.city-development-target strong { margin-top: 0.12rem; font: 0.78rem ui-monospace, monospace; }
.city-development-target small { margin-top: 0.15rem; color: var(--ui-label-secondary); font-size: 0.64rem; }
.city-development-type-tabs { display: grid; grid-template-columns: 1fr 1fr; border: 1px solid var(--ui-separator); }
.city-development-type-tabs button { min-height: 2.65rem; color: var(--ui-label-secondary); background: var(--ui-control); font-size: 0.75rem; }
.city-development-type-tabs button + button { border-left: 1px solid var(--ui-separator); }
.city-development-type-tabs button.active { color: #fff; background: var(--ui-accent); }
.city-development-form-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 0.85rem; }
.city-development-form-grid label > span, .city-development-action-form label > span { display: block; margin-bottom: 0.35rem; color: var(--ui-label-secondary); font-size: 0.68rem; }
.city-development-form-grid label > small { display: block; margin-top: 0.3rem; color: var(--ui-label-secondary); font-size: 0.6rem; }
.city-development-estimate { border: 1px solid var(--ui-separator); padding: 0.8rem; }
.city-development-estimate > header { display: flex; justify-content: space-between; gap: 0.8rem; font-size: 0.7rem; }
.city-development-estimate header code { color: var(--ui-label-secondary); font-size: 0.6rem; }
.city-development-estimate p { margin: 0.65rem 0 0; color: var(--ui-label-secondary); font-size: 0.62rem; }
.city-development-action-form p { margin: 0 0 1rem; color: var(--ui-label-secondary); font-size: 0.75rem; }
.city-development-action-form textarea { min-height: 6rem; resize: vertical; }
@media (max-width: 820px) {
  .city-development-header { align-items: flex-start; flex-direction: column; }
  .city-development-summary { grid-template-columns: repeat(2, 1fr); }
  .city-development-summary code { grid-column: 1 / -1; justify-self: start; padding: 0.65rem 0.85rem; }
  .city-development-form-grid { grid-template-columns: 1fr; }
}
</style>
