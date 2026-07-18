package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityEnterpriseLocationPolicyHashBindsCanonicalPolicy(t *testing.T) {
	sum := sha256.Sum256([]byte(cityEnterpriseLocationPolicyCanonical))
	require.Equal(t, cityEnterpriseLocationPolicyHash, hex.EncodeToString(sum[:]))
}

func TestCityEnterpriseMinimumOccupiedUnitsUsesFixedIntegerDensity(t *testing.T) {
	tests := []struct {
		siteType                       string
		employees, capital, production int64
		want                           int64
	}{
		{CityEnterpriseSiteHeadquarters, 320, 1500, 400, 80},
		{CityEnterpriseSiteOffice, 5, 0, 0, 2},
		{CityEnterpriseSiteProduction, 0, 0, 401, 201},
		{CityEnterpriseSiteWarehouse, 0, 101, 0, 11},
		{CityEnterpriseSiteRetail, 9, 0, 0, 2},
		{CityEnterpriseSiteHeadquarters, 0, 0, 0, 1},
	}
	for _, test := range tests {
		got, err := cityEnterpriseMinimumOccupiedUnits(
			test.siteType, test.employees, test.capital, test.production,
		)
		require.NoError(t, err)
		require.Equal(t, test.want, got)
	}
	_, err := cityEnterpriseMinimumOccupiedUnits(CityEnterpriseSiteHeadquarters, math.MaxInt64, 0, 0)
	require.ErrorIs(t, err, errCityEnterprisePlacementInvalid)
	_, err = cityEnterpriseMinimumOccupiedUnits("unknown", 1, 1, 1)
	require.ErrorIs(t, err, errCityEnterprisePlacementInvalid)
}

func TestPlanInitialCityEnterpriseSitesIsStableAndCapacityConserving(t *testing.T) {
	firms := []cityEnterprisePlacementFirm{
		{EntityID: 2, Code: "firm_b", Name: "Firm B", DistrictID: 10, DistrictCode: "central", EmployeeUnits: 8, CapitalStockUnits: 10, ProductionCapacityUnits: 4},
		{EntityID: 1, Code: "firm_a", Name: "Firm A", DistrictID: 10, DistrictCode: "central", EmployeeUnits: 5, CapitalStockUnits: 10, ProductionCapacityUnits: 3},
	}
	pools := []cityEnterprisePlacementPool{
		{PoolID: 22, PoolCode: "pool_industrial_b", BuildingID: 202, BuildingCode: "industrial_b", DistrictID: 10, DistrictCode: "central", UseType: "industrial", EffectiveUnitCount: 10},
		{PoolID: 11, PoolCode: "pool_commercial_a", BuildingID: 101, BuildingCode: "commercial_a", DistrictID: 10, DistrictCode: "central", UseType: "commercial", EffectiveUnitCount: 10},
		{PoolID: 21, PoolCode: "pool_industrial_a", BuildingID: 201, BuildingCode: "industrial_a", DistrictID: 10, DistrictCode: "central", UseType: "industrial", EffectiveUnitCount: 2},
	}

	first, err := planInitialCityEnterpriseSites(7, firms, pools)
	require.NoError(t, err)
	second, err := planInitialCityEnterpriseSites(7, []cityEnterprisePlacementFirm{firms[1], firms[0]}, []cityEnterprisePlacementPool{pools[2], pools[0], pools[1]})
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.Len(t, first, 4)
	require.Equal(t, "site_firm_a_headquarters", first[0].Site.Code)
	require.Equal(t, "pool_commercial_a", first[0].Site.PoolCode)
	require.Equal(t, int64(2), first[0].Site.OccupiedUnits)
	require.Equal(t, "site_firm_a_production", first[1].Site.Code)
	require.Equal(t, "pool_industrial_a", first[1].Site.PoolCode)
	require.Equal(t, "pool_industrial_b", first[3].Site.PoolCode)

	sites := make([]CityEnterpriseSite, 0, len(first))
	for _, placement := range first {
		sites = append(sites, placement.Site)
	}
	firstHash, err := cityEnterpriseLocationBaselineHash(sites)
	require.NoError(t, err)
	secondHash, err := cityEnterpriseLocationBaselineHash([]CityEnterpriseSite{sites[3], sites[1], sites[0], sites[2]})
	require.NoError(t, err)
	require.Equal(t, firstHash, secondHash)
	require.Len(t, firstHash, 64)
}

