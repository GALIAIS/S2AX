package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"

	"github.com/stretchr/testify/require"
)

type virtualCurrencyRepositoryStub struct {
	currency                *VirtualCurrency
	policies                []*VirtualCurrencyGroupPolicy
	wallets                 []*VirtualCurrencyWallet
	lastCode                string
	created                 VirtualCurrencyCreateInput
	updated                 VirtualCurrencyUpdateInput
	delta                   VirtualCurrencyDeltaInput
	reserved                VirtualCurrencyHoldReserveInput
	settled                 VirtualCurrencyHoldSettlementRepositoryInput
	createCalled            bool
	deltaCalled             bool
	enabledForAllCurrencyID int64
}

func (s *virtualCurrencyRepositoryStub) ListCurrencies(context.Context, bool) ([]*VirtualCurrency, error) {
	return []*VirtualCurrency{s.currency}, nil
}

func (s *virtualCurrencyRepositoryStub) GetCurrencyByID(context.Context, int64) (*VirtualCurrency, error) {
	return s.currency, nil
}

func (s *virtualCurrencyRepositoryStub) GetCurrencyByCode(_ context.Context, code string) (*VirtualCurrency, error) {
	s.lastCode = code
	return s.currency, nil
}

func (s *virtualCurrencyRepositoryStub) CreateCurrency(_ context.Context, input VirtualCurrencyCreateInput) (*VirtualCurrency, error) {
	s.created = input
	s.createCalled = true
	return s.currency, nil
}

func (s *virtualCurrencyRepositoryStub) UpdateCurrency(_ context.Context, _ int64, input VirtualCurrencyUpdateInput) (*VirtualCurrency, error) {
	s.updated = input
	return s.currency, nil
}

func (s *virtualCurrencyRepositoryStub) SetCurrencyStatus(context.Context, int64, string) (*VirtualCurrency, error) {
	return s.currency, nil
}

func (s *virtualCurrencyRepositoryStub) ListGroupPolicies(context.Context, int64) ([]*VirtualCurrencyGroupPolicy, error) {
	return s.policies, nil
}

func (s *virtualCurrencyRepositoryStub) UpsertGroupPolicy(context.Context, VirtualCurrencyPolicyInput) (*VirtualCurrencyGroupPolicy, error) {
	return &VirtualCurrencyGroupPolicy{}, nil
}

func (s *virtualCurrencyRepositoryStub) EnableForAllUsers(_ context.Context, currencyID int64) ([]*VirtualCurrencyGroupPolicy, error) {
	s.enabledForAllCurrencyID = currencyID
	return s.policies, nil
}

func (s *virtualCurrencyRepositoryStub) DeleteGroupPolicy(context.Context, int64, int64) error {
	return nil
}

func (s *virtualCurrencyRepositoryStub) ApplyCurrencyDelta(_ context.Context, input VirtualCurrencyDeltaInput) (*VirtualCurrencyLedgerEntry, error) {
	s.delta = input
	s.deltaCalled = true
	return &VirtualCurrencyLedgerEntry{CurrencyID: input.CurrencyID, DeltaUnits: input.DeltaUnits}, nil
}

func (s *virtualCurrencyRepositoryStub) ReserveHold(_ context.Context, input VirtualCurrencyHoldReserveInput) (*VirtualCurrencyHold, *VirtualCurrencyLedgerEntry, error) {
	s.reserved = input
	return &VirtualCurrencyHold{ID: 11, CurrencyID: input.CurrencyID, UserID: input.UserID, GroupID: int64Ptr(input.GroupID), AmountUnits: input.AmountUnits, ExpiresAt: input.ExpiresAt, Status: VirtualCurrencyHoldStatusActive}, &VirtualCurrencyLedgerEntry{CurrencyID: input.CurrencyID}, nil
}

func (s *virtualCurrencyRepositoryStub) CommitHold(_ context.Context, input VirtualCurrencyHoldSettlementRepositoryInput) (*VirtualCurrencyHold, *VirtualCurrencyLedgerEntry, error) {
	s.settled = input
	return &VirtualCurrencyHold{ID: input.HoldID, UserID: input.UserID, Status: VirtualCurrencyHoldStatusCommitted}, &VirtualCurrencyLedgerEntry{EntryType: VirtualCurrencyEntryCommit}, nil
}

func (s *virtualCurrencyRepositoryStub) ReleaseHold(_ context.Context, input VirtualCurrencyHoldSettlementRepositoryInput) (*VirtualCurrencyHold, *VirtualCurrencyLedgerEntry, error) {
	s.settled = input
	return &VirtualCurrencyHold{ID: input.HoldID, UserID: input.UserID, Status: VirtualCurrencyHoldStatusReleased}, &VirtualCurrencyLedgerEntry{EntryType: VirtualCurrencyEntryRelease}, nil
}

