package invocationarchive

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

const recordColumns = `
id,created_at,completed_at,expires_at,config_version,mode,transport,websocket_turn,
user_id,user_label,api_key_id,api_key_name,group_id,group_name,
request_id,client_request_id,method,request_path,model,client_ip,user_agent,
request_content_type,response_content_type,http_status,
request_total_bytes,request_captured_bytes,request_truncated,request_status,
response_total_bytes,response_captured_bytes,response_truncated,response_status,outcome`

func (s *Service) persistCandidate(ctx context.Context, candidate archiveCandidate) error {
	if s == nil || s.db == nil {
		return errors.New("invocation archive database unavailable")
	}
	requestCiphertext, requestStatus, requestErr := protectCapturedPayload(s.encryptor, candidate.request)
	responseCiphertext, responseStatus, responseErr := protectCapturedPayload(s.encryptor, candidate.response)
	if requestErr != nil {
		requestStatus = "encryption_failed"
	}
	if responseErr != nil {
		responseStatus = "encryption_failed"
	}

	metadata := candidate.identity
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO invocation_archive_records(
			created_at,completed_at,expires_at,config_version,mode,transport,websocket_turn,
			user_id,user_label,api_key_id,api_key_name,group_id,group_name,
			request_id,client_request_id,method,request_path,model,client_ip,user_agent,
			request_content_type,response_content_type,http_status,
			request_total_bytes,request_captured_bytes,request_truncated,request_status,request_ciphertext,
			response_total_bytes,response_captured_bytes,response_truncated,response_status,response_ciphertext,outcome
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,
			$8,$9,$10,$11,$12,$13,
			$14,$15,$16,$17,$18,$19,$20,
			$21,$22,$23,
			$24,$25,$26,$27,$28,
			$29,$30,$31,$32,$33,$34)`,
		candidate.createdAt, candidate.completedAt, candidate.expiresAt, candidate.configVersion, candidate.mode, candidate.transport, candidate.websocketTurn,
		metadata.userID, metadata.userLabel, metadata.apiKeyID, metadata.apiKeyName, metadata.groupID, metadata.groupName,
		candidate.requestID, candidate.clientRequestID, candidate.method, candidate.path, candidateModel(candidate), candidate.clientIP, candidate.userAgent,
		candidate.request.contentType, candidate.response.contentType, candidate.httpStatus,
		candidate.request.total, int64(len(candidate.request.bytes)), candidate.request.truncated, requestStatus, requestCiphertext,
		candidate.response.total, int64(len(candidate.response.bytes)), candidate.response.truncated, responseStatus, responseCiphertext, candidate.outcome,
	)
	if err != nil {
		return err
	}
	return nil
}

func candidateModel(candidate archiveCandidate) string {
	if candidate.model != "" {
		return trimText(candidate.model, 255)
	}
	return extractModel(candidate.request.bytes, candidate.path)
}

type archiveRecordIdentity struct {
	userID     any
	userLabel  string
	apiKeyID   any
	apiKeyName string
	groupID    any
	groupName  string
}

func archiveIdentity(apiKey *service.APIKey) archiveRecordIdentity {
	if apiKey == nil {
		return archiveRecordIdentity{}
	}
	identity := archiveRecordIdentity{
		userID: nullableID(apiKey.UserID), apiKeyID: nullableID(apiKey.ID), apiKeyName: trimText(apiKey.Name, 255),
	}
	if apiKey.User != nil {
		identity.userLabel = trimText(apiKey.User.Email, 255)
		if apiKey.User.Username != "" {
			identity.userLabel = trimText(apiKey.User.Username+" <"+apiKey.User.Email+">", 255)
		}
	}
	if apiKey.GroupID != nil && *apiKey.GroupID > 0 {
		identity.groupID = *apiKey.GroupID
	}
	if apiKey.Group != nil {
		identity.groupName = trimText(apiKey.Group.Name, 255)
	}
	return identity
}

func protectCapturedPayload(encryptor service.SecretEncryptor, payload capturedPayload) (string, string, error) {
	if payload.status != "captured" {
		return "", payload.status, nil
	}
	ciphertext, _, err := protectPayload(encryptor, payload.bytes)
	if err != nil {
		return "", "encryption_failed", err
	}
	return ciphertext, payload.status, nil
}

func (s *Service) ListRecords(ctx context.Context, filter RecordFilter, page, pageSize int) (*RecordPage, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("invocation archive database unavailable")
	}
	if page < 1 || pageSize < 1 || pageSize > 100 {
		return nil, infraerrors.BadRequest("invocation_archive_pagination_invalid", "分页参数无效")
	}
	where, args, err := recordWhere(filter)
	if err != nil {
		return nil, err
	}
	var total int64
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM invocation_archive_records WHERE "+where, args...).Scan(&total); err != nil {
		return nil, err
	}
	args = append(args, pageSize, (page-1)*pageSize)
	query := "SELECT " + recordColumns + " FROM invocation_archive_records WHERE " + where +
		fmt.Sprintf(" ORDER BY created_at DESC,id DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]Record, 0, pageSize)
	for rows.Next() {
		record, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &RecordPage{Items: items, Page: page, PageSize: pageSize, Total: total}, nil
}

func (s *Service) GetRecord(ctx context.Context, id int64) (*Record, error) {
	stored, err := s.getStoredRecord(ctx, id)
	if err != nil {
		return nil, err
	}
	record := stored.Record
	return &record, nil
}

func (s *Service) getStoredRecord(ctx context.Context, id int64) (*storedRecord, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("invocation archive database unavailable")
	}
	if id <= 0 {
		return nil, ErrRecordNotFound
	}
	row := s.db.QueryRowContext(ctx, "SELECT "+recordColumns+`,request_ciphertext,response_ciphertext
		FROM invocation_archive_records WHERE id=$1`, id)
	stored, err := scanStoredRecord(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrRecordNotFound
	}
	if err != nil {
		return nil, err
	}
	return &stored, nil
}

func (s *Service) ListAccessLogs(ctx context.Context, recordID int64, limit int) ([]AccessLog, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("invocation archive database unavailable")
	}
	if recordID <= 0 {
		return nil, ErrRecordNotFound
	}
	if limit < 1 || limit > 100 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT l.id,l.record_id,l.admin_id,COALESCE(u.email,''),l.reason,l.outcome,l.client_ip,l.user_agent,l.created_at
		FROM invocation_archive_access_logs l
		LEFT JOIN users u ON u.id=l.admin_id
		WHERE l.record_id=$1
		ORDER BY l.created_at DESC,l.id DESC
		LIMIT $2`, recordID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]AccessLog, 0)
	for rows.Next() {
		var item AccessLog
		var adminID sql.NullInt64
		if err := rows.Scan(&item.ID, &item.RecordID, &adminID, &item.AdminName, &item.Reason, &item.Outcome, &item.ClientIP, &item.UserAgent, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.AdminID = nullableInt64Pointer(adminID)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) RevealRecord(ctx context.Context, id, adminID int64, reason, clientIP, userAgent string) (*Reveal, error) {
	if adminID <= 0 {
		return nil, infraerrors.Forbidden("invocation_archive_admin_required", "管理员身份无效")
	}
	reason = strings.TrimSpace(reason)
	length := len([]rune(reason))
	if length < 3 || length > 256 {
		return nil, ErrInvalidRevealReason
	}
	stored, err := s.getStoredRecord(ctx, id)
	if err != nil {
		return nil, err
	}
	if !s.activeConfig().DirectViewEnabled {
		_ = s.recordAccess(ctx, id, adminID, reason, "direct_view_disabled", clientIP, userAgent)
		return nil, ErrDirectViewDisabled
	}
	if !time.Now().UTC().Before(stored.ExpiresAt) {
		_ = s.recordAccess(ctx, id, adminID, reason, "expired", clientIP, userAgent)
		return nil, ErrPayloadExpired
	}
	request, err := revealPayload(s.encryptor, stored.requestCiphertext, stored.RequestContentType, stored.RequestStatus, stored.RequestTotalBytes, stored.RequestCapturedBytes, stored.RequestTruncated)
	if err != nil {
		_ = s.recordAccess(ctx, id, adminID, reason, "decrypt_failed", clientIP, userAgent)
		return nil, ErrPayloadUnavailable
	}
	response, err := revealPayload(s.encryptor, stored.responseCiphertext, stored.ResponseContentType, stored.ResponseStatus, stored.ResponseTotalBytes, stored.ResponseCapturedBytes, stored.ResponseTruncated)
	if err != nil {
		_ = s.recordAccess(ctx, id, adminID, reason, "decrypt_failed", clientIP, userAgent)
		return nil, ErrPayloadUnavailable
	}
	if !request.Available && !response.Available {
		_ = s.recordAccess(ctx, id, adminID, reason, "unavailable", clientIP, userAgent)
		return nil, ErrPayloadUnavailable
	}
	if err := s.recordAccess(ctx, id, adminID, reason, "revealed", clientIP, userAgent); err != nil {
		return nil, fmt.Errorf("record invocation archive reveal before response: %w", err)
	}
	return &Reveal{RecordID: id, RevealedAt: time.Now().UTC(), Request: request, Response: response}, nil
}

func (s *Service) DeleteRecord(ctx context.Context, id int64) (int64, error) {
	if s == nil || s.db == nil {
		return 0, errors.New("invocation archive database unavailable")
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM invocation_archive_records WHERE id=$1`, id)
	if err != nil {
		return 0, err
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if deleted == 0 {
		return 0, ErrRecordNotFound
	}
	return deleted, nil
}

func (s *Service) DeleteRecords(ctx context.Context, ids []int64) (int64, error) {
	if s == nil || s.db == nil {
		return 0, errors.New("invocation archive database unavailable")
	}
	unique := uniquePositiveIDs(ids)
	if len(unique) == 0 || len(unique) > 100 {
		return 0, infraerrors.BadRequest("invocation_archive_record_ids_invalid", "归档记录 ID 列表无效")
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM invocation_archive_records WHERE id=ANY($1)`, pq.Array(unique))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *Service) ListSubjects(ctx context.Context, scope Scope, query string, limit int) ([]Subject, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("invocation archive database unavailable")
	}
	if !validScope(scope) {
		return nil, infraerrors.BadRequest("invocation_archive_subject_scope_invalid", "归档范围类型无效")
	}
	if limit < 1 || limit > 50 {
		limit = 20
	}
	needle := "%" + trimText(query, 128) + "%"
	subjectID, _ := strconv.ParseInt(strings.TrimSpace(query), 10, 64)
	var rows *sql.Rows
	var err error
	switch scope {
	case ScopeUser:
		rows, err = s.db.QueryContext(ctx, `
			SELECT id,COALESCE(NULLIF(username,''),email),email
			FROM users
			WHERE deleted_at IS NULL AND (id=$2 OR email ILIKE $1 OR username ILIKE $1)
			ORDER BY email ASC,id ASC LIMIT $3`, needle, subjectID, limit)
	case ScopeGroup:
		rows, err = s.db.QueryContext(ctx, `
			SELECT id,name,'' FROM groups
			WHERE deleted_at IS NULL AND (id=$2 OR name ILIKE $1)
			ORDER BY name ASC,id ASC LIMIT $3`, needle, subjectID, limit)
	case ScopeAPIKey:
		rows, err = s.db.QueryContext(ctx, `
			SELECT k.id,k.name,COALESCE(u.email,'')
			FROM api_keys k LEFT JOIN users u ON u.id=k.user_id
			WHERE k.deleted_at IS NULL AND (k.id=$2 OR k.name ILIKE $1 OR u.email ILIKE $1)
			ORDER BY k.name ASC,k.id ASC LIMIT $3`, needle, subjectID, limit)
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]Subject, 0)
	for rows.Next() {
		var item Subject
		if err := rows.Scan(&item.ID, &item.Label, &item.Secondary); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) recordAccess(ctx context.Context, recordID, adminID int64, reason, outcome, clientIP, userAgent string) error {
	if s == nil || s.db == nil {
		return errors.New("invocation archive database unavailable")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO invocation_archive_access_logs(record_id,admin_id,reason,outcome,client_ip,user_agent)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		recordID, nullableID(adminID), trimText(reason, 256), trimText(outcome, 64), trimText(clientIP, 64), trimText(userAgent, 512))
	return err
}

