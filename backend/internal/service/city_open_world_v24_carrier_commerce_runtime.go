package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

type cityOpenWorldCarrierCommerceExecution struct {
	events              []worldRuntimeAutomaticEvent
	nextJournalSequence int64
}

type cityOpenWorldCarrierCommerceContractCandidate struct {
	caseCode   string
	sourceKind string
	sourceCode string
	sourceTick int64
}

type cityOpenWorldCarrierCommercePaymentCandidate struct {
	contractCode    string
	caseCode        string
	sellerFirmID    int64
	sellerFirmCode  string
	carrierFirmID   int64
	carrierFirmCode string
	cargoUnits      int64
	quotedFeeUnits  int64
	sourceTick      int64
}

func ensureCityOpenWorldCarrierCommerceEngine(ctx context.Context, tx *sql.Tx, worldID int64) error {
	var version string
	if err := tx.QueryRowContext(ctx, `SELECT simulation_version FROM city_worlds WHERE id = $1 FOR UPDATE`, worldID).Scan(&version); err != nil {
		return fmt.Errorf("lock V24 carrier-commerce world: %w", err)
	}
	if !cityEngineSupportsOpenWorldCarrierCommerce(version) {
		return ErrCitySimulationVersion.WithMetadata(map[string]string{"version": version})
	}
	return nil
}

func loadCityOpenWorldCarrierCommercePolicyForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
) (*CityOpenWorldCarrierCommercePolicy, error) {
	policy := &CityOpenWorldCarrierCommercePolicy{}
	err := tx.QueryRowContext(ctx, `
SELECT profile_id, profile_version, content_hash, baseline_tick,
       carrier_actor_code, carrier_firm_code, service_contract, payment_contract,
       fee_per_cargo_unit, maximum_contracts_per_tick, maximum_payments_per_tick,
       contract_count, payment_count, quoted_cargo_units, paid_cargo_units,
       paid_amount_units, revision, metadata
FROM city_open_world_carrier_commerce_profiles
WHERE world_id = $1
FOR UPDATE`, worldID).Scan(
		&policy.ProfileID, &policy.ProfileVersion, &policy.ContentHash,
		&policy.BaselineTick, &policy.CarrierActorCode, &policy.CarrierFirmCode,
		&policy.ServiceContract, &policy.PaymentContract, &policy.FeePerCargoUnit,
		&policy.MaximumContractsPerTick, &policy.MaximumPaymentsPerTick,
		&policy.ContractCount, &policy.PaymentCount, &policy.QuotedCargoUnits,
		&policy.PaidCargoUnits, &policy.PaidAmountUnits, &policy.Revision,
		&policy.Metadata,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v24_carrier_commerce_profile"})
	}
	if err != nil {
		return nil, fmt.Errorf("lock V24 carrier-commerce profile: %w", err)
	}
	contentHash, hashErr := cityOpenWorldCarrierCommercePolicyHash()
	if hashErr != nil || policy.ProfileID != cityOpenWorldCarrierCommerceProfileID ||
		policy.ProfileVersion != cityOpenWorldCarrierCommerceProfileVersion ||
		policy.ContentHash != contentHash || policy.BaselineTick < 0 ||
		policy.CarrierActorCode != cityOpenWorldEnterpriseFreightCarrierActorCode ||
		policy.CarrierFirmCode != cityOpenWorldCarrierRecoveryFirmCode ||
		policy.ServiceContract != cityOpenWorldCarrierCommerceContractContract ||
		policy.PaymentContract != cityOpenWorldCarrierCommercePaymentContract ||
		policy.FeePerCargoUnit != cityOpenWorldCarrierCommerceFeePerCargoUnit ||
		policy.MaximumContractsPerTick != cityOpenWorldCarrierCommerceMaximumContractsPerTick ||
		policy.MaximumPaymentsPerTick != cityOpenWorldCarrierCommerceMaximumPaymentsPerTick ||
		policy.ContractCount < 0 || policy.PaymentCount < 0 || policy.QuotedCargoUnits < 0 ||
		policy.PaidCargoUnits < 0 || policy.PaidAmountUnits < 0 ||
		policy.Revision != policy.ContractCount+policy.PaymentCount+1 ||
		!cityOpenWorldCarrierCommerceProfileMetadataValid(policy.Metadata) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v24_carrier_commerce_policy"})
	}
	if err = assertCityOpenWorldCarrierCommerceFoundation(ctx, tx, worldID); err != nil {
		return nil, err
	}
	return policy, nil
}

