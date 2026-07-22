package service

import (
	"context"
	"fmt"
)

// GetCityOpenWorldCarrierCommerceState exposes V24's quote/payment evidence.
// Members see only contracts and payments where their firm is the seller; the
// shared carrier identity and world-wide money counters remain administrative.
func (s *CityEconomyService) GetCityOpenWorldCarrierCommerceState(
	ctx context.Context,
	userID, worldID int64,
) (*CityOpenWorldCarrierCommerceState, error) {
	if userID <= 0 || worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, userID, worldID); err != nil {
		return nil, err
	}
	var version string
	if err := s.db.QueryRowContext(ctx, `SELECT simulation_version FROM city_worlds WHERE id = $1`, worldID).Scan(&version); err != nil {
		return nil, fmt.Errorf("load V24 carrier-commerce world version: %w", err)
	}
	if !cityEngineSupportsOpenWorldCarrierCommerce(version) {
		return nil, ErrCitySimulationVersion.WithMetadata(map[string]string{"version": version})
	}
	state, err := loadCityOpenWorldCarrierCommerceState(ctx, s.db, worldID)
	if err != nil {
		return nil, err
	}
	fullRead, ownedFirmCodes, err := s.cityOpenWorldSupplyChainReadScope(ctx, userID, worldID)
	if err != nil {
		return nil, err
	}
	if fullRead {
		return state, nil
	}
	return projectCityOpenWorldCarrierCommerceStateForOwnedFirms(state, ownedFirmCodes), nil
}

func projectCityOpenWorldCarrierCommerceStateForOwnedFirms(
	state *CityOpenWorldCarrierCommerceState,
	ownedFirmCodes map[string]struct{},
) *CityOpenWorldCarrierCommerceState {
	if state == nil {
		return nil
	}
	view := &CityOpenWorldCarrierCommerceState{
		Policy:    state.Policy,
		Contracts: make([]CityOpenWorldCarrierServiceContract, 0),
		Payments:  make([]CityOpenWorldCarrierFeePayment, 0),
	}
	view.Policy.CarrierActorCode = ""
	view.Policy.CarrierFirmCode = ""
	view.Policy.ContractCount = 0
	view.Policy.PaymentCount = 0
	view.Policy.QuotedCargoUnits = 0
	view.Policy.PaidCargoUnits = 0
	view.Policy.PaidAmountUnits = 0
	view.Policy.Revision = 0
	for _, contract := range state.Contracts {
		if _, owned := ownedFirmCodes[contract.SellerFirmCode]; !owned {
			continue
		}
		contract.CarrierActorCode = ""
		contract.CarrierFirmCode = ""
		view.Contracts = append(view.Contracts, contract)
	}
	for _, payment := range state.Payments {
		if _, owned := ownedFirmCodes[payment.SellerFirmCode]; !owned {
			continue
		}
		payment.CarrierFirmCode = ""
		view.Payments = append(view.Payments, payment)
	}
	return view
}
