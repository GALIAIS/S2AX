package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/cityspatial"
	"github.com/stretchr/testify/require"
)

func TestCitySnapshotCompressionIsDeterministicAndTamperEvident(t *testing.T) {
	canonical := []byte(`{"current_tick":0,"simulation_version":"city-f5-v1"}`)
	first, err := compressCitySnapshot(canonical)
	require.NoError(t, err)
	second, err := compressCitySnapshot(canonical)
	require.NoError(t, err)
	require.Equal(t, first, second)

	stateHash := sha256.Sum256(canonical)
	payloadHash := sha256.Sum256(first)
	snapshot := &CitySnapshot{
		Tick: 0, SimulationVersion: CitySimulationVersionV1, SnapshotFormat: citySnapshotFormat,
		StateHash: hex.EncodeToString(stateHash[:]), PayloadHash: hex.EncodeToString(payloadHash[:]),
		UncompressedSize: int64(len(canonical)), CompressedSize: int64(len(first)), payload: first,
	}
	_, _, err = verifyCitySnapshot(snapshot)
	require.ErrorIs(t, err, ErrCitySnapshotIntegrity)

	fullState := cityHashState{
		SimulationVersion: CitySimulationVersionV1,
		CurrentTick:       0,
		Spatial: &citySpatialHashState{
			Profile: cityHashSpatialProfile{Metadata: json.RawMessage(`{}`)},
			Chunks:  make([]cityHashSpatialChunk, 0),
		},
		Land: &cityLandHashState{},
		Development: &cityDevelopmentHashState{
			Projects:    make([]CityDevelopmentProject, 0),
			Facts:       make([]CityDevelopmentFact, 0),
			Adjustments: make([]CityBuildingAdjustment, 0),
		},
		EnterpriseLocation: &cityEnterpriseLocationHashState{
			BaselineSites: make([]CityEnterpriseSite, 0),
			Sites:         make([]CityEnterpriseSite, 0),
			Facts:         make([]CityEnterpriseLocationFact, 0),
		},
		WorldRuntime: &worldRuntimeHashState{
			Profile:     WorldRuntimeProfile{Metadata: json.RawMessage(`{}`)},
			Definitions: make([]WorldRuntimeDefinition, 0),
			Actors:      make([]WorldActor, 0),
			Attributes:  make([]WorldActorAttribute, 0),
			Roles:       make([]WorldActorRole, 0),
			Statuses:    make([]WorldActorStatus, 0),
			Facts:       make([]WorldRuntimeFact, 0),
			Effects:     make([]WorldEffectOperation, 0),
			RuleCases:   make([]WorldRuleCase, 0),
		},
	}
	canonical, hash, err := canonicalCityHashState(fullState)
	require.NoError(t, err)
	compressed, err := compressCitySnapshot(canonical)
	require.NoError(t, err)
	payloadHash = sha256.Sum256(compressed)
	snapshot = &CitySnapshot{
		Tick: 0, SimulationVersion: CitySimulationVersionV1, SnapshotFormat: citySnapshotFormat,
		StateHash: hash, PayloadHash: hex.EncodeToString(payloadHash[:]),
		UncompressedSize: int64(len(canonical)), CompressedSize: int64(len(compressed)), payload: compressed,
	}
	decoded, decodedRaw, err := verifyCitySnapshot(snapshot)
	require.NoError(t, err)
	require.Equal(t, fullState.SimulationVersion, decoded.SimulationVersion)
	require.Equal(t, fullState.CurrentTick, decoded.CurrentTick)
	require.Equal(t, canonical, decodedRaw)

	snapshot.payload[5] ^= 0xff
	_, _, err = verifyCitySnapshot(snapshot)
	require.ErrorIs(t, err, ErrCitySnapshotIntegrity)
}

