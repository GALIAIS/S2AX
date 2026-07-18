package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

const (
	VirtualCurrencyStatusActive   = "active"
	VirtualCurrencyStatusDisabled = "disabled"

	VirtualCurrencyHoldStatusActive    = "active"
	VirtualCurrencyHoldStatusCommitted = "committed"
	VirtualCurrencyHoldStatusReleased  = "released"
	VirtualCurrencyHoldStatusExpired   = "expired"

	VirtualCurrencyEntryGrant      = "grant"
	VirtualCurrencyEntrySpend      = "spend"
	VirtualCurrencyEntryRefund     = "refund"
	VirtualCurrencyEntryAdjustment = "adjustment"
	VirtualCurrencyEntryReserve    = "reserve"
	VirtualCurrencyEntryCommit     = "commit"
	VirtualCurrencyEntryRelease    = "release"
	VirtualCurrencyEntryExpire     = "expire"

	VirtualCurrencySourceAdmin       = "admin"
	VirtualCurrencySourceAPI         = "api"
	VirtualCurrencySourceRedeemCode  = "redeem_code"
	VirtualCurrencySourceMission     = "mission"
	VirtualCurrencySourceReferral    = "referral"
	VirtualCurrencySourceActivity    = "activity"
	VirtualCurrencySourceGame        = "game"
	VirtualCurrencySourceIntegration = "integration"
)

var (
	ErrVirtualCurrencyNotFound       = infraerrors.NotFound("VIRTUAL_CURRENCY_NOT_FOUND", "virtual currency not found")
	ErrVirtualCurrencyCodeExists     = infraerrors.Conflict("VIRTUAL_CURRENCY_CODE_EXISTS", "virtual currency code already exists")
	ErrVirtualCurrencyDisabled       = infraerrors.Conflict("VIRTUAL_CURRENCY_DISABLED", "virtual currency is disabled")
	ErrVirtualCurrencyPolicyNotFound = infraerrors.NotFound("VIRTUAL_CURRENCY_POLICY_NOT_FOUND", "virtual currency group policy not found")
	ErrVirtualCurrencyGroupDenied    = infraerrors.Forbidden("VIRTUAL_CURRENCY_GROUP_DENIED", "user is not allowed to use this currency in the group")
	ErrVirtualCurrencyInsufficient   = infraerrors.BadRequest("VIRTUAL_CURRENCY_INSUFFICIENT", "insufficient virtual currency balance")
	ErrVirtualCurrencyLimitExceeded  = infraerrors.BadRequest("VIRTUAL_CURRENCY_LIMIT_EXCEEDED", "virtual currency balance limit exceeded")
	ErrVirtualCurrencyIdempotency    = infraerrors.Conflict("VIRTUAL_CURRENCY_IDEMPOTENCY_CONFLICT", "idempotency key was already used with a different request")
	ErrVirtualCurrencyInvalidInput   = infraerrors.BadRequest("VIRTUAL_CURRENCY_INVALID_INPUT", "invalid virtual currency request")
	ErrVirtualCurrencyHoldNotFound   = infraerrors.NotFound("VIRTUAL_CURRENCY_HOLD_NOT_FOUND", "virtual currency hold not found")
	ErrVirtualCurrencyHoldState      = infraerrors.Conflict("VIRTUAL_CURRENCY_HOLD_STATE", "virtual currency hold is no longer active")
	ErrVirtualCurrencyHoldExpired    = infraerrors.Conflict("VIRTUAL_CURRENCY_HOLD_EXPIRED", "virtual currency hold has expired")
)

var virtualCurrencyCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,31}$`)
var virtualCurrencySourcePattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{1,31}$`)

const maxVirtualCurrencyMutationUnits int64 = 1 << 60

const (
	maxVirtualCurrencySourceIDRunes = 128
	maxVirtualCurrencyReasonRunes   = 500
	maxVirtualCurrencyMetadataBytes = 16 * 1024
	maxVirtualCurrencyHoldTTL       = 7 * 24 * time.Hour
	maxVirtualCurrencyHoldKeyRunes  = 96
)

