package service

import (
	"context"
	"database/sql"
	"fmt"
)

// restoreCityOpenWorldCarrierCommerceProjection restores V24 only after the
// V22 case graph, V23 reserve firm, and the journal stream have been restored.
// Snapshot state carries stable codes and journal cursors, never storage IDs.
func restoreCityOpenWorldCarrierCommerceProjection(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	carrierCommerce CityOpenWorldCarrierCommerceState,
) (int, error) {
	if err := validateCityOpenWorldCarrierCommerceState(&carrierCommerce); err != nil {
		return 0, fmt.Errorf("validate V24 carrier-commerce recovery input: %w", err)
	}
	if err := activateCityOpenWorldCarrierCommerceRecoveryWrite(ctx, tx, worldID); err != nil {
		return 0, err
	}

	count := 0
	policy := carrierCommerce.Policy
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_carrier_commerce_profiles
    (world_id, profile_id, profile_version, content_hash, baseline_tick,
     carrier_actor_code, carrier_firm_code, service_contract, payment_contract,
     fee_per_cargo_unit, maximum_contracts_per_tick, maximum_payments_per_tick,
     contract_count, payment_count, quoted_cargo_units, paid_cargo_units,
     paid_amount_units, revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14,
        $15, $16, $17, $18, $19::jsonb)`,
		worldID, policy.ProfileID, policy.ProfileVersion, policy.ContentHash,
		policy.BaselineTick, policy.CarrierActorCode, policy.CarrierFirmCode,
		policy.ServiceContract, policy.PaymentContract, policy.FeePerCargoUnit,
		policy.MaximumContractsPerTick, policy.MaximumPaymentsPerTick,
		policy.ContractCount, policy.PaymentCount, policy.QuotedCargoUnits,
		policy.PaidCargoUnits, policy.PaidAmountUnits, policy.Revision,
		[]byte(policy.Metadata)); err != nil {
		return count, fmt.Errorf("restore V24 carrier-commerce profile: %w", err)
	}
	count++

	carrierFirmID, err := loadCityOpenWorldCarrierRecoveryFirmIDForUpdate(
		ctx, tx, worldID, policy.CarrierFirmCode,
	)
	if err != nil {
		return count, fmt.Errorf("restore V24 carrier-commerce carrier firm: %w", err)
	}
	for _, contract := range carrierCommerce.Contracts {
		sellerFirmID, sellerErr := loadCityOpenWorldCarrierRecoverySellerFirmIDForUpdate(
			ctx, tx, worldID, contract.SellerFirmCode,
		)
		if sellerErr != nil {
			return count, fmt.Errorf("restore V24 carrier-service contract %s seller: %w", contract.Code, sellerErr)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_carrier_service_contracts
    (world_id, code, case_code, source_kind, source_code,
     seller_firm_entity_id, carrier_firm_entity_id, carrier_actor_code,
     source_tick, contract_tick, cargo_units, fee_per_cargo_unit,
     quoted_fee_units, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14::jsonb)`,
			worldID, contract.Code, contract.CaseCode, contract.SourceKind, contract.SourceCode,
			sellerFirmID, carrierFirmID, contract.CarrierActorCode, contract.SourceTick,
			contract.ContractTick, contract.CargoUnits, contract.FeePerCargoUnit,
			contract.QuotedFeeUnits, []byte(contract.Metadata)); err != nil {
			return count, fmt.Errorf("restore V24 carrier-service contract %s: %w", contract.Code, err)
		}
		count++
	}

	for _, payment := range carrierCommerce.Payments {
		sellerFirmID, sellerErr := loadCityOpenWorldCarrierRecoverySellerFirmIDForUpdate(
			ctx, tx, worldID, payment.SellerFirmCode,
		)
		if sellerErr != nil {
			return count, fmt.Errorf("restore V24 carrier-fee payment %s seller: %w", payment.Code, sellerErr)
		}
		journalID, journalErr := loadCityOpenWorldSupplyChainRecoveryJournalID(ctx, tx, worldID, payment.Journal)
		if journalErr != nil {
			return count, fmt.Errorf("restore V24 carrier-fee payment %s journal: %w", payment.Code, journalErr)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_carrier_fee_payments
    (world_id, code, contract_code, case_code, seller_firm_entity_id,
     carrier_firm_entity_id, payment_tick, cargo_units, amount_units,
     journal_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb)`,
			worldID, payment.Code, payment.ContractCode, payment.CaseCode,
			sellerFirmID, carrierFirmID, payment.PaymentTick, payment.CargoUnits,
			payment.AmountUnits, journalID, []byte(payment.Metadata)); err != nil {
			return count, fmt.Errorf("restore V24 carrier-fee payment %s: %w", payment.Code, err)
		}
		count++
	}

	if err := assertCityOpenWorldCarrierCommerceFoundation(ctx, tx, worldID); err != nil {
		return count, fmt.Errorf("validate restored V24 carrier-commerce foundation: %w", err)
	}
	return count, nil
}
