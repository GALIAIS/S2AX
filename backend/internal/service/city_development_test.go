package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeCityDevelopmentCommandsKeepsOnlyStrictIntent(t *testing.T) {
	targetFloors := int32(4)
	normalized, handled, err := normalizeCityDevelopmentCommand(
		CityCommandTypeDevelopmentSubmit,
		json.RawMessage(`{
			"project_type":" VERTICAL_EXPANSION ",
			"building_code":" BUILDING_CENTRAL ",
			"developer_entity_id":42,
			"target_floor_count":4,
			"name":"  Central Extension  "
		}`),
	)
	require.True(t, handled)
	require.NoError(t, err)
	require.Equal(t, cityDevelopmentSubmitPayload{
		ProjectType:  CityDevelopmentProjectVerticalExpansion,
		BuildingCode: "building_central", DeveloperEntityID: 42,
		TargetFloorCount: &targetFloors, Name: "Central Extension",
	}, normalized)

	normalized, handled, err = normalizeCityDevelopmentCommand(
		CityCommandTypeDevelopmentReview,
		json.RawMessage(`{"project_code":" DEVELOPMENT_9 ","decision":" APPROVE ","note":"  valid  "}`),
	)
	require.True(t, handled)
	require.NoError(t, err)
	require.Equal(t, cityDevelopmentReviewPayload{
		ProjectCode: "development_9", Decision: "approve", Note: "valid",
	}, normalized)

	_, handled, err = normalizeCityDevelopmentCommand(
		CityCommandTypeDevelopmentSubmit,
		json.RawMessage(`{
			"project_type":"vertical_expansion",
			"building_code":"building_central",
			"developer_entity_id":42,
			"target_floor_count":4,
			"target_quality_milli":1100
		}`),
	)
	require.True(t, handled)
	require.ErrorIs(t, err, ErrCityInvalidInput)

	_, handled, err = normalizeCityDevelopmentCommand("development.unknown", json.RawMessage(`{}`))
	require.False(t, handled)
	require.NoError(t, err)
}

func TestDeriveCityDevelopmentPlanUsesDeterministicIntegerPolicy(t *testing.T) {
	targetFloors := int32(4)
	expansion, err := deriveCityDevelopmentPlan(
		CityDevelopmentProjectVerticalExpansion,
		2, 400, 8, 1000,
		200, 500, 3000, 6, 50,
		&targetFloors, nil,
	)
	require.NoError(t, err)
	require.Equal(t, int32(2), expansion.addedFloorCount)
	require.Equal(t, int64(400), expansion.addedFloorAreaSQM)
	require.Equal(t, int64(8), expansion.addedCapacityUnits)
	require.Equal(t, int64(1), expansion.requiredBasicMaterialUnits)
	require.Equal(t, int64(1), expansion.requiredCapitalGoodsUnits)
	require.Equal(t, int64(1), expansion.requiredLaborUnits)
	require.Equal(t, int64(2), expansion.plannedDurationTicks)

	targetQuality := int64(1200)
	renovation, err := deriveCityDevelopmentPlan(
		CityDevelopmentProjectRenovation,
		2, 400, 8, 1000,
		200, 500, 3000, 6, 50,
		nil, &targetQuality,
	)
	require.NoError(t, err)
	require.Equal(t, int64(200), renovation.qualityDeltaMilli)
	require.Equal(t, int64(2), renovation.requiredBasicMaterialUnits)
	require.Equal(t, int64(1), renovation.requiredCapitalGoodsUnits)
	require.Equal(t, int64(1), renovation.requiredLaborUnits)
	require.Equal(t, int64(1), renovation.plannedDurationTicks)

	overZoning := int32(7)
	_, err = deriveCityDevelopmentPlan(
		CityDevelopmentProjectVerticalExpansion,
		2, 400, 8, 1000,
		200, 500, 3000, 6, 50,
		&overZoning, nil,
	)
	require.Equal(t, cityDevelopmentRejectionZoning, cityDevelopmentBusinessRejectionCode(err))
}

func TestCityDevelopmentPolicyHashBindsCanonicalCoefficients(t *testing.T) {
	digest := sha256.Sum256([]byte(cityDevelopmentPolicyCanonical))
	require.Equal(t, cityDevelopmentPolicyHash, hex.EncodeToString(digest[:]))
}

func TestCityDevelopmentProgressIsMonotonicAndTickDerived(t *testing.T) {
	for targetTick, expected := range map[int64]int64{10: 0, 11: 333, 12: 666, 13: 1000, 14: 1000} {
		progress, err := cityDevelopmentProgress(10, 3, targetTick)
		require.NoError(t, err)
		require.Equal(t, expected, progress)
	}
	_, err := cityDevelopmentProgress(10, 3, 9)
	require.Error(t, err)
}

