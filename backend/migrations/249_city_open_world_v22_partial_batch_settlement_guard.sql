-- A V18 plan can settle one consignment before its remaining consignments
-- finish. The V22 case is then settled, but the V15 order and its V17/V18
-- custody evidence cannot be terminally settled until every case has an
-- immutable outcome and the single order.settled fact exists. Preserve the
-- V22.1 assertion in full while correcting that order-level boundary.

CREATE OR REPLACE FUNCTION assert_city_open_world_freight_settlement_foundation(target_world_id BIGINT)
RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE
    world_version TEXT;
    world_tick BIGINT;
    profile_tick BIGINT;
    profile_orders BIGINT;
    profile_cases BIGINT;
    profile_receipts BIGINT;
    profile_claims BIGINT;
    profile_accepted BIGINT;
    profile_lost BIGINT;
    profile_rejected BIGINT;
    profile_refunded BIGINT;
BEGIN
    SELECT simulation_version, current_tick INTO world_version, world_tick
    FROM city_worlds WHERE id = target_world_id;
    IF world_version NOT IN ('city-openworld-v22','city-openworld-v23','city-openworld-v24') THEN RETURN; END IF;

    IF NOT EXISTS (SELECT 1 FROM city_open_world_supply_chain_profiles WHERE world_id = target_world_id)
       OR NOT EXISTS (SELECT 1 FROM city_open_world_enterprise_freight_receipt_profiles WHERE world_id = target_world_id)
       OR NOT EXISTS (SELECT 1 FROM city_open_world_freight_batch_profiles WHERE world_id = target_world_id) THEN
        RAISE EXCEPTION 'city open-world V22 freight-settlement predecessor foundations are missing' USING ERRCODE = '23514';
    END IF;

    SELECT baseline_tick, order_count, case_count, receipt_count, claim_count,
           accepted_units, lost_units, rejected_units, refunded_units
      INTO profile_tick, profile_orders, profile_cases, profile_receipts, profile_claims,
           profile_accepted, profile_lost, profile_rejected, profile_refunded
    FROM city_open_world_freight_settlement_profiles
    WHERE world_id = target_world_id;
    IF profile_tick IS NULL OR profile_tick > world_tick
       OR profile_orders <> (SELECT COUNT(*) FROM city_open_world_freight_settlement_orders WHERE world_id = target_world_id)
       OR profile_cases <> (SELECT COUNT(*) FROM city_open_world_freight_settlement_cases WHERE world_id = target_world_id)
       OR profile_receipts <> (SELECT COUNT(*) FROM city_open_world_freight_settlement_receipts WHERE world_id = target_world_id)
       OR profile_claims <> (SELECT COUNT(*) FROM city_open_world_freight_settlement_claims WHERE world_id = target_world_id)
       OR profile_accepted <> COALESCE((SELECT SUM(accepted_units) FROM city_open_world_freight_settlement_receipt_lines WHERE world_id = target_world_id), 0)
       OR profile_lost <> COALESCE((SELECT SUM(lost_units) FROM city_open_world_freight_settlement_receipt_lines WHERE world_id = target_world_id), 0)
       OR profile_rejected <> COALESCE((SELECT SUM(rejected_units) FROM city_open_world_freight_settlement_receipt_lines WHERE world_id = target_world_id), 0)
       OR profile_refunded <> COALESCE((SELECT SUM(refunded_units) FROM city_open_world_freight_settlement_receipts WHERE world_id = target_world_id), 0) THEN
        RAISE EXCEPTION 'city open-world V22 freight-settlement profile is missing or inconsistent' USING ERRCODE = '23514';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM city_open_world_freight_settlement_orders settlement_order
        LEFT JOIN LATERAL (
            SELECT COUNT(*) AS case_count,
                   COUNT(*) FILTER (WHERE state = 'settled') AS settled_count,
                   COUNT(*) FILTER (WHERE state = 'voided') AS voided_count,
                   COUNT(*) FILTER (WHERE state = 'awaiting_outcome') AS awaiting_count,
                   BOOL_AND(source_tick = settlement_order.source_tick) AS source_tick_matches
            FROM city_open_world_freight_settlement_cases settlement_case
            WHERE settlement_case.world_id = settlement_order.world_id
              AND settlement_case.settlement_order_code = settlement_order.code
        ) cases ON TRUE
        WHERE settlement_order.world_id = target_world_id
          AND (settlement_order.source_tick <= profile_tick
               OR cases.case_count = 0
               OR NOT cases.source_tick_matches
               OR (settlement_order.state = 'settled' AND cases.settled_count <> cases.case_count)
               OR (settlement_order.state = 'voided' AND cases.voided_count <> cases.case_count)
               OR (settlement_order.state = 'blocked' AND cases.awaiting_count <> cases.case_count))
    ) THEN
        RAISE EXCEPTION 'city open-world V22 freight-settlement order linkage is invalid' USING ERRCODE = '23514';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM city_open_world_freight_settlement_cases settlement_case
        JOIN city_open_world_freight_settlement_orders settlement_order
          ON settlement_order.world_id = settlement_case.world_id
         AND settlement_order.code = settlement_case.settlement_order_code
        WHERE settlement_case.world_id = target_world_id
          AND (settlement_case.source_tick <= profile_tick
               OR settlement_case.source_kind <> settlement_order.source_kind
               OR (settlement_case.source_kind = 'shipment' AND settlement_case.source_code <> settlement_order.source_code))
    ) THEN
        RAISE EXCEPTION 'city open-world V22 freight-settlement case source linkage is invalid' USING ERRCODE = '23514';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM city_open_world_freight_settlement_receipt_lines line
        JOIN city_open_world_freight_settlement_case_lines case_line
          ON case_line.world_id = line.world_id
         AND case_line.case_code = line.case_code
         AND case_line.source_line_no = line.source_line_no
        WHERE line.world_id = target_world_id
        GROUP BY line.world_id, line.case_code, line.source_line_no, case_line.quantity_units
        HAVING SUM(line.accepted_units + line.lost_units + line.rejected_units) > MAX(case_line.quantity_units)
    ) THEN
        RAISE EXCEPTION 'city open-world V22 freight-settlement receipt line exceeds the source quantity' USING ERRCODE = '23514';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM city_open_world_freight_settlement_cases settlement_case
        JOIN city_open_world_freight_settlement_case_lines case_line
          ON case_line.world_id = settlement_case.world_id
         AND case_line.case_code = settlement_case.code
        LEFT JOIN (
            SELECT world_id, case_code, source_line_no,
                   SUM(accepted_units + lost_units + rejected_units) AS resolved_units
            FROM city_open_world_freight_settlement_receipt_lines
            WHERE world_id = target_world_id
            GROUP BY world_id, case_code, source_line_no
        ) outcomes
          ON outcomes.world_id = case_line.world_id
         AND outcomes.case_code = case_line.case_code
         AND outcomes.source_line_no = case_line.source_line_no
        LEFT JOIN LATERAL (
            SELECT COUNT(*) AS receipt_count
            FROM city_open_world_freight_settlement_receipts receipt
            WHERE receipt.world_id = settlement_case.world_id
              AND receipt.case_code = settlement_case.code
        ) receipts ON TRUE
        WHERE settlement_case.world_id = target_world_id
        GROUP BY settlement_case.world_id, settlement_case.code, settlement_case.state, receipts.receipt_count
        HAVING (MAX(settlement_case.state) = 'settled') IS DISTINCT FROM
                   BOOL_AND(COALESCE(outcomes.resolved_units, 0) = case_line.quantity_units)
            OR (MAX(settlement_case.state) = 'awaiting_outcome' AND
                   NOT BOOL_AND(COALESCE(outcomes.resolved_units, 0) = 0))
            OR (MAX(settlement_case.state) = 'receiving' AND
                   (NOT BOOL_OR(COALESCE(outcomes.resolved_units, 0) > 0)
                    OR BOOL_AND(COALESCE(outcomes.resolved_units, 0) = case_line.quantity_units)))
            OR (MAX(settlement_case.state) = 'voided' AND
                   (receipts.receipt_count <> 0 OR NOT BOOL_AND(COALESCE(outcomes.resolved_units, 0) = 0)))
    ) THEN
        RAISE EXCEPTION 'city open-world V22 freight-settlement case outcomes are incomplete' USING ERRCODE = '23514';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM city_open_world_freight_settlement_cases settlement_case
        JOIN city_open_world_freight_settlement_orders settlement_order
          ON settlement_order.world_id = settlement_case.world_id
         AND settlement_order.code = settlement_case.settlement_order_code
        LEFT JOIN city_open_world_enterprise_freight_shipments shipment
          ON shipment.world_id = settlement_case.world_id
         AND shipment.code = settlement_case.source_code
         AND settlement_case.source_kind = 'shipment'
        LEFT JOIN city_open_world_freight_batch_consignments consignment
          ON consignment.world_id = settlement_case.world_id
         AND consignment.code = settlement_case.source_code
         AND settlement_case.source_kind = 'consignment'
        LEFT JOIN city_open_world_freight_batch_plans plan
          ON plan.world_id = consignment.world_id AND plan.code = consignment.plan_code
        WHERE settlement_case.world_id = target_world_id
          AND ((settlement_case.source_kind = 'shipment' AND
                (shipment.code IS NULL OR settlement_order.source_code <> settlement_case.source_code
                 OR shipment.order_code <> settlement_order.order_code
                 OR (settlement_order.state = 'settled' AND shipment.state <> 'settled')
                 OR (settlement_order.state <> 'settled' AND shipment.state <> settlement_case.transport_state)))
               OR (settlement_case.source_kind = 'consignment' AND
                (consignment.code IS NULL OR plan.code <> settlement_order.source_code
                 OR plan.order_code <> settlement_order.order_code
                 OR (settlement_order.state = 'settled' AND consignment.state <> 'settled')
                 OR (settlement_order.state <> 'settled' AND consignment.state <> settlement_case.transport_state))))
    ) THEN
        RAISE EXCEPTION 'city open-world V22 freight-settlement custody linkage is invalid' USING ERRCODE = '23514';
    END IF;

    -- V15 may be settled only after all cases complete, failed only after the
    -- explicit zero-receipt void, and otherwise remains dispatched. V22 never
    -- allows the old atomic delivery row to coexist with its successor path.
    IF EXISTS (
        SELECT 1
        FROM city_open_world_freight_settlement_orders settlement_order
        JOIN LATERAL (
            SELECT transition.state
            FROM city_open_world_supply_chain_order_transitions transition
            WHERE transition.world_id = settlement_order.world_id
              AND transition.order_code = settlement_order.order_code
            ORDER BY transition.transition_tick DESC, transition.transition_sequence DESC
            LIMIT 1
        ) supply_state ON TRUE
        WHERE settlement_order.world_id = target_world_id
          AND ((settlement_order.state = 'settled' AND supply_state.state <> 'settled')
               OR (settlement_order.state = 'voided' AND supply_state.state <> 'failed')
               OR (settlement_order.state NOT IN ('settled','voided') AND supply_state.state <> 'dispatched')
               OR (settlement_order.state = 'voided' AND EXISTS (
                    SELECT 1
                    FROM city_open_world_freight_settlement_receipts receipt
                    JOIN city_open_world_freight_settlement_cases settlement_case
                      ON settlement_case.world_id = receipt.world_id
                     AND settlement_case.code = receipt.case_code
                    WHERE receipt.world_id = settlement_order.world_id
                      AND settlement_case.settlement_order_code = settlement_order.code
               ))
               OR EXISTS (
                    SELECT 1 FROM city_open_world_supply_chain_deliveries delivery
                    WHERE delivery.world_id = settlement_order.world_id
                      AND delivery.order_code = settlement_order.order_code
               ))
    ) THEN
        RAISE EXCEPTION 'city open-world V22 settlement must own the V15 terminal state' USING ERRCODE = '23514';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM city_open_world_freight_settlement_orders settlement_order
        WHERE settlement_order.world_id = target_world_id
          AND settlement_order.source_kind = 'consignment'
          AND settlement_order.state = 'settled'
          AND ((SELECT COUNT(*) FROM city_open_world_freight_settlement_cases settlement_case
                WHERE settlement_case.world_id = settlement_order.world_id
                  AND settlement_case.settlement_order_code = settlement_order.code)
               <> (SELECT COUNT(*) FROM city_open_world_freight_batch_consignments consignment
                   WHERE consignment.world_id = settlement_order.world_id
                     AND consignment.plan_code = settlement_order.source_code))
    ) THEN
        RAISE EXCEPTION 'city open-world V22 batch settlement omitted a consignment' USING ERRCODE = '23514';
    END IF;

    -- Settled V17 shipments must carry a V22 supply-chain proof. A voided V22
    -- case intentionally does not create a V17/V18 settlement adapter fact.
    IF EXISTS (
        SELECT 1
        FROM city_open_world_enterprise_freight_shipments shipment
        LEFT JOIN city_open_world_enterprise_freight_receipt_facts last_fact
          ON last_fact.id = shipment.last_receipt_fact_id AND last_fact.world_id = shipment.world_id
        LEFT JOIN city_open_world_supply_chain_facts settlement_fact
          ON settlement_fact.id = last_fact.supply_chain_fact_id AND settlement_fact.world_id = shipment.world_id
        WHERE shipment.world_id = target_world_id
          AND shipment.state = 'settled'
          AND (last_fact.id IS NULL
               OR last_fact.fact_type <> 'settlement.confirmed'
               OR last_fact.evidence_kind <> 'supply_chain'
               OR settlement_fact.fact_type <> 'order.settled'
               OR settlement_fact.order_code <> shipment.order_code
               OR NOT EXISTS (
                    SELECT 1
                    FROM city_open_world_enterprise_freight_shipment_transitions transition
                    WHERE transition.world_id = shipment.world_id
                      AND transition.shipment_code = shipment.code
                      AND transition.state = 'settled'
                      AND transition.reason_code = 'v22_freight_settlement_completed'
                      AND transition.source_fact_id = last_fact.id
               ))
    ) THEN
        RAISE EXCEPTION 'city open-world V22 V17 settlement proof is invalid' USING ERRCODE = '23514';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM city_open_world_freight_batch_consignments consignment
        JOIN city_open_world_freight_batch_plans plan
          ON plan.world_id = consignment.world_id AND plan.code = consignment.plan_code
        LEFT JOIN city_open_world_freight_batch_facts last_runtime_fact
          ON last_runtime_fact.world_id = consignment.world_id
         AND last_runtime_fact.consignment_code = consignment.code
         AND last_runtime_fact.runtime_fact_id = consignment.last_runtime_fact_id
        WHERE consignment.world_id = target_world_id
          AND consignment.state = 'settled'
          AND (last_runtime_fact.id IS NULL
               OR last_runtime_fact.evidence_kind <> 'runtime'
               OR last_runtime_fact.fact_type NOT IN ('route.completed','demand.expired','demand.voided','transport.orphaned')
               OR EXISTS (
                    SELECT 1 FROM city_open_world_freight_batch_receipts receipt
                    WHERE receipt.world_id = consignment.world_id AND receipt.consignment_code = consignment.code
               )
               OR NOT EXISTS (
                    SELECT 1
                    FROM city_open_world_freight_batch_transitions transition
                    JOIN city_open_world_freight_batch_facts settlement_adapter_fact
                      ON settlement_adapter_fact.id = transition.source_fact_id
                     AND settlement_adapter_fact.world_id = transition.world_id
                    JOIN city_open_world_supply_chain_facts settlement_fact
                      ON settlement_fact.id = settlement_adapter_fact.supply_chain_fact_id
                     AND settlement_fact.world_id = transition.world_id
                    WHERE transition.world_id = consignment.world_id
                      AND transition.consignment_code = consignment.code
                      AND transition.state = 'settled'
                      AND transition.reason_code = 'v22_freight_settlement_completed'
                      AND settlement_adapter_fact.fact_type = 'settlement.confirmed'
                      AND settlement_adapter_fact.evidence_kind = 'supply_chain'
                      AND settlement_fact.fact_type = 'order.settled'
                      AND settlement_fact.order_code = plan.order_code
               ))
    ) THEN
        RAISE EXCEPTION 'city open-world V22 V18 settlement proof is invalid' USING ERRCODE = '23514';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM city_open_world_freight_batch_plans plan
        WHERE plan.world_id = target_world_id
          AND plan.state IS DISTINCT FROM CASE
              WHEN EXISTS (
                  SELECT 1 FROM city_open_world_freight_batch_consignments consignment
                  WHERE consignment.world_id = plan.world_id AND consignment.plan_code = plan.code
                    AND consignment.state IN ('expired','voided','orphaned')
              ) THEN 'blocked'
              WHEN NOT EXISTS (
                  SELECT 1 FROM city_open_world_freight_batch_consignments consignment
                  WHERE consignment.world_id = plan.world_id AND consignment.plan_code = plan.code
                    AND consignment.state <> 'settled'
              ) THEN 'settled'
              WHEN NOT EXISTS (
                  SELECT 1 FROM city_open_world_freight_batch_consignments consignment
                  WHERE consignment.world_id = plan.world_id AND consignment.plan_code = plan.code
                    AND consignment.state <> 'received'
              ) THEN 'received'
              WHEN NOT EXISTS (
                  SELECT 1 FROM city_open_world_freight_batch_consignments consignment
                  WHERE consignment.world_id = plan.world_id AND consignment.plan_code = plan.code
                    AND consignment.state <> 'awaiting_receipt'
              ) THEN 'ready'
              ELSE 'active'
          END
    ) THEN
        RAISE EXCEPTION 'city open-world V22 V18 batch plan state is invalid' USING ERRCODE = '23514';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM city_open_world_freight_settlement_receipts receipt
        JOIN city_open_world_freight_settlement_cases settlement_case
          ON settlement_case.world_id = receipt.world_id AND settlement_case.code = receipt.case_code
        LEFT JOIN (
            SELECT line.world_id, line.receipt_code,
                   SUM(line.accepted_units + line.lost_units) AS outbound_units,
                   SUM((line.lost_units + line.rejected_units)::NUMERIC * case_line.unit_price_units::NUMERIC) AS refund_amount
            FROM city_open_world_freight_settlement_receipt_lines line
            JOIN city_open_world_freight_settlement_case_lines case_line
              ON case_line.world_id = line.world_id AND case_line.case_code = line.case_code
             AND case_line.source_line_no = line.source_line_no
            WHERE line.world_id = target_world_id
            GROUP BY line.world_id, line.receipt_code
        ) receipt_totals
          ON receipt_totals.world_id = receipt.world_id AND receipt_totals.receipt_code = receipt.code
        WHERE receipt.world_id = target_world_id
          AND (((COALESCE(receipt_totals.outbound_units, 0) > 0) <> (receipt.resource_operation_id IS NOT NULL))
               OR (receipt.refunded_units::NUMERIC <> COALESCE(receipt_totals.refund_amount, 0))
               OR ((receipt.refunded_units > 0) <> (receipt.journal_id IS NOT NULL))
               OR (receipt.liability_party = 'seller' AND EXISTS (
                    SELECT 1 FROM city_open_world_freight_settlement_claims claim
                    WHERE claim.world_id = receipt.world_id AND claim.receipt_code = receipt.code
               ))
               OR (receipt.liability_party = 'carrier' AND receipt.refunded_units > 0 AND NOT EXISTS (
                    SELECT 1 FROM city_open_world_freight_settlement_claims claim
                    WHERE claim.world_id = receipt.world_id AND claim.receipt_code = receipt.code
                      AND claim.liability_party = 'carrier' AND claim.claim_amount = receipt.refunded_units
               ))
               OR (receipt.liability_party = 'carrier' AND receipt.refunded_units = 0 AND EXISTS (
                    SELECT 1 FROM city_open_world_freight_settlement_claims claim
                    WHERE claim.world_id = receipt.world_id AND claim.receipt_code = receipt.code
               )))
    ) THEN
        RAISE EXCEPTION 'city open-world V22 freight-settlement financial or resource evidence is invalid' USING ERRCODE = '23514';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM city_open_world_enterprise_freight_receipt_profiles profile
        WHERE profile.world_id = target_world_id
          AND (profile.shipment_count <> (SELECT COUNT(*) FROM city_open_world_enterprise_freight_shipments WHERE world_id = target_world_id)
               OR profile.awaiting_route_count <> (SELECT COUNT(*) FROM city_open_world_enterprise_freight_shipments WHERE world_id = target_world_id AND state = 'awaiting_route')
               OR profile.in_transit_count <> (SELECT COUNT(*) FROM city_open_world_enterprise_freight_shipments WHERE world_id = target_world_id AND state = 'in_transit')
               OR profile.awaiting_receipt_count <> (SELECT COUNT(*) FROM city_open_world_enterprise_freight_shipments WHERE world_id = target_world_id AND state = 'awaiting_receipt')
               OR profile.received_count <> (SELECT COUNT(*) FROM city_open_world_enterprise_freight_shipments WHERE world_id = target_world_id AND state = 'received')
               OR profile.settled_count <> (SELECT COUNT(*) FROM city_open_world_enterprise_freight_shipments WHERE world_id = target_world_id AND state = 'settled')
               OR profile.expired_count <> (SELECT COUNT(*) FROM city_open_world_enterprise_freight_shipments WHERE world_id = target_world_id AND state = 'expired')
               OR profile.voided_count <> (SELECT COUNT(*) FROM city_open_world_enterprise_freight_shipments WHERE world_id = target_world_id AND state = 'voided')
               OR profile.orphaned_count <> (SELECT COUNT(*) FROM city_open_world_enterprise_freight_shipments WHERE world_id = target_world_id AND state = 'orphaned'))
    ) THEN
        RAISE EXCEPTION 'city open-world V22 V17 successor counters are inconsistent' USING ERRCODE = '23514';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM city_open_world_freight_batch_profiles profile
        WHERE profile.world_id = target_world_id
          AND (profile.consignment_count <> (SELECT COUNT(*) FROM city_open_world_freight_batch_consignments WHERE world_id = target_world_id)
               OR profile.awaiting_route_count <> (SELECT COUNT(*) FROM city_open_world_freight_batch_consignments WHERE world_id = target_world_id AND state = 'awaiting_route')
               OR profile.in_transit_count <> (SELECT COUNT(*) FROM city_open_world_freight_batch_consignments WHERE world_id = target_world_id AND state = 'in_transit')
               OR profile.awaiting_receipt_count <> (SELECT COUNT(*) FROM city_open_world_freight_batch_consignments WHERE world_id = target_world_id AND state = 'awaiting_receipt')
               OR profile.received_count <> (SELECT COUNT(*) FROM city_open_world_freight_batch_consignments WHERE world_id = target_world_id AND state = 'received')
               OR profile.settled_count <> (SELECT COUNT(*) FROM city_open_world_freight_batch_consignments WHERE world_id = target_world_id AND state = 'settled')
               OR profile.expired_count <> (SELECT COUNT(*) FROM city_open_world_freight_batch_consignments WHERE world_id = target_world_id AND state = 'expired')
               OR profile.voided_count <> (SELECT COUNT(*) FROM city_open_world_freight_batch_consignments WHERE world_id = target_world_id AND state = 'voided')
               OR profile.orphaned_count <> (SELECT COUNT(*) FROM city_open_world_freight_batch_consignments WHERE world_id = target_world_id AND state = 'orphaned'))
    ) THEN
        RAISE EXCEPTION 'city open-world V22 V18 successor counters are inconsistent' USING ERRCODE = '23514';
    END IF;