func (s *virtualCurrencyRepositoryStub) ExpireExpiredHolds(context.Context, int64, int) (int64, error) {
	return 0, nil
}

func (s *virtualCurrencyRepositoryStub) GetHold(_ context.Context, userID, holdID int64) (*VirtualCurrencyHold, error) {
	return &VirtualCurrencyHold{ID: holdID, CurrencyID: 7, UserID: userID, GroupID: int64Ptr(9), AmountUnits: 20, Status: VirtualCurrencyHoldStatusActive, ExpiresAt: time.Now().UTC().Add(time.Hour)}, nil
}

func (s *virtualCurrencyRepositoryStub) ListHolds(context.Context, VirtualCurrencyHoldQuery) ([]*VirtualCurrencyHold, *pagination.PaginationResult, error) {
	return nil, &pagination.PaginationResult{Page: 1, PageSize: 20}, nil
}

func (s *virtualCurrencyRepositoryStub) ListUserWallets(context.Context, int64) ([]*VirtualCurrencyWallet, error) {
	return s.wallets, nil
}

func (s *virtualCurrencyRepositoryStub) ListLedger(context.Context, VirtualCurrencyLedgerQuery) ([]*VirtualCurrencyLedgerEntry, *pagination.PaginationResult, error) {
	return nil, &pagination.PaginationResult{Page: 1, PageSize: 20}, nil
}

func (s *virtualCurrencyRepositoryStub) ReconcileCurrency(context.Context, int64, int) (*VirtualCurrencyReconciliationReport, error) {
	return &VirtualCurrencyReconciliationReport{}, nil
}

func TestVirtualCurrencyServiceCreateNormalizesInput(t *testing.T) {
	repo := &virtualCurrencyRepositoryStub{currency: &VirtualCurrency{ID: 1, Code: "gold"}}
	svc := NewVirtualCurrencyService(repo)

	_, err := svc.CreateCurrency(context.Background(), VirtualCurrencyCreateInput{
		Code:     " GOLD ",
		Name:     "金币",
		Metadata: map[string]any{"kind": "game"},
	})

	require.NoError(t, err)
	require.True(t, repo.createCalled)
	require.Equal(t, "gold", repo.created.Code)
	require.Equal(t, "金币", repo.created.Name)
}

func TestVirtualCurrencyServiceEnableForAllUsers(t *testing.T) {
	repo := &virtualCurrencyRepositoryStub{
		currency: &VirtualCurrency{ID: 7, Code: "gold", Status: VirtualCurrencyStatusActive},
		policies: []*VirtualCurrencyGroupPolicy{{CurrencyID: 7, GroupID: 9, Enabled: true, CanEarn: true, CanSpend: true}},
	}
	svc := NewVirtualCurrencyService(repo)

	policies, err := svc.EnableForAllUsers(context.Background(), 7)

	require.NoError(t, err)
	require.Equal(t, int64(7), repo.enabledForAllCurrencyID)
	require.Equal(t, repo.policies, policies)
}

func TestVirtualCurrencyServiceEnableForAllUsersRejectsDisabledCurrency(t *testing.T) {
	repo := &virtualCurrencyRepositoryStub{currency: &VirtualCurrency{ID: 7, Code: "gold", Status: VirtualCurrencyStatusDisabled}}
	svc := NewVirtualCurrencyService(repo)

	_, err := svc.EnableForAllUsers(context.Background(), 7)

	require.ErrorIs(t, err, ErrVirtualCurrencyDisabled)
	require.Zero(t, repo.enabledForAllCurrencyID)
}

func TestVirtualCurrencyServiceAdjustRequiresEarnPolicyForGrants(t *testing.T) {
	repo := &virtualCurrencyRepositoryStub{currency: &VirtualCurrency{
		ID:        7,
		Code:      "gold",
		Status:    VirtualCurrencyStatusActive,
		CreatedAt: time.Now(),
	}}
	svc := NewVirtualCurrencyService(repo)

	entry, err := svc.Adjust(context.Background(), VirtualCurrencyAdjustmentInput{
		CurrencyCode:   "GOLD",
		UserID:         42,
		GroupID:        9,
		AmountUnits:    100,
		SourceType:     VirtualCurrencySourceAdmin,
		IdempotencyKey: "grant-42",
		Reason:         "活动补偿",
	})

	require.NoError(t, err)
	require.NotNil(t, entry)
	require.True(t, repo.deltaCalled)
	require.Equal(t, int64(100), repo.delta.AvailableDeltaUnits)
	require.True(t, repo.delta.RequireCanEarn)
	require.True(t, repo.delta.RequireUserAccess)
	require.NotEmpty(t, repo.delta.RequestFingerprint)
}

