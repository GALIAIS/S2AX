package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	CityCommandTypeDevelopmentSubmit = "development.submit"
	CityCommandTypeDevelopmentReview = "development.review"
	CityCommandTypeDevelopmentStart  = "development.start"
	CityCommandTypeDevelopmentCancel = "development.cancel"

	CityDevelopmentProjectVerticalExpansion = "vertical_expansion"
	CityDevelopmentProjectRenovation        = "renovation"

	CityDevelopmentStatusSubmitted         = "submitted"
	CityDevelopmentStatusApproved          = "approved"
	CityDevelopmentStatusRejected          = "rejected"
	CityDevelopmentStatusUnderConstruction = "under_construction"
	CityDevelopmentStatusCompleted         = "completed"
	CityDevelopmentStatusCancelled         = "cancelled"

	CityDevelopmentFactSubmitted  = "submitted"
	CityDevelopmentFactApproved   = "approved"
	CityDevelopmentFactRejected   = "rejected"
	CityDevelopmentFactStarted    = "started"
	CityDevelopmentFactProgressed = "progressed"
	CityDevelopmentFactCompleted  = "completed"
	CityDevelopmentFactCancelled  = "cancelled"

	cityDevelopmentPolicyID        = "sub2api-development"
	cityDevelopmentPolicyVersion   = "1.0.0"
	cityDevelopmentPolicyCanonical = `{"id":"sub2api-development","max_quality_milli":1500,"renovation":{"basic_material_weighted_sqm_per_unit":50000,"capital_goods_weighted_sqm_per_unit":200000,"labor_units_per_tick":8,"labor_weighted_sqm_per_unit":100000,"max_duration_ticks":360,"min_duration_ticks":1},"version":"1.0.0","vertical_expansion":{"basic_material_sqm_per_unit":1000,"capital_goods_sqm_per_unit":10000,"labor_sqm_per_unit":5000,"labor_units_per_tick":8,"max_duration_ticks":720,"min_duration_ticks":2}}`
	cityDevelopmentPolicyHash      = "b1bbc919b39020a5bc4760fb0ee80468d286a4d74b97d4bbae8f8601c5bb9f3f"
	cityDevelopmentBaselineHash    = "fcb3ae78e18e4b3adb2db1cd9535403f61f28a04fee5eb13ac6ad284ca89459c"

	cityDevelopmentDefaultLimit = 100
	cityDevelopmentMaximumLimit = 200

	cityDevelopmentRejectionProjectNotFound  = "CITY_DEVELOPMENT_PROJECT_NOT_FOUND"
	cityDevelopmentRejectionBuildingNotFound = "CITY_DEVELOPMENT_BUILDING_NOT_FOUND"
	cityDevelopmentRejectionDeveloperInvalid = "CITY_DEVELOPMENT_DEVELOPER_INVALID"
	cityDevelopmentRejectionStateConflict    = "CITY_DEVELOPMENT_STATE_CONFLICT"
	cityDevelopmentRejectionActiveConflict   = "CITY_DEVELOPMENT_ACTIVE_PROJECT_CONFLICT"
	cityDevelopmentRejectionZoning           = "CITY_DEVELOPMENT_ZONING_REJECTED"
	cityDevelopmentRejectionLabor            = "CITY_DEVELOPMENT_LABOR_CAPACITY"
	cityDevelopmentRejectionResource         = "CITY_DEVELOPMENT_RESOURCE_INSUFFICIENT"
	cityDevelopmentRejectionPlanStale        = "CITY_DEVELOPMENT_PLAN_STALE"
)

