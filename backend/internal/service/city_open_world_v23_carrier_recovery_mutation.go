package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	cityOpenWorldCarrierRecoveryRejectionClaimNotFound  = "CITY_CARRIER_RECOVERY_CLAIM_NOT_FOUND"
	cityOpenWorldCarrierRecoveryRejectionClaimNotOpen   = "CITY_CARRIER_RECOVERY_CLAIM_NOT_OPEN"
	cityOpenWorldCarrierRecoveryRejectionClaimInvalid   = "CITY_CARRIER_RECOVERY_CLAIM_INVALID"
	cityOpenWorldCarrierRecoveryRejectionSellerInvalid  = "CITY_CARRIER_RECOVERY_SELLER_INVALID"
	cityOpenWorldCarrierRecoveryRejectionReserveInvalid = "CITY_CARRIER_RECOVERY_RESERVE_INVALID"
	cityOpenWorldCarrierRecoveryRejectionFundingLimit   = "CITY_CARRIER_RECOVERY_FUNDING_LIMIT"
	cityOpenWorldCarrierRecoveryRejectionRecoveryLimit  = "CITY_CARRIER_RECOVERY_RECOVERY_LIMIT"
)

type cityOpenWorldCarrierReserveFundPayload struct {
	AmountUnits int64  `json:"amount_units"`
	Memo        string `json:"memo,omitempty"`
}

type cityOpenWorldFreightClaimResolvePayload struct {
	ClaimCode string `json:"claim_code"`
	Memo      string `json:"memo,omitempty"`
}

type cityOpenWorldCarrierRecoveryBusinessError struct{ code string }

func (err *cityOpenWorldCarrierRecoveryBusinessError) Error() string { return err.code }

func cityOpenWorldCarrierRecoveryReject(code string) error {
	return &cityOpenWorldCarrierRecoveryBusinessError{code: code}
}

func cityOpenWorldCarrierRecoveryBusinessRejectionCode(err error) string {
	var businessErr *cityOpenWorldCarrierRecoveryBusinessError
	if errors.As(err, &businessErr) {
		return businessErr.code
	}
	return cityLedgerBusinessRejectionCode(err)
}

// V23 commands are explicit administrative reserve operations. World owners
// may fund or settle claims; regular members can inspect only seller-scoped
// recovery evidence through the read API and can never move reserve money.
func authorizeCityOpenWorldCarrierRecoveryCommandSubmission(
	_ context.Context,
	_ citySQLQueryer,
	world *lockedCityWorld,
	_ int64,
	_ int64,
	commandType string,
	_ json.RawMessage,
) error {
	if world == nil || !isCityOpenWorldCarrierRecoveryCommand(commandType) || world.memberRole != CityMemberRoleOwner {
		return ErrCityPermissionDenied
	}
	return nil
}

func normalizeCityOpenWorldCarrierRecoveryCommand(commandType string, rawPayload json.RawMessage) (any, bool, error) {
	normalizeMemo := func(value *string) error {
		*value = strings.TrimSpace(*value)
		if utf8.RuneCountInString(*value) > 256 {
			return ErrCityInvalidInput.WithMetadata(map[string]string{"field": "memo"})
		}
		return nil
	}
	switch commandType {
	case CityCommandTypeOpenWorldCarrierReserveFund:
		value := cityOpenWorldCarrierReserveFundPayload{}
		if err := decodeStrictCityObject(rawPayload, &value); err != nil {
			return nil, true, ErrCityInvalidInput.WithCause(err)
		}
		if value.AmountUnits <= 0 || value.AmountUnits > cityOpenWorldCarrierRecoveryMaximumAmountUnits {
			return nil, true, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "amount_units"})
		}
		if err := normalizeMemo(&value.Memo); err != nil {
			return nil, true, err
		}
		return value, true, nil
	case CityCommandTypeOpenWorldFreightClaimResolve:
		value := cityOpenWorldFreightClaimResolvePayload{}
		if err := decodeStrictCityObject(rawPayload, &value); err != nil {
			return nil, true, ErrCityInvalidInput.WithCause(err)
		}
		value.ClaimCode = strings.ToLower(strings.TrimSpace(value.ClaimCode))
		if !cityOpenWorldSupplyChainCodeValid(value.ClaimCode) {
			return nil, true, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "claim_code"})
		}
		if err := normalizeMemo(&value.Memo); err != nil {
			return nil, true, err
		}
		return value, true, nil
	default:
		return nil, false, nil
	}
}

