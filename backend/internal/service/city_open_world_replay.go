package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
)

// replayCityOpenWorldCheckpoint replays the V2+ open-world projection from
// the sealed per-tick checkpoint after independently proving the append-only
// runtime evidence written during the tick.  Open-world static generation and
// runtime projections predate the generic F7 row reducers: generation records
// immutable hashes, while actor/service/impact reducers publish facts and
// effects into their own ledger.  The checkpoint is therefore a historical
// input (never a live projection), and the checks below make a changed row,
// missing fact, or altered effect fail replay before it can be installed.
//
// This keeps old V2-V8 snapshots replayable without attempting to reinterpret
// a historical generator/content contract using the currently deployed
// generator.  It is deliberately separate from the F7 reducers so neither
// model can silently consume the other's state tables.
func replayCityOpenWorldCheckpoint(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, tick int64,
	state *cityHashState,
) error {
	if state == nil || !cityEngineSupportsOpenWorld(state.SimulationVersion) || state.OpenWorld == nil {
		return fmt.Errorf("open-world replay state is unavailable")
	}

	snapshot, err := loadCitySnapshotByTick(ctx, queryer, worldID, tick)
	if err != nil {
		return fmt.Errorf("load open-world replay checkpoint: %w", err)
	}
	checkpoint, _, err := verifyCitySnapshot(snapshot)
	if err != nil {
		return fmt.Errorf("verify open-world replay checkpoint: %w", err)
	}
	if checkpoint.SimulationVersion != state.SimulationVersion || checkpoint.CurrentTick != tick ||
		checkpoint.OpenWorld == nil {
		return fmt.Errorf("open-world replay checkpoint is inconsistent")
	}
	if err = validateCityOpenWorldStaticCheckpoint(state.OpenWorld, checkpoint.OpenWorld); err != nil {
		return err
	}

	if cityEngineSupportsOpenWorldRuntime(state.SimulationVersion) {
		if state.OpenWorldRuntime == nil || checkpoint.OpenWorldRuntime == nil {
			return fmt.Errorf("open-world runtime replay checkpoint is unavailable")
		}
		if err = validateCityOpenWorldRuntimeCheckpoint(
			ctx, queryer, worldID, tick, state.SimulationVersion,
			state.OpenWorldRuntime, checkpoint.OpenWorldRuntime,
		); err != nil {
			return err
		}
		if cityEngineSupportsOpenWorldMobility(state.SimulationVersion) {
			if !cityOpenWorldMobilityStaticCheckpointEqual(
				state.OpenWorldRuntime.Mobility, checkpoint.OpenWorldRuntime.Mobility,
			) {
				return fmt.Errorf("open-world mobility topology changed during replay")
			}
		}
		if cityEngineSupportsOpenWorldArrivalBridge(state.SimulationVersion) {
			if !cityOpenWorldMobilityArrivalStaticCheckpointEqual(
				state.OpenWorldRuntime.Arrivals, checkpoint.OpenWorldRuntime.Arrivals,
			) {
				return fmt.Errorf("open-world mobility arrival policy changed during replay")
			}
		}
		if cityEngineSupportsOpenWorldMobilityOD(state.SimulationVersion) {
			if !cityOpenWorldMobilityODStaticCheckpointEqual(
				state.OpenWorldRuntime.OD, checkpoint.OpenWorldRuntime.OD,
			) {
				return fmt.Errorf("open-world mobility OD policy changed during replay")
			}
		}
		if cityEngineSupportsOpenWorldCommuteBindings(state.SimulationVersion) {
			if !cityOpenWorldCommuteStaticCheckpointEqual(
				state.OpenWorldRuntime.Commutes, checkpoint.OpenWorldRuntime.Commutes,
			) {
				return fmt.Errorf("open-world commute binding foundation changed during replay")
			}
		}
		if cityEngineSupportsOpenWorldCommuteSources(state.SimulationVersion) {
			if !cityOpenWorldCommuteSourceStaticCheckpointEqual(
				state.OpenWorldRuntime.CommuteSources, checkpoint.OpenWorldRuntime.CommuteSources,
			) {
				return fmt.Errorf("open-world commute source policy changed during replay")
			}
		}
		if cityEngineSupportsOpenWorldSupplyChain(state.SimulationVersion) {
			if !cityOpenWorldSupplyChainStaticCheckpointEqual(
				state.OpenWorldRuntime.SupplyChain, checkpoint.OpenWorldRuntime.SupplyChain,
			) {
				return fmt.Errorf("open-world supply-chain foundation changed during replay")
			}
			if err = replayCityOpenWorldSupplyChainInventoryTopology(
				tick, state, checkpoint.OpenWorldRuntime.SupplyChain,
			); err != nil {
				return err
			}
		}
		if cityEngineSupportsOpenWorldEnterpriseFreight(state.SimulationVersion) {
			if !cityOpenWorldEnterpriseFreightStaticCheckpointEqual(
				state.OpenWorldRuntime.EnterpriseFreight, checkpoint.OpenWorldRuntime.EnterpriseFreight,
			) {
				return fmt.Errorf("open-world enterprise-freight foundation changed during replay")
			}
		}
		if cityEngineSupportsOpenWorldEnterpriseFreightReceipts(state.SimulationVersion) {
			if !cityOpenWorldEnterpriseFreightReceiptStaticCheckpointEqual(
				state.OpenWorldRuntime.EnterpriseFreightReceipts,
				checkpoint.OpenWorldRuntime.EnterpriseFreightReceipts,
			) {
				return fmt.Errorf("open-world enterprise-freight receipt foundation changed during replay")
			}
		}
		if cityEngineSupportsOpenWorldFreightBatches(state.SimulationVersion) {
			if !cityOpenWorldFreightBatchStaticCheckpointEqual(
				state.OpenWorldRuntime.EnterpriseFreightBatches,
				checkpoint.OpenWorldRuntime.EnterpriseFreightBatches,
			) {
				return fmt.Errorf("open-world freight-batch foundation changed during replay")
			}
		}
		if cityEngineSupportsOpenWorldSpatialNetwork(state.SimulationVersion) {
			if !cityOpenWorldSpatialNetworkStaticCheckpointEqual(
				state.OpenWorldRuntime.SpatialNetwork,
				checkpoint.OpenWorldRuntime.SpatialNetwork,
			) {
				return fmt.Errorf("open-world spatial-network foundation changed during replay")
			}
		}
		if cityEngineSupportsOpenWorldInfrastructure(state.SimulationVersion) {
			if !cityOpenWorldInfrastructureStaticCheckpointEqual(
				state.OpenWorldRuntime.Infrastructure,
				checkpoint.OpenWorldRuntime.Infrastructure,
			) {
				return fmt.Errorf("open-world infrastructure foundation changed during replay")
			}
		}
		if cityEngineSupportsOpenWorldEffectiveCapacity(state.SimulationVersion) {
			if !cityOpenWorldEffectiveCapacityStaticCheckpointEqual(
				state.OpenWorldRuntime.EffectiveCapacity,
				checkpoint.OpenWorldRuntime.EffectiveCapacity,
			) {
				return fmt.Errorf("open-world effective-capacity policy changed during replay")
			}
		}
		// V14 intentionally permits fact-backed successor epochs in a normal
		// tick, so unlike V12/V13 it has no blanket static equality check here.
		// Its dedicated stage proves each effective-assignment change against the
		// runtime fact ledger after this checkpoint becomes the replay state.
	}
	if cityEngineSupportsWorldVersionVector(state.SimulationVersion) {
		if state.VersionVector == nil || checkpoint.VersionVector == nil ||
			!reflect.DeepEqual(*state.VersionVector, *checkpoint.VersionVector) {
			return fmt.Errorf("open-world version vector changed during a normal tick")
		}
	}

	state.OpenWorld = checkpoint.OpenWorld
	state.OpenWorldRuntime = checkpoint.OpenWorldRuntime
	state.VersionVector = checkpoint.VersionVector
	return nil
}

func replayCityOpenWorldServiceCheckpoint(tick int64, state *cityHashState) error {
	if state == nil || !cityEngineSupportsOpenWorldServiceCoordination(state.SimulationVersion) ||
		state.OpenWorldRuntime == nil || state.OpenWorldRuntime.Services == nil {
		return fmt.Errorf("open-world service replay state is unavailable")
	}
	return validateCityOpenWorldServiceCheckpoint(tick, state.OpenWorldRuntime)
}

func replayCityOpenWorldImpactCheckpoint(tick int64, state *cityHashState) error {
	if state == nil || !cityEngineSupportsOpenWorldImpactBridge(state.SimulationVersion) ||
		state.OpenWorldRuntime == nil || state.OpenWorldRuntime.Impacts == nil {
		return fmt.Errorf("open-world impact replay state is unavailable")
	}
	if err := validateCityOpenWorldImpactState(state.OpenWorldRuntime.Impacts); err != nil {
		return fmt.Errorf("invalid V8 impact checkpoint: %w", err)
	}
	return validateCityOpenWorldImpactCheckpoint(tick, state.OpenWorldRuntime)
}

func replayCityOpenWorldMobilityCheckpoint(tick int64, state *cityHashState) error {
	if state == nil || !cityEngineSupportsOpenWorldMobility(state.SimulationVersion) ||
		state.OpenWorldRuntime == nil || state.OpenWorldRuntime.Mobility == nil {
		return fmt.Errorf("open-world mobility replay state is unavailable")
	}
	if err := validateCityOpenWorldMobilityState(state.OpenWorldRuntime.Mobility); err != nil {
		return fmt.Errorf("invalid V9 mobility checkpoint: %w", err)
	}
	return validateCityOpenWorldMobilityCheckpoint(tick, state.OpenWorldRuntime)
}

func replayCityOpenWorldMobilityArrivalCheckpoint(tick int64, state *cityHashState) error {
	if state == nil || !cityEngineSupportsOpenWorldArrivalBridge(state.SimulationVersion) ||
		state.OpenWorldRuntime == nil || state.OpenWorldRuntime.Arrivals == nil {
		return fmt.Errorf("open-world mobility arrival replay state is unavailable")
	}
	return validateCityOpenWorldMobilityArrivalCheckpoint(tick, state.OpenWorldRuntime)
}

func replayCityOpenWorldMobilityODCheckpoint(tick int64, state *cityHashState) error {
	if state == nil || !cityEngineSupportsOpenWorldMobilityOD(state.SimulationVersion) ||
		state.OpenWorldRuntime == nil || state.OpenWorldRuntime.OD == nil {
		return fmt.Errorf("open-world mobility OD replay state is unavailable")
	}
	return validateCityOpenWorldMobilityODCheckpoint(tick, state.OpenWorldRuntime)
}

