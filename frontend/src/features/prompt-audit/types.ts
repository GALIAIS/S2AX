export type PromptAuditMode = 'off' | 'async_audit' | 'blocking'
export type PromptDecision = 'pass' | 'flag' | 'critical' | 'degraded'
export type PromptRiskLevel = 'low' | 'medium' | 'high' | 'critical'
export type PromptAuditNetworkScope = 'public_https' | 'trusted_network' | 'loopback'
export type PromptAuditFailureMode = 'allow_and_record' | 'block_and_record' | 'fallback_local' | 'degraded_observe'
export type PromptAuditDetectorAdapter = 'qwen3guard_chat' | 'openai_moderations' | 'strict_json_chat'

export interface PromptAuditEndpoint {
  id: string
  name: string
  protocol: 'openai_compatible'
  adapter: PromptAuditDetectorAdapter
  base_url: string
  network_scope: PromptAuditNetworkScope
  model: string
  timeout_ms: number
  input_limit: number
  enabled: boolean
  has_token: boolean
  token_status: 'configured' | 'missing' | 'invalid' | string
}

export interface PromptAuditEndpointDraft extends PromptAuditEndpoint {
  token: string
  clear_token: boolean
}

export interface PromptAuditConfig {
  enabled: boolean
  blocking_enabled: boolean
  blocking_latest_turn_only: boolean
  store_pass_events: boolean
  failure_mode: PromptAuditFailureMode
  effective_mode: PromptAuditMode
  strategy: 'priority'
  worker_count: number
  queue_capacity: number
  scanners: string[]
  all_groups: boolean
  group_ids: number[]
  endpoints: PromptAuditEndpoint[]
  config_version: number
  updated_at: string
  updated_by: number
  change_summary: string
}

export interface PromptAuditDraft extends Omit<PromptAuditConfig, 'endpoints'> {
  endpoints: PromptAuditEndpointDraft[]
}

export interface PromptAuditUpdateRequest {
  expected_config_version: number
  enabled: boolean
  blocking_enabled: boolean
  blocking_latest_turn_only: boolean
  store_pass_events: boolean
  failure_mode: PromptAuditFailureMode
  strategy: 'priority'
  worker_count: number
  queue_capacity: number
  scanners: string[]
  all_groups: boolean
  group_ids: number[]
  endpoints: Array<{
    id: string
    name: string
    protocol: 'openai_compatible'
    adapter: PromptAuditDetectorAdapter
    base_url: string
    network_scope: PromptAuditNetworkScope
    model: string
    token?: string
    clear_token: boolean
    timeout_ms: number
    input_limit: number
    enabled: boolean
  }>
}

export interface PromptProbeResult {
  ok: boolean
  status: string
  error_code?: string
  message: string
  latency_ms: number
  http_status: number
  retryable: boolean
  checked_at: string
  token_applied: boolean
}

export interface PromptQueueStats {
  staging: number
  queued: number
  processing: number
  retry: number
  done: number
  failed: number
  quarantined: number
  discarded: number
  active: number
}

export interface PromptGuardMetrics {
  total: number
  allowed: number
  flagged: number
  blocked: number
  unavailable: number
  invalid: number
  timeouts: number
  failovers: number
  bulkhead_full: number
  record_failed: number
  latency_avg_ms?: number
  latency_p50_ms?: number
  latency_p95_ms?: number
  latency_p99_ms?: number
  latency_max_ms?: number
}

export interface PromptAuditRuntime {
  process_status: 'disabled' | 'running' | 'degraded' | 'error' | string
  effective_mode: PromptAuditMode
  expected_config_version: number
  active_config_version: number
  config_loaded_at?: string
  config_load_error?: string
  worker_total: number
  worker_active: number
  worker_heartbeat_at?: string
  queue_capacity: number
  queue: PromptQueueStats
  processed_total: number
  failed_total: number
  enqueued_total: number
  dropped_total: number
  last_processed_at?: string
  last_error_code?: string
  last_error_message?: string
  database_status: string
  redis_status: string
  endpoints: Record<string, PromptProbeResult>
  guard_metrics: PromptGuardMetrics
}

export type PromptAuditJobStatus =
  | 'staging'
  | 'queued'
  | 'processing'
  | 'retry'
  | 'done'
  | 'failed'
  | 'quarantined'
  | 'discarded'

