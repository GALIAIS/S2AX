import { apiClient } from './client'

export interface VirtualCurrencyWallet {
  currency_id: number
  currency_code: string
  currency_name: string
  currency_symbol: string
  currency_scale: number
  available_units: number
  reserved_units: number
  group_ids: number[]
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

export interface PaginatedVirtualCurrencyLedger {
  items: VirtualCurrencyLedgerEntry[]
  total: number
  page: number
  page_size: number
  pages: number
}

export interface VirtualCurrencySpendRequest {
  group_id: number
  amount_units: number
  source_id?: string
  reason?: string
  metadata?: Record<string, unknown>
}

function newIdempotencyKey(prefix: string): string {
  const requestID = globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random().toString(36).slice(2)}`
  return `${prefix}-${requestID}`
}

export async function listWallets(): Promise<VirtualCurrencyWallet[]> {
  const { data } = await apiClient.get<VirtualCurrencyWallet[]>('/user/currencies')
  return data
}

export async function listLedger(code: string, page = 1, pageSize = 20): Promise<PaginatedVirtualCurrencyLedger> {
  const { data } = await apiClient.get<PaginatedVirtualCurrencyLedger>(
    `/user/currencies/${encodeURIComponent(code)}/ledger`,
    { params: { page, page_size: pageSize } }
  )
  return data
}

export async function spend(code: string, request: VirtualCurrencySpendRequest): Promise<VirtualCurrencyLedgerEntry> {
  const { data } = await apiClient.post<VirtualCurrencyLedgerEntry>(
    `/user/currencies/${encodeURIComponent(code)}/spend`,
    request,
    { headers: { 'Idempotency-Key': newIdempotencyKey(`currency-spend-${code}`) } }
  )
  return data
}

const virtualCurrencyAPI = { listWallets, listLedger, spend }

export default virtualCurrencyAPI
