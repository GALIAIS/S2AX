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
	"sort"
)

const (
	cityOpenWorldCarrierCommerceSchemaVersion  = 1
	cityOpenWorldCarrierCommerceProfileID      = "sub2api-open-world-carrier-commerce"
	cityOpenWorldCarrierCommerceProfileVersion = "1.0.0"

	cityOpenWorldCarrierCommerceContractContract = "v22_case_quoted_carrier_service_v1"
	cityOpenWorldCarrierCommercePaymentContract  = "seller_cash_per_unit_carrier_fee_v1"

	// V24 intentionally begins with a narrow deterministic rate. Route length,
	// mass, service level, insurance and credit terms are all later versioned
	// inputs; none may be inferred from a mutable route projection here.
	cityOpenWorldCarrierCommerceFeePerCargoUnit         = int64(1)
	cityOpenWorldCarrierCommerceMaximumContractsPerTick = 256
	cityOpenWorldCarrierCommerceMaximumPaymentsPerTick  = 256
)

// CityOpenWorldCarrierCommercePolicy freezes V24's fee contract. Its counters
// are append-only projections; cash, revenue and expense truth lives in the
// underlying double-entry journal and account projections.
type CityOpenWorldCarrierCommercePolicy struct {
	ProfileID               string          `json:"profile_id"`
	ProfileVersion          string          `json:"profile_version"`
	ContentHash             string          `json:"content_hash"`
	BaselineTick            int64           `json:"baseline_tick"`
	CarrierActorCode        string          `json:"carrier_actor_code"`
	CarrierFirmCode         string          `json:"carrier_firm_code"`
	ServiceContract         string          `json:"service_contract"`
	PaymentContract         string          `json:"payment_contract"`
	FeePerCargoUnit         int64           `json:"fee_per_cargo_unit"`
	MaximumContractsPerTick int             `json:"maximum_contracts_per_tick"`
	MaximumPaymentsPerTick  int             `json:"maximum_payments_per_tick"`
	ContractCount           int64           `json:"contract_count"`
	PaymentCount            int64           `json:"payment_count"`
	QuotedCargoUnits        int64           `json:"quoted_cargo_units"`
	PaidCargoUnits          int64           `json:"paid_cargo_units"`
	PaidAmountUnits         int64           `json:"paid_amount_units"`
	Revision                int64           `json:"revision"`
	Metadata                json.RawMessage `json:"metadata"`
}

// CityOpenWorldCarrierServiceContract is a one-to-one immutable quote over an
// eligible V22 settlement case. It does not own mutable delivery, inventory,
// claim or debt state.
type CityOpenWorldCarrierServiceContract struct {
	Code             string          `json:"code"`
	CaseCode         string          `json:"case_code"`
	SourceKind       string          `json:"source_kind"`
	SourceCode       string          `json:"source_code"`
	SellerFirmCode   string          `json:"seller_firm_code"`
	CarrierFirmCode  string          `json:"carrier_firm_code"`
	CarrierActorCode string          `json:"carrier_actor_code"`
	SourceTick       int64           `json:"source_tick"`
	ContractTick     int64           `json:"contract_tick"`
	CargoUnits       int64           `json:"cargo_units"`
	FeePerCargoUnit  int64           `json:"fee_per_cargo_unit"`
	QuotedFeeUnits   int64           `json:"quoted_fee_units"`
	Metadata         json.RawMessage `json:"metadata"`
}

// CityOpenWorldCarrierFeePayment is appended only when the seller can pay the
// V24 quote in full. Absence of a payment is a derived pending-settlement
// condition, never an implicit liability or negative cash balance.
type CityOpenWorldCarrierFeePayment struct {
	Code            string            `json:"code"`
	ContractCode    string            `json:"contract_code"`
	CaseCode        string            `json:"case_code"`
	SellerFirmCode  string            `json:"seller_firm_code"`
	CarrierFirmCode string            `json:"carrier_firm_code"`
	PaymentTick     int64             `json:"payment_tick"`
	CargoUnits      int64             `json:"cargo_units"`
	AmountUnits     int64             `json:"amount_units"`
	Journal         CityJournalCursor `json:"journal"`
	Metadata        json.RawMessage   `json:"metadata"`
}

