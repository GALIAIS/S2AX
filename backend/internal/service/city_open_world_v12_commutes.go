package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

const (
	cityOpenWorldCommuteSchemaVersion                     = 1
	cityOpenWorldCommuteProfileID                         = "sub2api-open-world-commute-binding"
	cityOpenWorldCommuteProfileVersion                    = "1.0.0"
	cityOpenWorldCommuteAssignmentContract                = "deterministic_capacity_residence_assignment_v1"
	cityOpenWorldCommuteBindingKindNPCResidenceWork       = "npc.residence_employment"
	cityOpenWorldCommuteBindingStatusActive               = "active"
	cityOpenWorldCommutePeriodTicks                 int64 = 24
	cityOpenWorldCommuteMaximumBindings                   = 4096
)

// CityOpenWorldCommutePolicy seals the V12 bridge from V5 social identity to
// future commute demand. It contains no live routing state: V9/V10/V11 stay
// the only owners of routes, arrival, and automatic demand lifecycle.
type CityOpenWorldCommutePolicy struct {
	ProfileID             string          `json:"profile_id"`
	ProfileVersion        string          `json:"profile_version"`
	ContentHash           string          `json:"content_hash"`
	BaselineTick          int64           `json:"baseline_tick"`
	AssignmentContract    string          `json:"assignment_contract"`
	PeriodTicks           int64           `json:"period_ticks"`
	MaximumBindings       int             `json:"maximum_bindings"`
	CandidateCount        int64           `json:"candidate_count"`
	BindingCount          int64           `json:"binding_count"`
	UnboundCandidateCount int64           `json:"unbound_candidate_count"`
	ResidenceCount        int64           `json:"residence_count"`
	UsedResidenceUnits    int64           `json:"used_residence_units"`
	Revision              int64           `json:"revision"`
	Metadata              json.RawMessage `json:"metadata"`
}

// CityOpenWorldCommuteBinding is an immutable, capacity-limited residence and
// employment relationship. It intentionally records codes rather than live
// coordinates; a later source must verify its actual local origin separately.
type CityOpenWorldCommuteBinding struct {
	Code             string          `json:"code"`
	BindingKind      string          `json:"binding_kind"`
	ActorCode        string          `json:"actor_code"`
	EmploymentRole   string          `json:"employment_role_code"`
	HomeFacilityCode string          `json:"home_facility_code"`
	HomeHubCode      string          `json:"home_hub_code"`
	WorkFacilityCode string          `json:"work_facility_code"`
	WorkHubCode      string          `json:"work_hub_code"`
	PeriodTicks      int64           `json:"period_ticks"`
	OutboundPhase    int64           `json:"outbound_phase"`
	ReturnPhase      int64           `json:"return_phase"`
	Status           string          `json:"status"`
	Version          int64           `json:"version"`
	Metadata         json.RawMessage `json:"metadata"`
}

// CityOpenWorldCommuteState is the V12 canonical foundation. Bindings are
// immutable in V12; lifecycle/rebinding must arrive through a later engine
// version with explicit facts instead of an UPDATE escape hatch.
type CityOpenWorldCommuteState struct {
	Policy   CityOpenWorldCommutePolicy    `json:"policy"`
	Bindings []CityOpenWorldCommuteBinding `json:"bindings"`
}

type cityOpenWorldCommuteCandidate struct {
	actorID          int64
	actorCode        string
	employmentRole   string
	workFacilityCode string
	workHubCode      string
	preferredHome    *string
	preferredHomeHub *string
	scheduleOffset   int64
}

type cityOpenWorldCommuteResidence struct {
	facilityCode string
	hubCode      string
	capacity     int64
	used         int64
}

func cityOpenWorldCommuteBindingCode(actorCode string) string {
	sum := sha256.Sum256([]byte("npc.residence_employment\x00" + actorCode))
	return "commute.binding." + hex.EncodeToString(sum[:20])
}

func cityOpenWorldCommuteResidenceRank(actorCode, facilityCode string) string {
	sum := sha256.Sum256([]byte("commute.residence.v1\x00" + actorCode + "\x00" + facilityCode))
	return hex.EncodeToString(sum[:])
}