var (
	ErrCityDevelopmentStateNotFound = infraerrors.NotFound(
		"CITY_DEVELOPMENT_STATE_NOT_FOUND", "city development state not found",
	)
	cityDevelopmentBuildingCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{1,159}$`)
	cityDevelopmentProjectCodePattern  = regexp.MustCompile(`^development_[1-9][0-9]*$`)
)

type CityDevelopmentProfile struct {
	PolicyID        string `json:"policy_id"`
	PolicyVersion   string `json:"policy_version"`
	PolicyHash      string `json:"policy_hash"`
	BaselineTick    int64  `json:"baseline_tick"`
	BaselineHash    string `json:"baseline_hash"`
	ProjectCount    int64  `json:"project_count"`
	FactCount       int64  `json:"fact_count"`
	AdjustmentCount int64  `json:"adjustment_count"`
	Revision        int64  `json:"revision"`
}

type CityDevelopmentProject struct {
	Code                       string          `json:"code"`
	Name                       string          `json:"name"`
	ProjectType                string          `json:"project_type"`
	DistrictCode               string          `json:"district_code"`
	ParcelCode                 string          `json:"parcel_code"`
	BuildingCode               string          `json:"building_code"`
	PrimaryUse                 string          `json:"primary_use"`
	DeveloperEntityCode        string          `json:"developer_entity_code"`
	TargetFloorCount           *int32          `json:"target_floor_count,omitempty"`
	TargetQualityMilli         *int64          `json:"target_quality_milli,omitempty"`
	AddedFloorCount            int32           `json:"added_floor_count"`
	AddedFloorAreaSQM          int64           `json:"added_floor_area_sqm"`
	AddedCapacityUnits         int64           `json:"added_capacity_units"`
	QualityDeltaMilli          int64           `json:"quality_delta_milli"`
	RequiredBasicMaterialUnits int64           `json:"required_basic_material_units"`
	RequiredCapitalGoodsUnits  int64           `json:"required_capital_goods_units"`
	RequiredLaborUnits         int64           `json:"required_labor_units"`
	PlannedDurationTicks       int64           `json:"planned_duration_ticks"`
	Status                     string          `json:"status"`
	ProgressMilli              int64           `json:"progress_milli"`
	SubmittedTick              int64           `json:"submitted_tick"`
	ReviewedTick               *int64          `json:"reviewed_tick,omitempty"`
	StartedTick                *int64          `json:"started_tick,omitempty"`
	PlannedCompletionTick      *int64          `json:"planned_completion_tick,omitempty"`
	CompletedTick              *int64          `json:"completed_tick,omitempty"`
	CancelledTick              *int64          `json:"cancelled_tick,omitempty"`
	Version                    int64           `json:"version"`
	Metadata                   json.RawMessage `json:"metadata"`
}

type CityDevelopmentFact struct {
	Tick                  int64           `json:"tick"`
	Sequence              int64           `json:"sequence"`
	ProjectCode           string          `json:"project_code"`
	SourceCommandSequence *int64          `json:"source_command_sequence,omitempty"`
	FactType              string          `json:"fact_type"`
	FromStatus            *string         `json:"from_status,omitempty"`
	ToStatus              string          `json:"to_status"`
	ProgressBeforeMilli   int64           `json:"progress_before_milli"`
	ProgressAfterMilli    int64           `json:"progress_after_milli"`
	ProjectVersionBefore  int64           `json:"project_version_before"`
	ProjectVersionAfter   int64           `json:"project_version_after"`
	Metadata              json.RawMessage `json:"metadata"`
}

type CityBuildingAdjustment struct {
	ProjectCode        string          `json:"project_code"`
	BuildingCode       string          `json:"building_code"`
	DistrictCode       string          `json:"district_code"`
	AddedFloorCount    int32           `json:"added_floor_count"`
	AddedTopZ          int32           `json:"added_top_z"`
	AddedFloorAreaSQM  int64           `json:"added_floor_area_sqm"`
	AddedCapacityUnits int64           `json:"added_capacity_units"`
	QualityDeltaMilli  int64           `json:"quality_delta_milli"`
	CompletedTick      int64           `json:"completed_tick"`
	Metadata           json.RawMessage `json:"metadata"`
}

type CityDevelopmentDeveloper struct {
	EntityID            int64  `json:"entity_id"`
	EntityCode          string `json:"entity_code"`
	EntityName          string `json:"entity_name"`
	DistrictCode        string `json:"district_code"`
	EmployeeUnits       int64  `json:"employee_units"`
	ReservedLaborUnits  int64  `json:"reserved_labor_units"`
	AvailableLaborUnits int64  `json:"available_labor_units"`
}

type CityDevelopmentState struct {
	Profile     CityDevelopmentProfile     `json:"profile"`
	Projects    []CityDevelopmentProject   `json:"projects"`
	Facts       []CityDevelopmentFact      `json:"facts"`
	Adjustments []CityBuildingAdjustment   `json:"adjustments"`
	Developers  []CityDevelopmentDeveloper `json:"developers"`
	NextCursor  *CityDevelopmentFactCursor `json:"next_cursor,omitempty"`
}

type CityDevelopmentFactCursor struct {
	Tick     int64 `json:"tick"`
	Sequence int64 `json:"sequence"`
}

type CityDevelopmentQueryInput struct {
	UserID        int64
	WorldID       int64
	Status        string
	BuildingCode  string
	AfterTick     int64
	AfterSequence int64
	Limit         int
}

type cityDevelopmentHashState struct {
	Profile     CityDevelopmentProfile   `json:"profile"`
	Projects    []CityDevelopmentProject `json:"projects"`
	Facts       []CityDevelopmentFact    `json:"facts"`
	Adjustments []CityBuildingAdjustment `json:"adjustments"`
}

type cityDevelopmentSubmitPayload struct {
	ProjectType        string `json:"project_type"`
	BuildingCode       string `json:"building_code"`
	DeveloperEntityID  int64  `json:"developer_entity_id"`
	TargetFloorCount   *int32 `json:"target_floor_count,omitempty"`
	TargetQualityMilli *int64 `json:"target_quality_milli,omitempty"`
	Name               string `json:"name,omitempty"`
}

type cityDevelopmentReviewPayload struct {
	ProjectCode string `json:"project_code"`
	Decision    string `json:"decision"`
	Note        string `json:"note,omitempty"`
}

type cityDevelopmentStartPayload struct {
	ProjectCode string `json:"project_code"`
}

type cityDevelopmentCancelPayload struct {
	ProjectCode string `json:"project_code"`
	Reason      string `json:"reason"`
}

type cityDevelopmentBusinessError struct{ code string }

func (err *cityDevelopmentBusinessError) Error() string { return err.code }

func cityDevelopmentReject(code string) error {
	return &cityDevelopmentBusinessError{code: code}
}

func cityDevelopmentBusinessRejectionCode(err error) string {
	var businessErr *cityDevelopmentBusinessError
	if errors.As(err, &businessErr) {
		return businessErr.code
	}
	if code := cityResourceBusinessRejectionCode(err); code != "" {
		if code == cityResourceRejectionInsufficient {
			return cityDevelopmentRejectionResource
		}
		return code
	}
	return ""
}

func isCityDevelopmentCommand(commandType string) bool {
	switch commandType {
	case CityCommandTypeDevelopmentSubmit, CityCommandTypeDevelopmentReview,
		CityCommandTypeDevelopmentStart, CityCommandTypeDevelopmentCancel:
		return true
	default:
		return false
	}
}

func normalizeCityDevelopmentCommand(commandType string, rawPayload json.RawMessage) (any, bool, error) {
	normalizeProjectCode := func(value *string) error {
		*value = strings.ToLower(strings.TrimSpace(*value))
		if !cityDevelopmentProjectCodePattern.MatchString(*value) {
			return ErrCityInvalidInput
		}
		return nil
	}
	normalizeText := func(value *string, required bool, maximum int) error {
		*value = strings.TrimSpace(*value)
		count := utf8.RuneCountInString(*value)
		if count > maximum || required && count == 0 {
			return ErrCityInvalidInput
		}
		return nil
	}
	switch commandType {
	case CityCommandTypeDevelopmentSubmit:
		var payload cityDevelopmentSubmitPayload
		if err := decodeStrictCityObject(rawPayload, &payload); err != nil {
			return nil, true, ErrCityInvalidInput.WithCause(err)
		}
		payload.ProjectType = strings.ToLower(strings.TrimSpace(payload.ProjectType))
		payload.BuildingCode = strings.ToLower(strings.TrimSpace(payload.BuildingCode))
		if !cityDevelopmentBuildingCodePattern.MatchString(payload.BuildingCode) ||
			payload.DeveloperEntityID <= 0 || normalizeText(&payload.Name, false, 128) != nil {
			return nil, true, ErrCityInvalidInput
		}
		switch payload.ProjectType {
		case CityDevelopmentProjectVerticalExpansion:
			if payload.TargetFloorCount == nil || *payload.TargetFloorCount <= 0 ||
				*payload.TargetFloorCount > 128 || payload.TargetQualityMilli != nil {
				return nil, true, ErrCityInvalidInput
			}
		case CityDevelopmentProjectRenovation:
			if payload.TargetQualityMilli == nil || *payload.TargetQualityMilli <= 0 ||
				*payload.TargetQualityMilli > 1500 || payload.TargetFloorCount != nil {
				return nil, true, ErrCityInvalidInput
			}
		default:
			return nil, true, ErrCityInvalidInput
		}
		return payload, true, nil
	case CityCommandTypeDevelopmentReview:
		var payload cityDevelopmentReviewPayload
		if err := decodeStrictCityObject(rawPayload, &payload); err != nil {
			return nil, true, ErrCityInvalidInput.WithCause(err)
		}
		payload.Decision = strings.ToLower(strings.TrimSpace(payload.Decision))
		if normalizeProjectCode(&payload.ProjectCode) != nil ||
			(payload.Decision != "approve" && payload.Decision != "reject") ||
			normalizeText(&payload.Note, false, 256) != nil {
			return nil, true, ErrCityInvalidInput
		}
		return payload, true, nil
	case CityCommandTypeDevelopmentStart:
		var payload cityDevelopmentStartPayload
		if err := decodeStrictCityObject(rawPayload, &payload); err != nil {
			return nil, true, ErrCityInvalidInput.WithCause(err)
		}
		if normalizeProjectCode(&payload.ProjectCode) != nil {
			return nil, true, ErrCityInvalidInput
		}
		return payload, true, nil
	case CityCommandTypeDevelopmentCancel:
		var payload cityDevelopmentCancelPayload
		if err := decodeStrictCityObject(rawPayload, &payload); err != nil {
			return nil, true, ErrCityInvalidInput.WithCause(err)
		}
		if normalizeProjectCode(&payload.ProjectCode) != nil ||
			normalizeText(&payload.Reason, true, 256) != nil {
			return nil, true, ErrCityInvalidInput
		}
		return payload, true, nil
	default:
		return nil, false, nil
	}
}

type cityDevelopmentBuildingPlan struct {
	projectType                string
	buildingID                 int64
	buildingCode               string
	parcelID                   int64
	parcelCode                 string
	districtID                 int64
	districtCode               string
	primaryUse                 string
	developerEntityID          int64
	developerEntityCode        string
	effectiveFloorCount        int64
	effectiveFloorAreaSQM      int64
	effectiveCapacityUnits     int64
	effectiveQualityMilli      int64
	baseTopZ                   int64
	footprintAreaSQM           int64
	parcelAreaSQM              int64
	maxFloorAreaRatioMilli     int64
	maxFloors                  int64
	sqmPerCapacityUnit         int64
	targetFloorCount           *int32
	targetQualityMilli         *int64
	addedFloorCount            int32
	addedFloorAreaSQM          int64
	addedCapacityUnits         int64
	qualityDeltaMilli          int64
	requiredBasicMaterialUnits int64
	requiredCapitalGoodsUnits  int64
	requiredLaborUnits         int64
	plannedDurationTicks       int64
}

func deriveCityDevelopmentPlan(
	projectType string,
	currentFloorCount, currentFloorAreaSQM, currentCapacityUnits, currentQualityMilli,
	footprintAreaSQM, parcelAreaSQM, maxFloorAreaRatioMilli, maxFloors,
	sqmPerCapacityUnit int64,
	targetFloorCount *int32,
	targetQualityMilli *int64,
) (*cityDevelopmentBuildingPlan, error) {
	plan := &cityDevelopmentBuildingPlan{
		projectType:         projectType,
		effectiveFloorCount: currentFloorCount, effectiveFloorAreaSQM: currentFloorAreaSQM,
		effectiveCapacityUnits: currentCapacityUnits, effectiveQualityMilli: currentQualityMilli,
		footprintAreaSQM: footprintAreaSQM, parcelAreaSQM: parcelAreaSQM,
		maxFloorAreaRatioMilli: maxFloorAreaRatioMilli, maxFloors: maxFloors,
		sqmPerCapacityUnit: sqmPerCapacityUnit,
		targetFloorCount:   targetFloorCount, targetQualityMilli: targetQualityMilli,
	}
	if currentFloorCount <= 0 || currentFloorAreaSQM <= 0 || currentCapacityUnits < 0 ||
		currentQualityMilli <= 0 || footprintAreaSQM <= 0 || parcelAreaSQM <= 0 ||
		maxFloorAreaRatioMilli <= 0 || maxFloors <= 0 || sqmPerCapacityUnit <= 0 {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "development_building_state"})
	}
	switch projectType {
	case CityDevelopmentProjectVerticalExpansion:
		if targetFloorCount == nil || targetQualityMilli != nil {
			return nil, cityDevelopmentReject(cityDevelopmentRejectionZoning)
		}
		target := int64(*targetFloorCount)
		if target <= currentFloorCount || target > maxFloors {
			return nil, cityDevelopmentReject(cityDevelopmentRejectionZoning)
		}
		addedFloors := target - currentFloorCount
		addedArea, err := cityMultiplyUnits(footprintAreaSQM, addedFloors)
		if err != nil {
			return nil, err
		}
		maximumArea, err := cityMultiplyUnits(parcelAreaSQM, maxFloorAreaRatioMilli)
		if err != nil {
			return nil, err
		}
		maximumArea /= 1000
		if currentFloorAreaSQM > math.MaxInt64-addedArea || currentFloorAreaSQM+addedArea > maximumArea {
			return nil, cityDevelopmentReject(cityDevelopmentRejectionZoning)
		}
		addedCapacity := addedArea / sqmPerCapacityUnit
		if addedCapacity <= 0 {
			return nil, cityDevelopmentReject(cityDevelopmentRejectionZoning)
		}
		material, err := cityDivideRoundUp(addedArea, 1_000)
		if err != nil {
			return nil, err
		}
		capital, err := cityDivideRoundUp(addedArea, 10_000)
		if err != nil {
			return nil, err
		}
		labor, err := cityDivideRoundUp(addedArea, 5_000)
		if err != nil {
			return nil, err
		}
		duration, err := cityDivideRoundUp(labor, 8)
		if err != nil {
			return nil, err
		}
		plan.addedFloorCount = int32(addedFloors)
		plan.addedFloorAreaSQM = addedArea
		plan.addedCapacityUnits = addedCapacity
		plan.requiredBasicMaterialUnits = maxInt64(1, material)
		plan.requiredCapitalGoodsUnits = maxInt64(1, capital)
		plan.requiredLaborUnits = maxInt64(1, labor)
		plan.plannedDurationTicks = clampInt64(duration, 2, 720)
	case CityDevelopmentProjectRenovation:
		if targetQualityMilli == nil || targetFloorCount != nil ||
			*targetQualityMilli <= currentQualityMilli || *targetQualityMilli > 1500 {
			return nil, cityDevelopmentReject(cityDevelopmentRejectionZoning)
		}
		delta := *targetQualityMilli - currentQualityMilli
		weightedArea, err := cityMultiplyUnits(currentFloorAreaSQM, delta)
		if err != nil {
			return nil, err
		}
		material, err := cityDivideRoundUp(weightedArea, 50_000)
		if err != nil {
			return nil, err
		}
		capital, err := cityDivideRoundUp(weightedArea, 200_000)
		if err != nil {
			return nil, err
		}
		labor, err := cityDivideRoundUp(weightedArea, 100_000)
		if err != nil {
			return nil, err
		}
		duration, err := cityDivideRoundUp(maxInt64(1, labor), 8)
		if err != nil {
			return nil, err
		}
		plan.qualityDeltaMilli = delta
		plan.requiredBasicMaterialUnits = maxInt64(1, material)
		plan.requiredCapitalGoodsUnits = maxInt64(1, capital)
		plan.requiredLaborUnits = maxInt64(1, labor)
		plan.plannedDurationTicks = clampInt64(duration, 1, 360)
	default:
		return nil, cityDevelopmentReject(cityDevelopmentRejectionZoning)
	}
	return plan, nil
}

func clampInt64(value, minimum, maximum int64) int64 {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}

func initializeCityDevelopmentFoundation(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if _, err := tx.ExecContext(ctx, `SELECT initialize_city_f74_foundation($1)`, worldID); err != nil {
		return fmt.Errorf("initialize city F7.4 development foundation: %w", err)
	}
	return nil
}

func loadCityDevelopmentHashState(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	simulationVersion string,
) (*cityDevelopmentHashState, error) {
	if !cityEngineSupportsDevelopment(simulationVersion) {
		return nil, ErrCityDevelopmentStateNotFound
	}
	state := &cityDevelopmentHashState{
		Projects:    make([]CityDevelopmentProject, 0),
		Facts:       make([]CityDevelopmentFact, 0),
		Adjustments: make([]CityBuildingAdjustment, 0),
	}
	err := queryer.QueryRowContext(ctx, `
SELECT profile.policy_id, profile.policy_version, profile.policy_hash,
       profile.baseline_tick, baseline.baseline_hash, profile.project_count,
       profile.fact_count, profile.adjustment_count, profile.revision
FROM city_development_profiles profile
JOIN city_development_baselines baseline ON baseline.world_id = profile.world_id
WHERE profile.world_id = $1`, worldID).Scan(
		&state.Profile.PolicyID, &state.Profile.PolicyVersion, &state.Profile.PolicyHash,
		&state.Profile.BaselineTick, &state.Profile.BaselineHash,
		&state.Profile.ProjectCount, &state.Profile.FactCount,
		&state.Profile.AdjustmentCount, &state.Profile.Revision,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCityDevelopmentStateNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load city development profile: %w", err)
	}

	projectRows, err := queryer.QueryContext(ctx, cityDevelopmentProjectCanonicalSelect+`
WHERE project.world_id = $1
ORDER BY project.code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load city development projects: %w", err)
	}
	for projectRows.Next() {
		project, scanErr := scanCityDevelopmentProject(projectRows)
		if scanErr != nil {
			_ = projectRows.Close()
			return nil, scanErr
		}
		state.Projects = append(state.Projects, *project)
	}
	if err = closeCityRows(projectRows, "iterate city development projects"); err != nil {
		return nil, err
	}

	factRows, err := queryer.QueryContext(ctx, cityDevelopmentFactCanonicalSelect+`
WHERE fact.world_id = $1 AND fact.posted_at IS NOT NULL
ORDER BY fact.tick ASC, fact.sequence ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load city development facts: %w", err)
	}
	for factRows.Next() {
		fact, scanErr := scanCityDevelopmentFact(factRows)
		if scanErr != nil {
			_ = factRows.Close()
			return nil, scanErr
		}
		state.Facts = append(state.Facts, *fact)
	}
	if err = closeCityRows(factRows, "iterate city development facts"); err != nil {
		return nil, err
	}

	adjustmentRows, err := queryer.QueryContext(ctx, cityBuildingAdjustmentCanonicalSelect+`
WHERE adjustment.world_id = $1
ORDER BY building.code ASC, adjustment.project_code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load city building adjustments: %w", err)
	}
	for adjustmentRows.Next() {
		adjustment, scanErr := scanCityBuildingAdjustment(adjustmentRows)
		if scanErr != nil {
			_ = adjustmentRows.Close()
			return nil, scanErr
		}
		state.Adjustments = append(state.Adjustments, *adjustment)
	}
	if err = closeCityRows(adjustmentRows, "iterate city building adjustments"); err != nil {
		return nil, err
	}
	return state, nil
}

