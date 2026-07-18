package service

import (
	"context"
	"errors"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/cityspatial"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	ErrCitySpatialRuleSetNotFound = infraerrors.NotFound(
		"CITY_SPATIAL_RULE_SET_NOT_FOUND", "city spatial rule set not found",
	)
	ErrCitySpatialRuleSetInvalid = infraerrors.InternalServer(
		"CITY_SPATIAL_RULE_SET_INVALID", "city spatial rule set is invalid",
	)
)

type CitySpatialRuleSet = cityspatial.RuleSet
type CitySpatialRuleSetSummary = cityspatial.RuleSetSummary

func (s *CityEconomyService) ListSpatialRuleSets(
	ctx context.Context,
	userID int64,
) ([]CitySpatialRuleSetSummary, error) {
	if userID <= 0 {
		return nil, ErrCityInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	registry, err := cityspatial.DefaultRegistry()
	if err != nil {
		return nil, ErrCitySpatialRuleSetInvalid.WithCause(err)
	}
	return registry.List(), nil
}

func (s *CityEconomyService) GetSpatialRuleSet(
	ctx context.Context,
	userID int64,
	ruleSetID string,
) (*CitySpatialRuleSet, error) {
	ruleSetID = strings.TrimSpace(ruleSetID)
	if userID <= 0 || ruleSetID == "" {
		return nil, ErrCityInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	registry, err := cityspatial.DefaultRegistry()
	if err != nil {
		return nil, ErrCitySpatialRuleSetInvalid.WithCause(err)
	}
	ruleSet, err := registry.Get(ruleSetID)
	if errors.Is(err, cityspatial.ErrRuleSetNotFound) {
		return nil, ErrCitySpatialRuleSetNotFound
	}
	if err != nil {
		return nil, ErrCitySpatialRuleSetInvalid.WithCause(err)
	}
	return ruleSet, nil
}
