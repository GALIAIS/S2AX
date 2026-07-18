/**
 * User Announcements API endpoints
 */

import { apiClient } from './client'
import type { UserAnnouncement } from '@/types'

export async function list(unreadOnly: boolean = false): Promise<UserAnnouncement[]> {
  const { data } = await apiClient.get<UserAnnouncement[]>('/announcements', {
    params: unreadOnly ? { unread_only: 1 } : {}
  })
  return data
}

export async function markRead(id: number): Promise<{ message: string }> {
  const { data } = await apiClient.post<{ message: string }>(`/announcements/${id}/read`)
  return data
}

export async function markAllRead(): Promise<{ message: string; count: number }> {
  const { data } = await apiClient.post<{ message: string; count: number }>('/announcements/read-all')
  return data
}

const announcementsAPI = {
  list,
  markRead,
  markAllRead
}

export default announcementsAPI
