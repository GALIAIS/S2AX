import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import PromptJobWorkspace from '../components/PromptJobWorkspace.vue'
import type { PromptAuditAdminJob } from '../types'

const mocks = vi.hoisted(() => ({
  listJobs: vi.fn(),
  transitionJob: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn(),
  stepUpRun: vi.fn(async (operation: () => Promise<unknown>) => operation()),
}))

vi.mock('../api', () => ({
  default: {
    listJobs: mocks.listJobs,
    transitionJob: mocks.transitionJob,
  },
}))
vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess: mocks.showSuccess, showError: mocks.showError }),
}))
vi.mock('@/composables/useStepUp', () => ({
  useStepUp: () => ({ run: mocks.stepUpRun }),
  isStepUpBlocked: () => false,
  isStepUpCancelled: () => false,
  stepUpBlockReason: () => '',
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

const DialogStub = defineComponent({
  props: ['show', 'title'],
  emits: ['close'],
  template: '<div v-if="show" data-test="job-operation-dialog"><slot /></div>',
})
const PaginationStub = defineComponent({
  props: ['total', 'page', 'pageSize'],
  emits: ['update:page', 'update:pageSize'],
  template: '<div data-test="job-pagination" />',
})

function failedJob(payloadState: PromptAuditAdminJob['payload_state']): PromptAuditAdminJob {
  return {
    job: {
      id: 12,
      snapshot: {
        request_id: 'request-12',
        user_id: 3,
        username: 'alice',
        user_email: 'alice@example.test',
        api_key_id: 4,
        api_key_name: 'alice-key',
        group_id: 5,
        group_name: 'Default',
        provider: 'openai',
        endpoint: '/v1/chat/completions',
        protocol: 'openai_chat',
        model: 'guarded-model',
        prompt_hash: 'a'.repeat(64),
        redacted_preview: 'redacted preview',
        prompt_length: 16,
        message_count: 1,
        stage: 'http',
      },
      execution_mode: 'async_audit',
      config_version: 7,
      status: 'failed',
      attempts: 3,
      max_attempts: 3,
      claim_version: 2,
      next_attempt_at: '2026-07-24T00:00:00Z',
      processed_at: '2026-07-24T00:00:03Z',
      last_error_code: 'endpoint_unavailable',
      last_error_message: 'Audit detector is temporarily unavailable.',
      created_at: '2026-07-24T00:00:00Z',
      updated_at: '2026-07-24T00:00:03Z',
    },
    payload_state: payloadState,
    payload_ttl_seconds: payloadState === 'available' ? 600 : 0,
    operations: [],
  }
}

function mountWorkspace() {
  return mount(PromptJobWorkspace, {
    global: {
      stubs: {
        BaseDialog: DialogStub,
        Pagination: PaginationStub,
        Teleport: true,
      },
    },
  })
}

describe('PromptJobWorkspace', () => {
  beforeEach(() => {
    Object.values(mocks).forEach((mock) => mock.mockReset())
    mocks.stepUpRun.mockImplementation(async (operation: () => Promise<unknown>) => operation())
    mocks.transitionJob.mockResolvedValue({ id: 12, status: 'queued' })
  })

  it('shows failure attribution and only enables retry while the bounded payload exists', async () => {
    mocks.listJobs.mockResolvedValue({
      items: [failedJob('available')],
      failure_reasons: [{ error_code: 'endpoint_unavailable', count: 4 }],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
    })
    const wrapper = mountWorkspace()
    await flushPromises()

    expect(wrapper.text()).toContain('endpoint_unavailable')
    expect(wrapper.text()).toContain('alice@example.test')
    const retry = wrapper.findAll('button').find((button) => button.text().includes('admin.promptAudit.jobs.retry'))
    expect(retry).toBeTruthy()
    await retry!.trigger('click')
    expect(wrapper.find('[data-test="job-operation-dialog"]').exists()).toBe(true)
    await wrapper.get('textarea').setValue('endpoint recovered and response contract verified')
    const confirm = wrapper.findAll('[data-test="job-operation-dialog"] button')
      .find((button) => button.text().includes('admin.promptAudit.jobs.retry'))
    await confirm!.trigger('click')
    await flushPromises()

    expect(mocks.stepUpRun).toHaveBeenCalledOnce()
    expect(mocks.transitionJob).toHaveBeenCalledWith(
      12,
      'retry',
      'endpoint recovered and response contract verified',
    )
    expect(mocks.showSuccess).toHaveBeenCalledOnce()
  })

  it('does not offer a false retry after the Redis payload has expired', async () => {
    mocks.listJobs.mockResolvedValue({
      items: [failedJob('expired')],
      failure_reasons: [{ error_code: 'endpoint_unavailable', count: 1 }],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
    })
    const wrapper = mountWorkspace()
    await flushPromises()

    const retryButtons = wrapper.findAll('button')
      .filter((button) => button.text().includes('admin.promptAudit.jobs.retry'))
    expect(retryButtons).toHaveLength(0)
    expect(wrapper.text()).toContain('admin.promptAudit.jobs.payloadStates.expired')
    expect(wrapper.text()).toContain('admin.promptAudit.jobs.quarantine')
    expect(wrapper.text()).toContain('admin.promptAudit.jobs.discard')
  })
})
