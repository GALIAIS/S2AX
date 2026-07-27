export type InvocationArchiveMode = 'off' | 'request_only' | 'full'
export type InvocationArchiveScope = 'user' | 'group' | 'api_key'
export type InvocationArchiveOutcome = 'completed' | 'client_error' | 'server_error' | 'websocket_error'

export interface InvocationArchiveScopeRule {
  scope: InvocationArchiveScope
  subject_id: number
  mode: InvocationArchiveMode
}

export interface InvocationArchiveConfig {
  config_version: number
  default_mode: InvocationArchiveMode
  retention_days: number
  max_request_bytes: number
  max_response_bytes: number
  direct_view_enabled: boolean
  rules: InvocationArchiveScopeRule[]
  updated_at: string
  updated_by: number
}

export interface InvocationArchiveUpdateRequest {
  expected_config_version: number
  default_mode: InvocationArchiveMode
  retention_days: number
  max_request_bytes: number
  max_response_bytes: number
  direct_view_enabled: boolean
  rules: InvocationArchiveScopeRule[]
}

export interface InvocationArchiveRuntime {
  started: boolean
  config_version: number
  queue_depth: number
  queue_capacity: number
  accepted: number
  dropped: number
  persisted: number
  persist_failures: number
  expired_purged: number
  last_config_error?: string
  last_config_error_at?: string
  last_persist_error?: string
  last_persist_error_at?: string
}

export interface InvocationArchiveSubject {
  id: number
  label: string
  secondary?: string
}

export interface InvocationArchiveRecord {
  id: number
  created_at: string
  completed_at: string
  expires_at: string
  config_version: number
  mode: Exclude<InvocationArchiveMode, 'off'>
  transport: 'http' | 'websocket' | string
  websocket_turn?: number
  user_id?: number
  user_label: string
  api_key_id?: number
  api_key_name: string
  group_id?: number
  group_name: string
  request_id: string
  client_request_id: string
  method: string
  path: string
  model: string
  client_ip: string
  user_agent: string
  request_content_type: string
  response_content_type: string
  http_status: number
  request_total_bytes: number
  request_captured_bytes: number
  request_truncated: boolean
  request_status: string
  response_total_bytes: number
  response_captured_bytes: number
  response_truncated: boolean
  response_status: string
  outcome: InvocationArchiveOutcome
}

export interface InvocationArchiveRecordPage {
  items: InvocationArchiveRecord[]
  page: number
  page_size: number
  total: number
}

export interface InvocationArchiveAccessLog {
  id: number
  record_id: number
  admin_id?: number
  admin_name: string
  outcome: string
  client_ip: string
  user_agent: string
  created_at: string
}

export interface InvocationArchivePayloadView {
  available: boolean
  status: string
  content_type: string
  encoding?: 'utf8' | 'base64' | string
  data?: string
  total_bytes: number
  captured_bytes: number
  truncated: boolean
  frames?: InvocationArchivePayloadFrame[]
  frames_truncated?: boolean
}

export interface InvocationArchivePayloadFrame {
  sequence: number
  kind: 'text' | 'binary' | string
  occurred_at: string
  encoding: 'utf8' | 'base64' | string
  data: string
  total_bytes: number
  captured_bytes: number
  truncated: boolean
}

export interface InvocationArchiveReveal {
  record_id: number
  revealed_at: string
  request: InvocationArchivePayloadView
  response: InvocationArchivePayloadView
}

export interface InvocationArchiveFilters {
  q: string
  mode: '' | Exclude<InvocationArchiveMode, 'off'>
  outcome: '' | InvocationArchiveOutcome
  user_id: string
  group_id: string
  api_key_id: string
  from: string
  to: string
}

export const emptyInvocationArchiveFilters = (): InvocationArchiveFilters => ({
  q: '',
  mode: '',
  outcome: '',
  user_id: '',
  group_id: '',
  api_key_id: '',
  from: '',
  to: '',
})