func loadCityDevelopmentFactsForTick(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, tick int64,
) ([]CityDevelopmentFact, error) {
	rows, err := queryer.QueryContext(ctx, cityDevelopmentFactCanonicalSelect+`
WHERE fact.world_id = $1 AND fact.tick = $2 AND fact.posted_at IS NOT NULL
ORDER BY fact.sequence ASC`, worldID, tick)
	if err != nil {
		return nil, fmt.Errorf("load city development facts for tick: %w", err)
	}
	items := make([]CityDevelopmentFact, 0)
	for rows.Next() {
		item, scanErr := scanCityDevelopmentFact(rows)
		if scanErr != nil {
			_ = rows.Close()
			return nil, scanErr
		}
		items = append(items, *item)
	}
	if err = closeCityRows(rows, "iterate city development facts for tick"); err != nil {
		return nil, err
	}
	return items, nil
}

func loadCityBuildingAdjustmentsForTick(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, tick int64,
) ([]CityBuildingAdjustment, error) {
	rows, err := queryer.QueryContext(ctx, cityBuildingAdjustmentCanonicalSelect+`
WHERE adjustment.world_id = $1 AND adjustment.completed_tick = $2
ORDER BY building.code ASC, adjustment.project_code ASC`, worldID, tick)
	if err != nil {
		return nil, fmt.Errorf("load city building adjustments for tick: %w", err)
	}
	items := make([]CityBuildingAdjustment, 0)
	for rows.Next() {
		item, scanErr := scanCityBuildingAdjustment(rows)
		if scanErr != nil {
			_ = rows.Close()
			return nil, scanErr
		}
		items = append(items, *item)
	}
	if err = closeCityRows(rows, "iterate city building adjustments for tick"); err != nil {
		return nil, err
	}
	return items, nil
}

