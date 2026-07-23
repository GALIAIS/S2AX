import { apiClient } from '../client'

export type CityRealtimeAgentDecisionQueueStatus = 'active' | 'queued' | 'leased' | 'terminal' | 'all'
export type CityRealtimeAgentDecisionRequestStatus = 'queued' | 'leased' | 'accepted' | 'rejected' | 'stale' | 'failed_terminal' | 'cancelled'
export type CityRealtimeAgentDecisionOutboxStatus = 'queued' | 'leased' | 'succeeded' | 'failed' | 'cancelled'
export type CityRealtimeAgentDeadLetterStatus = 'quarantined' | 'released'
export type CityRealtimeAgentDeadLetterReason =
  | 'operator_review'
  | 'provider_configuration'
  | 'provider_incident'
  | 'budget_review'
  | 'world_maintenance'
export type CityRealtimeAgentDeadLetterEventType = CityRealtimeAgentDeadLetterStatus

export interface CityRealtimeAgentDecisionWorkerHealth {
  queued_requests: number
  leased_requests: number
  retry_scheduled: number
  quarantined_requests: number
  stale_quarantined_requests: number
  quarantine_stale_after_seconds: number
  oldest_quarantined_at?: string
  next_retry_not_before?: string
  last_failure_code?: string
  open_circuit_breakers: number
}

export interface CityRealtimeOperationalWorldHealth {
  world_id: number
  world_name: string
  world_status: string
  temporal_engine_version: string
  lifecycle_status: string
  clock_state: string
  recovery_state: string
  agent_decision_worker: CityRealtimeAgentDecisionWorkerHealth
}

export interface CityRealtimeOperationalHealth {
  worlds: CityRealtimeOperationalWorldHealth[]
  nodes: Array<{
    node_id: string
    source_clock_mode: string
    health_state: string
    observed_at: string
  }>
}

export interface CityRealtimeAgentDecisionQueueItem {
  world_id: number
  request_code: string
  agent_definition_code: string
  request_status: CityRealtimeAgentDecisionRequestStatus
  outbox_status: CityRealtimeAgentDecisionOutboxStatus
  attempt_count: number
  retry_not_before?: string
  model_profile_code?: string
  model_profile_version?: number
  last_attempt_status?: 'started' | 'succeeded' | 'failed'
  last_error_code?: string
  dead_letter_status?: CityRealtimeAgentDeadLetterStatus
  dead_letter_reason_code?: CityRealtimeAgentDeadLetterReason
  dead_letter_quarantined_at?: string
  created_at: string
  updated_at: string
}

export interface CityRealtimeAgentDecisionQueuePage {
  items: CityRealtimeAgentDecisionQueueItem[]
  next_cursor?: string
}

export interface CityRealtimeAgentDecisionDeadLetterResult {
  world_id: number
  request_code: string
  dead_letter_status: CityRealtimeAgentDeadLetterStatus
  reason_code: CityRealtimeAgentDeadLetterReason | 'operator_release'
}

export interface CityRealtimeAgentDecisionRetryResult {
  world_id: number
  request_code: string
  request_status: CityRealtimeAgentDecisionRequestStatus
  previous_retry_not_before?: string
}

export interface CityRealtimeAgentDecisionDeadLetterEvent {
  event_id: number
  event_type: CityRealtimeAgentDeadLetterEventType
  reason_code: CityRealtimeAgentDeadLetterReason | 'operator_release'
  actor_user_id: number
  created_at: string
}

export interface CityRealtimeAgentDecisionDeadLetterEventPage {
  items: CityRealtimeAgentDecisionDeadLetterEvent[]
  next_before_event_id?: number
}

function idempotencyKey(prefix: string): string {
  const requestID = globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random().toString(36).slice(2)}`
  return `${prefix}-${requestID}`
}

function decisionPath(worldID: number, requestCode: string): string {
  return `/admin/city/worlds/${worldID}/agent-decision-queue/${encodeURIComponent(requestCode)}`
}

export async function getCityRealtimeOperationalHealth(limit = 100): Promise<CityRealtimeOperationalHealth> {
  const { data } = await apiClient.get<CityRealtimeOperationalHealth>('/admin/city/clock-health', { params: { limit } })
  return data
}

export async function listCityRealtimeAgentDecisionQueue(input: {
  worldID: number
  status?: CityRealtimeAgentDecisionQueueStatus
  beforeCursor?: string
  limit?: number
}): Promise<CityRealtimeAgentDecisionQueuePage> {
  const { data } = await apiClient.get<CityRealtimeAgentDecisionQueuePage>('/admin/city/agent-decision-queue', {
    params: {
      world_id: input.worldID,
      status: input.status ?? 'active',
      before_cursor: input.beforeCursor,
      limit: input.limit ?? 50
    }
  })
  return data
}

export async function quarantineCityRealtimeAgentDecision(
  worldID: number,
  requestCode: string,
  reasonCode: CityRealtimeAgentDeadLetterReason
): Promise<CityRealtimeAgentDecisionDeadLetterResult> {
  const { data } = await apiClient.post<CityRealtimeAgentDecisionDeadLetterResult>(
    `${decisionPath(worldID, requestCode)}/dead-letter`,
    { reason_code: reasonCode },
    { headers: { 'Idempotency-Key': idempotencyKey('city-agent-decision-quarantine') } }
  )
  return data
}

export async function releaseCityRealtimeAgentDecisionDeadLetter(
  worldID: number,
  requestCode: string
): Promise<CityRealtimeAgentDecisionDeadLetterResult> {
  const { data } = await apiClient.post<CityRealtimeAgentDecisionDeadLetterResult>(
    `${decisionPath(worldID, requestCode)}/dead-letter/release`,
    {},
    { headers: { 'Idempotency-Key': idempotencyKey('city-agent-decision-release') } }
  )
  return data
}

export async function retryCityRealtimeAgentDecision(
  worldID: number,
  requestCode: string
): Promise<CityRealtimeAgentDecisionRetryResult> {
  const { data } = await apiClient.post<CityRealtimeAgentDecisionRetryResult>(
    `${decisionPath(worldID, requestCode)}/retry`,
    {},
    { headers: { 'Idempotency-Key': idempotencyKey('city-agent-decision-retry') } }
  )
  return data
}

export async function listCityRealtimeAgentDecisionDeadLetterEvents(input: {
  worldID: number
  requestCode: string
  beforeEventID?: number
  limit?: number
}): Promise<CityRealtimeAgentDecisionDeadLetterEventPage> {
  const { data } = await apiClient.get<CityRealtimeAgentDecisionDeadLetterEventPage>(
    `${decisionPath(input.worldID, input.requestCode)}/dead-letter/events`,
    {
      params: {
        before_event_id: input.beforeEventID,
        limit: input.limit ?? 50
      }
    }
  )
  return data
}

export default {
  getCityRealtimeOperationalHealth,
  listCityRealtimeAgentDecisionQueue,
  quarantineCityRealtimeAgentDecision,
  releaseCityRealtimeAgentDecisionDeadLetter,
  retryCityRealtimeAgentDecision,
  listCityRealtimeAgentDecisionDeadLetterEvents
}
