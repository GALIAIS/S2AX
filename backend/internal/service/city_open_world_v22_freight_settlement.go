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
	CityCommandTypeOpenWorldFreightSettlementReceipt = "open_world.freight_settlement.receipt"

	cityOpenWorldFreightSettlementSchemaVersion          = 1
	cityOpenWorldFreightSettlementProfileID              = "sub2api-open-world-freight-settlement"
	cityOpenWorldFreightSettlementProfileVersion         = "1.0.0"
	cityOpenWorldFreightSettlementSourceContract         = "v17_shipment_or_v18_consignment_v1"
	cityOpenWorldFreightSettlementReceiptContract        = "append_only_line_quantity_outcome_v1"
	cityOpenWorldFreightSettlementResourceContract       = "immediate_partial_transfer_loss_consumption_v1"
	cityOpenWorldFreightSettlementFinancialContract      = "accepted_price_refund_and_carrier_claim_v1"
	cityOpenWorldFreightSettlementLiabilityContract      = "seller_refund_or_carrier_claim_v1"
	cityOpenWorldFreightSettlementMaximumOrders          = 10000
	cityOpenWorldFreightSettlementMaximumCasesPerOrder   = 128
	cityOpenWorldFreightSettlementMaximumReceiptsPerCase = 128
	cityOpenWorldFreightSettlementMaximumReceiptsPerTick = 256

	cityOpenWorldFreightSettlementSourceShipment    = "shipment"
	cityOpenWorldFreightSettlementSourceConsignment = "consignment"
	cityOpenWorldFreightSettlementOrderAwaiting     = "awaiting_transport"
	cityOpenWorldFreightSettlementOrderReceiving    = "receiving"
	cityOpenWorldFreightSettlementOrderSettled      = "settled"
	cityOpenWorldFreightSettlementOrderVoided       = "voided"
	cityOpenWorldFreightSettlementOrderBlocked      = "blocked"
	cityOpenWorldFreightSettlementCaseAwaiting      = "awaiting_outcome"
	cityOpenWorldFreightSettlementCaseReceiving     = "receiving"
	cityOpenWorldFreightSettlementCaseSettled       = "settled"
	// A voided case records that the parent V15 order failed before any V22
	// receipt resolved cargo. It deliberately preserves V17/V18 custody as
	// historical transport evidence instead of pretending that the cargo was
	// delivered or settled.
	cityOpenWorldFreightSettlementCaseVoided       = "voided"
	cityOpenWorldFreightSettlementLiabilitySeller  = "seller"
	cityOpenWorldFreightSettlementLiabilityCarrier = "carrier"
	cityOpenWorldFreightSettlementClaimOpen        = "open"
	cityOpenWorldFreightSettlementClaimResolved    = "resolved"
	cityOpenWorldFreightSettlementReasonCompleted  = "freight_settlement_completed"
)

func isCityOpenWorldFreightSettlementCommand(commandType string) bool {
	return commandType == CityCommandTypeOpenWorldFreightSettlementReceipt
}

// CityOpenWorldFreightSettlementPolicy pins V22's future-only partial
// settlement protocol. It observes custody projections but remains the only
// owner of partial quantity outcomes, refunds and carrier liability claims.
type CityOpenWorldFreightSettlementPolicy struct {
	ProfileID              string          `json:"profile_id"`
	ProfileVersion         string          `json:"profile_version"`
	ContentHash            string          `json:"content_hash"`
	BaselineTick           int64           `json:"baseline_tick"`
	SourceContract         string          `json:"source_contract"`
	ReceiptContract        string          `json:"receipt_contract"`
	ResourceContract       string          `json:"resource_contract"`
	FinancialContract      string          `json:"financial_contract"`
	LiabilityContract      string          `json:"liability_contract"`
	MaximumOrders          int             `json:"maximum_orders"`
	MaximumCasesPerOrder   int             `json:"maximum_cases_per_order"`
	MaximumReceiptsPerCase int             `json:"maximum_receipts_per_case"`
	MaximumReceiptsPerTick int             `json:"maximum_receipts_per_tick"`
	OrderCount             int64           `json:"order_count"`
	CaseCount              int64           `json:"case_count"`
	ReceiptCount           int64           `json:"receipt_count"`
	ClaimCount             int64           `json:"claim_count"`
	AcceptedUnits          int64           `json:"accepted_units"`
	LostUnits              int64           `json:"lost_units"`
	RejectedUnits          int64           `json:"rejected_units"`
	RefundedUnits          int64           `json:"refunded_units"`
	Revision               int64           `json:"revision"`
	Metadata               json.RawMessage `json:"metadata"`
}

// CityOpenWorldFreightSettlementOrder is a V22 tracking projection for one
// V15 order. A V17 shipment becomes one case; a V18 batch plan becomes one
// case per consignment. Old sources at or before BaselineTick are never added.
type CityOpenWorldFreightSettlementOrder struct {
	Code       string          `json:"code"`
	SourceKind string          `json:"source_kind"`
	SourceCode string          `json:"source_code"`
	OrderCode  string          `json:"order_code"`
	SourceTick int64           `json:"source_tick"`
	State      string          `json:"state"`
	Version    int64           `json:"version"`
	Metadata   json.RawMessage `json:"metadata"`
}

type CityOpenWorldFreightSettlementCase struct {
	Code                string          `json:"code"`
	SettlementOrderCode string          `json:"settlement_order_code"`
	SourceKind          string          `json:"source_kind"`
	SourceCode          string          `json:"source_code"`
	TransportState      string          `json:"transport_state"`
	State               string          `json:"state"`
	SourceTick          int64           `json:"source_tick"`
	Version             int64           `json:"version"`
	Metadata            json.RawMessage `json:"metadata"`
}

