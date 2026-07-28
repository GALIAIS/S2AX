import { apiClient } from '../client'
import type { PaginatedResponse } from '@/types'

export type AccountAllocationPolicyStatus = 'active' | 'disabled'
export type AccountAllocationAssignmentStatus = 'active' | 'released'
export type AccountUsageVisibilityGrantScope = 'exclusive_group' | 'user_account'
export type AccountAllocationAccessStatus =
  | 'ready'
  | 'user_unavailable'
  | 'group_unavailable'
  | 'group_access_required'
  | 'subscription_required'

export interface AccountAllocationPolicy {
  id: number
  user_id: number
  user_email: string
  username: string
  group_id: number
  group_name: string
  group_platform: string
  desired_count: number
  auto_replenish: boolean
  replace_on_401: boolean
  replace_on_429: boolean
  status: AccountAllocationPolicyStatus
  access_status: AccountAllocationAccessStatus
  created_by?: number | null
  last_reconciled_at?: string | null
  created_at: string
  updated_at: string
  active_assignment_count: number
  shortage: number
}

export interface AccountAllocationAssignment {
  id: number
  policy_id: number
  user_id: number
  group_id: number
  account_id: number
  account_name: string
  platform: string
  account_type: string
  concurrency: number
  account_status: string
  schedulable: boolean
  rate_limit_reset_at?: string | null
  status: AccountAllocationAssignmentStatus
  assigned_by?: number | null
  assigned_at: string
  released_at?: string | null
  release_reason?: string
  last_reconciled_at?: string | null
}

export interface AccountAllocationCandidate {
  account_id: number
  account_name: string
  platform: string
  account_type: string
  concurrency: number
  priority: number
}

export interface AccountAllocationEvent {
  id: number
  policy_id: number
  assignment_id?: number | null
  event_type: string
  actor_user_id?: number | null
  metadata: Record<string, unknown>
  created_at: string
}

export interface AccountAllocationReconcileResult {
  policy_id: number
  desired_count: number
  active_before: number
  active_after: number
  released_count: number
  assigned_count: number
  shortage: number
  skipped_concurrent: boolean
  access_status: AccountAllocationAccessStatus
}

export interface AccountAllocationCapabilities {
  max_desired_count: number
  reconcile_interval_seconds: number
}

export interface AccountAllocationOverview {
  policy_count: number
  active_policy_count: number
  disabled_policy_count: number
  blocked_policy_count: number
  desired_account_count: number
  active_assignment_count: number
  shortage_count: number
  policies_with_shortage: number
  last_policy_reconciled_at?: string | null
  reconcile_interval_seconds: number
}

export interface AccountUsageVisibilityGrant {
  id: number
  scope: AccountUsageVisibilityGrantScope
  group_id: number
  group_name: string
  user_id?: number | null
  user_email?: string
  username?: string
  account_id?: number | null
  account_name?: string
  platform?: string
  account_type?: string
  created_by?: number | null
  created_at: string
}

export interface AccountUsageVisibilityGrantInput {
  scope: AccountUsageVisibilityGrantScope
  group_id: number
  user_id?: number
  account_id?: number
}

export interface AccountAllocationPolicyInput {
  user_id: number
  group_id: number
  desired_count: number
  auto_replenish: boolean
  replace_on_401: boolean
  replace_on_429: boolean
}

export interface AccountAllocationPolicyUpdate {
  desired_count: number
  auto_replenish: boolean
  replace_on_401: boolean
  replace_on_429: boolean
}

export interface AccountAllocationPolicyFilters {
  user_id?: number
  group_id?: number
  status?: AccountAllocationPolicyStatus
}

export async function list(
  page = 1,
  pageSize = 20,
  filters: AccountAllocationPolicyFilters = {},
  options?: { signal?: AbortSignal }
): Promise<PaginatedResponse<AccountAllocationPolicy>> {
  const { data } = await apiClient.get<PaginatedResponse<AccountAllocationPolicy>>('/admin/account-allocations/policies', {
    params: { page, page_size: pageSize, ...filters },
    signal: options?.signal
  })
  return data
}

export async function getCapabilities(): Promise<AccountAllocationCapabilities> {
  const { data } = await apiClient.get<AccountAllocationCapabilities>('/admin/account-allocations/capabilities')
  return data
}

export async function getOverview(): Promise<AccountAllocationOverview> {
  const { data } = await apiClient.get<AccountAllocationOverview>('/admin/account-allocations/overview')
  return data
}