func updateCityOpenWorldCarrierCommercePolicy(
	ctx context.Context,
	tx *sql.Tx,
	worldID, contractDelta, paymentDelta, quotedCargoDelta, paidCargoDelta, paidAmountDelta int64,
) error {
	if contractDelta < 0 || paymentDelta < 0 || quotedCargoDelta < 0 || paidCargoDelta < 0 || paidAmountDelta < 0 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v24_carrier_commerce_policy_delta"})
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_open_world_carrier_commerce_profiles
SET contract_count = contract_count + $2,
    payment_count = payment_count + $3,
    quoted_cargo_units = quoted_cargo_units + $4,
    paid_cargo_units = paid_cargo_units + $5,
    paid_amount_units = paid_amount_units + $6,
    revision = revision + $2 + $3,
    updated_at = NOW()
WHERE world_id = $1
  AND contract_count + $2 >= 0
  AND payment_count + $3 >= 0
  AND quoted_cargo_units + $4 >= 0
  AND paid_cargo_units + $5 >= 0
  AND paid_amount_units + $6 >= 0`,
		worldID, contractDelta, paymentDelta, quotedCargoDelta, paidCargoDelta, paidAmountDelta)
	if err != nil {
		return fmt.Errorf("update V24 carrier-commerce profile: %w", err)
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr != nil || rows != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v24_carrier_commerce_profile_update"})
	}
	return nil
}

// advanceCityOpenWorldV24CarrierCommerce runs after V22 has materialized
// durable cases and before V9 scheduling. V22 receipt commands are still
// applied later in the tick, so a newly settled case cannot be charged until a
// future automatic pass.
func (s *CityEconomyService) advanceCityOpenWorldV24CarrierCommerce(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, journalSequence int64,
	ledgerUnit *cityLedgerBaseUnit,
) (cityOpenWorldCarrierCommerceExecution, error) {
	execution := cityOpenWorldCarrierCommerceExecution{
		events:              make([]worldRuntimeAutomaticEvent, 0),
		nextJournalSequence: journalSequence,
	}
	if targetTick <= 0 || journalSequence <= 0 || ledgerUnit == nil {
		return execution, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v24_carrier_commerce_advance"})
	}
	if err := ensureCityOpenWorldCarrierCommerceEngine(ctx, tx, worldID); err != nil {
		return execution, err
	}
	if err := assertCityOpenWorldCarrierRecoveryFoundation(ctx, tx, worldID); err != nil {
		return execution, fmt.Errorf("validate V24 carrier-commerce V23 prerequisite: %w", err)
	}
	if err := activateCityOpenWorldCarrierCommerceWrite(ctx, tx, worldID); err != nil {
		return execution, err
	}
	policy, err := loadCityOpenWorldCarrierCommercePolicyForUpdate(ctx, tx, worldID)
	if err != nil {
		return execution, err
	}
	if err = materializeCityOpenWorldCarrierCommerceContracts(ctx, tx, worldID, targetTick, policy, &execution); err != nil {
		return execution, err
	}
	if err = settleCityOpenWorldCarrierCommercePayments(ctx, tx, worldID, targetTick, ledgerUnit, policy, &execution); err != nil {
		return execution, err
	}
	if err = assertCityOpenWorldCarrierCommerceFoundation(ctx, tx, worldID); err != nil {
		return execution, err
	}
	return execution, nil
}

func materializeCityOpenWorldCarrierCommerceContracts(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	policy *CityOpenWorldCarrierCommercePolicy,
	execution *cityOpenWorldCarrierCommerceExecution,
) error {
	if policy == nil || execution == nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v24_carrier_commerce_contracts"})
	}
	candidates, err := loadCityOpenWorldCarrierCommerceContractCandidates(ctx, tx, worldID, policy.BaselineTick, policy.MaximumContractsPerTick)
	if err != nil {
		return err
	}
	carrierFirmID, err := loadCityOpenWorldCarrierRecoveryFirmIDForUpdate(ctx, tx, worldID, policy.CarrierFirmCode)
	if err != nil {
		return fmt.Errorf("load V24 carrier-commerce carrier firm: %w", err)
	}
	for _, candidate := range candidates {
		sellerFirmID, sellerFirmCode, cargoUnits, inputErr := loadCityOpenWorldCarrierCommerceCasePricingInput(
			ctx, tx, worldID, candidate.caseCode,
		)
		if inputErr != nil {
			return inputErr
		}
		if sellerFirmID <= 0 || sellerFirmCode == policy.CarrierFirmCode || cargoUnits <= 0 {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v24_carrier_commerce_case_input"})
		}
		quotedFee, quoteErr := cityOpenWorldCarrierCommerceQuotedFee(cargoUnits, policy.FeePerCargoUnit)
		if quoteErr != nil {
			return quoteErr
		}
		contract := CityOpenWorldCarrierServiceContract{
			Code:             cityOpenWorldCarrierServiceContractCode(candidate.caseCode),
			CaseCode:         candidate.caseCode,
			SourceKind:       candidate.sourceKind,
			SourceCode:       candidate.sourceCode,
			SellerFirmCode:   sellerFirmCode,
			CarrierFirmCode:  policy.CarrierFirmCode,
			CarrierActorCode: policy.CarrierActorCode,
			SourceTick:       candidate.sourceTick,
			ContractTick:     targetTick,
			CargoUnits:       cargoUnits,
			FeePerCargoUnit:  policy.FeePerCargoUnit,
			QuotedFeeUnits:   quotedFee,
		}
		metadata, metadataErr := json.Marshal(cityOpenWorldCarrierCommerceContractMetadata{
			SchemaVersion: cityOpenWorldCarrierCommerceSchemaVersion,
			Contract:      cityOpenWorldCarrierCommerceContractContract,
			CaseCode:      contract.CaseCode,
			SourceKind:    contract.SourceKind,
			SourceCode:    contract.SourceCode,
			SellerFirm:    contract.SellerFirmCode,
			CarrierFirm:   contract.CarrierFirmCode,
			CarrierActor:  contract.CarrierActorCode,
			CargoUnits:    contract.CargoUnits,
			FeePerUnit:    contract.FeePerCargoUnit,
			QuotedFee:     contract.QuotedFeeUnits,
		})
		if metadataErr != nil {
			return fmt.Errorf("marshal V24 carrier-commerce contract metadata: %w", metadataErr)
		}
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_carrier_service_contracts
    (world_id, code, case_code, source_kind, source_code,
     seller_firm_entity_id, carrier_firm_entity_id, carrier_actor_code,
     source_tick, contract_tick, cargo_units, fee_per_cargo_unit,
     quoted_fee_units, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14::jsonb)`,
			worldID, contract.Code, contract.CaseCode, contract.SourceKind, contract.SourceCode,
			sellerFirmID, carrierFirmID, contract.CarrierActorCode, contract.SourceTick,
			contract.ContractTick, contract.CargoUnits, contract.FeePerCargoUnit,
			contract.QuotedFeeUnits, []byte(metadata)); err != nil {
			return fmt.Errorf("insert V24 carrier-commerce contract: %w", err)
		}
		if err = updateCityOpenWorldCarrierCommercePolicy(ctx, tx, worldID, 1, 0, cargoUnits, 0, 0); err != nil {
			return err
		}
		execution.events = append(execution.events, worldRuntimeAutomaticEvent{
			eventType: "city.open_world.carrier_service.contracted",
			payload:   map[string]any{"contract_code": contract.Code, "case_code": contract.CaseCode, "quoted_fee_units": contract.QuotedFeeUnits},
		})
	}
	return nil
}