type cityOpenWorldCarrierRecoveryExecution struct {
	pending             cityPendingEvent
	nextJournalSequence int64
}

func (s *CityEconomyService) applyCityOpenWorldCarrierRecoveryCommand(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, journalSequence int64,
	ledgerUnit *cityLedgerBaseUnit,
	command *CityCommand,
) (cityOpenWorldCarrierRecoveryExecution, error) {
	const savepoint = "city_open_world_carrier_recovery_command"
	if _, err := tx.ExecContext(ctx, `SAVEPOINT `+savepoint); err != nil {
		return cityOpenWorldCarrierRecoveryExecution{}, fmt.Errorf("create V23 carrier-recovery command savepoint: %w", err)
	}
	execution, err := s.postCityOpenWorldCarrierRecoveryCommand(
		ctx, tx, worldID, targetTick, journalSequence, ledgerUnit, command,
	)
	if err != nil {
		if _, rollbackErr := tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT `+savepoint); rollbackErr != nil {
			return cityOpenWorldCarrierRecoveryExecution{}, fmt.Errorf("rollback V23 carrier-recovery command after %v: %w", err, rollbackErr)
		}
		if _, releaseErr := tx.ExecContext(ctx, `RELEASE SAVEPOINT `+savepoint); releaseErr != nil {
			return cityOpenWorldCarrierRecoveryExecution{}, fmt.Errorf("release rejected V23 carrier-recovery command: %w", releaseErr)
		}
		if code := cityOpenWorldCarrierRecoveryBusinessRejectionCode(err); code != "" {
			return cityOpenWorldCarrierRecoveryExecution{
				pending: rejectedCityCommand(command, code), nextJournalSequence: journalSequence,
			}, nil
		}
		return cityOpenWorldCarrierRecoveryExecution{}, err
	}
	if _, err = tx.ExecContext(ctx, `RELEASE SAVEPOINT `+savepoint); err != nil {
		return cityOpenWorldCarrierRecoveryExecution{}, fmt.Errorf("release V23 carrier-recovery command savepoint: %w", err)
	}
	return execution, nil
}

func (s *CityEconomyService) postCityOpenWorldCarrierRecoveryCommand(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, journalSequence int64,
	ledgerUnit *cityLedgerBaseUnit,
	command *CityCommand,
) (cityOpenWorldCarrierRecoveryExecution, error) {
	if command == nil || command.ID <= 0 || command.Sequence <= 0 || targetTick <= 0 || journalSequence <= 0 || ledgerUnit == nil {
		return cityOpenWorldCarrierRecoveryExecution{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v23_carrier_recovery_command"})
	}
	if err := ensureCityOpenWorldCarrierRecoveryEngine(ctx, tx, worldID); err != nil {
		return cityOpenWorldCarrierRecoveryExecution{}, err
	}
	if err := activateCityOpenWorldCarrierRecoveryWrite(ctx, tx, worldID); err != nil {
		return cityOpenWorldCarrierRecoveryExecution{}, err
	}
	// V23 owns the successor state transition of a V22 claim. The V22 gate is
	// intentionally activated in the same savepoint, and its migration accepts
	// V23 only for this audited successor write.
	if err := activateCityOpenWorldFreightSettlementWrite(ctx, tx, worldID); err != nil {
		return cityOpenWorldCarrierRecoveryExecution{}, err
	}
	policy, err := loadCityOpenWorldCarrierRecoveryPolicyForUpdate(ctx, tx, worldID)
	if err != nil {
		return cityOpenWorldCarrierRecoveryExecution{}, err
	}
	switch command.CommandType {
	case CityCommandTypeOpenWorldCarrierReserveFund:
		payload, decodeErr := decodeStoredCityCommandPayload[cityOpenWorldCarrierReserveFundPayload](command)
		if decodeErr != nil {
			return cityOpenWorldCarrierRecoveryExecution{}, decodeErr
		}
		return recordCityOpenWorldCarrierReserveFunding(ctx, tx, worldID, targetTick, journalSequence, ledgerUnit, policy, command, payload)
	case CityCommandTypeOpenWorldFreightClaimResolve:
		payload, decodeErr := decodeStoredCityCommandPayload[cityOpenWorldFreightClaimResolvePayload](command)
		if decodeErr != nil {
			return cityOpenWorldCarrierRecoveryExecution{}, decodeErr
		}
		return recordCityOpenWorldFreightClaimRecovery(ctx, tx, worldID, targetTick, journalSequence, ledgerUnit, policy, command, payload)
	default:
		return cityOpenWorldCarrierRecoveryExecution{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"command_type": command.CommandType})
	}
}

func ensureCityOpenWorldCarrierRecoveryEngine(ctx context.Context, tx *sql.Tx, worldID int64) error {
	var version string
	if err := tx.QueryRowContext(ctx, `SELECT simulation_version FROM city_worlds WHERE id = $1 FOR UPDATE`, worldID).Scan(&version); err != nil {
		return fmt.Errorf("lock V23 carrier-recovery world: %w", err)
	}
	if !cityEngineSupportsOpenWorldCarrierRecovery(version) {
		return ErrCityCommandVersion.WithMetadata(map[string]string{"version": version})
	}
	return nil
}

func loadCityOpenWorldCarrierRecoveryPolicyForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
) (*CityOpenWorldCarrierRecoveryPolicy, error) {
	policy := &CityOpenWorldCarrierRecoveryPolicy{}
	err := tx.QueryRowContext(ctx, `
SELECT profile_id, profile_version, content_hash, baseline_tick,
       carrier_actor_code, carrier_firm_code, funding_contract, recovery_contract,
       reserve_policy, maximum_fundings_per_tick, maximum_recoveries_per_tick,
       maximum_amount_units, funding_count, recovery_count, funded_units,
       recovered_units, revision, metadata
FROM city_open_world_carrier_recovery_profiles
WHERE world_id = $1
FOR UPDATE`, worldID).Scan(
		&policy.ProfileID, &policy.ProfileVersion, &policy.ContentHash, &policy.BaselineTick,
		&policy.CarrierActorCode, &policy.CarrierFirmCode, &policy.FundingContract, &policy.RecoveryContract,
		&policy.ReservePolicy, &policy.MaximumFundingsPerTick, &policy.MaximumRecoveriesTick,
		&policy.MaximumAmountUnits, &policy.FundingCount, &policy.RecoveryCount, &policy.FundedUnits,
		&policy.RecoveredUnits, &policy.Revision, &policy.Metadata,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v23_carrier_recovery_profile"})
	}
	if err != nil {
		return nil, fmt.Errorf("lock V23 carrier-recovery profile: %w", err)
	}
	// Locking only the profile is not sufficient: a damaged historical funding
	// or recovery row could otherwise be carried into a new mutation. The SQL
	// assertion verifies the complete append-only projection before this command
	// is allowed to add its successor evidence.
	if err = assertCityOpenWorldCarrierRecoveryFoundation(ctx, tx, worldID); err != nil {
		return nil, err
	}
	hash, hashErr := cityOpenWorldCarrierRecoveryPolicyHash()
	if hashErr != nil || policy.ProfileID != cityOpenWorldCarrierRecoveryProfileID ||
		policy.ProfileVersion != cityOpenWorldCarrierRecoveryProfileVersion || policy.ContentHash != hash ||
		policy.CarrierActorCode != cityOpenWorldEnterpriseFreightCarrierActorCode ||
		policy.CarrierFirmCode != cityOpenWorldCarrierRecoveryFirmCode ||
		policy.FundingContract != cityOpenWorldCarrierRecoveryFundingContract ||
		policy.RecoveryContract != cityOpenWorldCarrierRecoveryRecoveryContract ||
		policy.ReservePolicy != cityOpenWorldCarrierRecoveryReservePolicy ||
		policy.MaximumFundingsPerTick != cityOpenWorldCarrierRecoveryMaximumFundingsPerTick ||
		policy.MaximumRecoveriesTick != cityOpenWorldCarrierRecoveryMaximumRecoveriesTick ||
		policy.MaximumAmountUnits != cityOpenWorldCarrierRecoveryMaximumAmountUnits ||
		policy.FundingCount < 0 || policy.RecoveryCount < 0 || policy.FundedUnits < 0 ||
		policy.RecoveredUnits < 0 || policy.Revision != policy.FundingCount+policy.RecoveryCount+1 ||
		!cityOpenWorldCarrierRecoveryProfileMetadataValid(policy.Metadata) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v23_carrier_recovery_policy"})
	}
	return policy, nil
}

func updateCityOpenWorldCarrierRecoveryPolicy(
	ctx context.Context,
	tx *sql.Tx,
	worldID, fundingDelta, recoveryDelta, fundedDelta, recoveredDelta int64,
) error {
	if fundingDelta < 0 || recoveryDelta < 0 || fundedDelta < 0 || recoveredDelta < 0 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v23_carrier_recovery_policy_delta"})
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_open_world_carrier_recovery_profiles
SET funding_count = funding_count + $2,
    recovery_count = recovery_count + $3,
    funded_units = funded_units + $4,
    recovered_units = recovered_units + $5,
    revision = revision + $2 + $3,
    updated_at = NOW()
WHERE world_id = $1
  AND funding_count + $2 >= 0
  AND recovery_count + $3 >= 0
  AND funded_units + $4 >= 0
  AND recovered_units + $5 >= 0`, worldID, fundingDelta, recoveryDelta, fundedDelta, recoveredDelta)
	if err != nil {
		return fmt.Errorf("update V23 carrier-recovery profile: %w", err)
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr != nil || rows != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v23_carrier_recovery_profile_update"})
	}
	return nil
}

