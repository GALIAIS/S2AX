import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import securityNotificationsAPI from '@/api/securityNotifications'
import type {
  SecurityNotificationStatus,
  UserSecurityAuditNotification
} from '@/types'

const THROTTLE_MS = 60 * 1000

export const useSecurityNotificationStore = defineStore('security-notifications', () => {
  const notifications = ref<UserSecurityAuditNotification[]>([])
  const loading = ref(false)
  const lastFetchTime = ref(0)
  let activeRequest: Promise<void> | null = null

  const unreadCount = computed(() =>
    notifications.value.filter((notification) => notification.status === 'unread').length
  )

  async function fetchNotifications(force = false): Promise<void> {
    const now = Date.now()
    if (!force && lastFetchTime.value > 0 && now - lastFetchTime.value < THROTTLE_MS) return
    if (activeRequest) return activeRequest

    loading.value = true
    activeRequest = securityNotificationsAPI.list(undefined, 100)
      .then((items) => {
        notifications.value = items.filter((item) => item.status !== 'dismissed')
        lastFetchTime.value = Date.now()
      })
      .catch((error) => {
        lastFetchTime.value = 0
        throw error
      })
      .finally(() => {
        loading.value = false
        activeRequest = null
      })
    return activeRequest
  }

  async function updateStatus(id: number, status: SecurityNotificationStatus): Promise<void> {
    const index = notifications.value.findIndex((item) => item.id === id)
    if (index < 0) return
    const previous = { ...notifications.value[index] }
    if (status === 'dismissed') {
      notifications.value.splice(index, 1)
    } else {
      notifications.value[index] = {
        ...notifications.value[index],
        status,
        read_at: status === 'read' ? new Date().toISOString() : null
      }
    }
    try {
      const updated = await securityNotificationsAPI.updateStatus(id, status)
      const currentIndex = notifications.value.findIndex((item) => item.id === id)
      if (currentIndex >= 0) notifications.value[currentIndex] = updated
    } catch (error) {
      const currentIndex = notifications.value.findIndex((item) => item.id === id)
      if (currentIndex >= 0) notifications.value[currentIndex] = previous
      else notifications.value.splice(Math.min(index, notifications.value.length), 0, previous)
      throw error
    }
  }

  async function markAllRead(): Promise<void> {
    const unread = notifications.value.filter((item) => item.status === 'unread')
    if (unread.length === 0) return
    const readAt = new Date().toISOString()
    unread.forEach((item) => {
      item.status = 'read'
      item.read_at = readAt
    })
    try {
      await securityNotificationsAPI.markAllRead()
    } catch (error) {
      unread.forEach((item) => {
        item.status = 'unread'
        item.read_at = null
      })
      throw error
    }
  }

  function reset(): void {
    notifications.value = []
    lastFetchTime.value = 0
    activeRequest = null
    loading.value = false
  }

  return {
    notifications,
    loading,
    unreadCount,
    fetchNotifications,
    updateStatus,
    markAllRead,
    reset
  }
})
