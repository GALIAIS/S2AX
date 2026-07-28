import { apiClient } from './client'
import type { AccountPlatform, AccountType, WindowStats } from '@/types'

export type AccountAllocationUserStatus = 'ready' | 'cooling' | 'unavailable'
export type AccountAllocationVisibleSource = 'public' | 'dedicated'
export type AccountAllocationVisibleUsageScope = 'rolling_24h' | 'personal_lease'
export type AccountAllocationUsageDetailAccess = 'assignment' | 'group' | 'direct'

export interface UserVisibleAccountQuotaWindow {
  utilization: number
  resets_at?: string | null
  window_stats?: WindowStats | null
}

// This cached, read-only projection requires an active dedicated lease or an
// explicit administrator-managed visibility grant. The user page never probes
// the upstream provider.
export interface UserVisibleAccountUpstreamQuota {
  updated_at?: string | null
  five_hour?: UserVisibleAccountQuotaWindow | null
  seven_day?: UserVisibleAccountQuotaWindow | null
}

// This projection intentionally mirrors the backend's safe user DTO. Never
// add an upstream account ID, name, credential, proxy, IP, model list, or
// global usage field here.
export interface UserAccountAllocation {
  assignment_id: number
  policy_id: number
  group_id: number
  group_name: string
  platform: AccountPlatform
  account_type: AccountType
  capacity: {
    concurrency: number
  }
  status: AccountAllocationUserStatus
  rate_limit_reset_at?: string | null
  usage: {
    request_count: number
    total_tokens: number
  }
  assigned_at: string
}

// This is a separate, intentionally safe directory projection. account_name
// is already masked on the server when it is an email address. Cached upstream
// quota is included only after the backend proves a lease or visibility grant;
// never use this API to request or infer an account ID, credentials, proxy/IP
// data, models, health errors, or a per-user usage breakdown.
export interface UserVisibleAccount {
  view_key: string
  source: AccountAllocationVisibleSource
  usage_detail_access?: AccountAllocationUsageDetailAccess
  group_id: number
  group_name: string
  subscription_type: string
  account_name: string
  account_name_masked: boolean
  platform: AccountPlatform
  account_type: AccountType
  capacity: {
    concurrency: number
  }
  status: AccountAllocationUserStatus
  rate_limit_reset_at?: string | null
  last_activity_at?: string | null
  usage: {
    scope: AccountAllocationVisibleUsageScope
    request_count: number
    total_tokens: number
    account_cost?: number
    user_cost?: number
  }
  upstream_quota?: UserVisibleAccountUpstreamQuota | null
  assigned_at?: string | null
}

export interface UserVisibleAccountSummary {
  public_group_count: number
  dedicated_group_count: number
  public_account_count: number
  dedicated_account_count: number
  ready_account_count: number
}

export interface UserVisibleAccountOverview {
  items: UserVisibleAccount[]
  summary: UserVisibleAccountSummary
}

interface UserAccountAllocationResponse {
	assignments: UserAccountAllocation[]
}

interface UserVisibleAccountResponse {
	items?: UserVisibleAccount[]
	summary?: UserVisibleAccountSummary
}

const emptyVisibleSummary = (): UserVisibleAccountSummary => ({
	public_group_count: 0,
	dedicated_group_count: 0,
	public_account_count: 0,
	dedicated_account_count: 0,
	ready_account_count: 0,
})

export async function listMine(): Promise<UserAccountAllocation[]> {
	const { data } = await apiClient.get<UserAccountAllocationResponse>('/account-allocations')
	return data.assignments ?? []
}

export async function listVisible(): Promise<UserVisibleAccountOverview> {
	const { data } = await apiClient.get<UserVisibleAccountResponse>('/account-allocations/visible')
	return {
		items: data.items ?? [],
		summary: data.summary ?? emptyVisibleSummary(),
	}
}

const accountAllocationsAPI = { listMine, listVisible }

export default accountAllocationsAPI