const cityDevelopmentProjectCanonicalSelect = `
SELECT project.code, project.name, project.project_type, district.code, parcel.code,
       building.code, building.primary_use, developer.code, project.target_floor_count,
       project.target_quality_milli, project.added_floor_count,
       project.added_floor_area_sqm, project.added_capacity_units,
       project.quality_delta_milli, project.required_basic_material_units,
       project.required_capital_goods_units, project.required_labor_units,
       project.planned_duration_ticks, project.status, project.progress_milli,
       project.submitted_tick, project.reviewed_tick, project.started_tick,
       project.planned_completion_tick, project.completed_tick,
       project.cancelled_tick, project.version, project.metadata
FROM city_development_projects project
JOIN city_districts district ON district.id = project.district_id
JOIN city_parcels parcel ON parcel.id = project.parcel_id
JOIN city_buildings building ON building.id = project.building_id
JOIN city_economic_entities developer ON developer.id = project.developer_entity_id
`

const cityDevelopmentFactCanonicalSelect = `
SELECT fact.tick, fact.sequence, fact.project_code, command.sequence,
       fact.fact_type, fact.from_status, fact.to_status,
       fact.progress_before_milli, fact.progress_after_milli,
       fact.project_version_before, fact.project_version_after, fact.metadata
FROM city_development_facts fact
LEFT JOIN city_commands command ON command.id = fact.source_command_id
`