func TestPlanInitialCityEnterpriseSitesRejectsPartialPlacement(t *testing.T) {
	_, err := planInitialCityEnterpriseSites(0, []cityEnterprisePlacementFirm{{
		EntityID: 1, Code: "firm", Name: "Firm", DistrictID: 10, DistrictCode: "central",
		EmployeeUnits: 8, ProductionCapacityUnits: 10,
	}}, []cityEnterprisePlacementPool{
		{PoolID: 1, PoolCode: "commercial", BuildingID: 1, BuildingCode: "commercial", DistrictID: 10, DistrictCode: "central", UseType: "commercial", EffectiveUnitCount: 2},
		{PoolID: 2, PoolCode: "industrial", BuildingID: 2, BuildingCode: "industrial", DistrictID: 10, DistrictCode: "central", UseType: "industrial", EffectiveUnitCount: 4},
	})
	require.ErrorIs(t, err, errCityEnterprisePlacementCapacity)
}

func TestNormalizeCityEnterpriseLocationCommandsCanonicalizesStrictIntent(t *testing.T) {
	payload, handled, err := normalizeCityEnterpriseLocationCommand(
		CityCommandTypeEnterpriseSiteOpen,
		json.RawMessage(`{"firm_entity_id":7,"pool_code":"  POOL_CENTRAL  ","site_type":" Warehouse ","name":"  Central Depot  ","target_occupied_units":12}`),
	)
	require.NoError(t, err)
	require.True(t, handled)
	require.Equal(t, cityEnterpriseSiteOpenPayload{
		FirmEntityID: 7, PoolCode: "pool_central", SiteType: CityEnterpriseSiteWarehouse,
		Name: "Central Depot", TargetOccupiedUnits: int64Pointer(12),
	}, payload)

	payload, handled, err = normalizeCityEnterpriseLocationCommand(
		CityCommandTypeEnterpriseRelocate,
		json.RawMessage(`{"firm_entity_id":7,"headquarters_pool_code":"POOL_NORTH_C","production_pool_code":"POOL_NORTH_I","reason":"  Expansion  "}`),
	)
	require.NoError(t, err)
	require.True(t, handled)
	require.Equal(t, "Expansion", payload.(cityEnterpriseRelocatePayload).Reason)

	_, handled, err = normalizeCityEnterpriseLocationCommand(
		CityCommandTypeEnterpriseSiteResize,
		json.RawMessage(`{"site_code":"site_a","target_occupied_units":0}`),
	)
	require.True(t, handled)
	require.Error(t, err)
	_, handled, err = normalizeCityEnterpriseLocationCommand(
		CityCommandTypeEnterpriseSiteClose,
		json.RawMessage(`{"site_code":"site_a","reason":"x","reason":"y"}`),
	)
	require.True(t, handled)
	require.Error(t, err)
	_, handled, err = normalizeCityEnterpriseLocationCommand("enterprise.unknown", json.RawMessage(`{}`))
	require.False(t, handled)
	require.NoError(t, err)
}

func TestCityEnterpriseLocationBaselineHashNormalizesJSONBOrdering(t *testing.T) {
	left := []CityEnterpriseSite{{Code: "site_a", Metadata: json.RawMessage(`{"schema_version":1,"policy_hash":"x"}`)}}
	right := []CityEnterpriseSite{{Code: "site_a", Metadata: json.RawMessage(`{"policy_hash":"x","schema_version":1}`)}}
	leftHash, err := cityEnterpriseLocationBaselineHash(left)
	require.NoError(t, err)
	rightHash, err := cityEnterpriseLocationBaselineHash(right)
	require.NoError(t, err)
	require.Equal(t, leftHash, rightHash)
}

