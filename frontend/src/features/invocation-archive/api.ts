import { apiClient } from '@/api/client'
import { defaultInvocationArchiveCompression } from './types'
import type {
  InvocationArchiveAccessLog,
  InvocationArchiveConfig,
  InvocationArchiveCleanupResult,
  InvocationArchiveCleanupStrategy,
  InvocationArchivePayloadChunk,
  InvocationArchiveFilters,
  InvocationArchiveRecord,
  InvocationArchiveRecordPage,
  InvocationArchiveReveal,
  InvocationArchiveRuntime,
  InvocationArchiveScope,
  InvocationArchiveSubject,
  InvocationArchiveUpdateRequest,
} from './types'

const basePath = '/admin/invocation-archive'

function normalizeConfig(data: InvocationArchiveConfig): InvocationArchiveConfig {
  return {
    ...data,
    compression: { ...defaultInvocationArchiveCompression(), ...(data.compression || {}) },
    rules: Array.isArray(data.rules) ? data.rules : [],
  }
}

export async function getConfig(): Promise<InvocationArchiveConfig> {
  const { data } = await apiClient.get<InvocationArchiveConfig>(`${basePath}/config`)
  return normalizeConfig(data)
}

export async function updateConfig(payload: InvocationArchiveUpdateRequest): Promise<InvocationArchiveConfig> {
  const { data } = await apiClient.put<InvocationArchiveConfig>(`${basePath}/config`, payload)
  return normalizeConfig(data)
}

export async function getRuntime(): Promise<InvocationArchiveRuntime> {
  const { data } = await apiClient.get<InvocationArchiveRuntime>(`${basePath}/runtime`)
  return data
}

export async function cleanup(strategy: InvocationArchiveCleanupStrategy): Promise<InvocationArchiveCleanupResult> {
  const { data } = await apiClient.post<InvocationArchiveCleanupResult>(`${basePath}/cleanup`, {
    strategy,
    confirm: strategy === 'all',
  })
  return data
}

export async function listSubjects(
  scope: InvocationArchiveScope,
  query = '',
  limit = 20,
): Promise<InvocationArchiveSubject[]> {
  const { data } = await apiClient.get<{ items: InvocationArchiveSubject[] }>(`${basePath}/subjects`, {
    params: { scope, q: query, limit },
  })
  return Array.isArray(data.items) ? data.items : []
}

function toRFC3339(value: string): string | undefined {
  if (!value) return undefined
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? undefined : date.toISOString()
}

export async function listRecords(
  filters: InvocationArchiveFilters,
  page: number,
  pageSize: number,
): Promise<InvocationArchiveRecordPage> {
  const params: Record<string, string | number> = { page, page_size: pageSize }
  if (filters.q.trim()) params.q = filters.q.trim()
  if (filters.mode) params.mode = filters.mode
  if (filters.outcome) params.outcome = filters.outcome
  for (const field of ['user_id', 'group_id', 'api_key_id'] as const) {
    if (filters[field].trim()) params[field] = filters[field].trim()
  }
  const from = toRFC3339(filters.from)
  const to = toRFC3339(filters.to)
  if (from) params.from = from
  if (to) params.to = to
  const { data } = await apiClient.get<InvocationArchiveRecordPage>(`${basePath}/records`, { params })
  return { ...data, items: Array.isArray(data.items) ? data.items : [] }
}

export async function getRecord(id: number): Promise<InvocationArchiveRecord> {
  const { data } = await apiClient.get<InvocationArchiveRecord>(`${basePath}/records/${id}`)
  return data
}

export async function listAccessLogs(id: number): Promise<InvocationArchiveAccessLog[]> {
  const { data } = await apiClient.get<{ items: InvocationArchiveAccessLog[] }>(`${basePath}/records/${id}/accesses`)
  return Array.isArray(data.items) ? data.items : []
}

export async function revealRecord(id: number): Promise<InvocationArchiveReveal> {
  const { data } = await apiClient.post<InvocationArchiveReveal>(`${basePath}/records/${id}/reveal`)
  return data
}

export async function revealPayloadChunk(
  id: number,
  slot: 'request' | 'response',
  offset = 0,
  limit = 256 * 1024,
): Promise<InvocationArchivePayloadChunk> {
  const { data } = await apiClient.post<InvocationArchivePayloadChunk>(`${basePath}/records/${id}/payloads/${slot}`, undefined, {
    params: { offset, limit },
  })
  return data
}

export async function deleteRecord(id: number): Promise<{ deleted: number }> {
  const { data } = await apiClient.delete<{ deleted: number }>(`${basePath}/records/${id}`)
  return data
}

export async function batchDeleteRecords(ids: number[]): Promise<{ deleted: number }> {
  const { data } = await apiClient.post<{ deleted: number }>(`${basePath}/records/batch-delete`, { ids })
  return data
}

export const invocationArchiveAPI = {
  getConfig,
  updateConfig,
  getRuntime,
  cleanup,
  listSubjects,
  listRecords,
  getRecord,
  listAccessLogs,
  revealRecord,
  revealPayloadChunk,
  deleteRecord,
  batchDeleteRecords,
}

export default invocationArchiveAPI