// VirtualCurrency is a configurable, non-fiat user asset.
type VirtualCurrency struct {
	ID          int64
	Code        string
	Name        string
	Symbol      string
	Description string
	Scale       int
	Status      string
	Metadata    map[string]any
	CreatedBy   *int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type VirtualCurrencyGroupPolicy struct {
	ID              int64
	CurrencyID      int64
	GroupID         int64
	Enabled         bool
	CanEarn         bool
	CanSpend        bool
	MaxBalanceUnits *int64
	Metadata        map[string]any
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type VirtualCurrencyWallet struct {
	CurrencyID     int64
	CurrencyCode   string
	CurrencyName   string
	CurrencySymbol string
	CurrencyScale  int
	AvailableUnits int64
	ReservedUnits  int64
	GroupIDs       []int64
	UpdatedAt      time.Time
}

type VirtualCurrencyLedgerEntry struct {
	ID                  int64
	JournalID           int64
	CurrencyID          int64
	CurrencyCode        string
	CurrencyName        string
	CurrencySymbol      string
	CurrencyScale       int
	UserID              int64
	GroupID             *int64
	DeltaUnits          int64
	AvailableDeltaUnits int64
	ReservedDeltaUnits  int64
	AvailableAfterUnits int64
	ReservedAfterUnits  int64
	EntryType           string
	SourceType          string
	SourceID            *string
	IdempotencyKey      string
	RequestFingerprint  string `json:"-"`
	Reason              string
	Metadata            map[string]any
	CreatedBy           *int64
	CreatedAt           time.Time
}

type VirtualCurrencyHold struct {
	ID             int64
	CurrencyID     int64
	CurrencyCode   string
	CurrencyName   string
	CurrencySymbol string
	CurrencyScale  int
	UserID         int64
	GroupID        *int64
	AmountUnits    int64
	Status         string
	SourceType     string
	SourceID       *string
	IdempotencyKey string
	ExpiresAt      time.Time
	Metadata       map[string]any
	CreatedAt      time.Time
	UpdatedAt      time.Time
	SettledAt      *time.Time
}

type VirtualCurrencyHoldResult struct {
	Hold   *VirtualCurrencyHold
	Ledger *VirtualCurrencyLedgerEntry
}

type VirtualCurrencyCreateInput struct {
	Code        string
	Name        string
	Symbol      string
	Description string
	Scale       int
	Metadata    map[string]any
	CreatedBy   *int64
}

type VirtualCurrencyUpdateInput struct {
	Name        *string
	Symbol      *string
	Description *string
	Metadata    map[string]any
}

type VirtualCurrencyPolicyInput struct {
	CurrencyID      int64
	GroupID         int64
	Enabled         bool
	CanEarn         bool
	CanSpend        bool
	MaxBalanceUnits *int64
	Metadata        map[string]any
}

type VirtualCurrencyAdjustmentInput struct {
	CurrencyCode      string
	UserID            int64
	GroupID           int64
	AmountUnits       int64
	EntryType         string
	SourceType        string
	SourceID          string
	IdempotencyKey    string
	Reason            string
	Metadata          map[string]any
	CreatedBy         *int64
	RequireUserAccess bool
}

// VirtualCurrencyGrantInput is the stable contract for every earning source.
// Mission, referral, redeem-code and game adapters should call Grant instead
// of changing a wallet directly. The currency service remains responsible for
// group policy checks, limits, idempotency and the immutable ledger.
type VirtualCurrencyGrantInput struct {
	CurrencyCode      string
	UserID            int64
	GroupID           int64
	AmountUnits       int64
	SourceType        string
	SourceID          string
	IdempotencyKey    string
	Reason            string
	Metadata          map[string]any
	CreatedBy         *int64
	RequireUserAccess bool
}

type VirtualCurrencySpendInput struct {
	CurrencyCode   string
	UserID         int64
	GroupID        int64
	AmountUnits    int64
	SourceType     string
	SourceID       string
	IdempotencyKey string
	Reason         string
	Metadata       map[string]any
}

// VirtualCurrencyReserveInput is the public domain contract for locking funds
// before an external order or game action is settled.
type VirtualCurrencyReserveInput struct {
	CurrencyCode   string
	UserID         int64
	GroupID        int64
	AmountUnits    int64
	ExpiresAt      time.Time
	SourceType     string
	SourceID       string
	IdempotencyKey string
	Reason         string
	Metadata       map[string]any
}

// VirtualCurrencyHoldSettlementInput is intentionally shared by commit and
// release. Action is set by the service method, never trusted from HTTP input.
type VirtualCurrencyHoldSettlementInput struct {
	HoldID         int64
	UserID         int64
	SourceType     string
	SourceID       string
	IdempotencyKey string
	Reason         string
	Metadata       map[string]any
}

type VirtualCurrencyHoldQuery struct {
	UserID       int64
	CurrencyCode string
	Status       string
	Page         int
	PageSize     int
}

type VirtualCurrencyLedgerQuery struct {
	UserID       int64
	CurrencyCode string
	Page         int
	PageSize     int
}

// CurrencyEarnProvider is intentionally small so future earning modules can
// depend on the currency domain without importing an HTTP handler or SQL
// repository. It is also convenient to fake in mission/game unit tests.
type CurrencyEarnProvider interface {
	Grant(ctx context.Context, input VirtualCurrencyGrantInput) (*VirtualCurrencyLedgerEntry, error)
}

// CurrencySpendProvider is the spending contract for immediate debits. Hold
// based purchase flows should additionally depend on CurrencySettlementProvider.
type CurrencySpendProvider interface {
	Spend(ctx context.Context, input VirtualCurrencySpendInput) (*VirtualCurrencyLedgerEntry, error)
}

// CurrencySettlementProvider is the optional hold contract for order/game
// adapters. Keeping it separate lets simple reward adapters depend only on
// CurrencyEarnProvider while purchase flows opt into reserve/commit/release.
type CurrencySettlementProvider interface {
	ReserveHold(ctx context.Context, input VirtualCurrencyReserveInput) (*VirtualCurrencyHoldResult, error)
	CommitHold(ctx context.Context, input VirtualCurrencyHoldSettlementInput) (*VirtualCurrencyHoldResult, error)
	ReleaseHold(ctx context.Context, input VirtualCurrencyHoldSettlementInput) (*VirtualCurrencyHoldResult, error)
}

type VirtualCurrencyReconciliationMismatch struct {
	UserID              int64 `json:"user_id"`
	WalletAvailable     int64 `json:"wallet_available_units"`
	WalletReserved      int64 `json:"wallet_reserved_units"`
	LedgerAvailable     int64 `json:"ledger_available_units"`
	LedgerReserved      int64 `json:"ledger_reserved_units"`
	WalletExists        bool  `json:"wallet_exists"`
	LedgerSnapshotFound bool  `json:"ledger_snapshot_found"`
}

// VirtualCurrencyAccountingSummary contains exact decimal strings for totals.
// Aggregate sums can exceed one wallet's int64 range, so these values must not
// be routed through float64 or JavaScript Number.
type VirtualCurrencyAccountingSummary struct {
	JournalCount             int64  `json:"journal_count"`
	PostingCount             int64  `json:"posting_count"`
	InvalidJournalCount      int64  `json:"invalid_journal_count"`
	WalletAvailableUnits     string `json:"wallet_available_units"`
	WalletReservedUnits      string `json:"wallet_reserved_units"`
	PostedUserAvailableUnits string `json:"posted_user_available_units"`
	PostedUserReservedUnits  string `json:"posted_user_reserved_units"`
	GrossIssuedUnits         string `json:"gross_issued_units"`
	NetSinkUnits             string `json:"net_sink_units"`
	NetAdjustmentUnits       string `json:"net_adjustment_units"`
	ProjectionDeltaUnits     string `json:"projection_delta_units"`
	ConservationDeltaUnits   string `json:"conservation_delta_units"`
}

type VirtualCurrencyReconciliationReport struct {
	CurrencyID      int64                                    `json:"currency_id"`
	WalletCount     int64                                    `json:"wallet_count"`
	LedgerUserCount int64                                    `json:"ledger_user_count"`
	MismatchCount   int64                                    `json:"mismatch_count"`
	SampleLimit     int                                      `json:"sample_limit"`
	Mismatches      []*VirtualCurrencyReconciliationMismatch `json:"mismatches"`
	Accounting      VirtualCurrencyAccountingSummary         `json:"accounting"`
	CheckedAt       time.Time                                `json:"checked_at"`
}

type VirtualCurrencyRepository interface {
	ListCurrencies(ctx context.Context, includeDisabled bool) ([]*VirtualCurrency, error)
	GetCurrencyByID(ctx context.Context, id int64) (*VirtualCurrency, error)
	GetCurrencyByCode(ctx context.Context, code string) (*VirtualCurrency, error)
	CreateCurrency(ctx context.Context, input VirtualCurrencyCreateInput) (*VirtualCurrency, error)
	UpdateCurrency(ctx context.Context, id int64, input VirtualCurrencyUpdateInput) (*VirtualCurrency, error)
	SetCurrencyStatus(ctx context.Context, id int64, status string) (*VirtualCurrency, error)
	ListGroupPolicies(ctx context.Context, currencyID int64) ([]*VirtualCurrencyGroupPolicy, error)
	UpsertGroupPolicy(ctx context.Context, input VirtualCurrencyPolicyInput) (*VirtualCurrencyGroupPolicy, error)
	EnableForAllUsers(ctx context.Context, currencyID int64) ([]*VirtualCurrencyGroupPolicy, error)
	DeleteGroupPolicy(ctx context.Context, currencyID, groupID int64) error
	ApplyCurrencyDelta(ctx context.Context, input VirtualCurrencyDeltaInput) (*VirtualCurrencyLedgerEntry, error)
	ReserveHold(ctx context.Context, input VirtualCurrencyHoldReserveInput) (*VirtualCurrencyHold, *VirtualCurrencyLedgerEntry, error)
	CommitHold(ctx context.Context, input VirtualCurrencyHoldSettlementRepositoryInput) (*VirtualCurrencyHold, *VirtualCurrencyLedgerEntry, error)
	ReleaseHold(ctx context.Context, input VirtualCurrencyHoldSettlementRepositoryInput) (*VirtualCurrencyHold, *VirtualCurrencyLedgerEntry, error)
	ExpireExpiredHolds(ctx context.Context, currencyID int64, limit int) (int64, error)
	GetHold(ctx context.Context, userID, holdID int64) (*VirtualCurrencyHold, error)
	ListHolds(ctx context.Context, query VirtualCurrencyHoldQuery) ([]*VirtualCurrencyHold, *pagination.PaginationResult, error)
	ListUserWallets(ctx context.Context, userID int64) ([]*VirtualCurrencyWallet, error)
	ListLedger(ctx context.Context, query VirtualCurrencyLedgerQuery) ([]*VirtualCurrencyLedgerEntry, *pagination.PaginationResult, error)
	ReconcileCurrency(ctx context.Context, currencyID int64, sampleLimit int) (*VirtualCurrencyReconciliationReport, error)
}

// VirtualCurrencyHoldReserveInput is the repository-facing form of a reserve
// request. The currency is already resolved by the service and the fingerprint
// is generated server-side.
type VirtualCurrencyHoldReserveInput struct {
	CurrencyID         int64
	UserID             int64
	GroupID            int64
	AmountUnits        int64
	ExpiresAt          time.Time
	SourceType         string
	SourceID           string
	IdempotencyKey     string
	RequestFingerprint string
	Reason             string
	Metadata           map[string]any
}

// VirtualCurrencyHoldSettlementRepositoryInput is never populated directly by
// an HTTP client. It carries the action and fingerprint selected by the service
// into the repository transaction.
type VirtualCurrencyHoldSettlementRepositoryInput struct {
	HoldID             int64
	UserID             int64
	Action             string
	SourceType         string
	SourceID           string
	IdempotencyKey     string
	RequestFingerprint string
	Reason             string
	Metadata           map[string]any
}

// VirtualCurrencyDeltaInput is the repository-level atomic mutation contract.
// available_delta_units and reserved_delta_units are separate so reserve/commit/release can be added without changing the ledger shape.
type VirtualCurrencyDeltaInput struct {
	CurrencyID                   int64
	UserID                       int64
	GroupID                      int64
	DeltaUnits                   int64
	AvailableDeltaUnits          int64
	ReservedDeltaUnits           int64
	EntryType                    string
	SourceType                   string
	SourceID                     string
	IdempotencyKey               string
	RequestFingerprint           string
	Reason                       string
	Metadata                     map[string]any
	CreatedBy                    *int64
	RequireUserAccess            bool
	RequireCanSpend              bool
	RequireCanEarn               bool
	AllowSettlementWithoutPolicy bool
}

// VirtualCurrencyService owns business validation and keeps all balance mutations behind one interface.
type VirtualCurrencyService struct {
	repo VirtualCurrencyRepository
}

func NewVirtualCurrencyService(repo VirtualCurrencyRepository) *VirtualCurrencyService {
	return &VirtualCurrencyService{repo: repo}
}

func (s *VirtualCurrencyService) ListCurrencies(ctx context.Context, includeDisabled bool) ([]*VirtualCurrency, error) {
	if s == nil || s.repo == nil {
		return nil, errors.New("virtual currency repository is not configured")
	}
	return s.repo.ListCurrencies(ctx, includeDisabled)
}

func (s *VirtualCurrencyService) GetCurrency(ctx context.Context, id int64) (*VirtualCurrency, error) {
	if id <= 0 {
		return nil, ErrVirtualCurrencyInvalidInput
	}
	currency, err := s.repo.GetCurrencyByID(ctx, id)
	if errors.Is(err, ErrVirtualCurrencyNotFound) {
		return nil, err
	}
	return currency, err
}

func (s *VirtualCurrencyService) GetCurrencyByCode(ctx context.Context, code string) (*VirtualCurrency, error) {
	code = strings.ToLower(strings.TrimSpace(code))
	if !virtualCurrencyCodePattern.MatchString(code) {
		return nil, ErrVirtualCurrencyInvalidInput
	}
	return s.repo.GetCurrencyByCode(ctx, code)
}

// GrantByID is used by trusted server-side adapters that persist a currency
// foreign key (for example redeem codes). Resolving the code and applying the
// grant through the same context keeps the operation transaction-aware.
func (s *VirtualCurrencyService) GrantByID(ctx context.Context, currencyID int64, input VirtualCurrencyGrantInput) (*VirtualCurrencyLedgerEntry, error) {
	currency, err := s.GetCurrency(ctx, currencyID)
	if err != nil {
		return nil, err
	}
	input.CurrencyCode = currency.Code
	return s.Grant(ctx, input)
}

func (s *VirtualCurrencyService) CreateCurrency(ctx context.Context, input VirtualCurrencyCreateInput) (*VirtualCurrency, error) {
	input.Code = strings.ToLower(strings.TrimSpace(input.Code))
	input.Name = strings.TrimSpace(input.Name)
	input.Symbol = strings.TrimSpace(input.Symbol)
	input.Description = strings.TrimSpace(input.Description)
	if !virtualCurrencyCodePattern.MatchString(input.Code) || input.Name == "" || len([]rune(input.Name)) > 64 {
		return nil, ErrVirtualCurrencyInvalidInput
	}
	if len([]rune(input.Symbol)) > 16 || len([]rune(input.Description)) > 2000 || input.Scale < 0 || input.Scale > 8 {
		return nil, ErrVirtualCurrencyInvalidInput
	}
	if input.Metadata == nil {
		input.Metadata = map[string]any{}
	}
	if err := validateVirtualCurrencyMetadata(input.Metadata); err != nil {
		return nil, err
	}
	return s.repo.CreateCurrency(ctx, input)
}

func (s *VirtualCurrencyService) UpdateCurrency(ctx context.Context, id int64, input VirtualCurrencyUpdateInput) (*VirtualCurrency, error) {
	if id <= 0 || (input.Name == nil && input.Symbol == nil && input.Description == nil && input.Metadata == nil) {
		return nil, ErrVirtualCurrencyInvalidInput
	}
	if input.Name != nil {
		value := strings.TrimSpace(*input.Name)
		if value == "" || len([]rune(value)) > 64 {
			return nil, ErrVirtualCurrencyInvalidInput
		}
		input.Name = &value
	}
	if input.Symbol != nil {
		value := strings.TrimSpace(*input.Symbol)
		if len([]rune(value)) > 16 {
			return nil, ErrVirtualCurrencyInvalidInput
		}
		input.Symbol = &value
	}
	if input.Description != nil {
		value := strings.TrimSpace(*input.Description)
		if len([]rune(value)) > 2000 {
			return nil, ErrVirtualCurrencyInvalidInput
		}
		input.Description = &value
	}
	if input.Metadata != nil {
		if err := validateVirtualCurrencyMetadata(input.Metadata); err != nil {
			return nil, err
		}
	}
	return s.repo.UpdateCurrency(ctx, id, input)
}

func (s *VirtualCurrencyService) SetCurrencyStatus(ctx context.Context, id int64, status string) (*VirtualCurrency, error) {
	status = strings.ToLower(strings.TrimSpace(status))
	if id <= 0 || (status != VirtualCurrencyStatusActive && status != VirtualCurrencyStatusDisabled) {
		return nil, ErrVirtualCurrencyInvalidInput
	}
	return s.repo.SetCurrencyStatus(ctx, id, status)
}

func (s *VirtualCurrencyService) ListGroupPolicies(ctx context.Context, currencyID int64) ([]*VirtualCurrencyGroupPolicy, error) {
	if currencyID <= 0 {
		return nil, ErrVirtualCurrencyInvalidInput
	}
	return s.repo.ListGroupPolicies(ctx, currencyID)
}

// ValidateGrantPolicy verifies the configuration needed by a server-side
// reward source before it creates a redeem code or reward event. The user's
// actual group entitlement is checked again when the reward is claimed.
func (s *VirtualCurrencyService) ValidateGrantPolicy(ctx context.Context, currencyID, groupID int64) (*VirtualCurrency, error) {
	if currencyID <= 0 || groupID <= 0 {
		return nil, ErrVirtualCurrencyInvalidInput
	}
	currency, err := s.GetCurrency(ctx, currencyID)
	if err != nil {
		return nil, err
	}
	if currency.Status != VirtualCurrencyStatusActive {
		return nil, ErrVirtualCurrencyDisabled
	}
	policies, err := s.ListGroupPolicies(ctx, currencyID)
	if err != nil {
		return nil, err
	}
	for _, policy := range policies {
		if policy != nil && policy.GroupID == groupID && policy.Enabled && policy.CanEarn {
			return currency, nil
		}
	}
	return nil, ErrVirtualCurrencyGroupDenied
}

func (s *VirtualCurrencyService) UpsertGroupPolicy(ctx context.Context, input VirtualCurrencyPolicyInput) (*VirtualCurrencyGroupPolicy, error) {
	if input.CurrencyID <= 0 || input.GroupID <= 0 || (input.MaxBalanceUnits != nil && (*input.MaxBalanceUnits <= 0 || *input.MaxBalanceUnits > maxVirtualCurrencyMutationUnits)) {
		return nil, ErrVirtualCurrencyInvalidInput
	}
	if input.Metadata == nil {
		input.Metadata = map[string]any{}
	}
	if err := validateVirtualCurrencyMetadata(input.Metadata); err != nil {
		return nil, err
	}
	return s.repo.UpsertGroupPolicy(ctx, input)
}

// EnableForAllUsers opens the currency on every active public standard group.
// Public group policies cover both current and future users without eagerly
// creating one wallet row per user; wallets remain materialized on first use.
func (s *VirtualCurrencyService) EnableForAllUsers(ctx context.Context, currencyID int64) ([]*VirtualCurrencyGroupPolicy, error) {
	if currencyID <= 0 {
		return nil, ErrVirtualCurrencyInvalidInput
	}
	currency, err := s.GetCurrency(ctx, currencyID)
	if err != nil {
		return nil, err
	}
	if currency.Status != VirtualCurrencyStatusActive {
		return nil, ErrVirtualCurrencyDisabled
	}
	return s.repo.EnableForAllUsers(ctx, currencyID)
}

func (s *VirtualCurrencyService) DeleteGroupPolicy(ctx context.Context, currencyID, groupID int64) error {
	if currencyID <= 0 || groupID <= 0 {
		return ErrVirtualCurrencyInvalidInput
	}
	return s.repo.DeleteGroupPolicy(ctx, currencyID, groupID)
}

// Grant is the single earning entry point for admin-independent adapters.
// A positive amount is mandatory and is always checked against can_earn.
func (s *VirtualCurrencyService) Grant(ctx context.Context, input VirtualCurrencyGrantInput) (*VirtualCurrencyLedgerEntry, error) {
	if input.AmountUnits <= 0 {
		return nil, ErrVirtualCurrencyInvalidInput
	}
	return s.Adjust(ctx, VirtualCurrencyAdjustmentInput{
		CurrencyCode:      input.CurrencyCode,
		UserID:            input.UserID,
		GroupID:           input.GroupID,
		AmountUnits:       input.AmountUnits,
		EntryType:         VirtualCurrencyEntryGrant,
		SourceType:        input.SourceType,
		SourceID:          input.SourceID,
		IdempotencyKey:    input.IdempotencyKey,
		Reason:            input.Reason,
		Metadata:          input.Metadata,
		CreatedBy:         input.CreatedBy,
		RequireUserAccess: input.RequireUserAccess,
	})
}

func (s *VirtualCurrencyService) Adjust(ctx context.Context, input VirtualCurrencyAdjustmentInput) (*VirtualCurrencyLedgerEntry, error) {
	input.CurrencyCode = strings.ToLower(strings.TrimSpace(input.CurrencyCode))
	input.EntryType = strings.ToLower(strings.TrimSpace(input.EntryType))
	input.SourceType = strings.ToLower(strings.TrimSpace(input.SourceType))
	input.SourceID = strings.TrimSpace(input.SourceID)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	input.Reason = strings.TrimSpace(input.Reason)
	if input.UserID <= 0 || input.GroupID < 0 || input.CurrencyCode == "" || input.AmountUnits == 0 || input.SourceType == "" || input.IdempotencyKey == "" || len([]rune(input.IdempotencyKey)) > maxVirtualCurrencySourceIDRunes {
		return nil, ErrVirtualCurrencyInvalidInput
	}
	if input.GroupID == 0 && input.SourceType != VirtualCurrencySourceAdmin {
		return nil, ErrVirtualCurrencyInvalidInput
	}
	if input.AmountUnits > maxVirtualCurrencyMutationUnits || input.AmountUnits < -maxVirtualCurrencyMutationUnits {
		return nil, ErrVirtualCurrencyInvalidInput
	}
	if input.EntryType == "" {
		if input.AmountUnits > 0 {
			input.EntryType = VirtualCurrencyEntryGrant
		} else {
			input.EntryType = VirtualCurrencyEntryAdjustment
		}
	}
	switch input.EntryType {
	case VirtualCurrencyEntryGrant, VirtualCurrencyEntryRefund:
		if input.AmountUnits <= 0 {
			return nil, ErrVirtualCurrencyInvalidInput
		}
	case VirtualCurrencyEntryAdjustment:
	default:
		return nil, ErrVirtualCurrencyInvalidInput
	}
	if !virtualCurrencySourcePattern.MatchString(input.SourceType) ||
		len([]rune(input.SourceID)) > maxVirtualCurrencySourceIDRunes || len([]rune(input.Reason)) > maxVirtualCurrencyReasonRunes {
		return nil, ErrVirtualCurrencyInvalidInput
	}
	if input.Metadata == nil {
		input.Metadata = map[string]any{}
	}
	if err := validateVirtualCurrencyMetadata(input.Metadata); err != nil {
		return nil, err
	}
	currency, err := s.repo.GetCurrencyByCode(ctx, input.CurrencyCode)
	if err != nil {
		return nil, err
	}
	if currency == nil {
		return nil, ErrVirtualCurrencyDisabled
	}
	if input.GroupID == 0 {
		input.GroupID, err = s.resolveAdminAdjustmentGroup(ctx, currency.ID, input.UserID, input.AmountUnits > 0)
		if err != nil {
			return nil, err
		}
	}
	fingerprint := virtualCurrencyMutationFingerprint(currency.ID, input.UserID, input.GroupID, input.AmountUnits, 0, input.EntryType, input.SourceType, input.SourceID, input.Reason, input.Metadata)
	return s.repo.ApplyCurrencyDelta(ctx, VirtualCurrencyDeltaInput{
		CurrencyID:          currency.ID,
		UserID:              input.UserID,
		GroupID:             input.GroupID,
		DeltaUnits:          input.AmountUnits,
		AvailableDeltaUnits: input.AmountUnits,
		EntryType:           input.EntryType,
		SourceType:          input.SourceType,
		SourceID:            input.SourceID,
		IdempotencyKey:      input.IdempotencyKey,
		RequestFingerprint:  fingerprint,
		Reason:              input.Reason,
		Metadata:            input.Metadata,
		CreatedBy:           input.CreatedBy,
		RequireUserAccess:   input.RequireUserAccess || (input.SourceType == VirtualCurrencySourceAdmin && input.AmountUnits > 0),
		RequireCanEarn:      input.AmountUnits > 0,
	})
}

func (s *VirtualCurrencyService) resolveAdminAdjustmentGroup(ctx context.Context, currencyID, userID int64, requireEarn bool) (int64, error) {
	wallets, err := s.repo.ListUserWallets(ctx, userID)
	if err != nil {
		return 0, err
	}
	accessible := make(map[int64]struct{})
	for _, wallet := range wallets {
		if wallet != nil && wallet.CurrencyID == currencyID {
			for _, groupID := range wallet.GroupIDs {
				accessible[groupID] = struct{}{}
			}
			break
		}
	}
	policies, err := s.repo.ListGroupPolicies(ctx, currencyID)
	if err != nil {
		return 0, err
	}
	var selected int64
	for _, policy := range policies {
		if policy == nil || !policy.Enabled || (requireEarn && !policy.CanEarn) {
			continue
		}
		if _, ok := accessible[policy.GroupID]; !ok {
			continue
		}
		if selected == 0 || policy.GroupID < selected {
			selected = policy.GroupID
		}
	}
	if selected == 0 {
		return 0, ErrVirtualCurrencyGroupDenied
	}
	return selected, nil
}

func (s *VirtualCurrencyService) Spend(ctx context.Context, input VirtualCurrencySpendInput) (*VirtualCurrencyLedgerEntry, error) {
	input.CurrencyCode = strings.ToLower(strings.TrimSpace(input.CurrencyCode))
	input.SourceType = strings.ToLower(strings.TrimSpace(input.SourceType))
	input.SourceID = strings.TrimSpace(input.SourceID)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	input.Reason = strings.TrimSpace(input.Reason)
	if input.UserID <= 0 || input.GroupID <= 0 || input.AmountUnits <= 0 || input.AmountUnits > maxVirtualCurrencyMutationUnits || input.CurrencyCode == "" || input.SourceType == "" || input.IdempotencyKey == "" || len([]rune(input.IdempotencyKey)) > maxVirtualCurrencySourceIDRunes {
		return nil, ErrVirtualCurrencyInvalidInput
	}
	if !virtualCurrencySourcePattern.MatchString(input.SourceType) ||
		len([]rune(input.SourceID)) > maxVirtualCurrencySourceIDRunes || len([]rune(input.Reason)) > maxVirtualCurrencyReasonRunes {
		return nil, ErrVirtualCurrencyInvalidInput
	}
	if input.Metadata == nil {
		input.Metadata = map[string]any{}
	}
	if err := validateVirtualCurrencyMetadata(input.Metadata); err != nil {
		return nil, err
	}
	currency, err := s.repo.GetCurrencyByCode(ctx, input.CurrencyCode)
	if err != nil {
		return nil, err
	}
	if currency == nil {
		return nil, ErrVirtualCurrencyDisabled
	}
	delta := -input.AmountUnits
	fingerprint := virtualCurrencyMutationFingerprint(currency.ID, input.UserID, input.GroupID, delta, 0, VirtualCurrencyEntrySpend, input.SourceType, input.SourceID, input.Reason, input.Metadata)
	return s.repo.ApplyCurrencyDelta(ctx, VirtualCurrencyDeltaInput{
		CurrencyID:          currency.ID,
		UserID:              input.UserID,
		GroupID:             input.GroupID,
		DeltaUnits:          delta,
		AvailableDeltaUnits: delta,
		EntryType:           VirtualCurrencyEntrySpend,
		SourceType:          input.SourceType,
		SourceID:            input.SourceID,
		IdempotencyKey:      input.IdempotencyKey,
		RequestFingerprint:  fingerprint,
		Reason:              input.Reason,
		Metadata:            input.Metadata,
		RequireUserAccess:   true,
		RequireCanSpend:     true,
	})
}

// ReserveHold moves funds from available to reserved and creates the hold in
// the same database transaction. A missing expiry defaults to a short order
// window, while callers may request a longer window up to seven days.
func (s *VirtualCurrencyService) ReserveHold(ctx context.Context, input VirtualCurrencyReserveInput) (*VirtualCurrencyHoldResult, error) {
	input.CurrencyCode = strings.ToLower(strings.TrimSpace(input.CurrencyCode))
	input.SourceType = strings.ToLower(strings.TrimSpace(input.SourceType))
	input.SourceID = strings.TrimSpace(input.SourceID)
	input.Reason = strings.TrimSpace(input.Reason)
	if input.UserID <= 0 || input.GroupID <= 0 || input.AmountUnits <= 0 || input.AmountUnits > maxVirtualCurrencyMutationUnits || input.CurrencyCode == "" {
		return nil, ErrVirtualCurrencyInvalidInput
	}
	if !virtualCurrencySourcePattern.MatchString(input.SourceType) || len([]rune(input.SourceID)) > maxVirtualCurrencySourceIDRunes || len([]rune(input.Reason)) > maxVirtualCurrencyReasonRunes {
		return nil, ErrVirtualCurrencyInvalidInput
	}
	idempotencyKey, err := virtualCurrencyHoldIdempotencyKey("reserve", input.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	if input.Metadata == nil {
		input.Metadata = map[string]any{}
	}
	if err := validateVirtualCurrencyMetadata(input.Metadata); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	expiresAt := input.ExpiresAt.UTC()
	if input.ExpiresAt.IsZero() {
		expiresAt = now.Add(15 * time.Minute)
	}
	if !expiresAt.After(now) || expiresAt.After(now.Add(maxVirtualCurrencyHoldTTL)) {
		return nil, ErrVirtualCurrencyInvalidInput
	}
	currency, err := s.repo.GetCurrencyByCode(ctx, input.CurrencyCode)
	if err != nil {
		return nil, err
	}
	fingerprint := virtualCurrencyHoldMutationFingerprint(currency.ID, input.UserID, input.GroupID, input.AmountUnits, expiresAt, VirtualCurrencyEntryReserve, input.SourceType, input.SourceID, input.Reason, input.Metadata)
	hold, ledger, err := s.repo.ReserveHold(ctx, VirtualCurrencyHoldReserveInput{
		CurrencyID:         currency.ID,
		UserID:             input.UserID,
		GroupID:            input.GroupID,
		AmountUnits:        input.AmountUnits,
		ExpiresAt:          expiresAt,
		SourceType:         input.SourceType,
		SourceID:           input.SourceID,
		IdempotencyKey:     idempotencyKey,
		RequestFingerprint: fingerprint,
		Reason:             input.Reason,
		Metadata:           input.Metadata,
	})
	if err != nil {
		return nil, err
	}
	return &VirtualCurrencyHoldResult{Hold: hold, Ledger: ledger}, nil
}

func (s *VirtualCurrencyService) CommitHold(ctx context.Context, input VirtualCurrencyHoldSettlementInput) (*VirtualCurrencyHoldResult, error) {
	return s.settleHold(ctx, input, VirtualCurrencyEntryCommit)
}

func (s *VirtualCurrencyService) ReleaseHold(ctx context.Context, input VirtualCurrencyHoldSettlementInput) (*VirtualCurrencyHoldResult, error) {
	return s.settleHold(ctx, input, VirtualCurrencyEntryRelease)
}

// ExpireExpiredHolds is safe to call from a scheduler or an admin maintenance
// action. The repository claims rows one at a time, so concurrent sweepers do
// not double-release a hold.
func (s *VirtualCurrencyService) ExpireExpiredHolds(ctx context.Context, currencyID int64, limit int) (int64, error) {
	if currencyID < 0 {
		return 0, ErrVirtualCurrencyInvalidInput
	}
	if limit < 1 {
		limit = 100
	}
	if limit > 500 {
		return 0, ErrVirtualCurrencyInvalidInput
	}
	return s.repo.ExpireExpiredHolds(ctx, currencyID, limit)
}

func (s *VirtualCurrencyService) settleHold(ctx context.Context, input VirtualCurrencyHoldSettlementInput, action string) (*VirtualCurrencyHoldResult, error) {
	input.SourceType = strings.ToLower(strings.TrimSpace(input.SourceType))
	input.SourceID = strings.TrimSpace(input.SourceID)
	input.Reason = strings.TrimSpace(input.Reason)
	if input.UserID <= 0 || input.HoldID <= 0 || !virtualCurrencySourcePattern.MatchString(input.SourceType) || len([]rune(input.SourceID)) > maxVirtualCurrencySourceIDRunes || len([]rune(input.Reason)) > maxVirtualCurrencyReasonRunes {
		return nil, ErrVirtualCurrencyInvalidInput
	}
	idempotencyKey, err := virtualCurrencyHoldIdempotencyKey(action, input.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	if input.Metadata == nil {
		input.Metadata = map[string]any{}
	}
	if err := validateVirtualCurrencyMetadata(input.Metadata); err != nil {
		return nil, err
	}
	hold, err := s.repo.GetHold(ctx, input.UserID, input.HoldID)
	if err != nil {
		return nil, err
	}
	fingerprint := virtualCurrencyHoldMutationFingerprint(hold.CurrencyID, hold.UserID, dereferenceInt64(hold.GroupID), hold.AmountUnits, hold.ExpiresAt, action, input.SourceType, input.SourceID, input.Reason, input.Metadata)
	var settle func(context.Context, VirtualCurrencyHoldSettlementRepositoryInput) (*VirtualCurrencyHold, *VirtualCurrencyLedgerEntry, error)
	switch action {
	case VirtualCurrencyEntryCommit:
		settle = s.repo.CommitHold
	case VirtualCurrencyEntryRelease:
		settle = s.repo.ReleaseHold
	default:
		return nil, ErrVirtualCurrencyInvalidInput
	}
	settledHold, ledger, err := settle(ctx, VirtualCurrencyHoldSettlementRepositoryInput{
		HoldID:             input.HoldID,
		UserID:             input.UserID,
		Action:             action,
		SourceType:         input.SourceType,
		SourceID:           input.SourceID,
		IdempotencyKey:     idempotencyKey,
		RequestFingerprint: fingerprint,
		Reason:             input.Reason,
		Metadata:           input.Metadata,
	})
	if err != nil {
		return nil, err
	}
	return &VirtualCurrencyHoldResult{Hold: settledHold, Ledger: ledger}, nil
}

func (s *VirtualCurrencyService) GetHold(ctx context.Context, userID, holdID int64) (*VirtualCurrencyHold, error) {
	if userID <= 0 || holdID <= 0 {
		return nil, ErrVirtualCurrencyInvalidInput
	}
	return s.repo.GetHold(ctx, userID, holdID)
}

func (s *VirtualCurrencyService) ListHolds(ctx context.Context, query VirtualCurrencyHoldQuery) ([]*VirtualCurrencyHold, *pagination.PaginationResult, error) {
	if query.UserID <= 0 {
		return nil, nil, ErrVirtualCurrencyInvalidInput
	}
	query.CurrencyCode = strings.ToLower(strings.TrimSpace(query.CurrencyCode))
	query.Status = strings.ToLower(strings.TrimSpace(query.Status))
	if query.Status != "" && !isVirtualCurrencyHoldStatus(query.Status) {
		return nil, nil, ErrVirtualCurrencyInvalidInput
	}
	if query.Page < 1 {
		query.Page = 1
	}
	if query.PageSize < 1 {
		query.PageSize = 20
	}
	if query.PageSize > 100 {
		query.PageSize = 100
	}
	return s.repo.ListHolds(ctx, query)
}

func (s *VirtualCurrencyService) ListUserWallets(ctx context.Context, userID int64) ([]*VirtualCurrencyWallet, error) {
	if userID <= 0 {
		return nil, ErrVirtualCurrencyInvalidInput
	}
	return s.repo.ListUserWallets(ctx, userID)
}

func (s *VirtualCurrencyService) ListLedger(ctx context.Context, query VirtualCurrencyLedgerQuery) ([]*VirtualCurrencyLedgerEntry, *pagination.PaginationResult, error) {
	if query.UserID <= 0 {
		return nil, nil, ErrVirtualCurrencyInvalidInput
	}
	query.CurrencyCode = strings.ToLower(strings.TrimSpace(query.CurrencyCode))
	if query.Page < 1 {
		query.Page = 1
	}
	if query.PageSize < 1 {
		query.PageSize = 20
	}
	if query.PageSize > 100 {
		query.PageSize = 100
	}
	return s.repo.ListLedger(ctx, query)
}

func (s *VirtualCurrencyService) ReconcileCurrency(ctx context.Context, currencyID int64, sampleLimit int) (*VirtualCurrencyReconciliationReport, error) {
	if currencyID <= 0 {
		return nil, ErrVirtualCurrencyInvalidInput
	}
	if sampleLimit < 1 {
		sampleLimit = 20
	}
	if sampleLimit > 100 {
		return nil, ErrVirtualCurrencyInvalidInput
	}
	return s.repo.ReconcileCurrency(ctx, currencyID, sampleLimit)
}

func (s *VirtualCurrencyService) FormatUnits(currency *VirtualCurrency, units int64) string {
	if currency == nil || currency.Scale == 0 {
		return fmt.Sprintf("%d", units)
	}
	negative := units < 0
	var magnitude uint64
	if negative {
		// -(MinInt64) overflows. Adding one before negating keeps the operation
		// representable, then the final +1 restores the exact magnitude.
		magnitude = uint64(-(units + 1)) + 1
	} else {
		magnitude = uint64(units)
	}
	base := uint64(1)
	for index := 0; index < currency.Scale; index++ {
		base *= 10
	}
	whole := magnitude / base
	fraction := magnitude % base
	result := fmt.Sprintf("%d.%0*d", whole, currency.Scale, fraction)
	if negative {
		return "-" + result
	}
	return result
}

func virtualCurrencyMutationFingerprint(currencyID, userID, groupID, deltaUnits, reservedDeltaUnits int64, entryType, sourceType, sourceID, reason string, metadata map[string]any) string {
	payload := struct {
		CurrencyID         int64          `json:"currency_id"`
		UserID             int64          `json:"user_id"`
		GroupID            int64          `json:"group_id"`
		DeltaUnits         int64          `json:"delta_units"`
		ReservedDeltaUnits int64          `json:"reserved_delta_units"`
		EntryType          string         `json:"entry_type"`
		SourceType         string         `json:"source_type"`
		SourceID           string         `json:"source_id"`
		Reason             string         `json:"reason"`
		Metadata           map[string]any `json:"metadata"`
	}{currencyID, userID, groupID, deltaUnits, reservedDeltaUnits, entryType, sourceType, sourceID, reason, metadata}
	encoded, _ := json.Marshal(payload)
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:])
}

func virtualCurrencyHoldMutationFingerprint(currencyID, userID, groupID, amountUnits int64, expiresAt time.Time, entryType, sourceType, sourceID, reason string, metadata map[string]any) string {
	payload := struct {
		CurrencyID  int64          `json:"currency_id"`
		UserID      int64          `json:"user_id"`
		GroupID     int64          `json:"group_id"`
		AmountUnits int64          `json:"amount_units"`
		ExpiresAt   int64          `json:"expires_at_unix_nano"`
		EntryType   string         `json:"entry_type"`
		SourceType  string         `json:"source_type"`
		SourceID    string         `json:"source_id"`
		Reason      string         `json:"reason"`
		Metadata    map[string]any `json:"metadata"`
	}{currencyID, userID, groupID, amountUnits, expiresAt.UTC().UnixNano(), entryType, sourceType, sourceID, reason, metadata}
	encoded, _ := json.Marshal(payload)
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:])
}

