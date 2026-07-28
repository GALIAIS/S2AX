// Package invocationarchive stores opt-in gateway request/response copies for
// administrator review. Payloads are encrypted before they leave the worker.
package invocationarchive

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	SettingKeyConfig = "invocation_archive_config"

	defaultRetentionDays              = 7
	defaultMaxRequestBytes            = 1 << 20
	defaultMaxResponseBytes           = 4 << 20
	defaultCompressionAfterHours      = 24
	defaultCompressionMinBytes        = 8 << 10
	defaultCompressionTriggerBytes    = 128 << 20
	defaultCompressionTriggerRecords  = 500
	defaultCompressionBatchSize       = 25
	defaultCompressionIntervalMinutes = 60
	minCaptureBytes                   = 1 << 10
	// Match the default gateway request ceiling while keeping each archive
	// explicitly bounded so an unending stream cannot exhaust the process.
	maxCaptureBytes             = 256 << 20
	maxRetentionDays            = 3650
	maxScopeRules               = 500
	maxCompressionAfterHours    = 8760
	maxCompressionTriggerBytes  = int64(1) << 40
	maxCompressionTriggerRecord = 1000000
	maxCompressionBatchSize     = 100
	maxCompressionInterval      = 1440
)

type Mode string

const (
	ModeOff         Mode = "off"
	ModeRequestOnly Mode = "request_only"
	ModeFull        Mode = "full"
)

type Scope string

const (
	ScopeUser   Scope = "user"
	ScopeGroup  Scope = "group"
	ScopeAPIKey Scope = "api_key"
)

type ScopeRule struct {
	Scope     Scope `json:"scope" binding:"required"`
	SubjectID int64 `json:"subject_id" binding:"required"`
	Mode      Mode  `json:"mode" binding:"required"`
}

// CompressionConfig controls background, post-write compression. Compression
// always happens before encryption and never runs on the gateway request path.
// A zero time/capacity/count trigger disables that individual trigger.
type CompressionConfig struct {
	Enabled         bool  `json:"enabled"`
	AfterHours      int   `json:"after_hours"`
	MinBytes        int   `json:"min_bytes"`
	TriggerBytes    int64 `json:"trigger_bytes"`
	TriggerRecords  int   `json:"trigger_records"`
	BatchSize       int   `json:"batch_size"`
	IntervalMinutes int   `json:"interval_minutes"`
}

type Config struct {
	ConfigVersion     int64             `json:"config_version"`
	DefaultMode       Mode              `json:"default_mode"`
	RetentionDays     int               `json:"retention_days"`
	MaxRequestBytes   int               `json:"max_request_bytes"`
	MaxResponseBytes  int               `json:"max_response_bytes"`
	DirectViewEnabled bool              `json:"direct_view_enabled"`
	Compression       CompressionConfig `json:"compression"`
	Rules             []ScopeRule       `json:"rules"`
	UpdatedAt         time.Time         `json:"updated_at"`
	UpdatedBy         int64             `json:"updated_by"`
}

type UpdateConfigRequest struct {
	ExpectedConfigVersion int64             `json:"expected_config_version" binding:"required"`
	DefaultMode           Mode              `json:"default_mode" binding:"required"`
	RetentionDays         int               `json:"retention_days"`
	MaxRequestBytes       int               `json:"max_request_bytes"`
	MaxResponseBytes      int               `json:"max_response_bytes"`
	DirectViewEnabled     bool              `json:"direct_view_enabled"`
	Compression           CompressionConfig `json:"compression"`
	Rules                 []ScopeRule       `json:"rules"`
}

type Subject struct {
	ID        int64  `json:"id"`
	Label     string `json:"label"`
	Secondary string `json:"secondary,omitempty"`
}

type RecordFilter struct {
	Query    string
	Mode     Mode
	Outcome  string
	UserID   int64
	GroupID  int64
	APIKeyID int64
	From     *time.Time
	To       *time.Time
}

