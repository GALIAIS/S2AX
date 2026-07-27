import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import SecurityAuditWorkbench from '../components/SecurityAuditWorkbench.vue'

const mocks = vi.hoisted(() => ({
  getSecurityOverview: vi.fn(),
  listSecurityNotifications: vi.fn(),
  listSecurityDecisions: vi.fn(),
  listSecurityCases: vi.fn(),
  listSecurityPolicies: vi.fn(),
  listSecurityPolicyVersions: vi.fn(),
  listSecurityActions: vi.fn(),
  listSecurityBehaviorSignals: vi.fn(),
  listSecurityExceptions: vi.fn(),
  listSecurityEndpointHealth: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn(),
}))

vi.mock('../api', () => ({ default: mocks }))
vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess: mocks.showSuccess, showError: mocks.showError }),
}))
vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      locale: { value: 'en' },
      t: (key: string, params?: Record<string, unknown>) =>
        key.replace(/\{(\w+)\}/g, (_, token) => String(params?.[token] ?? `{${token}}`)),
    }),
  }
})

function overview(total = 12) {
  return {
    window_hours: 24,
    total_decisions: total,
    allowed: 8,
    warned: 3,
    blocked: 1,
    degraded: 0,
    open_cases: 2,
    pending_actions: 1,
    failed_actions: 0,
    active_policies: 1,
    active_exceptions: 0,
    behavior_matches: 4,
    unread_notifications: 1,
    signal_lag_seconds: 8,
    signal_last_aggregated_at: '2026-07-24T10:00:00Z',
    signal_last_error: '',
    by_source: { prompt_audit: 9, behavior: 3 },
  }
}

const emptyPage = { items: [], total: 0, page: 1, page_size: 20, pages: 0 }

function mountWorkbench() {
  return mount(SecurityAuditWorkbench, {
    global: {
      stubs: {
        Icon: { template: '<span />' },
        Select: { props: ['modelValue', 'options'], template: '<div data-test="select" />' },
        Pagination: { template: '<div data-test="pagination" />' },
        BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' },
        TotpStepUpDialog: { template: '<div />' },
      },
    },
  })
}

describe('SecurityAuditWorkbench', () => {
  beforeEach(() => {
    Object.values(mocks).forEach((mock) => mock.mockReset())
    mocks.getSecurityOverview.mockResolvedValue(overview())
    mocks.listSecurityNotifications.mockResolvedValue([])
    mocks.listSecurityDecisions.mockResolvedValue(emptyPage)
    mocks.listSecurityCases.mockResolvedValue(emptyPage)
    mocks.listSecurityPolicies.mockResolvedValue([])
    mocks.listSecurityActions.mockResolvedValue(emptyPage)
    mocks.listSecurityBehaviorSignals.mockResolvedValue(emptyPage)
    mocks.listSecurityExceptions.mockResolvedValue([])
    mocks.listSecurityEndpointHealth.mockResolvedValue([])
  })

  it('loads the overview and switches sections without remounting the workbench', async () => {
    const wrapper = mountWorkbench()
    await flushPromises()

    expect(mocks.getSecurityOverview).toHaveBeenCalledWith(24)
    expect(mocks.listSecurityNotifications).toHaveBeenCalledWith({
      status: 'unread',
      audience: 'admin',
      limit: 10,
    })
    expect(wrapper.text()).toContain('12')

    await wrapper.get('[data-test="core-decisions"]').trigger('click')
    await flushPromises()
    expect(mocks.listSecurityDecisions).toHaveBeenCalledWith(expect.objectContaining({ page: 1, page_size: 20 }))
    expect(wrapper.find('[data-test="security-audit-workbench"]').exists()).toBe(true)

    await wrapper.get('[data-test="core-cases"]').trigger('click')
    await flushPromises()
    expect(mocks.listSecurityCases).toHaveBeenCalledWith(expect.objectContaining({ page: 1, page_size: 20 }))
  })

  it('keeps previously rendered metrics while a local refresh is pending', async () => {
    const wrapper = mountWorkbench()
    await flushPromises()
    expect(wrapper.text()).toContain('12')

    let resolveRefresh!: (value: ReturnType<typeof overview>) => void
    mocks.getSecurityOverview.mockImplementationOnce(() => new Promise((resolve) => {
      resolveRefresh = resolve
    }))
    const refresh = wrapper.findAll('button').find((button) =>
      button.text().includes('admin.promptAudit.actions.refresh'))
    expect(refresh).toBeTruthy()
    await refresh!.trigger('click')

    expect(wrapper.text()).toContain('12')
    resolveRefresh(overview(20))
    await flushPromises()
    expect(wrapper.text()).toContain('20')
  })
})
