package invocationarchive

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

const (
	configLockKey         int64 = 579147893221901923
	archiveQueueCapacity        = 2048
	archiveWorkerCount          = 2
	archiveWriteTimeout         = 5 * time.Second
	configRefreshInterval       = 5 * time.Second
	// Cleanup is deliberately frequent: retention changes must take effect
	// without waiting for a long maintenance window, while the indexed expiry
	// query keeps each pass cheap when there is no backlog.
	cleanupInterval           = 5 * time.Minute
	cleanupBatchSize          = 500
	runtimeStatsInterval      = time.Minute
	archiveMaintenanceTimeout = 30 * time.Second
)

type capturedPayload struct {
	bytes           []byte
	contentType     string
	total           int64
	truncated       bool
	status          string
	frames          []capturedFrame
	framesTruncated bool
}

// capturedFrame describes one client-visible WebSocket frame. The body bytes
// remain stored once in capturedPayload; Offset and CapturedBytes preserve the
// exact frame boundaries without duplicating an often-large stream.
type capturedFrame struct {
	sequence      int
	kind          string
	occurredAt    time.Time
	offset        int64
	totalBytes    int64
	capturedBytes int64
	truncated     bool
}

type archiveCandidate struct {
	createdAt       time.Time
	completedAt     time.Time
	expiresAt       time.Time
	configVersion   int64
	mode            Mode
	transport       string
	websocketTurn   int
	identity        archiveRecordIdentity
	requestID       string
	clientRequestID string
	method          string
	path            string
	model           string
	clientIP        string
	userAgent       string
	httpStatus      int
	outcome         string
	request         capturedPayload
	response        capturedPayload
}

type storedRecord struct {
	Record
}

type Service struct {
	db        *sql.DB
	settings  service.SettingRepository
	encryptor service.SecretEncryptor

	snapshot atomic.Pointer[Config]

	lifecycleMu sync.Mutex
	cleanupMu   sync.Mutex
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	queue       chan archiveCandidate
	accepting   bool
	started     atomic.Bool

	accepted        atomic.Uint64
	dropped         atomic.Uint64
	persisted       atomic.Uint64
	persistFailures atomic.Uint64
	expiredPurged   atomic.Uint64

	stateMu                      sync.RWMutex
	lastConfigError              string
	lastConfigErrorAt            *time.Time
	lastPersistError             string
	lastPersistErrorAt           *time.Time
	lastStorageError             string
	lastStorageErrorAt           *time.Time
	lastCleanupAt                *time.Time
	lastCleanupStrategy          CleanupStrategy
	lastCleanupDeleted           int64
	lastCleanupAccessLogsDeleted int64
	lastCleanupError             string
	lastCleanupErrorAt           *time.Time
	storage                      ArchiveStorageStats
	compression                  CompressionRuntime
	retentionConfigVersion       atomic.Int64
}

func NewService(db *sql.DB, settings service.SettingRepository, encryptor service.SecretEncryptor) *Service {
	archive := &Service{
		db: db, settings: settings, encryptor: encryptor,
		queue: make(chan archiveCandidate, archiveQueueCapacity),
	}
	defaultConfig := DefaultConfig()
	archive.snapshot.Store(&defaultConfig)
	return archive
}

func (s *Service) Start(ctx context.Context) error {
	if s == nil {
		return errors.New("invocation archive service unavailable")
	}
	s.lifecycleMu.Lock()
	if s.cancel != nil {
		s.lifecycleMu.Unlock()
		return nil
	}
	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.accepting = true
	s.started.Store(true)
	for range archiveWorkerCount {
		s.wg.Add(1)
		go s.persistWorker()
	}
	s.wg.Add(1)
	go s.maintenanceLoop(runCtx)
	s.lifecycleMu.Unlock()

	reloadErr := s.Reload(runCtx)
	if reloadErr == nil {
		if err := s.reconcileRetention(runCtx, s.activeConfig().RetentionDays); err != nil {
			s.setCleanupResult(CleanupResult{Strategy: CleanupExpired}, err)
		} else {
			s.retentionConfigVersion.Store(s.activeConfig().ConfigVersion)
		}
	}
	// Run one cleanup with the freshly loaded retention policy instead of the
	// in-memory default; otherwise a retention change would wait for the first
	// ticker interval after every restart.
	if reloadErr == nil {
		_, _ = s.purgeExpired(runCtx)
	}
	return reloadErr
}

