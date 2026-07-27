import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const archiveAPI = vi.hoisted(() => ({
  getConfig: vi.fn(),
  getRuntime: vi.fn(),
  listRecords: vi.fn(),
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
      locale: { value: 'en-US' },
    }),
  }
})
vi.mock('../api', () => ({ default: archiveAPI }))
vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn() }),
}))
vi.mock('@/composables/useStepUp', () => ({
  useStepUp: () => ({ run: (operation: () => Promise<unknown>) => operation() }),
  isStepUpBlocked: () => false,
  isStepUpCancelled: () => false,
  stepUpBlockReason: () => '',
}))
vi.mock('@/utils/apiError', () => ({
  extractApiErrorCode: () => '',
  extractApiErrorMessage: (_error: unknown, fallback: string) => fallback,
}))

import InvocationArchiveView from '../InvocationArchiveView.vue'

const config = {
  config_version: 1,
  default_mode: 'off',
  retention_days: 7,
  max_request_bytes: 1024,
  max_response_bytes: 1024,
  direct_view_enabled: false,
  rules: [],
  updated_at: '',
  updated_by: 0,
}
const runtime = {
  started: true,
  config_version: 1,
  queue_depth: 0,
  queue_capacity: 100,
  persisted: 0,
  accepted: 0,
  dropped: 0,
  expired_purged: 0,
  persist_failures: 0,
  last_config_error: '',
  last_persist_error: '',
}
const records = {
  items: [{
    id: 42,
    created_at: '2026-07-27T00:00:00Z',
    expires_at: '2026-08-03T00:00:00Z',
    mode: 'full',
    outcome: 'completed',
    http_status: 200,
    method: 'POST',
    path: '/v1/chat/completions',
    model: 'test-model',
    user_label: 'admin@example.test',
    api_key_name: 'test-key',
    group_name: 'default',
    transport: 'http',
    websocket_turn: 0,
    request_status: 'captured',
    request_captured_bytes: 32,
    request_total_bytes: 32,
    request_truncated: false,
    response_status: 'captured',
    response_captured_bytes: 64,
    response_total_bytes: 64,
    response_truncated: false,
  }],
  page: 1,
  page_size: 20,
  total: 1,
}

function mountView() {
  return mount(InvocationArchiveView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        BaseDialog: true,
        ConfirmDialog: true,
        Pagination: true,
        TotpStepUpDialog: true,
      },
    },
  })
}

describe('InvocationArchiveView', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    archiveAPI.getConfig.mockReset().mockResolvedValue(config)
    archiveAPI.getRuntime.mockReset().mockResolvedValue(runtime)
    archiveAPI.listRecords.mockReset().mockResolvedValue(records)
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('uses the shared page header, refreshes records, and keeps automatic refresh alive only while mounted', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.findAll('h1')).toHaveLength(0)
    expect(wrapper.text()).not.toContain('common.settings')
    expect(wrapper.text()).toContain('admin.invocationArchive.refresh.updatedAt')
    expect(archiveAPI.getConfig).toHaveBeenCalledTimes(1)
    expect(archiveAPI.getRuntime).toHaveBeenCalledTimes(1)
    expect(archiveAPI.listRecords).toHaveBeenCalledTimes(1)

    await wrapper.get('button.btn-secondary.btn-sm').trigger('click')
    await flushPromises()
    expect(archiveAPI.getRuntime).toHaveBeenCalledTimes(2)
    expect(archiveAPI.listRecords).toHaveBeenCalledTimes(2)

    await vi.advanceTimersByTimeAsync(15_000)
    await flushPromises()
    expect(archiveAPI.getRuntime).toHaveBeenCalledTimes(3)
    expect(archiveAPI.listRecords).toHaveBeenCalledTimes(3)

    wrapper.unmount()
    await vi.advanceTimersByTimeAsync(15_000)
    expect(archiveAPI.getRuntime).toHaveBeenCalledTimes(3)
    expect(archiveAPI.listRecords).toHaveBeenCalledTimes(3)
  })
})
