package service

import (
	"context"
	"database/sql"
	"fmt"
)

// restoreCityOpenWorldCarrierRecoveryProjection restores V23 only after V22
// claim rows, durable commands, and journals are available. The snapshot
// preserves public codes and cursor pairs, never incidental database IDs.
func restoreCityOpenWorldCarrierRecoveryProjection(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	carrierRecovery CityOpenWorldCarrierRecoveryState,
	commandIDs map[int64]int64,
) (int, error) {
	if err := validateCityOpenWorldCarrierRecoveryState(&carrierRecovery); err != nil {
		return 0, fmt.Errorf("validate V23 carrier-recovery recovery input: %w", err)
	}
	if err := activateCityOpenWorldCarrierRecoveryRecoveryWrite(ctx, tx, worldID); err != nil {
		return 0, err
	}

	count := 0
	policy := carrierRecovery.Policy
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_carrier_recovery_profiles
    (world_id, profile_id, profile_version, content_hash, baseline_tick,
     carrier_actor_code, carrier_firm_code, funding_contract, recovery_contract,
     reserve_policy, maximum_fundings_per_tick, maximum_recoveries_per_tick,
     maximum_amount_units, funding_count, recovery_count, funded_units,
     recovered_units, revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14,
        $15, $16, $17, $18, $19::jsonb)`,
		worldID, policy.ProfileID, policy.ProfileVersion, policy.ContentHash,
		policy.BaselineTick, policy.CarrierActorCode, policy.CarrierFirmCode,
		policy.FundingContract, policy.RecoveryContract, policy.ReservePolicy,
		policy.MaximumFundingsPerTick, policy.MaximumRecoveriesTick,
		policy.MaximumAmountUnits, policy.FundingCount, policy.RecoveryCount,
		policy.FundedUnits, policy.RecoveredUnits, policy.Revision,
		[]byte(policy.Metadata)); err != nil {
		return count, fmt.Errorf("restore V23 carrier-recovery profile: %w", err)
	}
	count++

	carrierFirmID, err := loadCityOpenWorldCarrierRecoveryFirmIDForUpdate(
		ctx, tx, worldID, policy.CarrierFirmCode,
	)
	if err != nil {
		return count, fmt.Errorf("restore V23 carrier reserve firm: %w", err)
	}
	for _, funding := range carrierRecovery.Fundings {
		sourceCommandID, found := commandIDs[funding.SourceCommandSequence]
		if !found || sourceCommandID <= 0 {
			return count, fmt.Errorf("V23 carrier-reserve funding %s source command %d is unavailable", funding.Code, funding.SourceCommandSequence)
		}
		journalID, journalErr := loadCityOpenWorldSupplyChainRecoveryJournalID(ctx, tx, worldID, funding.Journal)
		if journalErr != nil {
			return count, fmt.Errorf("restore V23 carrier-reserve funding %s journal: %w", funding.Code, journalErr)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_carrier_reserve_fundings
    (world_id, code, funding_tick, source_command_id, amount_units, journal_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)`,
			worldID, funding.Code, funding.FundingTick, sourceCommandID,
			funding.AmountUnits, journalID, []byte(funding.Metadata)); err != nil {
			return count, fmt.Errorf("restore V23 carrier-reserve funding %s: %w", funding.Code, err)
		}
		count++
	}

	for _, recovery := range carrierRecovery.Recoveries {
		sourceCommandID, found := commandIDs[recovery.SourceCommandSequence]
		if !found || sourceCommandID <= 0 {
			return count, fmt.Errorf("V23 carrier claim recovery %s source command %d is unavailable", recovery.Code, recovery.SourceCommandSequence)
		}
		journalID, journalErr := loadCityOpenWorldSupplyChainRecoveryJournalID(ctx, tx, worldID, recovery.Journal)
		if journalErr != nil {
			return count, fmt.Errorf("restore V23 carrier claim recovery %s journal: %w", recovery.Code, journalErr)
		}
		sellerFirmID, sellerErr := loadCityOpenWorldCarrierRecoverySellerFirmIDForUpdate(
			ctx, tx, worldID, recovery.SellerFirmCode,
		)
		if sellerErr != nil {
			return count, fmt.Errorf("restore V23 carrier claim recovery %s seller: %w", recovery.Code, sellerErr)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_freight_claim_recoveries
    (world_id, code, claim_code, case_code, seller_firm_entity_id,
     carrier_firm_entity_id, recovery_tick, source_command_id, amount_units,
     journal_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb)`,
			worldID, recovery.Code, recovery.ClaimCode, recovery.CaseCode,
			sellerFirmID, carrierFirmID, recovery.RecoveryTick, sourceCommandID,
			recovery.AmountUnits, journalID, []byte(recovery.Metadata)); err != nil {
			return count, fmt.Errorf("restore V23 carrier claim recovery %s: %w", recovery.Code, err)
		}
		count++
	}

	if err := assertCityOpenWorldFreightSettlementFoundation(ctx, tx, worldID); err != nil {
		return count, fmt.Errorf("validate restored V23 predecessor freight settlement: %w", err)
	}
	if err := assertCityOpenWorldCarrierRecoveryFoundation(ctx, tx, worldID); err != nil {
		return count, fmt.Errorf("validate restored V23 carrier-recovery foundation: %w", err)
	}
	return count, nil
}
