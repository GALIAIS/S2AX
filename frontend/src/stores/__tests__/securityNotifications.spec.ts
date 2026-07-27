import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

const api = vi.hoisted(() => ({
  list: vi.fn(),
  updateStatus: vi.fn(),
  markAllRead: vi.fn()
}))
vi.mock('@/api/securityNotifications', () => ({ default: api }))

import { useSecurityNotificationStore } from '../securityNotifications'

const unreadNotification = {
  id: 7,
  notification_id: 'ntf_7',
  severity: 'high',
  title: '请求安全提醒',
  body: '已脱敏的安全提醒',
  status: 'unread' as const,
  read_at: null,
  created_at: '2026-07-24T03:00:00Z'
}

describe('security notification store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    api.list.mockReset()
    api.updateStatus.mockReset()
    api.markAllRead.mockReset()
  })

  it('deduplicates concurrent refreshes and excludes dismissed entries', async () => {
    api.list.mockResolvedValue([
      unreadNotification,
      { ...unreadNotification, id: 8, notification_id: 'ntf_8', status: 'dismissed' }
    ])
    const store = useSecurityNotificationStore()

    await Promise.all([store.fetchNotifications(true), store.fetchNotifications(true)])

    expect(api.list).toHaveBeenCalledTimes(1)
    expect(store.notifications).toHaveLength(1)
    expect(store.unreadCount).toBe(1)
  })

  it('rolls back an optimistic status change when the server rejects it', async () => {
    api.list.mockResolvedValue([unreadNotification])
    api.updateStatus.mockRejectedValue(new Error('request failed'))
    const store = useSecurityNotificationStore()
    await store.fetchNotifications(true)

    await expect(store.updateStatus(7, 'read')).rejects.toThrow('request failed')

    expect(store.notifications[0].status).toBe('unread')
    expect(store.notifications[0].read_at).toBeNull()
  })
})