export async function reconcileAll(): Promise<{ items: AccountAllocationReconcileResult[]; processed: number }> {
  const { data } = await apiClient.post<{ items: AccountAllocationReconcileResult[]; processed: number }>(
    '/admin/account-allocations/reconcile'
  )
  return {
    items: data.items ?? [],
    processed: data.processed ?? 0
  }
}

export async function getById(id: number): Promise<AccountAllocationPolicy> {
  const { data } = await apiClient.get<AccountAllocationPolicy>(`/admin/account-allocations/policies/${id}`)
  return data
}

export async function create(input: AccountAllocationPolicyInput): Promise<AccountAllocationPolicy> {
  const { data } = await apiClient.post<AccountAllocationPolicy>('/admin/account-allocations/policies', input)
  return data
}

export async function update(id: number, input: AccountAllocationPolicyUpdate): Promise<AccountAllocationPolicy> {
  const { data } = await apiClient.put<AccountAllocationPolicy>(`/admin/account-allocations/policies/${id}`, input)
  return data
}

export async function remove(id: number): Promise<{ message: string }> {
  const { data } = await apiClient.delete<{ message: string }>(`/admin/account-allocations/policies/${id}`)
  return data
}

export async function setStatus(id: number, enabled: boolean): Promise<AccountAllocationPolicy> {
  const { data } = await apiClient.post<AccountAllocationPolicy>(`/admin/account-allocations/policies/${id}/status`, { enabled })
  return data
}

export async function reconcile(id: number): Promise<AccountAllocationReconcileResult> {
  const { data } = await apiClient.post<AccountAllocationReconcileResult>(`/admin/account-allocations/policies/${id}/reconcile`)
  return data
}

export async function listAssignments(id: number): Promise<AccountAllocationAssignment[]> {
  const { data } = await apiClient.get<{ items: AccountAllocationAssignment[] }>(`/admin/account-allocations/policies/${id}/assignments`)
  return data.items ?? []
}

export async function listCandidates(
  id: number,
  query = '',
  limit = 100,
  options?: { signal?: AbortSignal }
): Promise<AccountAllocationCandidate[]> {
  const { data } = await apiClient.get<{ items: AccountAllocationCandidate[] }>(`/admin/account-allocations/policies/${id}/candidates`, {
    params: { q: query || undefined, limit },
    signal: options?.signal
  })
  return data.items ?? []
}

export async function assign(id: number, accountID: number): Promise<AccountAllocationAssignment> {
  const { data } = await apiClient.post<AccountAllocationAssignment>(`/admin/account-allocations/policies/${id}/assignments`, {
    account_id: accountID
  })
  return data
}

export async function release(id: number, assignmentID: number): Promise<{ message: string }> {
  const { data } = await apiClient.delete<{ message: string }>(
    `/admin/account-allocations/policies/${id}/assignments/${assignmentID}`
  )
  return data
}

export async function listEvents(
  id: number,
  page = 1,
  pageSize = 20
): Promise<PaginatedResponse<AccountAllocationEvent>> {
  const { data } = await apiClient.get<PaginatedResponse<AccountAllocationEvent>>(`/admin/account-allocations/policies/${id}/events`, {
    params: { page, page_size: pageSize }
  })
  return data
}

export async function listUsageVisibilityGrants(): Promise<AccountUsageVisibilityGrant[]> {
  const { data } = await apiClient.get<{ items: AccountUsageVisibilityGrant[] }>(
    '/admin/account-allocations/usage-visibility-grants'
  )
  return data.items ?? []
}

export async function createUsageVisibilityGrant(
  input: AccountUsageVisibilityGrantInput
): Promise<AccountUsageVisibilityGrant> {
  const { data } = await apiClient.post<AccountUsageVisibilityGrant>(
    '/admin/account-allocations/usage-visibility-grants',
    input
  )
  return data
}

export async function removeUsageVisibilityGrant(id: number): Promise<{ message: string }> {
  const { data } = await apiClient.delete<{ message: string }>(
    `/admin/account-allocations/usage-visibility-grants/${id}`
  )
  return data
}

const accountAllocationsAPI = {
  getCapabilities,
  getOverview,
  reconcileAll,
  list,
  getById,
  create,
  update,
  remove,
  setStatus,
  reconcile,
  listAssignments,
  listCandidates,
  assign,
  release,
  listEvents,
  listUsageVisibilityGrants,
  createUsageVisibilityGrant,
  removeUsageVisibilityGrant
}

export default accountAllocationsAPI
