import { apiClient } from './client'

export type AccountAllocationUserStatus = 'ready' | 'cooling' | 'unavailable'

// This projection intentionally mirrors the backend's safe user DTO. Never
// add an upstream account ID, name, credential, proxy, IP, model list, or
// global usage field here.
export interface UserAccountAllocation {
  assignment_id: number
  policy_id: number
  group_id: number
  group_name: string
  platform: string
  account_type: string
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

interface UserAccountAllocationResponse {
  assignments: UserAccountAllocation[]
}

export async function listMine(): Promise<UserAccountAllocation[]> {
  const { data } = await apiClient.get<UserAccountAllocationResponse>('/account-allocations')
  return data.assignments ?? []
}

const accountAllocationsAPI = { listMine }

export default accountAllocationsAPI
