package invocationarchive

import (
	"errors"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
)

type AdminHandler struct{ service *Service }

func NewAdminHandler(service *Service) *AdminHandler { return &AdminHandler{service: service} }

func (h *AdminHandler) GetConfig(c *gin.Context) {
	response.Success(c, h.service.GetConfig().Public())
}

func (h *AdminHandler) UpdateConfig(c *gin.Context) {
	var request UpdateConfigRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		setArchiveAudit(c, "failed", "invocation_archive_invalid_config_request", nil)
		response.ErrorFrom(c, infraerrors.BadRequest("invocation_archive_invalid_config_request", "调用归档配置请求无效"))
		return
	}
	config, err := h.service.SaveConfig(c.Request.Context(), request, archiveAdminID(c))
	if err != nil {
		setArchiveAudit(c, "failed", infraerrors.Reason(err), map[string]any{"config_version": request.ExpectedConfigVersion})
		response.ErrorFrom(c, err)
		return
	}
	setArchiveAudit(c, "success", "", map[string]any{
		"config_version": config.ConfigVersion, "archive_mode": string(config.DefaultMode),
		"direct_view_enabled": config.DirectViewEnabled, "rule_count": len(config.Rules),
	})
	response.Success(c, config.Public())
}

func (h *AdminHandler) GetRuntime(c *gin.Context) { response.Success(c, h.service.Runtime()) }

func (h *AdminHandler) ListSubjects(c *gin.Context) {
	scope := Scope(strings.TrimSpace(c.Query("scope")))
	items, err := h.service.ListSubjects(c.Request.Context(), scope, c.Query("q"), positiveQuery(c, "limit", 20, 50))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"items": items})
}

func (h *AdminHandler) ListRecords(c *gin.Context) {
	page := positiveQuery(c, "page", 1, 1000000)
	pageSize := positiveQuery(c, "page_size", 20, 100)
	filter, err := archiveFilterFromQuery(c)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	result, err := h.service.ListRecords(c.Request.Context(), filter, page, pageSize)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *AdminHandler) GetRecord(c *gin.Context) {
	id, ok := archiveRecordID(c)
	if !ok {
		return
	}
	record, err := h.service.GetRecord(c.Request.Context(), id)
	if respondArchiveRecordError(c, err) {
		return
	}
	response.Success(c, record)
}

func (h *AdminHandler) ListAccessLogs(c *gin.Context) {
	id, ok := archiveRecordID(c)
	if !ok {
		return
	}
	// Access evidence deliberately outlives its archived payload. Do not require
	// the record to remain present here: an administrator must still be able to
	// inspect a lawful direct-view trail after the payload was deleted.
	items, err := h.service.ListAccessLogs(c.Request.Context(), id, positiveQuery(c, "limit", 50, 100))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"items": items})
}

type revealRequest struct {
	Reason string `json:"reason"`
}

func (h *AdminHandler) RevealRecord(c *gin.Context) {
	id, ok := archiveRecordID(c)
	if !ok {
		return
	}
	var request revealRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		setArchiveAudit(c, "failed", "invocation_archive_invalid_reveal_request", map[string]any{"archive_record_id": id})
		response.ErrorFrom(c, infraerrors.BadRequest("invocation_archive_invalid_reveal_request", "调用归档查看请求无效"))
		return
	}
	c.Header("Cache-Control", "no-store, no-cache, must-revalidate, private")
	c.Header("Pragma", "no-cache")
	reveal, err := h.service.RevealRecord(c.Request.Context(), id, archiveAdminID(c), request.Reason, middleware.SecurityClientIP(c), c.Request.UserAgent())
	if err != nil {
		setArchiveAudit(c, "failed", archiveErrorCode(err), map[string]any{"archive_record_id": id, "reason_length": len([]rune(strings.TrimSpace(request.Reason)))})
		respondArchiveRecordError(c, err)
		return
	}
	setArchiveAudit(c, "success", "", map[string]any{"archive_record_id": id, "reason_length": len([]rune(strings.TrimSpace(request.Reason)))})
	response.Success(c, reveal)
}

func (h *AdminHandler) DeleteRecord(c *gin.Context) {
	id, ok := archiveRecordID(c)
	if !ok {
		return
	}
	deleted, err := h.service.DeleteRecord(c.Request.Context(), id)
	if err != nil {
		setArchiveAudit(c, "failed", archiveErrorCode(err), map[string]any{"archive_record_id": id})
		respondArchiveRecordError(c, err)
		return
	}
	setArchiveAudit(c, "success", "", map[string]any{"archive_record_id": id, "archive_deleted": deleted})
	response.Success(c, gin.H{"deleted": deleted})
}

type batchDeleteRequest struct {
	IDs []int64 `json:"ids"`
}

