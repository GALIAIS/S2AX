package securityaudit

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"time"
)

type replayDecision struct {
	ID               int64
	DecisionID       string
	SourceType       string
	UserID           sql.NullInt64
	APIKeyID         sql.NullInt64
	GroupID          sql.NullInt64
	Protocol         string
	Endpoint         string
	RequestedModel   string
	RiskLevel        string
	RequestAction    string
	CandidateActions []string
	CreatedAt        time.Time
}

func (r *PostgreSQLRepository) ReplayPolicy(
	ctx context.Context,
	policy *PolicyVersion,
	request PolicyReplayRequest,
) (*PolicyReplayResult, error) {
	if policy == nil {
		return nil, ErrPolicyNotFound
	}
	if request.WindowHours < 1 {
		request.WindowHours = 24 * 7
	}
	if request.WindowHours > 24*90 {
		request.WindowHours = 24 * 90
	}
	if request.Limit < 1 {
		request.Limit = 1000
	}
	if request.Limit > 5000 {
		request.Limit = 5000
	}
	rows, err := r.db.QueryContext(ctx, `
SELECT id,decision_id,source_type,user_id,api_key_id,group_id,protocol,endpoint,
       requested_model,risk_level,request_action,candidate_actions,created_at
FROM security_audit_decisions
WHERE created_at >= NOW()-($1 * INTERVAL '1 hour')
ORDER BY created_at DESC,id DESC
LIMIT $2`, request.WindowHours, request.Limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	result := &PolicyReplayResult{
		PolicyKey: policy.PolicyKey, PolicyVersion: policy.Version, ConfigDigest: policy.ConfigDigest,
		WindowHours: request.WindowHours, RequestedLimit: request.Limit,
		BySource: map[string]int{}, ByProposedAction: map[string]int{},
		Examples: make([]PolicyReplayExample, 0, 20), GeneratedAt: time.Now().UTC(),
	}
	for rows.Next() {
		var item replayDecision
		var candidatesRaw []byte
		if err := rows.Scan(
			&item.ID, &item.DecisionID, &item.SourceType, &item.UserID, &item.APIKeyID,
			&item.GroupID, &item.Protocol, &item.Endpoint, &item.RequestedModel,
			&item.RiskLevel, &item.RequestAction, &candidatesRaw, &item.CreatedAt,
		); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(candidatesRaw, &item.CandidateActions)
		result.Analyzed++
		result.BySource[item.SourceType]++
		simulation := simulatePolicy(policy, PolicySimulationRequest{
			UserID: item.UserID.Int64, APIKeyID: item.APIKeyID.Int64,
			GroupID: nullableReplayID(item.GroupID), Protocol: item.Protocol,
			Endpoint: item.Endpoint, Model: item.RequestedModel, RiskLevel: item.RiskLevel,
		})
		if !simulation.Matched {
			result.Unmatched++
			continue
		}
		result.Matched++
		proposed := simulation.RequestAction
		if proposed == "" {
			proposed = "allow"
		}
		result.ByProposedAction[proposed]++
		actionChanged := proposed != item.RequestAction
		candidateChanged := !sameStringSet(item.CandidateActions, simulation.CandidateActions)
		if actionChanged {
			result.ActionChanges++
			if requestActionRank(proposed) > requestActionRank(item.RequestAction) {
				result.StricterChanges++
			} else {
				result.LooserChanges++
			}
		}
		if candidateChanged {
			result.CandidateActionChanges++
		}
		if (actionChanged || candidateChanged) && len(result.Examples) < 20 {
			result.Examples = append(result.Examples, PolicyReplayExample{
				DecisionPK: item.ID, DecisionID: item.DecisionID, SourceType: item.SourceType,
				RiskLevel: item.RiskLevel, PreviousAction: item.RequestAction,
				ProposedAction: proposed, CandidateChanged: candidateChanged, CreatedAt: item.CreatedAt,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func nullableReplayID(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	id := value.Int64
	return &id
}

func requestActionRank(value string) int {
	switch value {
	case "block":
		return 3
	case "warn":
		return 2
	case "allow":
		return 1
	default:
		return 0
	}
}

func sameStringSet(left, right []string) bool {
	left = canonicalStrings(left)
	right = canonicalStrings(right)
	sort.Strings(left)
	sort.Strings(right)
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