func replayCityOpenWorldCommuteSourceCheckpoint(tick int64, state *cityHashState) error {
	if state == nil || !cityEngineSupportsOpenWorldCommuteSources(state.SimulationVersion) ||
		state.OpenWorldRuntime == nil || state.OpenWorldRuntime.CommuteSources == nil {
		return fmt.Errorf("open-world commute source replay state is unavailable")
	}
	return validateCityOpenWorldCommuteSourceCheckpoint(tick, state.OpenWorldRuntime)
}

func replayCityOpenWorldCommuteLifecycleCheckpoint(tick int64, state *cityHashState) error {
	if state == nil || !cityEngineSupportsOpenWorldCommuteLifecycle(state.SimulationVersion) ||
		state.OpenWorldRuntime == nil || state.OpenWorldRuntime.CommuteLifecycle == nil {
		return fmt.Errorf("open-world commute lifecycle replay state is unavailable")
	}
	return validateCityOpenWorldCommuteLifecycleCheckpoint(tick, state.OpenWorldRuntime)
}

func replayCityOpenWorldSupplyChainCheckpoint(tick int64, state *cityHashState) error {
	if state == nil || !cityEngineSupportsOpenWorldSupplyChain(state.SimulationVersion) ||
		state.OpenWorldRuntime == nil || state.OpenWorldRuntime.SupplyChain == nil {
		return fmt.Errorf("open-world supply-chain replay state is unavailable")
	}
	return validateCityOpenWorldSupplyChainCheckpoint(tick, state.OpenWorldRuntime)
}

func replayCityOpenWorldEnterpriseFreightCheckpoint(tick int64, state *cityHashState) error {
	if state == nil || !cityEngineSupportsOpenWorldEnterpriseFreight(state.SimulationVersion) ||
		state.OpenWorldRuntime == nil || state.OpenWorldRuntime.EnterpriseFreight == nil {
		return fmt.Errorf("open-world enterprise-freight replay state is unavailable")
	}
	return validateCityOpenWorldEnterpriseFreightCheckpoint(tick, state.OpenWorldRuntime)
}

func replayCityOpenWorldEnterpriseFreightReceiptCheckpoint(tick int64, state *cityHashState) error {
	if state == nil || !cityEngineSupportsOpenWorldEnterpriseFreightReceipts(state.SimulationVersion) ||
		state.OpenWorldRuntime == nil || state.OpenWorldRuntime.EnterpriseFreightReceipts == nil {
		return fmt.Errorf("open-world enterprise-freight receipt replay state is unavailable")
	}
	if err := validateCityOpenWorldEnterpriseFreightReceiptState(state.OpenWorldRuntime.EnterpriseFreightReceipts); err != nil {
		return fmt.Errorf("invalid V17 enterprise-freight receipt checkpoint: %w", err)
	}
	return nil
}

func replayCityOpenWorldFreightBatchCheckpoint(tick int64, state *cityHashState) error {
	if state == nil || !cityEngineSupportsOpenWorldFreightBatches(state.SimulationVersion) ||
		state.OpenWorldRuntime == nil || state.OpenWorldRuntime.EnterpriseFreightBatches == nil {
		return fmt.Errorf("open-world freight-batch replay state is unavailable")
	}
	if err := validateCityOpenWorldFreightBatchState(state.OpenWorldRuntime.EnterpriseFreightBatches); err != nil {
		return fmt.Errorf("invalid V18 freight-batch checkpoint: %w", err)
	}
	return nil
}

// replayCityOpenWorldFreightSettlementCheckpoint validates the successor
// receipt overlay only after V15, V17, and V18 have all replaced their sealed
// checkpoint state. It therefore proves the partial settlement links without
// reinterpreting historic atomic-delivery evidence or inferring outcomes from
// current inventory balances.
func replayCityOpenWorldFreightSettlementCheckpoint(tick int64, state *cityHashState) error {
	if state == nil || !cityEngineSupportsOpenWorldFreightSettlements(state.SimulationVersion) ||
		state.OpenWorldRuntime == nil || state.OpenWorldRuntime.FreightSettlements == nil {
		return fmt.Errorf("open-world freight-settlement replay state is unavailable")
	}
	if err := validateCityOpenWorldFreightSettlementState(state.OpenWorldRuntime.FreightSettlements); err != nil {
		return fmt.Errorf("invalid V22 freight-settlement checkpoint: %w", err)
	}
	return validateCityOpenWorldFreightSettlementCheckpoint(tick, state.OpenWorldRuntime)
}

// replayCityOpenWorldCarrierRecoveryCheckpoint validates V23 only after the
// V22 settlement checkpoint has restored its immutable claims. A carrier
// claim remains V22 evidence; V23 contributes the one-to-one successor
// recovery, backed by a same-tick journal and the original seller identity.
func replayCityOpenWorldCarrierRecoveryCheckpoint(tick int64, state *cityHashState) error {
	if state == nil || !cityEngineSupportsOpenWorldCarrierRecovery(state.SimulationVersion) ||
		state.OpenWorldRuntime == nil || state.OpenWorldRuntime.CarrierRecovery == nil {
		return fmt.Errorf("open-world carrier-recovery replay state is unavailable")
	}
	if err := validateCityOpenWorldCarrierRecoveryState(state.OpenWorldRuntime.CarrierRecovery); err != nil {
		return fmt.Errorf("invalid V23 carrier-recovery checkpoint: %w", err)
	}
	return validateCityOpenWorldCarrierRecoveryCheckpoint(tick, state.OpenWorldRuntime)
}

func validateCityOpenWorldCarrierRecoveryCheckpoint(
	tick int64,
	runtime *cityOpenWorldRuntimeHashState,
) error {
	if tick <= 0 || runtime == nil || runtime.CarrierRecovery == nil || runtime.FreightSettlements == nil {
		return fmt.Errorf("V23 carrier-recovery checkpoint prerequisites are unavailable")
	}
	claims := make(map[string]CityOpenWorldFreightSettlementClaim, len(runtime.FreightSettlements.Claims))
	for _, claim := range runtime.FreightSettlements.Claims {
		claims[claim.Code] = claim
	}
	linesByCase := make(map[string][]CityOpenWorldFreightSettlementCaseLine, len(runtime.FreightSettlements.Lines))
	for _, line := range runtime.FreightSettlements.Lines {
		linesByCase[line.CaseCode] = append(linesByCase[line.CaseCode], line)
	}
	recoveredClaims := make(map[string]struct{}, len(runtime.CarrierRecovery.Recoveries))
	for _, recovery := range runtime.CarrierRecovery.Recoveries {
		claim, found := claims[recovery.ClaimCode]
		if !found || claim.LiabilityParty != cityOpenWorldFreightSettlementLiabilityCarrier ||
			claim.State != cityOpenWorldFreightSettlementClaimResolved ||
			claim.CaseCode != recovery.CaseCode || claim.ClaimAmount != recovery.AmountUnits ||
			claim.CreatedTick > recovery.RecoveryTick || recovery.RecoveryTick > tick ||
			recovery.Journal.Tick != recovery.RecoveryTick || recovery.Journal.Tick > tick {
			return fmt.Errorf("V23 carrier recovery has inconsistent V22 claim evidence")
		}
		caseLines := linesByCase[claim.CaseCode]
		if len(caseLines) == 0 {
			return fmt.Errorf("V23 carrier recovery claim has no settlement lines")
		}
		for _, line := range caseLines {
			if line.SourceFirmCode != recovery.SellerFirmCode {
				return fmt.Errorf("V23 carrier recovery seller does not match settlement source")
			}
		}
		if _, duplicate := recoveredClaims[recovery.ClaimCode]; duplicate {
			return fmt.Errorf("V23 carrier recovery claim is duplicated")
		}
		recoveredClaims[recovery.ClaimCode] = struct{}{}
	}
	for _, claim := range runtime.FreightSettlements.Claims {
		if claim.LiabilityParty != cityOpenWorldFreightSettlementLiabilityCarrier {
			continue
		}
		_, recovered := recoveredClaims[claim.Code]
		if claim.State == cityOpenWorldFreightSettlementClaimResolved && !recovered {
			return fmt.Errorf("V23 resolved carrier claim lacks recovery evidence")
		}
		if claim.State == cityOpenWorldFreightSettlementClaimOpen && recovered {
			return fmt.Errorf("V23 recovery closes a still-open carrier claim")
		}
	}
	return nil
}

