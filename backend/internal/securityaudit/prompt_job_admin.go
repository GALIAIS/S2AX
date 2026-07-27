package securityaudit

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
)

var (
	ErrJobNotFound           = errors.New("prompt audit job not found")
	ErrJobTransitionConflict = errors.New("prompt audit job transition conflict")
	ErrJobPayloadUnavailable = errors.New("prompt audit job payload unavailable")
)

type JobFilter struct {
	Status    string     `json:"status,omitempty"`
	ErrorCode string     `json:"error_code,omitempty"`
	Keyword   string     `json:"keyword,omitempty"`
	StartAt   *time.Time `json:"start_at,omitempty"`
	EndAt     *time.Time `json:"end_at,omitempty"`
}

type JobFailureReasonCount struct {
	ErrorCode string `json:"error_code"`
	Count     int64  `json:"count"`
}

type JobOperation struct {
	ID               int64     `json:"id"`
	JobID            int64     `json:"job_id"`
	Operation        string    `json:"operation"`
	FromStatus       string    `json:"from_status"`
	ToStatus         string    `json:"to_status"`
	ActorID          int64     `json:"actor_id"`
	Reason           string    `json:"reason"`
	PayloadAvailable bool      `json:"payload_available"`
	CreatedAt        time.Time `json:"created_at"`
}

type AdminJob struct {
	Job               *Job           `json:"job"`
	PayloadState      string         `json:"payload_state"`
	PayloadTTLSeconds int64          `json:"payload_ttl_seconds"`
	Operations        []JobOperation `json:"operations,omitempty"`
}

type JobPage struct {
	Items          []*AdminJob             `json:"items"`
	FailureReasons []JobFailureReasonCount `json:"failure_reasons"`
	Total          int64                   `json:"total"`
	Page           int                     `json:"page"`
	PageSize       int                     `json:"page_size"`
	Pages          int                     `json:"pages"`
}