type CityOpenWorldCarrierCommerceState struct {
	Policy    CityOpenWorldCarrierCommercePolicy    `json:"policy"`
	Contracts []CityOpenWorldCarrierServiceContract `json:"contracts"`
	Payments  []CityOpenWorldCarrierFeePayment      `json:"payments"`
}

type cityOpenWorldCarrierCommerceProfileMetadata struct {
	SchemaVersion int    `json:"schema_version"`
	Scope         string `json:"scope"`
	Pricing       string `json:"pricing"`
	Settlement    string `json:"settlement"`
}

type cityOpenWorldCarrierCommerceContractMetadata struct {
	SchemaVersion int    `json:"schema_version"`
	Contract      string `json:"contract"`
	CaseCode      string `json:"case_code"`
	SourceKind    string `json:"source_kind"`
	SourceCode    string `json:"source_code"`
	SellerFirm    string `json:"seller_firm"`
	CarrierFirm   string `json:"carrier_firm"`
	CarrierActor  string `json:"carrier_actor"`
	CargoUnits    int64  `json:"cargo_units"`
	FeePerUnit    int64  `json:"fee_per_cargo_unit"`
	QuotedFee     int64  `json:"quoted_fee_units"`
}

type cityOpenWorldCarrierCommercePaymentMetadata struct {
	SchemaVersion int    `json:"schema_version"`
	Contract      string `json:"contract"`
	ContractCode  string `json:"contract_code"`
	CaseCode      string `json:"case_code"`
	SellerFirm    string `json:"seller_firm"`
	CarrierFirm   string `json:"carrier_firm"`
	CargoUnits    int64  `json:"cargo_units"`
	AmountUnits   int64  `json:"amount_units"`
}

