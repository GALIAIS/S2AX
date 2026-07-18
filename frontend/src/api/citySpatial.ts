import { apiClient } from './client'

export type CitySpatialRuleKind =
  | 'terrain'
  | 'furniture'
  | 'structure'
  | 'portal'
  | 'item'
  | 'entity'
  | 'field'
  | 'overlay'

export interface CityWorld {
  id: number
  name: string
  owner_user_id: number
  group_id?: number
  status: string
  simulation_version: string
  current_tick: number
  simulated_at?: string
  next_tick_at?: string
  speed_multiplier: number
  timezone: string
  state_hash?: string
  settings: Record<string, unknown>
  member_role: string
  created_at: string
  updated_at: string
}

export interface CityWorldFoundation {
  world: CityWorld
  monetary_units: unknown[]
  account_templates: unknown[]
  entities: unknown[]
  physical: unknown
  markets: unknown
}

export interface CreateCityWorldRequest {
  name: string
  timezone?: string
  monetary_unit?: {
    code?: string
    name?: string
    symbol?: string
    scale?: number
  }
}

export interface CitySpatialPaletteEntry {
  id: string
  name: string
  classic_foreground: number
  classic_background?: number
}

export interface CitySpatialDefinition {
  id: string
  kind: CitySpatialRuleKind
  name: string
  glyph?: string
  foreground: string
  background?: string
  looks_like?: string
  sprite?: string
  movement_cost: number
  flags: string[]
  metadata: Record<string, unknown>
}

export interface CitySpatialRuleSet {
  id: string
  version: string
  name: string
  chunk_size: number
  min_z: number
  max_z: number
  palette: CitySpatialPaletteEntry[]
  definitions: CitySpatialDefinition[]
  content_hash: string
}

export interface CitySpatialProfile {
  world_id: number
  rule_set_id: string
  rule_set_version: string
  rule_set_hash: string
  chunk_size: number
  minimum_z: number
  maximum_z: number
  generator_id: string
  generator_version: string
  minimum_chunk_x: number
  maximum_chunk_x: number
  minimum_chunk_y: number
  maximum_chunk_y: number
  overmap_seed_proof: string
  overmap_root_hash: string
  overmap_revision: number
  metadata: Record<string, unknown>
  created_at: string
  updated_at: string
}

export interface CityWorldSpatialRuleSet {
  profile: CitySpatialProfile
  rule_set: CitySpatialRuleSet
}

export interface CityOvermapTile {
  chunk_x: number
  chunk_y: number
  z: number
  district_code: string
  terrain_definition_id: string
  road_mask: number
  river_mask: number
  variant: number
  tile_hash: string
  metadata: Record<string, unknown>
}

export interface CityOvermapState {
  profile: CitySpatialProfile
  tiles: CityOvermapTile[]
}

export type CityLandUse = 'residential' | 'commercial' | 'industrial'

export interface CityLandProfile {
  rule_set_id: string
  rule_set_version: string
  rule_set_hash: string
  spatial_overmap_root_hash: string
  nominal_cell_area_sqm: number
  baseline_hash: string
  baseline_tick: number
  zoning_rule_count: number
  parcel_count: number
  building_count: number
  unit_pool_count: number
  housing_allocation_count: number
  portal_count: number
  revision: number
}

export interface CityLandZoningRule {
  code: string
  name: string
  primary_use: CityLandUse
  max_floor_area_ratio_milli: number
  max_coverage_milli: number
  max_floors: number
  sqm_per_capacity_unit: number
}

export interface CityLandRectangle {
  chunk_x: number
  chunk_y: number
  z: number
  local_min_x: number
  local_min_y: number
  local_max_x: number
  local_max_y: number
}

export interface CityParcel {
  code: string
  district_code: string
  zone_code: string
  geometry: CityLandRectangle
  area_sqm: number
  developable_area_sqm: number
  status: string
  version: number
}

export interface CityBuilding {
  code: string
  parcel_code: string
  district_code: string
  primary_use: CityLandUse
  footprint: CityLandRectangle
  base_z: number
  top_z: number
  floor_count: number
  footprint_area_sqm: number
  floor_area_sqm: number
  capacity_units: number
  occupied_units: number
  quality_milli: number
  status: string
  completed_tick: number
  version: number
}

export interface CityBuildingUnitPool {
  code: string
  building_code: string
  district_code: string
  use_type: CityLandUse
  unit_count: number
  occupied_unit_count: number
  capacity_units_per_unit: number
  version: number
}