func validateCityOpenWorldFreightSettlementCheckpoint(
	tick int64,
	runtime *cityOpenWorldRuntimeHashState,
) error {
	if tick <= 0 || runtime == nil || runtime.FreightSettlements == nil || runtime.SupplyChain == nil ||
		runtime.EnterpriseFreightReceipts == nil || runtime.EnterpriseFreightBatches == nil {
		return fmt.Errorf("V22 freight-settlement checkpoint prerequisites are unavailable")
	}
	settlements := runtime.FreightSettlements
	orders := make(map[string]CityOpenWorldFreightSettlementOrder, len(settlements.Orders))
	for _, order := range settlements.Orders {
		orders[order.Code] = order
	}
	supplyOrders := make(map[string]struct{}, len(runtime.SupplyChain.Orders))
	for _, order := range runtime.SupplyChain.Orders {
		supplyOrders[order.Code] = struct{}{}
	}
	deliveries := make(map[string]struct{}, len(runtime.SupplyChain.Deliveries))
	for _, delivery := range runtime.SupplyChain.Deliveries {
		deliveries[delivery.OrderCode] = struct{}{}
	}
	shipments := make(map[string]CityOpenWorldEnterpriseFreightShipment, len(runtime.EnterpriseFreightReceipts.Shipments))
	for _, shipment := range runtime.EnterpriseFreightReceipts.Shipments {
		shipments[shipment.Code] = shipment
	}
	plans := make(map[string]CityOpenWorldFreightBatchPlan, len(runtime.EnterpriseFreightBatches.Plans))
	for _, plan := range runtime.EnterpriseFreightBatches.Plans {
		plans[plan.Code] = plan
	}
	consignments := make(map[string]CityOpenWorldFreightBatchConsignment, len(runtime.EnterpriseFreightBatches.Consignments))
	for _, consignment := range runtime.EnterpriseFreightBatches.Consignments {
		consignments[consignment.Code] = consignment
	}
	casesByOrder := make(map[string]int, len(orders))
	settledCasesByOrder := make(map[string]int, len(orders))
	for _, settlementCase := range settlements.Cases {
		order, exists := orders[settlementCase.SettlementOrderCode]
		if !exists || settlementCase.SourceTick > tick {
			return fmt.Errorf("V22 freight-settlement case has an unavailable or future order")
		}
		casesByOrder[order.Code]++
		if settlementCase.State == cityOpenWorldFreightSettlementCaseSettled {
			settledCasesByOrder[order.Code]++
		}
		switch settlementCase.SourceKind {
		case cityOpenWorldFreightSettlementSourceShipment:
			shipment, found := shipments[settlementCase.SourceCode]
			if !found || order.SourceKind != cityOpenWorldFreightSettlementSourceShipment ||
				order.SourceCode != shipment.Code || order.OrderCode != shipment.OrderCode ||
				settlementCase.SourceTick != shipment.SourceTick {
				return fmt.Errorf("V22 freight-settlement shipment custody reference is inconsistent")
			}
			expectedState := settlementCase.TransportState
			// V22 records a case-level financial outcome, but the V17 custody
			// transition remains an order-level consequence of the one V15
			// settlement fact. A partial V18 batch may therefore contain a
			// settled V22 case while its source custody stays awaiting receipt.
			if order.State == cityOpenWorldFreightSettlementOrderSettled {
				expectedState = cityOpenWorldEnterpriseFreightReceiptStateSettled
			}
			if shipment.State != expectedState {
				return fmt.Errorf("V22 freight-settlement shipment custody state is inconsistent")
			}
		case cityOpenWorldFreightSettlementSourceConsignment:
			consignment, found := consignments[settlementCase.SourceCode]
			plan, planFound := plans[order.SourceCode]
			if !found || !planFound || order.SourceKind != cityOpenWorldFreightSettlementSourceConsignment ||
				consignment.PlanCode != plan.Code || order.OrderCode != plan.OrderCode ||
				settlementCase.SourceTick != plan.SourceTick {
				return fmt.Errorf("V22 freight-settlement consignment custody reference is inconsistent")
			}
			expectedState := settlementCase.TransportState
			if order.State == cityOpenWorldFreightSettlementOrderSettled {
				expectedState = cityOpenWorldFreightBatchConsignmentStateSettled
			}
			if consignment.State != expectedState {
				return fmt.Errorf("V22 freight-settlement consignment custody state is inconsistent")
			}
		default:
			return fmt.Errorf("V22 freight-settlement case has an unknown source kind")
		}
	}
	for _, order := range settlements.Orders {
		if order.SourceTick > tick {
			return fmt.Errorf("V22 freight-settlement order contains future source evidence")
		}
		if _, exists := supplyOrders[order.OrderCode]; !exists || casesByOrder[order.Code] == 0 {
			return fmt.Errorf("V22 freight-settlement order has unavailable V15 evidence")
		}
		expectedSupplyState := cityOpenWorldSupplyChainStateDispatched
		switch order.State {
		case cityOpenWorldFreightSettlementOrderSettled:
			expectedSupplyState = cityOpenWorldSupplyChainStateSettled
			if settledCasesByOrder[order.Code] != casesByOrder[order.Code] {
				return fmt.Errorf("V22 freight-settlement order settled before all cases")
			}
		case cityOpenWorldFreightSettlementOrderVoided:
			// A no-receipt V22 void is the only successor path that allows V15
			// to fail. It leaves V17/V18 transport observation immutable and
			// intentionally has no V15 atomic delivery.
			expectedSupplyState = cityOpenWorldSupplyChainStateFailed
		}
		if cityOpenWorldSupplyChainCurrentState(runtime.SupplyChain.Transitions, order.OrderCode) != expectedSupplyState {
			return fmt.Errorf("V22 freight-settlement order V15 terminal state is inconsistent")
		}
		if _, delivered := deliveries[order.OrderCode]; delivered {
			return fmt.Errorf("V22 freight-settlement order cannot retain a V15 atomic delivery")
		}
		if order.SourceKind == cityOpenWorldFreightSettlementSourceConsignment &&
			order.State == cityOpenWorldFreightSettlementOrderSettled && plans[order.SourceCode].State != cityOpenWorldFreightBatchPlanStateSettled {
			return fmt.Errorf("V22 freight-settlement batch plan is not settled")
		}
	}
	for _, receipt := range settlements.Receipts {
		if receipt.ReceiptTick > tick ||
			(receipt.ResourceOperation != nil && receipt.ResourceOperation.Tick > tick) ||
			(receipt.Journal != nil && receipt.Journal.Tick > tick) {
			return fmt.Errorf("V22 freight-settlement receipt contains future evidence")
		}
	}
	for _, claim := range settlements.Claims {
		if claim.CreatedTick > tick {
			return fmt.Errorf("V22 freight-settlement claim contains future evidence")
		}
	}
	return nil
}

// replayCityOpenWorldInfrastructureCheckpoint proves V20's mutable asset
// projection after the sealed tick checkpoint has replaced the prior state.
// V20 does not alter V9 scheduling; each state transition must instead point
// to one exact runtime fact with a matching immutable asset identity.
func replayCityOpenWorldInfrastructureCheckpoint(tick int64, state *cityHashState) error {
	if state == nil || !cityEngineSupportsOpenWorldInfrastructure(state.SimulationVersion) ||
		state.OpenWorldRuntime == nil || state.OpenWorldRuntime.Infrastructure == nil {
		return fmt.Errorf("open-world infrastructure replay state is unavailable")
	}
	return validateCityOpenWorldInfrastructureCheckpoint(tick, state.OpenWorldRuntime)
}

// replayCityOpenWorldEffectiveCapacityCheckpoint runs after V9's mobility
// checkpoint. V21 admission evidence references the V9 allocation and its
// schedule fact, so validating it earlier would invert the causal order.
func replayCityOpenWorldEffectiveCapacityCheckpoint(tick int64, state *cityHashState) error {
	if state == nil || !cityEngineSupportsOpenWorldEffectiveCapacity(state.SimulationVersion) ||
		state.OpenWorldRuntime == nil || state.OpenWorldRuntime.EffectiveCapacity == nil {
		return fmt.Errorf("open-world effective-capacity replay state is unavailable")
	}
	if err := validateCityOpenWorldEffectiveCapacityRuntimeState(state.OpenWorldRuntime); err != nil {
		return fmt.Errorf("invalid V21 effective-capacity checkpoint: %w", err)
	}
	for _, admission := range state.OpenWorldRuntime.EffectiveCapacity.Admissions {
		if admission.DepartureTick > tick || admission.ScheduleFact.Tick > tick ||
			(admission.StateSourceFact != nil && admission.StateSourceFact.Tick > tick) {
			return fmt.Errorf("V21 effective-capacity checkpoint contains future evidence")
		}
	}
	return nil
}

func cityOpenWorldSpatialNetworkStaticCheckpointEqual(
	current, checkpoint *CityOpenWorldSpatialNetworkState,
) bool {
	if current == nil || checkpoint == nil {
		return false
	}
	if validateCityOpenWorldSpatialNetworkState(current) != nil ||
		validateCityOpenWorldSpatialNetworkState(checkpoint) != nil {
		return false
	}
	return reflect.DeepEqual(*current, *checkpoint)
}

// cityOpenWorldInfrastructureStaticCheckpointEqual compares the immutable
// V20 foundation only. State, transition count, and revision are intentionally
// omitted because they are append-only runtime facts validated by the V20
// replay stage after the checkpoint is installed.
func cityOpenWorldInfrastructureStaticCheckpointEqual(
	previous, checkpoint *CityOpenWorldInfrastructureState,
) bool {
	if previous == nil || checkpoint == nil ||
		validateCityOpenWorldInfrastructureState(previous) != nil ||
		validateCityOpenWorldInfrastructureState(checkpoint) != nil {
		return false
	}
	left, right := previous.Policy, checkpoint.Policy
	if left.ProfileID != right.ProfileID || left.ProfileVersion != right.ProfileVersion ||
		left.ContentHash != right.ContentHash || left.BaselineTick != right.BaselineTick ||
		left.AssetContract != right.AssetContract || left.StateContract != right.StateContract ||
		left.MaximumAssets != right.MaximumAssets || left.AssetCount != right.AssetCount ||
		left.NodeAssetCount != right.NodeAssetCount || left.SegmentAssetCount != right.SegmentAssetCount ||
		!reflect.DeepEqual(left.Metadata, right.Metadata) {
		return false
	}
	return reflect.DeepEqual(previous.Assets, checkpoint.Assets)
}

// replayCityOpenWorldSupplyChainInventoryTopology restores a compatibility
// detail of early V15 worlds.  Their first order could lazily allocate a zero
// balance for a node/resource pair before any F3 entry existed, so a replay
// based on a pre-order snapshot had no generic resource event from which to
// discover that balance.  The authoritative order.proposed fact and its
// immutable line are sufficient provenance for the zero row.  New V15 worlds
// pre-provision this topology at genesis, but retaining the reconstruction
// keeps previously created V15 worlds replayable.
func replayCityOpenWorldSupplyChainInventoryTopology(
	tick int64,
	state *cityHashState,
	supplyChain *CityOpenWorldSupplyChainState,
) error {
	if state == nil || supplyChain == nil {
		return fmt.Errorf("open-world supply-chain inventory topology is unavailable")
	}
	orders := make(map[string]CityOpenWorldSupplyChainOrder, len(supplyChain.Orders))
	for _, order := range supplyChain.Orders {
		orders[order.Code] = order
	}
	known := make(map[string]struct{}, len(state.Physical.Inventories))
	for _, inventory := range state.Physical.Inventories {
		known[cityInventoryHashKey(inventory.EntityCode, inventory.DistrictCode, inventory.ResourceCode)] = struct{}{}
	}
	for _, line := range supplyChain.Lines {
		order, found := orders[line.OrderCode]
		if !found {
			return fmt.Errorf("open-world supply-chain line references an unavailable order")
		}
		if order.CreatedTick > tick {
			continue
		}
		for _, endpoint := range []struct {
			firmCode     string
			districtCode string
		}{
			{firmCode: line.SourceFirmCode, districtCode: line.SourceDistrictCode},
			{firmCode: line.DestinationFirmCode, districtCode: line.DestinationDistrictCode},
		} {
			key := cityInventoryHashKey(endpoint.firmCode, endpoint.districtCode, line.ResourceCode)
			if _, exists := known[key]; exists {
				continue
			}
			state.Physical.Inventories = append(state.Physical.Inventories, cityHashInventory{
				EntityType: "firm", EntityCode: endpoint.firmCode, DistrictCode: endpoint.districtCode,
				ResourceCode: line.ResourceCode, OpeningQuantityUnits: 0, QuantityUnits: 0,
				Version: 0, Status: "active", Metadata: json.RawMessage(`{}`),
			})
			known[key] = struct{}{}
		}
	}
	districtOrder := make(map[string]int, len(state.Physical.Districts))
	for _, district := range state.Physical.Districts {
		districtOrder[district.Code] = district.SortOrder
	}
	sort.Slice(state.Physical.Inventories, func(i, j int) bool {
		left, right := state.Physical.Inventories[i], state.Physical.Inventories[j]
		if left.EntityCode != right.EntityCode {
			return left.EntityCode < right.EntityCode
		}
		if districtOrder[left.DistrictCode] != districtOrder[right.DistrictCode] {
			return districtOrder[left.DistrictCode] < districtOrder[right.DistrictCode]
		}
		return left.ResourceCode < right.ResourceCode
	})
	return nil
}

