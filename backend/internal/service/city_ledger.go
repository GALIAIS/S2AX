package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/lib/pq"
)

const (
	CityCommandTypeLedgerCashTransfer = "ledger.cash_transfer"
	CityCommandTypeLedgerWage         = "ledger.wage"
	CityCommandTypeLedgerPurchase     = "ledger.purchase"
	CityCommandTypeLedgerTax          = "ledger.tax"
	CityCommandTypeLedgerSubsidy      = "ledger.subsidy"
	CityCommandTypeLedgerReverse      = "ledger.reverse"

	cityDefaultJournalLimit           = 50
	cityMaximumJournalLimit           = 200
	cityMaximumTransactionUnits int64 = math.MaxInt64 / 2

	cityLedgerRejectionAccountNotFound   = "CITY_LEDGER_ACCOUNT_NOT_FOUND"
	cityLedgerRejectionEntityType        = "CITY_LEDGER_ENTITY_TYPE_INVALID"
	cityLedgerRejectionInsufficient      = "CITY_LEDGER_INSUFFICIENT_BALANCE"
	cityLedgerRejectionJournalNotFound   = "CITY_LEDGER_JOURNAL_NOT_FOUND"
	cityLedgerRejectionReversalForbidden = "CITY_LEDGER_REVERSAL_NOT_ALLOWED"
	cityLedgerRejectionAlreadyReversed   = "CITY_LEDGER_ALREADY_REVERSED"
)

var ErrCityJournalNotFound = infraerrors.NotFound("CITY_JOURNAL_NOT_FOUND", "city journal not found")

type CityJournalEntry struct {
	ID                   int64     `json:"id"`
	JournalID            int64     `json:"journal_id"`
	LineNo               int       `json:"line_no"`
	AccountID            int64     `json:"account_id"`
	EntityID             int64     `json:"entity_id"`
	EntityType           string    `json:"entity_type"`
	EntityCode           string    `json:"entity_code"`
	EntityName           string    `json:"entity_name"`
	AccountCode          string    `json:"account_code"`
	AccountName          string    `json:"account_name"`
	AccountClass         string    `json:"account_class"`
	NormalSide           string    `json:"normal_side"`
	DebitUnits           int64     `json:"debit_units"`
	CreditUnits          int64     `json:"credit_units"`
	BalanceBeforeUnits   int64     `json:"balance_before_units"`
	BalanceAfterUnits    int64     `json:"balance_after_units"`
	AccountVersionBefore int64     `json:"account_version_before"`
	AccountVersionAfter  int64     `json:"account_version_after"`
	Memo                 string    `json:"memo"`
	CreatedAt            time.Time `json:"created_at"`
}

type CityJournal struct {
	ID                  int64               `json:"id"`
	WorldID             int64               `json:"world_id"`
	MonetaryUnitID      int64               `json:"monetary_unit_id"`
	MonetaryUnitCode    string              `json:"monetary_unit_code"`
	MonetaryUnitName    string              `json:"monetary_unit_name"`
	MonetaryUnitSymbol  string              `json:"monetary_unit_symbol"`
	MonetaryUnitScale   int                 `json:"monetary_unit_scale"`
	Tick                int64               `json:"tick"`
	Sequence            int64               `json:"sequence"`
	OperationKey        string              `json:"operation_key"`
	JournalType         string              `json:"journal_type"`
	SourceCommandID     *int64              `json:"source_command_id,omitempty"`
	MarketSettlementID  *int64              `json:"market_settlement_id,omitempty"`
	ReversalOfJournalID *int64              `json:"reversal_of_journal_id,omitempty"`
	ReversalOfTick      *int64              `json:"reversal_of_tick,omitempty"`
	ReversalOfSequence  *int64              `json:"reversal_of_sequence,omitempty"`
	Description         string              `json:"description"`
	Metadata            map[string]any      `json:"metadata"`
	EntryCount          int                 `json:"entry_count"`
	DebitTotalUnits     int64               `json:"debit_total_units"`
	CreditTotalUnits    int64               `json:"credit_total_units"`
	CreatedAt           time.Time           `json:"created_at"`
	PostedAt            time.Time           `json:"posted_at"`
	Entries             []*CityJournalEntry `json:"entries,omitempty"`
}

type CityJournalCursor struct {
	Tick     int64 `json:"tick"`
	Sequence int64 `json:"sequence"`
}

type CityJournalPage struct {
	Items      []*CityJournal     `json:"items"`
	NextCursor *CityJournalCursor `json:"next_cursor,omitempty"`
}

type CityJournalListInput struct {
	UserID        int64
	WorldID       int64
	AfterTick     int64
	AfterSequence int64
	Limit         int
}

type CityTrialBalanceAccount struct {
	AccountID       int64  `json:"account_id"`
	EntityID        int64  `json:"entity_id"`
	EntityType      string `json:"entity_type"`
	EntityCode      string `json:"entity_code"`
	EntityName      string `json:"entity_name"`
	AccountCode     string `json:"account_code"`
	AccountName     string `json:"account_name"`
	AccountClass    string `json:"account_class"`
	NormalSide      string `json:"normal_side"`
	BalanceSide     string `json:"balance_side"`
	BalanceUnits    int64  `json:"balance_units"`
	DebitUnits      int64  `json:"debit_units"`
	CreditUnits     int64  `json:"credit_units"`
	Version         int64  `json:"version"`
	ProjectionValid bool   `json:"projection_valid"`
}

type CityTrialBalanceUnit struct {
	MonetaryUnitID          int64                      `json:"monetary_unit_id"`
	Code                    string                     `json:"code"`
	Name                    string                     `json:"name"`
	Symbol                  string                     `json:"symbol"`
	Scale                   int                        `json:"scale"`
	AccountCount            int                        `json:"account_count"`
	ProjectionMismatchCount int                        `json:"projection_mismatch_count"`
	EntityImbalanceCount    int                        `json:"entity_imbalance_count"`
	TotalDebitUnits         int64                      `json:"total_debit_units"`
	TotalCreditUnits        int64                      `json:"total_credit_units"`
	DifferenceUnits         int64                      `json:"difference_units"`
	Balanced                bool                       `json:"balanced"`
	Accounts                []*CityTrialBalanceAccount `json:"accounts"`
}

type CityTrialBalance struct {
	WorldID  int64                   `json:"world_id"`
	AsOfTick int64                   `json:"as_of_tick"`
	Balanced bool                    `json:"balanced"`
	Units    []*CityTrialBalanceUnit `json:"units"`
}

type cityCashTransferPayload struct {
	FromEntityID int64  `json:"from_entity_id"`
	ToEntityID   int64  `json:"to_entity_id"`
	AmountUnits  int64  `json:"amount_units"`
	Memo         string `json:"memo,omitempty"`
}

type cityWagePayload struct {
	FirmEntityID      int64  `json:"firm_entity_id"`
	HouseholdEntityID int64  `json:"household_entity_id"`
	AmountUnits       int64  `json:"amount_units"`
	Memo              string `json:"memo,omitempty"`
}

type cityPurchasePayload struct {
	HouseholdEntityID int64  `json:"household_entity_id"`
	FirmEntityID      int64  `json:"firm_entity_id"`
	AmountUnits       int64  `json:"amount_units"`
	Memo              string `json:"memo,omitempty"`
}

type cityTaxPayload struct {
	PayerEntityID int64  `json:"payer_entity_id"`
	AmountUnits   int64  `json:"amount_units"`
	Memo          string `json:"memo,omitempty"`
}

type citySubsidyPayload struct {
	RecipientEntityID int64  `json:"recipient_entity_id"`
	AmountUnits       int64  `json:"amount_units"`
	Memo              string `json:"memo,omitempty"`
}

