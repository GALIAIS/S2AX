import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import AccountAllocationsView from '../AccountAllocationsView.vue'

const api = vi.hoisted(() => ({
  listPolicies: vi.fn(),
  getCapabilities: vi.fn(),
  getOverview: vi.fn(),
  reconcileAll: vi.fn(),
  getById: vi.fn(),
  create: vi.fn(),
  update: vi.fn(),
  remove: vi.fn(),
  setStatus: vi.fn(),
  reconcile: vi.fn(),
  listAssignments: vi.fn(),
  listCandidates: vi.fn(),
  assign: vi.fn(),
  release: vi.fn(),
  listEvents: vi.fn(),
  listUsers: vi.fn(),
  listGroups: vi.fn(),
}))

const notifications = vi.hoisted(() => ({
  showSuccess: vi.fn(),
  showError: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accountAllocations: {
      list: api.listPolicies,
      getCapabilities: api.getCapabilities,
      getOverview: api.getOverview,
      reconcileAll: api.reconcileAll,
      getById: api.getById,
      create: api.create,
      update: api.update,
      remove: api.remove,
      setStatus: api.setStatus,
      reconcile: api.reconcile,
      listAssignments: api.listAssignments,
      listCandidates: api.listCandidates,
      assign: api.assign,
      release: api.release,
      listEvents: api.listEvents,
    },
    users: { list: api.listUsers },
    groups: { getAll: api.listGroups },
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => notifications,
}))

vi.mock('@/composables/usePersistedPageSize', () => ({
  getPersistedPageSize: () => 20,
}))

vi.mock('@/utils/apiError', () => ({
  extractApiErrorMessage: (error: unknown, fallback: string) =>
    error instanceof Error ? error.message : fallback,
}))

vi.mock('@/utils/format', () => ({
  formatDateTime: (value: string | null | undefined) => value ?? '',
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, values?: Record<string, string | number>) => {
        if (!values) return key
        return `${key} ${Object.entries(values).map(([name, value]) => `${name}=${value}`).join(' ')}`
      },
    }),
  }
})

const policy = {
  id: 41,
  user_id: 8,
  user_email: 'member@example.com',
  username: 'member',
  group_id: 12,
  group_name: 'Dedicated pool',
  group_platform: 'openai',
  desired_count: 2,
  auto_replenish: true,
  replace_on_401: true,
  replace_on_429: true,
  status: 'active',
  access_status: 'group_access_required',
  created_at: '2026-07-23T10:00:00Z',
  updated_at: '2026-07-23T10:00:00Z',
  last_reconciled_at: '2026-07-23T10:05:00Z',
  active_assignment_count: 0,
  shortage: 2,
} as const

const overview = {
  policy_count: 3,
  active_policy_count: 2,
  disabled_policy_count: 1,
  blocked_policy_count: 1,
  desired_account_count: 5,
  active_assignment_count: 3,
  shortage_count: 2,
  policies_with_shortage: 1,
  last_policy_reconciled_at: '2026-07-23T10:05:00Z',
  reconcile_interval_seconds: 15,
}

const AppLayoutStub = { template: '<div><slot /></div>' }
const TablePageLayoutStub = {
  template: '<div><slot name="actions" /><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>',
}
const IconStub = { template: '<span aria-hidden="true" />' }
const SelectStub = {
  props: ['modelValue', 'options', 'disabled'],
  emits: ['update:modelValue', 'change', 'search'],
  template: '<select :disabled="disabled"><option v-for="option in options" :key="String(option.value)">{{ option.label }}</option></select>',
}
const DataTableStub = {
  props: ['data', 'loading', 'error'],
  emits: ['retry'],
  template: `
    <div data-test="policy-table" :data-loading="String(loading)">
      <div v-for="row in data" :key="row.id" data-test="policy-row">
        <slot name="cell-user_email" :row="row" />
        <slot name="cell-status" :row="row" />
        <slot name="cell-actions" :row="row" />
      </div>
    </div>
  `,
}
const RowActionMenuStub = {
  props: ['items'],
  emits: ['select'],
  template: '<button data-test="open-details" @click="$emit(\'select\', items[0].key)">details</button>',
}
const PaginationStub = {
  emits: ['update:page', 'update:pageSize'],
  template: '<button data-test="next-page" @click="$emit(\'update:page\', 2)">next</button>',
}
const BaseDialogStub = {
  props: ['show', 'title'],
  template: '<div v-if="show" data-test="dialog"><h2>{{ title }}</h2><slot /><slot name="footer" /></div>',
}
const ConfirmDialogStub = { template: '<div />' }

