import { apiClient } from '@/api/client'
import type {
  PromptAuditConfig,
  PromptAuditEvent,
  PromptAuditGroup,
  PromptAuditRuntime,
  PromptAuditJobFilters,
  PromptAuditJobPage,
  PromptAuditJobRecord,
  PromptAuditUpdateRequest,
  PromptDeletePreview,
  PromptDeleteResult,
  PromptEventFilters,
  PromptEventPage,
  PromptProbeResult,
  PromptAuditEndpointDraft,
  PromptAuditEvidenceReveal,
  SecurityActionPage,
  SecurityAuditCase,
  SecurityAuditException,
  SecurityAuditNotification,
  SecurityAuditOverview,
  SecurityBehaviorSignalPage,
  SecurityCasePage,
  SecurityEndpointHealth,
  SecurityEnforcementAction,
  SecurityPolicyConfig,
  SecurityPolicyReplayResult,
  SecurityPolicyShadowEvaluationSummary,
  SecurityPolicySummary,
  SecurityPolicyTransition,
  SecurityPolicyVersion,
  SecurityUnifiedDecision,
  SecurityDecisionPage,
} from './types'
import { eventFilterPayload, eventQueryParams } from './viewModel'

const basePath = '/admin/prompt-audit'
const securityBasePath = '/admin/security-audit'

export async function getConfig(): Promise<PromptAuditConfig> {
  const { data } = await apiClient.get<PromptAuditConfig>(`${basePath}/config`)
  return data
}

export async function updateConfig(payload: PromptAuditUpdateRequest): Promise<PromptAuditConfig> {
  const { data } = await apiClient.put<PromptAuditConfig>(`${basePath}/config`, payload)
  return data
}

export async function probeEndpoint(endpoint: PromptAuditEndpointDraft): Promise<PromptProbeResult> {
  const { data } = await apiClient.post<PromptProbeResult>(`${basePath}/endpoints/probe`, {
    endpoint: {
      id: endpoint.id,
      name: endpoint.name,
      protocol: 'openai_compatible',
      adapter: endpoint.adapter,
      base_url: endpoint.base_url,
      network_scope: endpoint.network_scope,
      model: endpoint.model,
      token: endpoint.token || undefined,
      timeout_ms: endpoint.timeout_ms,
      input_limit: endpoint.input_limit,
      enabled: endpoint.enabled,
    },
  })
  return data
}

export async function getRuntime(): Promise<PromptAuditRuntime> {
  const { data } = await apiClient.get<PromptAuditRuntime>(`${basePath}/runtime`)
  return data
}

export async function listJobs(
  filters: PromptAuditJobFilters,
  page: number,
  pageSize: number,
): Promise<PromptAuditJobPage> {
  const params: Record<string, string | number> = { page, page_size: pageSize }
  for (const [key, value] of Object.entries(filters)) {
    if (value.trim()) params[key] = value.trim()
  }
  const { data } = await apiClient.get<PromptAuditJobPage>(`${basePath}/jobs`, { params })
  return data
}

export async function transitionJob(
  id: number,
  operation: 'retry' | 'quarantine' | 'discard',
  reason: string,
): Promise<PromptAuditJobRecord> {
  const { data } = await apiClient.post<PromptAuditJobRecord>(
    `${basePath}/jobs/${id}/${operation}`,
    { reason },
  )
  return data
}

export async function listEvents(
  filters: PromptEventFilters,
  page: number,
  pageSize: number,
): Promise<PromptEventPage> {
  const { data } = await apiClient.get<PromptEventPage>(`${basePath}/events`, {
    params: { page, page_size: pageSize, ...eventQueryParams(filters) },
  })
  return data
}

export async function getEvent(id: number): Promise<PromptAuditEvent> {
  const { data } = await apiClient.get<PromptAuditEvent>(`${basePath}/events/${id}`)
  return data
}

export async function revealEventEvidence(id: number, reason: string): Promise<PromptAuditEvidenceReveal> {
  const { data } = await apiClient.post<PromptAuditEvidenceReveal>(
    `${basePath}/events/${id}/evidence/reveal`,
    { reason },
  )
  return data
}

export async function deleteEvent(id: number): Promise<PromptDeleteResult> {
  const { data } = await apiClient.delete<PromptDeleteResult>(`${basePath}/events/${id}`)
  return data
}

export async function batchDeleteEvents(ids: number[]): Promise<PromptDeleteResult> {
  const { data } = await apiClient.post<PromptDeleteResult>(`${basePath}/events/batch-delete`, { ids })
  return data
}

export async function previewDelete(filters: PromptEventFilters): Promise<PromptDeletePreview> {
  const { data } = await apiClient.post<PromptDeletePreview>(
    `${basePath}/events/delete-preview`,
    eventFilterPayload(filters),
  )
  return data
}