export interface PromptAuditJobRecord {
  id: number
  snapshot: PromptSnapshot
  execution_mode: PromptAuditMode
  config_version: number
  status: PromptAuditJobStatus
  attempts: number
  max_attempts: number
  claim_version: number
  next_attempt_at: string
  processing_started_at?: string
  processed_at?: string
  last_error_code?: string
  last_error_message?: string
  created_at: string
  updated_at: string
}

export interface PromptAuditJobOperation {
  id: number
  job_id: number
  operation: 'retry' | 'quarantine' | 'discard'
  from_status: PromptAuditJobStatus
  to_status: PromptAuditJobStatus
  actor_id: number
  reason: string
  payload_available: boolean
  created_at: string
}

export interface PromptAuditAdminJob {
  job: PromptAuditJobRecord
  payload_state: 'available' | 'expired' | 'unknown' | 'not_applicable'
  payload_ttl_seconds: number
  operations?: PromptAuditJobOperation[]
}

export interface PromptAuditJobFailureReason {
  error_code: string
  count: number
}

export interface PromptAuditJobPage {
  items: PromptAuditAdminJob[]
  failure_reasons: PromptAuditJobFailureReason[]
  total: number
  page: number
  page_size: number
  pages: number
}

export interface PromptAuditJobFilters {
  status: string
  error_code: string
  keyword: string
  start_at: string
  end_at: string
}

export interface PromptSnapshot {
  request_id: string
  user_id: number
  username: string
  user_email: string
  api_key_id: number
  api_key_name: string
  group_id?: number
  group_name: string
  provider: string
  endpoint: string
  protocol: string
  model: string
  prompt_hash: string
  redacted_preview: string
  prompt_length: number
  message_count: number
  stage: string
}

export interface PromptIssueSummary {
  category: string
  scanner_id: string
  title: string
  description: string
  severity: string
  severity_label: string
  action: string
  action_label: string
  code: string
  score: number
  evidence: string
  evidence_hash: string
  start_rune?: number
  end_rune?: number
}

export interface PromptAuditEvent {
  id: number
  job_id: number
  snapshot: PromptSnapshot
  decision: PromptDecision
  risk_level: PromptRiskLevel
  action: 'Allow' | 'Warn' | 'Block' | string
  categories: string[]
  matched_scanners: string[]
  scanner_scores: Record<string, number>
  scanner_evidence: Record<string, string>
  scanner_backend: string
  scanner_version: string
  guard_endpoint_id: string
  detector_adapter: PromptAuditDetectorAdapter
  provider_request_id?: string
  finish_reason?: string
  model_digest?: string
  policy_id: string
  policy_version: number
  config_version: number
  chunk_total: number
  latency_ms: number
  issue_summaries: PromptIssueSummary[]
  evidence_available: boolean
  evidence_status: 'not_stored' | 'encrypted' | 'expired' | 'encryption_failed' | 'legacy_plaintext' | string
  evidence_expires_at?: string
  evaluation_status: 'complete' | 'degraded' | 'failed' | string
  failure_mode?: PromptAuditFailureMode
  failure_reason?: string
  created_at: string
}

export interface PromptAuditEvidenceReveal {
  event_id: number
  full_prompt: string
  revealed_at: string
}

export interface PromptEventFilters {
  decision: string
  risk_level: string
  endpoint: string
  group_id: string
  user_id: string
  api_key_id: string
  request_id: string
  prompt_hash: string
  keyword: string
  start_at: string
  end_at: string
}

export interface PromptEventPage {
  items: PromptAuditEvent[]
  total: number
  page: number
  page_size: number
  pages: number
}

export interface PromptDeleteResult {
  deleted_events: number
  deleted_jobs: number
}

export interface PromptDeletePreview {
  matched_count: number
  filter_summary: Record<string, unknown>
  snapshot_max_id: number
  filter_hash: string
  confirmation_token: string
  expires_at: string
}

export interface PromptAuditGroup {
  id: number
  name: string
  status: 'active' | 'inactive'
  platform: string
}

export interface PromptLoadErrors {
  config: string
  runtime: string
  groups: string
  events: string
}

export type SecurityPolicyStatus = 'draft' | 'validated' | 'shadow' | 'active' | 'retired'
export type SecurityActionStatus = 'pending' | 'processing' | 'retry' | 'succeeded' | 'failed' | 'cancelled' | 'reverted'
export type SecurityCaseStatus = 'open' | 'reviewing' | 'confirmed' | 'false_positive' | 'dismissed' | 'expired'

