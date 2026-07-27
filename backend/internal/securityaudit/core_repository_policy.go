package securityaudit

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrPolicyNotFound         = errors.New("security audit policy not found")
	ErrDecisionNotFound       = errors.New("security audit decision not found")
	ErrActionNotFound         = errors.New("security audit action not found")
	ErrCaseNotFound           = errors.New("security audit case not found")
	ErrExceptionNotFound      = errors.New("security audit exception not found")
	ErrExceptionReasonInvalid = errors.New("security audit exception revocation reason invalid")
	ErrNotificationNotFound   = errors.New("security audit notification not found")
	ErrInvalidTransition      = errors.New("security audit invalid state transition")
	ErrPolicyReasonInvalid    = errors.New("security audit policy transition reason invalid")
)

func newSecurityID(prefix string) string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err == nil {
		return prefix + "_" + hex.EncodeToString(raw[:])
	}
	fallback := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", prefix, time.Now().UnixNano())))
	return prefix + "_" + hex.EncodeToString(fallback[:16])
}

func (r *PostgreSQLRepository) CreatePolicyVersion(ctx context.Context, request CreatePolicyRequest, actorID int64) (*PolicyVersion, error) {
	policyKey := strings.TrimSpace(request.PolicyKey)
	config := canonicalSecurityPolicy(request.Config)
	digest, raw, err := policyDigest(config)
	if err != nil {
		return nil, err
	}
	validationErrors := validateSecurityPolicy(policyKey, config)
	validationRaw, _ := json.Marshal(validationErrors)

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "security-audit-policy:"+policyKey); err != nil {
		return nil, err
	}
	var version int64
	if err = tx.QueryRowContext(ctx, `
SELECT COALESCE(MAX(version),0)+1
FROM security_audit_policy_versions
WHERE policy_key=$1`, policyKey).Scan(&version); err != nil {
		return nil, err
	}
	row := tx.QueryRowContext(ctx, `
INSERT INTO security_audit_policy_versions(
    policy_key,version,name,status,priority,config,config_digest,validation_errors,
    change_reason,created_by
) VALUES ($1,$2,$3,'draft',$4,$5::jsonb,$6,$7::jsonb,$8,$9)
RETURNING `+policyColumns("security_audit_policy_versions"),
		policyKey, version, config.Name, config.Priority, raw, digest, validationRaw,
		TrimRunes(strings.TrimSpace(request.ChangeReason), 512), nullableID(actorID))
	policy, err := scanPolicy(row)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return policy, nil
}