func cityOpenWorldMobilityStaticCheckpointEqual(
	previous, checkpoint *CityOpenWorldMobilityState,
) bool {
	if previous == nil || checkpoint == nil {
		return previous == checkpoint
	}
	left, right := previous.Policy, checkpoint.Policy
	if left.ProfileID != right.ProfileID || left.ProfileVersion != right.ProfileVersion ||
		left.ContentHash != right.ContentHash || left.BaselineTick != right.BaselineTick ||
		left.TopologyContractVersion != right.TopologyContractVersion ||
		left.SchedulingContract != right.SchedulingContract ||
		left.MaximumSchedulesPerTick != right.MaximumSchedulesPerTick ||
		left.MaximumWaitTicks != right.MaximumWaitTicks || left.ModeCount != right.ModeCount ||
		left.HubCount != right.HubCount || left.EdgeCount != right.EdgeCount ||
		!reflect.DeepEqual(left.Metadata, right.Metadata) {
		return false
	}
	return reflect.DeepEqual(previous.Modes, checkpoint.Modes) &&
		reflect.DeepEqual(previous.Hubs, checkpoint.Hubs) &&
		reflect.DeepEqual(previous.Edges, checkpoint.Edges)
}

func cityOpenWorldCommuteStaticCheckpointEqual(
	previous, checkpoint *CityOpenWorldCommuteState,
) bool {
	if previous == nil || checkpoint == nil {
		return previous == checkpoint
	}
	return reflect.DeepEqual(previous.Policy, checkpoint.Policy) &&
		reflect.DeepEqual(previous.Bindings, checkpoint.Bindings)
}

func cityOpenWorldSupplyChainStaticCheckpointEqual(
	previous, checkpoint *CityOpenWorldSupplyChainState,
) bool {
	if previous == nil || checkpoint == nil {
		return previous == checkpoint
	}
	left, right := previous.Policy, checkpoint.Policy
	if left.ProfileID != right.ProfileID || left.ProfileVersion != right.ProfileVersion ||
		left.ContentHash != right.ContentHash || left.BaselineTick != right.BaselineTick ||
		left.NodeContract != right.NodeContract || left.OrderContract != right.OrderContract ||
		left.SettlementContract != right.SettlementContract || left.DeliveryContract != right.DeliveryContract ||
		left.MaximumOrders != right.MaximumOrders || left.MaximumOrderLines != right.MaximumOrderLines ||
		left.MaximumTransitionsPerTick != right.MaximumTransitionsPerTick ||
		left.AcceptTimeoutTicks != right.AcceptTimeoutTicks || left.DispatchTimeoutTicks != right.DispatchTimeoutTicks ||
		!reflect.DeepEqual(left.Metadata, right.Metadata) {
		return false
	}
	return reflect.DeepEqual(previous.Nodes, checkpoint.Nodes)
}

func cityOpenWorldEnterpriseFreightStaticCheckpointEqual(
	previous, checkpoint *CityOpenWorldEnterpriseFreightState,
) bool {
	if previous == nil || checkpoint == nil {
		return previous == checkpoint
	}
	return reflect.DeepEqual(previous.Policy.ProfileID, checkpoint.Policy.ProfileID) &&
		reflect.DeepEqual(previous.Policy.ProfileVersion, checkpoint.Policy.ProfileVersion) &&
		reflect.DeepEqual(previous.Policy.ContentHash, checkpoint.Policy.ContentHash) &&
		previous.Policy.BaselineTick == checkpoint.Policy.BaselineTick &&
		previous.Policy.SourceContract == checkpoint.Policy.SourceContract &&
		previous.Policy.DemandContract == checkpoint.Policy.DemandContract &&
		previous.Policy.CompletionContract == checkpoint.Policy.CompletionContract &&
		previous.Policy.TerminalContract == checkpoint.Policy.TerminalContract &&
		previous.Policy.CarrierActorCode == checkpoint.Policy.CarrierActorCode &&
		previous.Policy.MaximumSources == checkpoint.Policy.MaximumSources &&
		previous.Policy.MaximumGenerationsPerTick == checkpoint.Policy.MaximumGenerationsPerTick &&
		reflect.DeepEqual(previous.Policy.Metadata, checkpoint.Policy.Metadata)
}

func cityOpenWorldEnterpriseFreightReceiptStaticCheckpointEqual(
	previous, checkpoint *CityOpenWorldEnterpriseFreightReceiptState,
) bool {
	if previous == nil || checkpoint == nil {
		return previous == checkpoint
	}
	left, right := previous.Policy, checkpoint.Policy
	return left.ProfileID == right.ProfileID &&
		left.ProfileVersion == right.ProfileVersion &&
		left.ContentHash == right.ContentHash &&
		left.BaselineTick == right.BaselineTick &&
		left.ShipmentContract == right.ShipmentContract &&
		left.ReceiptContract == right.ReceiptContract &&
		left.LegacyContract == right.LegacyContract &&
		left.MaximumShipments == right.MaximumShipments &&
		left.MaximumObservationsPerTick == right.MaximumObservationsPerTick &&
		reflect.DeepEqual(left.Metadata, right.Metadata)
}

func cityOpenWorldFreightBatchStaticCheckpointEqual(
	previous, checkpoint *CityOpenWorldFreightBatchState,
) bool {
	if previous == nil || checkpoint == nil {
		return previous == checkpoint
	}
	left, right := previous.Policy, checkpoint.Policy
	return left.ProfileID == right.ProfileID &&
		left.ProfileVersion == right.ProfileVersion &&
		left.ContentHash == right.ContentHash &&
		left.BaselineTick == right.BaselineTick &&
		left.SourceContract == right.SourceContract &&
		left.PackingContract == right.PackingContract &&
		left.TransportContract == right.TransportContract &&
		left.ReceiptContract == right.ReceiptContract &&
		left.MaximumUnits == right.MaximumUnits &&
		left.MaximumConsignmentsPerPlan == right.MaximumConsignmentsPerPlan &&
		left.MaximumPlansPerTick == right.MaximumPlansPerTick &&
		left.MaximumObservationsPerTick == right.MaximumObservationsPerTick &&
		reflect.DeepEqual(left.Metadata, right.Metadata)
}

func validateCityOpenWorldStaticCheckpoint(
	previous, checkpoint *cityOpenWorldHashState,
) error {
	if previous == nil || checkpoint == nil || !reflect.DeepEqual(previous.Binding, checkpoint.Binding) {
		return fmt.Errorf("open-world generator binding changed during replay")
	}
	if err := cityOpenWorldReplayContains(previous.Regions, checkpoint.Regions, "regions"); err != nil {
		return err
	}
	if err := cityOpenWorldReplayContains(previous.Sectors, checkpoint.Sectors, "sectors"); err != nil {
		return err
	}
	if err := cityOpenWorldReplayContains(previous.Chunks, checkpoint.Chunks, "chunks"); err != nil {
		return err
	}
	if err := cityOpenWorldReplayContains(previous.Buildings, checkpoint.Buildings, "buildings"); err != nil {
		return err
	}
	if err := cityOpenWorldReplayContains(previous.Interiors, checkpoint.Interiors, "interiors"); err != nil {
		return err
	}
	return cityOpenWorldReplayContains(previous.Portals, checkpoint.Portals, "portals")
}

func cityOpenWorldReplayContains[T any](previous, checkpoint []T, label string) error {
	known := make(map[string]struct{}, len(checkpoint))
	for _, item := range checkpoint {
		identity, err := cityOpenWorldReplayCanonicalIdentity(item)
		if err != nil {
			return fmt.Errorf("encode open-world replay %s: %w", label, err)
		}
		known[identity] = struct{}{}
	}
	for _, item := range previous {
		identity, err := cityOpenWorldReplayCanonicalIdentity(item)
		if err != nil {
			return fmt.Errorf("encode open-world replay %s: %w", label, err)
		}
		if _, found := known[identity]; !found {
			return fmt.Errorf("open-world replay checkpoint removed immutable %s", label)
		}
	}
	return nil
}

func validateCityOpenWorldRuntimeCheckpoint(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, tick int64,
	simulationVersion string,
	previous, checkpoint *cityOpenWorldRuntimeHashState,
) error {
	if previous == nil || checkpoint == nil {
		return fmt.Errorf("open-world runtime replay checkpoint is unavailable")
	}
	if err := validateCityOpenWorldRuntimeRecoveryState(simulationVersion, checkpoint); err != nil {
		return fmt.Errorf("invalid open-world runtime replay checkpoint: %w", err)
	}
	if !reflect.DeepEqual(previous.Definitions, checkpoint.Definitions) {
		return fmt.Errorf("open-world runtime definitions changed during replay")
	}

	facts, err := loadCityOpenWorldRuntimeFactsForTick(ctx, queryer, worldID, tick)
	if err != nil {
		return err
	}
	effects, err := loadCityOpenWorldRuntimeEffectsForTick(ctx, queryer, worldID, tick)
	if err != nil {
		return err
	}
	cases, err := loadCityOpenWorldRuleCasesForTick(ctx, queryer, worldID, tick)
	if err != nil {
		return err
	}
	if err = cityOpenWorldReplayTail(previous.Facts, checkpoint.Facts, facts, "facts"); err != nil {
		return err
	}
	if err = cityOpenWorldReplayTail(previous.Effects, checkpoint.Effects, effects, "effects"); err != nil {
		return err
	}
	if err = cityOpenWorldReplayTail(previous.RuleCases, checkpoint.RuleCases, cases, "rule cases"); err != nil {
		return err
	}
	if err = validateCityOpenWorldRuntimeEvidence(tick, checkpoint); err != nil {
		return err
	}
	return nil
}

func cityOpenWorldReplayTail[T any](
	previous, checkpoint, persisted []T,
	label string,
) error {
	if len(checkpoint) < len(previous) || len(checkpoint)-len(previous) != len(persisted) {
		return fmt.Errorf("open-world replay %s cardinality is inconsistent", label)
	}
	for index := range previous {
		equal, err := cityOpenWorldReplayEquivalent(previous[index], checkpoint[index])
		if err != nil {
			return fmt.Errorf("encode open-world replay %s: %w", label, err)
		}
		if !equal {
			return fmt.Errorf("open-world replay %s rewrote prior evidence", label)
		}
	}
	for index := range persisted {
		equal, err := cityOpenWorldReplayEquivalent(persisted[index], checkpoint[len(previous)+index])
		if err != nil {
			return fmt.Errorf("encode open-world replay %s: %w", label, err)
		}
		if !equal {
			return fmt.Errorf("open-world replay %s checkpoint tail differs from persisted evidence", label)
		}
	}
	return nil
}

func cityOpenWorldReplayCanonicalIdentity(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	canonical, err := canonicalWorldRuntimeJSON(raw)
	if err != nil {
		return "", err
	}
	return string(canonical), nil
}

