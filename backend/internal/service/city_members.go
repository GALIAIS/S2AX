package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	CityMemberRolePlanner   = "planner"
	CityMemberRoleTreasurer = "treasurer"
	CityMemberRoleTrader    = "trader"
	CityMemberRoleViewer    = "viewer"

	CityMemberStatusActive = "active"
	CityMemberStatusLeft   = "left"
	CityMemberStatusBanned = "banned"
)

var (
	ErrCityMemberNotFound = infraerrors.NotFound("CITY_MEMBER_NOT_FOUND", "city member not found")
	ErrCityMemberIdentity = infraerrors.BadRequest("CITY_MEMBER_IDENTITY_INVALID", "city member identity must resolve to one active user")
	ErrCityMemberOwner    = infraerrors.Conflict("CITY_MEMBER_OWNER_IMMUTABLE", "city owner membership cannot be changed")
	ErrCityMemberGrants   = infraerrors.Conflict("CITY_MEMBER_HAS_ACTIVE_ACTOR_GRANTS", "revoke active actor control grants before removing this member")
)

type CityMember struct {
	UserID    int64      `json:"user_id"`
	Email     string     `json:"email"`
	Username  string     `json:"username"`
	Role      string     `json:"role"`
	Status    string     `json:"status"`
	JoinedAt  time.Time  `json:"joined_at"`
	LeftAt    *time.Time `json:"left_at,omitempty"`
	BannedAt  *time.Time `json:"banned_at,omitempty"`
	UpdatedAt time.Time  `json:"updated_at"`
}

type CityMemberAddInput struct {
	UserID   int64
	WorldID  int64
	Identity string
	Role     string
}

type CityMemberUpdateInput struct {
	UserID       int64
	WorldID      int64
	TargetUserID int64
	Role         string
	Status       string
}