END;
$$;

-- V18's projection trigger predates V22 and accepts only an atomic V15
-- order.delivered cursor for supply-chain evidence. A completed V22 batch
-- instead emits settlement.confirmed against the one order.settled cursor.
-- Keep every legacy receipt and transition guard unchanged while allowing
-- exactly that successor fact pair.
CREATE OR REPLACE FUNCTION guard_city_open_world_freight_batch_projection()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    target_world_id BIGINT;
    row_data JSONB;
    plan_source_code VARCHAR(160);
    plan_order_code VARCHAR(160);
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_recovery_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'DELETE' OR NOT city_open_world_freight_batch_write_enabled(target_world_id) THEN
        RAISE EXCEPTION 'open-world V18 freight-batch projections require their audited write context'
            USING ERRCODE = '55000';
    END IF;
    -- One guarded function serves rows with deliberately different schemas.
    -- JSONB access prevents PostgreSQL from resolving a non-existent column
    -- while compiling this trigger for a line or receipt table.
    row_data := to_jsonb(NEW);
    IF TG_TABLE_NAME = 'city_open_world_freight_batch_plans' THEN
        SELECT source.code, source.order_code
          INTO plan_source_code, plan_order_code
        FROM city_open_world_enterprise_freight_sources source
        JOIN city_open_world_freight_batch_profiles profile
          ON profile.world_id = source.world_id
        WHERE source.world_id = target_world_id
          AND source.code = row_data->>'overflow_source_code'
          AND source.state = 'suppressed'
          AND source.source_tick > profile.baseline_tick
          AND source.requested_units > profile.maximum_units;
        IF plan_source_code IS DISTINCT FROM row_data->>'overflow_source_code'
           OR plan_order_code IS DISTINCT FROM row_data->>'order_code' THEN
            RAISE EXCEPTION 'open-world V18 freight-batch plan must root in a post-baseline V16 overflow source'
                USING ERRCODE = '23514';
        END IF;
    ELSIF TG_TABLE_NAME = 'city_open_world_freight_batch_consignments' THEN
        IF NOT EXISTS (
            SELECT 1
            FROM city_open_world_freight_batch_plans plan
            JOIN city_open_world_mobility_demands demand
              ON demand.id = (row_data->>'mobility_demand_id')::BIGINT AND demand.world_id = plan.world_id
            WHERE plan.world_id = target_world_id AND plan.code = row_data->>'plan_code'
              AND demand.actor_id = plan.carrier_actor_id
              AND demand.source_hub_code = plan.source_hub_code
              AND demand.destination_hub_code = plan.destination_hub_code
              AND demand.mode_code = 'freight'
              AND demand.purpose_code = 'enterprise.freight_batch'
              AND demand.requested_units = (row_data->>'requested_units')::BIGINT
              AND demand.metadata->'transport_adapter'->>'kind' = 'enterprise_freight_batch_v1'
              AND demand.metadata->'transport_adapter'->>'plan_code' = plan.code
              AND demand.metadata->'transport_adapter'->>'consignment_code' = row_data->>'code'
        ) THEN
            RAISE EXCEPTION 'open-world V18 consignment must own its capacity-bounded V9 demand'
                USING ERRCODE = '23514';
        END IF;
    ELSIF TG_TABLE_NAME = 'city_open_world_freight_batch_facts' THEN
        IF row_data->>'evidence_kind' = 'runtime' AND NOT EXISTS (
            SELECT 1 FROM city_open_world_runtime_facts fact
            WHERE fact.id = (row_data->>'runtime_fact_id')::BIGINT AND fact.world_id = target_world_id
              AND fact.tick = (row_data->>'tick')::BIGINT AND fact.sequence = (row_data->>'sequence')::BIGINT
        ) THEN
            RAISE EXCEPTION 'open-world V18 runtime evidence must match its runtime fact cursor' USING ERRCODE = '23514';
        END IF;
        IF row_data->>'evidence_kind' = 'supply_chain' AND NOT EXISTS (
            SELECT 1 FROM city_open_world_supply_chain_facts fact
            WHERE fact.id = (row_data->>'supply_chain_fact_id')::BIGINT AND fact.world_id = target_world_id
              AND fact.tick = (row_data->>'tick')::BIGINT AND fact.sequence = (row_data->>'sequence')::BIGINT
              AND (
                  fact.fact_type = 'order.delivered'
                  OR (fact.fact_type = 'order.settled' AND row_data->>'fact_type' = 'settlement.confirmed')
              )
        ) THEN
            RAISE EXCEPTION 'open-world V18 receipt evidence must match its V15 delivery or V22 settlement fact cursor' USING ERRCODE = '23514';
        END IF;
    ELSIF TG_TABLE_NAME = 'city_open_world_freight_batch_transitions' AND NOT EXISTS (
        SELECT 1 FROM city_open_world_freight_batch_facts fact
        WHERE fact.id = (row_data->>'source_fact_id')::BIGINT AND fact.world_id = target_world_id
          AND fact.consignment_code = row_data->>'consignment_code'
          AND fact.tick = (row_data->>'transition_tick')::BIGINT
          AND fact.sequence = (row_data->>'transition_sequence')::BIGINT
    ) THEN
        RAISE EXCEPTION 'open-world V18 consignment transition must reference its batch fact cursor'
            USING ERRCODE = '23514';
    ELSIF TG_TABLE_NAME = 'city_open_world_freight_batch_receipts' THEN
        IF NOT EXISTS (
            SELECT 1
            FROM city_open_world_freight_batch_consignments consignment
            JOIN city_open_world_freight_batch_plans plan
              ON plan.world_id = consignment.world_id AND plan.code = consignment.plan_code
            JOIN city_open_world_freight_batch_facts fact
              ON fact.id = (row_data->>'source_fact_id')::BIGINT AND fact.world_id = consignment.world_id
            JOIN city_open_world_supply_chain_deliveries delivery
              ON delivery.id = (row_data->>'supply_chain_delivery_id')::BIGINT AND delivery.world_id = consignment.world_id
            WHERE consignment.world_id = target_world_id
              AND consignment.code = row_data->>'consignment_code'
              AND plan.code = row_data->>'plan_code'
              AND plan.order_code = row_data->>'order_code'
              AND delivery.order_code = plan.order_code
              AND fact.consignment_code = consignment.code
              AND fact.fact_type = 'receipt.confirmed'
              AND fact.evidence_kind = 'supply_chain'
        ) THEN
            RAISE EXCEPTION 'open-world V18 receipt must bind one batch consignment to its V15 delivery'
                USING ERRCODE = '23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
