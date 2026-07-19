package service

import (
	"context"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
)

// SetAccountAllocationService wires the policy layer after the three gateway
// services are constructed. Keeping this as a setter preserves the existing
// constructor contracts used by focused gateway tests.
func (s *GatewayService) SetAccountAllocationService(allocation *AccountAllocationService) {
	if s != nil {
		s.accountAllocationService = allocation
	}
}

func (s *OpenAIGatewayService) SetAccountAllocationService(allocation *AccountAllocationService) {
	if s != nil {
		s.accountAllocationService = allocation
	}
}

func (s *GeminiMessagesCompatService) SetAccountAllocationService(allocation *AccountAllocationService) {
	if s != nil {
		s.accountAllocationService = allocation
	}
}

func accountAllocationUserID(ctx context.Context) int64 {
	if ctx == nil {
		return 0
	}
	userID, _ := ctx.Value(ctxkey.UserID).(int64)
	return userID
}

func accountAllocationContextGroupID(ctx context.Context) *int64 {
	if ctx == nil {
		return nil
	}
	group, ok := ctx.Value(ctxkey.Group).(*Group)
	if !ok || !IsGroupContextValid(group) {
		return nil
	}
	groupID := group.ID
	return &groupID
}

func filterAccountAllocationCandidates(ctx context.Context, allocation *AccountAllocationService, groupID *int64, candidates []Account) ([]Account, error) {
	if allocation == nil || len(candidates) == 0 {
		return candidates, nil
	}
	userID := accountAllocationUserID(ctx)
	filtered, err := allocation.FilterCandidates(ctx, userID, groupID, candidates)
	if err != nil {
		return nil, fmt.Errorf("filter account allocation candidates: %w", err)
	}
	return filtered, nil
}

func canUseAllocatedAccount(ctx context.Context, allocation *AccountAllocationService, accountID int64) (bool, error) {
	if allocation == nil || accountID <= 0 {
		return true, nil
	}
	userID := accountAllocationUserID(ctx)
	groupID := accountAllocationContextGroupID(ctx)
	allowed, err := allocation.CanUseAccount(ctx, userID, groupID, accountID)
	if err != nil {
		return false, fmt.Errorf("check account allocation lease: %w", err)
	}
	return allowed, nil
}
