import { apiClient } from './client'
import type {
  SecurityNotificationStatus,
  UserSecurityAuditNotification
} from '@/types'

const basePath = '/security-audit/notifications'

export async function list(
  status?: SecurityNotificationStatus,
  limit = 100
): Promise<UserSecurityAuditNotification[]> {
  const { data } = await apiClient.get<UserSecurityAuditNotification[]>(basePath, {
    params: { ...(status ? { status } : {}), limit }
  })
  return data
}

export async function updateStatus(
  id: number,
  status: SecurityNotificationStatus
): Promise<UserSecurityAuditNotification> {
  const { data } = await apiClient.post<UserSecurityAuditNotification>(
    `${basePath}/${id}/status`,
    { status }
  )
  return data
}

export async function markAllRead(): Promise<{ updated_count: number }> {
  const { data } = await apiClient.post<{ updated_count: number }>(`${basePath}/read-all`)
  return data
}

const securityNotificationsAPI = {
  list,
  updateStatus,
  markAllRead
}

export default securityNotificationsAPI