type cityReverseJournalPayload struct {
	JournalTick     int64  `json:"journal_tick"`
	JournalSequence int64  `json:"journal_sequence"`
	Reason          string `json:"reason"`
}

func normalizeCityLedgerCommand(commandType string, rawPayload json.RawMessage) (any, bool, error) {
	normalizeMemo := func(value *string) error {
		*value = strings.TrimSpace(*value)
		if utf8.RuneCountInString(*value) > 256 {
			return ErrCityInvalidInput.WithMetadata(map[string]string{"field": "memo"})
		}
		return nil
	}
	switch commandType {
	case CityCommandTypeLedgerCashTransfer:
		var value cityCashTransferPayload
		if err := decodeStrictCityObject(rawPayload, &value); err != nil {
			return nil, true, ErrCityInvalidInput.WithCause(err)
		}
		if value.FromEntityID <= 0 || value.ToEntityID <= 0 || value.FromEntityID == value.ToEntityID ||
			value.AmountUnits <= 0 || value.AmountUnits > cityMaximumTransactionUnits {
			return nil, true, ErrCityInvalidInput
		}
		if err := normalizeMemo(&value.Memo); err != nil {
			return nil, true, err
		}
		return value, true, nil
	case CityCommandTypeLedgerWage:
		var value cityWagePayload
		if err := decodeStrictCityObject(rawPayload, &value); err != nil {
			return nil, true, ErrCityInvalidInput.WithCause(err)
		}
		if value.FirmEntityID <= 0 || value.HouseholdEntityID <= 0 ||
			value.AmountUnits <= 0 || value.AmountUnits > cityMaximumTransactionUnits {
			return nil, true, ErrCityInvalidInput
		}
		if err := normalizeMemo(&value.Memo); err != nil {
			return nil, true, err
		}
		return value, true, nil
	case CityCommandTypeLedgerPurchase:
		var value cityPurchasePayload
		if err := decodeStrictCityObject(rawPayload, &value); err != nil {
			return nil, true, ErrCityInvalidInput.WithCause(err)
		}
		if value.HouseholdEntityID <= 0 || value.FirmEntityID <= 0 ||
			value.AmountUnits <= 0 || value.AmountUnits > cityMaximumTransactionUnits {
			return nil, true, ErrCityInvalidInput
		}
		if err := normalizeMemo(&value.Memo); err != nil {
			return nil, true, err
		}
		return value, true, nil
	case CityCommandTypeLedgerTax:
		var value cityTaxPayload
		if err := decodeStrictCityObject(rawPayload, &value); err != nil {
			return nil, true, ErrCityInvalidInput.WithCause(err)
		}
		if value.PayerEntityID <= 0 || value.AmountUnits <= 0 || value.AmountUnits > cityMaximumTransactionUnits {
			return nil, true, ErrCityInvalidInput
		}
		if err := normalizeMemo(&value.Memo); err != nil {
			return nil, true, err
		}
		return value, true, nil
	case CityCommandTypeLedgerSubsidy:
		var value citySubsidyPayload
		if err := decodeStrictCityObject(rawPayload, &value); err != nil {
			return nil, true, ErrCityInvalidInput.WithCause(err)
		}
		if value.RecipientEntityID <= 0 || value.AmountUnits <= 0 || value.AmountUnits > cityMaximumTransactionUnits {
			return nil, true, ErrCityInvalidInput
		}
		if err := normalizeMemo(&value.Memo); err != nil {
			return nil, true, err
		}
		return value, true, nil
	case CityCommandTypeLedgerReverse:
		var value cityReverseJournalPayload
		if err := decodeStrictCityObject(rawPayload, &value); err != nil {
			return nil, true, ErrCityInvalidInput.WithCause(err)
		}
		value.Reason = strings.TrimSpace(value.Reason)
		if value.JournalTick <= 0 || value.JournalSequence <= 0 || utf8.RuneCountInString(value.Reason) < 1 || utf8.RuneCountInString(value.Reason) > 256 {
			return nil, true, ErrCityInvalidInput
		}
		return value, true, nil
	default:
		return nil, false, nil
	}
}

func isCityLedgerCommand(commandType string) bool {
	switch commandType {
	case CityCommandTypeLedgerCashTransfer, CityCommandTypeLedgerWage,
		CityCommandTypeLedgerPurchase, CityCommandTypeLedgerTax,
		CityCommandTypeLedgerSubsidy, CityCommandTypeLedgerReverse:
		return true
	default:
		return false
	}
}

func cityLedgerCommandNeedsBootstrap(commandType string) bool {
	return isCityLedgerCommand(commandType) && commandType != CityCommandTypeLedgerReverse
}

const cityJournalSelectColumns = `
j.id, j.world_id, j.monetary_unit_id, u.code, u.name, u.symbol, u.scale,
j.tick, j.sequence, j.operation_key, j.journal_type, j.source_command_id,
j.market_settlement_id, j.reversal_of_journal_id, original.tick, original.sequence, j.description,
j.metadata, COALESCE(stats.entry_count, 0), COALESCE(stats.debit_total, 0),
COALESCE(stats.credit_total, 0), j.created_at, j.posted_at`

const cityJournalSelectJoins = `
JOIN city_monetary_units u ON u.id = j.monetary_unit_id AND u.world_id = j.world_id
LEFT JOIN city_journals original ON original.id = j.reversal_of_journal_id
LEFT JOIN LATERAL (
    SELECT COUNT(*)::bigint AS entry_count,
           COALESCE(SUM(e.debit_units), 0)::bigint AS debit_total,
           COALESCE(SUM(e.credit_units), 0)::bigint AS credit_total
    FROM city_journal_entries e
    WHERE e.journal_id = j.id
) stats ON TRUE`