func (s *Service) Shutdown(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.lifecycleMu.Lock()
	cancel := s.cancel
	if cancel == nil {
		s.lifecycleMu.Unlock()
		return nil
	}
	s.accepting = false
	s.cancel = nil
	s.started.Store(false)
	cancel()
	close(s.queue)
	s.lifecycleMu.Unlock()

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) activeConfig() Config {
	if s == nil {
		return DefaultConfig()
	}
	if cfg := s.snapshot.Load(); cfg != nil {
		return *cfg
	}
	return DefaultConfig()
}

func (s *Service) GetConfig() Config {
	return cloneConfig(s.activeConfig())
}

func (s *Service) Reload(ctx context.Context) error {
	if s == nil || s.settings == nil {
		err := errors.New("invocation archive setting repository unavailable")
		s.installDisabledConfig(err)
		return err
	}
	values, err := s.settings.GetMultiple(ctx, []string{SettingKeyConfig})
	if err != nil {
		s.installDisabledConfig(err)
		return err
	}
	cfg, err := ParseConfig(values[SettingKeyConfig])
	if err != nil {
		s.installDisabledConfig(err)
		return err
	}
	s.snapshot.Store(&cfg)
	s.clearConfigError()
	return nil
}

func (s *Service) SaveConfig(ctx context.Context, request UpdateConfigRequest, actorID int64) (Config, error) {
	if s == nil || s.db == nil {
		return Config{}, errors.New("invocation archive configuration persistence unavailable")
	}
	if actorID <= 0 {
		return Config{}, infraerrors.Forbidden("invocation_archive_admin_required", "管理员身份无效")
	}
	transaction, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Config{}, err
	}
	defer func() { _ = transaction.Rollback() }()
	if _, err := transaction.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, configLockKey); err != nil {
		return Config{}, err
	}
	current := DefaultConfig()
	var raw string
	err = transaction.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=$1 FOR UPDATE`, SettingKeyConfig).Scan(&raw)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Config{}, err
	}
	if err == nil {
		current, err = ParseConfig(raw)
		if err != nil {
			return Config{}, fmt.Errorf("decode existing invocation archive config: %w", err)
		}
	}
	if current.ConfigVersion != request.ExpectedConfigVersion {
		return Config{}, infraerrors.Conflict("invocation_archive_config_conflict", "调用归档配置已被其他管理员更新")
	}
	next, err := configFromUpdate(current, request, actorID, time.Now())
	if err != nil {
		return Config{}, err
	}
	if err := validateRuleSubjects(ctx, transaction, next.Rules); err != nil {
		return Config{}, err
	}
	if err := persistConfigVersion(ctx, transaction, current); err != nil {
		return Config{}, err
	}
	if err := persistConfigVersion(ctx, transaction, next); err != nil {
		return Config{}, err
	}
	nextRaw, err := json.Marshal(next)
	if err != nil {
		return Config{}, err
	}
	if _, err := transaction.ExecContext(ctx, `
		INSERT INTO settings (key,value,updated_at) VALUES ($1,$2,NOW())
		ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value, updated_at=EXCLUDED.updated_at`,
		SettingKeyConfig, string(nextRaw)); err != nil {
		return Config{}, err
	}
	if _, err := transaction.ExecContext(ctx, `
		UPDATE invocation_archive_records
		SET expires_at = created_at + ($1 * INTERVAL '1 day')
		WHERE expires_at > created_at + ($1 * INTERVAL '1 day')`, next.RetentionDays); err != nil {
		return Config{}, err
	}
	if err := transaction.Commit(); err != nil {
		return Config{}, err
	}
	s.snapshot.Store(&next)
	s.retentionConfigVersion.Store(next.ConfigVersion)
	s.clearConfigError()
	return cloneConfig(next), nil
}

func (s *Service) Runtime() RuntimeSnapshot {
	if s == nil {
		return RuntimeSnapshot{ConfigVersion: DefaultConfig().ConfigVersion, QueueCapacity: archiveQueueCapacity}
	}
	cfg := s.activeConfig()
	s.stateMu.RLock()
	lastConfigError := s.lastConfigError
	lastConfigErrorAt := cloneTime(s.lastConfigErrorAt)
	lastPersistError := s.lastPersistError
	lastPersistErrorAt := cloneTime(s.lastPersistErrorAt)
	lastStorageError := s.lastStorageError
	lastStorageErrorAt := cloneTime(s.lastStorageErrorAt)
	lastCleanupAt := cloneTime(s.lastCleanupAt)
	lastCleanupStrategy := s.lastCleanupStrategy
	lastCleanupDeleted := s.lastCleanupDeleted
	lastCleanupAccessLogsDeleted := s.lastCleanupAccessLogsDeleted
	lastCleanupError := s.lastCleanupError
	lastCleanupErrorAt := cloneTime(s.lastCleanupErrorAt)
	storage := cloneArchiveStorageStats(s.storage)
	compression := cloneCompressionRuntime(s.compression)
	s.stateMu.RUnlock()
	compression.Enabled = cfg.Compression.Enabled
	queueDepth := 0
	if s.queue != nil {
		queueDepth = len(s.queue)
	}
	return RuntimeSnapshot{
		Started: s.started.Load(), ConfigVersion: cfg.ConfigVersion,
		QueueDepth: queueDepth, QueueCapacity: archiveQueueCapacity,
		Accepted: s.accepted.Load(), Dropped: s.dropped.Load(), Persisted: s.persisted.Load(),
		PersistFailures: s.persistFailures.Load(), ExpiredPurged: s.expiredPurged.Load(),
		LastCleanupAt: lastCleanupAt, LastCleanupDeleted: lastCleanupDeleted,
		LastCleanupStrategy:          lastCleanupStrategy,
		LastCleanupAccessLogsDeleted: lastCleanupAccessLogsDeleted,
		Storage:                      storage, Compression: compression,
		LastConfigError: lastConfigError, LastConfigErrorAt: lastConfigErrorAt,
		LastPersistError: lastPersistError, LastPersistErrorAt: lastPersistErrorAt,
		LastStorageError: lastStorageError, LastStorageErrorAt: lastStorageErrorAt,
		LastCleanupError: lastCleanupError, LastCleanupErrorAt: lastCleanupErrorAt,
	}
}

func validateRuleSubjects(ctx context.Context, transaction *sql.Tx, rules []ScopeRule) error {
	if len(rules) == 0 {
		return nil
	}
	idsByScope := map[Scope][]int64{}
	for _, rule := range rules {
		idsByScope[rule.Scope] = append(idsByScope[rule.Scope], rule.SubjectID)
	}
	for scope, ids := range idsByScope {
		var query string
		switch scope {
		case ScopeUser:
			query = `SELECT COUNT(*) FROM users WHERE deleted_at IS NULL AND id = ANY($1)`
		case ScopeGroup:
			query = `SELECT COUNT(*) FROM groups WHERE deleted_at IS NULL AND id = ANY($1)`
		case ScopeAPIKey:
			query = `SELECT COUNT(*) FROM api_keys WHERE deleted_at IS NULL AND id = ANY($1)`
		default:
			return infraerrors.BadRequest("invocation_archive_rule_invalid", "归档范围规则无效")
		}
		var count int
		if err := transaction.QueryRowContext(ctx, query, pq.Array(ids)).Scan(&count); err != nil {
			return err
		}
		if count != len(ids) {
			return infraerrors.BadRequest("invocation_archive_rule_subject_not_found", "归档范围规则包含不存在或已删除的主体")
		}
	}
	return nil
}

func (s *Service) enqueue(candidate archiveCandidate) {
	if s == nil {
		return
	}
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if !s.accepting {
		s.dropped.Add(1)
		return
	}
	select {
	case s.queue <- candidate:
		s.accepted.Add(1)
	default:
		s.dropped.Add(1)
	}
}

func (s *Service) persistWorker() {
	defer s.wg.Done()
	for candidate := range s.queue {
		ctx, cancel := context.WithTimeout(context.Background(), archiveWriteTimeout)
		err := s.persistCandidate(ctx, candidate)
		cancel()
		if err != nil {
			s.persistFailures.Add(1)
			s.setPersistError(err)
			continue
		}
		s.persisted.Add(1)
	}
}

func (s *Service) maintenanceLoop(ctx context.Context) {
	defer s.wg.Done()
	s.shardLegacyPayloads(context.Background())
	s.refreshStorageStats(context.Background())
	s.maybeCompactArchive(context.Background())
	configTicker := time.NewTicker(configRefreshInterval)
	cleanupTicker := time.NewTicker(cleanupInterval)
	statsTicker := time.NewTicker(runtimeStatsInterval)
	defer configTicker.Stop()
	defer cleanupTicker.Stop()
	defer statsTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-configTicker.C:
			if err := s.Reload(ctx); err == nil {
				cfg := s.activeConfig()
				if cfg.ConfigVersion != s.retentionConfigVersion.Load() {
					if err := s.reconcileRetention(ctx, cfg.RetentionDays); err != nil {
						s.setCleanupResult(CleanupResult{Strategy: CleanupExpired}, err)
					} else {
						s.retentionConfigVersion.Store(cfg.ConfigVersion)
					}
				}
			}
		case <-cleanupTicker.C:
			_, _ = s.purgeExpired(ctx)
			s.refreshStorageStats(ctx)
		case <-statsTicker.C:
			s.shardLegacyPayloads(ctx)
			s.refreshStorageStats(ctx)
			s.maybeCompactArchive(ctx)
		}
	}
}

func (s *Service) purgeExpired(parent context.Context) (CleanupResult, error) {
	if s == nil {
		return CleanupResult{Strategy: CleanupExpired}, errors.New("invocation archive service unavailable")
	}
	s.cleanupMu.Lock()
	defer s.cleanupMu.Unlock()
	result := CleanupResult{Strategy: CleanupExpired, CompletedAt: time.Now().UTC()}
	if s.db == nil {
		return result, errors.New("invocation archive database unavailable")
	}
	if configErr := s.currentConfigError(); configErr != "" {
		err := fmt.Errorf("invocation archive configuration unavailable: %s", configErr)
		s.setCleanupResult(result, err)
		return result, err
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), archiveMaintenanceTimeout)
	defer cancel()
	now := time.Now().UTC()
	result.CompletedAt = now
	retentionDays := s.activeConfig().RetentionDays
	for {
		deleted, err := s.deleteExpired(ctx, now, retentionDays, cleanupBatchSize)
		if err != nil {
			s.setCleanupResult(result, err)
			return result, err
		}
		result.DeletedRecords += int64(deleted)
		s.expiredPurged.Add(uint64(deleted))
		if deleted < cleanupBatchSize {
			break
		}
	}
	for {
		deleted, err := s.deleteExpiredAccessLogs(ctx, now.AddDate(0, 0, -retentionDays), cleanupBatchSize)
		if err != nil {
			s.setCleanupResult(result, err)
			return result, err
		}
		result.DeletedAccessLogs += int64(deleted)
		if deleted < cleanupBatchSize {
			break
		}
	}
	s.setCleanupResult(result, nil)
	s.refreshStorageStats(ctx)
	return result, nil
}

func (s *Service) Cleanup(ctx context.Context, request CleanupRequest) (CleanupResult, error) {
	if s == nil || s.db == nil {
		return CleanupResult{}, errors.New("invocation archive database unavailable")
	}
	if request.Strategy == CleanupExpired {
		return s.purgeExpired(ctx)
	}
	if request.Strategy != CleanupAll || !request.Confirm {
		return CleanupResult{}, infraerrors.BadRequest("invocation_archive_cleanup_confirmation_required", "清空归档记录必须明确确认")
	}
	s.cleanupMu.Lock()
	defer s.cleanupMu.Unlock()
	if configErr := s.currentConfigError(); configErr != "" {
		err := fmt.Errorf("invocation archive configuration unavailable: %s", configErr)
		result := CleanupResult{Strategy: CleanupAll, CompletedAt: time.Now().UTC()}
		s.setCleanupResult(result, err)
		return result, err
	}
	result := CleanupResult{Strategy: CleanupAll, CompletedAt: time.Now().UTC()}
	maintenanceCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), archiveMaintenanceTimeout)
	defer cancel()
	for {
		deleted, err := s.deleteAll(maintenanceCtx, cleanupBatchSize)
		if err != nil {
			s.setCleanupResult(result, err)
			return result, err
		}
		result.DeletedRecords += int64(deleted)
		if deleted < cleanupBatchSize {
			break
		}
	}
	s.setCleanupResult(result, nil)
	s.refreshStorageStats(maintenanceCtx)
	return result, nil
}

func (s *Service) setCleanupResult(result CleanupResult, err error) {
	if s == nil {
		return
	}
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	now := time.Now().UTC()
	s.lastCleanupAt = &now
	s.lastCleanupStrategy = result.Strategy
	s.lastCleanupDeleted = result.DeletedRecords
	s.lastCleanupAccessLogsDeleted = result.DeletedAccessLogs
	if err != nil {
		s.lastCleanupError = trimText(err.Error(), 512)
		s.lastCleanupErrorAt = &now
		return
	}
	s.lastCleanupError = ""
	s.lastCleanupErrorAt = nil
}

func (s *Service) currentConfigError() string {
	if s == nil {
		return "invocation archive service unavailable"
	}
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return s.lastConfigError
}

func (s *Service) installDisabledConfig(err error) {
	cfg := DefaultConfig()
	s.snapshot.Store(&cfg)
	s.stateMu.Lock()
	now := time.Now().UTC()
	s.lastConfigError = trimText(err.Error(), 512)
	s.lastConfigErrorAt = &now
	s.stateMu.Unlock()
}

func (s *Service) clearConfigError() {
	if s == nil {
		return
	}
	s.stateMu.Lock()
	s.lastConfigError = ""
	s.lastConfigErrorAt = nil
	s.stateMu.Unlock()
}

func (s *Service) setPersistError(err error) {
	if s == nil || err == nil {
		return
	}
	s.stateMu.Lock()
	now := time.Now().UTC()
	s.lastPersistError = trimText(err.Error(), 512)
	s.lastPersistErrorAt = &now
	s.stateMu.Unlock()
}

func (s *Service) setStorageError(err error) {
	if s == nil || err == nil {
		return
	}
	s.stateMu.Lock()
	now := time.Now().UTC()
	s.lastStorageError = trimText(err.Error(), 512)
	s.lastStorageErrorAt = &now
	s.stateMu.Unlock()
}

func (s *Service) clearStorageError() {
	if s == nil {
		return
	}
	s.stateMu.Lock()
	s.lastStorageError = ""
	s.lastStorageErrorAt = nil
	s.stateMu.Unlock()
}

func cloneArchiveStorageStats(value ArchiveStorageStats) ArchiveStorageStats {
	value.UpdatedAt = cloneTime(value.UpdatedAt)
	return value
}

func cloneCompressionRuntime(value CompressionRuntime) CompressionRuntime {
	value.LastCheckedAt = cloneTime(value.LastCheckedAt)
	value.LastCompressedAt = cloneTime(value.LastCompressedAt)
	value.LastErrorAt = cloneTime(value.LastErrorAt)
	return value
}

func persistConfigVersion(ctx context.Context, transaction *sql.Tx, cfg Config) error {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(raw)
	_, err = transaction.ExecContext(ctx, `
		INSERT INTO invocation_archive_config_versions(config_version,config_json,config_digest,created_by,created_at)
		VALUES ($1,$2::jsonb,$3,$4,COALESCE($5::timestamptz,NOW()))
		ON CONFLICT (config_version) DO NOTHING`,
		cfg.ConfigVersion, string(raw), hex.EncodeToString(digest[:]), nullableID(cfg.UpdatedBy), nullableTime(cfg.UpdatedAt))
	return err
}

func nullableID(value int64) any {
	if value <= 0 {
		return nil
	}
	return value
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