func TestCitySnapshotVerificationPreservesLegacyF5CanonicalFormat(t *testing.T) {
	legacy := cityHashStateF5{
		Name: "Legacy City", Status: CityWorldStatusPaused,
		SimulationVersion: CitySimulationVersionF5, Seed: 42, CurrentTick: 7,
		SimulatedAt: "2000-01-01T07:00:00Z", SpeedMilli: 1000, Timezone: "UTC",
		Settings:         json.RawMessage(`{"schema_version":1}`),
		MonetaryUnits:    make([]cityHashMonetaryUnit, 0),
		AccountTemplates: make([]cityHashAccountTemplate, 0),
		Entities:         make([]cityHashEntity, 0), Accounts: make([]cityHashAccount, 0),
	}
	canonical, err := json.Marshal(legacy)
	require.NoError(t, err)
	compressed, err := compressCitySnapshot(canonical)
	require.NoError(t, err)
	stateHash := sha256.Sum256(canonical)
	payloadHash := sha256.Sum256(compressed)
	snapshot := &CitySnapshot{
		Tick: legacy.CurrentTick, SimulationVersion: CitySimulationVersionF5,
		SnapshotFormat: citySnapshotFormat, StateHash: hex.EncodeToString(stateHash[:]),
		PayloadHash: hex.EncodeToString(payloadHash[:]), UncompressedSize: int64(len(canonical)),
		CompressedSize: int64(len(compressed)), payload: compressed,
	}

	decoded, raw, err := verifyCitySnapshot(snapshot)
	require.NoError(t, err)
	require.Equal(t, legacy.SimulationVersion, decoded.SimulationVersion)
	require.Equal(t, legacy.CurrentTick, decoded.CurrentTick)
	require.Equal(t, canonical, raw)
	require.Empty(t, decoded.Demography.Cohorts)
}

func TestCitySnapshotDecoderKeepsEveryF6CanonicalVersionReadable(t *testing.T) {
	for _, version := range []string{CitySimulationVersionF6, CitySimulationVersionF6V2, CitySimulationVersionF6V3} {
		t.Run(version, func(t *testing.T) {
			state := cityHashState{
				SimulationVersion: version,
				Demography: cityDemographyHashState{
					Calendar: cityDemographyHashCalendar{LocalDate: "2000-01-01"},
					Cohorts:  make([]cityDemographyHashCohort, 0),
				},
			}
			canonical, err := marshalCanonicalCityState(state)
			require.NoError(t, err)
			decoded, reencoded, err := decodeCanonicalCitySnapshot(canonical, version)
			require.NoError(t, err)
			require.Equal(t, version, decoded.SimulationVersion)
			require.Equal(t, canonical, reencoded)
		})
	}
}

func TestCitySnapshotDecoderKeepsF7SpatialCanonicalStateReadable(t *testing.T) {
	state := cityHashState{
		SimulationVersion: CitySimulationVersionF7,
		Demography: cityDemographyHashState{
			Calendar: cityDemographyHashCalendar{LocalDate: "2000-01-01"},
			Cohorts:  make([]cityDemographyHashCohort, 0),
		},
		Spatial: &citySpatialHashState{
			Profile: cityHashSpatialProfile{Metadata: json.RawMessage(`{}`)},
			Chunks:  make([]cityHashSpatialChunk, 0),
		},
	}
	canonical, err := marshalCanonicalCityState(state)
	require.NoError(t, err)
	decoded, reencoded, err := decodeCanonicalCitySnapshot(canonical, CitySimulationVersionF7)
	require.NoError(t, err)
	require.NotNil(t, decoded.Spatial)
	require.Equal(t, canonical, reencoded)

	missingSpatial, err := json.Marshal(cityHashState{SimulationVersion: CitySimulationVersionF7})
	require.NoError(t, err)
	_, _, err = decodeCanonicalCitySnapshot(missingSpatial, CitySimulationVersionF7)
	require.Error(t, err)

	legacyWithSpatial, err := json.Marshal(cityHashState{
		SimulationVersion: CitySimulationVersionF6V3,
		Spatial:           state.Spatial,
	})
	require.NoError(t, err)
	_, _, err = decodeCanonicalCitySnapshot(legacyWithSpatial, CitySimulationVersionF6V3)
	require.Error(t, err)
}

func TestCitySnapshotDecoderKeepsF7V2LandCanonicalStateReadable(t *testing.T) {
	state := cityHashState{
		SimulationVersion: CitySimulationVersionF7V2,
		Demography: cityDemographyHashState{
			Calendar: cityDemographyHashCalendar{LocalDate: "2000-01-01"},
			Cohorts:  make([]cityDemographyHashCohort, 0),
		},
		Spatial: &citySpatialHashState{
			Profile: cityHashSpatialProfile{Metadata: json.RawMessage(`{}`)},
			Chunks:  make([]cityHashSpatialChunk, 0),
		},
		Land: &cityLandHashState{
			ZoningRules:        make([]cityspatial.LandZoningRule, 0),
			Parcels:            make([]cityspatial.GeneratedParcel, 0),
			Buildings:          make([]cityspatial.GeneratedBuilding, 0),
			UnitPools:          make([]cityspatial.GeneratedBuildingUnitPool, 0),
			HousingAllocations: make([]cityspatial.GeneratedHousingAllocation, 0),
			Portals:            make([]cityspatial.GeneratedBuildingPortal, 0),
		},
	}
	canonical, err := marshalCanonicalCityState(state)
	require.NoError(t, err)
	decoded, reencoded, err := decodeCanonicalCitySnapshot(canonical, CitySimulationVersionF7V2)
	require.NoError(t, err)
	require.NotNil(t, decoded.Spatial)
	require.NotNil(t, decoded.Land)
	require.Equal(t, canonical, reencoded)

	missingLand, err := json.Marshal(cityHashState{
		SimulationVersion: CitySimulationVersionF7V2,
		Spatial:           state.Spatial,
	})
	require.NoError(t, err)
	_, _, err = decodeCanonicalCitySnapshot(missingLand, CitySimulationVersionF7V2)
	require.Error(t, err)

	legacyWithLand, err := json.Marshal(cityHashState{
		SimulationVersion: CitySimulationVersionF7,
		Spatial:           state.Spatial,
		Land:              state.Land,
	})
	require.NoError(t, err)
	_, _, err = decodeCanonicalCitySnapshot(legacyWithLand, CitySimulationVersionF7)
	require.Error(t, err)
}

