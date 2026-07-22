import { apiClient } from '../client'

export type CityVisualPackStatus = 'staging' | 'published' | 'retired'
export type CityVisualRenderContract = 'procedural_pixel_v1' | 'atlas_pixel_v1'
export type CityVisualGenerationAssetClass =
  | 'terrain'
  | 'infrastructure'
  | 'building_exterior'
  | 'interior'
  | 'furniture'
  | 'item'
  | 'vehicle'
  | 'character_base'
  | 'character_wear'
  | 'effect'
  | 'marker'

export type CityVisualGenerationJobStatus =
  | 'draft'
  | 'queued'
  | 'generated'
  | 'reviewing'
  | 'approved'
  | 'rejected'
  | 'cancelled'
  | 'failed'

export interface CityVisualProceduralManifest {
  schema_version: number
  render_mode: 'procedural_pixel_v1'
  logical_tile_px: number
  profile_palettes?: Record<string, Record<string, string>>
  semantic_rules?: Record<string, string[]>
  assets: []
}

export interface CityVisualPackSummary {
  pack_id: string
  pack_version: string
  status: CityVisualPackStatus
  semantic_projection_version: string
  render_contract_version: CityVisualRenderContract
  manifest_hash: string
  asset_set_hash: string
  compatibility: {
    spatial_profile_ids?: string[]
    semantic_projection_versions?: string[]
  }
  created_at: string
  published_at?: string
}

export interface CityVisualPackDetail extends CityVisualPackSummary {
  manifest: CityVisualProceduralManifest
  provenance: Record<string, unknown>
}

export interface CityVisualGenerationJob {
  id: number
  pack_id: string
  pack_version: string
  asset_class: CityVisualGenerationAssetClass
  status: CityVisualGenerationJobStatus
  request_spec: {
    schema_version: number
    asset_class: CityVisualGenerationAssetClass
    semantic_tags: string[]
    pixel_width: number
    pixel_height: number
    frame_count: number
    prompt_template_id: string
    render_contract_version: CityVisualRenderContract
  }
  candidate_hash?: string
  review: {
    decision?: 'approved' | 'rejected' | 'cancelled'
    reason_code?: string
  }
  created_by_user_id?: number
  reviewed_by_user_id?: number
  created_at: string
  reviewed_at?: string
}

export interface CityVisualReleasePolicy {
  semantic_projection_version: string
  spatial_profile_id: string
  pack_id: string
  pack_version: string
  created_at: string
  updated_at: string
  created_by_user_id?: number
  updated_by_user_id?: number
}

export interface CityVisualReviewEvent {
  id: number
  pack_id: string
  pack_version: string
  generation_job_id?: number
  event_type:
    | 'staging_created'
    | 'manifest_updated'
    | 'generation_requested'
    | 'generation_reviewed'
    | 'published'
    | 'retired'
    | 'release_policy_assigned'
  actor_user_id?: number
  metadata: Record<string, string>
  created_at: string
}

export interface CityVisualPackCreateRequest {
  pack_id: string
  pack_version: string
  spatial_profile_ids: string[]
  manifest: CityVisualProceduralManifest
}

export interface CityVisualPackUpdateRequest {
  spatial_profile_ids: string[]
  manifest: CityVisualProceduralManifest
}

export interface CityVisualGenerationJobCreateRequest {
  asset_class: CityVisualGenerationAssetClass
  semantic_tags: string[]
  pixel_width: number
  pixel_height: number
  frame_count: number
}

export interface CityVisualGenerationJobReviewRequest {
  decision: 'approved' | 'rejected' | 'cancelled'
  reason_code?: string
}

export interface CityVisualReleasePolicyRequest {
  pack_id: string
  pack_version: string
}