export interface SecurityPolicyScope {
  all_groups: boolean
  group_ids: number[]
  user_ids: number[]
  api_key_ids: number[]
  protocols: string[]
  endpoints: string[]
  models: string[]
}

export interface SecurityBehaviorSignalRule {
  id: string
  enabled: boolean
  metric: string
  subject_type: 'user' | 'api_key' | 'group'
  window_minutes: number
  threshold: number
  minimum_samples: number
  severity: PromptRiskLevel
}

export interface SecurityPolicyConfig {
  name: string
  priority: number
  scope: SecurityPolicyScope
  mode: PromptAuditMode
  detectors: Array<{ id: string; enabled: boolean; timeout_ms: number }>
  failure: {
    local_error: PromptAuditFailureMode
    remote_timeout: PromptAuditFailureMode
    remote_invalid: PromptAuditFailureMode
  }
  actions: Record<PromptRiskLevel, string[]>
  evidence: { mode: string; retention_days: number }
  signals: { enabled: boolean; rules: SecurityBehaviorSignalRule[] }
}

export interface SecurityPolicyVersion {
  id: number
  policy_key: string
  version: number
  name: string
  status: SecurityPolicyStatus
  priority: number
  config: SecurityPolicyConfig
  config_digest: string
  validation_errors: string[]
  change_reason: string
  created_by?: number
  created_at: string
  validated_at?: string
  shadowed_at?: string
  activated_at?: string
  retired_at?: string
}

export interface SecurityPolicyTransition {
  id: number
  policy_version_id: number
  policy_key: string
  version: number
  from_status: SecurityPolicyStatus
  to_status: SecurityPolicyStatus
  actor_id?: number
  reason: string
  created_at: string
}

export interface SecurityPolicySummary {
  policy_key: string
  name: string
  latest_version: number
  active_version?: number
  shadow_version?: number
  status: SecurityPolicyStatus
  priority: number
  version_count: number
  updated_at: string
}

export interface SecurityPolicyReplayExample {
  decision_pk: number
  decision_id: string
  source_type: string
  risk_level: string
  previous_action: string
  proposed_action: string
  candidate_changed: boolean
  created_at: string
}

export interface SecurityPolicyReplayResult {
  policy_key: string
  policy_version: number
  config_digest: string
  window_hours: number
  requested_limit: number
  analyzed: number
  matched: number
  unmatched: number
  action_changes: number
  stricter_changes: number
  looser_changes: number
  candidate_action_changes: number
  by_source: Record<string, number>
  by_proposed_action: Record<string, number>
  examples: SecurityPolicyReplayExample[]
  generated_at: string
}

export interface SecurityPolicyShadowEvaluation {
  id: number
  decision_pk: number
  decision_id: string
  source_type: string
  policy_version_id: number
  policy_key: string
  policy_version: number
  risk_level: string
  baseline_request_action: string
  proposed_request_action: string
  baseline_actions: string[]
  proposed_actions: string[]
  request_action_changed: boolean
  actions_changed: boolean
  created_at: string
  decision_created_at: string
  detector_summary: SecurityDetectorEvidence[]
}

export interface SecurityPolicyShadowEvaluationSummary {
  policy_key: string
  policy_version: number
  window_hours: number
  total: number
  request_action_changes: number
  candidate_action_changes: number
  stricter_changes: number
  looser_changes: number
  unchanged: number
  last_evaluated_decision_pk: number
  last_evaluated_at?: string
  last_error: string
  items: SecurityPolicyShadowEvaluation[]
}

export interface SecurityAuditOverview {
  window_hours: number
  total_decisions: number
  allowed: number
  warned: number
  blocked: number
  degraded: number
  open_cases: number
  pending_actions: number
  failed_actions: number
  active_policies: number
  active_exceptions: number
  behavior_matches: number
  unread_notifications: number
  detector_p95_ms: number
  oldest_pending_action_seconds: number
  false_positive_count: number
  false_negative_count: number
  evidence_reveal_count: number
  signal_lag_seconds: number
  signal_last_aggregated_at?: string
  signal_last_error: string
  by_risk: Record<string, number>
  by_source: Record<string, number>
  generated_at: string
}