func TestCitySnapshotDecoderKeepsF7V3DevelopmentCanonicalStateReadable(t *testing.T) {
	state := cityHashState{
		SimulationVersion: CitySimulationVersionF7V3,
		Demography: cityDemographyHashState{
			Calendar: cityDemographyHashCalendar{LocalDate: "2000-01-01"},
			Cohorts:  make([]cityDemographyHashCohort, 0),
		},
		Spatial: &citySpatialHashState{
			Profile: cityHashSpatialProfile{Metadata: json.RawMessage(`{}`)},
			Chunks:  make([]cityHashSpatialChunk, 0),
		},
		Land: &cityLandHashState{
			ZoningRules:        make([]cityspatial.LandZoningRule, 0),
			Parcels:            make([]cityspatial.GeneratedParcel, 0),
			Buildings:          make([]cityspatial.GeneratedBuilding, 0),
			UnitPools:          make([]cityspatial.GeneratedBuildingUnitPool, 0),
			HousingAllocations: make([]cityspatial.GeneratedHousingAllocation, 0),
			Portals:            make([]cityspatial.GeneratedBuildingPortal, 0),
		},
		Development: &cityDevelopmentHashState{
			Projects:    make([]CityDevelopmentProject, 0),
			Facts:       make([]CityDevelopmentFact, 0),
			Adjustments: make([]CityBuildingAdjustment, 0),
		},
	}
	canonical, err := marshalCanonicalCityState(state)
	require.NoError(t, err)
	decoded, reencoded, err := decodeCanonicalCitySnapshot(canonical, CitySimulationVersionF7V3)
	require.NoError(t, err)
	require.NotNil(t, decoded.Spatial)
	require.NotNil(t, decoded.Land)
	require.NotNil(t, decoded.Development)
	require.Equal(t, canonical, reencoded)

	missingDevelopment, err := json.Marshal(cityHashState{
		SimulationVersion: CitySimulationVersionF7V3,
		Spatial:           state.Spatial,
		Land:              state.Land,
	})
	require.NoError(t, err)
	_, _, err = decodeCanonicalCitySnapshot(missingDevelopment, CitySimulationVersionF7V3)
	require.Error(t, err)

	legacyWithDevelopment, err := json.Marshal(cityHashState{
		SimulationVersion: CitySimulationVersionF7V2,
		Spatial:           state.Spatial,
		Land:              state.Land,
		Development:       state.Development,
	})
	require.NoError(t, err)
	_, _, err = decodeCanonicalCitySnapshot(legacyWithDevelopment, CitySimulationVersionF7V2)
	require.Error(t, err)
}

func TestCityFirstJSONDifferenceReturnsStableJSONPointer(t *testing.T) {
	expected := []byte(`{"accounts":[{"balance":10},{"balance":20}],"name":"city"}`)
	actual := []byte(`{"accounts":[{"balance":10},{"balance":21}],"name":"city"}`)
	require.Equal(t, "/accounts/1/balance", cityFirstJSONDifference(expected, actual))
	require.Empty(t, cityFirstJSONDifference(expected, expected))
	require.Equal(t, "/a~1b/~0key", cityFirstJSONDifference(
		[]byte(`{"a/b":{"~key":1}}`), []byte(`{"a/b":{"~key":2}}`),
	))
}

func TestCityAuditDetailNormalizesAndTruncatesByUnicodeCodePoint(t *testing.T) {
	detail := "  " + strings.Repeat("城市 ", 300) + "  "
	normalized := cityAuditDetail(detail)
	require.True(t, utf8.ValidString(normalized))
	require.Equal(t, 512, utf8.RuneCountInString(normalized))
	require.NotContains(t, normalized, "  ")
}