func settleCityOpenWorldCarrierCommercePayments(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick int64,
	ledgerUnit *cityLedgerBaseUnit,
	policy *CityOpenWorldCarrierCommercePolicy,
	execution *cityOpenWorldCarrierCommerceExecution,
) error {
	if policy == nil || execution == nil || ledgerUnit == nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v24_carrier_commerce_payments"})
	}
	candidates, err := loadCityOpenWorldCarrierCommercePaymentCandidates(
		ctx, tx, worldID, policy.BaselineTick, targetTick, policy.MaximumPaymentsPerTick,
	)
	if err != nil {
		return err
	}
	for _, candidate := range candidates {
		if candidate.carrierFirmCode != policy.CarrierFirmCode || candidate.sellerFirmCode == candidate.carrierFirmCode ||
			candidate.cargoUnits <= 0 || candidate.quotedFeeUnits <= 0 {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v24_carrier_commerce_payment_candidate"})
		}
		sellerExpense, accountErr := loadCityLedgerAccount(ctx, tx, worldID, ledgerUnit.id, candidate.sellerFirmID, CityEntityTypeFirm, "transfer_expense")
		if accountErr != nil {
			return accountErr
		}
		sellerCash, accountErr := loadCityLedgerAccount(ctx, tx, worldID, ledgerUnit.id, candidate.sellerFirmID, CityEntityTypeFirm, "cash")
		if accountErr != nil {
			return accountErr
		}
		carrierCash, accountErr := loadCityLedgerAccount(ctx, tx, worldID, ledgerUnit.id, candidate.carrierFirmID, CityEntityTypeFirm, "cash")
		if accountErr != nil {
			return accountErr
		}
		carrierRevenue, accountErr := loadCityLedgerAccount(ctx, tx, worldID, ledgerUnit.id, candidate.carrierFirmID, CityEntityTypeFirm, "revenue")
		if accountErr != nil {
			return accountErr
		}
		// V24 has no credit facility. A failed affordability check leaves only
		// immutable quote evidence; the stable next tick retry remains eligible.
		if sellerCash.balanceUnits < candidate.quotedFeeUnits {
			continue
		}
		journal, journalErr := postCityJournal(ctx, tx, cityLedgerJournalSpec{
			worldID: worldID, unit: ledgerUnit, tick: targetTick, sequence: execution.nextJournalSequence,
			operationKey: "open_world.carrier_fee.payment." + candidate.contractCode,
			journalType:  "freight_fee", description: "Carrier freight service fee",
			metadata: map[string]any{
				"schema_version": cityOpenWorldCarrierCommerceSchemaVersion,
				"contract":       cityOpenWorldCarrierCommercePaymentContract,
				"contract_code":  candidate.contractCode,
				"case_code":      candidate.caseCode,
				"seller_firm":    candidate.sellerFirmCode,
				"carrier_firm":   candidate.carrierFirmCode,
				"cargo_units":    candidate.cargoUnits,
				"amount_units":   candidate.quotedFeeUnits,
			},
			lines: []cityLedgerPostingLine{
				{account: sellerExpense, debitUnits: candidate.quotedFeeUnits, memo: "Carrier freight service fee"},
				{account: carrierCash, debitUnits: candidate.quotedFeeUnits, memo: "Carrier freight service fee"},
				{account: sellerCash, creditUnits: candidate.quotedFeeUnits, memo: "Carrier freight service fee"},
				{account: carrierRevenue, creditUnits: candidate.quotedFeeUnits, memo: "Carrier freight service fee"},
			},
		})
		if journalErr != nil {
			return fmt.Errorf("post V24 carrier-commerce payment journal: %w", journalErr)
		}
		payment := CityOpenWorldCarrierFeePayment{
			Code:            cityOpenWorldCarrierFeePaymentCode(candidate.contractCode),
			ContractCode:    candidate.contractCode,
			CaseCode:        candidate.caseCode,
			SellerFirmCode:  candidate.sellerFirmCode,
			CarrierFirmCode: candidate.carrierFirmCode,
			PaymentTick:     targetTick,
			CargoUnits:      candidate.cargoUnits,
			AmountUnits:     candidate.quotedFeeUnits,
			Journal:         CityJournalCursor{Tick: journal.Tick, Sequence: journal.Sequence},
		}
		metadata, metadataErr := json.Marshal(cityOpenWorldCarrierCommercePaymentMetadata{
			SchemaVersion: cityOpenWorldCarrierCommerceSchemaVersion,
			Contract:      cityOpenWorldCarrierCommercePaymentContract,
			ContractCode:  payment.ContractCode,
			CaseCode:      payment.CaseCode,
			SellerFirm:    payment.SellerFirmCode,
			CarrierFirm:   payment.CarrierFirmCode,
			CargoUnits:    payment.CargoUnits,
			AmountUnits:   payment.AmountUnits,
		})
		if metadataErr != nil {
			return fmt.Errorf("marshal V24 carrier-commerce payment metadata: %w", metadataErr)
		}
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_carrier_fee_payments
    (world_id, code, contract_code, case_code, seller_firm_entity_id,
     carrier_firm_entity_id, payment_tick, cargo_units, amount_units,
     journal_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb)`,
			worldID, payment.Code, payment.ContractCode, payment.CaseCode,
			candidate.sellerFirmID, candidate.carrierFirmID, payment.PaymentTick,
			payment.CargoUnits, payment.AmountUnits, journal.ID, []byte(metadata)); err != nil {
			return fmt.Errorf("insert V24 carrier-commerce payment: %w", err)
		}
		if err = updateCityOpenWorldCarrierCommercePolicy(ctx, tx, worldID, 0, 1, 0, payment.CargoUnits, payment.AmountUnits); err != nil {
			return err
		}
		execution.nextJournalSequence++
		execution.events = append(execution.events, worldRuntimeAutomaticEvent{
			eventType: "city.open_world.carrier_fee.paid",
			payload:   map[string]any{"payment_code": payment.Code, "contract_code": payment.ContractCode, "amount_units": payment.AmountUnits},
		})
	}
	return nil
}

func loadCityOpenWorldCarrierCommerceContractCandidates(
	ctx context.Context,
	tx *sql.Tx,
	worldID, baselineTick int64,
	limit int,
) ([]cityOpenWorldCarrierCommerceContractCandidate, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT settlement_case.code, settlement_case.source_kind, settlement_case.source_code,
       settlement_case.source_tick
FROM city_open_world_freight_settlement_cases settlement_case
WHERE settlement_case.world_id = $1
  AND settlement_case.source_tick > $2
  AND settlement_case.state = 'settled'
  AND NOT EXISTS (
      SELECT 1
      FROM city_open_world_carrier_service_contracts contract
      WHERE contract.world_id = settlement_case.world_id
        AND contract.case_code = settlement_case.code
  )
ORDER BY settlement_case.source_tick, settlement_case.code
LIMIT $3
FOR UPDATE OF settlement_case`, worldID, baselineTick, limit)
	if err != nil {
		return nil, fmt.Errorf("load V24 carrier-commerce contract candidates: %w", err)
	}
	items := make([]cityOpenWorldCarrierCommerceContractCandidate, 0)
	for rows.Next() {
		item := cityOpenWorldCarrierCommerceContractCandidate{}
		if err = rows.Scan(&item.caseCode, &item.sourceKind, &item.sourceCode, &item.sourceTick); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan V24 carrier-commerce contract candidate: %w", err)
		}
		items = append(items, item)
	}
	if err = closeCityRows(rows, "iterate V24 carrier-commerce contract candidates"); err != nil {
		return nil, err
	}
	return items, nil
}