export interface SecurityDetectorEvidence {
  id: number
  detector_id: string
  detector_version: string
  outcome: string
  category: string
  score: number
  severity: string
  safe_summary: string
  evidence_digest: string
  latency_ms: number
  error_code: string
  expires_at?: string
  hold_until?: string
  created_at: string
}

export interface SecurityEnforcementAction {
  id: number
  action_id: string
  decision_pk: number
  action_type: string
  subject_type: string
  subject_id: number
  status: SecurityActionStatus
  attempts: number
  max_attempts: number
  error_code: string
  error_message: string
  created_at: string
  processed_at?: string
  reverted_at?: string
  updated_at: string
}

export interface SecurityUnifiedDecision {
  id: number
  decision_id: string
  audit_id: string
  source_type: string
  source_event_id?: number
  request_id: string
  stage: string
  user_id?: number
  user_snapshot: string
  api_key_id?: number
  api_key_snapshot: string
  group_id?: number
  group_snapshot: string
  provider: string
  endpoint: string
  protocol: string
  requested_model: string
  policy_key: string
  policy_version: number
  evaluation_status: string
  risk_level: string
  request_action: string
  failure_mode?: PromptAuditFailureMode
  failure_reason?: string
  redacted_preview: string
  detector_summary: SecurityDetectorEvidence[]
  candidate_actions: string[]
  evidence?: SecurityDetectorEvidence[]
  actions?: SecurityEnforcementAction[]
  created_at: string
}

export interface SecurityDecisionPage {
  items: SecurityUnifiedDecision[]
  total: number
  page: number
  page_size: number
  pages: number
}

export interface SecurityCaseEvent {
  id: number
  event_type: string
  actor_id?: number
  summary: string
  details: Record<string, unknown>
  created_at: string
}

export interface SecurityAuditCase {
  id: number
  case_id: string
  primary_decision_pk?: number
  title: string
  severity: PromptRiskLevel
  status: SecurityCaseStatus
  assignee_id?: number
  opened_reason: string
  resolution: string
  resolution_note: string
  labels: string[]
  created_at: string
  updated_at: string
  resolved_at?: string
  timeline?: SecurityCaseEvent[]
}

export interface SecurityCasePage {
  items: SecurityAuditCase[]
  total: number
  page: number
  page_size: number
  pages: number
}

export interface SecurityActionPage {
  items: SecurityEnforcementAction[]
  total: number
  page: number
  page_size: number
  pages: number
}

export interface SecurityAuditException {
  id: number
  exception_id: string
  name: string
  scope_type: string
  scope_id: string
  detector_id: string
  category: string
  effect: string
  reason: string
  status: string
  starts_at: string
  expires_at?: string
  permanent: boolean
  revoked_by?: number
  revoked_reason: string
  created_at: string
}

export interface SecurityEndpointHealth {
  endpoint_id: string
  network_scope: string
  status: string
  breaker_state: string
  consecutive_failures: number
  request_count: number
  success_count: number
  timeout_count: number
  rate_limited_count: number
  server_error_count: number
  invalid_response_count: number
  latency_sum_ms: number
  latency_max_ms: number
  latency_ms: number
  http_status: number
  error_code: string
  checked_at?: string
  breaker_opened_at?: string
  updated_at: string
}

export interface SecurityBehaviorSignalWindow {
  id: number
  bucket_start: string
  bucket_seconds: number
  subject_type: string
  subject_id: number
  user_id?: number
  api_key_id?: number
  group_id?: number
  subject_snapshot: string
  request_count: number
  success_count: number
  error_count: number
  business_limited_count: number
  token_count: number
  actual_cost: number
  duration_sum_ms: number
  duration_sample_count: number
  duration_max_ms: number
  distinct_ip_count: number
  distinct_model_count: number
  matched_rules: number
  highest_severity: string
  computed_at: string
}

export interface SecurityBehaviorSignalPage {
  items: SecurityBehaviorSignalWindow[]
  total: number
  page: number
  page_size: number
  pages: number
}

export interface SecurityAuditNotification {
  id: number
  notification_id: string
  action_id: number
  decision_pk: number
  audience: 'admin' | 'user'
  recipient_user_id?: number
  severity: PromptRiskLevel
  title: string
  body: string
  status: 'unread' | 'read' | 'dismissed'
  read_at?: string
  created_at: string
}