func cityOpenWorldCommutePolicyHash(
	assignmentContract string,
	periodTicks int64,
	maximumBindings int,
) (string, error) {
	raw, err := json.Marshal(struct {
		SchemaVersion      int    `json:"schema_version"`
		ProfileID          string `json:"profile_id"`
		ProfileVersion     string `json:"profile_version"`
		AssignmentContract string `json:"assignment_contract"`
		PeriodTicks        int64  `json:"period_ticks"`
		MaximumBindings    int    `json:"maximum_bindings"`
	}{
		SchemaVersion: cityOpenWorldCommuteSchemaVersion,
		ProfileID:     cityOpenWorldCommuteProfileID, ProfileVersion: cityOpenWorldCommuteProfileVersion,
		AssignmentContract: assignmentContract, PeriodTicks: periodTicks,
		MaximumBindings: maximumBindings,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func activateCityOpenWorldCommuteBootstrapWrite(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if _, err := tx.ExecContext(ctx, `
SELECT set_config('sub2api.city_open_world_commute_bootstrap_world_id', $1, TRUE)`, fmt.Sprintf("%d", worldID)); err != nil {
		return fmt.Errorf("enable open-world V12 commute bootstrap: %w", err)
	}
	return nil
}

// initializeCityOpenWorldV12CommuteFoundation creates a strictly bounded
// bridge from the V5 actor/facility contract. It does not mutate V5 profiles
// and never derives a home from the current position or work assignment.
func initializeCityOpenWorldV12CommuteFoundation(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if worldID <= 0 {
		return ErrCityInvalidInput
	}
	var simulationVersion string
	var baselineTick int64
	if err := tx.QueryRowContext(ctx, `
SELECT simulation_version, current_tick
FROM city_worlds WHERE id = $1 FOR UPDATE`, worldID).Scan(&simulationVersion, &baselineTick); err != nil {
		return fmt.Errorf("load V12 commute world: %w", err)
	}
	if !cityEngineSupportsOpenWorldCommuteBindings(simulationVersion) {
		return ErrCitySimulationVersion.WithMetadata(map[string]string{"version": simulationVersion})
	}
	if _, err := tx.ExecContext(ctx, `SELECT assert_city_open_world_mobility_od_foundation($1)`, worldID); err != nil {
		return fmt.Errorf("validate V12 commute OD prerequisite: %w", err)
	}
	if err := activateCityOpenWorldCommuteBootstrapWrite(ctx, tx, worldID); err != nil {
		return err
	}
	candidates, err := loadCityOpenWorldCommuteCandidates(ctx, tx, worldID)
	if err != nil {
		return err
	}
	residences, err := loadCityOpenWorldCommuteResidences(ctx, tx, worldID)
	if err != nil {
		return err
	}
	contentHash, err := cityOpenWorldCommutePolicyHash(
		cityOpenWorldCommuteAssignmentContract, cityOpenWorldCommutePeriodTicks, cityOpenWorldCommuteMaximumBindings,
	)
	if err != nil {
		return fmt.Errorf("hash V12 commute profile: %w", err)
	}
	bindings, unboundCount := cityOpenWorldCommuteBindingsForCandidates(candidates, residences)
	metadata, err := json.Marshal(map[string]any{
		"schema_version":     1,
		"candidate_adapter":  "v5_npc_employment_role_and_facility_v1",
		"home_assignment":    "prefer_v5_home_then_capacity_rank_v1",
		"future_source_gate": "verified_building_origin_v1",
	})
	if err != nil {
		return fmt.Errorf("marshal V12 commute profile metadata: %w", err)
	}
	policy := CityOpenWorldCommutePolicy{
		ProfileID: cityOpenWorldCommuteProfileID, ProfileVersion: cityOpenWorldCommuteProfileVersion,
		ContentHash: contentHash, BaselineTick: baselineTick,
		AssignmentContract: cityOpenWorldCommuteAssignmentContract,
		PeriodTicks:        cityOpenWorldCommutePeriodTicks, MaximumBindings: cityOpenWorldCommuteMaximumBindings,
		CandidateCount: int64(len(candidates)), BindingCount: int64(len(bindings)),
		UnboundCandidateCount: unboundCount, ResidenceCount: int64(len(residences)),
		UsedResidenceUnits: int64(len(bindings)), Revision: 1, Metadata: metadata,
	}
	if err = validateCityOpenWorldCommuteState(&CityOpenWorldCommuteState{Policy: policy, Bindings: bindings}); err != nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v12_commute"}).WithCause(err)
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_commute_profiles
    (world_id, profile_id, profile_version, content_hash, baseline_tick,
     assignment_contract, period_ticks, maximum_bindings, candidate_count,
     binding_count, unbound_candidate_count, residence_count,
     used_residence_units, revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15::jsonb)`,
		worldID, policy.ProfileID, policy.ProfileVersion, policy.ContentHash, policy.BaselineTick,
		policy.AssignmentContract, policy.PeriodTicks, policy.MaximumBindings, policy.CandidateCount,
		policy.BindingCount, policy.UnboundCandidateCount, policy.ResidenceCount,
		policy.UsedResidenceUnits, policy.Revision, []byte(policy.Metadata)); err != nil {
		return fmt.Errorf("insert V12 commute profile: %w", err)
	}
	candidateByActor := make(map[string]cityOpenWorldCommuteCandidate, len(candidates))
	for _, candidate := range candidates {
		candidateByActor[candidate.actorCode] = candidate
	}
	for _, binding := range bindings {
		candidate, found := candidateByActor[binding.ActorCode]
		if !found {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v12_commute_candidate"})
		}
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_commute_bindings
    (world_id, code, binding_kind, actor_id, employment_role_code,
     home_facility_code, home_hub_code, work_facility_code, work_hub_code,
     period_ticks, outbound_phase, return_phase, status, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15::jsonb)`,
			worldID, binding.Code, binding.BindingKind, candidate.actorID, binding.EmploymentRole,
			binding.HomeFacilityCode, binding.HomeHubCode, binding.WorkFacilityCode, binding.WorkHubCode,
			binding.PeriodTicks, binding.OutboundPhase, binding.ReturnPhase, binding.Status,
			binding.Version, []byte(binding.Metadata)); err != nil {
			return fmt.Errorf("insert V12 commute binding %s: %w", binding.Code, err)
		}
	}
	if _, err = tx.ExecContext(ctx, `SELECT assert_city_open_world_commute_foundation($1)`, worldID); err != nil {
		return fmt.Errorf("validate V12 commute foundation: %w", err)
	}
	return nil
}

func loadCityOpenWorldCommuteCandidates(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) ([]cityOpenWorldCommuteCandidate, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT DISTINCT ON (actor.id)
       actor.id, actor.code, role.role_code, work.code, work_hub.code,
       home.code, home_hub.code, profile.schedule_offset
FROM city_open_world_npc_profiles profile
JOIN city_open_world_actors actor
  ON actor.id = profile.actor_id AND actor.world_id = profile.world_id
JOIN city_open_world_actor_roles role
  ON role.actor_id = actor.id AND role.world_id = actor.world_id
 AND role.status = 'active' AND role.category_code = 'employment'
JOIN city_open_world_facilities work
  ON work.id = profile.work_facility_id AND work.world_id = profile.world_id
 AND work.state = 'active' AND work.facility_type_code <> 'residence'
JOIN city_open_world_mobility_hubs work_hub
  ON work_hub.world_id = work.world_id AND work_hub.facility_id = work.id
 AND work_hub.facility_code = work.code AND work_hub.hub_kind = 'facility'
LEFT JOIN city_open_world_facilities home
  ON home.id = profile.home_facility_id AND home.world_id = profile.world_id
 AND home.state = 'active' AND home.facility_type_code = 'residence'
LEFT JOIN city_open_world_mobility_hubs home_hub
  ON home_hub.world_id = home.world_id AND home_hub.facility_id = home.id
 AND home_hub.facility_code = home.code AND home_hub.hub_kind = 'facility'
WHERE profile.world_id = $1
  AND actor.status = 'active'
  AND profile.lod_tier <> 'dormant'
ORDER BY actor.id, role.role_code`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V12 commute candidates: %w", err)
	}
	items := make([]cityOpenWorldCommuteCandidate, 0)
	for rows.Next() {
		item := cityOpenWorldCommuteCandidate{}
		var homeCode, homeHub sql.NullString
		if err = rows.Scan(&item.actorID, &item.actorCode, &item.employmentRole,
			&item.workFacilityCode, &item.workHubCode, &homeCode, &homeHub, &item.scheduleOffset); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan V12 commute candidate: %w", err)
		}
		if homeCode.Valid && homeHub.Valid {
			item.preferredHome = cityOpenWorldStringPointer(homeCode.String)
			item.preferredHomeHub = cityOpenWorldStringPointer(homeHub.String)
		}
		items = append(items, item)
	}
	if err = closeCityRows(rows, "iterate V12 commute candidates"); err != nil {
		return nil, err
	}
	sort.Slice(items, func(left, right int) bool { return items[left].actorCode < items[right].actorCode })
	return items, nil
}

func loadCityOpenWorldCommuteResidences(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) ([]cityOpenWorldCommuteResidence, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT facility.code, hub.code, facility.capacity_units
FROM city_open_world_facilities facility
JOIN city_open_world_mobility_hubs hub
  ON hub.world_id = facility.world_id AND hub.facility_id = facility.id
 AND hub.facility_code = facility.code AND hub.hub_kind = 'facility'
WHERE facility.world_id = $1
  AND facility.state = 'active'
  AND facility.facility_type_code = 'residence'
ORDER BY facility.code`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V12 commute residences: %w", err)
	}
	items := make([]cityOpenWorldCommuteResidence, 0)
	for rows.Next() {
		item := cityOpenWorldCommuteResidence{}
		if err = rows.Scan(&item.facilityCode, &item.hubCode, &item.capacity); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan V12 commute residence: %w", err)
		}
		if item.capacity < 1 {
			_ = rows.Close()
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v12_residence_capacity"})
		}
		items = append(items, item)
	}
	if err = closeCityRows(rows, "iterate V12 commute residences"); err != nil {
		return nil, err
	}
	return items, nil
}