const cityBuildingAdjustmentCanonicalSelect = `
SELECT adjustment.project_code, building.code, district.code,
       adjustment.added_floor_count, adjustment.added_top_z,
       adjustment.added_floor_area_sqm, adjustment.added_capacity_units,
       adjustment.quality_delta_milli, adjustment.completed_tick,
       adjustment.metadata
FROM city_building_adjustments adjustment
JOIN city_buildings building ON building.id = adjustment.building_id
JOIN city_districts district ON district.id = adjustment.district_id
`

func scanCityDevelopmentProject(row cityScannable) (*CityDevelopmentProject, error) {
	project := &CityDevelopmentProject{}
	var targetFloors sql.NullInt32
	var targetQuality, reviewed, started, planned, completed, cancelled sql.NullInt64
	if err := row.Scan(
		&project.Code, &project.Name, &project.ProjectType, &project.DistrictCode,
		&project.ParcelCode, &project.BuildingCode, &project.PrimaryUse,
		&project.DeveloperEntityCode,
		&targetFloors, &targetQuality, &project.AddedFloorCount,
		&project.AddedFloorAreaSQM, &project.AddedCapacityUnits,
		&project.QualityDeltaMilli, &project.RequiredBasicMaterialUnits,
		&project.RequiredCapitalGoodsUnits, &project.RequiredLaborUnits,
		&project.PlannedDurationTicks, &project.Status, &project.ProgressMilli,
		&project.SubmittedTick, &reviewed, &started, &planned, &completed,
		&cancelled, &project.Version, &project.Metadata,
	); err != nil {
		return nil, err
	}
	if targetFloors.Valid {
		value := targetFloors.Int32
		project.TargetFloorCount = &value
	}
	project.TargetQualityMilli = nullableInt64Pointer(targetQuality)
	project.ReviewedTick = nullableInt64Pointer(reviewed)
	project.StartedTick = nullableInt64Pointer(started)
	project.PlannedCompletionTick = nullableInt64Pointer(planned)
	project.CompletedTick = nullableInt64Pointer(completed)
	project.CancelledTick = nullableInt64Pointer(cancelled)
	return project, nil
}

