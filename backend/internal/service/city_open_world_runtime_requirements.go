package service

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
)

// The requirement AST is shared with the generic runtime, while all lookup
// queries below point at the V4 runtime projections.  Keeping the evaluator
// here prevents an open-world world from accidentally consulting F7 actor data.
type cityOpenWorldRuntimeRequirementEvaluator struct {
	ctx       context.Context
	queryer   citySQLQueryer
	worldID   int64
	actorID   int64
	worldTick int64
}

func validateCityOpenWorldRuntimeRequirementReferences(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	requirement WorldRequirementNode,
) error {
	if err := validateWorldRequirement(requirement); err != nil {
		return err
	}
	type reference struct{ kind, code string }
	references := make(map[reference]struct{})
	var collect func(WorldRequirementNode)
	collect = func(node WorldRequirementNode) {
		switch node.Operator {
		case WorldRequirementAll, WorldRequirementAny:
			for _, item := range node.Items {
				collect(item)
			}
		case WorldRequirementNot:
			if node.Item != nil {
				collect(*node.Item)
			}
		case WorldRequirementAttributeGTE, WorldRequirementAttributeLTE, WorldRequirementExperienceGTE:
			references[reference{kind: WorldRuntimeDefinitionAttribute, code: node.AttributeCode}] = struct{}{}
		case WorldRequirementRoleActive, WorldRequirementRoleInactive:
			references[reference{kind: WorldRuntimeDefinitionRole, code: node.RoleCode}] = struct{}{}
		case WorldRequirementStatusPresent, WorldRequirementStatusAbsent:
			references[reference{kind: WorldRuntimeDefinitionStatus, code: node.StatusCode}] = struct{}{}
		}
	}
	collect(requirement)
	for item := range references {
		var exists bool
		if err := queryer.QueryRowContext(ctx, `
SELECT EXISTS (
    SELECT 1 FROM city_open_world_runtime_definitions
    WHERE world_id = $1 AND definition_kind = $2 AND code = $3
)`, worldID, item.kind, item.code).Scan(&exists); err != nil {
			return fmt.Errorf("verify open-world requirement reference: %w", err)
		}
		if !exists {
			return ErrCityOpenWorldRuntimeDefinitionNotFound.WithMetadata(map[string]string{
				"kind": item.kind, "code": item.code,
			})
		}
	}
	return nil
}

func evaluateCityOpenWorldRuntimeRequirement(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, actorID, worldTick int64,
	requirement WorldRequirementNode,
) (WorldRequirementEvaluation, error) {
	if err := validateWorldRequirement(requirement); err != nil {
		return WorldRequirementEvaluation{}, err
	}
	evaluator := cityOpenWorldRuntimeRequirementEvaluator{
		ctx: ctx, queryer: queryer, worldID: worldID, actorID: actorID, worldTick: worldTick,
	}
	satisfied, failures, err := evaluator.evaluate(requirement, "requirements")
	if err != nil {
		return WorldRequirementEvaluation{}, err
	}
	if failures == nil {
		failures = make([]WorldRequirementFailure, 0)
	}
	return WorldRequirementEvaluation{Satisfied: satisfied, Failures: failures}, nil
}

