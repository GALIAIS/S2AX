import { beforeEach, describe, expect, it, vi } from 'vitest'

const client = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: client }))

import securityNotificationsAPI from '../securityNotifications'

describe('securityNotificationsAPI', () => {
  beforeEach(() => {
    client.get.mockReset()
    client.post.mockReset()
  })

  it('uses recipient-scoped user routes', async () => {
    client.get.mockResolvedValue({ data: [] })
    client.post.mockResolvedValue({ data: { updated_count: 2 } })

    await securityNotificationsAPI.list('unread', 25)
    expect(client.get).toHaveBeenCalledWith('/security-audit/notifications', {
      params: { status: 'unread', limit: 25 }
    })

    await securityNotificationsAPI.markAllRead()
    expect(client.post).toHaveBeenCalledWith('/security-audit/notifications/read-all')
  })

  it('sends only the requested notification status', async () => {
    client.post.mockResolvedValue({ data: { id: 7, status: 'dismissed' } })
    await securityNotificationsAPI.updateStatus(7, 'dismissed')
    expect(client.post).toHaveBeenCalledWith(
      '/security-audit/notifications/7/status',
      { status: 'dismissed' }
    )
  })
})