func scanCityDevelopmentFact(row cityScannable) (*CityDevelopmentFact, error) {
	fact := &CityDevelopmentFact{}
	var commandSequence sql.NullInt64
	var fromStatus sql.NullString
	if err := row.Scan(
		&fact.Tick, &fact.Sequence, &fact.ProjectCode, &commandSequence,
		&fact.FactType, &fromStatus, &fact.ToStatus,
		&fact.ProgressBeforeMilli, &fact.ProgressAfterMilli,
		&fact.ProjectVersionBefore, &fact.ProjectVersionAfter, &fact.Metadata,
	); err != nil {
		return nil, err
	}
	fact.SourceCommandSequence = nullableInt64Pointer(commandSequence)
	if fromStatus.Valid {
		value := fromStatus.String
		fact.FromStatus = &value
	}
	return fact, nil
}

func scanCityBuildingAdjustment(row cityScannable) (*CityBuildingAdjustment, error) {
	adjustment := &CityBuildingAdjustment{}
	if err := row.Scan(
		&adjustment.ProjectCode, &adjustment.BuildingCode, &adjustment.DistrictCode,
		&adjustment.AddedFloorCount, &adjustment.AddedTopZ,
		&adjustment.AddedFloorAreaSQM, &adjustment.AddedCapacityUnits,
		&adjustment.QualityDeltaMilli, &adjustment.CompletedTick,
		&adjustment.Metadata,
	); err != nil {
		return nil, err
	}
	return adjustment, nil
}