func recordCityOpenWorldCarrierReserveFunding(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, journalSequence int64,
	ledgerUnit *cityLedgerBaseUnit,
	policy *CityOpenWorldCarrierRecoveryPolicy,
	command *CityCommand,
	payload cityOpenWorldCarrierReserveFundPayload,
) (cityOpenWorldCarrierRecoveryExecution, error) {
	if policy == nil || command == nil || ledgerUnit == nil || payload.AmountUnits <= 0 || payload.AmountUnits > policy.MaximumAmountUnits {
		return cityOpenWorldCarrierRecoveryExecution{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v23_carrier_reserve_funding"})
	}
	var count int64
	if err := tx.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM city_open_world_carrier_reserve_fundings
WHERE world_id = $1 AND funding_tick = $2`, worldID, targetTick).Scan(&count); err != nil {
		return cityOpenWorldCarrierRecoveryExecution{}, fmt.Errorf("count V23 carrier-reserve fundings: %w", err)
	}
	if count >= int64(policy.MaximumFundingsPerTick) {
		return cityOpenWorldCarrierRecoveryExecution{}, cityOpenWorldCarrierRecoveryReject(cityOpenWorldCarrierRecoveryRejectionFundingLimit)
	}
	governmentID, err := loadCityGovernmentEntityID(ctx, tx, worldID)
	if err != nil {
		return cityOpenWorldCarrierRecoveryExecution{}, err
	}
	carrierFirmID, err := loadCityOpenWorldCarrierRecoveryFirmIDForUpdate(ctx, tx, worldID, policy.CarrierFirmCode)
	if err != nil {
		return cityOpenWorldCarrierRecoveryExecution{}, err
	}
	governmentExpense, err := loadCityLedgerAccount(ctx, tx, worldID, ledgerUnit.id, governmentID, CityEntityTypeGovernment, "subsidy_expense")
	if err != nil {
		return cityOpenWorldCarrierRecoveryExecution{}, err
	}
	governmentCash, err := loadCityLedgerAccount(ctx, tx, worldID, ledgerUnit.id, governmentID, CityEntityTypeGovernment, "cash")
	if err != nil {
		return cityOpenWorldCarrierRecoveryExecution{}, err
	}
	carrierCash, err := loadCityLedgerAccount(ctx, tx, worldID, ledgerUnit.id, carrierFirmID, CityEntityTypeFirm, "cash")
	if err != nil {
		return cityOpenWorldCarrierRecoveryExecution{}, err
	}
	carrierEquity, err := loadCityLedgerAccount(ctx, tx, worldID, ledgerUnit.id, carrierFirmID, CityEntityTypeFirm, "equity")
	if err != nil {
		return cityOpenWorldCarrierRecoveryExecution{}, err
	}
	journal, err := postCityJournal(ctx, tx, cityLedgerJournalSpec{
		worldID: worldID, unit: ledgerUnit, tick: targetTick, sequence: journalSequence,
		operationKey: "open_world.carrier_reserve.fund." + fmt.Sprintf("%d", command.Sequence),
		journalType:  "subsidy", sourceCommandID: &command.ID,
		description: "Carrier reserve funding",
		metadata: map[string]any{
			"schema_version": cityOpenWorldCarrierRecoverySchemaVersion,
			"contract":       cityOpenWorldCarrierRecoveryFundingContract,
			"carrier_firm":   carrierCash.entityCode,
			"amount_units":   payload.AmountUnits,
		},
		lines: []cityLedgerPostingLine{
			{account: governmentExpense, debitUnits: payload.AmountUnits, memo: payload.Memo},
			{account: carrierCash, debitUnits: payload.AmountUnits, memo: payload.Memo},
			{account: governmentCash, creditUnits: payload.AmountUnits, memo: payload.Memo},
			{account: carrierEquity, creditUnits: payload.AmountUnits, memo: payload.Memo},
		},
	})
	if err != nil {
		return cityOpenWorldCarrierRecoveryExecution{}, err
	}
	code := cityOpenWorldCarrierReserveFundingCode(command.Sequence)
	metadata, err := json.Marshal(map[string]any{
		"schema_version": cityOpenWorldCarrierRecoverySchemaVersion,
		"contract":       cityOpenWorldCarrierRecoveryFundingContract,
		"carrier_firm":   carrierCash.entityCode,
		"amount_units":   payload.AmountUnits,
	})
	if err != nil {
		return cityOpenWorldCarrierRecoveryExecution{}, fmt.Errorf("marshal V23 carrier-reserve funding metadata: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_carrier_reserve_fundings
    (world_id, code, funding_tick, source_command_id, amount_units, journal_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)`,
		worldID, code, targetTick, command.ID, payload.AmountUnits, journal.ID, []byte(metadata)); err != nil {
		return cityOpenWorldCarrierRecoveryExecution{}, fmt.Errorf("insert V23 carrier-reserve funding: %w", err)
	}
	if err = updateCityOpenWorldCarrierRecoveryPolicy(ctx, tx, worldID, 1, 0, payload.AmountUnits, 0); err != nil {
		return cityOpenWorldCarrierRecoveryExecution{}, err
	}
	if err = assertCityOpenWorldCarrierRecoveryFoundation(ctx, tx, worldID); err != nil {
		return cityOpenWorldCarrierRecoveryExecution{}, err
	}
	return cityOpenWorldCarrierRecoveryExecution{
		pending:             cityOpenWorldCarrierReserveFundingPending(command, code, payload.AmountUnits, journal),
		nextJournalSequence: journalSequence + 1,
	}, nil
}

