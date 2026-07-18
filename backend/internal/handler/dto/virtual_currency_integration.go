package dto

import (
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type VirtualCurrencyIntegration struct {
	ID         int64          `json:"id"`
	Code       string         `json:"code"`
	Name       string         `json:"name"`
	SecretHint string         `json:"secret_hint"`
	Status     string         `json:"status"`
	Metadata   map[string]any `json:"metadata"`
	CreatedBy  *int64         `json:"created_by,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
}

type VirtualCurrencyIntegrationSecretResult struct {
	Integration *VirtualCurrencyIntegration `json:"integration"`
	Secret      string                      `json:"secret"`
}

type VirtualCurrencyIntegrationScope struct {
	ID            int64          `json:"id"`
	IntegrationID int64          `json:"integration_id"`
	CurrencyID    int64          `json:"currency_id"`
	GroupID       int64          `json:"group_id"`
	Enabled       bool           `json:"enabled"`
	CanEarn       bool           `json:"can_earn"`
	CanSpend      bool           `json:"can_spend"`
	CanSettle     bool           `json:"can_settle"`
	Metadata      map[string]any `json:"metadata"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}

type VirtualCurrencyIntegrationMutationResult struct {
	Operation string                      `json:"operation"`
	Ledger    *VirtualCurrencyLedgerEntry `json:"ledger,omitempty"`
	Hold      *VirtualCurrencyHold        `json:"hold,omitempty"`
}

func VirtualCurrencyIntegrationFromService(item *service.VirtualCurrencyIntegration) *VirtualCurrencyIntegration {
	if item == nil {
		return nil
	}
	return &VirtualCurrencyIntegration{
		ID: item.ID, Code: item.Code, Name: item.Name, SecretHint: item.SecretHint, Status: item.Status,
		Metadata: item.Metadata, CreatedBy: item.CreatedBy, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
	}
}

func VirtualCurrencyIntegrationsFromService(items []*service.VirtualCurrencyIntegration) []*VirtualCurrencyIntegration {
	out := make([]*VirtualCurrencyIntegration, 0, len(items))
	for _, item := range items {
		if mapped := VirtualCurrencyIntegrationFromService(item); mapped != nil {
			out = append(out, mapped)
		}
	}
	return out
}

func VirtualCurrencyIntegrationSecretFromService(item *service.VirtualCurrencyIntegrationSecretResult) *VirtualCurrencyIntegrationSecretResult {
	if item == nil {
		return nil
	}
	return &VirtualCurrencyIntegrationSecretResult{Integration: VirtualCurrencyIntegrationFromService(item.Integration), Secret: item.Secret}
}

func VirtualCurrencyIntegrationScopeFromService(item *service.VirtualCurrencyIntegrationScope) *VirtualCurrencyIntegrationScope {
	if item == nil {
		return nil
	}
	return &VirtualCurrencyIntegrationScope{
		ID: item.ID, IntegrationID: item.IntegrationID, CurrencyID: item.CurrencyID, GroupID: item.GroupID,
		Enabled: item.Enabled, CanEarn: item.CanEarn, CanSpend: item.CanSpend, CanSettle: item.CanSettle,
		Metadata: item.Metadata, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
	}
}

func VirtualCurrencyIntegrationScopesFromService(items []*service.VirtualCurrencyIntegrationScope) []*VirtualCurrencyIntegrationScope {
	out := make([]*VirtualCurrencyIntegrationScope, 0, len(items))
	for _, item := range items {
		if mapped := VirtualCurrencyIntegrationScopeFromService(item); mapped != nil {
			out = append(out, mapped)
		}
	}
	return out
}

func VirtualCurrencyIntegrationMutationResultFromService(item *service.VirtualCurrencyIntegrationMutationResult) *VirtualCurrencyIntegrationMutationResult {
	if item == nil {
		return nil
	}
	return &VirtualCurrencyIntegrationMutationResult{
		Operation: item.Operation,
		Ledger:    VirtualCurrencyLedgerFromService(item.Ledger),
		Hold:      VirtualCurrencyHoldFromService(item.Hold),
	}
}