func TestVirtualCurrencyServiceAdjustResolvesAccessibleAdminGroup(t *testing.T) {
	repo := &virtualCurrencyRepositoryStub{
		currency: &VirtualCurrency{ID: 7, Code: "gold", Status: VirtualCurrencyStatusActive},
		wallets:  []*VirtualCurrencyWallet{{CurrencyID: 7, GroupIDs: []int64{12, 9}}},
		policies: []*VirtualCurrencyGroupPolicy{
			{CurrencyID: 7, GroupID: 9, Enabled: true, CanEarn: false},
			{CurrencyID: 7, GroupID: 12, Enabled: true, CanEarn: true},
		},
	}
	svc := NewVirtualCurrencyService(repo)

	_, err := svc.Adjust(context.Background(), VirtualCurrencyAdjustmentInput{
		CurrencyCode:   "gold",
		UserID:         42,
		AmountUnits:    100,
		SourceType:     VirtualCurrencySourceAdmin,
		IdempotencyKey: "admin-deposit-42",
	})

	require.NoError(t, err)
	require.Equal(t, int64(12), repo.delta.GroupID)
	require.True(t, repo.delta.RequireCanEarn)

	_, err = svc.Adjust(context.Background(), VirtualCurrencyAdjustmentInput{
		CurrencyCode:   "gold",
		UserID:         42,
		AmountUnits:    -50,
		EntryType:      VirtualCurrencyEntryAdjustment,
		SourceType:     VirtualCurrencySourceAdmin,
		IdempotencyKey: "admin-refund-42",
	})

	require.NoError(t, err)
	require.Equal(t, int64(9), repo.delta.GroupID)
	require.False(t, repo.delta.RequireCanEarn)
}

func TestVirtualCurrencyServiceAdjustRejectsInvalidDirectLedgerSemantics(t *testing.T) {
	tests := []struct {
		name       string
		entryType  string
		amountUnit int64
	}{
		{name: "negative grant", entryType: VirtualCurrencyEntryGrant, amountUnit: -1},
		{name: "negative refund", entryType: VirtualCurrencyEntryRefund, amountUnit: -1},
		{name: "spend", entryType: VirtualCurrencyEntrySpend, amountUnit: -1},
		{name: "reserve", entryType: VirtualCurrencyEntryReserve, amountUnit: -1},
		{name: "commit", entryType: VirtualCurrencyEntryCommit, amountUnit: -1},
		{name: "release", entryType: VirtualCurrencyEntryRelease, amountUnit: 1},
		{name: "expire", entryType: VirtualCurrencyEntryExpire, amountUnit: 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := &virtualCurrencyRepositoryStub{currency: &VirtualCurrency{ID: 7, Code: "gold", Status: VirtualCurrencyStatusActive}}
			svc := NewVirtualCurrencyService(repo)

			_, err := svc.Adjust(context.Background(), VirtualCurrencyAdjustmentInput{
				CurrencyCode:   "gold",
				UserID:         42,
				GroupID:        9,
				AmountUnits:    test.amountUnit,
				EntryType:      test.entryType,
				SourceType:     VirtualCurrencySourceAdmin,
				IdempotencyKey: "admin-adjustment",
			})

			require.ErrorIs(t, err, ErrVirtualCurrencyInvalidInput)
			require.False(t, repo.deltaCalled)
		})
	}
}

func TestVirtualCurrencyServiceGrantUsesStableEarnContract(t *testing.T) {
	repo := &virtualCurrencyRepositoryStub{currency: &VirtualCurrency{
		ID:     7,
		Code:   "gold",
		Status: VirtualCurrencyStatusActive,
	}}
	svc := NewVirtualCurrencyService(repo)

	_, err := svc.Grant(context.Background(), VirtualCurrencyGrantInput{
		CurrencyCode:   " GOLD ",
		UserID:         42,
		GroupID:        9,
		AmountUnits:    25,
		SourceType:     "GAME",
		SourceID:       "quest-42",
		IdempotencyKey: "game-quest-42",
	})

	require.NoError(t, err)
	require.Equal(t, VirtualCurrencyEntryGrant, repo.delta.EntryType)
	require.Equal(t, VirtualCurrencySourceGame, repo.delta.SourceType)
	require.True(t, repo.delta.RequireCanEarn)
}

func TestVirtualCurrencyServiceGrantByIDResolvesCurrencyCode(t *testing.T) {
	repo := &virtualCurrencyRepositoryStub{currency: &VirtualCurrency{
		ID:     7,
		Code:   "gold",
		Status: VirtualCurrencyStatusActive,
	}}
	svc := NewVirtualCurrencyService(repo)

	entry, err := svc.GrantByID(context.Background(), 7, VirtualCurrencyGrantInput{
		UserID:            42,
		GroupID:           9,
		AmountUnits:       25,
		SourceType:        VirtualCurrencySourceRedeemCode,
		SourceID:          "REDEEM-001",
		IdempotencyKey:    "redeem-code:1",
		RequireUserAccess: true,
	})

	require.NoError(t, err)
	require.NotNil(t, entry)
	require.Equal(t, "gold", repo.lastCode)
	require.Equal(t, int64(7), repo.delta.CurrencyID)
	require.Equal(t, VirtualCurrencySourceRedeemCode, repo.delta.SourceType)
	require.True(t, repo.delta.RequireUserAccess)
}

