import { afterEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import type {
  WorldActor,
  WorldActorRoleOption,
  WorldActorState,
  WorldRuntimeCatalog,
  WorldRuntimeDefinition
} from '@/api/citySpatial'
import zh from '@/i18n/locales/zh/common'
import CityWorldRuntimePanel from '../CityWorldRuntimePanel.vue'

function runtimeMessages(value: unknown): any {
  if (typeof value === 'string') return () => value
  if (!value || typeof value !== 'object') return value
  return Object.fromEntries(Object.entries(value).map(([key, child]) => [key, runtimeMessages(child)]))
}

const i18n = createI18n({ legacy: false, locale: 'zh', messages: { zh: runtimeMessages(zh) } })

function definition(
  kind: WorldRuntimeDefinition['kind'],
  code: string,
  payload: Record<string, unknown>
): WorldRuntimeDefinition {
  return {
    kind,
    code,
    version: '1.0.0',
    hash: code.padEnd(64, '0').slice(0, 64),
    visibility: 'public',
    payload
  }
}

const catalog: WorldRuntimeCatalog = {
  profile: {
    runtime_id: 'sub2api-open-world', runtime_version: '1.0.0', catalog_version: '1.0.0',
    catalog_hash: 'a'.repeat(64), baseline_tick: 0, maximum_player_actors_per_member: 3,
    actor_count: 1, fact_count: 4, effect_count: 8, case_count: 1, revision: 5, metadata: {}
  },
  definitions: [
    definition('attribute', 'reasoning', {
      name_key: 'worldRuntime.attributes.reasoning', minimum_units: 0, maximum_units: 100000
    }),
    definition('attribute', 'coordination', {
      name_key: 'worldRuntime.attributes.coordination', minimum_units: 0, maximum_units: 100000
    }),
    definition('archetype', 'urban_apprentice', {
      name_key: 'worldRuntime.archetypes.urbanApprentice',
      description_key: 'worldRuntime.archetypes.urbanApprenticeDescription',
      initial_attributes: { reasoning: 16000, coordination: 12000 }
    }),
    definition('activity', 'technical_study', {
      name_key: 'worldRuntime.activities.technicalStudy',
      description_key: 'worldRuntime.activities.technicalStudyDescription',
      effects: [{ type: 'attribute.add', key: 'reasoning', value_units: 2500 }]
    }),
    definition('role', 'profession.apprentice', {
      name_key: 'worldRuntime.roles.apprentice',
      description_key: 'worldRuntime.roles.apprenticeDescription'
    }),
    definition('role', 'profession.technician', {
      name_key: 'worldRuntime.roles.technician',
      description_key: 'worldRuntime.roles.technicianDescription'
    }),
    definition('role', 'profession.magistrate', {
      name_key: 'worldRuntime.roles.magistrate',
      description_key: 'worldRuntime.roles.magistrateDescription'
    })
  ]
}

const actor: WorldActor = {
  code: 'actor_00000001', owner_user_id: 9, actor_type_code: 'character', name: 'Aster',
  status: 'active', archetype_code: 'urban_apprentice', archetype_version: '1.0.0',
  created_tick: 1, updated_tick: 3, version: 4, metadata: {}
}

const actorState: WorldActorState = {
  actor,
  attributes: [
    {
      actor_code: actor.code, attribute_code: 'reasoning', value_units: 22000,
      experience_units: 6000, last_changed_tick: 3, version: 3, metadata: {}
    },
    {
      actor_code: actor.code, attribute_code: 'coordination', value_units: 14000,
      experience_units: 2000, last_changed_tick: 2, version: 2, metadata: {}
    }
  ],
  roles: [{
    actor_code: actor.code, role_code: 'profession.apprentice', category_code: 'profession',
    status: 'active', granted_tick: 1, version: 1, metadata: {}
  }],
  statuses: [],
  recent_facts: []
}

function roleOption(
  roleCode: string,
  eligible: boolean,
  failure?: WorldActorRoleOption['evaluation']['failures'][number]
): WorldActorRoleOption {
  const roleDefinition = catalog.definitions.find(item => item.code === roleCode)!
  return {
    definition: roleDefinition,
    active: false,
    eligible,
    current_category_role: 'profession.apprentice',
    cooldown_remaining_ticks: 0,
    blocked_reason_codes: failure ? [failure.message_code] : [],
    evaluation: { satisfied: eligible, failures: failure ? [failure] : [] }
  }
}

afterEach(() => {
  document.body.innerHTML = ''
})

describe('CityWorldRuntimePanel', () => {
  it('creates a character from a versioned starter archetype using an exact runtime command', async () => {
    const wrapper = mount(CityWorldRuntimePanel, {
      props: {
        catalog: { ...catalog, profile: { ...catalog.profile, actor_count: 0 } },
        actors: [], selectedActorCode: null, actorState: null, roleOptions: [], rules: [], cases: [],
        loading: false, busyCommandCode: null
      },
      global: { plugins: [i18n] }
    })

    await wrapper.get('input[placeholder="输入角色名称"]').setValue('Aster')
    await wrapper.get('form.runtime-character-create').trigger('submit')

    expect(wrapper.emitted('command')?.[0]).toEqual([
      'actor.create',
      { name: 'Aster', archetype_code: 'urban_apprentice' },
      'actor.create'
    ])
  })

  it('emits server-authoritative activity and eligible role-transition intents only', async () => {
    const options = [
      roleOption('profession.technician', true),
      roleOption('profession.magistrate', false, {
        path: 'requirements.0', operator: 'attribute_min', code: 'reasoning',
        actual_units: 22000, required_units: 50000, message_code: 'requirement.attribute_min'
      })
    ]
    const wrapper = mount(CityWorldRuntimePanel, {
      props: {
        catalog, actors: [actor], selectedActorCode: actor.code, actorState,
        roleOptions: options, rules: [], cases: [], loading: false, busyCommandCode: null
      },
      global: { plugins: [i18n] }
    })

    const activity = wrapper.findAll('.runtime-activity-list article')
      .find(item => item.text().includes('技术研习'))!
    await activity.get('button').trigger('click')

    const roleCards = wrapper.findAll('.runtime-role-list article')
    const technician = roleCards.find(item => item.text().includes('技术员'))!
    const magistrate = roleCards.find(item => item.text().includes('Magistrate'))!
    expect(technician.get('button').attributes('disabled')).toBeUndefined()
    expect(magistrate.get('button').attributes('disabled')).toBeDefined()
    await technician.get('button').trigger('click')

    expect(wrapper.emitted('command')).toEqual([
      ['actor.activity.perform', { actor_code: actor.code, activity_code: 'technical_study' }, 'activity:technical_study'],
      ['actor.role.transition', { actor_code: actor.code, role_code: 'profession.technician' }, 'role:profession.technician']
    ])
  })
})