type CityOpenWorldFreightSettlementCaseLine struct {
	CaseCode                string          `json:"case_code"`
	SourceLineNo            int             `json:"source_line_no"`
	ResourceCode            string          `json:"resource_code"`
	SourceFirmCode          string          `json:"source_firm_code"`
	SourceDistrictCode      string          `json:"source_district_code"`
	DestinationFirmCode     string          `json:"destination_firm_code"`
	DestinationDistrictCode string          `json:"destination_district_code"`
	QuantityUnits           int64           `json:"quantity_units"`
	UnitPriceUnits          int64           `json:"unit_price_units"`
	TotalPriceUnits         int64           `json:"total_price_units"`
	Metadata                json.RawMessage `json:"metadata"`
}

type CityOpenWorldFreightSettlementReceipt struct {
	Code                  string                       `json:"code"`
	CaseCode              string                       `json:"case_code"`
	ReceiptTick           int64                        `json:"receipt_tick"`
	SourceCommandSequence int64                        `json:"source_command_sequence"`
	LiabilityParty        string                       `json:"liability_party"`
	RefundedUnits         int64                        `json:"refunded_units"`
	ResourceOperation     *CityResourceOperationCursor `json:"resource_operation,omitempty"`
	Journal               *CityJournalCursor           `json:"journal,omitempty"`
	Metadata              json.RawMessage              `json:"metadata"`
}

type CityOpenWorldFreightSettlementReceiptLine struct {
	ReceiptCode   string `json:"receipt_code"`
	CaseCode      string `json:"case_code"`
	SourceLineNo  int    `json:"source_line_no"`
	AcceptedUnits int64  `json:"accepted_units"`
	LostUnits     int64  `json:"lost_units"`
	RejectedUnits int64  `json:"rejected_units"`
}

type CityOpenWorldFreightSettlementClaim struct {
	Code           string          `json:"code"`
	ReceiptCode    string          `json:"receipt_code"`
	CaseCode       string          `json:"case_code"`
	LiabilityParty string          `json:"liability_party"`
	ClaimAmount    int64           `json:"claim_amount"`
	State          string          `json:"state"`
	CreatedTick    int64           `json:"created_tick"`
	Metadata       json.RawMessage `json:"metadata"`
}

type CityOpenWorldFreightSettlementState struct {
	Policy       CityOpenWorldFreightSettlementPolicy        `json:"policy"`
	Orders       []CityOpenWorldFreightSettlementOrder       `json:"orders"`
	Cases        []CityOpenWorldFreightSettlementCase        `json:"cases"`
	Lines        []CityOpenWorldFreightSettlementCaseLine    `json:"lines"`
	Receipts     []CityOpenWorldFreightSettlementReceipt     `json:"receipts"`
	ReceiptLines []CityOpenWorldFreightSettlementReceiptLine `json:"receipt_lines"`
	Claims       []CityOpenWorldFreightSettlementClaim       `json:"claims"`
}

func cityOpenWorldFreightSettlementPolicyHash() (string, error) {
	raw, err := json.Marshal(struct {
		SchemaVersion          int    `json:"schema_version"`
		ProfileID              string `json:"profile_id"`
		ProfileVersion         string `json:"profile_version"`
		SourceContract         string `json:"source_contract"`
		ReceiptContract        string `json:"receipt_contract"`
		ResourceContract       string `json:"resource_contract"`
		FinancialContract      string `json:"financial_contract"`
		LiabilityContract      string `json:"liability_contract"`
		MaximumOrders          int    `json:"maximum_orders"`
		MaximumCasesPerOrder   int    `json:"maximum_cases_per_order"`
		MaximumReceiptsPerCase int    `json:"maximum_receipts_per_case"`
		MaximumReceiptsPerTick int    `json:"maximum_receipts_per_tick"`
	}{
		SchemaVersion:          cityOpenWorldFreightSettlementSchemaVersion,
		ProfileID:              cityOpenWorldFreightSettlementProfileID,
		ProfileVersion:         cityOpenWorldFreightSettlementProfileVersion,
		SourceContract:         cityOpenWorldFreightSettlementSourceContract,
		ReceiptContract:        cityOpenWorldFreightSettlementReceiptContract,
		ResourceContract:       cityOpenWorldFreightSettlementResourceContract,
		FinancialContract:      cityOpenWorldFreightSettlementFinancialContract,
		LiabilityContract:      cityOpenWorldFreightSettlementLiabilityContract,
		MaximumOrders:          cityOpenWorldFreightSettlementMaximumOrders,
		MaximumCasesPerOrder:   cityOpenWorldFreightSettlementMaximumCasesPerOrder,
		MaximumReceiptsPerCase: cityOpenWorldFreightSettlementMaximumReceiptsPerCase,
		MaximumReceiptsPerTick: cityOpenWorldFreightSettlementMaximumReceiptsPerTick,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func cityOpenWorldFreightSettlementOrderCode(sourceKind, sourceCode string) string {
	sum := sha256.Sum256([]byte("v22\x00order\x00" + sourceKind + "\x00" + sourceCode))
	return "freight.settlement.order." + hex.EncodeToString(sum[:20])
}

func cityOpenWorldFreightSettlementCaseCode(orderCode, sourceKind, sourceCode string) string {
	sum := sha256.Sum256([]byte("v22\x00case\x00" + orderCode + "\x00" + sourceKind + "\x00" + sourceCode))
	return "freight.settlement.case." + hex.EncodeToString(sum[:20])
}

func cityOpenWorldFreightSettlementReceiptCode(commandSequence int64) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("v22\x00receipt\x00%d", commandSequence)))
	return "freight.settlement.receipt." + hex.EncodeToString(sum[:20])
}

func cityOpenWorldFreightSettlementClaimCode(receiptCode string) string {
	sum := sha256.Sum256([]byte("v22\x00claim\x00" + receiptCode))
	return "freight.settlement.claim." + hex.EncodeToString(sum[:20])
}