func (s *CityEconomyService) ListJournals(ctx context.Context, input CityJournalListInput) (*CityJournalPage, error) {
	if input.UserID <= 0 || input.WorldID <= 0 || input.AfterTick < 0 || input.AfterSequence < 0 {
		return nil, ErrCityInvalidInput
	}
	if input.Limit <= 0 {
		input.Limit = cityDefaultJournalLimit
	}
	if input.Limit > cityMaximumJournalLimit {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, input.UserID, input.WorldID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT `+cityJournalSelectColumns+`
FROM city_journals j
`+cityJournalSelectJoins+`
WHERE j.world_id = $1
  AND j.posted_at IS NOT NULL
  AND (j.tick > $2 OR (j.tick = $2 AND j.sequence > $3))
ORDER BY j.tick ASC, j.sequence ASC
LIMIT $4`, input.WorldID, input.AfterTick, input.AfterSequence, input.Limit+1)
	if err != nil {
		return nil, fmt.Errorf("list city journals: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]*CityJournal, 0, input.Limit)
	for rows.Next() {
		item, scanErr := scanCityJournal(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate city journals: %w", err)
	}
	page := &CityJournalPage{Items: items}
	if len(items) > input.Limit {
		items = items[:input.Limit]
		page.Items = items
		last := items[len(items)-1]
		page.NextCursor = &CityJournalCursor{Tick: last.Tick, Sequence: last.Sequence}
	}
	return page, nil
}

func (s *CityEconomyService) GetJournal(ctx context.Context, userID, worldID, tick, sequence int64) (*CityJournal, error) {
	if userID <= 0 || worldID <= 0 || tick <= 0 || sequence <= 0 {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, userID, worldID); err != nil {
		return nil, err
	}
	item, err := loadCityJournalByCursor(ctx, s.db, worldID, tick, sequence, true)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCityJournalNotFound
	}
	return item, err
}

func (s *CityEconomyService) GetTrialBalance(ctx context.Context, userID, worldID int64) (*CityTrialBalance, error) {
	if userID <= 0 || worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("begin city trial balance snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err = authorizeCityWorldRead(ctx, tx, userID, worldID); err != nil {
		return nil, err
	}
	result := &CityTrialBalance{WorldID: worldID, Balanced: true, Units: make([]*CityTrialBalanceUnit, 0)}
	if err = tx.QueryRowContext(ctx, `SELECT current_tick FROM city_worlds WHERE id = $1`, worldID).Scan(&result.AsOfTick); err != nil {
		return nil, fmt.Errorf("load city trial balance tick: %w", err)
	}
	unitRows, err := tx.QueryContext(ctx, `
SELECT id, code, name, symbol, scale
FROM city_monetary_units
WHERE world_id = $1
ORDER BY code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load city trial balance units: %w", err)
	}
	unitsByID := make(map[int64]*CityTrialBalanceUnit)
	entityDebitsByUnit := make(map[int64]map[int64]int64)
	entityCreditsByUnit := make(map[int64]map[int64]int64)
	for unitRows.Next() {
		unit := &CityTrialBalanceUnit{Balanced: true, Accounts: make([]*CityTrialBalanceAccount, 0)}
		if err = unitRows.Scan(&unit.MonetaryUnitID, &unit.Code, &unit.Name, &unit.Symbol, &unit.Scale); err != nil {
			_ = unitRows.Close()
			return nil, err
		}
		unitsByID[unit.MonetaryUnitID] = unit
		entityDebitsByUnit[unit.MonetaryUnitID] = make(map[int64]int64)
		entityCreditsByUnit[unit.MonetaryUnitID] = make(map[int64]int64)
		result.Units = append(result.Units, unit)
	}
	if err = unitRows.Err(); err != nil {
		_ = unitRows.Close()
		return nil, fmt.Errorf("iterate city trial balance units: %w", err)
	}
	_ = unitRows.Close()

	accountRows, err := tx.QueryContext(ctx, `
SELECT a.id, a.monetary_unit_id, e.id, e.entity_type, e.code, e.name,
       t.code, t.name, t.account_class, t.normal_side,
       a.current_balance_units, a.version,
       latest.balance_after_units, latest.account_version_after
FROM city_accounts a
JOIN city_economic_entities e ON e.id = a.entity_id AND e.world_id = a.world_id
JOIN city_account_templates t ON t.id = a.template_id AND t.world_id = a.world_id
LEFT JOIN LATERAL (
    SELECT entry.balance_after_units, entry.account_version_after
    FROM city_journal_entries entry
    WHERE entry.account_id = a.id
    ORDER BY entry.id DESC
    LIMIT 1
) latest ON TRUE
WHERE a.world_id = $1
ORDER BY a.monetary_unit_id, e.entity_type, e.code, t.sort_order, t.code`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load city trial balance accounts: %w", err)
	}
	for accountRows.Next() {
		account := &CityTrialBalanceAccount{}
		var unitID int64
		var latestBalance, latestVersion sql.NullInt64
		if err = accountRows.Scan(
			&account.AccountID, &unitID, &account.EntityID, &account.EntityType,
			&account.EntityCode, &account.EntityName, &account.AccountCode,
			&account.AccountName, &account.AccountClass, &account.NormalSide,
			&account.BalanceUnits, &account.Version, &latestBalance, &latestVersion,
		); err != nil {
			_ = accountRows.Close()
			return nil, err
		}
		unit := unitsByID[unitID]
		if unit == nil {
			_ = accountRows.Close()
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"monetary_unit_id": strconv.FormatInt(unitID, 10)})
		}
		if account.BalanceUnits == math.MinInt64 {
			_ = accountRows.Close()
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"account_id": strconv.FormatInt(account.AccountID, 10)})
		}
		account.ProjectionValid = latestBalance.Valid == latestVersion.Valid &&
			((latestBalance.Valid && latestBalance.Int64 == account.BalanceUnits && latestVersion.Int64 == account.Version) ||
				(!latestBalance.Valid && account.BalanceUnits == 0 && account.Version == 0))
		if account.BalanceUnits == 0 {
			account.BalanceSide = "zero"
		} else if (account.NormalSide == "debit" && account.BalanceUnits > 0) || (account.NormalSide == "credit" && account.BalanceUnits < 0) {
			account.BalanceSide = "debit"
			account.DebitUnits = account.BalanceUnits
			if account.DebitUnits < 0 {
				account.DebitUnits = -account.DebitUnits
			}
		} else {
			account.BalanceSide = "credit"
			account.CreditUnits = account.BalanceUnits
			if account.CreditUnits < 0 {
				account.CreditUnits = -account.CreditUnits
			}
		}
		unit.TotalDebitUnits, err = addCityLedgerUnits(unit.TotalDebitUnits, account.DebitUnits)
		if err != nil {
			_ = accountRows.Close()
			return nil, err
		}
		unit.TotalCreditUnits, err = addCityLedgerUnits(unit.TotalCreditUnits, account.CreditUnits)
		if err != nil {
			_ = accountRows.Close()
			return nil, err
		}
		if !account.ProjectionValid {
			unit.ProjectionMismatchCount++
		}
		entityDebitsByUnit[unitID][account.EntityID], err = addCityLedgerUnits(
			entityDebitsByUnit[unitID][account.EntityID], account.DebitUnits,
		)
		if err != nil {
			_ = accountRows.Close()
			return nil, err
		}
		entityCreditsByUnit[unitID][account.EntityID], err = addCityLedgerUnits(
			entityCreditsByUnit[unitID][account.EntityID], account.CreditUnits,
		)
		if err != nil {
			_ = accountRows.Close()
			return nil, err
		}
		unit.Accounts = append(unit.Accounts, account)
	}
	if err = accountRows.Err(); err != nil {
		_ = accountRows.Close()
		return nil, fmt.Errorf("iterate city trial balance accounts: %w", err)
	}
	_ = accountRows.Close()
	for _, unit := range result.Units {
		unit.AccountCount = len(unit.Accounts)
		for entityID, debits := range entityDebitsByUnit[unit.MonetaryUnitID] {
			if debits != entityCreditsByUnit[unit.MonetaryUnitID][entityID] {
				unit.EntityImbalanceCount++
			}
		}
		unit.DifferenceUnits = unit.TotalDebitUnits - unit.TotalCreditUnits
		unit.Balanced = unit.DifferenceUnits == 0 && unit.ProjectionMismatchCount == 0 && unit.EntityImbalanceCount == 0
		if !unit.Balanced {
			result.Balanced = false
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit city trial balance snapshot: %w", err)
	}
	return result, nil
}

func addCityLedgerUnits(left, right int64) (int64, error) {
	if right < 0 || left > math.MaxInt64-right {
		return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "ledger_total"})
	}
	return left + right, nil
}

type cityLedgerBaseUnit struct {
	id    int64
	code  string
	scale int
}

type cityLedgerAccountRef struct {
	id            int64
	entityID      int64
	entityType    string
	entityCode    string
	entityName    string
	accountCode   string
	normalSide    string
	allowNegative bool
	balanceUnits  int64
	version       int64
}

type cityLedgerPostingLine struct {
	account     *cityLedgerAccountRef
	debitUnits  int64
	creditUnits int64
	memo        string
}

type cityLedgerJournalSpec struct {
	worldID             int64
	unit                *cityLedgerBaseUnit
	tick                int64
	sequence            int64
	operationKey        string
	journalType         string
	sourceCommandID     *int64
	marketSettlementID  *int64
	reversalOfJournalID *int64
	description         string
	metadata            map[string]any
	lines               []cityLedgerPostingLine
}

