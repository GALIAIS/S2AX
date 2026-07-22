import { afterEach, describe, expect, it } from 'vitest'
import { nextTick } from 'vue'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import type {
  CityMember,
  CityNavigationPath,
  WorldActor,
  WorldActorNavigationIntent,
  WorldActorLocation,
  WorldActorRoleOption,
  WorldActorState,
  WorldPortalAccessView,
  WorldNavigationReservation,
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

const actorLocation: WorldActorLocation = {
  actor_code: 'actor_00000001', space_kind: 'world', space_code: 'world',
  x: 10, y: 20, z: 0, chunk_x: 0, chunk_y: 0, local_x: 10, local_y: 20,
  jurisdiction_code: 'central', moved_tick: 1, version: 1, metadata: {}
}

const actor: WorldActor = {
  code: 'actor_00000001', owner_user_id: 9, actor_type_code: 'character', name: 'Aster',
  status: 'active', archetype_code: 'urban_apprentice', archetype_version: '1.0.0',
  created_tick: 1, updated_tick: 3, version: 4, metadata: {}, location: actorLocation
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
  recent_facts: [],
  location: actorLocation,
  control_grants: [
    {
      code: 'grant_owner_command', actor_code: actor.code, user_id: 9, capability: 'actor.command',
      status: 'active', granted_by_user_id: 9, granted_tick: 1, version: 1, metadata: {}
    },
    {
      code: 'grant_owner_manage', actor_code: actor.code, user_id: 9, capability: 'actor.control.manage',
      status: 'active', granted_by_user_id: 9, granted_tick: 1, version: 1, metadata: {}
    },
    {
      code: 'grant_delegate_command', actor_code: actor.code, user_id: 12, capability: 'actor.command',
      status: 'active', granted_by_user_id: 9, granted_tick: 2, version: 1, metadata: {}
    }
  ],
  capabilities: ['actor.command', 'actor.control.manage']
}

const portal: WorldPortalAccessView = {
  state: {
    building_code: 'building_central', portal_code: 'entrance_main', portal_type: 'entrance',
    state_code: 'open', access_requirement: { op: 'all' }, access_policy_hash: 'b'.repeat(64),
    changed_tick: 3, version: 1, metadata: {}
  },
  from: { x: 10, y: 20, z: 0 },
  to: { x: 11, y: 20, z: 0 },
  bidirectional: true,
  accessible: true,
  access_evaluation: { satisfied: true, failures: [] }
}

const navigationIntent: WorldActorNavigationIntent = {
  actor_code: actor.code,
  intent_code: 'navigation_intent_00000000000000000031',
  destination: { x: 12, y: 20, z: 0 },
  status: 'active',
  on_blocked: 'retry',
  priority: 1,
  max_steps: 128,
  budget_units: 80,
  budget_gain_units: 100,
  budget_cap_units: 400,
  blocked_attempts: 0,
  next_attempt_tick: 4,
  created_tick: 3,
  updated_tick: 3,
  source_fact: { tick: 3, sequence: 7 },
  version: 1,
  metadata: { schema_version: 1 }
}

const navigationReservation: WorldNavigationReservation = {
  tick: 4,
  sequence: 2,
  actor_code: actor.code,
  intent_code: navigationIntent.intent_code,
  from: { x: 10, y: 20, z: 0 },
  to: { x: 11, y: 20, z: 0 },
  target_key: '11:20:0',
  edge_key: '10:20:0|11:20:0',
  step_cost: 80,
  source_fact: { tick: 4, sequence: 5 },
  status: 'consumed',
  metadata: { schema_version: 1 }
}

const members: CityMember[] = [
  {
    user_id: 9, email: 'owner@example.com', username: 'owner', role: 'owner', status: 'active',
    joined_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z'
  },
  {
    user_id: 12, email: 'delegate@example.com', username: 'delegate', role: 'planner', status: 'active',
    joined_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z'
  },
  {
    user_id: 14, email: 'candidate@example.com', username: 'candidate', role: 'viewer', status: 'active',
    joined_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z'
  }
]

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
  it('emits exact-identity member management without replacing runtime content', async () => {
    const wrapper = mount(CityWorldRuntimePanel, {
      props: {
        catalog, actors: [actor], selectedActorCode: actor.code, actorState,
        roleOptions: [], rules: [], cases: [], members, commandReceipts: [], memberRole: 'owner', systemAdmin: true,
        memberBusyKey: null, loading: false, busyCommandCode: null
      },
      global: { plugins: [i18n] }
    })

    await wrapper.get('input[placeholder="精确输入邮箱或用户名"]').setValue('new@example.com')
    await wrapper.get('form.runtime-member-add').trigger('submit')
    const candidateMember = wrapper.findAll('.runtime-member-list article')
      .find(item => item.text().includes('candidate@example.com'))!
    await candidateMember.get('.runtime-member-remove').trigger('click')

    expect(wrapper.emitted('memberAdd')?.[0]).toEqual([
      { identity: 'new@example.com', role: 'viewer' }
    ])
    expect(wrapper.emitted('memberUpdate')?.[0]).toEqual([14, { status: 'left' }])
    expect(wrapper.get('.runtime-identity-card').text()).toContain('Aster')
  })

  it('does not elevate a world owner into a platform administrator', () => {
    const wrapper = mount(CityWorldRuntimePanel, {
      props: {
        catalog, actors: [actor], selectedActorCode: actor.code, actorState,
        roleOptions: [], rules: [], cases: [], members, commandReceipts: [], memberRole: 'owner',
        memberBusyKey: null, loading: false, busyCommandCode: null, portals: [portal]
      },
      global: { plugins: [i18n] }
    })

    expect(wrapper.find('form.runtime-member-add').exists()).toBe(false)
    expect(wrapper.find('form.runtime-control-form').exists()).toBe(false)
    expect(wrapper.find('form.runtime-portal-policy-form').exists()).toBe(false)
  })

  it('keeps a roster entry identifiable when member email is redacted', () => {
    const wrapper = mount(CityWorldRuntimePanel, {
      props: {
        catalog, actors: [actor], selectedActorCode: actor.code, actorState,
        roleOptions: [], rules: [], cases: [],
        members: [{ ...members[0], email: '', username: '' }],
        commandReceipts: [], memberRole: 'viewer', memberBusyKey: null,
        loading: false, busyCommandCode: null
      },
      global: { plugins: [i18n] }
    })

    expect(wrapper.get('.runtime-member-identity strong').text()).toBe('#9')
  })

  it('creates a character from a versioned starter archetype using an exact runtime command', async () => {
    const wrapper = mount(CityWorldRuntimePanel, {
      props: {
        catalog: { ...catalog, profile: { ...catalog.profile, actor_count: 0 } },
        actors: [], selectedActorCode: null, actorState: null, roleOptions: [], rules: [], cases: [],
        members, commandReceipts: [], memberRole: 'owner', memberBusyKey: null,
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
        roleOptions: options, rules: [], cases: [], members, commandReceipts: [], memberRole: 'owner',
        memberBusyKey: null, loading: false, busyCommandCode: null
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

  it('emits adjacent movement and capability-grant lifecycle commands from authoritative state', async () => {
    const wrapper = mount(CityWorldRuntimePanel, {
      props: {
        catalog, actors: [actor], selectedActorCode: actor.code, actorState,
        roleOptions: [], rules: [], cases: [], members, commandReceipts: [], memberRole: 'owner', systemAdmin: true,
        memberBusyKey: null, loading: false, busyCommandCode: null
      },
      global: { plugins: [i18n] }
    })

    await wrapper.get('.runtime-move-north').trigger('click')
    await wrapper.get('form.runtime-control-form .select-trigger').trigger('click')
    const candidate = [...document.body.querySelectorAll<HTMLElement>('.select-option')]
      .find(item => item.textContent?.includes('candidate@example.com'))
    expect(candidate).toBeTruthy()
    candidate!.click()
    await nextTick()
    await wrapper.get('form.runtime-control-form').trigger('submit')
    const delegated = wrapper.findAll('.runtime-delegation-list > article')
      .find(item => item.text().includes('#12'))!
    await delegated.get('.runtime-capability-list button').trigger('click')

    expect(wrapper.emitted('command')).toEqual([
      ['actor.location.move', {
        actor_code: actor.code, x: 10, y: 19, z: 0
      }, 'move:north'],
      ['actor.control.grant', {
        actor_code: actor.code, user_id: 14, capabilities: ['actor.command']
      }, 'control:grant:14'],
      ['actor.control.revoke', {
        actor_code: actor.code, user_id: 12, capabilities: ['actor.command']
      }, 'control:revoke:12:actor.command']
    ])
  })

  it('keeps delegated read-only actor state inspectable while disabling mutations', () => {
    const wrapper = mount(CityWorldRuntimePanel, {
      props: {
        catalog, actors: [actor], selectedActorCode: actor.code,
        actorState: { ...actorState, control_grants: [], capabilities: [] },
        roleOptions: [], rules: [], cases: [], members, commandReceipts: [], memberRole: 'viewer',
        memberBusyKey: null, loading: false, busyCommandCode: null
      },
      global: { plugins: [i18n] }
    })

    expect(wrapper.find('form.runtime-control-form').exists()).toBe(false)
    expect(wrapper.get('.runtime-move-north').attributes('disabled')).toBeDefined()
    expect(wrapper.get('.runtime-activity-list button').attributes('disabled')).toBeDefined()
  })

  it('previews a deterministic route and emits only its next adjacent movement step', async () => {
    const navigationPath: CityNavigationPath = {
      navigation_version: '1.0.0', world_tick: 3, spatial_rule_hash: 'a'.repeat(64),
      actor_code: actor.code,
      from: { x: 10, y: 20, z: 0 }, to: { x: 12, y: 20, z: 0 },
      reachable: true, total_cost: 160, expanded_nodes: 3,
      steps: [
        { coordinate: { x: 10, y: 20, z: 0 }, step_cost: 0, total_cost: 0, anchor_kind: 'chunk', anchor_code: 'chunk.z0.x0.y0', jurisdiction_code: 'central' },
        { coordinate: { x: 11, y: 20, z: 0 }, step_cost: 80, total_cost: 80, anchor_kind: 'chunk', anchor_code: 'chunk.z0.x0.y0', jurisdiction_code: 'central' },
        { coordinate: { x: 12, y: 20, z: 0 }, step_cost: 80, total_cost: 160, anchor_kind: 'chunk', anchor_code: 'chunk.z0.x0.y0', jurisdiction_code: 'central' }
      ]
    }
    const wrapper = mount(CityWorldRuntimePanel, {
      props: {
        catalog, actors: [actor], selectedActorCode: actor.code, actorState,
        roleOptions: [], rules: [], cases: [], members, commandReceipts: [], memberRole: 'owner', systemAdmin: true,
        memberBusyKey: null, loading: false, busyCommandCode: null,
        navigationPath, navigationDestination: { x: 12, y: 20, z: 0 }
      },
      global: { plugins: [i18n] }
    })

    await wrapper.get('.runtime-navigation-actions .btn').trigger('click')
    await wrapper.get('.runtime-navigation-result .btn-primary').trigger('click')

    expect(wrapper.emitted('previewPath')).toHaveLength(1)
    expect(wrapper.emitted('command')?.[0]).toEqual([
      'actor.location.move',
      { actor_code: actor.code, x: 11, y: 20, z: 0 },
      'move:navigation'
    ])
    expect(wrapper.get('.runtime-navigation-result').text()).toContain('2')
    expect(wrapper.findAll('.runtime-navigation-steps li')).toHaveLength(3)
  })

  it('shows authoritative movement scheduling and emits exact set and cancel intents', async () => {
    const wrapper = mount(CityWorldRuntimePanel, {
      props: {
        catalog, actors: [actor], selectedActorCode: actor.code,
        actorState: { ...actorState, navigation_intent: navigationIntent },
        roleOptions: [], rules: [], cases: [], members, commandReceipts: [], memberRole: 'owner',
        memberBusyKey: null, loading: false, busyCommandCode: null,
        navigationDestination: { x: 13, y: 20, z: 0 },
        navigationIntents: [navigationIntent], navigationReservations: [navigationReservation],
        navigationIntentAvailability: 'available', navigationIntentLoading: false
      },
      global: { plugins: [i18n] }
    })

    expect(wrapper.get('.runtime-navigation-intent-state').text()).toContain('执行中')
    expect(wrapper.get('.runtime-navigation-reservation').text()).toContain('10, 20, 0 → 11, 20, 0')
    await wrapper.get('.runtime-navigation-intent-form > .btn-secondary').trigger('click')
    await wrapper.get('form.runtime-navigation-intent-form').trigger('submit')
    await wrapper.get('.runtime-navigation-intent-form-actions .btn-secondary').trigger('click')

    expect(wrapper.emitted('command')).toEqual([
      ['actor.navigation.intent.set', {
        actor_code: actor.code,
        destination: { x: 13, y: 20, z: 0 },
        priority: 1,
        max_steps: 128,
        on_blocked: 'retry'
      }, `navigation:intent:set:${actor.code}`],
      ['actor.navigation.intent.cancel', {
        actor_code: actor.code
      }, `navigation:intent:cancel:${actor.code}`]
    ])
  })

  it('keeps an untouched navigation form synchronized with authoritative movement without overwriting a manual destination', async () => {
    const wrapper = mount(CityWorldRuntimePanel, {
      props: {
        catalog, actors: [actor], selectedActorCode: actor.code, actorState,
        roleOptions: [], rules: [], cases: [], members, commandReceipts: [], memberRole: 'owner',
        memberBusyKey: null, loading: false, busyCommandCode: null,
        navigationIntentAvailability: 'available', navigationIntentLoading: false
      },
      global: { plugins: [i18n] }
    })

    const fields = wrapper.findAll('.runtime-navigation-coordinate-fields input')
    expect((fields[0]!.element as HTMLInputElement).value).toBe('10')
    const movedLocation = { ...actorLocation, x: 11, version: 2 }
    await wrapper.setProps({
      actorState: {
        ...actorState,
        actor: { ...actor, location: movedLocation },
        location: movedLocation
      }
    })
    expect((fields[0]!.element as HTMLInputElement).value).toBe('11')

    await fields[0]!.setValue('25')
    const laterLocation = { ...movedLocation, x: 12, version: 3 }
    await wrapper.setProps({
      actorState: {
        ...actorState,
        actor: { ...actor, location: laterLocation },
        location: laterLocation
      }
    })
    expect((fields[0]!.element as HTMLInputElement).value).toBe('25')
  })

  it('offers an open-world portal traversal only while the actor stands at its registered endpoint', async () => {
    const wrapper = mount(CityWorldRuntimePanel, {
      props: {
        catalog: {
          ...catalog,
          profile: { ...catalog.profile, runtime_id: 'sub2api-city-open-world-social-runtime' }
        },
        actors: [actor], selectedActorCode: actor.code, actorState,
        roleOptions: [], rules: [], cases: [], members, commandReceipts: [], memberRole: 'owner',
        memberBusyKey: null, loading: false, busyCommandCode: null,
        portals: [portal], portalAccessAvailability: 'available', portalLoading: false
      },
      global: { plugins: [i18n] }
    })

    await wrapper.get('.runtime-portal-traverse').trigger('click')

    expect(wrapper.emitted('command')).toEqual([
      ['open_world.actor.portal.use', {
        actor_code: actor.code,
        portal_code: portal.state.portal_code
      }, 'portal:use:building_central/entrance_main']
    ])

    const remoteLocation = { ...actorLocation, x: 12, version: 2 }
    await wrapper.setProps({
      actorState: {
        ...actorState,
        actor: { ...actor, location: remoteLocation },
        location: remoteLocation
      }
    })
    expect(wrapper.find('.runtime-portal-traverse').exists()).toBe(false)
  })

  it('controls an adjacent portal and replaces its declarative policy through exact commands', async () => {
    const wrapper = mount(CityWorldRuntimePanel, {
      props: {
        catalog, actors: [actor], selectedActorCode: actor.code, actorState,
        roleOptions: [], rules: [], cases: [], members, commandReceipts: [], memberRole: 'owner', systemAdmin: true,
        memberBusyKey: null, loading: false, busyCommandCode: null,
        portals: [portal], portalAccessAvailability: 'available', portalLoading: false
      },
      global: { plugins: [i18n] }
    })

    expect(wrapper.get('.runtime-portal-list').text()).toContain('允许通过')
    await wrapper.get('.runtime-portal-actions button').trigger('click')

    const policySelects = wrapper.findAll('form.runtime-portal-policy-form .select-trigger')
    await policySelects[1]!.trigger('click')
    const roleMode = [...document.body.querySelectorAll<HTMLElement>('.select-option')]
      .find(item => item.textContent?.includes('要求生效职业'))
    expect(roleMode).toBeTruthy()
    roleMode!.click()
    await nextTick()
    await wrapper.get('form.runtime-portal-policy-form').trigger('submit')

    expect(wrapper.emitted('command')).toEqual([
      ['portal.state.transition', {
        actor_code: actor.code,
        building_code: 'building_central', portal_code: 'entrance_main', action: 'close'
      }, 'portal:state:building_central/entrance_main:close'],
      ['portal.access.configure', {
        building_code: 'building_central', portal_code: 'entrance_main',
        requirements: { op: 'role_active', role_code: 'profession.apprentice' }
      }, 'portal:policy:building_central/entrance_main']
    ])
  })
})