func recordCityOpenWorldFreightClaimRecovery(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, journalSequence int64,
	ledgerUnit *cityLedgerBaseUnit,
	policy *CityOpenWorldCarrierRecoveryPolicy,
	command *CityCommand,
	payload cityOpenWorldFreightClaimResolvePayload,
) (cityOpenWorldCarrierRecoveryExecution, error) {
	if policy == nil || command == nil || ledgerUnit == nil || !cityOpenWorldSupplyChainCodeValid(payload.ClaimCode) {
		return cityOpenWorldCarrierRecoveryExecution{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v23_claim_recovery"})
	}
	var count int64
	if err := tx.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM city_open_world_freight_claim_recoveries
WHERE world_id = $1 AND recovery_tick = $2`, worldID, targetTick).Scan(&count); err != nil {
		return cityOpenWorldCarrierRecoveryExecution{}, fmt.Errorf("count V23 freight claim recoveries: %w", err)
	}
	if count >= int64(policy.MaximumRecoveriesTick) {
		return cityOpenWorldCarrierRecoveryExecution{}, cityOpenWorldCarrierRecoveryReject(cityOpenWorldCarrierRecoveryRejectionRecoveryLimit)
	}
	claim, err := loadCityOpenWorldCarrierClaimForRecovery(ctx, tx, worldID, payload.ClaimCode)
	if err != nil {
		return cityOpenWorldCarrierRecoveryExecution{}, err
	}
	if claim.state != cityOpenWorldFreightSettlementClaimOpen {
		return cityOpenWorldCarrierRecoveryExecution{}, cityOpenWorldCarrierRecoveryReject(cityOpenWorldCarrierRecoveryRejectionClaimNotOpen)
	}
	if claim.amountUnits <= 0 || claim.amountUnits > policy.MaximumAmountUnits || claim.createdTick > targetTick ||
		claim.liabilityParty != cityOpenWorldFreightSettlementLiabilityCarrier {
		return cityOpenWorldCarrierRecoveryExecution{}, cityOpenWorldCarrierRecoveryReject(cityOpenWorldCarrierRecoveryRejectionClaimInvalid)
	}
	carrierFirmID, err := loadCityOpenWorldCarrierRecoveryFirmIDForUpdate(ctx, tx, worldID, policy.CarrierFirmCode)
	if err != nil {
		return cityOpenWorldCarrierRecoveryExecution{}, err
	}
	sellerFirmID, err := loadCityOpenWorldCarrierRecoverySellerFirmIDForUpdate(ctx, tx, worldID, claim.sellerFirmCode)
	if err != nil {
		return cityOpenWorldCarrierRecoveryExecution{}, err
	}
	carrierExpense, err := loadCityLedgerAccount(ctx, tx, worldID, ledgerUnit.id, carrierFirmID, CityEntityTypeFirm, "transfer_expense")
	if err != nil {
		return cityOpenWorldCarrierRecoveryExecution{}, err
	}
	carrierCash, err := loadCityLedgerAccount(ctx, tx, worldID, ledgerUnit.id, carrierFirmID, CityEntityTypeFirm, "cash")
	if err != nil {
		return cityOpenWorldCarrierRecoveryExecution{}, err
	}
	sellerCash, err := loadCityLedgerAccount(ctx, tx, worldID, ledgerUnit.id, sellerFirmID, CityEntityTypeFirm, "cash")
	if err != nil {
		return cityOpenWorldCarrierRecoveryExecution{}, err
	}
	sellerIncome, err := loadCityLedgerAccount(ctx, tx, worldID, ledgerUnit.id, sellerFirmID, CityEntityTypeFirm, "other_income")
	if err != nil {
		return cityOpenWorldCarrierRecoveryExecution{}, err
	}
	journal, err := postCityJournal(ctx, tx, cityLedgerJournalSpec{
		worldID: worldID, unit: ledgerUnit, tick: targetTick, sequence: journalSequence,
		operationKey: "open_world.freight_claim.resolve." + fmt.Sprintf("%d", command.Sequence),
		journalType:  "cash_transfer", sourceCommandID: &command.ID,
		description: "Carrier claim recovery",
		metadata: map[string]any{
			"schema_version": cityOpenWorldCarrierRecoverySchemaVersion,
			"contract":       cityOpenWorldCarrierRecoveryRecoveryContract,
			"claim_code":     claim.code,
			"case_code":      claim.caseCode,
			"seller_firm":    sellerCash.entityCode,
			"carrier_firm":   carrierCash.entityCode,
			"amount_units":   claim.amountUnits,
		},
		lines: []cityLedgerPostingLine{
			{account: carrierExpense, debitUnits: claim.amountUnits, memo: payload.Memo},
			{account: sellerCash, debitUnits: claim.amountUnits, memo: payload.Memo},
			{account: carrierCash, creditUnits: claim.amountUnits, memo: payload.Memo},
			{account: sellerIncome, creditUnits: claim.amountUnits, memo: payload.Memo},
		},
	})
	if err != nil {
		return cityOpenWorldCarrierRecoveryExecution{}, err
	}
	code := cityOpenWorldFreightClaimRecoveryCode(command.Sequence)
	metadata, err := json.Marshal(map[string]any{
		"schema_version": cityOpenWorldCarrierRecoverySchemaVersion,
		"contract":       cityOpenWorldCarrierRecoveryRecoveryContract,
		"claim_code":     claim.code,
		"case_code":      claim.caseCode,
		"seller_firm":    claim.sellerFirmCode,
		"carrier_firm":   carrierCash.entityCode,
		"amount_units":   claim.amountUnits,
	})
	if err != nil {
		return cityOpenWorldCarrierRecoveryExecution{}, fmt.Errorf("marshal V23 carrier claim recovery metadata: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_freight_claim_recoveries
    (world_id, code, claim_code, case_code, seller_firm_entity_id,
     carrier_firm_entity_id, recovery_tick, source_command_id, amount_units,
     journal_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb)`,
		worldID, code, claim.code, claim.caseCode, sellerFirmID, carrierFirmID,
		targetTick, command.ID, claim.amountUnits, journal.ID, []byte(metadata)); err != nil {
		return cityOpenWorldCarrierRecoveryExecution{}, fmt.Errorf("insert V23 carrier claim recovery: %w", err)
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_open_world_freight_settlement_claims
SET state = $3
WHERE world_id = $1 AND code = $2 AND state = $4`,
		worldID, claim.code, cityOpenWorldFreightSettlementClaimResolved, cityOpenWorldFreightSettlementClaimOpen)
	if err != nil {
		return cityOpenWorldCarrierRecoveryExecution{}, fmt.Errorf("resolve V22 carrier claim: %w", err)
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr != nil || rows != 1 {
		return cityOpenWorldCarrierRecoveryExecution{}, cityOpenWorldCarrierRecoveryReject(cityOpenWorldCarrierRecoveryRejectionClaimNotOpen)
	}
	if err = updateCityOpenWorldCarrierRecoveryPolicy(ctx, tx, worldID, 0, 1, 0, claim.amountUnits); err != nil {
		return cityOpenWorldCarrierRecoveryExecution{}, err
	}
	if err = assertCityOpenWorldFreightSettlementFoundation(ctx, tx, worldID); err != nil {
		return cityOpenWorldCarrierRecoveryExecution{}, err
	}
	if err = assertCityOpenWorldCarrierRecoveryFoundation(ctx, tx, worldID); err != nil {
		return cityOpenWorldCarrierRecoveryExecution{}, err
	}
	return cityOpenWorldCarrierRecoveryExecution{
		pending:             cityOpenWorldFreightClaimRecoveryPending(command, code, claim.code, claim.caseCode, claim.sellerFirmCode, claim.amountUnits, journal),
		nextJournalSequence: journalSequence + 1,
	}, nil
}

type cityOpenWorldCarrierClaimRecord struct {
	code, caseCode, state, liabilityParty, sellerFirmCode string
	amountUnits, createdTick                              int64
}

func loadCityOpenWorldCarrierClaimForRecovery(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	claimCode string,
) (*cityOpenWorldCarrierClaimRecord, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT claim.code, claim.case_code, claim.liability_party, claim.claim_amount,
       claim.state, claim.created_tick, line.source_firm_code
FROM city_open_world_freight_settlement_claims claim
JOIN city_open_world_freight_settlement_case_lines line
  ON line.world_id = claim.world_id AND line.case_code = claim.case_code
WHERE claim.world_id = $1 AND claim.code = $2
ORDER BY line.source_line_no
FOR UPDATE OF claim, line`, worldID, claimCode)
	if err != nil {
		return nil, fmt.Errorf("lock V23 carrier claim: %w", err)
	}
	defer func() { _ = rows.Close() }()
	claim := &cityOpenWorldCarrierClaimRecord{}
	lineCount := 0
	for rows.Next() {
		item := cityOpenWorldCarrierClaimRecord{}
		if err = rows.Scan(&item.code, &item.caseCode, &item.liabilityParty, &item.amountUnits,
			&item.state, &item.createdTick, &item.sellerFirmCode); err != nil {
			return nil, fmt.Errorf("scan V23 carrier claim: %w", err)
		}
		if lineCount == 0 {
			*claim = item
		} else if item.code != claim.code || item.caseCode != claim.caseCode || item.liabilityParty != claim.liabilityParty ||
			item.amountUnits != claim.amountUnits || item.state != claim.state || item.createdTick != claim.createdTick ||
			item.sellerFirmCode != claim.sellerFirmCode {
			return nil, cityOpenWorldCarrierRecoveryReject(cityOpenWorldCarrierRecoveryRejectionClaimInvalid)
		}
		lineCount++
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate V23 carrier claim: %w", err)
	}
	if lineCount == 0 {
		return nil, cityOpenWorldCarrierRecoveryReject(cityOpenWorldCarrierRecoveryRejectionClaimNotFound)
	}
	return claim, nil
}

