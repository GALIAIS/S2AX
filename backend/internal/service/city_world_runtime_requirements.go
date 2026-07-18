package service

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
)

const (
	WorldRequirementAll           = "all"
	WorldRequirementAny           = "any"
	WorldRequirementNot           = "not"
	WorldRequirementAttributeGTE  = "attribute_gte"
	WorldRequirementAttributeLTE  = "attribute_lte"
	WorldRequirementExperienceGTE = "experience_gte"
	WorldRequirementRoleActive    = "role_active"
	WorldRequirementRoleInactive  = "role_inactive"
	WorldRequirementStatusPresent = "status_present"
	WorldRequirementStatusAbsent  = "status_absent"
	WorldRequirementFactCountGTE  = "fact_count_gte"
	WorldRequirementWorldTickGTE  = "world_tick_gte"

	worldRequirementMaximumDepth = 8
	worldRequirementMaximumNodes = 64
	worldRequirementMaximumItems = 16
)

type WorldRequirementNode struct {
	Operator      string                 `json:"op"`
	Items         []WorldRequirementNode `json:"items,omitempty"`
	Item          *WorldRequirementNode  `json:"item,omitempty"`
	AttributeCode string                 `json:"attribute_code,omitempty"`
	RoleCode      string                 `json:"role_code,omitempty"`
	StatusCode    string                 `json:"status_code,omitempty"`
	FactType      string                 `json:"fact_type,omitempty"`
	ValueUnits    int64                  `json:"value_units,omitempty"`
	MinimumStacks int                    `json:"minimum_stacks,omitempty"`
	WindowTicks   int64                  `json:"window_ticks,omitempty"`
}

type WorldRequirementFailure struct {
	Path          string `json:"path"`
	Operator      string `json:"operator"`
	Code          string `json:"code,omitempty"`
	ActualUnits   *int64 `json:"actual_units,omitempty"`
	RequiredUnits *int64 `json:"required_units,omitempty"`
	MessageCode   string `json:"message_code"`
}

type WorldRequirementEvaluation struct {
	Satisfied bool                      `json:"satisfied"`
	Failures  []WorldRequirementFailure `json:"failures"`
}

func validateWorldRequirement(root WorldRequirementNode) error {
	nodes := 0
	var visit func(WorldRequirementNode, int) error
	visit = func(node WorldRequirementNode, depth int) error {
		nodes++
		if nodes > worldRequirementMaximumNodes || depth > worldRequirementMaximumDepth {
			return fmt.Errorf("%w: requirement tree limit exceeded", errWorldRuntimeInvalidDefinition)
		}
		switch node.Operator {
		case WorldRequirementAll, WorldRequirementAny:
			if len(node.Items) > worldRequirementMaximumItems || node.Item != nil {
				return errWorldRuntimeInvalidDefinition
			}
			for _, item := range node.Items {
				if err := visit(item, depth+1); err != nil {
					return err
				}
			}
		case WorldRequirementNot:
			if node.Item == nil || len(node.Items) != 0 {
				return errWorldRuntimeInvalidDefinition
			}
			return visit(*node.Item, depth+1)
		case WorldRequirementAttributeGTE, WorldRequirementAttributeLTE, WorldRequirementExperienceGTE:
			if !worldRuntimeCodeValid(node.AttributeCode, 128) {
				return errWorldRuntimeInvalidDefinition
			}
		case WorldRequirementRoleActive, WorldRequirementRoleInactive:
			if !worldRuntimeCodeValid(node.RoleCode, 128) {
				return errWorldRuntimeInvalidDefinition
			}
		case WorldRequirementStatusPresent:
			if !worldRuntimeCodeValid(node.StatusCode, 128) || node.MinimumStacks < 0 || node.MinimumStacks > 1000000 {
				return errWorldRuntimeInvalidDefinition
			}
		case WorldRequirementStatusAbsent:
			if !worldRuntimeCodeValid(node.StatusCode, 128) {
				return errWorldRuntimeInvalidDefinition
			}
		case WorldRequirementFactCountGTE:
			if !worldRuntimeCodeValid(node.FactType, 128) || node.WindowTicks < 1 || node.ValueUnits < 0 {
				return errWorldRuntimeInvalidDefinition
			}
		case WorldRequirementWorldTickGTE:
			if node.ValueUnits < 0 {
				return errWorldRuntimeInvalidDefinition
			}
		default:
			return fmt.Errorf("%w: unknown requirement operator %q", errWorldRuntimeInvalidDefinition, node.Operator)
		}
		return nil
	}
	return visit(root, 1)
}

type worldRequirementEvaluator struct {
	ctx       context.Context
	queryer   citySQLQueryer
	worldID   int64
	actorID   int64
	worldTick int64
}

func evaluateWorldRequirement(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, actorID, worldTick int64,
	root WorldRequirementNode,
) (WorldRequirementEvaluation, error) {
	if err := validateWorldRequirement(root); err != nil {
		return WorldRequirementEvaluation{}, err
	}
	evaluator := worldRequirementEvaluator{
		ctx: ctx, queryer: queryer, worldID: worldID, actorID: actorID, worldTick: worldTick,
	}
	satisfied, failures, err := evaluator.evaluate(root, "requirements")
	if err != nil {
		return WorldRequirementEvaluation{}, err
	}
	if failures == nil {
		failures = make([]WorldRequirementFailure, 0)
	}
	return WorldRequirementEvaluation{Satisfied: satisfied, Failures: failures}, nil
}

func (e worldRequirementEvaluator) evaluate(
	node WorldRequirementNode,
	path string,
) (bool, []WorldRequirementFailure, error) {
	failure := func(code string, actual, required *int64) (bool, []WorldRequirementFailure, error) {
		return false, []WorldRequirementFailure{{
			Path: path, Operator: node.Operator, Code: code,
			ActualUnits: actual, RequiredUnits: required,
			MessageCode: "worldRuntime.requirements." + node.Operator,
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
FROM world_actor_attributes
WHERE world_id = $1 AND actor_id = $2 AND attribute_code = $3`,
			e.worldID, e.actorID, node.AttributeCode).Scan(&value, &experience)
		if err != nil && err != sql.ErrNoRows {
			return false, nil, fmt.Errorf("evaluate world actor attribute requirement: %w", err)
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
SELECT COUNT(*) FROM world_actor_roles
WHERE world_id = $1 AND actor_id = $2 AND role_code = $3 AND status = 'active'`,
			e.worldID, e.actorID, node.RoleCode).Scan(&count); err != nil {
			return false, nil, fmt.Errorf("evaluate world actor role requirement: %w", err)
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
FROM world_actor_statuses
WHERE world_id = $1 AND actor_id = $2 AND status_code = $3
  AND lifecycle_status = 'active'`, e.worldID, e.actorID, node.StatusCode).Scan(&stacks); err != nil {
			return false, nil, fmt.Errorf("evaluate world actor status requirement: %w", err)
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
SELECT COUNT(*) FROM world_runtime_facts
WHERE world_id = $1 AND actor_id = $2 AND fact_type = $3
  AND tick BETWEEN $4 AND $5`, e.worldID, e.actorID, node.FactType, fromTick, e.worldTick).Scan(&count); err != nil {
			return false, nil, fmt.Errorf("evaluate world actor fact requirement: %w", err)
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
		return false, nil, fmt.Errorf("unsupported world requirement operator %q", node.Operator)
	}
}
