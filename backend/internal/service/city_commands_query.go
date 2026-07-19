package service

import (
	"context"
	"fmt"
	"strings"
)

type CityCommandListInput struct {
	UserID        int64
	WorldID       int64
	Status        string
	AfterSequence int64
	Limit         int
	Latest        bool
}

type CityCommandPage struct {
	Items      []*CityCommand `json:"items"`
	NextCursor *int64         `json:"next_cursor,omitempty"`
}

func (s *CityEconomyService) ListCommands(ctx context.Context, input CityCommandListInput) (*CityCommandPage, error) {
	status := strings.ToLower(strings.TrimSpace(input.Status))
	if input.UserID <= 0 || input.WorldID <= 0 || input.AfterSequence < 0 ||
		(status != "" && status != CityCommandStatusPending && status != CityCommandStatusApplied && status != CityCommandStatusRejected) {
		return nil, ErrCityInvalidInput
	}
	limit := input.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 200 {
		return nil, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "limit"})
	}
	orderClause := "ORDER BY c.sequence ASC"
	if input.Latest {
		orderClause = "ORDER BY c.sequence DESC"
	}
	query := `
SELECT ` + cityCommandColumns + `
FROM city_commands c
JOIN city_members member
  ON member.world_id = c.world_id AND member.user_id = $1 AND member.status = 'active'
WHERE c.world_id = $2
  AND (member.role = 'owner' OR c.user_id = $1)
  AND ($3 = '' OR c.status = $3)
  AND c.sequence > $4
` + orderClause + `
LIMIT $5`
	args := []any{input.UserID, input.WorldID, status, input.AfterSequence, limit + 1}
	if IsCitySystemAdministrator(ctx) {
		query = `
SELECT ` + cityCommandColumns + `
FROM city_commands c
WHERE c.world_id = $1
  AND ($2 = '' OR c.status = $2)
  AND c.sequence > $3
` + orderClause + `
LIMIT $4`
		args = []any{input.WorldID, status, input.AfterSequence, limit + 1}
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list city commands: %w", err)
	}
	items := make([]*CityCommand, 0, limit+1)
	for rows.Next() {
		item, scanErr := scanCityCommand(rows)
		if scanErr != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan city command list: %w", scanErr)
		}
		items = append(items, item)
	}
	if err = closeCityRows(rows, "iterate city commands"); err != nil {
		return nil, err
	}
	page := &CityCommandPage{Items: items}
	if len(items) > limit {
		page.Items = items[:limit]
		if !input.Latest {
			cursor := items[limit-1].Sequence
			page.NextCursor = &cursor
		}
	}
	return page, nil
}