func activateCityOpenWorldFreightSettlementBootstrapWrite(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if _, err := tx.ExecContext(ctx, `
SELECT set_config('sub2api.city_open_world_freight_settlement_bootstrap_world_id', $1, TRUE)`, fmt.Sprintf("%d", worldID)); err != nil {
		return fmt.Errorf("enable open-world V22 freight-settlement bootstrap: %w", err)
	}
	return nil
}

func activateCityOpenWorldFreightSettlementWrite(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if _, err := tx.ExecContext(ctx, `
SELECT set_config('sub2api.city_open_world_freight_settlement_world_id', $1, TRUE)`, fmt.Sprintf("%d", worldID)); err != nil {
		return fmt.Errorf("enable open-world V22 freight-settlement write: %w", err)
	}
	return nil
}

func activateCityOpenWorldFreightSettlementRecoveryWrite(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if _, err := tx.ExecContext(ctx, `
SELECT set_config('sub2api.city_open_world_freight_settlement_recovery_world_id', $1, TRUE)`, fmt.Sprintf("%d", worldID)); err != nil {
		return fmt.Errorf("enable open-world V22 freight-settlement recovery: %w", err)
	}
	return nil
}

func assertCityOpenWorldFreightSettlementFoundation(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if _, err := tx.ExecContext(ctx, `SELECT assert_city_open_world_freight_settlement_foundation($1)`, worldID); err != nil {
		return fmt.Errorf("validate V22 freight-settlement foundation: %w", err)
	}
	return nil
}

