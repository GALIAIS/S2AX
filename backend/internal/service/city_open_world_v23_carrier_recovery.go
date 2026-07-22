package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
)

const (
	// CityCommandTypeOpenWorldCarrierReserveFund moves audited public funds into
	// the unowned carrier-reserve firm. It is deliberately separate from a
	// generic subsidy so the reserve never acquires an implicit opening balance.
	CityCommandTypeOpenWorldCarrierReserveFund = "open_world.carrier_reserve.fund"
	// CityCommandTypeOpenWorldFreightClaimResolve settles one open V22 carrier
	// liability claim from the reserve to the affected seller firm.
	CityCommandTypeOpenWorldFreightClaimResolve = "open_world.freight_claim.resolve"

	cityOpenWorldCarrierRecoverySchemaVersion          = 1
	cityOpenWorldCarrierRecoveryProfileID              = "sub2api-open-world-carrier-recovery"
	cityOpenWorldCarrierRecoveryProfileVersion         = "1.0.0"
	cityOpenWorldCarrierRecoveryFundingContract        = "government_to_manual_carrier_reserve_v1"
	cityOpenWorldCarrierRecoveryRecoveryContract       = "carrier_claim_to_seller_cash_recovery_v1"
	cityOpenWorldCarrierRecoveryReservePolicy          = "manual_reserve_only"
	cityOpenWorldCarrierRecoveryFirmCode               = "system_freight_reserve"
	cityOpenWorldCarrierRecoveryFirmName               = "Freight Carrier Reserve"
	cityOpenWorldCarrierRecoveryMaximumFundingsPerTick = 32
	cityOpenWorldCarrierRecoveryMaximumRecoveriesTick  = 256
	cityOpenWorldCarrierRecoveryMaximumAmountUnits     = cityMaximumTransactionUnits
)

// CityOpenWorldCarrierRecoveryPolicy is V23's sealed manual reserve and
// claim-recovery contract. It deliberately does not model insurance pricing,
// carrier fees, SLAs, or transit economics; those later layers can consume
// this auditable cash-and-claim substrate without rewriting it.
type CityOpenWorldCarrierRecoveryPolicy struct {
	ProfileID              string          `json:"profile_id"`
	ProfileVersion         string          `json:"profile_version"`
	ContentHash            string          `json:"content_hash"`
	BaselineTick           int64           `json:"baseline_tick"`
	CarrierActorCode       string          `json:"carrier_actor_code"`
	CarrierFirmCode        string          `json:"carrier_firm_code"`
	FundingContract        string          `json:"funding_contract"`
	RecoveryContract       string          `json:"recovery_contract"`
	ReservePolicy          string          `json:"reserve_policy"`
	MaximumFundingsPerTick int             `json:"maximum_fundings_per_tick"`
	MaximumRecoveriesTick  int             `json:"maximum_recoveries_per_tick"`
	MaximumAmountUnits     int64           `json:"maximum_amount_units"`
	FundingCount           int64           `json:"funding_count"`
	RecoveryCount          int64           `json:"recovery_count"`
	FundedUnits            int64           `json:"funded_units"`
	RecoveredUnits         int64           `json:"recovered_units"`
	Revision               int64           `json:"revision"`
	Metadata               json.RawMessage `json:"metadata"`
}

// CityOpenWorldCarrierReserveFunding is immutable evidence that the world
// government funded the V23 reserve. Journal cursors survive recovery while
// storage IDs deliberately do not.
type CityOpenWorldCarrierReserveFunding struct {
	Code                  string            `json:"code"`
	FundingTick           int64             `json:"funding_tick"`
	SourceCommandSequence int64             `json:"source_command_sequence"`
	AmountUnits           int64             `json:"amount_units"`
	Journal               CityJournalCursor `json:"journal"`
	Metadata              json.RawMessage   `json:"metadata"`
}