func TestRedeemServicePreparesVirtualCurrencyCode(t *testing.T) {
	repo := &virtualCurrencyRepositoryStub{
		currency: &VirtualCurrency{ID: 7, Code: "gold", Status: VirtualCurrencyStatusActive},
		policies: []*VirtualCurrencyGroupPolicy{{GroupID: 9, Enabled: true, CanEarn: true}},
	}
	svc := &RedeemService{virtualCurrency: NewVirtualCurrencyService(repo)}
	amount := int64(125)
	groupID := int64(9)
	code := &RedeemCode{
		Code:                "REDEEM-001",
		Type:                RedeemTypeVirtualCurrency,
		Value:               99,
		CurrencyCode:        " GOLD ",
		CurrencyAmountUnits: &amount,
		CurrencyGroupID:     &groupID,
	}

	err := svc.prepareVirtualCurrencyCode(context.Background(), code)

	require.NoError(t, err)
	require.NotNil(t, code.CurrencyID)
	require.Equal(t, int64(7), *code.CurrencyID)
	require.Equal(t, "gold", code.CurrencyCode)
	require.Equal(t, float64(0), code.Value)
}

func TestVirtualCurrencyServiceSpendRejectsInvalidAmount(t *testing.T) {
	repo := &virtualCurrencyRepositoryStub{currency: &VirtualCurrency{ID: 1, Code: "gold", Status: VirtualCurrencyStatusActive}}
	svc := NewVirtualCurrencyService(repo)

	_, err := svc.Spend(context.Background(), VirtualCurrencySpendInput{
		CurrencyCode:   "gold",
		UserID:         1,
		GroupID:        1,
		AmountUnits:    0,
		SourceType:     VirtualCurrencySourceAPI,
		IdempotencyKey: "spend-1",
	})

	require.ErrorIs(t, err, ErrVirtualCurrencyInvalidInput)
	require.False(t, repo.deltaCalled)
}

func TestVirtualCurrencyServiceReserveNormalizesAndPrefixesIdempotency(t *testing.T) {
	repo := &virtualCurrencyRepositoryStub{currency: &VirtualCurrency{ID: 7, Code: "gold", Status: VirtualCurrencyStatusActive}}
	svc := NewVirtualCurrencyService(repo)

	result, err := svc.ReserveHold(context.Background(), VirtualCurrencyReserveInput{
		CurrencyCode:   " GOLD ",
		UserID:         42,
		GroupID:        9,
		AmountUnits:    20,
		SourceType:     "GAME",
		SourceID:       "order-42",
		IdempotencyKey: "reserve-42",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "hold.reserve:reserve-42", repo.reserved.IdempotencyKey)
	require.Equal(t, VirtualCurrencySourceGame, repo.reserved.SourceType)
	require.Equal(t, int64(20), repo.reserved.AmountUnits)
	require.True(t, repo.reserved.ExpiresAt.After(time.Now().UTC()))
}

func TestVirtualCurrencyServiceCommitHoldUsesSettlementContract(t *testing.T) {
	repo := &virtualCurrencyRepositoryStub{currency: &VirtualCurrency{ID: 7, Code: "gold", Status: VirtualCurrencyStatusActive}}
	svc := NewVirtualCurrencyService(repo)

	result, err := svc.CommitHold(context.Background(), VirtualCurrencyHoldSettlementInput{
		HoldID:         11,
		UserID:         42,
		SourceType:     VirtualCurrencySourceGame,
		SourceID:       "order-42",
		IdempotencyKey: "commit-42",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, VirtualCurrencyEntryCommit, repo.settled.Action)
	require.Equal(t, "hold.commit:commit-42", repo.settled.IdempotencyKey)
	require.NotEmpty(t, repo.settled.RequestFingerprint)
}

func TestVirtualCurrencyServiceFormatUnits(t *testing.T) {
	svc := NewVirtualCurrencyService(nil)
	currency := &VirtualCurrency{Scale: 2}

	require.Equal(t, "12.34", svc.FormatUnits(currency, 1234))
	require.Equal(t, "-0.05", svc.FormatUnits(currency, -5))
	require.Equal(t, "-92233720368547758.08", svc.FormatUnits(currency, -1<<63))
	require.Equal(t, "7", svc.FormatUnits(&VirtualCurrency{Scale: 0}, 7))
}
