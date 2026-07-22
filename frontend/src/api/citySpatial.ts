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

export type CityRealtimeEngineVersion = 'city-openworld-realtime-v2'

export interface CityRealtimeViewerScope {
  membership_role: string
  can_view_shared_world: boolean
  can_manage_world: boolean
  redaction_policy: string
  projection_scope_epoch: number
}

export interface CityRealtimeSpatialBinding {
  generator_id: string
  generator_version: string
  rule_set_id: string
  rule_set_version: string
  rule_set_hash: string
  profile_id: string
  profile_version: string
  profile_hash: string
  context_hash: string
  seed: number
  spawn_sector_x: number
  spawn_sector_y: number
  spawn_x: number
  spawn_y: number
  spawn_z: number
  chunk_size: number
  sector_size_chunks: number
  epoch: number
  bootstrap_plan_hash: string
  genesis_hash: string
}

export type CityRealtimeVisualRenderContract = 'procedural_pixel_v1' | 'atlas_pixel_v1'

export interface CityRealtimeVisualBinding {
  pack_id: string
  pack_version: string
  spatial_profile_id: string
  semantic_projection_version: string
  render_contract_version: CityRealtimeVisualRenderContract
  manifest_hash: string
  asset_set_hash: string
  binding_hash: string
}

export interface CityRealtimeVisualManifestPayload {
  schema_version: number
  render_mode: CityRealtimeVisualRenderContract
  logical_tile_px: number
  profile_palettes?: Record<string, Record<string, string>>
  semantic_rules?: Record<string, string[]>
  assets?: unknown[]
}

export interface CityRealtimeVisualManifest {
  world_id: number
  binding: CityRealtimeVisualBinding
  manifest: CityRealtimeVisualManifestPayload
}

export interface CityRealtimeWorldProjection {
  world_id: number
  world_status: string
  temporal_engine_version: CityRealtimeEngineVersion
  timeline_frame_sequence: number
  timeline_cursor: string
  semantic_projection_version: string
  static_projection_hash: string
  viewer: CityRealtimeViewerScope
  spatial: CityRealtimeSpatialBinding
  visual: CityRealtimeVisualBinding
}

export interface CityRealtimeTerrainRun {
  definition_id: string
  length: number
}

export interface CityRealtimeSemanticLayer {
  x: number
  y: number
  kind: CitySpatialRuleKind
  definition_id: string
}

export interface CityRealtimeChunkPayload {
  format: string
  width: number
  height: number
  terrain_runs: CityRealtimeTerrainRun[]
  layers: CityRealtimeSemanticLayer[]
}

export interface CityRealtimeSemanticChunk {
  chunk_x: number
  chunk_y: number
  z: number
  payload: CityRealtimeChunkPayload
  payload_hash: string
  revision: number
}

export interface CityRealtimeWorldPoint {
  x: number
  y: number
  z: number
}

export interface CityRealtimeSemanticBuilding {
  code: string
  primary_use: string
  archetype_code: string
  layout_style: string
  floor_count: number
  entrance: CityRealtimeWorldPoint
  footprint: CityRealtimeWorldPoint[]
  footprint_hash: string
  revision: number
}

export interface CityRealtimePixelChunkProjection {
  world_id: number
  timeline_frame_sequence: number
  timeline_cursor: string
  semantic_projection_version: string
  static_projection_hash: string
  chunk: CityRealtimeSemanticChunk
  buildings: CityRealtimeSemanticBuilding[]
}

export interface CityRealtimeTemporalFrame {
  world_id: number
  frame_sequence: number
  timeline_cursor: string
  world_time_from_us: number
  world_time_to_us: number
  clock_segment_sequence: number
  frame_kind: string
  state_hash: string
  previous_state_hash?: string
  due_event_digest: string
  phase_summary: Record<string, unknown>
  effective_utc_from: string
  effective_utc_to: string
  created_at: string
}

export interface CityRealtimePatchPage {
  world_id: number
  after_frame_sequence: number
  current_frame_sequence: number
  current_cursor: string
  static_projection_hash: string
  full_resync_required: boolean
  items: CityRealtimeTemporalFrame[]
  next_after_frame_sequence?: number
}

export type CityRealtimeActorKind = 'npc' | 'character' | 'service'
export type CityRealtimeActorMotionState = 'idle' | 'walking' | 'inside' | 'unavailable'
export type CityRealtimeCharacterControlMode = 'manual' | 'assisted' | 'autonomous' | 'suspended'

// This contract is intentionally account-blind. The actor code and label are
// simulation-facing facts; the shared map never receives a user's email,
// username, ownership metadata, agent prompt, model, memory, or control grant.
export interface CityRealtimePublicActor {
  actor_code: string
  actor_kind: CityRealtimeActorKind
  public_label: string
  appearance_variant: string
  lifecycle_status: 'active' | 'inactive' | 'retired'
  x: number
  y: number
  z: number
  motion_state: CityRealtimeActorMotionState
  position_revision: number
  last_frame_sequence: number
}

// This owner-only projection is intentionally separate from
// CityRealtimePublicActor. The shared actor stream must remain account-blind,
// while the requesting user needs to know which public Actor is theirs.
export interface CityRealtimeCharacter {
  actor_code: string
  public_label: string
  appearance_variant: string
  lifecycle_status: 'active' | 'inactive' | 'retired'
  control_mode: CityRealtimeCharacterControlMode
  x: number
  y: number
  z: number
  motion_state: CityRealtimeActorMotionState
  position_revision: number
  last_frame_sequence: number
}

// Owner-private life state. City credits are limited to the simulated world;
// they are not a platform wallet, balance, or redemption entitlement.
export interface CityRealtimeCharacterLife {
  energy_milli: number
  satiety_milli: number
  morale_milli: number
  civic_standing_milli: number
  city_credit_units: number
  revision: number
  activity_revision: number
  law_revision: number
  metabolism_revision: number
  last_frame_sequence: number
  last_activity_world_time_us: number
  last_metabolism_world_time_us: number
  inventory: CityRealtimeCharacterInventoryStack[]
  progression?: CityRealtimeCharacterProgression
}

export interface CityRealtimeCharacterInventoryStack {
  item_code: string
  quantity: number
  revision: number
  last_frame_sequence: number
}

export interface CityRealtimeCharacterExperienceDelta {
  attribute_code: string
  experience_units: number
}

export interface CityRealtimeCharacterAttribute {
  code: string
  value_milli: number
  experience_units: number
  revision: number
  last_frame_sequence: number
}

export interface CityRealtimeCharacterRole {
  code: string
  category_code: string
  granted_frame_sequence: number
  revision: number
}

export interface CityRealtimeCharacterAttributeRequirement {
  attribute_code: string
  minimum_value_milli: number
}

export interface CityRealtimeCharacterRoleRequirements {
  minimum_civic_standing_milli?: number
  minimum_total_experience_units?: number
  attributes?: CityRealtimeCharacterAttributeRequirement[]
  required_role_codes?: string[]
}

export interface CityRealtimeCharacterRoleAvailability {
  code: string
  category_code: string
  available: boolean
  reason_code?: 'active' | 'civic_standing' | 'experience' | 'role' | 'attribute'
  requirements: CityRealtimeCharacterRoleRequirements
}

export interface CityRealtimeCharacterArchetypeOption {
  code: string
  initial_role_code: string
  initial_attributes: CityRealtimeCharacterAttribute[]
}

export interface CityRealtimeCharacterProgression {
  schema_version: number
  archetype_code: string
  revision: number
  attributes: CityRealtimeCharacterAttribute[]
  roles: CityRealtimeCharacterRole[]
  available_roles: CityRealtimeCharacterRoleAvailability[]
}

export interface CityRealtimeCharacterActivityAvailability {
  code: string
  category_code: string
  available: boolean
  reason_code?: 'cooldown' | 'location' | 'inventory' | 'needs' | 'role' | 'progression'
  cooldown_remaining_us?: number
  required_role_codes?: string[]
}

export interface CityRealtimeCharacterLocation {
	x: number
	y: number
	z: number
}