func cityOpenWorldReplayEquivalent(left, right any) (bool, error) {
	leftIdentity, err := cityOpenWorldReplayCanonicalIdentity(left)
	if err != nil {
		return false, err
	}
	rightIdentity, err := cityOpenWorldReplayCanonicalIdentity(right)
	if err != nil {
		return false, err
	}
	return bytes.Equal([]byte(leftIdentity), []byte(rightIdentity)), nil
}

func validateCityOpenWorldRuntimeEvidence(tick int64, runtime *cityOpenWorldRuntimeHashState) error {
	if runtime == nil {
		return fmt.Errorf("open-world runtime evidence is unavailable")
	}
	definitions := make(map[string]CityOpenWorldRuntimeDefinition, len(runtime.Definitions))
	for _, definition := range runtime.Definitions {
		definitions[definition.Kind+"\x00"+definition.Code] = definition
	}
	actors := make(map[string]struct{}, len(runtime.Actors))
	for _, actor := range runtime.Actors {
		if actor.Code == "" {
			return fmt.Errorf("open-world runtime evidence contains an anonymous actor")
		}
		actors[actor.Code] = struct{}{}
	}
	facts := make(map[CityOpenWorldRuntimeFactRef]CityOpenWorldRuntimeFact, len(runtime.Facts))
	for _, fact := range runtime.Facts {
		key := CityOpenWorldRuntimeFactRef{Tick: fact.Tick, Sequence: fact.Sequence}
		if fact.Tick < 1 || fact.Sequence < 1 || !json.Valid(fact.Payload) {
			return fmt.Errorf("open-world runtime fact envelope is invalid")
		}
		if _, duplicate := facts[key]; duplicate {
			return fmt.Errorf("open-world runtime fact identity is duplicated")
		}
		if fact.Parent != nil {
			parent, found := facts[*fact.Parent]
			if !found || parent.Tick > fact.Tick ||
				(parent.Tick == fact.Tick && parent.Sequence >= fact.Sequence) {
				return fmt.Errorf("open-world runtime fact parent chain is invalid")
			}
		}
		if fact.ActorCode != nil {
			if _, found := actors[*fact.ActorCode]; !found {
				return fmt.Errorf("open-world runtime fact references an unknown actor")
			}
		}
		if fact.DefinitionKind != nil || fact.DefinitionCode != nil || fact.DefinitionVersion != nil || fact.DefinitionHash != nil {
			if fact.DefinitionKind == nil || fact.DefinitionCode == nil || fact.DefinitionVersion == nil || fact.DefinitionHash == nil {
				return fmt.Errorf("open-world runtime fact definition proof is incomplete")
			}
			definition, found := definitions[*fact.DefinitionKind+"\x00"+*fact.DefinitionCode]
			if !found || definition.Version != *fact.DefinitionVersion || definition.Hash != *fact.DefinitionHash {
				return fmt.Errorf("open-world runtime fact definition proof mismatch")
			}
		}
		facts[key] = fact
	}

	effectSequences := make(map[CityOpenWorldRuntimeFactRef]struct{}, len(runtime.Effects))
	for _, effect := range runtime.Effects {
		if effect.Tick < 1 || effect.Sequence < 1 || effect.EffectType == "" ||
			!json.Valid(effect.Payload) {
			return fmt.Errorf("open-world runtime effect envelope is invalid")
		}
		identity := CityOpenWorldRuntimeFactRef{Tick: effect.Tick, Sequence: effect.Sequence}
		if _, duplicate := effectSequences[identity]; duplicate {
			return fmt.Errorf("open-world runtime effect sequence is duplicated")
		}
		effectSequences[identity] = struct{}{}
		if _, found := facts[effect.SourceFact]; !found {
			return fmt.Errorf("open-world runtime effect source fact is missing")
		}
		if effect.TargetActorCode != nil {
			if _, found := actors[*effect.TargetActorCode]; !found {
				return fmt.Errorf("open-world runtime effect target actor is unknown")
			}
		}
		if effect.BeforeUnits != nil || effect.DeltaUnits != nil || effect.AfterUnits != nil {
			if effect.BeforeUnits == nil || effect.DeltaUnits == nil || effect.AfterUnits == nil ||
				cityOpenWorldRuntimeSaturatingAdd(*effect.BeforeUnits, *effect.DeltaUnits) != *effect.AfterUnits {
				return fmt.Errorf("open-world runtime effect unit envelope is invalid")
			}
		}
	}

	caseSequences := make(map[CityOpenWorldRuntimeFactRef]struct{}, len(runtime.RuleCases))
	for _, item := range runtime.RuleCases {
		if item.Tick < 1 || item.Sequence < 1 || item.Code == "" || !json.Valid(item.Payload) {
			return fmt.Errorf("open-world runtime rule case envelope is invalid")
		}
		identity := CityOpenWorldRuntimeFactRef{Tick: item.Tick, Sequence: item.Sequence}
		if _, duplicate := caseSequences[identity]; duplicate {
			return fmt.Errorf("open-world runtime rule case sequence is duplicated")
		}
		caseSequences[identity] = struct{}{}
		if _, found := facts[item.SourceFact]; !found {
			return fmt.Errorf("open-world runtime rule case source fact is missing")
		}
		if item.ConsequenceFact != nil {
			if _, found := facts[*item.ConsequenceFact]; !found {
				return fmt.Errorf("open-world runtime rule case consequence fact is missing")
			}
		}
		if _, found := actors[item.SubjectActorCode]; !found {
			return fmt.Errorf("open-world runtime rule case subject actor is unknown")
		}
	}

	if runtime.Profile.FactCount != int64(len(runtime.Facts)) ||
		runtime.Profile.EffectCount != int64(len(runtime.Effects)) ||
		runtime.Profile.CaseCount != int64(len(runtime.RuleCases)) ||
		runtime.Profile.Revision != runtime.Profile.FactCount+1 ||
		runtime.Profile.ActorCount != int64(len(runtime.Actors)) {
		return fmt.Errorf("open-world runtime profile evidence counters are inconsistent")
	}
	if tick > 0 && runtime.Profile.FactCount > 0 {
		last := runtime.Facts[len(runtime.Facts)-1]
		if last.Tick > tick {
			return fmt.Errorf("open-world runtime checkpoint contains future facts")
		}
	}
	return nil
}

func validateCityOpenWorldServiceCheckpoint(tick int64, runtime *cityOpenWorldRuntimeHashState) error {
	services := runtime.Services
	if services == nil || services.Policy.ProfileID != cityOpenWorldServiceProfileID ||
		services.Policy.ProfileVersion != cityOpenWorldServiceProfileVersion ||
		services.Policy.BaselineTick < 0 || services.Policy.Revision < 1 ||
		!json.Valid(services.Policy.Metadata) {
		return fmt.Errorf("open-world service checkpoint policy is invalid")
	}
	actors := make(map[string]struct{}, len(runtime.Actors))
	for _, actor := range runtime.Actors {
		actors[actor.Code] = struct{}{}
	}
	facts := make(map[CityOpenWorldRuntimeFactRef]struct{}, len(runtime.Facts))
	for _, fact := range runtime.Facts {
		facts[CityOpenWorldRuntimeFactRef{Tick: fact.Tick, Sequence: fact.Sequence}] = struct{}{}
	}
	providers := make(map[string]struct{}, len(services.Providers))
	for _, provider := range services.Providers {
		if provider.Code == "" || provider.ServiceCode == "" || provider.CapacityUnitsPerTick < 1 ||
			provider.AccessRadiusUnits < 0 || provider.Version < 1 || !json.Valid(provider.Metadata) {
			return fmt.Errorf("open-world service provider checkpoint is invalid")
		}
		if _, duplicate := providers[provider.Code]; duplicate {
			return fmt.Errorf("open-world service provider is duplicated")
		}
		providers[provider.Code] = struct{}{}
	}
	requests := make(map[string]CityOpenWorldServiceRequest, len(services.Requests))
	for _, request := range services.Requests {
		if request.Code == "" || request.ActorCode == "" || request.ServiceCode == "" ||
			request.RequestedTick < 1 || request.Version < 1 || !json.Valid(request.Metadata) {
			return fmt.Errorf("open-world service request checkpoint is invalid")
		}
		if _, found := actors[request.ActorCode]; !found {
			return fmt.Errorf("open-world service request actor is unknown")
		}
		if _, found := facts[request.SourceFact]; !found {
			return fmt.Errorf("open-world service request source fact is missing")
		}
		if request.LastFact == nil {
			return fmt.Errorf("open-world service request last fact is missing")
		}
		if _, found := facts[*request.LastFact]; !found {
			return fmt.Errorf("open-world service request last fact is missing")
		}
		if request.ProviderCode != nil {
			if _, found := providers[*request.ProviderCode]; !found {
				return fmt.Errorf("open-world service request provider is unknown")
			}
		}
		if _, duplicate := requests[request.Code]; duplicate {
			return fmt.Errorf("open-world service request is duplicated")
		}
		requests[request.Code] = request
	}
	responses := make(map[string]struct{}, len(services.Responses))
	for _, response := range services.Responses {
		request, found := requests[response.RequestCode]
		if response.Code == "" || !found || response.ActorCode != request.ActorCode ||
			response.ServiceCode != request.ServiceCode || response.ResolvedTick < response.RequestedTick ||
			response.ResolvedTick > tick || !json.Valid(response.Metadata) {
			return fmt.Errorf("open-world service response checkpoint is invalid")
		}
		if _, found = facts[response.SourceFact]; !found {
			return fmt.Errorf("open-world service response source fact is missing")
		}
		if response.ProviderCode != nil {
			if _, found = providers[*response.ProviderCode]; !found {
				return fmt.Errorf("open-world service response provider is unknown")
			}
		}
		if _, duplicate := responses[response.Code]; duplicate {
			return fmt.Errorf("open-world service response is duplicated")
		}
		responses[response.Code] = struct{}{}
	}
	return nil
}

func validateCityOpenWorldImpactCheckpoint(tick int64, runtime *cityOpenWorldRuntimeHashState) error {
	impacts := runtime.Impacts
	if impacts == nil || runtime.Services == nil {
		return fmt.Errorf("open-world impact checkpoint is unavailable")
	}
	responses := make(map[string]struct{}, len(runtime.Services.Responses))
	for _, response := range runtime.Services.Responses {
		responses[response.Code] = struct{}{}
	}
	facts := make(map[CityOpenWorldRuntimeFactRef]struct{}, len(runtime.Facts))
	for _, fact := range runtime.Facts {
		facts[CityOpenWorldRuntimeFactRef{Tick: fact.Tick, Sequence: fact.Sequence}] = struct{}{}
	}
	for _, effect := range impacts.Effects {
		if _, found := responses[effect.SourceResponseCode]; !found {
			return fmt.Errorf("V8 impact checkpoint source response is missing")
		}
		if _, found := facts[effect.SourceFact]; !found {
			return fmt.Errorf("V8 impact checkpoint source fact is missing")
		}
		if effect.Status == "applied" {
			if effect.ApplicationFact == nil || *effect.AppliedTick > tick {
				return fmt.Errorf("V8 impact checkpoint application is invalid")
			}
			if _, found := facts[*effect.ApplicationFact]; !found {
				return fmt.Errorf("V8 impact checkpoint application fact is missing")
			}
		}
	}
	return nil
}