func (s *Service) deleteExpired(ctx context.Context, now time.Time, limit int) (int, error) {
	result, err := s.db.ExecContext(ctx, `
		WITH expired AS (
			SELECT id FROM invocation_archive_records
			WHERE expires_at <= $1
			ORDER BY expires_at ASC,id ASC
			LIMIT $2 FOR UPDATE SKIP LOCKED
		)
		DELETE FROM invocation_archive_records r
		USING expired WHERE r.id=expired.id`, now, limit)
	if err != nil {
		return 0, err
	}
	deleted, err := result.RowsAffected()
	return int(deleted), err
}

func (s *Service) deleteExpiredAccessLogs(ctx context.Context, before time.Time, limit int) error {
	_, err := s.db.ExecContext(ctx, `
		WITH expired AS (
			SELECT id FROM invocation_archive_access_logs
			WHERE created_at < $1
			ORDER BY created_at ASC,id ASC
			LIMIT $2 FOR UPDATE SKIP LOCKED
		)
		DELETE FROM invocation_archive_access_logs a
		USING expired WHERE a.id=expired.id`, before, limit)
	return err
}

func recordWhere(filter RecordFilter) (string, []any, error) {
	clauses := []string{"1=1"}
	args := make([]any, 0, 8)
	add := func(clause string, value any) {
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf(clause, len(args)))
	}
	if query := trimText(filter.Query, 128); query != "" {
		add("(user_label ILIKE $%d OR api_key_name ILIKE $%d OR group_name ILIKE $%d OR model ILIKE $%d OR request_id ILIKE $%d OR client_request_id ILIKE $%d)", "%"+query+"%")
		// The query predicate needs the same parameter six times; PostgreSQL permits
		// a single placeholder to be referenced repeatedly, avoiding duplicate args.
		last := len(args)
		clauses[len(clauses)-1] = fmt.Sprintf("(user_label ILIKE $%d OR api_key_name ILIKE $%d OR group_name ILIKE $%d OR model ILIKE $%d OR request_id ILIKE $%d OR client_request_id ILIKE $%d)", last, last, last, last, last, last)
	}
	if filter.Mode != "" {
		if !validMode(filter.Mode) {
			return "", nil, infraerrors.BadRequest("invocation_archive_mode_invalid", "归档模式筛选无效")
		}
		add("mode=$%d", filter.Mode)
	}
	if filter.Outcome != "" {
		if !validOutcome(filter.Outcome) {
			return "", nil, infraerrors.BadRequest("invocation_archive_outcome_invalid", "归档结果筛选无效")
		}
		add("outcome=$%d", filter.Outcome)
	}
	if filter.UserID > 0 {
		add("user_id=$%d", filter.UserID)
	}
	if filter.GroupID > 0 {
		add("group_id=$%d", filter.GroupID)
	}
	if filter.APIKeyID > 0 {
		add("api_key_id=$%d", filter.APIKeyID)
	}
	if filter.From != nil {
		add("created_at >= $%d", filter.From.UTC())
	}
	if filter.To != nil {
		add("created_at <= $%d", filter.To.UTC())
	}
	return strings.Join(clauses, " AND "), args, nil
}

