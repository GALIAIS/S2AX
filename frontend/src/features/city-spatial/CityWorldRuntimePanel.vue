<template>
  <section class="world-runtime-panel" :aria-busy="loading">
    <header class="runtime-header">
      <div>
        <p>{{ t('citySpatial.runtime.eyebrow') }}</p>
        <h2>{{ t('citySpatial.runtime.title') }}</h2>
        <span>{{ t('citySpatial.runtime.description') }}</span>
      </div>
      <dl v-if="catalog" class="runtime-counters">
        <div><dt>{{ t('citySpatial.runtime.counters.actors') }}</dt><dd>{{ catalog.profile.actor_count }}</dd></div>
        <div><dt>{{ t('citySpatial.runtime.counters.facts') }}</dt><dd>{{ catalog.profile.fact_count }}</dd></div>
        <div><dt>{{ t('citySpatial.runtime.counters.cases') }}</dt><dd>{{ catalog.profile.case_count }}</dd></div>
        <div><dt>R</dt><dd>{{ catalog.profile.revision }}</dd></div>
      </dl>
    </header>

    <div v-if="!catalog" class="runtime-unavailable">
      {{ t('citySpatial.runtime.unavailable') }}
    </div>

    <template v-else>
      <div v-if="actors.length" class="runtime-actor-tabs" role="tablist" :aria-label="t('citySpatial.runtime.actorSelection')">
        <button
          v-for="actor in actors"
          :key="actor.code"
          type="button"
          role="tab"
          :aria-selected="actor.code === selectedActorCode"
          :class="{ active: actor.code === selectedActorCode }"
          @click="emit('selectActor', actor.code)"
        >
          <span>{{ actor.name.slice(0, 1).toUpperCase() }}</span>
          <strong>{{ actor.name }}</strong>
          <small>{{ actor.code }}</small>
        </button>
      </div>

      <form v-if="canCreateActor" class="runtime-character-create" @submit.prevent="createActor">
        <div class="runtime-section-heading">
          <div>
            <p>{{ t('citySpatial.runtime.creation.eyebrow') }}</p>
            <h3>{{ t('citySpatial.runtime.creation.title') }}</h3>
          </div>
          <span>{{ t('citySpatial.runtime.creation.capacity', {
            current: actors.length,
            maximum: catalog.profile.maximum_player_actors_per_member
          }) }}</span>
        </div>
        <div class="runtime-archetypes" role="radiogroup" :aria-label="t('citySpatial.runtime.creation.archetype')">
          <label
            v-for="definition in archetypes"
            :key="definition.code"
            :class="{ active: createForm.archetypeCode === definition.code }"
          >
            <input v-model="createForm.archetypeCode" type="radio" :value="definition.code">
            <span class="runtime-archetype-index">{{ archetypeIndex(definition.code) }}</span>
            <strong>{{ definitionName(definition) }}</strong>
            <small>{{ definitionDescription(definition) }}</small>
            <dl>
              <div v-for="(value, code) in definitionInitialAttributes(definition)" :key="code">
                <dt>{{ definitionLabel('attribute', String(code)) }}</dt>
                <dd>{{ formatScaled(Number(value)) }}</dd>
              </div>
            </dl>
          </label>
        </div>
        <div class="runtime-create-controls">
          <label>
            <span>{{ t('citySpatial.runtime.creation.name') }}</span>
            <input
              v-model.trim="createForm.name"
              class="input"
              maxlength="96"
              required
              :placeholder="t('citySpatial.runtime.creation.namePlaceholder')"
            >
          </label>
          <button
            type="submit"
            class="btn btn-primary"
            :disabled="!createForm.name || !createForm.archetypeCode || Boolean(busyCommandCode)"
          >
            {{ busyCommandCode === 'actor.create'
              ? t('citySpatial.runtime.processing')
              : t('citySpatial.runtime.creation.confirm') }}
          </button>
        </div>
      </form>

      <div v-if="actorState" class="runtime-actor-workbench">
        <section class="runtime-identity-card">
          <div class="runtime-avatar">{{ actorState.actor.name.slice(0, 2).toUpperCase() }}</div>
          <div>
            <p>{{ definitionLabel('archetype', actorState.actor.archetype_code ?? '') }}</p>
            <h3>{{ actorState.actor.name }}</h3>
            <span>{{ actorState.actor.code }} · V{{ actorState.actor.version }} · T{{ actorState.actor.updated_tick }}</span>
          </div>
          <div class="runtime-active-roles">
            <span v-for="role in activeRoles" :key="`${role.role_code}-${role.granted_tick}`">
              {{ definitionLabel('role', role.role_code) }}
            </span>
          </div>
        </section>

        <section class="runtime-attributes">
          <div class="runtime-section-heading">
            <div><p>STATE VECTOR</p><h3>{{ t('citySpatial.runtime.attributes') }}</h3></div>
            <span>{{ t('citySpatial.runtime.serverAuthoritative') }}</span>
          </div>
          <div class="runtime-attribute-grid">
            <article v-for="attribute in actorState.attributes" :key="attribute.attribute_code">
              <header>
                <strong>{{ definitionLabel('attribute', attribute.attribute_code) }}</strong>
                <span>{{ formatScaled(attribute.value_units) }}</span>
              </header>
              <div class="runtime-meter"><i :style="{ width: `${attributePercent(attribute)}%` }" /></div>
              <footer>
                <span>XP {{ attribute.experience_units.toLocaleString() }}</span>
                <span>T{{ attribute.last_changed_tick }} · V{{ attribute.version }}</span>
              </footer>
            </article>
          </div>
        </section>

        <div class="runtime-actions-grid">
          <section>
            <div class="runtime-section-heading">
              <div><p>ACTION CATALOG</p><h3>{{ t('citySpatial.runtime.activities') }}</h3></div>
              <span>{{ t('citySpatial.runtime.activitiesHint') }}</span>
            </div>
            <div class="runtime-activity-list">
              <article v-for="activity in activities" :key="activity.code">
                <div>
                  <strong>{{ definitionName(activity) }}</strong>
                  <p>{{ definitionDescription(activity) }}</p>
                  <small>{{ activityEffectSummary(activity) }}</small>
                </div>
                <button
                  type="button"
                  class="btn btn-secondary btn-sm"
                  :disabled="Boolean(busyCommandCode)"
                  @click="performActivity(activity.code)"
                >
                  {{ busyCommandCode === `activity:${activity.code}`
                    ? t('citySpatial.runtime.processing')
                    : t('citySpatial.runtime.perform') }}
                </button>
              </article>
            </div>
          </section>

          <section>
            <div class="runtime-section-heading">
              <div><p>ROLE GRAPH</p><h3>{{ t('citySpatial.runtime.roles') }}</h3></div>
              <span>{{ t('citySpatial.runtime.rolesHint') }}</span>
            </div>
            <div class="runtime-role-list">
              <article v-for="option in roleOptions" :key="option.definition.code" :class="{ active: option.active }">
                <div>
                  <strong>{{ definitionName(option.definition) }}</strong>
                  <p>{{ definitionDescription(option.definition) }}</p>
                  <small v-if="option.active">{{ t('citySpatial.runtime.roleState.active') }}</small>
                  <small v-else-if="option.eligible">{{ t('citySpatial.runtime.roleState.eligible') }}</small>
                  <small v-else>{{ roleBlockSummary(option) }}</small>
                </div>
                <button
                  v-if="!option.active"
                  type="button"
                  class="btn btn-secondary btn-sm"
                  :disabled="!option.eligible || Boolean(busyCommandCode)"
                  @click="transitionRole(option.definition.code)"
                >
                  {{ busyCommandCode === `role:${option.definition.code}`
                    ? t('citySpatial.runtime.processing')
                    : t('citySpatial.runtime.transition') }}
                </button>
                <span v-else class="runtime-active-mark">ACTIVE</span>
              </article>
            </div>
          </section>
        </div>

        <div class="runtime-governance-grid">
          <section>
            <div class="runtime-section-heading">
              <div><p>STATUS LEDGER</p><h3>{{ t('citySpatial.runtime.statuses') }}</h3></div>
              <span>{{ activeStatuses.length }}</span>
            </div>
            <div v-if="actorState.statuses.length" class="runtime-status-list">
              <article v-for="status in actorState.statuses" :key="status.instance_code" :data-lifecycle="status.lifecycle_status">
                <header>
                  <strong>{{ definitionLabel('status', status.status_code) }}</strong>
                  <span>{{ status.lifecycle_status }}</span>
                </header>
                <p>{{ t('citySpatial.runtime.statusSummary', {
                  stacks: status.stacks,
                  intensity: formatScaled(status.intensity_units)
                }) }}</p>
                <footer>T{{ status.granted_tick }} → {{ status.expires_tick ? `T${status.expires_tick}` : '∞' }}</footer>
              </article>
            </div>
            <p v-else class="runtime-empty">{{ t('citySpatial.runtime.noStatuses') }}</p>
          </section>

          <section>
            <div class="runtime-section-heading">
              <div><p>RULE CASES</p><h3>{{ t('citySpatial.runtime.cases') }}</h3></div>
              <span>{{ actorCases.length }}</span>
            </div>
            <div v-if="actorCases.length" class="runtime-case-list">
              <article v-for="item in actorCases.slice(0, 12)" :key="item.code">
                <span>T{{ item.tick }}.{{ item.sequence }}</span>
                <strong>{{ definitionLabel('rule', item.rule_code) }}</strong>
                <small>{{ item.decision_code ?? item.status }} · {{ formatScaled(item.severity_units) }}</small>
              </article>
            </div>
            <p v-else class="runtime-empty">{{ t('citySpatial.runtime.noCases') }}</p>
          </section>

          <section>
            <div class="runtime-section-heading">
              <div><p>PUBLIC RULEBOOK</p><h3>{{ t('citySpatial.runtime.rules') }}</h3></div>
              <span>{{ rules.length }}</span>
            </div>
            <div class="runtime-rule-list">
              <article v-for="rule in rules" :key="rule.code">
                <strong>{{ definitionName(rule) }}</strong>
                <p>{{ definitionDescription(rule) }}</p>
                <small>{{ ruleScope(rule) }}</small>
              </article>
            </div>
          </section>
        </div>

        <section class="runtime-fact-stream">
          <div class="runtime-section-heading">
            <div><p>IMMUTABLE FACT STREAM</p><h3>{{ t('citySpatial.runtime.facts') }}</h3></div>
            <span>{{ actorState.recent_facts.length }}</span>
          </div>
          <div v-if="actorState.recent_facts.length">
            <article v-for="fact in actorState.recent_facts.slice(0, 16)" :key="`${fact.tick}-${fact.sequence}`">
              <span>T{{ fact.tick }}.{{ fact.sequence }}</span>
              <strong>{{ factTypeLabel(fact.fact_type) }}</strong>
              <small>{{ fact.definition_code ? definitionLabel(fact.definition_kind ?? '', fact.definition_code) : 'SYSTEM' }}</small>
            </article>
          </div>
        </section>
      </div>
    </template>

    <div v-if="loading" class="runtime-loading-line" aria-hidden="true" />
  </section>