func cityOpenWorldCarrierCommercePolicyHash() (string, error) {
	raw, err := json.Marshal(struct {
		SchemaVersion           int    `json:"schema_version"`
		ProfileID               string `json:"profile_id"`
		ProfileVersion          string `json:"profile_version"`
		CarrierActorCode        string `json:"carrier_actor_code"`
		CarrierFirmCode         string `json:"carrier_firm_code"`
		ServiceContract         string `json:"service_contract"`
		PaymentContract         string `json:"payment_contract"`
		FeePerCargoUnit         int64  `json:"fee_per_cargo_unit"`
		MaximumContractsPerTick int    `json:"maximum_contracts_per_tick"`
		MaximumPaymentsPerTick  int    `json:"maximum_payments_per_tick"`
	}{
		SchemaVersion:           cityOpenWorldCarrierCommerceSchemaVersion,
		ProfileID:               cityOpenWorldCarrierCommerceProfileID,
		ProfileVersion:          cityOpenWorldCarrierCommerceProfileVersion,
		CarrierActorCode:        cityOpenWorldEnterpriseFreightCarrierActorCode,
		CarrierFirmCode:         cityOpenWorldCarrierRecoveryFirmCode,
		ServiceContract:         cityOpenWorldCarrierCommerceContractContract,
		PaymentContract:         cityOpenWorldCarrierCommercePaymentContract,
		FeePerCargoUnit:         cityOpenWorldCarrierCommerceFeePerCargoUnit,
		MaximumContractsPerTick: cityOpenWorldCarrierCommerceMaximumContractsPerTick,
		MaximumPaymentsPerTick:  cityOpenWorldCarrierCommerceMaximumPaymentsPerTick,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func cityOpenWorldCarrierServiceContractCode(caseCode string) string {
	sum := sha256.Sum256([]byte("v24\x00carrier-service-contract\x00" + caseCode))
	return "carrier.service.contract." + hex.EncodeToString(sum[:20])
}

func cityOpenWorldCarrierFeePaymentCode(contractCode string) string {
	sum := sha256.Sum256([]byte("v24\x00carrier-fee-payment\x00" + contractCode))
	return "carrier.fee.payment." + hex.EncodeToString(sum[:20])
}

func activateCityOpenWorldCarrierCommerceBootstrapWrite(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if _, err := tx.ExecContext(ctx, `
SELECT set_config('sub2api.city_open_world_carrier_commerce_bootstrap_world_id', $1, TRUE)`, fmt.Sprintf("%d", worldID)); err != nil {
		return fmt.Errorf("enable open-world V24 carrier-commerce bootstrap: %w", err)
	}
	return nil
}

func activateCityOpenWorldCarrierCommerceWrite(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if _, err := tx.ExecContext(ctx, `
SELECT set_config('sub2api.city_open_world_carrier_commerce_world_id', $1, TRUE)`, fmt.Sprintf("%d", worldID)); err != nil {
		return fmt.Errorf("enable open-world V24 carrier-commerce write: %w", err)
	}
	return nil
}

func activateCityOpenWorldCarrierCommerceRecoveryWrite(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if _, err := tx.ExecContext(ctx, `
SELECT set_config('sub2api.city_open_world_carrier_commerce_recovery_world_id', $1, TRUE)`, fmt.Sprintf("%d", worldID)); err != nil {
		return fmt.Errorf("enable open-world V24 carrier-commerce recovery: %w", err)
	}
	return nil
}

func assertCityOpenWorldCarrierCommerceFoundation(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if _, err := tx.ExecContext(ctx, `SELECT assert_city_open_world_carrier_commerce_foundation($1)`, worldID); err != nil {
		return fmt.Errorf("validate V24 carrier-commerce foundation: %w", err)
	}
	return nil
}

func initializeCityOpenWorldV24CarrierCommerceFoundation(ctx context.Context, tx *sql.Tx, worldID int64) error {
	var version string
	var baselineTick int64
	if err := tx.QueryRowContext(ctx, `
SELECT simulation_version, current_tick
FROM city_worlds
WHERE id = $1
FOR UPDATE`, worldID).Scan(&version, &baselineTick); err != nil {
		return fmt.Errorf("lock V24 carrier-commerce world: %w", err)
	}
	if !cityEngineSupportsOpenWorldCarrierCommerce(version) || baselineTick < 0 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v24_carrier_commerce_world"})
	}
	if err := assertCityOpenWorldCarrierRecoveryFoundation(ctx, tx, worldID); err != nil {
		return fmt.Errorf("validate V24 carrier-commerce V23 prerequisite: %w", err)
	}
	if err := ensureCityOpenWorldEnterpriseFreightCarrier(ctx, tx, worldID, baselineTick); err != nil {
		return fmt.Errorf("ensure V24 carrier actor: %w", err)
	}
	if _, err := ensureCityOpenWorldCarrierRecoveryFirm(ctx, tx, worldID); err != nil {
		return fmt.Errorf("ensure V24 carrier firm: %w", err)
	}
	contentHash, err := cityOpenWorldCarrierCommercePolicyHash()
	if err != nil {
		return fmt.Errorf("hash V24 carrier-commerce policy: %w", err)
	}
	metadata, err := json.Marshal(cityOpenWorldCarrierCommerceProfileMetadata{
		SchemaVersion: cityOpenWorldCarrierCommerceSchemaVersion,
		Scope:         "post_baseline_v22_case_carrier_service_fee",
		Pricing:       "fixed_per_cargo_unit_v1",
		Settlement:    "cash_only_retry_without_credit_v1",
	})
	if err != nil {
		return fmt.Errorf("marshal V24 carrier-commerce profile metadata: %w", err)
	}
	if err = activateCityOpenWorldCarrierCommerceBootstrapWrite(ctx, tx, worldID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_carrier_commerce_profiles
    (world_id, profile_id, profile_version, content_hash, baseline_tick,
     carrier_actor_code, carrier_firm_code, service_contract, payment_contract,
     fee_per_cargo_unit, maximum_contracts_per_tick, maximum_payments_per_tick,
     revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, 1, $13::jsonb)`,
		worldID, cityOpenWorldCarrierCommerceProfileID, cityOpenWorldCarrierCommerceProfileVersion,
		contentHash, baselineTick, cityOpenWorldEnterpriseFreightCarrierActorCode,
		cityOpenWorldCarrierRecoveryFirmCode, cityOpenWorldCarrierCommerceContractContract,
		cityOpenWorldCarrierCommercePaymentContract, cityOpenWorldCarrierCommerceFeePerCargoUnit,
		cityOpenWorldCarrierCommerceMaximumContractsPerTick, cityOpenWorldCarrierCommerceMaximumPaymentsPerTick,
		[]byte(metadata)); err != nil {
		return fmt.Errorf("insert V24 carrier-commerce profile: %w", err)
	}
	return assertCityOpenWorldCarrierCommerceFoundation(ctx, tx, worldID)
}

func loadCityOpenWorldCarrierCommerceState(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*CityOpenWorldCarrierCommerceState, error) {
	if worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	state := &CityOpenWorldCarrierCommerceState{
		Contracts: make([]CityOpenWorldCarrierServiceContract, 0),
		Payments:  make([]CityOpenWorldCarrierFeePayment, 0),
	}
	err := queryer.QueryRowContext(ctx, `
SELECT profile_id, profile_version, content_hash, baseline_tick,
       carrier_actor_code, carrier_firm_code, service_contract, payment_contract,
       fee_per_cargo_unit, maximum_contracts_per_tick, maximum_payments_per_tick,
       contract_count, payment_count, quoted_cargo_units, paid_cargo_units,
       paid_amount_units, revision, metadata
FROM city_open_world_carrier_commerce_profiles
WHERE world_id = $1`, worldID).Scan(
		&state.Policy.ProfileID, &state.Policy.ProfileVersion, &state.Policy.ContentHash,
		&state.Policy.BaselineTick, &state.Policy.CarrierActorCode, &state.Policy.CarrierFirmCode,
		&state.Policy.ServiceContract, &state.Policy.PaymentContract, &state.Policy.FeePerCargoUnit,
		&state.Policy.MaximumContractsPerTick, &state.Policy.MaximumPaymentsPerTick,
		&state.Policy.ContractCount, &state.Policy.PaymentCount, &state.Policy.QuotedCargoUnits,
		&state.Policy.PaidCargoUnits, &state.Policy.PaidAmountUnits, &state.Policy.Revision,
		&state.Policy.Metadata,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v24_carrier_commerce_profile"})
	}
	if err != nil {
		return nil, fmt.Errorf("load V24 carrier-commerce profile: %w", err)
	}
	contractRows, err := queryer.QueryContext(ctx, `
SELECT contract.code, contract.case_code, contract.source_kind, contract.source_code,
       seller.code, carrier.code, contract.carrier_actor_code, contract.source_tick,
       contract.contract_tick, contract.cargo_units, contract.fee_per_cargo_unit,
       contract.quoted_fee_units, contract.metadata
FROM city_open_world_carrier_service_contracts contract
JOIN city_economic_entities seller
  ON seller.id = contract.seller_firm_entity_id AND seller.world_id = contract.world_id
JOIN city_economic_entities carrier
  ON carrier.id = contract.carrier_firm_entity_id AND carrier.world_id = contract.world_id
WHERE contract.world_id = $1
ORDER BY contract.source_tick, contract.code`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V24 carrier-commerce contracts: %w", err)
	}
	for contractRows.Next() {
		item := CityOpenWorldCarrierServiceContract{}
		if err = contractRows.Scan(&item.Code, &item.CaseCode, &item.SourceKind, &item.SourceCode,
			&item.SellerFirmCode, &item.CarrierFirmCode, &item.CarrierActorCode, &item.SourceTick,
			&item.ContractTick, &item.CargoUnits, &item.FeePerCargoUnit, &item.QuotedFeeUnits,
			&item.Metadata); err != nil {
			_ = contractRows.Close()
			return nil, fmt.Errorf("scan V24 carrier-commerce contract: %w", err)
		}
		state.Contracts = append(state.Contracts, item)
	}
	if err = closeCityRows(contractRows, "iterate V24 carrier-commerce contracts"); err != nil {
		return nil, err
	}
	paymentRows, err := queryer.QueryContext(ctx, `
SELECT payment.code, payment.contract_code, payment.case_code, seller.code,
       carrier.code, payment.payment_tick, payment.cargo_units, payment.amount_units,
       journal.tick, journal.sequence, payment.metadata
FROM city_open_world_carrier_fee_payments payment
JOIN city_economic_entities seller
  ON seller.id = payment.seller_firm_entity_id AND seller.world_id = payment.world_id
JOIN city_economic_entities carrier
  ON carrier.id = payment.carrier_firm_entity_id AND carrier.world_id = payment.world_id
JOIN city_journals journal
  ON journal.id = payment.journal_id AND journal.world_id = payment.world_id
WHERE payment.world_id = $1
ORDER BY payment.payment_tick, payment.code`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V24 carrier-commerce payments: %w", err)
	}
	for paymentRows.Next() {
		item := CityOpenWorldCarrierFeePayment{}
		if err = paymentRows.Scan(&item.Code, &item.ContractCode, &item.CaseCode,
			&item.SellerFirmCode, &item.CarrierFirmCode, &item.PaymentTick, &item.CargoUnits,
			&item.AmountUnits, &item.Journal.Tick, &item.Journal.Sequence, &item.Metadata); err != nil {
			_ = paymentRows.Close()
			return nil, fmt.Errorf("scan V24 carrier-commerce payment: %w", err)
		}
		state.Payments = append(state.Payments, item)
	}
	if err = closeCityRows(paymentRows, "iterate V24 carrier-commerce payments"); err != nil {
		return nil, err
	}
	if err = validateCityOpenWorldCarrierCommerceState(state); err != nil {
		return nil, fmt.Errorf("validate loaded V24 carrier-commerce state: %w", err)
	}
	return state, nil
}

func validateCityOpenWorldCarrierCommerceState(state *CityOpenWorldCarrierCommerceState) error {
	if state == nil {
		return errors.New("carrier-commerce state is required")
	}
	p := state.Policy
	expectedHash, hashErr := cityOpenWorldCarrierCommercePolicyHash()
	if hashErr != nil {
		return hashErr
	}
	if p.ProfileID != cityOpenWorldCarrierCommerceProfileID ||
		p.ProfileVersion != cityOpenWorldCarrierCommerceProfileVersion || p.ContentHash != expectedHash ||
		p.BaselineTick < 0 || p.CarrierActorCode != cityOpenWorldEnterpriseFreightCarrierActorCode ||
		p.CarrierFirmCode != cityOpenWorldCarrierRecoveryFirmCode ||
		p.ServiceContract != cityOpenWorldCarrierCommerceContractContract ||
		p.PaymentContract != cityOpenWorldCarrierCommercePaymentContract ||
		p.FeePerCargoUnit != cityOpenWorldCarrierCommerceFeePerCargoUnit ||
		p.MaximumContractsPerTick != cityOpenWorldCarrierCommerceMaximumContractsPerTick ||
		p.MaximumPaymentsPerTick != cityOpenWorldCarrierCommerceMaximumPaymentsPerTick ||
		p.ContractCount < 0 || p.PaymentCount < 0 || p.QuotedCargoUnits < 0 ||
		p.PaidCargoUnits < 0 || p.PaidAmountUnits < 0 ||
		p.Revision != p.ContractCount+p.PaymentCount+1 ||
		!cityOpenWorldCarrierCommerceProfileMetadataValid(p.Metadata) {
		return errors.New("invalid carrier-commerce policy")
	}
	if int64(len(state.Contracts)) != p.ContractCount || int64(len(state.Payments)) != p.PaymentCount {
		return errors.New("carrier-commerce counter mismatch")
	}
	contracts := make(map[string]CityOpenWorldCarrierServiceContract, len(state.Contracts))
	caseCodes := make(map[string]struct{}, len(state.Contracts))
	var quotedCargoUnits int64
	for _, contract := range state.Contracts {
		if !cityOpenWorldSupplyChainCodeValid(contract.Code) || !cityOpenWorldSupplyChainCodeValid(contract.CaseCode) ||
			!cityOpenWorldSupplyChainCodeValid(contract.SourceCode) ||
			!cityOpenWorldSupplyChainCodeValid(contract.SellerFirmCode) ||
			contract.CarrierFirmCode != p.CarrierFirmCode || contract.CarrierActorCode != p.CarrierActorCode ||
			(contract.SourceKind != cityOpenWorldFreightSettlementSourceShipment && contract.SourceKind != cityOpenWorldFreightSettlementSourceConsignment) ||
			contract.SellerFirmCode == contract.CarrierFirmCode || contract.SourceTick <= p.BaselineTick ||
			contract.ContractTick < contract.SourceTick || contract.CargoUnits <= 0 || contract.FeePerCargoUnit != p.FeePerCargoUnit ||
			contract.Code != cityOpenWorldCarrierServiceContractCode(contract.CaseCode) ||
			!cityOpenWorldCarrierCommerceContractMetadataValid(contract.Metadata, contract) {
			return errors.New("invalid carrier-commerce contract")
		}
		quoted, err := cityOpenWorldCarrierCommerceQuotedFee(contract.CargoUnits, contract.FeePerCargoUnit)
		if err != nil || quoted != contract.QuotedFeeUnits {
			return errors.New("invalid carrier-commerce quote")
		}
		if _, exists := contracts[contract.Code]; exists {
			return errors.New("duplicate carrier-commerce contract")
		}
		if _, exists := caseCodes[contract.CaseCode]; exists {
			return errors.New("duplicate carrier-commerce case contract")
		}
		contracts[contract.Code] = contract
		caseCodes[contract.CaseCode] = struct{}{}
		quotedCargoUnits, err = addCityLedgerUnits(quotedCargoUnits, contract.CargoUnits)
		if err != nil {
			return err
		}
	}
	if quotedCargoUnits != p.QuotedCargoUnits {
		return errors.New("carrier-commerce quoted cargo counter mismatch")
	}
	payments := make(map[string]struct{}, len(state.Payments))
	var paidCargoUnits, paidAmountUnits int64
	for _, payment := range state.Payments {
		contract, found := contracts[payment.ContractCode]
		if !found || !cityOpenWorldSupplyChainCodeValid(payment.Code) || payment.Code != cityOpenWorldCarrierFeePaymentCode(payment.ContractCode) ||
			payment.CaseCode != contract.CaseCode || payment.SellerFirmCode != contract.SellerFirmCode ||
			payment.CarrierFirmCode != contract.CarrierFirmCode || payment.PaymentTick <= contract.ContractTick ||
			payment.CargoUnits != contract.CargoUnits || payment.AmountUnits != contract.QuotedFeeUnits ||
			payment.Journal.Tick != payment.PaymentTick || payment.Journal.Sequence <= 0 ||
			!cityOpenWorldCarrierCommercePaymentMetadataValid(payment.Metadata, payment) {
			return errors.New("invalid carrier-commerce payment")
		}
		if _, exists := payments[payment.ContractCode]; exists {
			return errors.New("duplicate carrier-commerce payment")
		}
		payments[payment.ContractCode] = struct{}{}
		var err error
		paidCargoUnits, err = addCityLedgerUnits(paidCargoUnits, payment.CargoUnits)
		if err != nil {
			return err
		}
		paidAmountUnits, err = addCityLedgerUnits(paidAmountUnits, payment.AmountUnits)
		if err != nil {
			return err
		}
	}
	quotedAmountLimit := int64(0)
	if p.QuotedCargoUnits > 0 {
		var quoteErr error
		quotedAmountLimit, quoteErr = cityOpenWorldCarrierCommerceQuotedFee(p.QuotedCargoUnits, p.FeePerCargoUnit)
		if quoteErr != nil {
			return errors.New("invalid carrier-commerce quoted amount limit")
		}
	}
	if paidCargoUnits != p.PaidCargoUnits || paidAmountUnits != p.PaidAmountUnits ||
		p.PaidCargoUnits > p.QuotedCargoUnits || p.PaidAmountUnits > quotedAmountLimit {
		return errors.New("carrier-commerce payment counter mismatch")
	}
	return nil
}

func cityOpenWorldCarrierCommerceQuotedFee(cargoUnits, feePerCargoUnit int64) (int64, error) {
	if cargoUnits <= 0 || feePerCargoUnit <= 0 || cargoUnits > math.MaxInt64/feePerCargoUnit {
		return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v24_carrier_commerce_quote"})
	}
	return cargoUnits * feePerCargoUnit, nil
}

func cityOpenWorldCarrierCommerceProfileMetadataValid(raw json.RawMessage) bool {
	item := cityOpenWorldCarrierCommerceProfileMetadata{}
	return json.Unmarshal(raw, &item) == nil && item.SchemaVersion == cityOpenWorldCarrierCommerceSchemaVersion &&
		item.Scope == "post_baseline_v22_case_carrier_service_fee" && item.Pricing == "fixed_per_cargo_unit_v1" &&
		item.Settlement == "cash_only_retry_without_credit_v1"
}

func cityOpenWorldCarrierCommerceContractMetadataValid(raw json.RawMessage, contract CityOpenWorldCarrierServiceContract) bool {
	item := cityOpenWorldCarrierCommerceContractMetadata{}
	return json.Unmarshal(raw, &item) == nil && item.SchemaVersion == cityOpenWorldCarrierCommerceSchemaVersion &&
		item.Contract == cityOpenWorldCarrierCommerceContractContract && item.CaseCode == contract.CaseCode &&
		item.SourceKind == contract.SourceKind && item.SourceCode == contract.SourceCode &&
		item.SellerFirm == contract.SellerFirmCode && item.CarrierFirm == contract.CarrierFirmCode &&
		item.CarrierActor == contract.CarrierActorCode && item.CargoUnits == contract.CargoUnits &&
		item.FeePerUnit == contract.FeePerCargoUnit && item.QuotedFee == contract.QuotedFeeUnits
}

func cityOpenWorldCarrierCommercePaymentMetadataValid(raw json.RawMessage, payment CityOpenWorldCarrierFeePayment) bool {
	item := cityOpenWorldCarrierCommercePaymentMetadata{}
	return json.Unmarshal(raw, &item) == nil && item.SchemaVersion == cityOpenWorldCarrierCommerceSchemaVersion &&
		item.Contract == cityOpenWorldCarrierCommercePaymentContract && item.ContractCode == payment.ContractCode &&
		item.CaseCode == payment.CaseCode && item.SellerFirm == payment.SellerFirmCode &&
		item.CarrierFirm == payment.CarrierFirmCode && item.CargoUnits == payment.CargoUnits &&
		item.AmountUnits == payment.AmountUnits
}

func sortCityOpenWorldCarrierCommerceState(state *CityOpenWorldCarrierCommerceState) {
	if state == nil {
		return
	}
	sort.Slice(state.Contracts, func(i, j int) bool {
		if state.Contracts[i].SourceTick != state.Contracts[j].SourceTick {
			return state.Contracts[i].SourceTick < state.Contracts[j].SourceTick
		}
		return state.Contracts[i].Code < state.Contracts[j].Code
	})
	sort.Slice(state.Payments, func(i, j int) bool {
		if state.Payments[i].PaymentTick != state.Payments[j].PaymentTick {
			return state.Payments[i].PaymentTick < state.Payments[j].PaymentTick
		}
		return state.Payments[i].Code < state.Payments[j].Code
	})
}