func validateCityOpenWorldMobilityCheckpoint(tick int64, runtime *cityOpenWorldRuntimeHashState) error {
	if runtime == nil || runtime.Mobility == nil {
		return fmt.Errorf("open-world mobility checkpoint is unavailable")
	}
	actors := make(map[string]struct{}, len(runtime.Actors))
	for _, actor := range runtime.Actors {
		actors[actor.Code] = struct{}{}
	}
	facts := make(map[CityOpenWorldRuntimeFactRef]CityOpenWorldRuntimeFact, len(runtime.Facts))
	for _, fact := range runtime.Facts {
		facts[CityOpenWorldRuntimeFactRef{Tick: fact.Tick, Sequence: fact.Sequence}] = fact
	}
	for _, demand := range runtime.Mobility.Demands {
		if _, found := actors[demand.ActorCode]; !found {
			return fmt.Errorf("V9 mobility checkpoint demand actor is unknown")
		}
		source, found := facts[demand.SourceFact]
		if !found || source.FactType != CityOpenWorldRuntimeFactMobilityRequested ||
			source.ActorCode == nil || *source.ActorCode != demand.ActorCode || source.Tick > tick {
			return fmt.Errorf("V9 mobility checkpoint demand source fact is invalid")
		}
		if demand.LastFact == nil {
			return fmt.Errorf("V9 mobility checkpoint demand last fact is missing")
		}
		last, found := facts[*demand.LastFact]
		if !found || last.ActorCode == nil || *last.ActorCode != demand.ActorCode || last.Tick > tick {
			return fmt.Errorf("V9 mobility checkpoint demand last fact is invalid")
		}
		expectedFactType := CityOpenWorldRuntimeFactMobilityRequested
		switch demand.Status {
		case "scheduled":
			expectedFactType = CityOpenWorldRuntimeFactMobilityScheduled
		case "completed":
			expectedFactType = CityOpenWorldRuntimeFactMobilityCompleted
		case "expired":
			expectedFactType = CityOpenWorldRuntimeFactMobilityExpired
		}
		if last.FactType != expectedFactType {
			return fmt.Errorf("V9 mobility checkpoint demand lifecycle fact is invalid")
		}
	}
	for _, route := range runtime.Mobility.Routes {
		if _, found := actors[route.ActorCode]; !found {
			return fmt.Errorf("V9 mobility checkpoint route actor is unknown")
		}
		source, found := facts[route.SourceFact]
		if !found || source.FactType != CityOpenWorldRuntimeFactMobilityScheduled ||
			source.ActorCode == nil || *source.ActorCode != route.ActorCode || source.Tick > tick {
			return fmt.Errorf("V9 mobility checkpoint route source fact is invalid")
		}
		if route.Status == "completed" {
			if route.CompletionFact == nil {
				return fmt.Errorf("V9 mobility checkpoint completed route fact is missing")
			}
			completion, found := facts[*route.CompletionFact]
			if !found || completion.FactType != CityOpenWorldRuntimeFactMobilityCompleted ||
				completion.ActorCode == nil || *completion.ActorCode != route.ActorCode || completion.Tick > tick {
				return fmt.Errorf("V9 mobility checkpoint route completion fact is invalid")
			}
		}
	}
	return nil
}

func validateCityOpenWorldMobilityArrivalCheckpoint(tick int64, runtime *cityOpenWorldRuntimeHashState) error {
	if runtime == nil || runtime.Arrivals == nil || runtime.Mobility == nil {
		return fmt.Errorf("open-world mobility arrival checkpoint is unavailable")
	}
	if err := validateCityOpenWorldMobilityArrivalState(runtime.Arrivals); err != nil {
		return fmt.Errorf("invalid V10 arrival state: %w", err)
	}
	routes := make(map[string]CityOpenWorldMobilityRoute, len(runtime.Mobility.Routes))
	for _, route := range runtime.Mobility.Routes {
		routes[route.Code] = route
	}
	facts := make(map[CityOpenWorldRuntimeFactRef]CityOpenWorldRuntimeFact, len(runtime.Facts))
	for _, fact := range runtime.Facts {
		facts[CityOpenWorldRuntimeFactRef{Tick: fact.Tick, Sequence: fact.Sequence}] = fact
	}
	for _, arrival := range runtime.Arrivals.Arrivals {
		route, found := routes[arrival.RouteCode]
		if !found || route.DemandCode != arrival.DemandCode || route.ActorCode != arrival.ActorCode ||
			route.DestinationHubCode != arrival.DestinationHubCode || route.Status != "completed" || route.CompletionFact == nil {
			return fmt.Errorf("V10 arrival checkpoint route linkage is invalid")
		}
		if arrival.SourceFact != *route.CompletionFact {
			return fmt.Errorf("V10 arrival checkpoint source does not match route completion")
		}
		source, found := facts[arrival.SourceFact]
		if !found || source.FactType != CityOpenWorldRuntimeFactMobilityCompleted ||
			source.ActorCode == nil || *source.ActorCode != arrival.ActorCode || source.Tick > tick {
			return fmt.Errorf("V10 arrival checkpoint completion fact is invalid")
		}
		last, found := facts[arrival.LastFact]
		if !found || last.ActorCode == nil || *last.ActorCode != arrival.ActorCode || last.Tick > tick {
			return fmt.Errorf("V10 arrival checkpoint last fact is invalid")
		}
		expectedLast := CityOpenWorldRuntimeFactMobilityArrivalPending
		switch arrival.Status {
		case cityOpenWorldMobilityArrivalStatusBlocked:
			expectedLast = CityOpenWorldRuntimeFactMobilityArrivalBlocked
		case cityOpenWorldMobilityArrivalStatusLanded:
			expectedLast = CityOpenWorldRuntimeFactMobilityArrivalLanded
		case cityOpenWorldMobilityArrivalStatusFailed:
			expectedLast = CityOpenWorldRuntimeFactMobilityArrivalFailed
		}
		if last.FactType != expectedLast || last.Tick < arrival.CreatedTick {
			return fmt.Errorf("V10 arrival checkpoint lifecycle fact is invalid")
		}
		if arrival.LandingFact != nil {
			landing, found := facts[*arrival.LandingFact]
			if !found || landing.FactType != CityOpenWorldRuntimeFactMobilityArrivalLanded ||
				landing.ActorCode == nil || *landing.ActorCode != arrival.ActorCode {
				return fmt.Errorf("V10 arrival checkpoint landing fact is invalid")
			}
		}
	}
	return nil
}

func validateCityOpenWorldMobilityODCheckpoint(tick int64, runtime *cityOpenWorldRuntimeHashState) error {
	if runtime == nil || runtime.OD == nil || runtime.Mobility == nil {
		return fmt.Errorf("open-world mobility OD checkpoint is unavailable")
	}
	if err := validateCityOpenWorldMobilityODState(runtime.OD); err != nil {
		return fmt.Errorf("invalid V11 OD state: %w", err)
	}
	actors := make(map[string]struct{}, len(runtime.Actors))
	for _, actor := range runtime.Actors {
		actors[actor.Code] = struct{}{}
	}
	facts := make(map[CityOpenWorldRuntimeFactRef]CityOpenWorldRuntimeFact, len(runtime.Facts))
	for _, fact := range runtime.Facts {
		facts[CityOpenWorldRuntimeFactRef{Tick: fact.Tick, Sequence: fact.Sequence}] = fact
	}
	for _, source := range runtime.OD.Sources {
		if _, found := actors[source.ActorCode]; !found {
			return fmt.Errorf("V11 OD checkpoint source actor is unknown")
		}
		if source.LastFact == nil {
			continue
		}
		fact, found := facts[*source.LastFact]
		if !found || fact.ActorCode == nil || *fact.ActorCode != source.ActorCode ||
			fact.Tick != source.LastTransitionTick || fact.Tick > tick ||
			(fact.FactType != cityOpenWorldRuntimeFactMobilityODGenerated &&
				fact.FactType != cityOpenWorldRuntimeFactMobilityODSuppressed) {
			return fmt.Errorf("V11 OD checkpoint source transition fact is invalid")
		}
	}
	for _, metric := range runtime.OD.Metrics {
		fact, found := facts[metric.SourceFact]
		if !found || fact.FactType != cityOpenWorldRuntimeFactMobilityODCycleClose ||
			fact.Tick != metric.ClosedTick || fact.Tick > tick {
			return fmt.Errorf("V11 OD checkpoint cycle metric source fact is invalid")
		}
	}
	for _, demand := range runtime.Mobility.Demands {
		if demand.SourceFact.Tick > tick {
			return fmt.Errorf("V11 OD checkpoint mobility demand is ahead of tick")
		}
		source, found := facts[demand.SourceFact]
		if !found || source.FactType != CityOpenWorldRuntimeFactMobilityRequested {
			return fmt.Errorf("V11 OD checkpoint mobility demand source fact is invalid")
		}
	}
	return nil
}

func validateCityOpenWorldCommuteSourceCheckpoint(tick int64, runtime *cityOpenWorldRuntimeHashState) error {
	if runtime == nil || runtime.CommuteSources == nil || runtime.Commutes == nil || runtime.Mobility == nil {
		return fmt.Errorf("open-world commute source checkpoint is unavailable")
	}
	if err := validateCityOpenWorldCommuteSourceState(runtime.CommuteSources); err != nil {
		return fmt.Errorf("invalid V13 commute source state: %w", err)
	}
	actors := make(map[string]struct{}, len(runtime.Actors))
	for _, actor := range runtime.Actors {
		actors[actor.Code] = struct{}{}
	}
	facts := make(map[CityOpenWorldRuntimeFactRef]CityOpenWorldRuntimeFact, len(runtime.Facts))
	for _, fact := range runtime.Facts {
		facts[CityOpenWorldRuntimeFactRef{Tick: fact.Tick, Sequence: fact.Sequence}] = fact
	}
	bindings := make(map[string]struct{}, len(runtime.Commutes.Bindings))
	for _, binding := range runtime.Commutes.Bindings {
		bindings[binding.Code] = struct{}{}
	}
	for _, source := range runtime.CommuteSources.Sources {
		if _, found := actors[source.ActorCode]; !found {
			return fmt.Errorf("V13 commute source actor is unknown")
		}
		if _, found := bindings[source.BindingCode]; !found {
			return fmt.Errorf("V13 commute source binding is unknown")
		}
		if source.LastFact == nil {
			continue
		}
		fact, found := facts[*source.LastFact]
		if !found || fact.ActorCode == nil || *fact.ActorCode != source.ActorCode ||
			fact.Tick != source.LastTransitionTick || fact.Tick > tick ||
			(fact.FactType != cityOpenWorldRuntimeFactCommuteSourceGenerated &&
				fact.FactType != cityOpenWorldRuntimeFactCommuteSourceSuppressed) {
			return fmt.Errorf("V13 commute source transition fact is invalid")
		}
	}
	for _, metric := range runtime.CommuteSources.Metrics {
		fact, found := facts[metric.SourceFact]
		if !found || fact.FactType != cityOpenWorldRuntimeFactCommuteSourceCycleClose ||
			fact.Tick != metric.ClosedTick || fact.Tick > tick {
			return fmt.Errorf("V13 commute source cycle metric fact is invalid")
		}
	}
	return nil
}