type cityLedgerBootstrapEvent struct {
	eventType string
	payload   map[string]any
}

type cityLedgerBusinessError struct {
	code string
}

func (e *cityLedgerBusinessError) Error() string { return e.code }

func cityLedgerReject(code string) error { return &cityLedgerBusinessError{code: code} }

func loadCityLedgerBaseUnit(ctx context.Context, queryer citySQLQueryer, worldID int64) (*cityLedgerBaseUnit, error) {
	unit := &cityLedgerBaseUnit{}
	if err := queryer.QueryRowContext(ctx, `
SELECT id, code, scale
FROM city_monetary_units
WHERE world_id = $1 AND is_base AND status = 'active'`, worldID).Scan(&unit.id, &unit.code, &unit.scale); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "base_monetary_unit"})
		}
		return nil, fmt.Errorf("load city base monetary unit: %w", err)
	}
	return unit, nil
}

func loadCityLedgerAccount(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, unitID, entityID int64,
	expectedEntityType, accountCode string,
) (*cityLedgerAccountRef, error) {
	account := &cityLedgerAccountRef{}
	err := queryer.QueryRowContext(ctx, `
SELECT a.id, e.id, e.entity_type, e.code, e.name, t.code, t.normal_side,
       a.allow_negative, a.current_balance_units, a.version
FROM city_accounts a
JOIN city_economic_entities e
  ON e.id = a.entity_id AND e.world_id = a.world_id AND e.status = 'active'
JOIN city_account_templates t
  ON t.id = a.template_id AND t.world_id = a.world_id
WHERE a.world_id = $1
  AND a.monetary_unit_id = $2
  AND a.entity_id = $3
  AND t.code = $4
  AND a.status = 'active'`, worldID, unitID, entityID, accountCode).Scan(
		&account.id, &account.entityID, &account.entityType, &account.entityCode,
		&account.entityName, &account.accountCode, &account.normalSide,
		&account.allowNegative, &account.balanceUnits, &account.version,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, cityLedgerReject(cityLedgerRejectionAccountNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("load city ledger account: %w", err)
	}
	if expectedEntityType != "" && account.entityType != expectedEntityType {
		return nil, cityLedgerReject(cityLedgerRejectionEntityType)
	}
	return account, nil
}

func loadCityLedgerAccountByID(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, unitID, accountID int64,
) (*cityLedgerAccountRef, error) {
	account := &cityLedgerAccountRef{}
	err := queryer.QueryRowContext(ctx, `
SELECT a.id, e.id, e.entity_type, e.code, e.name, t.code, t.normal_side,
       a.allow_negative, a.current_balance_units, a.version
FROM city_accounts a
JOIN city_economic_entities e
  ON e.id = a.entity_id AND e.world_id = a.world_id AND e.status = 'active'
JOIN city_account_templates t
  ON t.id = a.template_id AND t.world_id = a.world_id
WHERE a.id = $1 AND a.world_id = $2 AND a.monetary_unit_id = $3 AND a.status = 'active'`,
		accountID, worldID, unitID).Scan(
		&account.id, &account.entityID, &account.entityType, &account.entityCode,
		&account.entityName, &account.accountCode, &account.normalSide,
		&account.allowNegative, &account.balanceUnits, &account.version,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, cityLedgerReject(cityLedgerRejectionAccountNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("load city ledger account by id: %w", err)
	}
	return account, nil
}

func loadCityGovernmentEntityID(ctx context.Context, queryer citySQLQueryer, worldID int64) (int64, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT id
FROM city_economic_entities
WHERE world_id = $1 AND entity_type = 'government' AND status = 'active'
ORDER BY id ASC
LIMIT 2`, worldID)
	if err != nil {
		return 0, fmt.Errorf("load city government entity: %w", err)
	}
	defer func() { _ = rows.Close() }()
	ids := make([]int64, 0, 2)
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			return 0, err
		}
		ids = append(ids, id)
	}
	if err = rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate city government entity: %w", err)
	}
	if len(ids) == 0 {
		return 0, cityLedgerReject(cityLedgerRejectionAccountNotFound)
	}
	if len(ids) != 1 {
		return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "government_entity_count"})
	}
	return ids[0], nil
}

func decodeStoredCityCommandPayload[T any](command *CityCommand) (T, error) {
	var value T
	raw, err := json.Marshal(command.Payload)
	if err != nil {
		return value, fmt.Errorf("marshal stored city command payload: %w", err)
	}
	if err = json.Unmarshal(raw, &value); err != nil {
		return value, fmt.Errorf("decode stored city command payload: %w", err)
	}
	return value, nil
}

func (s *CityEconomyService) ensureCityLedgerBootstrap(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, firstJournalSequence int64,
	unit *cityLedgerBaseUnit,
) ([]cityLedgerBootstrapEvent, int64, error) {
	type openingEntity struct {
		id, majorUnits int64
		entityType     string
		code           string
		counterpart    string
	}
	rows, err := tx.QueryContext(ctx, `
SELECT id, entity_type, code
FROM city_economic_entities
WHERE world_id = $1
  AND status = 'active'
  AND entity_type IN ('household', 'firm', 'government')
ORDER BY entity_type ASC, code ASC`, worldID)
	if err != nil {
		return nil, firstJournalSequence, fmt.Errorf("load city opening entities: %w", err)
	}
	entities := make([]openingEntity, 0, 3)
	for rows.Next() {
		entity := openingEntity{}
		if err = rows.Scan(&entity.id, &entity.entityType, &entity.code); err != nil {
			_ = rows.Close()
			return nil, firstJournalSequence, err
		}
		switch entity.entityType {
		case CityEntityTypeHousehold:
			entity.majorUnits, entity.counterpart = 10_000, "capital"
		case CityEntityTypeFirm:
			entity.majorUnits, entity.counterpart = 50_000, "equity"
		case CityEntityTypeGovernment:
			entity.majorUnits, entity.counterpart = 100_000, "fund_balance"
		}
		entities = append(entities, entity)
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return nil, firstJournalSequence, fmt.Errorf("iterate city opening entities: %w", err)
	}
	_ = rows.Close()
	if len(entities) == 0 {
		return nil, firstJournalSequence, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "opening_entities"})
	}
	var openingCount, journalCount int
	if err = tx.QueryRowContext(ctx, `
SELECT COUNT(*) FILTER (WHERE journal_type = 'opening'), COUNT(*)
FROM city_journals
WHERE world_id = $1`, worldID).Scan(&openingCount, &journalCount); err != nil {
		return nil, firstJournalSequence, fmt.Errorf("inspect city ledger bootstrap: %w", err)
	}
	if openingCount == len(entities) {
		return []cityLedgerBootstrapEvent{}, firstJournalSequence, nil
	}
	if openingCount != 0 || journalCount != 0 {
		return nil, firstJournalSequence, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "opening_journal_count"})
	}
	var dirtyAccounts int
	if err = tx.QueryRowContext(ctx, `
SELECT COUNT(*) FROM city_accounts
WHERE world_id = $1 AND (current_balance_units <> 0 OR version <> 0)`, worldID).Scan(&dirtyAccounts); err != nil {
		return nil, firstJournalSequence, fmt.Errorf("inspect city pre-ledger balances: %w", err)
	}
	if dirtyAccounts != 0 {
		return nil, firstJournalSequence, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "pre_ledger_account_projection"})
	}
	events := make([]cityLedgerBootstrapEvent, 0, len(entities))
	sequence := firstJournalSequence
	for _, entity := range entities {
		amount, amountErr := cityOpeningAmountUnits(entity.majorUnits, unit.scale)
		if amountErr != nil {
			return nil, firstJournalSequence, amountErr
		}
		cash, loadErr := loadCityLedgerAccount(ctx, tx, worldID, unit.id, entity.id, entity.entityType, "cash")
		if loadErr != nil {
			return nil, firstJournalSequence, loadErr
		}
		counterpart, loadErr := loadCityLedgerAccount(ctx, tx, worldID, unit.id, entity.id, entity.entityType, entity.counterpart)
		if loadErr != nil {
			return nil, firstJournalSequence, loadErr
		}
		journal, postErr := postCityJournal(ctx, tx, cityLedgerJournalSpec{
			worldID: worldID, unit: unit, tick: targetTick, sequence: sequence,
			operationKey: fmt.Sprintf("opening:v1:%s:%s", entity.entityType, entity.code),
			journalType:  "opening", description: "Opening balance: " + entity.code,
			metadata: map[string]any{
				"entity_type": entity.entityType, "entity_code": entity.code,
				"amount_units": amount, "schema_version": 1,
			},
			lines: []cityLedgerPostingLine{
				{account: cash, debitUnits: amount, memo: "Opening cash"},
				{account: counterpart, creditUnits: amount, memo: "Opening capital"},
			},
		})
		if postErr != nil {
			return nil, firstJournalSequence, postErr
		}
		events = append(events, cityLedgerBootstrapEvent{
			eventType: "city.ledger.opened",
			payload: map[string]any{
				"entity_type": entity.entityType, "entity_code": entity.code,
				"amount_units": amount, "monetary_unit_code": unit.code,
				"journal_tick": journal.Tick, "journal_sequence": journal.Sequence,
			},
		})
		sequence++
	}
	return events, sequence, nil
}

func cityOpeningAmountUnits(majorUnits int64, scale int) (int64, error) {
	if majorUnits <= 0 || scale < 0 || scale > 8 {
		return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "opening_amount"})
	}
	multiplier := int64(1)
	for range scale {
		if multiplier > math.MaxInt64/10 {
			return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "opening_amount"})
		}
		multiplier *= 10
	}
	if majorUnits > math.MaxInt64/multiplier {
		return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "opening_amount"})
	}
	return majorUnits * multiplier, nil
}

func (s *CityEconomyService) applyCityLedgerCommand(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, journalSequence int64,
	unit *cityLedgerBaseUnit,
	command *CityCommand,
) (cityPendingEvent, *CityJournal, error) {
	if _, err := tx.ExecContext(ctx, `SAVEPOINT city_ledger_command`); err != nil {
		return cityPendingEvent{}, nil, fmt.Errorf("create city ledger command savepoint: %w", err)
	}
	journal, err := s.postCityLedgerCommand(ctx, tx, worldID, targetTick, journalSequence, unit, command)
	if err != nil {
		if _, rollbackErr := tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT city_ledger_command`); rollbackErr != nil {
			return cityPendingEvent{}, nil, fmt.Errorf("rollback city ledger command savepoint after %v: %w", err, rollbackErr)
		}
		if _, releaseErr := tx.ExecContext(ctx, `RELEASE SAVEPOINT city_ledger_command`); releaseErr != nil {
			return cityPendingEvent{}, nil, fmt.Errorf("release rejected city ledger command savepoint: %w", releaseErr)
		}
		if code := cityLedgerBusinessRejectionCode(err); code != "" {
			return rejectedCityCommand(command, code), nil, nil
		}
		return cityPendingEvent{}, nil, err
	}
	if _, err = tx.ExecContext(ctx, `RELEASE SAVEPOINT city_ledger_command`); err != nil {
		return cityPendingEvent{}, nil, fmt.Errorf("release city ledger command savepoint: %w", err)
	}
	eventType := map[string]string{
		CityCommandTypeLedgerCashTransfer: "city.ledger.cash_transferred",
		CityCommandTypeLedgerWage:         "city.ledger.wage_paid",
		CityCommandTypeLedgerPurchase:     "city.ledger.purchase_settled",
		CityCommandTypeLedgerTax:          "city.ledger.tax_collected",
		CityCommandTypeLedgerSubsidy:      "city.ledger.subsidy_paid",
		CityCommandTypeLedgerReverse:      "city.ledger.journal_reversed",
	}[command.CommandType]
	payload := map[string]any{
		"journal_tick": journal.Tick, "journal_sequence": journal.Sequence,
		"journal_type": journal.JournalType, "monetary_unit_code": journal.MonetaryUnitCode,
		"debit_total_units": journal.DebitTotalUnits,
	}
	if journal.ReversalOfTick != nil {
		payload["reversal_of_tick"] = *journal.ReversalOfTick
		payload["reversal_of_sequence"] = *journal.ReversalOfSequence
	}
	return cityPendingEvent{
		command: command, status: CityCommandStatusApplied, eventType: eventType,
		payload: payload,
		result: map[string]any{
			"applied": true, "journal_tick": journal.Tick,
			"journal_sequence": journal.Sequence, "journal_type": journal.JournalType,
		},
	}, journal, nil
}

