import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const archiveAPI = vi.hoisted(() => ({
  getConfig: vi.fn(),
  getRuntime: vi.fn(),
  listRecords: vi.fn(),
  getRecord: vi.fn(),
  listAccessLogs: vi.fn(),
  revealPayloadChunk: vi.fn(),
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
  direct_view_enabled: true,
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

const detailRecord = {
  ...records.items[0],
  completed_at: '2026-07-27T00:00:01Z',
  config_version: 1,
  request_id: 'req-42',
  client_request_id: '',
  client_ip: '127.0.0.1',
  user_agent: 'vitest',
  request_content_type: 'application/json',
  response_content_type: 'text/event-stream',
}

function payloadChunk(slot: 'request' | 'response') {
  const response = slot === 'response'
  return {
    record_id: 42,
    slot,
    next_offset: response ? 97 : 82,
    payload: {
      available: true,
      status: 'captured',
      content_type: response ? 'text/event-stream; charset=utf-8' : 'application/json; charset=utf-8',
      encoding: 'utf8',
      compression: 'none',
      data: response
        ? 'event: message\\ndata: {"choices":[{"delta":{"content":"hello"}}]}\\n\\n'
        : '{"messages":[{"role":"user","content":"hello"}]}',
      total_bytes: response ? 97 : 82,
      captured_bytes: response ? 97 : 82,
      offset: 0,
      loaded_bytes: response ? 97 : 82,
      complete: true,
      truncated: false,
    },
  }
}

function mountView() {
  return mount(InvocationArchiveView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /></div>' },
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
    archiveAPI.getRecord.mockReset().mockResolvedValue(detailRecord)
    archiveAPI.listAccessLogs.mockReset().mockResolvedValue([])
    archiveAPI.revealPayloadChunk.mockReset().mockImplementation((_id: number, slot: 'request' | 'response') => Promise.resolve(payloadChunk(slot)))
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

  it('keeps request and response payloads in equal panels with their own scrolling viewer', async () => {
    const wrapper = mountView()
    await flushPromises()

    const viewButton = wrapper.findAll('button').find((button) => button.text() === 'common.view')
    expect(viewButton).toBeDefined()
    await viewButton!.trigger('click')
    await flushPromises()

    const revealButton = wrapper.findAll('button').find((button) => button.text() === 'admin.invocationArchive.detail.reveal')
    expect(revealButton).toBeDefined()
    await revealButton!.trigger('click')
    await flushPromises()

    const grid = wrapper.find('.archive-payload-grid')
    expect(grid.classes()).toContain('grid')
    expect(grid.classes()).not.toContain('flex')
    expect(wrapper.findAll('.archive-payload-panel')).toHaveLength(2)
    expect(wrapper.findAll('.archive-payload-viewer')).toHaveLength(2)
    expect(wrapper.findAll('.archive-payload-viewer').every((viewer) => viewer.classes().includes('bg-gray-50'))).toBe(true)
  })
})
