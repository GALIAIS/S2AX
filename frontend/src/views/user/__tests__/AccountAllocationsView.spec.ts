import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import AccountAllocationsView from '../AccountAllocationsView.vue'

const { listVisible } = vi.hoisted(() => ({
  listVisible: vi.fn(),
}))

const messages: Record<string, string> = {
  'accountAllocations.readOnlyTitle': 'Read-only account directory',
  'accountAllocations.readOnlyDescription': 'Safe summary only',
  'accountAllocations.privacyNotice': 'Sensitive values stay hidden',
  'accountAllocations.summary.publicGroups': 'Public groups',
  'accountAllocations.summary.dedicatedGroups': 'Dedicated groups',
  'accountAllocations.summary.visibleAccounts': 'Visible accounts',
  'accountAllocations.summary.readyAccounts': 'Ready now',
  'accountAllocations.searchLabel': 'Search account directory',
  'accountAllocations.searchPlaceholder': 'Search accounts',
  'accountAllocations.filters.allSources': 'All sources',
  'accountAllocations.filters.allGroups': 'All groups',
  'accountAllocations.filters.allPlatforms': 'All platforms',
  'accountAllocations.filters.allStatuses': 'All statuses',
  'accountAllocations.sourcePublic': 'Public group',
  'accountAllocations.sourceDedicated': 'Dedicated assignment',
  'accountAllocations.status.ready': 'Ready',
  'accountAllocations.status.cooling': 'Cooling',
  'accountAllocations.status.unavailable': 'Unavailable',
  'accountAllocations.viewMode': 'View mode',
  'accountAllocations.viewModes.list': 'List view',
  'accountAllocations.viewModes.grid': 'Grid view',
  'accountAllocations.showing': 'Showing {count} of {total} accounts',
  'accountAllocations.account': 'Account',
  'accountAllocations.group': 'Group',
  'accountAllocations.platformType': 'Platform / type',
  'accountAllocations.capacity': 'Concurrency',
  'accountAllocations.usage': 'Usage',
  'accountAllocations.concurrentRequests': 'concurrent requests',
  'accountAllocations.masked': 'Masked',
  'accountAllocations.groupTypes.standard': 'Standard group',
  'accountAllocations.groupTypes.subscription': 'Subscription group',
  'accountAllocations.usageScopes.rolling24h': 'This group, last 24 hours',
  'accountAllocations.usageScopes.personalLease': 'Your current assignment',
  'accountAllocations.usageWindows.rolling24h': '24h',
  'accountAllocations.usageWindows.personalLease': 'Lease',
  'accountAllocations.readyHint': 'Currently schedulable',
  'accountAllocations.unavailableHint': 'Currently unavailable',
  'accountAllocations.viewDetails': 'View details',
  'accountAllocations.detailTitle': 'Account details',
  'accountAllocations.detailPrivacyNotice': 'Sensitive details stay hidden',
  'accountAllocations.source': 'Source',
  'accountAllocations.groupType': 'Group type',
  'accountAllocations.tokenUsage': 'Token usage',
  'accountAllocations.lastActivity': 'Recent activity',
  'accountAllocations.upstreamQuota': 'Upstream quota',
  'accountAllocations.cachedQuotaSnapshot': 'Cached snapshot',
  'accountAllocations.leaseAccountCost': 'Lease account cost',
  'accountAllocations.leaseUserCost': 'Lease user cost',
  'accountAllocations.requests': 'requests',
  'accountAllocations.tokens': 'tokens',
  'accountAllocations.assignedAt': 'Assigned',
  'accountAllocations.coolingUntilLabel': 'Expected recovery',
  'accountAllocations.unknownPlatform': 'Unknown platform',
  'common.retry': 'Retry',
  'common.refresh': 'Refresh',
  'common.refreshing': 'Refreshing',
  'common.status': 'Status',
  'common.close': 'Close',
}

vi.mock('@/api/accountAllocations', () => ({
  default: { listVisible },
}))

vi.mock('@/utils/apiError', () => ({
  extractApiErrorMessage: (error: unknown) => error instanceof Error ? error.message : 'Load failed',
}))

vi.mock('@/utils/format', () => ({
  formatDateTime: (value: string | null | undefined) => value ?? '',
  formatCompactNumber: (value: number) => String(value),
  formatCostFixed: (value: number) => value.toFixed(2),
  formatNumber: (value: number) => String(value),
  formatRelativeTime: (value: string | null | undefined) => value ? '5m ago' : 'never',
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, values?: Record<string, number>) => {
        const template = messages[key] ?? key
        return values
          ? template.replace('{count}', String(values.count ?? '')).replace('{total}', String(values.total ?? ''))
          : template
      },
    }),
  }
})