// CityOpenWorldFreightClaimRecovery is immutable, one-to-one closure evidence
// for a V22 carrier-liability claim. SellerFirmCode is retained as public
// stable identity rather than a database ID so snapshot recovery cannot leak
// incidental storage details into the canonical state.
type CityOpenWorldFreightClaimRecovery struct {
	Code                  string            `json:"code"`
	ClaimCode             string            `json:"claim_code"`
	CaseCode              string            `json:"case_code"`
	SellerFirmCode        string            `json:"seller_firm_code"`
	RecoveryTick          int64             `json:"recovery_tick"`
	SourceCommandSequence int64             `json:"source_command_sequence"`
	AmountUnits           int64             `json:"amount_units"`
	Journal               CityJournalCursor `json:"journal"`
	Metadata              json.RawMessage   `json:"metadata"`
}

type CityOpenWorldCarrierRecoveryState struct {
	Policy     CityOpenWorldCarrierRecoveryPolicy   `json:"policy"`
	Fundings   []CityOpenWorldCarrierReserveFunding `json:"fundings"`
	Recoveries []CityOpenWorldFreightClaimRecovery  `json:"recoveries"`
}

func isCityOpenWorldCarrierRecoveryCommand(commandType string) bool {
	return commandType == CityCommandTypeOpenWorldCarrierReserveFund ||
		commandType == CityCommandTypeOpenWorldFreightClaimResolve
}

func cityOpenWorldCarrierRecoveryPolicyHash() (string, error) {
	raw, err := json.Marshal(struct {
		SchemaVersion          int    `json:"schema_version"`
		ProfileID              string `json:"profile_id"`
		ProfileVersion         string `json:"profile_version"`
		CarrierActorCode       string `json:"carrier_actor_code"`
		CarrierFirmCode        string `json:"carrier_firm_code"`
		FundingContract        string `json:"funding_contract"`
		RecoveryContract       string `json:"recovery_contract"`
		ReservePolicy          string `json:"reserve_policy"`
		MaximumFundingsPerTick int    `json:"maximum_fundings_per_tick"`
		MaximumRecoveriesTick  int    `json:"maximum_recoveries_per_tick"`
		MaximumAmountUnits     int64  `json:"maximum_amount_units"`
	}{
		SchemaVersion:          cityOpenWorldCarrierRecoverySchemaVersion,
		ProfileID:              cityOpenWorldCarrierRecoveryProfileID,
		ProfileVersion:         cityOpenWorldCarrierRecoveryProfileVersion,
		CarrierActorCode:       cityOpenWorldEnterpriseFreightCarrierActorCode,
		CarrierFirmCode:        cityOpenWorldCarrierRecoveryFirmCode,
		FundingContract:        cityOpenWorldCarrierRecoveryFundingContract,
		RecoveryContract:       cityOpenWorldCarrierRecoveryRecoveryContract,
		ReservePolicy:          cityOpenWorldCarrierRecoveryReservePolicy,
		MaximumFundingsPerTick: cityOpenWorldCarrierRecoveryMaximumFundingsPerTick,
		MaximumRecoveriesTick:  cityOpenWorldCarrierRecoveryMaximumRecoveriesTick,
		MaximumAmountUnits:     cityOpenWorldCarrierRecoveryMaximumAmountUnits,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func cityOpenWorldCarrierReserveFundingCode(commandSequence int64) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("v23\\x00funding\\x00%d", commandSequence)))
	return "carrier.reserve.funding." + hex.EncodeToString(sum[:20])
}

func cityOpenWorldFreightClaimRecoveryCode(commandSequence int64) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("v23\\x00recovery\\x00%d", commandSequence)))
	return "carrier.claim.recovery." + hex.EncodeToString(sum[:20])
}

func activateCityOpenWorldCarrierRecoveryBootstrapWrite(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if _, err := tx.ExecContext(ctx, `
SELECT set_config('sub2api.city_open_world_carrier_recovery_bootstrap_world_id', $1, TRUE)`, fmt.Sprintf("%d", worldID)); err != nil {
		return fmt.Errorf("enable open-world V23 carrier-recovery bootstrap: %w", err)
	}
	return nil
}

func activateCityOpenWorldCarrierRecoveryWrite(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if _, err := tx.ExecContext(ctx, `
SELECT set_config('sub2api.city_open_world_carrier_recovery_world_id', $1, TRUE)`, fmt.Sprintf("%d", worldID)); err != nil {
		return fmt.Errorf("enable open-world V23 carrier-recovery write: %w", err)
	}
	return nil
}