function idempotencyKey(prefix: string): string {
  const requestID = globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random().toString(36).slice(2)}`
  return `${prefix}-${requestID}`
}

function packPath(packID: string, packVersion: string): string {
  return `/admin/city/visual-packs/${encodeURIComponent(packID)}/${encodeURIComponent(packVersion)}`
}

export async function listVisualPacks(limit = 50): Promise<CityVisualPackSummary[]> {
  const { data } = await apiClient.get<CityVisualPackSummary[]>('/admin/city/visual-packs', { params: { limit } })
  return data
}

export async function getVisualPack(packID: string, packVersion: string): Promise<CityVisualPackDetail> {
  const { data } = await apiClient.get<CityVisualPackDetail>(packPath(packID, packVersion))
  return data
}

export async function createVisualPack(request: CityVisualPackCreateRequest): Promise<CityVisualPackDetail> {
  const { data } = await apiClient.post<CityVisualPackDetail>('/admin/city/visual-packs', request, {
    headers: { 'Idempotency-Key': idempotencyKey('city-visual-pack-create') }
  })
  return data
}

export async function updateVisualPack(
  packID: string,
  packVersion: string,
  request: CityVisualPackUpdateRequest
): Promise<CityVisualPackDetail> {
  const { data } = await apiClient.patch<CityVisualPackDetail>(packPath(packID, packVersion), request, {
    headers: { 'Idempotency-Key': idempotencyKey(`city-visual-pack-update-${packID}-${packVersion}`) }
  })
  return data
}

export async function listVisualGenerationJobs(
  packID: string,
  packVersion: string,
  limit = 50
): Promise<CityVisualGenerationJob[]> {
  const { data } = await apiClient.get<CityVisualGenerationJob[]>(`${packPath(packID, packVersion)}/generation-jobs`, {
    params: { limit }
  })
  return data
}

export async function createVisualGenerationJob(
  packID: string,
  packVersion: string,
  request: CityVisualGenerationJobCreateRequest
): Promise<CityVisualGenerationJob> {
  const { data } = await apiClient.post<CityVisualGenerationJob>(`${packPath(packID, packVersion)}/generation-jobs`, request, {
    headers: { 'Idempotency-Key': idempotencyKey(`city-visual-generation-create-${packID}-${packVersion}`) }
  })
  return data
}

export async function reviewVisualGenerationJob(
  packID: string,
  packVersion: string,
  jobID: number,
  request: CityVisualGenerationJobReviewRequest
): Promise<CityVisualGenerationJob> {
  const { data } = await apiClient.patch<CityVisualGenerationJob>(
    `${packPath(packID, packVersion)}/generation-jobs/${jobID}/review`,
    request,
    { headers: { 'Idempotency-Key': idempotencyKey(`city-visual-generation-review-${jobID}`) } }
  )
  return data
}

export async function publishVisualPack(packID: string, packVersion: string): Promise<CityVisualPackDetail> {
  const { data } = await apiClient.post<CityVisualPackDetail>(`${packPath(packID, packVersion)}/publish`, undefined, {
    headers: { 'Idempotency-Key': idempotencyKey(`city-visual-pack-publish-${packID}-${packVersion}`) }
  })
  return data
}

export async function retireVisualPack(packID: string, packVersion: string): Promise<CityVisualPackDetail> {
  const { data } = await apiClient.post<CityVisualPackDetail>(`${packPath(packID, packVersion)}/retire`, undefined, {
    headers: { 'Idempotency-Key': idempotencyKey(`city-visual-pack-retire-${packID}-${packVersion}`) }
  })
  return data
}

export async function listVisualReleasePolicies(): Promise<CityVisualReleasePolicy[]> {
  const { data } = await apiClient.get<CityVisualReleasePolicy[]>('/admin/city/visual-release-policies')
  return data
}

export async function setVisualReleasePolicy(
  spatialProfileID: string,
  request: CityVisualReleasePolicyRequest
): Promise<CityVisualReleasePolicy> {
  const { data } = await apiClient.put<CityVisualReleasePolicy>(
    `/admin/city/visual-release-policies/${encodeURIComponent(spatialProfileID)}`,
    request,
    { headers: { 'Idempotency-Key': idempotencyKey(`city-visual-release-policy-${spatialProfileID}`) } }
  )
  return data
}

export async function listVisualReviewEvents(
  packID: string,
  packVersion: string,
  limit = 100
): Promise<CityVisualReviewEvent[]> {
  const { data } = await apiClient.get<CityVisualReviewEvent[]>(`${packPath(packID, packVersion)}/review-events`, {
    params: { limit }
  })
  return data
}

const cityVisualPacksAPI = {
  listVisualPacks,
  getVisualPack,
  createVisualPack,
  updateVisualPack,
  listVisualGenerationJobs,
  createVisualGenerationJob,
  reviewVisualGenerationJob,
  publishVisualPack,
  retireVisualPack,
  listVisualReleasePolicies,
  setVisualReleasePolicy,
  listVisualReviewEvents
}

export default cityVisualPacksAPI