</template>

<script setup lang="ts">
import { computed, reactive, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type {
  WorldActor,
  WorldActorAttribute,
  WorldActorRoleOption,
  WorldActorState,
  WorldRuleCase,
  WorldRuntimeCatalog,
  WorldRuntimeCommandType,
  WorldRuntimeDefinition
} from '@/api/citySpatial'

const props = defineProps<{
  catalog: WorldRuntimeCatalog | null
  actors: WorldActor[]
  selectedActorCode: string | null
  actorState: WorldActorState | null
  roleOptions: WorldActorRoleOption[]
  rules: WorldRuntimeDefinition[]
  cases: WorldRuleCase[]
  loading: boolean
  busyCommandCode: string | null
}>()

const emit = defineEmits<{
  selectActor: [actorCode: string]
  command: [commandType: WorldRuntimeCommandType, payload: Record<string, unknown>, commandCode: string]
}>()

const { t, te } = useI18n()
const createForm = reactive({ name: '', archetypeCode: '' })

const definitions = computed(() => props.catalog?.definitions ?? [])
const archetypes = computed(() => definitions.value.filter(item => item.kind === 'archetype'))
const activities = computed(() => definitions.value.filter(item => item.kind === 'activity'))
const activeRoles = computed(() => props.actorState?.roles.filter(item => item.status === 'active') ?? [])
const activeStatuses = computed(() => props.actorState?.statuses.filter(item => item.lifecycle_status === 'active') ?? [])
const actorCases = computed(() => props.cases.filter(item => item.subject_actor_code === props.selectedActorCode))
const canCreateActor = computed(() => Boolean(
  props.catalog && props.actors.length < props.catalog.profile.maximum_player_actors_per_member
))

watch(archetypes, items => {
  if (!items.some(item => item.code === createForm.archetypeCode)) {
    createForm.archetypeCode = items[0]?.code ?? ''
  }
}, { immediate: true })

function payloadRecord(definition: WorldRuntimeDefinition): Record<string, unknown> {
  return definition.payload && typeof definition.payload === 'object' ? definition.payload : {}
}

function definitionName(definition: WorldRuntimeDefinition): string {
  const key = String(payloadRecord(definition).name_key ?? '')
  return key && te(key) ? t(key) : prettifyCode(definition.code)
}

function definitionDescription(definition: WorldRuntimeDefinition): string {
  const key = String(payloadRecord(definition).description_key ?? '')
  return key && te(key) ? t(key) : definition.code
}

function definitionLabel(kind: string, code: string): string {
  if (!code) return t('citySpatial.runtime.unknown')
  const definition = definitions.value.find(item => item.kind === kind && item.code === code)
    ?? props.rules.find(item => item.kind === kind && item.code === code)
  return definition ? definitionName(definition) : prettifyCode(code)
}

function prettifyCode(code: string): string {
  const parts = code.split('.')
  const tail = parts[parts.length - 1] ?? code
  return tail.split('_').join(' ').replace(/\b\w/g, (value: string) => value.toUpperCase())
}

function archetypeIndex(code: string): string {
  const index = archetypes.value.findIndex(item => item.code === code)
  return String(index + 1).padStart(2, '0')
}

function definitionInitialAttributes(definition: WorldRuntimeDefinition): Record<string, number> {
  const value = payloadRecord(definition).initial_attributes
  return value && typeof value === 'object' ? value as Record<string, number> : {}
}

function attributeDefinition(code: string): Record<string, unknown> {
  const definition = definitions.value.find(item => item.kind === 'attribute' && item.code === code)
  return definition ? payloadRecord(definition) : {}
}

function attributePercent(attribute: WorldActorAttribute): number {
  const definition = attributeDefinition(attribute.attribute_code)
  const minimum = Number(definition.minimum_units ?? 0)
  const maximum = Number(definition.maximum_units ?? 100000)
  if (!Number.isFinite(minimum) || !Number.isFinite(maximum) || maximum <= minimum) return 0
  return Math.max(0, Math.min(100, ((attribute.value_units - minimum) / (maximum - minimum)) * 100))
}

function formatScaled(value: number): string {
  return new Intl.NumberFormat(undefined, { maximumFractionDigits: 1 }).format(value / 1000)
}

function activityEffectSummary(definition: WorldRuntimeDefinition): string {
  const effects = payloadRecord(definition).effects
  if (!Array.isArray(effects)) return ''
  return effects.map(effect => {
    const item = effect as Record<string, unknown>
    const value = Number(item.value_units ?? item.stacks ?? 0)
    const sign = value > 0 ? '+' : ''
    return `${definitionLabel(String(item.type ?? '').startsWith('status') ? 'status' : 'attribute', String(item.key ?? ''))} ${sign}${formatScaled(value)}`
  }).join(' · ')
}

function roleBlockSummary(option: WorldActorRoleOption): string {
  if (option.cooldown_remaining_ticks > 0) {
    return t('citySpatial.runtime.roleState.cooldown', { count: option.cooldown_remaining_ticks })
  }
  const failure = option.evaluation.failures[0]
  if (failure) {
    const code = failure.code ? definitionLabel(
      failure.operator.startsWith('role_') ? 'role' : failure.operator.startsWith('status_') ? 'status' : 'attribute',
      failure.code
    ) : t('citySpatial.runtime.roleState.requirements')
    return failure.required_units === undefined
      ? code
      : `${code} ${formatScaled(failure.actual_units ?? 0)} / ${formatScaled(failure.required_units)}`
  }
  return t('citySpatial.runtime.roleState.requirements')
}

function ruleScope(definition: WorldRuntimeDefinition): string {
  const payload = payloadRecord(definition)
  return `${String(payload.category_code ?? 'rule').toUpperCase()} · ${String(payload.scope_kind ?? 'world').toUpperCase()}:${String(payload.scope_code ?? 'world')}`
}

function factTypeLabel(value: string): string {
  const key = `worldRuntime.facts.${value.split('.').join('_')}`
  return te(key) ? t(key) : prettifyCode(value)
}

function createActor(): void {
  if (!createForm.name || !createForm.archetypeCode) return
  emit('command', 'actor.create', {
    name: createForm.name,
    archetype_code: createForm.archetypeCode
  }, 'actor.create')
}

function performActivity(activityCode: string): void {
  if (!props.selectedActorCode) return
  emit('command', 'actor.activity.perform', {
    actor_code: props.selectedActorCode,
    activity_code: activityCode
  }, `activity:${activityCode}`)
}

function transitionRole(roleCode: string): void {
  if (!props.selectedActorCode) return
  emit('command', 'actor.role.transition', {
    actor_code: props.selectedActorCode,
    role_code: roleCode
  }, `role:${roleCode}`)
}
</script>

<style scoped>
.world-runtime-panel { position: relative; margin-top: 1rem; border: 1px solid var(--ui-separator); background: var(--ui-surface); }
.runtime-header { display: flex; align-items: flex-end; justify-content: space-between; gap: 1.5rem; border-bottom: 1px solid var(--ui-separator); padding: 1rem; }
.runtime-header p, .runtime-section-heading p { margin: 0; color: var(--ui-label-secondary); font: 0.6rem ui-monospace, monospace; letter-spacing: 0.13em; text-transform: uppercase; }
.runtime-header h2 { margin: 0.2rem 0; font-size: 1rem; }
.runtime-header > div > span { color: var(--ui-label-secondary); font-size: 0.73rem; }
.runtime-counters { display: flex; margin: 0; border: 1px solid var(--ui-separator); }
.runtime-counters div { min-width: 4.5rem; border-right: 1px solid var(--ui-separator); padding: 0.45rem 0.65rem; }
.runtime-counters div:last-child { border-right: 0; }
.runtime-counters dt { color: var(--ui-label-secondary); font: 0.55rem ui-monospace, monospace; text-transform: uppercase; }
.runtime-counters dd { margin: 0.15rem 0 0; font: 0.75rem ui-monospace, monospace; }
.runtime-unavailable, .runtime-empty { margin: 0; padding: 1rem; color: var(--ui-label-secondary); font-size: 0.72rem; }
.runtime-actor-tabs { display: flex; overflow-x: auto; border-bottom: 1px solid var(--ui-separator); }
.runtime-actor-tabs button { display: grid; min-width: 13rem; grid-template-columns: 2rem 1fr; column-gap: 0.65rem; border-right: 1px solid var(--ui-separator); padding: 0.65rem 0.8rem; text-align: left; }
.runtime-actor-tabs button > span { display: grid; grid-row: 1 / 3; place-items: center; border: 1px solid var(--ui-separator); color: var(--ui-accent); font: 0.72rem ui-monospace, monospace; }
.runtime-actor-tabs strong { font-size: 0.75rem; }
.runtime-actor-tabs small { color: var(--ui-label-secondary); font: 0.57rem ui-monospace, monospace; }
.runtime-actor-tabs button.active { box-shadow: inset 0 -2px 0 var(--ui-accent); background: var(--ui-control); }
.runtime-section-heading { display: flex; align-items: flex-end; justify-content: space-between; gap: 1rem; border-bottom: 1px solid var(--ui-separator); padding: 0.7rem 0.8rem; }
.runtime-section-heading h3 { margin: 0.12rem 0 0; font-size: 0.82rem; }
.runtime-section-heading > span { color: var(--ui-label-secondary); font: 0.6rem ui-monospace, monospace; }
.runtime-character-create { padding: 1rem; }
.runtime-character-create > .runtime-section-heading { margin: -1rem -1rem 1rem; }
.runtime-archetypes { display: grid; grid-template-columns: repeat(auto-fit, minmax(15rem, 1fr)); border-top: 1px solid var(--ui-separator); border-left: 1px solid var(--ui-separator); }
.runtime-archetypes label { position: relative; display: grid; gap: 0.35rem; border-right: 1px solid var(--ui-separator); border-bottom: 1px solid var(--ui-separator); padding: 0.85rem; cursor: pointer; }
.runtime-archetypes label.active { box-shadow: inset 3px 0 0 var(--ui-accent); background: var(--ui-control); }
.runtime-archetypes input { position: absolute; opacity: 0; pointer-events: none; }
.runtime-archetype-index { color: var(--ui-accent); font: 0.6rem ui-monospace, monospace; }
.runtime-archetypes strong { font-size: 0.8rem; }
.runtime-archetypes small { min-height: 2.6rem; color: var(--ui-label-secondary); font-size: 0.68rem; line-height: 1.45; }
.runtime-archetypes dl { display: grid; grid-template-columns: repeat(2, 1fr); gap: 0.25rem 0.7rem; margin: 0.5rem 0 0; }
.runtime-archetypes dl div { display: flex; justify-content: space-between; gap: 0.5rem; color: var(--ui-label-secondary); font: 0.58rem ui-monospace, monospace; }
.runtime-archetypes dd { margin: 0; color: var(--ui-label); }
.runtime-create-controls { display: grid; grid-template-columns: minmax(14rem, 1fr) auto; align-items: end; gap: 0.75rem; margin-top: 0.8rem; }
.runtime-create-controls label > span { display: block; margin-bottom: 0.3rem; color: var(--ui-label-secondary); font-size: 0.68rem; }
.runtime-actor-workbench { display: grid; }
.runtime-identity-card { display: grid; grid-template-columns: 3.5rem minmax(0, 1fr) auto; align-items: center; gap: 0.9rem; border-bottom: 1px solid var(--ui-separator); padding: 0.9rem 1rem; background: var(--ui-canvas-raised); }
.runtime-avatar { display: grid; width: 3.5rem; height: 3.5rem; place-items: center; border: 1px solid var(--ui-separator); color: var(--ui-accent); font: 0.9rem ui-monospace, monospace; }
.runtime-identity-card p { margin: 0; color: var(--ui-label-secondary); font-size: 0.65rem; }
.runtime-identity-card h3 { margin: 0.15rem 0; font-size: 1rem; }
.runtime-identity-card > div > span { color: var(--ui-label-secondary); font: 0.58rem ui-monospace, monospace; }
.runtime-active-roles { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 0.35rem; }
.runtime-active-roles span, .runtime-active-mark { border: 1px solid var(--ui-separator); padding: 0.25rem 0.4rem; color: var(--ui-accent); font: 0.58rem ui-monospace, monospace; text-transform: uppercase; }
.runtime-attribute-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(12rem, 1fr)); }
.runtime-attribute-grid article { border-right: 1px solid var(--ui-separator); border-bottom: 1px solid var(--ui-separator); padding: 0.75rem 0.8rem; }
.runtime-attribute-grid header, .runtime-attribute-grid footer { display: flex; justify-content: space-between; gap: 0.5rem; }
.runtime-attribute-grid header { font-size: 0.7rem; }
.runtime-attribute-grid header span { font: 0.72rem ui-monospace, monospace; }
.runtime-meter { height: 0.28rem; margin: 0.55rem 0; background: var(--ui-control); }
.runtime-meter i { display: block; height: 100%; background: var(--ui-accent); }
.runtime-attribute-grid footer { color: var(--ui-label-secondary); font: 0.55rem ui-monospace, monospace; }
.runtime-actions-grid { display: grid; grid-template-columns: 1fr 1fr; border-top: 1px solid var(--ui-separator); }
.runtime-actions-grid > section:first-child { border-right: 1px solid var(--ui-separator); }
.runtime-activity-list, .runtime-role-list { display: grid; max-height: 24rem; overflow: auto; }
.runtime-activity-list article, .runtime-role-list article { display: flex; align-items: center; justify-content: space-between; gap: 1rem; border-bottom: 1px solid var(--ui-separator); padding: 0.75rem 0.8rem; }
.runtime-activity-list article > div, .runtime-role-list article > div { min-width: 0; }
.runtime-activity-list strong, .runtime-role-list strong { font-size: 0.73rem; }
.runtime-activity-list p, .runtime-role-list p, .runtime-rule-list p { margin: 0.18rem 0; color: var(--ui-label-secondary); font-size: 0.65rem; line-height: 1.45; }
.runtime-activity-list small, .runtime-role-list small { color: var(--ui-label-secondary); font: 0.56rem ui-monospace, monospace; }
.runtime-role-list article.active { box-shadow: inset 3px 0 0 var(--ui-accent); }
.runtime-governance-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); border-top: 1px solid var(--ui-separator); }
.runtime-governance-grid > section { min-width: 0; border-right: 1px solid var(--ui-separator); }
.runtime-governance-grid > section:last-child { border-right: 0; }
.runtime-status-list, .runtime-case-list, .runtime-rule-list { max-height: 18rem; overflow: auto; }
.runtime-status-list article, .runtime-case-list article, .runtime-rule-list article { border-bottom: 1px solid var(--ui-separator); padding: 0.65rem 0.8rem; }
.runtime-status-list header { display: flex; justify-content: space-between; gap: 0.5rem; font-size: 0.7rem; }
.runtime-status-list header span { color: var(--ui-label-secondary); font: 0.55rem ui-monospace, monospace; text-transform: uppercase; }
.runtime-status-list article[data-lifecycle='active'] { box-shadow: inset 3px 0 0 #d97706; }
.runtime-status-list p { margin: 0.25rem 0; color: var(--ui-label-secondary); font-size: 0.63rem; }
.runtime-status-list footer { color: var(--ui-label-secondary); font: 0.55rem ui-monospace, monospace; }
.runtime-case-list article { display: grid; grid-template-columns: auto 1fr; gap: 0.2rem 0.6rem; }
.runtime-case-list span { grid-row: 1 / 3; color: #d97706; font: 0.58rem ui-monospace, monospace; }
.runtime-case-list strong { font-size: 0.68rem; }
.runtime-case-list small, .runtime-rule-list small { color: var(--ui-label-secondary); font: 0.55rem ui-monospace, monospace; }
.runtime-rule-list strong { font-size: 0.7rem; }
.runtime-fact-stream { border-top: 1px solid var(--ui-separator); }
.runtime-fact-stream > div:last-child { display: grid; grid-template-columns: repeat(auto-fit, minmax(14rem, 1fr)); max-height: 12rem; overflow: auto; }
.runtime-fact-stream article { display: grid; grid-template-columns: auto 1fr; gap: 0.15rem 0.55rem; border-right: 1px solid var(--ui-separator); border-bottom: 1px solid var(--ui-separator); padding: 0.6rem 0.75rem; }
.runtime-fact-stream article span { grid-row: 1 / 3; color: var(--ui-accent); font: 0.58rem ui-monospace, monospace; }
.runtime-fact-stream article strong { font-size: 0.68rem; }
.runtime-fact-stream article small { color: var(--ui-label-secondary); font: 0.55rem ui-monospace, monospace; }
.runtime-loading-line { position: absolute; top: 0; left: 0; width: 35%; height: 2px; background: var(--ui-accent); animation: runtime-loading 1s steps(8, end) infinite; }
@keyframes runtime-loading { from { transform: translateX(-100%); } to { transform: translateX(385%); } }
@media (max-width: 1000px) {
  .runtime-actions-grid, .runtime-governance-grid { grid-template-columns: 1fr; }
  .runtime-actions-grid > section:first-child, .runtime-governance-grid > section { border-right: 0; border-bottom: 1px solid var(--ui-separator); }
}
@media (max-width: 640px) {
  .runtime-header, .runtime-identity-card { align-items: flex-start; grid-template-columns: 1fr; flex-direction: column; }
  .runtime-counters { width: 100%; overflow-x: auto; }
  .runtime-create-controls { grid-template-columns: 1fr; }
  .runtime-active-roles { justify-content: flex-start; }
}
@media (prefers-reduced-motion: reduce) { .runtime-loading-line { animation: none; width: 100%; } }
</style>