func cityLedgerBusinessRejectionCode(err error) string {
	var businessErr *cityLedgerBusinessError
	if errors.As(err, &businessErr) {
		return businessErr.code
	}
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		switch pqErr.Constraint {
		case "city_account_balance_check":
			return cityLedgerRejectionInsufficient
		case "idx_city_journals_one_reversal":
			return cityLedgerRejectionAlreadyReversed
		}
	}
	return ""
}

func (s *CityEconomyService) postCityLedgerCommand(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, journalSequence int64,
	unit *cityLedgerBaseUnit,
	command *CityCommand,
) (*CityJournal, error) {
	spec := cityLedgerJournalSpec{
		worldID: worldID, unit: unit, tick: targetTick, sequence: journalSequence,
		operationKey:    fmt.Sprintf("command:%d", command.Sequence),
		sourceCommandID: &command.ID,
		metadata:        map[string]any{"command_sequence": command.Sequence, "schema_version": 1},
	}
	switch command.CommandType {
	case CityCommandTypeLedgerCashTransfer:
		payload, err := decodeStoredCityCommandPayload[cityCashTransferPayload](command)
		if err != nil {
			return nil, err
		}
		fromCash, err := loadCityLedgerAccount(ctx, tx, worldID, unit.id, payload.FromEntityID, "", "cash")
		if err != nil {
			return nil, err
		}
		toCash, err := loadCityLedgerAccount(ctx, tx, worldID, unit.id, payload.ToEntityID, "", "cash")
		if err != nil {
			return nil, err
		}
		if !cityTransferEntityTypeAllowed(fromCash.entityType) || !cityTransferEntityTypeAllowed(toCash.entityType) {
			return nil, cityLedgerReject(cityLedgerRejectionEntityType)
		}
		fromExpense, err := loadCityLedgerAccount(ctx, tx, worldID, unit.id, payload.FromEntityID, fromCash.entityType, "transfer_expense")
		if err != nil {
			return nil, err
		}
		toIncome, err := loadCityLedgerAccount(ctx, tx, worldID, unit.id, payload.ToEntityID, toCash.entityType, "other_income")
		if err != nil {
			return nil, err
		}
		spec.journalType = "cash_transfer"
		spec.description = "Cash transfer"
		spec.metadata["from_entity_code"] = fromCash.entityCode
		spec.metadata["to_entity_code"] = toCash.entityCode
		spec.metadata["amount_units"] = payload.AmountUnits
		spec.lines = []cityLedgerPostingLine{
			{account: fromExpense, debitUnits: payload.AmountUnits, memo: payload.Memo},
			{account: toCash, debitUnits: payload.AmountUnits, memo: payload.Memo},
			{account: fromCash, creditUnits: payload.AmountUnits, memo: payload.Memo},
			{account: toIncome, creditUnits: payload.AmountUnits, memo: payload.Memo},
		}
	case CityCommandTypeLedgerWage:
		payload, err := decodeStoredCityCommandPayload[cityWagePayload](command)
		if err != nil {
			return nil, err
		}
		firmExpense, err := loadCityLedgerAccount(ctx, tx, worldID, unit.id, payload.FirmEntityID, CityEntityTypeFirm, "wage_expense")
		if err != nil {
			return nil, err
		}
		firmCash, err := loadCityLedgerAccount(ctx, tx, worldID, unit.id, payload.FirmEntityID, CityEntityTypeFirm, "cash")
		if err != nil {
			return nil, err
		}
		householdCash, err := loadCityLedgerAccount(ctx, tx, worldID, unit.id, payload.HouseholdEntityID, CityEntityTypeHousehold, "cash")
		if err != nil {
			return nil, err
		}
		householdIncome, err := loadCityLedgerAccount(ctx, tx, worldID, unit.id, payload.HouseholdEntityID, CityEntityTypeHousehold, "wage_income")
		if err != nil {
			return nil, err
		}
		spec.journalType = "wage"
		spec.description = "Wage payment"
		spec.metadata["firm_entity_code"] = firmCash.entityCode
		spec.metadata["household_entity_code"] = householdCash.entityCode
		spec.metadata["amount_units"] = payload.AmountUnits
		spec.lines = []cityLedgerPostingLine{
			{account: firmExpense, debitUnits: payload.AmountUnits, memo: payload.Memo},
			{account: householdCash, debitUnits: payload.AmountUnits, memo: payload.Memo},
			{account: firmCash, creditUnits: payload.AmountUnits, memo: payload.Memo},
			{account: householdIncome, creditUnits: payload.AmountUnits, memo: payload.Memo},
		}
	case CityCommandTypeLedgerPurchase:
		payload, err := decodeStoredCityCommandPayload[cityPurchasePayload](command)
		if err != nil {
			return nil, err
		}
		householdExpense, err := loadCityLedgerAccount(ctx, tx, worldID, unit.id, payload.HouseholdEntityID, CityEntityTypeHousehold, "consumption_expense")
		if err != nil {
			return nil, err
		}
		householdCash, err := loadCityLedgerAccount(ctx, tx, worldID, unit.id, payload.HouseholdEntityID, CityEntityTypeHousehold, "cash")
		if err != nil {
			return nil, err
		}
		firmCash, err := loadCityLedgerAccount(ctx, tx, worldID, unit.id, payload.FirmEntityID, CityEntityTypeFirm, "cash")
		if err != nil {
			return nil, err
		}
		firmRevenue, err := loadCityLedgerAccount(ctx, tx, worldID, unit.id, payload.FirmEntityID, CityEntityTypeFirm, "revenue")
		if err != nil {
			return nil, err
		}
		spec.journalType = "purchase"
		spec.description = "Household purchase"
		spec.metadata["household_entity_code"] = householdCash.entityCode
		spec.metadata["firm_entity_code"] = firmCash.entityCode
		spec.metadata["amount_units"] = payload.AmountUnits
		spec.lines = []cityLedgerPostingLine{
			{account: householdExpense, debitUnits: payload.AmountUnits, memo: payload.Memo},
			{account: firmCash, debitUnits: payload.AmountUnits, memo: payload.Memo},
			{account: householdCash, creditUnits: payload.AmountUnits, memo: payload.Memo},
			{account: firmRevenue, creditUnits: payload.AmountUnits, memo: payload.Memo},
		}
	case CityCommandTypeLedgerTax:
		payload, err := decodeStoredCityCommandPayload[cityTaxPayload](command)
		if err != nil {
			return nil, err
		}
		payerCash, err := loadCityLedgerAccount(ctx, tx, worldID, unit.id, payload.PayerEntityID, "", "cash")
		if err != nil {
			return nil, err
		}
		if !cityTransferEntityTypeAllowed(payerCash.entityType) {
			return nil, cityLedgerReject(cityLedgerRejectionEntityType)
		}
		payerExpense, err := loadCityLedgerAccount(ctx, tx, worldID, unit.id, payload.PayerEntityID, payerCash.entityType, "tax_expense")
		if err != nil {
			return nil, err
		}
		governmentID, err := loadCityGovernmentEntityID(ctx, tx, worldID)
		if err != nil {
			return nil, err
		}
		governmentCash, err := loadCityLedgerAccount(ctx, tx, worldID, unit.id, governmentID, CityEntityTypeGovernment, "cash")
		if err != nil {
			return nil, err
		}
		governmentRevenue, err := loadCityLedgerAccount(ctx, tx, worldID, unit.id, governmentID, CityEntityTypeGovernment, "tax_revenue")
		if err != nil {
			return nil, err
		}
		spec.journalType = "tax"
		spec.description = "Tax collection"
		spec.metadata["payer_entity_code"] = payerCash.entityCode
		spec.metadata["government_entity_code"] = governmentCash.entityCode
		spec.metadata["amount_units"] = payload.AmountUnits
		spec.lines = []cityLedgerPostingLine{
			{account: payerExpense, debitUnits: payload.AmountUnits, memo: payload.Memo},
			{account: governmentCash, debitUnits: payload.AmountUnits, memo: payload.Memo},
			{account: payerCash, creditUnits: payload.AmountUnits, memo: payload.Memo},
			{account: governmentRevenue, creditUnits: payload.AmountUnits, memo: payload.Memo},
		}
	case CityCommandTypeLedgerSubsidy:
		payload, err := decodeStoredCityCommandPayload[citySubsidyPayload](command)
		if err != nil {
			return nil, err
		}
		recipientCash, err := loadCityLedgerAccount(ctx, tx, worldID, unit.id, payload.RecipientEntityID, "", "cash")
		if err != nil {
			return nil, err
		}
		if !cityTransferEntityTypeAllowed(recipientCash.entityType) {
			return nil, cityLedgerReject(cityLedgerRejectionEntityType)
		}
		recipientIncome, err := loadCityLedgerAccount(ctx, tx, worldID, unit.id, payload.RecipientEntityID, recipientCash.entityType, "other_income")
		if err != nil {
			return nil, err
		}
		governmentID, err := loadCityGovernmentEntityID(ctx, tx, worldID)
		if err != nil {
			return nil, err
		}
		governmentExpense, err := loadCityLedgerAccount(ctx, tx, worldID, unit.id, governmentID, CityEntityTypeGovernment, "subsidy_expense")
		if err != nil {
			return nil, err
		}
		governmentCash, err := loadCityLedgerAccount(ctx, tx, worldID, unit.id, governmentID, CityEntityTypeGovernment, "cash")
		if err != nil {
			return nil, err
		}
		spec.journalType = "subsidy"
		spec.description = "Government subsidy"
		spec.metadata["recipient_entity_code"] = recipientCash.entityCode
		spec.metadata["government_entity_code"] = governmentCash.entityCode
		spec.metadata["amount_units"] = payload.AmountUnits
		spec.lines = []cityLedgerPostingLine{
			{account: governmentExpense, debitUnits: payload.AmountUnits, memo: payload.Memo},
			{account: recipientCash, debitUnits: payload.AmountUnits, memo: payload.Memo},
			{account: governmentCash, creditUnits: payload.AmountUnits, memo: payload.Memo},
			{account: recipientIncome, creditUnits: payload.AmountUnits, memo: payload.Memo},
		}
	case CityCommandTypeLedgerReverse:
		payload, err := decodeStoredCityCommandPayload[cityReverseJournalPayload](command)
		if err != nil {
			return nil, err
		}
		if payload.JournalTick >= targetTick {
			return nil, cityLedgerReject(cityLedgerRejectionReversalForbidden)
		}
		original, err := loadCityJournalByCursor(ctx, tx, worldID, payload.JournalTick, payload.JournalSequence, true)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, cityLedgerReject(cityLedgerRejectionJournalNotFound)
		}
		if err != nil {
			return nil, err
		}
		if original.JournalType == "opening" || original.JournalType == "reversal" || original.MonetaryUnitID != unit.id {
			return nil, cityLedgerReject(cityLedgerRejectionReversalForbidden)
		}
		var alreadyReversed bool
		if err = tx.QueryRowContext(ctx, `
SELECT EXISTS (SELECT 1 FROM city_journals WHERE reversal_of_journal_id = $1)`, original.ID).Scan(&alreadyReversed); err != nil {
			return nil, fmt.Errorf("inspect city journal reversal: %w", err)
		}
		if alreadyReversed {
			return nil, cityLedgerReject(cityLedgerRejectionAlreadyReversed)
		}
		spec.journalType = "reversal"
		spec.reversalOfJournalID = &original.ID
		spec.description = payload.Reason
		spec.metadata["reversal_of_tick"] = original.Tick
		spec.metadata["reversal_of_sequence"] = original.Sequence
		spec.metadata["reason"] = payload.Reason
		spec.lines = make([]cityLedgerPostingLine, 0, len(original.Entries))
		for _, originalEntry := range original.Entries {
			account, loadErr := loadCityLedgerAccountByID(ctx, tx, worldID, unit.id, originalEntry.AccountID)
			if loadErr != nil {
				return nil, loadErr
			}
			spec.lines = append(spec.lines, cityLedgerPostingLine{
				account: account, debitUnits: originalEntry.CreditUnits,
				creditUnits: originalEntry.DebitUnits, memo: payload.Reason,
			})
		}
	default:
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"command_type": command.CommandType})
	}
	return postCityJournal(ctx, tx, spec)
}