func loadCityOpenWorldCarrierRecoveryFirmIDForUpdate(ctx context.Context, tx *sql.Tx, worldID int64, code string) (int64, error) {
	var id int64
	var ownerUserID sql.NullInt64
	var openingPolicy string
	err := tx.QueryRowContext(ctx, `
SELECT id, owner_user_id, COALESCE(metadata ->> 'opening_policy', '')
FROM city_economic_entities
WHERE world_id = $1 AND entity_type = 'firm' AND code = $2 AND status = 'active'
FOR UPDATE`, worldID, code).Scan(&id, &ownerUserID, &openingPolicy)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, cityOpenWorldCarrierRecoveryReject(cityOpenWorldCarrierRecoveryRejectionReserveInvalid)
	}
	if err != nil {
		return 0, fmt.Errorf("lock V23 carrier reserve firm: %w", err)
	}
	if id <= 0 || ownerUserID.Valid || openingPolicy != cityOpenWorldCarrierRecoveryReservePolicy {
		return 0, cityOpenWorldCarrierRecoveryReject(cityOpenWorldCarrierRecoveryRejectionReserveInvalid)
	}
	return id, nil
}

func loadCityOpenWorldCarrierRecoverySellerFirmIDForUpdate(ctx context.Context, tx *sql.Tx, worldID int64, code string) (int64, error) {
	var id int64
	err := tx.QueryRowContext(ctx, `
SELECT id
FROM city_economic_entities
WHERE world_id = $1 AND entity_type = 'firm' AND code = $2 AND status = 'active'
FOR UPDATE`, worldID, code).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, cityOpenWorldCarrierRecoveryReject(cityOpenWorldCarrierRecoveryRejectionSellerInvalid)
	}
	if err != nil {
		return 0, fmt.Errorf("lock V23 recovery seller firm: %w", err)
	}
	return id, nil
}

