import { apiClient } from '../client'

export interface VirtualCurrency {
  id: number
  code: string
  name: string
  symbol: string
  description: string
  scale: number
  status: 'active' | 'disabled'
  metadata: Record<string, unknown>
  created_by?: number
  created_at: string
  updated_at: string
}

export interface VirtualCurrencyGroupPolicy {
  id: number
  currency_id: number
  group_id: number
  enabled: boolean
  can_earn: boolean
  can_spend: boolean
  max_balance_units?: number
  metadata: Record<string, unknown>
  created_at: string
  updated_at: string
}

export interface VirtualCurrencyLedgerEntry {
  id: number
  currency_id: number
  currency_code: string
  currency_name: string
  currency_symbol: string
  currency_scale: number
  user_id: number
  group_id?: number
  delta_units: number
  available_delta_units: number
  reserved_delta_units: number
  available_after_units: number
  reserved_after_units: number
  entry_type: string
  source_type: string
  source_id?: string
  idempotency_key: string
  reason: string
  metadata: Record<string, unknown>
  created_by?: number
  created_at: string
}

export interface VirtualCurrencyCreateRequest {
  code: string
  name: string
  symbol?: string
  description?: string
  scale?: number
  metadata?: Record<string, unknown>
}

export interface VirtualCurrencyUpdateRequest {
  name?: string
  symbol?: string
  description?: string
  metadata?: Record<string, unknown>
}

export interface VirtualCurrencyPolicyRequest {
  enabled: boolean
  can_earn: boolean
  can_spend: boolean
  max_balance_units?: number | null
  metadata?: Record<string, unknown>
}

export interface VirtualCurrencyAdjustmentRequest {
  user_id: number
  group_id?: number
  amount_units: number
  entry_type?: string
  source_id?: string
  reason?: string
  metadata?: Record<string, unknown>
}

export interface PaginatedVirtualCurrencyLedger {
  items: VirtualCurrencyLedgerEntry[]
  total: number
  page: number
  page_size: number
  pages: number
}

export interface VirtualCurrencyReconciliationMismatch {
  user_id: number
  wallet_available_units: number
  wallet_reserved_units: number
  ledger_available_units: number
  ledger_reserved_units: number
  wallet_exists: boolean
  ledger_snapshot_found: boolean
}

export interface VirtualCurrencyReconciliationReport {
  currency_id: number
  wallet_count: number
  ledger_user_count: number
  mismatch_count: number
  sample_limit: number
  mismatches: VirtualCurrencyReconciliationMismatch[]
  accounting: {
    journal_count: number
    posting_count: number
    invalid_journal_count: number
    wallet_available_units: string
    wallet_reserved_units: string
    posted_user_available_units: string
    posted_user_reserved_units: string
    gross_issued_units: string
    net_sink_units: string
    net_adjustment_units: string
    projection_delta_units: string
    conservation_delta_units: string
  }
  checked_at: string
}

export interface VirtualCurrencyEnableForAllUsersResult {
  currency_id: number
  group_count: number
  policies: VirtualCurrencyGroupPolicy[]
}

function newIdempotencyKey(prefix: string): string {
  const requestID = globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random().toString(36).slice(2)}`
  return `${prefix}-${requestID}`
}

export async function list(includeDisabled = true): Promise<VirtualCurrency[]> {
  const { data } = await apiClient.get<VirtualCurrency[]>('/admin/currencies', {
    params: { include_disabled: includeDisabled }
  })
  return data
}

export async function create(request: VirtualCurrencyCreateRequest): Promise<VirtualCurrency> {
  const { data } = await apiClient.post<VirtualCurrency>('/admin/currencies', request)
  return data
}

export async function update(id: number, request: VirtualCurrencyUpdateRequest): Promise<VirtualCurrency> {
  const { data } = await apiClient.patch<VirtualCurrency>(`/admin/currencies/${id}`, request)
  return data
}

export async function setStatus(id: number, status: 'active' | 'disabled'): Promise<VirtualCurrency> {
  const { data } = await apiClient.post<VirtualCurrency>(`/admin/currencies/${id}/status`, { status })
  return data
}

export async function listGroups(id: number): Promise<VirtualCurrencyGroupPolicy[]> {
  const { data } = await apiClient.get<VirtualCurrencyGroupPolicy[]>(`/admin/currencies/${id}/groups`)
  return data
}

export async function upsertGroup(id: number, groupID: number, request: VirtualCurrencyPolicyRequest): Promise<VirtualCurrencyGroupPolicy> {
  const { data } = await apiClient.put<VirtualCurrencyGroupPolicy>(
    `/admin/currencies/${id}/groups/${groupID}`,
    request
  )
  return data
}

export async function deleteGroup(id: number, groupID: number): Promise<{ message: string }> {
  const { data } = await apiClient.delete<{ message: string }>(`/admin/currencies/${id}/groups/${groupID}`)
  return data
}

export async function enableForAllUsers(id: number): Promise<VirtualCurrencyEnableForAllUsersResult> {
  const { data } = await apiClient.post<VirtualCurrencyEnableForAllUsersResult>(
    `/admin/currencies/${id}/enable-for-all-users`
  )
  return data
}

export async function adjust(code: string, request: VirtualCurrencyAdjustmentRequest): Promise<VirtualCurrencyLedgerEntry> {
  const { data } = await apiClient.post<VirtualCurrencyLedgerEntry>(
    `/admin/currencies/${encodeURIComponent(code)}/adjustments`,
    request,
    { headers: { 'Idempotency-Key': newIdempotencyKey(`currency-adjust-${code}`) } }
  )
  return data
}

export async function userLedger(
  code: string,
  userID: number,
  page = 1,
  pageSize = 20
): Promise<PaginatedVirtualCurrencyLedger> {
  const { data } = await apiClient.get<PaginatedVirtualCurrencyLedger>(
    `/admin/currencies/${encodeURIComponent(code)}/users/${userID}/ledger`,
    { params: { page, page_size: pageSize } }
  )
  return data
}

export async function expireHolds(currencyID: number, limit = 100): Promise<{ currency_id: number; expired: number; limit: number }> {
  const { data } = await apiClient.post<{ currency_id: number; expired: number; limit: number }>(
    `/admin/currencies/${currencyID}/holds/expire`,
    undefined,
    { params: { limit } }
  )
  return data
}

export async function reconcile(currencyID: number, sampleLimit = 20): Promise<VirtualCurrencyReconciliationReport> {
  const { data } = await apiClient.get<VirtualCurrencyReconciliationReport>(
    `/admin/currencies/${currencyID}/reconciliation`,
    { params: { limit: sampleLimit } }
  )
  return data
}

const virtualCurrenciesAPI = {
  list,
  create,
  update,
  setStatus,
  listGroups,
  upsertGroup,
  deleteGroup,
  enableForAllUsers,
  adjust,
  userLedger,
  expireHolds,
  reconcile
}

export default virtualCurrenciesAPI