func validOutcome(value string) bool {
	switch value {
	case "completed", "client_error", "server_error", "websocket_error":
		return true
	default:
		return false
	}
}

func scanRecord(scanner interface{ Scan(...any) error }) (Record, error) {
	var record Record
	var userID, apiKeyID, groupID sql.NullInt64
	err := scanner.Scan(
		&record.ID, &record.CreatedAt, &record.CompletedAt, &record.ExpiresAt, &record.ConfigVersion, &record.Mode, &record.Transport, &record.WebSocketTurn,
		&userID, &record.UserLabel, &apiKeyID, &record.APIKeyName, &groupID, &record.GroupName,
		&record.RequestID, &record.ClientRequestID, &record.Method, &record.Path, &record.Model, &record.ClientIP, &record.UserAgent,
		&record.RequestContentType, &record.ResponseContentType, &record.HTTPStatus,
		&record.RequestTotalBytes, &record.RequestCapturedBytes, &record.RequestTruncated, &record.RequestStatus,
		&record.ResponseTotalBytes, &record.ResponseCapturedBytes, &record.ResponseTruncated, &record.ResponseStatus, &record.Outcome,
	)
	if err != nil {
		return Record{}, err
	}
	record.UserID = nullableInt64Pointer(userID)
	record.APIKeyID = nullableInt64Pointer(apiKeyID)
	record.GroupID = nullableInt64Pointer(groupID)
	return record, nil
}