func nullableInt64Pointer(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	result := value.Int64
	return &result
}

func cityDevelopmentProjectCode(commandSequence int64) string {
	return "development_" + strconv.FormatInt(commandSequence, 10)
}

func cityDevelopmentProgress(startedTick, durationTicks, targetTick int64) (int64, error) {
	if startedTick <= 0 || durationTicks <= 0 || targetTick < startedTick {
		return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "development_progress"})
	}
	elapsed := targetTick - startedTick
	if elapsed >= durationTicks {
		return 1000, nil
	}
	progress, err := cityMultiplyUnits(elapsed, 1000)
	if err != nil {
		return 0, err
	}
	return progress / durationTicks, nil
}

func replayCityDevelopmentFacts(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, tick int64,
	state *cityHashState,
) error {
	if state == nil || state.Development == nil || !cityEngineSupportsDevelopment(state.SimulationVersion) {
		return fmt.Errorf("city development replay state is unavailable")
	}
	facts, err := loadCityDevelopmentFactsForTick(ctx, queryer, worldID, tick)
	if err != nil {
		return err
	}
	for index := range facts {
		fact := facts[index]
		if fact.Tick != tick || fact.Sequence != int64(index+1) {
			return fmt.Errorf("city development fact sequence is not contiguous")
		}
		if err = applyCityDevelopmentFactToHashState(state, fact); err != nil {
			return err
		}
	}
	return nil
}