export interface CityHousingAllocation {
  pool_code: string
  district_code: string
  cohort_key: string
  allocated_units: number
  status: string
  version: number
}

export interface CityBuildingPortal {
  code: string
  building_code: string
  district_code: string
  portal_type: string
  from_x: number
  from_y: number
  from_z: number
  to_x: number
  to_y: number
  to_z: number
  bidirectional: boolean
  status: string
  version: number
}

export interface CityLandState {
  profile: CityLandProfile
  zoning_rules: CityLandZoningRule[]
  parcels: CityParcel[]
  buildings: CityBuilding[]
  unit_pools: CityBuildingUnitPool[]
  housing_allocations: CityHousingAllocation[]
  portals: CityBuildingPortal[]
}

export type CityDevelopmentProjectType = 'vertical_expansion' | 'renovation'
export type CityDevelopmentStatus =
  | 'submitted'
  | 'approved'
  | 'rejected'
  | 'under_construction'
  | 'completed'
  | 'cancelled'

export interface CityDevelopmentProfile {
  policy_id: string
  policy_version: string
  policy_hash: string
  baseline_tick: number
  baseline_hash: string
  project_count: number
  fact_count: number
  adjustment_count: number
  revision: number
}

export interface CityDevelopmentProject {
  code: string
  name: string
  project_type: CityDevelopmentProjectType
  district_code: string
  parcel_code: string
  building_code: string
  primary_use: CityLandUse
  developer_entity_code: string
  target_floor_count?: number
  target_quality_milli?: number
  added_floor_count: number
  added_floor_area_sqm: number
  added_capacity_units: number
  quality_delta_milli: number
  required_basic_material_units: number
  required_capital_goods_units: number
  required_labor_units: number
  planned_duration_ticks: number
  status: CityDevelopmentStatus
  progress_milli: number
  submitted_tick: number
  reviewed_tick?: number
  started_tick?: number
  planned_completion_tick?: number
  completed_tick?: number
  cancelled_tick?: number
  version: number
  metadata: Record<string, unknown>
}

export interface CityDevelopmentFact {
  tick: number
  sequence: number
  project_code: string
  source_command_sequence?: number
  fact_type: string
  from_status?: CityDevelopmentStatus
  to_status: CityDevelopmentStatus
  progress_before_milli: number
  progress_after_milli: number
  project_version_before: number
  project_version_after: number
  metadata: Record<string, unknown>
}

export interface CityBuildingAdjustment {
  project_code: string
  building_code: string
  district_code: string
  added_floor_count: number
  added_top_z: number
  added_floor_area_sqm: number
  added_capacity_units: number
  quality_delta_milli: number
  completed_tick: number
  metadata: Record<string, unknown>
}

export interface CityDevelopmentDeveloper {
  entity_id: number
  entity_code: string
  entity_name: string
  district_code: string
  employee_units: number
  reserved_labor_units: number
  available_labor_units: number
}

export interface CityDevelopmentFactCursor {
  tick: number
  sequence: number
}

export interface CityDevelopmentState {
  profile: CityDevelopmentProfile
  projects: CityDevelopmentProject[]
  facts: CityDevelopmentFact[]
  adjustments: CityBuildingAdjustment[]
  developers: CityDevelopmentDeveloper[]
  next_cursor?: CityDevelopmentFactCursor
}

export interface CityDevelopmentQuery {
  status?: CityDevelopmentStatus
  building_code?: string
  after_tick?: number
  after_sequence?: number
  limit?: number
}

export interface CityDevelopmentSubmitIntent {
  project_type: CityDevelopmentProjectType
  building_code: string
  developer_entity_id: number
  target_floor_count?: number
  target_quality_milli?: number
  name?: string
}

export type CityEnterpriseSiteType =
  | 'headquarters'
  | 'office'
  | 'production'
  | 'warehouse'
  | 'retail'

export type CityEnterpriseSiteStatus = 'active' | 'closed'

export interface CityEnterpriseLocationProfile {
  policy_id: string
  policy_version: string
  policy_hash: string
  baseline_tick: number
  baseline_hash: string
  baseline_site_count: number
  site_count: number
  fact_count: number
  revision: number
}

export interface CityEnterpriseSite {
  code: string
  firm_entity_code: string
  district_code: string
  building_code: string
  pool_code: string
  site_type: CityEnterpriseSiteType
  name: string
  occupied_units: number
  is_primary: boolean
  status: CityEnterpriseSiteStatus
  opened_tick: number
  last_changed_tick: number
  closed_tick?: number
  version: number
  metadata: Record<string, unknown>
}