func (h *AdminHandler) BatchDelete(c *gin.Context) {
	var request batchDeleteRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		setArchiveAudit(c, "failed", "invocation_archive_invalid_batch_delete_request", nil)
		response.ErrorFrom(c, infraerrors.BadRequest("invocation_archive_invalid_batch_delete_request", "归档批量删除请求无效"))
		return
	}
	deleted, err := h.service.DeleteRecords(c.Request.Context(), request.IDs)
	if err != nil {
		setArchiveAudit(c, "failed", archiveErrorCode(err), map[string]any{"requested_count": len(request.IDs)})
		response.ErrorFrom(c, err)
		return
	}
	setArchiveAudit(c, "success", "", map[string]any{"requested_count": len(request.IDs), "archive_deleted": deleted})
	response.Success(c, gin.H{"deleted": deleted})
}

func archiveFilterFromQuery(c *gin.Context) (RecordFilter, error) {
	filter := RecordFilter{
		Query: c.Query("q"), Mode: Mode(strings.TrimSpace(c.Query("mode"))), Outcome: strings.TrimSpace(c.Query("outcome")),
	}
	for _, entry := range []struct {
		name   string
		target *int64
	}{{"user_id", &filter.UserID}, {"group_id", &filter.GroupID}, {"api_key_id", &filter.APIKeyID}} {
		value := strings.TrimSpace(c.Query(entry.name))
		if value == "" {
			continue
		}
		id, err := strconv.ParseInt(value, 10, 64)
		if err != nil || id <= 0 {
			return RecordFilter{}, infraerrors.BadRequest("invocation_archive_filter_invalid", "归档筛选条件无效")
		}
		*entry.target = id
	}
	for _, entry := range []struct {
		name   string
		target **time.Time
	}{{"from", &filter.From}, {"to", &filter.To}} {
		value := strings.TrimSpace(c.Query(entry.name))
		if value == "" {
			continue
		}
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return RecordFilter{}, infraerrors.BadRequest("invocation_archive_time_invalid", "归档时间筛选必须使用 RFC3339 格式")
		}
		*entry.target = &parsed
	}
	if filter.From != nil && filter.To != nil && filter.To.Before(*filter.From) {
		return RecordFilter{}, infraerrors.BadRequest("invocation_archive_time_range_invalid", "归档时间范围无效")
	}
	return filter, nil
}

func archiveRecordID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.ErrorFrom(c, infraerrors.BadRequest("invocation_archive_record_id_invalid", "归档记录 ID 无效"))
		return 0, false
	}
	return id, true
}

func archiveAdminID(c *gin.Context) int64 {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		return 0
	}
	return subject.UserID
}

func positiveQuery(c *gin.Context, name string, fallback, maximum int) int {
	value := strings.TrimSpace(c.Query(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		return fallback
	}
	if parsed > maximum {
		return maximum
	}
	return parsed
}

func respondArchiveRecordError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, ErrRecordNotFound):
		response.ErrorFrom(c, infraerrors.NotFound("invocation_archive_record_not_found", "调用归档记录不存在"))
	case errors.Is(err, ErrDirectViewDisabled):
		response.ErrorFrom(c, infraerrors.Forbidden("invocation_archive_direct_view_disabled", "管理员尚未启用调用归档直接查看"))
	case errors.Is(err, ErrPayloadExpired):
		response.ErrorFrom(c, infraerrors.Conflict("invocation_archive_payload_expired", "调用归档载荷已过期"))
	case errors.Is(err, ErrPayloadUnavailable):
		response.ErrorFrom(c, infraerrors.Conflict("invocation_archive_payload_unavailable", "调用归档载荷不可用"))
	case errors.Is(err, ErrInvalidRevealReason):
		response.ErrorFrom(c, infraerrors.BadRequest("invocation_archive_reveal_reason_invalid", "查看理由需为 3-256 个字符"))
	default:
		response.ErrorFrom(c, err)
	}
	return true
}

func archiveErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrRecordNotFound):
		return "invocation_archive_record_not_found"
	case errors.Is(err, ErrDirectViewDisabled):
		return "invocation_archive_direct_view_disabled"
	case errors.Is(err, ErrPayloadExpired):
		return "invocation_archive_payload_expired"
	case errors.Is(err, ErrPayloadUnavailable):
		return "invocation_archive_payload_unavailable"
	case errors.Is(err, ErrInvalidRevealReason):
		return "invocation_archive_reveal_reason_invalid"
	default:
		return "invocation_archive_request_failed"
	}
}

func setArchiveAudit(c *gin.Context, result, errorCode string, fields map[string]any) {
	if result != "" {
		middleware.SetAuditExtra(c, map[string]any{"result": result, "error_code": errorCode})
	}
	middleware.SetAuditAction(c, "admin.invocation_archive.operation")
	middleware.SetAuditExtra(c, fields)
}
