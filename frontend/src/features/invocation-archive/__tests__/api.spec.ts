import { beforeEach, describe, expect, it, vi } from 'vitest'
import { emptyInvocationArchiveFilters } from '../types'

const client = vi.hoisted(() => ({ get: vi.fn(), put: vi.fn(), post: vi.fn(), delete: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: client }))

import invocationArchiveAPI from '../api'

describe('Invocation Archive API', () => {
  beforeEach(() => Object.values(client).forEach((mock) => mock.mockReset()))

  it('normalizes legacy null collection fields before the archive view consumes them', async () => {
    client.get.mockResolvedValue({
      data: {
        config_version: 1,
        default_mode: 'off',
        retention_days: 7,
        max_request_bytes: 1048576,
        max_response_bytes: 4194304,
        direct_view_enabled: false,
        rules: null,
        updated_at: '',
        updated_by: 0,
      },
    })

    await expect(invocationArchiveAPI.getConfig()).resolves.toMatchObject({
      rules: [],
      compression: expect.objectContaining({ enabled: true, batch_size: 25, interval_minutes: 60 }),
    })
  })

  it('keeps metadata listing separate from the step-up protected direct reveal endpoint', async () => {
    client.get.mockResolvedValue({ data: { items: [], page: 1, page_size: 20, total: 0 } })
    const filters = emptyInvocationArchiveFilters()
    filters.q = 'request-42'
    filters.from = '2026-07-27T08:00'
    await invocationArchiveAPI.listRecords(filters, 1, 20)
    expect(client.get).toHaveBeenCalledWith('/admin/invocation-archive/records', {
      params: expect.objectContaining({ page: 1, page_size: 20, q: 'request-42', from: expect.any(String) }),
    })

    client.post.mockResolvedValue({ data: { record_id: 42 } })
    await invocationArchiveAPI.revealRecord(42)
    expect(client.post).toHaveBeenCalledWith('/admin/invocation-archive/records/42/reveal')

    client.post.mockResolvedValue({ data: { record_id: 42, slot: 'response', payload: {}, next_offset: 262144 } })
    await invocationArchiveAPI.revealPayloadChunk(42, 'response', 262144)
    expect(client.post).toHaveBeenCalledWith('/admin/invocation-archive/records/42/payloads/response', undefined, {
      params: { offset: 262144, limit: 262144 },
    })
  })

  it('uses the scoped selector and destructive record endpoints', async () => {
    client.get.mockResolvedValue({ data: { items: [{ id: 7, label: 'operator@example.test' }] } })
    await invocationArchiveAPI.listSubjects('user', 'operator', 10)
    expect(client.get).toHaveBeenCalledWith('/admin/invocation-archive/subjects', {
      params: { scope: 'user', q: 'operator', limit: 10 },
    })

    client.delete.mockResolvedValue({ data: { deleted: 1 } })
    await invocationArchiveAPI.deleteRecord(7)
    expect(client.delete).toHaveBeenCalledWith('/admin/invocation-archive/records/7')

    client.post.mockResolvedValue({ data: { deleted: 2 } })
    await invocationArchiveAPI.batchDeleteRecords([7, 8])
    expect(client.post).toHaveBeenCalledWith('/admin/invocation-archive/records/batch-delete', { ids: [7, 8] })
  })
})