export interface CityEnterpriseLocationFact {
  tick: number
  sequence: number
  source_command_sequence: number
  firm_entity_code: string
  site_code?: string
  fact_type: 'opened' | 'resized' | 'closed' | 'relocated'
  from_status?: CityEnterpriseSiteStatus
  to_status?: CityEnterpriseSiteStatus
  occupied_before_units: number
  occupied_after_units: number
  site_version_before: number
  site_version_after: number
  metadata: Record<string, unknown>
}

export interface CityEnterpriseFirmOption {
  entity_id: number
  entity_code: string
  entity_name: string
  district_code: string
  employee_units: number
  capital_stock_units: number
  production_capacity_units: number
  active_site_count: number
}

export interface CityEnterprisePoolAvailability {
  code: string
  building_code: string
  district_code: string
  use_type: 'commercial' | 'industrial'
  effective_unit_count: number
  occupied_unit_count: number
  available_unit_count: number
}

export interface CityEnterpriseLocationFactCursor {
  tick: number
  sequence: number
}

export interface CityEnterpriseLocationState {
  profile: CityEnterpriseLocationProfile
  baseline_sites: CityEnterpriseSite[]
  sites: CityEnterpriseSite[]
  facts: CityEnterpriseLocationFact[]
  firms: CityEnterpriseFirmOption[]
  pools: CityEnterprisePoolAvailability[]
  next_cursor?: CityEnterpriseLocationFactCursor
}

export interface CityEnterpriseLocationQuery {
  firm_code?: string
  district_code?: string
  site_type?: CityEnterpriseSiteType
  status?: CityEnterpriseSiteStatus
  after_tick?: number
  after_sequence?: number
  limit?: number
}

export type CityEnterpriseLocationCommandType =
  | 'enterprise.site.open'
  | 'enterprise.site.resize'
  | 'enterprise.site.close'
  | 'enterprise.relocate'

export interface CityTerrainRun {
  definition_id: string
  length: number
}

export interface CityFurnitureCell {
  x: number
  y: number
  definition_id: string
}

export interface CityMapChunkPayload {
  format: string
  width: number
  height: number
  terrain_runs: CityTerrainRun[]
  furniture: CityFurnitureCell[]
}

export interface CityMapChunkSummary {
  chunk_x: number
  chunk_y: number
  z: number
  district_code: string
  generator_id: string
  generator_version: string
  generation_proof: string
  revision: number
  payload_hash: string
  generated_tick: number
}

export interface CityMapChunk extends CityMapChunkSummary {
  world_id: number
  rule_set_hash: string
  payload: CityMapChunkPayload
  metadata: Record<string, unknown>
  created_at: string
  updated_at: string
}

export interface CitySpatialMutationLine {
  line_no: number
  chunk_x: number
  chunk_y: number
  z: number
  revision_before: number
  revision_after: number
  payload_hash_before?: string
  payload_hash_after: string
}

export interface CitySpatialMutation {
  id: number
  world_id: number
  tick: number
  sequence: number
  source_command_id: number
  mutation_type: string
  expected_line_count: number
  metadata: Record<string, unknown>
  posted_at: string
  created_at: string
  lines: CitySpatialMutationLine[]
}

export interface CitySpatialMutationPage {
  items: CitySpatialMutation[]
  next_cursor?: { tick: number; sequence: number }
}

export type WorldRuntimeDefinitionKind =
  | 'actor_type'
  | 'archetype'
  | 'attribute'
  | 'activity'
  | 'role'
  | 'status'
  | 'rule'

export interface WorldRuntimeProfile {
  runtime_id: string
  runtime_version: string
  catalog_version: string
  catalog_hash: string
  baseline_tick: number
  maximum_player_actors_per_member: number
  actor_count: number
  fact_count: number
  effect_count: number
  case_count: number
  revision: number
  metadata: Record<string, unknown>
}

export interface WorldRuntimeDefinition {
  kind: WorldRuntimeDefinitionKind
  code: string
  version: string
  hash: string
  visibility: 'public' | 'discoverable' | 'hidden'
  payload: Record<string, unknown>
}

export interface WorldRuntimeCatalog {
  profile: WorldRuntimeProfile
  definitions: WorldRuntimeDefinition[]
}