type Record struct {
	ID                    int64     `json:"id"`
	CreatedAt             time.Time `json:"created_at"`
	CompletedAt           time.Time `json:"completed_at"`
	ExpiresAt             time.Time `json:"expires_at"`
	ConfigVersion         int64     `json:"config_version"`
	Mode                  Mode      `json:"mode"`
	Transport             string    `json:"transport"`
	WebSocketTurn         int       `json:"websocket_turn,omitempty"`
	UserID                *int64    `json:"user_id,omitempty"`
	UserLabel             string    `json:"user_label"`
	APIKeyID              *int64    `json:"api_key_id,omitempty"`
	APIKeyName            string    `json:"api_key_name"`
	GroupID               *int64    `json:"group_id,omitempty"`
	GroupName             string    `json:"group_name"`
	RequestID             string    `json:"request_id"`
	ClientRequestID       string    `json:"client_request_id"`
	Method                string    `json:"method"`
	Path                  string    `json:"path"`
	Model                 string    `json:"model"`
	ClientIP              string    `json:"client_ip"`
	UserAgent             string    `json:"user_agent"`
	RequestContentType    string    `json:"request_content_type"`
	ResponseContentType   string    `json:"response_content_type"`
	HTTPStatus            int       `json:"http_status"`
	RequestTotalBytes     int64     `json:"request_total_bytes"`
	RequestCapturedBytes  int64     `json:"request_captured_bytes"`
	RequestTruncated      bool      `json:"request_truncated"`
	RequestStatus         string    `json:"request_status"`
	ResponseTotalBytes    int64     `json:"response_total_bytes"`
	ResponseCapturedBytes int64     `json:"response_captured_bytes"`
	ResponseTruncated     bool      `json:"response_truncated"`
	ResponseStatus        string    `json:"response_status"`
	Outcome               string    `json:"outcome"`
}

type RecordPage struct {
	Items    []Record `json:"items"`
	Page     int      `json:"page"`
	PageSize int      `json:"page_size"`
	Total    int64    `json:"total"`
}

type AccessLog struct {
	ID        int64     `json:"id"`
	RecordID  int64     `json:"record_id"`
	AdminID   *int64    `json:"admin_id,omitempty"`
	AdminName string    `json:"admin_name"`
	Reason    string    `json:"reason"`
	Outcome   string    `json:"outcome"`
	ClientIP  string    `json:"client_ip"`
	UserAgent string    `json:"user_agent"`
	CreatedAt time.Time `json:"created_at"`
}

type PayloadView struct {
	Available       bool               `json:"available"`
	Status          string             `json:"status"`
	ContentType     string             `json:"content_type"`
	Encoding        string             `json:"encoding,omitempty"`
	Compression     string             `json:"compression,omitempty"`
	Data            string             `json:"data,omitempty"`
	TotalBytes      int64              `json:"total_bytes"`
	CapturedBytes   int64              `json:"captured_bytes"`
	Offset          int64              `json:"offset"`
	LoadedBytes     int64              `json:"loaded_bytes"`
	Complete        bool               `json:"complete"`
	Truncated       bool               `json:"truncated"`
	Frames          []PayloadFrameView `json:"frames,omitempty"`
	FramesTruncated bool               `json:"frames_truncated,omitempty"`
}

// PayloadFrameView exposes a WebSocket frame after its parent archive record
// has been decrypted. It is omitted for ordinary HTTP payloads.
type PayloadFrameView struct {
	Sequence      int       `json:"sequence"`
	Kind          string    `json:"kind"`
	OccurredAt    time.Time `json:"occurred_at"`
	Offset        int64     `json:"offset"`
	TotalBytes    int64     `json:"total_bytes"`
	CapturedBytes int64     `json:"captured_bytes"`
	Truncated     bool      `json:"truncated"`
}

type Reveal struct {
	RecordID   int64       `json:"record_id"`
	RevealedAt time.Time   `json:"revealed_at"`
	Request    PayloadView `json:"request"`
	Response   PayloadView `json:"response"`
}

type PayloadSlot string

const (
	PayloadSlotRequest  PayloadSlot = "request"
	PayloadSlotResponse PayloadSlot = "response"
)