func cityOpenWorldCommuteBindingsForCandidates(
	candidates []cityOpenWorldCommuteCandidate,
	residences []cityOpenWorldCommuteResidence,
) ([]CityOpenWorldCommuteBinding, int64) {
	byCode := make(map[string]int, len(residences))
	for index := range residences {
		byCode[residences[index].facilityCode] = index
	}
	bindings := make([]CityOpenWorldCommuteBinding, 0, len(candidates))
	var unbound int64
	for _, candidate := range candidates {
		if len(bindings) >= cityOpenWorldCommuteMaximumBindings {
			unbound++
			continue
		}
		selected := -1
		if candidate.preferredHome != nil && candidate.preferredHomeHub != nil {
			if index, found := byCode[*candidate.preferredHome]; found &&
				residences[index].hubCode == *candidate.preferredHomeHub &&
				residences[index].used < residences[index].capacity {
				selected = index
			}
		}
		if selected < 0 {
			indexes := make([]int, 0, len(residences))
			for index := range residences {
				if residences[index].used < residences[index].capacity {
					indexes = append(indexes, index)
				}
			}
			sort.Slice(indexes, func(left, right int) bool {
				leftResidence, rightResidence := residences[indexes[left]], residences[indexes[right]]
				leftRank := cityOpenWorldCommuteResidenceRank(candidate.actorCode, leftResidence.facilityCode)
				rightRank := cityOpenWorldCommuteResidenceRank(candidate.actorCode, rightResidence.facilityCode)
				if leftRank == rightRank {
					return leftResidence.facilityCode < rightResidence.facilityCode
				}
				return leftRank < rightRank
			})
			if len(indexes) > 0 {
				selected = indexes[0]
			}
		}
		if selected < 0 {
			unbound++
			continue
		}
		residence := &residences[selected]
		residence.used++
		outbound := candidate.scheduleOffset % cityOpenWorldCommutePeriodTicks
		if outbound < 0 {
			outbound += cityOpenWorldCommutePeriodTicks
		}
		assignment := "capacity_rank"
		if candidate.preferredHome != nil && *candidate.preferredHome == residence.facilityCode {
			assignment = "v5_home"
		}
		metadata, err := json.Marshal(map[string]any{
			"schema_version":      1,
			"assignment":          assignment,
			"home_capacity_units": residence.capacity,
		})
		if err != nil {
			// map literals here are static and JSON cannot fail. A panic would be
			// worse than treating this candidate as explicitly unbound.
			unbound++
			residence.used--
			continue
		}
		bindings = append(bindings, CityOpenWorldCommuteBinding{
			Code:        cityOpenWorldCommuteBindingCode(candidate.actorCode),
			BindingKind: cityOpenWorldCommuteBindingKindNPCResidenceWork,
			ActorCode:   candidate.actorCode, EmploymentRole: candidate.employmentRole,
			HomeFacilityCode: residence.facilityCode, HomeHubCode: residence.hubCode,
			WorkFacilityCode: candidate.workFacilityCode, WorkHubCode: candidate.workHubCode,
			PeriodTicks: cityOpenWorldCommutePeriodTicks, OutboundPhase: outbound,
			ReturnPhase: (outbound + cityOpenWorldCommutePeriodTicks/2) % cityOpenWorldCommutePeriodTicks,
			Status:      cityOpenWorldCommuteBindingStatusActive, Version: 1, Metadata: metadata,
		})
	}
	sortCityOpenWorldCommuteBindings(bindings)
	return bindings, unbound
}