export interface WorldActor {
  code: string
  owner_user_id?: number
  actor_type_code: string
  name: string
  status: string
  archetype_code?: string
  archetype_version?: string
  created_tick: number
  updated_tick: number
  version: number
  metadata: Record<string, unknown>
}

export interface WorldActorAttribute {
  actor_code: string
  attribute_code: string
  value_units: number
  experience_units: number
  last_changed_tick: number
  version: number
  metadata: Record<string, unknown>
}

export interface WorldActorRole {
  actor_code: string
  role_code: string
  category_code: string
  status: 'active' | 'revoked'
  granted_tick: number
  revoked_tick?: number
  version: number
  metadata: Record<string, unknown>
}

export interface WorldActorStatus {
  actor_code: string
  instance_code: string
  status_code: string
  lifecycle_status: 'active' | 'revoked' | 'expired'
  intensity_units: number
  stacks: number
  granted_tick: number
  expires_tick?: number
  ended_tick?: number
  source_fact_tick: number
  source_fact_sequence: number
  version: number
  metadata: Record<string, unknown>
}

export interface WorldRuntimeFactRef {
  tick: number
  sequence: number
}

export interface WorldRuntimeFact {
  tick: number
  sequence: number
  source_command_sequence?: number
  parent?: WorldRuntimeFactRef
  actor_code?: string
  fact_type: string
  definition_kind?: string
  definition_code?: string
  definition_version?: string
  definition_hash?: string
  payload: Record<string, unknown>
}

export interface WorldEffectOperation {
  tick: number
  sequence: number
  source_fact: WorldRuntimeFactRef
  operation_index: number
  effect_type: string
  executor_version: string
  target_actor_code?: string
  target_key?: string
  before_units?: number
  delta_units?: number
  after_units?: number
  payload: Record<string, unknown>
}

export interface WorldRuleCase {
  code: string
  tick: number
  sequence: number
  source_fact: WorldRuntimeFactRef
  consequence_fact?: WorldRuntimeFactRef
  subject_actor_code: string
  rule_code: string
  rule_version: string
  rule_hash: string
  category_code: string
  scope_kind: string
  scope_code: string
  status: string
  severity_units: number
  decision_code?: string
  created_tick: number
  decided_tick?: number
  closed_tick?: number
  payload: Record<string, unknown>
}

export interface WorldRuleCaseCursor {
  tick: number
  sequence: number
}

export interface WorldRuleCasePage {
  items: WorldRuleCase[]
  next_cursor?: WorldRuleCaseCursor
}

export interface WorldRuleCaseQuery {
  actor_code?: string
  category_code?: string
  status?: string
  after_tick?: number
  after_sequence?: number
  limit?: number
}

export interface WorldRequirementFailure {
  path: string
  operator: string
  code?: string
  actual_units?: number
  required_units?: number
  message_code: string
}

export interface WorldRequirementEvaluation {
  satisfied: boolean
  failures: WorldRequirementFailure[]
}

export interface WorldActorRoleOption {
  definition: WorldRuntimeDefinition
  active: boolean
  eligible: boolean
  current_category_role?: string
  cooldown_remaining_ticks: number
  blocked_reason_codes: string[]
  evaluation: WorldRequirementEvaluation
}

export interface WorldActorState {
  actor: WorldActor
  attributes: WorldActorAttribute[]
  roles: WorldActorRole[]
  statuses: WorldActorStatus[]
  recent_facts: WorldRuntimeFact[]
}

export type WorldRuntimeCommandType =
  | 'actor.create'
  | 'actor.activity.perform'
  | 'actor.role.transition'

export interface CityCommand {
  id: number
  world_id: number
  user_id: number
  sequence: number
  client_request_id: string
  command_type: string
  payload: Record<string, unknown>
  expected_world_tick?: number
  status: 'pending' | 'applied' | 'rejected'
  processed_tick?: number
  result: Record<string, unknown>
  error_code?: string
  submitted_at: string
  updated_at: string
}

export interface CityTick {
  id: number
  world_id: number
  tick: number
  state_hash: string
  command_count: number
  applied_command_count: number
  rejected_command_count: number
  event_count: number
  duration_ms: number
}

export interface CityStepResult {
  tick: CityTick
  commands: CityCommand[]
  spatial_mutations: CitySpatialMutation[]
  development_facts: CityDevelopmentFact[]
  building_adjustments: CityBuildingAdjustment[]
  enterprise_location_facts: CityEnterpriseLocationFact[]
  world_runtime_facts: WorldRuntimeFact[]
  world_effect_operations: WorldEffectOperation[]
  world_rule_cases: WorldRuleCase[]
  events: unknown[]
}