func activateCityOpenWorldCarrierRecoveryRecoveryWrite(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if _, err := tx.ExecContext(ctx, `
SELECT set_config('sub2api.city_open_world_carrier_recovery_recovery_world_id', $1, TRUE)`, fmt.Sprintf("%d", worldID)); err != nil {
		return fmt.Errorf("enable open-world V23 carrier-recovery recovery: %w", err)
	}
	return nil
}

func assertCityOpenWorldCarrierRecoveryFoundation(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if _, err := tx.ExecContext(ctx, `SELECT assert_city_open_world_carrier_recovery_foundation($1)`, worldID); err != nil {
		return fmt.Errorf("validate V23 carrier-recovery foundation: %w", err)
	}
	return nil
}

// initializeCityOpenWorldV23CarrierRecoveryFoundation creates one unowned
// reserve firm and a manual-only profile. The carrier actor remains V16's
// traffic identity; conflating it with an economic firm would make future
// carrier contracts and competing operators impossible to model cleanly.
func initializeCityOpenWorldV23CarrierRecoveryFoundation(ctx context.Context, tx *sql.Tx, worldID int64) error {
	var version string
	var baselineTick int64
	if err := tx.QueryRowContext(ctx, `
SELECT simulation_version, current_tick
FROM city_worlds
WHERE id = $1
FOR UPDATE`, worldID).Scan(&version, &baselineTick); err != nil {
		return fmt.Errorf("lock V23 carrier-recovery world: %w", err)
	}
	if !cityEngineSupportsOpenWorldCarrierRecovery(version) || baselineTick < 0 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v23_carrier_recovery_world"})
	}
	if err := assertCityOpenWorldFreightSettlementFoundation(ctx, tx, worldID); err != nil {
		return fmt.Errorf("validate V23 carrier-recovery V22 prerequisite: %w", err)
	}
	if err := ensureCityOpenWorldEnterpriseFreightCarrier(ctx, tx, worldID, baselineTick); err != nil {
		return fmt.Errorf("ensure V23 carrier actor: %w", err)
	}
	if _, err := ensureCityOpenWorldCarrierRecoveryFirm(ctx, tx, worldID); err != nil {
		return err
	}
	policyHash, err := cityOpenWorldCarrierRecoveryPolicyHash()
	if err != nil {
		return fmt.Errorf("hash V23 carrier-recovery policy: %w", err)
	}
	metadata, err := json.Marshal(map[string]any{
		"schema_version":   cityOpenWorldCarrierRecoverySchemaVersion,
		"scope":            "manual_carrier_reserve_and_claim_recovery",
		"reserve_policy":   cityOpenWorldCarrierRecoveryReservePolicy,
		"claim_visibility": "seller_scoped_recovery_evidence",
	})
	if err != nil {
		return fmt.Errorf("marshal V23 carrier-recovery profile metadata: %w", err)
	}
	if err = activateCityOpenWorldCarrierRecoveryBootstrapWrite(ctx, tx, worldID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_carrier_recovery_profiles
    (world_id, profile_id, profile_version, content_hash, baseline_tick,
     carrier_actor_code, carrier_firm_code, funding_contract, recovery_contract,
     reserve_policy, maximum_fundings_per_tick, maximum_recoveries_per_tick,
     maximum_amount_units, revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, 1, $14::jsonb)`,
		worldID, cityOpenWorldCarrierRecoveryProfileID, cityOpenWorldCarrierRecoveryProfileVersion,
		policyHash, baselineTick, cityOpenWorldEnterpriseFreightCarrierActorCode,
		cityOpenWorldCarrierRecoveryFirmCode, cityOpenWorldCarrierRecoveryFundingContract,
		cityOpenWorldCarrierRecoveryRecoveryContract, cityOpenWorldCarrierRecoveryReservePolicy,
		cityOpenWorldCarrierRecoveryMaximumFundingsPerTick, cityOpenWorldCarrierRecoveryMaximumRecoveriesTick,
		cityOpenWorldCarrierRecoveryMaximumAmountUnits, []byte(metadata)); err != nil {
		return fmt.Errorf("insert V23 carrier-recovery profile: %w", err)
	}
	return assertCityOpenWorldCarrierRecoveryFoundation(ctx, tx, worldID)
}

func ensureCityOpenWorldCarrierRecoveryFirm(ctx context.Context, tx *sql.Tx, worldID int64) (int64, error) {
	var entityID int64
	var entityType, status, openingPolicy string
	var ownerUserID sql.NullInt64
	err := tx.QueryRowContext(ctx, `
SELECT id, entity_type, status, owner_user_id, COALESCE(metadata ->> 'opening_policy', '')
FROM city_economic_entities
WHERE world_id = $1 AND code = $2
FOR UPDATE`, worldID, cityOpenWorldCarrierRecoveryFirmCode).Scan(
		&entityID, &entityType, &status, &ownerUserID, &openingPolicy,
	)
	if errors.Is(err, sql.ErrNoRows) {
		metadata, marshalErr := json.Marshal(map[string]any{
			"schema_version": cityOpenWorldCarrierRecoverySchemaVersion,
			"foundation":     "open_world_carrier_recovery_v23",
			"opening_policy": cityOpenWorldCarrierRecoveryReservePolicy,
			"economic_role":  "carrier_claim_reserve",
		})
		if marshalErr != nil {
			return 0, marshalErr
		}
		if err = tx.QueryRowContext(ctx, `
INSERT INTO city_economic_entities
    (world_id, entity_type, code, name, owner_user_id, status, metadata)
VALUES ($1, 'firm', $2, $3, NULL, 'active', $4::jsonb)
RETURNING id`, worldID, cityOpenWorldCarrierRecoveryFirmCode,
			cityOpenWorldCarrierRecoveryFirmName, []byte(metadata)).Scan(&entityID); err != nil {
			return 0, fmt.Errorf("insert V23 carrier reserve firm: %w", err)
		}
	} else if err != nil {
		return 0, fmt.Errorf("load V23 carrier reserve firm: %w", err)
	} else if entityID <= 0 || entityType != CityEntityTypeFirm || status != "active" || ownerUserID.Valid ||
		openingPolicy != cityOpenWorldCarrierRecoveryReservePolicy {
		return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v23_carrier_reserve_firm"})
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_accounts
    (world_id, entity_id, entity_type, monetary_unit_id, template_id,
     allow_negative, current_balance_units, version, status, metadata)
SELECT $1, $2, 'firm', unit.id, template.id, template.allow_negative,
       0, 0, 'active', '{}'::jsonb
FROM city_monetary_units unit
JOIN city_account_templates template
  ON template.world_id = unit.world_id AND template.entity_type = 'firm'
WHERE unit.world_id = $1 AND unit.is_base AND unit.status = 'active'
ON CONFLICT DO NOTHING`, worldID, entityID); err != nil {
		return 0, fmt.Errorf("ensure V23 carrier reserve accounts: %w", err)
	}
	return entityID, nil
}