func (s *CityEconomyService) ListWorldMembers(ctx context.Context, userID, worldID int64) ([]CityMember, error) {
	if userID <= 0 || worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, userID, worldID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT member.user_id, users.email, COALESCE(users.username, ''), member.role,
       member.status, member.joined_at, member.left_at, member.banned_at, member.updated_at
FROM city_members member
JOIN users ON users.id = member.user_id
WHERE member.world_id = $1 AND member.status = 'active'
  AND users.status = 'active' AND users.deleted_at IS NULL
ORDER BY CASE member.role WHEN 'owner' THEN 0 ELSE 1 END,
         COALESCE(NULLIF(users.username, ''), users.email), member.user_id`, worldID)
	if err != nil {
		return nil, fmt.Errorf("list city world members: %w", err)
	}
	items := make([]CityMember, 0)
	for rows.Next() {
		item, scanErr := scanCityMember(rows)
		if scanErr != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan city world member: %w", scanErr)
		}
		items = append(items, *item)
	}
	if err = closeCityRows(rows, "iterate city world members"); err != nil {
		return nil, err
	}
	if !IsCitySystemAdministrator(ctx) {
		for index := range items {
			items[index].Email = ""
		}
	}
	return items, nil
}

func (s *CityEconomyService) AddWorldMember(ctx context.Context, input CityMemberAddInput) (*CityMember, error) {
	identity := strings.TrimSpace(input.Identity)
	role := strings.ToLower(strings.TrimSpace(input.Role))
	if input.UserID <= 0 || input.WorldID <= 0 || utf8.RuneCountInString(identity) < 1 ||
		utf8.RuneCountInString(identity) > 255 || !validDelegatedCityMemberRole(role) {
		return nil, ErrCityInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin add city member transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	world, err := lockCityWorld(ctx, tx, input.UserID, input.WorldID)
	if err != nil {
		return nil, err
	}
	if err = ensureCityWorldWritable(world); err != nil {
		return nil, err
	}
	targetUserID, err := resolveCityMemberIdentity(ctx, tx, identity)
	if err != nil {
		return nil, err
	}
	if targetUserID == input.UserID {
		return nil, ErrCityMemberOwner
	}
	item, err := scanCityMember(tx.QueryRowContext(ctx, `
INSERT INTO city_members AS member
    (world_id, user_id, role, status, joined_at, left_at, banned_at, updated_at)
VALUES ($1, $2, $3, 'active', NOW(), NULL, NULL, NOW())
ON CONFLICT (world_id, user_id) DO UPDATE
SET role = EXCLUDED.role, status = 'active', joined_at = NOW(),
    left_at = NULL, banned_at = NULL, updated_at = NOW()
WHERE member.role <> 'owner'
RETURNING member.user_id,
          (SELECT email FROM users WHERE id = member.user_id),
          (SELECT COALESCE(username, '') FROM users WHERE id = member.user_id),
          member.role, member.status, member.joined_at, member.left_at,
          member.banned_at, member.updated_at`, input.WorldID, targetUserID, role))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCityMemberOwner
	}
	if err != nil {
		return nil, fmt.Errorf("upsert city world member: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit add city member: %w", err)
	}
	return item, nil
}

func (s *CityEconomyService) UpdateWorldMember(ctx context.Context, input CityMemberUpdateInput) (*CityMember, error) {
	role := strings.ToLower(strings.TrimSpace(input.Role))
	status := strings.ToLower(strings.TrimSpace(input.Status))
	if input.UserID <= 0 || input.WorldID <= 0 || input.TargetUserID <= 0 ||
		(role != "" && !validDelegatedCityMemberRole(role)) ||
		(status != "" && status != CityMemberStatusActive && status != CityMemberStatusLeft && status != CityMemberStatusBanned) ||
		(role == "" && status == "") {
		return nil, ErrCityInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin update city member transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	world, err := lockCityWorld(ctx, tx, input.UserID, input.WorldID)
	if err != nil {
		return nil, err
	}
	if err = ensureCityWorldWritable(world); err != nil {
		return nil, err
	}
	var currentRole, currentStatus string
	if err = tx.QueryRowContext(ctx, `
SELECT role, status FROM city_members
WHERE world_id = $1 AND user_id = $2
FOR UPDATE`, input.WorldID, input.TargetUserID).Scan(&currentRole, &currentStatus); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCityMemberNotFound
	} else if err != nil {
		return nil, fmt.Errorf("lock city world member: %w", err)
	}
	if currentRole == CityMemberRoleOwner || input.TargetUserID == input.UserID {
		return nil, ErrCityMemberOwner
	}
	if role == "" {
		role = currentRole
	}
	if status == "" {
		status = currentStatus
	}
	if status != CityMemberStatusActive {
		var activeGrantCount int
		if err = tx.QueryRowContext(ctx, `
SELECT COUNT(*) FROM world_actor_control_grants
WHERE world_id = $1 AND user_id = $2 AND status = 'active'`,
			input.WorldID, input.TargetUserID).Scan(&activeGrantCount); err != nil {
			return nil, fmt.Errorf("count member actor control grants: %w", err)
		}
		if activeGrantCount != 0 {
			return nil, ErrCityMemberGrants.WithMetadata(map[string]string{
				"active_grant_count": fmt.Sprintf("%d", activeGrantCount),
			})
		}
	}
	item, err := scanCityMember(tx.QueryRowContext(ctx, `
UPDATE city_members AS member
SET role = $3::varchar(16), status = $4::varchar(16),
    joined_at = CASE WHEN $4::varchar = 'active' AND member.status <> 'active' THEN NOW() ELSE member.joined_at END,
    left_at = CASE WHEN $4::varchar = 'left' THEN NOW() ELSE NULL END,
    banned_at = CASE WHEN $4::varchar = 'banned' THEN NOW() ELSE NULL END,
    updated_at = NOW()
WHERE member.world_id = $1 AND member.user_id = $2
RETURNING member.user_id,
          (SELECT email FROM users WHERE id = member.user_id),
          (SELECT COALESCE(username, '') FROM users WHERE id = member.user_id),
          member.role, member.status, member.joined_at, member.left_at,
          member.banned_at, member.updated_at`, input.WorldID, input.TargetUserID, role, status))
	if err != nil {
		return nil, fmt.Errorf("update city world member: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit update city member: %w", err)
	}
	return item, nil
}

func validDelegatedCityMemberRole(role string) bool {
	switch role {
	case CityMemberRolePlanner, CityMemberRoleTreasurer, CityMemberRoleTrader, CityMemberRoleViewer:
		return true
	default:
		return false
	}
}

func resolveCityMemberIdentity(ctx context.Context, queryer citySQLQueryer, identity string) (int64, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT id
FROM users
WHERE status = 'active' AND deleted_at IS NULL
  AND (LOWER(email) = LOWER($1)
       OR (NULLIF(BTRIM(username), '') IS NOT NULL AND LOWER(username) = LOWER($1)))
ORDER BY CASE WHEN LOWER(email) = LOWER($1) THEN 0 ELSE 1 END, id
LIMIT 2`, identity)
	if err != nil {
		return 0, fmt.Errorf("resolve city member identity: %w", err)
	}
	ids := make([]int64, 0, 2)
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			_ = rows.Close()
			return 0, fmt.Errorf("scan city member identity: %w", err)
		}
		ids = append(ids, id)
	}
	if err = closeCityRows(rows, "iterate city member identity"); err != nil {
		return 0, err
	}
	if len(ids) != 1 {
		return 0, ErrCityMemberIdentity
	}
	return ids[0], nil
}

func scanCityMember(row cityScannable) (*CityMember, error) {
	item := &CityMember{}
	var leftAt, bannedAt sql.NullTime
	if err := row.Scan(&item.UserID, &item.Email, &item.Username, &item.Role, &item.Status,
		&item.JoinedAt, &leftAt, &bannedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	item.LeftAt = nullTimePointer(leftAt)
	item.BannedAt = nullTimePointer(bannedAt)
	return item, nil
}