func TestApplyCityDevelopmentFactsRebuildsProjectAdjustmentAndCapacity(t *testing.T) {
	state := &cityHashState{
		SimulationVersion: CitySimulationVersionF7V3,
		Physical: cityPhysicalHashState{Districts: []cityHashDistrict{{
			Code: "central", ResidentialCapacity: 20,
		}}},
		Development: &cityDevelopmentHashState{
			Projects: make([]CityDevelopmentProject, 0), Facts: make([]CityDevelopmentFact, 0),
			Adjustments: make([]CityBuildingAdjustment, 0),
		},
	}
	project := CityDevelopmentProject{
		Code: "development_7", Name: "Central Extension",
		ProjectType:  CityDevelopmentProjectVerticalExpansion,
		DistrictCode: "central", ParcelCode: "parcel_central",
		BuildingCode: "building_central", PrimaryUse: "residential",
		DeveloperEntityCode: "firm_central", AddedFloorCount: 2,
		AddedFloorAreaSQM: 400, AddedCapacityUnits: 8,
		RequiredBasicMaterialUnits: 40, RequiredCapitalGoodsUnits: 4,
		RequiredLaborUnits: 8, PlannedDurationTicks: 2,
		Status: CityDevelopmentStatusSubmitted, SubmittedTick: 1,
		Version: 1, Metadata: json.RawMessage(`{"schema_version":1}`),
	}
	submittedMetadata, err := json.Marshal(map[string]any{"project": project, "schema_version": 1})
	require.NoError(t, err)
	require.NoError(t, applyCityDevelopmentFactToHashState(state, CityDevelopmentFact{
		Tick: 1, Sequence: 1, ProjectCode: project.Code,
		FactType: CityDevelopmentFactSubmitted, ToStatus: CityDevelopmentStatusSubmitted,
		ProjectVersionBefore: 0, ProjectVersionAfter: 1, Metadata: submittedMetadata,
	}))

	submitted := CityDevelopmentStatusSubmitted
	require.NoError(t, applyCityDevelopmentFactToHashState(state, CityDevelopmentFact{
		Tick: 2, Sequence: 1, ProjectCode: project.Code,
		FactType: CityDevelopmentFactApproved, FromStatus: &submitted,
		ToStatus: CityDevelopmentStatusApproved, ProjectVersionBefore: 1,
		ProjectVersionAfter: 2, Metadata: json.RawMessage(`{"schema_version":1}`),
	}))
	approved := CityDevelopmentStatusApproved
	require.NoError(t, applyCityDevelopmentFactToHashState(state, CityDevelopmentFact{
		Tick: 3, Sequence: 1, ProjectCode: project.Code,
		FactType: CityDevelopmentFactStarted, FromStatus: &approved,
		ToStatus:             CityDevelopmentStatusUnderConstruction,
		ProjectVersionBefore: 2, ProjectVersionAfter: 3,
		Metadata: json.RawMessage(`{"started_tick":3,"planned_completion_tick":5,"schema_version":1}`),
	}))
	constructing := CityDevelopmentStatusUnderConstruction
	require.NoError(t, applyCityDevelopmentFactToHashState(state, CityDevelopmentFact{
		Tick: 4, Sequence: 1, ProjectCode: project.Code,
		FactType: CityDevelopmentFactProgressed, FromStatus: &constructing,
		ToStatus:            CityDevelopmentStatusUnderConstruction,
		ProgressBeforeMilli: 0, ProgressAfterMilli: 500,
		ProjectVersionBefore: 3, ProjectVersionAfter: 4,
		Metadata: json.RawMessage(`{"schema_version":1}`),
	}))
	adjustment := CityBuildingAdjustment{
		ProjectCode: project.Code, BuildingCode: project.BuildingCode,
		DistrictCode: project.DistrictCode, AddedFloorCount: 2, AddedTopZ: 2,
		AddedFloorAreaSQM: 400, AddedCapacityUnits: 8, CompletedTick: 5,
		Metadata: json.RawMessage(`{"schema_version":1}`),
	}
	completedMetadata, err := json.Marshal(map[string]any{"adjustment": adjustment, "schema_version": 1})
	require.NoError(t, err)
	require.NoError(t, applyCityDevelopmentFactToHashState(state, CityDevelopmentFact{
		Tick: 5, Sequence: 1, ProjectCode: project.Code,
		FactType: CityDevelopmentFactCompleted, FromStatus: &constructing,
		ToStatus:            CityDevelopmentStatusCompleted,
		ProgressBeforeMilli: 500, ProgressAfterMilli: 1000,
		ProjectVersionBefore: 4, ProjectVersionAfter: 5, Metadata: completedMetadata,
	}))

	require.Len(t, state.Development.Projects, 1)
	require.Equal(t, CityDevelopmentStatusCompleted, state.Development.Projects[0].Status)
	require.Equal(t, int64(1000), state.Development.Projects[0].ProgressMilli)
	require.Equal(t, int64(5), *state.Development.Projects[0].CompletedTick)
	require.Len(t, state.Development.Facts, 5)
	require.Len(t, state.Development.Adjustments, 1)
	require.Equal(t, int64(1), state.Development.Profile.ProjectCount)
	require.Equal(t, int64(5), state.Development.Profile.FactCount)
	require.Equal(t, int64(1), state.Development.Profile.AdjustmentCount)
	require.Equal(t, int64(5), state.Development.Profile.Revision)
	require.Equal(t, int64(28), state.Physical.Districts[0].ResidentialCapacity)
}

func TestCityDevelopmentStatusFilterAcceptsOnlyProtocolStates(t *testing.T) {
	for _, status := range []string{
		"", CityDevelopmentStatusSubmitted, CityDevelopmentStatusApproved,
		CityDevelopmentStatusRejected, CityDevelopmentStatusUnderConstruction,
		CityDevelopmentStatusCompleted, CityDevelopmentStatusCancelled,
	} {
		require.True(t, isCityDevelopmentStatusFilter(status), status)
	}
	require.False(t, isCityDevelopmentStatusFilter("pending"))
}