func cityTransferEntityTypeAllowed(entityType string) bool {
	return entityType == CityEntityTypeHousehold || entityType == CityEntityTypeFirm
}

func postCityJournal(ctx context.Context, tx *sql.Tx, spec cityLedgerJournalSpec) (*CityJournal, error) {
	if spec.worldID <= 0 || spec.unit == nil || spec.unit.id <= 0 || spec.tick <= 0 || spec.sequence <= 0 ||
		len(spec.operationKey) < 1 || len(spec.operationKey) > 128 || utf8.RuneCountInString(spec.description) > 256 {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "journal_header"})
	}
	if err := validateCityLedgerPostingLines(spec.lines); err != nil {
		return nil, err
	}
	accountIDs := make([]int64, 0, len(spec.lines))
	for _, line := range spec.lines {
		accountIDs = append(accountIDs, line.account.id)
	}
	sort.Slice(accountIDs, func(i, j int) bool { return accountIDs[i] < accountIDs[j] })
	lockedRows, err := tx.QueryContext(ctx, `
SELECT id
FROM city_accounts
WHERE world_id = $1 AND monetary_unit_id = $2 AND id = ANY($3) AND status = 'active'
ORDER BY id ASC
FOR UPDATE`, spec.worldID, spec.unit.id, pq.Array(accountIDs))
	if err != nil {
		return nil, fmt.Errorf("lock city journal accounts: %w", err)
	}
	lockedCount := 0
	for lockedRows.Next() {
		var ignored int64
		if err = lockedRows.Scan(&ignored); err != nil {
			_ = lockedRows.Close()
			return nil, err
		}
		lockedCount++
	}
	if err = lockedRows.Err(); err != nil {
		_ = lockedRows.Close()
		return nil, fmt.Errorf("iterate locked city journal accounts: %w", err)
	}
	_ = lockedRows.Close()
	if lockedCount != len(accountIDs) {
		return nil, cityLedgerReject(cityLedgerRejectionAccountNotFound)
	}
	metadata, err := json.Marshal(spec.metadata)
	if err != nil {
		return nil, fmt.Errorf("marshal city journal metadata: %w", err)
	}
	var sourceCommandID, marketSettlementID, reversalOfJournalID any
	if spec.sourceCommandID != nil {
		sourceCommandID = *spec.sourceCommandID
	}
	if spec.marketSettlementID != nil {
		marketSettlementID = *spec.marketSettlementID
	}
	if spec.reversalOfJournalID != nil {
		reversalOfJournalID = *spec.reversalOfJournalID
	}
	var journalID int64
	err = tx.QueryRowContext(ctx, `
INSERT INTO city_journals
    (world_id, monetary_unit_id, tick, sequence, operation_key, journal_type,
     source_command_id, market_settlement_id, reversal_of_journal_id, description, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb)
RETURNING id`, spec.worldID, spec.unit.id, spec.tick, spec.sequence, spec.operationKey,
		spec.journalType, sourceCommandID, marketSettlementID, reversalOfJournalID,
		spec.description, metadata).Scan(&journalID)
	if err != nil {
		return nil, fmt.Errorf("create city journal draft: %w", err)
	}
	for index, line := range spec.lines {
		var entryID int64
		if err = tx.QueryRowContext(ctx, `
SELECT post_city_journal_entry($1, $2, $3, $4, $5, $6)`,
			journalID, line.account.id, index+1, line.debitUnits, line.creditUnits, line.memo).Scan(&entryID); err != nil {
			return nil, fmt.Errorf("post city journal entry %d: %w", index+1, err)
		}
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_journals SET posted_at = NOW() WHERE id = $1 AND posted_at IS NULL`, journalID)
	if err != nil {
		return nil, fmt.Errorf("seal city journal: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil || rowsAffected != 1 {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"journal_id": strconv.FormatInt(journalID, 10)})
	}
	journal, err := loadCityJournalByID(ctx, tx, spec.worldID, journalID, true)
	if err != nil {
		return nil, fmt.Errorf("load posted city journal: %w", err)
	}
	if err = syncCityLedgerAccountRefs(spec.lines, journal.Entries); err != nil {
		return nil, err
	}
	return journal, nil
}

func syncCityLedgerAccountRefs(lines []cityLedgerPostingLine, entries []*CityJournalEntry) error {
	if len(lines) != len(entries) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "journal_projection_sync"})
	}
	accounts := make(map[int64]*cityLedgerAccountRef, len(lines))
	for _, line := range lines {
		if line.account == nil {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "journal_projection_sync"})
		}
		accounts[line.account.id] = line.account
	}
	for _, entry := range entries {
		account := accounts[entry.AccountID]
		if account == nil || account.balanceUnits != entry.BalanceBeforeUnits ||
			account.version != entry.AccountVersionBefore {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{
				"field": "journal_projection_chain", "account_id": strconv.FormatInt(entry.AccountID, 10),
			})
		}
		account.balanceUnits = entry.BalanceAfterUnits
		account.version = entry.AccountVersionAfter
	}
	return nil
}

func validateCityLedgerPostingLines(lines []cityLedgerPostingLine) error {
	if len(lines) < 2 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "journal_lines"})
	}
	seenAccounts := make(map[int64]struct{}, len(lines))
	entityDebits := make(map[int64]int64)
	entityCredits := make(map[int64]int64)
	var totalDebits, totalCredits int64
	for _, line := range lines {
		if line.account == nil || line.account.id <= 0 || utf8.RuneCountInString(line.memo) > 256 ||
			!((line.debitUnits > 0 && line.creditUnits == 0) || (line.creditUnits > 0 && line.debitUnits == 0)) {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "journal_line"})
		}
		if _, duplicate := seenAccounts[line.account.id]; duplicate {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "journal_account_duplicate"})
		}
		seenAccounts[line.account.id] = struct{}{}
		var err error
		totalDebits, err = addCityLedgerUnits(totalDebits, line.debitUnits)
		if err != nil {
			return err
		}
		totalCredits, err = addCityLedgerUnits(totalCredits, line.creditUnits)
		if err != nil {
			return err
		}
		entityDebits[line.account.entityID], err = addCityLedgerUnits(entityDebits[line.account.entityID], line.debitUnits)
		if err != nil {
			return err
		}
		entityCredits[line.account.entityID], err = addCityLedgerUnits(entityCredits[line.account.entityID], line.creditUnits)
		if err != nil {
			return err
		}
		projected, projectErr := projectCityLedgerBalance(line.account, line.debitUnits, line.creditUnits)
		if projectErr != nil {
			return projectErr
		}
		if !line.account.allowNegative && projected < 0 {
			return cityLedgerReject(cityLedgerRejectionInsufficient)
		}
	}
	if totalDebits <= 0 || totalDebits != totalCredits {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "journal_balance"})
	}
	for entityID, debits := range entityDebits {
		if debits != entityCredits[entityID] {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "journal_entity_balance"})
		}
	}
	return nil
}

func projectCityLedgerBalance(account *cityLedgerAccountRef, debitUnits, creditUnits int64) (int64, error) {
	delta := debitUnits - creditUnits
	if account.normalSide == "credit" {
		delta = creditUnits - debitUnits
	}
	if delta > 0 && account.balanceUnits > math.MaxInt64-delta {
		return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"account_id": strconv.FormatInt(account.id, 10)})
	}
	if delta < 0 && account.balanceUnits < math.MinInt64-delta {
		return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"account_id": strconv.FormatInt(account.id, 10)})
	}
	return account.balanceUnits + delta, nil
}

func loadCityJournalByCursor(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, tick, sequence int64,
	withEntries bool,
) (*CityJournal, error) {
	journal, err := scanCityJournal(queryer.QueryRowContext(ctx, `
SELECT `+cityJournalSelectColumns+`
FROM city_journals j
`+cityJournalSelectJoins+`
WHERE j.world_id = $1 AND j.tick = $2 AND j.sequence = $3 AND j.posted_at IS NOT NULL`,
		worldID, tick, sequence))
	if err != nil {
		return nil, err
	}
	if withEntries {
		journal.Entries, err = loadCityJournalEntries(ctx, queryer, journal.ID)
		if err != nil {
			return nil, err
		}
	}
	return journal, nil
}

func loadCityJournalByID(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, journalID int64,
	withEntries bool,
) (*CityJournal, error) {
	journal, err := scanCityJournal(queryer.QueryRowContext(ctx, `
SELECT `+cityJournalSelectColumns+`
FROM city_journals j
`+cityJournalSelectJoins+`
WHERE j.world_id = $1 AND j.id = $2 AND j.posted_at IS NOT NULL`, worldID, journalID))
	if err != nil {
		return nil, err
	}
	if withEntries {
		journal.Entries, err = loadCityJournalEntries(ctx, queryer, journal.ID)
		if err != nil {
			return nil, err
		}
	}
	return journal, nil
}

func loadCityJournalsForTick(ctx context.Context, queryer citySQLQueryer, worldID, tick int64) ([]*CityJournal, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT `+cityJournalSelectColumns+`
FROM city_journals j
`+cityJournalSelectJoins+`
WHERE j.world_id = $1 AND j.tick = $2 AND j.posted_at IS NOT NULL
ORDER BY j.sequence ASC`, worldID, tick)
	if err != nil {
		return nil, fmt.Errorf("load city tick journals: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]*CityJournal, 0)
	for rows.Next() {
		item, scanErr := scanCityJournal(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate city tick journals: %w", err)
	}
	return items, nil
}

func loadCityJournalEntries(ctx context.Context, queryer citySQLQueryer, journalID int64) ([]*CityJournalEntry, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT entry.id, entry.journal_id, entry.line_no, entry.account_id,
       entity.id, entity.entity_type, entity.code, entity.name,
       template.code, template.name, template.account_class, entry.normal_side,
       entry.debit_units, entry.credit_units, entry.balance_before_units,
       entry.balance_after_units, entry.account_version_before,
       entry.account_version_after, entry.memo, entry.created_at
FROM city_journal_entries entry
JOIN city_accounts account ON account.id = entry.account_id
JOIN city_economic_entities entity ON entity.id = account.entity_id
JOIN city_account_templates template ON template.id = account.template_id
WHERE entry.journal_id = $1
ORDER BY entry.line_no ASC`, journalID)
	if err != nil {
		return nil, fmt.Errorf("load city journal entries: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]*CityJournalEntry, 0)
	for rows.Next() {
		item := &CityJournalEntry{}
		if err = rows.Scan(
			&item.ID, &item.JournalID, &item.LineNo, &item.AccountID,
			&item.EntityID, &item.EntityType, &item.EntityCode, &item.EntityName,
			&item.AccountCode, &item.AccountName, &item.AccountClass, &item.NormalSide,
			&item.DebitUnits, &item.CreditUnits, &item.BalanceBeforeUnits,
			&item.BalanceAfterUnits, &item.AccountVersionBefore,
			&item.AccountVersionAfter, &item.Memo, &item.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate city journal entries: %w", err)
	}
	return items, nil
}

func scanCityJournal(row cityScannable) (*CityJournal, error) {
	item := &CityJournal{}
	var sourceCommandID, marketSettlementID, reversalID, reversalTick, reversalSequence sql.NullInt64
	var metadata []byte
	var entryCount int64
	var postedAt sql.NullTime
	if err := row.Scan(
		&item.ID, &item.WorldID, &item.MonetaryUnitID, &item.MonetaryUnitCode,
		&item.MonetaryUnitName, &item.MonetaryUnitSymbol, &item.MonetaryUnitScale,
		&item.Tick, &item.Sequence, &item.OperationKey, &item.JournalType,
		&sourceCommandID, &marketSettlementID, &reversalID, &reversalTick, &reversalSequence,
		&item.Description, &metadata, &entryCount, &item.DebitTotalUnits,
		&item.CreditTotalUnits, &item.CreatedAt, &postedAt,
	); err != nil {
		return nil, err
	}
	if !postedAt.Valid || entryCount < 2 || entryCount > int64(math.MaxInt) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"journal_id": strconv.FormatInt(item.ID, 10)})
	}
	item.PostedAt = postedAt.Time
	item.EntryCount = int(entryCount)
	item.SourceCommandID = nullInt64Pointer(sourceCommandID)
	item.MarketSettlementID = nullInt64Pointer(marketSettlementID)
	item.ReversalOfJournalID = nullInt64Pointer(reversalID)
	item.ReversalOfTick = nullInt64Pointer(reversalTick)
	item.ReversalOfSequence = nullInt64Pointer(reversalSequence)
	var err error
	item.Metadata, err = decodeCityJSONMap(metadata)
	if err != nil {
		return nil, fmt.Errorf("decode city journal metadata: %w", err)
	}
	if item.DebitTotalUnits <= 0 || item.DebitTotalUnits != item.CreditTotalUnits {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"journal_id": strconv.FormatInt(item.ID, 10)})
	}
	return item, nil
}
