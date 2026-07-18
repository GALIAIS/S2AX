package dto

import (
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// VirtualCurrency is the public representation of a configurable non-fiat asset.
type VirtualCurrency struct {
	ID          int64          `json:"id"`
	Code        string         `json:"code"`
	Name        string         `json:"name"`
	Symbol      string         `json:"symbol"`
	Description string         `json:"description"`
	Scale       int            `json:"scale"`
	Status      string         `json:"status"`
	Metadata    map[string]any `json:"metadata"`
	CreatedBy   *int64         `json:"created_by,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

type VirtualCurrencyGroupPolicy struct {
	ID              int64          `json:"id"`
	CurrencyID      int64          `json:"currency_id"`
	GroupID         int64          `json:"group_id"`
	Enabled         bool           `json:"enabled"`
	CanEarn         bool           `json:"can_earn"`
	CanSpend        bool           `json:"can_spend"`
	MaxBalanceUnits *int64         `json:"max_balance_units,omitempty"`
	Metadata        map[string]any `json:"metadata"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

type VirtualCurrencyWallet struct {
	CurrencyID     int64     `json:"currency_id"`
	CurrencyCode   string    `json:"currency_code"`
	CurrencyName   string    `json:"currency_name"`
	CurrencySymbol string    `json:"currency_symbol"`
	CurrencyScale  int       `json:"currency_scale"`
	AvailableUnits int64     `json:"available_units"`
	ReservedUnits  int64     `json:"reserved_units"`
	GroupIDs       []int64   `json:"group_ids"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// VirtualCurrencyLedgerEntry intentionally omits the internal request fingerprint.
type VirtualCurrencyLedgerEntry struct {
	ID                  int64          `json:"id"`
	JournalID           int64          `json:"journal_id"`
	CurrencyID          int64          `json:"currency_id"`
	CurrencyCode        string         `json:"currency_code"`
	CurrencyName        string         `json:"currency_name"`
	CurrencySymbol      string         `json:"currency_symbol"`
	CurrencyScale       int            `json:"currency_scale"`
	UserID              int64          `json:"user_id"`
	GroupID             *int64         `json:"group_id,omitempty"`
	DeltaUnits          int64          `json:"delta_units"`
	AvailableDeltaUnits int64          `json:"available_delta_units"`
	ReservedDeltaUnits  int64          `json:"reserved_delta_units"`
	AvailableAfterUnits int64          `json:"available_after_units"`
	ReservedAfterUnits  int64          `json:"reserved_after_units"`
	EntryType           string         `json:"entry_type"`
	SourceType          string         `json:"source_type"`
	SourceID            *string        `json:"source_id,omitempty"`
	IdempotencyKey      string         `json:"idempotency_key"`
	Reason              string         `json:"reason"`
	Metadata            map[string]any `json:"metadata"`
	CreatedBy           *int64         `json:"created_by,omitempty"`
	CreatedAt           time.Time      `json:"created_at"`
}

type VirtualCurrencyHold struct {
	ID             int64          `json:"id"`
	CurrencyID     int64          `json:"currency_id"`
	CurrencyCode   string         `json:"currency_code"`
	CurrencyName   string         `json:"currency_name"`
	CurrencySymbol string         `json:"currency_symbol"`
	CurrencyScale  int            `json:"currency_scale"`
	UserID         int64          `json:"user_id"`
	GroupID        *int64         `json:"group_id,omitempty"`
	AmountUnits    int64          `json:"amount_units"`
	Status         string         `json:"status"`
	SourceType     string         `json:"source_type"`
	SourceID       *string        `json:"source_id,omitempty"`
	IdempotencyKey string         `json:"idempotency_key"`
	ExpiresAt      time.Time      `json:"expires_at"`
	Metadata       map[string]any `json:"metadata"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
	SettledAt      *time.Time     `json:"settled_at,omitempty"`
}

type VirtualCurrencyHoldResult struct {
	Hold   *VirtualCurrencyHold        `json:"hold"`
	Ledger *VirtualCurrencyLedgerEntry `json:"ledger"`
}

func VirtualCurrencyFromService(item *service.VirtualCurrency) *VirtualCurrency {
	if item == nil {
		return nil
	}
	return &VirtualCurrency{
		ID:          item.ID,
		Code:        item.Code,
		Name:        item.Name,
		Symbol:      item.Symbol,
		Description: item.Description,
		Scale:       item.Scale,
		Status:      item.Status,
		Metadata:    item.Metadata,
		CreatedBy:   item.CreatedBy,
		CreatedAt:   item.CreatedAt,
		UpdatedAt:   item.UpdatedAt,
	}
}

func VirtualCurrencyPoliciesFromService(items []*service.VirtualCurrencyGroupPolicy) []*VirtualCurrencyGroupPolicy {
	out := make([]*VirtualCurrencyGroupPolicy, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		out = append(out, &VirtualCurrencyGroupPolicy{
			ID:              item.ID,
			CurrencyID:      item.CurrencyID,
			GroupID:         item.GroupID,
			Enabled:         item.Enabled,
			CanEarn:         item.CanEarn,
			CanSpend:        item.CanSpend,
			MaxBalanceUnits: item.MaxBalanceUnits,
			Metadata:        item.Metadata,
			CreatedAt:       item.CreatedAt,
			UpdatedAt:       item.UpdatedAt,
		})
	}
	return out
}

func VirtualCurrencyWalletsFromService(items []*service.VirtualCurrencyWallet) []*VirtualCurrencyWallet {
	out := make([]*VirtualCurrencyWallet, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		groupIDs := item.GroupIDs
		if groupIDs == nil {
			groupIDs = []int64{}
		}
		out = append(out, &VirtualCurrencyWallet{
			CurrencyID:     item.CurrencyID,
			CurrencyCode:   item.CurrencyCode,
			CurrencyName:   item.CurrencyName,
			CurrencySymbol: item.CurrencySymbol,
			CurrencyScale:  item.CurrencyScale,
			AvailableUnits: item.AvailableUnits,
			ReservedUnits:  item.ReservedUnits,
			GroupIDs:       groupIDs,
			UpdatedAt:      item.UpdatedAt,
		})
	}
	return out
}

func VirtualCurrencyLedgerFromService(item *service.VirtualCurrencyLedgerEntry) *VirtualCurrencyLedgerEntry {
	if item == nil {
		return nil
	}
	return &VirtualCurrencyLedgerEntry{
		ID:                  item.ID,
		JournalID:           item.JournalID,
		CurrencyID:          item.CurrencyID,
		CurrencyCode:        item.CurrencyCode,
		CurrencyName:        item.CurrencyName,
		CurrencySymbol:      item.CurrencySymbol,
		CurrencyScale:       item.CurrencyScale,
		UserID:              item.UserID,
		GroupID:             item.GroupID,
		DeltaUnits:          item.DeltaUnits,
		AvailableDeltaUnits: item.AvailableDeltaUnits,
		ReservedDeltaUnits:  item.ReservedDeltaUnits,
		AvailableAfterUnits: item.AvailableAfterUnits,
		ReservedAfterUnits:  item.ReservedAfterUnits,
		EntryType:           item.EntryType,
		SourceType:          item.SourceType,
		SourceID:            item.SourceID,
		IdempotencyKey:      item.IdempotencyKey,
		Reason:              item.Reason,
		Metadata:            item.Metadata,
		CreatedBy:           item.CreatedBy,
		CreatedAt:           item.CreatedAt,
	}
}

func VirtualCurrencyLedgersFromService(items []*service.VirtualCurrencyLedgerEntry) []*VirtualCurrencyLedgerEntry {
	out := make([]*VirtualCurrencyLedgerEntry, 0, len(items))
	for _, item := range items {
		if mapped := VirtualCurrencyLedgerFromService(item); mapped != nil {
			out = append(out, mapped)
		}
	}
	return out
}

func VirtualCurrencyHoldFromService(item *service.VirtualCurrencyHold) *VirtualCurrencyHold {
	if item == nil {
		return nil
	}
	return &VirtualCurrencyHold{
		ID:             item.ID,
		CurrencyID:     item.CurrencyID,
		CurrencyCode:   item.CurrencyCode,
		CurrencyName:   item.CurrencyName,
		CurrencySymbol: item.CurrencySymbol,
		CurrencyScale:  item.CurrencyScale,
		UserID:         item.UserID,
		GroupID:        item.GroupID,
		AmountUnits:    item.AmountUnits,
		Status:         item.Status,
		SourceType:     item.SourceType,
		SourceID:       item.SourceID,
		IdempotencyKey: item.IdempotencyKey,
		ExpiresAt:      item.ExpiresAt,
		Metadata:       item.Metadata,
		CreatedAt:      item.CreatedAt,
		UpdatedAt:      item.UpdatedAt,
		SettledAt:      item.SettledAt,
	}
}

func VirtualCurrencyHoldsFromService(items []*service.VirtualCurrencyHold) []*VirtualCurrencyHold {
	out := make([]*VirtualCurrencyHold, 0, len(items))
	for _, item := range items {
		if mapped := VirtualCurrencyHoldFromService(item); mapped != nil {
			out = append(out, mapped)
		}
	}
	return out
}

func VirtualCurrencyHoldResultFromService(item *service.VirtualCurrencyHoldResult) *VirtualCurrencyHoldResult {
	if item == nil {
		return nil
	}
	return &VirtualCurrencyHoldResult{
		Hold:   VirtualCurrencyHoldFromService(item.Hold),
		Ledger: VirtualCurrencyLedgerFromService(item.Ledger),
	}
}
