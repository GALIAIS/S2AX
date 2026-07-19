package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
)

const cityOpenWorldV5NavigationMaximumSearchNodes = 512

type cityOpenWorldV5NavigationPortalEdge struct {
	portal *cityOpenWorldStaticPortal
	from   CityOpenWorldActorLocation
	to     CityOpenWorldActorLocation
}

func advanceCityOpenWorldV5NavigationIntents(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, effectSequence int64,
) (cityOpenWorldRuntimeAutomaticExecution, error) {
	execution := cityOpenWorldRuntimeAutomaticExecution{
		facts: make([]CityOpenWorldRuntimeFact, 0), effects: make([]CityOpenWorldRuntimeEffect, 0),
		events: make([]worldRuntimeAutomaticEvent, 0), nextFactSeq: factSequence, nextEffectSeq: effectSequence,
	}
	candidates, err := loadCityOpenWorldV5NavigationCandidates(ctx, tx, worldID, targetTick)
	if err != nil {
		return execution, err
	}
	if len(candidates) == 0 {
		return execution, nil
	}
	if err = activateCityOpenWorldRuntimeFactWrite(ctx, tx, worldID); err != nil {
		return execution, err
	}
	for _, actorCode := range candidates {
		if _, err = tx.ExecContext(ctx, `SAVEPOINT city_open_world_v5_navigation_step`); err != nil {
			return execution, fmt.Errorf("create V5 navigation savepoint: %w", err)
		}
		step, stepErr := advanceCityOpenWorldV5NavigationIntent(
			ctx, tx, worldID, targetTick, execution.nextFactSeq, execution.nextEffectSeq, actorCode,
		)
		if stepErr != nil {
			if _, rollbackErr := tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT city_open_world_v5_navigation_step`); rollbackErr != nil {
				return execution, fmt.Errorf("rollback V5 navigation step after %v: %w", stepErr, rollbackErr)
			}
			if _, releaseErr := tx.ExecContext(ctx, `RELEASE SAVEPOINT city_open_world_v5_navigation_step`); releaseErr != nil {
				return execution, fmt.Errorf("release failed V5 navigation savepoint: %w", releaseErr)
			}
			return execution, stepErr
		}
		if _, err = tx.ExecContext(ctx, `RELEASE SAVEPOINT city_open_world_v5_navigation_step`); err != nil {
			return execution, fmt.Errorf("release V5 navigation savepoint: %w", err)
		}
		if step == nil {
			continue
		}
		execution.facts = append(execution.facts, step.facts...)
		execution.effects = append(execution.effects, step.effects...)
		execution.events = append(execution.events, step.events...)
		execution.nextFactSeq = step.nextFactSeq
		execution.nextEffectSeq = step.nextEffectSeq
	}
	return execution, nil
}

func loadCityOpenWorldV5NavigationCandidates(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, targetTick int64,
) ([]string, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT actor.code
FROM city_open_world_actor_navigation_intents intent
JOIN city_open_world_actors actor
  ON actor.id = intent.actor_id AND actor.world_id = intent.world_id
WHERE intent.world_id = $1 AND intent.status = 'active' AND intent.next_attempt_tick <= $2
ORDER BY intent.priority DESC, intent.blocked_attempts DESC, intent.created_tick ASC, actor.code ASC
LIMIT $3`, worldID, targetTick, cityOpenWorldV5NavigationMaximumPerTick)
	if err != nil {
		return nil, fmt.Errorf("load V5 navigation candidates: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]string, 0)
	for rows.Next() {
		var code string
		if err = rows.Scan(&code); err != nil {
			return nil, fmt.Errorf("scan V5 navigation candidate: %w", err)
		}
		items = append(items, code)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate V5 navigation candidates: %w", err)
	}
	return items, nil
}

func loadCityOpenWorldV5NavigationActorForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	actorCode string,
) (*cityOpenWorldRuntimeActorRef, error) {
	actor := &cityOpenWorldRuntimeActorRef{}
	var buildingCode sql.NullString
	var factTick, factSequence sql.NullInt64
	err := tx.QueryRowContext(ctx, `
SELECT actor.id, actor.code, actor.actor_type_code,
       location.space_kind, location.location_scope, location.building_code,
       location.floor_index, location.x, location.y, location.z,
       location.sector_x, location.sector_y, location.chunk_x, location.chunk_y,
       location.local_x, location.local_y, location.moved_tick,
       source_fact.tick, source_fact.sequence, location.version, location.metadata
FROM city_open_world_actors actor
JOIN city_open_world_actor_locations location
  ON location.actor_id = actor.id AND location.world_id = actor.world_id
LEFT JOIN city_open_world_runtime_facts source_fact
  ON source_fact.id = location.source_fact_id AND source_fact.world_id = location.world_id
WHERE actor.world_id = $1 AND actor.code = $2 AND actor.status = 'active'
FOR UPDATE OF actor, location`, worldID, actorCode).Scan(
		&actor.id, &actor.actor.Code, &actor.actor.ActorTypeCode,
		&actor.location.SpaceKind, &actor.location.LocationScope, &buildingCode,
		&actor.location.FloorIndex, &actor.location.X, &actor.location.Y, &actor.location.Z,
		&actor.location.SectorX, &actor.location.SectorY, &actor.location.ChunkX, &actor.location.ChunkY,
		&actor.location.LocalX, &actor.location.LocalY, &actor.location.MovedTick,
		&factTick, &factSequence, &actor.location.Version, &actor.location.Metadata,
	)
	if err == sql.ErrNoRows {
		return nil, cityOpenWorldRuntimeReject(cityOpenWorldRuntimeRejectionActorNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("lock V5 navigation actor: %w", err)
	}
	actor.location.ActorCode = actor.actor.Code
	if buildingCode.Valid {
		actor.location.BuildingCode = cityOpenWorldStringPointer(buildingCode.String)
	}
	if factTick.Valid {
		actor.location.SourceFact = &CityOpenWorldRuntimeFactRef{Tick: factTick.Int64, Sequence: factSequence.Int64}
	}
	actor.actor.Location = &actor.location
	return actor, nil
}

func advanceCityOpenWorldV5NavigationIntent(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, effectSequence int64,
	actorCode string,
) (*cityOpenWorldRuntimeAutomaticExecution, error) {
	// Lock actor before intent, matching user command order and avoiding an
	// actor/intent lock inversion under concurrent stepping attempts.
	actor, err := loadCityOpenWorldV5NavigationActorForUpdate(ctx, tx, worldID, actorCode)
	if err != nil {
		return nil, err
	}
	record, err := loadCityOpenWorldV5NavigationIntent(ctx, tx, worldID, actorCode, true)
	if err != nil || record == nil {
		return nil, err
	}
	if record.intent.Status != cityOpenWorldV5NavigationStatusActive || record.intent.NextAttemptTick > targetTick {
		return nil, nil
	}
	target, targetErr := cityOpenWorldV5NavigationIntentTarget(record.intent)
	if targetErr != nil || cityOpenWorldRuntimeValidatePassableLocation(ctx, tx, worldID, target) != nil {
		return persistCityOpenWorldV5NavigationAutomaticState(
			ctx, tx, worldID, targetTick, factSequence, effectSequence, actor, record,
			cityOpenWorldV5NavigationFailedIntent(record.intent, targetTick, cityOpenWorldV5NavigationReasonTargetInvalid),
			CityOpenWorldRuntimeFactNavigationFailed, nil,
			map[string]any{"reason": cityOpenWorldV5NavigationReasonTargetInvalid},
		)
	}
	if cityOpenWorldRuntimeLocationEqual(actor.location, target) {
		return persistCityOpenWorldV5NavigationAutomaticState(
			ctx, tx, worldID, targetTick, factSequence, effectSequence, actor, record,
			cityOpenWorldV5NavigationArrivedIntent(record.intent, targetTick),
			CityOpenWorldRuntimeFactNavigationArrived, nil,
			map[string]any{"reason": cityOpenWorldV5NavigationReasonArrived, "position": target},
		)
	}
	if record.intent.CompletedSteps >= record.intent.MaximumSteps {
		return persistCityOpenWorldV5NavigationAutomaticState(
			ctx, tx, worldID, targetTick, factSequence, effectSequence, actor, record,
			cityOpenWorldV5NavigationFailedIntent(record.intent, targetTick, cityOpenWorldV5NavigationReasonStepLimit),
			CityOpenWorldRuntimeFactNavigationFailed, nil,
			map[string]any{"reason": cityOpenWorldV5NavigationReasonStepLimit},
		)
	}

	portalEdge, routeErr := cityOpenWorldV5NavigationNextPortalEdge(ctx, tx, worldID, actor.id, targetTick, actor.location, target)
	if routeErr != nil {
		return persistCityOpenWorldV5NavigationBlockedState(
			ctx, tx, worldID, targetTick, factSequence, effectSequence, actor, record, routeErr.Error(),
		)
	}
	if portalEdge != nil && cityOpenWorldRuntimeLocationEqual(actor.location, portalEdge.from) {
		if err = cityOpenWorldRuntimeValidatePassableLocation(ctx, tx, worldID, portalEdge.to); err != nil {
			return persistCityOpenWorldV5NavigationBlockedState(ctx, tx, worldID, targetTick, factSequence, effectSequence, actor, record, cityOpenWorldV5NavigationReasonBlocked)
		}
		occupied, occupancyErr := cityOpenWorldRuntimeLocationOccupied(ctx, tx, worldID, actor.id, portalEdge.to)
		if occupancyErr != nil {
			return nil, occupancyErr
		}
		if occupied {
			return persistCityOpenWorldV5NavigationBlockedState(ctx, tx, worldID, targetTick, factSequence, effectSequence, actor, record, cityOpenWorldRuntimeRejectionCellOccupied)
		}
		after := cityOpenWorldV5NavigationProgressedIntent(record.intent, targetTick)
		return persistCityOpenWorldV5NavigationAutomaticState(
			ctx, tx, worldID, targetTick, factSequence, effectSequence, actor, record, after,
			CityOpenWorldRuntimeFactNavigationProgressed, &portalEdge.to,
			map[string]any{"operation": "portal", "portal_code": portalEdge.portal.Code, "from": actor.location, "to": portalEdge.to},
		)
	}
	goal := target
	if portalEdge != nil {
		goal = portalEdge.from
	}
	next, reachable, pathErr := cityOpenWorldV5NavigationNextLocalStep(ctx, tx, worldID, actor.id, actor.location, goal)
	if pathErr != nil {
		return nil, pathErr
	}
	if next == nil {
		return persistCityOpenWorldV5NavigationBlockedState(ctx, tx, worldID, targetTick, factSequence, effectSequence, actor, record, cityOpenWorldV5NavigationReasonBlocked)
	}
	after := cityOpenWorldV5NavigationProgressedIntent(record.intent, targetTick)
	factType := CityOpenWorldRuntimeFactNavigationProgressed
	detail := map[string]any{"operation": "move", "from": actor.location, "to": *next, "goal": goal, "path_reached_goal": reachable}
	if cityOpenWorldRuntimeLocationEqual(*next, target) {
		after = cityOpenWorldV5NavigationArrivedIntent(after, targetTick)
		factType = CityOpenWorldRuntimeFactNavigationArrived
		detail["reason"] = cityOpenWorldV5NavigationReasonArrived
	} else if after.CompletedSteps >= after.MaximumSteps {
		after = cityOpenWorldV5NavigationFailedIntent(after, targetTick, cityOpenWorldV5NavigationReasonStepLimit)
		factType = CityOpenWorldRuntimeFactNavigationFailed
		detail["reason"] = cityOpenWorldV5NavigationReasonStepLimit
	}
	return persistCityOpenWorldV5NavigationAutomaticState(
		ctx, tx, worldID, targetTick, factSequence, effectSequence, actor, record, after,
		factType, next, detail,
	)
}

func cityOpenWorldV5NavigationArrivedIntent(intent CityOpenWorldNavigationIntent, targetTick int64) CityOpenWorldNavigationIntent {
	intent.Status = cityOpenWorldV5NavigationStatusArrived
	intent.BlockedAttempts = 0
	intent.NextAttemptTick = targetTick
	metadata, _ := cityOpenWorldV5NavigationMetadata(cityOpenWorldV5NavigationReasonArrived)
	intent.Metadata = metadata
	return intent
}

func cityOpenWorldV5NavigationProgressedIntent(intent CityOpenWorldNavigationIntent, targetTick int64) CityOpenWorldNavigationIntent {
	intent.CompletedSteps++
	intent.BlockedAttempts = 0
	intent.NextAttemptTick = targetTick + 1
	metadata, _ := cityOpenWorldV5NavigationMetadata("")
	intent.Metadata = metadata
	return intent
}

func cityOpenWorldV5NavigationFailedIntent(intent CityOpenWorldNavigationIntent, targetTick int64, reason string) CityOpenWorldNavigationIntent {
	intent.Status = cityOpenWorldV5NavigationStatusFailed
	intent.NextAttemptTick = targetTick
	metadata, _ := cityOpenWorldV5NavigationMetadata(reason)
	intent.Metadata = metadata
	return intent
}

func persistCityOpenWorldV5NavigationBlockedState(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, effectSequence int64,
	actor *cityOpenWorldRuntimeActorRef,
	record *cityOpenWorldV5NavigationIntentRecord,
	reason string,
) (*cityOpenWorldRuntimeAutomaticExecution, error) {
	after := record.intent
	after.BlockedAttempts++
	factType := CityOpenWorldRuntimeFactNavigationBlocked
	if after.BlockedAttempts >= cityOpenWorldV5NavigationMaximumBlocked {
		after = cityOpenWorldV5NavigationFailedIntent(after, targetTick, reason)
		factType = CityOpenWorldRuntimeFactNavigationFailed
	} else {
		after.Status = cityOpenWorldV5NavigationStatusActive
		after.NextAttemptTick = targetTick + cityOpenWorldV5NavigationRetryDelay(after.BlockedAttempts)
		metadata, metadataErr := cityOpenWorldV5NavigationMetadata(reason)
		if metadataErr != nil {
			return nil, metadataErr
		}
		after.Metadata = metadata
	}
	return persistCityOpenWorldV5NavigationAutomaticState(
		ctx, tx, worldID, targetTick, factSequence, effectSequence, actor, record, after,
		factType, nil, map[string]any{"reason": reason, "blocked_attempts": after.BlockedAttempts},
	)
}

func cityOpenWorldV5NavigationRetryDelay(blockedAttempts int) int64 {
	delay := int64(1 + blockedAttempts/4)
	if delay > cityOpenWorldV5NavigationMaximumRetryDelay {
		return cityOpenWorldV5NavigationMaximumRetryDelay
	}
	return delay
}

func persistCityOpenWorldV5NavigationAutomaticState(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, effectSequence int64,
	actor *cityOpenWorldRuntimeActorRef,
	record *cityOpenWorldV5NavigationIntentRecord,
	after CityOpenWorldNavigationIntent,
	factType string,
	movement *CityOpenWorldActorLocation,
	detail map[string]any,
) (*cityOpenWorldRuntimeAutomaticExecution, error) {
	if actor == nil || record == nil {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v5_navigation_automatic"})
	}
	after.ActorCode = actor.actor.Code
	after.UpdatedTick = targetTick
	after.Version = record.intent.Version + 1
	after.SourceFact = CityOpenWorldRuntimeFactRef{Tick: targetTick, Sequence: factSequence}
	payload := map[string]any{
		"schema_version": 1, "navigation_intent_before": record.intent,
		"navigation_intent_after": after,
	}
	for key, value := range detail {
		payload[key] = value
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal V5 automatic navigation fact: %w", err)
	}
	fact, err := insertCityOpenWorldRuntimeFact(ctx, tx, cityOpenWorldRuntimeFactInsert{
		worldID: worldID, tick: targetTick, sequence: factSequence, parentFactID: &record.sourceFactID,
		actorID: &actor.id, factType: factType, payload: raw,
	})
	if err != nil {
		return nil, err
	}
	effects := make([]CityOpenWorldRuntimeEffect, 0, 2)
	nextEffectSequence := effectSequence
	if movement != nil {
		if err = updateCityOpenWorldActorLocation(ctx, tx, worldID, actor.id, targetTick, fact.id, *movement); err != nil {
			return nil, err
		}
		locationPayload, marshalErr := json.Marshal(map[string]any{"from": actor.location, "to": *movement, "source": "navigation"})
		if marshalErr != nil {
			return nil, marshalErr
		}
		locationEffect, effectErr := insertCityOpenWorldRuntimeEffect(ctx, tx, cityOpenWorldRuntimeEffectInsert{
			worldID: worldID, tick: targetTick, sequence: nextEffectSequence, sourceFact: fact,
			operationIndex: 1, effectType: WorldRuntimeEffectLocationSet, targetActorID: &actor.id,
			targetActorCode: &actor.actor.Code, targetKey: cityOpenWorldStringPointer("location"), payload: locationPayload,
		})
		if effectErr != nil {
			return nil, effectErr
		}
		effects = append(effects, locationEffect)
		nextEffectSequence++
	}
	intentEffect, err := applyCityOpenWorldV5NavigationIntentEffect(
		ctx, tx, worldID, targetTick, nextEffectSequence, len(effects)+1, actor, fact, record, after,
	)
	if err != nil {
		return nil, err
	}
	effects = append(effects, intentEffect)
	nextEffectSequence++
	if err = touchCityOpenWorldActor(ctx, tx, worldID, actor.id, targetTick); err != nil {
		return nil, err
	}
	if err = postCityOpenWorldRuntimeFact(ctx, tx, fact.id); err != nil {
		return nil, err
	}
	if err = updateCityOpenWorldRuntimeProfile(ctx, tx, worldID, 0, 1, int64(len(effects)), 0); err != nil {
		return nil, err
	}
	eventType := "city.open_world.actor.navigation_progressed"
	switch factType {
	case CityOpenWorldRuntimeFactNavigationArrived:
		eventType = "city.open_world.actor.navigation_arrived"
	case CityOpenWorldRuntimeFactNavigationBlocked:
		eventType = "city.open_world.actor.navigation_blocked"
	case CityOpenWorldRuntimeFactNavigationFailed:
		eventType = "city.open_world.actor.navigation_failed"
	}
	eventPayload := map[string]any{"actor_code": actor.actor.Code, "intent": after}
	if movement != nil {
		eventPayload["location"] = *movement
	}
	return &cityOpenWorldRuntimeAutomaticExecution{
		facts: []CityOpenWorldRuntimeFact{fact.fact}, effects: effects,
		events:      []worldRuntimeAutomaticEvent{{eventType: eventType, payload: eventPayload}},
		nextFactSeq: factSequence + 1, nextEffectSeq: nextEffectSequence,
	}, nil
}

func cityOpenWorldV5NavigationNextPortalEdge(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, actorID, targetTick int64,
	current, target CityOpenWorldActorLocation,
) (*cityOpenWorldV5NavigationPortalEdge, error) {
	if cityOpenWorldV5NavigationDomainKey(current) == cityOpenWorldV5NavigationDomainKey(target) {
		return nil, nil
	}
	edges, err := loadCityOpenWorldV5NavigationPortalEdges(ctx, queryer, worldID, current.ActorCode)
	if err != nil {
		return nil, err
	}
	targetDomain := cityOpenWorldV5NavigationDomainKey(target)
	reachable := map[string]bool{targetDomain: true}
	for changed := true; changed; {
		changed = false
		for _, edge := range edges {
			from, to := cityOpenWorldV5NavigationDomainKey(edge.from), cityOpenWorldV5NavigationDomainKey(edge.to)
			if reachable[to] && !reachable[from] {
				reachable[from] = true
				changed = true
			}
		}
	}
	currentDomain := cityOpenWorldV5NavigationDomainKey(current)
	if !reachable[currentDomain] {
		return nil, cityOpenWorldRuntimeReject(cityOpenWorldV5NavigationReasonBlocked)
	}
	candidates := make([]cityOpenWorldV5NavigationPortalEdge, 0)
	for _, edge := range edges {
		if cityOpenWorldV5NavigationDomainKey(edge.from) == currentDomain && reachable[cityOpenWorldV5NavigationDomainKey(edge.to)] {
			candidates = append(candidates, edge)
		}
	}
	sort.Slice(candidates, func(left, right int) bool {
		leftDistance := cityOpenWorldV5NavigationDistance(current, candidates[left].from)
		rightDistance := cityOpenWorldV5NavigationDistance(current, candidates[right].from)
		if leftDistance != rightDistance {
			return leftDistance < rightDistance
		}
		return candidates[left].portal.Code < candidates[right].portal.Code
	})
	for index := range candidates {
		candidate := candidates[index]
		state, stateErr := loadCityOpenWorldRuntimePortalStateForUse(ctx, queryer, worldID, candidate.portal)
		if stateErr != nil || state.StateCode != WorldPortalStateOpen {
			continue
		}
		evaluation, evaluationErr := evaluateCityOpenWorldRuntimeRequirement(
			ctx, queryer, worldID, actorID, targetTick, state.AccessRequirement,
		)
		if evaluationErr != nil || !evaluation.Satisfied {
			continue
		}
		return &candidate, nil
	}
	return nil, cityOpenWorldRuntimeReject(cityOpenWorldV5NavigationReasonBlocked)
}

func loadCityOpenWorldV5NavigationPortalEdges(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	actorCode string,
) ([]cityOpenWorldV5NavigationPortalEdge, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT code, building_code, portal_type, from_floor_index, to_floor_index,
       from_x, from_y, from_z, to_x, to_y, to_z, bidirectional
FROM city_open_world_portals
WHERE world_id = $1
ORDER BY code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V5 navigation portals: %w", err)
	}
	defer func() { _ = rows.Close() }()
	edges := make([]cityOpenWorldV5NavigationPortalEdge, 0)
	for rows.Next() {
		portal := &cityOpenWorldStaticPortal{}
		if err = rows.Scan(
			&portal.Code, &portal.BuildingCode, &portal.PortalType, &portal.FromFloorIndex, &portal.ToFloorIndex,
			&portal.From.X, &portal.From.Y, &portal.From.Z, &portal.To.X, &portal.To.Y, &portal.To.Z, &portal.Bidirectional,
		); err != nil {
			return nil, fmt.Errorf("scan V5 navigation portal: %w", err)
		}
		from, to, endpointErr := cityOpenWorldRuntimePortalEndpoints(actorCode, portal)
		if endpointErr != nil {
			return nil, endpointErr
		}
		edges = append(edges, cityOpenWorldV5NavigationPortalEdge{portal: portal, from: from, to: to})
		if portal.Bidirectional {
			edges = append(edges, cityOpenWorldV5NavigationPortalEdge{portal: portal, from: to, to: from})
		}
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate V5 navigation portals: %w", err)
	}
	return edges, nil
}

func cityOpenWorldV5NavigationDomainKey(location CityOpenWorldActorLocation) string {
	return location.SpaceKind + "|" + location.LocationScope + "|" + strconv.FormatInt(int64(location.FloorIndex), 10)
}

func cityOpenWorldV5NavigationLocationKey(location CityOpenWorldActorLocation) string {
	return cityOpenWorldV5NavigationDomainKey(location) + "|" + strconv.FormatInt(location.X, 10) + "|" + strconv.FormatInt(location.Y, 10) + "|" + strconv.FormatInt(int64(location.Z), 10)
}

func cityOpenWorldV5NavigationDistance(left, right CityOpenWorldActorLocation) int64 {
	dx, dy := left.X-right.X, left.Y-right.Y
	if dx < 0 {
		dx = -dx
	}
	if dy < 0 {
		dy = -dy
	}
	if dx > dy {
		return dx
	}
	return dy
}

func cityOpenWorldV5NavigationNextLocalStep(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, actorID int64,
	current, goal CityOpenWorldActorLocation,
) (*CityOpenWorldActorLocation, bool, error) {
	if cityOpenWorldV5NavigationDomainKey(current) != cityOpenWorldV5NavigationDomainKey(goal) {
		return nil, false, nil
	}
	occupied, err := loadCityOpenWorldV5NavigationOccupancy(ctx, queryer, worldID, actorID, current)
	if err != nil {
		return nil, false, err
	}
	type node struct {
		location CityOpenWorldActorLocation
		first    *CityOpenWorldActorLocation
	}
	queue := []node{{location: current}}
	visited := map[string]bool{cityOpenWorldV5NavigationLocationKey(current): true}
	passable := make(map[string]bool)
	bestDistance := cityOpenWorldV5NavigationDistance(current, goal)
	var bestFirst *CityOpenWorldActorLocation
	expanded := 0
	for len(queue) > 0 && expanded < cityOpenWorldV5NavigationMaximumSearchNodes {
		item := queue[0]
		queue = queue[1:]
		expanded++
		if cityOpenWorldRuntimeLocationEqual(item.location, goal) {
			return item.first, true, nil
		}
		for _, neighbor := range cityOpenWorldV5NavigationNeighbors(item.location, goal) {
			key := cityOpenWorldV5NavigationLocationKey(neighbor)
			if visited[key] || occupied[key] {
				continue
			}
			valid, cached := passable[key]
			if !cached {
				valid = cityOpenWorldRuntimeValidatePassableLocation(ctx, queryer, worldID, neighbor) == nil
				passable[key] = valid
			}
			if !valid {
				continue
			}
			visited[key] = true
			first := item.first
			if first == nil {
				copy := neighbor
				first = &copy
			}
			distance := cityOpenWorldV5NavigationDistance(neighbor, goal)
			if distance < bestDistance {
				bestDistance = distance
				bestFirst = first
			}
			queue = append(queue, node{location: neighbor, first: first})
		}
	}
	if bestFirst != nil {
		return bestFirst, false, nil
	}
	return nil, false, nil
}

func loadCityOpenWorldV5NavigationOccupancy(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, actorID int64,
	location CityOpenWorldActorLocation,
) (map[string]bool, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT actor.code, location.space_kind, location.location_scope, location.building_code,
       location.floor_index, location.x, location.y, location.z, location.sector_x,
       location.sector_y, location.chunk_x, location.chunk_y, location.local_x, location.local_y,
       location.moved_tick, location.version, location.metadata
FROM city_open_world_actor_locations location
JOIN city_open_world_actors actor
  ON actor.id = location.actor_id AND actor.world_id = location.world_id
WHERE location.world_id = $1 AND location.actor_id <> $2 AND location.space_kind = $3
  AND location.location_scope = $4 AND location.floor_index = $5`,
		worldID, actorID, location.SpaceKind, location.LocationScope, location.FloorIndex)
	if err != nil {
		return nil, fmt.Errorf("load V5 navigation occupancy: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make(map[string]bool)
	for rows.Next() {
		item := CityOpenWorldActorLocation{}
		var buildingCode sql.NullString
		if err = rows.Scan(
			&item.ActorCode, &item.SpaceKind, &item.LocationScope, &buildingCode,
			&item.FloorIndex, &item.X, &item.Y, &item.Z, &item.SectorX, &item.SectorY,
			&item.ChunkX, &item.ChunkY, &item.LocalX, &item.LocalY, &item.MovedTick,
			&item.Version, &item.Metadata,
		); err != nil {
			return nil, fmt.Errorf("scan V5 navigation occupancy: %w", err)
		}
		if buildingCode.Valid {
			item.BuildingCode = cityOpenWorldStringPointer(buildingCode.String)
		}
		items[cityOpenWorldV5NavigationLocationKey(item)] = true
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate V5 navigation occupancy: %w", err)
	}
	return items, nil
}

func cityOpenWorldV5NavigationNeighbors(
	location, goal CityOpenWorldActorLocation,
) []CityOpenWorldActorLocation {
	directions := [][2]int64{{-1, -1}, {0, -1}, {1, -1}, {-1, 0}, {1, 0}, {-1, 1}, {0, 1}, {1, 1}}
	items := make([]CityOpenWorldActorLocation, 0, len(directions))
	for _, direction := range directions {
		candidate, err := cityOpenWorldRuntimeLocationFromPayload(cityOpenWorldActorMovePayload{
			ActorCode: location.ActorCode, SpaceKind: location.SpaceKind,
			BuildingCode: cityOpenWorldV5StringValue(location.BuildingCode), FloorIndex: location.FloorIndex,
			X: location.X + direction[0], Y: location.Y + direction[1], Z: location.Z,
		})
		if err == nil {
			items = append(items, candidate)
		}
	}
	sort.SliceStable(items, func(left, right int) bool {
		leftDistance := cityOpenWorldV5NavigationDistance(items[left], goal)
		rightDistance := cityOpenWorldV5NavigationDistance(items[right], goal)
		if leftDistance != rightDistance {
			return leftDistance < rightDistance
		}
		if items[left].Y != items[right].Y {
			return items[left].Y < items[right].Y
		}
		return items[left].X < items[right].X
	})
	return items
}