func cityOpenWorldCarrierReserveFundingPending(command *CityCommand, fundingCode string, amountUnits int64, journal *CityJournal) cityPendingEvent {
	payload := map[string]any{"funding_code": fundingCode, "amount_units": amountUnits}
	result := map[string]any{"applied": true, "funding_code": fundingCode, "amount_units": amountUnits}
	if journal != nil {
		payload["journal_tick"] = journal.Tick
		payload["journal_sequence"] = journal.Sequence
		result["journal_tick"] = journal.Tick
		result["journal_sequence"] = journal.Sequence
	}
	return cityPendingEvent{command: command, status: CityCommandStatusApplied, eventType: "city.open_world.carrier_reserve.funded", payload: payload, result: result}
}

func cityOpenWorldFreightClaimRecoveryPending(
	command *CityCommand,
	recoveryCode, claimCode, caseCode, sellerFirmCode string,
	amountUnits int64,
	journal *CityJournal,
) cityPendingEvent {
	payload := map[string]any{
		"recovery_code": recoveryCode, "claim_code": claimCode, "case_code": caseCode,
		"seller_firm_code": sellerFirmCode, "amount_units": amountUnits,
	}
	result := map[string]any{
		"applied": true, "recovery_code": recoveryCode, "claim_code": claimCode,
		"case_code": caseCode, "seller_firm_code": sellerFirmCode, "amount_units": amountUnits,
	}
	if journal != nil {
		payload["journal_tick"] = journal.Tick
		payload["journal_sequence"] = journal.Sequence
		result["journal_tick"] = journal.Tick
		result["journal_sequence"] = journal.Sequence
	}
	return cityPendingEvent{command: command, status: CityCommandStatusApplied, eventType: "city.open_world.freight_claim.recovered", payload: payload, result: result}
}