func (r *PostgreSQLRepository) ListJobs(
	ctx context.Context,
	filter JobFilter,
	page int,
	pageSize int,
) (*JobPage, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("prompt audit database unavailable")
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	where, args, err := buildJobWhere(filter)
	if err != nil {
		return nil, err
	}
	var total int64
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM prompt_audit_jobs j`+where, args...).Scan(&total); err != nil {
		return nil, err
	}
	queryArgs := append([]any(nil), args...)
	limitIndex := len(queryArgs) + 1
	queryArgs = append(queryArgs, pageSize, (page-1)*pageSize)
	rows, err := r.db.QueryContext(ctx, `SELECT `+jobColumns("j")+` FROM prompt_audit_jobs j`+where+
		fmt.Sprintf(` ORDER BY j.updated_at DESC,j.id DESC LIMIT $%d OFFSET $%d`, limitIndex, limitIndex+1),
		queryArgs...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]*AdminJob, 0, pageSize)
	for rows.Next() {
		job, scanErr := scanJob(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		job.Snapshot = job.Snapshot.Redacted()
		items = append(items, &AdminJob{Job: job, PayloadState: payloadStateNotApplicable})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	reasons, err := r.listJobFailureReasons(ctx, filter)
	if err != nil {
		return nil, err
	}
	pages := 0
	if total > 0 {
		pages = int((total + int64(pageSize) - 1) / int64(pageSize))
	}
	return &JobPage{
		Items: items, FailureReasons: reasons, Total: total,
		Page: page, PageSize: pageSize, Pages: pages,
	}, nil
}

func (r *PostgreSQLRepository) listJobFailureReasons(
	ctx context.Context,
	filter JobFilter,
) ([]JobFailureReasonCount, error) {
	filter.Status = ""
	where, args, err := buildJobWhere(filter)
	if err != nil {
		return nil, err
	}
	statusClause := " WHERE "
	if where != "" {
		statusClause = " AND "
	}
	rows, err := r.db.QueryContext(ctx, `
SELECT j.last_error_code,COUNT(*)
FROM prompt_audit_jobs j`+where+statusClause+`
  j.status IN ('failed','quarantined')
  AND j.last_error_code<>''
GROUP BY j.last_error_code
ORDER BY COUNT(*) DESC,j.last_error_code
LIMIT 20`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := make([]JobFailureReasonCount, 0, 20)
	for rows.Next() {
		var item JobFailureReasonCount
		if err := rows.Scan(&item.ErrorCode, &item.Count); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *PostgreSQLRepository) ListJobOperations(
	ctx context.Context,
	jobIDs []int64,
) (map[int64][]JobOperation, error) {
	result := make(map[int64][]JobOperation, len(jobIDs))
	if len(jobIDs) == 0 {
		return result, nil
	}
	rows, err := r.db.QueryContext(ctx, `
SELECT id,job_id,operation,from_status,to_status,COALESCE(actor_id,0),reason,payload_available,created_at
FROM prompt_audit_job_operations
WHERE job_id=ANY($1)
ORDER BY created_at DESC,id DESC`, pq.Array(jobIDs))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var item JobOperation
		if err := rows.Scan(
			&item.ID, &item.JobID, &item.Operation, &item.FromStatus, &item.ToStatus,
			&item.ActorID, &item.Reason, &item.PayloadAvailable, &item.CreatedAt,
		); err != nil {
			return nil, err
		}
		result[item.JobID] = append(result[item.JobID], item)
	}
	return result, rows.Err()
}

func (r *PostgreSQLRepository) TransitionJob(
	ctx context.Context,
	jobID int64,
	operation string,
	actorID int64,
	reason string,
	payloadAvailable bool,
) (*Job, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("prompt audit database unavailable")
	}
	if jobID <= 0 {
		return nil, ErrJobNotFound
	}
	operation = strings.TrimSpace(operation)
	reason = strings.TrimSpace(reason)
	if len([]rune(reason)) < 3 || len([]rune(reason)) > 256 {
		return nil, ErrEvidenceReasonInvalid
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	var fromStatus string
	if err := tx.QueryRowContext(ctx, `
SELECT status FROM prompt_audit_jobs WHERE id=$1 FOR UPDATE`, jobID).Scan(&fromStatus); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrJobNotFound
		}
		return nil, err
	}
	toStatus, updateSQL, err := jobTransition(operation, fromStatus, payloadAvailable)
	if err != nil {
		return nil, err
	}
	job, err := scanJob(tx.QueryRowContext(ctx, updateSQL+` RETURNING `+jobColumns("prompt_audit_jobs"), jobID))
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO prompt_audit_job_operations(
    job_id,operation,from_status,to_status,actor_id,reason,payload_available
) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		jobID, operation, fromStatus, toStatus, nullableID(actorID), reason, payloadAvailable); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return job, nil
}

func jobTransition(operation string, fromStatus string, payloadAvailable bool) (string, string, error) {
	switch operation {
	case "retry":
		if fromStatus != "failed" && fromStatus != "quarantined" {
			return "", "", ErrJobTransitionConflict
		}
		if !payloadAvailable {
			return "", "", ErrJobPayloadUnavailable
		}
		return "queued", `
UPDATE prompt_audit_jobs
SET status='queued',attempts=0,max_attempts=GREATEST(max_attempts,3),
    claim_version=claim_version+1,next_attempt_at=NOW(),processing_started_at=NULL,
    processed_at=NULL,last_error_code='',last_error_message='',updated_at=NOW()
WHERE id=$1`, nil
	case "quarantine":
		if fromStatus != "failed" {
			return "", "", ErrJobTransitionConflict
		}
		return "quarantined", `
UPDATE prompt_audit_jobs
SET status='quarantined',updated_at=NOW()
WHERE id=$1`, nil
	case "discard":
		if fromStatus != "failed" && fromStatus != "quarantined" {
			return "", "", ErrJobTransitionConflict
		}
		return "discarded", `
UPDATE prompt_audit_jobs
SET status='discarded',processed_at=COALESCE(processed_at,NOW()),updated_at=NOW()
WHERE id=$1`, nil
	default:
		return "", "", ErrJobTransitionConflict
	}
}

func buildJobWhere(filter JobFilter) (string, []any, error) {
	status := strings.TrimSpace(filter.Status)
	if status != "" && !validJobAdminStatus(status) {
		return "", nil, ErrJobTransitionConflict
	}
	clauses := make([]string, 0, 5)
	args := make([]any, 0, 5)
	add := func(format string, value any) {
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf(format, len(args)))
	}
	if status != "" {
		add("j.status=$%d", status)
	}
	if errorCode := strings.TrimSpace(filter.ErrorCode); errorCode != "" {
		add("j.last_error_code=$%d", TrimRunes(errorCode, 64))
	}
	if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
		args = append(args, TrimRunes(keyword, 128))
		index := len(args)
		clauses = append(clauses, fmt.Sprintf(`(
            j.request_id ILIKE '%%'||$%[1]d||'%%'
            OR j.username_snapshot ILIKE '%%'||$%[1]d||'%%'
            OR j.user_email_snapshot ILIKE '%%'||$%[1]d||'%%'
            OR j.model ILIKE '%%'||$%[1]d||'%%'
            OR j.prompt_hash ILIKE '%%'||$%[1]d||'%%'
            OR j.last_error_code ILIKE '%%'||$%[1]d||'%%'
        )`, index))
	}
	if filter.StartAt != nil {
		add("j.created_at >= $%d", filter.StartAt.UTC())
	}
	if filter.EndAt != nil {
		add("j.created_at <= $%d", filter.EndAt.UTC())
	}
	if len(clauses) == 0 {
		return "", args, nil
	}
	return " WHERE " + strings.Join(clauses, " AND "), args, nil
}

func validJobAdminStatus(status string) bool {
	switch status {
	case "staging", "queued", "processing", "retry", "done", "failed", "quarantined", "discarded":
		return true
	default:
		return false
	}
}

const (
	payloadStateAvailable     = "available"
	payloadStateExpired       = "expired"
	payloadStateUnknown       = "unknown"
	payloadStateNotApplicable = "not_applicable"
)