func loadCityOpenWorldCarrierCommerceCasePricingInput(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	caseCode string,
) (int64, string, int64, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT seller.id, seller.code, line.quantity_units
FROM city_open_world_freight_settlement_case_lines line
JOIN city_economic_entities seller
  ON seller.world_id = line.world_id AND seller.code = line.source_firm_code
WHERE line.world_id = $1 AND line.case_code = $2
ORDER BY line.source_line_no
FOR UPDATE OF line, seller`, worldID, caseCode)
	if err != nil {
		return 0, "", 0, fmt.Errorf("load V24 carrier-commerce case lines: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var sellerID, cargoUnits int64
	var sellerCode string
	count := 0
	for rows.Next() {
		var currentID, quantity int64
		var currentCode string
		if err = rows.Scan(&currentID, &currentCode, &quantity); err != nil {
			return 0, "", 0, fmt.Errorf("scan V24 carrier-commerce case line: %w", err)
		}
		if count == 0 {
			sellerID, sellerCode = currentID, currentCode
		} else if sellerID != currentID || sellerCode != currentCode {
			return 0, "", 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v24_carrier_commerce_case_seller"})
		}
		cargoUnits, err = addCityLedgerUnits(cargoUnits, quantity)
		if err != nil {
			return 0, "", 0, err
		}
		count++
	}
	if err = rows.Err(); err != nil {
		return 0, "", 0, fmt.Errorf("iterate V24 carrier-commerce case lines: %w", err)
	}
	if count == 0 || sellerID <= 0 || sellerCode == "" || cargoUnits <= 0 {
		return 0, "", 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v24_carrier_commerce_case_lines"})
	}
	return sellerID, sellerCode, cargoUnits, nil
}

func loadCityOpenWorldCarrierCommercePaymentCandidates(
	ctx context.Context,
	tx *sql.Tx,
	worldID, baselineTick, targetTick int64,
	limit int,
) ([]cityOpenWorldCarrierCommercePaymentCandidate, error) {
	if targetTick <= 0 {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v24_carrier_commerce_payment_tick"})
	}
	rows, err := tx.QueryContext(ctx, `
SELECT contract.code, contract.case_code, seller.id, seller.code,
       carrier.id, carrier.code, contract.cargo_units, contract.quoted_fee_units,
       contract.source_tick
FROM city_open_world_carrier_service_contracts contract
JOIN city_open_world_freight_settlement_cases settlement_case
  ON settlement_case.world_id = contract.world_id
 AND settlement_case.code = contract.case_code
JOIN city_economic_entities seller
  ON seller.id = contract.seller_firm_entity_id AND seller.world_id = contract.world_id
JOIN city_economic_entities carrier
  ON carrier.id = contract.carrier_firm_entity_id AND carrier.world_id = contract.world_id
WHERE contract.world_id = $1
  AND contract.source_tick > $2
  AND contract.contract_tick < $3
  AND settlement_case.state = 'settled'
  AND NOT EXISTS (
      SELECT 1
      FROM city_open_world_carrier_fee_payments payment
      WHERE payment.world_id = contract.world_id
        AND payment.contract_code = contract.code
  )
ORDER BY contract.source_tick, contract.code
LIMIT $4
FOR UPDATE OF contract, settlement_case, seller, carrier`, worldID, baselineTick, targetTick, limit)
	if err != nil {
		return nil, fmt.Errorf("load V24 carrier-commerce payment candidates: %w", err)
	}
	items := make([]cityOpenWorldCarrierCommercePaymentCandidate, 0)
	for rows.Next() {
		item := cityOpenWorldCarrierCommercePaymentCandidate{}
		if err = rows.Scan(&item.contractCode, &item.caseCode, &item.sellerFirmID, &item.sellerFirmCode,
			&item.carrierFirmID, &item.carrierFirmCode, &item.cargoUnits, &item.quotedFeeUnits,
			&item.sourceTick); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan V24 carrier-commerce payment candidate: %w", err)
		}
		items = append(items, item)
	}
	if err = closeCityRows(rows, "iterate V24 carrier-commerce payment candidates"); err != nil {
		return nil, err
	}
	return items, nil
}