export async function deleteEventsByFilter(
  filters: PromptEventFilters,
  preview: PromptDeletePreview,
): Promise<PromptDeleteResult> {
  const { data } = await apiClient.post<PromptDeleteResult>(`${basePath}/events/delete-by-filter`, {
    filter: eventFilterPayload(filters),
    snapshot_max_id: preview.snapshot_max_id,
    filter_hash: preview.filter_hash,
    confirmation_token: preview.confirmation_token,
    confirm: true,
  })
  return data
}

export async function listGroups(): Promise<PromptAuditGroup[]> {
  const { data } = await apiClient.get<PromptAuditGroup[]>('/admin/groups/all', {
    params: { include_inactive: true },
  })
  return data
}

export async function getSecurityOverview(windowHours = 24): Promise<SecurityAuditOverview> {
  const { data } = await apiClient.get<SecurityAuditOverview>(`${securityBasePath}/overview`, {
    params: { window_hours: windowHours },
  })
  return data
}

export async function listSecurityPolicies(): Promise<SecurityPolicySummary[]> {
  const { data } = await apiClient.get<SecurityPolicySummary[]>(`${securityBasePath}/policies`)
  return data
}

export async function createSecurityPolicy(payload: {
  policy_key: string
  config: SecurityPolicyConfig
  change_reason: string
}): Promise<SecurityPolicyVersion> {
  const { data } = await apiClient.post<SecurityPolicyVersion>(`${securityBasePath}/policies`, payload)
  return data
}

export async function listSecurityPolicyVersions(policyKey: string): Promise<SecurityPolicyVersion[]> {
  const { data } = await apiClient.get<SecurityPolicyVersion[]>(
    `${securityBasePath}/policies/${encodeURIComponent(policyKey)}/versions`,
  )
  return data
}

export async function listSecurityPolicyTransitions(
  policyKey: string,
  limit = 100,
): Promise<SecurityPolicyTransition[]> {
  const { data } = await apiClient.get<SecurityPolicyTransition[]>(
    `${securityBasePath}/policies/${encodeURIComponent(policyKey)}/transitions`,
    { params: { limit } },
  )
  return data
}

export async function getSecurityPolicyShadowEvaluations(
  policyKey: string,
  version: number,
  params: { window_hours?: number; limit?: number } = {},
): Promise<SecurityPolicyShadowEvaluationSummary> {
  const { data } = await apiClient.get<SecurityPolicyShadowEvaluationSummary>(
    `${securityBasePath}/policies/${encodeURIComponent(policyKey)}/versions/${version}/shadow-evaluations`,
    { params: { window_hours: params.window_hours ?? 168, limit: params.limit ?? 50 } },
  )
  return data
}

export async function transitionSecurityPolicy(
  policyKey: string,
  version: number,
  transition: 'validate' | 'shadow' | 'activate' | 'rollback',
  reason = '',
): Promise<SecurityPolicyVersion> {
  const { data } = await apiClient.post<SecurityPolicyVersion>(
    `${securityBasePath}/policies/${encodeURIComponent(policyKey)}/versions/${version}/${transition}`,
    { reason },
  )
  return data
}

export async function simulateSecurityPolicy(
  policyKey: string,
  version: number,
  payload: Record<string, unknown>,
): Promise<Record<string, unknown>> {
  const { data } = await apiClient.post<Record<string, unknown>>(
    `${securityBasePath}/policies/${encodeURIComponent(policyKey)}/versions/${version}/simulate`,
    payload,
  )
  return data
}

export async function replaySecurityPolicy(
  policyKey: string,
  version: number,
  payload: { window_hours: number; limit: number } = { window_hours: 168, limit: 1000 },
): Promise<SecurityPolicyReplayResult> {
  const { data } = await apiClient.post<SecurityPolicyReplayResult>(
    `${securityBasePath}/policies/${encodeURIComponent(policyKey)}/versions/${version}/replay`,
    payload,
  )
  return data
}

export async function listSecurityDecisions(
  params: Record<string, unknown>,
): Promise<SecurityDecisionPage> {
  const { data } = await apiClient.get<SecurityDecisionPage>(`${securityBasePath}/decisions`, { params })
  return data
}

export async function getSecurityDecision(id: number): Promise<SecurityUnifiedDecision> {
  const { data } = await apiClient.get<SecurityUnifiedDecision>(`${securityBasePath}/decisions/${id}`)
  return data
}

export async function revealSecurityEvidence(id: number, reason: string): Promise<PromptAuditEvidenceReveal> {
  const { data } = await apiClient.post<PromptAuditEvidenceReveal>(
    `${securityBasePath}/decisions/${id}/evidence/reveal`,
    { reason },
  )
  return data
}

export async function openSecurityDecisionCase(id: number, reason: string): Promise<SecurityAuditCase> {
  const { data } = await apiClient.post<SecurityAuditCase>(
    `${securityBasePath}/decisions/${id}/open-case`,
    { reason },
  )
  return data
}

export async function addSecurityDecisionFeedback(
  id: number,
  payload: Record<string, unknown>,
): Promise<Record<string, unknown>> {
  const { data } = await apiClient.post<Record<string, unknown>>(
    `${securityBasePath}/decisions/${id}/feedback`,
    payload,
  )
  return data
}

