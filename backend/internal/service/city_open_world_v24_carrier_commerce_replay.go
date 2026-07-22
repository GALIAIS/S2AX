package service

import "fmt"

// replayCityOpenWorldCarrierCommerceCheckpoint runs after V22 settlement and
// V23 recovery have reconstructed their immutable predecessor evidence. V24
// adds only a case quote and an affordable-payment successor; it never makes a
// delivery outcome, claim, or account balance itself the source of truth.
func replayCityOpenWorldCarrierCommerceCheckpoint(tick int64, state *cityHashState) error {
	if state == nil || !cityEngineSupportsOpenWorldCarrierCommerce(state.SimulationVersion) ||
		state.OpenWorldRuntime == nil || state.OpenWorldRuntime.FreightSettlements == nil ||
		state.OpenWorldRuntime.CarrierRecovery == nil || state.OpenWorldRuntime.CarrierCommerce == nil {
		return fmt.Errorf("open-world carrier-commerce replay state is unavailable")
	}
	if err := validateCityOpenWorldCarrierCommerceState(state.OpenWorldRuntime.CarrierCommerce); err != nil {
		return fmt.Errorf("invalid V24 carrier-commerce checkpoint: %w", err)
	}
	return validateCityOpenWorldCarrierCommerceCheckpoint(tick, state.OpenWorldRuntime)
}

func validateCityOpenWorldCarrierCommerceCheckpoint(
	tick int64,
	runtime *cityOpenWorldRuntimeHashState,
) error {
	if tick <= 0 || runtime == nil || runtime.FreightSettlements == nil ||
		runtime.CarrierRecovery == nil || runtime.CarrierCommerce == nil {
		return fmt.Errorf("V24 carrier-commerce checkpoint prerequisites are unavailable")
	}
	cases := make(map[string]CityOpenWorldFreightSettlementCase, len(runtime.FreightSettlements.Cases))
	for _, settlementCase := range runtime.FreightSettlements.Cases {
		cases[settlementCase.Code] = settlementCase
	}
	linesByCase := make(map[string][]CityOpenWorldFreightSettlementCaseLine, len(runtime.FreightSettlements.Lines))
	for _, line := range runtime.FreightSettlements.Lines {
		linesByCase[line.CaseCode] = append(linesByCase[line.CaseCode], line)
	}
	contracts := make(map[string]CityOpenWorldCarrierServiceContract, len(runtime.CarrierCommerce.Contracts))
	for _, contract := range runtime.CarrierCommerce.Contracts {
		settlementCase, found := cases[contract.CaseCode]
		if !found || contract.SourceTick <= runtime.CarrierCommerce.Policy.BaselineTick ||
			contract.SourceTick != settlementCase.SourceTick || contract.SourceKind != settlementCase.SourceKind ||
			contract.SourceCode != settlementCase.SourceCode || contract.ContractTick > tick {
			return fmt.Errorf("V24 carrier-service contract has inconsistent V22 case evidence")
		}
		lines := linesByCase[contract.CaseCode]
		if len(lines) == 0 {
			return fmt.Errorf("V24 carrier-service contract has no V22 case lines")
		}
		var cargoUnits int64
		for _, line := range lines {
			if line.SourceFirmCode != contract.SellerFirmCode {
				return fmt.Errorf("V24 carrier-service contract seller does not match V22 case source")
			}
			var err error
			cargoUnits, err = addCityLedgerUnits(cargoUnits, line.QuantityUnits)
			if err != nil {
				return err
			}
		}
		if cargoUnits != contract.CargoUnits {
			return fmt.Errorf("V24 carrier-service contract cargo quote differs from V22 case lines")
		}
		if _, exists := contracts[contract.Code]; exists {
			return fmt.Errorf("V24 carrier-service contract is duplicated")
		}
		contracts[contract.Code] = contract
	}
	for _, payment := range runtime.CarrierCommerce.Payments {
		contract, found := contracts[payment.ContractCode]
		settlementCase, caseFound := cases[payment.CaseCode]
		if !found || !caseFound || settlementCase.State != cityOpenWorldFreightSettlementCaseSettled ||
			payment.PaymentTick > tick || payment.PaymentTick <= contract.ContractTick || payment.Journal.Tick != payment.PaymentTick ||
			payment.CaseCode != contract.CaseCode || payment.SellerFirmCode != contract.SellerFirmCode ||
			payment.CarrierFirmCode != contract.CarrierFirmCode || payment.AmountUnits != contract.QuotedFeeUnits {
			return fmt.Errorf("V24 carrier-fee payment has inconsistent contract or settlement evidence")
		}
	}
	return nil
}
