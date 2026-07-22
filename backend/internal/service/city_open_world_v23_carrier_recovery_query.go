package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// GetCityOpenWorldCarrierRecoveryState exposes V23's audited recovery evidence
// without exposing reserve balances, funding controls, credentials, or any
// upstream account information. Ordinary members receive only recoveries for
// firms they own; world owners and system administrators receive the complete
// administrative projection.
func (s *CityEconomyService) GetCityOpenWorldCarrierRecoveryState(
	ctx context.Context,
	userID, worldID int64,
) (*CityOpenWorldCarrierRecoveryState, error) {
	if userID <= 0 || worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, userID, worldID); err != nil {
		return nil, err
	}
	var version string
	if err := s.db.QueryRowContext(ctx, `SELECT simulation_version FROM city_worlds WHERE id = $1`, worldID).Scan(&version); err != nil {
		return nil, fmt.Errorf("load V23 carrier-recovery world version: %w", err)
	}
	if !cityEngineSupportsOpenWorldCarrierRecovery(version) {
		return nil, ErrCitySimulationVersion.WithMetadata(map[string]string{"version": version})
	}
	state, err := loadCityOpenWorldCarrierRecoveryState(ctx, s.db, worldID)
	if err != nil {
		return nil, err
	}
	fullRead, ownedFirmCodes, err := s.cityOpenWorldSupplyChainReadScope(ctx, userID, worldID)
	if err != nil {
		return nil, err
	}
	if fullRead {
		return state, nil
	}
	return projectCityOpenWorldCarrierRecoveryStateForOwnedFirms(state, ownedFirmCodes), nil
}

func projectCityOpenWorldCarrierRecoveryStateForOwnedFirms(
	state *CityOpenWorldCarrierRecoveryState,
	ownedFirmCodes map[string]struct{},
) *CityOpenWorldCarrierRecoveryState {
	if state == nil {
		return nil
	}
	view := &CityOpenWorldCarrierRecoveryState{
		Policy:     state.Policy,
		Fundings:   make([]CityOpenWorldCarrierReserveFunding, 0),
		Recoveries: make([]CityOpenWorldFreightClaimRecovery, 0),
	}
	// Reserve funding and aggregate money figures are administrative-only. A
	// member can verify its own claim recovery but must not learn how much cash
	// is held for every carrier or how much other firms received.
	view.Policy.CarrierFirmCode = ""
	view.Policy.FundingCount = 0
	view.Policy.RecoveryCount = 0
	view.Policy.FundedUnits = 0
	view.Policy.RecoveredUnits = 0
	for _, recovery := range state.Recoveries {
		if _, owned := ownedFirmCodes[recovery.SellerFirmCode]; owned {
			view.Recoveries = append(view.Recoveries, recovery)
		}
	}
	return view
}

func loadCityOpenWorldCarrierRecoveryState(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*CityOpenWorldCarrierRecoveryState, error) {
	if worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	state := &CityOpenWorldCarrierRecoveryState{
		Fundings:   make([]CityOpenWorldCarrierReserveFunding, 0),
		Recoveries: make([]CityOpenWorldFreightClaimRecovery, 0),
	}
	err := queryer.QueryRowContext(ctx, `
SELECT profile_id, profile_version, content_hash, baseline_tick,
       carrier_actor_code, carrier_firm_code, funding_contract, recovery_contract,
       reserve_policy, maximum_fundings_per_tick, maximum_recoveries_per_tick,
       maximum_amount_units, funding_count, recovery_count, funded_units,
       recovered_units, revision, metadata
FROM city_open_world_carrier_recovery_profiles
WHERE world_id = $1`, worldID).Scan(
		&state.Policy.ProfileID, &state.Policy.ProfileVersion, &state.Policy.ContentHash, &state.Policy.BaselineTick,
		&state.Policy.CarrierActorCode, &state.Policy.CarrierFirmCode, &state.Policy.FundingContract, &state.Policy.RecoveryContract,
		&state.Policy.ReservePolicy, &state.Policy.MaximumFundingsPerTick, &state.Policy.MaximumRecoveriesTick,
		&state.Policy.MaximumAmountUnits, &state.Policy.FundingCount, &state.Policy.RecoveryCount,
		&state.Policy.FundedUnits, &state.Policy.RecoveredUnits, &state.Policy.Revision, &state.Policy.Metadata,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v23_carrier_recovery_profile"})
	}
	if err != nil {
		return nil, fmt.Errorf("load V23 carrier-recovery profile: %w", err)
	}
	fundingRows, err := queryer.QueryContext(ctx, `
SELECT funding.code, funding.funding_tick, command.sequence, funding.amount_units,
       journal.tick, journal.sequence, funding.metadata
FROM city_open_world_carrier_reserve_fundings funding
JOIN city_commands command
  ON command.id = funding.source_command_id AND command.world_id = funding.world_id
JOIN city_journals journal
  ON journal.id = funding.journal_id AND journal.world_id = funding.world_id
WHERE funding.world_id = $1
ORDER BY funding.funding_tick, command.sequence, funding.code`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V23 carrier-reserve fundings: %w", err)
	}
	for fundingRows.Next() {
		item := CityOpenWorldCarrierReserveFunding{}
		if err = fundingRows.Scan(&item.Code, &item.FundingTick, &item.SourceCommandSequence, &item.AmountUnits,
			&item.Journal.Tick, &item.Journal.Sequence, &item.Metadata); err != nil {
			_ = fundingRows.Close()
			return nil, fmt.Errorf("scan V23 carrier-reserve funding: %w", err)
		}
		state.Fundings = append(state.Fundings, item)
	}
	if err = closeCityRows(fundingRows, "iterate V23 carrier-reserve fundings"); err != nil {
		return nil, err
	}
	recoveryRows, err := queryer.QueryContext(ctx, `
SELECT recovery.code, recovery.claim_code, recovery.case_code, seller.code,
       recovery.recovery_tick, command.sequence, recovery.amount_units,
       journal.tick, journal.sequence, recovery.metadata
FROM city_open_world_freight_claim_recoveries recovery
JOIN city_economic_entities seller
  ON seller.id = recovery.seller_firm_entity_id AND seller.world_id = recovery.world_id
JOIN city_commands command
  ON command.id = recovery.source_command_id AND command.world_id = recovery.world_id
JOIN city_journals journal
  ON journal.id = recovery.journal_id AND journal.world_id = recovery.world_id
WHERE recovery.world_id = $1
ORDER BY recovery.recovery_tick, command.sequence, recovery.code`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V23 freight claim recoveries: %w", err)
	}
	for recoveryRows.Next() {
		item := CityOpenWorldFreightClaimRecovery{}
		if err = recoveryRows.Scan(&item.Code, &item.ClaimCode, &item.CaseCode, &item.SellerFirmCode,
			&item.RecoveryTick, &item.SourceCommandSequence, &item.AmountUnits,
			&item.Journal.Tick, &item.Journal.Sequence, &item.Metadata); err != nil {
			_ = recoveryRows.Close()
			return nil, fmt.Errorf("scan V23 freight claim recovery: %w", err)
		}
		state.Recoveries = append(state.Recoveries, item)
	}
	if err = closeCityRows(recoveryRows, "iterate V23 freight claim recoveries"); err != nil {
		return nil, err
	}
	if err = validateCityOpenWorldCarrierRecoveryState(state); err != nil {
		return nil, fmt.Errorf("validate loaded V23 carrier-recovery state: %w", err)
	}
	return state, nil
}