const AppLayoutStub = { template: '<div><slot /></div>' }
const IconStub = { template: '<span aria-hidden="true" />' }
const PlatformTypeBadgeStub = { props: ['platform', 'type'], template: '<span>{{ platform }} / {{ type }}</span>' }
const UsageProgressBarStub = { props: ['label', 'utilization'], template: '<span data-test="quota-window">{{ label }} {{ utilization }}</span>' }
const SelectStub = {
  props: ['modelValue', 'options'],
  template: '<select :value="modelValue"><option v-for="option in options" :key="String(option.value)">{{ option.label }}</option></select>',
}
const DataTableStub = {
  props: ['data'],
  emits: ['rowClick', 'retry'],
  template: '<div data-test="table"><button v-for="row in data" :key="row.view_key" data-test="table-row" @click="$emit(\'rowClick\', row)">{{ row.account_name }}</button></div>',
}
const BaseDialogStub = {
  props: ['show', 'title'],
  template: '<div v-if="show" data-test="dialog"><h2>{{ title }}</h2><slot /><slot name="footer" /></div>',
}

const visibleOverview = {
  items: [
    {
      view_key: 'visible-public-1',
      source: 'public',
      group_id: 4,
      group_name: 'Open Pool',
      subscription_type: 'standard',
      account_name: 'a***e@example.com',
      account_name_masked: true,
      platform: 'openai',
      account_type: 'oauth',
      capacity: { concurrency: 3 },
      status: 'ready',
      last_activity_at: '2026-07-23T11:55:00Z',
      usage: { scope: 'rolling_24h', request_count: 12, total_tokens: 3456 },
    },
    {
      view_key: 'visible-dedicated-1',
      source: 'dedicated',
      group_id: 8,
      group_name: 'VIP Pool',
      subscription_type: 'subscription',
      account_name: 'Private Pool 02',
      account_name_masked: false,
      platform: 'anthropic',
      account_type: 'apikey',
      capacity: { concurrency: 2 },
      status: 'cooling',
      rate_limit_reset_at: '2026-07-23T12:00:00Z',
      last_activity_at: null,
      usage: { scope: 'personal_lease', request_count: 8, total_tokens: 1234, account_cost: 2.09, user_cost: 1.69 },
      upstream_quota: {
        updated_at: '2026-07-23T11:55:00Z',
        five_hour: { utilization: 35, resets_at: '2026-07-23T15:00:00Z' },
        seven_day: { utilization: 12, resets_at: '2026-07-30T10:00:00Z' },
      },
      assigned_at: '2026-07-23T10:00:00Z',
    },
  ],
  summary: {
    public_group_count: 1,
    dedicated_group_count: 1,
    public_account_count: 1,
    dedicated_account_count: 1,
    ready_account_count: 1,
  },
}

const mountView = () => mount(AccountAllocationsView, {
  global: {
    stubs: {
      AppLayout: AppLayoutStub,
      BaseDialog: BaseDialogStub,
      DataTable: DataTableStub,
      Select: SelectStub,
      Icon: IconStub,
      PlatformTypeBadge: PlatformTypeBadgeStub,
      UsageProgressBar: UsageProgressBarStub,
    },
  },
})

describe('AccountAllocationsView', () => {
  beforeEach(() => {
    window.localStorage.clear()
    listVisible.mockReset()
    listVisible.mockResolvedValue(visibleOverview)
  })

  it('renders only server-masked names and opens a read-only detail view', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(listVisible).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('a***e@example.com')
    expect(wrapper.text()).not.toContain('alice@example.com')
    expect(wrapper.text()).toContain('Public group')
    expect(wrapper.text()).toContain('Dedicated assignment')

    await wrapper.get('[data-test="table-row"]').trigger('click')
    expect(wrapper.get('[data-test="dialog"]').text()).toContain('a***e@example.com')
    expect(wrapper.get('[data-test="dialog"]').findAll('button')).toHaveLength(1)
  })

  it('switches to the persisted grid view without requesting a broader dataset', async () => {
    const wrapper = mountView()
    await flushPromises()

    const gridButton = wrapper.findAll('button').find((button) => button.attributes('title') === 'Grid view')
    expect(gridButton).toBeDefined()
    await gridButton!.trigger('click')

    expect(wrapper.text()).toContain('View details')
    expect(wrapper.text()).toContain('openai / oauth')
    expect(wrapper.text()).toContain('Recent activity · 5m ago')
    expect(window.localStorage.getItem('user-account-directory-view-mode')).toBe('grid')
    expect(listVisible).toHaveBeenCalledTimes(1)
  })

  it('renders cached upstream quota only for the dedicated account', async () => {
    const wrapper = mountView()
    await flushPromises()

    const gridButton = wrapper.findAll('button').find((button) => button.attributes('title') === 'Grid view')
    await gridButton!.trigger('click')
    expect(wrapper.findAll('[data-test="quota-window"]')).toHaveLength(2)
    expect(wrapper.text()).toContain('5h 35')
    expect(wrapper.text()).toContain('7d 12')
    expect(wrapper.text()).toContain('A $2.09')
    expect(wrapper.text()).toContain('U $1.69')

    await wrapper.findAll('button.card')[1].trigger('click')
    expect(wrapper.get('[data-test="dialog"]').text()).toContain('Upstream quota')
  })
})