func applyCityDevelopmentFactToHashState(state *cityHashState, fact CityDevelopmentFact) error {
	development := state.Development
	if development == nil || fact.ProjectCode == "" {
		return fmt.Errorf("city development fact has no canonical target")
	}
	if fact.FactType == CityDevelopmentFactSubmitted {
		var metadata struct {
			Project CityDevelopmentProject `json:"project"`
		}
		if err := decodeCityDevelopmentMetadata(fact.Metadata, &metadata); err != nil {
			return err
		}
		project := metadata.Project
		if project.Code != fact.ProjectCode || project.Status != CityDevelopmentStatusSubmitted ||
			project.ProgressMilli != 0 || project.Version != fact.ProjectVersionAfter ||
			project.SubmittedTick != fact.Tick || findCityDevelopmentProject(development.Projects, project.Code) >= 0 {
			return fmt.Errorf("submitted city development fact is inconsistent")
		}
		development.Projects = append(development.Projects, project)
		sort.Slice(development.Projects, func(i, j int) bool {
			return development.Projects[i].Code < development.Projects[j].Code
		})
		development.Profile.ProjectCount++
	} else {
		projectIndex := findCityDevelopmentProject(development.Projects, fact.ProjectCode)
		if projectIndex < 0 {
			return fmt.Errorf("city development fact references an unknown project")
		}
		project := &development.Projects[projectIndex]
		if fact.FromStatus == nil || project.Status != *fact.FromStatus ||
			project.ProgressMilli != fact.ProgressBeforeMilli ||
			project.Version != fact.ProjectVersionBefore ||
			fact.ProjectVersionAfter != fact.ProjectVersionBefore+1 {
			return fmt.Errorf("city development fact before-state is inconsistent")
		}
		project.Status = fact.ToStatus
		project.ProgressMilli = fact.ProgressAfterMilli
		project.Version = fact.ProjectVersionAfter
		switch fact.FactType {
		case CityDevelopmentFactApproved, CityDevelopmentFactRejected:
			project.ReviewedTick = int64Pointer(fact.Tick)
		case CityDevelopmentFactStarted:
			var metadata struct {
				StartedTick           int64 `json:"started_tick"`
				PlannedCompletionTick int64 `json:"planned_completion_tick"`
			}
			if err := decodeCityDevelopmentMetadata(fact.Metadata, &metadata); err != nil {
				return err
			}
			if metadata.StartedTick != fact.Tick ||
				metadata.PlannedCompletionTick != fact.Tick+project.PlannedDurationTicks {
				return fmt.Errorf("started city development fact timing is inconsistent")
			}
			project.StartedTick = int64Pointer(metadata.StartedTick)
			project.PlannedCompletionTick = int64Pointer(metadata.PlannedCompletionTick)
		case CityDevelopmentFactProgressed:
		case CityDevelopmentFactCancelled:
			project.CancelledTick = int64Pointer(fact.Tick)
		case CityDevelopmentFactCompleted:
			var metadata struct {
				Adjustment CityBuildingAdjustment `json:"adjustment"`
			}
			if err := decodeCityDevelopmentMetadata(fact.Metadata, &metadata); err != nil {
				return err
			}
			adjustment := metadata.Adjustment
			if adjustment.ProjectCode != project.Code || adjustment.BuildingCode != project.BuildingCode ||
				adjustment.DistrictCode != project.DistrictCode || adjustment.CompletedTick != fact.Tick ||
				adjustment.AddedFloorCount != project.AddedFloorCount ||
				adjustment.AddedFloorAreaSQM != project.AddedFloorAreaSQM ||
				adjustment.AddedCapacityUnits != project.AddedCapacityUnits ||
				adjustment.QualityDeltaMilli != project.QualityDeltaMilli {
				return fmt.Errorf("completed city development adjustment is inconsistent")
			}
			project.CompletedTick = int64Pointer(fact.Tick)
			development.Adjustments = append(development.Adjustments, adjustment)
			sort.Slice(development.Adjustments, func(i, j int) bool {
				left, right := development.Adjustments[i], development.Adjustments[j]
				if left.BuildingCode != right.BuildingCode {
					return left.BuildingCode < right.BuildingCode
				}
				return left.ProjectCode < right.ProjectCode
			})
			development.Profile.AdjustmentCount++
			if err := addCityDevelopmentCapacityToHashState(
				&state.Physical, project.DistrictCode, project.PrimaryUse,
				project.AddedCapacityUnits,
			); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported city development fact type %q", fact.FactType)
		}
	}
	development.Facts = append(development.Facts, fact)
	development.Profile.FactCount++
	development.Profile.Revision++
	return nil
}

func decodeCityDevelopmentMetadata(raw json.RawMessage, target any) error {
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("decode city development fact metadata: %w", err)
	}
	return nil
}

func findCityDevelopmentProject(projects []CityDevelopmentProject, code string) int {
	for index := range projects {
		if projects[index].Code == code {
			return index
		}
	}
	return -1
}

func addCityDevelopmentCapacityToHashState(
	physical *cityPhysicalHashState,
	districtCode, primaryUse string,
	capacity int64,
) error {
	if physical == nil || capacity < 0 {
		return fmt.Errorf("city development physical projection is invalid")
	}
	if capacity == 0 {
		return nil
	}
	for index := range physical.Districts {
		if physical.Districts[index].Code != districtCode {
			continue
		}
		district := &physical.Districts[index]
		var target *int64
		switch primaryUse {
		case "residential":
			target = &district.ResidentialCapacity
		case "commercial":
			target = &district.CommercialCapacity
		case "industrial":
			target = &district.IndustrialCapacity
		default:
			return fmt.Errorf("city development building use is invalid")
		}
		if *target > math.MaxInt64-capacity {
			return fmt.Errorf("city development district capacity overflow")
		}
		*target += capacity
		return nil
	}
	return fmt.Errorf("city development district is missing from physical state")
}

func int64Pointer(value int64) *int64 { return &value }

func developmentNullableInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func developmentNullableInt32(value *int32) any {
	if value == nil {
		return nil
	}
	return *value
}

// Keep compiler-enforced use of deterministic policy constants in one place.
func cityDevelopmentPolicyIdentity() (string, string, string, string) {
	return cityDevelopmentPolicyID, cityDevelopmentPolicyVersion,
		cityDevelopmentPolicyHash, cityDevelopmentBaselineHash
}
