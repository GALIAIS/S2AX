<template>
  <section class="city-enterprise-panel" aria-labelledby="city-enterprise-title">
    <header class="city-enterprise-header">
      <div>
        <p>{{ t('citySpatial.enterprise.eyebrow') }}</p>
        <h2 id="city-enterprise-title">{{ t('citySpatial.enterprise.title') }}</h2>
        <span>{{ t('citySpatial.enterprise.description') }}</span>
      </div>
      <button
        v-if="owner && state"
        type="button"
        class="btn btn-primary btn-sm"
        :disabled="Boolean(busyCommandCode) || state.firms.length === 0"
        @click="openCreateDialog"
      >
        <Icon name="plus" size="sm" />
        {{ t('citySpatial.enterprise.action.open') }}
      </button>
    </header>

    <div v-if="!state" class="city-enterprise-empty">
      <span aria-hidden="true">⌂</span>
      <p>{{ t('citySpatial.enterprise.unavailable') }}</p>
    </div>

    <template v-else>
      <div class="city-enterprise-summary">
        <div><span>SITES</span><strong>{{ state.profile.site_count }}</strong></div>
        <div><span>ACTIVE</span><strong>{{ activeSites.length }}</strong></div>
        <div><span>OCCUPIED</span><strong>{{ formatInteger(occupiedUnits) }}</strong></div>
        <div><span>AVAILABLE</span><strong>{{ formatInteger(availableUnits) }}</strong></div>
        <div><span>FACTS</span><strong>{{ state.profile.fact_count }}</strong></div>
        <code>{{ state.profile.policy_id }}@{{ state.profile.policy_version }}</code>
      </div>

      <div class="city-enterprise-filters">
        <label>
          <span>{{ t('citySpatial.enterprise.filter.firm') }}</span>
          <Select v-model="firmFilter" :options="firmFilterOptions" :searchable="state.firms.length > 6" />
        </label>
        <label>
          <span>{{ t('citySpatial.enterprise.filter.district') }}</span>
          <Select v-model="districtFilter" :options="districtFilterOptions" :searchable="false" />
        </label>
        <label>
          <span>{{ t('citySpatial.enterprise.filter.type') }}</span>
          <Select v-model="typeFilter" :options="typeFilterOptions" :searchable="false" />
        </label>
        <label>
          <span>{{ t('citySpatial.enterprise.filter.status') }}</span>
          <Select v-model="statusFilter" :options="statusFilterOptions" :searchable="false" />
        </label>
      </div>

      <div class="city-enterprise-table-wrap">
        <table class="city-enterprise-table">
          <thead>
            <tr>
              <th>{{ t('citySpatial.enterprise.columns.site') }}</th>
              <th>{{ t('citySpatial.enterprise.columns.firm') }}</th>
              <th>{{ t('citySpatial.enterprise.columns.location') }}</th>
              <th>{{ t('citySpatial.enterprise.columns.capacity') }}</th>
              <th>{{ t('citySpatial.enterprise.columns.status') }}</th>
              <th v-if="owner">{{ t('common.actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="site in filteredSites" :key="site.code">
              <td>
                <strong>{{ site.name }}</strong>
                <span>{{ site.code }}</span>
                <small>{{ t(`citySpatial.enterprise.siteType.${site.site_type}`) }}</small>
              </td>
              <td>
                <strong>{{ firmName(site.firm_entity_code) }}</strong>
                <span>{{ site.firm_entity_code }}</span>
              </td>
              <td>
                <strong>{{ site.district_code }}</strong>
                <span>{{ site.building_code }}</span>
                <small>{{ site.pool_code }}</small>
              </td>
              <td>
                <strong>{{ formatInteger(site.occupied_units) }}</strong>
                <span>{{ t('citySpatial.enterprise.version', { version: site.version }) }}</span>
              </td>
              <td>
                <span class="city-enterprise-status" :data-status="site.status">
                  {{ t(`citySpatial.enterprise.status.${site.status}`) }}
                </span>
                <small v-if="site.is_primary">{{ t('citySpatial.enterprise.primary') }}</small>
              </td>
              <td v-if="owner">
                <div v-if="site.status === 'active'" class="city-enterprise-actions">
                  <button type="button" class="btn btn-secondary btn-sm" :disabled="Boolean(busyCommandCode)" @click="openResizeDialog(site)">
                    {{ t('citySpatial.enterprise.action.resize') }}
                  </button>
                  <button
                    v-if="site.site_type === 'headquarters' && site.is_primary"
                    type="button"
                    class="btn btn-secondary btn-sm"
                    :disabled="Boolean(busyCommandCode) || relocationDistricts(site.firm_entity_code).length === 0"
                    @click="openRelocateDialog(site.firm_entity_code)"
                  >
                    {{ t('citySpatial.enterprise.action.relocate') }}
                  </button>
                  <button
                    v-if="!site.is_primary"
                    type="button"
                    class="btn btn-secondary btn-sm"
                    :disabled="Boolean(busyCommandCode)"
                    @click="openCloseDialog(site)"
                  >
                    {{ t('citySpatial.enterprise.action.close') }}
                  </button>
                </div>
                <span v-else>—</span>
              </td>
            </tr>
          </tbody>
        </table>
        <div v-if="filteredSites.length === 0" class="city-enterprise-empty city-enterprise-empty-list">
          <span aria-hidden="true">·</span>
          <p>{{ t('citySpatial.enterprise.noSites') }}</p>
        </div>
      </div>

      <section class="city-enterprise-facts">
        <header>
          <strong>{{ t('citySpatial.enterprise.facts.title') }}</strong>
          <span>{{ t('citySpatial.enterprise.facts.count', { count: state.facts.length }) }}</span>
        </header>
        <div v-if="state.facts.length" class="city-enterprise-fact-list">
          <article v-for="fact in recentFacts" :key="`${fact.tick}:${fact.sequence}`">
            <code>T{{ fact.tick }}.{{ fact.sequence }}</code>
            <strong>{{ t(`citySpatial.enterprise.factType.${fact.fact_type}`) }}</strong>
            <span>{{ firmName(fact.firm_entity_code) }}</span>
            <small>{{ fact.site_code ?? t('citySpatial.enterprise.facts.multiSite') }}</small>
            <b>{{ formatCapacityChange(fact.occupied_before_units, fact.occupied_after_units) }}</b>
          </article>
        </div>
        <p v-else>{{ t('citySpatial.enterprise.facts.empty') }}</p>
      </section>
    </template>

    <BaseDialog
      :show="dialogMode === 'open'"
      :title="t('citySpatial.enterprise.action.open')"
      width="normal"
      @close="dialogMode = null"
    >
      <form class="city-enterprise-form" @submit.prevent="submitOpen">
        <label>
          <span>{{ t('citySpatial.enterprise.form.firm') }}</span>
          <Select v-model="openForm.firmID" :options="firmOptions" :searchable="firmOptions.length > 6" />
        </label>
        <label>
          <span>{{ t('citySpatial.enterprise.form.siteType') }}</span>
          <Select v-model="openForm.siteType" :options="openSiteTypeOptions" :searchable="false" />
        </label>
        <label class="city-enterprise-form-wide">
          <span>{{ t('citySpatial.enterprise.form.pool') }}</span>
          <Select v-model="openForm.poolCode" :options="openPoolOptions" :searchable="openPoolOptions.length > 6" />
        </label>
        <label class="city-enterprise-form-wide">
          <span>{{ t('citySpatial.enterprise.form.name') }}</span>
          <input v-model.trim="openForm.name" class="input" maxlength="128" required />
        </label>
        <label>
          <span>{{ t('citySpatial.enterprise.form.occupiedUnits') }}</span>
          <input v-model="openForm.occupiedUnits" class="input font-mono" type="number" min="1" max="1000000000" :placeholder="t('citySpatial.enterprise.form.policyMinimum')" />
        </label>
        <p class="city-enterprise-form-note">{{ t('citySpatial.enterprise.form.serverAuthoritative') }}</p>
      </form>
      <template #footer>
        <button type="button" class="btn btn-secondary" @click="dialogMode = null">{{ t('common.cancel') }}</button>
        <button type="button" class="btn btn-primary" :disabled="!canSubmitOpen || Boolean(busyCommandCode)" @click="submitOpen">
          {{ t('citySpatial.enterprise.action.confirm') }}
        </button>
      </template>
    </BaseDialog>

    <BaseDialog
      :show="dialogMode === 'resize'"
      :title="t('citySpatial.enterprise.action.resize')"
      width="narrow"
      @close="dialogMode = null"
    >
      <form class="city-enterprise-single-form" @submit.prevent="submitResize">
        <div><span>{{ selectedSite?.name }}</span><code>{{ selectedSite?.code }}</code></div>
        <label>
          <span>{{ t('citySpatial.enterprise.form.occupiedUnits') }}</span>
          <input v-model.number="resizeUnits" class="input font-mono" type="number" min="1" max="1000000000" required />
        </label>
      </form>
      <template #footer>
        <button type="button" class="btn btn-secondary" @click="dialogMode = null">{{ t('common.cancel') }}</button>
        <button type="button" class="btn btn-primary" :disabled="!canSubmitResize || Boolean(busyCommandCode)" @click="submitResize">
          {{ t('citySpatial.enterprise.action.confirm') }}
        </button>
      </template>
    </BaseDialog>

    <BaseDialog
      :show="dialogMode === 'close'"
      :title="t('citySpatial.enterprise.action.close')"
      width="narrow"
      @close="dialogMode = null"
    >
      <form class="city-enterprise-single-form" @submit.prevent="submitClose">
        <div><span>{{ selectedSite?.name }}</span><code>{{ selectedSite?.code }}</code></div>
        <label>
          <span>{{ t('citySpatial.enterprise.form.reason') }}</span>
          <textarea v-model.trim="closeReason" class="input" rows="4" maxlength="256" required />
        </label>
      </form>
      <template #footer>
        <button type="button" class="btn btn-secondary" @click="dialogMode = null">{{ t('common.cancel') }}</button>
        <button type="button" class="btn btn-primary" :disabled="!closeReason || Boolean(busyCommandCode)" @click="submitClose">
          {{ t('citySpatial.enterprise.action.confirm') }}
        </button>
      </template>
    </BaseDialog>

    <BaseDialog
      :show="dialogMode === 'relocate'"
      :title="t('citySpatial.enterprise.action.relocate')"
      width="normal"
      @close="dialogMode = null"
    >
      <form class="city-enterprise-form" @submit.prevent="submitRelocate">
        <div class="city-enterprise-relocate-source city-enterprise-form-wide">
          <span>{{ t('citySpatial.enterprise.form.currentDistrict') }}</span>
          <strong>{{ selectedFirm?.district_code ?? '—' }}</strong>
        </div>
        <label class="city-enterprise-form-wide">
          <span>{{ t('citySpatial.enterprise.form.targetDistrict') }}</span>
          <Select v-model="relocateForm.districtCode" :options="relocationDistrictOptions" :searchable="false" />
        </label>
        <label>
          <span>{{ t('citySpatial.enterprise.form.headquartersPool') }}</span>
          <Select v-model="relocateForm.headquartersPoolCode" :options="relocationHeadquartersPools" :searchable="relocationHeadquartersPools.length > 6" />
        </label>
        <label>
          <span>{{ t('citySpatial.enterprise.form.productionPool') }}</span>
          <Select v-model="relocateForm.productionPoolCode" :options="relocationProductionPools" :searchable="relocationProductionPools.length > 6" />
        </label>
        <label class="city-enterprise-form-wide">
          <span>{{ t('citySpatial.enterprise.form.reason') }}</span>
          <textarea v-model.trim="relocateForm.reason" class="input" rows="3" maxlength="256" required />
        </label>
        <p class="city-enterprise-form-note city-enterprise-form-wide">{{ t('citySpatial.enterprise.form.relocationWarning') }}</p>
      </form>
      <template #footer>
        <button type="button" class="btn btn-secondary" @click="dialogMode = null">{{ t('common.cancel') }}</button>
        <button type="button" class="btn btn-primary" :disabled="!canSubmitRelocate || Boolean(busyCommandCode)" @click="submitRelocate">
          {{ t('citySpatial.enterprise.action.confirm') }}
        </button>
      </template>
    </BaseDialog>
  </section>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type {
  CityEnterpriseLocationCommandType,
  CityEnterpriseLocationState,
  CityEnterpriseSite,
  CityEnterpriseSiteType
} from '@/api/citySpatial'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select, { type SelectOption } from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'

type DialogMode = 'open' | 'resize' | 'close' | 'relocate' | null

const props = defineProps<{
  state: CityEnterpriseLocationState | null
  owner: boolean
  busyCommandCode: string | null
}>()

const emit = defineEmits<{
  (event: 'command', value: {
    commandType: CityEnterpriseLocationCommandType
    payload: Record<string, unknown>
    commandCode: string
  }): void
}>()

const { t, locale } = useI18n()
const dialogMode = ref<DialogMode>(null)
const firmFilter = ref('all')
const districtFilter = ref('all')
const typeFilter = ref('all')
const statusFilter = ref('active')
const selectedSite = ref<CityEnterpriseSite | null>(null)
const selectedFirmCode = ref('')
const resizeUnits = ref(1)
const closeReason = ref('')
const openForm = reactive({
  firmID: null as number | null,
  siteType: 'office' as CityEnterpriseSiteType,
  poolCode: '',
  name: '',
  occupiedUnits: ''
})
const relocateForm = reactive({
  districtCode: '',
  headquartersPoolCode: '',
  productionPoolCode: '',
  reason: ''
})

const siteTypes: CityEnterpriseSiteType[] = ['headquarters', 'office', 'production', 'warehouse', 'retail']
const activeSites = computed(() => props.state?.sites.filter(site => site.status === 'active') ?? [])
const occupiedUnits = computed(() => activeSites.value.reduce((sum, site) => sum + site.occupied_units, 0))
const availableUnits = computed(() => props.state?.pools.reduce((sum, pool) => sum + Math.max(0, pool.available_unit_count), 0) ?? 0)
const selectedFirm = computed(() => props.state?.firms.find(firm => firm.entity_code === selectedFirmCode.value) ?? null)
const firmOptions = computed<SelectOption[]>(() => props.state?.firms.map(firm => ({
  value: firm.entity_id,
  label: `${firm.entity_name} · ${firm.entity_code} · ${firm.district_code}`
})) ?? [])
const firmFilterOptions = computed<SelectOption[]>(() => [
  { value: 'all', label: t('citySpatial.enterprise.filter.allFirms') },
  ...(props.state?.firms.map(firm => ({ value: firm.entity_code, label: firm.entity_name })) ?? [])
])
const districtCodes = computed(() => [...new Set(props.state?.pools.map(pool => pool.district_code) ?? [])].sort())
const districtFilterOptions = computed<SelectOption[]>(() => [
  { value: 'all', label: t('citySpatial.enterprise.filter.allDistricts') },
  ...districtCodes.value.map(code => ({ value: code, label: code }))
])
const typeFilterOptions = computed<SelectOption[]>(() => [
  { value: 'all', label: t('citySpatial.enterprise.filter.allTypes') },
  ...siteTypes.map(value => ({ value, label: t(`citySpatial.enterprise.siteType.${value}`) }))
])
const statusFilterOptions = computed<SelectOption[]>(() => [
  { value: 'all', label: t('citySpatial.enterprise.filter.allStatuses') },
  { value: 'active', label: t('citySpatial.enterprise.status.active') },
  { value: 'closed', label: t('citySpatial.enterprise.status.closed') }
])
const openSiteTypeOptions = computed<SelectOption[]>(() => siteTypes.map(value => ({
  value,
  label: t(`citySpatial.enterprise.siteType.${value}`),
  disabled: value === 'headquarters' && activeSites.value.some(site => (
    site.firm_entity_code === selectedOpenFirmCode.value && site.site_type === 'headquarters'
  ))
})))
const selectedOpenFirmCode = computed(() => props.state?.firms.find(firm => firm.entity_id === openForm.firmID)?.entity_code ?? '')
const selectedOpenPoolUse = computed(() => (
  openForm.siteType === 'production' || openForm.siteType === 'warehouse' ? 'industrial' : 'commercial'
))
const openPoolOptions = computed<SelectOption[]>(() => props.state?.pools
  .filter(pool => pool.use_type === selectedOpenPoolUse.value && pool.available_unit_count > 0)
  .map(pool => ({
    value: pool.code,
    label: `${pool.district_code} · ${pool.building_code} · ${pool.available_unit_count}/${pool.effective_unit_count}`
  })) ?? [])
const filteredSites = computed(() => props.state?.sites.filter(site => (
  (firmFilter.value === 'all' || site.firm_entity_code === firmFilter.value) &&
  (districtFilter.value === 'all' || site.district_code === districtFilter.value) &&
  (typeFilter.value === 'all' || site.site_type === typeFilter.value) &&
  (statusFilter.value === 'all' || site.status === statusFilter.value)
)) ?? [])
const recentFacts = computed(() => [...(props.state?.facts ?? [])]
  .sort((left, right) => right.tick - left.tick || right.sequence - left.sequence)
  .slice(0, 20))
const relocationDistrictOptions = computed<SelectOption[]>(() => relocationDistricts(selectedFirmCode.value).map(code => ({ value: code, label: code })))
const relocationHeadquartersPools = computed(() => poolOptionsForRelocation('commercial'))
const relocationProductionPools = computed(() => poolOptionsForRelocation('industrial'))
const canSubmitOpen = computed(() => Boolean(
  openForm.firmID && openForm.poolCode && openForm.name.trim() &&
  (!openForm.occupiedUnits || validPositiveInteger(openForm.occupiedUnits))
))
const canSubmitResize = computed(() => Boolean(
  selectedSite.value && Number.isSafeInteger(resizeUnits.value) && resizeUnits.value > 0 &&
  resizeUnits.value !== selectedSite.value.occupied_units
))
const canSubmitRelocate = computed(() => Boolean(
  selectedFirm.value && relocateForm.districtCode && relocateForm.headquartersPoolCode &&
  relocateForm.productionPoolCode && relocateForm.reason.trim()
))

watch([() => openForm.siteType, () => props.state?.pools], () => {
  if (!openPoolOptions.value.some(option => option.value === openForm.poolCode)) {
    openForm.poolCode = String(openPoolOptions.value[0]?.value ?? '')
  }
})

watch(() => relocateForm.districtCode, () => {
  relocateForm.headquartersPoolCode = String(relocationHeadquartersPools.value[0]?.value ?? '')
  relocateForm.productionPoolCode = String(relocationProductionPools.value[0]?.value ?? '')
})

function validPositiveInteger(value: string): boolean {
  const parsed = Number(value)
  return Number.isSafeInteger(parsed) && parsed > 0 && parsed <= 1_000_000_000
}

function firmName(code: string): string {
  return props.state?.firms.find(firm => firm.entity_code === code)?.entity_name ?? code
}

function relocationDistricts(firmCode: string): string[] {
  const firm = props.state?.firms.find(item => item.entity_code === firmCode)
  if (!firm) return []
  return districtCodes.value.filter(district => district !== firm.district_code &&
    (props.state?.pools.some(pool => pool.district_code === district && pool.use_type === 'commercial' && pool.available_unit_count > 0) ?? false) &&
    (props.state?.pools.some(pool => pool.district_code === district && pool.use_type === 'industrial' && pool.available_unit_count > 0) ?? false))
}

function poolOptionsForRelocation(useType: 'commercial' | 'industrial'): SelectOption[] {
  return props.state?.pools.filter(pool => (
    pool.district_code === relocateForm.districtCode && pool.use_type === useType && pool.available_unit_count > 0
  )).map(pool => ({
    value: pool.code,
    label: `${pool.building_code} · ${pool.available_unit_count}/${pool.effective_unit_count}`
  })) ?? []
}

function openCreateDialog(): void {
  openForm.firmID = props.state?.firms[0]?.entity_id ?? null
  openForm.siteType = 'office'
  openForm.poolCode = String(openPoolOptions.value[0]?.value ?? '')
  openForm.name = ''
  openForm.occupiedUnits = ''
  dialogMode.value = 'open'
}

function openResizeDialog(site: CityEnterpriseSite): void {
  selectedSite.value = site
  resizeUnits.value = site.occupied_units
  dialogMode.value = 'resize'
}

function openCloseDialog(site: CityEnterpriseSite): void {
  selectedSite.value = site
  closeReason.value = ''
  dialogMode.value = 'close'
}

function openRelocateDialog(firmCode: string): void {
  selectedFirmCode.value = firmCode
  relocateForm.districtCode = relocationDistricts(firmCode)[0] ?? ''
  relocateForm.headquartersPoolCode = String(relocationHeadquartersPools.value[0]?.value ?? '')
  relocateForm.productionPoolCode = String(relocationProductionPools.value[0]?.value ?? '')
  relocateForm.reason = ''
  dialogMode.value = 'relocate'
}

function sendCommand(commandType: CityEnterpriseLocationCommandType, payload: Record<string, unknown>, commandCode: string): void {
  emit('command', { commandType, payload, commandCode })
  dialogMode.value = null
}

function submitOpen(): void {
  if (!canSubmitOpen.value || !openForm.firmID) return
  const payload: Record<string, unknown> = {
    firm_entity_id: openForm.firmID,
    pool_code: openForm.poolCode,
    site_type: openForm.siteType,
    name: openForm.name.trim()
  }
  if (openForm.occupiedUnits) payload.target_occupied_units = Number(openForm.occupiedUnits)
  sendCommand('enterprise.site.open', payload, 'new-site')
}

function submitResize(): void {
  if (!canSubmitResize.value || !selectedSite.value) return
  sendCommand('enterprise.site.resize', {
    site_code: selectedSite.value.code,
    target_occupied_units: Math.trunc(resizeUnits.value)
  }, selectedSite.value.code)
}

function submitClose(): void {
  if (!selectedSite.value || !closeReason.value) return
  sendCommand('enterprise.site.close', {
    site_code: selectedSite.value.code,
    reason: closeReason.value
  }, selectedSite.value.code)
}

function submitRelocate(): void {
  if (!canSubmitRelocate.value || !selectedFirm.value) return
  sendCommand('enterprise.relocate', {
    firm_entity_id: selectedFirm.value.entity_id,
    headquarters_pool_code: relocateForm.headquartersPoolCode,
    production_pool_code: relocateForm.productionPoolCode,
    reason: relocateForm.reason
  }, selectedFirm.value.entity_code)
}

function formatInteger(value: number): string {
  return new Intl.NumberFormat(locale.value, { maximumFractionDigits: 0 }).format(value)
}

function formatCapacityChange(before: number, after: number): string {
  if (before === after) return '—'
  return `${formatInteger(before)} → ${formatInteger(after)}`
}
</script>

<style scoped>
.city-enterprise-panel { margin-top: 1rem; border: 1px solid var(--ui-separator); background: var(--ui-surface); }
.city-enterprise-header { display: flex; align-items: center; justify-content: space-between; gap: 1rem; border-bottom: 1px solid var(--ui-separator); padding: 1rem; }
.city-enterprise-header p { margin: 0; color: var(--ui-accent); font: 0.62rem ui-monospace, monospace; letter-spacing: 0.12em; text-transform: uppercase; }
.city-enterprise-header h2 { margin: 0.2rem 0 0.15rem; font-size: 1rem; }
.city-enterprise-header > div > span { color: var(--ui-label-secondary); font-size: 0.75rem; }
.city-enterprise-summary { display: grid; grid-template-columns: repeat(5, minmax(5rem, 0.55fr)) minmax(12rem, 1fr); border-bottom: 1px solid var(--ui-separator); }
.city-enterprise-summary > div { border-right: 1px solid var(--ui-separator); padding: 0.7rem 0.85rem; }
.city-enterprise-summary span { display: block; color: var(--ui-label-secondary); font: 0.58rem ui-monospace, monospace; letter-spacing: 0.08em; }
.city-enterprise-summary strong { display: block; margin-top: 0.15rem; font: 1rem ui-monospace, monospace; }
.city-enterprise-summary code { align-self: center; justify-self: end; padding: 0 0.85rem; color: var(--ui-label-secondary); font-size: 0.65rem; }
.city-enterprise-filters { display: grid; grid-template-columns: repeat(4, minmax(10rem, 1fr)); gap: 0.7rem; border-bottom: 1px solid var(--ui-separator); padding: 0.75rem 1rem; background: var(--ui-control); }
.city-enterprise-filters label > span, .city-enterprise-form label > span, .city-enterprise-single-form label > span { display: block; margin-bottom: 0.3rem; color: var(--ui-label-secondary); font-size: 0.64rem; }
.city-enterprise-table-wrap { overflow-x: auto; }
.city-enterprise-table { width: 100%; min-width: 68rem; border-collapse: collapse; }
.city-enterprise-table th { border-bottom: 1px solid var(--ui-separator); padding: 0.65rem 0.75rem; color: var(--ui-label-secondary); background: var(--ui-control); font-size: 0.62rem; font-weight: 600; text-align: left; }
.city-enterprise-table td { border-bottom: 1px solid var(--ui-separator); padding: 0.75rem; vertical-align: top; font-size: 0.7rem; }
.city-enterprise-table td strong, .city-enterprise-table td span, .city-enterprise-table td small { display: block; }
.city-enterprise-table td span, .city-enterprise-table td small { margin-top: 0.12rem; color: var(--ui-label-secondary); font: 0.6rem ui-monospace, monospace; overflow-wrap: anywhere; }
.city-enterprise-status { width: fit-content; border-left: 3px solid var(--ui-separator); padding: 0.2rem 0.4rem; }
.city-enterprise-status[data-status='active'] { border-left-color: #16a36a; color: #16865a; }
.city-enterprise-actions { display: flex; flex-wrap: wrap; gap: 0.35rem; }
.city-enterprise-empty { display: grid; min-height: 10rem; place-content: center; justify-items: center; gap: 0.5rem; color: var(--ui-label-secondary); }
.city-enterprise-empty > span { color: var(--ui-accent); font: 1.5rem ui-monospace, monospace; }
.city-enterprise-empty p { margin: 0; font-size: 0.75rem; }
.city-enterprise-empty-list { min-height: 8rem; }
.city-enterprise-facts { border-top: 1px solid var(--ui-separator); }
.city-enterprise-facts > header { display: flex; justify-content: space-between; padding: 0.7rem 1rem; font-size: 0.68rem; }
.city-enterprise-facts > header span, .city-enterprise-facts > p { color: var(--ui-label-secondary); }
.city-enterprise-facts > p { margin: 0; padding: 1rem; font-size: 0.68rem; }
.city-enterprise-fact-list { display: grid; grid-template-columns: repeat(auto-fit, minmax(18rem, 1fr)); gap: 1px; background: var(--ui-separator); }
.city-enterprise-fact-list article { display: grid; grid-template-columns: auto 1fr auto; gap: 0.2rem 0.55rem; min-width: 0; padding: 0.65rem 0.75rem; background: var(--ui-surface); }
.city-enterprise-fact-list code { grid-row: span 2; color: var(--ui-accent); font-size: 0.6rem; }
.city-enterprise-fact-list strong { font-size: 0.68rem; }
.city-enterprise-fact-list span, .city-enterprise-fact-list small, .city-enterprise-fact-list b { min-width: 0; color: var(--ui-label-secondary); font-size: 0.6rem; overflow-wrap: anywhere; }
.city-enterprise-fact-list b { grid-column: 3; font-family: ui-monospace, monospace; font-weight: 500; }
.city-enterprise-form { display: grid; grid-template-columns: 1fr 1fr; gap: 0.85rem; }
.city-enterprise-form-wide, .city-enterprise-form-note { grid-column: 1 / -1; }
.city-enterprise-form-note { margin: 0; color: var(--ui-label-secondary); font-size: 0.64rem; }
.city-enterprise-single-form { display: grid; gap: 1rem; }
.city-enterprise-single-form > div { border-left: 3px solid var(--ui-accent); padding-left: 0.75rem; }
.city-enterprise-single-form > div span, .city-enterprise-single-form > div code { display: block; }
.city-enterprise-single-form > div code { margin-top: 0.15rem; color: var(--ui-label-secondary); font-size: 0.62rem; }
.city-enterprise-single-form textarea, .city-enterprise-form textarea { min-height: 5.5rem; resize: vertical; }
.city-enterprise-relocate-source { border-left: 3px solid var(--ui-accent); padding: 0.25rem 0.75rem; }
.city-enterprise-relocate-source span { display: block; color: var(--ui-label-secondary); font-size: 0.62rem; }
.city-enterprise-relocate-source strong { display: block; margin-top: 0.15rem; font: 0.76rem ui-monospace, monospace; }
@media (max-width: 900px) {
  .city-enterprise-summary { grid-template-columns: repeat(2, 1fr); }
  .city-enterprise-summary code { grid-column: 1 / -1; justify-self: start; padding: 0.65rem 0.85rem; }
  .city-enterprise-filters { grid-template-columns: 1fr 1fr; }
}
@media (max-width: 620px) {
  .city-enterprise-header { align-items: flex-start; flex-direction: column; }
  .city-enterprise-filters, .city-enterprise-form { grid-template-columns: 1fr; }
  .city-enterprise-form-wide, .city-enterprise-form-note { grid-column: auto; }
}
</style>