func validateCityOpenWorldCommuteLifecycleCheckpoint(tick int64, runtime *cityOpenWorldRuntimeHashState) error {
	if runtime == nil || runtime.CommuteLifecycle == nil || runtime.Commutes == nil || runtime.Mobility == nil {
		return fmt.Errorf("open-world commute lifecycle checkpoint is unavailable")
	}
	if err := validateCityOpenWorldCommuteLifecycleState(runtime.CommuteLifecycle); err != nil {
		return fmt.Errorf("invalid V14 commute lifecycle state: %w", err)
	}
	actors := make(map[string]struct{}, len(runtime.Actors))
	for _, actor := range runtime.Actors {
		actors[actor.Code] = struct{}{}
	}
	facts := make(map[CityOpenWorldRuntimeFactRef]CityOpenWorldRuntimeFact, len(runtime.Facts))
	for _, fact := range runtime.Facts {
		facts[CityOpenWorldRuntimeFactRef{Tick: fact.Tick, Sequence: fact.Sequence}] = fact
	}
	bindings := make(map[string]struct{}, len(runtime.Commutes.Bindings))
	for _, binding := range runtime.Commutes.Bindings {
		bindings[binding.Code] = struct{}{}
	}
	assignments := make(map[string]CityOpenWorldCommuteAssignmentEpoch, len(runtime.CommuteLifecycle.Assignments))
	for _, assignment := range runtime.CommuteLifecycle.Assignments {
		if _, found := actors[assignment.ActorCode]; !found {
			return fmt.Errorf("V14 commute lifecycle assignment actor is unknown")
		}
		if _, found := bindings[assignment.BindingCode]; !found {
			return fmt.Errorf("V14 commute lifecycle assignment binding is unknown")
		}
		if assignment.OpenedFact != nil {
			fact, found := facts[*assignment.OpenedFact]
			if !found || fact.ActorCode == nil || *fact.ActorCode != assignment.ActorCode ||
				fact.FactType != cityOpenWorldRuntimeFactCommuteLifecycleRebound ||
				fact.Tick != assignment.OpenedTick || fact.Tick > tick {
				return fmt.Errorf("V14 commute lifecycle successor epoch opening fact is invalid")
			}
		}
		assignments[assignment.Code] = assignment
	}
	for _, transition := range runtime.CommuteLifecycle.Transitions {
		assignment, found := assignments[transition.AssignmentCode]
		if !found {
			return fmt.Errorf("V14 commute lifecycle transition assignment is unknown")
		}
		if transition.SourceFact == nil {
			continue
		}
		fact, found := facts[*transition.SourceFact]
		if !found || fact.ActorCode == nil || *fact.ActorCode != assignment.ActorCode ||
			fact.Tick != transition.TransitionTick || fact.Tick > tick ||
			(fact.FactType != cityOpenWorldRuntimeFactCommuteLifecycleRebound &&
				fact.FactType != cityOpenWorldRuntimeFactCommuteLifecycleStateChanged &&
				fact.FactType != cityOpenWorldRuntimeFactCommuteLifecycleAutoSuspended &&
				fact.FactType != cityOpenWorldRuntimeFactCommuteLifecycleAutoResumed &&
				fact.FactType != cityOpenWorldRuntimeFactCommuteLifecycleTerminated) {
			return fmt.Errorf("V14 commute lifecycle transition fact is invalid")
		}
	}
	sources := make(map[string]CityOpenWorldCommuteLifecycleSource, len(runtime.CommuteLifecycle.Sources))
	for _, source := range runtime.CommuteLifecycle.Sources {
		assignment, found := assignments[source.AssignmentCode]
		if !found || assignment.ActorCode != source.ActorCode {
			return fmt.Errorf("V14 commute lifecycle source assignment is invalid")
		}
		if source.LastFact != nil {
			fact, found := facts[*source.LastFact]
			if !found || fact.ActorCode == nil || *fact.ActorCode != source.ActorCode ||
				fact.Tick != source.LastTransitionTick || fact.Tick > tick ||
				(fact.FactType != cityOpenWorldRuntimeFactCommuteLifecycleRebound &&
					fact.FactType != cityOpenWorldRuntimeFactCommuteLifecycleSourceGenerated &&
					fact.FactType != cityOpenWorldRuntimeFactCommuteLifecycleSourceSuppressed) {
				return fmt.Errorf("V14 commute lifecycle source transition fact is invalid")
			}
		}
		sources[source.Code] = source
	}
	for _, metric := range runtime.CommuteLifecycle.Metrics {
		fact, found := facts[metric.SourceFact]
		if !found || fact.FactType != cityOpenWorldRuntimeFactCommuteLifecycleCycleClose ||
			fact.Tick != metric.ClosedTick || fact.Tick > tick {
			return fmt.Errorf("V14 commute lifecycle cycle metric fact is invalid")
		}
	}
	for _, demand := range runtime.Mobility.Demands {
		var metadata map[string]any
		if err := json.Unmarshal(demand.Metadata, &metadata); err != nil {
			return fmt.Errorf("decode V14 commute lifecycle mobility demand metadata: %w", err)
		}
		sourceCode, present := metadata["commute_lifecycle_source_code"].(string)
		if !present || sourceCode == "" {
			continue
		}
		if _, found := sources[sourceCode]; !found || demand.SourceFact.Tick > tick {
			return fmt.Errorf("V14 commute lifecycle mobility demand source is invalid")
		}
		fact, found := facts[demand.SourceFact]
		if !found || fact.FactType != CityOpenWorldRuntimeFactMobilityRequested {
			return fmt.Errorf("V14 commute lifecycle mobility demand fact is invalid")
		}
	}
	return nil
}

// validateCityOpenWorldInfrastructureCheckpoint proves that V20's mutable
// projection has no independent state path: every non-baseline transition is
// backed by one runtime fact, and every infrastructure transition fact is
// consumed by exactly one asset transition. V21 is the only successor allowed
// to make the fact visible to V9; that visibility starts on the next tick.
func validateCityOpenWorldInfrastructureCheckpoint(tick int64, runtime *cityOpenWorldRuntimeHashState) error {
	if runtime == nil || runtime.Infrastructure == nil {
		return fmt.Errorf("open-world infrastructure checkpoint is unavailable")
	}
	if err := validateCityOpenWorldInfrastructureState(runtime.Infrastructure); err != nil {
		return fmt.Errorf("invalid V20 infrastructure state: %w", err)
	}
	assets := make(map[string]CityOpenWorldInfrastructureAsset, len(runtime.Infrastructure.Assets))
	for _, asset := range runtime.Infrastructure.Assets {
		assets[asset.Code] = asset
	}
	facts := make(map[CityOpenWorldRuntimeFactRef]CityOpenWorldRuntimeFact, len(runtime.Facts))
	for _, fact := range runtime.Facts {
		facts[CityOpenWorldRuntimeFactRef{Tick: fact.Tick, Sequence: fact.Sequence}] = fact
	}
	consumedFacts := make(map[CityOpenWorldRuntimeFactRef]struct{})
	for _, transition := range runtime.Infrastructure.Transitions {
		if transition.SourceFact == nil {
			continue
		}
		fact, found := facts[*transition.SourceFact]
		asset, assetFound := assets[transition.AssetCode]
		if !found || !assetFound || fact.FactType != cityOpenWorldInfrastructureFactAssetTransition ||
			fact.Tick != transition.TransitionTick || fact.Sequence != transition.TransitionSeq ||
			fact.Tick > tick {
			return fmt.Errorf("V20 infrastructure transition fact is invalid")
		}
		var payload struct {
			AssetCode                    string `json:"asset_code"`
			AssetKind                    string `json:"asset_kind"`
			FromState                    string `json:"from_state"`
			ToState                      string `json:"to_state"`
			CapacityMilli                int64  `json:"capacity_milli"`
			ReasonCode                   string `json:"reason_code"`
			V9SchedulerEffect            string `json:"v9_scheduler_effect"`
			V9SchedulerEffectiveFromTick int64  `json:"v9_scheduler_effective_from_tick"`
		}
		expectedSchedulerEffect := "none"
		expectedSchedulerEffectiveFromTick := int64(0)
		if runtime.EffectiveCapacity != nil && transition.TransitionTick > runtime.EffectiveCapacity.Policy.BaselineTick {
			expectedSchedulerEffect = cityOpenWorldEffectiveCapacitySchedulerEffect
			expectedSchedulerEffectiveFromTick = transition.TransitionTick + 1
		}
		if err := json.Unmarshal(fact.Payload, &payload); err != nil ||
			payload.AssetCode != transition.AssetCode || payload.AssetKind != asset.AssetKind ||
			payload.FromState != transition.FromState || payload.ToState != transition.ToState ||
			payload.CapacityMilli != transition.CapacityMilli || payload.ReasonCode != transition.ReasonCode ||
			payload.V9SchedulerEffect != expectedSchedulerEffect ||
			payload.V9SchedulerEffectiveFromTick != expectedSchedulerEffectiveFromTick {
			return fmt.Errorf("V20 infrastructure transition payload is invalid")
		}
		if _, duplicate := consumedFacts[*transition.SourceFact]; duplicate {
			return fmt.Errorf("V20 infrastructure runtime fact is reused")
		}
		consumedFacts[*transition.SourceFact] = struct{}{}
	}
	for _, fact := range runtime.Facts {
		if fact.FactType != cityOpenWorldInfrastructureFactAssetTransition {
			continue
		}
		identity := CityOpenWorldRuntimeFactRef{Tick: fact.Tick, Sequence: fact.Sequence}
		if _, found := consumedFacts[identity]; !found {
			return fmt.Errorf("V20 infrastructure runtime fact is not projected")
		}
	}
	return nil
}