func validateCityOpenWorldCarrierRecoveryState(state *CityOpenWorldCarrierRecoveryState) error {
	if state == nil {
		return errors.New("carrier-recovery state is nil")
	}
	p := state.Policy
	hash, err := cityOpenWorldCarrierRecoveryPolicyHash()
	if err != nil {
		return err
	}
	if p.ProfileID != cityOpenWorldCarrierRecoveryProfileID ||
		p.ProfileVersion != cityOpenWorldCarrierRecoveryProfileVersion ||
		p.ContentHash != hash || p.BaselineTick < 0 ||
		p.CarrierActorCode != cityOpenWorldEnterpriseFreightCarrierActorCode ||
		p.CarrierFirmCode != cityOpenWorldCarrierRecoveryFirmCode ||
		p.FundingContract != cityOpenWorldCarrierRecoveryFundingContract ||
		p.RecoveryContract != cityOpenWorldCarrierRecoveryRecoveryContract ||
		p.ReservePolicy != cityOpenWorldCarrierRecoveryReservePolicy ||
		p.MaximumFundingsPerTick != cityOpenWorldCarrierRecoveryMaximumFundingsPerTick ||
		p.MaximumRecoveriesTick != cityOpenWorldCarrierRecoveryMaximumRecoveriesTick ||
		p.MaximumAmountUnits != cityOpenWorldCarrierRecoveryMaximumAmountUnits ||
		p.FundingCount < 0 || p.RecoveryCount < 0 || p.FundedUnits < 0 ||
		p.RecoveredUnits < 0 || p.Revision != p.FundingCount+p.RecoveryCount+1 ||
		!cityOpenWorldCarrierRecoveryProfileMetadataValid(p.Metadata) {
		return errors.New("invalid carrier-recovery policy")
	}
	if int64(len(state.Fundings)) != p.FundingCount || int64(len(state.Recoveries)) != p.RecoveryCount {
		return errors.New("carrier-recovery policy counters do not match evidence")
	}
	var funded, recovered int64
	fundingCodes := make(map[string]struct{}, len(state.Fundings))
	fundingCommands := make(map[int64]struct{}, len(state.Fundings))
	fundingsPerTick := make(map[int64]int, len(state.Fundings))
	for _, funding := range state.Fundings {
		if !cityOpenWorldSupplyChainCodeValid(funding.Code) || funding.Code != cityOpenWorldCarrierReserveFundingCode(funding.SourceCommandSequence) ||
			funding.FundingTick <= 0 || funding.SourceCommandSequence <= 0 || funding.AmountUnits <= 0 ||
			funding.AmountUnits > p.MaximumAmountUnits || funding.Journal.Tick <= 0 || funding.Journal.Sequence <= 0 ||
			!cityOpenWorldCarrierReserveFundingMetadataValid(funding.Metadata, funding) {
			return fmt.Errorf("invalid carrier-reserve funding %q", funding.Code)
		}
		if _, exists := fundingCodes[funding.Code]; exists {
			return fmt.Errorf("duplicate carrier-reserve funding %q", funding.Code)
		}
		if _, exists := fundingCommands[funding.SourceCommandSequence]; exists {
			return fmt.Errorf("duplicate carrier-reserve funding command %d", funding.SourceCommandSequence)
		}
		if funding.AmountUnits > math.MaxInt64-funded {
			return errors.New("carrier-reserve funding total overflow")
		}
		fundingCodes[funding.Code] = struct{}{}
		fundingCommands[funding.SourceCommandSequence] = struct{}{}
		fundingsPerTick[funding.FundingTick]++
		if fundingsPerTick[funding.FundingTick] > p.MaximumFundingsPerTick {
			return errors.New("carrier-reserve funding tick limit exceeded")
		}
		funded += funding.AmountUnits
	}
	recoveryCodes := make(map[string]struct{}, len(state.Recoveries))
	recoveryClaims := make(map[string]struct{}, len(state.Recoveries))
	recoveryCommands := make(map[int64]struct{}, len(state.Recoveries))
	recoveriesPerTick := make(map[int64]int, len(state.Recoveries))
	for _, recovery := range state.Recoveries {
		if !cityOpenWorldSupplyChainCodeValid(recovery.Code) ||
			recovery.Code != cityOpenWorldFreightClaimRecoveryCode(recovery.SourceCommandSequence) ||
			!cityOpenWorldSupplyChainCodeValid(recovery.ClaimCode) ||
			!cityOpenWorldSupplyChainCodeValid(recovery.CaseCode) ||
			!cityOpenWorldSupplyChainCodeValid(recovery.SellerFirmCode) ||
			recovery.RecoveryTick <= 0 || recovery.SourceCommandSequence <= 0 ||
			recovery.AmountUnits <= 0 || recovery.AmountUnits > p.MaximumAmountUnits ||
			recovery.Journal.Tick <= 0 || recovery.Journal.Sequence <= 0 ||
			!cityOpenWorldFreightClaimRecoveryMetadataValid(recovery.Metadata, recovery) {
			return fmt.Errorf("invalid carrier claim recovery %q", recovery.Code)
		}
		if _, exists := recoveryCodes[recovery.Code]; exists {
			return fmt.Errorf("duplicate carrier claim recovery %q", recovery.Code)
		}
		if _, exists := recoveryClaims[recovery.ClaimCode]; exists {
			return fmt.Errorf("duplicate carrier claim recovery %q", recovery.ClaimCode)
		}
		if _, exists := recoveryCommands[recovery.SourceCommandSequence]; exists {
			return fmt.Errorf("duplicate carrier claim recovery command %d", recovery.SourceCommandSequence)
		}
		if recovery.AmountUnits > math.MaxInt64-recovered {
			return errors.New("carrier claim recovery total overflow")
		}
		recoveryCodes[recovery.Code] = struct{}{}
		recoveryClaims[recovery.ClaimCode] = struct{}{}
		recoveryCommands[recovery.SourceCommandSequence] = struct{}{}
		recoveriesPerTick[recovery.RecoveryTick]++
		if recoveriesPerTick[recovery.RecoveryTick] > p.MaximumRecoveriesTick {
			return errors.New("carrier claim recovery tick limit exceeded")
		}
		recovered += recovery.AmountUnits
	}
	if funded != p.FundedUnits || recovered != p.RecoveredUnits || recovered > funded {
		return errors.New("carrier-recovery aggregate totals do not match evidence")
	}
	return nil
}