func (r *PostgreSQLRepository) ListPolicySummaries(ctx context.Context) ([]PolicySummary, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT
    policy_key,
    (array_agg(name ORDER BY version DESC))[1],
    MAX(version),
    MAX(version) FILTER (WHERE status='active'),
    MAX(version) FILTER (WHERE status='shadow'),
    (array_agg(status ORDER BY version DESC))[1],
    (array_agg(priority ORDER BY version DESC))[1],
    COUNT(*)::int,
    MAX(created_at)
FROM security_audit_policy_versions
GROUP BY policy_key
ORDER BY MAX(priority) DESC, policy_key`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]PolicySummary, 0)
	for rows.Next() {
		var item PolicySummary
		var active, shadow sql.NullInt64
		if err := rows.Scan(
			&item.PolicyKey, &item.Name, &item.Latest, &active, &shadow,
			&item.Status, &item.Priority, &item.VersionCount, &item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if active.Valid {
			item.Active = &active.Int64
		}
		if shadow.Valid {
			item.Shadow = &shadow.Int64
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgreSQLRepository) ListPolicyVersions(ctx context.Context, policyKey string) ([]*PolicyVersion, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT `+policyColumns("p")+`
FROM security_audit_policy_versions p
WHERE p.policy_key=$1
ORDER BY p.version DESC`, strings.TrimSpace(policyKey))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]*PolicyVersion, 0)
	for rows.Next() {
		item, err := scanPolicy(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgreSQLRepository) ListPolicyTransitions(
	ctx context.Context,
	policyKey string,
	limit int,
) ([]PolicyTransition, error) {
	policyKey = strings.TrimSpace(policyKey)
	if limit < 1 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	rows, err := r.db.QueryContext(ctx, `
SELECT id,policy_version_id,policy_key,version,from_status,to_status,actor_id,reason,created_at
FROM security_audit_policy_transitions
WHERE policy_key=$1
ORDER BY created_at DESC,id DESC
LIMIT $2`, policyKey, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]PolicyTransition, 0)
	for rows.Next() {
		var item PolicyTransition
		var actorID sql.NullInt64
		if err := rows.Scan(
			&item.ID, &item.PolicyVersionID, &item.PolicyKey, &item.Version,
			&item.FromStatus, &item.ToStatus, &actorID, &item.Reason, &item.CreatedAt,
		); err != nil {
			return nil, err
		}
		if actorID.Valid {
			item.ActorID = &actorID.Int64
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgreSQLRepository) GetPolicyVersion(ctx context.Context, policyKey string, version int64) (*PolicyVersion, error) {
	item, err := scanPolicy(r.db.QueryRowContext(ctx, `
SELECT `+policyColumns("p")+`
FROM security_audit_policy_versions p
WHERE p.policy_key=$1 AND p.version=$2`, strings.TrimSpace(policyKey), version))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrPolicyNotFound
	}
	return item, err
}

func (r *PostgreSQLRepository) TransitionPolicy(ctx context.Context, policyKey string, version int64, target string, actorID int64, reason string) (*PolicyVersion, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "security-audit-policy:"+policyKey); err != nil {
		return nil, err
	}
	current, err := scanPolicy(tx.QueryRowContext(ctx, `
SELECT `+policyColumns("p")+`
FROM security_audit_policy_versions p
WHERE p.policy_key=$1 AND p.version=$2
FOR UPDATE`, policyKey, version))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrPolicyNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := validatePolicyTransition(current.Status, target); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidTransition, err)
	}
	validationErrors := validateSecurityPolicy(policyKey, current.Config)
	if (target == PolicyStatusValidated || target == PolicyStatusShadow || target == PolicyStatusActive) && len(validationErrors) > 0 {
		return nil, fmt.Errorf("策略校验失败: %s", strings.Join(validationErrors, "; "))
	}
	if target == PolicyStatusActive {
		if _, err = tx.ExecContext(ctx, `
WITH retired AS (
UPDATE security_audit_policy_versions
SET status='retired',retired_at=NOW()
WHERE policy_key=$1 AND status='active' AND version<>$2
RETURNING id,policy_key,version
)
INSERT INTO security_audit_policy_transitions(
    policy_version_id,policy_key,version,from_status,to_status,actor_id,reason
)
SELECT id,policy_key,version,'active','retired',$3,$4
FROM retired`,
			policyKey, version, nullableID(actorID), TrimRunes(strings.TrimSpace(reason), 512)); err != nil {
			return nil, err
		}
	}
	if target == PolicyStatusShadow {
		if _, err = tx.ExecContext(ctx, `
WITH retired AS (
UPDATE security_audit_policy_versions
SET status='retired',retired_at=NOW()
WHERE policy_key=$1 AND status='shadow' AND version<>$2
RETURNING id,policy_key,version
)
INSERT INTO security_audit_policy_transitions(
    policy_version_id,policy_key,version,from_status,to_status,actor_id,reason
)
SELECT id,policy_key,version,'shadow','retired',$3,$4
FROM retired`,
			policyKey, version, nullableID(actorID), TrimRunes(strings.TrimSpace(reason), 512)); err != nil {
			return nil, err
		}
	}
	timeColumn := map[string]string{
		PolicyStatusValidated: "validated_at",
		PolicyStatusShadow:    "shadowed_at",
		PolicyStatusActive:    "activated_at",
		PolicyStatusRetired:   "retired_at",
	}[target]
	query := fmt.Sprintf(`
UPDATE security_audit_policy_versions
SET status=$3,%s=NOW(),change_reason=CASE WHEN $4='' THEN change_reason ELSE $4 END
WHERE policy_key=$1 AND version=$2
RETURNING %s`, timeColumn, policyColumns("security_audit_policy_versions"))
	updated, err := scanPolicy(tx.QueryRowContext(ctx, query, policyKey, version, target, TrimRunes(strings.TrimSpace(reason), 512)))
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO security_audit_policy_transitions(
    policy_version_id,policy_key,version,from_status,to_status,actor_id,reason
) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		updated.ID, updated.PolicyKey, updated.Version, current.Status, target,
		nullableID(actorID), TrimRunes(strings.TrimSpace(reason), 512)); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return updated, nil
}

func (r *PostgreSQLRepository) GetEffectivePolicy(ctx context.Context) (*PolicyVersion, error) {
	item, err := scanPolicy(r.db.QueryRowContext(ctx, `
SELECT `+policyColumns("p")+`
FROM security_audit_policy_versions p
WHERE p.status='active'
ORDER BY p.priority DESC,p.policy_key,p.version DESC
LIMIT 1`))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrPolicyNotFound
	}
	return item, err
}

func policyColumns(alias string) string {
	return alias + `.id,` + alias + `.policy_key,` + alias + `.version,` + alias + `.name,` +
		alias + `.status,` + alias + `.priority,` + alias + `.config,` + alias + `.config_digest,` +
		alias + `.validation_errors,` + alias + `.change_reason,` + alias + `.created_by,` +
		alias + `.created_at,` + alias + `.validated_at,` + alias + `.shadowed_at,` +
		alias + `.activated_at,` + alias + `.retired_at`
}

func scanPolicy(row rowScanner) (*PolicyVersion, error) {
	item := &PolicyVersion{}
	var configRaw, errorsRaw []byte
	var createdBy sql.NullInt64
	var validatedAt, shadowedAt, activatedAt, retiredAt sql.NullTime
	if err := row.Scan(
		&item.ID, &item.PolicyKey, &item.Version, &item.Name, &item.Status, &item.Priority,
		&configRaw, &item.ConfigDigest, &errorsRaw, &item.ChangeReason, &createdBy,
		&item.CreatedAt, &validatedAt, &shadowedAt, &activatedAt, &retiredAt,
	); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(configRaw, &item.Config); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(errorsRaw, &item.ValidationErrors)
	if item.ValidationErrors == nil {
		item.ValidationErrors = []string{}
	}
	if createdBy.Valid {
		item.CreatedBy = &createdBy.Int64
	}
	item.ValidatedAt = nullableTime(validatedAt)
	item.ShadowedAt = nullableTime(shadowedAt)
	item.ActivatedAt = nullableTime(activatedAt)
	item.RetiredAt = nullableTime(retiredAt)
	return item, nil
}

func nullableTime(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	v := value.Time
	return &v
}