func validateCityOpenWorldCommutePolicy(policy CityOpenWorldCommutePolicy) error {
	expectedHash, err := cityOpenWorldCommutePolicyHash(
		policy.AssignmentContract, policy.PeriodTicks, policy.MaximumBindings,
	)
	if err != nil {
		return err
	}
	if policy.ProfileID != cityOpenWorldCommuteProfileID ||
		policy.ProfileVersion != cityOpenWorldCommuteProfileVersion ||
		policy.ContentHash != expectedHash || policy.BaselineTick < 0 ||
		policy.AssignmentContract != cityOpenWorldCommuteAssignmentContract ||
		policy.PeriodTicks != cityOpenWorldCommutePeriodTicks ||
		policy.MaximumBindings != cityOpenWorldCommuteMaximumBindings ||
		policy.CandidateCount < 0 || policy.BindingCount < 0 ||
		policy.UnboundCandidateCount < 0 || policy.ResidenceCount < 0 ||
		policy.UsedResidenceUnits < 0 || policy.BindingCount > int64(policy.MaximumBindings) ||
		policy.CandidateCount != policy.BindingCount+policy.UnboundCandidateCount ||
		policy.UsedResidenceUnits != policy.BindingCount || policy.Revision != 1 ||
		!json.Valid(policy.Metadata) {
		return errors.New("invalid V12 commute policy")
	}
	return nil
}