func cityOpenWorldCarrierRecoveryProfileMetadataValid(raw json.RawMessage) bool {
	var metadata struct {
		SchemaVersion   int    `json:"schema_version"`
		Scope           string `json:"scope"`
		ReservePolicy   string `json:"reserve_policy"`
		ClaimVisibility string `json:"claim_visibility"`
	}
	return json.Unmarshal(raw, &metadata) == nil &&
		metadata.SchemaVersion == cityOpenWorldCarrierRecoverySchemaVersion &&
		metadata.Scope == "manual_carrier_reserve_and_claim_recovery" &&
		metadata.ReservePolicy == cityOpenWorldCarrierRecoveryReservePolicy &&
		metadata.ClaimVisibility == "seller_scoped_recovery_evidence"
}

func cityOpenWorldCarrierReserveFundingMetadataValid(
	raw json.RawMessage,
	funding CityOpenWorldCarrierReserveFunding,
) bool {
	var metadata struct {
		SchemaVersion int    `json:"schema_version"`
		Contract      string `json:"contract"`
		CarrierFirm   string `json:"carrier_firm"`
		AmountUnits   int64  `json:"amount_units"`
	}
	return json.Unmarshal(raw, &metadata) == nil &&
		metadata.SchemaVersion == cityOpenWorldCarrierRecoverySchemaVersion &&
		metadata.Contract == cityOpenWorldCarrierRecoveryFundingContract &&
		metadata.CarrierFirm == cityOpenWorldCarrierRecoveryFirmCode &&
		metadata.AmountUnits == funding.AmountUnits
}

func cityOpenWorldFreightClaimRecoveryMetadataValid(
	raw json.RawMessage,
	recovery CityOpenWorldFreightClaimRecovery,
) bool {
	var metadata struct {
		SchemaVersion int    `json:"schema_version"`
		Contract      string `json:"contract"`
		ClaimCode     string `json:"claim_code"`
		CaseCode      string `json:"case_code"`
		SellerFirm    string `json:"seller_firm"`
		CarrierFirm   string `json:"carrier_firm"`
		AmountUnits   int64  `json:"amount_units"`
	}
	return json.Unmarshal(raw, &metadata) == nil &&
		metadata.SchemaVersion == cityOpenWorldCarrierRecoverySchemaVersion &&
		metadata.Contract == cityOpenWorldCarrierRecoveryRecoveryContract &&
		metadata.ClaimCode == recovery.ClaimCode && metadata.CaseCode == recovery.CaseCode &&
		metadata.SellerFirm == recovery.SellerFirmCode &&
		metadata.CarrierFirm == cityOpenWorldCarrierRecoveryFirmCode &&
		metadata.AmountUnits == recovery.AmountUnits
}
