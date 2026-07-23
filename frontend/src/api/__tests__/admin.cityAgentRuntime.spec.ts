import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post } = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn()
}))

vi.mock('../client', () => ({
  apiClient: { get, post }
}))

import {
  getCityRealtimeOperationalHealth,
  listCityRealtimeAgentDecisionDeadLetterEvents,
  listCityRealtimeAgentDecisionQueue,
  quarantineCityRealtimeAgentDecision,
  releaseCityRealtimeAgentDecisionDeadLetter,
  retryCityRealtimeAgentDecision
} from '@/api/admin/cityAgentRuntime'

describe('admin city agent runtime API', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
  })

  it('reads only bounded health and one-world queue projections', async () => {
    get.mockResolvedValue({ data: { worlds: [], nodes: [] } })

    await getCityRealtimeOperationalHealth()
    await listCityRealtimeAgentDecisionQueue({ worldID: 7, status: 'active', beforeCursor: 'cursor', limit: 20 })

    expect(get).toHaveBeenNthCalledWith(1, '/admin/city/clock-health', { params: { limit: 100 } })
    expect(get).toHaveBeenNthCalledWith(2, '/admin/city/agent-decision-queue', {
      params: { world_id: 7, status: 'active', before_cursor: 'cursor', limit: 20 }
    })
  })

  it('keeps quarantine, release and retry requests scoped to one encoded decision code', async () => {
    post.mockResolvedValue({ data: {} })

    await quarantineCityRealtimeAgentDecision(7, 'adr.queue/a', 'provider_incident')
    await releaseCityRealtimeAgentDecisionDeadLetter(7, 'adr.queue/a')
    await retryCityRealtimeAgentDecision(7, 'adr.queue/a')

    expect(post).toHaveBeenNthCalledWith(
      1,
      '/admin/city/worlds/7/agent-decision-queue/adr.queue%2Fa/dead-letter',
      { reason_code: 'provider_incident' },
      expect.objectContaining({ headers: expect.objectContaining({ 'Idempotency-Key': expect.stringMatching(/^city-agent-decision-quarantine-/) }) })
    )
    expect(post).toHaveBeenNthCalledWith(
      2,
      '/admin/city/worlds/7/agent-decision-queue/adr.queue%2Fa/dead-letter/release',
      {},
      expect.objectContaining({ headers: expect.objectContaining({ 'Idempotency-Key': expect.stringMatching(/^city-agent-decision-release-/) }) })
    )
    expect(post).toHaveBeenNthCalledWith(
      3,
      '/admin/city/worlds/7/agent-decision-queue/adr.queue%2Fa/retry',
      {},
      expect.objectContaining({ headers: expect.objectContaining({ 'Idempotency-Key': expect.stringMatching(/^city-agent-decision-retry-/) }) })
    )
  })

  it('reads redacted dead-letter events with a local keyset cursor', async () => {
    get.mockResolvedValue({ data: { items: [] } })

    await listCityRealtimeAgentDecisionDeadLetterEvents({ worldID: 7, requestCode: 'adr.queue.one', beforeEventID: 44, limit: 10 })

    expect(get).toHaveBeenCalledWith(
      '/admin/city/worlds/7/agent-decision-queue/adr.queue.one/dead-letter/events',
      { params: { before_event_id: 44, limit: 10 } }
    )
  })
})