// PayloadChunk is a bounded direct-view segment. Large payloads are reviewed
// in segments so opening a record never transfers or renders the whole body.
type PayloadChunk struct {
	RecordID   int64       `json:"record_id"`
	Slot       PayloadSlot `json:"slot"`
	Payload    PayloadView `json:"payload"`
	NextOffset int64       `json:"next_offset"`
}

type ArchiveStorageStats struct {
	RecordCount        int64      `json:"record_count"`
	BlockCount         int64      `json:"block_count"`
	CapturedBytes      int64      `json:"captured_bytes"`
	PayloadBytes       int64      `json:"payload_bytes"`
	DatabaseBytes      int64      `json:"database_bytes"`
	CompressedRecords  int64      `json:"compressed_records"`
	CompressedPayloads int64      `json:"compressed_payloads"`
	SavedBytes         int64      `json:"saved_bytes"`
	UpdatedAt          *time.Time `json:"updated_at,omitempty"`
}

type CompressionRuntime struct {
	Enabled          bool       `json:"enabled"`
	Runs             uint64     `json:"runs"`
	Records          uint64     `json:"records"`
	Payloads         uint64     `json:"payloads"`
	SavedBytes       uint64     `json:"saved_bytes"`
	LastCheckedAt    *time.Time `json:"last_checked_at,omitempty"`
	LastCompressedAt *time.Time `json:"last_compressed_at,omitempty"`
	LastError        string     `json:"last_error,omitempty"`
	LastErrorAt      *time.Time `json:"last_error_at,omitempty"`
}

type RuntimeSnapshot struct {
	Started            bool                `json:"started"`
	ConfigVersion      int64               `json:"config_version"`
	QueueDepth         int                 `json:"queue_depth"`
	QueueCapacity      int                 `json:"queue_capacity"`
	Accepted           uint64              `json:"accepted"`
	Dropped            uint64              `json:"dropped"`
	Persisted          uint64              `json:"persisted"`
	PersistFailures    uint64              `json:"persist_failures"`
	ExpiredPurged      uint64              `json:"expired_purged"`
	Storage            ArchiveStorageStats `json:"storage"`
	Compression        CompressionRuntime  `json:"compression"`
	LastConfigError    string              `json:"last_config_error,omitempty"`
	LastConfigErrorAt  *time.Time          `json:"last_config_error_at,omitempty"`
	LastPersistError   string              `json:"last_persist_error,omitempty"`
	LastPersistErrorAt *time.Time          `json:"last_persist_error_at,omitempty"`
	LastStorageError   string              `json:"last_storage_error,omitempty"`
	LastStorageErrorAt *time.Time          `json:"last_storage_error_at,omitempty"`
}

var (
	ErrRecordNotFound      = errors.New("invocation archive record not found")
	ErrDirectViewDisabled  = errors.New("invocation archive direct view is disabled")
	ErrPayloadUnavailable  = errors.New("invocation archive payload unavailable")
	ErrPayloadExpired      = errors.New("invocation archive payload expired")
	ErrPayloadRangeInvalid = errors.New("invocation archive payload range is invalid")
)

func DefaultConfig() Config {
	return Config{
		ConfigVersion:     1,
		DefaultMode:       ModeOff,
		RetentionDays:     defaultRetentionDays,
		MaxRequestBytes:   defaultMaxRequestBytes,
		MaxResponseBytes:  defaultMaxResponseBytes,
		DirectViewEnabled: false,
		Compression:       DefaultCompressionConfig(),
		Rules:             []ScopeRule{},
	}
}

func DefaultCompressionConfig() CompressionConfig {
	return CompressionConfig{
		Enabled: true, AfterHours: defaultCompressionAfterHours, MinBytes: defaultCompressionMinBytes,
		TriggerBytes: defaultCompressionTriggerBytes, TriggerRecords: defaultCompressionTriggerRecords,
		BatchSize: defaultCompressionBatchSize, IntervalMinutes: defaultCompressionIntervalMinutes,
	}
}