export async function listSecurityActions(params: Record<string, unknown>): Promise<SecurityActionPage> {
  const { data } = await apiClient.get<SecurityActionPage>(`${securityBasePath}/actions`, { params })
  return data
}

export async function transitionSecurityAction(
  id: number,
  transition: 'retry' | 'cancel' | 'revert',
): Promise<SecurityEnforcementAction> {
  const { data } = await apiClient.post<SecurityEnforcementAction>(
    `${securityBasePath}/actions/${id}/${transition}`,
  )
  return data
}

export async function listSecurityCases(params: Record<string, unknown>): Promise<SecurityCasePage> {
  const { data } = await apiClient.get<SecurityCasePage>(`${securityBasePath}/cases`, { params })
  return data
}

export async function getSecurityCase(id: number): Promise<SecurityAuditCase> {
  const { data } = await apiClient.get<SecurityAuditCase>(`${securityBasePath}/cases/${id}`)
  return data
}

export async function transitionSecurityCase(
  id: number,
  payload: Record<string, unknown>,
): Promise<SecurityAuditCase> {
  const { data } = await apiClient.post<SecurityAuditCase>(
    `${securityBasePath}/cases/${id}/transition`,
    payload,
  )
  return data
}

export async function listSecurityExceptions(includeInactive = false): Promise<SecurityAuditException[]> {
  const { data } = await apiClient.get<SecurityAuditException[]>(`${securityBasePath}/exceptions`, {
    params: { include_inactive: includeInactive },
  })
  return data
}

export async function createSecurityException(
  payload: Record<string, unknown>,
): Promise<SecurityAuditException> {
  const { data } = await apiClient.post<SecurityAuditException>(`${securityBasePath}/exceptions`, payload)
  return data
}

export async function expireSecurityException(id: number, reason: string): Promise<SecurityAuditException> {
  const { data } = await apiClient.post<SecurityAuditException>(
    `${securityBasePath}/exceptions/${id}/expire`,
    { reason },
  )
  return data
}

export async function listSecurityEndpointHealth(): Promise<SecurityEndpointHealth[]> {
  const { data } = await apiClient.get<SecurityEndpointHealth[]>(`${securityBasePath}/endpoints`)
  return data
}

export async function resetSecurityEndpointBreaker(endpointID: string): Promise<SecurityEndpointHealth> {
  const { data } = await apiClient.post<SecurityEndpointHealth>(
    `${securityBasePath}/endpoints/${encodeURIComponent(endpointID)}/reset-breaker`,
  )
  return data
}

export async function listSecurityBehaviorSignals(
  params: Record<string, unknown>,
): Promise<SecurityBehaviorSignalPage> {
  const { data } = await apiClient.get<SecurityBehaviorSignalPage>(`${securityBasePath}/signals`, { params })
  return data
}

export async function listSecurityNotifications(
  params: Record<string, unknown> = {},
): Promise<SecurityAuditNotification[]> {
  const { data } = await apiClient.get<SecurityAuditNotification[]>(
    `${securityBasePath}/notifications`,
    { params },
  )
  return data
}

export async function updateSecurityNotificationStatus(
  id: number,
  status: 'unread' | 'read' | 'dismissed',
): Promise<SecurityAuditNotification> {
  const { data } = await apiClient.post<SecurityAuditNotification>(
    `${securityBasePath}/notifications/${id}/status`,
    { status },
  )
  return data
}

export async function markAllSecurityNotificationsRead(
  audience = 'admin',
): Promise<{ updated_count: number }> {
  const { data } = await apiClient.post<{ updated_count: number }>(
    `${securityBasePath}/notifications/read-all`,
    { audience },
  )
  return data
}

export const promptAuditAPI = {
  getConfig,
  updateConfig,
  probeEndpoint,
  getRuntime,
  listJobs,
  transitionJob,
  listEvents,
  getEvent,
  revealEventEvidence,
  deleteEvent,
  batchDeleteEvents,
  previewDelete,
  deleteEventsByFilter,
  listGroups,
  getSecurityOverview,
  listSecurityPolicies,
  createSecurityPolicy,
  listSecurityPolicyVersions,
  listSecurityPolicyTransitions,
  getSecurityPolicyShadowEvaluations,
  transitionSecurityPolicy,
  simulateSecurityPolicy,
  replaySecurityPolicy,
  listSecurityDecisions,
  getSecurityDecision,
  revealSecurityEvidence,
  openSecurityDecisionCase,
  addSecurityDecisionFeedback,
  listSecurityActions,
  transitionSecurityAction,
  listSecurityCases,
  getSecurityCase,
  transitionSecurityCase,
  listSecurityExceptions,
  createSecurityException,
  expireSecurityException,
  listSecurityEndpointHealth,
  resetSecurityEndpointBreaker,
  listSecurityBehaviorSignals,
  listSecurityNotifications,
  updateSecurityNotificationStatus,
  markAllSecurityNotificationsRead,
}

export default promptAuditAPI
