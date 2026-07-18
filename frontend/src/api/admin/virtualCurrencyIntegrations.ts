import { apiClient } from '../client'

export type VirtualCurrencyIntegrationStatus = 'active' | 'disabled'

export interface VirtualCurrencyIntegration {
  id: number
  code: string
  name: string
  secret_hint: string
  status: VirtualCurrencyIntegrationStatus
  metadata: Record<string, unknown>
  created_by?: number
  created_at: string
  updated_at: string
}

export interface VirtualCurrencyIntegrationSecretResult {
  integration: VirtualCurrencyIntegration
  secret: string
}

export interface VirtualCurrencyIntegrationScope {
  id: number
  integration_id: number
  currency_id: number
  group_id: number
  enabled: boolean
  can_earn: boolean
  can_spend: boolean
  can_settle: boolean
  metadata: Record<string, unknown>
  created_at: string
  updated_at: string
}

export interface VirtualCurrencyIntegrationCreateRequest {
  code: string
  name: string
  metadata?: Record<string, unknown>
}

export interface VirtualCurrencyIntegrationUpdateRequest {
  name?: string
  metadata?: Record<string, unknown>
}

export interface VirtualCurrencyIntegrationScopeRequest {
  enabled: boolean
  can_earn: boolean
  can_spend: boolean
  can_settle: boolean
  metadata?: Record<string, unknown>
}

function newIdempotencyKey(prefix: string): string {
  const requestID = globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random().toString(36).slice(2)}`
  return `${prefix}-${requestID}`
}

export async function list(includeDisabled = true): Promise<VirtualCurrencyIntegration[]> {
  const { data } = await apiClient.get<VirtualCurrencyIntegration[]>('/admin/currency-integrations', {
    params: { include_disabled: includeDisabled }
  })
  return data
}

export async function get(id: number): Promise<VirtualCurrencyIntegration> {
  const { data } = await apiClient.get<VirtualCurrencyIntegration>(`/admin/currency-integrations/${id}`)
  return data
}

export async function create(request: VirtualCurrencyIntegrationCreateRequest): Promise<VirtualCurrencyIntegrationSecretResult> {
  const { data } = await apiClient.post<VirtualCurrencyIntegrationSecretResult>('/admin/currency-integrations', request)
  return data
}

export async function update(id: number, request: VirtualCurrencyIntegrationUpdateRequest): Promise<VirtualCurrencyIntegration> {
  const { data } = await apiClient.patch<VirtualCurrencyIntegration>(`/admin/currency-integrations/${id}`, request)
  return data
}

export async function setStatus(id: number, status: VirtualCurrencyIntegrationStatus): Promise<VirtualCurrencyIntegration> {
  const { data } = await apiClient.post<VirtualCurrencyIntegration>(`/admin/currency-integrations/${id}/status`, { status })
  return data
}

export async function rotateSecret(id: number): Promise<VirtualCurrencyIntegrationSecretResult> {
  const { data } = await apiClient.post<VirtualCurrencyIntegrationSecretResult>(
    `/admin/currency-integrations/${id}/rotate-secret`,
    undefined,
    { headers: { 'Idempotency-Key': newIdempotencyKey(`currency-integration-rotate-${id}`) } }
  )
  return data
}

export async function listScopes(id: number): Promise<VirtualCurrencyIntegrationScope[]> {
  const { data } = await apiClient.get<VirtualCurrencyIntegrationScope[]>(`/admin/currency-integrations/${id}/scopes`)
  return data
}

export async function upsertScope(
  integrationID: number,
  currencyID: number,
  groupID: number,
  request: VirtualCurrencyIntegrationScopeRequest
): Promise<VirtualCurrencyIntegrationScope> {
  const { data } = await apiClient.put<VirtualCurrencyIntegrationScope>(
    `/admin/currency-integrations/${integrationID}/scopes/${currencyID}/${groupID}`,
    request
  )
  return data
}

export async function deleteScope(integrationID: number, currencyID: number, groupID: number): Promise<{ message: string }> {
  const { data } = await apiClient.delete<{ message: string }>(
    `/admin/currency-integrations/${integrationID}/scopes/${currencyID}/${groupID}`
  )
  return data
}

const virtualCurrencyIntegrationsAPI = {
  list,
  get,
  create,
  update,
  setStatus,
  rotateSecret,
  listScopes,
  upsertScope,
  deleteScope
}

export default virtualCurrencyIntegrationsAPI