func TestApplyCityEnterpriseLocationFactsRebuildsSiteLifecycle(t *testing.T) {
	active := CityEnterpriseSiteStatusActive
	closed := CityEnterpriseSiteStatusClosed
	base := CityEnterpriseSite{
		Code: "site_firm_a_headquarters", FirmEntityCode: "firm_a",
		DistrictCode: "central", BuildingCode: "building_c", PoolCode: "pool_c",
		SiteType: CityEnterpriseSiteHeadquarters, Name: "HQ", OccupiedUnits: 4,
		IsPrimary: true, Status: active, OpenedTick: 0, LastChangedTick: 0,
		Version: 1, Metadata: json.RawMessage(`{"schema_version":1}`),
	}
	state := cityHashState{
		SimulationVersion: CitySimulationVersionF7V4,
		Physical: cityPhysicalHashState{Firms: []cityHashFirm{{
			EntityCode: "firm_a", DistrictCode: "central", Version: 1,
		}}},
		EnterpriseLocation: &cityEnterpriseLocationHashState{
			Profile:       CityEnterpriseLocationProfile{SiteCount: 1, FactCount: 0, Revision: 1},
			BaselineSites: []CityEnterpriseSite{base}, Sites: []CityEnterpriseSite{base},
			Facts: make([]CityEnterpriseLocationFact, 0),
		},
	}

	opened := CityEnterpriseSite{
		Code: "enterprise_site_9", FirmEntityCode: "firm_a", DistrictCode: "central",
		BuildingCode: "building_i", PoolCode: "pool_i", SiteType: CityEnterpriseSiteWarehouse,
		Name: "Depot", OccupiedUnits: 8, Status: active, OpenedTick: 1,
		LastChangedTick: 1, Version: 1, Metadata: json.RawMessage(`{"schema_version":1}`),
	}
	require.NoError(t, applyCityEnterpriseLocationFactToHashState(&state, enterpriseLocationFactForTest(
		1, 1, "firm_a", &opened.Code, CityEnterpriseLocationFactOpened, nil, &active,
		0, 8, 0, 1, cityEnterpriseLocationFactMetadata{SchemaVersion: 1, Site: &opened},
	)))
	resized := opened
	resized.OccupiedUnits = 12
	resized.LastChangedTick = 2
	resized.Version = 2
	require.NoError(t, applyCityEnterpriseLocationFactToHashState(&state, enterpriseLocationFactForTest(
		2, 1, "firm_a", &opened.Code, CityEnterpriseLocationFactResized, &active, &active,
		8, 12, 1, 2, cityEnterpriseLocationFactMetadata{SchemaVersion: 1, Site: &resized},
	)))
	closedSite := resized
	closedSite.Status = closed
	closedSite.LastChangedTick = 3
	closedSite.ClosedTick = int64Pointer(3)
	closedSite.Version = 3
	require.NoError(t, applyCityEnterpriseLocationFactToHashState(&state, enterpriseLocationFactForTest(
		3, 1, "firm_a", &opened.Code, CityEnterpriseLocationFactClosed, &active, &closed,
		12, 0, 2, 3, cityEnterpriseLocationFactMetadata{SchemaVersion: 1, Site: &closedSite},
	)))
	require.Equal(t, int64(2), state.EnterpriseLocation.Profile.SiteCount)
	require.Equal(t, int64(3), state.EnterpriseLocation.Profile.FactCount)
	require.Equal(t, int64(4), state.EnterpriseLocation.Profile.Revision)
	closedIndex := findCityEnterpriseSite(state.EnterpriseLocation.Sites, opened.Code)
	require.GreaterOrEqual(t, closedIndex, 0)
	require.Equal(t, closed, state.EnterpriseLocation.Sites[closedIndex].Status)
}