func (e cityOpenWorldRuntimeRequirementEvaluator) evaluate(
	node WorldRequirementNode,
	path string,
) (bool, []WorldRequirementFailure, error) {
	failure := func(code string, actual, required *int64) (bool, []WorldRequirementFailure, error) {
		return false, []WorldRequirementFailure{{
			Path: path, Operator: node.Operator, Code: code,
			ActualUnits: actual, RequiredUnits: required,
			MessageCode: "openWorldRuntime.requirements." + node.Operator,
		}}, nil
	}
	switch node.Operator {
	case WorldRequirementAll:
		failures := make([]WorldRequirementFailure, 0)
		for index, item := range node.Items {
			satisfied, itemFailures, err := e.evaluate(item, path+".items["+strconv.Itoa(index)+"]")
			if err != nil {
				return false, nil, err
			}
			if !satisfied {
				failures = append(failures, itemFailures...)
			}
		}
		return len(failures) == 0, failures, nil
	case WorldRequirementAny:
		failures := make([]WorldRequirementFailure, 0)
		for index, item := range node.Items {
			satisfied, itemFailures, err := e.evaluate(item, path+".items["+strconv.Itoa(index)+"]")
			if err != nil {
				return false, nil, err
			}
			if satisfied {
				return true, nil, nil
			}
			failures = append(failures, itemFailures...)
		}
		return false, failures, nil
	case WorldRequirementNot:
		satisfied, _, err := e.evaluate(*node.Item, path+".item")
		if err != nil {
			return false, nil, err
		}
		if satisfied {
			return failure("", nil, nil)
		}
		return true, nil, nil
	case WorldRequirementAttributeGTE, WorldRequirementAttributeLTE, WorldRequirementExperienceGTE:
		var value, experience int64
		err := e.queryer.QueryRowContext(e.ctx, `
SELECT value_units, experience_units
FROM city_open_world_actor_attributes
WHERE world_id = $1 AND actor_id = $2 AND attribute_code = $3`,
			e.worldID, e.actorID, node.AttributeCode).Scan(&value, &experience)
		if err != nil && err != sql.ErrNoRows {
			return false, nil, fmt.Errorf("evaluate open-world actor attribute requirement: %w", err)
		}
		if err == sql.ErrNoRows {
			value, experience = 0, 0
		}
		actual := value
		if node.Operator == WorldRequirementExperienceGTE {
			actual = experience
		}
		satisfied := actual >= node.ValueUnits
		if node.Operator == WorldRequirementAttributeLTE {
			satisfied = actual <= node.ValueUnits
		}
		if !satisfied {
			return failure(node.AttributeCode, &actual, &node.ValueUnits)
		}
		return true, nil, nil
	case WorldRequirementRoleActive, WorldRequirementRoleInactive:
		var count int64
		if err := e.queryer.QueryRowContext(e.ctx, `
SELECT COUNT(*) FROM city_open_world_actor_roles
WHERE world_id = $1 AND actor_id = $2 AND role_code = $3 AND status = 'active'`,
			e.worldID, e.actorID, node.RoleCode).Scan(&count); err != nil {
			return false, nil, fmt.Errorf("evaluate open-world actor role requirement: %w", err)
		}
		satisfied := count > 0
		if node.Operator == WorldRequirementRoleInactive {
			satisfied = !satisfied
		}
		if !satisfied {
			return failure(node.RoleCode, nil, nil)
		}
		return true, nil, nil
	case WorldRequirementStatusPresent, WorldRequirementStatusAbsent:
		var stacks int64
		if err := e.queryer.QueryRowContext(e.ctx, `
SELECT COALESCE(SUM(stacks), 0)
FROM city_open_world_actor_statuses
WHERE world_id = $1 AND actor_id = $2 AND status_code = $3
  AND lifecycle_status = 'active'`, e.worldID, e.actorID, node.StatusCode).Scan(&stacks); err != nil {
			return false, nil, fmt.Errorf("evaluate open-world actor status requirement: %w", err)
		}
		minimum := int64(node.MinimumStacks)
		if minimum == 0 {
			minimum = 1
		}
		satisfied := stacks >= minimum
		if node.Operator == WorldRequirementStatusAbsent {
			satisfied = stacks == 0
		}
		if !satisfied {
			return failure(node.StatusCode, &stacks, &minimum)
		}
		return true, nil, nil
	case WorldRequirementFactCountGTE:
		fromTick := e.worldTick - node.WindowTicks + 1
		if fromTick < 1 {
			fromTick = 1
		}
		var count int64
		if err := e.queryer.QueryRowContext(e.ctx, `
SELECT COUNT(*) FROM city_open_world_runtime_facts
WHERE world_id = $1 AND actor_id = $2 AND fact_type = $3
  AND tick BETWEEN $4 AND $5`, e.worldID, e.actorID, node.FactType, fromTick, e.worldTick).Scan(&count); err != nil {
			return false, nil, fmt.Errorf("evaluate open-world actor fact requirement: %w", err)
		}
		if count < node.ValueUnits {
			return failure(node.FactType, &count, &node.ValueUnits)
		}
		return true, nil, nil
	case WorldRequirementWorldTickGTE:
		if e.worldTick < node.ValueUnits {
			return failure("world_tick", &e.worldTick, &node.ValueUnits)
		}
		return true, nil, nil
	default:
		return false, nil, fmt.Errorf("unsupported open-world requirement operator %q", node.Operator)
	}
}