const mountView = () => mount(AccountAllocationsView, {
  global: {
    stubs: {
      AppLayout: AppLayoutStub,
      TablePageLayout: TablePageLayoutStub,
      DataTable: DataTableStub,
      Pagination: PaginationStub,
      BaseDialog: BaseDialogStub,
      ConfirmDialog: ConfirmDialogStub,
      RowActionMenu: RowActionMenuStub,
      Select: SelectStub,
      Icon: IconStub,
    },
  },
})

describe('Admin AccountAllocationsView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    api.listPolicies.mockResolvedValue({ items: [policy], total: 1, page: 1, page_size: 20 })
    api.getCapabilities.mockResolvedValue({ max_desired_count: 50, reconcile_interval_seconds: 15 })
    api.getOverview.mockResolvedValue(overview)
    api.getById.mockResolvedValue(policy)
    api.listAssignments.mockResolvedValue([])
    api.listCandidates.mockResolvedValue([])
    api.listEvents.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20 })
    api.listUsers.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 50 })
    api.listGroups.mockResolvedValue([])
    api.reconcileAll.mockResolvedValue({
      processed: 2,
      items: [
        {
          policy_id: 41,
          desired_count: 2,
          active_before: 0,
          active_after: 0,
          released_count: 0,
          assigned_count: 0,
          shortage: 2,
          skipped_concurrent: false,
          access_status: 'group_access_required',
        },
      ],
    })
  })

  it('surfaces blocked access and does not query assignable accounts for an ineligible policy', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('member@example.com')
    expect(wrapper.text()).toContain('admin.accountAllocations.accessStatus.group_access_required')
    expect(wrapper.text()).toContain('3 / 5')

    await wrapper.get('[data-test="open-details"]').trigger('click')
    await flushPromises()

    expect(api.getById).toHaveBeenCalledWith(41)
    expect(api.listAssignments).toHaveBeenCalledWith(41)
    expect(api.listEvents).toHaveBeenCalledWith(41)
    expect(api.listCandidates).not.toHaveBeenCalled()
    expect(wrapper.get('[data-test="dialog"]').text()).toContain('admin.accountAllocations.accessBlockedHint')
  })

  it('reconciles the whole control plane and refreshes overview and policy data', async () => {
    const wrapper = mountView()
    await flushPromises()
    vi.clearAllMocks()

    const reconcileButton = wrapper.findAll('button').find((button) =>
      button.text().includes('admin.accountAllocations.reconcileAll')
    )
    expect(reconcileButton).toBeDefined()
    await reconcileButton!.trigger('click')
    await flushPromises()

    expect(api.reconcileAll).toHaveBeenCalledTimes(1)
    expect(api.listPolicies).toHaveBeenCalledTimes(1)
    expect(api.getOverview).toHaveBeenCalledTimes(1)
    expect(notifications.showSuccess).toHaveBeenCalledWith(
      expect.stringContaining('processed=2')
    )
    expect(notifications.showSuccess).toHaveBeenCalledWith(
      expect.stringContaining('shortage=2')
    )
  })

  it('keeps existing rows mounted while a page request is pending', async () => {
    let resolveSecondRequest: ((value: unknown) => void) | undefined
    const secondRequest = new Promise((resolve) => {
      resolveSecondRequest = resolve
    })
    api.listPolicies
      .mockResolvedValueOnce({ items: [policy], total: 21, page: 1, page_size: 20 })
      .mockReturnValueOnce(secondRequest)

    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.get('[data-test="policy-table"]').attributes('data-loading')).toBe('false')

    await wrapper.get('[data-test="next-page"]').trigger('click')

    expect(wrapper.text()).toContain('member@example.com')
    expect(wrapper.get('[data-test="policy-table"]').attributes('data-loading')).toBe('false')

    resolveSecondRequest?.({ items: [], total: 21, page: 2, page_size: 20 })
    await flushPromises()
    expect(wrapper.findAll('[data-test="policy-row"]')).toHaveLength(0)
  })
})