func TestApplyCityEnterpriseRelocationFactReplacesPrimarySitesAndFirmDistrict(t *testing.T) {
	active := CityEnterpriseSiteStatusActive
	oldHQ := CityEnterpriseSite{
		Code: "site_firm_a_headquarters", FirmEntityCode: "firm_a", DistrictCode: "central",
		BuildingCode: "c", PoolCode: "cp", SiteType: CityEnterpriseSiteHeadquarters,
		Name: "HQ", OccupiedUnits: 2, IsPrimary: true, Status: active, Version: 1,
		Metadata: json.RawMessage(`{"schema_version":1}`),
	}
	oldProduction := CityEnterpriseSite{
		Code: "site_firm_a_production", FirmEntityCode: "firm_a", DistrictCode: "central",
		BuildingCode: "i", PoolCode: "ip", SiteType: CityEnterpriseSiteProduction,
		Name: "Plant", OccupiedUnits: 3, IsPrimary: true, Status: active, Version: 1,
		Metadata: json.RawMessage(`{"schema_version":1}`),
	}
	closedTick := int64(4)
	closedHQ := oldHQ
	closedHQ.Status, closedHQ.LastChangedTick, closedHQ.ClosedTick, closedHQ.Version = CityEnterpriseSiteStatusClosed, 4, &closedTick, 2
	closedProduction := oldProduction
	closedProduction.Status, closedProduction.LastChangedTick, closedProduction.ClosedTick, closedProduction.Version = CityEnterpriseSiteStatusClosed, 4, &closedTick, 2
	newHQ := oldHQ
	newHQ.Code, newHQ.DistrictCode, newHQ.BuildingCode, newHQ.PoolCode = "enterprise_site_11_headquarters", "north", "nc", "ncp"
	newHQ.OpenedTick, newHQ.LastChangedTick = 4, 4
	newProduction := oldProduction
	newProduction.Code, newProduction.DistrictCode, newProduction.BuildingCode, newProduction.PoolCode = "enterprise_site_11_production", "north", "ni", "nip"
	newProduction.OpenedTick, newProduction.LastChangedTick = 4, 4
	state := cityHashState{
		SimulationVersion: CitySimulationVersionF7V4,
		Physical:          cityPhysicalHashState{Firms: []cityHashFirm{{EntityCode: "firm_a", DistrictCode: "central", Version: 1}}},
		EnterpriseLocation: &cityEnterpriseLocationHashState{
			Profile: CityEnterpriseLocationProfile{SiteCount: 2, Revision: 1},
			Sites:   []CityEnterpriseSite{oldHQ, oldProduction}, Facts: make([]CityEnterpriseLocationFact, 0),
		},
	}
	fact := enterpriseLocationFactForTest(
		4, 1, "firm_a", nil, CityEnterpriseLocationFactRelocated, nil, nil,
		0, 0, 0, 0, cityEnterpriseLocationFactMetadata{
			SchemaVersion: 1, SitesBefore: []CityEnterpriseSite{oldHQ, oldProduction},
			SitesAfter:                 []CityEnterpriseSite{closedHQ, closedProduction, newHQ, newProduction},
			FirmBefore:                 &cityEnterpriseFirmLocationSnapshot{DistrictCode: "central", Version: 1},
			FirmAfter:                  &cityEnterpriseFirmLocationSnapshot{DistrictCode: "north", Version: 2},
			ResourceOperationSequences: []int64{3}, Reason: "move",
		},
	)
	require.NoError(t, applyCityEnterpriseLocationFactToHashState(&state, fact))
	require.Equal(t, "north", state.Physical.Firms[0].DistrictCode)
	require.Equal(t, int64(2), state.Physical.Firms[0].Version)
	require.Equal(t, int64(4), state.EnterpriseLocation.Profile.SiteCount)
	require.Len(t, state.EnterpriseLocation.Sites, 4)
}

func enterpriseLocationFactForTest(
	tick, sequence int64,
	firmCode string,
	siteCode *string,
	factType string,
	fromStatus, toStatus *string,
	occupiedBefore, occupiedAfter, versionBefore, versionAfter int64,
	metadata cityEnterpriseLocationFactMetadata,
) CityEnterpriseLocationFact {
	raw, err := json.Marshal(metadata)
	if err != nil {
		panic(err)
	}
	return CityEnterpriseLocationFact{
		Tick: tick, Sequence: sequence, SourceCommandSequence: sequence,
		FirmEntityCode: firmCode, SiteCode: siteCode, FactType: factType,
		FromStatus: fromStatus, ToStatus: toStatus,
		OccupiedBeforeUnits: occupiedBefore, OccupiedAfterUnits: occupiedAfter,
		SiteVersionBefore: versionBefore, SiteVersionAfter: versionAfter,
		Metadata: raw,
	}
}