// VirtualCurrencyHoldMutationFingerprint lets trusted maintenance workers
// create the same request fingerprint as the normal settlement path.
func VirtualCurrencyHoldMutationFingerprint(currencyID, userID, groupID, amountUnits int64, expiresAt time.Time, entryType, sourceType, sourceID, reason string, metadata map[string]any) string {
	return virtualCurrencyHoldMutationFingerprint(currencyID, userID, groupID, amountUnits, expiresAt, entryType, sourceType, sourceID, reason, metadata)
}

func virtualCurrencyHoldIdempotencyKey(action, key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" || len([]rune(key)) > maxVirtualCurrencyHoldKeyRunes {
		return "", ErrVirtualCurrencyInvalidInput
	}
	result := "hold." + action + ":" + key
	if len([]rune(result)) > maxVirtualCurrencySourceIDRunes {
		return "", ErrVirtualCurrencyInvalidInput
	}
	return result, nil
}

func isVirtualCurrencyHoldStatus(value string) bool {
	switch value {
	case VirtualCurrencyHoldStatusActive, VirtualCurrencyHoldStatusCommitted, VirtualCurrencyHoldStatusReleased, VirtualCurrencyHoldStatusExpired:
		return true
	default:
		return false
	}
}

func dereferenceInt64(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func validateVirtualCurrencyMetadata(metadata map[string]any) error {
	encoded, err := json.Marshal(metadata)
	if err != nil || len(encoded) > maxVirtualCurrencyMetadataBytes {
		return ErrVirtualCurrencyInvalidInput
	}
	return nil
}