// validateCityOpenWorldSupplyChainCheckpoint proves that a V15 checkpoint
// contains no future evidence and that every mutable lifecycle artifact is
// causally anchored to a supply-chain fact already visible at this tick. F2
// journal and F3 resource-operation cursors are validated as temporal facts
// here; replay has independently applied their entries before this stage.
func validateCityOpenWorldSupplyChainCheckpoint(tick int64, runtime *cityOpenWorldRuntimeHashState) error {
	if runtime == nil || runtime.SupplyChain == nil {
		return fmt.Errorf("open-world supply-chain checkpoint is unavailable")
	}
	supplyChain := runtime.SupplyChain
	if err := validateCityOpenWorldSupplyChainState(supplyChain); err != nil {
		return fmt.Errorf("invalid V15 supply-chain state: %w", err)
	}
	facts := make(map[CityOpenWorldRuntimeFactRef]CityOpenWorldSupplyChainFact, len(supplyChain.Facts))
	for _, fact := range supplyChain.Facts {
		if fact.Tick > tick {
			return fmt.Errorf("V15 supply-chain fact is ahead of replay tick")
		}
		facts[CityOpenWorldRuntimeFactRef{Tick: fact.Tick, Sequence: fact.Sequence}] = fact
	}
	requireFact := func(reference CityOpenWorldRuntimeFactRef, label string) error {
		if reference.Tick <= 0 || reference.Sequence <= 0 || reference.Tick > tick {
			return fmt.Errorf("V15 supply-chain %s has an invalid fact cursor", label)
		}
		if _, found := facts[reference]; !found {
			return fmt.Errorf("V15 supply-chain %s fact is unavailable", label)
		}
		return nil
	}
	for _, order := range supplyChain.Orders {
		if order.CreatedTick > tick {
			return fmt.Errorf("V15 supply-chain order is ahead of replay tick")
		}
		if err := requireFact(order.CreatedFact, "order creation"); err != nil {
			return err
		}
	}
	for _, transition := range supplyChain.Transitions {
		if transition.TransitionTick > tick {
			return fmt.Errorf("V15 supply-chain transition is ahead of replay tick")
		}
		if err := requireFact(transition.SourceFact, "transition"); err != nil {
			return err
		}
	}
	for _, reservation := range supplyChain.Reservations {
		if reservation.ReservedTick > tick {
			return fmt.Errorf("V15 supply-chain reservation is ahead of replay tick")
		}
		if err := requireFact(reservation.SourceFact, "reservation"); err != nil {
			return err
		}
	}
	for _, release := range supplyChain.Releases {
		if release.ReleasedTick > tick {
			return fmt.Errorf("V15 supply-chain release is ahead of replay tick")
		}
		if err := requireFact(release.SourceFact, "reservation release"); err != nil {
			return err
		}
	}
	for _, dispatch := range supplyChain.Dispatches {
		if dispatch.DispatchedTick > tick {
			return fmt.Errorf("V15 supply-chain dispatch is ahead of replay tick")
		}
		if err := requireFact(dispatch.SourceFact, "dispatch"); err != nil {
			return err
		}
	}
	for _, delivery := range supplyChain.Deliveries {
		if delivery.DeliveredTick > tick || delivery.ResourceOperation.Tick <= 0 ||
			delivery.ResourceOperation.Sequence <= 0 || delivery.ResourceOperation.Tick > tick {
			return fmt.Errorf("V15 supply-chain delivery is ahead of replay tick")
		}
		if err := requireFact(delivery.SourceFact, "delivery"); err != nil {
			return err
		}
	}
	for _, settlement := range supplyChain.Settlements {
		if settlement.Journal.Tick <= 0 || settlement.Journal.Sequence <= 0 || settlement.Journal.Tick > tick {
			return fmt.Errorf("V15 supply-chain settlement journal is ahead of replay tick")
		}
		if err := requireFact(settlement.SourceFact, "settlement"); err != nil {
			return err
		}
	}
	return nil
}

// validateCityOpenWorldEnterpriseFreightCheckpoint proves the V16 adapter is
// a one-way view of older facts. It deliberately validates linkage only: a
// scheduled or completed route must never be interpreted here as a receipt,
// inventory movement, settlement, or local actor relocation.
func validateCityOpenWorldEnterpriseFreightCheckpoint(tick int64, runtime *cityOpenWorldRuntimeHashState) error {
	if runtime == nil || runtime.EnterpriseFreight == nil || runtime.SupplyChain == nil || runtime.Mobility == nil {
		return fmt.Errorf("open-world enterprise-freight checkpoint is unavailable")
	}
	freight := runtime.EnterpriseFreight
	if err := validateCityOpenWorldEnterpriseFreightState(freight); err != nil {
		return fmt.Errorf("invalid V16 enterprise-freight state: %w", err)
	}
	runtimeFacts := make(map[CityOpenWorldRuntimeFactRef]CityOpenWorldRuntimeFact, len(runtime.Facts))
	for _, fact := range runtime.Facts {
		if fact.Tick > tick {
			return fmt.Errorf("V16 enterprise-freight runtime fact is ahead of replay tick")
		}
		runtimeFacts[CityOpenWorldRuntimeFactRef{Tick: fact.Tick, Sequence: fact.Sequence}] = fact
	}
	supplyFacts := make(map[CityOpenWorldRuntimeFactRef]CityOpenWorldSupplyChainFact, len(runtime.SupplyChain.Facts))
	for _, fact := range runtime.SupplyChain.Facts {
		if fact.Tick > tick {
			return fmt.Errorf("V16 enterprise-freight dispatch evidence is ahead of replay tick")
		}
		supplyFacts[CityOpenWorldRuntimeFactRef{Tick: fact.Tick, Sequence: fact.Sequence}] = fact
	}
	demands := make(map[string]CityOpenWorldMobilityDemand, len(runtime.Mobility.Demands))
	for _, demand := range runtime.Mobility.Demands {
		if demand.RequestedTick > tick {
			return fmt.Errorf("V16 enterprise-freight mobility demand is ahead of replay tick")
		}
		demands[demand.Code] = demand
	}
	routes := make(map[string]CityOpenWorldMobilityRoute, len(runtime.Mobility.Routes))
	for _, route := range runtime.Mobility.Routes {
		if route.DepartureTick > tick || route.ArrivalTick > tick+cityOpenWorldMobilityMaximumWaitTicks {
			return fmt.Errorf("V16 enterprise-freight mobility route is temporally invalid")
		}
		routes[route.Code] = route
	}
	requireRuntimeFact := func(reference CityOpenWorldRuntimeFactRef, label string) (CityOpenWorldRuntimeFact, error) {
		if reference.Tick <= 0 || reference.Sequence <= 0 || reference.Tick > tick {
			return CityOpenWorldRuntimeFact{}, fmt.Errorf("V16 enterprise-freight %s has an invalid runtime fact cursor", label)
		}
		fact, found := runtimeFacts[reference]
		if !found {
			return CityOpenWorldRuntimeFact{}, fmt.Errorf("V16 enterprise-freight %s runtime fact is unavailable", label)
		}
		return fact, nil
	}
	for _, source := range freight.Sources {
		if source.DispatchTick > tick || source.SourceTick > tick || source.MobilityDeadlineTick <= source.SourceTick {
			return fmt.Errorf("V16 enterprise-freight source is temporally invalid")
		}
		dispatch, found := supplyFacts[source.DispatchFact]
		if !found || dispatch.FactType != "order.dispatched" || dispatch.OrderCode == nil || *dispatch.OrderCode != source.OrderCode {
			return fmt.Errorf("V16 enterprise-freight source dispatch evidence is invalid")
		}
		root, err := requireRuntimeFact(source.SourceFact, "source root")
		if err != nil || root.FactType != cityOpenWorldRuntimeFactEnterpriseFreightSourceCreated {
			return fmt.Errorf("V16 enterprise-freight source root is invalid")
		}
		if _, err = requireRuntimeFact(source.LastFact, "source last"); err != nil {
			return err
		}
		if source.DemandCode == nil {
			continue
		}
		demand, found := demands[*source.DemandCode]
		if !found || demand.ActorCode != cityOpenWorldEnterpriseFreightCarrierActorCode ||
			demand.SourceHubCode != source.SourceHubCode || demand.DestinationHubCode != source.DestinationHubCode ||
			demand.ModeCode != cityOpenWorldEnterpriseFreightModeCode || demand.PurposeCode != cityOpenWorldEnterpriseFreightPurposeCode ||
			demand.RequestedUnits != source.RequestedUnits {
			return fmt.Errorf("V16 enterprise-freight source demand is invalid")
		}
		requestFact, err := requireRuntimeFact(demand.SourceFact, "mobility demand")
		if err != nil || requestFact.FactType != CityOpenWorldRuntimeFactMobilityRequested {
			return fmt.Errorf("V16 enterprise-freight demand fact is invalid")
		}
		var metadata map[string]any
		if err = json.Unmarshal(demand.Metadata, &metadata); err != nil {
			return fmt.Errorf("decode V16 enterprise-freight demand metadata: %w", err)
		}
		adapter, valid := metadata["transport_adapter"].(map[string]any)
		if !valid || adapter["kind"] != "enterprise_freight_v1" || adapter["source_code"] != source.Code ||
			adapter["arrival_bridge"] != "excluded" {
			return fmt.Errorf("V16 enterprise-freight demand adapter metadata is invalid")
		}
		if source.RouteCode != nil {
			route, found := routes[*source.RouteCode]
			if !found || route.DemandCode != demand.Code || route.ActorCode != cityOpenWorldEnterpriseFreightCarrierActorCode ||
				route.SourceHubCode != source.SourceHubCode || route.DestinationHubCode != source.DestinationHubCode ||
				route.ModeCode != cityOpenWorldEnterpriseFreightModeCode {
				return fmt.Errorf("V16 enterprise-freight source route is invalid")
			}
		}
	}
	expectedRuntimeFactType := map[string]string{
		"source.created":     cityOpenWorldRuntimeFactEnterpriseFreightSourceCreated,
		"source.suppressed":  cityOpenWorldRuntimeFactEnterpriseFreightSourceSuppressed,
		"demand.requested":   CityOpenWorldRuntimeFactMobilityRequested,
		"route.scheduled":    cityOpenWorldRuntimeFactEnterpriseFreightRouteScheduled,
		"route.completed":    cityOpenWorldRuntimeFactEnterpriseFreightRouteCompleted,
		"demand.expired":     cityOpenWorldRuntimeFactEnterpriseFreightDemandExpired,
		"demand.voided":      CityOpenWorldRuntimeFactMobilityExpired,
		"transport.orphaned": cityOpenWorldRuntimeFactEnterpriseFreightTransportOrphaned,
	}
	for _, fact := range freight.Facts {
		if fact.Tick > tick {
			return fmt.Errorf("V16 enterprise-freight fact is ahead of replay tick")
		}
		runtimeFact, err := requireRuntimeFact(fact.RuntimeFact, "adapter fact")
		if err != nil || runtimeFact.FactType != expectedRuntimeFactType[fact.FactType] {
			return fmt.Errorf("V16 enterprise-freight adapter fact is invalid")
		}
	}
	return nil
}
