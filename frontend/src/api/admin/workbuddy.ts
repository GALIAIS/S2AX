import { apiClient } from '../client'

export type WorkBuddyPlatform = 'workbuddy' | 'workbuddy_ai'
export type WorkBuddyCredentials = Record<string, unknown>

export function isWorkBuddyPlatform(platform: string | null | undefined): platform is WorkBuddyPlatform {
  return platform === 'workbuddy' || platform === 'workbuddy_ai'
}

export interface WorkBuddyLogin {
  session_id?: string
  auth_url?: string
  expires_at: string
  status: 'pending' | 'ready'
  credentials?: WorkBuddyCredentials
}

export interface WorkBuddyUsage {
  unit: 'credits'
  plan: string
  total: number
  used: number
  remaining: number
  resets_at?: string
  expires_at?: string
  expiring_credits?: number
  expiry_warning?: string
}

export async function startWorkBuddyLogin(platform: WorkBuddyPlatform, proxyId: number | null, signal: AbortSignal): Promise<WorkBuddyLogin> {
  const { data } = await apiClient.post<WorkBuddyLogin>('/admin/workbuddy/auth', { platform, proxy_id: proxyId }, { signal, timeout: 35_000 })
  return data
}

export async function pollWorkBuddyLogin(session: string, signal: AbortSignal): Promise<WorkBuddyLogin> {
  const { data } = await apiClient.get<WorkBuddyLogin>(`/admin/workbuddy/auth/${encodeURIComponent(session)}`, { signal, timeout: 35_000 })
  return data
}

export async function importWorkBuddyCredentials(raw: unknown, signal: AbortSignal): Promise<WorkBuddyCredentials> {
  const { data } = await apiClient.post<{ credentials: WorkBuddyCredentials }>('/admin/workbuddy/import', { data: raw }, { signal })
  return data.credentials
}

export async function fetchWorkBuddyUsage(accountId: number, signal: AbortSignal): Promise<WorkBuddyUsage> {
  const { data } = await apiClient.get<WorkBuddyUsage>(`/admin/accounts/${accountId}/workbuddy-usage`, { signal, timeout: 65_000 })
  return data
}