func validateCityOpenWorldCommuteState(state *CityOpenWorldCommuteState) error {
	if state == nil {
		return errors.New("missing V12 commute state")
	}
	if err := validateCityOpenWorldCommutePolicy(state.Policy); err != nil {
		return err
	}
	if state.Policy.BindingCount != int64(len(state.Bindings)) {
		return errors.New("V12 commute binding count is inconsistent")
	}
	seenCodes := make(map[string]struct{}, len(state.Bindings))
	seenActors := make(map[string]struct{}, len(state.Bindings))
	for _, binding := range state.Bindings {
		if !worldRuntimeCodeValid(binding.Code, 160) ||
			binding.BindingKind != cityOpenWorldCommuteBindingKindNPCResidenceWork ||
			!worldRuntimeCodeValid(binding.ActorCode, 128) ||
			!worldRuntimeCodeValid(binding.EmploymentRole, 96) ||
			!worldRuntimeCodeValid(binding.HomeFacilityCode, 160) ||
			!worldRuntimeCodeValid(binding.HomeHubCode, 160) ||
			!worldRuntimeCodeValid(binding.WorkFacilityCode, 160) ||
			!worldRuntimeCodeValid(binding.WorkHubCode, 160) ||
			binding.HomeFacilityCode == binding.WorkFacilityCode ||
			binding.PeriodTicks != state.Policy.PeriodTicks ||
			binding.OutboundPhase < 0 || binding.OutboundPhase >= binding.PeriodTicks ||
			binding.ReturnPhase != (binding.OutboundPhase+binding.PeriodTicks/2)%binding.PeriodTicks ||
			binding.Status != cityOpenWorldCommuteBindingStatusActive || binding.Version != 1 ||
			!json.Valid(binding.Metadata) {
			return fmt.Errorf("invalid V12 commute binding %s", binding.Code)
		}
		if _, duplicate := seenCodes[binding.Code]; duplicate {
			return fmt.Errorf("duplicate V12 commute binding %s", binding.Code)
		}
		if _, duplicate := seenActors[binding.ActorCode]; duplicate {
			return fmt.Errorf("duplicate V12 commute actor %s", binding.ActorCode)
		}
		seenCodes[binding.Code] = struct{}{}
		seenActors[binding.ActorCode] = struct{}{}
	}
	return nil
}