func scanStoredRecord(scanner interface{ Scan(...any) error }) (storedRecord, error) {
	var stored storedRecord
	var userID, apiKeyID, groupID sql.NullInt64
	err := scanner.Scan(
		&stored.ID, &stored.CreatedAt, &stored.CompletedAt, &stored.ExpiresAt, &stored.ConfigVersion, &stored.Mode, &stored.Transport, &stored.WebSocketTurn,
		&userID, &stored.UserLabel, &apiKeyID, &stored.APIKeyName, &groupID, &stored.GroupName,
		&stored.RequestID, &stored.ClientRequestID, &stored.Method, &stored.Path, &stored.Model, &stored.ClientIP, &stored.UserAgent,
		&stored.RequestContentType, &stored.ResponseContentType, &stored.HTTPStatus,
		&stored.RequestTotalBytes, &stored.RequestCapturedBytes, &stored.RequestTruncated, &stored.RequestStatus,
		&stored.ResponseTotalBytes, &stored.ResponseCapturedBytes, &stored.ResponseTruncated, &stored.ResponseStatus, &stored.Outcome,
		&stored.requestCiphertext, &stored.responseCiphertext,
	)
	if err != nil {
		return storedRecord{}, err
	}
	stored.UserID = nullableInt64Pointer(userID)
	stored.APIKeyID = nullableInt64Pointer(apiKeyID)
	stored.GroupID = nullableInt64Pointer(groupID)
	return stored, nil
}

func nullableInt64Pointer(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	result := value.Int64
	return &result
}

func uniquePositiveIDs(ids []int64) []int64 {
	seen := make(map[int64]struct{}, len(ids))
	result := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}