func ParseConfig(raw string) (Config, error) {
	cfg := DefaultConfig()
	if strings.TrimSpace(raw) == "" {
		return cfg, nil
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return Config{}, fmt.Errorf("decode invocation archive config: %w", err)
	}
	normalizeConfig(&cfg)
	if err := validateConfig(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Public() Config {
	return cloneConfig(c)
}

func (c Config) Resolve(userID int64, groupID *int64, apiKeyID int64) Mode {
	if mode, ok := ruleMode(c.Rules, ScopeAPIKey, apiKeyID); ok {
		return mode
	}
	if mode, ok := ruleMode(c.Rules, ScopeUser, userID); ok {
		return mode
	}
	if groupID != nil {
		if mode, ok := ruleMode(c.Rules, ScopeGroup, *groupID); ok {
			return mode
		}
	}
	return c.DefaultMode
}

func ruleMode(rules []ScopeRule, scope Scope, subjectID int64) (Mode, bool) {
	for _, rule := range rules {
		if rule.Scope == scope && rule.SubjectID == subjectID {
			return rule.Mode, true
		}
	}
	return ModeOff, false
}

func normalizeConfig(cfg *Config) {
	if cfg == nil {
		return
	}
	if cfg.ConfigVersion < 1 {
		cfg.ConfigVersion = 1
	}
	cfg.DefaultMode = normalizeMode(cfg.DefaultMode)
	if cfg.RetentionDays == 0 {
		cfg.RetentionDays = defaultRetentionDays
	}
	if cfg.MaxRequestBytes == 0 {
		cfg.MaxRequestBytes = defaultMaxRequestBytes
	}
	if cfg.MaxResponseBytes == 0 {
		cfg.MaxResponseBytes = defaultMaxResponseBytes
	}
	normalizeCompressionConfig(&cfg.Compression)
	if cfg.Rules == nil {
		cfg.Rules = []ScopeRule{}
	}
	for index := range cfg.Rules {
		cfg.Rules[index].Scope = Scope(strings.TrimSpace(string(cfg.Rules[index].Scope)))
		cfg.Rules[index].Mode = normalizeMode(cfg.Rules[index].Mode)
	}
	sort.Slice(cfg.Rules, func(i, j int) bool {
		if cfg.Rules[i].Scope != cfg.Rules[j].Scope {
			return cfg.Rules[i].Scope < cfg.Rules[j].Scope
		}
		return cfg.Rules[i].SubjectID < cfg.Rules[j].SubjectID
	})
}

func normalizeCompressionConfig(cfg *CompressionConfig) {
	if cfg == nil {
		return
	}
	if cfg.MinBytes == 0 {
		cfg.MinBytes = defaultCompressionMinBytes
	}
	if cfg.BatchSize == 0 {
		cfg.BatchSize = defaultCompressionBatchSize
	}
	if cfg.IntervalMinutes == 0 {
		cfg.IntervalMinutes = defaultCompressionIntervalMinutes
	}
}

func validateConfig(cfg Config) error {
	if cfg.ConfigVersion < 1 {
		return infraerrors.BadRequest("invocation_archive_config_version_invalid", "归档配置版本无效")
	}
	if !validMode(cfg.DefaultMode) {
		return infraerrors.BadRequest("invocation_archive_default_mode_invalid", "默认归档模式无效")
	}
	if cfg.RetentionDays < 1 || cfg.RetentionDays > maxRetentionDays {
		return infraerrors.BadRequest("invocation_archive_retention_invalid", "保留天数必须在 1-3650 天之间")
	}
	if !validCaptureBytes(cfg.MaxRequestBytes) || !validCaptureBytes(cfg.MaxResponseBytes) {
		return infraerrors.BadRequest("invocation_archive_capture_limit_invalid", "归档载荷上限必须在 1 KiB 到 256 MiB 之间")
	}
	if err := validateCompressionConfig(cfg.Compression); err != nil {
		return err
	}
	if len(cfg.Rules) > maxScopeRules {
		return infraerrors.BadRequest("invocation_archive_rule_limit_exceeded", "归档范围规则不能超过 500 条")
	}
	seen := make(map[string]struct{}, len(cfg.Rules))
	for _, rule := range cfg.Rules {
		if !validScope(rule.Scope) || rule.SubjectID <= 0 || !validMode(rule.Mode) {
			return infraerrors.BadRequest("invocation_archive_rule_invalid", "归档范围规则无效")
		}
		key := string(rule.Scope) + ":" + fmt.Sprint(rule.SubjectID)
		if _, exists := seen[key]; exists {
			return infraerrors.BadRequest("invocation_archive_rule_duplicate", "归档范围规则重复")
		}
		seen[key] = struct{}{}
	}
	return nil
}

func validateCompressionConfig(cfg CompressionConfig) error {
	if cfg.AfterHours < 0 || cfg.AfterHours > maxCompressionAfterHours {
		return infraerrors.BadRequest("invocation_archive_compression_age_invalid", "归档压缩时间阈值必须在 0-8760 小时之间")
	}
	if !validCaptureBytes(cfg.MinBytes) {
		return infraerrors.BadRequest("invocation_archive_compression_min_bytes_invalid", "归档压缩最小载荷必须在 1 KiB 到 256 MiB 之间")
	}
	if cfg.TriggerBytes < 0 || cfg.TriggerBytes > maxCompressionTriggerBytes {
		return infraerrors.BadRequest("invocation_archive_compression_size_invalid", "归档压缩容量阈值无效")
	}
	if cfg.TriggerRecords < 0 || cfg.TriggerRecords > maxCompressionTriggerRecord {
		return infraerrors.BadRequest("invocation_archive_compression_record_count_invalid", "归档压缩记录数阈值无效")
	}
	if cfg.BatchSize < 1 || cfg.BatchSize > maxCompressionBatchSize {
		return infraerrors.BadRequest("invocation_archive_compression_batch_invalid", "归档压缩批量大小必须在 1-100 之间")
	}
	if cfg.IntervalMinutes < 1 || cfg.IntervalMinutes > maxCompressionInterval {
		return infraerrors.BadRequest("invocation_archive_compression_interval_invalid", "归档压缩间隔必须在 1-1440 分钟之间")
	}
	if cfg.Enabled && cfg.AfterHours == 0 && cfg.TriggerBytes == 0 && cfg.TriggerRecords == 0 {
		return infraerrors.BadRequest("invocation_archive_compression_trigger_required", "启用归档压缩时至少需要一个触发条件")
	}
	return nil
}

func configFromUpdate(current Config, req UpdateConfigRequest, actorID int64, now time.Time) (Config, error) {
	if req.ExpectedConfigVersion < 1 {
		return Config{}, infraerrors.BadRequest("invocation_archive_expected_config_version_required", "必须提供有效的归档配置版本")
	}
	next := Config{
		ConfigVersion:     current.ConfigVersion + 1,
		DefaultMode:       req.DefaultMode,
		RetentionDays:     req.RetentionDays,
		MaxRequestBytes:   req.MaxRequestBytes,
		MaxResponseBytes:  req.MaxResponseBytes,
		DirectViewEnabled: req.DirectViewEnabled,
		Compression:       req.Compression,
		Rules:             append([]ScopeRule(nil), req.Rules...),
		UpdatedAt:         now.UTC(),
		UpdatedBy:         actorID,
	}
	normalizeConfig(&next)
	if err := validateConfig(next); err != nil {
		return Config{}, err
	}
	return next, nil
}

func cloneConfig(cfg Config) Config {
	cfg.Rules = append([]ScopeRule{}, cfg.Rules...)
	return cfg
}

func normalizeMode(mode Mode) Mode {
	return Mode(strings.TrimSpace(string(mode)))
}

func validMode(mode Mode) bool {
	return mode == ModeOff || mode == ModeRequestOnly || mode == ModeFull
}

func validScope(scope Scope) bool {
	return scope == ScopeUser || scope == ScopeGroup || scope == ScopeAPIKey
}

func validCaptureBytes(value int) bool {
	return value >= minCaptureBytes && value <= maxCaptureBytes
}