func loadCityOpenWorldCommuteState(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*CityOpenWorldCommuteState, error) {
	state := &CityOpenWorldCommuteState{Bindings: make([]CityOpenWorldCommuteBinding, 0)}
	err := queryer.QueryRowContext(ctx, `
SELECT profile_id, profile_version, content_hash, baseline_tick,
       assignment_contract, period_ticks, maximum_bindings, candidate_count,
       binding_count, unbound_candidate_count, residence_count,
       used_residence_units, revision, metadata
FROM city_open_world_commute_profiles
WHERE world_id = $1`, worldID).Scan(
		&state.Policy.ProfileID, &state.Policy.ProfileVersion, &state.Policy.ContentHash,
		&state.Policy.BaselineTick, &state.Policy.AssignmentContract, &state.Policy.PeriodTicks,
		&state.Policy.MaximumBindings, &state.Policy.CandidateCount, &state.Policy.BindingCount,
		&state.Policy.UnboundCandidateCount, &state.Policy.ResidenceCount,
		&state.Policy.UsedResidenceUnits, &state.Policy.Revision, &state.Policy.Metadata,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v12_commute_profile"})
	}
	if err != nil {
		return nil, fmt.Errorf("load V12 commute profile: %w", err)
	}
	rows, err := queryer.QueryContext(ctx, `
SELECT binding.code, binding.binding_kind, actor.code, binding.employment_role_code,
       binding.home_facility_code, binding.home_hub_code, binding.work_facility_code,
       binding.work_hub_code, binding.period_ticks, binding.outbound_phase,
       binding.return_phase, binding.status, binding.version, binding.metadata
FROM city_open_world_commute_bindings binding
JOIN city_open_world_actors actor
  ON actor.id = binding.actor_id AND actor.world_id = binding.world_id
WHERE binding.world_id = $1
ORDER BY binding.code`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V12 commute bindings: %w", err)
	}
	for rows.Next() {
		binding := CityOpenWorldCommuteBinding{}
		if err = rows.Scan(&binding.Code, &binding.BindingKind, &binding.ActorCode,
			&binding.EmploymentRole, &binding.HomeFacilityCode, &binding.HomeHubCode,
			&binding.WorkFacilityCode, &binding.WorkHubCode, &binding.PeriodTicks,
			&binding.OutboundPhase, &binding.ReturnPhase, &binding.Status,
			&binding.Version, &binding.Metadata); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan V12 commute binding: %w", err)
		}
		state.Bindings = append(state.Bindings, binding)
	}
	if err = closeCityRows(rows, "iterate V12 commute bindings"); err != nil {
		return nil, err
	}
	sortCityOpenWorldCommuteBindings(state.Bindings)
	if err = validateCityOpenWorldCommuteState(state); err != nil {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v12_commute"}).WithCause(err)
	}
	return state, nil
}

// GetCityOpenWorldCommuteState follows the existing source privacy contract:
// aggregate profile counters are world-readable, while binding identities are
// filtered to controlled actors unless the caller has full-world access.
func (s *CityEconomyService) GetCityOpenWorldCommuteState(
	ctx context.Context,
	userID, worldID int64,
) (*CityOpenWorldCommuteState, error) {
	if userID <= 0 || worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, userID, worldID); err != nil {
		return nil, err
	}
	var version string
	if err := s.db.QueryRowContext(ctx, `SELECT simulation_version FROM city_worlds WHERE id = $1`, worldID).Scan(&version); err != nil {
		return nil, fmt.Errorf("load V12 commute world version: %w", err)
	}
	if !cityEngineSupportsOpenWorldCommuteBindings(version) {
		return nil, ErrCitySimulationVersion.WithMetadata(map[string]string{"version": version})
	}
	state, err := loadCityOpenWorldCommuteState(ctx, s.db, worldID)
	if err != nil {
		return nil, err
	}
	all, err := cityOpenWorldServiceMayReadAll(ctx, s.db, userID, worldID)
	if err != nil || all {
		return state, err
	}
	visible, err := cityOpenWorldServiceVisibleActorCodes(ctx, s.db, userID, worldID)
	if err != nil {
		return nil, err
	}
	filtered := make([]CityOpenWorldCommuteBinding, 0, len(state.Bindings))
	for _, binding := range state.Bindings {
		if _, found := visible[binding.ActorCode]; found {
			filtered = append(filtered, binding)
		}
	}
	state.Bindings = filtered
	return state, nil
}

func sortCityOpenWorldCommuteBindings(items []CityOpenWorldCommuteBinding) {
	sort.Slice(items, func(left, right int) bool { return items[left].Code < items[right].Code })
}