// initializeCityOpenWorldV22FreightSettlementFoundation creates only a
// profile anchored at the creation/upgrade tick. Historical shipments and
// consignments remain untracked by design; the runtime admits only sources
// whose source tick is strictly later than the sealed baseline.
func initializeCityOpenWorldV22FreightSettlementFoundation(ctx context.Context, tx *sql.Tx, worldID int64) error {
	var version string
	var baselineTick int64
	if err := tx.QueryRowContext(ctx, `
SELECT simulation_version, current_tick
FROM city_worlds
WHERE id = $1
FOR UPDATE`, worldID).Scan(&version, &baselineTick); err != nil {
		return fmt.Errorf("lock V22 freight-settlement world: %w", err)
	}
	if !cityEngineSupportsOpenWorldFreightSettlements(version) || baselineTick < 0 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v22_freight_settlement_world"})
	}
	if err := assertCityOpenWorldFreightBatchFoundation(ctx, tx, worldID); err != nil {
		return fmt.Errorf("validate V22 freight-settlement V18 prerequisite: %w", err)
	}
	hash, err := cityOpenWorldFreightSettlementPolicyHash()
	if err != nil {
		return fmt.Errorf("hash V22 freight-settlement profile: %w", err)
	}
	metadata, err := json.Marshal(map[string]any{
		"schema_version":      cityOpenWorldFreightSettlementSchemaVersion,
		"scope":               "post_baseline_v17_v18_partial_freight_settlement",
		"historical_behavior": "pre_baseline_sources_untracked",
		"delivery_behavior":   "v15_settled_successor_not_atomic_delivery",
	})
	if err != nil {
		return fmt.Errorf("marshal V22 freight-settlement profile metadata: %w", err)
	}
	if err = activateCityOpenWorldFreightSettlementBootstrapWrite(ctx, tx, worldID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_freight_settlement_profiles
    (world_id, profile_id, profile_version, content_hash, baseline_tick,
     source_contract, receipt_contract, resource_contract, financial_contract,
     liability_contract, maximum_orders, maximum_cases_per_order,
     maximum_receipts_per_case, maximum_receipts_per_tick, revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, 1, $15::jsonb)`,
		worldID, cityOpenWorldFreightSettlementProfileID, cityOpenWorldFreightSettlementProfileVersion,
		hash, baselineTick, cityOpenWorldFreightSettlementSourceContract,
		cityOpenWorldFreightSettlementReceiptContract, cityOpenWorldFreightSettlementResourceContract,
		cityOpenWorldFreightSettlementFinancialContract, cityOpenWorldFreightSettlementLiabilityContract,
		cityOpenWorldFreightSettlementMaximumOrders, cityOpenWorldFreightSettlementMaximumCasesPerOrder,
		cityOpenWorldFreightSettlementMaximumReceiptsPerCase, cityOpenWorldFreightSettlementMaximumReceiptsPerTick,
		[]byte(metadata)); err != nil {
		return fmt.Errorf("insert V22 freight-settlement profile: %w", err)
	}
	return assertCityOpenWorldFreightSettlementFoundation(ctx, tx, worldID)
}

func loadCityOpenWorldFreightSettlementState(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*CityOpenWorldFreightSettlementState, error) {
	state := &CityOpenWorldFreightSettlementState{
		Orders: make([]CityOpenWorldFreightSettlementOrder, 0), Cases: make([]CityOpenWorldFreightSettlementCase, 0),
		Lines: make([]CityOpenWorldFreightSettlementCaseLine, 0), Receipts: make([]CityOpenWorldFreightSettlementReceipt, 0),
		ReceiptLines: make([]CityOpenWorldFreightSettlementReceiptLine, 0), Claims: make([]CityOpenWorldFreightSettlementClaim, 0),
	}
	err := queryer.QueryRowContext(ctx, `
SELECT profile_id, profile_version, content_hash, baseline_tick,
       source_contract, receipt_contract, resource_contract, financial_contract,
       liability_contract, maximum_orders, maximum_cases_per_order,
       maximum_receipts_per_case, maximum_receipts_per_tick, order_count,
       case_count, receipt_count, claim_count, accepted_units, lost_units,
       rejected_units, refunded_units, revision, metadata
FROM city_open_world_freight_settlement_profiles
WHERE world_id = $1`, worldID).Scan(
		&state.Policy.ProfileID, &state.Policy.ProfileVersion, &state.Policy.ContentHash, &state.Policy.BaselineTick,
		&state.Policy.SourceContract, &state.Policy.ReceiptContract, &state.Policy.ResourceContract,
		&state.Policy.FinancialContract, &state.Policy.LiabilityContract, &state.Policy.MaximumOrders,
		&state.Policy.MaximumCasesPerOrder, &state.Policy.MaximumReceiptsPerCase, &state.Policy.MaximumReceiptsPerTick,
		&state.Policy.OrderCount, &state.Policy.CaseCount, &state.Policy.ReceiptCount, &state.Policy.ClaimCount,
		&state.Policy.AcceptedUnits, &state.Policy.LostUnits, &state.Policy.RejectedUnits, &state.Policy.RefundedUnits,
		&state.Policy.Revision, &state.Policy.Metadata,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v22_freight_settlement_profile"})
	}
	if err != nil {
		return nil, fmt.Errorf("load V22 freight-settlement profile: %w", err)
	}
	orders, err := queryer.QueryContext(ctx, `
SELECT code, source_kind, source_code, order_code, source_tick, state, version, metadata
FROM city_open_world_freight_settlement_orders
WHERE world_id = $1
ORDER BY source_tick, code`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V22 freight-settlement orders: %w", err)
	}
	for orders.Next() {
		item := CityOpenWorldFreightSettlementOrder{}
		if err = orders.Scan(&item.Code, &item.SourceKind, &item.SourceCode, &item.OrderCode, &item.SourceTick, &item.State, &item.Version, &item.Metadata); err != nil {
			_ = orders.Close()
			return nil, fmt.Errorf("scan V22 freight-settlement order: %w", err)
		}
		state.Orders = append(state.Orders, item)
	}
	if err = closeCityRows(orders, "iterate V22 freight-settlement orders"); err != nil {
		return nil, err
	}
	cases, err := queryer.QueryContext(ctx, `
SELECT code, settlement_order_code, source_kind, source_code, transport_state,
       state, source_tick, version, metadata
FROM city_open_world_freight_settlement_cases
WHERE world_id = $1
ORDER BY source_tick, code`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V22 freight-settlement cases: %w", err)
	}
	for cases.Next() {
		item := CityOpenWorldFreightSettlementCase{}
		if err = cases.Scan(&item.Code, &item.SettlementOrderCode, &item.SourceKind, &item.SourceCode,
			&item.TransportState, &item.State, &item.SourceTick, &item.Version, &item.Metadata); err != nil {
			_ = cases.Close()
			return nil, fmt.Errorf("scan V22 freight-settlement case: %w", err)
		}
		state.Cases = append(state.Cases, item)
	}
	if err = closeCityRows(cases, "iterate V22 freight-settlement cases"); err != nil {
		return nil, err
	}
	lines, err := queryer.QueryContext(ctx, `
SELECT case_code, source_line_no, resource_code, source_firm_code,
       source_district_code, destination_firm_code, destination_district_code,
       quantity_units, unit_price_units, total_price_units, metadata
FROM city_open_world_freight_settlement_case_lines
WHERE world_id = $1
ORDER BY case_code, source_line_no`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V22 freight-settlement case lines: %w", err)
	}
	for lines.Next() {
		item := CityOpenWorldFreightSettlementCaseLine{}
		if err = lines.Scan(&item.CaseCode, &item.SourceLineNo, &item.ResourceCode, &item.SourceFirmCode,
			&item.SourceDistrictCode, &item.DestinationFirmCode, &item.DestinationDistrictCode,
			&item.QuantityUnits, &item.UnitPriceUnits, &item.TotalPriceUnits, &item.Metadata); err != nil {
			_ = lines.Close()
			return nil, fmt.Errorf("scan V22 freight-settlement case line: %w", err)
		}
		state.Lines = append(state.Lines, item)
	}
	if err = closeCityRows(lines, "iterate V22 freight-settlement case lines"); err != nil {
		return nil, err
	}
	receipts, err := queryer.QueryContext(ctx, `
SELECT receipt.code, receipt.case_code, receipt.receipt_tick,
       command.sequence, receipt.liability_party, receipt.refunded_units,
       operation.tick, operation.sequence, journal.tick, journal.sequence,
       receipt.metadata
FROM city_open_world_freight_settlement_receipts receipt
JOIN city_commands command
  ON command.id = receipt.source_command_id AND command.world_id = receipt.world_id
LEFT JOIN city_resource_operations operation
  ON operation.id = receipt.resource_operation_id AND operation.world_id = receipt.world_id
LEFT JOIN city_journals journal
  ON journal.id = receipt.journal_id AND journal.world_id = receipt.world_id
WHERE receipt.world_id = $1
ORDER BY receipt.receipt_tick, command.sequence`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V22 freight-settlement receipts: %w", err)
	}
	for receipts.Next() {
		item := CityOpenWorldFreightSettlementReceipt{}
		var operationTick, operationSequence, journalTick, journalSequence sql.NullInt64
		if err = receipts.Scan(&item.Code, &item.CaseCode, &item.ReceiptTick, &item.SourceCommandSequence,
			&item.LiabilityParty, &item.RefundedUnits, &operationTick, &operationSequence,
			&journalTick, &journalSequence, &item.Metadata); err != nil {
			_ = receipts.Close()
			return nil, fmt.Errorf("scan V22 freight-settlement receipt: %w", err)
		}
		if operationTick.Valid && operationSequence.Valid {
			item.ResourceOperation = &CityResourceOperationCursor{Tick: operationTick.Int64, Sequence: operationSequence.Int64}
		}
		if journalTick.Valid && journalSequence.Valid {
			item.Journal = &CityJournalCursor{Tick: journalTick.Int64, Sequence: journalSequence.Int64}
		}
		state.Receipts = append(state.Receipts, item)
	}
	if err = closeCityRows(receipts, "iterate V22 freight-settlement receipts"); err != nil {
		return nil, err
	}
	receiptLines, err := queryer.QueryContext(ctx, `
SELECT receipt_code, case_code, source_line_no, accepted_units, lost_units, rejected_units
FROM city_open_world_freight_settlement_receipt_lines
WHERE world_id = $1
ORDER BY receipt_code, source_line_no`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V22 freight-settlement receipt lines: %w", err)
	}
	for receiptLines.Next() {
		item := CityOpenWorldFreightSettlementReceiptLine{}
		if err = receiptLines.Scan(&item.ReceiptCode, &item.CaseCode, &item.SourceLineNo, &item.AcceptedUnits, &item.LostUnits, &item.RejectedUnits); err != nil {
			_ = receiptLines.Close()
			return nil, fmt.Errorf("scan V22 freight-settlement receipt line: %w", err)
		}
		state.ReceiptLines = append(state.ReceiptLines, item)
	}
	if err = closeCityRows(receiptLines, "iterate V22 freight-settlement receipt lines"); err != nil {
		return nil, err
	}
	claims, err := queryer.QueryContext(ctx, `
SELECT code, receipt_code, case_code, liability_party, claim_amount,
       state, created_tick, metadata
FROM city_open_world_freight_settlement_claims
WHERE world_id = $1
ORDER BY created_tick, code`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V22 freight-settlement claims: %w", err)
	}
	for claims.Next() {
		item := CityOpenWorldFreightSettlementClaim{}
		if err = claims.Scan(&item.Code, &item.ReceiptCode, &item.CaseCode, &item.LiabilityParty,
			&item.ClaimAmount, &item.State, &item.CreatedTick, &item.Metadata); err != nil {
			_ = claims.Close()
			return nil, fmt.Errorf("scan V22 freight-settlement claim: %w", err)
		}
		state.Claims = append(state.Claims, item)
	}
	if err = closeCityRows(claims, "iterate V22 freight-settlement claims"); err != nil {
		return nil, err
	}
	if err = validateCityOpenWorldFreightSettlementState(state); err != nil {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v22_freight_settlement_state"}).WithCause(err)
	}
	return state, nil
}

func validateCityOpenWorldFreightSettlementState(state *CityOpenWorldFreightSettlementState) error {
	if state == nil {
		return errors.New("freight-settlement state is required")
	}
	p := state.Policy
	hash, err := cityOpenWorldFreightSettlementPolicyHash()
	if err != nil {
		return err
	}
	if p.ProfileID != cityOpenWorldFreightSettlementProfileID || p.ProfileVersion != cityOpenWorldFreightSettlementProfileVersion ||
		p.ContentHash != hash || p.BaselineTick < 0 || p.SourceContract != cityOpenWorldFreightSettlementSourceContract ||
		p.ReceiptContract != cityOpenWorldFreightSettlementReceiptContract || p.ResourceContract != cityOpenWorldFreightSettlementResourceContract ||
		p.FinancialContract != cityOpenWorldFreightSettlementFinancialContract || p.LiabilityContract != cityOpenWorldFreightSettlementLiabilityContract ||
		p.MaximumOrders != cityOpenWorldFreightSettlementMaximumOrders || p.MaximumCasesPerOrder != cityOpenWorldFreightSettlementMaximumCasesPerOrder ||
		p.MaximumReceiptsPerCase != cityOpenWorldFreightSettlementMaximumReceiptsPerCase || p.MaximumReceiptsPerTick != cityOpenWorldFreightSettlementMaximumReceiptsPerTick ||
		p.OrderCount != int64(len(state.Orders)) || p.CaseCount != int64(len(state.Cases)) || p.ReceiptCount != int64(len(state.Receipts)) ||
		p.ClaimCount != int64(len(state.Claims)) || p.Revision < 1 || !cityOpenWorldFreightSettlementJSONObject(p.Metadata) {
		return errors.New("invalid freight-settlement policy")
	}
	orders := make(map[string]CityOpenWorldFreightSettlementOrder, len(state.Orders))
	ordersBySupplyOrder := make(map[string]string, len(state.Orders))
	for _, order := range state.Orders {
		if !worldRuntimeCodeValid(order.Code, 160) || !cityOpenWorldFreightSettlementSourceKindValid(order.SourceKind) ||
			!worldRuntimeCodeValid(order.SourceCode, 160) || !worldRuntimeCodeValid(order.OrderCode, 160) ||
			order.SourceTick <= p.BaselineTick || !cityOpenWorldFreightSettlementOrderStateValid(order.State) ||
			order.Version < 1 || !cityOpenWorldFreightSettlementJSONObject(order.Metadata) {
			return fmt.Errorf("invalid freight-settlement order %q", order.Code)
		}
		if _, exists := orders[order.Code]; exists {
			return fmt.Errorf("duplicate freight-settlement order %q", order.Code)
		}
		if previousCode, exists := ordersBySupplyOrder[order.OrderCode]; exists && previousCode != order.Code {
			return fmt.Errorf("multiple freight-settlement orders track supply order %q", order.OrderCode)
		}
		orders[order.Code] = order
		ordersBySupplyOrder[order.OrderCode] = order.Code
	}
	cases := make(map[string]CityOpenWorldFreightSettlementCase, len(state.Cases))
	casesByOrder := make(map[string]int)
	for _, item := range state.Cases {
		order, exists := orders[item.SettlementOrderCode]
		if !exists || !worldRuntimeCodeValid(item.Code, 160) || !cityOpenWorldFreightSettlementSourceKindValid(item.SourceKind) ||
			!worldRuntimeCodeValid(item.SourceCode, 160) || item.SourceTick != order.SourceTick || item.SourceTick <= p.BaselineTick ||
			item.SourceKind != order.SourceKind ||
			(item.SourceKind == cityOpenWorldFreightSettlementSourceShipment && item.SourceCode != order.SourceCode) ||
			!cityOpenWorldFreightSettlementCaseStateValid(item.State) || !cityOpenWorldFreightSettlementTransportStateValid(item.TransportState) ||
			item.Version < 1 || !cityOpenWorldFreightSettlementJSONObject(item.Metadata) {
			return fmt.Errorf("invalid freight-settlement case %q", item.Code)
		}
		if _, duplicate := cases[item.Code]; duplicate {
			return fmt.Errorf("duplicate freight-settlement case %q", item.Code)
		}
		cases[item.Code] = item
		casesByOrder[item.SettlementOrderCode]++
	}
	for code, count := range casesByOrder {
		if count > p.MaximumCasesPerOrder || code == "" {
			return fmt.Errorf("invalid freight-settlement case count")
		}
	}
	for _, order := range state.Orders {
		if casesByOrder[order.Code] == 0 {
			return fmt.Errorf("freight-settlement order %q has no cases", order.Code)
		}
	}
	caseLines := make(map[string]map[int]CityOpenWorldFreightSettlementCaseLine, len(cases))
	for _, line := range state.Lines {
		if _, exists := cases[line.CaseCode]; !exists || line.SourceLineNo < 1 ||
			!worldRuntimeCodeValid(line.ResourceCode, 160) || !worldRuntimeCodeValid(line.SourceFirmCode, 160) ||
			!worldRuntimeCodeValid(line.SourceDistrictCode, 160) || !worldRuntimeCodeValid(line.DestinationFirmCode, 160) ||
			!worldRuntimeCodeValid(line.DestinationDistrictCode, 160) || line.QuantityUnits <= 0 || line.UnitPriceUnits <= 0 ||
			line.TotalPriceUnits <= 0 || line.QuantityUnits > line.TotalPriceUnits ||
			line.QuantityUnits > 0 && line.TotalPriceUnits/line.QuantityUnits != line.UnitPriceUnits ||
			!cityOpenWorldFreightSettlementJSONObject(line.Metadata) {
			return fmt.Errorf("invalid freight-settlement case line %q/%d", line.CaseCode, line.SourceLineNo)
		}
		if caseLines[line.CaseCode] == nil {
			caseLines[line.CaseCode] = make(map[int]CityOpenWorldFreightSettlementCaseLine)
		}
		if _, duplicate := caseLines[line.CaseCode][line.SourceLineNo]; duplicate {
			return fmt.Errorf("duplicate freight-settlement case line %q/%d", line.CaseCode, line.SourceLineNo)
		}
		caseLines[line.CaseCode][line.SourceLineNo] = line
	}
	for code := range cases {
		if len(caseLines[code]) == 0 {
			return fmt.Errorf("freight-settlement case %q has no lines", code)
		}
	}
	receipts := make(map[string]CityOpenWorldFreightSettlementReceipt, len(state.Receipts))
	receiptsByCase := make(map[string]int)
	receiptsByTick := make(map[int64]int)
	receiptSequences := make(map[int64]struct{}, len(state.Receipts))
	for _, receipt := range state.Receipts {
		if _, exists := cases[receipt.CaseCode]; !exists || !worldRuntimeCodeValid(receipt.Code, 160) || receipt.ReceiptTick <= 0 ||
			receipt.SourceCommandSequence <= 0 || !cityOpenWorldFreightSettlementLiabilityValid(receipt.LiabilityParty) ||
			receipt.Code != cityOpenWorldFreightSettlementReceiptCode(receipt.SourceCommandSequence) ||
			receipt.RefundedUnits < 0 || !cityOpenWorldFreightSettlementJSONObject(receipt.Metadata) {
			return fmt.Errorf("invalid freight-settlement receipt %q", receipt.Code)
		}
		if (receipt.ResourceOperation != nil && (receipt.ResourceOperation.Tick <= 0 || receipt.ResourceOperation.Sequence <= 0)) ||
			(receipt.Journal != nil && (receipt.Journal.Tick <= 0 || receipt.Journal.Sequence <= 0)) ||
			(receipt.RefundedUnits > 0) != (receipt.Journal != nil) {
			return fmt.Errorf("invalid freight-settlement receipt evidence %q", receipt.Code)
		}
		if _, duplicate := receipts[receipt.Code]; duplicate {
			return fmt.Errorf("duplicate freight-settlement receipt %q", receipt.Code)
		}
		if _, duplicate := receiptSequences[receipt.SourceCommandSequence]; duplicate {
			return fmt.Errorf("duplicate freight-settlement receipt command sequence %d", receipt.SourceCommandSequence)
		}
		receipts[receipt.Code] = receipt
		receiptSequences[receipt.SourceCommandSequence] = struct{}{}
		receiptsByCase[receipt.CaseCode]++
		receiptsByTick[receipt.ReceiptTick]++
	}
	for _, count := range receiptsByCase {
		if count > p.MaximumReceiptsPerCase {
			return errors.New("freight-settlement receipt case limit exceeded")
		}
	}
	for _, count := range receiptsByTick {
		if count > p.MaximumReceiptsPerTick {
			return errors.New("freight-settlement receipt tick limit exceeded")
		}
	}
	totals := make(map[string]map[int][3]int64, len(cases))
	receiptTotals := make(map[string][3]int64, len(receipts))
	receiptRefunds := make(map[string]int64, len(receipts))
	receiptLineCounts := make(map[string]int, len(receipts))
	receiptLineKeys := make(map[string]struct{}, len(state.ReceiptLines))
	for _, line := range state.ReceiptLines {
		receipt, exists := receipts[line.ReceiptCode]
		if !exists || receipt.CaseCode != line.CaseCode || line.SourceLineNo < 1 ||
			line.AcceptedUnits < 0 || line.LostUnits < 0 || line.RejectedUnits < 0 ||
			line.AcceptedUnits+line.LostUnits+line.RejectedUnits <= 0 {
			return fmt.Errorf("invalid freight-settlement receipt line %q/%d", line.ReceiptCode, line.SourceLineNo)
		}
		if _, exists = caseLines[line.CaseCode][line.SourceLineNo]; !exists {
			return fmt.Errorf("freight-settlement receipt line references unavailable case line")
		}
		lineKey := fmt.Sprintf("%s\x00%d", line.ReceiptCode, line.SourceLineNo)
		if _, duplicate := receiptLineKeys[lineKey]; duplicate {
			return fmt.Errorf("duplicate freight-settlement receipt line %q/%d", line.ReceiptCode, line.SourceLineNo)
		}
		receiptLineKeys[lineKey] = struct{}{}
		if totals[line.CaseCode] == nil {
			totals[line.CaseCode] = make(map[int][3]int64)
		}
		previous := totals[line.CaseCode][line.SourceLineNo]
		if line.AcceptedUnits > math.MaxInt64-previous[0] ||
			line.LostUnits > math.MaxInt64-previous[1] ||
			line.RejectedUnits > math.MaxInt64-previous[2] {
			return errors.New("freight-settlement receipt quantity overflow")
		}
		previous[0] += line.AcceptedUnits
		previous[1] += line.LostUnits
		previous[2] += line.RejectedUnits
		totals[line.CaseCode][line.SourceLineNo] = previous

		receiptTotal := receiptTotals[line.ReceiptCode]
		if line.AcceptedUnits > math.MaxInt64-receiptTotal[0] ||
			line.LostUnits > math.MaxInt64-receiptTotal[1] ||
			line.RejectedUnits > math.MaxInt64-receiptTotal[2] {
			return errors.New("freight-settlement receipt total overflow")
		}
		receiptTotal[0] += line.AcceptedUnits
		receiptTotal[1] += line.LostUnits
		receiptTotal[2] += line.RejectedUnits
		receiptTotals[line.ReceiptCode] = receiptTotal
		receiptLineCounts[line.ReceiptCode]++

		if line.LostUnits > math.MaxInt64-line.RejectedUnits {
			return errors.New("freight-settlement receipt refund overflow")
		}
		outcomeUnits := line.LostUnits + line.RejectedUnits
		caseLine := caseLines[line.CaseCode][line.SourceLineNo]
		if outcomeUnits > 0 {
			if outcomeUnits > math.MaxInt64/caseLine.UnitPriceUnits {
				return errors.New("freight-settlement receipt refund overflow")
			}
			refundDelta := outcomeUnits * caseLine.UnitPriceUnits
			if refundDelta > math.MaxInt64-receiptRefunds[line.ReceiptCode] {
				return errors.New("freight-settlement receipt refund overflow")
			}
			receiptRefunds[line.ReceiptCode] += refundDelta
		}
	}
	var accepted, lost, rejected, refunded int64
	for caseCode, lines := range caseLines {
		allResolved := true
		hasOutcome := false
		for lineNo, line := range lines {
			resolved := totals[caseCode][lineNo]
			if resolved[0] > math.MaxInt64-resolved[1] || resolved[0]+resolved[1] > math.MaxInt64-resolved[2] {
				return errors.New("freight-settlement case outcome overflow")
			}
			resolvedUnits := resolved[0] + resolved[1] + resolved[2]
			if resolvedUnits > line.QuantityUnits {
				return fmt.Errorf("freight-settlement case line over-resolved")
			}
			allResolved = allResolved && resolvedUnits == line.QuantityUnits
			hasOutcome = hasOutcome || resolvedUnits > 0
			if resolved[0] > math.MaxInt64-accepted || resolved[1] > math.MaxInt64-lost || resolved[2] > math.MaxInt64-rejected {
				return errors.New("freight-settlement policy quantity overflow")
			}
			accepted += resolved[0]
			lost += resolved[1]
			rejected += resolved[2]
		}
		settlementCase := cases[caseCode]
		if (settlementCase.State == cityOpenWorldFreightSettlementCaseSettled) != allResolved ||
			(settlementCase.State == cityOpenWorldFreightSettlementCaseAwaiting && hasOutcome) ||
			(settlementCase.State == cityOpenWorldFreightSettlementCaseReceiving && !hasOutcome) ||
			(settlementCase.State == cityOpenWorldFreightSettlementCaseVoided && hasOutcome) {
			return fmt.Errorf("freight-settlement case %q outcome state is inconsistent", caseCode)
		}
	}
	for _, receipt := range state.Receipts {
		outcome := receiptTotals[receipt.Code]
		if receiptLineCounts[receipt.Code] == 0 || receipt.RefundedUnits != receiptRefunds[receipt.Code] ||
			((outcome[0] > 0 || outcome[1] > 0) != (receipt.ResourceOperation != nil)) {
			return fmt.Errorf("freight-settlement receipt %q evidence does not match outcomes", receipt.Code)
		}
		if receipt.RefundedUnits > math.MaxInt64-refunded {
			return errors.New("freight-settlement policy refund overflow")
		}
		refunded += receipt.RefundedUnits
	}
	if accepted < 0 || lost < 0 || rejected < 0 || refunded < 0 ||
		p.AcceptedUnits != accepted || p.LostUnits != lost || p.RejectedUnits != rejected || p.RefundedUnits != refunded {
		return errors.New("freight-settlement policy counters do not match evidence")
	}
	claims := make(map[string]CityOpenWorldFreightSettlementClaim, len(state.Claims))
	claimsByReceipt := make(map[string]int, len(state.Claims))
	for _, claim := range state.Claims {
		receipt, exists := receipts[claim.ReceiptCode]
		if !exists || receipt.CaseCode != claim.CaseCode || !worldRuntimeCodeValid(claim.Code, 160) ||
			claim.Code != cityOpenWorldFreightSettlementClaimCode(claim.ReceiptCode) ||
			claim.LiabilityParty != cityOpenWorldFreightSettlementLiabilityCarrier || claim.ClaimAmount <= 0 ||
			receipt.LiabilityParty != cityOpenWorldFreightSettlementLiabilityCarrier || claim.ClaimAmount != receipt.RefundedUnits ||
			(claim.State != cityOpenWorldFreightSettlementClaimOpen && claim.State != cityOpenWorldFreightSettlementClaimResolved) ||
			claim.CreatedTick <= 0 || !cityOpenWorldFreightSettlementJSONObject(claim.Metadata) {
			return fmt.Errorf("invalid freight-settlement claim %q", claim.Code)
		}
		if _, duplicate := claims[claim.Code]; duplicate {
			return fmt.Errorf("duplicate freight-settlement claim %q", claim.Code)
		}
		claims[claim.Code] = claim
		claimsByReceipt[claim.ReceiptCode]++
	}
	for _, receipt := range state.Receipts {
		claimCount := claimsByReceipt[receipt.Code]
		if (receipt.LiabilityParty == cityOpenWorldFreightSettlementLiabilityCarrier && receipt.RefundedUnits > 0 && claimCount != 1) ||
			((receipt.LiabilityParty != cityOpenWorldFreightSettlementLiabilityCarrier || receipt.RefundedUnits == 0) && claimCount != 0) {
			return fmt.Errorf("freight-settlement receipt %q claim evidence is inconsistent", receipt.Code)
		}
	}
	for _, order := range state.Orders {
		settledCases := 0
		voidedCases := 0
		awaitingCases := 0
		for _, settlementCase := range state.Cases {
			if settlementCase.SettlementOrderCode != order.Code {
				continue
			}
			switch settlementCase.State {
			case cityOpenWorldFreightSettlementCaseSettled:
				settledCases++
			case cityOpenWorldFreightSettlementCaseVoided:
				voidedCases++
			case cityOpenWorldFreightSettlementCaseAwaiting:
				awaitingCases++
			}
		}
		allCasesSettled := settledCases == casesByOrder[order.Code]
		allCasesVoided := voidedCases == casesByOrder[order.Code]
		if (order.State == cityOpenWorldFreightSettlementOrderSettled) != allCasesSettled ||
			(order.State == cityOpenWorldFreightSettlementOrderVoided) != allCasesVoided ||
			(order.State == cityOpenWorldFreightSettlementOrderBlocked && awaitingCases != casesByOrder[order.Code]) {
			return fmt.Errorf("freight-settlement order %q state does not match its cases", order.Code)
		}
	}
	return nil
}

func cityOpenWorldFreightSettlementSourceKindValid(value string) bool {
	return value == cityOpenWorldFreightSettlementSourceShipment || value == cityOpenWorldFreightSettlementSourceConsignment
}

func cityOpenWorldFreightSettlementOrderStateValid(value string) bool {
	return value == cityOpenWorldFreightSettlementOrderAwaiting || value == cityOpenWorldFreightSettlementOrderReceiving ||
		value == cityOpenWorldFreightSettlementOrderSettled || value == cityOpenWorldFreightSettlementOrderVoided ||
		value == cityOpenWorldFreightSettlementOrderBlocked
}

func cityOpenWorldFreightSettlementCaseStateValid(value string) bool {
	return value == cityOpenWorldFreightSettlementCaseAwaiting || value == cityOpenWorldFreightSettlementCaseReceiving ||
		value == cityOpenWorldFreightSettlementCaseSettled || value == cityOpenWorldFreightSettlementCaseVoided
}

func cityOpenWorldFreightSettlementTransportStateValid(value string) bool {
	return value == cityOpenWorldEnterpriseFreightReceiptStateAwaitingRoute ||
		value == cityOpenWorldEnterpriseFreightReceiptStateInTransit ||
		value == cityOpenWorldEnterpriseFreightReceiptStateAwaitingReceipt ||
		value == cityOpenWorldEnterpriseFreightReceiptStateExpired ||
		value == cityOpenWorldEnterpriseFreightReceiptStateVoided ||
		value == cityOpenWorldEnterpriseFreightReceiptStateOrphaned ||
		value == cityOpenWorldFreightBatchConsignmentStateAwaitingRoute ||
		value == cityOpenWorldFreightBatchConsignmentStateInTransit ||
		value == cityOpenWorldFreightBatchConsignmentStateAwaitingReceipt ||
		value == cityOpenWorldFreightBatchConsignmentStateExpired ||
		value == cityOpenWorldFreightBatchConsignmentStateVoided ||
		value == cityOpenWorldFreightBatchConsignmentStateOrphaned
}

func cityOpenWorldFreightSettlementLiabilityValid(value string) bool {
	return value == cityOpenWorldFreightSettlementLiabilitySeller || value == cityOpenWorldFreightSettlementLiabilityCarrier
}

func cityOpenWorldFreightSettlementJSONObject(raw json.RawMessage) bool {
	if len(raw) == 0 || !json.Valid(raw) {
		return false
	}
	var value any
	if json.Unmarshal(raw, &value) != nil || value == nil {
		return false
	}
	_, ok := value.(map[string]any)
	return ok
}

func sortCityOpenWorldFreightSettlementState(state *CityOpenWorldFreightSettlementState) {
	if state == nil {
		return
	}
	sort.Slice(state.Orders, func(i, j int) bool { return state.Orders[i].Code < state.Orders[j].Code })
	sort.Slice(state.Cases, func(i, j int) bool { return state.Cases[i].Code < state.Cases[j].Code })
	sort.Slice(state.Lines, func(i, j int) bool {
		return state.Lines[i].CaseCode < state.Lines[j].CaseCode || state.Lines[i].CaseCode == state.Lines[j].CaseCode && state.Lines[i].SourceLineNo < state.Lines[j].SourceLineNo
	})
	sort.Slice(state.Receipts, func(i, j int) bool { return state.Receipts[i].Code < state.Receipts[j].Code })
	sort.Slice(state.ReceiptLines, func(i, j int) bool {
		return state.ReceiptLines[i].ReceiptCode < state.ReceiptLines[j].ReceiptCode || state.ReceiptLines[i].ReceiptCode == state.ReceiptLines[j].ReceiptCode && state.ReceiptLines[i].SourceLineNo < state.ReceiptLines[j].SourceLineNo
	})
	sort.Slice(state.Claims, func(i, j int) bool { return state.Claims[i].Code < state.Claims[j].Code })
}