export interface CityRealtimeCharacterPortalTransition {
	portal_code: string
	portal_type: 'entrance' | 'stairs'
	direction: 'enter' | 'exit' | 'ascend' | 'descend'
	building_code: string
	target: CityRealtimeCharacterLocation
}

export type CityRealtimeCharacterInteriorCellKind = 'wall' | 'window' | 'floor' | 'door' | 'furniture'

export interface CityRealtimeCharacterInteriorCell {
	x: number
	y: number
	z: number
	kind: CityRealtimeCharacterInteriorCellKind
	feature?: string
	traversable: boolean
}

export interface CityRealtimeCharacterInteriorProjection {
	building_code: string
	floor_index: number
	z: number
	layout_style: string
	cells: CityRealtimeCharacterInteriorCell[]
}

export interface CityRealtimeCharacterActivityResult {
  code: string
  category_code: string
  outcome: 'completed' | 'penalized'
  public_visibility: boolean
  energy_delta_milli: number
  satiety_delta_milli: number
  morale_delta_milli: number
  civic_standing_delta_milli: number
  city_credit_delta_units: number
  item_code?: string
  item_quantity_delta?: number
  law_case_code?: string
  experience_deltas?: CityRealtimeCharacterExperienceDelta[]
}

export interface CityRealtimeCharacterActivityEvent {
  sequence: number
  frame_sequence: number
  activity_code: string
  category_code: string
  outcome: 'completed' | 'penalized'
  public_visibility: boolean
  energy_delta_milli: number
  satiety_delta_milli: number
  morale_delta_milli: number
  civic_standing_delta_milli: number
  city_credit_delta_units: number
  item_code?: string
  item_quantity_delta?: number
  law_case_code?: string
  law_rule_code?: string
  law_disposition?: string
  law_penalty_city_credit_units?: number
}

export interface CityRealtimeCharacterEventPage {
  items: CityRealtimeCharacterActivityEvent[]
  next_before_sequence?: number
}

export interface CityRealtimePublicCharacterEvent {
  frame_sequence: number
  actor_code: string
  public_label: string
  activity_code: string
  category_code: string
  outcome: 'completed' | 'penalized'
  law_rule_code?: string
  law_disposition?: string
}

export interface CityRealtimePublicCharacterEventPage {
  items: CityRealtimePublicCharacterEvent[]
  next_cursor?: string
}

// Owner-private personality data. This is deliberately absent from shared
// actor projections, event feeds and model observation APIs.
export interface CityRealtimeCharacterPersonalitySeed {
  values: string[]
  preferences: Record<string, string>
  background: string
  hard_boundaries: string[]
  freeform_notes: string
}

export interface CityRealtimeCharacterPersonalityProjection {
  schema_version: number
  revision: number
  seed_hash: string
  seed: CityRealtimeCharacterPersonalitySeed
}

export interface CityRealtimeCharacterAgentConfiguration {
  control_mode: CityRealtimeCharacterControlMode
  personality?: CityRealtimeCharacterPersonalityProjection
  pending_decision: boolean
  pending_intent: boolean
  autonomy_runtime_available: boolean
}

export interface CityRealtimeMyCharacterProjection {
  world_id: number
  timeline_frame_sequence: number
  timeline_cursor: string
  runtime_ready: boolean
  exists: boolean
  character?: CityRealtimeCharacter
  life?: CityRealtimeCharacterLife
  agent?: CityRealtimeCharacterAgentConfiguration
  available_archetypes?: CityRealtimeCharacterArchetypeOption[]
  available_activities?: CityRealtimeCharacterActivityAvailability[]
  available_portals?: CityRealtimeCharacterPortalTransition[]
  current_interior?: CityRealtimeCharacterInteriorProjection
}

export interface CityRealtimeCharacterMutationResult {
  character: CityRealtimeCharacter
  life?: CityRealtimeCharacterLife
  activity?: CityRealtimeCharacterActivityResult
  role_change?: CityRealtimeCharacterRoleChangeResult
  agent?: CityRealtimeCharacterAgentConfiguration
  frame: CityRealtimeTemporalFrame
}

export interface CityRealtimeCharacterRoleChangeResult {
  category_code: string
  from_role_code: string
  to_role_code: string
}

export interface CityRealtimeCreateCharacterRequest {
  public_label: string
  archetype_code?: string
}

export interface CityRealtimeConfigureCharacterAgentRequest {
  control_mode?: Extract<CityRealtimeCharacterControlMode, 'autonomous' | 'suspended'>
  personality?: CityRealtimeCharacterPersonalitySeed
}

export interface CityRealtimeMoveCharacterRequest {
	x: number
	y: number
	z: number
}

export interface CityRealtimeTraverseCharacterPortalRequest {
	portal_code: string
}

export interface CityRealtimePerformCharacterActivityRequest {
  activity_code: string
}

export interface CityRealtimeChangeCharacterRoleRequest {
  role_code: string
}

export interface CityRealtimeActorSnapshot {
  world_id: number
  timeline_frame_sequence: number
  timeline_cursor: string
  static_projection_hash: string
  projection_scope_epoch: number
  minimum_chunk_x: number
  maximum_chunk_x: number
  minimum_chunk_y: number
  maximum_chunk_y: number
  z: number
  actor_projection_hash: string
  actors: CityRealtimePublicActor[]
}

export interface CityRealtimeActorSnapshotQuery {
  min_chunk_x: number
  max_chunk_x: number
  min_chunk_y: number
  max_chunk_y: number
  z?: number
  limit?: number
}

export interface CityRealtimeClock {
  world_id: number
  temporal_engine_version: CityRealtimeEngineVersion
  timeline_cursor: string
  clock_profile_id: string
  clock_profile_hash: string
  time_quantum_us: number
  world_time: {
    elapsed_us: number
    committed_elapsed_us: number
    live_projection: boolean
    timezone: string
    local_time: string
    source_effective_utc: string
    clock_state: string
    recovery_state: string
    catchup_target_world_time_us?: number
    source_clock_mode: string
  }
}

export interface CityWorldFoundation {
  world: CityWorld
  monetary_units: unknown[]
  account_templates: unknown[]
  entities: unknown[]
  physical: unknown
  markets: unknown
}

export type CityMemberRole = 'owner' | 'planner' | 'treasurer' | 'trader' | 'viewer'
export type CityMemberStatus = 'active' | 'left' | 'banned'

export interface CityMember {
  user_id: number
  email: string
  username: string
  role: CityMemberRole
  status: CityMemberStatus
  joined_at: string
  left_at?: string
  banned_at?: string
  updated_at: string
}

export interface AddCityWorldMemberRequest {
  identity: string
  role: Exclude<CityMemberRole, 'owner'>
}

export interface UpdateCityWorldMemberRequest {
  role?: Exclude<CityMemberRole, 'owner'>
  status?: CityMemberStatus
}