export interface CityChunkBoundsQuery {
  min_x: number
  max_x: number
  min_y: number
  max_y: number
  z: number
}

const worldPath = (worldID: number): string => `/city/worlds/${worldID}`

function idempotencyKey(prefix: string): string {
  const id = globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random().toString(36).slice(2)}`
  return `${prefix}-${id}`
}

export async function listCityWorlds(): Promise<CityWorld[]> {
  const { data } = await apiClient.get<CityWorld[]>('/city/worlds')
  return data
}

export async function createCityWorld(request: CreateCityWorldRequest): Promise<CityWorldFoundation> {
  const { data } = await apiClient.post<CityWorldFoundation>('/city/worlds', request, {
    headers: { 'Idempotency-Key': idempotencyKey('city-world-create') }
  })
  return data
}

export async function getCityWorldSpatialRuleSet(worldID: number): Promise<CityWorldSpatialRuleSet> {
  const { data } = await apiClient.get<CityWorldSpatialRuleSet>(`${worldPath(worldID)}/spatial/ruleset`)
  return data
}

export async function getCityOvermap(worldID: number): Promise<CityOvermapState> {
  const { data } = await apiClient.get<CityOvermapState>(`${worldPath(worldID)}/spatial/overmap`)
  return data
}

export async function getCityLandState(
  worldID: number,
  bounds: CityChunkBoundsQuery
): Promise<CityLandState> {
  const { data } = await apiClient.get<CityLandState>(`${worldPath(worldID)}/land`, {
    params: bounds
  })
  return data
}

export async function getCityDevelopmentState(
  worldID: number,
  query: CityDevelopmentQuery = {}
): Promise<CityDevelopmentState> {
  const { data } = await apiClient.get<CityDevelopmentState>(`${worldPath(worldID)}/development`, {
    params: {
      after_tick: 0,
      after_sequence: 0,
      limit: 200,
      ...query
    }
  })
  return data
}

export async function getCityEnterpriseLocationState(
  worldID: number,
  query: CityEnterpriseLocationQuery = {}
): Promise<CityEnterpriseLocationState> {
  const { data } = await apiClient.get<CityEnterpriseLocationState>(
    `${worldPath(worldID)}/enterprise-locations`,
    {
      params: {
        after_tick: 0,
        after_sequence: 0,
        limit: 200,
        ...query
      }
    }
  )
  return data
}

export async function listCityMapChunks(
  worldID: number,
  bounds: CityChunkBoundsQuery
): Promise<CityMapChunkSummary[]> {
  const { data } = await apiClient.get<CityMapChunkSummary[]>(`${worldPath(worldID)}/spatial/chunks`, {
    params: bounds
  })
  return data
}

export async function getCityMapChunk(
  worldID: number,
  chunkX: number,
  chunkY: number,
  z: number
): Promise<CityMapChunk> {
  const { data } = await apiClient.get<CityMapChunk>(
    `${worldPath(worldID)}/spatial/chunks/${chunkX}/${chunkY}/${z}`
  )
  return data
}

export async function listCitySpatialChanges(worldID: number, limit = 100): Promise<CitySpatialMutationPage> {
  const { data } = await apiClient.get<CitySpatialMutationPage>(`${worldPath(worldID)}/spatial/changes`, {
    params: { after_tick: 0, after_sequence: 0, limit }
  })
  return data
}

export async function submitGenerateCityChunk(
  worldID: number,
  chunkX: number,
  chunkY: number,
  z: number,
  expectedWorldTick: number
): Promise<CityCommand> {
  const { data } = await apiClient.post<CityCommand>(
    `${worldPath(worldID)}/commands`,
    {
      command_type: 'spatial.generate_chunk',
      payload: { chunk_x: chunkX, chunk_y: chunkY, z },
      expected_world_tick: expectedWorldTick
    },
    { headers: { 'Idempotency-Key': idempotencyKey(`city-chunk-${worldID}-${chunkX}-${chunkY}-${z}`) } }
  )
  return data
}

export async function submitCityDevelopmentCommand(
  worldID: number,
  commandType: 'development.submit' | 'development.review' | 'development.start' | 'development.cancel',
  payload: Record<string, unknown>,
  expectedWorldTick: number
): Promise<CityCommand> {
  const { data } = await apiClient.post<CityCommand>(
    `${worldPath(worldID)}/commands`,
    {
      command_type: commandType,
      payload,
      expected_world_tick: expectedWorldTick
    },
    {
      headers: {
        'Idempotency-Key': idempotencyKey(`city-development-${worldID}-${commandType}`)
      }
    }
  )
  return data
}

export async function submitCityEnterpriseLocationCommand(
  worldID: number,
  commandType: CityEnterpriseLocationCommandType,
  payload: Record<string, unknown>,
  expectedWorldTick: number
): Promise<CityCommand> {
  const { data } = await apiClient.post<CityCommand>(
    `${worldPath(worldID)}/commands`,
    {
      command_type: commandType,
      payload,
      expected_world_tick: expectedWorldTick
    },
    {
      headers: {
        'Idempotency-Key': idempotencyKey(`city-enterprise-${worldID}-${commandType}`)
      }
    }
  )
  return data
}

export async function getWorldRuntimeCatalog(worldID: number): Promise<WorldRuntimeCatalog> {
  const { data } = await apiClient.get<WorldRuntimeCatalog>(`${worldPath(worldID)}/runtime/catalog`)
  return data
}

export async function listWorldActors(worldID: number): Promise<WorldActor[]> {
  const { data } = await apiClient.get<WorldActor[]>(`${worldPath(worldID)}/runtime/actors`)
  return data
}

export async function getWorldActorState(worldID: number, actorCode: string): Promise<WorldActorState> {
  const { data } = await apiClient.get<WorldActorState>(
    `${worldPath(worldID)}/runtime/actors/${encodeURIComponent(actorCode)}`
  )
  return data
}

export async function getWorldActorRoleOptions(
  worldID: number,
  actorCode: string
): Promise<WorldActorRoleOption[]> {
  const { data } = await apiClient.get<WorldActorRoleOption[]>(
    `${worldPath(worldID)}/runtime/actors/${encodeURIComponent(actorCode)}/roles`
  )
  return data
}

export async function listWorldRuntimeRules(worldID: number): Promise<WorldRuntimeDefinition[]> {
  const { data } = await apiClient.get<WorldRuntimeDefinition[]>(`${worldPath(worldID)}/runtime/rules`)
  return data
}

export async function listWorldRuleCases(
  worldID: number,
  query: WorldRuleCaseQuery = { limit: 100 }
): Promise<WorldRuleCasePage> {
  const { data } = await apiClient.get<WorldRuleCasePage>(`${worldPath(worldID)}/runtime/cases`, {
    params: query
  })
  return data
}

export async function submitWorldRuntimeCommand(
  worldID: number,
  commandType: WorldRuntimeCommandType,
  payload: Record<string, unknown>,
  expectedWorldTick: number
): Promise<CityCommand> {
  const { data } = await apiClient.post<CityCommand>(
    `${worldPath(worldID)}/commands`,
    { command_type: commandType, payload, expected_world_tick: expectedWorldTick },
    {
      headers: {
        'Idempotency-Key': idempotencyKey(`world-runtime-${worldID}-${commandType}`)
      }
    }
  )
  return data
}

export async function stepCityWorld(worldID: number, expectedWorldTick: number): Promise<CityStepResult> {
  const { data } = await apiClient.post<CityStepResult>(
    `${worldPath(worldID)}/step`,
    { expected_world_tick: expectedWorldTick },
    { headers: { 'Idempotency-Key': idempotencyKey(`city-step-${worldID}-${expectedWorldTick}`) } }
  )
  return data
}

const citySpatialAPI = {
  listWorlds: listCityWorlds,
  createWorld: createCityWorld,
  getWorldSpatialRuleSet: getCityWorldSpatialRuleSet,
  getOvermap: getCityOvermap,
  getLandState: getCityLandState,
  getDevelopmentState: getCityDevelopmentState,
  getEnterpriseLocationState: getCityEnterpriseLocationState,
  listMapChunks: listCityMapChunks,
  getMapChunk: getCityMapChunk,
  listSpatialChanges: listCitySpatialChanges,
  submitGenerateChunk: submitGenerateCityChunk,
  submitDevelopmentCommand: submitCityDevelopmentCommand,
  submitEnterpriseLocationCommand: submitCityEnterpriseLocationCommand,
  getWorldRuntimeCatalog,
  listWorldActors,
  getWorldActorState,
  getWorldActorRoleOptions,
  listWorldRuntimeRules,
  listWorldRuleCases,
  submitWorldRuntimeCommand,
  stepWorld: stepCityWorld
}

export default citySpatialAPI