export interface CreateCityWorldRequest {
  name: string
  timezone?: string
  style_profile_id?: string
  spawn_policy?: 'city_center'
  // Realtime is a server-owned world mode. The browser cannot select an
  // engine version, clock profile, source, or clock tolerance.
  realtime?: boolean
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

export type CityBuildingLayoutCellKind = 'wall' | 'window' | 'floor' | 'door' | 'furniture'

export interface CityBuildingLayoutCell {
  x: number
  y: number
  z: number
  kind: CityBuildingLayoutCellKind
  feature?: string
}

export interface CityBuildingLayout {
  building_code: string
  layout_version: string
  archetype: string
  cells: CityBuildingLayoutCell[]
}

export type CityParcelLayoutCellKind = 'path' | 'garden' | 'tree' | 'sidewalk' | 'parking' | 'loading'

export interface CityParcelLayoutCell {
  x: number
  y: number
  z: number
  kind: CityParcelLayoutCellKind
}

export interface CityParcelLayout {
  parcel_code: string
  layout_version: string
  style: string
  cells: CityParcelLayoutCell[]
}

export interface CityWorldgenBounds {
  minimum_chunk_x: number
  maximum_chunk_x: number
  minimum_chunk_y: number
  maximum_chunk_y: number
  z: number
}

export interface CityWorldgenTerrainPatch {
  chunk_x: number
  chunk_y: number
  z: number
  biome_code: string
  definition_id: string
  elevation_milli: number
  moisture_milli: number
}

export interface CityWorldgenPoint {
  x: number
  y: number
  z: number
}

export interface CityWorldgenRoad {
  code: string
  city_code: string
  class: 'arterial' | 'local'
  width: number
  points: CityWorldgenPoint[]
}

export interface CityWorldgenLot {
  code: string
  city_code: string
  district_code: string
  primary_use: CityLandUse
  bounds: {
    minimum_x: number
    maximum_x: number
    minimum_y: number
    maximum_y: number
    z: number
  }
  frontage_road_code: string
  frontage_direction: 'north' | 'east' | 'south' | 'west'
}

export interface CityWorldgenBuilding {
  code: string
  city_code: string
  lot_code: string
  primary_use: CityLandUse
  archetype_code: string
  layout_style: string
  floor_count: number
  entrance: CityWorldgenPoint
  footprint: CityWorldgenPoint[]
}

export interface CityOpenWorldStyleProfile {
  id: string
  version: string
  name: string
  content_hash: string
}

export interface CityOpenWorldBinding {
  world_id: number
  generator_id: string
  generator_version: string
  rule_set_id: string
  rule_set_version: string
  rule_set_hash: string
  profile_id: string
  profile_version: string
  profile_hash: string
  context_hash: string
  seed: number
  spawn_sector_x: number
  spawn_sector_y: number
  spawn_x: number
  spawn_y: number
  spawn_z: number
  epoch: number
  bootstrap_plan_hash: string
  genesis_hash: string
  created_at: string
  updated_at: string
}

export interface CityOpenWorldSector {
  sector_x: number
  sector_y: number
  epoch: number
  chunk_size: number
  sector_size_chunks: number
  status: 'generated'
  plan_hash: string
  content_hash: string
  generated_tick: number
  revision: number
  created_at: string
  updated_at: string
}

export interface CityOpenWorldRegion {
  region_x: number
  region_y: number
  epoch: number
  chunk_size: number
  region_size_chunks: number
  status: 'generated'
  plan_hash: string
  generated_tick: number
  revision: number
  created_at: string
  updated_at: string
}

export interface CityOpenWorldGenerationState {
  binding: CityOpenWorldBinding
  regions: CityOpenWorldRegion[]
  sectors: CityOpenWorldSector[]
}

export interface CityOpenWorldCellLayer {
  x: number
  y: number
  kind: CitySpatialRuleKind
  definition_id: string
}

export interface CityOpenWorldChunkPayload {
  format: 'city-openworld-chunk-v1'
  width: number
  height: number
  terrain_runs: CityTerrainRun[]
  layers: CityOpenWorldCellLayer[]
}

export interface CityOpenWorldChunk {
  chunk_x: number
  chunk_y: number
  z: number
  payload: CityOpenWorldChunkPayload
  payload_hash: string
  revision: number
}

export interface CityOpenWorldBuilding {
  code: string
  city_code: string
  lot_code: string
  primary_use: CityLandUse
  archetype_code: string
  layout_style: string
  floor_count: number
  entrance: CityWorldgenPoint
  footprint: CityWorldgenPoint[]
  footprint_hash: string
  interior_floor_count: number
  ground_interior_version?: string
  ground_interior_hash?: string
  revision: number
}

export interface CityOpenWorldBuildingInterior {
  building_code: string
  floor_index: number
  z: number
  layout_version: string
  layout_style: string
  cells: CityBuildingLayoutCell[]
  content_hash: string
  revision: number
}

export interface CityOpenWorldPortal {
	code: string
	building_code: string
  portal_type: 'entrance' | 'stairs'
  from_floor_index: number
  to_floor_index: number
  from: CityWorldgenPoint
  to: CityWorldgenPoint
  bidirectional: boolean
  topology_hash: string
	revision: number
}

export interface CityOpenWorldVerification {
	world_id: number
	simulation_version: string
	scope: 'world' | 'region'
	region_x?: number
	region_y?: number
	current_tick: number
	state_hash: string
	canonical_state_verified: boolean
	region_count: number
	sector_count: number
	chunk_count: number
	building_count: number
	interior_count: number
	portal_count: number
	verified_at: string
}

export interface CityOpenWorldMapQuery {
  min_x: number
  max_x: number
  min_y: number
  max_y: number
  z: number
}

export interface CityOpenWorldMap {
  binding: CityOpenWorldBinding
  chunks: CityOpenWorldChunk[]
  buildings: CityOpenWorldBuilding[]
}

export interface CityWorldgenWindow {
  generator_id: string
  generator_version: string
  profile_id: string
  profile_version: string
  plan_hash: string
  bounds: CityWorldgenBounds
  terrain: CityWorldgenTerrainPatch[]
  cities: Array<{
    code: string
    center: CityWorldgenPoint
    radius_chunks: number
    biome_code: string
    elevation_milli: number
    moisture_milli: number
    placement_mode: 'preferred' | 'fallback'
  }>
  roads: CityWorldgenRoad[]
  lots: CityWorldgenLot[]
  buildings: CityWorldgenBuilding[]
}

export interface CityLandState {
  profile: CityLandProfile
  zoning_rules: CityLandZoningRule[]
  parcels: CityParcel[]
  buildings: CityBuilding[]
  unit_pools: CityBuildingUnitPool[]
  housing_allocations: CityHousingAllocation[]
  portals: CityBuildingPortal[]
  building_layouts?: CityBuildingLayout[]
  parcel_layouts?: CityParcelLayout[]
  worldgen?: CityWorldgenWindow
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
  location?: WorldActorLocation
}

export interface WorldActorLocation {
  actor_code: string
  space_kind: string
  space_code: string
  x: number
  y: number
  z: number
  chunk_x: number
  chunk_y: number
  local_x: number
  local_y: number
  anchor_kind?: 'chunk' | 'building' | 'site'
  anchor_code?: string
  jurisdiction_code: string
  moved_tick: number
  source_fact?: WorldRuntimeFactRef
  version: number
  metadata: Record<string, unknown>
}

export interface CityNavigationCoordinate {
  x: number
  y: number
  z: number
}

export interface CityNavigationPathStep {
  coordinate: CityNavigationCoordinate
  step_cost: number
  total_cost: number
  anchor_kind: string
  anchor_code: string
  jurisdiction_code: string
}

export interface CityNavigationPath {
  navigation_version: string
  world_tick: number
  spatial_rule_hash: string
  actor_code: string
  from: CityNavigationCoordinate
  to: CityNavigationCoordinate
  reachable: boolean
  reason?: string
  total_cost: number
  expanded_nodes: number
  steps: CityNavigationPathStep[]
}

export type WorldNavigationIntentStatus =
  | 'active'
  | 'blocked'
  | 'arrived'
  | 'cancelled'
  | 'failed'

export type WorldNavigationOnBlocked = 'retry' | 'cancel'

export interface WorldNavigationProfile {
  profile_version: string
  baseline_tick: number
  maximum_intents_per_tick: number
  default_budget_gain_units: number
  default_budget_cap_units: number
  default_max_steps: number
  maximum_blocked_attempts: number
  maximum_retry_delay_ticks: number
  fairness_aging_cap: number
  revision: number
  metadata: Record<string, unknown>
}

export interface WorldActorNavigationIntent {
  actor_code: string
  intent_code: string
  destination: CityNavigationCoordinate
  status: WorldNavigationIntentStatus
  on_blocked: WorldNavigationOnBlocked
  priority: number
  max_steps: number
  budget_units: number
  budget_gain_units: number
  budget_cap_units: number
  blocked_attempts: number
  last_reason?: string
  next_attempt_tick: number
  created_tick: number
  updated_tick: number
  source_fact: WorldRuntimeFactRef
  version: number
  metadata: Record<string, unknown>
}

export interface WorldNavigationReservation {
  tick: number
  sequence: number
  actor_code: string
  intent_code: string
  from: CityNavigationCoordinate
  to: CityNavigationCoordinate
  target_key: string
  edge_key: string
  step_cost: number
  source_fact: WorldRuntimeFactRef
  status: string
  metadata: Record<string, unknown>
}

export type WorldRequirementOperator =
  | 'all'
  | 'any'
  | 'not'
  | 'attribute_gte'
  | 'attribute_lte'
  | 'experience_gte'
  | 'role_active'
  | 'role_inactive'
  | 'status_present'
  | 'status_absent'
  | 'fact_count_gte'
  | 'world_tick_gte'

export interface WorldRequirementNode {
  op: WorldRequirementOperator
  items?: WorldRequirementNode[]
  item?: WorldRequirementNode
  attribute_code?: string
  role_code?: string
  status_code?: string
  fact_type?: string
  value_units?: number
  minimum_stacks?: number
  window_ticks?: number
}

export type WorldPortalStateCode = 'open' | 'closed' | 'locked'
export type WorldPortalAction = 'open' | 'close' | 'lock' | 'unlock'

export interface WorldPortalState {
  building_code: string
  portal_code: string
  portal_type: string
  state_code: WorldPortalStateCode
  access_requirement: WorldRequirementNode
  access_policy_hash: string
  changed_tick: number
  source_fact?: WorldRuntimeFactRef
  version: number
  metadata: Record<string, unknown>
}

export interface WorldPortalAccessView {
  state: WorldPortalState
  from: CityNavigationCoordinate
  to: CityNavigationCoordinate
  bidirectional: boolean
  accessible?: boolean
  access_evaluation?: WorldRequirementEvaluation
}

export type WorldActorCapability = 'actor.command' | 'actor.control.manage'

export interface WorldActorControlGrant {
  code: string
  actor_code: string
  user_id: number
  capability: WorldActorCapability
  status: 'active' | 'revoked'
  granted_by_user_id: number
  granted_tick: number
  revoked_tick?: number
  grant_source_fact?: WorldRuntimeFactRef
  revoke_source_fact?: WorldRuntimeFactRef
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
  location?: WorldActorLocation
  control_grants: WorldActorControlGrant[]
  capabilities: WorldActorCapability[]
  navigation_intent?: WorldActorNavigationIntent
}

export type WorldRuntimeCommandType =
  | 'actor.create'
  | 'actor.activity.perform'
  | 'actor.role.transition'
  | 'actor.location.move'
  | 'actor.control.grant'
  | 'actor.control.revoke'
  | 'portal.state.transition'
  | 'portal.access.configure'
  | 'actor.navigation.intent.set'
  | 'actor.navigation.intent.cancel'

export type CityOpenWorldRuntimeCommandType =
  | 'open_world.actor.create'
  | 'open_world.actor.activity.perform'
  | 'open_world.actor.role.transition'
  | 'open_world.actor.move'
  | 'open_world.actor.portal.use'
  | 'open_world.actor.control.grant'
  | 'open_world.actor.control.revoke'
  | 'open_world.portal.state.set'
  | 'open_world.portal.access.set'
  | 'open_world.actor.navigation.set'
  | 'open_world.actor.navigation.cancel'

export type CityRuntimeCommandType = WorldRuntimeCommandType | CityOpenWorldRuntimeCommandType

// These are world-lifecycle controls, deliberately separate from player
// runtime commands. The server keeps them administrator-only.
export type CityWorldControlCommandType =
  | 'world.pause'
  | 'world.resume'
  | 'world.set_speed'

export type CityServiceAvailability = 'available' | 'unsupported'
export type CityFacilityStatus = 'offline' | 'operational' | 'degraded' | 'retired'
export type CityServiceProjectionStatus = 'active' | 'suspended' | 'retired'
export type CityServiceSubjectKind = 'district' | 'building' | 'household' | 'enterprise' | 'actor'
export type CityServiceCommandType =
  | 'facility.register'
  | 'facility.status.transition'
  | 'facility.capacity.configure'
  | 'service.demand.configure'
  | 'service.connection.configure'
  | 'network.configure'
  | 'network.node.configure'
  | 'network.edge.configure'
  | 'network.edge.transition'

export interface CityServiceProfile {
  catalog_id: string
  catalog_version: string
  catalog_hash: string
  settlement_version: string
  baseline_tick: number
  service_definition_count: number
  facility_type_count: number
  facility_count: number
  capacity_count: number
  demand_count: number
  connection_count: number
  fact_count: number
  allocation_count: number
  settlement_count: number
  revision: number
  metadata: Record<string, unknown>
}

export interface CityServiceDefinition {
  code: string
  definition_version: string
  definition_hash: string
  name: string
  category: string
  unit_code: string
  flow_kind: 'delivery' | 'collection' | 'capacity'
  status: string
  sort_order: number
  payload: Record<string, unknown>
}

export interface CityFacilityTypeDefinition {
  code: string
  definition_version: string
  definition_hash: string
  name: string
  minimum_floor_area_sqm: number
  default_reliability_milli: number
  allowed_service_codes: string[]
  status: string
  sort_order: number
  payload: Record<string, unknown>
}

export interface CityFacility {
  code: string
  name: string
  facility_type_code: string
  facility_type_version: string
  facility_type_hash: string
  district_code: string
  building_code: string
  owner_entity_code?: string
  status: CityFacilityStatus
  reliability_milli: number
  created_tick: number
  updated_tick: number
  version: number
  source_fact_tick: number
  source_fact_sequence: number
  metadata: Record<string, unknown>
}

export interface CityFacilityServiceCapacity {
  facility_code: string
  service_code: string
  service_version: string
  service_hash: string
  installed_capacity_units: number
  availability_milli: number
  available_capacity_units: number
  dispatch_capacity_units: number
  updated_tick: number
  version: number
  source_fact_tick: number
  source_fact_sequence: number
  metadata: Record<string, unknown>
}

export interface CityServiceDemand {
  code: string
  service_code: string
  service_version: string
  service_hash: string
  subject_kind: CityServiceSubjectKind
  subject_code: string
  district_code: string
  building_code?: string
  requested_units_per_tick: number
  priority: number
  status: CityServiceProjectionStatus
  created_tick: number
  updated_tick: number
  version: number
  source_fact_tick: number
  source_fact_sequence: number
  metadata: Record<string, unknown>
}

export interface CityServiceConnection {
  code: string
  facility_code: string
  service_code: string
  demand_code: string
  max_flow_units_per_tick: number
  loss_milli: number
  preference: number
  status: CityServiceProjectionStatus
  created_tick: number
  updated_tick: number
  version: number
  source_fact_tick: number
  source_fact_sequence: number
  metadata: Record<string, unknown>
}

export interface CityServiceFact {
  tick: number
  sequence: number
  source_command_sequence?: number
  fact_type: string
  subject_kind: string
  subject_code: string
  version_before: number
  version_after: number
  payload: Record<string, unknown>
}

export interface CityServiceAllocation {
  tick: number
  sequence: number
  allocation_index: number
  service_code: string
  facility_code: string
  demand_code: string
  connection_code: string
  capacity_version: number
  demand_version: number
  connection_version: number
  facility_capacity_units: number
  connection_capacity_units: number
  loss_milli: number
  dispatched_units: number
  delivered_units: number
  loss_units: number
  metadata: Record<string, unknown>
}

export interface CityServiceSettlement {
  tick: number
  sequence: number
  service_code: string
  demand_code: string
  demand_version: number
  requested_units: number
  delivered_units: number
  shortage_units: number
  allocation_count: number
  quality_milli: number
  metadata: Record<string, unknown>
}

export interface CityServiceOverview {
  facility_count: number
  operational_facility_count: number
  active_capacity_count: number
  dispatch_capacity_units: string
  active_demand_count: number
  requested_units_per_tick: string
  latest_settlement_tick?: number
  latest_requested_units: string
  latest_delivered_units: string
  latest_shortage_units: string
  latest_weighted_quality_milli: number
}

export interface CityServiceCatalogView {
  availability: CityServiceAvailability
  simulation_version: string
  required_version: string
  profile?: CityServiceProfile
  overview?: CityServiceOverview
  service_definitions: CityServiceDefinition[]
  facility_types: CityFacilityTypeDefinition[]
}

export interface CityServiceFacilityView {
  facility: CityFacility
  capacities: CityFacilityServiceCapacity[]
}

export interface CityServiceFacilityPage {
  availability: CityServiceAvailability
  simulation_version: string
  required_version: string
  items: CityServiceFacilityView[]
  next_code?: string
}

export interface CityServiceDemandView {
  demand: CityServiceDemand
  latest_settlement?: CityServiceSettlement
}

export interface CityServiceDemandPage {
  availability: CityServiceAvailability
  simulation_version: string
  required_version: string
  items: CityServiceDemandView[]
  next_code?: string
}

export interface CityServiceConnectionPage {
  availability: CityServiceAvailability
  simulation_version: string
  required_version: string
  items: CityServiceConnection[]
  next_code?: string
}

export interface CityServiceSettlementView {
  settlement: CityServiceSettlement
  allocations: CityServiceAllocation[]
}

export interface CityServiceSettlementPage {
  availability: CityServiceAvailability
  simulation_version: string
  required_version: string
  items: CityServiceSettlementView[]
  next_cursor?: { tick: number; sequence: number }
}

export interface CityServiceListQuery {
  service?: string
  status?: string
  district?: string
  facility?: string
  demand?: string
  after_code?: string
  after_tick?: number
  after_sequence?: number
  limit?: number
}

export type CityPhysicalNetworkStatus = 'active' | 'suspended' | 'retired'
export type CityPhysicalNetworkNodeRole = 'supply' | 'demand' | 'junction' | 'storage' | 'gateway'
export type CityPhysicalNetworkNodeStatus = 'active' | 'offline' | 'retired'
export type CityPhysicalNetworkEdgeDirection = 'directed' | 'bidirectional'
export type CityPhysicalNetworkEdgeStatus = 'active' | 'isolated' | 'failed' | 'retired'

export interface CityPhysicalNetworkProfile {
  policy_id: string
  policy_version: string
  policy_hash: string
  baseline_tick: number
  policy_count: number
  network_count: number
  node_count: number
  edge_count: number
  fact_count: number
  batch_count: number
  path_count: number
  segment_count: number
  revision: number
  metadata: Record<string, unknown>
}

export interface CityPhysicalNetworkPolicy {
  service_code: string
  policy_version: string
  policy_hash: string
  network_required: boolean
  route_direction: 'supply_to_demand' | 'demand_to_facility'
  maximum_nodes: number
  maximum_edges: number
  maximum_paths: number
  maximum_hops: number
  loss_cost_weight: number
  allow_bidirectional: boolean
  algorithm_version: string
  payload: Record<string, unknown>
}

export interface CityPhysicalNetworkOverview {
  active_network_count: number
  active_node_count: number
  active_edge_count: number
  isolated_edge_count: number
  failed_edge_count: number
  installed_edge_capacity_units: string
  available_edge_capacity_units: string
  latest_flow_tick?: number
  latest_dispatched_units: string
  latest_network_received_units: string
  latest_network_loss_units: string
  latest_delivery_ratio_milli: number
}

export interface CityPhysicalNetworkCatalogView {
  availability: CityServiceAvailability
  simulation_version: string
  required_version: string
  profile?: CityPhysicalNetworkProfile
  overview?: CityPhysicalNetworkOverview
  policies: CityPhysicalNetworkPolicy[]
}

export interface CityPhysicalNetwork {
  code: string
  name: string
  service_code: string
  status: CityPhysicalNetworkStatus
  topology_revision: number
  created_tick: number
  updated_tick: number
  version: number
  source_fact_tick: number
  source_fact_sequence: number
  metadata: Record<string, unknown>
}

export interface CityPhysicalNetworkNode {
  code: string
  network_code: string
  role: CityPhysicalNetworkNodeRole
  capacity_code?: string
  demand_code?: string
  district_code?: string
  building_code?: string
  world_x?: number
  world_y?: number
  world_z?: number
  status: CityPhysicalNetworkNodeStatus
  created_tick: number
  updated_tick: number
  version: number
  source_fact_tick: number
  source_fact_sequence: number
  metadata: Record<string, unknown>
}

export interface CityPhysicalNetworkEdge {
  code: string
  network_code: string
  from_node_code: string
  to_node_code: string
  direction: CityPhysicalNetworkEdgeDirection
  installed_capacity_units: number
  availability_milli: number
  available_capacity_units: number
  loss_milli: number
  base_cost_units: number
  status: CityPhysicalNetworkEdgeStatus
  condition_milli: number
  failure_count: number
  created_tick: number
  updated_tick: number
  version: number
  source_fact_tick: number
  source_fact_sequence: number
  metadata: Record<string, unknown>
}

export interface CityPhysicalNetworkFact {
  tick: number
  sequence: number
  phase: 'command' | 'pre_network' | 'settlement' | string
  source_command_sequence?: number
  fact_type: string
  subject_kind: string
  subject_code: string
  version_before: number
  version_after: number
  payload: Record<string, unknown>
}

export interface CityPhysicalNetworkFlowBatch {
  tick: number
  sequence: number
  network_code: string
  service_code: string
  topology_revision: number
  allocation_count: number
  path_count: number
  segment_count: number
  dispatched_units: number
  network_received_units: number
  network_loss_units: number
  source_fact_tick: number
  source_fact_sequence: number
  metadata: Record<string, unknown>
}

export interface CityPhysicalNetworkFlowPath {
  tick: number
  sequence: number
  service_sequence: number
  allocation_index: number
  path_index: number
  network_code: string
  connection_code: string
  source_node_code: string
  sink_node_code: string
  hop_count: number
  dispatched_units: number
  network_received_units: number
  network_loss_units: number
  path_cost_units: number
  path_hash: string
  metadata: Record<string, unknown>
}

export interface CityPhysicalNetworkFlowSegment {
  tick: number
  sequence: number
  service_sequence: number
  allocation_index: number
  path_index: number
  segment_index: number
  edge_code: string
  edge_version: number
  direction: string
  from_node_code: string
  to_node_code: string
  edge_capacity_units: number
  loss_milli: number
  input_units: number
  output_units: number
  loss_units: number
  metadata: Record<string, unknown>
}

export interface CityPhysicalNetworkFlowView {
  batch: CityPhysicalNetworkFlowBatch
  paths: CityPhysicalNetworkFlowPath[]
  segments: CityPhysicalNetworkFlowSegment[]
}

export interface CityPhysicalNetworkPage {
  availability: CityServiceAvailability
  simulation_version: string
  required_version: string
  items: CityPhysicalNetwork[]
  next_code?: string
}

export interface CityPhysicalNetworkNodePage {
  availability: CityServiceAvailability
  simulation_version: string
  required_version: string
  items: CityPhysicalNetworkNode[]
  next_code?: string
}

export interface CityPhysicalNetworkEdgePage {
  availability: CityServiceAvailability
  simulation_version: string
  required_version: string
  items: CityPhysicalNetworkEdge[]
  next_code?: string
}

export interface CityPhysicalNetworkFlowPage {
  availability: CityServiceAvailability
  simulation_version: string
  required_version: string
  items: CityPhysicalNetworkFlowView[]
  next_cursor?: { tick: number; sequence: number }
}

export interface CityPhysicalNetworkFactPage {
  availability: CityServiceAvailability
  simulation_version: string
  required_version: string
  items: CityPhysicalNetworkFact[]
  next_cursor?: { tick: number; sequence: number }
}

export interface CityPhysicalNetworkComponentDiagnostic {
  index: number
  node_count: number
  edge_count: number
  supply_node_count: number
  demand_node_count: number
  node_codes: string[]
  service_island: boolean
}

export interface CityPhysicalNetworkEdgeDiagnostic {
  edge_code: string
  status: CityPhysicalNetworkEdgeStatus
  available_capacity_units: number
  latest_input_units: number
  latest_output_units: number
  latest_loss_units: number
  utilization_milli: number
  saturated: boolean
  bottleneck: boolean
}

export interface CityPhysicalNetworkDiagnosticSegment {
  index: number
  edge_code: string
  direction: string
  from_node_code: string
  to_node_code: string
  edge_capacity_units: number
  loss_milli: number
  input_units: number
  output_units: number
  loss_units: number
}

export interface CityPhysicalNetworkDiagnosticPath {
  index: number
  cost_units: number
  dispatched_units: number
  network_received_units: number
  network_loss_units: number
  path_hash: string
  segments: CityPhysicalNetworkDiagnosticSegment[]
}

export interface CityPhysicalNetworkRouteDiagnostic {
  source_node_code: string
  sink_node_code: string
  probe_units: number
  reachable: boolean
  reason_code: string
  dispatched_units: number
  network_received_units: number
  network_loss_units: number
  paths: CityPhysicalNetworkDiagnosticPath[]
}

export interface CityPhysicalNetworkDiagnosticsView {
  availability: CityServiceAvailability
  simulation_version: string
  required_version: string
  network?: CityPhysicalNetwork
  policy?: CityPhysicalNetworkPolicy
  latest_flow_tick?: number
  active_node_count: number
  active_edge_count: number
  component_count: number
  isolated_node_count: number
  service_island_count: number
  bottleneck_edge_count: number
  saturated_edge_count: number
  components: CityPhysicalNetworkComponentDiagnostic[]
  edge_diagnostics: CityPhysicalNetworkEdgeDiagnostic[]
  truncated_edge_diagnostic_count: number
  route?: CityPhysicalNetworkRouteDiagnostic
}

export interface CityPhysicalNetworkDiagnosticQuery {
  network: string
  source?: string
  sink?: string
  probe_units?: number
}

export interface CityPhysicalNetworkListQuery {
  service?: string
  network?: string
  status?: string
  role?: string
  phase?: string
  fact_type?: string
  after_code?: string
  after_tick?: number
  after_sequence?: number
  limit?: number
}

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

export interface CityCommandPage {
  items: CityCommand[]
  next_cursor?: number
}

export interface CityCommandQuery {
  status?: CityCommand['status']
  after_sequence?: number
  limit?: number
  latest?: boolean
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
  service_facts: CityServiceFact[]
  service_allocations: CityServiceAllocation[]
  service_settlements: CityServiceSettlement[]
  physical_network_facts: CityPhysicalNetworkFact[]
  physical_network_batches: CityPhysicalNetworkFlowBatch[]
  physical_network_paths: CityPhysicalNetworkFlowPath[]
  physical_network_segments: CityPhysicalNetworkFlowSegment[]
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

export async function getRealtimeWorldProjection(worldID: number): Promise<CityRealtimeWorldProjection> {
  const { data } = await apiClient.get<CityRealtimeWorldProjection>(`${worldPath(worldID)}/realtime/projection`)
  return data
}

export async function getRealtimeVisualManifest(worldID: number): Promise<CityRealtimeVisualManifest> {
  const { data } = await apiClient.get<CityRealtimeVisualManifest>(`${worldPath(worldID)}/realtime/visual-manifest`)
  return data
}

export async function getRealtimePixelChunk(
  worldID: number,
  chunkX: number,
  chunkY: number,
  z = 0
): Promise<CityRealtimePixelChunkProjection> {
  const { data } = await apiClient.get<CityRealtimePixelChunkProjection>(
    `${worldPath(worldID)}/realtime/pixel-chunks/${chunkX}/${chunkY}/${z}`
  )
  return data
}

export async function listRealtimePatches(
  worldID: number,
  afterFrameSequence: number,
  limit = 100
): Promise<CityRealtimePatchPage> {
  const { data } = await apiClient.get<CityRealtimePatchPage>(`${worldPath(worldID)}/realtime/patches`, {
    params: { after_frame_sequence: afterFrameSequence, limit }
  })
  return data
}

export async function getRealtimeActors(
  worldID: number,
  query: CityRealtimeActorSnapshotQuery
): Promise<CityRealtimeActorSnapshot> {
  const { data } = await apiClient.get<CityRealtimeActorSnapshot>(`${worldPath(worldID)}/realtime/actors`, {
    params: query
  })
  return data
}

export async function getRealtimeMyCharacter(worldID: number): Promise<CityRealtimeMyCharacterProjection> {
  const { data } = await apiClient.get<CityRealtimeMyCharacterProjection>(`${worldPath(worldID)}/realtime/character`)
  return data
}

export async function createRealtimeCharacter(
  worldID: number,
  request: CityRealtimeCreateCharacterRequest,
  key = idempotencyKey(`realtime-character-create-${worldID}`)
): Promise<CityRealtimeCharacterMutationResult> {
  const { data } = await apiClient.post<CityRealtimeCharacterMutationResult>(
    `${worldPath(worldID)}/realtime/character`,
    request,
    { headers: { 'Idempotency-Key': key } }
  )
  return data
}

export async function configureRealtimeCharacterAgent(
  worldID: number,
  request: CityRealtimeConfigureCharacterAgentRequest,
  key = idempotencyKey(`realtime-character-agent-configure-${worldID}`)
): Promise<CityRealtimeCharacterMutationResult> {
  const { data } = await apiClient.post<CityRealtimeCharacterMutationResult>(
    `${worldPath(worldID)}/realtime/character/agent`,
    request,
    { headers: { 'Idempotency-Key': key } }
  )
  return data
}

export async function moveRealtimeCharacter(
  worldID: number,
  request: CityRealtimeMoveCharacterRequest,
  key = idempotencyKey(`realtime-character-move-${worldID}`)
): Promise<CityRealtimeCharacterMutationResult> {
  const { data } = await apiClient.post<CityRealtimeCharacterMutationResult>(
    `${worldPath(worldID)}/realtime/character/move`,
    request,
    { headers: { 'Idempotency-Key': key } }
  )
  return data
}

export async function performRealtimeCharacterActivity(
  worldID: number,
  request: CityRealtimePerformCharacterActivityRequest,
  key = idempotencyKey(`realtime-character-activity-${worldID}-${request.activity_code}`)
): Promise<CityRealtimeCharacterMutationResult> {
  const { data } = await apiClient.post<CityRealtimeCharacterMutationResult>(
    `${worldPath(worldID)}/realtime/character/activities`,
    request,
    { headers: { 'Idempotency-Key': key } }
  )
  return data
}

export async function changeRealtimeCharacterRole(
  worldID: number,
  request: CityRealtimeChangeCharacterRoleRequest,
  key = idempotencyKey(`realtime-character-role-${worldID}-${request.role_code}`)
): Promise<CityRealtimeCharacterMutationResult> {
  const { data } = await apiClient.post<CityRealtimeCharacterMutationResult>(
    `${worldPath(worldID)}/realtime/character/roles`,
    request,
    { headers: { 'Idempotency-Key': key } }
  )
  return data
}

export async function listRealtimeMyCharacterEvents(
  worldID: number,
  query: { before_sequence?: number; limit?: number } = {}
): Promise<CityRealtimeCharacterEventPage> {
  const { data } = await apiClient.get<CityRealtimeCharacterEventPage>(
    `${worldPath(worldID)}/realtime/character/events`,
    { params: query }
  )
  return data
}

export async function listRealtimePublicCharacterEvents(
  worldID: number,
  query: { before_cursor?: string; limit?: number } = {}
): Promise<CityRealtimePublicCharacterEventPage> {
  const { data } = await apiClient.get<CityRealtimePublicCharacterEventPage>(
    `${worldPath(worldID)}/realtime/events`,
    { params: query }
  )
  return data
}

export async function getRealtimeClock(worldID: number): Promise<CityRealtimeClock> {
  const { data } = await apiClient.get<CityRealtimeClock>(`${worldPath(worldID)}/clock`)
  return data
}

function idempotencyKey(prefix: string): string {
  const id = globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random().toString(36).slice(2)}`
  return `${prefix}-${id}`
}

export async function listCityWorlds(): Promise<CityWorld[]> {
  const { data } = await apiClient.get<CityWorld[]>('/city/worlds')
  return data
}

export async function getCityWorld(worldID: number): Promise<CityWorld> {
  const { data } = await apiClient.get<CityWorld>(worldPath(worldID))
  return data
}

export async function listCityWorldMembers(worldID: number): Promise<CityMember[]> {
  const { data } = await apiClient.get<CityMember[]>(`${worldPath(worldID)}/members`)
  return data
}

export async function addCityWorldMember(
  worldID: number,
  request: AddCityWorldMemberRequest
): Promise<CityMember> {
  const { data } = await apiClient.post<CityMember>(`${worldPath(worldID)}/members`, request, {
    headers: { 'Idempotency-Key': idempotencyKey(`city-member-add-${worldID}`) }
  })
  return data
}

export async function updateCityWorldMember(
  worldID: number,
  userID: number,
  request: UpdateCityWorldMemberRequest
): Promise<CityMember> {
  const { data } = await apiClient.patch<CityMember>(
    `${worldPath(worldID)}/members/${userID}`,
    request,
    { headers: { 'Idempotency-Key': idempotencyKey(`city-member-update-${worldID}-${userID}`) } }
  )
  return data
}

export async function createCityWorld(request: CreateCityWorldRequest): Promise<CityWorldFoundation> {
  const { data } = await apiClient.post<CityWorldFoundation>('/city/worlds', request, {
    headers: { 'Idempotency-Key': idempotencyKey('city-world-create') }
  })
  return data
}

export async function getCitySpatialRuleSet(ruleSetID: string): Promise<CitySpatialRuleSet> {
  const { data } = await apiClient.get<CitySpatialRuleSet>(`/city/spatial/rule-sets/${encodeURIComponent(ruleSetID)}`)
  return data
}

export async function listOpenWorldStyleProfiles(): Promise<CityOpenWorldStyleProfile[]> {
  const { data } = await apiClient.get<CityOpenWorldStyleProfile[]>('/city/open-world/styles')
  return data
}

export async function getOpenWorldGeneration(worldID: number): Promise<CityOpenWorldGenerationState> {
	const { data } = await apiClient.get<CityOpenWorldGenerationState>(`${worldPath(worldID)}/open-world/generation`)
	return data
}

export async function traverseRealtimeCharacterPortal(
	worldID: number,
	request: CityRealtimeTraverseCharacterPortalRequest,
	key = idempotencyKey(`realtime-character-portal-${worldID}-${request.portal_code}`)
): Promise<CityRealtimeCharacterMutationResult> {
	const { data } = await apiClient.post<CityRealtimeCharacterMutationResult>(
		`${worldPath(worldID)}/realtime/character/portals`,
		request,
		{ headers: { 'Idempotency-Key': key } }
	)
	return data
}

export async function getOpenWorldVerification(
	worldID: number,
	region?: { region_x: number; region_y: number }
): Promise<CityOpenWorldVerification> {
	const { data } = await apiClient.get<CityOpenWorldVerification>(`${worldPath(worldID)}/open-world/verification`, {
		params: region
	})
	return data
}

export async function getOpenWorldMap(
  worldID: number,
  query: CityOpenWorldMapQuery
): Promise<CityOpenWorldMap> {
  const { data } = await apiClient.get<CityOpenWorldMap>(`${worldPath(worldID)}/open-world/map`, {
    params: query
  })
  return data
}

export async function getOpenWorldBuildingInterior(
  worldID: number,
  buildingCode: string,
  floorIndex: number
): Promise<CityOpenWorldBuildingInterior> {
  const { data } = await apiClient.get<CityOpenWorldBuildingInterior>(
    `${worldPath(worldID)}/open-world/buildings/${encodeURIComponent(buildingCode)}/interiors/${floorIndex}`
  )
  return data
}

export async function listOpenWorldBuildingPortals(
  worldID: number,
  buildingCode: string
): Promise<CityOpenWorldPortal[]> {
  const { data } = await apiClient.get<CityOpenWorldPortal[]>(
    `${worldPath(worldID)}/open-world/buildings/${encodeURIComponent(buildingCode)}/portals`
  )
  return data
}

export async function submitOpenWorldSectorMaterialization(
  worldID: number,
  sectorX: number,
  sectorY: number,
  expectedWorldTick: number
): Promise<CityCommand> {
  const { data } = await apiClient.post<CityCommand>(
    `${worldPath(worldID)}/commands`,
    {
      command_type: 'open_world.sector.materialize',
      payload: { sector_x: sectorX, sector_y: sectorY },
      expected_world_tick: expectedWorldTick
    },
    {
      headers: {
        'Idempotency-Key': idempotencyKey(`open-world-sector-${worldID}-${sectorX}-${sectorY}`)
      }
    }
  )
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

export async function getCityServiceCatalog(worldID: number): Promise<CityServiceCatalogView> {
  const { data } = await apiClient.get<CityServiceCatalogView>(`${worldPath(worldID)}/services/catalog`)
  return data
}

export async function listCityServiceFacilities(
  worldID: number,
  query: CityServiceListQuery = {}
): Promise<CityServiceFacilityPage> {
  const { data } = await apiClient.get<CityServiceFacilityPage>(
    `${worldPath(worldID)}/services/facilities`,
    { params: { limit: 200, ...query } }
  )
  return data
}

export async function listCityServiceDemands(
  worldID: number,
  query: CityServiceListQuery = {}
): Promise<CityServiceDemandPage> {
  const { data } = await apiClient.get<CityServiceDemandPage>(
    `${worldPath(worldID)}/services/demands`,
    { params: { limit: 200, ...query } }
  )
  return data
}

export async function listCityServiceConnections(
  worldID: number,
  query: CityServiceListQuery = {}
): Promise<CityServiceConnectionPage> {
  const { data } = await apiClient.get<CityServiceConnectionPage>(
    `${worldPath(worldID)}/services/connections`,
    { params: { limit: 200, ...query } }
  )
  return data
}

export async function listCityServiceSettlements(
  worldID: number,
  query: CityServiceListQuery = {}
): Promise<CityServiceSettlementPage> {
  const { data } = await apiClient.get<CityServiceSettlementPage>(
    `${worldPath(worldID)}/services/settlements`,
    { params: { after_tick: 0, after_sequence: 0, limit: 200, ...query } }
  )
  return data
}

export async function getCityPhysicalNetworkCatalog(
  worldID: number
): Promise<CityPhysicalNetworkCatalogView> {
  const { data } = await apiClient.get<CityPhysicalNetworkCatalogView>(
    `${worldPath(worldID)}/services/networks/catalog`
  )
  return data
}

export async function listCityPhysicalNetworks(
  worldID: number,
  query: CityPhysicalNetworkListQuery = {}
): Promise<CityPhysicalNetworkPage> {
  const { data } = await apiClient.get<CityPhysicalNetworkPage>(
    `${worldPath(worldID)}/services/networks`,
    { params: { limit: 200, ...query } }
  )
  return data
}

export async function listCityPhysicalNetworkNodes(
  worldID: number,
  query: CityPhysicalNetworkListQuery = {}
): Promise<CityPhysicalNetworkNodePage> {
  const { data } = await apiClient.get<CityPhysicalNetworkNodePage>(
    `${worldPath(worldID)}/services/networks/nodes`,
    { params: { limit: 200, ...query } }
  )
  return data
}

export async function listCityPhysicalNetworkEdges(
  worldID: number,
  query: CityPhysicalNetworkListQuery = {}
): Promise<CityPhysicalNetworkEdgePage> {
  const { data } = await apiClient.get<CityPhysicalNetworkEdgePage>(
    `${worldPath(worldID)}/services/networks/edges`,
    { params: { limit: 200, ...query } }
  )
  return data
}

export async function listCityPhysicalNetworkFlows(
  worldID: number,
  query: CityPhysicalNetworkListQuery = {}
): Promise<CityPhysicalNetworkFlowPage> {
  const { data } = await apiClient.get<CityPhysicalNetworkFlowPage>(
    `${worldPath(worldID)}/services/networks/flows`,
    { params: { after_tick: 0, after_sequence: 0, limit: 100, ...query } }
  )
  return data
}

export async function listCityPhysicalNetworkFacts(
  worldID: number,
  query: CityPhysicalNetworkListQuery = {}
): Promise<CityPhysicalNetworkFactPage> {
  const { data } = await apiClient.get<CityPhysicalNetworkFactPage>(
    `${worldPath(worldID)}/services/networks/facts`,
    { params: { after_tick: 0, after_sequence: 0, limit: 100, ...query } }
  )
  return data
}

export async function getCityPhysicalNetworkDiagnostics(
  worldID: number,
  query: CityPhysicalNetworkDiagnosticQuery
): Promise<CityPhysicalNetworkDiagnosticsView> {
  const { data } = await apiClient.get<CityPhysicalNetworkDiagnosticsView>(
    `${worldPath(worldID)}/services/networks/diagnostics`,
    { params: query }
  )
  return data
}

export async function submitCityServiceCommand(
  worldID: number,
  commandType: CityServiceCommandType,
  payload: Record<string, unknown>,
  expectedWorldTick: number
): Promise<CityCommand> {
  const { data } = await apiClient.post<CityCommand>(
    `${worldPath(worldID)}/commands`,
    { command_type: commandType, payload, expected_world_tick: expectedWorldTick },
    {
      headers: {
        'Idempotency-Key': idempotencyKey(`city-service-${worldID}-${commandType}`)
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

export async function findWorldActorPath(
  worldID: number,
  actorCode: string,
  destination: CityNavigationCoordinate,
  maxSteps = 256
): Promise<CityNavigationPath> {
  const { data } = await apiClient.post<CityNavigationPath>(
    `${worldPath(worldID)}/navigation/path`,
    { actor_code: actorCode, destination, max_steps: maxSteps }
  )
  return data
}

export async function listWorldPortalStates(
  worldID: number,
  actorCode?: string
): Promise<WorldPortalAccessView[]> {
  const { data } = await apiClient.get<WorldPortalAccessView[]>(
    `${worldPath(worldID)}/navigation/portals`,
    { params: actorCode ? { actor_code: actorCode } : undefined }
  )
  return data
}

export async function listWorldNavigationIntents(
  worldID: number
): Promise<WorldActorNavigationIntent[]> {
  const { data } = await apiClient.get<WorldActorNavigationIntent[]>(
    `${worldPath(worldID)}/navigation/intents`
  )
  return data
}

export async function getWorldNavigationIntent(
  worldID: number,
  actorCode: string
): Promise<WorldActorNavigationIntent> {
  const { data } = await apiClient.get<WorldActorNavigationIntent>(
    `${worldPath(worldID)}/navigation/intents/${encodeURIComponent(actorCode)}`
  )
  return data
}

export async function listWorldNavigationReservations(
  worldID: number,
  tick?: number
): Promise<WorldNavigationReservation[]> {
  const { data } = await apiClient.get<WorldNavigationReservation[]>(
    `${worldPath(worldID)}/navigation/reservations`,
    { params: tick === undefined ? undefined : { tick } }
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
  commandType: CityRuntimeCommandType,
  payload: Record<string, unknown>,
  expectedWorldTick?: number
): Promise<CityCommand> {
  const { data } = await apiClient.post<CityCommand>(
    `${worldPath(worldID)}/commands`,
    {
      command_type: commandType,
      payload,
      ...(expectedWorldTick === undefined ? {} : { expected_world_tick: expectedWorldTick })
    },
    {
      headers: {
        'Idempotency-Key': idempotencyKey(`world-runtime-${worldID}-${commandType}`)
      }
    }
  )
  return data
}

export async function submitWorldControlCommand(
  worldID: number,
  commandType: CityWorldControlCommandType,
  payload: Record<string, unknown>,
  expectedWorldTick?: number
): Promise<CityCommand> {
  const { data } = await apiClient.post<CityCommand>(
    `${worldPath(worldID)}/commands`,
    {
      command_type: commandType,
      payload,
      ...(expectedWorldTick === undefined ? {} : { expected_world_tick: expectedWorldTick })
    },
    {
      headers: {
        'Idempotency-Key': idempotencyKey(`world-control-${worldID}-${commandType}`)
      }
    }
  )
  return data
}

export async function getCityCommand(worldID: number, commandID: number): Promise<CityCommand> {
  const { data } = await apiClient.get<CityCommand>(`${worldPath(worldID)}/commands/${commandID}`)
  return data
}

export async function listCityCommands(
  worldID: number,
  query: CityCommandQuery = {}
): Promise<CityCommandPage> {
  const { data } = await apiClient.get<CityCommandPage>(`${worldPath(worldID)}/commands`, {
    params: { after_sequence: 0, limit: 100, ...query }
  })
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
  getWorld: getCityWorld,
  createWorld: createCityWorld,
  getRealtimeWorldProjection,
  getRealtimePixelChunk,
  listRealtimePatches,
  getRealtimeMyCharacter,
	createRealtimeCharacter,
	configureRealtimeCharacterAgent,
	moveRealtimeCharacter,
	traverseRealtimeCharacterPortal,
  performRealtimeCharacterActivity,
  changeRealtimeCharacterRole,
  listRealtimeMyCharacterEvents,
  listRealtimePublicCharacterEvents,
  getRealtimeClock,
  getSpatialRuleSet: getCitySpatialRuleSet,
	listOpenWorldStyleProfiles,
	getOpenWorldGeneration,
	getOpenWorldVerification,
	getOpenWorldMap,
  submitOpenWorldSectorMaterialization,
  listWorldMembers: listCityWorldMembers,
  addWorldMember: addCityWorldMember,
  updateWorldMember: updateCityWorldMember,
  getWorldSpatialRuleSet: getCityWorldSpatialRuleSet,
  getOvermap: getCityOvermap,
  getLandState: getCityLandState,
  getDevelopmentState: getCityDevelopmentState,
  getEnterpriseLocationState: getCityEnterpriseLocationState,
  getServiceCatalog: getCityServiceCatalog,
  listServiceFacilities: listCityServiceFacilities,
  listServiceDemands: listCityServiceDemands,
  listServiceConnections: listCityServiceConnections,
  listServiceSettlements: listCityServiceSettlements,
  getPhysicalNetworkCatalog: getCityPhysicalNetworkCatalog,
  listPhysicalNetworks: listCityPhysicalNetworks,
  listPhysicalNetworkNodes: listCityPhysicalNetworkNodes,
  listPhysicalNetworkEdges: listCityPhysicalNetworkEdges,
  listPhysicalNetworkFlows: listCityPhysicalNetworkFlows,
  listPhysicalNetworkFacts: listCityPhysicalNetworkFacts,
  getPhysicalNetworkDiagnostics: getCityPhysicalNetworkDiagnostics,
  submitServiceCommand: submitCityServiceCommand,
  listMapChunks: listCityMapChunks,
  getMapChunk: getCityMapChunk,
  listSpatialChanges: listCitySpatialChanges,
  submitGenerateChunk: submitGenerateCityChunk,
  submitDevelopmentCommand: submitCityDevelopmentCommand,
  submitEnterpriseLocationCommand: submitCityEnterpriseLocationCommand,
  getWorldRuntimeCatalog,
  listWorldActors,
  getWorldActorState,
  findWorldActorPath,
  listWorldPortalStates,
  listWorldNavigationIntents,
  getWorldNavigationIntent,
  listWorldNavigationReservations,
  getWorldActorRoleOptions,
  listWorldRuntimeRules,
  listWorldRuleCases,
  submitWorldRuntimeCommand,
  submitWorldControlCommand,
  getCommand: getCityCommand,
  listCommands: listCityCommands,
  stepWorld: stepCityWorld
}

export default citySpatialAPI
